// Synthetic lifetime extensions retain existing work allocations and expose
// genuine semantic or monetary changes without any chain access.
package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Renders two real action graphs, then records the extended lifetime authority
// on the predecessor just as an append-only renewal does without rerendering it.
func retainedAllocationTestPlans(t *testing.T, evm, native bool) (*SetupPlan, *SetupPlan) {
	t.Helper()
	cfg := testResolvedConfig(t)
	cfg.MaximumTAORao = 1_000_000_000_000
	roles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	prior, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	if evm {
		cfg.MaximumEVMGasWei, err = addDecimalUint(cfg.MaximumEVMGasWei, "17000000000000000000")
		if err != nil {
			t.Fatal(err)
		}
	}
	if native {
		cfg.MaximumTAORao += 50_000_000_000
	}
	revised, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	prior.Limits = revised.Limits
	prior.PlanHash, err = prior.hash()
	if err != nil {
		t.Fatal(err)
	}
	revised.PriorPlanHashes = []string{prior.PlanHash}
	return prior, revised
}

// Owns every nested map/slice so negative controls cannot corrupt their source.
func cloneRetainedAllocationTestPlan(t *testing.T, plan *SetupPlan) *SetupPlan {
	t.Helper()
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	var copy SetupPlan
	if err := json.Unmarshal(raw, &copy); err != nil {
		t.Fatal(err)
	}
	return &copy
}

// Every sibling percentage family and each base signer funding stays exact;
// all lifetime authority remains available for explicitly appended work.
func assertRetainedAllocationTestCarry(t *testing.T, prior, revised *SetupPlan) {
	t.Helper()
	source := cloneRetainedAllocationTestPlan(t, prior)
	limits := revised.Limits
	if err := preserveRetainedCampaignAllocations(revised, prior); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, original := range prior.Actions {
		if retainedPercentageGasAction(original.ID) || strings.HasPrefix(original.ID, "evm.fund-") {
			count++
			if !finalJSONEqual(original, actionByID(t, revised, original.ID)) {
				t.Fatalf("lifetime extension resized %s", original.ID)
			}
		}
	}
	if count != 26 || revised.MaximumSpend != prior.MaximumSpend || revised.Limits != limits || !finalJSONEqual(source, prior) {
		t.Fatalf("carry changed authority, historical source or counted spend: families=%d before=%+v after=%+v", count, prior.MaximumSpend, revised.MaximumSpend)
	}
	if err := validatePlanBudget(revised); err != nil {
		t.Fatal(err)
	}
}

// Reproduces renewal EVM authority inflating all unexecuted scenario ceilings.
func TestRetainedCampaignAllocationEvmExtension(t *testing.T) {
	prior, revised := retainedAllocationTestPlans(t, true, false)
	if actionByID(t, prior, "precompile.seed").Spend == actionByID(t, revised, "precompile.seed").Spend || actionByID(t, prior, "evm.fund-deployer").Spend == actionByID(t, revised, "evm.fund-deployer").Spend {
		t.Fatal("fixture did not reproduce gas and signer-funding inflation")
	}
	assertRetainedAllocationTestCarry(t, prior, revised)
}

// A native-only lifetime extension cannot change original funding or gas.
func TestRetainedCampaignAllocationNativeExtension(t *testing.T) {
	prior, revised := retainedAllocationTestPlans(t, false, true)
	assertRetainedAllocationTestCarry(t, prior, revised)
}

// Joint extensions and repeated software restarts preserve the same envelopes.
func TestRetainedCampaignAllocationBothExtensionsAndRepeatedCarry(t *testing.T) {
	prior, revised := retainedAllocationTestPlans(t, true, true)
	assertRetainedAllocationTestCarry(t, prior, revised)
	before := cloneRetainedAllocationTestPlan(t, revised)
	if err := preserveRetainedCampaignAllocations(revised, prior); err != nil || !finalJSONEqual(before, revised) {
		t.Fatalf("second carry changed the candidate: %v", err)
	}
}

