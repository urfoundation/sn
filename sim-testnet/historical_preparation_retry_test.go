package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
)

func TestHistoricalPreparationContinuesExhaustedReadWithoutOpeningGate(t *testing.T) {
	exhausted := &evmReadRpcExhaustedError{operation: "synthetic pinned read", attempts: 4, attemptTimeout: 30 * time.Second, cause: context.DeadlineExceeded}
	reads, waits, applications := 0, 0, 0
	err := continueHistoricalPreparationWithWait(t.Context(), func(context.Context) error {
		reads++
		if applications != 0 {
			t.Fatal("action gate opened before historical verification completed")
		}
		if reads == 1 {
			return fmt.Errorf("synthetic retained action: %w", exhausted)
		}
		return nil
	}, func(ctx context.Context, delay time.Duration) error {
		waits++
		if delay != 5*time.Second || reads != 1 || ctx.Err() != nil {
			t.Fatalf("unexpected continuation boundary: reads=%d delay=%s error=%v", reads, delay, ctx.Err())
		}
		return nil
	})
	report := &launchPreparationReport{Ready: true}
	report.add("carried history", err)
	err = finishLaunchPreparation(report, func(_ *launchPreparationReport, err error) error { return err }, func() error { applications++; return nil })
	if err != nil || reads != 2 || waits != 1 || applications != 1 || evmReadRpcErrorIsTransient(exhausted) {
		t.Fatalf("continuation reads/waits/applications=%d/%d/%d error=%v", reads, waits, applications, err)
	}
}

func TestHistoricalPreparationCancellationRetainsUnresolvedFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var delays []time.Duration
	reads := 0
	exhausted := &evmReadRpcExhaustedError{operation: "synthetic retained read", attempts: 4, cause: context.DeadlineExceeded}
	err := continueHistoricalPreparationWithWait(ctx, func(context.Context) error { reads++; return exhausted }, func(ctx context.Context, delay time.Duration) error {
		delays = append(delays, delay)
		if len(delays) == 5 {
			cancel()
		}
		return ctx.Err()
	})
	if reads != 5 || !errors.Is(err, context.Canceled) || !errors.Is(err, context.DeadlineExceeded) || !slices.Equal(delays, []time.Duration{5 * time.Second, 10 * time.Second, 20 * time.Second, 30 * time.Second, 30 * time.Second}) {
		t.Fatalf("canceled continuation reads=%d delays=%v error=%v", reads, delays, err)
	}
}

func TestHistoricalPreparationNeverDefersPermanentOrMixedFailures(t *testing.T) {
	exhausted := &evmReadRpcExhaustedError{operation: "synthetic pinned read", attempts: 4, cause: context.DeadlineExceeded}
	for _, failure := range []error{
		errors.New("synthetic proof timeout digest mismatch"),
		errors.Join(exhausted, errors.New("synthetic changed canonical hash")),
		fmt.Errorf("synthetic artifact: %w", &os.PathError{Op: "read", Path: "synthetic-proof.json", Err: io.ErrUnexpectedEOF}),
		finalSemanticTestRPCError{code: -32002, message: "state already discarded; archive required"},
		finalSemanticTestRPCError{code: -32005, message: "execution reverted: timeout"},
		context.Canceled,
	} {
		reads := 0
		err := continueHistoricalPreparationWithWait(t.Context(), func(context.Context) error { reads++; return failure }, func(context.Context, time.Duration) error {
			t.Fatalf("permanent or mixed failure was deferred: %v", failure)
			return nil
		})
		if reads != 1 || !errors.Is(err, failure) {
			t.Errorf("permanent failure reads=%d error=%v want=%v", reads, err, failure)
		}
	}
}

func TestHistoricalPreparationClassifiesEveryJoinedCause(t *testing.T) {
	transient := errors.Join(
		fmt.Errorf("synthetic batch: %w", &evmReadRpcExhaustedError{operation: "synthetic read", cause: context.DeadlineExceeded}),
		fmt.Errorf("synthetic checkpoint prerequisite: %w", rpc.HTTPError{StatusCode: http.StatusServiceUnavailable}),
	)
	if !historicalPreparationReadIsTransient(transient) || historicalPreparationReadIsTransient(errors.Join(transient, errors.New("synthetic invalid receipt"))) {
		t.Fatal("continuation failed to preserve all joined failure classifications")
	}
}

