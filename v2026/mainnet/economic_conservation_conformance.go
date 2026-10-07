// Mathematical conformance and authority are independent. A complete original
// denominator can disprove the fixed ten/ninety split; matching a selected
// execution and artifact does not prove independent finality or measured usage.
package main

import (
	"errors"
	"math/big"
)

// A measured contradiction is distinct from missing approval or source evidence.
type economicConservationConformance struct {
	TreasuryWithinTolerance     *bool    `json:"original_treasury_ninety_within_tolerance,omitempty"`
	TreasuryDeviationNumerator  *string  `json:"treasury_deviation_times_ten_alpha,omitempty"`
	TreasuryCustodyComplete     *bool    `json:"original_treasury_custody_complete,omitempty"`
	TreasuryIncomeSpendable     *bool    `json:"original_treasury_income_unlocked_or_spent,omitempty"`
	NativeSplitWithinTolerance  *bool    `json:"native_ten_ninety_within_tolerance"`
	OwnerRecycleWithinTolerance *bool    `json:"original_owner_ninety_within_tolerance"`
	ProviderDeviationNumerator  *string  `json:"provider_deviation_times_ten_alpha"`
	OwnerDeviationNumerator     *string  `json:"owner_deviation_times_ten_alpha"`
	NativeSplitTolerance        *string  `json:"complete_original_arithmetic_tolerance_alpha"`
	NoNonIncomeProviderCredit   *bool    `json:"provider_credit_excludes_non_income"`
	CapitalSubsidyAuthorized    bool     `json:"capital_subsidy_authorized"`
	CompleteEvidence            bool     `json:"complete_goal_evidence"`
	Missing                     []string `json:"missing_evidence"`
	Contradictions              []string `json:"observed_contradictions"`
}

// Integer-scaled comparison avoids rounding the target to a convenient amount.
// The full allocation model already includes final normalization and integer
// emission conversion. Its subset final-stage bound must not be added again.
func economicNativeSplit(denominator, provider, owner, allocationTolerance string) (bool, string, string, string, error) {
	d, err := monitorEconomicInteger(denominator)
	if err != nil {
		return false, "", "", "", err
	}
	p, err := monitorEconomicInteger(provider)
	if err != nil {
		return false, "", "", "", err
	}
	o, err := monitorEconomicInteger(owner)
	if err != nil {
		return false, "", "", "", err
	}
	bound, err := monitorEconomicInteger(allocationTolerance)
	if err != nil {
		return false, "", "", "", err
	}
	pd := new(big.Int).Sub(new(big.Int).Mul(p, big.NewInt(10)), d)
	od := new(big.Int).Sub(new(big.Int).Mul(o, big.NewInt(10)), new(big.Int).Mul(d, big.NewInt(9)))
	limit := new(big.Int).Mul(bound, big.NewInt(10))
	within := new(big.Int).Abs(pd).Cmp(limit) <= 0 && new(big.Int).Abs(od).Cmp(limit) <= 0
	return within, pd.String(), od.String(), allocationTolerance, nil
}

// This comparison accepts the signed scaled difference generated above. It
// does not classify unknown recipients just because their reward is nonzero.
func economicNativeDeviationWithin(deviation, tolerance string) (bool, error) {
	value, ok := new(big.Int).SetString(deviation, 10)
	if !ok || value.String() != deviation {
		return false, errors.New("native conformance deviation is not canonical")
	}
	bound, err := monitorEconomicInteger(tolerance)
	if err != nil {
		return false, err
	}
	return value.Abs(value).Cmp(bound.Mul(bound, big.NewInt(10))) <= 0, nil
}

