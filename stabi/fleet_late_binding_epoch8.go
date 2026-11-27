// The epoch 8 late-binding window applied to the coordinator's bindingAt
// answers (protocol/fleet_late_binding_epoch8.go). Every reader that asks
// whether a client is bound for a settlement epoch at a block, the validator,
// snclaim and the network operator's server alike, applies it through
// Epoch8BindingsAt, so they all answer alike.
package stabi

import (
	"context"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"

	"github.com/urfoundation/sn/protocol"
)

// BindingAtReader reads the coordinator's bindingAt(clientIds[i], epoch) at
// one finalized block, in order. The caller pins the block's canonical hash.
type BindingAtReader func(ctx context.Context, block uint64, clientIds [][16]byte, epoch uint64) ([]BindingAtOutput, error)

// Epoch8BindingsAt returns answers, bindingAt(clientIds[i], epoch) read at
// block, with the epoch 8 late-binding window applied. Outside the window's
// deployment, epoch and blocks it returns answers unchanged and reads nothing.
//
// A client the coordinator answers unbound for epoch 8 at block is bound for
// epoch 8 when its newest binding signed from epoch 9 landed in the window
// and is in force (protocol.Epoch8LateBindingInForce). The landing is read
// from the coordinator alone: the binding is absent from bindingAt(client, 9)
// at the block before epoch 8 and, once the window has closed, present at the
// window's last block. The answer is the window binding's record with
// ValidFromEpoch 8, the epoch it counts from, active. A client the
// coordinator answers bound for epoch 8 keeps that answer.
func Epoch8BindingsAt(ctx context.Context, chainId uint64, coordinator common.Address, epoch uint64, block uint64, clientIds [][16]byte, answers []BindingAtOutput, read BindingAtReader) ([]BindingAtOutput, error) {
	if len(answers) != len(clientIds) {
		return nil, fmt.Errorf("epoch 8 late binding: %d answers for %d clients", len(answers), len(clientIds))
	}
	if !protocol.Epoch8LateBindingAppliesAt(chainId, coordinator, epoch, block) {
		return answers, nil
	}
	if ctx == nil || read == nil {
		return nil, errors.New("epoch 8 late binding reader is unavailable")
	}
	out := make([]BindingAtOutput, len(answers))
	copy(out, answers)
	unbound := []int{}
	for index := range answers {
		if !answers[index].Active {
			unbound = append(unbound, index)
		}
	}
	if len(unbound) == 0 {
		return out, nil
	}
	firstSigned := protocol.Epoch8LateBindingEpoch + 1
	atBlock, err := readEpoch8Bindings(ctx, read, block, clientIds, unbound, firstSigned)
	if err != nil {
		return nil, err
	}
	signed := []int{}
	for position, index := range unbound {
		record := atBlock[position].Record
		if record.Generation != 0 && record.ValidFromEpoch == firstSigned {
			signed = append(signed, index)
		}
	}
	if len(signed) == 0 {
		return out, nil
	}
	signedAt := make(map[int]BindingAtOutput, len(signed))
	for position, index := range unbound {
		signedAt[index] = atBlock[position]
	}
	before, err := readEpoch8Bindings(ctx, read, protocol.Epoch8LateBindingFirstBlock-1, clientIds, signed, firstSigned)
	if err != nil {
		return nil, err
	}
	closed := protocol.Epoch8LateBindingEndBlock <= block
	var lastWindow []BindingAtOutput
	if closed {
		if lastWindow, err = readEpoch8Bindings(ctx, read, protocol.Epoch8LateBindingEndBlock-1, clientIds, signed, firstSigned); err != nil {
			return nil, err
		}
	}
	for position, index := range signed {
		at, prior := signedAt[index], before[position].Record
		late, replaced := at, false
		if closed {
			window := lastWindow[position]
			if window.Record.Generation == 0 || window.Record.ValidFromEpoch != firstSigned || window.Record.Generation == prior.Generation {
				// no binding signed from epoch 9 landed in the window
				continue
			}
			switch {
			case window.Record.Generation == at.Record.Generation:
			case window.Record.Generation < at.Record.Generation:
				// a binding that landed after the window replaced it
				late, replaced = window, true
			default:
				return nil, fmt.Errorf("epoch 8 late binding: client 0x%x's binding at block %d is older than at block %d", clientIds[index], block, protocol.Epoch8LateBindingEndBlock-1)
			}
		} else if at.Record.Generation == prior.Generation {
			// it landed before epoch 8, and starts at its signed epoch
			continue
		}
		if prior.Generation != 0 && late.Record.Generation <= prior.Generation {
			return nil, fmt.Errorf("epoch 8 late binding: client 0x%x's window generation %d does not follow generation %d", clientIds[index], late.Record.Generation, prior.Generation)
		}
		record := late.Record
		if !protocol.Epoch8LateBindingInForce(late.Active && !replaced, record.ValidToEpoch, record.Cleaned, record.CleanedAtEpoch, replaced) {
			continue
		}
		record.ValidFromEpoch = protocol.Epoch8LateBindingEpoch
		out[index] = BindingAtOutput{Active: true, Record: record}
	}
	return out, nil
}

// readEpoch8Bindings reads bindingAt(client, epoch) at block for the clients
// at indexes, in order.
func readEpoch8Bindings(ctx context.Context, read BindingAtReader, block uint64, clientIds [][16]byte, indexes []int, epoch uint64) ([]BindingAtOutput, error) {
	ids := make([][16]byte, len(indexes))
	for position, index := range indexes {
		ids[position] = clientIds[index]
	}
	answers, err := read(ctx, block, ids, epoch)
	if err != nil {
		return nil, fmt.Errorf("epoch 8 late binding: bindingAt(_, %d) at block %d: %w", epoch, block, err)
	}
	if len(answers) != len(ids) {
		return nil, fmt.Errorf("epoch 8 late binding: bindingAt(_, %d) at block %d answered %d of %d clients", epoch, block, len(answers), len(ids))
	}
	return answers, nil
}
