// The combined owner consumes actual stake causes from the original replay.
// Unknown transitions stay hot; a complete contiguous prefix can retire only
// into authenticated original checkpoints, with exact boundary stock retained.
package main

import (
	"errors"
	"reflect"
)

// Recipient effects stay with the aggregate so original liquid earnings can be
// checked again without importing any detached caller-authored amount report.
type economicConservationPrincipalExecution struct {
	Projection nativePrincipalExecutionProjection `json:"original_execution_effects"`
	Outcome    nativeExecutionOutcome             `json:"original_native_outcome"`
}

type economicConservationPrincipalArchive struct {
	Blocks  uint64                           `json:"complete_original_blocks"`
	Through economicEmissionBoundary         `json:"through"`
	After   []historicalPrincipalObservation `json:"last_original_stake_queries"`
	Pools   []nativePrincipalPoolEffects     `json:"cumulative_original_pool_effects"`
}

type economicConservationPrincipalEffectSummary struct {
	Retained  *economicConservationPrincipalRetained `json:"retained_original_effects,omitempty"`
	Archived  *economicConservationPrincipalArchive  `json:"archived_complete_effects,omitempty"`
	Active    []nativePrincipalReconciliation        `json:"active_original_effects"`
	Through   economicEmissionBoundary               `json:"through"`
	Current   bool                                   `json:"complete_through_native_cursor"`
	Authority string                                 `json:"authority"`
}

func (self *economicConservationState) appendPrincipalEffects(policy economicConservationPolicy, outcome nativeExecutionOutcome, native *monitorEconomicNativeState) error {
	authority := policy.Native.Observation.Execution.Principal
	if authority == nil || authority.Effects == nil {
		if outcome.PrincipalEffects != nil {
			return errors.New("economic principal effect lacks original independent authority")
		}
		return nil
	}
	nativePolicy, err := native.runtimeReadPolicy(policy.Native)
	if err != nil {
		return err
	}
	executionPolicy, err := nativePolicy.runtimeAdmission.principalExecutionPolicy(policy.Native.Observation, outcome)
	if err != nil {
		return err
	}
	if err := outcome.PrincipalEffects.validate(executionPolicy, outcome); err != nil {
		return err
	}
	for _, query := range outcome.PrincipalEffects.Authority.Queries {
		matched := nativeTreasuryPrincipalQuery(policy.Native.Observation.Execution.Treasury, query, policy.Native.Observation.Netuid)
		for _, route := range policy.Routes {
			if route.Kind != "tail-pool" {
				continue
			}
			expected, err := economicConservationPrincipalQuery(route, policy.Native.Observation.Netuid)
			if err != nil {
				return err
			}
			matched = matched || query == expected
		}
		if !matched {
			return errors.New("economic principal effects substituted original pool route")
		}
	}
	value := economicConservationPrincipalExecution{Projection: *outcome.PrincipalEffects, Outcome: outcome}
	value.Outcome.PrincipalEffects, value.Outcome.OpeningPrincipals, value.Outcome.Yuma = nil, nil, nil
	value.Outcome.CertifiedWindow = nil
	value.Outcome.FeeCensus = nil
	if self.facts()+value.facts() > policy.MaximumFacts {
		return errMonitorEconomicCapacity
	}
	self.PrincipalExecutions = append(self.PrincipalExecutions, value)
	return nil
}

func (self economicConservationPrincipalExecution) facts() uint64 {
	return uint64(len(self.Projection.Before)+len(self.Projection.After)+len(self.Projection.Mutations)+len(self.Projection.Effects)) + 1
}

func (self economicConservationState) principalEffectFacts() uint64 {
	count := uint64(0)
	for _, value := range self.PrincipalExecutions {
		count += value.facts()
	}
	return count
}

