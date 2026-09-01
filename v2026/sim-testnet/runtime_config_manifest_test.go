package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Verify the exact rendered live inventory without mutating it.
func TestLiveRuntimeConfigManifest(t *testing.T) {
	if os.Getenv("SIM_TESTNET_LIVE_RUNTIME_CONFIG") != "1" {
		t.Skip("set SIM_TESTNET_LIVE_RUNTIME_CONFIG=1 to verify the rendered live runtime inventory")
	}
	stateDir := os.Getenv("SIM_TESTNET_STATE_DIR")
	if stateDir == "" {
		t.Fatal("SIM_TESTNET_STATE_DIR is required")
	}
	cfg, err := LoadResolved(LoadOptions{ConfigPath: "testnet.yml", RequireSecrets: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := validateOperatorConfigOverlays(cfg, stateDir); err != nil {
		t.Fatal(err)
	}
	verified, err := verifyRuntimeConfigManifest(cfg, stateDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("files=%d manifest_hash=%s", verified.FileCount, verified.ManifestHash)
}

func runtimeConfigManifestFixture(t *testing.T) (*ResolvedConfig, string) {
	t.Helper()
	cfg := testResolvedConfig(t)
	cfg.Config.Topology.Operators = 1
	cfg.Config.Topology.Validators = 1
	cfg.Config.Topology.Miners = 2
	cfg.Config.Topology.MinerSwarmProcesses = 1
	vault := t.TempDir()
	if err := os.MkdirAll(filepath.Join(vault, "local"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vault, "local", "auth.yml"), []byte("auth: test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg.Repos.Vault = vault
	stateDir := t.TempDir()
	expected, err := expectedRuntimeConfigFiles(cfg, stateDir)
	if err != nil {
		t.Fatal(err)
	}
	for relative, mode := range expected {
		path := filepath.Join(stateDir, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(relative+"\n"), mode); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeRuntimeConfigManifest(cfg, stateDir); err != nil {
		t.Fatal(err)
	}
	return cfg, stateDir
}

func rewriteRuntimeConfigManifest(t *testing.T, stateDir string, mutate func(*RuntimeConfigManifest)) {
	t.Helper()
	var manifest RuntimeConfigManifest
	if err := decodeStrictJSONFile(runtimeConfigManifestPath(stateDir), &manifest); err != nil {
		t.Fatal(err)
	}
	mutate(&manifest)
	hash, err := runtimeConfigManifestHash(manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifest.ManifestHash = hash
	wire, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(runtimeConfigManifestPath(stateDir), append(wire, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeConfigManifestAuthenticatesEveryStaticInput(t *testing.T) {
	cfg, stateDir := runtimeConfigManifestFixture(t)
	verified, err := verifyRuntimeConfigManifest(cfg, stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if verified.FileCount == 0 || verified.ManifestHash == "" {
		t.Fatalf("runtime config verification=%+v", verified)
	}

	path := filepath.Join(stateDir, "runtime", "miner-1", "miner.yml")
	if err := os.WriteFile(path, []byte("altered\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := verifyRuntimeConfigManifest(cfg, stateDir); err == nil || !strings.Contains(err.Error(), "differs from its manifest") {
		t.Fatalf("altered runtime config was accepted: %v", err)
	}
}

func TestRuntimeConfigManifestRejectsEveryIdentityDrift(t *testing.T) {
	mutations := []func(*RuntimeConfigManifest){
		func(manifest *RuntimeConfigManifest) { manifest.DeploymentID = "other-deployment" },
		func(manifest *RuntimeConfigManifest) { manifest.ConfigHash = "0x" + strings.Repeat("77", 32) },
		func(manifest *RuntimeConfigManifest) { manifest.PolicyHash = "0x" + strings.Repeat("88", 32) },
	}
	for index, mutate := range mutations {
		cfg, stateDir := runtimeConfigManifestFixture(t)
		rewriteRuntimeConfigManifest(t, stateDir, mutate)
		if _, err := verifyRuntimeConfigManifest(cfg, stateDir); err == nil || !strings.Contains(err.Error(), "identity") {
			t.Errorf("stale runtime identity %d was accepted: %v", index, err)
		}
	}
}

func TestRuntimeConfigManifestRejectsMissingEntry(t *testing.T) {
	cfg, stateDir := runtimeConfigManifestFixture(t)
	rewriteRuntimeConfigManifest(t, stateDir, func(manifest *RuntimeConfigManifest) {
		manifest.Files = manifest.Files[:len(manifest.Files)-1]
	})
	if _, err := verifyRuntimeConfigManifest(cfg, stateDir); err == nil || !strings.Contains(err.Error(), "files, want") {
		t.Fatalf("incomplete runtime config inventory was accepted: %v", err)
	}
}

func TestRuntimeConfigManifestRejectsUnexpectedStaticFile(t *testing.T) {
	cfg, stateDir := runtimeConfigManifestFixture(t)
	path := filepath.Join(stateDir, "runtime", "operator-1", "vault", "stale.yml")
	if err := os.WriteFile(path, []byte("stale: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := verifyRuntimeConfigManifest(cfg, stateDir); err == nil || !strings.Contains(err.Error(), "unexpected static runtime config") {
		t.Fatalf("unexpected static config was accepted: %v", err)
	}
}

// Reproduces the live config.render failure: the renderer intentionally adds
// two exact platform-config directory overlays, which are not extra rendered
// files. Only those paths and exact targets may cross the static-tree audit.
func TestRuntimeConfigManifestAcceptsOnlyExactOperatorConfigOverlays(t *testing.T) {
	fixture := func(t *testing.T) (*ResolvedConfig, string) {
		t.Helper()
		cfg, stateDir := runtimeConfigManifestFixture(t)
		platformConfig := t.TempDir()
		for _, name := range []string{"local", "all"} {
			if err := os.MkdirAll(filepath.Join(platformConfig, name), 0o700); err != nil {
				t.Fatal(err)
			}
		}
		cfg.Repos.PlatformConfig = platformConfig
		home := operatorConfigHome(stateDir, 1)
		if err := os.Symlink(filepath.Join(platformConfig, "local"), filepath.Join(home, operatorEnvironment(1))); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(platformConfig, "all"), filepath.Join(home, "all")); err != nil {
			t.Fatal(err)
		}
		return cfg, stateDir
	}
	cfg, stateDir := fixture(t)
	if _, err := verifyRuntimeConfigManifest(cfg, stateDir); err != nil {
		t.Fatalf("exact required overlays were rejected: %v", err)
	}

	tests := []struct {
		name string
		want string
		run  func(*testing.T, *ResolvedConfig, string)
	}{
		{"wrong target", "targets", func(t *testing.T, cfg *ResolvedConfig, stateDir string) {
			link := filepath.Join(operatorConfigHome(stateDir, 1), "all")
			if err := os.Remove(link); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(t.TempDir(), link); err != nil {
				t.Fatal(err)
			}
		}},
		{"regular substitution", "readlink", func(t *testing.T, cfg *ResolvedConfig, stateDir string) {
			link := filepath.Join(operatorConfigHome(stateDir, 1), operatorEnvironment(1))
			if err := os.Remove(link); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(link, []byte("not a link\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"unapproved link", "unexpected static runtime config", func(t *testing.T, cfg *ResolvedConfig, stateDir string) {
			if err := os.Symlink(filepath.Join(cfg.Repos.PlatformConfig, "local"), filepath.Join(operatorConfigHome(stateDir, 1), "foreign")); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, test := range tests {
		cfg, stateDir := fixture(t)
		test.run(t, cfg, stateDir)
		if _, err := verifyRuntimeConfigManifest(cfg, stateDir); err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(test.want)) {
			t.Errorf("%s overlay mutation error=%v, want %q", test.name, err, test.want)
		}
	}
}

func TestRuntimeConfigManifestRejectsSymlinkSubstitution(t *testing.T) {
	cfg, stateDir := runtimeConfigManifestFixture(t)
	path := filepath.Join(stateDir, "runtime", "miner-1", "miner.yml")
	target := filepath.Join(t.TempDir(), "replacement")
	if err := os.WriteFile(target, []byte("replacement\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if _, err := verifyRuntimeConfigManifest(cfg, stateDir); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("runtime config symlink was accepted: %v", err)
	}
}

func TestRuntimeConfigManifestRejectsParentDirectorySymlinkSubstitution(t *testing.T) {
	cfg, stateDir := runtimeConfigManifestFixture(t)
	root := filepath.Join(stateDir, "runtime", "miner-1")
	target := filepath.Join(t.TempDir(), "miner-1")
	if err := os.Rename(root, target); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, root); err != nil {
		t.Fatal(err)
	}
	if _, err := verifyRuntimeConfigManifest(cfg, stateDir); err == nil || !strings.Contains(err.Error(), "not a real directory") {
		t.Fatalf("runtime config parent symlink was accepted: %v", err)
	}
}

func TestRuntimeConfigManifestRejectsModeDrift(t *testing.T) {
	cfg, stateDir := runtimeConfigManifestFixture(t)
	path := filepath.Join(stateDir, "runtime", "miner-1", "miner.yml")
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := verifyRuntimeConfigManifest(cfg, stateDir); err == nil || !strings.Contains(err.Error(), "differs from its manifest") {
		t.Fatalf("public runtime config mode was accepted: %v", err)
	}
}

func TestRuntimeConfigManifestRejectsSelfHashAndOrderingDrift(t *testing.T) {
	cfg, stateDir := runtimeConfigManifestFixture(t)
	var manifest RuntimeConfigManifest
	if err := decodeStrictJSONFile(runtimeConfigManifestPath(stateDir), &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.ManifestHash = "0x" + strings.Repeat("99", 32)
	wire, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(runtimeConfigManifestPath(stateDir), append(wire, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := verifyRuntimeConfigManifest(cfg, stateDir); err == nil || !strings.Contains(err.Error(), "manifest hash") {
		t.Fatalf("invalid runtime manifest self-hash was accepted: %v", err)
	}

	if err := writeRuntimeConfigManifest(cfg, stateDir); err != nil {
		t.Fatal(err)
	}
	rewriteRuntimeConfigManifest(t, stateDir, func(manifest *RuntimeConfigManifest) {
		manifest.Files[0], manifest.Files[1] = manifest.Files[1], manifest.Files[0]
	})
	if _, err := verifyRuntimeConfigManifest(cfg, stateDir); err == nil || !strings.Contains(err.Error(), "not unique and sorted") {
		t.Fatalf("unordered runtime manifest was accepted: %v", err)
	}
}
