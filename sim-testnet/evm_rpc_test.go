package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestPublicEVMTransportReplays429RequestAfterBoundedDelay(t *testing.T) {
	payload := []byte(`{"jsonrpc":"2.0","method":"eth_chainId","id":1}`)
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://rpc.example", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	transport := &rateLimitedRetryTransport{
		gate: &rpcRequestGate{interval: time.Nanosecond}, maximumRetries: 1,
		defaultRetryAfter: time.Millisecond, maximumRetryAfter: time.Second,
		base: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			calls++
			body, readErr := io.ReadAll(request.Body)
			if readErr != nil || !bytes.Equal(body, payload) {
				t.Fatalf("replayed request body = %q, error=%v", body, readErr)
			}
			if calls == 1 {
				return &http.Response{StatusCode: http.StatusTooManyRequests, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"retry_after_seconds":0}`))}, nil
			}
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"jsonrpc":"2.0","result":"0x3b1","id":1}`))}, nil
		}),
	}
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || calls != 2 {
		t.Fatalf("response/calls = %d/%d, want 200/2", response.StatusCode, calls)
	}
}

func TestPublicEVMTransportReplaysHTTP200UpstreamOverloadForRead(t *testing.T) {
	payload := []byte(`{"jsonrpc":"2.0","method":"eth_call","params":[{"to":"0x0000000000000000000000000000000000000802","data":"0x12345678"},"latest"],"id":1}`)
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://rpc.example", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	transport := &rateLimitedRetryTransport{
		gate: &rpcRequestGate{interval: time.Nanosecond}, maximumRetries: 1,
		defaultRetryAfter: time.Nanosecond, maximumRetryAfter: time.Second,
		base: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			calls++
			body, readErr := io.ReadAll(request.Body)
			if readErr != nil || !bytes.Equal(body, payload) {
				t.Fatalf("replayed request body = %q, error=%v", body, readErr)
			}
			if calls == 1 {
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"jsonrpc":"2.0","error":{"code":-32000,"message":"Upstream overloaded"},"id":1}`))}, nil
			}
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"jsonrpc":"2.0","result":"0x01","id":1}`))}, nil
		}),
	}
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || calls != 2 || !strings.Contains(string(body), `"result":"0x01"`) {
		t.Fatalf("response calls=%d body=%q error=%v", calls, body, err)
	}
}

func TestPublicEVMTransportReplaysReadBatchWithOneUpstreamOverload(t *testing.T) {
	payload := []byte(`[{"jsonrpc":"2.0","method":"eth_call","params":[],"id":1},{"jsonrpc":"2.0","method":"eth_getCode","params":[],"id":2}]`)
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://rpc.example", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	transport := &rateLimitedRetryTransport{
		gate: &rpcRequestGate{interval: time.Nanosecond}, maximumRetries: 1,
		defaultRetryAfter: time.Nanosecond, maximumRetryAfter: time.Second,
		base: roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`[{"jsonrpc":"2.0","result":"0x01","id":1},{"jsonrpc":"2.0","error":{"code":-32000,"message":"upstream overloaded"},"id":2}]`))}, nil
			}
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`[{"jsonrpc":"2.0","result":"0x01","id":1},{"jsonrpc":"2.0","result":"0x02","id":2}]`))}, nil
		}),
	}
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if calls != 2 {
		t.Fatalf("read batch calls = %d, want 2", calls)
	}
}

