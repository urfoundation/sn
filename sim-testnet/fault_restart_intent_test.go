// Restart interruption points are forced at persistence and signal boundaries.
// Every process is a synthetic child owned and reaped by this test fixture.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"
	"time"
)

type processRestartIntentFixture struct {
	driver   *liveScenarioFaultDriver
	fault    scenarioFaultSpec
	manifest SupervisorFile
	state    SupervisorState
	commands []supervisedCommand
	exits    []<-chan error
}

func newProcessRestartIntentFixture(t *testing.T, count int) *processRestartIntentFixture {
	t.Helper()
	dir := t.TempDir()
	fixture := &processRestartIntentFixture{
		driver:   &liveScenarioFaultDriver{stateDir: dir},
		fault:    scenarioFaultSpec{ID: "synthetic-write-ahead", Kind: "process-restart", DurationBlocks: 4},
		manifest: SupervisorFile{Schema: "urnetwork-sim-supervisor-v1"},
		state:    SupervisorState{Schema: "urnetwork-sim-supervisor-state-v1", SupervisorPID: os.Getpid()},
	}
	var err error
	fixture.state.SupervisorStartTimeTicks, err = processStartTimeTicks(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	for index := range count {
		id := fmt.Sprintf("synthetic-worker-%d", index)
		spec := ProcessSpec{
			ID: id, Role: "miner-swarm", Identity: id + "-members", RestartLimit: 2,
			Command: "/bin/sleep", Args: []string{"300"}, WorkDir: dir,
			StdoutPath: filepath.Join(dir, id+".out"), StderrPath: filepath.Join(dir, id+".err"),
		}
		command, exited := startRestartIntentTestProcess(t, spec)
		fixture.commands = append(fixture.commands, command)
		fixture.exits = append(fixture.exits, exited)
		fixture.manifest.Specs = append(fixture.manifest.Specs, spec)
		fixture.fault.Targets = append(fixture.fault.Targets, id)
		fixture.state.Processes = append(fixture.state.Processes, ProcessState{ID: id, Role: spec.Role, Identity: spec.Identity, PID: command.identity.PID, Healthy: true})
	}
	fixture.write(t)
	return fixture
}

func startRestartIntentTestProcess(t *testing.T, spec ProcessSpec) (supervisedCommand, <-chan error) {
	t.Helper()
	cmd, exited, err := startSpecWithExit(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); <-exited })
	identity, err := observeStartedSupervisedProcessIdentity(t.Context(), cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	return supervisedCommand{spec: spec, cmd: cmd, identity: identity}, exited
}

func (self *processRestartIntentFixture) write(t *testing.T) {
	t.Helper()
	hash, err := canonicalHashHex(self.manifest)
	if err != nil {
		t.Fatal(err)
	}
	self.state.ManifestHash = hash
	if err := writePublicJSON(filepath.Join(self.driver.stateDir, "supervisor.json"), self.manifest); err != nil {
		t.Fatal(err)
	}
	if err := writePublicJSON(filepath.Join(self.driver.stateDir, "supervisor.state.json"), self.state); err != nil {
		t.Fatal(err)
	}
}

func (self *processRestartIntentFixture) retained(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(self.driver.activePath())
	if err != nil {
		t.Fatal(err)
	}
	active, err := readActiveFaultFile(self.driver.activePath())
	if err != nil || len(active.Processes) != len(self.commands) {
		t.Fatalf("durable restart census = %+v, %v", active.Processes, err)
	}
	if _, err := activeFaultIndex(active, self.fault); err != nil {
		t.Fatal(err)
	}
	for index, original := range active.Processes {
		if original.PID != self.commands[index].identity.PID || original.StartTimeTicks != self.commands[index].identity.StartTimeTicks || original.StartTimeTicks == 0 {
			t.Fatalf("retained original %d differs: %+v", index, original)
		}
	}
	return raw
}

func TestProcessRestartWriteAheadFailureSendsNoSignal(t *testing.T) {
	fixture := newProcessRestartIntentFixture(t, 1)
	calls := 0
	fixture.driver.restartSignal = func(supervisedCommand, syscall.Signal) bool { calls++; return true }
	fixture.driver.restartPersist = func(string, activeFaultFile, scenarioFaultSpec, []FaultProcessEvidence) error {
		return os.ErrPermission
	}
	if _, err := fixture.driver.Apply(t.Context(), fixture.fault); !errors.Is(err, os.ErrPermission) || calls != 0 {
		t.Fatalf("failed intent write dispatched termination: calls=%d err=%v", calls, err)
	}
	if _, err := os.Stat(fixture.driver.activePath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed intent write invented an active fault: %v", err)
	}
	if !supervisedCommandAlive(fixture.commands[0]) {
		t.Fatal("original process changed before durable intent")
	}
}

