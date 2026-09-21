// Synthetic responses force timeout, partial-batch and cancellation outcomes.
// No test contacts a chain or depends on a timeout occurring by scheduler luck.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"

	"github.com/urfoundation/sn/stabi"
)

func TestEvmReadRpcTimeoutRetriesWithFreshBudget(t *testing.T) {
	t.Parallel()
	policy := defaultFinalSemanticRPCRetryPolicy()
	var contexts []context.Context
	var delays []time.Duration
	policy.wait = func(ctx context.Context, delay time.Duration) error {
		delays = append(delays, delay)
		if ctx.Err() != nil || contexts[len(contexts)-1].Err() != context.Canceled {
			t.Fatal("attempt must close before cancellable backoff")
		}
		return nil
	}
	err := retryEvmReadRpcCall(t.Context(), "synthetic pinned read", policy, func(ctx context.Context) error {
		contexts = append(contexts, ctx)
		if _, ok := ctx.Deadline(); !ok || ctx.Err() != nil || ctx.Value(ownedEvmRpcRetryBudgetKey{}) != true {
			t.Fatal("attempt has no fresh bounded retry ownership")
		}
		if len(contexts) == 1 {
			return fmt.Errorf("synthetic RPC timeout: %w", context.DeadlineExceeded)
		}
		return nil
	})
	if err != nil || len(contexts) != 2 || !slices.Equal(delays, []time.Duration{time.Second}) {
		t.Fatalf("timeout recovery: contexts=%d delays=%v error=%v", len(contexts), delays, err)
	}
}

func TestEvmReadRpcExhaustionDoesNotRestartCarriedAudit(t *testing.T) {
	t.Parallel()
	policy := defaultFinalSemanticRPCRetryPolicy()
	var delays []time.Duration
	policy.wait = func(ctx context.Context, delay time.Duration) error { delays = append(delays, delay); return ctx.Err() }
	attempts, audits := 0, 0
	err := verifyCarriedActionWithTimeoutFor(t.Context(), &ResolvedConfig{OperationalRPCMode: rpcModeOwnedNode}, func(ctx context.Context) error {
		audits++
		return retryEvmReadRpcCall(ctx, "synthetic pinned read", policy, func(context.Context) error {
			attempts++
			return context.DeadlineExceeded
		})
	})
	if audits != 1 || attempts != 4 || !slices.Equal(delays, []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}) {
		t.Fatalf("multiplied or unbounded retry: audits=%d attempts=%d delays=%v", audits, attempts, delays)
	}
	if !errors.Is(err, context.DeadlineExceeded) || !evmReadRpcRetriesExhausted(err) || finalSemanticRPCErrorIsTransient(err) || !strings.Contains(err.Error(), "exhausted after 4 attempts (per-attempt timeout 30s)") {
		t.Fatalf("exhaustion lost its cause or bound: %v", err)
	}
}

