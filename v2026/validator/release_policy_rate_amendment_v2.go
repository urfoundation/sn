//go:build linux || darwin

// Immutable attempt replay remains separate from governed decision policy.
package validator

import (
	"errors"
	"fmt"
	"reflect"
	"slices"

	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/stabi"
)

// The original attempt activation may survive this one rate-only amendment;
// its replay always receives the original document and strict hash verifier.
func releaseMeasurementReplayPolicy(options ReleaseMeasurementV2Options) (protocol.Policy, error) {
	if options.ReplayPolicy == nil {
		return options.Policy, nil
	}
	if !reflect.DeepEqual(*options.ReplayPolicy, options.Policy) {
		if err := protocol.ValidateTestnetRateAmendment(options.ReplayPolicy, &options.Policy); err != nil {
			return protocol.Policy{}, err
		}
	}
	policy := *options.ReplayPolicy
	policy.Deposit.Tiers = slices.Clone(policy.Deposit.Tiers)
	return policy, nil
}

// A rate amendment crosses an actual settlement boundary. Every other
// identity and predecessor check remains the lineage verifier's obligation.
func verifyReleasePolicyLineage(previous, current *ReleaseMeasurementArtifact) error {
	if previous.PolicyHash == current.PolicyHash {
		return nil
	}
	if current.SettlementEpoch <= previous.SettlementEpoch {
		return errors.New("release policy changed without a forward settlement boundary")
	}
	if err := protocol.ValidateTestnetRateAmendment(&previous.Policy, &current.Policy); err != nil {
		return fmt.Errorf("release policy lineage: %w", err)
	}
	return nil
}

// Exact on-chain policy fields and the boundary's selected document remain
// independently checked even when the activation was signed under its parent.
func validateReleaseStartupBoundaryPolicyV2(cfg *ReleaseConfig, domain protocol.ValidatorEvidenceDomain, boundary AttemptBoundary, expectedHash string, actual stabi.STCoordinatorPolicySnapshot) error {
	if actual.EffectiveEpoch > boundary.SettlementEpoch || actual.EffectiveBlock == 0 || actual.EpochBlocks == 0 {
		return errors.New("startup history policy differs from independent deployment authority")
	}
	if cfg == nil {
		if actual.PolicyHash != domain.PolicyHash {
			return errors.New("startup history policy differs from independent deployment authority")
		}
		return nil
	}
	activationPolicy, err := ReleasePolicyForHash(cfg, releaseHex32(domain.PolicyHash))
	if err != nil {
		return err
	}
	observedPolicy, err := ReleasePolicyForHash(cfg, releaseHex32(actual.PolicyHash))
	if err != nil {
		return err
	}
	if actual.PolicyHash != domain.PolicyHash {
		if err := protocol.ValidateTestnetRateAmendment(&activationPolicy, &observedPolicy); err != nil {
			return err
		}
	}
	if expectedHash != "" {
		expected, err := canonicalAttemptHex32("startup decision policy", expectedHash, false)
		if err != nil || expected != actual.PolicyHash {
			return errors.Join(errors.New("startup history policy differs from its exact decision"), err)
		}
	}
	if cfg.PreviousPolicy == nil {
		// Existing same-policy callers retain their historical boundary checks.
		return nil
	}
	decisionDomain := domain
	decisionDomain.PolicyHash = actual.PolicyHash
	return validateReleaseDecisionChainV2Policy(releaseDecisionChainV2Query{domain: decisionDomain, boundary: boundary, policy: observedPolicy}, actual)
}

// Public evidence retains its signed activation namespace; only the exact
// independently supplied rate amendment may change its payload decision hash.
func validateValidatorEvidenceDepositAuditV2DecisionWithPolicy(decision ReleaseMeasurementV2Decision, domain protocol.ValidatorEvidenceDomain, window protocol.ValidatorEvidenceWindow, current, previous *protocol.Policy) error {
	if current == nil {
		if previous != nil {
			return errors.New("deposit audit predecessor lacks current policy authority")
		}
		return validateValidatorEvidenceDepositAuditV2Decision(decision, domain, window)
	}
	currentHash, err := current.HashHex()
	if err != nil {
		return err
	}
	cfg := ReleaseConfig{Policy: *current, PolicyHash: currentHash, PreviousPolicy: previous}
	replay, err := ReleasePolicyForHash(&cfg, releaseHex32(domain.PolicyHash))
	if err != nil {
		return err
	}
	selected, err := ReleasePolicyForHash(&cfg, decision.PolicyHash)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(replay, selected) {
		if err := protocol.ValidateTestnetRateAmendment(&replay, &selected); err != nil {
			return err
		}
	}
	// This projection validates payload identity only. The original signed
	// header and on-chain activation are checked against the unmodified domain.
	decisionDomain := domain
	decisionDomain.PolicyHash, err = selected.Hash()
	if err != nil {
		return err
	}
	return validateValidatorEvidenceDepositAuditV2Decision(decision, decisionDomain, window)
}
