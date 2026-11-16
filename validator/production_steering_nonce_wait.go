// The weight fallback (sn-validator-stopgap) signs with the validator's hotkey.
// Its transaction can take the nonce a pending intent was signed with, or sit
// at an earlier nonce those signed bytes wait behind. The pool then refuses the
// exact bytes as outdated, outranked, banned, already pooled or future, or a
// watch ends without a verdict, and the finalized nonce can lag the prepared
// one. None of these decides the bytes' fate; finality does. On 2026-11-08 ten
// such refusals in a row spent the steering failure budget, the validator
// panicked, and 34 native epochs passed before a manual recovery.
package validator

import (
	"fmt"

	"github.com/urfoundation/sn/crv4"
)

// A pending intent's signed bytes wait on their nonce. The next poll reads the
// finalized chain again: a receipt finalizes the intent, a nonce consumed by a
// different extrinsic fails it (and a fresh nonce follows only if still
// needed), and an unspent nonce rebroadcasts the same bytes. It spends no
// steering failure, and it neither grants a signature nor ends the liability.
type productionPendingNonceWait struct {
	nativeEpoch   uint64
	extrinsicHash string
	prepared      uint32
	// A gap: the finalized nonce at the scanned receipt boundary is below
	// prepared, so the bytes wait behind an earlier transaction.
	gap       bool
	finalized uint32
	// The pool's answer to the exact bytes, when it refused them or a watch
	// ended without a verdict; nil for a gap.
	cause error
}

func (self *productionPendingNonceWait) Error() string {
	if self.gap {
		return fmt.Sprintf("pending production transaction %s (nonce %d) waits behind finalized nonce %d in native epoch %d; observing receipts without rebroadcast", self.extrinsicHash, self.prepared, self.finalized, self.nativeEpoch)
	}
	return fmt.Sprintf("pending production transaction %s (nonce %d) in native epoch %d is left to finality: %v", self.extrinsicHash, self.prepared, self.nativeEpoch, self.cause)
}

// Retains the pool's answer for diagnostics; classifiers stop at the marker.
func (self *productionPendingNonceWait) Unwrap() error { return self.cause }

// A failed intent's native epoch is not signed again: the hotkey's weights were
// written after that intent was prepared and its finalized row already equals
// the intent's exact prepared row. The epoch's next decision follows at the
// next native epoch. It spends no steering failure.
type productionSteeringEpochHold struct {
	nativeEpoch uint64
	prepared    uint64
	written     uint64
}

func (self *productionSteeringEpochHold) Error() string {
	return fmt.Sprintf("native epoch %d is not signed again: the hotkey's weights were written at block %d, after its failed intent was prepared at block %d, and already equal that intent's row", self.nativeEpoch, self.written, self.prepared)
}

// The submission owner's answer for a pending intent's exact bytes. A pool
// answer that leaves them to finality (crv4.SubmissionUnresolvedError) is a
// nonce wait; every other cause keeps the existing pending-error verdict, so a
// genuinely hard refusal still spends the failure budget.
func (self *ReleaseSteerer) productionPendingSubmissionFailure(intent *SteeringIntent, nativeEpoch uint64, err error) error {
	if err == nil || intent == nil {
		return err
	}
	if isOwnerRecycleProductionConfig(self.cfg) && intent.Prepared != nil {
		if unresolved := releaseErrorMarker[*crv4.SubmissionUnresolvedError](err); unresolved != nil && releaseOnlyErrors(err, unresolved) {
			return &productionPendingNonceWait{nativeEpoch: nativeEpoch, extrinsicHash: intent.Prepared.ExtrinsicHash, prepared: intent.Prepared.AccountNonce, cause: unresolved}
		}
	}
	return self.recordReleasePendingError(intent.VectorHash, err)
}
