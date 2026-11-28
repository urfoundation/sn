// Archive admission is derived from the independently approved configuration,
// never from a manifest's own claimed capacity or a nominal block duration.
package main

import (
	"container/heap"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	validatorpkg "github.com/urfoundation/sn/v2026/validator"
)

// Decode one bounded raw document. A malformed document contributes no partial
// locators, matching the original graph discovery contract.
func collectCampaignArtifactReferencesWithLimits(raw json.RawMessage, references map[string]campaignArtifactReference, depth int, limits campaignEvidenceLimits) error {
	if err := limits.validate(); err != nil {
		return err
	}
	if depth > maximumCampaignEvidenceJSONDepth {
		return errors.New("campaign artifact Json exceeds maximum depth")
	}
	var value any
	if err := decodeCampaignArtifactReferenceJSON(raw, &value); err != nil {
		return nil
	}
	return collectCampaignArtifactReferenceValuesWithLimits(value, references, depth, limits)
}

// A run file referenced by its original locator remains one physical archive
// object. External locators still consume a separate exact URI census slot.
func validateCampaignArtifactObjectCountWithLimits(files map[string]string, references map[string]campaignArtifactReference, limits campaignEvidenceLimits) error {
	if err := limits.validate(); err != nil {
		return err
	}
	count := uint64(len(files))
	if count > limits.maximumObjects {
		return errors.New("campaign evidence graph exceeds its configured object bound")
	}
	for name := range references {
		if _, found := files[name]; found {
			continue
		}
		if count == limits.maximumObjects {
			return errors.New("campaign evidence graph exceeds its configured object bound")
		}
		count++
	}
	return nil
}

// Ordinary raw/envelope and Json-depth limits remain unchanged. Exact typed
// metadata controls and separately owned source bytes have finite owners.
type campaignEvidenceLimits struct {
	maximumBytes   uint64
	maximumObjects uint64
	metadata       *campaignMetadataLimitsV2
}

// Each exact locator is queued once. Heap ordering preserves the original
// lexicographic traversal without repeatedly sorting the entire remaining set.
type campaignArtifactQueueV2 struct {
	files          map[string]string
	known          map[string]bool
	names          campaignArtifactNamesV2
	objects        uint64
	maximumObjects uint64
}

// The original run-file census is fixed before any locator is discovered.
func newCampaignArtifactQueueV2(files map[string]string, limits campaignEvidenceLimits) (*campaignArtifactQueueV2, error) {
	if err := limits.validate(); err != nil {
		return nil, err
	}
	if uint64(len(files)) > limits.maximumObjects {
		return nil, errors.New("campaign original file census exceeds its configured object bound")
	}
	return &campaignArtifactQueueV2{files: files, known: map[string]bool{}, objects: uint64(len(files)), maximumObjects: limits.maximumObjects}, nil
}

// Inspect only the just-authenticated source edges. Refusal is atomic and a
// run-file locator does not debit its already counted physical object twice.
func (self *campaignArtifactQueueV2) admit(targets map[string]bool) error {
	if self == nil || self.known == nil || self.objects > self.maximumObjects {
		return errors.New("campaign locator queue owner is invalid")
	}
	var added uint64
	for name := range targets {
		if self.known[name] {
			continue
		}
		if _, found := self.files[name]; !found {
			if added == self.maximumObjects-self.objects {
				return errors.New("campaign evidence graph exceeds its configured object bound")
			}
			added++
		}
	}
	for name := range targets {
		if self.known[name] {
			continue
		}
		self.known[name] = true
		heap.Push(&self.names, name)
	}
	self.objects += added
	return nil
}

// A consumed name remains known, so another original edge cannot requeue it.
func (self *campaignArtifactQueueV2) next() (string, bool) {
	if len(self.names) == 0 {
		return "", false
	}
	return heap.Pop(&self.names).(string), true
}

// Only string slots are owned here; original bodies never enter the queue.
type campaignArtifactNamesV2 []string

// These method names implement the standard heap interface.
func (self campaignArtifactNamesV2) Len() int           { return len(self) }
func (self campaignArtifactNamesV2) Less(i, j int) bool { return self[i] < self[j] }
func (self campaignArtifactNamesV2) Swap(i, j int)      { self[i], self[j] = self[j], self[i] }
func (self *campaignArtifactNamesV2) Push(value any)    { *self = append(*self, value.(string)) }
func (self *campaignArtifactNamesV2) Pop() any {
	last := len(*self) - 1
	value := (*self)[last]
	(*self)[last] = ""
	*self = (*self)[:last]
	return value
}

// Logical retained locator/edge bytes have their own fixed control owner.
// This is not a heap-size prediction: map overhead is also count-bounded.
type campaignEvidenceMetadataBudgetV2 struct {
	maximumBytes uint64
	usedBytes    uint64
}

