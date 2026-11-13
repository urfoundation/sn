// Deterministic response custody and native/EVM grammar separation tests.
package miner

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// Each test owns its transport; no global client override affects other work.
type ethRpcTestTransport func(*http.Request) (*http.Response, error)

// Supply the exact response boundary under test.
func (self ethRpcTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return self(request)
}

// Count every read and close to verify refusal never drains an unbounded body.
type ethRpcTestBody struct {
	reader     io.Reader
	reads      int
	bytesRead  int
	closes     int
	closeErr   error
	afterClose func()
}

// Retain the physical byte census, independent of JSON parsing.
func (self *ethRpcTestBody) Read(buffer []byte) (int, error) {
	self.reads++
	n, err := self.reader.Read(buffer)
	self.bytesRead += n
	return n, err
}

// The post-close hook establishes cancellation without timing races.
func (self *ethRpcTestBody) Close() error {
	self.closes++
	if self.afterClose != nil {
		self.afterClose()
	}
	return self.closeErr
}

// Declared size and non-success status refuse before any body read; chunked
// overflow consumes only the one extra byte needed to prove the bound.
func TestEthRpcResponseAdmissionPreservesBodyOwnership(t *testing.T) {
	for _, test := range []struct {
		status int
		length int64
		read   int
	}{
		{status: http.StatusOK, length: ethRpcResponseLimit + 1},
		{status: http.StatusServiceUnavailable, length: -1},
		{status: http.StatusOK, length: -1, read: ethRpcResponseLimit + 1},
	} {
		closeErr := errors.New("synthetic response close failure")
		body := &ethRpcTestBody{reader: strings.NewReader(strings.Repeat(" ", ethRpcResponseLimit+100)), closeErr: closeErr}
		client := &http.Client{Transport: ethRpcTestTransport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: test.status, ContentLength: test.length, Body: body}, nil
		})}
		value, err := ethRpcHexResultWithClient(t.Context(), client, "http://rpc.example", "eth_chainId", []any{})
		if value != "" || !errors.Is(err, closeErr) || body.bytesRead != test.read || body.closes != 1 {
			t.Fatalf("body refusal lost custody: case=%+v value=%q read=%d closes=%d error=%v", test, value, body.bytesRead, body.closes, err)
		}
	}
}

// Cancellation immediately after a complete body must not publish its result.
func TestEthRpcCancellationAfterCloseRejectsResult(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	body := &ethRpcTestBody{reader: strings.NewReader(`{"jsonrpc":"2.0","id":1,"result":"0x3b1"}`), afterClose: cancel}
	client := &http.Client{Transport: ethRpcTestTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: body}, nil
	})}
	value, err := ethRpcHexResultWithClient(ctx, client, "http://rpc.example", "eth_chainId", []any{})
	if value != "" || !errors.Is(err, context.Canceled) || body.closes != 1 {
		t.Fatalf("canceled body published a result: value=%q closes=%d error=%v", value, body.closes, err)
	}
}

// Invalid ownership and write methods never reach this read-only transport.
func TestEthRpcReadOnlyOwnerAdmission(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: ethRpcTestTransport(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("unexpected transport call")
	})}
	for _, method := range []string{"", "eth_sendRawTransaction", "author_submitExtrinsic"} {
		if value, err := ethRpcHexResultWithClient(t.Context(), client, "http://rpc.example", method, []any{}); value != "" || err == nil {
			t.Fatalf("write admitted by read owner: method=%q value=%q error=%v", method, value, err)
		}
	}
	if _, err := ethRpcHexResultWithClient(nil, client, "http://rpc.example", "eth_chainId", []any{}); err == nil {
		t.Fatal("nil context admitted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := ethRpcHexResultWithClient(ctx, client, "http://rpc.example", "eth_chainId", []any{}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation lost", err)
	}
	if calls != 0 {
		t.Fatalf("invalid read owner reached transport %d times", calls)
	}
}

// A successful envelope has exactly one result and canonical data. Null,
// conflicting members and a response-level error never become zero values.
func TestEthRpcRejectsAmbiguousOrMalformedResults(t *testing.T) {
	for _, raw := range []string{
		`{"jsonrpc":"2.0","id":1,"result":null}`,
		`{"jsonrpc":"2.0","id":1,"result":"0x3b1","error":null}`,
		`{"jsonrpc":"2.0","id":1,"error":null}`,
		`{"jsonrpc":"2.0","id":1,"result":"0x3b1","error":{"code":-32000,"message":"timeout"}}`,
		`{"jsonrpc":"2.0","id":1,"result":"3b1"}`,
		`{"jsonrpc":"2.0","id":1,"result":"0x03b1"}`,
		`{"jsonrpc":"2.0","id":1,"result":945}`,
		`{"jsonrpc":"2.0","id":1}`,
	} {
		client := &http.Client{Transport: ethRpcTestTransport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(raw))}, nil
		})}
		if value, err := ethRpcHexResultWithClient(t.Context(), client, "http://rpc.example", "eth_chainId", []any{}); value != "" || err == nil {
			t.Fatalf("ambiguous result admitted: response=%s value=%q error=%v", raw, value, err)
		}
	}
}

// Redirects must not replace the configured node with an answering foreign
// endpoint, even when that endpoint returns a plausible matching chain id.
func TestEthRpcRefusesRedirectedEndpoint(t *testing.T) {
	for _, status := range []int{http.StatusFound, http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		var targetCalls atomic.Int64
		target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			targetCalls.Add(1)
			_, _ = writer.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x3b1"}`))
		}))
		t.Cleanup(target.Close)
		source := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			http.Redirect(writer, request, target.URL, status)
		}))
		t.Cleanup(source.Close)
		value, err := ethRpcHexResult(t.Context(), source.URL, "eth_chainId", []any{})
		if value != "" || err == nil || targetCalls.Load() != 0 {
			t.Errorf("redirect changed RPC authority: status=%d value=%q target-calls=%d error=%v", status, value, targetCalls.Load(), err)
		}
	}
}

// Duplicate keys, including escaped aliases and nested fields, cannot be
// resolved by whichever value encoding/json happens to keep last.
func TestEthRpcRejectsDuplicateJsonKeys(t *testing.T) {
	for _, raw := range []string{
		`{"jsonrpc":"1.0","jsonrpc":"2.0","id":1,"result":"0x3b1"}`,
		`{"jsonrpc":"2.0","id":99,"i\u0064":1,"result":"0x3b1"}`,
		`{"jsonrpc":"2.0","id":99,"Id":1,"result":"0x3b1"}`,
		`{"jsonrpc":"2.0","id":1,"result":"0x3b2","reſult":"0x3b1"}`,
		`{"jsonrpc":"2.0","id":1,"result":"0x3b1","extra":{"n":1,"n":2}}`,
		`{"jsonrpc":"2.0","id":1,"result":"0x3b1","extra":{"n":1,"N":2}}`,
	} {
		client := &http.Client{Transport: ethRpcTestTransport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(raw))}, nil
		})}
		if value, err := ethRpcHexResultWithClient(t.Context(), client, "http://rpc.example", "eth_chainId", []any{}); value != "" || err == nil {
			t.Errorf("ambiguous keys gained RPC authority: value=%q error=%v", value, err)
		}
	}
}
