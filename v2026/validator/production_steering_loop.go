// Production polls the actual retained-intent owner before fresh scheduler
// state. Independent workers survive read outages without forgetting defects.
package validator

import (
	"context"
	"errors"
	"time"

	"github.com/urnetwork/connect/v2026"
)

// A cut wait is the attempt ledger draining its active trails before it cuts,
// or the next poll supplying a fresh canonical snapshot. No trail starts once
// a cut is pending, so the drain ends within the longest trail: at most
// connect.VerifyMMax hops, each bounded by the policy's step timeout and grace.
// Consecutive cut waits keep the failure budget for twice that, and at least
// this long. A drain that outlasts the bound is stuck, and every further cut
// wait counts as a hard failure so it still surfaces.
const releaseProductionCutWaitMinimumLimit = 20 * time.Minute

func releaseProductionCutWaitLimit(cfg *ReleaseConfig) time.Duration {
	limit := releaseProductionCutWaitMinimumLimit
	if cfg == nil {
		return limit
	}
	step := time.Duration(max(cfg.Policy.Verify.StepTimeoutSeconds, 0)+max(cfg.Policy.Verify.StepTimeoutGraceSeconds, 0)) * time.Second
	if step <= 0 || step > time.Hour {
		return limit
	}
	return max(limit, 2*time.Duration(connect.VerifyMMax)*step)
}

// Only a graph whose every cause is a cut wait or a stale cut or settlement
// snapshot is a cut wait. Any independent cause, a trail-fatal wrapper or a
// local file error keeps the whole graph hard.
func productionSteeringCutWait(err error) bool {
	return err != nil && releaseOnlyErrors(err, errAttemptCutPending, errAttemptCutSnapshotStale, errAttemptSettlementSnapshotStale)
}

// The only injected decisions are the existing poll and read operation. The
// caller passes SubmitOnce itself; no callback grants receipt or signing proof.
func runReleaseProductionSteeringLoopWithWait(ctx context.Context, submit func() error, wait func() bool, progress *releaseProgress) error {
	return runReleaseProductionSteeringLoopWithCutWait(ctx, submit, wait, progress, time.Now, releaseProductionCutWaitMinimumLimit)
}