func TestProcessRestartWriteAheadCommitPrecedesEverySignal(t *testing.T) {
	fixture := newProcessRestartIntentFixture(t, 2)
	var signaled []string
	var retained []byte
	fixture.driver.restartSignal = func(command supervisedCommand, value syscall.Signal) bool {
		if value != syscall.SIGTERM {
			t.Fatalf("unexpected initial signal %v", value)
		}
		raw := fixture.retained(t)
		if retained != nil && !bytes.Equal(raw, retained) {
			t.Fatal("later dispatch rewrote the immutable restart cohort")
		}
		retained = raw
		signaled = append(signaled, command.spec.ID)
		return true
	}
	first, err := fixture.driver.Apply(t.Context(), fixture.fault)
	if err != nil || len(signaled) != len(fixture.commands) || len(first) != len(fixture.commands) {
		t.Fatalf("first restart dispatch = %+v, calls=%v, %v", first, signaled, err)
	}
	resumed := &liveScenarioFaultDriver{stateDir: fixture.driver.stateDir, restartSignal: func(supervisedCommand, syscall.Signal) bool { t.Fatal("adoption repeated termination"); return false }}
	second, err := resumed.Apply(t.Context(), fixture.fault)
	if err != nil || !reflect.DeepEqual(first, second) || !bytes.Equal(retained, fixture.retained(t)) {
		t.Fatalf("exact intent adoption changed its original evidence: %+v, %v", second, err)
	}
}

// Cancellation after commit leaves an exact authorized restart request. Resume
// never repeats termination, the supervisor can finish that original, and only
// independently observed replacement health permits removal of the marker.
func TestProcessRestartWriteAheadCanceledDispatchResumesThroughReplacement(t *testing.T) {
	fixture := newProcessRestartIntentFixture(t, 1)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	calls := 0
	fixture.driver.restartSignal = func(supervisedCommand, syscall.Signal) bool { calls++; return true }
	fixture.driver.restartPersist = func(path string, active activeFaultFile, spec scenarioFaultSpec, processes []FaultProcessEvidence) error {
		if err := appendActiveFault(path, active, spec, processes); err != nil {
			return err
		}
		cancel()
		return nil
	}
	if _, err := fixture.driver.Apply(ctx, fixture.fault); !errors.Is(err, context.Canceled) || calls != 0 {
		t.Fatalf("post-commit cancellation lost dispatch ownership: calls=%d err=%v", calls, err)
	}
	retained := fixture.retained(t)
	resumed := &liveScenarioFaultDriver{stateDir: fixture.driver.stateDir, restartSignal: func(supervisedCommand, syscall.Signal) bool { calls++; return true }}
	if _, err := resumed.Apply(t.Context(), fixture.fault); err != nil || calls != 0 || !bytes.Equal(retained, fixture.retained(t)) {
		t.Fatalf("resumed intent repeated or rewrote dispatch: calls=%d err=%v", calls, err)
	}
	if _, err := resumed.Restore(t.Context(), fixture.fault); !processRestartPending(fixture.fault, err) {
		t.Fatalf("unfulfilled restart was reported restored: %v", err)
	}
	recovery := &supervisorFaultRestartRecovery{}
	now := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	signal := func(command supervisedCommand, value syscall.Signal) bool {
		return signalSupervisedCommandWithObserver(command, value, observeSupervisedProcessIdentity, syscall.Kill)
	}
	if _, err := recovery.reconcile(t.Context(), now, fixture.driver.stateDir, fixture.commands, signal); err != nil {
		t.Fatal(err)
	}
	if escalations, err := recovery.reconcile(t.Context(), now.Add(supervisorFaultRestartGrace), fixture.driver.stateDir, fixture.commands, signal); err != nil || len(escalations) != 1 {
		t.Fatalf("supervisor could not finish committed unsent request: %+v, %v", escalations, err)
	}
	if err := <-fixture.exits[0]; err == nil {
		t.Fatal("original did not exit through bounded escalation")
	}
	if !bytes.Equal(retained, fixture.retained(t)) {
		t.Fatal("shutdown completion erased the pending replacement evidence")
	}
	replacement, _ := startRestartIntentTestProcess(t, fixture.commands[0].spec)
	fixture.state.Processes[0].PID = replacement.identity.PID
	fixture.state.Processes[0].Restarts++
	fixture.state.Processes[0].Healthy = true
	fixture.write(t)
	processes, err := resumed.Restore(t.Context(), fixture.fault)
	if err != nil || len(processes) != 1 || processes[0].PID != replacement.identity.PID {
		t.Fatalf("actual healthy replacement did not restore: %+v, %v", processes, err)
	}
	if _, err := os.Stat(resumed.activePath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("restored exact intent remains active: %v", err)
	}
}

