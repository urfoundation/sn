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
	"github.com/urfoundation/sn/merkle"
)

func cloneCustody(c *ledger.MemoryCustody) *ledger.MemoryCustody {
	return &ledger.MemoryCustody{Objects: maps.Clone(c.Objects), Rows: maps.Clone(c.Rows), Nulls: maps.Clone(c.Nulls)}
}

// verifyCopy audits with a copy of the scenario's custody, so that one
// audit's findings do not reach the next.
func (s *scenario) verifyCopy(t testing.TB, pending ledger.Root, objects ledger.Objects) *Result {
	t.Helper()
	saved := s.custody
	s.custody = cloneCustody(saved)
	defer func() { s.custody = saved }()
	return s.verify(t, pending, objects)
}

func objectBytes(objects ledger.Objects) uint64 {
	var n uint64
	for _, data := range objects {
		n += uint64(len(data))
	}
	return n
}

// An honest root passes; the audit keeps what it fetched and the recomputed
// pot and pending rows, records each epoch's awards as valid, and reports
// the epochs exactly as Build did. Auditing again fetches nothing.
func TestVerifyHonest(t *testing.T) {
	s := newScenario(t)
	res := s.verify(t, s.p.Commit, s.p.Objects)
	expectPass(t, res)
	if !res.Reproduced || len(res.Faults) != 0 || !slices.Equal(res.Accounts, s.p.Accounts) || !slices.Equal(res.PreviousAccounts, s.f.Accounts) {
		t.Fatal("accounts not returned")
	}
	if res.PreviousState.Pots != s.f.State.Pots {
		t.Fatal("previous state")
	}
	if len(res.Epochs) != 2 {
		t.Fatalf("%d epochs", len(res.Epochs))
	}
	for i, e := range res.Epochs {
		b := s.p.Epochs[i]
		if e.AwardsRef != b.AwardsRef || e.PotsRef != b.PotsRef || e.PendingRef != b.PendingRef || !e.Fresh.Eq(&b.Fresh) || e.Matured != b.Matured || e.Seed != b.Seed || e.Round != b.Round {
			t.Fatalf("epoch %d differs from the build", e.Epoch)
		}
		if held, ok := s.custody.Rows[ledger.PoolEpoch{NoID: PoolID(testNoID), Epoch: e.Epoch}]; !ok || !slices.Equal(held, []ledger.Ref{e.AwardsRef}) {
			t.Fatalf("epoch %d awards not recorded", e.Epoch)
		}
		for _, ref := range []ledger.Ref{e.PotsRef, e.PendingRef} {
			if ref != emptyRef && !bytes.Equal(s.custody.Objects[ref], s.p.Objects[ref]) {
				t.Fatalf("epoch %d object %v not kept", e.Epoch, ref)
			}
		}
	}
	again := s.verify(t, s.p.Commit, ledger.Objects{})
	expectPass(t, again)
	if again.FetchedBytes != 0 {
		t.Fatalf("the second audit fetched %d bytes", again.FetchedBytes)
	}
}

// A watcher with no custody, as snclaim v2 check, fetches both roots.
func TestVerifyFreshCustody(t *testing.T) {
	s := newScenario(t)
	objects := maps.Clone(s.f.Objects)
	maps.Copy(objects, s.p.Objects)
	s.custody = ledger.NewMemoryCustody()
	res := s.verify(t, s.p.Commit, objects)
	expectPass(t, res)
	if res.FetchedBytes == 0 || res.FetchedBytes > objectBytes(objects) {
		t.Fatalf("fetched %d bytes of %d", res.FetchedBytes, objectBytes(objects))
	}
}

