package validator

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"
)

// The collector joins separately owned operator results. Each participant can
// need a different next poll without spending a native submission attempt.
func TestReleasePreparationJoinedCutsDrainBeyondFailureLimit(t *testing.T) {
	t.Parallel()
	markers := []error{errAttemptCutPending, errAttemptCutSnapshotStale, errAttemptSettlementSnapshotStale}
	for _, first := range markers {
		for _, second := range markers {
			for _, provisional := range []bool{false, true} {
				joined := errors.Join(fmt.Errorf("native operator 1: %w", first), fmt.Errorf("native operator 2: %w", second))
				reads, attempts := 0, 0
				err := runReleaseSteeringLoopWithWaitAndDeferral(t.Context(), func() (uint64, error) {
					reads++
					return 7, nil
				}, func() error {
					attempts++
					if attempts <= releaseSteeringFailureLimit+2 {
						return joined
					}
					return nil
				}, func() bool { return reads < releaseSteeringFailureLimit+4 }, provisional)
				if err != nil || attempts != releaseSteeringFailureLimit+3 || reads != releaseSteeringFailureLimit+4 {
					t.Fatalf("parallel cut waits spent failures or resubmitted completion: provisional=%t first=%v second=%v reads=%d attempts=%d error=%v", provisional, first, second, reads, attempts, err)
				}
			}
		}
	}
}

// A provisional epoch transition preserves the same permission for joined
// cut waits. Strict execution still requires completion of its original epoch.
func TestReleasePreparationJoinedCutsKeepEpochPermission(t *testing.T) {
	t.Parallel()
	joined := fmt.Errorf("parallel native collection: %w", errors.Join(errAttemptCutPending, errors.Join(errAttemptCutSnapshotStale, errAttemptSettlementSnapshotStale)))
	for _, provisional := range []bool{false, true} {
		reads, attempts := 0, 0
		err := runReleaseSteeringLoopWithWaitAndDeferral(t.Context(), func() (uint64, error) {
			reads++
			return uint64(6 + reads), nil
		}, func() error {
			attempts++
			if attempts == 1 {
				return joined
			}
			return nil
		}, func() bool { return reads < 2 }, provisional)
		if provisional {
			if err != nil || reads != 2 || attempts != 2 {
				t.Fatalf("joined cut lost provisional continuation: reads=%d attempts=%d error=%v", reads, attempts, err)
			}
		} else if err == nil || !strings.Contains(err.Error(), "incomplete epoch") || reads != 2 || attempts != 1 {
			t.Fatalf("joined cut skipped a strict native epoch: reads=%d attempts=%d error=%v", reads, attempts, err)
		}
	}
}

// A read interruption from one operator does not turn another operator's cut
// wait into failure. Only the existing provisional collection owner admits it.
func TestReleasePreparationJoinedCutAndTransportContinue(t *testing.T) {
	t.Parallel()
	for _, marker := range []error{errAttemptCutPending, errAttemptCutSnapshotStale, errAttemptSettlementSnapshotStale} {
		joined := errors.Join(fmt.Errorf("native operator 1: %w", marker), fmt.Errorf("native operator 2 replica: %w", &attemptReplicaPublicationError{causes: []error{context.DeadlineExceeded, context.Canceled}}))
		reads, attempts := 0, 0
		err := runReleaseSteeringLoopWithWaitAndDeferral(t.Context(), func() (uint64, error) {
			reads++
			if reads > releaseSteeringFailureLimit+2 {
				return 8, nil
			}
			return 7, nil
		}, func() error {
			attempts++
			if attempts <= releaseSteeringFailureLimit+2 {
				return joined
			}
			return nil
		}, func() bool { return reads < releaseSteeringFailureLimit+4 }, true)
		if err != nil || reads != releaseSteeringFailureLimit+4 || attempts != releaseSteeringFailureLimit+3 {
			t.Fatalf("typed parallel read/cut lost recovery: marker=%v reads=%d attempts=%d error=%v", marker, reads, attempts, err)
		}
	}
}

