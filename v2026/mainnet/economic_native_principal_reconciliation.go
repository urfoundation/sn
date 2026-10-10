// Reconcile the actual boundary stake with every committed causal transition.
// Complete means only the independently admitted query/storage scope; native
// policy conformance, vault finality and provider entitlement remain separate.
package main

import (
	"errors"
	"math/big"
)

type nativePrincipalPoolEffects struct {
	CapturedEarnings *string                  `json:"native_captured_earnings_alpha,omitempty"`
	Query            historicalPrincipalQuery `json:"original_identity"`
	Before           *string                  `json:"before_stock_alpha"`
	After            *string                  `json:"after_stock_alpha"`
	StockChange      *string                  `json:"net_stock_change_alpha"`
	Deposits         string                   `json:"principal_deposits_alpha"`
	Withdrawals      string                   `json:"principal_withdrawals_alpha"`
	Refunds          string                   `json:"principal_refunds_alpha"`
	VaultCaptures    string                   `json:"vault_stake_reductions_alpha"`
	NativeEarnings   string                   `json:"native_liquid_earnings_alpha"`
	Residual         *string                  `json:"unexplained_stock_change_alpha"`
	Complete         bool                     `json:"original_cause_census_complete"`
}

type nativePrincipalReconciliation struct {
	Pools              []nativePrincipalPoolEffects `json:"original_pool_effects"`
	UnmatchedMutations []uint64                     `json:"unclassified_mutation_ordinals"`
	Status             string                       `json:"status"`
}

// This recomputes sums from the admitted original effect evidence. Neither a
// zero residual nor a matching total waives an unclassified mutation or gap.
func (self nativePrincipalExecutionProjection) reconcile(outcome nativeExecutionOutcome) (nativePrincipalReconciliation, error) {
	result := nativePrincipalReconciliation{Pools: []nativePrincipalPoolEffects{}, UnmatchedMutations: []uint64{}, Status: "original-stake-cause-census-incomplete"}
	if len(self.Before) != len(self.Authority.Queries) || len(self.After) != len(self.Authority.Queries) {
		return result, errors.New("principal reconciliation omitted boundary query census")
	}
	known := map[historicalPrincipalQuery]bool{}
	used := map[uint64]bool{}
	for _, query := range self.Authority.Queries {
		known[query] = true
	}
	for _, effect := range self.Effects {
		if known[effect.Query] {
			used[effect.Original.Ordinal] = true
		}
	}
	for _, mutation := range self.Mutations {
		if !used[mutation.Ordinal] {
			result.UnmatchedMutations = append(result.UnmatchedMutations, mutation.Ordinal)
		}
	}
	allComplete := len(result.UnmatchedMutations) == 0
	for index, query := range self.Authority.Queries {
		pool := nativePrincipalPoolEffects{Query: query, Before: self.Before[index].OpeningStakeAlpha, After: self.After[index].OpeningStakeAlpha, Deposits: "0", Withdrawals: "0", Refunds: "0", VaultCaptures: "0", NativeEarnings: "0", Complete: len(result.UnmatchedMutations) == 0}
		current := pool.Before
		for _, effect := range self.Effects {
			if effect.Query != query {
				continue
			}
			if current == nil || *current != effect.Before {
				pool.Complete = false
			}
			after := effect.After
			current = &after
			var target *string
			switch effect.Kind {
			case "deposit":
				target = &pool.Deposits
			case "withdrawal":
				target = &pool.Withdrawals
			case "refund":
				target = &pool.Refunds
			case "vault-capture":
				target = &pool.VaultCaptures
			case "earning":
				target = &pool.NativeEarnings
			case "support":
				continue
			default:
				return result, errors.New("principal reconciliation encountered unreviewed cause")
			}
			sum, err := economicConservationSum(*target, effect.Amount)
			if err != nil {
				return result, err
			}
			*target = sum
		}
		if current == nil || pool.After == nil || *current != *pool.After {
			pool.Complete = false
		}
		if pool.Before != nil && pool.After != nil {
			before, e1 := monitorEconomicInteger(*pool.Before)
			after, e2 := monitorEconomicInteger(*pool.After)
			if err := errors.Join(e1, e2); err != nil {
				return result, err
			}
			net := new(big.Int).Sub(after, before)
			text := net.String()
			pool.StockChange = &text
			for _, term := range []struct {
				value string
				sign  int64
			}{{value: pool.Deposits, sign: -1}, {value: pool.Refunds, sign: -1}, {value: pool.NativeEarnings, sign: -1}, {value: pool.Withdrawals, sign: 1}, {value: pool.VaultCaptures, sign: 1}} {
				value, err := monitorEconomicInteger(term.value)
				if err != nil {
					return result, err
				}
				net.Add(net, new(big.Int).Mul(value, big.NewInt(term.sign)))
			}
			residual := net.String()
			pool.Residual = &residual
			if net.Sign() != 0 {
				pool.Complete = false
			}
		} else {
			pool.Complete = false
		}
		// Independent native recipient accounting already retained liquid versus
		// collateral. A principal cause cannot relabel deposits as native income.
		liquid, captured := new(big.Int), new(big.Int)
		if outcome.RecipientEffects == nil {
			pool.Complete = false
		} else {
			for _, effect := range outcome.RecipientEffects.Effects {
				if effect.Recipient.Coldkey != economicNativeFeeHash(query.Coldkey) {
					continue
				}
				if effect.Treasury != nil {
					// Collateral is staked at the original recipient before its
					// lock; only liquid rewards follow AutoStakeDestination.
					for _, part := range []struct {
						hotkey   string
						amount   string
						captured bool
					}{
						{hotkey: effect.Recipient.Hotkey, amount: effect.Collateral, captured: true},
						{hotkey: nativeTreasuryLiquidDestination(effect), amount: effect.Liquid},
					} {
						if part.hotkey != economicNativeFeeHash(query.Hotkey) {
							continue
						}
						value, err := monitorEconomicInteger(part.amount)
						if err != nil {
							return result, err
						}
						liquid.Add(liquid, value)
						if part.captured {
							captured.Add(captured, value)
						}
					}
					continue
				}
				if effect.Recipient.Hotkey != economicNativeFeeHash(query.Hotkey) {
					continue
				}
				value, err := monitorEconomicInteger(effect.Liquid)
				if err != nil {
					return result, err
				}
				liquid.Add(liquid, value)
			}
		}
		if liquid.String() != pool.NativeEarnings {
			pool.Complete = false
		}
		if outcome.Treasury != nil && [32]byte(query.Coldkey) == outcome.Treasury.Policy.MultisigAccount {
			value := captured.String()
			pool.CapturedEarnings = &value
			pool.NativeEarnings = new(big.Int).Sub(liquid, captured).String()
		}
		allComplete = allComplete && pool.Complete
		result.Pools = append(result.Pools, pool)
	}
	if allComplete {
		result.Status = "original-stake-cause-census-reconciled"
	}
	return result, nil
}
