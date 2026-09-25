package validator

// Inner owners must return the actual durable failure before outer shutdown can
// preserve it. The real polling loops run with deterministic wait decisions;
// private snapshots, signed transitions and complete intents are not mocked.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// Obstructs a real replacing snapshot after retaining its previous bytes.
func obstructReleaseShutdownSnapshot(t *testing.T, stateDir string) string {
	t.Helper()
	path := filepath.Join(stateDir, "stats.json")
	if err := os.Rename(path, filepath.Join(stateDir, "preserved-stats.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

// The actual signed transition and write-ahead transaction reach the failed
// participant snapshot before cancellation is made visible to the refresher.
func TestReleaseShutdownRefreshRetainsPhysicalAdvanceFailure(t *testing.T) {
	participant, ledger := newAttemptSettlementTestParticipant(t, 1)
	t.Cleanup(func() {
		if err := ledger.Close(); err != nil {
			t.Errorf("close refresh ledger: %v", err)
		}
	})
	path := obstructReleaseShutdownSnapshot(t, participant.StateDir)
	coordinatorDir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var advanceErr error
	loads, advances, publishes := 0, 0, 0
	err := runReleaseSettlementRefresh(ctx, time.Hour,
		func(context.Context) (*ReleaseSnapshot, error) {
			loads++
			return &ReleaseSnapshot{Epoch: big.NewInt(43)}, nil
		},
		func(ctx context.Context, snapshot *ReleaseSnapshot) error {
			advances++
			advanceErr = advanceReleaseSettlementSnapshot(ctx, coordinatorDir, snapshot, []AttemptSettlementParticipant{participant},
				func(context.Context, *ReleaseSnapshot) (AttemptBoundary, error) {
					return attemptLedgerTestBoundary(), nil
				})
			cancel()
			return advanceErr
		}, func(*ReleaseSnapshot) { publishes++ }, func(context.Context, time.Duration) error { return nil })
	var renameErr *os.LinkError
	if loads != 1 || advances != 1 || publishes != 0 || !errors.As(advanceErr, &renameErr) || renameErr.New != path {
		t.Fatalf("actual refresh snapshot prerequisite differs: load=%d advance=%d publish=%d error=%v", loads, advances, publishes, advanceErr)
	}
	if !errors.Is(err, advanceErr) {
		t.Fatalf("release refresh replaced actual signed-transition snapshot failure with cancellation: return=%v advance=%v", err, advanceErr)
	}
}

// A scheduler failure can contain cancellation and a distinct owned-resource
// failure. errors.Is(canceled) must not erase the second cause.
func TestReleaseShutdownSteeringRetainsJoinedSchedulerFailure(t *testing.T) {
	cfg, runtime := newReleaseShutdownTestRuntime(t)
	path := obstructReleaseShutdownSnapshot(t, cfg.Operators[0].StateDir)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var saveErr error
	reads, submits, waits := 0, 0, 0
	err := runReleaseSteeringLoopWithWait(ctx, func() (uint64, error) {
		reads++
		saveErr = runtime.stats.Save(cfg.Operators[0].StateDir)
		cancel()
		return 0, errors.Join(context.Canceled, saveErr)
	}, func() error { submits++; return nil }, func() bool { waits++; return false })
	var renameErr *os.LinkError
	if reads != 1 || submits != 0 || waits != 0 || !errors.As(saveErr, &renameErr) || renameErr.New != path {
		t.Fatalf("scheduler barrier prerequisite differs: reads=%d submits=%d waits=%d save=%v", reads, submits, waits, saveErr)
	}
	if !errors.Is(err, saveErr) {
		t.Fatalf("release steering discarded durable scheduler cause joined with cancellation: return=%v save=%v", err, saveErr)
	}
}

// The production SubmitOnce callback returns into this exact loop. A failed
// save in that call must survive the canceled false-poll exit, without another
// epoch read or a repeated submit.
func TestReleaseShutdownSteeringRetainsJoinedSubmissionFailure(t *testing.T) {
	cfg, runtime := newReleaseShutdownTestRuntime(t)
	path := obstructReleaseShutdownSnapshot(t, cfg.Operators[0].StateDir)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var saveErr error
	reads, submits, waits := 0, 0, 0
	err := runReleaseSteeringLoopWithWait(ctx, func() (uint64, error) { reads++; return 7, nil },
		func() error {
			submits++
			saveErr = runtime.stats.Save(cfg.Operators[0].StateDir)
			cancel()
			return errors.Join(context.Canceled, saveErr)
		}, func() bool { waits++; return false })
	var renameErr *os.LinkError
	if reads != 1 || submits != 1 || !errors.As(saveErr, &renameErr) || renameErr.New != path {
		t.Fatalf("submission barrier prerequisite differs: reads=%d submits=%d waits=%d save=%v", reads, submits, waits, saveErr)
	}
	if !errors.Is(err, saveErr) {
		t.Fatalf("release steering discarded durable submission cause joined with cancellation: return=%v save=%v", err, saveErr)
	}
}

// Complete signed measurement/envelope bytes and their prepared submission are
// validated by the real existing fixture and the actual IntentStore.Begin.
func newReleaseShutdownPendingIntent(t *testing.T) (*ReleaseSteerer, *SteeringIntent) {
	t.Helper()
	stateDir := filepath.Join(t.TempDir(), "state")
	store, err := NewIntentStore(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	intent, err := store.Begin(testSteeringIntent(t, stateDir, 3, ""))
	if err != nil {
		t.Fatal(err)
	}
	return &ReleaseSteerer{intents: store}, intent
}

// Mutating the real pending image must validate its complete resulting state
// before replacing the old bytes, including status-specific terminal fields.
func TestReleaseShutdownIntentUpdateRejectsInvalidLifecycleBeforeWrite(t *testing.T) {
	steerer, intent := newReleaseShutdownPendingIntent(t)
	path := steerer.intents.path
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, status string
		mutate       func(*SteeringIntent) error
	}{
		{name: "pending-error", status: "pending", mutate: func(i *SteeringIntent) error { i.Error = "unreadable pending diagnostic"; return nil }},
		{name: "pending-receipt", status: "pending", mutate: func(i *SteeringIntent) error { i.FinalizedBlock = 1; return nil }},
		{name: "finalized-without-receipt", status: "finalized"},
		{name: "applied-without-receipt", status: "applied"},
		{name: "failed-without-cause", status: "failed"},
	} {
		// Restore only this test-owned original between complete independent
		// variations; even the old corrupting writer must reach every case.
		if err := os.WriteFile(path, before, 0o600); err != nil {
			t.Fatal(err)
		}
		updateErr := steerer.intents.update(intent.VectorHash, test.status, test.mutate)
		after, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if updateErr == nil || !bytes.Equal(before, after) {
			t.Errorf("release intent update persisted an unreadable lifecycle before refusal: variant=%s error=%v changed=%t", test.name, updateErr, !bytes.Equal(before, after))
		}
	}
}

// Begin fills canonical metadata itself, but must still refuse caller-carried
// pending error/terminal fields before any first journal image is published.
func TestReleaseShutdownIntentBeginRejectsInvalidLifecycleBeforeWrite(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*SteeringIntent)
	}{
		{name: "pending-error", mutate: func(i *SteeringIntent) { i.Error = "unreadable first pending error" }},
		{name: "pending-receipt", mutate: func(i *SteeringIntent) { i.FinalizedBlock = 1 }},
		{name: "pending-application", mutate: func(i *SteeringIntent) { i.ApplicationBlock = 1 }},
	} {
		stateDir := filepath.Join(t.TempDir(), "state")
		store, err := NewIntentStore(stateDir)
		if err != nil {
			t.Fatal(err)
		}
		intent := testSteeringIntent(t, stateDir, 3, "")
		test.mutate(&intent)
		created, beginErr := store.Begin(intent)
		_, statErr := os.Lstat(store.path)
		if created != nil || beginErr == nil || !errors.Is(statErr, os.ErrNotExist) {
			t.Errorf("release intent Begin published an unreadable pending lifecycle: variant=%s error=%v exists=%t", test.name, beginErr, statErr == nil)
		}
	}
}

// Recording an uncertain submission must leave the same prepared bytes and
// pending restart guard readable; it cannot invent a terminal native outcome.
func TestReleaseShutdownPendingDiagnosticPreservesRestartReadability(t *testing.T) {
	steerer, intent := newReleaseShutdownPendingIntent(t)
	cause := errors.New("test-owned uncertain submission result")
	err := steerer.recordReleasePendingError(intent.VectorHash, cause)
	if !errors.Is(err, cause) {
		t.Fatalf("pending diagnostic lost original submission cause: %v", err)
	}
	encoded, err := os.ReadFile(steerer.intents.path)
	if err != nil {
		t.Fatal(err)
	}
	var state steeringIntentFile
	if err := json.Unmarshal(encoded, &state); err != nil {
		t.Fatal(err)
	}
	if state.Current == nil || state.Current.Status != "pending" || state.Current.VectorHash != intent.VectorHash || !reflect.DeepEqual(state.Current.Prepared, intent.Prepared) {
		t.Fatal("pending diagnostic changed the actual uncertain prepared submission")
	}
	restarted, err := NewIntentStore(steerer.intents.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	current, err := restarted.Current()
	if err != nil {
		t.Fatalf("release pending diagnostic made its genuine prepared intent unreadable on restart: %v", err)
	}
	if current == nil || current.Status != "pending" || current.VectorHash != intent.VectorHash || !reflect.DeepEqual(current.Prepared, intent.Prepared) {
		t.Fatal("restarted pending intent differs from exact prepared submission")
	}
}

// Pure cancellation still ends the loop without a spurious service failure.
func TestReleaseShutdownSteeringPureCancellationStillSucceeds(t *testing.T) {
	for _, boundary := range []string{"scheduler", "submit"} {
		ctx, cancel := context.WithCancel(context.Background())
		reads, submits := 0, 0
		err := runReleaseSteeringLoopWithWait(ctx, func() (uint64, error) {
			reads++
			if boundary == "scheduler" {
				cancel()
				return 0, context.Canceled
			}
			return 7, nil
		}, func() error { submits++; cancel(); return context.Canceled }, func() bool { return false })
		cancel()
		wantSubmits := 1
		if boundary == "scheduler" {
			wantSubmits = 0
		}
		if err != nil || reads != 1 || submits != wantSubmits {
			t.Errorf("pure %s cancellation changed behavior: reads=%d submits=%d error=%v", boundary, reads, submits, err)
		}
	}
}
