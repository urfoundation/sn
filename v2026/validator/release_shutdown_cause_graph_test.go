// Error-shape controls run at the real worker owner and its shared classifier.
// Original causes stay visible; no foreign matcher can grant cancellation.
package validator

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// A finite diagnostic keeps cycle failures about traversal, not formatting.
type releaseCauseJoin struct {
	causes []error
	visits int
}

// Do not recursively print a deliberately cyclic test graph.
func (*releaseCauseJoin) Error() string { return "synthetic release cause graph" }

// The guard makes an unbounded regression fail deterministically in its owner.
func (self *releaseCauseJoin) Unwrap() []error {
	self.visits++
	if self.visits > 8192 {
		panic("release cause traversal escaped its finite allowance")
	}
	return self.causes
}

// A nil receiver would panic if classification called Unwrap before admission.
type releaseCauseWrapper struct{ cause error }

// Keep diagnostics independent of the original cause's shape.
func (*releaseCauseWrapper) Error() string { return "synthetic release wrapper" }

// Preserve an exact child, including an intentionally invalid nil branch.
func (self *releaseCauseWrapper) Unwrap() error { return self.cause }

// Custom matching cannot impersonate a pending marker or cancellation.
type releaseCauseMatcher struct{ cause error }

// Diagnostic text grants no lifecycle authority.
func (*releaseCauseMatcher) Error() string { return "synthetic foreign release matcher" }

// Its real child is insufficient authority when the wrapper is foreign.
func (self *releaseCauseMatcher) Unwrap() error { return self.cause }

// Neither classifier nor marker search may invoke this method.
func (*releaseCauseMatcher) Is(error) bool { panic("release invoked foreign Is") }

// Neither classifier nor marker search may invoke this method.
func (*releaseCauseMatcher) As(any) bool { panic("release invoked foreign As") }

// Exact identity checks must safely refuse non-comparable error values.
type releaseUncomparableCause []byte

// No content-derived marker or identity is available for this value.
func (releaseUncomparableCause) Error() string { return "synthetic uncomparable release cause" }

// Canceled file operations are supplied at their worker boundary. The release
// owner still joins every worker and performs a real final Stats save.
func TestReleaseShutdownKeepsFileCancellationAfterWorkerJoin(t *testing.T) {
	for _, link := range []bool{false, true} {
		cfg, runtime := newReleaseShutdownTestRuntime(t)
		path := filepath.Join(cfg.Operators[0].StateDir, "synthetic-worker-state")
		var cause error = &os.PathError{Op: "write", Path: path, Err: context.Canceled}
		if link {
			cause = &os.LinkError{Op: "rename", Old: path, New: path + ".next", Err: context.DeadlineExceeded}
		}
		var joined atomic.Int32
		operations := releaseShutdownTestOperations(nil)
		operations.refresh = func(ctx context.Context) error {
			<-ctx.Done()
			joined.Add(1)
			return cause
		}
		operations.newSteerer = func([]*ReleaseMeasurementContext) (releaseSteererRunner, error) {
			return releaseShutdownSteererFunc(func(ctx context.Context) error {
				<-ctx.Done()
				joined.Add(1)
				return ctx.Err()
			}), nil
		}
		runtime.engine = releaseShutdownTrailFunc(func(ctx context.Context, _ int) error {
			<-ctx.Done()
			joined.Add(1)
			return ctx.Err()
		})
		writes, closes := 0, 0
		runtime.stats.writeHooks.writeSnapshot = func(directory *statsSnapshotDirectory, write statsSnapshotWrite) error {
			writes++
			if joined.Load() != 3 {
				return errors.New("final save preceded joined workers")
			}
			return writeStatsSnapshotOwned(directory, write)
		}
		runtime.close = newReleaseOperatorClose(runtime.stats, cfg.Operators[0].StateDir, func() error { closes++; return nil })
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		operations.running = cancel
		err := runReleaseOperatorWorkers(ctx, cancel, cfg, []*releaseOperatorRuntime{runtime}, operations)
		cancel()
		if joined.Load() != 3 || writes != 1 || closes != 1 || !errors.Is(err, cause) {
			t.Fatalf("file cancellation escaped the real shutdown owner: link=%t joined=%d writes=%d closes=%d error=%v", link, joined.Load(), writes, closes, err)
		}
	}
}

