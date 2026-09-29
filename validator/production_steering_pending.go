//go:build linux || darwin

// Pending production work owns its original signed bytes before any new
// decision. Absence, nonce and epoch observations share one canonical boundary.
package validator

import (
	"context"
	"errors"
	"fmt"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/crv4"
)

// The stored source grants historical observation independently of current
// signing eligibility. Durable transitions remain outside read retry owners.
func (self *ReleaseSteerer) reconcileProductionPendingV2(ctx context.Context, current *SteeringIntent, advertised *crv4.EpochScheduleState) (bool, error) {
	if current == nil || current.Status != "pending" || current.Prepared == nil {
		return false, errors.New("production pending owner lacks a retained intent")
	}
	if _, err := current.Prepared.Validate(); err != nil {
		return false, err
	}
	decisionCfg, err := productionConfigForIntent(self.cfg, current)
	if err != nil {
		return false, err
	}
	preparedHash, err := types.NewHashFromHexString(current.Prepared.PreparedAtBlockHash)
	if err != nil {
		return false, err
	}
	txHash, err := types.NewHashFromHexString(current.Prepared.ExtrinsicHash)
	if err != nil {
		return false, err
	}
	native := *self.native
	receipt, number, boundary, err := self.scanProductionPendingReceipt(ctx, current, decisionCfg, &native, preparedHash, txHash)
	if err != nil {
		// Retain the already published original-authority wait surface for
		// callers observing a renewed config; every other leaf remains hard.
		var unavailable *productionSteeringReadWait
		if decisionCfg.ownerRecycleProduction.historicalOnly && errors.As(err, &unavailable) && releaseOnlyErrors(err, unavailable) && advertised != nil {
			return false, &productionPendingReconciliation{nativeEpoch: advertised.SubnetEpochIndex, extrinsicHash: current.Prepared.ExtrinsicHash, cause: unavailable.cause}
		}
		return false, err
	}
	if receipt != nil {
		err := self.productionRead(ctx, productionReadReceipt, current, func(readCtx context.Context) error {
			return authenticateProductionFinalizedSourceContext(readCtx, &native, self.cfg, current.Prepared, receipt)
		})
		if err != nil {
			var dispatch *crv4.FinalizedDispatchError
			if errors.As(err, &dispatch) && releaseOnlyErrors(err, dispatch) {
				return false, self.intents.markFailedV2(ctx, current.VectorHash, dispatch)
			}
			return false, err
		}
		if err := self.intents.markFinalizedV2(ctx, current.VectorHash, receipt.ExtrinsicHash.Hex(), receipt.BlockNumber, receipt.BlockHash.Hex(), current.Prepared.RevealBlock, current.Prepared.Values); err != nil {
			return false, self.productionRetainedReadFailure(ctx, productionReadIntent, current, err)
		}
		return true, nil
	}
	var state *crv4.EpochScheduleState
	err = self.productionRead(ctx, productionReadReceipt, current, func(readCtx context.Context) error {
		if err := authenticateHistoricalNativeRuntimeAtContext(readCtx, &native, self.cfg, boundary); err != nil {
			return err
		}
		var err error
		state, err = native.EpochScheduleStateAtContext(readCtx, self.cfg.Netuid, boundary)
		if err != nil {
			return err
		}
		if state.CurrentBlock != number {
			return errors.New("pending schedule differs from the authenticated receipt coverage boundary")
		}
		return nil
	})
	self.runtimeProgress().observeNative(state, err)
	if err != nil {
		return false, err
	}
	if current.SubnetEpoch > state.SubnetEpochIndex {
		return false, &productionSteeringReadWait{phase: productionReadReceipt, nativeEpoch: state.SubnetEpochIndex, epochKnown: true, extrinsicHash: current.Prepared.ExtrinsicHash,
			cause: &crv4.ReceiptEvidenceUnavailableError{BlockHash: boundary, Field: "finalized schedule caught up to retained intent"}}
	}
	var nonce uint32
	err = self.productionRead(ctx, productionReadReceipt, current, func(readCtx context.Context) error {
		var err error
		nonce, err = native.AccountNonceAtContext(readCtx, self.hotkey.PublicKey(), boundary)
		return err
	})
	if err != nil {
		return false, err
	}
	if nonce > current.Prepared.AccountNonce {
		cause := fmt.Errorf("steering nonce %d was consumed by a different finalized extrinsic by scanned block %d", current.Prepared.AccountNonce, number)
		return false, self.intents.markFailedV2(ctx, current.VectorHash, cause)
	}
	if nonce < current.Prepared.AccountNonce {
		return false, fmt.Errorf("steering nonce gap at scanned block %d: finalized %d, prepared %d", number, nonce, current.Prepared.AccountNonce)
	}
	if decisionCfg.ownerRecycleProduction.historicalOnly {
		return false, &productionPendingReconciliation{nativeEpoch: state.SubnetEpochIndex, extrinsicHash: current.Prepared.ExtrinsicHash}
	}
	if current.SubnetEpoch < state.SubnetEpochIndex {
		// These signatures have an immortal era. A missed local epoch does
		// not extinguish their chain liability or permit a replacement vector.
		return false, &productionPendingReconciliation{nativeEpoch: state.SubnetEpochIndex, extrinsicHash: current.Prepared.ExtrinsicHash}
	}
	if err := self.runtimeV2.authenticationPending(current); err != nil {
		return false, err
	}
	var signingHash types.Hash
	err = self.productionRead(ctx, productionReadReceipt, current, func(readCtx context.Context) error {
		var err error
		signingHash, err = authenticatePinnedNativeRuntimeContext(readCtx, &native, self.cfg)
		if err != nil || signingHash != boundary {
			return err
		}
		return validatePreparedNativeRuntimeContext(readCtx, &native, self.cfg, preparedHash, boundary)
	})
	if err != nil {
		return false, err
	}
	if signingHash != boundary {
		// A newer head may contain our transaction. Extend the scan next poll
		// before reading its nonce or broadcasting anything again.
		return false, &productionPendingReconciliation{nativeEpoch: state.SubnetEpochIndex, extrinsicHash: current.Prepared.ExtrinsicHash}
	}
	// Exact-byte rebroadcast still requires the original local publication
	// and EMA duties. Receipt reconciliation itself can proceed before them.
	if err := self.restoreHeadEmaV2(ctx, current); err != nil {
		return false, self.productionRetainedReadFailure(ctx, productionReadIntent, current, err)
	}
	submissionCfg, err := self.intents.ownerRecyclePreparedConfig(ctx, self.cfg, current.Prepared)
	if err != nil {
		return false, self.productionRetainedReadFailure(ctx, productionReadIntent, current, err)
	}
	if err := validateOwnerRecyclePreparedAuthorization(submissionCfg, current.Prepared); err != nil {
		return false, err
	}
	if err := self.runtimeV2.authenticationPending(current); err != nil {
		return false, err
	}
	result, err := crv4.SubmitPrepared(ctx, &native, current.Prepared)
	if err != nil {
		return false, self.recordReleasePendingError(current.VectorHash, err)
	}
	if err := self.productionRead(ctx, productionReadReceipt, current, func(readCtx context.Context) error {
		return authenticateHistoricalNativeRuntimeAtContext(readCtx, &native, self.cfg, result.FinalizedBlockHash)
	}); err != nil {
		return false, err
	}
	if err := self.intents.markFinalizedV2(ctx, current.VectorHash, result.TxHash.Hex(), result.FinalizedBlock, result.FinalizedBlockHash.Hex(), result.RevealBlock, result.Values); err != nil {
		return false, self.productionRetainedReadFailure(ctx, productionReadIntent, current, err)
	}
	return true, nil
}

// Older retained-authority helpers can operate without a live publisher.
func (self *ReleaseSteerer) runtimeProgress() *releaseProgress {
	if self == nil || self.runtimeV2 == nil {
		return nil
	}
	return self.runtimeV2.progress
}
