// Exact-block batch tests cover the public-provider transport used by release
// steering. The fixture reverses JSON-RPC responses so positional luck cannot
// hide an id/order bug.
package validator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	gethrpc "github.com/ethereum/go-ethereum/rpc"

	"github.com/urfoundation/sn/stabi"
)

var chainBatchTestHeader = &types.Header{
	UncleHash: types.EmptyUncleHash, TxHash: types.EmptyTxsHash,
	ReceiptHash: types.EmptyReceiptsHash, Difficulty: big.NewInt(0),
	Number: big.NewInt(123), GasLimit: 1, Time: 1,
}

var chainBatchTestBlockHash = [32]byte(chainBatchTestHeader.Hash())

var chainBatchAdjacentHeader = &types.Header{
	ParentHash: common.Hash(chainBatchTestBlockHash), UncleHash: types.EmptyUncleHash,
	TxHash: types.EmptyTxsHash, ReceiptHash: types.EmptyReceiptsHash,
	Difficulty: big.NewInt(0), Number: big.NewInt(124), GasLimit: 1, Time: 2,
}

var chainBatchAdjacentBlockHash = [32]byte(chainBatchAdjacentHeader.Hash())

// Fault switches select malformed responses without timing or scheduler
// dependencies.
type chainBatchRPCFaults struct {
	bindingErrorClient byte
	bindingEmptyClient byte
	duplicateHotkeys   bool
	noncanonicalCount  bool
}

// Captures the physical request shape as well as the semantic fixture state.
type chainBatchRPCFixture struct {
	stateLock       sync.Mutex
	faults          chainBatchRPCFaults
	metagraphCount  uint16
	batchSizes      []int
	blockHashes     []common.Hash
	canonicalFlags  []bool
	singleCallCount int
	bindingABI      *stabi.STCoordinator
	parsedABI       *abi.ABI
	bindingSelector [4]byte
	countSelector   [4]byte
	hotkeySelector  [4]byte
}

// Minimal request envelope accepted by the deterministic HTTP fixture.
type chainBatchRPCRequest struct {
	ID     json.RawMessage   `json:"id"`
	Method string            `json:"method"`
	Params []json.RawMessage `json:"params"`
}

// Creates one fixture with selectors derived from the same checked-in ABI as
// production.
func newChainBatchRPCFixture(t *testing.T, count uint16, faults chainBatchRPCFaults) *chainBatchRPCFixture {
	t.Helper()
	coordinator := stabi.NewSTCoordinator()
	parsedABI, err := stabi.STCoordinatorMetaData.ParseABI()
	if err != nil {
		t.Fatal(err)
	}
	var bindingSelector [4]byte
	copy(bindingSelector[:], coordinator.PackBindingAt([16]byte{}, big.NewInt(1))[:4])
	return &chainBatchRPCFixture{
		faults:          faults,
		metagraphCount:  count,
		bindingABI:      coordinator,
		parsedABI:       parsedABI,
		bindingSelector: bindingSelector,
		countSelector:   evmSelector("getUidCount(uint16)"),
		hotkeySelector:  evmSelector("getHotkey(uint16,uint16)"),
	}
}

// Encodes the complete tuple returned by bindingAt for one client id.
func (self *chainBatchRPCFixture) bindingResult(clientID [16]byte) (string, error) {
	uid := uint16(clientID[14])<<8 | uint16(clientID[15])
	record := stabi.STCoordinatorBindingRecord{
		FleetId:        [32]byte{0: 1, 30: clientID[14], 31: clientID[15]},
		Hotkey:         chainBatchHotkey(uid),
		ClientKey:      [32]byte{0: 2, 30: clientID[14], 31: clientID[15]},
		CommitmentHash: [32]byte{0: 3, 30: clientID[14], 31: clientID[15]},
		Generation:     2,
		ValidFromEpoch: 7,
		ValidToEpoch:   17,
		CleanedAtEpoch: 0,
		Uid:            uid,
		Cleaned:        false,
	}
	encoded, err := self.parsedABI.Methods["bindingAt"].Outputs.Pack(true, record)
	if err != nil {
		return "", err
	}
	return hexutil.Encode(encoded), nil
}

