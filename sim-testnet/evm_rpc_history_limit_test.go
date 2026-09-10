package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Historical reads retain their exact block, calldata and JSON-RPC identities
// when an HTTP200 capacity refusal is retried, including partially successful
// read-only batches. A later success must replace the refused response.
func TestPublicEVMTransportReplaysHistoricalWorkLimit(t *testing.T) {
	for _, fixture := range []struct {
		name, request, limited, success string
	}{
		{
			name:    "single",
			request: `{"jsonrpc":"2.0","method":"eth_call","params":[{"to":"0x0000000000000000000000000000000000000802","data":"0x12345678"},"0x789e77"],"id":88}`,
			limited: `{"jsonrpc":"2.0","error":{"code":-32000,"message":"Historical work rate limit exceeded"},"id":88}`,
			success: `{"jsonrpc":"2.0","result":"0x01","id":88}`,
		},
		{
			name:    "partial_read_batch",
			request: `[{"jsonrpc":"2.0","method":"eth_call","params":[{"to":"0x0000000000000000000000000000000000000802","data":"0x12345678"},"0x789e77"],"id":88},{"jsonrpc":"2.0","method":"eth_getCode","params":["0x0000000000000000000000000000000000000802","0x789e77"],"id":89}]`,
			limited: `[{"jsonrpc":"2.0","result":"0x01","id":88},{"jsonrpc":"2.0","error":{"code":-32000,"message":"Historical work rate limit exceeded"},"id":89}]`,
			success: `[{"jsonrpc":"2.0","result":"0x01","id":88},{"jsonrpc":"2.0","result":"0x02","id":89}]`,
		},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://rpc.example", strings.NewReader(fixture.request))
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			transport := &rateLimitedRetryTransport{
				gate: &rpcRequestGate{interval: time.Nanosecond}, maximumRetries: publicEVMMaximumRetries,
				defaultRetryAfter: time.Nanosecond, maximumRetryAfter: time.Second,
				base: roundTripFunc(func(request *http.Request) (*http.Response, error) {
					calls++
					body, err := io.ReadAll(request.Body)
					if err != nil || !bytes.Equal(body, []byte(fixture.request)) {
						t.Fatalf("historical replay changed request bytes: error=%v", err)
					}
					response := fixture.limited
					if calls == 3 {
						response = fixture.success
					}
					return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(response))}, nil
				}),
			}
			response, err := transport.RoundTrip(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil || calls != 3 || string(body) != fixture.success {
				t.Fatalf("historical work-limit retry did not reach bounded success: calls=%d body=%q error=%v", calls, body, err)
			}
		})
	}
}

// A capacity sentinel does not authorize transaction replay, unbounded
// attempts, or reinterpretation of a permanent/different history error.
func TestPublicEVMTransportHistoricalWorkLimitKeepsRetryAndWriteBounds(t *testing.T) {
	const read = `{"jsonrpc":"2.0","method":"eth_call","params":[{"to":"0x0000000000000000000000000000000000000802","data":"0x12345678"},"0x789e77"],"id":1}`
	const write = `{"jsonrpc":"2.0","method":"eth_sendRawTransaction","params":["0xdead"],"id":2}`
	const limited = `{"jsonrpc":"2.0","error":{"code":-32000,"message":"Historical work rate limit exceeded"},"id":1}`
	for _, fixture := range []struct {
		name, request, response string
		calls                   int
	}{
		{"exhausted", read, limited, publicEVMMaximumRetries + 1},
		{"transaction_submission", write, limited, 1},
		{"mixed_batch", "[" + read + "," + write + "]", "[" + limited + "," + limited + "]", 1},
		{"permanent_history", read, `{"jsonrpc":"2.0","error":{"code":-32000,"message":"Historical state unavailable"},"id":1}`, 1},
		{"different_error", read, `{"jsonrpc":"2.0","error":{"code":-32000,"message":"Historical work rate limit exceeded: invalid proof"},"id":1}`, 1},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://rpc.example", strings.NewReader(fixture.request))
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			transport := &rateLimitedRetryTransport{
				gate: &rpcRequestGate{interval: time.Nanosecond}, maximumRetries: publicEVMMaximumRetries,
				defaultRetryAfter: time.Nanosecond, maximumRetryAfter: time.Second,
				base: roundTripFunc(func(*http.Request) (*http.Response, error) {
					calls++
					return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(fixture.response))}, nil
				}),
			}
			response, err := transport.RoundTrip(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil || calls != fixture.calls || string(body) != fixture.response {
				t.Fatalf("historical work-limit refusal changed retry/write boundary: calls=%d want=%d body=%q error=%v", calls, fixture.calls, body, err)
			}
		})
	}
}