// The pre-intent marker may retain a parallel cut wait beside its exact typed
// read failure. Pure cut waits do not acquire new pre-intent epoch permissions.
func TestReleasePreparationJoinedReadKeepsPreIntentAuthority(t *testing.T) {
	t.Parallel()
	joined := errors.Join(fmt.Errorf("native operator 1: %w", errAttemptCutPending), fmt.Errorf("native operator 2 read: %w", context.DeadlineExceeded))
	if got := classifyProvisionalNativeRead(false, 7, joined); got != joined {
		t.Fatal("post-intent collection gained read retry authority")
	}
	pureCut := errors.Join(errAttemptCutPending, errAttemptCutSnapshotStale)
	if got := classifyProvisionalNativeRead(true, 7, pureCut); got != pureCut {
		t.Fatal("pure cut wait acquired pre-intent read authority")
	}
	interrupted := classifyProvisionalNativeRead(true, 7, joined)
	var read *provisionalNativeReadInterruption
	if !errors.As(interrupted, &read) || read.nativeEpoch != 7 || !errors.Is(interrupted, errAttemptCutPending) || !errors.Is(interrupted, context.DeadlineExceeded) {
		t.Fatalf("parallel pre-intent read lost its exact scope/causes: %v", interrupted)
	}
	reads, attempts := 0, 0
	err := runReleaseSteeringLoopWithWaitAndPermissions(t.Context(), func() (uint64, error) {
		reads++
		return uint64(6 + reads), nil
	}, func() error {
		attempts++
		if attempts == 1 {
			return interrupted
		}
		return nil
	}, func() bool { return reads < 2 }, false, true)
	if err != nil || reads != 2 || attempts != 2 {
		t.Fatalf("pre-intent read could not resume in the next native epoch: reads=%d attempts=%d error=%v", reads, attempts, err)
	}
	for _, cause := range []error{joined, classifyProvisionalNativeRead(true, 8, joined)} {
		attempts = 0
		err := runReleaseSteeringLoopWithWaitAndPermissions(t.Context(), func() (uint64, error) { return 7, nil }, func() error { attempts++; return cause }, func() bool { return attempts <= releaseSteeringFailureLimit }, false, true)
		if err == nil || attempts != releaseSteeringFailureLimit {
			t.Fatalf("unmarked or foreign-epoch read gained authority: attempts=%d error=%v", attempts, err)
		}
	}
}

// Exact markers and transport owners matter. Neither diagnostic text nor a
// recoverable sibling can make local evidence or a fatal trail retryable.
func TestReleasePreparationJoinedFailuresPreserveEveryHardCause(t *testing.T) {
	t.Parallel()
	broken := errors.New("synthetic signed cut persistence failed")
	for _, hard := range []error{
		broken,
		context.Canceled,
		io.EOF,
		errors.New("attempt ledger cut is waiting for active trails"),
		&os.PathError{Op: "read", Path: "synthetic-cut.json", Err: context.DeadlineExceeded},
		&TrailFatalError{Err: errAttemptCutPending},
	} {
		joined := errors.Join(errAttemptCutPending, errors.Join(context.DeadlineExceeded, hard))
		if retryable, transport := classifyReleasePreparationRetry(joined); retryable || transport {
			t.Fatalf("independent cause was hidden by cut/transport: cause=%v retryable=%t transport=%t", hard, retryable, transport)
		}
		if got := classifyProvisionalNativeRead(true, 7, joined); got != joined {
			t.Fatalf("independent cause acquired pre-intent authority: %v", hard)
		}
	}
	for _, preceding := range []bool{false, true} {
		reads, attempts := 0, 0
		err := runReleaseSteeringLoopWithWaitAndDeferral(t.Context(), func() (uint64, error) {
			reads++
			if reads > 2 {
				return 8, nil
			}
			return 7, nil
		}, func() error {
			attempts++
			if preceding && attempts == 1 {
				return broken
			}
			joined := errors.Join(errAttemptCutPending, errAttemptCutSnapshotStale)
			if !preceding {
				joined = errors.Join(joined, broken)
			}
			return joined
		}, func() bool { return reads < 3 }, true)
		if !errors.Is(err, broken) || !strings.Contains(err.Error(), "incomplete epoch") || attempts != 2 || reads != 3 {
			t.Fatalf("parallel wait erased prior/mixed failure: preceding=%t reads=%d attempts=%d error=%v", preceding, reads, attempts, err)
		}
	}
}

