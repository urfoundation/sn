package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestProcessLogClassifierFailsClosedWithoutRejectingExpectedNoise(t *testing.T) {
	tests := []struct {
		name                  string
		line                  string
		wantClass             string
		wantDisposition       string
		wantFaultAttributable bool
	}{
		{name: "panic", line: "[reconcile]stripe: panic: recovered provider panic", wantClass: "panic"},
		{name: "structured-panic", line: `{"level":"panic","message":"process aborted"}`, wantClass: "panic"},
		{name: "fatal-runtime", line: "fatal error: concurrent map writes", wantClass: "fatal"},
		{name: "structured-fatal", line: `time=now level=fatal msg="peer connection memory budget exhausted"`, wantClass: "fatal"},
		{name: "release-steering-attempt", line: "release steer: subnet epoch 1511 attempt 1: V2 intent 2: compact measurement envelope artifact", wantClass: "release-steering-attempt-failure"},
		{name: "release-steering-continuity", line: "sim-testnet: release steering advanced from incomplete epoch 1511 to 1512", wantClass: "release-steering-continuity"},
		{name: "early-commit-alert", line: "E0924 08:48:16.375559 1387508 st_controller.go:3327] [st]epoch 597 commit DEADLINE ALERT: unconfirmed with ~7m48s left (deadline block 8074824, head 8074785)", wantClass: "commit-deadline-warning", wantDisposition: "pending-commit-retry"},
		{name: "imminent-commit-alert-remains-blocking", line: "E0924 08:48:16.375559 1387508 st_controller.go:3327] [st]epoch 597 commit DEADLINE ALERT: unconfirmed with ~48s left (deadline block 8074824, head 8074820)", wantClass: "error"},
		{name: "passed-commit-deadline-remains-blocking", line: "E0924 08:48:16.375559 1387508 st_controller.go:3327] [st]epoch 597 commit DEADLINE ALERT: unconfirmed with ~7m48s left (deadline block 8074824, head 8074825)", wantClass: "error"},
		{name: "quic-buffer", line: "failed to sufficiently increase receive buffer size (was: 208 kiB, wanted: 7168 kiB, got: 416 kiB)", wantClass: "quic-receive-buffer"},
		{name: "tls-timeout", line: "E0902 18:07:08 completeHandshake failed: tls handshake timeout", wantClass: "tls-handshake-timeout", wantFaultAttributable: true},
		{name: "h3-internal-error", line: "E0902 18:07:08 h3 connect err = CRYPTO_ERROR 0x150 (remote): tls: internal error", wantClass: "h3-tls-internal"},
		{name: "h3-internal-fallback", line: "I0902 18:07:08 h3 connect err = CRYPTO_ERROR 0x150 (remote): tls: internal error", wantClass: "h3-tls-internal", wantDisposition: "resilient-fallback"},
		{name: "packet-timeout-fallback", line: "I0902 18:07:08 [pt]read packet timeout", wantClass: "packet-read-timeout", wantDisposition: "resilient-fallback", wantFaultAttributable: true},
		{name: "packet-timeout-error", line: "E0902 18:07:08 [pt]read packet timeout", wantClass: "packet-read-timeout", wantFaultAttributable: true},
		{name: "close-timeout", line: "I0902 18:07:08 close timeout", wantClass: "connection-close-timeout", wantFaultAttributable: true},
		{name: "exit-gap-timeout", line: "E0902 18:07:08 exit gap timeout", wantClass: "exit-gap-timeout", wantFaultAttributable: true},
		{name: "contract-create", line: "I0902 18:07:08 exit could not create contract", wantClass: "contract-create", wantFaultAttributable: true},
		{name: "restart-network-contract-verification", line: "E0902 18:07:08.000000 123 transfer.go:1] [r]receiver<-sender s(00000000-0000-0000-0000-000000000000) exit contract verification failed (Network)", wantClass: "restart-stale-contract", wantFaultAttributable: true},
		{name: "restart-public-contract-verification", line: "E0902 18:07:08.000000 123 transfer.go:1] [r]receiver<-sender s(00000000-0000-0000-0000-000000000000) exit contract verification failed (Public)", wantClass: "error"},
		{name: "api-token-refresh-timeout", line: "E0917 15:03:00 [api-token]failed to refresh JWT: Timeout.", wantClass: "api-token-refresh-timeout", wantFaultAttributable: true},
		{name: "seed-unavailable-first-worker-fault-signature", line: "I0902 18:07:08 [trail 0] trail error (kind 0, hop 00000000-0000-0000-0000-000000000000): seed pick: no seed providers available", wantClass: "seed-unavailable", wantFaultAttributable: true},
		{name: "seed-unavailable-fourth-worker-fault-signature", line: "I0902 18:07:08 [trail 3] trail error (kind 0, hop 00000000-0000-0000-0000-000000000000): seed pick: no seed providers available", wantClass: "seed-unavailable", wantFaultAttributable: true},
		{name: "seed-unavailable-generic", line: "I0902 18:07:08 no seed providers", wantClass: "seed-unavailable"},
		{name: "seed-unavailable-worker-out-of-range", line: "I0902 18:07:08 [trail 128] trail error (kind 0, hop 00000000-0000-0000-0000-000000000000): seed pick: no seed providers available", wantClass: "seed-unavailable"},
		{name: "seed-unavailable-worker-noncanonical", line: "I0902 18:07:08 [trail 03] trail error (kind 0, hop 00000000-0000-0000-0000-000000000000): seed pick: no seed providers available", wantClass: "seed-unavailable"},
		{name: "seed-unavailable-wrong-kind", line: "I0902 18:07:08 [trail 3] trail error (kind 1, hop 00000000-0000-0000-0000-000000000000): seed pick: no seed providers available", wantClass: "seed-unavailable"},
		{name: "seed-unavailable-wrong-hop", line: "I0902 18:07:08 [trail 3] trail error (kind 0, hop 00000000-0000-0000-0000-000000000001): seed pick: no seed providers available", wantClass: "seed-unavailable"},
		{name: "postgres-nul", line: `ERROR: invalid byte sequence for encoding "UTF8": 0x00`, wantClass: "postgres-null-byte"},
		{name: "unknown-klog-error", line: "E0902 18:07:08 an unclassified failure", wantClass: "error"},
		{name: "unknown-klog-warning", line: "W0902 18:07:08 an unclassified warning", wantClass: "warning"},
		{name: "unknown-klog-fatal", line: "F0902 18:07:08 an unclassified fatal", wantClass: "fatal"},
		{name: "structured-error", line: `{"level":"error","message":"unknown"}`, wantClass: "error"},
		{name: "structured-warning", line: "time=now level=warn msg=unknown", wantClass: "warning"},
		{name: "panic-precedes-budget-allow", line: "W0902 peer connection memory budget exhausted panic: impossible invariant", wantClass: "panic"},
		{name: "expected-http-400", line: "I0902 router_stats.go POST /verify status=400 request rejected", wantClass: ""},
		{name: "expected-http-429", line: "I0902 router_stats.go POST /verify status=429 request rejected", wantClass: ""},
		{name: "bounded-budget", line: "W0902 18:07:08 peer connection memory budget exhausted", wantClass: ""},
		{name: "bounded-priority", line: "W0902 18:07:08 setup admission refused reason=priority", wantClass: ""},
		{name: "bounded-admission", line: "W0902 18:07:08 setup admission refused reason=budget", wantClass: ""},
		{name: "shutdown-cancel", line: "E0902 18:07:08 completeHandshake failed: context canceled", wantClass: "connection-canceled", wantFaultAttributable: true},
		{name: "release-steering-info-near-miss", line: "release steer: provisional native epoch 1511 retryable cut continued in native epoch 1512; no process restart", wantClass: ""},
		{name: "release-steering-malformed-near-miss", line: "sim-testnet: release steering advanced from incomplete epoch unknown to 1512", wantClass: ""},
		{name: "ordinary-info", line: "I0902 18:07:08 service healthy", wantClass: ""},
	}
	for _, test := range tests {
		classification, matched := classifyProcessLogLine([]byte(test.line))
		if test.wantClass == "" {
			if matched {
				t.Fatalf("%s: safe line classified as %+v", test.name, classification)
			}
			continue
		}
		if !matched || classification.class != test.wantClass {
			t.Fatalf("%s: classification=(%+v,%t), want %q", test.name, classification, matched, test.wantClass)
		}
		if classification.nonblockingDisposition != test.wantDisposition || classification.faultAttributable != test.wantFaultAttributable {
			t.Fatalf("%s: classification=%+v, want disposition=%q fault_attributable=%t", test.name, classification, test.wantDisposition, test.wantFaultAttributable)
		}
	}
}

