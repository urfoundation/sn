//go:build linux || darwin

// Actual Rpc errors are injected after signed preparation or broadcast. Their
// bounded owners must retry without publishing readiness or changing custody.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// Both canonical anchor reads and the native continuation read happen behind
// the real phase owner. One failed read cannot invent a changed snapshot.
func TestEvidenceRelayReadErrorsPhaseAdmissionRetriesPinnedReads(t *testing.T) {
	for _, failure := range []string{"continuation", "original", "native"} {
		fixture, runtime := provisionalRelayContinuationRuntimeTest(t)
		before, err := os.ReadFile(runtime.executor.journal.path)
		if err != nil {
			t.Fatal(err)
		}
		matches, injected := 0, false
		fixture.stateLock.Lock()
		fixture.requestError = func(method string, params []json.RawMessage) error {
			match := failure == "native" && method == "state_call"
			if failure != "native" && method == "eth_getBlockByNumber" && len(params) == 2 && string(params[0]) == `"0xc8"` {
				matches++
				match = matches == 1 && failure == "continuation" || matches == 2 && failure == "original"
			}
			if match && !injected {
				injected = true
				return errors.New("upstream overloaded")
			}
			return nil
		}
		fixture.stateLock.Unlock()
		if err := runtime.prepareHorizon(); err != nil {
			t.Fatalf("%s phase admission did not retry its read: %v", failure, err)
		}
		fixture.stateLock.Lock()
		wasInjected := injected
		fixture.stateLock.Unlock()
		if !wasInjected || runtime.horizon == nil {
			t.Fatalf("%s read failure did not precede successful admission", failure)
		}
		after, err := os.ReadFile(runtime.executor.journal.path)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("%s read retry changed signed transaction progress: %v", failure, err)
		}
	}
}

// One consumed admission request remains owned until retry completes. It is
// answered exactly once, with no advancement from the failed native read.
func TestEvidenceRelayReadErrorsPhaseTransitionRetainsRequest(t *testing.T) {
	runtime, fixture := newEvidenceRelayPhaseSchedulerTest(t)
	injected := false
	fixture.stateLock.Lock()
	fixture.requestError = func(method string, _ []json.RawMessage) error {
		if method == "state_call" && !injected {
			injected = true
			return errors.New("upstream overloaded")
		}
		return nil
	}
	fixture.stateLock.Unlock()
	result := queuedEvidenceRelayPreparationTest(t, runtime, t.Context())
	if err := runtime.serviceRemainingRequests(); err != nil {
		t.Fatal("phase transition abandoned its consumed request", err)
	}
	if err := <-result; err != nil || !runtime.prepared || len(runtime.remainingRequests) != 0 {
		t.Fatal("phase request lost its successful read or received failure", err)
	}
	fixture.stateLock.Lock()
	defer fixture.stateLock.Unlock()
	if !injected {
		t.Fatal("phase transition skipped the required native read")
	}
}

// Exhaustion is bounded; caller abandonment keeps the worker available, while
// worker shutdown cancels a request that otherwise still has time to run.
func TestEvidenceRelayReadErrorsPhaseTransitionBoundsAndCancellation(t *testing.T) {
	for _, failure := range []string{"exhausted", "request", "worker"} {
		runtime, fixture := newEvidenceRelayPhaseSchedulerTest(t)
		workerCtx, cancelWorker := context.WithCancel(t.Context())
		defer cancelWorker()
		runtime.ctx = workerCtx
		requestCtx, cancelRequest := context.WithCancel(t.Context())
		defer cancelRequest()
		reads := 0
		fixture.stateLock.Lock()
		fixture.requestError = func(method string, _ []json.RawMessage) error {
			if method == "state_call" {
				reads++
				return errors.New("upstream overloaded")
			}
			return nil
		}
		fixture.stateLock.Unlock()
		request := evidenceRelayRemainingRequest{ctx: requestCtx, remaining: 1, result: make(chan error, 1)}
		waits := 0
		err := runtime.completeRemainingRequestWithWait(request, func(ctx context.Context, _ time.Duration) error {
			waits++
			if failure == "request" {
				cancelRequest()
				return ctx.Err()
			}
			if failure == "worker" {
				cancelWorker()
				<-ctx.Done()
				return ctx.Err()
			}
			return nil
		})
		answer := <-request.result
		fixture.stateLock.Lock()
		observedReads := reads
		fixture.stateLock.Unlock()
		if answer == nil || runtime.prepared || len(request.result) != 0 {
			t.Fatalf("%s completed an unproven phase transition", failure)
		}
		if failure == "exhausted" {
			if err == nil || observedReads != evidenceRelayStepMaximumAttempts || waits != observedReads-1 {
				t.Fatalf("phase transition lost retry bound: reads=%d waits=%d err=%v", observedReads, waits, err)
			}
		} else if observedReads != 1 || waits != 1 || !errors.Is(answer, context.Canceled) || failure == "request" && err != nil || failure == "worker" && err == nil {
			t.Fatalf("%s changed cancellation ownership: reads=%d waits=%d answer=%v err=%v", failure, observedReads, waits, answer, err)
		}
	}
}

