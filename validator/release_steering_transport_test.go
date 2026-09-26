// Transport retries retain the native epoch and its semantic failure budget.
// Explicit poll steps exercise recovery and shutdown without clock timing.
package validator

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
)

// Normal retained steering must survive an outage longer than ten requests,
// then complete once without granting another submission in the same epoch.
func TestReleaseSteeringTransportRecoversBeyondFailureLimit(t *testing.T) {
	t.Parallel()
	for _, cause := range []error{
		&attemptStreamHttpStatusError{status: http.StatusInternalServerError, upload: true, detail: "synthetic redis dependency: connection refused"},
		&attemptStreamHttpStatusError{status: http.StatusBadGateway, upload: true},
		&attemptStreamHttpStatusError{status: http.StatusTooManyRequests, upload: true},
		&url.Error{Op: "Post", URL: "https://publication.example/sn/attempt-artifact", Err: context.DeadlineExceeded},
		&attemptReplicaPublicationError{causes: []error{context.DeadlineExceeded, context.Canceled}},
		errors.Join(errAttemptCutSnapshotStale, context.DeadlineExceeded),
	} {
		reads, attempts := 0, 0
		err := runReleaseSteeringLoopWithWait(t.Context(), func() (uint64, error) {
			reads++
			return 17, nil
		}, func() error {
			attempts++
			if attempts <= releaseSteeringFailureLimit+2 {
				return cause
			}
			return nil
		}, func() bool { return reads < releaseSteeringFailureLimit+4 })
		if err != nil || attempts != releaseSteeringFailureLimit+3 || reads != releaseSteeringFailureLimit+4 {
			t.Fatalf("transport outage spent semantic failures or repeated completion: cause=%v attempts=%d reads=%d error=%v", cause, attempts, reads, err)
		}
	}
}

// A service outage neither spends nor resets failures from the native intent
// lifecycle. Its eventual tenth semantic failure still reaches supervision.
func TestReleaseSteeringTransportPreservesPriorFailureCount(t *testing.T) {
	t.Parallel()
	broken := errors.New("prior subnet epoch 16 intent is finalized; refusing a new commit")
	const outageAttempts = releaseSteeringFailureLimit + 2
	for _, provisional := range []bool{false, true} {
		attempts := 0
		err := runReleaseSteeringLoopWithWaitAndDeferral(t.Context(), func() (uint64, error) { return 17, nil }, func() error {
			attempts++
			if attempts >= releaseSteeringFailureLimit && attempts < releaseSteeringFailureLimit+outageAttempts {
				return &attemptReplicaPublicationError{causes: []error{
					&attemptStreamHttpStatusError{status: http.StatusInternalServerError, upload: true}, context.Canceled,
				}}
			}
			return broken
		}, func() bool { return attempts <= releaseSteeringFailureLimit+outageAttempts }, provisional)
		if !errors.Is(err, broken) || !strings.Contains(err.Error(), "10 consecutive attempts") || attempts != releaseSteeringFailureLimit+outageAttempts {
			t.Fatalf("outage changed prior semantic failure count: provisional=%t attempts=%d error=%v", provisional, attempts, err)
		}
	}
}

// Retrying the current epoch is independent of permission to advance. Neither
// ordinary transport nor a cut joined to it creates that permission.
func TestReleaseSteeringTransportRejectsIncompleteEpochAdvance(t *testing.T) {
	t.Parallel()
	for _, cause := range []error{context.DeadlineExceeded, errors.Join(errAttemptCutSnapshotStale, context.DeadlineExceeded)} {
		for _, freshWeights := range []bool{false, true} {
			reads, attempts := 0, 0
			err := runReleaseSteeringLoopWithWaitAndPermissions(t.Context(), func() (uint64, error) {
				reads++
				if reads > releaseSteeringFailureLimit+2 {
					return 18, nil
				}
				return 17, nil
			}, func() error { attempts++; return cause }, func() bool { return reads < releaseSteeringFailureLimit+4 }, false, freshWeights)
			if !errors.Is(err, cause) || !strings.Contains(err.Error(), "incomplete epoch 17 to 18") || attempts != releaseSteeringFailureLimit+2 || reads != attempts+1 {
				t.Fatalf("transport advanced an incomplete native epoch: fresh=%t cause=%v attempts=%d reads=%d error=%v", freshWeights, cause, attempts, reads, err)
			}
		}
	}
}

