// Mainnet identity tests use a local RPC server and never contact a chain.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const testGenesisHash = "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

// Independently encoded synthetic block 100; observer fixtures share its height.
const testFinalizedHash = "0xac3adf52d28d2edd2e2d4e3081fe5885f38a0f860758157ccb9c7df51e572f55"

// A complete deterministic header lets identity tests exercise real SCALE
// authentication. TestIdentityFixtureHeaderMatchesIndependentCodec proves it.
func identityTestHeader() rootReceiptHeader {
	header := rootReceiptHeader{ParentHash: testGenesisHash, Number: "0x64", StateRoot: "0x" + strings.Repeat("ab", 32), ExtrinsicsRoot: "0x03170a2e7597b7b7e3d84c05391d139a62b157e78786d8c082f29dcf4c111314"}
	header.Digest.Logs = []string{}
	return header
}

// roundTripFunc gives deadline tests a synchronous transport without clocks or sockets.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (self roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return self(request)
}

// testRpcServer returns one testnet identity and optionally overloads one read.
func testRpcServer(t *testing.T, overloadedMethod string, inconsistentHeight ...bool) (*httptest.Server, func(string) int) {
	return testRpcServerWithEvm(t, "0x3b1", overloadedMethod, inconsistentHeight...)
}

func testRpcServerWithEvm(t *testing.T, evmChainId, overloadedMethod string, inconsistentHeight ...bool) (*httptest.Server, func(string) int) {
	return testRpcServerWithIdentity(t, "Bittensor", evmChainId, overloadedMethod, inconsistentHeight...)
}

