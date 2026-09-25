// Synthetic signed recovery state reproduces a lifecycle bypass whose
// companion filter can never acquire a terminal-effective mutation epoch.
package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// Share real signed recovery authority while keeping observations synthetic.
type lifecycleCleanupTestFixture struct {
	history *historicalLifecycleFixture
	window  *ScenarioAcceptanceWindow
	current *ScenarioObservation
	binding *ScenarioLifecycleHandoff
	records []ScenarioFaultRecord
}

// Authenticate a current owner over an immutable historical bypass.
func newLifecycleCleanupTestFixture(t *testing.T) *lifecycleCleanupTestFixture {
	t.Helper()
	f := newHistoricalLifecycleFixture(t)
	if err := f.lifecycle.BeginPhase("release-1.0", f.attempt.payload.RunID); err != nil {
		t.Fatal(err)
	}
	bindFailedRecoveryGeneration(t, f.campaign, f.attempt, 200)
	f.attempt.payload.AcceptanceInvalidation, f.attempt.payload.AcceptanceInvalidatedAt = "", ""
	if err := writeScenarioCampaignAttempt(f.attempt); err != nil {
		t.Fatal(err)
	}
	window := &f.attempt.payload.AcceptanceBoundary.AcceptanceWindow
	current := testScenarioObservation(f.campaign.cfg, window.FirstEpoch+window.EpochCount)
	current.Status.Contracts.FinalizedHead = ChainHead{Number: window.TerminalBlock, Hash: "0x" + strings.Repeat("e1", 32)}
	started, err := time.Parse(time.RFC3339Nano, f.attempt.payload.StartedAt)
	if err != nil {
		t.Fatal(err)
	}
	current.ObservedAt = started.Add(8 * time.Hour).Format(time.RFC3339Nano)
	current.FleetLifecycle = f.evidence
	current.ObservationHash = ""
	current.ObservationHash, _ = canonicalHashHex(current)
	binding, err := f.lifecycle.provisionalTerminalCleanup(f.attempt, window, current)
	if err != nil || binding == nil {
		t.Fatalf("exact bypass authority: %+v %v", binding, err)
	}
	specs, err := releaseFleetLifecycleFaults(f.campaign.cfg, window.EpochBlocks)
	if err != nil {
		t.Fatal(err)
	}
	records, err := initializeFaultRecords(window.StartBlock, specs)
	if err != nil {
		t.Fatal(err)
	}
	for i := range records {
		r := &records[i]
		r.Status, r.AppliedBlock, r.AppliedBlockHash = "active", r.TriggerBlock, "0x"+strings.Repeat("e2", 32)
		r.ArmedBlock, r.ArmedBlockHash = window.StartBlock-1, "0x"+strings.Repeat("e3", 32)
		for _, target := range r.Targets {
			r.Processes = append(r.Processes, FaultProcessEvidence{ID: target, Role: "synthetic-view", Identity: "synthetic-owner", PID: 123})
		}
	}
	return &lifecycleCleanupTestFixture{history: f, window: window, current: current, binding: binding, records: records}
}

// Expose a deterministic checkpoint barrier immediately before removal.
type lifecycleCleanupTestDriver struct {
	fakeFaultDriver
	before func(scenarioFaultSpec) error
}

// Record the exact synthetic control result after the test-owned barrier.
func (self *lifecycleCleanupTestDriver) Restore(_ context.Context, spec scenarioFaultSpec) ([]FaultProcessEvidence, error) {
	if self.before != nil {
		if err := self.before(spec); err != nil {
			return nil, err
		}
	}
	self.restored = append(self.restored, spec.ID)
	return []FaultProcessEvidence{{ID: spec.Targets[0], Role: "synthetic-view", Identity: "synthetic-owner", PID: 123}}, nil
}

// Keep the request immutable while advancing the complete observation.
func (self *lifecycleCleanupTestFixture) nextObservation(t *testing.T) {
	t.Helper()
	copy := *self.current
	status, contracts := *copy.Status, *copy.Status.Contracts
	copy.Status, status.Contracts = &status, &contracts
	copy.Status.Contracts.FinalizedHead = ChainHead{Number: self.current.Status.Contracts.FinalizedHead.Number + 1, Hash: "0x" + strings.Repeat("e4", 32)}
	observed, err := time.Parse(time.RFC3339Nano, self.current.ObservedAt)
	if err != nil {
		t.Fatal(err)
	}
	copy.ObservedAt = observed.Add(time.Minute).Format(time.RFC3339Nano)
	copy.ObservationHash = ""
	copy.ObservationHash, _ = canonicalHashHex(&copy)
	self.current = &copy
}

