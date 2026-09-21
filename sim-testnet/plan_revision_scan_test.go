package main

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestRetiredEvmGasConsumesJournalOnceAcrossFleet(t *testing.T) {
	prior := &SetupPlan{PlanHash: "0x" + strings.Repeat("11", 32)}
	revised := &SetupPlan{PlanHash: "0x" + strings.Repeat("22", 32)}
	var entries []JournalEntry
	for index := range 64 {
		action := Action{ID: fmt.Sprintf("synthetic.transaction.%d", index), Kind: "evm-transaction", IntentHash: fmt.Sprintf("0x%064x", index+1), Spend: Spend{EVMGasWei: "101"}}
		prior.Actions = append(prior.Actions, action)
		replacement := action
		replacement.IntentHash = fmt.Sprintf("0x%064x", index+1000)
		revised.Actions = append(revised.Actions, replacement)
		for _, stage := range []JournalStage{StageIntent, StageBroadcast, StageIncluded, StageFinalized, StageVerified, StageVerified} {
			entries = append(entries, JournalEntry{PlanHash: prior.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: stage})
		}
	}
	passes, visits := 0, 0
	journal := func(yield func(JournalEntry) bool) {
		passes++
		for _, entry := range entries {
			visits++
			if !yield(entry) {
				return
			}
		}
	}
	got, err := retiredVerifiedEvmGasFromJournal(prior, revised, journal, Spend{EVMGasWei: "17"})
	if err != nil || got.EVMGasWei != "6481" {
		t.Fatalf("retired gas=%s want=6481 error=%v", got.EVMGasWei, err)
	}
	if passes != 1 || visits != len(entries) {
		t.Fatalf("journal traversals=%d row visits=%d want one pass over %d rows", passes, visits, len(entries))
	}
}

func TestRetiredEvmGasIndexPreservesExactApprovalAndAliasIntersection(t *testing.T) {
	planHash := "0x" + strings.Repeat("11", 32)
	ancestorHash := "0x" + strings.Repeat("22", 32)
	foreignHash := "0x" + strings.Repeat("33", 32)
	oldIntent := "0x" + strings.Repeat("44", 32)
	aliasIntent := "0x" + strings.Repeat("55", 32)
	newIntent := "0x" + strings.Repeat("66", 32)
	action := Action{ID: "synthetic.transaction", Kind: "evm-transaction", IntentHash: oldIntent, Spend: Spend{EVMGasWei: "11"}, AcceptedPriorIntentHashes: []string{aliasIntent}}
	verified := JournalEntry{PlanHash: planHash, ActionID: action.ID, IntentHash: oldIntent, Stage: StageVerified}
	for _, test := range []struct {
		name        string
		planHash    string
		stage       JournalStage
		intents     []string
		aliases     []string
		wantRetired string
	}{
		{name: "exact repeated verification", planHash: planHash, stage: StageVerified, intents: []string{oldIntent, oldIntent}, wantRetired: "11"},
		{name: "approved ancestor alias", planHash: ancestorHash, stage: StageVerified, intents: []string{aliasIntent}, wantRetired: "11"},
		{name: "carried alias intersection", planHash: ancestorHash, stage: StageVerified, intents: []string{aliasIntent, oldIntent}, aliases: []string{aliasIntent}, wantRetired: "0"},
		{name: "replacement-only intent cannot cancel prior spend", planHash: planHash, stage: StageVerified, intents: []string{oldIntent, newIntent}, wantRetired: "11"},
		{name: "foreign approval", planHash: foreignHash, stage: StageVerified, intents: []string{oldIntent}, wantRetired: "0"},
		{name: "finalized without verification", planHash: planHash, stage: StageFinalized, intents: []string{oldIntent}, wantRetired: "0"},
		{name: "unaccepted intent", planHash: planHash, stage: StageVerified, intents: []string{newIntent}, wantRetired: "0"},
	} {
		prior := &SetupPlan{PlanHash: planHash, PriorPlanHashes: []string{ancestorHash}, Actions: []Action{action}}
		replacement := action
		replacement.IntentHash = newIntent
		replacement.AcceptedPriorIntentHashes = test.aliases
		revised := &SetupPlan{Actions: []Action{replacement}}
		var entries []JournalEntry
		for _, intent := range test.intents {
			entry := verified
			entry.PlanHash, entry.Stage, entry.IntentHash = test.planHash, test.stage, intent
			entries = append(entries, entry)
		}
		// A receipt for another action must never enter this action's alias set.
		adjacent := verified
		adjacent.ActionID = "synthetic.adjacent"
		entries = append(entries, adjacent)
		got, err := addRetiredVerifiedEVMGas(prior, revised, entries, Spend{EVMGasWei: "0"})
		if err != nil || got.EVMGasWei != DecimalUint(test.wantRetired) {
			t.Errorf("%s: retired gas=%s want=%s error=%v", test.name, got.EVMGasWei, test.wantRetired, err)
		}
	}
}

