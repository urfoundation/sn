package ctf

import (
	"encoding/binary"
	"errors"
	"math/rand/v2"
	"testing"

	"github.com/holiman/uint256"

	"github.com/urfoundation/sn/v2026/protocol"
)

// potDraw describes one pot of a synthetic draw.
type potDraw struct {
	country string
	role    Role
	units   int
	winners int
}

// drawWith returns a draw whose listed pots have the given units and
// winners; winner rank r of pot j is client 1000·j + r.
func drawWith(pots ...potDraw) *Draw {
	d := &Draw{}
	for _, p := range pots {
		c := cc(p.country)
		j := PotIndex(c.Index(), p.role)
		d.Units[j], d.Winners[j] = uint32(p.units), nil
		for r := 1; r <= p.winners; r++ {
			d.Winners[j] = append(d.Winners[j], Winner{Role: p.role, Country: c, Rank: r, Unit: unitHash(1000*j + r), ClientID: id(1000*j + r)})
		}
	}
	return d
}

// winnerKey is a winner's coldkey in the walk tests: its client's.
func winnerKey(w *Winner) [32]byte {
	return key(int(binary.BigEndian.Uint64(w.ClientID[8:])))
}

type walkEpoch struct {
	f        uint256.Int
	recycled uint256.Int
	draw     *Draw
}

// runWalk walks epochs from k = 8 over state, checking after each that the
// pots and pending awards are what is owed back plus the funding not yet
// paid (O = X + U), and returns each epoch and the deltas.
func runWalk(t *testing.T, state *State, epochs ...walkEpoch) ([]*EpochResult, []protocol.LedgerDelta, *walk) {
	t.Helper()
	w := newWalk(state)
	var results []*EpochResult
	for i := range epochs {
		e, err := w.step(uint64(8+i), &epochs[i].f, &epochs[i].recycled, epochs[i].draw, winnerKey)
		if err != nil {
			t.Fatalf("epoch %d: %v", 8+i, err)
		}
		checkConservation(t, w, e)
		results = append(results, e)
	}
	deltas, err := w.finish(carryKey)
	if err != nil {
		t.Fatal(err)
	}
	var sum uint256.Int
	for _, d := range deltas {
		sum.Add(&sum, &d.Delta)
	}
	if !sum.Eq(&w.funding) {
		t.Fatalf("deltas sum to %s, funding %s", sum.Dec(), w.funding.Dec())
	}
	return results, deltas, w
}

func checkConservation(t *testing.T, w *walk, e *EpochResult) {
	t.Helper()
	o := w.state.Obligations()
	var u, xu uint256.Int
	if w.paid.Gt(&w.funding) {
		t.Fatalf("epoch %d paid %s of funding %s", e.Epoch, w.paid.Dec(), w.funding.Dec())
	}
	u.Sub(&w.funding, &w.paid)
	xu.Add(&w.owed, &u)
	if !o.Eq(&xu) {
		t.Fatalf("epoch %d: obligations %s, owed %s + unpaid %s", e.Epoch, o.Dec(), w.owed.Dec(), u.Dec())
	}
	var potsOut uint256.Int
	for j := range e.Pots {
		p := &e.Pots[j]
		if !p.Out.Lt(amountLimit) || !p.In.Lt(amountLimit) {
			t.Fatalf("epoch %d pot %d wrapped", e.Epoch, j)
		}
		potsOut.Add(&potsOut, &p.Out)
	}
	if !potsOut.Eq(&e.Carried) {
		t.Fatal("carried is not the pots out")
	}
	var slots uint256.Int
	for s := range NumSlots {
		a := e.SlotAmount(s)
		slots.Add(&slots, &a)
	}
	if !slots.Eq(&e.Fresh) {
		t.Fatalf("epoch %d: slots sum to %s, fresh %s", e.Epoch, slots.Dec(), e.Fresh.Dec())
	}
}

func deltaOf(deltas []protocol.LedgerDelta, coldkey [32]byte) uint256.Int {
	for _, d := range deltas {
		if d.Coldkey == coldkey {
			return d.Delta
		}
	}
	return uint256.Int{}
}

func u(v uint64) uint256.Int { return rao(v) }

func sum(values ...uint256.Int) uint256.Int {
	var s uint256.Int
	for i := range values {
		s.Add(&s, &values[i])
	}
	return s
}

