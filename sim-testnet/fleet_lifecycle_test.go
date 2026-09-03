package main

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func lifecyclePruneFixture(t *testing.T) (FleetLifecyclePruneSnapshot, [32]byte, [32]byte) {
	t.Helper()
	var ownerHotkey, ownerColdkey, targetHotkey, targetColdkey, adjacentHotkey, adjacentColdkey [32]byte
	ownerHotkey[0], ownerColdkey[0] = 1, 2
	targetHotkey[0], targetColdkey[0] = 3, 4
	adjacentHotkey[0], adjacentColdkey[0] = 5, 6
	inputs := []FleetLifecyclePruneInput{
		{UID: 0, Hotkey: fleetLifecycleHex(ownerHotkey), Coldkey: fleetLifecycleHex(ownerColdkey), RegistrationBlock: 1, Immune: true, Immortal: true},
		{UID: 1, Hotkey: fleetLifecycleHex(targetHotkey), Coldkey: fleetLifecycleHex(targetColdkey), RegistrationBlock: 10, Immune: true},
		{UID: 2, Hotkey: fleetLifecycleHex(adjacentHotkey), Coldkey: fleetLifecycleHex(adjacentColdkey), RegistrationBlock: 10, Immune: true},
	}
	return FleetLifecyclePruneSnapshot{
		Head: ChainHead{Number: 20, Hash: "0x" + strings.Repeat("11", 32)}, UIDCount: 3, MaximumUIDs: 3,
		ImmunityPeriodBlocks: 100, MinimumNonImmuneUIDs: 1, RuntimePruneUID: 1, Inputs: inputs,
	}, targetHotkey, targetColdkey
}

func TestFleetLifecycleSelectsTheOldestSurvivingProviderChurnRoles(t *testing.T) {
	topology := TopologyConfig{Operators: 2, HeadFleets: 200, HeadSlots: 200, ChallengerFleets: 2, ChurnFloorUIDs: 47}
	if err := validateFleetLifecycleTopology(topology); err != nil {
		t.Fatal(err)
	}
	if got := fleetProviderHotkeyLabel(fleetLifecycleTargetFleet); got != churnHotkeyLabel(6) {
		t.Fatalf("target provider hotkey = %q, want churn slot 6", got)
	}
	if got := fleetProviderColdkeyLabel(fleetLifecycleCompanionFleet); got != churnColdkeyLabel(7) {
		t.Fatalf("companion provider coldkey = %q, want churn slot 7", got)
	}
	if got, err := churnIndexForContractRegistration(topology, 1, 1); err != nil || got != 1 {
		t.Fatalf("first replacement churn = %d, %v; want 1", got, err)
	}
	if got, err := churnIndexForChallenger(topology, 0, 1); err != nil || got != 1 {
		t.Fatalf("first challenger churn = %d, %v; want 1", got, err)
	}
	if got, err := churnIndexForChallenger(topology, 0, 2); err != nil || got != 2 {
		t.Fatalf("second challenger churn = %d, %v; want 2", got, err)
	}
}

func TestFleetLifecycleDoesNotRedirectOrdinaryFleetSignerFunding(t *testing.T) {
	cfg := testResolvedConfig(t)
	for _, fleet := range []int{fleetLifecycleTargetFleet, fleetLifecycleCompanionFleet} {
		action := Action{ID: fmt.Sprintf("fleet.fund-hotkey.%d", fleet), Kind: "substrate-extrinsic", Target: fmt.Sprintf("head-fleet-hotkey:%d", fleet)}
		role, err := fleetHotkeyFundingRole(cfg, action)
		if err != nil {
			t.Fatal(err)
		}
		if role != fleetHotkeyLabel(fleet) || role == fleetProviderHotkeyLabel(fleet) {
			t.Fatalf("ordinary fleet %d funding role=%q, registered=%q lifecycle=%q", fleet, role, fleetHotkeyLabel(fleet), fleetProviderHotkeyLabel(fleet))
		}
	}
}

