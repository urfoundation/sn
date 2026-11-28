//go:build windows

package winservice

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// StopHandleVariable names the inherited event a supervised miner ends on.
const StopHandleVariable = "URNETWORK_SERVICE_STOP_HANDLE"

// The exit code of a miner whose device needs setup; restarting won't help.
const ExitNeedsSetup = 3

// ErrRunning is a second supervisor of the same service.
var ErrRunning = errors.New("the service is already running")

// Config is what install saves and the supervisor runs.
type Config struct {
	Name       string `json:"name"`
	Executable string `json:"executable"`
	StateDir   string `json:"state_dir"`
	// KEY=VALUE pairs the miner gets, as Environment= lines in a unit
	Environment []string `json:"environment,omitempty"`
}

// The folders, replaced by tests.
var localAppData = func() (string, error) {
	return windows.KnownFolderPath(windows.FOLDERID_LocalAppData, 0)
}

var startupFolder = func() (string, error) {
	return windows.KnownFolderPath(windows.FOLDERID_Startup, 0)
}

// Dir holds each service's config and lock.
func Dir() (string, error) {
	base, err := localAppData()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "urnetwork-provider-service"), nil
}

// ShortcutPath is the Startup folder shortcut that starts the service at sign-in.
func ShortcutPath(name string) (string, error) {
	dir, err := startupFolder()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name+".lnk"), nil
}

func configPath(name string) (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name+".json"), nil
}

func lockPath(name string) (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name+".lock"), nil
}

// Load reads a saved service. A missing one is os.ErrNotExist.
func Load(name string) (*Config, error) {
	path, err := configPath(name)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	config := &Config{}
	if err := json.Unmarshal(raw, config); err != nil {
		return nil, fmt.Errorf("%s is damaged: %w", path, err)
	}
	if config.Name != name || config.Executable == "" || config.StateDir == "" {
		return nil, fmt.Errorf("%s is incomplete", path)
	}
	return config, nil
}

// Save writes the config whole: a temporary, flushed, then moved over the old
// one with write-through.
func Save(config Config) error {
	path, err := configPath(config.Name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	temp := path + ".tmp"
	file, err := os.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(raw)
	if err := errors.Join(writeErr, file.Sync(), file.Close()); err != nil {
		os.Remove(temp)
		return err
	}
	if err := move(temp, path, true); err != nil {
		os.Remove(temp)
		return err
	}
	return nil
}

// Remove deletes a service's config and lock. The caller stops it first.
func Remove(name string) error {
	config, err := configPath(name)
	if err != nil {
		return err
	}
	lock, err := lockPath(name)
	if err != nil {
		return err
	}
	var all error
	for _, path := range []string{config, lock} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			all = errors.Join(all, err)
		}
	}
	return all
}

// Arguments are the shortcut's arguments for a service.
func Arguments(name string) string {
	return "service run " + syscall.EscapeArg(name)
}

// ShortcutCommand is the PowerShell command that writes a minimized shortcut.
// Only WScript.Shell can write a .lnk, and only from COM. The values are
// quoted into the script, which is passed encoded, so no quoting of the
// command line is involved.
func ShortcutCommand(shortcut string, target string, arguments string, workingDir string) (string, []string, error) {
	system, err := windows.GetSystemDirectory()
	if err != nil {
		return "", nil, err
	}
	script := strings.Join([]string{
		"$ErrorActionPreference = 'Stop'",
		"$link = (New-Object -ComObject WScript.Shell).CreateShortcut(" + powerShellQuote(shortcut) + ")",
		"$link.TargetPath = " + powerShellQuote(target),
		"$link.Arguments = " + powerShellQuote(arguments),
		"$link.WorkingDirectory = " + powerShellQuote(workingDir),
		"$link.WindowStyle = 7",
		"$link.Description = 'URnetwork miner'",
		"$link.Save()",
	}, "\n")
	return filepath.Join(system, "WindowsPowerShell", "v1.0", "powershell.exe"), []string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-EncodedCommand", EncodePowerShell(script)}, nil
}