func TestEvmReadRpcKeepsPermanentAndMixedErrorsStrict(t *testing.T) {
	t.Parallel()
	permanent := []error{
		errors.New("evidence timeout digest mismatch"),
		finalSemanticTestRPCError{code: -32000, message: "execution reverted: timeout"},
		finalSemanticTestRPCError{code: -32002, message: "state already discarded; archive required"},
		finalSemanticTestRPCError{code: -32005, message: "execution reverted: rate limit"},
		finalSemanticTestRPCError{code: -32602, message: "invalid params"},
		finalSemanticTestRPCError{code: 3, message: "Upstream overloaded"},
		finalSemanticTestRPCError{code: -32602, message: "Historical work rate limit exceeded"},
		&json.SyntaxError{Offset: 7}, rpc.ErrNoResult,
		&os.PathError{Op: "read", Path: "synthetic-evidence.json", Err: io.ErrUnexpectedEOF},
		errors.Join(context.DeadlineExceeded, errors.New("signed evidence mismatch")),
		&rpc.HTTPError{StatusCode: http.StatusUnauthorized, Status: "401 Unauthorized"},
		&net.DNSError{Err: "synthetic name does not exist", IsNotFound: true},
	}
	for _, failure := range permanent {
		attempts := 0
		policy := defaultFinalSemanticRPCRetryPolicy()
		policy.wait = func(context.Context, time.Duration) error { t.Fatalf("hard failure retried: %v", failure); return nil }
		err := retryEvmReadRpcCall(t.Context(), "synthetic read", policy, func(context.Context) error { attempts++; return failure })
		if !errors.Is(err, failure) || attempts != 1 || evmReadRpcErrorIsTransient(failure) {
			t.Fatalf("hard failure=%v attempts=%d error=%v", failure, attempts, err)
		}
	}
	for _, failure := range []error{
		context.DeadlineExceeded, syscall.ECONNRESET, io.ErrUnexpectedEOF, rpc.ErrMissingBatchResponse,
		finalSemanticTestRPCError{code: -32002, message: "request timed out"},
		finalSemanticTestRPCError{code: -32005, message: "server busy"},
		finalSemanticTestRPCError{code: -32016, message: "temporarily unavailable"},
		finalSemanticTestRPCError{code: -32000, message: "Upstream overloaded"},
		finalSemanticTestRPCError{code: -32000, message: "Historical work rate limit exceeded"},
		rpc.HTTPError{StatusCode: http.StatusServiceUnavailable},
		&rpc.HTTPError{StatusCode: http.StatusServiceUnavailable},
		&net.DNSError{Err: "synthetic temporary lookup failure", IsTemporary: true},
		&net.DNSError{Err: "synthetic lookup timeout", IsTimeout: true},
		errors.Join(context.DeadlineExceeded, io.ErrUnexpectedEOF),
	} {
		if !evmReadRpcErrorIsTransient(fmt.Errorf("read: %w", failure)) {
			t.Fatalf("typed transient failure refused: %v", failure)
		}
	}
	// A joined hard failure cannot be retried by the older whole-action timeout
	// wrapper merely because errors.Is also finds a deadline in the same error.
	attempts := 0
	failure := errors.Join(errors.New("signed evidence mismatch"), context.DeadlineExceeded)
	err := verifyCarriedActionWithTimeoutFor(t.Context(), &ResolvedConfig{OperationalRPCMode: rpcModeOwnedNode}, func(context.Context) error { attempts++; return failure })
	if attempts != 1 || !errors.Is(err, failure) {
		t.Fatalf("mixed action failure retried: attempts=%d error=%v", attempts, err)
	}
}

func TestEvmReadRpcCancellationStopsAttemptsAndBackoff(t *testing.T) {
	t.Parallel()
	for _, at := range []string{"before", "request", "backoff", "success"} {
		ctx, cancel := context.WithCancel(t.Context())
		attempts, waits := 0, 0
		policy := defaultFinalSemanticRPCRetryPolicy()
		policy.wait = func(ctx context.Context, _ time.Duration) error {
			waits++
			cancel()
			return ctx.Err()
		}
		if at == "before" {
			cancel()
		}
		err := retryEvmReadRpcCall(ctx, "synthetic read", policy, func(context.Context) error {
			attempts++
			if at != "backoff" {
				cancel()
			}
			if at == "success" {
				return nil
			}
			return context.DeadlineExceeded
		})
		cancel()
		wantAttempts, wantWaits := 1, 0
		if at == "before" {
			wantAttempts = 0
		} else if at == "backoff" {
			wantWaits = 1
		}
		if !errors.Is(err, context.Canceled) || attempts != wantAttempts || waits != wantWaits {
			t.Fatalf("cancellation=%s attempts=%d waits=%d error=%v", at, attempts, waits, err)
		}
	}
}

