package main

// Exercise the actual authenticated reader and append boundary. The historical
// comparison oracle below is the original full scan; work bounds count visited
// entries rather than depending on elapsed time or scheduler behavior.
import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Synthetic complete rows keep stage framing independent of history mutations.
func journalIndexTestEntry(stage JournalStage) JournalEntry {
	entry := JournalEntry{DeploymentID: "synthetic-deployment", PlanHash: "synthetic-plan", ActionID: "synthetic-action", IntentHash: "synthetic-intent", Stage: stage}
	switch stage {
	case StageBroadcast:
		entry.Signer, entry.Nonce, entry.TransactionHash = "synthetic-signer", "1", "synthetic-transaction"
		entry.RecoveryBlock, entry.RecoveryBlockHash = 11, "synthetic-recovery"
	case StageIncluded, StageFinalized:
		entry.TransactionHash, entry.BlockNumber, entry.BlockHash = "synthetic-transaction", 12, "synthetic-block"
	case StageVerified:
		entry.PostconditionHash = "synthetic-postcondition"
		entry.PostconditionPath = "receipts/postconditions/synthetic-action.json"
	}
	return entry
}

// Sign no transactions: only construct the existing local canonical hash chain.
func writeJournalIndexTestHistory(t *testing.T, entries []JournalEntry) (string, []JournalEntry) {
	t.Helper()
	stateDir := t.TempDir()
	file, err := os.OpenFile(filepath.Join(stateDir, "journal.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	writer := bufio.NewWriter(file)
	encoder := json.NewEncoder(writer)
	accepted := make([]JournalEntry, 0, len(entries))
	previous := ""
	for index, entry := range entries {
		entry.Schema, entry.Sequence, entry.Time = "urnetwork-sim-journal-v1", uint64(index+1), "2020-01-01T00:00:00Z"
		entry.PreviousHash, entry.EntryHash = previous, ""
		entry.EntryHash, err = canonicalHashHex(entry)
		if err != nil {
			t.Fatal(err)
		}
		if err := encoder.Encode(entry); err != nil {
			t.Fatal(err)
		}
		accepted = append(accepted, entry)
		previous = entry.EntryHash
	}
	if err := writer.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return stateDir, accepted
}

// A full-sized replay includes more actions than the launch-scale setup, plus
// one repeatedly retried action so indexing only the owner would be insufficient.
func TestJournalValidationIndexBoundsAuthenticatedReplay(t *testing.T) {
	entries := make([]JournalEntry, 0, 6000*7+2048)
	for action := 0; action < 6000; action++ {
		for step, stage := range []JournalStage{StageIntent, StageFailed, StageIntent, StageBroadcast, StageIncluded, StageFinalized, StageVerified} {
			entry := journalIndexTestEntry(stage)
			entry.ActionID = fmt.Sprintf("synthetic-action-%d", action)
			entry.PlanHash = fmt.Sprintf("synthetic-plan-%d", action%3)
			if step == 2 {
				entry.TransactionHash = "synthetic-transaction"
			}
			if stage == StageVerified {
				entry.PostconditionPath = "receipts/postconditions/" + entry.ActionID + ".json"
			}
			entries = append(entries, entry)
		}
	}
	for retry := 0; retry < 2048; retry++ {
		entry := journalIndexTestEntry(StageFailed)
		entry.ActionID = "synthetic-retry"
		entries = append(entries, entry)
	}
	stateDir, expected := writeJournalIndexTestHistory(t, entries)
	file, err := os.Open(filepath.Join(stateDir, "journal.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	visits := 0
	journal := &Journal{validationHistoryVisit: func() {
		visits++
		if visits > 4*len(entries) {
			t.Fatal("authenticated replay exceeded four historical comparisons per input row")
		}
	}}
	if err := journal.loadReader(file); err != nil {
		t.Fatal(err)
	}
	if visits == 0 || journal.validationCount != len(entries) || !reflect.DeepEqual(journal.Entries(), expected) || journal.lastHash != expected[len(expected)-1].EntryHash {
		t.Fatal("indexed replay lost its authenticated entries, chain, or observed work")
	}
	if len(journal.validationKVs) != 6001 {
		t.Fatalf("indexed owners=%d, want 6001", len(journal.validationKVs))
	}
	for owner, witnesses := range journal.validationKVs {
		if len(witnesses) == 0 || len(witnesses) > 4 {
			t.Fatalf("owner %+v retained %d witnesses", owner, len(witnesses))
		}
	}
}

// Loading enables the same bounded comparison path for durable appends, while
// all entries remain available to existing latest-stage and transaction readers.
func TestJournalValidationIndexBoundsAppendAfterReopen(t *testing.T) {
	entries := make([]JournalEntry, 2048)
	for index := range entries {
		entries[index] = journalIndexTestEntry(StageFailed)
	}
	stateDir, _ := writeJournalIndexTestHistory(t, entries)
	journal, err := OpenJournal(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	visits := 0
	journal.validationHistoryVisit = func() {
		visits++
		if visits > 4 {
			t.Fatal("one append scanned more than four prior witnesses")
		}
	}
	for _, stage := range []JournalStage{StageBroadcast, StageIntent, StageBroadcast, StageIncluded, StageFinalized, StageVerified} {
		visits = 0
		if err := journal.Append(journalIndexTestEntry(stage)); err != nil {
			t.Fatal(err)
		}
	}
	all := journal.Entries()
	if last, found := journal.LastStage("synthetic-action", "synthetic-intent", "synthetic-plan"); !found || last != all[len(all)-1] {
		t.Fatal("indexed append changed the latest-stage view")
	}
	if transaction, found := journal.LatestTransaction("synthetic-plan", "synthetic-action", "synthetic-intent"); !found || transaction.Stage != StageFinalized {
		t.Fatal("indexed append changed the latest-transaction view")
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := readJournalEntries(stateDir)
	if err != nil || !reflect.DeepEqual(reopened, all) {
		t.Fatalf("read-only replay changed accepted history: %v", err)
	}
}

// Retain the exact first-conflict ordering of the original history loop as an
// independent compatibility oracle, including multiple simultaneous violations.
func journalIndexOriginalHistoryError(entries []JournalEntry, entry JournalEntry) string {
	for _, prior := range entries {
		if prior.PlanHash == entry.PlanHash && prior.ActionID == entry.ActionID && prior.IntentHash != entry.IntentHash {
			return "one planned action cannot use multiple intent hashes"
		}
		if prior.PlanHash == entry.PlanHash && prior.ActionID == entry.ActionID && prior.IntentHash == entry.IntentHash && prior.Stage == StageVerified {
			return "postcondition verification is terminal for one action intent"
		}
		if prior.PlanHash == entry.PlanHash && prior.ActionID == entry.ActionID && prior.IntentHash == entry.IntentHash && prior.TransactionHash != "" && entry.TransactionHash != "" && prior.TransactionHash != entry.TransactionHash {
			return "one action intent cannot use multiple transactions"
		}
		if prior.PlanHash == entry.PlanHash && prior.ActionID == entry.ActionID && prior.IntentHash == entry.IntentHash && prior.Stage == StageBroadcast && entry.Stage == StageBroadcast && (prior.Signer != entry.Signer || prior.Nonce != entry.Nonce || prior.RecoveryBlock != entry.RecoveryBlock || prior.RecoveryBlockHash != entry.RecoveryBlockHash) {
			return "replayed broadcast metadata does not match original intent"
		}
	}
	return ""
}

// Transaction identity may precede the first broadcast or occur on the terminal
// row itself. Distinct plan/action tuples must remain independent in all cases.
func TestJournalValidationIndexPreservesFirstConflict(t *testing.T) {
	intent, broadcast := journalIndexTestEntry(StageIntent), journalIndexTestEntry(StageBroadcast)
	transaction, verified := intent, journalIndexTestEntry(StageVerified)
	transaction.TransactionHash = broadcast.TransactionHash
	terminalTransaction := verified
	terminalTransaction.TransactionHash = broadcast.TransactionHash
	otherPlan, otherAction := broadcast, broadcast
	otherPlan.PlanHash, otherPlan.TransactionHash = "other-plan", "other-plan-transaction"
	otherAction.ActionID, otherAction.TransactionHash = "other-action", "other-action-transaction"
	for historyIndex, history := range [][]JournalEntry{
		{intent, transaction, broadcast, verified},
		{intent, broadcast, transaction, verified},
		{intent, terminalTransaction},
		{terminalTransaction},
		{otherPlan, intent, otherAction, broadcast, verified},
	} {
		for count := 0; count <= len(history); count++ {
			stateDir, expected := writeJournalIndexTestHistory(t, history[:count])
			journal, err := OpenJournal(stateDir)
			if err != nil {
				t.Fatal(err)
			}
			for variation := 0; variation < 9; variation++ {
				candidate := broadcast
				switch variation {
				case 1:
					candidate.IntentHash, candidate.TransactionHash, candidate.Signer = "other-intent", "other-transaction", "other-signer"
				case 2:
					candidate.TransactionHash, candidate.Signer = "other-transaction", "other-signer"
				case 3:
					candidate.Signer = "other-signer"
				case 4:
					candidate.Nonce = "2"
				case 5:
					candidate.RecoveryBlock++
				case 6:
					candidate.RecoveryBlockHash = "other-recovery"
				case 7:
					candidate.PlanHash = "unused-plan"
				case 8:
					candidate.ActionID = "unused-action"
				}
				want := journalIndexOriginalHistoryError(expected, candidate)
				got := ""
				if err := journal.validateEntry(candidate); err != nil {
					got = err.Error()
				}
				if got != want {
					t.Fatalf("history=%d prefix=%d variation=%d: got %q want %q", historyIndex, count, variation, got, want)
				}
			}
			if err := journal.Close(); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// Semantic rejection happens after authenticating a row's hash and chain, but
// that rejected row must never become a witness for a later valid continuation.
func TestJournalValidationIndexRejectedLoadKeepsAcceptedPrefix(t *testing.T) {
	for _, change := range []struct {
		name   string
		mutate func(*JournalEntry)
		want   string
	}{
		{name: "identity", mutate: func(e *JournalEntry) { e.IntentHash = "" }, want: "journal entry identity is incomplete"},
		{name: "deployment", mutate: func(e *JournalEntry) { e.DeploymentID = "other-deployment" }, want: "does not match"},
		{name: "stage", mutate: func(e *JournalEntry) { e.Stage = "unknown" }, want: "unknown journal stage"},
		{name: "signer", mutate: func(e *JournalEntry) { e.Signer = "" }, want: "broadcast entry is incomplete"},
		{name: "nonce", mutate: func(e *JournalEntry) { e.Nonce = "" }, want: "broadcast entry is incomplete"},
		{name: "transaction", mutate: func(e *JournalEntry) { e.TransactionHash = "" }, want: "broadcast entry is incomplete"},
		{name: "recovery height", mutate: func(e *JournalEntry) { e.RecoveryBlock = 0 }, want: "no finalized recovery checkpoint"},
		{name: "recovery hash", mutate: func(e *JournalEntry) { e.RecoveryBlockHash = "" }, want: "no finalized recovery checkpoint"},
		{name: "fee", mutate: func(e *JournalEntry) { e.FeeEstimateRao, e.FeeLimitRao = 2, 1 }, want: "exceeds its approved limit"},
		{name: "intent", mutate: func(e *JournalEntry) { e.IntentHash = "other-intent" }, want: "multiple intent hashes"},
		{name: "included", mutate: func(e *JournalEntry) { e.Stage = StageIncluded }, want: "included entry is incomplete"},
		{name: "finalized", mutate: func(e *JournalEntry) { e.Stage = StageFinalized }, want: "finalized entry is incomplete"},
		{name: "verification", mutate: func(e *JournalEntry) { *e = journalIndexTestEntry(StageVerified); e.PostconditionHash = "" }, want: "no postcondition hash/path"},
		{name: "verification path", mutate: func(e *JournalEntry) {
			*e = journalIndexTestEntry(StageVerified)
			e.PostconditionPath = "../other.json"
		}, want: "noncanonical postcondition path"},
	} {
		bad := journalIndexTestEntry(StageBroadcast)
		change.mutate(&bad)
		stateDir, encoded := writeJournalIndexTestHistory(t, []JournalEntry{journalIndexTestEntry(StageIntent), bad})
		file, err := os.Open(filepath.Join(stateDir, "journal.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		journal := &Journal{}
		loadErr := journal.loadReader(file)
		closeErr := file.Close()
		if closeErr != nil || loadErr == nil || !strings.Contains(loadErr.Error(), change.want) {
			t.Fatalf("%s: load=%v close=%v", change.name, loadErr, closeErr)
		}
		if journal.validationCount != 1 || len(journal.validationKVs) != 1 || !reflect.DeepEqual(journal.Entries(), encoded[:1]) || journal.lastHash != encoded[0].EntryHash {
			t.Fatalf("%s: rejected row changed accepted history", change.name)
		}
		candidate := journalIndexTestEntry(StageBroadcast)
		candidate.TransactionHash = "valid-replacement-after-rejection"
		if err := journal.validateEntry(candidate); err != nil {
			t.Fatalf("%s: rejected row poisoned valid continuation: %v", change.name, err)
		}
	}
}

// Hash and chain checks still precede semantic admission and index publication.
func TestJournalValidationIndexRejectsHashAndChainBeforeCommit(t *testing.T) {
	for _, failure := range []string{"hash", "chain"} {
		stateDir, encoded := writeJournalIndexTestHistory(t, []JournalEntry{journalIndexTestEntry(StageIntent), journalIndexTestEntry(StageBroadcast)})
		bad := encoded[1]
		bad.EntryHash = "forged-entry-hash"
		if failure == "chain" {
			bad.PreviousHash, bad.EntryHash = "foreign-prefix", ""
			var err error
			bad.EntryHash, err = canonicalHashHex(bad)
			if err != nil {
				t.Fatal(err)
			}
		}
		first, err := json.Marshal(encoded[0])
		if err != nil {
			t.Fatal(err)
		}
		last, err := json.Marshal(bad)
		if err != nil {
			t.Fatal(err)
		}
		wire := append(append(append(first, '\n'), last...), '\n')
		path := filepath.Join(stateDir, "journal.jsonl")
		if err := os.WriteFile(path, wire, 0o600); err != nil {
			t.Fatal(err)
		}
		file, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		journal := &Journal{}
		loadErr := journal.loadReader(file)
		closeErr := file.Close()
		if closeErr != nil || loadErr == nil || loadErr.Error() != "journal line 2 "+failure+" mismatch" {
			t.Fatalf("%s: load=%v close=%v", failure, loadErr, closeErr)
		}
		if journal.validationCount != 1 || journal.lastHash != encoded[0].EntryHash || !reflect.DeepEqual(journal.Entries(), encoded[:1]) {
			t.Fatalf("%s rejection committed unauthenticated history", failure)
		}
		candidate := journalIndexTestEntry(StageBroadcast)
		candidate.TransactionHash = "valid-after-corrupt-row"
		if err := journal.validateEntry(candidate); err != nil {
			t.Fatalf("%s rejection poisoned the index: %v", failure, err)
		}
	}
}

// Both failed Write and successful Write followed by failed Sync leave the new
// index unchanged. Real OS descriptors force each boundary without timing hooks.
func TestJournalValidationIndexFailedAppendDoesNotCommit(t *testing.T) {
	for _, failure := range []string{"write", "sync"} {
		stateDir, _ := writeJournalIndexTestHistory(t, []JournalEntry{journalIndexTestEntry(StageIntent)})
		journal, err := OpenJournal(stateDir)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = journal.Close() })
		original := journal.file
		before, beforeHash := journal.Entries(), journal.lastHash
		var broken, reader *os.File
		if failure == "write" {
			broken, err = os.Open(filepath.Join(stateDir, "journal.jsonl"))
		} else {
			reader, broken, err = os.Pipe()
		}
		if err != nil {
			t.Fatal(err)
		}
		journal.file = broken
		rejected := journalIndexTestEntry(StageBroadcast)
		rejected.TransactionHash = "discarded-transaction"
		appendErr := journal.Append(rejected)
		journal.file = original
		if closeErr := broken.Close(); closeErr != nil {
			t.Fatal(closeErr)
		}
		if reader != nil {
			if closeErr := reader.Close(); closeErr != nil {
				t.Fatal(closeErr)
			}
		}
		if appendErr == nil || journal.validationCount != len(before) || journal.lastHash != beforeHash || !reflect.DeepEqual(journal.Entries(), before) {
			t.Fatalf("%s failure committed an unaccepted row: %v", failure, appendErr)
		}
		var fileErr *os.PathError
		if !errors.As(appendErr, &fileErr) || fileErr.Op != failure {
			t.Fatalf("%s did not reach its intended I/O boundary: %v", failure, appendErr)
		}
		if err := journal.Append(journalIndexTestEntry(StageBroadcast)); err != nil {
			t.Fatalf("%s failure left a transaction witness: %v", failure, err)
		}
		if err := journal.Close(); err != nil {
			t.Fatal(err)
		}
		if reopened, err := readJournalEntries(stateDir); err != nil || len(reopened) != 2 || reopened[1].TransactionHash != "synthetic-transaction" {
			t.Fatalf("%s failure changed durable history: entries=%d err=%v", failure, len(reopened), err)
		}
	}
}

// Existing read-only callers and fixtures construct and mutate slice snapshots.
// They are not authenticated append owners and must not acquire stale indexes.
func TestJournalValidationIndexLiteralSnapshotsStayMutable(t *testing.T) {
	broadcast := journalIndexTestEntry(StageBroadcast)
	journal := &Journal{entries: []JournalEntry{broadcast}}
	candidate := broadcast
	candidate.TransactionHash = "other-transaction"
	if err := journal.validateEntry(candidate); err == nil {
		t.Fatal("literal snapshot ignored its existing transaction")
	}
	journal.entries[0].TransactionHash = candidate.TransactionHash
	if err := journal.validateEntry(candidate); err != nil {
		t.Fatalf("literal snapshot retained an earlier transaction: %v", err)
	}
	journal.entries[0] = journalIndexTestEntry(StageVerified)
	if err := journal.validateEntry(candidate); err == nil || !strings.Contains(err.Error(), "terminal") {
		t.Fatalf("literal snapshot ignored replaced terminal history: %v", err)
	}
	journal.entries = nil
	if err := journal.validateEntry(candidate); err != nil || journal.validationKVs != nil {
		t.Fatalf("literal snapshot retained removed history or acquired an index: %v", err)
	}
}
