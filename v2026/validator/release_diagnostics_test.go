//go:build linux || darwin

// These tests exercise actual loop, publisher and public lifecycle boundaries.
// Output failures never supply protocol admission or change retry decisions.
package validator

import (
	"bytes"
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/crv4"
)

// The destination deliberately remains congested until the real context ends.
type releaseDiagnosticBlockedSink struct {
	entered, left  chan struct{}
	once, leftOnce sync.Once
}

// A known finite memory sink reports actual writes before lifecycle shutdown.
// Tests do not rely on Close's optional 250ms drain to deliver a queued record.
type releaseDiagnosticCapture struct {
	bytes.Buffer
	written chan struct{}
}

// Only fixed test record counts enter this bounded channel.
func (self *releaseDiagnosticCapture) WriteContext(ctx context.Context, raw []byte) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	count, err := self.Buffer.Write(raw)
	self.written <- struct{}{}
	return count, err
}

// There is no synchronous fallback for a long-lived validator diagnostic.
func (self *releaseDiagnosticBlockedSink) Write([]byte) (int, error) {
	panic("synchronous validator output")
}

// The exporter owns and joins every actual invocation.
func (self *releaseDiagnosticBlockedSink) WriteContext(ctx context.Context, _ []byte) (int, error) {
	self.once.Do(func() { close(self.entered) })
	<-ctx.Done()
	self.leftOnce.Do(func() { close(self.left) })
	return 0, ctx.Err()
}

// An original cause remains usable by typed classification without formatting.
type releaseDiagnosticUnformattedError struct{ cause error }

// A raw diagnostic formatter is an immediate deterministic regression.
func (self *releaseDiagnosticUnformattedError) Error() string { panic("raw read cause was formatted") }

// Existing owner classification still sees the genuine typed cause.
func (self *releaseDiagnosticUnformattedError) Unwrap() error { return self.cause }

// Receipt retries continue beyond the hard-error budget while their log pipe
// is blocked; progress remains read-wait at the original native epoch.
func TestReleaseDiagnosticsBlockedSinkPreservesSteeringAndJoins(t *testing.T) {
	sink := &releaseDiagnosticBlockedSink{entered: make(chan struct{}), left: make(chan struct{})}
	ctx, owner, err := newReleaseDiagnostics(t.Context(), sink, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.close() })
	progress, _ := newReleaseProgressTest(t)
	progress.diagnostics = owner
	attempts := 0
	err = runReleaseProductionSteeringLoopWithWait(ctx, func() error {
		attempts++
		return &productionSteeringReadWait{phase: productionReadReceipt, nativeEpoch: 7, epochKnown: true, cause: &releaseDiagnosticUnformattedError{cause: context.DeadlineExceeded}}
	}, func() bool {
		if attempts == 1 {
			<-sink.entered
		}
		return attempts < releaseSteeringFailureLimit+6
	}, progress)
	if err != nil || attempts != releaseSteeringFailureLimit+6 {
		t.Fatal("diagnostic congestion changed retry authority")
	}
	value := releaseProgressTestSnapshot(t, progress)
	if value.Steering.Current || value.Steering.NativeEpoch != 7 || value.Steering.Outcome != "receipt_transport_wait" || value.Diagnostics.Steering.Delivered != 0 || value.Diagnostics.Steering.Dropped == 0 {
		t.Fatal("blocked logs refreshed protocol success or lacked drop evidence")
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	<-sink.left
}

// Closed facts retain phase, original epoch and typed cause without exposing
// transport strings. Unknown scheduler epoch remains explicitly unknown.
func TestReleaseDiagnosticsRetainsClosedReadFacts(t *testing.T) {
	for _, candidate := range []struct {
		cause    error
		expected string
		known    bool
	}{
		{cause: &releaseDiagnosticUnformattedError{cause: context.DeadlineExceeded}, expected: "unknown", known: true},
		{cause: &url.Error{Op: "Get", URL: "https://synthetic.example", Err: context.DeadlineExceeded}, expected: "timeout", known: true},
		{cause: &crv4.ReceiptEvidenceUnavailableError{}, expected: "unavailable", known: false},
	} {
		output := &releaseDiagnosticCapture{written: make(chan struct{}, 2)}
		ctx, owner, err := newReleaseDiagnostics(t.Context(), output, time.Now)
		if err != nil {
			t.Fatal(err)
		}
		err = runReleaseProductionSteeringLoopWithWait(ctx, func() error {
			return &productionSteeringReadWait{phase: productionReadApplication, nativeEpoch: 7, epochKnown: candidate.known, cause: candidate.cause}
		}, func() bool { return false }, nil)
		if err != nil {
			t.Fatal("read wait became a hard failure")
		}
		<-output.written
		if err := owner.close(); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(output.String(), "phase=application cause="+candidate.expected) || !strings.Contains(output.String(), "code=read_wait") {
			t.Fatal("closed cause or phase lost", output.String())
		}
		if !candidate.known && !strings.Contains(output.String(), "native_epoch=0 epoch_known=false") {
			t.Fatal("unknown schedule invented epoch", output.String())
		}
	}
}

// Startup reports typed outage and remains cancelable at the real retry wait.
func TestReleaseDiagnosticsStartupWaitCannotBlockCancellation(t *testing.T) {
	sink := &releaseDiagnosticBlockedSink{entered: make(chan struct{}), left: make(chan struct{})}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ctx, owner, err := newReleaseDiagnostics(ctx, sink, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.close() })
	done := make(chan error, 1)
	go func() {
		done <- awaitProductionStartupStage(ctx, &ReleaseConfig{SchemaVersion: ReleaseMainnetProductionSchemaVersion, PollSeconds: 3600}, nil, "native identity", func(context.Context) error {
			return &releaseDiagnosticUnformattedError{cause: context.DeadlineExceeded}
		})
	}()
	<-sink.entered
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal("startup did not leave its real wait")
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	<-sink.left
}

