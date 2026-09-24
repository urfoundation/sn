//go:build linux || darwin

// Diagnostic failures are evidence, never permission to mutate a running run.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestTerminalDiagnosticsContinueAfterFailureAndPreserveAbsence(t *testing.T) {
	report := &terminalDiagnosticReport{Schema: terminalDiagnosticSchema, ReadOnly: true, FinalAcceptance: false}
	collector := &terminalDiagnosticCollector{ctx: t.Context(), report: report, output: t.TempDir()}
	calls := []string{}
	for _, item := range []struct {
		id  string
		err error
	}{
		{id: "malformed", err: errors.New("invalid signed record")},
		{id: "absent", err: terminalDiagnosticFinding("no local intent store")},
		{id: "missing", err: os.ErrNotExist},
		{id: "valid"},
	} {
		collector.check(item.id, "", time.Minute, func(context.Context) (any, error) { calls = append(calls, item.id); return item.id, item.err })
	}
	collector.check("dependent", "authenticated source unavailable", time.Minute, func(context.Context) (any, error) { t.Fatal("missing prerequisite was ignored"); return nil, nil })
	if !reflect.DeepEqual(calls, []string{"malformed", "absent", "missing", "valid"}) {
		t.Fatal(calls)
	}
	statuses := []string{}
	for _, check := range report.Checks {
		statuses = append(statuses, check.Status)
	}
	if !reflect.DeepEqual(statuses, []string{"fail", "finding", "unavailable", "pass", "unavailable"}) || report.FinalAcceptance || collector.persistErr != nil {
		t.Fatalf("diagnostic outcomes=%v error=%v", statuses, collector.persistErr)
	}
	var persisted terminalDiagnosticReport
	if err := readJSONFile(filepath.Join(collector.output, "progress.json"), &persisted); err != nil || persisted.FinalAcceptance || len(persisted.Checks) != 5 {
		t.Fatalf("durable nonaccepting progress: %+v %v", persisted, err)
	}
}

func TestTerminalDiagnosticsCancellationInventoriesRemainingChecks(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	report := &terminalDiagnosticReport{Schema: terminalDiagnosticSchema, ReadOnly: true}
	collector := &terminalDiagnosticCollector{ctx: ctx, report: report}
	for _, id := range []string{"native-receipts", "payouts", "path-proofs"} {
		collector.check(id, "", time.Minute, func(context.Context) (any, error) { t.Fatal("canceled check executed"); return nil, nil })
	}
	if len(report.Checks) != 3 {
		t.Fatal("cancellation hid independent checks")
	}
	for _, check := range report.Checks {
		if check.Status != "unavailable" || !strings.Contains(check.Detail, "canceled") {
			t.Fatal(check)
		}
	}
}

func TestTerminalDiagnosticsReaderPanicDoesNotHideIndependentCheck(t *testing.T) {
	collector := &terminalDiagnosticCollector{ctx: t.Context(), report: &terminalDiagnosticReport{ReadOnly: true}}
	collector.check("legacy-reader", "", time.Minute, func(context.Context) (any, error) { panic("synthetic reader failure") })
	collector.check("independent-receipt", "", time.Minute, func(context.Context) (any, error) { return "authenticated synthetic receipt", nil })
	if len(collector.report.Checks) != 2 || collector.report.Checks[0].Status != "fail" || !strings.Contains(collector.report.Checks[0].Detail, "panicked") || collector.report.Checks[1].Status != "pass" || collector.report.FinalAcceptance {
		t.Fatal(collector.report.Checks)
	}
}

