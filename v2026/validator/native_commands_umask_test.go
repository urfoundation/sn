//go:build linux || darwin

// Private seed fixtures must not inherit the host's shared directory policy.
package validator

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"testing"
	"time"
)

// Only a subprocess running this single test changes the process-wide umask;
// unrelated parallel tests retain their own creation policy.
func TestInitPrivateSeedsAcrossUmasks(t *testing.T) {
	const umaskEnvironment = "SN_TEST_PRIVATE_SEED_UMASK"
	if value := os.Getenv(umaskEnvironment); value != "" {
		mask, err := strconv.ParseUint(value, 8, 9)
		if err != nil {
			t.Fatal(err)
		}
		previous := syscall.Umask(int(mask))
		defer syscall.Umask(previous)
		assertInitCreatesPrivateSeeds(t)
		return
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mask := range []string{"0000", "0002", "0022", "0077"} {
		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		command := exec.CommandContext(ctx, executable, "-test.run=^TestInitPrivateSeedsAcrossUmasks$", "-test.count=1", "-test.timeout=45s")
		command.Env = append(os.Environ(), umaskEnvironment+"="+mask)
		output, err := command.CombinedOutput()
		cancel()
		if err != nil {
			t.Fatalf("umask %s: %v\n%s", mask, err, output)
		}
	}
}
