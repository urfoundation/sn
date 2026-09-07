package validator

// Shutdown failures are observed at the shared production worker owner, not at
// a replacement verdict. Outbound workers are local; Stats, ledger and proof
// operations use real test-owned files and complete signed trail execution.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/urnetwork/connect/v2026"
)

// Only the external work is replaceable; its owner always runs production
// cancellation, join, final snapshot and result selection.
type releaseShutdownTrailFunc func(context.Context, int) error

func (self releaseShutdownTrailFunc) Run(ctx context.Context, concurrency int) error {
	return self(ctx, concurrency)
}

type releaseShutdownSteererFunc func(context.Context) error

func (self releaseShutdownSteererFunc) Run(ctx context.Context) error { return self(ctx) }

// The zero-work control waits for normal cancellation without inventing an
// external service failure. A non-nil override still runs under the real owner.
func releaseShutdownTestOperations(running func()) releaseRuntimeOperations {
	return releaseRuntimeOperations{
		refresh: func(ctx context.Context) error { <-ctx.Done(); return nil },
		newSteerer: func([]*ReleaseMeasurementContext) (releaseSteererRunner, error) {
			return releaseShutdownSteererFunc(func(ctx context.Context) error { <-ctx.Done(); return nil }), nil
		},
		running: running,
	}
}

// A private real snapshot directory is prepared before the worker operation.
func newReleaseShutdownTestRuntime(t *testing.T) (*ReleaseConfig, *releaseOperatorRuntime) {
	t.Helper()
	stateDir := t.TempDir()
	stats := NewStatsEngine(StatsConfig{AMin: 8})
	if err := stats.Save(stateDir); err != nil {
		t.Fatal(err)
	}
	cfg := &ReleaseConfig{Operators: []OperatorConfig{{NoID: 9, Concurrency: 1, StateDir: stateDir}}}
	runtime := &releaseOperatorRuntime{
		stats:  stats,
		engine: releaseShutdownTrailFunc(func(ctx context.Context, _ int) error { <-ctx.Done(); return ctx.Err() }),
		close:  newReleaseOperatorClose(stats, stateDir, func() error { return nil }),
	}
	return cfg, runtime
}

