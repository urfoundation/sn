// Real signed checkpoints and synthetic finalized chain rows distinguish a
// completed failed interval from missing evidence or unfinished cleanup.
package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Sample readiness can remain false when an immutable root failure prevents
// an adversary from producing a meaningful native vector. Stop still joins it.
type failedTerminalAdversary struct {
	scenarioAdversaryStub
}

func (self *failedTerminalAdversary) Ready() bool { return false }

func (self *failedTerminalAdversary) Stop(ctx context.Context) (*AdversaryCampaignEvidence, error) {
	evidence, err := self.scenarioAdversaryStub.Stop(ctx)
	evidence.Status = "stopped"
	return evidence, err
}

// Own a real newly signed acceptance boundary, exact fault census and suffix.
type failedTerminalFixture struct {
	cfg        *ResolvedConfig
	definition scenarioDefinition
	window     *ScenarioAcceptanceWindow
	current    *ScenarioObservation
	faults     []ScenarioFaultRecord
	assertions []AssertionRecord
	options    scenarioRunOptions
	runDir     string
}

func newFailedTerminalFixture(t *testing.T) *failedTerminalFixture {
	t.Helper()
	cfg := testResolvedConfig(t)
	cfg.provisionalResume = &provisionalResumeState{Record: &provisionalResumeRecord{Provisional: true, PlanHash: campaignTestPlanHash}}
	attempt, runDir, window, faults := bindCampaignAttemptBoundaryFixture(t, cfg, t.TempDir())
	definition, err := scenarioDefinitionFor(cfg, "release-1.0")
	if err != nil {
		t.Fatal(err)
	}
	terminalBlock := window.TerminalBlock
	for i := range faults {
		fault := &faults[i]
		fault.Status = "restored"
		fault.AppliedBlock, fault.AppliedBlockHash = fault.TriggerBlock, "0x"+strings.Repeat("31", 32)
		fault.RestoredBlock, fault.RestoredBlockHash = fault.RestoreBlock, "0x"+strings.Repeat("32", 32)
		if fault.ActivationCondition != "" {
			fault.ActivationConditionMet, fault.ActivationConditionBlock = true, fault.TriggerBlock
		}
		if fault.RestoreCondition != "" {
			fault.RestoreConditionMet, fault.RestoreConditionBlock = true, fault.RestoreBlock
		}
		for j, target := range fault.Targets {
			fault.Processes = append(fault.Processes, FaultProcessEvidence{ID: target, Role: "fixture", Identity: target, PID: 100 + j})
			fault.RestoredProcesses = append(fault.RestoredProcesses, FaultProcessEvidence{ID: target, Role: "fixture", Identity: target, PID: 200 + j})
		}
		terminalBlock = max(terminalBlock, fault.RestoredBlock)
	}
	current := testScenarioObservation(cfg, window.FirstEpoch+window.EpochCount+10)
	current.Status.Contracts.FinalizedHead = ChainHead{Number: terminalBlock, Hash: "0x" + strings.Repeat("41", 32)}
	current.Status.Contracts.Epochs = nil
	for epoch := window.FirstEpoch; epoch < window.FirstEpoch+window.EpochCount; epoch++ {
		view := EpochView{Epoch: epoch}
		for noId := 1; noId <= cfg.Config.Topology.Operators; noId++ {
			view.Operators = append(view.Operators, EpochOperatorView{NoID: uint64(noId), Status: 2, CommitBlock: window.StartBlock, ArtifactHash: "0x" + strings.Repeat("51", 32), PayoutRoot: "0x" + strings.Repeat("52", 32)})
		}
		current.Status.Contracts.Epochs = append(current.Status.Contracts.Epochs, view)
	}
	missed := &current.Status.Contracts.Epochs[0].Operators[1]
	missed.Status, missed.CommitBlock = 3, 0
	missed.ArtifactHash, missed.PayoutRoot = "0x"+strings.Repeat("0", 64), "0x"+strings.Repeat("0", 64)
	current.ObservedAt = "2026-09-03T15:00:00Z"
	boundary := attempt.payload.AcceptanceBoundary
	evidence := healthyAdversaryEvidence()
	evidence.Status, evidence.MatrixHash = "running", definition.AdversarialMatrixHash
	evidence.StartedAt, evidence.HappyPathStartedAt = boundary.AdversaryStartedAt, boundary.AdversaryHappyPathStartedAt
	evidence.Actors[0].Samples, evidence.Actors[0].ControlSamples, evidence.Actors[0].AttackSamples = 0, 0, 0
	adversary := &failedTerminalAdversary{scenarioAdversaryStub: scenarioAdversaryStub{evidence: evidence, started: true, happyStarted: time.Now()}}
	f := &failedTerminalFixture{cfg: cfg, definition: definition, window: window, current: current, faults: faults, runDir: runDir,
		options: scenarioRunOptions{Attempt: attempt, ProcessSessionID: boundary.ProcessSessionID, Adversaries: adversary}}
	f.checkpoint(t)
	return f
}

