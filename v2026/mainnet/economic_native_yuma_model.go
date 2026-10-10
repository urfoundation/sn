// The closed allocation model starts at the original integer stake, graph,
// weight and bond inputs. The fixed lane must reproduce the actual Wasm's
// outputs; the rational lane measures rounding over that same finite algorithm.
package main

import (
	"context"
	"errors"
	"math/big"
	"sort"
)

// These are decoded original-memory values, never an independently supplied
// amount report. Parent and child lists retain their original iteration order.
type nativeYumaInput struct {
	Netuid            uint16
	Count             uint16
	CurrentBlock      uint64
	Tempo             uint64
	ActivityCutoff    uint64
	LastStep          uint64
	MinimumStake      uint64
	OwnerUid          uint16
	TaoWeight         uint64
	Kappa             uint16
	BondsPenalty      uint16
	MovingAverage     uint64
	Yuma3             bool
	LiquidAlpha       bool
	CommitReveal      bool
	ConsensusMode     uint8
	AlphaLow          uint16
	AlphaHigh         uint16
	Steepness         int16
	PreviousConsensus []uint16
	Nodes             []nativeYumaNode
	Weights           [][]nativeYumaEdge
	Bonds             [][]nativeYumaEdge
}

// Raw stakes precede inherited-stake rounding. The source-selected earliest
// active commit block is integer state, not an already rounded weight.
type nativeYumaNode struct {
	Uid         uint16
	Hotkey      string
	Registered  uint64
	LastUpdate  uint64
	Permit      bool
	Alpha       uint64
	Tao         uint64
	Children    []nativeYumaShare
	Parents     []nativeYumaShare
	CommitBlock uint64
}

// nativeYumaShare binds each raw proportional stake input to its original hotkey.
type nativeYumaShare struct {
	Hotkey     string
	Proportion uint64
	Alpha      uint64
	Tao        uint64
}

// nativeYumaEdge retains a unique original sparse u16 column and raw weight.
type nativeYumaEdge struct {
	Column uint16
	Value  uint16
}

// nativeYumaCell holds a source-ordered intermediate fixed or rational value.
type nativeYumaCell struct {
	column int
	value  *big.Rat
}

// nativeYumaMatrix retains every UID row, including observed empty rows.
type nativeYumaMatrix [][]nativeYumaCell

// Intermediate stages are retained in the projection for review, but are
// always recomputed from original inputs before any tolerance is consumed.
type nativeYumaResult struct {
	stake          []*big.Rat
	active         []*big.Rat
	consensus      []*big.Rat
	incentive      []*big.Rat
	dividend       []*big.Rat
	server         []*big.Rat
	validator      []*big.Rat
	serverAlpha    []*big.Rat
	validatorAlpha []*big.Rat
	steps          uint64
}

// The runtime's inherited stake uses unsigned U96F32 then a u64 conversion.
// Keeping this conversion in the fixed lane closes a previously uncounted loss.
func (self *nativeYumaArithmetic) inherited(node nativeYumaNode, tao bool) *big.Rat {
	initial := node.Alpha
	if tao {
		initial = node.Tao
	}
	children, parents := new(big.Rat), new(big.Rat)
	for _, share := range node.Children {
		if !self.check() {
			return new(big.Rat)
		}
		proportion := self.u96(new(big.Rat).Quo(nativeYumaUint(share.Proportion), nativeYumaUint(^uint64(0))))
		children = self.u96(new(big.Rat).Add(children, self.u96(new(big.Rat).Mul(nativeYumaUint(initial), proportion))))
	}
	for _, share := range node.Parents {
		if !self.check() {
			return new(big.Rat)
		}
		amount := share.Alpha
		if tao {
			amount = share.Tao
		}
		proportion := self.u96(new(big.Rat).Quo(nativeYumaUint(share.Proportion), nativeYumaUint(^uint64(0))))
		parents = self.u96(new(big.Rat).Add(parents, self.u96(new(big.Rat).Mul(nativeYumaUint(amount), proportion))))
	}
	value := self.u96(new(big.Rat).Add(self.u96(new(big.Rat).Sub(nativeYumaUint(initial), children)), parents))
	return self.convert(value, 0, 64, false)
}

