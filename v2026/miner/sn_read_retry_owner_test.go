// Real HTTP exchanges retain request bytes and cancellation while logical
// retry time advances only at explicit completed-attempt boundaries.
package miner

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/urnetwork/connect/v2026"
)

// Wrap a real socket response while counting its actual close boundary.
type minerReadOwnerTestBody struct {
	io.ReadCloser
	closes   *atomic.Int32
	closeErr error
}

// No later attempt may start while the preceding body is still owned.
func (self *minerReadOwnerTestBody) Close() error {
	err := self.ReadCloser.Close()
	self.closes.Add(1)
	return errors.Join(err, self.closeErr)
}

// EOF during actual body close has the same physical origin as an interrupted
// body read. A local-file or mixed hard cause still prohibits another request.
func TestEthRpcReadHttpRetriesPhysicalCloseAndKeepsHardCauses(t *testing.T) {
	hard := errors.New("synthetic response integrity failure")
	for _, test := range []struct {
		cause    error
		retry    bool
		response string
	}{
		{cause: io.EOF, retry: true},
		{cause: io.ErrUnexpectedEOF, retry: true},
		{cause: errors.Join(io.ErrUnexpectedEOF, hard)},
		{cause: &os.PathError{Op: "read", Path: "synthetic-response", Err: io.ErrUnexpectedEOF}},
		{cause: errors.Join(io.EOF, context.Canceled)},
		{cause: io.ErrUnexpectedEOF, response: `{"jsonrpc":"2.0","id":99,"result":"0x1234"}`},
		{cause: io.ErrUnexpectedEOF, response: `{"jsonrpc":"2.0","id":1,"result":123}`},
		{cause: io.ErrUnexpectedEOF, response: `{"jsonrpc":"2.0","id":1,"result":"not-hex"}`},
		{cause: io.ErrUnexpectedEOF, response: `{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"synthetic refusal"}}`},
		{cause: io.ErrUnexpectedEOF, response: `{"jsonrpc":"2.0",`},
	} {
		var calls, closes atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			calls.Add(1)
			_, _ = io.Copy(io.Discard, request.Body)
			response := test.response
			if response == "" {
				response = `{"jsonrpc":"2.0","id":1,"result":"0x1234"}`
			}
			_, _ = io.WriteString(writer, response)
		}))
		transport := &http.Transport{Proxy: nil}
		client := &http.Client{Transport: ethRpcTestTransport(func(request *http.Request) (*http.Response, error) {
			response, err := transport.RoundTrip(request)
			if err == nil {
				var closeErr error
				if calls.Load() == 1 {
					closeErr = test.cause
				}
				response.Body = &minerReadOwnerTestBody{ReadCloser: response.Body, closes: &closes, closeErr: closeErr}
			}
			return response, err
		})}
		waits := 0
		value, err := ethRpcHexResultWithRetry(t.Context(), client, server.URL, "eth_call", []any{"synthetic-pinned-call"}, ethRpcRetryHooks{wait: func(ctx context.Context, _ time.Duration) error {
			waits++
			if closes.Load() != calls.Load() {
				t.Error("physical close had not completed before retry")
			}
			return ctx.Err()
		}})
		transport.CloseIdleConnections()
		server.Close()
		if test.retry && (err != nil || value != "0x1234" || calls.Load() != 2 || waits != 1) || !test.retry && (err == nil || !errors.Is(err, test.cause) || value != "" || calls.Load() != 1 || waits != 0) || closes.Load() != calls.Load() {
			t.Fatalf("physical close changed read authority: cause=%T retry=%t value=%q calls=%d closes=%d waits=%d err=%v", test.cause, test.retry, value, calls.Load(), closes.Load(), waits, err)
		}
	}
}