func TestPublicEVMTransportBuffersProxyRequestBeforeOverloadReplay(t *testing.T) {
	payload := []byte(`{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}`)
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://rpc.example", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	request.GetBody = nil
	calls := 0
	transport := &rateLimitedRetryTransport{
		gate: &rpcRequestGate{interval: time.Nanosecond}, maximumRetries: 1,
		defaultRetryAfter: time.Nanosecond, maximumRetryAfter: time.Second,
		base: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			calls++
			body, readErr := io.ReadAll(request.Body)
			if readErr != nil || !bytes.Equal(body, payload) {
				t.Fatalf("proxy replay body = %q, error=%v", body, readErr)
			}
			if calls == 1 {
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"jsonrpc":"2.0","error":{"code":-32000,"message":"Upstream overloaded"},"id":1}`))}, nil
			}
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"jsonrpc":"2.0","result":"0x2a","id":1}`))}, nil
		}),
	}
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if calls != 2 {
		t.Fatalf("proxy request calls = %d, want 2", calls)
	}
}

func TestPublicEVMTransportReplaysTransientHTTPStatusForRead(t *testing.T) {
	for _, status := range []int{http.StatusRequestTimeout, http.StatusTooEarly, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout} {
		payload := []byte(`{"jsonrpc":"2.0","method":"eth_getCode","params":[],"id":1}`)
		request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://rpc.example", bytes.NewReader(payload))
		if err != nil {
			t.Fatal(err)
		}
		calls := 0
		transport := &rateLimitedRetryTransport{
			gate: &rpcRequestGate{interval: time.Nanosecond}, maximumRetries: 1,
			defaultRetryAfter: time.Nanosecond, maximumRetryAfter: time.Second,
			base: roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("temporarily unavailable"))}, nil
				}
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"jsonrpc":"2.0","result":"0x","id":1}`))}, nil
			}),
		}
		response, err := transport.RoundTrip(request)
		if err != nil {
			t.Errorf("status %d: %v", status, err)
			continue
		}
		response.Body.Close()
		if calls != 2 || response.StatusCode != http.StatusOK {
			t.Errorf("status %d response/calls = %d/%d, want 200/2", status, response.StatusCode, calls)
		}
	}
}

func TestPublicEVMTransportReplaysTransportFailureForRead(t *testing.T) {
	payload := []byte(`{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}`)
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://rpc.example", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	transport := &rateLimitedRetryTransport{
		gate: &rpcRequestGate{interval: time.Nanosecond}, maximumRetries: 1,
		defaultRetryAfter: time.Nanosecond, maximumRetryAfter: time.Second,
		base: roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				return nil, io.ErrUnexpectedEOF
			}
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"jsonrpc":"2.0","result":"0x2a","id":1}`))}, nil
		}),
	}
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if calls != 2 {
		t.Fatalf("transport read calls = %d, want 2", calls)
	}
}

