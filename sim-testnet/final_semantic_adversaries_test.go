package main

// These tests bind the complete concurrent adversarial campaign and canonical
// matrix into the same closed evidence graph as the happy-path report.

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Derives a stopped campaign from the checked-in matrix, preserving every row
// field projected into runtime evidence rather than manufacturing a summary.
func finalSemanticAdversarialTestCampaign(t *testing.T) (*AdversaryCampaignEvidence, *AdversarialMatrix, []byte) {
	t.Helper()
	cfg := testResolvedConfig(t)
	return finalSemanticAdversarialTestCampaignForConfig(t, cfg)
}

// Reuses a caller's resolved release configuration while building the same
// complete stopped campaign fixture.
func finalSemanticAdversarialTestCampaignForConfig(t *testing.T, cfg *ResolvedConfig) (*AdversaryCampaignEvidence, *AdversarialMatrix, []byte) {
	t.Helper()
	matrix, canonical, err := loadCanonicalAdversarialMatrix(cfg.Repos.SN, cfg.Config.Scenarios.Adversaries.Matrix)
	if err != nil {
		t.Fatal(err)
	}
	adversaryConfig := cfg.Config.Scenarios.Adversaries
	rows := make(map[string]AdversarialMatrixRow, len(matrix.Rows))
	for _, row := range matrix.Rows {
		rows[row.ID] = row
	}
	campaign := healthyAdversaryEvidence()
	campaign.MatrixHash = matrix.Hash
	campaign.Seed = adversaryConfig.Seed
	campaign.MinimumSamplesPerActor = adversaryConfig.MinimumSamplesPerActor
	campaign.MaximumSampleGapMillis = int64(adversaryConfig.SampleIntervalMilliseconds + 2*adversaryConfig.RequestTimeoutMilliseconds)
	campaign.MaximumActorErrorRatePPM = adversaryConfig.MaximumActorErrorRatePPM
	campaign.MaximumP99Milliseconds = adversaryConfig.MaximumP99LatencyMilliseconds
	campaign.MaximumAttackControlRatio = adversaryConfig.MaximumAttackControlP95Ratio
	campaign.OperatorRequestCeilingQPS = adversaryConfig.MaximumOperatorRequestsPerSec
	campaign.RPCRequestCeilingQPS = adversaryConfig.MaximumRPCRequestsPerSec
	for index := range campaign.Actors {
		actor := &campaign.Actors[index]
		actor.Samples = uint64(campaign.MinimumSamplesPerActor)
		actor.ControlSamples = 1
		actor.AttackSamples = uint64(campaign.MinimumSamplesPerActor - 1)
		actor.Successful = uint64(campaign.MinimumSamplesPerActor)
	}
	for index := range campaign.Vectors {
		row, ok := rows[campaign.Vectors[index].ID]
		if !ok {
			t.Fatalf("campaign vector %s has no checked-in matrix row", campaign.Vectors[index].ID)
		}
		vector := &campaign.Vectors[index]
		vector.Class = row.Class
		vector.ExecutionMode = row.ExecutionMode
		vector.ConcurrentCoverage = adversaryCoverageForMode(row.ExecutionMode)
		vector.ActorIDs = append([]string(nil), row.ActorIDs...)
		vector.LocalTests = append([]string(nil), row.LocalTests...)
		vector.RequiredMetrics = append([]string(nil), row.Metrics...)
		vector.MeasuredMetrics = append([]string(nil), row.Metrics...)
		vector.Oracle = row.Oracle
		vector.SampleFloor = uint64(campaign.MinimumSamplesPerActor)
		vector.Errors = 0
		vector.MaximumP99LatencyMilliseconds = 10
	}
	actorIndex := make(map[string]int, len(campaign.Actors))
	for index := range campaign.Actors {
		actor := &campaign.Actors[index]
		actorIndex[actor.ID] = index
		actor.Metrics = make(map[string]AdversaryMetricEvidence)
	}
	for _, row := range matrix.Rows {
		for _, actorID := range row.ActorIDs {
			index, ok := actorIndex[actorID]
			if !ok {
				t.Fatalf("matrix actor %s is absent from campaign", actorID)
			}
			for _, metricName := range row.Metrics {
				metric := AdversaryMetricEvidence{Samples: uint64(campaign.MinimumSamplesPerActor), Minimum: 1, Maximum: 1, Last: 1}
				if actorID == "custody-boundary-emulation" && metricName == "live_invalid_merkle_proof_rejections" {
					metric = AdversaryMetricEvidence{Samples: 1, Minimum: 2, Maximum: 2, Last: 2}
				}
				if actorID == "custody-boundary-emulation" && metricName == "live_merkle_state_mutations" {
					metric = AdversaryMetricEvidence{Samples: 1, Minimum: 0, Maximum: 0, Last: 0}
				}
				campaign.Actors[index].Metrics[metricName] = metric
			}
		}
	}
	vectorIDsByActor := vectorIDsByActor(matrix)
	for index := range campaign.Actors {
		campaign.Actors[index].VectorIDs = append([]string(nil), vectorIDsByActor[campaign.Actors[index].ID]...)
	}
	return campaign, matrix, canonical
}

