//go:build linux || darwin

package validator

// Scratch is a fresh, descriptor-anchored private owner, never a pathname
// permission to rewrite another directory. Barriers deterministically swap
// names, aliases and descriptor bytes while real storage performs its checks.

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// Existing directories, files and symlinks are never repaired or reused.
func TestAttemptCutV2SealScratchRequiresFreshLeaf(t *testing.T) {
	fixture := newAttemptCutV2SealTestFixture(t, 8, 0, 1)
	for _, variation := range []string{"directory", "file", "symlink"} {
		options, objects := newAttemptCutV2SealTestOptions(t, fixture)
		sentinel := []byte("preserved unrelated scratch owner")
		target := options.ScratchDirectory
		switch variation {
		case "directory":
			if err := os.Mkdir(target, 0o700); err != nil {
				t.Fatal(err)
			}
			target = filepath.Join(target, "sentinel")
		case "symlink":
			target += "-target"
		}
		if err := os.WriteFile(target, sentinel, 0o600); err != nil {
			t.Fatal(err)
		}
		if variation == "symlink" {
			if err := os.Symlink(target, options.ScratchDirectory); err != nil {
				t.Fatal(err)
			}
		}
		before, err := os.Lstat(options.ScratchDirectory)
		if err != nil {
			t.Fatal(err)
		}
		cut, result, err := SealAttemptCutV2(context.Background(), fixture.ledger, fixture.expected, fixture.policy, fixture.key, fixture.bounds, options)
		assertAttemptCutV2SealTestFailure(t, cut, result, err, "must be fresh")
		after, statErr := os.Lstat(options.ScratchDirectory)
		actual, readErr := os.ReadFile(target)
		if statErr != nil || readErr != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !bytes.Equal(actual, sentinel) || objects.writes != 0 || objects.reads != 0 {
			t.Fatalf("%s existing scratch was changed: %v/%v", variation, statErr, readErr)
		}
	}
}

// Keep the original directory inode alive while replacing its displayed name
// with another valid private directory. No spool may appear in the replacement.
func TestAttemptCutV2SealScratchRejectsReplacedCreatedLeaf(t *testing.T) {
	fixture := newAttemptCutV2SealTestFixture(t, 8, 0, 1)
	options, objects := newAttemptCutV2SealTestOptions(t, fixture)
	var original os.FileInfo
	cut, result, err := sealAttemptCutV2WithHooks(context.Background(), fixture.ledger, fixture.expected, fixture.policy, fixture.key, fixture.bounds, options, attemptCutV2SealHooks{ScratchCreated: func(path string) error {
		var err error
		original, err = os.Lstat(path)
		if err != nil {
			return err
		}
		if err := os.Rename(path, path+"-retained"); err != nil {
			return err
		}
		if err := os.Mkdir(path, 0o700); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(path, "sentinel"), []byte("replacement owner"), 0o600)
	}})
	assertAttemptCutV2SealTestFailure(t, cut, result, err, "anchor changed")
	entries, err := os.ReadDir(options.ScratchDirectory)
	retained, retainedErr := os.Lstat(options.ScratchDirectory + "-retained")
	if err != nil || len(entries) != 1 || entries[0].Name() != "sentinel" || retainedErr != nil || original == nil || !os.SameFile(original, retained) || objects.writes != 0 || objects.reads != 0 {
		t.Fatalf("replacement scratch was mutated or original evidence lost: %v/%v", err, retainedErr)
	}
}

// Once descriptors exist, replacing the parent still cannot redirect replay
// scratch creation into a new valid private namespace at the same path.
func TestAttemptCutV2SealScratchRejectsReplacedParentBeforeReplay(t *testing.T) {
	fixture := newAttemptCutV2SealTestFixture(t, 8, 1, 0)
	options, objects := newAttemptCutV2SealTestOptions(t, fixture)
	base := t.TempDir()
	parent := filepath.Join(base, "parent")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	options.ScratchDirectory = filepath.Join(parent, "seal-scratch")
	replaced := false
	cut, result, err := sealAttemptCutV2WithHooks(context.Background(), fixture.ledger, fixture.expected, fixture.policy, fixture.key, fixture.bounds, options, attemptCutV2SealHooks{Step: func(operation, _ string) error {
		if operation != "before-fetch-back" || replaced {
			return nil
		}
		replaced = true
		if err := os.Rename(parent, filepath.Join(base, "retained-parent")); err != nil {
			return err
		}
		if err := os.Mkdir(parent, 0o700); err != nil {
			return err
		}
		if err := os.Mkdir(options.ScratchDirectory, 0o700); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(options.ScratchDirectory, "sentinel"), []byte("replacement parent owner"), 0o600)
	}})
	assertAttemptCutV2SealTestFailure(t, cut, result, err, "anchor changed")
	entries, err := os.ReadDir(options.ScratchDirectory)
	if err != nil || !replaced || len(entries) != 1 || entries[0].Name() != "sentinel" || objects.reads != 0 || objects.writes == 0 {
		t.Fatalf("parent replacement redirected replay or lost staged evidence: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(base, "retained-parent", "seal-scratch", "records.descriptors")); err != nil {
		t.Fatalf("original parent evidence disappeared: %v", err)
	}
}

