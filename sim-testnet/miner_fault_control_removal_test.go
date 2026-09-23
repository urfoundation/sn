// Force failure after successful unlink, then reopen the driver so no in-memory
// completion can stand in for durable intent or current live reconciliation.
package main

import (
	"errors"
	"os"
	"syscall"
	"testing"
)

func TestMinerControlRemovalSyncFailureReconcilesRestoredCensusAfterRestart(t *testing.T) {
	for _, changed := range []bool{false, true} {
		fixture := newMinerControlTestFixture(t, "miner-1", "miner-2")
		fixture.apply(t)
		fixture.driver.minerControlRemove = func(path string, active activeFaultFile, index int) error {
			if err := removeActiveFault(path, active, index); err != nil {
				return err
			}
			return &os.PathError{Op: "sync", Path: path, Err: syscall.EIO}
		}
		if _, err := fixture.driver.Restore(t.Context(), fixture.fault); !minerControlPending(err) || !errors.Is(err, syscall.EIO) {
			t.Fatalf("post-unlink persistence failure became terminal: %v", err)
		}
		if _, err := os.Stat(fixture.driver.activePath()); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("fixture missed the post-unlink crash: %v", err)
		}
		if changed {
			fixture.stateLock.Lock()
			fixture.states["miner-1"] = "disabled"
			fixture.stateLock.Unlock()
		}
		reopened := *fixture.driver
		reopened.minerControlRemove, reopened.minerControlReconciled = nil, nil
		if _, err := reopened.Restore(t.Context(), fixture.fault); err != nil {
			t.Fatalf("exact restored intent did not resume changed=%t: %v", changed, err)
		}
		want := []string{"/control/miner-1/enable", "/control/miner-2/enable"}
		if changed {
			want = append(want, "/control/miner-1/enable")
		}
		fixture.requirePosts(t, want...)
		if _, err := os.Stat(fixture.driver.activePath()); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("successful cleanup left active intent: %v", err)
		}
	}
}

func TestMinerControlRemovalRejectsAlteredOrIncompleteCheckpoint(t *testing.T) {
	for _, fault := range []string{"schedule", "partial", "identity", "absent"} {
		fixture := newMinerControlTestFixture(t, "miner-1")
		fixture.apply(t)
		if _, err := fixture.driver.Restore(t.Context(), fixture.fault); err != nil {
			t.Fatal(err)
		}
		path, err := fixture.driver.minerControlRemovalPath(fixture.fault)
		if err != nil {
			t.Fatal(err)
		}
		checkpoint, err := readActiveFaultFile(path)
		if err != nil {
			t.Fatal(err)
		}
		switch fault {
		case "schedule":
			checkpoint.Faults[0].DurationBlocks++
		case "partial":
			checkpoint.MinerControls[0].Completed = nil
			checkpoint.MinerControls[0].CompletedCount = 0
		case "identity":
			checkpoint.Processes[0].Identity = "different-owner"
		}
		if fault == "absent" {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		} else if err := writePublicJSON(path, checkpoint); err != nil {
			t.Fatal(err)
		}
		reopened := *fixture.driver
		reopened.minerControlReconciled = nil
		if _, err := reopened.Restore(t.Context(), fixture.fault); err == nil || minerControlPending(err) {
			t.Fatalf("invalid %s checkpoint acquired retry authority: %v", fault, err)
		}
		fixture.requirePosts(t, "/control/miner-1/enable")
	}
}
