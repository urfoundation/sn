package main

import (
	"os"
	"testing"
)

func TestStatusJournalSummaryCachesStableJournalAndInvalidatesAppend(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	journal, err := OpenJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = journal.Close() })
	first := JournalEntry{DeploymentID: "test", PlanHash: "0xplan", ActionID: "first", IntentHash: "0xintent", Stage: StageIntent}
	if err := journal.Append(first); err != nil {
		t.Fatal(err)
	}
	path := journal.path
	initial, err := statusJournalSummary(path)
	if err != nil || initial.Entries != 1 || initial.LastHash == "" {
		t.Fatalf("initial summary=(%+v,%v)", initial, err)
	}
	// Callers receive detached maps, so they cannot poison the cached status.
	initial.LatestByStage[string(StageIntent)] = 0
	cached, err := statusJournalSummary(path)
	if err != nil || cached.Entries != 1 || cached.LatestByStage[string(StageIntent)] != 1 {
		t.Fatalf("cached summary=(%+v,%v)", cached, err)
	}
	if err := journal.Append(JournalEntry{DeploymentID: "test", PlanHash: "0xplan", ActionID: "second", IntentHash: "0xintent-2", Stage: StageIntent}); err != nil {
		t.Fatal(err)
	}
	updated, err := statusJournalSummary(path)
	if err != nil || updated.Entries != 2 || updated.LatestByStage[string(StageIntent)] != 2 {
		t.Fatalf("updated summary=(%+v,%v)", updated, err)
	}
}
