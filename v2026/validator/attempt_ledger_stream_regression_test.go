//go:build linux || darwin

package validator

// These two regressions use only the pre-integration ledger API and qualified
// store fixture helper, so Terra can transplant this file onto the qualified
// store foundation for an independent deterministic baseline failure capture.

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// The private persistence seam may inspect ledger state. It is serialized by
// the append gate, not called while the metadata mutex is still held.
func TestAttemptLedgerAppendSeamDoesNotHoldStateLock(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 1)
	ledger, err := NewAttemptLedger(filepath.Join(t.TempDir(), "state"), fixture.identity, fixture.validatorKey)
	if err != nil {
		t.Fatal(err)
	}
	appendRecord := ledger.appendFn
	ledger.appendFn = func(path string, raw []byte) error {
		if !ledger.stateLock.TryLock() {
			return errors.New("legacy persistence seam invoked with stateLock held")
		}
		ledger.stateLock.Unlock()
		return appendRecord(path, raw)
	}
	if _, err := ledger.Append(fixture.recordTs[0]); err != nil {
		t.Fatal(err)
	}
}

// Earlier valid terminal records cannot become visible when a later pending
// record proves that the replay belongs to a different settlement epoch.
func TestAttemptLedgerAttachLateFailureKeepsPriorState(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 2)
	records := append([]AttemptRecord(nil), fixture.recordTs[:9]...)
	records[8].Boundary.SettlementEpoch++
	records[8] = resignAttemptRecordStoreTest(t, records[8], fixture.validatorKey)
	var raw []byte
	for _, record := range records {
		line, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		raw = append(raw, line...)
		raw = append(raw, '\n')
	}
	dir := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "attempt-ledger.jsonl"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	ledger, err := NewAttemptLedger(dir, fixture.identity, fixture.validatorKey)
	if err != nil {
		t.Fatal(err)
	}
	stats := NewStatsEngine(StatsConfig{})
	stats.settlementEpoch, stats.settlementEpochKnown = 42, true
	stats.attemptSettlementFirstSequence, stats.attemptEgressFirstSequence = 1, 1
	if err := stats.AttachAttemptLedger(ledger, dir); err == nil {
		t.Fatal("late wrong-epoch record admitted")
	}
	if stats.attemptLedger != nil || stats.attemptLastAppliedSequence != 0 || len(stats.window) != 0 || len(stats.egress) != 0 {
		t.Fatal("failed attachment exposed a partial replay or ledger binding")
	}
}
