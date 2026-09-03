package main

// Deterministic regressions for the public-RPC observation batches. These
// tests count HTTP requests and validate every pinned selector without a live
// chain, reproducing the serialized snapshot bottleneck directly.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
)

// observeBatchRPC returns static ABI-shaped zero values based on each method
// selector while recording the exact HTTP batch boundaries and block tags.
type observeBatchRPC struct {
	t              *testing.T
	selectorWords  map[string]int
	httpBatchSizes []int
	blockSelectors []uint64
}

// ServeHTTP implements the minimal eth_call JSON-RPC surface used by the
// contract observation reader.
func (self *observeBatchRPC) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	self.t.Helper()
	defer request.Body.Close()
	body, err := io.ReadAll(request.Body)
	if err != nil {
		self.t.Errorf("read observation RPC body: %v", err)
		return
	}
	var calls []fleetHistoryBatchRPCRequest
	if json.Unmarshal(body, &calls) != nil || len(calls) == 0 || len(calls) > maximumEVMRPCBatchCalls {
		self.t.Errorf("invalid observation batch: %s", body)
		return
	}
	self.httpBatchSizes = append(self.httpBatchSizes, len(calls))
	responses := make([]map[string]any, len(calls))
	for index, call := range calls {
		if call.Method != "eth_call" || len(call.Params) != 2 {
			self.t.Errorf("unexpected observation call %d: %+v", index, call)
			return
		}
		var message struct {
			Data string `json:"data"`
		}
		var selector string
		if json.Unmarshal(call.Params[0], &message) != nil || json.Unmarshal(call.Params[1], &selector) != nil || len(message.Data) < 10 {
			self.t.Errorf("invalid observation call %d", index)
			return
		}
		block, decodeErr := hexutil.DecodeUint64(selector)
		if decodeErr != nil {
			self.t.Errorf("decode observation selector %q: %v", selector, decodeErr)
			return
		}
		words, ok := self.selectorWords[strings.ToLower(message.Data[:10])]
		if !ok || words < 1 {
			self.t.Errorf("unknown observation method selector %s", message.Data[:10])
			return
		}
		self.blockSelectors = append(self.blockSelectors, block)
		responses[index] = map[string]any{
			"jsonrpc": "2.0", "id": call.ID,
			"result": hexutil.Encode(make([]byte, words*32)),
		}
	}
	writer.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(writer).Encode(responses); err != nil {
		self.t.Errorf("encode observation batch: %v", err)
	}
}

// abiStaticWords returns the number of 32-byte words in the static ABI type
// surface used by the release observation methods.
func abiStaticWords(value abi.Type) (int, error) {
	switch value.T {
	case abi.TupleTy:
		words := 0
		for _, element := range value.TupleElems {
			count, err := abiStaticWords(*element)
			if err != nil {
				return 0, err
			}
			words += count
		}
		return words, nil
	case abi.ArrayTy:
		count, err := abiStaticWords(*value.Elem)
		return count * value.Size, err
	case abi.SliceTy, abi.StringTy, abi.BytesTy:
		return 0, fmt.Errorf("dynamic ABI type %s is unsupported by the observation fixture", value.String())
	default:
		return 1, nil
	}
}

// observationABIMethodSet identifies only the static release observation
// methods needed by one fixture; unrelated dynamic contract methods remain out
// of scope.
type observationABIMethodSet struct {
	Contract abi.ABI
	Methods  []string
}

// observationSelectorWords maps every requested method to its static output
// size and rejects missing methods or selector aliases which could make the
// fixture ambiguous.
func observationSelectorWords(t testing.TB, sets ...observationABIMethodSet) map[string]int {
	t.Helper()
	result := map[string]int{}
	for _, set := range sets {
		for _, name := range set.Methods {
			method, ok := set.Contract.Methods[name]
			if !ok {
				t.Fatalf("ABI method %s is unavailable", name)
			}
			words := 0
			for _, output := range method.Outputs {
				count, err := abiStaticWords(output.Type)
				if err != nil {
					t.Fatal(err)
				}
				words += count
			}
			selector := "0x" + common.Bytes2Hex(method.ID)
			if prior, ok := result[selector]; ok && prior != words {
				t.Fatalf("ABI selector %s has conflicting output sizes %d and %d", selector, prior, words)
			}
			result[selector] = words
		}
	}
	return result
}

// newObserveBatchClient exposes one deterministic observation RPC as an
// ethclient and returns the server cleanup function.
func newObserveBatchClient(t testing.TB, fixture http.Handler) (*ethclient.Client, func()) {
	t.Helper()
	server := httptest.NewServer(fixture)
	rpcClient, err := rpc.DialHTTP(server.URL)
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	client := ethclient.NewClient(rpcClient)
	cleanup := func() {
		client.Close()
		server.Close()
	}
	return client, cleanup
}

