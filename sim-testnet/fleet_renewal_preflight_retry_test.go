// Deterministic preflight and submission races retain exact signed authority.
// Synthetic transports advance checkpoints at the real Rpc call boundaries.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
)

// Read-only fake capabilities cannot accidentally sign or broadcast.
type fleetPreflightNonceReader func(context.Context, common.Address) (uint64, error)

// Exercise the production owner with a deterministic sequence of observations.
func (self fleetPreflightNonceReader) PendingNonceAt(ctx context.Context, address common.Address) (uint64, error) {
	return self(ctx, address)
}

// Every receipt lookup remains pinned to its expected signed transaction hash.
type fleetPreflightReceiptReader func(context.Context, common.Hash) (*ethTypes.Receipt, error)

// Satisfy only the exact read capability required by reconciliation.
func (self fleetPreflightReceiptReader) TransactionReceipt(ctx context.Context, hash common.Hash) (*ethTypes.Receipt, error) {
	return self(ctx, hash)
}

// The normal bounded policy is retained; the test observes waits without sleep.
func fleetPreflightReadPolicy(t *testing.T) finalSemanticRPCRetryPolicy {
	t.Helper()
	policy := defaultFinalSemanticRPCRetryPolicy()
	policy.maximumAttempts = 3
	policy.wait = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
	return policy
}

// Pool propagation may catch up to the exact reservation. It may never choose
// a new nonce after an independent transaction passes that reservation.
func TestFleetRenewalPreflightPendingNonceConvergesWithoutChangingApproval(t *testing.T) {
	action := Action{ID: "fleet.renew.1.1.bind.1", Parameters: map[string]string{"renewal_expected_nonce": "4"}}
	before, err := canonicalHashHex(action)
	if err != nil {
		t.Fatal(err)
	}
	for _, advanced := range []bool{false, true} {
		calls := 0
		reader := fleetPreflightNonceReader(func(context.Context, common.Address) (uint64, error) {
			calls++
			if advanced {
				return 5, nil
			}
			if calls == 1 {
				return 3, nil
			}
			return 4, nil
		})
		nonce, err := readFleetRenewalPendingNonce(t.Context(), reader, common.Address{1}, action, fleetPreflightReadPolicy(t))
		wantNonce, wantCalls := uint64(4), 2
		if advanced {
			wantNonce, wantCalls = 5, 1
		}
		if err != nil || nonce != wantNonce || calls != wantCalls {
			t.Fatalf("nonce=%d calls=%d err=%v", nonce, calls, err)
		}
	}
	if after, err := canonicalHashHex(action); err != nil || after != before {
		t.Fatal("preflight changed approved nonce authority", err)
	}
}

// Missing receipts and lower pool observations have finite budgets. Malformed
// receipt identities and canceled owners never spend a retry allowance.
func TestFleetRenewalPreflightReconciliationKeepsHardBoundaries(t *testing.T) {
	hash := common.Hash{1}
	for _, mode := range []string{"missing", "wrong-hash", "canceled"} {
		ctx, cancel := context.WithCancel(t.Context())
		if mode == "canceled" {
			cancel()
		}
		calls := 0
		reader := fleetPreflightReceiptReader(func(context.Context, common.Hash) (*ethTypes.Receipt, error) {
			calls++
			if mode == "wrong-hash" {
				return &ethTypes.Receipt{TxHash: common.Hash{2}, BlockNumber: big.NewInt(2), BlockHash: common.Hash{3}, Status: 1}, nil
			}
			return nil, ethereum.NotFound
		})
		receipt, err := readExactEvmReceipt(ctx, reader, hash, true, fleetPreflightReadPolicy(t))
		cancel()
		wantCalls := 3
		if mode == "wrong-hash" {
			wantCalls = 1
		}
		if mode == "canceled" {
			wantCalls = 0
		}
		if err == nil || receipt != nil || calls != wantCalls {
			t.Fatalf("%s calls=%d err=%v", mode, calls, err)
		}
		if mode == "missing" {
			var pending *evmReceiptPropagationError
			if !evmReadRpcRetriesExhausted(err) || !errors.As(err, &pending) || pending.hash != hash {
				t.Fatal("unknown receipt was mislabeled as a different winning transaction", err)
			}
		}
	}
	calls := 0
	action := Action{ID: "fleet.renew.1.1.bind.1", Parameters: map[string]string{"renewal_expected_nonce": "4"}}
	_, err := readFleetRenewalPendingNonce(t.Context(), fleetPreflightNonceReader(func(context.Context, common.Address) (uint64, error) { calls++; return 3, nil }), common.Address{1}, action, fleetPreflightReadPolicy(t))
	if !evmReadRpcRetriesExhausted(err) || calls != 3 {
		t.Fatal("pool propagation retry has no finite boundary", calls, err)
	}
}

// A transport timeout followed by a publication gap still resolves only the
// same receipt. Each read and wait consumes the single supplied retry owner.
func TestFleetRenewalPreflightExactReceiptRetriesTransportAndPropagation(t *testing.T) {
	hash := common.Hash{1}
	calls := 0
	reader := fleetPreflightReceiptReader(func(_ context.Context, actual common.Hash) (*ethTypes.Receipt, error) {
		if actual != hash {
			t.Fatal("retry changed transaction identity")
		}
		calls++
		if calls == 1 {
			return nil, context.DeadlineExceeded
		}
		if calls == 2 {
			return nil, ethereum.NotFound
		}
		return &ethTypes.Receipt{TxHash: hash, BlockNumber: big.NewInt(2), BlockHash: common.Hash{3}, Status: 1}, nil
	})
	receipt, err := readExactEvmReceipt(t.Context(), reader, hash, true, fleetPreflightReadPolicy(t))
	if err != nil || receipt == nil || receipt.TxHash != hash || calls != 3 {
		t.Fatal("bounded exact receipt failed to converge", calls, err)
	}
}

