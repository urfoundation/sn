// The rpc event fixture is bound to actual exported original-Wasm emissions;
// the public producer still executes and checks the same program independently.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"
)

type nativeYumaOriginalEventsFixture struct {
	RuntimeCodeSha256 historicalReplayDigest `json:"runtime_code_sha256"`
	Uids              int                    `json:"uids"`
	Validators        int                    `json:"validators"`
	Tranche           uint64                 `json:"tranche"`
	Emissions         []uint64               `json:"emissions"`
}

func nativeYumaTestOriginalEvents(t *testing.T, job historicalReplayJob, count int) *nativeYumaOriginalEventsFixture {
	t.Helper()
	path := os.Getenv("URNETWORK_NATIVE_YUMA_POPULATED_EXPECTATION")
	if path == "" {
		return nil
	}
	raw, _, err := readPlanFile(t.Context(), path, maxRpcReplyBytes)
	if err != nil {
		t.Fatal(err)
	}
	var result nativeYumaOriginalEventsFixture
	if err := decodePlanJson(raw, &result); err != nil || result.RuntimeCodeSha256 != job.RuntimeCodeSha256 || result.Uids != count || result.Validators != 64 || result.Tranche != uint64(count)*512 || len(result.Emissions) != count {
		t.Fatal("populated original event fixture changed its program or complete census", err)
	}
	for _, amount := range result.Emissions {
		if amount == 0 {
			t.Fatal("populated original event fixture omitted a nonzero miner")
		}
	}
	return &result
}

func economicConservationTestPopulatedYuma(t *testing.T, count int) {
	t.Helper()
	f, _ := newEconomicConservationPrincipalFixture(t, fmt.Sprintf("yuma-populated-%d", count), true, nil)
	first := f.sample(t, monitorServiceHooks{})
	if first.Yuma == nil || !first.Yuma.Current || len(first.Yuma.Active) != 1 || len(first.Yuma.Active[0].Allocations) != count || len(first.Yuma.Active[0].Records) != 2+3*count || first.Yuma.MinerDenominator == nil || first.FullQuantizationToleranceAlpha == nil || first.NativeHeld || !first.NativeCurrent || !first.VaultCurrent {
		t.Fatal("complete populated original allocation failed public consumption", count, first)
	}
	value := first.Yuma.Active[0]
	for index, allocation := range value.Allocations {
		if allocation.ActualMiner == "0" || allocation.ReferenceMiner == "0" || (index < 64) != (allocation.ActualValidator != "0") {
			t.Fatal("complete populated original public census was trimmed or padded", index, allocation)
		}
	}
	forecast, err := value.Authority.Workload.forecast(value.Authority.MaximumWitnessBytes)
	if err != nil || forecast.ReservedEdges != 2*uint64(130*count) || value.Authority.MaximumOperations < forecast.ReservedOperations || value.Authority.HotBlockReserve != 2 {
		t.Fatal("public original populated authority lost twofold aggregate forecast", forecast, err)
	}
	state := f.source.state(t)
	actualBytes, err := nativeYumaWitnessBytes(state.Yuma[0].Projection, state.Yuma[0].Outcome)
	if err != nil || actualBytes > value.Authority.MaximumWitnessBytes || 2*actualBytes+64*1024 > first.Resources.headBytes() || first.FactsRemaining == 0 {
		t.Fatal("actual complete populated witness cannot fit both admitted hot slots", actualBytes, forecast, err)
	}
	raw, err := os.ReadFile(f.source.checkpoint)
	if err != nil || len(raw) <= maxRpcReplyBytes || 2*uint64(len(raw)) > first.Resources.headBytes() {
		t.Fatal("populated original owner did not exercise complete physical sizing", len(raw), err)
	}
	if count == 2048 {
		economicConservationTestRestoreLargeHead(t, f.source.policy, raw)
	}
	f.reset(t)
	plan, args := f.plan(t)
	if plan.RequiredBytes < 2*(uint64(len(raw))+2*economicConservationStorageMaximum) {
		t.Fatal("populated original archive omitted full twofold publication forecast", plan)
	}
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal("populated original archive failed", code, issue)
	}
	second := f.sample(t, monitorServiceHooks{})
	if second.Yuma == nil || !second.Yuma.Current || second.Yuma.Archived == nil || second.Yuma.Archived.Blocks != 1 || len(second.Yuma.Active) != 0 || *second.Yuma.MinerDenominator != *first.Yuma.MinerDenominator || *second.FullQuantizationToleranceAlpha != *first.FullQuantizationToleranceAlpha || second.ActualNativeOutcomeVerified || second.ActivationReady {
		t.Fatal("populated original cold restore lost arithmetic or manufactured economic authority", second)
	}
	// This populated capacity program deliberately pays every UID, including
	// unclassified recipients. Unknown membership alone proves no violation.
	// Its complete original owner branch nevertheless receives far below90%.
	owner, err := monitorEconomicInteger(second.Execution.OwnerRecycled)
	if err != nil {
		t.Fatal(err)
	}
	denominator, err := monitorEconomicInteger(*second.Yuma.MinerDenominator)
	if err != nil {
		t.Fatal(err)
	}
	owner.Mul(owner, big.NewInt(10))
	if owner.Cmp(denominator) >= 0 || second.Conformance == nil || second.Conformance.OwnerRecycleWithinTolerance == nil || *second.Conformance.OwnerRecycleWithinTolerance || second.Execution.ResidualEntitlement == "0" || second.TargetMet == nil || *second.TargetMet || second.Conformance.CompleteEvidence {
		t.Fatal("complete populated owner bound was hidden or partial membership became authority", second)
	}
	t.Logf("actual populated witness uids=%d validators=64 wire_bytes=%d head_bytes=%d twofold_witness_bytes=%d twofold_edge_reserve=%d twofold_operation_reserve=%d actual_host_capacity=unmeasured", count, actualBytes, len(raw), forecast.ReservedWitnessBytes, forecast.ReservedEdges, forecast.ReservedOperations)
}

