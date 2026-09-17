package crv4

// Key creation cannot interpret an occupied dangling leaf as a free namespace.
// All keys and targets here belong to the test; no configured wallet is read.

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// A deterministic missing target drives the production read-miss/create path.
// Refusal must preserve both the occupied entry and the unrelated namespace.
func TestSeedCustodyRejectsDanglingLeafBeforeCreation(t *testing.T) {
	privateDirectory := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(privateDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	seedPath := filepath.Join(privateDirectory, "hotkey.seed")
	unrelatedSeedPath := filepath.Join(t.TempDir(), "must-not-create.seed")
	if err := os.Symlink(unrelatedSeedPath, seedPath); err != nil {
		t.Fatal(err)
	}
	before, err := os.Lstat(seedPath)
	if err != nil || before.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("fixture did not install an occupied leaf symlink: %v", err)
	}
	seed, created, createErr := LoadOrCreateSeedFile(seedPath)
	if _, err := os.Lstat(unrelatedSeedPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("seed creation followed a dangling leaf symlink into an unrelated namespace")
	}
	after, err := os.Lstat(seedPath)
	if err != nil || !os.SameFile(before, after) || after.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("seed creation changed the already occupied leaf: %v", err)
	}
	if createErr == nil || created || seed != ([32]byte{}) {
		t.Fatalf("occupied key path returned usable creation authority: created=%t error=%v", created, createErr)
	}
}
