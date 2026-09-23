// Approved plans have a distinct finite owner through closed capture and
// public replay. Compound lineage documents may contain many exact ancestors;
// neither those documents nor plan carriers enlarge ordinary proof budgets.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	maximumFinalPlanBundleBytes              = ((maximumSetupPlanFileBytes + 2) / 3 * 4) + maximumCampaignFileEnvelopeOverhead
	maximumFinalPlanLineageBytes      uint64 = 4 * 1024 * 1024 * 1024
	maximumFinalPlanRetentionBytes    uint64 = 8 * 1024 * 1024 * 1024
	maximumFinalPriorPlanCarrierBytes        = ((maximumFinalPlanLineageBytes + 2) / 3 * 4) + maximumCampaignFileEnvelopeOverhead
)

// Hash-bearing paths admit exactly one lowercase canonical digest component.
func finalPlanHashFile(name, prefix string) bool {
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".json") {
		return false
	}
	digest := strings.TrimSuffix(strings.TrimPrefix(name, prefix), ".json")
	return len(digest) == 64 && digest == strings.ToLower(digest) && validCanonicalHashHex("0x"+digest)
}

// Larger carriers use only the producer's exact class or numbered chunk form.
func finalPlanBundleClass(name string) string {
	for _, class := range []string{"launch-foundation", "plan-history"} {
		if name == class {
			return class
		}
		if !strings.HasPrefix(name, class+"-") {
			continue
		}
		parts := strings.Split(strings.TrimPrefix(name, class+"-"), "-of-")
		if len(parts) != 2 {
			continue
		}
		index, indexErr := strconv.ParseUint(parts[0], 10, 64)
		count, countErr := strconv.ParseUint(parts[1], 10, 64)
		if indexErr == nil && countErr == nil && index > 0 && index <= count && count >= 2 && name == fmt.Sprintf("%s-%03d-of-%03d", class, index, count) {
			return class
		}
	}
	return ""
}

// Raw plan capacity belongs only to these exact source slots inside a bundle.
func finalPlanBundleSourceBytes(name, source string) uint64 {
	class := finalPlanBundleClass(name)
	if class == "launch-foundation" && source == "plan.json" || class == "plan-history" && finalPlanHashFile(source, "") {
		return maximumSetupPlanFileBytes
	}
	return finalCollectedBundleMaximumRawBytes
}

// A plan carrier has room for one full encoded approval plus finite metadata.
// Packing may combine smaller entries but still enforces each source's owner.
func finalPlanBundleBytes(name string) uint64 {
	if finalPlanBundleClass(name) != "" {
		return maximumFinalPlanBundleBytes
	}
	return maximumCampaignEvidenceRawFileBytes
}

// Zero means the ordinary proof owner applies. Prefixes alone never grant a
// larger file; every producer path is either exact or has canonical fields.
func finalPlanArtifactBytes(name string) uint64 {
	switch name {
	case "final-derived/setup-plan.json":
		return maximumSetupPlanFileBytes
	case "final-derived/fleet-generation-lineage.json", "final-derived/fleet-lifecycle-lineage.json":
		return maximumFinalPlanLineageBytes
	}
	if finalPlanHashFile(name, "final-derived/historical-coordinator/plans/") || finalPlanHashFile(name, "final-derived/validator-activation-plan-") {
		return maximumSetupPlanFileBytes
	}
	if finalPriorPlanCarrierPath(name) {
		return maximumFinalPriorPlanCarrierBytes
	}
	const renewalPrefix = "final-derived/fleet-generation/renewal-"
	const renewalSuffix = "-approval.json"
	if strings.HasPrefix(name, renewalPrefix) && strings.HasSuffix(name, renewalSuffix) {
		round, err := strconv.ParseUint(strings.TrimSuffix(strings.TrimPrefix(name, renewalPrefix), renewalSuffix), 10, 64)
		if err == nil && round > 0 && name == fmt.Sprintf("%s%d%s", renewalPrefix, round, renewalSuffix) {
			return maximumSetupPlanFileBytes
		}
	}
	const bundlePrefix = "final-inputs/bundles/"
	if strings.HasPrefix(name, bundlePrefix) && strings.HasSuffix(name, ".json") {
		bundle := strings.TrimSuffix(strings.TrimPrefix(name, bundlePrefix), ".json")
		if finalPlanBundleClass(bundle) != "" {
			return maximumFinalPlanBundleBytes
		}
	}
	return 0
}

