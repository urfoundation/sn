// Recovery authenticates its signed journal cut even after the live journal
// grows beyond the raw evidence limit; suffix entries grant no prior authority.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// Produces deterministic, hash-chained synthetic records without writer clocks.
func campaignRecoveryPrefixTestLine(t *testing.T, sequence uint64, previous, padding string) ([]byte, string) {
	t.Helper()
	entry := JournalEntry{
		Schema: "urnetwork-sim-journal-v1", Sequence: sequence, Time: "2026-01-01T00:00:00Z",
		DeploymentID: "synthetic-prefix-deployment", PlanHash: "synthetic-prefix-plan",
		ActionID: fmt.Sprintf("synthetic-action-%d", sequence), IntentHash: "synthetic-intent",
		Stage: StageIntent, PreviousHash: previous, Error: padding,
	}
	hash, err := canonicalHashHex(entry)
	if err != nil {
		t.Fatal(err)
	}
	entry.EntryHash = hash
	raw, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	return append(raw, '\n'), hash
}

// The signed generation binds only the first complete record.
func campaignRecoveryPrefixTestFixture(t *testing.T) (*scenarioCampaignAttempt, *scenarioCampaignAttempt, []byte) {
	t.Helper()
	prefix, _ := campaignRecoveryPrefixTestLine(t, 1, "", "")
	attempt := &scenarioCampaignAttempt{stateDir: t.TempDir(), payload: scenarioCampaignAttemptPayload{
		RunID: "synthetic-current-run", Recovery: &scenarioCampaignRecovery{
			PriorRunID: "synthetic-prior-run", PriorJournalBytes: uint64(len(prefix)), PriorJournalSha256: bytesSHA256(prefix),
		},
	}}
	prior := &scenarioCampaignAttempt{payload: scenarioCampaignAttemptPayload{RunID: "synthetic-prior-run"}}
	return attempt, prior, prefix
}

func TestScenarioCampaignRecoveryJournalPrefixAllowsLegacyModeAndLargeGrowth(t *testing.T) {
	attempt, prior, prefix := campaignRecoveryPrefixTestFixture(t)
	path := filepath.Join(attempt.stateDir, "journal.jsonl")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(prefix); err != nil {
		t.Fatal(err)
	}
	_, lastHash := campaignRecoveryPrefixTestLine(t, 1, "", "")
	for sequence := uint64(2); sequence <= 34; sequence++ {
		line, nextHash := campaignRecoveryPrefixTestLine(t, sequence, lastHash, strings.Repeat("x", 1024*1024))
		if _, err := file.Write(line); err != nil {
			t.Fatal(err)
		}
		lastHash = nextHash
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() <= maximumCampaignEvidenceRawFileBytes {
		t.Fatalf("growth fixture did not exceed raw evidence bound: %v %v", info, err)
	}
	raw, entries, err := readScenarioCampaignRecoveryJournalPrefix(attempt, prior)
	if err != nil || !bytes.Equal(raw, prefix) || len(entries) != 1 || entries[0].ActionID != "synthetic-action-1" {
		t.Fatalf("signed prefix was lost or suffix granted prior authority: bytes=%d entries=%d error=%v", len(raw), len(entries), err)
	}
	after, err := os.Stat(path)
	if err != nil || !sameFinalCollectedFileState(info, after) {
		t.Fatalf("read changed legacy journal: %v", err)
	}
}

func TestScenarioCampaignRecoveryJournalPrefixRejectsTamperAndInvalidBounds(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(*scenarioCampaignRecovery, []byte) []byte
	}{
		{name: "changed-prefix", edit: func(_ *scenarioCampaignRecovery, raw []byte) []byte {
			return bytes.Replace(raw, []byte("synthetic-action-1"), []byte("synthetic-action-2"), 1)
		}},
		{name: "truncated", edit: func(_ *scenarioCampaignRecovery, raw []byte) []byte { return raw[:len(raw)-1] }},
		{name: "empty-bound", edit: func(recovery *scenarioCampaignRecovery, raw []byte) []byte {
			recovery.PriorJournalBytes = 0
			return raw
		}},
		{name: "oversized-bound", edit: func(recovery *scenarioCampaignRecovery, raw []byte) []byte {
			recovery.PriorJournalBytes = maximumCampaignEvidenceRawFileBytes + 1
			return raw
		}},
		{name: "overflow-bound", edit: func(recovery *scenarioCampaignRecovery, raw []byte) []byte {
			recovery.PriorJournalBytes = ^uint64(0)
			return raw
		}},
		{name: "malformed-hash", edit: func(recovery *scenarioCampaignRecovery, raw []byte) []byte {
			recovery.PriorJournalSha256 = "malformed"
			return raw
		}},
		{name: "record-cut", edit: func(recovery *scenarioCampaignRecovery, raw []byte) []byte {
			recovery.PriorJournalBytes--
			recovery.PriorJournalSha256 = bytesSHA256(raw[:len(raw)-1])
			return raw
		}},
		{name: "invalid-chain", edit: func(recovery *scenarioCampaignRecovery, raw []byte) []byte {
			raw = bytes.Replace(raw, []byte("synthetic-action-1"), []byte("synthetic-action-2"), 1)
			recovery.PriorJournalSha256 = bytesSHA256(raw)
			return raw
		}},
		{name: "invalid-suffix", edit: func(_ *scenarioCampaignRecovery, raw []byte) []byte {
			return append(raw, []byte("invalid suffix\n")...)
		}},
	} {
		attempt, prior, prefix := campaignRecoveryPrefixTestFixture(t)
		raw := test.edit(attempt.payload.Recovery, prefix)
		if err := os.WriteFile(filepath.Join(attempt.stateDir, "journal.jsonl"), raw, 0o644); err != nil {
			t.Fatal(err)
		}
		if raw, entries, err := readScenarioCampaignRecoveryJournalPrefix(attempt, prior); err == nil || raw != nil || entries != nil {
			t.Fatalf("accepted %s: bytes=%d entries=%d error=%v", test.name, len(raw), len(entries), err)
		}
	}
}

