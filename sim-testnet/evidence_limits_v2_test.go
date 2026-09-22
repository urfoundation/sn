// Admission tests use declared lengths/counts and tiny exact objects. The
// multi-gigabyte full-population envelope never becomes a test allocation.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Decode the actual launch file. Geometry is a nominal 12-second planning
// envelope; enforced disk/history/count ceilings remain the capture authority.
func TestCampaignEvidenceCapacityV2DerivesFullPopulationPhaseBounds(t *testing.T) {
	cfg := runtimeEvidenceLaunchConfigTest(t)
	if cfg.Config.Topology.Miners != 1000 || cfg.Config.Topology.HeadSlots != 200 || cfg.Config.Topology.Validators != 2 || cfg.Config.Topology.Operators != 2 || cfg.Policy.Verify.TrailDepth != 8 || cfg.Policy.Verify.HardSeedPerMinutePerSource != 40 {
		t.Fatal("full-population launch geometry changed")
	}
	limits, err := campaignEvidenceLimitsForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	owners := uint64(cfg.Config.Topology.Validators * cfg.Config.Topology.Operators)
	seedRate := uint64(cfg.Policy.Verify.HardSeedPerMinutePerSource)
	blockSeconds := uint64(cfg.Public.Chain.ExpectedBlockSeconds)
	for _, phase := range []struct {
		name   string
		blocks uint64
		trails uint64
		rows   uint64
	}{
		{name: "release", blocks: 5 * 300, trails: 12000, rows: 96000},
		{name: "production-increment", blocks: 3 * 360, trails: 8640, rows: 69120},
		{name: "production-prefix", blocks: 5*300 + 3*360, trails: 20640, rows: 165120},
	} {
		trails := phase.blocks * blockSeconds * seedRate / 60
		rows := trails * uint64(cfg.Policy.Verify.TrailDepth)
		if trails != phase.trails || rows != phase.rows {
			t.Fatalf("%s nominal workload: %d trails %d rows", phase.name, trails, rows)
		}
		var hardBytes uint64
		for _, configured := range cfg.Config.ValidatorEvidenceV2 {
			bounds := configured.Evidence.Bounds
			if trails > bounds.Disk.MaxTrailCount || rows > bounds.Disk.MaxRecordCount {
				t.Fatalf("%s count ceiling cannot retain the source prefix", phase.name)
			}
			sourceBytes := rows*bounds.Disk.MaxRecordBytes + trails*bounds.Replay.MaxProofBytes
			hardBytes += uint64(len(configured.Evidence.Operators)) * sourceBytes
		}
		if hardBytes <= maximumCampaignEvidenceAggregateBytes || hardBytes > limits.maximumBytes {
			t.Fatalf("%s bounded arbitrary-wire workload %d is not admitted by the explicit capacity %d", phase.name, hardBytes, limits.maximumBytes)
		}
		t.Logf("%s: owners=%d nominal trails/source=%d, rows/source=%d, hard row/proof bytes=%d, archive ceiling=%d; no body allocation", phase.name, owners, trails, rows, hardBytes, limits.maximumBytes)
	}
	if limits.maximumBytes != 384*1024*1024*1024+maximumCampaignEvidenceAggregateBytes || limits.maximumObjects != 8531968 {
		t.Fatalf("cfg-derived hard owner census drifted: %+v", limits)
	}
	metadata, err := campaignEvidenceMetadataForConfigV2(cfg)
	if err != nil || metadata.maximumBytes != limits.metadata.graphBytes || metadata.maximumBytes <= cfg.Config.ValidatorEvidenceV2[0].Evidence.Bounds.MaxControlBytes || metadata.maximumBytes > cfg.Config.EvidenceArchiveMetadata.MaximumDocumentBytes {
		t.Fatalf("configured graph census lacks its independent finite metadata owner: %+v %v", metadata, err)
	}
}

// Default callers still refuse the same ninth maximum-sized raw object. An
// explicit checked cfg-derived allowance admits the metadata without bodies.
func TestCampaignEvidenceCapacityV2PreservesDefaultAndExactByteCeilings(t *testing.T) {
	cfg := runtimeEvidenceLaunchConfigTest(t)
	limits, err := campaignEvidenceLimitsForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	entries := make([]campaignEvidenceFileEntry, 9)
	for index := range entries {
		entries[index] = campaignEvidenceFileEntry{Path: fmt.Sprintf("source-%02d.bin", index), Size: maximumCampaignEvidenceRawFileBytes, ContentHash: "sha256:" + strings.Repeat("11", 32), EnvelopeHash: "sha256:" + strings.Repeat("22", 32)}
	}
	if _, err := campaignEvidenceManifestFiles(entries); err == nil {
		t.Fatal("legacy aggregate ceiling widened")
	}
	if _, err := campaignEvidenceManifestFilesWithLimits(entries, limits); err != nil {
		t.Fatal(err)
	}
	exact := campaignEvidenceLimits{maximumBytes: 9 * maximumCampaignEvidenceRawFileBytes, maximumObjects: 9}
	if _, err := campaignEvidenceManifestFilesWithLimits(entries, exact); err != nil {
		t.Fatal(err)
	}
	exact.maximumBytes--
	if _, err := campaignEvidenceManifestFilesWithLimits(entries, exact); err == nil {
		t.Fatal("one-over declared aggregate was admitted")
	}
	exact.maximumBytes = 10 * maximumCampaignEvidenceRawFileBytes
	entries[0].Size++
	if _, err := campaignEvidenceManifestFilesWithLimits(entries, exact); err == nil {
		t.Fatal("explicit aggregate allowance widened the individual raw-file cap")
	}
}

