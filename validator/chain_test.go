package validator

// Chain-client tests that need no live chain: endpoint failover against
// minimal JSON-RPC stubs, and the hand-encoded metagraph calldata against
// cast-derived selectors.

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	gethrpc "github.com/ethereum/go-ethereum/rpc"

	"github.com/urfoundation/sn/stabi"
)

// Encodes a unique nonzero block identity for cache-only tests.
func chainTestBlockIdentityHash(block uint64) [32]byte {
	var blockHash [32]byte
	binary.BigEndian.PutUint64(blockHash[24:], block)
	return blockHash
}

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

// Lets an ordered-dial test trigger one synthetic provider deadline without
// waiting for the production wall-clock budget.
type chainTestDeadlineContext struct {
	context.Context
	done <-chan struct{}
}

func (self *chainTestDeadlineContext) Done() <-chan struct{} {
	return self.done
}

func (self *chainTestDeadlineContext) Err() error {
	select {
	case <-self.done:
		return context.DeadlineExceeded
	default:
		return nil
	}
}

// A slow first public endpoint consumes only its own dial/probe context; the
// next healthy endpoint remains available under the same release lifecycle.
func TestDialChainContextFailsOverAfterSlowEndpointDeadline(t *testing.T) {
	firstStarted := make(chan struct{})
	firstExpired := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer request.Body.Close()
		var rpcRequest struct {
			Method string `json:"method"`
		}
		if err := json.NewDecoder(request.Body).Decode(&rpcRequest); err != nil || rpcRequest.Method != "eth_chainId" {
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		close(firstStarted)
		<-request.Context().Done()
	}))
	defer slow.Close()
	healthy := jsonRpcStub(t, "0x3b1")
	defer healthy.Close()

	endpointContexts := 0
	var endpointTimeouts []time.Duration
	endpointContext := func(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
		endpointContexts++
		endpointTimeouts = append(endpointTimeouts, timeout)
		if endpointContexts == 1 {
			return &chainTestDeadlineContext{Context: parent, done: firstExpired}, func() {}
		}
		return context.WithCancel(parent)
	}
	done := make(chan struct {
		chain *ChainClient
		err   error
	}, 1)
	go func() {
		chain, err := dialChainWithEndpointContext(context.Background(), []string{slow.URL, healthy.URL}, common.Address{}, false, endpointContext)
		done <- struct {
			chain *ChainClient
			err   error
		}{chain: chain, err: err}
	}()
	<-firstStarted
	close(firstExpired)
	result := <-done
	endpoint := ""
	if result.chain != nil {
		endpoint = result.chain.RpcUrl()
	}
	if result.err != nil || result.chain == nil || endpoint != healthy.URL {
		t.Fatalf("failover chain=%p endpoint=%q err=%v", result.chain, endpoint, result.err)
	}
	defer result.chain.Close()
	if endpointContexts != 2 || len(endpointTimeouts) != 2 || endpointTimeouts[0] != chainDialTimeout || endpointTimeouts[1] != chainDialTimeout {
		t.Fatalf("endpoint contexts=%d timeouts=%v", endpointContexts, endpointTimeouts)
	}
}

// The production constructor must stop before a fallback endpoint when its
// parent lifecycle is canceled during the first provider's chain-id probe.
func TestDialChainContextStopsOnParentCancellation(t *testing.T) {
	started := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer request.Body.Close()
		var rpcRequest struct {
			Method string `json:"method"`
		}
		if err := json.NewDecoder(request.Body).Decode(&rpcRequest); err != nil || rpcRequest.Method != "eth_chainId" {
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		close(started)
		<-request.Context().Done()
	}))
	defer slow.Close()
	healthyRequests := make(chan struct{}, 1)
	healthy := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		healthyRequests <- struct{}{}
		writer.WriteHeader(http.StatusInternalServerError)
	}))
	defer healthy.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		chain, err := DialChainContext(ctx, []string{slow.URL, healthy.URL}, common.Address{})
		if chain != nil {
			chain.Close()
		}
		done <- err
	}()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("parent-canceled EVM dial error=%v", err)
	}
	select {
	case <-healthyRequests:
		t.Fatal("parent cancellation reached a fallback endpoint")
	default:
	}
}

