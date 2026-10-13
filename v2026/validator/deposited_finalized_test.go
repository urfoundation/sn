package validator

// Finalized deposit scans must obey public-RPC range limits and commit their
// cache only after both demand and conviction share one canonical checkpoint.

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
)

// Generates distinct identities for every height, including genesis and the
// largest uint64 value, independently from Ethereum header recomputation.
func depositedTestBlockHash(block uint64) common.Hash {
	return crypto.Keccak256Hash([]byte(fmt.Sprintf("synthetic-deposit-block-%d", block)))
}

// Supplies a minimal explicit RPC identity for older single-request fixtures.
func depositedTestHeader(params []json.RawMessage, finalized uint64) any {
	var selector string
	if len(params) != 2 || json.Unmarshal(params[0], &selector) != nil {
		return nil
	}
	block := finalized
	if selector != "finalized" {
		var err error
		block, err = strconv.ParseUint(strings.TrimPrefix(selector, "0x"), 16, 64)
		if err != nil {
			return nil
		}
	}
	return map[string]any{"number": hexutil.EncodeUint64(block), "hash": depositedTestBlockHash(block).Hex()}
}

// Captures one inclusive requested range for exact paging assertions.
type depositedTestRange struct{ from, to uint64 }

// Test-owned state changes occur between completed scans or under stateLock.
// onLogs forces a fork at an exact request boundary without scheduling luck.
type depositedRPCFixture struct {
	stateLock      sync.Mutex
	finalized      uint64
	epoch          uint64
	epochStart     uint64
	contract       common.Address
	blockHashes    map[uint64]common.Hash
	logs           []map[string]any
	ranges         []depositedTestRange
	finalizedReads int
	latestReads    int
	failEpochScan  bool
	onLogs         func(*depositedRPCFixture)
}