// A 250-second outage needs more than the former 64 attempts. The original
// five-minute owner, exact calldata and configured endpoint survive it.
func TestEthRpcReadHttpRecoversWithinFiveMinuteOwner(t *testing.T) {
	var elapsed atomic.Int64
	var calls, closes atomic.Int32
	var first []byte
	var stateLock sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		count := calls.Add(1)
		raw, err := io.ReadAll(request.Body)
		if err != nil {
			t.Error(err)
			return
		}
		func() {
			stateLock.Lock()
			defer stateLock.Unlock()
			if count == 1 {
				first = append([]byte(nil), raw...)
			} else if !bytes.Equal(first, raw) {
				t.Error("retry changed the original read request")
			}
		}()
		if request.Method != http.MethodPost {
			t.Error("read-only RPC changed its HTTP method")
		}
		if time.Duration(elapsed.Load()) < 250*time.Second {
			http.Error(writer, "synthetic temporary outage", http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(writer, `{"jsonrpc":"2.0","id":1,"result":"0x1234"}`)
	}))
	defer server.Close()
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: ethRpcTestTransport(func(request *http.Request) (*http.Response, error) {
		response, err := transport.RoundTrip(request)
		if err == nil {
			response.Body = &minerReadOwnerTestBody{ReadCloser: response.Body, closes: &closes}
		}
		return response, err
	})}
	input := map[string]any{"to": "synthetic-contract", "data": "0x1234"}
	params := []any{input, "finalized"}
	budgets := 0
	var admittedTimeout time.Duration
	var owner *ethRpcTestDeadline
	hooks := ethRpcRetryHooks{
		withTimeout: func(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
			budgets++
			admittedTimeout = timeout
			owner = &ethRpcTestDeadline{Context: ctx, deadline: time.Now().Add(timeout), done: make(chan struct{})}
			return owner, owner.finish
		},
		wait: func(ctx context.Context, delay time.Duration) error {
			if ctx != owner || closes.Load() != calls.Load() || delay < 2*time.Second || delay >= 3*time.Second {
				t.Fatal("retry lost its owner, pacing or preceding body")
			}
			input["data"], params[1] = "0xbeef", "latest"
			if time.Duration(elapsed.Add(int64(delay))) >= admittedTimeout {
				owner.finish()
			}
			return ctx.Err()
		},
	}
	value, err := ethRpcHexResultWithRetry(t.Context(), client, server.URL, "eth_call", params, hooks)
	if err != nil || value != "0x1234" || budgets != 1 || admittedTimeout != 300*time.Second || calls.Load() <= 64 || closes.Load() != calls.Load() || time.Duration(elapsed.Load()) < 250*time.Second || time.Duration(elapsed.Load()) >= 300*time.Second {
		t.Fatalf("HTTP read ended before its admitted deadline: value=%q calls=%d closes=%d budgets=%d elapsed=%s err=%v", value, calls.Load(), closes.Load(), budgets, time.Duration(elapsed.Load()), err)
	}
}

// Cancellation happens only after a flushed incomplete response is observed.
// The caller, actual body and server request all terminate without a retry.
func TestEthRpcReadHttpOwnerCancellationClosesActiveBody(t *testing.T) {
	entered, released := make(chan struct{}), make(chan struct{})
	var calls, closes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if calls.Add(1) != 1 {
			http.Error(writer, "unexpected second request", http.StatusBadRequest)
			return
		}
		_, _ = io.Copy(io.Discard, request.Body)
		writer.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(writer, `{"jsonrpc":"2.0",`)
		writer.(http.Flusher).Flush()
		close(entered)
		<-request.Context().Done()
		close(released)
	}))
	defer server.Close()
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: ethRpcTestTransport(func(request *http.Request) (*http.Response, error) {
		response, err := transport.RoundTrip(request)
		if err == nil {
			response.Body = &minerReadOwnerTestBody{ReadCloser: response.Body, closes: &closes}
		}
		return response, err
	})}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		_, err := ethRpcHexResultWithClient(ctx, client, server.URL, "eth_chainId", []any{})
		finished <- err
	}()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("actual response body did not enter")
	}
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("owner cancellation lost", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("canceled read did not join")
	}
	select {
	case <-released:
	case <-time.After(10 * time.Second):
		t.Fatal("actual server request remained active")
	}
	if calls.Load() != 1 || closes.Load() != 1 {
		t.Fatal("canceled read leaked its body or retried", calls.Load(), closes.Load())
	}
}

// Cycles remain self-referential throughout the permitted inspection. The
// distant escape makes an omitted bound fail an assertion instead of hanging.
type minerReadOwnerTestLink struct {
	cause error
	calls int
}

// Diagnostics never recursively format the graph under test.
func (self *minerReadOwnerTestLink) Error() string { return "synthetic read wrapper" }

// A nil receiver would wrongly confer retry authority if it were dispatched.
func (self *minerReadOwnerTestLink) Unwrap() error {
	if self == nil {
		return syscall.ECONNRESET
	}
	self.calls++
	if self.cause != nil {
		return self.cause
	}
	if self.calls > 256 {
		return syscall.ECONNRESET
	}
	return self
}

