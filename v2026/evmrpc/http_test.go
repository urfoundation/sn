// Deterministic physical transport controls use real geth decoding and finite
// synthetic bodies. Faults occur after explicit read/close boundaries.
package evmrpc

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
)

// Each fixture owns one sequential call, except the explicit cancellation case.
type httpTestBody struct {
	reader   io.Reader
	read     int
	closes   int
	closeErr error
	onClose  func()
}

// Count physical bytes, including a limit's one-byte probe.
func (self *httpTestBody) Read(value []byte) (int, error) {
	n, err := self.reader.Read(value)
	self.read += n
	return n, err
}

// A close hook is a deterministic final-publication barrier.
func (self *httpTestBody) Close() error {
	self.closes++
	if self.onClose != nil {
		self.onClose()
	}
	return self.closeErr
}

type httpTestTransport func(*http.Request) (*http.Response, error)

func (self httpTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return self(request)
}

type httpTestReadError struct{ err error }

func (self httpTestReadError) Read([]byte) (int, error) { return 0, self.err }

// Construct geth on the same transport as production with a smaller local cap.
func httpTestClient(t *testing.T, base http.RoundTripper, maximum int64) *ethclient.Client {
	t.Helper()
	client, err := rpc.DialOptions(t.Context(), "http://synthetic-rpc.example", rpc.WithHTTPClient(&http.Client{
		Transport: &responseTransport{base: base, maximumBytes: maximum},
	}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	return ethclient.NewClient(client)
}

func httpTestResponse(body io.ReadCloser, length int64) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: body, ContentLength: length}
}

