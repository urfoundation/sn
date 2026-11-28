// Real HTTP controls exercise the body boundary before geth's JSON decoder.
// Explicit callbacks and channels force cancellation without wall-clock races.
package validator

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
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
)

// An instance-local hook wraps a real transport or supplies a faulty body.
type chainHTTPTestRoundTripper func(*http.Request) (*http.Response, error)

func (self chainHTTPTestRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return self(request)
}

// Count downstream result decoding, not merely successful HTTP delivery.
type chainHTTPTestResult struct {
	value   string
	decodes int
}

func (self *chainHTTPTestResult) UnmarshalJSON(data []byte) error {
	self.decodes++
	return json.Unmarshal(data, &self.value)
}

// Each test owns its client and standard transport; no global overrides.
func chainHTTPTestRPC(t *testing.T, endpoint string, limit int64, decorate func(http.RoundTripper) http.RoundTripper) *gethrpc.Client {
	t.Helper()
	base := http.DefaultTransport.(*http.Transport).Clone()
	t.Cleanup(base.CloseIdleConnections)
	var transport http.RoundTripper = base
	if decorate != nil {
		transport = decorate(transport)
	}
	client, err := gethrpc.DialOptions(t.Context(), endpoint, gethrpc.WithHTTPClient(&http.Client{
		Transport: &chainHTTPTransport{base: transport, maxResponseBytes: limit},
	}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	return client
}

// Retain the actual request id so both the healthy and oversized replies are
// otherwise admissible to the real SDK, rather than failing for missing ids.
func chainHTTPTestReply(request *http.Request) ([]byte, error) {
	var input struct {
		ID json.RawMessage `json:"id"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"jsonrpc": "2.0", "id": input.ID, "result": "ok"})
}

func TestChainHTTPBoundAdmitsExactLimit(t *testing.T) {
	const limit = int64(1024)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := chainHTTPTestReply(request)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		body = append(body, bytes.Repeat([]byte(" "), int(limit)-len(body))...)
		writer.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = writer.Write(body)
	}))
	t.Cleanup(server.Close)
	client := chainHTTPTestRPC(t, server.URL, limit, nil)
	var result chainHTTPTestResult
	if err := client.CallContext(t.Context(), &result, "fixture_read"); err != nil || result.value != "ok" || result.decodes != 1 {
		t.Fatalf("exact HTTP limit lost valid JSON: value=%q decodes=%d error=%v", result.value, result.decodes, err)
	}
}

func TestChainHTTPBoundRejectsChunkedBeforeJSON(t *testing.T) {
	const limit = int64(1024)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := chainHTTPTestReply(request)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		_, _ = writer.Write(body)
		writer.(http.Flusher).Flush()
		_, _ = writer.Write(bytes.Repeat([]byte(" "), int(limit)+1-len(body)))
	}))
	t.Cleanup(server.Close)
	client := chainHTTPTestRPC(t, server.URL, limit, nil)
	var result chainHTTPTestResult
	if err := client.CallContext(t.Context(), &result, "fixture_read"); !errors.Is(err, errChainHTTPResponseTooLarge) || result.decodes != 0 {
		t.Fatalf("oversized chunked reply reached JSON: decodes=%d error=%v", result.decodes, err)
	}
}

func TestChainHTTPBoundRejectsCompressedExpansion(t *testing.T) {
	const limit = int64(1024)
	for _, automatic := range []bool{true, false} {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			body, err := chainHTTPTestReply(request)
			if err != nil {
				http.Error(writer, err.Error(), http.StatusBadRequest)
				return
			}
			var encoded bytes.Buffer
			compressed := gzip.NewWriter(&encoded)
			_, _ = compressed.Write(body)
			_, _ = compressed.Write(bytes.Repeat([]byte(" "), int(limit)+1-len(body)))
			if err := compressed.Close(); err != nil {
				http.Error(writer, err.Error(), http.StatusInternalServerError)
				return
			}
			writer.Header().Set("Content-Encoding", "gzip")
			writer.Header().Set("Content-Length", strconv.Itoa(encoded.Len()))
			_, _ = writer.Write(encoded.Bytes())
		}))
		t.Cleanup(server.Close)
		client := chainHTTPTestRPC(t, server.URL, limit, func(base http.RoundTripper) http.RoundTripper {
			base.(*http.Transport).DisableCompression = !automatic
			return base
		})
		var result chainHTTPTestResult
		if err := client.CallContext(t.Context(), &result, "fixture_read"); !errors.Is(err, errChainHTTPResponseTooLarge) || result.decodes != 0 {
			t.Fatalf("gzip expansion automatic=%t reached JSON: decodes=%d error=%v", automatic, result.decodes, err)
		}
	}
}

func TestChainHTTPBoundAdmitsExactCompressedLimit(t *testing.T) {
	const limit = int64(1024)
	for _, automatic := range []bool{true, false} {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			body, err := chainHTTPTestReply(request)
			if err != nil {
				http.Error(writer, err.Error(), http.StatusBadRequest)
				return
			}
			writer.Header().Set("Content-Encoding", "gzip")
			compressed := gzip.NewWriter(writer)
			_, _ = compressed.Write(body)
			_, _ = compressed.Write(bytes.Repeat([]byte(" "), int(limit)-len(body)))
			_ = compressed.Close()
		}))
		t.Cleanup(server.Close)
		client := chainHTTPTestRPC(t, server.URL, limit, func(base http.RoundTripper) http.RoundTripper {
			base.(*http.Transport).DisableCompression = !automatic
			return base
		})
		var result chainHTTPTestResult
		if err := client.CallContext(t.Context(), &result, "fixture_read"); err != nil || result.value != "ok" || result.decodes != 1 {
			t.Fatalf("exact gzip limit automatic=%t failed: value=%q decodes=%d error=%v", automatic, result.value, result.decodes, err)
		}
	}
}

func TestChainHTTPBoundRejectsEveryHTTPErrorBody(t *testing.T) {
	const limit = int64(1024)
	for _, status := range []int{http.StatusBadRequest, http.StatusTooManyRequests, http.StatusInternalServerError} {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			writer.WriteHeader(status)
			writer.(http.Flusher).Flush()
			_, _ = writer.Write(bytes.Repeat([]byte("x"), int(limit)+1))
		}))
		t.Cleanup(server.Close)
		client := chainHTTPTestRPC(t, server.URL, limit, nil)
		var result chainHTTPTestResult
		if err := client.CallContext(t.Context(), &result, "fixture_read"); !errors.Is(err, errChainHTTPResponseTooLarge) || result.decodes != 0 {
			t.Fatalf("HTTP %d error bypassed the response limit: decodes=%d error=%v", status, result.decodes, err)
		}
	}
}

// A readable body exposes the exact transport budget and close ownership.
type chainHTTPTestBody struct {
	reader     io.Reader
	reads      int
	bytesRead  int
	closes     int
	closeErr   error
	afterClose func()
}

func (self *chainHTTPTestBody) Read(data []byte) (int, error) {
	self.reads++
	n, err := self.reader.Read(data)
	self.bytesRead += n
	return n, err
}

func (self *chainHTTPTestBody) Close() error {
	self.closes++
	if self.afterClose != nil {
		self.afterClose()
	}
	return self.closeErr
}

func TestChainHTTPBoundRejectsDeclaredLengthBeforeRead(t *testing.T) {
	body := &chainHTTPTestBody{reader: strings.NewReader("must never be read")}
	transport := &chainHTTPTransport{maxResponseBytes: 64, base: chainHTTPTestRoundTripper(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, ContentLength: 65, Body: body}, nil
	})}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://fixture.invalid", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := transport.RoundTrip(request)
	if response != nil || !errors.Is(err, errChainHTTPResponseTooLarge) || body.reads != 0 || body.closes != 1 {
		t.Fatalf("declared limit ownership: response=%p reads=%d closes=%d error=%v", response, body.reads, body.closes, err)
	}
}

func TestChainHTTPBoundNeverDrainsRejectedBodies(t *testing.T) {
	body := &chainHTTPTestBody{reader: strings.NewReader(strings.Repeat("x", 4096))}
	transport := &chainHTTPTransport{maxResponseBytes: 64, base: chainHTTPTestRoundTripper(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, ContentLength: -1, Body: body}, nil
	})}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://fixture.invalid", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := transport.RoundTrip(request)
	if response != nil || !errors.Is(err, errChainHTTPResponseTooLarge) || body.bytesRead != 65 || body.closes != 1 {
		t.Fatalf("rejected body was drained: response=%p bytes=%d closes=%d error=%v", response, body.bytesRead, body.closes, err)
	}
}

func TestChainHTTPBoundRejectsInvalidOwnerBeforeTransport(t *testing.T) {
	calls := 0
	base := chainHTTPTestRoundTripper(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("unexpected transport")
	})
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://fixture.invalid", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, limit := range []int64{-1, 0, chainHTTPResponseLimit + 1, int64(^uint64(0) >> 1)} {
		transport := &chainHTTPTransport{base: base, maxResponseBytes: limit}
		if response, err := transport.RoundTrip(request); response != nil || err == nil {
			t.Fatalf("invalid HTTP limit %d admitted", limit)
		}
	}
	if calls != 0 {
		t.Fatalf("invalid owner reached transport %d times", calls)
	}
}

func TestChainHTTPBoundPreservesBodyAndTransportErrors(t *testing.T) {
	transportErr := errors.New("fixture transport error")
	closeErr := errors.New("fixture close error")
	for _, returnedErr := range []error{nil, transportErr} {
		body := &chainHTTPTestBody{reader: strings.NewReader("{}"), closeErr: closeErr}
		transport := &chainHTTPTransport{maxResponseBytes: 64, base: chainHTTPTestRoundTripper(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, ContentLength: 2, Body: body}, returnedErr
		})}
		request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://fixture.invalid", nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := transport.RoundTrip(request)
		if response != nil || !errors.Is(err, closeErr) || (returnedErr != nil && !errors.Is(err, transportErr)) || body.closes != 1 {
			t.Fatalf("HTTP error ownership lost: response=%p closes=%d error=%v", response, body.closes, err)
		}
	}
}

func TestChainHTTPBoundRejectsMalformedEncoding(t *testing.T) {
	for _, encoding := range []string{"gzip", "br", "gzip, gzip"} {
		body := &chainHTTPTestBody{reader: strings.NewReader("{}")}
		transport := &chainHTTPTransport{maxResponseBytes: 64, base: chainHTTPTestRoundTripper(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, ContentLength: 2, Header: http.Header{"Content-Encoding": []string{encoding}}, Body: body}, nil
		})}
		request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://fixture.invalid", nil)
		if err != nil {
			t.Fatal(err)
		}
		if response, err := transport.RoundTrip(request); response != nil || err == nil || body.closes != 1 {
			t.Fatalf("malformed encoding %q admitted: response=%p closes=%d error=%v", encoding, response, body.closes, err)
		}
	}
}

func TestChainHTTPBoundCancelsInflightBody(t *testing.T) {
	entered, joined := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer close(joined)
		if _, err := chainHTTPTestReply(request); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			close(entered)
			return
		}
		writer.WriteHeader(http.StatusOK)
		writer.(http.Flusher).Flush()
		close(entered)
		<-request.Context().Done()
	}))
	t.Cleanup(server.Close)
	client := chainHTTPTestRPC(t, server.URL, 1024, nil)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var result chainHTTPTestResult
	done := make(chan error, 1)
	go func() { done <- client.CallContext(ctx, &result, "fixture_read") }()
	<-entered
	cancel()
	err := <-done
	<-joined
	if !errors.Is(err, context.Canceled) || result.decodes != 0 {
		t.Fatalf("in-flight cancellation published result: decodes=%d error=%v", result.decodes, err)
	}
}

func TestChainHTTPBoundCancelsAfterCompleteBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := chainHTTPTestReply(request)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		_, _ = writer.Write(body)
	}))
	t.Cleanup(server.Close)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var closes atomic.Int64
	client := chainHTTPTestRPC(t, server.URL, 1024, func(base http.RoundTripper) http.RoundTripper {
		return chainHTTPTestRoundTripper(func(request *http.Request) (*http.Response, error) {
			response, err := base.RoundTrip(request)
			if err == nil {
				body := response.Body
				response.Body = &chainHTTPTestBody{reader: body, afterClose: func() {
					_ = body.Close()
					closes.Add(1)
					cancel()
				}}
			}
			return response, err
		})
	})
	var result chainHTTPTestResult
	if err := client.CallContext(ctx, &result, "fixture_read"); !errors.Is(err, context.Canceled) || result.decodes != 0 || closes.Load() != 1 {
		t.Fatalf("complete-body cancellation published result: decodes=%d closes=%d error=%v", result.decodes, closes.Load(), err)
	}
}

func TestDialChainHTTPBoundFailsOverOversizedDeclaredReply(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Length", strconv.FormatInt(chainHTTPResponseLimit+1, 10))
		writer.WriteHeader(http.StatusOK)
		writer.(http.Flusher).Flush()
	}))
	t.Cleanup(bad.Close)
	if client, err := DialChainContext(t.Context(), []string{bad.URL}, common.Address{}); client != nil || !errors.Is(err, errChainHTTPResponseTooLarge) {
		if client != nil {
			client.Close()
		}
		t.Fatalf("production dial bypassed declared response bound: %v", err)
	}
	good := jsonRpcStub(t, "0x3b1")
	t.Cleanup(good.Close)
	client, err := DialReleaseChainContext(t.Context(), []string{bad.URL, good.URL}, common.Address{1})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if client.RpcUrl() != good.URL || client.ChainId().Uint64() != 945 {
		t.Fatalf("oversized endpoint did not fail over to exact healthy identity")
	}
}

// The old SDK path decoded this complete JSON object, including its oversized
// unknown field, then returned a usable client. A valid chain id is not a byte
// budget, and waiting until ABI/result validation is too late for allocation.
func TestDialChainHTTPBoundRejectsChunkedProbe(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var input chainBatchRPCRequest
		if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		prefix := fmt.Sprintf("{\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":\"0x3b1\",\"padding\":\"", input.ID)
		_, _ = io.WriteString(writer, prefix)
		writer.(http.Flusher).Flush()
		_, _ = writer.Write(bytes.Repeat([]byte("x"), int(chainHTTPResponseLimit)+1-len(prefix)-2))
		_, _ = io.WriteString(writer, "\"}")
	}))
	t.Cleanup(server.Close)
	client, err := DialReleaseChainContext(t.Context(), []string{server.URL}, common.Address{1})
	if client != nil {
		client.Close()
	}
	if client != nil || !errors.Is(err, errChainHTTPResponseTooLarge) {
		t.Fatalf("oversized chunked probe reached usable client: client=%p error=%v", client, err)
	}
}

func TestDialChainRejectsCancellationAfterSuccessfulProbe(t *testing.T) {
	server := jsonRpcStub(t, "0x3b1")
	t.Cleanup(server.Close)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	endpoint := func(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
		child, finish := context.WithTimeout(parent, timeout)
		return child, func() { finish(); cancel() }
	}
	client, err := dialChainWithEndpointContext(ctx, []string{server.URL}, common.Address{1}, true, endpoint)
	if client != nil {
		client.Close()
	}
	if client != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("successful probe escaped parent cancellation: client=%p error=%v", client, err)
	}
}

// Cancel only after the real HTTP request and ABI decode have succeeded.
func chainHTTPTestCancellationAfterDecode(t *testing.T, exactHash bool) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var input chainBatchRPCRequest
		if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		value := "0x3b1"
		if input.Method == "eth_call" {
			value = "0x" + strings.Repeat("0", 63) + "7"
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": input.ID, "result": value})
	}))
	t.Cleanup(server.Close)
	client, err := DialReleaseChainContext(t.Context(), []string{server.URL}, common.Address{1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	blockHash := [32]byte{0x9a}
	if err := client.rememberBlockIdentity(7, blockHash); err != nil {
		t.Fatal(err)
	}
	read := func(ctx context.Context, decode func([]byte) (*big.Int, error)) (*big.Int, error) {
		if exactHash {
			return chainViewAtHashContext(ctx, client, 7, blockHash, client.coordinator.PackCurrentEpoch(), decode)
		}
		return chainViewAtContext(ctx, client, 7, client.coordinator.PackCurrentEpoch(), decode)
	}
	if value, err := read(t.Context(), client.coordinator.UnpackCurrentEpoch); err != nil || value == nil || value.Uint64() != 7 {
		t.Fatalf("healthy ABI prerequisite failed: value=%v error=%v", value, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	decoded := false
	value, err := read(ctx, func(data []byte) (*big.Int, error) {
		value, err := client.coordinator.UnpackCurrentEpoch(data)
		if err == nil {
			decoded = true
			cancel()
		}
		return value, err
	})
	if !decoded || value != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("successful ABI decode escaped cancellation: decoded=%t value=%v error=%v", decoded, value, err)
	}
}

func TestChainHTTPBoundExactViewCancelsAfterDecode(t *testing.T) {
	chainHTTPTestCancellationAfterDecode(t, true)
}

func TestChainHTTPBoundLegacyViewCancelsAfterDecode(t *testing.T) {
	chainHTTPTestCancellationAfterDecode(t, false)
}

// Both real evidence readers must clear typed publication outputs when a
// complete final batch loses its request owner before JSON handoff.
func TestChainHTTPBoundEvidenceCancellationClearsPublication(t *testing.T) {
	for _, commitment := range []bool{false, true} {
		fixture := newEvidenceChainFixture(t)
		if publication, err := fixture.readCommitment(t.Context()); err != nil || publication.Header != fixture.header {
			t.Fatalf("healthy evidence prerequisite: %v", err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		var batchCloses atomic.Int64
		rpcClient := chainHTTPTestRPC(t, fixture.chain.RpcUrl(), chainHTTPResponseLimit, func(base http.RoundTripper) http.RoundTripper {
			return chainHTTPTestRoundTripper(func(request *http.Request) (*http.Response, error) {
				copyBody, err := request.GetBody()
				if err != nil {
					return nil, err
				}
				wire, readErr := io.ReadAll(copyBody)
				closeErr := copyBody.Close()
				if err := errors.Join(readErr, closeErr); err != nil {
					return nil, err
				}
				response, err := base.RoundTrip(request)
				if err == nil && bytes.HasPrefix(wire, []byte("[")) {
					body := response.Body
					response.Body = &chainHTTPTestBody{reader: body, afterClose: func() {
						_ = body.Close()
						batchCloses.Add(1)
						cancel()
					}}
				}
				return response, err
			})
		})
		chain := &ChainClient{client: ethclient.NewClient(rpcClient), coordinator: fixture.chain.coordinator, chainId: fixture.chain.ChainId(), contractAddr: fixture.chain.contractAddr, release: true}
		var readErr error
		var nonzero bool
		if commitment {
			publication, err := chain.ValidatorEvidenceAtHashContext(ctx, fixture.journal, fixture.runtimeHash, fixture.activation, fixture.header, fixture.window, fixture.block, fixture.blockHash)
			readErr, nonzero = err, publication != (ValidatorEvidencePublication{})
		} else {
			publication, err := chain.ValidatorEvidenceActivationAtHashContext(ctx, fixture.journal, fixture.runtimeHash, fixture.activation, fixture.block, fixture.blockHash)
			readErr, nonzero = err, publication != (ValidatorEvidenceActivationPublication{})
		}
		cancel()
		if !errors.Is(readErr, context.Canceled) || nonzero || batchCloses.Load() != 1 {
			t.Fatalf("evidence cancellation commitment=%t retained publication=%t closes=%d error=%v", commitment, nonzero, batchCloses.Load(), readErr)
		}
	}
}

func TestChainHTTPBoundRejectsOversizedBatchBeforeAnyResult(t *testing.T) {
	const limit = int64(1024)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var requests []chainBatchRPCRequest
		if err := json.NewDecoder(request.Body).Decode(&requests); err != nil || len(requests) != 2 {
			http.Error(writer, "expected real two-call batch", http.StatusBadRequest)
			return
		}
		body, err := json.Marshal([]map[string]any{
			{"jsonrpc": "2.0", "id": requests[1].ID, "result": "ok"},
			{"jsonrpc": "2.0", "id": requests[0].ID, "result": "ok"},
		})
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		_, _ = writer.Write(body)
		writer.(http.Flusher).Flush()
		_, _ = writer.Write(bytes.Repeat([]byte(" "), int(limit)+1-len(body)))
	}))
	t.Cleanup(server.Close)
	client := chainHTTPTestRPC(t, server.URL, limit, nil)
	results := make([]chainHTTPTestResult, 2)
	batch := make([]gethrpc.BatchElem, 2)
	for index := range batch {
		batch[index] = gethrpc.BatchElem{Method: fmt.Sprintf("fixture_read_%d", index), Result: &results[index]}
	}
	err := client.BatchCallContext(t.Context(), batch)
	if !errors.Is(err, errChainHTTPResponseTooLarge) || results[0].decodes != 0 || results[1].decodes != 0 {
		t.Fatalf("oversized batch published partial values: results=%+v error=%v", results, err)
	}
}
