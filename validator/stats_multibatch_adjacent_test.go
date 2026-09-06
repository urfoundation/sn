//go:build linux || darwin

package validator

// Ledger identity can become known while a batch waits. Stable lock order
// must also remain separate from the signed transaction's canonical ordering.

import (
	"context"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Startup is unpublished at the first identity check. Once its real signed
// replay finishes, the queued participant must recheck the new ledger binding
// before changing any admission flag or writing a transaction.
func TestStatsMultiBatchRechecksLedgerBoundDuringQueuedAdmission(t *testing.T) {
	stats, ledger, fixture, dir := newStatsWriteReplayTest(t)
	entered, release := pauseStatsWriteReplayTest(ledger)
	waiting := make(chan struct{})
	snapshotWrites := 0
	stats.writeHooks = statsWriteHooks{step: func(operation, stage string) {
		if operation == "settlement" && stage == "waiting" {
			close(waiting)
		}
	}, writeSnapshot: func(path string, raw []byte) error {
		snapshotWrites++
		return atomicStateWrite(path, raw, 0o600)
	}}
	replay := startStatsWriteTestCall(func() error { return stats.AttachAttemptLedger(ledger, dir) })
	defer func() { release(); _ = replay.join() }()
	<-entered
	coordinator := t.TempDir()
	settlementWrites := 0
	participants := []AttemptSettlementParticipant{{NoID: ledger.identity.NoID + 1, StateDir: dir, Stats: stats}}
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
		t.Fatalf("unpublished binding did not reach queued admission: %v", settlement.err)
	}
	release()
	if err := replay.join(); err != nil {
		t.Fatal(err)
	}
	if err := settlement.join(); err == nil || !strings.Contains(err.Error(), "differs from bound ledger") {
		t.Fatalf("queued participant did not recheck its newly published ledger identity: %v", err)
	}
	snapshot, _ := readStatsWriteSnapshotTest(t, dir)
	if snapshotWrites != 1 || settlementWrites != 0 || stats.attemptLedger != ledger || stats.attemptCutPending || stats.attemptSettlementCutPending || stats.attemptSettlementCutEpoch != 0 || snapshot.AttemptLastAppliedSequence != 8 {
		t.Fatal("wrong operator changed replayed state before its post-admission refusal")
	}
	if head, err := ledger.Head(); err != nil || head.LastSequence != 8 || head.Root != fixture.recordTs[7].RecordHash {
		t.Fatalf("queued wrong operator changed genuine attempt authority: %+v/%v", head, err)
	}
	entries, err := os.ReadDir(coordinator)
	if err != nil || len(entries) != 0 {
		t.Fatalf("queued wrong operator published a transaction: %v", err)
	}
}

// Initialize NoID2 before NoID1 to make immutable engine order the reverse of
// canonical wire order. Writes, signed membership and final epochs must still
// use the canonical operator sequence, not leak internal lock ordering.
func TestStatsMultiBatchEngineOrderPreservesCanonicalTransactionOrder(t *testing.T) {
	second, secondLedger := newAttemptSettlementTestParticipant(t, 2)
	first, firstLedger := newAttemptSettlementTestParticipant(t, 1)
	t.Cleanup(func() { _ = firstLedger.Close(); _ = secondLedger.Close() })
	participants := []AttemptSettlementParticipant{first, second}
	engineOrder, err := orderStatsEngineOwners(participants)
	if err != nil || len(engineOrder) != 2 || engineOrder[0].Stats != second.Stats || engineOrder[1].Stats != first.Stats {
		t.Fatalf("fixture did not establish reverse engine lifetime order: %v", err)
	}
	relabeledOrder, err := orderStatsEngineOwners([]AttemptSettlementParticipant{{NoID: 1, StateDir: second.StateDir, Stats: second.Stats}, {NoID: 2, StateDir: first.StateDir, Stats: first.Stats}})
	if err != nil || relabeledOrder[0].Stats != second.Stats || relabeledOrder[1].Stats != first.Stats {
		t.Fatalf("engine order changed under caller relabeling: %v", err)
	}
	var writes []string
	err = advanceAttemptSettlementEpochWithWrite(t.TempDir(), 43, attemptLedgerTestBoundary(), participants, func(path string, raw []byte) error {
		writes = append(writes, path)
		return atomicStateWrite(path, raw, 0o600)
	})
	if err != nil || len(writes) != 2 || writes[0] != filepath.Join(first.StateDir, "stats.json") || writes[1] != filepath.Join(second.StateDir, "stats.json") {
		t.Fatalf("engine acquisition order changed canonical snapshot order: %v/%v", writes, err)
	}
	for _, participant := range participants {
		transition := participant.Stats.settlementTransition
		if participant.Stats.settlementEpoch != 43 || transition == nil || len(transition.Batch) != 2 || transition.Batch[0].NoID != 1 || transition.Batch[1].NoID != 2 {
			t.Fatal("stable engine order changed signed canonical batch membership")
		}
	}
	if err := advanceReleaseSettlementSnapshotWithMode(context.Background(), t.TempDir(), &ReleaseSnapshot{Epoch: new(big.Int).SetUint64(43)}, []AttemptSettlementParticipant{second, first}, func(context.Context, *ReleaseSnapshot) (AttemptBoundary, error) {
		return AttemptBoundary{}, errors.New("unchanged canonical batch unexpectedly requested a boundary")
	}, false); err != nil {
		t.Fatalf("stable reader order could not read the completed canonical batch: %v", err)
	}
}
