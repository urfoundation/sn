// Identity regressions exercise the public read path with deterministic RPC
// replies; a matching height lookup must not authenticate fabricated headers.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

// The synchronous transport owns one test's reply values and counters. It
// deliberately confirms the announced hash at every nonzero height, reproducing
// the old number-only check without trusting that lookup as header evidence.
func identityIntegrityClient(t *testing.T, header any, finalizedHash string, runtime any) (*rpcClient, map[string]int) {
	t.Helper()
	client, err := newRpcClient("http://rpc.example", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	methodCounts := map[string]int{}
	client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var call struct {
			Method string `json:"method"`
			Params []any  `json:"params"`
		}
		if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
			return nil, err
		}
		methodCounts[call.Method]++
		results := map[string]any{
			"system_chain":            "synthetic-chain",
			"eth_chainId":             "0x3c4",
			"system_version":          "synthetic-node",
			"chain_getFinalizedHead":  finalizedHash,
			"chain_getHeader":         header,
			"state_getRuntimeVersion": runtime,
			"chain_getBlockHash":      finalizedHash,
		}
		result, exists := results[call.Method]
		if !exists {
			return nil, fmt.Errorf("unexpected method %s", call.Method)
		}
		if call.Method == "chain_getBlockHash" && len(call.Params) == 1 && call.Params[0] == float64(0) {
			result = testGenesisHash
		}
		body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "result": result})
		if err != nil {
			return nil, err
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body)), Header: make(http.Header), Request: request}, nil
	})
	return client, methodCounts
}

// The shared fixture is bound to independently encoded header bytes rather
// than a hard-coded digest that the tested authentication path invented.
func TestIdentityFixtureHeaderMatchesIndependentCodec(t *testing.T) {
	header, hash := rootReceiptHeaderFixture(t, testGenesisHash, 100, nil, false)
	if !reflect.DeepEqual(header, identityTestHeader()) || hash != testFinalizedHash {
		t.Fatalf("identity header fixture differs from independent codec: %+v %s", header, hash)
	}
}

// Every omitted commitment field is rejected before a runtime read or a
// redundant same-height confirmation can make an incomplete header look valid.
func TestIdentityRejectsIncompleteFinalizedHeaders(t *testing.T) {
	runtime := json.RawMessage(`{"specName":"synthetic-runtime","specVersion":991,"transactionVersion":1,"stateVersion":1}`)
	for _, missing := range []string{"parentHash", "number", "stateRoot", "extrinsicsRoot", "digest", "logs"} {
		encoded, err := json.Marshal(identityTestHeader())
		if err != nil {
			t.Fatal(err)
		}
		var header map[string]any
		if err := json.Unmarshal(encoded, &header); err != nil {
			t.Fatal(err)
		}
		if missing == "logs" {
			header["digest"] = map[string]any{}
		} else {
			delete(header, missing)
		}
		client, counts := identityIntegrityClient(t, header, testFinalizedHash, runtime)
		identity, err := client.readIdentity(context.Background())
		if !errors.Is(err, errRpcIntegrity) || identity.FinalizedHash != "" || counts["state_getRuntimeVersion"] != 0 || counts["chain_getHeader"] != 1 {
			t.Errorf("missing %s was not a terminal header integrity failure: identity=%+v calls=%v err=%v", missing, identity, counts, err)
		}
	}
}

