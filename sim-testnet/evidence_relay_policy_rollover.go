//go:build linux || darwin

package main

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/urfoundation/sn/protocol"
	validatorcomponent "github.com/urfoundation/sn/validator"
)

// Discovery keeps the original source and cursor. Only the exact approved
// cutoff chooses a successor namespace and activation generation.
func (source *evidenceRelaySource) forEpoch(epoch uint64) *evidenceRelaySource {
	if source.successor != nil && epoch >= source.successor.activations[0].Domain.Epoch {
		return source.successor
	}
	return source
}

func (source *evidenceRelaySource) generations() []*evidenceRelaySource {
	if source.successor == nil {
		return []*evidenceRelaySource{source}
	}
	return []*evidenceRelaySource{source, source.successor}
}

// The durable approved handoff reader supplies the namespace/configuration;
// this installer independently checks the entire finalized onchain activation
// census before enabling either the historical gap cutoff or new routing.
func (self *evidenceRelayRuntime) installPolicyRolloverSource(ctx context.Context, validatorID uint64, successor evidenceRelaySource, signed []evidenceRelayPolicyGapActivation) error {
	if ctx == nil || self == nil || self.executor == nil || self.executor.plan == nil || self.executor.plan.ValidatorEvidence == nil || self.chain == nil {
		return errors.New("evidence policy successor has no approved companion owner")
	}
	var original *evidenceRelaySource
	for index := range self.sources {
		source := &self.sources[index]
		for _, generation := range source.generations() {
			if generation.stateDir == successor.stateDir {
				return errors.New("evidence policy successor reuses an existing source namespace")
			}
		}
		if source.validatorId == validatorID {
			original = source
		}
	}
	relative, err := filepath.Rel(self.executor.stateDir, successor.stateDir)
	if err != nil || original == nil || successor.validatorId != validatorID || successor.successor != nil || len(signed) == 0 || len(signed) != len(successor.activations) ||
		!reflect.DeepEqual(original.bounds, successor.bounds) || !filepath.IsAbs(successor.stateDir) || filepath.Clean(successor.stateDir) != successor.stateDir || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return errors.Join(errors.New("evidence policy successor changes its approved source bounds or namespace"), err)
	}
	block, hash, err := self.chain.FinalizedBlockContext(ctx)
	if err != nil {
		return err
	}
	for index, member := range signed {
		if member.Activation != successor.activations[index] {
			return errors.New("evidence policy successor differs from its signed activation census")
		}
		if _, err := self.chain.ValidatorEvidenceActivationAtHashContext(ctx, self.executor.plan.ValidatorEvidence.Address, [32]byte(self.executor.plan.ValidatorEvidence.RuntimeCodeHash), member.Activation, block, hash); err != nil {
			return err
		}
	}
	successor.nextEpoch = signed[0].Activation.Domain.Epoch
	successor.activations = slices.Clone(successor.activations)
	return self.installPolicyGapCutoffAndSource(ctx, validatorID, signed, &successor)
}

// The original receipt-prefix and cold-census formats bind one generation.
// Cold verification across both namespaces avoids borrowing that older proof.
func (self *evidenceRelayRuntime) hasPolicyRolloverSources() bool {
	for index := range self.sources {
		if self.sources[index].successor != nil {
			return true
		}
	}
	return false
}

func validateEvidenceRelayGenerationEpoch(source, generation *evidenceRelaySource, epoch uint64) error {
	if source.forEpoch(epoch) != generation {
		return errors.New("evidence publication is outside its approved source generation")
	}
	return nil
}

func discoverEvidenceRelayClosedGenerations(ctx context.Context, source *evidenceRelaySource) ([]validatorcomponent.ValidatorEvidencePublicationV2Manifest, error) {
	var result []validatorcomponent.ValidatorEvidencePublicationV2Manifest
	for _, generation := range source.generations() {
		manifests, err := validatorcomponent.DiscoverValidatorEvidencePublicationV2Manifests(ctx, generation.stateDir, generation.bounds)
		if err != nil {
			return nil, err
		}
		for _, manifest := range manifests {
			if err := validateEvidenceRelayGenerationEpoch(source, generation, manifest.Epoch); err != nil {
				return nil, err
			}
		}
		result = append(result, manifests...)
	}
	return result, ctx.Err()
}

func discoverEvidenceRelayAuditGenerations(ctx context.Context, source *evidenceRelaySource) ([]validatorcomponent.ValidatorEvidenceDepositAuditV2Manifest, error) {
	var result []validatorcomponent.ValidatorEvidenceDepositAuditV2Manifest
	for _, generation := range source.generations() {
		manifests, err := validatorcomponent.DiscoverValidatorEvidenceDepositAuditV2Manifests(ctx, generation.stateDir, generation.bounds)
		if err != nil {
			return nil, err
		}
		for _, manifest := range manifests {
			if err := validateEvidenceRelayGenerationEpoch(source, generation, manifest.Epoch); err != nil {
				return nil, err
			}
		}
		result = append(result, manifests...)
	}
	return result, ctx.Err()
}

// Funding and source-count arithmetic keep their original activation anchor.
// A successor changes only the exact accepted signing domain after its cutoff.
func (self *evidenceRelayRuntime) installPolicyRolloverHorizon(horizon *evidenceRelayHorizon) {
	for index := range self.sources {
		if next := self.sources[index].successor; next != nil {
			if horizon.successorKVs == nil {
				horizon.successorKVs = map[evidenceRelayHorizonSource]protocol.ValidatorEvidenceActivation{}
			}
			for _, activation := range next.activations {
				horizon.successorKVs[evidenceRelayHorizonSource{hotkey: activation.Hotkey, noId: activation.NoID}] = activation
			}
		}
	}
}
