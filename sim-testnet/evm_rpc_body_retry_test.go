package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Return a partial HTTP response and a typed read failure after headers have
// succeeded, matching the failure boundary of the historical receipt audit.
type failedPublicEVMResponseBody struct {
	data   []byte
	cause  error
	closed bool
}

func (body *failedPublicEVMResponseBody) Read(output []byte) (int, error) {
	n := copy(output, body.data)
	body.data = body.data[n:]
	if len(body.data) > 0 {
		return n, nil
	}
	return n, body.cause
}

func (body *failedPublicEVMResponseBody) Close() error {
	body.closed = true
	return nil
}

func publicEVMBodyRetryRequest(t *testing.T, ctx context.Context, payload string) *http.Request {
	t.Helper()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://rpc.example", strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func TestPublicEVMBodyReadRetryReplaysExactReadRequest(t *testing.T) {
	for _, cause := range []error{
		&net.OpError{Op: "read", Net: "tcp", Err: &os.SyscallError{Syscall: "read", Err: syscall.ECONNRESET}},
		fmt.Errorf("read response: %w", io.ErrUnexpectedEOF),
		&net.OpError{Op: "read", Net: "tcp", Err: context.DeadlineExceeded},
	} {
		for _, payload := range []string{
			`{"jsonrpc":"2.0","method":"eth_getTransactionReceipt","params":["0x0123"],"id":1}`,
			`[{"jsonrpc":"2.0","method":"eth_call","params":[{},"0x100"],"id":1},{"jsonrpc":"2.0","method":"eth_getCode","params":["0x1234","0x100"],"id":2}]`,
		} {
			t.Run(fmt.Sprintf("%T/%s", cause, payload[:1]), func(t *testing.T) {
				request := publicEVMBodyRetryRequest(t, context.Background(), payload)
				broken := &failedPublicEVMResponseBody{data: []byte(`{"jsonrpc":"2.0","result":`), cause: cause}
				calls := 0
				transport := &rateLimitedRetryTransport{
					gate: &rpcRequestGate{interval: time.Nanosecond}, maximumRetries: 1,
					defaultRetryAfter: time.Nanosecond, maximumRetryAfter: time.Second,
					base: roundTripFunc(func(current *http.Request) (*http.Response, error) {
						calls++
						body, err := io.ReadAll(current.Body)
						current.Body.Close()
						if err != nil || !bytes.Equal(body, []byte(payload)) {
							t.Fatalf("attempt %d changed request bytes=%q err=%v", calls, body, err)
						}
						if calls == 1 {
							return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: broken}, nil
						}
						if !broken.closed {
							t.Fatal("failed response body was not closed before replay")
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
				if err != nil || calls != 2 || string(body) != `{"jsonrpc":"2.0","result":"0x01","id":1}` {
					t.Fatalf("replayed response calls=%d body=%q err=%v", calls, body, err)
				}
			})
		}
	}
}

func TestPublicEVMBodyReadRetrySharesExistingAttemptBudget(t *testing.T) {
	request := publicEVMBodyRetryRequest(t, context.Background(), `{"jsonrpc":"2.0","method":"eth_getTransactionReceipt","params":["0x0123"],"id":1}`)
	calls := 0
	var bodies []*failedPublicEVMResponseBody
	transport := &rateLimitedRetryTransport{
		gate: &rpcRequestGate{interval: time.Nanosecond}, maximumRetries: 2,
		defaultRetryAfter: time.Nanosecond, maximumRetryAfter: time.Second,
		base: roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				return nil, io.ErrUnexpectedEOF
			}
			body := &failedPublicEVMResponseBody{cause: syscall.ECONNRESET}
			bodies = append(bodies, body)
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: body}, nil
		}),
	}
	_, err := transport.RoundTrip(request)
	if !errors.Is(err, syscall.ECONNRESET) || calls != 3 {
		t.Fatalf("combined transport/body retry calls=%d err=%v, want 3 and reset", calls, err)
	}
	for _, body := range bodies {
		if !body.closed {
			t.Fatal("failed response body remained open after retry exhaustion")
		}
	}
}

func TestPublicEVMBodyReadRetryRetainsSharedCancelableCooldown(t *testing.T) {
	anchor := time.Now().Add(time.Hour)
	gate := &rpcRequestGate{interval: time.Nanosecond, changed: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request := publicEVMBodyRetryRequest(t, ctx, `{"jsonrpc":"2.0","method":"eth_getTransactionReceipt","params":["0x0123"],"id":1}`)
	calls := 0
	transport := &rateLimitedRetryTransport{
		gate: gate, maximumRetries: 3, defaultRetryAfter: time.Millisecond, maximumRetryAfter: 2 * time.Second,
		now: func() time.Time { return anchor },
		base: roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			return &http.Response{
				StatusCode: http.StatusOK, Header: http.Header{"Retry-After": []string{"1"}},
				Body: &failedPublicEVMResponseBody{cause: syscall.ECONNRESET},
			}, nil
		}),
	}
	done := make(chan error, 1)
	go func() {
		_, err := transport.RoundTrip(request)
		done <- err
	}()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	var until time.Time
	for until.IsZero() {
		gate.stateLock.Lock()
		until = gate.cooldownUntil
		changed := gate.changed
		gate.stateLock.Unlock()
		if !until.IsZero() {
			if !until.Equal(anchor.Add(time.Second)) {
				cancel()
				<-done
				t.Fatalf("body-read cooldown=%s, want %s", until, anchor.Add(time.Second))
			}
			break
		}
		select {
		case <-changed:
		case <-deadline.C:
			cancel()
			<-done
			t.Fatal("body-read failure did not install a shared cooldown")
		}
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("canceled body-read retry calls=%d err=%v", calls, err)
	}
	other := gate.enqueue()
	defer gate.remove(other)
	front, delay, _ := gate.waiterState(other, anchor)
	if !front || delay != time.Second || gate.admit(other, anchor.Add(time.Second-time.Nanosecond)) {
		t.Fatalf("another caller bypassed retained cooldown: front=%t delay=%s", front, delay)
	}
}

