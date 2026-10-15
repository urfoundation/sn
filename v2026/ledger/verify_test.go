package ledger

import (
	"bytes"
	"context"
	"errors"
	"io"
	"maps"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/holiman/uint256"

	"github.com/urfoundation/sn/v2026/merkle"
	"github.com/urfoundation/sn/v2026/protocol"
)

func cloneCustody(c *MemoryCustody) *MemoryCustody {
	return &MemoryCustody{Objects: maps.Clone(c.Objects), Rows: maps.Clone(c.Rows), Nulls: maps.Clone(c.Nulls)}
}

func objectBytes(objects Objects) uint64 {
	var n uint64
	for _, data := range objects {
		n += uint64(len(data))
	}
	return n
}

// GL-9 (baseline): an honest root passes every check. The audit keeps what
// it fetched and records each epoch's valid rows; auditing again fetches
// nothing.
func TestVerifyHonest(t *testing.T) {
	s := newScenario(t)
	res := s.verify(t, s.p.Commit, s.p.Objects)
	expectPass(t, res)
	if res.FetchedBytes != objectBytes(s.p.Objects) {
		t.Fatalf("fetched %d bytes, objects hold %d", res.FetchedBytes, objectBytes(s.p.Objects))
	}
	if root, _ := res.Previous.Root(); root != s.f.Commit.Root || !slices.Equal(res.PreviousAccounts, s.f.Accounts) {
		t.Fatal("previous root not returned")
	}
	if root, _ := res.Tree.Root(); root != s.p.Commit.Root || !slices.Equal(res.Accounts, s.p.Accounts) {
		t.Fatal("pending tree not returned")
	}
	for ref := range s.p.Objects {
		if !bytes.Equal(s.custody.Objects[ref], s.p.Objects[ref]) {
			t.Fatalf("object %v not kept", ref)
		}
	}
	for i, epoch := range []uint64{11, 12} {
		if chunks, ok := s.custody.Rows[PoolEpoch{testNoID, epoch}]; !ok || !slices.Equal(chunks, s.p.Epochs[i].Providers) {
			t.Fatalf("epoch %d rows recorded as %v", epoch, chunks)
		}
	}
	again := s.verify(t, s.p.Commit, Objects{})
	expectPass(t, again)
	if again.FetchedBytes != 0 {
		t.Fatalf("second audit fetched %d bytes", again.FetchedBytes)
	}
}

// GL-9 (check 2): a validator without custody fetches and verifies the
// previous root's header and account chunks too.
func TestVerifyFreshCustody(t *testing.T) {
	s := newScenario(t)
	s.custody = NewMemoryCustody()
	objects := maps.Clone(s.p.Objects)
	maps.Copy(objects, s.f.Objects)
	res := s.verify(t, s.p.Commit, objects)
	expectPass(t, res)
	previous := len(s.f.Header)
	for _, chunk := range encodeAccountChunks(s.f.Accounts) {
		previous += len(chunk)
	}
	if res.FetchedBytes != objectBytes(s.p.Objects)+uint64(previous) {
		t.Fatalf("fetched %d bytes", res.FetchedBytes)
	}
}

// GL-6: fixed inputs. A later root listing the chunks of an epoch whose
// rows were found valid passes; rows changed, or null, for such an epoch
// are refused; null for any other epoch is accepted and recorded.
func TestVerifyFixedInputs(t *testing.T) {
	s := newScenario(t)
	// P1 covers epoch 11 alone; it passes but is not finalized.
	p1, err := buildOver(s.f, []EpochInput{{Epoch: 11, Rows: epoch11Rows()}}, s.funded)
	if err != nil {
		t.Fatal(err)
	}
	expectPass(t, s.verify(t, p1.Commit, p1.Objects))
	held := s.custody.Rows[PoolEpoch{testNoID, 11}]
	if !slices.Equal(held, p1.Epochs[0].Providers) {
		t.Fatal("epoch 11 rows not recorded")
	}
	base := cloneCustody(s.custody)
	// P2 replaces P1, covering 11 and 12 with epoch 11's chunks.
	if !slices.Equal(s.p.Epochs[0].Providers, held) {
		t.Fatal("P2 does not list epoch 11's chunks")
	}
	expectPass(t, s.verify(t, s.p.Commit, s.p.Objects))

	// GL-9 (check 3): rows changed.
	s.custody = cloneCustody(base)
	changed := s.build(t, append(epoch11Rows(), row(14, 9, 40, false)), epoch12Rows())
	expectFailure(t, s.verify(t, changed.Commit, changed.Objects), CheckFixedInputs, ActionRefuse, ErrFixedInputs)

	// GL-9 (check 3): null against held rows.
	s.custody = cloneCustody(base)
	declared, err := buildOver(s.f, []EpochInput{{Epoch: 11, Null: true}, {Epoch: 12, Rows: epoch12Rows()}}, s.funded)
	if err != nil {
		t.Fatal(err)
	}
	expectFailure(t, s.verify(t, declared.Commit, declared.Objects), CheckFixedInputs, ActionRefuse, ErrNullAgainstValidRows)

	// Null for an epoch without valid rows is accepted and recorded.
	fresh := newScenario(t)
	in := fresh.input(declared.Commit, declared.Objects)
	in.Seq = 9
	expectPass(t, mustVerify(t, in))
	if seqs := fresh.custody.Nulls[PoolEpoch{testNoID, 11}]; !slices.Equal(seqs, []uint64{9}) {
		t.Fatalf("null recorded for seqs %v", seqs)
	}
	if _, ok := fresh.custody.Rows[PoolEpoch{testNoID, 11}]; ok {
		t.Fatal("rows recorded for a null epoch")
	}
}

