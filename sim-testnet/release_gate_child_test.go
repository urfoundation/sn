package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// An actual forked grandchild changes its process group/session, announces its
// PID, and blocks at an explicit barrier. No process timing supplies the proof.
const releaseGateDescendantFixture = `import ctypes, os, signal, sys
from pathlib import Path
root = Path(sys.argv[1])
mode = sys.argv[2]
(root / "worker-tree").write_text(os.environ["RELEASE_GATE_WORKER_PID"])
child = os.fork()
if child == 0:
    if mode == "session": os.setsid()
    else: os.setpgrp()
    if mode == "nonutf8":
        ctypes.CDLL(None).prctl(15, ctypes.c_char_p(b"\xffgate-child"), 0, 0, 0)
    def stopped(number, frame):
        (root / "stopped").write_text(str(os.getpid()))
        os._exit(0)
    signal.signal(signal.SIGTERM, stopped)
    with (root / "events").open("w") as events:
        events.write(str(os.getpid()) + "\n")
        events.flush()
    with (root / "leaf-control").open() as control: action = control.readline().strip()
    os._exit(0 if action == "release" else int(action))
with (root / "control").open() as control: action = control.readline().strip()
if action == "join":
    os.waitpid(child, 0)
    os._exit(0)
os._exit(int(action))
`

type releaseGateChildFixture struct {
	root     string
	command  *exec.Cmd
	output   bytes.Buffer
	files    map[string]*os.File
	lines    map[string]chan string
	readers  sync.WaitGroup
	finished chan struct{}
	err      error
}