func TestProcessRestartWriteAheadInterruptedCohortDoesNotRepeatSignals(t *testing.T) {
	fixture := newProcessRestartIntentFixture(t, 2)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	calls := 0
	fixture.driver.restartSignal = func(command supervisedCommand, value syscall.Signal) bool {
		fixture.retained(t)
		calls++
		sent := signalSupervisedCommandWithObserver(command, value, observeSupervisedProcessIdentity, syscall.Kill)
		cancel()
		return sent
	}
	if _, err := fixture.driver.Apply(ctx, fixture.fault); !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("cohort interruption crossed the next signal boundary: calls=%d err=%v", calls, err)
	}
	if err := <-fixture.exits[0]; err == nil {
		t.Fatal("first original did not receive its graceful signal")
	}
	retained := fixture.retained(t)
	resumed := &liveScenarioFaultDriver{stateDir: fixture.driver.stateDir, restartSignal: func(supervisedCommand, syscall.Signal) bool { calls++; return true }}
	if _, err := resumed.Apply(t.Context(), fixture.fault); err != nil || calls != 1 || !bytes.Equal(retained, fixture.retained(t)) {
		t.Fatalf("interrupted cohort redispatched on adoption: calls=%d err=%v", calls, err)
	}
	if !supervisedCommandAlive(fixture.commands[1]) {
		t.Fatal("unsent second original was terminated during adoption")
	}
}

// Both a refused syscall and stale/incomplete identity remain unfinished. They
// cannot authorize a second initial signal or claim replacement readiness.
func TestProcessRestartWriteAheadSignalRefusalRetainsExactIntent(t *testing.T) {
	for _, refusal := range []string{"syscall", "pid-reuse", "incomplete-image"} {
		fixture := newProcessRestartIntentFixture(t, 1)
		syscalls := 0
		fixture.driver.restartSignal = func(command supervisedCommand, value syscall.Signal) bool {
			fixture.retained(t)
			observed := command.identity
			switch refusal {
			case "pid-reuse":
				observed.StartTimeTicks++
			case "incomplete-image":
				command.identity.ExecutableFile.Inode = 0
			}
			return signalSupervisedCommandWithObserver(command, value, func(int) (supervisedProcessIdentity, error) { return observed, nil }, func(int, syscall.Signal) error { syscalls++; return syscall.EPERM })
		}
		if _, err := fixture.driver.Apply(t.Context(), fixture.fault); err != nil {
			t.Fatal(err)
		}
		wantCalls := 0
		if refusal == "syscall" {
			wantCalls = 1
		}
		if syscalls != wantCalls {
			t.Fatalf("%s signaling escaped ownership guard: calls=%d", refusal, syscalls)
		}
		retained := fixture.retained(t)
		if _, err := fixture.driver.Apply(t.Context(), fixture.fault); err != nil || syscalls != wantCalls || !bytes.Equal(retained, fixture.retained(t)) {
			t.Fatalf("%s repeated ambiguous dispatch: calls=%d err=%v", refusal, syscalls, err)
		}
		if _, err := fixture.driver.Restore(t.Context(), fixture.fault); !processRestartPending(fixture.fault, err) {
			t.Fatalf("%s manufactured completed restart: %v", refusal, err)
		}
	}
}

func TestProcessRestartWriteAheadRejectsIncompleteCohortBeforeCommit(t *testing.T) {
	for _, changed := range []string{"role", "identity", "pid", "restart-policy"} {
		fixture := newProcessRestartIntentFixture(t, 2)
		switch changed {
		case "role":
			fixture.state.Processes[1].Role = "another-role"
		case "identity":
			fixture.state.Processes[1].Identity = ""
		case "pid":
			fixture.state.Processes[1].PID = 1
		case "restart-policy":
			fixture.manifest.Specs[1].RestartLimit = 0
		}
		fixture.write(t)
		writes, signals := 0, 0
		fixture.driver.restartPersist = func(string, activeFaultFile, scenarioFaultSpec, []FaultProcessEvidence) error { writes++; return nil }
		fixture.driver.restartSignal = func(supervisedCommand, syscall.Signal) bool { signals++; return true }
		if _, err := fixture.driver.Apply(t.Context(), fixture.fault); err == nil || writes != 0 || signals != 0 {
			t.Fatalf("%s invalid later member allowed an earlier mutation: writes=%d signals=%d err=%v", changed, writes, signals, err)
		}
	}
}