// A completed prior phase stores an exact signed file envelope, addressed by
// the hash of its inner source path. Its body must also prove that routing.
func finalPriorPlanCarrierPath(name string) bool {
	const prefix = "final-inputs/prior-release/semantic-files/"
	const suffix = ".plan.evidence.json"
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
		return false
	}
	digest := strings.TrimSuffix(strings.TrimPrefix(name, prefix), suffix)
	return len(digest) == 64 && digest == strings.ToLower(digest) && validCanonicalHashHex("0x"+digest)
}

// The implementation ceiling cannot enlarge an explicitly approved metadata
// grant. Legacy configurations keep their original outer archive authority.
func (self campaignEvidenceLimits) finalPlanDocumentBytes(maximum uint64) uint64 {
	maximum = min(maximum, self.maximumBytes)
	if self.metadata != nil {
		maximum = min(maximum, self.metadata.capacity.limits().MaximumDocumentBytes)
	}
	return maximum
}

// Combined retention uses the grant, while each existing metadata forecast and
// the new plan counter remain independent. A clipped sum is not a new grant.
func (self campaignEvidenceLimits) finalPlanRetentionBytes(derived bool) uint64 {
	maximum := uint64(maximumCampaignEvidenceAggregateBytes)
	if self.metadata != nil {
		grant := self.metadata.capacity.limits()
		maximum = grant.MaximumRetainedBytes
		if derived {
			maximum = grant.MaximumSupplementBytes
		}
	}
	return min(maximum, self.maximumBytes)
}

// File names select capacity; the actual larger body must have that producer's
// schema and bounded source census. Semantic acceptance remains independent.
func validateFinalPlanArtifactBytes(limits campaignEvidenceLimits, name string, raw []byte) error {
	maximum := finalPlanArtifactBytes(name)
	if maximum == 0 || len(raw) <= maximumCampaignEvidenceRawFileBytes && !finalPriorPlanCarrierPath(name) {
		return nil
	}
	if uint64(len(raw)) > maximum {
		return errors.New("plan artifact exceeds its typed file capacity")
	}
	if finalPriorPlanCarrierPath(name) {
		var envelope ReleaseEvidenceEnvelope
		if err := decodeStrictJSONBytes(raw, &envelope); err != nil {
			return err
		}
		if envelope.Kind != finalSemanticSupplementFileKind {
			return errors.New("prior plan carrier has a different evidence kind")
		}
		if err := verifyEvidence(&envelope, nil); err != nil {
			return err
		}
		var payload finalSemanticSupplementFilePayload
		if err := decodeStrictJSONBytes(envelope.Payload, &payload); err != nil {
			return err
		}
		digest := sha256.Sum256([]byte(payload.Path))
		expected := "final-inputs/prior-release/semantic-files/" + hex.EncodeToString(digest[:]) + ".plan.evidence.json"
		if expected != name || payload.Schema != finalSemanticSupplementFileSchema || payload.RunID != envelope.RunID || validateFinalSemanticPostCapturePath(payload.Path) != nil || finalPlanArtifactBytes(payload.Path) == 0 || payload.Size != uint64(len(payload.Data)) || payload.ContentHash != bytesSHA256(payload.Data) {
			return errors.New("prior plan carrier differs from its exact inner source authority")
		}
		return validateCampaignMetadataRawV2(limits, payload.Path, payload.Data)
	}
	if strings.HasPrefix(name, "final-inputs/bundles/") {
		bundle, err := decodeFinalCollectedFileBundle(raw)
		if err != nil {
			return err
		}
		if name != "final-inputs/bundles/"+bundle.Name+".json" {
			return errors.New("plan carrier name differs from its exact producer path")
		}
		return nil
	}
	if maximum == maximumSetupPlanFileBytes {
		_, err := decodePersistedPlanWire(raw)
		return err
	}
	var lineage struct {
		Schema       string                            `json:"schema"`
		DeploymentId string                            `json:"deployment_id"`
		PlanHash     string                            `json:"plan_hash"`
		RunId        string                            `json:"run_id,omitempty"`
		Files        []finalFleetGenerationLineageFile `json:"files"`
	}
	if err := decodeStrictJSONBytes(raw, &lineage); err != nil {
		return err
	}
	if lineage.DeploymentId == "" || !validCanonicalHashHex(lineage.PlanHash) || len(lineage.Files) == 0 || uint64(len(lineage.Files)) > limits.maximumObjects {
		return errors.New("plan lineage artifact identity or source census is invalid")
	}
	if name == "final-derived/fleet-generation-lineage.json" && (lineage.Schema != finalFleetGenerationLineageSchema || lineage.RunId != "") || name == "final-derived/fleet-lifecycle-lineage.json" && (lineage.Schema != finalFleetLifecycleLineageSchema || lineage.RunId == "") {
		return errors.New("plan lineage artifact schema differs from its producer path")
	}
	previous := ""
	for _, source := range lineage.Files {
		if err := validateCampaignEvidencePath(source.Path); err != nil || source.Path <= previous || source.SizeBytes == 0 || source.SizeBytes != uint64(len(source.Data)) || source.ContentHash != bytesSHA256(source.Data) {
			return errors.New("plan lineage source is unsafe, unordered or differs from its committed bytes")
		}
		maximum := uint64(maximumCampaignEvidenceRawFileBytes)
		if source.Path == "launch-foundation/plan.json" || finalPlanHashFile(source.Path, "plan-history/") {
			maximum = maximumSetupPlanFileBytes
		}
		if source.SizeBytes > maximum {
			return errors.New("plan lineage source exceeds its independent file capacity")
		}
		previous = source.Path
	}
	return nil
}

