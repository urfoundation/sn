//go:build linux || darwin

package validator

import (
	"context"
	"errors"
	"testing"
	"time"
)

// The actual application reader selects before runtime admission. A retry
// retains that first selection even when the finalized endpoint advances.
func TestProductionApplicationRetainsOriginalFinalizedSelection(t *testing.T) {
	fixture := newProductionContinuationTestFixture(t)
	native := installProductionContinuationNative(t, fixture)
	fixture.production.head = 102
	original := fixture.production.block(102)
	native.currentReadError = context.DeadlineExceeded
	native.applied = true
	current := *fixture.intent
	current.Status, current.FinalizedBlock, current.RevealBlock = "finalized", 101, 1000
	call := native.validatorRuntimeIdentityTestClient.callContext
	runtimeReads := 0
	native.validatorRuntimeIdentityTestClient.callContext = func(ctx context.Context, target any, method string, args ...any) error {
		if method == "state_getRuntimeVersion" {
			runtimeReads++
			if args[0] != original.Hex() {
				t.Fatalf("application retry selected a later block: %v", args)
			}
		}
		return call(ctx, target, method, args...)
	}
	waits := 0
	fixture.steerer.productionReadHooks.wait = func(ctx context.Context, _ time.Duration) error {
		waits++
		if waits > 1 {
			return errors.New("unexpected extra application retry")
		}
		fixture.production.head, native.currentReadError = 103, nil
		return ctx.Err()
	}
	err := fixture.steerer.observeProductionApplicationV2(t.Context(), &current)
	var transition *productionSteeringTransition
	if !errors.As(err, &transition) || !transition.revealWait || waits != 1 || runtimeReads < 2 || len(native.broadcasts) != 0 || current.Prepared != fixture.intent.Prepared {
		t.Fatalf("application selection or retained work changed: waits=%d reads=%d error=%v", waits, runtimeReads, err)
	}
}
