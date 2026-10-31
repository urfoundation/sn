package ledger

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/holiman/uint256"

	"github.com/urfoundation/sn/merkle"
	"github.com/urfoundation/sn/protocol"
)

var (
	// ErrCoveredEpochs is returned when the covered epochs are not exactly
	// p+1 … e, with p the previous finalized root's epoch, or firstEpoch − 1
	// for a pool's first root.
	ErrCoveredEpochs = errors.New("ledger: covered epochs are not p+1 … e")
	// ErrFunding is returned when vault2's fundedThrough is missing for a
	// covered epoch, decreases, or reaches 2^128. vault2 makes all three
	// impossible, so a verifier reports it as its caller's error.
	ErrFunding = errors.New("ledger: funding snapshots are missing, decreasing or not below 2^128")
)

// fundings returns f_k = fundedThrough[k] − fundedThrough[k−1] for k = p+1 …
// e, with fundedThrough[p] = base, and fundedThrough[e]. base is the previous
// finalized root's total, which vault2 committed as fundedThrough[p], or 0
// for a pool's first root (fundedThrough[firstEpoch − 1] = 0). The caller
// has checked 0 < e − p <= MaxCoveredEpochs.
func fundings(fundedThrough map[uint64]uint256.Int, p, e uint64, base *uint256.Int) ([]uint256.Int, uint256.Int, error) {
	f := make([]uint256.Int, e-p)
	last := *base
	for i := range f {
		k := p + 1 + uint64(i)
		next, ok := fundedThrough[k]
		if !ok {
			return nil, last, fmt.Errorf("%w: epoch %d has no snapshot", ErrFunding, k)
		}
		if next.Lt(&last) {
			return nil, last, fmt.Errorf("%w: epoch %d decreases", ErrFunding, k)
		}
		f[i].Sub(&next, &last)
		last = next
	}
	if !last.Lt(valueLimit) {
		return nil, last, fmt.Errorf("%w: epoch %d reaches 2^128", ErrFunding, e)
	}
	return f, last, nil
}

// walk is the §6.4 allocation walk over a root's covered epochs.
type walk struct {
	carry uint256.Int
	// allocated reports whether the latest epoch allocated.
	allocated bool
	// sum holds each coldkey's delta summed over the allocated epochs, in
	// ascending coldkey order.
	sum []protocol.LedgerDelta
}

// step walks epoch k: with rows of positive eligible usage it allocates f_k
// plus the carry (§6.3) and clears the carry; otherwise, for no rows, no
// eligible usage, or an unrecoverable epoch, it adds f_k to the carry.
func (w *walk) step(ctx context.Context, f *uint256.Int, rows []protocol.LedgerUsageRow, null bool) (uint256.Int, []protocol.LedgerDelta, error) {
	var amount uint256.Int
	amount.Add(&w.carry, f)
	w.allocated = false
	if !null {
		deltas, err := protocol.AllocateLedgerEpoch(ctx, &amount, rows)
		if err == nil {
			w.carry.Clear()
			w.allocated = true
			w.sum = addDeltas(w.sum, deltas)
			return amount, deltas, nil
		}
		if !errors.Is(err, protocol.ErrNoEligibleUsage) {
			return uint256.Int{}, nil, err
		}
	}
	w.carry = amount
	return uint256.Int{}, nil, nil
}

// recover allocates a recovery component's amount over its source epoch's
// rows (§6.3) and adds the deltas to the root's sums. It reports false, with
// no deltas, when the rows have no eligible usage, leaving the amount to the
// covered epoch's ordinary funding. The walk's carry is not touched.
func (w *walk) recover(ctx context.Context, amount *uint256.Int, rows []protocol.LedgerUsageRow) (bool, []protocol.LedgerDelta, error) {
	deltas, err := protocol.AllocateLedgerEpoch(ctx, amount, rows)
	if errors.Is(err, protocol.ErrNoEligibleUsage) {
		return false, nil, nil
	}
	if err != nil {
		return false, nil, err
	}
	w.sum = addDeltas(w.sum, deltas)
	return true, deltas, nil
}

// addDeltas merges two lists in ascending coldkey order, summing the deltas
// of a coldkey in both.
func addDeltas(a, b []protocol.LedgerDelta) []protocol.LedgerDelta {
	out := make([]protocol.LedgerDelta, 0, max(len(a), len(b)))
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		var c int
		switch {
		case i == len(a):
			c = 1
		case j == len(b):
			c = -1
		default:
			c = bytes.Compare(a[i].Coldkey[:], b[j].Coldkey[:])
		}
		switch {
		case c < 0:
			out = append(out, a[i])
			i++
		case c > 0:
			out = append(out, b[j])
			j++
		default:
			d := a[i]
			d.Delta.Add(&d.Delta, &b[j].Delta)
			out = append(out, d)
			i++
			j++
		}
	}
	return out
}

// coldkeyOrder returns the account positions in ascending coldkey order, and
// the position of a coldkey that appears twice, or −1.
func coldkeyOrder(accounts []merkle.LedgerAccount) ([]uint32, int) {
	order := make([]uint32, len(accounts))
	for i := range order {
		order[i] = uint32(i)
	}
	slices.SortFunc(order, func(a, b uint32) int {
		if c := bytes.Compare(accounts[a].Coldkey[:], accounts[b].Coldkey[:]); c != 0 {
			return c
		}
		return cmp.Compare(a, b)
	})
	for i := 1; i < len(order); i++ {
		if accounts[order[i-1]].Coldkey == accounts[order[i]].Coldkey {
			return order, int(order[i])
		}
	}
	return order, -1
}
