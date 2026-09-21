// Owned-node retry tests use deterministic RoundTrippers and waits: no chain
// RPC, scheduler sleeps, submissions or process-state changes are involved.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

type ownedEvmRetryTestBody struct {
	io.Reader
	readError error
	closed    bool
}

func (self *ownedEvmRetryTestBody) Read(data []byte) (int, error) {
	if self.readError != nil {
		return 0, self.readError
	}
	return self.Reader.Read(data)
}

func (self *ownedEvmRetryTestBody) Close() error { self.closed = true; return nil }

func ownedEvmRetryTestRequest(t *testing.T, payload string) *http.Request {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://owned-rpc.invalid:9944", strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-Synthetic-Identity", "preserved")
	return request
}

func TestOwnedEvmRetryReplaysExactReadWithoutPacing(t *testing.T) {
	t.Parallel()
	const payload = ` {"jsonrpc":"2.0","method":"eth_getBalance","params":["synthetic-account","0x123"],"id":7} `
	request := ownedEvmRetryTestRequest(t, payload)
	// A caller's misleading replay function must not alter the actual bytes.
	request.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(`{"method":"eth_sendRawTransaction"}`)), nil
	}
	calls, waits := 0, 0
	failedBody := &ownedEvmRetryTestBody{readError: io.ErrUnexpectedEOF}
	transport := newOwnedEvmRetryTransport(roundTripFunc(func(current *http.Request) (*http.Response, error) {
		calls++
		raw, err := io.ReadAll(current.Body)
		if err != nil || string(raw) != payload || current.URL.String() != request.URL.String() || current.Header.Get("X-Synthetic-Identity") != "preserved" {
			t.Fatalf("request changed: raw=%q error=%v", raw, err)
		}
		deadline, ok := current.Context().Deadline()
		if !ok || time.Until(deadline) > finalSemanticRPCAttemptTimeout {
			t.Fatal("headers/body lack a bounded attempt deadline")
		}
		switch calls {
		case 1:
			return nil, &net.OpError{Op: "read", Net: "tcp", Err: context.DeadlineExceeded}
		case 2:
			return &http.Response{StatusCode: http.StatusOK, Body: failedBody}, nil
		default:
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"jsonrpc":"2.0","id":7,"result":"0x19"}`))}, nil
		}
	}))
	transport.policy.wait = func(ctx context.Context, delay time.Duration) error {
		waits++
		if delay != time.Duration(waits)*time.Second {
			t.Fatalf("retry-only backoff=%s", delay)
		}
		return ctx.Err()
	}
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || !strings.Contains(string(body), `"result":"0x19"`) || calls != 3 || waits != 2 || !failedBody.closed {
		t.Fatalf("retry outcome: calls=%d waits=%d body=%q closed=%t error=%v", calls, waits, body, failedBody.closed, err)
	}
}

func TestOwnedEvmRetryNeverReplaysSubmissionsOrUnknownMethods(t *testing.T) {
	t.Parallel()
	for _, payload := range []string{
		`{"method":"eth_sendRawTransaction","params":["0x123"],"id":1}`,
		`[{"method":"eth_call","id":1},{"method":"eth_sendTransaction","id":2}]`,
		`{"method":"personal_unlockAccount","id":1}`,
		`{"method":"unknown_method","id":1}`,
		`{invalid-json`,
	} {
		calls := 0
		transport := newOwnedEvmRetryTransport(roundTripFunc(func(request *http.Request) (*http.Response, error) {
			calls++
			raw, err := io.ReadAll(request.Body)
			if err != nil || string(raw) != payload {
				t.Fatal("single-send request changed")
			}
			return nil, syscall.ECONNRESET
		}))
		transport.policy.wait = func(context.Context, time.Duration) error { t.Fatal("non-read request retried"); return nil }
		if _, err := transport.RoundTrip(ownedEvmRetryTestRequest(t, payload)); !errors.Is(err, syscall.ECONNRESET) || calls != 1 {
			t.Fatalf("payload=%q calls=%d error=%v", payload, calls, err)
		}
	}
}

func TestOwnedEvmRetryLeavesSemanticResponsesAndMixedFailuresUntouched(t *testing.T) {
	t.Parallel()
	for _, payload := range []string{`{"error":{"code":-32000,"message":"execution reverted: timeout"},"id":1}`, `{"error":{"message":"upstream overloaded"}}`, `{malformed-json`} {
		calls := 0
		transport := newOwnedEvmRetryTransport(roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(payload))}, nil
		}))
		transport.policy.wait = func(context.Context, time.Duration) error { t.Fatal("complete semantic response retried"); return nil }
		response, err := transport.RoundTrip(ownedEvmRetryTestRequest(t, `{"method":"eth_call","id":1}`))
		if err != nil {
			t.Fatal(err)
		}
		raw, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || string(raw) != payload || calls != 1 {
			t.Fatalf("semantic response changed: calls=%d raw=%q error=%v", calls, raw, err)
		}
	}
	for _, failure := range []error{errors.New("certificate authentication failed"), &json.SyntaxError{Offset: 9}, errors.Join(syscall.ECONNRESET, errors.New("signed evidence digest mismatch")), &os.PathError{Op: "read", Path: "evidence.json", Err: io.ErrUnexpectedEOF}, context.Canceled} {
		for _, bodyRead := range []bool{false, true} {
			calls := 0
			transport := newOwnedEvmRetryTransport(roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				if bodyRead {
					return &http.Response{StatusCode: http.StatusOK, Body: &ownedEvmRetryTestBody{readError: failure}}, nil
				}
				return nil, failure
			}))
			transport.policy.wait = func(context.Context, time.Duration) error { t.Fatal("integrity failure retried"); return nil }
			if _, err := transport.RoundTrip(ownedEvmRetryTestRequest(t, `{"method":"eth_chainId","id":1}`)); !errors.Is(err, failure) || calls != 1 {
				t.Fatalf("failure=%v body=%t calls=%d error=%v", failure, bodyRead, calls, err)
			}
		}
	}
}

func TestOwnedEvmRetryBudgetAndParentCancellation(t *testing.T) {
	t.Parallel()
	for _, cancelAt := range []string{"none", "before", "request", "backoff"} {
		ctx, cancel := context.WithCancel(t.Context())
		request := ownedEvmRetryTestRequest(t, `[{"method":"eth_chainId","id":1},{"method":"eth_getCode","id":2}]`).WithContext(ctx)
		calls, waits := 0, 0
		transport := newOwnedEvmRetryTransport(roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			if cancelAt == "request" {
				cancel()
			}
			return nil, syscall.ECONNRESET
		}))
		transport.policy.wait = func(ctx context.Context, _ time.Duration) error {
			waits++
			if cancelAt == "backoff" {
				cancel()
			}
			return ctx.Err()
		}
		if cancelAt == "before" {
			cancel()
		}
		_, err := transport.RoundTrip(request)
		if cancelAt == "none" {
			if !errors.Is(err, syscall.ECONNRESET) || calls != finalSemanticRPCMaximumAttempts || waits != finalSemanticRPCMaximumAttempts-1 {
				t.Fatalf("attempt bound: calls=%d waits=%d error=%v", calls, waits, err)
			}
		} else {
			wantCalls := 1
			if cancelAt == "before" {
				wantCalls = 0
			}
			if !errors.Is(err, context.Canceled) || calls != wantCalls {
				t.Fatalf("cancellation=%s calls=%d error=%v", cancelAt, calls, err)
			}
		}
		cancel()
	}
}

func TestOwnedEvmRetryDoesNotMultiplyOuterBudget(t *testing.T) {
	t.Parallel()
	calls, outerWaits, innerWaits := 0, 0, 0
	transport := newOwnedEvmRetryTransport(roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, syscall.ECONNRESET }))
	transport.policy.wait = func(context.Context, time.Duration) error { innerWaits++; return nil }
	policy := defaultFinalSemanticRPCRetryPolicy()
	policy.wait = func(context.Context, time.Duration) error { outerWaits++; return nil }
	err := retryFinalSemanticRPCCall(t.Context(), nil, policy, func(ctx context.Context) error {
		_, err := transport.RoundTrip(ownedEvmRetryTestRequest(t, `{"method":"eth_call","id":1}`).WithContext(ctx))
		return err
	})
	if !errors.Is(err, syscall.ECONNRESET) || calls != finalSemanticRPCMaximumAttempts || outerWaits != finalSemanticRPCMaximumAttempts-1 || innerWaits != 0 {
		t.Fatalf("multiplied retry budget: calls=%d outer=%d inner=%d error=%v", calls, outerWaits, innerWaits, err)
	}
}

// net.DNSError can carry its own transient flags while Unwrap returns nil.
// Preserve those typed flags without treating a missing name as recoverable.
func TestOwnedEvmRetryHandlesDnsErrorsWithoutWrappedCause(t *testing.T) {
	t.Parallel()
	for _, failure := range []*net.DNSError{
		{Err: "synthetic temporary lookup failure", IsTemporary: true},
		{Err: "synthetic lookup timeout", IsTimeout: true},
		{Err: "synthetic name does not exist", IsNotFound: true},
	} {
		calls, waits := 0, 0
		transport := newOwnedEvmRetryTransport(roundTripFunc(func(request *http.Request) (*http.Response, error) {
			calls++
			request.Body.Close()
			if calls == 1 {
				return nil, failure
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"jsonrpc":"2.0","id":1,"result":"0x1"}`))}, nil
		}))
		transport.policy.wait = func(ctx context.Context, _ time.Duration) error { waits++; return ctx.Err() }
		response, err := transport.RoundTrip(ownedEvmRetryTestRequest(t, `{"jsonrpc":"2.0","id":1,"method":"eth_chainId"}`))
		if response != nil {
			response.Body.Close()
		}
		if failure.IsNotFound {
			if !errors.Is(err, failure) || calls != 1 || waits != 0 {
				t.Fatalf("missing name retried: calls=%d waits=%d error=%v", calls, waits, err)
			}
		} else if err != nil || calls != 2 || waits != 1 {
			t.Fatalf("temporary DNS failure lost: calls=%d waits=%d error=%v", calls, waits, err)
		}
	}
}

