// Recovery publication must survive interruption without claiming that a
// partially written directory is a completed historical run.
package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Retain a genuine signed predecessor and introduce an authorized successor
// without ever creating the predecessor's scenario directory.
func absentCampaignRecoveryFixture(t *testing.T) (*campaignSuccessionFixture, *scenarioCampaignAttempt) {
	t.Helper()
	fixture := newCampaignSuccessionFixture(t)
	bindCampaignRecoveryFixture(t, fixture)
	prior, err := loadOrCreateScenarioCampaignAttempt(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, "release-1.0", nil, fixture.now.Add(time.Hour), fixture.journal)
	if err != nil {
		t.Fatal(err)
	}
	next := *fixture.current
	next.PriorPlanHashes = append([]string{fixture.current.PlanHash}, fixture.current.PriorPlanHashes...)
	fixture.current = &next
	fixture.writeCurrent(t)
	fixture.cfg.provisionalResume = &provisionalResumeState{Record: &provisionalResumeRecord{PlanHash: fixture.current.PlanHash, Provisional: true, Command: "scenario", Scenario: "release-candidate"}}
	if _, err := os.Lstat(filepath.Join(fixture.stateDir, "runs", prior.payload.RunID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("fixture must stop before the predecessor creates its scenario directory", err)
	}
	return fixture, prior
}

// A deterministic gate read failure formerly left observations in the target
// directory, making the next invocation reject its own partial recovery.
func TestCampaignRecoveryMissingGateRetryPreservesAbsentRun(t *testing.T) {
	t.Parallel()
	fixture, prior := absentCampaignRecoveryFixture(t)
	gatePath := filepath.Join(fixture.stateDir, "supervisor.state.json")
	gate, err := os.ReadFile(gatePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(gatePath); err != nil {
		t.Fatal(err)
	}
	runDir := filepath.Join(fixture.stateDir, "runs", prior.payload.RunID)
	if _, err := createScenarioCampaignRecovery(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, fixture.now.Add(2*time.Hour), fixture.journal); err == nil || !strings.Contains(err.Error(), "process-log gate") {
		t.Fatal("missing live gate did not stop before publication", err)
	}
	if _, err := os.Lstat(runDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed materialization stranded a partial predecessor directory", err)
	}
	if err := os.WriteFile(gatePath, gate, 0o644); err != nil {
		t.Fatal(err)
	}
	next, err := createScenarioCampaignRecovery(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, fixture.now.Add(2*time.Hour), fixture.journal)
	if err != nil || next.payload.Recovery == nil || next.payload.Recovery.PriorRunID != prior.payload.RunID {
		t.Fatal("repaired gate could not finish the exact interrupted predecessor", err)
	}
	result, _, err := readScenarioCampaignRecoveryResult(fixture.cfg, fixture.stateDir, prior)
	if err != nil || result.Result != "fail" || !result.Provisional || result.FinalAcceptance == nil || *result.FinalAcceptance {
		t.Fatal("recovery retry lost the terminal non-accepting result", err)
	}
}

// Force an error after a partial file and again after result.json was written.
// Neither failure may claim the destination; the next complete writer wins.
func TestCampaignRecoveryPublicationRetriesPartialWriter(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	runDir := filepath.Join(root, "synthetic-run")
	failure := errors.New("synthetic interrupted recovery write")
	for _, resultWritten := range []bool{false, true} {
		err := publishAbsentCampaignRecoveryRun(runDir, func(staging string) error {
			if err := atomicWrite(filepath.Join(staging, "observations.jsonl"), []byte(preAcceptanceInterruptedObservationMarker), 0o600); err != nil {
				return err
			}
			if resultWritten {
				if err := atomicWrite(filepath.Join(staging, "result.json"), []byte("{}\n"), 0o644); err != nil {
					return err
				}
			}
			return failure
		})
		if !errors.Is(err, failure) {
			t.Fatal("publication concealed the interrupted writer", err)
		}
		entries, err := os.ReadDir(root)
		if err != nil || len(entries) != 0 {
			t.Fatal("partial publication changed the authoritative namespace or leaked staging", err)
		}
	}
	raw := []byte("{\"synthetic_complete_output\":true}\n")
	if err := publishAbsentCampaignRecoveryRun(runDir, func(staging string) error {
		return atomicWrite(filepath.Join(staging, "result.json"), raw, 0o644)
	}); err != nil {
		t.Fatal("complete retry could not publish", err)
	}
	actual, err := os.ReadFile(filepath.Join(runDir, "result.json"))
	if err != nil || !bytes.Equal(actual, raw) {
		t.Fatal("complete retry changed output bytes", err)
	}
}

// The publisher cannot reinterpret existing empty or partial evidence as an
// absent run, nor follow an alias to replace another owner's files.
func TestCampaignRecoveryPublicationPreservesExistingNamespace(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"empty-directory", "partial-directory", "file", "symlink"} {
		root := t.TempDir()
		runDir := filepath.Join(root, "synthetic-run")
		if kind == "file" {
			if err := os.WriteFile(runDir, nil, 0o600); err != nil {
				t.Fatal(err)
			}
		} else if kind == "symlink" {
			if err := os.Symlink(filepath.Join(root, "absent-target"), runDir); err != nil {
				t.Fatal(err)
			}
		} else {
			if err := os.Mkdir(runDir, 0o700); err != nil {
				t.Fatal(err)
			}
			if kind == "partial-directory" {
				if err := os.WriteFile(filepath.Join(runDir, "result.json"), nil, 0o644); err != nil {
					t.Fatal(err)
				}
			}
		}
		before, err := os.Lstat(runDir)
		if err != nil {
			t.Fatal(err)
		}
		called := false
		err = publishAbsentCampaignRecoveryRun(runDir, func(string) error { called = true; return nil })
		if err == nil || called {
			t.Fatalf("%s namespace was admitted for replacement: %v", kind, err)
		}
		after, err := os.Lstat(runDir)
		if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || before.Size() != after.Size() {
			t.Fatalf("%s rejection altered existing evidence: %v", kind, err)
		}
	}
}

// A nil writer result is insufficient: the expected regular, nonempty final
// file must actually exist before the directory can become authoritative.
func TestCampaignRecoveryPublicationRejectsIncompleteResults(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"missing", "empty", "directory", "symlink"} {
		root := t.TempDir()
		runDir := filepath.Join(root, "synthetic-run")
		err := publishAbsentCampaignRecoveryRun(runDir, func(staging string) error {
			path := filepath.Join(staging, "result.json")
			switch kind {
			case "empty":
				return os.WriteFile(path, nil, 0o644)
			case "directory":
				return os.Mkdir(path, 0o700)
			case "symlink":
				return os.Symlink(filepath.Join(staging, "synthetic-absent"), path)
			}
			return nil
		})
		if err == nil || !strings.Contains(err.Error(), "complete regular result") {
			t.Fatalf("%s result was admitted for publication: %v", kind, err)
		}
		entries, err := os.ReadDir(root)
		if err != nil || len(entries) != 0 {
			t.Fatalf("%s rejection stranded partial evidence: %v", kind, err)
		}
	}
}

// An explicit callback barrier creates a competing destination after the
// first absence check. Publication must retain that directory byte-for-byte.
func TestCampaignRecoveryPublicationRefusesLateDestination(t *testing.T) {
	t.Parallel()
	runDir := filepath.Join(t.TempDir(), "synthetic-run")
	marker := []byte("synthetic competing owner\n")
	err := publishAbsentCampaignRecoveryRun(runDir, func(staging string) error {
		if err := atomicWrite(filepath.Join(runDir, "owner.txt"), marker, 0o600); err != nil {
			return err
		}
		return atomicWrite(filepath.Join(staging, "result.json"), []byte("{}\n"), 0o644)
	})
	if err == nil || !strings.Contains(err.Error(), "destination appeared") {
		t.Fatal("late destination was overwritten", err)
	}
	actual, err := os.ReadFile(filepath.Join(runDir, "owner.txt"))
	if err != nil || !bytes.Equal(actual, marker) {
		t.Fatal("late destination lost its existing owner", err)
	}
	if _, err := os.Lstat(filepath.Join(runDir, "result.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("staged result crossed into the late destination", err)
	}
}