func TestPublicEVMTransportDoesNotReplayTransactionSubmissionOverload(t *testing.T) {
	payload := []byte(`{"jsonrpc":"2.0","method":"eth_sendRawTransaction","params":["0x01"],"id":1}`)
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://rpc.example", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	transport := &rateLimitedRetryTransport{
		gate: &rpcRequestGate{interval: time.Nanosecond}, maximumRetries: 3,
		defaultRetryAfter: time.Nanosecond, maximumRetryAfter: time.Second,
		base: roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"jsonrpc":"2.0","error":{"code":-32000,"message":"Upstream overloaded"},"id":1}`))}, nil
		}),
	}
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if calls != 1 {
		t.Fatalf("transaction submission calls = %d, want 1", calls)
	}
}

func TestPublicEVMTransportDoesNotReplayMixedBatchWithTransactionSubmission(t *testing.T) {
	payload := []byte(`[{"jsonrpc":"2.0","method":"eth_call","params":[],"id":1},{"jsonrpc":"2.0","method":"eth_sendRawTransaction","params":["0x01"],"id":2}]`)
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://rpc.example", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	transport := &rateLimitedRetryTransport{
		gate: &rpcRequestGate{interval: time.Nanosecond}, maximumRetries: 3,
		defaultRetryAfter: time.Nanosecond, maximumRetryAfter: time.Second,
		base: roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`[{"jsonrpc":"2.0","result":"0x01","id":1},{"jsonrpc":"2.0","error":{"code":-32000,"message":"Upstream overloaded"},"id":2}]`))}, nil
		}),
	}
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if calls != 1 {
		t.Fatalf("mixed write batch calls = %d, want 1", calls)
	}
}

func TestPublicEVMTransportDoesNotReplayTransactionSubmissionHTTPFailure(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		payload := []byte(`{"jsonrpc":"2.0","method":"eth_sendRawTransaction","params":["0x01"],"id":1}`)
		request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://rpc.example", bytes.NewReader(payload))
		if err != nil {
			t.Fatal(err)
		}
		calls := 0
		transport := &rateLimitedRetryTransport{
			gate: &rpcRequestGate{interval: time.Nanosecond}, maximumRetries: 3,
			defaultRetryAfter: time.Nanosecond, maximumRetryAfter: time.Second,
			base: roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("temporarily unavailable"))}, nil
			}),
		}
		response, err := transport.RoundTrip(request)
		if err != nil {
			t.Errorf("status %d: %v", status, err)
			continue
		}
		response.Body.Close()
		if calls != 1 || response.StatusCode != status {
			t.Errorf("status %d transaction response/calls = %d/%d, want %d/1", status, response.StatusCode, calls, status)
		}
	}
}

func TestPublicEVMTransportDoesNotReplayTransactionSubmissionTransportFailure(t *testing.T) {
	payload := []byte(`{"jsonrpc":"2.0","method":"eth_sendRawTransaction","params":["0x01"],"id":1}`)
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://rpc.example", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	transport := &rateLimitedRetryTransport{
		gate: &rpcRequestGate{interval: time.Nanosecond}, maximumRetries: 3,
		defaultRetryAfter: time.Nanosecond, maximumRetryAfter: time.Second,
		base: roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			return nil, io.ErrUnexpectedEOF
		}),
	}
	if _, err := transport.RoundTrip(request); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("transaction transport error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("transaction transport calls = %d, want 1", calls)
	}
}

func TestPublicEVMTransportDoesNotReplayContractRevert(t *testing.T) {
	payload := []byte(`{"jsonrpc":"2.0","method":"eth_call","params":[],"id":1}`)
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://rpc.example", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	transport := &rateLimitedRetryTransport{
		gate: &rpcRequestGate{interval: time.Nanosecond}, maximumRetries: 3,
		defaultRetryAfter: time.Nanosecond, maximumRetryAfter: time.Second,
		base: roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"jsonrpc":"2.0","error":{"code":3,"message":"execution reverted"},"id":1}`))}, nil
		}),
	}
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if calls != 1 {
		t.Fatalf("contract revert calls = %d, want 1", calls)
	}
}