// Every byte of the header, changed and committed, fails the audit.
func TestVerifyHeaderMutations(t *testing.T) {
	s := newScenario(t)
	pub := publish(t, s.p)
	checks := map[Check]int{}
	stride := 1
	if testing.Short() {
		stride = 10
	}
	for i := 0; i < len(s.p.Header); i += stride {
		data := flip(s.p.Header, i, 1)
		res := s.verifyCopy(t, pub.sealBytes(data), pub.objects)
		if res.Err == nil {
			t.Fatalf("byte %d (%q) changed, and the audit passed", i, s.p.Header[i])
		}
		checks[res.Check]++
	}
	t.Logf("%d header mutations: %v", len(s.p.Header), checks)

	cases := []struct {
		name   string
		mutate func(*Header)
		check  Check
		want   error
	}{
		{"no_id", func(h *Header) { h.NoID = 2 }, CheckHeader, ledger.ErrHeaderNoID},
		{"epoch", func(h *Header) { h.Epoch = 12 }, CheckHeader, ledger.ErrHeaderEpoch},
		{"carry coldkey", func(h *Header) { h.CarryColdkey = key(9) }, CheckHeader, ErrHeaderCarryColdkey},
		{"an epoch missing", func(h *Header) { h.Epochs = h.Epochs[1:] }, CheckHeader, ledger.ErrCoveredEpochs},
		{"epochs shifted", func(h *Header) { h.Epochs[0].Epoch = 9 }, CheckHeader, ledger.ErrCoveredEpochs},
		{"census null", func(h *Header) { h.Epochs[0].Census = nil }, CheckCensus, ErrCensusReference},
		{"another epoch's census", func(h *Header) { h.Epochs[0].Census = h.Epochs[1].Census }, CheckCensus, ErrCensusReference},
		{"round", func(h *Header) { h.Epochs[1].Round++ }, CheckSeed, ErrRound},
		{"awards of another epoch", func(h *Header) { h.Epochs[0].Awards = h.Epochs[1].Awards }, CheckDraw, ErrAwardWinner},
		{"no awards", func(h *Header) { h.Epochs[0].Awards = emptyRef }, CheckDraw, ErrAwardWinner},
		{"pots of another epoch", func(h *Header) { h.Epochs[1].Pots = h.Epochs[0].Pots }, CheckAmounts, ErrPots},
		{"pending of another epoch", func(h *Header) { h.Epochs[1].Pending = h.Epochs[0].Pending }, CheckAmounts, ErrPending},
		{"pending cleared", func(h *Header) { h.Epochs[0].Pending = emptyRef }, CheckAmounts, ErrPending},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pub := publish(t, s.p)
			c.mutate(pub.header)
			expectFailure(t, s.verifyCopy(t, pub.seal(t), pub.objects), c.check, c.want)
		})
	}
}

// mutateObject publishes each single-byte change of one epoch's object,
// names it in the header and audits the root; every change must fail.
func mutateObject(t *testing.T, s *scenario, epoch int, field func(*HeaderEpoch) *ledger.Ref, stride int) map[Check]int {
	t.Helper()
	checks := map[Check]int{}
	pub := publish(t, s.p)
	ref := field(&pub.header.Epochs[epoch])
	original := *ref
	data := s.p.Objects[original]
	if len(data) == 0 {
		t.Fatalf("epoch %d object is empty", epoch)
	}
	if testing.Short() {
		stride *= 10
	}
	for i := 0; i < len(data); i += stride {
		// Alternate the low and the high bit of each byte.
		mask := byte(1) << (7 * (i / stride % 2))
		*ref = pub.put(flip(data, i, mask))
		res := s.verifyCopy(t, pub.seal(t), pub.objects)
		if res.Err == nil {
			t.Fatalf("epoch %d object byte %d ^ %#x: the audit passed", epoch, i, mask)
		}
		checks[res.Check]++
	}
	*ref = original
	return checks
}

