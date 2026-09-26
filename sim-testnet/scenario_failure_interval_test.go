// The real signed controller must stop after the last required transition,
// while preserving every strict failure and joining its adversary workers.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Return an exact synthetic target census; no process or service is touched.
type failedIntervalFaultDriver struct {
	fakeFaultDriver
}

func (self *failedIntervalFaultDriver) Apply(_ context.Context, spec scenarioFaultSpec) ([]FaultProcessEvidence, error) {
	self.applied = append(self.applied, spec.ID)
	var processes []FaultProcessEvidence
	for i, target := range spec.Targets {
		processes = append(processes, FaultProcessEvidence{ID: target, Role: "fixture", Identity: target, PID: 100 + i})
	}
	return processes, nil
}

func (self *failedIntervalFaultDriver) Restore(_ context.Context, spec scenarioFaultSpec) ([]FaultProcessEvidence, error) {
	self.restored = append(self.restored, spec.ID)
	var processes []FaultProcessEvidence
	for i, target := range spec.Targets {
		processes = append(processes, FaultProcessEvidence{ID: target, Role: "fixture", Identity: target, PID: 200 + i})
	}
	return processes, nil
}

// Every declared fault and the finalization offset run through the production
// scheduler. A probe barrier rejects the old extra poll after cleanup, without
// waiting for a watchdog or changing any assertion to make the fixture pass.
func TestScenarioFailedTerminalControllerSealsFailureAfterEveryFault(t *testing.T) {
	cfg, _, _, base := newScenarioIntervalFixture(t)
	cfg.provisionalResume.Record.PlanHash = campaignTestPlanHash
	definition, err := scenarioDefinitionFor(cfg, "release-1.0")
	if err != nil {
		t.Fatal(err)
	}
	baseline := base[0]
	window, err := buildScenarioAcceptanceWindow(cfg, definition, baseline)
	if err != nil {
		t.Fatal(err)
	}
	fixture := newProcessLogGateFixture(t, "", "")
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Date(2026, 9, 3, 7, 0, 0, 0, time.UTC)
	attempt, err := loadOrCreateScenarioCampaignAttempt(cfg, fixture.dir, roles, campaignTestPlanHash, definition.Name, nil, started)
	if err != nil {
		t.Fatal(err)
	}
	runDir := filepath.Join(fixture.dir, "runs", attempt.payload.RunID)
	evidence := healthyAdversaryEvidence()
	evidence.Status, evidence.MatrixHash = "running", definition.AdversarialMatrixHash
	evidence.StartedAt, evidence.HappyPathStartedAt = started.Format(time.RFC3339Nano), started.Format(time.RFC3339Nano)
	evidence.Actors[0].Samples, evidence.Actors[0].ControlSamples, evidence.Actors[0].AttackSamples = 0, 0, 0
	campaign := &failedTerminalAdversary{scenarioAdversaryStub: scenarioAdversaryStub{evidence: evidence}}
	baseline.ObservedAt = started.Format(time.RFC3339Nano)
	observations := []*ScenarioObservation{baseline}
	for i := uint64(1); i <= 110; i++ {
		// Cross the existing hard restore bounds for conditional head probes;
		// this fixture tests terminal control, not native decision derivation.
		block := window.StartBlock + (i-1)*10000
		epoch := (block - 1_000) / cfg.Policy.Settlement.EpochBlocks
		observation := testScenarioObservation(cfg, epoch)
		observation.ObservedAt = started.Add(time.Duration(i) * time.Minute).Format(time.RFC3339Nano)
		observation.CandidateFleetUIDs = append([]uint16(nil), baseline.CandidateFleetUIDs...)
		observation.Status.Contracts.FinalizedHead.Number = block
		observation.FleetLifecycle = &FleetLifecycleEvidence{Stage: fleetLifecycleStageComplete, TerminalEffectiveEpoch: window.FirstEpoch}
		observation.Status.Contracts.Epochs = nil
		for accepted := window.FirstEpoch; accepted < window.FirstEpoch+window.EpochCount; accepted++ {
			view := EpochView{Epoch: accepted}
			for noId := 1; noId <= cfg.Config.Topology.Operators; noId++ {
				view.Operators = append(view.Operators, EpochOperatorView{NoID: uint64(noId), Status: 3, ArtifactHash: "0x" + strings.Repeat("0", 64), PayoutRoot: "0x" + strings.Repeat("0", 64)})
			}
			observation.Status.Contracts.Epochs = append(observation.Status.Contracts.Epochs, view)
		}
		observations = append(observations, observation)
	}
	probe := &scenarioIntervalProbe{observations: observations}
	probe.before = func(_ context.Context, index int) error {
		if index == 0 {
			return nil
		}
		var ledger struct {
			Faults []ScenarioFaultRecord `json:"faults"`
		}
		if err := readJSONFile(filepath.Join(runDir, "faults.json"), &ledger); err == nil && len(ledger.Faults) == len(definition.Faults) && faultsComplete(ledger.Faults) && scenarioAcceptanceIntervalObserved(window, observations[index-1]) {
			return errors.New("complete failed interval requested another snapshot instead of sealing")
		}
		return nil
	}
	driver := &failedIntervalFaultDriver{}
	result, runErr := runScenarioWithProbe(t.Context(), cfg, fixture.dir, definition, probe, scenarioRunOptions{
		Now: func() time.Time { return started }, PollInterval: time.Microsecond, Timeout: time.Hour,
		Attempt: attempt, Roles: roles, ProcessLogs: fixture.gate, FaultDriver: driver, Adversaries: campaign,
	})
	if result == nil || runErr == nil || result.Result != "fail" || !result.Provisional || result.FinalAcceptance == nil || *result.FinalAcceptance || !scenarioAcceptanceIntervalObserved(result.AcceptanceWindow, observations[probe.calls.Load()-1]) || !faultsComplete(result.Faults) {
		t.Fatalf("failed controller lost terminal evidence: result=%+v error=%v reads=%d", result, runErr, probe.calls.Load())
	}
	if campaign.stopCalls != 1 || campaign.evidence.Status != "stopped" {
		t.Fatal("adversary workers were not joined exactly once")
	}
	complete, payoutFailed, sampleFailed := false, false, false
	for _, assertion := range result.Assertions {
		if assertion.ID == "scenario_context" {
			t.Fatalf("failed verdict was fabricated as interruption: %s", assertion.Message)
		}
		complete = complete || assertion.ID == "provisional_failed_interval_complete" && !assertion.Passed
		payoutFailed = payoutFailed || assertion.ID == "payout_artifacts_enforce_one_tier" && !assertion.Passed
		sampleFailed = sampleFailed || strings.HasSuffix(assertion.ID, "_samples") && !assertion.Passed
	}
	if !complete || !payoutFailed || !sampleFailed {
		t.Fatalf("strict failure or stopping reason lost: %t/%t/%t error=%v assertions=%+v", complete, payoutFailed, sampleFailed, runErr, result.Assertions)
	}
	if _, err := os.Stat(filepath.Join(runDir, "complete.json")); !os.IsNotExist(err) {
		t.Fatal("failed provisional run produced acceptance marker", err)
	}
	if _, err := os.Stat(filepath.Join(runDir, "result.json")); err != nil {
		t.Fatal("failed result was not durably sealed", err)
	}
	for _, record := range result.Faults {
		if record.AppliedBlock == 0 || record.RestoredBlock < record.AppliedBlock {
			t.Fatal(fmt.Sprintf("fault evidence lost: %+v", record))
		}
	}
}
