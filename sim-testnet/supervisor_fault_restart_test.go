// Fault-owned escalation is tested at the actual signaling boundary. Synthetic
// time advances grace deterministically; a readiness pipe orders the real child.
package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

type supervisorFaultRestartFixture struct {
	dir      string
	command  supervisedCommand
	fault    scenarioFaultSpec
	active   activeFaultFile
	clock    time.Time
	recovery supervisorFaultRestartRecovery
}

func newSupervisorFaultRestartFixture(t *testing.T) *supervisorFaultRestartFixture {
	t.Helper()
	identity := supervisedProcessIdentity{
		PID: 31337, ProcessGroupID: 31337, StartTimeTicks: 100,
		Executable: "/synthetic/worker", CommandLineHash: "synthetic-arguments",
		ExecutableFile: supervisedExecutableFileIdentity{Device: 7, Inode: 11},
	}
	spec := ProcessSpec{ID: "synthetic-swarm", Role: "miner-swarm", Identity: "synthetic-members", RestartLimit: 2}
	fault := scenarioFaultSpec{ID: "synthetic-rolling", Kind: "process-restart", Targets: []string{spec.ID}, DurationBlocks: 4}
	fixture := &supervisorFaultRestartFixture{
		dir: t.TempDir(), clock: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), fault: fault,
		command: supervisedCommand{spec: spec, cmd: &exec.Cmd{Process: &os.Process{Pid: identity.PID}}, identity: identity},
		active:  activeFaultFile{Schema: "urnetwork-sim-active-faults-v1", Faults: []scenarioFaultSpec{fault}, Processes: []FaultProcessEvidence{{ID: spec.ID, Role: spec.Role, Identity: spec.Identity, PID: identity.PID, StartTimeTicks: identity.StartTimeTicks}}},
	}
	fixture.write(t)
	return fixture
}

func (self *supervisorFaultRestartFixture) write(t *testing.T) {
	t.Helper()
	if err := writePublicJSON(filepath.Join(self.dir, "active-faults.json"), self.active); err != nil {
		t.Fatal(err)
	}
}

