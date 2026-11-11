package ctf

import (
	"bytes"
	"context"
	"errors"
	"maps"
	"slices"
	"testing"

	"github.com/holiman/uint256"

	"github.com/urfoundation/sn/ledger"
)

// The scenario's roots: F draws epoch 8 and carries all of 9; P's epoch 10
// waits for the carry and 11 pays it; the totals are vault2's funding and the
// carry account holds what was not paid.
func TestBuildScenario(t *testing.T) {
	s := newScenario(t)
	f8, f9 := s.f.Epochs[0], s.f.Epochs[1]
	if !f8.Drawn || f8.Round != DrawRound(s.census[8].time) || !f8.Matured || f8.Fresh.Uint64() != scenarioFresh8 || f8.SlotRemainder != 7 {
		t.Fatalf("epoch 8: %+v", f8)
	}
	if f9.Drawn || f9.Census != nil || f9.Round != 0 || len(f9.Awards) != 0 || f9.AwardsRef != emptyRef {
		t.Fatalf("epoch 9 drew: %+v", f9)
	}
	// Epoch 8: DE has 7 provider units, 5 win; IS's provider unit and the
	// TM/IS unit (TM, its lowest client's) each win; DE's 2 extender units
	// and US's 5 win.
	wins := map[string]int{}
	for _, a := range f8.Awards {
		wins[a.Country.String()+"/"+a.Role.String()]++
		if !a.Pot.IsZero() || a.Deferred {
			t.Fatalf("a pot share in the first epoch: %+v", a)
		}
	}
	want := map[string]int{"de/provider": 5, "is/provider": 1, "tm/provider": 1, "de/extender": 2, "us/extender": 5}
	if !maps.Equal(wins, want) {
		t.Fatalf("wins %v", wins)
	}
	if units := f8.Pots[PotIndex(cc("de").Index(), RoleProvider)].Units; units != 7 {
		t.Fatalf("DE provider units %d", units)
	}
	if !s.f.Commit.Total.Eq(ptr(s.funded[9])) || !s.p.Commit.Total.Eq(ptr(s.funded[11])) {
		t.Fatal("a total is not fundedThrough")
	}

	p10, p11 := s.p.Epochs[0], s.p.Epochs[1]
	owed := s.f.State.Obligations()
	if p10.Matured || !p10.Owed.Eq(&owed) || !p10.Fresh.Eq(uint256.NewInt(scenarioFresh10)) || len(p10.Pending) == 0 {
		t.Fatalf("epoch 10: matured %t owed %s fresh %s pending %d", p10.Matured, p10.Owed.Dec(), p10.Fresh.Dec(), len(p10.Pending))
	}
	for _, a := range p10.Awards {
		if a.Deferred != !a.Pot.IsZero() {
			t.Fatalf("epoch 10 award %+v", a)
		}
	}
	if !p11.Matured || !p11.Returned.Eq(&owed) || !p11.Fresh.Eq(uint256.NewInt(scenarioFresh11)) || !p11.PaidPending.Eq(&p10.PendingTotal) || len(p11.Pending) != 0 {
		t.Fatalf("epoch 11: %+v", p11)
	}
	// Previous positions keep their coldkeys; the carry account grows.
	for i := range s.f.Accounts {
		if s.p.Accounts[i].Coldkey != s.f.Accounts[i].Coldkey || s.p.Accounts[i].Cumulative.Lt(&s.f.Accounts[i].Cumulative) {
			t.Fatalf("account %d moved", i)
		}
	}
	// Each account's delta, the carry's included.
	for i := range s.p.Accounts {
		increase := s.p.Accounts[i].Cumulative
		if i < len(s.f.Accounts) {
			increase.Sub(&increase, &s.f.Accounts[i].Cumulative)
		}
		if d := deltaOf(s.p.Deltas, s.p.Accounts[i].Coldkey); !d.Eq(&increase) {
			t.Fatalf("account %d increased %s, delta %s", i, increase.Dec(), d.Dec())
		}
	}
	// Clients 1 and 2 share a coldkey, which has one account.
	seen := map[[32]byte]bool{}
	for _, a := range s.p.Accounts {
		if seen[a.Coldkey] {
			t.Fatal("a coldkey has two accounts")
		}
		seen[a.Coldkey] = true
	}
	if !seen[carryKey] {
		t.Fatal("no carry account")
	}
	// The objects are what the header names.
	h, err := DecodeHeader(s.p.Header)
	if err != nil {
		t.Fatal(err)
	}
	for i, e := range s.p.Epochs {
		entry := h.Epochs[i]
		if entry.Awards != e.AwardsRef || entry.Pots != e.PotsRef || entry.Pending != e.PendingRef || *entry.Census != s.census[e.Epoch].objects.Hash || entry.Round != e.Round {
			t.Fatalf("header epoch %d", e.Epoch)
		}
		for _, ref := range []ledger.Ref{entry.Awards, entry.Pots, entry.Pending, *entry.Census} {
			if data, ok := s.p.Objects[ref]; !ok || ledger.RefOf(data) != ref {
				t.Fatalf("object %v", ref)
			}
		}
	}
}

