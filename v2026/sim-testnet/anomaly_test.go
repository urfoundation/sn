package main

import (
	"strings"
	"testing"
	"time"
)

func TestScenarioAnomalyLedgerAllowsOnlyExactScheduledRestarts(t *testing.T) {
	cfg := testResolvedConfig(t)
	start := testScenarioObservation(cfg, 1)
	current := testScenarioObservation(cfg, 2)
	start.Status.Supervisor = &SupervisorState{Processes: []ProcessState{{ID: "miner-1", PID: 101, Restarts: 2, Healthy: true}}}
	current.Status.Supervisor = &SupervisorState{Processes: []ProcessState{{ID: "miner-1", PID: 202, Restarts: 3, Healthy: true}}}
	faults := []ScenarioFaultRecord{{
		ID: "scheduled", Kind: "process-restart", Targets: []string{"miner-1"}, Status: "restored",
		AppliedBlock: 101, RestoredBlock: 102,
	}}
	ledger := buildScenarioAnomalyLedger("run", time.Date(2026, 8, 21, 1, 2, 3, 0, time.UTC), start, current, nil, faults, nil)
	if ledger.Status != "clean" || len(ledger.Entries) != 0 {
		t.Fatalf("scheduled restart produced anomalies: %+v", ledger)
	}

	current.Status.Supervisor.Processes[0].Restarts++
	ledger = buildScenarioAnomalyLedger("run", time.Now(), start, current, nil, faults, nil)
	if ledger.Status != "open" || !hasAnomalyClass(ledger, "unexpected-restart") {
		t.Fatalf("excess restart was not reported: %+v", ledger)
	}
}

func TestScenarioAnomalyLedgerCapturesEveryUnexpectedEvidenceChannel(t *testing.T) {
	cfg := testResolvedConfig(t)
	start := testScenarioObservation(cfg, 1)
	current := testScenarioObservation(cfg, 2)
	start.Status.Supervisor = &SupervisorState{Processes: []ProcessState{{ID: "miner-1", PID: 101, Healthy: true}}}
	current.Status.Supervisor = &SupervisorState{Processes: []ProcessState{
		{ID: "miner-1", PID: 202, Restarts: 2, Healthy: true},
		{ID: "miner-2", PID: 303, Healthy: false, ExitError: "crashed"},
	}}
	current.Status.Warnings = []string{"RPC view drift"}
	current.Operators = []OperatorObservation{{NoID: 1, APIURL: "http://127.0.0.1:8080", StatusCode: 503, Error: "timeout"}}
	current.Validators = []ValidatorObservation{{ValidatorID: 1, Error: "weight reveal failed"}}
	current.Claims = []ClaimObservation{{MinerID: 1, NoID: 1, Uncertain: 1}}
	current.NativeCustodyError = "reserve mismatch"
	assertions := []AssertionRecord{{ID: "conservation", Passed: false, Message: "captured != paid + outstanding", CompletedAt: current.ObservedAt}}
	faults := []ScenarioFaultRecord{{ID: "scheduled", Kind: "process-restart", Targets: []string{"miner-1"}, Status: "restored", AppliedBlock: 101, RestoredBlock: 102}}
	adversaries := &AdversaryCampaignEvidence{
		Actors:  []AdversaryActorEvidence{{ID: "rpc-consistency-pressure", Errors: 1, ErrorRatePPM: 10, LastDetail: "endpoint mismatch"}},
		Vectors: []AdversaryVectorEvidence{{ID: "rpc-finality-equivocation", Status: "fail", ConcurrentCoverage: "live-exercised", SampleFloor: 100, Errors: 1}},
	}
	ledger := buildScenarioAnomalyLedger("run", time.Now(), start, current, assertions, faults, adversaries)
	for _, class := range []string{
		"assertion-failure", "deployment-warning", "operator-health", "operator-error",
		"validator-error", "claim-terminal-state", "native-custody-error", "process-health",
		"process-exit", "process-topology", "unexpected-restart", "adversary-actor-error",
		"adversary-vector-failure",
	} {
		if !hasAnomalyClass(ledger, class) {
			t.Errorf("missing anomaly class %q: %+v", class, ledger.Entries)
		}
	}
	if ledger.Status != "open" {
		t.Fatalf("ledger status = %q", ledger.Status)
	}
	for _, entry := range ledger.Entries {
		if !strings.HasPrefix(entry.ID, "ANOM-") || entry.Status != "open" || entry.FirstObservedAt == "" {
			t.Errorf("invalid anomaly entry: %+v", entry)
		}
	}
}

func TestScenarioAnomalyLedgerRetainsRecoveredIntermediateWarning(t *testing.T) {
	cfg := testResolvedConfig(t)
	start := testScenarioObservation(cfg, 1)
	middle := testScenarioObservation(cfg, 1)
	current := testScenarioObservation(cfg, 2)
	middle.ObservationHash = "transient-observation"
	middle.Status.Warnings = []string{"private RPC timed out and recovered on retry"}
	ledger := buildScenarioAnomalyLedger("run", time.Now(), start, current, nil, nil, nil, start, middle, current)
	if ledger.Status != "open" || !hasAnomalyClass(ledger, "deployment-warning") {
		t.Fatalf("recovered warning disappeared from append-only ledger: %+v", ledger)
	}
	foundHash := false
	for _, entry := range ledger.Entries {
		if entry.Class == "deployment-warning" && entry.ObservationHash == middle.ObservationHash {
			foundHash = true
		}
	}
	if !foundHash {
		t.Fatalf("recovered warning did not retain its first observation hash: %+v", ledger.Entries)
	}
}

