//go:build linux || darwin

// Explicit source lifetime successors preserve original identity and evidence.
package main

import (
	"errors"
	"fmt"
	"reflect"
	"slices"

	validatorcomponent "github.com/urfoundation/sn/validator"
)

// V6 authorizes one exact doubling of the original source lifetime. The
// predecessor's records, activation, policy, per-record limits and liabilities
// remain unchanged. Every later continuation retains this same revision.
const evidenceRelayContinuationSourceExpansionSchema = "urnetwork-sim-evidence-relay-continuation-v6"

// Each validator retains both immutable predecessor and approved capacities.
type evidenceRelaySourceBounds struct {
	ValidatorId uint64                                     `json:"validator_id"`
	Original    validatorcomponent.ReleaseEvidenceV2Bounds `json:"original"`
	Approved    validatorcomponent.ReleaseEvidenceV2Bounds `json:"approved"`
}

// Extend the lifetime containers together. Per-record, per-proof, metadata,
// participant, upload and policy limits do not change.
func doubledEvidenceRelaySourceBounds(original validatorcomponent.ReleaseEvidenceV2Bounds) (validatorcomponent.ReleaseEvidenceV2Bounds, error) {
	if err := original.Validate(2); err != nil {
		return validatorcomponent.ReleaseEvidenceV2Bounds{}, err
	}
	next := original
	for _, value := range []*uint64{
		&next.Disk.MaxRecordCount, &next.Disk.MaxTrailCount,
		&next.Disk.MaxRawRecordBytes, &next.Disk.MaxStorageBytes,
		&next.Disk.MaxStorageFiles, &next.Disk.MaxProofBytes,
		&next.Cut.Records.MaxDataBytes, &next.Cut.Records.MaxItems,
		&next.Cut.Records.MaxChunks, &next.Cut.Records.MaxPages,
		&next.Cut.Proofs.MaxDataBytes, &next.Cut.Proofs.MaxItems,
		&next.Cut.Proofs.MaxChunks, &next.Cut.Proofs.MaxPages,
		&next.Replay.MaxTrails, &next.Replay.MaxScratchBytes,
		&next.Replay.MaxScratchFiles,
	} {
		doubled, ok := checkedMul(*value, 2)
		if !ok {
			return validatorcomponent.ReleaseEvidenceV2Bounds{}, errors.New("relay source lifetime doubling overflows")
		}
		*value = doubled
	}
	if err := next.Validate(2); err != nil {
		return validatorcomponent.ReleaseEvidenceV2Bounds{}, err
	}
	return next, nil
}

// Only the explicit successor may carry one exact original capacity doubling.
func (self *EvidenceRelayContinuation) validateSourceBounds() error {
	if self == nil {
		return errors.New("relay source lifetime owner is absent")
	}
	if self.Schema != evidenceRelayContinuationSourceExpansionSchema {
		if len(self.SourceBounds) != 0 {
			return errors.New("relay source lifetime revision requires its explicit v6 approval")
		}
		return nil
	}
	if len(self.SourceBounds) != 2 {
		return errors.New("relay source lifetime revision requires both original validators")
	}
	for index, source := range self.SourceBounds {
		expected, err := doubledEvidenceRelaySourceBounds(source.Original)
		if err != nil || source.ValidatorId != uint64(index+1) || !reflect.DeepEqual(source.Approved, expected) {
			return errors.Join(errors.New("relay source lifetime revision differs from one exact original doubling"), err)
		}
	}
	return nil
}

// Both expanded successors retain the same approved relay funding profile.
func evidenceRelayExpandedFunding(schema string) bool {
	return schema == evidenceRelayContinuationExpansionSchema || schema == evidenceRelayContinuationSourceExpansionSchema
}

