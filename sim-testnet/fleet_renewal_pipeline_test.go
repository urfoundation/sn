package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

func TestFleetRenewalPipelineSubmitsExactNoncesBeforeFinality(t *testing.T) {
	key, err := crypto.HexToECDSA(strings.Repeat("8", 64))
	if err != nil {
		t.Fatal(err)
	}
	chain := big.NewInt(945)
	address := crypto.PubkeyToAddress(key.PublicKey)
	to := common.Address{9}
	data := []byte{1, 2, 3, 4}
	stateDir := filepath.Join(t.TempDir(), "state")
	journal, err := OpenJournal(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	var actions []Action
	var transactions []*ethTypes.Transaction
	receipts := map[string]*ethTypes.Receipt{}
	for i := 0; i < 2; i++ {
		a := Action{ID: fmt.Sprintf("fleet.renew.1.1.bind.%d", i+1), Kind: "evm-transaction", Target: to.Hex(), Parameters: map[string]string{"operation": "bind", "renewal_expected_nonce": fmt.Sprint(4 + i), "renewal_expected_signer": address.Hex(), "renewal_calldata": "0x01020304", evmMaximumGasUnitsParameter: "100000", evmMaximumFeePerGasParameter: "10"}, Spend: Spend{EVMGasWei: "1000000"}}
		a.IntentHash, err = actionIntentHash(a)
		if err != nil {
			t.Fatal(err)
		}
		tx, err := ethTypes.SignTx(ethTypes.NewTx(&ethTypes.DynamicFeeTx{ChainID: chain, Nonce: uint64(4 + i), GasTipCap: big.NewInt(2), GasFeeCap: big.NewInt(10), Gas: 55000, To: &to, Value: new(big.Int), Data: data}), ethTypes.LatestSignerForChainID(chain), key)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := tx.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		if err := atomicWrite(filepath.Join(stateDir, "transactions", stringsTrim0x(tx.Hash().Hex())+".rlp"), raw, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := journal.Append(JournalEntry{DeploymentID: "pipeline", PlanHash: "approved", ActionID: a.ID, IntentHash: a.IntentHash, Stage: StageBroadcast, Signer: address.Hex(), Nonce: fmt.Sprint(4 + i), TransactionHash: tx.Hash().Hex(), RecoveryBlock: 19, RecoveryBlockHash: common.Hash{0x19}.Hex()}); err != nil {
			t.Fatal(err)
		}
		actions = append(actions, a)
		transactions = append(transactions, tx)
		receipts[tx.Hash().Hex()] = &ethTypes.Receipt{Type: tx.Type(), Status: 1, TxHash: tx.Hash(), BlockNumber: big.NewInt(20), BlockHash: common.Hash{0x20}, GasUsed: 40000, CumulativeGasUsed: 40000, EffectiveGasPrice: big.NewInt(6), Logs: []*ethTypes.Log{}}
	}
	var submitted atomic.Int64
	var finalized atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var call struct {
			ID     json.RawMessage   `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&call); err != nil {
			t.Error(err)
			return
		}
		var result any
		switch call.Method {
		case "eth_getTransactionReceipt":
			if finalized.Load() {
				var hash string
				_ = json.Unmarshal(call.Params[0], &hash)
				result = receipts[hash]
			}
		case "eth_getBlockByNumber":
			number, hash := "0x14", common.Hash{0x20}
			if string(call.Params[0]) == `"finalized"` {
				number, hash = "0x15", common.Hash{0x21}
			}
			result = map[string]any{"number": number, "hash": hash}
		case "eth_getTransactionCount":
			result = "0x4"
		case "eth_sendRawTransaction":
			var raw hexutil.Bytes
			if err := json.Unmarshal(call.Params[0], &raw); err != nil {
				t.Error(err)
				return
			}
			var tx ethTypes.Transaction
			if err := tx.UnmarshalBinary(raw); err != nil {
				t.Error(err)
				return
			}
			index := submitted.Add(1) - 1
			if index >= 2 || tx.Hash() != transactions[index].Hash() {
				t.Error("pipeline submitted reordered or changed signed bytes")
			}
			result = tx.Hash().Hex()
		default:
			t.Error("unexpected fresh signing RPC " + call.Method)
			http.Error(w, "unexpected RPC", 400)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": call.ID, "result": result})
	}))
	defer server.Close()
	client, err := ethclient.Dial(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	manager := &EvmTxManager{client: client, chainID: chain, deploymentID: "pipeline", stateDir: stateDir, journal: journal, key: key}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for i, action := range actions {
		signed, err := manager.submitFleetRenewal(ctx, "approved", action, &to, new(big.Int), data)
		if err != nil || signed.Hash() != transactions[i].Hash() {
			t.Fatalf("ordered submission %d blocked or changed bytes: %v", i, err)
		}
	}
	if submitted.Load() != 2 {
		t.Fatal("second nonce waited for first transaction finality")
	}
	finalized.Store(true)
	jobs := []func(context.Context) error{}
	for i, action := range actions {
		tx := transactions[i]
		a := action
		jobs = append(jobs, func(ctx context.Context) error {
			_, err := manager.waitExactTransaction(ctx, "approved", a, tx)
			return err
		})
	}
	if err := runFleetRenewalJoined(ctx, jobs); err != nil {
		t.Fatal(err)
	}
	if submitted.Load() != 2 {
		t.Fatal("canonical reconciliation rebroadcast completed transactions")
	}
}

func TestFleetRenewalPipelineJoinsCanceledWorkers(t *testing.T) {
	started := make(chan struct{}, 3)
	fail := make(chan struct{})
	cleanup := make(chan struct{})
	done := make(chan error, 1)
	jobs := []func(context.Context) error{func(ctx context.Context) error { started <- struct{}{}; <-fail; return errors.New("owned failure") }}
	for i := 0; i < 2; i++ {
		jobs = append(jobs, func(ctx context.Context) error { started <- struct{}{}; <-ctx.Done(); <-cleanup; return ctx.Err() })
	}
	go func() { done <- runFleetRenewalJoined(context.Background(), jobs) }()
	for i := 0; i < 3; i++ {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("worker did not start")
		}
	}
	close(fail)
	select {
	case err := <-done:
		t.Fatalf("pipeline returned before owned cleanup: %v", err)
	default:
	}
	close(cleanup)
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "owned failure") {
			t.Fatalf("failure lost during join: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("pipeline did not join canceled workers")
	}
	if err := runFleetRenewalJoined(context.Background(), make([]func(context.Context) error, fleetRenewalMaximumInFlight+1)); err == nil {
		t.Fatal("unbounded wave admitted")
	}
}

func TestFleetRenewalFeeQuoteUsesExactApprovedCeiling(t *testing.T) {
	action := Action{ID: "fleet.renew.1.1.mirror", Parameters: map[string]string{evmMaximumGasUnitsParameter: "200000", evmMaximumFeePerGasParameter: "25000000000"}, Spend: Spend{EVMGasWei: "5000000000000000"}}
	fee, err := fleetRenewalQuotedFeeCap(action, big.NewInt(20_134_283_587), new(big.Int))
	if err != nil || fee.Uint64() != 25_000_000_000 {
		t.Fatalf("usable approved quote rejected: %v", err)
	}
	if _, err := fleetRenewalQuotedFeeCap(action, big.NewInt(25_000_000_001), new(big.Int)); err == nil {
		t.Fatal("price above exact approval accepted")
	}
	if _, err := fleetRenewalQuotedFeeCap(action, big.NewInt(24_000_000_000), big.NewInt(2_000_000_000)); err == nil {
		t.Fatal("tip pushed inclusion price above approval")
	}
}
