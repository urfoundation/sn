package validator

// Identity creation must preserve occupied key entries even when their target
// is absent. Test-owned symlinks force the read-miss path without timing races.

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// The public identity loader may not replace a dangling path-key alias with a
// newly generated identity and report that unrelated authority as initialized.
func TestIdentityCustodyRejectsDanglingVpkBeforeReplacement(t *testing.T) {
	privateDirectory := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(privateDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	seedPath := filepath.Join(privateDirectory, vpkSeedFileName)
	unrelatedSeedPath := filepath.Join(t.TempDir(), "must-not-create.seed")
	if err := os.Symlink(unrelatedSeedPath, seedPath); err != nil {
		t.Fatal(err)
	}
	before, err := os.Lstat(seedPath)
	if err != nil || before.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("fixture did not install an occupied path-key symlink: %v", err)
	}
	identity, loadErr := LoadIdentity(IdentityOptions{StateDir: privateDirectory})
	after, err := os.Lstat(seedPath)
	if err != nil || !os.SameFile(before, after) || after.Mode()&os.ModeSymlink == 0 {
		t.Fatal("validator path-key creation replaced an occupied dangling symlink")
	}
	if _, err := os.Lstat(unrelatedSeedPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("validator path-key creation wrote to the unrelated alias target")
	}
	if loadErr == nil || identity != nil {
		t.Fatalf("occupied path-key entry returned a usable identity: %v", loadErr)
	}
}
