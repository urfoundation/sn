//go:build linux || darwin

// The actual executable path exposes complete-profile refusal as failure and
// reaches no auth, wallet or provider allocation path before that admission.
package miner

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// An explicit child marker makes os.Exit observable without replacing Run.
func TestProviderWholeWorkRequiredCliRefusesBeforeAuthProvide(t *testing.T) {
	if command := os.Getenv("SN_TEST_WHOLE_WORK_CLI_CHILD"); command != "" {
		args := []string{command, "--require-whole-work-capture", "--api_url=" + os.Getenv("SN_TEST_WHOLE_WORK_API")}
		if command == "auth-provide" {
			args = append(args, "--user_auth=synthetic-provider", "--password=synthetic-password")
		}
		Run(args)
		return
	}
	for _, command := range []string{"provide", "auth-provide"} {
		fixture := newProviderRegistrationFixture(t)
		privateHome := providerRegistrationPrivateDir(t)
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestProviderWholeWorkRequiredCliRefusesBeforeAuthProvide$", "-test.timeout=30s")
		child.Env = append(os.Environ(), "SN_TEST_WHOLE_WORK_CLI_CHILD="+command, "SN_TEST_WHOLE_WORK_API="+fixture.server.URL, "HOME="+privateHome, "URNETWORK_STATE_DIR="+fixture.dir, "WARP_VERSION=synthetic-test")
		var stdout, stderr bytes.Buffer
		child.Stdout, child.Stderr = &stdout, &stderr
		err := child.Run()
		cancel()
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 || !bytes.Contains(stderr.Bytes(), []byte("whole-work launch requires its reviewed profile")) || bytes.Contains(stderr.Bytes(), []byte("synthetic-password")) {
			t.Fatalf("actual complete-profile command did not refuse admission: command=%s error=%v stdout=%s stderr=%s", command, err, stdout.String(), stderr.String())
		}
		posts, legacy, allocations, refreshes := fixture.counts()
		if posts != 0 || legacy != 0 || allocations != 0 || refreshes != 0 {
			t.Fatal("complete-profile refusal reached registration or auth HTTP")
		}
		if _, err := os.Stat(filepath.Join(fixture.dir, ".provider.key")); !os.IsNotExist(err) {
			t.Fatal("complete-profile refusal created provider key custody", err)
		}
	}
}
