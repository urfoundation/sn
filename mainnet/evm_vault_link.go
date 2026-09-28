// Vault binding fixes the initialized proxy once while retaining the packed
// escrow-registration flag and all original predecessor custody.
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/stabi"
)

// Both derived addresses belong to the original approved installation graph.
type contractVaultBinding struct {
	Vault       common.Address `json:"vault"`
	Coordinator common.Address `json:"coordinator"`
}

// Deployment action six calls the registered vault with exactly the initialized
// proxy address and zero value, after the same bootstrap's reserve-binding nonce.
func contractVaultLinkAction(plan evmPhasePlan, vault, proxy evmCreatePlan) (contractVaultBinding, error) {
	var result contractVaultBinding
	if len(plan.Actions) < 7 || proxy.ProxyConstructor == nil {
		return result, errors.New("vault binding lacks the approved initialized proxy")
	}
	action, predecessor := plan.Actions[6], plan.Actions[5]
	result = contractVaultBinding{Vault: crypto.CreateAddress(plan.Actions[1].Sender, plan.Actions[1].Nonce), Coordinator: crypto.CreateAddress(plan.Actions[4].Sender, plan.Actions[4].Nonce)}
	if predecessor.Nonce == ^uint64(0) || action.Sender != predecessor.Sender || action.Nonce != predecessor.Nonce+1 || action.To == nil || *action.To != result.Vault || vault.Address != result.Vault || proxy.Address != result.Coordinator || action.ValueWei != "0" {
		return result, errors.New("vault binding must call the derived vault with zero value at the deployer's next nonce")
	}
	data, err := rootReceiptHex(action.Data, 36)
	if err != nil || !bytes.Equal(data, stabi.NewSTSettlementVault().PackSetCoordinatorOnce(result.Coordinator)) {
		return result, errors.New("vault binding calldata differs from the exact initialized proxy call")
	}
	return result, nil
}

// Coordinator and escrowRegistered share slot zero. The flag's byte at offset
// twenty stays one; the reentrancy guard and five full accounting words stay zero.
func contractVaultLinkState(vault evmCreatePlan, coordinator common.Address) ([]contractGetter, []contractStorageWord) {
	getters := contractEscrowGetters(vault.Getters)
	selector := "0x" + hex.EncodeToString(stabi.NewSTSettlementVault().PackCoordinator())
	for index := range getters {
		if getters[index].Data == selector {
			getters[index].Expected = common.BytesToHash(coordinator[:]).Hex()
		}
	}
	packed := common.BytesToHash(coordinator[:])
	packed[11] = 1
	zero := "0x" + strings.Repeat("00", 32)
	storage := []contractStorageWord{{Slot: zero, Expected: packed.Hex()}}
	for _, slot := range []uint64{1, 8, 9, 10, 11, 12} {
		storage = append(storage, contractStorageWord{Slot: common.BigToHash(new(big.Int).SetUint64(slot)).Hex(), Expected: zero})
	}
	return getters, storage
}

// The derived vault/proxy pair and exact event position seal retained binding.
func evmVaultBindingHash(plan evmCreatePlan, receipt evmCreateReceipt) string {
	return rootObjectHash(struct {
		Binding  contractVaultBinding
		LogIndex uint64
	}{Binding: *plan.VaultBinding, LogIndex: receipt.CoordinatorLogIndex})
}

// The reviewed setter makes no external calls and emits one exact indexed event.
// Its explicit identity and removed flag cannot be inferred from receipt status.
func evmVaultLinkReceiptEvent(fields map[string]json.RawMessage, receipt *evmCreateReceipt, transactionIndex uint64, plan evmCreatePlan) error {
	raw, exists := fields["logs"]
	var logs []json.RawMessage
	if !exists || bytes.Equal(raw, []byte("null")) || json.Unmarshal(raw, &logs) != nil || len(logs) > 1 {
		return errors.New("vault binding receipt lacks exact explicit event logs")
	}
	if receipt.Status == 0 {
		if len(logs) != 0 {
			return errors.New("reverted vault binding receipt retains an event")
		}
		return nil
	}
	if len(logs) != 1 || plan.VaultBinding == nil {
		return errors.New("successful vault binding lacks its unique CoordinatorFixed event")
	}
	var log struct {
		Address          string   `json:"address"`
		Topics           []string `json:"topics"`
		Data             string   `json:"data"`
		TransactionHash  string   `json:"transactionHash"`
		BlockHash        string   `json:"blockHash"`
		BlockNumber      string   `json:"blockNumber"`
		TransactionIndex string   `json:"transactionIndex"`
		LogIndex         string   `json:"logIndex"`
		Removed          *bool    `json:"removed"`
	}
	if err := json.Unmarshal(logs[0], &log); err != nil {
		return err
	}
	block, e1 := evmQuantity(log.BlockNumber, 64)
	position, e2 := evmQuantity(log.TransactionIndex, 64)
	index, e3 := evmQuantity(log.LogIndex, 64)
	if errors.Join(e1, e2, e3) != nil || log.Removed == nil || *log.Removed || !common.IsHexAddress(log.Address) || common.HexToAddress(log.Address) != plan.Address || log.TransactionHash != receipt.TransactionHash || log.BlockHash != receipt.BlockHash || block.Uint64() != receipt.BlockNumber || position.Uint64() != transactionIndex {
		return errors.New("vault binding event identity or position differs")
	}
	binding, err := stabi.STSettlementVaultMetaData.ParseABI()
	if err != nil {
		return err
	}
	if len(log.Topics) != 2 || log.Topics[0] != binding.Events["CoordinatorFixed"].ID.Hex() || log.Topics[1] != common.BytesToHash(plan.VaultBinding.Coordinator[:]).Hex() || log.Data != "0x" {
		return errors.New("vault binding event differs from the exact indexed proxy")
	}
	receipt.CoordinatorLogIndex = index.Uint64()
	return nil
}

