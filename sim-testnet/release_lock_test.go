package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runTestGit(t *testing.T, root string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}

func TestCleanGitSubtreeHashRejectsRuntimeAssetDrift(t *testing.T) {
	root := t.TempDir()
	runTestGit(t, root, "init", "-q")
	runTestGit(t, root, "config", "user.email", "sim-testnet@example.invalid")
	runTestGit(t, root, "config", "user.name", "sim-testnet")
	if err := os.Mkdir(filepath.Join(root, "all"), 0o700); err != nil {
		t.Fatal(err)
	}
	asset := filepath.Join(root, "all", "asset.mmdb")
	if err := os.WriteFile(asset, []byte("reviewed"), 0o600); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, root, "add", "all/asset.mmdb")
	runTestGit(t, root, "commit", "-qm", "review shared assets")
	first, err := cleanGitSubtreeHash(root, "all")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(asset, []byte("drifted"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := cleanGitSubtreeHash(root, "all"); err == nil || !strings.Contains(err.Error(), "differs from reviewed HEAD") {
		t.Fatalf("modified shared asset was accepted: %v", err)
	}
	if err := os.WriteFile(asset, []byte("reviewed"), 0o600); err != nil {
		t.Fatal(err)
	}
	untracked := filepath.Join(root, "all", "unreviewed.yml")
	if err := os.WriteFile(untracked, []byte("unreviewed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := cleanGitSubtreeHash(root, "all"); err == nil {
		t.Fatal("untracked shared asset was accepted")
	}
	if err := os.Remove(untracked); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(asset, []byte("reviewed-v2"), 0o600); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, root, "add", "all/asset.mmdb")
	runTestGit(t, root, "commit", "-qm", "update shared assets")
	second, err := cleanGitSubtreeHash(root, "all")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("reviewed shared asset update did not change release-lock hash")
	}
}