// §4.5: a slot's amount is floor(F/2490), and the remainder goes one rao each
// to the first slots in slot order; the slots sum to F.
func TestSlotAmounts(t *testing.T) {
	for _, fresh := range []uint64{0, 1, 2489, 2490, 2491, 2490*48_025_660 + 172, 1<<63 + 12345} {
		results, _, _ := runWalk(t, &State{}, walkEpoch{f: u(fresh)})
		e := results[0]
		base, rem := fresh/NumSlots, fresh%NumSlots
		if !e.Fresh.Eq(uint256.NewInt(fresh)) || e.SlotBase.Uint64() != base || e.SlotRemainder != rem {
			t.Fatalf("fresh %d: base %s remainder %d", fresh, e.SlotBase.Dec(), e.SlotRemainder)
		}
		for s := range NumSlots {
			want := base
			if uint64(s) < rem {
				want++
			}
			if a := e.SlotAmount(s); a.Uint64() != want {
				t.Fatalf("fresh %d slot %d: %s, want %d", fresh, s, a.Dec(), want)
			}
		}
		// No census: every slot joins its pot.
		for j := range e.Pots {
			p := &e.Pots[j]
			want := sum(e.SlotAmount(j*Ranks), e.SlotAmount(j*Ranks+1), e.SlotAmount(j*Ranks+2), e.SlotAmount(j*Ranks+3), e.SlotAmount(j*Ranks+4))
			if !p.In.IsZero() || !p.Out.Eq(&want) || p.Units != 0 || p.Winners != 0 {
				t.Fatalf("fresh %d pot %d: %+v", fresh, j, p)
			}
		}
		if len(e.Awards) != 0 || e.Drawn {
			t.Fatal("awards without a draw")
		}
	}
}

// §4.5: each winner takes its slot and q = floor(P/5); five winners empty the
// pot, the remainder rao going to the best ranks; fewer leave P − W·q, and the
// unfilled slots join the pot.
func TestFifthOfPot(t *testing.T) {
	const a = 1_000
	for _, pot := range []uint64{0, 1, 4, 5, 9, 1_003, 1_000_004} {
		for winners := 0; winners <= Ranks; winners++ {
			state := &State{}
			j := PotIndex(cc("lk").Index(), RoleExtender)
			state.Pots[j] = u(pot)
			// The carry is back: everything owed is in f.
			f := u(pot + a*NumSlots)
			results, deltas, _ := runWalk(t, state, walkEpoch{f: f, recycled: u(pot), draw: drawWith(potDraw{"lk", RoleExtender, winners + 2, winners})})
			e := results[0]
			if !e.Matured || e.Fresh.Uint64() != a*NumSlots {
				t.Fatalf("pot %d: matured %t fresh %s", pot, e.Matured, e.Fresh.Dec())
			}
			q, r := pot/5, pot%5
			var shares uint64
			for i, award := range e.Awards {
				want := q
				if winners == Ranks && uint64(i+1) <= r {
					want++
				}
				shares += want
				if award.Pot.Uint64() != want || award.Slot.Uint64() != a || award.Deferred || award.Rank != i+1 {
					t.Fatalf("pot %d, %d winners: rank %d got %+v", pot, winners, i+1, award)
				}
				var paid uint256.Int
				paid.AddUint64(&award.Pot, a)
				if got := deltaOf(deltas, award.Coldkey); !got.Eq(&paid) {
					t.Fatalf("rank %d paid %s", i+1, got.Dec())
				}
			}
			want := pot - uint64(winners)*q + uint64(Ranks-winners)*a
			if winners == Ranks {
				want = 0
			}
			p := e.Pots[j]
			if len(e.Awards) != winners || shares != pot-want+uint64(Ranks-winners)*a || p.Out.Uint64() != want || p.In.Uint64() != pot || p.Winners != winners || p.Units != uint32(winners+2) {
				t.Fatalf("pot %d, %d winners: pot %+v", pot, winners, p)
			}
		}
	}
}

