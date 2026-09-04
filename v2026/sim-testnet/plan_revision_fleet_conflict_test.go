package main

// This file locks the fail-closed choice of one verified historical fleet
// batch source when a revision lineage contains competing generations.

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Writes one hash-authenticated source plan used only by revision history.
func writeFleetBatchConflictSourcePlan(t *testing.T, stateDir string, plan *SetupPlan) {
	t.Helper()
	hash, err := plan.hash()
	if err != nil {
		t.Fatal(err)
	}
	plan.PlanHash = hash
	wire, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(stateDir, "plans", stringsTrim0x(plan.PlanHash)+".json")
	if err := atomicWrite(path, append(wire, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readPersistedPlanFile(path); err != nil {
		t.Fatal(err)
	}
}

// Clones a plan before giving one historical generation a distinct immutable
// release identity or batch transaction intent.
func cloneFleetBatchConflictPlan(t *testing.T, source *SetupPlan) *SetupPlan {
	t.Helper()
	wire, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	var result SetupPlan
	if err := json.Unmarshal(wire, &result); err != nil {
		t.Fatal(err)
	}
	return &result
}

// Ensures an earlier conflicting verified source cannot be hidden merely by
// appending a compatible source after it in the durable journal history.
func TestPreserveVerifiedFleetBatchActionsRejectsConflictingVerifiedSourceGenerations(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	facts := *testSetupFacts()
	prior, err := buildPlan(cfg, &facts, roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	stateDir := t.TempDir()
	compatible := cloneFleetBatchConflictPlan(t, prior)
	compatible.PriorPlanHashes = nil
	compatible.ReleaseLockHash = "0x" + strings.Repeat("a1", 32)
	writeFleetBatchConflictSourcePlan(t, stateDir, compatible)
	conflicting := cloneFleetBatchConflictPlan(t, prior)
	conflicting.PriorPlanHashes = nil
	conflicting.ReleaseLockHash = "0x" + strings.Repeat("b2", 32)
	conflictingAction := actionByID(t, conflicting, "fleet.refresh.batch.1")
	for index := range conflicting.Actions {
		if conflicting.Actions[index].ID != conflictingAction.ID {
			continue
		}
		conflicting.Actions[index].Description += " conflicting historical generation"
		intentHash, err := actionIntentHash(conflicting.Actions[index])
		if err != nil {
			t.Fatal(err)
		}
		conflicting.Actions[index].IntentHash = intentHash
		conflictingAction = conflicting.Actions[index]
	}
	writeFleetBatchConflictSourcePlan(t, stateDir, conflicting)
	prior.PriorPlanHashes = []string{conflicting.PlanHash, compatible.PlanHash}
	prior.PlanHash, err = prior.hash()
	if err != nil {
		t.Fatal(err)
	}
	revised, err := buildPlan(cfg, &facts, roles, time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	compatibleAction := actionByID(t, compatible, "fleet.refresh.batch.1")
	entries := []JournalEntry{
		{PlanHash: conflicting.PlanHash, ActionID: conflictingAction.ID, IntentHash: conflictingAction.IntentHash, Stage: StageVerified},
		{PlanHash: compatible.PlanHash, ActionID: compatibleAction.ID, IntentHash: compatibleAction.IntentHash, Stage: StageVerified},
	}
	if err := preserveVerifiedFleetBatchActions(cfg, stateDir, revised, prior, entries); err == nil || !strings.Contains(err.Error(), "conflicting source generations") {
		t.Fatalf("conflicting verified fleet batch generations were accepted: %v", err)
	}
}
