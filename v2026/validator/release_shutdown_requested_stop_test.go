package validator

// A stop the owner requests cancels work that is still in flight, among it the
// tunnel generator's final identity retirement. That cancellation is the stop
// itself, not a failure; runReleaseConfig panics on any error, so returning it
// turned every systemd stop into exit status 2. File and ledger errors seen at
// the same teardown stay fatal.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The production shape of the 11-03 stop panics: connect records the retirement
// stage over the canceled request, sn's tunnel transport names the generator,
// and release_run.go names the tunnel transport inside the resource shutdown.
func releaseShutdownCanceledRetirement() error {
	retirement := fmt.Errorf("client retirement %s: %w", "identity", context.Canceled)
	return releaseStageError("tunnel transport", errors.Join(fmt.Errorf("close tunnel generator: %w", retirement)))
}

func runReleaseShutdownRequestedStop(t *testing.T, cfg *ReleaseConfig, runtime *releaseOperatorRuntime) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	// running is called once every worker is admitted; canceling there is the
	// owner's stop request, as SIGTERM cancels event.Ctx() in production.
	return runReleaseOperatorWorkers(ctx, cancel, cfg, []*releaseOperatorRuntime{runtime}, releaseShutdownTestOperations(cancel))
}

func TestReleaseShutdownRequestedStopEndsCleanThroughCanceledRetirement(t *testing.T) {
	cfg, runtime := newReleaseShutdownTestRuntime(t)
	closes := 0
	runtime.close = newReleaseOperatorClose(runtime.stats, cfg.Operators[0].StateDir, func() error {
		closes++
		return releaseShutdownCanceledRetirement()
	})
	err := runReleaseShutdownRequestedStop(t, cfg, runtime)
	if closes != 1 {
		t.Fatalf("the requested stop closed the operator resources %d times", closes)
	}
	if err != nil {
		t.Fatalf("a requested stop returned its own canceled retirement, which panics the process with exit status 2: %v", err)
	}
	// The real final save still ran before the resources closed.
	if _, statErr := os.Stat(filepath.Join(cfg.Operators[0].StateDir, "stats.json")); statErr != nil {
		t.Fatalf("the clean stop skipped the final stats save: %v", statErr)
	}
}

func TestReleaseShutdownRequestedStopKeepsFileAndLedgerCloseErrors(t *testing.T) {
	ledgerErr := &os.PathError{Op: "close", Path: "attempt-ledger.jsonl", Err: os.ErrClosed}
	for _, test := range []struct {
		name      string
		resources func() error
		obstruct  bool
		want      func(error) bool
	}{
		{
			name: "ledger close beside the canceled retirement",
			resources: func() error {
				return errors.Join(releaseShutdownCanceledRetirement(),
					releaseStageError("validator no_id 9 attempt ledger shutdown", ledgerErr))
			},
			want: func(err error) bool { return errors.Is(err, ledgerErr) },
		},
		{
			name: "ledger close wrapping cancellation",
			resources: func() error {
				return releaseStageError("validator no_id 9 attempt ledger shutdown",
					&os.PathError{Op: "sync", Path: "attempt-ledger.jsonl", Err: context.Canceled})
			},
			want: func(err error) bool {
				var pathErr *os.PathError
				return errors.As(err, &pathErr) && pathErr.Op == "sync"
			},
		},
		{
			name:      "final stats save beside the canceled retirement",
			resources: releaseShutdownCanceledRetirement,
			obstruct:  true,
			want: func(err error) bool {
				var linkErr *os.LinkError
				return errors.As(err, &linkErr)
			},
		},
		{
			name: "ordinary resource failure beside the canceled retirement",
			resources: func() error {
				return errors.Join(releaseShutdownCanceledRetirement(), errors.New("remove tunnel identity: response rejected"))
			},
			want: func(err error) bool { return err != nil },
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg, runtime := newReleaseShutdownTestRuntime(t)
			if test.obstruct {
				obstructReleaseShutdownSnapshot(t, cfg.Operators[0].StateDir)
			}
			runtime.close = newReleaseOperatorClose(runtime.stats, cfg.Operators[0].StateDir, test.resources)
			err := runReleaseShutdownRequestedStop(t, cfg, runtime)
			if err == nil || !test.want(err) {
				t.Fatalf("a requested stop hid a durable close failure: %v", err)
			}
		})
	}
}

// A run that ends on a worker failure is not a requested stop: its result keeps
// the worker's error and every close error, cancellation included.
func TestReleaseShutdownWorkerFailureKeepsItsCloseErrors(t *testing.T) {
	cfg, runtime := newReleaseShutdownTestRuntime(t)
	runtime.close = newReleaseOperatorClose(runtime.stats, cfg.Operators[0].StateDir, releaseShutdownCanceledRetirement)
	workerErr := errors.New("test-owned steering failure")
	operations := releaseShutdownTestOperations(func() {})
	operations.newSteerer = func([]*ReleaseMeasurementContext) (releaseSteererRunner, error) {
		return releaseShutdownSteererFunc(func(context.Context) error { return workerErr }), nil
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	err := runReleaseOperatorWorkers(ctx, cancel, cfg, []*releaseOperatorRuntime{runtime}, operations)
	if !errors.Is(err, workerErr) || !errors.Is(err, context.Canceled) {
		t.Fatalf("a worker failure lost its own error or its close errors: %v", err)
	}
}
