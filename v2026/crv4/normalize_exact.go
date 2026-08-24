package crv4

import (
	"errors"
	"fmt"
	"math/big"
	"sort"
)

// ApplyMaxWeightLimitRational is the exact integer/rational variant of the
// SDK's water-filling cap. It never converts through float64, so independent
// validators with the same inputs produce identical commit bytes.
func ApplyMaxWeightLimitRational(weights []*big.Rat, maxWeightLimit uint16) ([]*big.Rat, error) {
	if maxWeightLimit == 0 {
		return nil, errors.New("crv4: max weight limit must be > 0")
	}
	out := make([]*big.Rat, len(weights))
	total := new(big.Rat)
	max := new(big.Rat)
	positive := 0
	for i, weight := range weights {
		if weight == nil || weight.Sign() < 0 {
			return nil, errors.New("crv4: rational weights must be non-nil and non-negative")
		}
		out[i] = new(big.Rat).Set(weight)
		total.Add(total, weight)
		if weight.Sign() > 0 {
			positive++
		}
		if weight.Cmp(max) > 0 {
			max.Set(weight)
		}
	}
	if total.Sign() == 0 || maxWeightLimit == U16Max {
		return out, nil
	}
	limit := new(big.Rat).SetFrac(big.NewInt(int64(maxWeightLimit)), big.NewInt(U16Max))
	if new(big.Rat).Quo(max, total).Cmp(limit) <= 0 {
		return out, nil
	}
	if positive == 0 || new(big.Int).Mul(big.NewInt(int64(maxWeightLimit)), big.NewInt(int64(positive))).Cmp(big.NewInt(U16Max)) < 0 {
		return nil, fmt.Errorf("crv4: max weight limit %d is infeasible for %d positive weights", maxWeightLimit, positive)
	}

	sorted := make([]*big.Rat, 0, positive)
	for _, weight := range out {
		if weight.Sign() > 0 {
			sorted = append(sorted, new(big.Rat).Set(weight))
		}
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Cmp(sorted[j]) < 0 })
	prefix := new(big.Rat)
	var capValue *big.Rat
	for lowCount := 0; lowCount < len(sorted); lowCount++ {
		highCount := len(sorted) - lowCount
		// c = L*S/(1-L*k), where S is the sum below c and k values
		// are clipped to c.
		den := new(big.Rat).Sub(big.NewRat(1, 1), new(big.Rat).Mul(limit, big.NewRat(int64(highCount), 1)))
		if den.Sign() > 0 && prefix.Sign() > 0 {
			candidate := new(big.Rat).Quo(new(big.Rat).Mul(limit, prefix), den)
			lowerOK := lowCount == 0 || candidate.Cmp(sorted[lowCount-1]) >= 0
			upperOK := candidate.Cmp(sorted[lowCount]) <= 0
			if lowerOK && upperOK {
				capValue = candidate
				break
			}
		}
		prefix.Add(prefix, sorted[lowCount])
	}
	if capValue == nil {
		return nil, errors.New("crv4: exact max-weight cap could not be solved")
	}
	for i, weight := range out {
		if weight.Cmp(capValue) > 0 {
			out[i] = new(big.Rat).Set(capValue)
		}
	}
	return out, nil
}

// NormalizeRationalToU16 max-normalizes exact rational scores and rounds
// half-up to u16. The largest positive weight is exactly 65535 and zero
// entries are omitted.
func NormalizeRationalToU16(uids []uint16, weights []*big.Rat) ([]uint16, []uint16, error) {
	if len(uids) != len(weights) {
		return nil, nil, fmt.Errorf("crv4: uids/weights length mismatch: %d != %d", len(uids), len(weights))
	}
	max := new(big.Rat)
	for _, weight := range weights {
		if weight == nil || weight.Sign() < 0 {
			return nil, nil, errors.New("crv4: rational weights must be non-nil and non-negative")
		}
		if weight.Cmp(max) > 0 {
			max.Set(weight)
		}
	}
	if max.Sign() == 0 {
		return []uint16{}, []uint16{}, nil
	}
	outUIDs := make([]uint16, 0, len(uids))
	outValues := make([]uint16, 0, len(uids))
	for i, weight := range weights {
		if weight.Sign() == 0 {
			continue
		}
		scaled := new(big.Rat).Mul(new(big.Rat).Quo(weight, max), big.NewRat(U16Max, 1))
		q, rem := new(big.Int), new(big.Int)
		q.QuoRem(scaled.Num(), scaled.Denom(), rem)
		if new(big.Int).Lsh(rem, 1).Cmp(scaled.Denom()) >= 0 {
			q.Add(q, big.NewInt(1))
		}
		if q.Sign() > 0 {
			if !q.IsUint64() || q.Uint64() > U16Max {
				return nil, nil, errors.New("crv4: internal exact normalization overflow")
			}
			outUIDs = append(outUIDs, uids[i])
			outValues = append(outValues, uint16(q.Uint64()))
		}
	}
	return outUIDs, outValues, nil
}