// §4.5, §4.7: an epoch whose carry is not back pays slot amounts now and the
// pot shares as pending awards; the first matured epoch pays every pending
// award in full, with or without a draw.
func TestPendingAwards(t *testing.T) {
	state := &State{}
	tm := PotIndex(cc("tm").Index(), RoleProvider)
	is := PotIndex(cc("is").Index(), RoleProvider)
	state.Pots[tm], state.Pots[is] = u(1_000_000), u(999)
	owed := state.Obligations()
	fresh := u(NumSlots * 10)
	results, deltas, w := runWalk(t, state,
		// 8: the carry is late.
		walkEpoch{f: fresh, draw: drawWith(potDraw{"tm", RoleProvider, 1, 1}, potDraw{"is", RoleProvider, 5, 5})},
		// 9: still late, and no census: no draw.
		walkEpoch{f: fresh},
		// 10: the carry is back, with no census.
		walkEpoch{f: sum(fresh, owed), recycled: owed},
	)
	late := results[0]
	if late.Matured || !late.Credited.IsZero() || !late.Fresh.Eq(&fresh) {
		t.Fatalf("late epoch: matured %t credited %s fresh %s", late.Matured, late.Credited.Dec(), late.Fresh.Dec())
	}
	var created uint64
	for _, a := range late.Awards {
		if a.Slot.Uint64() != 10 || (a.Pot.IsZero() == a.Deferred) {
			t.Fatalf("late award %+v", a)
		}
		created += a.Pot.Uint64()
	}
	if len(late.Pending) != 6 || late.PendingTotal.Uint64() != created || created != 200_000+999 {
		t.Fatalf("pending %v total %s", late.Pending, late.PendingTotal.Dec())
	}
	// Five winners empty IS's pot: 200 to ranks 1 to 4, 199 to rank 5.
	if late.Pots[tm].Out.Uint64() != 800_000+40 || !late.Pots[is].Out.IsZero() || late.Awards[1].Pot.Uint64() != 200 || late.Awards[4].Pot.Uint64() != 199 || late.Awards[5].Pot.Uint64() != 200_000 {
		t.Fatalf("late pots %s %s", late.Pots[tm].Out.Dec(), late.Pots[is].Out.Dec())
	}
	if results[1].Matured || len(results[1].Pending) != 6 || !results[1].PaidPending.IsZero() {
		t.Fatal("epoch 9 paid pending awards")
	}
	back := results[2]
	if !back.Matured || len(back.Pending) != 0 || back.PaidPending.Uint64() != created || !back.Fresh.Eq(&fresh) {
		t.Fatalf("epoch 10: matured %t pending %v paid %s fresh %s", back.Matured, back.Pending, back.PaidPending.Dec(), back.Fresh.Dec())
	}
	if !w.owed.IsZero() {
		t.Fatal("still owed")
	}
	// TM's winner got its slot in 8 and its fifth in 10.
	if got := deltaOf(deltas, key(1000*tm+1)); got.Uint64() != 10+200_000 {
		t.Fatalf("TM's winner got %s", got.Dec())
	}
}

// §4.6, §4.7: a missed close defers the epoch with f = 0. Its recorded
// return rolls to the next captured epoch; its draw still runs, with zero
// slot amounts and, unmatured, pot shares pending.
func TestDeferredEpoch(t *testing.T) {
	state := &State{}
	us := PotIndex(cc("us").Index(), RoleExtender)
	state.Pots[us] = u(5_000)
	owed := state.Obligations()
	fresh := u(NumSlots * 3)
	results, _, w := runWalk(t, state,
		walkEpoch{recycled: owed, draw: drawWith(potDraw{"us", RoleExtender, 2, 2})},
		walkEpoch{f: sum(owed, fresh, fresh), draw: drawWith(potDraw{"us", RoleExtender, 2, 2})},
	)
	deferred := results[0]
	if deferred.Matured || !deferred.Returned.IsZero() || !deferred.Fresh.IsZero() || len(deferred.Pending) != 2 {
		t.Fatalf("deferred: %+v", deferred)
	}
	for _, a := range deferred.Awards {
		if !a.Slot.IsZero() || a.Pot.Uint64() != 1_000 || !a.Deferred {
			t.Fatalf("deferred award %+v", a)
		}
	}
	captured := results[1]
	if !captured.Matured || !captured.Returned.Eq(&owed) || captured.Fresh.Uint64() != 2*NumSlots*3 || captured.PaidPending.Uint64() != 2_000 {
		t.Fatalf("captured: matured %t returned %s fresh %s paid pending %s", captured.Matured, captured.Returned.Dec(), captured.Fresh.Dec(), captured.PaidPending.Dec())
	}
	if !w.owed.IsZero() || !w.rolled.IsZero() {
		t.Fatal("the rolled return was not credited")
	}

	// A return cannot roll past the root's last epoch: vault2 captures it.
	w = newWalk(state)
	if _, err := w.step(8, &uint256.Int{}, &owed, nil, winnerKey); err != nil {
		t.Fatal(err)
	}
	if _, err := w.finish(carryKey); !errors.Is(err, ErrReturnRolls) {
		t.Fatalf("rolled past the last epoch: %v", err)
	}
}