// Exact matrix counts, locators, bytes, and row projections must all survive
// report summarization and offline replay.
func TestFinalSemanticAdversarialArtifactBindsExactMatrixSummary(t *testing.T) {
	campaign, matrix, matrixData := finalSemanticAdversarialTestCampaign(t)
	summary, err := summarizeFinalAdversarialCampaign(campaign, matrix)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(campaign)
	if err != nil {
		t.Fatal(err)
	}
	summary.MatrixArtifact = FinalArtifactLocator{Kind: "adversarial-matrix", URI: "final-inputs/adversarial-matrix.json", ContentHash: bytesSHA256(matrixData), SizeBytes: uint64(len(matrixData))}
	summary.Artifact = FinalArtifactLocator{Kind: "scenario-adversaries", URI: "final-inputs/adversaries.json", ContentHash: bytesSHA256(data), SizeBytes: uint64(len(data))}
	if err := verifyFinalAdversarialCampaign(summary); err != nil {
		t.Fatalf("valid adversarial summary rejected: %v", err)
	}
	decodedMatrix, err := verifyFinalAdversarialMatrixArtifact(summary, matrixData)
	if err != nil {
		t.Fatalf("valid adversarial matrix rejected: %v", err)
	}
	if err := verifyFinalAdversarialCampaignArtifact(summary, data, decodedMatrix); err != nil {
		t.Fatalf("valid adversaries.json rejected: %v", err)
	}

	truncated := *campaign
	truncated.Vectors = append([]AdversaryVectorEvidence(nil), campaign.Vectors[:len(campaign.Vectors)-1]...)
	truncatedData, err := json.Marshal(&truncated)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyFinalAdversarialCampaignArtifact(summary, truncatedData, decodedMatrix); err == nil || !strings.Contains(err.Error(), "vectors=60, want 61") {
		t.Fatalf("truncated exact matrix was accepted: %v", err)
	}
}

