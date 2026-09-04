package main

// This file exercises the production carried-history cache with the complete
// forty-batch shape without requiring a live chain or transaction sender.

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
)

const qualificationFleetBatchCount = 40

// Builds append-only ancestor receipts for the exact cache path. Budget
// actions make the cache transition observable without introducing a live RPC
// dependency; the actual batch-generation binding is covered separately.
func newCarriedFleetBatchHistoryFixture(t *testing.T, carried int) (*Executor, []Action, *Journal) {
	t.Helper()
	if carried < 0 || carried > qualificationFleetBatchCount {
		t.Fatalf("carried batch count %d is outside 0..%d", carried, qualificationFleetBatchCount)
	}
	cfg := testResolvedConfig(t)
	stateDir := t.TempDir()
	if err := os.Chmod(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	journal, err := OpenJournal(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := journal.Close(); err != nil {
			t.Error(err)
		}
	})
	ancestorHash := "0x" + strings.Repeat("a1", 32)
	activeHash := "0x" + strings.Repeat("b2", 32)
	actions := make([]Action, qualificationFleetBatchCount)
	for index := range actions {
		action := Action{
			ID:          fmt.Sprintf("fleet.qualification.batch.%d", index+1),
			Kind:        "budget-reserve",
			Target:      cfg.Config.Deployment.DeploymentID,
			Description: "qualification-only carried fleet batch cache boundary",
			Parameters:  map[string]string{},
		}
		if index >= carried {
			// The pending action intentionally reaches the executor dispatch
			// boundary and fails before any external sender is selected.
			action.Kind = "local"
		}
		intentHash, err := actionIntentHash(action)
		if err != nil {
			t.Fatal(err)
		}
		action.IntentHash = intentHash
		actions[index] = action
	}
	ancestorPlan := &SetupPlan{PlanHash: ancestorHash, Actions: append([]Action(nil), actions...)}
	activePlan := &SetupPlan{
		PlanHash: activeHash, PriorPlanHashes: []string{ancestorHash}, Actions: append([]Action(nil), actions...),
	}
	ancestorExecutor := &Executor{cfg: cfg, stateDir: stateDir, plan: ancestorPlan}
	for index := 0; index < carried; index++ {
		action := actions[index]
		record := &ActionPostcondition{
			Schema: "urnetwork-sim-action-postcondition-v4", DeploymentID: cfg.Config.Deployment.DeploymentID,
			PlanHash: ancestorHash, ActionID: action.ID, IntentHash: action.IntentHash,
			OperationalRPCMode: cfg.OperationalRPCMode, IndependentRPC: independentRPCRequired(cfg),
			SubstrateFinalized: ChainHead{Number: 10, Hash: "0x" + strings.Repeat("c3", 32)},
			EVMFinalized:       ChainHead{Number: 11, Hash: "0x" + strings.Repeat("d4", 32)}, EVMHashDomain: "evm-rpc",
			Observed:                      map[string]any{"kind": action.Kind, "batch": index + 1},
			IndependentSubstrateFinalized: ChainHead{Number: 10, Hash: "0x" + strings.Repeat("c3", 32)},
			IndependentEVMFinalized:       ChainHead{Number: 11, Hash: "0x" + strings.Repeat("d4", 32)}, IndependentEVMHashDomain: "evm-rpc",
			IndependentObserved: map[string]any{"kind": action.Kind, "batch": index + 1},
		}
		path, hash, err := ancestorExecutor.persistActionPostcondition(record)
		if err != nil {
			t.Fatal(err)
		}
		if err := journal.Append(JournalEntry{
			DeploymentID: cfg.Config.Deployment.DeploymentID, PlanHash: ancestorHash,
			ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageVerified,
			PostconditionHash: hash, PostconditionPath: path,
		}); err != nil {
			t.Fatal(err)
		}
	}
	return &Executor{cfg: cfg, stateDir: stateDir, plan: activePlan, journal: journal}, actions, journal
}

// Verifies the real preflight seeds every cache key before any carried batch
// can enter the mutation path, so all forty resumes are sender-free.
func TestVerifyCarriedActionHistorySeedsAllFortyFleetBatchCacheKeys(t *testing.T) {
	executor, actions, journal := newCarriedFleetBatchHistoryFixture(t, qualificationFleetBatchCount)
	if err := executor.verifyCarriedActionHistory(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(executor.carriedVerificationKeys) != qualificationFleetBatchCount {
		t.Fatalf("carried verification cache has %d entries, want %d", len(executor.carriedVerificationKeys), qualificationFleetBatchCount)
	}
	before := len(journal.Entries())
	for _, action := range actions {
		if err := executor.Execute(context.Background(), action); err != nil {
			t.Fatalf("carried batch %s entered execution: %v", action.ID, err)
		}
	}
	if got := len(journal.Entries()); got != before {
		t.Fatalf("carried batches appended %d journal entries, want zero", got-before)
	}
}

// Verifies a partial carry caches only authenticated ancestors and leaves the
// one incomplete batch at the intent/execution boundary rather than skipping it.
func TestVerifyCarriedActionHistoryLeavesOnlyUnverifiedFleetBatchAtExecutionBoundary(t *testing.T) {
	executor, actions, journal := newCarriedFleetBatchHistoryFixture(t, qualificationFleetBatchCount-1)
	if err := executor.verifyCarriedActionHistory(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(executor.carriedVerificationKeys) != qualificationFleetBatchCount-1 {
		t.Fatalf("partial carried verification cache has %d entries, want %d", len(executor.carriedVerificationKeys), qualificationFleetBatchCount-1)
	}
	before := len(journal.Entries())
	for _, action := range actions[:qualificationFleetBatchCount-1] {
		if err := executor.Execute(context.Background(), action); err != nil {
			t.Fatalf("carried batch %s entered execution: %v", action.ID, err)
		}
	}
	if got := len(journal.Entries()); got != before {
		t.Fatalf("carried subset appended %d journal entries, want zero", got-before)
	}
	pending := actions[qualificationFleetBatchCount-1]
	if err := executor.Execute(context.Background(), pending); err == nil || !strings.Contains(err.Error(), "no executor") {
		t.Fatalf("unverified batch did not reach the execution boundary: %v", err)
	}
	after := journal.Entries()
	if len(after) != before+2 || after[before].Stage != StageIntent || after[before+1].Stage != StageFailed || after[before].PlanHash != executor.plan.PlanHash || after[before].ActionID != pending.ID {
		t.Fatalf("unverified batch journal transition=%+v", after[before:])
	}
}