func TestEvmReadRpcBatchRetriesOnlyPendingElements(t *testing.T) {
	t.Parallel()
	reads := []evmRpcRead{
		{method: "eth_call", args: []any{"first", "0x123"}},
		{method: "eth_call", args: []any{"second", "0x456"}},
		{method: "eth_call", args: []any{"third", "0x789"}},
	}
	permanent := finalSemanticTestRPCError{code: -32000, message: "execution reverted: timeout"}
	attempts := 0
	results, err := readEvmRpcBatchWithPolicy[hexutil.Bytes](t.Context(), "synthetic batch", reads, immediateFinalSemanticRetryPolicy(), func(ctx context.Context, batch []rpc.BatchElem) error {
		attempts++
		if ctx.Value(ownedEvmRpcRetryBudgetKey{}) != true {
			t.Fatal("batch does not own its retry budget")
		}
		if attempts == 1 {
			if len(batch) != 3 {
				t.Fatal("initial batch lost a request")
			}
			*batch[0].Result.(*hexutil.Bytes) = []byte{1}
			batch[1].Error = rpc.ErrMissingBatchResponse
			batch[2].Error = permanent
			return nil
		}
		if len(batch) != 1 || batch[0].Method != "eth_call" || !slices.Equal(batch[0].Args, reads[1].args) || len(*batch[0].Result.(*hexutil.Bytes)) != 0 {
			t.Fatalf("pending request changed or stale output reused: %+v", batch)
		}
		if attempts == 2 {
			*batch[0].Result.(*hexutil.Bytes) = []byte{0xff}
			batch[0].Error = finalSemanticTestRPCError{code: -32002, message: "request timed out"}
		} else {
			*batch[0].Result.(*hexutil.Bytes) = []byte{2}
		}
		return nil
	})
	if err != nil || attempts != 3 || len(results) != 3 || !slices.Equal(results[0].value, []byte{1}) || !slices.Equal(results[1].value, []byte{2}) || results[0].err != nil || results[1].err != nil || !errors.Is(results[2].err, permanent) {
		t.Fatalf("partial results lost: attempts=%d results=%+v error=%v", attempts, results, err)
	}
}

func TestEvmReadRpcBatchExhaustionPreservesSuccessfulSiblings(t *testing.T) {
	t.Parallel()
	reads := []evmRpcRead{{method: "eth_call"}, {method: "eth_call"}, {method: "eth_call"}}
	permanent := errors.New("synthetic contract error")
	attempts := 0
	results, err := readEvmRpcBatchWithPolicy[hexutil.Bytes](t.Context(), "synthetic batch", reads, immediateFinalSemanticRetryPolicy(), func(_ context.Context, batch []rpc.BatchElem) error {
		attempts++
		if attempts == 1 {
			*batch[0].Result.(*hexutil.Bytes) = []byte{1}
			batch[1].Error = context.DeadlineExceeded
			batch[2].Error = permanent
			return nil
		}
		if len(batch) != 1 {
			t.Fatal("completed elements were repeated")
		}
		return context.DeadlineExceeded
	})
	if err != nil || attempts != 4 || len(results) != 3 || !slices.Equal(results[0].value, []byte{1}) || results[0].err != nil || !evmReadRpcRetriesExhausted(results[1].err) || !errors.Is(results[1].err, context.DeadlineExceeded) || !errors.Is(results[2].err, permanent) {
		t.Fatalf("exhaustion lost independent results: attempts=%d results=%+v error=%v", attempts, results, err)
	}
}

func TestEvmReadRpcBatchDiscardsPartialTransportResults(t *testing.T) {
	t.Parallel()
	attempts := 0
	results, err := readEvmRpcBatchWithPolicy[*evmRPCBlock](t.Context(), "synthetic block", []evmRpcRead{{method: "eth_getBlockByNumber"}}, immediateFinalSemanticRetryPolicy(), func(_ context.Context, batch []rpc.BatchElem) error {
		attempts++
		result := batch[0].Result.(**evmRPCBlock)
		if *result != nil {
			t.Fatal("failed response contaminated the next attempt")
		}
		if attempts == 1 {
			*result = &evmRPCBlock{Number: "0x123", Hash: common.HexToHash("0x1234").Hex()}
			return io.ErrUnexpectedEOF
		}
		*result = &evmRPCBlock{Number: "0x123"}
		return nil
	})
	if err != nil || attempts != 2 || len(results) != 1 || results[0].err != nil {
		t.Fatalf("transport retry: attempts=%d results=%+v error=%v", attempts, results, err)
	}
	if _, err := decodeEVMRPCBlock(results[0].value, big.NewInt(0x123)); err == nil || !strings.Contains(err.Error(), "invalid EVM block hash") {
		t.Fatalf("malformed response reused a stale authenticated hash: %v", err)
	}
}

