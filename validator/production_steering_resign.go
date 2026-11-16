//go:build linux || darwin

// A pending intent fails when a different finalized extrinsic consumed its
// nonce: its signed bytes can never land. Its native epoch is then signed
// again, with a fresh nonce from a fresh preparation, only while that is
// still needed, so the weight fallback's own write is never duplicated.
package validator

import (
	"context"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/crv4"
)

// Still needed, read at one authenticated finalized block B (the fresh
// preparation's own native snapshot) against the latest intent record, which
// is failed:
//   - B's native epoch is that intent's native epoch. Once the epoch has
//     passed, the next epoch's own decision follows and nothing is re-signed;
//   - and the intended weights are not already on chain: it is not the case
//     that the hotkey's weights were written after the intent was prepared
//     (its LastUpdate at B is after Prepared.PreparedAtBlock) and its weight
//     row at B equals the intent's exact prepared row (every UID with its
//     exact u16 value, and no other UID). When both hold, a later transaction,
//     such as the fallback's that took the nonce, already set exactly what the
//     validator decided, and a second commit would duplicate it.
//
// The intended commit being on chain is the intent's own receipt, which the
// pending owner's receipt scan finds before any failure is recorded. A pending
// commit's row cannot be read before its reveal, so a fallback commit that has
// not revealed never counts as the intended weights.
func (self *ReleaseSteerer) productionResignHold(ctx context.Context, retained *SteeringIntent, nativeHash types.Hash, nativeState *crv4.EpochScheduleState) (*productionSteeringEpochHold, error) {
	if !isOwnerRecycleProductionConfig(self.cfg) || retained == nil || retained.Status != "failed" || retained.Prepared == nil || nativeState == nil || retained.SubnetEpoch != nativeState.SubnetEpochIndex {
		return nil, nil
	}
	var row []crv4.WeightPair
	var written uint64
	err := self.productionRead(ctx, productionReadPreparation, retained, func(readCtx context.Context) error {
		own := *self.native
		var err error
		row, written, err = self.readProductionWeightWriteV2(readCtx, &own, nativeHash, nativeState.CurrentBlock)
		return err
	})
	if err != nil {
		return nil, err
	}
	// A failed intent never recorded applied values; its prepared row is what
	// it would have committed, compared by the same rule as an application.
	intended := &SteeringIntent{UIDs: retained.Prepared.UIDs, Values: retained.Prepared.Values}
	if written <= retained.Prepared.PreparedAtBlock || !productionApplicationRowMatches(intended, row) {
		return nil, nil
	}
	return &productionSteeringEpochHold{nativeEpoch: retained.SubnetEpoch, prepared: retained.Prepared.PreparedAtBlock, written: written}, nil
}

// One finalized block's weight row and LastUpdate for the hotkey's actual UID
// there, read under that block's own authenticated runtime and then rechecked
// as canonical at its height.
func (self *ReleaseSteerer) readProductionWeightWriteV2(ctx context.Context, native *crv4.Chain, hash types.Hash, number uint64) ([]crv4.WeightPair, uint64, error) {
	if err := authenticateHistoricalNativeRuntimeAtContext(ctx, native, self.cfg, hash); err != nil {
		return nil, 0, err
	}
	allowed, err := releaseHistoricalRuntimeArtifactsAt(self.cfg, number)
	if err != nil {
		return nil, 0, err
	}
	observed, err := crv4.ReadValidatorScheduleAtContext(ctx, native, crv4.ValidatorScheduleQuery{GenesisHash: native.GenesisHash, BlockHash: hash, BlockNumber: number, Netuid: self.cfg.Netuid, Hotkey: self.hotkey.PublicKey(), MaximumSubnetUIDs: releaseNativeValidatorMaximumUIDs}, allowed...)
	if err != nil {
		return nil, 0, err
	}
	uid := observed.Stake.Identity.UID
	row, err := native.WeightsAtContext(ctx, self.cfg.Netuid, uid, hash)
	if err != nil {
		return nil, 0, err
	}
	written, err := native.LastUpdateAtContext(ctx, self.cfg.Netuid, uid, hash)
	if err != nil {
		return nil, 0, err
	}
	return row, written, native.CheckCanonicalBlockAtContext(ctx, hash, number)
}
