package validator

// Explicit barriers prove refresh independence and cancellation ownership;
// elapsed-time sleeps never provide the ordering assertion.

import (
	"context"
	"errors"
	"math/big"
	"testing"
	"time"
)

// A held production steering submission cannot starve the independent EVM owner.
func TestReleaseSettlementRefreshClosesWhileNativeSubmissionHeld(t *testing.T) {
	root := t.TempDir()
	participant, _ := newAttemptSettlementTestParticipant(t, 1)
	participants := []AttemptSettlementParticipant{participant}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	submitted := make(chan struct{})
	nativeDone := make(chan error, 1)
	go func() {
		nativeDone <- runReleaseSteeringLoop(ctx, time.Hour, func() (uint64, error) { return 7, nil }, func() error {
			close(submitted)
			<-ctx.Done()
			return ctx.Err()
		})
	}()
	<-submitted
	closed := make(chan struct{})
	refreshDone := make(chan error, 1)
	waits := 0
	go func() {
		refreshDone <- runReleaseSettlementRefresh(ctx, time.Hour,
			func(context.Context) (*ReleaseSnapshot, error) { return &ReleaseSnapshot{Epoch: big.NewInt(43)}, nil },
			func(ctx context.Context, snapshot *ReleaseSnapshot) error {
				return advanceReleaseSettlementSnapshot(ctx, root, snapshot, participants, func(context.Context, *ReleaseSnapshot) (AttemptBoundary, error) {
					return attemptLedgerTestBoundary(), nil
				})
			},
			func(*ReleaseSnapshot) { close(closed) },
			func(ctx context.Context, _ time.Duration) error {
				waits++
				if waits == 1 {
					return nil
				}
				<-ctx.Done()
				return ctx.Err()
			})
	}()
	<-closed
	if _, err := ReadAttemptSettlementClosure(root, 42); err != nil {
		t.Fatal(err)
	}
	select {
	case <-nativeDone:
		t.Fatal("native submit ended before terminal closure")
	default:
	}
	cancel()
	if err := <-refreshDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("refresh cancel: %v", err)
	}
	if err := <-nativeDone; err != nil {
		t.Fatalf("native join: %v", err)
	}
}

// A cancelled in-flight EVM read is joined before its owner returns.
func TestReleaseSettlementRefreshJoinsCanceledPoll(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered, released := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- runReleaseSettlementRefresh(ctx, time.Hour, func(ctx context.Context) (*ReleaseSnapshot, error) {
			close(entered)
			<-ctx.Done()
			<-released
			return nil, ctx.Err()
		}, func(context.Context, *ReleaseSnapshot) error { t.Error("cancelled read advanced"); return nil }, func(*ReleaseSnapshot) { t.Error("cancelled read published") }, func(context.Context, time.Duration) error { return nil })
	}()
	<-entered
	cancel()
	select {
	case <-done:
		t.Fatal("refresh returned before the owned read joined")
	default:
	}
	close(released)
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled poll: %v", err)
	}
}

// A native fold with an older snapshot cannot undo a newer independent advance.
func TestReleaseSettlementRefreshRejectsStaleConcurrentAdvance(t *testing.T) {
	root := t.TempDir()
	participant, _ := newAttemptSettlementTestParticipant(t, 1)
	participants := []AttemptSettlementParticipant{participant}
	entered, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- advanceReleaseSettlementSnapshot(context.Background(), root, &ReleaseSnapshot{Epoch: big.NewInt(43)}, participants, func(context.Context, *ReleaseSnapshot) (AttemptBoundary, error) {
			close(entered)
			<-release
			return attemptLedgerTestBoundary(), nil
		})
	}()
	<-entered
	if err := AdvanceAttemptSettlementEpoch(root, 43, attemptLedgerTestBoundary(), participants); err != nil {
		t.Fatal(err)
	}
	next := attemptLedgerTestBoundary()
	next.SettlementEpoch = 43
	next.EVMBlock++
	if err := AdvanceAttemptSettlementEpoch(root, 44, next, participants); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; !errors.Is(err, errAttemptSettlementSnapshotStale) {
		t.Fatalf("old native snapshot: %v", err)
	}
	if participant.Stats.settlementEpoch != 44 || participant.Stats.attemptCutPending {
		t.Fatal("stale fold changed epoch or admission")
	}
}

