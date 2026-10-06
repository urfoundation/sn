//go:build linux || darwin

// Bootstrap retries observe absolute deadlines while cancellation is queued;
// explicit read and wait transitions exercise the existing retry owner.
package validator

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"testing"
	"time"
)

// Caller and operation expiry refuse new work and late results without losing
// completed transport, integrity or close failures before cancellation arrives.
func TestProductionBootstrapPrefixReadRefusesElapsedOwnerBeforeCancellation(t *testing.T) {
	for _, scope := range []string{"caller", "operation"} {
		for _, boundary := range []string{"before", "attempt", "success", "transport", "hard", "wait", "wait-error"} {
			caller := &evidenceReadPendingDeadlineTestContext{Context: t.Context(), deadline: time.Now().Add(time.Hour)}
			operation := &evidenceReadPendingDeadlineTestContext{Context: caller, deadline: caller.deadline}
			owner := operation
			if scope == "caller" {
				owner = caller
			}
			if boundary == "before" {
				owner.deadline = time.Unix(1, 0)
			}
			calls, waits, attempts, attemptCloses, operationCloses := 0, 0, 0, 0, 0
			status := &releaseHttpGetStatusError{status: http.StatusServiceUnavailable}
			integrity := errors.New("synthetic bootstrap integrity failure")
			closeErr := errors.New("synthetic bootstrap close failure")
			waitErr := errors.New("synthetic bootstrap wait failure")
			hooks := releaseHttpGetRetryHooks{
				withTimeout: func(ctx context.Context, duration time.Duration) (context.Context, context.CancelFunc) {
					if duration == 300*time.Second {
						if ctx != caller {
							t.Fatal("operation replaced its caller")
						}
						return operation, func() { operationCloses++ }
					}
					if duration != 60*time.Second || ctx != operation {
						t.Fatal("attempt changed its owner or duration", duration)
					}
					attempts++
					if boundary == "attempt" {
						owner.deadline = time.Unix(1, 0)
					}
					return &evidenceReadPendingDeadlineTestContext{Context: ctx, deadline: time.Now().Add(duration)}, func() { attemptCloses++ }
				},
				wait: func(ctx context.Context, duration time.Duration) error {
					waits++
					if (boundary != "wait" && boundary != "wait-error") || waits != 1 || ctx != operation || duration != releaseSnapshotStartupRetryDelay || attemptCloses != calls {
						t.Fatal("expired owner waited or retry lost its closed attempt", scope, boundary, waits)
					}
					owner.deadline = time.Unix(1, 0)
					if boundary == "wait-error" {
						return waitErr
					}
					return nil
				},
			}
			err := retryProductionBootstrapPrefixRead(caller, func(context.Context) error {
				calls++
				if calls > 1 {
					t.Fatal("elapsed owner acquired another read", scope, boundary)
				}
				if boundary != "wait" && boundary != "wait-error" {
					owner.deadline = time.Unix(1, 0)
				}
				if boundary == "success" {
					return nil
				}
				if boundary == "hard" {
					return errors.Join(status, integrity, closeErr)
				}
				return status
			}, hooks)
			wantCalls, wantAttempts, wantWaits := 1, 1, 0
			if boundary == "before" {
				wantCalls, wantAttempts = 0, 0
			} else if boundary == "attempt" {
				wantCalls = 0
			} else if boundary == "wait" || boundary == "wait-error" {
				wantWaits = 1
			}
			if owner.Err() != nil || !errors.Is(err, context.DeadlineExceeded) || calls != wantCalls || waits != wantWaits || attempts != wantAttempts || attemptCloses != attempts || operationCloses != 1 {
				t.Fatalf("%s/%s lost deadline or ownership: calls=%d waits=%d attempts=%d closes=%d/%d error=%v", scope, boundary, calls, waits, attempts, attemptCloses, operationCloses, err)
			}
			if (boundary == "transport" || boundary == "hard" || boundary == "wait" || boundary == "wait-error") && !errors.Is(err, status) || boundary == "hard" && (!errors.Is(err, integrity) || !errors.Is(err, closeErr) || retryableProductionSteeringRead(err)) || boundary == "wait-error" && !errors.Is(err, waitErr) {
				t.Fatal("elapsed owner lost a completed cause", scope, boundary, err)
			}
		}
	}
}

