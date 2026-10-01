// Causal mapping tests use independent SCALE/RLP fixture encoders and only
// synthetic identities. Native and EVM heights deliberately differ.
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	substrate "github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	"golang.org/x/crypto/blake2b"
)

// Immutable fixture state is installed before reads; the synchronous transport
// and HTTP command adapter share counters under the same lock.
type finalizedMappingFixture struct {
	stateLock         sync.Mutex
	counts            map[string]int
	nativeHeader      rootReceiptHeader
	nativeHash        string
	evmHeader         *types.Header
	evmHash           string
	rawEvmHeader      string
	renderedEvmBlock  any
	transactionHashes []string
	runtimeCode       []byte
	runtimeMetadata   []byte
	fault             func(string, []any, int) (any, bool)
	rawRequests       []string
}

// Typed fixture errors preserve JSON-RPC codes through the production reader.
type mappingFixtureRpcError struct {
	code int
}

// Generates the exact SCALE Vec<Hash> with the external codec, avoiding the
// parser's compact integer implementation when constructing expectations.
func mappingTestDigest(t *testing.T, variant byte, blockHash string, transactionHashes []string) string {
	t.Helper()
	hash, err := hex.DecodeString(blockHash[2:])
	if err != nil {
		t.Fatal(err)
	}
	payload := append([]byte{variant}, hash...)
	if variant == 1 {
		values := make([]substrate.Hash, len(transactionHashes))
		for index, encoded := range transactionHashes {
			value, err := substrate.NewHashFromHexString(encoded)
			if err != nil {
				t.Fatal(err)
			}
			values[index] = value
		}
		encoded, err := codec.Encode(values)
		if err != nil {
			t.Fatal(err)
		}
		payload = append(payload, encoded...)
	}
	return mappingTestPayloadDigest(t, payload)
}

// The outer consensus engine and bounded Vec<u8> use their wire encodings.
func mappingTestPayloadDigest(t *testing.T, payload []byte) string {
	t.Helper()
	encoded, err := codec.Encode(substrate.Bytes(payload))
	if err != nil {
		t.Fatal(err)
	}
	return "0x" + hex.EncodeToString(append([]byte{4, 'f', 'r', 'o', 'n'}, encoded...))
}

// Recomputes the native commitment independently so malformed Frontier payload
// tests still have a genuine header hash and reach the mapping-specific parser.
func (self *finalizedMappingFixture) replaceNativeLogs(t *testing.T, logs []string) {
	t.Helper()
	parent, _ := substrate.NewHashFromHexString(testGenesisHash)
	state, _ := substrate.NewHashFromHexString("0x" + strings.Repeat("ab", 32))
	body, _ := substrate.NewHashFromHexString(identityTestHeader().ExtrinsicsRoot)
	header := substrate.Header{ParentHash: parent, Number: 100, StateRoot: state, ExtrinsicsRoot: body, Digest: substrate.Digest{}}
	raw, err := codec.Encode(header)
	if err != nil {
		t.Fatal(err)
	}
	count, err := codec.Encode(substrate.NewUCompactFromUInt(uint64(len(logs))))
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw[:len(raw)-1], count...)
	for _, encoded := range logs {
		log, err := hex.DecodeString(encoded[2:])
		if err != nil {
			t.Fatal(err)
		}
		raw = append(raw, log...)
	}
	digest := blake2b.Sum256(raw)
	self.nativeHash = "0x" + hex.EncodeToString(digest[:])
	self.nativeHeader = rootReceiptHeader{ParentHash: parent.Hex(), Number: "0x64", StateRoot: state.Hex(), ExtrinsicsRoot: body.Hex()}
	self.nativeHeader.Digest.Logs = logs
}