// Exact configured census geometry owns metadata independently of tape bytes.
func campaignEvidenceMetadataForConfigV2(cfg *ResolvedConfig) (*campaignEvidenceMetadataBudgetV2, error) {
	if !finalUsesEvidenceV2(cfg) {
		return nil, nil
	}
	limits, err := campaignEvidenceLimitsForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &campaignEvidenceMetadataBudgetV2{maximumBytes: limits.metadata.graphBytes}, nil
}

// Count the exact retained string bytes plus a fixed slot debit before any
// new reference or edge is inserted. Repeated identical edges consume once.
func (self *campaignEvidenceMetadataBudgetV2) admit(references map[string]campaignArtifactReference, edges map[string]map[string]bool, source string, additions map[string]campaignArtifactReference) error {
	if self == nil {
		return nil
	}
	if self.maximumBytes == 0 || self.usedBytes > self.maximumBytes {
		return errors.New("compact metadata owner is invalid")
	}
	var charge uint64
	add := func(size uint64) error {
		if size > self.maximumBytes-self.usedBytes-charge {
			return errors.New("compact locator/edge metadata exceeds its configured control bound")
		}
		charge += size
		return nil
	}
	for name, reference := range additions {
		if _, found := references[name]; !found {
			for _, size := range []uint64{64, uint64(len(reference.Kind)), uint64(len(reference.URI)), uint64(len(reference.ContentHash))} {
				if err := add(size); err != nil {
					return err
				}
			}
		}
		if !edges[source][name] {
			for _, size := range []uint64{32, uint64(len(source)), uint64(len(name))} {
				if err := add(size); err != nil {
					return err
				}
			}
		}
	}
	self.usedBytes += charge
	return nil
}

// Unqualified/legacy callers retain the old small-archive refusal boundary.
func defaultCampaignEvidenceLimits() campaignEvidenceLimits {
	return campaignEvidenceLimits{maximumBytes: maximumCampaignEvidenceAggregateBytes, maximumObjects: maximumCampaignEvidenceObjects}
}

// Counts are checked before conversion to int or allocation of a census map.
func (self campaignEvidenceLimits) validate() error {
	if self.maximumBytes == 0 || self.maximumBytes > uint64(^uint64(0)>>1) || self.maximumObjects == 0 || self.maximumObjects > uint64(^uint(0)>>1) {
		return errors.New("campaign evidence capacity is absent or exceeds signed allocation bounds")
	}
	return self.metadata.validate()
}

// A byte owner is an admission ceiling, not a preallocation. Capture refuses
// history that outgrows this finite budget even if nominal time has not elapsed.
type campaignValidatorCaptureLimitsV2 struct {
	dataBytes      uint64
	controlBytes   uint64
	maximumObjects uint64
}

// Preserve independently bounded history families: combined input/closure/
// intent references, client observations, Http observations, prepared audits,
// audit locators, native read capture and selected original-control copies.
// Setup additionally owns one exact activation history per original operator.
func campaignValidatorLimitsV2(bounds validatorpkg.ReleaseEvidenceV2Bounds, operators, relaySlots uint64) (campaignValidatorCaptureLimitsV2, error) {
	var result campaignValidatorCaptureLimitsV2
	if operators == 0 || relaySlots == 0 || bounds.Disk.MaxStorageBytes == 0 || bounds.Disk.MaxStorageFiles == 0 || bounds.MaxHistoryBytes == 0 || bounds.MaxHeadEntries == 0 {
		return result, errors.New("compact capture requires explicit source/history/relay capacities")
	}
	var ok bool
	result.dataBytes, ok = checkedMul(operators, bounds.Disk.MaxStorageBytes)
	if !ok {
		return result, errors.New("compact source-data capacity overflows")
	}
	families, ok := checkedAdd(operators, 7)
	if !ok {
		return result, errors.New("compact source-history census overflows")
	}
	result.controlBytes, ok = checkedMul(families, bounds.MaxHistoryBytes)
	if !ok {
		return result, errors.New("compact source-control capacity overflows")
	}
	sourceObjects, ok := checkedMul(operators, bounds.Disk.MaxStorageFiles)
	if !ok {
		return result, errors.New("compact source-object capacity overflows")
	}
	// Both public origins retain separate provenance entries, even when their
	// authenticated bytes occupy one content-addressed file.
	sourceObjects, ok = checkedMul(sourceObjects, 2)
	if !ok {
		return result, errors.New("compact replica provenance capacity overflows")
	}
	decisionObjects, ok := checkedMul(relaySlots, bounds.MaxHeadEntries)
	if !ok {
		return result, errors.New("compact decision-object capacity overflows")
	}
	decisionObjects, ok = checkedMul(decisionObjects, operators)
	if !ok {
		return result, errors.New("compact operator decision-object capacity overflows")
	}
	result.maximumObjects, ok = checkedAdd(sourceObjects, decisionObjects)
	if !ok {
		return result, errors.New("compact source census overflows")
	}
	result.maximumObjects, ok = checkedAdd(result.maximumObjects, maximumCampaignEvidenceObjects)
	if !ok {
		return result, errors.New("compact control census overflows")
	}
	total, ok := checkedAdd(result.dataBytes, result.controlBytes)
	if !ok {
		return result, errors.New("compact aggregate capacity overflows")
	}
	if err := (campaignEvidenceLimits{maximumBytes: total, maximumObjects: result.maximumObjects}).validate(); err != nil {
		return result, err
	}
	return result, nil
}

