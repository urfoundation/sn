// Read-only native scheduler interruptions preserve the current submission
// state. Explicit poll steps prove recovery without wall-clock timing.
package validator

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// Even a strict owner can retry a pure scheduler read: it neither broadcasts
// nor relaxes any native epoch, intent, or evidence acceptance requirement.
func TestReleaseSchedulerRetriesTransportBeyondFailureLimit(t *testing.T) {
	t.Parallel()
	reads, submissions := 0, 0
	err := runReleaseSteeringLoopWithWait(t.Context(), func() (uint64, error) {
		reads++
		if reads <= releaseSteeringFailureLimit+2 {
			return 0, fmt.Errorf("native finalized head: %w", context.DeadlineExceeded)
		}
		return 11, nil
	}, func() error { submissions++; return nil }, func() bool { return submissions == 0 })
	if err != nil || reads != releaseSteeringFailureLimit+3 || submissions != 1 {
		t.Fatalf("scheduler transport outage killed the runtime: reads=%d submissions=%d error=%v", reads, submissions, err)
	}
}

// A recovered scheduler cannot turn a completed epoch into another submit.
func TestReleaseSchedulerRetryPreservesCompletedEpoch(t *testing.T) {
	t.Parallel()
	reads, submissions := 0, 0
	finalRead := releaseSteeringFailureLimit + 4
	err := runReleaseSteeringLoopWithWait(t.Context(), func() (uint64, error) {
		reads++
		if reads > 1 && reads < finalRead {
			return 0, context.DeadlineExceeded
		}
		return 11, nil
	}, func() error { submissions++; return nil }, func() bool { return reads < finalRead })
	if err != nil || reads != finalRead || submissions != 1 {
		t.Fatalf("scheduler retry lost completed-epoch ownership: reads=%d submissions=%d error=%v", reads, submissions, err)
	}
}

// An outage must preserve the narrow provisional permission to continue an
// already pending attempt cut when the next native epoch becomes readable.
func TestReleaseSchedulerRetryPreservesPendingCutContinuation(t *testing.T) {
	t.Parallel()
	reads, submissions := 0, 0
	err := runReleaseSteeringLoopWithWaitAndDeferral(t.Context(), func() (uint64, error) {
		reads++
		if reads == 1 {
			return 11, nil
		}
		if reads <= releaseSteeringFailureLimit+2 {
			return 0, context.DeadlineExceeded
		}
		return 12, nil
	}, func() error {
		submissions++
		if submissions == 1 {
			return errAttemptCutPending
		}
		return nil
	}, func() bool { return submissions < 2 }, true)
	if err != nil || reads != releaseSteeringFailureLimit+3 || submissions != 2 {
		t.Fatalf("scheduler retry discarded pending cut: reads=%d submissions=%d error=%v", reads, submissions, err)
	}
}

// A timeout cannot hide malformed evidence, local custody errors, diagnostic
// text or any independent hard cause carried by the same scheduler result.
func TestReleaseSchedulerRetryRejectsMixedIntegrityFailure(t *testing.T) {
	t.Parallel()
	integrityErr := errors.New("native metadata differs from authenticated runtime")
	for _, cause := range []error{errors.Join(context.DeadlineExceeded, integrityErr), errors.New("context deadline exceeded"), integrityErr} {
		reads, submissions := 0, 0
		err := runReleaseSteeringLoopWithWait(t.Context(), func() (uint64, error) { reads++; return 0, cause }, func() error { submissions++; return nil }, func() bool { return reads < releaseSteeringFailureLimit+2 })
		if !errors.Is(err, cause) || reads != releaseSteeringFailureLimit || submissions != 0 {
			t.Fatalf("nontransport failure escaped its budget: cause=%v reads=%d submissions=%d error=%v", cause, reads, submissions, err)
		}
	}
}

// Pure transport retry neither discards a failed submission nor grants it
// permission to skip the pending epoch once the endpoint recovers.
func TestReleaseSchedulerRetryPreservesUnresolvedSubmission(t *testing.T) {
	t.Parallel()
	pendingErr := errors.New("native intent durability failed")
	reads, submissions := 0, 0
	err := runReleaseSteeringLoopWithWaitAndDeferral(t.Context(), func() (uint64, error) {
		reads++
		if reads == 1 {
			return 11, nil
		}
		if reads <= releaseSteeringFailureLimit+2 {
			return 0, context.DeadlineExceeded
		}
		return 12, nil
	}, func() error { submissions++; return pendingErr }, func() bool { return reads < releaseSteeringFailureLimit+4 }, true)
	if !errors.Is(err, pendingErr) || !strings.Contains(err.Error(), "incomplete epoch") || reads != releaseSteeringFailureLimit+3 || submissions != 1 {
		t.Fatalf("outage erased a pending submission: reads=%d submissions=%d error=%v", reads, submissions, err)
	}
}

// Owner cancellation stops the next scheduler read. Pure deadline/cancel
// errors retain the runtime's established graceful-shutdown contract.
func TestReleaseSchedulerRetryHonorsOwnerCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	reads, submissions := 0, 0
	err := runReleaseSteeringLoopWithWait(ctx, func() (uint64, error) { reads++; return 0, context.DeadlineExceeded }, func() error { submissions++; return nil }, func() bool { cancel(); return true })
	if err != nil || reads != 1 || submissions != 0 {
		t.Fatalf("cancellation started more scheduler work or failed graceful shutdown: reads=%d submissions=%d error=%v", reads, submissions, err)
	}
}

// Ending an observation without owner cancellation retains the last transport
// failure, so an unfinished scheduler cannot look like completed native work.
func TestReleaseSchedulerRetryRetainsUnresolvedTransport(t *testing.T) {
	t.Parallel()
	reads, submissions := 0, 0
	err := runReleaseSteeringLoopWithWait(t.Context(), func() (uint64, error) { reads++; return 0, context.DeadlineExceeded }, func() error { submissions++; return nil }, func() bool { return false })
	if !errors.Is(err, context.DeadlineExceeded) || reads != 1 || submissions != 0 {
		t.Fatalf("unfinished scheduler lost its transport error: reads=%d submissions=%d error=%v", reads, submissions, err)
	}
}

// A valid transport recovery still has to supply a monotonic native epoch.
func TestReleaseSchedulerRetryRejectsEpochRegression(t *testing.T) {
	t.Parallel()
	reads, submissions := 0, 0
	err := runReleaseSteeringLoopWithWait(t.Context(), func() (uint64, error) {
		reads++
		switch reads {
		case 1:
			return 11, nil
		case 2:
			return 0, context.DeadlineExceeded
		default:
			return 10, nil
		}
	}, func() error { submissions++; return nil }, func() bool { return reads < 4 })
	if err == nil || !strings.Contains(err.Error(), "epoch regressed") || reads != 3 || submissions != 1 {
		t.Fatalf("transport recovery bypassed the epoch guard: reads=%d submissions=%d error=%v", reads, submissions, err)
	}
}
