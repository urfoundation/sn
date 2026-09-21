package main

// Synthetic archive responses force a batch timeout without sleeping. The
// assertions cover request identity, retained progress and actual retry bounds.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
)

func adaptiveEvmBatchTestReads(count int) []evmRpcRead {
	reads := make([]evmRpcRead, count)
	for index := range reads {
		reads[index] = evmRpcRead{method: "eth_call", args: []any{index, "0x7b"}}
	}
	return reads
}

func TestEvmReadRpcBatchTimeoutSplitsPendingRecovery(t *testing.T) {
	t.Parallel()
	reads := adaptiveEvmBatchTestReads(50)
	wantWidths := []int{50, 25, 25, 13, 12}
	wantStarts := []int{0, 0, 25, 25, 38}
	var contexts []context.Context
	var delays []time.Duration
	policy := defaultFinalSemanticRPCRetryPolicy()
	policy.wait = func(ctx context.Context, delay time.Duration) error { delays = append(delays, delay); return ctx.Err() }
	results, err := readEvmRpcBatchWithPolicy[hexutil.Bytes](t.Context(), "synthetic archive batch", reads, policy, func(ctx context.Context, batch []rpc.BatchElem) error {
		step := len(contexts)
		if step >= len(wantWidths) || len(batch) != wantWidths[step] {
			t.Fatalf("archive batch was not split: request=%d width=%d", step, len(batch))
		}
		contexts = append(contexts, ctx)
		for index, element := range batch {
			original := wantStarts[step] + index
			if element.Method != reads[original].method || !slices.Equal(element.Args, reads[original].args) || len(*element.Result.(*hexutil.Bytes)) != 0 {
				t.Fatalf("pinned request changed or failed bytes survived: step=%d index=%d", step, index)
			}
			*element.Result.(*hexutil.Bytes) = []byte{byte(original + 1)}
		}
		if step == 0 || step == 2 {
			return context.DeadlineExceeded
		}
		return nil
	})
	if err != nil || len(contexts) != len(wantWidths) || !slices.Equal(delays, []time.Duration{time.Second, 2 * time.Second}) {
		t.Fatalf("split recovery: requests=%d delays=%v error=%v", len(contexts), delays, err)
	}
	if contexts[0] == contexts[1] || contexts[1] != contexts[2] || contexts[2] == contexts[3] || contexts[3] != contexts[4] {
		t.Fatal("chunks must share one attempt context; only retry rounds receive a fresh budget")
	}
	for index, result := range results {
		if result.err != nil || !slices.Equal(result.value, []byte{byte(index + 1)}) {
			t.Fatalf("result identity or completed prefix lost at %d: %+v", index, result)
		}
	}
}

func TestEvmReadRpcBatchAdaptiveExhaustionKeepsFourRoundBound(t *testing.T) {
	t.Parallel()
	var widths []int
	results, err := readEvmRpcBatchWithPolicy[hexutil.Bytes](t.Context(), "synthetic archive batch", adaptiveEvmBatchTestReads(50), immediateFinalSemanticRetryPolicy(), func(_ context.Context, batch []rpc.BatchElem) error {
		widths = append(widths, len(batch))
		return context.DeadlineExceeded
	})
	if err != nil || !slices.Equal(widths, []int{50, 25, 13, 7}) || len(results) != 50 {
		t.Fatalf("split multiplied the retry budget: widths=%v results=%d error=%v", widths, len(results), err)
	}
	for index, result := range results {
		if !evmReadRpcRetriesExhausted(result.err) || !errors.Is(result.err, context.DeadlineExceeded) || len(result.value) != 0 {
			t.Fatalf("unverified result %d escaped exhaustion: %+v", index, result)
		}
	}
}