// The public summary consumes already admitted witnesses and their exact
// boundaries. No external projection JSON, amount, or artifact signer can set
// the measurement/finality/whole-census predicates below.
func (self *economicConservationSummary) assessConformance() error {
	// The current public caller constructs a fresh summary. Reset owned
	// projections anyway so a later refresh cannot retain stale authority.
	self.TargetMet, self.NativeFeeWithdrawalRao, self.NativeFeeRefundRao = nil, nil, nil
	self.Conformance = nil
	self.MissingEvidence = nil
	if self.OriginalEntitlements != nil {
		self.OriginalEntitlements.NativeIncomeFundingAlpha, self.OriginalEntitlements.CapitalFundingAlpha = nil, nil
		self.OriginalEntitlements.CapitalSubsidyAuthorized = false
	}
	result := &economicConservationConformance{Missing: []string{}, Contradictions: []string{}}
	if self.Funding == nil {
		return errors.New("economic conformance lacks original funding accounting")
	}
	result.NoNonIncomeProviderCredit = self.Funding.NoNonIncomeProviderCredit
	if result.NoNonIncomeProviderCredit != nil && !*result.NoNonIncomeProviderCredit {
		result.Contradictions = append(result.Contradictions, "original-provider-credit-necessarily-consumes-unapproved-non-income")
	}
	if self.Execution != nil && self.Execution.Through == self.NativeCursor && self.Yuma != nil && self.Yuma.Current && self.Yuma.Through == self.NativeCursor && self.Yuma.MinerDenominator != nil && self.Yuma.FullQuantizationTolerance != nil {
		within, pd, od, q, err := economicNativeSplit(*self.Yuma.MinerDenominator, self.Execution.ProviderEntitlement, self.Execution.OwnerRecycled, *self.Yuma.FullQuantizationTolerance)
		if self.Execution.Treasury != nil {
			allocated, sumErr := economicConservationSum(self.Execution.ProviderEntitlement, self.Execution.OwnerRecycled, self.Execution.ResidualEntitlement, self.Execution.Treasury.Gross)
			if sumErr != nil || *self.Yuma.MinerDenominator != allocated {
				return errors.Join(errors.New("treasury conformance changed the original complete miner allocation"), sumErr)
			}
			within, pd, od, q, err = economicTreasurySplit(*self.Execution, *self.Yuma.FullQuantizationTolerance)
		}
		if err != nil {
			return err
		}
		result.NativeSplitWithinTolerance = &within
		result.ProviderDeviationNumerator, result.OwnerDeviationNumerator, result.NativeSplitTolerance = &pd, &od, &q
		ownerWithin, err := economicNativeDeviationWithin(od, q)
		if err != nil {
			return err
		}
		result.OwnerRecycleWithinTolerance = &ownerWithin
		if self.Execution.Treasury != nil {
			result.OwnerRecycleWithinTolerance, result.OwnerDeviationNumerator = nil, nil
			result.TreasuryWithinTolerance, result.TreasuryDeviationNumerator = &ownerWithin, &od
			if self.Execution.OwnerRecycled != "0" {
				within = false
				result.NativeSplitWithinTolerance = &within
			}
		}
		if self.Execution.ResidualEntitlement != "0" {
			result.NativeSplitWithinTolerance = nil
			result.Missing = append(result.Missing, "complete-original-provider-recipient-membership")
		}
		if !ownerWithin || self.Execution.ResidualEntitlement == "0" && !within || self.Execution.Treasury != nil && self.Execution.OwnerRecycled != "0" {
			value := false
			result.NativeSplitWithinTolerance = &value
			label := "complete-native-miner-allocation-contradicts-provider-ten-owner-ninety"
			if self.Execution.Treasury != nil {
				label = "complete-native-miner-tranche-contradicts-provider-ten-treasury-remainder"
			}
			result.Contradictions = append(result.Contradictions, label)
		}
	} else {
		result.Missing = append(result.Missing, "complete-original-miner-denominator-and-all-quantization-stages")
	}
	if self.Execution != nil && self.Execution.Treasury != nil {
		complete := self.Treasury != nil && self.Treasury.Through == self.NativeCursor && self.Treasury.CauseCensusComplete && self.Treasury.StockCovered && self.Treasury.AvailabilityKnown
		result.TreasuryCustodyComplete = &complete
		if !complete {
			result.Missing = append(result.Missing, "complete-current-original-treasury-custody-and-availability")
		}
		if self.Treasury != nil {
			result.TreasuryIncomeSpendable = self.Treasury.IncomeUnlockedOrSpent
		}
		if result.TreasuryIncomeSpendable == nil {
			result.Missing = append(result.Missing, "original-treasury-income-unlocked-or-spent")
		} else if !*result.TreasuryIncomeSpendable {
			result.Contradictions = append(result.Contradictions, "original-treasury-native-income-remains-locked")
		}
	}
	if !self.NativeCurrent || !self.VaultCurrent || self.NativeHeld || self.VaultHeld || self.JoinIssue != "" {
		result.Missing = append(result.Missing, "current-original-native-and-vault-boundaries")
	}
	for _, status := range self.ClaimStatuses {
		if status != "ok" {
			result.Missing = append(result.Missing, "current-original-claim-expectations")
			break
		}
	}
	if self.OpeningPrincipals == nil || self.OpeningPrincipalAlpha == nil || self.PrincipalEffects == nil || !self.PrincipalEffects.Current {
		result.Missing = append(result.Missing, "original-opening-stock-and-complete-stake-causes")
	}
	if !self.Funding.Captured.Complete || !self.Funding.Accepted.Complete || !self.Funding.Paid.Complete || self.Funding.NoNonIncomeProviderCredit == nil {
		result.Missing = append(result.Missing, "complete-capture-carry-credit-and-payment-source-composition")
	}
	if self.OriginalEntitlements == nil || !self.OriginalEntitlements.CompleteObservedCensus {
		result.Missing = append(result.Missing, "complete-original-authorized-root-and-leaf-census")
	}
	if self.OriginalEntitlements == nil || !self.OriginalEntitlements.ProviderMeasurementsAuthenticated {
		result.Missing = append(result.Missing, "independent-original-provider-measurements")
	}
	if self.OriginalEntitlements == nil || !self.OriginalEntitlements.IndependentFinalityAuthenticated {
		result.Missing = append(result.Missing, "independent-vault-and-entitlement-finality")
	}
	if self.OriginalFees == nil || !self.OriginalFees.Complete || self.OriginalFees.Head.Through != self.NativeCursor || self.OriginalFees.WithdrawalRao == nil || self.OriginalFees.RefundRao == nil {
		result.Missing = append(result.Missing, "whole-original-provider-native-fee-withdrawal-refund-census")
	} else {
		self.NativeFeeWithdrawalRao, self.NativeFeeRefundRao = self.OriginalFees.WithdrawalRao, self.OriginalFees.RefundRao
	}
	if self.OriginalEntitlements != nil {
		// This scope counts unique original capture sources, not the sum of
		// repeated finalizations which can legitimately transport prior carry.
		self.OriginalEntitlements.FundingComposition = "unique-original-captures; carry-and-payment-do-not-create-income"
		if self.Funding.Captured.Complete && self.Funding.Captured.MinimumIncome == self.Funding.Captured.MaximumIncome {
			income, capital := self.Funding.Captured.MinimumIncome, self.Funding.Captured.MinimumNonIncome
			self.OriginalEntitlements.NativeIncomeFundingAlpha, self.OriginalEntitlements.CapitalFundingAlpha = &income, &capital
		}
	}
	result.CompleteEvidence = len(result.Missing) == 0
	self.Conformance = result
	self.MissingEvidence = append([]string(nil), result.Missing...)
	if len(result.Contradictions) != 0 {
		value := false
		self.TargetMet = &value
	} else if result.CompleteEvidence && result.NativeSplitWithinTolerance != nil && *result.NativeSplitWithinTolerance && result.NoNonIncomeProviderCredit != nil && *result.NoNonIncomeProviderCredit {
		value := true
		self.TargetMet = &value
	}
	// Activation has its own reviewed deployment gate. This observational
	// predicate does not send funds, sign a contract action or launch a service.
	return nil
}

