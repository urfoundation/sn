package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// The public lock supplies non-build annotations and the separate node-image
// pin. This owned fixture binds reviewed current runtime literals and the
// actual generated build without editing or approving the checked-in lock.
func testReleaseLockFixture(t *testing.T) *ReleaseLock {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "deploy", "testnet", "release.lock.yml"))
	if err != nil {
		t.Fatal(err)
	}
	lock, err := decodeReleaseLockBytes(raw)
	if err != nil {
		t.Fatal(err)
	}
	runtime := runtime467ReviewedTestLock().Runtime
	runtime.Image = lock.Runtime.Image
	lock.Runtime = runtime
	for _, item := range []struct {
		prefix   string
		artifact ContractArtifact
	}{
		{prefix: "coordinator_implementation", artifact: artifactByName("Coordinator")},
		{prefix: "coordinator_proxy", artifact: artifactByName("ERC1967Proxy")},
		{prefix: "fleet_batcher", artifact: TestnetFleetBatcherArtifact},
		{prefix: "governance_drill_implementation", artifact: TestnetGovernanceDrillArtifact},
		{prefix: "precompile_probe", artifact: TestnetPrecompileProbeArtifact},
		{prefix: "reserve_sink", artifact: artifactByName("ReserveSink")},
		{prefix: "settlement_vault", artifact: artifactByName("SettlementVault")},
		{prefix: "validator_evidence", artifact: artifactByName("ValidatorEvidence")},
	} {
		lock.EVMBuild[item.prefix+"_artifact_hash"] = item.artifact.FoundryArtifactHash
		lock.EVMBuild[item.prefix+"_runtime_hash"] = item.artifact.RuntimeBytecodeHash
	}
	lock.EVMBuild["coordinator_storage_layout_hash"] = CoordinatorStorageLayoutHash
	lock.EVMBuild["governance_drill_storage_layout_hash"] = CoordinatorAdversaryStorageLayoutHash
	lock.EVMBuild["fleet_batcher_storage_layout_hash"] = FleetBatcherStorageLayoutHash
	lock.EVMBuild["validator_evidence_storage_layout_hash"] = ValidatorEvidenceStorageLayoutHash
	lock.EVMBuild["abi_hash"] = generatedABIHash()
	// A lock rendered since URnetwork's quic-go joined the observation pins its
	// source; the checked-in lock was attested before then.
	lock.Repositories["quic_go_go_source_hash"] = "sha256:" + strings.Repeat("5", 64)
	if err := validateReleaseLockStatic(lock); err != nil {
		t.Fatalf("complete generated release fixture: %v", err)
	}
	return lock
}

// The fixture's current identity is complete and independent of any old
// checked-in runtime stanza. The real file and historical bytes stay intact.
func TestReleaseLockFixtureUsesReviewedCurrentRuntimeWithoutRewritingLock(t *testing.T) {
	path := filepath.Join("..", "deploy", "testnet", "release.lock.yml")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	original, err := decodeReleaseLockBytes(before)
	if err != nil {
		t.Fatal(err)
	}
	fixture := testReleaseLockFixture(t)
	expected := runtime467ReviewedTestLock().Runtime
	expected.Image = original.Runtime.Image
	if fixture.Runtime != expected || fixture.Runtime.SourceTag != "" || fixture.Runtime.UpstreamReleaseCallHash != "" || fixture.Runtime.UpstreamReleaseTimepoint != "" {
		t.Fatalf("owned fixture inherited stale or incomplete runtime provenance: %+v", fixture.Runtime)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("owned fixture rewrote the actual release lock")
	}
	second := testReleaseLockFixture(t)
	fixture.Runtime.SpecVersion = 454
	fixture.EVMBuild["coordinator_implementation_runtime_hash"] = "changed owned fixture"
	if second.Runtime != expected || second.EVMBuild["coordinator_implementation_runtime_hash"] != artifactByName("Coordinator").RuntimeBytecodeHash {
		t.Fatal("fixture instances share mutable runtime/build identity")
	}
}