// A later successful read may observe a newer head, but it cannot restart the
// fixed native preparation budget that was set by the first admitted head.
func TestEvidenceRelayReadErrorsPhaseAdmissionKeepsOriginalDeadline(t *testing.T) {
	runtime, fixture := newEvidenceRelayPhaseSchedulerTest(t)
	runtime.horizon = nil
	original := *runtime.nativeWarmupBudget
	injected := false
	fixture.stateLock.Lock()
	fixture.requestError = func(method string, _ []json.RawMessage) error {
		if method == "state_call" && !injected {
			injected = true
			return errors.New("upstream overloaded")
		}
		return nil
	}
	fixture.stateLock.Unlock()
	var retryStarted time.Time
	err := runtime.prepareHorizonWithWait(func(context.Context, time.Duration) error {
		retryStarted = time.Now()
		fixture.stateLock.Lock()
		defer fixture.stateLock.Unlock()
		fixture.finalizedEvmNumber, fixture.finalizedEvmHash = 211, common.Hash{0x34}
		return nil
	})
	if err != nil || runtime.nativeWarmupBudget == nil || runtime.nativeWarmupBudget.StartHead != original.StartHead || runtime.nativeWarmupBudget.EndBlock != original.EndBlock || retryStarted.IsZero() || !runtime.nativeWarmupBudget.StartedAt.Before(retryStarted) || runtime.nativeWarmupBudget.Deadline.Sub(runtime.nativeWarmupBudget.StartedAt) != original.Deadline.Sub(original.StartedAt) {
		t.Fatal("retry replaced the first native preparation clock", err)
	}
}

// Cancellation never hides an actual mismatch or a failed durable write,
// including errors with a transient sibling that otherwise permits retry.
func TestEvidenceRelayReadErrorsAbandonedRequestPreservesHardFailures(t *testing.T) {
	for _, failure := range []error{
		errors.Join(context.Canceled, errors.New("canonical hash differs")),
		errors.Join(context.Canceled, context.DeadlineExceeded, errors.New("signed intent differs")),
		errors.Join(context.Canceled, &os.PathError{Op: "write", Path: "synthetic-retry.json", Err: context.DeadlineExceeded}),
	} {
		if evidenceRelayAbandonedRequestError(failure, context.Canceled) {
			t.Fatal("request cancellation concealed a hard failure", failure)
		}
	}
}

// Refusals describe observed values, not availability. A changed permit or
// independently pinned nonce must never enter the transport retry path.
func TestEvidenceRelayReadErrorsActualMismatchesStayHard(t *testing.T) {
	fixture, runtime := provisionalRelayContinuationRuntimeTest(t)
	fixture.stateLock.Lock()
	fixture.permits[1] = false
	fixture.stateLock.Unlock()
	if err := runtime.prepareHorizon(); err == nil || evidenceRelayTransientError(err) || runtime.horizon != nil {
		t.Fatal("actual eligibility mismatch was admitted or retryable", err)
	}
	executor, exposure, _ := evidenceRelayNonceTest(t, rpcModePrivateAuthority)
	first := common.Address{19: 1}.Hex() + "/0x28"
	second := common.Address{19: 2}.Hex() + "/0x28"
	independent := &evidenceRelayNonceRpcFixture{nonces: map[string]uint64{first: 2, second: 1}}
	executor.independentEVM = independent.client(t)
	if points, err := executor.observeEvidenceRelayContinuationNonces(t.Context(), exposure, 40); err == nil || evidenceRelayTransientError(err) || points != nil {
		t.Fatal("actual nonce mismatch was admitted or retryable", err)
	}
}

// The exact independent read error survives its Evm client and census owner.
func TestEvidenceRelayReadErrorsIndependentNoncePreservesTransport(t *testing.T) {
	executor, exposure, _ := evidenceRelayNonceTest(t, rpcModePrivateAuthority)
	first := common.Address{19: 1}.Hex() + "/0x28"
	second := common.Address{19: 2}.Hex() + "/0x28"
	independent := &evidenceRelayNonceRpcFixture{nonces: map[string]uint64{first: 1, second: 1}, failure: first, failureMessage: "upstream overloaded"}
	executor.independentEVM = independent.client(t)
	if points, err := executor.observeEvidenceRelayContinuationNonces(t.Context(), exposure, 40); err == nil || !evidenceRelayTransientError(err) || points != nil || strings.Contains(err.Error(), "differs") {
		t.Fatal("independent transport error became an observed nonce mismatch", err)
	}
}

