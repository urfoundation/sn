// Final journal capture preserves raw commitments above ordinary proof limits
// while keeping descriptor, memory, record and action-history bounds explicit.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// The fixture is a real hash chain larger than both old producer limits;
// expected bytes are hashed while writing instead of keeping another copy.
func TestFinalJournalSourceCapturesLargeExactFoundation(t *testing.T) {
	dir := t.TempDir()
	file, err := os.OpenFile(filepath.Join(dir, "journal.jsonl"), os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.New()
	writer := io.MultiWriter(file, digest)
	previous := ""
	var size uint64
	padding := strings.Repeat("x", 1024*1024)
	for sequence := uint64(1); sequence <= 33; sequence++ {
		line, hash := campaignRecoveryPrefixTestLine(t, sequence, previous, padding)
		if _, err := writer.Write(line); err != nil {
			t.Fatal(err)
		}
		size += uint64(len(line))
		previous = hash
	}
	if err := errors.Join(file.Sync(), file.Close()); err != nil {
		t.Fatal(err)
	}
	if size <= maximumCampaignEvidenceRawFileBytes || size <= finalCollectedBundleMaximumRawBytes {
		t.Fatal("fixture did not cross both former producer limits")
	}
	entry, err := finalCollectedJournalEntryContext(t.Context(), dir)
	if err != nil || entry.Path != "journal.jsonl" || entry.SizeBytes != size || entry.ContentHash != fmt.Sprintf("sha256:%x", digest.Sum(nil)) {
		t.Fatalf("large source lost original bytes: path=%s bytes=%d hash=%s error=%v", entry.Path, entry.SizeBytes, entry.ContentHash, err)
	}
	entries, err := decodeFinalSemanticJournalBytes(entry.Data)
	if err != nil || len(entries) != 33 || entries[len(entries)-1].EntryHash != previous {
		t.Fatalf("captured large chain failed exact replay: entries=%d error=%v", len(entries), err)
	}
}

// Reads are bounded independently of source length. Exact capacity is accepted;
// oversized files fail before the first body allocation or source read.
func TestFinalJournalSourceBoundsReadsAndCapacity(t *testing.T) {
	dir := t.TempDir()
	line, _ := campaignRecoveryPrefixTestLine(t, 1, "", strings.Repeat("x", 2*finalJournalReadChunkBytes))
	path := filepath.Join(dir, "journal.jsonl")
	if err := os.WriteFile(path, line, 0o600); err != nil {
		t.Fatal(err)
	}
	reads := 0
	hooks := finalJournalReadHooks{read: func(reader io.Reader, target []byte) (int, error) {
		reads++
		if len(target) > finalJournalReadChunkBytes {
			return 0, errors.New("unbounded final journal read")
		}
		return io.ReadFull(reader, target)
	}}
	raw, err := readFinalJournalSourceWithHooks(t.Context(), dir, int64(len(line)), hooks)
	if err != nil || !bytes.Equal(raw, line) || reads < 6 {
		t.Fatalf("bounded exact-capacity capture failed: reads=%d error=%v", reads, err)
	}
	for _, maximum := range []int64{int64(len(line) - 1), 0, maximumFinalJournalBytes + 1} {
		before := reads
		if raw, err := readFinalJournalSourceWithHooks(t.Context(), dir, maximum, hooks); err == nil || raw != nil || reads != before {
			t.Fatalf("invalid capacity %d read or accepted body: reads=%d before=%d error=%v", maximum, reads, before, err)
		}
	}
	if err := os.Truncate(path, maximumFinalJournalBytes+1); err != nil {
		t.Fatal(err)
	}
	before := reads
	if raw, err := readFinalJournalSourceWithHooks(t.Context(), dir, maximumFinalJournalBytes, hooks); err == nil || raw != nil || reads != before {
		t.Fatalf("oversized physical journal was read: reads=%d before=%d error=%v", reads, before, err)
	}
}

// Changes occur after the initial bytes have been read. Appends are a later
// snapshot; prefix tamper, truncation, replacement and canceled owners fail.
func TestFinalJournalSourceRejectsTamperAndRetainsFiniteAppend(t *testing.T) {
	for _, kind := range []string{"rewrite", "truncate", "replacement", "append", "cancel"} {
		dir := t.TempDir()
		path := filepath.Join(dir, "journal.jsonl")
		line, hash := campaignRecoveryPrefixTestLine(t, 1, "", "")
		suffix, _ := campaignRecoveryPrefixTestLine(t, 2, hash, "")
		if err := os.WriteFile(path, line, 0o600); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		hook := func() {
			switch kind {
			case "rewrite":
				changed, _ := campaignRecoveryPrefixTestLine(t, 2, "", "")
				if err := os.WriteFile(path, changed, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
					t.Fatal(err)
				}
			case "truncate":
				if err := os.Truncate(path, int64(len(line)-1)); err != nil {
					t.Fatal(err)
				}
			case "replacement":
				replacement := filepath.Join(dir, "replacement.jsonl")
				if err := os.WriteFile(replacement, line, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(replacement, path); err != nil {
					t.Fatal(err)
				}
			case "append":
				file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
				if err != nil {
					t.Fatal(err)
				}
				_, writeErr := file.Write(suffix)
				if err := errors.Join(writeErr, file.Close()); err != nil {
					t.Fatal(err)
				}
			case "cancel":
				cancel()
			}
		}
		raw, err := readFinalJournalSourceWithHooks(ctx, dir, maximumFinalJournalBytes, finalJournalReadHooks{captured: hook})
		cancel()
		if kind == "append" {
			if err != nil || !bytes.Equal(raw, line) {
				t.Fatalf("concurrent append changed finite source image: %v", err)
			}
			continue
		}
		if err == nil || raw != nil || kind == "cancel" && !errors.Is(err, context.Canceled) {
			t.Fatalf("%s returned untrusted final bytes: %v", kind, err)
		}
	}
}

// Missing or unsafe source paths remain precise failures and cannot block on a
// FIFO or silently become a valid empty finalization journal.
func TestFinalJournalSourceRejectsMissingEmptyAndUnsafeFiles(t *testing.T) {
	for _, kind := range []string{"missing", "empty", "symlink", "fifo", "directory", "partial"} {
		dir := t.TempDir()
		path := filepath.Join(dir, "journal.jsonl")
		var err error
		switch kind {
		case "missing":
		case "empty":
			err = os.WriteFile(path, nil, 0o600)
		case "symlink":
			target := filepath.Join(t.TempDir(), "source.jsonl")
			line, _ := campaignRecoveryPrefixTestLine(t, 1, "", "")
			if err := os.WriteFile(target, line, 0o600); err != nil {
				t.Fatal(err)
			}
			err = os.Symlink(target, path)
		case "fifo":
			err = syscall.Mkfifo(path, 0o600)
		case "directory":
			err = os.Mkdir(path, 0o700)
		case "partial":
			line, _ := campaignRecoveryPrefixTestLine(t, 1, "", "")
			err = os.WriteFile(path, line[:len(line)-1], 0o600)
		}
		if err != nil {
			t.Fatal(err)
		}
		if raw, err := readFinalJournalSourceContext(t.Context(), dir); err == nil || raw != nil {
			t.Fatalf("%s became a final journal: %v", kind, err)
		}
	}
}

// A complete hash chain must also preserve one deployment, action intent,
// transaction and terminal verification. Per-record validation cannot do this.
func TestFinalJournalSourceReplayRetainsActionHistoryAndRecordBound(t *testing.T) {
	first, hash := campaignRecoveryPrefixTestLine(t, 1, "", "")
	var initial JournalEntry
	if err := json.Unmarshal(first, &initial); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"deployment", "intent", "transaction", "terminal"} {
		prior := initial
		if kind == "transaction" {
			prior.Stage, prior.TransactionHash, prior.BlockHash, prior.BlockNumber = StageFinalized, "synthetic-tx-one", "synthetic-block", 1
		}
		if kind == "terminal" {
			prior.Stage, prior.PostconditionHash = StageVerified, "synthetic-postcondition"
			var err error
			prior.PostconditionPath, err = legacyPostconditionRelativePath(prior.ActionID)
			if err != nil {
				t.Fatal(err)
			}
		}
		prior.EntryHash = ""
		hash, err := canonicalHashHex(prior)
		if err != nil {
			t.Fatal(err)
		}
		prior.EntryHash = hash
		priorRaw, err := json.Marshal(prior)
		if err != nil {
			t.Fatal(err)
		}
		next := initial
		next.Sequence, next.PreviousHash, next.EntryHash = 2, hash, ""
		switch kind {
		case "deployment":
			next.DeploymentID = "foreign-deployment"
		case "intent":
			next.IntentHash = "foreign-intent"
		case "transaction":
			next.Stage, next.TransactionHash, next.BlockHash, next.BlockNumber = StageFinalized, "synthetic-tx-two", "synthetic-block", 2
		}
		next.EntryHash, err = canonicalHashHex(next)
		if err != nil {
			t.Fatal(err)
		}
		nextRaw, err := json.Marshal(next)
		if err != nil {
			t.Fatal(err)
		}
		raw := append(append(append(priorRaw, '\n'), nextRaw...), '\n')
		if _, err := decodeFinalSemanticJournalBytes(raw); err == nil {
			t.Fatalf("individually valid %s rewrite passed full journal replay", kind)
		}
	}
	oversized, _ := campaignRecoveryPrefixTestLine(t, 2, hash, strings.Repeat("x", maximumJournalRecordBytes))
	if _, err := decodeFinalSemanticJournalBytes(append(first, oversized...)); err == nil || !strings.Contains(err.Error(), "token too long") {
		t.Fatalf("oversized individual record escaped the journal scanner bound: %v", err)
	}
}

// Journal capacity is routed only by the exact local relay source. Generic
// content-addressed proofs and foreign journal identities retain old admission.
func TestFinalJournalSourceRoutesOnlyExactRelayIdentity(t *testing.T) {
	hash := bytesSHA256([]byte("synthetic journal"))
	digest := strings.TrimPrefix(hash, "sha256:")
	path, err := finalValidatorSourcePathV2("relay-journal", "journal.jsonl", "", hash)
	if err != nil || path != "final-inputs/validators/v2/journals/"+digest+".jsonl" {
		t.Fatalf("exact journal lost its dedicated owner: %s %v", path, err)
	}
	for _, test := range []struct{ kind, name, origin string }{
		{kind: "private", name: "journal.jsonl"},
		{kind: "setup", name: "journal.jsonl"},
	} {
		path, err := finalValidatorSourcePathV2(test.kind, test.name, test.origin, hash)
		if err != nil || path != "final-inputs/validators/v2/"+digest+".bin" {
			t.Fatalf("unrelated proof borrowed journal capacity: %s %v", path, err)
		}
	}
	for _, test := range []struct{ name, origin string }{{name: "other.jsonl"}, {name: "journal.jsonl", origin: "foreign-origin"}} {
		if _, err := finalValidatorSourcePathV2("relay-journal", test.name, test.origin, hash); err == nil {
			t.Fatal("foreign journal identity obtained a dedicated owner")
		}
	}
}
