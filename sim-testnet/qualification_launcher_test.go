//go:build linux || darwin

package main

// The qualification wrapper fixes two observer-only launch failures: a frozen
// 0644 script cannot execute directly, and a relative source inventory cannot
// be checked from its capture directory. Real child exits remain authoritative.

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// Every file is owned by this test; names with spaces exercise argv quoting.
// No Go toolchain, service, wallet or product workload is replaced by fixtures.
func qualificationLauncherFixture(t *testing.T, body string) (source, manifest, runner string) {
	t.Helper()
	base := t.TempDir()
	source = filepath.Join(base, "source with spaces")
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatal(err)
	}
	raw := []byte("test-owned source version one\n")
	if err := os.WriteFile(filepath.Join(source, "input.txt"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	manifest = filepath.Join(base, "source inventory.sha256")
	if err := os.WriteFile(manifest, []byte(fmt.Sprintf("%x  input.txt\n", sha256.Sum256(raw))), 0o600); err != nil {
		t.Fatal(err)
	}
	runner = filepath.Join(base, "frozen runner.sh")
	if err := os.WriteFile(runner, []byte("#!/usr/bin/env bash\n"+body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(runner, 0o644); err != nil {
		t.Fatal(err)
	}
	return source, manifest, runner
}

// The wrapper itself is also invoked through Bash and may be non-executable.
// Starting outside the source tree deliberately reproduces the caller layout.
func qualificationLauncherCommand(t *testing.T, source, manifest, runner string) *exec.Cmd {
	t.Helper()
	launcher, err := filepath.Abs("../scripts/run-qualification-capture.sh")
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("bash", launcher, source, manifest, runner)
	command.Dir = filepath.Dir(manifest)
	return command
}

// First reproduce direct execution's actual EACCES, then show the checked-in
// wrapper reaches that same immutable 0644 script and preserves its exit17.
func TestQualificationLauncherRunsNonExecutableFrozenBody(t *testing.T) {
	source, manifest, runner := qualificationLauncherFixture(t, "printf '%s\\n' \"$PWD\"\nexit 17\n")
	before, err := os.ReadFile(runner)
	if err != nil {
		t.Fatal(err)
	}
	if err := exec.Command(runner).Run(); !errors.Is(err, syscall.EACCES) {
		t.Fatalf("direct execution did not reproduce the mode failure: %v", err)
	}
	output, err := qualificationLauncherCommand(t, source, manifest, runner).CombinedOutput()
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) || exitError.ExitCode() != 17 || strings.TrimSpace(string(output)) != source {
		t.Fatalf("frozen body exit/output differs: %v %q", err, output)
	}
	after, readErr := os.ReadFile(runner)
	info, statErr := os.Stat(runner)
	if readErr != nil || statErr != nil || !bytes.Equal(before, after) || info.Mode().Perm() != 0o644 {
		t.Fatalf("wrapper modified its immutable input: %v/%v", readErr, statErr)
	}
}

// A relative inventory demonstrably fails from the capture parent but passes
// through the wrapper's source-root transition before the body is admitted.
func TestQualificationLauncherChecksManifestFromSourceRoot(t *testing.T) {
	source, manifest, runner := qualificationLauncherFixture(t, "printf 'runner-entered\\n'\n")
	wrongDirectory := exec.Command("sha256sum", "--check", "--strict", "--quiet", manifest)
	wrongDirectory.Dir = filepath.Dir(manifest)
	if output, err := wrongDirectory.CombinedOutput(); err == nil || !bytes.Contains(output, []byte("input.txt")) {
		t.Fatalf("capture-directory check did not reproduce relative lookup failure: %v %q", err, output)
	}
	output, err := qualificationLauncherCommand(t, source, manifest, runner).CombinedOutput()
	if err != nil || string(output) != "runner-entered\n" {
		t.Fatalf("source-root qualification did not enter its body: %v %q", err, output)
	}
}

// A real digest mismatch must stop before running even a well-formed body.
func TestQualificationLauncherRejectsChangedSourceBeforeBody(t *testing.T) {
	source, manifest, runner := qualificationLauncherFixture(t, "printf 'runner-entered\\n'\n")
	if err := os.WriteFile(filepath.Join(source, "input.txt"), []byte("changed test source\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	output, err := qualificationLauncherCommand(t, source, manifest, runner).CombinedOutput()
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) || exitError.ExitCode() != 1 || !bytes.Contains(output, []byte("FAILED")) || bytes.Contains(output, []byte("runner-entered")) {
		t.Fatalf("source mismatch reached the body or hid its failure: %v %q", err, output)
	}
}

// Missing and relative coordinates cannot silently choose another source or
// runner through the caller's cwd; none of these failures may enter the body.
func TestQualificationLauncherRejectsAmbiguousOrMissingInputs(t *testing.T) {
	source, manifest, runner := qualificationLauncherFixture(t, "printf 'runner-entered\\n'\n")
	for _, input := range []struct {
		source, manifest, runner string
		code                     int
	}{
		{source: ".", manifest: manifest, runner: runner, code: 64},
		{source: source, manifest: "source.sha256", runner: runner, code: 64},
		{source: source, manifest: manifest, runner: "runner.sh", code: 64},
		{source: source + "-missing", manifest: manifest, runner: runner, code: 66},
		{source: source, manifest: manifest + "-missing", runner: runner, code: 66},
		{source: source, manifest: manifest, runner: runner + "-missing", code: 66},
	} {
		output, err := qualificationLauncherCommand(t, input.source, input.manifest, input.runner).CombinedOutput()
		var exitError *exec.ExitError
		if !errors.As(err, &exitError) || exitError.ExitCode() != input.code || bytes.Contains(output, []byte("runner-entered")) {
			t.Fatalf("invalid qualification coordinates did not fail closed: %v %q", err, output)
		}
	}
}