// Produces a nonzero unique runtime hotkey for one UID.
func chainBatchHotkey(uid uint16) [32]byte {
	return [32]byte{0: 0xa5, 30: byte(uid >> 8), 31: byte(uid)}
}

// Handles one eth_call and returns either a result or an explicit JSON-RPC
// error object.
func (self *chainBatchRPCFixture) response(request chainBatchRPCRequest) map[string]any {
	response := map[string]any{"jsonrpc": "2.0", "id": request.ID}
	if request.Method == "eth_getBlockByHash" {
		var blockHash common.Hash
		if len(request.Params) != 2 || json.Unmarshal(request.Params[0], &blockHash) != nil {
			response["error"] = map[string]any{"code": -32602, "message": "malformed block hash request"}
			return response
		}
		switch {
		case bytes.Equal(blockHash[:], chainBatchTestBlockHash[:]):
			response["result"] = chainBatchTestHeader
		case bytes.Equal(blockHash[:], chainBatchAdjacentBlockHash[:]):
			response["result"] = chainBatchAdjacentHeader
		default:
			response["result"] = nil
		}
		return response
	}
	if request.Method != "eth_call" || len(request.Params) != 2 {
		response["error"] = map[string]any{"code": -32601, "message": "unsupported fixture request"}
		return response
	}
	var call struct {
		To    common.Address `json:"to"`
		Data  hexutil.Bytes  `json:"data"`
		Input hexutil.Bytes  `json:"input"`
	}
	var blockSelector gethrpc.BlockNumberOrHash
	if err := json.Unmarshal(request.Params[0], &call); err != nil || json.Unmarshal(request.Params[1], &blockSelector) != nil {
		response["error"] = map[string]any{"code": -32602, "message": "malformed fixture request"}
		return response
	}
	if blockSelector.BlockHash == nil || *blockSelector.BlockHash != common.Hash(chainBatchTestBlockHash) || !blockSelector.RequireCanonical {
		response["error"] = map[string]any{"code": -32000, "message": "synthetic noncanonical block identity"}
		return response
	}
	calldata := call.Data
	if len(calldata) == 0 {
		calldata = call.Input
	}
	if len(calldata) < 4 {
		response["error"] = map[string]any{"code": -32602, "message": "malformed fixture request"}
		return response
	}
	self.stateLock.Lock()
	self.blockHashes = append(self.blockHashes, *blockSelector.BlockHash)
	self.canonicalFlags = append(self.canonicalFlags, blockSelector.RequireCanonical)
	self.stateLock.Unlock()
	var selector [4]byte
	copy(selector[:], calldata[:4])
	switch selector {
	case self.bindingSelector:
		if len(calldata) != 68 {
			response["error"] = map[string]any{"code": -32602, "message": "malformed binding calldata"}
			return response
		}
		var clientID [16]byte
		copy(clientID[:], calldata[4:20])
		if clientID[15] == self.faults.bindingErrorClient && self.faults.bindingErrorClient != 0 {
			response["error"] = map[string]any{"code": -32000, "message": "synthetic binding failure"}
			return response
		}
		if clientID[15] == self.faults.bindingEmptyClient && self.faults.bindingEmptyClient != 0 {
			response["result"] = "0x"
			return response
		}
		result, err := self.bindingResult(clientID)
		if err != nil {
			response["error"] = map[string]any{"code": -32000, "message": err.Error()}
			return response
		}
		response["result"] = result
	case self.countSelector:
		encoded := make([]byte, 32)
		encoded[30] = byte(self.metagraphCount >> 8)
		encoded[31] = byte(self.metagraphCount)
		if self.faults.noncanonicalCount {
			encoded[0] = 1
		}
		response["result"] = hexutil.Encode(encoded)
	case self.hotkeySelector:
		if len(calldata) != 68 {
			response["error"] = map[string]any{"code": -32602, "message": "malformed hotkey calldata"}
			return response
		}
		uid := uint16(calldata[66])<<8 | uint16(calldata[67])
		if self.faults.duplicateHotkeys && uid == 1 {
			uid = 0
		}
		hotkey := chainBatchHotkey(uid)
		response["result"] = hexutil.Encode(hotkey[:])
	default:
		response["error"] = map[string]any{"code": -32601, "message": fmt.Sprintf("unknown selector 0x%x", selector)}
	}
	return response
}

