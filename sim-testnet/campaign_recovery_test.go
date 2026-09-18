// Recovery tests keep a failed acceptance immutable while proving that only
// its owner-authenticated observation boundary can supply runtime state.
package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Builds the smallest complete post-acceptance failure from the real signed
// succession fixture. Every source consumed by recovery remains on disk.
func bindCampaignRecoveryFixture(t *testing.T, fixture *campaignSuccessionFixture) (*scenarioCampaignAttempt, string) {
	t.Helper()
	fixture.cfg.provisionalResume = &provisionalResumeState{Record: &provisionalResumeRecord{PlanHash: fixture.current.PlanHash, Provisional: true}}
	installCampaignSuccessionLogFixture(t, fixture)
	prior, err := fixture.open()
	if err != nil {
		t.Fatal(err)
	}
	if err := prior.updateProgress(false, true); err != nil {
		t.Fatal(err)
	}
	runDir := filepath.Join(fixture.stateDir, "runs", prior.payload.RunID)
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	prefix, err := captureScenarioObservationLogPrefix(filepath.Join(runDir, "observations.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	started, err := time.Parse(time.RFC3339Nano, prior.payload.StartedAt)
	if err != nil {
		t.Fatal(err)
	}
	campaignStart, baseline := testScenarioObservation(fixture.cfg, 6), testScenarioObservation(fixture.cfg, 7)
	campaignStart.ObservedAt = started.Format(time.RFC3339Nano)
	baseline.ObservedAt = started.Add(time.Minute).Format(time.RFC3339Nano)
	campaignStart.ObservationHash, _ = canonicalHashHex(campaignStart)
	baseline.ObservationHash, _ = canonicalHashHex(baseline)
	for _, observation := range []*ScenarioObservation{campaignStart, baseline} {
		if err := appendObservation(filepath.Join(runDir, "observations.jsonl"), observation); err != nil {
			t.Fatal(err)
		}
	}
	definition, err := scenarioDefinitionFor(fixture.cfg, "release-1.0")
	if err != nil {
		t.Fatal(err)
	}
	definitionHash, err := scenarioDefinitionHash(definition)
	if err != nil {
		t.Fatal(err)
	}
	window, err := buildScenarioAcceptanceWindow(fixture.cfg, definition, baseline)
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
	gate, err := loadLiveProcessLogGate(fixture.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	gate.provisionalObservationOnly = true
	boundaryHash, err := bindScenarioProcessLogAcceptance(gate, runDir, started.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	adversary := &AdversaryCampaignEvidence{
		Schema: "urnetwork-adversary-campaign-v1", Release: "1.0", MatrixHash: definition.AdversarialMatrixHash,
		StartedAt: started.Add(-time.Minute).Format(time.RFC3339Nano), HappyPathStartedAt: started.Format(time.RFC3339Nano), Status: "running",
	}
	if err := prior.bindAcceptanceBoundary(runDir, "0x"+strings.Repeat("71", 32), definitionHash, adversary, started.Add(2*time.Minute), campaignStart, baseline, window, faults, prefix, boundaryHash); err != nil {
		t.Fatal(err)
	}
	if err := prior.invalidateAcceptance("execution-exited-before-completion", started.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	finalAcceptance := false
	result := &ScenarioResult{
		Schema: "urnetwork-sim-scenario-result-v1", Release: "1.0", DeploymentID: fixture.cfg.Config.Deployment.DeploymentID,
		RunID: prior.payload.RunID, Name: "release-1.0", StartedAt: started.Format(time.RFC3339Nano), CompletedAt: started.Add(4 * time.Minute).Format(time.RFC3339Nano),
		ConfigHash: fixture.cfg.ConfigHash, PolicyHash: fixture.cfg.PolicyHash, ChainID: fixture.cfg.ChainID, GenesisHash: fixture.cfg.Public.Chain.GenesisHash, Netuid: fixture.cfg.Netuid,
		Provisional: true, FinalAcceptance: &finalAcceptance, Result: "fail", AssertionCount: 1, FailedAssertionCount: 1,
		Assertions: []AssertionRecord{{ID: "precompile_conformance", Passed: false, Message: "deferred startup gate remains unrun"}},
	}
	result.EvidenceHash, err = canonicalScenarioResultHash(result)
	if err != nil {
		t.Fatal(err)
	}
	if err := writePublicJSON(filepath.Join(runDir, "result.json"), result); err != nil {
		t.Fatal(err)
	}
	return prior, runDir
}

// Appends one valid observation after the immutable signed boundary without
// updating that boundary, reproducing the failed predecessor's exit path.
func appendCampaignRecoveryObservationSuffix(t *testing.T, fixture *campaignSuccessionFixture, prior *scenarioCampaignAttempt, runDir string) *ScenarioObservation {
	t.Helper()
	boundary := prior.payload.AcceptanceBoundary
	invalidated, err := time.Parse(time.RFC3339Nano, prior.payload.AcceptanceInvalidatedAt)
	if err != nil {
		t.Fatal(err)
	}
	observation := testScenarioObservation(fixture.cfg, boundary.LastObservationEpoch+1)
	observation.ObservedAt = invalidated.Add(-time.Second).Format(time.RFC3339Nano)
	observation.Status.Contracts.FinalizedHead = ChainHead{Number: boundary.LastObservationHead.Number + 1, Hash: "0x" + strings.Repeat("8b", 32)}
	observation.ObservationHash = ""
	observation.ObservationHash, err = canonicalHashHex(observation)
	if err != nil {
		t.Fatal(err)
	}
	if err := appendObservation(filepath.Join(runDir, "observations.jsonl"), observation); err != nil {
		t.Fatal(err)
	}
	return observation
}

// Builds one complete failed recovery interval for use as the next predecessor.
func bindFailedRecoveryGeneration(t *testing.T, fixture *campaignSuccessionFixture, attempt *scenarioCampaignAttempt, firstEpoch uint64) string {
	t.Helper()
	if err := attempt.updateProgress(false, true); err != nil {
		t.Fatal(err)
	}
	runDir := filepath.Join(fixture.stateDir, "runs", attempt.payload.RunID)
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	prefix, err := captureScenarioObservationLogPrefix(filepath.Join(runDir, "observations.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	started, err := time.Parse(time.RFC3339Nano, attempt.payload.StartedAt)
	if err != nil {
		t.Fatal(err)
	}
	campaignStart, baseline := testScenarioObservation(fixture.cfg, firstEpoch), testScenarioObservation(fixture.cfg, firstEpoch+1)
	campaignStart.ObservedAt = started.Format(time.RFC3339Nano)
	baseline.ObservedAt = started.Add(time.Minute).Format(time.RFC3339Nano)
	campaignStart.ObservationHash, _ = canonicalHashHex(campaignStart)
	baseline.ObservationHash, _ = canonicalHashHex(baseline)
	for _, observation := range []*ScenarioObservation{campaignStart, baseline} {
		if err := appendObservation(filepath.Join(runDir, "observations.jsonl"), observation); err != nil {
			t.Fatal(err)
		}
	}
	definition, err := scenarioDefinitionFor(fixture.cfg, "release-1.0")
	if err != nil {
		t.Fatal(err)
	}
	definitionHash, err := scenarioDefinitionHash(definition)
	if err != nil {
		t.Fatal(err)
	}
	window, err := buildScenarioAcceptanceWindow(fixture.cfg, definition, baseline)
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
	gate, err := loadLiveProcessLogGate(fixture.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	gate.provisionalObservationOnly = true
	if err := gate.BeginAttempt(); err != nil {
		t.Fatal(err)
	}
	boundaryHash, err := bindScenarioProcessLogAcceptance(gate, runDir, started.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	adversary := &AdversaryCampaignEvidence{
		Schema: "urnetwork-adversary-campaign-v1", Release: "1.0", MatrixHash: definition.AdversarialMatrixHash,
		StartedAt: started.Add(-time.Minute).Format(time.RFC3339Nano), HappyPathStartedAt: started.Format(time.RFC3339Nano), Status: "running",
	}
	if err := attempt.bindAcceptanceBoundary(runDir, "0x"+strings.Repeat("72", 32), definitionHash, adversary, started.Add(2*time.Minute), campaignStart, baseline, window, faults, prefix, boundaryHash); err != nil {
		t.Fatal(err)
	}
	if err := attempt.invalidateAcceptance("execution-exited-before-completion", started.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	finalAcceptance := false
	result := &ScenarioResult{
		Schema: "urnetwork-sim-scenario-result-v1", Release: "1.0", DeploymentID: fixture.cfg.Config.Deployment.DeploymentID,
		RunID: attempt.payload.RunID, Name: "release-1.0", StartedAt: started.Format(time.RFC3339Nano), CompletedAt: started.Add(4 * time.Minute).Format(time.RFC3339Nano),
		ConfigHash: fixture.cfg.ConfigHash, PolicyHash: fixture.cfg.PolicyHash, ChainID: fixture.cfg.ChainID, GenesisHash: fixture.cfg.Public.Chain.GenesisHash, Netuid: fixture.cfg.Netuid,
		Provisional: true, FinalAcceptance: &finalAcceptance, Result: "fail", AssertionCount: 1, FailedAssertionCount: 1,
		Assertions: []AssertionRecord{{ID: "native_application", Passed: false, Message: "recovery generation failed after its signed boundary"}},
	}
	result.EvidenceHash, err = canonicalScenarioResultHash(result)
	if err != nil {
		t.Fatal(err)
	}
	if err := writePublicJSON(filepath.Join(runDir, "result.json"), result); err != nil {
		t.Fatal(err)
	}
	return runDir
}

// Builds the R24 failure shape: a signed recovery completed with a terminal
// provisional failure before preparation or an acceptance boundary existed.
func bindPreAcceptanceFailedRecoveryGeneration(t *testing.T, fixture *campaignSuccessionFixture, attempt *scenarioCampaignAttempt) string {
	t.Helper()
	runDir := filepath.Join(fixture.stateDir, "runs", attempt.payload.RunID)
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	started, err := time.Parse(time.RFC3339Nano, attempt.payload.StartedAt)
	if err != nil {
		t.Fatal(err)
	}
	observation := testScenarioObservation(fixture.cfg, 8)
	observation.ObservedAt = started.Add(time.Minute).Format(time.RFC3339Nano)
	observation.ObservationHash = ""
	observation.ObservationHash, err = canonicalHashHex(observation)
	if err != nil {
		t.Fatal(err)
	}
	if err := appendObservation(filepath.Join(runDir, "observations.jsonl"), observation); err != nil {
		t.Fatal(err)
	}
	gate, err := loadLiveProcessLogGate(fixture.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	gate.provisionalObservationOnly = true
	if err := gate.BeginAttempt(); err != nil {
		t.Fatal(err)
	}
	if err := gate.WriteEvidence(runDir); err != nil {
		t.Fatal(err)
	}
	finalAcceptance := false
	result := &ScenarioResult{
		Schema: "urnetwork-sim-scenario-result-v1", Release: "1.0", DeploymentID: fixture.cfg.Config.Deployment.DeploymentID,
		RunID: attempt.payload.RunID, Name: "release-1.0", StartedAt: attempt.payload.StartedAt, CompletedAt: started.Add(4 * time.Minute).Format(time.RFC3339Nano),
		ConfigHash: fixture.cfg.ConfigHash, PolicyHash: fixture.cfg.PolicyHash, ChainID: fixture.cfg.ChainID, GenesisHash: fixture.cfg.Public.Chain.GenesisHash, Netuid: fixture.cfg.Netuid,
		Provisional: true, FinalAcceptance: &finalAcceptance, Result: "fail", AssertionCount: 1, FailedAssertionCount: 1,
		Assertions: []AssertionRecord{{ID: "initial_observation", Passed: false, Message: "prepare scenario: release precompile conformance: action precompile.seed: estimate precompile.seed: VM Exception while processing transaction: revert"}},
	}
	result.EvidenceHash, err = canonicalScenarioResultHash(result)
	if err != nil {
		t.Fatal(err)
	}
	if err := writePublicJSON(filepath.Join(runDir, "result.json"), result); err != nil {
		t.Fatal(err)
	}
	return runDir
}

// Extends the legacy recovery with one explicit generation-two record.
func createSecondCampaignRecovery(t *testing.T, fixture *campaignSuccessionFixture) (*scenarioCampaignAttempt, *scenarioCampaignAttempt, string) {
	t.Helper()
	_, _ = bindCampaignRecoveryFixture(t, fixture)
	first, err := loadOrCreateScenarioCampaignAttempt(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, "release-1.0", nil, fixture.now.Add(time.Hour), fixture.journal)
	if err != nil {
		t.Fatal(err)
	}
	firstRunDir := bindFailedRecoveryGeneration(t, fixture, first, 8)
	second, err := loadOrCreateScenarioCampaignAttempt(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, "release-1.0", nil, fixture.now.Add(2*time.Hour), fixture.journal)
	if err != nil {
		t.Fatal(err)
	}
	return first, second, firstRunDir
}

// Proves exit-path bytes remain evidence instead of accepted runtime state.
func TestScenarioCampaignRecoveryRetainsUnsignedSuffixWithoutAdoptingItsState(t *testing.T) {
	t.Parallel()
	fixture := newCampaignSuccessionFixture(t)
	prior, runDir := bindCampaignRecoveryFixture(t, fixture)
	boundary := prior.payload.AcceptanceBoundary
	unsigned := appendCampaignRecoveryObservationSuffix(t, fixture, prior, runDir)
	path := filepath.Join(runDir, "observations.jsonl")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if uint64(len(before)) <= boundary.ObservationLogBytes {
		t.Fatalf("observation suffix was not appended: bytes=%d boundary=%d", len(before), boundary.ObservationLogBytes)
	}
	if _, _, _, _, _, err := prior.loadAuthenticatedRuntimeForensics(runDir); err == nil || !strings.Contains(err.Error(), "unauthenticated suffix") {
		t.Fatalf("strict forensic loader accepted unsigned suffix: %v", err)
	}

	recovery, err := loadOrCreateScenarioCampaignAttempt(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, "release-1.0", nil, fixture.now.Add(time.Hour), fixture.journal)
	if err != nil {
		t.Fatal(err)
	}
	if recovery.payload.Recovery == nil || recovery.payload.Recovery.PriorObservationLogBytes != uint64(len(before)) || recovery.payload.Recovery.PriorObservationLogSha256 != bytesSHA256(before) {
		t.Fatalf("recovery did not bind complete observation log: %+v", recovery.payload.Recovery)
	}
	history, _, _, current, _, err := prior.loadAuthenticatedRecoveryRuntimeForensics(runDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || current.ObservationHash != boundary.LastObservationHash || current.Status.Contracts.FinalizedHead != boundary.LastObservationHead || current.ObservationHash == unsigned.ObservationHash {
		t.Fatalf("recovery adopted unsigned suffix: history=%d current=%+v unsigned=%+v", len(history), current, unsigned)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatal("recovery rewrote the append-only observation log")
	}
}

// Proves an open acceptance cannot use the failed-predecessor exception.
func TestScenarioCampaignRecoveryRejectsNonInvalidatedSuffixSource(t *testing.T) {
	t.Parallel()
	fixture := newCampaignSuccessionFixture(t)
	prior, runDir := bindCampaignRecoveryFixture(t, fixture)
	appendCampaignRecoveryObservationSuffix(t, fixture, prior, runDir)
	prior.payload.AcceptanceInvalidation = ""
	prior.payload.AcceptanceInvalidatedAt = ""
	if err := writeScenarioCampaignAttempt(prior); err != nil {
		t.Fatal(err)
	}
	if _, err := createScenarioCampaignRecovery(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, fixture.now.Add(time.Hour), fixture.journal); err == nil || !strings.Contains(err.Error(), "no terminal invalidated acceptance") {
		t.Fatalf("non-invalidated recovery source error=%v", err)
	}
}

// A later generation cannot use an open recovery acceptance as its source.
func TestScenarioCampaignRecoveryRejectsNonInvalidatedRecoveryPredecessor(t *testing.T) {
	t.Parallel()
	fixture := newCampaignSuccessionFixture(t)
	_, _ = bindCampaignRecoveryFixture(t, fixture)
	first, err := loadOrCreateScenarioCampaignAttempt(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, "release-1.0", nil, fixture.now.Add(time.Hour), fixture.journal)
	if err != nil {
		t.Fatal(err)
	}
	_ = bindFailedRecoveryGeneration(t, fixture, first, 8)
	first.payload.AcceptanceInvalidation = ""
	first.payload.AcceptanceInvalidatedAt = ""
	if err := writeScenarioCampaignAttempt(first); err != nil {
		t.Fatal(err)
	}
	if _, err := createScenarioCampaignRecovery(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, fixture.now.Add(2*time.Hour), fixture.journal); err == nil || !strings.Contains(err.Error(), "no terminal invalidated acceptance") {
		t.Fatalf("non-invalidated recovery predecessor error=%v", err)
	}
}

// A later generation cannot use a successful recovery result as its source.
func TestScenarioCampaignRecoveryRejectsNonFailedRecoveryPredecessor(t *testing.T) {
	t.Parallel()
	fixture := newCampaignSuccessionFixture(t)
	_, _ = bindCampaignRecoveryFixture(t, fixture)
	first, err := loadOrCreateScenarioCampaignAttempt(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, "release-1.0", nil, fixture.now.Add(time.Hour), fixture.journal)
	if err != nil {
		t.Fatal(err)
	}
	runDir := bindFailedRecoveryGeneration(t, fixture, first, 8)
	path := filepath.Join(runDir, "result.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var result ScenarioResult
	if err := decodeStrictJSONBytes(raw, &result); err != nil {
		t.Fatal(err)
	}
	result.Result = "pass"
	result.FailedAssertionCount = 0
	for index := range result.Assertions {
		result.Assertions[index].Passed = true
	}
	result.EvidenceHash = ""
	result.EvidenceHash, err = canonicalScenarioResultHash(&result)
	if err != nil {
		t.Fatal(err)
	}
	if err := writePublicJSON(path, &result); err != nil {
		t.Fatal(err)
	}
	if _, err := createScenarioCampaignRecovery(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, fixture.now.Add(2*time.Hour), fixture.journal); err == nil || !strings.Contains(err.Error(), "invalidated provisional source") {
		t.Fatalf("non-failed recovery predecessor error=%v", err)
	}
}

// Proves the recovery exception cannot weaken signed-prefix length binding.
func TestScenarioCampaignRecoveryRejectsTruncatedSignedObservationPrefix(t *testing.T) {
	t.Parallel()
	fixture := newCampaignSuccessionFixture(t)
	prior, runDir := bindCampaignRecoveryFixture(t, fixture)
	path := filepath.Join(runDir, "observations.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	boundBytes := prior.payload.AcceptanceBoundary.ObservationLogBytes
	if boundBytes == 0 || uint64(len(data)) < boundBytes {
		t.Fatalf("invalid signed fixture boundary: bytes=%d boundary=%d", len(data), boundBytes)
	}
	if err := os.WriteFile(path, data[:boundBytes-1], 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := createScenarioCampaignRecovery(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, fixture.now.Add(time.Hour), fixture.journal); err == nil || !strings.Contains(err.Error(), "shorter than its owner-authenticated prefix") {
		t.Fatalf("truncated signed observation prefix error=%v", err)
	}
}

// Proves a newly signed hash cannot make malformed observation bytes usable.
func TestScenarioCampaignRecoveryRejectsMalformedSignedObservationPrefix(t *testing.T) {
	t.Parallel()
	fixture := newCampaignSuccessionFixture(t)
	prior, runDir := bindCampaignRecoveryFixture(t, fixture)
	malformed := []byte("{malformed}\n")
	if err := os.WriteFile(filepath.Join(runDir, "observations.jsonl"), malformed, 0o644); err != nil {
		t.Fatal(err)
	}
	prior.payload.AcceptanceBoundary.ObservationLogBytes = uint64(len(malformed))
	prior.payload.AcceptanceBoundary.ObservationLogContentHash = bytesSHA256(malformed)
	if err := writeScenarioCampaignAttempt(prior); err != nil {
		t.Fatal(err)
	}
	if _, err := createScenarioCampaignRecovery(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, fixture.now.Add(time.Hour), fixture.journal); err == nil || !strings.Contains(err.Error(), "scenario observation log record 0") {
		t.Fatalf("malformed signed observation prefix error=%v", err)
	}
}

// Proves the recovery signature freezes every retained suffix byte.
func TestScenarioCampaignRecoveryRejectsPostSignatureSuffixSubstitution(t *testing.T) {
	t.Parallel()
	fixture := newCampaignSuccessionFixture(t)
	prior, runDir := bindCampaignRecoveryFixture(t, fixture)
	appendCampaignRecoveryObservationSuffix(t, fixture, prior, runDir)
	recovery, err := loadOrCreateScenarioCampaignAttempt(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, "release-1.0", nil, fixture.now.Add(time.Hour), fixture.journal)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(runDir, "observations.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	boundBytes := prior.payload.AcceptanceBoundary.ObservationLogBytes
	data[boundBytes] ^= 1
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validateScenarioCampaignRecovery(recovery); err == nil || !strings.Contains(err.Error(), "changed its retained predecessor") {
		t.Fatalf("post-signature suffix substitution error=%v", err)
	}
}

// Proves recovery retains the failed source and preserves the authenticated
// provisional startup waiver used by its succession predecessor.
func TestScenarioCampaignRecoveryPinsFailedAcceptanceAndPreservesStartupWaiver(t *testing.T) {
	t.Parallel()
	fixture := newCampaignSuccessionFixture(t)
	prior, runDir := bindCampaignRecoveryFixture(t, fixture)
	sourcePaths := []string{
		scenarioCampaignSuccessorPath(fixture.stateDir), filepath.Join(runDir, scenarioCampaignStartFilename),
		filepath.Join(runDir, "observations.jsonl"), filepath.Join(runDir, processLogEvidenceFilename), filepath.Join(runDir, "result.json"),
	}
	sourceBytes := make([][]byte, len(sourcePaths))
	for index, path := range sourcePaths {
		var err error
		sourceBytes[index], err = os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
	}
	recovery, err := loadOrCreateScenarioCampaignAttempt(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, "release-1.0", nil, fixture.now.Add(time.Hour), fixture.journal)
	if err != nil {
		t.Fatal(err)
	}
	if recovery.payload.Recovery == nil || recovery.payload.RunID == prior.payload.RunID || recovery.payload.PreparationComplete || releaseStartupGatesRequired(fixture.cfg, prior) || releaseStartupGatesRequired(fixture.cfg, recovery) {
		t.Fatalf("provisional succession or recovery required a strict startup gate: prior=%+v recovery=%+v", prior.payload, recovery.payload)
	}
	for index, path := range sourcePaths {
		current, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(current, sourceBytes[index]) {
			t.Fatalf("recovery changed retained source %s: %v", path, err)
		}
	}
	reopened, err := readScenarioCampaignAttempt(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, "release-1.0")
	if err != nil || reopened.payload.RunID != recovery.payload.RunID || reopened.payload.Recovery == nil {
		t.Fatalf("signed recovery did not reopen exactly: attempt=%+v err=%v", reopened, err)
	}
	strict := *fixture.cfg
	strict.provisionalResume = nil
	if !releaseStartupGatesRequired(&strict, nil) {
		t.Fatal("strict release unexpectedly waived startup gates")
	}
}

// Proves signed observation bytes cannot be replaced after recovery creation.
func TestScenarioCampaignRecoveryRejectsRetainedEvidenceSubstitution(t *testing.T) {
	t.Parallel()
	fixture := newCampaignSuccessionFixture(t)
	_, runDir := bindCampaignRecoveryFixture(t, fixture)
	recovery, err := loadOrCreateScenarioCampaignAttempt(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, "release-1.0", nil, fixture.now.Add(time.Hour), fixture.journal)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(runDir, "observations.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data[0] ^= 1
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validateScenarioCampaignRecovery(recovery); err == nil || !strings.Contains(err.Error(), "owner-authenticated prefix was substituted") {
		t.Fatalf("retained observation substitution error=%v", err)
	}
}

// Later failures extend the chain while generation one keeps its legacy wire shape.
func TestScenarioCampaignRecoveryChainCreatesAndReopensSecondGeneration(t *testing.T) {
	t.Parallel()
	fixture := newCampaignSuccessionFixture(t)
	first, second, _ := createSecondCampaignRecovery(t, fixture)
	if generation, err := scenarioCampaignRecoveryGeneration(first.payload.Recovery); err != nil || generation != 1 || first.payload.Recovery.Generation != 0 || first.payload.Recovery.PriorGeneration != 0 || first.payload.Recovery.PriorAttemptPath != "" {
		t.Fatalf("legacy first recovery changed shape: generation=%d recovery=%+v err=%v", generation, first.payload.Recovery, err)
	}
	if generation, err := scenarioCampaignRecoveryGeneration(second.payload.Recovery); err != nil || generation != 2 {
		t.Fatalf("second recovery generation=%d recovery=%+v err=%v", generation, second.payload.Recovery, err)
	}
	if second.payload.Recovery.PriorGeneration != 1 || second.payload.Recovery.PriorAttemptPath != scenarioCampaignRecoveryRelativePath(1) || second.payload.Recovery.PriorRunID != first.payload.RunID {
		t.Fatalf("second recovery did not bind its exact predecessor: %+v", second.payload.Recovery)
	}
	if releaseStartupGatesRequired(fixture.cfg, first) || releaseStartupGatesRequired(fixture.cfg, second) {
		t.Fatalf("authenticated provisional recovery generation required strict startup gates: first=%+v second=%+v", first.payload, second.payload)
	}
	firstRaw, err := os.ReadFile(scenarioCampaignRecoveryPath(fixture.stateDir))
	if err != nil {
		t.Fatal(err)
	}
	for _, futureField := range []string{`"generation"`, `"prior_generation"`, `"prior_attempt_path"`} {
		if bytes.Contains(firstRaw, []byte(futureField)) {
			t.Fatalf("legacy first recovery serialized future field %s", futureField)
		}
	}
	if second.payload.Recovery.PriorAttemptSha256 != bytesSHA256(firstRaw) {
		t.Fatalf("second recovery predecessor hash=%s want=%s", second.payload.Recovery.PriorAttemptSha256, bytesSHA256(firstRaw))
	}
	if _, err := os.Stat(scenarioCampaignRecoveryGenerationPath(fixture.stateDir, 2)); err != nil {
		t.Fatal(err)
	}
	reopened, err := readScenarioCampaignAttempt(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, "release-1.0")
	if err != nil || reopened.payload.RunID != second.payload.RunID || !reflect.DeepEqual(reopened.payload.Recovery, second.payload.Recovery) {
		t.Fatalf("latest contiguous recovery did not reopen: attempt=%+v err=%v", reopened, err)
	}
}

// The real single-phase entry point advances an invalidated recovery rather than reopening it.
func TestScenarioCampaignRecoverySinglePhaseEntryExtendsLatestGeneration(t *testing.T) {
	t.Parallel()
	fixture := newCampaignSuccessionFixture(t)
	_, second, _ := createSecondCampaignRecovery(t, fixture)
	_ = bindFailedRecoveryGeneration(t, fixture, second, 10)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := runScenarioCampaignAttemptWithTimeout(ctx, fixture.cfg, fixture.stateDir, "release-1.0", fixture.journal, &Executor{plan: fixture.current}, nil, 0)
	latest, readErr := readScenarioCampaignAttempt(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, "release-1.0")
	generation := uint64(0)
	if readErr == nil {
		generation, readErr = scenarioCampaignRecoveryGeneration(latest.payload.Recovery)
	}
	if err == nil || !strings.Contains(err.Error(), "invalid RPC endpoint") || readErr != nil || generation != 3 || latest.payload.Recovery.PriorGeneration != 2 || latest.payload.Recovery.PriorRunID != second.payload.RunID {
		t.Fatalf("single-phase recovery did not extend the latest generation: error=%v read=%v generation=%d attempt=%+v", err, readErr, generation, latest)
	}
}

// The real single-phase entry point retires an R24-shaped pre-acceptance
// failure and starts a fresh generation without rerunning strict startup gates.
func TestScenarioCampaignRecoverySinglePhaseEntryExtendsPreAcceptanceFailure(t *testing.T) {
	t.Parallel()
	fixture := newCampaignSuccessionFixture(t)
	_, _ = bindCampaignRecoveryFixture(t, fixture)
	first, err := loadOrCreateScenarioCampaignAttempt(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, "release-1.0", nil, fixture.now.Add(time.Hour), fixture.journal)
	if err != nil {
		t.Fatal(err)
	}
	runDir := bindPreAcceptanceFailedRecoveryGeneration(t, fixture, first)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	runErr := runScenarioCampaignAttemptWithTimeout(ctx, fixture.cfg, fixture.stateDir, "release-1.0", fixture.journal, &Executor{plan: fixture.current}, nil, 0)
	latest, readErr := readScenarioCampaignAttempt(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, "release-1.0")
	generation := uint64(0)
	if readErr == nil {
		generation, readErr = scenarioCampaignRecoveryGeneration(latest.payload.Recovery)
	}
	if runErr == nil || readErr != nil || generation != 2 || latest.payload.Recovery.PriorRunID != first.payload.RunID || latest.payload.Recovery.PriorCampaignStartSha256 != "" || !validSHA256String(latest.payload.Recovery.PriorJournalSha256) || latest.payload.Recovery.PriorJournalBytes == 0 || latest.payload.PreparationComplete || latest.payload.AcceptanceBoundary != nil || releaseStartupGatesRequired(fixture.cfg, latest) {
		t.Fatalf("single-phase pre-acceptance recovery did not advance safely: run=%v read=%v generation=%d attempt=%+v", runErr, readErr, generation, latest)
	}
	if _, err := os.Lstat(filepath.Join(runDir, scenarioCampaignStartFilename)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pre-acceptance source unexpectedly acquired a campaign-start marker: %v", err)
	}
}

// A signed pre-acceptance preparation checkpoint is reusable because the next
// recovery binds the exact attempt, plan, journal, process log, and result.
func TestScenarioCampaignRecoveryCarriesCompletedPreAcceptancePreparation(t *testing.T) {
	t.Parallel()
	fixture := newCampaignSuccessionFixture(t)
	_, _ = bindCampaignRecoveryFixture(t, fixture)
	first, err := loadOrCreateScenarioCampaignAttempt(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, "release-1.0", nil, fixture.now.Add(time.Hour), fixture.journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.updateProgress(false, true); err != nil {
		t.Fatal(err)
	}
	_ = bindPreAcceptanceFailedRecoveryGeneration(t, fixture, first)
	if first.payload.Recovery.InheritedPreparationSha256 != "" {
		t.Fatal("same-attempt preparation was mislabeled as inherited")
	}
	if err := validateScenarioCampaignRecovery(first); err != nil {
		t.Fatalf("legacy recovery with same-attempt preparation no longer validates: %v", err)
	}
	second, err := loadOrCreateScenarioCampaignAttempt(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, "release-1.0", nil, fixture.now.Add(2*time.Hour), fixture.journal)
	if err != nil {
		t.Fatal(err)
	}
	if !second.payload.PreparationComplete || second.payload.AcceptanceBoundary != nil || second.payload.Recovery.InheritedPreparationSha256 != second.payload.Recovery.PriorAttemptSha256 {
		t.Fatalf("recovery did not carry only the pre-acceptance preparation checkpoint: %+v", second.payload)
	}
	prepareCalls := 0
	if err := beginScenarioCampaignPreparation(t.Context(), "release-1.0", second.payload.RunID, scenarioRunOptions{
		Attempt: second,
		Prepare: func(context.Context) error {
			prepareCalls++
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	if prepareCalls != 0 {
		t.Fatalf("carried preparation checkpoint replayed preparation %d times", prepareCalls)
	}
}

func TestScenarioCampaignRecoveryRejectsUnbackedPreparationCarry(t *testing.T) {
	t.Parallel()
	fixture := newCampaignSuccessionFixture(t)
	_, _ = bindCampaignRecoveryFixture(t, fixture)
	first, err := loadOrCreateScenarioCampaignAttempt(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, "release-1.0", nil, fixture.now.Add(time.Hour), fixture.journal)
	if err != nil {
		t.Fatal(err)
	}
	_ = bindPreAcceptanceFailedRecoveryGeneration(t, fixture, first)
	second, err := loadOrCreateScenarioCampaignAttempt(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, "release-1.0", nil, fixture.now.Add(2*time.Hour), fixture.journal)
	if err != nil {
		t.Fatal(err)
	}
	if second.payload.PreparationComplete {
		t.Fatal("incomplete predecessor unexpectedly carried preparation")
	}
	priorRaw, err := os.ReadFile(scenarioCampaignRecoveryPath(fixture.stateDir))
	if err != nil {
		t.Fatal(err)
	}
	candidate := *second
	candidate.payload = second.payload
	recovery := *second.payload.Recovery
	candidate.payload.Recovery = &recovery
	candidate.payload.Recovery.InheritedPreparationSha256 = candidate.payload.Recovery.PriorAttemptSha256
	if err := validateScenarioCampaignAttemptPayload(fixture.cfg, fixture.current.PlanHash, "release-1.0", &candidate.payload); err == nil {
		t.Fatal("recovery accepted an inherited preparation marker without a completed checkpoint")
	}
	candidate.payload.PreparationComplete = true
	if err := validateScenarioCampaignRecoveryFromPrior(&candidate, first, scenarioCampaignRecoveryRelativePath(1), priorRaw); err == nil || !strings.Contains(err.Error(), "inherited preparation has no completed pre-acceptance predecessor") {
		t.Fatalf("unbacked preparation carry error=%v", err)
	}
	candidate.payload.Recovery.InheritedPreparationSha256 = "sha256:" + strings.Repeat("11", 32)
	if err := validateScenarioCampaignRecoveryFromPrior(&candidate, first, scenarioCampaignRecoveryRelativePath(1), priorRaw); err == nil {
		t.Fatal("recovery accepted an inherited preparation marker for another attempt")
	}
}

// A missing terminal result cannot turn an interrupted pre-acceptance run
// into a new recovery generation.
func TestScenarioCampaignRecoveryRejectsIncompletePreAcceptanceFailure(t *testing.T) {
	t.Parallel()
	fixture := newCampaignSuccessionFixture(t)
	_, _ = bindCampaignRecoveryFixture(t, fixture)
	first, err := loadOrCreateScenarioCampaignAttempt(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, "release-1.0", nil, fixture.now.Add(time.Hour), fixture.journal)
	if err != nil {
		t.Fatal(err)
	}
	if scenarioCampaignAttemptNeedsRecovery(first) {
		t.Fatal("pre-acceptance recovery without a terminal result requested advancement")
	}
	if _, err := createScenarioCampaignRecovery(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, fixture.now.Add(2*time.Hour), fixture.journal); err == nil || !strings.Contains(err.Error(), "no terminal failed result") {
		t.Fatalf("incomplete pre-acceptance recovery error=%v", err)
	}
}

// A successful result is terminal but cannot authorize pre-acceptance recovery.
func TestScenarioCampaignRecoveryRejectsNonfailedPreAcceptanceResult(t *testing.T) {
	t.Parallel()
	fixture := newCampaignSuccessionFixture(t)
	_, _ = bindCampaignRecoveryFixture(t, fixture)
	first, err := loadOrCreateScenarioCampaignAttempt(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, "release-1.0", nil, fixture.now.Add(time.Hour), fixture.journal)
	if err != nil {
		t.Fatal(err)
	}
	runDir := bindPreAcceptanceFailedRecoveryGeneration(t, fixture, first)
	path := filepath.Join(runDir, "result.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var result ScenarioResult
	if err := decodeStrictJSONBytes(raw, &result); err != nil {
		t.Fatal(err)
	}
	result.Result = "pass"
	result.FailedAssertionCount = 0
	result.Assertions[0].Passed = true
	result.EvidenceHash = ""
	result.EvidenceHash, err = canonicalScenarioResultHash(&result)
	if err != nil {
		t.Fatal(err)
	}
	if err := writePublicJSON(path, &result); err != nil {
		t.Fatal(err)
	}
	if _, err := createScenarioCampaignRecovery(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, fixture.now.Add(2*time.Hour), fixture.journal); err == nil || !strings.Contains(err.Error(), "invalidated provisional source") {
		t.Fatalf("nonfailed pre-acceptance recovery error=%v", err)
	}
}

// Every pre-acceptance source byte remains immutable after the next owner
// signs its result, observation, process-log, journal, and plan commitments.
func TestScenarioCampaignRecoveryRejectsPreAcceptanceSourceSubstitution(t *testing.T) {
	t.Parallel()
	fixture := newCampaignSuccessionFixture(t)
	_, _ = bindCampaignRecoveryFixture(t, fixture)
	first, err := loadOrCreateScenarioCampaignAttempt(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, "release-1.0", nil, fixture.now.Add(time.Hour), fixture.journal)
	if err != nil {
		t.Fatal(err)
	}
	runDir := bindPreAcceptanceFailedRecoveryGeneration(t, fixture, first)
	second, err := loadOrCreateScenarioCampaignAttempt(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, "release-1.0", nil, fixture.now.Add(2*time.Hour), fixture.journal)
	if err != nil {
		t.Fatal(err)
	}
	firstRaw, err := os.ReadFile(scenarioCampaignRecoveryPath(fixture.stateDir))
	if err != nil {
		t.Fatal(err)
	}
	candidate := *second
	candidate.payload = second.payload
	recovery := *second.payload.Recovery
	candidate.payload.Recovery = &recovery
	candidate.payload.Recovery.PriorJournalSha256 = "sha256:" + strings.Repeat("11", 32)
	if err := validateScenarioCampaignRecoveryFromPrior(&candidate, first, scenarioCampaignRecoveryRelativePath(1), firstRaw); err == nil {
		t.Fatal("pre-acceptance recovery accepted a substituted journal-prefix commitment")
	}
	path := filepath.Join(runDir, "result.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw[0] ^= 1
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validateScenarioCampaignRecovery(second); err == nil {
		t.Fatal("pre-acceptance recovery accepted a substituted terminal result")
	}
}

// File discovery rejects gaps and aliases rather than skipping an untrusted link.
func TestScenarioCampaignRecoveryChainRejectsGapAndDuplicateGeneration(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		seed func(*testing.T, *campaignSuccessionFixture, []byte)
		want string
	}{
		{
			name: "gap",
			seed: func(t *testing.T, fixture *campaignSuccessionFixture, raw []byte) {
				t.Helper()
				if err := os.WriteFile(scenarioCampaignRecoveryGenerationPath(fixture.stateDir, 3), raw, 0o600); err != nil {
					t.Fatal(err)
				}
			},
			want: "not contiguous",
		},
		{
			name: "duplicate",
			seed: func(t *testing.T, fixture *campaignSuccessionFixture, raw []byte) {
				t.Helper()
				for _, name := range []string{"release-1.0.recovery.2.evidence.json", "release-1.0.recovery.02.evidence.json"} {
					if err := os.WriteFile(filepath.Join(fixture.stateDir, "campaign-attempts", name), raw, 0o600); err != nil {
						t.Fatal(err)
					}
				}
			},
			want: "duplicated",
		},
	} {
		fixture := newCampaignSuccessionFixture(t)
		_, _ = bindCampaignRecoveryFixture(t, fixture)
		if _, err := loadOrCreateScenarioCampaignAttempt(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, "release-1.0", nil, fixture.now.Add(time.Hour), fixture.journal); err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(scenarioCampaignRecoveryPath(fixture.stateDir))
		if err != nil {
			t.Fatal(err)
		}
		test.seed(t, fixture, raw)
		if _, err := readScenarioCampaignAttempt(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, "release-1.0"); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("recovery %s error=%v", test.name, err)
		}
	}
}

// Every explicit lineage field is checked against the exact preceding record.
func TestScenarioCampaignRecoveryChainBindsExactPriorIdentity(t *testing.T) {
	t.Parallel()
	fixture := newCampaignSuccessionFixture(t)
	first, second, _ := createSecondCampaignRecovery(t, fixture)
	priorRaw, err := os.ReadFile(scenarioCampaignRecoveryPath(fixture.stateDir))
	if err != nil {
		t.Fatal(err)
	}
	mutations := []struct {
		name   string
		mutate func(*scenarioCampaignRecovery)
	}{
		{name: "path", mutate: func(value *scenarioCampaignRecovery) {
			value.PriorAttemptPath = "campaign-attempts/foreign.evidence.json"
		}},
		{name: "hash", mutate: func(value *scenarioCampaignRecovery) { value.PriorAttemptSha256 = "sha256:" + strings.Repeat("11", 32) }},
		{name: "run", mutate: func(value *scenarioCampaignRecovery) { value.PriorRunID += "-foreign" }},
		{name: "prior-generation", mutate: func(value *scenarioCampaignRecovery) { value.PriorGeneration = 9 }},
		{name: "generation", mutate: func(value *scenarioCampaignRecovery) { value.Generation = 9 }},
	}
	for _, mutation := range mutations {
		candidate := *second
		candidate.payload = second.payload
		recovery := *second.payload.Recovery
		candidate.payload.Recovery = &recovery
		mutation.mutate(candidate.payload.Recovery)
		if err := validateScenarioCampaignRecoveryFromPrior(&candidate, first, scenarioCampaignRecoveryRelativePath(1), priorRaw); err == nil {
			t.Errorf("mutated recovery predecessor identity %s was accepted", mutation.name)
		}
	}
}

// Invalid generation metadata cannot redirect a write onto generation one.
func TestScenarioCampaignRecoveryWriterRejectsMalformedGenerationBeforePathSelection(t *testing.T) {
	t.Parallel()
	fixture := newCampaignSuccessionFixture(t)
	_, second, _ := createSecondCampaignRecovery(t, fixture)
	legacyPath := scenarioCampaignRecoveryPath(fixture.stateDir)
	legacyBefore, err := os.ReadFile(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	candidate := *second
	candidate.payload = second.payload
	recovery := *second.payload.Recovery
	candidate.payload.Recovery = &recovery
	candidate.payload.Recovery.PriorAttemptPath = "campaign-attempts/foreign.evidence.json"
	if err := writeScenarioCampaignAttempt(&candidate); err == nil || !strings.Contains(err.Error(), "generation or prior path is noncanonical") {
		t.Fatalf("malformed recovery generation write error=%v", err)
	}
	legacyAfter, err := os.ReadFile(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(legacyAfter, legacyBefore) {
		t.Fatal("malformed later recovery overwrote the legacy generation")
	}
}

// Terminal success and descendants permanently close the recovery path.
func TestScenarioCampaignRecoveryRejectsCompletedAndProductionPredecessors(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		path func(*campaignSuccessionFixture, string) string
		want string
	}{
		{name: "complete", path: func(_ *campaignSuccessionFixture, runDir string) string {
			return filepath.Join(runDir, "complete.json")
		}, want: "completed release"},
		{name: "production", path: func(fixture *campaignSuccessionFixture, _ string) string {
			return scenarioCampaignAttemptPath(fixture.stateDir, "production-soak")
		}, want: "production descendant"},
	} {
		fixture := newCampaignSuccessionFixture(t)
		_, runDir := bindCampaignRecoveryFixture(t, fixture)
		if err := os.WriteFile(test.path(fixture, runDir), []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := createScenarioCampaignRecovery(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, fixture.now.Add(time.Hour), fixture.journal); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s predecessor error=%v", test.name, err)
		}
	}
}

// A lifecycle handoff is durable work, not a signed campaign completion. The
// failed interval and its ancestry still require complete authentication.
func TestScenarioCampaignRecoveryPreservesHandoffWithoutClaimingCompletion(t *testing.T) {
	t.Parallel()
	fixture := newCampaignSuccessionFixture(t)
	prior, runDir := bindCampaignRecoveryFixture(t, fixture)
	handoff := &FleetLifecycleEvidence{Schema: fleetLifecycleEvidenceSchema, DeploymentID: fixture.cfg.Config.Deployment.DeploymentID,
		PlanHash: fixture.current.PlanHash, RunID: prior.payload.RunID, Stage: fleetLifecycleStageReleaseHandoff}
	path := filepath.Join(runDir, scenarioLifecycleHandoffFilename)
	if err := writePublicJSON(path, handoff); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	recovery, err := createScenarioCampaignRecovery(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, fixture.now.Add(time.Hour), fixture.journal)
	if err != nil {
		t.Fatal(err)
	}
	if recovery.payload.AcceptanceBoundary != nil || recovery.payload.HandoffAuthenticated || recovery.payload.PriorRelease != nil {
		t.Fatal("retained handoff promoted a failed interval into accepted completion")
	}
	if err := validateScenarioCampaignRecoveryAncestor(recovery, prior.payload.RunID); err != nil {
		t.Fatal(err)
	}
	retained, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(raw, retained) {
		t.Fatalf("recovery changed retained lifecycle work: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(runDir, "complete.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("recovery minted a completion for the failed predecessor: %v", err)
	}
}

// A child starts strictly after both authenticated predecessor terminal times.
func TestScenarioCampaignRecoveryStartsAfterResultAndInvalidation(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		mutate func(*testing.T, *campaignSuccessionFixture, *scenarioCampaignAttempt, string)
	}{
		{
			name: "invalidation",
			mutate: func(t *testing.T, fixture *campaignSuccessionFixture, prior *scenarioCampaignAttempt, _ string) {
				t.Helper()
				prior.payload.AcceptanceInvalidatedAt = fixture.now.Add(2 * time.Hour).Format(time.RFC3339Nano)
				if err := writeScenarioCampaignAttempt(prior); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "result-completion",
			mutate: func(t *testing.T, fixture *campaignSuccessionFixture, _ *scenarioCampaignAttempt, runDir string) {
				t.Helper()
				path := filepath.Join(runDir, "result.json")
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				var result ScenarioResult
				if err := decodeStrictJSONBytes(raw, &result); err != nil {
					t.Fatal(err)
				}
				result.CompletedAt = fixture.now.Add(2 * time.Hour).Format(time.RFC3339Nano)
				result.EvidenceHash = ""
				result.EvidenceHash, err = canonicalScenarioResultHash(&result)
				if err != nil {
					t.Fatal(err)
				}
				if err := writePublicJSON(path, &result); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		fixture := newCampaignSuccessionFixture(t)
		prior, runDir := bindCampaignRecoveryFixture(t, fixture)
		test.mutate(t, fixture, prior, runDir)
		if _, err := createScenarioCampaignRecovery(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, fixture.now.Add(time.Hour), fixture.journal); err == nil || !strings.Contains(err.Error(), "completion and invalidation") {
			t.Errorf("recovery started before predecessor %s: %v", test.name, err)
		}
	}
}

// A signed campaign boundary must name the exact decoded process-log cut.
func TestScenarioCampaignRecoveryCrossBindsProcessLogBoundary(t *testing.T) {
	t.Parallel()
	fixture := newCampaignSuccessionFixture(t)
	prior, runDir := bindCampaignRecoveryFixture(t, fixture)
	wrong := "0x" + strings.Repeat("99", 32)
	startPath := filepath.Join(runDir, scenarioCampaignStartFilename)
	start, _, err := readScenarioCampaignAttemptAt(fixture.cfg, fixture.stateDir, fixture.roles, prior.payload.PlanHash, "release-1.0", startPath)
	if err != nil {
		t.Fatal(err)
	}
	start.payload.AcceptanceBoundary.ProcessLogBoundaryHash = wrong
	owner := fixture.roles.EVM["testnet-owner"]
	envelope, err := signEvidence(fixture.cfg, scenarioCampaignAttemptEvidenceKind, start.payload.RunID, start.payload, owner)
	if err != nil {
		t.Fatal(err)
	}
	if err := writePublicJSON(startPath, envelope); err != nil {
		t.Fatal(err)
	}
	prior.payload.AcceptanceBoundary.ProcessLogBoundaryHash = wrong
	if err := writeScenarioCampaignAttempt(prior); err != nil {
		t.Fatal(err)
	}
	if _, err := createScenarioCampaignRecovery(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, fixture.now.Add(time.Hour), fixture.journal); err == nil || !strings.Contains(err.Error(), "process-log boundary differs") {
		t.Fatalf("mismatched process-log boundary error=%v", err)
	}
}

// Every historical source remains authenticated through later generations.
func TestScenarioCampaignRecoveryChainRejectsPriorSourceSubstitution(t *testing.T) {
	t.Parallel()
	fixture := newCampaignSuccessionFixture(t)
	_, second, firstRunDir := createSecondCampaignRecovery(t, fixture)
	path := filepath.Join(firstRunDir, "result.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw[0] ^= 1
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validateScenarioCampaignRecovery(second); err == nil {
		t.Fatal("second recovery accepted substituted first-generation result")
	}
}
