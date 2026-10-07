//go:build linux || darwin

// The real trail and release owners keep durable measurement independent of
// optional diagnostics. Explicit picker/write barriers determine every order.
package validator

import (
	"context"
	"crypto/ed25519"
	"errors"
	"math"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urnetwork/connect/v2026"
)

// A genuine signed server, physical ledger and proof projection back each run.
type trailDiagnosticFixture struct {
	engine *TrailEngine
	server *mockVerifyServer
	store  *ProofStore
	ledger *AttemptLedger
	state  string
}

// No measurement admission or proof writer is replaced by this fixture.
func newTrailDiagnosticFixture(t *testing.T, expectedCloseCause ...error) trailDiagnosticFixture {
	t.Helper()
	if len(expectedCloseCause) > 1 {
		t.Fatal("trail fixture accepts at most one exact close cause")
	}
	state := t.TempDir()
	server, key, clientId := newMockVerifyServer(t, 16)
	store, err := NewProofStore(state)
	if err != nil {
		t.Fatal(err)
	}
	engine, stats, _ := newTestEngine(t, server, key, clientId, 8, store)
	generation := uint64(1)
	ledger := configureAttemptLedgerTestEngine(t, engine, stats, state, &generation)
	t.Cleanup(func() {
		err := ledger.Close()
		if len(expectedCloseCause) == 1 {
			if !errors.Is(err, expectedCloseCause[0]) || !releaseOnlyErrors(err, expectedCloseCause[0]) {
				t.Error("trail fixture close lost its exact original fault or added another failure")
			}
		} else if err != nil {
			t.Error("trail fixture ledger close failed")
		}
	})
	return trailDiagnosticFixture{engine: engine, server: server, store: store, ledger: ledger, state: state}
}

// A barrier occurs only when the actual Run has finished its previous trail.
func (self trailDiagnosticFixture) stopAtPicker(call int) <-chan struct{} {
	reached := make(chan struct{})
	pick := self.engine.pickSeed
	calls := 0
	self.engine.pickSeed = func(ctx context.Context) (connect.Id, error) {
		calls++
		if calls == call {
			close(reached)
			<-ctx.Done()
			return connect.Id{}, ctx.Err()
		}
		return pick(ctx)
	}
	return reached
}

// Waiting on an actual operation barrier has only a generous deadlock backstop.
func awaitTrailDiagnostic(t *testing.T, ctx context.Context, reached <-chan struct{}) {
	t.Helper()
	select {
	case <-reached:
	case <-ctx.Done():
		t.Fatal("trail diagnostic operation did not reach its owned barrier")
	}
}

// Failure paths cancel and join the same work as the normal assertion path.
// Waiting twice only observes the immutable completion; no result is consumed.
func startTrailDiagnosticTestWork(t *testing.T, cancel context.CancelFunc, run func() error) func() error {
	t.Helper()
	finished := make(chan struct{})
	var result error
	go func() { result = run(); close(finished) }()
	t.Cleanup(func() { cancel(); <-finished })
	return func() error { <-finished; return result }
}

// Only the first real transport response fails; all later protocol bytes pass
// through the actual signed server and original retry/persistence machinery.
type trailDiagnosticFirstReadFailure struct {
	inner TrailTransport
	once  bool
}

// The failure is typed and carries no fabricated server acceptance.
func (self *trailDiagnosticFirstReadFailure) PostVerify(ctx context.Context, hop connect.Id, raw []byte) ([]byte, error) {
	if !self.once {
		self.once = true
		return nil, context.DeadlineExceeded
	}
	return self.inner.PostVerify(ctx, hop, raw)
}

