//go:build linux || darwin

// Real archived approvals and signed receipt formats exercise cap adoption
// with an intentionally pending deployment action and no network dispatcher.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Keep a valid full plan, its retained release and an authentic topology
// receipt. All other setup actions remain pending exactly as in its approval.
func provisionalAllowanceAdoptionFixture(t *testing.T) (*Executor, []byte) {
	t.Helper()
	cfg, source, stateDir, options, original := provisionalRuntimePlanFixture(t)
	if err := atomicWrite(filepath.Join(stateDir, "plans", stringsTrim0x(source.PlanHash)+".json"), original, 0o600); err != nil {
		t.Fatal(err)
	}
	journal := openCampaignTestJournal(t, stateDir)
	executor := &Executor{cfg: cfg, plan: source, stateDir: stateDir, journal: journal}
	persistProvisionalAdoptionTopologyTest(t, executor)
	cfg.MaximumTAORao = 512_000_000_000
	cfg.MaximumEVMGasWei = "512000000000000000000"
	reviewed, err := buildAllowanceOnlyPlan(t.Context(), cfg, stateDir, source.PlanHash)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := archiveReviewedSetupPlan(stateDir, reviewed); err != nil {
		t.Fatal(err)
	}
	executor.plan = reviewed
	options.PlanHash = reviewed.PlanHash
	options.Name = ""
	if err := prepareProvisionalResume(t.Context(), cfg, stateDir, "setup", options, reviewed); err != nil {
		t.Fatal(err)
	}
	return executor, original
}