func TestDigestNamedFilesIsOrderedAndContentSensitive(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a"), []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b"), []byte("two"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := digestNamedFiles(dir, []string{"b", "a"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := digestNamedFiles(dir, []string{"a", "b"})
	if err != nil || first != second {
		t.Fatalf("ordered digest mismatch: %s %s %v", first, second, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b"), []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, _ := digestNamedFiles(dir, []string{"a", "b"})
	if changed == first {
		t.Fatal("content change did not change digest")
	}
}

// TestGeneratedABIHashBindsFleetBatcher proves the accelerated setup helper is
// an authenticated release input rather than an unreviewed testnet sidecar.
func TestGeneratedABIHashBindsFleetBatcher(t *testing.T) {
	baseline := generatedABIHash()
	changed := append([]releaseABI(nil), generatedReleaseABIs...)
	found := false
	for index := range changed {
		if changed[index].name != "FleetBatcher" {
			continue
		}
		found = true
		changed[index].abi += " "
	}
	if !found {
		t.Fatal("FleetBatcher ABI is absent from the release lock")
	}
	if digestReleaseABIs(changed) == baseline {
		t.Fatal("FleetBatcher ABI drift did not alter the release lock")
	}
}

func TestServerLocalDependencyHashCoversEveryPostgresInitHook(t *testing.T) {
	serverRoot := t.TempDir()
	initDir := filepath.Join(serverRoot, "local", "postgres", "initdb")
	if err := os.MkdirAll(initDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(serverRoot, "local", "docker-compose.yml"), []byte("services: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(initDir, "01-init.sh"), []byte("first\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := serverLocalDependencyConfigHash(serverRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(initDir, "02-unreviewed.sh"), []byte("second\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := serverLocalDependencyConfigHash(serverRoot)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("new PostgreSQL init hook did not change the release-lock digest")
	}
}

func TestSubtensorReleaseLockCoversLightnodeRuntimeSurface(t *testing.T) {
	gateway := strings.Join(subtensorGatewayReleaseFiles, "\n")
	for _, required := range []string{"playbook-subtensor.yml", "subtensor-gateway.yml", "nginx.conf.j2"} {
		if !strings.Contains(gateway, required) {
			t.Fatalf("gateway release files do not cover %s", required)
		}
	}
	node := strings.Join(subtensorNodeReleaseFiles, "\n")
	for _, required := range []string{
		"vars.yml",
		"docker-compose.yml.j2",
		"subtensor.service",
		"playbook-subtensor-lightnode.yml",
		"run-playbook.sh",
		"run-subtensor-lightnode.sh",
		"subtensor-lightnode-preflight.yml",
	} {
		if !strings.Contains(node, required) {
			t.Fatalf("node release files do not cover %s", required)
		}
	}
}

func TestFoundryStorageLayoutHashIsCanonicalAndComplete(t *testing.T) {
	dir := t.TempDir()
	first := `{"storageLayout":{"types":{"t_struct(Config)12_storage":{"label":"struct C.Config","members":[{"astId":13,"contract":"C","label":"value","slot":"0","offset":0,"type":"t_uint"}]},"t_uint":{"label":"uint256"}},"storage":[{"astId":12,"contract":"C","slot":"0","offset":0,"type":"t_struct(Config)12_storage","label":"owner"}]}}`
	second := `{"storageLayout": {"storage": [{"astId":999,"contract":"src/C.sol:C","label":"owner","type":"t_struct(Config)999_storage","offset":0,"slot":"0"}], "types": {"t_uint": {"label":"uint256"},"t_struct(Config)999_storage":{"label":"struct C.Config","members":[{"astId":1000,"contract":"src/C.sol:C","offset":0,"slot":"0","type":"t_uint","label":"value"}]}}}}`
	path := filepath.Join(dir, "artifact.json")
	if err := os.WriteFile(path, []byte(first), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := foundryStorageLayoutHash(dir, "artifact.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(second), 0o600); err != nil {
		t.Fatal(err)
	}
	b, err := foundryStorageLayoutHash(dir, "artifact.json")
	if err != nil || a != b {
		t.Fatalf("equivalent layouts hash differently: %s %s %v", a, b, err)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(second, `"slot":"0"`, `"slot":"1"`, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := foundryStorageLayoutHash(dir, "artifact.json")
	if err != nil || c == a {
		t.Fatalf("slot drift was not detected: %s %s %v", a, c, err)
	}
}

func TestReleaseLockMatchesCheckout(t *testing.T) {
	cfg, err := LoadResolved(LoadOptions{ConfigPath: "testnet.yml", RequireSecrets: false})
	if err != nil {
		t.Fatal(err)
	}
	if err := validateReleaseLock(cfg); err != nil {
		observed, observationErr := observeReleaseLock(cfg)
		t.Fatalf("%v\nobserved=%+v\nobservation_error=%v", err, observed, observationErr)
	}
}

func TestReleaseLockRejectsGeneratedRuntimeDrift(t *testing.T) {
	cfg := testResolvedConfig(t)
	cfg.Release = &ReleaseLock{SchemaVersion: 1, Release: "1.0"}
	cfg.Release.Runtime.SourceTag = "testnet"
	cfg.Release.Runtime.SourceCommit = "da06f033663896ef2fdbbfc3ecc68ca908fba0f5"
	cfg.Release.Runtime.CodeHash = "0x40a8c3c99a47d6739b086236308535fab26d5fd4cc5c88eb83f6a3c8b928f7cc"
	cfg.Release.Runtime.SpecVersion = 452
	cfg.Release.Runtime.TransactionVersion = 1
	cfg.Release.Runtime.Image = "image@sha256:" + strings.Repeat("0", 64)
	cfg.Release.Dependencies = map[string]string{"postgres": "postgres@sha256:" + strings.Repeat("0", 64)}
	cfg.Release.EVMBuild = map[string]any{"solidity": "0.8.24"}
	cfg.Release.Repositories = map[string]any{"x": "y"}
	cfg.Release.Interfaces = map[string]any{"x": "y"}
	cfg.Release.Infrastructure = map[string]any{"x": "y"}
	if err := validateReleaseLock(cfg); err == nil {
		t.Fatal("incomplete/mismatched release lock passed")
	}
}

func TestCompareLockSectionRejectsUnobservedField(t *testing.T) {
	locked := map[string]any{"source_hash": "sha256:one", "stale_hash": "sha256:old"}
	observed := map[string]string{"source_hash": "sha256:one"}
	if err := compareLockSection("interfaces", locked, observed, nil); err == nil {
		t.Fatal("unobserved release-lock field passed validation")
	}
	if err := compareLockSection("interfaces", locked, observed, map[string]struct{}{"stale_hash": {}}); err != nil {
		t.Fatalf("explicit annotation was rejected: %v", err)
	}
}
