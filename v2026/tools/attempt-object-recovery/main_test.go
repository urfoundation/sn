//go:build linux || darwin

// Staging tests authenticate retained files instead of trusting prior writes.
package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// Reusing a staged identity must verify the current file, including same-size damage.
func TestObjectStagingRejectsChangedExistingBytes(t *testing.T) {
	path := t.TempDir()
	root, err := os.OpenRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := root.Mkdir("metadata", 0o700); err != nil {
		t.Fatal(err)
	}
	raw := []byte("synthetic public metadata\n")
	hash := fmt.Sprintf("0x%x", sha256.Sum256(raw))
	staging := &objectStaging{root: root, maximumBytes: 1024, maximumObjects: 1, objectKVs: map[string]stagedObject{}}
	if err := staging.write(t.Context(), "metadata", hash, raw); err != nil {
		t.Fatal(err)
	}
	name, err := stagedObjectPath("metadata", hash)
	if err != nil {
		t.Fatal(err)
	}
	changed := append([]byte(nil), raw...)
	changed[0] ^= 1
	if err := root.WriteFile(name, changed, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := staging.write(t.Context(), "metadata", hash, raw); err == nil {
		t.Fatal("same-size damaged staged object was treated as already verified")
	}
}

// Pinned public documents refuse symlink aliases and wrong content identities.
func TestPinnedPublicFileRejectsAliasesAndWrongHash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "public.json")
	raw := []byte("{\"synthetic\":true}\n")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	hash := fmt.Sprintf("%x", sha256.Sum256(raw))
	if _, err := readPinnedPublicFile(path, hash, 1024); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "alias.json")
	if err := os.Symlink(path, alias); err != nil {
		t.Fatal(err)
	}
	if _, err := readPinnedPublicFile(alias, hash, 1024); err == nil {
		t.Fatal("aliased public input was accepted")
	}
	if _, err := readPinnedPublicFile(path, fmt.Sprintf("%x", sha256.Sum256([]byte("other synthetic bytes"))), 1024); err == nil {
		t.Fatal("unmatched public input pin was accepted")
	}
}