// Every projected row, actor, metric, and derived status field is part of the
// authenticated campaign rather than a producer-supplied assertion.
func TestFinalSemanticAdversarialCampaignRejectsProjectedAndDerivedMutations(t *testing.T) {
	campaign, matrix, _ := finalSemanticAdversarialTestCampaign(t)
	mutations := []struct {
		name   string
		mutate func(*AdversaryCampaignEvidence)
		want   string
	}{
		{name: "class", mutate: func(value *AdversaryCampaignEvidence) { value.Vectors[0].Class = "tampered" }, want: "class differs"},
		{name: "execution_mode", mutate: func(value *AdversaryCampaignEvidence) { value.Vectors[0].ExecutionMode = "invalid" }, want: "execution coverage"},
		{name: "actor_ids", mutate: func(value *AdversaryCampaignEvidence) { value.Vectors[0].ActorIDs[0] = "tampered" }, want: "actor_ids differs"},
		{name: "local_tests", mutate: func(value *AdversaryCampaignEvidence) { value.Vectors[0].LocalTests[0] = "tampered" }, want: "local_tests differs"},
		{name: "required_metrics", mutate: func(value *AdversaryCampaignEvidence) { value.Vectors[0].RequiredMetrics[0] = "tampered" }, want: "required_metrics differs"},
		{name: "oracle", mutate: func(value *AdversaryCampaignEvidence) { value.Vectors[0].Oracle = "tampered" }, want: "oracle differs"},
		{name: "measured_metrics", mutate: func(value *AdversaryCampaignEvidence) { value.Vectors[0].MeasuredMetrics[0] = "tampered" }, want: "measured_metrics differs"},
		{name: "missing_measured_metric", mutate: func(value *AdversaryCampaignEvidence) {
			value.Vectors[0].MeasuredMetrics = append([]string(nil), value.Vectors[0].MeasuredMetrics[:len(value.Vectors[0].MeasuredMetrics)-1]...)
		}, want: "measured_metrics differs"},
		{name: "sample_floor", mutate: func(value *AdversaryCampaignEvidence) { value.Vectors[0].SampleFloor++ }, want: "sample_floor differs"},
		{name: "errors", mutate: func(value *AdversaryCampaignEvidence) { value.Vectors[0].Errors++ }, want: "errors differs"},
		{name: "maximum_p99", mutate: func(value *AdversaryCampaignEvidence) { value.Vectors[0].MaximumP99LatencyMilliseconds++ }, want: "maximum_p99_latency_milliseconds differs"},
		{name: "actor_vector_ids", mutate: func(value *AdversaryCampaignEvidence) {
			value.Actors[0].VectorIDs = append(value.Actors[0].VectorIDs, "tampered")
		}, want: "vector_ids differs"},
		{name: "actor_status", mutate: func(value *AdversaryCampaignEvidence) { value.Actors[0].Status = "running" }, want: "not a stopped healthy"},
		{name: "actor_sample_gap", mutate: func(value *AdversaryCampaignEvidence) {
			value.Actors[0].MaximumSampleGapMillis = value.MaximumSampleGapMillis + 1
		}, want: "sampling gap"},
		{name: "actor_error_rate", mutate: func(value *AdversaryCampaignEvidence) { value.Actors[0].ErrorRatePPM++ }, want: "error_rate_ppm differs"},
		{name: "actor_negative_latency", mutate: func(value *AdversaryCampaignEvidence) { value.Actors[0].P99LatencyMilliseconds = -1 }, want: "negative latency"},
		{name: "actor_attack_control_ratio", mutate: func(value *AdversaryCampaignEvidence) { value.Actors[0].AttackControlP95RatioPPM++ }, want: "attack_control_p95_ratio_ppm differs"},
		{name: "actor_metric_bounds", mutate: func(value *AdversaryCampaignEvidence) {
			for metricName, metric := range value.Actors[0].Metrics {
				metric.Last = metric.Maximum + 1
				value.Actors[0].Metrics[metricName] = metric
				return
			}
		}, want: "metric"},
		{name: "actor_metric_without_samples", mutate: func(value *AdversaryCampaignEvidence) {
			for metricName, metric := range value.Actors[0].Metrics {
				metric.Samples = 0
				value.Actors[0].Metrics[metricName] = metric
				return
			}
		}, want: "values without samples"},
	}
	for _, mutation := range mutations {
		mutated := cloneFinalSemanticAdversarialCampaign(t, campaign)
		mutation.mutate(mutated)
		if _, err := summarizeFinalAdversarialCampaign(mutated, matrix); err == nil || !strings.Contains(err.Error(), mutation.want) {
			t.Errorf("%s mutation accepted or failed unclearly: %v", mutation.name, err)
		}
	}
}

