// Metadata grows with the admitted original census, not with tape bytes.
// Ordinary artifacts keep their original limits; only these typed controls
// can use the independently configured compact campaign allowance.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	validatorpkg "github.com/urfoundation/sn/v2026/validator"
)

const (
	// These are implementation admission ceilings, not source-byte grants or
	// allocations. Larger configurations require a separately reviewed format.
	maximumCampaignMetadataDocumentV2    = 2 * 1024 * 1024 * 1024
	maximumCampaignRetainedMetadataV2    = 4 * 1024 * 1024 * 1024
	campaignCollectedIndexPathV2         = "final-inputs/manifest.json"
	campaignPriorIndexPathV2             = "final-inputs/prior-release/collected-inputs-manifest.json.bin"
	campaignPriorManifestPathV2          = "final-inputs/prior-release/campaign-evidence-manifest.json.bin"
	campaignPriorCompletionPathV2        = "final-inputs/prior-release/complete.json.bin"
	campaignDerivedPriorManifestPathV2   = "final-derived/prior-release/campaign-evidence-manifest.json"
	campaignDerivedPriorCompletionPathV2 = "final-derived/prior-release/complete.json"
)

// All counters are logical/wire bounds. Parser/map/encoding overhead is
// additionally count-bounded and is measured separately during qualification.
type campaignMetadataLimitsV2 struct {
	indexBytes      uint64
	manifestBytes   uint64
	completionBytes uint64
	graphBytes      uint64
	retainedBytes   uint64
	supplementBytes uint64
	intentBytes     uint64
	validators      uint64
	operators       uint64
	sourceRowBytes  uint64
	carrierRowBytes uint64
}

