package validator

// Chain-client tests that need no live chain: endpoint failover against
// minimal JSON-RPC stubs, and the hand-encoded metagraph calldata against
// cast-derived selectors.

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// jsonRpcStub answers eth_chainId (and enough of the surface for dialing).
func jsonRpcStub(t *testing.T, chainIdHex string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Id     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		var result string
		switch request.Method {
		case "eth_chainId":
			result = chainIdHex
		default:
			result = "0x0"
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      request.Id,
			"result":  result,
		})
	}))
}

func TestDialChainFailover(t *testing.T) {
	// First endpoint refuses everything; the second answers. DialChain
	// must fail over in order (PLAN.md §11.1).
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", 500)
	}))
	defer bad.Close()
	good := jsonRpcStub(t, "0x3b1") // 945, the testnet chain id
	defer good.Close()

	chain, err := DialChain([]string{bad.URL, good.URL}, common.Address{})
	if err != nil {
		t.Fatal(err)
	}
	defer chain.Close()
	if chain.RpcUrl() != good.URL {
		t.Fatalf("dialed %s, want the second (good) endpoint", chain.RpcUrl())
	}
	if chain.ChainId().Uint64() != 945 {
		t.Fatalf("chain id %s", chain.ChainId())
	}
}

func TestDialChainAllDown(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", 500)
	}))
	defer bad.Close()
	if _, err := DialChain([]string{bad.URL}, common.Address{}); err == nil {
		t.Fatal("expected failure when every endpoint is down")
	}
	if _, err := DialChain(nil, common.Address{}); err == nil {
		t.Fatal("expected failure with no endpoints")
	}
}

// A provider may return a null block result while it is catching up. That
// response and invalid local dependencies must be errors, not process-killing
// nil dereferences.
func TestFinalizedBlockRejectsEmptyHeaderAndInvalidInputs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var request struct {
			Id     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var result any
		if request.Method == "eth_chainId" {
			result = "0x3b1"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      request.Id,
			"result":  result,
		})
	}))
	defer server.Close()

	chain, err := DialChain([]string{server.URL}, common.Address{})
	if err != nil {
		t.Fatal(err)
	}
	defer chain.Close()
	if _, _, err := chain.FinalizedBlockContext(context.Background()); err == nil || !strings.Contains(err.Error(), "finalized EVM head") {
		t.Fatalf("empty finalized header error = %v", err)
	}
	if _, _, err := chain.FinalizedBlockContext(nil); err == nil || !strings.Contains(err.Error(), "context is nil") {
		t.Fatalf("nil finalized context error = %v", err)
	}
	if _, _, err := (&ChainClient{}).FinalizedBlockContext(context.Background()); err == nil || !strings.Contains(err.Error(), "client is unavailable") {
		t.Fatalf("nil finalized client error = %v", err)
	}
	if _, err := chainViewAtContext[uint64](nil, chain, 1, nil, nil); err == nil || !strings.Contains(err.Error(), "context is nil") {
		t.Fatalf("nil chain view context error = %v", err)
	}
}

// Selector goldens computed with `cast sig` (Foundry) against the vendored
// IMetagraph interface (evm/src/interfaces/metagraph.sol):
//
//	cast sig 'getUidCount(uint16)'        = 0x1f193572
//	cast sig 'getHotkey(uint16,uint16)'   = 0x3adc89da
func TestMetagraphSelectors(t *testing.T) {
	if got := evmSelector("getUidCount(uint16)"); hex.EncodeToString(got[:]) != "1f193572" {
		t.Fatalf("getUidCount selector %x", got)
	}
	if got := evmSelector("getHotkey(uint16,uint16)"); hex.EncodeToString(got[:]) != "3adc89da" {
		t.Fatalf("getHotkey selector %x", got)
	}
	// Arg encoding: uint16 left-padded to one word.
	word := evmUint16Word(0x1234)
	want := "0000000000000000000000000000000000000000000000000000000000001234"
	if hex.EncodeToString(word[:]) != want {
		t.Fatalf("uint16 word %x", word)
	}
}

// TestDepositedTopic0 pins topic0 of the Deposited log (D25 — the deposit record
// moved from the DT ledger to the event log) against an independent keccak of the
// canonical event signature.
func TestDepositedTopic0(t *testing.T) {
	want := crypto.Keccak256([]byte("Deposited(uint256,uint256,address,uint256)"))
	if hex.EncodeToString(depositedTopic0[:]) != hex.EncodeToString(want) {
		t.Fatalf("depositedTopic0 = %x, want %x", depositedTopic0, want)
	}
}