func TestEvmReadRpcBatchSplitRetainsSuccessAndPermanentElements(t *testing.T) {
	t.Parallel()
	permanent := finalSemanticTestRPCError{code: 3, message: "execution reverted: synthetic integrity failure"}
	var requests [][]int
	results, err := readEvmRpcBatchWithPolicy[hexutil.Bytes](t.Context(), "synthetic archive batch", adaptiveEvmBatchTestReads(4), immediateFinalSemanticRetryPolicy(), func(_ context.Context, batch []rpc.BatchElem) error {
		indexes := make([]int, len(batch))
		for index, element := range batch {
			indexes[index] = element.Args[0].(int)
		}
		requests = append(requests, indexes)
		if len(requests) == 1 {
			return context.DeadlineExceeded
		}
		for index, element := range batch {
			original := element.Args[0].(int)
			if original == 1 {
				batch[index].Error = permanent
			} else if original == 2 && len(requests) == 3 {
				batch[index].Error = context.DeadlineExceeded
			} else {
				*element.Result.(*hexutil.Bytes) = []byte{byte(original + 1)}
			}
		}
		return nil
	})
	want := [][]int{{0, 1, 2, 3}, {0, 1}, {2, 3}, {2}}
	if err != nil || !slices.EqualFunc(requests, want, slices.Equal[[]int]) || len(results) != 4 {
		t.Fatalf("completed elements were repeated: requests=%v error=%v", requests, err)
	}
	for index, result := range results {
		if index == 1 {
			if !errors.Is(result.err, permanent) || len(result.value) != 0 {
				t.Fatalf("permanent element was retried or accepted: %+v", result)
			}
		} else if result.err != nil || !slices.Equal(result.value, []byte{byte(index + 1)}) {
			t.Fatalf("completed element %d lost: %+v", index, result)
		}
	}
}

func TestEvmReadRpcBatchDoesNotSplitUnrelatedTransientFailures(t *testing.T) {
	t.Parallel()
	for _, failure := range []error{
		syscall.ECONNRESET, io.ErrUnexpectedEOF, rpc.ErrMissingBatchResponse,
		rpc.HTTPError{StatusCode: http.StatusTooManyRequests},
		finalSemanticTestRPCError{code: -32005, message: "synthetic rate limit"},
	} {
		var widths []int
		results, err := readEvmRpcBatchWithPolicy[hexutil.Bytes](t.Context(), "synthetic archive batch", adaptiveEvmBatchTestReads(4), immediateFinalSemanticRetryPolicy(), func(_ context.Context, batch []rpc.BatchElem) error {
			widths = append(widths, len(batch))
			if len(widths) == 1 {
				return failure
			}
			for _, element := range batch {
				*element.Result.(*hexutil.Bytes) = []byte{1}
			}
			return nil
		})
		if err != nil || !slices.Equal(widths, []int{4, 4}) || len(results) != 4 {
			t.Fatalf("unrelated failure changed batch shape: failure=%v widths=%v error=%v", failure, widths, err)
		}
	}
}

func TestEvmReadRpcBatchSplitStopsOnHardFailureAndCancellation(t *testing.T) {
	t.Parallel()
	for _, cancelPrefix := range []bool{false, true} {
		ctx, cancel := context.WithCancel(t.Context())
		calls := 0
		permanent := finalSemanticTestRPCError{code: 3, message: "execution reverted: synthetic integrity failure"}
		results, err := readEvmRpcBatchWithPolicy[hexutil.Bytes](ctx, "synthetic archive batch", adaptiveEvmBatchTestReads(4), immediateFinalSemanticRetryPolicy(), func(_ context.Context, batch []rpc.BatchElem) error {
			calls++
			if calls == 1 {
				return context.DeadlineExceeded
			}
			if calls == 3 {
				return permanent
			}
			for _, element := range batch {
				*element.Result.(*hexutil.Bytes) = []byte{1}
			}
			if cancelPrefix {
				cancel()
			}
			return nil
		})
		cancel()
		wantCalls := 3
		var wantFailure error = permanent
		if cancelPrefix {
			wantCalls, wantFailure = 2, context.Canceled
		}
		if err != nil || calls != wantCalls || len(results) != 4 {
			t.Fatalf("hard outcome continued: canceled=%t calls=%d results=%d error=%v", cancelPrefix, calls, len(results), err)
		}
		for index, result := range results {
			if index < 2 {
				if result.err != nil || !slices.Equal(result.value, []byte{1}) {
					t.Fatalf("completed prefix lost at %d: %+v", index, result)
				}
			} else if !errors.Is(result.err, wantFailure) || len(result.value) != 0 {
				t.Fatalf("unverified suffix accepted at %d: %+v", index, result)
			}
		}
	}
}

