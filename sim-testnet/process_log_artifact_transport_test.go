// Artifact transport interruptions retain strict findings while an authorized
// provisional owner collects the rest of its exact terminal interval.
package main

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Preserve the R47 handler's record shape with reserved synthetic endpoints.
func artifactTransportTestLine() string {
	return "W0926 15:58:12.869839 123 sn_attempt_artifact_handlers.go:49] [sn-attempt] artifact stream failed: kind=records hash=0x" + strings.Repeat("ab", 32) + " bytes=3064430 elapsed=2m35.81601504s context=<nil> error=" + strconv.Quote("read tcp 192.0.2.1:60874->192.0.2.2:23900: read: connection reset by peer")
}

// Hashes, byte counts, stream kinds and addresses are data, not exemptions.
// Joined integrity causes and unrelated handlers remain ordinary hard errors.
func TestProcessLogArtifactTransportResetIsTypedAndBlocking(t *testing.T) {
	t.Parallel()
	line := artifactTransportTestLine()
	for _, candidate := range []string{
		line,
		strings.Replace(line, "kind=records", "kind=proofs", 1),
		strings.Replace(line, "kind=records", "kind=metadata", 1),
		strings.ReplaceAll(line, "read", "write"),
		strings.ReplaceAll(line, "192.0.2.1:60874", "[2001:db8::1]:60874"),
		strings.Replace(line, strings.Repeat("ab", 32), strings.Repeat("cd", 32), 1),
	} {
		classification, matched := classifyProcessLogLine([]byte(candidate))
		if !matched || classification.class != "artifact-stream-transport-reset" || classification.nonblockingDisposition != "" || classification.faultAttributable {
			t.Fatalf("reset lost its strict typed finding: %+v %q", classification, candidate)
		}
	}
	for _, mutation := range []struct{ from, to string }{
		{from: "sn_attempt_artifact_handlers.go", to: "other_handler.go"},
		{from: "context=<nil>", to: "context=context canceled"},
		{from: "kind=records", to: "kind=unknown"},
		{from: strings.Repeat("ab", 32), to: "malformed"},
		{from: "bytes=3064430", to: "bytes=-1"},
		{from: "elapsed=2m35.81601504s", to: "elapsed=-1s"},
		{from: "192.0.2.1:60874", to: "192.0.2.1:0"},
		{from: "192.0.2.1:60874", to: "192.0.2.1:060874"},
		{from: "192.0.2.2:23900", to: "not-an-address"},
		{from: ": read: connection reset", to: ": write: connection reset"},
		{from: "connection reset by peer", to: "context deadline exceeded"},
		{from: `by peer"`, to: `by peer\nartifact hash mismatch"`},
		{from: `by peer"`, to: `by peer\ncontext deadline exceeded"`},
	} {
		candidate := strings.Replace(line, mutation.from, mutation.to, 1)
		classification, matched := classifyProcessLogLine([]byte(candidate))
		if candidate == line || !matched || classification.class != "warning" || classification.nonblockingDisposition != "" {
			t.Fatalf("near-miss was forgiven: %+v %q", classification, candidate)
		}
	}
}

// Aggregation and reload keep every occurrence blocking, even when a fault
// happens to target the same process. Strict and mixed hard failures still stop.
func TestProcessLogArtifactTransportResetRetainedAcrossScans(t *testing.T) {
	t.Parallel()
	cfg, _, _, _ := newScenarioIntervalFixture(t)
	fixture := newProvisionalArtifactLogFixture(t)
	for range 2 {
		appendProcessLog(t, fixture.stderrPath, artifactTransportTestLine()+"\n")
		if err := fixture.gate.RequireClean(false); err == nil {
			t.Fatal("transport reset was globally forgiven")
		}
	}
	retained := append([]ProcessLogFinding(nil), fixture.gate.state.Findings...)
	if len(retained) != 1 || retained[0].Count != 2 || !retained[0].Blocking || retained[0].Disposition != "unexplained" {
		t.Fatal("reset aggregation changed original severity", retained)
	}
	reloaded, err := loadProcessLogGate(fixture.dir, fixture.manifest, fixture.supervisor)
	if err != nil || !reflect.DeepEqual(retained, reloaded.state.Findings) {
		t.Fatal("reload lost retained resets", err)
	}
	scanned, err := reloaded.Scan(true, processLogFaultScope{ID: "api-restart", Kind: "process-restart", Targets: []string{"operator-1-api"}})
	if err != nil {
		t.Fatal(err)
	}
	failure := processLogFindingsError(scanned.Findings)
	if failure == nil || !scenarioProcessLogFailureDeferred(cfg, "release-1.0", failure) || !scenarioProcessLogFailureDeferred(cfg, "production-soak", failure) {
		t.Fatal("provisional observation could not retain the transport finding", failure)
	}
	otherOperator := retained[0]
	otherOperator.ProcessID, otherOperator.Count = "operator-2-api", 1
	mixed := append(append([]ProcessLogFinding(nil), retained...), otherOperator,
		ProcessLogFinding{ProcessID: "validator-1", Role: "validator", Class: "release-steering-attempt-failure", Blocking: true, Count: 37},
		ProcessLogFinding{ProcessID: "validator-2", Role: "validator", Class: "release-steering-attempt-failure", Blocking: true, Count: 40})
	if !scenarioProcessLogFailureDeferred(cfg, "release-1.0", processLogFindingsError(mixed)) {
		t.Fatal("the four R47 stop-time finding classes did not all defer")
	}
	for _, candidate := range []error{errors.Join(failure, errors.New("evidence write failed")), processLogFindingsError([]ProcessLogFinding{{Class: "warning", Blocking: true, Count: 1}})} {
		if scenarioProcessLogFailureDeferred(cfg, "release-1.0", candidate) {
			t.Fatal("unknown or custody failure was deferred", candidate)
		}
	}
	if scenarioProcessLogFailureDeferred(nil, "release-1.0", failure) || scenarioProcessLogFailureDeferred(cfg, "epoch", failure) {
		t.Fatal("strict or unrelated owner deferred transport findings")
	}
	foreign := retained[0]
	foreign.Role = "validator"
	if scenarioProcessLogFailureDeferred(cfg, "release-1.0", processLogFindingsError([]ProcessLogFinding{foreign})) {
		t.Fatal("another process role borrowed the API transport class")
	}
	appendProcessLog(t, fixture.stderrPath, "W0926 15:58:13.000000 123 other.go:1] artifact hash mismatch\n")
	if scenarioProcessLogFailureDeferred(cfg, "release-1.0", reloaded.RequireClean(false), reloaded) {
		t.Fatal("adjacent integrity warning was hidden by the transport reset")
	}
}

