package validator

// Adjacent shutdown controls retain real snapshot, journal and ledger I/O.
// Synthetic worker/resource results test ownership contracts only; they do not
// claim an external Background CloseAndWait can fail during ordinary teardown.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// Every caller receives the same completed close result, and even concurrent
// calls cause exactly one real failed Save and one resource teardown. The first
// Save is explicitly held while all other callers are admitted to the test.
func TestReleaseShutdownFinalCloseOwnsSaveAndResourcesOnce(t *testing.T) {
	cfg, runtime := newReleaseShutdownTestRuntime(t)
	stateDir := cfg.Operators[0].StateDir
	path := obstructReleaseShutdownSnapshot(t, stateDir)
	entered, release := make(chan struct{}), make(chan struct{})
	var writes, closes atomic.Int32
	var saveErr error
	runtime.stats.writeHooks.writeSnapshot = func(directory *statsSnapshotDirectory, write statsSnapshotWrite) error {
		if writes.Add(1) == 1 {
			close(entered)
			<-release
		}
		err := writeStatsSnapshotOwned(directory, write)
		if saveErr == nil {
			saveErr = err
		}
		return err
	}
	resourceErr := errors.New("test-owned resource close contract failure")
	closeOperator := newReleaseOperatorClose(runtime.stats, stateDir, func() error {
		closes.Add(1)
		return resourceErr
	})
	const callers = 8
	results := make(chan error, callers)
	go func() { results <- closeOperator() }()
	<-entered
	admitted := make(chan struct{})
	for index := 1; index < callers; index++ {
		go func() {
			admitted <- struct{}{}
			results <- closeOperator()
		}()
	}
	for index := 1; index < callers; index++ {
		<-admitted
	}
	close(release)
	var completedErr error
	for index := 0; index < callers; index++ {
		err := <-results
		if index == 0 {
			completedErr = err
		}
		if err != completedErr || !errors.Is(err, resourceErr) || !errors.Is(err, saveErr) {
			t.Errorf("close caller %d lost completed snapshot/resource result: %v", index, err)
		}
	}
	var renameErr *os.LinkError
	if writes.Load() != 1 || closes.Load() != 1 || !errors.As(saveErr, &renameErr) || renameErr.New != path {
		t.Fatalf("close did not own one physical snapshot and all resources: writes=%d closes=%d save=%v", writes.Load(), closes.Load(), saveErr)
	}
	if err := closeOperator(); err != completedErr || writes.Load() != 1 || closes.Load() != 1 {
		t.Fatalf("completed close repeated work or changed its result: %v", err)
	}
}