// Serves both singleton count calls and reversed batch replies.
func (self *chainBatchRPCFixture) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	defer request.Body.Close()
	var raw json.RawMessage
	if err := json.NewDecoder(request.Body).Decode(&raw); err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	trimmed := bytes.TrimSpace(raw)
	writer.Header().Set("Content-Type", "application/json")
	if len(trimmed) != 0 && trimmed[0] == '[' {
		var requests []chainBatchRPCRequest
		if err := json.Unmarshal(trimmed, &requests); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		self.stateLock.Lock()
		self.batchSizes = append(self.batchSizes, len(requests))
		self.stateLock.Unlock()
		responses := make([]map[string]any, len(requests))
		for index, rpcRequest := range requests {
			responses[len(requests)-1-index] = self.response(rpcRequest)
		}
		_ = json.NewEncoder(writer).Encode(responses)
		return
	}
	var rpcRequest chainBatchRPCRequest
	if err := json.Unmarshal(trimmed, &rpcRequest); err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	self.stateLock.Lock()
	self.singleCallCount++
	self.stateLock.Unlock()
	_ = json.NewEncoder(writer).Encode(self.response(rpcRequest))
}

// Connects a release client directly to the deterministic batch fixture.
func chainBatchTestClient(t *testing.T, fixture *chainBatchRPCFixture) (*ChainClient, func()) {
	t.Helper()
	server := httptest.NewServer(fixture)
	client, err := ethclient.Dial(server.URL)
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	chain := &ChainClient{
		client:       client,
		coordinator:  fixture.bindingABI,
		contractAddr: common.HexToAddress("0x1000000000000000000000000000000000000001"),
		release:      true,
	}
	closeFixture := func() {
		client.Close()
		server.Close()
	}
	return chain, closeFixture
}

// The full 1,000-provider release shape must consume twenty bounded batches
// per census and retain caller order when responses arrive in reverse order.
func TestReleaseExactBlockBatchesBindingsAndMetagraph(t *testing.T) {
	const providerCount = 1000
	fixture := newChainBatchRPCFixture(t, providerCount, chainBatchRPCFaults{})
	chain, closeFixture := chainBatchTestClient(t, fixture)
	defer closeFixture()
	clientIDs := make([][16]byte, providerCount)
	for index := range clientIDs {
		clientIDs[index][14] = byte(index >> 8)
		clientIDs[index][15] = byte(index)
	}
	bindings, err := chain.ReleaseBindingsAtHashContext(context.Background(), 123, chainBatchTestBlockHash, clientIDs, big.NewInt(7))
	if err != nil || len(bindings) != len(clientIDs) {
		t.Fatalf("binding census count=%d error=%v", len(bindings), err)
	}
	for index, binding := range bindings {
		if !binding.Active || binding.Record.Uid != uint16(index) || binding.Record.Hotkey != chainBatchHotkey(uint16(index)) {
			t.Fatalf("binding %d was reordered or corrupted: %+v", index, binding)
		}
	}
	hotkeys, err := chain.MetagraphHotkeysAtHashContext(context.Background(), 123, chainBatchTestBlockHash, 521)
	if err != nil || len(hotkeys) != len(clientIDs) {
		t.Fatalf("metagraph census count=%d error=%v", len(hotkeys), err)
	}
	for index := range clientIDs {
		if uid, ok := hotkeys[chainBatchHotkey(uint16(index))]; !ok || uid != uint16(index) {
			t.Fatalf("metagraph uid %d was reordered or omitted", index)
		}
	}
	fixture.stateLock.Lock()
	defer fixture.stateLock.Unlock()
	wantBatches := make([]int, 0, 2*providerCount/chainMaximumBatchCalls)
	for index := 0; index < 2*providerCount/chainMaximumBatchCalls; index++ {
		wantBatches = append(wantBatches, chainMaximumBatchCalls)
	}
	if fmt.Sprint(fixture.batchSizes) != fmt.Sprint(wantBatches) || fixture.singleCallCount != 2 {
		t.Fatalf("physical RPC shape batches=%v singles=%d want=%v/2", fixture.batchSizes, fixture.singleCallCount, wantBatches)
	}
	for index, blockHash := range fixture.blockHashes {
		if blockHash != common.Hash(chainBatchTestBlockHash) || !fixture.canonicalFlags[index] {
			t.Fatalf("RPC element %d block hash=%s canonical=%t", index, blockHash, fixture.canonicalFlags[index])
		}
	}
}

