//go:build linux || darwin

package validator

// The epoch 8 late-binding window (protocol/fleet_late_binding_epoch8.go) on
// the validator's coordinator reads. Every reader that asks whether a client
// is bound for a settlement epoch at a block applies it through
// stabi.Epoch8BindingsAt: the attempt boundary, the head census, the decision
// chain observer and the settlement audit's head oracle.

import (
	"context"
	"errors"
	"math/big"

	"github.com/urfoundation/sn/v2026/stabi"
)

// releaseEpoch8Bindings applies the window to answers, bindingAt(clientIds[i],
// epoch) read at block, whose canonical hash is hash. The window's own reads
// are bindingAt batches at block's pinned hash and at the window's two fixed
// blocks, each at its canonical hash.
func (self *ChainClient) releaseEpoch8Bindings(ctx context.Context, block uint64, hash [32]byte, clientIds [][16]byte, epoch uint64, answers []stabi.BindingAtOutput) ([]stabi.BindingAtOutput, error) {
	if self == nil || self.coordinator == nil {
		return nil, errors.New("epoch 8 late binding chain is unavailable")
	}
	var chainID uint64
	if self.chainId != nil && self.chainId.IsUint64() {
		chainID = self.chainId.Uint64()
	}
	read := func(ctx context.Context, at uint64, ids [][16]byte, epoch uint64) ([]stabi.BindingAtOutput, error) {
		atHash := hash
		if at != block {
			var err error
			if atHash, err = self.BlockHashContext(ctx, at); err != nil {
				return nil, err
			}
		}
		calls := make([]chainBatchCall, len(ids))
		for index, id := range ids {
			calls[index] = chainBatchCall{address: self.contractAddr, calldata: self.coordinator.PackBindingAt(id, new(big.Int).SetUint64(epoch))}
		}
		encoded, err := self.batchCallsAtHashContext(ctx, at, atHash, calls)
		if err != nil {
			return nil, err
		}
		if len(encoded) != len(ids) {
			return nil, errors.New("epoch 8 late binding response census differs")
		}
		out := make([]stabi.BindingAtOutput, len(encoded))
		for index, raw := range encoded {
			if err := canonicalReleaseDecisionV2View("bindingAt", raw); err != nil {
				return nil, err
			}
			if out[index], err = self.coordinator.UnpackBindingAt(raw); err != nil {
				return nil, err
			}
		}
		return out, nil
	}
	return stabi.Epoch8BindingsAt(ctx, chainID, self.contractAddr, epoch, block, clientIds, answers, read)
}

// recordedBinding returns the observed row that a recorded decision's binding
// row at index matches: the row with the epoch 8 late-binding window applied,
// or, for a decision made before this validator applied the window, the row
// of the coordinator's own answer. claimed has no local key observation.
func (self *releaseDecisionChainV2Observation) recordedBinding(index int, claimed ReleaseBindingMeasurement) (ReleaseBindingMeasurement, bool) {
	if index < 0 || index >= len(self.bindings) {
		return ReleaseBindingMeasurement{}, false
	}
	if claimed == self.bindings[index] {
		return self.bindings[index], true
	}
	if index < len(self.coordinatorBindings) && claimed == self.coordinatorBindings[index] {
		return self.coordinatorBindings[index], true
	}
	return ReleaseBindingMeasurement{}, false
}
