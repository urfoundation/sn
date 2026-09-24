package main

import (
	"errors"
	"fmt"
	"math/big"
)

// A rollover uses unallocated headroom under the source plan's lifetime caps.
// The full approved and superseded spend remains reserved, and all additional
// signed liabilities are conservatively charged again before the four actions.
func validatePolicyRolloverBudgetV2(cfg *ResolvedConfig, stateDir string, base *SetupPlan, p *policyRolloverPlanV2, entries []JournalEntry) error {
	if cfg == nil || cfg.Config == nil || base == nil || p == nil {
		return errors.New("rollover lifetime budget owner is absent")
	}
	prior := make([]JournalEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.PlanHash != p.PlanHash {
			prior = append(prior, entry)
		}
	}
	external, err := readFleetRenewalQueueTransactions(cfg, stateDir)
	if err != nil {
		return err
	}
	exposure, err := fleetRenewalCampaignExposure(stateDir, base, prior, external)
	if err != nil {
		return err
	}
	return validatePolicyRolloverLifetimeBudgetV2(base, p, exposure.Liability)
}

func validatePolicyRolloverLifetimeBudgetV2(base *SetupPlan, p *policyRolloverPlanV2, additional DecimalUint) error {
	if base == nil || p == nil {
		return errors.New("rollover lifetime budget owner is absent")
	}
	maximum, err := p.MaximumGasWei.Big()
	if err != nil || maximum.Sign() <= 0 {
		return errors.Join(errors.New("rollover gas maximum is invalid"), err)
	}
	liability, err := additional.Big()
	if err != nil {
		return err
	}
	required := new(big.Int).Add(liability, maximum)
	// Charge wei to the independent total-TAO cap too, rounding upward. Existing
	// funding is retained in full; no campaign or relay earmark is reassigned.
	rao, err := precompileRecoverySupplementalTao(required)
	if err != nil {
		return err
	}
	retained, err := addSpends(base.MaximumSpend, base.SupersededSpend)
	if err != nil {
		return err
	}
	total, err := addSpends(retained, Spend{TAORao: rao, EVMGasWei: DecimalUint(required.String())})
	if err != nil {
		return err
	}
	comparison, err := total.EVMGasWei.Cmp(base.Limits.EVMGasWei)
	if err != nil || comparison > 0 || total.TAORao > base.Limits.TAORao || total.AlphaRao > base.Limits.AlphaRao || total.Registrations > base.Limits.Registrations || total.SubnetCreations > base.Limits.SubnetCreations {
		return errors.Join(fmt.Errorf("rollover lifetime liability exceeds approved caps: total=%+v limits=%+v", total, base.Limits), err)
	}
	return nil
}