// Exact current producer shapes give per-row wire geometry, including the
// deepest indentation, maximum-width integers, original hashes and origins.
// A legacy-sized residual separately owns all other controls/long paths.
func campaignMetadataLimitsForConfigV2(cfg *ResolvedConfig, objects uint64) (*campaignMetadataLimitsV2, error) {
	if !finalUsesEvidenceV2(cfg) {
		return nil, nil
	}
	if objects == 0 || len(cfg.OperatorAPIOrigins) != 2 || cfg.Config.Topology.Validators < 1 {
		return nil, errors.New("compact metadata has no complete configured census")
	}
	result := &campaignMetadataLimitsV2{validators: uint64(cfg.Config.Topology.Validators), operators: uint64(cfg.Config.Topology.Operators)}
	hash := "sha256:" + strings.Repeat("f", 64)
	sourcePath := "final-inputs/validators/v2/" + strings.Repeat("f", 64) + ".bin"
	maximum := ^uint64(0)
	origin := ""
	for _, candidate := range cfg.OperatorAPIOrigins {
		encoded, err := json.Marshal(candidate)
		if err != nil || candidate == "" {
			return nil, errors.Join(errors.New("compact metadata original origin is absent"), err)
		}
		previous, _ := json.Marshal(origin)
		if len(encoded) > len(previous) {
			origin = candidate
		}
	}
	source := FinalCollectedValidatorSourceV2{
		Source: validatorpkg.ReleaseEvidenceV2CaptureSource{
			Kind:   "relay-original-transaction",
			Name:   "evidence-deposit-audit-prepared/epoch-" + strings.Repeat("9", 20) + "-observation-" + strings.Repeat("9", 20) + "-native-" + strings.Repeat("9", 20) + ".json",
			Origin: origin,
		},
		Artifact: FinalArtifactLocator{Kind: "validator-evidence-v2-source", URI: sourcePath, ContentHash: hash, SizeBytes: maximum},
	}
	carrier := FinalCollectedPriorCarrierV2{Scope: "reference", Path: sourcePath, EnvelopeHash: hash, WireHash: hash, WireBytes: maximum, LocalHash: hash, LocalBytes: maximum, LocalSuffix: []byte{'\n'}}
	rowBytes := func(value any) (uint64, error) {
		raw, err := json.MarshalIndent(value, strings.Repeat(" ", 10), "  ")
		return uint64(len(raw)) + 12, err
	}
	var err error
	if result.sourceRowBytes, err = rowBytes(source); err != nil {
		return nil, err
	}
	if result.carrierRowBytes, err = rowBytes(carrier); err != nil {
		return nil, err
	}
	entry, err := json.Marshal(campaignEvidenceFileEntry{Path: sourcePath, ContentHash: hash, Size: maximum, EnvelopeHash: hash})
	if err != nil {
		return nil, err
	}
	pathWire, _ := json.Marshal(sourcePath)
	hashWire, _ := json.Marshal(hash)
	ref := campaignArtifactReference{Kind: source.Artifact.Kind, URI: sourcePath, ContentHash: hash, Size: maximum}
	graphRow := uint64(64 + len(ref.Kind) + len(ref.URI) + len(ref.ContentHash) + 32 + len(campaignCollectedIndexPathV2) + len(ref.URI))
	var arithmeticErr error
	add := func(values ...uint64) uint64 {
		var total uint64
		for _, value := range values {
			next, ok := checkedAdd(total, value)
			if !ok {
				arithmeticErr = errors.New("compact metadata geometry overflows")
				return 0
			}
			total = next
		}
		return total
	}
	rows := func(size uint64) uint64 {
		total, ok := checkedMul(objects, size)
		if !ok {
			arithmeticErr = errors.New("compact metadata census geometry overflows")
			return 0
		}
		return total
	}
	result.indexBytes = add(maximumCampaignEvidenceRawFileBytes, rows(add(result.sourceRowBytes, result.carrierRowBytes)))
	result.manifestBytes = add(maximumCampaignEvidenceRawFileBytes, rows(uint64(len(entry)+1)))
	result.completionBytes = add(maximumCampaignEvidenceRawFileBytes, rows(uint64(len(pathWire)+len(hashWire)+2)))
	result.graphBytes = add(maximumCampaignEvidenceEnvelopeBytes, rows(graphRow))
	// Current index, exact prior index, exact prior manifest/completion, and
	// the unchanged aggregate owner for all remaining retained controls.
	result.retainedBytes = add(maximumCampaignEvidenceAggregateBytes, result.indexBytes, result.indexBytes, result.manifestBytes, result.completionBytes, 2*maximumCampaignFileEnvelopeOverhead)
	// Final semantic output owns separate exact prior-control copies. This is
	// not part of the public capture's retained-control owner or an RSS bound.
	result.supplementBytes = add(maximumCampaignEvidenceAggregateBytes, result.manifestBytes, result.completionBytes, 2*maximumCampaignFileEnvelopeOverhead)
	for _, configured := range cfg.Config.ValidatorEvidenceV2 {
		bounds := configured.Evidence.Bounds
		if bounds.MaxControlBytes == 0 || bounds.MaxControlBytes > maximumCampaignEvidenceEnvelopeBytes || bounds.MaxHistoryBytes == 0 {
			return nil, errors.New("compact metadata original control owner is invalid")
		}
		result.intentBytes = max(result.intentBytes, min(bounds.MaxControlBytes, bounds.MaxHistoryBytes))
	}
	if arithmeticErr != nil {
		return nil, arithmeticErr
	}
	if err := result.validate(); err != nil {
		return nil, err
	}
	return result, nil
}

// Refuse profiles that cannot be represented within this finite implementation
// before any capture/publication owner is created. No cap is preallocated.
func (self *campaignMetadataLimitsV2) validate() error {
	if self == nil {
		return nil
	}
	for _, bound := range []uint64{self.indexBytes, self.manifestBytes, self.completionBytes, self.graphBytes} {
		if bound == 0 || bound > maximumCampaignMetadataDocumentV2 {
			return errors.New("compact metadata profile exceeds the reviewed document bound")
		}
	}
	if self.retainedBytes == 0 || self.retainedBytes > maximumCampaignRetainedMetadataV2 || self.supplementBytes == 0 || self.supplementBytes > maximumCampaignMetadataDocumentV2 || self.intentBytes == 0 || self.intentBytes > maximumCampaignEvidenceEnvelopeBytes || self.validators == 0 || self.operators != 2 {
		return errors.New("compact metadata retained/control profile is invalid")
	}
	return nil
}

