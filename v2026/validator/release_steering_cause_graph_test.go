// Actual steering loop owners must finish refused cause trees without asking
// foreign matchers for authority or turning incomplete graphs into waits.
package validator

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/crv4"
)

// Both loops retain the refused result and stop at the real hard-failure
// limit. Cyclic diagnostics are finite and their traversal has a panic guard.
func TestReleaseSteeringLoopsBoundRefusedCauseGraphs(t *testing.T) {
	for _, production := range []bool{false, true} {
		cycle := &releaseCauseJoin{}
		cycle.causes = []error{cycle}
		var typedNil *productionPreparationPending
		for _, cause := range []error{
			cycle, typedNil,
			&releaseCauseJoin{causes: []error{ErrSteeringAlreadyFinal, nil}},
			&releaseCauseMatcher{cause: &productionPreparationPending{nativeEpoch: 7, epochKnown: true}},
		} {
			attempts, waits := 0, 0
			submit := func() error { attempts++; return cause }
			wait := func() bool { waits++; return attempts < releaseSteeringFailureLimit+1 }
			var err error
			if production {
				err = runReleaseProductionSteeringLoopWithWait(t.Context(), submit, wait, nil)
			} else {
				err = runReleaseSteeringLoopWithWaitAndDeferral(t.Context(), func() (uint64, error) { return 7, nil }, submit, wait, true)
			}
			if err == nil || attempts != releaseSteeringFailureLimit || waits != releaseSteeringFailureLimit-1 {
				t.Fatalf("refused graph escaped the steering owner: production=%t attempts=%d waits=%d", production, attempts, waits)
			}
		}
	}
}

// Concrete original pending identity remains opaque, but its phase and cause
// must be valid. Pure repeated/wrapped originals continue past the hard limit.
func TestProductionSteeringConcretePendingMarkersKeepOriginalSemantics(t *testing.T) {
	for _, marker := range []error{
		&productionPreparationPending{nativeEpoch: 7, epochKnown: true},
		&productionPendingReconciliation{nativeEpoch: 7},
		&productionSteeringReadWait{phase: productionReadReceipt, nativeEpoch: 7, epochKnown: true, cause: context.DeadlineExceeded},
	} {
		attempts := 0
		progress := &releaseProgress{now: func() time.Time { return time.Unix(2_000_000_000, 0) }}
		err := runReleaseProductionSteeringLoopWithWait(t.Context(), func() error {
			attempts++
			return errors.Join(fmt.Errorf("first original: %w", marker), fmt.Errorf("same original: %w", marker))
		}, func() bool { return attempts < releaseSteeringFailureLimit+2 }, progress)
		if err != nil || attempts != releaseSteeringFailureLimit+2 || progress.value.Steering == nil || progress.value.Steering.Outcome == "hard_error" {
			t.Fatalf("concrete original pending result became a hard failure: attempts=%d error=%v", attempts, err)
		}
	}
	for _, marker := range []error{
		&productionSteeringReadWait{phase: 0, cause: context.DeadlineExceeded},
		&productionSteeringReadWait{phase: productionReadReceipt, cause: &os.PathError{Op: "read", Path: "synthetic-intent", Err: context.DeadlineExceeded}},
		&productionPendingReconciliation{nativeEpoch: 7, cause: &os.PathError{Op: "read", Path: "synthetic-intent", Err: context.DeadlineExceeded}},
	} {
		attempts := 0
		err := runReleaseProductionSteeringLoopWithWait(t.Context(), func() error { attempts++; return marker }, func() bool { return false }, nil)
		if err == nil || attempts != 1 {
			t.Fatal("a marker hid its invalid phase or actual hard cause")
		}
	}
}

// A file result returned concurrently with cancellation remains visible at
// both loop exits, even when its inner error is an ordinary canceled result.
func TestReleaseSteeringLoopsKeepFileCauseAtCancellation(t *testing.T) {
	for _, production := range []bool{false, true} {
		ctx, cancel := context.WithCancel(t.Context())
		cause := &os.PathError{Op: "write", Path: "synthetic-retained-intent", Err: context.Canceled}
		attempts, waits := 0, 0
		submit := func() error { attempts++; cancel(); return cause }
		wait := func() bool { waits++; return false }
		var err error
		if production {
			err = runReleaseProductionSteeringLoopWithWait(ctx, submit, wait, nil)
		} else {
			err = runReleaseSteeringLoopWithWaitAndDeferral(ctx, func() (uint64, error) { return 7, nil }, submit, wait, true)
		}
		cancel()
		if !errors.Is(err, cause) || attempts != 1 || production && waits != 0 {
			t.Fatalf("cancellation erased a loop's file result: production=%t attempts=%d waits=%d error=%v", production, attempts, waits, err)
		}
	}
}

// The marker search grants presence only. A legitimate compact replay marker
// may still be joined with another complete transport cause at the real loop.
func TestReleaseSteeringReplayMarkerPreservesJoinedTransport(t *testing.T) {
	marker := classifyAttemptReplayRead(context.DeadlineExceeded)
	if _, ok := marker.(*attemptReplayReadInterruption); !ok {
		t.Fatal("actual replay owner did not admit the typed read interruption")
	}
	attempts := 0
	err := runReleaseSteeringLoopWithWaitAndDeferral(t.Context(), func() (uint64, error) { return 7, nil }, func() error {
		attempts++
		return errors.Join(marker, context.DeadlineExceeded)
	}, func() bool { return attempts < releaseSteeringFailureLimit+2 }, false)
	if err != nil || attempts != releaseSteeringFailureLimit+2 {
		t.Fatalf("bounded marker search narrowed valid replay retry: attempts=%d error=%v", attempts, err)
	}
}

// The pre-intent owner preserves a real exact-weight rejection while refusing
// malformed graphs before its adjacent marker search can recurse or dispatch.
func TestReleasePreIntentWeightOwnerBoundsMarkerSearch(t *testing.T) {
	_, original := crv4.ApplyMaxWeightLimitRational([]*big.Rat{big.NewRat(1, 1)}, 32768)
	if _, ok := original.(*crv4.InfeasibleWeightLimitError); !ok {
		t.Fatal("actual exact-weight owner did not produce its infeasible result")
	}
	wrapped := fmt.Errorf("original pre-intent assembly: %w", original)
	result := classifyProvisionalNativeWeights(t.Context(), true, 7, 9, wrapped)
	rejected, ok := result.(*provisionalNativeWeightRejection)
	if !ok || rejected.nativeEpoch != 7 || rejected.settlementEpoch != 9 || !errors.Is(rejected.cause, wrapped) {
		t.Fatal("actual weight rejection lost its original pre-intent scope")
	}
	cycle := &releaseCauseJoin{}
	cycle.causes = []error{cycle}
	var typedNil *crv4.InfeasibleWeightLimitError
	for _, cause := range []error{
		cycle, typedNil, &releaseCauseMatcher{cause: original},
		&releaseCauseJoin{causes: []error{original, nil}},
		errors.Join(original, &os.PathError{Op: "write", Path: "synthetic-native-intent", Err: context.Canceled}),
	} {
		if result := classifyProvisionalNativeWeights(t.Context(), true, 7, 9, cause); !errors.Is(result, cause) || releaseErrorMarker[*provisionalNativeWeightRejection](result) != nil {
			t.Fatal("pre-intent marker search downgraded an incomplete or hard original")
		}
	}
}