// Refreshing an owned fixture never widens production static admission:
// historical, cross-artifact, invented-provenance and compiler drift still fail.
func TestReleaseLockFixtureRetainsRuntimeAndBuildDriftRejection(t *testing.T) {
	for _, mutate := range []func(*ReleaseLock){
		func(lock *ReleaseLock) { lock.Runtime.SpecVersion = 454 },
		func(lock *ReleaseLock) {
			lock.Runtime.CodeHash = "0x725e3d1eca8d5c29c1f0fa6476d5360661b852f52aebad979d6636e227a431ef"
		},
		func(lock *ReleaseLock) {
			lock.Runtime.MetadataHash = "0x4d17516b694ef8d18f8a565dcb2df0117e7a0018a3ffa40812c91a1621225702"
		},
		func(lock *ReleaseLock) { lock.Runtime.SourceRefKind, lock.Runtime.SourceRefName = "", "" },
		func(lock *ReleaseLock) { lock.Runtime.SourceTag = "v455" },
		func(lock *ReleaseLock) { lock.Runtime.UpstreamReleaseTimepoint = "8996567:7" },
		func(lock *ReleaseLock) { lock.Runtime.Image = "unpinned-image:latest" },
		func(lock *ReleaseLock) { lock.EVMBuild["solidity"] = "0.8.25" },
	} {
		lock := testReleaseLockFixture(t)
		mutate(lock)
		if err := validateReleaseLockStatic(lock); err == nil {
			t.Fatalf("changed owned fixture bypassed static lock admission: %+v", lock.Runtime)
		}
	}
}

func testReleaseLockObservation(t *testing.T, lock *ReleaseLock) *releaseLockObservation {
	t.Helper()
	evm, err := releaseLockObservedSection(lock.EVMBuild, releaseEVMObservedKeys)
	if err != nil {
		t.Fatal(err)
	}
	repositories, err := releaseLockObservedSection(lock.Repositories, releaseRepositoryObservedKeys)
	if err != nil {
		t.Fatal(err)
	}
	interfaces, err := releaseLockObservedSection(lock.Interfaces, releaseInterfaceObservedKeys)
	if err != nil {
		t.Fatal(err)
	}
	infrastructure, err := releaseLockObservedSection(lock.Infrastructure, releaseInfrastructureObservedKeys)
	if err != nil {
		t.Fatal(err)
	}
	return &releaseLockObservation{EVMBuild: evm, Repositories: repositories, Interfaces: interfaces, Infrastructure: infrastructure}
}

func reversedReleaseMap(source map[string]any) map[string]any {
	keys := make([]string, 0, len(source))
	for key := range source {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for left, right := 0, len(keys)-1; left < right; left, right = left+1, right-1 {
		keys[left], keys[right] = keys[right], keys[left]
	}
	reversed := make(map[string]any, len(source))
	for _, key := range keys {
		reversed[key] = source[key]
	}
	return reversed
}

func TestReleaseLockObservationRejectsMissingAndAdditionalFields(t *testing.T) {
	lock := testReleaseLockFixture(t)
	missing := testReleaseLockObservation(t, lock)
	delete(missing.Repositories, "sdk_go_source_hash")
	if err := validateReleaseLockObservation(missing); err == nil || !strings.Contains(err.Error(), "sdk_go_source_hash is missing") {
		t.Fatalf("incomplete release observation was accepted: %v", err)
	}
	additional := testReleaseLockObservation(t, lock)
	additional.Interfaces["unreviewed_hash"] = "sha256:" + strings.Repeat("0", 64)
	if err := validateReleaseLockObservation(additional); err == nil || !strings.Contains(err.Error(), "not in the observation schema") {
		t.Fatalf("additional release observation field was accepted: %v", err)
	}
	malformed := testReleaseLockObservation(t, lock)
	malformed.Infrastructure["gateway_config_hash"] = "sha256:not-a-hash"
	if err := validateReleaseLockObservation(malformed); err == nil || !strings.Contains(err.Error(), "canonical SHA-256") {
		t.Fatalf("malformed release observation was accepted: %v", err)
	}
}

func TestCanonicalReleaseLockRenderingIsStableAndRoundTrips(t *testing.T) {
	lock := testReleaseLockFixture(t)
	first, err := canonicalReleaseLockBytes(lock)
	if err != nil {
		t.Fatal(err)
	}
	reordered := *lock
	reordered.EVMBuild = reversedReleaseMap(lock.EVMBuild)
	reordered.Repositories = reversedReleaseMap(lock.Repositories)
	reordered.Interfaces = reversedReleaseMap(lock.Interfaces)
	reordered.Infrastructure = reversedReleaseMap(lock.Infrastructure)
	second, err := canonicalReleaseLockBytes(&reordered)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("release-lock rendering depends on map insertion order")
	}
	decoded, err := decodeReleaseLockBytes(first)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, lock) {
		t.Fatal("canonical release-lock rendering did not preserve semantic values")
	}
	if !bytes.HasSuffix(first, []byte("\n")) {
		t.Fatal("canonical release-lock rendering omitted its final newline")
	}
}

