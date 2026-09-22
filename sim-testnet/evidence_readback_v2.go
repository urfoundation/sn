// Compact public readback owns one original carrier at a time. It proves
// replicated raw custody, never substitutes for the independent interpreter.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/ethereum/go-ethereum/common"
)

// Logical byte ownership is observable without relying on garbage-collector
// timing. It excludes bounded encoded envelopes and Go map/runtime overhead.
type campaignEvidenceReadbackV2 struct {
	controls             map[string][]byte
	sourceCount          uint64
	sourceBytes          uint64
	peakOriginalBytes    uint64
	retainedControlBytes uint64
	graphMetadataBytes   uint64
}

// Keep only bounded original control documents. Even an explicitly large tape
// allowance does not grant a correspondingly large retained in-memory map.
func campaignCaptureControlV2(name string) bool {
	switch name {
	case "result.json", "assertions.json", "anomalies.json", "adversaries.json", "analysis.json", scenarioCampaignStartFilename, scenarioLifecycleHandoffFilename, "final-inputs/manifest.json", finalSemanticCaptureStatusFilename:
		return true
	}
	return false
}

// The immutable all-origin reader remains the signed byte authority; only
// exact producer-shaped files avoid repeated general Json carrier decoding.
func (self *liveScenarioProbe) readPublicCampaignSourceV2(ctx context.Context, public *PublicDeploymentManifest, runId, ownerSigner, scope string, entry campaignEvidenceFileEntry) ([]byte, error) {
	return self.readPublicCampaignSourceWithObserverV2(ctx, public, runId, ownerSigner, scope, entry, nil)
}

// Observe the real per-call algorithm choice without replacing any reader,
// byte check or signature verdict. Production carries no observer or cache.
func (self *liveScenarioProbe) readPublicCampaignSourceWithObserverV2(ctx context.Context, public *PublicDeploymentManifest, runId, ownerSigner, scope string, entry campaignEvidenceFileEntry, observed func(bool)) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	limits, err := campaignEvidenceLimitsForConfig(self.cfg)
	if err != nil {
		return nil, err
	}
	limit, err := limits.fileEnvelopeBytes(entry.Path, entry.Size)
	if err != nil {
		return nil, err
	}
	var original []byte
	_, err = self.fetchReplicatedCampaignEnvelopeWithDecodeV2(ctx, public, entry.EnvelopeHash, campaignEvidenceFileKind, runId, ownerSigner, limit, limits, func(encoded []byte) (*ReleaseEvidenceEnvelope, error) {
		envelope, raw, canonical, err := verifyPublicCampaignFileWireV2(limits, runId, scope, entry, encoded)
		if observed != nil {
			observed(canonical)
		}
		if err != nil {
			return nil, err
		}
		original = raw
		return envelope, nil
	})
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return original, nil
}

