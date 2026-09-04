package main

import (
	"slices"
	"strings"
	"testing"
	"time"
)

func TestContractRegistrationRoleGenerationsAreDeterministicAndChurnBounded(t *testing.T) {
	topology := testResolvedConfig(t).Config.Topology
	if got, want := maximumContractRegistrationGeneration(topology), uint64(15); got != want {
		t.Fatalf("maximum generation=%d, want %d", got, want)
	}
	if escrowHotkeyLabelForGeneration(0) != "escrow-hotkey" || escrowHotkeyLabelForGeneration(1) != "escrow-hotkey-generation-1" {
		t.Fatal("escrow generation labels are not backward-compatible and scoped")
	}
	if operatorPoolHotkeyLabelForGeneration(2, 0) != "operator-2-pool-hotkey" || operatorPoolHotkeyLabelForGeneration(2, 1) != "operator-2-pool-hotkey-generation-1" {
		t.Fatal("operator generation labels are not backward-compatible and scoped")
	}
	for _, label := range contractRegistrationRoleLabels(topology, 1) {
		generation, _, _, ok := parseContractRegistrationRoleLabel(topology, label)
		if !ok || generation != 1 {
			t.Fatalf("generation-one label %q parsed as generation=%d ok=%t", label, generation, ok)
		}
	}
	if err := validateContractRegistrationGeneration(topology, 15); err != nil {
		t.Fatal(err)
	}
	if err := validateContractRegistrationGeneration(topology, 16); err == nil {
		t.Fatal("generation without enough reserved churn identities was accepted")
	}
}

