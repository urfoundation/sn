//go:build linux || darwin

// A requested stop exits cleanly; only a hard cause is process-visible.
package validator

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// 2026-11-11 05:34:58: the deploy's stop arrived during startup and the
// cancellation exited the process with a panic (status 2), which reads as a
// crash. A dial canceled at shutdown carries net's own cancellation value,
// which the shutdown check refused as an opaque matcher. A requested stop is
// no failure; a hard cause joined to it still is.
func TestReleaseRequestedStopIsNotAFailure(t *testing.T) {
	canceled, failure := releaseTransientTestDialCanceled(t)
	if retryableProductionSteeringRead(failure) {
		t.Fatal("a cancellation authorized a retry")
	}
	if err := releaseRuntimeError(canceled, failure); err != nil {
		t.Fatalf("a canceled dial at shutdown became a failure: %v", err)
	}
	startup := fmt.Errorf("release V2 semantic startup: %w", errors.Join(fmt.Errorf("compact final directory witness synthetic/state: %w", context.Canceled), failure))
	wait := &productionSteeringReadWait{phase: productionReadPreparation, cause: releaseTransientTestRateLimit()}
	for _, stopped := range []error{failure, startup, errors.Join(wait, context.Canceled)} {
		if err := releaseProcessError(canceled, stopped); err != nil {
			t.Errorf("a requested stop exited as a failure: %v", err)
		}
	}
	hard := errors.New("synthetic durable intent write failed")
	if err := releaseProcessError(canceled, errors.Join(failure, hard)); !errors.Is(err, hard) {
		t.Fatalf("a hard cause joined to a stop was dropped: %v", err)
	}
	if err := releaseProcessError(t.Context(), wait); err == nil {
		t.Fatal("a release result outside shutdown was dropped")
	}
}
