// The weight fallback (sn-validator-stopgap) signs with the validator's hotkey.
// Its transaction can take the nonce a pending intent was signed with, or sit
// at an earlier nonce those signed bytes wait behind. The pool then refuses the
// exact bytes as outdated, outranked, banned, already pooled or future, or a
// watch ends without a verdict, and the finalized nonce can lag the prepared
// one. None of these decides the bytes' fate; finality does. On 2026-11-08 ten
// such refusals in a row spent the steering failure budget, the validator
// panicked, and 34 native epochs passed before a manual recovery.
//
// Each is a wait only with nonce evidence from the node's pool-aware next index
// (system_accountNextIndex: the best block's nonce plus this hotkey's
// consecutive ready pool transactions). A refusal needs the next index past the
// prepared nonce: some transaction, the fallback's or these bytes, holds or used
// that nonce. A gap needs it past the finalized nonce: the earlier transaction
// is in the best chain or the pool. Without that evidence the answer is about
// the bytes alone, and it stays hard as before. For example, the pool bans the
// hash of bytes it found invalid, such as for unpaid fees, and renews the ban
// on every rebroadcast.
package validator

import (
	"context"
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
	// The pool-aware next index that showed the nonce held or pending.
	next uint32
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
		return fmt.Sprintf("pending production transaction %s (nonce %d) waits behind finalized nonce %d (next index %d) in native epoch %d; observing receipts without rebroadcast", self.extrinsicHash, self.prepared, self.finalized, self.next, self.nativeEpoch)
	}
	return fmt.Sprintf("pending production transaction %s (nonce %d, next index %d) in native epoch %d is left to finality: %v", self.extrinsicHash, self.prepared, self.next, self.nativeEpoch, self.cause)
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

// The hotkey's pool-aware next index, read through the receipt phase's bounded
// owner, so a transport failure is a read wait.
func (self *ReleaseSteerer) productionNextIndex(ctx context.Context, intent *SteeringIntent) (uint32, error) {
	var next uint32
	err := self.productionRead(ctx, productionReadReceipt, intent, func(readCtx context.Context) error {
		var err error
		next, err = self.native.AccountNonceContext(readCtx, self.hotkey.Address())
		return err
	})
	return next, err
}

// The submission owner's answer for a pending intent's exact bytes. A pool
// answer that leaves them to finality (crv4.SubmissionUnresolvedError) is a
// nonce wait when the next index shows the nonce held or used. Every other
// cause keeps the existing pending-error verdict, so a genuinely hard refusal
// still spends the failure budget.
func (self *ReleaseSteerer) productionPendingSubmissionFailure(ctx context.Context, intent *SteeringIntent, nativeEpoch uint64, err error) error {
	if err == nil || intent == nil {
		return err
	}
	if isOwnerRecycleProductionConfig(self.cfg) && intent.Prepared != nil {
		if unresolved := releaseErrorMarker[*crv4.SubmissionUnresolvedError](err); unresolved != nil && releaseOnlyErrors(err, unresolved) {
			next, readErr := self.productionNextIndex(ctx, intent)
			if readErr != nil {
				return readErr
			}
			if next > intent.Prepared.AccountNonce {
				return &productionPendingNonceWait{nativeEpoch: nativeEpoch, extrinsicHash: intent.Prepared.ExtrinsicHash, prepared: intent.Prepared.AccountNonce, next: next, cause: unresolved}
			}
		}
	}
	return self.recordReleasePendingError(intent.VectorHash, err)
}
