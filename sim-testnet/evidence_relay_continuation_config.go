//go:build linux || darwin

// Current capacity may describe an authenticated funded successor without
// changing its original liabilities, per-call price or custody.
package main

import "errors"

// Reconstruct the allocation that actually funded the retained fleet. The
// expanded reserve is carried verbatim later; sizing a fresh reserve here
// would incorrectly spend its current slot margin at the original fee.
func evidenceRelayRevisionFundingConfig(cfg *ResolvedConfig, prior *SetupPlan) (*ResolvedConfig, error) {
	if prior == nil || prior.EvidenceRelayContinuation == nil {
		return cfg, nil
	}
	if cfg == nil || cfg.Config == nil {
		return nil, errors.New("relay continuation revision configuration is absent")
	}
	if err := validateEvidenceRelayContinuationBudget(prior); err != nil {
		return nil, err
	}
	owner := *prior
	owner.ConfigHash = cfg.ConfigHash
	if err := validateEvidenceRelayContinuationConfig(cfg, &owner); err != nil {
		return nil, err
	}
	result := *cfg
	config := *cfg.Config
	config.ValidatorEvidenceRelay.MaxSlots = evidenceRelayOriginalSlots
	config.ValidatorEvidenceRelay.GasUnits = evidenceRelayContinuationGas
	result.Config = &config
	return &result, nil
}

// The full budget verifier runs before this configuration check. The original
// 256-slot template remains valid for retained approvals; only an exact funded
// v6 successor may expose its already approved 2048 slots in the current config.
func validateEvidenceRelayContinuationConfig(cfg *ResolvedConfig, plan *SetupPlan) error {
	if cfg == nil || cfg.Config == nil || plan == nil || plan.EvidenceRelayContinuation == nil || cfg.ConfigHash != plan.ConfigHash {
		return errors.New("relay continuation changed its current configured approval")
	}
	configured := cfg.Config.ValidatorEvidenceRelay
	if configured.GasUnits != evidenceRelayContinuationGas || cfg.Config.Budgets.MaximumEVMFeePerGasWei != evidenceRelayOriginalFee {
		return errors.New("relay continuation changed its original configured gas or fee ceiling")
	}
	if configured.MaxSlots == evidenceRelayOriginalSlots {
		return nil
	}
	continuation := plan.EvidenceRelayContinuation
	_, slots, err := continuation.feeTerms()
	if err != nil || (continuation.Schema != evidenceRelayContinuationSourceExpansionSchema && continuation.Schema != evidenceRelayContinuationGenerationSchema) || configured.MaxSlots != slots {
		return errors.Join(errors.New("relay continuation configured slots differ from its original or exact funded v6 approval"), err)
	}
	return nil
}
