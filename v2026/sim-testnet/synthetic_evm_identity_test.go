// Synthetic EVM identities must survive each live evidence producer, rather
// than being replaced with a locally reconstructed Ethereum header hash.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// Provides controlled public-RPC responses at the real producer boundary.
func syntheticIdentityTestRPC(t *testing.T, call func(string, []json.RawMessage) (any, error)) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer request.Body.Close()
		var envelope struct {
			ID     json.RawMessage   `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(request.Body).Decode(&envelope); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		result, err := call(envelope.Method, envelope.Params)
		response := map[string]any{"jsonrpc": "2.0", "id": envelope.ID}
		if err != nil {
			response["error"] = map[string]any{"code": -32000, "message": err.Error()}
		} else {
			response["result"] = result
		}
		if err := json.NewEncoder(writer).Encode(response); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(server.Close)
	return server.URL
}

// Returns complete header fields so a regression to Header.Hash produces an
// identity mismatch, rather than merely failing on a missing header field.
func syntheticIdentityTestBlock(t *testing.T, head ChainHead) map[string]any {
	t.Helper()
	header := &types.Header{Number: new(big.Int).SetUint64(head.Number), Difficulty: big.NewInt(0), Extra: []byte("synthetic")}
	if strings.EqualFold(header.Hash().Hex(), head.Hash) {
		t.Fatal("fixture must distinguish RPC identity from Header.Hash")
	}
	encoded, err := json.Marshal(header)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	fields["hash"] = head.Hash
	return fields
}

// Canonical quantity syntax and nonzero complete hashes are required even
// when the rest of the response can be decoded as an Ethereum header.
func TestSyntheticEVMIdentityRejectsMalformedRPCFields(t *testing.T) {
	cases := []*evmRPCBlock{
		nil, {},
		{Number: "10", Hash: finalTestHex(0x10)},
		{Number: "0x010", Hash: finalTestHex(0x10)},
		{Number: "0x10000000000000000", Hash: finalTestHex(0x10)},
		{Number: "0x11", Hash: finalTestHex(0x10)},
		{Number: "0x10", Hash: strings.Repeat("10", 32)},
		{Number: "0x10", Hash: common.Hash{}.Hex()},
		{Number: "0x10", Hash: "0x01"},
		{Number: "0x10", Hash: "0x" + strings.Repeat("gg", 32)},
	}
	for index, block := range cases {
		if head, err := decodeEVMRPCBlock(block, big.NewInt(16)); err == nil {
			t.Errorf("case %d accepted malformed identity %+v", index, head)
		}
	}
}

// Keeps the complete launch-scale identity census while supplying each stake
// read locally; only the EVM block identity is under test.
func syntheticRewardStakeCaptureFixture(t *testing.T) (*ResolvedConfig, string, *ContractDeployment, ChainHead, ChainHead, int) {
	t.Helper()
	cfg := testResolvedConfig(t)
	root := t.TempDir()
	deployment := &ContractDeployment{DeploymentID: cfg.Config.Deployment.DeploymentID, SettlementVault: common.HexToAddress("0x1234"), ReserveSink: common.HexToAddress("0x5678")}
	identities := finalPublicIdentities{Schema: "urnetwork-sim-public-identities-v1", DeploymentID: deployment.DeploymentID, Substrate: map[string]finalPublicIdentity{}}
	add := func(label string) {
		identities.Substrate[label] = finalPublicIdentity{PublicKey: fmt.Sprintf("0x%064x", len(identities.Substrate)+1)}
	}
	for fleet := 1; fleet <= cfg.Config.Topology.fleetCandidates(); fleet++ {
		add(fleetHotkeyLabel(fleet))
		add(fleetColdkeyLabel(fleet))
	}
	for churn := 1; churn <= cfg.Config.Topology.ChurnFloorUIDs; churn++ {
		add(churnHotkeyLabel(churn))
		add(churnColdkeyLabel(churn))
	}
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		add(fmt.Sprintf("operator-%d-pool-hotkey", operator))
	}
	for validator := 1; validator <= cfg.Config.Topology.Validators; validator++ {
		add(validatorHotkeyLabel(validator))
		add(fmt.Sprintf("validator-%d-coldkey", validator))
	}
	if err := writePublicJSON(filepath.Join(root, "public", "identities.json"), identities); err != nil {
		t.Fatal(err)
	}
	pairs, err := finalRewardStakePairs(cfg, deployment, &identities)
	if err != nil {
		t.Fatal(err)
	}
	return cfg, root, deployment, ChainHead{Number: 10, Hash: finalTestHex(0x11)}, ChainHead{Number: 10, Hash: finalTestHex(0x22)}, len(pairs)
}

// Same-height reward snapshots retain the synthetic EVM hash independently
// of both the native checkpoint hash and the recomputed Ethereum header hash.
func TestSyntheticEVMRewardStakeCapturePreservesRPCIdentity(t *testing.T) {
	cfg, root, deployment, nativeHead, evmHead, pairCount := syntheticRewardStakeCaptureFixture(t)
	block := syntheticIdentityTestBlock(t, evmHead)
	var stakeCalls atomic.Int64
	cfg.OperationalEVM = syntheticIdentityTestRPC(t, func(method string, parameters []json.RawMessage) (any, error) {
		switch method {
		case "eth_getBlockByNumber":
			return block, nil
		case "eth_call":
			stakeCalls.Add(1)
			if len(parameters) != 2 || string(parameters[1]) != `"0xa"` {
				return nil, fmt.Errorf("wrong reward stake checkpoint %s", parameters)
			}
			return "0x" + fmt.Sprintf("%064x", 123), nil
		default:
			return nil, fmt.Errorf("unexpected reward stake method %s", method)
		}
	})
	hash, snapshots, err := captureFinalRewardStakeSnapshots(context.Background(), cfg, root, deployment, evmHead, []ChainHead{nativeHead})
	if err != nil || hash == "" || len(snapshots) != 1 {
		t.Fatalf("synthetic reward capture hash=%s snapshots=%+v error=%v", hash, snapshots, err)
	}
	if snapshots[0].EVMHead != evmHead || snapshots[0].NativeHead != nativeHead || len(snapshots[0].Positions) != pairCount || stakeCalls.Load() != int64(pairCount) {
		t.Fatalf("synthetic reward snapshot %+v, stake calls=%d want=%d", snapshots[0], stakeCalls.Load(), pairCount)
	}
}

// A same-height fork introduced after the stake reads invalidates capture.
func TestSyntheticEVMRewardStakeCaptureRejectsReorg(t *testing.T) {
	cfg, root, deployment, nativeHead, evmHead, _ := syntheticRewardStakeCaptureFixture(t)
	block := syntheticIdentityTestBlock(t, evmHead)
	replacement := syntheticIdentityTestBlock(t, ChainHead{Number: evmHead.Number, Hash: finalTestHex(0x33)})
	var stakeRead atomic.Bool
	cfg.OperationalEVM = syntheticIdentityTestRPC(t, func(method string, _ []json.RawMessage) (any, error) {
		switch method {
		case "eth_getBlockByNumber":
			if stakeRead.Load() {
				return replacement, nil
			}
			return block, nil
		case "eth_call":
			stakeRead.Store(true)
			return "0x" + fmt.Sprintf("%064x", 123), nil
		default:
			return nil, fmt.Errorf("unexpected reward stake method %s", method)
		}
	})
	_, snapshots, err := captureFinalRewardStakeSnapshots(context.Background(), cfg, root, deployment, evmHead, []ChainHead{nativeHead})
	if err == nil || !strings.Contains(err.Error(), "changed during capture") || snapshots != nil {
		t.Fatalf("reorged reward snapshots=%+v error=%v", snapshots, err)
	}
}

// Supplies a signed release transaction and log at one synthetic checkpoint.
func syntheticEVMLogCaptureFixture(t *testing.T, reorg bool) (*ResolvedConfig, ChainHead, common.Address) {
	t.Helper()
	cfg := testResolvedConfig(t)
	head := ChainHead{Number: 10, Hash: finalTestHex(0x22)}
	block := syntheticIdentityTestBlock(t, head)
	replacement := syntheticIdentityTestBlock(t, ChainHead{Number: head.Number, Hash: finalTestHex(0x33)})
	address := common.HexToAddress("0x1234")
	key, err := crypto.ToECDSA(bytes.Repeat([]byte{0x11}, 32))
	if err != nil {
		t.Fatal(err)
	}
	transaction, err := types.SignNewTx(key, types.LatestSignerForChainID(new(big.Int).SetUint64(cfg.ChainID)), &types.LegacyTx{To: &address, Gas: 100_000, GasPrice: big.NewInt(1), Value: big.NewInt(0), Data: []byte{0x12}})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(transaction)
	if err != nil {
		t.Fatal(err)
	}
	var transactionFields map[string]any
	if err := json.Unmarshal(encoded, &transactionFields); err != nil {
		t.Fatal(err)
	}
	transactionFields["blockHash"], transactionFields["blockNumber"] = head.Hash, hexutil.EncodeUint64(head.Number)
	log := types.Log{Address: address, Topics: []common.Hash{common.HexToHash(finalTestHex(0x44))}, Data: []byte{0x01}, BlockNumber: head.Number, BlockHash: common.HexToHash(head.Hash), TxHash: transaction.Hash()}
	var transactionRead atomic.Bool
	cfg.OperationalEVM = syntheticIdentityTestRPC(t, func(method string, _ []json.RawMessage) (any, error) {
		switch method {
		case "eth_getBlockByNumber":
			if reorg && transactionRead.Load() {
				return replacement, nil
			}
			return block, nil
		case "eth_getLogs":
			return []types.Log{log}, nil
		case "eth_getTransactionByHash":
			transactionRead.Store(true)
			return transactionFields, nil
		default:
			return nil, fmt.Errorf("unexpected log capture method %s", method)
		}
	})
	return cfg, head, address
}

// Final log capture accepts the actual hash used in synthetic EVM logs.
func TestSyntheticEVMLogCapturePreservesRPCIdentity(t *testing.T) {
	cfg, head, address := syntheticEVMLogCaptureFixture(t, false)
	logs, transactions, err := captureFinalEVMLogs(context.Background(), cfg, head.Number, []common.Address{address}, head)
	if err != nil || len(logs) != 1 || len(transactions) != 1 || logs[0].BlockHash != head.Hash || transactions[0].Block != head {
		t.Fatalf("synthetic log capture logs=%+v transactions=%+v error=%v", logs, transactions, err)
	}
}

// An explicit transition after the transaction response forces the capture
// recheck to observe a fork, independent of timing or scheduler behavior.
func TestSyntheticEVMLogCaptureRejectsReorg(t *testing.T) {
	cfg, head, address := syntheticEVMLogCaptureFixture(t, true)
	logs, transactions, err := captureFinalEVMLogs(context.Background(), cfg, head.Number, []common.Address{address}, head)
	if err == nil || !strings.Contains(err.Error(), "changed during capture") || logs != nil || transactions != nil {
		t.Fatalf("reorged log capture logs=%+v transactions=%+v error=%v", logs, transactions, err)
	}
}

// Builds an initializer-journal baseline with exact historical hash selectors.
func syntheticEVMBaselineFixture(t *testing.T, reorg bool) (*ResolvedConfig, string, *SetupPlan, ChainHead, []finalCanonicalEVMLog) {
	t.Helper()
	cfg := testResolvedConfig(t)
	root := filepath.Join(t.TempDir(), "state")
	head := ChainHead{Number: 10, Hash: finalTestHex(0x22)}
	block := syntheticIdentityTestBlock(t, head)
	replacement := syntheticIdentityTestBlock(t, ChainHead{Number: head.Number, Hash: finalTestHex(0x33)})
	proxy, implementation := common.HexToAddress("0x1234"), common.HexToAddress("0x5678")
	code := []byte{0x60, 0x00, 0x56}
	action := Action{ID: "evm.coordinator-proxy", Kind: "evm-transaction", IntentHash: finalTestHex(0x44)}
	plan := &SetupPlan{PlanHash: finalTestHex(0x55), DeploymentID: cfg.Config.Deployment.DeploymentID, ChainID: cfg.ChainID, Netuid: cfg.Netuid, Actions: []Action{action}, Deployment: ContractDeployment{CoordinatorProxy: proxy, CoordinatorImplementation: implementation, RuntimeHashes: map[string]string{implementation.Hex(): crypto.Keccak256Hash(code).Hex()}}}
	journal, err := OpenJournal(root)
	if err != nil {
		t.Fatal(err)
	}
	entry := JournalEntry{DeploymentID: plan.DeploymentID, PlanHash: plan.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageFinalized, TransactionHash: finalTestHex(0x66), BlockNumber: head.Number, BlockHash: head.Hash}
	if err := journal.Append(entry); err != nil {
		t.Fatal(err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	logs := []finalCanonicalEVMLog{{Address: strings.ToLower(proxy.Hex()), Topics: []string{crypto.Keccak256Hash([]byte("Upgraded(address)")).Hex(), common.BytesToHash(implementation.Bytes()).Hex()}, Data: "0x", BlockNumber: head.Number, BlockHash: head.Hash, TransactionHash: entry.TransactionHash}}
	var stateReads atomic.Int64
	cfg.OperationalEVM = syntheticIdentityTestRPC(t, func(method string, parameters []json.RawMessage) (any, error) {
		if method == "eth_getBlockByNumber" {
			if reorg && stateReads.Load() == 3 {
				return replacement, nil
			}
			return block, nil
		}
		if method != "eth_getStorageAt" && method != "eth_getCode" {
			return nil, fmt.Errorf("unexpected baseline method %s", method)
		}
		var selector finalEVMBlockSelector
		if len(parameters) < 2 || json.Unmarshal(parameters[len(parameters)-1], &selector) != nil || selector.BlockHash != head.Hash || !selector.RequireCanonical {
			return nil, fmt.Errorf("historical state did not use its exact synthetic hash: %s", parameters)
		}
		stateReads.Add(1)
		if method == "eth_getStorageAt" {
			return common.BytesToHash(implementation.Bytes()).Hex(), nil
		}
		return hexutil.Encode(code), nil
	})
	return cfg, root, plan, head, logs
}

// Historical initializer evidence preserves the explicit RPC block hash.
func TestSyntheticEVMBaselineCapturePreservesRPCIdentity(t *testing.T) {
	cfg, root, plan, head, logs := syntheticEVMBaselineFixture(t, false)
	baselines, err := captureFinalHistoricalCoordinatorBaselines(context.Background(), cfg, root, plan, ChainHead{Number: head.Number + 1, Hash: finalTestHex(0x77)}, logs)
	if err != nil || len(baselines) != 1 || baselines[0].Head != head {
		t.Fatalf("synthetic historical baseline=%+v error=%v", baselines, err)
	}
}

// A fork after the final historical code read invalidates the whole baseline.
func TestSyntheticEVMBaselineCaptureRejectsReorg(t *testing.T) {
	cfg, root, plan, head, logs := syntheticEVMBaselineFixture(t, true)
	baselines, err := captureFinalHistoricalCoordinatorBaselines(context.Background(), cfg, root, plan, ChainHead{Number: head.Number + 1, Hash: finalTestHex(0x77)}, logs)
	if err == nil || !strings.Contains(err.Error(), "changed during capture") || baselines != nil {
		t.Fatalf("reorged historical baseline=%+v error=%v", baselines, err)
	}
}
