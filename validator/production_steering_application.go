//go:build linux || darwin

// Application observation needs the retained native identity and actual row;
// an unrelated fresh EVM decision must not strand a finalized transaction.
package validator

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/crv4"
)

// A finalized row can be applied and then replaced before this process reads
// the head: a restart or a stalled retained-intent read can outlast it, and
// another commit by the same hotkey (the weight fallback's) then replaces it.
// On 2026-11-08 intent 26216's row was live for 71 minutes that way and the
// validator waited in reveal_wait for good. Heights below the head are read
// back from the reveal block, at most this many per poll.
const productionApplicationHistoryBlocksPerPoll = 64

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

// The next height a finalized intent's look-back has yet to read. Finalized
// history does not change, so one process reads each height at most once per
// intent. A nil memo starts every poll at the reveal block, so it reads only
// the first productionApplicationHistoryBlocksPerPoll heights after it.
type productionApplicationHistoryMemo struct {
	lock        sync.Mutex
	vectorHash  string
	revealBlock uint64
	next        uint64
}

func newProductionApplicationHistoryMemo() *productionApplicationHistoryMemo {
	return &productionApplicationHistoryMemo{}
}

func (self *productionApplicationHistoryMemo) start(current *SteeringIntent) uint64 {
	if self == nil {
		return current.RevealBlock
	}
	self.lock.Lock()
	defer self.lock.Unlock()
	if self.vectorHash != current.VectorHash || self.revealBlock != current.RevealBlock || self.next < current.RevealBlock {
		return current.RevealBlock
	}
	return self.next
}

func (self *productionApplicationHistoryMemo) advance(current *SteeringIntent, next uint64) {
	if self == nil {
		return
	}
	self.lock.Lock()
	defer self.lock.Unlock()
	self.vectorHash, self.revealBlock, self.next = current.VectorHash, current.RevealBlock, next
}

// The whole row, as a set: every committed target with its exact value, and no
// other target.
func productionApplicationRowMatches(current *SteeringIntent, row []crv4.WeightPair) bool {
	if len(row) != len(current.UIDs) || len(current.Values) != len(current.UIDs) {
		return false
	}
	want := make(map[uint16]uint16, len(current.UIDs))
	for index, uid := range current.UIDs {
		want[uid] = current.Values[index]
	}
	for _, pair := range row {
		value, found := want[uint16(pair.UID)]
		if !found || value != uint16(pair.Value) {
			return false
		}
		delete(want, uint16(pair.UID))
	}
	return len(want) == 0
}

// One finalized block's applied row, read under that block's own authenticated
// runtime and the hotkey's actual UID there, then rechecked as canonical at
// its height.
func (self *ReleaseSteerer) readProductionApplicationRowV2(ctx context.Context, native *crv4.Chain, hash types.Hash, number uint64) (uint16, []crv4.WeightPair, error) {
	if err := authenticateHistoricalNativeRuntimeAtContext(ctx, native, self.cfg, hash); err != nil {
		return 0, nil, err
	}
	allowed, err := releaseHistoricalRuntimeArtifactsAt(self.cfg, number)
	if err != nil {
		return 0, nil, err
	}
	observed, err := crv4.ReadValidatorScheduleAtContext(ctx, native, crv4.ValidatorScheduleQuery{GenesisHash: native.GenesisHash, BlockHash: hash, BlockNumber: number, Netuid: self.cfg.Netuid, Hotkey: self.hotkey.PublicKey(), MaximumSubnetUIDs: releaseNativeValidatorMaximumUIDs}, allowed...)
	if err != nil {
		return 0, nil, err
	}
	row, err := native.WeightsAtContext(ctx, self.cfg.Netuid, observed.Stake.Identity.UID, hash)
	if err == nil {
		err = native.CheckCanonicalBlockAtContext(ctx, hash, number)
	}
	return observed.Stake.Identity.UID, row, err
}

// Reads back the heights the head did not show, from the reveal block (or where
// an earlier poll stopped) toward the head. Every height is below the selected
// finalized head, so its canonical hash is final. A height whose row matches is
// read again in full, under its own runtime and UID, before it counts; the
// first such height is the application a head read there would have recorded.
func (self *ReleaseSteerer) observeProductionApplicationHistoryV2(ctx context.Context, native *crv4.Chain, current *SteeringIntent, uid uint16, head uint64) (uint64, types.Hash, bool, error) {
	start := self.applicationHistory.start(current)
	if start >= head {
		return 0, types.Hash{}, false, nil
	}
	end := min(head, start+productionApplicationHistoryBlocksPerPoll)
	var number uint64
	var hash types.Hash
	found := false
	err := self.productionRead(ctx, productionReadApplication, current, func(readCtx context.Context) error {
		for height := max(start, self.applicationHistory.start(current)); height < end; height++ {
			candidate, err := readRuntimeWitnessHash(readCtx, native, "application history block", "chain_getBlockHash", height)
			if err != nil {
				return err
			}
			row, err := native.WeightsAtContext(readCtx, self.cfg.Netuid, uid, candidate)
			if err != nil {
				return err
			}
			if productionApplicationRowMatches(current, row) {
				own := *native
				_, row, err = self.readProductionApplicationRowV2(readCtx, &own, candidate, height)
				if err != nil {
					return err
				}
				if productionApplicationRowMatches(current, row) {
					number, hash, found = height, candidate, true
					return nil
				}
			}
			self.applicationHistory.advance(current, height+1)
		}
		return nil
	})
	if err != nil {
		return 0, types.Hash{}, false, err
	}
	return number, hash, found, nil
}

// Reads are block-bound under already approved historical observation, rather
// than borrowing the current signing tuple. A row mismatch at the head is
// pending until a finalized height at or after the reveal block shows the
// exact row; it is not invented economic success or a permission to replace it.
func (self *ReleaseSteerer) observeProductionApplicationV2(ctx context.Context, current *SteeringIntent) error {
	if current == nil || current.Status != "finalized" || current.Prepared == nil {
		return errors.New("production application owner has no finalized intent")
	}
	native := *self.native
	var hash types.Hash
	var number uint64
	var uid uint16
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
		uid, row, err = self.readProductionApplicationRowV2(readCtx, &native, hash, number)
		return err
	})
	if err != nil {
		return err
	}
	wait := &productionSteeringTransition{nativeEpoch: current.SubnetEpoch, revealWait: true}
	if number < current.RevealBlock {
		return wait
	}
	if !productionApplicationRowMatches(current, row) {
		head := number
		var found bool
		number, hash, found, err = self.observeProductionApplicationHistoryV2(ctx, &native, current, uid, head)
		if err != nil {
			// The look-back only adds evidence. A failed read leaves the head's
			// verdict, spends no steering failure and names its causes; the next
			// poll reads from the same height again.
			releaseDiagnosticCauses(ctx, "steering", "application_history", 0, err)
			return wait
		}
		if !found {
			return wait
		}
		fmt.Fprintf(os.Stderr, "validator: native epoch %d's exact row was applied at block %d and replaced before finalized head %d; recording that application\n", current.SubnetEpoch, number, head)
	}
	if err := self.intents.markAppliedV2(ctx, current.VectorHash, number, hash.Hex()); err != nil {
		return self.productionRetainedReadFailure(ctx, productionReadIntent, current, err)
	}
	return &productionSteeringTransition{nativeEpoch: current.SubnetEpoch}
}
