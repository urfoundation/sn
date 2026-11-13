//go:build windows

package winservice

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

// The test binary also plays the miner, an update check and a supervisor,
// chosen by its arguments: winservice-role <role>.
func TestMain(m *testing.M) {
	if dir := os.Getenv("WINSERVICE_TEST_DIR"); dir != "" {
		localAppData = func() (string, error) { return dir, nil }
	}
	role := ""
	if len(os.Args) == 3 && os.Args[1] == "winservice-role" {
		role = os.Args[2]
	}
	switch role {
	case "miner":
		os.Exit(fakeMiner())
	case "update":
		appendMark("update")
		if os.Getenv("WINSERVICE_TEST_SLOW_UPDATE") != "" {
			time.Sleep(2 * time.Second)
			appendMark("update-done")
		}
		os.Exit(0)
	case "supervisor":
		os.Exit(fakeSupervisor())
	case "sleep":
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func appendMark(mark string) {
	file, err := os.OpenFile(os.Getenv("WINSERVICE_TEST_MARKS"), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		os.Exit(90)
	}
	fmt.Fprintln(file, mark)
	file.Close()
}

func readMarks(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Fields(string(raw))
}

// fakeMiner fails WINSERVICE_TEST_FAILS times with code 7 (counted in the
// marks), exits with WINSERVICE_TEST_EXIT if set, or waits for its stop
// event, unless WINSERVICE_TEST_IGNORE_STOP is set.
func fakeMiner() int {
	marks := os.Getenv("WINSERVICE_TEST_MARKS")
	raw, _ := os.ReadFile(marks)
	starts := strings.Count(string(raw), "start")
	appendMark("start")
	appendMark("pid:" + strconv.Itoa(os.Getpid()))
	if os.Getenv("URNETWORK_SHELL_SETTING") != "" {
		appendMark("leaked")
	}
	if os.Getenv("URNETWORK_STATE_DIR") != "" {
		appendMark("state")
	}
	if fails, _ := strconv.Atoi(os.Getenv("WINSERVICE_TEST_FAILS")); starts < fails {
		return 7
	}
	if code := os.Getenv("WINSERVICE_TEST_EXIT"); code != "" {
		value, _ := strconv.Atoi(code)
		return value
	}
	handle, ok := StopHandle()
	if !ok {
		appendMark("nohandle")
		return 91
	}
	if os.Getenv(StopHandleVariable) != "" {
		appendMark("handle-left-in-environment")
	}
	if os.Getenv("WINSERVICE_TEST_IGNORE_STOP") != "" {
		time.Sleep(time.Minute)
		return 0
	}
	stopped := make(chan struct{})
	WaitStop(handle, func() { close(stopped) })
	select {
	case <-stopped:
		appendMark("graceful")
		return 0
	case <-time.After(time.Minute):
		return 92
	}
}

func fakeSupervisor() int {
	log, err := os.OpenFile(filepath.Join(os.Getenv("WINSERVICE_TEST_DIR"), "service.log"), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		return 93
	}
	defer log.Close()
	supervisor := testSupervisor(os.Getenv("WINSERVICE_TEST_NAME"), log)
	code, err := supervisor.Run(os.Environ())
	if err != nil {
		fmt.Fprintln(log, err)
	}
	return code
}

func testSupervisor(name string, log *os.File) *Supervisor {
	executable, _ := os.Executable()
	supervisor := NewSupervisor(Config{Name: name, Executable: executable, StateDir: filepath.Dir(log.Name())}, log)
	supervisor.RestartDelay = 100 * time.Millisecond
	supervisor.FirstUpdate = 200 * time.Millisecond
	supervisor.UpdateEvery = time.Hour
	supervisor.UpdateJitter = 0
	supervisor.StopWait = 5 * time.Second
	supervisor.MinerArgs = []string{"winservice-role", "miner"}
	supervisor.UpdateArgs = []string{"winservice-role", "update"}
	return supervisor
}

// testService points this process's folders at a temporary one, with marks
// and a log in it, and the role environment the children read.
func testService(t *testing.T) (name string, dir string, marks string) {
	t.Helper()
	dir = t.TempDir()
	saved := localAppData
	localAppData = func() (string, error) { return dir, nil }
	t.Cleanup(func() { localAppData = saved })
	name = "winservice-test-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	marks = filepath.Join(dir, "marks")
	t.Setenv("WINSERVICE_TEST_DIR", dir)
	t.Setenv("WINSERVICE_TEST_MARKS", marks)
	t.Setenv("WINSERVICE_TEST_NAME", name)
	return name, dir, marks
}

func openTestLog(t *testing.T, dir string) *os.File {
	t.Helper()
	log, err := OpenLog(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { log.Close() })
	return log
}

// runSupervisor runs a supervisor in this process with the miner role.
func runSupervisor(t *testing.T, name string, dir string, environment ...string) (*Supervisor, chan int) {
	t.Helper()
	supervisor := testSupervisor(name, openTestLog(t, dir))
	env := append(Environment(os.Environ(), supervisor.Config, "urnetwork-provider"), environment...)
	done := make(chan int, 1)
	go func() {
		code, err := supervisor.Run(env)
		if err != nil {
			t.Error(err)
		}
		done <- code
	}()
	return supervisor, done
}

func waitFor(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func waitCode(t *testing.T, done chan int) int {
	t.Helper()
	select {
	case code := <-done:
		return code
	case <-time.After(30 * time.Second):
		t.Fatal("the supervisor didn't return")
		return -1
	}
}

func count(marks []string, mark string) int {
	n := 0
	for _, m := range marks {
		if m == mark {
			n++
		}
	}
	return n
}

func TestSupervisorStopsTheMinerGracefully(t *testing.T) {
	name, dir, marks := testService(t)
	t.Setenv("URNETWORK_SHELL_SETTING", "from a shell")
	_, done := runSupervisor(t, name, dir)
	waitFor(t, "the miner and an update check", func() bool {
		m := readMarks(t, marks)
		return count(m, "start") == 1 && count(m, "update") == 1
	})
	if running, err := Running(name); err != nil || !running {
		t.Fatalf("Running = %v, %v while supervising", running, err)
	}
	if err := Stop(name, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	if code := waitCode(t, done); code != 0 {
		t.Fatalf("code after a stop = %d", code)
	}
	got := readMarks(t, marks)
	for _, want := range []string{"graceful", "state"} {
		if count(got, want) != 1 {
			t.Errorf("marks = %v, want one %q", got, want)
		}
	}
	for _, unwanted := range []string{"leaked", "nohandle", "handle-left-in-environment"} {
		if count(got, unwanted) != 0 {
			t.Errorf("marks = %v, has %q", got, unwanted)
		}
	}
	if running, _ := Running(name); running {
		t.Fatal("still running after a stop")
	}
	if err := Stop(name, time.Second); err != nil {
		t.Fatalf("stopping a stopped service: %v", err)
	}
}

func TestSupervisorRestartsAFailedMiner(t *testing.T) {
	name, dir, marks := testService(t)
	_, done := runSupervisor(t, name, dir, "WINSERVICE_TEST_FAILS=2")
	waitFor(t, "a third start", func() bool { return count(readMarks(t, marks), "start") == 3 })
	if err := Stop(name, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	if code := waitCode(t, done); code != 0 {
		t.Fatalf("code = %d", code)
	}
	if got := readMarks(t, marks); count(got, "start") != 3 || count(got, "graceful") != 1 {
		t.Fatalf("marks = %v", got)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, LogName))
	if !strings.Contains(string(raw), "exited with code 7; starting it again") {
		t.Fatalf("log doesn't say why it restarted:\n%s", raw)
	}
}

func TestSupervisorLeavesAMinerThatNeedsSetup(t *testing.T) {
	name, dir, marks := testService(t)
	_, done := runSupervisor(t, name, dir, "WINSERVICE_TEST_EXIT=3")
	if code := waitCode(t, done); code != ExitNeedsSetup {
		t.Fatalf("code = %d, want %d", code, ExitNeedsSetup)
	}
	if got := readMarks(t, marks); count(got, "start") != 1 {
		t.Fatalf("a miner that needs setup was started again: %v", got)
	}
}

func TestSupervisorEndsAMinerThatIgnoresTheStop(t *testing.T) {
	name, dir, marks := testService(t)
	supervisor, done := runSupervisor(t, name, dir, "WINSERVICE_TEST_IGNORE_STOP=1")
	supervisor.StopWait = 500 * time.Millisecond
	waitFor(t, "the miner", func() bool { return count(readMarks(t, marks), "start") == 1 })
	started := time.Now()
	if err := Stop(name, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	if code := waitCode(t, done); code != 0 {
		t.Fatalf("code = %d", code)
	}
	if 8*time.Second < time.Since(started) {
		t.Fatalf("the stop took %s", time.Since(started))
	}
}

// provider service start polls Running while the supervisor it launched
// takes its lock; the probe must not turn that supervisor away.
func TestSupervisorStartsWhileRunningIsPolled(t *testing.T) {
	name, dir, marks := testService(t)
	polling := make(chan struct{})
	go func() {
		for {
			select {
			case <-polling:
				return
			default:
				Running(name)
			}
		}
	}()
	defer close(polling)
	_, done := runSupervisor(t, name, dir)
	waitFor(t, "the miner", func() bool { return count(readMarks(t, marks), "start") == 1 })
	if err := Stop(name, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	if code := waitCode(t, done); code != 0 {
		t.Fatalf("code = %d", code)
	}
}

func TestSecondSupervisorDoesNotRun(t *testing.T) {
	name, dir, marks := testService(t)
	_, done := runSupervisor(t, name, dir)
	waitFor(t, "the miner", func() bool { return count(readMarks(t, marks), "start") == 1 })
	second := testSupervisor(name, openTestLog(t, dir))
	if _, err := second.Run(os.Environ()); !errors.Is(err, ErrRunning) {
		t.Fatalf("second supervisor: %v", err)
	}
	if err := Stop(name, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	waitCode(t, done)
}

// A supervisor in its own process, started detached, is found and stopped
// by name; ending it ends its miner with it.
func TestLaunchedSupervisorStopsAcrossProcesses(t *testing.T) {
	name, _, marks := testService(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := Launch(executable, "winservice-role", "supervisor"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the launched supervisor's miner", func() bool { return count(readMarks(t, marks), "start") == 1 })
	if running, _ := Running(name); !running {
		t.Fatal("the launched supervisor isn't running")
	}
	if pid := supervisorPid(name); pid <= 0 || pid == os.Getpid() {
		t.Fatalf("supervisor pid = %d", pid)
	}
	if err := Stop(name, 15*time.Second); err != nil {
		t.Fatal(err)
	}
	if got := readMarks(t, marks); count(got, "graceful") != 1 {
		t.Fatalf("marks = %v", got)
	}
}

func markedPid(t *testing.T, marks []string) int {
	t.Helper()
	for _, mark := range marks {
		if value, ok := strings.CutPrefix(mark, "pid:"); ok {
			pid, _ := strconv.Atoi(value)
			return pid
		}
	}
	t.Fatalf("no pid in %v", marks)
	return 0
}

func processAlive(pid int) bool {
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(process)
	event, _ := windows.WaitForSingleObject(process, 0)
	return event == uint32(windows.WAIT_TIMEOUT)
}

// A supervisor that dies (ended in Task Manager, say) takes its miner with
// it, through the job; an update check it started lives on, as one that
// restarts the service must.
func TestDeadSupervisorEndsItsMinerButNotAnUpdate(t *testing.T) {
	name, _, marks := testService(t)
	t.Setenv("WINSERVICE_TEST_SLOW_UPDATE", "1")
	t.Setenv("WINSERVICE_TEST_IGNORE_STOP", "1")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := Launch(executable, "winservice-role", "supervisor"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the miner and an update check", func() bool {
		m := readMarks(t, marks)
		return count(m, "start") == 1 && count(m, "update") == 1
	})
	miner := markedPid(t, readMarks(t, marks))
	supervisor, err := windows.OpenProcess(windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, uint32(supervisorPid(name)))
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(supervisor)
	if err := windows.TerminateProcess(supervisor, 1); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the miner to end with its supervisor", func() bool { return !processAlive(miner) })
	waitFor(t, "the update check to finish", func() bool { return count(readMarks(t, marks), "update-done") == 1 })
	if running, _ := Running(name); running {
		t.Fatal("a dead supervisor still counts as running")
	}
}

func TestEnvironmentKeepsOnlyTheServiceSettings(t *testing.T) {
	config := Config{Name: "test-miner", StateDir: `C:\state`, Environment: []string{"URNETWORK_AUTO_UPDATE=0"}}
	got := Environment([]string{"PATH=C:\\Windows", "URNETWORK_STATE_DIR=C:\\other", "urnetwork_update_url=http://x", "URNETWORK_SERVICE_STOP_HANDLE=4"}, config, "urnetwork-provider")
	want := []string{"PATH=C:\\Windows", `URNETWORK_STATE_DIR=C:\state`, "URNETWORK_SERVICE_NAME=test-miner", "URNETWORK_AUTO_UPDATE=0"}
	if !slices.Equal(got, want) {
		t.Fatalf("Environment = %q, want %q", got, want)
	}
	if got := Environment(nil, Config{Name: "urnetwork-provider", StateDir: `C:\s`}, "urnetwork-provider"); !slices.Equal(got, []string{`URNETWORK_STATE_DIR=C:\s`}) {
		t.Fatalf("default name: %q", got)
	}
}

func TestConfigSavesWholeAndLoads(t *testing.T) {
	name, _, _ := testService(t)
	if _, err := Load(name); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing config: %v", err)
	}
	config := Config{Name: name, Executable: `C:\bin\provider.exe`, StateDir: `C:\Users\me\.urnetwork`, Environment: []string{"URNETWORK_UPDATE_URL=https://example.test"}}
	for range 2 {
		if err := Save(config); err != nil {
			t.Fatal(err)
		}
	}
	loaded, err := Load(name)
	if err != nil || loaded.Executable != config.Executable || loaded.StateDir != config.StateDir || !slices.Equal(loaded.Environment, config.Environment) {
		t.Fatalf("Load = %+v, %v", loaded, err)
	}
	dir, _ := Dir()
	if _, err := os.Stat(filepath.Join(dir, name+".json.tmp")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a temporary remains: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".json"), []byte(`{"name":"other"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(name); err == nil {
		t.Fatal("a config for another name was loaded")
	}
	if err := Remove(name); err != nil {
		t.Fatal(err)
	}
	if err := Remove(name); err != nil {
		t.Fatalf("removing twice: %v", err)
	}
}

func decodePowerShell(t *testing.T, encoded string) string {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(raw)%2 != 0 {
		t.Fatalf("not base64 UTF-16: %v", err)
	}
	units := make([]uint16, len(raw)/2)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(raw[2*i:])
	}
	return string(utf16.Decode(units))
}

func TestShortcutCommandQuotesEveryValue(t *testing.T) {
	shortcut := `C:\Users\O'Brien\Start Menu\Startup\urnetwork-provider.lnk`
	target := `C:\Users\O’Brien\$HOME\provider.exe`
	name, args, err := ShortcutCommand(shortcut, target, Arguments("urnetwork-provider"), `C:\a "b"`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(strings.ToLower(name), `\windowspowershell\v1.0\powershell.exe`) {
		t.Fatalf("PowerShell = %s", name)
	}
	if len(args) < 2 || args[len(args)-2] != "-EncodedCommand" {
		t.Fatalf("args = %q", args)
	}
	script := decodePowerShell(t, args[len(args)-1])
	for _, want := range []string{
		`CreateShortcut('C:\Users\O''Brien\Start Menu\Startup\urnetwork-provider.lnk')`,
		`$link.TargetPath = 'C:\Users\O’’Brien\$HOME\provider.exe'`,
		`$link.Arguments = 'service run urnetwork-provider'`,
		`$link.WorkingDirectory = 'C:\a "b"'`,
		"$link.WindowStyle = 7",
		"$ErrorActionPreference = 'Stop'",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("script lacks %s:\n%s", want, script)
		}
	}
}

func TestReplaceExecutableWhileItRuns(t *testing.T) {
	dir := t.TempDir()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(dir, "provider.exe")
	copyTestFile(t, self, executable)
	running := exec.Command(executable, "winservice-role", "sleep")
	if err := running.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { running.Process.Kill(); running.Wait() })
	for round := range 3 {
		newPath := filepath.Join(dir, fmt.Sprintf(".provider.exe.new-%d.exe", round))
		if err := os.WriteFile(newPath, []byte(fmt.Sprintf("version %d", round)), 0755); err != nil {
			t.Fatal(err)
		}
		if err := ReplaceExecutable(newPath, executable); err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
		if raw, _ := os.ReadFile(executable); string(raw) != fmt.Sprintf("version %d", round) {
			t.Fatalf("round %d: executable = %.20q", round, raw)
		}
		if _, err := os.Stat(newPath); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("round %d: the new file remains", round)
		}
	}
	if raw, _ := os.ReadFile(executable + ".previous"); string(raw) != "version 1" {
		t.Fatalf("previous = %.20q", raw)
	}
	if err := RestorePrevious(executable); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(executable); string(raw) != "version 1" {
		t.Fatalf("restored = %.20q", raw)
	}
	if _, err := os.Stat(executable + ".previous"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("previous remains after a restore")
	}
}

func copyTestFile(t *testing.T, from string, to string) {
	t.Helper()
	in, err := os.Open(from)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.Create(to)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestTailShowsTheEndAndFollows(t *testing.T) {
	path := filepath.Join(t.TempDir(), LogName)
	var lines []string
	for i := range 150 {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\r\n")+"\r\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Tail(path, 100, false, &out, nil); err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(got) != 100 || got[0] != "line 50" || got[99] != "line 149" {
		t.Fatalf("tail = %d lines, %q … %q", len(got), got[0], got[len(got)-1])
	}
	reader, writer := io.Pipe()
	stop := make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		finished <- Tail(path, 1, true, writer, stop)
		writer.Close()
	}()
	buffered := make(chan string, 16)
	go func() {
		raw := make([]byte, 4096)
		for {
			n, err := reader.Read(raw)
			if n > 0 {
				buffered <- string(raw[:n])
			}
			if err != nil {
				close(buffered)
				return
			}
		}
	}()
	if first := <-buffered; !strings.Contains(first, "line 149") {
		t.Fatalf("first = %q", first)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprint(file, "added\r\n")
	file.Close()
	select {
	case next := <-buffered:
		if !strings.Contains(next, "added") {
			t.Fatalf("followed = %q", next)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("follow didn't show the added line")
	}
	close(stop)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
}

func TestOpenLogRotatesALargeLog(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, LogName)
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), logRotateBytes+1), 0600); err != nil {
		t.Fatal(err)
	}
	log, err := OpenLog(dir)
	if err != nil {
		t.Fatal(err)
	}
	log.Close()
	if info, err := os.Stat(path); err != nil || info.Size() != 0 {
		t.Fatalf("new log: %v, %v", info, err)
	}
	if info, err := os.Stat(path + ".1"); err != nil || info.Size() != logRotateBytes+1 {
		t.Fatalf("rotated log: %v", err)
	}
}