// Only one outstanding transport cause is retained. Ending observation must
// expose that failure instead of claiming an incomplete operation succeeded.
func TestReleaseSteeringTransportRetainsLatestUnresolvedCause(t *testing.T) {
	t.Parallel()
	first := &attemptStreamHttpStatusError{status: http.StatusBadGateway, upload: true}
	last := &attemptStreamHttpStatusError{status: http.StatusInternalServerError, upload: true}
	attempts := 0
	err := runReleaseSteeringLoopWithWait(t.Context(), func() (uint64, error) { return 17, nil }, func() error {
		attempts++
		if attempts < releaseSteeringFailureLimit+2 {
			return first
		}
		return last
	}, func() bool { return attempts < releaseSteeringFailureLimit+2 })
	if !errors.Is(err, last) || errors.Is(err, first) || attempts != releaseSteeringFailureLimit+2 {
		t.Fatalf("unfinished transport lost its latest cause or accumulated retries: attempts=%d error=%v", attempts, err)
	}
}

// Every hard branch remains counted, including diagnostic text that resembles
// an outage and cancellation outside the replica worker's joined ownership.
func TestReleaseSteeringTransportRejectsMixedAndUnownedFailures(t *testing.T) {
	t.Parallel()
	broken := errors.New("immutable object content hash differs")
	for _, cause := range []error{
		errors.Join(context.DeadlineExceeded, broken),
		&attemptReplicaPublicationError{causes: []error{context.DeadlineExceeded, broken, context.Canceled}},
		errors.Join(context.DeadlineExceeded, context.Canceled),
		&attemptStreamHttpStatusError{status: http.StatusConflict, upload: true, detail: "connection reset"},
		&os.PathError{Op: "read", Path: "synthetic-intent.json", Err: context.DeadlineExceeded},
		&os.LinkError{Op: "rename", Old: "synthetic-candidate.json", New: "synthetic-intent.json", Err: context.DeadlineExceeded},
		&TrailFatalError{Err: context.DeadlineExceeded},
		errors.New("immutable hash mismatch: connection reset"),
		errors.New("attempt upload response status is 500: unsigned diagnostic"),
	} {
		for _, provisional := range []bool{false, true} {
			attempts := 0
			err := runReleaseSteeringLoopWithWaitAndDeferral(t.Context(), func() (uint64, error) { return 17, nil }, func() error { attempts++; return cause }, func() bool { return attempts <= releaseSteeringFailureLimit }, provisional)
			if !errors.Is(err, cause) || attempts != releaseSteeringFailureLimit || !strings.Contains(err.Error(), "consecutive attempts") {
				t.Fatalf("semantic or unowned failure escaped budget: provisional=%t cause=%v attempts=%d error=%v", provisional, cause, attempts, err)
			}
		}
	}
}

// A recovered scheduler still owes its unresolved submission. A prior semantic
// failure also prevents a provisional owner from adopting the next epoch.
func TestReleaseSteeringTransportPreservesPendingSubmissionAcrossSchedulerOutage(t *testing.T) {
	t.Parallel()
	broken := errors.New("prior subnet epoch 16 intent is finalized; refusing a new commit")
	for _, provisional := range []bool{false, true} {
		reads, attempts := 0, 0
		err := runReleaseSteeringLoopWithWaitAndDeferral(t.Context(), func() (uint64, error) {
			reads++
			if reads == 3 {
				return 0, fmt.Errorf("scheduler: %w", context.DeadlineExceeded)
			}
			if reads == 4 {
				return 18, nil
			}
			return 17, nil
		}, func() error {
			attempts++
			if attempts == 1 {
				return broken
			}
			return context.DeadlineExceeded
		}, func() bool { return reads < 5 }, provisional)
		if !errors.Is(err, broken) || !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "incomplete epoch") || reads != 4 || attempts != 2 {
			t.Fatalf("outage erased unresolved native work: provisional=%t reads=%d attempts=%d error=%v", provisional, reads, attempts, err)
		}
	}
}

// A ready poll cannot start new work after owner cancellation. Existing
// semantic failure survives; a pure operation timeout shuts down gracefully.
func TestReleaseSteeringTransportHonorsOwnerCancellation(t *testing.T) {
	t.Parallel()
	broken := errors.New("synthetic intent durability failure")
	for _, priorFailure := range []bool{false, true} {
		ctx, cancel := context.WithCancel(t.Context())
		reads, attempts := 0, 0
		err := runReleaseSteeringLoopWithWait(ctx, func() (uint64, error) { reads++; return 17, nil }, func() error {
			attempts++
			if priorFailure && attempts == 1 {
				return broken
			}
			return context.DeadlineExceeded
		}, func() bool {
			if !priorFailure || attempts == 2 {
				cancel()
			}
			return true
		})
		cancel()
		if priorFailure && (!errors.Is(err, broken) || attempts != 2) || !priorFailure && (err != nil || attempts != 1) || reads != attempts {
			t.Fatalf("cancellation lost a failure or admitted more work: prior=%t reads=%d attempts=%d error=%v", priorFailure, reads, attempts, err)
		}
	}
}