func TestFleetLifecycleOrdinaryFleetSignerFundingRejectsMalformedAction(t *testing.T) {
	cfg := testResolvedConfig(t)
	for _, action := range []Action{
		{ID: "lifecycle.prepare.target.fund-hotkey", Kind: "substrate-extrinsic", Target: "head-fleet-hotkey:5"},
		{ID: "fleet.fund-hotkey.0", Kind: "substrate-extrinsic", Target: "head-fleet-hotkey:0"},
		{ID: "fleet.fund-hotkey.invalid", Kind: "substrate-extrinsic", Target: "head-fleet-hotkey:5"},
		{ID: "fleet.fund-hotkey.5", Kind: "substrate-extrinsic", Target: "challenger-fleet-hotkey:5"},
	} {
		if _, err := fleetHotkeyFundingRole(cfg, action); err == nil {
			t.Fatalf("malformed ordinary funding action %+v was accepted", action)
		}
	}
}

func TestFleetLifecycleFundingRolesDistinguishRegistrationAndCommitmentSigners(t *testing.T) {
	cases := []struct {
		actionID string
		role     string
	}{
		{"lifecycle.prepare.target.fund-hotkey", churnHotkeyLabel(fleetLifecycleTargetChurn)},
		{"lifecycle.prepare.companion.fund-hotkey", churnHotkeyLabel(fleetLifecycleCompanionChurn)},
		{"lifecycle.fallback.fund", churnColdkeyLabel(fleetLifecycleFallbackChurn)},
		{"lifecycle.fallback.fund-hotkey", churnHotkeyLabel(fleetLifecycleFallbackChurn)},
		{"lifecycle.provider.fund", fleetProviderColdkeyLabel(fleetLifecycleTargetFleet)},
		{"lifecycle.provider.fund-hotkey", fleetProviderHotkeyLabel(fleetLifecycleTargetFleet)},
		{"lifecycle.terminal.fund", churnColdkeyLabel(fleetLifecycleCompanionChurn)},
		{"lifecycle.terminal.fund-hotkey", churnHotkeyLabel(fleetLifecycleCompanionChurn)},
	}
	for _, test := range cases {
		role, ok := fleetLifecycleFundingRole(test.actionID)
		if !ok || role != test.role {
			t.Fatalf("funding action %s role=%q found=%t, want %q", test.actionID, role, ok, test.role)
		}
	}
	if _, ok := fleetLifecycleFundingRole("fleet.fund-hotkey.5"); ok {
		t.Fatal("ordinary fleet funding was accepted as lifecycle funding")
	}
}

func TestFleetLifecyclePlanBindsEveryFundingActionToItsExactSignerRole(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	actions := make(map[string]Action, len(plan.Actions))
	for _, action := range plan.Actions {
		actions[action.ID] = action
	}
	for actionID, action := range actions {
		role, lifecycleFunding := fleetLifecycleFundingRole(actionID)
		if !lifecycleFunding {
			continue
		}
		if action.Kind != "substrate-extrinsic" || action.Target != role || action.Spend.TAORao == 0 {
			t.Fatalf("lifecycle funding action %s is not bound to role %q: %+v", actionID, role, action)
		}
	}
	for _, actionID := range []string{
		"lifecycle.prepare.target.fund-hotkey", "lifecycle.prepare.companion.fund-hotkey",
		"lifecycle.fallback.fund", "lifecycle.fallback.fund-hotkey",
		"lifecycle.provider.fund", "lifecycle.provider.fund-hotkey",
		"lifecycle.terminal.fund", "lifecycle.terminal.fund-hotkey",
	} {
		if _, ok := actions[actionID]; !ok {
			t.Fatalf("plan is missing lifecycle funding action %s", actionID)
		}
	}
	for _, registration := range []struct {
		actionID string
		hotkey   string
	}{
		{"lifecycle.fallback.register", churnHotkeyLabel(fleetLifecycleFallbackChurn)},
		{"lifecycle.provider.register", fleetProviderHotkeyLabel(fleetLifecycleTargetFleet)},
		{"lifecycle.terminal.register", churnHotkeyLabel(fleetLifecycleCompanionChurn)},
	} {
		action, ok := actions[registration.actionID]
		if !ok || action.Target != registration.hotkey || action.Spend.Registrations != 1 {
			t.Fatalf("registration %s is not bound to hotkey %q: %+v", registration.actionID, registration.hotkey, action)
		}
	}
}

func TestFleetLifecycleRejectsTopologyWithoutReservedSlots(t *testing.T) {
	topology := TopologyConfig{Operators: 2, HeadFleets: 5, HeadSlots: 5, ChallengerFleets: 2, ChurnFloorUIDs: 2}
	if err := validateFleetLifecycleTopology(topology); err == nil {
		t.Fatal("undersized lifecycle topology unexpectedly accepted")
	}
}

