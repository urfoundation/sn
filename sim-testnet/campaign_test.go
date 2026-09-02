package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Build a fully signed scenario directory without running wall-clock epochs so
// gate and handoff tests can force exact boundary and tamper cases.
func writeScenarioCampaignFixture(t *testing.T, cfg *ResolvedConfig, stateDir, name string, startEpoch, endEpoch uint64) (*ScenarioResult, *RoleSecrets, string) {
	t.Helper()
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := scenarioDefinitionFor(cfg, name)
	if err != nil {
		t.Fatal(err)
	}
	definitionHash, err := scenarioDefinitionHash(definition)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	completed := started.Add(20 * time.Hour)
	adversaries := healthyAdversaryEvidence()
	adversaryConfig := cfg.Config.Scenarios.Adversaries
	adversaries.MatrixHash = definition.AdversarialMatrixHash
	adversaries.Seed = adversaryConfig.Seed
	adversaries.MinimumSamplesPerActor = adversaryConfig.MinimumSamplesPerActor
	adversaries.MaximumActorErrorRatePPM = adversaryConfig.MaximumActorErrorRatePPM
	adversaries.MaximumP99Milliseconds = adversaryConfig.MaximumP99LatencyMilliseconds
	adversaries.MaximumAttackControlRatio = adversaryConfig.MaximumAttackControlP95Ratio
	adversaries.OperatorRequestCeilingQPS = adversaryConfig.MaximumOperatorRequestsPerSec
	adversaries.RPCRequestCeilingQPS = adversaryConfig.MaximumRPCRequestsPerSec
	for i := range adversaries.Actors {
		adversaries.Actors[i].Samples = uint64(adversaryConfig.MinimumSamplesPerActor)
		adversaries.Actors[i].ControlSamples = 1
		adversaries.Actors[i].AttackSamples = uint64(adversaryConfig.MinimumSamplesPerActor - 1)
		adversaries.Actors[i].Successful = uint64(adversaryConfig.MinimumSamplesPerActor)
	}
	for i := range adversaries.Vectors {
		adversaries.Vectors[i].SampleFloor = uint64(adversaryConfig.MinimumSamplesPerActor)
	}
	wantEpochs := uint64(cfg.Config.Scenarios.ShortEpochs)
	epochBlocks := cfg.Policy.Settlement.EpochBlocks
	finalizeOffset := cfg.Policy.Settlement.FinalizeOffsetBlocks
	policyEffectiveEpoch := uint64(1)
	policyEffectiveBlock := uint64(1_000)
	if name == "production-soak" {
		wantEpochs = uint64(cfg.Config.Scenarios.ProductionEpochs)
		epochBlocks = cfg.Policy.ProductionCadence.EpochBlocks
		finalizeOffset = cfg.Policy.ProductionCadence.FinalizeOffsetBlocks
		policyEffectiveEpoch = startEpoch
		policyEffectiveBlock = 9_000
	}
	startBlock := uint64(10_000)
	endBlock := startBlock + wantEpochs*epochBlocks
	terminalBlock := endBlock + finalizeOffset
	baselineHead := ChainHead{Number: startBlock - 10, Hash: "0x" + strings.Repeat("11", 32)}
	window := &ScenarioAcceptanceWindow{
		Schema: "urnetwork-sim-acceptance-window-v1", BaselineHead: baselineHead, BaselineObservationHash: "0x" + strings.Repeat("10", 32),
		BaselineEpoch: startEpoch, FirstEpoch: startEpoch + 1, EpochCount: wantEpochs, EpochBlocks: epochBlocks,
		StartBlock: startBlock, EndBlock: endBlock, FinalizeOffsetBlocks: finalizeOffset, TerminalBlock: terminalBlock,
		PolicyEffectiveEpoch: policyEffectiveEpoch, PolicyEffectiveBlock: policyEffectiveBlock,
	}
	faults := make([]ScenarioFaultRecord, 0, len(definition.Faults))
	for _, spec := range definition.Faults {
		trigger := window.StartBlock + spec.TriggerOffsetBlocks
		processes := make([]FaultProcessEvidence, 0, len(spec.Targets))
		restored := make([]FaultProcessEvidence, 0, len(spec.Targets))
		for j, target := range spec.Targets {
			processes = append(processes, FaultProcessEvidence{ID: target, Role: "fixture", Identity: target, PID: 100 + j})
			restored = append(restored, FaultProcessEvidence{ID: target, Role: "fixture", Identity: target, PID: 200 + j})
		}
		faults = append(faults, ScenarioFaultRecord{
			ID: spec.ID, Kind: spec.Kind, Targets: append([]string(nil), spec.Targets...), Impacts: append([]string(nil), spec.Impacts...),
			TriggerBlock: trigger, RestoreBlock: trigger + spec.DurationBlocks, AppliedBlock: trigger,
			AppliedBlockHash: "0x" + strings.Repeat("31", 32), RestoredBlock: trigger + spec.DurationBlocks,
			RestoredBlockHash: "0x" + strings.Repeat("32", 32), Processes: processes, RestoredProcesses: restored, Status: "restored",
		})
	}
	assertionsByID := map[string]AssertionRecord{}
	addAssertion := func(assertion AssertionRecord) { assertionsByID[assertion.ID] = assertion }
	for _, check := range definition.Checks {
		addAssertion(AssertionRecord{ID: check.ID, Passed: true, Message: "fixture", StartedAt: started.Format(time.RFC3339Nano), CompletedAt: completed.Format(time.RFC3339Nano)})
	}
	for _, assertion := range appendFaultAssertions(nil, faults, started, &ScenarioObservation{}) {
		addAssertion(assertion)
	}
	for _, assertion := range appendAcceptanceFaultAssertion(nil, faults, window, started, &ScenarioObservation{}) {
		addAssertion(assertion)
	}
	for _, assertion := range adversaryAssertions(adversaries, started, "") {
		addAssertion(assertion)
	}
	assertions := make([]AssertionRecord, 0, len(assertionsByID))
	for _, assertion := range assertionsByID {
		assertions = append(assertions, assertion)
	}
	result := &ScenarioResult{
		Schema: "urnetwork-sim-scenario-result-v1", Release: "1.0", RunID: "20260830T120000.000000000Z-" + name,
		DeploymentID: cfg.Config.Deployment.DeploymentID, Name: name, ScenarioDefinition: definitionHash,
		ScenarioMatrix: definition.MatrixHash, AdversarialMatrix: definition.AdversarialMatrixHash,
		ConfigHash: cfg.ConfigHash, PolicyHash: cfg.PolicyHash, ChainID: cfg.ChainID, GenesisHash: cfg.Public.Chain.GenesisHash, Netuid: cfg.Netuid,
		StartedAt: started.Format(time.RFC3339Nano), CompletedAt: completed.Format(time.RFC3339Nano),
		CampaignStartHead: ChainHead{Number: baselineHead.Number - 100, Hash: "0x" + strings.Repeat("09", 32)}, CampaignStartEpoch: startEpoch,
		StartHead: baselineHead, EndHead: ChainHead{Number: terminalBlock + 10, Hash: "0x" + strings.Repeat("22", 32)},
		StartEpoch: startEpoch, EndEpoch: endEpoch, AcceptanceWindow: window, Assertions: assertions, Faults: faults, Adversaries: adversaries,
		ValueReconciliation: map[string]string{"captured_rao": "1"}, Result: "pass",
	}
	attachScenarioAnomalyGate(result, completed, nil, nil)
	result.EvidenceHash, err = canonicalScenarioResultHash(result)
	if err != nil {
		t.Fatal(err)
	}
	runDir := filepath.Join(stateDir, "runs", result.RunID)
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writePublicJSON(filepath.Join(runDir, "result.json"), result); err != nil {
		t.Fatal(err)
	}
	if err := writePublicJSON(filepath.Join(runDir, "assertions.json"), assertionFile{Schema: "urnetwork-sim-assertions-v1", Assertions: result.Assertions}); err != nil {
		t.Fatal(err)
	}
	if err := writePublicJSON(filepath.Join(runDir, "anomalies.json"), result.Anomalies); err != nil {
		t.Fatal(err)
	}
	if err := writePublicJSON(filepath.Join(runDir, "adversaries.json"), result.Adversaries); err != nil {
		t.Fatal(err)
	}
	hashes, err := evidenceFileHashes(runDir, cfg.Config.Topology.Operators)
	if err != nil {
		t.Fatal(err)
	}
	complete, err := signEvidence(cfg, "scenario-complete", result.RunID, scenarioCompletePayload{ResultHash: result.EvidenceHash, Files: hashes}, roles.EVM["testnet-owner"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writePublicJSON(filepath.Join(runDir, "complete.json"), complete); err != nil {
		t.Fatal(err)
	}
	return result, roles, runDir
}

// Build the release-1.0 fixture used by the production-policy gate tests.
func writeReleaseCampaignFixture(t *testing.T, cfg *ResolvedConfig, stateDir string, startEpoch, endEpoch uint64) (*ScenarioResult, *RoleSecrets, string) {
	t.Helper()
	return writeScenarioCampaignFixture(t, cfg, stateDir, "release-1.0", startEpoch, endEpoch)
}

func TestPublishedCompletionCommitsAreExcludedAndIndependentlyValidated(t *testing.T) {
	cfg := testResolvedConfig(t)
	result, roles, runDir := writeReleaseCampaignFixture(t, cfg, t.TempDir(), 26, 32)
	publishResult := *result
	publishResult.PublishedEvidence = nil
	bundlePayload := ScenarioEvidenceBundle{Schema: "urnetwork-sim-scenario-evidence-v1", Result: &publishResult, Observation: &ScenarioObservation{ObservationHash: "fixture"}}
	bundleBytes, err := json.Marshal(bundlePayload)
	if err != nil {
		t.Fatal(err)
	}
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		bundle, err := signEvidence(cfg, "scenario-bundle", result.RunID, bundlePayload, roles.EVM[fmt.Sprintf("operator-%d-artifact", operator)])
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(bundle)
		if err != nil {
			t.Fatal(err)
		}
		if err := atomicWrite(filepath.Join(runDir, fmt.Sprintf("scenario-bundle.operator-%d.evidence.json", operator)), append(encoded, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		result.PublishedEvidence = append(result.PublishedEvidence, PublishedEvidence{ContentHash: bundle.ContentHash})
	}
	if err := writePublicJSON(filepath.Join(runDir, "result.json"), result); err != nil {
		t.Fatal(err)
	}
	hashes, err := evidenceFileHashes(runDir, cfg.Config.Topology.Operators)
	if err != nil {
		t.Fatal(err)
	}
	complete, err := signEvidence(cfg, "scenario-complete", result.RunID, scenarioCompletePayload{
		ResultHash: result.EvidenceHash, Files: hashes, BundlePayloadHash: bytesSHA256(bundleBytes),
	}, roles.EVM["testnet-owner"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writePublicJSON(filepath.Join(runDir, "complete.json"), complete); err != nil {
		t.Fatal(err)
	}
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		commit, err := signEvidence(cfg, "scenario-complete-commit", result.RunID, complete, roles.EVM[fmt.Sprintf("operator-%d-artifact", operator)])
		if err != nil {
			t.Fatal(err)
		}
		if err := writePublicJSON(filepath.Join(runDir, fmt.Sprintf("scenario-complete-commit.operator-%d.evidence.json", operator)), commit); err != nil {
			t.Fatal(err)
		}
	}
	after, err := evidenceFileHashes(runDir, cfg.Config.Topology.Operators)
	if err != nil {
		t.Fatal(err)
	}
	if !stringMapsEqual(hashes, after) {
		t.Fatalf("outer completion commits changed signed evidence hashes: before=%v after=%v", hashes, after)
	}
	if _, err := validateScenarioCampaignComplete(cfg, roles, runDir, result, "release-1.0"); err != nil {
		t.Fatal(err)
	}
}

// Open a private append-only journal owned by one composite campaign test.
func openCampaignTestJournal(t testing.TB, stateDir string) *Journal {
	t.Helper()
	if err := os.Chmod(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := ensurePrivateDir(stateDir); err != nil {
		t.Fatal(err)
	}
	journal, err := OpenJournal(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = journal.Close() })
	return journal
}

func TestReleaseCampaignGateAcceptsFiveEpochsAfterDelayedLaunch(t *testing.T) {
	cfg := testResolvedConfig(t)
	stateDir := t.TempDir()
	result, roles, _ := writeReleaseCampaignFixture(t, cfg, stateDir, 26, 32)
	gate, err := loadReleaseCampaignGate(cfg, stateDir, roles)
	if err != nil {
		t.Fatal(err)
	}
	if gate.RunID != result.RunID || gate.ResultHash != result.EvidenceHash || gate.StartEpoch != 26 || gate.EndEpoch != 32 || gate.CompleteContentHash == "" {
		t.Fatalf("release gate=%+v result=%+v", gate, result)
	}
}

func TestReleaseCampaignGateRejectsPartialBaselineAsOneOfFiveEpochs(t *testing.T) {
	cfg := testResolvedConfig(t)
	stateDir := t.TempDir()
	_, roles, _ := writeReleaseCampaignFixture(t, cfg, stateDir, 26, 31)
	if _, err := loadReleaseCampaignGate(cfg, stateDir, roles); err == nil || !strings.Contains(err.Error(), "precedes accepted boundary") {
		t.Fatalf("partial-baseline gate error=%v", err)
	}
}

func TestReleaseCampaignGateRejectsResultTamperingAfterSignature(t *testing.T) {
	cfg := testResolvedConfig(t)
	stateDir := t.TempDir()
	result, roles, runDir := writeReleaseCampaignFixture(t, cfg, stateDir, 26, 32)
	result.EndEpoch++
	if err := writePublicJSON(filepath.Join(runDir, "result.json"), result); err != nil {
		t.Fatal(err)
	}
	if _, err := loadReleaseCampaignGate(cfg, stateDir, roles); err == nil || !strings.Contains(err.Error(), "result hash") {
		t.Fatalf("tampered release gate error=%v", err)
	}
}

// Independent result validation rejects a terminal boundary that is even one
// block short or arithmetically detached from the accepted epoch range.
func TestReleaseCampaignResultRejectsPrematureTerminalEvidence(t *testing.T) {
	cfg := testResolvedConfig(t)
	result, _, _ := writeReleaseCampaignFixture(t, cfg, t.TempDir(), 26, 32)
	originalTerminal := result.AcceptanceWindow.TerminalBlock
	result.AcceptanceWindow.TerminalBlock--
	if err := validateScenarioCampaignResult(cfg, result, "release-1.0"); err == nil || !strings.Contains(err.Error(), "terminal finalization") {
		t.Fatalf("noncanonical terminal boundary error=%v", err)
	}
	result.AcceptanceWindow.TerminalBlock = originalTerminal
	result.EndHead.Number = originalTerminal - 1
	if err := validateScenarioCampaignResult(cfg, result, "release-1.0"); err == nil || !strings.Contains(err.Error(), "terminal finalization") {
		t.Fatalf("premature terminal head error=%v", err)
	}
}

// Planned offsets are insufficient if actual fault execution or restoration
// escapes the five complete accepted epochs.
func TestReleaseCampaignResultRejectsFaultOutsideAcceptanceWindow(t *testing.T) {
	cfg := testResolvedConfig(t)
	result, _, _ := writeReleaseCampaignFixture(t, cfg, t.TempDir(), 26, 32)
	if len(result.Faults) == 0 {
		t.Fatal("release fixture has no faults")
	}
	result.Faults[0].RestoredBlock = result.AcceptanceWindow.EndBlock + 1
	if err := validateScenarioCampaignResult(cfg, result, "release-1.0"); err == nil || !strings.Contains(err.Error(), "outside the complete-epoch") {
		t.Fatalf("out-of-window fault error=%v", err)
	}
}

// A result cannot invent a boundary baseline after signing a different start
// observation, even when its total epoch count appears sufficient.
func TestReleaseCampaignResultRejectsSubstitutedBaseline(t *testing.T) {
	cfg := testResolvedConfig(t)
	result, _, _ := writeReleaseCampaignFixture(t, cfg, t.TempDir(), 26, 32)
	result.AcceptanceWindow.BaselineEpoch++
	if err := validateScenarioCampaignResult(cfg, result, "release-1.0"); err == nil || !strings.Contains(err.Error(), "baseline") {
		t.Fatalf("substituted baseline error=%v", err)
	}
}

// A production result must span the transition boundary plus all three fully
// observed production epochs before the composite command may adopt it.
func TestProductionCampaignCompletionRequiresTransitionAndThreeEpochs(t *testing.T) {
	cfg := testResolvedConfig(t)
	stateDir := t.TempDir()
	result, roles, runDir := writeScenarioCampaignFixture(t, cfg, stateDir, "production-soak", 31, 35)
	loaded, complete, err := loadCompletedScenarioCampaign(cfg, stateDir, roles, "production-soak")
	if err != nil || loaded.RunID != result.RunID || complete.ContentHash == "" {
		t.Fatalf("production completion loaded=%+v complete=%+v error=%v", loaded, complete, err)
	}
	result.EndEpoch++
	if err := writePublicJSON(filepath.Join(runDir, "result.json"), result); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadCompletedScenarioCampaign(cfg, stateDir, roles, "production-soak"); err == nil || !strings.Contains(err.Error(), "result hash") {
		t.Fatalf("tampered production completion error=%v", err)
	}

	incompleteDir := t.TempDir()
	_, incompleteRoles, _ := writeScenarioCampaignFixture(t, cfg, incompleteDir, "production-soak", 31, 34)
	if _, _, err := loadCompletedScenarioCampaign(cfg, incompleteDir, incompleteRoles, "production-soak"); err == nil || !strings.Contains(err.Error(), "precedes accepted boundary") {
		t.Fatalf("three-transition production completion error=%v", err)
	}
}

// With no completed phase, the composite command must run M2 then M3 in that
// order and authenticate each signed marker before continuing.
func TestReleaseCandidateCampaignRunsBothMissingPhasesInOrder(t *testing.T) {
	cfg := testResolvedConfig(t)
	stateDir := t.TempDir()
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	journal := openCampaignTestJournal(t, stateDir)
	var names []string
	runner := func(_ context.Context, _ *ResolvedConfig, fixtureDir, name string, _ *Journal, _ *Executor) error {
		names = append(names, name)
		switch name {
		case "release-1.0":
			writeScenarioCampaignFixture(t, cfg, fixtureDir, name, 26, 32)
		case "production-soak":
			writeScenarioCampaignFixture(t, cfg, fixtureDir, name, 32, 36)
		default:
			return fmt.Errorf("unexpected campaign %s", name)
		}
		return nil
	}
	if err := runReleaseCandidateCampaign(context.Background(), cfg, stateDir, journal, &Executor{}, roles, runner); err != nil {
		t.Fatal(err)
	}
	if strings.Join(names, ",") != "release-1.0,production-soak" {
		t.Fatalf("campaign order=%v", names)
	}
}

// A valid M2 marker is reusable, but absence of M3 must start exactly one new
// production soak rather than replaying value-bearing M2 preparation.
func TestReleaseCandidateCampaignAdoptsReleaseAndRunsProduction(t *testing.T) {
	cfg := testResolvedConfig(t)
	stateDir := t.TempDir()
	_, roles, _ := writeReleaseCampaignFixture(t, cfg, stateDir, 26, 32)
	journal := openCampaignTestJournal(t, stateDir)
	var names []string
	runner := func(_ context.Context, _ *ResolvedConfig, fixtureDir, name string, _ *Journal, _ *Executor) error {
		names = append(names, name)
		writeScenarioCampaignFixture(t, cfg, fixtureDir, name, 32, 36)
		return nil
	}
	if err := runReleaseCandidateCampaign(context.Background(), cfg, stateDir, journal, &Executor{}, roles, runner); err != nil {
		t.Fatal(err)
	}
	if strings.Join(names, ",") != "production-soak" {
		t.Fatalf("campaign phases=%v", names)
	}
}

// Once both exact signed phases are complete, resume is read-only and cannot
// invoke either live scenario runner again.
func TestReleaseCandidateCampaignSkipsBothAuthenticatedPhases(t *testing.T) {
	cfg := testResolvedConfig(t)
	stateDir := t.TempDir()
	_, roles, _ := writeReleaseCampaignFixture(t, cfg, stateDir, 26, 32)
	writeScenarioCampaignFixture(t, cfg, stateDir, "production-soak", 32, 36)
	journal := openCampaignTestJournal(t, stateDir)
	called := false
	runner := func(context.Context, *ResolvedConfig, string, string, *Journal, *Executor) error {
		called = true
		return errors.New("completed campaign runner was called")
	}
	if err := runReleaseCandidateCampaign(context.Background(), cfg, stateDir, journal, &Executor{}, roles, runner); err != nil || called {
		t.Fatalf("completed campaign called=%t error=%v", called, err)
	}
}

// A zero-error scenario process is not a handoff boundary by itself. Without
// the signed result and exact file hashes, the composite command must stop
// before production policy preparation.
func TestReleaseCandidateCampaignDoesNotCrossUnsignedReleaseResult(t *testing.T) {
	cfg := testResolvedConfig(t)
	stateDir := t.TempDir()
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	journal := openCampaignTestJournal(t, stateDir)
	var names []string
	runner := func(_ context.Context, _ *ResolvedConfig, _ string, name string, _ *Journal, _ *Executor) error {
		names = append(names, name)
		return nil
	}
	err = runReleaseCandidateCampaign(context.Background(), cfg, stateDir, journal, &Executor{}, roles, runner)
	if err == nil || !strings.Contains(err.Error(), "authenticate completed release-1.0 handoff") || strings.Join(names, ",") != "release-1.0" {
		t.Fatalf("unsigned handoff phases=%v error=%v", names, err)
	}
	if _, definitionErr := scenarioDefinitionFor(cfg, releaseCandidateCampaignName); definitionErr == nil {
		t.Fatal("release-candidate orchestration was accepted as an unauthenticated scenario definition")
	}
}
