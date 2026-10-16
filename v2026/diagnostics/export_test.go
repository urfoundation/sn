// Exporter tests force actual queue, ownership and write boundaries. Timeouts
// are cancellation guards, never evidence that an unobserved worker progressed.
package diagnostics

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// The first actual write holds the sole worker while producers fill queues.
type barrierWriter struct {
	entered    chan string
	release    chan struct{}
	calls      atomic.Int64
	partial    bool
	panicWrite bool
}

// The old synchronous interface is deliberately unusable.
func (self *barrierWriter) Write([]byte) (int, error) { panic("synchronous diagnostic write") }

// Every blocked operation owns and obeys its real cancellation context.
func (self *barrierWriter) WriteContext(ctx context.Context, raw []byte) (int, error) {
	self.entered <- string(raw)
	if self.calls.Add(1) == 1 {
		select {
		case <-self.release:
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}
	if self.panicWrite {
		panic("synthetic sink defect")
	}
	if self.partial {
		return 1, io.ErrUnexpectedEOF
	}
	return len(raw), nil
}

// Constructing this explicit cancellation adapter does not acquire any worker.
func newBarrierWriter() *barrierWriter {
	return &barrierWriter{entered: make(chan string, 32), release: make(chan struct{})}
}

// A noisy domain cannot borrow a peer's queue; owned copies survive mutation.
func TestExporterBoundsDomainsAndCopiesRecords(t *testing.T) {
	sink := newBarrierWriter()
	owner, err := New(t.Context(), sink, []string{"alpha", "beta"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	// Only the attempt clock is held at this explicit barrier. Actual Close
	// still cancels and joins the context-aware write if the test fails.
	owner.withTimeout = func(ctx context.Context, _ time.Duration) (context.Context, context.CancelFunc) {
		return context.WithCancel(ctx)
	}
	completed := make(chan struct{}, 4)
	owner.afterWrite = func() { completed <- struct{}{} }
	if !owner.Offer("alpha", []byte("first")) {
		t.Fatal("first admission")
	}
	if <-sink.entered != "first" {
		t.Fatal("wrong active record")
	}
	raw := []byte("original")
	if !owner.Offer("alpha", raw) || !owner.Offer("alpha", []byte("last")) {
		t.Fatal("bounded alpha queue refused")
	}
	copy(raw, []byte("mutation"))
	if owner.Offer("alpha", []byte("excess")) || owner.Offer("beta", bytes.Repeat([]byte("x"), MaximumRecordBytes+1)) {
		t.Fatal("count/byte bound escaped")
	}
	if !owner.Offer("beta", []byte("peer")) {
		t.Fatal("noisy alpha consumed beta capacity")
	}
	before := owner.Snapshot("alpha")
	if before.Delivered != 0 || before.Dropped != 1 {
		t.Fatal("queue was acknowledged as delivery", before)
	}
	close(sink.release)
	for range 4 {
		<-completed
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"peer", "original", "last"} {
		if observed := <-sink.entered; observed != expected {
			t.Fatalf("fair immutable order: %q != %q", observed, expected)
		}
	}
	if before.Delivered != 0 || owner.Snapshot("alpha").Delivered != 3 || owner.Snapshot("beta").Delivered != 1 {
		t.Fatal("mutable snapshot or lost completion")
	}
}

// A failed prefix is never followed by another record on that destination.
func TestExporterPartialWriteDisablesWithoutSplicing(t *testing.T) {
	sink := newBarrierWriter()
	sink.partial = true
	owner, err := New(t.Context(), sink, []string{"alpha", "beta"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	owner.withTimeout = func(ctx context.Context, _ time.Duration) (context.Context, context.CancelFunc) {
		return context.WithCancel(ctx)
	}
	completed := make(chan struct{})
	owner.wait = func(ctx context.Context, _ time.Duration) bool { close(completed); <-ctx.Done(); return false }
	owner.Offer("alpha", []byte("partial"))
	<-sink.entered
	owner.Offer("beta", []byte("must-not-splice"))
	close(sink.release)
	<-completed
	if owner.Offer("beta", []byte("later")) {
		t.Fatal("partial output destination accepted a record after ambiguous bytes")
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	if sink.calls.Load() != 1 || owner.Snapshot("alpha").Unavailable != 1 || owner.Snapshot("beta").Dropped != 2 {
		t.Fatal("partial output was reused")
	}
}

// Only explicitly admitted cancellation contracts can reach a worker.
type refusedWriter struct{ calls atomic.Int64 }

// A bounded counting fixture proves an arbitrary Writer was never called.
func (self *refusedWriter) Write(raw []byte) (int, error) { self.calls.Add(1); return len(raw), nil }

// Regular files retain their exact contents and unknown callbacks are unused.
func TestExporterRefusesUninterruptibleSinksAndInvalidCensus(t *testing.T) {
	unknown := &refusedWriter{}
	if owner, err := New(t.Context(), unknown, []string{"same", "same"}); owner != nil || err == nil {
		t.Fatal("duplicate census admitted")
	}
	path := filepath.Join(t.TempDir(), "ordinary.log")
	if err := os.WriteFile(path, []byte("retained"), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	for _, sink := range []io.Writer{unknown, file, (*barrierWriter)(nil)} {
		owner, err := New(t.Context(), sink, []string{"alpha"})
		if err != nil {
			t.Fatal(err)
		}
		if owner.Offer("alpha", []byte("must-not-write")) || owner.Snapshot("alpha").Outcome != "unavailable" {
			t.Fatal("unsupported sink accepted")
		}
		if err := owner.Close(); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != "retained" || unknown.calls.Load() != 0 {
		t.Fatal("refused destination invoked", err)
	}
}

// Caller ownership ends at successful Offer, not at later sink readiness.
func TestExporterCopiesBeforeCallerMutation(t *testing.T) {
	sink := newBarrierWriter()
	owner, err := New(t.Context(), sink, []string{"alpha"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	owner.withTimeout = func(ctx context.Context, _ time.Duration) (context.Context, context.CancelFunc) {
		return context.WithCancel(ctx)
	}
	completed := make(chan struct{}, 2)
	owner.afterWrite = func() { completed <- struct{}{} }
	owner.Offer("alpha", []byte("barrier"))
	<-sink.entered
	raw := []byte("original")
	if !owner.Offer("alpha", raw) {
		t.Fatal("second record refused")
	}
	copy(raw, []byte("mutation"))
	close(sink.release)
	for range 2 {
		<-completed
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	if observed := <-sink.entered; observed != "original" {
		t.Fatal("queued record aliased caller memory", observed)
	}
}

// Counter overflow, memory capture and a fresh process instance stay bounded.
func TestExporterSaturatesCountersAndResetsOnlyWithNewOwner(t *testing.T) {
	owner, err := New(t.Context(), &refusedWriter{}, []string{"alpha"})
	if err != nil {
		t.Fatal(err)
	}
	owner.stateLock.Lock()
	owner.domains[0].state.Dropped = math.MaxUint64 - 1
	owner.domains[0].state.DroppedBytes = math.MaxUint64 - 1
	owner.stateLock.Unlock()
	owner.Offer("alpha", []byte("xx"))
	owner.Offer("alpha", []byte("xx"))
	if state := owner.Snapshot("alpha"); state.Dropped != math.MaxUint64 || state.DroppedBytes != math.MaxUint64 {
		t.Fatal("counter wrapped", state)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal("idempotent close", err)
	}
	buffer := bytes.NewBufferString(strings.Repeat("x", maximumMemoryBytes))
	next, err := New(t.Context(), buffer, []string{"alpha"})
	if err != nil {
		t.Fatal(err)
	}
	if next.Snapshot("alpha").Dropped != 0 {
		t.Fatal("new owner inherited counters")
	}
	completed := make(chan struct{}, 1)
	next.afterWrite = func() { completed <- struct{}{} }
	next.Offer("alpha", []byte("excess"))
	<-completed
	if err := next.Close(); err != nil {
		t.Fatal(err)
	}
	if buffer.Len() != maximumMemoryBytes || next.Snapshot("alpha").Unavailable != 1 {
		t.Fatal("memory capture escaped its bound")
	}
}

// Core cancellation does not abandon the owner; explicit Close joins its write.
func TestExporterCloseJoinsCanceledSinkAndSurvivesSinkPanic(t *testing.T) {
	for _, panicWrite := range []bool{false, true} {
		sink := newBarrierWriter()
		sink.panicWrite = panicWrite
		ctx, cancel := context.WithCancel(t.Context())
		owner, err := New(ctx, sink, []string{"alpha"})
		if err != nil {
			t.Fatal(err)
		}
		owner.Offer("alpha", []byte("active"))
		<-sink.entered
		cancel()
		if panicWrite {
			close(sink.release)
		}
		if err := owner.Close(); err != nil {
			t.Fatal(err)
		}
		select {
		case <-owner.done:
		default:
			t.Fatal("close left owner running")
		}
		if owner.Snapshot("alpha").Dropped != 1 || owner.Snapshot("alpha").Delivered != 0 || !errors.Is(owner.ctx.Err(), context.Canceled) {
			t.Fatal("failed actual write acknowledged")
		}
	}
}
