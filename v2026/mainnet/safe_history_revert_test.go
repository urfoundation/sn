// A published Safe proxy/singleton executes a real reverted delegatecall. Its
// transient storage and log effects demonstrate the archive census's hard limit.
package main

import (
	"encoding/hex"
	"errors"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/core/vm/runtime"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
)

// A public simulation can write Safe storage and emit a log internally, then
// revert both. Exact complete receipt bytes retain no proof of those effects.
func TestSafeHistoryCaptureKeepsRevertedDelegatecallUnproven(t *testing.T) {
	safe := newSafeExecutionFixture(t, "1.4.1", "Safe")
	address := safe.transaction.Safe
	slot := crypto.Keccak256Hash([]byte("synthetic orphan mapping entry"))
	code := append([]byte{0x60, 0x01, 0x7f}, slot[:]...)
	code = append(code, byte(vm.SSTORE), 0x60, 0x00, 0x60, 0x00, byte(vm.LOG0), byte(vm.STOP))
	safe.state.SetCode(safe.transaction.To, code, tracing.CodeChangeUnspecified)
	input, err := safe.oracleAbi.Pack("simulateAndRevert", safe.transaction.To, []byte{})
	if err != nil {
		t.Fatal(err)
	}
	delegated, transient := false, false
	safe.vm.EVMConfig.Tracer = &tracing.Hooks{
		OnEnter: func(_ int, kind byte, _, to common.Address, _ []byte, _ uint64, _ *big.Int) {
			if kind == byte(vm.DELEGATECALL) && to == safe.transaction.To {
				delegated = true
			}
		},
		OnOpcode: func(_ uint64, op byte, _, _ uint64, scope tracing.OpContext, _ []byte, _ int, _ error) {
			if op == byte(vm.LOG0) && scope.Address() == address && safe.state.GetState(address, slot) == common.BigToHash(big.NewInt(1)) {
				transient = true
			}
		},
	}
	logsBefore := len(safe.state.Logs())
	_, gasLeft, err := runtime.Call(address, input, &safe.vm)
	if !errors.Is(err, vm.ErrExecutionReverted) || !delegated || !transient || safe.state.GetState(address, slot) != (common.Hash{}) || len(safe.state.Logs()) != logsBefore {
		t.Fatalf("published Safe did not execute and erase transient delegatecall effects: %v delegated=%v transient=%v", err, delegated, transient)
	}
	client, archive := newSafeHistoryFixture(t)
	witness := archive.witnesses[0]
	key, err := crypto.ToECDSA(crypto.Keccak256([]byte("synthetic reverted archive transaction")))
	if err != nil {
		t.Fatal(err)
	}
	transaction, err := types.SignTx(types.NewTx(&types.DynamicFeeTx{ChainID: big.NewInt(mainnetEvmChainId), To: &address, Gas: safe.vm.GasLimit,
		GasFeeCap: big.NewInt(2), GasTipCap: big.NewInt(1), Value: new(big.Int), Data: input}), types.LatestSignerForChainID(big.NewInt(mainnetEvmChainId)), key)
	if err != nil {
		t.Fatal(err)
	}
	transactions := types.Transactions{transaction}
	receipts := types.Receipts{&types.Receipt{Type: transaction.Type(), Status: 0, CumulativeGasUsed: safe.vm.GasLimit - gasLeft, Logs: []*types.Log{}}}
	raw, _ := hex.DecodeString(witness.EvmHeader[2:])
	var header types.Header
	if err := rlp.DecodeBytes(raw, &header); err != nil {
		t.Fatal(err)
	}
	header.TxHash = types.DeriveSha(transactions, trie.NewStackTrie(nil))
	header.ReceiptHash = types.DeriveSha(receipts, trie.NewStackTrie(nil))
	header.GasUsed = receipts[0].CumulativeGasUsed
	raw, err = rlp.EncodeToBytes(&header)
	if err != nil {
		t.Fatal(err)
	}
	witness.EvmHeader = "0x" + hex.EncodeToString(raw)
	raw, err = rlp.EncodeToBytes(types.NewBlockWithHeader(&header).WithBody(types.Body{Transactions: transactions}))
	if err != nil {
		t.Fatal(err)
	}
	witness.EvmBlock = "0x" + hex.EncodeToString(raw)
	raw, err = receipts[0].MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	witness.EvmReceipts = []string{"0x" + hex.EncodeToString(raw)}
	witness.NativeHeader.Digest.Logs = []string{mappingTestDigest(t, 3, header.Hash().Hex(), nil)}
	safeHistoryTestSealNative(t, &witness)
	archive.witnesses = []safeHistoryWitness{witness}
	archive.scope = safeHistoryScope{Safe: strings.ToLower(address.Hex()), From: witness.Native, Through: witness.Native}
	capture, err := client.captureSafeHistory(t.Context(), archive.expected, archive.scope)
	if err != nil || len(capture.Blocks) != 1 || len(capture.Blocks[0].DirectCalls) != 1 || len(capture.Blocks[0].Logs) != 0 ||
		capture.InternalExecutionVerified || capture.DeploymentHistoryVerified || capture.SendAuthorized {
		t.Fatalf("complete reverted receipt census claimed invisible Safe history: %v", err)
	}
}