// Authenticate the exact bytes through the production append-only checkpoint.
func (self *failedTerminalFixture) checkpoint(t *testing.T) {
	t.Helper()
	self.current.ObservationHash = ""
	self.current.ObservationHash, _ = canonicalHashHex(self.current)
	if err := appendObservation(filepath.Join(self.runDir, "observations.jsonl"), self.current); err != nil {
		t.Fatal(err)
	}
	if err := self.options.Attempt.updateAuthenticatedRuntime(self.runDir, self.faults); err != nil {
		t.Fatal(err)
	}
	self.assertions = []AssertionRecord{{ID: "payout_artifacts_enforce_one_tier", Passed: false, Message: "an accepted operator epoch has no committed payout", ObservationHash: self.current.ObservationHash},
		{ID: "claims_finalized_per_no", Passed: false, Message: "an unresolved claim remains", ObservationHash: self.current.ObservationHash}}
}

func (self *failedTerminalFixture) reason() string {
	return scenarioProvisionalFailureCompletion(self.cfg, self.definition, self.window, self.current, self.faults, self.assertions, self.options)
}

// The production checkpoint proves a terminal missed root. No number of new
// reads can repair it, and an unsatisfied sample quota is retained as a failure.
func TestScenarioFailedTerminalCompletesSignedMissedRootWithoutWaiver(t *testing.T) {
	f := newFailedTerminalFixture(t)
	before, err := os.ReadFile(f.options.Attempt.path())
	if err != nil {
		t.Fatal(err)
	}
	assertions := append([]AssertionRecord(nil), f.assertions...)
	if reason := f.reason(); !strings.Contains(reason, "finalized RootMissed") {
		t.Fatalf("complete immutable failed interval still requests another snapshot: %q", reason)
	}
	if !reflect.DeepEqual(f.assertions, assertions) || assertionsPass(f.assertions) || f.options.Adversaries.Ready() {
		t.Fatal("failure completion changed strict assertions or sample readiness")
	}
	started, _ := time.Parse(time.RFC3339Nano, f.options.Attempt.payload.StartedAt)
	evidence, records, err := finalizeAdversaryEvidence(f.options.Adversaries, f.runDir, started, f.current.ObservationHash, started.Add(8*time.Hour), true)
	if err != nil || evidence.Status != "stopped" || f.options.Adversaries.(*failedTerminalAdversary).stopCalls != 1 {
		t.Fatal("failed completion did not join its exact adversary workers", err)
	}
	missingSamples := false
	for _, record := range records {
		missingSamples = missingSamples || strings.HasSuffix(record.ID, "_samples") && !record.Passed
	}
	if !missingSamples {
		t.Fatal("failed adversary sampling was hidden")
	}
	after, err := os.ReadFile(f.options.Attempt.path())
	if err != nil || string(before) != string(after) {
		t.Fatal("stopping rule rewrote signed authority", err)
	}
	if _, err := os.Stat(filepath.Join(f.runDir, "complete.json")); !os.IsNotExist(err) {
		t.Fatal("failed observation manufactured an acceptance marker", err)
	}
}

