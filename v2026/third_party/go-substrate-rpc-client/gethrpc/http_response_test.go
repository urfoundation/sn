// Force body ownership and exact HTTP admission without timing assumptions.
package rpc

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type httpResponseTestTransport func(*http.Request) (*http.Response, error)

func (self httpResponseTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return self(request)
}

type httpResponseTestBody struct {
	reader    io.Reader
	readBytes int
	closes    int
	closeErr  error
	onClose   func()
}

func (self *httpResponseTestBody) Read(value []byte) (int, error) {
	n, err := self.reader.Read(value)
	self.readBytes += n
	return n, err
}

func (self *httpResponseTestBody) Close() error {
	self.closes++
	if self.onClose != nil {
		self.onClose()
	}
	return self.closeErr
}

// A finite repeated stream exercises the physical reader without first
// allocating an attacker-sized fixture. The byte counter proves consumption.
type httpResponseRepeatedByte struct {
	remaining int64
	value     byte
}

func (self *httpResponseRepeatedByte) Read(value []byte) (int, error) {
	if self.remaining == 0 {
		return 0, io.EOF
	}
	n := min(int64(len(value)), self.remaining)
	for index := range value[:n] {
		value[index] = self.value
	}
	self.remaining -= n
	return int(n), nil
}

func newHttpResponseClient(t *testing.T, response func(*http.Request) *http.Response) *Client {
	t.Helper()
	client, err := DialHTTPWithClient("http://rpc.example", &http.Client{Transport: httpResponseTestTransport(func(request *http.Request) (*http.Response, error) { return response(request), nil })})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	return client
}

func TestHttpResponseDeclaredLimitBeforeRead(t *testing.T) {
	for _, batch := range []bool{false, true} {
		body := &httpResponseTestBody{reader: strings.NewReader(`{"jsonrpc":"2.0","id":1,"result":"accepted"}`)}
		calls := 0
		client := newHttpResponseClient(t, func(*http.Request) *http.Response {
			calls++
			return &http.Response{StatusCode: 200, ContentLength: maximumHttpResponseBytes + 1, Body: body}
		})
		result := "unchanged"
		var err error
		if batch {
			err = client.BatchCallContext(context.Background(), []BatchElem{{Method: "synthetic_read", Result: &result}})
		} else {
			err = client.CallContext(context.Background(), &result, "synthetic_read")
		}
		if !errors.Is(err, ErrHttpResponseLimit) || body.readBytes != 0 || body.closes != 1 || calls != 1 || result != "unchanged" {
			t.Fatalf("declared size reached parsing: batch=%t calls=%d bytes=%d closes=%d result=%q err=%v", batch, calls, body.readBytes, body.closes, result, err)
		}
	}
}

func TestHttpResponseUnknownLengthStopsAtProbe(t *testing.T) {
	body := &httpResponseTestBody{reader: &httpResponseRepeatedByte{remaining: 2 * maximumHttpResponseBytes, value: ' '}}
	client := newHttpResponseClient(t, func(*http.Request) *http.Response {
		return &http.Response{StatusCode: 200, ContentLength: -1, Body: body}
	})
	result := "unchanged"
	err := client.CallContext(context.Background(), &result, "state_getMetadata")
	if !errors.Is(err, ErrHttpResponseLimit) || int64(body.readBytes) != maximumHttpResponseBytes+1 || body.closes != 1 || result != "unchanged" {
		t.Fatalf("stream escaped admission: bytes=%d closes=%d result=%q err=%v", body.readBytes, body.closes, result, err)
	}
}

func TestHttpResponseExactByteBoundary(t *testing.T) {
	for _, size := range []int{64, 65} {
		body := &httpResponseTestBody{reader: strings.NewReader(strings.Repeat("x", size))}
		raw, err := readHttpResponse(context.Background(), &http.Response{StatusCode: 200, ContentLength: -1, Body: body}, 64)
		if body.closes != 1 || body.readBytes != size || size == 64 && (err != nil || len(raw) != 64) || size == 65 && (!errors.Is(err, ErrHttpResponseLimit) || raw != nil) {
			t.Fatalf("exact byte boundary changed: size=%d bytes=%d closes=%d len=%d err=%v", size, body.readBytes, body.closes, len(raw), err)
		}
	}
}

