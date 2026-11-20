//go:build linux || darwin

package crv4

// Deterministic acquisition and publication barriers cover the shared hotkey
// and raw VPK custody implementation. Every key and namespace is test-owned.

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// Keeps the containing namespace private without changing TempDir's ancestry.
func seedCustodyTestPath(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, "seed")
}

// A paused loser has already observed absence and synced its different seed.
// The other real creator publishes first; neither API may replace that winner.
func TestSeedCustodyConcurrentCreatorsKeepPublishedWinner(t *testing.T) {
	for _, rawOnly := range []bool{false, true} {
		path := seedCustodyTestPath(t)
		ready, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
		var releaseOnce sync.Once
		unblock := func() { releaseOnce.Do(func() { close(release) }) }
		var loser [32]byte
		var loserCreated bool
		var loserErr error
		go func() {
			defer close(done)
			loser, loserCreated, loserErr = loadSeedFileOwned(path, true, rawOnly, seedFileHooks{
				random: bytes.NewReader(bytes.Repeat([]byte{0x11}, 32)),
				step: func(operation, _ string) error {
					if operation == "publish" {
						close(ready)
						<-release
					}
					return nil
				},
			})
		}()
		defer func() { unblock(); <-done }()
		select {
		case <-ready:
		case <-done:
			t.Fatalf("raw=%t loser did not reach publication: %v", rawOnly, loserErr)
		}
		winner, created, err := loadSeedFileOwned(path, true, rawOnly, seedFileHooks{random: bytes.NewReader(bytes.Repeat([]byte{0x22}, 32))})
		if err != nil || !created || !bytes.Equal(winner[:], bytes.Repeat([]byte{0x22}, 32)) {
			t.Fatalf("raw=%t winner did not durably publish: %t/%v", rawOnly, created, err)
		}
		before, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		unblock()
		<-done
		if loserErr != nil || loserCreated || loser != winner {
			t.Fatalf("raw=%t concurrent loser changed or failed to load the winner: %t/%v", rawOnly, loserCreated, loserErr)
		}
		after, err := os.Lstat(path)
		if err != nil || !os.SameFile(before, after) {
			t.Fatalf("raw=%t concurrent creation replaced winner inode: %v", rawOnly, err)
		}
		entries, err := os.ReadDir(filepath.Dir(path))
		if err != nil || len(entries) != 1 || entries[0].Name() != "seed" {
			t.Fatalf("raw=%t owned temporary was not joined: %v", rawOnly, err)
		}
	}
}

