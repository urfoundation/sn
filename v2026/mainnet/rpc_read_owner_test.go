// Read-only identity transport regressions use real HTTP response bodies and
// explicit cancellation/close boundaries. No writes or external nodes run.
package main

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
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// The actual body closes before the deterministic injected close outcome.
type rpcReadOwnerTestBody struct {
	io.ReadCloser
	closes         *atomic.Int32
	closeErr       error
	afterClose     func()
	readErrorAfter int
	bytesRead      int
}

// Preserve a simultaneous byte-limit crossing and physical read failure.
func (self *rpcReadOwnerTestBody) Read(buffer []byte) (int, error) {
	n, err := self.ReadCloser.Read(buffer)
	self.bytesRead += n
	if self.readErrorAfter > 0 && self.bytesRead >= self.readErrorAfter {
		err = errors.Join(err, io.ErrUnexpectedEOF)
	}
	return n, err
}

// Count socket-body ownership, then expose the selected late boundary.
func (self *rpcReadOwnerTestBody) Close() error {
	err := self.ReadCloser.Close()
	self.closes.Add(1)
	if self.afterClose != nil {
		self.afterClose()
	}
	return errors.Join(err, self.closeErr)
}

// A physical read failure cannot erase an already observed byte-bound breach.
func TestRpcReadOwnerHttpOverflowRetainsSimultaneousReadCause(t *testing.T) {
	var calls, closes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = io.Copy(io.Discard, request.Body)
		if calls.Add(1) == 1 {
			_, _ = io.WriteString(writer, strings.Repeat(" ", 65))
		} else {
			_, _ = io.WriteString(writer, `{"jsonrpc":"2.0","id":1,"result":"synthetic-value"}`)
		}
	}))
	defer server.Close()
	client, err := newRpcClient(server.URL, 300*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	transport := client.httpClient.Transport.(*http.Transport)
	defer transport.CloseIdleConnections()
	client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		response, err := transport.RoundTrip(request)
		if err == nil {
			body := &rpcReadOwnerTestBody{ReadCloser: response.Body, closes: &closes}
			if calls.Load() == 1 {
				body.readErrorAfter = 65
			}
			response.Body = body
		}
		return response, err
	})
	waits := 0
	client.retryWait = func(ctx context.Context, _ time.Duration) error { waits++; return ctx.Err() }
	var observed string
	err = client.callBoundedRead(t.Context(), "system_chain", []any{}, &observed, false, 64)
	if !errors.Is(err, errRpcIntegrity) || !errors.Is(err, io.ErrUnexpectedEOF) || observed != "" || calls.Load() != 1 || closes.Load() != 1 || waits != 0 {
		t.Fatal("physical error erased observed overflow", observed, calls.Load(), closes.Load(), waits, err)
	}
}

// A transport-only close failure discards the completed value and repeats the
// exact pinned query after close. The foreign caller mutation has no effect.
func TestRpcReadOwnerHttpRetriesTransportCloseWithPinnedRequest(t *testing.T) {
	var calls, closes atomic.Int32
	var stateLock sync.Mutex
	var first []byte
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
				t.Error("retry changed the pinned query")
			}
		}()
		_, _ = io.WriteString(writer, `{"jsonrpc":"2.0","id":1,"result":"synthetic-value"}`)
	}))
	defer server.Close()
	client, err := newRpcClient(server.URL, 300*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	transport := client.httpClient.Transport
	defer client.httpClient.CloseIdleConnections()
	defer transport.(*http.Transport).CloseIdleConnections()
	client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		response, err := transport.RoundTrip(request)
		if err == nil {
			var closeErr error
			if calls.Load() == 1 {
				closeErr = io.ErrUnexpectedEOF
			}
			response.Body = &rpcReadOwnerTestBody{ReadCloser: response.Body, closes: &closes, closeErr: closeErr}
		}
		return response, err
	})
	params := []any{"synthetic-storage-key", "synthetic-pinned-block"}
	waits := 0
	var observed string
	client.retryWait = func(ctx context.Context, delay time.Duration) error {
		waits++
		if closes.Load() != 1 || observed != "" {
			t.Error("failed body published a value or retained custody before retry")
		}
		params[1] = "foreign-block"
		return ctx.Err()
	}
	err = client.call(t.Context(), "state_getStorage", params, &observed)
	if err != nil || observed != "synthetic-value" || calls.Load() != 2 || closes.Load() != 2 || waits != 1 {
		t.Fatal("transport close did not recover the original read", observed, calls.Load(), closes.Load(), waits, err)
	}
}