// A failed replacing rename is an actual durable Save failure, with its
// occupied destination preserved. Cancellation does not authorize success.
func TestReleaseShutdownReturnsPhysicalFinalSnapshotFailure(t *testing.T) {
	cfg, runtime := newReleaseShutdownTestRuntime(t)
	stateDir := cfg.Operators[0].StateDir
	path := filepath.Join(stateDir, "stats.json")
	if err := os.Rename(path, filepath.Join(stateDir, "preserved-stats.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	var actualWriteErr error
	writes, resourceCloses := 0, 0
	runtime.stats.writeHooks.writeSnapshot = func(path string, payload []byte) error {
		writes++
		actualWriteErr = atomicStateWrite(path, payload, 0o600)
		return actualWriteErr
	}
	runtime.close = newReleaseOperatorClose(runtime.stats, stateDir, func() error { resourceCloses++; return nil })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := runReleaseOperatorWorkers(ctx, cancel, cfg, []*releaseOperatorRuntime{runtime}, releaseShutdownTestOperations(cancel))
	var renameErr *os.LinkError
	info, statErr := os.Lstat(path)
	if writes != 1 || resourceCloses != 1 || !errors.As(actualWriteErr, &renameErr) || renameErr.New != path || statErr != nil || !info.IsDir() {
		t.Fatalf("physical final-save prerequisite differs: writes=%d closes=%d write=%v stat=%v", writes, resourceCloses, actualWriteErr, statErr)
	}
	if !errors.Is(err, actualWriteErr) {
		t.Fatalf("release shutdown reported success after actual final stats write failure: return=%v write=%v", err, actualWriteErr)
	}
}

// The server completes a genuine eight-hop signed trail. Only its actual
// projection destination is obstructed; cancellation occurs after Run returns
// that fatal state error, before the release owner's reporting boundary.
func TestReleaseShutdownRetainsRealM8ProofFailureAtCancellation(t *testing.T) {
	stateDir := t.TempDir()
	server, validatorKey, clientID := newMockVerifyServer(t, 16)
	store, err := NewProofStore(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	engine, stats, _ := newTestEngine(t, server, validatorKey, clientID, 8, store)
	generation := uint64(1)
	ledger := configureAttemptLedgerTestEngine(t, engine, stats, stateDir, &generation)
	t.Cleanup(func() {
		if err := ledger.Close(); err != nil {
			t.Errorf("close actual trail ledger: %v", err)
		}
	})
	if err := os.Mkdir(store.path, 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var actualTrailErr error
	runtime := &releaseOperatorRuntime{
		stats: stats,
		engine: releaseShutdownTrailFunc(func(ctx context.Context, concurrency int) error {
			actualTrailErr = engine.Run(ctx, concurrency)
			cancel()
			return actualTrailErr
		}),
		close: newReleaseOperatorClose(stats, stateDir, func() error { return nil }),
	}
	cfg := &ReleaseConfig{Operators: []OperatorConfig{{NoID: 9, Concurrency: 1, StateDir: stateDir}}}
	err = runReleaseOperatorWorkers(ctx, cancel, cfg, []*releaseOperatorRuntime{runtime}, releaseShutdownTestOperations(func() {}))
	var fatalErr *TrailFatalError
	var pathErr *os.PathError
	if !errors.As(actualTrailErr, &fatalErr) || !errors.As(actualTrailErr, &pathErr) || ledger.LastSequence() != 8 || len(stats.ProviderIDs()) != 7 {
		t.Fatalf("real M8 projection prerequisite differs: error=%v sequence=%d providers=%d", actualTrailErr, ledger.LastSequence(), len(stats.ProviderIDs()))
	}
	if !errors.Is(err, actualTrailErr) {
		t.Fatalf("release shutdown discarded real signed M8 proof durability failure at cancellation: return=%v trail=%v", err, actualTrailErr)
	}
}

// The first steerer error is the only ready termination event. The refresh
// and trail errors arrive strictly after deferred cancellation, forcing their
// joined results to be retained instead of relying on select scheduling.
func TestReleaseShutdownJoinsLateWorkerFailuresAfterFirstError(t *testing.T) {
	cfg, runtime := newReleaseShutdownTestRuntime(t)
	firstErr := errors.New("test-owned first steerer failure")
	refreshErr := errors.New("test-owned refresh failure after owner cancellation")
	trailErr := errors.New("test-owned trail failure after owner cancellation")
	var joined atomic.Int32
	runtime.engine = releaseShutdownTrailFunc(func(ctx context.Context, _ int) error {
		<-ctx.Done()
		joined.Add(1)
		return trailErr
	})
	operations := releaseShutdownTestOperations(func() {})
	operations.refresh = func(ctx context.Context) error { <-ctx.Done(); joined.Add(1); return refreshErr }
	operations.newSteerer = func([]*ReleaseMeasurementContext) (releaseSteererRunner, error) {
		return releaseShutdownSteererFunc(func(context.Context) error { return firstErr }), nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := runReleaseOperatorWorkers(ctx, cancel, cfg, []*releaseOperatorRuntime{runtime}, operations)
	if joined.Load() != 2 || !errors.Is(err, firstErr) {
		t.Fatalf("first-error/join prerequisite differs: joined=%d error=%v", joined.Load(), err)
	}
	if !errors.Is(err, refreshErr) || !errors.Is(err, trailErr) {
		t.Fatalf("release shutdown discarded failures from joined workers after first error: %v", err)
	}
}

// An actual proof read fails after the ledger is acquired and Stats attached.
// The retained descriptor owner must close even though no state is published.
func TestReleaseShutdownClosesLedgerAfterPreparationFailure(t *testing.T) {
	fixture := newReleaseClientSeedContinuityFixture(t)
	if err := os.Mkdir(fixture.state.store.path, 0o700); err != nil {
		t.Fatal(err)
	}
	var opened *AttemptLedger
	state, err := loadReleaseAttemptStateWithObserver(fixture.cfg, fixture.op, 7, func(ledger *AttemptLedger) { opened = ledger })
	if opened != nil {
		t.Cleanup(func() {
			if err := opened.Close(); err != nil {
				t.Errorf("close refused preparation owner: %v", err)
			}
		})
	}
	var pathErr *os.PathError
	if state != nil || opened == nil || !errors.As(err, &pathErr) || !strings.Contains(err.Error(), "proof projection reconciliation") {
		t.Fatalf("actual post-acquisition refusal prerequisite differs: state=%p owner=%p error=%v", state, opened, err)
	}
	select {
	case <-opened.closeDone:
	default:
		t.Fatal("release preparation leaked its actual acquired ledger after proof read refusal")
	}
}

// Successful preparation transfers a live ledger; refusal cleanup must not
// accidentally close the object handed to the caller.
func TestReleaseShutdownSuccessfulPreparationTransfersLiveLedger(t *testing.T) {
	fixture := newReleaseClientSeedContinuityFixture(t)
	var opened *AttemptLedger
	state, err := loadReleaseAttemptStateWithObserver(fixture.cfg, fixture.op, 7, func(ledger *AttemptLedger) { opened = ledger })
	if opened != nil {
		t.Cleanup(func() {
			if err := opened.Close(); err != nil {
				t.Errorf("close transferred preparation owner: %v", err)
			}
		})
	}
	if err != nil || state == nil || state.ledger != opened {
		t.Fatalf("successful preparation did not transfer exactly its owner: %v", err)
	}
	select {
	case <-opened.closeDone:
		t.Fatal("successful preparation prematurely closed its transferred ledger")
	default:
	}
	if _, err := opened.Head(); err != nil {
		t.Fatalf("transferred real ledger is unusable: %v", err)
	}
}

// All producers join before the real final snapshot. A canceled last worker
// can still finish a valid Stats update, which must survive an actual reload.
func TestReleaseShutdownJoinsWorkersBeforeFinalDurableSnapshot(t *testing.T) {
	cfg, runtime := newReleaseShutdownTestRuntime(t)
	providerID := connect.Id{1}
	var joined atomic.Int32
	runtime.engine = releaseShutdownTrailFunc(func(ctx context.Context, _ int) error {
		<-ctx.Done()
		runtime.stats.RecordAssignment(providerID)
		joined.Add(1)
		return ctx.Err()
	})
	operations := releaseShutdownTestOperations(nil)
	operations.refresh = func(ctx context.Context) error { <-ctx.Done(); joined.Add(1); return nil }
	operations.newSteerer = func([]*ReleaseMeasurementContext) (releaseSteererRunner, error) {
		return releaseShutdownSteererFunc(func(ctx context.Context) error { <-ctx.Done(); joined.Add(1); return nil }), nil
	}
	writes, closes := 0, 0
	runtime.stats.writeHooks.writeSnapshot = func(path string, payload []byte) error {
		writes++
		if joined.Load() != 3 {
			return errors.New("final snapshot preceded all worker joins")
		}
		return atomicStateWrite(path, payload, 0o600)
	}
	runtime.close = newReleaseOperatorClose(runtime.stats, cfg.Operators[0].StateDir, func() error { closes++; return nil })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	operations.running = cancel
	err := runReleaseOperatorWorkers(ctx, cancel, cfg, []*releaseOperatorRuntime{runtime}, operations)
	if err != nil || writes != 1 || closes != 1 || joined.Load() != 3 {
		t.Fatalf("normal cancellation changed lifecycle: error=%v writes=%d closes=%d joined=%d", err, writes, closes, joined.Load())
	}
	reloaded := NewStatsEngine(StatsConfig{AMin: 8})
	if err := reloaded.Load(cfg.Operators[0].StateDir); err != nil {
		t.Fatal(err)
	}
	if reloaded.Exposure()[providerID] != 1 {
		t.Fatal("final worker's real assignment did not survive durable reload")
	}
}

// A steerer construction refusal still cancels and joins already launched
// workers, then saves and closes the operator exactly once.
func TestReleaseShutdownConstructionRefusalJoinsAndCloses(t *testing.T) {
	cfg, runtime := newReleaseShutdownTestRuntime(t)
	refusal := errors.New("test-owned steerer construction refusal")
	var joined atomic.Int32
	runtime.engine = releaseShutdownTrailFunc(func(ctx context.Context, _ int) error { <-ctx.Done(); joined.Add(1); return ctx.Err() })
	operations := releaseShutdownTestOperations(func() { t.Fatal("refused construction reached running publication") })
	operations.refresh = func(ctx context.Context) error { <-ctx.Done(); joined.Add(1); return nil }
	operations.newSteerer = func([]*ReleaseMeasurementContext) (releaseSteererRunner, error) { return nil, refusal }
	closes := 0
	runtime.close = newReleaseOperatorClose(runtime.stats, cfg.Operators[0].StateDir, func() error { closes++; return nil })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := runReleaseOperatorWorkers(ctx, cancel, cfg, []*releaseOperatorRuntime{runtime}, operations)
	if !errors.Is(err, refusal) || closes != 1 || joined.Load() != 2 {
		t.Fatalf("construction refusal leaked worker ownership: error=%v closes=%d joined=%d", err, closes, joined.Load())
	}
}