// bindingVersion is one FleetBound binding of a client as STCoordinator
// holds it at one block.
type bindingVersion struct {
	validFrom, validTo uint64
	// cleanedAt is the epoch a cleaned binding stops at; 0 when not cleaned.
	cleanedAt uint64
	// live: the hotkey's UID exists and is the recorded one.
	live bool
}

// bindingAt mirrors STCoordinator.bindingAt over a client's versions at one
// block.
func bindingAt(versions []bindingVersion, epoch uint64) bool {
	for i := len(versions) - 1; i >= 0; i-- {
		v := versions[i]
		if epoch < v.validFrom {
			continue
		}
		return epoch <= v.validTo && (v.cleanedAt == 0 || epoch < v.cleanedAt) && v.live
	}
	return false
}

// GL-7: head exclusion decided from the candidate set (clients with a
// FleetBound event whose validity covers the epoch, with bindingAt at both
// boundaries for them only) equals per-provider bindingAt at both
// boundaries, and check 6 accepts exactly those flags.
func TestVerifyHeadExclusionCandidates(t *testing.T) {
	const epoch = 20
	random := rand.New(rand.NewPCG(7, 20))
	var rows []protocol.LedgerUsageRow
	oracle := heads{epoch: {}}
	calls := 0
	for client := 1; client <= 400; client++ {
		// Versions bound by the epoch's end; some were bound after its start.
		var atEnd []bindingVersion
		next := uint64(random.IntN(30))
		for n := random.IntN(4); n > 0; n-- {
			v := bindingVersion{validFrom: next, validTo: next + uint64(random.IntN(6)), live: random.IntN(5) > 0}
			if random.IntN(3) == 0 {
				v.cleanedAt = v.validFrom + uint64(random.IntN(8))
			}
			atEnd = append(atEnd, v)
			next = v.validTo + 1 + uint64(random.IntN(4))
		}
		atStart := slices.Clone(atEnd[:random.IntN(len(atEnd)+1)])
		for i := range atStart {
			// At the start a later-cleaned binding was not cleaned yet, and
			// the hotkey's UID may have changed since.
			if random.IntN(2) == 0 {
				atStart[i].cleanedAt = 0
			}
			atStart[i].live = random.IntN(5) > 0
		}
		flag := bindingAt(atStart, epoch) || bindingAt(atEnd, epoch)
		candidate := slices.ContainsFunc(atEnd, func(v bindingVersion) bool { return v.validFrom <= epoch && epoch <= v.validTo })
		if candidate {
			oracle[epoch][id(client)] = bindingAt(atStart, epoch) || bindingAt(atEnd, epoch)
			calls += 2
		} else if flag {
			t.Fatalf("client %d is active without a covering FleetBound event", client)
		}
		rows = append(rows, row(client, client%37+1, uint64(client), flag))
	}
	bound := 0
	for _, active := range oracle[epoch] {
		if active {
			bound++
		}
	}
	if bound == 0 || len(oracle[epoch]) == bound || calls >= 2*len(rows) {
		t.Fatalf("degenerate history: %d candidates, %d bound", len(oracle[epoch]), bound)
	}
	build := func(rows []protocol.LedgerUsageRow) *Ledger {
		l, err := Build(context.Background(), BuildInput{NoID: testNoID, Epoch: epoch, FirstEpoch: epoch, Epochs: []EpochInput{{Epoch: epoch, Rows: rows}}, FundedThrough: funded(epoch, 1_000_000)})
		if err != nil {
			t.Fatal(err)
		}
		return l
	}
	verify := func(l *Ledger) *Result {
		return mustVerify(t, VerifyInput{NoID: testNoID, FirstEpoch: epoch, Pending: l.Commit, Seq: 1, FundedThrough: funded(epoch, 1_000_000), Fetcher: l.Objects, Custody: NewMemoryCustody(), Heads: oracle, MaxFetchBytes: DefaultMaxFetchBytes})
	}
	expectPass(t, verify(build(rows)))
	for i := range rows {
		flipped := slices.Clone(rows)
		flipped[i].HeadExcluded = !flipped[i].HeadExcluded
		res := verify(build(flipped))
		if rows[i].HeadExcluded {
			expectFailure(t, res, CheckHeadExclusion, ActionRefuse, ErrHeadFlagCleared)
		} else {
			expectFailure(t, res, CheckHeadExclusion, ActionRefuse, ErrHeadFlagSet)
		}
	}
}