func TestScenarioCampaignRecoveryJournalPrefixRejectsUnsafeFileTypes(t *testing.T) {
	for _, kind := range []string{"symlink", "fifo", "directory", "parent-symlink"} {
		attempt, prior, prefix := campaignRecoveryPrefixTestFixture(t)
		path := filepath.Join(attempt.stateDir, "journal.jsonl")
		var err error
		switch kind {
		case "symlink":
			target := filepath.Join(t.TempDir(), "synthetic-journal.jsonl")
			if err := os.WriteFile(target, prefix, 0o644); err != nil {
				t.Fatal(err)
			}
			err = os.Symlink(target, path)
		case "fifo":
			err = syscall.Mkfifo(path, 0o600)
		case "directory":
			err = os.Mkdir(path, 0o700)
		case "parent-symlink":
			if err := os.WriteFile(path, prefix, 0o644); err != nil {
				t.Fatal(err)
			}
			alias := filepath.Join(t.TempDir(), "synthetic-state")
			err = os.Symlink(attempt.stateDir, alias)
			attempt.stateDir = alias
		}
		if err != nil {
			t.Fatal(err)
		}
		if raw, entries, err := readScenarioCampaignRecoveryJournalPrefix(attempt, prior); err == nil || raw != nil || entries != nil {
			t.Fatalf("accepted %s: bytes=%d entries=%d error=%v", kind, len(raw), len(entries), err)
		}
	}
}

func TestScenarioCampaignRecoveryJournalPrefixCapturesUnsignedCut(t *testing.T) {
	attempt, prior, prefix := campaignRecoveryPrefixTestFixture(t)
	attempt.payload.Recovery = nil
	if err := os.WriteFile(filepath.Join(attempt.stateDir, "journal.jsonl"), prefix, 0o600); err != nil {
		t.Fatal(err)
	}
	raw, entries, err := readScenarioCampaignRecoveryJournalPrefix(attempt, prior)
	if err != nil || !bytes.Equal(raw, prefix) || len(entries) != 1 {
		t.Fatalf("new recovery could not capture exact journal cut: %d %d %v", len(raw), len(entries), err)
	}
}