func TestEconomicConservationPublicPopulated64By1024CompleteArchive(t *testing.T) {
	economicConservationTestPopulatedYuma(t, 1024)
}
func TestEconomicConservationPublicPopulated64By2048CompleteArchiveRestore(t *testing.T) {
	economicConservationTestPopulatedYuma(t, 2048)
}

func TestEconomicConservationPublicPopulatedUnderprovisioningRefusesBeforeSource(t *testing.T) {
	f, producer := newEconomicConservationPrincipalFixture(t, "yuma-populated-2048", true, nil)
	// The signed logical workload is valid. Only the combined physical head is
	// reduced here, so the public preflight must name the missing full reserve.
	f.source.policy.Continuation = nil
	f.source.policy.StorageProfile = nil
	f.source.policy.MaximumFacts = 8192
	f.source.writePolicy(t)
	reads, requests := f.source.claimReads.Load(), producer.requests.Load()
	var output, issue bytes.Buffer
	code := runMainWithMonitorHooks(f.ctx, f.source.args(t), &output, &issue, func() time.Time { return f.source.now }, monitorServiceHooks{})
	if code != 2 || output.Len() != 0 || f.source.claimReads.Load() != reads || producer.requests.Load() != requests || !strings.Contains(issue.String(), "complete witness reserve") {
		t.Fatal("public populated workload borrowed an unprovisioned physical profile", code, issue.String())
	}
}

func TestEconomicConservationPublicPopulatedWorkloadSignatureCannotBeReused(t *testing.T) {
	f, producer := newEconomicConservationPrincipalFixture(t, "yuma-populated-1024", true, nil)
	// Keep a valid logical envelope while changing its independent authority.
	producer.authority.Yuma.Workload.MaximumParents++
	producer.authority.Yuma.MaximumEdges += 2
	forecast, err := producer.authority.Yuma.Workload.forecast(producer.authority.Yuma.MaximumWitnessBytes)
	if err != nil {
		t.Fatal(err)
	}
	producer.authority.Yuma.MaximumOperations = forecast.ReservedOperations
	raw, err := json.Marshal(producer.authority)
	if err != nil {
		t.Fatal(err)
	}
	reference := &producer.source.policy.Execution.Producer.Authority
	if err := os.WriteFile(reference.Path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	reference.Sha256 = monitorReadDigest(raw)
	f.source.policy.Native.Observation.Execution.Yuma = producer.authority.Yuma
	f.source.writePolicy(t)
	var output, issue bytes.Buffer
	code := runMainWithMonitorHooks(f.ctx, f.source.args(t), &output, &issue, func() time.Time { return f.source.now }, monitorServiceHooks{})
	var summary economicConservationSummary
	if err := json.Unmarshal(output.Bytes(), &summary); err != nil {
		t.Fatal(code, issue.String(), err)
	}
	if code != 3 || !summary.NativeHeld || summary.NativeCurrent || !summary.VaultCurrent || summary.NativeCursor != f.source.policy.Native.Observation.From || summary.TargetMet != nil || !strings.Contains(summary.NativeIssue, "native producer independent authority signature is invalid") {
		t.Fatal("changed populated workload borrowed original producer signature or held healthy siblings", code, summary)
	}
}