// GL-9 (check 1): header mutations.
func TestVerifyHeaderMutations(t *testing.T) {
	t.Run("sha256 is not the ledger hash", func(t *testing.T) {
		s := newScenario(t)
		pub := publish(t, s.p)
		pub.objects[s.p.Commit.LedgerHash] = bytes.Replace(s.p.Header, []byte(`"no_id":7`), []byte(`"no_id":8`), 1)
		res := s.verify(t, s.p.Commit, pub.objects)
		expectFailure(t, res, CheckHeader, ActionDemand, ErrLedgerHash)
		if !slices.Equal(res.Missing, []Ref{s.p.Commit.LedgerHash}) {
			t.Fatalf("missing %v", res.Missing)
		}
	})
	t.Run("non-canonical bytes", func(t *testing.T) {
		s := newScenario(t)
		pub := publish(t, s.p)
		commit := pub.sealBytes(bytes.Replace(s.p.Header, []byte(`"no_id":7`), []byte(`"no_id": 7`), 1))
		expectFailure(t, s.verify(t, commit, pub.objects), CheckHeader, ActionDemand, ErrHeaderNotCanonical)
	})
	t.Run("over 256 KiB", func(t *testing.T) {
		s := newScenario(t)
		pub := publish(t, s.p)
		commit := pub.sealBytes(headerOfSize(t, MaxHeaderBytes+1))
		expectFailure(t, s.verify(t, commit, pub.objects), CheckHeader, ActionDemand, ErrHeaderTooLarge)
	})
	cases := []struct {
		name   string
		mutate func(*publication)
		want   error
	}{
		{"no_id", func(pub *publication) { pub.header.NoID = 8 }, ErrHeaderNoID},
		{"epoch", func(pub *publication) { pub.header.Epoch = 13 }, ErrHeaderEpoch},
		{"covered range missing an epoch", func(pub *publication) { pub.header.Epochs = pub.header.Epochs[1:] }, ErrCoveredEpochs},
		{"covered range shifted", func(pub *publication) { pub.header.Epochs[0].Epoch = 10 }, ErrCoveredEpochs},
		{"epoch not after the previous root", func(pub *publication) { pub.header.Epoch, pub.commit.Epoch = 10, 10 }, ErrCoveredEpochs},
		{"covered range too long", func(pub *publication) {
			pub.header.Epoch, pub.commit.Epoch = 107, 107
			pub.header.Epochs = nil
			for k := uint64(12); k <= 107; k++ {
				pub.header.Epochs = append(pub.header.Epochs, HeaderEpoch{Epoch: k})
			}
		}, ErrTooManyEpochs},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newScenario(t)
			pub := publish(t, s.p)
			c.mutate(pub)
			expectFailure(t, s.verify(t, pub.seal(t), pub.objects), CheckHeader, ActionDemand, c.want)
		})
	}
	t.Run("more than 96 epochs listed", func(t *testing.T) {
		s := newScenario(t)
		pub := publish(t, s.p)
		raw := headerJSON{Schema: Schema, NoID: testNoID, Epoch: 107, Accounts: refStrings(pub.header.Accounts)}
		for k := uint64(11); k <= 107; k++ {
			raw.Epochs = append(raw.Epochs, headerEpochJSON{Epoch: k, Providers: []string{}})
		}
		commit := pub.sealBytes(raw.appendJSON(nil))
		commit.Epoch = 107
		expectFailure(t, s.verify(t, commit, pub.objects), CheckHeader, ActionDemand, ErrTooManyEpochs)
	})
}

// GL-9 (check 2): the previous root is not tight, or its objects do not
// rebuild it.
func TestVerifyPreviousMutations(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Root)
		want   error
	}{
		{"not tight", func(r *Root) { r.Total.AddUint64(&r.Total, 1) }, ErrPreviousNotTight},
		{"root not rebuilt", func(r *Root) { r.Root[5] ^= 1 }, ErrPreviousRebuild},
		{"leaf count not rebuilt", func(r *Root) { r.LeafCount++ }, ErrPreviousRebuild},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newScenario(t)
			in := s.input(s.p.Commit, s.p.Objects)
			previous := s.f.Commit
			c.mutate(&previous)
			in.Previous = &previous
			expectFailure(t, mustVerify(t, in), CheckPrevious, ActionRefuse, c.want)
		})
	}
	// A validator without custody fetches the previous root's objects; each
	// fault in them fails check 2.
	accountChunk := encodeAccountChunks(nil)
	_ = accountChunk
	objectCases := []struct {
		name   string
		mutate func(t *testing.T, previous *Root, objects Objects)
		want   []error
	}{
		{"header bytes", func(t *testing.T, previous *Root, objects Objects) {
			objects[previous.LedgerHash] = append(slices.Clone(objects[previous.LedgerHash]), ' ')
		}, []error{ErrPreviousRebuild, ErrLedgerHash}},
		{"header not canonical", func(t *testing.T, previous *Root, objects Objects) {
			header := bytes.Replace(objects[previous.LedgerHash], []byte(`"no_id":7`), []byte(`"no_id": 7`), 1)
			previous.LedgerHash = RefOf(header)
			objects[previous.LedgerHash] = header
		}, []error{ErrPreviousRebuild, ErrHeaderNotCanonical}},
		{"header over 256 KiB", func(t *testing.T, previous *Root, objects Objects) {
			header := headerOfSize(t, MaxHeaderBytes+1)
			previous.LedgerHash = RefOf(header)
			objects[previous.LedgerHash] = header
		}, []error{ErrPreviousRebuild, ErrHeaderTooLarge}},
		{"account chunk bytes", func(t *testing.T, previous *Root, objects Objects) {
			h, _ := DecodeHeader(objects[previous.LedgerHash])
			objects[h.Accounts[0]] = append(slices.Clone(objects[h.Accounts[0]]), 0)
		}, []error{ErrPreviousRebuild, ErrChunkHash}},
		{"account chunk length", func(t *testing.T, previous *Root, objects Objects) {
			h, _ := DecodeHeader(objects[previous.LedgerHash])
			chunk := append(slices.Clone(objects[h.Accounts[0]]), 0)
			h.Accounts[0] = RefOf(chunk)
			objects[h.Accounts[0]] = chunk
			header, _ := EncodeHeader(h)
			previous.LedgerHash = RefOf(header)
			objects[previous.LedgerHash] = header
		}, []error{ErrPreviousRebuild, ErrChunkLength}},
		{"account chunk over 131,072 rows", func(t *testing.T, previous *Root, objects Objects) {
			h, _ := DecodeHeader(objects[previous.LedgerHash])
			chunk := make([]byte, (ChunkRows+1)*AccountRowBytes)
			h.Accounts[0] = RefOf(chunk)
			objects[h.Accounts[0]] = chunk
			header, _ := EncodeHeader(h)
			previous.LedgerHash = RefOf(header)
			objects[previous.LedgerHash] = header
		}, []error{ErrPreviousRebuild, ErrChunkLength}},
	}
	for _, c := range objectCases {
		t.Run(c.name, func(t *testing.T) {
			s := newScenario(t)
			s.custody = NewMemoryCustody()
			objects := maps.Clone(s.p.Objects)
			maps.Copy(objects, s.f.Objects)
			previous := s.f.Commit
			c.mutate(t, &previous, objects)
			in := s.input(s.p.Commit, objects)
			in.Previous = &previous
			expectFailure(t, mustVerify(t, in), CheckPrevious, ActionRefuse, c.want...)
		})
	}
}