// Exact EVM block views must pass the selected block tag to the provider;
// otherwise a later head could change a settlement boundary mid-decision.
func TestReleaseEpochStartBlockAtContextUsesExactBlock(t *testing.T) {
	var blockTag string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer request.Body.Close()
		var rpcRequest struct {
			Id     json.RawMessage   `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(request.Body).Decode(&rpcRequest); err != nil {
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		result := any("0x3b1")
		switch rpcRequest.Method {
		case "eth_chainId":
		case "eth_call":
			if len(rpcRequest.Params) != 2 || json.Unmarshal(rpcRequest.Params[1], &blockTag) != nil {
				writer.WriteHeader(http.StatusBadRequest)
				return
			}
			result = "0x0000000000000000000000000000000000000000000000000000000000000008"
		default:
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": rpcRequest.Id, "result": result})
	}))
	defer server.Close()
	chain, err := DialReleaseChainContext(context.Background(), []string{server.URL}, common.Address{1})
	if err != nil {
		t.Fatal(err)
	}
	defer chain.Close()
	start, err := chain.ReleaseEpochStartBlockAtContext(context.Background(), 7, big.NewInt(3))
	if err != nil || start != 8 || blockTag != "0x7" {
		t.Fatalf("start=%d block_tag=%q err=%v", start, blockTag, err)
	}
}

// A finalized hash that ceases to be canonical before the first coordinator
// view must abort snapshot construction. Falling back to its old height would
// splice state from the replacement branch into the captured identity.
func TestReleaseSnapshotRejectsReorgAfterFinalizedHeader(t *testing.T) {
	header := &types.Header{
		UncleHash:   types.EmptyUncleHash,
		TxHash:      types.EmptyTxsHash,
		ReceiptHash: types.EmptyReceiptsHash,
		Difficulty:  big.NewInt(0),
		Number:      big.NewInt(7),
		GasLimit:    1,
		Time:        1,
	}
	headerHash := header.Hash()
	var selectorObserved atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer request.Body.Close()
		var rpcRequest struct {
			ID     json.RawMessage   `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(request.Body).Decode(&rpcRequest); err != nil {
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		response := map[string]any{"jsonrpc": "2.0", "id": rpcRequest.ID}
		switch rpcRequest.Method {
		case "eth_getBlockByNumber":
			response["result"] = header
		case "eth_call":
			var selector gethrpc.BlockNumberOrHash
			if len(rpcRequest.Params) != 2 || json.Unmarshal(rpcRequest.Params[1], &selector) != nil || selector.BlockHash == nil || *selector.BlockHash != headerHash || !selector.RequireCanonical {
				response["result"] = "0x0000000000000000000000000000000000000000000000000000000000000001"
				break
			}
			selectorObserved.Store(true)
			response["error"] = map[string]any{"code": -32000, "message": "synthetic finalized reorg"}
		default:
			response["error"] = map[string]any{"code": -32601, "message": "unsupported fixture request"}
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(response)
	}))
	client, err := ethclient.Dial(server.URL)
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	chain := &ChainClient{
		client:       client,
		coordinator:  stabi.NewSTCoordinator(),
		contractAddr: common.Address{1},
		release:      true,
	}
	_, err = chain.ReleaseSnapshotContext(context.Background())
	client.Close()
	server.Close()
	if err == nil || !strings.Contains(err.Error(), "synthetic finalized reorg") || !selectorObserved.Load() {
		t.Fatalf("reorged release snapshot selector_observed=%t error=%v", selectorObserved.Load(), err)
	}
}

// Both contract and header exact-block reads must abandon an upstream request
// immediately on cancellation, rather than waiting for background defaults.
func TestReleaseExactBlockReadersCancelProvider(t *testing.T) {
	for _, test := range []struct {
		name   string
		method string
		call   func(context.Context, *ChainClient) error
	}{
		{
			name:   "epoch start contract view",
			method: "eth_call",
			call: func(ctx context.Context, chain *ChainClient) error {
				_, err := chain.ReleaseEpochStartBlockAtContext(ctx, 7, big.NewInt(3))
				return err
			},
		},
		{
			name:   "numbered block hash",
			method: "eth_getBlockByNumber",
			call: func(ctx context.Context, chain *ChainClient) error {
				_, err := chain.BlockHashContext(ctx, 7)
				return err
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			started := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				defer request.Body.Close()
				var rpcRequest struct {
					Id     json.RawMessage `json:"id"`
					Method string          `json:"method"`
				}
				if err := json.NewDecoder(request.Body).Decode(&rpcRequest); err != nil {
					writer.WriteHeader(http.StatusBadRequest)
					return
				}
				if rpcRequest.Method == "eth_chainId" {
					writer.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": rpcRequest.Id, "result": "0x3b1"})
					return
				}
				if rpcRequest.Method != test.method {
					writer.WriteHeader(http.StatusBadRequest)
					return
				}
				close(started)
				<-request.Context().Done()
			}))
			defer server.Close()
			chain, err := DialReleaseChainContext(context.Background(), []string{server.URL}, common.Address{1})
			if err != nil {
				t.Fatal(err)
			}
			defer chain.Close()
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			go func() { done <- test.call(ctx, chain) }()
			<-started
			cancel()
			if err := <-done; !errors.Is(err, context.Canceled) {
				t.Fatalf("canceled %s error=%v", test.name, err)
			}
		})
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

