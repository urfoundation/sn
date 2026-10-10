// Production polls the actual retained-intent owner before fresh scheduler
// state. Independent workers survive read outages without forgetting defects.
package validator

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// The only injected decisions are the existing poll and read operation. The
// caller passes SubmitOnce itself; no callback grants receipt or signing proof.
func runReleaseProductionSteeringLoopWithWait(ctx context.Context, submit func() error, wait func() bool, progress *releaseProgress) error {
	if ctx == nil || submit == nil || wait == nil {
		return errors.New("production steering loop owner is incomplete")
	}
	var pendingErr error
	failures := 0
	for {
		if err := ctx.Err(); err != nil {
			return releaseRuntimeError(ctx, errors.Join(pendingErr, err))
		}
		err := observeReleaseError(submit())
		if ctx.Err() != nil {
			return releaseRuntimeError(ctx, errors.Join(pendingErr, err, ctx.Err()))
		}
		readWait := releaseErrorMarker[*productionSteeringReadWait](err)
		originalPending := releaseErrorMarker[*productionPendingReconciliation](err)
		transition := releaseErrorMarker[*productionSteeringTransition](err)
		preparation := releaseErrorMarker[*productionPreparationPending](err)
		authentication := releaseErrorMarker[*productionOperatorAuthenticationPending](err)
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
		case preparation != nil && releaseOnlyErrors(err, preparation):
			progress.observeSteering(preparation.nativeEpoch, preparation.epochKnown, "read_wait", false)
			releaseDiagnostic(ctx, "steering", "preparation_pending", preparation.nativeEpoch, preparation.epochKnown, 0, releaseDiagnosticFacts{phase: productionReadPreparation})
		case authentication != nil && releaseOnlyErrors(err, authentication):
			observeProductionAuthenticationWait(ctx, progress, authentication)
		case originalPending != nil && (originalPending.cause == nil || retryableProductionSteeringRead(originalPending.cause)) && releaseOnlyErrors(err, originalPending):
			progress.observeSteering(originalPending.nativeEpoch, true, "receipt_pending", originalPending.cause == nil)
			releaseDiagnostic(ctx, "steering", "receipt_pending", originalPending.nativeEpoch, true, 0, releaseDiagnosticFacts{phase: productionReadReceipt, cause: releaseDiagnosticReadCause(originalPending.cause)})
		case transition != nil && releaseOnlyErrors(err, transition):
			outcome := "working"
			if transition.revealWait {
				outcome = "reveal_wait"
			}
			progress.observeSteering(transition.nativeEpoch, true, outcome, true)
			if !transition.revealWait {
				failures, pendingErr = 0, nil
			}
		default:
			failures++
			pendingErr = errors.Join(pendingErr, err)
			progress.observeSteering(0, false, "hard_error", false)
			releaseDiagnostic(ctx, "steering", "hard_error", 0, false, uint64(failures), releaseDiagnosticFacts{cause: releaseDiagnosticHardError})
		}
		if failures >= releaseSteeringFailureLimit {
			return fmt.Errorf("release steering failed %d consecutive attempts: %w", failures, pendingErr)
		}
		if !wait() {
			return releaseRuntimeError(ctx, pendingErr)
		}
	}
}

// The deadline covers preparation as well as bounded retained reads. A poll
// never consults fresh signing runtime before the durable intent owner does.
func (self *ReleaseSteerer) runProductionSteering(ctx context.Context, poll, timeout time.Duration) error {
	if ctx == nil || poll <= 0 || timeout < productionSteeringReadTimeout {
		return errors.New("production steering timing owner is invalid")
	}
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	return runReleaseProductionSteeringLoopWithWait(ctx, func() error {
		return runReleaseSteeringOperation(ctx, timeout, self.SubmitOnce)
	}, func() bool {
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
			return true
		}
	}, self.runtimeProgress())
}