// A failed read does not stop the next genuine trail; only completed durable
// proof work is reported as complete, with the original settlement epoch.
func TestTrailDiagnosticsRunContinuesAfterReadFailureWithOriginalProof(t *testing.T) {
	fixture := newTrailDiagnosticFixture(t)
	fixture.engine.transport = &trailDiagnosticFirstReadFailure{inner: fixture.server}
	fixture.engine.cfg.ExtendAttempts = 1
	reached := fixture.stopAtPicker(3)
	output := &releaseDiagnosticCapture{written: make(chan struct{}, 2)}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	ctx, owner, err := newReleaseDiagnostics(ctx, output, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.close() })
	ctx = withTrailDiagnosticOperator(ctx, 17)
	wait := startTrailDiagnosticTestWork(t, cancel, func() error { return fixture.engine.Run(ctx, 1) })
	awaitTrailDiagnostic(t, ctx, reached)
	awaitTrailDiagnostic(t, ctx, output.written)
	awaitTrailDiagnostic(t, ctx, output.written)
	cancel()
	if err := wait(); err != nil {
		t.Fatal("ordinary read failure stopped trail work")
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	records, skipped, err := fixture.store.Load()
	if err != nil || skipped != 0 || len(records) != 1 || fixture.ledger.LastSequence() != 8 || fixture.engine.Completed() != 1 {
		t.Fatal("diagnostics changed actual completed proof or ledger work")
	}
	if err := VerifyProofRecord(records[0], fixture.engine.vsk.Public().(ed25519.PublicKey), fixture.server.serverPublicKeys(), 8); err != nil {
		t.Fatal("retained original signed proof did not verify")
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	// postStep retains the timeout in an opaque fmt wrapper. Optional output
	// preserves the seed kind while leaving that nested cause unknown.
	if len(lines) != 2 || !strings.Contains(lines[0], "code=trail_failed operator_id=17 operator_known=true trail_kind=seed cause=unknown") ||
		!strings.Contains(lines[1], "code=trail_complete operator_id=17") || !strings.Contains(lines[1], "depth=8 settlement_epoch=42 settlement_epoch_known=true") {
		t.Fatal("trail diagnostic lost closed cause or original settlement identity")
	}
	if strings.Contains(output.String(), "native_epoch=") || strings.Contains(output.String(), records[0].TrailId.String()) || owner.snapshot(time.Now()).Runtime.Delivered != 2 {
		t.Fatal("local proof output invented native progress or exposed trail identity")
	}
}

// A configured source can supply a known epoch zero. An absent source uses
// the same numeric value but must remain explicitly unknown in actual Run output.
func TestTrailDiagnosticsRunDistinguishesKnownZeroFromAbsentEpoch(t *testing.T) {
	for _, known := range []bool{true, false} {
		server, key, clientId := newMockVerifyServer(t, 16)
		store, err := NewProofStore(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		engine, _, _ := newTestEngine(t, server, key, clientId, 8, store)
		engine.epochFn = nil
		if known {
			engine.epochFn = func() uint64 { return 0 }
		}
		fixture := trailDiagnosticFixture{engine: engine}
		reached := fixture.stopAtPicker(2)
		output := &releaseDiagnosticCapture{written: make(chan struct{}, 1)}
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()
		ctx, owner, err := newReleaseDiagnostics(ctx, output, time.Now)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = owner.close() })
		wait := startTrailDiagnosticTestWork(t, cancel, func() error { return engine.Run(ctx, 1) })
		awaitTrailDiagnostic(t, ctx, reached)
		awaitTrailDiagnostic(t, ctx, output.written)
		cancel()
		if err := wait(); err != nil {
			t.Fatal("epoch source changed actual trail completion")
		}
		if err := owner.close(); err != nil {
			t.Fatal(err)
		}
		expected := "settlement_epoch=0 settlement_epoch_known=false"
		if known {
			expected = "settlement_epoch=0 settlement_epoch_known=true"
		}
		if !strings.Contains(output.String(), expected) {
			t.Fatal("trail output confused known zero with absent epoch")
		}
	}
}

