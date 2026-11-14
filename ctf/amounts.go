package ctf

import (
	"bytes"
	"errors"
	"fmt"
	"slices"

	"github.com/holiman/uint256"

	"github.com/urfoundation/sn/ledger"
	"github.com/urfoundation/sn/protocol"
)

const (
	// CarryRounding is the rao of rounding that one carry cycle may lose
	// (§4.6: a claim, two stake moves and the capture, at most a rao each).
	CarryRounding = 4
	// CarryTolerance is what an epoch with a return writes off of what is
	// still owed: the rounding of two cycles, since a late root's return and
	// the next root's can come back in one epoch. An epoch whose return is
	// within CarryTolerance of what is owed is matured (§4.5). Because it
	// exceeds one cycle's rounding, rounding left owed shrinks by at least
	// CarryRounding with every single return and never grows with two, so it
	// cannot keep later epochs unmatured.
	CarryTolerance = 2 * CarryRounding
)

var (
	// ErrReturnRolls is returned for returned carry recorded for the root's
	// last covered epoch when that epoch funded nothing: it would roll past
	// the root, which no rule can allocate. STSettlement2's commit window
	// makes it impossible, since a root's last epoch is captured before its
	// root is committed.
	ErrReturnRolls = errors.New("ctf: returned carry rolls past the root's last epoch")

	five      = uint256.NewInt(5)
	slotCount = uint256.NewInt(NumSlots)
	errWalk   = errors.New("ctf: a shortfall exceeds the pots and pending awards")
)

// Award is a winner's award (§4.5): its slot amount, paid now, and its share
// of the pot carried into its country and role, paid now when the epoch is
// matured and otherwise a pending award.
type Award struct {
	Role    Role
	Country Country
	// Rank is 1 to 5, the winning unit's rank in its draw.
	Rank     int
	Unit     [32]byte
	ClientID [16]byte
	// Coldkey is the payout coldkey that the provider pool's wallet selection
	// gives ClientID (§3.2), operator-attested.
	Coldkey [32]byte
	Slot    uint256.Int
	Pot     uint256.Int
	// Deferred reports that Pot is a pending award: the epoch is not matured
	// and the share is positive.
	Deferred bool
}

// Pot is a country and role's pot in an epoch (§4.5).
type Pot struct {
	Country Country
	Role    Role
	// Units is |U_{c,r}|, the units in the draw; Winners is W = min(5, Units).
	// Both are 0 for an epoch without a draw.
	Units   uint32
	Winners int
	// In is the pot carried in, less any shortfall taken from it; Out is what
	// carries on: In − W·q − (W = 5 ? In mod 5 : 0) + the unfilled slots.
	In  uint256.Int
	Out uint256.Int
}

// PendingAward is a winner's pot share that waits for the carry's return
// (§4.5), paid in full in the first matured epoch.
type PendingAward struct {
	// Epoch is the epoch that awarded it.
	Epoch   uint64
	Role    Role
	Country Country
	Rank    int
	Coldkey [32]byte
	Amount  uint256.Int
}

// State is what the CTF pool owes after an epoch: its pots, in pot order,
// and its pending awards, by awarding epoch, role, country and rank. The
// last covered epoch's pot rows (pot out) and pending rows are a root's
// state, from which the next root's walk starts.
type State struct {
	Pots    [NumPots]uint256.Int
	Pending []PendingAward
}

// Obligations returns O, the pots and pending awards together.
func (s *State) Obligations() uint256.Int {
	var o uint256.Int
	for i := range s.Pots {
		o.Add(&o, &s.Pots[i])
	}
	for i := range s.Pending {
		o.Add(&o, &s.Pending[i].Amount)
	}
	return o
}

