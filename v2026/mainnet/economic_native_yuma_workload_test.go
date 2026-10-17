// Populated source-model cases retain every uid, raw stake, original edge and
// inheritance share. These are deterministic source fixtures, not live sizing.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

func nativeYumaTestWorkload(count int) nativeYumaWorkload {
	return nativeYumaWorkload{Schema: nativeYumaWorkloadSchema, MaximumUids: uint64(count), MaximumValidators: 64, MaximumWeights: uint64(64 * count), MaximumBonds: uint64(64 * count), MaximumParents: uint64(count), MaximumChildren: uint64(count)}
}

func nativeYumaTestPopulated(count int) nativeYumaInput {
	input := nativeYumaTestInput()
	input.Count, input.OwnerUid, input.BondsPenalty = uint16(count), 1, 32768
	input.Nodes = make([]nativeYumaNode, count)
	input.Weights, input.Bonds = make([][]nativeYumaEdge, count), make([][]nativeYumaEdge, count)
	key := func(index int) string { return fmt.Sprintf("0x%064x", index+1) }
	for index := 0; index < count; index++ {
		input.Nodes[index] = nativeYumaNode{Uid: uint16(index), Hotkey: key(index), Registered: 20, LastUpdate: 99, Permit: index < 64, Alpha: 100, Tao: 10, CommitBlock: ^uint64(0),
			Parents:  []nativeYumaShare{{Hotkey: key((index + count - 1) % count), Proportion: ^uint64(0) / 2, Alpha: 100, Tao: 10}},
			Children: []nativeYumaShare{{Hotkey: key((index + 1) % count), Proportion: ^uint64(0) / 2}}}
		if index < 64 {
			for column := 0; column < count; column++ {
				input.Weights[index] = append(input.Weights[index], nativeYumaEdge{Column: uint16(column), Value: 3})
				input.Bonds[index] = append(input.Bonds[index], nativeYumaEdge{Column: uint16(column), Value: 2})
			}
		}
	}
	return input
}

func nativeYumaTestPopulatedPair(t *testing.T, count int, branch string) {
	t.Helper()
	input := nativeYumaTestPopulated(count)
	input.Yuma3, input.LiquidAlpha = branch != "legacy", branch == "liquid"
	input.Steepness = 100
	workload := nativeYumaTestWorkload(count)
	forecast, err := workload.forecast(15 * 1024 * 1024)
	if err != nil {
		t.Fatal(err)
	}
	policy := nativeYumaPolicy{Schema: nativeYumaSchema, LayoutSha256: monitorReadDigest([]byte(nativeYumaLayout)), ReviewSha256: monitorReadDigest([]byte("synthetic populated complete source review")), MaximumWitnessBytes: 15 * 1024 * 1024, HotBlockReserve: 2, MaximumEdges: forecast.ReservedEdges, MaximumOperations: forecast.ReservedOperations, Workload: &workload}
	if err := policy.validate(); err != nil {
		t.Fatal(err)
	}
	if err := policy.admitWorkload(input); err != nil {
		t.Fatal(err)
	}
	fixed, err := evaluateNativeYuma(t.Context(), input, uint64(count)*1024, false, forecast.PairOperations)
	if err != nil {
		t.Fatal("complete populated fixed lane failed", count, branch, err)
	}
	reference, err := evaluateNativeYuma(t.Context(), input, uint64(count)*1024, true, forecast.PairOperations-fixed.steps)
	if err != nil {
		t.Fatal("complete populated rational lane failed", count, branch, err)
	}
	if len(fixed.serverAlpha) != count || len(reference.serverAlpha) != count || fixed.steps+reference.steps+uint64(2*count) > forecast.PairOperations || forecast.ReservedOperations != 2*forecast.PairOperations || forecast.ReservedEdges != 2*uint64(128*count+2*count) {
		t.Fatal("complete populated pair borrowed its twofold logical reserve", fixed.steps, reference.steps, forecast)
	}
	for index := range input.Nodes {
		if fixed.stake[index].Sign() <= 0 || reference.stake[index].Sign() <= 0 || fixed.serverAlpha[index].Sign() <= 0 || reference.serverAlpha[index].Sign() <= 0 || (index < 64) != (fixed.active[index].Sign() > 0) || (index < 64) != (fixed.validatorAlpha[index].Sign() > 0) {
			t.Fatal("populated census was reduced to zero-stake or zero-allocation padding", count, branch, index)
		}
	}
	t.Logf("complete populated source pair uids=%d validators=64 branch=%s fixed_operations=%d rational_operations=%d pair_forecast=%d reserved_operations=%d aggregate_edges=%d reserved_edges=%d host_capacity=unmeasured", count, branch, fixed.steps, reference.steps, forecast.PairOperations, forecast.ReservedOperations, forecast.Edges, forecast.ReservedEdges)
}