// Treasury reference rounding carries across the complete original miner
// tranche, including emission redirected to validators. Gross already contains
// collateral; current spendability is checked separately from this split.
func economicTreasurySplit(window nativeExecutionWindow, tolerance string) (bool, string, string, string, error) {
	if window.Treasury == nil {
		return false, "", "", "", errors.New("treasury split lacks original income")
	}
	if err := window.validate(); err != nil {
		return false, "", "", "", err
	}
	provider, ok := new(big.Int).SetString(window.ProviderDeviation, 10)
	if !ok || window.TreasuryDeviation == nil {
		return false, "", "", "", errors.New("treasury split lacks exact cumulative reference")
	}
	treasury, ok := new(big.Int).SetString(*window.TreasuryDeviation, 10)
	if !ok {
		return false, "", "", "", errors.New("treasury split deviation differs")
	}
	pd, td := provider.Mul(provider, big.NewInt(10)).String(), treasury.Mul(treasury, big.NewInt(10)).String()
	providerWithin, err := economicNativeDeviationWithin(pd, tolerance)
	if err != nil {
		return false, "", "", "", err
	}
	treasuryWithin, err := economicNativeDeviationWithin(td, tolerance)
	return providerWithin && treasuryWithin && window.OwnerRecycled == "0", pd, td, tolerance, err
}
