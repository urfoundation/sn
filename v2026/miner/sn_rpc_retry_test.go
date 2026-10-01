// Deterministic recovery tests exercise the real request and response boundary.
package miner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/protocol"
)

// Logical time advances only at chosen attempt and wait boundaries. The real
// request context still observes a finite deadline and terminal cancellation.
type ethRpcTestDeadline struct {
	context.Context
	deadline time.Time
	done     chan struct{}
	close    sync.Once
}

// Preserve the operation boundary in every real request context.
func (self *ethRpcTestDeadline) Deadline() (time.Time, bool) { return self.deadline, true }

// Context propagation can observe the explicitly triggered end of the budget.
func (self *ethRpcTestDeadline) Done() <-chan struct{} { return self.done }

// Parent cancellation remains distinguishable from a spent operation budget.
func (self *ethRpcTestDeadline) Err() error {
	if err := self.Context.Err(); err != nil {
		return err
	}
	select {
	case <-self.done:
		return context.DeadlineExceeded
	default:
		return nil
	}
}

// Deferred owner cleanup and logical expiry can each close the owner safely.
func (self *ethRpcTestDeadline) finish() { self.close.Do(func() { close(self.done) }) }

// The command-facing wrapper must actually enter the retry owner. The first
// serialized response is refused; only a second request can produce a value.
func TestEthRpcRetryPublicReadRecoversTransientStatus(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: ethRpcTestTransport(func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: http.NoBody}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"jsonrpc":"2.0","id":1,"result":"0x3b1"}`))}, nil
	})}
	value, err := ethRpcHexResultWithClient(t.Context(), client, "http://rpc.example", "eth_chainId", []any{})
	if err != nil || value != "0x3b1" || calls != 2 {
		t.Fatalf("command-facing read did not recover: value=%q calls=%d error=%v", value, calls, err)
	}
}

// Four full request timeouts can span a minute; the same bounded read must
// still be able to recover without a wall-clock wait in this regression test.
func TestEthRpcRetryRecoversAfterMinuteOfTimeouts(t *testing.T) {
	var owner *ethRpcTestDeadline
	elapsed := time.Duration(0)
	calls, budgets, waits := 0, 0, 0
	var attemptContexts []context.Context
	client := &http.Client{Transport: ethRpcTestTransport(func(request *http.Request) (*http.Response, error) {
		calls++
		attemptContexts = append(attemptContexts, request.Context())
		deadline, ok := request.Context().Deadline()
		if !ok || deadline.After(owner.deadline) || time.Until(deadline) > ethRpcTimeout {
			t.Fatal("attempt extended its admitted timeout")
		}
		if elapsed < time.Minute {
			elapsed += ethRpcTimeout
			return nil, context.DeadlineExceeded
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"jsonrpc":"2.0","id":1,"result":"0x3b1"}`))}, nil
	})}
	hooks := ethRpcRetryHooks{
		withTimeout: func(ctx context.Context, duration time.Duration) (context.Context, context.CancelFunc) {
			budgets++
			if duration != ethRpcOperationTimeout || duration < time.Minute || duration > 2*time.Minute {
				t.Fatalf("operation retry budget differs: %s", duration)
			}
			owner = &ethRpcTestDeadline{Context: ctx, deadline: time.Now().Add(duration), done: make(chan struct{})}
			return owner, owner.finish
		},
		wait: func(ctx context.Context, delay time.Duration) error {
			waits++
			if ctx != owner || delay < ethRpcRetryDelay || delay >= ethRpcRetryDelay*3/2 {
				t.Fatalf("retry replaced its owner or pacing: %s", delay)
			}
			elapsed += delay
			return nil
		},
	}
	value, err := ethRpcHexResultWithRetry(t.Context(), client, "http://rpc.example", "eth_chainId", []any{}, hooks)
	if err != nil || value != "0x3b1" || calls != 5 || waits != 4 || budgets != 1 || elapsed < time.Minute || elapsed >= ethRpcOperationTimeout {
		t.Fatalf("minute-long outage ended the read: value=%q calls=%d waits=%d budgets=%d elapsed=%s error=%v", value, calls, waits, budgets, elapsed, err)
	}
	for _, ctx := range attemptContexts {
		if ctx.Err() == nil {
			t.Fatal("completed attempt retained a live context")
		}
	}
}

