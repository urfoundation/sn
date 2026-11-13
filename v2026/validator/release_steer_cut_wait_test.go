package validator

// Expected attempt draining is paced lifecycle work, not a failed native
// submission. Force its poll ordering without measuring elapsed wall time.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// An actual active attempt may legitimately need more polls than the generic
// failure limit before the exact ordinary cut can be durably detached.
func TestReleaseSteeringLoopDrainsAdmittedAttemptBeyondFailureLimit(t *testing.T) {
	participant, ledger := newAttemptSettlementTestParticipant(t, 1)
	if err := participant.Stats.beginAttempt(42, ledger); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if participant.Stats.activeAttemptCount != 0 {
			participant.Stats.abortAttempt()
		}
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	attempts, persisted := 0, 0
	err := runReleaseSteeringLoop(ctx, time.Nanosecond, func() (uint64, error) { return 7, nil }, func() error {
		attempts++
		if attempts == releaseSteeringFailureLimit+2 {
			participant.Stats.abortAttempt()
		}
		_, err := participant.Stats.detachReleaseStatsMeasurementWithAttemptCut(participant.StateDir, attemptLedgerTestBoundary(), func(ReleaseStatsMeasurement, uint64) error { persisted++; return nil })
		if err != nil {
			return fmt.Errorf("gather exact native cut: %w", err)
		}
		cancel()
		return nil
	})
	if err != nil || attempts != releaseSteeringFailureLimit+2 || persisted != 1 {
		t.Fatalf("legitimate cut drain consumed native failure budget: attempts=%d persisted=%d error=%v", attempts, persisted, err)
	}
}

// Pending polls neither spend nor reset the previous real-failure budget.
func TestReleaseSteeringLoopPendingCutPreservesRealFailureCount(t *testing.T) {
	attempts := 0
	broken := errors.New("permanent native submission failure")
	const pending = 11
	err := runReleaseSteeringLoop(context.Background(), time.Nanosecond, func() (uint64, error) { return 7, nil }, func() error {
		attempts++
		if attempts >= releaseSteeringFailureLimit && attempts < releaseSteeringFailureLimit+pending {
			return fmt.Errorf("draining: %w", errAttemptCutPending)
		}
		return broken
	})
	if !errors.Is(err, broken) || attempts != releaseSteeringFailureLimit+pending || !strings.Contains(err.Error(), "consecutive attempts") {
		t.Fatalf("pending wait changed prior real failure budget: attempts=%d error=%v", attempts, err)
	}
}

// Cancellation before entry, during the scheduler read, or alongside an
// already-ready poll cannot admit another scheduler read or native submission.
func TestReleaseSteeringLoopPendingCutHonorsCancellation(t *testing.T) {
	for _, test := range []struct {
		boundary string
		reads    int
		attempts int
		waits    int
	}{
		{boundary: "entry", reads: 0, attempts: 0, waits: 0},
		{boundary: "scheduler", reads: 1, attempts: 0, waits: 0},
		{boundary: "ready-poll", reads: 11, attempts: 11, waits: 11},
	} {
		ctx, cancel := context.WithCancel(context.Background())
		reads, attempts, waits := 0, 0, 0
		if test.boundary == "entry" {
			cancel()
		}
		err := runReleaseSteeringLoopWithWait(ctx, func() (uint64, error) {
			reads++
			if test.boundary == "scheduler" {
				cancel()
			}
			return 7, nil
		}, func() error {
			attempts++
			if test.boundary == "ready-poll" && attempts == 11 {
				cancel()
			}
			return errAttemptCutPending
		}, func() bool {
			waits++
			// Force the ready-poll outcome even after cancellation. The
			// final false bounds the pre-fix failure, never its assertion.
			return test.boundary == "ready-poll" && waits <= 11
		})
		cancel()
		if err != nil || reads != test.reads || attempts != test.attempts || waits != test.waits {
			t.Errorf("canceled %s admitted extra steering work: reads=%d attempts=%d waits=%d error=%v", test.boundary, reads, attempts, waits, err)
		}
	}
}

// A drain cannot silently carry an unfinished intent across a native epoch.
func TestReleaseSteeringLoopPendingCutCannotSkipNativeEpoch(t *testing.T) {
	reads, attempts := 0, 0
	err := runReleaseSteeringLoop(context.Background(), time.Nanosecond, func() (uint64, error) { reads++; return uint64(6 + reads), nil }, func() error { attempts++; return errAttemptCutPending })
	if err == nil || !strings.Contains(err.Error(), "incomplete epoch") || reads != 2 || attempts != 1 {
		t.Fatalf("pending cut weakened native gap guard: reads=%d attempts=%d error=%v", reads, attempts, err)
	}
}
