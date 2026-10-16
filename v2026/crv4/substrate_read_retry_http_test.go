// Real configured native HTTP clients preserve transport origin through GSRPC.
// Virtual retry pacing owns time; deterministic body/cancel barriers own order.
package crv4

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	gsrpcgeth "github.com/centrifuge/go-substrate-rpc-client/v4/gethrpc"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// Every synthetic request uses the same production dial and allowlisted owner.
// Request history is synchronized independently of HTTP connection scheduling.
type substrateReadHttpFixture struct {
	stateLock sync.Mutex
	requests  []chainContextRPCRequest
	client    *contextSubstrateClient
	server    *httptest.Server
}

// Tests inject physical responses after decoding the actual GSRPC request.
func newSubstrateReadHttpFixture(t *testing.T, reply func(http.ResponseWriter, *http.Request, chainContextRPCRequest, int)) *substrateReadHttpFixture {
	t.Helper()
	f := &substrateReadHttpFixture{}
	f.server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var call chainContextRPCRequest
		if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
			t.Errorf("native HTTP fixture request: %v", err)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		f.stateLock.Lock()
		f.requests = append(f.requests, call)
		number := len(f.requests)
		f.stateLock.Unlock()
		reply(writer, request, call, number)
	}))
	client, err := dialContextSubstrateClient(t.Context(), f.server.URL)
	if err != nil {
		f.server.Close()
		t.Fatal(err)
	}
	f.client = client
	t.Cleanup(func() {
		f.client.Close()
		f.server.Close()
	})
	return f
}

// Copy counters after a completed invocation without relying on socket timing.
func (self *substrateReadHttpFixture) snapshot() []chainContextRPCRequest {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return append([]chainContextRPCRequest(nil), self.requests...)
}

