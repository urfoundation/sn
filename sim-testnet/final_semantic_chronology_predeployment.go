package main

// Historical plans before the deployment envelope may approve future proxy
// work without having a proxy identity. Their preparatory receipts remain in
// the authenticated journal but cannot supply a coordinator transition.

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/common"
)

// Derives only deployed proxies from an already authenticated plan lineage.
// Legacy placeholders may retain planned actions and preparatory finalizations;
// a finalized transition always requires a bound, nonzero proxy, at any height.
func finalHistoricalCoordinatorProxyCensus(current *SetupPlan, plans map[string]*SetupPlan, entries []JournalEntry) (map[string]bool, error) {
	if current == nil || len(plans) == 0 {
		return nil, errors.New("historical coordinator proxy census is incomplete")
	}
	allowed := current.allowedPlanHashes()
	if plans[current.PlanHash] == nil {
		return nil, errors.New("historical coordinator proxy census lacks the active plan")
	}
	for hash := range allowed {
		if plans[hash] == nil {
			return nil, fmt.Errorf("historical coordinator proxy census lacks approved plan %s", hash)
		}
	}
	proxies := make(map[string]bool, len(plans))
	predeploymentPlans := make(map[string]bool)
	for hash, plan := range plans {
		if plan == nil || !allowed[hash] || !strings.EqualFold(hash, plan.PlanHash) || plan.DeploymentID != current.DeploymentID || plan.ChainID != current.ChainID || plan.Netuid != current.Netuid {
			return nil, errors.New("historical coordinator proxy census has an unapproved plan")
		}
		if plan.Deployment.CoordinatorProxy != (common.Address{}) {
			proxies[strings.ToLower(plan.Deployment.CoordinatorProxy.Hex())] = true
			continue
		}
		if hash == current.PlanHash || !supportedSetupPlanSchema(plan.Schema) || planUsesContractDeploymentEnvelope(plan.Schema) ||
			!finalJSONEqual(plan.Deployment, ContractDeployment{}) || plan.CoordinatorUpgrade != (CoordinatorUpgrade{}) ||
			!plan.CoordinatorUpgradeBaseline.isZero() || plan.CoordinatorRepairCarry != nil || len(plan.SupersededDeployments) != 0 {
			return nil, fmt.Errorf("historical coordinator zero-proxy plan %s is not a predeployment ancestor", hash)
		}
		predeploymentPlans[hash] = true
	}
	for _, entry := range entries {
		if entry.Stage != StageFinalized || !predeploymentPlans[strings.ToLower(entry.PlanHash)] {
			continue
		}
		if finalHistoricalCoordinatorTransitionAction(entry.ActionID) {
			return nil, fmt.Errorf("historical coordinator zero-proxy plan %s has finalized transition %s", entry.PlanHash, entry.ActionID)
		}
	}
	return proxies, nil
}

// Keeps predeployment refusal and transition reconstruction on the same exact
// action identities. Implementation deployment alone does not create a proxy.
func finalHistoricalCoordinatorTransitionAction(actionId string) bool {
	switch actionId {
	case "evm.coordinator-proxy", "evm.coordinator-upgrade-activate", "repair.coordinator-rounding.activate":
		return true
	default:
		return false
	}
}
