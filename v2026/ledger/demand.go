package ledger

import (
	"errors"
	"fmt"
	"io"
	"math/bits"
	"slices"

	"github.com/holiman/uint256"

	"github.com/urfoundation/sn/v2026/merkle"
)

// DemandSample is the size of the §9.3 value-weighted sample of the
// previous root's accounts before any doubling.
const DemandSample = 256

// ErrDemandIndex is returned by DemandSet for a fault or an earlier
// unanswered index that is not an account of the given root.
var ErrDemandIndex = errors.New("ledger: demand index is not an account of the previous root")

// DemandSet returns the indexes of a finalized root's accounts, previous, to
// demand on a pending root whose objects are missing or do not reproduce it
// (§9.3, the residual risk of §4.2 G2), each once, in the order to send
// them. previous is the newest finalized root whose accounts the caller
// holds and verified:
//
//   - first a sample of DemandSample << doublings of previous's accounts,
//     all of them when it holds fewer, where doublings counts the earlier
//     pending roots over the latest finalized root that were replaced or
//     deleted while a demand was open. It is drawn by value without
//     replacement over every account, faulted or demanded before included,
//     in the order drawn. An account of zero value holds nothing to protect
//     and is never drawn: the sample ends when only such accounts remain;
//   - then every index in again not drawn, ascending: the unanswered demands
//     of those earlier roots;
//   - then every other fault, ascending: an index whose published row is
//     below the account's value, names another coldkey or is missing
//     (Faults, Result.Faults).
//
// The sample comes first, in draw order, so that the demands sent before the
// challenge window closes or a fee budget runs out are a value-weighted
// sample whatever else the set holds: a theft of a fraction f of previous's
// value escapes the first k demands, k at most the sample's size, with
// probability at most (1−f)^k. Published rows that fault every account, and
// demands anyone left unanswered on an earlier root, can neither shrink the
// sample nor move it back.
//
// The sample reads its randomness from random, which must be a CSPRNG read
// after the pending root was committed (crypto/rand.Reader), so that the
// operator cannot predict it. The result depends only on previous,
// doublings, the faulted and again indexes as sets, and the bytes read.
func DemandSet(previous []merkle.LedgerAccount, faults []Fault, doublings uint, again []uint64, random io.Reader) ([]uint64, error) {
	n := uint64(len(previous))
	faulted := make([]uint64, len(faults))
	for k := range faults {
		faulted[k] = faults[k].Index
	}
	for _, i := range slices.Concat(again, faulted) {
		if i >= n {
			return nil, fmt.Errorf("%w: %d of %d", ErrDemandIndex, i, n)
		}
	}
	size := n
	if doublings < 32 {
		size = min(n, DemandSample<<doublings)
	}
	demanded := make([]bool, n)
	var set []uint64
	weights := newWeightTree(previous)
	for uint64(len(set)) < size && !weights.total.IsZero() {
		r, err := uniform(random, &weights.total)
		if err != nil {
			return nil, fmt.Errorf("ledger: demand sample randomness: %w", err)
		}
		i := weights.find(r)
		weights.remove(i, &previous[i].Cumulative)
		demanded[i] = true
		set = append(set, uint64(i))
	}
	for _, forced := range [][]uint64{again, faulted} {
		for _, i := range slices.Sorted(slices.Values(forced)) {
			if !demanded[i] {
				demanded[i] = true
				set = append(set, i)
			}
		}
	}
	return set, nil
}

// weightTree is a Fenwick tree over account values, for drawing accounts
// by value without replacement.
type weightTree struct {
	// node[k], 1-based, sums the values of positions k − lowbit(k) … k − 1.
	node  []uint256.Int
	total uint256.Int
}

// newWeightTree holds every account's value.
func newWeightTree(accounts []merkle.LedgerAccount) *weightTree {
	t := &weightTree{node: make([]uint256.Int, len(accounts)+1)}
	for i := range accounts {
		k := i + 1
		t.node[k].Add(&t.node[k], &accounts[i].Cumulative)
		t.total.Add(&t.total, &accounts[i].Cumulative)
		if parent := k + k&-k; parent < len(t.node) {
			t.node[parent].Add(&t.node[parent], &t.node[k])
		}
	}
	return t
}

// find returns the position whose value interval holds r, for r below the
// total: the first position whose prefix sum exceeds r.
func (t *weightTree) find(r uint256.Int) int {
	position := 0
	for step := 1 << (bits.Len(uint(len(t.node)-1)) - 1); step > 0; step >>= 1 {
		if next := position + step; next < len(t.node) && !t.node[next].Gt(&r) {
			position = next
			r.Sub(&r, &t.node[next])
		}
	}
	return position
}

// remove takes value off position i.
func (t *weightTree) remove(i int, value *uint256.Int) {
	for k := i + 1; k < len(t.node); k += k & -k {
		t.node[k].Sub(&t.node[k], value)
	}
	t.total.Sub(&t.total, value)
}

// uniform returns a uniform value below a positive bound, by rejection.
func uniform(random io.Reader, bound *uint256.Int) (uint256.Int, error) {
	n := bound.BitLen()
	buf := make([]byte, (n+7)/8)
	for {
		var r uint256.Int
		if _, err := io.ReadFull(random, buf); err != nil {
			return r, err
		}
		buf[0] &= 0xff >> (8*len(buf) - n)
		r.SetBytes(buf)
		if r.Lt(bound) {
			return r, nil
		}
	}
}
