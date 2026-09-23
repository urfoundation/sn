//go:build linux || darwin

// Deterministic transport transitions prove that retries retain custody and
// audit separation without sleeping, sending to a live chain or resetting state.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	gethrpc "github.com/ethereum/go-ethereum/rpc"
)

// Both former immediate-return sites use this common finite owner. Every wait
// follows persisted failure evidence, and exhausted inner reads remain eligible.
func TestEvidenceRelayStepRetriesOnlyDurablyObservedTransientFailure(t *testing.T) {
	for _, operation := range []string{"closed-publications", "deposit-audits"} {
		attempts, waits := 0, 0
		var observations []evidenceRelayRetryObservation
		cause := &evmReadRpcExhaustedError{operation: operation, attempts: 3, cause: context.DeadlineExceeded}
		err := runEvidenceRelayStep(t.Context(), func() error {
			attempts++
			if attempts < 3 {
				return cause
			}
			return nil
		}, func(_ context.Context, delay time.Duration) error {
			waits++
			if len(observations) != waits || observations[waits-1].Outcome != "retrying" || delay != time.Duration(waits)*time.Second {
				t.Fatal("retry preceded durable failure or changed pacing")
			}
			return nil
		}, func(observation evidenceRelayRetryObservation) error {
			observations = append(observations, observation)
			return nil
		})
		if err != nil || attempts != 3 || waits != 2 || len(observations) != 3 || observations[2].Outcome != "recovered" {
			t.Fatalf("%s attempts=%d waits=%d observations=%v err=%v", operation, attempts, waits, observations, err)
		}
	}
}

// Integrity, authorization, local decode and mixed failures remain immediate,
// including diagnostic strings that resemble a network failure.
func TestEvidenceRelayStepRetainsHardAndMixedFailures(t *testing.T) {
	for _, cause := range []error{
		errors.New("signature mismatch: connection reset"), io.EOF,
		&os.PathError{Op: "read", Path: "synthetic.json", Err: context.DeadlineExceeded},
		gethrpc.HTTPError{StatusCode: http.StatusForbidden, Body: []byte("connection reset")},
		errors.Join(context.DeadlineExceeded, errors.New("content hash differs")),
		errors.Join(context.DeadlineExceeded, context.Canceled),
	} {
		calls := 0
		err := runEvidenceRelayStep(t.Context(), func() error { calls++; return cause }, func(context.Context, time.Duration) error {
			t.Fatal("hard failure reached retry wait")
			return nil
		}, func(evidenceRelayRetryObservation) error {
			t.Fatal("hard initial failure became a transient incident")
			return nil
		})
		if err == nil || err.Error() != cause.Error() || calls != 1 {
			t.Fatalf("hard failure changed: calls=%d err=%v", calls, err)
		}
	}
}

// Neither exhausted retries nor a persistence refusal produces completion.
func TestEvidenceRelayStepBoundsRetriesAndRequiresDurableDiagnostics(t *testing.T) {
	for _, failPersistence := range []bool{false, true} {
		calls, waits := 0, 0
		var observations []evidenceRelayRetryObservation
		storageFailure := errors.New("synthetic durable write refusal")
		err := runEvidenceRelayStep(t.Context(), func() error { calls++; return context.DeadlineExceeded }, func(context.Context, time.Duration) error {
			waits++
			return nil
		}, func(observation evidenceRelayRetryObservation) error {
			observations = append(observations, observation)
			if failPersistence {
				return storageFailure
			}
			return nil
		})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal("retry discarded its actual cause", err)
		}
		if failPersistence {
			if !errors.Is(err, storageFailure) || calls != 1 || waits != 0 {
				t.Fatal("undurable failure admitted more work", calls, waits, err)
			}
		} else if calls != evidenceRelayStepMaximumAttempts || waits != calls-1 || len(observations) != calls || observations[calls-1].Outcome != "exhausted" {
			t.Fatal("exhaustion changed finite budget", calls, waits, observations, err)
		}
	}
}

// Cancellation wins before another call and after a successful call; a canceled
// operation cannot manufacture a recovered diagnostic or passing completion.
func TestEvidenceRelayStepCancellationNeverAdmitsFurtherWork(t *testing.T) {
	for _, cancelSuccess := range []bool{false, true} {
		ctx, cancel := context.WithCancel(t.Context())
		calls, waits := 0, 0
		err := runEvidenceRelayStep(ctx, func() error {
			calls++
			if calls == 1 {
				return context.DeadlineExceeded
			}
			cancel()
			return nil
		}, func(context.Context, time.Duration) error {
			waits++
			if !cancelSuccess {
				cancel()
				return ctx.Err()
			}
			return nil
		}, func(observation evidenceRelayRetryObservation) error {
			if observation.Outcome == "recovered" {
				t.Fatal("canceled call was marked recovered")
			}
			return nil
		})
		cancel()
		wantCalls := 1
		if cancelSuccess {
			wantCalls = 2
		}
		if !errors.Is(err, context.Canceled) || calls != wantCalls || waits != 1 {
			t.Fatal("cancellation boundary changed", calls, waits, err)
		}
	}
}

