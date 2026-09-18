package main

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// Exposes the accepted gate's real scan/evidence methods to the small scenario
// fixture without asking that fixture to establish a new acceptance window.
type acceptedTlsScenarioLogGate struct {
	scenarioProcessLogGate
}

// A single retryable handshake failure is retained without aborting runtime
// observation; reloading and final scanning must keep the exact same budget.
func TestProcessLogTlsTransientSurvivesReloadAndFinalScan(t *testing.T) {
	fixture := newProcessLogGateFixture(t, "", "")
	boundAt := time.Date(2032, time.January, 2, 3, 4, 5, 0, time.UTC)
	if _, _, err := fixture.gate.BindAcceptance(boundAt); err != nil {
		t.Fatal(err)
	}
	appendProcessLog(t, fixture.stderrPath, "E0102 03:04:06 completeHandshake failed: tls handshake timeout\n")
	observation := &ScenarioObservation{}
	runDir := filepath.Join(fixture.dir, "run")
	if err := scanScenarioProcessLogs(fixture.gate, runDir, observation, false); err != nil {
		t.Fatalf("isolated handshake failure aborted the campaign: %v", err)
	}
	if len(observation.ProcessLogFindings) != 1 || observation.ProcessLogFindings[0].Blocking || observation.ProcessLogFindings[0].Disposition != "isolated-network-transient" {
		t.Fatalf("isolated failure projection: %+v", observation.ProcessLogFindings)
	}
	raw := fixture.gate.state.Findings[0]
	if !raw.Blocking || raw.Disposition != "unexplained" || raw.Count != 1 || raw.FirstLineSHA256 == "" {
		t.Fatalf("raw timeout evidence changed: %+v", raw)
	}
	var published processLogGateState
	if err := readJSONFile(filepath.Join(runDir, processLogEvidenceFilename), &published); err != nil || len(published.Findings) != 1 || !reflect.DeepEqual(published.Findings[0], raw) {
		t.Fatalf("published raw evidence differs: %+v %v", published.Findings, err)
	}
	reloaded, err := loadProcessLogGate(fixture.dir, fixture.manifest, fixture.supervisor)
	if err != nil {
		t.Fatal(err)
	}
	if err := scanScenarioProcessLogs(reloaded, runDir, observation, true); err != nil {
		t.Fatalf("isolated timeout failed the final scan after recovery: %v", err)
	}
	appendProcessLog(t, fixture.stdoutPath, "E0102 03:04:07 completeHandshake failed: tls handshake timeout\n")
	if err := scanScenarioProcessLogs(reloaded, runDir, observation, true); err != nil {
		t.Fatalf("one retry in the other stream aborted the final gate: %v", err)
	}
	appendProcessLog(t, fixture.stderrPath, "E0102 03:04:08 completeHandshake failed: tls handshake timeout\n")
	if err := scanScenarioProcessLogs(reloaded, runDir, observation, true); err == nil {
		t.Fatal("a third timeout escaped the final gate")
	}
	if len(blockingProcessLogFindings(observation.ProcessLogFindings)) != 2 {
		t.Fatalf("third timeout did not remain blocking: %+v", observation.ProcessLogFindings)
	}
}

// Per-process limits alone would hide a widespread outage across many workers.
// The exact class and acceptance scope also keep hard failures out of the rule.
func TestProcessLogTlsTransientBudgetAndAdjacentFailures(t *testing.T) {
	makeFinding := func(process string) ProcessLogFinding {
		return ProcessLogFinding{ProcessID: process, Class: "tls-handshake-timeout", Blocking: true, Disposition: "unexplained", Count: 1, AcceptanceScope: "accepted"}
	}
	findings := []ProcessLogFinding{makeFinding("miner-1"), makeFinding("miner-2")}
	projectIsolatedProcessLogTlsTimeouts(findings)
	if len(blockingProcessLogFindings(findings)) != 0 {
		t.Fatal("two independently isolated timeouts were rejected")
	}
	findings = []ProcessLogFinding{makeFinding("miner-1"), makeFinding("miner-2"), makeFinding("miner-3")}
	projectIsolatedProcessLogTlsTimeouts(findings)
	if len(blockingProcessLogFindings(findings)) != 3 {
		t.Fatal("widespread timeout exceeded the campaign budget")
	}
	findings = []ProcessLogFinding{makeFinding("miner-1"), makeFinding("miner-2"), makeFinding("miner-3")}
	findings[0].Class = "panic"
	findings[1].AcceptanceScope = ""
	findings[2].Count = ^uint64(0)
	projectIsolatedProcessLogTlsTimeouts(findings)
	if len(blockingProcessLogFindings(findings)) != 3 {
		t.Fatal("hard, unscoped, or overflowing findings were waived")
	}
	// Two retries from one worker fit the same global budget; a third does not.
	findings = []ProcessLogFinding{makeFinding("miner-1")}
	findings[0].Count = 2
	projectIsolatedProcessLogTlsTimeouts(findings)
	if findings[0].Blocking {
		t.Fatalf("bounded worker retry remained blocking: %+v", findings)
	}
	findings[0].Blocking, findings[0].Disposition, findings[0].Count = true, "unexplained", 3
	projectIsolatedProcessLogTlsTimeouts(findings)
	if !findings[0].Blocking {
		t.Fatalf("third worker timeout was waived: %+v", findings)
	}
}