// Owns the test subprocess and every FIFO-reader goroutine through cleanup,
// including a failed assertion before the release/acknowledgement barriers.
func newReleaseGateChildFixture(t *testing.T, function string) *releaseGateChildFixture {
	t.Helper()
	self := &releaseGateChildFixture{
		root: t.TempDir(), files: map[string]*os.File{}, lines: map[string]chan string{}, finished: make(chan struct{}),
	}
	constructed := false
	defer func() {
		if !constructed {
			for _, file := range self.files {
				_ = file.Close()
			}
			self.readers.Wait()
		}
	}()
	for _, directory := range []string{"job-0", "job-0/tmp", "job-0/gotmp"} {
		if err := os.Mkdir(filepath.Join(self.root, directory), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"events", "control", "leaf-control", "completions", "job-0/ack"} {
		path := filepath.Join(self.root, name)
		if err := syscall.Mkfifo(path, 0o600); err != nil {
			t.Fatal(err)
		}
		file, err := os.OpenFile(path, os.O_RDWR, 0)
		if err != nil {
			t.Fatal(err)
		}
		self.files[name] = file
		if name == "events" || name == "completions" {
			lines := make(chan string, 8)
			self.lines[name] = lines
			self.readers.Add(1)
			go func() {
				defer self.readers.Done()
				defer close(lines)
				scanner := bufio.NewScanner(file)
				if scanner.Scan() {
					lines <- scanner.Text()
				}
			}()
		}
	}
	fixture := filepath.Join(self.root, "descendant.py")
	if err := os.WriteFile(fixture, []byte(releaseGateDescendantFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	owner, err := filepath.Abs("../scripts/release-gate-child.py")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	self.command = exec.CommandContext(ctx, "bash", "-c", "phase() { "+function+"; }; export -f phase; exec python3 \"$1\" \"$2\" 0 phase", "release-child-test", owner, self.root)
	self.command.Env = append(os.Environ(), "RELEASE_GATE_FIXTURE_ROOT="+self.root)
	self.command.Cancel = func() error { return self.command.Process.Signal(syscall.SIGTERM) }
	self.command.WaitDelay = 15 * time.Second
	self.command.Stdout, self.command.Stderr = &self.output, &self.output
	if err := self.command.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	go func() { self.err = self.command.Wait(); close(self.finished) }()
	t.Cleanup(func() {
		_ = self.command.Process.Signal(syscall.SIGCONT)
		cancel()
		// Controls are owned by this test, so even an assertion failure releases
		// fixtures instead of stranding a process in a test-only FIFO read.
		for _, pair := range [][2]string{{"control", "23\n"}, {"leaf-control", "release\n"}, {"job-0/ack", "joined\n"}} {
			_, _ = self.files[pair[0]].WriteString(pair[1])
		}
		select {
		case <-self.finished:
		case <-time.After(20 * time.Second):
			t.Error("release child fixture did not join after cancellation")
			_ = self.command.Process.Kill()
			<-self.finished
		}
		for _, file := range self.files {
			_ = file.Close()
		}
		self.readers.Wait()
	})
	constructed = true
	return self
}

func (self *releaseGateChildFixture) line(t *testing.T, name string) string {
	t.Helper()
	select {
	case line, ok := <-self.lines[name]:
		if !ok {
			t.Fatalf("%s reader closed", name)
		}
		return line
	case <-self.finished:
		t.Fatalf("child owner exited before %s barrier: %v\n%s", name, self.err, self.output.String())
	case <-time.After(20 * time.Second):
		t.Fatalf("child owner did not reach %s barrier", name)
	}
	return ""
}

func (self *releaseGateChildFixture) write(t *testing.T, name, value string) {
	t.Helper()
	if _, err := self.files[name].WriteString(value + "\n"); err != nil {
		t.Fatal(err)
	}
}

func (self *releaseGateChildFixture) completed(t *testing.T, expected int, acknowledgement string, actualExit int) {
	t.Helper()
	if got := self.line(t, "completions"); got != fmt.Sprintf("0 %d", expected) {
		t.Fatalf("completion = %q, want 0 %d", got, expected)
	}
	self.write(t, "job-0/ack", acknowledgement)
	select {
	case <-self.finished:
	case <-time.After(20 * time.Second):
		t.Fatal("acknowledged child owner did not join")
	}
	if self.command.ProcessState.ExitCode() != actualExit {
		t.Fatalf("owner exit=%d error=%v, want %d\n%s", self.command.ProcessState.ExitCode(), self.err, actualExit, self.output.String())
	}
}

func assertReleaseGateDescendantReaped(t *testing.T, root, pid string) {
	t.Helper()
	if _, err := strconv.Atoi(pid); err != nil {
		t.Fatalf("invalid descendant PID %q", pid)
	}
	if _, err := os.Stat(filepath.Join("/proc", pid)); !os.IsNotExist(err) {
		t.Fatalf("descendant %s was not reaped: %v", pid, err)
	}
	stopped, err := os.ReadFile(filepath.Join(root, "stopped"))
	if err != nil || string(stopped) != pid {
		t.Fatalf("descendant TERM acknowledgement = %q, %v, want %s", stopped, err, pid)
	}
}

func releaseGateEscapedChild(t *testing.T, mode string) (*releaseGateChildFixture, string) {
	t.Helper()
	self := newReleaseGateChildFixture(t, `python3 "$RELEASE_GATE_FIXTURE_ROOT/descendant.py" "$RELEASE_GATE_FIXTURE_ROOT" `+mode)
	return self, self.line(t, "events")
}

func TestReleaseGateChildReapsNewProcessGroupAfterWorkerFailure(t *testing.T) {
	self, pid := releaseGateEscapedChild(t, "group")
	self.write(t, "control", "23")
	self.completed(t, 23, "joined", 23)
	assertReleaseGateDescendantReaped(t, self.root, pid)
}

func TestReleaseGateChildReapsNewSessionAfterWorkerFailure(t *testing.T) {
	self, pid := releaseGateEscapedChild(t, "session")
	self.write(t, "control", "23")
	self.completed(t, 23, "joined", 23)
	assertReleaseGateDescendantReaped(t, self.root, pid)
}

func TestReleaseGateChildHandlesNonUTF8ProcessNames(t *testing.T) {
	self, pid := releaseGateEscapedChild(t, "nonutf8")
	self.write(t, "control", "23")
	self.completed(t, 23, "joined", 23)
	assertReleaseGateDescendantReaped(t, self.root, pid)
}

func TestReleaseGateChildCancellationJoinsEscapedGrandchild(t *testing.T) {
	self, pid := releaseGateEscapedChild(t, "session")
	if err := self.command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	self.completed(t, 143, "joined", 143)
	assertReleaseGateDescendantReaped(t, self.root, pid)
}

func TestReleaseGateChildFailureDoesNotSignalConcurrentOwner(t *testing.T) {
	first, firstPID := releaseGateEscapedChild(t, "session")
	second, secondPID := releaseGateEscapedChild(t, "group")
	first.write(t, "control", "23")
	first.completed(t, 23, "joined", 23)
	assertReleaseGateDescendantReaped(t, first.root, firstPID)
	// This positive live-child observation follows the first owner's complete
	// cancellation/reap. No timeout or negative scheduling inference is needed.
	pid, err := strconv.Atoi(secondPID)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatalf("first owner affected second descendant: %v", err)
	}
	if _, err := os.Stat(filepath.Join(second.root, "stopped")); !os.IsNotExist(err) {
		t.Fatalf("second owner was signaled: %v", err)
	}
	second.write(t, "control", "23")
	second.completed(t, 23, "joined", 23)
	assertReleaseGateDescendantReaped(t, second.root, secondPID)
}

func TestReleaseGateChildRejectsSuccessfulPhaseWithUnjoinedDescendant(t *testing.T) {
	self, pid := releaseGateEscapedChild(t, "session")
	self.write(t, "control", "0")
	self.completed(t, 125, "joined", 125)
	assertReleaseGateDescendantReaped(t, self.root, pid)
}

func TestReleaseGateChildNormalJoinedDescendantPasses(t *testing.T) {
	self, pid := releaseGateEscapedChild(t, "session")
	self.write(t, "leaf-control", "release")
	self.write(t, "control", "join")
	self.completed(t, 0, "joined", 0)
	if _, err := os.Stat(filepath.Join("/proc", pid)); !os.IsNotExist(err) {
		t.Fatalf("normally joined child remains: %v", err)
	}
}

func TestReleaseGateChildRealExitRejectsMalformedAcknowledgement(t *testing.T) {
	self := newReleaseGateChildFixture(t, "true")
	self.completed(t, 0, "wrong-owner", 125)
}

func TestReleaseGateChildPreservesWorkerFailureStatus(t *testing.T) {
	self := newReleaseGateChildFixture(t, "return 37")
	self.completed(t, 37, "joined", 37)
}

// Stop the owner at a barrier while both worker and adopted orphan exit. The
// next wait drain sees both statuses and ECHILD together; neither may be lost.
func TestReleaseGateChildRejectsAlreadyExitedAdoptedOrphan(t *testing.T) {
	self, pid := releaseGateEscapedChild(t, "session")
	if err := self.command.Process.Signal(syscall.SIGSTOP); err != nil {
		t.Fatal(err)
	}
	stopDeadline := time.Now().Add(20 * time.Second)
	for {
		data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", self.command.Process.Pid))
		if err != nil {
			t.Fatal(err)
		}
		fields := string(data)[strings.LastIndex(string(data), ") ")+2:]
		if strings.HasPrefix(fields, "T ") {
			break
		}
		if time.Now().After(stopDeadline) {
			t.Fatal("owner never acknowledged the kernel stopped state")
		}
		runtime.Gosched()
	}
	workerBytes, err := os.ReadFile(filepath.Join(self.root, "worker-tree"))
	if err != nil {
		t.Fatal(err)
	}
	self.write(t, "leaf-control", "1")
	self.write(t, "control", "0")
	deadline := time.Now().Add(20 * time.Second)
	for _, process := range []string{pid, string(workerBytes)} {
		for {
			data, err := os.ReadFile(filepath.Join("/proc", process, "stat"))
			if err != nil {
				t.Fatalf("stopped-owner child disappeared before wait: %s: %v", process, err)
			}
			_, fields, ok := strings.Cut(string(data), ") ")
			if ok && strings.HasPrefix(fields, "Z ") {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("child %s did not exit at the stopped-owner barrier", process)
			}
			runtime.Gosched()
		}
	}
	if err := self.command.Process.Signal(syscall.SIGCONT); err != nil {
		t.Fatal(err)
	}
	self.completed(t, 125, "joined", 125)
	if _, err := os.Stat(filepath.Join("/proc", pid)); !os.IsNotExist(err) {
		t.Fatalf("already-exited orphan was not reaped: %v", err)
	}
}

func TestReleaseGateChildCancellationAfterCompletionHasRealFailingExit(t *testing.T) {
	self := newReleaseGateChildFixture(t, "true")
	if got := self.line(t, "completions"); got != "0 0" {
		t.Fatalf("completion = %q", got)
	}
	if err := self.command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	self.write(t, "job-0/ack", "joined")
	select {
	case <-self.finished:
	case <-time.After(20 * time.Second):
		t.Fatal("canceled acknowledged owner did not join")
	}
	if self.command.ProcessState.ExitCode() != 143 {
		t.Fatalf("late cancellation was hidden: %v\n%s", self.err, self.output.String())
	}
}

// Static coupling complements real child tests: the completion boundary is
// kernel ECHILD, never a shell session-only enumeration or a detached daemon.
func TestReleaseGateChildRequiresSubreaperAndPinnedSignals(t *testing.T) {
	data, err := os.ReadFile("../scripts/release-gate-child.py")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"((36, 1), (1, signal.SIGTERM))", "os.waitpid(-1, os.WNOHANG)", "except ChildProcessError:", "os.pidfd_open(pid)", "signal.pidfd_send_signal(descriptor, selected_signal)"} {
		if !strings.Contains(string(data), required) {
			t.Errorf("phase child ownership omits %q", required)
		}
	}
}
