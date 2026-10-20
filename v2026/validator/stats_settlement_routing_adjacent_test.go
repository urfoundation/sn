//go:build linux || darwin

package validator

// Queued startup may establish routing authority after initial validation.
// These controls force that publication and preserve valid canonical spelling.

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The supplied path is not authoritative while the engine is unbound. Its
// queued owner must recheck the real ledger's path after startup publishes it.
func TestStatsSettlementRoutingRechecksPathAfterQueuedBinding(t *testing.T) {
	stats, ledger, fixture, dir := newStatsWriteReplayTest(t)
	wrongDir := newAttemptLedgerDiskTestStateDir(t)
	wrongPath := filepath.Join(wrongDir, "stats.json")
	untouched := []byte("test-owned unrelated namespace sentinel\n")
	if err := os.WriteFile(wrongPath, untouched, 0o600); err != nil {
		t.Fatal(err)
	}
	entered, release := pauseStatsWriteReplayTest(ledger)
	waiting := make(chan struct{})
	replayWrites := 0
	stats.writeHooks = statsWriteHooks{step: func(operation, stage string) {
		if operation == "settlement" && stage == "waiting" {
			close(waiting)
		}
	}, writeSnapshot: func(directory *statsSnapshotDirectory, write statsSnapshotWrite) error {
		replayWrites++
		return writeStatsSnapshotOwned(directory, write)
	}}
	replay := startStatsWriteTestCall(func() error { return stats.AttachAttemptLedger(ledger, dir) })
	defer func() { release(); _ = replay.join() }()
	<-entered
	coordinator := t.TempDir()
	settlementWrites := 0
	participants := []AttemptSettlementParticipant{{NoID: ledger.identity.NoID, StateDir: wrongDir, Stats: stats}}
	settlement := startStatsWriteTestCall(func() error {
		return advanceAttemptSettlementEpochWithWrite(coordinator, 43, attemptLedgerTestBoundary(), participants, func(string, []byte) error {
			settlementWrites++
			return nil
		})
	})
	defer func() { release(); _ = settlement.join() }()
	select {
	case <-waiting:
	case <-settlement.done:
		t.Fatalf("unbound path fixture did not enter queued admission: %v", settlement.err)
	}
	release()
	if err := replay.join(); err != nil {
		t.Fatal(err)
	}
	if err := settlement.join(); err == nil || !strings.Contains(err.Error(), "state directory differs from bound ledger") {
		t.Fatalf("queued participant did not recheck its newly bound state directory: %v", err)
	}
	gotUntouched, err := os.ReadFile(wrongPath)
	if err != nil || !bytes.Equal(untouched, gotUntouched) {
		t.Fatalf("queued routing refusal changed the unrelated namespace: %v", err)
	}
	snapshot, _ := readStatsWriteSnapshotTest(t, dir)
	if replayWrites != 1 || settlementWrites != 0 || stats.attemptLedger != ledger || stats.attemptCutPending || stats.attemptSettlementCutPending || stats.attemptSettlementCutEpoch != 0 || snapshot.AttemptLastAppliedSequence != 8 {
		t.Fatal("queued path refusal changed the completed replay or admission")
	}
	if head, err := ledger.Head(); err != nil || head.LastSequence != 8 || head.Root != fixture.recordTs[7].RecordHash {
		t.Fatalf("queued routing changed genuine attempt authority: %+v/%v", head, err)
	}
	entries, err := os.ReadDir(coordinator)
	if err != nil || len(entries) != 0 {
		t.Fatalf("queued path refusal published a transaction: %v", err)
	}
}

// The first engine is already bound; the second acquires a genuinely different
// domain during its paused replay. Both paths and NoIDs remain correct.
func TestStatsSettlementRoutingRechecksDomainAfterQueuedBinding(t *testing.T) {
	first, firstLedger := newAttemptSettlementTestParticipant(t, 1)
	t.Cleanup(func() { _ = firstLedger.Close() })
	second, ledger, _, secondDir := newStatsWriteReplayTest(t)
	if equalAttemptSettlementIdentity(firstLedger.identity, ledger.identity) {
		t.Fatal("queued-domain fixture does not contain distinct genuine domains")
	}
	_, firstBefore := readStatsWriteSnapshotTest(t, first.StateDir)
	entered, release := pauseStatsWriteReplayTest(ledger)
	waiting := make(chan struct{})
	replayWrites := 0
	second.writeHooks = statsWriteHooks{step: func(operation, stage string) {
		if operation == "settlement" && stage == "waiting" {
			close(waiting)
		}
	}, writeSnapshot: func(directory *statsSnapshotDirectory, write statsSnapshotWrite) error {
		replayWrites++
		return writeStatsSnapshotOwned(directory, write)
	}}
	replay := startStatsWriteTestCall(func() error { return second.AttachAttemptLedger(ledger, secondDir) })
	defer func() { release(); _ = replay.join() }()
	<-entered
	coordinator := t.TempDir()
	settlementWrites := 0
	participants := []AttemptSettlementParticipant{first, {NoID: ledger.identity.NoID, StateDir: secondDir, Stats: second}}
	settlement := startStatsWriteTestCall(func() error {
		return advanceAttemptSettlementEpochWithWrite(coordinator, 43, attemptLedgerTestBoundary(), participants, func(string, []byte) error {
			settlementWrites++
			return nil
		})
	})
	defer func() { release(); _ = settlement.join() }()
	select {
	case <-waiting:
	case <-settlement.done:
		t.Fatalf("unbound-domain fixture did not enter queued admission: %v", settlement.err)
	}
	release()
	if err := replay.join(); err != nil {
		t.Fatal(err)
	}
	if err := settlement.join(); err == nil || !strings.Contains(err.Error(), "settlement transition validator identities differ") {
		t.Fatalf("queued participant did not recheck its newly bound domain: %v", err)
	}
	_, firstAfter := readStatsWriteSnapshotTest(t, first.StateDir)
	snapshot, _ := readStatsWriteSnapshotTest(t, secondDir)
	if replayWrites != 1 || settlementWrites != 0 || !bytes.Equal(firstBefore, firstAfter) || snapshot.AttemptLastAppliedSequence != 8 || second.attemptLedger != ledger {
		t.Fatal("queued domain refusal changed either legitimate snapshot")
	}
	for _, participant := range participants {
		stats := participant.Stats
		if stats.settlementEpoch != 42 || stats.attemptCutPending || stats.attemptSettlementCutPending || stats.attemptSettlementCutEpoch != 0 || stats.writeOwner != nil {
			t.Fatal("queued domain refusal changed admission or leaked ownership")
		}
	}
	entries, err := os.ReadDir(coordinator)
	if err != nil || len(entries) != 0 {
		t.Fatalf("queued domain refusal published a transaction: %v", err)
	}
}

