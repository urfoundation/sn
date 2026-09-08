package crv4

import (
	"errors"
	"fmt"
	"math"
	"sort"
)

// U16Max is the maximum u16 weight value; the largest weight maps to this.
const U16Max = 65535

// NormalizeToU16 converts float scores to the on-chain u16 weight
// representation, mirroring the bittensor SDK's
// convert_weights_and_uids_for_emit (bittensor/utils/weight_utils.py
// @ c4dca6b):
//
//   - negative weights are rejected
//   - all-zero weights produce empty vectors (nothing to set on chain)
//   - weights are max-upscaled (max -> 1.0) then rounded to round(w * 65535),
//     so the largest weight is exactly 65535
//   - zero entries are filtered out (uid dropped)
//
// The returned slices are parallel uids/values ready for Payload.
// Note: Go's math.Round rounds half away from zero while Python's round()
// rounds half to even; exact .5 products differ in the last bit only and are
// client-side choices (any u16 vector is chain-valid).
func NormalizeToU16(uids []uint16, weights []float64) ([]uint16, []uint16, error) {
	if len(uids) != len(weights) {
		return nil, nil, fmt.Errorf("crv4: uids/weights length mismatch: %d != %d", len(uids), len(weights))
	}
	maxW := 0.0
	sum := 0.0
	for _, w := range weights {
		if w < 0 || math.IsNaN(w) || math.IsInf(w, 0) {
			return nil, nil, errors.New("crv4: weights must be finite and non-negative")
		}
		if w > maxW {
			maxW = w
		}
		sum += w
	}
	if sum == 0 {
		return []uint16{}, []uint16{}, nil
	}
	outUids := make([]uint16, 0, len(uids))
	outVals := make([]uint16, 0, len(uids))
	for i, w := range weights {
		v := math.Round(w / maxW * U16Max)
		if v != 0 {
			outUids = append(outUids, uids[i])
			outVals = append(outVals, uint16(v))
		}
	}
	return outUids, outVals, nil
}

// ApplyMaxWeightLimit caps weights so they satisfy subtensor's
// max_weight_limited check at reveal time
// (pallets/subtensor/src/subnets/weights.rs + epoch/math.rs
// check_vec_max_limited): after sum-normalization, every weight must satisfy
// max(w)/sum(w) <= maxWeightLimit/65535. Self-weight-only vectors and
// maxWeightLimit == 65535 are exempt on chain.
//
// Semantics follow the SDK's normalize_max_weight water-filling: find the
// largest cap c such that c / sum(min(w_i, c)) <= limit and clip to it. If
// the constraint is unsatisfiable (limit * positive-count < 1), it fails
// rather than returning a vector which the chain or signed policy rejects.
//
// Returns a new slice; the input is not modified. A maxWeightLimit of 0 is
// rejected.
func ApplyMaxWeightLimit(weights []float64, maxWeightLimit uint16) ([]float64, error) {
	if maxWeightLimit == 0 {
		return nil, errors.New("crv4: max weight limit must be > 0")
	}
	out := make([]float64, len(weights))
	copy(out, weights)
	if len(weights) == 0 {
		return out, nil
	}
	limit := float64(maxWeightLimit) / U16Max

	maxW := 0.0
	sum := 0.0
	positive := 0
	for _, w := range out {
		if w < 0 || math.IsNaN(w) || math.IsInf(w, 0) {
			return nil, errors.New("crv4: weights must be finite and non-negative")
		}
		if w > 0 {
			positive++
		}
		if w > maxW {
			maxW = w
		}
		sum += w
	}
	if sum == 0 || maxWeightLimit == U16Max {
		return out, nil
	}
	if maxW/sum <= limit {
		return out, nil // already satisfies the chain check
	}
	if uint64(maxWeightLimit)*uint64(positive) < uint64(U16Max) {
		return nil, fmt.Errorf("crv4: max weight limit %d is infeasible for %d positive weights", maxWeightLimit, positive)
	}

	// Binary search the clip cap c in (0, maxW]: f(c) = c / sum(min(w, c))
	// is monotonically increasing, f(maxW) > limit here, f(0+) -> 1/n < limit.
	lo, hi := 0.0, maxW
	for i := 0; i < 128; i++ {
		c := (lo + hi) / 2
		s := 0.0
		for _, w := range out {
			s += math.Min(w, c)
		}
		if c/s <= limit {
			lo = c
		} else {
			hi = c
		}
	}
	for i, w := range out {
		out[i] = math.Min(w, lo)
	}
	return out, nil
}

// Repair the at-most-one-unit losses from u16 rounding before payload signing.
// Water filling proves the rational inequality, but rounding smaller entries
// down can otherwise make the emitted integer vector exceed the same cap.
func repairMaxWeightLimitU16(uids, values []uint16, maxWeightLimit uint16) error {
	if len(uids) != len(values) {
		return fmt.Errorf("crv4: uids/values length mismatch: %d != %d", len(uids), len(values))
	}
	if maxWeightLimit == 0 {
		return errors.New("crv4: max weight limit must be > 0")
	}
	if len(values) == 0 || maxWeightLimit == U16Max {
		return nil
	}
	var maximum uint16
	var sum uint64
	positive := 0
	for _, value := range values {
		sum += uint64(value)
		if value > 0 {
			positive++
		}
		if value > maximum {
			maximum = value
		}
	}
	if maximum == 0 {
		return nil
	}
	if uint64(maxWeightLimit)*uint64(positive) < uint64(U16Max) {
		return fmt.Errorf("crv4: max weight limit %d is infeasible for %d emitted weights", maxWeightLimit, positive)
	}
	left := uint64(maximum) * uint64(U16Max)
	right := sum * uint64(maxWeightLimit)
	if left <= right {
		return nil
	}
	requiredSum := (left + uint64(maxWeightLimit) - 1) / uint64(maxWeightLimit)
	deficit := requiredSum - sum
	indices := make([]int, 0, len(values))
	for index, value := range values {
		if value == 0 || value == maximum {
			continue
		}
		indices = append(indices, index)
	}
	sort.SliceStable(indices, func(i, j int) bool { return uids[indices[i]] < uids[indices[j]] })
	for _, index := range indices {
		value := values[index]
		increment := uint64(maximum - value)
		if increment > deficit {
			increment = deficit
		}
		values[index] += uint16(increment)
		deficit -= increment
		if deficit == 0 {
			break
		}
	}
	if deficit != 0 {
		return errors.New("crv4: emitted weights cannot be repaired to the max weight limit")
	}
	return nil
}
