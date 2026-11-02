// Approval key loading reuses the provisioned seed grammar and private-file
// checks; callers cannot accidentally generate or disclose an approval key.
package validator

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// Both supported encodings produce the same key without changing input bytes.
func TestApprovalKeyFileLoadsExistingPrivateSeed(t *testing.T) {
	seed := bytes.Repeat([]byte{0x63}, ed25519.SeedSize)
	want := ed25519.NewKeyFromSeed(seed)
	for index, raw := range [][]byte{seed, []byte(hex.EncodeToString(seed) + "\n")} {
		path := filepath.Join(identityTestStateDir(t), "approval.key")
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := loadApprovalKeyFile(path)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatal("approval seed encoding refused", index, err)
		}
		if after, err := os.ReadFile(path); err != nil || !bytes.Equal(after, raw) {
			t.Fatal("loading changed approval seed", err)
		}
	}
}

// No-create loading refuses missing, public, malformed and symlinked material.
func TestApprovalKeyFileRefusesInvalidOrSharedSeed(t *testing.T) {
	root := identityTestStateDir(t)
	path := filepath.Join(root, "approval.key")
	if key, err := loadApprovalKeyFile(path); err == nil || key != nil {
		t.Fatal("a missing approval key was created")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("approval key loading created an entry", err)
	}
	if err := os.WriteFile(path, bytes.Repeat([]byte{0x37}, 32), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if key, err := loadApprovalKeyFile(path); err == nil || key != nil {
		t.Fatal("a shared approval seed was admitted")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias.key")
	if err := os.Symlink(path, alias); err != nil {
		t.Fatal(err)
	}
	if key, err := loadApprovalKeyFile(alias); err == nil || key != nil {
		t.Fatal("a symlink approval key was admitted")
	}
	if err := os.WriteFile(path, []byte("not an encoded approval seed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if key, err := loadApprovalKeyFile(path); err == nil || key != nil {
		t.Fatal("an invalid approval seed was admitted")
	}
}
