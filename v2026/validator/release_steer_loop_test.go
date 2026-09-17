package validator

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestReleaseSteeringLoopRetriesIncompleteEpoch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	attempts := 0
	err := runReleaseSteeringLoop(ctx, time.Millisecond, func() (uint64, error) {
		return 7, nil
	}, func() error {
		attempts++
		if attempts == 1 {
			return errors.New("transient pre-intent failure")
		}
		cancel()
		return nil
	})
	if err != nil || attempts != 2 {
		t.Fatalf("retry loop attempts=%d err=%v", attempts, err)
	}
}

func TestReleaseSteeringLoopSuppressesCompletedDuplicate(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	epochReads, submissions := 0, 0
	err := runReleaseSteeringLoop(ctx, time.Millisecond, func() (uint64, error) {
		epochReads++
		if epochReads == 4 {
			cancel()
		}
		return 9, nil
	}, func() error {
		submissions++
		return nil
	})
	if err != nil || submissions != 1 {
		t.Fatalf("completed loop submissions=%d err=%v", submissions, err)
	}
}

func TestReleaseSteeringLoopFailsOnMissedOrPersistentlyBrokenEpoch(t *testing.T) {
	epochReads := 0
	err := runReleaseSteeringLoop(context.Background(), time.Nanosecond, func() (uint64, error) {
		epochReads++
		return uint64(11 + epochReads - 1), nil
	}, func() error {
		return errors.New("uncommitted")
	})
	if err == nil || !strings.Contains(err.Error(), "incomplete epoch") {
		t.Fatalf("missed epoch result = %v", err)
	}

	attempts := 0
	err = runReleaseSteeringLoop(context.Background(), time.Nanosecond, func() (uint64, error) {
		return 20, nil
	}, func() error {
		attempts++
		return errors.New("corrupt state")
	})
	if err == nil || attempts != releaseSteeringFailureLimit || !strings.Contains(err.Error(), "consecutive attempts") {
		t.Fatalf("persistent failure attempts=%d err=%v", attempts, err)
	}
}
