// Each originally selected pool owns one bounded artifact read and result slot.
// Missing content cannot park another pool or the native/vault/Claim observers.
// Only the parent loop mutates checkpoint state; cancellation joins every read.
package main

import (
	"context"
	"errors"
	"slices"
	"time"
)

type economicEntitlementResult struct {
	id      string
	pool    string
	funding string
	census  *economicConservationEntitlementCensus
	err     error
}
type economicEntitlementPoolWorker struct {
	source economicConservationEntitlementSource
	client *rpcClient
	active bool
	cancel context.CancelFunc
	done   chan struct{}
	result chan economicEntitlementResult
}
type economicConservationEntitlementWorker struct {
	ctx    context.Context
	policy economicConservationPolicy
	budget time.Duration
	hooks  monitorServiceHooks
	pools  []*economicEntitlementPoolWorker
}

func newEconomicConservationEntitlementWorker(ctx context.Context, policy economicConservationPolicy, rpcUrl string, hooks monitorServiceHooks) (*economicConservationEntitlementWorker, error) {
	if policy.EntitlementSources == nil {
		return nil, nil
	}
	result := &economicConservationEntitlementWorker{ctx: ctx, policy: policy, budget: time.Duration(monitorEconomicReadSeconds(policy.ReadBudgetSeconds)) * time.Second, hooks: hooks}
	for _, source := range policy.EntitlementSources.Sources {
		client, err := newRpcClient(rpcUrl, result.budget)
		if err != nil {
			return nil, errors.Join(err, result.close())
		}
		if hooks.rpcWait != nil {
			client.retryWait = func(ctx context.Context, delay time.Duration) error {
				return hooks.rpcWait(ctx, "entitlement-"+source.PoolId, delay)
			}
		}
		result.pools = append(result.pools, &economicEntitlementPoolWorker{source: source, client: client, result: make(chan economicEntitlementResult, 1)})
	}
	return result, nil
}

func (self *economicConservationEntitlementWorker) start(state *economicConservationState) {
	if self == nil || self.ctx.Err() != nil {
		return
	}
	capacityBasis, err := state.entitlementCapacityBasis(self.policy)
	if err != nil {
		return
	}
	resources, err := state.resources(self.policy)
	if err != nil {
		return
	}
	for _, pool := range self.pools {
		if pool.active {
			continue
		}
		candidates := []economicConservationEntitlement{}
		for _, record := range state.Entitlements {
			if record.PoolId == pool.source.PoolId && record.censusHash() == "" && !record.CensusHeld && record.Finalization != nil && (record.CensusCapacityBasis == "" || record.CensusCapacityBasis != capacityBasis) {
				candidates = append(candidates, record)
			}
		}
		if len(candidates) == 0 {
			continue
		}
		slices.SortFunc(candidates, func(a, b economicConservationEntitlement) int {
			if a.Id < b.Id {
				return -1
			}
			if a.Id > b.Id {
				return 1
			}
			return 0
		})
		record := candidates[0]
		for _, candidate := range candidates {
			if candidate.Id > state.EntitlementReadAfter[pool.source.PoolId] {
				record = candidate
				break
			}
		}
		// Clone the only slice/map members read by the child before publication.
		record.Sources = slices.Clone(record.Sources)
		original := *record.Finalization
		original.Values = make(map[string]string, len(record.Finalization.Values))
		for key, value := range record.Finalization.Values {
			original.Values[key] = value
		}
		record.Finalization = &original
		budget := time.Duration(resources.ReadBudgetSeconds) * time.Second
		pool.client.retryWindow = budget
		ctx, cancel := context.WithTimeout(self.ctx, budget)
		pool.cancel, pool.done, pool.active = cancel, make(chan struct{}), true
		go func(pool *economicEntitlementPoolWorker, record economicConservationEntitlement) {
			defer close(pool.done)
			defer cancel()
			value, err := readEconomicEntitlementCensus(ctx, self.policy, pool.source, record, pool.client)
			pool.result <- economicEntitlementResult{id: record.Id, pool: record.PoolId, funding: economicEntitlementFundingHash(record), census: value, err: err}
			if self.hooks.afterEntitlementRead != nil {
				self.hooks.afterEntitlementRead(ctx, record.Id, err)
			}
		}(pool, record)
	}
}