// Constant failures spend one deadline rather than obtaining a new budget on
// every request. The exhausted error retains the original structured status.
func TestEthRpcRetryStopsAtOperationDeadline(t *testing.T) {
	var owner *ethRpcTestDeadline
	elapsed := time.Duration(0)
	calls, budgets := 0, 0
	client := &http.Client{Transport: ethRpcTestTransport(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: http.NoBody}, nil
	})}
	hooks := ethRpcRetryHooks{
		withTimeout: func(ctx context.Context, duration time.Duration) (context.Context, context.CancelFunc) {
			budgets++
			owner = &ethRpcTestDeadline{Context: ctx, deadline: time.Now().Add(duration), done: make(chan struct{})}
			return owner, owner.finish
		},
		wait: func(ctx context.Context, delay time.Duration) error {
			elapsed += delay
			if elapsed >= ethRpcOperationTimeout {
				owner.finish()
			}
			return ctx.Err()
		},
	}
	value, err := ethRpcHexResultWithRetry(t.Context(), client, "http://rpc.example", "eth_chainId", []any{}, hooks)
	var status *ethRpcStatusError
	if value != "" || !errors.Is(err, context.DeadlineExceeded) || !errors.As(err, &status) || status.status != http.StatusServiceUnavailable || calls < 20 || calls >= ethRpcMaximumAttempts || budgets != 1 || elapsed < ethRpcOperationTimeout || elapsed >= ethRpcOperationTimeout+ethRpcRetryDelay*3/2 {
		t.Fatalf("retry extended or lost its budget: value=%q calls=%d budgets=%d elapsed=%s error=%v", value, calls, budgets, elapsed, err)
	}
}

// A retry owns its already encoded request; mutation of the caller's map at
// the retry boundary must not change a contract, calldata or block selector.
func TestEthRpcRetryPreservesRequestAndClosesPriorResponse(t *testing.T) {
	input := map[string]any{"to": "0x0000000000000000000000000000000000000001", "data": "0x1234"}
	params := []any{input, "finalized"}
	var first []byte
	priorBody := &ethRpcTestBody{reader: strings.NewReader("capacity")}
	calls, waits := 0, 0
	client := &http.Client{Transport: ethRpcTestTransport(func(request *http.Request) (*http.Response, error) {
		calls++
		raw, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		if calls == 1 {
			first = raw
			return &http.Response{StatusCode: http.StatusTooManyRequests, Body: priorBody}, nil
		}
		if !bytes.Equal(raw, first) || priorBody.closes != 1 || priorBody.reads != 0 {
			t.Fatalf("retry changed request or response ownership: closes=%d reads=%d\nfirst=%s\nnext=%s", priorBody.closes, priorBody.reads, first, raw)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"jsonrpc":"2.0","id":1,"result":"0x1234"}`))}, nil
	})}
	value, err := ethRpcHexResultWithRetry(t.Context(), client, "http://rpc.example", "eth_call", params, ethRpcRetryHooks{wait: func(context.Context, time.Duration) error {
		waits++
		if priorBody.closes != 1 {
			t.Fatal("retry started before closing the failed response")
		}
		input["to"], input["data"], params[1] = "foreign", "0xbeef", "latest"
		return nil
	}})
	if err != nil || value != "0x1234" || calls != 2 || waits != 1 {
		t.Fatalf("identical read did not recover: value=%q calls=%d waits=%d error=%v", value, calls, waits, err)
	}
}

// A response-body interruption has transport provenance; a complete malformed
// JSON document with the same EOF diagnostic never receives that authority.
func TestEthRpcRetryRecoversBodyInterruptionOnly(t *testing.T) {
	for _, interrupted := range []bool{true, false} {
		calls, waits := 0, 0
		body := &ethRpcTestBody{reader: strings.NewReader(`{"jsonrpc":"2.0","id":1,`)}
		if interrupted {
			body.reader = io.MultiReader(body.reader, ethRpcTestErrorReader{err: io.ErrUnexpectedEOF})
		}
		client := &http.Client{Transport: ethRpcTestTransport(func(*http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				return &http.Response{StatusCode: http.StatusOK, Body: body}, nil
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"jsonrpc":"2.0","id":1,"result":"0x3b1"}`))}, nil
		})}
		value, err := ethRpcHexResultWithRetry(t.Context(), client, "http://rpc.example", "eth_chainId", []any{}, ethRpcRetryHooks{wait: func(context.Context, time.Duration) error {
			waits++
			if body.closes != 1 {
				t.Fatal("body was not closed before retry")
			}
			return nil
		}})
		if interrupted && (err != nil || value != "0x3b1" || calls != 2 || waits != 1) || !interrupted && (err == nil || value != "" || calls != 1 || waits != 0) {
			t.Fatalf("body failure classification differs: interrupted=%t value=%q calls=%d waits=%d error=%v", interrupted, value, calls, waits, err)
		}
	}
}

// Return a physical body read error at an explicit boundary.
type ethRpcTestErrorReader struct{ err error }

// No scheduler or wall-clock delay decides when the body is interrupted.
func (self ethRpcTestErrorReader) Read([]byte) (int, error) { return 0, self.err }

