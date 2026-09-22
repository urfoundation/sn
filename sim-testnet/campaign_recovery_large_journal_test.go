package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func growCampaignRecoveryJournal(t *testing.T, fixture *campaignSuccessionFixture) {
	t.Helper()
	for index := 0; index < 33; index++ {
		if err := fixture.journal.Append(JournalEntry{
			DeploymentID: fixture.current.DeploymentID, PlanHash: fixture.current.PlanHash,
			ActionID: fmt.Sprintf("retained-growth-%d", index), IntentHash: "synthetic-growth-intent",
			Stage: StageFailed, Error: strings.Repeat("x", 1024*1024),
		}); err != nil {
			t.Fatal(err)
		}
	}
	info, err := os.Stat(filepath.Join(fixture.stateDir, "journal.jsonl"))
	if err != nil || info.Size() <= maximumCampaignEvidenceRawFileBytes {
		t.Fatalf("journal fixture did not exceed the raw artifact limit: %v %v", info, err)
	}
}

// Reproduce the complete historical generation-two path and the next failed
// pre-acceptance generation. Neither may depend on the current raw-file size.
func TestScenarioCampaignRecoveryLargeJournalSinglePhaseEntry(t *testing.T) {
	fixture, _, second, _ := newCampaignRecoveryCacheFixture(t)
	original, err := os.ReadFile(second.path())
	if err != nil {
		t.Fatal(err)
	}
	growCampaignRecoveryJournal(t, fixture)
	reopened, err := readScenarioCampaignAttempt(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, "release-1.0")
	if err != nil || reopened.payload.RunID != second.payload.RunID || !reflect.DeepEqual(reopened.payload.Recovery, second.payload.Recovery) {
		t.Fatalf("large suffix prevented exact generation-two recovery: %v", err)
	}
	bindPreAcceptanceFailedRecoveryGeneration(t, fixture, second)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	runErr := runScenarioCampaignAttemptWithTimeout(ctx, fixture.cfg, fixture.stateDir, "release-1.0", fixture.journal, &Executor{plan: fixture.current}, nil, 0)
	latest, err := readScenarioCampaignAttempt(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, "release-1.0")
	if runErr == nil || err != nil {
		t.Fatalf("single-phase recovery failed before the cancelled runtime boundary: run=%v read=%v", runErr, err)
	}
	if latest.payload.Recovery.Generation != 3 || latest.payload.Recovery.PriorRunID != second.payload.RunID || latest.payload.Recovery.PriorJournalBytes <= maximumCampaignEvidenceRawFileBytes || latest.payload.AcceptanceBoundary != nil || latest.payload.PreparationComplete {
		t.Fatalf("new recovery lost its complete journal cut or claimed acceptance: %+v", latest.payload)
	}
	after, err := os.ReadFile(second.path())
	if err != nil || !bytes.Equal(original, after) {
		t.Fatalf("recovery rewrote its signed predecessor: %v", err)
	}
	strict := *fixture.cfg
	strict.provisionalResume = nil
	if !releaseStartupGatesRequired(&strict, latest) {
		t.Fatal("journal growth waived strict startup acceptance")
	}
}

// Both cold and warm proof paths authenticate the stream, retaining only its
// commitment. Appends preserve immutable ancestry; malformed tails still fail.
func TestScenarioCampaignRecoveryLargeJournalCache(t *testing.T) {
	fixture, root, attempt, _ := newCampaignRecoveryCacheFixture(t)
	growCampaignRecoveryJournal(t, fixture)
	calls := 0
	validate := func(candidate *scenarioCampaignAttempt) error {
		calls++
		return validateScenarioCampaignRecovery(candidate)
	}
	for index := 0; index < 2; index++ {
		ancestors, err := scenarioCampaignRecoveryAncestors(attempt, validate)
		if err != nil || !ancestors[root.payload.RunID] || !ancestors[attempt.payload.Recovery.PriorRunID] {
			t.Fatalf("large journal lost its authenticated ancestry: %v", err)
		}
	}
	if calls != 1 {
		t.Fatalf("large journal disabled ancestry caching: validations=%d", calls)
	}
	if err := fixture.journal.Append(JournalEntry{DeploymentID: fixture.current.DeploymentID, PlanHash: fixture.current.PlanHash,
		ActionID: "retained-growth-after-cache", IntentHash: "synthetic-growth-intent", Stage: StageIntent}); err != nil {
		t.Fatal(err)
	}
	if _, err := scenarioCampaignRecoveryAncestors(attempt, validate); err != nil || calls != 1 {
		t.Fatalf("valid suffix discarded an immutable proof: calls=%d error=%v", calls, err)
	}
	file, err := os.OpenFile(filepath.Join(fixture.stateDir, "journal.jsonl"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := file.WriteString("{}\n")
	if err := errors.Join(writeErr, file.Close()); err != nil {
		t.Fatal(err)
	}
	if _, err := scenarioCampaignRecoveryAncestors(attempt, validate); err == nil || !strings.Contains(err.Error(), "hash mismatch") || calls != 2 {
		t.Fatalf("large journal cache hid an invalid suffix: calls=%d error=%v", calls, err)
	}
}
