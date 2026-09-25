// Final receipt consumers authenticate journal history before selecting one
// action, including malformed bytes and independently rehashed conflicts.
package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// Fixtures preserve raw hash-chain construction independently of the decoder
// so intentionally invalid semantic histories can reach the public boundary.
func finalArchiveJournalTestBytes(t *testing.T, entries []JournalEntry) []byte {
	t.Helper()
	var result bytes.Buffer
	previous := ""
	for index, entry := range entries {
		entry.Sequence, entry.PreviousHash, entry.EntryHash = uint64(index+1), previous, ""
		hash, err := canonicalHashHex(entry)
		if err != nil {
			t.Fatal(err)
		}
		entry.EntryHash = hash
		raw, err := json.Marshal(entry)
		if err != nil {
			t.Fatal(err)
		}
		result.Write(raw)
		result.WriteByte('\n')
		previous = hash
	}
	return result.Bytes()
}

// A stale outer source selection cannot turn a tampered receipt record into
// action authority even when the JSON remains valid and the action matches.
func TestFinalArchiveJournalAuthenticatesReceiptSelection(t *testing.T) {
	entry := JournalEntry{Schema: "urnetwork-sim-journal-v1", DeploymentID: "synthetic-deployment", PlanHash: finalTestHex(31),
		ActionID: "synthetic.finalized", IntentHash: finalTestHex(32), Stage: StageFinalized,
		TransactionHash: "synthetic-transaction", BlockNumber: 73, BlockHash: "synthetic-block"}
	raw := finalArchiveJournalTestBytes(t, []JournalEntry{entry})
	archive := &finalSemanticArchive{files: map[string][]byte{"launch-foundation/journal.jsonl": raw}}
	got, err := archive.actionFinalized(entry.ActionID)
	if err != nil || got.TransactionHash != entry.TransactionHash || got.BlockNumber != entry.BlockNumber {
		t.Fatalf("valid retained receipt was rejected: %+v %v", got, err)
	}
	for _, change := range []struct{ from, to string }{
		{from: "synthetic-transaction", to: "synthetic-substitution"},
		{from: `"sequence":1`, to: `"sequence":2`},
		{from: `"entry_hash":"`, to: `"entry_hash":"changed-`},
	} {
		changed := bytes.Replace(raw, []byte(change.from), []byte(change.to), 1)
		if bytes.Equal(changed, raw) {
			t.Fatal("mutation did not reach original journal bytes", change.from)
		}
		archive.files["launch-foundation/journal.jsonl"] = changed
		if _, err := archive.actionFinalized(entry.ActionID); err == nil || !strings.Contains(err.Error(), "hash-chain") {
			t.Fatalf("tampered %s selected a finalized receipt: %v", change.from, err)
		}
	}
	archive.files["launch-foundation/journal.jsonl"] = raw
	if got, err := archive.actionFinalized(entry.ActionID); err != nil || got.TransactionHash != entry.TransactionHash {
		t.Fatalf("restored exact source did not recover: %+v %v", got, err)
	}
}

// Rehashing every line is insufficient when one planned action changes its
// identity, transaction, deployment or already-terminal outcome.
func TestFinalArchiveJournalRejectsRehashedActionConflicts(t *testing.T) {
	initial := JournalEntry{Schema: "urnetwork-sim-journal-v1", DeploymentID: "synthetic-deployment", PlanHash: finalTestHex(41),
		ActionID: "synthetic.action", IntentHash: finalTestHex(42), Stage: StageFinalized,
		TransactionHash: "synthetic-transaction", BlockNumber: 91, BlockHash: "synthetic-block"}
	for _, kind := range []string{"deployment", "intent", "transaction", "terminal"} {
		first, next := initial, initial
		switch kind {
		case "deployment":
			next.DeploymentID = "synthetic-foreign-deployment"
		case "intent":
			next.IntentHash = finalTestHex(43)
		case "transaction":
			next.TransactionHash = "synthetic-second-transaction"
		case "terminal":
			first.Stage, first.PostconditionHash = StageVerified, finalTestHex(44)
			var err error
			first.PostconditionPath, err = postconditionRelativePath(first.PlanHash, first.ActionID)
			if err != nil {
				t.Fatal(err)
			}
		}
		archive := &finalSemanticArchive{files: map[string][]byte{"launch-foundation/journal.jsonl": finalArchiveJournalTestBytes(t, []JournalEntry{first, next})}}
		if _, err := archive.actionFinalized(initial.ActionID); err == nil || !strings.Contains(err.Error(), "captured journal") {
			t.Fatalf("rehashed %s conflict became action authority: %v", kind, err)
		}
	}
}

// Missing archive state is a bounded diagnostic, never a panic or an empty
// successful journal that another finalization reader could mistake for proof.
func TestFinalArchiveJournalRejectsAbsentAndMalformedSources(t *testing.T) {
	for _, archive := range []*finalSemanticArchive{nil, {}, {files: map[string][]byte{"launch-foundation/journal.jsonl": nil}}, {files: map[string][]byte{"launch-foundation/journal.jsonl": []byte("{}\n")}}} {
		if entries, err := archive.journalEntries(); err == nil || entries != nil {
			t.Fatalf("absent or malformed history became valid: entries=%d error=%v", len(entries), err)
		}
	}
}
