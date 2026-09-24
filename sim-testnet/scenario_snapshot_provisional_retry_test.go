// Provisional observations keep independent transport outages recoverable while
// preserving every failed read and the original deadline of an unfinished one.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	gethrpc "github.com/ethereum/go-ethereum/rpc"
)

func newProvisionalSnapshotRetryTestState(t *testing.T) *scenarioSnapshotRetryState {
	t.Helper()
	state := newSnapshotRetryTestState(t)
	state.provisional = true
	return state
}

func reloadProvisionalSnapshotRetryTestState(t *testing.T, state *scenarioSnapshotRetryState) *scenarioSnapshotRetryState {
	t.Helper()
	reloaded := &scenarioSnapshotRetryState{runDir: state.runDir, phase: state.phase, now: state.now, wait: state.wait, provisional: state.provisional}
	if err := reloaded.load(); err != nil {
		t.Fatal(err)
	}
	return reloaded
}

func TestProvisionalScenarioSnapshotRetryKeepsSeparateIncidentsAfterReload(t *testing.T) {
	t.Parallel()
	state := newProvisionalSnapshotRetryTestState(t)
	for index := range 4 {
		observation := &ScenarioObservation{ObservationHash: fmt.Sprintf("synthetic-fresh-observation-%d", index)}
		probe := &snapshotRetryTestProbe{steps: []snapshotRetryTestStep{{err: snapshotRetryTestTransportError()}, {observation: observation}}}
		if got, err := state.wrap(probe).Snapshot(t.Context()); err != nil || got != observation || probe.calls != 2 {
			t.Fatalf("incident %d: observation=%v calls=%d error=%v", index, got, probe.calls, err)
		}
		state = reloadProvisionalSnapshotRetryTestState(t, state)
		if records := state.assertions(); len(records) != index+1 {
			t.Fatalf("incident %d lost previous diagnostics: %+v", index, records)
		}
	}
	var evidence scenarioSnapshotRetryEvidence
	if err := decodeStrictJSONFile(filepath.Join(state.runDir, scenarioSnapshotRetryFilename), &evidence); err != nil {
		t.Fatal(err)
	}
	if evidence.Schema != "urnetwork-sim-snapshot-retries-v2" || !evidence.Provisional || evidence.MaximumRecoveries != scenarioProvisionalSnapshotMaximumRecords || len(evidence.Records) != 4 || evidence.TerminalError != "" {
		t.Fatalf("provisional policy or outcome was lost: %+v", evidence)
	}
	for _, record := range evidence.Records {
		if !record.Passed || !strings.Contains(record.Message, snapshotRetryTestTransportError().Error()) {
			t.Fatalf("recovery discarded the failed read: %+v", record)
		}
	}
}

func TestProvisionalScenarioSnapshotRetryUsesOneDeadlineAcrossRepeatedReads(t *testing.T) {
	t.Parallel()
	state := newProvisionalSnapshotRetryTestState(t)
	now := state.now()
	state.now = func() time.Time { return now }
	var delays []time.Duration
	var deadline time.Time
	state.wait = func(ctx context.Context, delay time.Duration) error {
		current, ok := ctx.Deadline()
		if !ok || time.Until(current) > scenarioSnapshotRetryTimeout {
			t.Fatal("retry lacks a bounded deadline")
		}
		if deadline.IsZero() {
			deadline = current
		} else if current != deadline {
			t.Fatal("a repeated read refreshed the incident deadline")
		}
		delays = append(delays, delay)
		now = now.Add(delay)
		return ctx.Err()
	}
	observation := &ScenarioObservation{ObservationHash: "synthetic-fresh-after-consecutive-timeouts"}
	probe := &snapshotRetryTestProbe{}
	for range 7 {
		probe.steps = append(probe.steps, snapshotRetryTestStep{err: snapshotRetryTestTransportError()})
	}
	probe.steps = append(probe.steps, snapshotRetryTestStep{observation: observation})
	if got, err := state.wrap(probe).Snapshot(t.Context()); err != nil || got != observation || probe.calls != 8 {
		t.Fatalf("consecutive transient reads: observation=%v calls=%d error=%v", got, probe.calls, err)
	}
	wantDelays := []time.Duration{250 * time.Millisecond, 500 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second, 5 * time.Second, 5 * time.Second}
	if !reflect.DeepEqual(delays, wantDelays) {
		t.Fatalf("retry backoff=%v want=%v", delays, wantDelays)
	}
	records := reloadProvisionalSnapshotRetryTestState(t, state).assertions()
	if len(records) != 7 || records[0].DurationSeconds != 17.75 {
		t.Fatalf("consecutive diagnostics changed: %+v", records)
	}
	for _, record := range records {
		if !record.Passed {
			t.Fatalf("fresh result did not resolve its pending read: %+v", record)
		}
	}
}