func (self *economicConservationEntitlementWorker) take(wait bool) []economicEntitlementResult {
	if self == nil {
		return nil
	}
	var results []economicEntitlementResult
	for _, pool := range self.pools {
		if !pool.active {
			continue
		}
		if wait {
			<-pool.done
		}
		select {
		case result := <-pool.result:
			<-pool.done
			pool.active = false
			results = append(results, result)
		default:
		}
	}
	return results
}

func (self *economicConservationEntitlementWorker) close() error {
	if self == nil {
		return nil
	}
	for _, pool := range self.pools {
		if pool.active {
			pool.cancel()
		}
	}
	var failures []error
	for _, result := range self.take(true) {
		if result.err != nil && !monitorOnlyCancellationCauses(result.err, 0) {
			failures = append(failures, result.err)
		}
	}
	for _, pool := range self.pools {
		pool.client.httpClient.CloseIdleConnections()
	}
	return errors.Join(failures...)
}

// Apply to the latest parent state. Another healthy domain may have progressed
// while this read ran; stale work cannot restore an earlier cursor or funding.
func applyEconomicEntitlementResult(ctx context.Context, policy economicConservationPolicy, state *economicConservationState, result economicEntitlementResult) (*economicConservationState, error) {
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(err, result.err)
	}
	next, err := cloneEconomicConservation(state, policy)
	if err != nil {
		return nil, err
	}
	if err := next.index(); err != nil {
		return nil, err
	}
	index, exists := next.entitlementIds[result.id]
	if !exists {
		return nil, errors.New("economic artifact handoff lost original unmatched entitlement")
	}
	record := &next.Entitlements[index]
	if record.PoolId != result.pool || economicEntitlementFundingHash(*record) != result.funding {
		return nil, errors.New("economic artifact handoff changed original funding")
	}
	if next.EntitlementReadAfter == nil {
		next.EntitlementReadAfter = map[string]string{}
	}
	next.EntitlementReadAfter[result.pool] = result.id
	err = result.err
	if err == nil && result.census == nil {
		err = monitorEvmIntegrity("economic artifact handoff omitted complete original evidence")
	}
	var candidate *economicProviderCandidate
	if source, exists := policy.entitlementSource(record.PoolId); err == nil && exists && source.ProviderMeasurements != nil {
		candidate, err = next.archiveView.beginProviderCandidate()
		if err == nil {
			defer candidate.finish(false)
		}
	}
	if err == nil {
		if record.censusHash() != "" && record.censusHash() != result.census.ContentHash {
			err = monitorEvmIntegrity("economic artifact handoff replaced retained original census")
		} else {
			record.Census = result.census
			err = next.archiveView.sealProviderOriginals(ctx, policy, next, record)
			if err == nil {
				err = result.census.validate(ctx, policy, *record)
			}
			if err == nil {
				operating, policyErr := next.operatingPolicy(policy)
				err = policyErr
				if err == nil && next.facts() > operating.MaximumFacts {
					err = errMonitorEconomicCapacity
				}
			}
			if err == nil {
				err = next.archiveView.admitEntitlementCensuses(ctx, policy, next)
			}
			if err == nil {
				err = next.reconcileEntitlementLeaves(ctx)
			}
			if err == nil {
				next.ContentHash = next.hash()
				err = next.validate(ctx, policy)
			}
			err = economicEntitlementEvidenceError(err)
		}
	}
	if err != nil {
		candidate.finish(false)
		// Refusal never removes old facts or manufactures a zero obligation.
		next, cloneErr := cloneEconomicConservation(state, policy)
		if cloneErr != nil {
			return nil, cloneErr
		}
		if next.EntitlementReadAfter == nil {
			next.EntitlementReadAfter = map[string]string{}
		}
		next.EntitlementReadAfter[result.pool] = result.id
		record := &next.Entitlements[index]
		record.CensusIssue = economicConservationIssue(err)
		record.CensusHeld = monitorEconomicEvmReadCode(err) == "identity-conflict"
		if errors.Is(err, errMonitorEconomicCapacity) {
			basis, basisErr := next.entitlementCapacityBasis(policy)
			if basisErr != nil {
				return nil, basisErr
			}
			record.CensusCapacityBasis = basis
		}
		return next, nil
	}
	candidate.finish(true)
	record.CensusIssue, record.CensusHeld, record.CensusCapacityBasis = "", false, ""
	return next, nil
}