func TestScenarioAnomalyLedgerRetainsRecoveredContractInvariantFailure(t *testing.T) {
	cfg := testResolvedConfig(t)
	start := testScenarioObservation(cfg, 1)
	middle := testScenarioObservation(cfg, 1)
	current := testScenarioObservation(cfg, 2)
	middle.ObservationHash = "transient-contract-invariant"
	middle.Status.PolicyHash = cfg.PolicyHash
	middle.Status.Contracts.PolicyHash = "0x" + strings.Repeat("00", 32)
	middle.Status.Contracts.RuntimeCodeMatches = false
	middle.Status.Contracts.ConservationHolds = false
	middle.Status.Contracts.TotalCaptured = "11"

	ledger := buildScenarioAnomalyLedger("run", time.Now(), start, current, nil, nil, nil, start, middle, current)
	for _, class := range []string{"contract-runtime-drift", "contract-policy-drift", "value-conservation"} {
		if !hasAnomalyClass(ledger, class) {
			t.Errorf("recovered invariant failure %q disappeared: %+v", class, ledger.Entries)
		}
	}
	for _, entry := range ledger.Entries {
		if (entry.Class == "contract-runtime-drift" || entry.Class == "contract-policy-drift" || entry.Class == "value-conservation") && entry.ObservationHash != middle.ObservationHash {
			t.Errorf("invariant anomaly lost the failing observation hash: %+v", entry)
		}
	}
}

func TestScenarioAnomalyLedgerRejectsFinalityEpochAndTopologyRegression(t *testing.T) {
	cfg := testResolvedConfig(t)
	start := testScenarioObservation(cfg, 1)
	equivocation := testScenarioObservation(cfg, 1)
	regression := testScenarioObservation(cfg, 0)
	equivocation.Status.Contracts.FinalizedHead.Hash = "0x" + strings.Repeat("cd", 32)
	regression.Status.Contracts.FinalizedHead.Number = start.Status.Contracts.FinalizedHead.Number - 1
	regression.Status.Contracts.CurrentEpoch = 0
	regression.Status.Contracts.OperatorCount++

	ledger := buildScenarioAnomalyLedger("run", time.Now(), start, regression, nil, nil, nil, start, equivocation, regression)
	for _, class := range []string{"finality-equivocation", "finality-regression", "epoch-regression", "operator-topology-drift"} {
		if !hasAnomalyClass(ledger, class) {
			t.Errorf("missing monotonicity anomaly %q: %+v", class, ledger.Entries)
		}
	}
}

func TestScenarioAnomalyLedgerAttributesScheduledTransientDegradation(t *testing.T) {
	cfg := testResolvedConfig(t)
	start := testScenarioObservation(cfg, 1)
	middle := testScenarioObservation(cfg, 1)
	current := testScenarioObservation(cfg, 2)
	start.Status.Supervisor = &SupervisorState{Processes: []ProcessState{{ID: "miner-1", PID: 101, Healthy: true}}}
	middle.Status.Healthy = false
	middle.Status.Supervisor = &SupervisorState{Processes: []ProcessState{{ID: "miner-1", PID: 101, Healthy: false}}}
	middle.ExpectedFaultIDs = []string{"scheduled-pause"}
	middle.ExpectedFaultTargets = []string{"miner-1"}
	middle.Claims = []ClaimObservation{{MinerID: 1, NoID: 1, Error: "provider is intentionally paused"}}
	current.Status.Supervisor = &SupervisorState{Processes: []ProcessState{{ID: "miner-1", PID: 101, Healthy: true}}}
	faults := []ScenarioFaultRecord{{ID: "scheduled-pause", Kind: "process-pause", Targets: []string{"miner-1"}, Status: "restored", AppliedBlock: 101, RestoredBlock: 102}}
	ledger := buildScenarioAnomalyLedger("run", time.Now(), start, current, nil, faults, nil, start, middle, current)
	if ledger.Status != "clean" || len(ledger.Entries) != 0 {
		t.Fatalf("scheduled and attributed degradation produced anomalies: %+v", ledger)
	}
}

func TestAttachScenarioAnomalyGateIsIdempotent(t *testing.T) {
	cfg := testResolvedConfig(t)
	observation := testScenarioObservation(cfg, 1)
	now := time.Now().UTC()
	result := &ScenarioResult{RunID: "run", StartedAt: now.Add(-time.Minute).Format(time.RFC3339Nano), Assertions: []AssertionRecord{{ID: "base", Passed: true}}}
	attachScenarioAnomalyGate(result, now, observation, observation)
	attachScenarioAnomalyGate(result, now, observation, observation)
	count := 0
	for _, assertion := range result.Assertions {
		if assertion.ID == anomalyGateAssertionID {
			count++
		}
	}
	if count != 1 || result.Result != "pass" || result.Anomalies == nil || result.Anomalies.Status != "clean" {
		t.Fatalf("idempotent gate result = %+v", result)
	}
}

func hasAnomalyClass(ledger *ScenarioAnomalyLedger, class string) bool {
	for _, entry := range ledger.Entries {
		if entry.Class == class {
			return true
		}
	}
	return false
}