// One hundred and twenty logical reads must retain their IDs and values while
// consuming exactly three public-RPC requests of at most fifty elements.
func TestReadContractBatchAtUsesBoundedHTTPBatches(t *testing.T) {
	contract, err := abi.JSON(strings.NewReader(`[{"type":"function","name":"value","stateMutability":"view","inputs":[],"outputs":[{"name":"","type":"uint256"}]}]`))
	if err != nil {
		t.Fatal(err)
	}
	output, err := contract.Methods["value"].Outputs.Pack(big.NewInt(7))
	if err != nil {
		t.Fatal(err)
	}
	fixture := &fleetHistoryBatchRPC{t: t, contractResult: hexutil.Encode(output)}
	client, cleanup := newObserveBatchClient(t, fixture)
	defer cleanup()
	address := common.HexToAddress("0x1234567890123456789012345678901234567890")
	specs := make([]contractReadSpec, 120)
	for index := range specs {
		specs[index] = contractReadSpec{ID: fmt.Sprintf("value_%03d", index), Address: address, ContractABI: contract, Method: "value", Args: []any{}}
	}
	results, err := readContractBatchAt(context.Background(), client, 55, specs)
	if err != nil {
		t.Fatal(err)
	}
	if fixture.httpRequests != 3 || fixture.contractBatchRequests != 3 || len(results) != len(specs) {
		t.Fatalf("HTTP batches/results=%d/%d/%d, want 3/3/%d", fixture.httpRequests, fixture.contractBatchRequests, len(results), len(specs))
	}
	for index, spec := range specs {
		value, valueErr := requiredContractScalar(results, spec.ID)
		number, ok := value.(*big.Int)
		if valueErr != nil || !ok || number.Uint64() != 7 || fixture.contractSelectors[index] != 55 {
			t.Fatalf("result %s=%T(%v) selector=%d error=%v", spec.ID, value, value, fixture.contractSelectors[index], valueErr)
		}
	}
}

// Duplicate evidence identifiers must fail before the transport is used.
func TestReadContractBatchAtRejectsDuplicateIdentifiersBeforeRPC(t *testing.T) {
	contract, err := abi.JSON(strings.NewReader(`[{"type":"function","name":"value","stateMutability":"view","inputs":[],"outputs":[{"name":"","type":"uint256"}]}]`))
	if err != nil {
		t.Fatal(err)
	}
	fixture := &fleetHistoryBatchRPC{t: t}
	client, cleanup := newObserveBatchClient(t, fixture)
	defer cleanup()
	address := common.HexToAddress("0x1234567890123456789012345678901234567890")
	specs := []contractReadSpec{
		{ID: "same", Address: address, ContractABI: contract, Method: "value", Args: []any{}},
		{ID: "same", Address: address, ContractABI: contract, Method: "value", Args: []any{}},
	}
	if _, err := readContractBatchAt(context.Background(), client, 55, specs); err == nil || fixture.httpRequests != 0 {
		t.Fatalf("duplicate identifier error=%v HTTP requests=%d", err, fixture.httpRequests)
	}
}

// The retained two-operator, eleven-epoch surface is 96 exact calls. It must
// use two HTTP batches and preserve the finalized block tag on all 96.
func TestInspectOperatorEpochsBatchesEntireRetentionWindow(t *testing.T) {
	coordinator, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		t.Fatal(err)
	}
	vault, err := abi.JSON(strings.NewReader(SettlementVaultABI))
	if err != nil {
		t.Fatal(err)
	}
	fixture := &observeBatchRPC{t: t, selectorWords: observationSelectorWords(t,
		observationABIMethodSet{Contract: coordinator, Methods: []string{"operatorAt", "cumulativeConviction", "epochDeposits", "epochConvictionAdded", "rootCommitments"}},
		observationABIMethodSet{Contract: vault, Methods: []string{"pools", "carry", "entitlement"}},
	)}
	client, cleanup := newObserveBatchClient(t, fixture)
	defer cleanup()
	deployment := &ContractDeployment{
		CoordinatorProxy: common.HexToAddress("0x1234567890123456789012345678901234567890"),
		SettlementVault:  common.HexToAddress("0x2234567890123456789012345678901234567890"),
	}
	policy := PolicyView{ClaimTTLEpochs: 8, ClaimGraceEpochs: 1}
	operators, epochs, err := inspectOperatorEpochs(context.Background(), client, deployment, coordinator, vault, 900, 100, []uint64{1, 2}, policy)
	if err != nil {
		t.Fatal(err)
	}
	if len(operators) != 2 || len(epochs) != 11 || epochs[0].Epoch != 90 || epochs[10].Epoch != 100 {
		t.Fatalf("operators/epochs=%d/%d range=%d..%d", len(operators), len(epochs), epochs[0].Epoch, epochs[len(epochs)-1].Epoch)
	}
	if fmt.Sprint(fixture.httpBatchSizes) != "[50 46]" || len(fixture.blockSelectors) != 96 {
		t.Fatalf("batch sizes/selectors=%v/%d, want [50 46]/96", fixture.httpBatchSizes, len(fixture.blockSelectors))
	}
	for index, block := range fixture.blockSelectors {
		if block != 900 {
			t.Fatalf("selector %d=%d, want 900", index, block)
		}
	}
}

