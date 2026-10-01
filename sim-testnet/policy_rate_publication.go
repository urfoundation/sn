//go:build linux || darwin

// Public evidence keeps its immutable activation domain while later decision
// readers independently authenticate the exact governed policy transition.
package main

import validatorcomponent "github.com/urfoundation/sn/v2026/validator"

// Supply owned policy documents only from the approved amendment, never from
// the discovered publication whose signature and subject are being checked.
func bindPolicyRatePublicationOptions(cfg *ResolvedConfig, plan *SetupPlan, options *validatorcomponent.ValidatorEvidencePublicationV2ReadOptions) error {
	if plan.PolicyRateAmendment == nil {
		return nil
	}
	resolved, err := configWithPolicyRateAmendment(cfg, plan)
	if err != nil {
		return err
	}
	current := *resolved.Policy
	current.Deposit.Tiers = append(current.Deposit.Tiers[:0:0], current.Deposit.Tiers...)
	options.Policy, options.PreviousPolicy = &current, resolved.previousPolicy
	return nil
}