// Marshal an ordinary complete JSON-RPC response with the actual request id.
func substrateReadHttpReply(t *testing.T, call chainContextRPCRequest) []byte {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": call.ID, "result": "0x2a00"})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// Repeated 500/502 refusals replay the original pinned request through the real
// configured adapter; a recovered reply ends the same finite retry owner.
func TestSubstrateReadHttpStatusRecoversPinnedRequest(t *testing.T) {
	f := newSubstrateReadHttpFixture(t, func(writer http.ResponseWriter, _ *http.Request, call chainContextRPCRequest, number int) {
		if number <= 2 {
			status := http.StatusInternalServerError
			if number == 2 {
				status = http.StatusBadGateway
			}
			writer.WriteHeader(status)
			_, _ = writer.Write([]byte("synthetic upstream unavailable"))
			return
		}
		_, _ = writer.Write(substrateReadHttpReply(t, call))
	})
	budget, hooks := newSubstrateReadTestBudget(t, 75*time.Second)
	f.client.readRetry = hooks
	block := types.Hash{47}
	var result string
	err := f.client.CallContext(t.Context(), &result, "state_getStorage", "0x0102", block.Hex())
	requests := f.snapshot()
	if err != nil || result != "0x2a00" || len(requests) != 3 || budget.attempts != 3 || budget.elapsed != 150*time.Second {
		t.Fatalf("configured native HTTP status recovery failed: result=%q calls=%d attempts=%d elapsed=%s err=%v", result, len(requests), budget.attempts, budget.elapsed, err)
	}
	for _, call := range requests {
		if call.Method != "state_getStorage" || len(call.Params) != 2 || string(call.Params[0]) != `"0x0102"` || string(call.Params[1]) != fmt.Sprintf("%q", block.Hex()) {
			t.Fatalf("HTTP retry changed pinned native request: %+v", call)
		}
	}
}

// Exhaustion retains typed physical status and the shared five-minute deadline;
// no early attempt ceiling or fresh budget is introduced by the HTTP adapter.
func TestSubstrateReadHttpFailureExhaustsSharedBudget(t *testing.T) {
	f := newSubstrateReadHttpFixture(t, func(writer http.ResponseWriter, _ *http.Request, _ chainContextRPCRequest, _ int) {
		writer.WriteHeader(http.StatusBadGateway)
	})
	budget, hooks := newSubstrateReadTestBudget(t, 75*time.Second)
	f.client.readRetry = hooks
	var result string
	err := f.client.CallContext(t.Context(), &result, "chain_getFinalizedHead")
	var status *SubstrateReadHttpStatusError
	if !errors.Is(err, context.DeadlineExceeded) || !errors.As(err, &status) || status.StatusCode() != http.StatusBadGateway || !RetryableSubstrateReadTransportError(err) || len(f.snapshot()) != 4 || budget.attempts != 4 || budget.elapsed != 300*time.Second {
		t.Fatalf("HTTP exhaustion lost physical status or full bounded owner: result=%q calls=%d attempts=%d elapsed=%s err=%v", result, len(f.snapshot()), budget.attempts, budget.elapsed, err)
	}
	for _, hard := range []error{errors.Join(err, errors.New("canonical header hash differs")), errors.Join(err, context.Canceled), &os.PathError{Op: "read", Path: "synthetic-custody", Err: err}, errors.New(status.Error()), io.EOF, io.ErrUnexpectedEOF, context.DeadlineExceeded} {
		if RetryableSubstrateReadTransportError(hard) {
			t.Fatalf("unowned or mixed error gained native transport authority: %v", hard)
		}
	}
}

// Empty HTTP success and genuinely truncated framing retain physical origin.
// Even a complete JSON prefix cannot conceal a later failed body read.
func TestSubstrateReadHttpBodiesRecoverPhysicalFailure(t *testing.T) {
	for _, kind := range []string{"empty", "partial-json", "complete-json-prefix"} {
		f := newSubstrateReadHttpFixture(t, func(writer http.ResponseWriter, _ *http.Request, call chainContextRPCRequest, number int) {
			if number == 1 {
				if kind == "empty" {
					writer.WriteHeader(http.StatusOK)
					return
				}
				body := []byte(`{"jsonrpc":`)
				if kind == "complete-json-prefix" {
					body = substrateReadHttpReply(t, call)
				}
				writer.Header().Set("Content-Length", strconv.Itoa(len(body)+17))
				_, _ = writer.Write(body)
				return
			}
			_, _ = writer.Write(substrateReadHttpReply(t, call))
		})
		budget, hooks := newSubstrateReadTestBudget(t, 75*time.Second)
		f.client.readRetry = hooks
		var result string
		err := f.client.CallContext(t.Context(), &result, "chain_getFinalizedHead")
		if err != nil || result != "0x2a00" || len(f.snapshot()) != 2 || budget.attempts != 2 || budget.elapsed != 75*time.Second {
			t.Fatalf("physical %s body did not retry before decoding: result=%q calls=%d elapsed=%s err=%v", kind, result, len(f.snapshot()), budget.elapsed, err)
		}
	}
}

// Nonempty complete malformed bodies are decoder failures, while permanent
// JSON-RPC errors retain their structured code even if they mention timeouts.
func TestSubstrateReadHttpMalformedCompleteBodiesStayHard(t *testing.T) {
	for _, kind := range []string{"partial-json", "syntax", "wrong-type", "rpc-error"} {
		f := newSubstrateReadHttpFixture(t, func(writer http.ResponseWriter, _ *http.Request, call chainContextRPCRequest, _ int) {
			body := []byte(`{"jsonrpc":`)
			switch kind {
			case "syntax":
				body = []byte(`{"jsonrpc":broken}`)
			case "wrong-type":
				body = bytes.Replace(substrateReadHttpReply(t, call), []byte(`"0x2a00"`), []byte(`{}`), 1)
			case "rpc-error":
				body = []byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"error":{"code":-32602,"message":"canonical mismatch: timeout"}}`, call.ID))
			}
			writer.Header().Set("Content-Length", strconv.Itoa(len(body)))
			_, _ = writer.Write(body)
		})
		budget, hooks := newSubstrateReadTestBudget(t, 75*time.Second)
		f.client.readRetry = hooks
		var result string
		err := f.client.CallContext(t.Context(), &result, "chain_getFinalizedHead")
		if err == nil || RetryableSubstrateReadTransportError(err) || substrateRPCDisconnected(err) || len(f.snapshot()) != 1 || budget.attempts != 1 || budget.elapsed != 0 {
			t.Fatalf("complete malformed %s became a physical retry: calls=%d elapsed=%s err=%v", kind, len(f.snapshot()), budget.elapsed, err)
		}
		if kind == "rpc-error" {
			var rpcError gsrpcgeth.Error
			if !errors.As(err, &rpcError) || rpcError.ErrorCode() != -32602 {
				t.Fatalf("permanent JSON-RPC cause was erased: %v", err)
			}
		}
	}
}

// A refusal outside the explicit transient class never redirects or retries,
// regardless of a response body's chosen diagnostic words.
func TestSubstrateReadHttpPermanentStatusNeverRetries(t *testing.T) {
	for _, code := range []int{http.StatusMovedPermanently, http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound} {
		f := newSubstrateReadHttpFixture(t, func(writer http.ResponseWriter, _ *http.Request, _ chainContextRPCRequest, _ int) {
			writer.Header().Set("Location", "/synthetic-redirect")
			writer.WriteHeader(code)
			_, _ = writer.Write([]byte("timeout: 502 Bad Gateway"))
		})
		budget, hooks := newSubstrateReadTestBudget(t, 75*time.Second)
		f.client.readRetry = hooks
		var result string
		err := f.client.CallContext(t.Context(), &result, "chain_getFinalizedHead")
		var status *SubstrateReadHttpStatusError
		if !errors.As(err, &status) || status.StatusCode() != code || RetryableSubstrateReadTransportError(err) || len(f.snapshot()) != 1 || budget.attempts != 1 || budget.elapsed != 0 {
			t.Fatalf("permanent HTTP status was retried or flattened: status=%d calls=%d elapsed=%s err=%v", code, len(f.snapshot()), budget.elapsed, err)
		}
	}
}

// An explicit response-body barrier proves cancellation interrupts the actual
// physical read and prevents another request under the shared budget.
func TestSubstrateReadHttpCancellationStopsBodyAndBudget(t *testing.T) {
	entered, stopped := make(chan struct{}), make(chan struct{})
	f := newSubstrateReadHttpFixture(t, func(writer http.ResponseWriter, request *http.Request, _ chainContextRPCRequest, _ int) {
		writer.Header().Set("Content-Length", "1000")
		_, _ = writer.Write([]byte(`{"jsonrpc":`))
		writer.(http.Flusher).Flush()
		close(entered)
		<-request.Context().Done()
		close(stopped)
	})
	budget, hooks := newSubstrateReadTestBudget(t, 75*time.Second)
	f.client.readRetry = hooks
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		var result string
		done <- f.client.CallContext(ctx, &result, "chain_getFinalizedHead")
	}()
	<-entered
	cancel()
	err := <-done
	<-stopped
	if !errors.Is(err, context.Canceled) || RetryableSubstrateReadTransportError(err) || len(f.snapshot()) != 1 || budget.attempts != 1 || budget.elapsed != 0 {
		t.Fatalf("canceled physical read retried or lost cancellation: calls=%d elapsed=%s err=%v", len(f.snapshot()), budget.elapsed, err)
	}
}

// Writes and unclassified methods never receive a read-origin marker or an
// attempt owner, even when the same configured endpoint returns a retry status.
func TestSubstrateReadHttpNeverRetriesWritesOrUnknownMethods(t *testing.T) {
	for _, method := range []string{"author_submitExtrinsic", "author_submitAndWatchExtrinsic", "synthetic_unknown_read"} {
		f := newSubstrateReadHttpFixture(t, func(writer http.ResponseWriter, _ *http.Request, _ chainContextRPCRequest, _ int) {
			writer.WriteHeader(http.StatusServiceUnavailable)
		})
		budget, hooks := newSubstrateReadTestBudget(t, 75*time.Second)
		f.client.readRetry = hooks
		var result string
		err := f.client.CallContext(t.Context(), &result, method, "0x0102")
		if err == nil || RetryableSubstrateReadTransportError(err) || len(f.snapshot()) != 1 || budget.attempts != 0 || budget.owner != nil {
			t.Fatalf("nonread %s gained native retry authority: calls=%d attempts=%d err=%v", method, len(f.snapshot()), budget.attempts, err)
		}
	}
}

// Real high-level initialization must use the decorated constructor too; the
// first metadata status failure recovers before genesis/runtime initialization.
func TestSubstrateReadHttpDialChainRecoversInitialization(t *testing.T) {
	metadata, _ := runtimeIdentityTestMetadata(t)
	genesis := types.Hash{29}
	var stateLock sync.Mutex
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var call chainContextRPCRequest
		if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
			t.Errorf("initialization request: %v", err)
			return
		}
		stateLock.Lock()
		methods = append(methods, call.Method)
		number := len(methods)
		stateLock.Unlock()
		if number == 1 {
			writer.WriteHeader(http.StatusBadGateway)
			return
		}
		switch call.Method {
		case "state_getMetadata":
			writeChainContextRPCResult(writer, call, metadata)
		case "chain_getBlockHash":
			writeChainContextRPCResult(writer, call, genesis.Hex())
		case "state_getRuntimeVersion":
			writeChainContextRPCResult(writer, call, map[string]any{"apis": []any{}, "authoringVersion": 1, "implName": "synthetic", "implVersion": 1, "specName": "synthetic", "specVersion": 1, "transactionVersion": 1})
		default:
			t.Errorf("unexpected initialization method %s", call.Method)
		}
	}))
	defer server.Close()
	chain, err := DialChainContext(t.Context(), server.URL)
	if err != nil {
		t.Fatalf("production initialization flattened native HTTP status: %v", err)
	}
	defer chain.API.Client.Close()
	stateLock.Lock()
	sequence := strings.Join(methods, ",")
	stateLock.Unlock()
	if chain.GenesisHash != genesis || sequence != "state_getMetadata,state_getMetadata,chain_getBlockHash,state_getRuntimeVersion" {
		t.Fatalf("recovered initialization changed identity or read sequence: %s", sequence)
	}
}
