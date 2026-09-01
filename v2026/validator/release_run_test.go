// Deterministic tests for release-validator startup retry boundaries.
package validator

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	gethrpc "github.com/ethereum/go-ethereum/rpc"
)

// Only conditions that can recover without a configuration change are retried.
func TestTransientReleaseSnapshotErrorSeparatesCapacityFromContractFailure(t *testing.T) {
	transientErrors := []error{
		context.DeadlineExceeded,
		gethrpc.HTTPError{StatusCode: http.StatusTooManyRequests, Status: "429 Too Many Requests"},
		errors.New("rpc: upstream overloaded"),
		errors.New("read: connection reset by peer"),
	}
	for _, err := range transientErrors {
		if !transientReleaseSnapshotError(err) {
			t.Errorf("transient error was classified permanent: %v", err)
		}
	}
	for _, err := range []error{context.Canceled, errors.New("execution reverted: InvalidPolicy"), errors.New("abi: cannot unmarshal tuple")} {
		if transientReleaseSnapshotError(err) {
			t.Errorf("permanent error was classified transient: %v", err)
		}
	}
}

// A short public-provider outage must not force the supervisor to restart an
// otherwise valid validator process.
func TestInitialReleaseSnapshotRetriesTransientFailureWithoutRestart(t *testing.T) {
	want := &ReleaseSnapshot{}
	loads := 0
	waits := 0
	load := func(context.Context) (*ReleaseSnapshot, error) {
		loads++
		if loads < 3 {
			return nil, context.DeadlineExceeded
		}
		return want, nil
	}
	wait := func(_ context.Context, delay time.Duration) error {
		waits++
		if delay != releaseSnapshotStartupRetryDelay {
			t.Fatalf("retry delay = %s, want %s", delay, releaseSnapshotStartupRetryDelay)
		}
		return nil
	}
	got, err := loadInitialReleaseSnapshot(context.Background(), load, wait)
	if err != nil || got != want || loads != 3 || waits != 2 {
		t.Fatalf("snapshot=%p error=%v loads=%d waits=%d", got, err, loads, waits)
	}
}

// Contract and ABI failures remain fail-fast so retries cannot hide drift.
func TestInitialReleaseSnapshotFailsPermanentErrorImmediately(t *testing.T) {
	wantErr := errors.New("execution reverted: InvalidPolicy")
	loads := 0
	waits := 0
	_, err := loadInitialReleaseSnapshot(context.Background(), func(context.Context) (*ReleaseSnapshot, error) {
		loads++
		return nil, wantErr
	}, func(context.Context, time.Duration) error {
		waits++
		return nil
	})
	if !errors.Is(err, wantErr) || loads != 1 || waits != 0 {
		t.Fatalf("error=%v loads=%d waits=%d", err, loads, waits)
	}
}

// A provider that stays unavailable cannot leave startup retrying forever.
func TestInitialReleaseSnapshotBoundsPersistentTransientFailure(t *testing.T) {
	loads := 0
	waits := 0
	_, err := loadInitialReleaseSnapshot(context.Background(), func(context.Context) (*ReleaseSnapshot, error) {
		loads++
		return nil, context.DeadlineExceeded
	}, func(context.Context, time.Duration) error {
		waits++
		return nil
	})
	if err == nil || loads != releaseSnapshotStartupAttempts || waits != releaseSnapshotStartupAttempts-1 {
		t.Fatalf("error=%v loads=%d waits=%d", err, loads, waits)
	}
}

// Lifecycle cancellation wins before another retry delay or RPC attempt.
func TestInitialReleaseSnapshotStopsOnParentCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	loads := 0
	_, err := loadInitialReleaseSnapshot(ctx, func(context.Context) (*ReleaseSnapshot, error) {
		loads++
		return nil, context.DeadlineExceeded
	}, func(context.Context, time.Duration) error {
		t.Fatal("canceled startup waited for another attempt")
		return nil
	})
	if !errors.Is(err, context.Canceled) || loads != 1 {
		t.Fatalf("error=%v loads=%d", err, loads)
	}
}
