// The reserve's one-shot recorder binding consumes only the original approved
// call. Historical proxy initialization and present chain state stay distinct.
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/stabi"
)

// Both addresses derive from the already approved creation nonces. These review
// fields supply no new owner, target or transaction authority.
type contractReserveBinding struct {
	Reserve  common.Address `json:"reserve"`
	Recorder common.Address `json:"recorder"`
}

// Deployment action five calls the immutable bootstrap's reserve, with exactly
// the initialized proxy address and no value, at the same deployer's next nonce.
func contractReserveLinkAction(plan evmPhasePlan, reserve, proxy evmCreatePlan) (contractReserveBinding, error) {
	var result contractReserveBinding
	if len(plan.Actions) < 6 || proxy.ProxyConstructor == nil {
		return result, errors.New("reserve binding lacks the approved initialized proxy")
	}
	action, predecessor := plan.Actions[5], plan.Actions[4]
	result = contractReserveBinding{Reserve: crypto.CreateAddress(plan.Actions[0].Sender, plan.Actions[0].Nonce), Recorder: crypto.CreateAddress(predecessor.Sender, predecessor.Nonce)}
	if predecessor.Nonce == ^uint64(0) || action.Sender != predecessor.Sender || action.Nonce != predecessor.Nonce+1 || action.To == nil || *action.To != result.Reserve || reserve.Address != result.Reserve || proxy.Address != result.Recorder || action.ValueWei != "0" {
		return result, errors.New("reserve binding must call the derived reserve with zero value at the deployer's next nonce")
	}
	data, err := rootReceiptHex(action.Data, 36)
	if err != nil || !bytes.Equal(data, stabi.NewSTReserveSink().PackSetRecorderOnce(result.Recorder)) {
		return result, errors.New("reserve binding calldata differs from the exact initialized proxy call")
	}
	return result, nil
}

// The reserve runtime and immutable getters remain unchanged; the only allowed
// mutation is recorder. Principal stays zero until later installation activity.
func contractReserveLinkState(reserve evmCreatePlan, recorder common.Address) ([]contractGetter, []contractStorageWord) {
	contract := stabi.NewSTReserveSink()
	zero := "0x" + strings.Repeat("00", 32)
	word := common.BytesToHash(recorder[:]).Hex()
	getters := append([]contractGetter(nil), reserve.Getters...)
	selector := "0x" + hex.EncodeToString(contract.PackRecorder())
	for index := range getters {
		if getters[index].Data == selector {
			getters[index].Expected = word
		}
	}
	getters = append(getters, contractGetter{Data: "0x" + hex.EncodeToString(contract.PackPrincipal()), Expected: zero})
	// Reviewed STReserveSink layout: recorder is address at slot zero; principal
	// is the full uint256 at slot one. Immutable constructor fields use no slots.
	storage := []contractStorageWord{{Slot: zero, Expected: word}, {Slot: common.HexToHash("0x01").Hex(), Expected: zero}}
	return getters, storage
}

// The exact event position is retained with the fixed graph-derived binding;
// historical journal encodings omit both of the new optional receipt fields.
func evmReserveBindingHash(plan evmCreatePlan, receipt evmCreateReceipt) string {
	return rootObjectHash(struct {
		Binding  contractReserveBinding
		LogIndex uint64
	}{Binding: *plan.ReserveBinding, LogIndex: receipt.RecorderLogIndex})
}

// This reviewed function emits exactly one indexed RecorderFixed event and
// makes no external calls. Failure must leave no logs. Every identity is explicit.
func evmReserveLinkReceiptEvent(fields map[string]json.RawMessage, receipt *evmCreateReceipt, transactionIndex uint64, plan evmCreatePlan) error {
	raw, exists := fields["logs"]
	var logs []json.RawMessage
	if !exists || bytes.Equal(raw, []byte("null")) || json.Unmarshal(raw, &logs) != nil || len(logs) > 1 {
		return errors.New("reserve binding receipt lacks exact explicit event logs")
	}
	if receipt.Status == 0 {
		if len(logs) != 0 {
			return errors.New("reverted reserve binding receipt retains an event")
		}
		return nil
	}
	if len(logs) != 1 || plan.ReserveBinding == nil {
		return errors.New("successful reserve binding lacks its unique RecorderFixed event")
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
		return errors.New("reserve binding event identity or position differs")
	}
	binding, err := stabi.STReserveSinkMetaData.ParseABI()
	if err != nil {
		return err
	}
	if len(log.Topics) != 2 || log.Topics[0] != binding.Events["RecorderFixed"].ID.Hex() || log.Topics[1] != common.BytesToHash(plan.ReserveBinding.Recorder[:]).Hex() || log.Data != "0x" {
		return errors.New("reserve binding event differs from the exact indexed proxy")
	}
	receipt.RecorderLogIndex = index.Uint64()
	return nil
}

