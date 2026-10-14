// The closed Yuma reference evaluates the original fixed operations and an
// exact rational counterpart from the same witnessed inputs. Its finite work
// and rational-size limits refuse unknown work instead of widening tolerance.
package main

import (
	"context"
	"errors"
	"math/big"
)

// Each instance belongs to one replay derivation. The exact lane removes only
// fixed precision rounding; source saturation and branch semantics remain.
type nativeYumaArithmetic struct {
	context context.Context
	exact   bool
	steps   uint64
	maximum uint64
	err     error
}

// Charge every operator before allocating growing rational expressions.
func (self *nativeYumaArithmetic) check(values ...*big.Rat) bool {
	if self.err != nil {
		return false
	}
	self.steps++
	if self.steps > self.maximum {
		self.err = errors.Join(errMonitorEconomicCapacity, errors.New("native Yuma arithmetic exceeds admitted finite work"))
		return false
	}
	if self.steps%1024 == 0 {
		self.err = self.context.Err()
		if self.err != nil {
			return false
		}
	}
	for _, value := range values {
		if value == nil || value.Num().BitLen() > 8192 || value.Denom().BitLen() > 8192 {
			self.err = errors.Join(errMonitorEconomicCapacity, errors.New("native Yuma rational reference exceeds finite precision work"))
			return false
		}
	}
	return true
}

// Signed fixed formats use the exact representable source limits. Inputs and
// conversions round toward negative infinity, as the original fixed shifts do.
func (self *nativeYumaArithmetic) convert(value *big.Rat, fraction, width uint, signed bool) *big.Rat {
	if !self.check(value) {
		return new(big.Rat)
	}
	scale := new(big.Int).Lsh(big.NewInt(1), fraction)
	maximum := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), width), big.NewInt(1))
	minimum := new(big.Int)
	if signed {
		maximum.Rsh(maximum, 1)
		minimum.Neg(new(big.Int).Lsh(big.NewInt(1), width-1))
	}
	low, high := new(big.Rat).SetFrac(minimum, scale), new(big.Rat).SetFrac(maximum, scale)
	value = new(big.Rat).Set(value)
	if value.Cmp(low) < 0 {
		value = low
	}
	if value.Cmp(high) > 0 {
		value = high
	}
	if self.exact {
		return value
	}
	numerator := new(big.Int).Mul(value.Num(), scale)
	bits := new(big.Int).Div(numerator, value.Denom())
	return new(big.Rat).SetFrac(bits, scale)
}

// q32 preserves the signed I32F32 source domain.
func (self *nativeYumaArithmetic) q32(value *big.Rat) *big.Rat {
	return self.convert(value, 32, 64, true)
}

// q64 preserves the signed I64F64 stake domain.
func (self *nativeYumaArithmetic) q64(value *big.Rat) *big.Rat {
	return self.convert(value, 64, 128, true)
}

// u96 preserves the unsigned U96F32 inherited-stake domain.
func (self *nativeYumaArithmetic) u96(value *big.Rat) *big.Rat {
	return self.convert(value, 32, 128, false)
}

// nativeYumaUint retains the complete unsigned integer before any conversion.
func nativeYumaUint(value uint64) *big.Rat { return new(big.Rat).SetInt(new(big.Int).SetUint64(value)) }

// add charges its inputs before allocating the exact addition.
func (self *nativeYumaArithmetic) add(x, y *big.Rat) *big.Rat {
	if !self.check(x, y) {
		return new(big.Rat)
	}
	return self.q32(new(big.Rat).Add(x, y))
}

// sub charges its inputs before allocating the exact subtraction.
func (self *nativeYumaArithmetic) sub(x, y *big.Rat) *big.Rat {
	if !self.check(x, y) {
		return new(big.Rat)
	}
	return self.q32(new(big.Rat).Sub(x, y))
}

// mul charges its inputs before allocating the exact multiplication.
func (self *nativeYumaArithmetic) mul(x, y *big.Rat) *big.Rat {
	if !self.check(x, y) {
		return new(big.Rat)
	}
	return self.q32(new(big.Rat).Mul(x, y))
}

// Fixed division truncates its integer quotient toward zero. The safe source
// division returns zero for a zero denominator; it does not invent a weight.
func (self *nativeYumaArithmetic) div(x, y *big.Rat) *big.Rat {
	if !self.check(x, y) || y.Sign() == 0 {
		return new(big.Rat)
	}
	value := new(big.Rat).Quo(x, y)
	if self.exact {
		return self.q32(value)
	}
	scale := new(big.Int).Lsh(big.NewInt(1), 32)
	bits := new(big.Int).Quo(new(big.Int).Mul(value.Num(), scale), value.Denom())
	return self.q32(new(big.Rat).SetFrac(bits, scale))
}

// normalize preserves the source row order and its zero-sum behavior.
func (self *nativeYumaArithmetic) normalize(values []*big.Rat) []*big.Rat {
	sum := new(big.Rat)
	for _, value := range values {
		if !self.check(value) {
			return nativeYumaZeros(len(values))
		}
		sum = self.add(sum, value)
	}
	result := make([]*big.Rat, len(values))
	for index, value := range values {
		if !self.check(value) {
			return nativeYumaZeros(len(values))
		}
		result[index] = new(big.Rat).Set(value)
		if sum.Sign() != 0 {
			result[index] = self.div(value, sum)
		}
	}
	return result
}

