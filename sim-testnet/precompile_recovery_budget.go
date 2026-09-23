// Supplemental reserve is an exact dual-signed allocation within the retained
// lifetime caps. It changes neither setup intent nor the custody repair bounds.
package main

import (
	"errors"
	"math/big"
)

// The two lifetime dimensions are independent. Charging every supplemental wei
// to both is conservative even when it funds a value-bearing reseed.
type PrecompileRecoverySupplementalBudget struct {
	Spend          Spend `json:"spend"`
	RetainedSpend  Spend `json:"retained_spend"`
	ApprovedLimits Spend `json:"approved_limits"`
}

// Existing reserve is consumed first. The signed extension covers exactly the
// observed shortfall, including already signed liabilities, and no new margin.
func allocatePrecompileRecoveryBudget(plan *SetupPlan, budget *PrecompileRecoveryBudget, reserved, liability, maximum *big.Int) error {
	if plan == nil || budget == nil || reserved == nil || liability == nil || maximum == nil || reserved.Sign() < 0 || liability.Sign() < 0 || maximum.Sign() <= 0 {
		return errors.New("probe recovery allocation has invalid retained amounts")
	}
	effective := new(big.Int).Set(reserved)
	shortfall := new(big.Int).Sub(new(big.Int).Add(liability, maximum), reserved)
	if shortfall.Sign() > 0 {
		tao, err := precompileRecoverySupplementalTao(shortfall)
		if err != nil {
			return err
		}
		retained, err := addSpends(plan.MaximumSpend, plan.SupersededSpend)
		if err != nil {
			return err
		}
		budget.Schema = "urnetwork-precompile-recovery-budget-v2"
		budget.Supplemental = &PrecompileRecoverySupplementalBudget{
			Spend:         Spend{TAORao: tao, EVMGasWei: DecimalUint(shortfall.String())},
			RetainedSpend: retained, ApprovedLimits: plan.Limits,
		}
		effective.Add(effective, shortfall)
	}
	budget.AvailableWei = DecimalUint(new(big.Int).Sub(effective, liability).String())
	if _, err := precompileRecoveryEffectiveReserve(*budget); err != nil {
		return err
	}
	return validatePrecompileRecoverySupplementalPlan(plan, *budget)
}

// Round wei upward to native rao rather than letting a fractional rao disappear
// from the independent total-TAO cap. Arbitrarily large integers cannot wrap.
func precompileRecoverySupplementalTao(amount *big.Int) (uint64, error) {
	if amount == nil || amount.Sign() <= 0 {
		return 0, errors.New("probe supplemental allowance is not positive")
	}
	rao := new(big.Int).Add(amount, big.NewInt(999_999_999))
	rao.Quo(rao, big.NewInt(1_000_000_000))
	if !rao.IsUint64() {
		return 0, errors.New("probe supplemental total-TAO allowance overflows")
	}
	return rao.Uint64(), nil
}

// Self-contained validation lets offline receipt and completion verifiers reject
// a substituted amount or cap even before matching the retained plan.
func precompileRecoveryEffectiveReserve(budget PrecompileRecoveryBudget) (*big.Int, error) {
	reserved, err := budget.CampaignReserveWei.Big()
	if err != nil {
		return nil, err
	}
	if budget.Supplemental == nil {
		if budget.Schema != "urnetwork-precompile-recovery-budget-v1" {
			return nil, errors.New("probe recovery budget omitted its supplemental allowance")
		}
		return reserved, nil
	}
	if budget.Schema != "urnetwork-precompile-recovery-budget-v2" {
		return nil, errors.New("probe recovery budget has an unsigned legacy supplemental allowance")
	}
	supplemental := budget.Supplemental
	amount, err := supplemental.Spend.EVMGasWei.Big()
	if err != nil {
		return nil, err
	}
	tao, err := precompileRecoverySupplementalTao(amount)
	if err != nil || supplemental.Spend.TAORao != tao || supplemental.Spend.AlphaRao != 0 || supplemental.Spend.Registrations != 0 || supplemental.Spend.SubnetCreations != 0 {
		return nil, errors.Join(errors.New("probe recovery supplemental spend changed its permitted dimensions"), err)
	}
	liability, err := budget.CommittedOrPendingMaxWei.Big()
	if err != nil {
		return nil, err
	}
	maximum, err := budget.MaximumRecoveryWei.Big()
	if err != nil {
		return nil, err
	}
	shortfall := new(big.Int).Sub(new(big.Int).Add(liability, maximum), reserved)
	if shortfall.Sign() <= 0 || amount.Cmp(shortfall) != 0 {
		return nil, errors.New("probe recovery supplemental allowance differs from its exact shortfall")
	}
	total, err := addSpends(supplemental.RetainedSpend, supplemental.Spend)
	if err != nil {
		return nil, err
	}
	comparison, err := total.EVMGasWei.Cmp(supplemental.ApprovedLimits.EVMGasWei)
	if err != nil || comparison > 0 || total.TAORao > supplemental.ApprovedLimits.TAORao || total.AlphaRao > supplemental.ApprovedLimits.AlphaRao || total.Registrations > supplemental.ApprovedLimits.Registrations || total.SubnetCreations > supplemental.ApprovedLimits.SubnetCreations {
		return nil, errors.Join(errors.New("probe recovery supplemental allowance exceeds an approved lifetime cap"), err)
	}
	return new(big.Int).Add(reserved, amount), nil
}

// The same exact retained plan anchors both lifetime dimensions; publishing the
// repair approval cannot replace a cap, erase superseded spend or revise setup.
func validatePrecompileRecoverySupplementalPlan(plan *SetupPlan, budget PrecompileRecoveryBudget) error {
	if budget.Supplemental == nil {
		return nil
	}
	if plan == nil {
		return errors.New("probe recovery supplemental allowance has no retained plan")
	}
	retained, err := addSpends(plan.MaximumSpend, plan.SupersededSpend)
	if err != nil {
		return err
	}
	retainedEqual, err := equalSpend(retained, budget.Supplemental.RetainedSpend)
	if err != nil || !retainedEqual {
		return errors.Join(errors.New("probe recovery supplemental allowance changed retained lifetime spend"), err)
	}
	limitsEqual, err := equalSpend(plan.Limits, budget.Supplemental.ApprovedLimits)
	if err != nil || !limitsEqual {
		return errors.Join(errors.New("probe recovery supplemental allowance changed approved lifetime caps"), err)
	}
	_, err = precompileRecoveryEffectiveReserve(budget)
	return err
}
