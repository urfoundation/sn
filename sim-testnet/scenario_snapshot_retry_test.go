// Deterministic retry fixtures use a fake wait and explicit transport types;
// no network, wall-clock sleeps, or broad error-message matching is required.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	gethrpc "github.com/ethereum/go-ethereum/rpc"
)

func snapshotRetryTestTransportError() error {
	return &net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}
}

type snapshotRetryTestStep struct {
	observation *ScenarioObservation
	err         error
}

type snapshotRetryTestProbe struct {
	steps  []snapshotRetryTestStep
	calls  int
	before func(context.Context, int)
}

func (self *snapshotRetryTestProbe) Snapshot(ctx context.Context) (*ScenarioObservation, error) {
	index := self.calls
	self.calls++
	if self.before != nil {
		self.before(ctx, index)
	}
	if index >= len(self.steps) {
		return nil, errors.New("unexpected extra snapshot")
	}
	return self.steps[index].observation, self.steps[index].err
}

func newSnapshotRetryTestState(t *testing.T) *scenarioSnapshotRetryState {
	t.Helper()
	runDir := filepath.Join(t.TempDir(), "retry-run")
	if err := ensurePrivateDir(runDir); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2032, 1, 2, 3, 4, 5, 0, time.UTC)
	return &scenarioSnapshotRetryState{runDir: runDir, phase: "unit-retry", now: func() time.Time { return now },
		wait: func(ctx context.Context, delay time.Duration) error { now = now.Add(delay); return ctx.Err() }}
}

func TestScenarioSnapshotRetryClassifiesOnlyTransportErrors(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		err   error
		retry bool
	}{
		{snapshotRetryTestTransportError(), true},
		{fmt.Errorf("RPC read: %w", &url.Error{Op: "Post", URL: "http://rpc.invalid", Err: io.ErrUnexpectedEOF}), true},
		{&net.OpError{Op: "read", Net: "tcp", Err: context.DeadlineExceeded}, true},
		{gethrpc.HTTPError{StatusCode: http.StatusServiceUnavailable}, true},
		{gethrpc.HTTPError{StatusCode: http.StatusUnauthorized}, false},
		{&json.SyntaxError{Offset: 3}, false},
		{io.EOF, false},
		{context.DeadlineExceeded, false},
		{context.Canceled, false},
		{errors.New("signature mismatch after RPC timeout"), false},
		{&os.PathError{Op: "read", Path: "signed-evidence.json", Err: io.ErrUnexpectedEOF}, false},
		{errors.Join(snapshotRetryTestTransportError(), errors.New("evidence digest mismatch")), false},
		{&url.Error{Op: "Post", URL: "https://rpc.invalid", Err: errors.New("certificate authentication failed")}, false},
	} {
		if got := scenarioSnapshotTransportError(test.err, false); got != test.retry {
			t.Errorf("%T (%v): retry=%t want=%t", test.err, test.err, got, test.retry)
		}
	}
}

// Two isolated recoveries consume the whole phase allowance, even after a
// state reload. A third incident is durable and terminal before another retry.
func TestScenarioSnapshotRetryPhaseBudgetSurvivesReload(t *testing.T) {
	t.Parallel()
	state := newSnapshotRetryTestState(t)
	observation := &ScenarioObservation{ObservationHash: "synthetic-observation"}
	steps := []snapshotRetryTestStep{{err: snapshotRetryTestTransportError()}, {observation: observation}}
	first := &snapshotRetryTestProbe{steps: steps}
	if got, err := state.wrap(first).Snapshot(t.Context()); err != nil || got != observation || first.calls != 2 {
		t.Fatalf("first recovery: got=%v calls=%d error=%v", got, first.calls, err)
	}
	reloaded := &scenarioSnapshotRetryState{runDir: state.runDir, phase: state.phase, now: state.now, wait: state.wait}
	if err := reloaded.load(); err != nil {
		t.Fatal(err)
	}
	second := &snapshotRetryTestProbe{steps: steps}
	if _, err := reloaded.wrap(second).Snapshot(t.Context()); err != nil {
		t.Fatal(err)
	}
	third := &snapshotRetryTestProbe{steps: steps}
	if _, err := reloaded.wrap(third).Snapshot(t.Context()); err == nil || !strings.Contains(err.Error(), "budget exhausted") || third.calls != 1 {
		t.Fatalf("phase cap: calls=%d error=%v", third.calls, err)
	}
	records := reloaded.assertions()
	if len(records) != 3 || !records[0].Passed || !records[1].Passed || records[2].Passed {
		t.Fatalf("retry outcomes lost: %+v", records)
	}
	var evidence scenarioSnapshotRetryEvidence
	if err := decodeStrictJSONFile(filepath.Join(state.runDir, scenarioSnapshotRetryFilename), &evidence); err != nil || len(evidence.Records) != 3 {
		t.Fatalf("durable incidents=%+v error=%v", evidence, err)
	}
}

