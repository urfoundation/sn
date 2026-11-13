//go:build linux

// Public commands share one selected directory. Private subprocess homes and
// synthetic local HTTP responses isolate both legacy files and authentication.
package miner

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urnetwork/sdk/v2026"
)

// Only the child enters the CLI's signal and exit lifecycle. The same network
// credential reader used by provider commands checks the auth result there.
func TestMinerStateDirectoryCliChild(t *testing.T) {
	if os.Getenv("SN_TEST_STATE_DIRECTORY_CHILD") != "1" {
		return
	}
	var args []string
	if err := json.Unmarshal([]byte(os.Getenv("SN_TEST_STATE_DIRECTORY_ARGS")), &args); err != nil {
		t.Fatal(err)
	}
	Run(args)
	if expected := os.Getenv("SN_TEST_STATE_DIRECTORY_TOKEN"); expected != "" {
		actual, err := readNetworkJwt()
		if err != nil || actual != expected {
			t.Fatal("auth and provider commands selected different bootstrap credentials", err)
		}
	}
}

// Child-only environment selection cannot redirect another test's files.
func runMinerStateDirectoryCli(t *testing.T, directory, privateHome, state, input, expectedToken string, args []string) string {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "-test.run=^TestMinerStateDirectoryCliChild$", "-test.timeout=15s")
	command.Dir = directory
	command.Env = append(os.Environ(), "SN_TEST_STATE_DIRECTORY_CHILD=1", "SN_TEST_STATE_DIRECTORY_ARGS="+string(raw), "SN_TEST_STATE_DIRECTORY_TOKEN="+expectedToken,
		"HOME="+privateHome, "URNETWORK_STATE_DIR="+state, "WARP_VERSION=synthetic-state-directory", "HTTP_PROXY=", "HTTPS_PROXY=", "ALL_PROXY=", "NO_PROXY=*")
	command.Stdin = strings.NewReader(input)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("selected-directory command failed: %v\n%s", err, output)
	}
	return string(output)
}

// The real SDK request/parser runs against one local peer; no credentials or
// responses are borrowed from the developer's configured operator.
func minerStateDirectoryAuthServer(t *testing.T, token string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	requests := &atomic.Int32{}
	failures := make(chan error, 1)
	retainFailure := func(err error) {
		select {
		case failures <- err:
		default:
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet && request.URL.Path == "/hello" {
			return
		}
		requests.Add(1)
		var args sdk.AuthCodeLoginArgs
		if request.Method != http.MethodPost || request.URL.Path != "/auth/code-login" || json.NewDecoder(request.Body).Decode(&args) != nil || args.AuthCode != "synthetic-state-directory-code" {
			retainFailure(errors.New("auth command changed its synthetic local request"))
			http.Error(writer, "unexpected synthetic request", http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(writer).Encode(&sdk.AuthCodeLoginResult{Jwt: token}); err != nil {
			retainFailure(err)
		}
	}))
	t.Cleanup(func() {
		server.Close()
		select {
		case err := <-failures:
			t.Error(err)
		default:
		}
	})
	return server, requests
}

// Absolute/relative overrides and an absent home reach the actual auth writer
// and provider reader. An unset override preserves the established home path.
func TestMinerStateDirectoryAuthWritesSelectedState(t *testing.T) {
	const token = "synthetic-selected-network-token"
	server, requests := minerStateDirectoryAuthServer(t, token)
	for _, mode := range []string{"absolute", "relative", "no-home", "default"} {
		directory := t.TempDir()
		privateHome := filepath.Join(directory, "private-home")
		homeState := filepath.Join(privateHome, ".urnetwork")
		if err := os.MkdirAll(homeState, 0700); err != nil {
			t.Fatal(err)
		}
		const unselected = "synthetic-unselected-network-token"
		if err := os.WriteFile(filepath.Join(homeState, "jwt"), []byte(unselected), 0600); err != nil {
			t.Fatal(err)
		}
		selected := filepath.Join(directory, "selected-state")
		override := selected
		switch mode {
		case "relative":
			override = "selected-state"
		case "no-home":
			privateHome = ""
		case "default":
			override, selected = "", homeState
		}
		before := requests.Load()
		runMinerStateDirectoryCli(t, directory, privateHome, override, "", token, []string{"auth", "synthetic-state-directory-code", "--api_url=" + server.URL, "-f"})
		raw, err := os.ReadFile(filepath.Join(selected, "jwt"))
		if err != nil || string(raw) != token || requests.Load() <= before {
			t.Fatalf("%s auth did not write its selected state: %v", mode, err)
		}
		info, err := os.Stat(filepath.Join(selected, "jwt"))
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("%s auth lost private token permissions: %v", mode, err)
		}
		if mode != "default" {
			raw, err := os.ReadFile(filepath.Join(homeState, "jwt"))
			if err != nil || string(raw) != unselected {
				t.Fatalf("%s auth changed the unselected home credential: %v", mode, err)
			}
		}
	}
}

