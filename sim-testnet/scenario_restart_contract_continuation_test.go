// Late traffic may retain a contract from the previous receiver generation.
// Provisional observation must reach terminal without accepting that contract
// or changing its independently classified final finding.
package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A completed restart must include a different synthetic process generation;
// the ordinary fake driver intentionally models pause/continue with one PID.
type scenarioIntervalContractRestartDriver struct{ fakeFaultDriver }

func (self *scenarioIntervalContractRestartDriver) Restore(ctx context.Context, spec scenarioFaultSpec) ([]FaultProcessEvidence, error) {
	processes, err := self.fakeFaultDriver.Restore(ctx, spec)
	for index := range processes {
		processes[index].PID++
	}
	return processes, err
}

// The explicit observation index follows completed fault restoration, forcing
// the live failure ordering without a grace timer or an invented attribution.
func TestScenarioIntervalLateRestartContractReachesTerminalAndFailsFinalGate(t *testing.T) {
	t.Parallel()
	cfg, definition, window, observations := newScenarioIntervalFixture(t)
	fixture := newProcessLogGateFixture(t, "", "")
	definition.Faults = []scenarioFaultSpec{{ID: "synthetic-restart", Kind: "process-restart", Targets: []string{"miner-1"}, TriggerOffsetBlocks: 1, DurationBlocks: 1}}
	definition.Checks = []scenarioCheck{{ID: "terminal-only", Check: func(evaluation *scenarioEvaluation) (bool, string) {
		return scenarioAcceptanceIntervalObserved(evaluation.Window, evaluation.Current), "terminal checkpoint observed"
	}}}
	driver := &scenarioIntervalContractRestartDriver{}
	probe := &scenarioIntervalProbe{observations: observations}
	probe.before = func(_ context.Context, index int) error {
		if index != 4 {
			return nil
		}
		if len(driver.restored) != 1 {
			return errors.New("late-contract fixture did not complete restart before the log arrived")
		}
		return os.WriteFile(fixture.stderrPath, []byte(
			"E0801 12:00:00.000000 123 transfer.go:1] [r]receiver<-sender s(00000000-0000-0000-0000-000000000000) exit contract verification failed (Network)\n"+
				"E0801 12:00:00.000000 123 transfer.go:1] [r]receiver<-sender s(00000000-0000-0000-0000-000000000000) ack could not register contracts = Contract verification failed.\n"+
				"E0801 12:00:00.000000 123 transfer.go:1] [r]receiver<-sender s(00000000-0000-0000-0000-000000000000) exit could not receive ack = Contract verification failed.\n"+
				"E0801 12:00:00.000000 123 transfer.go:1] exit gap timeout\n"), 0o600)
	}
	result, err := runScenarioWithProbe(t.Context(), cfg, fixture.dir, definition, probe, scenarioRunOptions{PollInterval: time.Microsecond, Timeout: time.Hour, ProcessLogs: fixture.gate, FaultDriver: driver})
	if err == nil || result == nil || result.EndHead.Number != window.TerminalBlock || probe.calls.Load() != int64(len(observations)) || !faultsComplete(result.Faults) {
		t.Fatalf("late rejected contract interrupted diagnostic coverage: result=%+v reads=%d error=%v", result, probe.calls.Load(), err)
	}
	for _, assertion := range result.Assertions {
		if assertion.ID == "fault_synthetic-restart" && !assertion.Passed || assertion.ID == "scenario_context" {
			t.Fatalf("fixture failed outside the final process gate: %+v", assertion)
		}
	}
	scan, scanErr := fixture.gate.Scan(true)
	if scanErr != nil {
		t.Fatal(scanErr)
	}
	found := false
	for _, finding := range scan.Findings {
		if finding.Class != "restart-stale-contract" {
			continue
		}
		found = true
		if !finding.Blocking || finding.Disposition != "unexplained" || finding.Count != 3 || len(finding.FaultIDs) != 0 || finding.FirstLineSHA256 == "" {
			t.Fatalf("late finding was waived or attributed retroactively: %+v", finding)
		}
	}
	if !found || fixture.gate.RequireClean(true) == nil || result.Result != "fail" {
		t.Fatal("diagnostic continuation weakened final rejection")
	}
	if _, err := os.Stat(filepath.Join(fixture.dir, "runs", result.RunID, "complete.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("late unresolved contract finding produced completion: %v", err)
	}
}

// The class is deliberately narrower than all verification errors. Public
// signatures, foreign streams, unknown errors and joined persistence failures
// cannot borrow the diagnostic retry permission of a primary network contract.
func TestScenarioIntervalRestartContractDeferralKeepsIntegrityBoundary(t *testing.T) {
	t.Parallel()
	cfg, _, _, _ := newScenarioIntervalFixture(t)
	primary := "E0801 12:00:00.000000 123 transfer.go:1] [r]receiver<-sender s(00000000-0000-0000-0000-000000000000) exit contract verification failed (Network)"
	classified := func(line string) error {
		classification, ok := classifyProcessLogLine([]byte(line))
		if !ok {
			t.Fatalf("verification line lost classification: %s", line)
		}
		return processLogFindingsError([]ProcessLogFinding{{ProcessID: "synthetic-miner", Blocking: true, Class: classification.class.definition().name, Count: 1}})
	}
	known := classified(primary)
	if !scenarioProcessLogFailureDeferred(cfg, "release-1.0", known) || !scenarioProcessLogFailureDeferred(cfg, "production-soak", known) {
		t.Fatal("known primary rejection stopped an authorized diagnostic interval")
	}
	for _, failure := range []error{
		classified(strings.Replace(primary, "(Network)", "(Public)", 1)),
		classified(strings.Replace(primary, "000000000000)", "000000000001)", 1)),
		classified("E0801 12:00:00.000000 123 transfer.go:1] unknown contract verification failure"),
		errors.New(known.Error()),
		errors.Join(known, errors.New("synthetic evidence hash mismatch")),
		errors.Join(known, processLogFindingsError([]ProcessLogFinding{{Blocking: true, Class: "panic", Count: 1}})),
	} {
		if scenarioProcessLogFailureDeferred(cfg, "release-1.0", failure) {
			t.Fatalf("foreign failure acquired contract deferral: %v", failure)
		}
	}
	cfg.provisionalResume.Record.FinalAcceptance = true
	if scenarioProcessLogFailureDeferred(cfg, "release-1.0", known) {
		t.Fatal("strict acceptance deferred an unresolved contract finding")
	}
}

// A late line cannot inherit an already-ended expected fault scope. Deferral
// affects only controller liveness, leaving both old and new scan facts intact.
func TestScenarioIntervalRestartContractAfterRestoreRetainsUnexplainedFinding(t *testing.T) {
	t.Parallel()
	cfg, _, _, _ := newScenarioIntervalFixture(t)
	fixture := newProcessLogGateFixture(t, "", "")
	line := "E0801 12:00:00.000000 123 transfer.go:1] [r]receiver<-sender s(00000000-0000-0000-0000-000000000000) exit contract verification failed (Network)\n"
	records := []ScenarioFaultRecord{{ID: "synthetic-restart", Kind: "process-restart", Targets: []string{"miner-1"}, Status: "active", AppliedBlock: 100}}
	appendProcessLog(t, fixture.stderrPath, line)
	first, err := fixture.gate.Scan(false, activeProcessLogFaultScopes(records)...)
	if err != nil || len(first.Findings) != 1 || first.Findings[0].Blocking || first.Findings[0].Disposition != "expected-fault" {
		t.Fatalf("active exact fault was not attributed: %+v error=%v", first, err)
	}
	records[0].Status, records[0].RestoredBlock = "restored", 120
	appendProcessLog(t, fixture.stderrPath, line)
	second, err := fixture.gate.Scan(false, activeProcessLogFaultScopes(records)...)
	if err != nil || len(second.Findings) != 2 {
		t.Fatalf("late line merged with an ended fault: %+v error=%v", second, err)
	}
	blocking := blockingProcessLogFindings(second.Findings)
	if len(blocking) != 1 || blocking[0].Disposition != "unexplained" || len(blocking[0].FaultIDs) != 0 || blocking[0].Count != 1 || !scenarioProcessLogFailureDeferred(cfg, "release-1.0", processLogFindingsError(blocking)) || fixture.gate.RequireClean(true) == nil {
		t.Fatalf("post-restore finding lost independent final severity: %+v", blocking)
	}
}
