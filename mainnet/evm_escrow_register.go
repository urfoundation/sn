// Escrow registration calls the reviewed vault with an exact bounded value.
// Native mapping observations remain subject to the phase's owned-RPC trust.
package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/ss58"
	"github.com/urfoundation/sn/stabi"
)

// Review values derive only from already approved calldata and the vault's
// exact constructor. They add no unsigned cap, hotkey or funding authority.
type contractEscrowRegistration struct {
	MaximumBurnRao uint64 `json:"maximum_burn_rao"`
	FundingWei     string `json:"funding_wei"`
	Hotkey         string `json:"hotkey"`
	Coldkey        string `json:"coldkey"`
}

// Runtime source 67dcf7f's SubtensorEvmBalanceConverter scales rao by 10^9.
// Big integers preserve the uint64 cap without narrowing its wei product.
func contractEscrowAction(plan evmPhasePlan, vault evmCreatePlan) (contractEscrowRegistration, error) {
	var result contractEscrowRegistration
	if len(plan.Actions) < 4 || vault.VaultConstructor == nil {
		return result, errors.New("escrow registration lacks its approved vault constructor")
	}
	action, predecessor := plan.Actions[3], plan.Actions[2]
	address := crypto.CreateAddress(plan.Actions[1].Sender, plan.Actions[1].Nonce)
	if predecessor.Nonce == ^uint64(0) || action.Sender != predecessor.Sender || action.Nonce != predecessor.Nonce+1 || action.To == nil || *action.To != address || vault.Address != address {
		return result, errors.New("escrow registration must call the derived vault at the deployer's next nonce")
	}
	data, err := rootReceiptHex(action.Data, 36)
	if err != nil || len(data) != 36 {
		return result, errors.New("escrow registration calldata must be one exact uint64 call")
	}
	cap := binary.BigEndian.Uint64(data[28:36])
	if cap == 0 || !bytes.Equal(data, stabi.NewSTSettlementVault().PackRegisterEscrow(cap)) {
		return result, errors.New("escrow registration selector or canonical burn cap differs")
	}
	funding := new(big.Int).Mul(new(big.Int).SetUint64(cap), big.NewInt(1_000_000_000))
	if action.ValueWei != funding.String() {
		return result, errors.New("escrow registration value must equal its exact rao cap in wei")
	}
	mirror := ss58.EvmMirrorPubkey(address)
	return contractEscrowRegistration{MaximumBurnRao: cap, FundingWei: funding.String(), Hotkey: vault.VaultConstructor.EscrowHotkey, Coldkey: "0x" + hex.EncodeToString(mirror[:])}, nil
}

// Only the one-shot registration flag changes; the installation still has no
// coordinator binding or captured, paid, funded or outstanding accounting.
func contractEscrowGetters(vaultGetters []contractGetter) []contractGetter {
	getters := append([]contractGetter(nil), vaultGetters...)
	selector := "0x" + hex.EncodeToString(stabi.NewSTSettlementVault().PackEscrowRegistered())
	for index := range getters {
		if getters[index].Data == selector {
			getters[index].Expected = "0x" + strings.Repeat("00", 31) + "01"
		}
	}
	return getters
}

// The retained digest binds the exact event position and both registration
// identities. It records a bounded outcome, not measured burn or refund amounts.
func evmEscrowRegistrationHash(plan evmCreatePlan, receipt evmCreateReceipt) string {
	return rootObjectHash(struct {
		Registration contractEscrowRegistration
		Uid          uint16
		LogIndex     uint64
	}{Registration: *plan.EscrowRegistration, Uid: receipt.EscrowUid, LogIndex: receipt.EscrowLogIndex})
}