func TestEvmHttpDeclaredLimitPrecedesRead(t *testing.T) {
	body := &httpTestBody{reader: strings.NewReader(`{"jsonrpc":"2.0","id":1,"result":"0x3b1"}`)}
	client, err := dialContext(t.Context(), "http://synthetic-rpc.example", httpTestTransport(func(*http.Request) (*http.Response, error) {
		return httpTestResponse(body, maximumResponseBytes+1), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	value, err := client.ChainID(t.Context())
	if value != nil || !errors.Is(err, ErrResponseLimit) || body.read != 0 || body.closes != 1 {
		t.Fatalf("declared limit published/read: value=%v err=%v bytes=%d closes=%d", value, err, body.read, body.closes)
	}
}

func TestEvmHttpStreamingLimitPrecedesPrefixPublication(t *testing.T) {
	for _, batch := range []bool{false, true} {
		prefix := `{"jsonrpc":"2.0","id":1,"result":"accepted"}`
		if batch {
			prefix = "[" + prefix + "]"
		}
		body := &httpTestBody{reader: io.MultiReader(strings.NewReader(prefix), strings.NewReader(strings.Repeat(" ", int(maximumResponseBytes)+100)))}
		client := httpTestClient(t, httpTestTransport(func(*http.Request) (*http.Response, error) {
			return httpTestResponse(body, -1), nil
		}), maximumResponseBytes)
		value := "untouched"
		var err error
		if batch {
			err = client.Client().BatchCallContext(t.Context(), []rpc.BatchElem{{Method: "eth_call", Result: &value}})
		} else {
			err = client.Client().CallContext(t.Context(), &value, "eth_call")
		}
		if !errors.Is(err, ErrResponseLimit) || value != "untouched" || body.read != int(maximumResponseBytes)+1 || body.closes != 1 {
			t.Fatalf("batch=%t ignored aggregate bound: value=%q err=%v bytes=%d closes=%d", batch, value, err, body.read, body.closes)
		}
	}
}

func TestEvmHttpExactBoundaryPreservesCompleteReply(t *testing.T) {
	for _, extra := range []int{0, 1} {
		encoded := `{"jsonrpc":"2.0","id":1,"result":"accepted"}`
		encoded += strings.Repeat(" ", 128-len(encoded)+extra)
		body := &httpTestBody{reader: strings.NewReader(encoded)}
		client := httpTestClient(t, httpTestTransport(func(*http.Request) (*http.Response, error) {
			return httpTestResponse(body, -1), nil
		}), 128)
		value := "untouched"
		err := client.Client().CallContext(t.Context(), &value, "eth_call")
		if extra == 0 && (err != nil || value != "accepted") || extra == 1 && (!errors.Is(err, ErrResponseLimit) || value != "untouched") || body.closes != 1 {
			t.Fatalf("exact boundary extra=%d value=%q error=%v closes=%d", extra, value, err, body.closes)
		}
	}
}

func TestEvmHttpStatusNeverReadsOrRetainsDiagnostic(t *testing.T) {
	for _, status := range []int{307, 400, 429, 503} {
		body := &httpTestBody{reader: strings.NewReader("already known synthetic secret diagnostic")}
		client := httpTestClient(t, httpTestTransport(func(*http.Request) (*http.Response, error) {
			response := httpTestResponse(body, maximumResponseBytes+1)
			response.StatusCode = status
			return response, nil
		}), maximumResponseBytes)
		_, err := client.ChainID(t.Context())
		var statusErr rpc.HTTPError
		if !errors.As(err, &statusErr) || statusErr.StatusCode != status || len(statusErr.Body) != 0 || strings.Contains(err.Error(), "already known") || body.read != 0 || body.closes != 1 {
			t.Fatalf("status %d leaked/read body or lost typed cause: %v bytes=%d closes=%d", status, err, body.read, body.closes)
		}
	}
}

func TestEvmHttpCompletePrefixCannotHidePhysicalFailure(t *testing.T) {
	for _, fault := range []string{"read", "close", "cancel-close", "transport"} {
		ctx, cancel := context.WithCancel(t.Context())
		body := &httpTestBody{reader: strings.NewReader(`{"jsonrpc":"2.0","id":1,"result":"accepted"}`)}
		cause := errors.New("synthetic physical " + fault)
		switch fault {
		case "read":
			body.reader = io.MultiReader(body.reader, httpTestReadError{err: cause})
		case "close":
			body.closeErr = cause
		case "cancel-close":
			body.onClose, cause = cancel, context.Canceled
		}
		calls := 0
		client := httpTestClient(t, httpTestTransport(func(*http.Request) (*http.Response, error) {
			calls++
			if fault == "transport" {
				return httpTestResponse(body, -1), cause
			}
			return httpTestResponse(body, -1), nil
		}), maximumResponseBytes)
		value := "untouched"
		err := client.Client().CallContext(ctx, &value, "eth_sendRawTransaction", "0x1234")
		cancel()
		if !errors.Is(err, cause) || value != "untouched" || body.closes != 1 || calls != 1 {
			t.Fatalf("%s published or retried after physical failure: value=%q err=%v closes=%d calls=%d", fault, value, err, body.closes, calls)
		}
	}
}

func TestEvmHttpCompleteEnvelopeRefusesAmbiguousEvidence(t *testing.T) {
	for _, encoded := range []string{
		`{"jsonrpc":"2.0","id":1,"result":"yes"} {}`,
		`{"jsonrpc":"2.0","id":1,"result":"yes","result":"no"}`,
		`{"jsonrpc":"2.0","id":2,"result":"yes"}`,
		`{"jsonrpc":"2.0","id":1,"result":"yes","error":{"code":3,"message":"revert"}}`,
		`{"jsonrpc":"2.0","id":1,"error":null}`,
		`{"jsonrpc":"2.0","id":1`,
	} {
		body := &httpTestBody{reader: strings.NewReader(encoded)}
		client := httpTestClient(t, httpTestTransport(func(*http.Request) (*http.Response, error) {
			return httpTestResponse(body, -1), nil
		}), maximumResponseBytes)
		value := "untouched"
		err := client.Client().CallContext(t.Context(), &value, "eth_call")
		if err == nil || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || value != "untouched" || body.closes != 1 {
			t.Fatalf("complete malformed document received physical EOF authority: %s value=%q error=%v closes=%d", encoded, value, err, body.closes)
		}
	}
}

func TestEvmHttpBatchRejectsPartialPublication(t *testing.T) {
	for _, encoded := range []string{
		`[{"jsonrpc":"2.0","id":1,"result":"yes"}]`,
		`[{"jsonrpc":"2.0","id":1,"result":"yes"},{"jsonrpc":"2.0","id":1,"result":"yes"}]`,
		`[{"jsonrpc":"2.0","id":1,"result":"yes"},{"jsonrpc":"2.0","id":3,"result":"yes"}]`,
		`[{"jsonrpc":"2.0","id":1,"result":"yes"},{"jsonrpc":"2.0","id":2,"result":"yes"},{"jsonrpc":"2.0","id":3,"result":"yes"}]`,
	} {
		body := &httpTestBody{reader: strings.NewReader(encoded)}
		client := httpTestClient(t, httpTestTransport(func(*http.Request) (*http.Response, error) {
			return httpTestResponse(body, -1), nil
		}), maximumResponseBytes)
		first, second := "untouched", "untouched"
		err := client.Client().BatchCallContext(t.Context(), []rpc.BatchElem{{Method: "eth_call", Result: &first}, {Method: "eth_call", Result: &second}})
		if err == nil || first != "untouched" || second != "untouched" || body.closes != 1 {
			t.Fatalf("partial batch publication: first=%q second=%q error=%v", first, second, err)
		}
	}
}

func TestEvmHttpBatchPreservesReorderedResultsAndReverts(t *testing.T) {
	body := &httpTestBody{reader: strings.NewReader(`[{"jsonrpc":"2.0","id":2,"error":{"code":3,"message":"synthetic revert","data":"0x1234"}},{"jsonrpc":"2.0","id":1,"result":null}]`)}
	client := httpTestClient(t, httpTestTransport(func(*http.Request) (*http.Response, error) {
		return httpTestResponse(body, -1), nil
	}), maximumResponseBytes)
	first, second := json.RawMessage(`"before"`), "untouched"
	batch := []rpc.BatchElem{{Method: "eth_call", Result: &first}, {Method: "eth_call", Result: &second}}
	if err := client.Client().BatchCallContext(t.Context(), batch); err != nil {
		t.Fatal(err)
	}
	var dataErr rpc.DataError
	if string(first) != "null" || batch[0].Error != nil || !errors.As(batch[1].Error, &dataErr) || dataErr.ErrorData() != "0x1234" || second != "untouched" || body.closes != 1 {
		t.Fatalf("batch changed typed result/revert: %s %+v", first, batch)
	}
}

func TestEvmHttpPreservesExplicitNullErrorCompatibility(t *testing.T) {
	for _, result := range []string{`"accepted"`, `null`} {
		encoded := `{"jsonrpc":"2.0","id":1,"result":` + result + `,"error":null}`
		body := &httpTestBody{reader: strings.NewReader(encoded)}
		client := httpTestClient(t, httpTestTransport(func(*http.Request) (*http.Response, error) {
			return httpTestResponse(body, -1), nil
		}), maximumResponseBytes)
		var observed json.RawMessage
		if err := client.Client().CallContext(t.Context(), &observed, "eth_call"); err != nil || string(observed) != result || body.closes != 1 {
			t.Fatalf("explicit null error changed compatible result: %s %v", observed, err)
		}
	}
}

func TestEvmHttpGzipBoundsExpandedBytes(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		for _, excess := range []bool{false, true} {
			encoded := `{"jsonrpc":"2.0","id":1,"result":"accepted"}`
			if excess {
				encoded += strings.Repeat(" ", 256)
			}
			var compressed bytes.Buffer
			writer := gzip.NewWriter(&compressed)
			if _, err := io.WriteString(writer, encoded); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.Header().Set("Content-Encoding", "gzip")
				_, _ = writer.Write(compressed.Bytes())
			}))
			transport := http.DefaultTransport.(*http.Transport).Clone()
			transport.DisableCompression = explicit
			client, err := rpc.DialOptions(t.Context(), server.URL, rpc.WithHTTPClient(&http.Client{Transport: &responseTransport{base: transport, maximumBytes: 128}}))
			if err != nil {
				t.Fatal(err)
			}
			value := "untouched"
			err = client.CallContext(t.Context(), &value, "eth_call")
			client.Close()
			transport.CloseIdleConnections()
			server.Close()
			if excess && (!errors.Is(err, ErrResponseLimit) || value != "untouched") || !excess && (err != nil || value != "accepted") {
				t.Fatalf("gzip explicit=%t excess=%t value=%q error=%v", explicit, excess, value, err)
			}
		}
	}
}

