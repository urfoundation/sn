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

const campaignTestPlanHash = "0x4242424242424242424242424242424242424242424242424242424242424242"

func campaignTestExecutor() *Executor {
	return &Executor{plan: &SetupPlan{PlanHash: campaignTestPlanHash}}
}

// Build a fully signed scenario directory without running wall-clock epochs so
// gate and handoff tests can force exact boundary and tamper cases.
func writeScenarioCampaignFixture(t *testing.T, cfg *ResolvedConfig, stateDir, name string, startEpoch, endEpoch uint64, startedOverride ...time.Time) (*ScenarioResult, *RoleSecrets, string) {
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
	if name == "production-soak" {
		started = started.Add(24 * time.Hour)
	}
	if len(startedOverride) > 1 {
		t.Fatal("scenario fixture accepts at most one start override")
	}
	if len(startedOverride) == 1 {
		started = startedOverride[0].UTC()
	}
	var prior *ReleaseCampaignGate
	if name == "production-soak" {
		prior, err = loadReleaseCampaignGate(cfg, stateDir, roles)
		if err != nil {
			t.Fatalf("production fixture release gate: %v", err)
		}
	}
	attempt, err := loadOrCreateScenarioCampaignAttempt(cfg, stateDir, roles, campaignTestPlanHash, name, prior, started)
	if err != nil {
		t.Fatalf("campaign fixture attempt: %v", err)
	}
	completed := started.Add(20 * time.Hour)
	adversaries, _, _ := finalSemanticAdversarialTestCampaignForConfig(t, cfg)
	adversaryConfig := cfg.Config.Scenarios.Adversaries
	adversaries.MatrixHash = definition.AdversarialMatrixHash
	adversaries.Seed = adversaryConfig.Seed
	adversaries.MinimumSamplesPerActor = adversaryConfig.MinimumSamplesPerActor
	adversaries.MaximumActorErrorRatePPM = adversaryConfig.MaximumActorErrorRatePPM
	adversaries.MaximumP99Milliseconds = adversaryConfig.MaximumP99LatencyMilliseconds
	adversaries.MaximumAttackControlRatio = adversaryConfig.MaximumAttackControlP95Ratio
	adversaries.MaximumSampleGapMillis = int64(adversaryConfig.SampleIntervalMilliseconds + 2*adversaryConfig.RequestTimeoutMilliseconds)
	adversaries.OperatorRequestCeilingQPS = adversaryConfig.MaximumOperatorRequestsPerSec
	adversaries.RPCRequestCeilingQPS = adversaryConfig.MaximumRPCRequestsPerSec
	adversaries.StartedAt = started.Add(-time.Minute).Format(time.RFC3339Nano)
	adversaries.HappyPathStartedAt = started.Format(time.RFC3339Nano)
	adversaries.HappyPathCompletedAt = completed.Format(time.RFC3339Nano)
	adversaries.StoppedAt = completed.Add(time.Minute).Format(time.RFC3339Nano)
	for i := range adversaries.Actors {
		adversaries.Actors[i].StartedAt = adversaries.StartedAt
		adversaries.Actors[i].StoppedAt = adversaries.StoppedAt
		adversaries.Actors[i].FirstSampleAt = started.Format(time.RFC3339Nano)
		adversaries.Actors[i].LastSampleAt = completed.Format(time.RFC3339Nano)
		adversaries.Actors[i].MaximumSampleGapMillis = 1_000
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
	if name == "production-soak" {
		startBlock = 20_000
	}
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
		record := ScenarioFaultRecord{
			ID: spec.ID, Kind: spec.Kind, Targets: append([]string(nil), spec.Targets...), Impacts: append([]string(nil), spec.Impacts...),
			ValidatorID: spec.ValidatorID, FleetIndex: spec.FleetIndex, FleetIndices: append([]int(nil), spec.FleetIndices...), PreAcceptance: spec.PreAcceptance, PostAcceptanceEvidenceTail: spec.PostAcceptanceEvidenceTail,
			ActivationCondition: spec.ActivationCondition, RestoreCondition: spec.RestoreCondition, MinimumDurationBlocks: spec.MinimumDurationBlocks,
			TriggerBlock: trigger, RestoreBlock: trigger + spec.DurationBlocks, AppliedBlock: trigger,
			AppliedBlockHash: "0x" + strings.Repeat("31", 32), ActivationConditionMet: spec.ActivationCondition != "", ActivationConditionBlock: trigger,
			RestoredBlock:     trigger + spec.DurationBlocks,
			RestoredBlockHash: "0x" + strings.Repeat("32", 32), RestoreConditionMet: spec.RestoreCondition != "", RestoreConditionBlock: trigger + spec.DurationBlocks,
			Processes: processes, RestoredProcesses: restored, Status: "restored",
		}
		if spec.PreAcceptance {
			record.ArmedBlock = window.StartBlock - 1
			record.ArmedBlockHash = "0x" + strings.Repeat("30", 32)
		}
		faults = append(faults, record)
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
	addAssertion(AssertionRecord{ID: "adversary_signed_start_continuity", Passed: true, Message: "fixture", StartedAt: started.Format(time.RFC3339Nano), CompletedAt: completed.Format(time.RFC3339Nano)})
	assertions := make([]AssertionRecord, 0, len(assertionsByID))
	for _, assertion := range assertionsByID {
		assertions = append(assertions, assertion)
	}
	result := &ScenarioResult{
		Schema: "urnetwork-sim-scenario-result-v1", Release: "1.0", RunID: attempt.payload.RunID,
		DeploymentID: cfg.Config.Deployment.DeploymentID, Name: name, ScenarioDefinition: definitionHash,
		ScenarioMatrix: definition.MatrixHash, AdversarialMatrix: definition.AdversarialMatrixHash,
		ConfigHash: cfg.ConfigHash, PolicyHash: cfg.PolicyHash, ChainID: cfg.ChainID, GenesisHash: cfg.Public.Chain.GenesisHash, Netuid: cfg.Netuid,
		StartedAt: started.Format(time.RFC3339Nano), CompletedAt: completed.Format(time.RFC3339Nano),
		CampaignStartHead: ChainHead{Number: baselineHead.Number - 100, Hash: "0x" + strings.Repeat("09", 32)}, CampaignStartEpoch: startEpoch,
		StartHead: baselineHead, EndHead: ChainHead{Number: terminalBlock + 10, Hash: "0x" + strings.Repeat("22", 32)},
		StartEpoch: startEpoch, EndEpoch: endEpoch, AcceptanceWindow: window, Assertions: assertions, Faults: faults, Adversaries: adversaries,
		ValueReconciliation: map[string]string{"captured_rao": "1"}, Result: "pass",
	}
	if name == "release-1.0" {
		lifecycle := FleetLifecycleEvidence{
			Schema: fleetLifecycleEvidenceSchema, DeploymentID: cfg.Config.Deployment.DeploymentID,
			PlanHash: campaignTestPlanHash, RunID: result.RunID, Stage: fleetLifecycleStageReleaseHandoff,
		}
		lifecycleBytes, marshalErr := json.MarshalIndent(lifecycle, "", "  ")
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		lifecycleBytes = append(lifecycleBytes, '\n')
		result.LifecycleHandoff = &ScenarioLifecycleHandoff{
			Schema: scenarioLifecycleHandoffSchema, ReleaseRunID: result.RunID, Stage: fleetLifecycleStageReleaseHandoff,
			File: scenarioLifecycleHandoffFilename, ContentHash: bytesSHA256(lifecycleBytes), SizeBytes: uint64(len(lifecycleBytes)),
		}
		if err := atomicWrite(filepath.Join(stateDir, "public", "fleet-lifecycle.json"), lifecycleBytes, 0o644); err != nil {
			t.Fatal(err)
		}
	} else {
		copyGate := *prior
		result.PriorRelease = &copyGate
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
	if err := attempt.updateProgress(name == "production-soak", true); err != nil {
		t.Fatal(err)
	}
	campaignStartObservation := testScenarioObservation(cfg, startEpoch)
	campaignStartObservation.ObservedAt = started.Format(time.RFC3339Nano)
	campaignStartObservation.Status.Contracts.CurrentEpoch = result.CampaignStartEpoch
	campaignStartObservation.Status.Contracts.FinalizedHead = result.CampaignStartHead
	campaignStartObservation.ObservationHash, _ = canonicalHashHex(campaignStartObservation)
	baselineObservation := testScenarioObservation(cfg, startEpoch)
	baselineObservation.ObservedAt = started.Add(time.Minute).Format(time.RFC3339Nano)
	baselineObservation.Status.Contracts.CurrentEpoch = window.BaselineEpoch
	baselineObservation.Status.Contracts.FinalizedHead = window.BaselineHead
	baselineObservation.ObservationHash, _ = canonicalHashHex(baselineObservation)
	window.BaselineObservationHash = baselineObservation.ObservationHash
	result.AcceptanceWindow.BaselineObservationHash = baselineObservation.ObservationHash
	if err := appendObservation(filepath.Join(runDir, "observations.jsonl"), campaignStartObservation); err != nil {
		t.Fatal(err)
	}
	if err := appendObservation(filepath.Join(runDir, "observations.jsonl"), baselineObservation); err != nil {
		t.Fatal(err)
	}
	initialFaults, err := initializeFaultRecords(window.StartBlock, definition.Faults)
	if err != nil {
		t.Fatal(err)
	}
	for index := range initialFaults {
		if initialFaults[index].PreAcceptance {
			initialFaults[index].ArmedBlock = faults[index].ArmedBlock
			initialFaults[index].ArmedBlockHash = faults[index].ArmedBlockHash
		}
	}
	runningAdversaries := *adversaries
	runningAdversaries.Status = "running"
	runningAdversaries.HappyPathCompletedAt = ""
	runningAdversaries.StoppedAt = ""
	runningAdversaries.StoppedAfterHappyPath = false
	if err := attempt.bindAcceptanceBoundary(runDir, "0x"+strings.Repeat("77", 32), definitionHash, &runningAdversaries, started.Add(2*time.Minute), campaignStartObservation, baselineObservation, window, initialFaults); err != nil {
		t.Fatal(err)
	}
	result.EvidenceHash, err = canonicalScenarioResultHash(result)
	if err != nil {
		t.Fatal(err)
	}
	if result.LifecycleHandoff != nil {
		live, readErr := os.ReadFile(filepath.Join(stateDir, "public", "fleet-lifecycle.json"))
		if readErr != nil {
			t.Fatal(readErr)
		}
		if err := atomicWrite(filepath.Join(runDir, result.LifecycleHandoff.File), live, 0o644); err != nil {
			t.Fatal(err)
		}
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
	writeFinalSemanticCaptureCampaignFixture(t, cfg, runDir, result)
	hashes, err := evidenceFileHashes(runDir, cfg.Config.Topology.Operators)
	if err != nil {
		t.Fatal(err)
	}
	complete, err := signEvidence(cfg, "scenario-complete", result.RunID, scenarioCompletePayload{ResultHash: result.EvidenceHash, Files: hashes, LifecycleHandoff: result.LifecycleHandoff, PriorRelease: result.PriorRelease}, roles.EVM["testnet-owner"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writePublicJSON(filepath.Join(runDir, "complete.json"), complete); err != nil {
		t.Fatal(err)
	}
	return result, roles, runDir
}

func writeFinalSemanticCaptureCampaignFixture(t *testing.T, cfg *ResolvedConfig, runDir string, result *ScenarioResult) {
	t.Helper()
	fixtureLocator := func(kind, name string, data []byte) FinalArtifactLocator {
		t.Helper()
		locator, err := persistFinalCollectedArtifact(runDir, kind, filepath.ToSlash(filepath.Join("final-inputs", "fixture", name)), data)
		if err != nil {
			t.Fatal(err)
		}
		return locator
	}
	jsonFixture := []byte("{\"schema\":\"urnetwork-sim-final-capture-test-fixture-v1\"}\n")
	matrix, matrixBytes, err := loadCanonicalAdversarialMatrix(cfg.Repos.SN, cfg.Config.Scenarios.Adversaries.Matrix)
	if err != nil {
		t.Fatal(err)
	}
	if result.Adversaries == nil || !strings.EqualFold(matrix.Hash, result.AdversarialMatrix) || !strings.EqualFold(matrix.Hash, result.Adversaries.MatrixHash) {
		t.Fatal("capture fixture adversarial matrix does not match its scenario result")
	}
	adversariesBytes, err := json.Marshal(result.Adversaries)
	if err != nil {
		t.Fatal(err)
	}
	bundleEntry := FinalCollectedFileBundleEntry{Path: "fixture.json", ContentHash: bytesSHA256(jsonFixture), SizeBytes: uint64(len(jsonFixture)), Data: jsonFixture}
	bundleBytes, err := json.Marshal(&FinalCollectedFileBundle{Schema: finalCollectedFileBundleSchema, Name: "fixture", Files: []FinalCollectedFileBundleEntry{bundleEntry}})
	if err != nil {
		t.Fatal(err)
	}
	common := map[string]FinalArtifactLocator{
		"policy":                    fixtureLocator("policy", "policy.json", jsonFixture),
		"result":                    fixtureLocator("scenario-result-candidate", "scenario-result.json", jsonFixture),
		"matrix":                    fixtureLocator("adversarial-matrix", "adversarial-matrix.json", matrixBytes),
		"adversaries":               fixtureLocator("scenario-adversaries", "adversaries.json", adversariesBytes),
		"terminal":                  fixtureLocator("scenario-terminal-observation", "terminal-observation.json", jsonFixture),
		"history":                   fixtureLocator("scenario-observation-history", "observation-history.json", jsonFixture),
		"bundle":                    fixtureLocator("closed-input-bundle", "bundle.json", bundleBytes),
		"payout":                    fixtureLocator("payout-artifact", "payout.json", jsonFixture),
		"intent-store":              fixtureLocator("validator-steering-intent-store", "intent-store.json", jsonFixture),
		"intent":                    fixtureLocator("steering-intent", "intent.json", jsonFixture),
		"measurement":               fixtureLocator("validator-release-measurement", "measurement.json", jsonFixture),
		"envelope":                  fixtureLocator("validator-release-measurement-envelope", "envelope.json", jsonFixture),
		"attempts":                  fixtureLocator("validator-attempt-records", "attempts.json", jsonFixture),
		"proofs":                    fixtureLocator("validator-path-proofs", "proofs.json", jsonFixture),
		"closure":                   fixtureLocator("validator-settlement-closure", "closure.json", jsonFixture),
		"prior-chain":               fixtureLocator("prior-live-chain-bundle", "prior-live-chain.json", bundleBytes),
		"prior-semantic-supplement": fixtureLocator("prior-semantic-supplement-envelope", "prior-semantic-verified.json.bin", jsonFixture),
		"prior-semantic-file-1":     fixtureLocator("prior-semantic-file-envelope", "prior-semantic-file-1.json.bin", jsonFixture),
		"prior-semantic-file-2":     fixtureLocator("prior-semantic-file-envelope", "prior-semantic-file-2.json.bin", jsonFixture),
	}
	collected := &FinalSemanticCollectedInputs{
		Schema: finalSemanticCollectedInputsSchema, Phase: result.Name, RunID: result.RunID, ResultHash: result.EvidenceHash,
		Window: *result.AcceptanceWindow, Policy: common["policy"], ScenarioResult: common["result"], AdversarialMatrix: common["matrix"], Adversaries: common["adversaries"], TerminalObservation: common["terminal"],
		ObservationHistory: common["history"], ClosedInputBundles: []FinalArtifactLocator{common["bundle"]},
	}
	if result.Name == "production-soak" {
		stateDir := filepath.Dir(filepath.Dir(runDir))
		roles, roleErr := BuildRoleSecrets(cfg)
		if roleErr != nil {
			t.Fatal(roleErr)
		}
		prior, _, priorErr := loadCompletedScenarioCampaign(cfg, stateDir, roles, "release-1.0")
		if priorErr != nil {
			t.Fatalf("production fixture requires an authenticated release phase: %v", priorErr)
		}
		if prior.LifecycleHandoff == nil {
			t.Fatal("production fixture prior release has no lifecycle handoff")
		}
		priorHandoffBytes, readErr := os.ReadFile(filepath.Join(stateDir, "runs", prior.RunID, prior.LifecycleHandoff.File))
		if readErr != nil {
			t.Fatal(readErr)
		}
		collected.PriorPhase = &FinalCollectedPriorPhaseInputs{
			Phase: "release-1.0", RunID: prior.RunID, ResultHash: prior.EvidenceHash, Window: *prior.AcceptanceWindow,
			ScenarioResult:          fixtureLocator("prior-scenario-result", "prior-result.json.bin", jsonFixture),
			OwnerCompletion:         fixtureLocator("prior-owner-completion-envelope", "prior-complete.json.bin", jsonFixture),
			EvidenceManifest:        fixtureLocator("prior-evidence-manifest-envelope", "prior-manifest.json.bin", jsonFixture),
			LifecycleHandoff:        fixtureLocator("prior-lifecycle-handoff", "prior-lifecycle-handoff.json.bin", priorHandoffBytes),
			CaptureStatus:           fixtureLocator("prior-capture-status", "prior-capture.json.bin", jsonFixture),
			CollectedInputsManifest: fixtureLocator("prior-collected-input-manifest", "prior-inputs.json.bin", jsonFixture),
			LiveChainBundles:        []FinalArtifactLocator{common["prior-chain"]},
			SemanticSupplement:      common["prior-semantic-supplement"],
			SemanticFileEnvelopes:   []FinalArtifactLocator{common["prior-semantic-file-1"], common["prior-semantic-file-2"]},
		}
	}
	lastEpoch := collected.Window.FirstEpoch + collected.Window.EpochCount - 1
	payoutSourceOffset := uint64(1)
	if result.Name == "production-soak" {
		payoutSourceOffset = 2
	}
	if collected.Window.FirstEpoch < payoutSourceOffset {
		t.Fatalf("%s fixture payout source underflows first epoch %d by %d", result.Name, collected.Window.FirstEpoch, payoutSourceOffset)
	}
	for epoch := collected.Window.FirstEpoch - payoutSourceOffset; epoch <= lastEpoch; epoch++ {
		for noID := 1; noID <= cfg.Config.Topology.Operators; noID++ {
			collected.Payouts = append(collected.Payouts, FinalCollectedPayoutArtifact{NoID: uint64(noID), Epoch: epoch, Artifact: common["payout"]})
		}
	}
	payoutEpochs, payoutOK := checkedAdd(collected.Window.EpochCount, payoutSourceOffset)
	wantPayouts, countOK := checkedMul(payoutEpochs, uint64(cfg.Config.Topology.Operators))
	if !payoutOK || !countOK || uint64(len(collected.Payouts)) != wantPayouts {
		t.Fatalf("%s fixture payout coverage=%d, want %d epochs x %d operators", result.Name, len(collected.Payouts), payoutEpochs, cfg.Config.Topology.Operators)
	}
	for validatorID := 1; validatorID <= cfg.Config.Topology.Validators; validatorID++ {
		validator := FinalCollectedValidatorInputs{ValidatorID: uint64(validatorID), PathVPK: "0x" + strings.Repeat(fmt.Sprintf("%02x", validatorID), 32), IntentStore: common["intent-store"]}
		if result.Name == "production-soak" {
			if collected.Window.FirstEpoch == 0 {
				t.Fatal("production fixture dishonest-deposit epoch underflows")
			}
			validator.DishonestDepositIntent = &FinalCollectedValidatorIntent{
				Sequence: 1, SettlementEpoch: collected.Window.FirstEpoch - 1, SubnetEpoch: 900,
				Status: "applied", VectorHash: "0x" + strings.Repeat("cd", 32), Artifact: common["intent"], Measurement: common["measurement"], Envelope: common["envelope"],
			}
			intent := validator.DishonestDepositIntent
			if intent.SettlementEpoch+1 != collected.Window.FirstEpoch || intent.SubnetEpoch != 900 || intent.Status != "applied" || intent.VectorHash != "0x"+strings.Repeat("cd", 32) || intent.Artifact != common["intent"] || intent.Measurement != common["measurement"] || intent.Envelope != common["envelope"] {
				t.Fatalf("production fixture validator %d dishonest-deposit identity is not exact: %+v", validatorID, intent)
			}
		} else if validator.DishonestDepositIntent != nil {
			t.Fatalf("release fixture validator %d unexpectedly has a dishonest-deposit intent", validatorID)
		}
		for epoch := collected.Window.FirstEpoch; epoch <= lastEpoch; epoch++ {
			validator.SettlementClosures = append(validator.SettlementClosures, FinalCollectedSettlementClosure{Epoch: epoch, Boundary: ChainHead{Number: collected.Window.StartBlock + (epoch-collected.Window.FirstEpoch+1)*collected.Window.EpochBlocks - 1, Hash: finalTestHex(byte(epoch))}, Artifact: common["closure"]})
			validator.Intents = append(validator.Intents, FinalCollectedValidatorIntent{
				Sequence: epoch - collected.Window.FirstEpoch + 1, SettlementEpoch: epoch, SubnetEpoch: 900 + epoch - collected.Window.FirstEpoch,
				Status: "applied", VectorHash: "0x" + strings.Repeat("ab", 32), Artifact: common["intent"], Measurement: common["measurement"], Envelope: common["envelope"],
			})
		}
		for noID := 1; noID <= cfg.Config.Topology.Operators; noID++ {
			validator.Attempts = append(validator.Attempts, FinalCollectedValidatorAttempts{NoID: uint64(noID), RecordCount: 1, CompleteCount: 1, Artifact: common["attempts"]})
			validator.PathProofs = append(validator.PathProofs, FinalCollectedValidatorPathProof{NoID: uint64(noID), FirstEpoch: collected.Window.FirstEpoch, LastEpoch: lastEpoch, ProofCount: collected.Window.EpochCount, Artifact: common["proofs"]})
		}
		collected.Validators = append(collected.Validators, validator)
	}
	collected.EvidenceHash, err = finalSemanticCollectedInputsHash(collected)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyFinalSemanticCollectedInputs(cfg, collected); err != nil {
		t.Fatal(err)
	}
	manifestBytes, err := json.MarshalIndent(collected, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	manifestBytes = append(manifestBytes, '\n')
	manifest, err := persistFinalCollectedArtifact(runDir, "final-semantic-input-manifest", "final-inputs/manifest.json", manifestBytes)
	if err != nil {
		t.Fatal(err)
	}
	status := finalSemanticCaptureStatus(result, collected, manifest)
	status.EvidenceHash, err = finalSemanticCaptureStatusHash(status)
	if err != nil {
		t.Fatal(err)
	}
	statusBytes, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := persistFinalCollectedArtifact(runDir, "final-semantic-capture-status", finalSemanticCaptureStatusFilename, append(statusBytes, '\n')); err != nil {
		t.Fatal(err)
	}
}

// Build the release-1.0 fixture used by the production-policy gate tests.
func writeReleaseCampaignFixture(t *testing.T, cfg *ResolvedConfig, stateDir string, startEpoch, endEpoch uint64) (*ScenarioResult, *RoleSecrets, string) {
	t.Helper()
	return writeScenarioCampaignFixture(t, cfg, stateDir, "release-1.0", startEpoch, endEpoch)
}

func TestPublishedCompletionCommitsAreExcludedAndIndependentlyValidated(t *testing.T) {
	cfg := testResolvedConfig(t)
	stateDir := t.TempDir()
	result, roles, runDir := writeReleaseCampaignFixture(t, cfg, stateDir, 26, 32)
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
	archive, err := prepareCampaignEvidenceArchive(cfg, roles, stateDir, result.RunID, result.EvidenceHash, bytesSHA256(bundleBytes), hashes)
	if err != nil {
		t.Fatal(err)
	}
	complete, err := signEvidence(cfg, "scenario-complete", result.RunID, scenarioCompletePayload{
		ResultHash: result.EvidenceHash, Files: hashes, BundlePayloadHash: bytesSHA256(bundleBytes), EvidenceManifestHash: archive.Manifest.ContentHash,
		LifecycleHandoff: result.LifecycleHandoff,
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
	writeReleaseCampaignFixture(t, cfg, stateDir, 26, 32)
	result, roles, runDir := writeScenarioCampaignFixture(t, cfg, stateDir, "production-soak", 32, 36)
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
	writeReleaseCampaignFixture(t, cfg, incompleteDir, 26, 32)
	_, incompleteRoles, _ := writeScenarioCampaignFixture(t, cfg, incompleteDir, "production-soak", 32, 35)
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
	runner := func(_ context.Context, _ *ResolvedConfig, fixtureDir, name string, _ *Journal, _ *Executor, _ *scenarioCampaignAttempt) error {
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
	if err := runReleaseCandidateCampaignWithAnalyzer(context.Background(), cfg, stateDir, journal, campaignTestExecutor(), roles, runner, noOpCampaignPreflight, func(context.Context, *ResolvedConfig, string, string, *RoleSecrets, *ScenarioResult) error {
		return nil
	}); err != nil {
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
	runner := func(_ context.Context, _ *ResolvedConfig, fixtureDir, name string, _ *Journal, _ *Executor, _ *scenarioCampaignAttempt) error {
		names = append(names, name)
		writeScenarioCampaignFixture(t, cfg, fixtureDir, name, 32, 36)
		return nil
	}
	if err := runReleaseCandidateCampaignWithAnalyzer(context.Background(), cfg, stateDir, journal, campaignTestExecutor(), roles, runner, noOpCampaignPreflight, func(context.Context, *ResolvedConfig, string, string, *RoleSecrets, *ScenarioResult) error {
		return nil
	}); err != nil {
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
	runner := func(context.Context, *ResolvedConfig, string, string, *Journal, *Executor, *scenarioCampaignAttempt) error {
		called = true
		return errors.New("completed campaign runner was called")
	}
	if err := runReleaseCandidateCampaignWithAnalyzer(context.Background(), cfg, stateDir, journal, campaignTestExecutor(), roles, runner, noOpCampaignPreflight, func(context.Context, *ResolvedConfig, string, string, *RoleSecrets, *ScenarioResult) error {
		return nil
	}); err != nil || called {
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
	runner := func(_ context.Context, _ *ResolvedConfig, _ string, name string, _ *Journal, _ *Executor, _ *scenarioCampaignAttempt) error {
		names = append(names, name)
		return nil
	}
	err = runReleaseCandidateCampaignWithAnalyzer(context.Background(), cfg, stateDir, journal, campaignTestExecutor(), roles, runner, noOpCampaignPreflight, func(context.Context, *ResolvedConfig, string, string, *RoleSecrets, *ScenarioResult) error {
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "authenticate completed release-1.0 handoff") || strings.Join(names, ",") != "release-1.0" {
		t.Fatalf("unsigned handoff phases=%v error=%v", names, err)
	}
	if _, definitionErr := scenarioDefinitionFor(cfg, releaseCandidateCampaignName); definitionErr == nil {
		t.Fatal("release-candidate orchestration was accepted as an unauthenticated scenario definition")
	}
}