func TestNativeYumaPopulated64By1024Legacy(t *testing.T) {
	nativeYumaTestPopulatedPair(t, 1024, "legacy")
}
func TestNativeYumaPopulated64By2048Legacy(t *testing.T) {
	nativeYumaTestPopulatedPair(t, 2048, "legacy")
}
func TestNativeYumaPopulated64By1024Yuma3(t *testing.T) {
	nativeYumaTestPopulatedPair(t, 1024, "yuma3")
}
func TestNativeYumaPopulated64By2048Yuma3(t *testing.T) {
	nativeYumaTestPopulatedPair(t, 2048, "yuma3")
}
func TestNativeYumaPopulated64By1024Liquid(t *testing.T) {
	nativeYumaTestPopulatedPair(t, 1024, "liquid")
}
func TestNativeYumaPopulated64By2048Liquid(t *testing.T) {
	nativeYumaTestPopulatedPair(t, 2048, "liquid")
}

func TestNativeYumaWorkloadRequiresTwofoldAggregateAndCompleteShares(t *testing.T) {
	workload := nativeYumaTestWorkload(2048)
	forecast, err := workload.forecast(15 * 1024 * 1024)
	if err != nil || forecast.ReservedEdges != 532480 || forecast.ReservedOperations <= 64000000 || forecast.ReservedOperations != 2*forecast.PairOperations || forecast.ReservedWitnessBytes != 30*1024*1024 || !strings.Contains(forecast.Authority, "unmeasured") {
		t.Fatal("full original twofold forecast differs", forecast, err)
	}
	policy := nativeYumaPolicy{Schema: nativeYumaSchema, LayoutSha256: monitorReadDigest([]byte(nativeYumaLayout)), ReviewSha256: monitorReadDigest([]byte("synthetic bounded review")), MaximumWitnessBytes: 15 * 1024 * 1024, HotBlockReserve: 2, MaximumEdges: forecast.ReservedEdges, MaximumOperations: forecast.ReservedOperations, Workload: &workload}
	if err := policy.validate(); err != nil {
		t.Fatal("complete populated prerequisite", err)
	}
	for _, mutate := range []func(*nativeYumaPolicy){
		func(value *nativeYumaPolicy) {
			value.MaximumEdges = 2 * (workload.MaximumWeights + workload.MaximumBonds)
		},
		func(value *nativeYumaPolicy) { value.MaximumOperations = forecast.PairOperations },
		func(value *nativeYumaPolicy) { value.HotBlockReserve = 1 },
		func(value *nativeYumaPolicy) { value.MaximumWitnessBytes = forecast.MinimumWitnessBytes - 1 },
	} {
		changed := policy
		mutate(&changed)
		if err := changed.validate(); !errors.Is(err, errMonitorEconomicCapacity) {
			t.Fatal("underprovisioned complete workload acquired authority", changed, err)
		}
	}
	legacy := policy
	legacy.Workload = nil
	if err := legacy.validate(); err == nil {
		t.Fatal("legacy authority silently acquired populated limits")
	}
	legacy.MaximumEdges, legacy.MaximumOperations = 64, 200000
	if raw, err := json.Marshal(legacy); err != nil || strings.Contains(string(raw), "populated_workload") {
		t.Fatal("nil workload changed legacy wire grammar", err)
	}
	input := nativeYumaTestPopulated(2048)
	if err := policy.admitWorkload(input); err != nil {
		t.Fatal(err)
	}
	input.Nodes[100].Permit = true
	if err := policy.admitWorkload(input); !errors.Is(err, errMonitorEconomicCapacity) {
		t.Fatal("additional original validator row bypassed complete admission", err)
	}
	input.Nodes[100].Permit = false
	input.Nodes[100].Parents = append(input.Nodes[100].Parents, nativeYumaShare{Hotkey: "0x" + strings.Repeat("a", 64), Alpha: 1})
	if err := policy.admitWorkload(input); !errors.Is(err, errMonitorEconomicCapacity) {
		t.Fatal("inherited share escaped aggregate authority", err)
	}
}

// This dense reference follows retained original interpolate_sparse exactly.
// Different nonzero signs exercise fixed floor before division; missing cells
// and explicit zero entries exercise the sparse support equivalence.
func TestNativeYumaSparseInterpolationEqualsOriginalDenseOrder(t *testing.T) {
	weights := nativeYumaMatrix{{{column: 0, value: new(big.Rat).SetFrac64(1, 3)}, {column: 2, value: new(big.Rat)}, {column: 4, value: new(big.Rat).SetFrac64(7, 11)}}, {}, {{column: 1, value: new(big.Rat).SetFrac64(1, 1<<31)}}}
	clipped := nativeYumaMatrix{{{column: 0, value: new(big.Rat).SetFrac64(1, 7)}, {column: 2, value: new(big.Rat)}}, {}, {}}
	for _, exact := range []bool{false, true} {
		for _, penalty := range []uint16{1, 32768, 65534} {
			a, b := &nativeYumaArithmetic{context: t.Context(), exact: exact, maximum: 100000}, &nativeYumaArithmetic{context: t.Context(), exact: exact, maximum: 100000}
			got := a.interpolate(weights, clipped, penalty)
			want := make(nativeYumaMatrix, len(weights))
			ratio := b.div(nativeYumaUint(uint64(penalty)), nativeYumaUint(65535))
			for row := range weights {
				for column := 0; column < 5; column++ {
					old := nativeYumaCellAt(weights[row], column)
					value := b.add(old, b.mul(ratio, b.sub(nativeYumaCellAt(clipped[row], column), old)))
					if value.Sign() > 0 {
						want[row] = append(want[row], nativeYumaCell{column: column, value: value})
					}
				}
			}
			if a.err != nil || b.err != nil || !reflect.DeepEqual(got, want) {
				t.Fatal("sparse interpolation changed original signed rounding/support", exact, penalty, got, want, a.err, b.err)
			}
		}
	}
}

