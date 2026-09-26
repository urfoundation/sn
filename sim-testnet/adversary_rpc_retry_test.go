// Synthetic RPC transports prove bounded retry without relying on the live node.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// A transient transport or gateway failure replays the same idempotent read
// and reports the extra wire attempt to the owning sample.
func TestRpcAdversaryRetriesTransientReadAndCountsAttempts(t *testing.T) {
	for _, first := range []struct {
		name   string
		status int
		err    error
	}{
		{name: "deadline", err: context.DeadlineExceeded},
		{name: "gateway", status: http.StatusServiceUnavailable},
	} {
		calls := 0
		client := adversaryGetTestClient(func(request *http.Request) (*http.Response, error) {
			calls++
			body, err := io.ReadAll(request.Body)
			if err != nil || request.Method != http.MethodPost || request.URL.Path != "/rpc" {
				t.Fatalf("changed RPC request: method=%s path=%s error=%v", request.Method, request.URL.Path, err)
			}
			var payload struct {
				Id     uint64 `json:"id"`
				Method string `json:"method"`
			}
			if json.Unmarshal(body, &payload) != nil || payload.Id != 7 || payload.Method != "chain_getHeader" {
				t.Fatalf("retry changed RPC identity: %s", body)
			}
			if calls == 1 {
				if first.err != nil {
					return nil, first.err
				}
				return adversaryGetTestResponse(first.status, "temporary"), nil
			}
			return adversaryGetTestResponse(http.StatusOK, `{"jsonrpc":"2.0","id":7,"result":{"number":"0x1"}}`), nil
		})
		actor := &rpcAdversary{http: client}
		response, err := actor.call(context.Background(), "http://node.example/rpc", "chain_getHeader", []any{"0x01"}, 7)
		if err != nil || calls != 2 || actor.retryRequests != 1 || string(response.Result) != `{"number":"0x1"}` {
			t.Fatalf("failure=%s recovered RPC read response=%+v error=%v calls=%d retries=%d", first.name, response, err, calls, actor.retryRequests)
		}
	}
}

// Sample evidence includes recovered attempts rather than only logical reads.
func TestRpcAdversarySampleReportsRecoveredWireAttempt(t *testing.T) {
	calls := 0
	client := adversaryGetTestClient(func(request *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return adversaryGetTestResponse(http.StatusBadGateway, "temporary"), nil
		}
		if calls == 2 {
			return adversaryGetTestResponse(http.StatusOK, `{"jsonrpc":"2.0","id":1,"result":"0x3b1"}`), nil
		}
		return adversaryGetTestResponse(http.StatusOK, `{"jsonrpc":"2.0","id":2,"result":"0x3b1"}`), nil
	})
	cfg := testResolvedConfig(t)
	cfg.OperationalEVM = "http://node.example/rpc"
	cfg.Public.Chain.EVMPublicReadEndpoint = "http://node.example/rpc"
	actor := &rpcAdversary{cfg: cfg, http: client}
	result := actor.Sample(context.Background(), adversaryControlPhase, 0)
	if result.Outcome != adversaryOutcomeSuccess || result.Requests != 3 || calls != 3 {
		t.Fatalf("recovered sample hid wire attempt: result=%+v calls=%d", result, calls)
	}
}

// A responsive semantic rejection cannot be hidden as a transient outage.
func TestRpcAdversaryDoesNotRetrySemanticFailure(t *testing.T) {
	for _, fixture := range []struct {
		name   string
		status int
		body   string
	}{
		{name: "http-bad-request", status: http.StatusBadRequest, body: "invalid request"},
		{name: "malformed-envelope", status: http.StatusOK, body: `{"jsonrpc":"2.0","id":8,"result":null}`},
	} {
		calls := 0
		client := adversaryGetTestClient(func(*http.Request) (*http.Response, error) {
			calls++
			return adversaryGetTestResponse(fixture.status, fixture.body), nil
		})
		actor := &rpcAdversary{http: client}
		if _, err := actor.call(context.Background(), "http://node.example/rpc", "chain_getHeader", []any{}, 7); err == nil || calls != 1 || actor.retryRequests != 0 {
			t.Fatalf("failure=%s semantic failure retried: error=%v calls=%d retries=%d", fixture.name, err, calls, actor.retryRequests)
		}
	}
}

// Exhaustion remains visible; cancellation prevents any further wire attempt.
func TestRpcAdversaryRetryExhaustionAndCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	client := adversaryGetTestClient(func(*http.Request) (*http.Response, error) {
		calls++
		return adversaryGetTestResponse(http.StatusGatewayTimeout, "temporary"), nil
	})
	actor := &rpcAdversary{http: client}
	if _, err := actor.call(context.Background(), "http://node.example/rpc", "chain_getHeader", []any{}, 7); err == nil || !strings.Contains(err.Error(), "HTTP 504") || calls != 3 || actor.retryRequests != 2 {
		t.Fatalf("exhaustion error=%v calls=%d retries=%d", err, calls, actor.retryRequests)
	}
	client.retryWait = func(context.Context, time.Duration) error { cancel(); return ctx.Err() }
	if _, err := actor.call(ctx, "http://node.example/rpc", "chain_getHeader", []any{}, 7); !errors.Is(err, context.Canceled) || calls != 4 || actor.retryRequests != 2 {
		t.Fatalf("canceled retry error=%v calls=%d retries=%d", err, calls, actor.retryRequests)
	}
}