// Actual source locators count their existing run object once, but retain a
// separate edge/provenance identity and require exact matching file metadata.
func TestCampaignEvidenceCapacityV2ExactObjectCensusDeduplicatesRunReferences(t *testing.T) {
	limits := campaignEvidenceLimits{maximumBytes: 10, maximumObjects: 2}
	files := map[string]string{"first.bin": "sha256:" + strings.Repeat("11", 32), "second.bin": "sha256:" + strings.Repeat("22", 32)}
	references := map[string]campaignArtifactReference{"first.bin": {Kind: "source", URI: "first.bin", ContentHash: files["first.bin"], Size: 1}}
	if err := validateCampaignArtifactObjectCountWithLimits(files, references, limits); err != nil {
		t.Fatal(err)
	}
	references["external.bin"] = campaignArtifactReference{Kind: "source", URI: "external.bin", ContentHash: files["first.bin"], Size: 1}
	if err := validateCampaignArtifactObjectCountWithLimits(files, references, limits); err == nil {
		t.Fatal("one-over external source hid behind equal bytes")
	}
	queue, err := newCampaignArtifactQueueV2(files, limits)
	if err != nil {
		t.Fatal(err)
	}
	if err := queue.admit(map[string]bool{"second.bin": true, "first.bin": true}); err != nil {
		t.Fatal(err)
	}
	if err := queue.admit(map[string]bool{"external.bin": true}); err == nil || queue.objects != 2 || len(queue.known) != 2 {
		t.Fatal("one-over incremental census was not an atomic refusal")
	}
	for _, expected := range []string{"first.bin", "second.bin"} {
		if name, found := queue.next(); !found || name != expected {
			t.Fatalf("incremental exact traversal: %q %t, want %q", name, found, expected)
		}
		if err := queue.admit(map[string]bool{expected: true}); err != nil {
			t.Fatal(err)
		}
	}
	if _, found := queue.next(); found {
		t.Fatal("already consumed original was requeued by a second source")
	}
	edges := map[string]map[string]bool{}
	for depth := 0; depth < maximumCampaignEvidenceJSONDepth; depth++ {
		edges[fmt.Sprintf("node-%03d", depth)] = map[string]bool{fmt.Sprintf("node-%03d", depth+1): true}
	}
	if err := validateCampaignArtifactGraph(edges); err != nil {
		t.Fatalf("exact existing graph-depth ceiling changed: %v", err)
	}
	edges[fmt.Sprintf("node-%03d", maximumCampaignEvidenceJSONDepth)] = map[string]bool{"one-over": true}
	if err := validateCampaignArtifactGraph(edges); err == nil {
		t.Fatal("expanded object admission widened the recursive graph-depth limit")
	}
}

// Overflow never wraps into a small allocation, and candidates cannot obtain
// capacity by changing a claimed archive field or omitting an original owner.
func TestCampaignEvidenceCapacityV2RejectsOverflowAndIncompleteOwnerCensus(t *testing.T) {
	cfg := runtimeEvidenceLaunchConfigTest(t)
	original := cfg.Config.ValidatorEvidenceV2[0].Evidence.Bounds
	for _, mutate := range []func(){
		func() { cfg.Config.ValidatorEvidenceV2[0].Evidence.Bounds.Disk.MaxStorageBytes = math.MaxUint64 },
		func() { cfg.Config.ValidatorEvidenceV2[0].Evidence.Bounds.MaxHistoryBytes = math.MaxUint64 },
		func() { cfg.Config.ValidatorEvidenceV2[0].Evidence.Bounds.Disk.MaxStorageFiles = math.MaxUint64 },
		func() { cfg.Config.ValidatorEvidenceV2[0].Evidence.Bounds.MaxHeadEntries = math.MaxUint64 },
	} {
		cfg.Config.ValidatorEvidenceV2[0].Evidence.Bounds = original
		mutate()
		if _, err := campaignEvidenceLimitsForConfig(cfg); err == nil {
			t.Fatal("overflowed configured owner was admitted")
		}
	}
	cfg.Config.ValidatorEvidenceV2[0].Evidence.Bounds = original
	cfg.Config.ValidatorEvidenceV2[1].ValidatorID = cfg.Config.ValidatorEvidenceV2[0].ValidatorID
	if _, err := campaignEvidenceLimitsForConfig(cfg); err == nil {
		t.Fatal("duplicate configured validator redefined capacity")
	}
}

