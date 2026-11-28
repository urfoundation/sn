package miner

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/docopt/docopt-go"
)

// packagedBinary lays out a package's binary under root at bin (a path
// relative to root) with marker (nil for none) as its managed-by file.
func packagedBinary(t *testing.T, root string, bin string, marker []byte) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(bin))
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho packaged\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if marker != nil {
		file := filepath.Join(filepath.Dir(filepath.Dir(path)), "share", "urnetwork-provider", "managed-by")
		if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, marker, 0644); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

// The layouts of contract section 4: the deb and rpm in /usr, Homebrew's keg,
// Chocolatey's package folder.
func TestFindMinerPackageManagerReadsTheMarker(t *testing.T) {
	for _, c := range []struct {
		bin, marker, command string
	}{
		{"usr/bin/urnetwork-provider", "sudo apt-get install --only-upgrade urnetwork-provider\n", "sudo apt-get install --only-upgrade urnetwork-provider"},
		{"usr/bin/urnetwork-provider", "\n  sudo dnf upgrade urnetwork-provider  \nignored\n", "sudo dnf upgrade urnetwork-provider"},
		{"opt/homebrew/Cellar/urnetwork-provider/2026.11.22-1105365180/bin/urnetwork-provider", "brew upgrade urnetwork-provider\n", "brew upgrade urnetwork-provider"},
		{"ProgramData/chocolatey/lib/urnetwork-provider/tools/urnetwork-provider.exe", "choco upgrade urnetwork-provider\r\n", "choco upgrade urnetwork-provider"},
	} {
		executable := packagedBinary(t, t.TempDir(), c.bin, []byte(c.marker))
		for _, goos := range []string{"linux", "darwin", "windows"} {
			manager := findMinerPackageManager(executable, goos)
			if manager == nil || manager.command != c.command {
				t.Errorf("%s on %s: %+v, want %q", c.bin, goos, manager, c.command)
			}
		}
	}
}

func TestFindMinerPackageManagerWithoutAMarker(t *testing.T) {
	root := t.TempDir()
	for _, executable := range []string{
		// the official installer's layout, and a binary a person put anywhere
		packagedBinary(t, root, "home/u/.local/share/urnetwork-provider/bin/urnetwork", nil),
		packagedBinary(t, root, "srv/provider", nil),
		// an empty marker names no command
		packagedBinary(t, t.TempDir(), "usr/bin/urnetwork-provider", []byte("\n  \n")),
		"",
	} {
		for _, goos := range []string{"linux", "darwin", "windows"} {
			if manager := findMinerPackageManager(executable, goos); manager != nil {
				t.Errorf("%q on %s is managed by %+v", executable, goos, manager)
			}
		}
	}
}

func TestFindMinerPackageManagerWinget(t *testing.T) {
	for _, executable := range []string{
		`C:\Users\u\AppData\Local\Microsoft\WinGet\Packages\URnetwork.Provider_Microsoft.Winget.Source_8wekyb3d8bbwe\urnetwork-provider.exe`,
		`C:\Program Files\WinGet\Packages\URnetwork.Provider_Microsoft.Winget.Source_8wekyb3d8bbwe\urnetwork-provider.exe`,
	} {
		manager := findMinerPackageManager(executable, "windows")
		if manager == nil || manager.command != "winget upgrade --id URnetwork.Provider" {
			t.Errorf("%s: %+v", executable, manager)
		}
		if manager := findMinerPackageManager(executable, "linux"); manager != nil {
			t.Errorf("the winget rule applied off Windows: %+v", manager)
		}
	}
	if manager := findMinerPackageManager(`C:\Users\u\AppData\Local\Microsoft\WinGet\Packages\Example.Other_x\provider.exe`, "windows"); manager != nil {
		t.Errorf("another winget package: %+v", manager)
	}
}