// GL-9 (check 4): chunk mutations, of provider chunks and of account chunks.
func TestVerifyChunkMutations(t *testing.T) {
	chunk11 := EncodeProviderChunks(epoch11Rows())[0]
	cases := []struct {
		name   string
		mutate func(*publication)
		action Action
		want   error
	}{
		{"a hash", func(pub *publication) {
			pub.objects[pub.header.Epochs[0].Providers[0]] = EncodeProviderChunks(epoch11Rows()[1:])[0]
		}, ActionDemand, ErrChunkHash},
		{"a short middle chunk", func(pub *publication) {
			pub.setChunks(0, [][]byte{chunk11[:3*ProviderRowBytes], chunk11[3*ProviderRowBytes:]})
		}, ActionRefuse, ErrChunkShort},
		{"an empty last chunk", func(pub *publication) { pub.setChunks(0, [][]byte{{}}) }, ActionRefuse, ErrChunkEmpty},
		{"a bad row length", func(pub *publication) {
			pub.setChunks(0, [][]byte{append(slices.Clone(chunk11), 0)})
		}, ActionRefuse, ErrChunkLength},
		{"more than 131,072 rows", func(pub *publication) {
			pub.setChunks(0, [][]byte{make([]byte, (ChunkRows+1)*ProviderRowBytes)})
		}, ActionRefuse, ErrChunkLength},
		{"a head_excluded byte of 2", func(pub *publication) {
			chunk := slices.Clone(chunk11)
			chunk[ProviderRowBytes-1] = 2
			pub.setChunks(0, [][]byte{chunk})
		}, ActionRefuse, ErrRowEncoding},
		{"an account chunk hash", func(pub *publication) {
			pub.objects[pub.header.Accounts[0]] = append(slices.Clone(pub.objects[pub.header.Accounts[0]]), 0)
		}, ActionDemand, ErrChunkHash},
		{"an account chunk length", func(pub *publication) {
			pub.header.Accounts = pub.put([][]byte{append(slices.Clone(pub.objects[pub.header.Accounts[0]]), 0)})
		}, ActionDemand, ErrChunkLength},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newScenario(t)
			pub := publish(t, s.p)
			c.mutate(pub)
			expectFailure(t, s.verify(t, pub.seal(t), pub.objects), CheckChunks, c.action, c.want)
		})
	}
}

// GL-9 (check 4): an epoch of 131,073 rows is a full chunk and a chunk of
// one row; a short chunk before the last, or an empty chunk after a full
// one, is refused.
func TestVerifyChunkBoundaries(t *testing.T) {
	s := newScenario(t)
	rows := make([]protocol.LedgerUsageRow, ChunkRows+1)
	for i := range rows {
		rows[i] = row(100+i, i%50+1, uint64(1+i%7), false)
	}
	p := s.build(t, rows, epoch12Rows())
	if len(p.Epochs[0].Providers) != 2 {
		t.Fatalf("%d chunks", len(p.Epochs[0].Providers))
	}
	base := cloneCustody(s.custody)
	expectPass(t, s.verify(t, p.Commit, p.Objects))
	chunks := EncodeProviderChunks(rows)
	full := chunks[0]
	for _, c := range []struct {
		name   string
		chunks [][]byte
		want   error
	}{
		{"a short middle chunk", [][]byte{full[:(ChunkRows-1)*ProviderRowBytes], append(slices.Clone(full[(ChunkRows-1)*ProviderRowBytes:]), chunks[1]...)}, ErrChunkShort},
		{"an empty last chunk", [][]byte{full, {}}, ErrChunkEmpty},
	} {
		t.Run(c.name, func(t *testing.T) {
			s.custody = cloneCustody(base)
			pub := publish(t, p)
			pub.setChunks(0, c.chunks)
			expectFailure(t, s.verify(t, pub.seal(t), pub.objects), CheckChunks, ActionRefuse, c.want)
		})
	}
}

// GL-9 (check 5): provider row mutations.
func TestVerifyRowMutations(t *testing.T) {
	cases := []struct {
		name   string
		mutate func([]protocol.LedgerUsageRow)
		want   error
	}{
		{"unsorted", func(r []protocol.LedgerUsageRow) { r[0], r[1] = r[1], r[0] }, ErrRowsUnsorted},
		{"duplicate", func(r []protocol.LedgerUsageRow) { r[1].ClientID = r[0].ClientID }, ErrRowDuplicate},
		{"zero id", func(r []protocol.LedgerUsageRow) { r[0].ClientID = [16]byte{} }, ErrRowZeroClientID},
		{"zero usage", func(r []protocol.LedgerUsageRow) { r[2].UsageBytes = 0 }, ErrRowZeroUsage},
		{"total usage 2^64", func(r []protocol.LedgerUsageRow) { r[5].UsageBytes = ^uint64(0) - 999 }, ErrUsageOverflow},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newScenario(t)
			pub := publish(t, s.p)
			rows := epoch11Rows()
			c.mutate(rows)
			pub.setRows(0, rows)
			expectFailure(t, s.verify(t, pub.seal(t), pub.objects), CheckRows, ActionRefuse, c.want)
		})
	}
}

