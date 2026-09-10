// Materialize the complete configured flat metadata census and largest real
// base64 carrier. This is metadata/RSS qualification, not a raw-source transfer
// or acceptance of synthetic semantic evidence. Keep existing gate deadlines.
package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	validatorpkg "github.com/urfoundation/sn/v2026/validator"
)

// One current index plus every admitted source slot, and a complete prior
// carrier census, exercise actual flat encoding, graph discovery and signing.
// The external qualification owner records actual process-tree peak RSS on
// the 128 GiB host; these logical-byte assertions are not an RSS prediction.
func TestCampaignEvidenceCapacityV2MetadataFullCensusMaterializesFlatWireAndCarrier(t *testing.T) {
	cfg := campaignMetadataConfigTestV2(t)
	limits, err := campaignEvidenceLimitsForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if limits.maximumObjects != 1191936 || cfg.Config.Topology.Miners != 1000 || cfg.Config.Topology.Validators != 2 {
		t.Fatal("full metadata qualification census changed")
	}
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	owner := EVMRoleSecret{PrivateKeyHex: hex.EncodeToString(crypto.FromECDSA(key))}
	count := int(limits.maximumObjects)
	hash := "sha256:" + strings.Repeat("f", 64)
	value := &FinalSemanticCollectedInputs{Schema: finalSemanticCollectedInputsSchema, Phase: "production-soak", RunID: "full-metadata-census", Validators: []FinalCollectedValidatorInputs{
		{ValidatorID: 1, EvidenceV2: &FinalCollectedValidatorEvidenceV2{Schema: finalCollectedValidatorEvidenceV2Schema}},
		{ValidatorID: 2, EvidenceV2: &FinalCollectedValidatorEvidenceV2{Schema: finalCollectedValidatorEvidenceV2Schema}},
	}, PriorPhase: &FinalCollectedPriorPhaseInputs{Phase: "release-1.0", RunID: "prior-metadata-census", PublicCarriers: make([]FinalCollectedPriorCarrierV2, count)}}
	manifest := campaignEvidenceManifestPayload{Schema: campaignEvidenceManifestSchema, DeploymentID: cfg.Config.Deployment.DeploymentID, ChainID: cfg.ChainID, GenesisHash: cfg.Public.Chain.GenesisHash, Netuid: cfg.Netuid, RunID: value.RunID, ResultHash: "0x" + strings.Repeat("1", 64), BundlePayloadHash: hash, Files: make([]campaignEvidenceFileEntry, count)}
	completion := scenarioCompletePayload{ResultHash: manifest.ResultHash, BundlePayloadHash: hash, EvidenceManifestHash: hash, Files: make(map[string]string, count)}
	for index := 0; index < count-1; index++ {
		name := fmt.Sprintf("final-inputs/validators/v2/%064x.bin", index+1)
		source := FinalCollectedValidatorSourceV2{Source: validatorpkg.ReleaseEvidenceV2CaptureSource{Kind: "relay-original-transaction", Name: fmt.Sprintf("evidence-deposit-audit-prepared/epoch-%020d-observation-%020d-native-%020d.json", index+1, ^uint64(0), ^uint64(0)), Origin: cfg.OperatorAPIOrigins[index%2]}, Artifact: FinalArtifactLocator{Kind: "validator-evidence-v2-source", URI: name, ContentHash: hash, SizeBytes: 1}}
		member := value.Validators[index%2].EvidenceV2
		member.Sources = append(member.Sources, source)
		manifest.Files[index+1] = campaignEvidenceFileEntry{Path: name, ContentHash: hash, Size: 1, EnvelopeHash: hash}
		completion.Files[name] = hash
		value.PriorPhase.PublicCarriers[index+1] = FinalCollectedPriorCarrierV2{Scope: "run", Path: name, EnvelopeHash: hash, WireHash: hash, WireBytes: ^uint64(0), LocalHash: hash, LocalBytes: ^uint64(0), LocalSuffix: []byte{'\n'}}
	}
	value.PriorPhase.PublicCarriers[0] = FinalCollectedPriorCarrierV2{Scope: "run", Path: campaignCollectedIndexPathV2, EnvelopeHash: hash, WireHash: hash, WireBytes: ^uint64(0), LocalHash: hash, LocalBytes: ^uint64(0), LocalSuffix: []byte{'\n'}}
	if err := validateCollectedMetadataForConfigV2(cfg, value); err != nil {
		t.Fatal(err)
	}
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	if len(raw) <= 1024*1024*1024 || uint64(len(raw)) > limits.metadata.indexBytes {
		t.Fatalf("full flat index wire %d is outside its configured owner %d", len(raw), limits.metadata.indexBytes)
	}
	indexBytes := len(raw)
	root := t.TempDir()
	locator, err := persistFinalCollectedArtifactForConfigV2(cfg, root, "final-semantic-input-manifest", campaignCollectedIndexPathV2, raw)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := readCampaignEvidenceFileWithLimitsV2(root, campaignCollectedIndexPathV2, limits, false)
	if err != nil || !bytes.Equal(actual, raw) {
		t.Fatalf("materialized full index reader: %v", err)
	}
	actual = nil
	value = nil
	runtime.GC()
	references := map[string]campaignArtifactReference{}
	edges := map[string]map[string]bool{}
	budget, err := campaignEvidenceMetadataForConfigV2(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := mergeCampaignArtifactSourceWithBudgetV2(references, edges, campaignCollectedIndexPathV2, raw, limits, budget); err != nil {
		t.Fatal(err)
	}
	if len(references) != count-1 || len(edges[campaignCollectedIndexPathV2]) != count-1 || budget.usedBytes <= maximumCampaignEvidenceEnvelopeBytes || budget.usedBytes > limits.metadata.graphBytes {
		t.Fatalf("actual full flat graph: refs=%d edges=%d bytes=%d", len(references), len(edges[campaignCollectedIndexPathV2]), budget.usedBytes)
	}
	oldBudget := &campaignEvidenceMetadataBudgetV2{maximumBytes: maximumCampaignEvidenceEnvelopeBytes}
	if err := oldBudget.admit(map[string]campaignArtifactReference{}, map[string]map[string]bool{}, campaignCollectedIndexPathV2, references); err == nil || oldBudget.usedBytes != 0 {
		t.Fatal("original 64 MiB graph refusal was not reproduced atomically")
	}
	graphBytes := budget.usedBytes
	references, edges = nil, nil
	runtime.GC()
	manifest.Files[0] = campaignEvidenceFileEntry{Path: locator.URI, ContentHash: locator.ContentHash, Size: locator.SizeBytes, EnvelopeHash: hash}
	completion.Files[locator.URI] = locator.ContentHash
	signedManifest, err := signEvidence(cfg, campaignEvidenceManifestKind, manifest.RunID, manifest, owner)
	if err != nil || signedManifest == nil || len(signedManifest.Payload) <= maximumCampaignEvidenceEnvelopeBytes || uint64(len(signedManifest.Payload)) > limits.metadata.manifestBytes {
		t.Fatalf("full original signed manifest: %v", err)
	}
	manifestBytes := len(signedManifest.Payload)
	signedManifest, manifest.Files = nil, nil
	completion.Files["one-over-control.bin"] = hash
	if _, err := signEvidence(cfg, "scenario-complete", manifest.RunID, completion, owner); err == nil {
		t.Fatal("one-over full completion census reached encoding")
	}
	delete(completion.Files, "one-over-control.bin")
	signedCompletion, err := signEvidence(cfg, "scenario-complete", manifest.RunID, completion, owner)
	if err != nil || signedCompletion == nil || len(signedCompletion.Payload) <= maximumCampaignEvidenceEnvelopeBytes || uint64(len(signedCompletion.Payload)) > limits.metadata.completionBytes {
		t.Fatalf("full original signed completion: %v", err)
	}
	completionBytes := len(signedCompletion.Payload)
	signedCompletion, completion.Files = nil, nil
	runtime.GC()
	payload := campaignEvidenceFilePayload{Schema: campaignEvidenceFileSchema, RunID: manifest.RunID, Scope: "run", Path: locator.URI, ContentHash: locator.ContentHash, Size: locator.SizeBytes, Data: raw}
	// Exercise the actual bounded local publisher for the largest carrier.
	// Independent legacy-wire parity remains in the encoding regressions;
	// this whole-census owner still verifies every raw/hash/signature byte.
	carrierPath := filepath.Join(root, "published", "full-census.evidence.json")
	signedCarrier, wire, err := prepareLocalEvidence(cfg, root, carrierPath, campaignEvidenceFileKind, manifest.RunID, payload, owner, 1)
	if err != nil {
		t.Fatal(err)
	}
	maximum, err := limits.fileEnvelopeBytes(locator.URI, locator.SizeBytes)
	if err != nil || uint64(len(wire)) > uint64(maximum) || len(wire) <= len(raw) {
		t.Fatalf("full original base64 carrier: %d bound=%d %v", len(wire), maximum, err)
	}
	entry := campaignEvidenceFileEntry{Path: locator.URI, ContentHash: locator.ContentHash, Size: locator.SizeBytes, EnvelopeHash: signedCarrier.ContentHash}
	if err := verifyFinalPriorCarrierWireV2(cfg, manifest.RunID, "run", entry, signedCarrier.Signer, wire); err != nil {
		t.Fatalf("full prior canonical carrier authentication: %v", err)
	}
	t.Logf("materialized full metadata: objects=%d flat-index=%d signed-manifest-payload=%d signed-completion-payload=%d graph=%d original-base64-carrier=%d; source tape is not materialized; record external peak RSS", count, indexBytes, manifestBytes, completionBytes, graphBytes, len(wire))
}

// Refuse mismatched local completion bytes before acquiring either external
// store. An owner-signed envelope cannot authorize a different local marker.
func TestCampaignEvidenceCapacityV2MetadataCompletionWriteBindsExactSignedObject(t *testing.T) {
	cfg := campaignMetadataConfigTestV2(t)
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	owner := EVMRoleSecret{PrivateKeyHex: hex.EncodeToString(crypto.FromECDSA(key))}
	payload := scenarioCompletePayload{Files: map[string]string{"source.bin": "sha256:" + strings.Repeat("1", 64)}}
	complete, err := signEvidence(cfg, "scenario-complete", "metadata-completion", payload, owner)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.MarshalIndent(complete, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	var changed ReleaseEvidenceEnvelope
	if err := decodeStrictJSONBytes(encoded, &changed); err != nil {
		t.Fatal(err)
	}
	changed.RunID = "changed-completion"
	encoded, err = json.Marshal(&changed)
	if err != nil {
		t.Fatal(err)
	}
	stage, err := commitPublishedScenarioCompletion(t.Context(), cfg, nil, t.TempDir(), complete.RunID, complete, encoded, nil)
	if err == nil || stage != "complete_evidence_encoding" {
		t.Fatalf("different local completion reached publication: %q %v", stage, err)
	}
}