func TestHttpResponseStatusDoesNotReadOrLeakBody(t *testing.T) {
	for _, batch := range []bool{false, true} {
		body := &httpResponseTestBody{reader: strings.NewReader("synthetic-private-response")}
		client := newHttpResponseClient(t, func(*http.Request) *http.Response {
			return &http.Response{StatusCode: 503, Status: "503 synthetic", ContentLength: -1, Body: body}
		})
		var result string
		var err error
		if batch {
			err = client.BatchCallContext(context.Background(), []BatchElem{{Method: "synthetic_read", Result: &result}})
		} else {
			err = client.CallContext(context.Background(), &result, "synthetic_read")
		}
		if err == nil || !strings.Contains(err.Error(), "503") || strings.Contains(err.Error(), "synthetic-private-response") || body.readBytes != 0 || body.closes != 1 {
			t.Fatalf("status body acquired authority/storage: batch=%t bytes=%d closes=%d err=%v", batch, body.readBytes, body.closes, err)
		}
	}
}

type httpResponseLateError struct {
	sent  bool
	cause error
}

func (self *httpResponseLateError) Read(value []byte) (int, error) {
	if self.sent {
		return 0, self.cause
	}
	self.sent = true
	return copy(value, `{"jsonrpc":"2.0","id":1,"result":"accepted"}`), nil
}

func TestHttpResponseCompletePrefixCannotHideLateFailure(t *testing.T) {
	for _, cause := range []string{"tail", "read", "close", "cancel-close"} {
		ctx, cancel := context.WithCancel(context.Background())
		sentinel := errors.New("synthetic body ownership failure")
		body := &httpResponseTestBody{reader: strings.NewReader(`{"jsonrpc":"2.0","id":1,"result":"accepted"}`)}
		switch cause {
		case "tail":
			body.reader = strings.NewReader(`{"jsonrpc":"2.0","id":1,"result":"accepted"} {}`)
		case "read":
			body.reader = &httpResponseLateError{cause: sentinel}
		case "close":
			body.closeErr = sentinel
		case "cancel-close":
			body.onClose = cancel
		}
		client := newHttpResponseClient(t, func(*http.Request) *http.Response {
			return &http.Response{StatusCode: 200, ContentLength: -1, Body: body}
		})
		result := "unchanged"
		err := client.CallContext(ctx, &result, "synthetic_read")
		cancel()
		if err == nil || body.closes != 1 || result != "unchanged" || (cause == "read" || cause == "close") && !errors.Is(err, sentinel) || cause == "cancel-close" && !errors.Is(err, context.Canceled) {
			t.Fatalf("late %s hidden by JSON prefix: result=%q closes=%d err=%v", cause, result, body.closes, err)
		}
	}
}