// Preserve nil children that errors.Join would otherwise remove.
type minerReadOwnerTestJoin struct{ causes []error }

// Error display is independent of child traversal.
func (self *minerReadOwnerTestJoin) Error() string { return "synthetic joined read failure" }

// A nil joined receiver must be refused before this method is called.
func (self *minerReadOwnerTestJoin) Unwrap() []error {
	if self == nil {
		return []error{syscall.ECONNRESET}
	}
	return self.causes
}

// Nil network implementations must not receive method dispatch either.
type minerReadOwnerTestNetwork struct{}

// Structured methods, rather than diagnostic words, establish transport type.
func (self *minerReadOwnerTestNetwork) Error() string { return "synthetic network cause" }

// The permissive method exposes accidental dispatch on a nil receiver.
func (self *minerReadOwnerTestNetwork) Timeout() bool { return true }

// Match the network interface without affecting the test's error tree.
func (self *minerReadOwnerTestNetwork) Temporary() bool { return true }

// The actual read owners must refuse every incomplete graph before any wait
// or second read. Both policies retain local-file and mixed hard precedence.
func TestMinerReadOwnersBoundIncompleteCauseGraphs(t *testing.T) {
	var deep error = syscall.ECONNRESET
	for range 40 {
		deep = &minerReadOwnerTestLink{cause: deep}
	}
	wide := make([]error, 129)
	for index := range wide {
		wide[index] = syscall.ECONNRESET
	}
	for _, cause := range []error{
		&minerReadOwnerTestLink{}, deep, &minerReadOwnerTestJoin{causes: wide},
		&minerReadOwnerTestJoin{}, &minerReadOwnerTestJoin{causes: []error{nil}},
		&minerReadOwnerTestJoin{causes: []error{syscall.ECONNRESET, nil}},
		(*minerReadOwnerTestLink)(nil), (*minerReadOwnerTestJoin)(nil), (*minerReadOwnerTestNetwork)(nil),
		(*ethRpcStatusError)(nil), (*ethRpcTransportError)(nil), (*connect.HttpStatusError)(nil),
		(*url.Error)(nil), (*net.OpError)(nil), (*os.PathError)(nil),
		&os.PathError{Op: "read", Path: "synthetic-fixture", Err: context.DeadlineExceeded},
		errors.Join(syscall.ECONNRESET, errors.New("synthetic integrity failure")),
		errors.Join(syscall.ECONNRESET, context.Canceled),
	} {
		for _, claim := range []bool{false, true} {
			reads, waits, panicked := 0, 0, false
			var value string
			var err error
			func() {
				defer func() { panicked = recover() != nil }()
				read := func(context.Context) (string, error) { reads++; return "", cause }
				wait := func(context.Context, time.Duration) error { waits++; return errors.New("unexpected retry") }
				if claim {
					value, err = retryClaimApiRead(t.Context(), claimReadRetryHooks{wait: wait}, read)
				} else {
					value, err = retryEthRpcRead(t.Context(), ethRpcRetryHooks{wait: wait}, read)
				}
			}()
			if panicked || err == nil || value != "" || reads != 1 || waits != 0 {
				t.Fatalf("incomplete graph escaped its read owner: cause=%T claim=%t panic=%t reads=%d waits=%d", cause, claim, panicked, reads, waits)
			}
		}
	}
}

// A bounded complete cause tree still allows independent healthy recovery;
// a previous graph refusal cannot consume a later operation's traversal budget.
func TestMinerReadOwnersRetainCompleteTransportRecovery(t *testing.T) {
	for _, claim := range []bool{false, true} {
		reads, waits := 0, 0
		read := func(context.Context) (string, error) {
			reads++
			if reads == 1 {
				return "", errors.Join(&url.Error{Op: "Get", URL: "https://rpc.example", Err: io.ErrUnexpectedEOF}, &net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET})
			}
			return "healthy", nil
		}
		wait := func(context.Context, time.Duration) error { waits++; return nil }
		var value string
		var err error
		if claim {
			value, err = retryClaimApiRead(t.Context(), claimReadRetryHooks{wait: wait}, read)
		} else {
			value, err = retryEthRpcRead(t.Context(), ethRpcRetryHooks{wait: wait}, read)
		}
		if err != nil || value != "healthy" || reads != 2 || waits != 1 {
			t.Fatal("bounded complete transport tree lost recovery", claim, value, reads, waits, err)
		}
	}
}
