// These two finite references preserve the exact pre-optimization helpers.
// Their source binding is retained in the intake; comparisons exercise source
// ordering, fixed signed rounding and the complete uncached liquid coefficient.
package main

import (
	"math/big"
	"reflect"
	"testing"
)

func (self *nativeYumaArithmetic) nativeYumaTestOriginalMedian(stakes, scores []*big.Rat, kappa *big.Rat) *big.Rat {
	var selected []int
	var positive []*big.Rat
	for index, stake := range stakes {
		if !self.check(stake) {
			return new(big.Rat)
		}
		if stake.Sign() > 0 {
			selected = append(selected, index)
			positive = append(positive, stake)
		}
	}
	positive = self.normalize(positive)
	indices := make([]int, len(selected))
	sum := new(big.Rat)
	for index := range indices {
		indices[index] = index
		sum = self.add(sum, positive[index])
	}
	minority := self.sub(sum, kappa)
	low, high := new(big.Rat), sum
	for steps := 0; steps <= len(selected); steps++ {
		if len(indices) == 0 {
			return new(big.Rat)
		}
		if len(indices) == 1 {
			return new(big.Rat).Set(scores[selected[indices[0]]])
		}
		pivot := scores[selected[indices[len(indices)/2]]]
		lo, hi := new(big.Rat), new(big.Rat)
		var lower, upper []int
		for _, index := range indices {
			if !self.check() {
				return new(big.Rat)
			}
			comparison := scores[selected[index]].Cmp(pivot)
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

func (self *nativeYumaArithmetic) nativeYumaTestOriginalEma(input nativeYumaInput, fresh, old nativeYumaMatrix, consensus []*big.Rat) nativeYumaMatrix {
	count := int(input.Count)
	selected := nativeYumaCopy(consensus)
	previous := input.ConsensusMode == 1 || input.ConsensusMode == 2 && input.BondsPenalty == 65535
	if previous && len(input.PreviousConsensus) != 0 {
		selected = nativeYumaZeros(count)
		for index, value := range input.PreviousConsensus {
			if index < count {
				selected[index] = self.div(nativeYumaUint(uint64(value)), nativeYumaUint(65535))
			}
		}
	}
	dynamic := false
	if input.Yuma3 && input.LiquidAlpha {
		for _, value := range selected {
			dynamic = dynamic || value.Sign() != 0
		}
	}
	moving := self.q64(new(big.Rat).Quo(self.q64(nativeYumaUint(input.MovingAverage)), nativeYumaUint(1000000)))
	alpha := self.sub(nativeYumaUint(1), self.q32(moving))
	result := make(nativeYumaMatrix, count)
	for index := 0; index < count; index++ {
		if !self.check() {
			return result
		}
		if dynamic {
			for _, cell := range fresh[index] {
				if !self.check(cell.value) {
					return result
				}
				bond := nativeYumaCellAt(old[index], cell.column)
				coefficient := self.liquidAlpha(input, cell.value, bond, selected[cell.column])
				decay := self.mul(self.sub(nativeYumaUint(1), coefficient), bond)
				increment := nativeYumaClamp(self.mul(coefficient, cell.value), new(big.Rat), nativeYumaUint(1))
				value := nativeYumaClamp(self.add(decay, increment), new(big.Rat), nativeYumaUint(1))
				if value.Sign() > 0 {
					result[index] = append(result[index], nativeYumaCell{column: cell.column, value: value})
				}
			}
			continue
		}
		values := nativeYumaZeros(count)
		for _, cell := range fresh[index] {
			if !self.check(cell.value) {
				return result
			}
			values[cell.column] = self.add(values[cell.column], self.mul(alpha, cell.value))
		}
		for _, cell := range old[index] {
			if !self.check(cell.value) {
				return result
			}
			values[cell.column] = self.add(values[cell.column], self.mul(self.sub(nativeYumaUint(1), alpha), cell.value))
		}
		for column, value := range values {
			if value.Sign() > 0 {
				result[index] = append(result[index], nativeYumaCell{column: column, value: value})
			}
		}
	}
	return result
}

func TestNativeYumaPreparedMedianEqualsOriginalSourceOrdering(t *testing.T) {
	for _, exact := range []bool{false, true} {
		for variant := 0; variant < 17; variant++ {
			stakes, scores := nativeYumaZeros(64), nativeYumaZeros(64)
			for index := range stakes {
				if (index+variant)%5 != 0 {
					stakes[index] = new(big.Rat).SetFrac64(int64((index+variant)%7+1), 113)
				}
				scores[index] = new(big.Rat).SetFrac64(int64((index*13+variant)%9), 17)
			}
			a, b := &nativeYumaArithmetic{context: t.Context(), exact: exact, maximum: 100000}, &nativeYumaArithmetic{context: t.Context(), exact: exact, maximum: 100000}
			rows, positive := a.medianStake(stakes)
			columns := make([]*big.Rat, len(rows))
			for i, row := range rows {
				columns[i] = scores[row]
			}
			got, want := a.medianPrepared(positive, columns, new(big.Rat).SetFrac64(1, 2)), b.nativeYumaTestOriginalMedian(stakes, scores, new(big.Rat).SetFrac64(1, 2))
			if a.err != nil || b.err != nil || got.Cmp(want) != 0 {
				t.Fatal("prepared median changed original source ordering or fixed tie", exact, variant, got, want, a.err, b.err)
			}
		}
	}
}

func TestNativeYumaLiquidCachePreservesAllExactCoefficientInputs(t *testing.T) {
	input := nativeYumaTestInput()
	input.Count = 64
	input.Yuma3 = true
	input.LiquidAlpha = true
	input.Steepness = 100
	fresh, old := make(nativeYumaMatrix, 64), make(nativeYumaMatrix, 64)
	consensus := nativeYumaZeros(64)
	for index := range consensus {
		consensus[index] = new(big.Rat).SetFrac64(int64(index%3+1), 16)
	}
	for row := range fresh {
		for column := 0; column < 64; column++ {
			fresh[row] = append(fresh[row], nativeYumaCell{column: column, value: new(big.Rat).SetFrac64(int64((row+column)%3+1), 13)})
			old[row] = append(old[row], nativeYumaCell{column: column, value: new(big.Rat).SetFrac64(int64((row+column)%5+1), 19)})
		}
	}
	for _, exact := range []bool{false, true} {
		a, b := &nativeYumaArithmetic{context: t.Context(), exact: exact, maximum: 10000000}, &nativeYumaArithmetic{context: t.Context(), exact: exact, maximum: 10000000}
		got, want := a.ema(input, fresh, old, consensus), b.nativeYumaTestOriginalEma(input, fresh, old, consensus)
		if a.err != nil || b.err != nil || !reflect.DeepEqual(got, want) {
			t.Fatal("owner-local liquid coefficient cache changed weight/bond/consensus semantics", exact, a.err, b.err)
		}
		if a.steps >= b.steps {
			t.Fatal("repeated exact coefficient work was not bounded", a.steps, b.steps)
		}
	}
}
