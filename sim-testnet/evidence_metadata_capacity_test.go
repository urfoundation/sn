// Explicit metadata grants cover the whole authenticated census, preserve
// legacy authority, and never turn a size sentinel into an admitted row.
package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// The slot/source product is retained in full, with twice its exact wire
// forecast. Granting space does not allocate bodies or change raw-source caps.
func TestCampaignMetadataCapacityTwofoldProfile(t *testing.T) {
	cfg := campaignMetadataSourceSuccessorTest(t, runtimeEvidenceLaunchConfigTest(t))
	limits, err := campaignEvidenceLimitsForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	metadata := limits.metadata
	index := 2 * (maximumCampaignEvidenceRawFileBytes + limits.maximumObjects*(metadata.sourceRowBytes+metadata.carrierRowBytes))
	if limits.maximumObjects != 8663040 || metadata.indexBytes != index || metadata.indexBytes <= 2*maximumCampaignMetadataDocumentV2 {
		t.Fatalf("full slot/source product lost its twofold byte owner: objects=%d metadata=%+v", limits.maximumObjects, metadata)
	}
	if limits.rawFileBytes("ordinary.bin") != maximumCampaignEvidenceRawFileBytes || metadata.intentBytes != maximumCampaignEvidenceEnvelopeBytes {
		t.Fatal("metadata grant enlarged ordinary raw evidence or intent authority")
	}
	granted := *cfg.Config.EvidenceArchiveMetadata
	exact := campaignMetadataCapacityConfig{MaximumDocumentBytes: max(metadata.indexBytes, metadata.manifestBytes, metadata.completionBytes, metadata.graphBytes), MaximumRetainedBytes: metadata.retainedBytes, MaximumSupplementBytes: metadata.supplementBytes}
	cfg.Config.EvidenceArchiveMetadata = &exact
	if _, err := campaignEvidenceLimitsForConfig(cfg); err != nil {
		t.Fatal("exact configured metadata grant failed", err)
	}
	for _, field := range []string{"index_bytes", "retained_bytes", "supplement_bytes"} {
		changed := exact
		switch field {
		case "index_bytes":
			changed.MaximumDocumentBytes--
		case "retained_bytes":
			changed.MaximumRetainedBytes--
		case "supplement_bytes":
			changed.MaximumSupplementBytes--
		}
		cfg.Config.EvidenceArchiveMetadata = &changed
		if _, err := campaignEvidenceLimitsForConfig(cfg); err == nil || !strings.Contains(err.Error(), field) {
			t.Fatalf("one-over %s was admitted: %v", field, err)
		}
	}
	if *metadata.capacity != granted {
		t.Fatal("changing a later config rewrote an existing metadata owner")
	}
	cfg.Config.EvidenceArchiveMetadata = nil
	if _, err := campaignEvidenceLimitsForConfig(cfg); err == nil || !strings.Contains(err.Error(), "1..2147483648") {
		t.Fatal("large slot census silently gained legacy document authority", err)
	}
	t.Logf("full v6 product: objects=%d index=%d manifest=%d completion=%d graph=%d retained=%d supplement=%d source-bytes=%d; grants document=%d retained=%d supplement=%d; no preallocation", limits.maximumObjects, metadata.indexBytes, metadata.manifestBytes, metadata.completionBytes, metadata.graphBytes, metadata.retainedBytes, metadata.supplementBytes, limits.maximumBytes, granted.MaximumDocumentBytes, granted.MaximumRetainedBytes, granted.MaximumSupplementBytes)
}

// Omitted grants retain old config bytes. Every finite grant is bound by the
// reviewed config hash; partial or overflowing grants are rejected outright.
func TestCampaignMetadataCapacityHasExplicitHashAuthority(t *testing.T) {
	cfg := runtimeEvidenceLaunchConfigTest(t)
	before, err := canonicalHashHex(cfg.Config)
	if err != nil {
		t.Fatal(err)
	}
	grant := *cfg.Config.EvidenceArchiveMetadata
	for _, field := range []string{"document", "retained", "supplement"} {
		changed := grant
		switch field {
		case "document":
			changed.MaximumDocumentBytes++
		case "retained":
			changed.MaximumRetainedBytes++
		case "supplement":
			changed.MaximumSupplementBytes++
		}
		cfg.Config.EvidenceArchiveMetadata = &changed
		after, err := canonicalHashHex(cfg.Config)
		if err != nil || after == before {
			t.Fatalf("%s capacity change reused its predecessor identity: %v", field, err)
		}
	}
	for _, invalid := range []campaignMetadataCapacityConfig{
		{}, {MaximumDocumentBytes: 1},
		{MaximumDocumentBytes: maximumCampaignMetadataDocumentCapacity + 1, MaximumRetainedBytes: 1, MaximumSupplementBytes: 1},
		{MaximumDocumentBytes: 1, MaximumRetainedBytes: ^uint64(0), MaximumSupplementBytes: 1},
		{MaximumDocumentBytes: 1, MaximumRetainedBytes: 1, MaximumSupplementBytes: ^uint64(0)},
	} {
		if err := invalid.validate(); err == nil {
			t.Fatalf("incomplete/unbounded grant gained authority: %+v", invalid)
		}
	}
	cfg.Config.EvidenceArchiveMetadata = nil
	raw, err := json.Marshal(cfg.Config)
	if err != nil || strings.Contains(string(raw), "evidence_archive_metadata") {
		t.Fatal("absent grant changed legacy config wire", err)
	}
}

// Force both exact and saturated widths at small bounds. The production
// sizer must inherit the real larger owner, never the old smaller sentinel.
func TestCampaignMetadataCapacityRowSaturationUsesItsOwner(t *testing.T) {
	value := strings.Repeat("<", 20)
	for _, maximum := range []uint64{19, 100, 120, 121, 24 * 1024 * 1024 * 1024} {
		if width := campaignMetadataStringContentBytesV2(value, maximum); width != min(uint64(120), maximum+1) {
			t.Fatalf("width undercounted after saturation: maximum=%d width=%d", maximum, width)
		}
	}
	sizer, err := newCampaignMetadataRowSizerV2(24 * 1024 * 1024 * 1024)
	if err != nil || sizer.maximumBytes != 24*1024*1024*1024 {
		t.Fatal("expanded document retained an obsolete smaller width sentinel", err)
	}
	row := FinalCollectedPriorCarrierV2{Path: value, LocalSuffix: []byte{1, 2, 3, 4}}
	raw, err := json.MarshalIndent(row, "          ", "  ")
	if err != nil {
		t.Fatal(err)
	}
	exact := uint64(len(raw) + 12)
	for _, maximum := range []uint64{1, exact - 1, exact, exact + 1} {
		sizer, err := newCampaignMetadataRowSizerV2(maximum)
		if err != nil || sizer.carrierBytes(row) != min(exact, maximum+1) {
			t.Fatalf("carrier exact/saturated boundary differs: maximum=%d error=%v", maximum, err)
		}
	}
	for _, maximum := range []uint64{0, maximumCampaignMetadataDocumentCapacity + 1, ^uint64(0)} {
		if _, err := newCampaignMetadataRowSizerV2(maximum); err == nil {
			t.Fatalf("invalid size authority %d reached row accounting", maximum)
		}
	}
}