func TestScenarioSnapshotRetryPersistentAndIntegrityFailuresStop(t *testing.T) {
	t.Parallel()
	for _, failure := range []error{snapshotRetryTestTransportError(), errors.New("signed evidence digest mismatch"), &json.SyntaxError{Offset: 9}} {
		state := newSnapshotRetryTestState(t)
		probe := &snapshotRetryTestProbe{steps: []snapshotRetryTestStep{{err: failure}, {err: failure}, {observation: &ScenarioObservation{}}}}
		_, err := state.wrap(probe).Snapshot(t.Context())
		var terminal *scenarioSnapshotTerminalError
		if !errors.As(err, &terminal) {
			t.Fatalf("failure was not terminal: %v", err)
		}
		wantCalls, wantRecords := 1, 0
		if scenarioSnapshotTransportError(failure, false) {
			wantCalls, wantRecords = 2, 2
		}
		if probe.calls != wantCalls || len(state.assertions()) != wantRecords {
			t.Fatalf("failure %v: calls=%d records=%d", failure, probe.calls, len(state.assertions()))
		}
		for _, record := range state.assertions() {
			if record.Passed {
				t.Fatal("persistent failure was marked recovered")
			}
		}
		if wantRecords != 0 {
			reloaded := &scenarioSnapshotRetryState{runDir: state.runDir, phase: state.phase, now: state.now, wait: state.wait}
			if err := reloaded.load(); err != nil {
				t.Fatal(err)
			}
			healthy := &snapshotRetryTestProbe{steps: []snapshotRetryTestStep{{observation: &ScenarioObservation{}}}}
			if _, err := reloaded.wrap(healthy).Snapshot(t.Context()); err == nil || healthy.calls != 0 {
				t.Fatalf("reload erased persistent failure: calls=%d error=%v", healthy.calls, err)
			}
		}
	}
}

// The retry receives a real bounded context, while the deterministic wait
// forces timeout/cancellation before a second network read can start.
func TestScenarioSnapshotRetryDeadlineCancellationAndEvidenceWriteFailure(t *testing.T) {
	t.Parallel()
	for _, failure := range []error{context.DeadlineExceeded, context.Canceled} {
		state := newSnapshotRetryTestState(t)
		state.wait = func(ctx context.Context, delay time.Duration) error {
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) > scenarioSnapshotRetryTimeout || delay != scenarioSnapshotRetryDelay {
				t.Fatal("retry has no explicit deadline/backoff")
			}
			return failure
		}
		probe := &snapshotRetryTestProbe{steps: []snapshotRetryTestStep{{err: snapshotRetryTestTransportError()}}}
		if _, err := state.wrap(probe).Snapshot(t.Context()); !errors.Is(err, failure) || probe.calls != 1 {
			t.Fatalf("retry wait failure: calls=%d error=%v", probe.calls, err)
		}
		if records := state.assertions(); len(records) != 1 || records[0].Passed {
			t.Fatalf("unresolved retry was lost: %+v", records)
		}
	}
	state := newSnapshotRetryTestState(t)
	if err := os.Mkdir(filepath.Join(state.runDir, scenarioSnapshotRetryFilename), 0o700); err != nil {
		t.Fatal(err)
	}
	probe := &snapshotRetryTestProbe{steps: []snapshotRetryTestStep{{err: snapshotRetryTestTransportError()}}}
	if _, err := state.wrap(probe).Snapshot(t.Context()); err == nil || !strings.Contains(err.Error(), "persist snapshot retry") || probe.calls != 1 {
		t.Fatalf("unrecorded retry proceeded: calls=%d error=%v", probe.calls, err)
	}
}

// Both preparation boundaries use the same wrapper and allowance as the loop.
// Preparation is executed once; only the failed read is retried.
func TestScenarioSnapshotRetryInitialAndPostPreparationRecover(t *testing.T) {
	t.Parallel()
	cfg := testResolvedConfig(t)
	definition := scenarioDefinition{Name: "unit-preparation-retry", Checks: []scenarioCheck{{ID: "healthy", Check: func(*scenarioEvaluation) (bool, string) { return true, "healthy" }}}}
	probe := &snapshotRetryTestProbe{steps: []snapshotRetryTestStep{
		{err: snapshotRetryTestTransportError()}, {observation: testScenarioObservation(cfg, 1)},
		{err: snapshotRetryTestTransportError()}, {observation: testScenarioObservation(cfg, 2)},
	}}
	preparations := 0
	dir := t.TempDir()
	result, err := runScenarioWithProbe(t.Context(), cfg, dir, definition, probe, scenarioRunOptions{Prepare: func(context.Context) error { preparations++; return nil }})
	if err != nil || result == nil || result.Result != "pass" || preparations != 1 || probe.calls != 4 {
		t.Fatalf("preparation recovery: result=%+v calls=%d preparations=%d error=%v", result, probe.calls, preparations, err)
	}
	retries := 0
	for _, assertion := range result.Assertions {
		if strings.HasPrefix(assertion.ID, "scenario_snapshot_retry_") {
			retries++
			if !assertion.Passed {
				t.Fatal("recovered preparation left a failing assertion")
			}
		}
	}
	if retries != 2 {
		t.Fatalf("signed result retained %d retry diagnostics", retries)
	}
	var evidence scenarioSnapshotRetryEvidence
	if err := decodeStrictJSONFile(filepath.Join(dir, "runs", result.RunID, scenarioSnapshotRetryFilename), &evidence); err != nil || len(evidence.Records) != 2 {
		t.Fatalf("durable preparation retry evidence=%+v error=%v", evidence, err)
	}
}