// New command tests supply a synthetic chain name while sharing exact native
// header fixtures; existing tests preserve their established expectations.
func testRpcServerWithIdentity(t *testing.T, chainName, evmChainId, overloadedMethod string, inconsistentHeight ...bool) (*httptest.Server, func(string) int) {
	t.Helper()
	methodCounts := map[string]int{}
	var stateLock sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var call struct {
			JsonRpc string `json:"jsonrpc"`
			Id      int    `json:"id"`
			Method  string `json:"method"`
			Params  []any  `json:"params"`
		}
		if request.Method != http.MethodPost || json.NewDecoder(request.Body).Decode(&call) != nil || call.JsonRpc != "2.0" || call.Id != 1 {
			http.Error(writer, "unexpected request", http.StatusBadRequest)
			return
		}
		stateLock.Lock()
		methodCounts[call.Method]++
		count := methodCounts[call.Method]
		stateLock.Unlock()
		if call.Method == overloadedMethod && count == 1 {
			http.Error(writer, "temporary overload", http.StatusServiceUnavailable)
			return
		}
		results := map[string]any{
			"system_chain":           chainName,
			"chain_getBlockHash":     testGenesisHash,
			"eth_chainId":            evmChainId,
			"system_version":         "Subtensor Node test",
			"chain_getFinalizedHead": testFinalizedHash,
			"chain_getHeader":        identityTestHeader(),
			"state_getRuntimeVersion": map[string]any{
				"specName": "synthetic-runtime", "specVersion": uint64(991), "transactionVersion": uint64(1), "stateVersion": uint8(1),
			},
		}
		result, ok := results[call.Method]
		if call.Method == "chain_getBlockHash" && len(call.Params) == 1 {
			switch call.Params[0] {
			case float64(100):
				if len(inconsistentHeight) == 0 || !inconsistentHeight[0] {
					result = testFinalizedHash
				}
			}
		}
		if !ok {
			http.Error(writer, "unexpected RPC method", http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": result}); err != nil {
			t.Errorf("encode RPC reply: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	getCount := func(method string) int {
		stateLock.Lock()
		defer stateLock.Unlock()
		return methodCounts[method]
	}
	return server, getCount
}

// TestInspectRejectsTestnetRoute proves that a familiar chain name and genesis
// cannot admit the wrong EVM chain or suppress its recorded read-only snapshot.
func TestInspectRejectsTestnetRoute(t *testing.T) {
	server, _ := testRpcServer(t, "")
	var stdout, stderr bytes.Buffer
	result := runMain(context.Background(), []string{"inspect", "--rpc", server.URL, "--expected-chain", "Bittensor", "--expected-genesis", testGenesisHash, "--expected-evm-chain-id", "964", "--retry-window", "2s"}, &stdout, &stderr)
	if result != 3 || !strings.Contains(stderr.String(), "RPC identity mismatch") {
		t.Fatalf("wrong-chain inspection returned %d: %s", result, stderr.String())
	}
	var snapshot identityEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &snapshot); err != nil || snapshot.Identity.EvmChainId != 945 || snapshot.Identity.FinalizedHash != testFinalizedHash || !strings.HasPrefix(snapshot.ContentHash, "sha256:") {
		t.Fatalf("wrong-chain snapshot was not retained: %v %s", err, stdout.String())
	}
}

// TestMonitorRejectsTestnetRoute proves the long-running monitor exits before
// reporting a healthy mainnet sample from a testnet RPC route.
func TestMonitorRejectsTestnetRoute(t *testing.T) {
	server, _ := testRpcServer(t, "")
	var stdout, stderr bytes.Buffer
	result := runMain(context.Background(), []string{"monitor", "--rpc", server.URL, "--expected-chain", "Bittensor", "--expected-genesis", testGenesisHash, "--expected-evm-chain-id", "964", "--retry-window", "2s"}, &stdout, &stderr)
	if result != 3 {
		t.Fatalf("wrong-chain monitor returned %d: %s", result, stderr.String())
	}
	var event monitorEvent
	if err := json.Unmarshal(stdout.Bytes(), &event); err != nil || event.Status != "identity-mismatch" || event.Snapshot == nil || event.Snapshot.Identity.EvmChainId != 945 {
		t.Fatalf("wrong-chain monitor event: %v %s", err, stdout.String())
	}
}

// TestMonitorRequiresMainnetChainId refuses a testnet policy even before a
// healthy testnet endpoint could produce a misleading ok event.
func TestMonitorRequiresMainnetChainId(t *testing.T) {
	var stdout, stderr bytes.Buffer
	result := runMain(context.Background(), []string{"monitor", "--rpc", "http://rpc.example", "--expected-chain", "Bittensor", "--expected-genesis", testGenesisHash, "--expected-evm-chain-id", "945"}, &stdout, &stderr)
	if result != 2 || !strings.Contains(stderr.String(), "requires EVM chain ID 964") || stdout.Len() != 0 {
		t.Fatalf("testnet monitor policy was admitted: exit=%d stdout=%q stderr=%q", result, stdout.String(), stderr.String())
	}
}

// TestIdentityReadRetriesOverload proves a transient read is retried in place.
func TestIdentityReadRetriesOverload(t *testing.T) {
	server, getCount := testRpcServer(t, "eth_chainId")
	client, err := newRpcClient(server.URL, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := client.readIdentity(context.Background())
	if err != nil || identity.EvmChainId != 945 || getCount("eth_chainId") != 2 {
		t.Fatalf("transient overload was not recovered: identity=%+v calls=%d error=%v", identity, getCount("eth_chainId"), err)
	}
}

// TestMonitorStateDetectsFinalityStallAndConflict covers both liveness and
// contradictory finalized-head evidence without relying on wall-clock sleeps.
func TestMonitorStateDetectsFinalityStallAndConflict(t *testing.T) {
	state := &monitorState{}
	base := time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC)
	identity := chainIdentity{FinalizedHash: testFinalizedHash, FinalizedNumber: 100}
	if status, err := state.observe(base, identity, time.Minute); status != "ok" || err != nil {
		t.Fatalf("initial finalized head: %q %v", status, err)
	}
	identity.FinalizedHash = strings.ToUpper(identity.FinalizedHash[2:])
	identity.FinalizedHash = "0x" + identity.FinalizedHash
	if status, err := state.observe(base.Add(time.Second), identity, time.Minute); status != "ok" || err != nil {
		t.Fatalf("case-equivalent finalized hash: %q %v", status, err)
	}
	identity.FinalizedHash = testFinalizedHash
	if status, err := state.observe(base.Add(time.Minute), identity, time.Minute); status != "finality-stalled" || err != nil {
		t.Fatalf("stalled finalized head: %q %v", status, err)
	}
	identity.FinalizedNumber++
	identity.FinalizedHash = testGenesisHash
	if status, err := state.observe(base.Add(61*time.Second), identity, time.Minute); status != "ok" || err != nil {
		t.Fatalf("progressed finalized head: %q %v", status, err)
	}
	identity.FinalizedHash = testFinalizedHash
	if status, err := state.observe(base.Add(62*time.Second), identity, time.Minute); status != "finality-conflict" || err == nil {
		t.Fatalf("conflicting finalized head: %q %v", status, err)
	}
	identity.FinalizedNumber++
	identity.FinalizedHash = testGenesisHash
	if status, err := state.observe(base.Add(63*time.Second), identity, time.Minute); status != "finality-conflict" || err == nil {
		t.Fatalf("unchanged hash at higher height: %q %v", status, err)
	}
}

// TestIdentityRejectsHeaderNumberMismatch requires the announced finalized
// hash to resolve from the header's height on the same RPC route.
func TestIdentityRejectsHeaderNumberMismatch(t *testing.T) {
	server, _ := testRpcServer(t, "", true)
	client, err := newRpcClient(server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.readIdentity(context.Background())
	if err == nil || !strings.Contains(err.Error(), "does not resolve") {
		t.Fatalf("inconsistent header height was accepted: %v", err)
	}
}

// TestMonitorRejectsInconsistentRpcEvidence keeps a malformed finalized
// observation distinct from a transient RPC outage.
func TestMonitorRejectsInconsistentRpcEvidence(t *testing.T) {
	server, _ := testRpcServer(t, "", true)
	var stdout, stderr bytes.Buffer
	result := runMain(context.Background(), []string{"monitor", "--rpc", server.URL, "--expected-chain", "Bittensor", "--expected-genesis", testGenesisHash, "--expected-evm-chain-id", "964", "--retry-window", "2s"}, &stdout, &stderr)
	if result != 3 {
		t.Fatalf("inconsistent RPC evidence returned %d: %s", result, stderr.String())
	}
	var event monitorEvent
	if err := json.Unmarshal(stdout.Bytes(), &event); err != nil || event.Status != "rpc-integrity" || event.Snapshot != nil {
		t.Fatalf("inconsistent RPC evidence event: %v %s", err, stdout.String())
	}
}

// TestIdentityReadUsesOneSampleBudget proves every RPC call inherits the same
// sample deadline without depending on wall-clock timing.
func TestIdentityReadUsesOneSampleBudget(t *testing.T) {
	client, err := newRpcClient("http://rpc.example", 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	deadlines := []time.Time{}
	client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		deadline, ok := request.Context().Deadline()
		if !ok {
			return nil, fmt.Errorf("RPC read has no deadline")
		}
		deadlines = append(deadlines, deadline)
		var call struct {
			Method string `json:"method"`
			Params []any  `json:"params"`
		}
		if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
			return nil, err
		}
		results := map[string]any{
			"system_chain":            "Bittensor",
			"eth_chainId":             "0x3b1",
			"system_version":          "synthetic node",
			"chain_getFinalizedHead":  testFinalizedHash,
			"chain_getHeader":         identityTestHeader(),
			"state_getRuntimeVersion": map[string]any{"specName": "synthetic-runtime", "specVersion": 991, "transactionVersion": 1, "stateVersion": 1},
		}
		result := results[call.Method]
		if call.Method == "chain_getBlockHash" && len(call.Params) == 1 {
			result = testGenesisHash
			if call.Params[0] == float64(100) {
				result = testFinalizedHash
			}
		}
		body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "result": result})
		if err != nil {
			return nil, err
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body)), Header: make(http.Header), Request: request}, nil
	})
	if _, err := client.readIdentity(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(deadlines) != 11 {
		t.Fatalf("expected identity plus closing finality calls, got %d", len(deadlines))
	}
	for _, deadline := range deadlines[1:] {
		if !deadline.Equal(deadlines[0]) {
			t.Fatalf("identity calls used different sample deadlines: %v", deadlines)
		}
	}
}