// GL-9 (check 6): head flags set without an active binding, or cleared with one.
func TestVerifyHeadFlagMutations(t *testing.T) {
	cases := []struct {
		name string
		row  int
		want error
	}{
		{"set without a candidate", 0, ErrHeadFlagSet},
		{"set for a candidate without a binding", 2, ErrHeadFlagSet},
		{"cleared with a binding", 4, ErrHeadFlagCleared},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newScenario(t)
			pub := publish(t, s.p)
			rows := epoch11Rows()
			rows[c.row].HeadExcluded = !rows[c.row].HeadExcluded
			pub.setRows(0, rows)
			expectFailure(t, s.verify(t, pub.seal(t), pub.objects), CheckHeadExclusion, ActionRefuse, c.want)
		})
	}
	// Without an oracle check 6 is skipped.
	s := newScenario(t)
	pub := publish(t, s.p)
	rows := epoch11Rows()
	rows[0].HeadExcluded = true
	pub.setRows(0, rows)
	in := s.input(pub.seal(t), pub.objects)
	in.Heads = nil
	res := mustVerify(t, in)
	expectFailure(t, res, CheckAllocation, ActionRefuse, ErrAccountDelta)
}

// accountsOf returns a copy of the pending root's accounts for mutation.
func accountsOf(s *scenario) []merkle.LedgerAccount { return slices.Clone(s.p.Accounts) }

// GL-9 (checks 7 and 8): account mutations, each committed in a root built
// from the mutated accounts.
func TestVerifyAccountMutations(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(s *scenario) []merkle.LedgerAccount
		check  Check
		action Action
		want   error
		faults []Fault
	}{
		{"allocation: a delta", func(s *scenario) []merkle.LedgerAccount {
			a := accountsOf(s)
			a[0].Cumulative.SubUint64(&a[0].Cumulative, 1)
			a[1].Cumulative.AddUint64(&a[1].Cumulative, 1)
			return a
		}, CheckAllocation, ActionRefuse, ErrAccountDelta, nil},
		{"allocation: an omitted new account", func(s *scenario) []merkle.LedgerAccount {
			a := accountsOf(s)
			omitted := accountIndex(t, a, 5)
			a[omitted+1].Cumulative.Add(&a[omitted+1].Cumulative, &a[omitted].Cumulative)
			return slices.Delete(a, omitted, omitted+1)
		}, CheckAllocation, ActionRefuse, ErrAccountOmitted, nil},
		{"allocation: an account without a delta", func(s *scenario) []merkle.LedgerAccount {
			a := accountsOf(s)
			a[5].Cumulative.SubUint64(&a[5].Cumulative, 5)
			return slices.Insert(a, 5, merkle.LedgerAccount{Coldkey: key(6), Cumulative: rao(5)})
		}, CheckAllocation, ActionRefuse, ErrAccountDelta, nil},
		{"accounts: a moved coldkey", func(s *scenario) []merkle.LedgerAccount {
			a := accountsOf(s)
			a[1].Coldkey = key(99)
			return a
		}, CheckAccounts, ActionChallenge, ErrAccountMoved, []Fault{{1, merkle.LedgerFaultWrongLeaf}}},
		{"accounts: swapped positions", func(s *scenario) []merkle.LedgerAccount {
			a := accountsOf(s)
			a[0], a[1] = a[1], a[0]
			return a
		}, CheckAccounts, ActionChallenge, ErrAccountMoved, []Fault{{0, merkle.LedgerFaultWrongLeaf}, {1, merkle.LedgerFaultWrongLeaf}}},
		{"accounts: a lowered cumulative", func(s *scenario) []merkle.LedgerAccount {
			a := accountsOf(s)
			var moved uint256.Int
			moved.Sub(&a[0].Cumulative, &s.f.Accounts[0].Cumulative)
			moved.AddUint64(&moved, 1)
			a[0].Cumulative.Sub(&a[0].Cumulative, &moved)
			a[1].Cumulative.Add(&a[1].Cumulative, &moved)
			return a
		}, CheckAccounts, ActionChallenge, ErrAccountLowered, []Fault{{0, merkle.LedgerFaultLowerValue}}},
		{"accounts: an unsorted new account", func(s *scenario) []merkle.LedgerAccount {
			a := accountsOf(s)
			a[3], a[4] = a[4], a[3]
			return a
		}, CheckAccounts, ActionRefuse, ErrAccountUnsorted, nil},
		{"accounts: a new account repeating a previous coldkey", func(s *scenario) []merkle.LedgerAccount {
			a := accountsOf(s)
			a[0].Cumulative.SubUint64(&a[0].Cumulative, 1)
			return slices.Insert(a, 3, merkle.LedgerAccount{Coldkey: key(1), Cumulative: rao(1)})
		}, CheckAccounts, ActionRefuse, ErrAccountDuplicate, nil},
		{"accounts: a new account repeating a new coldkey", func(s *scenario) []merkle.LedgerAccount {
			a := accountsOf(s)
			a[4].Cumulative.SubUint64(&a[4].Cumulative, 1)
			return slices.Insert(a, 5, merkle.LedgerAccount{Coldkey: key(5), Cumulative: rao(1)})
		}, CheckAccounts, ActionRefuse, ErrAccountDuplicate, nil},
		{"accounts: a new account with a zero delta", func(s *scenario) []merkle.LedgerAccount {
			return slices.Insert(accountsOf(s), 5, merkle.LedgerAccount{Coldkey: key(6)})
		}, CheckAccounts, ActionRefuse, ErrAccountZeroDelta, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newScenario(t)
			pub := publish(t, s.p)
			pub.setAccounts(t, c.mutate(s))
			res := s.verify(t, pub.seal(t), pub.objects)
			expectFailure(t, res, c.check, c.action, c.want)
			if !res.Reproduced || !slices.Equal(res.Faults, c.faults) {
				t.Fatalf("reproduced %v, faults %v; want %v", res.Reproduced, res.Faults, c.faults)
			}
			expectProvable(t, s, res)
		})
	}
}

