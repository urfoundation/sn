//go:build linux || darwin

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// Actual signed activation production and native/Evm readers own admission.
// The synthetic future backlog is scheduler state, never a completed proof.
func newEvidenceRelayPhaseSchedulerTest(t *testing.T) (*evidenceRelayRuntime, *runtimeEvidenceActivationRpcV2TestFixture) {
	t.Helper()
	fixture := newRuntimeEvidenceActivationRpcV2TestFixture(t)
	fixture.executor.stateDir = filepath.Join(t.TempDir(), "state")
	journal, err := OpenJournal(fixture.executor.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := journal.Close(); err != nil {
			t.Error(err)
		}
	})
	fixture.executor.journal = journal
	prepared, _, err := fixture.executor.prepareRuntimeEvidenceActivationsV2(t.Context(), fixture.chain)
	if err != nil {
		t.Fatal(err)
	}
	runtime := &evidenceRelayRuntime{ctx: t.Context(), executor: fixture.executor, chain: fixture.chain, phase: "production-soak",
		origins: [2]string{fixture.base.cfg.OperatorAPIOrigins[0], fixture.base.cfg.OperatorAPIOrigins[1]},
		ready:   make(chan struct{}), done: make(chan struct{}), remainingRequests: make(chan evidenceRelayRemainingRequest, 1),
		startupProgress: true, poll: time.Hour}
	for _, configured := range fixture.base.cfg.Config.ValidatorEvidenceV2 {
		source := evidenceRelaySource{validatorId: configured.ValidatorID, bounds: configured.Evidence.Bounds,
			stateDir: filepath.Join(fixture.executor.stateDir, "runtime", fmt.Sprintf("validator-%d", configured.ValidatorID), "state"), nextEpoch: prepared.Epoch}
		for _, member := range prepared.Members {
			if member.ValidatorId == source.validatorId {
				source.activations = append(source.activations, member.Activation)
			}
		}
		runtime.sources = append(runtime.sources, source)
	}
	func() {
		fixture.stateLock.Lock()
		defer fixture.stateLock.Unlock()
		fixture.finalizedEvmNumber, fixture.finalizedEvmHash = 210, common.Hash{0x33}
	}()
	if err := runtime.prepareHorizon(); err != nil {
		t.Fatal("actual activation and finalized horizon failed", err)
	}
	close(runtime.ready)
	return runtime, fixture
}

// Observe the real caller enqueue before letting the pass boundary run.
// Channels establish ordering; no wall-clock delay or fabricated verdict does.
func queuedEvidenceRelayPreparationTest(t *testing.T, runtime *evidenceRelayRuntime, ctx context.Context) <-chan error {
	t.Helper()
	result := make(chan error, 1)
	go func() { result <- runtime.RequirePrepared(ctx) }()
	select {
	case request := <-runtime.remainingRequests:
		runtime.remainingRequests <- request
	case err := <-result:
		t.Fatal("preparation returned before its owned request", err)
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	}
	return result
}

func TestEvidenceRelayPhaseSchedulerAdmitsBeforeBacklogDrains(t *testing.T) {
	t.Parallel()
	runtime, fixture := newEvidenceRelayPhaseSchedulerTest(t)
	before, err := os.ReadFile(fixture.executor.journal.path)
	if err != nil {
		t.Fatal(err)
	}
	remainingSources := append([]evidenceRelaySource(nil), runtime.sources...)
	beforeCalls := fixture.callCount("state_call")
	result := queuedEvidenceRelayPreparationTest(t, runtime, t.Context())
	if err := runtime.awaitNextPass(); err != nil {
		t.Fatal("retained backlog refused a queued phase request", err)
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	}
	if !runtime.prepared || !runtime.startupProgress || !reflect.DeepEqual(remainingSources, runtime.sources) || fixture.callCount("state_call") <= beforeCalls || len(runtime.horizon.headerKVs) != 0 {
		t.Fatal("phase admission skipped fresh native validation, drained history or invented completion")
	}
	after, err := os.ReadFile(fixture.executor.journal.path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("phase scheduling changed original transaction progress", err)
	}
	// With no phase request, catch-up continues immediately at the same source
	// cursor. The configured hour-long idle poll is not involved.
	if err := runtime.awaitNextPass(); err != nil || !reflect.DeepEqual(remainingSources, runtime.sources) {
		t.Fatal("catch-up lost its next unprocessed source", err)
	}
}

func TestEvidenceRelayPhaseSchedulerCanceledRequestAllowsFreshRetry(t *testing.T) {
	t.Parallel()
	runtime, _ := newEvidenceRelayPhaseSchedulerTest(t)
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	result := queuedEvidenceRelayPreparationTest(t, runtime, ctx)
	cancel()
	if err := runtime.awaitNextPass(); err != nil || runtime.prepared {
		t.Fatal("abandoned request stopped the relay or authorized preparation", err)
	}
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatal("caller lost its canceled outcome", err)
	}
	result = queuedEvidenceRelayPreparationTest(t, runtime, t.Context())
	if err := runtime.awaitNextPass(); err != nil {
		t.Fatal("fresh request could not recover", err)
	}
	if err := <-result; err != nil || !runtime.prepared {
		t.Fatal("fresh request did not perform actual preparation admission", err)
	}
}

func TestEvidenceRelayPhaseSchedulerKeepsRealValidationFailuresBlocking(t *testing.T) {
	t.Parallel()
	runtime, fixture := newEvidenceRelayPhaseSchedulerTest(t)
	func() { fixture.stateLock.Lock(); defer fixture.stateLock.Unlock(); fixture.permits[1] = false }()
	result := queuedEvidenceRelayPreparationTest(t, runtime, t.Context())
	if err := runtime.awaitNextPass(); err == nil || runtime.prepared {
		t.Fatal("queued phase request bypassed the actual lost validator permit", err)
	}
	if err := <-result; err == nil {
		t.Fatal("caller received success after actual admission refusal")
	}
}

func TestEvidenceRelayPhaseSchedulerCancellationPreservesAdjacentErrors(t *testing.T) {
	t.Parallel()
	failure := errors.New("synthetic invalid source")
	for _, test := range []struct {
		err        error
		requestErr error
		canceled   bool
	}{
		{err: context.Canceled, requestErr: context.Canceled, canceled: true},
		{err: fmt.Errorf("actual caller: %w", context.DeadlineExceeded), requestErr: context.DeadlineExceeded, canceled: true},
		{err: errors.Join(context.Canceled, fmt.Errorf("read: %w", context.Canceled)), requestErr: context.Canceled, canceled: true},
		{err: errors.Join(context.Canceled, failure), requestErr: context.Canceled},
		{err: context.DeadlineExceeded, requestErr: context.Canceled},
		{err: context.Canceled},
		{err: failure, requestErr: context.Canceled},
		{},
	} {
		if got := evidenceRelayOnlyRequestCancellation(test.err, test.requestErr); got != test.canceled {
			t.Fatalf("caller cancellation=%v error=%v classified=%t", test.requestErr, test.err, got)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	runtime := &evidenceRelayRuntime{ctx: ctx, startupProgress: true, remainingRequests: make(chan evidenceRelayRemainingRequest, 1)}
	request := evidenceRelayRemainingRequest{ctx: t.Context(), result: make(chan error, 1)}
	runtime.remainingRequests <- request
	if err := runtime.awaitNextPass(); !errors.Is(err, context.Canceled) || len(runtime.remainingRequests) != 1 {
		t.Fatal("worker shutdown consumed or approved pending phase work", err)
	}
}
