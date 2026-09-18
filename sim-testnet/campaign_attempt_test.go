package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

type recordingHandoffLifecycle struct {
	events      []string
	handoff     []byte
	handoffHash string
	releaseRun  string
	production  string
}

type recordingAttemptFaultDriver struct{ recoverCalls int }

type recordingContextualLifecycle struct {
	recordingHandoffLifecycle
	contextCalls int
}

type rejectingResumeLifecycle struct {
	recordingHandoffLifecycle
	resumeCalls int
}

func (*recordingAttemptFaultDriver) Apply(context.Context, scenarioFaultSpec) ([]FaultProcessEvidence, error) {
	return nil, errors.New("unexpected apply")
}

func (*recordingAttemptFaultDriver) Restore(context.Context, scenarioFaultSpec) ([]FaultProcessEvidence, error) {
	return nil, errors.New("unexpected restore")
}

func (driver *recordingAttemptFaultDriver) Recover(context.Context) error {
	driver.recoverCalls++
	return nil
}

func (lifecycle *recordingHandoffLifecycle) AuthenticateReleaseHandoff(data []byte, hash, releaseRunID string) error {
	lifecycle.events = append(lifecycle.events, "authenticate")
	lifecycle.handoff = append([]byte(nil), data...)
	lifecycle.handoffHash = hash
	lifecycle.releaseRun = releaseRunID
	return nil
}

func (lifecycle *recordingHandoffLifecycle) BeginPhase(_ string, runID string) error {
	lifecycle.events = append(lifecycle.events, "begin")
	lifecycle.production = runID
	return nil
}

func (lifecycle *recordingContextualLifecycle) BeginPhaseContext(ctx context.Context, _ string, runID string) error {
	if ctx == nil {
		return errors.New("missing campaign context")
	}
	lifecycle.contextCalls++
	lifecycle.events = append(lifecycle.events, "context-begin")
	lifecycle.production = runID
	return nil
}

func (lifecycle *rejectingResumeLifecycle) ValidatePhaseResume(string, string) error {
	lifecycle.resumeCalls++
	lifecycle.events = append(lifecycle.events, "validate-resume")
	return errors.New("persisted lifecycle mismatch")
}

func (*recordingHandoffLifecycle) BindAcceptanceWindowForPhase(string, *ScenarioAcceptanceWindow) error {
	return nil
}

func (*recordingHandoffLifecycle) Advance(context.Context, *ScenarioObservation, []ScenarioFaultRecord) error {
	return nil
}

func (*recordingHandoffLifecycle) Complete() bool { return false }

