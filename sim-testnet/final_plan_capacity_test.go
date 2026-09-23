// Plan transport must preserve exact approval bytes across capture, typed
// archives and public carriers without enlarging ordinary proof admission.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Only wire identity is relevant to capture capacity. This synthetic approval
// has no permission to execute or bypass the separate economic plan verifier.
func finalPlanCapacityWire(t *testing.T, description string, ancestors ...string) (*SetupPlan, []byte) {
	t.Helper()
	plan := &SetupPlan{Schema: currentSetupPlanSchema, DeploymentID: "synthetic-capacity-deployment", PriorPlanHashes: ancestors,
		Actions: []Action{{ID: "synthetic-history", Kind: "local", Target: "synthetic", Description: description}}}
	var err error
	plan.Actions[0].IntentHash, err = actionIntentHash(plan.Actions[0])
	if err != nil {
		t.Fatal(err)
	}
	plan.PlanHash, err = plan.hash()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	return plan, raw
}

// A real oversized body travels through the production bundle writer, bounded
// reader, payload signer and independent public wire decoder with exact hashes.
func TestFinalPlanCapacityCaptureAndPublicCarrierRoundTrip(t *testing.T) {
	plan, raw := finalPlanCapacityWire(t, strings.Repeat("synthetic source ", maximumCampaignEvidenceRawFileBytes/17+1))
	if len(raw) <= maximumCampaignEvidenceRawFileBytes {
		t.Fatal("fixture did not cross the ordinary proof capacity")
	}
	stateRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(stateRoot, "plan.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	entry, err := finalCollectedFileEntryWithLimit(stateRoot, "plan.json", maximumSetupPlanFileBytes)
	if err != nil || !bytes.Equal(entry.Data, raw) {
		t.Fatal("large approval cannot be captured exactly", err)
	}
	if _, err := finalCollectedFileEntry(stateRoot, "plan.json"); err == nil {
		t.Fatal("ordinary source reader acquired plan capacity")
	}
	runRoot := filepath.Join(stateRoot, "runs", "synthetic-capacity-run")
	locators, err := persistFinalCollectedBundleChunksContext(t.Context(), runRoot, "launch-foundation", []FinalCollectedFileBundleEntry{entry})
	if err != nil || len(locators) != 1 {
		t.Fatal("large approval cannot be packed into an exact plan carrier", err)
	}
	cfg := campaignMetadataConfigTestV2(t)
	limits, err := campaignEvidenceLimitsForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	bundleRaw, err := readCampaignEvidenceFileWithLimitsV2(runRoot, locators[0].URI, limits, false)
	if err != nil || uint64(len(bundleRaw)) != locators[0].SizeBytes {
		t.Fatal("persisted plan carrier failed typed readback", err)
	}
	bundle, err := decodeFinalCollectedFileBundle(bundleRaw)
	if err != nil || len(bundle.Files) != 1 || !bytes.Equal(bundle.Files[0].Data, raw) {
		t.Fatal("closed plan bytes differ after carrier replay", err)
	}
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	runId := "synthetic-capacity-run"
	file := campaignEvidenceFilePayload{Schema: campaignEvidenceFileSchema, RunID: runId, Scope: "run", Path: locators[0].URI, ContentHash: bytesSHA256(bundleRaw), Size: uint64(len(bundleRaw)), Data: bundleRaw}
	envelope, wire, err := prepareLocalEvidence(cfg, stateRoot, filepath.Join(stateRoot, "carrier.evidence.json"), campaignEvidenceFileKind, runId, file, roles.EVM["testnet-owner"], 0)
	if err != nil {
		t.Fatal("large plan carrier cannot be signed under its exact typed bound", err)
	}
	manifestEntry := campaignEvidenceFileEntry{Path: file.Path, ContentHash: file.ContentHash, Size: file.Size, EnvelopeHash: envelope.ContentHash}
	_, recovered, _, err := verifyPublicCampaignFileWireV2(limits, runId, "run", manifestEntry, wire)
	if err != nil || !bytes.Equal(recovered, bundleRaw) {
		t.Fatal("public carrier rejected the generated approval graph", err)
	}
	if err := verifyFinalPriorCarrierWireV2(cfg, runId, "run", manifestEntry, envelope.Signer, wire); err != nil {
		t.Fatal("prior-phase verification cannot retain the same plan carrier", err)
	}
	locator, err := persistFinalCollectedArtifactForConfigV2(cfg, runRoot, "setup-plan", "final-derived/setup-plan.json", raw)
	if err != nil {
		t.Fatal("derived source plan rejected captured approval", err)
	}
	loader, err := newFinalSemanticCampaignArtifactLoaderForConfigV2(cfg, stateRoot, runRoot)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := loadFinalV2Source(t.Context(), loader, locator, maximumSetupPlanFileBytes)
	if err != nil || !bytes.Equal(replayed, raw) {
		t.Fatal("validator plan source cannot be replayed", err)
	}
	lineage := finalFleetGenerationLineageArtifact{Schema: finalFleetGenerationLineageSchema, DeploymentID: plan.DeploymentID, PlanHash: plan.PlanHash,
		Files: []finalFleetGenerationLineageFile{{Path: "launch-foundation/plan.json", ContentHash: bytesSHA256(raw), SizeBytes: uint64(len(raw)), Data: raw}}}
	lineageRaw, err := json.Marshal(lineage)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateCampaignMetadataRawV2(limits, "final-derived/fleet-generation-lineage.json", lineageRaw); err != nil {
		t.Fatal("compound lineage rejected the same exact plan", err)
	}
	if err := validateCampaignMetadataRawV2(limits, "final-derived/ordinary-proof.json", lineageRaw); err == nil {
		t.Fatal("ordinary proof acquired compound lineage capacity")
	}
	entry.Path = "ordinary-proof.json"
	if _, err := finalCollectedBundleChunkRanges(t.Context(), "launch-foundation", []FinalCollectedFileBundleEntry{entry}, finalPlanBundleBytes("launch-foundation")); err == nil {
		t.Fatal("ordinary source used a plan-bearing bundle to exceed its own capacity")
	}
}

// A completed prior phase retains the original signed semantic carrier under
// a distinct typed name. Generic hashed files cannot acquire plan capacity.
func TestFinalPlanCapacityCompletedPriorCarrierRoundTrip(t *testing.T) {
	cfg := campaignMetadataConfigTestV2(t)
	limits, err := campaignEvidenceLimitsForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	_, raw := finalPlanCapacityWire(t, strings.Repeat("synthetic prior ", maximumCampaignEvidenceRawFileBytes/16+1))
	const path = "final-derived/setup-plan.json"
	const runId = "synthetic-prior-run"
	payload := finalSemanticSupplementFilePayload{Schema: finalSemanticSupplementFileSchema, RunID: runId, Path: path, ContentHash: bytesSHA256(raw), Size: uint64(len(raw)), Data: raw}
	envelope, err := signEvidence(cfg, finalSemanticSupplementFileKind, runId, payload, roles.EVM["testnet-owner"])
	if err != nil {
		t.Fatal(err)
	}
	wire, err := json.Marshal(envelope)
	if err != nil || len(wire) <= maximumCampaignEvidenceRawFileBytes {
		t.Fatal("prior carrier fixture did not cross its old source bound", err)
	}
	pathHash := sha256.Sum256([]byte(path))
	name := "final-inputs/prior-release/semantic-files/" + hex.EncodeToString(pathHash[:]) + ".plan.evidence.json"
	stateRoot := t.TempDir()
	runRoot := filepath.Join(stateRoot, "runs", "synthetic-successor")
	locator, err := persistFinalCollectedArtifactForConfigV2(cfg, runRoot, "prior-semantic-file-envelope", name, wire)
	if err != nil {
		t.Fatal("completed prior plan carrier cannot be retained", err)
	}
	loader, err := newFinalSemanticCampaignArtifactLoaderForConfigV2(cfg, stateRoot, runRoot)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := loader(t.Context(), locator)
	if err != nil || !bytes.Equal(loaded, wire) {
		t.Fatal("completed prior carrier changed through bounded replay", err)
	}
	if err := validateCampaignMetadataRawV2(limits, strings.Replace(name, ".plan.evidence.json", ".evidence.json", 1), wire); err == nil {
		t.Fatal("ordinary prior carrier acquired plan capacity")
	}
	if err := validateCampaignMetadataRawV2(limits, "final-inputs/prior-release/semantic-files/"+strings.Repeat("a1", 32)+".plan.evidence.json", wire); err == nil {
		t.Fatal("prior carrier accepted a different inner path hash")
	}
	changed := bytes.Replace(wire, []byte(envelope.Signature), []byte("0x"+strings.Repeat("00", 65)), 1)
	if err := validateCampaignMetadataRawV2(limits, name, changed); err == nil {
		t.Fatal("prior carrier accepted a changed signature")
	}
	payload.Path, payload.Data = "final-derived/ordinary.json", []byte("synthetic ordinary proof")
	payload.ContentHash, payload.Size = bytesSHA256(payload.Data), uint64(len(payload.Data))
	ordinaryEnvelope, err := signEvidence(cfg, finalSemanticSupplementFileKind, runId, payload, roles.EVM["testnet-owner"])
	if err != nil {
		t.Fatal(err)
	}
	ordinaryWire, err := json.Marshal(ordinaryEnvelope)
	if err != nil {
		t.Fatal(err)
	}
	ordinaryHash := sha256.Sum256([]byte(payload.Path))
	if err := validateCampaignMetadataRawV2(limits, "final-inputs/prior-release/semantic-files/"+hex.EncodeToString(ordinaryHash[:])+".plan.evidence.json", ordinaryWire); err == nil {
		t.Fatal("small ordinary proof borrowed the plan aggregate through a typed wrapper")
	}
}

// Unsubmitted reviews and the separately captured active plan are not sources.
// Missing, swapped and duplicate approved ancestors remain hard failures.
func TestFinalPlanCapacityCaptureSelectsExactApprovedAncestors(t *testing.T) {
	ancestor, original := finalPlanCapacityWire(t, "approved ancestor")
	active, activeRaw := finalPlanCapacityWire(t, "active source", ancestor.PlanHash)
	other, otherRaw := finalPlanCapacityWire(t, "unsubmitted review", ancestor.PlanHash)
	stateRoot := t.TempDir()
	for _, item := range []struct {
		plan *SetupPlan
		raw  []byte
	}{{plan: ancestor, raw: original}, {plan: active, raw: activeRaw}, {plan: other, raw: otherRaw}} {
		if _, err := archiveReviewedSetupPlanBytes(stateRoot, item.plan.PlanHash, item.raw); err != nil {
			t.Fatal(err)
		}
	}
	foundation := []FinalCollectedFileBundleEntry{finalCaptureCapacityEntry("plan.json", activeRaw)}
	entries, err := finalCollectedPlanHistoryEntries(stateRoot, foundation)
	if err != nil || len(entries) != 1 || entries[0].Path != stringsTrim0x(ancestor.PlanHash)+".json" || !bytes.Equal(entries[0].Data, original) {
		t.Fatal("capture mixed approved ancestry with ambient reviews", err)
	}
	path := filepath.Join(stateRoot, "plans", entries[0].Path)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := finalCollectedPlanHistoryEntries(stateRoot, foundation); err == nil {
		t.Fatal("missing approved ancestor was omitted")
	}
	if err := os.WriteFile(path, otherRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := finalCollectedPlanHistoryEntries(stateRoot, foundation); err == nil {
		t.Fatal("valid foreign approval replaced a required ancestor")
	}
	_, duplicated := finalPlanCapacityWire(t, "duplicate ancestry", ancestor.PlanHash, ancestor.PlanHash)
	if _, err := finalCollectedPlanHistoryEntries(stateRoot, []FinalCollectedFileBundleEntry{finalCaptureCapacityEntry("plan.json", duplicated)}); err == nil {
		t.Fatal("duplicate approved ancestors were accepted")
	}
	if entries, err := finalCollectedPlanHistoryEntries(t.TempDir(), []FinalCollectedFileBundleEntry{finalCaptureCapacityEntry("plan.json", original)}); err != nil || len(entries) != 0 {
		t.Fatal("an initial approval required a nonexistent history directory", err)
	}
}

// All larger paths are exact producer names; ordinary and approximate paths
// keep the proof ceiling. Declared sizes test large limits without allocation.
func TestFinalPlanCapacityTypedPathsAndEnvelopeBounds(t *testing.T) {
	limits, err := campaignEvidenceLimitsForConfig(campaignMetadataConfigTestV2(t))
	if err != nil {
		t.Fatal(err)
	}
	hash := strings.Repeat("ac", 32)
	for _, name := range []string{"final-derived/setup-plan.json", "final-derived/validator-activation-plan-" + hash + ".json", "final-derived/historical-coordinator/plans/" + hash + ".json", "final-derived/fleet-generation/renewal-12-approval.json", "final-derived/fleet-generation-lineage.json", "final-derived/fleet-lifecycle-lineage.json", "final-inputs/bundles/launch-foundation.json", "final-inputs/bundles/plan-history-002-of-012.json", "final-inputs/prior-release/semantic-files/" + hash + ".plan.evidence.json"} {
		maximum := limits.rawFileBytes(name)
		if maximum <= maximumCampaignEvidenceRawFileBytes {
			t.Fatalf("typed plan path lacks its source capacity: %s", name)
		}
		encoded, err := limits.fileEnvelopeBytes(name, maximum)
		if err != nil || uint64(encoded) > limits.maximumEnvelopeBytes() {
			t.Fatalf("typed plan carrier cannot pass its public envelope owner: %s %v", name, err)
		}
		if _, err := limits.fileEnvelopeBytes(name, maximum+1); err == nil {
			t.Fatalf("one-over typed plan source accepted: %s", name)
		}
	}
	for _, name := range []string{"final-derived/ordinary.json", "final-derived/setup-plan.json/extra", "final-derived/validator-activation-plan-" + strings.ToUpper(hash) + ".json", "final-derived/historical-coordinator/plans/nested/" + hash + ".json", "final-derived/fleet-generation/renewal-01-approval.json", "final-inputs/bundles/launch-foundation-extra.json", "final-inputs/bundles/plan-history-0-of-2.json", "final-inputs/bundles/plan-history-003-of-002.json", "final-inputs/bundles/public.json", "final-inputs/prior-release/semantic-files/" + hash + ".evidence.json", "final-inputs/prior-release/semantic-files/" + strings.ToUpper(hash) + ".plan.evidence.json"} {
		if limits.rawFileBytes(name) != maximumCampaignEvidenceRawFileBytes {
			t.Fatalf("approximate producer path borrowed plan capacity: %s", name)
		}
	}
}

// Typed plan storage cannot fund ordinary output, borrow a larger configured
// document grant, or grow beyond either its own counter or the combined grant.
func TestFinalPlanCapacityIndependentRetentionAndGrant(t *testing.T) {
	limits, err := campaignEvidenceLimitsForConfig(campaignMetadataConfigTestV2(t))
	if err != nil {
		t.Fatal(err)
	}
	metadata := *limits.metadata
	metadata.capacity = &campaignMetadataCapacityConfig{MaximumDocumentBytes: maximumFinalPlanLineageBytes, MaximumRetainedBytes: 16 * 1024 * 1024 * 1024, MaximumSupplementBytes: 16 * 1024 * 1024 * 1024}
	limits.metadata = &metadata
	limits.maximumBytes = 32 * 1024 * 1024 * 1024
	var budget finalPlanRetentionBudget
	for _, name := range []string{"final-derived/fleet-generation-lineage.json", "final-derived/fleet-lifecycle-lineage.json"} {
		if err := budget.admit(limits, name, maximumFinalPlanLineageBytes, true); err != nil {
			t.Fatal("exact compound plan owner refused", err)
		}
	}
	for index := 0; index < 8; index++ {
		if err := budget.admit(limits, fmt.Sprintf("final-derived/ordinary-%d.json", index), maximumCampaignEvidenceRawFileBytes, true); err != nil {
			t.Fatal("plan bytes consumed the independent ordinary allowance", err)
		}
	}
	before := budget
	for _, name := range []string{"final-derived/setup-plan.json", "final-derived/ordinary-extra.json"} {
		if err := budget.admit(limits, name, 1, true); err == nil || budget != before {
			t.Fatal("overage borrowed an independent owner or changed counters", name, err)
		}
	}
	metadata.capacity.MaximumSupplementBytes = metadata.supplementBytes
	budget = finalPlanRetentionBudget{planBytes: metadata.supplementBytes - 1}
	if err := budget.admit(limits, "final-derived/setup-plan.json", 1, true); err != nil {
		t.Fatal("exact configured combined grant refused", err)
	}
	before = budget
	if err := budget.admit(limits, "final-derived/ordinary-extra.json", 1, true); err == nil || budget != before {
		t.Fatal("combined configured grant silently increased", err)
	}
	metadata.capacity.MaximumDocumentBytes = metadata.indexBytes
	if got := limits.rawFileBytes("final-derived/fleet-generation-lineage.json"); got != min(metadata.indexBytes, maximumFinalPlanLineageBytes) {
		t.Fatal("typed lineage enlarged the configured document grant", got)
	}
}
