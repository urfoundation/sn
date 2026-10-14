//go:build linux || darwin

package validator

// Caller-provided operator labels cannot order shared engine locks or grant
// authority to change a different ledger's admission state. All barriers and
// keys are private fixtures; cancellation joins a forced opposing owner.

import (
	"bytes"
	"context"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
)

// Pause the first batch after it owns the first engine but before it can queue
// for the held second engine. A relabeled batch must not acquire that second
// engine first. The old opposing order is observed, then canceled and joined
// before allowing the first batch forward; no actual deadlock is awaited.
func TestStatsMultiBatchRelabelCannotReverseEngineOwnership(t *testing.T) {
	first, second := NewStatsEngine(StatsConfig{}), NewStatsEngine(StatsConfig{})
	first.settlementEpoch, first.settlementEpochKnown = 42, true
	second.settlementEpoch, second.settlementEpochKnown = 42, true
	firstDir, secondDir := t.TempDir(), t.TempDir()
	if err := first.Save(firstDir); err != nil {
		t.Fatal(err)
	}
	if err := second.Save(secondDir); err != nil {
		t.Fatal(err)
	}
	_, firstBefore := readStatsWriteSnapshotTest(t, firstDir)
	_, secondBefore := readStatsWriteSnapshotTest(t, secondDir)
	firstBatchPaused, allowFirstBatch := make(chan struct{}), make(chan struct{})
	secondBatchWaitsFirst, secondBatchWaitsSecond := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	releaseFirst := func() { releaseOnce.Do(func() { close(allowFirstBatch) }) }
	var secondWaits atomic.Uint32
	first.writeHooks.step = func(operation, stage string) {
		if operation == "settlement" && stage == "waiting" {
			close(secondBatchWaitsFirst)
		}
	}
	second.writeHooks.step = func(operation, stage string) {
		if operation != "settlement" || stage != "waiting" {
			return
		}
		if secondWaits.Add(1) == 1 {
			close(firstBatchPaused)
			<-allowFirstBatch
		} else {
			close(secondBatchWaitsSecond)
		}
	}
	heldSecond := second.lockStatsWrite("test-second-engine")
	defer func() {
		if heldSecond != nil {
			heldSecond.release()
		}
		releaseFirst()
	}()
	normal := []AttemptSettlementParticipant{{NoID: 1, StateDir: firstDir, Stats: first}, {NoID: 2, StateDir: secondDir, Stats: second}}
	relabeled := []AttemptSettlementParticipant{{NoID: 1, StateDir: secondDir, Stats: second}, {NoID: 2, StateDir: firstDir, Stats: first}}
	firstCoordinator, secondCoordinator := t.TempDir(), t.TempDir()
	firstCall := startStatsWriteTestCall(func() error {
		return advanceAttemptSettlementEpochWithIOModeContext(context.Background(), firstCoordinator, 42, AttemptBoundary{}, normal, func(string, []byte) error {
			return errors.New("unchanged first batch unexpectedly wrote a snapshot")
		}, removeAttemptSettlementTransaction, false)
	})
	defer func() {
		if heldSecond != nil {
			heldSecond.release()
			heldSecond = nil
		}
		releaseFirst()
		_ = firstCall.join()
	}()
	select {
	case <-firstBatchPaused:
	case <-firstCall.done:
		t.Fatalf("first batch did not reach observed second-engine admission: %v", firstCall.err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	secondCall := startStatsWriteTestCall(func() error {
		return advanceAttemptSettlementEpochWithIOModeContext(ctx, secondCoordinator, 42, AttemptBoundary{}, relabeled, func(string, []byte) error {
			return errors.New("canceled second batch unexpectedly wrote a snapshot")
		}, removeAttemptSettlementTransaction, false)
	})
	defer func() { cancel(); _ = secondCall.join() }()
	oppositeOrder := false
	select {
	case <-secondBatchWaitsSecond:
		oppositeOrder = true
		heldSecond.release()
		heldSecond = nil
		select {
		case <-secondBatchWaitsFirst:
		case <-secondCall.done:
			t.Fatalf("opposing batch did not retain the second owner before requesting first: %v", secondCall.err)
		}
	case <-secondBatchWaitsFirst:
	case <-secondCall.done:
		t.Fatalf("relabeled batch bypassed shared engine admission: %v", secondCall.err)
	}
	if oppositeOrder {
		first.mu.Lock()
		firstOwner := first.writeOwner
		first.mu.Unlock()
		second.mu.Lock()
		secondOwner := second.writeOwner
		second.mu.Unlock()
		if firstOwner == nil || secondOwner == nil || firstOwner.operation != "settlement" || secondOwner.operation != "settlement" || firstOwner == secondOwner {
			t.Fatal("opposite-order probe did not establish both actual retained batch owners")
		}
	}
	cancel()
	if err := secondCall.join(); !errors.Is(err, context.Canceled) {
		t.Fatalf("opposing owner did not join at cancellation: %v", err)
	}
	if heldSecond != nil {
		heldSecond.release()
		heldSecond = nil
	}
	releaseFirst()
	if err := firstCall.join(); err != nil {
		t.Fatalf("first owner could not complete after joined cleanup: %v", err)
	}
	_, firstAfter := readStatsWriteSnapshotTest(t, firstDir)
	_, secondAfter := readStatsWriteSnapshotTest(t, secondDir)
	if !bytes.Equal(firstBefore, firstAfter) || !bytes.Equal(secondBefore, secondAfter) || first.writeOwner != nil || second.writeOwner != nil {
		t.Fatal("ordering probe changed snapshots or leaked ownership")
	}
	for _, directory := range []string{firstCoordinator, secondCoordinator} {
		entries, err := os.ReadDir(directory)
		if err != nil || len(entries) != 0 {
			t.Fatalf("unchanged ordering probe wrote settlement artifacts: %v", err)
		}
	}
	if oppositeOrder {
		t.Fatal("caller-controlled operator relabeling acquired Stats write owners in opposite engine order")
	}
}

// A genuine bound ledger authenticates its own operator id. A different
// participant label must fail before setting even an in-memory admission
// barrier, not merely at a later signed transition/batch consistency check.
func TestStatsMultiBatchWrongOperatorRefusesBeforeAdmissionMutation(t *testing.T) {
	participant, ledger := newAttemptSettlementTestParticipant(t, 9)
	t.Cleanup(func() { _ = ledger.Close() })
	_, before := readStatsWriteSnapshotTest(t, participant.StateDir)
	participant.NoID = 10
	coordinator := t.TempDir()
	writes := 0
	err := advanceAttemptSettlementEpochWithWrite(coordinator, 43, attemptLedgerTestBoundary(), []AttemptSettlementParticipant{participant}, func(string, []byte) error {
		writes++
		return nil
	})
	_, after := readStatsWriteSnapshotTest(t, participant.StateDir)
	entries, readErr := os.ReadDir(coordinator)
	if readErr != nil {
		t.Fatal(readErr)
	}
	stats := participant.Stats
	if err == nil || writes != 0 || len(entries) != 0 || !bytes.Equal(before, after) || stats.attemptCutPending || stats.attemptSettlementCutPending || stats.attemptSettlementCutEpoch != 0 || stats.settlementEpoch != 42 || stats.writeOwner != nil {
		t.Fatal("wrong participant operator identity changed Stats admission or durable state before refusal")
	}
}

// A paused transaction must not serialize an unrelated operator set. Both
// controls use real ledgers, terminal cuts, snapshots and closure publication.
func TestStatsMultiBatchIndependentSetsRetainParallelProgress(t *testing.T) {
	first, firstLedger := newAttemptSettlementTestParticipant(t, 1)
	second, secondLedger := newAttemptSettlementTestParticipant(t, 2)
	t.Cleanup(func() { _ = firstLedger.Close(); _ = secondLedger.Close() })
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	firstCoordinator, secondCoordinator := t.TempDir(), t.TempDir()
	firstCall := startStatsWriteTestCall(func() error {
		return advanceAttemptSettlementEpochWithWrite(firstCoordinator, 43, attemptLedgerTestBoundary(), []AttemptSettlementParticipant{first}, func(path string, raw []byte) error {
			close(entered)
			<-release
			return atomicStateWrite(path, raw, 0o600)
		})
	})
	defer func() { unblock(); _ = firstCall.join() }()
	select {
	case <-entered:
	case <-firstCall.done:
		t.Fatalf("first independent batch did not reach its real writer: %v", firstCall.err)
	}
	if err := AdvanceAttemptSettlementEpoch(secondCoordinator, 43, attemptLedgerTestBoundary(), []AttemptSettlementParticipant{second}); err != nil || second.Stats.settlementEpoch != 43 {
		t.Fatalf("independent operator set could not progress: %v", err)
	}
	unblock()
	if err := firstCall.join(); err != nil || first.Stats.settlementEpoch != 43 {
		t.Fatalf("first independent batch could not join: %v", err)
	}
}
