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

func testGitOutput(t *testing.T, root string, args ...string) []byte {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
	return output
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

func TestSDKMobileBuildTreeHashBindsNestedModule(t *testing.T) {
	root := t.TempDir()
	runTestGit(t, root, "init", "-q")
	runTestGit(t, root, "config", "user.email", "sim-testnet@example.invalid")
	runTestGit(t, root, "config", "user.name", "sim-testnet")
	commandDir := filepath.Join(root, "build", "cmd", "mobileexports")
	if err := os.MkdirAll(commandDir, 0o700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(commandDir, "main.go")
	if err := os.WriteFile(source, []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, root, "add", "build/cmd/mobileexports/main.go")
	runTestGit(t, root, "commit", "-qm", "review mobile build module")
	first, err := sdkMobileBuildTreeHash(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("package main\n\nfunc main() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := sdkMobileBuildTreeHash(root); err == nil || !strings.Contains(err.Error(), "differs from reviewed HEAD") {
		t.Fatalf("dirty mobile build source was accepted: %v", err)
	}
	runTestGit(t, root, "add", "build/cmd/mobileexports/main.go")
	runTestGit(t, root, "commit", "-qm", "update mobile build module")
	second, err := sdkMobileBuildTreeHash(root)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("reviewed mobile build source update did not change release-lock hash")
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

func TestSoliditySourceHashCoversDeploymentScripts(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"evm/src", "evm/script"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "evm", "src", "Contract.sol"), []byte("contract C {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	deploy := filepath.Join(root, "evm", "script", "Deploy.s.sol")
	if err := os.WriteFile(deploy, []byte("contract Deploy {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := soliditySourceHash(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(deploy, []byte("contract Deploy { function run() external {} }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := soliditySourceHash(root)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("deployment-script drift did not change the Solidity release source hash")
	}
}

func TestCleanGitRevisionRejectsDependencyDrift(t *testing.T) {
	root := t.TempDir()
	runTestGit(t, root, "init", "-q")
	runTestGit(t, root, "config", "user.email", "sim-testnet@example.invalid")
	runTestGit(t, root, "config", "user.name", "sim-testnet")
	dependency := filepath.Join(root, "Library.sol")
	if err := os.WriteFile(dependency, []byte("library Library {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, root, "add", "Library.sol")
	runTestGit(t, root, "commit", "-qm", "pin dependency")
	want := strings.TrimSpace(string(testGitOutput(t, root, "rev-parse", "HEAD")))
	got, err := cleanGitRevision(root)
	if err != nil || got != want {
		t.Fatalf("clean dependency revision = %q, want %q: %v", got, want, err)
	}
	if err := os.WriteFile(dependency, []byte("library Library { function changed() external {} }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := cleanGitRevision(root); err == nil {
		t.Fatal("modified dependency checkout was accepted")
	}
	if err := os.WriteFile(dependency, []byte("library Library {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Untracked.sol"), []byte("contract Untracked {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := cleanGitRevision(root); err == nil {
		t.Fatal("untracked dependency source was accepted")
	}
}

func TestOperatorProxyReleaseObservationBindsCleanCommitAndSource(t *testing.T) {
	root := t.TempDir()
	runTestGit(t, root, "init", "-q")
	runTestGit(t, root, "config", "user.email", "sim-testnet@example.invalid")
	runTestGit(t, root, "config", "user.name", "sim-testnet")
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module github.com/urnetwork/operator-proxy\n\ngo 1.26.3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "proxy.go")
	if err := os.WriteFile(source, []byte("package operatorproxy\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, root, "add", "go.mod", "proxy.go")
	runTestGit(t, root, "commit", "-qm", "initial operator proxy")
	first, err := observeOperatorProxyReleaseSource(root)
	if err != nil {
		t.Fatal(err)
	}
	wantCommit := strings.TrimSpace(string(testGitOutput(t, root, "rev-parse", "HEAD")))
	wantHash, err := goReleaseSourceHash(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 || first["operator_proxy_commit"] != wantCommit || first["operator_proxy_go_source_hash"] != wantHash {
		t.Fatalf("operator-proxy observation = %+v, want commit=%s source=%s", first, wantCommit, wantHash)
	}
	if err := os.WriteFile(source, []byte("package operatorproxy\n\nconst Version = 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := observeOperatorProxyReleaseSource(root); err == nil {
		t.Fatal("dirty operator-proxy source was accepted")
	}
	runTestGit(t, root, "add", "proxy.go")
	runTestGit(t, root, "commit", "-qm", "update operator proxy")
	second, err := observeOperatorProxyReleaseSource(root)
	if err != nil {
		t.Fatal(err)
	}
	if second["operator_proxy_commit"] == first["operator_proxy_commit"] || second["operator_proxy_go_source_hash"] == first["operator_proxy_go_source_hash"] {
		t.Fatalf("committed operator-proxy drift was not bound: first=%+v second=%+v", first, second)
	}
}

func TestReleaseRepositorySchemaRequiresCanonicalOperatorProxyFields(t *testing.T) {
	repositories := map[string]any{
		"operator_proxy_go_source_hash": "sha256:" + strings.Repeat("1", 64),
		"operator_proxy_commit":         strings.Repeat("a", 40),
	}
	if err := validateReleaseRepositorySchema(repositories); err != nil {
		t.Fatal(err)
	}
	delete(repositories, "operator_proxy_commit")
	if err := validateReleaseRepositorySchema(repositories); err == nil {
		t.Fatal("release repository schema without an operator-proxy commit was accepted")
	}
	repositories["operator_proxy_commit"] = strings.Repeat("A", 40)
	if err := validateReleaseRepositorySchema(repositories); err == nil {
		t.Fatal("noncanonical operator-proxy commit was accepted")
	}
	repositories["operator_proxy_commit"] = strings.Repeat("a", 40)
	repositories["operator_proxy_go_source_hash"] = "sha256:not-a-hash"
	if err := validateReleaseRepositorySchema(repositories); err == nil {
		t.Fatal("noncanonical operator-proxy source hash was accepted")
	}
}

func TestParseFoundryVersion(t *testing.T) {
	version, commit, err := parseFoundryVersion([]byte("forge Version: 1.7.1\nCommit SHA: 4072E48705AF9D93E3C0F6E29E93B5E9A40CAED8\nBuild Profile: dist\n"))
	if err != nil {
		t.Fatal(err)
	}
	if version != "1.7.1" || commit != "4072e48705af9d93e3c0f6e29e93b5e9a40caed8" {
		t.Fatalf("foundry identity = %q %q", version, commit)
	}
}

func TestParseFoundryVersionRejectsIncompleteOutput(t *testing.T) {
	if _, _, err := parseFoundryVersion([]byte("forge Version: 1.7.1\n")); err == nil {
		t.Fatal("forge output without a commit was accepted")
	}
	if _, _, err := parseFoundryVersion([]byte("forge Version: 1.7.1\nCommit SHA: not-a-commit\n")); err == nil {
		t.Fatal("forge output with a malformed commit was accepted")
	}
	if _, _, err := parseFoundryVersion([]byte("forge Version: 1.7.1\nforge Version: 1.7.1\nCommit SHA: 4072e48705af9d93e3c0f6e29e93b5e9a40caed8\n")); err == nil {
		t.Fatal("forge output with duplicate identity fields was accepted")
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

// The source tag alone cannot prove which runtime is deployed. Keep every
// independent source, artifact, upstream-proposal, and live-code binding exact.
func TestReviewedRuntimeIdentityRejectsEachAdjacentFieldDrift(t *testing.T) {
	valid := func() *ReleaseLock {
		lock := new(ReleaseLock)
		lock.Runtime.SourceRepository = reviewedRuntimeSourceRepository
		lock.Runtime.SourceTag = reviewedRuntimeSourceTag
		lock.Runtime.SourceCommit = reviewedRuntimeSourceCommit
		lock.Runtime.CodeHash = reviewedRuntimeCodeHash
		lock.Runtime.MetadataHash = reviewedRuntimeMetadataHash
		lock.Runtime.CompressedWasmSHA256 = reviewedRuntimeCompressedWasmSHA256
		lock.Runtime.UpstreamReleaseCallHash = reviewedRuntimeUpstreamReleaseCallHash
		lock.Runtime.UpstreamReleaseTimepoint = reviewedRuntimeUpstreamReleaseTimepoint
		lock.Runtime.SpecVersion = reviewedRuntimeSpecVersion
		lock.Runtime.TransactionVersion = reviewedRuntimeTransactionVersion
		lock.Runtime.StateVersion = reviewedRuntimeStateVersion
		return lock
	}
	if err := validateReviewedRuntimeIdentity(valid()); err != nil {
		t.Fatalf("reviewed runtime identity was rejected: %v", err)
	}
	if err := validateReviewedRuntimeIdentity(nil); err == nil {
		t.Fatal("missing runtime identity was accepted")
	}
	cases := []struct {
		name   string
		mutate func(*ReleaseLock)
	}{
		{name: "source repository", mutate: func(lock *ReleaseLock) { lock.Runtime.SourceRepository += "/fork" }},
		{name: "source tag", mutate: func(lock *ReleaseLock) { lock.Runtime.SourceTag = "v452" }},
		{name: "source commit", mutate: func(lock *ReleaseLock) { lock.Runtime.SourceCommit = strings.Repeat("0", 40) }},
		{name: "wasm hash", mutate: func(lock *ReleaseLock) { lock.Runtime.CodeHash = "0x" + strings.Repeat("0", 64) }},
		{name: "metadata hash", mutate: func(lock *ReleaseLock) { lock.Runtime.MetadataHash = "0x" + strings.Repeat("0", 64) }},
		{name: "compressed wasm SHA-256", mutate: func(lock *ReleaseLock) { lock.Runtime.CompressedWasmSHA256 = "0x" + strings.Repeat("0", 64) }},
		{name: "upstream release call hash", mutate: func(lock *ReleaseLock) { lock.Runtime.UpstreamReleaseCallHash = "0x" + strings.Repeat("0", 64) }},
		{name: "upstream release timepoint", mutate: func(lock *ReleaseLock) { lock.Runtime.UpstreamReleaseTimepoint = "8987926:12" }},
		{name: "spec version", mutate: func(lock *ReleaseLock) { lock.Runtime.SpecVersion-- }},
		{name: "transaction version", mutate: func(lock *ReleaseLock) { lock.Runtime.TransactionVersion++ }},
		{name: "state version", mutate: func(lock *ReleaseLock) { lock.Runtime.StateVersion++ }},
	}
	for _, testCase := range cases {
		lock := valid()
		testCase.mutate(lock)
		if err := validateReviewedRuntimeIdentity(lock); err == nil {
			t.Errorf("%s drift was accepted", testCase.name)
		}
	}
}

func TestReleaseLockRejectsGeneratedRuntimeDrift(t *testing.T) {
	cfg := testResolvedConfig(t)
	cfg.Release = &ReleaseLock{SchemaVersion: 1, Release: "1.0"}
	cfg.Release.Runtime.SourceRepository = reviewedRuntimeSourceRepository
	cfg.Release.Runtime.SourceTag = reviewedRuntimeSourceTag
	cfg.Release.Runtime.SourceCommit = reviewedRuntimeSourceCommit
	cfg.Release.Runtime.CodeHash = reviewedRuntimeCodeHash
	cfg.Release.Runtime.MetadataHash = reviewedRuntimeMetadataHash
	cfg.Release.Runtime.CompressedWasmSHA256 = reviewedRuntimeCompressedWasmSHA256
	cfg.Release.Runtime.UpstreamReleaseCallHash = reviewedRuntimeUpstreamReleaseCallHash
	cfg.Release.Runtime.UpstreamReleaseTimepoint = reviewedRuntimeUpstreamReleaseTimepoint
	cfg.Release.Runtime.SpecVersion = reviewedRuntimeSpecVersion
	cfg.Release.Runtime.TransactionVersion = reviewedRuntimeTransactionVersion
	cfg.Release.Runtime.StateVersion = reviewedRuntimeStateVersion
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