// powerShellQuote is a single-quoted PowerShell string. PowerShell also takes
// the typographic single quotes as quotes, so those are doubled too.
func powerShellQuote(value string) string {
	var b strings.Builder
	b.WriteByte('\'')
	for _, r := range value {
		switch r {
		case '\'', '‘', '’', '‚', '‛':
			b.WriteRune(r)
		}
		b.WriteRune(r)
	}
	b.WriteByte('\'')
	return b.String()
}

// EncodePowerShell is a script for -EncodedCommand: base64 of UTF-16LE.
func EncodePowerShell(script string) string {
	units := utf16.Encode([]rune(script))
	raw := make([]byte, 2*len(units))
	for i, unit := range units {
		binary.LittleEndian.PutUint16(raw[2*i:], unit)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

// StartupDisabled is whether the shortcut was turned off in Task Manager's
// Startup apps, which Windows records beside the folder, not in the shortcut.
func StartupDisabled(name string) bool {
	key, err := openRegistryKey(`Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\StartupFolder`)
	if err != nil {
		return false
	}
	defer windows.RegCloseKey(key)
	value, err := windows.UTF16PtrFromString(name + ".lnk")
	if err != nil {
		return false
	}
	var data [12]byte
	size := uint32(len(data))
	var kind uint32
	if err := windows.RegQueryValueEx(key, value, nil, &kind, &data[0], &size); err != nil || size == 0 {
		return false
	}
	// 2 (or 6) is enabled; an odd first byte is turned off
	return data[0]&1 == 1
}

func openRegistryKey(path string) (windows.Handle, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var key windows.Handle
	if err := windows.RegOpenKeyEx(windows.HKEY_CURRENT_USER, name, 0, windows.KEY_QUERY_VALUE, &key); err != nil {
		return 0, err
	}
	return key, nil
}

// The supervisor's lock covers a byte past the pid it writes, so a reader of
// the pid is never refused by the lock.
const lockOffsetHigh = 1

func openLock(name string) (*os.File, error) {
	path, err := lockPath(name)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	return os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0600)
}

func tryLock(file *os.File) (bool, error) {
	err := windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &windows.Overlapped{OffsetHigh: lockOffsetHigh})
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return false, nil
	}
	return err == nil, err
}

// Running is whether a supervisor of this service is running now.
func Running(name string) (bool, error) {
	file, err := openLock(name)
	if err != nil {
		return false, err
	}
	defer file.Close()
	locked, err := tryLock(file)
	if err != nil {
		return false, err
	}
	if locked {
		// at once: a supervisor starting now waits only briefly for it
		windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, &windows.Overlapped{OffsetHigh: lockOffsetHigh})
	}
	return !locked, nil
}

