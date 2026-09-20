package main

import (
	"encoding/json"
	"errors"
	"fmt"
)

// A normal revision rebuilds future setup actions. Approved renewals are
// append-only history: retain their exact actions and lifecycle successor
// consent, charging them once against the rebuilt campaign reserve.
func carryFleetRenewalRevision(revised, prior *SetupPlan) error {
	if revised == nil || prior == nil {
		return errors.New("fleet renewal revision source is absent")
	}
	if len(prior.FleetRenewals) == 0 {
		return nil
	}
	if len(revised.FleetRenewals) != 0 || revised.FleetLifecycleRenewal != nil {
		return errors.New("fleet renewal revision was already applied")
	}
	// A completed live renewal persists its signed renewal record and receipts;
	// successor reconstruction restores any compacted deterministic actions before
	// validating the append-only lineage. Existing actions must still match
	// exactly, so compaction cannot authorize a modified action.
	priorForValidation, err := restoreFleetRenewalActions(prior)
	if err != nil {
		return err
	}
	prior = priorForValidation
	if err := validateFleetRenewalPlan(prior); err != nil {
		return err
	}
	if err := validateFleetLifecycleRenewalPlan(prior); err != nil {
		return err
	}
	before, err := contractDeploymentIdentityHash(prior.Deployment)
	if err != nil {
		return err
	}
	after, err := contractDeploymentIdentityHash(revised.Deployment)
	if err != nil || before != after || prior.NativeTransactionFeeLimitRao != revised.NativeTransactionFeeLimitRao || prior.ChainID != revised.ChainID || prior.Netuid != revised.Netuid || prior.Owner != revised.Owner || prior.DeploymentID != revised.DeploymentID {
		return stateMismatchError(err, "fleet renewal revision cannot change approved custody or native fee envelope")
	}
	raw, err := json.Marshal(prior)
	if err != nil {
		return err
	}
	var retained SetupPlan
	if err := json.Unmarshal(raw, &retained); err != nil {
		return err
	}
	var renewalActions []Action
	future := map[string]Action{}
	for _, action := range retained.Actions {
		if isFleetRenewalAction(action) {
			renewalActions = append(renewalActions, action)
		}
		if fleetLifecycleRenewalFutureAction(action.ID) {
			future[action.ID] = action
		}
	}
	renewalSpend, err := maximumActionSpend(renewalActions)
	if err != nil {
		return err
	}
	foundReserve := false
	for index := range revised.Actions {
		action := &revised.Actions[index]
		if isFleetRenewalAction(*action) {
			return errors.New("rebuilt plan already contains renewal actions")
		}
		if original, found := future[action.ID]; found {
			*action = original
			delete(future, action.ID)
		}
		if action.ID != "campaign.evm-gas-reserve" {
			continue
		}
		if foundReserve || action.Kind != "budget-reserve" {
			return errors.New("fleet renewal revision has invalid campaign reserve ownership")
		}
		foundReserve = true
		action.Spend.EVMGasWei, err = subtractDecimalUint(action.Spend.EVMGasWei, renewalSpend.EVMGasWei)
		if err != nil {
			return fmt.Errorf("retained renewal gas exceeds rebuilt campaign reserve: %w", err)
		}
		action.IntentHash, err = actionIntentHash(*action)
		if err != nil {
			return err
		}
	}
	if !foundReserve || len(future) != 0 {
		return errors.New("fleet renewal revision omitted approved reserve or lifecycle actions")
	}
	revised.FleetRenewals = retained.FleetRenewals
	revised.FleetLifecycleRenewal = cloneFleetLifecycleRenewal(retained.FleetLifecycleRenewal)
	revised.Actions = append(revised.Actions, renewalActions...)
	revised.MaximumSpend, err = maximumActionSpend(revised.Actions)
	if err != nil {
		return err
	}
	if err := validateFleetRenewalPlan(revised); err != nil {
		return err
	}
	if err := validateFleetLifecycleRenewalPlan(revised); err != nil {
		return err
	}
	return validateFleetRenewalReservedLiability(revised)
}

func validateFleetRenewalReservedLiability(plan *SetupPlan) error {
	if plan == nil || len(plan.FleetRenewals) == 0 {
		return nil
	}
	liability := plan.FleetRenewals[len(plan.FleetRenewals)-1].CampaignLiabilityWei
	for _, action := range plan.Actions {
		if action.ID != "campaign.evm-gas-reserve" {
			continue
		}
		comparison, err := action.Spend.EVMGasWei.Cmp(liability)
		if err != nil || comparison < 0 {
			return stateMismatchError(err, "revised campaign reserve %s does not cover retained signed liabilities %s", action.Spend.EVMGasWei, liability)
		}
		return nil
	}
	return errors.New("renewed plan lost its signed campaign liability reserve")
}

func restoreFleetRenewalActions(plan *SetupPlan) (*SetupPlan, error) {
	if plan == nil || len(plan.FleetRenewals) == 0 {
		return plan, nil
	}
	raw, err := json.Marshal(plan)
	if err != nil {
		return nil, err
	}
	var restored SetupPlan
	if err := json.Unmarshal(raw, &restored); err != nil {
		return nil, err
	}
	expected := map[string]Action{}
	for _, renewal := range restored.FleetRenewals {
		actions, err := fleetRenewalPlanActions(&restored, renewal)
		if err != nil {
			return nil, err
		}
		for _, action := range actions {
			expected[action.ID] = action
		}
	}
	seen := map[string]bool{}
	for _, action := range restored.Actions {
		if !isFleetRenewalAction(action) && !isFleetRenewalExtensionAction(action) {
			continue
		}
		want, ok := expected[action.ID]
		gotHash, _ := canonicalHashHex(action)
		wantHash, _ := canonicalHashHex(want)
		if !ok || gotHash != wantHash {
			return nil, fmt.Errorf("renewal action %s differs from its approved generation", action.ID)
		}
		seen[action.ID] = true
	}
	for _, renewal := range restored.FleetRenewals {
		actions, err := fleetRenewalPlanActions(&restored, renewal)
		if err != nil {
			return nil, err
		}
		for _, action := range actions {
			if !seen[action.ID] {
				restored.Actions = append(restored.Actions, action)
			}
		}
	}
	return &restored, nil
}
