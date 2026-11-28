// Source-level branch controls supplement the real-Wasm public join. These
// small exact cases make upstream rounding and fallback differences observable.
package main

import (
	"context"
	"errors"
	"math/big"
	"strings"
	"testing"
)

func nativeYumaTestInput() nativeYumaInput {
	return nativeYumaInput{Netuid: 25, Count: 2, CurrentBlock: 101, Tempo: 10, ActivityCutoff: 300, LastStep: 90, OwnerUid: 1, Kappa: 32768, BondsPenalty: 65535, MovingAverage: 1000000, AlphaLow: 16384, AlphaHigh: 49152,
		Nodes:   []nativeYumaNode{{Uid: 0, Hotkey: "0x" + strings.Repeat("1", 64), Registered: 20, LastUpdate: 99, CommitBlock: ^uint64(0)}, {Uid: 1, Hotkey: "0x" + strings.Repeat("2", 64), Registered: 21, LastUpdate: 99, Permit: true, Alpha: 100, CommitBlock: ^uint64(0)}},
		Weights: [][]nativeYumaEdge{{}, {{Column: 0, Value: 1}, {Column: 1, Value: 9}}},
		Bonds:   [][]nativeYumaEdge{{{Column: 0, Value: 1}, {Column: 1, Value: 1}}, {}},
	}
}

func TestNativeYumaClosedLegacyQuantizationIncludesUpstreamNormalization(t *testing.T) {
	input := nativeYumaTestInput()
	fixed, err := evaluateNativeYuma(t.Context(), input, 200, false, 200000)
	if err != nil {
		t.Fatal(err)
	}
	reference, err := evaluateNativeYuma(t.Context(), input, 200, true, 200000)
	if err != nil {
		t.Fatal(err)
	}
	if fixed.serverAlpha[0].RatString() != "9" || fixed.serverAlpha[1].RatString() != "89" || reference.serverAlpha[0].RatString() != "10" || reference.serverAlpha[1].RatString() != "90" || fixed.validatorAlpha[0].RatString() != "100" {
		t.Fatal("closed original normalization/cast source differs", fixed.serverAlpha, reference.serverAlpha, fixed.validatorAlpha)
	}
	if fixed.consensus[1].RatString() == reference.consensus[1].RatString() {
		t.Fatal("upstream weight normalization was silently treated as exact")
	}
}

func TestNativeYumaInheritedStakeClosesParentProportionAndIntegerLoss(t *testing.T) {
	node := nativeYumaNode{Parents: []nativeYumaShare{{Hotkey: "0x" + strings.Repeat("3", 64), Proportion: ^uint64(0) / 2, Alpha: 1000}}}
	fixed := &nativeYumaArithmetic{context: t.Context(), maximum: 10000}
	reference := &nativeYumaArithmetic{context: t.Context(), maximum: 10000, exact: true}
	actual, ideal := fixed.inherited(node, false), reference.inherited(node, false)
	if fixed.err != nil || reference.err != nil || actual.RatString() != "499" || ideal.Cmp(nativeYumaUint(499)) <= 0 || ideal.Cmp(nativeYumaUint(500)) >= 0 {
		t.Fatal("parent stake lost its original fixed proportion/u64 conversion", actual, ideal, fixed.err, reference.err)
	}
}

func TestNativeYumaSignedInterpolationUsesOriginalFloorAndDivision(t *testing.T) {
	a := &nativeYumaArithmetic{context: t.Context(), maximum: 1000}
	unit := new(big.Rat).SetFrac(new(big.Int).SetInt64(-1), new(big.Int).Lsh(big.NewInt(1), 32))
	product := a.mul(unit, new(big.Rat).SetFrac64(1, 2))
	quotient := a.div(unit, nativeYumaUint(2))
	if a.err != nil || product.Cmp(unit) != 0 || quotient.Sign() != 0 {
		t.Fatal("negative interpolation multiplication/division rounding changed", product, quotient, a.err)
	}
}

