// Original-parent stake is a separately admitted opening stock census. The
// combined owner retains the replay evidence and exact pool route/boundary;
// it never subtracts stock from an unexplained capture to manufacture income.
package main

import (
	"errors"
	"reflect"
)

// A single opening census stays bounded and hot across compaction. Its first
// retained bytes remain authenticated through the complete archive chain.
type economicConservationOpeningPrincipal struct {
	Projection nativePrincipalProjection `json:"original_parent_replay"`
	Outcome    nativeExecutionOutcome    `json:"original_execution"`
}

// One result per independently admitted tail pool; nil remains an absent API
// value. Boundary and pool identity do not turn that absence into observed zero.
type economicConservationPoolPrincipal struct {
	Route       economicConservationRoute      `json:"original_pool_route"`
	Observation historicalPrincipalObservation `json:"original_stake_observation"`
}

type economicConservationPrincipalSummary struct {
	Parent       economicEmissionBoundary            `json:"original_native_parent"`
	VaultFrom    economicEmissionBoundary            `json:"original_vault_boundary"`
	EvmHash      string                              `json:"parent_frontier_evm_hash,omitempty"`
	Runtime      rootReceiptProfile                  `json:"original_runtime"`
	Projection   string                              `json:"projection_hash"`
	Pools        []economicConservationPoolPrincipal `json:"original_pool_census"`
	Status       string                              `json:"status"`
	OpeningAlpha *string                             `json:"opening_stock_alpha"`
}

// Optional legacy policies have no principal authority. Admission binds the
// exact opening parent; incomplete route coverage remains unknown at the
// consumer instead of rejecting healthy sibling observation at startup.
func (self economicConservationPolicy) validatePrincipalAuthority() error {
	authority := self.Native.Observation.Execution.Principal
	if authority == nil {
		return nil
	}
	if err := authority.validate(); err != nil {
		return err
	}
	if authority.Parent != self.Native.Observation.From {
		return errors.New("economic opening principal moved from its original native parent")
	}

	return nil
}

// Native and vault routes share exact account bytes and subnet; textual labels
// and a later UID cannot substitute for the independently admitted coldkey.
func economicConservationPrincipalQuery(route economicConservationRoute, netuid uint16) (historicalPrincipalQuery, error) {
	var result historicalPrincipalQuery
	hotkey, hotErr := rootReceiptHex(route.Hotkey, 32)
	coldkey, coldErr := rootReceiptHex(route.Coldkey, 32)
	if err := errors.Join(hotErr, coldErr); err != nil {
		return result, err
	}
	copy(result.Hotkey[:], hotkey)
	copy(result.Coldkey[:], coldkey)
	result.Netuid = netuid
	return result, nil
}

// This is called only after the owned native reader authenticated replay and
// its original recipient projection. A supplied detached amount is no input.
func (self *economicConservationState) appendOpeningPrincipal(policy economicConservationPolicy, outcome nativeExecutionOutcome) error {
	authority := policy.Native.Observation.Execution.Principal
	if authority == nil || outcome.Boundary.Number != authority.Parent.Number+1 {
		if outcome.OpeningPrincipals != nil {
			return errors.New("economic opening principal appeared outside its admitted original boundary")
		}
		return nil
	}
	if err := outcome.OpeningPrincipals.validate(policy.Native.Observation, outcome); err != nil {
		return err
	}
	for _, observation := range outcome.OpeningPrincipals.Observations {
		matched := nativeTreasuryPrincipalQuery(policy.Native.Observation.Execution.Treasury, observation.Query, policy.Native.Observation.Netuid)
		for _, route := range policy.Routes {
			if route.Kind != "tail-pool" {
				continue
			}
			query, err := economicConservationPrincipalQuery(route, policy.Native.Observation.Netuid)
			if err != nil {
				return err
			}
			matched = matched || query == observation.Query
		}
		if !matched {
			return errors.New("economic opening principal substituted original pool/hotkey/coldkey census")
		}
	}
	value := &economicConservationOpeningPrincipal{Projection: *outcome.OpeningPrincipals, Outcome: outcome}
	value.Outcome.OpeningPrincipals, value.Outcome.RecipientEffects, value.Outcome.PrincipalEffects = nil, nil, nil
	value.Outcome.CertifiedWindow = nil
	value.Outcome.FeeCensus = nil
	if self.OpeningPrincipals != nil && !reflect.DeepEqual(self.OpeningPrincipals, value) {
		return errors.New("economic opening principal replaced its original replay")
	}
	if self.OpeningPrincipals == nil && self.facts()+uint64(len(value.Projection.Observations))+1 > policy.MaximumFacts {
		return errMonitorEconomicCapacity
	}
	self.OpeningPrincipals = value
	return nil
}

