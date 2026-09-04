package main

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	validatorpkg "github.com/urfoundation/sn/validator"
	"github.com/urnetwork/connect"
)

func TestVerifyFinalCollectedAttemptRecordsAcceptsRecoveredPendingLifecycle(t *testing.T) {
	firstTrail := connect.Id{1}
	secondTrail := connect.Id{2}
	records := &FinalCollectedAttemptRecords{
		Schema: finalCollectedAttemptRecordsSchema, ValidatorID: 1, NoID: 1, FirstSequence: 1, LastSequence: 4,
		DispositionCounts: map[string]uint64{
			validatorpkg.AttemptDispositionPending:        2,
			validatorpkg.AttemptDispositionValidatorError: 1,
			validatorpkg.AttemptDispositionComplete:       1,
		},
		PendingRecoveredCount: 1,
		Records: []validatorpkg.AttemptRecord{
			{Sequence: 1, TrailID: firstTrail, Disposition: validatorpkg.AttemptDispositionPending},
			{Sequence: 2, TrailID: firstTrail, Disposition: validatorpkg.AttemptDispositionValidatorError},
			{Sequence: 3, TrailID: secondTrail, Disposition: validatorpkg.AttemptDispositionPending},
			{Sequence: 4, TrailID: secondTrail, Disposition: validatorpkg.AttemptDispositionComplete},
		},
	}
	summary := &FinalCollectedValidatorAttempts{
		NoID: 1, RecordCount: 4, CheckpointCount: 2, CompleteCount: 1, FailedCount: 1,
		PendingCount: 0, PendingRecoveredCount: 1,
	}
	if err := verifyFinalCollectedAttemptRecords(records, summary); err != nil {
		t.Fatal(err)
	}
	bad := *summary
	bad.PendingCount = 1
	if err := verifyFinalCollectedAttemptRecords(records, &bad); err == nil {
		t.Fatal("unresolved pending summary was accepted")
	}
	bad = *summary
	bad.PendingRecoveredCount = 0
	if err := verifyFinalCollectedAttemptRecords(records, &bad); err == nil {
		t.Fatal("recovered pending record was omitted from summary")
	}
}