// lockSupervisor takes the supervisor's lock, waiting out a Running probe
// that holds it for a moment.
func lockSupervisor(file *os.File) (bool, error) {
	deadline := time.Now().Add(2 * time.Second)
	for {
		locked, err := tryLock(file)
		if locked || err != nil || time.Now().After(deadline) {
			return locked, err
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func supervisorPid(name string) int {
	path, err := lockPath(name)
	if err != nil {
		return 0
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(raw)))
	return pid
}

// stopEventName is per user: the Local namespace is per sign-in session, and
// a second account in the same session can't take this user's name.
func stopEventName(name string) (*uint16, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	return windows.UTF16PtrFromString(`Local\urnetwork-provider-service-` + name + "-" + user.User.Sid.String())
}

// Stop asks the supervisor to stop its miner and waits until it has. One that
// doesn't stop within the wait is ended, and its job object ends the miner.
func Stop(name string, wait time.Duration) error {
	running, err := Running(name)
	if err != nil || !running {
		return err
	}
	if event, ok := openStopEvent(name); ok {
		err = windows.SetEvent(event)
		windows.CloseHandle(event)
		if err == nil && waitStopped(name, wait) {
			return nil
		}
	} else if running, _ := Running(name); !running {
		return nil
	}
	// a stop from another sign-in session (the event is per session), or a
	// supervisor that didn't answer: end it, and its job ends the miner
	pid := supervisorPid(name)
	if pid <= 0 {
		return errors.New("the service didn't stop, and its process id is unknown")
	}
	process, err := windows.OpenProcess(windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return fmt.Errorf("the service didn't stop: %w", err)
	}
	defer windows.CloseHandle(process)
	// the lock is still held and still names this pid, so the process is
	// its holder and not a later one that reused the id
	if running, _ := Running(name); !running {
		return nil
	}
	if supervisorPid(name) != pid {
		return errors.New("the service changed while stopping; try again")
	}
	if err := windows.TerminateProcess(process, 1); err != nil {
		return fmt.Errorf("the service didn't stop: %w", err)
	}
	if !waitStopped(name, 10*time.Second) {
		return errors.New("the service didn't stop")
	}
	return nil
}

// openStopEvent opens the running supervisor's stop event. A supervisor
// that has just taken its lock makes the event a moment later.
func openStopEvent(name string) (windows.Handle, bool) {
	eventName, err := stopEventName(name)
	if err != nil {
		return 0, false
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		event, err := windows.OpenEvent(windows.EVENT_MODIFY_STATE, false, eventName)
		if err == nil {
			return event, true
		}
		if running, _ := Running(name); !running || time.Now().After(deadline) {
			return 0, false
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func waitStopped(name string, wait time.Duration) bool {
	deadline := time.Now().Add(wait)
	for {
		if running, err := Running(name); err == nil && !running {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// Launch starts a program with no window, apart from this process's console
// and job, and doesn't wait for it.
func Launch(executable string, args ...string) error {
	flags := uint32(windows.CREATE_NO_WINDOW | windows.CREATE_NEW_PROCESS_GROUP)
	start := func(flags uint32) error {
		command := exec.Command(executable, args...)
		command.Dir = filepath.Dir(executable)
		command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: flags}
		if err := command.Start(); err != nil {
			return err
		}
		return command.Process.Release()
	}
	err := start(flags | windows.CREATE_BREAKAWAY_FROM_JOB)
	if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		// a job that doesn't allow breaking away (some remote shells): the
		// supervisor then ends with that job, as with any program started there
		err = start(flags)
	}
	return err
}

var procGetConsoleWindow = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetConsoleWindow")

// HasConsoleWindow is whether this process shows a console window, as one
// started from the shortcut does, minimized.
func HasConsoleWindow() bool {
	window, _, _ := procGetConsoleWindow.Call()
	return window != 0
}

// LogName is the supervisor's log in the state folder, as on macOS.
const LogName = "service.log"

// The log moves to service.log.1 once it is this large, when a supervisor starts.
const logRotateBytes = 10 << 20

// OpenLog appends to the state folder's service.log.
func OpenLog(stateDir string) (*os.File, error) {
	path := filepath.Join(stateDir, LogName)
	if info, err := os.Stat(path); err == nil && logRotateBytes < info.Size() {
		// still open in an update that outlived its supervisor: keep appending
		_ = move(path, path+".1", true)
	}
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
}

// Supervisor runs one service's miner and its update checks.
type Supervisor struct {
	Config Config
	Log    *os.File

	RestartDelay time.Duration
	FirstUpdate  time.Duration
	UpdateEvery  time.Duration
	UpdateJitter time.Duration
	StopWait     time.Duration
	// the arguments of the miner and of an update check
	MinerArgs  []string
	UpdateArgs []string
}

// NewSupervisor has the systemd unit's timings.
func NewSupervisor(config Config, log *os.File) *Supervisor {
	return &Supervisor{
		Config:       config,
		Log:          log,
		RestartDelay: 15 * time.Second,
		FirstUpdate:  15 * time.Minute,
		UpdateEvery:  6 * time.Hour,
		UpdateJitter: time.Hour,
		StopWait:     30 * time.Second,
		MinerArgs:    []string{"start"},
		UpdateArgs:   []string{"update", "--auto"},
	}
}

func (self *Supervisor) logf(format string, args ...any) {
	fmt.Fprintf(self.Log, "%s service: %s\n", time.Now().Format(time.RFC3339), fmt.Sprintf(format, args...))
}

// Environment is this environment for the miner: the URNETWORK_ settings of
// whoever started the supervisor are dropped, and the service's own are set,
// so a shell's settings never leak into a service started from it.
func Environment(base []string, config Config, defaultName string) []string {
	out := []string{}
	for _, variable := range base {
		key, _, _ := strings.Cut(variable, "=")
		if strings.HasPrefix(strings.ToUpper(key), "URNETWORK_") {
			continue
		}
		out = append(out, variable)
	}
	out = append(out, "URNETWORK_STATE_DIR="+config.StateDir)
	if config.Name != defaultName {
		out = append(out, "URNETWORK_SERVICE_NAME="+config.Name)
	}
	return append(out, config.Environment...)
}

// Run supervises until stopped, or until the miner exits for good. It returns
// the miner's last exit code: 0 after a stop, 3 when the device needs setup.
func (self *Supervisor) Run(environment []string) (int, error) {
	lock, err := openLock(self.Config.Name)
	if err != nil {
		return 1, err
	}
	defer lock.Close()
	locked, err := lockSupervisor(lock)
	if err != nil {
		return 1, err
	}
	if !locked {
		return 0, ErrRunning
	}
	if err := lock.Truncate(0); err != nil {
		return 1, err
	}
	if _, err := lock.WriteAt([]byte(strconv.Itoa(os.Getpid())), 0); err != nil {
		return 1, err
	}
	eventName, err := stopEventName(self.Config.Name)
	if err != nil {
		return 1, err
	}
	stop, err := windows.CreateEvent(nil, 1, 0, eventName)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		// still open in a stop that just ran; this supervisor holds the lock
		err = windows.ResetEvent(stop)
	}
	if err != nil {
		if stop != 0 {
			windows.CloseHandle(stop)
		}
		return 1, fmt.Errorf("can't make the stop event: %w", err)
	}
	defer windows.CloseHandle(stop)
	stopped := make(chan struct{})
	go func() {
		windows.WaitForSingleObject(stop, windows.INFINITE)
		close(stopped)
	}()
	job, err := newKillJob()
	if err != nil {
		return 1, err
	}
	// closing the job, also by dying, ends the miner
	defer windows.CloseHandle(job)

	self.logf("started (pid %d), running %s", os.Getpid(), self.Config.Executable)
	loop := &supervisorLoop{Supervisor: self, environment: environment, stopped: stopped, update: time.NewTimer(self.FirstUpdate + self.jitter())}
	defer loop.update.Stop()
	for {
		code, err := loop.miner(job)
		switch {
		case errors.Is(err, errStopped):
			self.logf("stopped")
			return 0, nil
		case err != nil:
			self.logf("the miner didn't start: %s", err)
		case code == 0:
			self.logf("the miner exited")
			return 0, nil
		case code == ExitNeedsSetup:
			self.logf("the miner needs setup (provider setup); not restarting it")
			return code, nil
		default:
			self.logf("the miner exited with code %d; starting it again in %s", code, self.RestartDelay)
		}
		if loop.wait(self.RestartDelay) {
			self.logf("stopped")
			return 0, nil
		}
	}
}

var errStopped = errors.New("stopped")

// supervisorLoop is one supervisor's state between miner runs.
type supervisorLoop struct {
	*Supervisor
	environment []string
	stopped     chan struct{}
	update      *time.Timer
	updating    atomic.Bool
}

// tick runs an update check that is due.
func (self *supervisorLoop) tick() {
	self.startUpdate(&self.updating, self.environment)
	self.update.Reset(self.UpdateEvery + self.jitter())
}

// wait waits out a delay, still running update checks. It reports a stop.
func (self *supervisorLoop) wait(delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	for {
		select {
		case <-self.stopped:
			return true
		case <-self.update.C:
			self.tick()
		case <-timer.C:
			return false
		}
	}
}

// miner runs the miner once, to its exit code, or to errStopped after a
// stop, by which time the miner has ended.
func (self *supervisorLoop) miner(job windows.Handle) (int, error) {
	command, done, childStop, err := self.startMiner(job, self.environment)
	if err != nil {
		return 1, err
	}
	defer windows.CloseHandle(childStop)
	for {
		select {
		case <-self.update.C:
			self.tick()
		case <-self.stopped:
			self.logf("stopping the miner")
			windows.SetEvent(childStop)
			select {
			case <-done:
			case <-time.After(self.StopWait):
				self.logf("the miner didn't stop within %s; ending it", self.StopWait)
				windows.TerminateJobObject(job, 1)
				<-done
			}
			return 0, errStopped
		case <-done:
			return command.ProcessState.ExitCode(), nil
		}
	}
}

func (self *Supervisor) jitter() time.Duration {
	if self.UpdateJitter <= 0 {
		return 0
	}
	return rand.N(self.UpdateJitter)
}

func newKillJob() (windows.Handle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(job)
		return 0, err
	}
	return job, nil
}

// startMiner starts the miner in the job, with an inherited event it ends on.
func (self *Supervisor) startMiner(job windows.Handle, environment []string) (*exec.Cmd, chan struct{}, windows.Handle, error) {
	childStop, err := windows.CreateEvent(&windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), InheritHandle: 1}, 1, 0, nil)
	if err != nil {
		return nil, nil, 0, err
	}
	command := exec.Command(self.Config.Executable, self.MinerArgs...)
	command.Dir = filepath.Dir(self.Config.Executable)
	command.Env = append(append([]string{}, environment...), StopHandleVariable+"="+strconv.FormatUint(uint64(childStop), 10))
	command.Stdout = self.Log
	command.Stderr = self.Log
	command.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:                 true,
		CreationFlags:              windows.CREATE_NO_WINDOW,
		AdditionalInheritedHandles: []syscall.Handle{syscall.Handle(childStop)},
	}
	if err := command.Start(); err != nil {
		windows.CloseHandle(childStop)
		return nil, nil, 0, err
	}
	if process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(command.Process.Pid)); err == nil {
		if err := windows.AssignProcessToJobObject(job, process); err != nil {
			self.logf("the miner isn't in the service's job (%s); it may outlive a supervisor that dies", err)
		}
		windows.CloseHandle(process)
	}
	done := make(chan struct{})
	go func() {
		_ = command.Wait()
		close(done)
	}()
	return command, done, childStop, nil
}

