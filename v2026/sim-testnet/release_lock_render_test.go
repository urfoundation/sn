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
	return lock
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

func TestCleanReleaseRepositorySnapshotRejectsDirtyAndMissingWorktrees(t *testing.T) {
	root := t.TempDir()
	runTestGit(t, root, "init", "-q")
	runTestGit(t, root, "config", "user.email", "sim-testnet@example.invalid")
	runTestGit(t, root, "config", "user.name", "sim-testnet")
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