// A parent swapped before OpenRoot or after its actual descriptor was acquired
// must be refused before any seed write in either old or replacement directory.
func TestSeedCustodyRejectsParentAcquisitionReplacement(t *testing.T) {
	for _, stage := range []string{"parent-observed", "parent-opened", "publish"} {
		for _, fresh := range []bool{false, true} {
			path := filepath.Join(t.TempDir(), "private", "seed")
			parent, old := filepath.Dir(path), filepath.Dir(path)+"-preserved"
			if !fresh {
				if err := os.Mkdir(parent, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			fired := false
			seed, created, err := loadSeedFileOwned(path, true, false, seedFileHooks{step: func(operation, _ string) error {
				if operation != stage {
					return nil
				}
				fired = true
				if err := os.Rename(parent, old); err != nil {
					return err
				}
				return os.Mkdir(parent, 0o700)
			}})
			if !fired || err == nil || created || seed != ([32]byte{}) {
				t.Fatalf("%s fresh=%t parent replacement returned authority: %t/%v", stage, fresh, created, err)
			}
			for _, dir := range []string{old, parent} {
				entries, err := os.ReadDir(dir)
				if err != nil || len(entries) != 0 {
					t.Fatalf("%s fresh=%t changed parent contents at %s: %v", stage, fresh, dir, err)
				}
			}
		}
	}
}

// Losing an observed existing leaf cannot be reinterpreted as first creation.
// The old key remains in its test-owned recovery name and no entropy is used.
func TestSeedCustodyNeverRegeneratesAfterObservedLeafDisappears(t *testing.T) {
	path := seedCustodyTestPath(t)
	original := bytes.Repeat([]byte{0x45}, 32)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	fired := false
	seed, created, err := loadSeedFileOwned(path, true, true, seedFileHooks{
		random: bytes.NewReader(nil),
		step: func(operation, _ string) error {
			if operation == "leaf-observed" {
				fired = true
				return os.Rename(path, path+"-preserved")
			}
			return nil
		},
	})
	if !fired || err == nil || created || seed != ([32]byte{}) || errors.Is(err, errSeedFileMissing) || errors.Is(err, io.EOF) {
		t.Fatalf("lost occupied leaf was treated as authorized creation: %t/%v", created, err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("lost occupied leaf was recreated: %v", err)
	}
	raw, err := os.ReadFile(path + "-preserved")
	if err != nil || !bytes.Equal(raw, original) {
		t.Fatalf("original identity was not preserved: %v", err)
	}
}

// Every existing symlink component is refused, even when it would resolve to
// an otherwise private parent. No directory is created under the alias target.
func TestSeedCustodyRejectsSymlinkParentAndAncestry(t *testing.T) {
	for _, nested := range []bool{false, true} {
		base, unrelated := t.TempDir(), t.TempDir()
		alias := filepath.Join(base, "alias")
		if err := os.Symlink(unrelated, alias); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(alias, "seed")
		if nested {
			path = filepath.Join(alias, "child", "seed")
		}
		seed, created, err := LoadOrCreateSeedFile(path)
		if err == nil || created || seed != ([32]byte{}) {
			t.Fatalf("nested=%t accepted parent alias: %t/%v", nested, created, err)
		}
		entries, err := os.ReadDir(unrelated)
		if err != nil || len(entries) != 0 {
			t.Fatalf("nested=%t wrote beneath aliased ancestry: %v", nested, err)
		}
	}
}

// Descriptor acquisition rejects regular-file, symlink and FIFO replacements;
// the latter must not block waiting for another process to open the FIFO.
func TestSeedCustodyRejectsLeafAcquisitionReplacement(t *testing.T) {
	for _, replacement := range []string{"regular", "symlink", "fifo"} {
		path := seedCustodyTestPath(t)
		original := bytes.Repeat([]byte{0x33}, 32)
		if err := os.WriteFile(path, original, 0o600); err != nil {
			t.Fatal(err)
		}
		foreign := filepath.Join(t.TempDir(), "foreign")
		if err := os.WriteFile(foreign, bytes.Repeat([]byte{0x44}, 32), 0o600); err != nil {
			t.Fatal(err)
		}
		fired := false
		seed, created, err := loadSeedFileOwned(path, true, false, seedFileHooks{step: func(operation, _ string) error {
			if operation != "leaf-observed" {
				return nil
			}
			fired = true
			if err := os.Rename(path, path+"-preserved"); err != nil {
				return err
			}
			switch replacement {
			case "regular":
				return os.WriteFile(path, bytes.Repeat([]byte{0x44}, 32), 0o600)
			case "symlink":
				return os.Symlink(foreign, path)
			default:
				return syscall.Mkfifo(path, 0o600)
			}
		}})
		if !fired || err == nil || created || seed != ([32]byte{}) {
			t.Fatalf("%s replacement returned usable seed: %t/%v", replacement, created, err)
		}
		for preservedPath, expected := range map[string][]byte{path + "-preserved": original, foreign: bytes.Repeat([]byte{0x44}, 32)} {
			raw, err := os.ReadFile(preservedPath)
			if err != nil || !bytes.Equal(raw, expected) {
				t.Fatalf("%s acquisition changed an existing identity: %v", replacement, err)
			}
		}
	}
}

// A warm descriptor is insufficient if its same inode is rewritten during the
// bounded read. Force a distinct write timestamp without relying on wall time.
func TestSeedCustodyRejectsSameInodeRewriteDuringRead(t *testing.T) {
	path := seedCustodyTestPath(t)
	if err := os.WriteFile(path, bytes.Repeat([]byte{0x55}, 32), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	fired := false
	seed, _, err := loadSeedFileOwned(path, false, false, seedFileHooks{step: func(operation, _ string) error {
		if operation != "seed-read" {
			return nil
		}
		fired = true
		if err := os.WriteFile(path, bytes.Repeat([]byte{0x66}, 32), 0o600); err != nil {
			return err
		}
		return os.Chtimes(path, time.Unix(1, 0), time.Unix(1, 0))
	}})
	after, statErr := os.Lstat(path)
	if !fired || err == nil || seed != ([32]byte{}) || statErr != nil || !os.SameFile(before, after) || before.Size() != after.Size() {
		t.Fatalf("same-inode rewrite was not rejected at read completion: %v/%v", err, statErr)
	}
}

// Private read-only files remain valid. Shared, executable and hardlinked
// files fail through both read-only and load-or-create paths without repair.
func TestSeedCustodyEnforcesPrivateFilePolicyWithoutRepair(t *testing.T) {
	for _, variation := range []string{"readonly", "shared", "group", "executable", "hardlink", "directory"} {
		path := seedCustodyTestPath(t)
		original := bytes.Repeat([]byte{0x77}, 32)
		if variation == "directory" {
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
		} else {
			if err := os.WriteFile(path, original, 0o600); err != nil {
				t.Fatal(err)
			}
			mode := os.FileMode(0o600)
			switch variation {
			case "readonly":
				mode = 0o400
			case "shared":
				mode = 0o644
			case "group":
				mode = 0o640
			case "executable":
				mode = 0o700
			case "hardlink":
				if err := os.Link(path, path+"-alias"); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Chmod(path, mode); err != nil {
				t.Fatal(err)
			}
		}
		before, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, create := range []bool{false, true} {
			seed, created, err := loadSeedFileOwned(path, create, false, seedFileHooks{})
			if variation == "readonly" {
				if err != nil || created || !bytes.Equal(seed[:], original) {
					t.Fatalf("read-only private seed refused: %t/%v", created, err)
				}
			} else if err == nil || created || seed != ([32]byte{}) {
				t.Fatalf("%s create=%t returned custody: %t/%v", variation, create, created, err)
			}
		}
		after, err := os.Lstat(path)
		if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() {
			t.Fatalf("%s was replaced or chmodded: %v", variation, err)
		}
		if variation != "directory" {
			raw, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(raw, original) {
				t.Fatalf("%s key bytes changed: %v", variation, err)
			}
		}
	}
}

// Malformed or oversized occupied files never fall through to regeneration.
// The oversize case is rejected before descriptor acquisition or allocation.
func TestSeedCustodyBoundsReadsAndNeverRegeneratesMalformedFiles(t *testing.T) {
	for _, raw := range [][]byte{[]byte("bad seed"), bytes.Repeat([]byte{'a'}, maximumSeedFileBytes+1)} {
		path := seedCustodyTestPath(t)
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		opened := false
		seed, created, err := loadSeedFileOwned(path, true, false, seedFileHooks{
			random: bytes.NewReader(nil),
			step: func(operation, _ string) error {
				opened = opened || operation == "leaf-observed"
				return nil
			},
		})
		if err == nil || created || seed != ([32]byte{}) || len(raw) > maximumSeedFileBytes && opened {
			t.Fatalf("malformed/oversized seed returned custody or opened oversized file: %t/%v", created, err)
		}
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(raw, after) {
			t.Fatalf("invalid occupied seed was changed: %v", err)
		}
	}
}

// Existing compatible parent modes remain unchanged; writable-by-others
// parents fail without chmod. Newly required ancestry remains private.
func TestSeedCustodyPreservesParentModesAndNestedCreation(t *testing.T) {
	for _, mode := range []os.FileMode{0o700, 0o755, 0o775, 0o777} {
		path := seedCustodyTestPath(t)
		parent := filepath.Dir(path)
		if err := os.Chmod(parent, mode); err != nil {
			t.Fatal(err)
		}
		seed, created, err := LoadOrCreateSeedFile(path)
		if mode&0o022 == 0 {
			if err != nil || !created {
				t.Fatalf("parent %o refused compatible creation: %v", mode, err)
			}
		} else if err == nil || created || seed != ([32]byte{}) {
			t.Fatalf("parent %o granted unsafe creation: %t/%v", mode, created, err)
		}
		info, err := os.Lstat(parent)
		if err != nil || info.Mode().Perm() != mode {
			t.Fatalf("parent %o silently chmodded: %v", mode, err)
		}
	}
	base := t.TempDir()
	path := filepath.Join(base, "one", "two", "three", "seed")
	if _, created, err := LoadOrCreateSeedFile(path); err != nil || !created {
		t.Fatalf("nested private creation: %t/%v", created, err)
	}
	for _, rel := range []string{"one", "one/two", "one/two/three"} {
		info, err := os.Lstat(filepath.Join(base, rel))
		if err != nil || info.Mode().Perm() != 0o700 {
			t.Fatalf("created parent is not private: %v", err)
		}
	}
}

// A valid private file in an explicitly shared immediate parent cannot grant
// custody, even beneath a private test ancestor. Neither read nor create may
// repair that layout, replace existing bytes, or publish a missing identity.
func TestSeedCustodyRejectsSharedParentBeforeReadingValidFormats(t *testing.T) {
	for _, mode := range []os.FileMode{0o775, 0o777} {
		for _, rawOnly := range []bool{false, true} {
			path := seedCustodyTestPath(t)
			parent := filepath.Dir(path)
			original := []byte("0x" + aliceSeedHex + "\n")
			if rawOnly {
				original = bytes.Repeat([]byte{0x31}, 32)
			}
			if err := os.WriteFile(path, original, 0o600); err != nil {
				t.Fatal(err)
			}
			before, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(parent, mode); err != nil {
				t.Fatal(err)
			}
			for _, create := range []bool{false, true} {
				seed, created, err := loadSeedFileOwned(path, create, rawOnly, seedFileHooks{})
				if err == nil || !strings.Contains(err.Error(), "seed parent must be owned and not writable by group or others") || created || seed != ([32]byte{}) {
					t.Fatalf("parent=%o raw=%t create=%t returned custody before parent refusal: %t/%v", mode, rawOnly, create, created, err)
				}
			}
			missing := filepath.Join(parent, "missing.seed")
			if seed, created, err := loadSeedFileOwned(missing, true, rawOnly, seedFileHooks{}); err == nil || created || seed != ([32]byte{}) {
				t.Fatalf("parent=%o raw=%t created a new identity in shared parent: %t/%v", mode, rawOnly, created, err)
			}
			after, err := os.Lstat(path)
			if err != nil || !os.SameFile(before, after) || after.Mode() != before.Mode() {
				t.Fatalf("parent=%o raw=%t replaced or changed occupied seed: %v", mode, rawOnly, err)
			}
			actual, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(actual, original) {
				t.Fatalf("parent=%o raw=%t changed valid format bytes: %v", mode, rawOnly, err)
			}
			parentInfo, err := os.Lstat(parent)
			if err != nil || parentInfo.Mode().Perm() != mode {
				t.Fatalf("parent=%o raw=%t silently repaired parent: %v", mode, rawOnly, err)
			}
			entries, err := os.ReadDir(parent)
			if err != nil || len(entries) != 1 || entries[0].Name() != filepath.Base(path) {
				t.Fatalf("parent=%o raw=%t mutated shared namespace: %v", mode, rawOnly, err)
			}
		}
	}
}

// Faults before publication leave no key; a failure after the no-replace
// publication keeps those exact bytes but never returns usable seed authority.
func TestSeedCustodyDurabilityFailureReturnsNoAuthorityAndRecovers(t *testing.T) {
	for _, stage := range []string{"seed-sync", "publish", "parent-sync", "load-sync"} {
		path := seedCustodyTestPath(t)
		failure := errors.New("test-controlled seed durability failure")
		expected := bytes.Repeat([]byte{0x88}, 32)
		seed, created, err := loadSeedFileOwned(path, true, true, seedFileHooks{
			random: bytes.NewReader(expected),
			step: func(operation, _ string) error {
				if operation == stage {
					return failure
				}
				return nil
			},
		})
		if !errors.Is(err, failure) || created || seed != ([32]byte{}) {
			t.Fatalf("%s returned undurable custody: %t/%v", stage, created, err)
		}
		published := stage == "parent-sync" || stage == "load-sync"
		raw, readErr := os.ReadFile(path)
		if published {
			if readErr != nil || !bytes.Equal(raw, expected) {
				t.Fatalf("%s lost its unacknowledged published seed: %v", stage, readErr)
			}
			loaded, created, err := LoadOrCreateRawSeedFile(path)
			if err != nil || created || !bytes.Equal(loaded[:], expected) {
				t.Fatalf("%s could not acknowledge the original seed on retry: %t/%v", stage, created, err)
			}
		} else if !errors.Is(readErr, os.ErrNotExist) {
			t.Fatalf("%s published a seed before its durable boundary: %v", stage, readErr)
		}
		entries, err := os.ReadDir(filepath.Dir(path))
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".seed-pending-") {
				t.Fatalf("%s left its owned temporary file", stage)
			}
		}
	}
}

// Entropy and short-write failures cannot expose either partial bytes or a
// usable generated seed. Existing files do not consume the entropy source.
func TestSeedCustodyEntropyAndShortWriteFailBeforePublication(t *testing.T) {
	for _, shortWrite := range []bool{false, true} {
		path := seedCustodyTestPath(t)
		hooks := seedFileHooks{random: bytes.NewReader([]byte{1, 2, 3})}
		if shortWrite {
			hooks.random = bytes.NewReader(bytes.Repeat([]byte{0x99}, 32))
			hooks.write = func(file *os.File, data []byte) (int, error) { return file.Write(data[:3]) }
		}
		seed, created, err := loadSeedFileOwned(path, true, true, hooks)
		if err == nil || created || seed != ([32]byte{}) || shortWrite && !errors.Is(err, io.ErrShortWrite) {
			t.Fatalf("short=%t partial creation returned custody: %t/%v", shortWrite, created, err)
		}
		entries, err := os.ReadDir(filepath.Dir(path))
		if err != nil || len(entries) != 0 {
			t.Fatalf("short=%t left partial seed artifacts: %v", shortWrite, err)
		}
	}
}