// Explicit fee, fixed gas, funding input, custody and action changes must use
// normal strict planning. A failed classification cannot partially copy budgets.
func TestRetainedCampaignAllocationRejectsChangedScope(t *testing.T) {
	prior, base := retainedAllocationTestPlans(t, true, true)
	for _, mutation := range []string{"fee", "fixed gas", "target", "value", "dependency", "funding input", "new action", "missing action", "policy", "probe input", "burn"} {
		candidate := cloneRetainedAllocationTestPlan(t, base)
		id := "precompile.seed"
		if mutation == "fixed gas" {
			id = "evm.reserve-sink"
		} else if mutation == "funding input" {
			id = "evm.fund-deployer"
		}
		for index := range candidate.Actions {
			action := &candidate.Actions[index]
			if action.ID != id {
				continue
			}
			switch mutation {
			case "fee":
				action.Parameters[evmMaximumFeePerGasParameter] = "2"
			case "fixed gas":
				action.Parameters[evmMaximumGasUnitsParameter] = "999999"
			case "target":
				action.Target = "changed-synthetic-target"
			case "value":
				action.Parameters["maximum_tao_rao"] = "123"
			case "dependency":
				action.DependsOn = append(action.DependsOn, "synthetic-new-dependency")
			case "funding input":
				action.Parameters["existential_deposit_rao"] = "123"
			}
			*action = testFleetSupersessionAction(t, *action)
		}
		switch mutation {
		case "new action":
			added := actionByID(t, candidate, id)
			added.ID = "synthetic-extra-transaction"
			candidate.Actions = append(candidate.Actions, testFleetSupersessionAction(t, added))
		case "missing action":
			for index, action := range candidate.Actions {
				if action.ID == id {
					candidate.Actions = append(candidate.Actions[:index], candidate.Actions[index+1:]...)
					break
				}
			}
		case "policy":
			candidate.PolicyHash = "synthetic-changed-policy"
		case "probe input":
			candidate.LiveFacts.ProbeTAORao++
		case "burn":
			candidate.RegistrationBurnLimitRao++
		}
		before := cloneRetainedAllocationTestPlan(t, candidate)
		if err := preserveRetainedCampaignAllocations(candidate, prior); err != nil {
			t.Fatalf("%s should retain ordinary planning: %v", mutation, err)
		}
		if !finalJSONEqual(before, candidate) {
			t.Fatalf("%s inherited incompatible or partial old allocations", mutation)
		}
	}
}

// Tampered action hashes are rejected, and intact carry never overrides a real
// native limit or a retained signed-liability floor.
func TestRetainedCampaignAllocationKeepsIdentityAndBudgetGuards(t *testing.T) {
	prior, revised := retainedAllocationTestPlans(t, true, true)
	tampered := cloneRetainedAllocationTestPlan(t, revised)
	for index := range tampered.Actions {
		if tampered.Actions[index].ID == "precompile.seed" {
			tampered.Actions[index].IntentHash = "tampered"
		}
	}
	if err := preserveRetainedCampaignAllocations(tampered, prior); err == nil {
		t.Fatal("altered action hash inherited a retained allowance")
	}
	foreign := cloneRetainedAllocationTestPlan(t, revised)
	foreign.PriorPlanHashes = nil
	if err := preserveRetainedCampaignAllocations(foreign, prior); err == nil {
		t.Fatal("unapproved source inherited a retained allowance")
	}
	assertRetainedAllocationTestCarry(t, prior, revised)
	revised.Limits.TAORao = revised.MaximumSpend.TAORao - 1
	if err := validatePlanBudget(revised); err == nil {
		t.Fatal("retained native funding bypassed its cumulative lifetime limit")
	}
	revised.FleetRenewals = []FleetRenewal{{CampaignLiabilityWei: "999999999999999999999999"}}
	if err := validateFleetRenewalReservedLiability(revised); err == nil {
		t.Fatal("retained reserve bypassed signed liabilities")
	}
}

// A new immutable probe keeps the original value ceilings but receives no old
// action intent or alias. Its completed predecessor CREATE remains retired once.
func TestRetainedCampaignAllocationKeepsAuthenticatedProbeGeneration(t *testing.T) {
	fixture := newPrecompileProbeRetirementFixture(t)
	prior, revised := fixture.plan, fixture.successor(t)
	for index, action := range revised.Actions {
		if retainedPercentageGasAction(action.ID) && action.Kind == "evm-transaction" {
			action.Spend.EVMGasWei = "123"
			action.Parameters[evmMaximumGasUnitsParameter] = "123"
			revised.Actions[index] = testFleetSupersessionAction(t, action)
		}
	}
	if err := preserveRetainedCampaignAllocations(revised, prior); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"precompile.seed", "precompile.move-forward", "precompile.move-back", "precompile.snapshot", "precompile.transfer-out"} {
		old, current := actionByID(t, prior, id), actionByID(t, revised, id)
		if !reflect.DeepEqual(old.Spend, current.Spend) || current.Parameters[precompileProbeAddressParameter] != revised.PrecompileProbeSuccessor.Probe || actionAcceptsIntent(current, old.IntentHash) {
			t.Fatalf("%s rebound the old intent or inflated the new value ceiling", id)
		}
	}
	retired, err := addRetiredVerifiedEVMGas(prior, revised, fixture.entries, prior.SupersededSpend)
	want, sumErr := addDecimalUint(prior.SupersededSpend.EVMGasWei, actionByID(t, prior, "precompile.probe-deploy").Spend.EVMGasWei)
	if err != nil || sumErr != nil || retired.EVMGasWei != want {
		t.Fatalf("probe replacement lost its exact retired CREATE ceiling: %v", err)
	}
	if err := validatePrecompileProbeSuccessorActions(revised); err != nil {
		t.Fatal(err)
	}
}
