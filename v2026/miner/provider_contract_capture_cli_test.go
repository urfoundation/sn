//go:build linux || darwin

// The real executable refusal precedes login, registration and key creation.
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

func TestProviderOriginalContractRequiredCliRefusesBeforeAuthProvide(t *testing.T) {
	if command := os.Getenv("SN_TEST_ORIGINAL_CONTRACT_CLI_CHILD"); command != "" {
		args := []string{command, "--require-original-contract-capture", "--api_url=" + os.Getenv("SN_TEST_ORIGINAL_CONTRACT_API")}
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
		child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestProviderOriginalContractRequiredCliRefusesBeforeAuthProvide$", "-test.timeout=30s")
		child.Env = append(os.Environ(), "SN_TEST_ORIGINAL_CONTRACT_CLI_CHILD="+command, "SN_TEST_ORIGINAL_CONTRACT_API="+fixture.server.URL, "HOME="+privateHome, "URNETWORK_STATE_DIR="+fixture.dir, "WARP_VERSION=synthetic-test")
		var stdout, stderr bytes.Buffer
		child.Stdout, child.Stderr = &stdout, &stderr
		err := child.Run()
		cancel()
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 || !bytes.Contains(stderr.Bytes(), []byte("original contract launch requires its approved source profile")) || bytes.Contains(stderr.Bytes(), []byte("synthetic-password")) {
			t.Fatalf("actual original-source command did not refuse admission: command=%s error=%v stdout=%s stderr=%s", command, err, stdout.String(), stderr.String())
		}
		posts, legacy, allocations, refreshes := fixture.counts()
		if posts != 0 || legacy != 0 || allocations != 0 || refreshes != 0 {
			t.Fatal("missing original source reached registration or authentication HTTP")
		}
		if _, err := os.Stat(filepath.Join(fixture.dir, ".provider.key")); !os.IsNotExist(err) {
			t.Fatal("original source refusal created provider key custody", err)
		}
	}
}
