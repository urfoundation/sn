// Synthetic observations distinguish old repair history, ordinary in-flight
// work and genuine uncertainty inside a signed settlement interval.
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// Every other anomaly surface is healthy, so the claim scope is observable.
func claimAnomalyScopeFixture(t *testing.T) (*ScenarioResult, *ScenarioObservation, *ScenarioObservation) {
	t.Helper()
	cfg := testResolvedConfig(t)
	start, current := testScenarioObservation(cfg, 100), testScenarioObservation(cfg, 102)
	for _, observation := range []*ScenarioObservation{start, current} {
		observation.Claims = []ClaimObservation{{MinerID: 1, NoID: 1, LastDiscovered: 101, Uncertain: 2, Failed: 1,
			EpochOutcomes: []ClaimEpochObservation{{Epoch: 99, Status: "uncertain"}, {Epoch: 100, Status: "no-claim"}, {Epoch: 101, Status: "finalized"}},
		}}
		var err error
		observation.ObservationHash, err = canonicalHashHex(observation)
		if err != nil {
			t.Fatal(err)
		}
	}
	return &ScenarioResult{RunID: "synthetic-claim-scope", StartedAt: start.ObservedAt, AcceptanceWindow: &ScenarioAcceptanceWindow{FirstEpoch: 100, EpochCount: 2}}, start, current
}

// Complete current outcomes do not erase historical incidents or turn them
// into new current failures. Non-window callers retain lifetime semantics.
func TestClaimAnomalyScopePreservesHistoryOutsideWindow(t *testing.T) {
	result, start, current := claimAnomalyScopeFixture(t)
	before, err := json.Marshal([]*ScenarioObservation{start, current})
	if err != nil {
		t.Fatal(err)
	}
	attachScenarioAnomalyGate(result, time.Now(), start, current, start, current)
	if result.Result != "pass" || result.Anomalies.Status != "clean" || len(result.Anomalies.Entries) != 0 {
		t.Fatalf("old queue failures became current anomalies: %+v", result.Anomalies)
	}
	after, err := json.Marshal([]*ScenarioObservation{start, current})
	if err != nil || string(before) != string(after) {
		t.Fatalf("window projection changed raw history: %v", err)
	}
	result.AcceptanceWindow = nil
	attachScenarioAnomalyGate(result, time.Now(), start, current, start, current)
	if result.Result != "fail" || !hasAnomalyClass(result.Anomalies, "claim-terminal-state") {
		t.Fatal("legacy lifetime caller lost its existing anomaly policy")
	}
}

// A queue status alone cannot resolve a real uncertainty or failure. Its first
// observed identity remains open even after a later row reports completion.
func TestClaimAnomalyScopeKeepsCurrentIncidentsOpen(t *testing.T) {
	for _, status := range []string{"uncertain", "failed"} {
		result, start, current := claimAnomalyScopeFixture(t)
		start.Claims[0].EpochOutcomes[1].Status = status
		attachScenarioAnomalyGate(result, time.Now(), start, current, start, current)
		if result.Result != "fail" || len(result.Anomalies.Entries) != 1 || result.Anomalies.Entries[0].Class != "claim-terminal-state" || result.Anomalies.Entries[0].Status != "open" || result.Anomalies.Entries[0].ObservationHash != start.ObservationHash {
			t.Fatalf("%s current incident was hidden or falsely reconciled: %+v", status, result.Anomalies)
		}
	}
}

// Ordinary asynchronous progress may complete successfully. An unfinished
// terminal submission is still rejected by the actual payout coverage gate.
func TestClaimAnomalyScopeAllowsOrdinaryProgressWithoutAcceptingPendingPayout(t *testing.T) {
	for _, status := range []string{"pending", "retry", "submitting"} {
		result, start, current := claimAnomalyScopeFixture(t)
		start.Claims[0].EpochOutcomes[1].Status = status
		attachScenarioAnomalyGate(result, time.Now(), start, current, start, current)
		if result.Result != "pass" {
			t.Fatalf("ordinary %s became a permanent anomaly: %+v", status, result.Anomalies)
		}
		e := scenarioClaimWindowTestEvaluation()
		e.Current.Claims[2].EpochOutcomes[1].Status = status
		if ok, _ := scenarioClaimWindowTestCheck(t, e, "claims_finalized_per_no"); ok {
			t.Fatalf("unfinished terminal %s satisfied claim coverage", status)
		}
	}
}

