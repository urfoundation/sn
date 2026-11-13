package protocol

import (
	"bytes"
	"context"
	"errors"
	"sort"

	"github.com/holiman/uint256"
)

// LedgerAllocationRule names the settlement v2 per-epoch allocation
// (VERSION2-FINAL.md §6.3).
const LedgerAllocationRule = "coldkey_usage_largest_remainder_rao_v1"

var (
	// ErrNoEligibleUsage is returned when an epoch's eligible rows carry no
	// usage. §6.4 carries such an epoch's funding to the next allocated epoch.
	ErrNoEligibleUsage = errors.New("protocol: epoch has no eligible usage")
	// ErrLedgerUsageOverflow is returned when an epoch's eligible usage reaches 2^64.
	ErrLedgerUsageOverflow = errors.New("protocol: epoch usage reaches 2^64")
	// ErrLedgerAmountRange is returned for an amount at or above 2^128.
	ErrLedgerAmountRange = errors.New("protocol: allocation amount must be below 2^128")
)

var ledgerAmountLimit = new(uint256.Int).Lsh(uint256.NewInt(1), 128)

// LedgerUsageRow is one provider row of an epoch (§6.2) as the allocation
// reads it.
type LedgerUsageRow struct {
	ClientID     [16]byte
	Coldkey      [32]byte
	UsageBytes   uint64
	HeadExcluded bool
}

// LedgerDelta is one coldkey's share of an epoch's amount, in rao.
type LedgerDelta struct {
	Coldkey [32]byte
	Delta   uint256.Int
}

// AllocateLedgerEpoch splits amount among the eligible rows' coldkeys in
// proportion to their summed usage. A row is eligible when its coldkey is
// nonzero and it is not head-excluded. Coldkeys are aggregated first, so
// splitting a provider never changes its coldkey's amount. With W_c the
// coldkey's usage and ΣW their sum:
//
//	q_c = floor(A·W_c / ΣW), r_c = (A·W_c) mod ΣW, R = A − Σ q_c,
//
// and the R coldkeys with the largest r_c each get one more rao, ties going
// to the ascending coldkey. The result lists every coldkey with positive
// usage in ascending byte order, zero deltas included; the deltas sum to the
// amount exactly, and each is within one rao of the exact share.
func AllocateLedgerEpoch(ctx context.Context, amount *uint256.Int, rows []LedgerUsageRow) ([]LedgerDelta, error) {
	if ctx == nil {
		return nil, errors.New("allocation requires an owner context")
	}
	if !amount.Lt(ledgerAmountLimit) {
		return nil, ErrLedgerAmountRange
	}
	weights := map[[32]byte]uint64{}
	var total uint64
	for i := range rows {
		if i%65536 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		row := &rows[i]
		if row.Coldkey == ([32]byte{}) || row.HeadExcluded || row.UsageBytes == 0 {
			continue
		}
		next := total + row.UsageBytes
		if next < total {
			return nil, ErrLedgerUsageOverflow
		}
		total = next
		// A coldkey's weight never exceeds the total, so it cannot overflow.
		weights[row.Coldkey] += row.UsageBytes
	}
	if total == 0 {
		return nil, ErrNoEligibleUsage
	}
	out := make([]LedgerDelta, 0, len(weights))
	for coldkey := range weights {
		out = append(out, LedgerDelta{Coldkey: coldkey})
	}
	sort.Slice(out, func(i, j int) bool { return bytes.Compare(out[i].Coldkey[:], out[j].Coldkey[:]) < 0 })

	// A < 2^128 and W_c < 2^64, so A·W_c fits in 256 bits.
	den := uint256.NewInt(total)
	remainders := make([]uint256.Int, len(out))
	allocated := new(uint256.Int)
	for i := range out {
		if i%65536 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		num := new(uint256.Int).Mul(amount, uint256.NewInt(weights[out[i].Coldkey]))
		out[i].Delta.DivMod(num, den, &remainders[i])
		allocated.Add(allocated, &out[i].Delta)
	}
	// Σ r_c = R·ΣW and every r_c < ΣW, so R is below the coldkey count.
	remaining := new(uint256.Int).Sub(amount, allocated).Uint64()
	order := make([]int, len(out))
	for i := range order {
		order[i] = i
	}
	// out is in ascending coldkey order, so the position breaks remainder ties.
	sort.Slice(order, func(a, b int) bool {
		if c := remainders[order[a]].Cmp(&remainders[order[b]]); c != 0 {
			return c > 0
		}
		return order[a] < order[b]
	})
	for k := uint64(0); k < remaining; k++ {
		delta := &out[order[k]].Delta
		delta.AddUint64(delta, 1)
	}
	return out, nil
}