// Missing bytes and one-field mutations fail both canonical matrix and raw
// campaign artifact verification.
func TestFinalSemanticAdversarialArtifactsRejectMissingAndTamperedInputs(t *testing.T) {
	campaign, matrix, matrixData := finalSemanticAdversarialTestCampaign(t)
	summary, err := summarizeFinalAdversarialCampaign(campaign, matrix)
	if err != nil {
		t.Fatal(err)
	}
	campaignData, err := json.Marshal(campaign)
	if err != nil {
		t.Fatal(err)
	}
	summary.MatrixArtifact = FinalArtifactLocator{Kind: "adversarial-matrix", URI: "final-inputs/adversarial-matrix.json", ContentHash: bytesSHA256(matrixData), SizeBytes: uint64(len(matrixData))}
	summary.Artifact = FinalArtifactLocator{Kind: "scenario-adversaries", URI: "final-inputs/adversaries.json", ContentHash: bytesSHA256(campaignData), SizeBytes: uint64(len(campaignData))}
	missingMatrix := summary
	missingMatrix.MatrixArtifact = FinalArtifactLocator{}
	if err := verifyFinalAdversarialCampaign(missingMatrix); err == nil || !strings.Contains(err.Error(), "adversarial matrix") {
		t.Fatalf("missing matrix locator accepted: %v", err)
	}
	if _, err := verifyFinalAdversarialMatrixArtifact(summary, append(append([]byte(nil), matrixData...), '\n')); err == nil || !strings.Contains(err.Error(), "not canonical") {
		t.Fatalf("tampered matrix bytes accepted: %v", err)
	}
	var alteredMatrix map[string]any
	decoder := json.NewDecoder(bytes.NewReader(matrixData))
	decoder.UseNumber()
	if err := decoder.Decode(&alteredMatrix); err != nil {
		t.Fatal(err)
	}
	rows, ok := alteredMatrix["rows"].([]any)
	if !ok || len(rows) == 0 {
		t.Fatal("canonical matrix rows are unavailable")
	}
	firstRow, ok := rows[0].(map[string]any)
	if !ok {
		t.Fatal("canonical matrix first row is unavailable")
	}
	firstRow["preconditions"] = "tampered"
	alteredMatrixData, err := json.Marshal(alteredMatrix)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifyFinalAdversarialMatrixArtifact(summary, alteredMatrixData); err == nil || !strings.Contains(err.Error(), "hash differs") {
		t.Fatalf("one-field matrix mutation accepted: %v", err)
	}
	if err := verifyFinalAdversarialCampaignArtifact(summary, nil, matrix); err == nil || !strings.Contains(err.Error(), "decode adversarial campaign artifact") {
		t.Fatalf("missing campaign bytes accepted: %v", err)
	}
	tamperedCampaign := cloneFinalSemanticAdversarialCampaign(t, campaign)
	tamperedCampaign.Vectors[0].Oracle = "tampered"
	tamperedCampaignData, err := json.Marshal(tamperedCampaign)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyFinalAdversarialCampaignArtifact(summary, tamperedCampaignData, matrix); err == nil || !strings.Contains(err.Error(), "oracle differs") {
		t.Fatalf("tampered campaign bytes accepted: %v", err)
	}
}

// Produces a deep copy so mutation cases cannot share nested actor state.
func cloneFinalSemanticAdversarialCampaign(t *testing.T, value *AdversaryCampaignEvidence) *AdversaryCampaignEvidence {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var cloned AdversaryCampaignEvidence
	if err := json.Unmarshal(data, &cloned); err != nil {
		t.Fatal(err)
	}
	return &cloned
}