// A false mutation condition remains false. The owner first checkpoints the
// cleanup request, then removal, then a later complete observation's block.
func TestLifecycleTerminalCleanupRetainsExceptionAndFreshCompletion(t *testing.T) {
	f := newLifecycleCleanupTestFixture(t)
	cfg := f.history.campaign.cfg
	if ready, err := faultRestoreConditionMet(cfg, f.current, f.current, scenarioFaultSpec{RestoreCondition: "fleet-lifecycle-terminal-effective"}); err != nil || ready {
		t.Fatal("bypassed mutation unexpectedly satisfied its condition", ready, err)
	}
	var saved []ScenarioFaultRecord
	writes := 0
	checkpoint := func() error { writes++; saved = cloneScenarioFaultRecords(f.records); return nil }
	driver := &lifecycleCleanupTestDriver{before: func(spec scenarioFaultSpec) error {
		for _, r := range saved {
			if r.ID == spec.ID && r.LifecycleCleanup != nil && len(r.LifecycleCleanup.RemovedProcesses) == 0 {
				return nil
			}
		}
		return errors.New("filter removed before durable cleanup request")
	}}
	if err := advanceScenarioLifecycleCleanup(t.Context(), cfg, f.window, f.current, f.binding, f.records, driver, checkpoint); err != nil {
		t.Fatal(err)
	}
	if len(driver.restored) != 2 || writes != 4 || faultsComplete(f.records) {
		t.Fatalf("cleanup did not retain post-action observation boundary: calls=%v writes=%d records=%+v", driver.restored, writes, f.records)
	}
	// Reusing the pre-action snapshot cannot invent completed restoration or
	// reissue the local mutation. Ordinary heartbeat must also leave it alone.
	if err := advanceScenarioLifecycleCleanup(t.Context(), cfg, f.window, f.current, f.binding, f.records, driver, checkpoint); err != nil {
		t.Fatal(err)
	}
	specs, _ := releaseFleetLifecycleFaults(cfg, f.window.EpochBlocks)
	if err := advanceFaults(t.Context(), ChainHead{Number: f.records[0].RestoreBlock + 1, Hash: "0x" + strings.Repeat("f1", 32)}, specs, f.records, driver); err != nil {
		t.Fatal(err)
	}
	if len(driver.restored) != 2 || faultsComplete(f.records) {
		t.Fatal("same cut or heartbeat repeated cleanup or fabricated completion")
	}
	requested := f.current
	f.nextObservation(t)
	if err := advanceScenarioLifecycleCleanup(t.Context(), cfg, f.window, f.current, f.binding, f.records, driver, checkpoint); err != nil {
		t.Fatal(err)
	}
	if !faultsComplete(f.records) || len(driver.restored) != 2 {
		t.Fatal("later observation did not finish exact cleanup")
	}
	for _, r := range f.records {
		if r.RestoreConditionMet || r.RestoreConditionBlock != 0 || r.RestoredBlock != f.current.Status.Contracts.FinalizedHead.Number || r.LifecycleCleanup.CompletedObservationHash != f.current.ObservationHash {
			t.Fatalf("cleanup fabricated mutation/timing: %+v", r)
		}
		if err := validateScenarioCampaignFaultState(f.window, r); err != nil {
			t.Fatal(err)
		}
	}
	assertions := appendAcceptanceFaultAssertion(nil, f.records, f.window, time.Now(), f.current)
	if assertionsPass(assertions) {
		t.Fatal("cleanup passed strict lifecycle tail gate")
	}
	result := &ScenarioResult{AcceptanceWindow: f.window, LifecycleHandoff: f.binding, Faults: f.records}
	if err := validateProvisionalLifecycleCleanupHistory(cfg, result, []*ScenarioObservation{requested, f.current}); err != nil {
		t.Fatal(err)
	}
	result.Faults[0].LifecycleCleanup.LifecycleHandoffHash = "sha256:" + strings.Repeat("a1", 32)
	if validateProvisionalLifecycleCleanupHistory(cfg, result, []*ScenarioObservation{requested, f.current}) == nil {
		t.Fatal("foreign handoff authenticated cleanup")
	}
}

