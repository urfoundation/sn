package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Build a fully signed release directory without running wall-clock epochs so
// gate tests can force exact boundary and tamper cases deterministically.
func writeReleaseCampaignFixture(t *testing.T, cfg *ResolvedConfig, stateDir string, startEpoch, endEpoch uint64) (*ScenarioResult, *RoleSecrets, string) {
	t.Helper()
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
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
	faults := make([]ScenarioFaultRecord, 0, len(definition.Faults))
	for i, spec := range definition.Faults {
		trigger := uint64(1_100 + i*100)
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
	for _, assertion := range adversaryAssertions(adversaries, started, "") {
		addAssertion(assertion)
	}
	assertions := make([]AssertionRecord, 0, len(assertionsByID))
	for _, assertion := range assertionsByID {
		assertions = append(assertions, assertion)
	}
	result := &ScenarioResult{
		Schema: "urnetwork-sim-scenario-result-v1", Release: "1.0", RunID: "20260830T120000.000000000Z-release-1.0",
		DeploymentID: cfg.Config.Deployment.DeploymentID, Name: "release-1.0", ScenarioDefinition: definitionHash,
		ScenarioMatrix: definition.MatrixHash, AdversarialMatrix: definition.AdversarialMatrixHash,
		ConfigHash: cfg.ConfigHash, PolicyHash: cfg.PolicyHash, ChainID: cfg.ChainID, GenesisHash: cfg.Public.Chain.GenesisHash, Netuid: cfg.Netuid,
		StartedAt: started.Format(time.RFC3339Nano), CompletedAt: completed.Format(time.RFC3339Nano),
		StartHead: ChainHead{Number: 1_000, Hash: "0x" + strings.Repeat("11", 32)}, EndHead: ChainHead{Number: 2_000, Hash: "0x" + strings.Repeat("22", 32)},
		StartEpoch: startEpoch, EndEpoch: endEpoch, Assertions: assertions, Faults: faults, Adversaries: adversaries,
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
	hashes, err := evidenceFileHashes(runDir)
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

func TestReleaseCampaignGateAcceptsFiveEpochsAfterDelayedLaunch(t *testing.T) {
	cfg := testResolvedConfig(t)
	stateDir := t.TempDir()
	result, roles, _ := writeReleaseCampaignFixture(t, cfg, stateDir, 26, 31)
	gate, err := loadReleaseCampaignGate(cfg, stateDir, roles)
	if err != nil {
		t.Fatal(err)
	}
	if gate.RunID != result.RunID || gate.ResultHash != result.EvidenceHash || gate.StartEpoch != 26 || gate.EndEpoch != 31 || gate.CompleteContentHash == "" {
		t.Fatalf("release gate=%+v result=%+v", gate, result)
	}
}

func TestReleaseCampaignGateRejectsFourEpochsAtExactBoundary(t *testing.T) {
	cfg := testResolvedConfig(t)
	stateDir := t.TempDir()
	_, roles, _ := writeReleaseCampaignFixture(t, cfg, stateDir, 26, 30)
	if _, err := loadReleaseCampaignGate(cfg, stateDir, roles); err == nil || !strings.Contains(err.Error(), "require 5 live epochs") {
		t.Fatalf("four-epoch gate error=%v", err)
	}
}

func TestReleaseCampaignGateRejectsResultTamperingAfterSignature(t *testing.T) {
	cfg := testResolvedConfig(t)
	stateDir := t.TempDir()
	result, roles, runDir := writeReleaseCampaignFixture(t, cfg, stateDir, 26, 31)
	result.EndEpoch++
	if err := writePublicJSON(filepath.Join(runDir, "result.json"), result); err != nil {
		t.Fatal(err)
	}
	if _, err := loadReleaseCampaignGate(cfg, stateDir, roles); err == nil || !strings.Contains(err.Error(), "result hash") {
		t.Fatalf("tampered release gate error=%v", err)
	}
}