// Canonical lexical equality retains harmless current-directory/trailing
// separator spellings. It does not resolve symlink aliases or change modes.
func TestStatsSettlementRoutingAcceptsCanonicalDirectorySpellings(t *testing.T) {
	first, firstLedger := newAttemptSettlementTestParticipant(t, 1)
	second, secondLedger := newAttemptSettlementTestParticipant(t, 2)
	t.Cleanup(func() { _ = firstLedger.Close(); _ = secondLedger.Close() })
	participants := []AttemptSettlementParticipant{
		{NoID: first.NoID, StateDir: first.StateDir + string(os.PathSeparator) + ".", Stats: first.Stats},
		{NoID: second.NoID, StateDir: second.StateDir + string(os.PathSeparator), Stats: second.Stats},
	}
	if err := AdvanceAttemptSettlementEpoch(t.TempDir(), 43, attemptLedgerTestBoundary(), participants); err != nil {
		t.Fatalf("canonical-equivalent operator directories were refused: %v", err)
	}
	transitions := []*AttemptSettlementTransition{first.Stats.settlementTransition, second.Stats.settlementTransition}
	if err := VerifyAttemptSettlementBatch(transitions); err != nil {
		t.Fatalf("canonical path normalization changed the genuine signed batch: %v", err)
	}
	for _, participant := range participants {
		snapshot, _ := readStatsWriteSnapshotTest(t, participant.StateDir)
		if snapshot.SettlementEpoch == nil || *snapshot.SettlementEpoch != 43 || snapshot.SettlementTransition == nil || snapshot.SettlementTransition.Identity.NoID != participant.NoID {
			t.Fatal("canonical path normalization changed operator snapshot routing")
		}
	}
}

// A coherent unchanged-epoch no-op may still include an unbound compatibility
// engine. Its caller label must not be promoted into authenticated authority.
func TestStatsSettlementRoutingRetainsMixedUnboundCurrentNoOp(t *testing.T) {
	bound, ledger := newAttemptSettlementTestParticipant(t, 1)
	t.Cleanup(func() { _ = ledger.Close() })
	compatibility := NewStatsEngine(StatsConfig{AMin: 1})
	compatibilityDir := t.TempDir()
	if err := compatibility.AdvanceSettlementEpoch(42, compatibilityDir); err != nil {
		t.Fatal(err)
	}
	_, boundBefore := readStatsWriteSnapshotTest(t, bound.StateDir)
	_, compatibilityBefore := readStatsWriteSnapshotTest(t, compatibilityDir)
	participants := []AttemptSettlementParticipant{bound, {NoID: 777, StateDir: compatibilityDir, Stats: compatibility}}
	coordinator := t.TempDir()
	writes := 0
	err := advanceAttemptSettlementEpochWithIOModeContext(context.Background(), coordinator, 42, AttemptBoundary{}, participants, func(string, []byte) error {
		writes++
		return nil
	}, removeAttemptSettlementTransaction, false)
	_, boundAfter := readStatsWriteSnapshotTest(t, bound.StateDir)
	_, compatibilityAfter := readStatsWriteSnapshotTest(t, compatibilityDir)
	if err != nil || writes != 0 || !bytes.Equal(boundBefore, boundAfter) || !bytes.Equal(compatibilityBefore, compatibilityAfter) || compatibility.attemptLedger != nil {
		t.Fatalf("current mixed compatibility batch changed state or invented ledger authority: %v", err)
	}
	for _, participant := range participants {
		stats := participant.Stats
		if stats.settlementEpoch != 42 || stats.attemptCutPending || stats.attemptSettlementCutPending || stats.attemptSettlementCutEpoch != 0 || stats.writeOwner != nil {
			t.Fatal("current mixed compatibility batch changed admission or ownership")
		}
	}
	entries, err := os.ReadDir(coordinator)
	if err != nil || len(entries) != 0 {
		t.Fatalf("current mixed compatibility batch published artifacts: %v", err)
	}
}
