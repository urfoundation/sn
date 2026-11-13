// Validate response identities before geth decodes a result. Its HTTP adapter
// does not check singleton ids and silently ignores extra batch responses.
package validator

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	gethrpc "github.com/ethereum/go-ethereum/rpc"
)

// A well-formed result cannot authorize a foreign or incomplete envelope.
func TestChainHttpEnvelopeRejectsForeignSingleton(t *testing.T) {
	for _, fault := range []string{"id", "version", "missing-version", "both", "trailing"} {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			var call chainBatchRPCRequest
			if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
				t.Error(err)
				return
			}
			reply := map[string]any{"jsonrpc": "2.0", "id": call.ID, "result": "ok"}
			switch fault {
			case "id":
				reply["id"] = 999
			case "version":
				reply["jsonrpc"] = "1.0"
			case "missing-version":
				delete(reply, "jsonrpc")
			case "both":
				reply["error"] = nil
			}
			_ = json.NewEncoder(writer).Encode(reply)
			if fault == "trailing" {
				_ = json.NewEncoder(writer).Encode(reply)
			}
		}))
		t.Cleanup(server.Close)
		client := chainHTTPTestRPC(t, server.URL, chainHTTPResponseLimit, nil)
		var result chainHTTPTestResult
		err := client.CallContext(t.Context(), &result, "fixture_read")
		if err == nil || result.decodes != 0 || RetryableEvidenceTransportError(err) {
			t.Errorf("%s envelope reached result decoder: result=%+v error=%v", fault, result, err)
		}
	}
}

// Duplicate and unknown ids must not be discarded around otherwise usable
// responses. A complete census must be unambiguous before any result decode.
func TestChainHttpEnvelopeRejectsAmbiguousBatch(t *testing.T) {
	for _, fault := range []string{"duplicate", "unknown", "version", "null"} {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			var calls []chainBatchRPCRequest
			if err := json.NewDecoder(request.Body).Decode(&calls); err != nil || len(calls) != 2 {
				t.Errorf("invalid batch request: %v", err)
				return
			}
			first := map[string]any{"jsonrpc": "2.0", "id": calls[0].ID, "result": "ok"}
			second := map[string]any{"jsonrpc": "2.0", "id": calls[1].ID, "result": "ok"}
			responses := []any{second, first}
			switch fault {
			case "duplicate":
				responses = append(responses, map[string]any{"jsonrpc": "2.0", "id": calls[0].ID, "result": "conflicting"})
			case "unknown":
				responses = append(responses, map[string]any{"jsonrpc": "2.0", "id": 999, "result": "foreign"})
			case "version":
				first["jsonrpc"] = "1.0"
			case "null":
				responses = append(responses, nil)
			}
			_ = json.NewEncoder(writer).Encode(responses)
		}))
		t.Cleanup(server.Close)
		client := chainHTTPTestRPC(t, server.URL, chainHTTPResponseLimit, nil)
		results := make([]chainHTTPTestResult, 2)
		batch := make([]gethrpc.BatchElem, 2)
		for index := range batch {
			batch[index] = gethrpc.BatchElem{Method: fmt.Sprintf("fixture_read_%d", index), Result: &results[index]}
		}
		err := client.BatchCallContext(t.Context(), batch)
		if err == nil || results[0].decodes != 0 || results[1].decodes != 0 || RetryableEvidenceTransportError(err) {
			t.Errorf("%s batch reached result decoders: result=%+v error=%v", fault, results, err)
		}
	}
}

// The bounded transport must refuse at the original response so a client's
// automatic redirect policy cannot contact another node behind its owner.
func TestChainHttpRefusesRedirectedEndpoint(t *testing.T) {
	for _, status := range []int{http.StatusFound, http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		var targetCalls atomic.Int64
		target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			targetCalls.Add(1)
			_, _ = writer.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"ok"}`))
		}))
		t.Cleanup(target.Close)
		source := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			http.Redirect(writer, request, target.URL, status)
		}))
		t.Cleanup(source.Close)
		client := chainHTTPTestRPC(t, source.URL, chainHTTPResponseLimit, nil)
		var result chainHTTPTestResult
		err := client.CallContext(t.Context(), &result, "fixture_read")
		if err == nil || result.decodes != 0 || targetCalls.Load() != 0 || RetryableEvidenceTransportError(err) {
			t.Errorf("redirect changed RPC authority: status=%d result=%+v target-calls=%d error=%v", status, result, targetCalls.Load(), err)
		}
	}
}

// Checking outer identities alone must not permit duplicate nested result
// fields to replace an authenticated block number, hash or runtime identity.
func TestChainHttpEnvelopeRejectsDuplicateJsonKeys(t *testing.T) {
	for _, raw := range []string{
		`{"jsonrpc":"1.0","jsonrpc":"2.0","id":REQUEST_ID,"result":"ok"}`,
		`{"jsonrpc":"2.0","id":999,"i\u0064":REQUEST_ID,"result":"ok"}`,
		`{"jsonrpc":"2.0","id":999,"Id":REQUEST_ID,"result":"ok"}`,
		`{"jsonrpc":"2.0","id":REQUEST_ID,"result":"conflicting","reſult":"ok"}`,
		`{"jsonrpc":"2.0","id":REQUEST_ID,"result":{"number":1,"number":2}}`,
		`{"jsonrpc":"2.0","id":REQUEST_ID,"result":{"number":1,"Number":2}}`,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			var call chainBatchRPCRequest
			if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
				t.Error(err)
				return
			}
			_, _ = writer.Write([]byte(strings.ReplaceAll(raw, "REQUEST_ID", string(call.ID))))
		}))
		t.Cleanup(server.Close)
		client := chainHTTPTestRPC(t, server.URL, chainHTTPResponseLimit, nil)
		var result json.RawMessage
		err := client.CallContext(t.Context(), &result, "fixture_read")
		if err == nil || len(result) != 0 || RetryableEvidenceTransportError(err) {
			t.Errorf("ambiguous keys reached result decoder: result=%s error=%v", result, err)
		}
	}
}