// Proxy policy normalization belongs to its original inclusion, not this later
// binding block. The time-varying currentEpoch getter is outside this stable
// initial-state projection; policy, ownership and all proxy slots remain exact.
func (self *evmOwnedChain) authenticateReserveRecorder(ctx context.Context, plan evmCreatePlan, block any) error {
	if plan.Proxy == nil || len(plan.Prerequisites) != 5 || plan.Prerequisites[4].Receipt == nil {
		return errors.New("reserve recorder lacks its retained proxy inclusion")
	}
	proxy := *plan.Proxy
	getters, err := proxy.receiptGetters(*plan.Prerequisites[4].Receipt)
	if err != nil {
		return err
	}
	var code string
	if err := self.read(ctx, "eth_getCode", []any{proxy.Address.Hex(), block}, &code); err != nil {
		return err
	}
	if code != "0x"+hex.EncodeToString(proxy.Runtime) {
		return errors.New("reserve recorder proxy runtime differs")
	}
	clock := "0x" + hex.EncodeToString(stabi.NewSTCoordinator().PackCurrentEpoch())
	for _, getter := range getters {
		if getter.Data == clock {
			continue
		}
		var output string
		if err := self.read(ctx, "eth_call", []any{map[string]any{"to": proxy.Address.Hex(), "data": getter.Data}, block}, &output); err != nil {
			return err
		}
		if output != getter.Expected {
			return errors.New("reserve recorder initialized proxy getter differs")
		}
	}
	for _, expected := range proxy.Storage {
		var output string
		if err := self.read(ctx, "eth_getStorageAt", []any{proxy.Address.Hex(), expected.Slot, block}, &output); err != nil {
			return err
		}
		if output != expected.Expected {
			return errors.New("reserve recorder initialized proxy storage differs")
		}
	}
	return self.authenticateProxyImplementation(ctx, proxy, block)
}

// Admission requires an unbound reserve at both the approved canonical head and
// pending state. A read failure stays unresolved; a known collision cannot send.
func (self *evmOwnedChain) admitReserveLinkTarget(ctx context.Context, plan evmCreatePlan, block map[string]any, pendingCode string) error {
	if pendingCode != "0x"+hex.EncodeToString(plan.Reserve.Runtime) {
		return errors.New("reserve binding pending target runtime differs")
	}
	getters, storage := contractReserveLinkState(*plan.Reserve, common.Address{})
	recorder := "0x" + hex.EncodeToString(stabi.NewSTReserveSink().PackRecorder())
	for index, selector := range []any{block, "pending"} {
		if index == 0 {
			var code string
			if err := self.read(ctx, "eth_getCode", []any{plan.Address.Hex(), selector}, &code); err != nil {
				return err
			}
			if code != pendingCode {
				return errors.New("reserve binding canonical target runtime differs")
			}
		}
		for _, getter := range getters {
			var output string
			if err := self.read(ctx, "eth_call", []any{map[string]any{"to": plan.Address.Hex(), "data": getter.Data}, selector}, &output); err != nil {
				return err
			}
			if output != getter.Expected {
				if getter.Data == recorder {
					word, err := rootReceiptHex(output, 32)
					if err != nil || len(word) != 32 || !bytes.Equal(word[:12], make([]byte, 12)) {
						return errors.New("reserve recorder observation is not a canonical address word")
					}
					return errors.New("reserve recorder is already fixed")
				}
				return errors.New("reserve binding target getter precondition differs")
			}
		}
		for _, expected := range storage {
			var output string
			if err := self.read(ctx, "eth_getStorageAt", []any{plan.Address.Hex(), expected.Slot, selector}, &output); err != nil {
				return err
			}
			if output != expected.Expected {
				return errors.New("reserve binding target storage precondition differs")
			}
		}
		if err := self.authenticateReserveRecorder(ctx, plan, selector); err != nil {
			return err
		}
	}
	return nil
}