// Malformed cancellation graphs must be returned by the actual joined owner.
// Its ordinary cancellation control still produces a successful shutdown.
func TestReleaseShutdownRejectsIncompleteCancellationGraphs(t *testing.T) {
	cycle := &releaseCauseJoin{}
	cycle.causes = []error{context.Canceled, cycle}
	var typedNil *releaseCauseWrapper
	for _, cause := range []error{
		cycle, typedNil, &releaseCauseWrapper{},
		&releaseCauseJoin{causes: []error{context.Canceled, nil}},
		&releaseCauseMatcher{cause: context.Canceled},
		&net.DNSError{Name: "resolver.example", IsNotFound: true, UnwrapErr: context.Canceled},
	} {
		cfg, runtime := newReleaseShutdownTestRuntime(t)
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		operations := releaseShutdownTestOperations(cancel)
		operations.refresh = func(ctx context.Context) error { <-ctx.Done(); return cause }
		err := runReleaseOperatorWorkers(ctx, cancel, cfg, []*releaseOperatorRuntime{runtime}, operations)
		cancel()
		if err == nil {
			t.Fatal("malformed worker cancellation became successful shutdown")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	pure := errors.Join(fmt.Errorf("owned worker: %w", context.Canceled), context.DeadlineExceeded)
	if releaseRuntimeError(ctx, pure) != nil || releaseRuntimeError(ctx, newArtifactUnavailable(context.Canceled)) != nil || !releaseOnlyErrors(nil, context.Canceled) {
		t.Fatal("ordinary owner cancellation lost its explicit success result")
	}
}

// Width, depth and total occurrences each have an independent finite limit.
// A shared branch is counted again; a graph cannot reset its global allowance.
func TestReleaseCancellationGraphHonorsCompleteFiniteAllowance(t *testing.T) {
	for _, leaves := range []int{126, 127} {
		root := &releaseCauseJoin{}
		for group := 0; group < 4; group++ {
			count := 127
			if group == 3 {
				count = leaves
			}
			branch := &releaseCauseJoin{causes: make([]error, count)}
			for index := range branch.causes {
				branch.causes[index] = context.Canceled
			}
			root.causes = append(root.causes, branch)
		}
		if allowed := releaseOnlyErrors(root, context.Canceled); allowed != (leaves == 126) {
			t.Fatalf("complete graph node allowance differs: final leaves=%d allowed=%t", leaves, allowed)
		}
	}
	for _, count := range []int{128, 129} {
		root := &releaseCauseJoin{causes: make([]error, count)}
		for index := range root.causes {
			root.causes[index] = context.Canceled
		}
		if allowed := releaseOnlyErrors(root, context.Canceled); allowed != (count == 128) {
			t.Fatalf("joined child allowance differs: children=%d allowed=%t", count, allowed)
		}
	}
	for _, depth := range []int{32, 33} {
		var root error = context.Canceled
		for index := 0; index < depth; index++ {
			root = &releaseCauseWrapper{cause: root}
		}
		if allowed := releaseOnlyErrors(root, context.Canceled); allowed != (depth == 32) {
			t.Fatalf("wrapped depth allowance differs: depth=%d allowed=%t", depth, allowed)
		}
	}
	uncomparable := releaseUncomparableCause{1}
	if releaseOnlyErrors(uncomparable, uncomparable) {
		t.Fatal("non-comparable error borrowed an exact identity grant")
	}
	marker := &productionPendingReconciliation{nativeEpoch: 7}
	if !releaseOnlyErrors(fmt.Errorf("original owner: %w", marker), marker) {
		t.Fatal("an exact opaque pending owner lost its allowed identity")
	}
}