// An expired attempt cannot start work or publish success. A pure attempt
// timeout may recover inside the unchanged owner, but mixed failures cannot.
func TestProductionBootstrapPrefixReadRefusesElapsedAttemptBeforeCancellation(t *testing.T) {
	for _, boundary := range []string{"before", "success", "hard"} {
		var expired *evidenceReadPendingDeadlineTestContext
		var durations []time.Duration
		calls, waits, attemptCloses, operationCloses := 0, 0, 0, 0
		status := &releaseHttpGetStatusError{status: http.StatusServiceUnavailable}
		integrity := errors.New("synthetic bootstrap integrity failure")
		closeErr := errors.New("synthetic bootstrap close failure")
		hooks := releaseHttpGetRetryHooks{
			withTimeout: func(ctx context.Context, duration time.Duration) (context.Context, context.CancelFunc) {
				durations = append(durations, duration)
				owner := &evidenceReadPendingDeadlineTestContext{Context: ctx, deadline: time.Now().Add(duration)}
				if duration == 300*time.Second {
					return owner, func() { operationCloses++ }
				}
				if duration != 60*time.Second {
					t.Fatal("attempt duration changed", duration)
				}
				if expired == nil {
					expired = owner
					if boundary == "before" {
						expired.deadline = time.Unix(1, 0)
					}
				}
				return owner, func() { attemptCloses++ }
			},
			wait: func(ctx context.Context, duration time.Duration) error {
				waits++
				if boundary == "hard" || waits != 1 || attemptCloses != 1 || duration != releaseSnapshotStartupRetryDelay || evidenceReadContextError(ctx) != nil {
					t.Fatal("attempt retried a hard failure or lost its live owner", boundary, waits)
				}
				return nil
			},
		}
		err := retryProductionBootstrapPrefixRead(t.Context(), func(attempt context.Context) error {
			calls++
			if boundary == "before" && attempt == expired {
				t.Fatal("elapsed attempt acquired a read")
			}
			if calls > 2 {
				t.Fatal("attempt timeout did not recover")
			}
			if calls == 1 {
				expired.deadline = time.Unix(1, 0)
				if boundary == "hard" {
					return errors.Join(status, integrity, closeErr)
				}
			}
			return nil
		}, hooks)
		wantCalls, wantWaits := 1, 0
		wantDurations := []time.Duration{300 * time.Second, 60 * time.Second}
		if boundary == "before" {
			wantWaits = 1
			wantDurations = append(wantDurations, 60*time.Second)
		} else if boundary == "success" {
			wantCalls, wantWaits = 2, 1
			wantDurations = append(wantDurations, 60*time.Second)
		}
		if expired == nil || expired.Err() != nil || calls != wantCalls || waits != wantWaits || operationCloses != 1 || attemptCloses != len(wantDurations)-1 || !reflect.DeepEqual(durations, wantDurations) {
			t.Fatalf("%s lost attempt admission or cleanup: calls=%d waits=%d closes=%d/%d durations=%v error=%v", boundary, calls, waits, attemptCloses, operationCloses, durations, err)
		}
		if boundary != "hard" && err != nil || boundary == "hard" && (!errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, status) || !errors.Is(err, integrity) || !errors.Is(err, closeErr) || retryableProductionSteeringRead(err)) {
			t.Fatal("attempt deadline replaced a completed cause or admitted late success", boundary, err)
		}
	}
}

// Real timeout construction keeps an earlier caller deadline on every owner.
func TestProductionBootstrapPrefixReadKeepsEarlierCallerDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	want, _ := ctx.Deadline()
	calls := 0
	var durations []time.Duration
	err := retryProductionBootstrapPrefixRead(ctx, func(attempt context.Context) error {
		calls++
		if deadline, bounded := attempt.Deadline(); !bounded || !deadline.Equal(want) {
			t.Fatal("attempt extended its caller deadline", deadline, want)
		}
		return nil
	}, releaseHttpGetRetryHooks{withTimeout: func(parent context.Context, duration time.Duration) (context.Context, context.CancelFunc) {
		durations = append(durations, duration)
		child, stop := context.WithTimeout(parent, duration)
		if deadline, bounded := child.Deadline(); !bounded || !deadline.Equal(want) {
			stop()
			t.Fatal("read owner extended its caller deadline", deadline, want)
		}
		return child, stop
	}})
	if err != nil || calls != 1 || !reflect.DeepEqual(durations, []time.Duration{300 * time.Second, 60 * time.Second}) {
		t.Fatal("earlier caller changed the read budgets", calls, durations, err)
	}
}
