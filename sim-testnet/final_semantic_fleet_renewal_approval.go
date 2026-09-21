package main

// final_semantic_fleet_renewal_approval.go authenticates historical renewal
// approvals without requiring their operational fingerprints to match the
// predecessor release that supplied the unchanged economic plan.

import "slices"

// finalFleetRenewalApprovalMatches reconstructs the exact append with only the
// approved config and resolved-input fingerprints rebound. Earlier producers
// left local runtime action stamps unchanged; their already-authenticated
// historical bytes remain valid. New producers also rebind those local intents.
// The candidate must
// also be authenticated against its archived bytes by the caller; this check
// locates that approval without admitting changed policy, route, or actions.
func finalFleetRenewalApprovalMatches(expected, candidate *SetupPlan) (bool, error) {
	if expected == nil || candidate == nil || candidate.PolicyHash != expected.PolicyHash || candidate.OwnedRPCAuthority != expected.OwnedRPCAuthority || !slices.Equal(candidate.PriorPlanHashes, expected.PriorPlanHashes) {
		return false, nil
	}
	rebound := *expected
	rebound.ConfigHash, rebound.ResolvedInputsHash = candidate.ConfigHash, candidate.ResolvedInputsHash
	hash, err := rebound.hash()
	if err != nil || hash == candidate.PlanHash {
		return err == nil, err
	}
	current, err := rebindFleetRenewalRuntimePlan(expected, candidate.ConfigHash, candidate.ResolvedInputsHash)
	return err == nil && current.PlanHash == candidate.PlanHash, err
}