// The actual optional file publisher can repair its output while diagnostic
// delivery is blocked. This is publication evidence, never protocol success.
func TestReleaseDiagnosticsBlockedLogPreservesProgressPublication(t *testing.T) {
	sink := &releaseDiagnosticBlockedSink{entered: make(chan struct{}), left: make(chan struct{})}
	ctx, owner, err := newReleaseDiagnostics(t.Context(), sink, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.close() })
	progress, clock := newReleaseProgressTest(t)
	progress.diagnostics = owner
	path := filepath.Join(filepath.Dir(releaseProgressFileTestPath(t)), "missing", "progress.json")
	publisherCtx, cancel := context.WithCancel(ctx)
	waits, resume := make(chan struct{}), make(chan struct{})
	publisher := &releaseProgressPublisher{ctx: publisherCtx, cancel: cancel, done: make(chan struct{}), progress: progress, path: path, stateDir: filepath.Join(filepath.Dir(path), "protocol-state"),
		report: func(code string) { releaseDiagnostic(ctx, "progress", code, 0, false, 0) },
		wait: func(ctx context.Context, _ time.Duration) bool {
			select {
			case waits <- struct{}{}:
			case <-ctx.Done():
				return false
			}
			select {
			case <-resume:
				return true
			case <-ctx.Done():
				return false
			}
		}}
	go publisher.run()
	t.Cleanup(publisher.close)
	<-waits
	<-sink.entered
	if err := os.Mkdir(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	clock.set(clock.now().Add(time.Minute))
	resume <- struct{}{}
	<-waits
	value := releaseProgressFileTestRead(t, path)
	if value.Diagnostics == nil || value.Diagnostics.Progress.Delivered != 0 || value.HeartbeatAt != clock.now().Format(time.RFC3339Nano) || value.Intent != nil || value.Publisher.Outcome != "retrying" {
		t.Fatal("file repair lost separation of prior acknowledgment and protocol progress")
	}
	publisher.close()
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	<-sink.left
}

// Public RunRelease owns diagnostics even without a progress file, and closes
// its constructor-created worker on the real missing-seed early return.
func TestReleaseDiagnosticsPublicEarlyReturnClosesAndWiresCacheNotice(t *testing.T) {
	for _, stage := range []productionReceiptCacheStage{productionReceiptCacheRead, productionReceiptCacheWrite} {
		cfg := validReleaseConfig(t)
		output := &releaseDiagnosticCapture{written: make(chan struct{}, 2)}
		created, closed := false, false
		hooks := releaseDiagnosticHooks{writer: output,
			afterCreate: func(ctx context.Context, owner *releaseDiagnostics) {
				t.Cleanup(func() { _ = owner.close() })
				created = true
				steerer := &ReleaseSteerer{runtimeV2: &releaseRuntimeV2{receiptCache: &productionReceiptCacheState{}}}
				steerer.disableReceiptCache(ctx, 7, stage)
				steerer.disableReceiptCache(ctx, 7, stage)
				releaseDiagnostic(ctx, "operator", "jwt_save_failed", 0, false, 0, releaseDiagnosticFacts{operatorId: 23, operatorKnown: true, cause: releaseDiagnosticHardError})
				<-output.written
				<-output.written
			},
			afterClose: func(owner *releaseDiagnostics, err error) {
				closed = err == nil && owner.exporter.Snapshot("steering").Outcome == "unavailable" && !owner.exporter.Offer("steering", []byte("after close"))
			}}
		ctx := context.WithValue(t.Context(), releaseDiagnosticHooksKey{}, hooks)
		if err := RunRelease(ctx, writeReleaseConfig(t, cfg)); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("real seed failure changed", err)
		}
		if !created || !closed {
			t.Fatal("public lifecycle did not join its diagnostic owner")
		}
		stageName := "read"
		if stage == productionReceiptCacheWrite {
			stageName = "write"
		}
		if strings.Count(output.String(), "code=receipt_cache_disabled") != 1 || !strings.Contains(output.String(), "native_epoch=7 epoch_known=true") || !strings.Contains(output.String(), "cache_stage="+stageName) || !strings.Contains(output.String(), "operator_id=23 operator_known=true") {
			t.Fatal("bounded cache/operator attribution lost", output.String())
		}
	}
}

// Snapshot clocks and diagnostic owners are external to progress state. The
// bounded nested value encode retains its existing lock after those copies.
func TestReleaseDiagnosticsSnapshotCallsClockOutsideProgressLock(t *testing.T) {
	progress, clock := newReleaseProgressTest(t)
	_, owner, err := newReleaseDiagnostics(t.Context(), &bytes.Buffer{}, clock.now)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.close()
	progress.diagnostics = owner
	progress.now = func() time.Time {
		if !progress.stateLock.TryLock() {
			t.Fatal("snapshot called its clock while holding progress state")
		}
		progress.stateLock.Unlock()
		return clock.now()
	}
	value := releaseProgressTestSnapshot(t, progress)
	if value.Diagnostics == nil || value.Diagnostics.ObservedAt != value.HeartbeatAt {
		t.Fatal("snapshot observation clocks are not explicit")
	}
}