func TestOwnedEvmRetryStreamsLargeBodiesWithoutNewSizeLimits(t *testing.T) {
	t.Parallel()
	large := bytes.Repeat([]byte("x"), publicEVMRPCResponseReadLimit+1024)
	calls := 0
	var observed context.Context
	original := &ownedEvmRetryTestBody{Reader: bytes.NewReader(large)}
	transport := newOwnedEvmRetryTransport(roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		observed = request.Context()
		return &http.Response{StatusCode: http.StatusOK, Body: original}, nil
	}))
	transport.policy.wait = func(context.Context, time.Duration) error { t.Fatal("large response was retried"); return nil }
	response, err := transport.RoundTrip(ownedEvmRetryTestRequest(t, `{"method":"eth_getLogs","id":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if observed.Err() != nil {
		t.Fatal("response context canceled before streaming finished")
	}
	raw, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || !bytes.Equal(raw, large) || calls != 1 || !original.closed || !errors.Is(observed.Err(), context.Canceled) {
		t.Fatalf("streaming response: length=%d calls=%d closed=%t context=%v error=%v", len(raw), calls, original.closed, observed.Err(), err)
	}
	request := ownedEvmRetryTestRequest(t, string(large))
	calls = 0
	transport.base = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		raw, err := io.ReadAll(request.Body)
		request.Body.Close()
		if err != nil || !bytes.Equal(raw, large) {
			t.Fatal("large request was truncated")
		}
		return nil, syscall.ECONNRESET
	})
	if _, err := transport.RoundTrip(request); !errors.Is(err, syscall.ECONNRESET) || calls != 1 {
		t.Fatalf("large request: calls=%d error=%v", calls, err)
	}
}
