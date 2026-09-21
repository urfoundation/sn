// Interrupted retry checkpoints preserve their original attempt and time
// budgets. Explicit clocks and responses keep these crash windows deterministic.
package main

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func reloadSnapshotRetryCheckpoint(t *testing.T, state *scenarioSnapshotRetryState) *scenarioSnapshotRetryState {
	t.Helper()
	reloaded := &scenarioSnapshotRetryState{runDir: state.runDir, phase: state.phase, now: state.now, wait: state.wait}
	if err := reloaded.load(); err != nil {
		t.Fatal(err)
	}
	return reloaded
}

// Simulate a process exit after the original failure was persisted and before
// the retry finished. Its first resumed read is already the last attempt.
func TestScenarioSnapshotRetryCheckpointDoesNotResetConsecutiveFailures(t *testing.T) {
	t.Parallel()
	state := newSnapshotRetryTestState(t)
	if allowed, err := state.record(snapshotRetryTestTransportError()); err != nil || !allowed {
		t.Fatalf("persist interrupted first failure: allowed=%t error=%v", allowed, err)
	}
	reloaded := reloadSnapshotRetryCheckpoint(t, state)
	probe := &snapshotRetryTestProbe{steps: []snapshotRetryTestStep{
		{err: snapshotRetryTestTransportError()}, {observation: &ScenarioObservation{}},
	}}
	_, err := reloaded.wrap(probe).Snapshot(t.Context())
	var terminal *scenarioSnapshotTerminalError
	if !errors.As(err, &terminal) || !strings.Contains(err.Error(), "budget exhausted") || probe.calls != 1 {
		t.Fatalf("reopened retry gained an extra attempt: calls=%d error=%v", probe.calls, err)
	}
	if records := reloaded.assertions(); len(records) != 2 || records[0].Passed || records[1].Passed {
		t.Fatalf("consecutive failed reads lost their outcomes: %+v", records)
	}
	closed := reloadSnapshotRetryCheckpoint(t, reloaded)
	healthy := &snapshotRetryTestProbe{steps: []snapshotRetryTestStep{{observation: &ScenarioObservation{}}}}
	if _, err := closed.wrap(healthy).Snapshot(t.Context()); err == nil || healthy.calls != 0 {
		t.Fatalf("terminal reload restarted observation: calls=%d error=%v", healthy.calls, err)
	}
}

func TestScenarioSnapshotRetryCheckpointKeepsRemainingDeadline(t *testing.T) {
	t.Parallel()
	state := newSnapshotRetryTestState(t)
	if _, err := state.record(snapshotRetryTestTransportError()); err != nil {
		t.Fatal(err)
	}
	now := state.now().Add(4 * time.Minute)
	state.now = func() time.Time { return now }
	reloaded := reloadSnapshotRetryCheckpoint(t, state)
	observation := &ScenarioObservation{ObservationHash: "fresh-after-restart"}
	probe := &snapshotRetryTestProbe{steps: []snapshotRetryTestStep{{observation: observation}},
		before: func(ctx context.Context, _ int) {
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) > time.Minute || time.Until(deadline) <= 0 {
				t.Fatalf("pending retry did not retain its one-minute remainder: deadline=%v present=%t", deadline, ok)
			}
		},
	}
	if got, err := reloaded.wrap(probe).Snapshot(t.Context()); err != nil || got != observation || probe.calls != 1 {
		t.Fatalf("bounded pending recovery failed: observation=%v calls=%d error=%v", got, probe.calls, err)
	}
	if records := reloaded.assertions(); len(records) != 1 || !records[0].Passed || records[0].DurationSeconds != (4*time.Minute).Seconds() {
		t.Fatalf("pending recovery rewrote the original incident: %+v", records)
	}
	if records := reloadSnapshotRetryCheckpoint(t, reloaded).assertions(); len(records) != 1 || !records[0].Passed {
		t.Fatalf("recovery outcome was not durable: %+v", records)
	}
}