func TestHttpResponseBatchRejectsForeignDuplicateAndMissing(t *testing.T) {
	for _, responses := range []string{
		`[{"jsonrpc":"2.0","id":1,"result":"one"}]`,
		`[{"jsonrpc":"2.0","id":1,"result":"one"},{"jsonrpc":"2.0","id":1,"result":"duplicate"}]`,
		`[{"jsonrpc":"2.0","id":1,"result":"one"},{"jsonrpc":"2.0","id":9,"result":"foreign"}]`,
		`[{"jsonrpc":"2.0","id":1,"result":"one"},{"jsonrpc":"2.0","id":2,"result":"two"},{"jsonrpc":"2.0","id":3,"result":"extra"}]`,
	} {
		body := &httpResponseTestBody{reader: strings.NewReader(responses)}
		client := newHttpResponseClient(t, func(*http.Request) *http.Response {
			return &http.Response{StatusCode: 200, ContentLength: -1, Body: body}
		})
		op := &requestOp{ids: []json.RawMessage{json.RawMessage("1"), json.RawMessage("2")}, resp: make(chan *jsonrpcMessage, 3)}
		// Spare channel capacity makes the old excess-response defect observable
		// without hanging the test. Admission must publish zero partial replies.
		err := client.sendBatchHTTP(context.Background(), op, []*jsonrpcMessage{{Version: vsn, ID: op.ids[0], Method: "one"}, {Version: vsn, ID: op.ids[1], Method: "two"}})
		if err == nil || len(op.resp) != 0 || body.closes != 1 {
			t.Fatalf("invalid batch partially published: replies=%d closes=%d err=%v", len(op.resp), body.closes, err)
		}
	}
}

func TestHttpResponseBatchPreservesReorderingAndRpcError(t *testing.T) {
	body := &httpResponseTestBody{reader: strings.NewReader(`[{"jsonrpc":"2.0","id":2,"error":{"code":-32601,"message":"synthetic refusal"}},{"jsonrpc":"2.0","id":1,"result":"one"}]`)}
	client := newHttpResponseClient(t, func(*http.Request) *http.Response {
		return &http.Response{StatusCode: 200, ContentLength: -1, Body: body}
	})
	var first, second string
	batch := []BatchElem{{Method: "one", Result: &first}, {Method: "two", Result: &second}}
	if err := client.BatchCallContext(context.Background(), batch); err != nil || first != "one" || batch[0].Error != nil || batch[1].Error == nil || body.closes != 1 {
		t.Fatalf("valid batch changed: first=%q batch=%+v closes=%d err=%v", first, batch, body.closes, err)
	}
	var rpcError Error
	if !errors.As(batch[1].Error, &rpcError) || rpcError.ErrorCode() != -32601 {
		t.Fatal("typed RPC error changed:", batch[1].Error)
	}
}

func TestHttpResponseNotificationHasNoReplyOwner(t *testing.T) {
	for _, wire := range []string{"", `{"jsonrpc":"2.0","id":null,"result":null}`} {
		body := &httpResponseTestBody{reader: strings.NewReader(wire)}
		client := newHttpResponseClient(t, func(*http.Request) *http.Response {
			return &http.Response{StatusCode: 200, ContentLength: -1, Body: body}
		})
		op := &requestOp{resp: make(chan *jsonrpcMessage, 1)}
		if err := client.sendHTTP(context.Background(), op, &jsonrpcMessage{Version: vsn, Method: "synthetic_notice"}); err != nil || len(op.resp) != 0 || body.closes != 1 {
			t.Fatalf("notification acquired reply owner: replies=%d closes=%d err=%v", len(op.resp), body.closes, err)
		}
	}
}

func TestHttpResponseRejectsSingleForeignIdentity(t *testing.T) {
	body := &httpResponseTestBody{reader: strings.NewReader(`{"jsonrpc":"2.0","id":9,"result":"foreign"}`)}
	client := newHttpResponseClient(t, func(*http.Request) *http.Response {
		return &http.Response{StatusCode: 200, ContentLength: -1, Body: body}
	})
	result := "unchanged"
	if err := client.CallContext(context.Background(), &result, "synthetic_read"); err == nil || result != "unchanged" || body.closes != 1 {
		t.Fatalf("foreign response published: result=%q closes=%d err=%v", result, body.closes, err)
	}
}