// Both finalization dimensions, every cleanup tail, and exact current signed
// authority remain mandatory even when a permanent failure already exists.
func TestScenarioFailedTerminalRejectsIncompleteOrForeignCheckpoint(t *testing.T) {
	f := newFailedTerminalFixture(t)
	for _, mutation := range []struct {
		name  string
		apply func() func()
	}{
		{name: "before-terminal", apply: func() func() {
			old := f.window.TerminalBlock
			f.window.TerminalBlock = f.current.Status.Contracts.FinalizedHead.Number + 1
			return func() { f.window.TerminalBlock = old }
		}},
		{name: "old-epoch", apply: func() func() {
			old := f.current.Status.Contracts.CurrentEpoch
			f.current.Status.Contracts.CurrentEpoch = f.window.FirstEpoch
			return func() { f.current.Status.Contracts.CurrentEpoch = old }
		}},
		{name: "unsigned-observation", apply: func() func() {
			old := f.current.ObservationHash
			f.current.ObservationHash = "0x" + strings.Repeat("71", 32)
			return func() { f.current.ObservationHash = old }
		}},
		{name: "foreign-session", apply: func() func() {
			old := f.options.ProcessSessionID
			f.options.ProcessSessionID = "0x" + strings.Repeat("72", 32)
			return func() { f.options.ProcessSessionID = old }
		}},
		{name: "invalidated-attempt", apply: func() func() {
			f.options.Attempt.payload.AcceptanceInvalidation = "execution-exited-before-completion"
			return func() { f.options.Attempt.payload.AcceptanceInvalidation = "" }
		}},
		{name: "missing-tail", apply: func() func() {
			old := f.faults
			f.faults = f.faults[:len(f.faults)-1]
			return func() { f.faults = old }
		}},
		{name: "active-tail", apply: func() func() {
			old := f.faults[0].Status
			f.faults[0].Status = "active"
			return func() { f.faults[0].Status = old }
		}},
		{name: "pending-restoration", apply: func() func() {
			old := f.faults[0].Status
			f.faults[0].Status = "pending"
			return func() { f.faults[0].Status = old }
		}},
		{name: "incomplete-lifecycle", apply: func() func() {
			f.options.FleetLifecycle = &recordingHandoffLifecycle{}
			return func() { f.options.FleetLifecycle = nil }
		}},
		{name: "missing-adversary-owner", apply: func() func() {
			old := f.options.Adversaries
			f.options.Adversaries = nil
			return func() { f.options.Adversaries = old }
		}},
		{name: "strict-invocation", apply: func() func() {
			f.cfg.provisionalResume.Record.FinalAcceptance = true
			return func() { f.cfg.provisionalResume.Record.FinalAcceptance = false }
		}},
		{name: "not-provisional", apply: func() func() {
			f.cfg.provisionalResume.Record.Provisional = false
			return func() { f.cfg.provisionalResume.Record.Provisional = true }
		}},
		{name: "diagnostic-only", apply: func() func() { f.cfg.readOnlyAudit = true; return func() { f.cfg.readOnlyAudit = false } }},
		{name: "other-phase", apply: func() func() {
			f.definition.Name = "production-soak"
			return func() { f.definition.Name = "release-1.0" }
		}},
	} {
		restore := mutation.apply()
		reason := f.reason()
		restore()
		if reason != "" {
			t.Fatalf("%s ended incomplete/foreign observation: %s", mutation.name, reason)
		}
	}
	if f.reason() == "" {
		t.Fatal("negative controls changed original signed fixture")
	}
}

// Repairable absence and below-margin usage are not a permanent failure proof.
func TestScenarioFailedTerminalKeepsMutableEvidenceAndOpenSettlementWaiting(t *testing.T) {
	f := newFailedTerminalFixture(t)
	missed := &f.current.Status.Contracts.Epochs[0].Operators[1]
	missed.Status, missed.CommitBlock = 2, f.window.StartBlock
	missed.ArtifactHash, missed.PayoutRoot = "0x"+strings.Repeat("51", 32), "0x"+strings.Repeat("52", 32)
	f.current.PolicyRateReadiness = &PolicyRateReadinessObservation{Ready: false, Detail: "synthetic low usage", MinimumTransferTaoRao: 100000}
	f.checkpoint(t)
	if f.reason() != "" {
		t.Fatal("missing artifact, low margin or uncertain claim was called immutable")
	}
	missed.Status = 1
	f.checkpoint(t)
	if f.reason() != "" {
		t.Fatal("an unfinalized settlement lost its observation tail")
	}
}

