package validator

// Chain-client tests that need no live chain: endpoint failover against
// minimal JSON-RPC stubs, and the hand-encoded metagraph calldata against
// cast-derived selectors.

import (
	"encoding/hex"
	"encoding/json"
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
func depositedLogJSON(contract common.Address, e, noId, amount int64) map[string]any {
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
		"logIndex":         "0x0",
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
				depositedLogJSON(contract, 5, 1, 100),
				depositedLogJSON(contract, 5, 1, 50), // same noId → summed
				depositedLogJSON(contract, 5, 2, 30), // different noId
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