// Only exact producer-owned control names can acquire a larger raw owner.
// Hash-addressed V2 files may hold the original intent-store control; a
// separate byte-shape check below refuses oversized ordinary proof bodies.
func (self campaignEvidenceLimits) rawFileBytes(name string) uint64 {
	if self.metadata == nil {
		return maximumCampaignEvidenceRawFileBytes
	}
	switch name {
	case campaignCollectedIndexPathV2, campaignPriorIndexPathV2:
		return self.metadata.indexBytes
	case campaignEvidenceManifestFilename, campaignPriorManifestPathV2, campaignDerivedPriorManifestPathV2:
		return self.metadata.manifestBytes + maximumCampaignFileEnvelopeOverhead
	case "complete.json", campaignPriorCompletionPathV2, campaignDerivedPriorCompletionPathV2:
		return self.metadata.completionBytes + maximumCampaignFileEnvelopeOverhead
	}
	for validatorId := uint64(1); validatorId <= self.metadata.validators; validatorId++ {
		if name == fmt.Sprintf("final-inputs/validators/validator-%d-steering-intents.json", validatorId) {
			return max(uint64(maximumCampaignEvidenceRawFileBytes), self.metadata.intentBytes)
		}
	}
	if campaignCompletionCommitNameV2(name, self.metadata.operators) {
		return self.metadata.completionBytes + 2*maximumCampaignFileEnvelopeOverhead
	}
	if campaignMetadataSourcePathV2(name) {
		return max(uint64(maximumCampaignEvidenceRawFileBytes), self.metadata.intentBytes)
	}
	return maximumCampaignEvidenceRawFileBytes
}

// This is an exact content-addressed path grammar, not a directory exclusion.
func campaignMetadataSourcePathV2(name string) bool {
	const prefix = "final-inputs/validators/v2/"
	if len(name) != len(prefix)+64+len(".bin") || !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".bin") {
		return false
	}
	digest := strings.TrimSuffix(strings.TrimPrefix(name, prefix), ".bin")
	return digest == strings.ToLower(digest) && validCanonicalHashHex("0x"+digest)
}

// Encode only after the complete supplied size has been admitted. Integer
// conversion is checked independently of the source/aggregate-byte ceiling.
func (self campaignEvidenceLimits) fileEnvelopeBytes(name string, size uint64) (int64, error) {
	if err := self.validate(); err != nil {
		return 0, err
	}
	if size > self.rawFileBytes(name) {
		return 0, errors.New("campaign file exceeds its typed raw metadata bound")
	}
	padded, ok := checkedAdd(size, 2)
	if !ok {
		return 0, errors.New("campaign file carrier size overflows")
	}
	encoded, ok := checkedMul(padded/3, 4)
	if !ok {
		return 0, errors.New("campaign file carrier size overflows")
	}
	encoded, ok = checkedAdd(encoded, maximumCampaignFileEnvelopeOverhead)
	if !ok || encoded >= uint64(^uint64(0)>>1) || encoded > uint64(^uint(0)>>1) {
		return 0, errors.New("campaign file carrier exceeds allocation bounds")
	}
	return int64(encoded), nil
}

// The largest carrier is derived from the largest admitted metadata document;
// callers still supply the exact per-entry bound, never this ceiling blindly.
func (self campaignEvidenceLimits) maximumEnvelopeBytes() uint64 {
	if self.metadata == nil {
		return maximumCampaignEvidenceEnvelopeBytes
	}
	maximum := max(self.metadata.indexBytes, self.metadata.manifestBytes+maximumCampaignFileEnvelopeOverhead, self.metadata.completionBytes+2*maximumCampaignFileEnvelopeOverhead, self.metadata.intentBytes)
	return max(uint64(maximumCampaignEvidenceEnvelopeBytes), ((maximum+2)/3)*4+maximumCampaignFileEnvelopeOverhead)
}

// Public control retrieval has the same original configuration authority as
// local publication. Other envelope kinds retain the ordinary bound.
func (self campaignEvidenceLimits) controlEnvelopeBytes(kind string) uint64 {
	if self.metadata != nil {
		switch kind {
		case campaignEvidenceManifestKind:
			return self.metadata.manifestBytes + maximumCampaignFileEnvelopeOverhead
		case "scenario-complete":
			return self.metadata.completionBytes + maximumCampaignFileEnvelopeOverhead
		case "scenario-complete-commit":
			return self.metadata.completionBytes + 2*maximumCampaignFileEnvelopeOverhead
		}
	}
	return maximumCampaignEvidenceEnvelopeBytes
}