// Raising ceilings leaves the old probe unverified, dispatches zero actions,
// and stays idempotent after the active approval pointer has been published.
func TestProvisionalAllowanceAdoptionPreservesPendingActionsAndRelease(t *testing.T) {
	self, original := provisionalAllowanceAdoptionFixture(t)
	before := self.journal.Entries()
	if _, _, err := self.provisionalSetupPrefix(t.Context(), self.plan, self.journal.Entries, readValidatorEvidenceHistoricalPlan, true); err == nil {
		t.Fatal("fixture unexpectedly has a completed setup prefix")
	}
	if err := self.verifyProvisionalActionHistory(t.Context()); err != nil {
		t.Fatal(err)
	}
	if ok, err := self.authenticateProvisionalPlanOnlyAdoption(t.Context(), original); err != nil || !ok {
		t.Fatal("pure cap adoption required pending deployment or current release", ok, err)
	}
	retained := []string{"supervisor.json", "supervisor.state.json", "runtime-config-manifest.json", "config.redacted.yml"}
	for _, name := range retained {
		if err := atomicWrite(filepath.Join(self.stateDir, name), []byte("unchanged runtime"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	calls := 0
	dispatch := func(context.Context, Action) error {
		calls++
		return errors.New("cap adoption reached a transaction dispatcher")
	}
	if err := self.activateProvisionalSetupRevision(t.Context(), original, dispatch); err != nil {
		t.Fatal(err)
	}
	active, err := os.ReadFile(filepath.Join(self.stateDir, "plan.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := self.activateProvisionalSetupRevision(t.Context(), active, dispatch); err != nil {
		t.Fatal("idempotent allowance adoption lost its original approval", err)
	}
	if calls != 0 || !reflect.DeepEqual(before, self.journal.Entries()) {
		t.Fatal("cap adoption dispatched or fabricated a completed receipt", calls)
	}
	var activation struct {
		PlanOnly             bool     `json:"plan_only"`
		FinalAcceptance      bool     `json:"final_acceptance"`
		PendingActions       []Action `json:"pending_setup_actions"`
		DeferredSetupActions []Action `json:"deferred_setup_actions"`
	}
	if err := readJSONFile(filepath.Join(filepath.Dir(self.cfg.provisionalResume.RecordPath), "setup-activation.json"), &activation); err != nil {
		t.Fatal(err)
	}
	probeDeferred := false
	for _, action := range activation.DeferredSetupActions {
		probeDeferred = probeDeferred || action.ID == "precompile.probe-deploy"
	}
	if !activation.PlanOnly || activation.FinalAcceptance || len(activation.PendingActions) != 0 || !probeDeferred {
		t.Fatalf("allowance activation hid its pending deployment or claimed acceptance: %+v", activation)
	}
	for _, name := range retained {
		if raw, err := os.ReadFile(filepath.Join(self.stateDir, name)); err != nil || string(raw) != "unchanged runtime" {
			t.Fatal("cap adoption rewrote runtime inputs", name, err)
		}
	}
	archived, err := os.ReadFile(filepath.Join(self.stateDir, "plans", stringsTrim0x(before[0].PlanHash)+".json"))
	if err != nil || !bytes.Equal(original, archived) {
		t.Fatal("original approval was rewritten", err)
	}
	if _, err := loadPlanIdentityBytes(self.cfg, active, false); !errors.Is(err, errPersistedPlanIdentityMismatch) || !strings.Contains(err.Error(), "release_lock_hash") {
		t.Fatal("provisional cap adoption granted strict current release acceptance", err)
	}
}

// The exemption is bound to actual provenance bytes, original release,
// exact current approval and authenticated topology receipt on every use.
func TestProvisionalAllowanceAdoptionRejectsChangedAuthorityAndReceipt(t *testing.T) {
	self, original := provisionalAllowanceAdoptionFixture(t)
	record := *self.cfg.provisionalResume.Record
	for _, mutate := range []func(*provisionalResumeRecord){
		func(r *provisionalResumeRecord) { r.FinalAcceptance = true },
		func(r *provisionalResumeRecord) { r.Provisional = false },
		func(r *provisionalResumeRecord) { r.PlanHash = "0x" + strings.Repeat("ab", 32) },
		func(r *provisionalResumeRecord) { r.ReleaseLockHash = "0x" + strings.Repeat("cd", 32) },
		func(r *provisionalResumeRecord) { r.Driver.ExecutableSHA256 = "sha256:" + strings.Repeat("ef", 32) },
	} {
		changed := record
		mutate(&changed)
		self.cfg.provisionalResume.Record = &changed
		if ok, err := self.authenticateProvisionalPlanOnlyAdoption(t.Context(), original); err == nil || ok {
			t.Fatal("changed invocation gained cap adoption", ok, err)
		}
	}
	self.cfg.provisionalResume.Record = &record
	provenance, err := os.ReadFile(self.cfg.provisionalResume.RecordPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(self.cfg.provisionalResume.RecordPath, append(provenance, ' '), 0o600); err != nil {
		t.Fatal(err)
	}
	if ok, err := self.authenticateProvisionalPlanOnlyAdoption(t.Context(), original); err == nil || ok {
		t.Fatal("changed provenance bytes were reused", ok, err)
	}
	if err := atomicWrite(self.cfg.provisionalResume.RecordPath, provenance, 0o600); err != nil {
		t.Fatal(err)
	}
	entry := self.journal.Entries()[0]
	if err := atomicWrite(filepath.Join(self.stateDir, entry.PostconditionPath), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := self.journal.Entries()
	calls := 0
	if err := self.activateProvisionalSetupRevision(t.Context(), original, func(context.Context, Action) error { calls++; return nil }); err == nil || calls != 0 {
		t.Fatal("tampered topology receipt activated approval", calls, err)
	}
	active, err := os.ReadFile(filepath.Join(self.stateDir, "plan.json"))
	if err != nil || !bytes.Equal(active, original) || !reflect.DeepEqual(before, self.journal.Entries()) {
		t.Fatal("failed authentication changed approval or journal", err)
	}
}

// An action delta remains ordinary setup, never cap-only adoption; cancellation
// and strict execution cannot enter the local provisional activation branch.
func TestProvisionalAllowanceAdoptionRejectsActionDeltaAndStrictMode(t *testing.T) {
	self, original := provisionalAllowanceAdoptionFixture(t)
	plan := *self.plan
	plan.Actions = append([]Action(nil), plan.Actions...)
	plan.Actions[0].Description += " changed approved action"
	self.plan = &plan
	if ok, err := self.authenticateProvisionalPlanOnlyAdoption(t.Context(), original); ok {
		t.Fatal("action delta was classified as a ceiling change", err)
	}
	if err := json.Unmarshal(original, &plan); err != nil {
		t.Fatal(err)
	}
	var err error
	self.plan, err = buildAllowanceOnlyPlanFromSource(self.cfg, original, plan.PlanHash)
	if err != nil {
		t.Fatal(err)
	}
	self.cfg.provisionalResume = nil
	if ok, err := self.authenticateProvisionalPlanOnlyAdoption(t.Context(), original); err == nil || ok {
		t.Fatal("strict invocation inherited provisional adoption", ok, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := self.authenticateProvisionalPlanOnlyAdoption(ctx, original); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled adoption continued", err)
	}
}

// A real immutable topology postcondition supplies local receipt authentication.
func persistProvisionalAdoptionTopologyTest(t *testing.T, executor *Executor) {
	t.Helper()
	action := actionByID(t, executor.plan, "topology.launch")
	head := testEVMHead(20, 0x35)
	record := &ActionPostcondition{
		Schema: "urnetwork-sim-action-postcondition-v4", DeploymentID: executor.plan.DeploymentID,
		PlanHash: executor.plan.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash,
		OperationalRPCMode: executor.cfg.OperationalRPCMode, IndependentRPC: independentRPCRequired(executor.cfg),
		SubstrateFinalized: head, EVMFinalized: head, EVMHashDomain: "evm-rpc", Observed: map[string]any{"synthetic": true},
		IndependentSubstrateFinalized: head, IndependentEVMFinalized: head, IndependentEVMHashDomain: "evm-rpc", IndependentObserved: map[string]any{"synthetic": true},
	}
	path, hash, err := executor.persistActionPostcondition(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := executor.journal.Append(JournalEntry{DeploymentID: executor.plan.DeploymentID, PlanHash: executor.plan.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageVerified, PostconditionPath: path, PostconditionHash: hash}); err != nil {
		t.Fatal(err)
	}

}

// Plan-only setup can keep the exact old generation stopped. A new process,
// changed manifest or accepting invocation cannot enter this handoff.
func TestProvisionalPlanAdoptionStoppedGeneration(t *testing.T) {
	self, original := provisionalAllowanceAdoptionFixture(t)
	ticks, err := processStartTimeTicks(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	manifest := SupervisorFile{Schema: "urnetwork-sim-supervisor-v1", DeploymentID: self.plan.DeploymentID,
		BinaryHash: "sha256:" + strings.Repeat("12", 32), Specs: []ProcessSpec{{ID: "validator-1", Role: "validator", Identity: "test-validator:1"}}}
	hash, err := canonicalHashHex(manifest)
	if err != nil {
		t.Fatal(err)
	}
	state := SupervisorState{Schema: "urnetwork-sim-supervisor-state-v1", UpdatedAt: "2026-01-01T00:00:00Z", ContractCleanupCutoff: "2026-01-01T00:00:00Z",
		ManifestHash: hash, SupervisorPID: os.Getpid(), SupervisorStartTimeTicks: ticks + 1,
		Processes: []ProcessState{{ID: "validator-1", Role: "validator", Identity: "test-validator:1", StartedAt: "2026-01-01T00:00:00Z", ExitError: "controlled stop"}}}
	writeState := func() {
		t.Helper()
		raw, err := json.Marshal(state)
		if err != nil {
			t.Fatal(err)
		}
		if err := atomicWrite(filepath.Join(self.stateDir, "supervisor.state.json"), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(self.stateDir, "supervisor.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	writeState()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "systemctl"), []byte("#!/bin/sh\nprintf '%s\\n' 'ActiveState=inactive' 'SubState=dead' 'Result=success' 'ExecMainCode=1' 'ExecMainStatus=0'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	first, err := prepareStoppedProvisionalTopology(t.Context(), self.cfg, self.stateDir, "setup")
	if err != nil || first == nil {
		t.Fatal("stopped approval required restarting the old plan", err)
	}
	second, err := prepareStoppedProvisionalTopology(t.Context(), self.cfg, self.stateDir, "setup")
	if err != nil {
		t.Fatal(err)
	}
	if err := provisionalStoppedAdoptionGeneration(first, second); err != nil {
		t.Fatal(err)
	}
	before := self.journal.Entries()
	calls := 0
	if err := self.activateProvisionalSetupRevision(t.Context(), original, func(context.Context, Action) error { calls++; return nil }); err != nil {
		t.Fatal(err)
	}
	if calls != 0 || !reflect.DeepEqual(before, self.journal.Entries()) {
		t.Fatal("stopped approval dispatched actions")
	}
	if err := prepareProvisionalResume(t.Context(), self.cfg, self.stateDir, "resume", cliOptions{Apply: true, ProvisionalResume: true, PlanHash: self.plan.PlanHash}, self.plan); err != nil {
		t.Fatal(err)
	}
	starts := 0
	start := func(context.Context, *Executor, *provisionalStoppedTopology, map[string]string) error {
		starts++
		return nil
	}
	if err := executeRetainedProvisionalResume(t.Context(), self, second, nil, start); err != nil || starts != 1 {
		t.Fatal("retained successor reopened pending setup instead of starting topology", starts, err)
	}
	if !reflect.DeepEqual(before, self.journal.Entries()) {
		t.Fatal("retained startup replayed setup")
	}
	self.cfg.provisionalResume.Record.FinalAcceptance = true
	if err := executeRetainedProvisionalResume(t.Context(), self, second, nil, start); err == nil || starts != 1 {
		t.Fatal("strict acceptance entered retained-only startup", starts, err)
	}
	self.cfg.provisionalResume.Record.FinalAcceptance = false
	stateBytes, err := os.ReadFile(filepath.Join(self.stateDir, "supervisor.state.json"))
	if err != nil {
		t.Fatal(err)
	}
	next := manifest
	next.ProvisionalProviderStartupObservationOnly = true
	next.BinaryHash = self.cfg.provisionalResume.Driver.ExecutableSHA256
	nextBytes, err := json.Marshal(next)
	if err != nil {
		t.Fatal(err)
	}
	publication := retainedStartupPublication{Schema: "urnetwork-sim-retained-start-publication-v1", PlanHash: self.plan.PlanHash,
		ProvenancePath: self.cfg.provisionalResume.RecordPath, ProvenanceHash: self.cfg.provisionalResume.RecordHash,
		Source: second, StateBytes: stateBytes, Original: raw, Successor: nextBytes}
	checkpoint, err := json.Marshal(publication)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(self.stateDir, "provisional-resumes", "retained-start-publication.json"), checkpoint, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(self.stateDir, "supervisor.json"), nextBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := recoverRetainedProvisionalPublication(t.Context(), self); err != nil {
		t.Fatal("manifest publication crash lost its retained checkpoint", err)
	}
	recovered, err := os.ReadFile(filepath.Join(self.stateDir, "supervisor.json"))
	if err != nil || !bytes.Equal(recovered, raw) || !reflect.DeepEqual(before, self.journal.Entries()) {
		t.Fatal("publication recovery changed more than its unstarted manifest", err)
	}
	if err := atomicWrite(filepath.Join(self.stateDir, "supervisor.json"), append(nextBytes, ' '), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := recoverRetainedProvisionalPublication(t.Context(), self); err == nil {
		t.Fatal("changed successor publication was restored without exact identity")
	}
	if err := atomicWrite(filepath.Join(self.stateDir, "supervisor.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	changed := *second
	changed.ManifestBytesSHA256 = "sha256:" + strings.Repeat("ab", 32)
	if err := provisionalStoppedAdoptionGeneration(first, &changed); err == nil {
		t.Fatal("changed stopped generation admitted")
	}
	state.Processes[0].PID = os.Getpid()
	writeState()
	if _, err := prepareStoppedProvisionalTopology(t.Context(), self.cfg, self.stateDir, "setup"); err == nil {
		t.Fatal("running validator admitted to stopped activation")
	}
	state.Processes[0].PID = 0
	writeState()
	self.cfg.provisionalResume.Record.FinalAcceptance = true
	if _, err := prepareStoppedProvisionalTopology(t.Context(), self.cfg, self.stateDir, "setup"); err == nil {
		t.Fatal("strict acceptance used stopped provisional adoption")
	}
	self.cfg.provisionalResume.Record.FinalAcceptance = false
	state.SupervisorStartTimeTicks = ticks
	writeState()
	current, err := prepareStoppedProvisionalTopology(t.Context(), self.cfg, self.stateDir, "setup")
	if err != nil {
		t.Fatal(err)
	}
	if err := provisionalStoppedAdoptionGeneration(first, current); err == nil {
		t.Fatal("restarted supervisor admitted to stopped activation")
	}
}

// Real signed relay liabilities and journal prefixes constrain the shared
// expansion transform. It may change reserve terms, never a funding action.
func TestProvisionalPlanAdoptionRelayExpansionPreservesLiabilities(t *testing.T) {
	_, self := newEvidenceRelayExpansionTest(t)
	persistProvisionalAdoptionTopologyTest(t, self)
	original, err := os.ReadFile(filepath.Join(self.stateDir, "plan.json"))
	if err != nil {
		t.Fatal(err)
	}
	continuation := evidenceRelayExpansionRequestTest(t, self)
	self.plan, err = appendEvidenceRelayContinuationPlan(self.plan, continuation)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := archiveReviewedSetupPlan(self.stateDir, self.plan); err != nil {
		t.Fatal(err)
	}
	self.cfg.provisionalResume = &provisionalResumeState{Driver: provisionalDriverProvenance{
		ExecutablePath: "/reviewed/test/sim-testnet", ExecutableSHA256: "sha256:" + strings.Repeat("12", 32),
		Build: releaseExecutableBuildIdentity{PackagePath: "github.com/urfoundation/sn/sim-testnet", ModulePath: "github.com/urfoundation/sn", Revision: strings.Repeat("34", 20), Modified: true}}}
	if err := prepareProvisionalResume(t.Context(), self.cfg, self.stateDir, "setup", cliOptions{Apply: true, ProvisionalResume: true, PlanHash: self.plan.PlanHash}, self.plan); err != nil {
		t.Fatal(err)
	}
	if ok, err := self.authenticateProvisionalPlanOnlyAdoption(t.Context(), original); err != nil || !ok {
		t.Fatal("exact relay reserve expansion rejected", ok, err)
	}
	before := self.journal.Entries()
	calls := 0
	if err := self.activateProvisionalSetupRevision(t.Context(), original, func(context.Context, Action) error { calls++; return nil }); err != nil {
		t.Fatal(err)
	}
	active, err := os.ReadFile(filepath.Join(self.stateDir, "plan.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := self.activateProvisionalSetupRevision(t.Context(), active, func(context.Context, Action) error { calls++; return nil }); err != nil {
		t.Fatal("repeated relay activation lost original source", err)
	}
	if calls != 0 || !reflect.DeepEqual(before, self.journal.Entries()) {
		t.Fatal("relay reserve activation dispatched or lost liabilities")
	}
	changed := *self.plan
	changed.Actions = append([]Action(nil), changed.Actions...)
	for index := range changed.Actions {
		if changed.Actions[index].ID == "evm.fund-keeper" {
			changed.Actions[index].Description += " changed funding"
		}
	}
	self.plan = &changed
	if ok, err := self.authenticateProvisionalPlanOnlyAdoption(t.Context(), active); err == nil || ok {
		t.Fatal("relay approval admitted a funding action change", ok, err)
	}
}

// The actual reviewed-plan loader and local activation admit a v6 capture with
// its original non-accepting marker. No chain reader or replay worker exists.
func TestProvisionalPlanAdoptionSourceExpansionRetainsCaptureMarker(t *testing.T) {
	fixture, self := newEvidenceRelayExpansionTest(t)
	// Owned-route admission requires private IPv4; this generated synthetic
	// authority is only an identity operand and is never dialed.
	authority := fmt.Sprintf("10.%d.%d.%d:9944", 71, 23, 9)
	var err error
	self.cfg, err = prepareOwnedRPCConfiguration(self.cfg, authority)
	if err != nil {
		t.Fatal(err)
	}
	fixture.cfg = self.cfg
	source := *self.plan
	source.PriorPlanHashes = append(append([]string(nil), source.PriorPlanHashes...), source.PlanHash)
	source.OwnedRPCAuthority = authority
	source.ResolvedInputsHash, err = resolvedInputsHash(self.cfg)
	if err != nil {
		t.Fatal(err)
	}
	source.PlanHash, err = source.hash()
	if err != nil {
		t.Fatal(err)
	}
	if err := writeRunInputs(self.cfg, self.stateDir, &source, self.roles); err != nil {
		t.Fatal(err)
	}
	self.plan = &source
	persistProvisionalAdoptionTopologyTest(t, self)
	original, err := os.ReadFile(filepath.Join(self.stateDir, "plan.json"))
	if err != nil {
		t.Fatal(err)
	}
	continuation := evidenceRelaySourceExpansionRequestTest(t, fixture, self)
	driver := provisionalDriverProvenance{ExecutablePath: "/reviewed/test/sim-testnet", ExecutableSHA256: "sha256:" + strings.Repeat("12", 32),
		Build: releaseExecutableBuildIdentity{PackagePath: "github.com/urfoundation/sn/sim-testnet", ModulePath: "github.com/urfoundation/sn", Revision: strings.Repeat("34", 20), Modified: true}}
	preview := *self.cfg
	preview.readOnlyAudit = true
	preview.provisionalResume = &provisionalResumeState{Driver: driver}
	captureOptions := cliOptions{ProvisionalCapture: true, PlanHash: source.PlanHash, OwnedRPCAuthority: authority, RelayEndBlock: continuation.EndBlock}
	if err := prepareProvisionalResume(t.Context(), &preview, self.stateDir, "relay-continuation", captureOptions, &source); err != nil {
		t.Fatal(err)
	}
	if err := validateProvisionalRelayCaptureContext(&preview, &source); err != nil {
		t.Fatal(err)
	}
	continuation.ProvisionalCapture = &EvidenceRelayProvisionalCapture{Record: *preview.provisionalResume.Record, RecordPath: preview.provisionalResume.RecordPath, RecordSHA256: preview.provisionalResume.RecordHash}
	reviewed, err := appendEvidenceRelayContinuationPlan(&source, continuation)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := archiveReviewedSetupPlan(self.stateDir, reviewed); err != nil {
		t.Fatal(err)
	}
	command, options, err := parseCLI([]string{"setup", "--provisional-resume", "--apply", "--plan-hash", reviewed.PlanHash, "--owned-rpc-authority", authority})
	if err != nil || options.PrepareOnly {
		t.Fatal("documented route failed command admission", err)
	}
	self.cfg.provisionalResume = &provisionalResumeState{Driver: driver}
	self.plan, err = loadInvocationPlan(self.cfg, self.stateDir, command, options)
	if err != nil {
		t.Fatal("v6 captured review did not load through the actual setup route", err)
	}
	if err := prepareProvisionalResume(t.Context(), self.cfg, self.stateDir, command, options, self.plan); err != nil {
		t.Fatal(err)
	}
	if ok, err := self.authenticateProvisionalPlanOnlyAdoption(t.Context(), original); err != nil || !ok {
		t.Fatal("v6 captured review required another ledger replay", ok, err)
	}
	before := self.journal.Entries()
	calls := 0
	if err := self.activateProvisionalSetupRevision(t.Context(), original, func(context.Context, Action) error { calls++; return errors.New("local approval dispatched an action") }); err != nil {
		t.Fatal(err)
	}
	active, err := os.ReadFile(filepath.Join(self.stateDir, "plan.json"))
	if err != nil {
		t.Fatal(err)
	}
	archived, err := os.ReadFile(filepath.Join(self.stateDir, "plans", stringsTrim0x(reviewed.PlanHash)+".json"))
	if err != nil || !bytes.Equal(active, archived) || calls != 0 || !reflect.DeepEqual(before, self.journal.Entries()) {
		t.Fatal("v6 adoption changed the exact review or dispatched a transaction", calls, err)
	}
	adopted, err := decodePersistedPlanWire(active)
	if err != nil || !reflect.DeepEqual(adopted.EvidenceRelayContinuation.SourceBounds, continuation.SourceBounds) || !reflect.DeepEqual(adopted.EvidenceRelayContinuation.ProvisionalCapture, continuation.ProvisionalCapture) || adopted.EvidenceRelayContinuation.HistoricalLiabilityWei != continuation.HistoricalLiabilityWei {
		t.Fatal("v6 adoption changed original bounds, capture provenance or liabilities", err)
	}
	if _, err := loadPlanIdentityBytes(self.cfg, active, false); !errors.Is(err, errPersistedPlanIdentityMismatch) || !strings.Contains(err.Error(), "relay_capture_requires_strict_reconciliation") {
		t.Fatal("provisional v6 adoption granted strict acceptance", err)
	}
	var activation struct {
		PlanOnly        bool `json:"plan_only"`
		FinalAcceptance bool `json:"final_acceptance"`
	}
	if err := readJSONFile(filepath.Join(filepath.Dir(self.cfg.provisionalResume.RecordPath), "setup-activation.json"), &activation); err != nil || !activation.PlanOnly || activation.FinalAcceptance {
		t.Fatal("local activation lost its non-accepting boundary", err)
	}
	provenance, err := os.ReadFile(continuation.ProvisionalCapture.RecordPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(continuation.ProvisionalCapture.RecordPath, append(provenance, ' '), 0o600); err != nil {
		t.Fatal(err)
	}
	if ok, err := self.authenticateProvisionalPlanOnlyAdoption(t.Context(), active); err == nil || ok {
		t.Fatal("changed capture provenance retained adoption authority", ok, err)
	}
}

// The fresh driver may replace executable bytes and current approval bindings;
// retained process arguments, runtime paths and original manifest stay exact.
func TestProvisionalPlanAdoptionRetainedManifestRejectsSubstitution(t *testing.T) {
	self, original := provisionalAllowanceAdoptionFixture(t)
	source, err := decodePersistedPlanWire(original)
	if err != nil {
		t.Fatal(err)
	}
	bins := map[string]string{"sim-testnet": filepath.Join(t.TempDir(), "sim-testnet"), connectServerBinaryName: filepath.Join(t.TempDir(), "connect-test")}
	for _, path := range bins {
		if err := os.WriteFile(path, []byte("qualified synthetic executable"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	driverHash, err := fileSHA256(bins["sim-testnet"])
	if err != nil {
		t.Fatal(err)
	}
	self.cfg.provisionalResume.Driver.ExecutableSHA256 = driverHash
	self.cfg.provisionalResume.Record.Driver.ExecutableSHA256 = driverHash
	manifest := SupervisorFile{Schema: "urnetwork-sim-supervisor-v1", DeploymentID: self.plan.DeploymentID,
		BinaryHash: "sha256:" + strings.Repeat("cd", 32), Specs: []ProcessSpec{
			{ID: "operator-1-api", Role: "operator-api", Command: bins["sim-testnet"], Env: map[string]string{validatorViewFilterPlanHashEnv: source.PlanHash, "synthetic": "retained"}, Args: []string{"__server_api", "--port=18888"}},
			{ID: "validator-1", Role: "validator", Command: bins["sim-testnet"], Args: []string{"__validator", "--config=/synthetic/retained.yml", "--provisional-activation-setup=/synthetic/prior.json", "--provisional-activation-setup-sha256=sha256:prior"}},
		}}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := canonicalHashHex(manifest)
	if err != nil {
		t.Fatal(err)
	}
	stopped := &provisionalStoppedTopology{ManifestBytesSHA256: bytesSHA256(raw), ManifestHash: hash, SupervisorBinarySHA256: manifest.BinaryHash}
	next, err := retainedProvisionalSupervisor(self.cfg, self.plan, stopped, raw, bins)
	if err != nil {
		t.Fatal(err)
	}
	if next.BinaryHash != driverHash || next.Specs[0].Env[validatorViewFilterPlanHashEnv] != self.plan.PlanHash || next.Specs[0].Env["synthetic"] != "retained" || !reflect.DeepEqual(next.Specs[1].Args, manifest.Specs[1].Args[:2]) {
		t.Fatal("retained runtime inputs were rebuilt or old handoff reused")
	}
	if _, err := retainedProvisionalSupervisor(self.cfg, self.plan, stopped, append(raw, ' '), bins); err == nil {
		t.Fatal("retained source bytes changed unnoticed")
	}
	self.cfg.provisionalResume.Record.FinalAcceptance = true
	if _, err := retainedProvisionalSupervisor(self.cfg, self.plan, stopped, raw, bins); err == nil {
		t.Fatal("strict final used provisional executable transform")
	}
	self.cfg.provisionalResume.Record.FinalAcceptance = false
	if err := os.WriteFile(bins[connectServerBinaryName], []byte("substituted executable"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := retainedProvisionalSupervisor(self.cfg, self.plan, stopped, raw, bins); err == nil {
		t.Fatal("substituted child executable accepted")
	}
}
