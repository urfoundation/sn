//go:build linux || darwin

// Source successors expand finite source counts without changing the size of
// any ordinary artifact or the identities of the original metadata owners.
package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// Resolve the same approved lifetime overlay as campaign startup without
// changing the immutable configuration or the existing relay slot template.
func campaignMetadataSourceSuccessorTest(t *testing.T, cfg *ResolvedConfig) *ResolvedConfig {
	t.Helper()
	before, err := canonicalHashHex(cfg.Config)
	if err != nil {
		t.Fatal(err)
	}
	continuation := &EvidenceRelayContinuation{Schema: evidenceRelayContinuationSourceExpansionSchema}
	for _, source := range cfg.Config.ValidatorEvidenceV2 {
		approved, err := doubledEvidenceRelaySourceBounds(source.Evidence.Bounds)
		if err != nil {
			t.Fatal(err)
		}
		continuation.SourceBounds = append(continuation.SourceBounds, evidenceRelaySourceBounds{ValidatorId: source.ValidatorID, Original: source.Evidence.Bounds, Approved: approved})
	}
	resolved, err := evidenceRelaySourceCapacityConfig(cfg, &SetupPlan{EvidenceRelayContinuation: continuation})
	if err != nil {
		t.Fatal(err)
	}
	after, err := canonicalHashHex(cfg.Config)
	if err != nil || after != before || resolved.ConfigHash != cfg.ConfigHash || resolved.Config.ValidatorEvidenceRelay != cfg.Config.ValidatorEvidenceRelay {
		t.Fatal("source resolution changed immutable configuration or relay funding", err)
	}
	return resolved
}

// The real launch shape plus the approved source doubling must remain
// representable by its aggregate metadata owners before starting a campaign.
func TestCampaignMetadataSourceSuccessorFitsApprovedLifetime(t *testing.T) {
	cfg := campaignMetadataSourceSuccessorTest(t, campaignMetadataConfigTestV2(t))
	limits, err := campaignEvidenceLimitsForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if limits.maximumObjects != 1323008 || limits.metadata.retainedBytes <= 4*1024*1024*1024 || limits.metadata.indexBytes > maximumCampaignMetadataDocumentV2 {
		t.Fatalf("approved source census no longer reproduces the old aggregate rejection: objects=%d metadata=%+v", limits.maximumObjects, limits.metadata)
	}
	if limits.rawFileBytes("ordinary.bin") != maximumCampaignEvidenceRawFileBytes || defaultCampaignEvidenceLimits().maximumBytes != maximumCampaignEvidenceAggregateBytes {
		t.Fatal("source metadata expansion changed ordinary artifact limits")
	}
	t.Logf("objects=%d index=%d manifest=%d completion=%d graph=%d retained=%d supplement=%d", limits.maximumObjects, limits.metadata.indexBytes, limits.metadata.manifestBytes, limits.metadata.completionBytes, limits.metadata.graphBytes, limits.metadata.retainedBytes, limits.metadata.supplementBytes)
}

// Expanded aggregate accounting never permits a document above its existing
// ceiling, including a larger template distinct from funded continuation slots.
func TestCampaignMetadataSourceSuccessorPreservesDocumentSlotBoundary(t *testing.T) {
	cfg := campaignMetadataSourceSuccessorTest(t, campaignMetadataConfigTestV2(t))
	var lastFitting uint64
	for slots := uint64(256); slots <= 2048; slots++ {
		cfg.Config.ValidatorEvidenceRelay.MaxSlots = slots
		limits, err := campaignEvidenceLimitsForConfig(cfg)
		if err != nil {
			if !strings.Contains(err.Error(), "index_bytes=") || !strings.Contains(err.Error(), "1..2147483648") || lastFitting == 0 {
				t.Fatalf("slot %d failed outside its finite document boundary: %v", slots, err)
			}
			break
		}
		if limits.metadata.indexBytes > maximumCampaignMetadataDocumentV2 {
			t.Fatalf("slot %d exceeded the admitted document ceiling: %d", slots, limits.metadata.indexBytes)
		}
		lastFitting = slots
	}
	if lastFitting <= 256 || lastFitting >= 2048 {
		t.Fatalf("expanded census lost a finite slot boundary: %d", lastFitting)
	}
	cfg.Config.ValidatorEvidenceRelay.MaxSlots = 2048
	if _, err := campaignEvidenceLimitsForConfig(cfg); err == nil || !strings.Contains(err.Error(), "index_bytes=") {
		t.Fatalf("oversized relay template escaped the document ceiling: %v", err)
	}
}