// Column sums preserve row order and each row's admitted original edge order.
func (self *nativeYumaArithmetic) normalizeColumns(matrix nativeYumaMatrix, count int) nativeYumaMatrix {
	sums := nativeYumaZeros(count)
	for _, row := range matrix {
		for _, cell := range row {
			if !self.check(cell.value) {
				return make(nativeYumaMatrix, len(matrix))
			}
			sums[cell.column] = self.add(sums[cell.column], cell.value)
		}
	}
	result := make(nativeYumaMatrix, len(matrix))
	for index, row := range matrix {
		for _, cell := range row {
			if !self.check(cell.value) {
				return make(nativeYumaMatrix, len(matrix))
			}
			value := new(big.Rat).Set(cell.value)
			if sums[cell.column].Sign() != 0 {
				value = self.div(value, sums[cell.column])
			}
			result[index] = append(result[index], nativeYumaCell{column: cell.column, value: value})
		}
	}
	return result
}

// Sparse access is only used for sorted, unique columns admitted by the decoder.
func nativeYumaCellAt(row []nativeYumaCell, column int) *big.Rat {
	index := sort.Search(len(row), func(index int) bool { return row[index].column >= column })
	if index < len(row) && row[index].column == column {
		return row[index].value
	}
	return new(big.Rat)
}

// nativeYumaClamp copies the selected bound without changing the source branch.
func nativeYumaClamp(value, low, high *big.Rat) *big.Rat {
	if value.Cmp(low) < 0 {
		return new(big.Rat).Set(low)
	}
	if value.Cmp(high) > 0 {
		return new(big.Rat).Set(high)
	}
	return new(big.Rat).Set(value)
}

// Saturating division and the original finite exponential remain part of the
// algorithm. This is not a claim about ideal transcendental sigmoid accuracy.
func (self *nativeYumaArithmetic) liquidAlpha(input nativeYumaInput, weight, bond, consensus *big.Rat) *big.Rat {
	zero, one := new(big.Rat), nativeYumaUint(1)
	low := self.div(nativeYumaUint(uint64(input.AlphaLow)), nativeYumaUint(65535))
	high := self.div(nativeYumaUint(uint64(input.AlphaHigh)), nativeYumaUint(65535))
	difference := nativeYumaClamp(self.sub(weight, consensus), zero, one)
	if weight.Cmp(bond) < 0 {
		difference = nativeYumaClamp(self.sub(bond, weight), zero, one)
	}
	scale := self.div(new(big.Rat).SetInt64(int64(input.Steepness)), new(big.Rat).SetInt64(-100))
	exponent := self.mul(scale, self.sub(difference, new(big.Rat).SetFrac64(1, 2)))
	sigmoid := self.div(one, self.add(one, self.exp(exponent)))
	return nativeYumaClamp(self.add(low, self.mul(sigmoid, self.sub(high, low))), low, high)
}