// Separate accounting keeps the ordinary/metadata residual unchanged while
// plan artifacts remain bounded both independently and by the approved graph.
type finalPlanRetentionBudget struct {
	controlBytes  uint64
	ordinaryBytes uint64
	planBytes     uint64
}

// Admission is atomic: an overage changes none of the caller's counters.
func (self *finalPlanRetentionBudget) admit(limits campaignEvidenceLimits, name string, size uint64, derived bool) error {
	if err := limits.validate(); err != nil {
		return err
	}
	maximum := limits.finalPlanRetentionBytes(derived)
	if self.controlBytes > maximum || self.planBytes > maximum-self.controlBytes || size > maximum-self.controlBytes-self.planBytes {
		return errors.New("retained artifacts exceed the configured archive capacity")
	}
	if finalPlanArtifactBytes(name) != 0 {
		if self.planBytes > maximumFinalPlanRetentionBytes || size > maximumFinalPlanRetentionBytes-self.planBytes || size > limits.rawFileBytes(name) {
			return errors.New("retained plan artifacts exceed their independent capacity")
		}
		self.planBytes += size
		return nil
	}
	used, ordinary, err := admitCampaignMetadataRetentionV2(limits, name, size, derived, self.controlBytes, self.ordinaryBytes)
	if err != nil {
		return err
	}
	self.controlBytes, self.ordinaryBytes = used, ordinary
	return nil
}

// The approved graph names ancestors; the archive directory may also contain
// the active approval or unsubmitted reviews, which are not lineage inputs.
func finalCollectedPlanHistoryEntries(stateRoot string, foundation []FinalCollectedFileBundleEntry) ([]FinalCollectedFileBundleEntry, error) {
	var source []byte
	for _, entry := range foundation {
		if entry.Path == "plan.json" {
			if source != nil {
				return nil, errors.New("closed foundation has duplicate active plans")
			}
			source = entry.Data
		}
	}
	plan, err := decodePersistedPlanWire(source)
	if err != nil {
		return nil, fmt.Errorf("closed foundation approval: %w", err)
	}
	names := make([]string, 0, len(plan.PriorPlanHashes))
	seen := make(map[string]bool, len(plan.PriorPlanHashes))
	for _, hash := range plan.PriorPlanHashes {
		if !validCanonicalHashHex(hash) || hash == plan.PlanHash || seen[hash] {
			return nil, errors.New("closed foundation has an invalid or duplicate ancestor")
		}
		seen[hash] = true
		names = append(names, stringsTrim0x(hash)+".json")
	}
	entries, err := finalCollectedNamedEntriesWithReader(filepath.Join(stateRoot, "plans"), names, func(root, name string) (FinalCollectedFileBundleEntry, error) {
		entry, err := finalCollectedFileEntryWithLimit(root, name, maximumSetupPlanFileBytes)
		if err != nil {
			return FinalCollectedFileBundleEntry{}, err
		}
		ancestor, err := decodePersistedPlanWire(entry.Data)
		if err != nil || stringsTrim0x(ancestor.PlanHash)+".json" != name {
			return FinalCollectedFileBundleEntry{}, stateMismatchError(err, "closed ancestor differs from its exact approved path")
		}
		return entry, nil
	})
	return entries, err
}
