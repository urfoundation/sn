// An abruptly interrupted owner has no terminal result. Its replacement must
// retain the signed recovery identity and recover faults before rearming them.
package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Reopen a real signed recovery with retained preparation and observations,
// using a new journal owner, driver identity and process session.
func TestScenarioCampaignPreAcceptanceOwnerReplacementKeepsRecoveryIdentity(t *testing.T) {
	t.Parallel()
	f := newCampaignSuccessionFixture(t)
	_, prior, _ := createSecondCampaignRecovery(t, f)
	if err := prior.updateProgress(false, true); err != nil {
		t.Fatal(err)
	}
	runDir := filepath.Join(f.stateDir, "runs", prior.payload.RunID)
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	observationPath := filepath.Join(runDir, "observations.jsonl")
	if err := appendObservation(observationPath, testScenarioObservation(f.cfg, 12)); err != nil {
		t.Fatal(err)
	}
	before := campaignProcessRecoveryBytes(t, prior.path(), observationPath)
	files, err := scenarioCampaignRecoveryFiles(f.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.journal.Close(); err != nil {
		t.Fatal(err)
	}
	f.journal, err = OpenJournal(f.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.journal.Close() })
	currentCfg, resume := *f.cfg, *f.cfg.provisionalResume
	resume.Driver.ExecutableSHA256 = "sha256:" + strings.Repeat("b7", 32)
	currentCfg.provisionalResume = &resume
	reopened, err := readScenarioCampaignAttempt(&currentCfg, f.stateDir, f.roles, f.current.PlanHash, "release-1.0")
	if err != nil || scenarioCampaignAttemptNeedsRecovery(reopened) {
		t.Fatalf("pre-acceptance replacement required a new recovery: %v", err)
	}
	reopened, err = loadOrCreateScenarioCampaignAttempt(&currentCfg, f.stateDir, f.roles, f.current.PlanHash, "release-1.0", nil, f.now.Add(3*time.Hour), f.journal)
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := recoverScenarioCampaignProcessSession(t.Context(), reopened, f.journal, "0x"+strings.Repeat("d8", 32), f.now.Add(3*time.Hour))
	if err != nil || resumed.payload.RunID != prior.payload.RunID || resumed.payload.Recovery.Generation != prior.payload.Recovery.Generation || !resumed.payload.PreparationComplete || resumed.payload.AcceptanceBoundary != nil {
		t.Fatalf("replacement changed the prepared recovery identity: %v", err)
	}
	prepared, armed := 0, 0
	if err := beginScenarioCampaignPreparation(t.Context(), "release-1.0", resumed.payload.RunID, scenarioRunOptions{
		Attempt:              resumed,
		Prepare:              func(context.Context) error { prepared++; return nil },
		BeforeFleetLifecycle: func(context.Context) error { armed++; return nil },
	}); err != nil || prepared != 0 || armed != 1 {
		t.Fatalf("replacement repeated preparation or lost fault rearming: prepared=%d armed=%d err=%v", prepared, armed, err)
	}
	currentFiles, err := scenarioCampaignRecoveryFiles(f.stateDir)
	if err != nil || len(currentFiles) != len(files) {
		t.Fatal("replacement created a successor recovery", err)
	}
	if _, err := os.Lstat(filepath.Join(runDir, "result.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("replacement sealed the unfinished interval", err)
	}
	requireCampaignProcessRecoveryBytes(t, before)
}