func TestEvmReadRpcBatchRejectsSubmissionsAndCanceledSuccess(t *testing.T) {
	t.Parallel()
	for _, method := range []string{"eth_sendRawTransaction", "eth_sendTransaction", "personal_unlockAccount", "unknown"} {
		_, err := readEvmRpcBatchWithPolicy[string](t.Context(), "synthetic batch", []evmRpcRead{{method: method}}, immediateFinalSemanticRetryPolicy(), func(context.Context, []rpc.BatchElem) error { t.Fatal("non-read method reached transport"); return nil })
		if err == nil {
			t.Fatalf("non-read method %s accepted", method)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	_, err := readEvmRpcBatchWithPolicy[string](ctx, "synthetic batch", []evmRpcRead{{method: "eth_call"}}, immediateFinalSemanticRetryPolicy(), func(_ context.Context, batch []rpc.BatchElem) error {
		*batch[0].Result.(*string) = "synthetic success"
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation was hidden by a successful final element: %v", err)
	}
}

func TestEvmReadRpcPublicTransportDoesNotMultiplyAttempts(t *testing.T) {
	t.Parallel()
	for _, failure := range []string{"transport", "body", "http", "overloaded", "historical-capacity"} {
		attempts := 0
		transport := &rateLimitedRetryTransport{
			gate: &rpcRequestGate{interval: time.Nanosecond}, maximumRetries: 3,
			defaultRetryAfter: time.Nanosecond, maximumRetryAfter: time.Second,
			base: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				attempts++
				var batch []struct {
					Id json.RawMessage `json:"id"`
				}
				if err := json.NewDecoder(request.Body).Decode(&batch); err != nil {
					return nil, err
				}
				request.Body.Close()
				if len(batch) != 1 {
					t.Fatal("synthetic public request changed")
				}
				if failure == "transport" {
					return nil, context.DeadlineExceeded
				}
				if failure == "body" {
					return &http.Response{StatusCode: http.StatusOK, Body: &ownedEvmRetryTestBody{readError: io.ErrUnexpectedEOF}}, nil
				}
				if failure == "http" {
					return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader("synthetic unavailable"))}, nil
				}
				message := "Upstream overloaded"
				if failure == "historical-capacity" {
					message = "Historical work rate limit exceeded"
				}
				body, err := json.Marshal([]map[string]any{{"jsonrpc": "2.0", "id": batch[0].Id, "error": map[string]any{"code": -32000, "message": message}}})
				if err != nil {
					return nil, err
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
			}),
		}
		rawClient, err := rpc.DialOptions(t.Context(), "https://synthetic-rpc.example", rpc.WithHTTPClient(&http.Client{Transport: transport}))
		if err != nil {
			t.Fatal(err)
		}
		policy := immediateFinalSemanticRetryPolicy()
		policy.wait = func(ctx context.Context, _ time.Duration) error {
			// Explicitly advance the synthetic provider window between attempts.
			// The separate cooldown test verifies its exact real deadline; this
			// test measures sends without racing a one-second capacity cooldown.
			transport.gate.stateLock.Lock()
			transport.gate.cooldownUntil = time.Time{}
			transport.gate.notifyLocked()
			transport.gate.stateLock.Unlock()
			return ctx.Err()
		}
		results, err := readEvmRpcBatchWithPolicy[hexutil.Bytes](t.Context(), "synthetic public read", []evmRpcRead{{method: "eth_call", args: []any{map[string]any{}, "0x123"}}}, policy, rawClient.BatchCallContext)
		rawClient.Close()
		if err != nil || attempts != 4 || len(results) != 1 || !evmReadRpcRetriesExhausted(results[0].err) {
			t.Fatalf("public failure=%s multiplied attempts=%d results=%+v error=%v", failure, attempts, results, err)
		}
		transport.gate.stateLock.Lock()
		cooldown, next := transport.gate.cooldownUntil, transport.gate.next
		transport.gate.stateLock.Unlock()
		if cooldown.IsZero() || next.IsZero() {
			t.Fatalf("public failure=%s lost request gate or provider cooldown", failure)
		}
	}
}