func TestFinalLifecycleIntentRequirementsKeepSettlementAndNativeClocksDistinct(t *testing.T) {
	observation := fleetLifecycleDecisionFixture(10)
	census, ready, err := fleetLifecycleDecisionCensus(observation)
	if err != nil || !ready {
		t.Fatalf("lifecycle fixture ready=%t error=%v", ready, err)
	}
	census.Phase = "release-1.0"
	observation.FleetLifecycle = &FleetLifecycleEvidence{
		FirstAcceptedEpoch:              census.Validators[0].SettlementEpoch,
		AcceptanceStartBlock:            1,
		AcceptanceEndBlock:              1_501,
		AcceptanceTerminalBlock:         1_651,
		ReleaseHandoffSchedule:          &FleetLifecycleNativeSchedule{ApplicationDeadlineBlock: 1_651},
		ReleaseEVMEvidenceDeadlineBlock: 1_651,
		CandidateCensuses:               []FleetLifecycleCandidateCensus{census},
	}
	requirements, err := finalLifecycleIntentRequirements(observation, "release-1.0")
	if err != nil {
		t.Fatal(err)
	}
	if len(requirements) != 2 || len(requirements[1]) != 1 || requirements[1][0].SettlementEpoch == requirements[1][0].SubnetEpoch {
		t.Fatalf("dual-clock lifecycle requirements=%+v", requirements)
	}
	expected := requirements[1][0]
	intent := &validatorpkg.SteeringIntent{
		ValidatorID:             uint64(expected.ValidatorID),
		Status:                  "applied",
		SettlementEpoch:         expected.SettlementEpoch,
		SubnetEpoch:             expected.SubnetEpoch,
		NativeSnapshotBlock:     expected.NativeSnapshot.Number,
		NativeSnapshotHash:      expected.NativeSnapshot.Hash,
		EVMSnapshotBlock:        expected.EVMSnapshot.Number,
		EVMSnapshotHash:         expected.EVMSnapshot.Hash,
		MeasurementArtifactHash: expected.MeasurementArtifactHash,
		VectorHash:              expected.VectorHash,
		ExtrinsicHash:           expected.ExtrinsicHash,
		FinalizedBlock:          expected.Commit.Number,
		FinalizedBlockHash:      expected.Commit.Hash,
		RevealBlock:             expected.RevealBlock,
		ApplicationBlock:        expected.Application.Number,
		ApplicationBlockHash:    expected.Application.Hash,
		EligibleHeadUIDs:        slices.Clone(expected.EligibleUIDs),
		SelectedHeadUIDs:        slices.Clone(expected.SelectedUIDs),
		RejectedHeadUIDs:        slices.Clone(expected.RejectedUIDs),
	}
	for _, weight := range expected.AppliedWeights {
		intent.UIDs = append(intent.UIDs, weight.UID)
		intent.Values = append(intent.Values, weight.Value)
	}
	if !finalLifecycleIntentMatches(intent, expected) {
		t.Fatal("exact dual-clock lifecycle intent did not match")
	}
	for index, uid := range intent.UIDs {
		if slices.Contains(expected.RejectedUIDs, uid) {
			intent.UIDs = append(intent.UIDs[:index], intent.UIDs[index+1:]...)
			intent.Values = append(intent.Values[:index], intent.Values[index+1:]...)
			break
		}
	}
	if !finalLifecycleIntentMatches(intent, expected) {
		t.Fatal("sparse lifecycle intent did not preserve an implicit rejected zero")
	}
	missingPositive := *intent
	missingPositive.UIDs = append([]uint16(nil), intent.UIDs...)
	missingPositive.Values = append([]uint16(nil), intent.Values...)
	for index, uid := range missingPositive.UIDs {
		if slices.Contains(expected.SelectedUIDs, uid) {
			missingPositive.UIDs = append(missingPositive.UIDs[:index], missingPositive.UIDs[index+1:]...)
			missingPositive.Values = append(missingPositive.Values[:index], missingPositive.Values[index+1:]...)
			break
		}
	}
	if finalLifecycleIntentMatches(&missingPositive, expected) {
		t.Fatal("sparse lifecycle intent omitted a positive selected weight")
	}
	intent.SubnetEpoch = intent.SettlementEpoch
	if finalLifecycleIntentMatches(intent, expected) {
		t.Fatal("settlement epoch substituted for native subnet epoch")
	}
	if other, err := finalLifecycleIntentRequirements(observation, "production-soak"); err != nil || len(other) != 0 {
		t.Fatalf("release lifecycle intents leaked into production collection: %v %+v", err, other)
	}
}

func TestVerifyFinalCollectedPriorManifestUsesPublishedCampaignKind(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	owner := roles.EVM["testnet-owner"]
	key, err := crypto.HexToECDSA(strings.TrimPrefix(owner.PrivateKeyHex, "0x"))
	if err != nil {
		t.Fatal(err)
	}
	const runID = "prior-release-run"
	envelope, err := signEvidence(cfg, campaignEvidenceManifestKind, runID, map[string]any{"schema": campaignEvidenceManifestSchema}, owner)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyFinalCollectedPriorManifestEnvelope(envelope, &key.PublicKey, runID, envelope.ContentHash); err != nil {
		t.Fatalf("published campaign manifest kind was rejected: %v", err)
	}
	oldLiteral, err := signEvidence(cfg, "campaign-evidence-manifest", runID, map[string]any{"schema": campaignEvidenceManifestSchema}, owner)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyFinalCollectedPriorManifestEnvelope(oldLiteral, &key.PublicKey, runID, oldLiteral.ContentHash); err == nil {
		t.Fatal("obsolete campaign manifest literal was accepted")
	}
}

