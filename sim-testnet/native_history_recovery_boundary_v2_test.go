//go:build linux || darwin

// Early sealed failures preserve partial coverage and the exact fault snapshot.
// Every identity and signature is synthetic; only R47's boundary shape is reused.
package main

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

// writeNativeRecoveryBoundaryTestV2 signs the fixture source and independently
// hashes its retained result. Tests may deliberately make their scopes differ.
func writeNativeRecoveryBoundaryTestV2(t *testing.T, x *nativeHistoryRecoveryTestV2, source *scenarioCampaignAttemptPayload, result *ScenarioResult) {
	t.Helper()
	signed, err := signEvidence(x.f.cfg, scenarioCampaignAttemptEvidenceKind, source.RunID, source, x.f.roles.EVM["testnet-owner"])
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(signed)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(x.p.Terminal.Path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	result.EvidenceHash, err = canonicalScenarioResultHash(result)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(x.p.Result.Path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestNativeHistoryRecoveryV2SealedEarlyFailure admits the same early failure
// geometry as R47 without claiming the planned final block was ever observed.
func TestNativeHistoryRecoveryV2SealedEarlyFailure(t *testing.T) {
	x := newNativeHistoryRecoveryTestV2(t)
	var envelope ReleaseEvidenceEnvelope
	raw, err := os.ReadFile(x.p.Terminal.Path)
	if err != nil || json.Unmarshal(raw, &envelope) != nil {
		t.Fatal("read synthetic sealed source", err)
	}
	var source scenarioCampaignAttemptPayload
	if err := json.Unmarshal(envelope.Payload, &source); err != nil {
		t.Fatal(err)
	}
	var result ScenarioResult
	raw, err = os.ReadFile(x.p.Result.Path)
	if err != nil || json.Unmarshal(raw, &result) != nil {
		t.Fatal("read synthetic failed result", err)
	}
	source.AcceptanceInvalidation = "execution-exited-before-completion"
	b := source.AcceptanceBoundary
	b.LastObservationHead = ChainHead{Number: 8091300, Hash: "0x" + strings.Repeat("ab", 32)}
	b.LastObservationEpoch = 653
	b.AcceptanceWindow.TerminalBlock = 8092324
	b.Faults = []ScenarioFaultRecord{
		{ID: "synthetic-active", Status: "active", AppliedBlock: 8090708, RestoreBlock: 8091445},
		{ID: "synthetic-pending", Status: "pending", TriggerBlock: 8091334},
		{ID: "synthetic-cleanup-after-observation", Status: "restored", AppliedBlock: 8091313, RestoredBlock: 8091333},
	}
	result.EndHead, result.EndEpoch = b.LastObservationHead, b.LastObservationEpoch
	result.AcceptanceWindow = &b.AcceptanceWindow
	result.Faults = cloneScenarioFaultRecords(b.Faults)
	result.Assertions = []AssertionRecord{{ID: "acceptance_interval_observed"}, {ID: "adversary_signed_start_continuity", Passed: true}, {ID: "anomaly_ledger_clean"}, {ID: "process_log_completion"}, {ID: "process_log_publication"}, {ID: "scenario_context"}}
	result.AssertionCount, result.FailedAssertionCount = 6, 5
	writeNativeRecoveryBoundaryTestV2(t, x, &source, &result)
	t.Run("sealed-partial-interval", func(t *testing.T) {
		if _, _, _, err := authenticateNativeRecoveryTerminalV2(t.Context(), x.f.cfg, x.f.stateDir, x.f.plan, x.p.Terminal.Path); err != nil {
			t.Fatal("owner-sealed early failure requires no fabricated terminal coverage", err)
		}
		if b.LastObservationHead.Number >= b.AcceptanceWindow.TerminalBlock || result.Assertions[0].Passed || result.FailedAssertionCount != 5 {
			t.Fatal("partial predecessor was converted into completed coverage")
		}
	})
	for _, fault := range []string{"no-invalidation", "unfinished-result", "unprepared-source", "false-coverage", "failure-inventory", "changed-faults", "changed-boundary", "wrong-start"} {
		t.Run(fault, func(t *testing.T) {
			changedSource, changedResult := source, result
			changedResult.Assertions = append([]AssertionRecord(nil), result.Assertions...)
			switch fault {
			case "no-invalidation":
				changedSource.AcceptanceInvalidation = ""
			case "unfinished-result":
				changedResult.CompletedAt = ""
			case "unprepared-source":
				changedSource.PreparationComplete = false
			case "false-coverage":
				changedResult.Assertions[0].Passed = true
				changedResult.FailedAssertionCount--
			case "failure-inventory":
				changedResult.FailedAssertionCount++
			case "changed-faults":
				changedResult.Faults = nil
			case "changed-boundary":
				changedResult.EndHead.Number++
			case "wrong-start":
				changedResult.StartedAt = "2025-12-31T00:00:00Z"
			}
			writeNativeRecoveryBoundaryTestV2(t, x, &changedSource, &changedResult)
			if _, _, _, err := authenticateNativeRecoveryTerminalV2(t.Context(), x.f.cfg, x.f.stateDir, x.f.plan, x.p.Terminal.Path); err == nil {
				t.Fatal("unfinished or inconsistent early source was admitted")
			}
		})
	}
}

// A missing generation cannot silently ignore an explicit recovery selection.
func TestNativeHistoryRecoveryV2RejectsSelectionWithoutGeneration(t *testing.T) {
	f := newRuntimeEvidenceProvisionV2TestFixture(t)
	f.cfg.nativeHistoryRecoveryV2 = &nativeHistoryRecoverySelectionV2{path: "/synthetic/unissued-handoff", sha256: "0x" + strings.Repeat("ba", 32)}
	if err := attachPolicyRolloverProcessConfigsV2(t.Context(), f.cfg, f.stateDir, f.plan, nil); err == nil {
		t.Fatal("native recovery selection was ignored without generation authority")
	}
}

// Only the locked publication helper may persist native recovery approval.
func TestNativeHistoryRecoveryV2ApplyPreparationDoesNotWrite(t *testing.T) {
	x := newNativeHistoryRecoveryTestV2(t)
	before, err := os.ReadDir(x.f.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	o := cliOptions{Apply: true, ProvisionalResume: true, PlanHash: x.f.plan.PlanHash, NativeRecoveryPlan: "/synthetic/review.json", NativeRecoveryPlanHash: x.p.PlanHash}
	if err := prepareProvisionalResume(t.Context(), x.f.cfg, x.f.stateDir, "native-history-recovery", o, x.f.plan); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadDir(x.f.stateDir)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("native recovery preparation wrote before locked receipt publication", err)
	}
}
