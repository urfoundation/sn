// The actual retained API diagnostic must survive a signed log boundary and
// later scenario snapshots without hiding any adjacent stream failure.
package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Synthetic content identities and logger metadata preserve the wire shape.
func artifactCancellationTestLine() string {
	return "W0102 03:04:06.000000 123 sn_attempt_artifact_handlers.go:108] [sn-attempt] artifact stream failed: kind=records hash=0x" + strings.Repeat("ab", 32) + " bytes=32768 elapsed=2.5s context=context canceled error=context canceled"
}

// Only the exact canceled request is nonblocking, including under acceptance.
func TestProcessLogArtifactCancellationRequiresExactHandlerRecord(t *testing.T) {
	t.Parallel()
	line := artifactCancellationTestLine()
	classification, matched := classifyProcessLogLine([]byte(line))
	if !matched || classification.class != processLogClassArtifactCanceled || classification.nonblockingDisposition != "request-canceled" {
		t.Fatalf("owned request cancellation rejected: %+v", classification)
	}
	for _, mutation := range []struct{ from, to string }{
		{from: "sn_attempt_artifact_handlers.go", to: "another_handler.go"},
		{from: "context=context canceled", to: "context=<nil>"},
		{from: "error=context canceled", to: "error=context deadline exceeded"},
		{from: "error=context canceled", to: "error=write: broken pipe"},
		{from: "kind=records", to: "kind=arbitrary"},
		{from: "bytes=32768", to: "bytes=-1"},
		{from: "elapsed=2.5s", to: "elapsed=-1s"},
		{from: "hash=0x", to: "digest=0x"},
		{from: strings.Repeat("ab", 32), to: "malformed"},
		{from: "W0102", to: "E0102"},
	} {
		candidate := strings.Replace(line, mutation.from, mutation.to, 1)
		classification, matched := classifyProcessLogLine([]byte(candidate))
		if !matched || classification.nonblockingDisposition != "" {
			t.Fatalf("near-miss waived: %q %+v", candidate, classification)
		}
	}
	classification, _ = classifyProcessLogLine([]byte(line + " panic: integrity failed"))
	if classification.class != processLogClassPanic || classification.nonblockingDisposition != "" {
		t.Fatal("cancellation concealed a panic")
	}
}

