//go:build linux || darwin

// Closed pre-broadcast attempts retain their own approved action identity while
// completed setup remains bound to the later original signatures and receipts.
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// Two archived predecessors contain three closed attempts with a different
// activation target. The real journal writer precedes all original setup work.
func newRuntimeEvidenceSetupClosedAttemptsV2Test(t *testing.T) (*runtimeEvidenceProvisionV2TestFixture, *SetupPlan, []JournalEntry, [2]*SetupPlan) {
	t.Helper()
	var ancestors [2]*SetupPlan
	fixture := newRuntimeEvidenceSetupOriginalCarryHistoryV2Test(t, func(executor *Executor) {
		source := *executor.plan
		source.PriorPlanHashes = append(slices.Clone(source.PriorPlanHashes), source.PlanHash)
		for index := range ancestors {
			ancestor := *executor.plan
			ancestor.Actions = slices.Clone(ancestor.Actions)
			ancestor.PriorPlanHashes = slices.Clone(source.PriorPlanHashes)
			for i := range ancestor.Actions {
				if ancestor.Actions[i].ID == runtimeEvidenceActivationActionId(1, 1) {
					ancestor.Actions[i].Target = common.Address{0x71}.Hex()
					var err error
					ancestor.Actions[i].IntentHash, err = actionIntentHash(ancestor.Actions[i])
					if err != nil {
						t.Fatal(err)
					}
				}
			}
			var err error
			ancestor.PlanHash, err = ancestor.hash()
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(ancestor)
			if err != nil {
				t.Fatal(err)
			}
			if err := atomicWrite(filepath.Join(executor.stateDir, "plans", stringsTrim0x(ancestor.PlanHash)+".json"), append(encoded, '\n'), 0o600); err != nil {
				t.Fatal(err)
			}
			ancestors[index] = &ancestor
			source.PriorPlanHashes = append(source.PriorPlanHashes, ancestor.PlanHash)
		}
		var err error
		source.PlanHash, err = source.hash()
		if err != nil {
			t.Fatal(err)
		}
		executor.plan = &source
		journal, err := OpenJournal(executor.stateDir)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = journal.Close() })
		for _, ancestor := range []*SetupPlan{ancestors[0], ancestors[0], ancestors[1]} {
			action := actionByID(t, ancestor, runtimeEvidenceActivationActionId(1, 1))
			for _, stage := range []JournalStage{StageIntent, StageFailed} {
				entry := JournalEntry{DeploymentID: ancestor.DeploymentID, PlanHash: ancestor.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: stage}
				if stage == StageFailed {
					entry.Error = "synthetic pre-broadcast admission refusal"
				}
				if err := journal.Append(entry); err != nil {
					t.Fatal(err)
				}
			}
		}
		if err := journal.Close(); err != nil {
			t.Fatal(err)
		}
	})
	revised, entries := prepareRuntimeEvidenceSetupCarryV2Test(t, fixture)
	return fixture, revised, entries, ancestors
}