func TestNativeYumaOriginalMedianTieDoesNotChooseSortedLowerValue(t *testing.T) {
	a := &nativeYumaArithmetic{context: t.Context(), maximum: 1000}
	value := a.median([]*big.Rat{new(big.Rat).SetFrac64(1, 2), new(big.Rat).SetFrac64(1, 2)}, []*big.Rat{new(big.Rat), nativeYumaUint(1)}, new(big.Rat).SetFrac64(1, 2))
	if a.err != nil || value.RatString() != "1" {
		t.Fatal("original deterministic midpoint tie changed", value, a.err)
	}
}

func TestNativeYumaInactiveStakeFallsBackOnlyToValidatorAllocation(t *testing.T) {
	input := nativeYumaTestInput()
	input.ActivityCutoff = 1
	input.Nodes[1].LastUpdate = 50
	result, err := evaluateNativeYuma(t.Context(), input, 200, false, 200000)
	if err != nil {
		t.Fatal(err)
	}
	if result.serverAlpha[0].Sign() != 0 || result.serverAlpha[1].Sign() != 0 || result.validatorAlpha[1].RatString() != "200" {
		t.Fatal("inactive original fallback fabricated miner allocation", result.serverAlpha, result.validatorAlpha)
	}
}

func TestNativeYumaOriginalCommitAndRegistrationMasksAffectCompleteAllocation(t *testing.T) {
	input := nativeYumaTestInput()
	input.CommitReveal = true
	input.Nodes[1].CommitBlock = 20
	result, err := evaluateNativeYuma(t.Context(), input, 200, false, 200000)
	if err != nil {
		t.Fatal(err)
	}
	if result.serverAlpha[0].RatString() != "100" || result.serverAlpha[1].Sign() != 0 {
		t.Fatal("original active commit admitted a later registration weight", result.serverAlpha)
	}
	input.CommitReveal = false
	input.Nodes[1].LastUpdate = 20
	result, err = evaluateNativeYuma(t.Context(), input, 200, false, 200000)
	if err != nil {
		t.Fatal(err)
	}
	if result.serverAlpha[0].Sign() != 0 || result.serverAlpha[1].Sign() != 0 {
		t.Fatal("outdated original weights acquired miner allocation", result.serverAlpha)
	}
}

func TestNativeYumaMinimumStakeCannotBorrowOwnerExemption(t *testing.T) {
	input := nativeYumaTestInput()
	input.OwnerUid = 65535
	input.MinimumStake = 101
	result, err := evaluateNativeYuma(t.Context(), input, 200, false, 200000)
	if err != nil {
		t.Fatal(err)
	}
	if result.stake[1].Sign() != 0 || result.serverAlpha[0].Sign() != 0 || result.validatorAlpha[1].Sign() != 0 {
		t.Fatal("non-owner below original stake threshold retained allocation", result)
	}
}

func TestNativeYumaOldBondsCannotBorrowNewRegistration(t *testing.T) {
	input := nativeYumaTestInput()
	input.LastStep = 0
	input.Tempo = 100
	result, err := evaluateNativeYuma(t.Context(), input, 200, false, 200000)
	if err != nil {
		t.Fatal(err)
	}
	if result.dividend[0].Sign() != 0 || result.dividend[1].Sign() != 0 || result.serverAlpha[0].RatString() != "19" || result.serverAlpha[1].RatString() != "179" {
		t.Fatal("old bond remained after the original registration mask", result.dividend, result.serverAlpha)
	}
}

func TestNativeYumaLiquidEmaDropsOldOnlyColumns(t *testing.T) {
	input := nativeYumaTestInput()
	input.Yuma3 = true
	input.LiquidAlpha = true
	a := &nativeYumaArithmetic{context: t.Context(), maximum: 10000}
	fresh := nativeYumaMatrix{{}, {{column: 0, value: new(big.Rat).SetFrac64(1, 4)}}}
	old := nativeYumaMatrix{{{column: 0, value: nativeYumaUint(1)}}, {}}
	result := a.ema(input, fresh, old, []*big.Rat{nativeYumaUint(1), nativeYumaUint(1)})
	if a.err != nil || len(result[0]) != 0 || len(result[1]) != 1 || result[1][0].value.Sign() <= 0 {
		t.Fatal("dynamic original EMA resurrected an old-only bond", result, a.err)
	}
}

