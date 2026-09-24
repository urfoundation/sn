//go:build linux || darwin

package main

import (
	"context"
	"errors"
)

// Original activation authentication precedes this adapter. The durable
// handoff reader owns plan, generation config, file and publication-receipt
// authority; the relay installer independently rechecks the finalized chain.
func (self *evidenceRelayRuntime) installAuthenticatedPolicyRolloverHandoffV2(ctx context.Context, approved *ResolvedConfig) error {
	handoff, err := readPolicyRolloverHandoffV2(ctx, approved, self.executor.stateDir, self.executor.plan)
	if err != nil || handoff == nil {
		return err
	}
	if !handoff.Activated || handoff.LedgerContinuityClaimed || !validCanonicalHashHex(handoff.PlanHash) || handoff.Generation == 0 || len(handoff.Validators) != len(self.sources) {
		return errors.New("evidence relay handoff has no complete active independent source generation")
	}
	seen := map[uint64]bool{}
	var consumed int
	for _, configured := range handoff.Validators {
		var original *evidenceRelaySource
		for index := range self.sources {
			if self.sources[index].validatorId == configured.ValidatorID {
				original = &self.sources[index]
				break
			}
		}
		if original == nil || seen[configured.ValidatorID] || original.stateDir != configured.PreviousStateDir || len(configured.Evidence.Operators) != len(original.activations) {
			return errors.New("evidence relay handoff changes its original source namespace or member census")
		}
		seen[configured.ValidatorID] = true
		successor := evidenceRelaySource{validatorId: configured.ValidatorID, stateDir: configured.StateDir, bounds: configured.Evidence.Bounds}
		var signed []evidenceRelayPolicyGapActivation
		for index, operator := range configured.Evidence.Operators {
			if operator.NoID != original.activations[index].NoID {
				return errors.New("evidence relay handoff reorders its configured operator census")
			}
			matches := 0
			for _, member := range handoff.Members {
				if member.ValidatorId != configured.ValidatorID || member.NoId != operator.NoID {
					continue
				}
				if member.Activation.Domain.Epoch != handoff.CutoffEpoch || member.Activation.VPK == original.activations[index].VPK || member.Activation.FirstSequence != 1 {
					return errors.New("evidence relay handoff changes its independent generation cutoff")
				}
				matches++
				successor.activations = append(successor.activations, member.Activation)
				signed = append(signed, evidenceRelayPolicyGapActivation{Activation: member.Activation, VPKSignature: member.VpkSignature, HotkeySignature: member.HotkeySignature})
			}
			if matches != 1 {
				return errors.New("evidence relay handoff lacks a unique signed activation for every operator")
			}
			consumed++
		}
		if err := self.installPolicyRolloverSource(ctx, configured.ValidatorID, successor, signed); err != nil {
			return err
		}
		if self.policyGapFirstEpoch[configured.ValidatorID] != handoff.FirstFullEpoch {
			return errors.New("evidence relay handoff changes the first complete acceptance epoch")
		}
	}
	if consumed != len(handoff.Members) {
		return errors.New("evidence relay handoff has an unconfigured activation member")
	}
	self.policyRolloverPlanHash = handoff.PlanHash
	return ctx.Err()
}
