//go:build linux || darwin

// Bounded complete-body progress survives later unavailable reads. Optional
// persistence retains it across restart without mutating original intent work.
package validator

import (
	"context"
	"errors"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/crv4"
)

const productionReceiptScanBlockLimit uint64 = 4096

// Original runtime authority is authenticated before opening disposable cache
// state. Each chunk owns the existing 60/300s read budgets independently of
// completed disk work. A busy advancing chain cannot make one poll unbounded.
func (self *ReleaseSteerer) scanProductionPendingReceipt(ctx context.Context, current *SteeringIntent, decisionCfg *ReleaseConfig, native *crv4.Chain, preparedHash, txHash types.Hash) (*crv4.FinalizedExtrinsic, uint64, types.Hash, error) {
	if err := self.productionRead(ctx, productionReadReceipt, current, func(readCtx context.Context) error {
		return authenticateProductionSourceRuntimeAtContext(readCtx, native, decisionCfg, preparedHash)
	}); err != nil {
		return nil, 0, types.Hash{}, err
	}
	owner, err := newProductionReceiptCheckpointOwner(self, decisionCfg, current)
	if err != nil {
		return nil, 0, types.Hash{}, err
	}
	var memory, checkpoint *productionReceiptCheckpoint
	disabled := false
	if self.runtimeV2 != nil {
		memory, disabled = self.runtimeV2.receiptCache.snapshot(owner.scope)
	}
	if !disabled {
		checkpoint, err = owner.load(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil, 0, types.Hash{}, errors.Join(err, ctx.Err())
			}
			self.disableReceiptCache(ctx, current.SubnetEpoch, productionReceiptCacheRead)
			disabled = true
		}
	}
	if memory != nil && (checkpoint == nil || memory.Through >= checkpoint.Through) {
		if checkpoint != nil && memory.Through == checkpoint.Through && memory.BlockHash != checkpoint.BlockHash {
			self.disableReceiptCache(ctx, current.SubnetEpoch, productionReceiptCacheRead)
			disabled = true
		}
		checkpoint = memory
	}
	owner.checkpoint = checkpoint
	first, previous := current.Prepared.PreparedAtBlock, types.Hash{}
	if checkpoint != nil {
		first, previous = checkpoint.Through+1, checkpoint.BlockHash
	}
	wait := func(field string) error {
		return &productionSteeringReadWait{phase: productionReadReceipt, nativeEpoch: current.SubnetEpoch, epochKnown: true, extrinsicHash: current.Prepared.ExtrinsicHash,
			cause: &crv4.ReceiptEvidenceUnavailableError{BlockHash: previous, Field: field}}
	}
	remaining := productionReceiptScanBlockLimit
	// Admission stays hard. Only optional local cache I/O can disable caching;
	// the exact completed prefix remains useful in this runtime's memory.
	retain := func(scan *crv4.FinalizedExtrinsicScan) error {
		value, err := owner.accept(scan)
		if err != nil {
			return err
		}
		if self.runtimeV2 != nil {
			self.runtimeV2.receiptCache.remember(value)
		}
		if !disabled {
			if err := owner.persist(ctx, *value); err != nil {
				if ctx.Err() != nil {
					return errors.Join(err, ctx.Err())
				}
				self.disableReceiptCache(ctx, current.SubnetEpoch, productionReceiptCacheWrite)
				disabled = true
			}
		}
		return nil
	}
	advance := func(scan *crv4.FinalizedExtrinsicScan) error {
		number, hash, covered := scan.AbsenceBoundary()
		if !covered || number < first || number-first+1 > remaining {
			return errors.New("production receipt coverage exceeds its requested chunk")
		}
		if err := retain(scan); err != nil {
			return err
		}
		remaining -= number - first + 1
		checkpoint = owner.checkpoint
		first, previous = number+1, hash
		return nil
	}
	finishedBudget := errors.New("production receipt scan completed its finite block allowance")
	for remaining != 0 {
		var scan *crv4.FinalizedExtrinsicScan
		err := self.productionRead(ctx, productionReadReceipt, current, func(readCtx context.Context) error {
			if remaining == 0 {
				return finishedBudget
			}
			var err error
			scan, err = native.ScanFinalizedExtrinsicRange(readCtx, txHash, crv4.FinalizedExtrinsicScanRange{
				First: first, PreviousHash: previous, MaximumBlocks: min(remaining, crv4.ReceiptScanChunkBlockLimit),
			})
			if _, _, covered := scan.AbsenceBoundary(); err != nil && covered && ctx.Err() == nil && retryableProductionSteeringRead(err) {
				// Preserve real completed bodies before the retry owner discards
				// its expired attempt. The next attempt starts after this prefix.
				if retainErr := advance(scan); retainErr != nil {
					return errors.Join(err, retainErr)
				}
			}
			return err
		})
		if err != nil {
			if errors.Is(err, finishedBudget) && releaseOnlyErrors(err, finishedBudget) {
				return nil, 0, types.Hash{}, wait("bounded receipt scan checkpointed; further canonical coverage remains")
			}
			return nil, 0, types.Hash{}, err
		}
		if receipt := scan.Receipt(); receipt != nil {
			return receipt, 0, types.Hash{}, nil
		}
		number, hash, covered := scan.AbsenceBoundary()
		if !covered {
			head, headHash := scan.FinalizedBoundary()
			if checkpoint != nil && head == checkpoint.Through && headHash == checkpoint.BlockHash {
				// The captured canonical head re-authenticates the saved boundary.
				// No body after it is borrowed for nonce or epoch observation.
				return nil, head, headHash, nil
			}
			return nil, 0, types.Hash{}, wait("finalized receipt head has not reached original or retained coverage")
		}
		if err := advance(scan); err != nil {
			return nil, 0, types.Hash{}, err
		}
		if scan.ReachedFinalizedBoundary() {
			return nil, number, hash, nil
		}
	}
	return nil, 0, types.Hash{}, wait("bounded receipt scan checkpointed; further canonical coverage remains")
}