// Old errors.Join output can straddle two polls and a driver reload. Its
// later disk/close error cannot hide beneath the first cancellation line.
func TestProcessLogArtifactCancellationRetainsJoinedFailuresAcrossReload(t *testing.T) {
	t.Parallel()
	fixture := newProcessLogGateFixture(t, "", "")
	if _, _, err := fixture.gate.BindAcceptance(time.Date(2032, 1, 2, 3, 4, 5, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	appendProcessLog(t, fixture.stderrPath, artifactCancellationTestLine()+"\ncontext canceled\n")
	if err := fixture.gate.RequireClean(false); err != nil {
		t.Fatal("pure canceled request blocked", err)
	}
	boundary := *fixture.gate.state.AcceptanceBoundary
	reloaded, err := loadProcessLogGate(fixture.dir, fixture.manifest, fixture.supervisor)
	if err != nil {
		t.Fatal(err)
	}
	appendProcessLog(t, fixture.stderrPath, "synthetic storage close failure\n")
	if err := reloaded.RequireClean(true); err == nil {
		t.Fatal("joined close error was lost across scanner recovery")
	}
	if !reflect.DeepEqual(boundary, *reloaded.state.AcceptanceBoundary) || len(reloaded.state.Findings) != 2 || reloaded.state.Findings[0].AcceptanceScope != boundary.ContentHash || reloaded.state.Findings[1].AcceptanceScope != boundary.ContentHash {
		t.Fatal("continuation changed acceptance or discarded the cause")
	}
}

// A later structured healthy record ends the cancellation continuation.
func TestProcessLogArtifactCancellationEndsAtNextLogRecord(t *testing.T) {
	t.Parallel()
	fixture := newProcessLogGateFixture(t, "", "")
	appendProcessLog(t, fixture.stderrPath, artifactCancellationTestLine()+"\ncontext canceled\nI0102 03:04:07.000000 123 handler.go:1] healthy\nnormal continuation\n")
	if err := fixture.gate.RequireClean(true); err != nil {
		t.Fatal("cancellation consumed a later independent log", err)
	}
	if len(fixture.gate.state.Findings) != 1 || fixture.gate.state.Findings[0].Blocking {
		t.Fatal("cancellation finding was lost or became blocking")
	}
}

// V9 findings remain immutable when the scanner acquires the new classifier.
func TestProcessLogArtifactCancellationMigratesWithoutForgivingHistory(t *testing.T) {
	t.Parallel()
	fixture := newProcessLogGateFixture(t, "", "")
	appendProcessLog(t, fixture.stderrPath, "W0102 03:04:06 historical unknown failure\n")
	if _, err := fixture.gate.Scan(false); err != nil {
		t.Fatal(err)
	}
	prior := append([]ProcessLogFinding(nil), fixture.gate.state.Findings...)
	fixture.gate.state.Classifier = processLogClassifierV9
	if err := fixture.gate.persistWithLock(); err != nil {
		t.Fatal(err)
	}
	reloaded, err := loadProcessLogGate(fixture.dir, fixture.manifest, fixture.supervisor)
	if err != nil || !reflect.DeepEqual(prior, reloaded.state.Findings) || reloaded.state.Classifier != processLogClassifierVersion {
		t.Fatalf("migration changed existing evidence: %v", err)
	}
}

// Record lifecycle progress through the actual scenario controller without
// an external chain, wall-clock deadline, or mutable global callback.
type artifactCancellationScenarioLifecycle struct {
	begin, bind, advances int
	window                *ScenarioAcceptanceWindow
	head                  uint64
}

// Preparation retains the same lifecycle owner.
func (self *artifactCancellationScenarioLifecycle) BeginPhase(string, string) error {
	self.begin++
	return nil
}

// Acceptance is bound once and kept across all observation retries.
func (self *artifactCancellationScenarioLifecycle) BindAcceptanceWindowForPhase(_ string, window *ScenarioAcceptanceWindow) error {
	self.bind++
	self.window = window
	return nil
}

// The real scenario owns ordering between snapshots, faults and lifecycle work.
func (self *artifactCancellationScenarioLifecycle) Advance(_ context.Context, observation *ScenarioObservation, _ []ScenarioFaultRecord) error {
	self.advances++
	self.head = observation.Status.Contracts.FinalizedHead.Number
	return nil
}

// Do not finish merely because the first current observation is healthy.
func (self *artifactCancellationScenarioLifecycle) Complete() bool {
	return self.window != nil && self.head >= self.window.TerminalBlock
}

// A real accepted log gate, scenario loop and lifecycle reach the terminal
// checkpoint after the exact handler cancellation that previously stopped Gen24.
func TestScenarioArtifactCancellationContinuesPastAcceptanceBoundary(t *testing.T) {
	t.Parallel()
	cfg, definition, window, observations := newScenarioIntervalFixture(t)
	cfg.provisionalResume = nil
	fixture := newProcessLogGateFixture(t, "", "")
	if _, _, err := fixture.gate.BindAcceptance(time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	boundary := *fixture.gate.state.AcceptanceBoundary
	definition.Checks = []scenarioCheck{{ID: "terminal", Check: func(evaluation *scenarioEvaluation) (bool, string) {
		return scenarioAcceptanceIntervalObserved(evaluation.Window, evaluation.Current), "complete interval"
	}}}
	probe := &scenarioIntervalProbe{observations: observations, before: func(_ context.Context, index int) error {
		if index == 1 {
			appendProcessLog(t, fixture.stderrPath, artifactCancellationTestLine()+"\ncontext canceled\ncontext canceled\n")
		}
		if !reflect.DeepEqual(boundary, *fixture.gate.state.AcceptanceBoundary) {
			return errors.New("acceptance scope was replaced by the canceled request")
		}
		return nil
	}}
	lifecycle := &artifactCancellationScenarioLifecycle{}
	result, err := runScenarioWithProbe(t.Context(), cfg, fixture.dir, definition, probe, scenarioRunOptions{PollInterval: time.Microsecond, Timeout: time.Hour, ProcessLogs: acceptedTlsScenarioLogGate{fixture.gate}, FaultDriver: &fakeFaultDriver{}, FleetLifecycle: lifecycle})
	if err != nil || result == nil || result.Result != "pass" || result.EndHead.Number != window.TerminalBlock || probe.calls.Load() != int64(len(observations)) || lifecycle.begin != 1 || lifecycle.bind != 1 || lifecycle.advances < 2 || !lifecycle.Complete() || !reflect.DeepEqual(boundary, *fixture.gate.state.AcceptanceBoundary) {
		t.Fatalf("cancellation reset or stopped accepted work: result=%+v lifecycle=%+v calls=%d error=%v", result, lifecycle, probe.calls.Load(), err)
	}
	if err := fixture.gate.RequireClean(true); err != nil || len(fixture.gate.state.Findings) != 1 || fixture.gate.state.Findings[0].Blocking {
		t.Fatal("completion lost cancellation evidence or rejected it", err)
	}
}

// A matching handler timeout is not cancellation and must still stop the
// actual controller before any later fault or lifecycle action is attempted.
func TestScenarioArtifactDeadlineStillStopsAfterAcceptanceBoundary(t *testing.T) {
	t.Parallel()
	cfg, definition, _, observations := newScenarioIntervalFixture(t)
	fixture := newProcessLogGateFixture(t, "", "")
	if _, _, err := fixture.gate.BindAcceptance(time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	probe := &scenarioIntervalProbe{observations: observations, before: func(_ context.Context, index int) error {
		if index == 1 {
			appendProcessLog(t, fixture.stderrPath, strings.ReplaceAll(artifactCancellationTestLine(), "context canceled", "context deadline exceeded")+"\n")
		}
		return nil
	}}
	result, err := runScenarioWithProbe(t.Context(), cfg, fixture.dir, definition, probe, scenarioRunOptions{PollInterval: time.Microsecond, Timeout: time.Hour, ProcessLogs: acceptedTlsScenarioLogGate{fixture.gate}, FaultDriver: &fakeFaultDriver{}})
	if err == nil || result == nil || result.Result != "fail" || probe.calls.Load() != 2 {
		t.Fatalf("deadline was confused with client cancellation: result=%+v reads=%d err=%v", result, probe.calls.Load(), err)
	}
}
