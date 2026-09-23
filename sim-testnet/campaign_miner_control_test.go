// Exercise retry state through the signer and observation controller, where a
// valid driver-level pending result previously terminated the live campaign.
package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// One transient apply and restore precede complete, exact process evidence.
// The controller owns this fixture and invokes it serially.
type scenarioPendingControlDriver struct {
	fakeFaultDriver
	applyCalls   int
	restoreCalls int
}

// Retain the entire intended census while the first round is incomplete.
func (self *scenarioPendingControlDriver) Apply(_ context.Context, spec scenarioFaultSpec) ([]FaultProcessEvidence, error) {
	self.applyCalls++
	processes := pendingControlTestProcesses(spec.Targets)
	if self.applyCalls == 1 {
		return processes, &minerControlPendingError{cause: context.DeadlineExceeded}
	}
	return processes, nil
}

// Restoration has the same incomplete-round semantics as activation.
func (self *scenarioPendingControlDriver) Restore(_ context.Context, spec scenarioFaultSpec) ([]FaultProcessEvidence, error) {
	self.restoreCalls++
	processes := pendingControlTestProcesses(spec.Targets)
	if self.restoreCalls == 1 {
		return processes, &minerControlPendingError{cause: context.DeadlineExceeded}
	}
	return processes, nil
}

// A complete census carries identity only; the transition block proves success.
func pendingControlTestProcesses(targets []string) []FaultProcessEvidence {
	processes := make([]FaultProcessEvidence, 0, len(targets))
	for index, target := range targets {
		processes = append(processes, FaultProcessEvidence{ID: target, Role: "miner", Identity: "synthetic-swarm", PID: 200 + index})
	}
	return processes
}

// Partial transitions may be signed, but arbitrary errors, census substitutions
// and rollback cannot use their retry authority.
func TestScenarioCampaignMinerControlValidatesPartialProgress(t *testing.T) {
	t.Parallel()
	window := &ScenarioAcceptanceWindow{StartBlock: 100}
	record := ScenarioFaultRecord{
		ID: "cohort", Kind: "miner-control", Targets: []string{"miner-1", "miner-2"},
		TriggerBlock: 105, RestoreBlock: 125, Status: "pending", Error: "synthetic timeout",
		ControlStartedBlock: 105, ControlStartedBlockHash: "0x" + strings.Repeat("a1", 32), ControlPendingRounds: 1,
	}
	record.Processes = pendingControlTestProcesses(record.Targets)
	if err := validateScenarioCampaignFaultState(window, record); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		mutate func(*ScenarioFaultRecord)
	}{
		{"foreign kind", func(value *ScenarioFaultRecord) { value.Kind = "process-pause" }},
		{"zero rounds", func(value *ScenarioFaultRecord) { value.ControlPendingRounds = 0 }},
		{"before trigger", func(value *ScenarioFaultRecord) { value.ControlStartedBlock-- }},
		{"missing start", func(value *ScenarioFaultRecord) { value.ControlStartedBlock = 0 }},
		{"missing hash", func(value *ScenarioFaultRecord) { value.ControlStartedBlockHash = "" }},
		{"missing cause", func(value *ScenarioFaultRecord) { value.Error = "" }},
		{"missing census", func(value *ScenarioFaultRecord) { value.Processes = value.Processes[:1] }},
		{"duplicate target", func(value *ScenarioFaultRecord) { value.Processes[1] = value.Processes[0] }},
		{"foreign target", func(value *ScenarioFaultRecord) { value.Processes[0].ID = "miner-3" }},
		{"invalid pid", func(value *ScenarioFaultRecord) { value.Processes[0].PID = 0 }},
		{"false completion", func(value *ScenarioFaultRecord) { value.AppliedBlock = 105 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			altered := cloneScenarioFaultRecords([]ScenarioFaultRecord{record})[0]
			test.mutate(&altered)
			if err := validateScenarioCampaignFaultState(window, altered); err == nil {
				t.Fatal("malformed pending transition was admitted")
			}
		})
	}
	active := record
	active.Status, active.AppliedBlock, active.AppliedBlockHash = "active", 110, record.ControlStartedBlockHash
	if err := validateScenarioCampaignFaultState(window, active); err != nil {
		t.Fatalf("pending restoration: %v", err)
	}
	// A first retry during restore has no earlier pending activation boundary.
	active.ControlStartedBlock, active.ControlStartedBlockHash = 0, ""
	if err := validateScenarioCampaignFaultState(window, active); err != nil {
		t.Fatalf("restore-only retry: %v", err)
	}
	active.ControlPendingRounds = 0
	if err := validateScenarioCampaignFaultState(window, active); err == nil {
		t.Fatal("unclassified active error was admitted")
	}
	for _, alter := range []func(*ScenarioFaultRecord){
		func(value *ScenarioFaultRecord) { value.ControlPendingRounds-- },
		func(value *ScenarioFaultRecord) { value.ControlStartedBlock++ },
		func(value *ScenarioFaultRecord) { value.ControlStartedBlockHash = "0x" + strings.Repeat("b2", 32) },
	} {
		after := record
		alter(&after)
		if err := validateScenarioFaultProgress([]ScenarioFaultRecord{record}, []ScenarioFaultRecord{after}); err == nil {
			t.Fatal("signed partial progress was erased or substituted")
		}
	}
}

