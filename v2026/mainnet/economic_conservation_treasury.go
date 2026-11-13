// Treasury stock is reconciled from retained original stake causes. The current
// coldkey-wide availability API is reported once, independently of gross income
// and without treating principal, refunds or captured rewards as spendable yield.
package main

import (
	"errors"
	"math"
	"math/big"
	"reflect"
)

type economicConservationTreasurySummary struct {
	Income                    *nativeTreasuryAmounts                  `json:"original_income"`
	Through                   economicEmissionBoundary                `json:"through"`
	Positions                 []nativePrincipalPoolEffects            `json:"approved_position_causes"`
	PositionStockAlpha        *string                                 `json:"approved_position_stock_alpha"`
	Availability              *historicalStakeAvailabilityObservation `json:"original_coldkey_availability,omitempty"`
	AvailabilityKnown         bool                                    `json:"current_availability_known"`
	CauseCensusComplete       bool                                    `json:"approved_position_causes_complete"`
	StockCovered              bool                                    `json:"coldkey_stock_covered_by_approved_positions"`
	IncomeUnlockedOrSpent     *bool                                   `json:"native_income_unlocked_or_spent"`
	SpendableIncomeLowerAlpha *string                                 `json:"spendable_native_income_lower_alpha"`
	SpendableIncomeUpperAlpha *string                                 `json:"spendable_native_income_upper_alpha"`
	Authority                 string                                  `json:"authority"`
}