func TestHttpResponsePreservesLargeNativeEvents(t *testing.T) {
	// This exceeds the unrelated 5 MiB websocket/server-request limit and
	// fills the existing 16 MiB raw-events contract after JSON hex expansion.
	payloadBytes := 2 + 2*16*1024*1024
	reader := io.MultiReader(strings.NewReader(`{"jsonrpc":"2.0","id":1,"result":"0x`), &httpResponseRepeatedByte{remaining: int64(payloadBytes - 2), value: '0'}, strings.NewReader(`"}`))
	body := &httpResponseTestBody{reader: reader}
	client := newHttpResponseClient(t, func(*http.Request) *http.Response {
		return &http.Response{StatusCode: 200, ContentLength: -1, Body: body}
	})
	var result string
	if err := client.CallContext(context.Background(), &result, "state_getStorage", "0x0102"); err != nil || len(result) != payloadBytes || body.closes != 1 {
		t.Fatalf("valid event allowance narrowed: len=%d closes=%d err=%v", len(result), body.closes, err)
	}
}

// Each event result fits on its own. The aggregate remains independently
// bounded and refuses before parsing or exposing either partial batch result.
func TestHttpResponseLargeBatchHasAggregateCeiling(t *testing.T) {
	reader := io.MultiReader(
		strings.NewReader(`[{"jsonrpc":"2.0","id":1,"result":"0x`),
		&httpResponseRepeatedByte{remaining: 32 * 1024 * 1024, value: '0'},
		strings.NewReader(`"},{"jsonrpc":"2.0","id":2,"result":"0x`),
		&httpResponseRepeatedByte{remaining: 32 * 1024 * 1024, value: '0'},
		strings.NewReader(`"}]`),
	)
	body := &httpResponseTestBody{reader: reader}
	client := newHttpResponseClient(t, func(*http.Request) *http.Response {
		return &http.Response{StatusCode: 200, ContentLength: -1, Body: body}
	})
	first, second := "unchanged", "unchanged"
	batch := []BatchElem{{Method: "state_getStorage", Result: &first}, {Method: "state_getStorage", Result: &second}}
	err := client.BatchCallContext(context.Background(), batch)
	if !errors.Is(err, ErrHttpResponseLimit) || int64(body.readBytes) != maximumHttpResponseBytes+1 || body.closes != 1 || first != "unchanged" || second != "unchanged" {
		t.Fatalf("large batch escaped aggregate admission: bytes=%d closes=%d first=%d second=%d err=%v", body.readBytes, body.closes, len(first), len(second), err)
	}
}

func TestHttpResponseGzipLimitUsesExpandedBytes(t *testing.T) {
	var compressed bytes.Buffer
	zip := gzip.NewWriter(&compressed)
	if _, err := io.Copy(zip, &httpResponseRepeatedByte{remaining: maximumHttpResponseBytes + 1, value: ' '}); err != nil {
		t.Fatal(err)
	}
	if err := zip.Close(); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Accept-Encoding") != "gzip" {
			t.Error("automatic HTTP gzip negotiation absent")
		}
		writer.Header().Set("Content-Encoding", "gzip")
		writer.Header().Set("Content-Length", fmt.Sprint(compressed.Len()))
		_, _ = writer.Write(compressed.Bytes())
	}))
	defer server.Close()
	client, err := DialHTTPWithClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	var result string
	if err := client.CallContext(context.Background(), &result, "state_getMetadata"); !errors.Is(err, ErrHttpResponseLimit) || result != "" {
		t.Fatalf("compressed length bypassed decoded-body bound: compressed=%d result=%q err=%v", compressed.Len(), result, err)
	}
}

func TestHttpResponseCancellationOwnsBodyRead(t *testing.T) {
	entered, stopped := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Length", "1000")
		_, _ = writer.Write([]byte(`{"jsonrpc":`))
		writer.(http.Flusher).Flush()
		close(entered)
		<-request.Context().Done()
		close(stopped)
	}))
	defer server.Close()
	client, err := DialHTTPWithClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { var result string; done <- client.CallContext(ctx, &result, "synthetic_read") }()
	<-entered
	cancel()
	err = <-done
	<-stopped
	if !errors.Is(err, context.Canceled) {
		t.Fatal("physical body read lost cancellation:", err)
	}
}