// nativeYumaZeros creates independent mutable rational cells.
func nativeYumaZeros(count int) []*big.Rat {
	values := make([]*big.Rat, count)
	for index := range values {
		values[index] = new(big.Rat)
	}
	return values
}

// nativeYumaCopy prevents an intermediate stage from aliasing its predecessor.
func nativeYumaCopy(values []*big.Rat) []*big.Rat {
	result := make([]*big.Rat, len(values))
	for index, value := range values {
		result[index] = new(big.Rat).Set(value)
	}
	return result
}

// This mirrors the deterministic midpoint partition and exact tie choices in
// the admitted sparse weighted-median helper; sorting into a new tie order is
// not an equivalent replacement at quantized stake boundaries.
func (self *nativeYumaArithmetic) median(stakes, scores []*big.Rat, kappa *big.Rat) *big.Rat {
	rows, positive := self.medianStake(stakes)
	selected := make([]*big.Rat, len(rows))
	for index, row := range rows {
		selected[index] = scores[row]
	}
	return self.medianPrepared(positive, selected, kappa)
}

// The original sparse helper selects positive stake in source order and
// normalizes it once for all columns. Zero-stake rows cannot affect its median.
func (self *nativeYumaArithmetic) medianStake(stakes []*big.Rat) ([]int, []*big.Rat) {
	var rows []int
	var positive []*big.Rat
	for index, stake := range stakes {
		if !self.check(stake) {
			return nil, nil
		}
		if stake.Sign() > 0 {
			rows = append(rows, index)
			positive = append(positive, stake)
		}
	}
	return rows, self.normalize(positive)
}

// The prepared columns preserve the exact original positive-stake ordering,
// midpoint pivot, fixed tie arithmetic and source-selected minority threshold.
func (self *nativeYumaArithmetic) medianPrepared(positive, scores []*big.Rat, kappa *big.Rat) *big.Rat {
	indices := make([]int, len(positive))
	sum := new(big.Rat)
	for index := range indices {
		indices[index] = index
		sum = self.add(sum, positive[index])
	}
	minority := self.sub(sum, kappa)
	low, high := new(big.Rat), sum
	for steps := 0; steps <= len(positive); steps++ {
		if len(indices) == 0 {
			return new(big.Rat)
		}
		if len(indices) == 1 {
			return new(big.Rat).Set(scores[indices[0]])
		}
		pivot := scores[indices[len(indices)/2]]
		lo, hi := new(big.Rat), new(big.Rat)
		var lower, upper []int
		for _, index := range indices {
			if !self.check() {
				return new(big.Rat)
			}
			comparison := scores[index].Cmp(pivot)
			if comparison < 0 {
				lo = self.add(lo, positive[index])
				lower = append(lower, index)
			} else if comparison > 0 {
				hi = self.add(hi, positive[index])
				upper = append(upper, index)
			}
		}
		if len(lower) > 0 && minority.Cmp(self.add(low, lo)) < 0 {
			indices = lower
			high = self.add(low, lo)
		} else if len(upper) > 0 && self.sub(high, hi).Cmp(minority) <= 0 {
			indices = upper
			low = self.sub(high, hi)
		} else {
			return new(big.Rat).Set(pivot)
		}
	}
	return new(big.Rat)
}

// Preserve the degree-31 finite Taylor expansion (32 terms),
// clamping and checked-overflow fallback used by the admitted source family.
// The rational lane removes intermediate fixed rounding, not that algorithm.
func (self *nativeYumaArithmetic) exp(value *big.Rat) *big.Rat {
	if !self.check(value) {
		return new(big.Rat)
	}
	limit := nativeYumaUint(20)
	negative := value.Sign() < 0
	x := new(big.Rat).Abs(value)
	if x.Cmp(limit) > 0 {
		x = limit
	}
	if x.Sign() == 0 {
		return nativeYumaUint(1)
	}
	// The source's special +1 branch uses its exact I9F23 constant.
	if !negative && x.Cmp(nativeYumaUint(1)) == 0 {
		raw, _ := new(big.Int).SetString("ADF85458A2BB4A9AAFDC5620273D3CF1", 16)
		if self.exact {
			return new(big.Rat).SetFrac(raw, new(big.Int).Lsh(big.NewInt(1), 126))
		}
		raw.Rsh(raw, 103)
		return new(big.Rat).SetFrac(raw, new(big.Int).Lsh(big.NewInt(1), 23))
	}
	result, term := self.add(x, nativeYumaUint(1)), new(big.Rat).Set(x)
	maximum := new(big.Rat).SetFrac(new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 63), big.NewInt(1)), new(big.Int).Lsh(big.NewInt(1), 32))
	for index := uint64(2); index < 32; index++ {
		if !self.check(term, x) {
			return new(big.Rat)
		}
		product := new(big.Rat).Mul(term, x)
		if !self.exact {
			// checked_mul tests the shifted integer product, before saturation.
			scale := new(big.Int).Lsh(big.NewInt(1), 32)
			bits := new(big.Int).Div(new(big.Int).Mul(product.Num(), scale), product.Denom())
			product = new(big.Rat).SetFrac(bits, scale)
		}
		if product.Cmp(maximum) > 0 {
			if negative {
				return new(big.Rat)
			}
			return maximum
		}
		term = self.div(self.q32(product), nativeYumaUint(index))
		sum := new(big.Rat).Add(result, term)
		if sum.Cmp(maximum) > 0 {
			if negative {
				return new(big.Rat)
			}
			return maximum
		}
		result = self.q32(sum)
	}
	if negative {
		return self.div(nativeYumaUint(1), result)
	}
	return result
}