func TestReleaseLockRenderingReplacesStaleObservedValuesWithoutSelfReference(t *testing.T) {
	first := testReleaseLockFixture(t)
	second := testReleaseLockFixture(t)
	second.Repositories["sdk_go_source_hash"] = "sha256:" + strings.Repeat("9", 64)
	observation := testReleaseLockObservation(t, first)
	firstCandidate, err := releaseLockWithObservation(first, observation)
	if err != nil {
		t.Fatal(err)
	}
	secondCandidate, err := releaseLockWithObservation(second, observation)
	if err != nil {
		t.Fatal(err)
	}
	firstWire, err := canonicalReleaseLockBytes(firstCandidate)
	if err != nil {
		t.Fatal(err)
	}
	secondWire, err := canonicalReleaseLockBytes(secondCandidate)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstWire, secondWire) {
		t.Fatal("stale observed lock bytes influenced the rendered observation")
	}
}

// A fresh rendering from an attested lock that still carries the retired
// operator-proxy pins keeps every other repository field and drops the pins.
func TestReleaseLockRenderingDropsRetiredOperatorProxyPins(t *testing.T) {
	attested := testReleaseLockFixture(t)
	attested.Repositories["operator_proxy_go_source_hash"] = "sha256:" + strings.Repeat("1", 64)
	attested.Repositories["operator_proxy_commit"] = strings.Repeat("a", 40)
	if err := validateReleaseLockStatic(attested); err != nil {
		t.Fatalf("attested lock with retired pins was refused: %v", err)
	}
	candidate, err := releaseLockWithObservation(attested, testReleaseLockObservation(t, attested))
	if err != nil {
		t.Fatal(err)
	}
	wire, err := canonicalReleaseLockBytes(candidate)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(wire, []byte("operator_proxy")) {
		t.Fatalf("fresh release lock carried retired operator-proxy pins:\n%s", wire)
	}
	for key, value := range attested.Repositories {
		if strings.HasPrefix(key, "operator_proxy") {
			continue
		}
		if candidate.Repositories[key] != value {
			t.Errorf("fresh release lock changed repositories.%s from %v to %v", key, value, candidate.Repositories[key])
		}
	}
}

// Every fresh observation pins URnetwork's quic-go source beside the other
// sibling modules; an observation without it is incomplete.
func TestReleaseLockObservationRequiresUrnetworkQuicGo(t *testing.T) {
	observation := testReleaseLockObservation(t, testReleaseLockFixture(t))
	if err := validateReleaseLockObservation(observation); err != nil {
		t.Fatalf("complete release observation was refused: %v", err)
	}
	delete(observation.Repositories, "quic_go_go_source_hash")
	if err := validateReleaseLockObservation(observation); err == nil || !strings.Contains(err.Error(), "quic_go_go_source_hash is missing") {
		t.Fatalf("release observation without quic-go's source pin was accepted: %v", err)
	}
}