// The expensive observer remains blocked until the real signed heartbeat path
// has persisted both a pending round and its completed application. No sleeps or
// wall-clock timeout decide when the snapshot may finish.
func TestScenarioHeartbeatCheckpointsPendingMinerControlsWithoutCancelingObservation(t *testing.T) {
	cfg := testResolvedConfig(t)
	attempt, runDir, _, faults := bindCampaignAttemptBoundaryFixture(t, cfg, t.TempDir())
	definition, err := scenarioDefinitionFor(cfg, "release-1.0")
	if err != nil {
		t.Fatal(err)
	}
	index := -1
	for candidate := range faults {
		if faults[candidate].ID == "quality-cohort" {
			index = candidate
			break
		}
	}
	if index < 0 {
		t.Fatal("release fixture has no quality cohort")
	}
	head := ChainHead{Number: faults[index].TriggerBlock, Hash: "0x" + strings.Repeat("c3", 32)}
	observation := testScenarioObservation(cfg, attempt.payload.AcceptanceBoundary.LastObservationEpoch)
	observation.Status.Contracts.FinalizedHead = head
	observation.ObservedAt = time.Date(2026, 9, 3, 7, 4, 0, 0, time.UTC).Format(time.RFC3339Nano)
	ready := make(chan struct{})
	probe := &scenarioIntervalHeartbeatProbe{
		scenarioIntervalProbe: &scenarioIntervalProbe{observations: []*ScenarioObservation{observation}, before: func(ctx context.Context, _ int) error {
			select {
			case <-ready:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}},
		head: head,
	}
	driver := &scenarioPendingControlDriver{}
	rounds := 0
	current, err := waitScenarioSnapshot(t.Context(), probe, time.Millisecond, func(ctx context.Context, head ChainHead) error {
		if rounds >= 2 {
			return nil
		}
		rounds++
		if err := advanceFaults(ctx, head, definition.Faults[index:index+1], faults[index:index+1], driver); err != nil {
			return err
		}
		if err := writeScenarioFaultEvidence(runDir, faults); err != nil {
			return err
		}
		if err := attempt.updateAuthenticatedRuntime(runDir, faults); err != nil {
			return err
		}
		reopened, err := readScenarioCampaignAttempt(cfg, attempt.stateDir, attempt.roles, campaignTestPlanHash, "release-1.0")
		if err != nil {
			return err
		}
		retained := reopened.payload.AcceptanceBoundary.Faults[index]
		if retained.ControlPendingRounds != 1 || retained.ControlStartedBlock != head.Number || rounds == 1 && (retained.Status != "pending" || retained.AppliedBlock != 0) || rounds == 2 && (retained.Status != "active" || retained.Error != "") {
			return fmt.Errorf("signed control checkpoint lost its exact transition: %+v", retained)
		}
		if rounds == 2 {
			close(ready)
		}
		return nil
	})
	if err != nil || rounds != 2 || current == nil {
		t.Fatalf("pending control canceled the observer: rounds=%d observation=%v err=%v", rounds, current, err)
	}
	if err := appendObservation(filepath.Join(runDir, "observations.jsonl"), current); err != nil {
		t.Fatal(err)
	}
	if err := attempt.updateAuthenticatedRuntime(runDir, faults); err != nil {
		t.Fatal(err)
	}
	head.Number = faults[index].RestoreBlock
	for round := 1; round <= 2; round++ {
		if err := advanceFaults(t.Context(), head, definition.Faults[index:index+1], faults[index:index+1], driver); err != nil {
			t.Fatal(err)
		}
		if err := attempt.updateAuthenticatedRuntime(runDir, faults); err != nil {
			t.Fatalf("restoration checkpoint %d: %v", round, err)
		}
	}
	if faults[index].Status != "restored" || faults[index].ControlPendingRounds != 2 || faults[index].Error != "" {
		t.Fatalf("restoration did not retain both bounded retries: %+v", faults[index])
	}
	_, _, _, retained, _, err := attempt.loadAuthenticatedRuntimeForensics(runDir)
	if err != nil || retained.ObservationHash != current.ObservationHash {
		t.Fatalf("observer progress lost authentication: %v", err)
	}
}

// The full scenario loop reaches its terminal block after both pending control
// phases, and only then certifies the restored fault.
func TestScenarioIntervalPendingMinerControlsReachTerminal(t *testing.T) {
	cfg, definition, window, observations := newScenarioIntervalFixture(t)
	definition.Faults[0].Kind = "miner-control"
	definition.Faults[0].Targets = []string{"miner-1"}
	definition.Checks = []scenarioCheck{{ID: "terminal-only", Check: func(evaluation *scenarioEvaluation) (bool, string) {
		return scenarioAcceptanceIntervalObserved(evaluation.Window, evaluation.Current), "complete interval"
	}}}
	// Both restore rounds must occur inside the actual acceptance window; the
	// finalization tail cannot retroactively certify an out-of-window fault.
	withRestore := append([]*ScenarioObservation(nil), observations[:4]...)
	for _, block := range []uint64{window.StartBlock + 3, window.StartBlock + 4} {
		observed := testScenarioObservation(cfg, (block-1_000)/cfg.Policy.Settlement.EpochBlocks)
		observed.Status.Contracts.FinalizedHead.Number = block
		observed.CandidateFleetUIDs = observations[0].CandidateFleetUIDs
		withRestore = append(withRestore, observed)
	}
	observations = append(withRestore, observations[4:]...)
	fixture := newProcessLogGateFixture(t, "", "")
	driver := &scenarioPendingControlDriver{}
	probe := &scenarioIntervalProbe{observations: observations}
	result, err := runScenarioWithProbe(t.Context(), cfg, fixture.dir, definition, probe, scenarioRunOptions{PollInterval: time.Millisecond, Timeout: time.Hour, ProcessLogs: fixture.gate, FaultDriver: driver})
	if err != nil || result == nil || result.Result != "pass" || result.EndHead.Number != window.TerminalBlock || driver.applyCalls != 2 || driver.restoreCalls != 2 || !faultsComplete(result.Faults) {
		t.Fatalf("partial control aborted acceptance: result=%+v calls=%d/%d err=%v", result, driver.applyCalls, driver.restoreCalls, err)
	}
}

// Cancellation only belongs to the round that issued it; neither a joined
// semantic failure nor an invalid status body can be hidden beneath it.
func TestMinerControlRoundCancellationKeepsMixedIntegrityFailureHard(t *testing.T) {
	for _, failure := range []error{
		errors.Join(context.Canceled, errors.New("synthetic integrity failure")),
		fmt.Errorf("control: %w", errors.Join(context.Canceled, errors.New("synthetic integrity failure"))),
		&minerControlInvalidStatusError{cause: context.Canceled},
	} {
		if minerControlRoundRetryable(failure, true) {
			t.Fatalf("integrity failure gained pending authority: %v", failure)
		}
	}
	if !minerControlRoundRetryable(errors.Join(context.Canceled, context.DeadlineExceeded), true) || minerControlRoundRetryable(context.Canceled, false) {
		t.Fatal("round cancellation ownership was lost")
	}
}