func TestRetiredEvmGasIndexRejectsDuplicateRevisedActionBeforeJournal(t *testing.T) {
	prior := &SetupPlan{PlanHash: "0x" + strings.Repeat("11", 32)}
	action := Action{ID: "synthetic.duplicate", Kind: "evm-transaction", Spend: Spend{EVMGasWei: "11"}}
	revised := &SetupPlan{Actions: []Action{action, action}}
	visited := false
	_, err := retiredVerifiedEvmGasFromJournal(prior, revised, func(yield func(JournalEntry) bool) { visited = true }, Spend{})
	if err == nil || !strings.Contains(err.Error(), "duplicate action") || visited {
		t.Fatalf("duplicate action error=%v journal visited=%t", err, visited)
	}
}

func TestEvmGasCarrySkipsArchivesForIneligibleActions(t *testing.T) {
	prior := &SetupPlan{PlanHash: "0x" + strings.Repeat("11", 32), PriorPlanHashes: []string{"0x" + strings.Repeat("22", 32)}}
	ineligible := []Action{
		{ID: "synthetic.native", Kind: "substrate-extrinsic"},
		{ID: "synthetic.reconciliation", Kind: "substrate-reconciliation"},
		{ID: "synthetic.local", Kind: "local"},
		{ID: "synthetic.read", Kind: "evm-read"},
		{ID: "fleet.bind.synthetic-unbatched", Kind: "evm-read", Parameters: map[string]string{"batch_installed": "false"}},
		{ID: "synthetic.batch-read", Kind: "evm-read", Parameters: map[string]string{"batch_installed": "true"}},
	}
	revised := &SetupPlan{Actions: slices.Clone(ineligible)}
	var entries []JournalEntry
	for _, action := range ineligible {
		entries = append(entries, JournalEntry{PlanHash: prior.PriorPlanHashes[0], ActionID: action.ID, IntentHash: "0x" + strings.Repeat("33", 32), Stage: StageVerified})
	}
	ancestor := Action{ID: "synthetic.gas", Kind: "evm-transaction", IntentHash: "0x" + strings.Repeat("44", 32), Spend: Spend{EVMGasWei: "10"}, Parameters: map[string]string{evmMaximumGasUnitsParameter: "10"}}
	prior.Actions = []Action{ancestor}
	replacement := ancestor
	replacement.IntentHash = "0x" + strings.Repeat("55", 32)
	replacement.Spend.EVMGasWei = "20"
	replacement.Parameters = map[string]string{evmMaximumGasUnitsParameter: "20"}
	revised.Actions = append(revised.Actions, replacement)
	entries = append(entries, JournalEntry{PlanHash: prior.PlanHash, ActionID: ancestor.ID, IntentHash: ancestor.IntentHash, Stage: StageVerified})
	if err := preserveVerifiedEVMGasReallocations(t.TempDir(), revised, prior, entries); err != nil {
		t.Fatalf("ineligible archives blocked unrelated verified gas carry: %v", err)
	}
	if !reflect.DeepEqual(revised.Actions[:len(ineligible)], ineligible) || !reflect.DeepEqual(revised.Actions[len(ineligible)], ancestor) || revised.MaximumSpend.EVMGasWei != "10" {
		t.Fatalf("gas carry changed non-gas actions or lost the exact charged ancestor: maximum=%+v", revised.MaximumSpend)
	}
}

func TestEvmGasCarryStillAuthenticatesAllEligibleSourceKinds(t *testing.T) {
	prior := &SetupPlan{PlanHash: "0x" + strings.Repeat("11", 32), PriorPlanHashes: []string{"0x" + strings.Repeat("22", 32)}}
	for _, action := range []Action{
		{ID: "synthetic.transaction", Kind: "evm-transaction"},
		{ID: "fleet.bind.synthetic", Kind: "evm-read", Parameters: map[string]string{"batch_installed": "true"}},
		{ID: "fleet.mirror.synthetic", Kind: "evm-read", Parameters: map[string]string{"batch_installed": "true"}},
	} {
		revised := &SetupPlan{Actions: []Action{action}}
		entries := []JournalEntry{{PlanHash: prior.PriorPlanHashes[0], ActionID: action.ID, IntentHash: "0x" + strings.Repeat("33", 32), Stage: StageVerified}}
		if err := preserveVerifiedEVMGasReallocations(t.TempDir(), revised, prior, entries); err == nil || !strings.Contains(err.Error(), "read ancestor plan") {
			t.Errorf("%s missing eligible archive was accepted: %v", action.ID, err)
		}
	}
}

func TestEvmGasCarryStillRejectsDuplicateExactAncestor(t *testing.T) {
	action := Action{ID: "synthetic.transaction", Kind: "evm-transaction", IntentHash: "0x" + strings.Repeat("11", 32), Spend: Spend{EVMGasWei: "11"}}
	prior := &SetupPlan{PlanHash: "0x" + strings.Repeat("22", 32), Actions: []Action{action, action}}
	revised := &SetupPlan{Actions: []Action{action}}
	entries := []JournalEntry{{PlanHash: prior.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageVerified}}
	if err := preserveVerifiedEVMGasReallocations(t.TempDir(), revised, prior, entries); err == nil || !strings.Contains(err.Error(), "duplicate exact action") {
		t.Fatalf("duplicate exact gas ancestor was accepted: %v", err)
	}
}