// Force the precise final metadata byte, then an independent new source edge.
// Refusal leaves the owner unchanged and never borrows data-tape allowance.
func TestCampaignEvidenceCapacityV2MetadataHasIndependentExactCeiling(t *testing.T) {
	limits := campaignEvidenceLimits{maximumBytes: 1024, maximumObjects: 3}
	locator := FinalArtifactLocator{Kind: "source", URI: "source.bin", ContentHash: "sha256:" + strings.Repeat("33", 32), SizeBytes: 1}
	raw, err := json.Marshal(locator)
	if err != nil {
		t.Fatal(err)
	}
	exact := uint64(64 + len(locator.Kind) + len(locator.URI) + len(locator.ContentHash) + 32 + len("root.json") + len(locator.URI))
	budget := &campaignEvidenceMetadataBudgetV2{maximumBytes: exact}
	references := map[string]campaignArtifactReference{}
	edges := map[string]map[string]bool{}
	if err := mergeCampaignArtifactSourceWithBudgetV2(references, edges, "root.json", raw, limits, budget); err != nil || budget.usedBytes != exact {
		t.Fatalf("exact metadata debit: %+v %v", budget, err)
	}
	if err := mergeCampaignArtifactSourceWithBudgetV2(references, edges, "root.json", raw, limits, budget); err != nil || budget.usedBytes != exact {
		t.Fatal("identical reference edge consumed another metadata debit")
	}
	if err := mergeCampaignArtifactSourceWithBudgetV2(references, edges, "next.json", raw, limits, budget); err == nil || budget.usedBytes != exact || len(edges) != 1 {
		t.Fatal("new edge escaped the independent metadata ceiling")
	}
}

// A carrier route query is still the complete raw hash for generic locators;
// only the typed prior census may distinguish an inner envelope route hash.
func TestCampaignEvidenceCapacityV2DoesNotRelaxGenericHashQueries(t *testing.T) {
	locator := FinalArtifactLocator{Kind: "source", URI: "https://no1.example/sn/evidence?hash=sha256:" + strings.Repeat("11", 32), ContentHash: "sha256:" + strings.Repeat("22", 32), SizeBytes: 1}
	raw, err := json.Marshal(locator)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := campaignArtifactReferencesWithLimits(map[string][]byte{"root.json": raw}, campaignEvidenceLimits{maximumBytes: 1024, maximumObjects: 8}); err == nil {
		t.Fatal("generic route hash was excused by typed carrier support")
	}
}

// An actual overlong sparse file is rejected from its descriptor size, before
// any corpus-sized allocation. Ordinary small raw bytes keep their exact hash.
func TestCampaignEvidenceCapacityV2HashCensusBoundsActualFiles(t *testing.T) {
	cfg := runtimeEvidenceLaunchConfigTest(t)
	root := t.TempDir()
	raw := []byte("owned original\n")
	path := filepath.Join(root, "source.bin")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	hashes, err := evidenceFileHashesForConfigV2(t.Context(), cfg, root, 2)
	if err != nil || len(hashes) != 1 || hashes["source.bin"] != bytesSHA256(raw) {
		t.Fatalf("bounded original census: %v %v", hashes, err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	resizeErr := file.Truncate(maximumCampaignEvidenceRawFileBytes + 1)
	if err := errors.Join(resizeErr, file.Close()); err != nil {
		t.Fatal(err)
	}
	if hashes, err := evidenceFileHashesForConfigV2(t.Context(), cfg, root, 2); hashes != nil || err == nil {
		t.Fatal("oversized actual file reached the completed hash census")
	}
	for _, name := range []string{"complete.json", campaignEvidenceManifestFilename} {
		if err := os.WriteFile(filepath.Join(root, name), raw, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := readCampaignEvidenceRegularFile(root, name); err == nil {
			t.Fatal("reserved control entered the ordinary archive path space")
		}
		if actual, err := readCampaignEvidenceControlFileV2(root, name); err != nil || string(actual) != string(raw) {
			t.Fatalf("exact original control refused its bounded private reader: %q %v", name, err)
		}
	}
}

// Every actual phase/analyzer entry receives its existing context; a cancelled
// owner refuses before inspecting state or searching prior completed runs.
func TestCampaignEvidenceCapacityV2CompletionReadersHonorCancelledOwner(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, read := range []func() error{
		func() error { _, err := evidenceFileHashesForConfigV2(ctx, nil, "", 0); return err },
		func() error {
			_, err := authenticateFinalSemanticOriginalClosureContext(ctx, nil, nil, "", "", nil)
			return err
		},
		func() error { _, err := authenticateFinalPriorCaptureV2Context(ctx, nil, "", nil); return err },
		func() error {
			_, err := validateScenarioCampaignCompleteContext(ctx, nil, nil, "", nil, "")
			return err
		},
		func() error { _, _, err := validateExactReleaseCampaignGateContext(ctx, nil, "", nil, nil); return err },
		func() error { _, _, err := loadCompletedScenarioCampaignContext(ctx, nil, "", nil, ""); return err },
		func() error {
			_, _, err := loadCompletedScenarioCampaignByRunIdContext(ctx, nil, "", nil, "", "")
			return err
		},
	} {
		if err := read(); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled owner began source discovery: %v", err)
		}
	}
}
