package main

// fleet_renewal_runtime_identity.go keeps runtime fingerprints consistent with
// the local actions that will consume them after a renewal, without changing
// any approved transaction or mutating the retained predecessor plan.

import (
	"errors"
	"maps"
)

// rebindFleetRenewalRuntimePlan owns a shallow plan copy and detached local
// action parameters. Changed runtime inputs require a new local intent; old
// local verification must not satisfy the render/launch for the new inputs.
func rebindFleetRenewalRuntimePlan(plan *SetupPlan, configHash, resolvedHash string) (*SetupPlan, error) {
	if plan == nil {
		return nil, errors.New("renewal runtime plan is unavailable")
	}
	rebound := *plan
	rebound.ConfigHash, rebound.ResolvedInputsHash = configHash, resolvedHash
	rebound.Actions = append([]Action(nil), plan.Actions...)
	for index := range rebound.Actions {
		action := &rebound.Actions[index]
		if action.ID != "config.render" && action.ID != "topology.launch" {
			continue
		}
		if action.Kind != "local" {
			return nil, errors.New("renewal runtime action is not local")
		}
		if action.Parameters["config_hash"] == configHash && action.Parameters["policy_hash"] == rebound.PolicyHash && (action.ID != "config.render" || action.Parameters["resolved_inputs_hash"] == resolvedHash) {
			continue
		}
		action.Parameters = maps.Clone(action.Parameters)
		if action.Parameters == nil {
			action.Parameters = map[string]string{}
		}
		action.Parameters["config_hash"], action.Parameters["policy_hash"] = configHash, rebound.PolicyHash
		if action.ID == "config.render" {
			action.Parameters["resolved_inputs_hash"] = resolvedHash
		}
		action.AcceptedPriorIntentHashes = nil
		var err error
		action.IntentHash, err = actionIntentHash(*action)
		if err != nil {
			return nil, err
		}
	}
	var err error
	rebound.PlanHash, err = rebound.hash()
	if err != nil {
		return nil, err
	}
	return &rebound, nil
}