// expectProvable requires that each fault is what checkPath finds: the old
// account proves against the previous root, and the pending root's path at
// its index shows the fault at the leaf.
func expectProvable(t *testing.T, s *scenario, res *Result) {
	t.Helper()
	previousRoot, previousTotal := res.Previous.Root()
	root, total := res.Tree.Root()
	for _, fault := range res.Faults {
		account := res.PreviousAccounts[fault.Index]
		proof, err := res.Previous.Proof(fault.Index)
		if err != nil || !merkle.VerifyLedgerProof(previousRoot, &previousTotal, res.Previous.LeafCount(), fault.Index, account.Coldkey, &account.Cumulative, proof) {
			t.Fatalf("account %d does not prove against the previous root: %v", fault.Index, err)
		}
		path, err := res.Tree.Path(fault.Index)
		if err != nil {
			t.Fatal(err)
		}
		kind, level, err := merkle.WalkLedgerPath(root, &total, res.Tree.LeafCount(), fault.Index, account.Coldkey, &account.Cumulative, path)
		if err != nil || kind != fault.Kind || level != 0 {
			t.Fatalf("checkPath at %d finds %v at level %d (%v), want %v", fault.Index, kind, level, err, fault.Kind)
		}
	}
}

// GL-9 (check 8): published rows that omit a previous account, under a
// commitment they do not reproduce.
func TestVerifyWithheldAccount(t *testing.T) {
	s := newScenario(t)
	pub := publish(t, s.p)
	chunks, err := EncodeAccountChunks(s.p.Accounts[:2])
	if err != nil {
		t.Fatal(err)
	}
	pub.header.Accounts = pub.put(chunks)
	res := s.verify(t, pub.seal(t), pub.objects)
	expectFailure(t, res, CheckAccounts, ActionDemand, ErrAccountMoved)
	if res.Reproduced || !slices.Equal(res.Faults, []Fault{{2, merkle.LedgerFaultWrongLeaf}}) {
		t.Fatalf("reproduced %v, faults %v", res.Reproduced, res.Faults)
	}
}

// GL-9 (check 7): the root's epoch does not allocate.
func TestVerifyEpochNotAllocated(t *testing.T) {
	s := newScenario(t)
	pub := publish(t, s.p)
	pub.setRows(1, []protocol.LedgerUsageRow{row(3, 0, 500, false), row(11, 0, 500, false)})
	expectFailure(t, s.verify(t, pub.seal(t), pub.objects), CheckAllocation, ActionRefuse, ErrEpochNotAllocated)
}

// GL-9 (check 9): the committed root, total and leaf count.
func TestVerifyTreeMutations(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Root)
		want   error
	}{
		{"root", func(r *Root) { r.Root[0] ^= 1 }, ErrTreeRoot},
		{"total", func(r *Root) { r.Total.AddUint64(&r.Total, 1) }, ErrTreeTotal},
		{"leaf count", func(r *Root) { r.LeafCount++ }, ErrTreeLeafCount},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newScenario(t)
			commit := s.p.Commit
			c.mutate(&commit)
			res := s.verify(t, commit, s.p.Objects)
			expectFailure(t, res, CheckTree, ActionDemand, c.want)
			if res.Reproduced || len(res.Faults) != 0 {
				t.Fatalf("reproduced %v, faults %v", res.Reproduced, res.Faults)
			}
		})
	}
	// Rows that build no tree: a first root without accounts.
	f, err := Build(context.Background(), BuildInput{NoID: testNoID, Epoch: 10, FirstEpoch: testFirst, Epochs: []EpochInput{{Epoch: 10, Rows: epoch10Rows()}}, FundedThrough: funded(10, 5)})
	if err != nil {
		t.Fatal(err)
	}
	pub := publish(t, f)
	pub.header.Accounts = []Ref{}
	commit := pub.seal(t)
	in := VerifyInput{NoID: testNoID, FirstEpoch: testFirst, Pending: commit, FundedThrough: funded(10, 0), Fetcher: pub.objects, Custody: NewMemoryCustody(), Heads: scenarioHeads(), MaxFetchBytes: DefaultMaxFetchBytes}
	expectFailure(t, mustVerify(t, in), CheckTree, ActionRefuse, ErrTreeLeafCount)
}

