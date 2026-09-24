// Synthetic finalized snapshots and explicit probe barriers reproduce early
// termination without waiting for chain blocks or relying on timed sleeps.
package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Only one snapshot is active at a time; atomic counters expose its progress
// to the controller test without reading a worker-owned mutable counter.
type scenarioIntervalProbe struct {
	observations []*ScenarioObservation
	calls        atomic.Int64
	before       func(context.Context, int) error
	after        func(int, *ScenarioObservation)
}

// This wrapper supplies only the scheduler's cheap finalized-head interface.
type scenarioIntervalHeartbeatProbe struct {
	*scenarioIntervalProbe
	head  ChainHead
	ready <-chan struct{}
}

// A repeated exact finalized head cannot move any fault transition backward.
func (self *scenarioIntervalHeartbeatProbe) FinalizedHead(ctx context.Context) (ChainHead, error) {
	if self.ready != nil {
		select {
		case <-self.ready:
		case <-ctx.Done():
			return ChainHead{}, ctx.Err()
		}
	}
	return self.head, nil
}

// Signal after evidence is persisted, which is also after the observation
// projection. This makes the immutability check independent of scheduling.
type scenarioIntervalSignalLogGate struct {
	gate       *processLogGate
	afterWrite func()
}

// All classification and source authentication remain the real gate's work.
func (self *scenarioIntervalSignalLogGate) Scan(final bool, faults ...processLogFaultScope) (processLogScanResult, error) {
	return self.gate.Scan(final, faults...)
}

// The signal establishes a deterministic happens-before boundary for a test.
func (self *scenarioIntervalSignalLogGate) WriteEvidence(runDir string) error {
	if err := self.gate.WriteEvidence(runDir); err != nil {
		return err
	}
	self.afterWrite()
	return nil
}

// Applying the fake fault is the barrier which makes the next log scan live.
type scenarioIntervalHeartbeatFaultDriver struct {
	fakeFaultDriver
	applied atomic.Bool
}

// Preserve the ordinary fake census while exposing its completed application.
func (self *scenarioIntervalHeartbeatFaultDriver) Apply(ctx context.Context, spec scenarioFaultSpec) ([]FaultProcessEvidence, error) {
	processes, err := self.fakeFaultDriver.Apply(ctx, spec)
	self.applied.Store(err == nil)
	return processes, err
}

// A failed control transition must remain the cause of an interrupted window.
type scenarioIntervalFailedFaultDriver struct {
	fakeFaultDriver
}

// Fail at the exact scheduled application rather than using an elapsed timer.
func (self *scenarioIntervalFailedFaultDriver) Apply(context.Context, scenarioFaultSpec) ([]FaultProcessEvidence, error) {
	return nil, errors.New("synthetic miner control ownership mismatch")
}

// The bounded list turns an unexpected extra read into a deterministic error.
func (self *scenarioIntervalProbe) Snapshot(ctx context.Context) (*ScenarioObservation, error) {
	index := int(self.calls.Add(1)) - 1
	if self.before != nil {
		if err := self.before(ctx, index); err != nil {
			return nil, err
		}
	}
	if index >= len(self.observations) {
		return nil, errors.New("interval requested an unexpected extra snapshot")
	}
	copy := *self.observations[index]
	copy.ObservationHash = ""
	copy.ObservationHash, _ = canonicalHashHex(copy)
	if self.after != nil {
		self.after(index, &copy)
	}
	return &copy, nil
}