// Derived output retains only its two exact prior controls in addition to the
// unchanged ordinary output aggregate; no raw body inherits these bytes.
func (self campaignEvidenceLimits) supplementFileBytes() uint64 {
	if self.metadata != nil {
		return self.metadata.supplementBytes
	}
	return maximumCampaignEvidenceAggregateBytes
}

// Only these four retained capture controls account against metadata rows.
// Every other retained control keeps the original 256 MiB residual owner.
func campaignRetainedMetadataPathV2(name string) bool {
	switch name {
	case campaignCollectedIndexPathV2, campaignPriorIndexPathV2, campaignPriorManifestPathV2, campaignPriorCompletionPathV2:
		return true
	}
	return false
}

// The two post-capture copies have a separate owner from the capture controls.
func campaignDerivedMetadataPathV2(name string) bool {
	return name == campaignDerivedPriorManifestPathV2 || name == campaignDerivedPriorCompletionPathV2
}

// One atomic debit is shared by actual capture retention and both supplement
// readers. Metadata can never enlarge the independent ordinary residual.
func admitCampaignMetadataRetentionV2(limits campaignEvidenceLimits, name string, size uint64, derived bool, used, ordinary uint64) (uint64, uint64, error) {
	if err := limits.validate(); err != nil {
		return used, ordinary, err
	}
	maximum := uint64(maximumCampaignEvidenceAggregateBytes)
	metadataPath := false
	if limits.metadata != nil {
		maximum = limits.metadata.retainedBytes
		metadataPath = campaignRetainedMetadataPathV2(name)
		if derived {
			maximum = limits.supplementFileBytes()
			metadataPath = campaignDerivedMetadataPathV2(name)
		}
	}
	if used > maximum || ordinary > maximumCampaignEvidenceAggregateBytes || size > limits.rawFileBytes(name) || size > maximum-used {
		return used, ordinary, errors.New("retained metadata exceeds its configured path or aggregate owner")
	}
	if !metadataPath {
		if size > maximumCampaignEvidenceAggregateBytes-ordinary {
			return used, ordinary, errors.New("ordinary retained controls exceed their unchanged aggregate owner")
		}
		ordinary += size
	}
	return used + size, ordinary, nil
}

// Both retained originals and a streamed prior carrier use the same typed
// byte owner. Streaming does not grant an additional raw-source allowance.
func validateCampaignMetadataRawSizeV2(limits campaignEvidenceLimits, name string, size uint64) error {
	if err := limits.validate(); err != nil {
		return err
	}
	if size > limits.rawFileBytes(name) {
		return errors.New("campaign metadata raw bytes exceed their configured owner")
	}
	return nil
}

// The inherited source-file allowance is metadata-only. A valid schema here
// is structural sizing, never semantic/native/source acceptance.
func validateCampaignMetadataRawV2(limits campaignEvidenceLimits, name string, raw []byte) error {
	if err := validateCampaignMetadataRawSizeV2(limits, name, uint64(len(raw))); err != nil {
		return err
	}
	if limits.metadata == nil || len(raw) <= maximumCampaignEvidenceRawFileBytes || !campaignMetadataSourcePathV2(name) {
		return nil
	}
	if bytesSHA256(raw) != "sha256:"+strings.TrimSuffix(strings.TrimPrefix(name, "final-inputs/validators/v2/"), ".bin") {
		return errors.New("compact metadata source differs from its content-addressed path")
	}
	var intentFile struct {
		Schema  string                        `json:"schema"`
		Current *validatorpkg.SteeringIntent  `json:"current,omitempty"`
		History []validatorpkg.SteeringIntent `json:"history"`
	}
	if err := decodeStrictJSONBytes(raw, &intentFile); err != nil || intentFile.Schema != validatorpkg.SteeringIntentSchema || intentFile.Current == nil && len(intentFile.History) == 0 {
		return errors.Join(errors.New("oversized compact source is not the original bounded intent control"), err)
	}
	return nil
}