// Starts a read-only RPC fixture with an enforced 1,000-block inclusive cap.
func (self *depositedRPCFixture) client(t *testing.T) *ChainClient {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var call struct {
			ID     json.RawMessage   `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		response := func() map[string]any {
			self.stateLock.Lock()
			defer self.stateLock.Unlock()
			response := map[string]any{"jsonrpc": "2.0", "id": call.ID}
			fail := func(message string) map[string]any {
				response["error"] = map[string]any{"code": -32000, "message": message}
				return response
			}
			switch call.Method {
			case "eth_chainId":
				response["result"] = "0x3b1"
			case "eth_blockNumber":
				self.latestReads++
				response["result"] = hexutil.EncodeUint64(self.finalized + 10)
			case "eth_getBlockByNumber":
				var selector string
				if len(call.Params) != 2 || json.Unmarshal(call.Params[0], &selector) != nil {
					return fail("bad header selector")
				}
				block := self.finalized
				if selector == "finalized" {
					self.finalizedReads++
				} else {
					var err error
					block, err = strconv.ParseUint(strings.TrimPrefix(selector, "0x"), 16, 64)
					if err != nil {
						return fail("bad numbered selector")
					}
				}
				hash := depositedTestBlockHash(block)
				if replacement, ok := self.blockHashes[block]; ok {
					hash = replacement
				}
				response["result"] = map[string]any{"number": hexutil.EncodeUint64(block), "hash": hash.Hex()}
			case "eth_call":
				var input map[string]string
				var selector gethrpc.BlockNumberOrHash
				if len(call.Params) != 2 || json.Unmarshal(call.Params[0], &input) != nil || json.Unmarshal(call.Params[1], &selector) != nil || selector.BlockHash == nil || *selector.BlockHash != depositedTestBlockHash(self.finalized) || !selector.RequireCanonical {
					return fail("unbound deposit view")
				}
				method := input["input"]
				if method == "" {
					method = input["data"]
				}
				epochSelector, startSelector := evmSelector("epoch()"), evmSelector("epochStartBlock()")
				value := self.epoch
				switch method {
				case hexutil.Encode(epochSelector[:]):
				case hexutil.Encode(startSelector[:]):
					value = self.epochStart
				default:
					return fail("unexpected deposit view")
				}
				response["result"] = fmt.Sprintf("0x%064x", value)
			case "eth_getLogs":
				var filter struct {
					FromBlock string           `json:"fromBlock"`
					ToBlock   string           `json:"toBlock"`
					Topics    [][]string       `json:"topics"`
					Address   []common.Address `json:"address"`
				}
				if len(call.Params) != 1 || json.Unmarshal(call.Params[0], &filter) != nil {
					return fail("bad log filter")
				}
				from, fromErr := hexutil.DecodeUint64(filter.FromBlock)
				to, toErr := hexutil.DecodeUint64(filter.ToBlock)
				if fromErr != nil || toErr != nil || to < from || to-from >= 1_000 || to > self.finalized || len(filter.Address) != 1 || filter.Address[0] != self.contract {
					return fail("log filter exceeds finalized 1000-block range")
				}
				self.ranges = append(self.ranges, depositedTestRange{from: from, to: to})
				if self.onLogs != nil {
					self.onLogs(self)
				}
				if self.failEpochScan && len(filter.Topics) > 1 {
					return fail("injected epoch scan failure")
				}
				logs := []map[string]any{}
				for _, log := range self.logs {
					block, err := hexutil.DecodeUint64(log["blockNumber"].(string))
					if err != nil || block < from || block > to {
						continue
					}
					if len(filter.Topics) > 1 && log["topics"].([]string)[1] != filter.Topics[1][0] {
						continue
					}
					logs = append(logs, log)
				}
				response["result"] = logs
			default:
				return fail("unexpected RPC " + call.Method)
			}
			return response
		}()
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(response)
	}))
	t.Cleanup(server.Close)
	chain, err := DialChain([]string{server.URL}, self.contract)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(chain.Close)
	return chain
}

// Places one deposit at a chosen canonical block without narrowing its height.
func depositedTestLog(contract common.Address, block uint64) map[string]any {
	log := depositedLogJSON(contract, 5, 1, 100, 0)
	log["blockNumber"], log["blockHash"] = hexutil.EncodeUint64(block), depositedTestBlockHash(block).Hex()
	log["transactionHash"] = crypto.Keccak256Hash([]byte(fmt.Sprintf("deposit-transaction-%d", block))).Hex()
	return log
}

// Exact-cap, adjacent-cap, multiple-cap and maximum-height scans retain every
// inclusive endpoint exactly once, never issuing a 1,001-block request.
func TestDepositedSumsUsesExactInclusiveThousandBlockChunks(t *testing.T) {
	cases := []struct {
		from, to uint64
		ranges   []depositedTestRange
	}{
		{from: 0, to: 999, ranges: []depositedTestRange{{from: 0, to: 999}}},
		{from: 0, to: 1000, ranges: []depositedTestRange{{from: 0, to: 999}, {from: 1000, to: 1000}}},
		{from: 123, to: 2122, ranges: []depositedTestRange{{from: 123, to: 1122}, {from: 1123, to: 2122}}},
		{from: 123, to: 2123, ranges: []depositedTestRange{{from: 123, to: 1122}, {from: 1123, to: 2122}, {from: 2123, to: 2123}}},
		{from: ^uint64(0) - 2000, to: ^uint64(0), ranges: []depositedTestRange{{from: ^uint64(0) - 2000, to: ^uint64(0) - 1001}, {from: ^uint64(0) - 1000, to: ^uint64(0) - 1}, {from: ^uint64(0), to: ^uint64(0)}}},
	}
	for _, testCase := range cases {
		fixture := &depositedRPCFixture{finalized: testCase.to, contract: common.HexToAddress("0xcc")}
		seenBlocks := map[uint64]bool{}
		for _, blockRange := range testCase.ranges {
			for _, block := range []uint64{blockRange.from, blockRange.to} {
				if !seenBlocks[block] {
					fixture.logs = append(fixture.logs, depositedTestLog(fixture.contract, block))
					seenBlocks[block] = true
				}
			}
		}
		chain := fixture.client(t)
		sums, err := chain.DepositedSums(testCase.from, testCase.to, nil)
		if err != nil {
			t.Fatalf("[%d,%d]: %v", testCase.from, testCase.to, err)
		}
		fixture.stateLock.Lock()
		gotRanges, latestReads := append([]depositedTestRange(nil), fixture.ranges...), fixture.latestReads
		fixture.stateLock.Unlock()
		if !reflect.DeepEqual(gotRanges, testCase.ranges) || latestReads != 0 || sums.Get(big.NewInt(1)).Int64() != int64(100*len(seenBlocks)) {
			t.Errorf("[%d,%d] ranges=%v sum=%s latest reads=%d", testCase.from, testCase.to, gotRanges, sums.Get(big.NewInt(1)), latestReads)
		}
	}
}

// A reorg is forced after the second chunk request. Either an orphaned log
// block or a changed finalized checkpoint invalidates the complete result.
func TestDepositedSumsRejectsForkAcrossChunks(t *testing.T) {
	for _, changedBlock := range []uint64{1, 1001} {
		fixture := &depositedRPCFixture{finalized: 1001, contract: common.HexToAddress("0xcc"), blockHashes: map[uint64]common.Hash{}}
		fixture.logs = []map[string]any{depositedTestLog(fixture.contract, 1), depositedTestLog(fixture.contract, 1001)}
		fixture.onLogs = func(self *depositedRPCFixture) {
			if len(self.ranges) == 2 {
				self.blockHashes[changedBlock] = common.HexToHash("0x99")
			}
		}
		sums, err := fixture.client(t).DepositedSums(0, 1001, nil)
		if err == nil || sums != nil {
			t.Errorf("fork at block %d returned sums=%v err=%v", changedBlock, sums, err)
		}
	}
}

// Even a valid first delta is provisional until the epoch scan succeeds.
// A retry commits once, and a repeated finalized checkpoint adds no duplicate.
func TestDepositedLedgerCommitsOnlyAfterCompleteFinalizedScan(t *testing.T) {
	fixture := &depositedRPCFixture{finalized: 105, epoch: 5, epochStart: 101, contract: common.HexToAddress("0xcc"), failEpochScan: true}
	fixture.logs = []map[string]any{depositedTestLog(fixture.contract, 101)}
	steerer := &Steerer{chain: fixture.client(t), deposits: depositLedger{conviction: DepositSums{"1": big.NewInt(25)}, scannedThrough: 100, scannedHash: [32]byte(depositedTestBlockHash(100)), started: true}}
	if _, _, err := steerer.gatherDeposits(big.NewInt(5)); err == nil {
		t.Fatal("injected second scan failure was accepted")
	}
	if steerer.deposits.scannedThrough != 100 || steerer.deposits.conviction.Get(big.NewInt(1)).Int64() != 25 {
		t.Fatalf("failed second scan advanced ledger: %+v", steerer.deposits)
	}
	fixture.stateLock.Lock()
	fixture.failEpochScan = false
	fixture.stateLock.Unlock()
	for attempt := 0; attempt < 2; attempt++ {
		epochSums, conviction, err := steerer.gatherDeposits(big.NewInt(5))
		if err != nil || epochSums.Get(big.NewInt(1)).Int64() != 100 || conviction.Get(big.NewInt(1)).Int64() != 125 || steerer.deposits.scannedThrough != 105 || steerer.deposits.scannedHash != [32]byte(depositedTestBlockHash(105)) {
			t.Fatalf("retry %d epoch=%v conviction=%v ledger=%+v err=%v", attempt, epochSums, conviction, steerer.deposits, err)
		}
	}
}

// Finalized head regression, cached fork, and maximum-height reuse must never
// rewind/wrap the incremental cursor or modify previously verified sums.
func TestDepositedLedgerRejectsForkAndHeadRegressionWithoutAdvancing(t *testing.T) {
	for _, name := range []string{"fork", "head regression", "epoch mismatch"} {
		fixture := &depositedRPCFixture{finalized: 105, epoch: 5, epochStart: 101, contract: common.HexToAddress("0xcc"), blockHashes: map[uint64]common.Hash{}}
		if name == "fork" {
			fixture.blockHashes[100] = common.HexToHash("0x99")
		}
		if name == "head regression" {
			fixture.finalized, fixture.epochStart = 99, 90
		}
		if name == "epoch mismatch" {
			fixture.epoch = 6
		}
		steerer := &Steerer{chain: fixture.client(t), deposits: depositLedger{conviction: DepositSums{"1": big.NewInt(25)}, scannedThrough: 100, scannedHash: [32]byte(depositedTestBlockHash(100)), started: true}}
		if _, _, err := steerer.gatherDeposits(big.NewInt(5)); err == nil || steerer.deposits.scannedThrough != 100 || steerer.deposits.conviction.Get(big.NewInt(1)).Int64() != 25 {
			t.Errorf("%s advanced or accepted cached ledger: %+v err=%v", name, steerer.deposits, err)
		}
	}
	fixture := &depositedRPCFixture{finalized: ^uint64(0), epoch: 5, epochStart: ^uint64(0), contract: common.HexToAddress("0xcc")}
	steerer := &Steerer{chain: fixture.client(t), deposits: depositLedger{conviction: DepositSums{"1": big.NewInt(25)}, scannedThrough: ^uint64(0), scannedHash: [32]byte(depositedTestBlockHash(^uint64(0))), started: true}}
	_, conviction, err := steerer.gatherDeposits(big.NewInt(5))
	if err != nil || conviction.Get(big.NewInt(1)).Int64() != 25 || len(fixture.ranges) != 1 || fixture.ranges[0].from != ^uint64(0) {
		t.Fatalf("maximum cursor wrapped: ranges=%v conviction=%v err=%v", fixture.ranges, conviction, err)
	}
}

// The public Subtensor RPC returns a synthetic hash different from Keccak of
// the standard header. All three readers and EIP-1898 must retain that hash.
func TestDepositedSyntheticRPCBlockIdentitySurvivesAllReaders(t *testing.T) {
	header := &types.Header{Number: big.NewInt(7), Difficulty: new(big.Int), GasLimit: 1, Time: 1}
	explicitHash := depositedTestBlockHash(7)
	if header.Hash() == explicitHash {
		t.Fatal("synthetic fixture unexpectedly equals Ethereum header hash")
	}
	encoded, err := json.Marshal(header)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	fields["hash"] = explicitHash.Hex()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var call struct {
			ID     json.RawMessage   `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		response := map[string]any{"jsonrpc": "2.0", "id": call.ID}
		switch call.Method {
		case "eth_chainId":
			response["result"] = "0x3b1"
		case "eth_getBlockByNumber", "eth_getBlockByHash":
			response["result"] = fields
		case "eth_call":
			var selector gethrpc.BlockNumberOrHash
			if len(call.Params) != 2 || json.Unmarshal(call.Params[1], &selector) != nil || selector.BlockHash == nil || *selector.BlockHash != explicitHash || !selector.RequireCanonical {
				response["error"] = map[string]any{"code": -32000, "message": "wrong synthetic canonical hash"}
			} else {
				response["result"] = "0x01"
			}
		default:
			response["error"] = map[string]any{"code": -32000, "message": "unexpected RPC"}
		}
		_ = json.NewEncoder(writer).Encode(response)
	}))
	defer server.Close()
	chain, err := DialChain([]string{server.URL}, common.HexToAddress("0xcc"))
	if err != nil {
		t.Fatal(err)
	}
	defer chain.Close()
	block, hash, err := chain.FinalizedBlockContext(context.Background())
	if err != nil || block != 7 || hash != [32]byte(explicitHash) {
		t.Fatalf("finalized synthetic identity=%d/0x%x err=%v", block, hash, err)
	}
	numberedHash, err := chain.BlockHashContext(context.Background(), 7)
	if err != nil || numberedHash != hash {
		t.Fatalf("numbered synthetic identity=0x%x err=%v", numberedHash, err)
	}
	chain.stateLock.Lock()
	clear(chain.blockNumbers)
	chain.stateLock.Unlock()
	if err := chain.validateBlockIdentityContext(context.Background(), 7, hash); err != nil {
		t.Fatalf("hash reader rejected synthetic identity: %v", err)
	}
	if _, err := chain.ethCallAtHashContext(context.Background(), chain.contractAddr, []byte{1}, 7, hash); err != nil {
		t.Fatalf("canonical call lost synthetic identity: %v", err)
	}
}

