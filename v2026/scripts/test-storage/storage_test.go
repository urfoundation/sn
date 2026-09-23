// Exercise the storage bootstrap through real command environments, without
// requiring this developer/test host to have the finalization data volume.
package teststorage

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Each command uses its own explicit synthetic storage root. The adapter must
// override inherited scratch paths before starting the actual tool.
func storageCommand(t *testing.T, root string, arguments ...string) *exec.Cmd {
	t.Helper()
	command := exec.CommandContext(t.Context(), "bash", append([]string{"../with-test-storage.sh"}, arguments...)...)
	command.Env = append(os.Environ(), "SN_TEST_STORAGE_ROOT="+root,
		"TMPDIR=/synthetic-inherited/tmp", "GOTMPDIR=/synthetic-inherited/gotmp",
		"TMP=/synthetic-inherited/tmp", "TEMP=/synthetic-inherited/tmp",
		"GOCACHE=/synthetic-inherited/gocache", "GOMODCACHE=/synthetic-inherited/gomodcache",
		"XDG_CACHE_HOME=/synthetic-inherited/cache", "CARGO_TARGET_DIR=/synthetic-inherited/target",
		"RUNTIME_METADATA_PROBE_TARGET_DIR=/synthetic-inherited/probe")
	return command
}

// Two launches share only reusable caches, while each tool receives already
// created private temporary/build directories, regardless of inherited paths.
func TestTestStorageCreatesPrivateOwnersAndWarmCachePaths(t *testing.T) {
	root := filepath.Join(t.TempDir(), "storage with spaces and $literal")
	previousOwner := ""
	for iteration := range 2 {
		command := storageCommand(t, root, "bash", "-c", `printf '%s\n' "$SN_TEST_STORAGE_ROOT" "$SN_TEST_COMMAND_ROOT" "$TMPDIR" "$GOTMPDIR" "$TMP" "$TEMP" "$GOCACHE" "$GOMODCACHE" "$XDG_CACHE_HOME" "$CARGO_TARGET_DIR" "$RUNTIME_METADATA_PROBE_TARGET_DIR"`)
		var stderr bytes.Buffer
		command.Stderr = &stderr
		output, err := command.Output()
		if err != nil {
			t.Fatalf("storage bootstrap: %v\n%s", err, stderr.String())
		}
		fields := strings.Split(strings.TrimSpace(string(output)), "\n")
		if len(fields) != 11 {
			t.Fatalf("storage bootstrap returned %d fields", len(fields))
		}
		canonicalRoot, err := filepath.EvalSymlinks(root)
		if err != nil {
			t.Fatal(err)
		}
		owner := fields[1]
		if fields[0] != canonicalRoot || filepath.Dir(owner) != filepath.Join(canonicalRoot, "temp") || owner == previousOwner {
			t.Fatalf("command scratch is not independently owned: %q", fields[:2])
		}
		previousOwner = owner
		expected := []string{filepath.Join(owner, "tmp"), filepath.Join(owner, "gotmp"), filepath.Join(owner, "tmp"), filepath.Join(owner, "tmp"), filepath.Join(canonicalRoot, "gocache"), filepath.Join(canonicalRoot, "gomodcache"), filepath.Join(canonicalRoot, "cache"), filepath.Join(owner, "cargo-target"), filepath.Join(canonicalRoot, "cache", "runtime-metadata-probe-target")}
		for index, want := range expected {
			if fields[index+2] != want {
				t.Fatalf("storage field %d = %q, want %q", index+2, fields[index+2], want)
			}
		}
		for _, path := range []string{owner, fields[2], fields[3], fields[6], fields[7], fields[8]} {
			info, err := os.Stat(path)
			if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
				t.Fatalf("command path is absent or not private: %s: %v", path, err)
			}
		}
		if !strings.Contains(stderr.String(), "[test storage] root="+canonicalRoot+"; owner="+owner) {
			t.Fatalf("storage selection was not observable: %s", stderr.String())
		}
		cacheMarker := filepath.Join(fields[6], "retained-cache-entry")
		if previous, err := os.ReadFile(cacheMarker); iteration > 0 && (err != nil || string(previous) != "warm") {
			t.Fatal("warm cache content was removed or changed", err)
		}
		if err := os.WriteFile(cacheMarker, []byte("warm"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// The mount refusal is forced by a shell boundary; it neither depends on this
// host's actual mounts nor creates a fallback directory on its root volume.
func TestTestStorageRefusesMissingDefaultMount(t *testing.T) {
	for _, root := range []string{"", "/mnt/data/synthetic-storage-root"} {
		command := exec.CommandContext(t.Context(), "bash", "-c", `set -euo pipefail
source ../test-storage.sh
mountpoint() { return 1; }
mkdir() { printf 'unexpected storage write\n'; return 1; }
sn_test_storage_init
printf 'unexpected command admission\n'
`)
		command.Env = append(os.Environ(), "SN_TEST_STORAGE_ROOT="+root)
		output, err := command.CombinedOutput()
		if err == nil || !strings.Contains(string(output), "is not mounted") || strings.Contains(string(output), "unexpected") {
			t.Fatalf("missing volume did not refuse before command admission: %v\n%s", err, output)
		}
	}
}

// Portable developer/CI storage is explicit, and malformed or impossible
// roots fail before the wrapped command can perform any work.
func TestTestStorageRefusesInvalidRootsBeforeExecution(t *testing.T) {
	blocked := filepath.Join(t.TempDir(), "regular-file")
	if err := os.WriteFile(blocked, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{"relative-storage", filepath.Join(blocked, "child")} {
		command := storageCommand(t, root, "bash", "-c", "printf 'unexpected command admission\\n'")
		output, err := command.CombinedOutput()
		if err == nil || strings.Contains(string(output), "unexpected command admission") {
			t.Fatalf("invalid storage %q admitted a command: %v\n%s", root, err, output)
		}
	}
	if raw, err := os.ReadFile(blocked); err != nil || string(raw) != "original" {
		t.Fatal("invalid destination modified the existing file", err)
	}
}

// Quoting, exit status and the caller's output modes survive the environment
// adapter. No shell evaluation is applied to the wrapped arguments.
func TestTestStoragePreservesArgumentsExitAndUmask(t *testing.T) {
	root := t.TempDir()
	command := exec.CommandContext(t.Context(), "bash", "-c", `umask 0027; exec bash ../with-test-storage.sh bash -c 'umask; printf "%s\n" "$@"; exit 37' synthetic-command "$@"`, "storage-wrapper", "with spaces", "$(literal-command)", "quote'and\"double")
	command.Env = append(os.Environ(), "SN_TEST_STORAGE_ROOT="+root)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 37 || string(output) != "0027\nwith spaces\n$(literal-command)\nquote'and\"double\n" {
		t.Fatalf("wrapper changed command semantics: %v\n%s\n%s", err, output, stderr.String())
	}
}

// Exercise both real entrypoints: storage must be admitted before repository
// preflights, compilers, service owners or qualification jobs can start.
func TestReleaseGateEntrypointsRefuseInvalidStorageBeforePreflight(t *testing.T) {
	for _, script := range []string{"../test-release-1.0-local.sh", "../test-release-1.0-producer-gate.sh"} {
		command := exec.CommandContext(t.Context(), "bash", script)
		command.Env = append(os.Environ(), "SN_TEST_STORAGE_ROOT=relative-storage")
		output, err := command.CombinedOutput()
		if err == nil || !strings.Contains(string(output), "SN_TEST_STORAGE_ROOT must be absolute") || strings.Contains(string(output), "preflight") {
			t.Fatalf("gate %s bypassed early storage admission: %v\n%s", script, err, output)
		}
	}
}