// The pipe proves SIGTERM is ignored before the actual Apply call. No elapsed
// sleep or negative timeout decides whether the original remains stuck.
func TestSupervisorFaultRestartEscalatesExactHungChild(t *testing.T) {
	dir := t.TempDir()
	cmd := exec.Command("/bin/sh", "-c", "trap '' TERM; printf 'ready\\n'; IFS= read -r control")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	reaped := false
	t.Cleanup(func() {
		if !reaped {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	if line, err := bufio.NewReader(stdout).ReadString('\n'); err != nil || line != "ready\n" {
		t.Fatalf("child readiness = %q, %v", line, err)
	}
	identity, err := observeSupervisedProcessIdentity(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	spec := ProcessSpec{ID: "synthetic-stuck-swarm", Role: "miner-swarm", Identity: "synthetic-members", RestartLimit: 2}
	manifest := SupervisorFile{Schema: "urnetwork-sim-supervisor-v1", Specs: []ProcessSpec{spec}}
	manifestHash, err := canonicalHashHex(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := writePublicJSON(filepath.Join(dir, "supervisor.json"), manifest); err != nil {
		t.Fatal(err)
	}
	state := SupervisorState{Schema: "urnetwork-sim-supervisor-state-v1", ManifestHash: manifestHash, Processes: []ProcessState{{ID: spec.ID, Role: spec.Role, Identity: spec.Identity, PID: cmd.Process.Pid}}}
	if err := writePublicJSON(filepath.Join(dir, "supervisor.state.json"), state); err != nil {
		t.Fatal(err)
	}
	fault := scenarioFaultSpec{ID: "synthetic-rolling", Kind: "process-restart", Targets: []string{spec.ID}, DurationBlocks: 4}
	driver := &liveScenarioFaultDriver{stateDir: dir}
	processes, err := driver.Apply(t.Context(), fault)
	if err != nil || len(processes) != 1 || processes[0].StartTimeTicks != identity.StartTimeTicks {
		t.Fatalf("restart did not retain exact kernel generation: %+v, %v", processes, err)
	}
	retained, err := os.ReadFile(driver.activePath())
	if err != nil {
		t.Fatal(err)
	}
	command := supervisedCommand{spec: spec, cmd: cmd, identity: identity}
	recovery := &supervisorFaultRestartRecovery{}
	now := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	signals := 0
	signal := func(command supervisedCommand, value syscall.Signal) bool {
		signals++
		if value != syscall.SIGKILL {
			t.Fatalf("unexpected escalation signal %v", value)
		}
		return signalSupervisedCommandWithObserver(command, value, observeSupervisedProcessIdentity, syscall.Kill)
	}
	for _, at := range []time.Time{now, now.Add(supervisorFaultRestartGrace - time.Nanosecond)} {
		if escalations, err := recovery.reconcile(t.Context(), at, dir, []supervisedCommand{command}, signal); err != nil || len(escalations) != 0 || signals != 0 {
			t.Fatalf("restart escalated before its own grace: %+v, %v, calls=%d", escalations, err, signals)
		}
	}
	escalations, err := recovery.reconcile(t.Context(), now.Add(supervisorFaultRestartGrace), dir, []supervisedCommand{command}, signal)
	if err != nil || len(escalations) != 1 || signals != 1 {
		t.Fatalf("hung original never received bounded escalation: %+v, %v, calls=%d", escalations, err, signals)
	}
	var exit *exec.ExitError
	if err := cmd.Wait(); !errors.As(err, &exit) || !exit.Sys().(syscall.WaitStatus).Signaled() || exit.Sys().(syscall.WaitStatus).Signal() != syscall.SIGKILL {
		t.Fatalf("hung original did not exit through exact escalation: %v", err)
	}
	reaped = true
	if _, err := driver.Restore(t.Context(), fault); !processRestartPending(fault, err) {
		t.Fatalf("killing the original manufactured replacement readiness: %v", err)
	}
	after, err := os.ReadFile(driver.activePath())
	if err != nil || !bytes.Equal(retained, after) {
		t.Fatalf("escalation rewrote fault evidence: %v", err)
	}
}

// Legacy receipts, reused pids and replacements cannot receive the original's
// authority. The same kernel guard rejects image/argv changes at signal time.
func TestSupervisorFaultRestartRejectsForeignOrLegacyGeneration(t *testing.T) {
	for _, changed := range []string{"legacy", "replacement", "pid-reuse", "image", "arguments", "unobservable"} {
		fixture := newSupervisorFaultRestartFixture(t)
		observed := fixture.command.identity
		var observeErr error
		switch changed {
		case "legacy":
			fixture.active.Processes[0].StartTimeTicks = 0
		case "replacement":
			fixture.command.cmd.Process.Pid++
			fixture.command.identity.PID++
			fixture.command.identity.ProcessGroupID++
		case "pid-reuse":
			fixture.command.identity.StartTimeTicks++
		case "image":
			observed.ExecutableFile.Inode++
		case "arguments":
			observed.CommandLineHash = "another-command"
		case "unobservable":
			observeErr = syscall.EACCES
		}
		fixture.write(t)
		calls := 0
		signal := func(command supervisedCommand, value syscall.Signal) bool {
			return signalSupervisedCommandWithObserver(command, value, func(int) (supervisedProcessIdentity, error) { return observed, observeErr }, func(int, syscall.Signal) error { calls++; return nil })
		}
		for _, at := range []time.Time{fixture.clock, fixture.clock.Add(2 * supervisorFaultRestartGrace)} {
			if escalations, err := fixture.recovery.reconcile(t.Context(), at, fixture.dir, []supervisedCommand{fixture.command}, signal); err != nil || len(escalations) != 0 || calls != 0 {
				t.Fatalf("%s signaled an unrelated generation: %+v, %v, calls=%d", changed, escalations, err, calls)
			}
		}
	}
}

// Matching the retained pid/start pair does not make an incomplete in-memory
// owner a valid signal target. Reject it before creating a grace deadline.
func TestSupervisorFaultRestartRejectsIncompleteKernelOwner(t *testing.T) {
	for _, changed := range []string{"image", "arguments", "group"} {
		fixture := newSupervisorFaultRestartFixture(t)
		switch changed {
		case "image":
			fixture.command.identity.ExecutableFile.Inode = 0
		case "arguments":
			fixture.command.identity.CommandLineHash = ""
		case "group":
			fixture.command.identity.ProcessGroupID++
		}
		calls := 0
		_, err := fixture.recovery.reconcile(t.Context(), fixture.clock, fixture.dir, []supervisedCommand{fixture.command}, func(supervisedCommand, syscall.Signal) bool { calls++; return true })
		if err == nil || calls != 0 || len(fixture.recovery.pendingProcessIdKVs) != 0 {
			t.Fatalf("%s incomplete owner acquired a signal deadline: calls=%d pending=%+v err=%v", changed, calls, fixture.recovery.pendingProcessIdKVs, err)
		}
	}
}

// A different immutable fault, or a gap with no fault, starts its own grace.
func TestSupervisorFaultRestartCannotInheritRemovedOrChangedDeadline(t *testing.T) {
	for _, removed := range []bool{false, true} {
		fixture := newSupervisorFaultRestartFixture(t)
		calls := 0
		signal := func(supervisedCommand, syscall.Signal) bool { calls++; return true }
		if _, err := fixture.recovery.reconcile(t.Context(), fixture.clock, fixture.dir, []supervisedCommand{fixture.command}, signal); err != nil {
			t.Fatal(err)
		}
		if removed {
			if err := os.Remove(filepath.Join(fixture.dir, "active-faults.json")); err != nil {
				t.Fatal(err)
			}
			if _, err := fixture.recovery.reconcile(t.Context(), fixture.clock.Add(supervisorFaultRestartGrace), fixture.dir, []supervisedCommand{fixture.command}, signal); err != nil {
				t.Fatal(err)
			}
		} else {
			fixture.active.Faults[0].DurationBlocks++
		}
		fixture.write(t)
		reappeared := fixture.clock.Add(2 * supervisorFaultRestartGrace)
		if escalations, err := fixture.recovery.reconcile(t.Context(), reappeared, fixture.dir, []supervisedCommand{fixture.command}, signal); err != nil || len(escalations) != 0 || calls != 0 {
			t.Fatalf("new fault inherited old deadline: %+v, %v, calls=%d", escalations, err, calls)
		}
		if escalations, err := fixture.recovery.reconcile(t.Context(), reappeared.Add(supervisorFaultRestartGrace), fixture.dir, []supervisedCommand{fixture.command}, signal); err != nil || len(escalations) != 1 || calls != 1 {
			t.Fatalf("new fault did not get its own bounded grace: %+v, %v, calls=%d", escalations, err, calls)
		}
	}
}

// Validate every retained target before the first signal, even if an earlier
// target is already due. Corrupt state stays diagnostic and never grants a kill.
func TestSupervisorFaultRestartValidatesWholeCohortBeforeSignal(t *testing.T) {
	fixture := newSupervisorFaultRestartFixture(t)
	second := fixture.command
	second.spec.ID = "synthetic-second"
	second.identity.PID++
	second.identity.ProcessGroupID++
	second.cmd = &exec.Cmd{Process: &os.Process{Pid: second.identity.PID}}
	fixture.active.Faults[0].Targets = append(fixture.active.Faults[0].Targets, second.spec.ID)
	original := fixture.active.Processes[0]
	original.ID = second.spec.ID
	original.PID = second.identity.PID
	fixture.active.Processes = append(fixture.active.Processes, original)
	fixture.write(t)
	commands := []supervisedCommand{fixture.command, second}
	calls := 0
	signal := func(supervisedCommand, syscall.Signal) bool { calls++; return true }
	if _, err := fixture.recovery.reconcile(t.Context(), fixture.clock, fixture.dir, commands, signal); err != nil {
		t.Fatal(err)
	}
	fixture.active.Processes[1].Identity = "another-owner"
	fixture.write(t)
	if _, err := fixture.recovery.reconcile(t.Context(), fixture.clock.Add(supervisorFaultRestartGrace), fixture.dir, commands, signal); err == nil || calls != 0 {
		t.Fatalf("later owner mismatch allowed an earlier kill: calls=%d err=%v", calls, err)
	}
	if err := os.WriteFile(filepath.Join(fixture.dir, "active-faults.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.recovery.reconcile(t.Context(), fixture.clock.Add(2*supervisorFaultRestartGrace), fixture.dir, commands, signal); err == nil || calls != 0 {
		t.Fatalf("malformed ledger authorized a kill: calls=%d err=%v", calls, err)
	}
}

// Failed signaling can be retried after a fresh identity check. Success emits
// one retained diagnostic; owner cancellation forbids any subsequent attempt.
func TestSupervisorFaultRestartRetriesSignalsAndHonorsOwnerCancellation(t *testing.T) {
	fixture := newSupervisorFaultRestartFixture(t)
	calls := 0
	signal := func(supervisedCommand, syscall.Signal) bool { calls++; return calls > 1 }
	commands := []supervisedCommand{fixture.command}
	if _, err := fixture.recovery.reconcile(t.Context(), fixture.clock, fixture.dir, commands, signal); err != nil {
		t.Fatal(err)
	}
	for index, count := range []int{0, 1, 0} {
		escalations, err := fixture.recovery.reconcile(t.Context(), fixture.clock.Add(time.Duration(index+1)*supervisorFaultRestartGrace), fixture.dir, commands, signal)
		if err != nil || len(escalations) != count {
			t.Fatalf("signal retry %d: %+v, %v", index, escalations, err)
		}
	}
	if calls != 2 {
		t.Fatalf("successful escalation repeated: calls=%d", calls)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := fixture.recovery.reconcile(ctx, fixture.clock.Add(5*supervisorFaultRestartGrace), fixture.dir, commands, signal); !errors.Is(err, context.Canceled) || calls != 2 {
		t.Fatalf("canceled owner authorized signaling: calls=%d err=%v", calls, err)
	}
}
