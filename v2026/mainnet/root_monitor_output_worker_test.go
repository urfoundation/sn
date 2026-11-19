// Root monitor fixtures retain command completion independently of readiness.
// Admission stays on the caller, and cleanup joins even after a failed barrier.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"testing"
	"time"
)

// The closed completion channel publishes code and permits repeated joins.
// Cancellation is safe concurrently; all other methods belong to the test.
type rootMonitorOutputTestRun struct {
	cancel context.CancelFunc
	done   chan struct{}
	code   int
}

// Callers prepare storage before starting; this worker never enrolls a fixture.
func startRootMonitorOutputTestRun(t *testing.T, ctx context.Context, command func(context.Context) int) *rootMonitorOutputTestRun {
	t.Helper()
	ctx, cancel := context.WithCancel(ctx)
	self := &rootMonitorOutputTestRun{cancel: cancel, done: make(chan struct{})}
	t.Cleanup(func() { joinMonitorTestWorker(t, self.cancel, self.done) })
	go func() {
		defer close(self.done)
		self.code = command(ctx)
	}()
	return self
}

// Already published readiness remains evidence even if the command then exits.
// Rechecking on completion covers publication between the first check and wait.
func (self *rootMonitorOutputTestRun) waitEvent(ctx context.Context, event <-chan struct{}) error {
	select {
	case <-event:
		return nil
	default:
	}
	select {
	case <-event:
		return nil
	case <-self.done:
		select {
		case <-event:
			return nil
		default:
		}
		return fmt.Errorf("root monitor worker exited before event (code=%d)", self.code)
	case <-ctx.Done():
		return ctx.Err()
	}
}

// The real event orders the assertion; the deadline is only a failure backstop.
func (self *rootMonitorOutputTestRun) event(t *testing.T, event <-chan struct{}) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	if err := self.waitEvent(ctx, event); err != nil {
		t.Fatal(err)
	}
}

// Known completion refuses new work even when the channel can accept a send.
// Unlike retained readiness, a new offer has no evidence to preserve after exit.
func (self *rootMonitorOutputTestRun) waitResume(ctx context.Context, resume chan<- struct{}) error {
	select {
	case <-self.done:
		return fmt.Errorf("root monitor worker exited before resume (code=%d)", self.code)
	default:
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case resume <- struct{}{}:
		return nil
	case <-self.done:
		return fmt.Errorf("root monitor worker exited before resume (code=%d)", self.code)
	case <-ctx.Done():
		return ctx.Err()
	}
}

// No test can strand itself offering work to a command that has already stopped.
func (self *rootMonitorOutputTestRun) resume(t *testing.T, resume chan<- struct{}) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	if err := self.waitResume(ctx, resume); err != nil {
		t.Fatal(err)
	}
}

// Joining does not consume completion, so normal assertions and cleanup agree.
func (self *rootMonitorOutputTestRun) join(t *testing.T) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	select {
	case <-self.done:
		return self.code
	case <-ctx.Done():
		t.Fatal("root monitor worker did not join", ctx.Err())
		return 0
	}
}

// The actual missing-declaration refusal completes before either wait begins.
// Supplying only the fixture reference then reaches the same real blocked Rpc.
func TestRootMonitorOutputWorkerReportsMissingCustodyBeforeRpc(t *testing.T) {
	_, fixture := newRootFixture(t)
	url, entered, left := monitorServicesBlockedChain(t)
	metrics := filepath.Join(monitorMetricsTestDir(t), "root.prom")
	args := []string{"root-monitor", "--rpc", url, "--policy", rootTestPolicyFile(t, fixture.policy), "--retry-window", "60s", "--metrics-file", metrics, "--metrics-role", "primary"}
	command := func(ctx context.Context) int { return runMain(ctx, args, io.Discard, io.Discard) }
	refused := startRootMonitorOutputTestRun(t, t.Context(), command)
	if code := refused.join(t); code != 2 {
		t.Fatal("missing durable declaration was not refused", code)
	}
	select {
	case <-entered:
		t.Fatal("missing custody reached the chain")
	default:
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	if err := refused.waitEvent(ctx, entered); err == nil || err.Error() != "root monitor worker exited before event (code=2)" {
		t.Fatal("early admission exit did not release readiness wait", err)
	}
	resume := make(chan struct{}, 1)
	if err := refused.waitResume(ctx, resume); err == nil || err.Error() != "root monitor worker exited before resume (code=2)" {
		t.Fatal("early admission exit did not release resume wait", err)
	}
	select {
	case <-resume:
		t.Fatal("completed command accepted another sample")
	default:
	}
	if code := refused.join(t); code != 2 {
		t.Fatal("observing failure consumed the command result", code)
	}
	storageCtx := monitorTestStorageContext(t, t.Context(), args)
	admitted := startRootMonitorOutputTestRun(t, storageCtx, command)
	admitted.event(t, entered)
	admitted.cancel()
	if code := admitted.join(t); code != 0 {
		t.Fatal("admitted blocked read did not join cancellation", code)
	}
	monitorTestEvent(t, nil, left)
}

// A canceled caller need not wait for a still-owned command to emit readiness.
// The explicit entered/left barriers also prove cancellation joins that worker.
func TestRootMonitorOutputWorkerWaitsObserveCancellationAndJoin(t *testing.T) {
	entered, left := make(chan struct{}), make(chan struct{})
	run := startRootMonitorOutputTestRun(t, t.Context(), func(ctx context.Context) int {
		close(entered)
		<-ctx.Done()
		close(left)
		return 0
	})
	run.event(t, entered)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := run.waitEvent(ctx, make(chan struct{})); !errors.Is(err, context.Canceled) {
		t.Fatal("event wait ignored caller cancellation", err)
	}
	if err := run.waitResume(ctx, make(chan struct{})); !errors.Is(err, context.Canceled) {
		t.Fatal("resume wait ignored caller cancellation", err)
	}
	run.cancel()
	if code := run.join(t); code != 0 {
		t.Fatal("worker changed its cancellation result", code)
	}
	// Both channels are now closed, so this is a forced state, not a race.
	if err := run.waitEvent(t.Context(), entered); err != nil {
		t.Fatal("command completion erased already published readiness", err)
	}
	monitorTestEvent(t, nil, left)
}