// EpochResult is what one covered epoch contributed to a root: its census,
// its draw, the walk's amounts and its objects. It is the report the
// operator's API and the audit read.
type EpochResult struct {
	Epoch uint64
	// Census is the committed census hash, nil when none was committed, and
	// CensusBlock its block B_k. CensusErr says why a committed census is not
	// canonical; such a census gives no draw.
	Census      *ledger.Ref
	CensusBlock uint64
	CensusErr   error
	// Candidates is the census's row count, and Excluded and ExcusedSamples
	// its exclusion counts and excused samples, for a canonical census.
	Candidates     int
	Excluded       []Exclusion
	ExcusedSamples []uint16
	// Drawn reports a draw, from a canonical committed census. Round is ρ_k,
	// Randomness rand_k and Seed σ_k, all zero without a draw; Bound lists
	// the head-bound candidates the draw left out.
	Drawn      bool
	Round      uint64
	Randomness [32]byte
	Seed       [32]byte
	Bound      []Candidate

	// Funding is f_k and Recycled the carry contract's recycledFor[k].
	Funding  uint256.Int
	Recycled uint256.Int
	// Owed is X_k, the carry still owed back before k's capture.
	Owed uint256.Int
	// Returned is R_k: the returned carry captured in k, recycledFor[k] and
	// the returns of the uncaptured epochs before it, never more than Owed.
	Returned uint256.Int
	// Credited is what k counts as returned of what was owed: Owed when
	// matured, Returned + CarryTolerance (below Owed) for a partial return,
	// else zero.
	Credited uint256.Int
	// Matured is M_k: Returned + CarryTolerance >= Owed.
	Matured bool
	// Fresh is F_k = Funding − Credited, or 0 when Credited exceeds Funding;
	// Shortfall is then Credited − Funding, taken from the pots.
	Fresh     uint256.Int
	Shortfall uint256.Int
	// SlotBase is floor(F_k / S), and the first SlotRemainder slots in slot
	// order get one rao more.
	SlotBase      uint256.Int
	SlotRemainder uint64

	// Awards are by role, country and rank; Pots are the 498 pots in pot
	// order; Pending is the pending awards outstanding after k.
	Awards  []Award
	Pots    []Pot
	Pending []PendingAward
	// PaidSlots, PaidPots and PaidPending are what k paid now: slot
	// amounts, pot shares, and pending awards of earlier epochs.
	PaidSlots   uint256.Int
	PaidPots    uint256.Int
	PaidPending uint256.Int
	// PendingTotal is the sum of Pending, and Carried the sum of the pots out.
	PendingTotal uint256.Int
	Carried      uint256.Int

	// AwardsRef, PotsRef and PendingRef name the epoch's objects.
	AwardsRef  ledger.Ref
	PotsRef    ledger.Ref
	PendingRef ledger.Ref
}

// Paid returns everything k paid now.
func (e *EpochResult) Paid() uint256.Int {
	var paid uint256.Int
	paid.Add(&e.PaidSlots, &e.PaidPots)
	paid.Add(&paid, &e.PaidPending)
	return paid
}

// SlotAmount returns a_{k,s}, slot s's amount in slot order: SlotBase, and
// one more rao for the first SlotRemainder slots.
func (e *EpochResult) SlotAmount(s int) uint256.Int {
	a := e.SlotBase
	if uint64(s) < e.SlotRemainder {
		a.AddUint64(&a, 1)
	}
	return a
}

// walk is the §4.5 walk over a root's covered epochs.
type walk struct {
	state State
	// owed is X: the carry the carry contract still owes back. It starts at
	// the previous root's obligations, which its walk allocated to the carry
	// account less what was still owed (§4.5): the carry account's cumulative
	// less everything returned and written off.
	owed uint256.Int
	// rolled is the returned carry of epochs without a capture, which the
	// next capture took (§4.6).
	rolled uint256.Int
	// funding and paid are Σ f_k and everything paid to winners.
	funding uint256.Int
	paid    uint256.Int
	deltas  map[[32]byte]*uint256.Int
}

func newWalk(state *State) *walk {
	w := &walk{deltas: map[[32]byte]*uint256.Int{}}
	w.state.Pots = state.Pots
	w.state.Pending = slices.Clone(state.Pending)
	w.owed = state.Obligations()
	return w
}

func (w *walk) pay(coldkey [32]byte, amount *uint256.Int) {
	if amount.IsZero() {
		return
	}
	d, ok := w.deltas[coldkey]
	if !ok {
		d = new(uint256.Int)
		w.deltas[coldkey] = d
	}
	d.Add(d, amount)
	w.paid.Add(&w.paid, amount)
}