// Permanent errors and repeated transport failures stop the actual accepted
// loop; the outer loop cannot silently reset the wrapper's retry allowance.
func TestScenarioSnapshotRetryLoopExhaustionAndIntegrityStayBlocking(t *testing.T) {
	t.Parallel()
	for _, failure := range []error{snapshotRetryTestTransportError(), errors.New("signed evidence digest mismatch")} {
		cfg := testResolvedConfig(t)
		definition := scenarioDefinition{Name: "unit-loop-failure", Checks: []scenarioCheck{{ID: "advance", Check: func(e *scenarioEvaluation) (bool, string) { return e.Current.Status.Contracts.CurrentEpoch > 1, "wait" }}}}
		probe := &snapshotRetryTestProbe{steps: []snapshotRetryTestStep{{observation: testScenarioObservation(cfg, 1)}, {err: failure}, {err: failure}, {observation: testScenarioObservation(cfg, 2)}}}
		result, err := runScenarioWithProbe(t.Context(), cfg, t.TempDir(), definition, probe, scenarioRunOptions{PollInterval: time.Microsecond, Timeout: time.Second})
		wantCalls := 2
		if scenarioSnapshotTransportError(failure, false) {
			wantCalls = 3
		}
		if err == nil || result == nil || result.Result != "fail" || probe.calls != wantCalls {
			t.Fatalf("terminal loop error: result=%+v calls=%d error=%v", result, probe.calls, err)
		}
	}
}

// A wrapped slow observation still exposes finalized heads to the scheduler;
// completing the observation depends on the heartbeat being delivered first.
func TestScenarioSnapshotRetryPreservesFaultHeartbeat(t *testing.T) {
	t.Parallel()
	state := newSnapshotRetryTestState(t)
	release := make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	source := blockedHeartbeatProbe{release: release, head: ChainHead{Number: 42, Hash: "synthetic-head"}}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	heartbeats := 0
	observation, err := waitScenarioSnapshot(ctx, state.wrap(source), time.Microsecond, func(_ context.Context, head ChainHead) error {
		if head != source.head {
			return errors.New("wrapped probe changed the finalized heartbeat")
		}
		heartbeats++
		once.Do(func() { close(release) })
		return nil
	})
	if err != nil || observation == nil || heartbeats == 0 {
		t.Fatalf("retry wrapper blocked fault timing: heartbeats=%d error=%v", heartbeats, err)
	}
}

// A controlled deadline lets the scheduler return before a slow canceled
// observer unwinds, without depending on timer delivery or wall-clock sleeps.
type snapshotRetryDeadlineContext struct {
	context.Context
	expired chan struct{}
}

func (self *snapshotRetryDeadlineContext) Done() <-chan struct{} { return self.expired }
func (self *snapshotRetryDeadlineContext) Err() error {
	select {
	case <-self.expired:
		return context.DeadlineExceeded
	default:
		return nil
	}
}

func TestScenarioSnapshotRetryParentDeadlineStopsLoop(t *testing.T) {
	t.Parallel()
	cfg := testResolvedConfig(t)
	ctx := &snapshotRetryDeadlineContext{Context: t.Context(), expired: make(chan struct{})}
	release := make(chan struct{})
	defer close(release)
	probe := &snapshotRetryTestProbe{steps: []snapshotRetryTestStep{{observation: testScenarioObservation(cfg, 1)}, {err: context.DeadlineExceeded}},
		before: func(_ context.Context, index int) {
			if index == 1 {
				close(ctx.expired)
				<-release
			}
		}}
	var clockTicks atomic.Int64
	clock := func() time.Time {
		return time.Date(2032, 1, 2, 3, 4, 5, 0, time.UTC).Add(time.Duration(clockTicks.Add(1)) * time.Second)
	}
	definition := scenarioDefinition{Name: "unit-parent-deadline", Checks: []scenarioCheck{{ID: "advance", Check: func(*scenarioEvaluation) (bool, string) { return false, "wait" }}}}
	result, err := runScenarioWithProbe(ctx, cfg, t.TempDir(), definition, probe, scenarioRunOptions{Now: clock, PollInterval: time.Microsecond, Timeout: 3 * time.Second})
	if err == nil || result == nil || result.Result != "fail" {
		t.Fatalf("parent deadline did not fail the phase: result=%+v error=%v", result, err)
	}
	found := false
	for _, assertion := range result.Assertions {
		if assertion.ID == "scenario_context" && strings.Contains(assertion.Message, context.DeadlineExceeded.Error()) {
			found = true
		}
		if strings.HasPrefix(assertion.ID, "scenario_snapshot_") {
			t.Fatal("expired parent context was retried as a transient observation")
		}
	}
	if !found {
		t.Fatal("terminal parent deadline was not recorded")
	}
}
