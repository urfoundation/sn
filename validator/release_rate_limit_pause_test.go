//go:build linux || darwin

// After an upstream refuses for load, the next attempt waits for its
// Retry-After, or a default pause when a 429 names none. Pacing never changes
// whether an outcome is a wait or a failure.
package validator

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/urnetwork/connect"
)

func TestReleaseRateLimitPauseHonorsServerHints(t *testing.T) {
	settings := DefaultReleaseSteeringSettings()
	if settings.RateLimitPause <= 0 || settings.RetryAfterLimit < settings.RateLimitPause || settings.RetryAfterLimit >= settings.OperationTimeout {
		t.Fatalf("default pacing does not fit inside one steering operation: %+v", settings)
	}
	cases := []struct {
		name string
		err  error
		want time.Duration
	}{
		{name: "no error", want: 0},
		{name: "hard", err: errors.New("synthetic canonical commitment differs"), want: 0},
		{name: "deadline", err: context.DeadlineExceeded, want: 0},
		{name: "load balancer 429", err: releaseTransientTestRateLimit(), want: settings.RateLimitPause},
		{name: "client-key 429 hint", err: &clientKeyObservationHttpStatusError{status: 429, operation: "batch", retryAfter: 45 * time.Second}, want: 45 * time.Second},
		{name: "artifact 429 long hint", err: &releaseHttpGetStatusError{endpoint: "https://api.example/sn/attempt-artifact", status: 429, retryAfter: time.Hour}, want: settings.RetryAfterLimit},
		{name: "upload 503 hint", err: &attemptStreamHttpStatusError{status: 503, upload: true, retryAfter: 20 * time.Second}, want: 20 * time.Second},
		{name: "503 without hint", err: &connect.HttpStatusError{StatusCode: 503, Status: "503 Service Unavailable"}, want: 0},
		{name: "500", err: &connect.HttpStatusError{StatusCode: 500, Status: "500 Internal Server Error"}, want: 0},
		{name: "longest joined hint", err: errors.Join(&clientKeyObservationHttpStatusError{status: 429, operation: "batch", retryAfter: 10 * time.Second}, &attemptStreamHttpStatusError{status: 429, retryAfter: 70 * time.Second}), want: 70 * time.Second},
		{name: "frozen read wait", err: productionPreparationFailure(t.Context(), fmt.Errorf("compact live head client key read for no_id 1: %w", releaseTransientTestRateLimit())), want: settings.RateLimitPause},
	}
	for _, item := range cases {
		if got := releaseRateLimitPause(item.err, settings); got != item.want {
			t.Errorf("%s: pause %s, want %s", item.name, got, item.want)
		}
	}
	quiet := ReleaseSteeringSettings{OperationTimeout: settings.OperationTimeout}
	if releaseRateLimitPause(releaseTransientTestRateLimit(), quiet) != 0 || releaseRateLimitPause(&clientKeyObservationHttpStatusError{status: 429, operation: "batch", retryAfter: 45 * time.Second}, quiet) != 0 {
		t.Fatal("non-positive settings still paused")
	}
}

// 2026-11-15: after each 429, the next steering attempt followed at the
// 2-second poll and kept the load balancer's limit tripped. The attempt now
// waits out the pause before the loop sees its unchanged outcome.
func TestProductionSteeringAttemptWaitsOutRateLimitPause(t *testing.T) {
	settings := DefaultReleaseSteeringSettings()
	var waits []time.Duration
	wait := func(_ context.Context, delay time.Duration) error {
		waits = append(waits, delay)
		return nil
	}
	limited := productionPreparationFailure(t.Context(), fmt.Errorf("compact live head client census observation read: %w", &clientKeyObservationHttpStatusError{status: 429, operation: "batch"}))
	var readWait *productionSteeringReadWait
	if !errors.As(limited, &readWait) {
		t.Fatalf("a rate-limited preparation was not a read wait: %v", limited)
	}
	if err := paceReleaseSteeringAttempt(t.Context(), limited, settings, wait); err != limited || !slices.Equal(waits, []time.Duration{settings.RateLimitPause}) {
		t.Fatalf("rate-limited attempt was not paced: waits=%v error=%v", waits, err)
	}
	waits = nil
	timeout := productionPreparationFailure(t.Context(), releaseTransientTestLookupTimeout(t))
	for _, other := range []error{nil, ErrSteeringAlreadyFinal, timeout, errors.New("synthetic hard cause")} {
		if err := paceReleaseSteeringAttempt(t.Context(), other, settings, wait); err != other {
			t.Fatalf("pacing changed an outcome: %v -> %v", other, err)
		}
	}
	if len(waits) != 0 {
		t.Fatalf("an attempt with no load refusal was paced: %v", waits)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	err := paceReleaseSteeringAttempt(canceled, limited, settings, waitReleaseSnapshotRetry)
	if !errors.Is(err, context.Canceled) || !errors.As(err, &readWait) {
		t.Fatalf("a pause cut short by shutdown lost the outcome or the cancellation: %v", err)
	}
	// The service stops during the pause: the loop ends, and the process
	// treats it as the requested stop it is.
	stopping, stop := context.WithCancel(t.Context())
	defer stop()
	progress, _ := newReleaseProgressTest(t)
	attempts := 0
	err = runReleaseProductionSteeringLoopWithWait(stopping, func() error {
		attempts++
		return paceReleaseSteeringAttempt(stopping, limited, settings, func(ctx context.Context, _ time.Duration) error {
			stop()
			return ctx.Err()
		})
	}, func() bool { return true }, progress)
	if attempts != 1 || releaseProcessError(stopping, err) != nil {
		t.Fatalf("a stop during a pause was a failure: attempts=%d error=%v", attempts, err)
	}
}

// Retries inside one bounded steering read honor the server's pace too, and
// keep the ordinary delay otherwise.
func TestProductionReadRetryHonorsRetryAfter(t *testing.T) {
	self := &ReleaseSteerer{cfg: &ReleaseConfig{SchemaVersion: ReleaseMainnetProductionSchemaVersion}}
	var delays []time.Duration
	self.productionReadHooks.wait = func(_ context.Context, delay time.Duration) error {
		delays = append(delays, delay)
		return nil
	}
	reads := 0
	err := self.productionRead(t.Context(), productionReadPreparation, nil, func(context.Context) error {
		reads++
		switch reads {
		case 1:
			return &releaseHttpGetStatusError{endpoint: "https://api.example/sn/attempt-artifact", status: 429, retryAfter: 40 * time.Second}
		case 2:
			return releaseTransientTestLookupTimeout(t)
		}
		return nil
	})
	if err != nil || reads != 3 || !slices.Equal(delays, []time.Duration{40 * time.Second, releaseSnapshotStartupRetryDelay}) {
		t.Fatalf("bounded read retry pace: reads=%d delays=%v error=%v", reads, delays, err)
	}
}