// A failed request checkpoint has no side effect. A subsequent failure cannot
// remove already signed cleanup evidence or change which filter was owned.
func TestLifecycleTerminalCleanupWritesBeforeActionAndRejectsScopeDrift(t *testing.T) {
	f := newLifecycleCleanupTestFixture(t)
	driver := &lifecycleCleanupTestDriver{}
	err := advanceScenarioLifecycleCleanup(t.Context(), f.history.campaign.cfg, f.window, f.current, f.binding, f.records, driver, func() error { return errors.New("synthetic durable write failure") })
	if err == nil || len(driver.restored) != 0 {
		t.Fatal("failed checkpoint still removed filter", err, driver.restored)
	}
	before := cloneScenarioFaultRecords(f.records)
	f.records[0].LifecycleCleanup.LifecycleHandoffHash = "sha256:" + strings.Repeat("a2", 32)
	if validateScenarioFaultProgress(before, f.records) == nil {
		t.Fatal("signed cleanup authority was replaced")
	}
	f.records = cloneScenarioFaultRecords(before)
	f.records[0].Kind = "process-restart"
	if err := advanceScenarioLifecycleCleanup(t.Context(), f.history.campaign.cfg, f.window, f.current, f.binding, f.records, driver, func() error { return nil }); err == nil || len(driver.restored) != 0 {
		t.Fatal("non-filter gained cleanup authority", err)
	}
	f.records = cloneScenarioFaultRecords(before)
	f.records[0].Targets = []string{"foreign-target"}
	if err := advanceScenarioLifecycleCleanup(t.Context(), f.history.campaign.cfg, f.window, f.current, f.binding, f.records, driver, func() error { return nil }); err == nil {
		t.Fatal("foreign target gained cleanup authority")
	}
}

// The signed owner and exact historical no-mutation bytes are required, with
// no pre-terminal action and no promotion of a provisional flag into a pass.
func TestLifecycleTerminalCleanupAuthenticatesOwnerWindowAndBypass(t *testing.T) {
	f := newLifecycleCleanupTestFixture(t)
	for _, edit := range []func(){
		func() { f.history.attempt.payload.AcceptanceInvalidation = "synthetic-interruption" },
		func() { f.current.Status.Contracts.FinalizedHead.Number = f.window.TerminalBlock - 1 },
		func() {
			copy := *f.current.FleetLifecycle
			copy.TerminalEffectiveEpoch = 1
			f.current.FleetLifecycle = &copy
		},
	} {
		attemptInvalid := f.history.attempt.payload.AcceptanceInvalidation
		head := f.current.Status.Contracts.FinalizedHead
		evidence := f.current.FleetLifecycle
		edit()
		if _, err := f.history.lifecycle.provisionalTerminalCleanup(f.history.attempt, f.window, f.current); err == nil {
			t.Fatal("changed cleanup authority accepted")
		}
		f.history.attempt.payload.AcceptanceInvalidation = attemptInvalid
		f.current.Status.Contracts.FinalizedHead = head
		f.current.FleetLifecycle = evidence
	}
	original := f.history.original
	raw, err := os.ReadFile(filepath.Join(f.history.campaign.stateDir, "public", "fleet-lifecycle.json"))
	if err != nil || !slices.Equal(raw, original) {
		t.Fatal("cleanup authentication changed inherited source", err)
	}
	f.history.campaign.cfg.provisionalResume = nil
	if binding, err := f.history.lifecycle.provisionalTerminalCleanup(f.history.attempt, f.window, f.current); err != nil || binding != nil {
		t.Fatal("strict run acquired provisional cleanup", binding, err)
	}
}

// Operational completion keeps the strict failed lifecycle assertion while
// demanding all ordinary faults, the complete interval and core integrity.
func TestLifecycleTerminalCleanupCompletesDiagnosticsWithoutAcceptance(t *testing.T) {
	f := newLifecycleCleanupTestFixture(t)
	driver := &lifecycleCleanupTestDriver{}
	checkpoint := func() error { return nil }
	if err := advanceScenarioLifecycleCleanup(t.Context(), f.history.campaign.cfg, f.window, f.current, f.binding, f.records, driver, checkpoint); err != nil {
		t.Fatal(err)
	}
	f.nextObservation(t)
	if err := advanceScenarioLifecycleCleanup(t.Context(), f.history.campaign.cfg, f.window, f.current, f.binding, f.records, driver, checkpoint); err != nil {
		t.Fatal(err)
	}
	definition := scenarioDefinition{Name: "release-1.0"}
	assertions := []AssertionRecord{{ID: "fleet_lifecycle_fault_tail_bounded", Passed: false}}
	for _, id := range provisionalProductionIntegrityAssertionIDs() {
		assertions = append(assertions, AssertionRecord{ID: id, Passed: true})
	}
	if assertionsPass(assertions) || !scenarioLifecycleOperationalCompletion(f.history.campaign.cfg, definition, f.window, f.current, f.binding, f.records, assertions) {
		t.Fatal("strict failure blocked fully observed diagnostic completion")
	}
	assertions[1].Passed = false
	if scenarioLifecycleOperationalCompletion(f.history.campaign.cfg, definition, f.window, f.current, f.binding, f.records, assertions) {
		t.Fatal("integrity failure was waived")
	}
	assertions[1].Passed = true
	f.records = append(f.records, ScenarioFaultRecord{ID: "synthetic-ordinary", Kind: "process-restart", Status: "active"})
	if scenarioLifecycleOperationalCompletion(f.history.campaign.cfg, definition, f.window, f.current, f.binding, f.records, assertions) {
		t.Fatal("ordinary active fault was waived")
	}
}

