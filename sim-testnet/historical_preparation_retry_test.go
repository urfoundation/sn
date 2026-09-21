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

// Each inner call exhausts deterministically; returning success on a second
// census would prove the old controller hid the original deferred boundary.
func TestHistoricalPreparationReportsExhaustedCensusWithoutRestarting(t *testing.T) {
	exhausted := &evmReadRpcExhaustedError{operation: "synthetic pinned read", attempts: 4, attemptTimeout: 30 * time.Second, cause: context.DeadlineExceeded}
	failure := errors.Join(fmt.Errorf("action synthetic.a: %w", exhausted), fmt.Errorf("action synthetic.b: %w", exhausted))
	reads, applications := 0, 0
	err := continueHistoricalPreparation(t.Context(), func(context.Context) error {
		reads++
		if reads > 1 {
			return nil
		}
		return failure
	})
	report := &launchPreparationReport{Ready: true}
	report.add("carried history", err)
	published := false
	err = finishLaunchPreparation(report, func(got *launchPreparationReport, err error) error {
		published = true
		if !got.StoppedBeforeActions || got.Ready || len(got.Checks) != 1 || got.Checks[0].Detail != failure.Error() {
			t.Fatalf("strict preparation lost exact deferred actions: %+v", got)
		}
		return err
	}, func() error { applications++; return nil })
	if reads != 1 || applications != 0 || !published || err == nil || !strings.Contains(err.Error(), "action synthetic.a:") || !strings.Contains(err.Error(), "action synthetic.b:") {
		t.Fatalf("restarted or admitted deferred census: reads=%d applications=%d published=%t error=%v", reads, applications, published, err)
	}
}

func TestHistoricalPreparationCancellationRetainsUnresolvedFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	exhausted := &evmReadRpcExhaustedError{operation: "synthetic retained read", attempts: 4, cause: context.DeadlineExceeded}
	reads := 0
	err := continueHistoricalPreparation(ctx, func(context.Context) error { reads++; cancel(); return exhausted })
	if reads != 1 || !errors.Is(err, context.Canceled) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("canceled census lost unresolved failure: reads=%d error=%v", reads, err)
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
		err := continueHistoricalPreparation(t.Context(), func(context.Context) error { reads++; return failure })
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
	passes := 0
	verify := func(ctx context.Context) error {
		passes++
		return f.reopenedExecutor().verifyFleetInstallPinnedState(ctx, f.action, f.head, f.evidence, f.snapshots)
	}
	if err := continueHistoricalPreparation(t.Context(), verify); !historicalPreparationReadIsTransient(err) || passes != 1 {
		t.Fatalf("exhausted census did not return its retained boundary: passes=%d error=%v", passes, err)
	}
	f.rpc.mu.Lock()
	if f.rpc.transientFailures != 0 || f.rpc.contractReads != 45 {
		t.Errorf("first proof group was lost or failed bytes counted: faults=%d reads=%d", f.rpc.transientFailures, f.rpc.contractReads)
	}
	f.rpc.mu.Unlock()
	if err := continueHistoricalPreparation(t.Context(), verify); err != nil || passes != 2 {
		t.Fatalf("independent retry failed: passes=%d error=%v", passes, err)
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