// Keep the actual five-epoch release geometry and independent head-tie census.
func newScenarioIntervalFixture(t *testing.T) (*ResolvedConfig, scenarioDefinition, *ScenarioAcceptanceWindow, []*ScenarioObservation) {
	t.Helper()
	cfg := testResolvedConfig(t)
	cfg.provisionalResume = &provisionalResumeState{Record: &provisionalResumeRecord{Provisional: true, PlanHash: "0x" + strings.Repeat("a3", 32)}}
	definition := scenarioDefinition{Name: "release-1.0", GoalEpochs: uint64(cfg.Config.Scenarios.ShortEpochs)}
	definition.Faults = []scenarioFaultSpec{{ID: "synthetic-pause", Kind: "process-pause", Targets: []string{"synthetic-miner"}, TriggerOffsetBlocks: 1, DurationBlocks: 1}}
	baseline := testScenarioObservation(cfg, 7)
	window, err := buildScenarioAcceptanceWindow(cfg, definition, baseline)
	if err != nil {
		t.Fatal(err)
	}
	observations := []*ScenarioObservation{baseline}
	for _, block := range []uint64{window.StartBlock, window.StartBlock + 1, window.StartBlock + 2, window.EndBlock, window.TerminalBlock - 1, window.TerminalBlock} {
		epoch := (block - 1_000) / cfg.Policy.Settlement.EpochBlocks
		observation := testScenarioObservation(cfg, epoch)
		observation.Status.Contracts.FinalizedHead.Number = block
		observations = append(observations, observation)
	}
	for _, observation := range observations {
		for index := 0; index < cfg.Config.Topology.fleetCandidates(); index++ {
			uid := uint16(index + 10)
			if index >= cfg.Config.Topology.HeadSlots {
				uid = uint16(index-cfg.Config.Topology.HeadSlots) + 1
			}
			observation.CandidateFleetUIDs = append(observation.CandidateFleetUIDs, uid)
		}
	}
	return cfg, definition, window, observations
}

// Both dimensions are required, including overflow and the final offset edge.
func TestScenarioIntervalRequiresTerminalBlockAndEpoch(t *testing.T) {
	t.Parallel()
	_, _, window, observations := newScenarioIntervalFixture(t)
	for _, observation := range observations[:len(observations)-1] {
		if scenarioAcceptanceIntervalObserved(window, observation) {
			t.Fatal("partial or not-yet-finalized interval was accepted")
		}
	}
	terminal := observations[len(observations)-1]
	if !scenarioAcceptanceIntervalObserved(window, terminal) {
		t.Fatal("exact terminal checkpoint was rejected")
	}
	terminal.Status.Contracts.CurrentEpoch--
	if scenarioAcceptanceIntervalObserved(window, terminal) {
		t.Fatal("terminal block with an old contract epoch was accepted")
	}
	window.FirstEpoch = math.MaxUint64
	if scenarioAcceptanceIntervalObserved(window, terminal) || scenarioAcceptanceIntervalObserved(window, nil) {
		t.Fatal("overflow or missing observation was accepted")
	}
	if !scenarioAcceptanceIntervalObserved(nil, nil) {
		t.Fatal("non-window scenarios lost their ordinary assertion controller")
	}
}

// Even an always-passing check cannot end a release before the full interval.
// Every probe barrier inspects disk and the evaluator before releasing a head.
func TestScenarioIntervalDefersEvaluationAndResultUntilTerminal(t *testing.T) {
	t.Parallel()
	cfg, definition, window, observations := newScenarioIntervalFixture(t)
	fixture := newProcessLogGateFixture(t, "", "")
	started := time.Now().UTC()
	runDir := filepath.Join(fixture.dir, "runs", started.Format("20060102T150405.000000000Z")+"-release-1.0")
	var evaluations atomic.Int64
	definition.Checks = []scenarioCheck{{ID: "terminal-only", Check: func(evaluation *scenarioEvaluation) (bool, string) {
		evaluations.Add(1)
		return scenarioAcceptanceIntervalObserved(evaluation.Window, evaluation.Current), "terminal checkpoint observed"
	}}}
	probe := &scenarioIntervalProbe{observations: observations, before: func(_ context.Context, index int) error {
		if evaluations.Load() != 0 {
			return errors.New("terminal evaluator ran before all finalized snapshots")
		}
		for _, name := range []string{"result.json", "assertions.json", "complete.json"} {
			if _, err := os.Stat(filepath.Join(runDir, name)); !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("premature terminal output %s at snapshot %d: %v", name, index, err)
			}
		}
		return nil
	}}
	result, err := runScenarioWithProbe(t.Context(), cfg, fixture.dir, definition, probe, scenarioRunOptions{Now: func() time.Time { return started }, PollInterval: time.Microsecond, Timeout: time.Hour, ProcessLogs: fixture.gate, FaultDriver: &fakeFaultDriver{}})
	if err != nil || result == nil || result.Result != "pass" || probe.calls.Load() != int64(len(observations)) || evaluations.Load() != 1 || result.EndHead.Number != window.TerminalBlock {
		t.Fatalf("interval exited early: result=%+v reads=%d evaluations=%d error=%v", result, probe.calls.Load(), evaluations.Load(), err)
	}
}

