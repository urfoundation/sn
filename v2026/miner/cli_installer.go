package miner

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// The official installers in urnetwork/connect scripts/ put urnet-tools beside
// the miner binary and own its updates: on Linux a systemd user timer
// (urnetwork-update.timer) runs `urnet-tools update`, on Windows a Startup
// shortcut runs urnetwork-updater.ps1. A binary they installed is updated
// through them, never swapped behind their back, so the installer's version
// file stays true and only one updater runs.

// minerInstaller is the official installer that manages a binary.
type minerInstaller struct {
	tools   string
	windows bool
}

// findMinerInstaller returns the installer managing this binary, if any.
func findMinerInstaller(executable string) *minerInstaller {
	if executable == "" {
		return nil
	}
	dir := filepath.Dir(executable)
	if runtime.GOOS == "windows" {
		tools := filepath.Join(dir, "urnet-tools.ps1")
		if info, err := os.Stat(tools); err == nil && info.Mode().IsRegular() {
			return &minerInstaller{tools: tools, windows: true}
		}
		return nil
	}
	tools := filepath.Join(dir, "urnet-tools")
	if info, err := os.Stat(tools); err == nil && info.Mode().IsRegular() {
		return &minerInstaller{tools: tools}
	}
	return nil
}

// command is how a person runs urnet-tools with these arguments.
func (self *minerInstaller) command(args ...string) (string, []string) {
	if self.windows {
		return "powershell.exe", append([]string{"-NoProfile", "-ExecutionPolicy", "Bypass", "-File", self.tools}, args...)
	}
	return self.tools, args
}

func (self *minerInstaller) display(args ...string) string {
	name := "urnet-tools"
	if self.windows {
		name = "urnet-tools.ps1"
	}
	return strings.Join(append([]string{name}, args...), " ")
}

func (self *minerInstaller) run(args ...string) error {
	name, all := self.command(args...)
	return cliRunAttached(name, all...)
}

func (self *minerInstaller) autoUpdateArgs(on bool) []string {
	switch {
	case self.windows && on:
		return []string{"auto-update-enable"}
	case self.windows:
		return []string{"auto-update-disable"}
	case on:
		return []string{"auto-update", "on"}
	}
	return []string{"auto-update", "off"}
}

// autoUpdate reports whether the installer's own update check is turned on.
func (self *minerInstaller) autoUpdate() (on bool, known bool) {
	if self.windows {
		appData := os.Getenv("APPDATA")
		if appData == "" {
			return false, false
		}
		_, err := os.Stat(filepath.Join(appData, "Microsoft", "Windows", "Start Menu", "Programs", "Startup", "urnetwork-update.lnk"))
		return err == nil, true
	}
	out, _ := cliRun("systemctl", "--user", "is-enabled", "urnetwork-update.timer")
	switch out {
	case "enabled":
		return true, true
	case "disabled", "masked", "not-found":
		return false, true
	}
	// no user systemd to ask, as over plain sudo or in a container
	return false, false
}
