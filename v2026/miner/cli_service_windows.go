//go:build windows

package miner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows"

	"github.com/urfoundation/sn/v2026/internal/winservice"
)

// windowsService is provider service on Windows: a Startup folder shortcut
// and the supervisor it starts at sign-in (internal/winservice).
type windowsService struct {
	name string
}

func newWindowsService() (minerServiceManager, error) {
	return &windowsService{name: minerServiceName()}, nil
}

func (self *windowsService) kind() string { return "Startup app" }

func (self *windowsService) config() *winservice.Config {
	config, err := winservice.Load(self.name)
	if err != nil {
		return nil
	}
	return config
}

func (self *windowsService) installed() bool { return self.config() != nil }

func (self *windowsService) active() bool {
	running, _ := winservice.Running(self.name)
	return running
}

func (self *windowsService) foreign() string {
	if shortcut := windowsInstallerShortcut(); shortcut != "" {
		return "urnetwork.lnk"
	}
	return ""
}

func (self *windowsService) configured() (string, string) {
	if config := self.config(); config != nil {
		return config.Executable, config.StateDir
	}
	return "", ""
}

func (self *windowsService) install(executable string, stateDir string, out io.Writer) error {
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		return err
	}
	if self.active() {
		// a reinstall runs the new binary and settings
		if err := winservice.Stop(self.name, windowsServiceStopWait); err != nil {
			return err
		}
	}
	config := winservice.Config{Name: self.name, Executable: executable, StateDir: stateDir, Environment: minerServiceEnvironment()}
	if err := winservice.Save(config); err != nil {
		return err
	}
	shortcut, err := winservice.ShortcutPath(self.name)
	if err == nil {
		err = writeWindowsShortcut(shortcut, executable, winservice.Arguments(self.name))
	}
	if err != nil {
		_ = winservice.Remove(self.name)
		return fmt.Errorf("couldn't add it to the Startup folder: %w", err)
	}
	if winservice.StartupDisabled(self.name) {
		fmt.Fprintf(out, "  %s Startup apps in Task Manager has %s turned off, so it won't start when you sign in.\n    Turn it on there (Task Manager, Startup apps).\n", cliYellow(out, "!"), self.name)
	}
	if err := self.control("start"); err != nil {
		_ = self.uninstall(io.Discard)
		return err
	}
	return nil
}

func writeWindowsShortcut(shortcut string, executable string, arguments string) error {
	name, args, err := winservice.ShortcutCommand(shortcut, executable, arguments, filepath.Dir(executable))
	if err != nil {
		return err
	}
	if out, err := cliRun(name, args...); err != nil {
		if out != "" {
			return fmt.Errorf("%s", firstLine(out))
		}
		return err
	}
	if _, err := os.Stat(shortcut); err != nil {
		return fmt.Errorf("PowerShell didn't write %s: %w", shortcut, err)
	}
	return nil
}

func (self *windowsService) uninstall(out io.Writer) error {
	stopErr := winservice.Stop(self.name, windowsServiceStopWait)
	shortcut, err := winservice.ShortcutPath(self.name)
	if err == nil {
		if err = os.Remove(shortcut); errors.Is(err, os.ErrNotExist) {
			err = nil
		}
	}
	if stopErr != nil || err != nil {
		return errors.Join(stopErr, err)
	}
	return winservice.Remove(self.name)
}

// How long a stop waits for the miner to end by itself; the supervisor gives
// its miner 30 seconds.
const windowsServiceStopWait = 45 * time.Second

func (self *windowsService) control(action string) error {
	switch action {
	case "stop":
		return winservice.Stop(self.name, windowsServiceStopWait)
	case "restart":
		if err := winservice.Stop(self.name, windowsServiceStopWait); err != nil {
			return err
		}
	}
	if self.active() {
		return nil
	}
	config := self.config()
	if config == nil {
		return errors.New("its settings are missing; install it again: provider service install")
	}
	if err := winservice.Launch(config.Executable, "service", "run", self.name); err != nil {
		return err
	}
	deadline := time.Now().Add(10 * time.Second)
	for !self.active() {
		if time.Now().After(deadline) {
			return fmt.Errorf("it didn't start; see %s", filepath.Join(config.StateDir, winservice.LogName))
		}
		time.Sleep(200 * time.Millisecond)
	}
	return nil
}

func (self *windowsService) logs(follow bool) error {
	_, stateDir := self.configured()
	if stateDir == "" {
		return errors.New("its settings are missing; install it again: provider service install")
	}
	path := filepath.Join(stateDir, winservice.LogName)
	if follow {
		fmt.Fprintf(os.Stdout, "Following %s (Ctrl-C to quit)\n", path)
	}
	err := winservice.Tail(path, 100, follow, os.Stdout, nil)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("there's no log yet at %s; it starts when the service runs", path)
	}
	return err
}