// A complete successful response cannot hide a local, mixed hard or canceled
// close. The operation returns before parsing its result or starting a retry.
func TestRpcReadOwnerHttpRetainsHardCloseAndCancellation(t *testing.T) {
	hard := errors.New("synthetic close integrity failure")
	for _, closeErr := range []error{hard, errors.Join(io.ErrUnexpectedEOF, hard), &os.PathError{Op: "read", Path: "synthetic-response", Err: context.DeadlineExceeded}, context.Canceled} {
		var calls, closes atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			calls.Add(1)
			_, _ = io.Copy(io.Discard, request.Body)
			_, _ = io.WriteString(writer, `{"jsonrpc":"2.0","id":1,"result":"synthetic-value"}`)
		}))
		client, err := newRpcClient(server.URL, 300*time.Second)
		if err != nil {
			server.Close()
			t.Fatal(err)
		}
		transport := client.httpClient.Transport.(*http.Transport)
		ctx, cancel := context.WithCancel(t.Context())
		client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
			response, err := transport.RoundTrip(request)
			if err == nil {
				body := &rpcReadOwnerTestBody{ReadCloser: response.Body, closes: &closes, closeErr: closeErr}
				if closeErr == context.Canceled {
					body.closeErr, body.afterClose = nil, cancel
				}
				response.Body = body
			}
			return response, err
		})
		waits := 0
		client.retryWait = func(context.Context, time.Duration) error { waits++; return errors.New("unexpected retry") }
		var observed string
		err = client.call(ctx, "system_chain", []any{}, &observed)
		cancel()
		transport.CloseIdleConnections()
		server.Close()
		if err == nil || !errors.Is(err, closeErr) || observed != "" || calls.Load() != 1 || closes.Load() != 1 || waits != 0 {
			t.Fatalf("hard close was lost: cause=%T value=%q calls=%d closes=%d waits=%d err=%v", closeErr, observed, calls.Load(), closes.Load(), waits, err)
		}
	}
}

// Even an interrupted denial body has an authoritative terminal status; a
// readable healthy response on a later request must never replace that fact.
func TestRpcReadOwnerHttpPreservesHardStatusDuringBodyInterruption(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusTemporaryRedirect} {
		var calls atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			_, _ = io.Copy(io.Discard, request.Body)
			if calls.Add(1) == 1 {
				writer.Header().Set("Content-Length", "100")
				writer.WriteHeader(status)
				_, _ = io.WriteString(writer, "short")
				return
			}
			_, _ = io.WriteString(writer, `{"jsonrpc":"2.0","id":1,"result":"synthetic-value"}`)
		}))
		client, err := newRpcClient(server.URL, 300*time.Second)
		if err != nil {
			server.Close()
			t.Fatal(err)
		}
		waits := 0
		client.retryWait = func(ctx context.Context, _ time.Duration) error { waits++; return ctx.Err() }
		var observed string
		err = client.call(t.Context(), "system_chain", []any{}, &observed)
		client.httpClient.CloseIdleConnections()
		server.Close()
		if err == nil || !errors.Is(err, io.ErrUnexpectedEOF) || status == http.StatusTemporaryRedirect && !errors.Is(err, errRpcIntegrity) || observed != "" || calls.Load() != 1 || waits != 0 {
			t.Fatal("interrupted body erased terminal status", status, observed, calls.Load(), waits, err)
		}
	}
}

// Complete envelope contradictions remain hard even when body close returns
// an independently retryable physical failure at the same boundary.
func TestRpcReadOwnerHttpMalformedEnvelopeRetainsCloseCause(t *testing.T) {
	for _, body := range []string{`{"jsonrpc":"2.0","id":99,"result":"synthetic-value"}`, `{"jsonrpc":"2.0","id":1,"result":"synthetic-value","result":"other"}`, `{"jsonrpc":"2.0","id":1,"result":123}`} {
		var calls, closes atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			calls.Add(1)
			_, _ = io.Copy(io.Discard, request.Body)
			_, _ = io.WriteString(writer, body)
		}))
		client, err := newRpcClient(server.URL, 300*time.Second)
		if err != nil {
			server.Close()
			t.Fatal(err)
		}
		transport := client.httpClient.Transport.(*http.Transport)
		client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
			response, err := transport.RoundTrip(request)
			if err == nil {
				response.Body = &rpcReadOwnerTestBody{ReadCloser: response.Body, closes: &closes, closeErr: io.ErrUnexpectedEOF}
			}
			return response, err
		})
		waits := 0
		client.retryWait = func(context.Context, time.Duration) error { waits++; return errors.New("unexpected retry") }
		var observed string
		err = client.call(t.Context(), "system_chain", []any{}, &observed)
		transport.CloseIdleConnections()
		server.Close()
		if !errors.Is(err, errRpcIntegrity) || !errors.Is(err, io.ErrUnexpectedEOF) || observed != "" || calls.Load() != 1 || closes.Load() != 1 || waits != 0 {
			t.Fatal("complete malformed envelope lost a simultaneous cause", observed, calls.Load(), closes.Load(), waits, err)
		}
	}
}

