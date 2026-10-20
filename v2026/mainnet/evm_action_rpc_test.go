// The shared contract read profile is exercised through real local HTTP. Its
// methods remain unavailable to the general native reader and cannot submit.
package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// One owned route returns exact synthetic wire bytes. A bounded reader may
// close an oversized response before the server finishes writing it.
func newEvmReadProfileFixture(t *testing.T, result string) (*rpcClient, *atomic.Int32) {
	t.Helper()
	count := &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		count.Add(1)
		var call struct {
			JsonRpc string `json:"jsonrpc"`
			Id      int    `json:"id"`
			Method  string `json:"method"`
		}
		if request.Method != http.MethodPost || json.NewDecoder(request.Body).Decode(&call) != nil || call.JsonRpc != "2.0" || call.Id != 1 || call.Method == "" {
			t.Error("contract read changed its RPC request identity")
			http.Error(writer, "invalid synthetic request", http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"jsonrpc":"2.0","id":1,"result":`+result+`}`)
	}))
	t.Cleanup(server.Close)
	client, err := newRpcClient(server.URL, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.httpClient.CloseIdleConnections)
	return client, count
}

// Every existing EVM read remains reachable through its explicit profile;
// general reads, writes, subscriptions and unbounded calls never reach HTTP.
func TestEvmReadProfileKeepsMethodsScoped(t *testing.T) {
	client, count := newEvmReadProfileFixture(t, `"0xab"`)
	for _, method := range []string{"eth_getTransactionReceipt", "eth_getTransactionByBlockHashAndIndex", "eth_getTransactionCount", "eth_getBalance", "eth_getCode", "eth_call", "eth_getStorageAt"} {
		before := count.Load()
		var result json.RawMessage
		if err := client.call(t.Context(), method, []any{}, &result); err == nil || count.Load() != before {
			t.Fatal("EVM method leaked into general read profile", method, err)
		}
		if err := client.callEvmRead(nil, method, []any{}, &result); err == nil || count.Load() != before {
			t.Fatal("EVM profile admitted an unbounded call", method, err)
		}
		if err := client.callEvmRead(t.Context(), method, []any{}, &result); err != nil || string(result) != `"0xab"` || count.Load() != before+1 {
			t.Fatal("scoped EVM read did not reach the owned transport exactly once", method, err, string(result), count.Load()-before)
		}
	}
	for _, method := range []string{"eth_sendRawTransaction", "eth_sendTransaction", "author_submitExtrinsic", "eth_sign", "personal_sign", "eth_subscribe", "state_subscribeStorage", "system_chain", "debug_getRawHeader", "eth_getBlockByHash", "eth_getBlockByNumber", "synthetic_unknown"} {
		before := count.Load()
		var result json.RawMessage
		if err := client.callEvmRead(t.Context(), method, []any{}, &result); err == nil || count.Load() != before {
			t.Fatal("unscoped method reached contract read transport", method, err)
		}
	}
}

// A pending receipt may be absent. Code, slots, calls and transaction/account
// facts cannot silently turn a null response into an observed zero value.
func TestEvmReadProfileAbsenceIsReceiptOnly(t *testing.T) {
	client, count := newEvmReadProfileFixture(t, "null")
	for _, method := range []string{"eth_getTransactionReceipt", "eth_getTransactionByBlockHashAndIndex", "eth_getTransactionCount", "eth_getBalance", "eth_getCode", "eth_call", "eth_getStorageAt"} {
		before := count.Load()
		var result json.RawMessage
		err := client.callEvmRead(t.Context(), method, []any{}, &result)
		if count.Load() != before+1 {
			t.Fatal("absence check did not consume the exact method", method, count.Load()-before)
		}
		if method == "eth_getTransactionReceipt" {
			if err != nil || string(result) != "null" {
				t.Fatal("pending receipt absence was rejected", err, string(result))
			}
		} else if !errors.Is(err, errRpcIntegrity) || result != nil {
			t.Fatal("missing contract fact was accepted or decoded", method, err, string(result))
		}
	}
}

// All shared methods preserve the existing fixed reply ceiling before JSON
// decoding; a node cannot grow a current observation through another method.
func TestEvmReadProfileBoundsEveryReply(t *testing.T) {
	client, count := newEvmReadProfileFixture(t, `"0x`+strings.Repeat("ab", maxRpcReplyBytes)+`"`)
	for _, method := range []string{"eth_getTransactionReceipt", "eth_getTransactionByBlockHashAndIndex", "eth_getTransactionCount", "eth_getBalance", "eth_getCode", "eth_call", "eth_getStorageAt"} {
		before := count.Load()
		var result json.RawMessage
		err := client.callEvmRead(t.Context(), method, []any{}, &result)
		if !errors.Is(err, errRpcIntegrity) || result != nil || count.Load() != before+1 || !strings.Contains(err.Error(), "reply exceeds") {
			t.Fatal("oversized EVM reply escaped the fixed ceiling", method, err, count.Load()-before)
		}
	}
}
