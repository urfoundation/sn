package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// The active predecessor differs from the reviewed successor. No RPC reader
// exists: loading must select the exact archived approval after a driver
// hotfix and later head observations, without rebuilding a new plan.
func TestReviewedProvisionalSetupUsesArchivedSnapshotAfterHeadDrift(t *testing.T) {
	cfg, prior, stateDir, options, activeBefore := provisionalRuntimePlanFixture(t)
	reviewed := *prior
	reviewed.PriorPlanHashes = []string{prior.PlanHash}
	var err error
	reviewed.PlanHash, err = reviewed.hash()
	if err != nil {
		t.Fatal(err)
	}
	if reviewed.PlanHash == prior.PlanHash {
		t.Fatal("review fixture did not create a distinct successor approval")
	}
	archived, err := archiveReviewedSetupPlan(stateDir, &reviewed)
	if err != nil {
		t.Fatal(err)
	}
	options.PlanHash = archived.PlanHash
	archivePath := filepath.Join(stateDir, "plans", stringsTrim0x(archived.PlanHash)+".json")
	bytesBefore, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	// Both observation heights are intentionally outside the economic hash,
	// but the first archived observation remains the exact reviewed snapshot.
	reviewed.GeneratedAt = time.Unix(2, 0).UTC().Format(time.RFC3339)
	reviewed.LiveFacts.FinalizedBlock += 100
	reviewed.LiveFacts.EVMFinalizedBlock += 100
	if hash, err := reviewed.hash(); err != nil || hash != options.PlanHash {
		t.Fatal("head-only drift changed economic approval", err)
	}
	again, err := archiveReviewedSetupPlan(stateDir, &reviewed)
	if err != nil || !reflect.DeepEqual(again.LiveFacts, archived.LiveFacts) || again.GeneratedAt != archived.GeneratedAt {
		t.Fatal("later planning replaced the first reviewed snapshot", err)
	}
	loaded, err := loadInvocationPlan(cfg, stateDir, "setup", options)
	if err != nil || loaded.PlanHash != options.PlanHash || !reflect.DeepEqual(loaded.LiveFacts, archived.LiveFacts) || loaded.ReleaseLockHash != prior.ReleaseLockHash {
		t.Fatal("provisional setup regenerated the active predecessor or lost reviewed facts", err)
	}
	activeAfter, err := os.ReadFile(filepath.Join(stateDir, "plan.json"))
	if err != nil || !bytes.Equal(activeBefore, activeAfter) {
		t.Fatal("review replaced the active approval", err)
	}
	bytesAfter, err := os.ReadFile(archivePath)
	if err != nil || !bytes.Equal(bytesBefore, bytesAfter) {
		t.Fatal("reviewed bytes changed after head drift", err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "journal.jsonl")); !os.IsNotExist(err) {
		t.Fatal("read-only review opened the deployment journal", err)
	}
	changed := *cfg
	changed.MaximumAlphaRao++
	if _, err := loadInvocationPlan(&changed, stateDir, "setup", options); err == nil {
		t.Fatal("archived approval bypassed the current allowance")
	}
	for _, fault := range []string{"missing", "mismatched-hash", "malformed"} {
		switch fault {
		case "missing":
			if err := os.Remove(archivePath); err != nil {
				t.Fatal(err)
			}
		case "mismatched-hash":
			if err := atomicWrite(archivePath, activeBefore, 0o600); err != nil {
				t.Fatal(err)
			}
		case "malformed":
			if err := atomicWrite(archivePath, []byte("{}"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := loadInvocationPlan(cfg, stateDir, "setup", options); err == nil {
			t.Fatal("provisional setup rebuilt or accepted an invalid review archive", fault)
		}
		if err := atomicWrite(archivePath, bytesBefore, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestReviewedSetupRetirementRejectsTransactionEvidenceAddedAfterReview(t *testing.T) {
	f := reserveRepairPreSignFixture(t)
	reviewed := f.revise(t)
	if err := validateReviewedSetupRepairRetirements(f.prior, reviewed, f.entries); err != nil {
		t.Fatal(err)
	}
	entries := append([]JournalEntry(nil), f.entries...)
	later := entries[len(entries)-1]
	later.Stage = StageBroadcast
	later.TransactionHash = "synthetic-post-review-transaction"
	entries = append(entries, later)
	if err := validateReviewedSetupRepairRetirements(f.prior, reviewed, entries); err == nil {
		t.Fatal("archived review discarded an action signed after review")
	}
}
