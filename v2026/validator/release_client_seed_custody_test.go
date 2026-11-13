//go:build linux || darwin

package validator

// The release validator reads operator path keys independently of LoadIdentity.
// Static test-owned aliases and permissions exercise that actual read boundary.

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every fixture owns its private parent and exact raw key. No deployed key or
// process-global permission is inspected or changed by these tests.
func newReleaseClientSeedCustodyFile(t *testing.T) (string, []byte) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "client.key")
	raw := bytes.Repeat([]byte{0xab}, ed25519.SeedSize)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path, raw
}

// Read rejection must preserve the caller's occupied entry and its exact bytes,
// including an alias deliberately installed inside this fixture's ownership.
func checkReleaseClientSeedCustodyUnchanged(t *testing.T, path string, before os.FileInfo, raw []byte) {
	t.Helper()
	after, err := os.Lstat(path)
	if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() {
		t.Fatalf("client-key reader replaced or chmodded its input: %v", err)
	}
	actual, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(raw, actual) {
		t.Fatalf("client-key reader changed its input bytes: %v", err)
	}
}

// File and ancestor aliases must not supply signing authority. The causal
// witness requires the original reader to return the exact aliased key, not an
// unrelated I/O error or a missing fixture.
func TestReleaseClientSeedCustodyRejectsAliases(t *testing.T) {
	var admitted []string
	for _, kind := range []string{"symlink", "hardlink", "ancestor"} {
		path, raw := newReleaseClientSeedCustodyFile(t)
		alias := filepath.Join(filepath.Dir(path), "alias.key")
		switch kind {
		case "symlink":
			if err := os.Symlink(path, alias); err != nil {
				t.Fatal(err)
			}
		case "hardlink":
			if err := os.Link(path, alias); err != nil {
				t.Fatal(err)
			}
		case "ancestor":
			aliasParent := filepath.Join(filepath.Dir(filepath.Dir(path)), "alias-parent")
			if err := os.Symlink(filepath.Dir(path), aliasParent); err != nil {
				t.Fatal(err)
			}
			alias = filepath.Join(aliasParent, filepath.Base(path))
		}
		before, err := os.Lstat(alias)
		if err != nil {
			t.Fatal(err)
		}
		seed, readErr := loadClientSeed(alias)
		checkReleaseClientSeedCustodyUnchanged(t, alias, before, raw)
		if readErr == nil && bytes.Equal(seed, raw) {
			admitted = append(admitted, kind)
		} else if readErr == nil || len(seed) != 0 {
			t.Fatalf("%s alias returned unexpected key authority: %v", kind, readErr)
		}
	}
	if len(admitted) != 0 {
		t.Fatalf("release client-key reader accepted aliased signing authority: %v", admitted)
	}
}

// Group-readable or group-writable key files cannot grant private path-signing
// authority even when their immediate directory is privately owned.
func TestReleaseClientSeedCustodyRejectsSharedFileModes(t *testing.T) {
	var admitted []os.FileMode
	for _, mode := range []os.FileMode{0o644, 0o660} {
		path, raw := newReleaseClientSeedCustodyFile(t)
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		before, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		seed, readErr := loadClientSeed(path)
		checkReleaseClientSeedCustodyUnchanged(t, path, before, raw)
		if readErr == nil && bytes.Equal(seed, raw) {
			admitted = append(admitted, mode)
		} else if readErr == nil || len(seed) != 0 {
			t.Fatalf("mode %o returned unexpected key authority: %v", mode, readErr)
		}
	}
	if len(admitted) != 0 {
		t.Fatalf("release client-key reader accepted shared signing files: %o", admitted)
	}
}

