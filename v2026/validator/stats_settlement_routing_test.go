//go:build linux || darwin

package validator

// Participant routing must be checked before write ownership changes any
// admission state. Fixtures use genuine constructed and signed ledger domains.

import (
	"bytes"
	"crypto/ed25519"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Correct operator labels cannot authorize writing their signed snapshots into
// the other operator's directory. The pre-fix witness checks the actual swapped
// signed transitions, not merely an error or an invoked fault callback.
func TestStatsSettlementRoutingRejectsSwappedStateDirectoriesBeforeMutation(t *testing.T) {
	first, firstLedger := newAttemptSettlementTestParticipant(t, 1)
	second, secondLedger := newAttemptSettlementTestParticipant(t, 2)
	t.Cleanup(func() { _ = firstLedger.Close(); _ = secondLedger.Close() })
	_, firstBefore := readStatsWriteSnapshotTest(t, first.StateDir)
	_, secondBefore := readStatsWriteSnapshotTest(t, second.StateDir)
	participants := []AttemptSettlementParticipant{
		{NoID: first.NoID, StateDir: second.StateDir, Stats: first.Stats},
		{NoID: second.NoID, StateDir: first.StateDir, Stats: second.Stats},
	}
	coordinator := t.TempDir()
	var writes []string
	err := advanceAttemptSettlementEpochWithWrite(coordinator, 43, attemptLedgerTestBoundary(), participants, func(path string, raw []byte) error {
		writes = append(writes, path)
		return atomicStateWrite(path, raw, 0o600)
	})
	firstSnapshot, firstAfter := readStatsWriteSnapshotTest(t, first.StateDir)
	secondSnapshot, secondAfter := readStatsWriteSnapshotTest(t, second.StateDir)
	if err == nil && len(writes) == 2 && writes[0] == filepath.Join(second.StateDir, "stats.json") && writes[1] == filepath.Join(first.StateDir, "stats.json") && firstSnapshot.SettlementTransition != nil && secondSnapshot.SettlementTransition != nil && firstSnapshot.SettlementTransition.Identity.NoID == second.NoID && secondSnapshot.SettlementTransition.Identity.NoID == first.NoID {
		t.Fatal("caller-supplied state directories published signed snapshots into the opposite operator namespaces")
	}
	if err == nil || !strings.Contains(err.Error(), "state directory differs from bound ledger") {
		t.Fatalf("swapped state directories did not receive their early routing refusal: %v", err)
	}
	if len(writes) != 0 || !bytes.Equal(firstBefore, firstAfter) || !bytes.Equal(secondBefore, secondAfter) {
		t.Fatal("state-directory routing refusal changed an existing snapshot")
	}
	for _, participant := range []AttemptSettlementParticipant{first, second} {
		stats := participant.Stats
		if stats.settlementEpoch != 42 || stats.attemptCutPending || stats.attemptSettlementCutPending || stats.attemptSettlementCutEpoch != 0 || stats.writeOwner != nil {
			t.Fatal("state-directory routing refusal changed admission or ownership")
		}
	}
	entries, readErr := os.ReadDir(coordinator)
	if readErr != nil || len(entries) != 0 {
		t.Fatalf("state-directory routing refusal published coordinator artifacts: %v", readErr)
	}
}

// Two individually genuine ledgers from different deployment domains must be
// refused before either engine takes the settlement retry barrier. The old
// closure check occurs after those barriers have already been changed.
func TestStatsSettlementRoutingRejectsMixedDomainsBeforeAdmission(t *testing.T) {
	first, firstLedger := newAttemptSettlementTestParticipant(t, 1)
	t.Cleanup(func() { _ = firstLedger.Close() })
	secondDir := t.TempDir()
	secondStats := NewStatsEngine(StatsConfig{AMin: 1})
	if err := secondStats.AdvanceSettlementEpoch(42, secondDir); err != nil {
		t.Fatal(err)
	}
	identity := firstLedger.identity
	identity.NoID, identity.DeploymentID = 2, "different-settlement-domain"
	privateKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x42}, ed25519.SeedSize))
	secondLedger, err := NewAttemptLedger(secondDir, identity, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = secondLedger.Close() })
	if err := secondStats.AttachAttemptLedger(secondLedger, secondDir); err != nil {
		t.Fatal(err)
	}
	second := AttemptSettlementParticipant{NoID: 2, StateDir: secondDir, Stats: secondStats}
	_, firstBefore := readStatsWriteSnapshotTest(t, first.StateDir)
	_, secondBefore := readStatsWriteSnapshotTest(t, second.StateDir)
	coordinator := t.TempDir()
	writes := 0
	err = advanceAttemptSettlementEpochWithWrite(coordinator, 43, attemptLedgerTestBoundary(), []AttemptSettlementParticipant{first, second}, func(path string, raw []byte) error {
		writes++
		return atomicStateWrite(path, raw, 0o600)
	})
	if err == nil || !strings.Contains(err.Error(), "settlement transition validator identities differ") {
		t.Fatalf("mixed genuine domains did not reach their identity refusal: %v", err)
	}
	_, firstAfter := readStatsWriteSnapshotTest(t, first.StateDir)
	_, secondAfter := readStatsWriteSnapshotTest(t, second.StateDir)
	if writes != 0 || !bytes.Equal(firstBefore, firstAfter) || !bytes.Equal(secondBefore, secondAfter) {
		t.Fatal("mixed-domain fixture unexpectedly passed the snapshot publication boundary")
	}
	if first.Stats.attemptCutPending && second.Stats.attemptCutPending && first.Stats.attemptSettlementCutPending && second.Stats.attemptSettlementCutPending && first.Stats.attemptSettlementCutEpoch == 43 && second.Stats.attemptSettlementCutEpoch == 43 {
		t.Fatal("mixed ledger domains changed settlement admission before identity refusal")
	}
	for _, participant := range []AttemptSettlementParticipant{first, second} {
		stats := participant.Stats
		if stats.settlementEpoch != 42 || stats.attemptCutPending || stats.attemptSettlementCutPending || stats.attemptSettlementCutEpoch != 0 || stats.writeOwner != nil {
			t.Fatal("mixed-domain refusal changed admission or ownership")
		}
	}
	entries, readErr := os.ReadDir(coordinator)
	if readErr != nil || len(entries) != 0 {
		t.Fatalf("mixed-domain refusal published coordinator artifacts: %v", readErr)
	}
}
