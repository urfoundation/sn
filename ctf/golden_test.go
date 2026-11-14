package ctf

import (
	"context"
	"testing"

	"github.com/holiman/uint256"

	"github.com/urfoundation/sn/ledger"
	"github.com/urfoundation/sn/merkle"
)

// The worked example of CAPTURETHEFLAG.md §6, at epoch 4's scale with no
// head fleet: F_k = 119,583,893,572 rao and S = 2,490, so a = 48,025,660
// with a remainder of 172 for AD … BA and BB's provider ranks 1 and 2.
const (
	exampleFresh = 119_583_893_572
	exampleSlot  = 48_025_660
)

// examplePots are the pots the example's epoch starts from: DE's extenders
// left 2 slots, IS's providers 3 and extenders 20, and TM's and KP's
// providers 50 epochs of five empty slots.
var examplePots = map[string]uint64{
	"de/extender": 2 * exampleSlot,
	"is/provider": 3 * exampleSlot,
	"is/extender": 20 * exampleSlot,
	"tm/provider": 250 * exampleSlot,
	"kp/provider": 250 * exampleSlot,
}

func examplePot(code string, role Role) int {
	return PotIndex(cc(code).Index(), role)
}

// exampleCensus has the example's units: 1,840 in DE's provider draw, 3 in
// DE's extender draw, 2 in IS's provider draw and 1 in TM's provider draw.
func exampleCensus(k uint64) *Census {
	c := &Census{NoID: testNoID, Epoch: k}
	n := 0
	add := func(role Role, code string, units int) {
		for range units {
			n++
			c.Candidates = append(c.Candidates, Candidate{Role: role, Country: cc(code), Unit: unitHash(n), Family: FamilyIPv6, ClientID: id(n)})
		}
	}
	add(RoleProvider, "de", 1_840)
	add(RoleExtender, "de", 3)
	add(RoleProvider, "is", 2)
	add(RoleProvider, "tm", 1)
	return c
}

// exampleRoot is the root built on, for epoch 8: its only account is the
// carry contract's, holding the pots, and its last epoch's pot rows are them.
type exampleRoot struct {
	previous *Previous
	objects  ledger.Objects
}

func newExampleRoot(t *testing.T) *exampleRoot {
	t.Helper()
	state := State{}
	for name, value := range examplePots {
		role := RoleProvider
		if name[3:] == "extender" {
			role = RoleExtender
		}
		state.Pots[examplePot(name[:2], role)] = u(value)
	}
	owed := state.Obligations()
	accounts := []merkle.LedgerAccount{{Coldkey: carryKey, Cumulative: owed}}
	tree, err := merkle.BuildLedgerSumTree(context.Background(), accounts)
	if err != nil {
		t.Fatal(err)
	}
	pots := make([]Pot, NumPots)
	for j := range pots {
		c, role := PotAt(j)
		pots[j] = Pot{Country: CountryAt(c), Role: role, In: state.Pots[j], Out: state.Pots[j]}
	}
	potRows, err := EncodePots(pots)
	if err != nil {
		t.Fatal(err)
	}
	objects := ledger.Objects{}
	put := func(data []byte) ledger.Ref {
		ref := ledger.RefOf(data)
		objects[ref] = data
		return ref
	}
	chunks, err := ledger.EncodeAccountChunks(accounts)
	if err != nil {
		t.Fatal(err)
	}
	header := &Header{NoID: testNoID, Epoch: 8, CarryColdkey: carryKey, Accounts: []ledger.Ref{put(chunks[0])},
		Epochs: []HeaderEpoch{{Epoch: 8, Awards: put(nil), Pots: put(potRows), Pending: put(nil)}}}
	data, err := EncodeHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	root, total := tree.Root()
	commit := ledger.Root{Epoch: 8, Root: root, Total: total, LeafCount: 1, LedgerHash: put(data)}
	return &exampleRoot{previous: &Previous{Root: commit, Accounts: accounts, State: state}, objects: objects}
}

type exampleRow struct {
	code    string
	role    Role
	units   uint32
	winners int
	potIn   uint64
	each    uint64 // what each winner gets now
	pending uint64 // each winner's pending award
	potOut  uint64
}