// The endpoint finalizes the actual signed transaction then loses its reply.
// Re-entry must discover that exact winner without sending or allocating again.
func TestEvidenceRelayStepReconcilesDurableWinnerBeforeAnyResend(t *testing.T) {
	fixture := newEvidenceRelayRpcFixture(t, "send-busy-finalized")
	fixture.stateLock.Lock()
	receiptFailures := 0
	fixture.requestError = func(request evidenceRelayRpcRequest) error {
		if len(fixture.sentBytes) != 0 && request.Method == "eth_getTransactionReceipt" && receiptFailures < defaultFinalSemanticRPCRetryPolicy().maximumAttempts {
			receiptFailures++
			return errors.New("upstream overloaded")
		}
		return nil
	}
	fixture.stateLock.Unlock()
	runtime := &evidenceRelayRuntime{phase: "release-1.0", executor: &Executor{cfg: &ResolvedConfig{}, stateDir: fixture.stateDir,
		plan: &SetupPlan{PlanHash: fixture.planHash, DeploymentID: fixture.manager.deploymentID}}}
	calls, waits := 0, 0
	var result *evidenceRelayTransactionResult
	err := runtime.retryStepWithWait(t.Context(), "closed-publications", func() error {
		calls++
		var err error
		result, err = fixture.manager.relayValidatorEvidenceTransaction(t.Context(), fixture.chain, fixture.planHash, fixture.action, fixture.expected)
		return err
	}, func(context.Context, time.Duration) error {
		waits++
		paths, err := filepath.Glob(filepath.Join(fixture.stateDir, "evidence-relay-retries", "incident-*", "retry.json"))
		if err != nil || len(paths) != 1 || fixture.requestCount("eth_sendRawTransaction") != 1 {
			t.Fatal("retry began before durable incident and original send", err, paths)
		}
		var evidence struct {
			Observations []evidenceRelayRetryObservation `json:"observations"`
		}
		raw, err := os.ReadFile(paths[0])
		if err != nil || json.Unmarshal(raw, &evidence) != nil || len(evidence.Observations) != 1 || evidence.Observations[0].Outcome != "retrying" {
			t.Fatal("original refusal was not durable before retry", err)
		}
		return nil
	})
	if err != nil || calls != 2 || waits != 1 || result == nil || result.Winner == nil || result.OwnReceipt == nil || !bytes.Equal(result.Winner.SignedTransaction, fixture.transactionBytes) {
		t.Fatal("durable exact winner did not reconcile", calls, waits, err)
	}
	if fixture.requestCount("eth_sendRawTransaction") != 1 || fixture.requestCount("pending-nonce") != 1 || fixture.requestCount("eth_estimateGas") != 1 {
		t.Fatal("retry resent or allocated another transaction")
	}
}

// A capacity refusal during historical read retries its durable census, while
// journal, source cursors and live slot admission remain independently owned.
func TestEvidenceRelayPublicCensusRetriesWithoutMutatingRuntime(t *testing.T) {
	fixture := newEvidenceRelayColdCensusTestFixture(t)
	runtime := fixture.runtime
	if err := runtime.prepareHorizon(); err != nil {
		t.Fatal(err)
	}
	before := runtime.executor.journal.Entries()
	fixture.originStatus = http.StatusTooManyRequests
	census := runtime.pendingPublicCensus
	waits := 0
	err := census.reader.retryStepWithWait(t.Context(), "public-census", func() error { return census.verify(t.Context()) }, func(context.Context, time.Duration) error {
		waits++
		fixture.stateLock.Lock()
		defer fixture.stateLock.Unlock()
		fixture.originStatus = 0
		return nil
	})
	if err != nil || waits != 1 || len(runtime.horizon.headerKVs) != 0 || len(census.horizon.headerKVs) != 6 || !reflect.DeepEqual(before, runtime.executor.journal.Entries()) {
		t.Fatal("read-only recovery changed runtime authority", waits, err)
	}
	paths, err := filepath.Glob(filepath.Join(runtime.executor.stateDir, evidenceRelayColdCensusDirectoryName, "*.json"))
	if err != nil || len(paths) != 3 {
		t.Fatal("complete authenticated publications were not retained", err, paths)
	}
}

// A missing queued census remains an ordinary failed audit, never a panic in
// its management goroutine or a passing final result.
func TestEvidenceRelayPublicAuditMissingOwnerRemainsHard(t *testing.T) {
	for _, census := range []*evidenceRelayPublicCensus{nil, {}} {
		audit := newEvidenceRelayPublicAudit(t.Context(), census)
		if err := audit.Wait(t.Context()); err == nil {
			t.Fatal("missing audit owner passed")
		}
		if err := audit.Close(); err == nil {
			t.Fatal("closing lost the missing-owner failure")
		}
	}
}