// minerServiceRun is the hidden "provider service run [<name>]" the Startup
// shortcut runs: the supervisor.
func minerServiceRun(args []string) (int, bool) {
	if len(args) < 2 || args[0] != "service" || args[1] != "run" {
		return 0, false
	}
	name := minerServiceDefaultName
	if len(args) == 3 {
		name = args[2]
	}
	if 3 < len(args) || !minerServiceNamePattern(name) {
		fmt.Fprintln(os.Stderr, "usage: provider service run [<name>] (the Startup shortcut runs this)")
		return 2, true
	}
	config, err := winservice.Load(name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s isn't installed (%s). Install it with: provider service install\n", name, err)
		return 1, true
	}
	if winservice.HasConsoleWindow() && os.Getenv("URNETWORK_SERVICE_DETACHED") == "" {
		// started from the shortcut, minimized: carry on without a window,
		// once (the supervisor drops URNETWORK_ settings from its miner's)
		os.Setenv("URNETWORK_SERVICE_DETACHED", "1")
		if err := winservice.Launch(config.Executable, "service", "run", name); err != nil {
			fmt.Fprintf(os.Stderr, "%s didn't start: %s\n", name, err)
			return 1, true
		}
		return 0, true
	}
	if err := os.MkdirAll(config.StateDir, 0700); err != nil {
		return 1, true
	}
	log, err := winservice.OpenLog(config.StateDir)
	if err != nil {
		return 1, true
	}
	defer log.Close()
	// the user's own environment, as at sign-in, not that of whichever shell
	// ran provider service start (systemd user units don't see a shell's)
	base := os.Environ()
	var token windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY|windows.TOKEN_DUPLICATE|windows.TOKEN_IMPERSONATE, &token); err == nil {
		if env, err := token.Environ(false); err == nil {
			base = env
		}
		token.Close()
	}
	code, err := winservice.NewSupervisor(*config, log).Run(winservice.Environment(base, *config, minerServiceDefaultName))
	if errors.Is(err, winservice.ErrRunning) {
		return 0, true
	}
	if err != nil {
		fmt.Fprintf(log, "%s service: %s\n", time.Now().Format(time.RFC3339), err)
	}
	return code, true
}

// minerServiceStopContext ends a supervised miner's run when its supervisor
// asks, as Ctrl-C does.
func minerServiceStopContext(ctx context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(ctx)
	if handle, ok := winservice.StopHandle(); ok {
		winservice.WaitStop(handle, cancel)
	}
	return ctx, cancel
}

// The URnetwork installer (urnetwork/connect scripts/Provider_Install_Win32.ps1)
// starts its miner from Startup\urnetwork.lnk with urnet-tools.ps1 and mines
// %USERPROFILE%\.urnetwork.

func windowsInstallerShortcut() string {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		return ""
	}
	path := filepath.Join(appData, "Microsoft", "Windows", "Start Menu", "Programs", "Startup", "urnetwork.lnk")
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	return path
}

// windowsInstallerTools is the installer's urnet-tools.ps1, in its default
// folder.
func windowsInstallerTools() string {
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData == "" {
		return ""
	}
	return filepath.Join(localAppData, "urnetwork", "provider", "urnet-tools.ps1")
}

func platformInstallerService(manager minerServiceManager) installerMiner {
	if _, ok := manager.(*windowsService); !ok {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	if ours, err := providerStateDir(); err != nil || !strings.EqualFold(filepath.Clean(ours), filepath.Join(home, ".urnetwork")) {
		return nil
	}
	other := &windowsInstaller{installer: minerInstaller{tools: windowsInstallerTools(), windows: true}}
	// without its shortcut (auto-start off) it may still be running
	if windowsInstallerShortcut() == "" && !other.active() {
		return nil
	}
	return other
}

// windowsInstaller drives the installer's miner through its urnet-tools.
type windowsInstaller struct {
	installer minerInstaller
}

func (self *windowsInstaller) kind() string  { return "the URnetwork installer's Startup shortcut" }
func (self *windowsInstaller) label() string { return "urnetwork" }
func (self *windowsInstaller) unit() string  { return "urnetwork.lnk" }
func (self *windowsInstaller) enabled() bool {
	return windowsInstallerShortcut() != "" || self.active()
}
func (self *windowsInstaller) installed() bool { return true }
func (self *windowsInstaller) foreign() string { return "" }

func (self *windowsInstaller) switchHint() string {
	return "This PC already runs the miner with the URnetwork installer (urnetwork.lnk in your Startup folder).\n  Keep using it: provider service start, stop, status and logs work on it, and urnet-tools updates it.\n  To switch to this command's service, turn the installer's off first (your identity is kept):\n  urnet-tools auto-start off; urnet-tools stop"
}

func (self *windowsInstaller) stopHint() string {
	return "urnet-tools auto-start off; urnet-tools stop"
}

func (self *windowsInstaller) configured() (string, string) {
	home, _ := os.UserHomeDir()
	return filepath.Join(filepath.Dir(self.installer.tools), "urnetwork.exe"), filepath.Join(home, ".urnetwork")
}

func (self *windowsInstaller) tools() error {
	if self.installer.tools == "" {
		return errors.New("can't find the installer's urnet-tools.ps1")
	}
	if _, err := os.Stat(self.installer.tools); err != nil {
		return fmt.Errorf("can't find the installer's urnet-tools.ps1 at %s; run urnet-tools yourself", self.installer.tools)
	}
	return nil
}

func (self *windowsInstaller) active() bool {
	if self.tools() != nil {
		return false
	}
	// urnet-tools status exits 0 only while the miner runs
	name, args := self.installer.command("status")
	_, err := cliRun(name, args...)
	return err == nil
}

func (self *windowsInstaller) control(action string) error {
	if err := self.tools(); err != nil {
		return err
	}
	name, args := self.installer.command(action)
	out, err := cliRun(name, args...)
	if err != nil && out != "" {
		return fmt.Errorf("%s", firstLine(out))
	}
	return err
}

func (self *windowsInstaller) logs(follow bool) error {
	if follow {
		if err := self.tools(); err != nil {
			return err
		}
		return self.installer.run("logs")
	}
	return winservice.Tail(filepath.Join(filepath.Dir(self.installer.tools), "provider.log"), 100, false, os.Stdout, nil)
}

func (self *windowsInstaller) install(string, string, io.Writer) error {
	return errors.New(self.switchHint())
}

func (self *windowsInstaller) uninstall(io.Writer) error {
	return errors.New("the URnetwork installer's miner is removed with urnet-tools uninstall; older installers also delete ~/.urnetwork with it, so to keep your identity turn it off with urnet-tools auto-start off; urnet-tools stop")
}