// This is the same complete raw locator graph as legacy readback, but bodies
// are discarded after their exact replica/signature/hash/schema/edge checks.
func (self *liveScenarioProbe) streamPublicCampaignArchiveV2(ctx context.Context, public *PublicDeploymentManifest, ownerSigner string, manifest *campaignEvidenceManifestPayload) (*campaignEvidenceReadbackV2, error) {
	if ctx == nil || self == nil || manifest == nil {
		return nil, errors.New("compact public archive owner is absent")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	expectedOrigins, err := finalPriorCarrierOriginsV2(self.cfg)
	if err != nil {
		return nil, err
	}
	if public == nil || len(public.Operators) != 2 {
		return nil, errors.New("compact public archive origin census is incomplete")
	}
	for index, operator := range public.Operators {
		if operator.NoID != index+1 || operator.APIURL != expectedOrigins[index] {
			return nil, errors.New("compact public archive origin differs from approved configuration")
		}
	}
	limits, err := campaignEvidenceLimitsForConfig(self.cfg)
	if err != nil {
		return nil, err
	}
	metadata, err := campaignEvidenceMetadataForConfigV2(self.cfg)
	if err != nil {
		return nil, err
	}
	if _, _, err := finalPriorCarrierEntriesV2(manifest, limits); err != nil {
		return nil, err
	}
	files, err := campaignEvidenceManifestFilesWithLimits(manifest.Files, limits)
	if err != nil {
		return nil, err
	}
	type sourceEntry struct {
		scope string
		entry campaignEvidenceFileEntry
	}
	sources := map[string]sourceEntry{}
	var aggregate uint64
	for _, group := range []struct {
		scope   string
		entries []campaignEvidenceFileEntry
	}{{scope: "run", entries: manifest.Files}, {scope: "reference", entries: manifest.References}} {
		for _, entry := range group.entries {
			if _, found := sources[entry.Path]; found {
				return nil, errors.New("compact public source appears in multiple scopes")
			}
			if entry.Size > limits.maximumBytes-aggregate {
				return nil, errors.New("compact public graph exceeds its approved aggregate byte bound")
			}
			aggregate += entry.Size
			sources[entry.Path] = sourceEntry{scope: group.scope, entry: entry}
		}
	}
	controls := map[string][]byte{}
	wantedControls := map[string]bool{}
	// Only the already named current manifest can add exact prior control
	// locators. The remaining prior tape is represented by its typed census.
	if entry, found := sources["final-inputs/manifest.json"]; found {
		raw, err := self.readPublicCampaignSourceV2(ctx, public, manifest.RunID, ownerSigner, entry.scope, entry.entry)
		if err != nil {
			return nil, err
		}
		var collected FinalSemanticCollectedInputs
		if err := decodeStrictJSONBytes(raw, &collected); err != nil {
			return nil, err
		}
		if err := verifyFinalSemanticCollectedInputs(self.cfg, &collected); err != nil {
			return nil, err
		}
		if prior := collected.PriorPhase; prior != nil {
			for _, locator := range finalCollectedPriorLocators(prior) {
				wantedControls[locator.URI] = true
			}
		}
	}
	var retained finalPlanRetentionBudget
	var peakOriginalBytes uint64
	sourceCount := uint64(len(sources))
	references := map[string]campaignArtifactReference{}
	edges := map[string]map[string]bool{}
	queue, err := newCampaignArtifactQueueV2(files, limits)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(sources))
	for name := range sources {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		source := sources[name]
		raw, err := self.readPublicCampaignSourceV2(ctx, public, manifest.RunID, ownerSigner, source.scope, source.entry)
		if err != nil {
			return nil, err
		}
		peakOriginalBytes = max(peakOriginalBytes, uint64(len(raw)))
		if source.scope == "run" {
			if isFinalSemanticPostCapturePath(name) {
				return nil, errors.New("compact original archive contains a post-capture verdict")
			}
			if err := validateCampaignEvidenceJSONSchemas(map[string][]byte{name: raw}); err != nil {
				return nil, err
			}
		}
		if err := mergeCampaignArtifactSourceWithBudgetV2(references, edges, name, raw, limits, metadata); err != nil {
			return nil, err
		}
		if err := queue.admit(edges[name]); err != nil {
			return nil, err
		}
		if campaignCaptureControlV2(name) || wantedControls[name] {
			if err := retained.admit(limits, name, uint64(len(raw)), false); err != nil {
				return nil, err
			}
			controls[name] = raw
		}
	}
	allowedOrigins, err := campaignArtifactAllowedOrigins(public, self.publicManifestURI)
	if err != nil {
		return nil, err
	}
	expectedReferences := map[string]string{}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		name, found := queue.next()
		if !found {
			break
		}
		reference := references[name]
		if source, found := sources[name]; found {
			if source.entry.Size != reference.Size || source.entry.ContentHash != reference.ContentHash {
				return nil, errors.New("compact public locator differs from its original source")
			}
			if source.scope == "reference" {
				expectedReferences[name] = reference.ContentHash
			}
			continue
		}
		parsed, err := url.Parse(name)
		if err != nil || parsed.Scheme == "" {
			return nil, errors.Join(errors.New("compact public locator lacks its exact source"), err)
		}
		if err := validateCampaignArtifactOrigin(name, allowedOrigins); err != nil {
			return nil, err
		}
		if reference.Size > limits.maximumBytes-aggregate {
			return nil, errors.New("compact public external graph exceeds the approved byte bound")
		}
		raw, _, err := self.get(ctx, name, int64(reference.Size))
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if uint64(len(raw)) != reference.Size || bytesSHA256(raw) != reference.ContentHash {
			return nil, errors.New("compact public external locator bytes differ")
		}
		aggregate += uint64(len(raw))
		sourceCount++
		peakOriginalBytes = max(peakOriginalBytes, uint64(len(raw)))
		if err := mergeCampaignArtifactSourceWithBudgetV2(references, edges, name, raw, limits, metadata); err != nil {
			return nil, err
		}
		if err := queue.admit(edges[name]); err != nil {
			return nil, err
		}
	}
	if err := validateCampaignArtifactObjectCountWithLimits(files, references, limits); err != nil {
		return nil, err
	}
	if err := validateCampaignArtifactGraph(edges); err != nil {
		return nil, err
	}
	manifestReferences, err := campaignEvidenceEntryFilesWithLimits(manifest.References, limits)
	if err != nil || !stringMapsEqual(manifestReferences, expectedReferences) {
		return nil, errors.Join(errors.New("compact public manifest reference census differs"), err)
	}
	var metadataBytes uint64
	if metadata != nil {
		metadataBytes = metadata.usedBytes
	}
	return &campaignEvidenceReadbackV2{controls: controls, sourceCount: sourceCount, sourceBytes: aggregate, peakOriginalBytes: peakOriginalBytes, retainedControlBytes: retained.controlBytes + retained.planBytes, graphMetadataBytes: metadataBytes}, nil
}