// depositedLogJSON builds one eth_getLogs Deposited log object: topic0 =
// depositedTopic0, indexed e / noId as topics, and (from, amount) abi-encoded in
// data.
func depositedLogJSON(contract common.Address, e, noId, amount, logIndex int64) map[string]any {
	from := common.HexToAddress("0x00000000000000000000000000000000000000aa")
	data := make([]byte, 64)
	copy(data[12:32], from.Bytes()) // address right-aligned in the first word
	new(big.Int).SetInt64(amount).FillBytes(data[32:64])
	return map[string]any{
		"address": contract.Hex(),
		"topics": []string{
			"0x" + hex.EncodeToString(depositedTopic0[:]),
			common.BigToHash(big.NewInt(e)).Hex(),
			common.BigToHash(big.NewInt(noId)).Hex(),
		},
		"data":             "0x" + hex.EncodeToString(data),
		"blockNumber":      "0x1",
		"transactionHash":  "0x" + strings.Repeat("11", 32),
		"transactionIndex": "0x0",
		"blockHash":        "0x" + strings.Repeat("22", 32),
		"logIndex":         fmt.Sprintf("0x%x", logIndex),
		"removed":          false,
	}
}

// TestDepositedSums exercises the event-log scanner end to end against a stub:
// it sends the Deposited topic0 + the epoch topic filter, decodes each log via
// the stabi ABI, and sums `amount` per noId.
func TestDepositedSums(t *testing.T) {
	contract := common.HexToAddress("0x00000000000000000000000000000000000000cc")

	var mu sync.Mutex
	var sawTopics [][]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Id     json.RawMessage   `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		var result any = "0x0"
		switch request.Method {
		case "eth_chainId":
			result = "0x3b1"
		case "eth_getLogs":
			var filter struct {
				Topics [][]string `json:"topics"`
			}
			if len(request.Params) > 0 {
				json.Unmarshal(request.Params[0], &filter)
			}
			mu.Lock()
			for _, group := range filter.Topics {
				sawTopics = append(sawTopics, group)
			}
			mu.Unlock()
			result = []map[string]any{
				depositedLogJSON(contract, 5, 1, 100, 0),
				depositedLogJSON(contract, 5, 1, 50, 1), // same noId → summed
				depositedLogJSON(contract, 5, 2, 30, 2), // different noId
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.Id, "result": result})
	}))
	defer server.Close()

	chain, err := DialChain([]string{server.URL}, contract)
	if err != nil {
		t.Fatal(err)
	}
	defer chain.Close()

	sums, err := chain.DepositedSums(0, 5, big.NewInt(5))
	if err != nil {
		t.Fatal(err)
	}
	if got := sums.Get(big.NewInt(1)).Int64(); got != 150 {
		t.Fatalf("noId 1 sum = %d, want 150 (100+50)", got)
	}
	if got := sums.Get(big.NewInt(2)).Int64(); got != 30 {
		t.Fatalf("noId 2 sum = %d, want 30", got)
	}
	if got := sums.Get(big.NewInt(9)); got.Sign() != 0 {
		t.Fatalf("absent noId sum = %s, want 0", got)
	}

	// The scan filtered on topic0 = Deposited and topic1 = the epoch (5).
	mu.Lock()
	defer mu.Unlock()
	if len(sawTopics) < 2 {
		t.Fatalf("filter topics %v, want [topic0, epoch]", sawTopics)
	}
	if len(sawTopics[0]) != 1 || sawTopics[0][0] != "0x"+hex.EncodeToString(depositedTopic0[:]) {
		t.Fatalf("topic0 filter %v, want the Deposited event id", sawTopics[0])
	}
	if len(sawTopics[1]) != 1 || sawTopics[1][0] != common.BigToHash(big.NewInt(5)).Hex() {
		t.Fatalf("epoch topic filter %v, want hash(5)", sawTopics[1])
	}
}

// The finalized event log is the validator's authoritative demand input. A
// provider response that does not exactly match the requested canonical log
// domain must stop scoring instead of being skipped or counted twice.
func TestDepositedSumsRejectsMalformedRemovedForeignAndDuplicateLogs(t *testing.T) {
	contract := common.HexToAddress("0x00000000000000000000000000000000000000cc")
	foreign := common.HexToAddress("0x00000000000000000000000000000000000000dd")
	base := func() map[string]any { return depositedLogJSON(contract, 5, 1, 100, 0) }
	cases := []struct {
		name string
		logs func() []map[string]any
	}{
		{name: "foreign address", logs: func() []map[string]any { return []map[string]any{depositedLogJSON(foreign, 5, 1, 100, 0)} }},
		{name: "removed", logs: func() []map[string]any { log := base(); log["removed"] = true; return []map[string]any{log} }},
		{name: "missing indexed topic", logs: func() []map[string]any {
			log := base()
			log["topics"] = log["topics"].([]string)[:2]
			return []map[string]any{log}
		}},
		{name: "wrong epoch", logs: func() []map[string]any { return []map[string]any{depositedLogJSON(contract, 6, 1, 100, 0)} }},
		{name: "malformed data", logs: func() []map[string]any { log := base(); log["data"] = "0x00"; return []map[string]any{log} }},
		{name: "outside block range", logs: func() []map[string]any { log := base(); log["blockNumber"] = "0x6"; return []map[string]any{log} }},
		{name: "missing block identity", logs: func() []map[string]any {
			log := base()
			log["blockHash"] = common.Hash{}.Hex()
			return []map[string]any{log}
		}},
		{name: "missing transaction identity", logs: func() []map[string]any {
			log := base()
			log["transactionHash"] = common.Hash{}.Hex()
			return []map[string]any{log}
		}},
		{name: "zero operator", logs: func() []map[string]any { return []map[string]any{depositedLogJSON(contract, 5, 0, 100, 0)} }},
		{name: "zero amount", logs: func() []map[string]any { return []map[string]any{depositedLogJSON(contract, 5, 1, 0, 0)} }},
		{name: "duplicate", logs: func() []map[string]any { log := base(); return []map[string]any{log, log} }},
	}
	for _, testCase := range cases {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var request struct {
				Id     json.RawMessage `json:"id"`
				Method string          `json:"method"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			var result any = "0x0"
			if request.Method == "eth_chainId" {
				result = "0x3b1"
			} else if request.Method == "eth_getLogs" {
				result = testCase.logs()
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.Id, "result": result})
		}))
		chain, err := DialChain([]string{server.URL}, contract)
		if err != nil {
			server.Close()
			t.Errorf("%s dial: %v", testCase.name, err)
			continue
		}
		_, scanErr := chain.DepositedSums(0, 5, big.NewInt(5))
		chain.Close()
		server.Close()
		if scanErr == nil {
			t.Errorf("%s log was accepted", testCase.name)
		}
	}
}