// A reverted original transaction is retained while its race proof retries.
// Each failure is injected at a different real read after the original send.
func TestEvidenceRelayReadErrorsPublicationRaceResumesExactCustody(t *testing.T) {
	for _, failure := range []string{"finality", "body", "winner", "canonical"} {
		fixture := newEvidenceRelayRpcFixture(t, "race")
		canonicalReads, failures := 0, 0
		maximumFailures := 1
		if failure == "finality" || failure == "canonical" {
			maximumFailures = defaultFinalSemanticRPCRetryPolicy().maximumAttempts
		}
		fixture.stateLock.Lock()
		fixture.requestError = func(request evidenceRelayRpcRequest) error {
			match := false
			if request.Method == "eth_getBlockByNumber" && len(request.Params) == 2 && string(request.Params[0]) == `"0x4b2"` {
				canonicalReads++
				match = failure == "finality" && canonicalReads >= 2 || failure == "canonical" && canonicalReads >= 3
			}
			if failure == "body" && request.Method == "eth_getTransactionByBlockHashAndIndex" && len(request.Params) == 2 && string(request.Params[0]) == `"`+(common.Hash{0xb2}).Hex()+`"` {
				match = true
			}
			if failure == "winner" && request.Method == "eth_getLogs" {
				match = true
			}
			if match && failures < maximumFailures {
				failures++
				return errors.New("upstream overloaded")
			}
			return nil
		}
		fixture.stateLock.Unlock()
		runtime := &evidenceRelayRuntime{phase: "release-1.0", executor: &Executor{cfg: &ResolvedConfig{}, stateDir: fixture.stateDir,
			plan: &SetupPlan{PlanHash: fixture.planHash, DeploymentID: fixture.manager.deploymentID}}}
		attempts, waits := 0, 0
		var result *evidenceRelayTransactionResult
		err := runtime.retryStepWithWait(t.Context(), "closed-publications", func() error {
			attempts++
			var err error
			result, err = fixture.manager.relayValidatorEvidenceTransaction(t.Context(), fixture.chain, fixture.planHash, fixture.action, fixture.expected)
			return err
		}, func(context.Context, time.Duration) error {
			waits++
			if fixture.requestCount("eth_sendRawTransaction") != 1 || fixture.requestCount("pending-nonce") != 1 {
				t.Fatal("retry lost the original broadcast or nonce")
			}
			fixture.reopen(t)
			return nil
		})
		fixture.stateLock.Lock()
		wasInjected := failures == maximumFailures
		fixture.stateLock.Unlock()
		if err != nil || !wasInjected || attempts != 2 || waits != 1 || result == nil || !result.LostPublicationRace || result.OwnReceipt == nil || result.OwnReceipt.TxHash != fixture.transaction.Hash() || result.Winner == nil || result.Winner.Receipt.TxHash != fixture.thirdPartyTransaction.Hash() {
			t.Fatalf("%s race read could not resume exact custody: attempts=%d waits=%d result=%+v err=%v", failure, attempts, waits, result, err)
		}
		if fixture.requestCount("eth_sendRawTransaction") != 1 || fixture.requestCount("pending-nonce") != 1 || result.OwnReceipt.GasUsed != 80000 {
			t.Fatalf("%s race retry resent, allocated or lost paid cost", failure)
		}
	}
}

// Failure to journal a known reverted receipt is a storage failure, not the
// canonical-revert outcome. A later read cannot replace it with retryability.
func TestEvidenceRelayReadErrorsPublicationRaceRetainsJournalFailure(t *testing.T) {
	fixture := newEvidenceRelayRpcFixture(t, "race")
	fixture.retain(t, fixture.transaction, fixture.expected.Relayer.Hex(), "4", "")
	fixture.installPublication(t, "race")
	fixture.manager.journal.mu.Lock()
	err := fixture.manager.journal.file.Close()
	fixture.manager.journal.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	fixture.stateLock.Lock()
	fixture.requestError = func(request evidenceRelayRpcRequest) error {
		if request.Method == "eth_getTransactionByBlockHashAndIndex" && len(request.Params) == 2 && string(request.Params[0]) == `"`+(common.Hash{0xb2}).Hex()+`"` {
			return errors.New("upstream overloaded")
		}
		return nil
	}
	fixture.stateLock.Unlock()
	result, err := fixture.manager.relayValidatorEvidenceTransaction(t.Context(), fixture.chain, fixture.planHash, fixture.action, fixture.expected)
	if err == nil || !errors.Is(err, os.ErrClosed) || evidenceRelayTransientError(err) || result != nil || fixture.requestCount("eth_sendRawTransaction") != 0 || fixture.requestCount("pending-nonce") != 0 {
		t.Fatal("later transient read concealed original receipt journal failure", err)
	}
}
