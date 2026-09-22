package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A streaming proof may grow in record count, but its individual decoder
// allocation remains the same bounded journal scanner used by the writer.
func TestScenarioCampaignRecoveryJournalSnapshotRetainsRecordLimit(t *testing.T) {
	dir := t.TempDir()
	line, _ := campaignRecoveryPrefixTestLine(t, 1, "", strings.Repeat("x", maximumJournalRecordBytes))
	if err := os.WriteFile(filepath.Join(dir, "journal.jsonl"), line, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readScenarioCampaignJournalSnapshot(dir, nil, nil); err == nil || !strings.Contains(err.Error(), "token too long") {
		t.Fatalf("streaming capture removed the journal record bound: %v", err)
	}
}

// Read-ahead must not turn a same-size rewrite or a truncation into a fresh
// signed cut. The observation hook mutates only the synthetic owned source.
func TestScenarioCampaignRecoveryJournalSnapshotRejectsConcurrentMutation(t *testing.T) {
	for _, kind := range []string{"truncate", "same-size"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "journal.jsonl")
			line, _ := campaignRecoveryPrefixTestLine(t, 1, "", "")
			if err := os.WriteFile(path, line, 0o600); err != nil {
				t.Fatal(err)
			}
			cut, snapshot, err := readScenarioCampaignJournalSnapshot(dir, nil, func(JournalEntry) {
				changed := line[:0]
				if kind == "same-size" {
					changed = bytes.Replace(line, []byte("synthetic-action-1"), []byte("synthetic-action-2"), 1)
				}
				if err := os.WriteFile(path, changed, 0o600); err != nil {
					t.Fatal(err)
				}
			})
			if err == nil || cut != (scenarioCampaignJournalCut{}) || snapshot != (scenarioCampaignJournalCut{}) {
				t.Fatalf("changed source returned a journal commitment: cut=%+v snapshot=%+v error=%v", cut, snapshot, err)
			}
		})
	}
}

// Concurrent appends do not extend the preselected finite snapshot. The next
// observation must nevertheless validate their chain before reusing that cut.
func TestScenarioCampaignRecoveryJournalSnapshotFreezesAppendBoundary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "journal.jsonl")
	line, hash := campaignRecoveryPrefixTestLine(t, 1, "", "")
	suffix, _ := campaignRecoveryPrefixTestLine(t, 2, hash, "")
	if err := os.WriteFile(path, line, 0o600); err != nil {
		t.Fatal(err)
	}
	visits := 0
	cut, snapshot, err := readScenarioCampaignJournalSnapshot(dir, nil, func(JournalEntry) {
		visits++
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
		if err != nil {
			t.Fatal(err)
		}
		_, writeErr := file.Write(suffix)
		if err := errors.Join(writeErr, file.Close()); err != nil {
			t.Fatal(err)
		}
	})
	if err != nil || visits != 1 || cut != snapshot || cut.Bytes != uint64(len(line)) || cut.SHA256 != bytesSHA256(line) {
		t.Fatalf("capture grew past its finite descriptor snapshot: visits=%d cut=%+v snapshot=%+v error=%v", visits, cut, snapshot, err)
	}
	visits = 0
	retained, current, err := readScenarioCampaignJournalSnapshot(dir, &cut, func(JournalEntry) { visits++ })
	if err != nil || visits != 1 || retained != cut || current.Bytes != uint64(len(line)+len(suffix)) || current.SHA256 != bytesSHA256(append(line, suffix...)) {
		t.Fatalf("suffix granted prefix authority or failed current validation: visits=%d cut=%+v current=%+v error=%v", visits, retained, current, err)
	}
}