func TestProcessLogIsolatedExitGapTimeoutRequiresSingleWorker(t *testing.T) {
	makeFinding := func(process string, count uint64) ProcessLogFinding {
		return ProcessLogFinding{ProcessID: process, Class: "exit-gap-timeout", Blocking: true, Disposition: "unexplained", Count: count, AcceptanceScope: "accepted"}
	}
	findings := []ProcessLogFinding{makeFinding("miner-19", 1)}
	projectIsolatedProcessLogExitGapTimeout(findings)
	if findings[0].Blocking || findings[0].Disposition != "isolated-worker-recovery" {
		t.Fatalf("isolated worker recovery remained blocking: %+v", findings)
	}

	findings = []ProcessLogFinding{makeFinding("miner-19", 2)}
	projectIsolatedProcessLogExitGapTimeout(findings)
	if !findings[0].Blocking {
		t.Fatalf("repeated exit gap was waived: %+v", findings)
	}
	findings = []ProcessLogFinding{makeFinding("miner-19", 1), makeFinding("miner-20", 1)}
	projectIsolatedProcessLogExitGapTimeout(findings)
	if !findings[0].Blocking || !findings[1].Blocking {
		t.Fatalf("multi-worker exit gap was waived: %+v", findings)
	}
	findings = []ProcessLogFinding{makeFinding("miner-19", 1)}
	findings[0].AcceptanceScope = ""
	projectIsolatedProcessLogExitGapTimeout(findings)
	if !findings[0].Blocking {
		t.Fatal("unscoped exit gap was waived")
	}
}

// An older classifier's signed scope and raw findings survive migration;
// reopening cannot reset the bounded per-process retry count.
func TestProcessLogTlsTransientMigratesV4WithoutResettingEvidence(t *testing.T) {
	fixture := newProcessLogGateFixture(t, "", "")
	if _, _, err := fixture.gate.BindAcceptance(time.Date(2032, time.January, 2, 3, 4, 5, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	appendProcessLog(t, fixture.stderrPath, "E0102 03:04:06 completeHandshake failed: tls handshake timeout\n")
	if _, err := fixture.gate.Scan(false); err != nil {
		t.Fatal(err)
	}
	prior := append([]ProcessLogFinding(nil), fixture.gate.state.Findings...)
	boundary := *fixture.gate.state.AcceptanceBoundary
	fixture.gate.state.Classifier = processLogClassifierV4
	if err := fixture.gate.persistWithLock(); err != nil {
		t.Fatal(err)
	}
	reloaded, err := loadProcessLogGate(fixture.dir, fixture.manifest, fixture.supervisor)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.state.Classifier != processLogClassifierVersion || !reflect.DeepEqual(prior, reloaded.state.Findings) || !reflect.DeepEqual(boundary, *reloaded.state.AcceptanceBoundary) {
		t.Fatal("classifier migration replaced retained evidence or its signed boundary")
	}
	appendProcessLog(t, fixture.stderrPath, "E0102 03:04:07 completeHandshake failed: tls handshake timeout\n")
	if err := reloaded.RequireClean(true); err != nil {
		t.Fatalf("reloading rejected the bounded second timeout: %v", err)
	}
	if reloaded.state.Findings[0].Count != 2 || reloaded.state.Findings[0].FirstLineSHA256 != prior[0].FirstLineSHA256 {
		t.Fatal("repeated timeout lost the original count or line identity")
	}
	appendProcessLog(t, fixture.stderrPath, "E0102 03:04:08 completeHandshake failed: tls handshake timeout\n")
	if err := reloaded.RequireClean(true); err == nil {
		t.Fatal("reloading forgave a third timeout in the same stream")
	}
}

// Exercise the scenario completion path, not just the classifier, with real
// persisted log bytes. It must retain the transient without an open anomaly.
func TestScenarioContinuesAfterIsolatedTlsTimeout(t *testing.T) {
	cfg := testResolvedConfig(t)
	fixture := newProcessLogGateFixture(t, "", "")
	if _, _, err := fixture.gate.BindAcceptance(time.Date(2032, time.January, 2, 3, 4, 5, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	appendProcessLog(t, fixture.stderrPath, "E0102 03:04:06 completeHandshake failed: tls handshake timeout\n")
	definition := scenarioDefinition{Name: "unit-tls-transient", Checks: []scenarioCheck{{ID: "healthy", Check: func(*scenarioEvaluation) (bool, string) { return true, "healthy" }}}}
	result, err := runScenarioWithProbe(context.Background(), cfg, fixture.dir, definition, &staticScenarioProbe{observations: []*ScenarioObservation{testScenarioObservation(cfg, 1)}}, scenarioRunOptions{Publish: false, ProcessLogs: acceptedTlsScenarioLogGate{fixture.gate}})
	if err != nil || result == nil || result.Result != "pass" {
		t.Fatalf("isolated TLS timeout stopped scenario: result=%+v err=%v", result, err)
	}
}