func TestPublicEVMTransportStopsAfterExactUpstreamOverloadRetryBound(t *testing.T) {
	payload := []byte(`{"jsonrpc":"2.0","method":"eth_call","params":[],"id":1}`)
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://rpc.example", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	transport := &rateLimitedRetryTransport{
		gate: &rpcRequestGate{interval: time.Nanosecond}, maximumRetries: 1,
		defaultRetryAfter: time.Nanosecond, maximumRetryAfter: time.Second,
		base: roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"jsonrpc":"2.0","error":{"code":-32000,"message":"Upstream overloaded"},"id":1}`))}, nil
		}),
	}
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || calls != 2 || !strings.Contains(string(body), "Upstream overloaded") {
		t.Fatalf("bounded overload calls=%d body=%q error=%v", calls, body, err)
	}
}

func TestPublicEVMTransportStopsAfterExactTransportRetryBound(t *testing.T) {
	payload := []byte(`{"jsonrpc":"2.0","method":"eth_call","params":[],"id":1}`)
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://rpc.example", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	transport := &rateLimitedRetryTransport{
		gate: &rpcRequestGate{interval: time.Nanosecond}, maximumRetries: 1,
		defaultRetryAfter: time.Nanosecond, maximumRetryAfter: time.Second,
		base: roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			return nil, io.ErrUnexpectedEOF
		}),
	}
	if _, err := transport.RoundTrip(request); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("bounded transport error = %v", err)
	}
	if calls != 2 {
		t.Fatalf("bounded transport calls = %d, want 2", calls)
	}
}

func TestPublicEVMTransportRejectsOversizedInspectedResponse(t *testing.T) {
	payload := []byte(`{"jsonrpc":"2.0","method":"eth_call","params":[],"id":1}`)
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://rpc.example", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	transport := &rateLimitedRetryTransport{
		gate: &rpcRequestGate{interval: time.Nanosecond}, maximumRetries: 1,
		defaultRetryAfter: time.Nanosecond, maximumRetryAfter: time.Second,
		base: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(strings.Repeat("x", publicEVMRPCResponseReadLimit+1)))}, nil
		}),
	}
	if _, err := transport.RoundTrip(request); err == nil || !strings.Contains(err.Error(), "response exceeds replay limit") {
		t.Fatalf("oversized inspected response error = %v", err)
	}
}

func TestPublicEVMTransportHonorsCancellationDuringProviderCooldown(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://rpc.example", bytes.NewReader([]byte(`{"jsonrpc":"2.0","method":"eth_chainId","id":1}`)))
	if err != nil {
		t.Fatal(err)
	}
	transport := &rateLimitedRetryTransport{
		gate: &rpcRequestGate{interval: time.Nanosecond}, maximumRetries: 3,
		defaultRetryAfter: time.Second, maximumRetryAfter: 2 * time.Second,
		base: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{"Retry-After": []string{"1"}}, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
		}),
	}
	if _, err := transport.RoundTrip(request); err == nil || ctx.Err() == nil {
		t.Fatalf("provider cooldown ignored request cancellation: %v", err)
	}
}

func TestPublicEVMTransportStopsAfterExactRetryBound(t *testing.T) {
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://rpc.example", bytes.NewReader([]byte(`{"jsonrpc":"2.0","method":"eth_chainId","id":1}`)))
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	transport := &rateLimitedRetryTransport{
		gate: &rpcRequestGate{interval: time.Nanosecond}, maximumRetries: 1,
		defaultRetryAfter: time.Nanosecond, maximumRetryAfter: time.Second,
		base: roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: http.StatusTooManyRequests, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"error":"still limited"}`))}, nil
		}),
	}
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if calls != 2 || response.StatusCode != http.StatusTooManyRequests || !strings.Contains(string(body), "still limited") {
		t.Fatalf("bounded response calls=%d status=%d body=%q", calls, response.StatusCode, body)
	}
}

func TestPublicEVMRetryAfterParsesProviderPolicyAndRejectsUnboundedWait(t *testing.T) {
	now := time.Unix(100, 0)
	if delay, err := rpcRetryAfter(http.Header{"Retry-After": []string{"7"}}, nil, now, time.Second, 10*time.Second); err != nil || delay != 7*time.Second {
		t.Fatalf("header retry delay = %s, %v", delay, err)
	}
	if delay, err := rpcRetryAfter(make(http.Header), []byte(`{"retry_after_seconds":6}`), now, time.Second, 10*time.Second); err != nil || delay != 6*time.Second {
		t.Fatalf("JSON retry delay = %s, %v", delay, err)
	}
	if _, err := rpcRetryAfter(http.Header{"Retry-After": []string{"11"}}, nil, now, time.Second, 10*time.Second); err == nil {
		t.Fatal("unbounded provider retry delay was accepted")
	}
}

func TestPublicEVMRequestGatePacesAndSharesCooldown(t *testing.T) {
	gate := &rpcRequestGate{interval: 5 * time.Millisecond}
	if err := gate.wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if err := gate.wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if time.Since(started) < 3*time.Millisecond {
		t.Fatal("consecutive public EVM requests were not paced")
	}
	gate.cooldown(time.Now().Add(10 * time.Millisecond))
	started = time.Now()
	if err := gate.wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if time.Since(started) < 8*time.Millisecond {
		t.Fatal("provider cooldown was not shared by the request gate")
	}
}

