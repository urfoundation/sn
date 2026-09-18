//go:build linux || darwin

// Historical and later-audit Rpc failures keep transport-only retry identity.
package validator

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// A retained-decision Rpc deadline remains eligible for local steering retry.
func TestReleaseHistoricalDecisionRpcDeadlineRemainsRetryable(t *testing.T) {
	t.Parallel()
	mismatch := errors.New("historical decision signer or epoch differs from real chain observations")
	deadline := fmt.Errorf("eth_call batch at canonical block 424242: Post http://203.0.113.44:18545: %w", context.DeadlineExceeded)
	err := releaseRpcObservationError(deadline, false, mismatch)
	if err != deadline || !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, mismatch) || strings.Contains(err.Error(), mismatch.Error()) || !transientReleaseSnapshotError(err) {
		t.Fatalf("historical transport failure acquired a semantic verdict: %v", err)
	}
	reads, attempts := 0, 0
	err = runReleaseSteeringLoopWithWaitAndDeferral(t.Context(), func() (uint64, error) {
		reads++
		return uint64(1514 + reads), nil
	}, func() error {
		attempts++
		if attempts == 1 {
			return deadline
		}
		return nil
	}, func() bool { return reads < 2 }, true)
	if err != nil || reads != 2 || attempts != 2 {
		t.Fatalf("historical deadline consumed a validator restart: reads=%d attempts=%d error=%v", reads, attempts, err)
	}
}

// The later audit uses the same completed-observation rule as retained replay.
func TestReleaseDepositAuditRpcDeadlineRemainsRetryable(t *testing.T) {
	t.Parallel()
	mismatch := errors.New("deposit audit native observation differs from its actual decision")
	deadline := fmt.Errorf("later audit decision read: %w", context.DeadlineExceeded)
	err := releaseRpcObservationError(deadline, false, mismatch)
	if err != deadline || !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, mismatch) || strings.Contains(err.Error(), mismatch.Error()) || !transientReleaseSnapshotError(err) {
		t.Fatalf("deposit-audit transport failure acquired a semantic verdict: %v", err)
	}
}

// A completed identity mismatch still consumes the bounded fatal budget, while
// owner cancellation is returned without inventing an integrity verdict.
func TestReleaseRpcObservationCompletedMismatchRemainsFatal(t *testing.T) {
	t.Parallel()
	for _, mismatch := range []error{
		errors.New("historical decision signer or epoch differs from real chain observations"),
		errors.New("deposit audit native observation differs from its actual decision"),
		errors.New("decision native signer lacks real stake/permit authority"),
	} {
		err := releaseRpcObservationError(nil, false, mismatch)
		if err != mismatch || transientReleaseSnapshotError(err) {
			t.Errorf("completed mismatch lost its fatal identity: %v", err)
		}
	}
	mismatch := errors.New("historical decision signer or epoch differs from real chain observations")
	attempts := 0
	err := runReleaseSteeringLoopWithWaitAndDeferral(t.Context(), func() (uint64, error) { return 1516, nil }, func() error {
		attempts++
		return releaseRpcObservationError(nil, false, mismatch)
	}, func() bool { return true }, true)
	if !errors.Is(err, mismatch) || attempts != releaseSteeringFailureLimit {
		t.Fatalf("completed mismatch did not remain supervisor-visible: attempts=%d error=%v", attempts, err)
	}
	if err := releaseRpcObservationError(context.Canceled, false, errors.New("unused mismatch")); err != context.Canceled || transientReleaseSnapshotError(err) {
		t.Fatalf("cancellation did not remain prompt and transport-only: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	reads, submissions := 0, 0
	err = runReleaseSteeringLoopWithWaitAndDeferral(ctx, func() (uint64, error) {
		reads++
		return 1516, nil
	}, func() error {
		submissions++
		return nil
	}, func() bool { return true }, true)
	if err != nil || reads != 0 || submissions != 0 {
		t.Fatalf("canceled steering performed work: reads=%d submissions=%d error=%v", reads, submissions, err)
	}
}