// Ordinary capture and exact import preserve an existing revision. The only
// introduction is the explicit multiplier flag on an existing continuation;
// imports recover that decision from the approved plan, never a new option.
func captureEvidenceRelaySourceBounds(cfg *ResolvedConfig, base *SetupPlan, multiplier uint64, pin *EvidenceRelayContinuation) ([]evidenceRelaySourceBounds, error) {
	if cfg == nil || cfg.Config == nil || base == nil || multiplier != 0 && multiplier != 2 || pin != nil && multiplier != 0 {
		return nil, errors.New("relay source lifetime revision requires an explicit multiplier of 2 or an exact import")
	}
	prior := base.EvidenceRelayContinuation
	if prior != nil && len(prior.SourceBounds) != 0 {
		if err := prior.validateSourceBounds(); err != nil {
			return nil, err
		}
		if pin != nil && !reflect.DeepEqual(pin.SourceBounds, prior.SourceBounds) {
			return nil, errors.New("relay source lifetime import changed its existing approved revision")
		}
		return slices.Clone(prior.SourceBounds), nil
	}
	if multiplier == 0 && (pin == nil || len(pin.SourceBounds) == 0) {
		return nil, nil
	}
	if prior == nil || len(cfg.Config.ValidatorEvidenceV2) != 2 {
		return nil, errors.New("relay source lifetime expansion requires the existing two-validator continuation")
	}
	result := make([]evidenceRelaySourceBounds, 0, 2)
	for index, configured := range cfg.Config.ValidatorEvidenceV2 {
		if configured.ValidatorID != uint64(index+1) || len(configured.Evidence.Operators) != 2 {
			return nil, errors.New("relay source lifetime expansion changed the source census")
		}
		approved, err := doubledEvidenceRelaySourceBounds(configured.Evidence.Bounds)
		if err != nil {
			return nil, err
		}
		result = append(result, evidenceRelaySourceBounds{ValidatorId: configured.ValidatorID, Original: configured.Evidence.Bounds, Approved: approved})
	}
	if pin != nil && (pin.Schema != evidenceRelayContinuationSourceExpansionSchema || !reflect.DeepEqual(pin.SourceBounds, result)) {
		return nil, errors.New("relay source lifetime import differs from the authenticated original configuration")
	}
	return result, nil
}

// Resolution is the common owner for renderer, startup, relay and independent
// runtime manifests. It accepts only the exact original template or its already
// resolved approved form, making repeated resolution idempotent.
func applyEvidenceRelaySourceBounds(values []validatorcomponent.ReleaseValidatorEvidenceV2Config, c *EvidenceRelayContinuation) ([]validatorcomponent.ReleaseValidatorEvidenceV2Config, error) {
	if c == nil {
		return values, nil
	}
	if err := c.validateSourceBounds(); err != nil {
		return nil, err
	}
	if len(c.SourceBounds) == 0 {
		return values, nil
	}
	if len(values) != len(c.SourceBounds) {
		return nil, errors.New("relay source lifetime configuration lost an approved validator")
	}
	result := slices.Clone(values)
	for index, source := range c.SourceBounds {
		configured := &result[index]
		if configured.ValidatorID != source.ValidatorId || (!reflect.DeepEqual(configured.Evidence.Bounds, source.Original) && !reflect.DeepEqual(configured.Evidence.Bounds, source.Approved)) {
			return nil, fmt.Errorf("relay source lifetime validator %d differs from its original and approved bounds", source.ValidatorId)
		}
		configured.Evidence.Bounds = source.Approved
	}
	return result, nil
}

// Forecast through the approved overlay without mutating the hashed template.
func evidenceRelaySourceCapacityConfig(cfg *ResolvedConfig, plan *SetupPlan) (*ResolvedConfig, error) {
	if cfg == nil || cfg.Config == nil || plan == nil {
		return nil, errors.New("relay source lifetime forecast has no configuration or approved plan")
	}
	values, err := applyEvidenceRelaySourceBounds(cfg.Config.ValidatorEvidenceV2, plan.EvidenceRelayContinuation)
	if err != nil {
		return nil, err
	}
	resolved, copied := *cfg, *cfg.Config
	copied.ValidatorEvidenceV2 = values
	resolved.Config = &copied
	return &resolved, nil
}