// RLP fixtures retain millisecond timestamps with a nonzero remainder, which
// the rendered Ethereum RPC view cannot reconstruct from its seconds field.
func newFinalizedMappingFixture(t *testing.T, variant byte, transactionCount int) (*rpcClient, *finalizedMappingFixture) {
	t.Helper()
	fixture := &finalizedMappingFixture{
		counts: map[string]int{}, transactionHashes: make([]string, transactionCount),
		runtimeCode: []byte{0, 97, 115, 109, 1, 0, 0, 0}, runtimeMetadata: []byte{109, 101, 116, 97, 255, 0},
	}
	for index := range fixture.transactionHashes {
		fixture.transactionHashes[index] = crypto.Keccak256Hash([]byte(fmt.Sprintf("synthetic transaction %d", index))).Hex()
	}
	fixture.evmHeader = &types.Header{
		ParentHash: common.HexToHash("0x" + strings.Repeat("ba", 32)), UncleHash: types.EmptyUncleHash,
		Root: common.HexToHash("0x" + strings.Repeat("cb", 32)), TxHash: types.EmptyTxsHash, ReceiptHash: types.EmptyReceiptsHash,
		Difficulty: big.NewInt(0), Number: big.NewInt(37), GasLimit: 75000000, Time: 1700000000001, Extra: []byte{},
	}
	raw, err := rlp.EncodeToBytes(fixture.evmHeader)
	if err != nil {
		t.Fatal(err)
	}
	fixture.rawEvmHeader = "0x" + hex.EncodeToString(raw)
	fixture.evmHash = fixture.evmHeader.Hash().Hex()
	fixture.replaceNativeLogs(t, []string{mappingTestDigest(t, variant, fixture.evmHash, fixture.transactionHashes)})
	client, err := newRpcClient("http://rpc.example", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	client.httpClient.Transport = roundTripFunc(fixture.roundTrip)
	return client, fixture
}

// Responds only to observation methods; all candidate raw-header lookups must
// carry the digest hash and requireCanonical, never a guessed native height.
func (self *finalizedMappingFixture) roundTrip(request *http.Request) (*http.Response, error) {
	var call struct {
		Method string `json:"method"`
		Params []any  `json:"params"`
	}
	if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
		return nil, err
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	self.counts[call.Method]++
	count := self.counts[call.Method]
	var result any
	switch call.Method {
	case "system_chain":
		result = "synthetic-chain"
	case "system_version":
		result = "synthetic-node"
	case "eth_chainId":
		result = "0x3c4"
	case "chain_getFinalizedHead":
		result = self.nativeHash
	case "chain_getHeader":
		if len(call.Params) != 1 || call.Params[0] != self.nativeHash {
			return nil, errors.New("native header was not pinned")
		}
		result = self.nativeHeader
	case "chain_getBlockHash":
		if len(call.Params) != 1 {
			return nil, errors.New("unexpected native lookup")
		}
		switch call.Params[0] {
		case float64(0):
			result = testGenesisHash
		case float64(100):
			result = self.nativeHash
		default:
			return nil, errors.New("native lookup guessed EVM height")
		}
	case "state_getRuntimeVersion":
		if len(call.Params) != 1 || call.Params[0] != self.nativeHash {
			return nil, errors.New("runtime version was not pinned")
		}
		result = map[string]any{"specName": "synthetic-runtime", "specVersion": 991, "transactionVersion": 1, "stateVersion": 1}
	case "state_getStorageHash", "state_getStorage":
		if len(call.Params) != 2 || call.Params[0] != runtimeCodeStorageKey || call.Params[1] != self.nativeHash {
			return nil, errors.New("runtime code read was not pinned")
		}
		if call.Method == "state_getStorage" {
			result = "0x" + hex.EncodeToString(self.runtimeCode)
		} else {
			digest := blake2b.Sum256(self.runtimeCode)
			result = "0x" + hex.EncodeToString(digest[:])
		}
	case "state_getMetadata":
		if len(call.Params) != 1 || call.Params[0] != self.nativeHash {
			return nil, errors.New("runtime metadata read was not pinned")
		}
		result = "0x" + hex.EncodeToString(self.runtimeMetadata)
	case "debug_getRawHeader":
		if len(call.Params) != 1 {
			return nil, errors.New("raw header selector count differs")
		}
		selector, ok := call.Params[0].(map[string]any)
		if !ok || len(selector) != 2 || selector["blockHash"] != self.evmHash || selector["requireCanonical"] != true {
			return nil, errors.New("raw header was not selected by canonical digest hash")
		}
		self.rawRequests = append(self.rawRequests, selector["blockHash"].(string))
		result = self.rawEvmHeader
	case "eth_getBlockByNumber":
		if len(call.Params) != 2 || call.Params[0] != "0x25" || call.Params[1] != false {
			return nil, errors.New("canonical EVM lookup did not use the raw EVM height")
		}
		result = map[string]any{"hash": self.evmHash, "number": "0x25", "transactions": self.transactionHashes, "timestamp": "0x6553f100", "baseFeePerGas": "0x1"}
	case "eth_getBlockByHash":
		if len(call.Params) != 2 || call.Params[0] != self.evmHash || call.Params[1] != false {
			return nil, errors.New("public EVM header was not selected by exact digest hash")
		}
		result = self.renderedEvmBlock
		if result == nil {
			result = mappingFixtureRpcError{code: -32601}
		}
	default:
		return nil, fmt.Errorf("unexpected or mutating RPC method %s", call.Method)
	}
	if self.fault != nil {
		if replacement, replace := self.fault(call.Method, call.Params, count); replace {
			result = replacement
		}
	}
	reply := map[string]any{"jsonrpc": "2.0", "id": 1, "result": result}
	if rpcErr, ok := result.(mappingFixtureRpcError); ok {
		delete(reply, "result")
		reply["error"] = map[string]any{"code": rpcErr.code, "message": "synthetic capability unavailable"}
	}
	raw, err := json.Marshal(reply)
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(raw)), Header: make(http.Header), Request: request}, nil
}

