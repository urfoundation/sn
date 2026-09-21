// Lifetime allowances also cover append-only renewal and retired transaction
// history. Rebuilding unchanged future work must not allocate that money again.
package main

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
)

// These ceilings are fractions of the initial campaign allocation, rather than
// release-specific gas estimates. Raising a lifetime limit cannot resize them.
func retainedPercentageGasAction(id string) bool {
	switch id {
	case "campaign.voluntary-conviction.1", "campaign.dishonest-deposit.2",
		"production.schedule-policy", "retirement.evm-gas-reserve", "campaign.evm-gas-reserve",
		"governance.guardian-pause", "governance.upgrade-adversary", "governance.probe-custody",
		"governance.restore-coordinator", "governance.guardian-unpause",
		"precompile.seed", "precompile.move-forward", "precompile.move-back", "precompile.snapshot", "precompile.transfer-out":
		return true
	default:
		return false
	}
}

// Compare all executable fields after removing only a selected monetary
// allocation. The caller separately authenticates both original intent hashes.
func sameRetainedAllocationAction(left, right Action, funding bool) bool {
	left.Parameters, right.Parameters = cloneStrings(left.Parameters), cloneStrings(right.Parameters)
	left.IntentHash, right.IntentHash = "", ""
	left.AcceptedPriorIntentHashes, right.AcceptedPriorIntentHashes = nil, nil
	if funding {
		delete(left.Parameters, "usable_evm_rao")
		delete(right.Parameters, "usable_evm_rao")
		left.Spend.TAORao, right.Spend.TAORao = 0, 0
	} else {
		delete(left.Parameters, evmMaximumGasUnitsParameter)
		delete(right.Parameters, evmMaximumGasUnitsParameter)
		left.Spend.EVMGasWei, right.Spend.EVMGasWei = "0", "0"
	}
	// Spend's decimal zero has two equivalent in-memory encodings.
	if left.Spend.EVMGasWei.IsZero() && right.Spend.EVMGasWei.IsZero() {
		left.Spend.EVMGasWei, right.Spend.EVMGasWei = "0", "0"
	}
	return reflect.DeepEqual(left, right)
}

