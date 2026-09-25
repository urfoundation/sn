// Provisional runtime continuation authenticates the exact retained warning
// bytes and owner while every strict and terminal process-log gate still fails.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Reserved synthetic addresses preserve the server's quoted errors.Join wire.
func provisionalArtifactTestLine() string {
	return strings.TrimSuffix(artifactCancellationTestLine(), "error=context canceled") + "error=" + strconv.Quote("write tcp 192.0.2.1:18081->192.0.2.2:58672: write: broken pipe\ncontext canceled")
}

// The role, cursor paths and supervisor identity are all real gate inputs.
func newProvisionalArtifactLogFixture(t *testing.T) processLogGateFixture {
	t.Helper()
	dir := t.TempDir()
	processDir := filepath.Join(dir, "processes")
	if err := os.MkdirAll(processDir, 0o700); err != nil {
		t.Fatal(err)
	}
	stdoutPath := filepath.Join(processDir, "operator-1-api.stdout.log")
	stderrPath := filepath.Join(processDir, "operator-1-api.stderr.log")
	for _, path := range []string{stdoutPath, stderrPath} {
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	manifest := SupervisorFile{Schema: "urnetwork-sim-supervisor-v1", DeploymentID: "provisional-artifact-test", BinaryHash: "binary-hash", Specs: []ProcessSpec{{ID: "operator-1-api", Role: "operator-api", Identity: "operator-1", StdoutPath: stdoutPath, StderrPath: stderrPath}}}
	manifestHash, err := canonicalHashHex(manifest)
	if err != nil {
		t.Fatal(err)
	}
	supervisor := SupervisorState{Schema: "urnetwork-sim-supervisor-state-v1", SupervisorPID: os.Getpid(), SupervisorStartTimeTicks: currentProcessStartTimeTicks(t), ManifestHash: manifestHash, Processes: []ProcessState{{ID: "operator-1-api", Role: "operator-api", Identity: "operator-1", PID: os.Getpid(), Healthy: true}}}
	gate, err := initializeProcessLogGate(dir, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := gate.Bind(supervisor); err != nil {
		t.Fatal(err)
	}
	if _, _, err := gate.BindAcceptance(time.Date(2032, 1, 2, 3, 4, 5, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	return processLogGateFixture{dir: dir, stdoutPath: stdoutPath, stderrPath: stderrPath, manifest: manifest, supervisor: supervisor, gate: gate}
}

// The quoted disconnect remains a blocking warning; no classifier migration
// or content-substring exemption can forgive an adjacent integrity failure.
func TestProcessLogProvisionalArtifactRequiresExactDisconnect(t *testing.T) {
	t.Parallel()
	line := provisionalArtifactTestLine()
	classification, matched := classifyProcessLogLine([]byte(line))
	if !processLogArtifactClientDisconnect(line) || !matched || classification.class != "warning" || classification.nonblockingDisposition != "" {
		t.Fatalf("exact disconnect lost its original blocking classification: %+v", classification)
	}
	for _, mutation := range []struct{ from, to string }{
		{"sn_attempt_artifact_handlers.go", "other_handler.go"},
		{"context=context canceled", "context=<nil>"},
		{"kind=records", "kind=unknown"},
		{strings.Repeat("ab", 32), "malformed"},
		{"bytes=32768", "bytes=-1"},
		{"write: broken pipe", "write: connection reset by peer"},
		{"192.0.2.1:18081", "192.0.2.1:0"},
		{"192.0.2.1:18081", "192.0.2.1:018081"},
		{"192.0.2.2:58672", "not-an-address"},
		{`\ncontext canceled"`, `\ncontext canceled\nsynthetic storage failure"`},
		{`\ncontext canceled"`, `\ncontext canceled\ncontext deadline exceeded"`},
		{`\ncontext canceled"`, `\ncontext deadline exceeded"`},
	} {
		if candidate := strings.Replace(line, mutation.from, mutation.to, 1); candidate == line || processLogArtifactClientDisconnect(candidate) {
			t.Fatalf("near-miss accepted: %q", candidate)
		}
	}
	for _, candidate := range []string{artifactCancellationTestLine(), line + " synthetic storage failure", line + " panic: integrity failure"} {
		if processLogArtifactClientDisconnect(candidate) {
			t.Fatalf("unquoted or joined failure accepted: %q", candidate)
		}
	}
}

// Every permission binds one exact saved row, signed scope, role and cursor.
// Reload is allowed without rewriting history or reducing final strictness.
func TestProcessLogProvisionalArtifactAuthenticatesRetainedWarning(t *testing.T) {
	t.Parallel()
	fixture := newProvisionalArtifactLogFixture(t)
	appendProcessLog(t, fixture.stderrPath, provisionalArtifactTestLine()+"\n")
	if err := fixture.gate.RequireClean(false); err == nil || len(fixture.gate.state.Findings) != 1 {
		t.Fatal("quoted warning was globally forgiven")
	}
	finding := fixture.gate.state.Findings[0]
	before, err := os.ReadFile(fixture.gate.path)
	if err != nil {
		t.Fatal(err)
	}
	if !fixture.gate.provisionalArtifactStreamCancellation(finding) {
		t.Fatal("retained exact single disconnect refused")
	}
	for _, mutate := range []func(*ProcessLogFinding){
		func(row *ProcessLogFinding) { row.Class = "error" },
		func(row *ProcessLogFinding) { row.Role = "validator" },
		func(row *ProcessLogFinding) { row.ProcessID = "operator-2-api" },
		func(row *ProcessLogFinding) { row.Stream = "stdout" },
		func(row *ProcessLogFinding) { row.Count = 2 },
		func(row *ProcessLogFinding) { row.FirstOffset++ },
		func(row *ProcessLogFinding) { row.LastOffset++ },
		func(row *ProcessLogFinding) { row.FirstLineSHA256 = strings.Repeat("00", 32) },
		func(row *ProcessLogFinding) { row.LastLineSHA256 = strings.Repeat("00", 32) },
		func(row *ProcessLogFinding) { row.AcceptanceScope = "" },
		func(row *ProcessLogFinding) { row.AcceptanceScope = "0x" + strings.Repeat("01", 32) },
		func(row *ProcessLogFinding) { row.Disposition = "expected-fault" },
		func(row *ProcessLogFinding) { row.Blocking = false },
	} {
		candidate := finding
		mutate(&candidate)
		if fixture.gate.provisionalArtifactStreamCancellation(candidate) {
			t.Fatalf("changed retained authority accepted: %+v", candidate)
		}
	}
	after, err := os.ReadFile(fixture.gate.path)
	if err != nil || string(before) != string(after) {
		t.Fatal("runtime projection changed retained bytes", err)
	}
	reloaded, err := loadProcessLogGate(fixture.dir, fixture.manifest, fixture.supervisor)
	if err != nil || !reloaded.provisionalArtifactStreamCancellation(finding) || !reflect.DeepEqual(fixture.gate.state.Findings, reloaded.state.Findings) {
		t.Fatal("reload lost exact evidence", err)
	}
	if reloaded.RequireClean(true) == nil {
		t.Fatal("runtime permission weakened strict final acceptance")
	}
	appendProcessLog(t, fixture.stderrPath, provisionalArtifactTestLine()+"\n")
	if err := reloaded.RequireClean(false); err == nil || len(reloaded.state.Findings) != 1 || reloaded.state.Findings[0].Count != 2 || reloaded.provisionalArtifactStreamCancellation(reloaded.state.Findings[0]) {
		t.Fatal("aggregate endpoints were used to excuse unseen interior warnings", err)
	}
}

// Saved metadata cannot excuse a replaced, shortened or edited local log.
func TestProcessLogProvisionalArtifactRejectsChangedLocalSource(t *testing.T) {
	t.Parallel()
	for _, mutation := range []string{"bytes", "inode", "symlink", "truncated", "cursor", "scope"} {
		fixture := newProvisionalArtifactLogFixture(t)
		line := provisionalArtifactTestLine() + "\n"
		appendProcessLog(t, fixture.stderrPath, line)
		if err := fixture.gate.RequireClean(false); err == nil {
			t.Fatal("warning not retained")
		}
		finding := fixture.gate.state.Findings[0]
		switch mutation {
		case "bytes":
			if err := os.WriteFile(fixture.stderrPath, []byte(strings.Replace(line, "bytes=32768", "bytes=32769", 1)), 0o600); err != nil {
				t.Fatal(err)
			}
		case "inode", "symlink":
			retained := fixture.stderrPath + ".retained"
			if err := os.Rename(fixture.stderrPath, retained); err != nil {
				t.Fatal(err)
			}
			var err error
			if mutation == "inode" {
				err = os.WriteFile(fixture.stderrPath, []byte(line), 0o600)
			} else {
				err = os.Symlink(retained, fixture.stderrPath)
			}
			if err != nil {
				t.Fatal(err)
			}
		case "truncated":
			if err := os.Truncate(fixture.stderrPath, int64(len(line)-1)); err != nil {
				t.Fatal(err)
			}
		case "cursor":
			for index := range fixture.gate.state.Cursors {
				if fixture.gate.state.Cursors[index].Stream == "stderr" {
					fixture.gate.state.Cursors[index].Path = "elsewhere.log"
				}
			}
		case "scope":
			fixture.gate.state.AcceptanceBoundary.ContentHash = "0x" + strings.Repeat("02", 32)
		}
		if fixture.gate.provisionalArtifactStreamCancellation(finding) {
			t.Fatalf("changed local source accepted: %s", mutation)
		}
	}
}

// The projection must be explicitly supplied by the live gate under a locally
// authenticated provisional invocation, with all error-tree causes repairable.
func TestScenarioProvisionalArtifactDeferralRequiresOwner(t *testing.T) {
	t.Parallel()
	cfg, _, _, _ := newScenarioIntervalFixture(t)
	fixture := newProvisionalArtifactLogFixture(t)
	appendProcessLog(t, fixture.stderrPath, provisionalArtifactTestLine()+"\n")
	failure := fixture.gate.RequireClean(false)
	if failure == nil || !scenarioProcessLogFailureDeferred(cfg, "release-1.0", fmt.Errorf("heartbeat: %w", failure), fixture.gate) || !scenarioProcessLogFailureDeferred(cfg, "production-soak", errors.Join(failure, failure), fixture.gate) {
		t.Fatal("exact provisional owner could not continue", failure)
	}
	if scenarioProcessLogFailureDeferred(cfg, "release-1.0", failure) || scenarioProcessLogFailureDeferred(cfg, "release-1.0", failure, fixture.gate, fixture.gate) || scenarioProcessLogFailureDeferred(cfg, "release-1.0", failure, acceptedTlsScenarioLogGate{fixture.gate}) || scenarioProcessLogFailureDeferred(cfg, "epoch", failure, fixture.gate) {
		t.Fatal("missing owner or unrelated phase permitted a warning")
	}
	for _, candidate := range []error{errors.New(failure.Error()), errors.Join(failure, errors.New("source hash mismatch")), fmt.Errorf("persist: %w", errors.Join(failure, errors.New("disk write failed"))), errors.Join(failure, processLogFindingsError([]ProcessLogFinding{{Blocking: true, Class: "error"}}))} {
		if scenarioProcessLogFailureDeferred(cfg, "release-1.0", candidate, fixture.gate) {
			t.Fatalf("unclassified or joined cause was hidden: %v", candidate)
		}
	}
	for _, mutate := range []func(*ResolvedConfig){
		func(copy *ResolvedConfig) { copy.provisionalResume = nil },
		func(copy *ResolvedConfig) { copy.provisionalResume.Record.FinalAcceptance = true },
		func(copy *ResolvedConfig) { copy.provisionalResume.Record.Provisional = false },
		func(copy *ResolvedConfig) { copy.readOnlyAudit = true },
	} {
		copy := *cfg
		invocation := *cfg.provisionalResume
		record := *invocation.Record
		invocation.Record, copy.provisionalResume = &record, &invocation
		mutate(&copy)
		if scenarioProcessLogFailureDeferred(&copy, "release-1.0", failure, fixture.gate) {
			t.Fatal("strict or unapproved invocation deferred a warning")
		}
	}
}

// Network stale contracts keep their exact classifier; public or foreign
// streams remain independent hard errors even for provisional observation.
func TestScenarioProvisionalStaleContractDeferralIsNarrow(t *testing.T) {
	t.Parallel()
	cfg, _, _, _ := newScenarioIntervalFixture(t)
	line := "E0102 03:04:06.000000 123 transfer.go:1] [r]receiver<-sender s(00000000-0000-0000-0000-000000000000) exit contract verification failed (Network)"
	for _, entry := range []struct {
		line string
		want bool
	}{{line, true}, {strings.Replace(line, "(Network)", "(Public)", 1), false}, {strings.Replace(line, "000000000000)", "000000000001)", 1), false}} {
		classification, matched := classifyProcessLogLine([]byte(entry.line))
		if !matched {
			t.Fatal("stale diagnostic lost classification")
		}
		failure := processLogFindingsError([]ProcessLogFinding{{Class: classification.class, Blocking: true, Count: 1}})
		if scenarioProcessLogFailureDeferred(cfg, "release-1.0", failure) != entry.want {
			t.Fatalf("wrong stale-contract permission: %s", entry.line)
		}
		if scenarioProcessLogFailureDeferred(nil, "release-1.0", failure) {
			t.Fatal("strict owner waived stale contract")
		}
	}
}

// Expose authenticated readback while keeping an already-bound test scope.
type provisionalArtifactScenarioGate struct{ gate *processLogGate }

// Classification and durable evidence remain the actual gate's work.
func (self provisionalArtifactScenarioGate) Scan(final bool, faults ...processLogFaultScope) (processLogScanResult, error) {
	return self.gate.Scan(final, faults...)
}

// Preserve all blocking findings in each scenario evidence checkpoint.
func (self provisionalArtifactScenarioGate) WriteEvidence(runDir string) error {
	return self.gate.WriteEvidence(runDir)
}

// Only the retained gate can authenticate the warning's immutable source.
func (self provisionalArtifactScenarioGate) provisionalArtifactStreamCancellation(finding ProcessLogFinding) bool {
	return self.gate.provisionalArtifactStreamCancellation(finding)
}

// The real controller completes its full observation and restores the exact
// scheduled fault, but publishes a failed final verdict for the retained rows.
func TestScenarioProvisionalProcessFindingsContinueThroughTerminal(t *testing.T) {
	t.Parallel()
	cfg, definition, window, observations := newScenarioIntervalFixture(t)
	fixture := newProvisionalArtifactLogFixture(t)
	boundary := *fixture.gate.state.AcceptanceBoundary
	definition.Checks = []scenarioCheck{{ID: "terminal", Check: func(evaluation *scenarioEvaluation) (bool, string) {
		return scenarioAcceptanceIntervalObserved(evaluation.Window, evaluation.Current), "complete interval"
	}}}
	probe := &scenarioIntervalProbe{observations: observations, before: func(_ context.Context, index int) error {
		if index == 1 {
			appendProcessLog(t, fixture.stderrPath, provisionalArtifactTestLine()+"\nE0102 03:04:07.000000 123 transfer.go:1] [r]receiver<-sender s(00000000-0000-0000-0000-000000000000) exit contract verification failed (Network)\n")
		}
		if !reflect.DeepEqual(boundary, *fixture.gate.state.AcceptanceBoundary) {
			return errors.New("runtime continuation replaced acceptance scope")
		}
		return nil
	}}
	lifecycle := &artifactCancellationScenarioLifecycle{}
	driver := &fakeFaultDriver{}
	result, err := runScenarioWithProbe(t.Context(), cfg, fixture.dir, definition, probe, scenarioRunOptions{PollInterval: time.Microsecond, Timeout: time.Hour, ProcessLogs: provisionalArtifactScenarioGate{fixture.gate}, FaultDriver: driver, FleetLifecycle: lifecycle})
	if err == nil || result == nil || result.Result != "fail" || result.EndHead.Number != window.TerminalBlock || probe.calls.Load() != int64(len(observations)) || !lifecycle.Complete() || len(fixture.gate.state.Findings) != 2 {
		t.Fatalf("runtime continuation ended before terminal or forgave final evidence: result=%+v calls=%d error=%v", result, probe.calls.Load(), err)
	}
	completionRejected := false
	for _, assertion := range result.Assertions {
		completionRejected = completionRejected || assertion.ID == "process_log_completion" && !assertion.Passed
	}
	if !completionRejected || fixture.gate.RequireClean(true) == nil {
		t.Fatal("provisional continuation weakened final acceptance")
	}
	if driver.recovered != 1 || !reflect.DeepEqual(driver.applied, []string{"synthetic-pause"}) || !reflect.DeepEqual(driver.restored, []string{"synthetic-pause"}) {
		t.Fatalf("continuation skipped fault recovery or restoration: %+v", driver)
	}
	for _, finding := range fixture.gate.state.Findings {
		if !finding.Blocking || finding.Disposition != "unexplained" || finding.AcceptanceScope != boundary.ContentHash {
			t.Fatal("runtime continuation changed original finding", finding)
		}
	}
}
