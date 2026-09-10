package validator

// The production runtime owns all workers through join, then final snapshots
// and resource teardown. A canceled select cannot suppress a durable failure
// returned by an already admitted worker or by its cleanup.

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// The concrete production steerer owns its Run lifetime through this method.
type releaseSteererRunner interface {
	Run(context.Context) error
}

// Call-local dependencies retain real production construction at RunRelease.
// Test functions cannot change the shared close/wait/error-selection algorithm.
type releaseRuntimeOperations struct {
	refresh    func(context.Context) error
	newSteerer func([]*ReleaseMeasurementContext) (releaseSteererRunner, error)
	running    func()
}

// Every non-nil cause must be an explicitly allowed lifecycle result. In
// particular, errors.Is(Canceled) is insufficient for a joined durability error,
// and a fatal trail never becomes ordinary cancellation through its cause.
func releaseOnlyErrors(err error, allowed ...error) bool {
	if err == nil {
		return true
	}
	if _, fatal := err.(*TrailFatalError); fatal {
		return false
	}
	for _, candidate := range allowed {
		if err == candidate {
			return true
		}
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		causes := joined.Unwrap()
		if len(causes) == 0 {
			return false
		}
		for _, cause := range causes {
			if !releaseOnlyErrors(cause, allowed...) {
				return false
			}
		}
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		cause := wrapped.Unwrap()
		return cause != nil && releaseOnlyErrors(cause, allowed...)
	}
	return false
}

// Ordinary owner cancellation is not a service failure. Independent errors,
// including a cancellation joined with failed I/O, remain supervisor-visible.
func releaseRuntimeError(ctx context.Context, err error) error {
	if err != nil && ctx.Err() != nil && releaseOnlyErrors(err, context.Canceled, context.DeadlineExceeded) {
		return nil
	}
	return err
}

// Avoids creating an error for a successful stage while preserving errors.Is.
func releaseStageError(stage string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", stage, err)
}

// One owner saves once and joins every resource even if saving fails. Concurrent
// callers wait for the first teardown and receive its same completed result.
func newReleaseOperatorClose(stats *StatsEngine, stateDir string, closeResources func() error) func() error {
	var closeOnce sync.Once
	var closeErr error
	return func() error {
		closeOnce.Do(func() {
			saveErr := stats.Save(stateDir)
			resourceErr := closeResources()
			closeErr = errors.Join(releaseStageError("final stats save", saveErr), releaseStageError("resource shutdown", resourceErr))
		})
		return closeErr
	}
}

// RunRelease retains acquired ledgers through all worker/resource joins and
// final snapshots, including partial startup. No proof-store Close API exists;
// its operation-owned file errors arrive through the fatal trail result.
func closeReleaseAttemptStates(states map[uint64]*releaseAttemptState) error {
	noIDs := make([]uint64, 0, len(states))
	for noID := range states {
		noIDs = append(noIDs, noID)
	}
	sort.Slice(noIDs, func(i, j int) bool { return noIDs[i] < noIDs[j] })
	var closeErr error
	for _, noID := range noIDs {
		state := states[noID]
		if state == nil || state.ledger == nil {
			closeErr = errors.Join(closeErr, fmt.Errorf("validator no_id %d acquired state has no ledger owner", noID))
			continue
		}
		closeErr = errors.Join(closeErr, releaseStageError(fmt.Sprintf("validator no_id %d attempt ledger shutdown", noID), state.ledger.Close()))
	}
	return closeErr
}

// Every process-owned worker joins before the same operator teardown callbacks;
// the public RunRelease returns this operation's result directly.
func runReleaseOperatorWorkers(ctx context.Context, cancel context.CancelFunc, cfg *ReleaseConfig, runtimes []*releaseOperatorRuntime, operations releaseRuntimeOperations) (returnErr error) {
	runtimeErrors := make(chan error, len(runtimes)+3)
	var workers sync.WaitGroup
	defer func() {
		cancel()
		workers.Wait()
		// Each admitted producer sends at most one result. After joining them,
		// no late send can race this close or escape the returned error.
		close(runtimeErrors)
		for err := range runtimeErrors {
			returnErr = errors.Join(returnErr, err)
		}
		for index, runtime := range runtimes {
			returnErr = errors.Join(returnErr, releaseStageError(fmt.Sprintf("validator no_id %d shutdown", cfg.Operators[index].NoID), runtime.close()))
		}
	}()
	workers.Go(func() {
		if err := releaseRuntimeError(ctx, operations.refresh(ctx)); err != nil {
			runtimeErrors <- err
		}
	})
	for index, runtime := range runtimes {
		operatorID := cfg.Operators[index].NoID
		concurrency := cfg.Operators[index].Concurrency
		workers.Go(func() { reportReleaseTrailEngineError(ctx, runtime.engine, operatorID, concurrency, runtimeErrors) })
	}
	measurements := make([]*ReleaseMeasurementContext, len(runtimes))
	for i, runtime := range runtimes {
		measurements[i] = runtime.measurement
	}
	steerer, err := operations.newSteerer(measurements)
	if err != nil {
		return err
	}
	workers.Go(func() {
		if err := releaseRuntimeError(ctx, steerer.Run(ctx)); err != nil {
			runtimeErrors <- err
		}
	})
	workers.Go(func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				for i, runtime := range runtimes {
					if err := runtime.stats.Save(cfg.Operators[i].StateDir); err != nil {
						runtimeErrors <- fmt.Errorf("validator no_id %d stats save: %w", cfg.Operators[i].NoID, err)
						return
					}
				}
			}
		}
	})
	operations.running()
	select {
	case <-ctx.Done():
		return nil
	case err := <-runtimeErrors:
		return err
	}
}