func TestFleetLifecyclePruneProofUsesRegistrationBlockThenUIDTieBreak(t *testing.T) {
	snapshot, hotkey, coldkey := lifecyclePruneFixture(t)
	if err := validateFleetLifecyclePruneSnapshot(snapshot, hotkey, coldkey); err != nil {
		t.Fatal(err)
	}
}

func TestFleetLifecyclePruneProofRejectsForeignLowerUIDCandidate(t *testing.T) {
	snapshot, hotkey, coldkey := lifecyclePruneFixture(t)
	snapshot.Inputs[2].RegistrationBlock = 9
	if err := validateFleetLifecyclePruneSnapshot(snapshot, hotkey, coldkey); err == nil {
		t.Fatal("foreign earlier prune candidate unexpectedly accepted")
	}
}

func TestFleetLifecyclePruneProofRejectsNoncanonicalUIDOrder(t *testing.T) {
	snapshot, hotkey, coldkey := lifecyclePruneFixture(t)
	snapshot.Inputs[1], snapshot.Inputs[2] = snapshot.Inputs[2], snapshot.Inputs[1]
	if err := validateFleetLifecyclePruneSnapshot(snapshot, hotkey, coldkey); err == nil {
		t.Fatal("out-of-order UID census unexpectedly accepted")
	}
}

func TestFleetLifecyclePruneProofRejectsNonzeroTargetEmission(t *testing.T) {
	snapshot, hotkey, coldkey := lifecyclePruneFixture(t)
	snapshot.Inputs[1].EmissionRao = 1
	snapshot.RuntimePruneUID = 2
	if err := validateFleetLifecyclePruneSnapshot(snapshot, hotkey, coldkey); err == nil {
		t.Fatal("emitting demotion target unexpectedly accepted")
	}
}

func TestFleetLifecycleCandidateMembershipChangesOnlyAtEffectiveEpochs(t *testing.T) {
	cfg := testResolvedConfig(t)
	evidence := &FleetLifecycleEvidence{FallbackEffectiveEpoch: 10, ProviderEffectiveEpoch: 12, TerminalEffectiveEpoch: 14}
	before := fleetLifecycleCandidateMinerSet(cfg, evidence, 9)
	fallback := fleetLifecycleCandidateMinerSet(cfg, evidence, 10)
	provider := fleetLifecycleCandidateMinerSet(cfg, evidence, 12)
	terminal := fleetLifecycleCandidateMinerSet(cfg, evidence, 14)
	if len(before) != 808 || len(fallback) != 808 || len(provider) != 808 || len(terminal) != 808 {
		t.Fatalf("candidate census before/fallback/provider/terminal=%d/%d/%d/%d, want 808", len(before), len(fallback), len(provider), len(terminal))
	}
	oldTarget := fleetMemberMinerIndex(cfg, fleetLifecycleTargetFleet, 1)
	companion := fleetMemberMinerIndex(cfg, fleetLifecycleCompanionFleet, 1)
	fallbackMiner, err := fleetLifecycleFallbackMinerIndex(cfg, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !before[oldTarget] || before[fallbackMiner] || fallback[oldTarget] || !fallback[fallbackMiner] || !fallback[companion] || !provider[oldTarget] || provider[companion] || !provider[fallbackMiner] || !terminal[oldTarget] || !terminal[companion] || terminal[fallbackMiner] {
		t.Fatalf("unexpected membership transition old=%d companion=%d fallback=%d", oldTarget, companion, fallbackMiner)
	}
}

func TestFleetLifecycleFallbackMembersShareTheReplacedProviderOperator(t *testing.T) {
	cfg := testResolvedConfig(t)
	wantOperator := operatorForMiner(cfg, fleetMemberMinerIndex(cfg, fleetLifecycleTargetFleet, 1))
	seen := map[int]bool{}
	for member := 1; member <= cfg.Config.Topology.ClientsPerHeadFleet; member++ {
		miner, err := fleetLifecycleFallbackMinerIndex(cfg, member)
		if err != nil {
			t.Fatal(err)
		}
		if seen[miner] || operatorForMiner(cfg, miner) != wantOperator {
			t.Fatalf("fallback member %d miner=%d operator=%d, want unique operator %d", member, miner, operatorForMiner(cfg, miner), wantOperator)
		}
		seen[miner] = true
	}
}
