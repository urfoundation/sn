// Recover exact-block reads at the real geth HTTP boundary. Explicit response
// faults establish the retry order without timing or live-node dependencies.
package validator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/ethclient"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
)

// Decode the actual request and authenticate its pinned selector before the
// fixture can return a result. Replies retain the transport's generated ids.
func chainReadRetryRequests(t *testing.T, request *http.Request) ([]chainBatchRPCRequest, bool) {
	t.Helper()
	raw, err := io.ReadAll(request.Body)
	if err != nil {
		t.Fatal(err)
	}
	var requests []chainBatchRPCRequest
	batch := bytes.HasPrefix(raw, []byte("["))
	if batch {
		err = json.Unmarshal(raw, &requests)
	} else {
		var call chainBatchRPCRequest
		err = json.Unmarshal(raw, &call)
		requests = append(requests, call)
	}
	if err != nil || len(requests) == 0 {
		t.Fatalf("invalid rpc request: %s %v", raw, err)
	}
	for _, call := range requests {
		var selector gethrpc.BlockNumberOrHash
		if call.Method != "eth_call" || len(call.Params) != 2 || json.Unmarshal(call.Params[1], &selector) != nil || selector.BlockHash == nil || *selector.BlockHash != common.Hash(chainBatchTestBlockHash) || !selector.RequireCanonical {
			t.Fatalf("read changed its canonical block: %+v", call)
		}
	}
	return requests, batch
}

// The synthetic one-byte calldata is also its result, making ordering and
// unintended repetition visible independently of a generated ABI decoder.
func chainReadRetryReply(t *testing.T, call chainBatchRPCRequest) map[string]any {
	t.Helper()
	var arguments struct {
		Input hexutil.Bytes `json:"input"`
	}
	if err := json.Unmarshal(call.Params[0], &arguments); err != nil || len(arguments.Input) != 1 {
		t.Fatalf("invalid call input: %s %v", call.Params[0], err)
	}
	return map[string]any{"jsonrpc": "2.0", "id": call.ID, "result": hexutil.Encode(arguments.Input)}
}

// Install the production bounded transport over an instance-owned fault
// source, then retain the already authenticated block-number/hash pair.
func chainReadRetryClient(t *testing.T, read func(*http.Request) (*http.Response, error)) *ChainClient {
	t.Helper()
	client := chainHTTPTestRPC(t, "http://read-retry.invalid", chainHTTPResponseLimit, func(http.RoundTripper) http.RoundTripper {
		return chainHTTPTestRoundTripper(read)
	})
	chain := &ChainClient{client: ethclient.NewClient(client)}
	if err := chain.rememberBlockIdentity(123, chainBatchTestBlockHash); err != nil {
		t.Fatal(err)
	}
	return chain
}

// Each attempt owns and closes a complete response before the next attempt.
func chainReadRetryResponse(t *testing.T, status int, value any) *http.Response {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Response{StatusCode: status, Status: fmt.Sprintf("%d fixture", status), Header: make(http.Header), ContentLength: int64(len(raw)), Body: io.NopCloser(bytes.NewReader(raw))}
}

// A transport refusal must not force a new steering snapshot or process.
func TestChainReadRetryKeepsCanonicalSingleton(t *testing.T) {
	calls := 0
	chain := chainReadRetryClient(t, func(request *http.Request) (*http.Response, error) {
		requests, batch := chainReadRetryRequests(t, request)
		if batch || len(requests) != 1 {
			t.Fatal("singleton changed its request shape")
		}
		calls++
		if calls == 1 {
			return chainReadRetryResponse(t, http.StatusServiceUnavailable, "capacity"), nil
		}
		return chainReadRetryResponse(t, http.StatusOK, chainReadRetryReply(t, requests[0])), nil
	})
	output, err := chain.ethCallAtHashContext(t.Context(), common.Address{1}, []byte{7}, 123, chainBatchTestBlockHash)
	if err != nil || !bytes.Equal(output, []byte{7}) || calls != 2 {
		t.Fatalf("canonical read did not recover: output=%x calls=%d error=%v", output, calls, err)
	}
}

// Capacity refusal splits only the failed group. Smaller successful groups
// keep their authenticated values while their neighboring group retries.
func TestChainReadRetrySplitsRefusedBatch(t *testing.T) {
	var sizes []int
	chain := chainReadRetryClient(t, func(request *http.Request) (*http.Response, error) {
		requests, batch := chainReadRetryRequests(t, request)
		if !batch {
			t.Fatal("batch changed transport shape")
		}
		sizes = append(sizes, len(requests))
		if len(requests) > 2 {
			return chainReadRetryResponse(t, http.StatusServiceUnavailable, "capacity"), nil
		}
		responses := make([]map[string]any, len(requests))
		for index, call := range requests {
			responses[len(responses)-1-index] = chainReadRetryReply(t, call)
		}
		return chainReadRetryResponse(t, http.StatusOK, responses), nil
	})
	calls := make([]chainBatchCall, 5)
	want := make([][]byte, len(calls))
	for index := range calls {
		want[index] = []byte{byte(index + 1)}
		calls[index] = chainBatchCall{address: common.Address{1}, calldata: want[index]}
	}
	outputs, err := chain.batchCallsAtHashContext(t.Context(), 123, chainBatchTestBlockHash, calls)
	if err != nil || !reflect.DeepEqual(outputs, want) || !reflect.DeepEqual(sizes, []int{5, 2, 3, 1, 2}) {
		t.Fatalf("failed batch did not split in place: outputs=%x sizes=%v error=%v", outputs, sizes, err)
	}
}

// A missing reply grants no value, but an authenticated sibling reply should
// not be requested again. Response order is deliberately reversed.
func TestChainReadRetryRetainsSuccessfulBatchMembers(t *testing.T) {
	var sizes []int
	seen := make(map[string]int)
	chain := chainReadRetryClient(t, func(request *http.Request) (*http.Response, error) {
		requests, _ := chainReadRetryRequests(t, request)
		sizes = append(sizes, len(requests))
		var responses []map[string]any
		for _, call := range requests {
			reply := chainReadRetryReply(t, call)
			value := reply["result"].(string)
			seen[value]++
			if len(sizes) == 1 && value == "0x02" {
				continue
			}
			responses = append([]map[string]any{reply}, responses...)
		}
		return chainReadRetryResponse(t, http.StatusOK, responses), nil
	})
	calls := []chainBatchCall{{address: common.Address{1}, calldata: []byte{1}}, {address: common.Address{1}, calldata: []byte{2}}, {address: common.Address{1}, calldata: []byte{3}}}
	outputs, err := chain.batchCallsAtHashContext(t.Context(), 123, chainBatchTestBlockHash, calls)
	if err != nil || !reflect.DeepEqual(outputs, [][]byte{{1}, {2}, {3}}) || !reflect.DeepEqual(sizes, []int{3, 1}) || !reflect.DeepEqual(seen, map[string]int{"0x01": 1, "0x02": 2, "0x03": 1}) {
		t.Fatalf("batch recovery discarded successful members: outputs=%x sizes=%v seen=%v error=%v", outputs, sizes, seen, err)
	}
}