// Four deterministic HTTP failures exhaust the real bounded read window.
// The next preparation pass must reopen only the unresolved authenticated group.
func TestHistoricalPreparationResumesOnlyUnresolvedPinnedGroups(t *testing.T) {
	f := newFleetInstallHistoryCacheFixture(t)
	f.rpc.transientFromBatch, f.rpc.transientFailures = 2, 4
	passes, waits := 0, 0
	err := continueHistoricalPreparationWithWait(t.Context(), func(ctx context.Context) error {
		passes++
		return f.reopenedExecutor().verifyFleetInstallPinnedState(ctx, f.action, f.head, f.evidence, f.snapshots)
	}, func(ctx context.Context, delay time.Duration) error {
		waits++
		f.rpc.mu.Lock()
		defer f.rpc.mu.Unlock()
		if passes != 1 || f.rpc.transientFailures != 0 || f.rpc.contractReads != 45 {
			t.Fatalf("first proof group was lost or failed bytes counted: passes=%d faults=%d reads=%d", passes, f.rpc.transientFailures, f.rpc.contractReads)
		}
		return ctx.Err()
	})
	if err != nil || passes != 2 || waits != 1 {
		t.Fatalf("resume passes/waits=%d/%d error=%v", passes, waits, err)
	}
	f.rpc.mu.Lock()
	defer f.rpc.mu.Unlock()
	if f.rpc.contractReads != 90 || f.rpc.contractBatches != 6 || f.rpc.finalizedReads != 2 || f.rpc.canonicalReads != 2 {
		t.Fatalf("resume repeated completed proof or skipped fresh finality: reads/batches/finalized/canonical=%d/%d/%d/%d", f.rpc.contractReads, f.rpc.contractBatches, f.rpc.finalizedReads, f.rpc.canonicalReads)
	}
}

func TestHistoricalPreparationCheckpointBatchRetriesOnlyUnresolvedHeader(t *testing.T) {
	var lock sync.Mutex
	var selectors []uint64
	failed := false
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		lock.Lock()
		defer lock.Unlock()
		defer request.Body.Close()
		wire, err := io.ReadAll(request.Body)
		if err != nil {
			t.Error(err)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		if !strings.HasPrefix(strings.TrimSpace(string(wire)), "[") {
			var call fleetHistoryBatchRPCRequest
			if json.Unmarshal(wire, &call) != nil || call.Method != "eth_getBlockByNumber" || len(call.Params) != 2 || string(call.Params[0]) != `"finalized"` {
				t.Errorf("unexpected finalized request: %s", wire)
				return
			}
			json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": call.ID, "result": map[string]any{"number": "0x3e8", "hash": fleetHistoryBatchBlockHash(1_000)}})
			return
		}
		var requests []fleetHistoryBatchRPCRequest
		if json.Unmarshal(wire, &requests) != nil || len(requests) == 0 {
			t.Errorf("invalid checkpoint batch: %s", wire)
			return
		}
		responses := make([]map[string]any, len(requests))
		for index, call := range requests {
			var selector string
			if call.Method != "eth_getBlockByNumber" || len(call.Params) != 2 || json.Unmarshal(call.Params[0], &selector) != nil {
				t.Errorf("invalid checkpoint read: %+v", call)
				return
			}
			number, err := hexutil.DecodeUint64(selector)
			if err != nil {
				t.Error(err)
				return
			}
			selectors = append(selectors, number)
			response := map[string]any{"jsonrpc": "2.0", "id": call.ID}
			if number == 102 && !failed {
				failed = true
				response["error"] = map[string]any{"code": -32002, "message": "request timed out"}
			} else {
				response["result"] = map[string]any{"number": selector, "hash": fleetHistoryBatchBlockHash(number)}
			}
			responses[index] = response
		}
		json.NewEncoder(writer).Encode(responses)
	}))
	defer server.Close()
	client, err := rpc.DialHTTP(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	var calls []historicalFleetGenerationOneCall
	for _, number := range []uint64{101, 102} {
		calls = append(calls, historicalFleetGenerationOneCall{
			action:  Action{ID: fmt.Sprintf("synthetic.history.%d", number)},
			record:  &ActionPostcondition{EVMFinalized: ChainHead{Number: number, Hash: fleetHistoryBatchBlockHash(number)}},
			observe: func([]byte) (map[string]any, error) { return map[string]any{}, nil },
		})
	}
	failures := collectHistoricalFleetCheckpoints(t.Context(), ethclient.NewClient(client), calls, false)
	if len(failures) != 2 || errors.Join(failures...) != nil {
		t.Fatalf("transient checkpoint batch blocked preparation: %v", failures)
	}
	lock.Lock()
	defer lock.Unlock()
	if !slices.Equal(selectors, []uint64{101, 102, 102}) {
		t.Fatalf("checkpoint continuation repeated completed or changed block reads: %v", selectors)
	}
}