// startUpdate runs one update check outside the job: an update restarts this
// supervisor, and must outlive it to start the new one.
func (self *Supervisor) startUpdate(updating *atomic.Bool, environment []string) {
	if !updating.CompareAndSwap(false, true) {
		self.logf("the last update check is still running; skipping this one")
		return
	}
	command := exec.Command(self.Config.Executable, self.UpdateArgs...)
	command.Dir = filepath.Dir(self.Config.Executable)
	command.Env = environment
	command.Stdout = self.Log
	command.Stderr = self.Log
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW | windows.CREATE_NEW_PROCESS_GROUP}
	if err := command.Start(); err != nil {
		updating.Store(false)
		self.logf("the update check didn't start: %s", err)
		return
	}
	go func() {
		_ = command.Wait()
		updating.Store(false)
	}()
}

// StopHandle is the inherited stop event a supervised miner waits on, if any.
// It is taken out of the environment, so nothing the miner starts sees it.
func StopHandle() (windows.Handle, bool) {
	value, ok := os.LookupEnv(StopHandleVariable)
	if !ok {
		return 0, false
	}
	os.Unsetenv(StopHandleVariable)
	handle, err := strconv.ParseUint(value, 10, 64)
	if err != nil || handle == 0 {
		return 0, false
	}
	return windows.Handle(handle), true
}