// The summary consumes the owned contiguous execution/archival state, never an
// imported balance report. Archived original checkpoints remain independently read.
func (self economicConservationState) treasurySummary(policy economicConservationPolicy) (*economicConservationTreasurySummary, error) {
	execution := policy.Native.Observation.Execution
	if execution == nil || execution.Treasury == nil {
		return nil, nil
	}
	if self.Native.ExecutionAccounting == nil || self.Native.ExecutionAccounting.Treasury == nil {
		return nil, nil
	}
	if err := self.validatePrincipalEffects(policy); err != nil {
		return nil, err
	}
	income := cloneNativeTreasuryAmounts(self.Native.ExecutionAccounting.Treasury)
	expected := newNativeTreasuryAmounts(execution.Treasury)
	if !sameNativeTreasuryAuthority(income, expected) {
		return nil, errors.New("treasury summary changed original economic approval")
	}
	result := &economicConservationTreasurySummary{Income: income, Through: execution.Principal.Parent, Positions: []nativePrincipalPoolEffects{}, CauseCensusComplete: true, Authority: "original-runtime-availability-and-retained-approved-position-causes"}
	var current []historicalPrincipalObservation
	pools := map[historicalPrincipalQuery]nativePrincipalPoolEffects{}
	appendPools := func(values []nativePrincipalPoolEffects) error {
		for _, value := range values {
			if !nativeTreasuryPrincipalQuery(execution.Treasury, value.Query, policy.Native.Observation.Netuid) {
				continue
			}
			if !value.Complete || value.Residual == nil || *value.Residual != "0" {
				result.CauseCensusComplete = false
			}
			prior, exists := pools[value.Query]
			if !exists {
				pools[value.Query] = value
				continue
			}
			if !reflect.DeepEqual(prior.After, value.Before) {
				return errors.New("treasury custody cause history skipped an original position")
			}
			for _, item := range []struct {
				target *string
				value  string
			}{
				{target: &prior.Deposits, value: value.Deposits}, {target: &prior.Withdrawals, value: value.Withdrawals}, {target: &prior.Refunds, value: value.Refunds}, {target: &prior.VaultCaptures, value: value.VaultCaptures}, {target: &prior.NativeEarnings, value: value.NativeEarnings},
			} {
				if err := nativeExecutionAdd(item.target, item.value, false); err != nil {
					return err
				}
			}
			if prior.CapturedEarnings == nil || value.CapturedEarnings == nil {
				return errors.New("treasury cause census lost its captured earning component")
			}
			captured, err := economicConservationSum(*prior.CapturedEarnings, *value.CapturedEarnings)
			if err != nil {
				return err
			}
			prior.CapturedEarnings = &captured
			if prior.Residual == nil || value.Residual == nil {
				prior.Residual = nil
			} else {
				residual := *prior.Residual
				if err := nativeExecutionAdd(&residual, *value.Residual, true); err != nil {
					return err
				}
				prior.Residual = &residual
			}
			prior.After, prior.Complete = value.After, prior.Complete && value.Complete
			if prior.Before != nil && prior.After != nil {
				before, err := monitorEconomicInteger(*prior.Before)
				if err != nil {
					return err
				}
				after, err := monitorEconomicInteger(*prior.After)
				if err != nil {
					return err
				}
				change := new(big.Int).Sub(after, before).String()
				prior.StockChange = &change
			}
			pools[value.Query] = prior
		}
		return nil
	}
	if self.Archive != nil && self.Archive.PrincipalEffects != nil {
		archive := self.Archive.PrincipalEffects
		if err := appendPools(archive.Pools); err != nil {
			return nil, err
		}
		current, result.Through = archive.After, archive.Through
	}
	if self.Archive != nil && self.Archive.PrincipalRetained != nil {
		head := self.Archive.PrincipalRetained
		if err := appendPools(head.Pools); err != nil {
			return nil, err
		}
		result.CauseCensusComplete = result.CauseCensusComplete && head.UnresolvedBlocks == 0
		current, result.Through = head.After, head.Through
	}
	for _, value := range self.PrincipalExecutions {
		reconciliation, err := value.Projection.reconcile(value.Outcome)
		if err != nil {
			return nil, err
		}
		if reconciliation.Status != "original-stake-cause-census-reconciled" {
			result.CauseCensusComplete = false
		}
		if err := appendPools(reconciliation.Pools); err != nil {
			return nil, err
		}
		current, result.Through = value.Projection.After, value.Projection.Boundary
	}
	result.CauseCensusComplete = result.CauseCensusComplete && result.Through == self.Native.Cursor
	stock, nonIncome, earnings, outflow := new(big.Int), new(big.Int), new(big.Int), new(big.Int)
	knownStock := true
	expectedPositions := 0
	for _, query := range execution.Principal.Queries {
		if !nativeTreasuryPrincipalQuery(execution.Treasury, query, policy.Native.Observation.Netuid) {
			continue
		}
		expectedPositions++
		pool, exists := pools[query]
		if !exists {
			result.CauseCensusComplete, knownStock = false, false
			continue
		}
		for _, pointer := range []**string{&pool.Before, &pool.After, &pool.StockChange, &pool.Residual, &pool.CapturedEarnings} {
			if *pointer != nil {
				text := **pointer
				*pointer = &text
			}
		}
		result.Positions = append(result.Positions, pool)
		if pool.CapturedEarnings == nil {
			return nil, errors.New("treasury original captured earnings are absent")
		}
		captured, err := monitorEconomicInteger(*pool.CapturedEarnings)
		if err != nil {
			return nil, err
		}
		earnings.Add(earnings, captured)
		if pool.Before == nil || pool.After == nil {
			knownStock = false
			continue
		}
		for _, part := range []struct {
			target *big.Int
			value  string
		}{
			{target: stock, value: *pool.After}, {target: nonIncome, value: *pool.Before}, {target: nonIncome, value: pool.Deposits}, {target: nonIncome, value: pool.Refunds}, {target: earnings, value: pool.NativeEarnings}, {target: outflow, value: pool.Withdrawals}, {target: outflow, value: pool.VaultCaptures},
		} {
			amount, err := monitorEconomicInteger(part.value)
			if err != nil {
				return nil, err
			}
			part.target.Add(part.target, amount)
		}
	}
	if len(result.Positions) != expectedPositions {
		result.CauseCensusComplete = false
	}
	if earnings.String() != income.Gross {
		result.CauseCensusComplete = false
	}
	if knownStock {
		value := stock.String()
		result.PositionStockAlpha = &value
	}
	for _, observation := range current {
		if !nativeTreasuryPrincipalQuery(execution.Treasury, observation.Query, policy.Native.Observation.Netuid) {
			continue
		}
		if observation.Availability == nil {
			return nil, errors.New("treasury current original availability was omitted")
		}
		if result.Availability == nil {
			// Detached copies prevent a display consumer mutating owned state.
			value := *observation.Availability
			for _, item := range []struct {
				target **string
				source *string
			}{
				{target: &value.TotalAlpha, source: observation.Availability.TotalAlpha}, {target: &value.LockedAlpha, source: observation.Availability.LockedAlpha}, {target: &value.AvailableAlpha, source: observation.Availability.AvailableAlpha},
			} {
				if item.source != nil {
					text := *item.source
					*item.target = &text
				}
			}
			result.Availability = &value
		} else if !reflect.DeepEqual(result.Availability, observation.Availability) {
			return nil, errors.New("treasury original coldkey-wide availability differs across queried positions")
		}
	}
	result.AvailabilityKnown = result.Through == self.Native.Cursor && result.Availability != nil && result.Availability.AvailableAlpha != nil && result.Availability.TotalAlpha != nil && result.Availability.LockedAlpha != nil
	if result.AvailabilityKnown && knownStock {
		total, err := monitorEconomicInteger(*result.Availability.TotalAlpha)
		if err != nil {
			return nil, err
		}
		// A saturated total cannot prove that selected positions cover custody.
		result.StockCovered = total.IsUint64() && total.Uint64() < math.MaxUint64 && total.Cmp(stock) == 0
	}
	if result.CauseCensusComplete && result.StockCovered && result.AvailabilityKnown {
		available, err := monitorEconomicInteger(*result.Availability.AvailableAlpha)
		if err != nil {
			return nil, err
		}
		// Fungible stock permits bounds, not a claim that opening principal is
		// native income. Any outflow can have consumed either funding source.
		lower := new(big.Int).Sub(available, nonIncome)
		if lower.Sign() < 0 {
			lower.SetInt64(0)
		}
		upper := new(big.Int).Set(earnings)
		if upper.Cmp(available) > 0 {
			upper.Set(available)
		}
		if lower.Cmp(upper) > 0 {
			return nil, errors.New("treasury original custody contradicts its income bounds")
		}
		held := new(big.Int).Sub(stock, available)
		if held.Sign() == 0 {
			value := true
			result.IncomeUnlockedOrSpent = &value
		} else if held.Cmp(nonIncome) > 0 {
			value := false
			result.IncomeUnlockedOrSpent = &value
		}
		low, high := lower.String(), upper.String()
		result.SpendableIncomeLowerAlpha, result.SpendableIncomeUpperAlpha = &low, &high
	}
	return result, nil
}