// Numbered header reads must reject both a null provider result and a header
// for another height before its hash can enter the exact-block identity cache.
func TestBlockHashContextRejectsEmptyAndMismatchedHeaders(t *testing.T) {
	tests := []struct {
		result any
		want   string
	}{
		{result: nil, want: "not found"},
		{result: &types.Header{
			UncleHash: types.EmptyUncleHash, TxHash: types.EmptyTxsHash,
			ReceiptHash: types.EmptyReceiptsHash, Difficulty: big.NewInt(0),
			Number: big.NewInt(8), GasLimit: 1, Time: 1,
		}, want: "another height"},
	}
	for _, test := range tests {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			defer request.Body.Close()
			var rpcRequest struct {
				ID json.RawMessage `json:"id"`
			}
			if err := json.NewDecoder(request.Body).Decode(&rpcRequest); err != nil {
				writer.WriteHeader(http.StatusBadRequest)
				return
			}
			writer.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": rpcRequest.ID, "result": test.result})
		}))
		client, err := ethclient.Dial(server.URL)
		if err != nil {
			server.Close()
			t.Errorf("dial header fixture: %v", err)
			continue
		}
		chain := &ChainClient{client: client}
		_, err = chain.BlockHashContext(context.Background(), 7)
		client.Close()
		server.Close()
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), test.want) {
			t.Errorf("header result %T error=%v want substring %q", test.result, err, test.want)
		}
	}
}

// eth_getBlockByHash cannot authenticate a pair merely by returning some
// header at the requested number; the header's recomputed hash must match too.
func TestExactBlockIdentityRejectsMismatchedHashHeader(t *testing.T) {
	expectedHeader := &types.Header{
		UncleHash: types.EmptyUncleHash, TxHash: types.EmptyTxsHash,
		ReceiptHash: types.EmptyReceiptsHash, Difficulty: big.NewInt(0),
		Number: big.NewInt(7), GasLimit: 1, Time: 1,
	}
	wrongHeader := types.CopyHeader(expectedHeader)
	wrongHeader.Time = 2
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer request.Body.Close()
		var rpcRequest struct {
			ID json.RawMessage `json:"id"`
		}
		if err := json.NewDecoder(request.Body).Decode(&rpcRequest); err != nil {
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": rpcRequest.ID, "result": wrongHeader})
	}))
	client, err := ethclient.Dial(server.URL)
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	chain := &ChainClient{client: client}
	err = chain.validateBlockIdentityContext(context.Background(), 7, [32]byte(expectedHeader.Hash()))
	client.Close()
	server.Close()
	if err == nil || !strings.Contains(err.Error(), "hash response identifies") {
		t.Fatalf("mismatched hash header error=%v", err)
	}
}