// A lock attested before URnetwork's quic-go was its own repository, as the
// checked-in lock was, stays admissible without the pin. It cannot pass a gate
// against a workspace that has quic-go, and a fresh rendering adds the pin.
func TestReleaseLockAttestedBeforeQuicGoIsAdmittedUntilRerendered(t *testing.T) {
	fresh := testReleaseLockFixture(t)
	observation := testReleaseLockObservation(t, fresh)
	attested := testReleaseLockFixture(t)
	delete(attested.Repositories, "quic_go_go_source_hash")
	if err := validateReleaseLockStatic(attested); err != nil {
		t.Fatalf("lock attested before quic-go was refused: %v", err)
	}
	ignored := releaseKeySet(releaseRepositoryAnnotationKeys, releaseRepositoryRetiredKeys)
	if err := compareLockSection("repositories", fresh.Repositories, observation.Repositories, ignored); err != nil {
		t.Fatalf("lock that pins quic-go differs from its own observation: %v", err)
	}
	if err := compareLockSection("repositories", attested.Repositories, observation.Repositories, ignored); err == nil || !strings.Contains(err.Error(), "quic_go_go_source_hash") {
		t.Fatalf("lock without quic-go's pin passed against a workspace that has quic-go: %v", err)
	}
	candidate, err := releaseLockWithObservation(attested, observation)
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Repositories["quic_go_go_source_hash"] != observation.Repositories["quic_go_go_source_hash"] {
		t.Fatalf("fresh rendering did not pin quic-go: %v", candidate.Repositories["quic_go_go_source_hash"])
	}
	if err := validateReleaseLockStatic(candidate); err != nil {
		t.Fatalf("fresh rendering with quic-go's pin was refused: %v", err)
	}
	attested.Repositories["quic_go_go_source_hash"] = "sha256:unresolved"
	if err := validateReleaseLockStatic(attested); err == nil {
		t.Fatal("lock with a malformed quic-go pin was admitted")
	}
}

