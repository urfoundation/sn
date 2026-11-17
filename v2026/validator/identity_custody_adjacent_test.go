//go:build linux || darwin

package validator

// Public identity loading shares strict raw seed custody with CRv4, while
// retaining its existing EVM, address and raw VPK wire conventions.

import (
	"bytes"
	"crypto/ed25519"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The VPK wrapper must not accept hex-only input, shared keys, or hardlinks
// even though the generic hotkey reader intentionally accepts hex seed files.
func TestIdentityCustodyRejectsNonRawAndUnsafeVpkFiles(t *testing.T) {
	for _, variation := range []string{"hex", "shared", "hardlink", "symlink"} {
		dir := filepath.Join(t.TempDir(), "private")
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, vpkSeedFileName)
		raw := bytes.Repeat([]byte{0x31}, ed25519.SeedSize)
		if variation == "hex" {
			raw = []byte("0x" + string(bytes.Repeat([]byte{'1'}, 64)) + "\n")
		}
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		switch variation {
		case "shared":
			if err := os.Chmod(path, 0o644); err != nil {
				t.Fatal(err)
			}
		case "hardlink":
			if err := os.Link(path, path+"-alias"); err != nil {
				t.Fatal(err)
			}
		case "symlink":
			if err := os.Rename(path, path+"-target"); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(path+"-target", path); err != nil {
				t.Fatal(err)
			}
		}
		before, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		identity, err := LoadIdentity(IdentityOptions{StateDir: dir})
		if err == nil || identity != nil {
			t.Fatalf("%s VPK returned identity authority: %v", variation, err)
		}
		after, err := os.Lstat(path)
		if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() {
			t.Fatalf("%s VPK was replaced or chmodded: %v", variation, err)
		}
		actual, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(raw, actual) {
			t.Fatalf("%s VPK bytes changed: %v", variation, err)
		}
	}
}

// Both VPK and hotkey creation retain the compatible existing parent mode;
// neither identity entry point may repair unrelated ancestry by chmod.
func TestIdentityCustodyPreservesParentModeAndReloadedKeys(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	first, err := LoadIdentity(IdentityOptions{StateDir: dir, LoadHotkey: true})
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadIdentity(IdentityOptions{StateDir: dir, LoadHotkey: true})
	if err != nil || !bytes.Equal(first.Vpk, second.Vpk) || first.Hotkey.Address() != second.Hotkey.Address() {
		t.Fatalf("private identity changed on reload: %v", err)
	}
	info, err := os.Lstat(dir)
	if err != nil || info.Mode().Perm() != 0o755 {
		t.Fatalf("identity creation silently chmodded its existing parent: %v", err)
	}
}

// The public wrapper cannot silently repair a shared namespace, whether its
// identity files are absent or already contain valid private test-owned keys.
func TestIdentityCustodyRejectsSharedParentWithoutCreatingKeys(t *testing.T) {
	for _, mode := range []os.FileMode{0o775, 0o777} {
		for _, occupied := range []bool{false, true} {
			dir := identityTestStateDir(t)
			originals := map[string][]byte{}
			infos := map[string]os.FileInfo{}
			if occupied {
				originals[vpkSeedFileName] = bytes.Repeat([]byte{0x31}, ed25519.SeedSize)
				originals["hotkey.seed"] = bytes.Repeat([]byte{0x42}, ed25519.SeedSize)
				for name, raw := range originals {
					path := filepath.Join(dir, name)
					if err := os.WriteFile(path, raw, 0o600); err != nil {
						t.Fatal(err)
					}
					info, err := os.Lstat(path)
					if err != nil {
						t.Fatal(err)
					}
					infos[name] = info
				}
			}
			if err := os.Chmod(dir, mode); err != nil {
				t.Fatal(err)
			}
			identity, err := LoadIdentity(IdentityOptions{StateDir: dir, LoadHotkey: true})
			if err == nil || identity != nil || !strings.Contains(err.Error(), "seed parent must be owned and not writable by group or others") {
				t.Fatalf("parent=%o occupied=%t wrapper returned unsafe custody: %v", mode, occupied, err)
			}
			parent, err := os.Lstat(dir)
			if err != nil || parent.Mode().Perm() != mode {
				t.Fatalf("parent=%o occupied=%t wrapper silently repaired mode: %v", mode, occupied, err)
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != len(originals) {
				t.Fatalf("parent=%o occupied=%t wrapper created or removed an identity: %v", mode, occupied, err)
			}
			for name, expected := range originals {
				path := filepath.Join(dir, name)
				info, err := os.Lstat(path)
				if err != nil || !os.SameFile(infos[name], info) || info.Mode() != infos[name].Mode() {
					t.Fatalf("parent=%o replaced or chmodded %s: %v", mode, name, err)
				}
				raw, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(raw, expected) {
					t.Fatalf("parent=%o changed existing %s bytes: %v", mode, name, err)
				}
			}
		}
	}
}