// §8 checks 3 and 4: every byte of the award rows, changed, fails the draw,
// the amounts or the accounts; a changed pot or pending row fails the amounts.
func TestVerifyObjectMutations(t *testing.T) {
	s := newScenario(t)
	awards := func(e *HeaderEpoch) *ledger.Ref { return &e.Awards }
	pots := func(e *HeaderEpoch) *ledger.Ref { return &e.Pots }
	pending := func(e *HeaderEpoch) *ledger.Ref { return &e.Pending }
	t.Logf("epoch 10 awards: %v", mutateObject(t, s, 0, awards, 1))
	t.Logf("epoch 11 awards: %v", mutateObject(t, s, 1, awards, 3))
	t.Logf("epoch 10 pending: %v", mutateObject(t, s, 0, pending, 1))
	for _, e := range []int{0, 1} {
		checks := mutateObject(t, s, e, pots, 97)
		if len(checks) != 1 || checks[CheckAmounts] == 0 {
			t.Fatalf("pots: %v", checks)
		}
	}

	// Each award field, precisely.
	cases := []struct {
		name   string
		mutate func(*Award)
		check  Check
		want   error
	}{
		{"unit", func(a *Award) { a.Unit[3] ^= 1 }, CheckDraw, ErrAwardWinner},
		{"member", func(a *Award) { a.ClientID[15] ^= 1 }, CheckDraw, ErrAwardWinner},
		{"zero coldkey", func(a *Award) { a.Coldkey = [32]byte{} }, CheckDraw, ErrAwardColdkey},
		// Epoch 10 is not matured: the pending award names the coldkey.
		{"coldkey", func(a *Award) { a.Coldkey = key(4242) }, CheckAmounts, ErrPending},
		{"slot", func(a *Award) { a.Slot.AddUint64(&a.Slot, 1) }, CheckAmounts, ErrAwardAmount},
		{"pot share", func(a *Award) { a.Pot.AddUint64(&a.Pot, 1) }, CheckAmounts, ErrAwardAmount},
		{"deferred", func(a *Award) { a.Deferred = !a.Deferred }, CheckAmounts, ErrAwardAmount},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pub := publish(t, s.p)
			rows := slices.Clone(s.p.Epochs[0].Awards)
			c.mutate(&rows[len(rows)-1])
			data, err := EncodeAwards(rows)
			if err != nil {
				t.Fatal(err)
			}
			pub.header.Epochs[0].Awards = pub.put(data)
			expectFailure(t, s.verifyCopy(t, pub.seal(t), pub.objects), c.check, c.want)
		})
	}
	t.Run("coldkey in a matured epoch", func(t *testing.T) {
		pub := publish(t, s.p)
		rows := slices.Clone(s.p.Epochs[1].Awards)
		rows[0].Coldkey = key(4242)
		data, _ := EncodeAwards(rows)
		pub.header.Epochs[1].Awards = pub.put(data)
		expectFailure(t, s.verifyCopy(t, pub.seal(t), pub.objects), CheckAllocation, ledger.ErrAccountOmitted)
	})
	t.Run("rows reordered", func(t *testing.T) {
		pub := publish(t, s.p)
		rows := slices.Clone(s.p.Epochs[0].Awards)
		rows[0], rows[1] = rows[1], rows[0]
		data, _ := EncodeAwards(rows)
		pub.header.Epochs[0].Awards = pub.put(data)
		expectFailure(t, s.verifyCopy(t, pub.seal(t), pub.objects), CheckDraw, ErrAwardRows)
	})
	t.Run("a winner dropped", func(t *testing.T) {
		pub := publish(t, s.p)
		rows := s.p.Epochs[0].Awards
		data, _ := EncodeAwards(rows[:len(rows)-1])
		pub.header.Epochs[0].Awards = pub.put(data)
		expectFailure(t, s.verifyCopy(t, pub.seal(t), pub.objects), CheckDraw, ErrAwardWinner)
	})
}