// Optional blocked diagnostics cannot make either ledger or proof projection
// failure retryable. Every successful signature remains in its actual ledger.
func TestTrailDiagnosticsHardCustodyFailureStillStopsRun(t *testing.T) {
	for _, projection := range []bool{false, true} {
		cause := errors.New("synthetic durable trail append failure")
		var expectedCloseCause []error
		if !projection {
			expectedCloseCause = []error{cause}
		}
		fixture := newTrailDiagnosticFixture(t, expectedCloseCause...)
		appends := 0
		if projection {
			if err := os.Mkdir(fixture.store.path, 0700); err != nil {
				t.Fatal(err)
			}
		} else {
			fixture.ledger.appendFn = func(string, []byte) error { appends++; return cause }
		}
		sink := &releaseDiagnosticBlockedSink{entered: make(chan struct{}), left: make(chan struct{})}
		ctx, owner, err := newReleaseDiagnostics(t.Context(), sink, time.Now)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = owner.close() })
		// Occupy the real export before the actual durable failure occurs.
		observeTrailDiagnostic(ctx, trailDiagnosticFailed, context.DeadlineExceeded, nil, false)
		<-sink.entered
		err = fixture.engine.Run(ctx, 1)
		var fatal *TrailFatalError
		if !errors.As(err, &fatal) || fixture.engine.Completed() != 0 || fixture.engine.Failed() != 1 {
			t.Fatal("optional trail output softened hard custody failure")
		}
		if projection {
			if fixture.ledger.LastSequence() != 8 || len(fixture.engine.stats.ProviderIDs()) != 7 {
				t.Fatal("projection failure changed signed original completion")
			}
		} else if !errors.Is(err, cause) || appends != 1 || fixture.ledger.LastSequence() != 0 {
			t.Fatal("ledger failure retried or lost its original cause")
		}
		if err := owner.close(); err != nil {
			t.Fatal(err)
		}
		<-sink.left
		state := owner.snapshot(time.Now())
		if state.Runtime.Delivered != 0 || state.Runtime.Dropped == 0 || state.Steering.Delivered != 0 {
			t.Fatal("blocked trail diagnostics claimed protocol delivery")
		}
		closeErr := fixture.ledger.Close()
		if projection {
			if closeErr != nil {
				t.Fatal("proof projection fault unexpectedly changed ledger close")
			}
		} else if !errors.Is(closeErr, cause) || !releaseOnlyErrors(closeErr, cause) {
			t.Fatal("ledger close lost the original append fault or added another failure")
		}
		if fixture.ledger.Close() != closeErr {
			t.Fatal("repeated ledger close replaced its original result")
		}
	}
}

// Error() would create arbitrary peer text; classification must never ask for it.
type trailDiagnosticFormattingProbe struct{ calls atomic.Uint64 }

// Count instead of panic so a causal control cannot abort its sibling roots.
func (self *trailDiagnosticFormattingProbe) Error() string {
	self.calls.Add(1)
	return "synthetic private raw transport detail"
}

// Arbitrary unwrapping is not needed for the known TrailError kind. The
// diagnostic cause must remain unknown instead of invoking this method.
func (*trailDiagnosticFormattingProbe) Unwrap() error { return context.DeadlineExceeded }

// Maximum scalar values still fit one bounded record and the existing runtime
// domain. No error method, peer id, dynamic domain or native epoch is needed.
func TestTrailDiagnosticsClosedFactsNeverFormatRawCause(t *testing.T) {
	output := &releaseDiagnosticCapture{written: make(chan struct{}, 2)}
	ctx, owner, err := newReleaseDiagnostics(t.Context(), output, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.close() })
	ctx = withTrailDiagnosticOperator(ctx, math.MaxUint64)
	cause := &trailDiagnosticFormattingProbe{}
	observeTrailDiagnostic(ctx, trailDiagnosticFailed, &TrailError{Kind: TrailErrorHop, Err: cause}, nil, false)
	<-output.written
	observeTrailDiagnostic(ctx, trailDiagnosticComplete, nil, &ProofRecord{M: connect.VerifyMMax, Epoch: math.MaxUint64}, true)
	<-output.written
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	if cause.calls.Load() != 0 {
		t.Fatal("trail diagnostics formatted arbitrary cause")
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 2 || len(lines[0]) > 512 || len(lines[1]) > 512 || !strings.Contains(lines[0], "trail_kind=hop cause=unknown") {
		t.Fatal("closed trail output exceeded its fixed scalar contract")
	}
	state := owner.snapshot(time.Now())
	if state.Runtime.Delivered != 2 || state.Operator.Delivered != 0 || state.Steering.Delivered != 0 || state.Startup.Delivered != 0 || state.Progress.Delivered != 0 {
		t.Fatal("trail output changed the existing five-domain routing")
	}
}