// runtimeCodeBatchRPC returns two exact code blobs and one ERC-1967 slot from
// a mixed-method batch while recording its finalized block selectors.
type runtimeCodeBatchRPC struct {
	t              *testing.T
	codes          map[string]string
	storage        string
	httpBatchSizes []int
	blockSelectors []uint64
}

// ServeHTTP implements eth_getCode and eth_getStorageAt for one mixed batch.
func (self *runtimeCodeBatchRPC) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	self.t.Helper()
	defer request.Body.Close()
	body, err := io.ReadAll(request.Body)
	if err != nil {
		self.t.Errorf("read runtime RPC body: %v", err)
		return
	}
	var calls []fleetHistoryBatchRPCRequest
	if json.Unmarshal(body, &calls) != nil || len(calls) == 0 || len(calls) > maximumEVMRPCBatchCalls {
		self.t.Errorf("invalid runtime batch: %s", body)
		return
	}
	self.httpBatchSizes = append(self.httpBatchSizes, len(calls))
	responses := make([]map[string]any, len(calls))
	for index, call := range calls {
		result := ""
		selectorIndex := 1
		switch call.Method {
		case "eth_getCode":
			var address string
			if len(call.Params) != 2 || json.Unmarshal(call.Params[0], &address) != nil {
				self.t.Errorf("invalid code request %d", index)
				return
			}
			result = self.codes[strings.ToLower(address)]
		case "eth_getStorageAt":
			if len(call.Params) != 3 {
				self.t.Errorf("invalid storage request %d", index)
				return
			}
			selectorIndex = 2
			result = self.storage
		default:
			self.t.Errorf("unexpected runtime method %s", call.Method)
			return
		}
		var selector string
		if result == "" || json.Unmarshal(call.Params[selectorIndex], &selector) != nil {
			self.t.Errorf("incomplete runtime response for call %d", index)
			return
		}
		block, decodeErr := hexutil.DecodeUint64(selector)
		if decodeErr != nil {
			self.t.Errorf("decode runtime selector %q: %v", selector, decodeErr)
			return
		}
		self.blockSelectors = append(self.blockSelectors, block)
		responses[index] = map[string]any{"jsonrpc": "2.0", "id": call.ID, "result": result}
	}
	writer.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(writer).Encode(responses); err != nil {
		self.t.Errorf("encode runtime batch: %v", err)
	}
}

// Code hashes and the implementation slot must share one pinned request and
// still compare against their independently release-locked expected values.
func TestInspectRuntimeCodeAtUsesOnePinnedMixedBatch(t *testing.T) {
	first := common.HexToAddress("0x1234567890123456789012345678901234567890")
	proxy := common.HexToAddress("0x2234567890123456789012345678901234567890")
	upgradeAddress := common.HexToAddress("0x3234567890123456789012345678901234567890")
	firstCode := []byte{0x60, 0x01}
	upgradeCode := []byte{0x60, 0x02}
	slot := common.LeftPadBytes(upgradeAddress.Bytes(), 32)
	fixture := &runtimeCodeBatchRPC{
		t: t,
		codes: map[string]string{
			strings.ToLower(first.Hex()):          hexutil.Encode(firstCode),
			strings.ToLower(upgradeAddress.Hex()): hexutil.Encode(upgradeCode),
		},
		storage: hexutil.Encode(slot),
	}
	client, cleanup := newObserveBatchClient(t, fixture)
	defer cleanup()
	deployment := &ContractDeployment{
		CoordinatorProxy: proxy,
		RuntimeHashes: map[string]string{
			first.Hex(): crypto.Keccak256Hash(firstCode).Hex(),
		},
	}
	upgrade := CoordinatorUpgrade{Implementation: upgradeAddress, RuntimeCodeHash: crypto.Keccak256Hash(upgradeCode).Hex()}
	hashes, matches, err := inspectRuntimeCodeAt(context.Background(), client, deployment, upgrade, []common.Address{first, upgradeAddress}, 77)
	if err != nil {
		t.Fatal(err)
	}
	if !matches || len(hashes) != 2 || fmt.Sprint(fixture.httpBatchSizes) != "[3]" || len(fixture.blockSelectors) != 3 {
		t.Fatalf("matches/hashes/batches/selectors=%t/%v/%v/%v", matches, hashes, fixture.httpBatchSizes, fixture.blockSelectors)
	}
	for index, block := range fixture.blockSelectors {
		if block != 77 {
			t.Fatalf("runtime selector %d=%d, want 77", index, block)
		}
	}
}