func TestEvmReadRpcBatchSplitsExplicitElementTimeouts(t *testing.T) {
	t.Parallel()
	var widths []int
	results, err := readEvmRpcBatchWithPolicy[hexutil.Bytes](t.Context(), "synthetic archive batch", adaptiveEvmBatchTestReads(6), immediateFinalSemanticRetryPolicy(), func(_ context.Context, batch []rpc.BatchElem) error {
		widths = append(widths, len(batch))
		for index, element := range batch {
			if len(widths) == 1 {
				batch[index].Error = finalSemanticTestRPCError{code: -32002, message: "request timed out"}
				if index == 0 {
					// A different temporary code must not hide the later
					// element timeouts inside the aggregate retry error.
					batch[index].Error = finalSemanticTestRPCError{code: -32005, message: "server busy"}
				}
			} else {
				*element.Result.(*hexutil.Bytes) = []byte{byte(element.Args[0].(int) + 1)}
			}
		}
		return nil
	})
	if err != nil || !slices.Equal(widths, []int{6, 3, 3}) || len(results) != 6 {
		t.Fatalf("element timeout did not split: widths=%v error=%v", widths, err)
	}
	for index, result := range results {
		if result.err != nil || !slices.Equal(result.value, []byte{byte(index + 1)}) {
			t.Fatalf("element %d lost: %+v", index, result)
		}
	}
}

func TestCoordinatorBatchCallsSplitTimeoutsThroughOwnedTransport(t *testing.T) {
	t.Parallel()
	address := common.HexToAddress("0x1234")
	requests := make([]coordinatorCallAt, 90)
	for index := range requests {
		requests[index] = coordinatorCallAt{Address: address, Block: 123, Data: []byte{1, byte(index)}}
	}
	var widths []int
	transport := newOwnedEvmRetryTransport(roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var batch []struct {
			ID     json.RawMessage   `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		decodeErr := json.NewDecoder(request.Body).Decode(&batch)
		request.Body.Close()
		if decodeErr != nil || len(batch) == 0 || request.Context().Value(ownedEvmRpcRetryBudgetKey{}) != true {
			t.Fatalf("unexpected batch ownership or encoding: size=%d error=%v", len(batch), decodeErr)
		}
		widths = append(widths, len(batch))
		responses := make([]map[string]any, len(batch))
		for index, element := range batch {
			var target struct {
				To   common.Address `json:"to"`
				Data hexutil.Bytes  `json:"data"`
			}
			if element.Method != "eth_call" || len(element.Params) != 2 || string(element.Params[1]) != `"0x7b"` || json.Unmarshal(element.Params[0], &target) != nil || target.To != address || len(target.Data) != 2 || target.Data[0] != 1 {
				t.Fatalf("pinned request changed: %+v", element)
			}
			responses[index] = map[string]any{"jsonrpc": "2.0", "id": element.ID, "result": hexutil.Encode(target.Data)}
		}
		if len(batch) > 25 {
			return nil, context.DeadlineExceeded
		}
		body, err := json.Marshal(responses)
		if err != nil {
			t.Fatal(err)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	}))
	transport.policy.wait = func(context.Context, time.Duration) error {
		t.Fatal("HTTP transport multiplied retry rounds")
		return nil
	}
	rawClient, err := rpc.DialOptions(t.Context(), "http://synthetic-rpc.example", rpc.WithHTTPClient(&http.Client{Transport: transport}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rawClient.Close)
	outputs, err := rawCoordinatorBatchCallsAt(t.Context(), ethclient.NewClient(rawClient), requests)
	if err != nil || !slices.Equal(widths, []int{50, 25, 25, 40, 20, 20}) || len(outputs) != len(requests) {
		t.Fatalf("real coordinator split recovery: widths=%v outputs=%d error=%v", widths, len(outputs), err)
	}
	for index, output := range outputs {
		if !slices.Equal(output, requests[index].Data) {
			t.Fatalf("coordinator output %d changed: %x", index, output)
		}
	}
}