// Transport status is structured and every joined branch is inspected. Hard
// refusal, integrity and semantic errors stop before any second request.
func TestEthRpcRetryClassifierPreservesHardFailures(t *testing.T) {
	for _, test := range []struct {
		failure error
		status  int
		body    string
		close   error
		retry   bool
	}{
		{failure: context.DeadlineExceeded, retry: true},
		{failure: syscall.ECONNRESET, retry: true},
		{failure: io.EOF, retry: true},
		{status: http.StatusTooManyRequests, retry: true},
		{status: http.StatusInternalServerError, retry: true},
		{status: http.StatusServiceUnavailable, retry: true},
		{status: 599, retry: true},
		{failure: context.Canceled},
		{failure: errors.New("synthetic timeout diagnostic")},
		{failure: &os.PathError{Op: "read", Path: "fixture", Err: context.DeadlineExceeded}},
		{failure: errors.Join(context.DeadlineExceeded, errors.New("synthetic integrity failure"))},
		{status: http.StatusBadRequest},
		{status: http.StatusRequestTimeout},
		{status: http.StatusServiceUnavailable, close: errors.New("synthetic close integrity failure")},
		{status: http.StatusOK, body: `{"jsonrpc":"2.0","id":99,"result":"0x3b1"}`},
		{status: http.StatusOK, body: `{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"timeout"}}`},
		{status: http.StatusOK, body: `{"jsonrpc":"2.0","id":1,"result":"0x03b1"}`},
		{status: http.StatusOK, body: ""},
	} {
		calls, waits := 0, 0
		client := &http.Client{Transport: ethRpcTestTransport(func(*http.Request) (*http.Response, error) {
			calls++
			if calls != 1 {
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"jsonrpc":"2.0","id":1,"result":"0x3b1"}`))}, nil
			}
			if test.failure != nil {
				return nil, test.failure
			}
			return &http.Response{StatusCode: test.status, Body: &ethRpcTestBody{reader: strings.NewReader(test.body), closeErr: test.close}}, nil
		})}
		value, err := ethRpcHexResultWithRetry(t.Context(), client, "http://rpc.example", "eth_chainId", []any{}, ethRpcRetryHooks{wait: func(context.Context, time.Duration) error { waits++; return nil }})
		if test.retry && (err != nil || value != "0x3b1" || calls != 2 || waits != 1) || !test.retry && (err == nil || value != "" || calls != 1 || waits != 0) {
			t.Fatalf("retry hid or misclassified a refusal: case=%+v value=%q calls=%d waits=%d error=%v", test, value, calls, waits, err)
		}
	}
}

// Owner cancellation in the retry boundary prevents any later request, even
// if a custom wait hook returns nil after observing that cancellation.
func TestEthRpcRetryRetainsCallerCancellationAndDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	deadline, _ := ctx.Deadline()
	calls, waits := 0, 0
	client := &http.Client{Transport: ethRpcTestTransport(func(request *http.Request) (*http.Response, error) {
		calls++
		if attemptDeadline, ok := request.Context().Deadline(); !ok || attemptDeadline.After(deadline) {
			t.Fatal("attempt escaped its caller deadline")
		}
		return nil, context.DeadlineExceeded
	})}
	value, err := ethRpcHexResultWithRetry(ctx, client, "http://rpc.example", "eth_chainId", []any{}, ethRpcRetryHooks{wait: func(waitCtx context.Context, _ time.Duration) error {
		waits++
		if operationDeadline, ok := waitCtx.Deadline(); !ok || !operationDeadline.Equal(deadline) {
			t.Fatal("operation extended its caller deadline")
		}
		cancel()
		return nil
	}})
	if value != "" || !errors.Is(err, context.Canceled) || calls != 1 || waits != 1 {
		t.Fatalf("retry ignored its canceled owner: value=%q calls=%d waits=%d error=%v", value, calls, waits, err)
	}
}

// A configured wrong-chain or malformed endpoint cannot be hidden by a
// healthy later endpoint. Both fleet and claim use the same chain-first view.
func TestEthRpcViewRefusesSemanticEndpointFailover(t *testing.T) {
	for _, fault := range []string{"chain-id", "malformed", "contract-error"} {
		var sourceCalls, sourceViews, fallbackCalls atomic.Int64
		fallback := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			fallbackCalls.Add(1)
			_, _ = writer.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x3b1"}`))
		}))
		t.Cleanup(fallback.Close)
		source := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			sourceCalls.Add(1)
			var call struct {
				Method string `json:"method"`
			}
			if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
				t.Error(err)
				return
			}
			if call.Method == "eth_call" {
				sourceViews.Add(1)
				_, _ = writer.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"synthetic contract refusal"}}`))
				return
			}
			switch fault {
			case "chain-id":
				_, _ = writer.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x3b2"}`))
			case "malformed":
				_, _ = writer.Write([]byte(`{"jsonrpc":"2.0","id":99,"result":"0x3b1"}`))
			default:
				_, _ = writer.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x3b1"}`))
			}
		}))
		t.Cleanup(source.Close)
		output, endpoint, err := finalizedCoordinatorCall(t.Context(), &protocol.FleetManifest{ChainID: 945}, []string{source.URL, fallback.URL}, []byte{1})
		wantCalls, wantViews := int64(1), int64(0)
		if fault == "contract-error" {
			wantCalls, wantViews = 2, 1
		}
		if output != nil || endpoint != "" || err == nil || retryableEthRpcError(err, false) || sourceCalls.Load() != wantCalls || sourceViews.Load() != wantViews || fallbackCalls.Load() != 0 {
			t.Fatalf("semantic endpoint refusal was hidden: fault=%s output=%x endpoint=%s calls=%d views=%d fallback=%d error=%v", fault, output, endpoint, sourceCalls.Load(), sourceViews.Load(), fallbackCalls.Load(), err)
		}
	}
}