// WaitStop calls stop once the supervisor asks its miner to end.
func WaitStop(handle windows.Handle, stop func()) {
	go func() {
		if event, err := windows.WaitForSingleObject(handle, windows.INFINITE); err == nil && event == windows.WAIT_OBJECT_0 {
			stop()
		}
	}()
}

// ReplaceExecutable puts newPath in place of executable, which may be
// running: Windows can rename a running program but not replace or delete
// it. The old one becomes executable.previous; an older previous that is
// still running (an old supervisor) is set aside under a random name and
// removed by a later update.
func ReplaceExecutable(newPath string, executable string) error {
	removeAside(executable)
	previous := executable + ".previous"
	if err := setAside(previous, executable); err != nil {
		return fmt.Errorf("can't set the older version %s aside: %w", previous, err)
	}
	if err := move(executable, previous, false); err != nil {
		return fmt.Errorf("can't keep the previous version beside %s: %w", executable, err)
	}
	if err := move(newPath, executable, false); err != nil {
		if restoreErr := move(previous, executable, false); restoreErr != nil {
			return fmt.Errorf("can't replace %s (%w), and the previous version couldn't be put back: %w", executable, err, restoreErr)
		}
		return fmt.Errorf("can't replace %s: %w", executable, err)
	}
	return nil
}

