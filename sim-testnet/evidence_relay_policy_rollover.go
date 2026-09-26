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
	for source.successor != nil && epoch >= source.successor.activations[0].Domain.Epoch {
		source = source.successor
	}
	return source
}

func (source *evidenceRelaySource) generations() []*evidenceRelaySource {
	var result []*evidenceRelaySource
	for next := source; next != nil; next = next.successor {
		result = append(result, next)
	}
	return result
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
	if original.successor != nil {
		return self.installPolicyRolloverSuccessorV2(ctx, original, &successor, signed, block)
	}
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
		for next := self.sources[index].successor; next != nil; next = next.successor {
			if horizon.successorKVs == nil {
				horizon.successorKVs = map[evidenceRelayHorizonSource][]protocol.ValidatorEvidenceActivation{}
			}
			for _, activation := range next.activations {
				key := evidenceRelayHorizonSource{hotkey: activation.Hotkey, noId: activation.NoID}
				horizon.successorKVs[key] = append(horizon.successorKVs[key], activation)
			}
		}
	}
}

// A same-policy fresh ledger adds a cutoff without widening the original
// policy-gap waiver or changing its historical receipt/first-acceptance fields.
func (self *evidenceRelayRuntime) installPolicyRolloverSuccessorV2(ctx context.Context, original, successor *evidenceRelaySource, signed []evidenceRelayPolicyGapActivation, block uint64) error {
	previous := original
	for previous.successor != nil {
		previous = previous.successor
	}
	self.stateLock.Lock()
	started := self.workerStarted || self.horizon != nil
	self.stateLock.Unlock()
	if started || len(previous.activations) != len(signed) {
		return errors.New("successor source cannot change after startup or alter its census")
	}
	for index, member := range signed {
		old, next := previous.activations[index], member.Activation
		domain := old.Domain
		domain.Epoch = next.Domain.Epoch
		if domain != next.Domain || next.Domain.Epoch <= old.Domain.Epoch || next.VPK == old.VPK || next.Hotkey != old.Hotkey || next.NoID != old.NoID || next.FirstSequence != 1 || next.PriorRoot != ([32]byte{}) || next.EVMBlock < old.EVMBlock || next.EVMBlock > block || index > 0 && next.Domain != signed[0].Activation.Domain {
			return errors.New("successor source changes its approved same-policy lineage or cutoff")
		}
		if err := next.Verify(next, member.VPKSignature, member.HotkeySignature); err != nil {
			return err
		}
		hash, err := self.chain.BlockHashContext(ctx, next.EVMBlock)
		if err != nil || hash != next.EVMHash {
			return errors.Join(errors.New("successor activation snapshot is not canonical"), err)
		}
		evidenceDomain, err := next.EvidenceDomain()
		if err != nil {
			return err
		}
		if err := self.chain.ValidateValidatorEvidencePolicyEraV2Context(ctx, protocol.ValidatorEvidenceHeader{Domain: evidenceDomain, Epoch: next.Domain.Epoch}); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if self.workerStarted || self.horizon != nil || previous.successor != nil {
		return errors.New("successor owner changed during authentication")
	}
	previous.successor = successor
	return nil
}