// The real signed handoff reader preserves the cleanup exception and may
// authorize only the existing non-accepting production continuation.
func TestLifecycleTerminalCleanupPreservesProvisionalProductionHandoff(t *testing.T) {
	f := newProvisionalProductionTestFixture(t)
	c := f.history.campaign
	attempt := f.history.attempt
	boundary := attempt.payload.AcceptanceBoundary
	window := &boundary.AcceptanceWindow
	requested := testScenarioObservation(c.cfg, f.result.EndEpoch)
	requested.PublicIdentitiesValid = true
	requested.ReserveValidatorRegistered, requested.EscrowHotkeyRegistered = true, true
	requested.Status.Contracts.FinalizedHead = ChainHead{Number: f.result.EndHead.Number + 1, Hash: "0x" + strings.Repeat("d1", 32)}
	completed, err := time.Parse(time.RFC3339Nano, f.result.CompletedAt)
	if err != nil {
		t.Fatal(err)
	}
	requested.ObservedAt = completed.Add(time.Minute).Format(time.RFC3339Nano)
	requested.FleetLifecycle = f.history.evidence
	requested.ObservationHash = ""
	requested.ObservationHash, err = canonicalHashHex(requested)
	if err != nil {
		t.Fatal(err)
	}
	records := cloneScenarioFaultRecords(boundary.Faults)
	for index := range records {
		record := &records[index]
		if !lifecycleCleanupFault(*record) {
			continue
		}
		record.Status, record.RestoredBlock, record.RestoredBlockHash = "active", 0, ""
		record.RestoredProcesses = nil
		record.RestoreConditionMet, record.RestoreConditionBlock = false, 0
		for index := range record.Processes {
			record.Processes[index].Role, record.Processes[index].Identity = "synthetic-view", "synthetic-owner"
		}
	}
	driver := &lifecycleCleanupTestDriver{}
	if err := advanceScenarioLifecycleCleanup(t.Context(), c.cfg, window, requested, f.result.LifecycleHandoff, records, driver, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := appendObservation(filepath.Join(f.runDir, "observations.jsonl"), requested); err != nil {
		t.Fatal(err)
	}
	terminal := *requested
	status, contracts := *requested.Status, *requested.Status.Contracts
	terminal.Status, status.Contracts = &status, &contracts
	terminal.Status.Contracts.FinalizedHead = ChainHead{Number: requested.Status.Contracts.FinalizedHead.Number + 1, Hash: "0x" + strings.Repeat("d2", 32)}
	terminal.ObservedAt = completed.Add(2 * time.Minute).Format(time.RFC3339Nano)
	terminal.ObservationHash = ""
	terminal.ObservationHash, err = canonicalHashHex(&terminal)
	if err != nil {
		t.Fatal(err)
	}
	if err := advanceScenarioLifecycleCleanup(t.Context(), c.cfg, window, &terminal, f.result.LifecycleHandoff, records, driver, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := appendObservation(filepath.Join(f.runDir, "observations.jsonl"), &terminal); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(f.runDir, "observations.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	boundary.Faults = cloneScenarioFaultRecords(records)
	boundary.LastObservationHead, boundary.LastObservationEpoch, boundary.LastObservationHash = terminal.Status.Contracts.FinalizedHead, terminal.Status.Contracts.CurrentEpoch, terminal.ObservationHash
	boundary.ObservationLogContentHash, boundary.ObservationLogBytes = bytesSHA256(raw), uint64(len(raw))
	attempt.payload.AcceptanceInvalidatedAt = completed.Add(3 * time.Minute).Format(time.RFC3339Nano)
	if err := writeScenarioCampaignAttempt(attempt); err != nil {
		t.Fatal(err)
	}
	f.result.EndHead, f.result.EndEpoch, f.result.CompletedAt = terminal.Status.Contracts.FinalizedHead, terminal.Status.Contracts.CurrentEpoch, completed.Add(2*time.Minute+time.Second).Format(time.RFC3339Nano)
	f.result.Faults = cloneScenarioFaultRecords(records)
	f.result.Assertions = append(f.result.Assertions, AssertionRecord{ID: "provisional_lifecycle_terminal_cleanup", Passed: false, Message: "synthetic exact cleanup; lifecycle mutations remain unproved"})
	attachScenarioAnomalyGate(f.result, completed.Add(2*time.Minute+time.Second), nil, &terminal)
	f.result.EvidenceHash, err = canonicalScenarioResultHash(f.result)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeScenarioOutputs(c.cfg, f.runDir, f.result, &terminal); err != nil {
		t.Fatal(err)
	}
	if err := writeScenarioFaultEvidence(f.runDir, records); err != nil {
		t.Fatal(err)
	}
	gate, err := f.prepare(t)
	if err != nil {
		t.Fatal(err)
	}
	verified, _, err := validateExactReleaseCampaignGateContext(t.Context(), c.cfg, c.stateDir, c.roles, gate)
	if err != nil {
		t.Fatal(err)
	}
	if gate.Schema != provisionalProductionGateSchema || gate.CompleteContentHash != "" || verified.Result != "fail" || verified.FinalAcceptance == nil || *verified.FinalAcceptance {
		t.Fatal("cleanup either blocked provisional continuation or promoted strict acceptance")
	}
	cleanups := 0
	for _, fault := range verified.Faults {
		if fault.LifecycleCleanup != nil {
			cleanups++
			if fault.RestoreConditionMet {
				t.Fatal("cleanup invented lifecycle mutation evidence")
			}
		}
	}
	if cleanups != 2 {
		t.Fatal("provisional handoff lost exact cleanup proofs", cleanups)
	}
	if _, err := os.Stat(filepath.Join(f.runDir, "complete.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("cleanup manufactured strict completion", err)
	}
}

// Operational stages in a bypass cannot prove any skipped installation or
// payment. The ordinary complete predicate and unknown-condition refusal stay.
func TestLifecycleTerminalCleanupBypassCannotSatisfyMutationConditions(t *testing.T) {
	cfg := testResolvedConfig(t)
	current := testScenarioObservation(cfg, 12)
	current.FleetLifecycle = &FleetLifecycleEvidence{Stage: fleetLifecycleStageReleaseHandoff, TerminalEffectiveEpoch: 11}
	for _, condition := range []string{"fleet-lifecycle-fallback-installed", "fleet-lifecycle-provider-installed", "fleet-lifecycle-provider-paid", "fleet-lifecycle-terminal-effective"} {
		current.FleetLifecycle.ProvisionalBypass = &FleetLifecycleProvisionalBypass{Provisional: true}
		met, err := faultRestoreConditionMet(cfg, current, current, scenarioFaultSpec{RestoreCondition: condition})
		if err != nil || met {
			t.Fatalf("%s inferred a skipped mutation from bypass stage: %t %v", condition, met, err)
		}
		current.FleetLifecycle.ProvisionalBypass = nil
		met, err = faultRestoreConditionMet(cfg, current, current, scenarioFaultSpec{RestoreCondition: condition})
		if err != nil || !met {
			t.Fatalf("%s rejected ordinary retained lifecycle evidence: %t %v", condition, met, err)
		}
	}
	current.FleetLifecycle.ProvisionalBypass = &FleetLifecycleProvisionalBypass{Provisional: true}
	current.FleetLifecycle.Stage = fleetLifecycleStageComplete
	if met, err := faultConditionMet(cfg, current, current, "fleet-lifecycle-complete"); err != nil || !met {
		t.Fatal("operational completion was confused with mutation proof", met, err)
	}
	if _, err := faultConditionMet(cfg, current, current, "fleet-lifecycle-unknown-condition"); err == nil {
		t.Fatal("bypass hid an unknown condition")
	}
}

// A later result cannot invent a checkpoint which supposedly preceded an
// already signed restoration, even when all of its new fields look valid.
func TestLifecycleTerminalCleanupCannotAttachAuthorityAfterRestoration(t *testing.T) {
	f := newLifecycleCleanupTestFixture(t)
	driver := &lifecycleCleanupTestDriver{}
	if err := advanceScenarioLifecycleCleanup(t.Context(), f.history.campaign.cfg, f.window, f.current, f.binding, f.records, driver, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	f.nextObservation(t)
	if err := advanceScenarioLifecycleCleanup(t.Context(), f.history.campaign.cfg, f.window, f.current, f.binding, f.records, driver, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	before := cloneScenarioFaultRecords(f.records)
	for index := range before {
		before[index].LifecycleCleanup = nil
	}
	if err := validateScenarioFaultProgress(before, f.records); err == nil {
		t.Fatal("unsigned result added a cleanup request after signed restoration")
	}
}
