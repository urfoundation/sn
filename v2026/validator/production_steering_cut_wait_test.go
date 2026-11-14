package validator

import (
	"errors"
	"fmt"
	"os"
	"testing"
	"time"
)

// A fake clock that advances by step on every read, so each poll is later.
func productionCutWaitClock(step time.Duration) func() time.Time {
	at := time.Unix(2_000_000_000, 0)
	return func() time.Time {
		at = at.Add(step)
		return at
	}
}

func productionCutWaitProgress() *releaseProgress {
	return &releaseProgress{now: func() time.Time { return time.Unix(2_000_000_000, 0) }}
}

// While active trails drain, every cut wait keeps the failure budget, however
// many polls it takes, and the loop goes on to succeed.
func TestProductionSteeringCutWaitKeepsFailureBudget(t *testing.T) {
	t.Parallel()
	for _, wait := range []error{
		errAttemptCutPending,
		errAttemptCutSnapshotStale,
		errAttemptSettlementSnapshotStale,
		errors.Join(errAttemptCutPending, errAttemptCutSnapshotStale),
		fmt.Errorf("collect native input: %w", errAttemptCutPending),
		// The submit defer's single production read observation, as the
		// loop receives it from a preparing production submit.
		observeProductionSteeringRead(errAttemptCutPending, false),
		observeProductionSteeringRead(fmt.Errorf("collect native input: %w", errors.Join(errAttemptCutPending, errAttemptSettlementSnapshotStale)), true),
	} {
		progress := productionCutWaitProgress()
		polls := 0
		const waits = 3*releaseSteeringFailureLimit + 5
		err := runReleaseProductionSteeringLoopWithCutWait(t.Context(), func() error {
			polls++
			if polls <= waits {
				return wait
			}
			return nil
		}, func() bool { return polls <= waits }, progress, productionCutWaitClock(6*time.Second), 20*time.Minute)
		if err != nil || polls != waits+1 || progress.value.Steering == nil || progress.value.Steering.Outcome != "complete" {
			t.Fatalf("cut wait %v spent the failure budget: polls=%d error=%v", wait, polls, err)
		}
	}
}

// A cut wait never hides an independent cause: the whole graph stays hard.
func TestProductionSteeringCutWaitKeepsIndependentCausesHard(t *testing.T) {
	t.Parallel()
	broken := errors.New("synthetic signed cut persistence failed")
	for _, hard := range []error{
		errors.Join(errAttemptCutPending, broken),
		errors.Join(errAttemptCutPending, errors.Join(errAttemptCutSnapshotStale, broken)),
		&TrailFatalError{Err: errAttemptCutPending},
		errors.Join(errAttemptCutPending, &os.PathError{Op: "read", Path: "synthetic-cut.json", Err: os.ErrNotExist}),
		observeProductionSteeringRead(errors.Join(errAttemptCutPending, broken), false),
	} {
		progress := productionCutWaitProgress()
		polls := 0
		err := runReleaseProductionSteeringLoopWithCutWait(t.Context(), func() error {
			polls++
			return hard
		}, func() bool { return true }, progress, productionCutWaitClock(6*time.Second), 20*time.Minute)
		if err == nil || polls != releaseSteeringFailureLimit || progress.value.Steering.Outcome != "hard_error" {
			t.Fatalf("cut wait concealed an independent cause %v: polls=%d error=%v", hard, polls, err)
		}
	}
}