func ptr(v uint256.Int) *uint256.Int { return &v }

// The result depends only on the input: building again gives the same bytes.
func TestBuildDeterminism(t *testing.T) {
	s := newScenario(t)
	for range 3 {
		again, err := Build(context.Background(), s.buildInput(11))
		if err != nil {
			t.Fatal(err)
		}
		if again.Commit != s.p.Commit || !bytes.Equal(again.Header, s.p.Header) || !maps.EqualFunc(again.Objects, s.p.Objects, bytes.Equal) {
			t.Fatal("the build is not deterministic")
		}
	}
}

// §5 fixed inputs: an epoch built before must encode to its first awards.
func TestBuildFixedAwards(t *testing.T) {
	s := newScenario(t)
	in := s.buildInput(11)
	first := s.p.Epochs[0].AwardsRef
	in.Epochs[0].Awards = &first
	if _, err := Build(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	// A winner's coldkey changed since: refused.
	winner := s.p.Epochs[0].Awards[0].ClientID
	in.Epochs[0].Coldkeys = maps.Clone(in.Epochs[0].Coldkeys)
	in.Epochs[0].Coldkeys[winner] = key(777)
	if _, err := Build(context.Background(), in); !errors.Is(err, ledger.ErrFixedInputs) {
		t.Fatalf("changed coldkey: %v", err)
	}
	// Without the fixed reference, the operator's rebuild takes the new one.
	in.Epochs[0].Awards = nil
	if _, err := Build(context.Background(), in); err != nil {
		t.Fatal(err)
	}
}

func TestBuildErrors(t *testing.T) {
	s := newScenario(t)
	cases := []struct {
		name   string
		mutate func(*BuildInput)
		want   error
	}{
		{"operator at 2^32", func(in *BuildInput) { in.NoID = PoolIDBase }, ErrInput},
		{"zero carry coldkey", func(in *BuildInput) { in.CarryColdkey = [32]byte{} }, ErrInput},
		{"first root from epoch 0", func(in *BuildInput) { in.Previous, in.FirstEpoch = nil, 0 }, ErrInput},
		{"an epoch missing", func(in *BuildInput) { in.Epochs = in.Epochs[1:] }, ledger.ErrCoveredEpochs},
		{"epochs shifted", func(in *BuildInput) { in.Epochs[0].Epoch = 9 }, ledger.ErrCoveredEpochs},
		{"epoch not after the previous root", func(in *BuildInput) { in.Epoch = 9 }, ledger.ErrCoveredEpochs},
		{"too many epochs", func(in *BuildInput) { in.Epoch = 9 + 97 }, ledger.ErrTooManyEpochs},
		{"previous accounts", func(in *BuildInput) {
			in.Previous = &Previous{Root: s.f.Commit, Accounts: slices.Clone(s.f.Accounts), State: s.f.State}
			in.Previous.Accounts[0].Cumulative.AddUint64(&in.Previous.Accounts[0].Cumulative, 1)
		}, ledger.ErrPrevious},
		{"previous state", func(in *BuildInput) {
			in.Previous = &Previous{Root: s.f.Commit, Accounts: s.f.Accounts, State: s.f.State}
			in.Previous.State.Pending = []PendingAward{{Epoch: 9, Role: RoleProvider, Country: cc("de"), Rank: 1, Coldkey: key(1)}}
		}, ledger.ErrPrevious},
		{"funding missing", func(in *BuildInput) { in.FundedThrough = map[uint64]uint256.Int{11: s.funded[11]} }, ledger.ErrFunding},
		{"funding decreasing", func(in *BuildInput) {
			in.FundedThrough = maps.Clone(s.funded)
			in.FundedThrough[10] = rao(0)
		}, ledger.ErrFunding},
		{"total not increased", func(in *BuildInput) {
			in.FundedThrough = maps.Clone(s.funded)
			in.FundedThrough[10], in.FundedThrough[11] = s.funded[9], s.funded[9]
		}, ledger.ErrTotalNotIncreased},
		{"no pulse", func(in *BuildInput) { in.Epochs[0].Pulse = nil }, ErrInput},
		{"another round's pulse", func(in *BuildInput) { in.Epochs[0].Pulse = testPulse(in.Epochs[0].Pulse.Round + 1) }, ErrInput},
		{"pulse not verified", func(in *BuildInput) {
			in.Epochs[0].Pulse = &Pulse{Round: in.Epochs[0].Pulse.Round, Signature: flip(in.Epochs[0].Pulse.Signature, 30, 1)}
		}, ErrPulse},
		{"a winner without a coldkey", func(in *BuildInput) { in.Epochs[1].Coldkeys = map[[16]byte][32]byte{} }, ErrColdkey},
		{"census header missing", func(in *BuildInput) { delete(in.Epochs[0].Census.Objects, in.Epochs[0].Census.Hash) }, ErrCensusUnavailable},
		{"census rows missing", func(in *BuildInput) {
			census := in.Epochs[0].Census
			census.Objects = ledger.Objects{census.Hash: census.Objects[census.Hash]}
		}, ErrCensusUnavailable},
		{"census bytes not the commitment", func(in *BuildInput) {
			census := in.Epochs[0].Census
			census.Hash = s.census[11].objects.Hash
		}, ErrCensusUnavailable},
		{"a return after the last epoch", func(in *BuildInput) {
			in.FundedThrough = maps.Clone(s.funded)
			in.FundedThrough[11] = s.funded[10]
			in.Epochs[1].Census = nil
		}, ErrReturnRolls},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := s.buildInput(11)
			c.mutate(&in)
			if _, err := Build(context.Background(), in); !errors.Is(err, c.want) {
				t.Fatalf("%v, want %v", err, c.want)
			}
		})
	}
	if _, err := Build(nil, s.buildInput(11)); !errors.Is(err, ErrInput) {
		t.Fatal(err)
	}
}