func bindCampaignAttemptBoundaryFixture(t *testing.T, cfg *ResolvedConfig, stateDir string) (*scenarioCampaignAttempt, string, *ScenarioAcceptanceWindow, []ScenarioFaultRecord) {
	t.Helper()
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Date(2026, 9, 3, 7, 0, 0, 0, time.UTC)
	attempt, err := loadOrCreateScenarioCampaignAttempt(cfg, stateDir, roles, campaignTestPlanHash, "release-1.0", nil, started)
	if err != nil {
		t.Fatal(err)
	}
	if err := attempt.updateProgress(false, true); err != nil {
		t.Fatal(err)
	}
	runDir := filepath.Join(stateDir, "runs", attempt.payload.RunID)
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	observationLogPrefix, err := captureScenarioObservationLogPrefix(filepath.Join(runDir, "observations.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	campaignStart := testScenarioObservation(cfg, 6)
	campaignStart.ObservedAt = started.Format(time.RFC3339Nano)
	campaignStart.ObservationHash, _ = canonicalHashHex(campaignStart)
	baseline := testScenarioObservation(cfg, 7)
	baseline.ObservedAt = started.Add(time.Minute).Format(time.RFC3339Nano)
	baseline.ObservationHash, _ = canonicalHashHex(baseline)
	if err := appendObservation(filepath.Join(runDir, "observations.jsonl"), campaignStart); err != nil {
		t.Fatal(err)
	}
	if err := appendObservation(filepath.Join(runDir, "observations.jsonl"), baseline); err != nil {
		t.Fatal(err)
	}
	definition, err := scenarioDefinitionFor(cfg, "release-1.0")
	if err != nil {
		t.Fatal(err)
	}
	definitionHash, err := scenarioDefinitionHash(definition)
	if err != nil {
		t.Fatal(err)
	}
	window, err := buildScenarioAcceptanceWindow(cfg, definition, baseline)
	if err != nil {
		t.Fatal(err)
	}
	faults, err := initializeFaultRecords(window.StartBlock, definition.Faults)
	if err != nil {
		t.Fatal(err)
	}
	for index := range faults {
		if faults[index].PreAcceptance {
			faults[index].ArmedBlock = baseline.Status.Contracts.FinalizedHead.Number
			faults[index].ArmedBlockHash = baseline.Status.Contracts.FinalizedHead.Hash
		}
	}
	adversary := &AdversaryCampaignEvidence{
		Schema: "urnetwork-adversary-campaign-v1", Release: "1.0", MatrixHash: definition.AdversarialMatrixHash,
		StartedAt: started.Add(-time.Minute).Format(time.RFC3339Nano), HappyPathStartedAt: started.Format(time.RFC3339Nano), Status: "running",
	}
	if err := attempt.bindAcceptanceBoundary(runDir, "0x"+strings.Repeat("77", 32), definitionHash, adversary, started.Add(2*time.Minute), campaignStart, baseline, window, faults, observationLogPrefix, "0x"+strings.Repeat("88", 32)); err != nil {
		t.Fatal(err)
	}
	if err := writeScenarioFaultEvidence(runDir, faults); err != nil {
		t.Fatal(err)
	}
	return attempt, runDir, window, faults
}

func TestProductionHandoffAuthenticationPrecedesPrepareMutation(t *testing.T) {
	cfg := testResolvedConfig(t)
	stateDir := t.TempDir()
	_, roles, _ := writeReleaseCampaignFixture(t, cfg, stateDir, 26, 32)
	gate, err := loadReleaseCampaignGate(cfg, stateDir, roles)
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := loadOrCreateScenarioCampaignAttempt(cfg, stateDir, roles, campaignTestPlanHash, "production-soak", gate, time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	livePath := filepath.Join(stateDir, "public", "fleet-lifecycle.json")
	live, err := os.ReadFile(livePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(livePath, append(live, ' '), 0o644); err != nil {
		t.Fatal(err)
	}
	prepareCalls := 0
	err = beginScenarioCampaignPreparation(context.Background(), "production-soak", attempt.payload.RunID, scenarioRunOptions{
		Attempt: attempt,
		Prepare: func(context.Context) error {
			prepareCalls++
			return nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "differs from the exact signed release handoff") || prepareCalls != 0 || attempt.payload.HandoffAuthenticated || attempt.payload.PreparationComplete {
		t.Fatalf("handoff drift error=%v prepare_calls=%d attempt=%+v", err, prepareCalls, attempt.payload)
	}
}

func TestScenarioCampaignAttemptRetryReusesRunIDAndSkipsCompletedPreparation(t *testing.T) {
	cfg := testResolvedConfig(t)
	stateDir := t.TempDir()
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	firstTime := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	first, err := loadOrCreateScenarioCampaignAttempt(cfg, stateDir, roles, campaignTestPlanHash, "release-1.0", nil, firstTime)
	if err != nil {
		t.Fatal(err)
	}
	prepareCalls := 0
	options := scenarioRunOptions{Attempt: first, Prepare: func(context.Context) error { prepareCalls++; return nil }}
	if err := beginScenarioCampaignPreparation(context.Background(), "release-1.0", first.payload.RunID, options); err != nil {
		t.Fatal(err)
	}
	second, err := loadOrCreateScenarioCampaignAttempt(cfg, stateDir, roles, campaignTestPlanHash, "release-1.0", nil, firstTime.Add(48*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if second.payload.RunID != first.payload.RunID || !second.payload.PreparationComplete {
		t.Fatalf("retry rebound attempt first=%+v second=%+v", first.payload, second.payload)
	}
	options.Attempt = second
	if err := beginScenarioCampaignPreparation(context.Background(), "release-1.0", second.payload.RunID, options); err != nil {
		t.Fatal(err)
	}
	if prepareCalls != 1 {
		t.Fatalf("completed preparation executed %d times", prepareCalls)
	}
}

func TestScenarioCampaignPreparationKeepsPendingLifecycleInOneInvocation(t *testing.T) {
	lifecycle := &recordingContextualLifecycle{}
	if err := beginScenarioCampaignPreparation(t.Context(), "release-1.0", "retained-run", scenarioRunOptions{FleetLifecycle: lifecycle}); err != nil {
		t.Fatal(err)
	}
	if lifecycle.contextCalls != 1 || !slices.Equal(lifecycle.events, []string{"context-begin"}) || lifecycle.production != "retained-run" {
		t.Fatalf("campaign bypassed contextual lifecycle: %+v", lifecycle)
	}
}

func TestScenarioCampaignPreparationValidatesPersistedLifecycleBeforePrepare(t *testing.T) {
	lifecycle := &rejectingResumeLifecycle{}
	prepareCalls := 0
	err := beginScenarioCampaignPreparation(t.Context(), "release-1.0", "recovery-run", scenarioRunOptions{
		FleetLifecycle: lifecycle,
		Prepare: func(context.Context) error {
			prepareCalls++
			return nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "validate fleet lifecycle resume before preparation") || lifecycle.resumeCalls != 1 || prepareCalls != 0 || !slices.Equal(lifecycle.events, []string{"validate-resume"}) {
		t.Fatalf("resume validation error=%v calls=%d prepare=%d events=%v", err, lifecycle.resumeCalls, prepareCalls, lifecycle.events)
	}
}

func TestScenarioCampaignAttemptRejectsFreshProductionRebind(t *testing.T) {
	cfg := testResolvedConfig(t)
	stateDir := t.TempDir()
	_, roles, _ := writeReleaseCampaignFixture(t, cfg, stateDir, 26, 32)
	gate, err := loadReleaseCampaignGate(cfg, stateDir, roles)
	if err != nil {
		t.Fatal(err)
	}
	first, err := loadOrCreateScenarioCampaignAttempt(cfg, stateDir, roles, campaignTestPlanHash, "production-soak", gate, time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	second, err := loadOrCreateScenarioCampaignAttempt(cfg, stateDir, roles, campaignTestPlanHash, "production-soak", gate, time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if first.payload.RunID != second.payload.RunID {
		t.Fatalf("production retry created fresh run %q after %q", second.payload.RunID, first.payload.RunID)
	}
	substituted := *gate
	substituted.ResultHash = "0x" + strings.Repeat("99", 32)
	if _, err := loadOrCreateScenarioCampaignAttempt(cfg, stateDir, roles, campaignTestPlanHash, "production-soak", &substituted, time.Now()); err == nil || !strings.Contains(err.Error(), "different release predecessor") {
		t.Fatalf("production predecessor rebind error=%v", err)
	}
}

func TestInitialScenarioFailurePreservesExactProductionPredecessor(t *testing.T) {
	cfg := testResolvedConfig(t)
	stateDir := t.TempDir()
	_, roles, _ := writeReleaseCampaignFixture(t, cfg, stateDir, 26, 32)
	gate, err := loadReleaseCampaignGate(cfg, stateDir, roles)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	attempt, err := loadOrCreateScenarioCampaignAttempt(cfg, stateDir, roles, campaignTestPlanHash, "production-soak", gate, started)
	if err != nil {
		t.Fatal(err)
	}
	runDir := filepath.Join(stateDir, "runs", attempt.payload.RunID)
	result, gotErr := writeInitialScenarioFailure(cfg, runDir, attempt.payload.RunID, "0x"+strings.Repeat("55", 32), scenarioDefinition{Name: "production-soak"}, started, nil, attempt, errors.New("fixture failure"))
	if gotErr == nil || result == nil || !releaseCampaignGatesEqual(result.PriorRelease, gate) {
		t.Fatalf("initial failure result=%+v error=%v", result, gotErr)
	}
	var persisted ScenarioResult
	if err := decodeStrictJSONFile(filepath.Join(runDir, "result.json"), &persisted); err != nil {
		t.Fatal(err)
	}
	if !releaseCampaignGatesEqual(persisted.PriorRelease, gate) {
		t.Fatal("persisted initial failure lost its exact release predecessor")
	}
}

func TestReleaseCampaignGateRejectsLifecycleHandoffByteDrift(t *testing.T) {
	cfg := testResolvedConfig(t)
	stateDir := t.TempDir()
	result, roles, runDir := writeReleaseCampaignFixture(t, cfg, stateDir, 26, 32)
	path := filepath.Join(runDir, scenarioLifecycleHandoffFilename)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(path, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := validateReleaseCampaignComplete(cfg, roles, runDir, result); err == nil || !strings.Contains(err.Error(), "file hashes") {
		t.Fatalf("handoff byte drift error=%v", err)
	}
}

func TestExactReleaseCampaignGateRejectsSignedGraphSplice(t *testing.T) {
	cfg := testResolvedConfig(t)
	stateDir := t.TempDir()
	_, roles, _ := writeReleaseCampaignFixture(t, cfg, stateDir, 26, 32)
	gate, err := loadReleaseCampaignGate(cfg, stateDir, roles)
	if err != nil {
		t.Fatal(err)
	}
	spliced := *gate
	spliced.CompleteContentHash = "sha256:" + strings.Repeat("88", 32)
	if _, _, err := validateExactReleaseCampaignGate(cfg, stateDir, roles, &spliced); err == nil || !strings.Contains(err.Error(), "differs from the exact signed") {
		t.Fatalf("signed graph splice error=%v", err)
	}
}

func TestNewerValidReleaseCannotRetargetDurableProductionAttempt(t *testing.T) {
	cfg := testResolvedConfig(t)
	stateDir := t.TempDir()
	_, roles, _ := writeReleaseCampaignFixture(t, cfg, stateDir, 26, 32)
	original, err := loadReleaseCampaignGate(cfg, stateDir, roles)
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := loadOrCreateScenarioCampaignAttempt(cfg, stateDir, roles, campaignTestPlanHash, "production-soak", original, time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}

	otherState := t.TempDir()
	newerResult, _, newerRunDir := writeScenarioCampaignFixture(t, cfg, otherState, "release-1.0", 46, 52, time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC))
	destination := filepath.Join(stateDir, "runs", newerResult.RunID)
	if err := os.CopyFS(destination, os.DirFS(newerRunDir)); err != nil {
		t.Fatal(err)
	}
	newest, err := loadReleaseCampaignGate(cfg, stateDir, roles)
	if err != nil {
		t.Fatal(err)
	}
	if newest.RunID != newerResult.RunID || releaseCampaignGatesEqual(newest, original) {
		t.Fatalf("fixture did not install a distinct newer valid release: original=%+v newest=%+v", original, newest)
	}
	loaded, err := readScenarioCampaignAttempt(cfg, stateDir, roles, campaignTestPlanHash, "production-soak")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.payload.RunID != attempt.payload.RunID || !releaseCampaignGatesEqual(loaded.payload.PriorRelease, original) {
		t.Fatalf("durable production attempt was retargeted: %+v", loaded.payload)
	}
	executor := &Executor{cfg: cfg, stateDir: stateDir, plan: &SetupPlan{PlanHash: campaignTestPlanHash}, roles: roles}
	bound, err := executor.boundProductionReleaseGate()
	if err != nil {
		t.Fatal(err)
	}
	if !releaseCampaignGatesEqual(bound, original) || releaseCampaignGatesEqual(bound, newest) {
		t.Fatalf("policy gate selected newest release instead of result-bound predecessor: bound=%+v", bound)
	}
}

func TestCanceledProductionCampaignResumesExactAttemptRunID(t *testing.T) {
	cfg := testResolvedConfig(t)
	stateDir := t.TempDir()
	_, roles, _ := writeReleaseCampaignFixture(t, cfg, stateDir, 26, 32)
	journal := openCampaignTestJournal(t, stateDir)
	var runIDs []string
	productionCalls := 0
	runner := func(_ context.Context, _ *ResolvedConfig, fixtureDir, phase string, _ *Journal, _ *Executor, attempt *scenarioCampaignAttempt) error {
		if phase != "production-soak" {
			return errors.New("completed release was unexpectedly rerun")
		}
		productionCalls++
		runIDs = append(runIDs, attempt.payload.RunID)
		if productionCalls == 1 {
			return context.Canceled
		}
		writeScenarioCampaignFixture(t, cfg, fixtureDir, phase, 32, 36)
		return nil
	}
	analyzer := func(context.Context, *ResolvedConfig, string, string, *RoleSecrets, *ScenarioResult) error {
		return nil
	}
	firstErr := runReleaseCandidateCampaignWithAnalyzer(context.Background(), cfg, stateDir, journal, campaignTestExecutor(), roles, runner, noOpCampaignPreflight, analyzer)
	if !errors.Is(firstErr, context.Canceled) {
		t.Fatalf("first production cancellation error=%v", firstErr)
	}
	if err := runReleaseCandidateCampaignWithAnalyzer(context.Background(), cfg, stateDir, journal, campaignTestExecutor(), roles, runner, noOpCampaignPreflight, analyzer); err != nil {
		t.Fatal(err)
	}
	if productionCalls != 2 || len(runIDs) != 2 || runIDs[0] != runIDs[1] {
		t.Fatalf("production retry calls=%d run_ids=%v", productionCalls, runIDs)
	}
}

func TestScenarioCompletionsBindExactLifecycleLineage(t *testing.T) {
	cfg := testResolvedConfig(t)
	stateDir := t.TempDir()
	release, roles, releaseDir := writeReleaseCampaignFixture(t, cfg, stateDir, 26, 32)
	var releaseEnvelope ReleaseEvidenceEnvelope
	if err := decodeStrictJSONFile(filepath.Join(releaseDir, "complete.json"), &releaseEnvelope); err != nil {
		t.Fatal(err)
	}
	var releasePayload scenarioCompletePayload
	if err := decodeStrictJSONBytes(releaseEnvelope.Payload, &releasePayload); err != nil {
		t.Fatal(err)
	}
	if releasePayload.LifecycleHandoff == nil || release.LifecycleHandoff == nil || *releasePayload.LifecycleHandoff != *release.LifecycleHandoff || releasePayload.PriorRelease != nil {
		t.Fatalf("release completion lineage=%+v result=%+v", releasePayload, release)
	}
	production, _, productionDir := writeScenarioCampaignFixture(t, cfg, stateDir, "production-soak", 32, 36)
	var productionEnvelope ReleaseEvidenceEnvelope
	if err := decodeStrictJSONFile(filepath.Join(productionDir, "complete.json"), &productionEnvelope); err != nil {
		t.Fatal(err)
	}
	var productionPayload scenarioCompletePayload
	if err := decodeStrictJSONBytes(productionEnvelope.Payload, &productionPayload); err != nil {
		t.Fatal(err)
	}
	gate, err := loadReleaseCampaignGate(cfg, stateDir, roles)
	if err != nil {
		t.Fatal(err)
	}
	if productionPayload.PriorRelease == nil || production.PriorRelease == nil || !releaseCampaignGatesEqual(productionPayload.PriorRelease, gate) || !releaseCampaignGatesEqual(production.PriorRelease, gate) || productionPayload.LifecycleHandoff != nil {
		t.Fatalf("production completion lineage=%+v result=%+v", productionPayload, production)
	}
}

func TestScenarioCampaignAttemptRejectsUnsignedObservationSuffixAfterAcceptance(t *testing.T) {
	cfg := testResolvedConfig(t)
	attempt, runDir, window, _ := bindCampaignAttemptBoundaryFixture(t, cfg, t.TempDir())
	later := testScenarioObservation(cfg, window.BaselineEpoch)
	later.ObservedAt = time.Date(2026, 9, 3, 7, 2, 0, 0, time.UTC).Format(time.RFC3339Nano)
	later.Status.Contracts.FinalizedHead = ChainHead{Number: window.BaselineHead.Number + 1, Hash: "0x" + strings.Repeat("cd", 32)}
	later.ObservationHash, _ = canonicalHashHex(later)
	if err := appendObservation(filepath.Join(runDir, "observations.jsonl"), later); err != nil {
		t.Fatal(err)
	}

	if _, _, _, _, _, err := attempt.loadAuthenticatedRuntimeForensics(runDir); err == nil || !strings.Contains(err.Error(), "unauthenticated suffix") {
		t.Fatalf("unsigned observation suffix error=%v", err)
	}
}

func TestScenarioCampaignAttemptAuthenticatesCurrentInvocationAfterRetainedFailure(t *testing.T) {
	cfg := testResolvedConfig(t)
	stateDir := t.TempDir()
	runDir := filepath.Join(stateDir, "runs", "20260903T070000.000000000Z-release-1.0")
	historical := testScenarioObservation(cfg, 5)
	historical.ObservedAt = time.Date(2026, 9, 2, 23, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	historical.Status.Contracts = nil
	historical.ObservationHash = ""
	historical.ObservationHash, _ = canonicalHashHex(historical)
	if err := appendObservation(filepath.Join(runDir, "observations.jsonl"), historical); err != nil {
		t.Fatal(err)
	}
	retained, err := os.ReadFile(filepath.Join(runDir, "observations.jsonl"))
	if err != nil {
		t.Fatal(err)
	}

	attempt, boundRunDir, window, faults := bindCampaignAttemptBoundaryFixture(t, cfg, stateDir)
	boundary := attempt.payload.AcceptanceBoundary
	if boundRunDir != runDir || boundary == nil || boundary.RetainedObservationLogBytes != uint64(len(retained)) || boundary.RetainedObservationLogContentHash != bytesSHA256(retained) {
		t.Fatalf("retained observation cut was not signed: run=%s boundary=%+v", boundRunDir, boundary)
	}
	data, err := os.ReadFile(filepath.Join(runDir, "observations.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, retained) {
		t.Fatal("current invocation replaced retained observation evidence")
	}
	later := testScenarioObservation(cfg, window.BaselineEpoch)
	later.ObservedAt = time.Date(2026, 9, 3, 7, 3, 0, 0, time.UTC).Format(time.RFC3339Nano)
	later.Status.Contracts.FinalizedHead = ChainHead{Number: window.BaselineHead.Number + 1, Hash: "0x" + strings.Repeat("cd", 32)}
	later.ObservationHash, _ = canonicalHashHex(later)
	if err := appendObservation(filepath.Join(runDir, "observations.jsonl"), later); err != nil {
		t.Fatal(err)
	}
	if err := attempt.updateAuthenticatedRuntime(runDir, faults); err != nil {
		t.Fatal(err)
	}
	history, campaignStart, baseline, current, _, err := attempt.loadAuthenticatedRuntimeForensics(runDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 3 || campaignStart.Status == nil || campaignStart.Status.Contracts == nil || baseline.ObservationHash != window.BaselineObservationHash || current.ObservationHash != later.ObservationHash {
		t.Fatalf("authenticated invocation suffix is wrong: history=%d start=%+v baseline=%+v current=%+v", len(history), campaignStart, baseline, current)
	}
}

func TestScenarioCampaignAttemptZeroCutStillRejectsIncompleteObservation(t *testing.T) {
	cfg := testResolvedConfig(t)
	stateDir := t.TempDir()
	runDir := filepath.Join(stateDir, "runs", "20260903T070000.000000000Z-release-1.0")
	historical := testScenarioObservation(cfg, 5)
	historical.Status.Contracts = nil
	historical.ObservationHash = ""
	historical.ObservationHash, _ = canonicalHashHex(historical)
	if err := appendObservation(filepath.Join(runDir, "observations.jsonl"), historical); err != nil {
		t.Fatal(err)
	}
	attempt, _, _, _ := bindCampaignAttemptBoundaryFixture(t, cfg, stateDir)
	data, err := os.ReadFile(filepath.Join(runDir, "observations.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	zeroCut := *attempt.payload.AcceptanceBoundary
	zeroCut.RetainedObservationLogBytes = 0
	zeroCut.RetainedObservationLogContentHash = bytesSHA256(nil)
	if _, _, _, _, err := validateScenarioAttemptObservationHistory(&zeroCut, data); err == nil || !strings.Contains(err.Error(), "record 0: scenario observation has no finalized contract identity") {
		t.Fatalf("zero-cut incomplete observation error=%v", err)
	}
}

func TestScenarioCampaignAttemptRetainedPrefixHashRejectsIndependentSubstitution(t *testing.T) {
	cfg := testResolvedConfig(t)
	stateDir := t.TempDir()
	runDir := filepath.Join(stateDir, "runs", "20260903T070000.000000000Z-release-1.0")
	historical := testScenarioObservation(cfg, 5)
	historical.Status.Contracts = nil
	historical.ObservationHash = ""
	historical.ObservationHash, _ = canonicalHashHex(historical)
	if err := appendObservation(filepath.Join(runDir, "observations.jsonl"), historical); err != nil {
		t.Fatal(err)
	}
	attempt, _, _, _ := bindCampaignAttemptBoundaryFixture(t, cfg, stateDir)
	data, err := os.ReadFile(filepath.Join(runDir, "observations.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	data[0] ^= 1
	rebound := *attempt.payload.AcceptanceBoundary
	rebound.ObservationLogContentHash = bytesSHA256(data)
	if _, _, _, _, err := validateScenarioAttemptObservationHistory(&rebound, data); err == nil || !strings.Contains(err.Error(), "retained prefix was substituted") {
		t.Fatalf("retained prefix substitution error=%v", err)
	}
}

func TestScenarioCampaignAttemptRejectsSignedBaselineByteSubstitution(t *testing.T) {
	cfg := testResolvedConfig(t)
	attempt, runDir, _, _ := bindCampaignAttemptBoundaryFixture(t, cfg, t.TempDir())
	path := filepath.Join(runDir, "observations.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)/2] ^= 1
	if err := atomicWrite(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, _, err := attempt.loadAuthenticatedRuntimeForensics(runDir); err == nil || !strings.Contains(err.Error(), "prefix was substituted") {
		t.Fatalf("baseline substitution error=%v", err)
	}
}

func TestScenarioCampaignAttemptLoadsExactSignedMidFaultLedgerForForensics(t *testing.T) {
	cfg := testResolvedConfig(t)
	attempt, runDir, window, faults := bindCampaignAttemptBoundaryFixture(t, cfg, t.TempDir())
	activeIndex := -1
	for index := range faults {
		if !faults[index].PreAcceptance && faults[index].ActivationCondition == "" {
			activeIndex = index
			break
		}
	}
	if activeIndex < 0 {
		t.Fatal("release fixture has no ordinary fault for mid-fault resume")
	}
	record := &faults[activeIndex]
	record.Status = "active"
	record.AppliedBlock = record.TriggerBlock
	record.AppliedBlockHash = "0x" + strings.Repeat("de", 32)
	for processIndex, target := range record.Targets {
		record.Processes = append(record.Processes, FaultProcessEvidence{ID: target, Role: "fixture", Identity: target, PID: 500 + processIndex})
	}
	later := testScenarioObservation(cfg, window.BaselineEpoch)
	later.ObservedAt = time.Date(2026, 9, 3, 7, 3, 0, 0, time.UTC).Format(time.RFC3339Nano)
	later.Status.Contracts.FinalizedHead = ChainHead{Number: record.AppliedBlock, Hash: record.AppliedBlockHash}
	later.ObservationHash, _ = canonicalHashHex(later)
	if err := appendObservation(filepath.Join(runDir, "observations.jsonl"), later); err != nil {
		t.Fatal(err)
	}
	if err := attempt.updateAuthenticatedRuntime(runDir, faults); err != nil {
		t.Fatal(err)
	}
	if err := writeScenarioFaultEvidence(runDir, []ScenarioFaultRecord{{ID: "substituted"}}); err != nil {
		t.Fatal(err)
	}
	loaded, err := readScenarioCampaignAttempt(cfg, attempt.stateDir, attempt.roles, campaignTestPlanHash, "release-1.0")
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, current, resumed, err := loaded.loadAuthenticatedRuntimeForensics(runDir)
	if err != nil {
		t.Fatal(err)
	}
	if resumed[activeIndex].Status != "active" || resumed[activeIndex].AppliedBlock != record.AppliedBlock || current.ObservationHash != later.ObservationHash {
		t.Fatalf("mid-fault checkpoint was not resumed exactly: fault=%+v current=%s", resumed[activeIndex], current.ObservationHash)
	}
	pending := cloneScenarioFaultRecords(resumed)
	pending[activeIndex].Status = "pending"
	pending[activeIndex].AppliedBlock = 0
	pending[activeIndex].AppliedBlockHash = ""
	pending[activeIndex].Processes = nil
	if err := loaded.updateAuthenticatedRuntime(runDir, pending); err == nil || !strings.Contains(err.Error(), "moved backward") {
		t.Fatalf("fault ledger rollback error=%v", err)
	}
	definition, err := scenarioDefinitionFor(cfg, "release-1.0")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 3, 9, 10, 0, 0, time.UTC)
	if _, err := runScenarioWithProbe(context.Background(), cfg, attempt.stateDir, definition, nil, scenarioRunOptions{
		Attempt: loaded, ProcessSessionID: loaded.payload.AcceptanceBoundary.ProcessSessionID, Now: func() time.Time { return now },
	}); err == nil || !strings.Contains(err.Error(), "attempt-reentered-after-acceptance") {
		t.Fatalf("mid-fault interrupted attempt reentry error=%v", err)
	}
}

func TestScenarioCampaignAttemptRejectsSameSessionReentryAfterAcceptanceStart(t *testing.T) {
	cfg := testResolvedConfig(t)
	attempt, _, _, _ := bindCampaignAttemptBoundaryFixture(t, cfg, t.TempDir())
	definition, err := scenarioDefinitionFor(cfg, "release-1.0")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC)
	_, err = runScenarioWithProbe(context.Background(), cfg, attempt.stateDir, definition, nil, scenarioRunOptions{
		Attempt: attempt, ProcessSessionID: attempt.payload.AcceptanceBoundary.ProcessSessionID, Now: func() time.Time { return now },
	})
	if err == nil || !strings.Contains(err.Error(), "attempt-reentered-after-acceptance") || attempt.payload.AcceptanceInvalidation != "attempt-reentered-after-acceptance" || attempt.payload.AcceptanceInvalidatedAt != now.Format(time.RFC3339Nano) {
		t.Fatalf("same-session reentry error=%v attempt=%+v", err, attempt.payload)
	}
}

func TestScenarioCampaignAttemptRejectsProcessRestartAfterAcceptanceStart(t *testing.T) {
	cfg := testResolvedConfig(t)
	attempt, _, _, _ := bindCampaignAttemptBoundaryFixture(t, cfg, t.TempDir())
	definition, err := scenarioDefinitionFor(cfg, "release-1.0")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 3, 9, 5, 0, 0, time.UTC)
	faultDriver := &recordingAttemptFaultDriver{}
	_, err = runScenarioWithProbe(context.Background(), cfg, attempt.stateDir, definition, nil, scenarioRunOptions{
		Attempt: attempt, ProcessSessionID: "0x" + strings.Repeat("88", 32), FaultDriver: faultDriver, Now: func() time.Time { return now },
	})
	if err == nil || !strings.Contains(err.Error(), "process-session-changed") || attempt.payload.AcceptanceInvalidation != "process-session-changed" || attempt.payload.AcceptanceInvalidatedAt != now.Format(time.RFC3339Nano) || faultDriver.recoverCalls != 1 {
		t.Fatalf("process restart error=%v recover=%d attempt=%+v", err, faultDriver.recoverCalls, attempt.payload)
	}
	var terminal ScenarioResult
	if err := readJSONFile(filepath.Join(attempt.stateDir, "runs", attempt.payload.RunID, "result.json"), &terminal); err != nil || terminal.Result != "fail" || len(terminal.Assertions) == 0 || terminal.Assertions[0].Passed {
		t.Fatalf("interrupted terminal result=(%+v,%v)", terminal, err)
	}
}

func TestScenarioCampaignAttemptRejectsSignedFaultScheduleSubstitution(t *testing.T) {
	cfg := testResolvedConfig(t)
	attempt, _, _, _ := bindCampaignAttemptBoundaryFixture(t, cfg, t.TempDir())
	attempt.payload.AcceptanceBoundary.Faults[0].Targets[0] = "foreign-process"
	if err := writeScenarioCampaignAttempt(attempt); err != nil {
		t.Fatal(err)
	}
	if _, err := readScenarioCampaignAttempt(cfg, attempt.stateDir, attempt.roles, campaignTestPlanHash, "release-1.0"); err == nil || !strings.Contains(err.Error(), "differs from its exact schedule") {
		t.Fatalf("signed fault schedule substitution error=%v", err)
	}
}

func TestScenarioCampaignAttemptAcceptsOnlyLegacySupervisorImpactExtension(t *testing.T) {
	cfg := testResolvedConfig(t)
	definition, err := scenarioDefinitionFor(cfg, "release-1.0")
	if err != nil {
		t.Fatal(err)
	}
	window := &ScenarioAcceptanceWindow{StartBlock: 100}
	records, err := initializeFaultRecords(window.StartBlock, definition.Faults)
	if err != nil {
		t.Fatal(err)
	}
	changed := false
	for index := range records {
		if records[index].PreAcceptance {
			records[index].ArmedBlock = window.StartBlock - 1
			records[index].ArmedBlockHash = "0x" + strings.Repeat("aa", 32)
		}
		impacts := records[index].Impacts[:0]
		for _, impact := range records[index].Impacts {
			if strings.HasPrefix(impact, "miner-swarm-") {
				changed = true
				continue
			}
			impacts = append(impacts, impact)
		}
		records[index].Impacts = impacts
	}
	if !changed {
		t.Fatal("release schedule has no supervisor impact extension")
	}
	legacy, err := validateScenarioAttemptFaultRecords(definition, window, records)
	if err != nil || !legacy {
		t.Fatalf("legacy supervisor impact schedule accepted=%t error=%v", legacy, err)
	}
	records[0].Targets[0] = "foreign-process"
	if legacy, err := validateScenarioAttemptFaultRecords(definition, window, records); err == nil || legacy {
		t.Fatalf("mutated legacy schedule accepted=%t error=%v", legacy, err)
	}
}

func TestProductionHandoffAuthenticationBitDoesNotSkipLiveByteRecheck(t *testing.T) {
	cfg := testResolvedConfig(t)
	stateDir := t.TempDir()
	_, roles, _ := writeReleaseCampaignFixture(t, cfg, stateDir, 26, 32)
	gate, err := loadReleaseCampaignGate(cfg, stateDir, roles)
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := loadOrCreateScenarioCampaignAttempt(cfg, stateDir, roles, campaignTestPlanHash, "production-soak", gate, time.Date(2026, 9, 3, 8, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if err := attempt.updateProgress(true, false); err != nil {
		t.Fatal(err)
	}
	livePath := filepath.Join(stateDir, "public", "fleet-lifecycle.json")
	live, err := os.ReadFile(livePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(livePath, append(live, ' '), 0o644); err != nil {
		t.Fatal(err)
	}
	lifecycle := &recordingHandoffLifecycle{}
	prepareCalls := 0
	err = beginScenarioCampaignPreparation(context.Background(), "production-soak", attempt.payload.RunID, scenarioRunOptions{
		Attempt: attempt, FleetLifecycle: lifecycle,
		Prepare: func(context.Context) error { prepareCalls++; return nil },
	})
	if err == nil || !strings.Contains(err.Error(), "differs from the exact signed release handoff") || prepareCalls != 0 || len(lifecycle.events) != 0 || attempt.payload.PreparationComplete {
		t.Fatalf("crash-after-auth retry error=%v prepare=%d events=%v attempt=%+v", err, prepareCalls, lifecycle.events, attempt.payload)
	}
}

func TestProductionPreparationBindsExactHandoffBeforeMutationAndSuccessorAfter(t *testing.T) {
	cfg := testResolvedConfig(t)
	stateDir := t.TempDir()
	_, roles, _ := writeReleaseCampaignFixture(t, cfg, stateDir, 26, 32)
	gate, err := loadReleaseCampaignGate(cfg, stateDir, roles)
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := loadOrCreateScenarioCampaignAttempt(cfg, stateDir, roles, campaignTestPlanHash, "production-soak", gate, time.Date(2026, 9, 3, 8, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	lifecycle := &recordingHandoffLifecycle{}
	prepareCalls := 0
	err = beginScenarioCampaignPreparation(context.Background(), "production-soak", attempt.payload.RunID, scenarioRunOptions{
		Attempt: attempt, FleetLifecycle: lifecycle,
		Prepare: func(context.Context) error {
			lifecycle.events = append(lifecycle.events, "prepare")
			prepareCalls++
			return nil
		},
		BeforeFleetLifecycle: func(context.Context) error {
			lifecycle.events = append(lifecycle.events, "prearm")
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(lifecycle.events, []string{"authenticate", "prepare", "prearm", "begin"}) || prepareCalls != 1 || !attempt.payload.HandoffAuthenticated || !attempt.payload.PreparationComplete || lifecycle.handoffHash != gate.LifecycleHandoff.ContentHash || lifecycle.releaseRun != gate.RunID || lifecycle.production != attempt.payload.RunID || bytesSHA256(lifecycle.handoff) != gate.LifecycleHandoff.ContentHash {
		t.Fatalf("first production preparation events=%v prepare=%d lifecycle=%+v attempt=%+v", lifecycle.events, prepareCalls, lifecycle, attempt.payload)
	}
	if err := atomicWrite(filepath.Join(stateDir, "public", "fleet-lifecycle.json"), []byte("production-successor\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lifecycle.events = nil
	if err := beginScenarioCampaignPreparation(context.Background(), "production-soak", attempt.payload.RunID, scenarioRunOptions{
		Attempt: attempt, FleetLifecycle: lifecycle,
		Prepare: func(context.Context) error { prepareCalls++; return nil },
		BeforeFleetLifecycle: func(context.Context) error {
			lifecycle.events = append(lifecycle.events, "prearm")
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(lifecycle.events, []string{"authenticate", "prearm", "begin"}) || prepareCalls != 1 || bytesSHA256(lifecycle.handoff) != gate.LifecycleHandoff.ContentHash {
		t.Fatalf("successor retry events=%v prepare=%d handoff=%s", lifecycle.events, prepareCalls, bytesSHA256(lifecycle.handoff))
	}
}
