package ledger

// Check 11 and the extender parts of checks 3, 4 and 6: the validator
// requires an extender component exactly for the epochs from E_x, recomputes
// the share rows from the census, and refuses whatever it can check that
// does not hold. What it cannot check, the census's membership and wallets
// and Z itself, is the operator's attestation (WHITEPAPER.md §8.6.4).

import (
	"context"
	"errors"
	"maps"
	"slices"
	"testing"

	"github.com/urfoundation/sn/protocol"
)

// The extender scenario's census of epoch 12: x21 and x22 share unit 1, x23
// is alone in unit 2, and x24 is head-bound.
func census12() []CensusRow {
	return []CensusRow{
		active(1, 21, 9, 1),
		active(2, 22, 10, 1),
		active(3, 23, 11, 2),
		censusRow(4, 24, 12, 3, CensusActive|CensusHeadBound),
	}
}

// Epoch 12's eligible provider usage is 1,017 bytes, so Z = 600 enters
// whole: units 1 and 2 take 300 each, x21 and x22 150 each.
const z12 = 600

// x24's client is bound in epochs 11 and 12.
func extenderHeads() heads {
	h := scenarioHeads()
	h[11][id(24)] = true
	h[12] = map[[16]byte]bool{id(24): true}
	return h
}

// Builds a root for epoch 12 over F with the given extender pools for
// epochs 11 and 12 (nil for none).
func (s *scenario) buildExtenders(t *testing.T, e11, e12 *ExtenderInput) *Ledger {
	t.Helper()
	l, err := Build(context.Background(), BuildInput{
		NoID: testNoID, Epoch: 12, FirstEpoch: testFirst,
		Previous:      &Previous{Root: s.f.Commit, Accounts: s.f.Accounts},
		Epochs:        []EpochInput{{Epoch: 11, Rows: epoch11Rows(), Extenders: e11}, {Epoch: 12, Rows: epoch12Rows(), Extenders: e12}},
		FundedThrough: s.funded,
	})
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// The scenario's audit input for pending, with the extender heads and E_x
// from.
func (s *scenario) extendersFrom(pending Root, objects Objects, from uint64) VerifyInput {
	in := s.input(pending, objects)
	in.Heads, in.ExtendersFrom = extenderHeads(), from
	return in
}

// Publishes covered epoch i's census.
func (pub *publication) setCensus(i int, census []CensusRow) {
	pub.header.Epochs[i].Extenders.Census = pub.put(EncodeCensusChunks(census))
}

// Publishes covered epoch i's share rows.
func (pub *publication) setShares(i int, rows []protocol.LedgerUsageRow) {
	pub.header.Epochs[i].Extenders.Rows = pub.put(EncodeProviderChunks(rows))
}

// The boundary: a component is required for every covered epoch from E_x
// that is not unrecoverable, and refused for every other. A root that covers
// both sides applies each epoch's own rule, and its epoch before E_x keeps
// the provider chunks a root without the amendment gives it. A root wholly
// before E_x is a v2 header, byte for byte, and passes with E_x set.
func TestVerifyExtendersBoundary(t *testing.T) {
	s := newScenario(t)
	l := s.buildExtenders(t, nil, &ExtenderInput{Bytes: z12, Census: census12()})
	if got := l.Epochs[1].Extenders.Rows; len(got) != 3 || got[0] != shareRow(21, 9, 150) || got[1] != shareRow(22, 10, 150) || got[2] != shareRow(23, 11, 300) {
		t.Fatalf("epoch 12 shares %+v", got)
	}
	if l.Epochs[0].Extenders != nil || !slices.Equal(l.Epochs[0].Providers, s.p.Epochs[0].Providers) {
		t.Fatal("epoch 11, before E_x, differs from its build without the amendment")
	}
	expectPass(t, mustVerify(t, s.extendersFrom(l.Commit, l.Objects, 12)))
	for _, c := range []struct {
		from uint64
		want error
	}{
		{from: 0, want: ErrExtendersUnexpected},
		{from: 13, want: ErrExtendersUnexpected},
		{from: 11, want: ErrExtendersMissing},
		{from: 1, want: ErrExtendersMissing},
	} {
		s.custody = cloneCustodyAfterF(t, s)
		expectFailure(t, mustVerify(t, s.extendersFrom(l.Commit, l.Objects, c.from)), CheckExtenders, ActionRefuse, c.want)
	}

	// Without the amendment, and with it from a later epoch, the same root
	// without components passes, and its header is v2.
	for _, from := range []uint64{0, 13, 1000} {
		s.custody = cloneCustodyAfterF(t, s)
		expectPass(t, mustVerify(t, s.extendersFrom(s.p.Commit, s.p.Objects, from)))
	}
	if string(s.p.Header[:len(`{"schema":"`+Schema+`"`)]) != `{"schema":"`+Schema+`"` {
		t.Fatalf("a root before E_x is not %s", Schema)
	}

	// From epoch 11: both epochs carry a component.
	s.custody = cloneCustodyAfterF(t, s)
	both := s.buildExtenders(t, &ExtenderInput{Bytes: 5, Census: census12()}, &ExtenderInput{Bytes: z12, Census: census12()})
	expectPass(t, mustVerify(t, s.extendersFrom(both.Commit, both.Objects, 11)))

	// A watcher without the amendments file takes the components as
	// declared, and still checks their arithmetic.
	for _, root := range []*Ledger{l, s.p, both} {
		s.custody = cloneCustodyAfterF(t, s)
		in := s.extendersFrom(root.Commit, root.Objects, 0)
		in.ExtendersUnchecked = true
		expectPass(t, mustVerify(t, in))
	}
	s.custody = cloneCustodyAfterF(t, s)
	pub := publish(t, l)
	pub.header.Epochs[1].Extenders.Bytes = z12 + 1
	in := s.extendersFrom(pub.seal(t), pub.objects, 0)
	in.ExtendersUnchecked = true
	expectFailure(t, mustVerify(t, in), CheckExtenders, ActionRefuse, ErrExtenderRows)

	// An unrecoverable epoch at or after E_x carries none.
	s.custody = cloneCustodyAfterF(t, s)
	declared, err := buildOver(s.f, []EpochInput{{Epoch: 11, Null: true}, {Epoch: 12, Rows: epoch12Rows(), Extenders: &ExtenderInput{Bytes: z12, Census: census12()}}}, s.funded)
	if err != nil {
		t.Fatal(err)
	}
	expectPass(t, mustVerify(t, s.extendersFrom(declared.Commit, declared.Objects, 11)))
	if _, err := buildOver(s.f, []EpochInput{{Epoch: 11, Null: true, Extenders: &ExtenderInput{}}, {Epoch: 12, Rows: epoch12Rows()}}, s.funded); !errors.Is(err, ErrEpochInput) {
		t.Fatalf("built an extender pool on an unrecoverable epoch: %v", err)
	}
}

// The custody as it was after F's audit: its valid rows of epoch 10
// and its objects, nothing of a later audit.
func cloneCustodyAfterF(t *testing.T, s *scenario) *MemoryCustody {
	t.Helper()
	custody := NewMemoryCustody()
	maps.Copy(custody.Objects, s.f.Objects)
	custody.Rows[PoolEpoch{testNoID, 10}] = s.f.Epochs[0].Providers
	return custody
}

// What the validator recomputes and so refuses: share rows that are not the
// split, census rules, census head flags, census and share objects that are
// not published as listed, and a component that changes once fixed. What it
// cannot check passes: Z above the cap, which enters no weight, and a census
// the operator forged consistently, with its share rows and accounts rebuilt
// from it.
func TestVerifyExtenderMutations(t *testing.T) {
	s := newScenario(t)
	honest := s.buildExtenders(t, nil, &ExtenderInput{Bytes: z12, Census: census12()})
	run := func(name string, mutate func(pub *publication)) *Result {
		t.Helper()
		s.custody = cloneCustodyAfterF(t, s)
		pub := publish(t, honest)
		mutate(pub)
		return mustVerify(t, s.extendersFrom(pub.seal(t), pub.objects, 12))
	}
	expectPass(t, run("honest", func(pub *publication) {}))

	shares := func() []protocol.LedgerUsageRow { return slices.Clone(honest.Epochs[1].Extenders.Rows) }
	expectFailure(t, run("a byte moved between extenders", func(pub *publication) {
		rows := shares()
		rows[0].UsageBytes++
		rows[2].UsageBytes--
		pub.setShares(1, rows)
	}), CheckExtenders, ActionRefuse, ErrExtenderRows)
	expectFailure(t, run("a share paid to another coldkey", func(pub *publication) {
		rows := shares()
		rows[1].Coldkey = key(1)
		pub.setShares(1, rows)
	}), CheckExtenders, ActionRefuse, ErrExtenderRows)
	expectFailure(t, run("a share row dropped", func(pub *publication) { pub.setShares(1, shares()[:2]) }), CheckExtenders, ActionRefuse, ErrExtenderRows)
	expectFailure(t, run("no share rows", func(pub *publication) { pub.header.Epochs[1].Extenders.Rows = []Ref{} }), CheckExtenders, ActionRefuse, ErrExtenderRows)
	expectFailure(t, run("Z raised below the cap", func(pub *publication) { pub.header.Epochs[1].Extenders.Bytes = z12 + 1 }), CheckExtenders, ActionRefuse, ErrExtenderRows)
	expectFailure(t, run("Z dropped", func(pub *publication) { pub.header.Epochs[1].Extenders.Bytes = 0 }), CheckExtenders, ActionRefuse, ErrExtenderRows)
	expectFailure(t, run("an eligible extender left out of the split", func(pub *publication) {
		census := census12()
		census[2].Flags |= CensusDeclared
		pub.setCensus(1, census)
	}), CheckExtenders, ActionRefuse, ErrExtenderRows)
	expectFailure(t, run("census unsorted", func(pub *publication) {
		census := census12()
		census[0], census[1] = census[1], census[0]
		pub.setCensus(1, census)
	}), CheckExtenders, ActionRefuse, ErrCensusUnsorted)
	expectFailure(t, run("a census row counted twice", func(pub *publication) {
		census := census12()
		census[1].ClientID, census[1].Coldkey = census[0].ClientID, census[0].Coldkey
		pub.setCensus(1, census)
	}), CheckExtenders, ActionRefuse, ErrCensusClient)
	expectFailure(t, run("an unknown census flag", func(pub *publication) {
		census := census12()
		census[0].Flags |= 0x80
		pub.setCensus(1, census)
	}), CheckChunks, ActionRefuse, ErrCensusEncoding)
	expectFailure(t, run("a census chunk cut short", func(pub *publication) {
		chunk := EncodeCensusChunks(census12())[0]
		pub.header.Epochs[1].Extenders.Census = pub.put([][]byte{chunk[:len(chunk)-1]})
	}), CheckChunks, ActionRefuse, ErrChunkLength)
	expectFailure(t, run("a head-bound flag cleared", func(pub *publication) {
		census := census12()
		census[3].Flags = CensusActive
		pub.setCensus(1, census)
	}), CheckHeadExclusion, ActionRefuse, ErrHeadFlagCleared)
	expectFailure(t, run("a head-bound flag set", func(pub *publication) {
		census := census12()
		census[0].Flags |= CensusHeadBound
		pub.setCensus(1, census)
	}), CheckHeadExclusion, ActionRefuse, ErrHeadFlagSet)
	censusRef := honest.Epochs[1].Extenders.Census[0]
	shareRef := honest.Epochs[1].Extenders.Chunks[0]
	expectFailure(t, run("census withheld", func(pub *publication) { delete(pub.objects, censusRef) }), CheckFetch, ActionDemand, ErrObjectUnavailable)
	expectFailure(t, run("share rows withheld", func(pub *publication) { delete(pub.objects, shareRef) }), CheckFetch, ActionDemand, ErrObjectUnavailable)
	expectFailure(t, run("census served altered", func(pub *publication) {
		census := census12()
		census[0].Coldkey = key(1)
		pub.objects[censusRef] = EncodeCensusChunks(census)[0]
	}), CheckChunks, ActionDemand, ErrChunkHash)

	// Z above the cap enters no weight: the validator cannot see it, and the
	// usage statement carries it to the deposit (M1).
	capped := s.buildExtenders(t, nil, &ExtenderInput{Bytes: 1_000_000, Census: census12()})
	s.custody = cloneCustodyAfterF(t, s)
	expectPass(t, mustVerify(t, s.extendersFrom(capped.Commit, capped.Objects, 12)))
	// A census forged consistently, an extender added with its share rows and
	// accounts rebuilt, is the operator's attestation and passes.
	forged := append(census12(), active(5, 25, 13, 4))
	consistent := s.buildExtenders(t, nil, &ExtenderInput{Bytes: z12, Census: forged})
	s.custody = cloneCustodyAfterF(t, s)
	expectPass(t, mustVerify(t, s.extendersFrom(consistent.Commit, consistent.Objects, 12)))
}

// Fixed inputs (§6.4, §8.6.4): an epoch's extender component is built once,
// at the first root that covers it, like its provider rows. Once the
// validator holds it as valid, a later root must list exactly it; an epoch
// whose rows were fixed without a component cannot gain one. The builder
// refuses to rebuild a census other than the one recorded.
func TestVerifyExtendersFixedInputs(t *testing.T) {
	s := newScenario(t)
	first := s.buildExtenders(t, nil, &ExtenderInput{Bytes: z12, Census: census12()})
	s.custody = cloneCustodyAfterF(t, s)
	expectPass(t, mustVerify(t, s.extendersFrom(first.Commit, first.Objects, 12)))
	held, ok, err := s.custody.ValidExtenders(context.Background(), testNoID, 12)
	if err != nil || !ok || !held.Equal(first.Epochs[1].Extenders.Header()) {
		t.Fatalf("custody holds %+v %v %v", held, ok, err)
	}
	// Auditing it again reads everything from custody.
	if res := mustVerify(t, s.extendersFrom(first.Commit, first.Objects, 12)); res.Err != nil || res.FetchedBytes != 0 {
		t.Fatalf("re-audit: %v, %d bytes fetched", res.Err, res.FetchedBytes)
	}
	audited := cloneCustody(s.custody)

	// A replacement root over F lists another component for epoch 12.
	for _, c := range []struct {
		name  string
		input *ExtenderInput
	}{
		{name: "other bytes", input: &ExtenderInput{Bytes: z12 - 1, Census: census12()}},
		{name: "other census", input: &ExtenderInput{Bytes: z12, Census: census12()[:3]}},
	} {
		t.Logf("a replacement root with %s", c.name)
		s.custody = cloneCustody(audited)
		other := s.buildExtenders(t, nil, c.input)
		expectFailure(t, mustVerify(t, s.extendersFrom(other.Commit, other.Objects, 12)), CheckFixedInputs, ActionRefuse, ErrFixedInputs)
	}
	// An epoch whose rows were fixed without a component cannot gain one.
	s.custody = cloneCustody(audited)
	gained := s.buildExtenders(t, &ExtenderInput{Bytes: 1, Census: census12()}, &ExtenderInput{Bytes: z12, Census: census12()})
	expectFailure(t, mustVerify(t, s.extendersFrom(gained.Commit, gained.Objects, 11)), CheckFixedInputs, ActionRefuse, ErrFixedInputs)

	// The builder holds a rebuilt epoch to its recorded census.
	census := first.Epochs[1].Extenders.Census
	if _, err := buildOver(s.f, []EpochInput{{Epoch: 11, Rows: epoch11Rows()}, {Epoch: 12, Rows: epoch12Rows(), Extenders: &ExtenderInput{Bytes: z12, Census: census12()[:3], Chunks: census}}}, s.funded); !errors.Is(err, ErrFixedInputs) {
		t.Fatalf("rebuilt another census: %v", err)
	}
	if _, err := buildOver(s.f, []EpochInput{{Epoch: 11, Rows: epoch11Rows()}, {Epoch: 12, Rows: epoch12Rows(), Extenders: &ExtenderInput{Bytes: z12, Census: census12(), Chunks: census}}}, s.funded); err != nil {
		t.Fatalf("rebuilt the recorded census: %v", err)
	}
}

// GL-9 for the extender component: Verify returns an error, not a result,
// for its custody's and head oracle's errors on every call an extender
// component adds.
func TestVerifyExtendersErrors(t *testing.T) {
	s := newScenario(t)
	l := s.buildExtenders(t, nil, &ExtenderInput{Bytes: z12, Census: census12()})
	base := cloneCustodyAfterF(t, s)
	methods := 0
	for n := 1; ; n++ {
		custody := &failingCustody{MemoryCustody: cloneCustody(base), n: n}
		in := s.extendersFrom(l.Commit, l.Objects, 12)
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
	if methods < 12 {
		t.Fatalf("only %d custody calls", methods)
	}
	// The census reads epoch 12's bindings second, after its provider rows.
	in := s.extendersFrom(l.Commit, l.Objects, 12)
	in.Custody = cloneCustody(base)
	oracle := &headsAfter{heads: extenderHeads(), epoch: 12, n: 2}
	in.Heads = oracle
	if _, err := Verify(context.Background(), in); !errors.Is(err, errInjected) || oracle.calls != 2 {
		t.Fatalf("%v after %d reads of epoch 12's bindings, want the oracle's error on the census's", err, oracle.calls)
	}
}

// Answers as heads does, except that its nth read of epoch's bindings
// fails.
type headsAfter struct {
	heads    heads
	epoch    uint64
	n, calls int
}

// The bindings of epoch, failing on its nth read of the failing epoch.
func (self *headsAfter) HeadBindings(ctx context.Context, epoch uint64) (map[[16]byte]bool, error) {
	if epoch == self.epoch {
		if self.calls++; self.calls == self.n {
			return nil, errInjected
		}
	}
	return self.heads[epoch], nil
}
