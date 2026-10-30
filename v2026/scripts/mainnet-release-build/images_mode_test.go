// Image context permissions must not inherit the build host's private umask.
package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// A child owns each process-global umask. Both the executable image input and
// its private metadata must retain their declared mode and exact source bytes.
func TestImageInputCopiesHonorDeclaredPermissions(t *testing.T) {
	const childMask = "SN_TEST_RELEASE_COPY_UMASK"
	if value, exists := os.LookupEnv(childMask); exists {
		mask, err := strconv.ParseUint(value, 8, 9)
		if err != nil {
			t.Fatal(err)
		}
		directory := t.TempDir()
		source := filepath.Join(directory, "synthetic-source")
		raw := []byte("synthetic immutable release input")
		if err := os.WriteFile(source, raw, 0600); err != nil {
			t.Fatal(err)
		}
		original, err := os.Stat(source)
		if err != nil {
			t.Fatal(err)
		}
		prior := syscall.Umask(int(mask))
		defer syscall.Umask(prior)
		for _, mode := range []os.FileMode{0755, 0600} {
			target := filepath.Join(directory, fmt.Sprintf("copy-%o", mode))
			artifact, err := copyBuildFile(source, target, mode)
			if err != nil {
				t.Fatalf("mask %03o mode %04o copy failed: %v", mask, mode, err)
			}
			actual, err := os.Stat(target)
			if err != nil || actual.Mode().Perm() != mode {
				t.Fatalf("mask %03o requested mode %04o, stat=%v error=%v", mask, mode, actual, err)
			}
			copied, err := os.ReadFile(target)
			if err != nil || !bytes.Equal(copied, raw) || artifact.Bytes != int64(len(raw)) || artifact.Sha256 != buildFixtureDigest(string(raw)) {
				t.Fatalf("mask %03o changed exact source bytes: %v", mask, err)
			}
			if _, err := copyBuildFile(source, target, 0644); err == nil {
				t.Fatal("copy overwrote an occupied target")
			}
			retained, err := os.Stat(target)
			if err != nil || retained.Mode() != actual.Mode() {
				t.Fatalf("refused overwrite changed target permissions: %v", err)
			}
		}
		after, err := os.Stat(source)
		if err != nil || after.Mode() != original.Mode() {
			t.Fatalf("copy changed original source permissions: %v", err)
		}
		return
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mask := range []string{"000", "077", "777"} {
		command := exec.CommandContext(t.Context(), executable, "-test.run=^TestImageInputCopiesHonorDeclaredPermissions$", "-test.count=1")
		for _, variable := range os.Environ() {
			if !strings.HasPrefix(variable, childMask+"=") {
				command.Env = append(command.Env, variable)
			}
		}
		command.Env = append(command.Env, childMask+"="+mask)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("isolated umask %s failed: %v\n%s", mask, err, output)
		}
	}
}