// Every block must connect to its original predecessor, including no-emission
// blocks. An absent API result stays absent across that comparison.
func (self economicConservationState) validatePrincipalEffects(policy economicConservationPolicy) error {
	authority := policy.Native.Observation.Execution.Principal
	var archived *economicConservationPrincipalArchive
	if self.Archive != nil {
		archived = self.Archive.PrincipalEffects
	}
	if authority == nil || authority.Effects == nil {
		if archived != nil || self.PrincipalExecutions != nil || self.Archive != nil && self.Archive.PrincipalRetained != nil {
			return errors.New("economic principal effects appeared under legacy authority")
		}
		return nil
	}
	nativePolicy, err := self.Native.runtimeReadPolicy(policy.Native)
	if err != nil {
		return err
	}
	previous := authority.Parent
	var after []historicalPrincipalObservation
	if archived != nil {
		if archived.Blocks == 0 || archived.Through.Number-authority.Parent.Number != archived.Blocks || archived.Through.Number < authority.Parent.Number || archived.Through.Number > self.Native.Cursor.Number || !rootCanonicalHash(archived.Through.Hash) || len(archived.Pools) != len(authority.Queries) {
			return errors.New("economic principal archive changed its complete original interval")
		}
		if err := validateHistoricalPrincipalReport(authority.Queries, archived.After); err != nil {
			return err
		}
		previous, after = archived.Through, archived.After
		for index, pool := range archived.Pools {
			if pool.Query != authority.Queries[index] || !pool.Complete || pool.Before == nil || pool.After == nil || pool.Residual == nil || *pool.Residual != "0" {
				return errors.New("economic principal archive summarized unknown effects")
			}
			treasury := nativeTreasuryPrincipalQuery(policy.Native.Observation.Execution.Treasury, pool.Query, policy.Native.Observation.Netuid)
			if treasury != (pool.CapturedEarnings != nil) {
				return errors.New("principal archive changed treasury collateral role")
			}
			if pool.CapturedEarnings != nil {
				if _, err := monitorEconomicInteger(*pool.CapturedEarnings); err != nil {
					return err
				}
			}
			for _, amount := range []string{pool.Deposits, pool.Withdrawals, pool.Refunds, pool.VaultCaptures, pool.NativeEarnings} {
				if _, err := monitorEconomicInteger(amount); err != nil {
					return err
				}
			}
		}
	}
	if self.Archive != nil && self.Archive.PrincipalRetained != nil {
		head := self.Archive.PrincipalRetained
		if err := head.validate(policy, previous, after, self.Native.Cursor); err != nil {
			return err
		}
		previous, after = head.Through, head.After
	}
	for _, value := range self.PrincipalExecutions {
		if value.Outcome.PrincipalEffects != nil || value.Outcome.OpeningPrincipals != nil || value.Projection.Parent != previous || after != nil && !reflect.DeepEqual(after, value.Projection.Before) {
			return errors.New("economic principal effects lost original contiguous stock lineage")
		}
		executionPolicy, err := nativePolicy.runtimeAdmission.principalExecutionPolicy(policy.Native.Observation, value.Outcome)
		if err != nil {
			return err
		}
		if err := errors.Join(value.Projection.validate(executionPolicy, value.Outcome), value.Outcome.RecipientEffects.validate(value.Outcome)); err != nil {
			return err
		}
		if self.archiveView != nil && self.archiveView.principalExecutionHashes[value.Projection.Boundary.Hash] != "" {
			return errors.New("economic principal execution repeats an archived original")
		}
		previous, after = value.Projection.Boundary, value.Projection.After
	}
	if previous != self.Native.Cursor {
		return errors.New("economic principal effects omitted an original executed block")
	}
	return nil
}

// Merge only fully reconciled contiguous original executions. Complete original
// evidence remains in its owned checkpoint; these amounts are a checked index.
func mergeEconomicPrincipalArchive(archive *economicConservationPrincipalArchive, value economicConservationPrincipalExecution) (*economicConservationPrincipalArchive, error) {
	result, err := value.Projection.reconcile(value.Outcome)
	if err != nil {
		return nil, err
	}
	if result.Status != "original-stake-cause-census-reconciled" {
		return nil, errors.New("economic principal retirement cannot discard unresolved effects")
	}
	next := &economicConservationPrincipalArchive{Blocks: 1, Through: value.Projection.Boundary, After: append([]historicalPrincipalObservation{}, value.Projection.After...), Pools: append([]nativePrincipalPoolEffects{}, result.Pools...)}
	if archive == nil {
		return next, nil
	}
	if archive.Through != value.Projection.Parent || !reflect.DeepEqual(archive.After, value.Projection.Before) || len(archive.Pools) != len(next.Pools) {
		return nil, errors.New("economic principal retirement changed original predecessor")
	}
	next.Blocks += archive.Blocks
	for index := range next.Pools {
		prior, pool := archive.Pools[index], &next.Pools[index]
		if prior.Query != pool.Query {
			return nil, errors.New("economic principal retirement changed original query")
		}
		if (pool.CapturedEarnings == nil) != (prior.CapturedEarnings == nil) {
			return nil, errors.New("principal retirement changed treasury collateral authority")
		}
		if pool.CapturedEarnings != nil {
			total, err := economicConservationSum(*pool.CapturedEarnings, *prior.CapturedEarnings)
			if err != nil {
				return nil, err
			}
			pool.CapturedEarnings = &total
		}
		pool.Before = prior.Before
		for _, pair := range []struct {
			target *string
			prior  string
		}{{target: &pool.Deposits, prior: prior.Deposits}, {target: &pool.Withdrawals, prior: prior.Withdrawals}, {target: &pool.Refunds, prior: prior.Refunds}, {target: &pool.VaultCaptures, prior: prior.VaultCaptures}, {target: &pool.NativeEarnings, prior: prior.NativeEarnings}} {
			value, err := economicConservationSum(*pair.target, pair.prior)
			if err != nil {
				return nil, err
			}
			*pair.target = value
		}
		// Recompute the cumulative stock delta without adding signed deltas to
		// unsigned financial amounts or recounting intermediate stock.
		before, e1 := monitorEconomicInteger(*pool.Before)
		after, e2 := monitorEconomicInteger(*pool.After)
		if err := errors.Join(e1, e2); err != nil {
			return nil, err
		}
		delta := after.Sub(after, before).String()
		pool.StockChange = &delta
	}
	return next, nil
}

