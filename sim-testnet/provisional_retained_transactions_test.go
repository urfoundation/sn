package main

import (
	"reflect"
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

// Reusing an action identity cannot let its old receipt authorize a new hash.
func TestProvisionalPlanAdoptionRetainedStartupOrdersExactTransactionReceipts(t *testing.T) {
	plan := &SetupPlan{PlanHash: "0x" + strings.Repeat("11", 32)}
	first := JournalEntry{PlanHash: plan.PlanHash, ActionID: "synthetic.reused-action", IntentHash: "0x" + strings.Repeat("22", 32), Stage: StageBroadcast, Signer: "synthetic-signer", Nonce: "7", TransactionHash: "0x" + strings.Repeat("33", 32)}
	second := first
	second.Nonce, second.TransactionHash = "8", "0x"+strings.Repeat("44", 32)
	verified := first
	verified.Stage, verified.TransactionHash = StageVerified, ""
	verified.PostconditionPath, verified.PostconditionHash = "postconditions/synthetic.json", "0x"+strings.Repeat("55", 32)
	exact := verified
	exact.TransactionHash = first.TransactionHash
	for _, entries := range [][]JournalEntry{
		{verified, first},
		{first, verified, second},
		{first, exact, second},
		{first, second, verified},
		{first, second, exact},
	} {
		if err := validateRetainedProvisionalTransactionOutcomes(plan, entries); err == nil {
			t.Fatalf("receipt resolved a later or ambiguous signed hash: %+v", entries)
		}
	}
	for _, entries := range [][]JournalEntry{{first, verified, first}, {first, exact, first}} {
		if err := validateRetainedProvisionalTransactionOutcomes(plan, entries); err != nil {
			t.Fatal("exact retained rebroadcast lost its prior authenticated outcome", err)
		}
	}
}

// Later finalized nonce progress consumes old slots without promoting their
// unknown effects to verified actions or rewriting any retained evidence.
func TestProvisionalPlanAdoptionRetainedStartupAcceptsConsumedSlotsWithoutRewriting(t *testing.T) {
	old := "0x" + strings.Repeat("11", 32)
	plan := &SetupPlan{PlanHash: "0x" + strings.Repeat("22", 32), PriorPlanHashes: []string{old}}
	prior := JournalEntry{PlanHash: old, ActionID: "synthetic.unresolved-effect", IntentHash: "0x" + strings.Repeat("33", 32), Stage: StageBroadcast, Signer: "0x" + strings.Repeat("aB", 20), Nonce: "7", TransactionHash: "0x" + strings.Repeat("44", 32)}
	failed := prior
	failed.Stage, failed.Error = StageFailed, "historical effect remains unknown"
	later := prior
	later.PlanHash, later.ActionID, later.IntentHash = plan.PlanHash, "synthetic.later-action", "0x"+strings.Repeat("55", 32)
	later.Signer, later.Nonce, later.TransactionHash = strings.ToLower(prior.Signer), "8", "0x"+strings.Repeat("66", 32)
	finalized := later
	finalized.Stage, finalized.BlockNumber, finalized.BlockHash = StageFinalized, 17, "0x"+strings.Repeat("77", 32)
	finalized.Signer, finalized.Nonce = "", ""
	entries := []JournalEntry{prior, failed, later, finalized}
	before := append([]JournalEntry(nil), entries...)
	if err := validateRetainedProvisionalTransactionOutcomes(plan, entries); err != nil {
		t.Fatal("durably consumed slot reopened old historical reconciliation", err)
	}
	if !reflect.DeepEqual(entries, before) {
		t.Fatal("startup rewrote an unknown historical effect as completed")
	}
	// Equal nonce finalization also closes the slot, without claiming which
	// transaction's effects happened. Failure finalization still consumes it.
	later.Nonce = prior.Nonce
	finalized.Error = "synthetic finalized dispatch failure"
	if err := validateRetainedProvisionalTransactionOutcomes(plan, []JournalEntry{prior, failed, later, finalized}); err != nil {
		t.Fatal("known failed replacement left a consumed nonce open", err)
	}
	// Native signer identity is preserved exactly, independently of EVM spelling.
	prior.Signer, later.Signer = "synthetic-native-Signer", "synthetic-native-Signer"
	if err := validateRetainedProvisionalTransactionOutcomes(plan, []JournalEntry{prior, later, finalized}); err != nil {
		t.Fatal("same native signer lost its consumed slot", err)
	}
}

// Consumption needs an exact preceding broadcast plus finalized coordinate in
// the admitted lineage, with a sufficient nonce from the same account/domain.
func TestProvisionalPlanAdoptionRetainedStartupRejectsUnprovedSlotConsumption(t *testing.T) {
	plan := &SetupPlan{PlanHash: "0x" + strings.Repeat("11", 32)}
	prior := JournalEntry{PlanHash: plan.PlanHash, ActionID: "synthetic.pending", IntentHash: "0x" + strings.Repeat("22", 32), Stage: StageBroadcast, Signer: "synthetic-native-Signer", Nonce: "7", TransactionHash: "0x" + strings.Repeat("33", 32)}
	later := prior
	later.ActionID, later.IntentHash, later.Nonce, later.TransactionHash = "synthetic.other", "0x"+strings.Repeat("44", 32), "8", "0x"+strings.Repeat("55", 32)
	finalized := later
	finalized.Stage, finalized.BlockNumber, finalized.BlockHash = StageFinalized, 10, "0x"+strings.Repeat("66", 32)
	finalized.Signer, finalized.Nonce = "", ""
	for _, name := range []string{"different-signer", "native-case", "lower-nonce", "unknown-plan", "no-broadcast", "included-only", "missing-block", "bad-block", "missing-signer", "missing-nonce", "bad-nonce", "noncanonical-nonce", "finalized-before-broadcast"} {
		changed, outcome := later, finalized
		entries := []JournalEntry{prior}
		switch name {
		case "different-signer":
			changed.Signer = "0x" + strings.Repeat("ab", 20)
		case "native-case":
			changed.Signer = strings.ToLower(prior.Signer)
		case "lower-nonce":
			changed.Nonce = "6"
		case "unknown-plan":
			changed.PlanHash, outcome.PlanHash = "0x"+strings.Repeat("77", 32), "0x"+strings.Repeat("77", 32)
		case "included-only":
			outcome.Stage = StageIncluded
		case "missing-block":
			outcome.BlockNumber = 0
		case "bad-block":
			outcome.BlockHash = "changed"
		case "missing-signer":
			changed.Signer = ""
		case "missing-nonce":
			changed.Nonce = ""
		case "bad-nonce":
			changed.Nonce = "18446744073709551616"
		case "noncanonical-nonce":
			changed.Nonce = "08"
		}
		if name == "finalized-before-broadcast" {
			entries = append(entries, outcome, changed)
		} else {
			if name != "no-broadcast" {
				entries = append(entries, changed)
			}
			entries = append(entries, outcome)
		}
		if err := validateRetainedProvisionalTransactionOutcomes(plan, entries); err == nil {
			t.Fatalf("%s resolved a still-unknown signed slot", name)
		}
	}
}

// Neither duplicate broadcasts nor finalization may substitute signed fields.
func TestProvisionalPlanAdoptionRetainedStartupRejectsChangedSignedSlot(t *testing.T) {
	plan := &SetupPlan{PlanHash: "0x" + strings.Repeat("11", 32)}
	broadcast := JournalEntry{PlanHash: plan.PlanHash, ActionID: "synthetic.exact", IntentHash: "0x" + strings.Repeat("22", 32), Stage: StageBroadcast, Signer: "0x" + strings.Repeat("ab", 20), Nonce: "7", TransactionHash: "0x" + strings.Repeat("33", 32)}
	for _, stage := range []JournalStage{StageBroadcast, StageFinalized} {
		for _, field := range []string{"signer", "nonce"} {
			changed := broadcast
			changed.Stage, changed.BlockNumber, changed.BlockHash = stage, 10, "0x"+strings.Repeat("44", 32)
			if field == "signer" {
				changed.Signer = "0x" + strings.Repeat("cd", 20)
			} else {
				changed.Nonce = "8"
			}
			if err := validateRetainedProvisionalTransactionOutcomes(plan, []JournalEntry{broadcast, changed}); err == nil || !strings.Contains(err.Error(), "changed") {
				t.Fatalf("%s substituted %s: %v", stage, field, err)
			}
		}
	}
}