// Declared lengths exercise the real retention debit at its final byte. No
// multi-gigabyte file or allocation is needed to prove aggregate composition.
func TestCampaignMetadataAggregateOwnersComposeExactDocumentLimits(t *testing.T) {
	profile := campaignMetadataLimitsV2{
		indexBytes: maximumCampaignMetadataDocumentV2, manifestBytes: maximumCampaignMetadataDocumentV2,
		completionBytes: maximumCampaignMetadataDocumentV2, graphBytes: maximumCampaignMetadataDocumentV2,
		retainedBytes: maximumCampaignRetainedMetadataV2, supplementBytes: maximumCampaignSupplementMetadataV2,
		intentBytes: maximumCampaignEvidenceEnvelopeBytes, validators: 2, operators: 2,
	}
	limits := defaultCampaignEvidenceLimits()
	limits.metadata = &profile
	for _, derived := range []bool{false, true} {
		names := []string{campaignCollectedIndexPathV2, campaignPriorIndexPathV2, campaignPriorManifestPathV2, campaignPriorCompletionPathV2}
		maximum := profile.retainedBytes
		if derived {
			names = []string{campaignDerivedPriorManifestPathV2, campaignDerivedPriorCompletionPathV2}
			maximum = profile.supplementBytes
		}
		var used, ordinary uint64
		for _, name := range names {
			size := limits.rawFileBytes(name)
			var err error
			used, ordinary, err = admitCampaignMetadataRetentionV2(limits, name, size, derived, used, ordinary)
			if err != nil || ordinary != 0 {
				t.Fatalf("typed document %s lost its independent owner: %v", name, err)
			}
			if next, residual, err := admitCampaignMetadataRetentionV2(limits, name, size+1, derived, 0, 0); err == nil || next != 0 || residual != 0 {
				t.Fatalf("oversized document %s borrowed aggregate capacity: %v", name, err)
			}
		}
		for index := 0; index < 8; index++ {
			var err error
			used, ordinary, err = admitCampaignMetadataRetentionV2(limits, fmt.Sprintf("ordinary-%d.bin", index), maximumCampaignEvidenceRawFileBytes, derived, used, ordinary)
			if err != nil {
				t.Fatal("aggregate rejected a separately admitted ordinary residual", err)
			}
		}
		if used != maximum || ordinary != maximumCampaignEvidenceAggregateBytes {
			t.Fatalf("derived=%t aggregate did not equal its member ceilings: used=%d maximum=%d ordinary=%d", derived, used, maximum, ordinary)
		}
		for _, name := range []string{"ordinary-one-over.bin", names[0]} {
			if next, residual, err := admitCampaignMetadataRetentionV2(limits, name, 1, derived, used, ordinary); err == nil || next != used || residual != ordinary {
				t.Fatalf("derived=%t one-over aggregate mutated its counters: %v", derived, err)
			}
		}
	}
}

// Report each malformed owner in one pass, with the values needed to repair
// a profile; a zero census still fails independently from byte arithmetic.
func TestCampaignMetadataInvalidProfileReportsEveryBound(t *testing.T) {
	profile := campaignMetadataLimitsV2{
		indexBytes: maximumCampaignMetadataDocumentV2 + 1, manifestBytes: 0,
		completionBytes: maximumCampaignMetadataDocumentV2 + 1, graphBytes: 0,
		retainedBytes: maximumCampaignRetainedMetadataV2 + 1, supplementBytes: maximumCampaignSupplementMetadataV2 + 1,
		intentBytes: maximumCampaignEvidenceEnvelopeBytes + 1, validators: 0, operators: 3,
	}
	err := profile.validate()
	if err == nil {
		t.Fatal("malformed metadata profile passed")
	}
	for _, detail := range []string{"index_bytes=2147483649", "manifest_bytes=0", "completion_bytes=2147483649", "graph_bytes=0", fmt.Sprintf("retained_bytes=%d", maximumCampaignRetainedMetadataV2+1), fmt.Sprintf("supplement_bytes=%d", maximumCampaignSupplementMetadataV2+1), "intent_bytes=67108865", "validators=0 operators=3"} {
		if !strings.Contains(err.Error(), detail) {
			t.Errorf("profile diagnostic omitted %s: %v", detail, err)
		}
	}
}

// Both real scenario entrypoints must pass this metadata admission before
// touching preparation. Remaining runtime owners are deliberately absent.
func TestCampaignMetadataSourceSuccessorReachesScenarioOwners(t *testing.T) {
	cfg := campaignMetadataSourceSuccessorTest(t, runtimeEvidenceProvisionalSourceCapacityTest(t))
	for _, phase := range []string{"release-1.0", "production-soak"} {
		prepared := false
		result, err := runScenarioWithEvidenceRelay(t.Context(), cfg, "", scenarioDefinition{Name: phase}, nil, scenarioRunOptions{Prepare: func(context.Context) error { prepared = true; return nil }}, nil)
		if err == nil || err.Error() != "evidence relay runtime owners are incomplete" || result != nil || prepared {
			t.Fatalf("%s source successor stopped at metadata admission or changed preparation: result=%+v prepared=%t error=%v", phase, result, prepared, err)
		}
	}
	if cfg.provisionalResume.Record.FinalAcceptance {
		t.Fatal("provisional source admission granted final acceptance")
	}
}