func TestRegistrationGenerationPromotionRequiresOnlyTheExactFinalizedReservePrefix(t *testing.T) {
	cfg := testResolvedConfig(t)
	publicRoles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	prior, err := buildPlan(cfg, testSetupFacts(), publicRoles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	prior.SupersededSpend.Registrations = uint32(contractRegistrationRoleCount(cfg.Config.Topology))
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	planned, err := buildDeploymentPayloadsWithRegistrationGeneration(cfg, roles, prior.Deployment.InitialNonce, 1)
	if err != nil {
		t.Fatal(err)
	}
	reserve := actionByID(t, prior, "evm.reserve-sink")
	entries := []JournalEntry{{PlanHash: prior.PlanHash, ActionID: reserve.ID, IntentHash: reserve.IntentHash, Stage: StageVerified}}
	if err := validateRegistrationRoleGenerationPromotion(cfg, prior, prior.Deployment, planned.Manifest, prior.Deployment.InitialNonce+1, entries); err != nil {
		t.Fatalf("exact post-reserve promotion was rejected: %v", err)
	}
	if err := validateRegistrationRoleGenerationPromotion(cfg, prior, prior.Deployment, planned.Manifest, prior.Deployment.InitialNonce+2, entries); err == nil || !strings.Contains(err.Error(), "post-reserve nonce") {
		t.Fatalf("post-vault nonce was accepted: %v", err)
	}
	settlement := actionByID(t, prior, "evm.settlement-vault")
	unsafe := append(entries, JournalEntry{PlanHash: prior.PlanHash, ActionID: settlement.ID, IntentHash: settlement.IntentHash, Stage: StageBroadcast, TransactionHash: "0x" + strings.Repeat("11", 32)})
	if err := validateRegistrationRoleGenerationPromotion(cfg, prior, prior.Deployment, planned.Manifest, prior.Deployment.InitialNonce+1, unsafe); err == nil || !strings.Contains(err.Error(), "settlement-vault") {
		t.Fatalf("broadcast vault constructor was accepted: %v", err)
	}
	failedEstimate := append(entries, JournalEntry{PlanHash: prior.PlanHash, ActionID: settlement.ID, IntentHash: settlement.IntentHash, Stage: StageFailed, Error: "gas guard"})
	if err := validateRegistrationRoleGenerationPromotion(cfg, prior, prior.Deployment, planned.Manifest, prior.Deployment.InitialNonce+1, failedEstimate); err != nil {
		t.Fatalf("pre-broadcast failed estimate incorrectly consumed the immutable constructor: %v", err)
	}
	drifted := planned.Manifest
	drifted.RuntimeHashes = cloneStrings(planned.Manifest.RuntimeHashes)
	drifted.RuntimeHashes[drifted.ReserveSink.Hex()] = "0x" + strings.Repeat("22", 32)
	if err := validateRegistrationRoleGenerationPromotion(cfg, prior, prior.Deployment, drifted, prior.Deployment.InitialNonce+1, entries); err == nil {
		t.Fatal("reserve runtime drift was accepted")
	}
}

func TestGenerationPromotionCarriesOnlyByteEquivalentReserveIntent(t *testing.T) {
	cfg := testResolvedConfig(t)
	publicRoles, _ := derivePublicRoles(cfg)
	prior, err := buildPlan(cfg, testSetupFacts(), publicRoles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	revised, err := buildPlanWithRegistrationGeneration(cfg, testSetupFacts(), publicRoles, time.Unix(2, 0), 1)
	if err != nil {
		t.Fatal(err)
	}
	reserve := actionByID(t, prior, "evm.reserve-sink")
	entries := []JournalEntry{{PlanHash: prior.PlanHash, ActionID: reserve.ID, IntentHash: reserve.IntentHash, Stage: StageVerified}}
	if err := carryVerifiedGenerationIndependentReserve(revised, prior, entries); err != nil {
		t.Fatal(err)
	}
	carried := actionByID(t, revised, "evm.reserve-sink")
	if !actionAcceptsIntent(carried, reserve.IntentHash) || carried.IntentHash == reserve.IntentHash || len(carried.AcceptedPriorIntentHashes) != 1 {
		t.Fatalf("reserve carry is not explicit and separately hashed: prior=%s revised=%+v", reserve.IntentHash, carried)
	}
	mutated := *prior
	mutated.Actions = append([]Action(nil), prior.Actions...)
	for index := range mutated.Actions {
		if mutated.Actions[index].ID == "evm.reserve-sink" {
			mutated.Actions[index].Parameters = cloneStrings(mutated.Actions[index].Parameters)
			mutated.Actions[index].Parameters["expected_data_keccak256"] = "0x" + strings.Repeat("33", 32)
			mutated.Actions[index].IntentHash, _ = actionIntentHash(mutated.Actions[index])
			entries[0].IntentHash = mutated.Actions[index].IntentHash
		}
	}
	if err := carryVerifiedGenerationIndependentReserve(revised, &mutated, entries); err == nil {
		t.Fatal("non-equivalent reserve transaction was carried")
	}
}

func TestGenerationOneTopologyReplacesContractRolesBeforeChallengers(t *testing.T) {
	topology := testResolvedConfig(t).Config.Topology
	base := baseInitialTopologyRoleLabels(topology)
	initial := initialTopologyRoleLabels(topology, 1)
	tournament := tournamentTopologyRoleLabels(topology, 1)
	if len(base) != 254 || len(initial) != len(base) || len(tournament) != len(base) {
		t.Fatalf("fixed topology sizes base/initial/tournament=%d/%d/%d", len(base), len(initial), len(tournament))
	}
	for index, label := range contractRegistrationRoleLabels(topology, 1) {
		if initial[index] != label || tournament[index] != label {
			t.Fatalf("contract replacement %d=%q/%q, want %q", index, initial[index], tournament[index], label)
		}
	}
	for challenger := 1; challenger <= topology.ChallengerFleets; challenger++ {
		index := contractRegistrationRoleCount(topology) + challenger - 1
		want := fleetHotkeyLabel(topology.HeadFleets + challenger)
		if initial[index] != churnHotkeyLabel(index+1) || tournament[index] != want {
			t.Fatalf("challenger %d topology slot %d=%q/%q, want churn then %q", challenger, index, initial[index], tournament[index], want)
		}
	}
	for _, retired := range contractRegistrationRoleLabels(topology, 0) {
		if !slices.Contains(initial, retired) || !slices.Contains(tournament, retired) {
			t.Fatalf("retired generation-zero role %q disappeared outside bounded pruning", retired)
		}
	}
	if _, err := topologyRoleLabelsAtProgress(topology, 1, 2, 1); err == nil {
		t.Fatal("challenger was allowed before all generation-one contract roles")
	}
}

func TestGenerationOnePlanBindsEveryReplacementToItsExactChurnAction(t *testing.T) {
	cfg := testResolvedConfig(t)
	publicRoles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildPlanWithRegistrationGeneration(cfg, testSetupFacts(), publicRoles, time.Unix(1, 0), 1)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Deployment.RegistrationRoleGeneration != 1 {
		t.Fatalf("deployment generation=%d", plan.Deployment.RegistrationRoleGeneration)
	}
	wants := map[string]string{
		"evm.vault-register-escrow": "1",
		"operator.register.1":       "2",
		"operator.register.2":       "3",
		"fleet.register.201":        "4",
		"fleet.register.202":        "5",
	}
	for actionID, churn := range wants {
		action := actionByID(t, plan, actionID)
		if action.Parameters["expected_replaced_churn"] != churn || !slices.Contains(action.DependsOn, "churn.register."+churn) {
			t.Fatalf("action %s replacement=%q dependencies=%v, want churn %s", actionID, action.Parameters["expected_replaced_churn"], action.DependsOn, churn)
		}
	}
	for _, actionID := range []string{"evm.vault-register-escrow", "operator.register.1", "operator.register.2"} {
		if actionByID(t, plan, actionID).Parameters["registration_role_generation"] != "1" {
			t.Fatalf("action %s does not bind generation one", actionID)
		}
	}
}

func TestSupersededRegistrationSpendSelectsOnlyExactWholeGenerations(t *testing.T) {
	topology := testResolvedConfig(t).Config.Topology
	plan := &SetupPlan{SupersededSpend: Spend{Registrations: 3}}
	if generation, err := contractRegistrationGenerationFromSupersededSpend(topology, plan); err != nil || generation != 1 {
		t.Fatalf("generation=%d err=%v, want one", generation, err)
	}
	plan.SupersededSpend.Registrations = 2
	if _, err := contractRegistrationGenerationFromSupersededSpend(topology, plan); err == nil {
		t.Fatal("partial superseded contract generation was accepted")
	}
	plan.SupersededSpend.Registrations = 48
	if _, err := contractRegistrationGenerationFromSupersededSpend(topology, plan); err == nil {
		t.Fatal("generation beyond reserved churn capacity was accepted")
	}
}