func TestProvisionalScenarioSnapshotRetryDeadlineSurvivesReload(t *testing.T) {
	t.Parallel()
	state := newProvisionalSnapshotRetryTestState(t)
	now := state.now()
	state.now = func() time.Time { return now }
	state.wait = func(ctx context.Context, delay time.Duration) error {
		if delay <= 0 || delay > scenarioProvisionalSnapshotMaximumDelay {
			t.Fatalf("unbounded delay %s", delay)
		}
		// Advance the incident clock explicitly, without waiting for wall time.
		now = now.Add(time.Minute)
		return ctx.Err()
	}
	probe := &snapshotRetryTestProbe{}
	for range 8 {
		probe.steps = append(probe.steps, snapshotRetryTestStep{err: snapshotRetryTestTransportError()})
	}
	if _, err := state.wrap(probe).Snapshot(t.Context()); !errors.Is(err, context.DeadlineExceeded) || probe.calls != 5 {
		t.Fatalf("incident deadline: calls=%d error=%v", probe.calls, err)
	}
	state = reloadProvisionalSnapshotRetryTestState(t, state)
	healthy := &snapshotRetryTestProbe{steps: []snapshotRetryTestStep{{observation: &ScenarioObservation{}}}}
	if _, err := state.wrap(healthy).Snapshot(t.Context()); err == nil || healthy.calls != 0 {
		t.Fatalf("terminal incident reopened: calls=%d error=%v", healthy.calls, err)
	}
	if records := state.assertions(); len(records) != 5 {
		t.Fatalf("terminal read census=%d want=5", len(records))
	} else {
		for _, record := range records {
			if record.Passed {
				t.Fatal("unresolved outage was marked recovered")
			}
		}
	}
}

func TestProvisionalScenarioSnapshotRetryPendingCheckpointKeepsOriginalAge(t *testing.T) {
	t.Parallel()
	state := newProvisionalSnapshotRetryTestState(t)
	now := state.now()
	state.now = func() time.Time { return now }
	for range 3 {
		if allowed, err := state.record(snapshotRetryTestTransportError()); err != nil || !allowed {
			t.Fatalf("checkpoint: allowed=%t error=%v", allowed, err)
		}
		now = now.Add(time.Minute)
	}
	state = reloadProvisionalSnapshotRetryTestState(t, state)
	probe := &snapshotRetryTestProbe{steps: []snapshotRetryTestStep{{observation: &ScenarioObservation{}}}, before: func(ctx context.Context, _ int) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 2*time.Minute || time.Until(deadline) <= 0 {
			t.Fatalf("pending incident did not retain original two-minute remainder: %v", deadline)
		}
	}}
	if _, err := state.wrap(probe).Snapshot(t.Context()); err != nil || probe.calls != 1 {
		t.Fatalf("pending multi-read recovery: calls=%d error=%v", probe.calls, err)
	}
	records := state.assertions()
	for index, record := range records {
		if !record.Passed || record.DurationSeconds != (time.Duration(3-index)*time.Minute).Seconds() {
			t.Fatalf("pending read %d changed its original age: %+v", index, record)
		}
	}
}