// Existing v12 warning rows remain exact historical evidence after migration;
// only newly scanned transport records receive the more precise classification.
func TestProcessLogArtifactTransportMigrationPreservesV12(t *testing.T) {
	t.Parallel()
	fixture := newProvisionalArtifactLogFixture(t)
	appendProcessLog(t, fixture.stderrPath, "W0926 15:58:12.000000 123 other.go:1] retained warning\n")
	if err := fixture.gate.RequireClean(false); err == nil {
		t.Fatal("historical warning missing")
	}
	fixture.gate.state.Classifier = "urnetwork-sim-process-log-classifier-v12"
	retained := fixture.gate.state
	if err := fixture.gate.persistWithLock(); err != nil {
		t.Fatal(err)
	}
	reloaded, err := loadProcessLogGate(fixture.dir, fixture.manifest, fixture.supervisor)
	if err != nil || !reflect.DeepEqual(retained.Findings, reloaded.state.Findings) || !reflect.DeepEqual(retained.Cursors, reloaded.state.Cursors) || !reflect.DeepEqual(retained.AcceptanceBoundary, reloaded.state.AcceptanceBoundary) {
		t.Fatal("migration rewrote historical warning custody", err)
	}
	appendProcessLog(t, fixture.stderrPath, artifactTransportTestLine()+"\n")
	if err := reloaded.RequireClean(true); err == nil || len(reloaded.state.Findings) != 2 {
		t.Fatal("new reset merged into or forgave a historical warning", err)
	}
}

// Hold the full snapshot until a real fault-transition heartbeat scans the
// aggregated reset. The controller must still restore faults and reach its
// terminal checkpoint, with a failed final verdict and an unchanged baseline.
func TestScenarioArtifactTransportHeartbeatContinuesToFailedTerminal(t *testing.T) {
	t.Parallel()
	cfg, definition, window, observations := newScenarioIntervalFixture(t)
	fixture := newProvisionalArtifactLogFixture(t)
	definition.Checks = []scenarioCheck{{ID: "terminal", Check: func(evaluation *scenarioEvaluation) (bool, string) {
		return scenarioAcceptanceIntervalObserved(evaluation.Window, evaluation.Current), "terminal checkpoint"
	}}}
	driver := &scenarioIntervalHeartbeatFaultDriver{}
	heartbeatScanned := make(chan struct{})
	requestReady := make(chan struct{})
	var once sync.Once
	gate := &scenarioIntervalSignalLogGate{gate: fixture.gate, afterWrite: func() {
		if driver.applied.Load() {
			once.Do(func() { close(heartbeatScanned) })
		}
	}}
	probe := &scenarioIntervalProbe{observations: observations}
	var baseline *ScenarioObservation
	var baselineHash string
	probe.after = func(index int, observation *ScenarioObservation) {
		if index == 0 {
			baseline, baselineHash = observation, observation.ObservationHash
		}
	}
	probe.before = func(ctx context.Context, index int) error {
		if index != 1 {
			return nil
		}
		if err := os.WriteFile(fixture.stderrPath, []byte(strings.Repeat(artifactTransportTestLine()+"\n", 2)), 0o600); err != nil {
			return err
		}
		close(requestReady)
		select {
		case <-heartbeatScanned:
			if baseline.ObservationHash != baselineHash || len(baseline.ProcessLogFindings) != 0 {
				return errors.New("heartbeat changed retained baseline")
			}
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	heartbeat := &scenarioIntervalHeartbeatProbe{scenarioIntervalProbe: probe, head: ChainHead{Number: window.StartBlock + 1, Hash: "0x" + strings.Repeat("a4", 32)}, ready: requestReady}
	result, err := runScenarioWithProbe(t.Context(), cfg, fixture.dir, definition, heartbeat, scenarioRunOptions{PollInterval: time.Millisecond, Timeout: time.Hour, ProcessLogs: gate, FaultDriver: driver})
	if err == nil || result == nil || result.Result != "fail" || result.EndHead.Number != window.TerminalBlock || probe.calls.Load() != int64(len(observations)) || !faultsComplete(result.Faults) {
		t.Fatalf("reset stopped before terminal or forgave final acceptance: result=%+v reads=%d error=%v", result, probe.calls.Load(), err)
	}
	completionRejected := false
	for _, assertion := range result.Assertions {
		completionRejected = completionRejected || assertion.ID == "process_log_completion" && !assertion.Passed && strings.Contains(assertion.Message, "artifact-stream-transport-reset")
		if assertion.ID == "scenario_context" {
			t.Fatal("soft transport failure interrupted scenario context", assertion)
		}
	}
	if !completionRejected || fixture.gate.RequireClean(true) == nil || len(fixture.gate.state.Findings) != 1 || fixture.gate.state.Findings[0].Count != 2 {
		t.Fatal("terminal report lost strict reset findings")
	}
}