// Chunk arithmetic remains monotonic through the largest uint64 block. The
// old addition-first calculation wrapped the first upper bound below `from`.
func TestDepositedSumsDoesNotWrapFinalChunkAtUint64Boundary(t *testing.T) {
	contract := common.HexToAddress("0x00000000000000000000000000000000000000cc")
	fromBlock, toBlock := ^uint64(0)-5, ^uint64(0)
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Id     json.RawMessage   `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		response := map[string]any{"jsonrpc": "2.0", "id": request.Id, "result": "0x0"}
		if request.Method == "eth_chainId" {
			response["result"] = "0x3b1"
		} else if request.Method == "eth_getLogs" {
			requests++
			var filter struct {
				FromBlock string `json:"fromBlock"`
				ToBlock   string `json:"toBlock"`
			}
			if len(request.Params) != 1 || json.Unmarshal(request.Params[0], &filter) != nil || filter.FromBlock != fmt.Sprintf("0x%x", fromBlock) || filter.ToBlock != fmt.Sprintf("0x%x", toBlock) || requests != 1 {
				delete(response, "result")
				response["error"] = map[string]any{"code": -32602, "message": "wrapped or repeated range"}
			} else {
				response["result"] = []any{}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()
	chain, err := DialChain([]string{server.URL}, contract)
	if err != nil {
		t.Fatal(err)
	}
	defer chain.Close()
	if _, err := chain.DepositedSums(fromBlock, toBlock, nil); err != nil {
		t.Fatal(err)
	}
	if requests != 1 {
		t.Fatalf("eth_getLogs requests=%d, want 1", requests)
	}
}

func TestDepositedSumsRejectsUnavailableScannerAndInvalidEpoch(t *testing.T) {
	if _, err := (&ChainClient{}).DepositedSums(0, 0, nil); err == nil {
		t.Fatal("unavailable deposited-event scanner was accepted")
	}
	server := jsonRpcStub(t, "0x3b1")
	defer server.Close()
	chain, err := DialChain([]string{server.URL}, common.HexToAddress("0x00000000000000000000000000000000000000cc"))
	if err != nil {
		t.Fatal(err)
	}
	defer chain.Close()
	for _, epoch := range []*big.Int{big.NewInt(-1), new(big.Int).Lsh(big.NewInt(1), 256)} {
		if _, err := chain.DepositedSums(0, 0, epoch); err == nil {
			t.Errorf("invalid epoch %s was accepted", epoch)
		}
	}
}