func TestEvmReadRpcManagedPublicResponseKeepsProviderCooldown(t *testing.T) {
	t.Parallel()
	now := time.Now()
	body := &ownedEvmRetryTestBody{Reader: strings.NewReader("synthetic capacity response")}
	attempts := 0
	transport := &rateLimitedRetryTransport{
		gate: &rpcRequestGate{interval: time.Nanosecond}, maximumRetries: 3,
		defaultRetryAfter: time.Second, maximumRetryAfter: time.Minute, now: func() time.Time { return now },
		base: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			attempts++
			request.Body.Close()
			return &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{"Retry-After": []string{"2"}}, Body: body}, nil
		}),
	}
	ctx := context.WithValue(t.Context(), ownedEvmRpcRetryBudgetKey{}, true)
	request := publicEVMBodyRetryRequest(t, ctx, `{"jsonrpc":"2.0","method":"eth_call","id":1}`)
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	observed, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || string(observed) != "synthetic capacity response" || response.StatusCode != http.StatusTooManyRequests || attempts != 1 || !body.closed || !transport.gate.cooldownUntil.Equal(now.Add(2*time.Second)) {
		t.Fatalf("managed response changed: attempts=%d body=%q cooldown=%s error=%v", attempts, observed, transport.gate.cooldownUntil, err)
	}
}