// Missing interpretation is reported only after complete bounded custody
// checks. It cannot hide an actual source, signature, schema or readback error.
func (self *liveScenarioProbe) verifyPublicCampaignCaptureV2(ctx context.Context, public *PublicDeploymentManifest, ownerSigner string, complete *ReleaseEvidenceEnvelope, completion scenarioCompletePayload, bundle *ScenarioEvidenceBundle, manifest *campaignEvidenceManifestPayload) error {
	readback, err := self.streamPublicCampaignArchiveV2(ctx, public, ownerSigner, manifest)
	if err != nil {
		return err
	}
	controls := readback.controls
	limits, err := campaignEvidenceLimitsForConfig(self.cfg)
	if err != nil {
		return err
	}
	files, err := campaignEvidenceManifestFilesWithLimits(manifest.Files, limits)
	if err != nil {
		return err
	}
	for _, name := range []string{"result.json", "assertions.json", "anomalies.json", "adversaries.json", "analysis.json", "analysis.html", "junit.xml", "observations.jsonl", scenarioCampaignStartFilename, "final-inputs/manifest.json", finalSemanticCaptureStatusFilename} {
		if files[name] == "" {
			return fmt.Errorf("compact public required original %q is missing", name)
		}
		for _, entry := range manifest.Files {
			if entry.Path == name && entry.Size == 0 {
				return fmt.Errorf("compact public required original %q is empty", name)
			}
		}
	}
	if err := validateCampaignEvidenceControlSemantics(self.cfg, ownerSigner, controls, completion, bundle); err != nil {
		return err
	}
	if _, _, err := authenticatePublicFinalSemanticClosure(self.cfg, controls, bundle.Result); err != nil {
		return err
	}
	var collected FinalSemanticCollectedInputs
	if err := decodeStrictJSONBytes(controls["final-inputs/manifest.json"], &collected); err != nil {
		return err
	}
	if prior := collected.PriorPhase; prior != nil {
		loaded := map[string][]byte{}
		for _, locator := range finalCollectedPriorLocators(prior) {
			raw := controls[locator.URI]
			if uint64(len(raw)) != locator.SizeBytes || bytesSHA256(raw) != locator.ContentHash {
				return errors.New("compact prior control lacks its exact original bytes")
			}
			loaded[locator.Kind], loaded[locator.URI] = raw, raw
		}
		if err := verifyFinalCollectedPendingPriorBinding(self.cfg, &collected, controls); err != nil {
			return err
		}
		if err := verifyFinalCollectedPriorPhaseBytes(self.cfg, prior, loaded); err != nil {
			return err
		}
		var priorManifest ReleaseEvidenceEnvelope
		if err := decodeStrictJSONBytes(loaded[prior.EvidenceManifest.URI], &priorManifest); err != nil {
			return err
		}
		priorPayload, err := decodeCampaignEvidenceManifestWithLimits(&priorManifest, limits)
		if err != nil {
			return err
		}
		if priorManifest.Signer != complete.Signer || !strings.EqualFold(priorManifest.Signer.Hex(), ownerSigner) {
			return errors.New("compact prior original signer differs from the current completion")
		}
		read := func(ctx context.Context, origin, hash string, maximumBytes uint64) ([]byte, error) {
			raw, _, err := self.getCampaignEvidence(ctx, origin+"/sn/evidence?hash="+hash, campaignEvidenceFileKind, int64(maximumBytes), limits)
			return raw, err
		}
		if err := verifyFinalPriorCarriersV2(ctx, self.cfg, prior.RunID, priorPayload, prior.CarrierOrigins, prior.PublicCarriers, common.HexToAddress(ownerSigner), read); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return &finalSemanticAnalysisPendingError{}
}