func (self *economicConservationState) retirePrincipalEffects() error {
	if self.Archive == nil {
		return errors.New("economic principal retirement requires an owned archive")
	}
	if self.Archive.PrincipalRetained != nil {
		// A complete later block cannot bridge an earlier unresolved interval.
		return nil
	}
	retired := 0
	for _, value := range self.PrincipalExecutions {
		result, err := value.Projection.reconcile(value.Outcome)
		if err != nil {
			return err
		}
		if result.Status != "original-stake-cause-census-reconciled" {
			break
		}
		next, err := mergeEconomicPrincipalArchive(self.Archive.PrincipalEffects, value)
		if err != nil {
			return err
		}
		self.Archive.PrincipalEffects = next
		retired++
	}
	if retired != 0 {
		self.PrincipalExecutions = append([]economicConservationPrincipalExecution{}, self.PrincipalExecutions[retired:]...)
	}
	return nil
}

func (self *economicConservationArchiveView) retainPrincipalEffects(original, compacted *economicConservationState) error {
	if err := self.indexPrincipalRetentions(original, compacted); err != nil {
		return err
	}
	if compacted.Archive == nil || compacted.Archive.PrincipalEffects == nil {
		return nil
	}
	through := compacted.Archive.PrincipalEffects.Through.Number
	for _, value := range original.PrincipalExecutions {
		if value.Projection.Boundary.Number > through {
			break
		}
		hash := rootObjectHash(value)
		if self.principalExecutionHashes[value.Projection.Boundary.Hash] != "" {
			return errors.New("economic principal archive repeated original execution")
		}
		next, err := mergeEconomicPrincipalArchive(self.principalEffects, value)
		if err != nil {
			return err
		}
		if err := self.charge(struct {
			Boundary string
			Hash     string
		}{Boundary: value.Projection.Boundary.Hash, Hash: hash}); err != nil {
			return err
		}
		// Future vault pages may lag this retired native block. Retain the
		// actual causal input in the bounded, authenticated private index.
		if err := self.charge(value); err != nil {
			return err
		}
		self.principalExecutions[value.Projection.Boundary.Number] = value
		self.principalExecutionHashes[value.Projection.Boundary.Hash] = hash
		self.principalEffects = next
	}
	if !reflect.DeepEqual(self.principalEffects, compacted.Archive.PrincipalEffects) {
		return errors.New("economic principal retired summary changed original evidence")
	}
	return nil
}

func (self economicConservationState) principalEffectsSummary(policy economicConservationPolicy) (*economicConservationPrincipalEffectSummary, error) {
	authority := policy.Native.Observation.Execution.Principal
	if authority == nil || authority.Effects == nil {
		return nil, nil
	}
	if err := self.validatePrincipalEffects(policy); err != nil {
		return nil, err
	}
	result := &economicConservationPrincipalEffectSummary{Through: authority.Parent, Current: true, Active: []nativePrincipalReconciliation{}, Authority: "admitted-original-runtime-cause-and-storage-scope-only"}
	if self.Archive != nil && self.Archive.PrincipalEffects != nil {
		result.Archived = self.Archive.PrincipalEffects
		result.Through = result.Archived.Through
	}
	if self.Archive != nil && self.Archive.PrincipalRetained != nil {
		result.Retained = self.Archive.PrincipalRetained
		result.Through = result.Retained.CompleteThrough
		result.Current = result.Retained.UnresolvedBlocks == 0
	}
	for _, value := range self.PrincipalExecutions {
		entry, err := value.Projection.reconcile(value.Outcome)
		if err != nil {
			return nil, err
		}
		result.Active = append(result.Active, entry)
		if entry.Status != "original-stake-cause-census-reconciled" {
			result.Current = false
		}
		if result.Current {
			result.Through = value.Projection.Boundary
		}
	}
	result.Current = result.Current && result.Through == self.Native.Cursor
	return result, nil
}