// Account rows: every byte, changed, fails; so do rows that rebuild their
// own committed root but lower or move a previous account, or pay another
// split.
func TestVerifyAccountMutations(t *testing.T) {
	s := newScenario(t)
	pub := publish(t, s.p)
	chunk := s.p.Objects[pub.header.Accounts[0]]
	checks := map[Check]int{}
	stride := 1
	if testing.Short() {
		stride = 10
	}
	for i := 0; i < len(chunk); i += stride {
		pub.header.Accounts = []ledger.Ref{pub.put(flip(chunk, i, 1))}
		res := s.verifyCopy(t, pub.seal(t), pub.objects)
		if res.Err == nil {
			t.Fatalf("account byte %d changed, and the audit passed", i)
		}
		checks[res.Check]++
	}
	t.Logf("%d account mutations: %v", len(chunk), checks)

	// Rows committed as their own root.
	recommit := func(t *testing.T, accounts []merkle.LedgerAccount) (*Result, ledger.Root) {
		t.Helper()
		pub := publish(t, s.p)
		chunks, err := ledger.EncodeAccountChunks(accounts)
		if err != nil {
			t.Fatal(err)
		}
		pub.header.Accounts = []ledger.Ref{pub.put(chunks[0])}
		tree, err := merkle.BuildLedgerSumTree(context.Background(), accounts)
		if err != nil {
			t.Fatal(err)
		}
		commit := pub.seal(t)
		commit.Root, commit.Total = tree.Root()
		commit.LeafCount = tree.LeafCount()
		return s.verifyCopy(t, commit, pub.objects), commit
	}
	t.Run("previous account lowered", func(t *testing.T) {
		accounts := slices.Clone(s.p.Accounts)
		accounts[0].Cumulative = s.f.Accounts[0].Cumulative
		accounts[0].Cumulative.SubUint64(&accounts[0].Cumulative, 1)
		res, _ := recommit(t, accounts)
		expectFailure(t, res, CheckAccounts, ledger.ErrAccountLowered)
		if res.Action() != ledger.ActionChallenge {
			t.Fatalf("action %v", res.Action())
		}
	})
	t.Run("value moved between winners", func(t *testing.T) {
		accounts := slices.Clone(s.p.Accounts)
		// Two accounts that P raised.
		var raised []int
		for i := range accounts {
			if i >= len(s.f.Accounts) || accounts[i].Cumulative.Gt(&s.f.Accounts[i].Cumulative) {
				raised = append(raised, i)
			}
		}
		accounts[raised[0]].Cumulative.AddUint64(&accounts[raised[0]].Cumulative, 1)
		accounts[raised[1]].Cumulative.SubUint64(&accounts[raised[1]].Cumulative, 1)
		res, _ := recommit(t, accounts)
		expectFailure(t, res, CheckAllocation, ledger.ErrAccountDelta)
	})
	t.Run("carry account withheld", func(t *testing.T) {
		accounts := slices.Clone(s.p.Accounts)
		for i := range accounts {
			if accounts[i].Coldkey == carryKey {
				accounts[i].Cumulative.SubUint64(&accounts[i].Cumulative, 1)
			}
		}
		res, _ := recommit(t, accounts)
		expectFailure(t, res, CheckAllocation, ledger.ErrAccountDelta)
	})
}

// Objects served with other bytes, or not at all, are missing, never a
// reason to cancel a draw: a census whose bytes do not match fails, and with
// a finalized root to protect the auditor demands.
func TestVerifyServedBytes(t *testing.T) {
	s := newScenario(t)
	census := s.census[10].objects
	pending, err := DecodeHeader(s.p.Header)
	if err != nil {
		t.Fatal(err)
	}
	for name, ref := range map[string]ledger.Ref{
		"census header":   census.Hash,
		"census rows":     census.Rows[0],
		"exclusion rows":  census.Excluded,
		"awards":          s.p.Epochs[0].AwardsRef,
		"pots":            s.p.Epochs[0].PotsRef,
		"pending":         s.p.Epochs[0].PendingRef,
		"pending header":  ledger.Ref(s.p.Commit.LedgerHash),
		"account chunk":   pending.Accounts[0],
		"previous header": ledger.Ref(s.f.Commit.LedgerHash),
	} {
		t.Run(name+" other bytes", func(t *testing.T) {
			objects := maps.Clone(s.p.Objects)
			if data, ok := objects[ref]; ok {
				objects[ref] = flip(data, len(data)-1, 1)
			}
			s.custody = ledger.NewMemoryCustody()
			maps.Copy(objects, map[ledger.Ref][]byte{})
			for k, v := range s.f.Objects {
				if _, ok := objects[k]; !ok {
					objects[k] = v
				}
			}
			if ref == ledger.Ref(s.f.Commit.LedgerHash) {
				objects[ref] = append(bytes.Clone(s.f.Header), ' ')
			}
			res := s.verify(t, s.p.Commit, objects)
			if res.Err == nil || !slices.Contains(res.Missing, ref) {
				t.Fatalf("check %v err %v missing %v", res.Check, res.Err, res.Missing)
			}
		})
		t.Run(name+" unavailable", func(t *testing.T) {
			s := newScenario(t)
			objects := maps.Clone(s.p.Objects)
			delete(objects, ref)
			s.custody = cloneCustody(s.custody)
			delete(s.custody.Objects, ref)
			res := s.verify(t, s.p.Commit, objects)
			if res.Check != CheckFetch || !errors.Is(res.Err, ledger.ErrObjectUnavailable) || !slices.Contains(res.Missing, ref) {
				t.Fatalf("check %v err %v", res.Check, res.Err)
			}
			if res.Previous != nil && res.Action() != ledger.ActionDemand {
				t.Fatalf("action %v", res.Action())
			}
		})
	}
}