// GL-9 (byte budget): the audit fetches at most MaxFetchBytes of objects
// new to custody; objects it holds cost nothing.
func TestVerifyByteBudget(t *testing.T) {
	s := newScenario(t)
	base := cloneCustody(s.custody)
	need := objectBytes(s.p.Objects)
	run := func(limit uint64) *Result {
		s.custody = cloneCustody(base)
		in := s.input(s.p.Commit, s.p.Objects)
		in.MaxFetchBytes = limit
		return mustVerify(t, in)
	}
	expectPass(t, run(need))
	res := run(need - 1)
	expectFailure(t, res, CheckFetch, ActionRefuse, ErrByteBudget)
	if res.FetchedBytes != need {
		t.Fatalf("fetched %d bytes", res.FetchedBytes)
	}
	expectFailure(t, run(0), CheckFetch, ActionDemand, ErrByteBudget)
	// What an earlier audit kept is not fetched again.
	run(need - 1)
	in := s.input(s.p.Commit, s.p.Objects)
	in.MaxFetchBytes = uint64(len(s.p.Objects[s.p.Epochs[1].Providers[0]]))
	expectPass(t, mustVerify(t, in))
}

// failingReader fails after its data.
type failingReader struct{ io.Reader }

func (r failingReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if err == io.EOF {
		return n, errors.New("connection reset")
	}
	return n, err
}

func (failingReader) Close() error { return nil }

// fetchFunc adapts a function to Fetcher.
type fetchFunc func(context.Context, Ref) (io.ReadCloser, error)

func (f fetchFunc) Fetch(ctx context.Context, ref Ref) (io.ReadCloser, error) { return f(ctx, ref) }

// GL-9 (fetch): an object that cannot be fetched stops the audit and is
// listed as missing.
func TestVerifyUnavailable(t *testing.T) {
	t.Run("provider chunk", func(t *testing.T) {
		s := newScenario(t)
		objects := maps.Clone(s.p.Objects)
		missing := s.p.Epochs[1].Providers[0]
		delete(objects, missing)
		res := s.verify(t, s.p.Commit, objects)
		expectFailure(t, res, CheckFetch, ActionDemand, ErrObjectUnavailable)
		if !slices.Equal(res.Missing, []Ref{missing}) || !errors.Is(res.Err, ErrObjectNotFound) {
			t.Fatalf("missing %v: %v", res.Missing, res.Err)
		}
	})
	t.Run("header", func(t *testing.T) {
		s := newScenario(t)
		res := s.verify(t, s.p.Commit, Objects{})
		expectFailure(t, res, CheckFetch, ActionDemand, ErrObjectUnavailable)
		if !slices.Equal(res.Missing, []Ref{s.p.Commit.LedgerHash}) {
			t.Fatalf("missing %v", res.Missing)
		}
	})
	t.Run("previous header", func(t *testing.T) {
		s := newScenario(t)
		s.custody = NewMemoryCustody()
		expectFailure(t, s.verify(t, s.p.Commit, s.p.Objects), CheckFetch, ActionRefuse, ErrObjectUnavailable)
	})
	t.Run("read error", func(t *testing.T) {
		s := newScenario(t)
		in := s.input(s.p.Commit, s.p.Objects)
		in.Fetcher = fetchFunc(func(ctx context.Context, ref Ref) (io.ReadCloser, error) {
			return failingReader{bytes.NewReader(s.p.Objects[ref])}, nil
		})
		expectFailure(t, mustVerify(t, in), CheckFetch, ActionDemand, ErrObjectUnavailable)
	})
}

var errInjected = errors.New("injected")

// failingCustody fails its nth call.
type failingCustody struct {
	*MemoryCustody
	n, calls int
}

func (c *failingCustody) fail() error {
	c.calls++
	if c.calls == c.n {
		return errInjected
	}
	return nil
}

func (c *failingCustody) Object(ctx context.Context, ref Ref) ([]byte, bool, error) {
	if err := c.fail(); err != nil {
		return nil, false, err
	}
	return c.MemoryCustody.Object(ctx, ref)
}

func (c *failingCustody) Keep(ctx context.Context, ref Ref, data []byte) error {
	if err := c.fail(); err != nil {
		return err
	}
	return c.MemoryCustody.Keep(ctx, ref, data)
}

func (c *failingCustody) ValidRows(ctx context.Context, noID, epoch uint64) ([]Ref, bool, error) {
	if err := c.fail(); err != nil {
		return nil, false, err
	}
	return c.MemoryCustody.ValidRows(ctx, noID, epoch)
}

func (c *failingCustody) KeepValidRows(ctx context.Context, noID, epoch uint64, chunks []Ref) error {
	if err := c.fail(); err != nil {
		return err
	}
	return c.MemoryCustody.KeepValidRows(ctx, noID, epoch, chunks)
}

func (c *failingCustody) KeepNull(ctx context.Context, noID, epoch, seq uint64) error {
	if err := c.fail(); err != nil {
		return err
	}
	return c.MemoryCustody.KeepNull(ctx, noID, epoch, seq)
}

// failingHeads fails every call.
type failingHeads struct{}

func (failingHeads) HeadBindings(ctx context.Context, epoch uint64) (map[[16]byte]bool, error) {
	return nil, errInjected
}