// Raw stopped-campaign bytes are copied into the closed input graph and must
// remain equal to the signed scenario result.
func TestCaptureFinalSemanticAdversariesBindsRawArtifact(t *testing.T) {
	runDir := t.TempDir()
	campaign, matrix, _ := finalSemanticAdversarialTestCampaign(t)
	result := &ScenarioResult{AdversarialMatrix: campaign.MatrixHash, Adversaries: campaign}
	data, err := json.MarshalIndent(campaign, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if err := atomicWrite(filepath.Join(runDir, "adversaries.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	locator, err := captureFinalSemanticAdversaries(runDir, result, matrix)
	if err != nil {
		t.Fatal(err)
	}
	if locator.Kind != "scenario-adversaries" || locator.URI != "final-inputs/adversaries.json" || locator.ContentHash != bytesSHA256(data) || locator.SizeBytes != uint64(len(data)) {
		t.Fatalf("captured adversarial locator=%+v", locator)
	}
	captured, err := os.ReadFile(filepath.Join(runDir, filepath.FromSlash(locator.URI)))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(captured, data) {
		t.Fatal("captured adversaries.json bytes differ from the authenticated raw artifact")
	}

	mutated := *campaign
	mutated.Vectors = append([]AdversaryVectorEvidence(nil), campaign.Vectors...)
	mutated.Vectors[0].Status = "fail"
	mutatedData, err := json.Marshal(&mutated)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(runDir, "adversaries.json"), mutatedData, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := captureFinalSemanticAdversaries(runDir, result, matrix); err == nil || !strings.Contains(err.Error(), "differs from the signed scenario result") {
		t.Fatalf("mismatched adversaries.json was accepted: %v", err)
	}
}

// The canonical checked-in matrix is frozen byte-for-byte before semantic
// analysis and cannot be substituted by a result-only hash.
func TestCaptureFinalSemanticAdversarialMatrixFreezesCanonicalSource(t *testing.T) {
	runDir := t.TempDir()
	cfg := testResolvedConfig(t)
	campaign, matrix, matrixData := finalSemanticAdversarialTestCampaignForConfig(t, cfg)
	result := &ScenarioResult{AdversarialMatrix: campaign.MatrixHash, Adversaries: campaign}
	locator, captured, err := captureFinalSemanticAdversarialMatrix(cfg, runDir, result)
	if err != nil {
		t.Fatal(err)
	}
	if captured == nil || !strings.EqualFold(captured.Hash, matrix.Hash) || locator.Kind != "adversarial-matrix" || locator.URI != "final-inputs/adversarial-matrix.json" || locator.ContentHash != bytesSHA256(matrixData) || locator.SizeBytes != uint64(len(matrixData)) {
		t.Fatalf("captured matrix locator=%+v matrix=%+v", locator, captured)
	}
	data, err := os.ReadFile(filepath.Join(runDir, filepath.FromSlash(locator.URI)))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, matrixData) {
		t.Fatal("captured matrix differs from canonical frozen source bytes")
	}
	mutated := *result
	mutated.AdversarialMatrix = finalTestHex(7)
	if _, _, err := captureFinalSemanticAdversarialMatrix(cfg, runDir, &mutated); err == nil || !strings.Contains(err.Error(), "differs from the signed scenario result") {
		t.Fatalf("mismatched matrix result accepted: %v", err)
	}
}

// Source replay reconstructs adversarial evidence from content-addressed
// closed inputs even when no loose derived report exists yet.
func TestFinalSemanticAdversarialClosedGraphReplaysSourceWithoutLooseFinal(t *testing.T) {
	t.Parallel()
	source, artifacts := finalSemanticFixture(t)
	cfg := testResolvedConfig(t)
	policy, err := finalSemanticFixturePolicy(&source, artifacts)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Config.Deployment.DeploymentID = source.DeploymentID
	cfg.Config.Scenarios.ShortEpochs = int(source.Window.EpochCount)
	cfg.Config.Topology.Operators = source.ExpectedOperators
	cfg.Config.Topology.Validators = source.ExpectedValidators
	cfg.Config.Topology.Miners = source.ExpectedMiners
	cfg.ConfigHash = source.ConfigHash
	cfg.Policy = policy
	cfg.PolicyHash = source.PolicyHash
	cfg.ChainID = source.ChainID
	cfg.Netuid = source.Netuid
	cfg.Public.Chain.GenesisHash = source.GenesisHash
	campaign, matrix, matrixData := finalSemanticAdversarialTestCampaignForConfig(t, cfg)
	result := &ScenarioResult{
		RunID: "closed-adversarial-replay", Name: "release-1.0", EvidenceHash: finalTestHex(1),
		AdversarialMatrix: matrix.Hash, Adversaries: campaign,
	}
	resultData, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	terminal := ScenarioObservation{ObservationHash: finalTestHex(2), PublicIdentityCount: 1004}
	history := []*ScenarioObservation{{ObservationHash: finalTestHex(3)}, &terminal}
	terminalData, err := json.Marshal(terminal)
	if err != nil {
		t.Fatal(err)
	}
	historyData, err := json.Marshal(history)
	if err != nil {
		t.Fatal(err)
	}
	stateDir := t.TempDir()
	runDir := filepath.Join(stateDir, "runs", result.RunID)
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	collected := finalSemanticSupplementCollectedFixture(t, cfg, source, result, resultData)
	campaignData := finalSemanticSupplementJSON(t, campaign)
	collected.ScenarioResult = FinalArtifactLocator{Kind: "scenario-result-candidate", URI: "result.json", ContentHash: bytesSHA256(resultData), SizeBytes: uint64(len(resultData))}
	collected.AdversarialMatrix = FinalArtifactLocator{Kind: "adversarial-matrix", URI: "final-inputs/adversarial-matrix.json", ContentHash: bytesSHA256(matrixData), SizeBytes: uint64(len(matrixData))}
	collected.Adversaries = FinalArtifactLocator{Kind: "scenario-adversaries", URI: "final-inputs/adversaries.json", ContentHash: bytesSHA256(campaignData), SizeBytes: uint64(len(campaignData))}
	collected.TerminalObservation = FinalArtifactLocator{Kind: "scenario-terminal-observation", URI: "final-inputs/terminal-observation.json", ContentHash: bytesSHA256(terminalData), SizeBytes: uint64(len(terminalData))}
	collected.ObservationHistory = FinalArtifactLocator{Kind: "scenario-observation-history", URI: "final-inputs/observation-history.json", ContentHash: bytesSHA256(historyData), SizeBytes: uint64(len(historyData))}
	collected.EvidenceHash, err = finalSemanticCollectedInputsHash(collected)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyFinalSemanticCollectedInputs(cfg, collected); err != nil {
		t.Fatal(err)
	}
	collectedData := finalSemanticSupplementJSON(t, collected)
	if err := atomicWrite(filepath.Join(runDir, "final-inputs", "manifest.json"), collectedData, 0o644); err != nil {
		t.Fatal(err)
	}
	references := map[string]campaignArtifactReference{}
	if err := collectCampaignArtifactReferences(collectedData, references, 0); err != nil {
		t.Fatal(err)
	}
	for name, reference := range references {
		data, ok := artifacts[name]
		switch name {
		case collected.ScenarioResult.URI:
			data, ok = resultData, true
		case collected.AdversarialMatrix.URI:
			data, ok = matrixData, true
		case collected.Adversaries.URI:
			data, ok = campaignData, true
		case collected.TerminalObservation.URI:
			data, ok = terminalData, true
		case collected.ObservationHistory.URI:
			data, ok = historyData, true
		case collected.ClosedInputBundles[0].URI:
			data, ok = finalSemanticSupplementFixtureClosedBundleBytes(t), true
		}
		if !ok {
			data = finalSemanticSupplementFixtureArtifactBytes(t, reference.Kind, name)
		}
		if uint64(len(data)) != reference.Size || !strings.EqualFold(bytesSHA256(data), reference.ContentHash) {
			t.Fatalf("closed replay artifact %q differs from its locator", name)
		}
		if err := atomicWrite(filepath.Join(runDir, filepath.FromSlash(name)), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{finalSemanticEvidenceFilename, finalSemanticMarkdownFilename} {
		if _, err := os.Stat(filepath.Join(runDir, name)); !os.IsNotExist(err) {
			t.Fatalf("closed replay unexpectedly has loose %s before source construction: %v", name, err)
		}
	}
	archive, err := openFinalSemanticArchive(context.Background(), cfg, stateDir, runDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := archive.bindCallInputs(result, &terminal, history); err != nil {
		t.Fatalf("closed replay inputs: %v", err)
	}
	var replayed FinalSemanticEvidence
	if err := archive.buildAdversarialCampaign(&replayed, result); err != nil {
		t.Fatalf("replay adversarial source from closed graph: %v", err)
	}
	if replayed.Adversaries.MatrixArtifact != collected.AdversarialMatrix || replayed.Adversaries.Artifact != collected.Adversaries {
		t.Fatalf("replayed adversarial locators=%+v", replayed.Adversaries)
	}
	if err := verifyFinalAdversarialCampaign(replayed.Adversaries); err != nil {
		t.Fatalf("replayed adversarial summary: %v", err)
	}
}

// Older semantic and collected-input schemas cannot omit the adversarial
// closed-graph fields introduced by the current release.
func TestFinalSemanticSchemasRejectPriorVersions(t *testing.T) {
	t.Parallel()
	source, _ := finalSemanticFixture(t)
	evidence, err := BuildFinalSemanticEvidence(source)
	if err != nil {
		t.Fatal(err)
	}
	legacyEvidence := *evidence
	legacyEvidence.Schema = "urnetwork-final-semantic-evidence-v2"
	if err := VerifyFinalSemanticEvidence(&legacyEvidence); err == nil || !strings.Contains(err.Error(), "unsupported final semantic evidence schema") {
		t.Fatalf("prior semantic evidence schema accepted: %v", err)
	}
	legacyInputs := &FinalSemanticCollectedInputs{Schema: "urnetwork-final-semantic-collected-inputs-v1"}
	if err := verifyFinalSemanticCollectedInputs(testResolvedConfig(t), legacyInputs); err == nil || !strings.Contains(err.Error(), "unsupported final semantic collected-inputs schema") {
		t.Fatalf("prior collected-input schema accepted: %v", err)
	}
}