// checkExample builds epoch 9 over the example's root with funding f and the
// carry's return, checks the table's rows and the root, and audits it.
func checkExample(t *testing.T, f, recycled uint64, rows []exampleRow) *Ledger {
	t.Helper()
	withTestKey(t)
	ctx := context.Background()
	base := newExampleRoot(t)
	census := commit(t, exampleCensus(9), testTime)
	decoded := exampleCensus(9)
	in := BuildInput{
		NoID: testNoID, ChainID: testChainID, Netuid: testNetuid, Epoch: 9, FirstEpoch: testFirst, CarryColdkey: carryKey,
		Previous: base.previous,
		Epochs: []EpochInput{{
			Epoch: 9, Census: census.input(), Pulse: testPulse(testRound), Recycled: u(recycled),
			Coldkeys: coldkeysOf(func(n uint64) [32]byte { return key(int(n)) }, decoded),
		}},
		FundedThrough: map[uint64]uint256.Int{9: sum(base.previous.Root.Total, u(f))},
	}
	l, err := Build(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	e := l.Epochs[0]
	if !e.Fresh.Eq(uint256.NewInt(exampleFresh)) || e.SlotBase.Uint64() != exampleSlot || e.SlotRemainder != 172 {
		t.Fatalf("fresh %s, slot %s, remainder %d", e.Fresh.Dec(), e.SlotBase.Dec(), e.SlotRemainder)
	}
	// The remainder: every slot of AD … BA, and BB's provider ranks 1 and 2.
	for s := range NumSlots {
		a := e.SlotAmount(s)
		want := uint64(exampleSlot)
		c, role, rank := s/(2*Ranks), Role(s/Ranks%2+1), s%Ranks+1
		if CountryAt(c).String() <= "ba" || (CountryAt(c).String() == "bb" && role == RoleProvider && rank <= 2) {
			want++
		}
		if a.Uint64() != want {
			t.Fatalf("slot %d (%v %v %d): %s", s, CountryAt(c), role, rank, a.Dec())
		}
	}
	var paid uint256.Int
	for _, row := range rows {
		j := examplePot(row.code, row.role)
		p := &e.Pots[j]
		if p.Units != row.units || p.Winners != row.winners || p.In.Uint64() != row.potIn || p.Out.Uint64() != row.potOut {
			t.Fatalf("%s %v: units %d winners %d in %s out %s", row.code, row.role, p.Units, p.Winners, p.In.Dec(), p.Out.Dec())
		}
		n := 0
		for _, a := range e.Awards {
			if a.Country != cc(row.code) || a.Role != row.role {
				continue
			}
			n++
			now := a.Slot
			if !a.Deferred {
				now.Add(&now, &a.Pot)
			}
			pending := uint64(0)
			if a.Deferred {
				pending = a.Pot.Uint64()
			}
			if now.Uint64() != row.each || pending != row.pending {
				t.Fatalf("%s %v rank %d: now %s pending %d", row.code, row.role, a.Rank, now.Dec(), pending)
			}
			if got := deltaOf(l.Deltas, a.Coldkey); !got.Eq(&now) {
				t.Fatalf("%s %v rank %d: delta %s", row.code, row.role, a.Rank, got.Dec())
			}
			paid.Add(&paid, &now)
		}
		if n != row.winners {
			t.Fatalf("%s %v: %d awards", row.code, row.role, n)
		}
	}
	// The root: everything not paid is the carry account's, so the total is
	// fundedThrough[k].
	got := e.Paid()
	if !got.Eq(&paid) {
		t.Fatalf("paid %s, rows %s", got.Dec(), paid.Dec())
	}
	var carry uint256.Int
	carry.Sub(uint256.NewInt(f), &paid)
	if d := deltaOf(l.Deltas, carryKey); !d.Eq(&carry) {
		t.Fatalf("carry delta %s, want %s", d.Dec(), carry.Dec())
	}
	if total := in.FundedThrough[9]; !l.Commit.Total.Eq(&total) || len(l.Accounts) != 1+len(l.Deltas)-1 {
		t.Fatalf("total %s, %d accounts", l.Commit.Total.Dec(), len(l.Accounts))
	}

	// The auditor reproduces it.
	objects := base.objects
	for ref, data := range l.Objects {
		objects[ref] = data
	}
	res, err := Verify(ctx, VerifyInput{
		NoID: testNoID, ChainID: testChainID, Netuid: testNetuid, FirstEpoch: testFirst, CarryColdkey: carryKey,
		Pending: l.Commit, Previous: &base.previous.Root, FundedThrough: in.FundedThrough,
		Epochs:  map[uint64]EpochFacts{9: {Census: census.facts(), Pulse: testPulse(testRound), Recycled: u(recycled)}},
		Fetcher: objects, Custody: ledger.NewMemoryCustody(), MaxFetchBytes: ledger.DefaultMaxFetchBytes,
	})
	if err != nil {
		t.Fatal(err)
	}
	expectPass(t, res)
	return l
}

// §6, the matured epoch: the carry from the epoch before is back.
func TestWorkedExample(t *testing.T) {
	owed := uint64(0)
	for _, v := range examplePots {
		owed += v
	}
	if owed != 25_213_471_500 {
		t.Fatalf("owed %d", owed)
	}
	l := checkExample(t, exampleFresh+owed, owed, []exampleRow{
		{"de", RoleProvider, 1_840, 5, 0, 48_025_660, 0, 0},
		{"de", RoleExtender, 3, 3, 96_051_320, 67_235_924, 0, 134_471_848},
		{"is", RoleProvider, 2, 2, 144_076_980, 76_841_056, 0, 230_523_168},
		{"is", RoleExtender, 0, 0, 960_513_200, 0, 0, 1_200_641_500},
		{"tm", RoleProvider, 1, 1, 12_006_415_000, 2_449_308_660, 0, 9_797_234_640},
		{"kp", RoleProvider, 0, 0, 12_006_415_000, 0, 0, 12_246_543_300},
	})
	e := l.Epochs[0]
	if !e.Matured || !e.Credited.Eq(uint256.NewInt(owed)) || len(e.Pending) != 0 {
		t.Fatalf("matured %t credited %s", e.Matured, e.Credited.Dec())
	}
	// Paid: DE's five providers, DE's three extenders, IS's two providers
	// and TM's one, 3,044,826,844 rao; the carry account gets the rest.
	paid, carry := e.Paid(), deltaOf(l.Deltas, carryKey)
	if paid.Uint64() != 3_044_826_844 || carry.Uint64() != 141_752_538_228 {
		t.Fatalf("paid %s carry %s", paid.Dec(), carry.Dec())
	}
	// KP: both pots grow by 5a a role, 0.24 α each.
	kp := e.Pots[examplePot("kp", RoleExtender)]
	if kp.Out.Uint64() != 5*exampleSlot || kp.Out.Uint64() != 240_128_300 {
		t.Fatalf("KP extenders %s", kp.Out.Dec())
	}
}

// §6, if the carry were late: R_k = 0, k is not matured and F_k = f_k. TM's
// winner gets its slot now and a pending award of 2,401,283,000 rao, and the
// pots move exactly as in the table.
func TestWorkedExampleCarryLate(t *testing.T) {
	l := checkExample(t, exampleFresh, 0, []exampleRow{
		{"de", RoleProvider, 1_840, 5, 0, 48_025_660, 0, 0},
		{"de", RoleExtender, 3, 3, 96_051_320, 48_025_660, 19_210_264, 134_471_848},
		{"is", RoleProvider, 2, 2, 144_076_980, 48_025_660, 28_815_396, 230_523_168},
		{"is", RoleExtender, 0, 0, 960_513_200, 0, 0, 1_200_641_500},
		{"tm", RoleProvider, 1, 1, 12_006_415_000, 48_025_660, 2_401_283_000, 9_797_234_640},
		{"kp", RoleProvider, 0, 0, 12_006_415_000, 0, 0, 12_246_543_300},
	})
	e := l.Epochs[0]
	if e.Matured || !e.Credited.IsZero() || len(e.Pending) != 6 || e.PendingTotal.Uint64() != 2_516_544_584 {
		t.Fatalf("matured %t pending %d total %s", e.Matured, len(e.Pending), e.PendingTotal.Dec())
	}
	paid, carry := e.Paid(), deltaOf(l.Deltas, carryKey)
	if paid.Uint64() != 11*exampleSlot || carry.Uint64() != exampleFresh-11*exampleSlot {
		t.Fatalf("paid %s carry %s", paid.Dec(), carry.Dec())
	}
}

// §4.5 and §6: a lone miner that stays in TM takes a fifth of what is left
// each epoch: 8.63 α over five epochs and 12.26 α over ten, about 67% and 89%
// of the pot, and the pot settles at twenty slot amounts.
func TestLoneWinnerStays(t *testing.T) {
	tm := examplePot("tm", RoleProvider)
	state := &State{}
	state.Pots[tm] = u(250 * exampleSlot)
	w := newWalk(state)
	var taken uint64
	draw := drawWith(potDraw{"tm", RoleProvider, 1, 1})
	for k := uint64(1); k <= 200; k++ {
		owed := w.owed
		f := sum(owed, u(exampleFresh))
		e, err := w.step(k, &f, &owed, draw, winnerKey)
		if err != nil {
			t.Fatal(err)
		}
		taken += e.Awards[0].Slot.Uint64() + e.Awards[0].Pot.Uint64()
		switch k {
		case 5:
			if taken != 8_627_022_197 {
				t.Fatalf("five epochs: %d rao", taken)
			}
		case 10:
			if taken != 12_261_140_124 {
				t.Fatalf("ten epochs: %d rao", taken)
			}
		case 200:
			if p := e.Pots[tm].Out.Uint64(); p < 20*exampleSlot-5 || p > 20*exampleSlot+5 || e.Awards[0].Pot.Uint64()+exampleSlot > 5*exampleSlot+1 {
				t.Fatalf("settled at %d, taking %s", p, e.Awards[0].Pot.Dec())
			}
		}
	}
}