func TestRuntimeEvidenceSetupCarryV2ClosedAncestorAttemptsKeepOriginalSetup(t *testing.T) {
	t.Parallel()
	fixture, revised, entries, ancestors := newRuntimeEvidenceSetupClosedAttemptsV2Test(t)
	action := actionByID(t, fixture.plan, runtimeEvidenceActivationActionId(1, 1))
	oldAction := actionByID(t, ancestors[0], action.ID)
	if oldAction.Target == action.Target || oldAction.IntentHash == action.IntentHash || ancestors[0].PlanHash == ancestors[1].PlanHash {
		t.Fatal("fixture did not retain distinct ancestor approvals and executable targets")
	}
	if _, _, _, err := validatorEvidenceHistoryReceipt(revised, entries, action); err == nil {
		t.Fatal("generic historical receipt authentication accepted the foreign intent")
	}
	beforeEntries := slices.Clone(entries)
	view, err := runtimeEvidenceSetupCarryHistoryV2(fixture.stateDir, revised, fixture.plan, []Action{action}, entries)
	if err != nil || len(view) != len(entries)-6 || !reflect.DeepEqual(entries, beforeEntries) {
		t.Fatalf("local history did not omit exactly six closed rows without rewriting the journal: %v", err)
	}
	for _, entry := range view {
		if entry != entries[entry.Sequence-1] {
			t.Fatal("local view changed a retained journal hash or field")
		}
	}
	current := fixture.plan.LiveFacts
	current.DeployerNonce = fixture.plan.ValidatorEvidence.DeployerNonce + 1
	revised, err = buildPlanRevisionFromFacts(fixture.cfg, fixture.stateDir, fixture.plan, &current, entries, time.Unix(2, 0))
	if err != nil {
		t.Fatalf("actual revision refused completed original setup after closed ancestors: %v", err)
	}
	encoded, err := json.Marshal(revised)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(fixture.stateDir, "plan.json"), append(encoded, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	before := validatorNamespaceTreeSnapshot(t, fixture.stateDir)
	if err := validateRuntimeEvidenceSetupRevisionV2(fixture.cfg, fixture.stateDir, revised, fixture.roles, entries); err != nil {
		t.Fatalf("full setup resolver refused closed ancestor attempts: %v", err)
	}
	resolved, err := runtimeEvidenceV2ResolvedConfig(fixture.cfg, fixture.stateDir)
	if err != nil {
		t.Fatalf("reopened renderer refused original used setup: %v", err)
	}
	expected, _, err := runtimeEvidenceFixedInputsV2(fixture.cfg, fixture.plan, fixture.stateDir, fixture.roles, fixture.prepared, fixture.completed)
	if err != nil || !reflect.DeepEqual(resolved.Config.ValidatorEvidenceV2, expected) || !reflect.DeepEqual(before, validatorNamespaceTreeSnapshot(t, fixture.stateDir)) || revised.MaximumSpend != fixture.plan.MaximumSpend || revised.SupersededSpend != fixture.plan.SupersededSpend {
		t.Fatalf("closed attempts changed original signed setup, window, history or spending: %v", err)
	}
}

func TestRuntimeEvidenceSetupCarryV2ClosedAncestorAttemptsRejectUnfinishedProgress(t *testing.T) {
	t.Parallel()
	fixture, revised, original, ancestors := newRuntimeEvidenceSetupClosedAttemptsV2Test(t)
	for _, fault := range []string{"dangling", "failure-only", "nested", "reopened", "different-plan-close", "after-source", "current-plan", "intent-error"} {
		entries := slices.Clone(original)
		first, last := -1, -1
		for index, entry := range entries {
			if entry.PlanHash == ancestors[1].PlanHash {
				if first < 0 {
					first = index
				}
				last = index
			}
		}
		switch fault {
		case "dangling":
			entries = slices.Delete(entries, last, last+1)
		case "failure-only":
			entries = slices.Delete(entries, first, first+1)
		case "nested":
			entries = slices.Insert(entries, first+1, entries[first])
		case "reopened":
			entries = slices.Insert(entries, last+1, entries[first])
		case "different-plan-close":
			entries[last].PlanHash = ancestors[0].PlanHash
		case "after-source":
			pair := slices.Clone(entries[first : last+1])
			entries = append(slices.Delete(entries, first, last+1), pair...)
		case "current-plan":
			action := actionByID(t, revised, entries[first].ActionID)
			for index := first; index <= last; index++ {
				entries[index].PlanHash, entries[index].IntentHash = revised.PlanHash, action.IntentHash
			}
		case "intent-error":
			entries[first].Error = "synthetic error cannot be a fresh intent"
		}
		stateDir := runtimeReservedCreationStateTest(t, fixture.stateDir, entries)
		entries, err := readJournalEntries(stateDir)
		if err != nil {
			t.Fatalf("%s is not an authenticated journal counterexample: %v", fault, err)
		}
		if _, err := runtimeEvidenceSetupSourcePlanV2(fixture.cfg, revised, stateDir, fixture.roles, fixture.prepared, fixture.preparedBytes, fixture.completed, entries); err == nil {
			t.Fatalf("%s ancestor progress became completed original setup", fault)
		}
	}
}

func TestRuntimeEvidenceSetupCarryV2ClosedAncestorAttemptsRejectTransactionMetadata(t *testing.T) {
	t.Parallel()
	fixture, revised, original, ancestors := newRuntimeEvidenceSetupClosedAttemptsV2Test(t)
	for _, field := range []string{"Signer", "Nonce", "TransactionHash", "BlockNumber", "BlockHash", "RecoveryBlock", "RecoveryBlockHash", "FeeEstimateRao", "FeeLimitRao", "PostconditionHash", "PostconditionPath"} {
		for _, stage := range []JournalStage{StageIntent, StageFailed} {
			entries := slices.Clone(original)
			for index := range entries {
				if entries[index].PlanHash == ancestors[1].PlanHash && entries[index].Stage == stage {
					value := reflect.ValueOf(&entries[index]).Elem().FieldByName(field)
					if value.Kind() == reflect.String {
						value.SetString("synthetic durable metadata")
					} else {
						value.SetUint(1)
					}
				}
			}
			stateDir := runtimeReservedCreationStateTest(t, fixture.stateDir, entries)
			entries, err := readJournalEntries(stateDir)
			if err != nil {
				t.Fatalf("%s/%s is not an authenticated journal counterexample: %v", field, stage, err)
			}
			if _, err := runtimeEvidenceSetupSourcePlanV2(fixture.cfg, revised, stateDir, fixture.roles, fixture.prepared, fixture.preparedBytes, fixture.completed, entries); err == nil {
				t.Fatalf("%s/%s metadata was discarded as a pre-broadcast attempt", field, stage)
			}
		}
	}
}

func TestRuntimeEvidenceSetupCarryV2ClosedAncestorAttemptsAuthenticateOldApproval(t *testing.T) {
	t.Parallel()
	fixture, revised, original, ancestors := newRuntimeEvidenceSetupClosedAttemptsV2Test(t)
	for _, fault := range []string{"missing-archive", "archive-wire", "entry-intent", "source-lineage"} {
		entries := slices.Clone(original)
		if fault == "entry-intent" {
			for index := range entries {
				if entries[index].PlanHash == ancestors[1].PlanHash {
					entries[index].IntentHash = common.Hash{0x72}.Hex()
				}
			}
		}
		stateDir := runtimeReservedCreationStateTest(t, fixture.stateDir, entries)
		path := filepath.Join(stateDir, "plans", stringsTrim0x(ancestors[1].PlanHash)+".json")
		switch fault {
		case "missing-archive":
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		case "archive-wire":
			altered := *ancestors[1]
			altered.Owner = common.Address{0x73}.Hex()
			encoded, err := json.Marshal(altered)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, encoded, 0o600); err != nil {
				t.Fatal(err)
			}
		case "source-lineage":
			// Current approval alone cannot authorize omitting newer attempts.
			source := *fixture.plan
			source.PriorPlanHashes = slices.DeleteFunc(slices.Clone(source.PriorPlanHashes), func(hash string) bool { return hash == ancestors[1].PlanHash })
			action := actionByID(t, fixture.plan, runtimeEvidenceActivationActionId(1, 1))
			if _, err := runtimeEvidenceSetupCarryHistoryV2(stateDir, revised, &source, []Action{action}, entries); err == nil {
				t.Fatal("current-only allowed hash substituted for original source lineage")
			}
			continue
		}
		entries, err := readJournalEntries(stateDir)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := runtimeEvidenceSetupSourcePlanV2(fixture.cfg, revised, stateDir, fixture.roles, fixture.prepared, fixture.preparedBytes, fixture.completed, entries); err == nil {
			t.Fatalf("%s authenticated an unbound ancestor approval", fault)
		}
	}
}
