// Fixed oracle groups own their retry partition without changing learned
// capacity for unrelated history work. All responses and timeouts are local.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/rpc"

	"github.com/urfoundation/sn/stabi"
)

func TestFleetRefreshOracleStateKeepsFullGroupAfterLearnedHistoryCapacity(t *testing.T) {
	t.Parallel()
	parsed, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		t.Fatal(err)
	}
	coordinator := stabi.NewSTCoordinator()
	immutable, active, pending := common.HexToAddress("0x1111"), common.HexToAddress("0x2222"), common.HexToAddress("0x3333")
	calls := [][]byte{coordinator.PackCurrentEpoch(), coordinator.PackCommitmentOracle(), coordinator.PackActiveCommitmentOracle(), coordinator.PackPendingCommitmentOracle(), coordinator.PackPendingCommitmentOracleEpoch()}
	outputs := map[string]string{
		hexutil.Encode(calls[0]): fleetRefreshTestOutput(t, parsed, "currentEpoch", big.NewInt(9)),
		hexutil.Encode(calls[1]): fleetRefreshTestOutput(t, parsed, "commitmentOracle", immutable),
		hexutil.Encode(calls[2]): fleetRefreshTestOutput(t, parsed, "activeCommitmentOracle", active),
		hexutil.Encode(calls[3]): fleetRefreshTestOutput(t, parsed, "pendingCommitmentOracle", pending),
		hexutil.Encode(calls[4]): fleetRefreshTestOutput(t, parsed, "pendingCommitmentOracleEpoch", uint64(10)),
	}
	server, recorder := newFleetRefreshRPCServer(t, outputs)
	defer server.Close()
	manager := fleetRefreshTestManager(t, server)
	capacity := evmReadBatchCapacities.forReads(manager.client.Client(), []evmRpcRead{{method: "eth_call"}})
	capacity.timedOut(2)
	address := common.HexToAddress("0x1234")
	state, err := readFleetRefreshOracleStateAt(t.Context(), manager, address, coordinator, 123)
	if err != nil || state.CurrentEpoch != 9 || state.Immutable != immutable || state.Active != active || state.Pending != pending || state.PendingEpoch != 10 {
		t.Fatalf("oracle state changed: state=%+v error=%v", state, err)
	}
	if count, widths, _ := recorder.snapshot(); count != 1 || !slices.Equal(widths, []int{5}) {
		t.Fatalf("oracle inherited unrelated history partition: requests=%d widths=%v", count, widths)
	}
	// The operation-local override must not escape back into the caller or
	// reset the existing hint. Ordinary work on this exact client keeps it.
	if _, err := rawCoordinatorBatchCallAt(t.Context(), manager, address, calls, 123); err != nil {
		t.Fatal(err)
	}
	count, widths, blocks := recorder.snapshot()
	if count != 4 || !slices.Equal(widths, []int{5, 2, 2, 1}) || capacity.begin(50).width != 2 {
		t.Fatalf("oracle changed ordinary history capacity: requests=%d widths=%v capacity=%d", count, widths, capacity.begin(50).width)
	}
	for _, block := range blocks {
		if block != "0x7b" {
			t.Fatalf("partition changed pinned block: %q", block)
		}
	}
}

func TestEvmReadBatchLocalCapacityTimeoutPreservesLaterPartitions(t *testing.T) {
	t.Parallel()
	var widths []int
	transport := newOwnedEvmRetryTransport(roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var batch []struct {
			ID     json.RawMessage   `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		err := json.NewDecoder(request.Body).Decode(&batch)
		request.Body.Close()
		if err != nil || len(batch) == 0 || request.Context().Value(ownedEvmRpcRetryBudgetKey{}) != true {
			t.Fatalf("batch ownership or encoding changed: size=%d error=%v", len(batch), err)
		}
		widths = append(widths, len(batch))
		responses := make([]map[string]any, len(batch))
		for index, call := range batch {
			var original int
			if call.Method != "eth_call" || len(call.Params) != 2 || json.Unmarshal(call.Params[0], &original) != nil || string(call.Params[1]) != `"0x7b"` {
				t.Fatalf("pinned request changed: %+v", call)
			}
			responses[index] = map[string]any{"jsonrpc": "2.0", "id": call.ID, "result": hexutil.Encode([]byte{byte(original + 1)})}
		}
		if len(widths) == 1 {
			return nil, context.DeadlineExceeded
		}
		body, err := json.Marshal(responses)
		if err != nil {
			t.Fatal(err)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	}))
	transport.policy.wait = func(context.Context, time.Duration) error { t.Fatal("HTTP multiplied the read owner"); return nil }
	client, err := rpc.DialOptions(t.Context(), "http://synthetic-rpc.example", rpc.WithHTTPClient(&http.Client{Transport: transport}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	local := context.WithValue(t.Context(), evmReadBatchLocalCapacityKey{}, true)
	for operation, count := range []int{5, 5, 8} {
		ctx := context.Context(local)
		if operation == 2 {
			ctx = t.Context()
		}
		results, err := readEvmRpcBatchForClientWithPolicy[hexutil.Bytes](ctx, "synthetic fixed group", adaptiveEvmBatchTestReads(count), immediateFinalSemanticRetryPolicy(), client)
		if err != nil || len(results) != count {
			t.Fatalf("operation%d failed: results=%d error=%v", operation, len(results), err)
		}
		for index, result := range results {
			if result.err != nil || !slices.Equal(result.value, []byte{byte(index + 1)}) {
				t.Fatalf("operation%d result%d changed: %+v", operation, index, result)
			}
		}
	}
	if !slices.Equal(widths, []int{5, 3, 2, 5, 8}) {
		t.Fatalf("local retry partition escaped its operation: widths=%v", widths)
	}
}

func TestEvmReadRpcBatchExpiredSuccessKeepsPendingReads(t *testing.T) {
	t.Parallel()
	deadline := &snapshotRetryDeadlineContext{Context: t.Context(), expired: make(chan struct{})}
	ctx := context.WithValue(deadline, ownedEvmRpcRetryBudgetKey{}, true)
	calls := 0
	results, err := readEvmRpcBatchWithPolicy[hexutil.Bytes](ctx, "late batch", adaptiveEvmBatchTestReads(5), immediateFinalSemanticRetryPolicy(), func(_ context.Context, batch []rpc.BatchElem) error {
		calls++
		for _, element := range batch {
			if len(*element.Result.(*hexutil.Bytes)) != 0 {
				t.Fatal("late attempt bytes survived into the next attempt")
			}
			*element.Result.(*hexutil.Bytes) = []byte{99}
		}
		close(deadline.expired)
		return nil
	})
	if err != nil || calls != 1 || len(results) != 5 {
		t.Fatalf("expired batch lost pending work: calls=%d results=%d error=%v", calls, len(results), err)
	}
	for index, result := range results {
		if !errors.Is(result.err, context.DeadlineExceeded) || len(result.value) != 0 {
			t.Fatalf("expired result%d was accepted: %+v", index, result)
		}
	}
}

func TestFleetRefreshOracleStateRejectsMissingContext(t *testing.T) {
	t.Parallel()
	if _, err := readFleetRefreshOracleStateAt(nil, nil, common.HexToAddress("0x1234"), stabi.NewSTCoordinator(), 123); err == nil {
		t.Fatal("oracle read accepted a missing context")
	}
}
