package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Reopening a process observation spends the same signed operation budget.
// Exhaustion never recreates a start or turns historical status into readiness.
func TestRootPassiveHostOperationAllowanceCannotRenewStart(t *testing.T) {
	f := newRootPassiveHostFixture(t)
	f.approval.Plan.MaximumOperations = 4
	f.sign()
	f.require("claim", "claimed")
	f.require("install", "installed")
	f.require("start", "acknowledged-running")
	f.require("resume", "acknowledged-running")
	f.require("resume", "acknowledged-running")
	for i := 0; i < 2; i++ {
		result, code, detail := f.command("resume")
		if code != 3 || result.Status != "operation-limit" || result.CurrentProcessRunning || !result.StartConsumed || f.starts != 1 {
			t.Fatal("operation exhaustion renewed process authority", result, code, detail)
		}
	}
	result := f.require("status", "operation-limit")
	if result.CurrentProcessRunning || result.Generation == nil || !result.StartConsumed || f.starts != 1 {
		t.Fatal("historical bounded result lost exact consumed invocation", result)
	}
}

// A changed directory census is refused before any process effect. Removing
// the foreign entry restores admission without replacing the original journal.
func TestRootPassiveHostChangedCheckpointEntryCannotAuthorizeStart(t *testing.T) {
	f := newRootPassiveHostFixture(t)
	f.require("claim", "claimed")
	f.require("install", "installed")
	foreign := filepath.Join(f.approval.Plan.CheckpointDirectory, "foreign.json")
	if err := os.WriteFile(foreign, []byte("synthetic unrelated retained state\n"), 0600); err != nil {
		t.Fatal(err)
	}
	result, code, detail := f.command("start")
	if code != 3 || result.StartConsumed || f.starts != 0 {
		t.Fatal("foreign checkpoint entry authorized start", result, code, detail)
	}
	if err := os.Remove(foreign); err != nil {
		t.Fatal(err)
	}
	f.require("start", "acknowledged-running")
	if f.starts != 1 {
		t.Fatal("checkpoint refusal changed the initial start allowance", f.starts)
	}
}