// step walks epoch k with funding f, the carry contract's recycledFor[k],
// and its draw, nil without one. coldkey gives a winner's payout coldkey.
func (w *walk) step(k uint64, f, recycled *uint256.Int, draw *Draw, coldkey func(*Winner) [32]byte) (*EpochResult, error) {
	e := &EpochResult{Epoch: k, Funding: *f, Recycled: *recycled, Owed: w.owed}
	w.funding.Add(&w.funding, f)

	// A return recorded for an epoch without a capture (deferred) is
	// captured by the next close, so it rolls forward to that epoch.
	var returned uint256.Int
	returned.Add(&w.rolled, recycled)
	if f.IsZero() {
		w.rolled, returned = returned, uint256.Int{}
	} else {
		w.rolled.Clear()
	}
	// Stake beyond what is owed is a donation, and fresh.
	e.Returned = returned
	if e.Returned.Gt(&w.owed) {
		e.Returned = w.owed
	}
	var within uint256.Int
	within.AddUint64(&e.Returned, CarryTolerance)
	switch {
	case !within.Lt(&w.owed):
		e.Matured, e.Credited = true, w.owed
	case !e.Returned.IsZero():
		e.Credited = within
	}
	w.owed.Sub(&w.owed, &e.Credited)
	if !f.Lt(&e.Credited) {
		e.Fresh.Sub(f, &e.Credited)
	} else {
		e.Shortfall.Sub(&e.Credited, f)
		if err := w.charge(e.Shortfall); err != nil {
			return nil, err
		}
	}

	var remainder uint256.Int
	e.SlotBase.DivMod(&e.Fresh, slotCount, &remainder)
	e.SlotRemainder = remainder.Uint64()

	e.Pots = make([]Pot, NumPots)
	var created []PendingAward
	for role := RoleProvider; role <= RoleExtender; role++ {
		for c := range NumCountries {
			j := PotIndex(c, role)
			pot := &e.Pots[j]
			pot.Country, pot.Role, pot.In = CountryAt(c), role, w.state.Pots[j]
			var winners []Winner
			if draw != nil {
				winners, pot.Units = draw.Winners[j], draw.Units[j]
			}
			pot.Winners = len(winners)
			var q, r uint256.Int
			q.DivMod(&pot.In, five, &r)
			pot.Out = pot.In
			for i := range winners {
				win := &winners[i]
				a := Award{Role: role, Country: pot.Country, Rank: i + 1, Unit: win.Unit, ClientID: win.ClientID, Coldkey: coldkey(win), Slot: e.SlotAmount(j*Ranks + i), Pot: q}
				// Five winners empty the pot: its remainder rao go to the best
				// ranks.
				if len(winners) == Ranks && uint64(a.Rank) <= r.Uint64() {
					a.Pot.AddUint64(&a.Pot, 1)
				}
				pot.Out.Sub(&pot.Out, &a.Pot)
				w.pay(a.Coldkey, &a.Slot)
				e.PaidSlots.Add(&e.PaidSlots, &a.Slot)
				switch {
				case e.Matured:
					w.pay(a.Coldkey, &a.Pot)
					e.PaidPots.Add(&e.PaidPots, &a.Pot)
				case !a.Pot.IsZero():
					a.Deferred = true
					created = append(created, PendingAward{Epoch: k, Role: role, Country: pot.Country, Rank: a.Rank, Coldkey: a.Coldkey, Amount: a.Pot})
				}
				e.Awards = append(e.Awards, a)
			}
			// What is not won carries.
			for i := len(winners); i < Ranks; i++ {
				a := e.SlotAmount(j*Ranks + i)
				pot.Out.Add(&pot.Out, &a)
			}
			w.state.Pots[j] = pot.Out
			e.Carried.Add(&e.Carried, &pot.Out)
		}
	}
	if e.Matured {
		for i := range w.state.Pending {
			p := &w.state.Pending[i]
			w.pay(p.Coldkey, &p.Amount)
			e.PaidPending.Add(&e.PaidPending, &p.Amount)
		}
		w.state.Pending = nil
	} else {
		w.state.Pending = append(w.state.Pending, created...)
	}
	e.Pending = slices.Clone(w.state.Pending)
	for i := range e.Pending {
		e.PendingTotal.Add(&e.PendingTotal, &e.Pending[i].Amount)
	}
	return e, nil
}

// charge takes a shortfall from the pots, the largest first (ties to the
// earliest in pot order), and then from the pending awards in their order.
// The walk keeps the obligations at least what is owed, and a shortfall is at
// most what was owed, so they always cover it.
func (w *walk) charge(shortfall uint256.Int) error {
	left := shortfall
	for !left.IsZero() {
		j := -1
		for i := range w.state.Pots {
			if !w.state.Pots[i].IsZero() && (j < 0 || w.state.Pots[i].Gt(&w.state.Pots[j])) {
				j = i
			}
		}
		if j < 0 {
			break
		}
		take := left
		if take.Gt(&w.state.Pots[j]) {
			take = w.state.Pots[j]
		}
		w.state.Pots[j].Sub(&w.state.Pots[j], &take)
		left.Sub(&left, &take)
	}
	for i := 0; !left.IsZero() && i < len(w.state.Pending); i++ {
		p := &w.state.Pending[i]
		take := left
		if take.Gt(&p.Amount) {
			take = p.Amount
		}
		p.Amount.Sub(&p.Amount, &take)
		left.Sub(&left, &take)
	}
	w.state.Pending = slices.DeleteFunc(w.state.Pending, func(p PendingAward) bool { return p.Amount.IsZero() })
	if !left.IsZero() {
		return fmt.Errorf("%w: %s rao", errWalk, left.Dec())
	}
	return nil
}

// finish ends the walk: the carry account receives everything the walk did
// not pay (§4.5), so the root's total is the pool's funding through its
// epoch. It returns every coldkey's delta, ascending.
func (w *walk) finish(carry [32]byte) ([]protocol.LedgerDelta, error) {
	if !w.rolled.IsZero() {
		return nil, fmt.Errorf("%w: %s rao", ErrReturnRolls, w.rolled.Dec())
	}
	if w.paid.Gt(&w.funding) {
		return nil, fmt.Errorf("%w: paid %s of funding %s", errWalk, w.paid.Dec(), w.funding.Dec())
	}
	var rest uint256.Int
	rest.Sub(&w.funding, &w.paid)
	w.pay(carry, &rest)
	deltas := make([]protocol.LedgerDelta, 0, len(w.deltas))
	for coldkey, delta := range w.deltas {
		deltas = append(deltas, protocol.LedgerDelta{Coldkey: coldkey, Delta: *delta})
	}
	slices.SortFunc(deltas, func(a, b protocol.LedgerDelta) int { return bytes.Compare(a.Coldkey[:], b.Coldkey[:]) })
	return deltas, nil
}