// Per-element errors, empty results, duplicate hotkeys, and noncanonical count
// words all fail closed at their exact decoding boundary.
func TestReleaseExactBlockBatchRejectsMalformedElements(t *testing.T) {
	tests := []struct {
		name   string
		faults chainBatchRPCFaults
		call   string
		want   string
	}{
		{name: "binding error", faults: chainBatchRPCFaults{bindingErrorClient: 2}, call: "bindings", want: "element 2"},
		{name: "binding empty", faults: chainBatchRPCFaults{bindingEmptyClient: 2}, call: "bindings", want: "element 2 is empty"},
		{name: "duplicate hotkey", faults: chainBatchRPCFaults{duplicateHotkeys: true}, call: "hotkeys", want: "duplicated at uid 1"},
		{name: "noncanonical count", faults: chainBatchRPCFaults{noncanonicalCount: true}, call: "hotkeys", want: "exceeds uint16"},
	}
	for _, test := range tests {
		fixture := newChainBatchRPCFixture(t, 3, test.faults)
		chain, closeFixture := chainBatchTestClient(t, fixture)
		var err error
		if test.call == "bindings" {
			clientIDs := make([][16]byte, 3)
			for index := range clientIDs {
				clientIDs[index][15] = byte(index)
			}
			_, err = chain.ReleaseBindingsAtHashContext(context.Background(), 123, chainBatchTestBlockHash, clientIDs, big.NewInt(7))
		} else {
			_, err = chain.MetagraphHotkeysAtHashContext(context.Background(), 123, chainBatchTestBlockHash, 521)
		}
		closeFixture()
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s error=%v want substring %q", test.name, err, test.want)
		}
	}
}

// An otherwise valid adjacent canonical hash cannot be mislabeled as the
// snapshot height merely because EIP-1898 itself selects only by hash.
func TestReleaseExactBlockBatchRejectsAdjacentHeightHash(t *testing.T) {
	fixture := newChainBatchRPCFixture(t, 1, chainBatchRPCFaults{})
	chain, closeFixture := chainBatchTestClient(t, fixture)
	defer closeFixture()
	_, err := chain.ReleaseBindingsAtHashContext(context.Background(), 123, chainBatchAdjacentBlockHash, [][16]byte{{15: 1}}, big.NewInt(7))
	if err == nil || !strings.Contains(err.Error(), "identifies block 124, not 123") {
		t.Fatalf("adjacent-height binding error=%v", err)
	}
	fixture.stateLock.Lock()
	defer fixture.stateLock.Unlock()
	if fixture.singleCallCount != 1 || len(fixture.batchSizes) != 0 {
		t.Fatalf("adjacent-height fence singles=%d batches=%v", fixture.singleCallCount, fixture.batchSizes)
	}
}

