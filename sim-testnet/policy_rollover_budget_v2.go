package main

import (
	"errors"
	"fmt"
	"math/big"
)

// A rollover consumes the existing campaign reserve. Its full four-action
// maximum remains reserved across partial completion; all older generations,
// retained transactions and external queues remain separate liabilities.
func validatePolicyRolloverBudgetV2(cfg *ResolvedConfig, stateDir string, base *SetupPlan, p *policyRolloverPlanV2, entries []JournalEntry) error {
	reserve, err := exactPlanActionByID(base, "campaign.evm-gas-reserve")
	if err != nil {
		return err
	}
	if reserve.Kind != "budget-reserve" {
		return errors.New("rollover has no existing campaign gas reserve")
	}
	reserved, err := reserve.Spend.EVMGasWei.Big()
	if err != nil {
		return err
	}
	maximum, err := p.MaximumGasWei.Big()
	if err != nil || maximum.Sign() <= 0 {
		return errors.Join(errors.New("rollover gas maximum is invalid"), err)
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
	liability, err := exposure.Liability.Big()
	if err != nil {
		return err
	}
	required := new(big.Int).Add(liability, maximum)
	if required.Cmp(reserved) > 0 {
		return fmt.Errorf("rollover requires %s wei plus retained campaign liability %s, exceeding approved reserve %s", maximum, liability, reserved)
	}
	return nil
}
