package miner

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/docopt/docopt-go"
)

// A package manager that installed this binary (Homebrew, apt, dnf, winget,
// Chocolatey) also updates it, so provider update and the service's
// automatic update never replace a binary one manages: they say which command
// updates it instead. The packages install the binary as urnetwork-provider
// and mark it with <prefix>/share/urnetwork-provider/managed-by, where
// <prefix> is the parent of the binary's directory (/usr for
// /usr/bin/urnetwork-provider, the keg for Homebrew, the package folder for
// Chocolatey's tools\ folder). The file's first non-empty line is the update
// command; it is printed, never run. winget's portable installs can carry no
// such file, so a binary under winget's package folder for URnetwork.Provider
// is winget's. A binary with neither behaves as it always has.

// minerManagedByMaxBytes bounds how much of a marker file is read.
const minerManagedByMaxBytes = 4096

// minerWingetPackageFolder is the part of a winget portable install's path
// that names the package (%LOCALAPPDATA% or %ProgramFiles%
// \WinGet\Packages\URnetwork.Provider_<source>\), lowercased with forward
// slashes.
const minerWingetPackageFolder = "/winget/packages/urnetwork.provider_"

// minerPackageManager is the package manager that manages a binary.
type minerPackageManager struct {
	// command updates the binary, e.g. "brew upgrade urnetwork-provider"
	command string
}

// findMinerPackageManager returns the package manager managing executable (a
// path with symlinks resolved, as cliExecutable returns it) on goos, if any.
func findMinerPackageManager(executable string, goos string) *minerPackageManager {
	if executable == "" {
		return nil
	}
	prefix := filepath.Dir(filepath.Dir(executable))
	marker := filepath.Join(prefix, "share", "urnetwork-provider", "managed-by")
	if f, err := os.Open(marker); err == nil {
		data, _ := io.ReadAll(io.LimitReader(f, minerManagedByMaxBytes))
		f.Close()
		for _, line := range strings.Split(string(data), "\n") {
			if line = strings.TrimSpace(line); line != "" {
				return &minerPackageManager{command: line}
			}
		}
	}
	if goos == "windows" && strings.Contains(strings.ToLower(strings.ReplaceAll(executable, `\`, "/")), minerWingetPackageFolder) {
		return &minerPackageManager{command: "winget upgrade --id URnetwork.Provider"}
	}
	return nil
}

// packageManagerUpdateCmd answers provider update for a package-managed
// binary: it reports and never replaces anything.
func packageManagerUpdateCmd(opts docopt.Opts, manager *minerPackageManager, stdout io.Writer) int {
	switch {
	case cliOptBool(opts, "--auto"):
		fmt.Fprintf(stdout, "automatic update: skipped, its package manager updates this miner (%s)\n", manager.command)
		return 0
	case cliOptBool(opts, "--status"):
		row := func(label string, value string) { fmt.Fprintf(stdout, "  %-18s %s\n", label, value) }
		row("Automatic updates", "by the package manager that installed this miner")
		row("Update with", manager.command)
		return 0
	case cliOptBool(opts, "--on"), cliOptBool(opts, "--off"):
		fmt.Fprintf(stdout, "  The package manager that installed this miner updates it, so there is nothing to turn on or off here.\n")
		fmt.Fprintf(stdout, "  Update it with: %s\n", manager.command)
		return 0
	case cliOptBool(opts, "--check"):
		fmt.Fprintf(stdout, "  The package manager that installed this miner updates it: %s\n", manager.command)
		return 0
	}
	fmt.Fprintf(stdout, "  Not updated here: the package manager that installed this miner updates it.\n")
	fmt.Fprintf(stdout, "  Update it with: %s\n", manager.command)
	return 1
}