// Cancellation joins the actual in-flight HTTP body and its server context.
func TestRpcReadOwnerHttpCancellationJoinsActiveBody(t *testing.T) {
	entered, released := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if calls.Add(1) != 1 {
			http.Error(writer, "unexpected second request", http.StatusBadRequest)
			return
		}
		_, _ = io.Copy(io.Discard, request.Body)
		_, _ = io.WriteString(writer, `{"jsonrpc":"2.0",`)
		writer.(http.Flusher).Flush()
		close(entered)
		<-request.Context().Done()
		close(released)
	}))
	defer server.Close()
	client, err := newRpcClient(server.URL, 300*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer client.httpClient.CloseIdleConnections()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		var observed string
		finished <- client.call(ctx, "system_chain", []any{}, &observed)
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
			t.Fatal("actual read lost its canceled owner", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("actual body did not terminate")
	}
	select {
	case <-released:
	case <-time.After(10 * time.Second):
		t.Fatal("server request did not observe cancellation")
	}
	if calls.Load() != 1 {
		t.Fatal("canceled owner retried", calls.Load())
	}
}

// The cycle escape sits beyond every admitted bound so a missing bound yields
// a deterministic retry assertion, without making a control run overflow.
type rpcReadOwnerTestLink struct {
	cause error
	calls int
}

// Error formatting does not recursively inspect the graph.
func (self *rpcReadOwnerTestLink) Error() string { return "synthetic read cause" }

// A nil wrapper is deliberately permissive to expose forbidden dispatch.
func (self *rpcReadOwnerTestLink) Unwrap() error {
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

// Custom joins preserve missing children for incomplete-graph regressions.
type rpcReadOwnerTestJoin struct{ causes []error }

// Use a stable diagnostic that cannot follow a cyclic subtree.
func (self *rpcReadOwnerTestJoin) Error() string { return "synthetic joined read causes" }

// A nil join must not be dispatched to obtain this retryable child.
func (self *rpcReadOwnerTestJoin) Unwrap() []error {
	if self == nil {
		return []error{syscall.ECONNRESET}
	}
	return self.causes
}

// The production request owner, not only a leaf helper, rejects incomplete,
// local and mixed hard causes before a wait can discard them.
func TestRpcReadOwnerBoundsTransportCauseInspection(t *testing.T) {
	var deep error = syscall.ECONNRESET
	for range 40 {
		deep = &rpcReadOwnerTestLink{cause: deep}
	}
	wide := make([]error, 129)
	for index := range wide {
		wide[index] = syscall.ECONNRESET
	}
	for _, cause := range []error{
		&rpcReadOwnerTestLink{}, deep, &rpcReadOwnerTestJoin{causes: wide},
		&rpcReadOwnerTestJoin{}, &rpcReadOwnerTestJoin{causes: []error{nil}},
		&rpcReadOwnerTestJoin{causes: []error{syscall.ECONNRESET, nil}},
		(*rpcReadOwnerTestLink)(nil), (*rpcReadOwnerTestJoin)(nil), (*url.Error)(nil), (*net.OpError)(nil),
		&os.PathError{Op: "read", Path: "synthetic-response", Err: context.DeadlineExceeded},
		errors.Join(syscall.ECONNRESET, errors.New("synthetic integrity failure")),
		errors.Join(syscall.ECONNRESET, context.Canceled),
	} {
		client, err := newRpcClient("http://rpc.example", 300*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		calls, waits := 0, 0
		client.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, cause })
		client.retryWait = func(context.Context, time.Duration) error { waits++; return errors.New("unexpected retry") }
		var observed string
		panicked := false
		func() {
			defer func() { panicked = recover() != nil }()
			err = client.call(t.Context(), "system_chain", []any{}, &observed)
		}()
		if panicked || err == nil || observed != "" || calls != 1 || waits != 0 {
			t.Fatalf("transport cause escaped its read owner: cause=%T panic=%t calls=%d waits=%d", cause, panicked, calls, waits)
		}
	}
}

// A complete admitted transport tree still retries, and a decoded hard RPC
// refusal remains terminal even if its diagnostic mentions network recovery.
func TestRpcReadOwnerRetainsTransportRecoveryAndRpcRefusal(t *testing.T) {
	for _, transportFailure := range []bool{true, false} {
		client, err := newRpcClient("http://rpc.example", 300*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		calls, waits := 0, 0
		client.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				if transportFailure {
					return nil, errors.Join(io.ErrUnexpectedEOF, &net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET})
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"synthetic timeout"}}`))}, nil
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"jsonrpc":"2.0","id":1,"result":"synthetic-value"}`))}, nil
		})
		client.retryWait = func(ctx context.Context, _ time.Duration) error { waits++; return ctx.Err() }
		var observed string
		err = client.call(t.Context(), "system_chain", []any{}, &observed)
		if transportFailure && (err != nil || observed != "synthetic-value" || calls != 2 || waits != 1) || !transportFailure && (err == nil || observed != "" || calls != 1 || waits != 0) {
			t.Fatal("bounded inspection changed read authority", transportFailure, observed, calls, waits, err)
		}
	}
}