// Retryable parallel results neither spend nor reset the real failure budget.
func TestReleasePreparationJoinedWaitKeepsPriorFailureCount(t *testing.T) {
	t.Parallel()
	broken := errors.New("synthetic native intent persistence failed")
	const pendingPolls = 12
	for _, provisional := range []bool{false, true} {
		attempts := 0
		err := runReleaseSteeringLoopWithWaitAndDeferral(t.Context(), func() (uint64, error) { return 7, nil }, func() error {
			attempts++
			if attempts >= releaseSteeringFailureLimit && attempts < releaseSteeringFailureLimit+pendingPolls {
				return errors.Join(errAttemptCutPending, errAttemptCutSnapshotStale)
			}
			return broken
		}, func() bool { return attempts <= releaseSteeringFailureLimit+pendingPolls }, provisional)
		if !errors.Is(err, broken) || !strings.Contains(err.Error(), "10 consecutive attempts") || attempts != releaseSteeringFailureLimit+pendingPolls {
			t.Fatalf("parallel wait changed native failure accounting: provisional=%t attempts=%d error=%v", provisional, attempts, err)
		}
	}
}

// The independent refresh applies the same all-causes rule. Failed parallel
// preparation publishes no snapshot; one later successful turn publishes once.
func TestReleasePreparationSettlementRetriesJoinedWaitAndTransport(t *testing.T) {
	t.Parallel()
	for _, cause := range []error{
		errors.Join(errAttemptCutPending, errAttemptCutSnapshotStale),
		errors.Join(errAttemptSettlementSnapshotStale, context.DeadlineExceeded),
	} {
		loads, advances, published, waits := 0, 0, 0, 0
		err := runReleaseSettlementRefresh(t.Context(), time.Hour, func(context.Context) (*ReleaseSnapshot, error) {
			loads++
			return &ReleaseSnapshot{Epoch: big.NewInt(8)}, nil
		}, func(context.Context, *ReleaseSnapshot) error {
			advances++
			if advances == 1 {
				return cause
			}
			return nil
		}, func(*ReleaseSnapshot) { published++ }, func(context.Context, time.Duration) error {
			waits++
			if waits == 3 {
				return context.Canceled
			}
			return nil
		})
		if !errors.Is(err, context.Canceled) || loads != 2 || advances != 2 || published != 1 || waits != 3 {
			t.Fatalf("parallel refresh did not recover exactly once: cause=%v loads=%d advances=%d published=%d waits=%d error=%v", cause, loads, advances, published, waits, err)
		}
	}
}

// errors.Is(pending) alone hid a simultaneous journal failure. Force a second
// poll to return a distinct sentinel so the old owner fails promptly in tests.
func TestReleasePreparationSettlementDoesNotHideJoinedIntegrity(t *testing.T) {
	t.Parallel()
	broken := errors.New("synthetic settlement journal failed")
	for _, marker := range []error{errAttemptCutPending, errAttemptCutSnapshotStale, errAttemptSettlementSnapshotStale} {
		waits, advances, published := 0, 0, 0
		err := runReleaseSettlementRefresh(t.Context(), time.Hour, func(context.Context) (*ReleaseSnapshot, error) {
			return &ReleaseSnapshot{Epoch: big.NewInt(8)}, nil
		}, func(context.Context, *ReleaseSnapshot) error {
			advances++
			return errors.Join(fmt.Errorf("operator 1: %w", marker), fmt.Errorf("operator 2: %w", broken))
		}, func(*ReleaseSnapshot) { published++ }, func(context.Context, time.Duration) error {
			waits++
			if waits > 1 {
				return context.Canceled
			}
			return nil
		})
		if !errors.Is(err, broken) || waits != 1 || advances != 1 || published != 0 {
			t.Fatalf("cut marker hid independent refresh failure: marker=%v waits=%d advances=%d published=%d error=%v", marker, waits, advances, published, err)
		}
	}
}

func TestReleasePreparationJoinedCutHonorsOwnerCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	reads, attempts, waits := 0, 0, 0
	err := runReleaseSteeringLoopWithWaitAndDeferral(ctx, func() (uint64, error) { reads++; return 7, nil }, func() error {
		attempts++
		return errors.Join(errAttemptCutPending, errAttemptCutSnapshotStale)
	}, func() bool { waits++; cancel(); return true }, true)
	if err != nil || reads != 1 || attempts != 1 || waits != 1 {
		t.Fatalf("canceled parallel wait spent failure or admitted more work: reads=%d attempts=%d waits=%d error=%v", reads, attempts, waits, err)
	}
}
