package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

type releaseGateJobsFixture struct {
	root       string
	command    *exec.Cmd
	output     bytes.Buffer
	files      map[string]*os.File
	lines      map[string]chan string
	readers    sync.WaitGroup
	readerStop chan struct{}
	finished   chan struct{}
	err        error
}

// Uses the actual queue and subreaper with only machine capacity and the
// external service cleanup callback supplied as deterministic boundaries.
func newReleaseGateJobsFixture(t *testing.T, body string) *releaseGateJobsFixture {
	t.Helper()
	self := &releaseGateJobsFixture{root: t.TempDir(), files: map[string]*os.File{}, lines: map[string]chan string{}, readerStop: make(chan struct{}), finished: make(chan struct{})}
	constructed := false
	defer func() {
		if !constructed {
			close(self.readerStop)
			for _, file := range self.files {
				_ = file.Close()
			}
			self.readers.Wait()
		}
	}()
	for _, name := range []string{"events", "control-a", "control-b", "control-c", "control", "leaf-control", "ack-relay"} {
		path := filepath.Join(self.root, name)
		if err := syscall.Mkfifo(path, 0o600); err != nil {
			t.Fatal(err)
		}
		file, err := os.OpenFile(path, os.O_RDWR, 0)
		if err != nil {
			t.Fatal(err)
		}
		self.files[name] = file
		if name == "events" || name == "ack-relay" {
			lines := make(chan string, 8)
			self.lines[name] = lines
			self.readers.Add(1)
			go func() {
				defer self.readers.Done()
				defer close(lines)
				scanner := bufio.NewScanner(file)
				for scanner.Scan() {
					select {
					case lines <- scanner.Text():
					case <-self.readerStop:
						return
					}
				}
			}()
		}
	}
	workspace := filepath.Join(self.root, "workspace")
	if err := os.MkdirAll(filepath.Join(workspace, "server", "local"), 0o700); err != nil {
		t.Fatal(err)
	}
	cleanup := `release_gate_services_cleanup() {
  if [[ -f "$RELEASE_GATE_QUEUE_FIXTURE_ROOT/descendant-pid" ]]; then
    local pid
    pid="$(< "$RELEASE_GATE_QUEUE_FIXTURE_ROOT/descendant-pid")"
    [[ ! -e "/proc/$pid" ]] || return 95
  fi
  printf 'cleaned\n' > "$RELEASE_GATE_QUEUE_FIXTURE_ROOT/cleaned"
}
`
	if err := os.WriteFile(filepath.Join(workspace, "server", "local", "release-gate-services.sh"), []byte(cleanup), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(self.root, "descendant.py"), []byte(releaseGateDescendantFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	helper, err := filepath.Abs("../scripts/release-gate-jobs.sh")
	if err != nil {
		t.Fatal(err)
	}
	sn, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	self.command = exec.CommandContext(ctx, "bash", "-c", `set -euo pipefail
source "$1"
sn_repo="$2"; workspace="$3"
release_gate_effective_resources() { RELEASE_GATE_CPUS=24; RELEASE_GATE_MEMORY=34359738368; }
release_gate_jobs_init
`+body, "release-jobs-test", helper, sn, workspace)
	self.command.Env = append(os.Environ(), "TMPDIR="+self.root, "RELEASE_GATE_QUEUE_FIXTURE_ROOT="+self.root, "RELEASE_GATE_JOBS=2", "RELEASE_GATE_CONCURRENT_GATES=2", "SIM_TESTNET_LIVE_DEPENDENCIES=0")
	self.command.Cancel = func() error { return self.command.Process.Signal(syscall.SIGTERM) }
	self.command.WaitDelay = 20 * time.Second
	self.command.Stdout, self.command.Stderr = &self.output, &self.output
	if err := self.command.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	go func() { self.err = self.command.Wait(); close(self.finished) }()
	t.Cleanup(func() {
		cancel()
		for _, name := range []string{"control-a", "control-b", "control-c", "control", "leaf-control"} {
			_, _ = self.files[name].WriteString("release\n")
		}
		select {
		case <-self.finished:
		case <-time.After(25 * time.Second):
			t.Error("foreground queue did not join after cancellation")
			_ = self.command.Process.Kill()
			<-self.finished
		}
		close(self.readerStop)
		for _, file := range self.files {
			_ = file.Close()
		}
		self.readers.Wait()
	})
	constructed = true
	return self
}

func (self *releaseGateJobsFixture) line(t *testing.T, name string) string {
	t.Helper()
	select {
	case line, ok := <-self.lines[name]:
		if !ok {
			t.Fatalf("queue %s reader closed", name)
		}
		return line
	case <-self.finished:
		t.Fatalf("queue exited before %s barrier: %v\n%s", name, self.err, self.output.String())
	case <-time.After(20 * time.Second):
		t.Fatalf("queue did not reach %s barrier", name)
	}
	return ""
}

func (self *releaseGateJobsFixture) write(t *testing.T, name, value string) {
	t.Helper()
	if _, err := self.files[name].WriteString(value + "\n"); err != nil {
		t.Fatal(err)
	}
}

func (self *releaseGateJobsFixture) join(t *testing.T, status int) {
	t.Helper()
	select {
	case <-self.finished:
	case <-time.After(20 * time.Second):
		t.Fatal("queue did not join")
	}
	if self.command.ProcessState.ExitCode() != status {
		t.Fatalf("queue exit=%d want=%d error=%v\n%s", self.command.ProcessState.ExitCode(), status, self.err, self.output.String())
	}
}

func TestReleaseGateJobsRunReadyPhasesConcurrentlyAndJoinFailures(t *testing.T) {
	self := newReleaseGateJobsFixture(t, `
phase_a() { printf 'a\n' > "$RELEASE_GATE_QUEUE_FIXTURE_ROOT/events"; read -r release < "$RELEASE_GATE_QUEUE_FIXTURE_ROOT/control-a"; return 23; }
phase_b() { printf 'b\n' > "$RELEASE_GATE_QUEUE_FIXTURE_ROOT/events"; read -r release < "$RELEASE_GATE_QUEUE_FIXTURE_ROOT/control-b"; }
release_gate_start a phase_a
release_gate_start b phase_b
release_gate_wait_one
[[ "$release_gate_result" == 23 && "$release_gate_active" == 1 ]]
printf 'first-joined\n' > "$RELEASE_GATE_QUEUE_FIXTURE_ROOT/events"
status=0; release_gate_complete || status=$?
[[ "$release_gate_active" == 0 && "$release_gate_services_cleaned" == 1 && "$status" == 23 ]]
exit "$status"
`)
	first, second := self.line(t, "events"), self.line(t, "events")
	if first+second != "ab" && first+second != "ba" {
		t.Fatalf("two ready workers = %q %q", first, second)
	}
	self.write(t, "control-a", "release")
	if got := self.line(t, "events"); got != "first-joined" {
		t.Fatalf("failure join boundary = %q", got)
	}
	self.write(t, "control-b", "release")
	self.join(t, 23)
	if _, err := os.Stat(filepath.Join(self.root, "cleaned")); err != nil {
		t.Fatalf("failed but joined phases omitted resource cleanup: %v", err)
	}
}

func TestReleaseGateJobsBoundReadyAdmissionByEffectiveCapacity(t *testing.T) {
	self := newReleaseGateJobsFixture(t, `
phase_a() { printf 'a\n' > "$RELEASE_GATE_QUEUE_FIXTURE_ROOT/events"; read -r release < "$RELEASE_GATE_QUEUE_FIXTURE_ROOT/control-a"; }
phase_b() { printf 'b\n' > "$RELEASE_GATE_QUEUE_FIXTURE_ROOT/events"; read -r release < "$RELEASE_GATE_QUEUE_FIXTURE_ROOT/control-b"; }
phase_c() { printf 'c\n' > "$RELEASE_GATE_QUEUE_FIXTURE_ROOT/events"; read -r release < "$RELEASE_GATE_QUEUE_FIXTURE_ROOT/control-c"; }
release_gate_start a phase_a
release_gate_start b phase_b
original_wait="$(declare -f release_gate_wait_one)"
eval "${original_wait/release_gate_wait_one/release_gate_wait_one_real}"
release_gate_wait_one() {
  [[ "$release_gate_active" == 2 ]]
  printf 'capacity-wait\n' > "$RELEASE_GATE_QUEUE_FIXTURE_ROOT/events"
  release_gate_wait_one_real
}
release_gate_start c phase_c
eval "$original_wait"
release_gate_complete
`)
	seen := map[string]bool{}
	for range 3 {
		seen[self.line(t, "events")] = true
	}
	if !seen["a"] || !seen["b"] || !seen["capacity-wait"] {
		t.Fatalf("capacity barrier entries=%v", seen)
	}
	self.write(t, "control-a", "release")
	if got := self.line(t, "events"); got != "c" {
		t.Fatalf("next admitted worker=%q", got)
	}
	self.write(t, "control-b", "release")
	self.write(t, "control-c", "release")
	self.join(t, 0)
}

func TestReleaseGateJobsCancellationReapsNewSessionBeforeServiceCleanup(t *testing.T) {
	self := newReleaseGateJobsFixture(t, `
phase_tree() { python3 "$RELEASE_GATE_QUEUE_FIXTURE_ROOT/descendant.py" "$RELEASE_GATE_QUEUE_FIXTURE_ROOT" session; }
release_gate_start tree phase_tree
read -r hold < "$RELEASE_GATE_QUEUE_FIXTURE_ROOT/control-a"
`)
	pid := self.line(t, "events")
	if err := os.WriteFile(filepath.Join(self.root, "descendant-pid"), []byte(pid), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := self.command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	self.join(t, 143)
	assertReleaseGateDescendantReaped(t, self.root, pid)
	if _, err := os.Stat(filepath.Join(self.root, "cleaned")); err != nil {
		t.Fatalf("canceled jobs were not reaped before cleanup: %v", err)
	}
}

// Relay the queue's acknowledgement through an explicit test boundary. The
// worker completion remains zero, but the real owner rejects the wrong token.
func TestReleaseGateJobsPreserveRealPostCompletionFailure(t *testing.T) {
	self := newReleaseGateJobsFixture(t, `
phase_ok() { true; }
release_gate_start ok phase_ok
original_ack="${release_gate_ack_fds[0]}"
exec {relay_ack}<>"$RELEASE_GATE_QUEUE_FIXTURE_ROOT/ack-relay"
release_gate_ack_fds[0]="$relay_ack"
printf '%s\n' "$release_gate_root" > "$RELEASE_GATE_QUEUE_FIXTURE_ROOT/events"
release_gate_wait_one
exec {original_ack}>&-
[[ "$release_gate_result" == 125 && "$RELEASE_GATE_JOB_EXIT" == 125 && "$release_gate_active" == 0 ]]
status=0; release_gate_complete || status=$?
exit "$status"
`)
	root := self.line(t, "events")
	acknowledgement, err := os.OpenFile(filepath.Join(root, "job-0", "ack"), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = acknowledgement.WriteString("joined\n"); _ = acknowledgement.Close() })
	if got := self.line(t, "ack-relay"); got != "joined" {
		t.Fatalf("queue acknowledgement=%q", got)
	}
	if _, err := acknowledgement.WriteString("wrong-owner\n"); err != nil {
		t.Fatal(err)
	}
	self.join(t, 125)
	if !strings.Contains(self.output.String(), "worker/owner exit mismatch: 0/125") {
		t.Fatalf("real owner failure lost attribution:\n%s", self.output.String())
	}
}

func TestReleaseGateJobsUsePrivateArtifactsCachesAndSourceReferences(t *testing.T) {
	self := newReleaseGateJobsFixture(t, `
[[ "$FOUNDRY_OUT" != "$SLITHER_FOUNDRY_OUT" && "$FOUNDRY_CACHE_PATH" != "$SLITHER_FOUNDRY_CACHE_PATH" ]]
[[ "$(readlink "$release_gate_root/src")" == "$sn_repo/evm/src" ]]
[[ "$(readlink "$release_gate_root/lib")" == "$sn_repo/evm/lib" ]]
[[ "${FOUNDRY_OUT%/*}" == "$release_gate_root" && "${SLITHER_FOUNDRY_OUT%/*}" == "$release_gate_root" ]]
[[ "$release_gate_limit" == 2 && "$GOMAXPROCS" == 4 && "$GOFLAGS" == *-p=4 ]]
release_gate_complete
`)
	self.join(t, 0)
}

func TestReleaseGateJobsTwoForegroundOwnersHaveDisjointMutableRoots(t *testing.T) {
	body := `
phase_hold() { printf '%s\n' "$release_gate_root" > "$RELEASE_GATE_QUEUE_FIXTURE_ROOT/events"; read -r release < "$RELEASE_GATE_QUEUE_FIXTURE_ROOT/control-a"; }
release_gate_start hold phase_hold
release_gate_complete
`
	first, second := newReleaseGateJobsFixture(t, body), newReleaseGateJobsFixture(t, body)
	firstRoot, secondRoot := first.line(t, "events"), second.line(t, "events")
	if firstRoot == secondRoot || filepath.Dir(firstRoot) != first.root || filepath.Dir(secondRoot) != second.root {
		t.Fatalf("independent gate roots=%q %q", firstRoot, secondRoot)
	}
	first.write(t, "control-a", "release")
	first.join(t, 0)
	if _, err := os.Stat(secondRoot); err != nil {
		t.Fatalf("first owner damaged second root: %v", err)
	}
	second.write(t, "control-a", "release")
	second.join(t, 0)
}

func assertReleaseGateEarlyExit(t *testing.T, parentStatus, expected int) {
	t.Helper()
	self := newReleaseGateJobsFixture(t, `
phase_hold() { printf 'held\n' > "$RELEASE_GATE_QUEUE_FIXTURE_ROOT/events"; read -r release < "$RELEASE_GATE_QUEUE_FIXTURE_ROOT/control-a"; }
release_gate_start hold phase_hold
read -r leave < "$RELEASE_GATE_QUEUE_FIXTURE_ROOT/control-b"
exit `+strconv.Itoa(parentStatus)+"\n")
	if got := self.line(t, "events"); got != "held" {
		t.Fatalf("early-exit worker barrier=%q", got)
	}
	self.write(t, "control-b", "leave")
	self.join(t, expected)
	if _, err := os.Stat(filepath.Join(self.root, "cleaned")); err != nil {
		t.Fatalf("early exit failed to join before cleanup: %v", err)
	}
}

func TestReleaseGateJobsExitCannotHideCanceledWorkerFailure(t *testing.T) {
	assertReleaseGateEarlyExit(t, 0, 143)
}

func TestReleaseGateJobsExitPreservesOriginalParentFailure(t *testing.T) {
	assertReleaseGateEarlyExit(t, 17, 17)
}

// Lose the completion endpoint only after admission. The real owner still
// reaps its worker, but cannot publish proof, so services must remain untouched.
func TestReleaseGateJobsLostOwnerRetainsServices(t *testing.T) {
	self := newReleaseGateJobsFixture(t, `
phase_hold() { printf 'held:%s\n' "$RELEASE_GATE_WORKER_PID" > "$RELEASE_GATE_QUEUE_FIXTURE_ROOT/events"; read -r release < "$RELEASE_GATE_QUEUE_FIXTURE_ROOT/control-a"; }
release_gate_start hold phase_hold
printf 'root:%s\n' "$release_gate_root" > "$RELEASE_GATE_QUEUE_FIXTURE_ROOT/events"
read -r continue < "$RELEASE_GATE_QUEUE_FIXTURE_ROOT/control-b"
if release_gate_complete; then exit 90; fi
[[ "$release_gate_active" == 1 ]]
exit 1
`)
	root, worker := "", ""
	for range 2 {
		line := self.line(t, "events")
		if strings.HasPrefix(line, "root:") {
			root = strings.TrimPrefix(line, "root:")
		} else if strings.HasPrefix(line, "held:") {
			worker = strings.TrimPrefix(line, "held:")
		} else {
			t.Fatalf("unexpected admission event=%q", line)
		}
	}
	if root == "" {
		t.Fatal("missing admitted owner root")
	}
	if pid, err := strconv.Atoi(worker); err != nil || pid <= 0 {
		t.Fatalf("invalid admitted worker PID=%q: %v", worker, err)
	}
	if err := os.Rename(filepath.Join(root, "completions"), filepath.Join(root, "unlinked-completions")); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(root, "completions"), 0o600); err != nil {
		t.Fatal(err)
	}
	self.write(t, "control-a", "release")
	self.write(t, "control-b", "continue")
	self.join(t, 1)
	if _, err := os.Stat(filepath.Join("/proc", worker)); !os.IsNotExist(err) {
		t.Fatalf("lost completion owner did not reap worker %s: %v", worker, err)
	}
	if _, err := os.Stat(filepath.Join(self.root, "cleaned")); !os.IsNotExist(err) {
		t.Fatalf("lost owner permitted service mutation: %v", err)
	}
	if !strings.Contains(self.output.String(), "private services retained") {
		t.Fatalf("lost owner did not report unproven cleanup:\n%s", self.output.String())
	}
}

// A real failed completion must not suppress a held peer's actual ack/wait.
// Either owner order and both incoming exit statuses retain the unproven slot.
func TestReleaseGateJobsExitJoinsSurvivorAfterLostOwner(t *testing.T) {
	for _, lostIndex := range []int{0, 1} {
		for _, parentStatus := range []int{0, 17} {
			self := newReleaseGateJobsFixture(t, `
phase_lost() { printf 'lost-worker:%s\n' "$RELEASE_GATE_WORKER_PID" > "$RELEASE_GATE_QUEUE_FIXTURE_ROOT/events"; read -r release < "$RELEASE_GATE_QUEUE_FIXTURE_ROOT/control-a"; }
phase_survivor() { printf 'survivor-worker:%s\n' "$RELEASE_GATE_WORKER_PID" > "$RELEASE_GATE_QUEUE_FIXTURE_ROOT/events"; read -r release < "$RELEASE_GATE_QUEUE_FIXTURE_ROOT/control-b"; }
lost_index=`+strconv.Itoa(lostIndex)+`
survivor_index=$((1 - lost_index))
if (( lost_index == 0 )); then
  release_gate_start lost phase_lost
  release_gate_start survivor phase_survivor
else
  release_gate_start survivor phase_survivor
  release_gate_start lost phase_lost
fi
printf 'root:%s\n' "$release_gate_root" > "$RELEASE_GATE_QUEUE_FIXTURE_ROOT/events"
printf 'owners:%s:%s\n' "${release_gate_pids[lost_index]}" "${release_gate_pids[survivor_index]}" > "$RELEASE_GATE_QUEUE_FIXTURE_ROOT/events"
read -r endpoint_lost < "$RELEASE_GATE_QUEUE_FIXTURE_ROOT/control-c"
lost_status=0
wait "${release_gate_pids[lost_index]}" || lost_status=$?
[[ "$lost_status" == 1 && "$release_gate_active" == 2 && "${release_gate_pending[lost_index]}" == 1 ]]
printf 'lost-owner-reaped\n' > "$RELEASE_GATE_QUEUE_FIXTURE_ROOT/events"
read -r endpoint_restored < "$RELEASE_GATE_QUEUE_FIXTURE_ROOT/control-c"

# Observe the real EXIT boundary before compensating cleanup. The compensation
# keeps this regression safe on the old queue: it cannot change the captured
# pending slots, parent status, or missing production join acknowledgement.
fixture_exiting=0
original_exit="$(declare -f release_gate_jobs_exit)"
eval "${original_exit/release_gate_jobs_exit/release_gate_jobs_exit_real}"
release_gate_jobs_exit() {
  fixture_exiting=1
  release_gate_jobs_exit_real "$@"
}
exit() {
  local status="${1:-0}"
  if (( fixture_exiting == 1 )); then
    printf '%s %s %s %s\n' "$release_gate_active" "${release_gate_pending[lost_index]}" "${release_gate_pending[survivor_index]}" "$status" > "$RELEASE_GATE_QUEUE_FIXTURE_ROOT/exit-state"
    while [[ "${release_gate_pending[survivor_index]}" == 1 ]] && release_gate_owned_job "$survivor_index"; do
      release_gate_wait_one || :
    done
  fi
  builtin exit "$status"
}
exit `+strconv.Itoa(parentStatus)+"\n")
			admitted := map[string]string{}
			for range 4 {
				line := self.line(t, "events")
				name, value, ok := strings.Cut(line, ":")
				if !ok || admitted[name] != "" {
					t.Fatalf("unexpected admission event=%q", line)
				}
				admitted[name] = value
			}
			root := admitted["root"]
			if filepath.Dir(root) != self.root {
				t.Fatalf("unexpected admitted owner root=%q", root)
			}
			lostOwner, survivorOwner, ok := strings.Cut(admitted["owners"], ":")
			if !ok {
				t.Fatalf("missing actual owner identities=%q", admitted["owners"])
			}
			for _, pid := range []string{lostOwner, survivorOwner, admitted["lost-worker"], admitted["survivor-worker"]} {
				if value, err := strconv.Atoi(pid); err != nil || value <= 0 {
					t.Fatalf("invalid admitted PID=%q: %v", pid, err)
				}
			}
			completion := filepath.Join(root, "completions")
			retainedCompletion := filepath.Join(root, "unlinked-completions")
			if err := os.Rename(completion, retainedCompletion); err != nil {
				t.Fatal(err)
			}
			restoreCompletion := func() error {
				if _, err := os.Stat(retainedCompletion); os.IsNotExist(err) {
					return nil
				} else if err != nil {
					return err
				}
				return os.Rename(retainedCompletion, completion)
			}
			t.Cleanup(func() {
				if err := restoreCompletion(); err != nil {
					t.Errorf("restore owned completion before fixture cleanup: %v", err)
				}
			})
			if err := syscall.Mkfifo(completion, 0o600); err != nil {
				t.Fatal(err)
			}
			self.write(t, "control-a", "release")
			self.write(t, "control-c", "endpoint-lost")
			if got := self.line(t, "events"); got != "lost-owner-reaped" {
				t.Fatalf("lost-owner wait boundary=%q", got)
			}
			for _, pid := range []string{lostOwner, admitted["lost-worker"]} {
				if _, err := os.Stat(filepath.Join("/proc", pid)); !os.IsNotExist(err) {
					t.Fatalf("lost owner failed its actual wait/reap for %s: %v", pid, err)
				}
			}
			for _, pid := range []string{survivorOwner, admitted["survivor-worker"]} {
				value, _ := strconv.Atoi(pid)
				if err := syscall.Kill(value, 0); err != nil {
					t.Fatalf("lost completion affected the independently held survivor %s: %v", pid, err)
				}
			}
			if err := restoreCompletion(); err != nil {
				t.Fatal(err)
			}
			self.write(t, "control-c", "endpoint-restored")
			select {
			case <-self.finished:
			case <-time.After(20 * time.Second):
				t.Fatal("EXIT did not finish the surviving owner's explicit join")
			}
			state, err := os.ReadFile(filepath.Join(self.root, "exit-state"))
			if err != nil {
				t.Fatal(err)
			}
			wantStatus := parentStatus
			if wantStatus == 0 {
				wantStatus = 143
			}
			wantState := "1 1 0 " + strconv.Itoa(wantStatus) + "\n"
			if string(state) != wantState {
				t.Fatalf("lost owner suppressed surviving phase join (lost index=%d, parent=%d): exit state=%q want=%q\n%s", lostIndex, parentStatus, state, wantState, self.output.String())
			}
			self.join(t, wantStatus)
			for _, pid := range []string{survivorOwner, admitted["survivor-worker"]} {
				if _, err := os.Stat(filepath.Join("/proc", pid)); !os.IsNotExist(err) {
					t.Fatalf("surviving owner was not explicitly reaped before EXIT: %s: %v", pid, err)
				}
			}
			if _, err := os.Stat(filepath.Join(self.root, "cleaned")); !os.IsNotExist(err) {
				t.Fatalf("unproven pending owner permitted service mutation: %v", err)
			}
			if !strings.Contains(self.output.String(), "joined survivor (exit 143)") || !strings.Contains(self.output.String(), "private services retained") {
				t.Fatalf("survivor join or unproven cleanup lost attribution:\n%s", self.output.String())
			}
		}
	}
}

// Execute the actual final-fence source from both gates, with only external
// Git/Go/source-snapshot commands replaced by finite recording boundaries.
func TestReleaseGateJobsFailedPhaseStillRunsEveryFinalSourceFence(t *testing.T) {
	for _, name := range []string{"test-release-1.0-local.sh", "test-release-1.0-producer-gate.sh"} {
		data, err := os.ReadFile(filepath.Join("..", "scripts", name))
		if err != nil {
			t.Fatal(err)
		}
		start := strings.Index(string(data), "# Join every admitted phase before inspecting the source again")
		if start < 0 {
			t.Fatalf("%s final owned-fence boundary is missing", name)
		}
		body := `
phase_fail() { return 23; }
release_gate_start failed phase_fail
go() { printf 'go %s\n' "$*" >> "$RELEASE_GATE_QUEUE_FIXTURE_ROOT/fence-events"; }
git() { printf 'git %s\n' "$*" >> "$RELEASE_GATE_QUEUE_FIXTURE_ROOT/fence-events"; }
sn_repo="$RELEASE_GATE_QUEUE_FIXTURE_ROOT/fence-sn"
release_repos=(sn)
release_source_snapshot=frozen
printf 'fence-ready\n' > "$RELEASE_GATE_QUEUE_FIXTURE_ROOT/events"
read -r continue < "$RELEASE_GATE_QUEUE_FIXTURE_ROOT/control-a"
` + string(data)[start:]
		self := newReleaseGateJobsFixture(t, body)
		if got := self.line(t, "events"); got != "fence-ready" {
			t.Fatalf("fence preparation=%q", got)
		}
		scripts := filepath.Join(self.root, "fence-sn", "scripts")
		if err := os.MkdirAll(scripts, 0o700); err != nil {
			t.Fatal(err)
		}
		checker := "#!/bin/sh\nprintf 'snapshot\\n' >> \"$RELEASE_GATE_QUEUE_FIXTURE_ROOT/fence-events\"\nprintf 'frozen\\n'\n"
		if err := os.WriteFile(filepath.Join(scripts, "check-release-source-freeze.sh"), []byte(checker), 0o700); err != nil {
			t.Fatal(err)
		}
		self.write(t, "control-a", "continue")
		self.join(t, 23)
		events, err := os.ReadFile(filepath.Join(self.root, "fence-events"))
		if err != nil {
			t.Fatal(err)
		}
		gitCount, goCount, snapshotCount := 0, 0, 0
		for _, line := range strings.Split(string(events), "\n") {
			if strings.HasPrefix(line, "git ") {
				gitCount++
			}
			if strings.HasPrefix(line, "go ") {
				goCount++
			}
			if line == "snapshot" {
				snapshotCount++
			}
		}
		wantGo := 1
		if strings.Contains(name, "producer") {
			wantGo = 2
		}
		if gitCount != 2 || goCount != wantGo || snapshotCount != 1 {
			t.Fatalf("%s lost final fences after worker23:\n%s", name, events)
		}
		if strings.Contains(self.output.String(), "gate passed") {
			t.Fatalf("%s hid failed phase behind passing fences", name)
		}
	}
}

func TestReleaseGateJobsEarlierSourceFenceFailureSurvivesLaterSuccess(t *testing.T) {
	data, err := os.ReadFile("../scripts/test-release-1.0-producer-gate.sh")
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(string(data), "# Join every admitted phase before inspecting the source again")
	if start < 0 {
		t.Fatal("missing producer final-fence boundary")
	}
	self := newReleaseGateJobsFixture(t, `
phase_ok() { true; }
release_gate_start ok phase_ok
go() {
  printf 'go %s\n' "$*" >> "$RELEASE_GATE_QUEUE_FIXTURE_ROOT/fence-events"
  [[ "$*" != *TestReleaseGatesPinProviderAndTransportRegressions* ]]
}
git() { :; }
sn_repo="$RELEASE_GATE_QUEUE_FIXTURE_ROOT/fence-sn"
release_repos=(sn)
release_source_snapshot=frozen
printf 'fence-ready\n' > "$RELEASE_GATE_QUEUE_FIXTURE_ROOT/events"
read -r continue < "$RELEASE_GATE_QUEUE_FIXTURE_ROOT/control-a"
`+string(data)[start:])
	if got := self.line(t, "events"); got != "fence-ready" {
		t.Fatalf("fence preparation=%q", got)
	}
	scripts := filepath.Join(self.root, "fence-sn", "scripts")
	if err := os.MkdirAll(scripts, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scripts, "check-release-source-freeze.sh"), []byte("#!/bin/sh\nprintf 'frozen\\n'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	self.write(t, "control-a", "continue")
	self.join(t, 1)
	events, err := os.ReadFile(filepath.Join(self.root, "fence-events"))
	if err != nil || !strings.Contains(string(events), "TestReleaseLockMatchesCheckout") {
		t.Fatalf("later source fence did not run: %v\n%s", err, events)
	}
	if strings.Contains(self.output.String(), "gate passed") {
		t.Fatalf("later source success hid the earlier failure:\n%s", self.output.String())
	}
}

func TestReleaseGateIsolationPinsPrivateResourcesAndFinalJoins(t *testing.T) {
	for _, name := range []string{"test-release-1.0-local.sh", "test-release-1.0-producer-gate.sh"} {
		data, err := os.ReadFile(filepath.Join("..", "scripts", name))
		if err != nil {
			t.Fatal(err)
		}
		script := string(data)
		for _, required := range []string{`source "$sn_repo/scripts/release-gate-jobs.sh"`, "release_gate_jobs_init", `source "$RELEASE_GATE_SERVICE_ENV"`, `source "$workspace/server/test-env.sh"`, "test_env_validate_suite_resource_manifest", `--check "$FOUNDRY_OUT" sim-testnet/contracts_gen.go`, `--check --artifacts "$FOUNDRY_OUT"`, "release_gate_complete || release_gate_status=$?", "release_gate_active != 0", "release_gate_fence_status"} {
			if !strings.Contains(script, required) {
				t.Errorf("%s omits private/owned boundary %q", name, required)
			}
		}
		for _, forbidden := range []string{"local-pg.bringyour.com", "local-redis.bringyour.com", "--check evm/out", "& disown"} {
			if strings.Contains(script, forbidden) {
				t.Errorf("%s retains shared/detached resource %q", name, forbidden)
			}
		}
		join := strings.LastIndex(script, "release_gate_complete")
		fence := strings.LastIndex(script, "final source-freeze checkout")
		if join < 0 || fence <= join {
			t.Errorf("%s final source fence precedes owned joins", name)
		}
		start := strings.Index(script, "release_phase_solidity() {")
		if start < 0 {
			t.Fatalf("%s lacks one full-build/generator owner", name)
		}
		end := strings.Index(script[start:], "\n}")
		if end < 0 {
			t.Fatal("unterminated Solidity phase")
		}
		phase := script[start : start+end]
		build, payload, binding := strings.Index(phase, "forge build"), strings.Index(phase, "gencontracts --check"), strings.Index(phase, "stabi/generate.sh")
		if build < 0 || payload <= build || binding <= payload {
			t.Errorf("%s breaks full build/payload/binding dependency", name)
		}
	}
}

// Reuse the gate source guards' exact phase/admission grammar. Require actual
// complete commands and owned working directories, with no extra shell branch
// that could skip execution.
// The simulator's five complementary owners retain its complete race census.
const releaseGateSimulatorPopulationSelector = "^(TestCampaignEvidenceCapacityV2MetadataFullCensusMaterializesFlatWireAndCarrier|TestCampaignEvidencePopulationV2StreamsPhaseCensusWithBoundedOwners)$"
const releaseGateSimulatorSupplementSelector = "^(TestFinalSemanticSupplementFailedReplicaDoesNotCommitAndRetryReusesStage|TestFinalSemanticSupplementPublishesResumesAndRejectsLooseTamper|TestValidateFinalSemanticSupplementDefaultCapturedStoresRequiresEveryReplica)$"
const releaseGateSimulatorSeparateSelector = "^(TestCampaignEvidenceCapacityV2MetadataFullCensusMaterializesFlatWireAndCarrier|TestCampaignEvidencePopulationV2StreamsPhaseCensusWithBoundedOwners|TestFinalSemanticSupplementFailedReplicaDoesNotCommitAndRetryReusesStage|TestFinalSemanticSupplementPublishesResumesAndRejectsLooseTamper|TestValidateFinalSemanticSupplementDefaultCapturedStoresRequiresEveryReplica)$"
const releaseGateSimulatorFinalSelector = "^TestFinal.*$"
const releaseGateSimulatorHistorySelector = "^Test(Fleet|Historical|Runtime|Scenario).*$"
const releaseGateSimulatorPartitionedSelector = "^(TestCampaignEvidenceCapacityV2MetadataFullCensusMaterializesFlatWireAndCarrier|TestCampaignEvidencePopulationV2StreamsPhaseCensusWithBoundedOwners|TestFinalSemanticSupplementFailedReplicaDoesNotCommitAndRetryReusesStage|TestFinalSemanticSupplementPublishesResumesAndRejectsLooseTamper|TestValidateFinalSemanticSupplementDefaultCapturedStoresRequiresEveryReplica|TestFinal.*|Test(Fleet|Historical|Runtime|Scenario).*)$"
const releaseGateSimulatorRaceCommand = "go test -race -parallel=4 -timeout 90m ./sim-testnet -count=1"
const releaseGateSimulatorOrdinaryRaceCommand = releaseGateSimulatorRaceCommand + " -skip '" + releaseGateSimulatorPartitionedSelector + "'"
const releaseGateSimulatorPopulationRaceCommand = releaseGateSimulatorRaceCommand + " -run '" + releaseGateSimulatorPopulationSelector + "'"
const releaseGateSimulatorSupplementRaceCommand = releaseGateSimulatorRaceCommand + " -run '" + releaseGateSimulatorSupplementSelector + "'"
const releaseGateSimulatorFinalRaceCommand = releaseGateSimulatorRaceCommand + " -run '" + releaseGateSimulatorFinalSelector + "' -skip '" + releaseGateSimulatorSeparateSelector + "'"
const releaseGateSimulatorHistoryRaceCommand = releaseGateSimulatorRaceCommand + " -run '" + releaseGateSimulatorHistorySelector + "'"

func verifyReleaseGateFullValidatorRace(script string) error {
	phaseDefinitions := regexp.MustCompile(`(?ms)^[\t ]*release_phase_[a-z0-9_]+\(\) \{\n.*?^[\t ]*\}[\t ]*$`)
	registry := phaseDefinitions.ReplaceAllString(script, "")
	groups := []struct {
		phase   string
		job     string
		command string
	}{
		{phase: "sn_all_normal", job: "sn-all-normal", command: "go test -parallel=4 -timeout 90m ./... -count=1"},
		{phase: "sn_core_race", job: "sn-core-race", command: "go test -race ./crv4 ./miner/... ./protocol -count=1"},
		{phase: "sn_validator_race", job: "sn-validator-race", command: "go test -race -parallel=4 -timeout 90m ./validator -count=1"},
		{phase: "sn_simulator_race", job: "sn-simulator-race", command: releaseGateSimulatorOrdinaryRaceCommand},
		{phase: "sn_simulator_populations_race", job: "sn-simulator-populations-race", command: releaseGateSimulatorPopulationRaceCommand},
		{phase: "sn_simulator_supplements_race", job: "sn-simulator-supplements-race", command: releaseGateSimulatorSupplementRaceCommand},
		{phase: "sn_simulator_final_race", job: "sn-simulator-final-race", command: releaseGateSimulatorFinalRaceCommand},
		{phase: "sn_simulator_history_race", job: "sn-simulator-history-race", command: releaseGateSimulatorHistoryRaceCommand},
	}
	for _, group := range groups {
		function := "release_phase_" + group.phase
		pattern := regexp.MustCompile("(?ms)^[\\t ]*" + regexp.QuoteMeta(function) + "\\(\\) \\{\\n(.*?)^[\\t ]*\\}[\\t ]*$")
		definitions := pattern.FindAllStringSubmatch(script, -1)
		if len(definitions) != 1 {
			return fmt.Errorf("full gate requires exactly one %s definition", function)
		}
		var commands []string
		for _, line := range strings.Split(definitions[0][1], "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				commands = append(commands, line)
			}
		}
		if len(commands) != 2 || commands[0] != `cd "$sn_repo"` || commands[1] != group.command {
			return fmt.Errorf("full gate phase %s changed its complete command or scoped budget", group.phase)
		}
		start := "release_gate_start " + group.job + " " + function
		invocations := regexp.MustCompile("(?m)^" + regexp.QuoteMeta(start) + "[\\t ]*$")
		calls := invocations.FindAllStringIndex(script, -1)
		definition := pattern.FindStringIndex(script)
		if len(calls) != 1 || len(invocations.FindAllString(registry, -1)) != 1 || calls[0][0] < definition[1] {
			return fmt.Errorf("full gate does not independently admit %s", group.phase)
		}
		conditions, err := releaseGateRegistrationConditions(script, start)
		if err != nil || len(conditions) != 0 {
			return fmt.Errorf("full gate has conditional job admission: %v %v", conditions, err)
		}
	}
	return nil
}

// The inherited combined core race command allowed less time than the measured
// passing serial work alone. The dedicated full-validator allowance leaves
// the other core packages on their original budgets.
func TestReleaseGateJobsRequireIndependentCompleteValidatorRace(t *testing.T) {
	t.Parallel()
	encoded, err := os.ReadFile("../scripts/test-release-1.0-local.sh")
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyReleaseGateFullValidatorRace(string(encoded)); err != nil {
		t.Fatalf("full validator race phase is not independently budgeted: %v", err)
	}
}

// Reproduce the exhausted single-package clock's original ownership, then
// reject omissions, overlaps, hidden owners and changes to any allowance.
func TestReleaseGateJobsRejectSimulatorPopulationPartitionDrift(t *testing.T) {
	t.Parallel()
	encoded, err := os.ReadFile("../scripts/test-release-1.0-local.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(encoded)
	sources := map[string]string{"population_test.go": "package main\nimport \"testing\"\n" +
		"func TestCampaignEvidenceCapacityV2MetadataFullCensusMaterializesFlatWireAndCarrier(t *testing.T) {}\n" +
		"func TestCampaignEvidencePopulationV2StreamsPhaseCensusWithBoundedOwners(t *testing.T) {}\n" +
		"func TestCampaignEvidencePopulationV2StreamsPhaseCensusWithBoundedOwnersFuture(t *testing.T) {}\n" +
		"func TestFinalSemanticSupplementFailedReplicaDoesNotCommitAndRetryReusesStage(t *testing.T) {}\n" +
		"func TestFinalSemanticSupplementPublishesResumesAndRejectsLooseTamper(t *testing.T) {}\n" +
		"func TestValidateFinalSemanticSupplementDefaultCapturedStoresRequiresEveryReplica(t *testing.T) {}\n" +
		"func TestFinalSemanticSupplementPublishesResumesAndRejectsLooseTamperFuture(t *testing.T) {}\n" +
		"func TestRuntimeFutureControl(t *testing.T) {}\n" +
		"func TestOrdinaryControl(t *testing.T) {}\n"}
	if err := verifyReleaseGateSimulatorRaceCensus(script, sources); err != nil {
		t.Fatal(err)
	}
	const start = "release_gate_start sn-simulator-populations-race release_phase_sn_simulator_populations_race"
	const definition = "release_phase_sn_simulator_populations_race() {\n  cd \"$sn_repo\"\n  " + releaseGateSimulatorPopulationRaceCommand + "\n}"
	const supplementStart = "release_gate_start sn-simulator-supplements-race release_phase_sn_simulator_supplements_race"
	const supplementDefinition = "release_phase_sn_simulator_supplements_race() {\n  cd \"$sn_repo\"\n  " + releaseGateSimulatorSupplementRaceCommand + "\n}"
	previous := strings.Replace(script, releaseGateSimulatorOrdinaryRaceCommand, releaseGateSimulatorRaceCommand+" -skip '"+releaseGateSimulatorPopulationSelector+"'", 1)
	previous = strings.Replace(previous, supplementDefinition, "", 1)
	previous = strings.Replace(previous, supplementStart, "", 1)
	if err := verifyReleaseGateSimulatorRaceCensus(previous, sources); err == nil {
		t.Fatal("aggregate restored the timed-out supplement/complement package clock")
	}
	old := strings.Replace(script, releaseGateSimulatorOrdinaryRaceCommand, releaseGateSimulatorRaceCommand, 1)
	old = strings.Replace(old, definition, "", 1)
	old = strings.Replace(old, start, "", 1)
	if err := verifyReleaseGateSimulatorRaceCensus(old, sources); err == nil {
		t.Fatal("aggregate restored the serial population in one exhausted package clock")
	}
	for _, command := range []string{releaseGateSimulatorOrdinaryRaceCommand, releaseGateSimulatorPopulationRaceCommand, releaseGateSimulatorSupplementRaceCommand, releaseGateSimulatorFinalRaceCommand, releaseGateSimulatorHistoryRaceCommand} {
		for _, replacement := range []string{
			releaseGateSimulatorRaceCommand,
			strings.Replace(command, " -race", "", 1),
			strings.Replace(command, " -count=1", "", 1),
			strings.Replace(command, "-parallel=4", "-parallel=8", 1),
			strings.Replace(command, "-timeout 90m", "-timeout 180m", 1),
			strings.Replace(command, "-timeout 90m", "-timeout 45m", 1),
			strings.Replace(command, "-timeout 90m", "-timeout 0", 1),
			strings.Replace(command, ")$'", ")'", 1),
			strings.Replace(command, "TestCampaignEvidenceCapacityV2MetadataFullCensusMaterializesFlatWireAndCarrier|", "", 1),
			strings.Replace(command, "TestFinalSemanticSupplementFailedReplicaDoesNotCommitAndRetryReusesStage|", "", 1),
			strings.Replace(command, "TestFinalSemanticSupplementPublishesResumesAndRejectsLooseTamper", "TestFinalSemanticSupplementPublishesResumesAndRejectsLooseTamper.*", 1),
			strings.Replace(command, "-skip", "-run", 1),
			strings.Replace(command, "-run", "-skip", 1),
			"# " + command,
			command + " -run '^$'",
			command + " || true",
			command + "\n  " + command,
		} {
			if replacement == command {
				continue
			}
			if strings.Count(script, command) != 1 {
				t.Fatal("mutation does not identify one simulator owner command")
			}
			if err := verifyReleaseGateSimulatorRaceCensus(strings.Replace(script, command, replacement, 1), sources); err == nil {
				t.Fatalf("aggregate accepted changed simulator execution: %s", replacement)
			}
		}
	}
	for _, admission := range []string{start, supplementStart, "release_gate_start sn-simulator-race release_phase_sn_simulator_race", "release_gate_start sn-simulator-final-race release_phase_sn_simulator_final_race", "release_gate_start sn-simulator-history-race release_phase_sn_simulator_history_race"} {
		for _, replacement := range []string{
			"# " + admission,
			admission + "\n" + admission,
			"if false; then\n" + admission + "\nfi",
			"release_phase_unused() {\n" + admission + "\n}",
		} {
			if err := verifyReleaseGateSimulatorRaceCensus(strings.Replace(script, admission, replacement, 1), sources); err == nil {
				t.Fatalf("aggregate accepted changed simulator admission: %s", replacement)
			}
		}
	}
	for _, omitted := range []string{
		"func TestCampaignEvidenceCapacityV2MetadataFullCensusMaterializesFlatWireAndCarrier(t *testing.T) {}\n",
		"func TestCampaignEvidencePopulationV2StreamsPhaseCensusWithBoundedOwners(t *testing.T) {}\n",
		"func TestFinalSemanticSupplementFailedReplicaDoesNotCommitAndRetryReusesStage(t *testing.T) {}\n",
		"func TestFinalSemanticSupplementPublishesResumesAndRejectsLooseTamper(t *testing.T) {}\n",
		"func TestValidateFinalSemanticSupplementDefaultCapturedStoresRequiresEveryReplica(t *testing.T) {}\n",
	} {
		changed := map[string]string{"population_test.go": strings.Replace(sources["population_test.go"], omitted, "", 1)}
		if err := verifyReleaseGateSimulatorRaceCensus(script, changed); err == nil {
			t.Fatal("aggregate separate owner admitted a missing required root")
		}
	}
}

// The complete node module owns future Subtensor regressions too. The exact
// shared gateway methods add their security assertions without making unrelated
// Grafana/edge/backup repositories part of the SN release source inventory.
func verifyReleaseGateSubtensorInfrastructureScope(script string) error {
	const function = "release_phase_xops"
	pattern := regexp.MustCompile(`(?ms)^release_phase_xops\(\) \{\n(.*?)^\}[\t ]*$`)
	definitions := pattern.FindAllStringSubmatch(script, -1)
	if len(definitions) != 1 {
		return fmt.Errorf("SN infrastructure requires one xops owner")
	}
	var commands []string
	for _, line := range strings.Split(definitions[0][1], "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			commands = append(commands, line)
		}
	}
	expected := []string{
		`cd "$workspace/xops"`,
		`python3 -m unittest \`,
		`main/ansible/tests/test_subtensor_playbook.py \`,
		`main.ansible.tests.test_vulnscan2_resolved.Vulnscan2ResolvedInfrastructureTests.test_vs2_011_subtensor_local_rpc_and_restricted_gateway_render \`,
		`main.ansible.tests.test_vulnscan2_resolved.Vulnscan2ResolvedInfrastructureTests.test_vs2_011_unpaced_gateway_rejects_request_and_connection_quotas`,
	}
	if len(commands) != len(expected) {
		return fmt.Errorf("SN infrastructure changed its complete node/gateway command")
	}
	for index, command := range expected {
		if commands[index] != command {
			return fmt.Errorf("SN infrastructure command %d differs from its node/gateway scope", index)
		}
	}
	const start = "release_gate_start xops " + function
	invocation := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(start) + `[\t ]*$`)
	phaseDefinitions := regexp.MustCompile(`(?ms)^[\t ]*release_phase_[a-z0-9_]+\(\) \{\n.*?^[\t ]*\}[\t ]*$`)
	registry := phaseDefinitions.ReplaceAllString(script, "")
	calls := invocation.FindAllStringIndex(script, -1)
	definition := pattern.FindStringIndex(script)
	if len(calls) != 1 || len(invocation.FindAllString(registry, -1)) != 1 || calls[0][0] < definition[1] {
		return fmt.Errorf("SN infrastructure has no unique admitted owner")
	}
	conditions, err := releaseGateRegistrationConditions(script, start)
	if err != nil || len(conditions) != 0 {
		return fmt.Errorf("SN infrastructure admission is conditional: %v %v", conditions, err)
	}
	return nil
}

// Reproduce the whole-repository scan that required absent Warp/Grafana source,
// while rejecting the adjacent mistake of omitting the actual gateway check.
func TestReleaseGateJobsRejectSubtensorInfrastructureScopeDrift(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../scripts/test-release-1.0-local.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(raw)
	if err := verifyReleaseGateSubtensorInfrastructureScope(script); err != nil {
		t.Fatal(err)
	}
	const gateway = "main.ansible.tests.test_vulnscan2_resolved.Vulnscan2ResolvedInfrastructureTests.test_vs2_011_subtensor_local_rpc_and_restricted_gateway_render"
	const unpaced = "main.ansible.tests.test_vulnscan2_resolved.Vulnscan2ResolvedInfrastructureTests.test_vs2_011_unpaced_gateway_rejects_request_and_connection_quotas"
	const node = "main/ansible/tests/test_subtensor_playbook.py"
	const start = "release_gate_start xops release_phase_xops"
	for _, change := range []struct{ old, replacement string }{
		{gateway, "main/ansible/tests/test_vulnscan2_resolved.py"},
		{gateway, ""},
		{gateway, gateway + " || true"},
		{gateway, gateway + " main/ansible/tests/test_vulnscan2_resolved.py"},
		{old: unpaced, replacement: ""},
		{old: unpaced, replacement: unpaced + " " + unpaced},
		{old: unpaced, replacement: unpaced + " || true"},
		{old: unpaced, replacement: "main/ansible/tests/test_vulnscan2_resolved.py"},
		{node, ""},
		{node, "main.ansible.tests.test_subtensor_playbook.SubtensorPlaybookTests.test_gateway_binds_and_verifies_every_restricted_management_address"},
		{"python3 -m unittest", "# python3 -m unittest"},
		{start, "# " + start},
		{start, start + "\n" + start},
		{start, "if false; then\n" + start + "\nfi"},
		{start, "release_phase_unused() {\n" + start + "\n}"},
	} {
		if strings.Count(script, change.old) != 1 {
			t.Fatalf("scope mutation is ambiguous: %s", change.old)
		}
		if err := verifyReleaseGateSubtensorInfrastructureScope(strings.Replace(script, change.old, change.replacement, 1)); err == nil {
			t.Fatalf("SN infrastructure accepted changed scope: %s", change.replacement)
		}
	}
}

// Commented, narrowed, disconnected or duplicated text cannot certify the
// actual phase; a validator allowance cannot silently fund other core packages.
func TestReleaseGateJobsRejectCompleteValidatorRaceBudgetOmissions(t *testing.T) {
	t.Parallel()
	encoded, err := os.ReadFile("../scripts/test-release-1.0-local.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(encoded)
	if err := verifyReleaseGateFullValidatorRace(script); err != nil {
		t.Fatal(err)
	}
	const command = "go test -race -parallel=4 -timeout 90m ./validator -count=1"
	const core = "go test -race ./crv4 ./miner/... ./protocol -count=1"
	const start = "release_gate_start sn-validator-race release_phase_sn_validator_race"
	const definition = "release_phase_sn_validator_race() {\n  cd \"$sn_repo\"\n  " + command + "\n}"
	cases := []struct {
		name        string
		old         string
		replacement string
	}{
		{name: "implicit deadline", old: command, replacement: "go test -race -parallel=4 ./validator -count=1"},
		{name: "inherited short deadline", old: command, replacement: "go test -race -parallel=4 -timeout 10m ./validator -count=1"},
		{name: "race omitted", old: command, replacement: "go test -parallel=4 -timeout 90m ./validator -count=1"},
		{name: "reduced workers", old: command, replacement: "go test -race -parallel=2 -timeout 90m ./validator -count=1"},
		{name: "cached execution", old: command, replacement: strings.Replace(command, " -count=1", "", 1)},
		{name: "no test bodies", old: command, replacement: strings.Replace(command, "-count=1", "-count=0", 1)},
		{name: "compile only", old: command, replacement: command + " -run '^$'"},
		{name: "narrowed population", old: command, replacement: command + " -run '^TestIntent'"},
		{name: "commented command", old: command, replacement: "# " + command},
		{name: "hidden failure", old: command, replacement: command + " || true"},
		{name: "unreachable command", old: command, replacement: "if false; then\n  " + command + "\n  fi"},
		{name: "different source", old: definition, replacement: strings.Replace(definition, `cd "$sn_repo"`, `cd "$workspace/server"`, 1)},
		{name: "lost core runtime", old: core, replacement: "go test -race ./miner/... ./protocol -count=1"},
		{name: "lost core miner", old: core, replacement: "go test -race ./crv4 ./protocol -count=1"},
		{name: "lost core protocol", old: core, replacement: "go test -race ./crv4 ./miner/... -count=1"},
		{name: "core budget leak", old: core, replacement: core + " -timeout 90m"},
		{name: "combined core owner", old: core, replacement: core + " ./validator"},
		{name: "commented admission", old: start, replacement: "# " + start},
		{name: "wrong admitted owner", old: start, replacement: "release_gate_start sn-validator-race release_phase_sn_core_race"},
		{name: "nested admission", old: start, replacement: "release_phase_unused() {\n" + start + "\n}"},
		{name: "unreachable admission", old: start, replacement: "if false; then\n" + start + "\nfi"},
		{name: "empty loop admission", old: start, replacement: "for omitted in; do\n" + start + "\ndone"},
		{name: "duplicate admission", old: start, replacement: start + "\n" + start},
		{name: "duplicate definition", old: definition, replacement: definition + "\n" + definition},
	}
	for _, testCase := range cases {
		if strings.Count(script, testCase.old) != 1 {
			t.Fatalf("mutation %s does not identify one original boundary", testCase.name)
		}
		changed := strings.Replace(script, testCase.old, testCase.replacement, 1)
		if err := verifyReleaseGateFullValidatorRace(changed); err == nil {
			t.Fatalf("full validator gate accepted %s", testCase.name)
		}
	}
	early := strings.Replace(script, start, "# admission moved before definition", 1)
	early = strings.Replace(early, definition, start+"\n"+definition, 1)
	if err := verifyReleaseGateFullValidatorRace(early); err == nil {
		t.Fatal("full validator gate admitted an undefined phase")
	}
}

// These previously cacheable bodies are pinned to their actual phase and
// selector. Compile-only package checks are separate and never count as bodies.
var releaseGateUncachedCommands = []struct {
	phase   string
	command string
}{
	{phase: "sn_all_normal", command: "go test -parallel=4 -timeout 90m ./... -count=1"},
	{phase: "sn_core_race", command: "go test -race ./crv4 ./miner/... ./protocol -count=1"},
	{phase: "sn_validator_race", command: "go test -race -parallel=4 -timeout 90m ./validator -count=1"},
	{phase: "sn_simulator_race", command: releaseGateSimulatorOrdinaryRaceCommand},
	{phase: "sn_simulator_populations_race", command: releaseGateSimulatorPopulationRaceCommand},
	{phase: "sn_simulator_supplements_race", command: releaseGateSimulatorSupplementRaceCommand},
	{phase: "sn_simulator_final_race", command: releaseGateSimulatorFinalRaceCommand},
	{phase: "sn_simulator_history_race", command: releaseGateSimulatorHistoryRaceCommand},
	{phase: "server_unit", command: "go test . -run '^Test(PgResourcesRedirectMaintenancePoolAndRestore|DatabaseTimeMatchesPostgresPrecision)$' -count=1"},
	{phase: "server_unit", command: "go test ./st ./startifact -count=1"},
	{phase: "server_unit", command: "go test ./controller -run '^Test(CoreStClient(BlockHashes|FinalizedHead|Epoch)|CoreStClientBindingsAt|DecodeStRPCBlockIdentity|StatsAlphaPriceURLIsMainnetOnly|StatsGaugeVecReplaceDeletesStaleSeries|StConfig|StCompute|StBuild|StDeposit|StEstimate|StReplacement|StDecode|StEvent|StBroadcast|StClientStub|StTransactionCancellation|VerifyEvidenceRange|VerifyKeyRotation|VerifySyntheticSeedId|VerifyUsesUrForwardedAddress|VerifyIgnoresLegacyForwardedAddress|VerifyClampM|VerifyCachedResponseRoundTrip|VerifySeedRejectsMissingSignature|StripeReconcileCredentialsRequireNonblankAPIToken|AppleReconcileCredentialsRequireCompleteServerAPIIdentity|PlayReconcileCredentialsRequireOAuthPackageAndSKUs|SolanaReconcileCredentialsRequireNonblankHeliusAPIKey)' -count=1"},
	{phase: "server_unit", command: "go test ./session -run 'Test.*(UrForwardedAddress|LegacyForwardedHeaders|RemoteAddress)' -count=1"},
	{phase: "server_unit", command: "go test ./router -run 'TestTrie' -count=1"},
	{phase: "server_unit", command: "go test ./model -run '^Test(VerifyEgressExactIndexAndPrefixScoreAreIndependent|StTransactionAdvisoryLockKeyUsesEthereumNonceScope|StHeadBoundCkeysFromEvents|ParseHeadEventCkey)$' -count=1"},
	{phase: "server_unit", command: "go test ./taskworker/work -run '^TestStSettlementTasksRejectStaleCoordinatorPayloads$' -count=1"},
	{phase: "server_unit", command: "go test ./monitor -count=1"},
	{phase: "server_unit", command: "go test -race ./monitor -count=1"},
	{phase: "connect", command: "go test . -run '^Test(Verify|Sn)' -count=1"},
	{phase: "sdk", command: "go test . -run '^Test(ApiSubnet|ProviderLocalUserNatSettings)' -count=1"},
	{phase: "server_db", command: "go test ./controller -run \"$controller_db_tests\" -count=1"},
	{phase: "server_db", command: "go test ./model -run \"$model_db_tests\" -count=1"},
	{phase: "server_db", command: "go test -race ./controller -run \"$controller_db_tests\" -count=1"},
	{phase: "server_db", command: "go test -race ./model -run \"$model_db_tests\" -count=1"},
}

// Check the existing line-oriented command contract, not a second shell runner.
// Fixed body commands cannot be hidden, replaced by a list, or made compile-only.
func verifyReleaseGateUncachedBodies(script string) error {
	for _, required := range releaseGateUncachedCommands {
		pattern := regexp.MustCompile("(?ms)^[\\t ]*release_phase_" + regexp.QuoteMeta(required.phase) + "\\(\\) \\{\\n(.*?)^[\\t ]*\\}[\\t ]*$")
		definitions := pattern.FindAllStringSubmatch(script, -1)
		if len(definitions) != 1 {
			return fmt.Errorf("uncached gate requires one %s phase", required.phase)
		}
		body := definitions[0][1]
		// The bounded phase extractor ends at the first closing brace. Refuse
		// nested declarations before an incomplete one can look executable.
		nested := regexp.MustCompile(`(?m)^[\t ]*(function[\t ]+|[A-Za-z_][A-Za-z0-9_]*[\t ]*\([\t ]*\))`)
		if nested.MatchString(body) {
			return fmt.Errorf("uncached gate phase contains a nested function: %s", required.phase)
		}
		for _, line := range strings.Split(body, "\n") {
			line = strings.TrimSpace(line)
			if line == required.command {
				break
			}
			if !strings.HasPrefix(line, "#") && strings.Contains(line, "<<") {
				return fmt.Errorf("uncached gate body has an unsupported document wrapper: %s", required.phase)
			}
		}
		conditions, err := releaseGateRegistrationConditions(body, required.command)
		if err != nil || len(conditions) != 0 {
			return fmt.Errorf("uncached gate body %s is missing, conditional or altered: %v %v", required.command, conditions, err)
		}
	}
	compileCounts := map[string]int{
		`go test "${server_packages[@]}" -run '^$'`: 0,
		"go test ./... -run '^$'":                   0,
	}
	for _, line := range strings.Split(script, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "go test ") {
			continue
		}
		if count, ok := compileCounts[line]; ok {
			compileCounts[line] = count + 1
			continue
		}
		count := 0
		for _, field := range strings.Fields(line) {
			if field == "-count" || strings.HasPrefix(field, "-count=") {
				if field != "-count=1" {
					return fmt.Errorf("gate body changes its uncached execution count: %s", line)
				}
				count++
			}
		}
		if count != 1 {
			return fmt.Errorf("gate body lacks exactly one uncached execution count: %s", line)
		}
	}
	if compileCounts[`go test "${server_packages[@]}" -run '^$'`] != 1 || compileCounts["go test ./... -run '^$'"] != 2 {
		return fmt.Errorf("compile-only checks changed their independent non-body census")
	}
	return nil
}

// Every original aggregate body executes fresh; compile-only checks stay
// explicit without being promoted into execution evidence.
func TestReleaseGateJobsRequireUncachedAggregateBodies(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../scripts/test-release-1.0-local.sh")
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyReleaseGateUncachedBodies(string(raw)); err != nil {
		t.Fatal(err)
	}
}

// Restoring each original omission independently must fail admission, as must
// substituted, unreachable, overridden, duplicated and new cacheable bodies.
func TestReleaseGateJobsRejectCachedOrHiddenAggregateBodies(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../scripts/test-release-1.0-local.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(raw)
	if err := verifyReleaseGateUncachedBodies(script); err != nil {
		t.Fatal(err)
	}
	for _, required := range releaseGateUncachedCommands {
		if strings.Count(script, required.command) != 1 {
			t.Fatalf("mutation lacks one actual %s body", required.phase)
		}
		for _, replacement := range []string{
			strings.Replace(required.command, " -count=1", "", 1),
			strings.Replace(required.command, "-count=1", "-count=0", 1),
			required.command + " -count=2",
			required.command + " -count=1",
			required.command + " -run '^$'",
			required.command + " -list '^Test'",
			required.command + " || true",
			"# " + required.command,
			"if false; then\n" + required.command + "\nfi",
			"release_unused_body() {\n" + required.command + "\n}",
			"release_unused_body () {\n" + required.command + "\n}",
			"function release_unused_body {\n" + required.command + "\n}",
			"release_unused_body()\n{\n" + required.command + "\n}",
			"(\n" + required.command + "\n)",
			"{\n" + required.command + "\n}",
			"cat <<'capture_body'\n" + required.command + "\ncapture_body",
			required.command + "\n" + required.command,
		} {
			changed := strings.Replace(script, required.command, replacement, 1)
			if err := verifyReleaseGateUncachedBodies(changed); err == nil {
				t.Fatalf("uncached body admitted %s: %s", required.phase, replacement)
			}
		}
	}
	if err := verifyReleaseGateUncachedBodies(script + "\ngo test ./new-synthetic-package\n"); err == nil {
		t.Fatal("new aggregate body omitted its fresh execution count")
	}
	if err := verifyReleaseGateUncachedBodies(script + "\ngo test ./... -run '^$'\n"); err == nil {
		t.Fatal("an extra compile-only check changed the non-body census")
	}
}
