// Plan revision tests cover safe continuation after a locked release changes
// during an interrupted, partially registered testnet deployment.
package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

func actionByID(t *testing.T, plan *SetupPlan, id string) Action {
	t.Helper()
	for _, action := range plan.Actions {
		if action.ID == id {
			return action
		}
	}
	t.Fatalf("plan has no action %s", id)
	return Action{}
}

func TestPlanRevisionFindsEveryUnverifiedTransactionAcrossLineage(t *testing.T) {
	prior := &SetupPlan{PlanHash: "0x" + strings.Repeat("11", 32), PriorPlanHashes: []string{"0x" + strings.Repeat("22", 32)}}
	ancestor := prior.PriorPlanHashes[0]
	entries := []JournalEntry{
		{PlanHash: prior.PlanHash, ActionID: "pending", IntentHash: "intent-pending", Stage: StageBroadcast, TransactionHash: "0x01", RecoveryBlock: 10, RecoveryBlockHash: "0xaa"},
		{PlanHash: prior.PlanHash, ActionID: "pending", IntentHash: "intent-pending", Stage: StageFailed, Error: "timeout"},
		{PlanHash: prior.PlanHash, ActionID: "verified", IntentHash: "intent-verified", Stage: StageBroadcast, TransactionHash: "0x02", RecoveryBlock: 10, RecoveryBlockHash: "0xaa"},
		{PlanHash: prior.PlanHash, ActionID: "verified", IntentHash: "intent-verified", Stage: StageVerified},
		{PlanHash: ancestor, ActionID: "reverted", IntentHash: "intent-reverted", Stage: StageBroadcast, TransactionHash: "0x03", RecoveryBlock: 11, RecoveryBlockHash: "0xbb"},
		{PlanHash: ancestor, ActionID: "reverted", IntentHash: "intent-reverted", Stage: StageIncluded, TransactionHash: "0x03", BlockNumber: 12, BlockHash: "0xcc"},
		{PlanHash: ancestor, ActionID: "reverted", IntentHash: "intent-reverted", Stage: StageFailed, Error: "dispatch failed"},
		{PlanHash: prior.PlanHash, ActionID: "local-failure", IntentHash: "intent-local", Stage: StageFailed, Error: "preflight"},
		{PlanHash: "0x" + strings.Repeat("33", 32), ActionID: "outsider", IntentHash: "intent-outsider", Stage: StageBroadcast, TransactionHash: "0x04"},
	}
	pending, err := pendingPlanRevisionTransactions(prior, entries)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]planRevisionTransaction{}
	for _, transaction := range pending {
		got[transaction.ActionID] = transaction
	}
	if len(got) != 2 || got["pending"].TransactionHash != "0x01" || got["reverted"].TransactionHash != "0x03" || got["reverted"].BlockNumber != 12 {
		t.Fatalf("unverified revision transactions=%+v", pending)
	}
}

func TestPlanRevisionRejectsMultipleTransactionsAndMissingArtifacts(t *testing.T) {
	prior := &SetupPlan{PlanHash: "0x" + strings.Repeat("11", 32)}
	entries := []JournalEntry{
		{PlanHash: prior.PlanHash, ActionID: "action", IntentHash: "intent", Stage: StageBroadcast, TransactionHash: "0x01"},
		{PlanHash: prior.PlanHash, ActionID: "action", IntentHash: "intent", Stage: StageIncluded, TransactionHash: "0x02"},
	}
	if _, err := pendingPlanRevisionTransactions(prior, entries); err == nil || !strings.Contains(err.Error(), "multiple transactions") {
		t.Fatalf("multiple prior transactions were accepted: %v", err)
	}
	entries = entries[:1]
	if err := validatePlanRevisionTransactions(context.Background(), testResolvedConfig(t), t.TempDir(), prior, entries); err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("missing transaction artifact was accepted: %v", err)
	}
}

func partialRevisionFacts(t *testing.T, cfg *ResolvedConfig, count int) *SetupFacts {
	t.Helper()
	facts := *testSetupFacts()
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for churn := 1; churn <= count; churn++ {
		hotkey := roles.Substrate[churnHotkeyLabel(churn)]
		coldkey := roles.Substrate[churnColdkeyLabel(churn)]
		facts.ExistingUIDs = append(facts.ExistingUIDs, ExistingUIDFact{
			UID: uint16(len(facts.ExistingUIDs)), Hotkey: "0x" + hotkey.PublicKeyHex,
			Coldkey: "0x" + coldkey.PublicKeyHex, RegistrationBlock: uint64(100 + churn),
		})
	}
	facts.ExistingUIDCount = uint16(len(facts.ExistingUIDs))
	return &facts
}

