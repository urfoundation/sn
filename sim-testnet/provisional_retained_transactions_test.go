package main

import (
	"strings"
	"testing"
)

// Preserve unresolved signed identities; neither an included block nor an
// arbitrary failure can claim the nonce is free. Known finalized failures pass.
func TestProvisionalPlanAdoptionRetainedStartupRequiresKnownTransactionOutcome(t *testing.T) {
	old := "0x" + strings.Repeat("11", 32)
	plan := &SetupPlan{PlanHash: "0x" + strings.Repeat("22", 32), PriorPlanHashes: []string{old}}
	broadcast := JournalEntry{PlanHash: old, ActionID: "synthetic.pending-deploy", IntentHash: "0x" + strings.Repeat("33", 32), Stage: StageBroadcast, Signer: "synthetic-signer", Nonce: "7", TransactionHash: "0x" + strings.Repeat("44", 32)}
	finalized := broadcast
	finalized.Stage, finalized.BlockNumber, finalized.BlockHash = StageFinalized, 10, "0x"+strings.Repeat("55", 32)
	verified := broadcast
	verified.Stage, verified.TransactionHash, verified.PostconditionPath, verified.PostconditionHash = StageVerified, "", "postconditions/synthetic.json", "0x"+strings.Repeat("66", 32)
	failed := broadcast
	failed.Stage, failed.Error = StageFailed, "synthetic RPC timeout: outcome unknown"
	included := finalized
	included.Stage = StageIncluded
	other := finalized
	other.ActionID = "synthetic.other-action"
	for _, entries := range [][]JournalEntry{{broadcast}, {broadcast, failed}, {broadcast, included, failed}, {broadcast, other}} {
		if err := validateRetainedProvisionalTransactionOutcomes(plan, entries); err == nil {
			t.Fatalf("unknown outcome was treated as permission to restart: %+v", entries)
		}
	}
	for _, entries := range [][]JournalEntry{{}, {broadcast, finalized}, {broadcast, finalized, failed}, {broadcast, verified}} {
		if err := validateRetainedProvisionalTransactionOutcomes(plan, entries); err != nil {
			t.Fatal("known retained outcome restarted its historical reconciliation", err)
		}
	}
	intent := broadcast
	intent.Stage, intent.TransactionHash = StageIntent, ""
	if err := validateRetainedProvisionalTransactionOutcomes(plan, []JournalEntry{intent}); err != nil {
		t.Fatal("never-broadcast pending setup blocked retained startup", err)
	}
	otherVerified := verified
	otherVerified.PlanHash = plan.PlanHash
	if err := validateRetainedProvisionalTransactionOutcomes(plan, []JournalEntry{broadcast, otherVerified}); err == nil {
		t.Fatal("another plan's receipt resolved an uncertain predecessor")
	}
}