func TestNativeYumaSparseEmaEqualsOriginalDenseOrder(t *testing.T) {
	input := nativeYumaTestInput()
	input.Count = 5
	input.MovingAverage = 333333
	fresh := nativeYumaMatrix{{{column: 0, value: new(big.Rat).SetFrac64(1, 3)}, {column: 4, value: new(big.Rat)}}, {}, {{column: 3, value: new(big.Rat).SetFrac64(9, 11)}}, {}, {}}
	old := nativeYumaMatrix{{{column: 1, value: new(big.Rat).SetFrac64(1, 7)}, {column: 4, value: new(big.Rat).SetFrac64(1, 9)}}, {{column: 2, value: nativeYumaUint(1)}}, {{column: 3, value: new(big.Rat).SetFrac64(1, 13)}}, {}, {}}
	for _, exact := range []bool{false, true} {
		a, b := &nativeYumaArithmetic{context: t.Context(), exact: exact, maximum: 100000}, &nativeYumaArithmetic{context: t.Context(), exact: exact, maximum: 100000}
		got := a.ema(input, fresh, old, nativeYumaZeros(5))
		want := make(nativeYumaMatrix, 5)
		moving := b.q64(new(big.Rat).Quo(b.q64(nativeYumaUint(input.MovingAverage)), nativeYumaUint(1000000)))
		alpha := b.sub(nativeYumaUint(1), b.q32(moving))
		for row := range fresh {
			values := nativeYumaZeros(5)
			for _, cell := range fresh[row] {
				values[cell.column] = b.add(values[cell.column], b.mul(alpha, cell.value))
			}
			for _, cell := range old[row] {
				values[cell.column] = b.add(values[cell.column], b.mul(b.sub(nativeYumaUint(1), alpha), cell.value))
			}
			for column, value := range values {
				if value.Sign() > 0 {
					want[row] = append(want[row], nativeYumaCell{column: column, value: value})
				}
			}
		}
		if a.err != nil || b.err != nil || !reflect.DeepEqual(got, want) {
			t.Fatal("sparse EMA changed original fresh-before-old arithmetic or old-only support", exact, got, want, a.err, b.err)
		}
	}
}

func TestNativeYumaPopulatedCapacityAndCancellationRemainTyped(t *testing.T) {
	input := nativeYumaTestPopulated(2048)
	if result, err := evaluateNativeYuma(t.Context(), input, 2048*1024, false, 1024); result != nil || !errors.Is(err, errMonitorEconomicCapacity) || errors.Is(nativeExecutionDerivationError(err), errRpcIntegrity) {
		t.Fatal("finite populated work exhaustion became partial arithmetic or integrity", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if result, err := evaluateNativeYuma(ctx, input, 2048*1024, true, nativeYumaPopulatedMaximumOperations); result != nil || !errors.Is(err, context.Canceled) || errors.Is(nativeExecutionDerivationError(err), errRpcIntegrity) {
		t.Fatal("canceled populated owner acquired integrity or completed arithmetic", err)
	}
}

// Cancellation is triggered by the owned arithmetic's second context check,
// after work has started. No timeout or scheduler race supplies causality.
type nativeYumaCancelDuringWork struct {
	context.Context
	checks atomic.Uint64
	cancel context.CancelFunc
}

func (self *nativeYumaCancelDuringWork) Err() error {
	if self.checks.Add(1) == 2 {
		self.cancel()
	}
	return self.Context.Err()
}
func TestNativeYumaPopulatedCancellationInterruptsChargedWork(t *testing.T) {
	base, cancel := context.WithCancel(t.Context())
	defer cancel()
	ctx := &nativeYumaCancelDuringWork{Context: base, cancel: cancel}
	result, err := evaluateNativeYuma(ctx, nativeYumaTestPopulated(2048), 2048*1024, true, 2048)
	if result != nil || !errors.Is(err, context.Canceled) || ctx.checks.Load() != 2 || errors.Is(nativeExecutionDerivationError(err), errRpcIntegrity) {
		t.Fatal("populated cancellation did not interrupt owned charged work", result, err, ctx.checks.Load())
	}
}
