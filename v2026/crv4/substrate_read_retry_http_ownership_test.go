// Faulted response owners cannot launder release/integrity errors into retry.
// Test-only transport decoration preserves the real GSRPC configured seam.
package crv4

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	gsrpcgeth "github.com/centrifuge/go-substrate-rpc-client/v4/gethrpc"
)

// Only the physical transport seam is replaceable; RPC parsing and the read
// marker/budget owner are the same implementations used by configured clients.
type substrateReadHttpTestRoundTripper func(*http.Request) (*http.Response, error)

// The fixture never starts a hidden retry or another endpoint lookup.
func (self substrateReadHttpTestRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return self(request)
}

// A response owns both its byte source and one explicit close outcome.
type substrateReadHttpTestBody struct {
	reader   io.Reader
	closeErr error
	closes   *int
}

// Return the injected physical reader's exact result.
func (self *substrateReadHttpTestBody) Read(value []byte) (int, error) {
	return self.reader.Read(value)
}

// The caller must complete exactly one release before a subsequent attempt.
func (self *substrateReadHttpTestBody) Close() error {
	*self.closes++
	return self.closeErr
}

// A partial physical body is different from complete malformed JSON bytes.
type substrateReadHttpBrokenBody struct{}

// Deliver a real interrupted-read cause after an incomplete JSON prefix.
func (self *substrateReadHttpBrokenBody) Read(value []byte) (int, error) {
	return copy(value, []byte(`{"jsonrpc":`)), io.ErrUnexpectedEOF
}

// Constructor helper binds the production decorator to a synthetic physical
// transport; it cannot contact the documentation address used by GSRPC.
func newSubstrateReadHttpDecoratedFixture(t *testing.T, maximum int64, transport http.RoundTripper) *contextSubstrateClient {
	t.Helper()
	endpoint := "http://192.0.2.17:80"
	client, err := gsrpcgeth.DialHTTPWithClient(endpoint, &http.Client{Transport: &substrateReadHttpTransport{base: transport, maximumBytes: maximum}})
	if err != nil {
		t.Fatal(err)
	}
	result := &contextSubstrateClient{Client: client, url: endpoint}
	t.Cleanup(result.Close)
	return result
}

// Mixed physical interruption/status and body-close integrity failures stop
// at one request, retaining both original causes without renewed read authority.
func TestSubstrateReadHttpBodyCloseFailureRemainsHard(t *testing.T) {
	for _, kind := range []string{"body", "status", "mixed-close-eof"} {
		calls, closes := 0, 0
		closeErr := errors.New("synthetic response ownership close failure")
		if kind == "mixed-close-eof" {
			closeErr = errors.Join(io.EOF, closeErr)
		}
		client := newSubstrateReadHttpDecoratedFixture(t, 1024, substrateReadHttpTestRoundTripper(func(*http.Request) (*http.Response, error) {
			calls++
			status := http.StatusOK
			var reader io.Reader = &substrateReadHttpBrokenBody{}
			if kind == "status" || kind == "mixed-close-eof" {
				status, reader = http.StatusBadGateway, strings.NewReader("synthetic unavailable")
			}
			return &http.Response{StatusCode: status, Header: make(http.Header), ContentLength: -1, Body: &substrateReadHttpTestBody{reader: reader, closeErr: closeErr, closes: &closes}}, nil
		}))
		budget, hooks := newSubstrateReadTestBudget(t, 75*time.Second)
		client.readRetry = hooks
		var result string
		err := client.CallContext(t.Context(), &result, "chain_getFinalizedHead")
		if err == nil || !errors.Is(err, closeErr) || RetryableSubstrateReadTransportError(err) || substrateRPCDisconnected(err) || calls != 1 || closes != 1 || budget.elapsed != 0 {
			t.Fatalf("physical %s hid close integrity behind retry: calls=%d closes=%d elapsed=%s err=%v", kind, calls, closes, budget.elapsed, err)
		}
		if kind == "body" && !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("mixed body failure discarded physical cause: %v", err)
		}
	}
}

// The finite transport ceiling covers declared and streamed bytes. A successful
// prefix cannot turn excess bytes or a sibling timeout into admissible evidence.
func TestSubstrateReadHttpResponseBoundRemainsHard(t *testing.T) {
	for _, declared := range []bool{false, true} {
		calls, closes := 0, 0
		client := newSubstrateReadHttpDecoratedFixture(t, 64, substrateReadHttpTestRoundTripper(func(*http.Request) (*http.Response, error) {
			calls++
			length := int64(-1)
			if declared {
				length = 128
			}
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), ContentLength: length, Body: &substrateReadHttpTestBody{reader: bytes.NewReader(bytes.Repeat([]byte{' '}, 128)), closes: &closes}}, nil
		}))
		budget, hooks := newSubstrateReadTestBudget(t, 75*time.Second)
		client.readRetry = hooks
		var result string
		err := client.CallContext(t.Context(), &result, "chain_getFinalizedHead")
		if err == nil || !strings.Contains(err.Error(), "exceeds byte limit") || RetryableSubstrateReadTransportError(errors.Join(err, context.DeadlineExceeded)) || calls != 1 || closes != 1 || budget.elapsed != 0 {
			t.Fatalf("HTTP byte bound became retry authority: declared=%t calls=%d closes=%d elapsed=%s err=%v", declared, calls, closes, budget.elapsed, err)
		}
	}
}

// An error from the physical read retains its origin after wrapping/exhaustion;
// a made-up reconnect diagnostic in that same transport is never sufficient.
func TestSubstrateReadHttpPhysicalCauseDoesNotUseDiagnosticMatching(t *testing.T) {
	for _, cause := range []error{io.EOF, io.ErrUnexpectedEOF, errors.New("client reconnected"), errors.New("502 Bad Gateway"), errors.New("connection reset by peer")} {
		calls := 0
		client := newSubstrateReadHttpDecoratedFixture(t, 1024, substrateReadHttpTestRoundTripper(func(*http.Request) (*http.Response, error) {
			calls++
			return nil, cause
		}))
		budget, hooks := newSubstrateReadTestBudget(t, 75*time.Second)
		client.readRetry = hooks
		var result string
		err := client.CallContext(t.Context(), &result, "chain_getFinalizedHead")
		transient := cause == io.EOF || cause == io.ErrUnexpectedEOF
		if err == nil || !errors.Is(err, cause) || RetryableSubstrateReadTransportError(err) != transient {
			t.Fatalf("configured physical cause changed classification: cause=%v err=%v", cause, err)
		}
		if transient && (calls != 4 || budget.elapsed != 300*time.Second) || !transient && (calls != 1 || budget.elapsed != 0) {
			t.Fatalf("diagnostic gained retry authority or EOF lost it: cause=%v calls=%d elapsed=%s", cause, calls, budget.elapsed)
		}
	}
}
