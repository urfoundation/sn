// Native identity reads use the configured physical Http owner. Exact method
// admission adds no submission, subscription, or runtime-selection authority.
package crv4

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"
)

// One transient status recovers the same identity query, preserving the actual
// returned identity for the caller's independent chain/genesis comparison.
func TestSubstrateIdentityReadHttpRecoversOriginalMethod(t *testing.T) {
	for _, item := range []struct {
		method string
		value  string
	}{
		{method: "system_chain", value: "Synthetic Original Native Chain"},
		{method: "eth_chainId", value: "0x3c4"},
	} {
		f := newSubstrateReadHttpFixture(t, func(writer http.ResponseWriter, _ *http.Request, call chainContextRPCRequest, number int) {
			if number == 1 {
				writer.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": call.ID, "result": item.value})
		})
		budget, hooks := newSubstrateReadTestBudget(t, 2*time.Second)
		f.client.readRetry = hooks
		var result string
		err := f.client.CallContext(t.Context(), &result, item.method)
		requests := f.snapshot()
		if err != nil || result != item.value || len(requests) != 2 || budget.attempts != 2 || budget.elapsed != 2*time.Second {
			t.Fatalf("native identity did not recover its original read: method=%s calls=%d attempts=%d elapsed=%s error=%v", item.method, len(requests), budget.attempts, budget.elapsed, err)
		}
		for _, request := range requests {
			if request.Method != item.method || len(request.Params) != 0 {
				t.Fatal("native identity recovery changed method or arguments")
			}
		}
	}
}

// Fast refusals must retain the full total without an early attempt ceiling.
// The logical clock advances only at the real owner's retry pacing boundary.
func TestSubstrateIdentityReadHttpFastRefusalUsesWholeBudget(t *testing.T) {
	for _, method := range []string{"system_chain", "eth_chainId"} {
		f := newSubstrateReadHttpFixture(t, func(writer http.ResponseWriter, _ *http.Request, _ chainContextRPCRequest, _ int) {
			writer.WriteHeader(http.StatusServiceUnavailable)
		})
		budget, hooks := newSubstrateReadTestBudget(t, 2*time.Second)
		f.client.readRetry = hooks
		var result string
		err := f.client.CallContext(t.Context(), &result, method)
		requests := f.snapshot()
		if !errors.Is(err, context.DeadlineExceeded) || !RetryableSubstrateReadTransportError(err) || result != "" ||
			len(requests) <= 4 || budget.attempts != len(requests) || budget.elapsed != 300*time.Second {
			t.Fatalf("fast native identity refusal shortened its total: method=%s calls=%d attempts=%d elapsed=%s error=%v", method, len(requests), budget.attempts, budget.elapsed, err)
		}
		for _, request := range requests {
			if request.Method != method || len(request.Params) != 0 {
				t.Fatal("native identity exhaustion changed its original request")
			}
		}
	}
}

// Typed application errors cannot borrow transport authority from diagnostic
// timeout text; write and subscription methods never enter the retry owner.
func TestSubstrateIdentityReadHttpKeepsApplicationAndWriteBoundaries(t *testing.T) {
	for _, method := range []string{"system_chain", "eth_chainId", "author_submitExtrinsic", "author_submitAndWatchExtrinsic"} {
		f := newSubstrateReadHttpFixture(t, func(writer http.ResponseWriter, _ *http.Request, call chainContextRPCRequest, _ int) {
			if method == "system_chain" || method == "eth_chainId" {
				_ = json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": call.ID,
					"error": map[string]any{"code": -32000, "message": "synthetic identity contradiction with timeout text"}})
				return
			}
			writer.WriteHeader(http.StatusServiceUnavailable)
		})
		budget, hooks := newSubstrateReadTestBudget(t, 2*time.Second)
		f.client.readRetry = hooks
		var result string
		err := f.client.CallContext(t.Context(), &result, method)
		if err == nil || len(f.snapshot()) != 1 || budget.elapsed != 0 || RetryableSubstrateReadTransportError(err) {
			t.Fatalf("application or write boundary borrowed identity retry: method=%s calls=%d elapsed=%s", method, len(f.snapshot()), budget.elapsed)
		}
	}
}