// The real pipelined sender must reconcile inclusion between receipt and nonce
// reads, and an uncertain accepted submission, without choosing fresh bytes.
func TestFleetRenewalPreflightExactSubmissionReconcilesConcurrentOutcome(t *testing.T) {
	for _, mode := range []string{"receipt-race", "wait-receipt-race", "uncertain-submission"} {
		key, err := crypto.ToECDSA(bytes.Repeat([]byte{0x65}, 32))
		if err != nil {
			t.Fatal(err)
		}
		chain := big.NewInt(1337)
		to, data := common.Address{0x17}, []byte{1, 2, 3, 4}
		action := Action{ID: "fleet.renew.1.1.bind.1", Kind: "evm-transaction", Target: to.Hex(), Parameters: map[string]string{"operation": "bind", "renewal_expected_nonce": "4", "renewal_expected_signer": crypto.PubkeyToAddress(key.PublicKey).Hex(), "renewal_calldata": "0x01020304", evmMaximumGasUnitsParameter: "100000", evmMaximumFeePerGasParameter: "10"}, Spend: Spend{EVMGasWei: "1000000"}}
		action.IntentHash, err = actionIntentHash(action)
		if err != nil {
			t.Fatal(err)
		}
		signed, err := ethTypes.SignTx(ethTypes.NewTx(&ethTypes.DynamicFeeTx{ChainID: chain, Nonce: 4, GasTipCap: big.NewInt(2), GasFeeCap: big.NewInt(10), Gas: 55000, To: &to, Value: new(big.Int), Data: data}), ethTypes.LatestSignerForChainID(chain), key)
		if err != nil {
			t.Fatal(err)
		}
		stateRoot := filepath.Join(t.TempDir(), "state")
		journal, err := OpenJournal(stateRoot)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = journal.Close() })
		raw, err := signed.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		if err := atomicWrite(filepath.Join(stateRoot, "transactions", stringsTrim0x(signed.Hash().Hex())+".rlp"), raw, 0o600); err != nil {
			t.Fatal(err)
		}
		for _, entry := range []JournalEntry{
			{DeploymentID: "synthetic-renewal", PlanHash: "synthetic-plan", ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageIntent},
			{DeploymentID: "synthetic-renewal", PlanHash: "synthetic-plan", ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageBroadcast, TransactionHash: signed.Hash().Hex(), Signer: crypto.PubkeyToAddress(key.PublicKey).Hex(), Nonce: "4", RecoveryBlock: 19, RecoveryBlockHash: common.Hash{0x19}.Hex()},
		} {
			if err := journal.Append(entry); err != nil {
				t.Fatal(err)
			}
		}
		receipt := &ethTypes.Receipt{Type: signed.Type(), Status: 1, TxHash: signed.Hash(), BlockNumber: big.NewInt(20), BlockHash: common.Hash{0x20}, GasUsed: 40000, CumulativeGasUsed: 40000, EffectiveGasPrice: big.NewInt(6), Logs: []*ethTypes.Log{}}
		reads, writes := 0, 0
		transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
			defer request.Body.Close()
			var call struct {
				Id     json.RawMessage   `json:"id"`
				Method string            `json:"method"`
				Params []json.RawMessage `json:"params"`
			}
			if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
				return nil, err
			}
			var result any
			switch call.Method {
			case "eth_getTransactionReceipt":
				reads++
				if reads > 1 {
					result = receipt
				}
			case "eth_getBlockByNumber":
				if string(call.Params[0]) == `"finalized"` {
					result = map[string]any{"number": "0x15", "hash": common.Hash{0x21}}
				} else {
					result = map[string]any{"number": "0x14", "hash": common.Hash{0x20}}
				}
			case "eth_getTransactionCount":
				result = "0x5"
				if mode == "uncertain-submission" {
					result = "0x4"
				}
			case "eth_sendRawTransaction":
				writes++
				var observed string
				if err := json.Unmarshal(call.Params[0], &observed); err != nil || observed != "0x"+common.Bytes2Hex(raw) {
					t.Error("submission changed retained signed bytes", err)
				}
				encoded, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": call.Id, "error": map[string]any{"code": -32002, "message": "synthetic upstream request timed out"}})
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(encoded)), Header: http.Header{}}, nil
			default:
				t.Error("recovery requested fresh signing or unexpected state", call.Method)
				return nil, errors.New("unexpected recovery Rpc")
			}
			encoded, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": call.Id, "result": result})
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(encoded)), Header: http.Header{}}, err
		})
		rpcClient, err := rpc.DialOptions(t.Context(), "https://rpc.example", rpc.WithHTTPClient(&http.Client{Transport: transport}))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(rpcClient.Close)
		manager := &EvmTxManager{client: ethclient.NewClient(rpcClient), chainID: chain, deploymentID: "synthetic-renewal", stateDir: stateRoot, journal: journal, key: key}
		prepared := signed
		if mode != "wait-receipt-race" {
			prepared, err = manager.submitFleetRenewal(t.Context(), "synthetic-plan", action, &to, new(big.Int), data)
		}
		if err != nil || prepared == nil || prepared.Hash() != signed.Hash() {
			t.Fatalf("%s did not reconcile the original action: %v", mode, err)
		}
		finalized, err := manager.waitExactTransaction(t.Context(), "synthetic-plan", action, prepared)
		wantWrites := 0
		if mode == "uncertain-submission" {
			wantWrites = 1
		}
		if err != nil || finalized == nil || finalized.TxHash != signed.Hash() || writes != wantWrites {
			t.Fatalf("%s writes=%d finality=%v err=%v", mode, writes, finalized, err)
		}
	}
}