// An actual historical oracle replay traverses the executor, checkpoint read,
// five-call ABI batch, owned HTTP transport and exact observed-state decoder.
func TestHistoricalEvmOracleReadRetriesTimeoutAndRejectsChangedState(t *testing.T) {
	t.Parallel()
	parsed, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		t.Fatal(err)
	}
	coordinator := stabi.NewSTCoordinator()
	address, immutable, active := common.HexToAddress("0x1234"), common.HexToAddress("0x1111"), common.HexToAddress("0x2222")
	head := ChainHead{Number: 123, Hash: common.HexToHash("0x4321").Hex()}
	outputs := map[string]string{
		hexutil.Encode(coordinator.PackCurrentEpoch()):                 fleetRefreshTestOutput(t, parsed, "currentEpoch", big.NewInt(9)),
		hexutil.Encode(coordinator.PackCommitmentOracle()):             fleetRefreshTestOutput(t, parsed, "commitmentOracle", immutable),
		hexutil.Encode(coordinator.PackActiveCommitmentOracle()):       fleetRefreshTestOutput(t, parsed, "activeCommitmentOracle", active),
		hexutil.Encode(coordinator.PackPendingCommitmentOracle()):      fleetRefreshTestOutput(t, parsed, "pendingCommitmentOracle", active),
		hexutil.Encode(coordinator.PackPendingCommitmentOracleEpoch()): fleetRefreshTestOutput(t, parsed, "pendingCommitmentOracleEpoch", uint64(9)),
	}
	type request struct {
		Id     json.RawMessage   `json:"id"`
		Method string            `json:"method"`
		Params []json.RawMessage `json:"params"`
	}
	batchAttempts := 0
	var batchSelectors [][]string
	transport := newOwnedEvmRetryTransport(roundTripFunc(func(httpRequest *http.Request) (*http.Response, error) {
		raw, err := io.ReadAll(httpRequest.Body)
		httpRequest.Body.Close()
		if err != nil {
			return nil, err
		}
		var body []byte
		if len(raw) > 0 && raw[0] == '[' {
			var batch []request
			if err := json.Unmarshal(raw, &batch); err != nil {
				return nil, err
			}
			batchAttempts++
			widths := []int{5, 3, 2, 5}
			if batchAttempts > len(widths) || len(batch) != widths[batchAttempts-1] {
				t.Fatalf("oracle batch did not preserve its bounded partition: attempt=%d width=%d", batchAttempts, len(batch))
			}
			responses := make([]map[string]any, len(batch))
			selectors := make([]string, len(batch))
			for index, call := range batch {
				var target struct {
					To   common.Address `json:"to"`
					Data string         `json:"data"`
				}
				if call.Method != "eth_call" || len(call.Params) != 2 || string(call.Params[1]) != `"0x7b"` || json.Unmarshal(call.Params[0], &target) != nil || target.To != address || outputs[target.Data] == "" {
					t.Fatalf("historical request identity changed: %+v", call)
				}
				if slices.Contains(selectors[:index], target.Data) {
					t.Fatal("historical oracle field was duplicated")
				}
				selectors[index] = target.Data
				responses[index] = map[string]any{"jsonrpc": "2.0", "id": call.Id, "result": outputs[target.Data]}
			}
			batchSelectors = append(batchSelectors, selectors)
			if batchAttempts == 1 {
				return nil, context.DeadlineExceeded
			}
			body, err = json.Marshal(responses)
		} else {
			var call request
			if err := json.Unmarshal(raw, &call); err != nil {
				return nil, err
			}
			if call.Method != "eth_getBlockByNumber" || string(call.Params[0]) != `"0x7b"` {
				t.Fatalf("unexpected historical read: %+v", call)
			}
			body, err = json.Marshal(map[string]any{"jsonrpc": "2.0", "id": call.Id, "result": evmRPCBlock{Number: "0x7b", Hash: head.Hash}})
		}
		if err != nil {
			return nil, err
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	}))
	transport.policy.wait = func(context.Context, time.Duration) error {
		t.Fatal("HTTP retries multiplied the read owner")
		return nil
	}
	rawClient, err := rpc.DialOptions(t.Context(), "http://synthetic-rpc.example", rpc.WithHTTPClient(&http.Client{Transport: transport}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rawClient.Close)
	manager := &EvmTxManager{client: ethclient.NewClient(rawClient)}
	action := Action{ID: "fleet.refresh.oracle-await-active", Kind: "evm-read", IntentHash: "synthetic-intent", Target: active.Hex()}
	state := map[string]any{"kind": action.Kind, "target": action.Target, "current_epoch": uint64(9), "immutable_oracle": immutable.Hex(), "active_oracle": active.Hex(), "pending_oracle": active.Hex(), "pending_epoch": uint64(9), "target_oracle": active.Hex()}
	record := &ActionPostcondition{Schema: "urnetwork-sim-action-postcondition-v4", PlanHash: "synthetic-source-plan", ActionID: action.ID, IntentHash: action.IntentHash, EVMFinalized: head, IndependentEVMFinalized: head, Observed: state, IndependentObserved: state}
	executor := &Executor{cfg: &ResolvedConfig{OperationalRPCMode: rpcModeOwnedNode}, plan: &SetupPlan{PlanHash: "synthetic-current-plan", PriorPlanHashes: []string{record.PlanHash}}, owner: manager, deployer: manager, payloads: &DeploymentPayloads{Manifest: ContractDeployment{CoordinatorProxy: address}, FleetBatcherAddress: active, CommitmentOracle: immutable}}
	if err := executor.verifyHistoricalEVMPostcondition(t.Context(), action, record, &head); err != nil || batchAttempts != 3 {
		t.Fatalf("historical timeout recovery: attempts=%d error=%v", batchAttempts, err)
	}
	if !slices.Equal(batchSelectors[0], append(slices.Clone(batchSelectors[1]), batchSelectors[2]...)) {
		t.Fatalf("oracle split lost or reordered fields: %v", batchSelectors)
	}
	outputs[hexutil.Encode(coordinator.PackCurrentEpoch())] = fleetRefreshTestOutput(t, parsed, "currentEpoch", big.NewInt(10))
	if err := executor.verifyHistoricalEVMPostcondition(t.Context(), action, record, &head); err == nil || !strings.Contains(err.Error(), "operational historical EVM state") || batchAttempts != 4 {
		t.Fatalf("changed historical evidence was retried or accepted: attempts=%d error=%v", batchAttempts, err)
	}
	if !slices.Equal(batchSelectors[0], batchSelectors[3]) {
		t.Fatalf("changed-state check did not read the full original oracle: %v", batchSelectors[3])
	}
}