// Fixed signatures and the exact finalized root make a bad tier permanent;
// a foreign hash, another epoch or an uncommitted row cannot borrow that proof.
func TestScenarioFailedTerminalPinsIrreversibleArtifactToAcceptedChainRow(t *testing.T) {
	f := newFailedTerminalFixture(t)
	missed := &f.current.Status.Contracts.Epochs[0].Operators[1]
	missed.Status, missed.CommitBlock = 2, f.window.StartBlock
	missed.ArtifactHash, missed.PayoutRoot = "0x"+strings.Repeat("51", 32), "0x"+strings.Repeat("52", 32)
	f.current.Operators = []OperatorObservation{{NoID: 2, PayoutTierArtifacts: []OperatorPayoutTierArtifactObservation{{Epoch: f.window.FirstEpoch, NoId: 2, ContentHash: "sha256:" + strings.Repeat("51", 32), PayoutRoot: missed.PayoutRoot, CandidateProviders: 4, CandidateLeaves: 1, PoolTailProviders: 2, PoolTailLeaves: 2, TierMembershipValid: false}}}}
	f.checkpoint(t)
	if reason := f.reason(); !strings.Contains(reason, "immutable invalid payout tiers") {
		t.Fatal("chain-matched invalid artifact kept polling", reason)
	}
	row := &f.current.Operators[0].PayoutTierArtifacts[0]
	row.ContentHash = "sha256:" + strings.Repeat("53", 32)
	f.checkpoint(t)
	if f.reason() != "" {
		t.Fatal("foreign artifact hash authorized completion")
	}
	row.ContentHash = "sha256:" + strings.Repeat("51", 32)
	row.Epoch = f.window.FirstEpoch - 1
	f.checkpoint(t)
	if f.reason() != "" {
		t.Fatal("historical artifact authorized completion")
	}
}

// Existing isolated timeout allowances remain repairable. Repeated exact
// accepted findings stay blocking, without classifying an error message.
func TestScenarioFailedTerminalUsesOnlyIrreversibleScopedProcessFindings(t *testing.T) {
	scope := "0x" + strings.Repeat("61", 32)
	finding := ProcessLogFinding{ProcessID: "synthetic-worker", Stream: "stderr", Class: "exit-gap-timeout", Blocking: true, Disposition: "unexplained", Count: 2,
		FirstOffset: 10, LastOffset: 30, FirstLineSHA256: "sha256:" + strings.Repeat("62", 32), LastLineSHA256: "sha256:" + strings.Repeat("63", 32), AcceptanceScope: scope}
	if scenarioIrreversibleProcessFailure(scope, []ProcessLogFinding{finding}) == "" {
		t.Fatal("repeated exact timeout did not establish strict failure")
	}
	for _, mutation := range []struct {
		name string
		edit func(*ProcessLogFinding)
	}{
		{name: "isolated", edit: func(f *ProcessLogFinding) { f.Count = 1 }},
		{name: "foreign-scope", edit: func(f *ProcessLogFinding) { f.AcceptanceScope = "0x" + strings.Repeat("64", 32) }},
		{name: "expected-fault", edit: func(f *ProcessLogFinding) { f.Disposition = "expected-fault"; f.Blocking = false }},
		{name: "recovering", edit: func(f *ProcessLogFinding) { f.RecoveryStartedAt = "2026-09-03T12:00:00Z" }},
		{name: "unknown", edit: func(f *ProcessLogFinding) { f.Class = "unknown-control" }},
		{name: "missing-byte-proof", edit: func(f *ProcessLogFinding) { f.FirstLineSHA256 = "" }},
		{name: "bounded-tls", edit: func(f *ProcessLogFinding) { f.Class = "tls-handshake-timeout" }},
	} {
		copy := finding
		mutation.edit(&copy)
		if reason := scenarioIrreversibleProcessFailure(scope, []ProcessLogFinding{copy}); reason != "" {
			t.Fatalf("%s became permanent: %s", mutation.name, reason)
		}
	}
	finding.Class, finding.Count = "tls-handshake-timeout", 3
	if scenarioIrreversibleProcessFailure(scope, []ProcessLogFinding{finding}) == "" {
		t.Fatal("repeated strict TLS finding was erased")
	}
}