// The finalized-head identity optimization must not retain one entry per poll
// forever. A cache hit at capacity also must not evict unrelated identities.
func TestBlockIdentityCacheIsBounded(t *testing.T) {
	chain := &ChainClient{}
	for block := uint64(1); block <= chainBlockIdentityCacheCapacity; block++ {
		if err := chain.rememberBlockIdentity(block, chainTestBlockIdentityHash(block)); err != nil {
			t.Fatal(err)
		}
	}
	if size := len(chain.blockNumbers); size != chainBlockIdentityCacheCapacity {
		t.Fatalf("identity cache size=%d want=%d", size, chainBlockIdentityCacheCapacity)
	}
	hitBlock := uint64(chainBlockIdentityCacheCapacity / 2)
	if err := chain.rememberBlockIdentity(hitBlock, chainTestBlockIdentityHash(hitBlock)); err != nil {
		t.Fatal(err)
	}
	if size := len(chain.blockNumbers); size != chainBlockIdentityCacheCapacity {
		t.Fatalf("cache hit changed full identity cache size to %d", size)
	}

	total := uint64(chainBlockIdentityCacheCapacity*3 + 1)
	for block := uint64(chainBlockIdentityCacheCapacity + 1); block <= total; block++ {
		blockHash := chainTestBlockIdentityHash(block)
		if err := chain.rememberBlockIdentity(block, blockHash); err != nil {
			t.Fatal(err)
		}
		chain.stateLock.Lock()
		size := len(chain.blockNumbers)
		storedBlock, found := chain.blockNumbers[blockHash]
		chain.stateLock.Unlock()
		if size > chainBlockIdentityCacheCapacity {
			t.Fatalf("identity cache size=%d exceeds capacity=%d", size, chainBlockIdentityCacheCapacity)
		}
		if !found || storedBlock != block {
			t.Fatalf("new identity block=%d stored=%d found=%t", block, storedBlock, found)
		}
	}
	if _, found := chain.knownBlockIdentity(chainTestBlockIdentityHash(1)); found {
		t.Fatal("oldest identity survived repeated capacity eviction")
	}
}

// Concurrent finalized-head observations and exact-block reads must preserve
// the fixed cache bound and must never corrupt an identity that remains cached.
func TestBlockIdentityCacheConcurrentResetIsBounded(t *testing.T) {
	const workers = 16
	const identitiesPerWorker = chainBlockIdentityCacheCapacity * 2
	chain := &ChainClient{}
	start := make(chan struct{})
	errors := make(chan error, workers)
	var wait sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			for index := 0; index < identitiesPerWorker; index++ {
				block := uint64(worker*identitiesPerWorker + index + 1)
				blockHash := chainTestBlockIdentityHash(block)
				if err := chain.rememberBlockIdentity(block, blockHash); err != nil {
					errors <- err
					return
				}
				if storedBlock, found := chain.knownBlockIdentity(blockHash); found && storedBlock != block {
					errors <- fmt.Errorf("identity block=%d stored=%d", block, storedBlock)
					return
				}
				chain.stateLock.Lock()
				size := len(chain.blockNumbers)
				chain.stateLock.Unlock()
				if size > chainBlockIdentityCacheCapacity {
					errors <- fmt.Errorf("identity cache size=%d exceeds capacity=%d", size, chainBlockIdentityCacheCapacity)
					return
				}
			}
		}()
	}
	close(start)
	wait.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
	chain.stateLock.Lock()
	size := len(chain.blockNumbers)
	for blockHash, block := range chain.blockNumbers {
		if encodedBlock := binary.BigEndian.Uint64(blockHash[24:]); encodedBlock != block {
			t.Errorf("cached hash encodes block=%d, stored=%d", encodedBlock, block)
		}
	}
	chain.stateLock.Unlock()
	if size == 0 || size > chainBlockIdentityCacheCapacity {
		t.Fatalf("final identity cache size=%d capacity=%d", size, chainBlockIdentityCacheCapacity)
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
		"blockHash":        depositedTestBlockHash(1).Hex(),
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
		case "eth_getBlockByNumber":
			result = depositedTestHeader(request.Params, 5)
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
		{name: "duplicate index with another transaction", logs: func() []map[string]any {
			first, second := base(), base()
			second["transactionHash"] = common.HexToHash("0x99").Hex()
			return []map[string]any{first, second}
		}},
		{name: "conflicting block identity", logs: func() []map[string]any {
			first, second := base(), depositedLogJSON(contract, 5, 1, 100, 1)
			second["blockHash"] = common.HexToHash("0x99").Hex()
			return []map[string]any{first, second}
		}},
	}
	for _, testCase := range cases {
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
			var result any = "0x0"
			if request.Method == "eth_chainId" {
				result = "0x3b1"
			} else if request.Method == "eth_getLogs" {
				result = testCase.logs()
			} else if request.Method == "eth_getBlockByNumber" {
				result = depositedTestHeader(request.Params, 5)
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
		} else if request.Method == "eth_getBlockByNumber" {
			response["result"] = depositedTestHeader(request.Params, toBlock)
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