func TestPublicEVMBodyReadRetryNeverReplaysWrites(t *testing.T) {
	for _, payload := range []string{
		`{"jsonrpc":"2.0","method":"eth_sendRawTransaction","params":["0x01"],"id":1}`,
		`[{"jsonrpc":"2.0","method":"eth_getTransactionReceipt","params":["0x0123"],"id":1},{"jsonrpc":"2.0","method":"eth_sendRawTransaction","params":["0x01"],"id":2}]`,
	} {
		request := publicEVMBodyRetryRequest(t, context.Background(), payload)
		calls := 0
		transport := &rateLimitedRetryTransport{
			gate: &rpcRequestGate{interval: time.Nanosecond}, maximumRetries: 3,
			defaultRetryAfter: time.Nanosecond, maximumRetryAfter: time.Second,
			base: roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: &failedPublicEVMResponseBody{cause: syscall.ECONNRESET}}, nil
			}),
		}
		response, err := transport.RoundTrip(request)
		if err != nil {
			t.Fatal(err)
		}
		_, err = io.ReadAll(response.Body)
		response.Body.Close()
		if !errors.Is(err, syscall.ECONNRESET) || calls != 1 {
			t.Fatalf("write response calls=%d err=%v, want 1 and reset", calls, err)
		}
	}
}

func TestPublicEVMBodyReadRetryKeepsPermanentFailures(t *testing.T) {
	for _, test := range []struct {
		name       string
		body       io.ReadCloser
		wantError  string
		wantOutput string
	}{
		{name: "text-only reset", body: &failedPublicEVMResponseBody{cause: errors.New("connection reset by peer")}, wantError: "read public EVM RPC response"},
		{name: "canceled read", body: &failedPublicEVMResponseBody{cause: context.Canceled}, wantError: "context canceled"},
		{name: "empty response", wantError: "empty HTTP response"},
		{name: "oversized interrupted body", body: &failedPublicEVMResponseBody{data: bytes.Repeat([]byte("x"), publicEVMRPCResponseReadLimit+1), cause: syscall.ECONNRESET}, wantError: "response exceeds replay limit"},
		{name: "malformed JSON", body: io.NopCloser(strings.NewReader(`{"result":`)), wantOutput: `{"result":`},
		{name: "semantic error text", body: io.NopCloser(strings.NewReader(`{"error":{"message":"connection reset by peer"},"id":1}`)), wantOutput: `{"error":{"message":"connection reset by peer"},"id":1}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := publicEVMBodyRetryRequest(t, context.Background(), `{"jsonrpc":"2.0","method":"eth_getTransactionReceipt","params":["0x0123"],"id":1}`)
			calls := 0
			transport := &rateLimitedRetryTransport{
				gate: &rpcRequestGate{interval: time.Nanosecond}, maximumRetries: 3,
				defaultRetryAfter: time.Nanosecond, maximumRetryAfter: time.Second,
				base: roundTripFunc(func(*http.Request) (*http.Response, error) {
					calls++
					return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: test.body}, nil
				}),
			}
			response, err := transport.RoundTrip(request)
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("permanent response error=%v, want %s", err, test.wantError)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				body, readErr := io.ReadAll(response.Body)
				response.Body.Close()
				if readErr != nil || string(body) != test.wantOutput {
					t.Fatalf("permanent response body=%q err=%v", body, readErr)
				}
			}
			if calls != 1 {
				t.Fatalf("permanent response replayed %d times", calls)
			}
		})
	}
}

func TestPublicEVMBodyReadRetryRejectsUnboundedRetryAfter(t *testing.T) {
	request := publicEVMBodyRetryRequest(t, context.Background(), `{"jsonrpc":"2.0","method":"eth_getTransactionReceipt","params":["0x0123"],"id":1}`)
	calls := 0
	transport := &rateLimitedRetryTransport{
		gate: &rpcRequestGate{interval: time.Nanosecond}, maximumRetries: 3,
		defaultRetryAfter: time.Second, maximumRetryAfter: publicEVMMaximumRetryAfter,
		base: roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			return &http.Response{
				StatusCode: http.StatusOK, Header: http.Header{"Retry-After": []string{"91"}},
				Body: &failedPublicEVMResponseBody{cause: syscall.ECONNRESET},
			}, nil
		}),
	}
	_, err := transport.RoundTrip(request)
	if !errors.Is(err, syscall.ECONNRESET) || !strings.Contains(err.Error(), "exceeds maximum") || calls != 1 {
		t.Fatalf("unbounded cooldown calls=%d err=%v", calls, err)
	}
}
