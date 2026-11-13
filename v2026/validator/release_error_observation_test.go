// Whole caller decisions consume a single bounded original graph, including
// marker causes, retained failures and later worker cancellation normalization.
package validator

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

// Completion and cut waits cannot replace a hard edge seen by the first query.
// Each actual loop stops with that original cause after one captured submission.
func TestReleaseSteeringLoopsCannotReplaceFirstHardObservation(t *testing.T) {
	for _, production := range []bool{false, true} {
		hard := &os.PathError{Op: "read", Path: "synthetic-original-release-state", Err: context.Canceled}
		later := errAttemptCutPending
		if production {
			later = ErrSteeringAlreadyFinal
		}
		probe := &releasePreparationChangingCause{first: hard, later: later}
		submissions := 0
		submit := func() error { submissions++; return probe }
		var err error
		if production {
			err = runReleaseProductionSteeringLoopWithWait(t.Context(), submit, func() bool { return false }, nil)
		} else {
			err = runReleaseSteeringLoopWithWaitAndDeferral(t.Context(), func() (uint64, error) { return 7, nil }, submit, func() bool { return false }, true)
		}
		if err == nil || !errors.Is(err, hard) || !errors.Is(err, probe) || probe.observations != 1 || submissions != 1 {
			t.Fatalf("loop replaced its first hard original: production=%t observations=%d submissions=%d", production, probe.observations, submissions)
		}
	}
}

// A captured hard result survives retention until a later poll cancels. The
// returned evidence identifies the original wrapper without reopening it.
func TestReleaseSteeringRetainedHardObservationSurvivesCancellation(t *testing.T) {
	for _, production := range []bool{false, true} {
		ctx, cancel := context.WithCancel(t.Context())
		hard := &os.PathError{Op: "write", Path: "synthetic-original-custody", Err: context.Canceled}
		probe := &releasePreparationChangingCause{first: hard, later: context.Canceled}
		submit := func() error { return probe }
		wait := func() bool { cancel(); return false }
		var err error
		if production {
			err = runReleaseProductionSteeringLoopWithWait(ctx, submit, wait, nil)
		} else {
			err = runReleaseSteeringLoopWithWaitAndDeferral(ctx, func() (uint64, error) { return 7, nil }, submit, wait, true)
		}
		cancel()
		if err == nil || !errors.Is(err, hard) || !errors.Is(err, probe) || probe.observations != 1 {
			t.Fatalf("later cancellation reopened a retained hard original: production=%t observations=%d", production, probe.observations)
		}
	}
}

// The shared public worker owner captures before cancellation normalization,
// joins the returning worker, and performs its actual final Stats write.
func TestReleaseShutdownRetainsFirstObservedWorkerCause(t *testing.T) {
	cfg, runtime := newReleaseShutdownTestRuntime(t)
	hard := &os.PathError{Op: "write", Path: "synthetic-original-worker-state", Err: context.Canceled}
	probe := &releasePreparationChangingCause{first: hard, later: context.Canceled}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	operations := releaseShutdownTestOperations(cancel)
	operations.refresh = func(ctx context.Context) error { <-ctx.Done(); return probe }
	err := runReleaseOperatorWorkers(ctx, cancel, cfg, []*releaseOperatorRuntime{runtime}, operations)
	if err == nil || !errors.Is(err, hard) || !errors.Is(err, probe) || probe.observations != 1 {
		t.Fatalf("shutdown reread the original worker cause: observations=%d", probe.observations)
	}
}

// A first cyclic graph cannot borrow a later deadline's read authority.
func TestProductionRetainedReadRefusesFirstCyclicObservation(t *testing.T) {
	cycle := &releaseCauseJoin{}
	cycle.causes = []error{cycle}
	probe := &releasePreparationChangingCause{first: cycle, later: context.DeadlineExceeded}
	self := &ReleaseSteerer{cfg: &ReleaseConfig{SchemaVersion: ReleaseMainnetProductionSchemaVersion}}
	err := self.productionRetainedReadFailure(t.Context(), productionReadReceipt, nil, probe)
	if err == nil || releaseErrorMarker[*productionSteeringReadWait](err) != nil || !errors.Is(err, probe) || probe.observations != 1 || cycle.visits != 1 {
		t.Fatal("retained read replaced or repeatedly traversed the first cyclic original")
	}
}

// Exact repeated marker identity and its child are copied once. Both the wait
// and subsequent runtime check see that same complete original deadline.
func TestProductionObservationPreservesRepeatedMarkerAndChild(t *testing.T) {
	probe := &releasePreparationChangingCause{first: context.DeadlineExceeded, later: &os.PathError{Op: "read", Path: "synthetic-later-state", Err: context.Canceled}}
	marker := &productionSteeringReadWait{phase: productionReadReceipt, nativeEpoch: 7, epochKnown: true, cause: probe}
	original := errors.Join(marker, marker)
	submissions := 0
	err := runReleaseProductionSteeringLoopWithWait(t.Context(), func() error { submissions++; return original }, func() bool { return false }, nil)
	if err != nil || submissions != 1 || probe.observations != 1 {
		t.Fatal("repeated marker identity or its original child was re-observed")
	}
}