// Missing, duplicate and unrecognized current rows remain explicit gaps.
func TestClaimAnomalyScopeRejectsAmbiguousCurrentRows(t *testing.T) {
	tests := []struct {
		name string
		edit func(*ScenarioResult, *ScenarioObservation)
	}{
		{name: "absent projection", edit: func(_ *ScenarioResult, current *ScenarioObservation) { current.Claims[0].EpochOutcomes = nil }},
		{name: "duplicate epoch", edit: func(_ *ScenarioResult, current *ScenarioObservation) {
			current.Claims[0].EpochOutcomes = append(current.Claims[0].EpochOutcomes, current.Claims[0].EpochOutcomes[1])
		}},
		{name: "unknown status", edit: func(_ *ScenarioResult, current *ScenarioObservation) {
			current.Claims[0].EpochOutcomes[1].Status = "invented-success"
		}},
		{name: "overflow", edit: func(result *ScenarioResult, _ *ScenarioObservation) { result.AcceptanceWindow.FirstEpoch = ^uint64(0) }},
		{name: "empty window", edit: func(result *ScenarioResult, _ *ScenarioObservation) { result.AcceptanceWindow.EpochCount = 0 }},
	}
	for _, test := range tests {
		result, start, current := claimAnomalyScopeFixture(t)
		test.edit(result, current)
		attachScenarioAnomalyGate(result, time.Now(), start, current, start, current)
		if result.Result != "fail" || !hasAnomalyClass(result.Anomalies, "claim-observation-gap") {
			t.Fatalf("%s was treated as complete current evidence: %+v", test.name, result.Anomalies)
		}
	}
}

// Stable epoch projection never mutates the caller's census and never hides a
// duplicate contract epoch behind map iteration or deduplication.
func TestClaimAnomalyScopeQueueProjectionIsOrdered(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "runtime", "miner-1", "claims", "claim-queue.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"schema":"urnetwork-provider-claim-queue-v1","last_discovered":101,"entries":{"100":{"epoch":100,"status":"finalized"},"101":{"epoch":101,"status":"no-claim"}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	epochs := []uint64{102, 100, 101}
	cfg := scenarioClaimWindowTestEvaluation().Cfg
	claim := inspectClaimQueue(cfg, dir, 1, epochs...)
	want := []ClaimEpochObservation{{Epoch: 100, Status: "finalized"}, {Epoch: 101, Status: "no-claim"}, {Epoch: 102, Status: "undiscovered"}}
	if claim.Error != "" || !slices.Equal(claim.EpochOutcomes, want) || !slices.Equal(epochs, []uint64{102, 100, 101}) {
		t.Fatalf("projection changed scope or caller census: %+v %v", claim, epochs)
	}
	if duplicate := inspectClaimQueue(cfg, dir, 1, 100, 100); duplicate.Error == "" {
		t.Fatal("duplicate contract epoch accepted")
	}
}

// A release caller may not accidentally choose legacy lifetime acceptance by
// omitting its signed window. Non-release legacy callers remain compatible.
func TestClaimAnomalyScopeReleaseRequiresWindow(t *testing.T) {
	for _, phase := range []string{"release-1.0", "production-soak"} {
		e := scenarioClaimWindowTestEvaluation()
		e.Window = nil
		e.Definition.Name = phase
		if _, err := scenarioClaimsForAcceptance(e); err == nil {
			t.Fatalf("%s silently selected legacy lifetime claim scope", phase)
		}
	}
}