// Success must emit one exact vault event from this transaction and inclusion.
// Required zero-valued positions and removed=false must be explicitly present.
func evmEscrowReceiptEvent(fields map[string]json.RawMessage, receipt *evmCreateReceipt, transactionIndex uint64, plan evmCreatePlan) error {
	raw, exists := fields["logs"]
	var logs []json.RawMessage
	if !exists || bytes.Equal(raw, []byte("null")) || json.Unmarshal(raw, &logs) != nil || len(logs) > 1024 {
		return errors.New("escrow receipt lacks bounded explicit event logs")
	}
	if receipt.Status == 0 {
		if len(logs) != 0 {
			return errors.New("reverted escrow receipt retains events")
		}
		return nil
	}
	binding, err := stabi.STSettlementVaultMetaData.ParseABI()
	if err != nil {
		return err
	}
	event := binding.Events["EscrowRegistered"]
	matched := false
	seen := map[uint64]bool{}
	for _, raw := range logs {
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
		if err := json.Unmarshal(raw, &log); err != nil {
			return err
		}
		block, e1 := evmQuantity(log.BlockNumber, 64)
		position, e2 := evmQuantity(log.TransactionIndex, 64)
		index, e3 := evmQuantity(log.LogIndex, 64)
		if errors.Join(e1, e2, e3) != nil || log.Removed == nil || *log.Removed || !common.IsHexAddress(log.Address) || log.TransactionHash != receipt.TransactionHash || log.BlockHash != receipt.BlockHash || block.Uint64() != receipt.BlockNumber || position.Uint64() != transactionIndex || seen[index.Uint64()] {
			return errors.New("escrow receipt event identity or position differs")
		}
		seen[index.Uint64()] = true
		if common.HexToAddress(log.Address) != plan.Address || len(log.Topics) == 0 || log.Topics[0] != event.ID.Hex() {
			continue
		}
		data, err := rootReceiptHex(log.Data, 32)
		if matched || len(log.Topics) != 2 || log.Topics[1] != plan.EscrowRegistration.Hotkey || err != nil || len(data) != 32 || !bytes.Equal(data[:30], make([]byte, 30)) {
			return errors.New("escrow registration event differs from approved hotkey or uint16 uid")
		}
		receipt.EscrowUid, receipt.EscrowLogIndex = binary.BigEndian.Uint16(data[30:]), index.Uint64()
		matched = true
	}
	if !matched {
		return errors.New("successful escrow receipt lacks its unique registration event")
	}
	return nil
}

// Reviewed neuron/metagraph interfaces map hotkey -> uid and uid -> hotkey/owner.
// UID zero is valid. Every read uses the same authenticated inclusion hash.
func (self *evmOwnedChain) authenticateEscrowRegistration(ctx context.Context, plan evmCreatePlan, receipt evmCreateReceipt, block map[string]any) error {
	word := func(value uint16) []byte {
		result := make([]byte, 32)
		binary.BigEndian.PutUint16(result[30:], value)
		return result
	}
	netuid, uid := word(plan.Config.Plan.Netuid), word(receipt.EscrowUid)
	hotkey, _ := rootReceiptHex(plan.EscrowRegistration.Hotkey, 32)
	registered := append(word(1), uid...)
	for _, query := range []struct {
		target    string
		signature string
		second    []byte
		expected  string
	}{
		{target: "0x0000000000000000000000000000000000000804", signature: "getUid(uint16,bytes32)", second: hotkey, expected: "0x" + hex.EncodeToString(registered)},
		{target: "0x0000000000000000000000000000000000000802", signature: "getHotkey(uint16,uint16)", second: uid, expected: plan.EscrowRegistration.Hotkey},
		{target: "0x0000000000000000000000000000000000000802", signature: "getColdkey(uint16,uint16)", second: uid, expected: plan.EscrowRegistration.Coldkey},
	} {
		data := append(append(crypto.Keccak256([]byte(query.signature))[:4], netuid...), query.second...)
		var output string
		if err := self.read(ctx, "eth_call", []any{map[string]any{"to": query.target, "data": "0x" + hex.EncodeToString(data)}, block}, &output); err != nil {
			return err
		}
		if output != query.expected {
			return errors.New("escrow canonical registration mapping differs")
		}
	}
	return nil
}

// Current and pending target state must still be the exact unregistered vault.
// This is an existing call target, so the CREATE empty-code rule cannot apply.
func (self *evmOwnedChain) admitEscrowTarget(ctx context.Context, plan evmCreatePlan, block map[string]any, pendingCode string) error {
	if pendingCode != "0x"+hex.EncodeToString(plan.Vault.Runtime) {
		return errors.New("escrow pending vault runtime differs")
	}
	for index, selector := range []any{block, "pending"} {
		if index == 0 {
			var code string
			if err := self.read(ctx, "eth_getCode", []any{plan.Address.Hex(), selector}, &code); err != nil {
				return err
			}
			if code != pendingCode {
				return errors.New("escrow canonical vault runtime differs")
			}
		}
		for _, getter := range plan.Vault.Getters {
			var output string
			if err := self.read(ctx, "eth_call", []any{map[string]any{"to": plan.Address.Hex(), "data": getter.Data}, selector}, &output); err != nil {
				return err
			}
			if output != getter.Expected {
				return errors.New("escrow registration target precondition differs")
			}
		}
		netuid := make([]byte, 32)
		binary.BigEndian.PutUint16(netuid[30:], plan.Config.Plan.Netuid)
		hotkey, _ := rootReceiptHex(plan.EscrowRegistration.Hotkey, 32)
		data := append(append(crypto.Keccak256([]byte("getUid(uint16,bytes32)"))[:4], netuid...), hotkey...)
		var output string
		if err := self.read(ctx, "eth_call", []any{map[string]any{"to": "0x0000000000000000000000000000000000000804", "data": "0x" + hex.EncodeToString(data)}, selector}, &output); err != nil {
			return err
		}
		if output != "0x"+strings.Repeat("00", 64) {
			return errors.New("escrow hotkey is already registered or its absence is unresolved")
		}
	}
	return nil
}