// §4.7, §8 check 1: a committed census that is not canonical gives no draw,
// for the builder and the auditor alike; every slot carries.
func TestNonCanonicalCensus(t *testing.T) {
	s := newScenario(t)
	// Epoch 10's census header names epoch 9: canonical bytes, the wrong
	// epoch.
	c := epochCensus(10)
	c.Epoch = 9
	s.census[10] = commit(t, c, s.census[10].time)
	var err error
	if s.p, err = Build(context.Background(), s.buildInput(11)); err != nil {
		t.Fatal(err)
	}
	e := s.p.Epochs[0]
	if e.Drawn || !errors.Is(e.CensusErr, ErrCensusEpoch) || e.Round != 0 || len(e.Awards) != 0 || e.Census == nil {
		t.Fatalf("epoch 10: drawn %t err %v round %d", e.Drawn, e.CensusErr, e.Round)
	}
	for j := range e.Pots {
		if e.Pots[j].Winners != 0 {
			t.Fatal("a winner without a draw")
		}
	}
	expectPass(t, s.verify(t, s.p.Commit, s.p.Objects))

	// A header that is not canonical JSON, committed: the same.
	s = newScenario(t)
	objects := s.census[10].objects
	bad := append(bytes.Clone(objects.Header), ' ')
	objects.Header, objects.Hash = bad, ledger.RefOf(bad)
	objects.Objects[objects.Hash] = bad
	if s.p, err = Build(context.Background(), s.buildInput(11)); err != nil {
		t.Fatal(err)
	}
	if e := s.p.Epochs[0]; e.Drawn || !errors.Is(e.CensusErr, ErrCensusTrailingData) {
		t.Fatalf("epoch 10: drawn %t err %v", e.Drawn, e.CensusErr)
	}
	expectPass(t, s.verify(t, s.p.Commit, s.p.Objects))
}