// The nested replay also compares the actual opened descriptor against its
// retained fresh inode before a lock, sync or database write can occur.
func TestAttemptCutV2SealReplayScratchRejectsReplacement(t *testing.T) {
	fixture := newAttemptCutV2SealTestFixture(t, 8, 0, 1)
	for _, replaceParent := range []bool{false, true} {
		options, objects := newAttemptCutV2SealTestOptions(t, fixture)
		replacement := ""
		cut, result, err := sealAttemptCutV2WithHooks(context.Background(), fixture.ledger, fixture.expected, fixture.policy, fixture.key, fixture.bounds, options, attemptCutV2SealHooks{Replay: attemptCutV2ReplayHooks{ScratchCreated: func(path string) error {
			replacement = path
			moved := path
			if replaceParent {
				moved = filepath.Dir(path)
			}
			if err := os.Rename(moved, moved+"-retained"); err != nil {
				return err
			}
			if err := os.Mkdir(moved, 0o700); err != nil {
				return err
			}
			if replaceParent {
				if err := os.Mkdir(path, 0o700); err != nil {
					return err
				}
			}
			return os.WriteFile(filepath.Join(path, "sentinel"), []byte("unrelated replay owner"), 0o600)
		}}})
		assertAttemptCutV2SealTestFailure(t, cut, result, err, "")
		entries, readErr := os.ReadDir(replacement)
		if readErr != nil || replacement == "" || len(entries) != 1 || entries[0].Name() != "sentinel" || objects.reads != 0 || objects.writes == 0 {
			t.Fatalf("parent=%t nested replay replacement was mutated: %v", replaceParent, readErr)
		}
	}
}

// Aliases and preexisting names cannot become a sealer descriptor spool.
// Exclusive creation refuses them before any caller-owned target is changed.
func TestAttemptCutV2SealSpoolRejectsPreexistingAlias(t *testing.T) {
	fixture := newAttemptCutV2SealTestFixture(t, 8, 0, 1)
	for _, variation := range []string{"file", "symlink", "hardlink"} {
		options, objects := newAttemptCutV2SealTestOptions(t, fixture)
		target := filepath.Join(filepath.Dir(options.ScratchDirectory), "unrelated-target")
		sentinel := []byte("unrelated spool target")
		if err := os.WriteFile(target, sentinel, 0o600); err != nil {
			t.Fatal(err)
		}
		cut, result, err := sealAttemptCutV2WithHooks(context.Background(), fixture.ledger, fixture.expected, fixture.policy, fixture.key, fixture.bounds, options, attemptCutV2SealHooks{ScratchCreated: func(path string) error {
			spool := filepath.Join(path, "records.descriptors")
			switch variation {
			case "symlink":
				return os.Symlink(target, spool)
			case "hardlink":
				return os.Link(target, spool)
			default:
				return os.WriteFile(spool, sentinel, 0o600)
			}
		}})
		assertAttemptCutV2SealTestFailure(t, cut, result, err, "")
		actual, readErr := os.ReadFile(target)
		if readErr != nil || !bytes.Equal(actual, sentinel) || objects.writes != 0 || objects.reads != 0 {
			t.Fatalf("%s alias target was changed: %v", variation, readErr)
		}
	}
}