func TestProvisionalScenarioSnapshotRetryDiskCannotGrantInvocationAuthority(t *testing.T) {
	t.Parallel()
	state := newProvisionalSnapshotRetryTestState(t)
	if _, err := state.record(snapshotRetryTestTransportError()); err != nil {
		t.Fatal(err)
	}
	strict := &scenarioSnapshotRetryState{runDir: state.runDir, phase: state.phase, now: state.now, wait: state.wait}
	if err := strict.load(); err == nil || !strings.Contains(err.Error(), "no admitted retry policy") {
		t.Fatalf("disk granted provisional authority: %v", err)
	}
	if strict.provisionalRetries() || len(strict.assertions()) != 0 {
		t.Fatal("rejected evidence altered strict invocation state")
	}
}

func TestProvisionalScenarioSnapshotRetryRetainsStrictCheckpointBudget(t *testing.T) {
	t.Parallel()
	state := newSnapshotRetryTestState(t)
	for range 2 {
		probe := &snapshotRetryTestProbe{steps: []snapshotRetryTestStep{{err: snapshotRetryTestTransportError()}, {observation: &ScenarioObservation{}}}}
		if _, err := state.wrap(probe).Snapshot(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	state.provisional = true
	state = reloadProvisionalSnapshotRetryTestState(t, state)
	probe := &snapshotRetryTestProbe{steps: []snapshotRetryTestStep{{err: snapshotRetryTestTransportError()}, {observation: &ScenarioObservation{}}}}
	if _, err := state.wrap(probe).Snapshot(t.Context()); err == nil || !strings.Contains(err.Error(), "budget exhausted") || probe.calls != 1 {
		t.Fatalf("old strict evidence gained a retry budget: calls=%d error=%v", probe.calls, err)
	}
	var evidence scenarioSnapshotRetryEvidence
	if err := decodeStrictJSONFile(filepath.Join(state.runDir, scenarioSnapshotRetryFilename), &evidence); err != nil {
		t.Fatal(err)
	}
	if evidence.Schema != "urnetwork-sim-snapshot-retries-v1" || evidence.Provisional || evidence.MaximumRecoveries != scenarioSnapshotMaximumRecoveries || evidence.TerminalError == "" {
		t.Fatalf("strict budget or terminal outcome was relabeled: %+v", evidence)
	}
}

func TestProvisionalScenarioSnapshotRetryRejectsIntegrityAndOwnerCancellation(t *testing.T) {
	t.Parallel()
	for _, failure := range []error{
		errors.Join(snapshotRetryTestTransportError(), errors.New("synthetic evidence digest mismatch")),
		&os.PathError{Op: "read", Path: "synthetic-evidence.json", Err: io.ErrUnexpectedEOF},
		context.Canceled,
	} {
		state := newProvisionalSnapshotRetryTestState(t)
		probe := &snapshotRetryTestProbe{steps: []snapshotRetryTestStep{{err: failure}, {observation: &ScenarioObservation{}}}}
		if _, err := state.wrap(probe).Snapshot(t.Context()); !errors.Is(err, failure) || probe.calls != 1 || len(state.assertions()) != 0 {
			t.Fatalf("nontransport failure retried: calls=%d error=%v", probe.calls, err)
		}
	}
	state := newProvisionalSnapshotRetryTestState(t)
	ctx, cancel := context.WithCancel(t.Context())
	state.wait = func(context.Context, time.Duration) error { cancel(); return nil }
	probe := &snapshotRetryTestProbe{steps: []snapshotRetryTestStep{{err: snapshotRetryTestTransportError()}, {observation: &ScenarioObservation{}}}}
	if _, err := state.wrap(probe).Snapshot(ctx); !errors.Is(err, context.Canceled) || probe.calls != 1 {
		t.Fatalf("canceled owner started another read: calls=%d error=%v", probe.calls, err)
	}
}

func TestProvisionalScenarioSnapshotRetryAdmissionReachesScenarioLoop(t *testing.T) {
	t.Parallel()
	for _, finalAcceptance := range []bool{false, true} {
		cfg := testResolvedConfig(t)
		cfg.provisionalResume = &provisionalResumeState{Record: &provisionalResumeRecord{Provisional: true, FinalAcceptance: finalAcceptance}}
		definition := scenarioDefinition{Name: "unit-provisional-snapshot", Checks: []scenarioCheck{{ID: "advance", Check: func(e *scenarioEvaluation) (bool, string) {
			return e.Current.Status.Contracts.CurrentEpoch > 1, "wait for next observed epoch"
		}}}}
		probe := &snapshotRetryTestProbe{steps: []snapshotRetryTestStep{
			{err: snapshotRetryTestTransportError()}, {observation: testScenarioObservation(cfg, 1)},
			{err: snapshotRetryTestTransportError()}, {observation: testScenarioObservation(cfg, 1)},
			{err: snapshotRetryTestTransportError()}, {observation: testScenarioObservation(cfg, 2)},
		}}
		preparations := 0
		dir := t.TempDir()
		result, err := runScenarioWithProbe(t.Context(), cfg, dir, definition, probe, scenarioRunOptions{
			Prepare: func(context.Context) error { preparations++; return nil }, PollInterval: time.Microsecond,
		})
		wantCalls, wantResult := 6, "pass"
		if finalAcceptance {
			wantCalls, wantResult = 5, "fail"
		}
		if result == nil || result.Result != wantResult || (err != nil) != finalAcceptance || probe.calls != wantCalls || preparations != 1 {
			t.Fatalf("final acceptance=%t: result=%+v calls=%d preparations=%d error=%v", finalAcceptance, result, probe.calls, preparations, err)
		}
		var evidence scenarioSnapshotRetryEvidence
		if err := decodeStrictJSONFile(filepath.Join(dir, "runs", result.RunID, scenarioSnapshotRetryFilename), &evidence); err != nil {
			t.Fatal(err)
		}
		if evidence.Provisional == finalAcceptance || len(evidence.Records) != 3 {
			t.Fatalf("invocation authority/diagnostics changed: %+v", evidence)
		}
		retryCount := 0
		for _, assertion := range result.Assertions {
			if strings.HasPrefix(assertion.ID, "scenario_snapshot_retry_") {
				retryCount++
			}
		}
		if retryCount != 3 {
			t.Fatalf("scenario result dropped retry diagnostics: %d", retryCount)
		}
	}
}

func TestProvisionalScenarioSnapshotRetryPreservesTypedReadClassification(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		failure error
		retry   bool
	}{
		{failure: &evmReadRpcExhaustedError{operation: "synthetic pinned read", attempts: 4, attemptTimeout: 30 * time.Second, cause: context.DeadlineExceeded}, retry: true},
		{failure: &evmReadRpcExhaustedError{operation: "synthetic mixed read", attempts: 4, attemptTimeout: 30 * time.Second, cause: errors.Join(context.DeadlineExceeded, errors.New("synthetic evidence digest mismatch"))}},
		{failure: finalSemanticTestRPCError{code: -32002, message: "request timed out"}, retry: true},
		{failure: finalSemanticTestRPCError{code: -32005, message: "request limit exceeded"}, retry: true},
		{failure: finalSemanticTestRPCError{code: -32016, message: "server temporarily busy"}, retry: true},
		{failure: finalSemanticTestRPCError{code: -32000, message: "upstream overloaded"}, retry: true},
		{failure: &gethrpc.HTTPError{StatusCode: http.StatusTooEarly}, retry: true},
		{failure: &gethrpc.HTTPError{StatusCode: http.StatusServiceUnavailable}, retry: true},
		{failure: gethrpc.ErrMissingBatchResponse, retry: true},
		{failure: finalSemanticTestRPCError{code: -32002, message: "state already discarded; archive required"}},
		{failure: finalSemanticTestRPCError{code: 3, message: "execution reverted: timeout"}},
		{failure: &gethrpc.HTTPError{StatusCode: http.StatusUnauthorized}},
		{failure: gethrpc.ErrNoResult},
		{failure: context.DeadlineExceeded},
		{failure: &scenarioSnapshotTerminalError{cause: snapshotRetryTestTransportError()}},
	} {
		state := newProvisionalSnapshotRetryTestState(t)
		observation := &ScenarioObservation{ObservationHash: "synthetic-after-typed-read-failure"}
		probe := &snapshotRetryTestProbe{steps: []snapshotRetryTestStep{{err: test.failure}, {observation: observation}}}
		got, err := state.wrap(probe).Snapshot(t.Context())
		if test.retry {
			if err != nil || got != observation || probe.calls != 2 {
				t.Errorf("typed failure %v did not recover: calls=%d observation=%v error=%v", test.failure, probe.calls, got, err)
			}
			if records := state.assertions(); len(records) != 1 || !records[0].Passed {
				t.Errorf("typed failure %v lost its diagnostic: %+v", test.failure, records)
			}
		} else if !errors.Is(err, test.failure) || got != nil || probe.calls != 1 || len(state.assertions()) != 0 {
			t.Errorf("permanent failure %v was retried: calls=%d observation=%v error=%v", test.failure, probe.calls, got, err)
		}
	}
}

func TestProvisionalScenarioSnapshotRetryBoundsRetainedDiagnostics(t *testing.T) {
	t.Parallel()
	state := newProvisionalSnapshotRetryTestState(t)
	longFailure := errors.New(strings.Repeat("\x00", 8*1024))
	if allowed, err := state.record(longFailure); err != nil || !allowed {
		t.Fatalf("bounded diagnostic: allowed=%t error=%v", allowed, err)
	}
	if err := state.recovered(); err != nil {
		t.Fatal(err)
	}
	_ = state.terminal(longFailure)
	state = reloadProvisionalSnapshotRetryTestState(t, state)
	if !strings.Contains(state.records[0].Message, "[diagnostic truncated]") || !strings.Contains(state.terminalError, "[diagnostic truncated]") {
		t.Fatal("large diagnostics were not explicitly bounded")
	}
	// Bound a full ledger from one maximally escaped record, without making
	// thousands of disk writes or allocating a huge observation fixture.
	recordBytes, err := json.MarshalIndent(state.records[0], "    ", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if bound := (len(recordBytes)+16)*(scenarioProvisionalSnapshotMaximumRecords+1) + 64*1024; bound >= scenarioProvisionalSnapshotMaximumBytes {
		t.Fatalf("serialized diagnostics exceed their read admission: %d", bound)
	}
}

func TestProvisionalScenarioSnapshotRetryDiagnosticCapacityDoesNotReopen(t *testing.T) {
	t.Parallel()
	state := newProvisionalSnapshotRetryTestState(t)
	stamp := state.now().Format(time.RFC3339Nano)
	// Materialize only concise diagnostic rows; no chain census is needed to
	// exercise the exact durable storage boundary.
	for index := range scenarioProvisionalSnapshotMaximumRecords - 1 {
		state.records = append(state.records, AssertionRecord{ID: fmt.Sprintf("scenario_snapshot_retry_%06d", index+1), Message: "synthetic recovered transport read", Passed: true, StartedAt: stamp, CompletedAt: stamp})
	}
	lastAllowed := &snapshotRetryTestProbe{steps: []snapshotRetryTestStep{{err: snapshotRetryTestTransportError()}, {observation: &ScenarioObservation{}}}}
	if _, err := state.wrap(lastAllowed).Snapshot(t.Context()); err != nil || lastAllowed.calls != 2 {
		t.Fatalf("exact diagnostic capacity was refused: calls=%d error=%v", lastAllowed.calls, err)
	}
	state = reloadProvisionalSnapshotRetryTestState(t, state)
	overflow := &snapshotRetryTestProbe{steps: []snapshotRetryTestStep{{err: snapshotRetryTestTransportError()}, {observation: &ScenarioObservation{}}}}
	if _, err := state.wrap(overflow).Snapshot(t.Context()); err == nil || overflow.calls != 1 {
		t.Fatalf("one-over diagnostic capacity retried: calls=%d error=%v", overflow.calls, err)
	}
	state = reloadProvisionalSnapshotRetryTestState(t, state)
	healthy := &snapshotRetryTestProbe{steps: []snapshotRetryTestStep{{observation: &ScenarioObservation{}}}}
	if _, err := state.wrap(healthy).Snapshot(t.Context()); err == nil || healthy.calls != 0 || len(state.assertions()) != scenarioProvisionalSnapshotMaximumRecords+1 {
		t.Fatalf("reopening erased exhausted diagnostics: calls=%d records=%d error=%v", healthy.calls, len(state.assertions()), err)
	}
}