// Keep the original alias/non-regular-file custody walk and owned Close.
// A typed bound is selected before allocation; ordinary callers remain small.
func readCampaignEvidenceFileWithLimitsV2(root, name string, limits campaignEvidenceLimits, control bool) ([]byte, error) {
	if err := limits.validate(); err != nil {
		return nil, err
	}
	commit := limits.metadata != nil && campaignCompletionCommitNameV2(name, limits.metadata.operators)
	if !control || name != "complete.json" && name != campaignEvidenceManifestFilename && !commit {
		if err := validateCampaignEvidencePath(name); err != nil {
			return nil, err
		}
	}
	raw, err := readCampaignEvidenceOwnedFileWithLimitV2(root, name, limits.rawFileBytes(name), nil)
	if err != nil {
		return nil, err
	}
	if err := validateCampaignMetadataRawV2(limits, name, raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// Original controls and their exact prior copies share the same finite owner.
func readCampaignEvidenceFileForConfigV2(cfg *ResolvedConfig, root, name string, control bool) ([]byte, error) {
	limits, err := campaignEvidenceLimitsForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return readCampaignEvidenceFileWithLimitsV2(root, name, limits, control)
}

// Reuse strict decoding after bounded real descriptor reads. This is not a
// size hint supplied by a candidate manifest.
func decodeCampaignControlForConfigV2(cfg *ResolvedConfig, root, name string, value any) error {
	if !finalUsesEvidenceV2(cfg) {
		return decodeFinalSemanticRegularJSON(filepath.Join(root, name), value)
	}
	raw, err := readCampaignEvidenceFileForConfigV2(cfg, root, name, true)
	if err != nil {
		return err
	}
	return decodeStrictJSONBytes(raw, value)
}

// Check provenance/cardinality and a bounded residual before constructing the
// large flat wire image. Rows are measured individually, not copied as a corpus.
func validateCollectedMetadataForConfigV2(cfg *ResolvedConfig, value *FinalSemanticCollectedInputs) error {
	if !finalUsesEvidenceV2(cfg) {
		return nil
	}
	limits, err := campaignEvidenceLimitsForConfig(cfg)
	if err != nil || value == nil {
		return errors.Join(errors.New("compact collected metadata owner is absent"), err)
	}
	return validateCollectedMetadataWithLimitsV2(limits, value)
}

// The cfg-derived owner is shared by actual admission and exact-bound
// controls. This helper cannot supply or widen its own metadata allowance.
func validateCollectedMetadataWithLimitsV2(limits campaignEvidenceLimits, value *FinalSemanticCollectedInputs) error {
	if err := limits.validate(); err != nil || limits.metadata == nil || value == nil {
		return errors.Join(errors.New("compact collected metadata owner is absent"), err)
	}
	copyValue := *value
	copyValue.Validators = append([]FinalCollectedValidatorInputs(nil), value.Validators...)
	rowSizer, err := newCampaignMetadataRowSizerV2()
	if err != nil {
		return err
	}
	var sources uint64
	var rowBytes uint64
	charge := func(size uint64) error {
		if size > limits.metadata.indexBytes-rowBytes {
			return errors.New("compact collected metadata rows exceed their configured wire owner")
		}
		rowBytes += size
		return nil
	}
	for index, validator := range value.Validators {
		if validator.EvidenceV2 == nil {
			continue
		}
		if uint64(len(validator.EvidenceV2.Sources)) > limits.maximumObjects-sources {
			return errors.New("compact collected provenance census exceeds its configured object bound")
		}
		sources += uint64(len(validator.EvidenceV2.Sources))
		for _, source := range validator.EvidenceV2.Sources {
			if err := charge(rowSizer.sourceBytes(source)); err != nil {
				return err
			}
		}
		copyEvidence := *validator.EvidenceV2
		copyEvidence.Sources = nil
		copyValue.Validators[index].EvidenceV2 = &copyEvidence
	}
	if value.PriorPhase != nil {
		if uint64(len(value.PriorPhase.PublicCarriers)) > limits.maximumObjects {
			return errors.New("compact collected prior carrier census exceeds its configured object bound")
		}
		for _, carrier := range value.PriorPhase.PublicCarriers {
			if err := charge(rowSizer.carrierBytes(carrier)); err != nil {
				return err
			}
		}
		copyPrior := *value.PriorPhase
		copyPrior.PublicCarriers = nil
		copyValue.PriorPhase = &copyPrior
	}
	residual, err := json.MarshalIndent(&copyValue, "", "  ")
	if err != nil {
		return err
	}
	if len(residual)+1 > maximumCampaignEvidenceRawFileBytes || uint64(len(residual)+1) > limits.metadata.indexBytes-rowBytes {
		return errors.New("compact collected residual/total metadata exceeds its configured wire owner")
	}
	return nil
}

// This bound is shared by initial signing and immutable retry reads. It does
// not grant metadata capacity to other evidence kinds.
func campaignEvidencePayloadLimitV2(cfg *ResolvedConfig, kind string, payload any) (uint64, error) {
	switch kind {
	case campaignEvidenceFileKind, finalSemanticSupplementFileKind, campaignEvidenceManifestKind, "scenario-complete", "scenario-complete-commit":
	default:
		return 0, nil
	}
	limits, err := campaignEvidenceLimitsForConfig(cfg)
	if err != nil {
		return 0, err
	}
	if limits.metadata == nil {
		return 0, nil
	}
	switch kind {
	case campaignEvidenceFileKind:
		file, ok := payload.(campaignEvidenceFilePayload)
		if !ok {
			return 0, errors.New("campaign file payload has no typed owner")
		}
		if uint64(len(file.Data)) != file.Size || bytesSHA256(file.Data) != file.ContentHash {
			return 0, errors.New("campaign file payload differs from its exact raw bytes")
		}
		if err := validateCampaignMetadataRawV2(limits, file.Path, file.Data); err != nil {
			return 0, err
		}
		bound, err := limits.fileEnvelopeBytes(file.Path, file.Size)
		return uint64(bound), err
	case finalSemanticSupplementFileKind:
		file, ok := payload.(finalSemanticSupplementFilePayload)
		if !ok || validateFinalSemanticPostCapturePath(file.Path) != nil || uint64(len(file.Data)) != file.Size || bytesSHA256(file.Data) != file.ContentHash {
			return 0, errors.New("semantic file payload differs from its typed original bytes")
		}
		if err := validateCampaignMetadataRawV2(limits, file.Path, file.Data); err != nil {
			return 0, err
		}
		bound, err := limits.fileEnvelopeBytes(file.Path, file.Size)
		return uint64(bound), err
	case campaignEvidenceManifestKind:
		manifest, ok := payload.(campaignEvidenceManifestPayload)
		if !ok {
			return 0, errors.New("campaign manifest payload has no typed metadata owner")
		}
		if _, _, err := finalPriorCarrierEntriesV2(&manifest, limits); err != nil {
			return 0, err
		}
		return limits.metadata.manifestBytes, nil
	case "scenario-complete":
		completion, ok := payload.(scenarioCompletePayload)
		if !ok || uint64(len(completion.Files)) > limits.maximumObjects {
			return 0, errors.New("campaign completion exceeds its typed metadata census")
		}
		return limits.metadata.completionBytes, nil
	case "scenario-complete-commit":
		complete, ok := payload.(*ReleaseEvidenceEnvelope)
		if !ok || complete == nil || complete.Kind != "scenario-complete" || uint64(len(complete.Payload)) > limits.metadata.completionBytes {
			return 0, errors.New("campaign completion commit has no bounded original completion")
		}
		return limits.metadata.completionBytes + maximumCampaignFileEnvelopeOverhead, nil
	}
	return 0, nil
}

// All larger local reads go through the private real-file walker. Existing
// files are checked before any immutable-retry decoding or signature work.
func readCampaignLocalEnvelopeWithLimitV2(localPath string, maximum uint64) ([]byte, error) {
	if maximum == 0 {
		return os.ReadFile(localPath)
	}
	return readCampaignEvidenceOwnedFileWithLimitV2(filepath.Dir(localPath), filepath.Base(localPath), maximum+maximumCampaignFileEnvelopeOverhead+1, nil)
}

// Control-only allocation bounds are available to capture callers without
// changing the original no-configuration private helper used by legacy tests.
func captureFinalSemanticClosedInputsWithPriorConfigV2(ctx context.Context, cfg *ResolvedConfig, stateRoot, runRoot string, result *ScenarioResult, terminal *ScenarioObservation, history []*ScenarioObservation, topologyMiners, topologySwarms, topologyOperators int, prior *FinalCollectedPriorPhaseInputs) ([]FinalArtifactLocator, FinalArtifactLocator, FinalArtifactLocator, FinalArtifactLocator, error) {
	limits, err := campaignEvidenceLimitsForConfig(cfg)
	if err != nil {
		return nil, FinalArtifactLocator{}, FinalArtifactLocator{}, FinalArtifactLocator{}, err
	}
	return captureFinalSemanticClosedInputsWithPriorLimitsV2(ctx, stateRoot, runRoot, result, terminal, history, topologyMiners, topologySwarms, topologyOperators, prior, limits)
}

// Exact generated operator completion names, not an arbitrary reserved prefix.
func campaignCompletionCommitNameV2(name string, operators uint64) bool {
	for operator := uint64(1); operator <= operators; operator++ {
		if name == "scenario-complete-commit.operator-"+strconv.FormatUint(operator, 10)+".evidence.json" {
			return true
		}
	}
	return false
}

// The actual capture and derived-output producers select the same path owner
// before an immutable write. Legacy callers preserve their original behavior.
func persistFinalCollectedArtifactForConfigV2(cfg *ResolvedConfig, runRoot, kind, relative string, data []byte) (FinalArtifactLocator, error) {
	if finalUsesEvidenceV2(cfg) {
		limits, err := campaignEvidenceLimitsForConfig(cfg)
		if err != nil {
			return FinalArtifactLocator{}, err
		}
		if err := validateCampaignMetadataRawV2(limits, relative, data); err != nil {
			return FinalArtifactLocator{}, err
		}
	}
	return persistFinalCollectedArtifact(runRoot, kind, relative, data)
}

// Actual configured replay uses a finite descriptor owner for every artifact;
// the public legacy loader remains available with its existing contract.
func newFinalSemanticCampaignArtifactLoaderForConfigV2(cfg *ResolvedConfig, stateDir, runDir string) (FinalArtifactLoader, error) {
	if !finalUsesEvidenceV2(cfg) {
		return NewFinalSemanticCampaignArtifactLoader(stateDir, runDir)
	}
	limits, err := campaignEvidenceLimitsForConfig(cfg)
	if err != nil {
		return nil, err
	}
	stateRoot, err := filepath.Abs(stateDir)
	if err != nil {
		return nil, err
	}
	runRoot, err := filepath.Abs(runDir)
	if err != nil || !pathWithinRoot(stateRoot, runRoot) {
		return nil, errors.New("final semantic run directory is outside the state directory")
	}
	return func(ctx context.Context, locator FinalArtifactLocator) ([]byte, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := verifyFinalArtifact("campaign artifact", locator, locator.Kind); err != nil {
			return nil, err
		}
		if strings.Contains(locator.URI, "://") {
			return nil, errors.New("pre-publication semantic loader rejects network locators")
		}
		if locator.SizeBytes > limits.rawFileBytes(locator.URI) {
			return nil, errors.New("final semantic artifact exceeds its configured metadata owner")
		}
		root := runRoot
		if strings.HasPrefix(locator.URI, "receipts/") || strings.HasPrefix(locator.URI, "public/") {
			root = stateRoot
		}
		raw, err := readCampaignEvidenceFileWithLimitsV2(root, locator.URI, limits, false)
		if err != nil {
			return nil, err
		}
		if uint64(len(raw)) != locator.SizeBytes || bytesSHA256(raw) != locator.ContentHash {
			return nil, errors.New("final semantic artifact differs from its exact locator")
		}
		return raw, ctx.Err()
	}, nil
}

// Configure only the exact derived-control owner in the existing enumerator.
func enumerateFinalSemanticRawFilesForConfigV2(cfg *ResolvedConfig, runRoot string, requirePair bool) ([]finalSemanticRawFile, error) {
	limits, err := campaignEvidenceLimitsForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return enumerateFinalSemanticRawFilesWithLimitsV2(runRoot, requirePair, limits)
}
