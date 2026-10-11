package validator

// The production runtime owns all workers through join, then final snapshots
// and resource teardown. A canceled select cannot suppress a durable failure
// returned by an already admitted worker or by its cleanup.

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"reflect"
	"sort"
	"sync"
	"syscall"
	"time"
)

// The concrete production steerer owns its Run lifetime through this method.
type releaseSteererRunner interface {
	Run(context.Context) error
}

// Call-local dependencies retain real production construction at RunRelease.
// Test functions cannot change the shared close/wait/error-selection algorithm.
type releaseRuntimeOperations struct {
	providerRequests func(context.Context) error
	refresh          func(context.Context) error
	newSteerer       func([]*ReleaseMeasurementContext) (releaseSteererRunner, error)
	running          func()
	trailReady       <-chan struct{}
}

// Every non-nil cause must be an explicitly allowed lifecycle result. In
// particular, errors.Is(Canceled) is insufficient for a joined durability error,
// and a fatal trail never becomes ordinary cancellation through its cause.
func releaseOnlyErrors(err error, allowed ...error) bool {
	if err == nil {
		return true
	}
	remaining := 512
	return releaseErrorGraph(err, func(cause error) bool {
		for _, candidate := range allowed {
			if releaseErrorIdentity(cause, candidate) {
				return true
			}
		}
		return false
	}, 0, &remaining)
}

// Only exact concrete identity grants an allowed result. Error implementations
// may contain slices or maps; comparing their interfaces must not panic.
func releaseErrorIdentity(left, right error) bool {
	return left != nil && right != nil && reflect.ValueOf(left).Comparable() && reflect.ValueOf(right).Comparable() && left == right
}

// A bounded concrete search preserves the producer's opaque marker without
// invoking foreign As methods. Presence grants no disposition: callers must
// still check all sibling causes and the marker's own phase/cause semantics.
func releaseErrorMarker[T error](err error) T {
	var result T
	var original error
	remaining := 512
	complete := releaseErrorGraph(err, func(cause error) bool {
		marker, ok := cause.(T)
		if ok {
			if original == nil {
				result, original = marker, cause
			}
			return true
		}
		switch cause.(type) {
		case *releaseObservedNativeRead, *releaseObservedNetworkRead:
			return true
		}
		if _, joined := cause.(interface{ Unwrap() []error }); joined {
			return false
		}
		if _, wrapped := cause.(interface{ Unwrap() error }); wrapped {
			return false
		}
		return true
	}, 0, &remaining)
	if !complete || original == nil {
		var absent T
		return absent
	}
	return result
}

// One finite allowance covers the complete graph. Local storage and fatal
// state errors remain hard before unwrapping their cancellation causes. Nil
// children, typed nils, cycles and oversized joins never become pure success.
func releaseErrorGraph(err error, accept func(error) bool, depth int, remaining *int) bool {
	err = releaseObservedValue(err)
	if err == nil || depth > 32 || *remaining <= 0 {
		return false
	}
	*remaining--
	value := reflect.ValueOf(err)
	switch value.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
		if value.IsNil() {
			return false
		}
	}
	switch cause := err.(type) {
	case *os.PathError, *os.LinkError, *TrailFatalError, *releaseObservedHard, *releaseObservedRefusal:
		return false
	case *releaseObservedNativeRead, *releaseObservedNetworkRead:
		return accept(err)
	case *net.DNSError:
		if cause.IsNotFound {
			return false
		}
	case *artifactUnavailable:
		// This package owns the concrete wrapper. Only its exact allowed
		// identity or complete child graph can authorize a disposition.
	case syscall.Errno:
		return accept(err)
	case interface{ Is(error) bool }, interface{ As(any) bool }:
		return false
	}
	if accept(err) {
		return true
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		causes := joined.Unwrap()
		if len(causes) == 0 || len(causes) > 128 || len(causes) > *remaining {
			return false
		}
		for _, cause := range causes {
			if !releaseErrorGraph(cause, accept, depth+1, remaining) {
				return false
			}
		}
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return releaseErrorGraph(wrapped.Unwrap(), accept, depth+1, remaining)
	}
	return false
}

// Ordinary owner cancellation is not a service failure. Independent errors,
// including a cancellation joined with failed I/O, remain supervisor-visible.
func releaseRuntimeError(ctx context.Context, err error) error {
	err = observeReleaseError(err)
	if err != nil && ctx != nil && ctx.Err() != nil && releaseOnlyErrors(err, context.Canceled, context.DeadlineExceeded) {
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
	runtimeErrors := make(chan error, 2*len(runtimes)+4)
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
	if operations.providerRequests != nil {
		workers.Go(func() {
			if err := releaseRuntimeError(ctx, operations.providerRequests(ctx)); err != nil {
				runtimeErrors <- err
			}
		})
	}
	workers.Go(func() {
		if err := releaseRuntimeError(ctx, operations.refresh(ctx)); err != nil {
			runtimeErrors <- err
		}
	})
	for index, runtime := range runtimes {
		operatorID := cfg.Operators[index].NoID
		concurrency := cfg.Operators[index].Concurrency
		if runtime.authentication != nil {
			workers.Go(func() {
				if err := releaseRuntimeError(ctx, runtime.authentication.run(ctx)); err != nil {
					runtimeErrors <- fmt.Errorf("validator no_id %d authentication: %w", operatorID, err)
				}
			})
		}
		workers.Go(func() {
			if operations.trailReady != nil {
				select {
				case <-ctx.Done():
					return
				case <-operations.trailReady:
				}
			}
			trailCtx := withTrailDiagnosticOperator(ctx, operatorID)
			reportReleaseTrailEngineError(trailCtx, runtime.engine, operatorID, concurrency, runtimeErrors)
		})
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