func TestEvmHttpCompressionFailureCannotPublishPrefix(t *testing.T) {
	for _, fault := range []string{"checksum", "encoding"} {
		var compressed bytes.Buffer
		writer := gzip.NewWriter(&compressed)
		_, _ = io.WriteString(writer, `{"jsonrpc":"2.0","id":1,"result":"accepted"}`)
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		encoded := bytes.Clone(compressed.Bytes())
		encoded[len(encoded)-1] ^= 0xff
		body := &httpTestBody{reader: bytes.NewReader(encoded)}
		client := httpTestClient(t, httpTestTransport(func(*http.Request) (*http.Response, error) {
			response := httpTestResponse(body, -1)
			response.Header.Set("Content-Encoding", "gzip")
			if fault == "encoding" {
				response.Header.Set("Content-Encoding", "unsupported")
			}
			return response, nil
		}), maximumResponseBytes)
		value := "untouched"
		if err := client.Client().CallContext(t.Context(), &value, "eth_call"); err == nil || value != "untouched" || body.closes != 1 {
			t.Fatalf("%s compression failure published: %q %v", fault, value, err)
		}
	}
}

func TestEvmHttpGzipWireBoundIncludesHeaders(t *testing.T) {
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	writer.Header.Extra = bytes.Repeat([]byte{0x55}, 256)
	_, _ = io.WriteString(writer, `{"jsonrpc":"2.0","id":1,"result":"accepted"}`)
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	body := &httpTestBody{reader: bytes.NewReader(compressed.Bytes())}
	client := httpTestClient(t, httpTestTransport(func(request *http.Request) (*http.Response, error) {
		if request.Header.Get("Accept-Encoding") != "gzip" {
			t.Error("physical owner delegated gzip expansion to the underlying transport")
		}
		response := httpTestResponse(body, -1)
		response.Header.Set("Content-Encoding", "gzip")
		return response, nil
	}), 128)
	value := "untouched"
	if err := client.Client().CallContext(t.Context(), &value, "eth_call"); !errors.Is(err, ErrResponseLimit) || value != "untouched" || body.read != 129 || body.closes != 1 {
		t.Fatalf("gzip wire header ignored byte ceiling: value=%q error=%v read=%d close=%d", value, err, body.read, body.closes)
	}
}