// Reopening cannot erase the first stock census or replace it with a later
// read. Zero and absent remain distinct even after the native history retires.
func (self economicConservationState) validateOpeningPrincipal(policy economicConservationPolicy) error {
	authority := policy.Native.Observation.Execution.Principal
	if authority == nil {
		if self.OpeningPrincipals != nil {
			return errors.New("economic legacy policy cannot enroll principal authority")
		}
		return nil
	}
	if self.OpeningPrincipals == nil {
		if self.Native.Cursor.Number > authority.Parent.Number {
			return errors.New("economic checkpoint dropped original opening principal evidence")
		}
		return nil
	}
	value := self.OpeningPrincipals
	if self.Native.Cursor.Number <= authority.Parent.Number || value.Outcome.OpeningPrincipals != nil || value.Outcome.RecipientEffects != nil || value.Outcome.PrincipalEffects != nil {
		return errors.New("economic opening principal is outside original native progress")
	}
	if err := value.Projection.validate(policy.Native.Observation, value.Outcome); err != nil {
		return err
	}
	if self.archiveView != nil && self.archiveView.openingPrincipalHash != "" && self.archiveView.openingPrincipalHash != rootObjectHash(value) {
		return errors.New("economic opening principal contradicts its first archived replay")
	}
	return nil
}

// This summary reports opening stock only after the original native parent
// commits the exact EVM opening hash. It grants neither independent vault
// finality nor authority over subsequent principal deposits/withdrawals.
func (self economicConservationState) principalSummary(policy economicConservationPolicy) (*economicConservationPrincipalSummary, error) {
	if self.OpeningPrincipals == nil {
		return nil, nil
	}
	if err := self.validateOpeningPrincipal(policy); err != nil {
		return nil, err
	}
	projection := self.OpeningPrincipals.Projection
	result := &economicConservationPrincipalSummary{Parent: projection.Parent, VaultFrom: policy.Vault.From, Runtime: projection.Runtime, Projection: projection.ContentHash, Status: "original-parent-stock-vault-boundary-unjoined"}
	header, err := projection.parentHeader()
	if err != nil {
		return nil, err
	}
	mapping, mapErr := finalizedFrontierPostLog(header)
	if mapErr != nil && (!errors.Is(mapErr, errFinalizedMappingUnavailable) || errors.Is(mapErr, errRpcIntegrity)) {
		return nil, mapErr
	}
	if mapErr == nil {
		result.EvmHash = mapping.BlockHash
	}
	total, complete := "0", true
	for _, route := range policy.Routes {
		if route.Kind != "tail-pool" {
			continue
		}
		query, err := economicConservationPrincipalQuery(route, policy.Native.Observation.Netuid)
		if err != nil {
			return nil, err
		}
		for _, observation := range projection.Observations {
			if observation.Query != query {
				continue
			}
			result.Pools = append(result.Pools, economicConservationPoolPrincipal{Route: route, Observation: observation})
			if observation.OpeningStakeAlpha == nil {
				complete = false
			} else if total, err = economicConservationSum(total, *observation.OpeningStakeAlpha); err != nil {
				return nil, err
			}
		}
	}
	if len(result.Pools) != len(policy.Vault.PoolIds) {
		result.Status = "original-parent-stock-pool-census-incomplete"
		return result, nil
	}
	if result.EvmHash != policy.Vault.From.Hash || self.OpeningVault == nil {
		return result, nil
	}
	result.Status = "original-parent-stock-absent-for-selected-pool"
	if complete {
		result.Status, result.OpeningAlpha = "original-parent-stock-observed-effects-unproved", &total
	}
	return result, nil
}

// The bounded archive index remembers one original evidence identity while
// complete original bytes remain in the shared checkpoint owners.
func (self *economicConservationArchiveView) retainOpeningPrincipal(original *economicConservationState) error {
	if original.OpeningPrincipals == nil {
		return nil
	}
	hash := rootObjectHash(original.OpeningPrincipals)
	if self.openingPrincipalHash != "" {
		if self.openingPrincipalHash != hash {
			return errors.New("economic archived opening principal changed original replay")
		}
		return nil
	}
	if err := self.charge(hash); err != nil {
		return err
	}
	self.openingPrincipalHash = hash
	return nil
}