// A crash between the second failed-read write and the terminal write must
// not reopen either consecutive failures or an exhausted phase budget.
func TestScenarioSnapshotRetryCheckpointRejectsUnsealedExhaustion(t *testing.T) {
	t.Parallel()
	for _, count := range []int{2, 3} {
		state := newSnapshotRetryTestState(t)
		for range count {
			if _, err := state.record(snapshotRetryTestTransportError()); err != nil {
				t.Fatal(err)
			}
		}
		reloaded := reloadSnapshotRetryCheckpoint(t, state)
		probe := &snapshotRetryTestProbe{steps: []snapshotRetryTestStep{{observation: &ScenarioObservation{}}}}
		if _, err := reloaded.wrap(probe).Snapshot(t.Context()); err == nil || !strings.Contains(err.Error(), "budget exhausted") || probe.calls != 0 {
			t.Fatalf("unsealed %d-failure checkpoint reopened: calls=%d error=%v", count, probe.calls, err)
		}
		if closed := reloadSnapshotRetryCheckpoint(t, reloaded); closed.terminalError == "" {
			t.Fatal("restart did not persist the inferred terminal verdict")
		}
	}
}

func TestScenarioSnapshotRetryCheckpointRejectsExpiredOrFutureIncident(t *testing.T) {
	t.Parallel()
	for _, elapsed := range []time.Duration{-time.Second, scenarioSnapshotRetryTimeout, scenarioSnapshotRetryTimeout + time.Second} {
		state := newSnapshotRetryTestState(t)
		if _, err := state.record(snapshotRetryTestTransportError()); err != nil {
			t.Fatal(err)
		}
		now := state.now().Add(elapsed)
		state.now = func() time.Time { return now }
		reloaded := reloadSnapshotRetryCheckpoint(t, state)
		probe := &snapshotRetryTestProbe{steps: []snapshotRetryTestStep{{observation: &ScenarioObservation{}}}}
		_, err := reloaded.wrap(probe).Snapshot(t.Context())
		if err == nil || probe.calls != 0 {
			t.Fatalf("invalid pending age %s reopened: calls=%d error=%v", elapsed, probe.calls, err)
		}
		if elapsed >= scenarioSnapshotRetryTimeout && !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expired checkpoint lost deadline cause: %v", err)
		}
		if records := reloaded.assertions(); len(records) != 1 || records[0].Passed {
			t.Fatalf("invalid chronology marked recovered: %+v", records)
		}
	}
}

func TestScenarioSnapshotRetryCheckpointRejectsMalformedChronology(t *testing.T) {
	t.Parallel()
	for _, mutate := range []func(*AssertionRecord){
		func(record *AssertionRecord) { record.StartedAt = "invalid" },
		func(record *AssertionRecord) { record.CompletedAt = "invalid" },
		func(record *AssertionRecord) { record.DurationSeconds = 1 },
		func(record *AssertionRecord) {
			started, _ := time.Parse(time.RFC3339Nano, record.StartedAt)
			record.CompletedAt = started.Add(-time.Second).Format(time.RFC3339Nano)
		},
		func(record *AssertionRecord) {
			started, _ := time.Parse(time.RFC3339Nano, record.StartedAt)
			record.CompletedAt = started.Add(time.Second).Format(time.RFC3339Nano)
			record.DurationSeconds = 1
		},
	} {
		state := newSnapshotRetryTestState(t)
		if _, err := state.record(snapshotRetryTestTransportError()); err != nil {
			t.Fatal(err)
		}
		var evidence scenarioSnapshotRetryEvidence
		path := filepath.Join(state.runDir, scenarioSnapshotRetryFilename)
		if err := decodeStrictJSONFile(path, &evidence); err != nil {
			t.Fatal(err)
		}
		mutate(&evidence.Records[0])
		if err := writePublicJSON(path, evidence); err != nil {
			t.Fatal(err)
		}
		reloaded := &scenarioSnapshotRetryState{runDir: state.runDir, phase: state.phase}
		if err := reloaded.load(); err == nil {
			t.Fatal("malformed retry checkpoint loaded")
		}
	}
}