func TestTerminalDiagnosticsOutputCannotAliasDeployment(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(root, "state")
	if err := os.Mkdir(state, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := validateTerminalDiagnosticOutput(state, filepath.Join(root, "diagnostics")); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(state, alias); err != nil {
		t.Fatal(err)
	}
	for _, output := range []string{state, filepath.Join(state, "diagnostics"), root, filepath.Join(alias, "diagnostics"), "relative"} {
		if err := validateTerminalDiagnosticOutput(state, output); err == nil {
			t.Errorf("accepted state alias %s", output)
		}
	}
}

func TestTerminalDiagnosticsKeepOriginalBytesOutsideState(t *testing.T) {
	state, output := t.TempDir(), t.TempDir()
	path := filepath.Join(state, "signed-evidence.json")
	raw := []byte("{\n  \"synthetic_signed_payload\": 3\n}\n")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	collector := &terminalDiagnosticCollector{ctx: t.Context(), output: output, report: &terminalDiagnosticReport{ReadOnly: true}}
	if err := collector.retain(path, raw); err != nil {
		t.Fatal(err)
	}
	if len(collector.report.Originals) != 1 {
		t.Fatal("missing original pin")
	}
	ref := collector.report.Originals[0]
	copy, err := os.ReadFile(filepath.Join(output, ref.Copy.URI))
	if err != nil || !bytes.Equal(copy, raw) || ref.Sha256 != bytesSHA256(raw) || ref.Bytes != uint64(len(raw)) {
		t.Fatalf("original bytes changed: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, raw) {
		t.Fatal("source modified", err)
	}
	entries, err := os.ReadDir(state)
	if err != nil || len(entries) != 1 {
		t.Fatal("diagnostic wrote into deployment", err)
	}
}

func TestTerminalDiagnosticsLateFaultRetainsActualDuration(t *testing.T) {
	started := time.Unix(1, 0).UTC()
	terminal := &ScenarioObservation{ObservationHash: "synthetic-observation"}
	faults := []ScenarioFaultRecord{{ID: "synthetic-database-pause", Kind: "container-pause", Status: "restored", TriggerBlock: 100, RestoreBlock: 120, AppliedBlock: 121, RestoredBlock: 141}}
	value, err := terminalDiagnosticFaultTiming(faults, started, terminal)
	if err != nil {
		t.Fatal("delayed but fully held fault was called invalid", err)
	}
	wire, err := json.Marshal(value)
	if err != nil || !bytes.Contains(wire, []byte("after scheduled restore")) {
		t.Fatal("nominal delay disappeared", err)
	}
	faults[0].RestoredBlock = 140
	if _, err := terminalDiagnosticFaultTiming(faults, started, terminal); err == nil {
		t.Fatal("short actual fault duration was accepted")
	}
}

func TestTerminalDiagnosticsRescanDoesNotWriteOriginalGateOrLogs(t *testing.T) {
	fixture := newProcessLogGateFixture(t, "original log prefix\n", "")
	_, boundaryHash, err := fixture.gate.BindAcceptance(time.Unix(200, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	runDir := filepath.Join(fixture.dir, "runs", "synthetic-run")
	if err := fixture.gate.WriteEvidence(runDir); err != nil {
		t.Fatal(err)
	}
	reportPath := filepath.Join(runDir, "process-logs.json")
	before, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	gateBefore, err := os.ReadFile(filepath.Join(fixture.dir, processLogGateStateFilename))
	if err != nil {
		t.Fatal(err)
	}
	appendProcessLog(t, fixture.stdoutPath, "panic: synthetic terminal failure\n")
	logBefore, err := os.ReadFile(fixture.stdoutPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg := testResolvedConfig(t)
	cfg.Config.Deployment.DeploymentID = fixture.manifest.DeploymentID
	collector := &terminalDiagnosticCollector{ctx: t.Context(), output: t.TempDir(), report: &terminalDiagnosticReport{ReadOnly: true}}
	findings, err := collectTerminalDiagnosticProcessLogs(t.Context(), collector, cfg, fixture.dir, runDir, &scenarioCampaignAcceptanceBoundary{ProcessLogBoundaryHash: boundaryHash})
	if err == nil || !strings.Contains(err.Error(), "panic") || findings == nil {
		t.Fatal("strict terminal failure was hidden", err)
	}
	for _, item := range []struct {
		path   string
		before []byte
	}{{path: reportPath, before: before}, {path: filepath.Join(fixture.dir, processLogGateStateFilename), before: gateBefore}, {path: fixture.stdoutPath, before: logBefore}} {
		after, err := os.ReadFile(item.path)
		if err != nil || !bytes.Equal(item.before, after) {
			t.Fatal("diagnostic mutated retained source", item.path, err)
		}
	}
	if _, err := os.Stat(filepath.Join(collector.output, "process-log-scan.json")); err != nil {
		t.Fatal("missing separate scan", err)
	}
}

func TestTerminalDiagnosticsCliRequiresReadOnlyExactOwner(t *testing.T) {
	// This private-route parser requires a generated private address; no socket
	// is opened, and no production host or authority is part of the fixture.
	authority := net.JoinHostPort(net.IPv4(10, 255, 254, 253).String(), "1234")
	args := []string{"terminal-diagnostics", "--name", "release-1.0", "--run-id", "synthetic-run", "--plan-hash", "0x" + strings.Repeat("63", 32), "--provisional-resume", "--diagnostic-output", "/synthetic/diagnostics", "--owned-rpc-authority", authority}
	command, options, err := parseCLI(args)
	if err != nil || command != "terminal-diagnostics" || executableAttestationModeForCommand(command, options) != executableAttestationProvisionalResume {
		t.Fatal(command, err)
	}
	for _, flag := range []string{"--apply", "--detach", "--prepare-only", "--then-release-candidate", "--provisional-capture"} {
		if _, _, err := parseCLI(append(append([]string(nil), args...), flag)); err == nil {
			t.Errorf("mutation flag %s admitted", flag)
		}
	}
	options.OwnedRPCAuthority = ""
	if err := validateTerminalDiagnosticOptions(command, options); err == nil {
		t.Fatal("unowned network route admitted")
	}
	if _, _, err := parseCLI([]string{"status", "--wait-for-terminal"}); err == nil {
		t.Fatal("diagnostic option escaped its command")
	}
}

func TestTerminalDiagnosticsIncompleteRunReportsEveryClassWithoutStateWrites(t *testing.T) {
	g, _, _ := policyRolloverObservationFixtureV2(t)
	f := g.fixture
	if err := os.MkdirAll(filepath.Join(f.stateDir, "secrets"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writePublicJSON(filepath.Join(f.stateDir, "secrets", "roles.json"), f.roles); err != nil {
		t.Fatal(err)
	}
	f.cfg.provisionalResume.Driver = provisionalDriverProvenance{ExecutableSHA256: "sha256:" + strings.Repeat("35", 32), ExecutablePath: "/synthetic/diagnostic", Build: releaseExecutableBuildIdentity{Revision: strings.Repeat("31", 20)}}
	output := filepath.Join(t.TempDir(), "diagnostics")
	options := cliOptions{ProvisionalResume: true, PlanHash: f.plan.PlanHash, RunID: "synthetic-run", Name: "release-1.0", DiagnosticOutput: output, OwnedRPCAuthority: net.JoinHostPort(net.IPv4(10, 255, 254, 253).String(), "1234")}
	snapshot := func() map[string]string {
		t.Helper()
		files := map[string]string{}
		err := filepath.WalkDir(f.stateDir, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			files[path] = bytesSHA256(raw)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return files
	}
	before := snapshot()
	report, err := runTerminalDiagnostics(t.Context(), f.cfg, f.stateDir, options)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, snapshot()) {
		t.Fatal("diagnostic created or rewrote deployment files")
	}
	if report.FinalAcceptance || !report.ReadOnly || report.Status != "complete-with-findings" || !validCanonicalHashHex(report.EvidenceHash) {
		t.Fatalf("invalid diagnostic disposition: %+v", report)
	}
	checks := map[string]string{}
	for _, check := range report.Checks {
		checks[check.Id] = check.Status
	}
	for _, id := range []string{"signed-acceptance-start", "original-scenario-result", "operator-1/current-signed-artifacts", "operator-2/current-signed-artifacts", "validator-1/exact-production-config", "validator-2/exact-production-config", "original-process-log-report", "acceptance-fault-timing", "signed-payout-artifacts", "adversarial-campaign", "canonical-contract-receipts-and-native-rewards", "original-strict-semantic-source"} {
		if checks[id] == "" {
			t.Errorf("missing independent check %s", id)
		}
	}
	if checks["active-generation-path-authority"] != "pass" || checks["validator-2/exact-production-config"] != "pass" || checks["strict-final-acceptance-gate"] != "unavailable" {
		t.Fatal(checks)
	}
	if _, err := os.Stat(filepath.Join(output, "report.json")); err != nil {
		t.Fatal(err)
	}
}

func TestTerminalDiagnosticsRetryTypedOperatorTimeoutWithoutCachingSuccess(t *testing.T) {
	calls, waits := 0, 0
	var attempts []evidenceRelayRetryObservation
	surfaces, err := readTerminalDiagnosticOperatorSurfaces(t.Context(), func(context.Context) scenarioOperatorSurfaces {
		calls++
		var result scenarioOperatorSurfaces
		if calls == 1 {
			result[scenarioOperatorStats].err = context.DeadlineExceeded
		} else {
			result[scenarioOperatorStats].data = []byte("fresh synthetic stats")
		}
		return result
	}, func(context.Context, time.Duration) error { waits++; return nil }, func(value evidenceRelayRetryObservation) error { attempts = append(attempts, value); return nil })
	if err != nil || calls != 2 || waits != 1 || string(surfaces[scenarioOperatorStats].data) != "fresh synthetic stats" || len(attempts) != 2 || attempts[0].Outcome != "retrying" || attempts[1].Outcome != "recovered" {
		t.Fatalf("calls=%d waits=%d attempts=%+v error=%v", calls, waits, attempts, err)
	}
	calls, waits = 0, 0
	_, err = readTerminalDiagnosticOperatorSurfaces(t.Context(), func(context.Context) scenarioOperatorSurfaces {
		calls++
		var result scenarioOperatorSurfaces
		result[scenarioOperatorStats].err = context.DeadlineExceeded
		result[scenarioOperatorProofs].err = errors.New("malformed signed proof")
		return result
	}, func(context.Context, time.Duration) error { waits++; return nil }, func(evidenceRelayRetryObservation) error { return nil })
	if err == nil || calls != 1 || waits != 0 {
		t.Fatalf("mixed structural failure retried: calls=%d waits=%d error=%v", calls, waits, err)
	}
}

func TestTerminalDiagnosticsResultWaitNeverCreatesRunEvidence(t *testing.T) {
	state := t.TempDir()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := waitTerminalDiagnosticResult(ctx, state, "synthetic-run")
	var finding terminalDiagnosticFinding
	if !errors.As(err, &finding) {
		t.Fatalf("canceled missing result was not an explicit finding: %v", err)
	}
	entries, err := os.ReadDir(state)
	if err != nil || len(entries) != 0 {
		t.Fatal("result wait created deployment state", err)
	}
	path := filepath.Join(state, "runs", "synthetic-run", "result.json")
	if err := writePublicJSON(path, &ScenarioResult{RunID: "synthetic-run", CompletedAt: time.Unix(3, 0).UTC().Format(time.RFC3339Nano), Result: "fail"}); err != nil {
		t.Fatal(err)
	}
	if err := waitTerminalDiagnosticResult(t.Context(), state, "synthetic-run"); err != nil {
		t.Fatal("failed original result was not retained for diagnostics", err)
	}
	if err := writePublicJSON(path, &ScenarioResult{RunID: "foreign-run", CompletedAt: time.Unix(3, 0).UTC().Format(time.RFC3339Nano), Result: "pass"}); err != nil {
		t.Fatal(err)
	}
	if err := waitTerminalDiagnosticResult(t.Context(), state, "synthetic-run"); err == nil {
		t.Fatal("different run satisfied result wait")
	}
}
