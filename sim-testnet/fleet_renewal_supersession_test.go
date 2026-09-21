// Retirement is limited to unsigned work whose complete successor is already
// finalized and verified. A started transaction still needs its exact outcome.
package main

import (
	"context"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// Minimal immutable identities make each ownership check independently mutable.
func fleetRenewalSupersessionFixture() (*SetupPlan, Action, []JournalEntry) {
	old := Action{ID: fleetRenewalActionID(1, 1, "bind", 1), Kind: "evm-transaction", IntentHash: common.Hash{0x40}.Hex()}
	plan := &SetupPlan{PlanHash: common.Hash{0x41}.Hex(), PriorPlanHashes: []string{common.Hash{0x42}.Hex()}, Actions: []Action{old},
		FleetRenewals: []FleetRenewal{{Round: 1, ValidFromEpoch: 10}, {Round: 2, ValidFromEpoch: 20, Fleets: []FleetRenewalFleet{{Fleet: 1, Members: make([]FleetRenewalMember, 4)}}}}}
	var entries []JournalEntry
	for _, operation := range []string{"commitment", "mirror", "bind"} {
		count := 1
		if operation == "bind" {
			count = 4
		}
		for index := range count {
			member := 0
			if operation == "bind" {
				member = index + 1
			}
			action := Action{ID: fleetRenewalActionID(2, 1, operation, member), IntentHash: common.Hash{0x43, byte(len(plan.Actions))}.Hex()}
			plan.Actions = append(plan.Actions, action)
			entries = append(entries, JournalEntry{PlanHash: plan.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageFinalized, TransactionHash: common.Hash{0x44, byte(len(plan.Actions))}.Hex()},
				JournalEntry{PlanHash: plan.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageVerified})
		}
	}
	return plan, old, entries
}

// Setup and final capture both accept a explicitly retired, never-signed action.
func TestFleetRenewalSupersededUnsignedWorkDoesNotRestartOrRequireReceipt(t *testing.T) {
	plan, old, entries := fleetRenewalSupersessionFixture()
	if !fleetRenewalSupersededUnsignedActions(plan, entries)[old.ID] {
		t.Fatal("complete successor did not retire unsigned expired predecessor work")
	}
	executor := &Executor{plan: plan, journal: &Journal{entries: entries}}
	if err := executeSetupActions(context.Background(), executor, []Action{old}, old.ID); err != nil {
		t.Fatalf("setup tried to restart expired unsigned renewal work: %v", err)
	}
	if _, err := captureFinalFleetRenewalTransactionEntries(context.Background(), t.TempDir(), plan, entries); err != nil {
		t.Fatalf("capture required a receipt for a transaction never submitted: %v", err)
	}
}

// A signature, missing successor receipt, or foreign approval forbids retirement.
func TestFleetRenewalSupersessionPreservesSignedAndUnresolvedActions(t *testing.T) {
	for _, fault := range []string{"signed", "unfinalized", "unverified", "foreign-plan", "foreign-intent", "same-window"} {
		plan, old, entries := fleetRenewalSupersessionFixture()
		switch fault {
		case "signed":
			entries = append(entries, JournalEntry{PlanHash: plan.PriorPlanHashes[0], ActionID: old.ID, IntentHash: old.IntentHash, Stage: StageBroadcast, TransactionHash: common.Hash{0x45}.Hex()})
		case "unfinalized":
			entries[0].Stage = StageBroadcast
		case "unverified":
			entries[1].Stage = StageIntent
		case "foreign-plan":
			entries[0].PlanHash = common.Hash{0x46}.Hex()
		case "foreign-intent":
			entries[1].IntentHash = common.Hash{0x47}.Hex()
		case "same-window":
			plan.FleetRenewals[1].ValidFromEpoch = plan.FleetRenewals[0].ValidFromEpoch
		}
		if fleetRenewalSupersededUnsignedActions(plan, entries)[old.ID] {
			t.Fatalf("retired unresolved predecessor with %s", fault)
		}
	}
}
