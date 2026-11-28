// Synthetic complete journals cross the former capture limits through the
// actual carrier readers. No production identity or live source is retained.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	validatorpkg "github.com/urfoundation/sn/v2026/validator"
)

// Distinct failed actions retain diagnostic padding inside each hashed record;
// every record stays below the journal's independent four-megabyte ceiling.
func finalJournalCapacityFixture(t *testing.T, records, padding int) []byte {
	t.Helper()
	var raw bytes.Buffer
	previous := ""
	for index := 0; index < records; index++ {
		entry := JournalEntry{Schema: "urnetwork-sim-journal-v1", Sequence: uint64(index + 1), Time: "2000-01-01T00:00:00Z",
			DeploymentID: "synthetic-journal-capacity", PlanHash: "0x" + strings.Repeat("12", 32),
			ActionID: fmt.Sprintf("synthetic-action-%d", index), IntentHash: "synthetic-intent", Stage: StageFailed,
			Error: strings.Repeat("x", padding), PreviousHash: previous}
		var err error
		entry.EntryHash, err = canonicalHashHex(entry)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.NewEncoder(&raw).Encode(entry); err != nil {
			t.Fatal(err)
		}
		previous = entry.EntryHash
	}
	return raw.Bytes()
}

// Both singleton bundle classes and compound lineages retain byte-identical
// journals beyond both old limits. The old ordinary reader still refuses.
func TestFinalJournalCapacityBundlesAndLineagesRoundTrip(t *testing.T) {
	raw := finalJournalCapacityFixture(t, 33, 1024*1024)
	if len(raw) <= maximumCampaignEvidenceRawFileBytes {
		t.Fatal("fixture did not cross the former journal bound")
	}
	cfg := campaignMetadataConfigTestV2(t)
	limits, err := campaignEvidenceLimitsForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	stateRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(stateRoot, "journal.jsonl"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	entry, err := finalCollectedFileEntryWithLimit(stateRoot, "journal.jsonl", maximumFinalJournalBytes)
	if err != nil || !bytes.Equal(entry.Data, raw) {
		t.Fatal("journal source lost exact bytes", err)
	}
	if _, err := finalCollectedFileEntry(stateRoot, "journal.jsonl"); err == nil {
		t.Fatal("ordinary file reader borrowed journal capacity")
	}
	runRoot := filepath.Join(stateRoot, "runs", "synthetic-journal-run")
	for _, class := range []string{"launch-foundation", "validator-evidence-companion"} {
		locators, err := persistFinalCollectedBundleChunksContext(t.Context(), runRoot, class, []FinalCollectedFileBundleEntry{entry})
		if err != nil || len(locators) != 1 {
			t.Fatalf("%s cannot carry the original singleton journal: %v", class, err)
		}
		encoded, err := readCampaignEvidenceFileWithLimitsV2(runRoot, locators[0].URI, limits, false)
		if err != nil {
			t.Fatal("typed bundle readback", err)
		}
		bundle, err := decodeFinalCollectedFileBundle(encoded)
		if err != nil || bundle.Schema != finalCollectedFileBundleSchema || len(bundle.Files) != 1 || bundle.Files[0].Path != "journal.jsonl" || !bytes.Equal(bundle.Files[0].Data, raw) {
			t.Fatal("legacy singleton wire changed", err)
		}
	}
	for _, phase := range []struct{ name, schema, runId string }{
		{name: "final-derived/fleet-generation-lineage.json", schema: finalFleetGenerationLineageSchema},
		{name: "final-derived/fleet-lifecycle-lineage.json", schema: finalFleetLifecycleLineageSchema, runId: "synthetic-journal-run"},
	} {
		lineage := struct {
			Schema       string                            `json:"schema"`
			DeploymentId string                            `json:"deployment_id"`
			PlanHash     string                            `json:"plan_hash"`
			RunId        string                            `json:"run_id,omitempty"`
			Files        []finalFleetGenerationLineageFile `json:"files"`
		}{Schema: phase.schema, DeploymentId: "synthetic-journal-capacity", PlanHash: "0x" + strings.Repeat("12", 32), RunId: phase.runId,
			Files: []finalFleetGenerationLineageFile{{Path: "launch-foundation/journal.jsonl", ContentHash: bytesSHA256(raw), SizeBytes: uint64(len(raw)), Data: raw}}}
		encoded, err := json.Marshal(lineage)
		if err != nil || validateCampaignMetadataRawV2(limits, phase.name, encoded) != nil {
			t.Fatal("lineage lost exact journal capacity", err)
		}
		lineage.Files[0].Path = "launch-foundation/ordinary-proof.json"
		encoded, err = json.Marshal(lineage)
		if err != nil || validateCampaignMetadataRawV2(limits, phase.name, encoded) == nil {
			t.Fatal("ordinary lineage source borrowed journal capacity", err)
		}
	}
	locator, err := persistFinalCollectedArtifactForConfigV2(cfg, runRoot, "historical-journal", finalHistoricalJournalPath, raw)
	if err != nil {
		t.Fatal("historical journal cannot be retained", err)
	}
	load, err := newFinalSemanticCampaignArtifactLoaderForConfigV2(cfg, stateRoot, runRoot)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := load(t.Context(), locator)
	if err != nil || !bytes.Equal(replayed, raw) {
		t.Fatal("historical journal consumer changed raw bytes", err)
	}
}

// The exact signed public envelope and prior-phase verifier must admit the
// new dedicated source while preserving generic hash-addressed proof limits.
func TestFinalJournalCapacitySignedPublicCarrierRoundTrip(t *testing.T) {
	raw := finalJournalCapacityFixture(t, 33, 1024*1024)
	cfg := campaignMetadataConfigTestV2(t)
	limits, err := campaignEvidenceLimitsForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	name := finalJournalCapturePrefixV2 + strings.TrimPrefix(bytesSHA256(raw), "sha256:") + ".jsonl"
	root := t.TempDir()
	locator, err := persistFinalCollectedArtifactForConfigV2(cfg, root, "validator-evidence-v2-source", name, raw)
	if err != nil || locator.ContentHash != bytesSHA256(raw) {
		t.Fatal("dedicated journal writer failed", err)
	}
	payload := campaignEvidenceFilePayload{Schema: campaignEvidenceFileSchema, RunID: "synthetic-journal-run", Scope: "run", Path: name, ContentHash: bytesSHA256(raw), Size: uint64(len(raw)), Data: raw}
	envelope, wire, err := prepareLocalEvidence(cfg, root, filepath.Join(root, "carrier.evidence.json"), campaignEvidenceFileKind, payload.RunID, payload, roles.EVM["testnet-owner"], 0)
	if err != nil {
		t.Fatal("journal public carrier cannot be signed", err)
	}
	entry := campaignEvidenceFileEntry{Path: name, ContentHash: payload.ContentHash, Size: payload.Size, EnvelopeHash: envelope.ContentHash}
	_, recovered, _, err := verifyPublicCampaignFileWireV2(limits, payload.RunID, "run", entry, wire)
	if err != nil || !bytes.Equal(recovered, raw) {
		t.Fatal("independent public journal readback differs", err)
	}
	if err := verifyFinalPriorCarrierWireV2(cfg, payload.RunID, "run", entry, envelope.Signer, wire); err != nil {
		t.Fatal("prior phase cannot authenticate the original journal", err)
	}
	if _, err := persistFinalCollectedArtifactForConfigV2(cfg, root, "source", "final-inputs/validators/v2/"+strings.TrimPrefix(payload.ContentHash, "sha256:")+".bin", raw); err == nil {
		t.Fatal("generic compact source acquired journal capacity")
	}
	// A carrier's larger outer bound cannot conceal an oversized ordinary
	// source inside a companion bundle, even with correct byte hashes.
	ordinaryBundle := FinalCollectedFileBundle{Schema: finalCollectedFileBundleSchema, Name: "validator-evidence-companion", Files: []FinalCollectedFileBundleEntry{finalCaptureCapacityEntry("ordinary-proof.json", raw)}}
	ordinaryRaw, err := json.Marshal(ordinaryBundle)
	if err != nil {
		t.Fatal(err)
	}
	payload.Path, payload.Data = "final-inputs/bundles/validator-evidence-companion.json", ordinaryRaw
	payload.Size, payload.ContentHash = uint64(len(ordinaryRaw)), bytesSHA256(ordinaryRaw)
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	entry = campaignEvidenceFileEntry{Path: payload.Path, ContentHash: payload.ContentHash, Size: payload.Size}
	if err := verifyFinalPriorCarrierFilePayloadV2(limits, payload.RunID, "run", entry, encoded); err == nil {
		t.Fatal("prior companion carrier hid an oversized ordinary source")
	}
}

// Declared bounds exercise exact and one-over admission without allocating a
// full maximum file. Approximate paths cannot borrow the dedicated namespace.
func TestFinalJournalCapacityExactPathsAndBounds(t *testing.T) {
	limits := defaultCampaignEvidenceLimits()
	digest := strings.Repeat("ab", 32)
	for _, name := range []string{finalHistoricalJournalPath, finalJournalCapturePrefixV2 + digest + ".jsonl"} {
		if limits.rawFileBytes(name) != maximumFinalJournalBytes {
			t.Fatalf("journal owner differs for %s", name)
		}
		if _, err := limits.fileEnvelopeBytes(name, maximumFinalJournalBytes); err != nil {
			t.Fatal("exact journal cannot receive a carrier", err)
		}
		if _, err := limits.fileEnvelopeBytes(name, maximumFinalJournalBytes+1); err == nil {
			t.Fatal("one-over journal received a carrier")
		}
	}
	for _, name := range []string{finalJournalCapturePrefixV2 + strings.ToUpper(digest) + ".jsonl", finalJournalCapturePrefixV2 + digest + ".bin", finalJournalCapturePrefixV2 + "nested/" + digest + ".jsonl", finalHistoricalJournalPath + ".extra", "final-inputs/bundles/validator-evidence-companion-extra.json", "final-inputs/bundles/validator-evidence-companion-0-of-2.json"} {
		if limits.rawFileBytes(name) != maximumCampaignEvidenceRawFileBytes {
			t.Fatalf("approximate journal path acquired capacity: %s", name)
		}
	}
	for _, class := range []string{"launch-foundation", "launch-foundation-001-of-002", "validator-evidence-companion", "validator-evidence-companion-002-of-002"} {
		if finalPlanBundleSourceBytes(class, "journal.jsonl") != maximumFinalJournalBytes || finalPlanBundleSourceBytes(class, "ordinary.json") != finalCollectedBundleMaximumRawBytes {
			t.Fatalf("%s changed ordinary source admission", class)
		}
	}
}

// Signed byte counts and hashes alone do not make malformed records a
// journal. Both prior-carrier decoder branches must apply the typed schema.
func TestFinalJournalCapacityPriorCarrierRejectsMalformedBody(t *testing.T) {
	raw := []byte("{}\n")
	name := finalJournalCapturePrefixV2 + strings.TrimPrefix(bytesSHA256(raw), "sha256:") + ".jsonl"
	payload := campaignEvidenceFilePayload{Schema: campaignEvidenceFileSchema, RunID: "synthetic-journal-run", Scope: "run", Path: name, ContentHash: bytesSHA256(raw), Size: uint64(len(raw)), Data: raw}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	entry := campaignEvidenceFileEntry{Path: name, ContentHash: payload.ContentHash, Size: payload.Size}
	limits := defaultCampaignEvidenceLimits()
	if err := verifyFinalPriorCarrierFilePayloadV2(limits, payload.RunID, "run", entry, encoded); err == nil {
		t.Fatal("base64 fast path accepted a malformed journal")
	}
	canonical, err := canonicalFinalPriorFilePayloadV2(limits, payload.RunID, "run", entry, encoded)
	if err != nil || canonical != nil {
		t.Fatal("typed journal bypassed full prior source validation", err)
	}
	if _, err := persistFinalCollectedArtifactForConfigV2(campaignMetadataConfigTestV2(t), t.TempDir(), "validator-evidence-v2-source", name, raw); err == nil {
		t.Fatal("malformed dedicated source reached an immutable write")
	}
}

// New source identity and its raw digest travel together. Existing small
// generic journals preserve their original locator and decoder contract.
func TestFinalJournalCapacitySourceIdentityAndLegacyWire(t *testing.T) {
	raw := finalJournalCapacityFixture(t, 2, 0)
	hash := bytesSHA256(raw)
	source := FinalCollectedValidatorSourceV2{Source: validatorpkg.ReleaseEvidenceV2CaptureSource{Kind: "relay-journal", Name: "journal.jsonl"}, Artifact: FinalArtifactLocator{Kind: "validator-evidence-v2-source", URI: finalJournalCapturePrefixV2 + strings.TrimPrefix(hash, "sha256:") + ".jsonl", ContentHash: hash, SizeBytes: uint64(len(raw))}}
	if err := validateFinalJournalCaptureSourceV2(source); err != nil || validateFinalJournalArtifactBytes(source.Artifact.URI, raw) != nil {
		t.Fatal("original journal identity refused", err)
	}
	for _, mutate := range []func(*FinalCollectedValidatorSourceV2){
		func(value *FinalCollectedValidatorSourceV2) { value.Source.Kind = "terminal-payload" },
		func(value *FinalCollectedValidatorSourceV2) { value.Source.Name = "different.jsonl" },
		func(value *FinalCollectedValidatorSourceV2) { value.Source.Origin = "https://synthetic.example" },
		func(value *FinalCollectedValidatorSourceV2) {
			value.Artifact.ContentHash = "sha256:" + strings.Repeat("ab", 32)
		},
	} {
		wrong := source
		mutate(&wrong)
		if err := validateFinalJournalCaptureSourceV2(wrong); err == nil {
			t.Fatal("another source borrowed journal capacity")
		}
	}
	source.Artifact.URI = "final-inputs/validators/v2/" + strings.TrimPrefix(hash, "sha256:") + ".bin"
	if err := validateFinalJournalCaptureSourceV2(source); err != nil {
		t.Fatal("legacy small journal locator changed", err)
	}
	source.Artifact.SizeBytes = maximumCampaignEvidenceRawFileBytes + 1
	if err := validateFinalJournalCaptureSourceV2(source); err == nil {
		t.Fatal("legacy generic locator inherited the new capacity")
	}
	entry := finalCaptureCapacityEntry("journal.jsonl", raw)
	bundle := FinalCollectedFileBundle{Schema: finalCollectedFileBundleSchema, Name: "launch-foundation", Files: []FinalCollectedFileBundleEntry{entry}}
	encoded, err := json.Marshal(bundle)
	decoded, decodeErr := decodeFinalCollectedFileBundle(encoded)
	if err != nil || decodeErr != nil || !bytes.Equal(decoded.Files[0].Data, raw) {
		t.Fatal("legacy journal bundle wire changed", err, decodeErr)
	}
}
