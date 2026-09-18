package protocol

import (
	"bytes"
	"errors"
	"math/big"
	"sort"
)

var ErrNoEligibleProviders = errors.New("no eligible payout weight")

type ProviderAllocation struct {
	ClientID       [16]byte
	Coldkey        [32]byte
	UsageBytes     uint64
	ReliabilityPPM uint32
	Eligible       bool
	HeadExcluded   bool
}

type PayoutShare struct {
	ClientID [16]byte `json:"client_id"`
	Coldkey  [32]byte `json:"coldkey"`
	ShareBPS uint64   `json:"share_bps"`
}

type allocationRemainder struct {
	index     int
	remainder *big.Int
}

// AllocateShares implements the policy's exact largest-remainder allocation.
// Only eligible, non-head providers with a payout coldkey and positive
// usage*reliability enter the payout set. The result always sums to 10,000 bps.
func AllocateShares(in []ProviderAllocation) ([]PayoutShare, error) {
	type weighted struct {
		ProviderAllocation
		weight *big.Int
	}
	// Claims are keyed by coldkey in the immutable vault. Multiple logical
	// providers may intentionally share a payout coldkey, but emitting two
	// leaves for that coldkey would make the second leaf unclaimable. Aggregate
	// those providers first and retain the lexicographically smallest client id
	// as the deterministic allocation/tie-break identity.
	byColdkey := map[[32]byte]weighted{}
	total := new(big.Int)
	for _, p := range in {
		if !p.Eligible || p.HeadExcluded || p.Coldkey == ([32]byte{}) || p.UsageBytes == 0 || p.ReliabilityPPM == 0 {
			continue
		}
		w := new(big.Int).Mul(new(big.Int).SetUint64(p.UsageBytes), new(big.Int).SetUint64(uint64(p.ReliabilityPPM)))
		if w.Sign() == 0 {
			continue
		}
		if current, ok := byColdkey[p.Coldkey]; ok {
			current.weight.Add(current.weight, w)
			if bytes.Compare(p.ClientID[:], current.ClientID[:]) < 0 {
				current.ClientID = p.ClientID
			}
			byColdkey[p.Coldkey] = current
		} else {
			byColdkey[p.Coldkey] = weighted{ProviderAllocation: p, weight: new(big.Int).Set(w)}
		}
		total.Add(total, w)
	}
	eligible := make([]weighted, 0, len(byColdkey))
	for _, p := range byColdkey {
		eligible = append(eligible, p)
	}
	if len(eligible) == 0 || total.Sign() == 0 {
		return nil, ErrNoEligibleProviders
	}
	sort.SliceStable(eligible, func(i, j int) bool {
		if c := bytes.Compare(eligible[i].ClientID[:], eligible[j].ClientID[:]); c != 0 {
			return c < 0
		}
		return bytes.Compare(eligible[i].Coldkey[:], eligible[j].Coldkey[:]) < 0
	})
	out := make([]PayoutShare, len(eligible))
	remainders := make([]allocationRemainder, len(eligible))
	var allocated uint64
	den := new(big.Int).Set(total)
	for i, p := range eligible {
		num := new(big.Int).Mul(p.weight, big.NewInt(10_000))
		q, r := new(big.Int), new(big.Int)
		q.QuoRem(num, den, r)
		share := q.Uint64()
		allocated += share
		out[i] = PayoutShare{ClientID: p.ClientID, Coldkey: p.Coldkey, ShareBPS: share}
		remainders[i] = allocationRemainder{index: i, remainder: r}
	}
	sort.SliceStable(remainders, func(i, j int) bool {
		if c := remainders[i].remainder.Cmp(remainders[j].remainder); c != 0 {
			return c > 0
		}
		return bytes.Compare(out[remainders[i].index].ClientID[:], out[remainders[j].index].ClientID[:]) < 0
	})
	remaining := uint64(10_000) - allocated
	for i := uint64(0); i < remaining; i++ {
		out[remainders[i%uint64(len(remainders))].index].ShareBPS++
	}
	var sum uint64
	for _, share := range out {
		sum += share.ShareBPS
	}
	if sum != 10_000 {
		return nil, errors.New("internal payout allocation error")
	}
	return out, nil
}