func TestFinalCollectedPriorLifecycleHandoffBindsCopiedRawBytes(t *testing.T) {
	cfg := testResolvedConfig(t)
	const runID = "prior-release-run"
	lifecycle := FleetLifecycleEvidence{
		Schema: fleetLifecycleEvidenceSchema, DeploymentID: cfg.Config.Deployment.DeploymentID,
		PlanHash: finalTestHex(0x41), RunID: runID, Stage: fleetLifecycleStageReleaseHandoff,
	}
	raw, err := json.Marshal(&lifecycle)
	if err != nil {
		t.Fatal(err)
	}
	binding := &ScenarioLifecycleHandoff{
		Schema: scenarioLifecycleHandoffSchema, ReleaseRunID: runID, Stage: fleetLifecycleStageReleaseHandoff,
		File: scenarioLifecycleHandoffFilename, ContentHash: bytesSHA256(raw), SizeBytes: uint64(len(raw)),
	}
	result := &ScenarioResult{Name: "release-1.0", RunID: runID, LifecycleHandoff: binding}
	completion := &scenarioCompletePayload{
		LifecycleHandoff: binding,
		Files:            map[string]string{binding.File: binding.ContentHash},
	}
	locator := FinalArtifactLocator{
		Kind: "prior-lifecycle-handoff", URI: "final-inputs/prior-release/fleet-lifecycle-handoff.json.bin",
		ContentHash: binding.ContentHash, SizeBytes: binding.SizeBytes,
	}
	if err := validateFinalCollectedPriorLifecycleHandoff(cfg, result, completion, locator, raw); err != nil {
		t.Fatalf("exact copied prior lifecycle handoff rejected: %v", err)
	}
	if err := validateFinalCollectedPriorLifecycleHandoff(cfg, result, completion, FinalArtifactLocator{}, raw); err == nil {
		t.Fatal("missing prior lifecycle handoff locator was accepted")
	}

	foreignLifecycle := lifecycle
	foreignLifecycle.RunID = "foreign-release-run"
	foreignRaw, err := json.Marshal(&foreignLifecycle)
	if err != nil {
		t.Fatal(err)
	}
	foreignBinding := *binding
	foreignBinding.ContentHash = bytesSHA256(foreignRaw)
	foreignBinding.SizeBytes = uint64(len(foreignRaw))
	foreignResult := *result
	foreignResult.LifecycleHandoff = &foreignBinding
	foreignCompletion := *completion
	foreignCompletion.LifecycleHandoff = &foreignBinding
	foreignCompletion.Files = map[string]string{foreignBinding.File: foreignBinding.ContentHash}
	foreignLocator := locator
	foreignLocator.ContentHash = foreignBinding.ContentHash
	foreignLocator.SizeBytes = foreignBinding.SizeBytes
	if err := validateFinalCollectedPriorLifecycleHandoff(cfg, &foreignResult, &foreignCompletion, foreignLocator, foreignRaw); err == nil {
		t.Fatal("foreign rehashed prior lifecycle handoff was accepted")
	}

	changedRaw := append(append([]byte(nil), raw...), '\n')
	changedLocator := locator
	changedLocator.ContentHash = bytesSHA256(changedRaw)
	changedLocator.SizeBytes = uint64(len(changedRaw))
	if err := validateFinalCollectedPriorLifecycleHandoff(cfg, result, completion, changedLocator, changedRaw); err == nil {
		t.Fatal("changed prior lifecycle handoff with a recomputed locator bypassed its signed result binding")
	}
}