// Cancellation must interrupt a batch that has reached the provider and is
// still stalled there, rather than merely canceling before request dispatch.
func TestReleaseExactBlockBatchCancelsStalledProvider(t *testing.T) {
	batchStarted := make(chan bool, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer request.Body.Close()
		var raw json.RawMessage
		if err := json.NewDecoder(request.Body).Decode(&raw); err != nil {
			batchStarted <- false
			return
		}
		trimmed := bytes.TrimSpace(raw)
		batchStarted <- len(trimmed) != 0 && trimmed[0] == '['
		<-request.Context().Done()
	}))
	client, err := ethclient.Dial(server.URL)
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	chain := &ChainClient{
		client:       client,
		coordinator:  stabi.NewSTCoordinator(),
		contractAddr: common.HexToAddress("0x1000000000000000000000000000000000000001"),
		release:      true,
	}
	if err := chain.rememberBlockIdentity(123, chainBatchTestBlockHash); err != nil {
		client.Close()
		server.Close()
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := chain.ReleaseBindingsAtHashContext(ctx, 123, chainBatchTestBlockHash, [][16]byte{{15: 1}, {15: 2}}, big.NewInt(7))
		done <- err
	}()
	if isBatch := <-batchStarted; !isBatch {
		cancel()
		client.Close()
		server.Close()
		t.Fatal("stalled request was not a JSON-RPC batch")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		client.Close()
		server.Close()
		t.Fatalf("canceled stalled batch error=%v", err)
	}
	client.Close()
	server.Close()
}

// Pins the algorithmic fix at the release consumer: one metagraph snapshot is
// passed through the whole decision, and provider bindings use the batch API.
func TestReleaseSteeringSourceUsesOneMetagraphSnapshotAndBindingBatches(t *testing.T) {
	source, err := os.ReadFile("release_steer.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	headStart := strings.Index(text, "func (s *ReleaseSteerer) gatherHead")
	headEnd := strings.Index(text, "func (s *ReleaseSteerer) gatherPools")
	if headStart < 0 || headEnd <= headStart {
		t.Fatal("release head decision source boundaries are absent")
	}
	head := text[headStart:headEnd]
	if !strings.Contains(head, "ReleaseBindingsAtHashContext") || !strings.Contains(head, "snapshot.BlockHash") || !strings.Contains(head, "hotkeyUIDs[binding.Record.Hotkey]") || strings.Contains(head, "FindUidByHotkey") {
		t.Fatal("release head decision does not use one metagraph snapshot plus binding batches")
	}
	if strings.Count(text, "s.chain.MetagraphHotkeysAtHashContext(ctx, snapshot.BlockNumber, snapshot.BlockHash, s.cfg.Netuid)") != 1 {
		t.Fatal("release decision does not acquire exactly one shared metagraph snapshot")
	}
	poolsStart := headEnd
	poolsEnd := strings.Index(text[poolsStart:], "func (s *ReleaseSteerer) foldSettlementEpoch")
	if poolsEnd < 0 {
		t.Fatal("release pool decision source boundary is absent")
	}
	pools := text[poolsStart : poolsStart+poolsEnd]
	operatorLoop := strings.Index(pools, "for index := int64(0)")
	if operatorLoop < 0 {
		t.Fatal("release pool operator loop is absent")
	}
	loopBody := pools[operatorLoop:]
	if strings.Contains(loopBody, "ReleaseEpochStartBlockAtHashContext") || strings.Contains(loopBody, "ReleaseEpochEndBlockAtHashContext") || strings.Contains(loopBody, "BlockHashContext") {
		t.Fatal("release pool decision repeats shared epoch boundary reads per operator")
	}
}