// Normal EMA retains old-only bonds. Dynamic alpha intentionally emits only
// new-weight columns, matching mat_ema_alpha_sparse in the admitted source.
func (self *nativeYumaArithmetic) ema(input nativeYumaInput, fresh, old nativeYumaMatrix, consensus []*big.Rat) nativeYumaMatrix {
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
	// This owner-local cache stores at most 256 pure coefficient evaluations.
	// Policy settings are immutable within this EMA; all three varying inputs
	// use canonical exact rational keys and results are copied before reuse.
	type alphaKey struct{ weight, bond, consensus string }
	coefficients := map[alphaKey]*big.Rat{}
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
				key := alphaKey{weight: cell.value.RatString(), bond: bond.RatString(), consensus: selected[cell.column].RatString()}
				coefficient, known := coefficients[key]
				if known {
					coefficient = new(big.Rat).Set(coefficient)
				} else {
					coefficient = self.liquidAlpha(input, cell.value, bond, selected[cell.column])
					if self.err != nil {
						return result
					}
					if len(coefficients) < 256 {
						coefficients[key] = new(big.Rat).Set(coefficient)
					}
				}
				decay := self.mul(self.sub(nativeYumaUint(1), coefficient), bond)
				increment := nativeYumaClamp(self.mul(coefficient, cell.value), new(big.Rat), nativeYumaUint(1))
				value := nativeYumaClamp(self.add(decay, increment), new(big.Rat), nativeYumaUint(1))
				if value.Sign() > 0 {
					result[index] = append(result[index], nativeYumaCell{column: cell.column, value: value})
				}
			}
			continue
		}
		// Original rows are sorted and unique. Visit their ordered union,
		// retaining fresh-before-old arithmetic for each column; every absent
		// column in the dense source accumulator remains exactly zero.
		for freshIndex, oldIndex := 0, 0; freshIndex < len(fresh[index]) || oldIndex < len(old[index]); {
			column := count
			if freshIndex < len(fresh[index]) {
				column = fresh[index][freshIndex].column
			}
			if oldIndex < len(old[index]) && old[index][oldIndex].column < column {
				column = old[index][oldIndex].column
			}
			value := new(big.Rat)
			if freshIndex < len(fresh[index]) && fresh[index][freshIndex].column == column {
				cell := fresh[index][freshIndex]
				if !self.check(cell.value) {
					return result
				}
				value = self.add(value, self.mul(alpha, cell.value))
				freshIndex++
			}
			if oldIndex < len(old[index]) && old[index][oldIndex].column == column {
				cell := old[index][oldIndex]
				if !self.check(cell.value) {
					return result
				}
				value = self.add(value, self.mul(self.sub(nativeYumaUint(1), alpha), cell.value))
				oldIndex++
			}
			if value.Sign() > 0 {
				result[index] = append(result[index], nativeYumaCell{column: column, value: value})
			}
		}
	}
	return result
}

// Clipping retains a subset of each original row. The omitted dense cells are
// exactly 0 + ratio*(0-0); signed subtraction and each fixed rounding remain in
// the original order for every present column, including explicit zero edges.
func (self *nativeYumaArithmetic) interpolate(weights, clipped nativeYumaMatrix, penalty uint16) nativeYumaMatrix {
	result := make(nativeYumaMatrix, len(weights))
	ratio := self.div(nativeYumaUint(uint64(penalty)), nativeYumaUint(65535))
	for row, cells := range weights {
		for _, cell := range cells {
			if !self.check() {
				return result
			}
			value := self.add(cell.value, self.mul(ratio, self.sub(nativeYumaCellAt(clipped[row], cell.column), cell.value)))
			if value.Sign() > 0 {
				result[row] = append(result[row], nativeYumaCell{column: cell.column, value: value})
			}
		}
	}
	return result
}