func finalCollectedPriorPhaseByteFixture(t *testing.T) (*ResolvedConfig, *FinalCollectedPriorPhaseInputs, map[string][]byte) {
	t.Helper()
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
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	source.Deployment.GovernanceOwner = strings.ToLower(roles.EVM["testnet-owner"].Address)
	result, lifecycleRaw, _ := finalSemanticCampaignResultFixture(t, cfg, roles, &source, artifacts)
	draft, err := BuildFinalSemanticEvidence(source)
	if err != nil {
		t.Fatal(err)
	}
	semantic, err := SealFinalSemanticEvidenceOnChain(context.Background(), draft, &finalTestChainReader{evidence: draft})
	if err != nil {
		t.Fatal(err)
	}

	encode := func(value any) []byte {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	locator := func(kind, uri string, data []byte) FinalArtifactLocator {
		return FinalArtifactLocator{Kind: kind, URI: uri, ContentHash: bytesSHA256(data), SizeBytes: uint64(len(data))}
	}
	resultData := encode(result)
	resultLocator := locator("prior-scenario-result", "final-inputs/prior-release/result.json.bin", resultData)
	handoffLocator := locator("prior-lifecycle-handoff", "final-inputs/prior-release/fleet-lifecycle-handoff.json.bin", lifecycleRaw)
	bundlePayloadHash := bytesSHA256([]byte("prior scenario bundle payload"))
	manifestPayload := campaignEvidenceManifestPayload{
		Schema: campaignEvidenceManifestSchema, DeploymentID: source.DeploymentID, ChainID: source.ChainID,
		GenesisHash: source.GenesisHash, Netuid: source.Netuid, RunID: source.RunID,
		ResultHash: source.ResultHash, BundlePayloadHash: bundlePayloadHash,
		Files: []campaignEvidenceFileEntry{{
			Path: result.LifecycleHandoff.File, ContentHash: result.LifecycleHandoff.ContentHash,
			Size: result.LifecycleHandoff.SizeBytes, EnvelopeHash: bytesSHA256([]byte("prior lifecycle file envelope")),
		}},
	}
	owner := roles.EVM["testnet-owner"]
	manifestEnvelope, err := signEvidence(cfg, campaignEvidenceManifestKind, result.RunID, manifestPayload, owner)
	if err != nil {
		t.Fatal(err)
	}
	manifestData := encode(manifestEnvelope)
	manifestLocator := locator("prior-evidence-manifest-envelope", "final-inputs/prior-release/campaign-evidence-manifest.json.bin", manifestData)
	completionPayload := scenarioCompletePayload{
		ResultHash: result.EvidenceHash, BundlePayloadHash: bundlePayloadHash,
		Files:                map[string]string{result.LifecycleHandoff.File: result.LifecycleHandoff.ContentHash},
		EvidenceManifestHash: manifestEnvelope.ContentHash, LifecycleHandoff: result.LifecycleHandoff,
	}
	completionEnvelope, err := signEvidence(cfg, "scenario-complete", result.RunID, completionPayload, owner)
	if err != nil {
		t.Fatal(err)
	}
	completionData := encode(completionEnvelope)
	completionLocator := locator("prior-owner-completion-envelope", "final-inputs/prior-release/complete.json.bin", completionData)

	collected := finalSemanticBuilderCollectedManifest(cfg, result.RunID, result.EvidenceHash, *result.AcceptanceWindow)
	collected.EvidenceHash, err = finalSemanticCollectedInputsHash(collected)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyFinalSemanticCollectedInputs(cfg, collected); err != nil {
		t.Fatalf("prior collected-input fixture: %v", err)
	}
	collectedData := encode(collected)
	collectedLocator := locator("prior-collected-input-manifest", "final-inputs/prior-release/collected-inputs-manifest.json.bin", collectedData)
	captureManifestLocator := locator("final-semantic-input-manifest", "final-inputs/manifest.json", collectedData)
	capture := finalSemanticCaptureStatus(result, collected, captureManifestLocator)
	capture.EvidenceHash, err = finalSemanticCaptureStatusHash(capture)
	if err != nil {
		t.Fatal(err)
	}
	captureData := encode(capture)
	captureLocator := locator("prior-capture-status", "final-inputs/prior-release/final-semantic-capture.json.bin", captureData)

	snapshot := FinalCollectedChainSnapshot{
		Schema: finalCollectedChainSnapshotSchema, Phase: "release-1.0", RunID: result.RunID,
		DeploymentID: result.DeploymentID, EVMHead: result.EndHead, NativeHead: semantic.NativeTerminalHead,
		NativeHeads: []ChainHead{semantic.NativeTerminalHead},
	}
	snapshotData := encode(snapshot)
	bundle := FinalCollectedFileBundle{
		Schema: finalCollectedFileBundleSchema, Name: "live-chain",
		Files: []FinalCollectedFileBundleEntry{{Path: "live-chain/final-chain-snapshot.json", ContentHash: bytesSHA256(snapshotData), SizeBytes: uint64(len(snapshotData)), Data: snapshotData}},
	}
	bundleData := encode(bundle)
	bundleLocator := locator("prior-live-chain-bundle", "final-inputs/prior-release/live-chain/bundle-000.json", bundleData)

	semanticData := encode(semantic)
	markdownData, err := RenderFinalSemanticEvidenceMarkdown(semantic)
	if err != nil {
		t.Fatal(err)
	}
	rawFiles := []finalSemanticRawFile{
		{Path: finalSemanticEvidenceFilename, ContentHash: bytesSHA256(semanticData), Data: semanticData},
		{Path: finalSemanticMarkdownFilename, ContentHash: bytesSHA256(markdownData), Data: markdownData},
	}
	sort.Slice(rawFiles, func(i, j int) bool { return rawFiles[i].Path < rawFiles[j].Path })
	semanticLocators := make([]FinalArtifactLocator, 0, len(rawFiles))
	semanticEntries := make([]FinalSemanticSupplementFile, 0, len(rawFiles))
	semanticEnvelopeData := make([][]byte, 0, len(rawFiles))
	for index, raw := range rawFiles {
		payload := finalSemanticSupplementFilePayload{
			Schema: finalSemanticSupplementFileSchema, RunID: result.RunID, Path: raw.Path,
			ContentHash: raw.ContentHash, Size: uint64(len(raw.Data)), Data: raw.Data,
		}
		envelope, err := signEvidence(cfg, finalSemanticSupplementFileKind, result.RunID, payload, owner)
		if err != nil {
			t.Fatal(err)
		}
		data := encode(envelope)
		semanticEntries = append(semanticEntries, FinalSemanticSupplementFile{Path: raw.Path, ContentHash: raw.ContentHash, Size: uint64(len(raw.Data)), EnvelopeHash: envelope.ContentHash})
		semanticLocators = append(semanticLocators, locator("prior-semantic-file-envelope", fmt.Sprintf("final-inputs/prior-release/semantic-files/%03d.evidence.json", index), data))
		semanticEnvelopeData = append(semanticEnvelopeData, data)
	}
	supplementPayload := FinalSemanticSupplementPayload{
		Schema: finalSemanticSupplementSchema, Status: finalSemanticSupplementStatus,
		Phase: "release-1.0", RunID: result.RunID, ResultHash: result.EvidenceHash,
		ScenarioCompleteHash: completionEnvelope.ContentHash, ScenarioEvidenceManifestHash: manifestEnvelope.ContentHash,
		CaptureStatusHash: capture.EvidenceHash, CollectedInputsHash: collected.EvidenceHash,
		SemanticEvidenceHash: semantic.EvidenceHash, PublicTranscriptHash: semantic.PublicVerification.TranscriptHash,
		Files: semanticEntries,
	}
	supplementEnvelope, err := signEvidence(cfg, finalSemanticSupplementKind, result.RunID, supplementPayload, owner)
	if err != nil {
		t.Fatal(err)
	}
	supplementData := encode(supplementEnvelope)
	supplementLocator := locator("prior-semantic-supplement-envelope", "final-inputs/prior-release/semantic-verified.evidence.json.bin", supplementData)

	prior := &FinalCollectedPriorPhaseInputs{
		Phase: "release-1.0", RunID: result.RunID, ResultHash: result.EvidenceHash, Window: *result.AcceptanceWindow,
		ScenarioResult: resultLocator, OwnerCompletion: completionLocator, EvidenceManifest: manifestLocator,
		LifecycleHandoff: handoffLocator, CaptureStatus: captureLocator, CollectedInputsManifest: collectedLocator,
		LiveChainBundles: []FinalArtifactLocator{bundleLocator}, SemanticSupplement: supplementLocator,
		SemanticFileEnvelopes: semanticLocators,
	}
	loaded := map[string][]byte{}
	add := func(item FinalArtifactLocator, data []byte) {
		loaded[item.Kind] = append([]byte(nil), data...)
		loaded[item.URI] = append([]byte(nil), data...)
	}
	for _, item := range []struct {
		locator FinalArtifactLocator
		data    []byte
	}{
		{resultLocator, resultData}, {completionLocator, completionData}, {manifestLocator, manifestData},
		{handoffLocator, lifecycleRaw}, {captureLocator, captureData}, {collectedLocator, collectedData},
		{bundleLocator, bundleData}, {supplementLocator, supplementData},
	} {
		add(item.locator, item.data)
	}
	for index, item := range semanticLocators {
		add(item, semanticEnvelopeData[index])
	}
	return cfg, prior, loaded
}

func TestVerifyFinalCollectedPriorPhaseBytesRejectsReopenedHandoffSubstitution(t *testing.T) {
	cfg, prior, loaded := finalCollectedPriorPhaseByteFixture(t)
	if err := verifyFinalCollectedPriorPhaseBytes(cfg, prior, loaded); err != nil {
		t.Fatalf("exact reopened prior release graph rejected: %v", err)
	}

	missing := *prior
	missing.LifecycleHandoff = FinalArtifactLocator{}
	if err := verifyFinalCollectedPriorPhaseBytes(cfg, &missing, loaded); err == nil {
		t.Fatal("reopened prior release graph without its handoff locator was accepted")
	}

	var result ScenarioResult
	if err := decodeStrictJSONBytes(loaded[prior.ScenarioResult.Kind], &result); err != nil {
		t.Fatal(err)
	}
	foreign := FleetLifecycleEvidence{
		Schema: fleetLifecycleEvidenceSchema, DeploymentID: cfg.Config.Deployment.DeploymentID,
		PlanHash: finalTestHex(0x51), RunID: "foreign-release-run", Stage: fleetLifecycleStageReleaseHandoff,
	}
	foreignRaw, err := json.Marshal(&foreign)
	if err != nil {
		t.Fatal(err)
	}
	changedBinding := *result.LifecycleHandoff
	changedBinding.ContentHash = bytesSHA256(foreignRaw)
	changedBinding.SizeBytes = uint64(len(foreignRaw))
	result.LifecycleHandoff = &changedBinding
	changedResultData, err := json.Marshal(&result)
	if err != nil {
		t.Fatal(err)
	}
	changed := *prior
	changed.ScenarioResult.ContentHash = bytesSHA256(changedResultData)
	changed.ScenarioResult.SizeBytes = uint64(len(changedResultData))
	changed.LifecycleHandoff.ContentHash = bytesSHA256(foreignRaw)
	changed.LifecycleHandoff.SizeBytes = uint64(len(foreignRaw))
	changedLoaded := make(map[string][]byte, len(loaded))
	for name, data := range loaded {
		changedLoaded[name] = append([]byte(nil), data...)
	}
	changedLoaded[changed.ScenarioResult.Kind] = changedResultData
	changedLoaded[changed.ScenarioResult.URI] = changedResultData
	changedLoaded[changed.LifecycleHandoff.Kind] = foreignRaw
	changedLoaded[changed.LifecycleHandoff.URI] = foreignRaw
	if err := verifyFinalCollectedPriorPhaseBytes(cfg, &changed, changedLoaded); err == nil {
		t.Fatal("reopened prior graph accepted a well-formed foreign handoff with recomputed result and locator hashes")
	}
}