// Observation hashes the Go source of every sibling module that a release lock
// pins, URnetwork's quic-go among them, each under the schema's observed key.
func TestReleaseLockHashesUrnetworkQuicGoSource(t *testing.T) {
	workspace, cfg := releaseObservationWorkspaceTest(t)
	hashes, modules, err := observeReleaseGoSources(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if modules["quic-go"] != filepath.Join(workspace, "quic-go") || !releaseSHA256.MatchString(hashes["quic_go_go_source_hash"]) {
		t.Fatalf("observation did not hash quic-go's source: %q %q", modules["quic-go"], hashes["quic_go_go_source_hash"])
	}
	observed := map[string]bool{}
	for _, key := range releaseRepositoryObservedKeys {
		if strings.HasSuffix(key, "_go_source_hash") {
			observed[key] = true
		}
	}
	if len(hashes) != len(observed) {
		t.Fatalf("observed Go source pins %v, want exactly %v", hashes, observed)
	}
	for key := range hashes {
		if !observed[key] {
			t.Errorf("observation hashed %s, which the release schema does not observe", key)
		}
	}
}

func TestCleanReleaseRepositorySnapshotRejectsDirtyAndMissingWorktrees(t *testing.T) {
	root := t.TempDir()
	runTestGit(t, root, "init", "-q")
	source := filepath.Join(root, "source.go")
	if err := os.WriteFile(source, []byte("package source\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, root, "add", "source.go")
	runTestGit(t, root, "commit", "-qm", "review source")
	repositories := []releaseRepository{{Name: "source", Root: root}}
	clean, err := cleanReleaseRepositorySnapshot(repositories)
	if err != nil || len(clean) != 1 {
		t.Fatalf("clean repository snapshot = %+v: %v", clean, err)
	}
	if err := os.WriteFile(source, []byte("package source\n\nconst Drift = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := cleanReleaseRepositorySnapshot(repositories); err == nil {
		t.Fatal("dirty tracked repository was accepted")
	}
	if err := os.WriteFile(source, []byte("package source\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "untracked.go"), []byte("package source\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := cleanReleaseRepositorySnapshot(repositories); err == nil {
		t.Fatal("repository with untracked source was accepted")
	}
	missing := []releaseRepository{{Name: "missing", Root: filepath.Join(root, "missing")}}
	if _, err := cleanReleaseRepositorySnapshot(missing); err == nil {
		t.Fatal("missing repository was accepted")
	}
}

// Builds the complete current release workspace as clean single-commit
// checkouts with exact module identities. No operator-proxy checkout exists.
func releaseObservationWorkspaceTest(t *testing.T) (string, *ResolvedConfig) {
	t.Helper()
	workspace := t.TempDir()
	for _, relative := range []string{
		"sn", "server", "vault", "config", "connect", "sdk", "glog", "quic-go",
		"goidenticons", "proxy", "userwireguard", "warp", "xops", "sn/evm/lib/forge-std",
		"sn/evm/lib/openzeppelin-contracts", "sn/evm/lib/openzeppelin-contracts-upgradeable",
	} {
		root := filepath.Join(workspace, filepath.FromSlash(relative))
		if err := os.MkdirAll(root, 0o700); err != nil {
			t.Fatal(err)
		}
		module := "module github.com/urnetwork/" + filepath.Base(root) + "\n"
		if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte(module), 0o600); err != nil {
			t.Fatal(err)
		}
		if relative == "sn" {
			if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("evm/lib/\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		runTestGit(t, root, "init", "-q")
		runTestGit(t, root, "add", ".")
		runTestGit(t, root, "commit", "-qm", "review local source")
	}
	cfg := &ResolvedConfig{Repos: RepoPaths{
		SN: filepath.Join(workspace, "sn"), Server: filepath.Join(workspace, "server"),
		Vault: filepath.Join(workspace, "vault"), PlatformConfig: filepath.Join(workspace, "config"),
	}}
	return workspace, cfg
}

// Release-lock observation fences every current repository without an
// operator-proxy checkout or observed field; the retired module is no input.
func TestReleaseLockObservationDoesNotRequireOperatorProxy(t *testing.T) {
	_, cfg := releaseObservationWorkspaceTest(t)
	repositories, err := releaseObservationRepositories(cfg)
	if err != nil {
		t.Fatalf("release observation required a retired repository: %v", err)
	}
	names := map[string]bool{}
	for _, repository := range repositories {
		names[repository.Name] = true
	}
	expected := []string{
		"sn", "server", "vault", "platform-config", "connect", "sdk", "glog", "quic-go", "goidenticons", "proxy",
		"userwireguard", "warp", "xops", "forge-std", "openzeppelin-contracts", "openzeppelin-contracts-upgradeable",
	}
	for _, name := range expected {
		if !names[name] {
			t.Errorf("release observation omitted %s", name)
		}
	}
	if names["operator-proxy"] || len(repositories) != len(expected) {
		t.Fatalf("release observation inventory = %+v", repositories)
	}
	if _, err := cleanReleaseRepositorySnapshot(repositories); err != nil {
		t.Fatal(err)
	}
	for _, key := range releaseRepositoryObservedKeys {
		if strings.HasPrefix(key, "operator_proxy") {
			t.Errorf("release observation still requires %s", key)
		}
	}
}

// The current build includes admission-only local modules in its source
// snapshot without adding required fields to retained release-lock bytes.
func TestReleaseLockObservationFencesAdmissionOnlyWarp(t *testing.T) {
	workspace, cfg := releaseObservationWorkspaceTest(t)
	repositories, err := releaseObservationRepositories(cfg)
	if err != nil {
		t.Fatal(err)
	}
	warpRoot := filepath.Join(workspace, "warp")
	found := false
	for _, repository := range repositories {
		if repository.Name == "warp" && repository.Root == warpRoot {
			found = true
		}
	}
	if !found {
		t.Fatal("current release observation omitted the server's local Warp dependency")
	}
	before, err := cleanReleaseRepositorySnapshot(repositories)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(workspace, "release.lock.yml")
	original := []byte("retained lock bytes\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	update := &preparedReleaseLockUpdate{Path: path, Original: original, Candidate: []byte("candidate\n"), Mode: 0o600, Snapshot: before}
	writes := 0
	writer := func(string, []byte, os.FileMode) error {
		writes++
		return nil
	}
	if err := os.WriteFile(filepath.Join(warpRoot, "services.go"), []byte("package warp\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if written, err := applyReleaseLockUpdate(cfg, update, writer); err == nil || written || writes != 0 || !strings.Contains(err.Error(), "release repository warp:") {
		t.Fatalf("dirty Warp reached lock publication: written=%t writes=%d error=%v", written, writes, err)
	}
	runTestGit(t, warpRoot, "add", "services.go")
	runTestGit(t, warpRoot, "commit", "-qm", "change Warp after observation")
	if written, err := applyReleaseLockUpdate(cfg, update, writer); err == nil || written || writes != 0 || !strings.Contains(err.Error(), "warp changed during observation") {
		t.Fatalf("changed Warp commit reached lock publication: written=%t writes=%d error=%v", written, writes, err)
	}
	retained, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(original, retained) {
		t.Fatalf("refused Warp change modified retained lock bytes: %v", err)
	}
	if err := os.WriteFile(filepath.Join(warpRoot, "go.mod"), []byte("module example.invalid/warp\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := releaseObservationRepositories(cfg); err == nil || !strings.Contains(err.Error(), "github.com/urnetwork/warp") {
		t.Fatalf("wrong Warp module identity was accepted: %v", err)
	}
}

func TestConfiguredReleaseLockPathRejectsSymlinkAndEscape(t *testing.T) {
	snRoot := t.TempDir()
	configDir := filepath.Join(snRoot, "sim-testnet")
	if err := os.Mkdir(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "release.lock.yml")
	if err := os.WriteFile(outside, []byte("outside\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &ResolvedConfig{ConfigPath: filepath.Join(configDir, "testnet.yml"), Config: &HarnessConfig{}, Repos: RepoPaths{SN: snRoot}}
	cfg.Config.Manifests.ReleaseLock = outside
	if _, _, err := configuredReleaseLockPath(cfg); err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("release-lock path escape was accepted: %v", err)
	}
	linked := filepath.Join(snRoot, "release.lock.yml")
	if err := os.Symlink(outside, linked); err != nil {
		t.Fatal(err)
	}
	cfg.Config.Manifests.ReleaseLock = linked
	if _, _, err := configuredReleaseLockPath(cfg); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("symlinked release-lock target was accepted: %v", err)
	}
}

func TestWriteReleaseLockUpdateIsAtomicAndRejectsChangedInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "release.lock.yml")
	original := []byte("original\n")
	candidate := []byte("candidate\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	update := &preparedReleaseLockUpdate{Path: path, Original: original, Candidate: candidate, Mode: 0o600}
	sentinel := errors.New("injected atomic write failure")
	writes := 0
	written, err := writeReleaseLockUpdate(update, func(string, []byte, os.FileMode) error {
		writes++
		return sentinel
	})
	if !errors.Is(err, sentinel) || written || writes != 1 {
		t.Fatalf("injected write failure = written %t writes %d error %v", written, writes, err)
	}
	afterFailure, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(afterFailure, original) {
		t.Fatal("failed atomic update changed the release lock")
	}
	if err := os.WriteFile(path, []byte("concurrent-change\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writes = 0
	written, err = writeReleaseLockUpdate(update, func(string, []byte, os.FileMode) error {
		writes++
		return nil
	})
	if err == nil || written || writes != 0 {
		t.Fatalf("changed input update = written %t writes %d error %v", written, writes, err)
	}
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	written, err = writeReleaseLockUpdate(update, atomicWrite)
	if err != nil || !written {
		t.Fatalf("atomic release-lock update = written %t error %v", written, err)
	}
	installed, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(installed, candidate) {
		t.Fatal("atomic update did not install exact candidate bytes")
	}
}
