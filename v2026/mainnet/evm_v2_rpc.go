// Settlement v2 readback compares exact runtime code, every getter and every
// reviewed storage word with the approved projections at one block selector.
// Missing archive values remain read errors, never a completed installation.
package main

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// One contract's exact runtime, getters and storage words at one selector.
func (self *evmOwnedChain) readback(ctx context.Context, contract evmCreatePlan, block any) error {
	name := contract.actionName()
	var code string
	if err := self.read(ctx, "eth_getCode", []any{contract.Address.Hex(), block}, &code); err != nil {
		return err
	}
	if code != "0x"+hex.EncodeToString(contract.Runtime) {
		return fmt.Errorf("%s readback runtime differs", name)
	}
	for _, getter := range contract.Getters {
		var output string
		if err := self.read(ctx, "eth_call", []any{map[string]any{"to": getter.target(contract.Address), "data": getter.Data}, block}, &output); err != nil {
			return err
		}
		if output != getter.Expected {
			return fmt.Errorf("%s readback getter %s differs", name, getter.Data)
		}
	}
	for _, word := range contract.Storage {
		var output string
		if err := self.read(ctx, "eth_getStorageAt", []any{contract.Address.Hex(), word.Slot, block}, &output); err != nil {
			return err
		}
		if output != word.Expected {
			return fmt.Errorf("%s readback storage slot %s differs", name, word.Slot)
		}
	}
	return nil
}

// Completion reads back the initialized proxy and its implementation at the
// binding's selector, beside vault2's own exact bound state.
func (self *evmOwnedChain) readbackSettlement2(ctx context.Context, plan evmCreatePlan, block any) error {
	if plan.Proxy == nil || plan.Coordinator == nil {
		return errors.New("settlement2 readback lacks its approved proxy and implementation")
	}
	if err := self.readback(ctx, *plan.Proxy, block); err != nil {
		return err
	}
	return self.readback(ctx, *plan.Coordinator, block)
}

// The one-shot binding needs a registered, unbound vault2 and the exact
// initialized proxy and implementation in both canonical and pending state.
// A coordinator that is already fixed, even to this proxy, prevents a send.
func (self *evmOwnedChain) admitVault2LinkTarget(ctx context.Context, plan evmCreatePlan, block map[string]any) error {
	if plan.Escrow == nil {
		return errors.New("vault2 binding lacks its registered vault2 projection")
	}
	for _, selector := range []any{block, "pending"} {
		if err := self.readback(ctx, *plan.Escrow, selector); err != nil {
			return err
		}
		if err := self.readbackSettlement2(ctx, plan, selector); err != nil {
			return err
		}
	}
	return nil
}

// Vault2 is created only while its escrow hotkey has no UID, at the canonical
// head and in pending state; a hotkey registered by anyone else can never
// become vault2's escrow, so neither vault2 nor its registration is sent.
func (self *evmOwnedChain) admitVault2Create(ctx context.Context, plan evmCreatePlan, block map[string]any) error {
	netuid, hotkey := common.BigToHash(big.NewInt(int64(plan.Config.Plan.Netuid))), common.HexToHash(plan.Vault2Constructor.EscrowHotkey)
	data := append(append(crypto.Keccak256([]byte("getUid(uint16,bytes32)"))[:4], netuid[:]...), hotkey[:]...)
	for _, selector := range []any{block, "pending"} {
		var output string
		if err := self.read(ctx, "eth_call", []any{map[string]any{"to": "0x0000000000000000000000000000000000000804", "data": "0x" + hex.EncodeToString(data)}, selector}, &output); err != nil {
			return err
		}
		if output != "0x"+strings.Repeat("00", 64) {
			return errors.New("vault2 escrow hotkey is already registered or its absence is unresolved")
		}
	}
	return nil
}

// The escrow registration's original bytes, written once right after vault2's
// own send under the same approval scope and a retained attempt (A7).
func (self *evmOwnedChain) submitEscrow(ctx context.Context, plan evmCreatePlan, record evmActionRecord) error {
	if self.configHash != rootObjectHash(plan.Config) || !plan.actionIs("vault2-create") || record.EscrowAttempts == 0 {
		return errors.New("escrow registration write lacks vault2 scope or a retained attempt")
	}
	if err := record.validateForAction(plan.Config, plan.ActionIndex); err != nil {
		return err
	}
	_, err := ownedSubmissionPost(ctx, self.client, plan.Config.Plan.Route, "eth_sendRawTransaction", record.EscrowSigned, record.EscrowTransactionHash)
	return err
}