// Each complete but forged commitment must change the full SCALE hash even
// when the RPC repeats the announced hash at the supplied height.
func TestIdentityRejectsForgedFinalizedHeaders(t *testing.T) {
	runtime := json.RawMessage(`{"specName":"synthetic-runtime","specVersion":991,"transactionVersion":1,"stateVersion":1}`)
	mutations := []struct {
		name   string
		mutate func(*rootReceiptHeader)
	}{
		{name: "parent", mutate: func(header *rootReceiptHeader) { header.ParentHash = "0x" + strings.Repeat("bc", 32) }},
		{name: "height", mutate: func(header *rootReceiptHeader) { header.Number = "0x65" }},
		{name: "state", mutate: func(header *rootReceiptHeader) { header.StateRoot = "0x" + strings.Repeat("cd", 32) }},
		{name: "body", mutate: func(header *rootReceiptHeader) { header.ExtrinsicsRoot = "0x" + strings.Repeat("de", 32) }},
		{name: "digest", mutate: func(header *rootReceiptHeader) { header.Digest.Logs = []string{"0x08"} }},
	}
	for _, mutation := range mutations {
		header := identityTestHeader()
		mutation.mutate(&header)
		client, counts := identityIntegrityClient(t, header, testFinalizedHash, runtime)
		identity, err := client.readIdentity(context.Background())
		if !errors.Is(err, errRpcIntegrity) || identity.FinalizedHash != "" || counts["state_getRuntimeVersion"] != 0 {
			t.Errorf("forged %s was accepted: identity=%+v calls=%v err=%v", mutation.name, identity, counts, err)
		}
	}
}

// A real upgrade digest and equivalent hex casing survive the stricter check;
// the fix must not recreate the old GSRPC digest8 compatibility failure.
func TestIdentityAuthenticatesUpgradeHeaderAndEquivalentHex(t *testing.T) {
	header, hash := rootReceiptHeaderFixture(t, testGenesisHash, 100, nil, true)
	header.ParentHash = "0x" + strings.ToUpper(header.ParentHash[2:])
	header.StateRoot = "0x" + strings.ToUpper(header.StateRoot[2:])
	header.ExtrinsicsRoot = "0x" + strings.ToUpper(header.ExtrinsicsRoot[2:])
	runtime := json.RawMessage(`{"specName":"synthetic-runtime","specVersion":991,"transactionVersion":1,"stateVersion":1,"systemVersion":1}`)
	client, _ := identityIntegrityClient(t, header, "0x"+strings.ToUpper(hash[2:]), runtime)
	identity, err := client.readIdentity(context.Background())
	if err != nil || identity.FinalizedHash != hash || identity.FinalizedNumber != 100 || identity.RuntimeSpec != 991 {
		t.Fatalf("valid upgrade header rejected: identity=%+v err=%v", identity, err)
	}
}

// Genesis alone has a zero parent. Sharing the root authenticator with general
// identity observations must keep that valid boundary without relaxing later blocks.
func TestIdentityHeaderAllowsZeroParentOnlyAtGenesis(t *testing.T) {
	for _, height := range []uint64{0, 1} {
		header, hash := rootReceiptHeaderFixture(t, "0x"+strings.Repeat("0", 64), height, nil, false)
		observed, err := header.authenticate(hash)
		if height == 0 && (err != nil || observed != 0) || height != 0 && err == nil {
			t.Errorf("zero parent at height %d: observed=%d err=%v", height, observed, err)
		}
	}
}

// Inspect and monitor consume the same complete runtime tuple decoder as
// artifact reads, so missing or contradictory aliases cannot disappear.
func TestIdentityRejectsIncompleteOrContradictoryRuntimeVersion(t *testing.T) {
	versions := []string{
		`{"specVersion":991,"transactionVersion":1,"stateVersion":1}`,
		`{"specName":"synthetic-runtime","specVersion":991,"transactionVersion":1}`,
		`{"specName":"synthetic-runtime","specVersion":991,"transactionVersion":1,"stateVersion":1,"systemVersion":2}`,
	}
	for _, version := range versions {
		client, counts := identityIntegrityClient(t, identityTestHeader(), testFinalizedHash, json.RawMessage(version))
		identity, err := client.readIdentity(context.Background())
		if !errors.Is(err, errRpcIntegrity) || identity.FinalizedHash != "" || counts["state_getRuntimeVersion"] != 1 {
			t.Errorf("invalid version was accepted: version=%s identity=%+v calls=%v err=%v", version, identity, counts, err)
		}
	}
}