// A software continuation retains the exact original allocations only when
// every monetary action keeps its meaning. New fixed gas estimates, fees,
// custody, value inputs or phases use normal planning and its strict limits.
// Probe v2 changes only the authenticated CREATE generation, never its lineage.
func preserveRetainedCampaignAllocations(revised, prior *SetupPlan) error {
	if revised == nil || prior == nil {
		return errors.New("retained campaign allocation plans are absent")
	}
	if !validCanonicalHashHex(prior.PlanHash) || !revised.allowedPlanHashes()[prior.PlanHash] {
		return errors.New("retained campaign allocation source is outside approved lineage")
	}
	if prior.DeploymentID != revised.DeploymentID || prior.ChainID != revised.ChainID || prior.GenesisHash != revised.GenesisHash || prior.Netuid != revised.Netuid || prior.PolicyHash != revised.PolicyHash || prior.Owner != revised.Owner || !reflect.DeepEqual(prior.Roles, revised.Roles) || !contractDeploymentAddressesEqual(prior.Deployment, revised.Deployment) || !contractDeploymentRuntimeHashesCompatible(prior.Deployment, revised.Deployment) || prior.MaximumEVMFeePerGasWei != revised.MaximumEVMFeePerGasWei || prior.RegistrationBurnLimitRao != revised.RegistrationBurnLimitRao || prior.NativeTransactionFeeLimitRao != revised.NativeTransactionFeeLimitRao || prior.LiveFacts.ProbeTAORao != revised.LiveFacts.ProbeTAORao || prior.LiveFacts.NominatorMinimumRao != revised.LiveFacts.NominatorMinimumRao || prior.LiveFacts.ExistentialDepositRao != revised.LiveFacts.ExistentialDepositRao {
		return nil
	}
	comparison, err := revised.Limits.EVMGasWei.Cmp(prior.Limits.EVMGasWei)
	if err != nil {
		return err
	}
	if comparison < 0 || revised.Limits.TAORao < prior.Limits.TAORao {
		return nil
	}
	newProbe := !reflect.DeepEqual(prior.PrecompileProbeSuccessor, revised.PrecompileProbeSuccessor)
	if newProbe {
		successor := revised.PrecompileProbeSuccessor
		if successor == nil || successor.Retirement == nil || successor.Retirement.SourcePlanHash != prior.PlanHash || !reflect.DeepEqual(successor.Retirement.Predecessor, prior.PrecompileProbeSuccessor) {
			return nil
		}
		if err := validatePrecompileProbeSuccessorActions(revised); err != nil {
			return err
		}
	}
	monetary := func(action Action) bool {
		return !isFleetRenewalAction(action) && !isFleetRenewalExtensionAction(action) && (action.Kind == "evm-transaction" || !action.Spend.EVMGasWei.IsZero() || strings.HasPrefix(action.ID, "evm.fund-"))
	}
	originals := map[string]Action{}
	for _, action := range prior.Actions {
		if !monetary(action) {
			continue
		}
		if _, exists := originals[action.ID]; exists {
			return fmt.Errorf("retained allocation duplicates source action %s", action.ID)
		}
		originals[action.ID] = action
	}
	updates := map[int]Action{}
	seen := map[string]bool{}
	for index, action := range revised.Actions {
		if !monetary(action) {
			continue
		}
		original, exists := originals[action.ID]
		if !exists {
			return nil
		}
		if seen[action.ID] {
			return fmt.Errorf("retained allocation duplicates revised action %s", action.ID)
		}
		seen[action.ID] = true
		for _, checked := range []Action{original, action} {
			hash, err := actionIntentHash(checked)
			if err != nil || hash != checked.IntentHash {
				return errors.Join(fmt.Errorf("retained allocation action %s changed its intent", checked.ID), err)
			}
		}
		comparable := action
		if newProbe && strings.HasPrefix(action.ID, "precompile.") {
			comparable.Parameters = cloneStrings(action.Parameters)
			for _, key := range []string{precompileProbeAddressParameter, precompileProbeRuntimeParameter} {
				comparable.Parameters[key] = original.Parameters[key]
			}
			if action.ID == "precompile.probe-deploy" {
				for _, key := range []string{"expected_created_address", "expected_data_keccak256", "expected_nonce"} {
					comparable.Parameters[key] = original.Parameters[key]
				}
			}
		}
		funding := strings.HasPrefix(action.ID, "evm.fund-")
		percentage := retainedPercentageGasAction(action.ID)
		if !sameRetainedAllocationAction(original, comparable, funding) {
			return nil
		}
		if !percentage && !funding && (original.Spend != action.Spend || original.Parameters[evmMaximumGasUnitsParameter] != action.Parameters[evmMaximumGasUnitsParameter]) {
			return nil
		}
		if funding {
			for _, checked := range []Action{original, action} {
				if _, err := evmFundingTerms(checked, prior.LiveFacts.ExistentialDepositRao); err != nil {
					return err
				}
			}
		}
		if !percentage && !funding {
			continue
		}
		if newProbe && strings.HasPrefix(action.ID, "precompile.") {
			action.Parameters = cloneStrings(action.Parameters)
			if units, ok := original.Parameters[evmMaximumGasUnitsParameter]; ok {
				action.Parameters[evmMaximumGasUnitsParameter] = units
			} else {
				delete(action.Parameters, evmMaximumGasUnitsParameter)
			}
			action.Spend.EVMGasWei = original.Spend.EVMGasWei
			action.IntentHash, err = actionIntentHash(action)
			if err != nil {
				return err
			}
			updates[index] = action
		} else {
			updates[index] = original
		}
	}
	if len(seen) != len(originals) {
		return nil
	}
	for index, action := range updates {
		revised.Actions[index] = action
	}
	revised.MaximumSpend, err = maximumActionSpend(revised.Actions)
	return err
}