// §4.6: stake beyond what is owed is a donation and fresh; a partial return
// writes off up to CarryTolerance and stays unmatured; a return within
// CarryTolerance of what is owed matures the epoch, and when the funding does
// not cover it the shortfall comes from the largest pot, so no amount is ever
// negative.
func TestCarryArithmetic(t *testing.T) {
	big := PotIndex(cc("aq").Index(), RoleProvider)
	small := PotIndex(cc("de").Index(), RoleProvider)
	start := func() *State {
		s := &State{}
		s.Pots[big], s.Pots[small] = u(900), u(100)
		return s
	}
	owed := u(1_000)
	t.Run("donation", func(t *testing.T) {
		results, _, _ := runWalk(t, start(), walkEpoch{f: u(1_000 + 2_490 + 77), recycled: u(1_077)})
		e := results[0]
		if !e.Matured || !e.Returned.Eq(&owed) || e.Fresh.Uint64() != 2_490+77 {
			t.Fatalf("returned %s fresh %s", e.Returned.Dec(), e.Fresh.Dec())
		}
	})
	t.Run("partial return", func(t *testing.T) {
		results, _, w := runWalk(t, start(),
			walkEpoch{f: u(600 + 2_490), recycled: u(600)},
			walkEpoch{f: u(392 + 2_490), recycled: u(400)},
		)
		e := results[0]
		if e.Matured || e.Credited.Uint64() != 608 || e.Fresh.Uint64() != 2_482 || !e.Owed.Eq(&owed) {
			t.Fatalf("partial: matured %t credited %s fresh %s", e.Matured, e.Credited.Dec(), e.Fresh.Dec())
		}
		// The rest comes back: 392 are owed, 400 came, 8 of them fresh.
		e = results[1]
		if !e.Matured || e.Owed.Uint64() != 392 || e.Returned.Uint64() != 392 || e.Fresh.Uint64() != 2_490 || !w.owed.IsZero() {
			t.Fatalf("rest: matured %t owed %s returned %s fresh %s", e.Matured, e.Owed.Dec(), e.Returned.Dec(), e.Fresh.Dec())
		}
	})
	t.Run("rounding without fresh funding", func(t *testing.T) {
		// No emission: three moves lost 3 rao and the capture 1.
		results, _, _ := runWalk(t, start(), walkEpoch{f: u(996), recycled: u(997), draw: drawWith(potDraw{"de", RoleProvider, 1, 1})})
		e := results[0]
		if !e.Matured || !e.Credited.Eq(&owed) || !e.Fresh.IsZero() || e.Shortfall.Uint64() != 4 {
			t.Fatalf("matured %t credited %s fresh %s shortfall %s", e.Matured, e.Credited.Dec(), e.Fresh.Dec(), e.Shortfall.Dec())
		}
		if e.Pots[big].In.Uint64() != 896 || e.Pots[small].In.Uint64() != 100 || e.Awards[0].Pot.Uint64() != 20 {
			t.Fatalf("pots in %s %s, award %+v", e.Pots[big].In.Dec(), e.Pots[small].In.Dec(), e.Awards[0])
		}
	})
	t.Run("short beyond rounding", func(t *testing.T) {
		results, _, _ := runWalk(t, start(), walkEpoch{f: u(991), recycled: u(991)})
		if e := results[0]; e.Matured || e.Credited.Uint64() != 999 || !e.Fresh.IsZero() || e.Shortfall.Uint64() != 8 {
			t.Fatalf("matured %t credited %s shortfall %s", e.Matured, e.Credited.Dec(), e.Shortfall.Dec())
		}
	})
	t.Run("nothing owed", func(t *testing.T) {
		results, _, _ := runWalk(t, &State{}, walkEpoch{f: u(5), recycled: u(5)})
		if e := results[0]; !e.Matured || !e.Returned.IsZero() || e.Fresh.Uint64() != 5 {
			t.Fatalf("matured %t returned %s fresh %s", e.Matured, e.Returned.Dec(), e.Fresh.Dec())
		}
	})
	t.Run("shortfall from pending awards", func(t *testing.T) {
		s := &State{Pending: []PendingAward{{Epoch: 7, Role: RoleProvider, Country: cc("de"), Rank: 1, Coldkey: key(1), Amount: u(3)}}}
		s.Pots[small] = u(1)
		w := newWalk(s)
		e, err := w.step(8, &uint256.Int{}, &uint256.Int{}, nil, winnerKey)
		if err != nil || !e.Matured || e.Shortfall.Uint64() != 4 || e.PaidPending.Uint64() != 0 || !e.Pots[small].In.IsZero() {
			t.Fatalf("%v: %+v", err, e)
		}
	})
}

