//go:build linux || darwin

package main

// Hard-cap revisions preserve the original role-funding allocation. A relay
// expansion separately adds only its exact reserve delta to the action budget.

import "errors"

// Legacy plans allocated their entire original EVM cap. Once cap and funding
// differ, the approved plan records that allocation so later hotfixes do not
// accidentally use the larger cap as a fresh funding target.
func planRevisionEVMFundingAllocation(cfg *ResolvedConfig, prior *SetupPlan) (DecimalUint, error) {
	if cfg == nil || prior == nil {
		return "", errors.New("funding revision has no configured prior approval")
	}
	allocation := prior.EVMFundingAllocationWei
	if allocation.IsZero() {
		allocation = prior.Limits.EVMGasWei
	}
	comparison, err := allocation.Cmp(prior.Limits.EVMGasWei)
	if err != nil || allocation.IsZero() || comparison > 0 {
		return "", errors.Join(errors.New("prior funding allocation exceeds its approved hard cap"), err)
	}
	comparison, err = allocation.Cmp(cfg.MaximumEVMGasWei)
	if err != nil {
		return "", err
	}
	if comparison > 0 {
		allocation = cfg.MaximumEVMGasWei
	}
	return allocation, nil
}