func TestProcessLogGateAttributesOnlyNarrowSignalsToExactFaultTargets(t *testing.T) {
	exactFault := []processLogFaultScope{{ID: "fault-1", Kind: "process-pause", Targets: []string{"miner-1"}}}
	wrongFault := []processLogFaultScope{{ID: "fault-1", Kind: "process-pause", Targets: []string{"validator-1"}}}
	restartFault := []processLogFaultScope{{ID: "restart-1", Kind: "process-restart", Targets: []string{"miner-1"}}}
	wrongRestartFault := []processLogFaultScope{{ID: "restart-1", Kind: "process-restart", Targets: []string{"validator-1"}}}
	tests := []struct {
		name            string
		line            string
		faults          []processLogFaultScope
		wantClass       string
		wantDisposition string
		wantBlocking    bool
		wantFaultID     string
		wantFaultKind   string
	}{
		{name: "exact-target-connection-loss", line: "E0902 18:07:08 dial failed: connection refused\n", faults: exactFault, wantClass: "error", wantDisposition: "expected-fault", wantFaultID: "fault-1"},
		{name: "wrong-target-connection-loss", line: "E0902 18:07:08 dial failed: connection refused\n", faults: wrongFault, wantClass: "error", wantDisposition: "unexplained", wantBlocking: true},
		{name: "panic-never-attributed", line: "panic: invariant violated during restart\n", faults: exactFault, wantClass: "panic", wantDisposition: "unexplained", wantBlocking: true},
		{name: "info-h3-fallback", line: "I0902 18:07:08 h3 connect err: tls: internal error\n", faults: exactFault, wantClass: "h3-tls-internal", wantDisposition: "resilient-fallback"},
		{name: "error-h3-fails-closed", line: "E0902 18:07:08 h3 connect err: tls: internal error\n", faults: exactFault, wantClass: "h3-tls-internal", wantDisposition: "unexplained", wantBlocking: true},
		{name: "info-packet-fallback", line: "I0902 18:07:08 [pt]read packet timeout\n", wantClass: "packet-read-timeout", wantDisposition: "resilient-fallback"},
		{name: "unproven-shutdown", line: "E0902 18:07:08 completeHandshake failed: context canceled\n", wantClass: "connection-canceled", wantDisposition: "unexplained", wantBlocking: true},
		{name: "proven-shutdown", line: "E0902 18:07:08 completeHandshake failed: context canceled\n", faults: exactFault, wantClass: "connection-canceled", wantDisposition: "expected-fault", wantFaultID: "fault-1"},
		{name: "contract-create-exact-target", line: "I0902 18:07:08 exit could not create contract.\n", faults: exactFault, wantClass: "contract-create", wantDisposition: "expected-fault", wantFaultID: "fault-1"},
		{name: "contract-create-wrong-target", line: "I0902 18:07:08 exit could not create contract.\n", faults: wrongFault, wantClass: "contract-create", wantDisposition: "unexplained", wantBlocking: true},
		{name: "restart-stale-contract-primary-exact-target", line: "E0902 18:07:08.000000 123 transfer.go:1] [r]receiver<-sender s(00000000-0000-0000-0000-000000000000) exit contract verification failed (Network)\n", faults: restartFault, wantClass: "restart-stale-contract", wantDisposition: "expected-fault", wantFaultID: "restart-1", wantFaultKind: "process-restart"},
		{name: "restart-stale-contract-register-exact-target", line: "E0902 18:07:08.000000 123 transfer.go:1] [r]receiver<-sender s(00000000-0000-0000-0000-000000000000) ack could not register contracts = Contract verification failed.\n", faults: restartFault, wantClass: "restart-stale-contract", wantDisposition: "expected-fault", wantFaultID: "restart-1", wantFaultKind: "process-restart"},
		{name: "restart-stale-contract-receive-exact-target", line: "E0902 18:07:08.000000 123 transfer.go:1] [r]receiver<-sender s(00000000-0000-0000-0000-000000000000) exit could not receive ack = Contract verification failed.\n", faults: restartFault, wantClass: "restart-stale-contract", wantDisposition: "expected-fault", wantFaultID: "restart-1", wantFaultKind: "process-restart"},
		{name: "restart-stale-contract-wrong-target", line: "E0902 18:07:08.000000 123 transfer.go:1] [r]receiver<-sender s(00000000-0000-0000-0000-000000000000) exit contract verification failed (Network)\n", faults: wrongRestartFault, wantClass: "restart-stale-contract", wantDisposition: "unexplained", wantBlocking: true},
		{name: "restart-stale-contract-wrong-kind", line: "E0902 18:07:08.000000 123 transfer.go:1] [r]receiver<-sender s(00000000-0000-0000-0000-000000000000) exit contract verification failed (Network)\n", faults: exactFault, wantClass: "restart-stale-contract", wantDisposition: "unexplained", wantBlocking: true},
		{name: "restart-stale-contract-without-fault", line: "E0902 18:07:08.000000 123 transfer.go:1] [r]receiver<-sender s(00000000-0000-0000-0000-000000000000) exit contract verification failed (Network)\n", wantClass: "restart-stale-contract", wantDisposition: "unexplained", wantBlocking: true},
		{name: "restart-stale-contract-public-remains-blocking", line: "E0902 18:07:08.000000 123 transfer.go:1] [r]receiver<-sender s(00000000-0000-0000-0000-000000000000) exit contract verification failed (Public)\n", faults: restartFault, wantClass: "error", wantDisposition: "unexplained", wantBlocking: true},
		{name: "restart-stale-contract-wrong-stream-remains-blocking", line: "E0902 18:07:08.000000 123 transfer.go:1] [r]receiver<-sender s(00000000-0000-0000-0000-000000000001) exit contract verification failed (Network)\n", faults: restartFault, wantClass: "error", wantDisposition: "unexplained", wantBlocking: true},
		{name: "exit-gap-exact-target", line: "E0902 18:07:08 exit gap timeout\n", faults: exactFault, wantClass: "exit-gap-timeout", wantDisposition: "expected-fault", wantFaultID: "fault-1"},
		{name: "exit-gap-wrong-target", line: "E0902 18:07:08 exit gap timeout\n", faults: wrongFault, wantClass: "exit-gap-timeout", wantDisposition: "unexplained", wantBlocking: true},
		{name: "api-token-exact-target", line: "E0917 15:03:00 [api-token]failed to refresh JWT: Timeout.\n", faults: exactFault, wantClass: "api-token-refresh-timeout", wantDisposition: "expected-fault", wantFaultID: "fault-1"},
		{name: "api-token-wrong-target", line: "E0917 15:03:00 [api-token]failed to refresh JWT: Timeout.\n", faults: wrongFault, wantClass: "api-token-refresh-timeout", wantDisposition: "unexplained", wantBlocking: true},
		{name: "seed-unavailable-exact-target", line: "E0917 15:24:00 [trail 3] trail error (kind 0, hop 00000000-0000-0000-0000-000000000000): seed pick: no seed providers available\n", faults: exactFault, wantClass: "seed-unavailable", wantDisposition: "expected-fault", wantFaultID: "fault-1"},
		{name: "seed-unavailable-wrong-target", line: "E0917 15:24:00 [trail 3] trail error (kind 0, hop 00000000-0000-0000-0000-000000000000): seed pick: no seed providers available\n", faults: wrongFault, wantClass: "seed-unavailable", wantDisposition: "unexplained", wantBlocking: true},
		{name: "seed-unavailable-without-fault", line: "E0917 15:24:00 [trail 0] trail error (kind 0, hop 00000000-0000-0000-0000-000000000000): seed pick: no seed providers available\n", wantClass: "seed-unavailable", wantDisposition: "unexplained", wantBlocking: true},
		{name: "generic-seed-unavailable-remains-blocking", line: "E0917 15:24:00 seed pick: no seed providers available\n", faults: exactFault, wantClass: "seed-unavailable", wantDisposition: "unexplained", wantBlocking: true},
		{name: "wrong-hop-seed-unavailable-remains-blocking", line: "E0917 15:24:00 [trail 3] trail error (kind 0, hop 00000000-0000-0000-0000-000000000001): seed pick: no seed providers available\n", faults: exactFault, wantClass: "seed-unavailable", wantDisposition: "unexplained", wantBlocking: true},
		{name: "malformed-worker-seed-unavailable-remains-blocking", line: "E0917 15:24:00 [trail invalid] trail error (kind 0, hop 00000000-0000-0000-0000-000000000000): seed pick: no seed providers available\n", faults: exactFault, wantClass: "seed-unavailable", wantDisposition: "unexplained", wantBlocking: true},
	}
	for _, test := range tests {
		fixture := newProcessLogGateFixture(t, "", "")
		appendProcessLog(t, fixture.stderrPath, test.line)
		result, err := fixture.gate.Scan(false, test.faults...)
		if err != nil || len(result.Findings) != 1 {
			t.Fatalf("%s: scan=(%+v,%v)", test.name, result, err)
		}
		finding := result.Findings[0]
		if finding.Class != test.wantClass || finding.Disposition != test.wantDisposition || finding.Blocking != test.wantBlocking {
			t.Fatalf("%s: finding=%+v", test.name, finding)
		}
		if test.wantFaultID == "" {
			if len(finding.FaultIDs) != 0 || len(finding.FaultKinds) != 0 {
				t.Fatalf("%s: unexpected fault attribution=%+v", test.name, finding)
			}
		} else {
			wantFaultKind := test.wantFaultKind
			if wantFaultKind == "" {
				wantFaultKind = "process-pause"
			}
			if len(finding.FaultIDs) != 1 || finding.FaultIDs[0] != test.wantFaultID || len(finding.FaultKinds) != 1 || finding.FaultKinds[0] != wantFaultKind {
				t.Fatalf("%s: fault attribution=%+v", test.name, finding)
			}
		}
	}
}