func TestNativeYumaPreviousConsensusSelectsLiquidFallback(t *testing.T) {
	input := nativeYumaTestInput()
	input.Yuma3 = true
	input.LiquidAlpha = true
	input.ConsensusMode = 1
	input.PreviousConsensus = []uint16{0, 0}
	input.MovingAverage = 1000000
	a := &nativeYumaArithmetic{context: t.Context(), maximum: 10000}
	fresh := nativeYumaMatrix{{}, {{column: 0, value: new(big.Rat).SetFrac64(1, 4)}}}
	old := nativeYumaMatrix{{{column: 0, value: nativeYumaUint(1)}}, {}}
	result := a.ema(input, fresh, old, []*big.Rat{nativeYumaUint(1), nativeYumaUint(1)})
	if a.err != nil || len(result[0]) != 1 || result[0][0].value.RatString() != "1" || len(result[1]) != 0 {
		t.Fatal("zero previous consensus borrowed current liquid branch", result, a.err)
	}
}

func TestNativeYumaFiniteArithmeticCannotProduceToleranceAfterExhaustion(t *testing.T) {
	if result, err := evaluateNativeYuma(t.Context(), nativeYumaTestInput(), 200, true, 1); err == nil || result != nil {
		t.Fatal("exhausted exact arithmetic returned usable allocation", result, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if result, err := evaluateNativeYuma(ctx, nativeYumaTestInput(), 200, false, 200000); !errors.Is(err, context.Canceled) || result != nil {
		t.Fatal("canceled owner retained original calculation", result, err)
	}
}

func TestNativeYumaStakeOverflowRemainsUnclosed(t *testing.T) {
	input := nativeYumaTestInput()
	input.Nodes[0].Alpha = ^uint64(0)
	input.Nodes[1].Alpha = ^uint64(0)
	if result, err := evaluateNativeYuma(t.Context(), input, 200, false, 200000); err == nil || result != nil {
		t.Fatal("unsupported overflowing source sum was silently saturated", result, err)
	}
}

func TestNativeYumaOriginalExponentialKeepsConstantAndClamping(t *testing.T) {
	a := &nativeYumaArithmetic{context: t.Context(), maximum: 20000}
	if value := a.exp(nativeYumaUint(1)); value.Cmp(new(big.Rat).SetFrac64(22802600, 1<<23)) != 0 {
		t.Fatal("original positive-one exponential lost its exact source constant", value)
	}
	if value := a.exp(new(big.Rat)); value.RatString() != "1" {
		t.Fatal("original zero exponential changed", value)
	}
	if a.exp(nativeYumaUint(21)).Cmp(a.exp(nativeYumaUint(20))) != 0 || a.exp(new(big.Rat).SetInt64(-21)).Cmp(a.exp(new(big.Rat).SetInt64(-20))) != 0 || a.err != nil {
		t.Fatal("original finite exponential clamp changed", a.err)
	}
}

func TestNativeYumaLiquidAlphaUsesOriginalNonzeroSteepness(t *testing.T) {
	input := nativeYumaTestInput()
	input.Steepness = 100
	a := &nativeYumaArithmetic{context: t.Context(), maximum: 10000}
	value := a.liquidAlpha(input, new(big.Rat).SetFrac64(1, 4), new(big.Rat), new(big.Rat).SetFrac64(1, 4))
	if a.err != nil || value.Cmp(new(big.Rat).SetFrac64(438, 1000)) <= 0 || value.Cmp(new(big.Rat).SetFrac64(439, 1000)) >= 0 {
		t.Fatal("original nonzero liquid sigmoid was replaced by a constant coefficient", value, a.err)
	}
}