// TestPriorFinalizedMatchesRejectsSubstitutedHistory checks ancestry at the
// last accepted height before a monitor advances to a newer finalized head.
func TestPriorFinalizedMatchesRejectsSubstitutedHistory(t *testing.T) {
	server, _ := testRpcServer(t, "")
	client, err := newRpcClient(server.URL, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	state := &monitorState{lastHash: testFinalizedHash, lastNumber: 100}
	next := chainIdentity{FinalizedHash: testGenesisHash, FinalizedNumber: 101}
	continuous, err := client.priorFinalizedMatches(context.Background(), state, next)
	if err != nil || !continuous {
		t.Fatalf("retained finalized hash was rejected: %t %v", continuous, err)
	}
	state.lastHash = testGenesisHash
	continuous, err = client.priorFinalizedMatches(context.Background(), state, next)
	if err != nil || continuous {
		t.Fatalf("substituted finalized history was accepted: %t %v", continuous, err)
	}
}

// TestRpcRejectsOversizedReply proves a valid JSON prefix cannot hide an
// oversized response behind trailing whitespace at the byte admission bound.
func TestRpcRejectsOversizedReply(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"Bittensor"}`))
		_, _ = writer.Write(bytes.Repeat([]byte{' '}, maxRpcReplyBytes))
	}))
	defer server.Close()
	client, err := newRpcClient(server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	var result string
	err = client.call(context.Background(), "system_chain", []any{}, &result)
	if err == nil || !strings.Contains(err.Error(), "exceeds 1 MiB") || result != "" {
		t.Fatalf("oversized reply was admitted: result=%q error=%v", result, err)
	}
}

// TestRpcRejectsAmbiguousJson keeps a later duplicate field from replacing
// the RPC identity or result after the bounded response is received.
func TestRpcRejectsAmbiguousJson(t *testing.T) {
	for _, wire := range []string{
		`{"jsonrpc":"2.0","id":1,"id":2,"result":"Bittensor"}`,
		`{"jsonrpc":"2.0","id":1,"result":"Bittensor","\u0072esult":"Other"}`,
		`{"jsonrpc":"2.0","id":1,"result":"Bittensor"} {"jsonrpc":"2.0","id":1,"result":"Other"}`,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			_, _ = io.WriteString(writer, wire)
		}))
		client, err := newRpcClient(server.URL, time.Second)
		if err != nil {
			server.Close()
			t.Fatal(err)
		}
		var result string
		err = client.call(context.Background(), "system_chain", []any{}, &result)
		server.Close()
		if !errors.Is(err, errRpcIntegrity) || result != "" {
			t.Errorf("ambiguous RPC document was admitted: %s result=%q error=%v", wire, result, err)
		}
	}
}

// TestRpcDoesNotFollowRedirect keeps an owned route from silently delegating
// read authority to another endpoint after launch admission.
func TestRpcDoesNotFollowRedirect(t *testing.T) {
	var redirectedCalls atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		redirectedCalls.Add(1)
	}))
	defer other.Close()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, other.URL, http.StatusFound)
	}))
	defer server.Close()
	client, err := newRpcClient(server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	var result string
	err = client.call(context.Background(), "system_chain", []any{}, &result)
	if !errors.Is(err, errRpcIntegrity) || !strings.Contains(err.Error(), "HTTP 302") || redirectedCalls.Load() != 0 {
		t.Fatalf("RPC redirect escaped owned route: result=%q calls=%d error=%v", result, redirectedCalls.Load(), err)
	}
}