// A precreated child retains its lifecycle and existing unrelated context data.
func TestTrailDiagnosticsContextCopyRetainsChildLifetime(t *testing.T) {
	output := &releaseDiagnosticCapture{written: make(chan struct{}, 1)}
	source, sourceCancel := context.WithCancel(t.Context())
	defer sourceCancel()
	source, owner, err := newReleaseDiagnostics(source, output, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.close() })
	type authorityKey struct{}
	source = context.WithValue(withTrailDiagnosticOperator(source, 23), authorityKey{}, "source")
	child, childCancel := context.WithCancel(context.WithValue(t.Context(), authorityKey{}, "child"))
	defer childCancel()
	child = copyTrailDiagnosticContext(child, source)
	sourceCancel()
	if child.Err() != nil || child.Value(authorityKey{}) != "child" || releaseDiagnosticOwner(child) != owner {
		t.Fatal("diagnostic context copy changed child authority or cancellation")
	}
	observeTrailDiagnostic(child, trailDiagnosticFailed, nil, nil, false)
	<-output.written
	childCancel()
	if !errors.Is(child.Err(), context.Canceled) {
		t.Fatal("diagnostic context copy detached the actual child lifetime")
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "operator_id=23 operator_known=true") {
		t.Fatal("precreated child lost admitted operator attribution")
	}
}

// The real release owner binds two operators without a per-worker domain or
// label. Both genuine trails complete, then all owners join before final saves.
func TestTrailDiagnosticsReleaseWorkersBindEachOperator(t *testing.T) {
	first, second := newTrailDiagnosticFixture(t), newTrailDiagnosticFixture(t)
	firstReached, secondReached := first.stopAtPicker(2), second.stopAtPicker(2)
	output := &releaseDiagnosticCapture{written: make(chan struct{}, 2)}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	ctx, owner, err := newReleaseDiagnostics(ctx, output, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.close() })
	cfg := &ReleaseConfig{Operators: []OperatorConfig{{NoID: 11, Concurrency: 1, StateDir: first.state}, {NoID: 12, Concurrency: 1, StateDir: second.state}}}
	var runtimes []*releaseOperatorRuntime
	for _, fixture := range []trailDiagnosticFixture{first, second} {
		runtimes = append(runtimes, &releaseOperatorRuntime{stats: fixture.engine.stats, engine: fixture.engine,
			close: newReleaseOperatorClose(fixture.engine.stats, fixture.state, fixture.ledger.Close)})
	}
	wait := startTrailDiagnosticTestWork(t, cancel, func() error {
		return runReleaseOperatorWorkers(ctx, cancel, cfg, runtimes, releaseShutdownTestOperations(func() {}))
	})
	awaitTrailDiagnostic(t, ctx, firstReached)
	awaitTrailDiagnostic(t, ctx, secondReached)
	awaitTrailDiagnostic(t, ctx, output.written)
	awaitTrailDiagnostic(t, ctx, output.written)
	cancel()
	if err := wait(); err != nil {
		t.Fatal("diagnostic binding changed release shutdown")
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	for _, operator := range []string{"operator_id=11 operator_known=true", "operator_id=12 operator_known=true"} {
		if strings.Count(output.String(), operator) != 1 {
			t.Fatal("release worker lost its own operator diagnostic binding")
		}
	}
	if first.engine.Completed() != 1 || second.engine.Completed() != 1 || owner.snapshot(time.Now()).Runtime.Delivered != 2 {
		t.Fatal("optional diagnostic binding lost independent trail completion")
	}
}