// RestorePrevious puts executable.previous back, setting the running new
// version aside.
func RestorePrevious(executable string) error {
	previous := executable + ".previous"
	if _, err := os.Stat(previous); err != nil {
		return err
	}
	// set aside under a known name, so a failed move can put it back
	failed := executable + ".failed"
	if err := setAside(failed, executable); err != nil {
		return err
	}
	if err := move(executable, failed, false); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := move(previous, executable, false); err != nil {
		if restoreErr := move(failed, executable, false); restoreErr != nil {
			return fmt.Errorf("can't restore %s (%w), and the new version couldn't be put back: %w", previous, err, restoreErr)
		}
		return err
	}
	_ = os.Remove(failed)
	return nil
}

// setAside removes path, or renames it aside when it is running.
func setAside(path string, executable string) error {
	err := os.Remove(path)
	if err == nil || errors.Is(err, os.ErrNotExist) {
		return nil
	}
	var random [8]byte
	for i := range random {
		random[i] = "abcdefghijklmnopqrstuvwxyz0123456789"[rand.N(36)]
	}
	return move(path, executable+".old-"+string(random[:]), false)
}

// removeAside removes what earlier updates set aside, where nothing runs it now.
func removeAside(executable string) {
	matches, _ := filepath.Glob(executable + ".old-*")
	for _, path := range matches {
		_ = os.Remove(path)
	}
}

// move is MoveFileEx with write-through, retried while a scanner or indexer
// briefly holds the file.
func move(from string, to string, replace bool) error {
	fromPtr, err := windows.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	toPtr, err := windows.UTF16PtrFromString(to)
	if err != nil {
		return err
	}
	flags := uint32(windows.MOVEFILE_WRITE_THROUGH)
	if replace {
		flags |= windows.MOVEFILE_REPLACE_EXISTING
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		err := windows.MoveFileEx(fromPtr, toPtr, flags)
		if err == nil {
			return nil
		}
		if !errors.Is(err, windows.ERROR_SHARING_VIOLATION) && !errors.Is(err, windows.ERROR_ACCESS_DENIED) || time.Now().After(deadline) {
			return &os.LinkError{Op: "rename", Old: from, New: to, Err: err}
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Tail writes the last lines of a log, then, with follow, what is added to
// it until stop closes. A log that shrinks (rotated) is read from its start.
func Tail(path string, lines int, follow bool, out io.Writer, stop <-chan struct{}) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	const window = 1 << 20
	start := max(info.Size()-window, 0)
	raw := make([]byte, info.Size()-start)
	if _, err := file.ReadAt(raw, start); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	text := strings.TrimRight(string(raw), "\n")
	all := strings.Split(text, "\n")
	if text == "" {
		all = nil
	}
	if lines < len(all) {
		all = all[len(all)-lines:]
	}
	for _, line := range all {
		fmt.Fprintln(out, strings.TrimRight(line, "\r"))
	}
	offset := info.Size()
	for follow {
		select {
		case <-stop:
			return nil
		case <-time.After(500 * time.Millisecond):
		}
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		if info.Size() < offset {
			if reopened, err := os.Open(path); err == nil {
				file.Close()
				file = reopened
				offset = 0
			}
		}
		if info.Size() == offset {
			continue
		}
		n, err := io.Copy(out, io.NewSectionReader(file, offset, info.Size()-offset))
		offset += n
		if err != nil {
			return err
		}
	}
	return nil
}
