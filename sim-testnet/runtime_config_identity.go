// Runtime migration preserves signed activation identity without making an
// original runtime an authority for current reads, signing or execution.
package main

import "errors"

// Only the explicitly reviewed testnet455 to testnet458/459/460/461 transitions separate
// its configuration identity from the current runtime expectation.
func validateRuntimeConfigIdentity(public *PublicManifest) error {
	if public == nil {
		return errors.New("runtime configuration manifest is unavailable")
	}
	if public.Chain.ConfigIdentityRuntimeSpec == 0 {
		return nil
	}
	if public.SchemaVersion != 1 || public.Profile != releaseProfile ||
		public.Chain.ChainID != testnetChainID || public.Chain.GenesisHash != testnetGenesis ||
		public.Chain.ConfigIdentityRuntimeSpec != 455 || (public.Chain.ExpectedRuntimeSpec != 458 && public.Chain.ExpectedRuntimeSpec != 459 && public.Chain.ExpectedRuntimeSpec != 460 && public.Chain.ExpectedRuntimeSpec != 461) ||
		public.Chain.ExpectedTransactionVersion != 1 || public.Chain.ExpectedStateVersion != 1 {
		return errors.New("runtime configuration identity is not the explicit reviewed 455-to-458/459/460/461 transitions")
	}
	return nil
}

// Current execution and offline authority require the independently approved
// pin. Historical readers authenticate their original wire without this check.
func validateRuntimeConfigIdentityPlan(cfg *ResolvedConfig, plan *SetupPlan) error {
	if cfg == nil || plan == nil {
		return errors.New("runtime configuration approval is unavailable")
	}
	if err := validateRuntimeConfigIdentity(cfg.Public); err != nil {
		return err
	}
	if plan.ConfigIdentityRuntimeSpec != cfg.Public.Chain.ConfigIdentityRuntimeSpec {
		return errors.New("runtime configuration identity differs from the exact approved plan")
	}
	if plan.ConfigIdentityRuntimeSpec != 0 {
		if cfg.Release == nil || cfg.Public.Chain.ExpectedRuntimeSpec != reviewedRuntimeSpecVersion ||
			cfg.Release.Runtime.SpecVersion != reviewedRuntimeSpecVersion ||
			cfg.Release.Runtime.TransactionVersion != 1 || cfg.Release.Runtime.StateVersion != 1 {
			return errors.New("runtime configuration migration requires the exact current461 release")
		}
		return validateReviewedRuntimeIdentity(cfg.Release)
	}
	return nil
}