// After open, every actual inode is still private, single-link and named by
// its original descriptor; matching path permissions alone are insufficient.
func TestAttemptCutV2SealSpoolRejectsChangedInodeOrPermissions(t *testing.T) {
	fixture := newAttemptCutV2SealTestFixture(t, 8, 0, 1)
	for _, variation := range []string{"public", "hardlink", "replacement", "symlink"} {
		options, objects := newAttemptCutV2SealTestOptions(t, fixture)
		changed := false
		cut, result, err := sealAttemptCutV2WithHooks(context.Background(), fixture.ledger, fixture.expected, fixture.policy, fixture.key, fixture.bounds, options, attemptCutV2SealHooks{Step: func(operation, name string) error {
			if operation != "after-spool-open" || name != "records.descriptors" || changed {
				return nil
			}
			changed = true
			path := filepath.Join(options.ScratchDirectory, name)
			switch variation {
			case "public":
				return os.Chmod(path, 0o644)
			case "hardlink":
				return os.Link(path, path+"-alias")
			default:
				if err := os.Rename(path, path+"-retained"); err != nil {
					return err
				}
				if variation == "symlink" {
					return os.Symlink(path+"-retained", path)
				}
				return os.WriteFile(path, []byte("replacement owner"), 0o600)
			}
		}})
		assertAttemptCutV2SealTestFailure(t, cut, result, err, "spool inode or bound changed")
		if !changed || objects.writes != 0 || objects.reads != 0 {
			t.Fatalf("%s spool change reached object I/O", variation)
		}
	}
}

// A private same-inode mutation is detected by the writer's chained exact
// descriptor replay, not merely by checking names, file length or permissions.
func TestAttemptCutV2SealSpoolRejectsChangedDescriptorBytes(t *testing.T) {
	fixture := newAttemptCutV2SealTestFixture(t, 8, 1, 0)
	options, objects := newAttemptCutV2SealTestOptions(t, fixture)
	changed := false
	cut, result, err := sealAttemptCutV2WithHooks(context.Background(), fixture.ledger, fixture.expected, fixture.policy, fixture.key, fixture.bounds, options, attemptCutV2SealHooks{Step: func(operation, name string) error {
		if operation != "before-spool-read" || name != "records.descriptors" || changed {
			return nil
		}
		changed = true
		file, err := os.OpenFile(filepath.Join(options.ScratchDirectory, name), os.O_RDWR, 0)
		if err != nil {
			return err
		}
		var value [1]byte
		_, readErr := file.ReadAt(value[:], 8)
		value[0] ^= 1
		_, writeErr := file.WriteAt(value[:], 8)
		return errors.Join(readErr, writeErr, file.Close())
	}})
	assertAttemptCutV2SealTestFailure(t, cut, result, err, "descriptor")
	if !changed || objects.writes == 0 || objects.reads != 0 {
		t.Fatal("same-inode descriptor mutation did not reach actual writer replay")
	}
}

// The private spool refuses an oversized write before changing its exact
// file bytes, and rejects unbounded/relative seek coordinates independently.
func TestAttemptCutV2SealSpoolEnforcesExactByteBound(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bounded-spool")
	scratch, err := newAttemptCutV2SealScratch(context.Background(), path, attemptCutV2SealHooks{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = scratch.close() })
	spool, err := scratch.openSpool(0, 1)
	if err != nil {
		t.Fatal(err)
	}
	data := bytes.Repeat([]byte{0x51}, int(attemptStreamV2DescriptorBytes))
	if n, err := spool.Write(data); err != nil || n != len(data) {
		t.Fatalf("exact bounded descriptor write=%d: %v", n, err)
	}
	if n, err := spool.Write([]byte{1}); err == nil || n != 0 {
		t.Fatalf("oversized descriptor write=%d: %v", n, err)
	}
	for _, seek := range []struct {
		offset int64
		whence int
	}{
		{offset: -1, whence: io.SeekStart},
		{offset: 105, whence: io.SeekStart},
		{offset: 1, whence: io.SeekEnd},
		{offset: 0, whence: io.SeekCurrent},
	} {
		if _, err := spool.Seek(seek.offset, seek.whence); err == nil {
			t.Fatalf("unbounded descriptor seek accepted: %+v", seek)
		}
	}
	actual, err := os.ReadFile(filepath.Join(path, "records.descriptors"))
	if err != nil || !bytes.Equal(actual, data) {
		t.Fatalf("refused spool write changed exact bytes: %v", err)
	}
	if err := scratch.close(); err != nil {
		t.Fatal(err)
	}
}