func TestBuildPlanRevisionCarriesExactVerifiedIntentsAndRepairsFunding(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, _ := derivePublicRoles(cfg)
	prior, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	registration := actionByID(t, prior, "churn.register.1")
	entries := []JournalEntry{{
		DeploymentID: cfg.Config.Deployment.DeploymentID, PlanHash: prior.PlanHash,
		ActionID: registration.ID, IntentHash: registration.IntentHash, Stage: StageVerified,
	}}
	cfg.Config.Budgets.MaximumNativeTransactionFeeRao = 4_000_000
	current := partialRevisionFacts(t, cfg, 3)
	revised, err := buildPlanRevisionFromFacts(cfg, t.TempDir(), prior, current, entries, time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	if revised.Schema != "urnetwork-sim-plan-v2" || revised.NativeTransactionFeeLimitRao != 4_000_000 || len(revised.PriorPlanHashes) != 1 || revised.PriorPlanHashes[0] != prior.PlanHash || !revised.allowedPlanHashes()[prior.PlanHash] {
		t.Fatalf("revision lineage/fee limit = schema=%s fee=%d prior=%v", revised.Schema, revised.NativeTransactionFeeLimitRao, revised.PriorPlanHashes)
	}
	if revised.LiveFacts.ExistingUIDCount != prior.LiveFacts.ExistingUIDCount || len(revised.LiveFacts.ExistingUIDs) != len(prior.LiveFacts.ExistingUIDs) {
		t.Fatalf("revision baseline drifted to partial live topology: count=%d facts=%d", revised.LiveFacts.ExistingUIDCount, len(revised.LiveFacts.ExistingUIDs))
	}
	if actionByID(t, revised, registration.ID).IntentHash != registration.IntentHash {
		t.Fatal("unchanged successful registration intent was not carryable")
	}
	if got := actionByID(t, revised, "churn.fund.1").Spend.TAORao; got != 5_000_500 {
		t.Fatalf("revised role funding = %d, want burn+fee+keep-alive 5000500", got)
	}
	if actionByID(t, revised, "churn.fund.1").IntentHash == actionByID(t, prior, "churn.fund.1").IntentHash {
		t.Fatal("funding without its own verified transfer evidence was treated as consumed")
	}
	remaining, err := remainingPlanSpend(revised, entries)
	if err != nil {
		t.Fatal(err)
	}
	if remaining.Registrations != revised.MaximumSpend.Registrations-1 {
		t.Fatalf("ancestor registration was not deducted: remaining=%d maximum=%d", remaining.Registrations, revised.MaximumSpend.Registrations)
	}
}

func TestBuildPlanRevisionPreservesDurablyConsumedRegistrationFunding(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, _ := derivePublicRoles(cfg)
	prior, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	funding := actionByID(t, prior, "churn.fund.1")
	registration := actionByID(t, prior, "churn.register.1")
	entries := []JournalEntry{
		{DeploymentID: cfg.Config.Deployment.DeploymentID, PlanHash: prior.PlanHash, ActionID: funding.ID, IntentHash: funding.IntentHash, Stage: StageVerified},
		{DeploymentID: cfg.Config.Deployment.DeploymentID, PlanHash: prior.PlanHash, ActionID: registration.ID, IntentHash: registration.IntentHash, Stage: StageVerified},
	}
	cfg.Config.Budgets.MaximumNativeTransactionFeeRao = 4_000_000
	revised, err := buildPlanRevisionFromFacts(cfg, t.TempDir(), prior, partialRevisionFacts(t, cfg, 1), entries, time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	carried := actionByID(t, revised, funding.ID)
	if carried.IntentHash != funding.IntentHash || carried.Spend != funding.Spend {
		t.Fatalf("consumed funding was rewritten: prior=%+v revised=%+v", funding, carried)
	}
	if repaired := actionByID(t, revised, "churn.fund.2"); repaired.Spend.TAORao != 5_000_500 || repaired.IntentHash == actionByID(t, prior, repaired.ID).IntentHash {
		t.Fatalf("unconsumed adjacent funding was not repaired: %+v", repaired)
	}
	remaining, err := remainingPlanSpend(revised, entries)
	if err != nil {
		t.Fatal(err)
	}
	if remaining.TAORao != revised.MaximumSpend.TAORao-funding.Spend.TAORao || remaining.Registrations != revised.MaximumSpend.Registrations-1 {
		t.Fatalf("carried funding/registration spend was not deducted exactly: maximum=%+v remaining=%+v", revised.MaximumSpend, remaining)
	}
}

func TestBuildPlanRevisionRepairsFundingWhenRegistrationIntentChanges(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, _ := derivePublicRoles(cfg)
	prior, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	funding := actionByID(t, prior, "churn.fund.1")
	registration := actionByID(t, prior, "churn.register.1")
	entries := []JournalEntry{
		{DeploymentID: cfg.Config.Deployment.DeploymentID, PlanHash: prior.PlanHash, ActionID: funding.ID, IntentHash: funding.IntentHash, Stage: StageVerified},
		{DeploymentID: cfg.Config.Deployment.DeploymentID, PlanHash: prior.PlanHash, ActionID: registration.ID, IntentHash: registration.IntentHash, Stage: StageVerified},
	}
	cfg.Config.Budgets.MaximumRegistrationBurnRao = 900_000
	revised, err := buildPlanRevisionFromFacts(cfg, t.TempDir(), prior, partialRevisionFacts(t, cfg, 1), entries, time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	repairedFunding := actionByID(t, revised, funding.ID)
	repairedRegistration := actionByID(t, revised, registration.ID)
	if repairedRegistration.IntentHash == registration.IntentHash || repairedFunding.IntentHash == funding.IntentHash || repairedFunding.Spend.TAORao != 3_900_500 {
		t.Fatalf("changed registration retained consumed funding: registration=%+v funding=%+v", repairedRegistration, repairedFunding)
	}
}

func TestBuildPlanRevisionRequiresTheRevisedRemainingBudget(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, _ := derivePublicRoles(cfg)
	prior, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Config.Budgets.MaximumNativeTransactionFeeRao = 4_000_000
	current := partialRevisionFacts(t, cfg, 1)
	revised, err := buildPlanRevisionFromFacts(cfg, t.TempDir(), prior, current, nil, time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	if revised.MaximumSpend.TAORao == 0 {
		t.Fatal("revised plan has no TAO budget")
	}
	current.WalletFreeTAORao = revised.MaximumSpend.TAORao - 1
	if _, err := buildPlanRevisionFromFacts(cfg, t.TempDir(), prior, current, nil, time.Unix(2, 0)); err == nil || !strings.Contains(err.Error(), "revised plan is not affordable") {
		t.Fatalf("unaffordable revised plan was accepted: %v", err)
	}
}

func TestPlanRevisionRejectsEveryAdjacentTopologyDrift(t *testing.T) {
	cfg := testResolvedConfig(t)
	publicRoles, _ := derivePublicRoles(cfg)
	prior, err := buildPlan(cfg, testSetupFacts(), publicRoles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func(*SetupFacts)
	}{
		{name: "external insertion", mutate: func(facts *SetupFacts) { facts.ExistingUIDs[2].Hotkey = "0x" + strings.Repeat("ee", 32) }},
		{name: "wrong coldkey", mutate: func(facts *SetupFacts) { facts.ExistingUIDs[2].Coldkey = "0x" + strings.Repeat("ef", 32) }},
		{name: "out of order", mutate: func(facts *SetupFacts) {
			roles, roleErr := BuildRoleSecrets(cfg)
			if roleErr != nil {
				t.Fatal(roleErr)
			}
			facts.ExistingUIDs[2].Hotkey = "0x" + roles.Substrate[churnHotkeyLabel(2)].PublicKeyHex
			facts.ExistingUIDs[2].Coldkey = "0x" + roles.Substrate[churnColdkeyLabel(2)].PublicKeyHex
		}},
		{name: "bootstrap mutation", mutate: func(facts *SetupFacts) { facts.ExistingUIDs[1].RegistrationBlock++ }},
		{name: "owner mutation", mutate: func(facts *SetupFacts) { facts.SubnetOwnerHotkey = "0x" + strings.Repeat("aa", 32) }},
	}
	for _, test := range tests {
		facts := partialRevisionFacts(t, cfg, 1)
		test.mutate(facts)
		_, err := buildPlanRevisionFromFacts(cfg, t.TempDir(), prior, facts, nil, time.Unix(2, 0))
		if err == nil {
			t.Errorf("%s was accepted", test.name)
		}
	}
}

func TestPlanRevisionRejectsDifferentDeploymentIdentity(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, _ := derivePublicRoles(cfg)
	prior, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func(*SetupPlan)
	}{
		{name: "deployment", mutate: func(plan *SetupPlan) { plan.DeploymentID = "other" }},
		{name: "netuid", mutate: func(plan *SetupPlan) { plan.Netuid++ }},
		{name: "owner", mutate: func(plan *SetupPlan) { plan.Owner = fmt.Sprintf("%s-other", plan.Owner) }},
		{name: "policy", mutate: func(plan *SetupPlan) { plan.PolicyHash = "0x" + strings.Repeat("12", 32) }},
	}
	for _, test := range tests {
		changed := *prior
		test.mutate(&changed)
		if _, err := buildPlanRevisionFromFacts(cfg, t.TempDir(), &changed, partialRevisionFacts(t, cfg, 1), nil, time.Unix(2, 0)); err == nil {
			t.Errorf("different %s was accepted", test.name)
		}
	}
}

func TestPlanRevisionTopologyAcceptsOnlyTheApprovedTournamentSequence(t *testing.T) {
	cfg := testResolvedConfig(t)
	publicRoles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	prior, err := buildPlan(cfg, testSetupFacts(), publicRoles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	stateDir := t.TempDir()
	deployment := ContractDeployment{
		Schema: "urnetwork-contract-deployment-v1", DeploymentID: cfg.Config.Deployment.DeploymentID,
		SettlementVault: common.HexToAddress("0x0000000000000000000000000000000000000521"),
	}
	if err := saveContractDeployment(stateDir, deployment); err != nil {
		t.Fatal(err)
	}
	roleSecrets, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	initialLabels := initialTopologyRoleLabels(cfg.Config.Topology)
	allLabels := append([]string(nil), initialLabels...)
	for challenger := 1; challenger <= cfg.Config.Topology.ChallengerFleets; challenger++ {
		allLabels = append(allLabels, fleetHotkeyLabel(cfg.Config.Topology.HeadFleets+challenger))
	}
	identities, err := expectedRegistrationIdentities(cfg, stateDir, roleSecrets, allLabels)
	if err != nil {
		t.Fatal(err)
	}
	fullFacts := func(replacements int) *SetupFacts {
		facts := *testSetupFacts()
		labels := append([]string(nil), initialLabels...)
		for index := 0; index < replacements; index++ {
			labels[index] = fleetHotkeyLabel(cfg.Config.Topology.HeadFleets + index + 1)
		}
		for index, label := range labels {
			identity := identities[label]
			facts.ExistingUIDs = append(facts.ExistingUIDs, ExistingUIDFact{
				UID: uint16(len(prior.LiveFacts.ExistingUIDs) + index), Hotkey: fmt.Sprintf("0x%x", identity.hotkey),
				Coldkey: fmt.Sprintf("0x%x", identity.coldkey), RegistrationBlock: uint64(1_000 + index),
			})
		}
		facts.ExistingUIDCount = uint16(len(facts.ExistingUIDs))
		return &facts
	}
	for replacements := 0; replacements <= cfg.Config.Topology.ChallengerFleets; replacements++ {
		if err := validatePlanRevisionTopology(cfg, stateDir, prior, fullFacts(replacements), roleSecrets); err != nil {
			t.Errorf("approved tournament prefix with %d replacements was rejected: %v", replacements, err)
		}
	}
	outOfOrder := fullFacts(0)
	challenger := identities[fleetHotkeyLabel(cfg.Config.Topology.HeadFleets+1)]
	outOfOrder.ExistingUIDs[len(prior.LiveFacts.ExistingUIDs)+1].Hotkey = fmt.Sprintf("0x%x", challenger.hotkey)
	outOfOrder.ExistingUIDs[len(prior.LiveFacts.ExistingUIDs)+1].Coldkey = fmt.Sprintf("0x%x", challenger.coldkey)
	if err := validatePlanRevisionTopology(cfg, stateDir, prior, outOfOrder, roleSecrets); err == nil {
		t.Fatal("out-of-order challenger replacement was accepted")
	}
	extra := fullFacts(0)
	extra.ExistingUIDs = append(extra.ExistingUIDs, ExistingUIDFact{UID: uint16(len(extra.ExistingUIDs)), Hotkey: "0x" + strings.Repeat("ab", 32), Coldkey: "0x" + strings.Repeat("cd", 32), RegistrationBlock: 2_000})
	extra.ExistingUIDCount++
	if err := validatePlanRevisionTopology(cfg, stateDir, prior, extra, roleSecrets); err == nil {
		t.Fatal("unapproved post-topology insertion was accepted")
	}
}