// Conservation, over random roots: in every epoch the slots sum to F_k, the
// pots and pending awards are what is owed back plus the funding not yet
// paid, nothing wraps, and the deltas, the carry's included, sum to the
// funding.
func TestWalkConservation(t *testing.T) {
	random := rand.New(rand.NewPCG(7, 11))
	for trial := range 300 {
		state := &State{}
		for range random.IntN(20) {
			state.Pots[random.IntN(NumPots)] = u(random.Uint64N(1 << 40))
		}
		for i := range random.IntN(4) {
			state.Pending = append(state.Pending, PendingAward{Epoch: 7, Role: RoleExtender, Country: CountryAt(i), Rank: 1, Coldkey: key(i + 1), Amount: u(1 + random.Uint64N(1<<30))})
		}
		owed := state.Obligations()
		var epochs []walkEpoch
		for range 1 + random.IntN(6) {
			var e walkEpoch
			switch random.IntN(5) {
			case 0: // deferred
				e.recycled = u(random.Uint64N(2) * owed.Uint64())
			case 1: // the carry back, with rounding and maybe no emission
				lost := random.Uint64N(6)
				back := owed.Uint64() - min(owed.Uint64(), lost)
				e.recycled = u(back)
				e.f = u(back - min(back, random.Uint64N(2)) + random.Uint64N(2)*random.Uint64N(1<<36))
			default:
				e.recycled = u(random.Uint64N(1 + owed.Uint64()))
				e.f = u(e.recycled.Uint64() + random.Uint64N(1<<36))
			}
			if random.IntN(4) > 0 {
				var pots []potDraw
				for range random.IntN(30) {
					units := random.IntN(9)
					pots = append(pots, potDraw{CountryAt(random.IntN(NumCountries)).String(), Role(1 + random.IntN(2)), units, min(units, Ranks)})
				}
				e.draw = drawWith(pots...)
			}
			epochs = append(epochs, e)
		}
		// A return never rolls past the last epoch.
		if last := &epochs[len(epochs)-1]; last.f.IsZero() {
			last.f = u(1)
		}
		t.Run("", func(t *testing.T) {
			_ = trial
			runWalk(t, state, epochs...)
		})
	}
}

// §4.6 across roots: one-epoch roots, the carry account's delta claimed and
// returned the next epoch through its own recycle, every recycle losing
// CarryRounding rao (the bound) and every capture one more, and the carry
// late for one, two and three epochs in a row, so that up to four cycles come
// back in one epoch. Rounding left owed shrinks by CarryRounding an epoch, so
// every epoch from the fourth after a late spell is matured with no pending
// awards, and the funding is conserved root by root. With a tolerance of one
// cycle's rounding, the residual would never shrink.
func TestCarryAcrossRoots(t *testing.T) {
	state := &State{}
	var queue []uint256.Int
	emission := u(NumSlots * 1_000)
	draw := drawWith(potDraw{"tm", RoleProvider, 1, 1}, potDraw{"is", RoleExtender, 3, 3})
	late := map[uint64]bool{3: true, 11: true, 12: true, 20: true, 21: true, 22: true}
	for k := uint64(1); k <= 60; k++ {
		var recycled uint256.Int
		if !late[k] {
			for _, d := range queue {
				if d.GtUint64(CarryRounding) {
					d.SubUint64(&d, CarryRounding)
					recycled.Add(&recycled, &d)
				}
			}
			queue = nil
		}
		f := sum(recycled, emission)
		if !recycled.IsZero() {
			f.SubUint64(&f, 1)
		}
		w := newWalk(state)
		e, err := w.step(k, &f, &recycled, draw, winnerKey)
		if err != nil {
			t.Fatalf("epoch %d: %v", k, err)
		}
		checkConservation(t, w, e)
		deltas, err := w.finish(carryKey)
		if err != nil {
			t.Fatal(err)
		}
		queue = append(queue, deltaOf(deltas, carryKey))
		state = &State{Pots: w.state.Pots, Pending: w.state.Pending}
		if !late[k] && !late[k-1] && !late[k-2] && !late[k-3] && (!e.Matured || len(e.Pending) != 0) {
			t.Fatalf("epoch %d, after the late spell: matured %t, %d pending, owed %s, returned %s", k, e.Matured, len(e.Pending), e.Owed.Dec(), e.Returned.Dec())
		}
	}
	if len(state.Pending) != 0 {
		t.Fatalf("%d pending awards at the end", len(state.Pending))
	}
}
