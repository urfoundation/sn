package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
	cfg.Release.Runtime.SourceTag = "v447"
	cfg.Release.Runtime.SourceCommit = "1f090af85d1771c5d8ece1f0910576fbd129906e"
	cfg.Release.Runtime.SpecVersion = 447
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