// Both reviewed variants work without any equality between the two heights.
func TestFinalizedMappingAuthenticatesUnequalHeightsAndExactRlp(t *testing.T) {
	for _, variant := range []byte{1, 3} {
		client, fixture := newFinalizedMappingFixture(t, variant, 4)
		mapping, err := client.readFinalizedMapping(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if mapping.Identity.FinalizedNumber != 100 || mapping.EvmHeader.Number != 37 || mapping.EvmHeader.Hash != fixture.evmHash || mapping.EvmCanonicalHash != fixture.evmHash || mapping.EvmHeader.HeaderRlp != fixture.rawEvmHeader || mapping.Admission != "unapproved_observation" || mapping.RuntimeSourceProven || fixture.counts["eth_getBlockByNumber"] != 2 {
			t.Fatalf("mapping lost its independently authenticated scope: %+v", mapping)
		}
		if variant == 1 && !reflect.DeepEqual(mapping.PostLog.TransactionHashes, fixture.transactionHashes) || variant == 3 && mapping.PostLog.TransactionHashes != nil {
			t.Fatalf("variant %d transaction authority differs: %+v", variant, mapping.PostLog)
		}
	}
}

// Compact lengths change at multiple boundaries; the complete ordered vector
// must survive, not just the first digest substring seen in an empty block.
func TestFinalizedMappingParsesVariableFrontierVectors(t *testing.T) {
	for _, count := range []int{0, 1, 3, 4, 63, 64, 300} {
		client, fixture := newFinalizedMappingFixture(t, 1, count)
		mapping, err := client.readFinalizedMapping(context.Background(), nil)
		if err != nil || !reflect.DeepEqual(mapping.PostLog.TransactionHashes, fixture.transactionHashes) {
			t.Errorf("transaction count %d: %+v %v", count, mapping.PostLog, err)
		}
	}
}

// Header bytes can use either hex case on the second read too; normalization
// preserves actual bytes and does not weaken the native commitment.
func TestFinalizedMappingNormalizesRetainedHeaderHex(t *testing.T) {
	client, fixture := newFinalizedMappingFixture(t, 1, 1)
	fixture.fault = func(method string, _ []any, count int) (any, bool) {
		if method != "chain_getHeader" || count != 2 {
			return nil, false
		}
		header := fixture.nativeHeader
		header.ParentHash = "0x" + strings.ToUpper(header.ParentHash[2:])
		header.StateRoot = "0x" + strings.ToUpper(header.StateRoot[2:])
		header.ExtrinsicsRoot = "0x" + strings.ToUpper(header.ExtrinsicsRoot[2:])
		header.Digest.Logs = []string{"0x" + strings.ToUpper(header.Digest.Logs[0][2:])}
		return header, true
	}
	if mapping, err := client.readFinalizedMapping(context.Background(), nil); err != nil || mapping.EvmHeader.Hash != fixture.evmHash {
		t.Fatalf("case-equivalent retained header rejected: %+v %v", mapping, err)
	}
}

// Removing all Frontier logs or changing the variant is a capability gap;
// malformed, duplicate, or trailing committed data is an integrity failure.
func TestFinalizedMappingRejectsUnsupportedAndMalformedDigests(t *testing.T) {
	cases := []struct {
		name string
		logs func(*testing.T, *finalizedMappingFixture) []string
		want error
	}{
		{name: "missing", logs: func(_ *testing.T, _ *finalizedMappingFixture) []string { return []string{} }, want: errFinalizedMappingUnavailable},
		{name: "unknown variant", logs: func(t *testing.T, f *finalizedMappingFixture) []string {
			return []string{mappingTestDigest(t, 2, f.evmHash, nil)}
		}, want: errFinalizedMappingUnavailable},
		{name: "duplicate", logs: func(_ *testing.T, f *finalizedMappingFixture) []string {
			return []string{f.nativeHeader.Digest.Logs[0], f.nativeHeader.Digest.Logs[0]}
		}, want: errRpcIntegrity},
		{name: "trailing", logs: func(t *testing.T, f *finalizedMappingFixture) []string {
			hash, _ := hex.DecodeString(f.evmHash[2:])
			return []string{mappingTestPayloadDigest(t, append(append([]byte{3}, hash...), 0))}
		}, want: errRpcIntegrity},
		{name: "truncated vector", logs: func(t *testing.T, f *finalizedMappingFixture) []string {
			hash, _ := hex.DecodeString(f.evmHash[2:])
			return []string{mappingTestPayloadDigest(t, append(append([]byte{1}, hash...), 4))}
		}, want: errRpcIntegrity},
	}
	for _, testCase := range cases {
		client, fixture := newFinalizedMappingFixture(t, 1, 0)
		fixture.replaceNativeLogs(t, testCase.logs(t, fixture))
		mapping, err := client.readFinalizedMapping(context.Background(), nil)
		if !errors.Is(err, testCase.want) || mapping.Schema != "" || fixture.counts["debug_getRawHeader"] != 0 {
			t.Errorf("%s was not refused before EVM reads: schema=%q err=%v", testCase.name, mapping.Schema, err)
		}
	}
}

// If neither exact header representation is available, no guessed height or
// empty header may become evidence. Null raw reads do not attempt a fallback.
func TestFinalizedMappingUnavailableHeadersHaveNoGuessedFallback(t *testing.T) {
	for _, reply := range []any{nil, mappingFixtureRpcError{code: -32601}, mappingFixtureRpcError{code: -32602}} {
		client, fixture := newFinalizedMappingFixture(t, 1, 0)
		fixture.fault = func(method string, _ []any, _ int) (any, bool) { return reply, method == "debug_getRawHeader" }
		mapping, err := client.readFinalizedMapping(context.Background(), nil)
		wantPublicReads := 1
		if reply == nil {
			wantPublicReads = 0
		}
		if !errors.Is(err, errFinalizedMappingUnavailable) || mapping.Schema != "" || fixture.counts["debug_getRawHeader"] != 1 || fixture.counts["eth_getBlockByNumber"] != 0 || fixture.counts["eth_getBlockByHash"] != wantPublicReads {
			t.Errorf("unavailable raw header became evidence: schema=%q counts=%v err=%v", mapping.Schema, fixture.counts, err)
		}
	}
}

// A returned RLP header is not proof until its exact bytes match the native
// commitment, even if the RPC continues to report a matching display hash.
func TestFinalizedMappingRejectsWrongRawHeader(t *testing.T) {
	client, fixture := newFinalizedMappingFixture(t, 1, 0)
	wrong := *fixture.evmHeader
	wrong.Time++
	raw, err := rlp.EncodeToBytes(&wrong)
	if err != nil {
		t.Fatal(err)
	}
	fixture.fault = func(method string, _ []any, _ int) (any, bool) {
		return "0x" + hex.EncodeToString(raw), method == "debug_getRawHeader"
	}
	mapping, err := client.readFinalizedMapping(context.Background(), nil)
	if !errors.Is(err, errRpcIntegrity) || mapping.Schema != "" || fixture.counts["eth_getBlockByNumber"] != 0 {
		t.Fatalf("wrong RLP header accepted: schema=%q err=%v", mapping.Schema, err)
	}
}

// Identity, number, ordered transaction vector and both canonical EVM reads
// must agree. A same-genesis proxy/index change after the first read is covered.
func TestFinalizedMappingRejectsChangedCanonicalEvidence(t *testing.T) {
	cases := []struct {
		name  string
		fault func(*finalizedMappingFixture, string, []any, int) (any, bool)
	}{
		{name: "final EVM hash", fault: func(f *finalizedMappingFixture, method string, _ []any, count int) (any, bool) {
			return map[string]any{"hash": testGenesisHash, "number": "0x25", "transactions": f.transactionHashes}, method == "eth_getBlockByNumber" && count == 2
		}},
		{name: "EVM number", fault: func(f *finalizedMappingFixture, method string, _ []any, _ int) (any, bool) {
			return map[string]any{"hash": f.evmHash, "number": "0x26", "transactions": f.transactionHashes}, method == "eth_getBlockByNumber"
		}},
		{name: "transaction order", fault: func(f *finalizedMappingFixture, method string, _ []any, _ int) (any, bool) {
			return map[string]any{"hash": f.evmHash, "number": "0x25", "transactions": []string{f.transactionHashes[1], f.transactionHashes[0]}}, method == "eth_getBlockByNumber"
		}},
		{name: "native canonical", fault: func(_ *finalizedMappingFixture, method string, params []any, count int) (any, bool) {
			return testGenesisHash, method == "chain_getBlockHash" && count > 2 && params[0] == float64(100)
		}},
		{name: "genesis", fault: func(f *finalizedMappingFixture, method string, params []any, count int) (any, bool) {
			return f.nativeHash, method == "chain_getBlockHash" && count > 2 && params[0] == float64(0)
		}},
		{name: "EVM route", fault: func(_ *finalizedMappingFixture, method string, _ []any, count int) (any, bool) {
			return "0x3b1", method == "eth_chainId" && count == 2
		}},
	}
	for _, testCase := range cases {
		client, fixture := newFinalizedMappingFixture(t, 1, 2)
		fixture.fault = func(method string, params []any, count int) (any, bool) {
			return testCase.fault(fixture, method, params, count)
		}
		mapping, err := client.readFinalizedMapping(context.Background(), nil)
		if !errors.Is(err, errRpcIntegrity) || mapping.Schema != "" {
			t.Errorf("%s accepted: schema=%q err=%v", testCase.name, mapping.Schema, err)
		}
	}
}

// An independently approved network mismatch stops before the optional EVM
// method, including when a familiar native chain name is shared by testnet.
func TestFinalizedMappingRejectsWrongApprovedNetworkBeforeEvmReads(t *testing.T) {
	client, fixture := newFinalizedMappingFixture(t, 1, 0)
	expected := &identityExpectation{NativeChain: "synthetic-chain", GenesisHash: testFinalizedHash, EvmChainId: mainnetEvmChainId}
	mapping, err := client.readFinalizedMapping(context.Background(), expected)
	if !errors.Is(err, errRpcIdentityMismatch) || mapping.Schema != "" || fixture.counts["debug_getRawHeader"] != 0 {
		t.Fatalf("wrong approved identity accepted: schema=%q err=%v", mapping.Schema, err)
	}
}

// The new method remains unavailable to ordinary identity/storage clients and
// no mutating method can enter the specialized observation profile either.
func TestFinalizedMappingRpcMethodsAreScopedAndBounded(t *testing.T) {
	client, fixture := newFinalizedMappingFixture(t, 1, 0)
	var result json.RawMessage
	if err := client.call(context.Background(), "debug_getRawHeader", []any{}, &result); err == nil || len(fixture.counts) != 0 {
		t.Fatal("raw-header method leaked into the ordinary profile")
	}
	if err := client.callFinalizedMappingRead(context.Background(), "eth_sendRawTransaction", []any{}, &result); err == nil || len(fixture.counts) != 0 {
		t.Fatal("mapping profile admitted a mutating method")
	}
	fixture.fault = func(method string, _ []any, _ int) (any, bool) {
		return "0x" + strings.Repeat("ab", maxRpcReplyBytes), method == "debug_getRawHeader"
	}
	mapping, err := client.readFinalizedMapping(context.Background(), nil)
	if !errors.Is(err, errRpcIntegrity) || mapping.Schema != "" {
		t.Fatalf("oversized raw-header reply accepted: schema=%q err=%v", mapping.Schema, err)
	}
}

// Explicit cancellation on the last successful read cannot publish a partial
// observation, even though all earlier evidence has already been collected.
func TestFinalizedMappingCancellationReturnsNoPartialProof(t *testing.T) {
	client, fixture := newFinalizedMappingFixture(t, 1, 0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fixture.fault = func(method string, _ []any, count int) (any, bool) {
		if method == "eth_getBlockByNumber" && count == 2 {
			cancel()
		}
		return nil, false
	}
	mapping, err := client.readFinalizedMapping(ctx, nil)
	if !errors.Is(err, context.Canceled) || mapping.Schema != "" {
		t.Fatalf("canceled read published partial proof: schema=%q err=%v", mapping.Schema, err)
	}
}

// The CLI retains raw audit bytes and a reproducible envelope, while missing
// capability yields a distinct exit and no partial stdout artifact.
func TestFinalizedMappingCommandRetainsProofAndUnavailableExit(t *testing.T) {
	for _, unavailable := range []bool{false, true} {
		_, fixture := newFinalizedMappingFixture(t, 1, 1)
		if unavailable {
			fixture.fault = func(method string, _ []any, _ int) (any, bool) { return nil, method == "debug_getRawHeader" }
		}
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			response, err := fixture.roundTrip(request)
			if err != nil {
				t.Error(err)
				http.Error(writer, "synthetic request failed", http.StatusBadRequest)
				return
			}
			defer response.Body.Close()
			writer.WriteHeader(response.StatusCode)
			if _, err := io.Copy(writer, response.Body); err != nil {
				t.Error(err)
			}
		}))
		var stdout, stderr bytes.Buffer
		code := runMain(context.Background(), []string{"finalized-mapping", "--rpc", server.URL}, &stdout, &stderr)
		server.Close()
		if unavailable {
			if code != 4 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "mapping is unavailable") {
				t.Fatalf("unavailable CLI result: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
			}
			continue
		}
		var envelope finalizedMappingEnvelope
		if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil || code != 0 || envelope.Mapping.EvmHeader.HeaderRlp != fixture.rawEvmHeader || envelope.Mapping.RuntimeVersion.SpecName != "synthetic-runtime" {
			t.Fatalf("CLI proof missing: code=%d err=%v stderr=%s", code, err, stderr.String())
		}
		resealed, err := sealFinalizedMapping(envelope.Mapping)
		if err != nil || resealed.ContentHash != envelope.ContentHash {
			t.Fatal("mapping envelope did not reproduce")
		}
	}
}