// The explicit channel records the body read before the caller cancels.
type httpTestBlockedBody struct {
	ctx         context.Context
	entered     chan struct{}
	enteredOnce sync.Once
	closes      atomic.Int32
}

func (self *httpTestBlockedBody) Read([]byte) (int, error) {
	self.enteredOnce.Do(func() { close(self.entered) })
	<-self.ctx.Done()
	return 0, self.ctx.Err()
}

func (self *httpTestBlockedBody) Close() error { self.closes.Add(1); return nil }

func TestEvmHttpCancellationJoinsPhysicalBody(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	body := &httpTestBlockedBody{ctx: ctx, entered: make(chan struct{})}
	client := httpTestClient(t, httpTestTransport(func(*http.Request) (*http.Response, error) {
		return httpTestResponse(body, -1), nil
	}), maximumResponseBytes)
	done := make(chan error, 1)
	go func() { _, err := client.ChainID(ctx); done <- err }()
	<-body.entered
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) || body.closes.Load() != 1 {
		t.Fatalf("cancellation lost physical ownership: %v closes=%d", err, body.closes.Load())
	}
}

func TestEvmHttpRedirectCannotReplaySignedPost(t *testing.T) {
	var first, second atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { second.Add(1) }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		first.Add(1)
		http.Redirect(writer, request, target.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	client, err := DialContext(t.Context(), source.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	var result common.Hash
	err = client.Client().CallContext(t.Context(), &result, "eth_sendRawTransaction", "0x1234")
	var statusErr rpc.HTTPError
	if !errors.As(err, &statusErr) || statusErr.StatusCode != 307 || first.Load() != 1 || second.Load() != 0 {
		t.Fatalf("redirect replayed signed POST: first=%d second=%d error=%v", first.Load(), second.Load(), err)
	}
}

func TestEvmHttpPreservesLargeReceiptLogs(t *testing.T) {
	receipt := &types.Receipt{Status: 1, CumulativeGasUsed: 21_000, GasUsed: 21_000, TxHash: common.Hash{1}, BlockHash: common.Hash{2}, BlockNumber: big.NewInt(90), Logs: []*types.Log{{Address: common.Address{3}, Topics: []common.Hash{{4}}, Data: bytes.Repeat([]byte{0x55}, 4*1024*1024), BlockNumber: 90}}}
	encoded, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "result": receipt})
	if err != nil {
		t.Fatal(err)
	}
	body := &httpTestBody{reader: bytes.NewReader(encoded)}
	client := httpTestClient(t, httpTestTransport(func(*http.Request) (*http.Response, error) {
		return httpTestResponse(body, int64(len(encoded))), nil
	}), maximumResponseBytes)
	observed, err := client.TransactionReceipt(t.Context(), receipt.TxHash)
	if err != nil || observed == nil || len(observed.Logs) != 1 || !bytes.Equal(observed.Logs[0].Data, receipt.Logs[0].Data) || observed.TxHash != receipt.TxHash || body.closes != 1 {
		t.Fatalf("valid large receipt changed: %v", err)
	}
}