// A previously captured refusal remains hard when another private marker
// carries it; reuse cannot conceal that status behind an opaque legacy branch.
func TestReleaseObservationHoistsRetainedHardChildThroughMarker(t *testing.T) {
	hard := &os.PathError{Op: "read", Path: "synthetic-original-child", Err: context.Canceled}
	probe := &releasePreparationChangingCause{first: hard, later: context.DeadlineExceeded}
	observed := observeReleaseError(probe)
	for _, marker := range []error{
		&productionPendingReconciliation{nativeEpoch: 7, cause: observed},
		&provisionalNativeReadInterruption{nativeEpoch: 7, cause: observed},
	} {
		err := runReleaseSteeringLoopWithWaitAndPermissions(t.Context(), func() (uint64, error) { return 7, nil }, func() error { return marker }, func() bool { return false }, true, true)
		if err == nil || !errors.Is(err, hard) || probe.observations != 1 {
			t.Fatal("opaque marker concealed a retained original hard child")
		}
	}
}

// Typed-nil captured envelopes are refused before reuse, both directly and
// inside a joined worker result. Cancellation cannot erase the refusal.
func TestReleaseObservationRefusesTypedNilEnvelopes(t *testing.T) {
	var missing *releaseObservedOriginal
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, original := range []error{missing, errors.Join(context.DeadlineExceeded, missing)} {
		observed := releaseRuntimeError(ctx, original)
		if observed == nil || !errors.Is(observed, original) || observed.Error() == "" || releaseOnlyErrors(observed, context.Canceled, context.DeadlineExceeded) {
			t.Fatal("typed-nil original was admitted or lost at runtime cancellation")
		}
		err := runReleaseProductionSteeringLoopWithWait(t.Context(), func() error { return observed }, func() bool { return false }, nil)
		if err == nil || !errors.Is(err, original) {
			t.Fatal("loop reused a typed-nil original as completion")
		}
	}
}

// The actual preparation, replay and weight owners must retain their first
// observation on refusal and success before the loop inspects their result.
func TestReleaseOriginalProducersCarryOneObservationIntoLoop(t *testing.T) {
	owners := []struct {
		name     string
		later    error
		positive error
		classify func(error) error
	}{
		{name: "pre-intent", later: errAttemptCutPending, positive: context.DeadlineExceeded, classify: func(err error) error { return classifyProvisionalNativeRead(true, 7, err) }},
		{name: "replay", later: context.DeadlineExceeded, positive: context.DeadlineExceeded, classify: classifyAttemptReplayRead},
		{name: "weights", later: errNoPositiveUnmaskedWeights, positive: errNoPositiveUnmaskedWeights, classify: func(err error) error { return classifyProvisionalNativeWeights(t.Context(), true, 7, 9, err) }},
	}
	for _, owner := range owners {
		for _, positive := range []bool{false, true} {
			hard := &os.PathError{Op: "read", Path: "synthetic-original-producer-state", Err: context.Canceled}
			probe := &releasePreparationChangingCause{first: hard, later: owner.later}
			if positive {
				probe.first, probe.later = owner.positive, hard
			}
			submissions := 0
			err := runReleaseSteeringLoopWithWaitAndPermissions(t.Context(), func() (uint64, error) { return 7, nil }, func() error {
				submissions++
				return owner.classify(probe)
			}, func() bool { return false }, true, true)
			if submissions != 1 || probe.observations != 1 || (positive && err != nil) || (!positive && (err == nil || !errors.Is(err, hard) || !errors.Is(err, probe))) {
				t.Fatalf("producer changed its original at loop admission: owner=%s positive=%t observations=%d submissions=%d", owner.name, positive, probe.observations, submissions)
			}
		}
	}
}

// The production read budget inspects each attempt before returning a marker.
// Its first hard read cannot become completion at the downstream loop.
func TestProductionReadCarriesFirstObservationIntoLoop(t *testing.T) {
	for _, positive := range []bool{false, true} {
		hard := &os.PathError{Op: "read", Path: "synthetic-original-receipt", Err: context.DeadlineExceeded}
		probe := &releasePreparationChangingCause{first: hard, later: ErrSteeringAlreadyFinal}
		if positive {
			probe.first, probe.later = context.DeadlineExceeded, hard
		}
		self := &ReleaseSteerer{cfg: &ReleaseConfig{SchemaVersion: ReleaseMainnetProductionSchemaVersion}}
		waits, reads, submissions := 0, 0, 0
		self.productionReadHooks.wait = func(context.Context, time.Duration) error { waits++; return context.DeadlineExceeded }
		err := runReleaseProductionSteeringLoopWithWait(t.Context(), func() error {
			submissions++
			return self.productionRead(t.Context(), productionReadReceipt, nil, func(context.Context) error { reads++; return probe })
		}, func() bool { return false }, nil)
		wantWaits := 0
		if positive {
			wantWaits = 1
		}
		if submissions != 1 || reads != 1 || waits != wantWaits || probe.observations != 1 || (positive && err != nil) || (!positive && (err == nil || !errors.Is(err, hard))) {
			t.Fatalf("production read changed its original at loop admission: positive=%t observations=%d reads=%d waits=%d", positive, probe.observations, reads, waits)
		}
	}
}