func runReleaseProductionSteeringLoopWithCutWait(ctx context.Context, submit func() error, wait func() bool, progress *releaseProgress, now func() time.Time, cutWaitLimit time.Duration) error {
	if ctx == nil || submit == nil || wait == nil || now == nil || cutWaitLimit <= 0 {
		return errors.New("production steering loop owner is incomplete")
	}
	var pendingErr error
	failures := 0
	// The current run of consecutive cut waits; any other outcome ends it.
	var cutWaitSince time.Time
	cutWaits := 0
	for {
		if err := ctx.Err(); err != nil {
			return releaseRuntimeError(ctx, errors.Join(pendingErr, err))
		}
		err := observeReleaseError(submit())
		gapHold := productionSteeringNativeGapHoldOf(err)
		if ctx.Err() != nil {
			// A completed refusal is the answer, not a failure of this shutdown.
			if gapHold != nil && gapHold.cause == nil {
				err = nil
			}
			return releaseRuntimeError(ctx, errors.Join(pendingErr, err, ctx.Err()))
		}
		if recorded := releaseErrorMarker[*productionNativeGapEdgeRecorded](err); recorded != nil && releaseOnlyErrors(err, recorded) {
			return recorded
		}
		readWait := releaseErrorMarker[*productionSteeringReadWait](err)
		originalPending := releaseErrorMarker[*productionPendingReconciliation](err)
		nonceWait := releaseErrorMarker[*productionPendingNonceWait](err)
		epochHold := releaseErrorMarker[*productionSteeringEpochHold](err)
		transition := releaseErrorMarker[*productionSteeringTransition](err)
		preparation := releaseErrorMarker[*productionPreparationPending](err)
		authentication := releaseErrorMarker[*productionOperatorAuthenticationPending](err)
		cutWait := productionSteeringCutWait(err)
		if !cutWait {
			cutWaitSince, cutWaits = time.Time{}, 0
		} else if cutWaitSince.IsZero() {
			cutWaitSince = now()
		}
		switch {
		case err == nil || releaseOnlyErrors(err, ErrSteeringAlreadyFinal):
			failures, pendingErr = 0, nil
			progress.observeSteering(0, false, "complete", true)
		case readWait != nil && readWait.phase >= productionReadIntent && readWait.phase <= productionReadApplication && retryableProductionSteeringRead(readWait.cause) && releaseOnlyErrors(err, readWait):
			outcome := "read_wait"
			if readWait.phase == productionReadReceipt {
				outcome = "receipt_transport_wait"
			}
			progress.observeSteering(readWait.nativeEpoch, readWait.epochKnown, outcome, false)
			releaseDiagnostic(ctx, "steering", outcome, readWait.nativeEpoch, readWait.epochKnown, 0, releaseDiagnosticFacts{phase: readWait.phase, cause: releaseDiagnosticReadCause(readWait.cause)})
			releaseDiagnosticCauses(ctx, "steering", outcome, 0, err)
		case preparation != nil && releaseOnlyErrors(err, preparation):
			progress.observeSteering(preparation.nativeEpoch, preparation.epochKnown, "read_wait", false)
			releaseDiagnostic(ctx, "steering", "preparation_pending", preparation.nativeEpoch, preparation.epochKnown, 0, releaseDiagnosticFacts{phase: productionReadPreparation})
		case authentication != nil && releaseOnlyErrors(err, authentication):
			observeProductionAuthenticationWait(ctx, progress, authentication)
		case originalPending != nil && (originalPending.cause == nil || retryableProductionSteeringRead(originalPending.cause)) && releaseOnlyErrors(err, originalPending):
			progress.observeSteering(originalPending.nativeEpoch, true, "receipt_pending", originalPending.cause == nil)
			releaseDiagnostic(ctx, "steering", "receipt_pending", originalPending.nativeEpoch, true, 0, releaseDiagnosticFacts{phase: productionReadReceipt, cause: releaseDiagnosticReadCause(originalPending.cause)})
		case nonceWait != nil && releaseOnlyErrors(err, nonceWait):
			// The signed bytes stay pending on their nonce, typically held by
			// the weight fallback's transaction, and finality decides them.
			// The wait neither spends nor restores the failure budget.
			progress.observeSteering(nonceWait.nativeEpoch, true, "receipt_pending", true)
			releaseDiagnostic(ctx, "steering", "nonce_wait", nonceWait.nativeEpoch, true, 0, releaseDiagnosticFacts{phase: productionReadReceipt})
			releaseDiagnosticCauses(ctx, "steering", "nonce_wait", 0, err)
		case epochHold != nil && releaseOnlyErrors(err, epochHold):
			// The epoch's weights are already written as decided, so its work
			// is complete; the next native epoch brings the next decision.
			failures, pendingErr = 0, nil
			progress.observeSteering(epochHold.nativeEpoch, true, "epoch_wait", true)
			releaseDiagnostic(ctx, "steering", "epoch_wait", epochHold.nativeEpoch, true, 0)
		case transition != nil && releaseOnlyErrors(err, transition):
			outcome := "working"
			if transition.revealWait {
				outcome = "reveal_wait"
			}
			progress.observeSteering(transition.nativeEpoch, true, outcome, true)
			if !transition.revealWait {
				failures, pendingErr = 0, nil
			}
		case gapHold != nil:
			// Nothing here can change the refusal, so it spends no failures and
			// the failures before it cannot end a process that has nothing left
			// to retry. The hold still reads as a failure on every health signal.
			failures, pendingErr = 0, nil
			observeProductionNativeGapHold(ctx, progress, gapHold, err)
			if gapHold.cause == nil {
				// This epoch's cut is done. Trails, settlement and the audit run
				// on; steering looks again at its slower pace.
				for range productionNativeGapHoldPolls - 1 {
					if !wait() {
						return releaseRuntimeError(ctx, pendingErr)
					}
				}
			}
		case cutWait && now().Sub(cutWaitSince) <= cutWaitLimit:
			// The drain keeps the failure budget: it neither spends nor
			// restores it, so real failures before it still count.
			cutWaits++
			progress.observeSteering(0, false, "read_wait", false)
			releaseDiagnostic(ctx, "steering", "preparation_pending", 0, false, uint64(cutWaits), releaseDiagnosticFacts{phase: productionReadPreparation})
			if cutWaits == 1 {
				releaseDiagnosticCauses(ctx, "steering", "preparation_pending", uint64(cutWaits), err)
			}
		default:
			failures++
			pendingErr = errors.Join(pendingErr, err)
			progress.observeSteering(0, false, "hard_error", false)
			releaseDiagnostic(ctx, "steering", "hard_error", 0, false, uint64(failures), releaseDiagnosticFacts{cause: releaseDiagnosticHardError})
			releaseDiagnosticCauses(ctx, "steering", "hard_error", uint64(failures), err)
		}
		if failures >= releaseSteeringFailureLimit {
			return &releaseSteeringFailureLimitError{failures: failures, cause: pendingErr}
		}
		if !wait() {
			return releaseRuntimeError(ctx, pendingErr)
		}
	}
}

// An attempt an upstream refused for load waits out its pause before the loop
// sees its outcome, so the next attempt cannot follow at the poll interval.
// The outcome is returned unchanged; a pause cut short by shutdown joins the
// owner's error, which the loop then treats as shutdown.
func paceReleaseSteeringAttempt(ctx context.Context, err error, settings ReleaseSteeringSettings, wait releaseSnapshotRetryWait) error {
	pause := releaseRateLimitPause(err, settings)
	if pause <= 0 {
		return err
	}
	if waitErr := wait(ctx, pause); waitErr != nil {
		return errors.Join(err, waitErr)
	}
	return err
}

// The deadline covers preparation as well as bounded retained reads. A poll
// never consults fresh signing runtime before the durable intent owner does.
func (self *ReleaseSteerer) runProductionSteering(ctx context.Context, poll time.Duration) error {
	timeout := releaseSteeringOperationTimeout(ctx)
	if ctx == nil || poll <= 0 || timeout < productionSteeringReadTimeout {
		return errors.New("production steering timing owner is invalid")
	}
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	settings := releaseSteeringSettingsFrom(ctx)
	return runReleaseProductionSteeringLoopWithCutWait(ctx, func() error {
		return paceReleaseSteeringAttempt(ctx, runConfiguredReleaseSteeringOperation(ctx, self.SubmitOnce), settings, waitReleaseSnapshotRetry)
	}, func() bool {
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
			return true
		}
	}, self.runtimeProgress(), time.Now, releaseProductionCutWaitLimit(self.cfg))
}