// An existing selected token triggers confirmation before any auth request,
// even when the default home's token does not exist.
func TestMinerStateDirectoryAuthDeclinesSelectedOverwrite(t *testing.T) {
	const original = "synthetic-existing-selected-token"
	server, requests := minerStateDirectoryAuthServer(t, "synthetic-replacement-token")
	directory := t.TempDir()
	privateHome, state := filepath.Join(directory, "private-home"), filepath.Join(directory, "selected-state")
	if err := os.MkdirAll(state, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(state, "jwt")
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	output := runMinerStateDirectoryCli(t, directory, privateHome, state, "n\n", original, []string{"auth", "synthetic-state-directory-code", "--api_url=" + server.URL})
	if requests.Load() != 0 || !strings.Contains(output, path+" exists. Overwrite?") {
		t.Fatal("auth ignored the selected token's overwrite refusal", output)
	}
	if _, err := os.Stat(filepath.Join(privateHome, ".urnetwork", "jwt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("declined auth touched the default home credential", err)
	}
}

// Actual proxy add/remove commands read and update the selected configuration;
// another home configuration cannot silently alter the provider roster.
func TestMinerStateDirectoryProxyCommandsStaySelected(t *testing.T) {
	for _, mode := range []string{"absolute", "relative", "default"} {
		directory := t.TempDir()
		privateHome := filepath.Join(directory, "private-home")
		homeState, selected := filepath.Join(privateHome, ".urnetwork"), filepath.Join(directory, "selected-state")
		override := selected
		if mode == "relative" {
			override = "selected-state"
		} else if mode == "default" {
			override, selected = "", homeState
		}
		for _, path := range []string{homeState, selected} {
			if err := os.MkdirAll(path, 0700); err != nil {
				t.Fatal(err)
			}
		}
		const original = `{"servers":{"192.0.2.41:1080":"synthetic-home-key"}}`
		if mode != "default" {
			if err := os.WriteFile(filepath.Join(homeState, "proxy"), []byte(original), 0600); err != nil {
				t.Fatal(err)
			}
		}
		runMinerStateDirectoryCli(t, directory, privateHome, override, "", "", []string{"proxy", "add", "192.0.2.40:1080"})
		raw, err := os.ReadFile(filepath.Join(selected, "proxy"))
		var config ProxyConfig
		if err != nil || json.Unmarshal(raw, &config) != nil || len(config.Servers) != 1 {
			t.Fatalf("%s proxy add borrowed another directory: %v", mode, err)
		}
		if key, ok := config.Servers["192.0.2.40:1080"]; !ok || key != "" {
			t.Fatalf("%s proxy add changed the selected server", mode)
		}
		runMinerStateDirectoryCli(t, directory, privateHome, override, "", "", []string{"proxy", "remove", "--all"})
		raw, err = os.ReadFile(filepath.Join(selected, "proxy"))
		config = ProxyConfig{}
		if err != nil || json.Unmarshal(raw, &config) != nil || len(config.Servers) != 0 {
			t.Fatalf("%s proxy removal missed the selected directory: %v", mode, err)
		}
		if mode != "default" {
			raw, err = os.ReadFile(filepath.Join(homeState, "proxy"))
			if err != nil || string(raw) != original {
				t.Fatalf("%s proxy command changed the unselected home configuration: %v", mode, err)
			}
		}
	}
}