// A replaceable parent defeats a private leaf. Refusal cannot silently chmod
// existing ancestry, generate a different key or repair a caller's namespace.
func TestReleaseClientSeedCustodyRejectsSharedParents(t *testing.T) {
	var admitted []os.FileMode
	for _, mode := range []os.FileMode{0o775, 0o777} {
		path, raw := newReleaseClientSeedCustodyFile(t)
		dir := filepath.Dir(path)
		if err := os.Chmod(dir, mode); err != nil {
			t.Fatal(err)
		}
		before, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		seed, readErr := loadClientSeed(path)
		checkReleaseClientSeedCustodyUnchanged(t, path, before, raw)
		parent, err := os.Lstat(dir)
		if err != nil || parent.Mode().Perm() != mode {
			t.Fatalf("client-key reader silently changed parent mode: %v", err)
		}
		if readErr == nil && bytes.Equal(seed, raw) {
			admitted = append(admitted, mode)
		} else if readErr == nil || len(seed) != 0 {
			t.Fatalf("parent %o returned unexpected key authority: %v", mode, readErr)
		}
	}
	if len(admitted) != 0 {
		t.Fatalf("release client-key reader accepted replaceable signing parents: %o", admitted)
	}
}

// The existing release format is raw32 or bare hexadecimal surrounded only by
// ASCII space, tab, CR and LF. Arbitrary raw32 bytes take precedence over text.
func TestReleaseClientSeedCustodyPreservesExistingFormats(t *testing.T) {
	path, raw := newReleaseClientSeedCustodyFile(t)
	encoded := hex.EncodeToString(raw)
	for _, format := range []struct {
		name string
		wire []byte
		want []byte
	}{
		{name: "raw", wire: raw, want: raw},
		{name: "hex", wire: []byte(encoded), want: raw},
		{name: "upper-hex", wire: []byte(strings.ToUpper(encoded)), want: raw},
		{name: "ascii-space", wire: []byte(" \t\r\n" + encoded + "\n\r\t "), want: raw},
		{name: "raw-space", wire: bytes.Repeat([]byte{' '}, ed25519.SeedSize), want: bytes.Repeat([]byte{' '}, ed25519.SeedSize)},
	} {
		if err := os.WriteFile(path, format.wire, 0o600); err != nil {
			t.Fatal(err)
		}
		before, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		seed, err := loadClientSeed(path)
		if err != nil || !bytes.Equal(seed, format.want) {
			t.Fatalf("%s lost its existing client-key format: %v", format.name, err)
		}
		checkReleaseClientSeedCustodyUnchanged(t, path, before, format.wire)
	}
}

// A shared reader must not silently broaden this caller's text grammar to
// hotkey 0x prefixes or Unicode/other whitespace that the old loader rejects.
func TestReleaseClientSeedCustodyPreservesRejectedFormats(t *testing.T) {
	path, raw := newReleaseClientSeedCustodyFile(t)
	encoded := hex.EncodeToString(raw)
	for _, wire := range [][]byte{
		nil, bytes.Repeat([]byte{'x'}, 31), bytes.Repeat([]byte{'x'}, 33),
		[]byte("0x" + encoded), []byte("0X" + encoded),
		[]byte("\u00a0" + encoded + "\u00a0"), []byte("\v" + encoded + "\v"),
		[]byte(encoded[:32] + " " + encoded[32:]),
	} {
		if err := os.WriteFile(path, wire, 0o600); err != nil {
			t.Fatal(err)
		}
		before, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		seed, readErr := loadClientSeed(path)
		if readErr == nil || len(seed) != 0 {
			t.Fatalf("rejected wire width%d returned key authority: %v", len(wire), readErr)
		}
		checkReleaseClientSeedCustodyUnchanged(t, path, before, wire)
	}
}

// Release startup consumes provisioned keys only; a missing key must remain
// missing rather than borrowing the generated-hotkey creation behavior.
func TestReleaseClientSeedCustodyNeverCreatesMissingKey(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "missing.key")
	seed, err := loadClientSeed(path)
	if !errors.Is(err, os.ErrNotExist) || len(seed) != 0 {
		t.Fatalf("missing provisioned key returned unexpected authority: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("missing-key read created or removed state: %v", err)
	}
}
