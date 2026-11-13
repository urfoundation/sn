//go:build linux || darwin

// The exact operator proof and coordinator census share one read owner even
// when an actual HTTP refusal interrupts the first original route check.
package validator

import (
	"context"
	"testing"
	"time"
)

// The complete original proof and census finish after one physical refusal.
func TestOwnerRecycleOperatorsShareWholeReadBudget(t *testing.T) {
	for _, parentLimit := range []time.Duration{0, 45 * time.Second} {
		f := newRecycleOperatorFixture(t)
		f.transientFirst = true
		ctx, recorder, cancel := newRuntimeReadBudgetRecorder(t, parentLimit)
		owners, waits := 0, 0
		f.chain.readRetryHooks.withTimeout = func(ctx context.Context, duration time.Duration) (context.Context, context.CancelFunc) {
			owners++
			if duration != 300*time.Second {
				t.Fatal("operator proof read changed its operation budget")
			}
			return context.WithTimeout(ctx, duration)
		}
		f.chain.readRetryHooks.withAttemptTimeout = func(ctx context.Context, duration time.Duration) (context.Context, context.CancelFunc) {
			recorder.observe(ctx)
			if duration != 60*time.Second {
				t.Fatal("operator coordinator read changed its attempt budget")
			}
			return context.WithTimeout(ctx, duration)
		}
		f.chain.readRetryHooks.wait = func(ctx context.Context, _ time.Duration) error {
			waits++
			return ctx.Err()
		}
		result, err := ObserveOwnerRecycleMeasurementOperators(ctx, f.measurement.authority, f.chain, f.measurement.encoded, f.measurement.provider.options(t))
		cancel()
		recorder.completed(err)
		if result == nil || owners != 1 || waits != 1 {
			t.Fatalf("operator proof or retry renewed the original owner: owners=%d waits=%d", owners, waits)
		}
	}
}