// Empty/malformed raw envelopes cannot authenticate either a numbered block
// or a requested hash. A valid shape with the wrong number/hash is also fatal.
func TestDepositedSyntheticRPCBlockIdentityRejectsMalformedAndForeignHeaders(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		result any
	}{
		{name: "null", result: nil},
		{name: "missing number", result: map[string]any{"hash": depositedTestBlockHash(7).Hex()}},
		{name: "missing hash", result: map[string]any{"number": "0x7"}},
		{name: "zero hash", result: map[string]any{"number": "0x7", "hash": common.Hash{}.Hex()}},
		{name: "negative number", result: map[string]any{"number": "-0x7", "hash": depositedTestBlockHash(7).Hex()}},
		{name: "overflow number", result: map[string]any{"number": "0x10000000000000000", "hash": depositedTestBlockHash(7).Hex()}},
		{name: "wrong number", result: map[string]any{"number": "0x8", "hash": depositedTestBlockHash(7).Hex()}},
		{name: "wrong hash", result: map[string]any{"number": "0x7", "hash": depositedTestBlockHash(8).Hex()}},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			var call struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
			}
			if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
				http.Error(writer, err.Error(), http.StatusBadRequest)
				return
			}
			result := testCase.result
			if call.Method == "eth_chainId" {
				result = "0x3b1"
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": call.ID, "result": result})
		}))
		chain, err := DialChain([]string{server.URL}, common.HexToAddress("0xcc"))
		if err != nil {
			server.Close()
			t.Fatal(err)
		}
		// A numbered lookup has no predetermined hash; a different canonical
		// hash is judged only when the caller compares its captured identity.
		if testCase.name != "wrong hash" {
			if _, err := chain.BlockHashContext(context.Background(), 7); err == nil {
				t.Errorf("%s numbered header accepted", testCase.name)
			}
		}
		if err := chain.validateBlockIdentityContext(context.Background(), 7, [32]byte(depositedTestBlockHash(7))); err == nil {
			t.Errorf("%s by-hash header accepted", testCase.name)
		}
		if testCase.name != "wrong number" && testCase.name != "wrong hash" {
			if _, _, err := chain.FinalizedBlockContext(context.Background()); err == nil {
				t.Errorf("%s finalized header accepted", testCase.name)
			}
		}
		chain.Close()
		server.Close()
	}
}

// A public compatibility caller cannot scan the latest, unfinalized range.
func TestDepositedSumsRejectsRangeAboveFinalizedHead(t *testing.T) {
	fixture := &depositedRPCFixture{finalized: 100, contract: common.HexToAddress("0xcc")}
	sums, err := fixture.client(t).DepositedSums(90, 101, nil)
	if err == nil || sums != nil || len(fixture.ranges) != 0 {
		t.Fatalf("unfinalized range was scanned: sums=%v ranges=%v err=%v", sums, fixture.ranges, err)
	}
}