func TestPackageManagerUpdateCmdOnlyReports(t *testing.T) {
	manager := &minerPackageManager{command: "brew upgrade urnetwork-provider"}
	for _, c := range []struct {
		opts docopt.Opts
		code int
		want string
	}{
		{docopt.Opts{"--auto": true}, 0, "automatic update: skipped, its package manager updates this miner (brew upgrade urnetwork-provider)"},
		{docopt.Opts{"--status": true}, 0, "Update with        brew upgrade urnetwork-provider"},
		{docopt.Opts{"--on": true}, 0, "nothing to turn on or off"},
		{docopt.Opts{"--off": true}, 0, "Update it with: brew upgrade urnetwork-provider"},
		{docopt.Opts{"--check": true}, 0, "updates it: brew upgrade urnetwork-provider"},
		{docopt.Opts{}, 1, "Not updated here"},
	} {
		var out bytes.Buffer
		if code := packageManagerUpdateCmd(c.opts, manager, &out); code != c.code || !strings.Contains(out.String(), c.want) {
			t.Errorf("%v: exit %d, %q; want %d, %q", c.opts, code, out.String(), c.code, c.want)
		}
	}
}

// The composed path: provider update, by hand or from the service's timer, on
// a package-managed binary changes nothing, leaves no update state behind and
// names the package manager's command; before this change it replaced the
// binary from the release feed.
func TestUpdateCmdNeverReplacesAPackageManagedBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake binary is a shell script")
	}
	state := phase2State(t)
	executable := packagedBinary(t, t.TempDir(), "usr/bin/urnetwork-provider", []byte("sudo apt-get install --only-upgrade urnetwork-provider\n"))
	before, _ := os.ReadFile(executable)
	cliExecutable = func() (string, error) { return executable, nil }
	t.Cleanup(func() { cliExecutable = defaultCliExecutable })
	// a newer release is out
	t.Setenv("URNETWORK_UPDATE_URL", fakeRelease(t, "2099.1.1-9", false, true))
	for _, c := range []struct {
		opts docopt.Opts
		code int
	}{
		{docopt.Opts{}, 1},
		{docopt.Opts{"--auto": true}, 0},
	} {
		var out, errOut bytes.Buffer
		if code := updateCmd(c.opts, &out, &errOut); code != c.code || !strings.Contains(out.String(), "sudo apt-get install --only-upgrade urnetwork-provider") {
			t.Fatalf("%v: exit %d\n%s%s", c.opts, code, out.String(), errOut.String())
		}
	}
	if after, _ := os.ReadFile(executable); !bytes.Equal(before, after) {
		t.Fatal("the package-managed binary was replaced")
	}
	if _, err := os.Stat(executable + ".previous"); err == nil {
		t.Fatal("an update left a .previous copy")
	}
	if _, err := os.Stat(filepath.Join(state, minerUpdateFileName)); err == nil {
		t.Fatal("a package-managed miner recorded an update check")
	}
}

// status and the release check name the package manager, not provider update.
func TestStatusAndReleaseCheckNameThePackageManager(t *testing.T) {
	phase2State(t)
	executable := packagedBinary(t, t.TempDir(), "usr/bin/urnetwork-provider", []byte("sudo dnf upgrade urnetwork-provider\n"))
	cliExecutable = func() (string, error) { return executable, nil }
	t.Cleanup(func() { cliExecutable = defaultCliExecutable })
	report, _ := buildMinerStatusReport(time.Now())
	if report.Update.PackageManager != "sudo dnf upgrade urnetwork-provider" || report.Update.Auto {
		t.Fatalf("status update report %+v", report.Update)
	}
	updater := readMinerUpdater("")
	release := &minerRelease{TagName: "v2099.1.1-9", PublishedAt: time.Now().Add(-time.Hour)}
	finding := minerUpdateFinding("2026.11.22+1105365180", release, updater, "", time.Now(), defaultMinerReleaseCheckSettings())
	if finding == nil || finding.Fix != "sudo dnf upgrade urnetwork-provider" || !strings.Contains(finding.Summary, "package manager") {
		t.Fatalf("finding %+v", finding)
	}
}