func TestEvmHttpPreservesNativeEventsOnSharedConnection(t *testing.T) {
	result := "0x" + strings.Repeat("55", 16*1024*1024)
	encoded := `{"jsonrpc":"2.0","id":1,"result":"` + result + `"}`
	body := &httpTestBody{reader: strings.NewReader(encoded)}
	client := httpTestClient(t, httpTestTransport(func(*http.Request) (*http.Response, error) {
		return httpTestResponse(body, int64(len(encoded))), nil
	}), maximumResponseBytes)
	var observed string
	if err := client.Client().CallContext(t.Context(), &observed, "state_getStorage", "0xsynthetic", common.Hash{1}); err != nil || observed != result || body.closes != 1 {
		t.Fatalf("native event wire allowance changed: %v bytes=%d", err, len(observed))
	}
}

// A real websocket handshake must not be redirected through HTTP admission.
type websocketTestEth struct{}

func (self *websocketTestEth) ChainId() hexutil.Uint64 { return 945 }

func TestEvmHttpDialPreservesWebsocketTransport(t *testing.T) {
	server := rpc.NewServer()
	defer server.Stop()
	if err := server.RegisterName("eth", &websocketTestEth{}); err != nil {
		t.Fatal(err)
	}
	host := httptest.NewServer(server.WebsocketHandler([]string{"*"}))
	defer host.Close()
	client, err := DialContext(t.Context(), "ws"+strings.TrimPrefix(host.URL, "http"))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	id, err := client.ChainID(t.Context())
	if err != nil || id == nil || id.Uint64() != 945 {
		t.Fatal(fmt.Errorf("websocket transport changed: %v %w", id, err))
	}
}
