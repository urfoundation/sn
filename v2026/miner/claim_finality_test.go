package miner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/urnetwork/sdk/v2026"
)

// Generated signed custody is real; both clocks, hashes and all Rpc replies
// are synthetic. Native100 deliberately does not identify EVMfinalized70.
type claimClockTestRPC struct {
	stateLock      sync.Mutex
	cfg            *ClaimDaemonConfig
	claim          *sdk.SnPoolClaimResult
	entry          *ClaimQueueEntry
	receipt        *types.Receipt
	finalized      uint64
	canonical      map[uint64]common.Hash
	nonce          uint64
	nativeReads    int
	stateSelectors []string
	nonceSelectors []string
	sends          []string
	canonicalReads map[uint64]int
	receiptReads   int
	reply          func(context.Context, string, []json.RawMessage, any) (any, error)
}

func newClaimClockTestRPC(t *testing.T, configure func(*claimClockTestRPC)) *claimClockTestRPC {
	t.Helper()
	cfg, claim, entry, receipt, _ := signedClaimFixture(t, 70, 23)
	receipt.BlockNumber = big.NewInt(90)
	for _, log := range receipt.Logs {
		log.BlockNumber = 90
	}
	fixture := &claimClockTestRPC{cfg: cfg, claim: claim, entry: entry, receipt: receipt, finalized: 70, nonce: 23,
		canonical: map[uint64]common.Hash{60: {0x60}, 70: {0x70}, 90: receipt.BlockHash, 95: {0x95}, 100: {0x64}}, canonicalReads: map[uint64]int{}}
	if configure != nil {
		configure(fixture)
	}
	entitlement := hexutil.Encode(stSettlementVault.PackEntitlement(big.NewInt(claim.Epoch), new(big.Int).SetBytes(claim.NoId)))
	key, err := claimKey(claim.NoId, claim.Coldkey)
	if err != nil {
		t.Fatal(err)
	}
	leaf := hexutil.Encode(stSettlementVault.PackLeafClaimed(big.NewInt(claim.Epoch), key))
	_, replay, err := claimCalldata(claim)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer request.Body.Close()
		var call struct {
			Id     json.RawMessage   `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
			t.Error(err)
			return
		}
		var result any
		var callErr error
		func() {
			fixture.stateLock.Lock()
			defer fixture.stateLock.Unlock()
			selector := func(index int) string {
				var value string
				if index >= len(call.Params) || json.Unmarshal(call.Params[index], &value) != nil {
					t.Errorf("invalid %s selector: %s", call.Method, call.Params)
				}
				return value
			}
			switch call.Method {
			case "eth_chainId":
				result = "0x3b1"
			case "chain_getFinalizedHead":
				fixture.nativeReads++
				result = common.Hash{0x64}.Hex()
			case "chain_getHeader":
				fixture.nativeReads++
				result = map[string]any{"number": "0x64"}
			case "eth_getBlockByNumber":
				selected := selector(0)
				number := fixture.finalized
				if selected != "finalized" {
					number, callErr = hexutil.DecodeUint64(selected)
					fixture.canonicalReads[number]++
				}
				result = map[string]any{"number": hexutil.EncodeUint64(number), "hash": fixture.canonical[number].Hex()}
			case "eth_getTransactionReceipt":
				fixture.receiptReads++
				result = fixture.receipt
			case "eth_estimateGas":
				result = "0x186a0"
			case "eth_gasPrice":
				result = "0x1"
			case "eth_getTransactionCount":
				selected := selector(1)
				fixture.nonceSelectors = append(fixture.nonceSelectors, selected)
				nonce := fixture.nonce
				if selected == "0x64" {
					nonce = 24 // wrong-domain state falsely consumes the saved nonce
				}
				result = hexutil.EncodeUint64(nonce)
			case "eth_call":
				selected := selector(1)
				fixture.stateSelectors = append(fixture.stateSelectors, selected)
				var message struct {
					Input string `json:"input"`
					Data  string `json:"data"`
				}
				if err := json.Unmarshal(call.Params[0], &message); err != nil {
					t.Error(err)
				}
				data := message.Input
				if data == "" {
					data = message.Data
				}
				switch data {
				case entitlement:
					result = hexutil.Encode(fixture.claim.PayoutRoot) + strings.Repeat("00", 32*6)
				case leaf:
					value := "01"
					if selected == "0x64" {
						value = "00" // unfinalized state cannot decide finalized readiness
					}
					result = "0x" + strings.Repeat("00", 31) + value
				case hexutil.Encode(replay):
					result = "0x"
				default:
					t.Errorf("unexpected synthetic claim call %s", data)
				}
			case "eth_sendRawTransaction":
				raw := selector(0)
				fixture.sends = append(fixture.sends, raw)
				encoded, err := hexutil.Decode(raw)
				var tx types.Transaction
				if err != nil || tx.UnmarshalBinary(encoded) != nil || raw != fixture.entry.RawTxHex {
					t.Error("send did not use exact durably retained claim bytes")
				}
				fixture.receipt.TxHash = tx.Hash()
				for _, log := range fixture.receipt.Logs {
					log.TxHash = tx.Hash()
				}
				result = tx.Hash().Hex()
			default:
				t.Errorf("unexpected claim clock method %s", call.Method)
			}
			if fixture.reply != nil && callErr == nil {
				result, callErr = fixture.reply(request.Context(), call.Method, call.Params, result)
			}
		}()
		response := map[string]any{"jsonrpc": "2.0", "id": call.Id, "result": result}
		if callErr != nil {
			delete(response, "result")
			response["error"] = map[string]any{"code": -32000, "message": callErr.Error()}
		}
		_ = json.NewEncoder(writer).Encode(response)
	}))
	t.Cleanup(server.Close)
	fixture.cfg.RPC = []string{server.URL}
	return fixture
}

func (self *claimClockTestRPC) evidence() (int, []string, []string, []string) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return self.nativeReads, slices.Clone(self.stateSelectors), slices.Clone(self.nonceSelectors), slices.Clone(self.sends)
}

func TestClaimClockReceiptWaitsForEVMFinality(t *testing.T) {
	for _, finalized := range []uint64{70, 95} {
		fixture := newClaimClockTestRPC(t, func(fixture *claimClockTestRPC) { fixture.finalized = finalized })
		receipt, err := finalizedClaimReceipt(t.Context(), fixture.cfg, fixture.entry.TxHash, big.NewInt(945))
		if finalized == 70 {
			if err == nil || receipt != nil || !strings.Contains(err.Error(), "not finalized") {
				t.Fatalf("native100 finalized EVMreceipt90 beyond EVM70: %+v %v", receipt, err)
			}
		} else if err != nil || receipt == nil || receipt.BlockNumber.Uint64() != 90 || receipt.BlockHash != fixture.receipt.BlockHash {
			t.Fatalf("EVM95 failed to admit canonical receipt90: %+v %v", receipt, err)
		}
		if native, _, _, _ := fixture.evidence(); native != 0 {
			t.Fatalf("claim finality consulted native coordinates: %d", native)
		}
	}
}

func TestClaimClockStateSelectsFinalizedEVMSnapshot(t *testing.T) {
	fixture := newClaimClockTestRPC(t, nil)
	claimed, err := queryClaimedFinalized(t.Context(), fixture.cfg, fixture.claim)
	if err != nil || !claimed {
		t.Fatalf("finalized EVM70 state was replaced by native100: %t %v", claimed, err)
	}
	native, state, _, sends := fixture.evidence()
	if native != 0 || !slices.Equal(state, []string{"0x46", "0x46"}) || len(sends) != 0 {
		t.Fatalf("claim state used another clock: native=%d selectors=%v sends=%v", native, state, sends)
	}
}

func TestClaimClockReplayUsesEVMNonceAndExactBytes(t *testing.T) {
	fixture := newClaimClockTestRPC(t, nil)
	tx, _, from, err := authenticateSignedClaim(fixture.cfg, fixture.entry)
	if err != nil {
		t.Fatal(err)
	}
	before := *fixture.entry
	consumed, err := rebroadcastSignedClaimTest(t, t.Context(), fixture.cfg, tx, from)
	if err != nil || consumed {
		t.Fatalf("native100 consumed nonce available at EVM70: %t %v", consumed, err)
	}
	native, state, nonce, sends := fixture.evidence()
	if native != 0 || !slices.Equal(state, []string{"0x46"}) || !slices.Equal(nonce, []string{"0x46"}) || !slices.Equal(sends, []string{before.RawTxHex}) || *fixture.entry != before {
		t.Fatalf("replay changed clock or signed custody: native=%d state=%v nonce=%v sends=%v", native, state, nonce, sends)
	}
}

func TestClaimClockRestartRetainsUnfinalizedSignedReceipt(t *testing.T) {
	fixture := newClaimClockTestRPC(t, func(fixture *claimClockTestRPC) { fixture.nonce = 24 })
	store := newClaimQueueTestStore(t, fixture.cfg.StateDir)
	queue := &ClaimQueue{Schema: "urnetwork-provider-claim-queue-v1", LastDiscovered: 70, Entries: map[string]*ClaimQueueEntry{"70": fixture.entry}}
	if err := store.save(queue); err != nil {
		t.Fatal(err)
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
	store = newClaimQueueTestStore(t, fixture.cfg.StateDir)
	queue, err := store.load()
	if err != nil {
		t.Fatal(err)
	}
	entry := queue.Entries["70"]
	before := *entry
	api := claimApiFunction(func(context.Context, *sdk.SnPoolClaimArgs) (*sdk.SnPoolClaimResult, error) {
		t.Fatal("signed receipt recovery consulted a replacement API intent")
		return nil, nil
	})
	status, err := reconcileClaimEntry(t.Context(), fixture.cfg, api, entry, store)
	var unresolved *claimSignedOutcomeError
	if status != "" || !errors.As(err, &unresolved) || *entry != before {
		t.Fatalf("restart discarded unfinalized EVM liability: status=%s error=%v entry=%+v", status, err, entry)
	}
	func() {
		fixture.stateLock.Lock()
		defer fixture.stateLock.Unlock()
		fixture.finalized = 95
	}()
	status, err = reconcileClaimEntry(t.Context(), fixture.cfg, api, entry, store)
	if err != nil || status != "finalized" || entry.FinalizedBlock != 90 || entry.RawTxHex != before.RawTxHex || entry.TxHash != before.TxHash || entry.Attempts != before.Attempts {
		t.Fatalf("original EVM95 recovery failed: status=%s error=%v entry=%+v", status, err, entry)
	}
	if err := store.save(queue); err != nil {
		t.Fatal(err)
	}
	retained, err := store.load()
	if err != nil || *retained.Entries["70"] != *entry {
		t.Fatalf("recovered receipt lost durable identity: %v", err)
	}
	if native, _, _, sends := fixture.evidence(); native != 0 || len(sends) != 0 {
		t.Fatalf("receipt recovery touched native clock or broadcast: native=%d sends=%v", native, sends)
	}
}

func TestClaimClockStateClosesOriginalFinalizedWitness(t *testing.T) {
	fixture := newClaimClockTestRPC(t, func(fixture *claimClockTestRPC) {
		fixture.reply = func(_ context.Context, method string, _ []json.RawMessage, result any) (any, error) {
			if method == "eth_call" && len(fixture.stateSelectors) == 2 {
				fixture.canonical[70] = common.Hash{0xee}
			}
			return result, nil
		}
	})
	claimed, err := queryClaimedFinalized(t.Context(), fixture.cfg, fixture.claim)
	if claimed || err == nil || !strings.Contains(err.Error(), "not canonical") {
		t.Fatalf("claim state escaped changed finalized witness: %t %v", claimed, err)
	}
}

func TestClaimClockReplayClosesBeforeNonceDispositionOrSend(t *testing.T) {
	for _, consumed := range []bool{false, true} {
		fixture := newClaimClockTestRPC(t, func(fixture *claimClockTestRPC) {
			if consumed {
				fixture.nonce = 24
			}
			fixture.reply = func(_ context.Context, method string, _ []json.RawMessage, result any) (any, error) {
				if method == "eth_call" || consumed && method == "eth_getTransactionCount" {
					fixture.canonical[70] = common.Hash{0xee}
				}
				return result, nil
			}
		})
		tx, _, from, err := authenticateSignedClaim(fixture.cfg, fixture.entry)
		if err != nil {
			t.Fatal(err)
		}
		before := *fixture.entry
		got, err := rebroadcastSignedClaimTest(t, t.Context(), fixture.cfg, tx, from)
		_, _, _, sends := fixture.evidence()
		if got || err == nil || !strings.Contains(err.Error(), "not canonical") || len(sends) != 0 || *fixture.entry != before {
			t.Fatalf("changed witness disposed or replayed nonce consumed=%t: got=%t error=%v sends=%v", consumed, got, err, sends)
		}
	}
}

func TestClaimClockReceiptClosesInclusionAndFinalizedWitness(t *testing.T) {
	for _, replaced := range []uint64{90, 95} {
		fixture := newClaimClockTestRPC(t, func(fixture *claimClockTestRPC) {
			fixture.finalized = 95
			fixture.reply = func(_ context.Context, method string, params []json.RawMessage, result any) (any, error) {
				if method == "eth_getBlockByNumber" && string(params[0]) == `"0x5a"` && fixture.canonicalReads[90] == 1 {
					fixture.canonical[replaced] = common.Hash{0xee}
				}
				return result, nil
			}
		})
		receipt, err := finalizedClaimReceipt(t.Context(), fixture.cfg, fixture.entry.TxHash, big.NewInt(945))
		if receipt != nil || err == nil || !strings.Contains(err.Error(), "not canonical") {
			t.Fatalf("claim receipt retained replaced EVM%d witness: %+v %v", replaced, receipt, err)
		}
	}
}

func TestClaimClockFinalityUnavailableNeverFallsBackToNative(t *testing.T) {
	for _, reply := range []any{nil, map[string]any{}, map[string]any{"number": "0x46", "hash": "0x01"}, map[string]any{"number": "0x46", "hash": common.Hash{}.Hex()}, map[string]any{"number": "0x10000000000000000", "hash": common.Hash{1}.Hex()}, map[string]any{"number": "0x0046", "hash": common.Hash{1}.Hex()}} {
		fixture := newClaimClockTestRPC(t, func(fixture *claimClockTestRPC) {
			fixture.reply = func(_ context.Context, method string, params []json.RawMessage, result any) (any, error) {
				if method == "eth_getBlockByNumber" && string(params[0]) == `"finalized"` {
					return reply, nil
				}
				return result, nil
			}
		})
		claimed, err := queryClaimedFinalized(t.Context(), fixture.cfg, fixture.claim)
		native, state, _, sends := fixture.evidence()
		if claimed || err == nil || native != 0 || len(state) != 0 || len(sends) != 0 {
			t.Fatalf("malformed EVMfinality substituted native authority: reply=%v claimed=%t error=%v native=%d state=%v", reply, claimed, err, native, state)
		}
	}
}

func TestClaimClockRootMismatchRequiresStableWitness(t *testing.T) {
	for _, outage := range []bool{false, true} {
		fixture := newClaimClockTestRPC(t, func(fixture *claimClockTestRPC) {
			fixture.reply = func(_ context.Context, method string, _ []json.RawMessage, result any) (any, error) {
				if method == "eth_call" {
					return "0x" + strings.Repeat("00", 32*7), nil
				}
				if outage && method == "eth_getBlockByNumber" && fixture.canonicalReads[70] == 2 {
					return nil, errors.New("synthetic historical RPC outage")
				}
				return result, nil
			}
		})
		claimed, err := queryClaimedFinalized(t.Context(), fixture.cfg, fixture.claim)
		var mismatch *claimArtifactRootMismatchError
		if claimed || err == nil || errors.As(err, &mismatch) == outage {
			t.Fatalf("transient outage/root mismatch classification changed: outage=%t claimed=%t error=%v", outage, claimed, err)
		}
		if outage && (!strings.Contains(err.Error(), "synthetic historical RPC outage") || strings.Contains(err.Error(), "not canonical")) {
			t.Fatalf("transient read became canonical mismatch: %v", err)
		}
	}
}

// A failed or exited worker cannot leave a test waiting for an unreachable RPC.
func waitClaimClockBarrier(ctx context.Context, blocked <-chan struct{}, done <-chan error) error {
	select {
	case <-blocked:
		return nil
	case err, present := <-done:
		return errors.Join(fmt.Errorf("claim worker stopped before cancellation barrier (result=%t)", present), err)
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestClaimClockBarrierObservesWorkerExit(t *testing.T) {
	done := make(chan error)
	close(done)
	if err := waitClaimClockBarrier(t.Context(), make(chan struct{}), done); err == nil || !strings.Contains(err.Error(), "worker stopped") {
		t.Fatal("exited worker was treated as a pending RPC barrier", err)
	}
}

func TestClaimClockCancellationClosesWithoutEvidenceOrSend(t *testing.T) {
	for _, replay := range []bool{false, true} {
		blocked := make(chan struct{})
		fixture := newClaimClockTestRPC(t, func(fixture *claimClockTestRPC) {
			fixture.reply = func(ctx context.Context, method string, params []json.RawMessage, result any) (any, error) {
				if method == "eth_getBlockByNumber" && string(params[0]) == `"0x46"` && fixture.canonicalReads[70] == 2 {
					close(blocked)
					<-ctx.Done()
					return nil, ctx.Err()
				}
				return result, nil
			}
		})
		// Enrollment must report fatal setup failures on the test goroutine.
		var tx *types.Transaction
		var from common.Address
		var store *claimQueueStore
		if replay {
			var err error
			tx, _, from, err = authenticateSignedClaim(fixture.cfg, fixture.entry)
			if err != nil {
				t.Fatal(err)
			}
			store = claimRetainedTestStore(t, fixture.cfg, fixture.entry)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		done := make(chan error, 1)
		t.Cleanup(func() {
			cancel()
			select {
			case <-done:
			case <-time.After(30 * time.Second):
				t.Error("claim worker survived cancellation cleanup")
			}
		})
		before := *fixture.entry
		go func() {
			defer close(done)
			if replay {
				_, err := rebroadcastSignedClaim(ctx, fixture.cfg, tx, from, store)
				done <- err
			} else {
				claimed, err := queryClaimedFinalized(ctx, fixture.cfg, fixture.claim)
				if claimed {
					t.Error("canceled closing read published claimed state")
				}
				done <- err
			}
		}()
		if err := waitClaimClockBarrier(ctx, blocked, done); err != nil {
			t.Fatal(err)
		}
		cancel()
		var err error
		select {
		case err = <-done:
		case <-time.After(30 * time.Second):
			t.Fatal("canceled claim worker did not join")
		}
		_, _, _, sends := fixture.evidence()
		if !errors.Is(err, context.Canceled) || len(sends) != 0 || *fixture.entry != before {
			t.Fatalf("canceled closing read changed authority replay=%t: %v sends=%v", replay, err, sends)
		}
	}
}