// The chain's facts decide the audit: the committed census, its block's
// time, the pulse, the carry's return, the funding and the bindings.
func TestVerifyChainFacts(t *testing.T) {
	s := newScenario(t)
	cases := []struct {
		name   string
		mutate func(*VerifyInput)
		check  Check
		want   error
	}{
		{"another census committed", func(in *VerifyInput) {
			f := in.Epochs[10]
			f.Census = s.census[11].facts()
			in.Epochs[10] = f
		}, CheckCensus, ErrCensusReference},
		{"no census committed", func(in *VerifyInput) {
			f := in.Epochs[10]
			f.Census = nil
			in.Epochs[10] = f
		}, CheckCensus, ErrCensusReference},
		{"another commitment time", func(in *VerifyInput) {
			f := in.Epochs[10]
			c := *f.Census
			c.Time += 3
			f.Census, f.Pulse = &c, testPulse(DrawRound(c.Time))
			in.Epochs[10] = f
		}, CheckSeed, ErrRound},
		{"the carry short beyond rounding", func(in *VerifyInput) {
			f := in.Epochs[11]
			f.Recycled.SubUint64(&f.Recycled, CarryTolerance+1)
			in.Epochs[11] = f
		}, CheckAmounts, ErrAwardAmount},
		// One more rao fills the remainder: only the last slot, an unfilled
		// one, moves.
		{"funding", func(in *VerifyInput) {
			in.FundedThrough = maps.Clone(in.FundedThrough)
			v := in.FundedThrough[10]
			v.AddUint64(&v, 1)
			in.FundedThrough[10] = v
		}, CheckAmounts, ErrPots},
		{"a winner bound", func(in *VerifyInput) {
			in.Heads = heads{10: {s.p.Epochs[0].Awards[0].ClientID: true}}
		}, CheckDraw, ErrAwardWinner},
		{"another chain", func(in *VerifyInput) { in.ChainID = 945 }, CheckDraw, ErrAwardWinner},
		{"another carry contract", func(in *VerifyInput) { in.CarryColdkey = key(3) }, CheckHeader, ErrHeaderCarryColdkey},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := s.verifyInput(s.p.Commit, &s.f.Commit, s.p.Objects)
			in.Custody = cloneCustody(s.custody)
			c.mutate(&in)
			res, err := Verify(context.Background(), in)
			if err != nil {
				t.Fatal(err)
			}
			expectFailure(t, res, c.check, c.want)
		})
	}
	errorCases := []struct {
		name   string
		mutate func(*VerifyInput)
		want   error
	}{
		{"no facts", func(in *VerifyInput) { delete(in.Epochs, 11) }, ErrInput},
		{"no pulse", func(in *VerifyInput) {
			f := in.Epochs[10]
			f.Pulse = nil
			in.Epochs[10] = f
		}, ErrInput},
		{"pulse not verified", func(in *VerifyInput) {
			f := in.Epochs[10]
			f.Pulse = &Pulse{Round: f.Pulse.Round, Signature: flip(f.Pulse.Signature, 20, 1)}
			in.Epochs[10] = f
		}, ErrPulse},
		{"funding missing", func(in *VerifyInput) { in.FundedThrough = map[uint64]uint256.Int{} }, ledger.ErrFunding},
		{"no custody", func(in *VerifyInput) { in.Custody = nil }, ErrInput},
		{"operator at 2^32", func(in *VerifyInput) { in.NoID = PoolIDBase }, ErrInput},
	}
	for _, c := range errorCases {
		t.Run(c.name, func(t *testing.T) {
			in := s.verifyInput(s.p.Commit, &s.f.Commit, s.p.Objects)
			in.Custody = cloneCustody(s.custody)
			c.mutate(&in)
			if in.Custody == nil {
				in.Custody = nil
			}
			if _, err := Verify(context.Background(), in); !errors.Is(err, c.want) {
				t.Fatalf("%v, want %v", err, c.want)
			}
		})
	}
}