// A drain that outlasts its bound is stuck: from then on each cut wait is a
// hard failure, so the loop still stops after the ordinary failure budget.
func TestProductionSteeringCutWaitPastItsBoundTurnsHard(t *testing.T) {
	t.Parallel()
	progress := productionCutWaitProgress()
	polls := 0
	err := runReleaseProductionSteeringLoopWithCutWait(t.Context(), func() error {
		polls++
		return errAttemptCutPending
	}, func() bool { return true }, progress, productionCutWaitClock(time.Minute), 5*time.Minute)
	// Each poll reads the clock twice (start of run, then the bound check),
	// so a one-minute step admits the polls within 5 minutes of the first
	// before the remaining ones count as failures.
	if err == nil || !errors.Is(err, errAttemptCutPending) || progress.value.Steering.Outcome != "hard_error" {
		t.Fatalf("stuck drain never surfaced: polls=%d error=%v", polls, err)
	}
	if polls <= releaseSteeringFailureLimit || polls > releaseSteeringFailureLimit+6 {
		t.Fatalf("stuck drain surfaced after %d polls; want the bound's waits plus %d failures", polls, releaseSteeringFailureLimit)
	}
}

// Real failures before a drain keep counting after it: the drain neither
// restores nor spends the failure budget.
func TestProductionSteeringCutWaitKeepsPriorRealFailures(t *testing.T) {
	t.Parallel()
	broken := errors.New("synthetic transient integrity failure")
	progress := productionCutWaitProgress()
	polls := 0
	const before, drain = releaseSteeringFailureLimit - 1, 30
	err := runReleaseProductionSteeringLoopWithCutWait(t.Context(), func() error {
		polls++
		switch {
		case polls <= before:
			return broken
		case polls <= before+drain:
			return errAttemptCutPending
		default:
			return broken
		}
	}, func() bool { return true }, progress, productionCutWaitClock(6*time.Second), 20*time.Minute)
	if !errors.Is(err, broken) || polls != before+drain+1 {
		t.Fatalf("cut wait changed the prior real failure budget: polls=%d error=%v", polls, err)
	}
}

// Any other outcome ends a run of cut waits, so a later drain starts its own
// bound instead of inheriting the time an earlier one took. A reveal wait
// keeps the failure budget, so accumulated bounds would show up as failures.
func TestProductionSteeringCutWaitBoundRestartsAfterOtherOutcomes(t *testing.T) {
	t.Parallel()
	progress := productionCutWaitProgress()
	polls := 0
	const runs, perRun = 8, 5
	reveal := &productionSteeringTransition{nativeEpoch: 7, revealWait: true}
	err := runReleaseProductionSteeringLoopWithCutWait(t.Context(), func() error {
		polls++
		if polls%(perRun+1) == 0 {
			return reveal
		}
		return errAttemptCutPending
	}, func() bool { return polls < runs*(perRun+1) }, progress, productionCutWaitClock(time.Minute), 6*time.Minute)
	if err != nil || polls != runs*(perRun+1) || progress.value.Steering.Outcome != "reveal_wait" {
		t.Fatalf("separate drains shared one bound: polls=%d error=%v", polls, err)
	}
}

// The deployed bound covers the longest trail the policy allows, twice over.
func TestProductionSteeringCutWaitLimitFollowsPolicy(t *testing.T) {
	t.Parallel()
	if got := releaseProductionCutWaitLimit(nil); got != releaseProductionCutWaitMinimumLimit {
		t.Fatalf("nil config bound = %s", got)
	}
	cfg := &ReleaseConfig{}
	cfg.Policy.Verify.StepTimeoutSeconds = 30
	if got := releaseProductionCutWaitLimit(cfg); got != releaseProductionCutWaitMinimumLimit {
		t.Fatalf("30 s steps bound = %s; want the %s minimum", got, releaseProductionCutWaitMinimumLimit)
	}
	cfg.Policy.Verify.StepTimeoutSeconds, cfg.Policy.Verify.StepTimeoutGraceSeconds = 50, 10
	if got := releaseProductionCutWaitLimit(cfg); got != 32*time.Minute {
		t.Fatalf("60 s steps bound = %s; want 32m", got)
	}
	cfg.Policy.Verify.StepTimeoutSeconds = 10_000
	if got := releaseProductionCutWaitLimit(cfg); got != releaseProductionCutWaitMinimumLimit {
		t.Fatalf("an implausible step timeout bound = %s", got)
	}
}