// A repairable provisional finding remains visible at every later poll, while
// the unchanged completion gate rejects the unresolved finding after the window.
func TestScenarioIntervalRetainsRepairableProcessFindingThroughTerminal(t *testing.T) {
	t.Parallel()
	cfg, definition, window, observations := newScenarioIntervalFixture(t)
	fixture := newProcessLogGateFixture(t, "", "")
	started := time.Now().UTC()
	runDir := filepath.Join(fixture.dir, "runs", started.Format("20060102T150405.000000000Z")+"-release-1.0")
	var evaluations atomic.Int64
	definition.Checks = []scenarioCheck{{ID: "terminal-only", Check: func(evaluation *scenarioEvaluation) (bool, string) {
		evaluations.Add(1)
		return scenarioAcceptanceIntervalObserved(evaluation.Window, evaluation.Current), "terminal checkpoint observed"
	}}}
	probe := &scenarioIntervalProbe{observations: observations, before: func(_ context.Context, index int) error {
		if index == 1 {
			return os.WriteFile(fixture.stdoutPath, []byte("release steer: subnet epoch 42 attempt 1: retryable native submission\n"), 0o600)
		}
		if index > 1 {
			var evidence processLogGateState
			if err := readJSONFile(filepath.Join(runDir, processLogEvidenceFilename), &evidence); err != nil {
				return err
			}
			if len(evidence.Findings) != 1 || !evidence.Findings[0].Blocking || evidence.Findings[0].Class != "release-steering-attempt-failure" || evidence.Findings[0].FirstLineSHA256 == "" {
				return errors.New("deferred finding was hidden or relabeled in durable evidence")
			}
		}
		return nil
	}}
	result, err := runScenarioWithProbe(t.Context(), cfg, fixture.dir, definition, probe, scenarioRunOptions{Now: func() time.Time { return started }, PollInterval: time.Microsecond, Timeout: time.Hour, ProcessLogs: fixture.gate, FaultDriver: &fakeFaultDriver{}})
	if err == nil || result == nil || result.Result != "fail" || probe.calls.Load() != int64(len(observations)) || evaluations.Load() != 1 || result.EndHead.Number != window.TerminalBlock {
		t.Fatalf("repairable finding ended observation early: result=%+v reads=%d evaluations=%d error=%v", result, probe.calls.Load(), evaluations.Load(), err)
	}
	completionRejected := false
	for _, assertion := range result.Assertions {
		completionRejected = completionRejected || assertion.ID == "process_log_completion" && !assertion.Passed && strings.Contains(assertion.Message, "release-steering-attempt-failure")
	}
	if !completionRejected || fixture.gate.RequireClean(true) == nil {
		t.Fatal("runtime deferral weakened final process-log rejection")
	}
	if _, err := os.Stat(filepath.Join(runDir, "complete.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("unresolved process finding produced completion", err)
	}
}

// Reproduce the incident's heartbeat path while a full snapshot is blocked.
// The repairable log cannot stop the loop or rewrite the saved baseline object.
func TestScenarioIntervalHeartbeatDefersFindingWithoutMutatingHistory(t *testing.T) {
	t.Parallel()
	cfg, definition, window, observations := newScenarioIntervalFixture(t)
	fixture := newProcessLogGateFixture(t, "", "")
	definition.Checks = []scenarioCheck{{ID: "terminal-only", Check: func(evaluation *scenarioEvaluation) (bool, string) {
		return scenarioAcceptanceIntervalObserved(evaluation.Window, evaluation.Current), "terminal checkpoint observed"
	}}}
	definition.Faults = []scenarioFaultSpec{{ID: "synthetic-pause", Kind: "process-pause", Targets: []string{"synthetic-miner"}, TriggerOffsetBlocks: 1, DurationBlocks: 1}}
	observations[1].Status.Contracts.FinalizedHead.Number = window.StartBlock + 1
	observations[2] = testScenarioObservation(cfg, window.FirstEpoch)
	observations[2].Status.Contracts.FinalizedHead.Number = window.StartBlock + 2
	observations = []*ScenarioObservation{observations[0], observations[1], observations[2], observations[len(observations)-1]}
	driver := &scenarioIntervalHeartbeatFaultDriver{}
	heartbeatScanned := make(chan struct{})
	requestReady := make(chan struct{})
	var once sync.Once
	gate := &scenarioIntervalSignalLogGate{gate: fixture.gate, afterWrite: func() {
		if driver.applied.Load() {
			once.Do(func() { close(heartbeatScanned) })
		}
	}}
	probe := &scenarioIntervalProbe{observations: observations}
	var baseline *ScenarioObservation
	var baselineHash string
	probe.after = func(index int, observation *ScenarioObservation) {
		if index == 0 {
			baseline = observation
			baselineHash = observation.ObservationHash
		}
	}
	probe.before = func(ctx context.Context, index int) error {
		if index != 1 {
			return nil
		}
		if err := os.WriteFile(fixture.stdoutPath, []byte("release steer: subnet epoch 42 attempt 1: retryable native submission\n"), 0o600); err != nil {
			return err
		}
		close(requestReady)
		select {
		case <-heartbeatScanned:
			if baseline.ObservationHash != baselineHash || len(baseline.ProcessLogFindings) != 0 {
				return errors.New("heartbeat changed the retained in-memory baseline")
			}
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	heartbeat := &scenarioIntervalHeartbeatProbe{scenarioIntervalProbe: probe, head: ChainHead{Number: window.StartBlock + 1, Hash: "0x" + strings.Repeat("a4", 32)}, ready: requestReady}
	result, err := runScenarioWithProbe(t.Context(), cfg, fixture.dir, definition, heartbeat, scenarioRunOptions{PollInterval: time.Millisecond, Timeout: time.Hour, ProcessLogs: gate, FaultDriver: driver})
	if err == nil || result == nil || result.EndHead.Number != window.TerminalBlock || probe.calls.Load() != int64(len(observations)) || !faultsComplete(result.Faults) {
		t.Fatalf("heartbeat repairable finding interrupted the interval: result=%+v reads=%d error=%v", result, probe.calls.Load(), err)
	}
	// The baseline line is independently hashed on disk; later snapshots may
	// contain the finding, but the earlier one must remain its original evidence.
	lines, err := os.ReadFile(filepath.Join(fixture.dir, "runs", result.RunID, "observations.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	firstLine := strings.SplitN(string(lines), "\n", 2)[0]
	var first ScenarioObservation
	if err := decodeStrictJSONBytes([]byte(firstLine), &first); err != nil || len(first.ProcessLogFindings) != 0 {
		t.Fatalf("heartbeat changed the retained baseline: findings=%v error=%v", first.ProcessLogFindings, err)
	}
}

// Hard classes stop at the offending snapshot. The failure report names the
// interruption and actual finding without evaluating future terminal checks.
func TestScenarioIntervalHardProcessFailureStopsWithoutCascade(t *testing.T) {
	t.Parallel()
	cfg, definition, _, observations := newScenarioIntervalFixture(t)
	fixture := newProcessLogGateFixture(t, "", "")
	var evaluations atomic.Int64
	definition.Checks = []scenarioCheck{{ID: "terminal-only", Check: func(*scenarioEvaluation) (bool, string) { evaluations.Add(1); return true, "terminal" }}}
	probe := &scenarioIntervalProbe{observations: observations, before: func(_ context.Context, index int) error {
		if index == 1 {
			return os.WriteFile(fixture.stdoutPath, []byte("panic: synthetic hard failure\n"), 0o600)
		}
		return nil
	}}
	result, err := runScenarioWithProbe(t.Context(), cfg, fixture.dir, definition, probe, scenarioRunOptions{PollInterval: time.Microsecond, Timeout: time.Hour, ProcessLogs: fixture.gate, FaultDriver: &fakeFaultDriver{}})
	if err == nil || result == nil || result.Result != "fail" || probe.calls.Load() != 2 || evaluations.Load() != 0 {
		t.Fatalf("hard finding did not stop immediately: result=%+v reads=%d evaluations=%d error=%v", result, probe.calls.Load(), evaluations.Load(), err)
	}
	incomplete, cause := false, false
	for _, assertion := range result.Assertions {
		if assertion.ID == "terminal-only" || strings.HasPrefix(assertion.ID, "fault_") {
			t.Fatal("interrupted result contains a future terminal verdict", assertion)
		}
		incomplete = incomplete || assertion.ID == "acceptance_interval_observed" && !assertion.Passed
		cause = cause || assertion.ID == "scenario_context" && strings.Contains(assertion.Message, "panic")
	}
	if !incomplete || !cause {
		t.Fatalf("interruption lost its exact cause: %+v", result.Assertions)
	}
}

// Cancellation preserves the interrupted interval and collected observations;
// the terminal evaluator cannot run merely to materialize a recovery result.
func TestScenarioIntervalCancellationDoesNotEvaluatePendingChecks(t *testing.T) {
	t.Parallel()
	cfg, definition, _, observations := newScenarioIntervalFixture(t)
	fixture := newProcessLogGateFixture(t, "", "")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var evaluations atomic.Int64
	definition.Checks = []scenarioCheck{{ID: "terminal-only", Check: func(*scenarioEvaluation) (bool, string) { evaluations.Add(1); return true, "terminal" }}}
	probe := &scenarioIntervalProbe{observations: observations, before: func(_ context.Context, index int) error {
		if index == 2 {
			cancel()
			return context.Canceled
		}
		return nil
	}}
	result, err := runScenarioWithProbe(ctx, cfg, fixture.dir, definition, probe, scenarioRunOptions{PollInterval: time.Microsecond, Timeout: time.Hour, ProcessLogs: fixture.gate, FaultDriver: &fakeFaultDriver{}})
	if err == nil || result == nil || result.Result != "fail" || probe.calls.Load() != 3 || evaluations.Load() != 0 {
		t.Fatalf("canceled interval lost its boundary: result=%+v reads=%d evaluations=%d error=%v", result, probe.calls.Load(), evaluations.Load(), err)
	}
	for _, assertion := range result.Assertions {
		if assertion.ID == "terminal-only" {
			t.Fatal("cancellation evaluated pending terminal checks")
		}
	}
	var stored ScenarioResult
	if err := readJSONFile(filepath.Join(fixture.dir, "runs", result.RunID, "result.json"), &stored); err != nil || stored.AcceptanceWindow == nil || stored.EndHead.Number >= stored.AcceptanceWindow.TerminalBlock {
		t.Fatalf("interrupted recovery evidence is absent or misstates completion: %+v %v", stored.AcceptanceWindow, err)
	}
}

// Suppressing future fault assertions must not erase the actual failed action.
func TestScenarioIntervalFaultFailureRetainsCauseWithoutCascade(t *testing.T) {
	t.Parallel()
	cfg, definition, _, observations := newScenarioIntervalFixture(t)
	fixture := newProcessLogGateFixture(t, "", "")
	var evaluations atomic.Int64
	definition.Checks = []scenarioCheck{{ID: "terminal-only", Check: func(*scenarioEvaluation) (bool, string) { evaluations.Add(1); return true, "terminal" }}}
	probe := &scenarioIntervalProbe{observations: observations}
	result, err := runScenarioWithProbe(t.Context(), cfg, fixture.dir, definition, probe, scenarioRunOptions{PollInterval: time.Microsecond, Timeout: time.Hour, ProcessLogs: fixture.gate, FaultDriver: &scenarioIntervalFailedFaultDriver{}})
	if err == nil || result == nil || probe.calls.Load() != 3 || evaluations.Load() != 0 {
		t.Fatalf("failed action did not interrupt its exact window: result=%+v reads=%d evaluations=%d error=%v", result, probe.calls.Load(), evaluations.Load(), err)
	}
	cause := false
	for _, assertion := range result.Assertions {
		if assertion.ID == "terminal-only" || strings.HasPrefix(assertion.ID, "fault_") {
			t.Fatal("failed action evaluated future terminal checks", assertion)
		}
		cause = cause || assertion.ID == "scenario_context" && strings.Contains(assertion.Message, "synthetic miner control ownership mismatch")
	}
	if !cause || len(result.Faults) != 1 || result.Faults[0].Error == "" {
		t.Fatalf("failed action lost its durable cause: %+v", result)
	}
}

// Interrupted actors retain their raw measurements without failing checks for
// future coverage. Reusing the same measurements at terminal still rejects them.
func TestScenarioIntervalInterruptedAdversaryEvidenceDefersTerminalChecks(t *testing.T) {
	t.Parallel()
	started := time.Date(2026, 8, 1, 1, 0, 0, 0, time.UTC)
	campaign := &scenarioAdversaryStub{evidence: &AdversaryCampaignEvidence{Schema: "urnetwork-adversary-campaign-v1"}}
	runDir := t.TempDir()
	evidence, assertions, err := finalizeAdversaryEvidence(campaign, runDir, started, "synthetic-observation", started.Add(time.Minute), false)
	if err != nil || evidence == nil || len(assertions) != 0 || campaign.stopCalls != 1 {
		t.Fatalf("interrupted actor evidence was evaluated: evidence=%+v assertions=%+v error=%v", evidence, assertions, err)
	}
	var stored AdversaryCampaignEvidence
	if err := readJSONFile(filepath.Join(runDir, "adversaries.json"), &stored); err != nil || stored.Schema != evidence.Schema {
		t.Fatalf("interrupted actor measurements were lost: %+v %v", stored, err)
	}
	_, assertions, err = finalizeAdversaryEvidence(campaign, runDir, started, "synthetic-observation", started.Add(time.Minute), true)
	if err != nil || len(assertions) == 0 || assertionsPass(assertions) {
		t.Fatalf("terminal actor checks were weakened: assertions=%+v error=%v", assertions, err)
	}
}

// Classification and invocation mode both matter; a recoverable finding cannot
// mask joined integrity/persistence errors or authorize strict acceptance.
func TestScenarioIntervalProcessDeferralIsTypedAndNarrow(t *testing.T) {
	t.Parallel()
	cfg, _, _, _ := newScenarioIntervalFixture(t)
	repairable := processLogFindingsError([]ProcessLogFinding{{Blocking: true, Class: "release-steering-attempt-failure", ProcessID: "synthetic-validator", Count: 1}})
	if !scenarioProcessLogFailureDeferred(cfg, "release-1.0", fmt.Errorf("heartbeat: %w", repairable)) {
		t.Fatal("typed provisional repairable finding was not deferred")
	}
	for _, failure := range []error{
		errors.New(repairable.Error()),
		errors.Join(repairable, errors.New("source hash mismatch")),
		processLogFindingsError([]ProcessLogFinding{{Blocking: true, Class: "panic"}}),
		processLogFindingsError([]ProcessLogFinding{{Blocking: true, Class: "fatal"}}),
		processLogFindingsError([]ProcessLogFinding{{Blocking: true, Class: "release-steering-continuity"}}),
		processLogFindingsError([]ProcessLogFinding{{Blocking: true, Class: "unknown-class"}}),
		&processLogFindingsFailure{},
	} {
		if scenarioProcessLogFailureDeferred(cfg, "release-1.0", failure) {
			t.Fatalf("unsafe or unclassified error was deferred: %v", failure)
		}
	}
	for _, change := range []func(*ResolvedConfig){
		func(cfg *ResolvedConfig) { cfg.provisionalResume = nil },
		func(cfg *ResolvedConfig) { cfg.provisionalResume.Record.FinalAcceptance = true },
		func(cfg *ResolvedConfig) { cfg.provisionalResume.Record.Provisional = false },
		func(cfg *ResolvedConfig) { cfg.readOnlyAudit = true },
	} {
		copied := *cfg
		invocation := *copied.provisionalResume
		record := *invocation.Record
		invocation.Record, copied.provisionalResume = &record, &invocation
		change(&copied)
		if scenarioProcessLogFailureDeferred(&copied, "release-1.0", repairable) {
			t.Fatal("strict or unapproved invocation deferred a process finding")
		}
	}
}

// Budget selection is deterministic without waiting for any actual deadline.
type scenarioIntervalRecoveryDriver struct {
	fakeFaultDriver
	timeout time.Duration
}

// The fixture proposes only a duration; production computes remaining work.
func (self *scenarioIntervalRecoveryDriver) RecoveryTimeout() time.Duration { return self.timeout }

// Ordinary faults keep their minimum, while large cohorts receive a finite
// deadline capable of outliving one 120-second wallet binding operation.
func TestScenarioIntervalRecoveryBudgetIsFiniteAndCohortAware(t *testing.T) {
	t.Parallel()
	for _, entry := range []struct{ proposed, want time.Duration }{
		{-time.Second, 30 * time.Second}, {0, 30 * time.Second}, {time.Second, 30 * time.Second},
		{3 * time.Minute, 3 * time.Minute}, {24 * time.Hour, scenarioFaultRecoveryMaximumTimeout},
	} {
		before := time.Now()
		ctx, cancel := newScenarioFaultRecoveryContext(&scenarioIntervalRecoveryDriver{timeout: entry.proposed})
		after := time.Now()
		deadline, ok := ctx.Deadline()
		cancel()
		if !ok || deadline.Before(before.Add(entry.want)) || deadline.After(after.Add(entry.want)) || !errors.Is(ctx.Err(), context.Canceled) {
			t.Fatalf("cleanup deadline=%v expected duration=%v canceled=%v", deadline, entry.want, ctx.Err())
		}
	}
}

func TestScenarioAcceptanceWindowRequiresFirstFullRolloverEpoch(t *testing.T) {
	t.Parallel()
	cfg := testResolvedConfig(t)
	definition := scenarioDefinition{Name: "release-1.0", GoalEpochs: uint64(cfg.Config.Scenarios.ShortEpochs)}
	for _, tc := range []struct {
		baseline uint64
		allowed  bool
	}{
		{baseline: 8, allowed: false}, // epoch 9 is the partial activation epoch
		{baseline: 9, allowed: true},  // epoch 10 is the first full epoch
		{baseline: 10, allowed: true},
	} {
		window, err := buildScenarioAcceptanceWindow(cfg, definition, testScenarioObservation(cfg, tc.baseline))
		if err != nil {
			t.Fatal(err)
		}
		if got := requireScenarioAcceptanceEpochFloor(window, 10) == nil; got != tc.allowed {
			t.Fatalf("baseline=%d first=%d allowed=%t, want %t", tc.baseline, window.FirstEpoch, got, tc.allowed)
		}
	}
	if err := requireScenarioAcceptanceEpochFloor(nil, 10); err == nil {
		t.Fatal("missing release window bypassed active generation floor")
	}
}