// §5 fixed inputs: once custody holds an epoch's awards as valid, a later
// root must list them again, even with accounts that match its own awards.
func TestVerifyFixedInputs(t *testing.T) {
	s := newScenario(t)
	expectPass(t, s.verify(t, s.p.Commit, s.p.Objects))
	in := s.buildInput(11)
	winner := s.p.Epochs[0].Awards[0].ClientID
	in.Epochs[0].Coldkeys = maps.Clone(in.Epochs[0].Coldkeys)
	in.Epochs[0].Coldkeys[winner] = key(777)
	redirected, err := Build(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	expectFailure(t, s.verify(t, redirected.Commit, redirected.Objects), CheckFixedInputs, ledger.ErrFixedInputs)
	// An auditor that never held them accepts the operator's root.
	s.custody = cloneCustody(s.custody)
	for _, k := range []uint64{10, 11} {
		delete(s.custody.Rows, ledger.PoolEpoch{NoID: PoolID(testNoID), Epoch: k})
	}
	expectPass(t, s.verify(t, redirected.Commit, redirected.Objects))
}

// The byte budget bounds what an audit fetches.
func TestVerifyByteBudget(t *testing.T) {
	s := newScenario(t)
	in := s.verifyInput(s.p.Commit, &s.f.Commit, s.p.Objects)
	in.MaxFetchBytes = 1_000
	res, err := Verify(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	expectFailure(t, res, CheckFetch, ledger.ErrByteBudget)
}

// The pool's first root has nothing to protect: a failure refuses.
func TestVerifyFirstRoot(t *testing.T) {
	s := newScenario(t)
	s.custody = ledger.NewMemoryCustody()
	in := s.verifyInput(s.f.Commit, nil, s.f.Objects)
	res, err := Verify(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	expectPass(t, res)
	pub := publish(t, s.f)
	pub.header.Epochs[0].Round++
	in = s.verifyInput(pub.seal(t), nil, pub.objects)
	in.Custody = ledger.NewMemoryCustody()
	if res, err = Verify(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	expectFailure(t, res, CheckSeed, ErrRound)
	if res.Action() != ledger.ActionRefuse {
		t.Fatalf("action %v", res.Action())
	}
	if CheckDraw.String() != "draw" || Check(99).String() != "check(99)" || RoleExtender.String() != "extender" || Role(7).String() != "role(7)" {
		t.Fatal("names")
	}
}

// Any changed input byte changes the root, or the build refuses it: the
// census objects (committed again), the pulse, the carry's return, the
// funding, the coldkeys and the bindings.
func TestBuildInputMutations(t *testing.T) {
	s := newScenario(t)
	base := s.p.Commit
	differs := func(t *testing.T, name string, in BuildInput) {
		t.Helper()
		l, err := Build(context.Background(), in)
		if err == nil && l.Commit == base {
			t.Fatalf("%s: the root did not change", name)
		}
	}
	census := s.census[10].objects
	recommit := func(header []byte, objects ledger.Objects) BuildInput {
		in := s.buildInput(11)
		in.Epochs[0].Census = &CensusInput{Hash: ledger.RefOf(header), Block: s.census[10].block, Time: s.census[10].time, Objects: objects}
		in.Epochs[0].Census.Objects[ledger.RefOf(header)] = header
		return in
	}
	stride := 1
	if testing.Short() {
		stride = 10
	}
	for i := 0; i < len(census.Header); i += stride {
		differs(t, "census header", recommit(flip(census.Header, i, 1), maps.Clone(census.Objects)))
	}
	rows := census.Objects[census.Rows[0]]
	for i := 0; i < len(rows); i += stride {
		objects := maps.Clone(census.Objects)
		changed := flip(rows, i, 1)
		objects[ledger.RefOf(changed)] = changed
		header := bytes.Replace(census.Header, []byte(census.Rows[0].String()), []byte(ledger.RefOf(changed).String()), 1)
		differs(t, "census rows", recommit(header, objects))
	}
	signature := s.pIn.Epochs[0].Pulse.Signature
	for i := range signature {
		in := s.buildInput(11)
		in.Epochs[0].Pulse = &Pulse{Round: in.Epochs[0].Pulse.Round, Signature: flip(signature, i, 1)}
		if _, err := Build(context.Background(), in); !errors.Is(err, ErrPulse) {
			t.Fatalf("signature byte %d: %v", i, err)
		}
	}
	// The carry's return within CarryTolerance of what is owed matures the
	// epoch as the exact return does, so it must fall short beyond it.
	for name, mutate := range map[string]func(*BuildInput){
		"recycled": func(in *BuildInput) { in.Epochs[1].Recycled.SubUint64(&in.Epochs[1].Recycled, CarryTolerance+1) },
		"funding": func(in *BuildInput) {
			in.FundedThrough = maps.Clone(in.FundedThrough)
			v := in.FundedThrough[11]
			v.AddUint64(&v, 1)
			in.FundedThrough[11] = v
		},
		"coldkey": func(in *BuildInput) {
			in.Epochs[1].Coldkeys = maps.Clone(in.Epochs[1].Coldkeys)
			in.Epochs[1].Coldkeys[s.p.Epochs[1].Awards[0].ClientID] = key(31337)
		},
		"bound":  func(in *BuildInput) { in.Epochs[0].Bound = map[[16]byte]bool{s.p.Epochs[0].Awards[0].ClientID: true} },
		"netuid": func(in *BuildInput) { in.Netuid = 26 },
		"chain":  func(in *BuildInput) { in.ChainID = 945 },
		"carry":  func(in *BuildInput) { in.CarryColdkey = key(5) },
		"time": func(in *BuildInput) {
			in.Epochs[0].Census.Time += 3
			in.Epochs[0].Pulse = testPulse(DrawRound(in.Epochs[0].Census.Time))
		},
		"no draw": func(in *BuildInput) { in.Epochs[0].Census = nil },
	} {
		in := s.buildInput(11)
		mutate(&in)
		differs(t, name, in)
	}
}

// §9.3: a withheld state object of the previous root, like any withheld
// object, leads to a demand: the previous accounts are verified before it is
// read, and the pending accounts too.
func TestVerifyPreviousStateWithheld(t *testing.T) {
	s := newScenario(t)
	f, err := DecodeHeader(s.f.Header)
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range []ledger.Ref{f.Epochs[len(f.Epochs)-1].Pots, f.Epochs[len(f.Epochs)-1].Pending} {
		if ref == emptyRef {
			continue
		}
		s.custody = cloneCustody(s.custody)
		delete(s.custody.Objects, ref)
		objects := maps.Clone(s.p.Objects)
		delete(objects, ref)
		res := s.verify(t, s.p.Commit, objects)
		if res.Check != CheckFetch || !slices.Contains(res.Missing, ref) || res.Previous == nil || !res.Reproduced || res.Action() != ledger.ActionDemand {
			t.Fatalf("check %v missing %v previous %t reproduced %t action %v", res.Check, res.Missing, res.Previous != nil, res.Reproduced, res.Action())
		}
	}
}

// A return recorded for a last covered epoch that funded nothing would roll
// past the root: the auditor refuses such a root, and Build refuses to build
// it.
func TestVerifyReturnRollsPastRoot(t *testing.T) {
	s := newScenario(t)
	s.funded[11] = s.funded[10]
	s.recycled[11] = uint256.Int{}
	p, err := Build(context.Background(), s.buildInput(11))
	if err != nil {
		t.Fatal(err)
	}
	s.recycled[11] = rao(12_345)
	res := s.verifyCopy(t, p.Commit, p.Objects)
	expectFailure(t, res, CheckAmounts, ErrReturnRolls)
	if _, err := Build(context.Background(), s.buildInput(11)); !errors.Is(err, ErrReturnRolls) {
		t.Fatalf("build: %v", err)
	}
}
