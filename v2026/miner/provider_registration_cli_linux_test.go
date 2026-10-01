//go:build linux

// A subprocess enters the real public command and captures its actual stdout
// pipe. Only the bounded closed diagnostic explains refused startup.
package miner

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The command retains its historic zero completion status while emitting
// actionable, secret-free guidance for key/legacy/proxy admission failures.
func TestProviderRegistrationCliRefusalHasClosedGuidance(t *testing.T) {
	if os.Getenv("SN_TEST_PROVIDER_REFUSAL_CHILD") == "1" {
		Run([]string{"provide", "--api_url=" + os.Getenv("SN_TEST_PROVIDER_API"), "--connect_url=" + os.Getenv("SN_TEST_PROVIDER_CONNECT")})
		return
	}
	for _, phase := range []string{"fresh", "legacy", "duplicate"} {
		fixture := newProviderRegistrationFixture(t)
		privateHome := providerRegistrationPrivateDir(t)
		if phase == "legacy" {
			if err := os.WriteFile(filepath.Join(fixture.dir, ".provider.key"), bytes.Repeat([]byte{31}, 32), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(fixture.dir, ".provider.jwt"), []byte(providerRegistrationTestToken(t, "00000000-0000-0000-0000-000000000101", "legacy")), 0600); err != nil {
				t.Fatal(err)
			}
		}
		if phase == "duplicate" {
			dir := filepath.Join(privateHome, ".urnetwork")
			if err := os.Mkdir(dir, 0700); err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(ProxyConfig{Servers: map[string]string{"192.0.2.40:1080:synthetic-one:synthetic-password": "", "192.0.2.40:1080:synthetic-two:synthetic-password": ""}})
			if err != nil || os.WriteFile(filepath.Join(dir, "proxy"), raw, 0600) != nil {
				t.Fatal("could not retain synthetic duplicate proxy configuration")
			}
		}
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestProviderRegistrationCliRefusalHasClosedGuidance$", "-test.timeout=30s")
		command.Env = append(os.Environ(), "SN_TEST_PROVIDER_REFUSAL_CHILD=1", "HOME="+privateHome, "URNETWORK_STATE_DIR="+fixture.dir, "WARP_VERSION=synthetic-test", "SN_TEST_PROVIDER_API="+fixture.server.URL, "SN_TEST_PROVIDER_CONNECT=ws"+strings.TrimPrefix(fixture.server.URL, "http"))
		var stdout, stderr bytes.Buffer
		command.Stdout, command.Stderr = &stdout, &stderr
		err := command.Run()
		cancel()
		posts, legacy, _, refreshed := fixture.counts()
		leaked := bytes.Contains(stdout.Bytes(), []byte(fixture.dir)) || bytes.Contains(stdout.Bytes(), []byte(privateHome)) || bytes.Contains(stdout.Bytes(), []byte("synthetic-password"))
		for _, name := range []string{"jwt", ".provider.jwt"} {
			if token, readErr := os.ReadFile(filepath.Join(fixture.dir, name)); readErr == nil && len(token) != 0 && bytes.Contains(stdout.Bytes(), token) {
				leaked = true
			}
		}
		if err != nil || !bytes.Contains(stdout.Bytes(), []byte(`"event":"startup_recovery_required"`)) || !bytes.Contains(stdout.Bytes(), []byte("--allow-client-registration")) || !bytes.Contains(stdout.Bytes(), []byte("--adopt-legacy-provider-key")) || leaked || posts != 0 || legacy != 0 || refreshed != 0 {
			t.Fatalf("actual provide command lost bounded custody recovery guidance: phase=%s error=%v stdout=%s stderr=%s", phase, err, stdout.String(), stderr.String())
		}
	}
}