// P5, X5: a candidate with a fleet binding at either boundary is never paid,
// even when the census lists it; the auditor, reading the same bindings,
// agrees.
func TestHeadBoundCandidates(t *testing.T) {
	s := newScenario(t)
	var winner [16]byte
	for _, a := range s.p.Epochs[0].Awards {
		if a.Country == cc("us") {
			winner = a.ClientID
		}
	}
	s.heads[10] = map[[16]byte]bool{winner: true, id(3): false}
	bound, err := Build(context.Background(), s.buildInput(11))
	if err != nil {
		t.Fatal(err)
	}
	e := bound.Epochs[0]
	if len(e.Bound) != 1 || e.Bound[0].ClientID != winner {
		t.Fatalf("bound %v", e.Bound)
	}
	for _, a := range e.Awards {
		if a.ClientID == winner {
			t.Fatal("a bound client won")
		}
	}
	if n := e.Pots[PotIndex(cc("us").Index(), RoleExtender)].Winners; n != 4 {
		t.Fatalf("US extenders: %d winners", n)
	}
	expectPass(t, s.verify(t, bound.Commit, bound.Objects))
	// Without the bindings, an auditor's draw keeps the bound client.
	in := s.verifyInput(bound.Commit, &s.f.Commit, bound.Objects)
	in.Heads = nil
	res, err := Verify(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	expectFailure(t, res, CheckDraw, ErrAwardWinner)
}

// ReadPrevious reads back, from published objects, what Build needs of a
// finalized root.
func TestReadPrevious(t *testing.T) {
	s := newScenario(t)
	previous, err := ReadPrevious(context.Background(), s.f.Commit, s.f.Objects)
	if err != nil {
		t.Fatal(err)
	}
	if previous.Root != s.f.Commit || !slices.Equal(previous.Accounts, s.f.Accounts) || previous.State.Pots != s.f.State.Pots || !slices.Equal(previous.State.Pending, s.f.State.Pending) {
		t.Fatal("ReadPrevious differs from the build")
	}
	p, err := ReadPrevious(context.Background(), s.p.Commit, s.p.Objects)
	if err != nil || p.State.Pots != s.p.State.Pots || len(p.State.Pending) != 0 {
		t.Fatalf("P: %v", err)
	}
	objects := maps.Clone(s.f.Objects)
	objects[s.f.Commit.LedgerHash] = append(bytes.Clone(s.f.Header), ' ')
	if _, err := ReadPrevious(context.Background(), s.f.Commit, objects); !errors.Is(err, ledger.ErrPrevious) {
		t.Fatalf("other bytes: %v", err)
	}
	commit := s.f.Commit
	commit.Total.AddUint64(&commit.Total, 1)
	if _, err := ReadPrevious(context.Background(), commit, s.f.Objects); !errors.Is(err, ledger.ErrPrevious) {
		t.Fatalf("another total: %v", err)
	}
}

// A committed census object longer than any canonical census holds makes the
// census not canonical for Build and the auditor alike (no draw), as long as
// its bytes match its reference; served with other bytes it is missing, so it
// never cancels a draw. Build reads the census in one order, the header, the
// row chunks, the exclusion object, and stops at the first such object.
func TestOversizeCensus(t *testing.T) {
	cases := []struct {
		name string
		// census returns the committed header and the objects it names, the
		// oversize one last.
		census func(base *CensusObjects) ([]byte, []ledger.Ref, [][]byte)
		want   error
	}{
		{"header over 256 KiB", func(base *CensusObjects) ([]byte, []ledger.Ref, [][]byte) {
			header := append(bytes.Clone(base.Header), bytes.Repeat([]byte(" "), ledger.MaxHeaderBytes)...)
			return header, nil, nil
		}, ErrCensusTooLarge},
		{"chunk over 131,072 rows", func(base *CensusObjects) ([]byte, []ledger.Ref, [][]byte) {
			chunk := bytes.Repeat([]byte{1}, (ledger.ChunkRows+1)*CensusRowBytes)
			ref := ledger.RefOf(chunk)
			raw := censusHeaderJSON{Schema: CensusSchema, NoID: testNoID, PoolID: PoolID(testNoID), Epoch: 10, Countries: CountriesRef.String(),
				Rows: []string{ref.String()}, Candidates: ledger.ChunkRows + 1, ExcusedSamples: []uint64{}, Excluded: emptyRef.String()}
			return raw.appendJSON(nil), []ledger.Ref{ref}, [][]byte{chunk}
		}, ledger.ErrChunkLength},
		{"exclusion object over 5,000 rows", func(base *CensusObjects) ([]byte, []ledger.Ref, [][]byte) {
			excluded := bytes.Repeat([]byte{2}, (maxExclusionRows+1)*ExclusionRowBytes)
			ref := ledger.RefOf(excluded)
			header := bytes.Replace(base.Header, []byte(base.Excluded.String()), []byte(ref.String()), 1)
			refs := append(slices.Clone(base.Rows), ref)
			datas := [][]byte{}
			for _, r := range base.Rows {
				datas = append(datas, base.Objects[r])
			}
			return header, refs, append(datas, excluded)
		}, ErrExcluded},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newScenario(t)
			base := s.census[10].objects
			header, refs, datas := c.census(base)
			objects := ledger.Objects{ledger.RefOf(header): header}
			for i, ref := range refs {
				objects[ref] = datas[i]
			}
			s.census[10] = &committed{objects: &CensusObjects{Hash: ledger.RefOf(header), Header: header, Objects: objects}, time: s.census[10].time, block: s.census[10].block}
			p, err := Build(context.Background(), s.buildInput(11))
			if err != nil {
				t.Fatal(err)
			}
			if e := p.Epochs[0]; e.Drawn || !errors.Is(e.CensusErr, c.want) || e.Round != 0 || len(e.Awards) != 0 {
				t.Fatalf("drawn %t err %v round %d", e.Drawn, e.CensusErr, e.Round)
			}
			published := maps.Clone(p.Objects)
			maps.Copy(published, objects)
			expectPass(t, s.verifyCopy(t, p.Commit, published))

			// The oversize object served with other bytes is missing.
			big := ledger.RefOf(header)
			if len(refs) > 0 {
				big = refs[len(refs)-1]
			}
			published[big] = flip(published[big], 0, 1)
			res := s.verifyCopy(t, p.Commit, published)
			if res.Check != CheckCensus || !errors.Is(res.Err, ErrObjectHash) || !slices.Contains(res.Missing, big) || res.Action() != ledger.ActionDemand {
				t.Fatalf("other bytes: check %v err %v missing %v action %v", res.Check, res.Err, res.Missing, res.Action())
			}
			// Beyond the byte budget, the auditor decides nothing.
			in := s.verifyInput(p.Commit, &s.f.Commit, maps.Clone(p.Objects))
			maps.Copy(in.Fetcher.(ledger.Objects), objects)
			in.Custody = cloneCustody(s.custody)
			in.MaxFetchBytes = uint64(len(header)) + 100
			if res, err := Verify(context.Background(), in); err != nil || res.Check != CheckFetch || !errors.Is(res.Err, ledger.ErrByteBudget) {
				t.Fatalf("budget: %v %v %v", err, res.Check, res.Err)
			}
		})
	}
}

// A census without exclusion counts names the empty object, which Build and
// DecodeCensus accept without its bytes.
func TestCensusEmptyExclusions(t *testing.T) {
	s := newScenario(t)
	c := epochCensus(10)
	c.Excluded = nil
	s.census[10] = commit(t, c, s.census[10].time)
	objects := s.census[10].objects
	if objects.Excluded != emptyRef {
		t.Fatal("no counts is not the empty object")
	}
	delete(objects.Objects, emptyRef)
	if _, err := DecodeCensus(objects.Header, objects.Objects); err != nil {
		t.Fatal(err)
	}
	p, err := Build(context.Background(), s.buildInput(11))
	if err != nil {
		t.Fatal(err)
	}
	if !p.Epochs[0].Drawn {
		t.Fatalf("not drawn: %v", p.Epochs[0].CensusErr)
	}
	expectPass(t, s.verifyCopy(t, p.Commit, p.Objects))
}
