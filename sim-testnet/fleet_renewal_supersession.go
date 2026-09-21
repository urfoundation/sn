// A complete successor can retire unsigned work from an older renewal while
// retaining its approval and every actual transaction as immutable history.
package main

// One plan/journal pass keeps startup and evidence capture bounded. Signed
// actions are never retired: their actual outcomes must still be reconciled.
func fleetRenewalSupersededUnsignedActions(plan *SetupPlan, entries []JournalEntry) map[string]bool {
	result := map[string]bool{}
	if plan == nil || len(plan.FleetRenewals) < 2 {
		return result
	}
	actionKVs := make(map[string]Action, len(plan.Actions))
	for _, action := range plan.Actions {
		actionKVs[action.ID] = action
	}
	verifiedActionKVs := exactVerifiedPlanActionIndex(plan, entries)
	signedActionKVs := map[string]bool{}
	finalizedActionKVs := map[string]bool{}
	allowedPlanHashKVs := plan.allowedPlanHashes()
	for _, entry := range entries {
		action, found := actionKVs[entry.ActionID]
		if !found || !allowedPlanHashKVs[entry.PlanHash] || !actionAcceptsIntent(action, entry.IntentHash) {
			continue
		}
		signedActionKVs[entry.ActionID] = signedActionKVs[entry.ActionID] || entry.TransactionHash != ""
		finalizedActionKVs[entry.ActionID] = finalizedActionKVs[entry.ActionID] || entry.Stage == StageFinalized && entry.TransactionHash != ""
	}
	latest := plan.FleetRenewals[len(plan.FleetRenewals)-1]
	completeFleetKVs := map[int]bool{}
	for _, fleet := range latest.Fleets {
		complete := len(fleet.Members) != 0
		for _, operation := range []string{"commitment", "mirror", "bind"} {
			count := 1
			if operation == "bind" {
				count = len(fleet.Members)
			}
			for index := range count {
				member := 0
				if operation == "bind" {
					member = index + 1
				}
				id := fleetRenewalActionID(latest.Round, fleet.Fleet, operation, member)
				complete = complete && verifiedActionKVs[id] && finalizedActionKVs[id]
			}
		}
		completeFleetKVs[fleet.Fleet] = complete
	}
	for _, action := range plan.Actions {
		round, fleet, _, _, ok := finalFleetRenewalActionCoordinates(action.ID)
		if !ok || round >= latest.Round || round > uint64(len(plan.FleetRenewals)) || signedActionKVs[action.ID] || verifiedActionKVs[action.ID] || !completeFleetKVs[fleet] {
			continue
		}
		if plan.FleetRenewals[round-1].ValidFromEpoch < latest.ValidFromEpoch {
			result[action.ID] = true
		}
	}
	return result
}