// Each closed phase gets its own finite admission. Production retains the
// activation prefix, so source storage/history limits—not just its three
// accepted epochs—are the authority. No new configuration switch grants bytes.
func campaignEvidenceLimitsForConfig(cfg *ResolvedConfig) (campaignEvidenceLimits, error) {
	limits := defaultCampaignEvidenceLimits()
	if !finalUsesEvidenceV2(cfg) {
		return limits, nil
	}
	if cfg.Config.Topology.Validators < 1 || len(cfg.Config.ValidatorEvidenceV2) != cfg.Config.Topology.Validators || cfg.Config.Topology.Operators != 2 {
		return campaignEvidenceLimits{}, errors.New("compact archive configured source census differs")
	}
	seen := map[uint64]bool{}
	for _, configured := range cfg.Config.ValidatorEvidenceV2 {
		if configured.ValidatorID == 0 || configured.ValidatorID > uint64(cfg.Config.Topology.Validators) || seen[configured.ValidatorID] || len(configured.Evidence.Operators) != cfg.Config.Topology.Operators {
			return campaignEvidenceLimits{}, errors.New("compact archive validator/operator routing differs")
		}
		seen[configured.ValidatorID] = true
		for index, operator := range configured.Evidence.Operators {
			if operator.NoID != uint64(index+1) {
				return campaignEvidenceLimits{}, errors.New("compact archive original operator census differs")
			}
		}
		capture, err := campaignValidatorLimitsV2(configured.Evidence.Bounds, uint64(len(configured.Evidence.Operators)), cfg.Config.ValidatorEvidenceRelay.MaxSlots)
		if err != nil {
			return campaignEvidenceLimits{}, fmt.Errorf("validator %d capture capacity: %w", configured.ValidatorID, err)
		}
		for _, bytes := range []uint64{capture.dataBytes, capture.controlBytes} {
			var ok bool
			limits.maximumBytes, ok = checkedAdd(limits.maximumBytes, bytes)
			if !ok {
				return campaignEvidenceLimits{}, errors.New("campaign archive byte capacity overflows")
			}
		}
		var ok bool
		limits.maximumObjects, ok = checkedAdd(limits.maximumObjects, capture.maximumObjects)
		if !ok {
			return campaignEvidenceLimits{}, errors.New("campaign archive object capacity overflows")
		}
	}
	var err error
	limits.metadata, err = campaignMetadataLimitsForConfigV2(cfg, limits.maximumObjects)
	if err != nil {
		return campaignEvidenceLimits{}, err
	}
	if err := limits.validate(); err != nil {
		return campaignEvidenceLimits{}, err
	}
	return limits, nil
}

// The actual V2 completion hash census uses the same finite approved capacity
// and private regular-file reader as publication, before any body allocation.
func evidenceFileHashesForConfigV2(ctx context.Context, cfg *ResolvedConfig, root string, operators int) (map[string]string, error) {
	if ctx == nil {
		return nil, errors.New("campaign file-hash context is absent")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !finalUsesEvidenceV2(cfg) {
		hashes, err := evidenceFileHashes(root, operators)
		return hashes, errors.Join(err, ctx.Err())
	}
	limits, err := campaignEvidenceLimitsForConfig(cfg)
	if err != nil {
		return nil, err
	}
	if operators != cfg.Config.Topology.Operators {
		return nil, errors.New("campaign file-hash operator census differs")
	}
	excluded := map[string]bool{"complete.json": true, campaignEvidenceManifestFilename: true}
	for operator := 1; operator <= operators; operator++ {
		excluded[fmt.Sprintf("scenario-complete-commit.operator-%d.evidence.json", operator)] = true
	}
	hashes := map[string]string{}
	var aggregate uint64
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if excluded[relative] || isFinalSemanticPostCapturePath(relative) {
			return nil
		}
		if uint64(len(hashes)) >= limits.maximumObjects {
			return errors.New("campaign file-hash census exceeds configured objects")
		}
		raw, err := readCampaignEvidenceFileWithLimitsV2(root, relative, limits, false)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if uint64(len(raw)) > limits.maximumBytes-aggregate {
			return errors.New("campaign file-hash census exceeds configured bytes")
		}
		aggregate += uint64(len(raw))
		hashes[relative] = bytesSHA256(raw)
		return nil
	})
	if err := errors.Join(err, ctx.Err()); err != nil {
		return nil, err
	}
	return hashes, nil
}