// GL-9: Verify returns an error, not a result, for its inputs, its context,
// and its custody's and head oracle's errors.
func TestVerifyErrors(t *testing.T) {
	s := newScenario(t)
	// A root with an unrecoverable epoch reaches every custody method.
	declared, err := buildOver(s.f, []EpochInput{{Epoch: 11, Null: true}, {Epoch: 12, Rows: epoch12Rows()}}, s.funded)
	if err != nil {
		t.Fatal(err)
	}
	base := cloneCustody(s.custody)
	t.Run("input", func(t *testing.T) {
		in := s.input(declared.Commit, declared.Objects)
		in.Fetcher = nil
		if _, err := Verify(context.Background(), in); !errors.Is(err, ErrInput) {
			t.Fatalf("no fetcher: %v", err)
		}
		in = s.input(declared.Commit, declared.Objects)
		in.Custody = nil
		if _, err := Verify(context.Background(), in); !errors.Is(err, ErrInput) {
			t.Fatalf("no custody: %v", err)
		}
		//lint:ignore SA1012 a nil context is the refusal under test
		if _, err := Verify(nil, s.input(declared.Commit, declared.Objects)); !errors.Is(err, ErrInput) {
			t.Fatalf("nil context: %v", err)
		}
	})
	t.Run("funding", func(t *testing.T) {
		for _, mutate := range []func(map[uint64]uint256.Int){
			func(f map[uint64]uint256.Int) { delete(f, 12) },
			func(f map[uint64]uint256.Int) { f[11] = rao(1) },
			func(f map[uint64]uint256.Int) { f[12] = *new(uint256.Int).Set(valueLimit) },
		} {
			s.custody = cloneCustody(base)
			in := s.input(declared.Commit, declared.Objects)
			in.FundedThrough = scenarioFunding()
			mutate(in.FundedThrough)
			if _, err := Verify(context.Background(), in); !errors.Is(err, ErrFunding) {
				t.Fatalf("%v, want ErrFunding", err)
			}
		}
	})
	t.Run("custody", func(t *testing.T) {
		methods := 0
		for n := 1; ; n++ {
			custody := &failingCustody{MemoryCustody: cloneCustody(base), n: n}
			in := s.input(declared.Commit, declared.Objects)
			in.Custody = custody
			res, err := Verify(context.Background(), in)
			if custody.calls < n {
				expectPass(t, res)
				methods = n - 1
				break
			}
			if !errors.Is(err, errInjected) || res != nil {
				t.Fatalf("custody call %d failing: %v %v", n, res, err)
			}
		}
		if methods < 8 {
			t.Fatalf("only %d custody calls", methods)
		}
	})
	t.Run("head oracle", func(t *testing.T) {
		s.custody = cloneCustody(base)
		in := s.input(declared.Commit, declared.Objects)
		in.Heads = failingHeads{}
		if _, err := Verify(context.Background(), in); !errors.Is(err, errInjected) {
			t.Fatalf("%v, want the oracle's error", err)
		}
	})
	t.Run("canceled", func(t *testing.T) {
		for n := 0; ; n++ {
			s.custody = cloneCustody(base)
			res, err := Verify(&cancelAfter{Context: context.Background(), n: n}, s.input(declared.Commit, declared.Objects))
			if err == nil {
				expectPass(t, res)
				if n < 5 {
					t.Fatalf("only %d context checks", n)
				}
				break
			}
			if !errors.Is(err, context.Canceled) || res != nil {
				t.Fatalf("canceled after %d checks: %v %v", n, res, err)
			}
		}
		// A fetch that fails because the context ended reports the context.
		in := VerifyInput{NoID: testNoID, FirstEpoch: testFirst, Pending: s.f.Commit, FundedThrough: s.funded, Custody: NewMemoryCustody(), MaxFetchBytes: DefaultMaxFetchBytes}
		in.Fetcher = fetchFunc(func(ctx context.Context, ref Ref) (io.ReadCloser, error) { return nil, ctx.Err() })
		if _, err := Verify(&cancelAfter{Context: context.Background(), n: 1}, in); !errors.Is(err, context.Canceled) {
			t.Fatalf("%v, want context.Canceled", err)
		}
	})
}

// GL-9: check and action names.
func TestCheckAndActionNames(t *testing.T) {
	if err := fail(CheckTree, ErrTreeRoot); err.Error() != ErrTreeRoot.Error() {
		t.Fatalf("failure reads %q", err.Error())
	}
	names := map[Check]string{
		CheckNone: "none", CheckHeader: "header", CheckPrevious: "previous root", CheckFixedInputs: "fixed inputs",
		CheckChunks: "chunks", CheckRows: "provider rows", CheckHeadExclusion: "head exclusion", CheckAllocation: "allocation",
		CheckAccounts: "accounts", CheckTree: "tree", CheckFetch: "fetch", Check(99): "check(99)",
	}
	for check, name := range names {
		if check.String() != name {
			t.Fatalf("%d is %q", int(check), check.String())
		}
	}
	for action, name := range map[Action]string{ActionRefuse: "refuse", ActionApprove: "approve", ActionChallenge: "challenge", ActionDemand: "demand"} {
		if action.String() != name {
			t.Fatalf("%d is %q", int(action), action.String())
		}
	}
}

// H1, VA-4: Faults compares published rows with any finalized root's
// accounts, by position: an honest successor shows none against an older
// root; a row below the older value, another coldkey or a missing row is a
// fault, and a row lowered only below a newer value is no fault against the
// older root.
func TestFaultsAgainstAnyRoot(t *testing.T) {
	s := newScenario(t)
	older, newer := s.f.Accounts, s.p.Accounts
	if faults := Faults(older, newer); faults != nil {
		t.Fatalf("honest successor faults %v", faults)
	}
	rows := slices.Clone(newer)
	rows[0].Cumulative.SubUint64(&older[0].Cumulative, 1)
	rows[1].Coldkey = key(99)
	rows[2].Cumulative = older[2].Cumulative
	if want := []Fault{{0, merkle.LedgerFaultLowerValue}, {1, merkle.LedgerFaultWrongLeaf}}; !slices.Equal(Faults(older, rows), want) {
		t.Fatalf("faults %v, want %v", Faults(older, rows), want)
	}
	if want := []Fault{{2, merkle.LedgerFaultWrongLeaf}}; len(older) != 3 || !slices.Equal(Faults(older, newer[:2]), want) {
		t.Fatalf("missing rows fault %v, want %v", Faults(older, newer[:2]), want)
	}
}