type processLogGateFixture struct {
	dir        string
	stdoutPath string
	stderrPath string
	manifest   SupervisorFile
	supervisor SupervisorState
	gate       *processLogGate
}

func newProcessLogGateFixture(t *testing.T, stdoutHistory, stderrHistory string) processLogGateFixture {
	t.Helper()
	dir := t.TempDir()
	processDir := filepath.Join(dir, "processes")
	if err := os.MkdirAll(processDir, 0o700); err != nil {
		t.Fatal(err)
	}
	stdoutPath := filepath.Join(processDir, "miner-1.stdout.log")
	stderrPath := filepath.Join(processDir, "miner-1.stderr.log")
	if err := os.WriteFile(stdoutPath, []byte(stdoutHistory), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stderrPath, []byte(stderrHistory), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := SupervisorFile{
		Schema: "urnetwork-sim-supervisor-v1", DeploymentID: "gate-test", BinaryHash: "binary-hash",
		Specs: []ProcessSpec{{ID: "miner-1", Role: "miner", Identity: "miner-1", StdoutPath: stdoutPath, StderrPath: stderrPath}},
	}
	manifestHash, err := canonicalHashHex(manifest)
	if err != nil {
		t.Fatal(err)
	}
	supervisor := SupervisorState{
		Schema: "urnetwork-sim-supervisor-state-v1", SupervisorPID: os.Getpid(), SupervisorStartTimeTicks: currentProcessStartTimeTicks(t), ManifestHash: manifestHash,
		Processes: []ProcessState{{ID: "miner-1", Role: "miner", Identity: "miner-1", PID: os.Getpid(), Healthy: true}},
	}
	gate, err := initializeProcessLogGate(dir, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := gate.Bind(supervisor); err != nil {
		t.Fatal(err)
	}
	return processLogGateFixture{dir: dir, stdoutPath: stdoutPath, stderrPath: stderrPath, manifest: manifest, supervisor: supervisor, gate: gate}
}

func appendProcessLog(t *testing.T, path, text string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(text); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func processLogCursorFor(t *testing.T, gate *processLogGate, stream string) processLogCursor {
	t.Helper()
	gate.stateLock.Lock()
	defer gate.stateLock.Unlock()
	for _, cursor := range gate.state.Cursors {
		if cursor.ProcessID == "miner-1" && cursor.Stream == stream {
			return cursor
		}
	}
	t.Fatalf("process-log cursor miner-1/%s is missing", stream)
	return processLogCursor{}
}

// newProcessLogGateTestSupervisor binds a fresh gate to a real, disposable
// kernel process generation.
func newProcessLogGateTestSupervisor(t *testing.T, fixture processLogGateFixture) (*processLogGate, *exec.Cmd) {
	t.Helper()
	command := exec.Command("/bin/sleep", "300")
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stopProcessLogGateTestSupervisor(command) })
	startTimeTicks, err := processStartTimeTicks(command.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	gate, err := initializeProcessLogGate(fixture.dir, fixture.manifest)
	if err != nil {
		t.Fatal(err)
	}
	supervisor := fixture.supervisor
	supervisor.SupervisorPID = command.Process.Pid
	supervisor.SupervisorStartTimeTicks = startTimeTicks
	if err := gate.Bind(supervisor); err != nil {
		t.Fatal(err)
	}
	return gate, command
}

// stopProcessLogGateTestSupervisor terminates and reaps the disposable process.
func stopProcessLogGateTestSupervisor(command *exec.Cmd) error {
	if command == nil || command.Process == nil || command.ProcessState != nil {
		return nil
	}
	killErr := command.Process.Kill()
	waitErr := command.Wait()
	if killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
		return killErr
	}
	var exitErr *exec.ExitError
	if waitErr != nil && !errors.As(waitErr, &exitErr) && !errors.Is(waitErr, os.ErrProcessDone) {
		return waitErr
	}
	return nil
}

func TestProcessLogGatePersistsLaunchBoundaryInodeOffsetsAndFindings(t *testing.T) {
	fixture := newProcessLogGateFixture(t, "panic: historical output from an earlier attempt\n", "")
	appendProcessLog(t, fixture.stdoutPath, "I0902 router POST /verify status=400\nW0902 peer connection memory budget exhausted\n")
	scanResult, err := fixture.gate.Scan(false)
	if err != nil || len(scanResult.Findings) != 0 {
		t.Fatalf("expected noise scan=(%+v,%v)", scanResult, err)
	}
	appendProcessLog(t, fixture.stdoutPath, "panic: first live failure\npanic: second live failure\n")
	scanResult, err = fixture.gate.Scan(false)
	if err != nil || len(scanResult.Findings) != 1 || scanResult.Findings[0].Class != "panic" || scanResult.Findings[0].Count != 2 || !scanResult.Findings[0].Blocking {
		t.Fatalf("live panic scan=(%+v,%v)", scanResult, err)
	}
	if scanResult.Findings[0].FirstOffset < int64(len("panic: historical output from an earlier attempt\n")) || scanResult.Findings[0].FirstLineSHA256 == "" || scanResult.Findings[0].LastLineSHA256 == "" {
		t.Fatalf("finding did not retain safe launch-relative identity: %+v", scanResult.Findings[0])
	}

	reloaded, err := loadProcessLogGate(fixture.dir, fixture.manifest, fixture.supervisor)
	if err != nil {
		t.Fatal(err)
	}
	appendProcessLog(t, fixture.stdoutPath, "panic: third live failure\n")
	scanResult, err = reloaded.Scan(false)
	if err != nil || len(scanResult.Findings) != 1 || scanResult.Findings[0].Count != 3 {
		t.Fatalf("reloaded scan=(%+v,%v)", scanResult, err)
	}
	var persisted processLogGateState
	if err := readJSONFile(filepath.Join(fixture.dir, processLogGateStateFilename), &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.Schema != processLogGateSchema || persisted.Classifier != processLogClassifierVersion || persisted.SupervisorPID != os.Getpid() || len(persisted.Cursors) != 2 || persisted.Cursors[1].Inode == 0 || persisted.Cursors[1].ScannedBytes == 0 || persisted.Cursors[1].ScannedLines != 5 || persisted.Cursors[1].ChunkChain == "" {
		t.Fatalf("persisted process log gate=%+v", persisted)
	}
	runDir := filepath.Join(fixture.dir, "runs", "test")
	if err := reloaded.WriteEvidence(runDir); err != nil {
		t.Fatal(err)
	}
	var evidence processLogGateState
	if err := readJSONFile(filepath.Join(runDir, processLogEvidenceFilename), &evidence); err != nil {
		t.Fatal(err)
	}
	if len(evidence.Findings) != 1 || evidence.Findings[0].Count != 3 || len(evidence.Cursors) != 2 || evidence.Cursors[1].ScannedBytes != persisted.Cursors[1].ScannedBytes || evidence.Cursors[1].ScannedLines != persisted.Cursors[1].ScannedLines || evidence.Cursors[1].ChunkChain != persisted.Cursors[1].ChunkChain {
		t.Fatalf("published process log evidence=%+v", evidence)
	}
}

func TestProvisionalProcessLogAcceptanceRetainsLateHistoricalLinesAndFailsClosedAfterCut(t *testing.T) {
	t.Parallel()
	fixture := newProcessLogGateFixture(t, "", "")
	fixture.gate.provisionalObservationOnly = true
	appendProcessLog(t, fixture.stderrPath, "E0902 11:27:00 completeHandshake failed: tls handshake timeout\n")
	appendProcessLog(t, fixture.stderrPath, "W0902 13:39:00 artifact stream stopped: context canceled\n")
	boundAt := time.Date(2031, time.April, 5, 14, 6, 0, 0, time.UTC)
	retained, boundaryHash, err := fixture.gate.BindAcceptance(boundAt)
	if err != nil || !validCanonicalHashHex(boundaryHash) || len(retained.Findings) != 2 {
		t.Fatalf("historical late scan could not be retained at the acceptance cut: findings=%+v hash=%q err=%v", retained.Findings, boundaryHash, err)
	}
	for _, finding := range retained.Findings {
		if finding.Blocking || finding.Disposition != "provisional-observation" {
			t.Fatalf("historical finding entered the accepted interval: %+v", finding)
		}
	}
	if current, err := fixture.gate.Scan(false); err != nil || len(current.Findings) != 0 {
		t.Fatalf("retained findings leaked through the acceptance cursor: %+v %v", current.Findings, err)
	}
	appendProcessLog(t, fixture.stderrPath, "E0902 14:07:00 completeHandshake failed: tls handshake timeout\n")
	appendProcessLog(t, fixture.stderrPath, "E0902 14:07:01 completeHandshake failed: tls handshake timeout\n")
	current, err := fixture.gate.Scan(false)
	if err != nil || len(current.Findings) != 1 || current.Findings[0].Blocking || current.Findings[0].Count != 2 || current.Findings[0].Disposition != "isolated-network-transient" || current.Findings[0].AcceptanceScope != boundaryHash {
		t.Fatalf("post-boundary retries did not retain their isolated scope: %+v %v", current.Findings, err)
	}
	// The historical timeout is outside this budget. Only the third current
	// timeout crosses the existing acceptance threshold and must fail closed.
	appendProcessLog(t, fixture.stderrPath, "E0902 14:07:02 completeHandshake failed: tls handshake timeout\n")
	current, err = fixture.gate.Scan(false)
	if err != nil || len(current.Findings) != 1 || !current.Findings[0].Blocking || current.Findings[0].Count != 3 || current.Findings[0].AcceptanceScope != boundaryHash {
		t.Fatalf("post-boundary finding did not fail closed: %+v %v", current.Findings, err)
	}
	if len(fixture.gate.state.Findings) != 3 || fixture.gate.state.AcceptanceBoundary == nil {
		t.Fatalf("durable report did not preserve retained and current findings: %+v", fixture.gate.state)
	}
	if err := validatePersistedProcessLogGate(fixture.gate.state); err != nil {
		t.Fatalf("scoped process log evidence is not self-authenticating: %v", err)
	}
}

func TestProcessLogAcceptanceCapturesReleaseSteeringFailureSequence(t *testing.T) {
	t.Parallel()
	fixture := newProcessLogGateFixture(t, "", "")
	fixture.gate.provisionalObservationOnly = true
	_, boundaryHash, err := fixture.gate.BindAcceptance(time.Date(2031, time.April, 5, 14, 6, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	appendProcessLog(t, fixture.stdoutPath, "release steer: subnet epoch 1511 attempt 1: V2 intent 2: compact measurement envelope artifact\n")
	appendProcessLog(t, fixture.stderrPath, "sim-testnet: release steering advanced from incomplete epoch 1511 to 1512\n")
	result, err := fixture.gate.Scan(false)
	if err != nil || len(result.Findings) != 2 {
		t.Fatalf("release steering failure scan=(%+v,%v)", result, err)
	}
	classes := map[string]bool{}
	for _, finding := range result.Findings {
		if !finding.Blocking || finding.Disposition != "unexplained" || finding.AcceptanceScope != boundaryHash {
			t.Fatalf("release steering failure escaped acceptance scope: %+v", finding)
		}
		classes[finding.Class] = true
	}
	if !classes["release-steering-attempt-failure"] || !classes["release-steering-continuity"] {
		t.Fatalf("release steering sequence classes=%v", classes)
	}
}

func TestProcessLogAcceptanceRejectsHistoricalStrictFindingAndPrefixSubstitution(t *testing.T) {
	t.Parallel()
	strict := newProcessLogGateFixture(t, "", "")
	appendProcessLog(t, strict.stderrPath, "E0902 11:27:00 completeHandshake failed: tls handshake timeout\n")
	if _, hash, err := strict.gate.BindAcceptance(time.Now().UTC()); err == nil || hash != "" || strict.gate.state.AcceptanceBoundary != nil {
		t.Fatalf("strict acceptance cut waived a pre-boundary release failure: hash=%q err=%v", hash, err)
	}

	provisional := newProcessLogGateFixture(t, "", "")
	provisional.gate.provisionalObservationOnly = true
	appendProcessLog(t, provisional.stderrPath, "I0902 14:00:00 retained healthy line\n")
	if _, _, err := provisional.gate.BindAcceptance(time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(provisional.stderrPath)
	if err != nil {
		t.Fatal(err)
	}
	data[0] ^= 1
	if err := os.WriteFile(provisional.stderrPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := provisional.gate.Scan(false)
	if err != nil || len(result.Findings) != 1 || !result.Findings[0].Blocking || result.Findings[0].Class != "log-integrity" {
		t.Fatalf("signed process-log prefix substitution was not rejected: %+v %v", result.Findings, err)
	}
}

func TestProcessLogGateRejectsPriorClassifierVersion(t *testing.T) {
	fixture := newProcessLogGateFixture(t, "", "")
	fixture.gate.stateLock.Lock()
	fixture.gate.state.Classifier = "urnetwork-sim-process-log-classifier-v1"
	err := fixture.gate.persistWithLock()
	fixture.gate.stateLock.Unlock()
	if err != nil {
		t.Fatal(err)
	}

	if _, err := loadProcessLogGate(fixture.dir, fixture.manifest, fixture.supervisor); err == nil || !strings.Contains(err.Error(), "schema or classifier") {
		t.Fatalf("prior classifier version load error=%v", err)
	}
}

func TestProcessLogGateMigratesV2CursorAndFindingInventoryWithoutRescan(t *testing.T) {
	t.Parallel()
	fixture := newProcessLogGateFixture(t, "", "")
	appendProcessLog(t, fixture.stderrPath, "E0902 11:27:00 completeHandshake failed: tls handshake timeout\n")
	if _, err := fixture.gate.Scan(false); err != nil {
		t.Fatal(err)
	}
	wantCursors := append([]processLogCursor(nil), fixture.gate.state.Cursors...)
	wantFindings := append([]ProcessLogFinding(nil), fixture.gate.state.Findings...)
	fixture.gate.state.Classifier = processLogClassifierV2
	if err := fixture.gate.persistWithLock(); err != nil {
		t.Fatal(err)
	}
	migrated, err := loadProcessLogGate(fixture.dir, fixture.manifest, fixture.supervisor)
	if err != nil {
		t.Fatal(err)
	}
	if migrated.state.Classifier != processLogClassifierVersion || !reflect.DeepEqual(migrated.state.Cursors, wantCursors) || !reflect.DeepEqual(migrated.state.Findings, wantFindings) {
		t.Fatalf("classifier migration changed retained evidence: %+v", migrated.state)
	}
	var persisted processLogGateState
	if err := readJSONFile(migrated.path, &persisted); err != nil || persisted.Classifier != processLogClassifierVersion || !reflect.DeepEqual(persisted.Cursors, wantCursors) || !reflect.DeepEqual(persisted.Findings, wantFindings) {
		t.Fatalf("classifier migration was not durable and lossless: %+v %v", persisted, err)
	}
}

func TestProcessLogGateMigratesV3SignedScopeWithoutRewritingEvidence(t *testing.T) {
	t.Parallel()
	fixture := newProcessLogGateFixture(t, "", "")
	_, boundaryHash, err := fixture.gate.BindAcceptance(time.Unix(123, 0))
	if err != nil || boundaryHash == "" {
		t.Fatalf("bind acceptance=(%q,%v)", boundaryHash, err)
	}
	appendProcessLog(t, fixture.stderrPath, "I0902 11:27:00 h3 connect err: tls: internal error\n")
	if _, err := fixture.gate.Scan(false); err != nil {
		t.Fatal(err)
	}
	wantCursors := append([]processLogCursor(nil), fixture.gate.state.Cursors...)
	wantFindings := append([]ProcessLogFinding(nil), fixture.gate.state.Findings...)
	wantBoundary := *fixture.gate.state.AcceptanceBoundary
	fixture.gate.state.Classifier = processLogClassifierV3
	if err := fixture.gate.persistWithLock(); err != nil {
		t.Fatal(err)
	}
	migrated, err := loadProcessLogGate(fixture.dir, fixture.manifest, fixture.supervisor)
	if err != nil {
		t.Fatal(err)
	}
	if migrated.state.Classifier != processLogClassifierVersion || !reflect.DeepEqual(migrated.state.Cursors, wantCursors) || !reflect.DeepEqual(migrated.state.Findings, wantFindings) || !reflect.DeepEqual(migrated.state.AcceptanceBoundary, &wantBoundary) {
		t.Fatalf("classifier migration changed signed evidence: %+v", migrated.state)
	}
}

func TestProcessLogGateMigratesV11WithoutReclassifyingRetainedAlerts(t *testing.T) {
	t.Parallel()
	fixture := newProcessLogGateFixture(t, "", "")
	appendProcessLog(t, fixture.stderrPath, "E0924 08:48:16.375559 1387508 st_controller.go:3327] [st]epoch 597 commit DEADLINE ALERT: unconfirmed with ~7m48s left (deadline block 8074824, head 8074785)\n")
	if _, err := fixture.gate.Scan(false); err != nil {
		t.Fatal(err)
	}
	retained := append([]ProcessLogFinding(nil), fixture.gate.state.Findings...)
	fixture.gate.state.Classifier = processLogClassifierV11
	if err := fixture.gate.persistWithLock(); err != nil {
		t.Fatal(err)
	}
	migrated, err := loadProcessLogGate(fixture.dir, fixture.manifest, fixture.supervisor)
	if err != nil || migrated.state.Classifier != processLogClassifierVersion || !reflect.DeepEqual(migrated.state.Findings, retained) {
		t.Fatalf("classifier migration changed retained evidence: %v", err)
	}
}

func TestProcessLogGateIncludesTemporaryProvisioningAfterEarlyBoundary(t *testing.T) {
	dir := t.TempDir()
	processDir := filepath.Join(dir, "processes")
	if err := os.MkdirAll(processDir, 0o700); err != nil {
		t.Fatal(err)
	}
	stdoutPath := filepath.Join(processDir, "operator-1-api.stdout.log")
	stderrPath := filepath.Join(processDir, "operator-1-api.stderr.log")
	if err := os.WriteFile(stdoutPath, []byte("historical attempt\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stderrPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := SupervisorFile{Schema: "urnetwork-sim-supervisor-v1", DeploymentID: "provisioning", BinaryHash: "hash", Specs: []ProcessSpec{{
		ID: "operator-1-api", Role: "operator-api", Identity: "no:1", StdoutPath: stdoutPath, StderrPath: stderrPath,
	}}}
	boundary, err := processLogCursors(dir, SupervisorFile{Specs: manifest.Specs})
	if err != nil {
		t.Fatal(err)
	}
	appendProcessLog(t, stdoutPath, "[reconcile]stripe: panic: temporary provisioning failure\n")
	gate, err := initializeProcessLogGateAtBoundary(dir, manifest, boundary)
	if err != nil {
		t.Fatal(err)
	}
	scanResult, err := gate.Scan(false)
	if err != nil || len(scanResult.Findings) != 1 || scanResult.Findings[0].Class != "panic" {
		t.Fatalf("temporary provisioning scan=(%+v,%v)", scanResult, err)
	}
}

func TestProcessLogGateRejectsAndDigestsUnterminatedFinalTailWithoutClassifyingIt(t *testing.T) {
	fixture := newProcessLogGateFixture(t, "", "")
	appendProcessLog(t, fixture.stderrPath, "panic: unterminated")
	scanResult, err := fixture.gate.Scan(false)
	if err != nil || len(scanResult.Findings) != 0 {
		t.Fatalf("nonfinal partial scan=(%+v,%v)", scanResult, err)
	}
	scanResult, err = fixture.gate.Scan(true)
	if err != nil || len(scanResult.Findings) != 1 || scanResult.Findings[0].Class != "log-integrity" || scanResult.Findings[0].Summary != "process log has an unterminated final line" || !scanResult.Findings[0].Blocking {
		t.Fatalf("final partial scan=(%+v,%v)", scanResult, err)
	}
	cursor := processLogCursorFor(t, fixture.gate, "stderr")
	if cursor.Offset != int64(len("panic: unterminated")) || cursor.DigestOffset != cursor.Offset || cursor.ScannedBytes != uint64(cursor.Offset) || cursor.ScannedLines != 1 || cursor.ChunkCount != 1 {
		t.Fatalf("unterminated final cursor=%+v", cursor)
	}
}

func TestProcessLogGateChunkChainIsDeterministicAcrossScansAndReload(t *testing.T) {
	line := "I0902 18:07:08 accepted healthy HTTP/3 carrier connection\n"
	content := strings.Repeat(line, 4_000)
	split := len(line) * 1_733

	oneShot := newProcessLogGateFixture(t, "", "")
	appendProcessLog(t, oneShot.stdoutPath, content)
	if scanResult, err := oneShot.gate.Scan(true); err != nil || len(scanResult.Findings) != 0 {
		t.Fatalf("one-shot final scan=(%+v,%v)", scanResult, err)
	}
	oneShotCursor := processLogCursorFor(t, oneShot.gate, "stdout")

	partitioned := newProcessLogGateFixture(t, "", "")
	appendProcessLog(t, partitioned.stdoutPath, content[:split])
	if scanResult, err := partitioned.gate.Scan(false); err != nil || len(scanResult.Findings) != 0 {
		t.Fatalf("partitioned first scan=(%+v,%v)", scanResult, err)
	}
	reloaded, err := loadProcessLogGate(partitioned.dir, partitioned.manifest, partitioned.supervisor)
	if err != nil {
		t.Fatal(err)
	}
	appendProcessLog(t, partitioned.stdoutPath, content[split:])
	if scanResult, err := reloaded.Scan(false); err != nil || len(scanResult.Findings) != 0 {
		t.Fatalf("partitioned second scan=(%+v,%v)", scanResult, err)
	}
	beforeFinal := processLogCursorFor(t, reloaded, "stdout")
	if beforeFinal.Offset != int64(len(content)) || beforeFinal.DigestOffset >= beforeFinal.Offset {
		t.Fatalf("nonfinal scan did not retain a bounded partial digest chunk: %+v", beforeFinal)
	}
	if scanResult, err := reloaded.Scan(true); err != nil || len(scanResult.Findings) != 0 {
		t.Fatalf("partitioned final scan=(%+v,%v)", scanResult, err)
	}
	partitionedCursor := processLogCursorFor(t, reloaded, "stdout")
	expectedChunks := uint64((len(content) + processLogDigestChunkBytes - 1) / processLogDigestChunkBytes)
	if partitionedCursor.Offset != int64(len(content)) || partitionedCursor.DigestOffset != partitionedCursor.Offset || partitionedCursor.ScannedBytes != uint64(len(content)) || partitionedCursor.ScannedLines != 4_000 || partitionedCursor.ChunkCount != expectedChunks {
		t.Fatalf("partitioned final cursor=%+v", partitionedCursor)
	}
	if oneShotCursor.Offset != partitionedCursor.Offset || oneShotCursor.DigestOffset != partitionedCursor.DigestOffset || oneShotCursor.ScannedBytes != partitionedCursor.ScannedBytes || oneShotCursor.ScannedLines != partitionedCursor.ScannedLines || oneShotCursor.ChunkCount != partitionedCursor.ChunkCount || oneShotCursor.ChunkChain != partitionedCursor.ChunkChain {
		t.Fatalf("chunk chain depends on scan partitioning: one_shot=%+v partitioned=%+v", oneShotCursor, partitionedCursor)
	}
}

func TestProcessLogGateFailsClosedOnMissingTruncatedAndRotatedLogs(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, processLogGateFixture)
	}{
		{name: "missing", mutate: func(t *testing.T, fixture processLogGateFixture) {
			if err := os.Remove(fixture.stdoutPath); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "truncated", mutate: func(t *testing.T, fixture processLogGateFixture) {
			if err := os.Truncate(fixture.stdoutPath, 0); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "rotated", mutate: func(t *testing.T, fixture processLogGateFixture) {
			rotated := fixture.stdoutPath + ".old"
			if err := os.Rename(fixture.stdoutPath, rotated); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(fixture.stdoutPath, []byte("replacement\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, test := range tests {
		fixture := newProcessLogGateFixture(t, "launch history\n", "")
		test.mutate(t, fixture)
		scanResult, err := fixture.gate.Scan(false)
		if err != nil || len(scanResult.Findings) != 1 || scanResult.Findings[0].Class != "log-integrity" {
			t.Fatalf("%s: integrity scan=(%+v,%v)", test.name, scanResult, err)
		}
	}
}

func TestProcessLogGateDefersSealedPrefixRehashUntilFinalAcceptance(t *testing.T) {
	fixture := newProcessLogGateFixture(t, "", "")
	appendProcessLog(t, fixture.stdoutPath, "healthy sealed history\n")
	if _, _, err := fixture.gate.BindAcceptance(time.Date(2032, time.January, 2, 3, 4, 5, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if result, err := fixture.gate.Scan(false); err != nil || len(result.Findings) != 0 || !fixture.gate.acceptancePrefixesVerified {
		t.Fatalf("initial provisional scan=(%+v,%v), prefixVerified=%t", result, err, fixture.gate.acceptancePrefixesVerified)
	}
	reloaded, err := loadProcessLogGate(fixture.dir, fixture.manifest, fixture.supervisor)
	if err != nil || !reloaded.state.ProvisionalPrefixVerified {
		t.Fatalf("reload provisional prefix memo=(%+v,%v)", reloaded, err)
	}
	fixture.gate = reloaded
	file, err := os.OpenFile(fixture.stdoutPath, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteAt([]byte("H"), 0); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	// Provisional polling preserves the prior check and waits for final
	// acceptance to reread the sealed prefix. The final check remains hard.
	if result, err := fixture.gate.Scan(false); err != nil || len(result.Findings) != 0 {
		t.Fatalf("cached provisional scan=(%+v,%v)", result, err)
	}
	result, err := fixture.gate.Scan(true)
	if err != nil || len(result.Findings) != 1 || result.Findings[0].Class != "log-integrity" || result.Findings[0].Summary != "process log acceptance prefix changed" || !result.Findings[0].Blocking {
		t.Fatalf("final prefix verification=(%+v,%v)", result, err)
	}
}

func TestProcessLogGateFailsClosedOnZeroAndPartialShortReads(t *testing.T) {
	tests := []struct {
		name      string
		readBytes int
	}{
		{name: "zero", readBytes: 0},
		{name: "partial", readBytes: 3},
	}
	for _, test := range tests {
		fixture := newProcessLogGateFixture(t, "", "")
		line := "I0902 18:07:08 healthy process output\n"
		appendProcessLog(t, fixture.stdoutPath, line)
		fixture.gate.readRangeForTest = func(_ *os.File, offset, length int64) ([]byte, error) {
			if offset != 0 || length != int64(len(line)) {
				t.Fatalf("%s: read range=%d/%d, want 0/%d", test.name, offset, length, len(line))
			}
			return make([]byte, test.readBytes), nil
		}

		scanResult, err := fixture.gate.Scan(true)
		if err != nil || len(scanResult.Findings) != 1 {
			t.Fatalf("%s: short-read scan=(%+v,%v)", test.name, scanResult, err)
		}
		finding := scanResult.Findings[0]
		if finding.Class != "log-integrity" || finding.Summary != "process log changed while it was read" || !finding.Blocking {
			t.Fatalf("%s: short-read finding=%+v", test.name, finding)
		}
		cursor := processLogCursorFor(t, fixture.gate, "stdout")
		if cursor.Offset != 0 || cursor.DigestOffset != 0 || cursor.ScannedBytes != 0 || cursor.ScannedLines != 0 || cursor.ChunkCount != 0 {
			t.Fatalf("%s: short read advanced cursor=%+v", test.name, cursor)
		}
	}
}

func TestProcessLogGateRejectsSupervisorGenerationChange(t *testing.T) {
	fixture := newProcessLogGateFixture(t, "", "")
	changed := fixture.supervisor
	changed.SupervisorPID++
	if err := fixture.gate.Bind(changed); err == nil || !strings.Contains(err.Error(), "generation changed") {
		t.Fatalf("generation change error=%v", err)
	}
}

func TestProcessLogGateScanRejectsDeadBoundSupervisor(t *testing.T) {
	fixture := newProcessLogGateFixture(t, "", "")
	gate, command := newProcessLogGateTestSupervisor(t, fixture)
	if err := stopProcessLogGateTestSupervisor(command); err != nil {
		t.Fatal(err)
	}

	if _, err := gate.Scan(false); err == nil || !strings.Contains(err.Error(), "process log gate supervisor generation") {
		t.Fatalf("dead supervisor scan error=%v", err)
	}
}

func TestProcessLogGateScanRejectsBoundSupervisorStartMismatch(t *testing.T) {
	fixture := newProcessLogGateFixture(t, "", "")
	gate, err := initializeProcessLogGate(fixture.dir, fixture.manifest)
	if err != nil {
		t.Fatal(err)
	}
	supervisor := fixture.supervisor
	supervisor.SupervisorStartTimeTicks++
	if err := gate.Bind(supervisor); err != nil {
		t.Fatal(err)
	}

	if _, err := gate.Scan(false); err == nil || !strings.Contains(err.Error(), "start time changed") {
		t.Fatalf("mismatched supervisor generation scan error=%v", err)
	}
}

func TestScenarioProcessLogGateRejectsSupervisorDeathDuringFinalScan(t *testing.T) {
	cfg := testResolvedConfig(t)
	fixture := newProcessLogGateFixture(t, "", "")
	gate, command := newProcessLogGateTestSupervisor(t, fixture)
	var stopErr error
	gate.afterCursorsForTest = func(final bool) {
		if final {
			stopErr = stopProcessLogGateTestSupervisor(command)
		}
	}
	observation := testScenarioObservation(cfg, 1)
	definition := scenarioDefinition{Name: "unit-process-log-supervisor-final", Checks: []scenarioCheck{{ID: "healthy", Check: func(*scenarioEvaluation) (bool, string) { return true, "healthy" }}}}

	result, err := runScenarioWithProbe(context.Background(), cfg, fixture.dir, definition, &staticScenarioProbe{observations: []*ScenarioObservation{observation}}, scenarioRunOptions{Publish: false, ProcessLogs: gate})
	if stopErr != nil {
		t.Fatal(stopErr)
	}
	if err == nil || result == nil || result.Result != "fail" {
		t.Fatalf("supervisor final-race result=%+v error=%v", result, err)
	}
	found := false
	for _, assertion := range result.Assertions {
		if assertion.ID == "process_log_final" && !assertion.Passed && strings.Contains(assertion.Message, "supervisor generation") {
			found = true
		}
	}
	if !found {
		t.Fatalf("supervisor final-race assertion missing: %+v", result.Assertions)
	}
	if _, statErr := os.Stat(filepath.Join(fixture.dir, "runs", result.RunID, "complete.json")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("supervisor final race wrote complete marker: %v", statErr)
	}
}

func missingProcessLogFixture(t *testing.T) (string, string, string, SupervisorFile, SupervisorState) {
	t.Helper()
	dir := t.TempDir()
	processDir := filepath.Join(dir, "processes")
	if err := os.MkdirAll(processDir, 0o700); err != nil {
		t.Fatal(err)
	}
	stdoutPath := filepath.Join(processDir, "miner-1.stdout.log")
	stderrPath := filepath.Join(processDir, "miner-1.stderr.log")
	manifest := SupervisorFile{
		Schema:       "urnetwork-sim-supervisor-v1",
		DeploymentID: "missing-before-start",
		BinaryHash:   "binary-hash",
		Specs: []ProcessSpec{{
			ID: "miner-1", Role: "miner", Identity: "miner-1", StdoutPath: stdoutPath, StderrPath: stderrPath,
		}},
	}
	manifestHash, err := canonicalHashHex(manifest)
	if err != nil {
		t.Fatal(err)
	}
	supervisor := SupervisorState{
		Schema: "urnetwork-sim-supervisor-state-v1", SupervisorPID: os.Getpid(), SupervisorStartTimeTicks: currentProcessStartTimeTicks(t), ManifestHash: manifestHash,
		Processes: []ProcessState{{ID: "miner-1", Role: "miner", Identity: "miner-1", PID: os.Getpid(), Healthy: true}},
	}
	return dir, stdoutPath, stderrPath, manifest, supervisor
}

func writeSupervisorStateForProcessLogTest(t *testing.T, dir string, supervisor SupervisorState) {
	t.Helper()
	raw, err := json.Marshal(supervisor)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "supervisor.state.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSupervisorReadinessBindsLogsCreatedAfterGateInitialization(t *testing.T) {
	dir, stdoutPath, stderrPath, manifest, supervisor := missingProcessLogFixture(t)
	gate, err := initializeProcessLogGate(dir, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if len(gate.state.Findings) != 0 || gate.state.Cursors[0].Inode != 0 || gate.state.Cursors[1].Inode != 0 {
		t.Fatalf("missing pre-start logs were prematurely rejected: %+v", gate.state)
	}
	if err := os.WriteFile(stdoutPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stderrPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	writeSupervisorStateForProcessLogTest(t, dir, supervisor)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ready, err := waitSupervisorReady(ctx, dir, manifest, gate, 100*time.Millisecond)
	if err != nil || ready == nil {
		t.Fatalf("logs created by healthy supervisor did not bind: ready=%+v error=%v", ready, err)
	}
	for _, cursor := range gate.state.Cursors {
		if cursor.Inode == 0 || cursor.Device == 0 {
			t.Fatalf("healthy supervisor log did not acquire an inode: %+v", cursor)
		}
	}
}

func TestSupervisorReadinessFailsClosedWhenHealthyProcessLogIsMissing(t *testing.T) {
	dir, stdoutPath, _, manifest, supervisor := missingProcessLogFixture(t)
	gate, err := initializeProcessLogGate(dir, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stdoutPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	writeSupervisorStateForProcessLogTest(t, dir, supervisor)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err = waitSupervisorReady(ctx, dir, manifest, gate, 100*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "process log gate") {
		t.Fatalf("healthy supervisor with a missing log error=%v", err)
	}
	if len(gate.state.Findings) != 1 || gate.state.Findings[0].Class != "log-integrity" || gate.state.Findings[0].Summary != "process log is missing" || !gate.state.Findings[0].Blocking {
		t.Fatalf("missing healthy log findings=%+v", gate.state.Findings)
	}
}

func TestSupervisorReadinessFailsOnProcessLogError(t *testing.T) {
	fixture := newProcessLogGateFixture(t, "", "")
	// Exercise the unbound launch path rather than the bound fixture helper.
	gate, err := initializeProcessLogGate(fixture.dir, fixture.manifest)
	if err != nil {
		t.Fatal(err)
	}
	appendProcessLog(t, fixture.stderrPath, "E0902 18:07:08 unexpected startup error\n")
	stateBytes, err := json.Marshal(fixture.supervisor)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture.dir, "supervisor.state.json"), stateBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err = waitSupervisorReady(ctx, fixture.dir, fixture.manifest, gate, 100*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "process log gate") {
		t.Fatalf("readiness process log error=%v", err)
	}
}

type stagedScenarioProcessLogGate struct {
	scans   int
	failAt  int
	writes  int
	finding ProcessLogFinding
}

type writingScenarioFaultDriver struct {
	path string
}

func appendProcessLogLine(path, line string) error {
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.WriteString(line); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func (self *writingScenarioFaultDriver) Apply(_ context.Context, spec scenarioFaultSpec) ([]FaultProcessEvidence, error) {
	if err := appendProcessLogLine(self.path, "E0902 18:07:08 dependency dial failed: connection refused\n"); err != nil {
		return nil, err
	}
	return []FaultProcessEvidence{{ID: spec.Targets[0], Role: "miner", Identity: spec.Targets[0], PID: os.Getpid()}}, nil
}

func (self *writingScenarioFaultDriver) Restore(_ context.Context, spec scenarioFaultSpec) ([]FaultProcessEvidence, error) {
	if err := appendProcessLogLine(self.path, "E0902 18:07:09 in-flight request failed: connection reset by peer\n"); err != nil {
		return nil, err
	}
	return []FaultProcessEvidence{{ID: spec.Targets[0], Role: "miner", Identity: spec.Targets[0], PID: os.Getpid()}}, nil
}

func (self *writingScenarioFaultDriver) Recover(context.Context) error {
	return nil
}

func (self *stagedScenarioProcessLogGate) Scan(bool, ...processLogFaultScope) (processLogScanResult, error) {
	self.scans++
	if self.scans < self.failAt {
		return processLogScanResult{}, nil
	}
	return processLogScanResult{Findings: []ProcessLogFinding{self.finding}}, nil
}

func (self *stagedScenarioProcessLogGate) WriteEvidence(runDir string) error {
	self.writes++
	raw, err := json.Marshal(map[string]any{"schema": processLogGateSchema, "scan_count": self.scans})
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(runDir, processLogEvidenceFilename), append(raw, '\n'), 0o644)
}

func TestScenarioProcessLogGateAttributesApplyAndRestoreWindowsWithoutGrace(t *testing.T) {
	cfg := testResolvedConfig(t)
	fixture := newProcessLogGateFixture(t, "", "")
	observations := []*ScenarioObservation{testScenarioObservation(cfg, 1), testScenarioObservation(cfg, 1), testScenarioObservation(cfg, 1)}
	observations[0].Status.Contracts.FinalizedHead.Number = 100
	observations[1].Status.Contracts.FinalizedHead.Number = 101
	observations[2].Status.Contracts.FinalizedHead.Number = 102
	definition := scenarioDefinition{
		Name: "unit-process-log-fault-window",
		Checks: []scenarioCheck{{ID: "always", Check: func(*scenarioEvaluation) (bool, string) {
			return true, "ready"
		}}},
		Faults: []scenarioFaultSpec{{ID: "pause-miner", Kind: "process-pause", Targets: []string{"miner-1"}, TriggerOffsetBlocks: 1, DurationBlocks: 1}},
	}
	driver := &writingScenarioFaultDriver{path: fixture.stderrPath}
	probe := &staticScenarioProbe{observations: observations}
	now := time.Date(2031, time.April, 5, 14, 6, 0, 0, time.UTC)
	testClock := func() time.Time {
		current := now
		now = now.Add(time.Millisecond)
		return current
	}
	result, err := runScenarioWithProbe(context.Background(), cfg, fixture.dir, definition, probe, scenarioRunOptions{
		PollInterval: time.Microsecond,
		Timeout:      time.Second,
		Publish:      false,
		FaultDriver:  driver,
		ProcessLogs:  fixture.gate,
		Now:          testClock,
	})
	if err != nil || result == nil || result.Result != "pass" || len(result.Faults) != 1 || result.Faults[0].Status != "restored" || result.Faults[0].AppliedBlock != 101 || result.Faults[0].RestoredBlock != 102 || probe.calls != 3 {
		t.Fatalf("fault-window scenario result=%+v probe_calls=%d error=%v", result, probe.calls, err)
	}
	var evidence processLogGateState
	if err := readJSONFile(filepath.Join(fixture.dir, "runs", result.RunID, processLogEvidenceFilename), &evidence); err != nil {
		t.Fatal(err)
	}
	if len(evidence.Findings) != 1 || evidence.Findings[0].Class != "error" || evidence.Findings[0].Blocking || evidence.Findings[0].Disposition != "expected-fault" || evidence.Findings[0].Count != 2 || len(evidence.Findings[0].FaultIDs) != 1 || evidence.Findings[0].FaultIDs[0] != "pause-miner" {
		t.Fatalf("apply/restore fault evidence=%+v", evidence.Findings)
	}
	if result.Anomalies == nil || result.Anomalies.Status == "open" || len(result.Anomalies.Entries) != 0 {
		t.Fatalf("expected-fault telemetry opened anomaly ledger: %+v", result.Anomalies)
	}

	appendProcessLog(t, fixture.stderrPath, "E0902 18:07:10 post-restore dial failed: connection refused\n")
	scanResult, err := fixture.gate.Scan(false)
	if err != nil || len(scanResult.Findings) != 2 {
		t.Fatalf("post-restore scan=(%+v,%v)", scanResult, err)
	}
	blocking := blockingProcessLogFindings(scanResult.Findings)
	if len(blocking) != 1 || blocking[0].Disposition != "unexplained" || blocking[0].Count != 1 {
		t.Fatalf("post-restore signal received fault grace: %+v", scanResult.Findings)
	}
}

func TestScenarioProcessLogGateRejectsCompletionAndPersistsObservation(t *testing.T) {
	cfg := testResolvedConfig(t)
	dir := t.TempDir()
	observation := testScenarioObservation(cfg, 1)
	definition := scenarioDefinition{Name: "unit-process-log", Checks: []scenarioCheck{{ID: "healthy", Check: func(*scenarioEvaluation) (bool, string) { return true, "healthy" }}}}
	gate := &stagedScenarioProcessLogGate{failAt: 2, finding: ProcessLogFinding{
		ProcessID: "validator-1", Role: "validator", Stream: "stderr", Class: "panic", Summary: "process reported a panic", Blocking: true, Disposition: "unexplained", Count: 1,
		FirstOffset: 42, LastOffset: 42, FirstObservedAt: time.Now().UTC().Format(time.RFC3339Nano), LastObservedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}}
	result, err := runScenarioWithProbe(context.Background(), cfg, dir, definition, &staticScenarioProbe{observations: []*ScenarioObservation{observation}}, scenarioRunOptions{Publish: false, ProcessLogs: gate})
	if err == nil || result == nil || result.Result != "fail" || gate.scans != 2 {
		t.Fatalf("result=%+v error=%v scans=%d", result, err, gate.scans)
	}
	if result.Anomalies == nil || result.Anomalies.Status != "open" {
		t.Fatalf("anomaly ledger=%+v", result.Anomalies)
	}
	found := false
	for _, anomaly := range result.Anomalies.Entries {
		if anomaly.Class == "process-log-panic" && anomaly.Source == "process:validator-1:stderr" {
			found = true
		}
	}
	if !found {
		t.Fatalf("process log anomaly missing: %+v", result.Anomalies.Entries)
	}
	runDir := filepath.Join(dir, "runs", result.RunID)
	if _, statErr := os.Stat(filepath.Join(runDir, processLogEvidenceFilename)); statErr != nil {
		t.Fatal(statErr)
	}
	if _, statErr := os.Stat(filepath.Join(runDir, "complete.json")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("failed process log campaign wrote complete marker: %v", statErr)
	}
	observations, readErr := os.ReadFile(filepath.Join(runDir, "observations.jsonl"))
	if readErr != nil || !strings.Contains(string(observations), `"process_log_findings"`) {
		t.Fatalf("process log finding not persisted in observations: %v %s", readErr, observations)
	}
}

func TestScenarioProcessLogGateClosesFinalCompletionRace(t *testing.T) {
	cfg := testResolvedConfig(t)
	dir := t.TempDir()
	observation := testScenarioObservation(cfg, 1)
	definition := scenarioDefinition{Name: "unit-process-log-final", Checks: []scenarioCheck{{ID: "healthy", Check: func(*scenarioEvaluation) (bool, string) { return true, "healthy" }}}}
	gate := &stagedScenarioProcessLogGate{failAt: 3, finding: ProcessLogFinding{
		ProcessID: "miner-1", Role: "miner", Stream: "stdout", Class: "tls-handshake-timeout", Summary: "TLS handshake timed out", Blocking: true, Disposition: "unexplained", Count: 1,
		FirstOffset: 90, LastOffset: 90, FirstObservedAt: time.Now().UTC().Format(time.RFC3339Nano), LastObservedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}}
	result, err := runScenarioWithProbe(context.Background(), cfg, dir, definition, &staticScenarioProbe{observations: []*ScenarioObservation{observation}}, scenarioRunOptions{Publish: false, ProcessLogs: gate})
	if err == nil || result == nil || result.Result != "fail" || gate.scans != 3 {
		t.Fatalf("result=%+v error=%v scans=%d", result, err, gate.scans)
	}
	found := false
	for _, assertion := range result.Assertions {
		if assertion.ID == "process_log_final" && !assertion.Passed {
			found = true
		}
	}
	if !found {
		t.Fatalf("final process log assertion missing: %+v", result.Assertions)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "runs", result.RunID, "complete.json")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("final race wrote complete marker: %v", statErr)
	}
}

func TestReleaseAndProductionScenariosRequireProcessLogGate(t *testing.T) {
	cfg := testResolvedConfig(t)
	for _, name := range []string{"release-1.0", "production-soak"} {
		definition := scenarioDefinition{Name: name, Checks: []scenarioCheck{{ID: "unused", Check: func(*scenarioEvaluation) (bool, string) { return true, "unused" }}}}
		result, err := runScenarioWithProbe(context.Background(), cfg, t.TempDir(), definition, nil, scenarioRunOptions{Publish: false})
		if err == nil || result != nil || !strings.Contains(err.Error(), "require the persisted process log gate") {
			t.Fatalf("%s without process-log gate result=%+v error=%v", name, result, err)
		}
	}
}