// Pure owner cancellation is expected. A real failed Save and an explicit
// fatal-trail marker survive even when another branch reports cancellation.
func TestReleaseShutdownCancellationClassificationRetainsIndependentCauses(t *testing.T) {
	cfg, runtime := newReleaseShutdownTestRuntime(t)
	obstructReleaseShutdownSnapshot(t, cfg.Operators[0].StateDir)
	saveErr := runtime.stats.Save(cfg.Operators[0].StateDir)
	var renameErr *os.LinkError
	if !errors.As(saveErr, &renameErr) {
		t.Fatalf("physical failure prerequisite: %v", saveErr)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	fatalErr := fatalTrailState("test-owned fatal classification", context.Canceled)
	for _, test := range []struct {
		name        string
		err         error
		wantFailure bool
	}{
		{name: "nil"},
		{name: "canceled", err: context.Canceled},
		{name: "wrapped-canceled", err: fmt.Errorf("owned request: %w", context.Canceled)},
		{name: "deadline", err: context.DeadlineExceeded},
		{name: "joined-cancellation", err: errors.Join(context.Canceled, context.DeadlineExceeded)},
		{name: "actual-save", err: errors.Join(context.Canceled, saveErr), wantFailure: true},
		{name: "wrapped-actual-save", err: fmt.Errorf("worker: %w", errors.Join(context.Canceled, saveErr)), wantFailure: true},
		{name: "fatal-canceled", err: fatalErr, wantFailure: true},
		{name: "joined-fatal-canceled", err: errors.Join(context.Canceled, fatalErr), wantFailure: true},
	} {
		actual := releaseRuntimeError(ctx, test.err)
		if (actual != nil) != test.wantFailure || test.wantFailure && !errors.Is(actual, test.err) {
			t.Errorf("cancellation classification %s lost its independent cause: %v", test.name, actual)
		}
	}
	if err := releaseRuntimeError(context.Background(), context.Canceled); !errors.Is(err, context.Canceled) {
		t.Fatal("an active owner suppressed an independently canceled worker")
	}
}

// The actual release loader selects legacy ledgers, which intentionally retain
// no directory descriptors. Prepared-owner teardown must still join all of
// them, without assuming that the optional disk backend was selected.
func TestReleaseShutdownClosesLegacyPreparedLedgersWithoutRetainedDescriptors(t *testing.T) {
	states := make(map[uint64]*releaseAttemptState)
	for noID := uint64(1); noID <= 2; noID++ {
		fixture := newReleaseClientSeedContinuityFixture(t)
		state := fixture.state
		if state == nil || state.ledger == nil || state.ledger.disk != nil || state.ledger.directory != nil {
			t.Fatal("actual release loader no longer selected a descriptor-free legacy ledger")
		}
		if _, err := state.ledger.Head(); err != nil {
			t.Fatalf("prepared legacy ledger is not live: %v", err)
		}
		states[noID] = state
	}
	if err := closeReleaseAttemptStates(states); err != nil {
		t.Fatal(err)
	}
	if err := closeReleaseAttemptStates(states); err != nil {
		t.Fatalf("repeated legacy prepared close changed its completed result: %v", err)
	}
	for noID, state := range states {
		select {
		case <-state.ledger.closeDone:
		default:
			t.Errorf("prepared legacy ledger %d was not joined", noID)
		}
		if _, err := state.ledger.Head(); !errors.Is(err, errAttemptLedgerClosed) {
			t.Errorf("closed legacy prepared ledger %d still admitted work: %v", noID, err)
		}
	}
}

// A ready poll can return after cancellation while the preceding scheduler or
// submission error is unresolved. No additional iteration may erase that cause.
func TestReleaseShutdownSteeringRetainsFailureAcrossReadyPollCancellation(t *testing.T) {
	for _, boundary := range []string{"scheduler", "submit"} {
		cfg, runtime := newReleaseShutdownTestRuntime(t)
		obstructReleaseShutdownSnapshot(t, cfg.Operators[0].StateDir)
		saveErr := runtime.stats.Save(cfg.Operators[0].StateDir)
		var renameErr *os.LinkError
		if !errors.As(saveErr, &renameErr) {
			t.Fatalf("physical failure prerequisite: %v", saveErr)
		}
		ctx, cancel := context.WithCancel(context.Background())
		reads, submits, waits := 0, 0, 0
		err := runReleaseSteeringLoopWithWait(ctx, func() (uint64, error) {
			reads++
			if boundary == "scheduler" {
				return 0, saveErr
			}
			return 7, nil
		}, func() error { submits++; return saveErr }, func() bool { waits++; cancel(); return true })
		cancel()
		wantSubmits := 1
		if boundary == "scheduler" {
			wantSubmits = 0
		}
		if !errors.Is(err, saveErr) || reads != 1 || submits != wantSubmits || waits != 1 {
			t.Errorf("ready poll lost %s failure or admitted another iteration: error=%v reads=%d submits=%d waits=%d", boundary, err, reads, submits, waits)
		}
	}
}

// Expected pending/finalized sentinels cannot hide a real failed Save joined to
// the same result. Mixed results consume the unchanged real-failure budget.
func TestReleaseShutdownSteeringExpectedResultsDoNotHideRealFailure(t *testing.T) {
	cfg, runtime := newReleaseShutdownTestRuntime(t)
	obstructReleaseShutdownSnapshot(t, cfg.Operators[0].StateDir)
	saveErr := runtime.stats.Save(cfg.Operators[0].StateDir)
	var renameErr *os.LinkError
	if !errors.As(saveErr, &renameErr) {
		t.Fatalf("physical failure prerequisite: %v", saveErr)
	}
	for _, marker := range []error{errAttemptCutPending, ErrSteeringAlreadyFinal} {
		reads, submits, waits := 0, 0, 0
		err := runReleaseSteeringLoopWithWait(context.Background(), func() (uint64, error) { reads++; return 7, nil }, func() error {
			submits++
			return errors.Join(marker, saveErr)
		}, func() bool { waits++; return waits < releaseSteeringFailureLimit })
		if !errors.Is(err, saveErr) || reads != releaseSteeringFailureLimit || submits != releaseSteeringFailureLimit || waits != releaseSteeringFailureLimit-1 {
			t.Errorf("expected result %v hid real failure or changed retry budget: error=%v reads=%d submits=%d waits=%d", marker, err, reads, submits, waits)
		}
	}
}

// A fully successful retry clears the earlier unresolved failure. Repairing
// the test-owned obstruction makes the second actual snapshot write succeed.
func TestReleaseShutdownSteeringSuccessfulRetryClearsPriorFailure(t *testing.T) {
	cfg, runtime := newReleaseShutdownTestRuntime(t)
	stateDir := cfg.Operators[0].StateDir
	path := obstructReleaseShutdownSnapshot(t, stateDir)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var saveErr error
	attempts := 0
	err := runReleaseSteeringLoopWithWait(ctx, func() (uint64, error) { return 7, nil }, func() error {
		attempts++
		if attempts == 1 {
			saveErr = runtime.stats.Save(stateDir)
			return saveErr
		}
		if err := os.Remove(path); err != nil {
			return err
		}
		if err := os.Rename(filepath.Join(stateDir, "preserved-stats.json"), path); err != nil {
			return err
		}
		err := runtime.stats.Save(stateDir)
		cancel()
		return err
	}, func() bool { return ctx.Err() == nil })
	var renameErr *os.LinkError
	if err != nil || attempts != 2 || !errors.As(saveErr, &renameErr) {
		t.Fatalf("successful retry retained an already recovered failure: error=%v attempts=%d original=%v", err, attempts, saveErr)
	}
}

// Both epoch-continuity refusals keep the unresolved real failure from the
// preceding submission while preserving the same immediate stop boundary.
func TestReleaseShutdownSteeringEpochRefusalRetainsUnresolvedFailure(t *testing.T) {
	cfg, runtime := newReleaseShutdownTestRuntime(t)
	obstructReleaseShutdownSnapshot(t, cfg.Operators[0].StateDir)
	saveErr := runtime.stats.Save(cfg.Operators[0].StateDir)
	var renameErr *os.LinkError
	if !errors.As(saveErr, &renameErr) {
		t.Fatalf("physical failure prerequisite: %v", saveErr)
	}
	for _, test := range []struct {
		epoch   uint64
		message string
	}{
		{epoch: 6, message: "epoch regressed"},
		{epoch: 8, message: "incomplete epoch"},
	} {
		reads, submits := 0, 0
		err := runReleaseSteeringLoopWithWait(context.Background(), func() (uint64, error) {
			reads++
			if reads == 1 {
				return 7, nil
			}
			return test.epoch, nil
		}, func() error { submits++; return saveErr }, func() bool { return true })
		if !errors.Is(err, saveErr) || !strings.Contains(err.Error(), test.message) || reads != 2 || submits != 1 {
			t.Errorf("epoch %d refusal lost its unresolved cause or repeated submission: error=%v reads=%d submits=%d", test.epoch, err, reads, submits)
		}
	}
}

// Even a readable rewrite would change the pending retry authority. The actual
// common submission/reconciliation handler leaves the complete file unchanged.
func TestReleaseShutdownPendingFailurePreservesExactJournalBytes(t *testing.T) {
	steerer, intent := newReleaseShutdownPendingIntent(t)
	before, err := os.ReadFile(steerer.intents.path)
	if err != nil {
		t.Fatal(err)
	}
	beforeInfo, err := os.Stat(steerer.intents.path)
	if err != nil {
		t.Fatal(err)
	}
	cause := errors.New("test-owned uncertain finality authentication result")
	if err := steerer.recordReleasePendingError(intent.VectorHash, cause); !errors.Is(err, cause) {
		t.Fatalf("pending result lost its actual cause: %v", err)
	}
	after, err := os.ReadFile(steerer.intents.path)
	if err != nil {
		t.Fatal(err)
	}
	afterInfo, err := os.Stat(steerer.intents.path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) || !os.SameFile(beforeInfo, afterInfo) || beforeInfo.ModTime() != afterInfo.ModTime() {
		t.Fatal("uncertain result replaced or changed the durable pending image")
	}
}

// A constructor can refuse after workers start. Its error and both failures
// produced only after deferred cancellation must all survive the same owner.
func TestReleaseShutdownConstructionFailureRetainsLateWorkerCauses(t *testing.T) {
	cfg, runtime := newReleaseShutdownTestRuntime(t)
	refusal := errors.New("test-owned steerer construction refusal")
	refreshErr := errors.New("test-owned late refresh result")
	trailErr := errors.New("test-owned late trail result")
	runtime.engine = releaseShutdownTrailFunc(func(ctx context.Context, _ int) error { <-ctx.Done(); return trailErr })
	operations := releaseShutdownTestOperations(func() { t.Error("refused construction published running") })
	operations.refresh = func(ctx context.Context) error { <-ctx.Done(); return refreshErr }
	operations.newSteerer = func([]*ReleaseMeasurementContext) (releaseSteererRunner, error) { return nil, refusal }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := runReleaseOperatorWorkers(ctx, cancel, cfg, []*releaseOperatorRuntime{runtime}, operations)
	if !errors.Is(err, refusal) || !errors.Is(err, refreshErr) || !errors.Is(err, trailErr) {
		t.Fatalf("construction refusal lost a joined worker result: %v", err)
	}
}

// Public startup owns prepared ledgers outside the shared worker owner. Its
// final snapshot and resource callback must still see a live ledger, which is
// closed only after the operator operation has completed.
func TestReleaseShutdownPreparedLedgerOutlivesFinalSaveAndResourceClose(t *testing.T) {
	fixture := newReleaseClientSeedContinuityFixture(t)
	state := fixture.state
	saves, closes := 0, 0
	state.stats.writeHooks.writeSnapshot = func(directory *statsSnapshotDirectory, write statsSnapshotWrite) error {
		saves++
		if _, err := state.ledger.Head(); err != nil {
			return fmt.Errorf("final snapshot lost its prepared ledger: %w", err)
		}
		return writeStatsSnapshotOwned(directory, write)
	}
	runtime := &releaseOperatorRuntime{
		stats:  state.stats,
		engine: releaseShutdownTrailFunc(func(ctx context.Context, _ int) error { <-ctx.Done(); return ctx.Err() }),
		close: newReleaseOperatorClose(state.stats, fixture.op.StateDir, func() error {
			closes++
			_, err := state.ledger.Head()
			return err
		}),
	}
	cfg := &ReleaseConfig{Operators: []OperatorConfig{fixture.op}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := runReleaseOperatorWorkers(ctx, cancel, cfg, []*releaseOperatorRuntime{runtime}, releaseShutdownTestOperations(cancel))
	if err != nil || saves != 1 || closes != 1 {
		t.Fatalf("operator cleanup lost prepared ownership: error=%v saves=%d closes=%d", err, saves, closes)
	}
	if err := closeReleaseAttemptStates(map[uint64]*releaseAttemptState{fixture.op.NoID: state}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-state.ledger.closeDone:
	default:
		t.Fatal("prepared ledger remained live after final operator teardown")
	}
}