func TestPublicEVMRequestGateQueuesFIFOAndReclaimsCanceledFront(t *testing.T) {
	now := time.Unix(1_000, 0)
	gate := &rpcRequestGate{interval: 10 * time.Second}
	first := gate.enqueue()
	second := gate.enqueue()
	third := gate.enqueue()
	if front, delay, _ := gate.waiterState(first, now); !front || delay != 0 {
		t.Fatalf("first waiter state = front %t delay %s", front, delay)
	}
	if front, _, _ := gate.waiterState(second, now); front {
		t.Fatal("second waiter bypassed the FIFO front")
	}
	gate.remove(second)
	if front, _, _ := gate.waiterState(third, now); front {
		t.Fatal("canceling a middle waiter displaced the FIFO front")
	}
	if !gate.admit(first, now) {
		t.Fatal("due FIFO front was not admitted")
	}
	if front, delay, _ := gate.waiterState(third, now); !front || delay != 10*time.Second {
		t.Fatalf("third waiter state = front %t delay %s", front, delay)
	}
	fourth := gate.enqueue()
	gate.remove(third)
	if front, delay, _ := gate.waiterState(fourth, now); !front || delay != 10*time.Second {
		t.Fatalf("canceled front did not promote its successor: front %t delay %s", front, delay)
	}
	gate.cooldown(now.Add(20 * time.Second))
	if front, delay, _ := gate.waiterState(fourth, now); !front || delay != 20*time.Second {
		t.Fatalf("shared cooldown state = front %t delay %s", front, delay)
	}
	if gate.admit(fourth, now.Add(19*time.Second)) || !gate.admit(fourth, now.Add(20*time.Second)) {
		t.Fatal("front admission ignored the exact cooldown boundary")
	}
	gate.stateLock.Lock()
	remaining := len(gate.waiters)
	gate.stateLock.Unlock()
	if remaining != 0 {
		t.Fatalf("request gate retained %d waiters", remaining)
	}
}

// A request canceled before admission leaves both the queue and the provider's
// next-capacity timestamp untouched.
func TestPublicEVMRequestGateDoesNotChargeCanceledDueWaiter(t *testing.T) {
	gate := &rpcRequestGate{interval: time.Second}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := gate.wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled gate wait = %v", err)
	}
	gate.stateLock.Lock()
	defer gate.stateLock.Unlock()
	if len(gate.waiters) != 0 || !gate.next.IsZero() {
		t.Fatalf("canceled gate consumed provider capacity: waiters=%d next=%s", len(gate.waiters), gate.next)
	}
}

func TestSharedProviderPostStateCloneIsDetached(t *testing.T) {
	original := map[string]any{"nested": map[string]any{"value": "before"}}
	cloned, err := cloneObservedPostState(original)
	if err != nil {
		t.Fatal(err)
	}
	cloned["nested"].(map[string]any)["value"] = "after"
	if original["nested"].(map[string]any)["value"] != "before" {
		t.Fatal("shared-provider evidence reused a mutable observation map")
	}
}

func TestConfiguredEVMRateLimitAppliesOnlyToPublicRoute(t *testing.T) {
	cfg := testResolvedConfig(t)
	public := cfg.Public.Chain.EVMPublicReadEndpoint
	if configuredEVMRequestsPerMinute(cfg, public) != 40 || configuredEVMRequestsPerMinute(cfg, cfg.OperationalEVM) != 0 {
		t.Fatal("private operational and public observer EVM routes were not distinguished")
	}
	cfg.OperationalEVM = public
	cfg.OperationalRPCMode = rpcModePublicOverride
	if configuredEVMRequestsPerMinute(cfg, cfg.OperationalEVM) != 40 {
		t.Fatal("public operational EVM route was not rate limited")
	}
}
