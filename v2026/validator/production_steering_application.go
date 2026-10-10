//go:build linux || darwin

// Application observation needs the retained native identity and actual row;
// an unrelated fresh EVM decision must not strand a finalized transaction.
package validator

import (
	"context"
	"errors"
	"fmt"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/v2026/crv4"
)

// A durable transition or a fully observed unrevealed row is work for the next
// poll, without pretending that a fresh native epoch has been completed.
type productionSteeringTransition struct {
	nativeEpoch uint64
	revealWait  bool
}

func (self *productionSteeringTransition) Error() string {
	if self.revealWait {
		return fmt.Sprintf("production epoch %d has a finalized receipt; awaiting its exact applied row", self.nativeEpoch)
	}
	return fmt.Sprintf("production epoch %d completed a durable reconciliation transition", self.nativeEpoch)
}

// Reads are block-bound under already approved historical observation, rather
// than borrowing the current signing tuple. A row mismatch remains observed
// pending; it is not invented economic success or a permission to replace it.
func (self *ReleaseSteerer) observeProductionApplicationV2(ctx context.Context, current *SteeringIntent) error {
	if current == nil || current.Status != "finalized" || current.Prepared == nil {
		return errors.New("production application owner has no finalized intent")
	}
	native := *self.native
	var hash types.Hash
	var number uint64
	var row []crv4.WeightPair
	err := self.productionRead(ctx, productionReadApplication, current, func(readCtx context.Context) error {
		var err error
		candidate, err := readRuntimeWitnessHash(readCtx, &native, "finalized head", "chain_getFinalizedHead")
		if err != nil {
			return err
		}
		hash, err = crv4.SelectFinalityReadBlockContext(readCtx, &native, types.Hash{}, candidate)
		if err != nil {
			return err
		}
		number, _, err = native.ReceiptHeaderAtContext(readCtx, hash)
		if err != nil {
			return err
		}
		if number < current.FinalizedBlock {
			return &crv4.ReceiptEvidenceUnavailableError{BlockHash: hash, Field: "application finalized head caught up to retained receipt"}
		}
		if err := authenticateHistoricalNativeRuntimeAtContext(readCtx, &native, self.cfg, hash); err != nil {
			return err
		}
		allowed, err := releaseHistoricalRuntimeArtifactsAt(self.cfg, number)
		if err != nil {
			return err
		}
		observed, err := crv4.ReadValidatorScheduleAtContext(readCtx, &native, crv4.ValidatorScheduleQuery{GenesisHash: native.GenesisHash, BlockHash: hash, BlockNumber: number, Netuid: self.cfg.Netuid, Hotkey: self.hotkey.PublicKey(), MaximumSubnetUIDs: releaseNativeValidatorMaximumUIDs}, allowed...)
		if err != nil {
			return err
		}
		row, err = native.WeightsAtContext(readCtx, self.cfg.Netuid, observed.Stake.Identity.UID, hash)
		if err == nil {
			err = native.CheckCanonicalBlockAtContext(readCtx, hash, number)
		}
		return err
	})
	if err != nil {
		return err
	}
	wait := &productionSteeringTransition{nativeEpoch: current.SubnetEpoch, revealWait: true}
	if number < current.RevealBlock || len(row) != len(current.UIDs) || len(current.Values) != len(current.UIDs) {
		return wait
	}
	want := make(map[uint16]uint16, len(current.UIDs))
	for index, uid := range current.UIDs {
		want[uid] = current.Values[index]
	}
	for _, pair := range row {
		value, found := want[uint16(pair.UID)]
		if !found || value != uint16(pair.Value) {
			return wait
		}
		delete(want, uint16(pair.UID))
	}
	if len(want) != 0 {
		return wait
	}
	if err := self.intents.markAppliedV2(ctx, current.VectorHash, number, hash.Hex()); err != nil {
		return self.productionRetainedReadFailure(ctx, productionReadIntent, current, err)
	}
	return &productionSteeringTransition{nativeEpoch: current.SubnetEpoch}
}
