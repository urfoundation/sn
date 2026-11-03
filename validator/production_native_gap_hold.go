// Once the validator misses a native epoch, the next one no longer follows its
// last applied intent, and head admission refuses it until a pinned bridge edge
// admits the step. An edge is read once, at startup, and only an operator can
// supply one, so retrying cannot change the answer and neither can a restart
// without one. Steering holds: it takes each native epoch's cut and refuses
// again, while trails, settlement and the ledger audit keep running.
package validator

import (
	"context"
	"errors"
	"fmt"
)

// The refusal keeps its original text: operators and the runbook search for it.
var errReleaseHeadEMANativeGap = errors.New("compact head EMA epoch jumped")

// Poll cycles a completed refusal waits before steering looks again. At the
// production three-second poll that is one look a minute, enough to keep
// SnValidatorSteeringFailing above its threshold of three in fifteen minutes.
const productionNativeGapHoldPolls = 20

// One native epoch's refusal, and the applied intent it could not follow. A nil
// cause means the attempt reached head admission; otherwise the attempt failed
// earlier, its cut may still be pending, and the next poll retries it.
type productionNativeGapHold struct {
	lastEpoch        uint64
	lastStatus       string
	lastArtifactHash string
	nativeEpoch      uint64
	cause            error
}

func (self *productionNativeGapHold) Error() string {
	if self.cause != nil {
		return fmt.Sprintf("native epoch %d does not follow applied intent %d, and its attempt failed: %v", self.nativeEpoch, self.lastEpoch, self.cause)
	}
	return fmt.Sprintf("%v: native epoch %d does not follow applied intent %d", errReleaseHeadEMANativeGap, self.nativeEpoch, self.lastEpoch)
}

// Nil keeps an ordinary attempt's error exactly as it was, and so does a pure
// cancellation or expiry: shutdown and the operation budget keep their meaning.
func (self *productionNativeGapHold) attemptFailed(cause error) error {
	if self == nil || releaseOnlyErrors(cause, context.Canceled, context.DeadlineExceeded) {
		return cause
	}
	return &productionNativeGapHold{lastEpoch: self.lastEpoch, lastStatus: self.lastStatus, lastArtifactHash: self.lastArtifactHash, nativeEpoch: self.nativeEpoch, cause: cause}
}

// Only a graph that is wholly this refusal holds. A local file error or any
// other independent cause stays a hard failure, and spends the budget as before.
func productionSteeringNativeGapHoldOf(err error) *productionNativeGapHold {
	if err == nil {
		return nil
	}
	if releaseOnlyErrors(err, errReleaseHeadEMANativeGap) {
		return &productionNativeGapHold{}
	}
	if marker := releaseErrorMarker[*productionNativeGapHold](err); marker != nil && releaseOnlyErrors(err, marker) {
		return marker
	}
	return nil
}

// A cut wait is the attempt's own drain, not a failure. Everything else in a
// hold reads as the hard error it is, so SnValidatorSteeringFailing stays on.
func observeProductionNativeGapHold(ctx context.Context, progress *releaseProgress, hold *productionNativeGapHold, err error) {
	known := hold.nativeEpoch != 0
	if hold.cause != nil && productionSteeringCutWait(hold.cause) {
		progress.observeSteering(hold.nativeEpoch, known, "read_wait", false)
		releaseDiagnostic(ctx, "steering", "preparation_pending", hold.nativeEpoch, known, 0, releaseDiagnosticFacts{phase: productionReadPreparation})
		return
	}
	progress.observeSteering(hold.nativeEpoch, known, "hard_error", false)
	releaseDiagnostic(ctx, "steering", "hard_error", hold.nativeEpoch, known, 1, releaseDiagnosticFacts{cause: releaseDiagnosticHardError})
	releaseDiagnostic(ctx, "steering", "native_gap_hold", hold.nativeEpoch, known, 0, releaseDiagnosticFacts{cause: releaseDiagnosticHardError})
	releaseDiagnosticCauses(ctx, "steering", "hard_error", 1, err)
}

// What an operator needs, once per native epoch, without reading the private
// intent file: the applied predecessor and the rule's first native epoch.
func releaseDiagnosticNativeGapHold(ctx context.Context, hold *productionNativeGapHold) {
	self := releaseDiagnosticOwner(ctx)
	if self == nil || hold.nativeEpoch == 0 {
		return
	}
	resume := fmt.Sprintf("steering resumes after a restart with the edge last_native_epoch=%d last_artifact_hash=%s first_native_epoch=%d or later", hold.lastEpoch, hold.lastArtifactHash, hold.nativeEpoch+1)
	if hold.lastStatus != "applied" {
		resume = "a bridge edge needs an applied predecessor, so this one needs an operator's review"
	}
	self.exporter.Offer("steering", []byte(fmt.Sprintf("validator steering hold: native epoch %d does not follow the %s intent at native epoch %d (%s) and no bridge edge admits it; trails and settlement keep running; %s\n",
		hold.nativeEpoch, hold.lastStatus, hold.lastEpoch, hold.lastArtifactHash, resume)))
}