// The reserve must still name this proxy with unchanged principal at the same
// present or inclusion block. Proxy policy retains its own original EVM height.
func (self *evmOwnedChain) authenticateVaultCoordinator(ctx context.Context, plan evmCreatePlan, block any) error {
	if plan.ReserveLink == nil || len(plan.Prerequisites) != 6 {
		return errors.New("vault coordinator lacks completed reserve-binding custody")
	}
	reserve := *plan.ReserveLink
	var code string
	if err := self.read(ctx, "eth_getCode", []any{reserve.Address.Hex(), block}, &code); err != nil {
		return err
	}
	if code != "0x"+hex.EncodeToString(reserve.Runtime) {
		return errors.New("vault coordinator bound reserve runtime differs")
	}
	for _, getter := range reserve.Getters {
		var output string
		if err := self.read(ctx, "eth_call", []any{map[string]any{"to": reserve.Address.Hex(), "data": getter.Data}, block}, &output); err != nil {
			return err
		}
		if output != getter.Expected {
			return errors.New("vault coordinator bound reserve getter differs")
		}
	}
	for _, expected := range reserve.Storage {
		var output string
		if err := self.read(ctx, "eth_getStorageAt", []any{reserve.Address.Hex(), expected.Slot, block}, &output); err != nil {
			return err
		}
		if output != expected.Expected {
			return errors.New("vault coordinator bound reserve storage differs")
		}
	}
	reserve.Prerequisites = plan.Prerequisites[:5]
	return self.authenticateReserveRecorder(ctx, reserve, block)
}

// One-shot admission is checked independently of every historical receipt.
// Canonical and pending reads must show an unbound, still registered vault.
func (self *evmOwnedChain) admitVaultLinkTarget(ctx context.Context, plan evmCreatePlan, block map[string]any, pendingCode string) error {
	if pendingCode != "0x"+hex.EncodeToString(plan.Vault.Runtime) {
		return errors.New("vault binding pending target runtime differs")
	}
	getters, storage := contractVaultLinkState(*plan.Vault, common.Address{})
	coordinator := "0x" + hex.EncodeToString(stabi.NewSTSettlementVault().PackCoordinator())
	for index, selector := range []any{block, "pending"} {
		if index == 0 {
			var code string
			if err := self.read(ctx, "eth_getCode", []any{plan.Address.Hex(), selector}, &code); err != nil {
				return err
			}
			if code != pendingCode {
				return errors.New("vault binding canonical target runtime differs")
			}
		}
		for _, getter := range getters {
			var output string
			if err := self.read(ctx, "eth_call", []any{map[string]any{"to": plan.Address.Hex(), "data": getter.Data}, selector}, &output); err != nil {
				return err
			}
			if output != getter.Expected {
				if getter.Data == coordinator {
					word, err := rootReceiptHex(output, 32)
					if err != nil || len(word) != 32 || !bytes.Equal(word[:12], make([]byte, 12)) {
						return errors.New("vault coordinator observation is not a canonical address word")
					}
					return errors.New("vault coordinator is already fixed")
				}
				return errors.New("vault binding target getter precondition differs")
			}
		}
		for _, expected := range storage {
			var output string
			if err := self.read(ctx, "eth_getStorageAt", []any{plan.Address.Hex(), expected.Slot, selector}, &output); err != nil {
				return err
			}
			if output != expected.Expected {
				return errors.New("vault binding target storage precondition differs")
			}
		}
		if err := self.authenticateVaultCoordinator(ctx, plan, selector); err != nil {
			return err
		}
	}
	return nil
}