// Active barriers retry, while invalid boundary/domain errors are supervised.
func TestReleaseSettlementRefreshRetriesOnlyTransientFailures(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	permanent := errors.New("permanent signed closure failure")
	loads, advances, published := 0, 0, 0
	err := runReleaseSettlementRefresh(ctx, time.Hour, func(context.Context) (*ReleaseSnapshot, error) {
		loads++
		return &ReleaseSnapshot{Epoch: big.NewInt(43)}, nil
	}, func(context.Context, *ReleaseSnapshot) error {
		advances++
		if advances == 1 {
			return errAttemptCutPending
		}
		return permanent
	}, func(*ReleaseSnapshot) { published++ }, func(context.Context, time.Duration) error { return nil })
	if !errors.Is(err, permanent) || loads != 2 || advances != 2 || published != 0 {
		t.Fatalf("refresh failure lifecycle loads=%d advances=%d published=%d err=%v", loads, advances, published, err)
	}
}

// An unchanged refresh cannot reopen a detach/transition barrier it does not own.
func TestReleaseSettlementRefreshPreservesCurrentDetachBarrier(t *testing.T) {
	participant, ledger := newAttemptSettlementTestParticipant(t, 1)
	if err := participant.Stats.beginAttempt(42, ledger); err != nil {
		t.Fatal(err)
	}
	participant.Stats.mu.Lock()
	participant.Stats.attemptCutPending = true
	participant.Stats.mu.Unlock()
	err := advanceReleaseSettlementSnapshotWithMode(context.Background(), t.TempDir(), &ReleaseSnapshot{Epoch: big.NewInt(42)}, []AttemptSettlementParticipant{participant}, func(context.Context, *ReleaseSnapshot) (AttemptBoundary, error) {
		t.Error("same epoch resolved a terminal block")
		return AttemptBoundary{}, nil
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !participant.Stats.attemptCutPending || participant.Stats.activeAttemptCount != 1 {
		t.Fatal("unchanged refresh mutated the ordinary cut barrier")
	}
	participant.Stats.abortAttempt()
}

// A native advance may win after refresh resolves its boundary but before the
// atomic primitive is entered; its subsequent ordinary cut still owns the gate.
func TestReleaseSettlementRefreshRacingNativeAdvancePreservesCut(t *testing.T) {
	root := t.TempDir()
	participant, ledger := newAttemptSettlementTestParticipant(t, 1)
	participants := []AttemptSettlementParticipant{participant}
	entered, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- advanceReleaseSettlementSnapshotWithMode(context.Background(), root, &ReleaseSnapshot{Epoch: big.NewInt(43)}, participants, func(context.Context, *ReleaseSnapshot) (AttemptBoundary, error) {
			close(entered)
			<-release
			return attemptLedgerTestBoundary(), nil
		}, false)
	}()
	<-entered
	if err := AdvanceAttemptSettlementEpoch(root, 43, attemptLedgerTestBoundary(), participants); err != nil {
		t.Fatal(err)
	}
	if err := participant.Stats.beginAttempt(43, ledger); err != nil {
		t.Fatal(err)
	}
	boundary := attemptLedgerTestBoundary()
	boundary.SettlementEpoch = 43
	if _, err := participant.Stats.detachReleaseStatsMeasurementWithAttemptCut(participant.StateDir, boundary, func(ReleaseStatsMeasurement, uint64) error { return nil }); !errors.Is(err, errAttemptCutPending) {
		t.Fatalf("native cut barrier: %v", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := participant.Stats.beginAttempt(43, ledger); !errors.Is(err, errAttemptCutPending) {
		t.Fatalf("racing refresh reopened native cut: %v", err)
	}
	participant.Stats.abortAttempt()
}