// One invocation is cancellation-owned and finite. The admitted original
// profile fixes the algorithm family; unknown shape or overflow is a refusal.
func evaluateNativeYuma(ctx context.Context, input nativeYumaInput, total uint64, exact bool, maximum uint64) (*nativeYumaResult, error) {
	if ctx == nil {
		return nil, errors.New("native Yuma calculation has no lifecycle owner")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	a := &nativeYumaArithmetic{context: ctx, exact: exact, maximum: maximum}
	count := int(input.Count)
	if count == 0 || count > rootCensusLimit || len(input.Nodes) != count || len(input.Weights) != count || len(input.Bonds) != count {
		return nil, errors.New("native Yuma complete input dimensions differ")
	}
	stake := nativeYumaZeros(count)
	taoWeight := a.q64(a.u96(new(big.Rat).Quo(nativeYumaUint(input.TaoWeight), nativeYumaUint(^uint64(0)))))
	sum := new(big.Rat)
	maximumStake := new(big.Rat).SetFrac(new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 127), big.NewInt(1)), new(big.Int).Lsh(big.NewInt(1), 64))
	for index, node := range input.Nodes {
		if !a.check() {
			return nil, a.err
		}
		alpha, tao := a.q64(a.inherited(node, false)), a.q64(a.inherited(node, true))
		value := a.q64(new(big.Rat).Add(alpha, a.q64(new(big.Rat).Mul(tao, taoWeight))))
		threshold := a.convert(value, 0, 64, false)
		if node.Uid != input.OwnerUid && threshold.Cmp(nativeYumaUint(input.MinimumStake)) < 0 {
			value = new(big.Rat)
		}
		stake[index] = value
		sum.Add(sum, value)
		if sum.Cmp(maximumStake) > 0 {
			return nil, errors.New("native Yuma stake sum exceeds nonoverflowing source domain")
		}
	}
	for index, value := range stake {
		if a.err != nil {
			return nil, a.err
		}
		if sum.Sign() != 0 {
			value = a.q64(new(big.Rat).Quo(value, sum))
		}
		stake[index] = a.q32(value)
	}
	active := nativeYumaCopy(stake)
	permits := make([]bool, count)
	for index, node := range input.Nodes {
		if !a.check() {
			return nil, a.err
		}
		permits[index] = node.Permit || node.Uid == input.OwnerUid
		updated := node.LastUpdate + input.ActivityCutoff
		if updated < node.LastUpdate {
			updated = ^uint64(0)
		}
		if !permits[index] || updated < input.CurrentBlock {
			active[index] = new(big.Rat)
		}
	}
	active = a.normalize(active)
	weights, bonds := make(nativeYumaMatrix, count), make(nativeYumaMatrix, count)
	lastTempo := uint64(0)
	if input.LastStep != 0 {
		lastTempo = input.LastStep + 1
		if lastTempo == 0 {
			lastTempo = ^uint64(0)
		}
	} else if input.CurrentBlock >= input.Tempo {
		lastTempo = input.CurrentBlock - input.Tempo
	}
	for index, node := range input.Nodes {
		if !a.check() {
			return nil, a.err
		}
		if permits[index] {
			var columns []int
			var values []*big.Rat
			for _, edge := range input.Weights[index] {
				if !a.check() {
					return nil, a.err
				}
				if int(edge.Column) >= count {
					return nil, errors.New("native Yuma weight has foreign UID")
				}
				if edge.Column == node.Uid && node.Uid != input.OwnerUid || node.LastUpdate <= input.Nodes[edge.Column].Registered || input.CommitReveal && node.CommitBlock < input.Nodes[edge.Column].Registered {
					continue
				}
				columns = append(columns, int(edge.Column))
				values = append(values, nativeYumaUint(uint64(edge.Value)))
			}
			values = a.normalize(values)
			for i, value := range values {
				weights[index] = append(weights[index], nativeYumaCell{column: columns[i], value: value})
			}
		}
		for _, edge := range input.Bonds[index] {
			if !a.check() {
				return nil, a.err
			}
			if int(edge.Column) >= count {
				return nil, errors.New("native Yuma bond has foreign UID")
			}
			if lastTempo <= input.Nodes[edge.Column].Registered {
				continue
			}
			value := nativeYumaUint(uint64(edge.Value))
			if input.Yuma3 {
				value = a.div(value, nativeYumaUint(65535))
			}
			bonds[index] = append(bonds[index], nativeYumaCell{column: int(edge.Column), value: value})
		}
	}
	consensus := nativeYumaZeros(count)
	kappa := a.div(nativeYumaUint(uint64(input.Kappa)), nativeYumaUint(65535))
	medianRows, medianStake := a.medianStake(active)
	for column := 0; column < count; column++ {
		scores := make([]*big.Rat, len(medianRows))
		for index, row := range medianRows {
			if !a.check() {
				return nil, a.err
			}
			scores[index] = nativeYumaCellAt(weights[row], column)
		}
		consensus[column] = a.medianPrepared(medianStake, scores, kappa)
		if a.err != nil {
			return nil, a.err
		}
	}
	clipped := make(nativeYumaMatrix, count)
	ranks := nativeYumaZeros(count)
	for index, row := range weights {
		for _, cell := range row {
			if !a.check(cell.value) {
				return nil, a.err
			}
			value := new(big.Rat).Set(cell.value)
			if value.Cmp(consensus[cell.column]) > 0 {
				value = new(big.Rat).Set(consensus[cell.column])
				if value.Sign() <= 0 {
					continue
				}
			}
			// An original zero entry survives when it was not clipped. Dynamic EMA
			// can retain an old bond at that explicitly present new-weight column.
			clipped[index] = append(clipped[index], nativeYumaCell{column: cell.column, value: value})
			ranks[cell.column] = a.add(ranks[cell.column], a.mul(value, active[index]))
		}
	}
	incentive := a.normalize(ranks)
	weightsForBonds := weights
	if input.BondsPenalty == 65535 {
		weightsForBonds = clipped
	} else if input.BondsPenalty != 0 {
		weightsForBonds = a.interpolate(weights, clipped, input.BondsPenalty)
	}
	var ema nativeYumaMatrix
	if input.Yuma3 {
		ema = a.ema(input, weightsForBonds, bonds, consensus)
	} else {
		bonds = a.normalizeColumns(bonds, count)
		delta := make(nativeYumaMatrix, count)
		for row, cells := range weightsForBonds {
			for _, cell := range cells {
				if a.err != nil {
					return nil, a.err
				}
				delta[row] = append(delta[row], nativeYumaCell{column: cell.column, value: a.mul(cell.value, active[row])})
			}
		}
		ema = a.ema(input, a.normalizeColumns(delta, count), bonds, consensus)
	}
	if a.err != nil {
		return nil, a.err
	}
	ema = a.normalizeColumns(ema, count)
	dividend := nativeYumaZeros(count)
	for row, cells := range ema {
		for _, cell := range cells {
			if !a.check(cell.value) {
				return nil, a.err
			}
			dividend[row] = a.add(dividend[row], a.mul(cell.value, incentive[cell.column]))
		}
		if input.Yuma3 {
			dividend[row] = a.mul(dividend[row], active[row])
		}
	}
	dividend = a.normalize(dividend)
	emissionSum := new(big.Rat)
	for index := range incentive {
		if a.err != nil {
			return nil, a.err
		}
		emissionSum = a.add(emissionSum, a.add(incentive[index], dividend[index]))
	}
	server, validator := nativeYumaCopy(incentive), nativeYumaCopy(dividend)
	for index := range server {
		if a.err != nil {
			return nil, a.err
		}
		if emissionSum.Sign() != 0 {
			server[index] = a.div(server[index], emissionSum)
			validator[index] = a.div(validator[index], emissionSum)
		}
	}
	if emissionSum.Sign() == 0 {
		activeSum := new(big.Rat)
		for _, value := range active {
			activeSum.Add(activeSum, value)
		}
		validator = nativeYumaCopy(active)
		if activeSum.Sign() == 0 {
			validator = nativeYumaCopy(stake)
		}
	}
	serverAlpha, validatorAlpha := nativeYumaZeros(count), nativeYumaZeros(count)
	for index := range server {
		if a.err != nil {
			return nil, a.err
		}
		serverAlpha[index] = a.convert(new(big.Rat).Mul(server[index], nativeYumaUint(total)), 0, 64, false)
		validatorAlpha[index] = a.convert(new(big.Rat).Mul(validator[index], nativeYumaUint(total)), 0, 64, false)
	}
	if err := errors.Join(a.err, ctx.Err()); err != nil {
		return nil, err
	}
	return &nativeYumaResult{stake: stake, active: active, consensus: consensus, incentive: incentive, dividend: dividend, server: server, validator: validator, serverAlpha: serverAlpha, validatorAlpha: validatorAlpha, steps: a.steps}, nil
}
