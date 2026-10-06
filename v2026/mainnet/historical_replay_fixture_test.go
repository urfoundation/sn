// Test-engine custody preserves retained exporter paths and owns temporary copies.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

// A protected selected image remains the exact exporter authority. A temporary
// go test image with writable permissions or extra links gets a separate inode;
// the original executable is never modified. Only bounded regular sources copy.
func historicalReplayFixtureEngine(path, directory string) (reference planFileReference, resultErr error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return planFileReference{}, err
	}
	source := os.NewFile(uintptr(fd), path)
	var copied *os.File
	defer func() {
		resultErr = errors.Join(resultErr, source.Close())
		if copied != nil {
			resultErr = errors.Join(resultErr, copied.Close())
		}
		if resultErr != nil {
			reference = planFileReference{}
		}
	}()
	before, err := source.Stat()
	if err != nil {
		return planFileReference{}, err
	}
	stat, ok := before.Sys().(*syscall.Stat_t)
	if !ok || !before.Mode().IsRegular() || before.Size() < 4 || before.Size() > historicalReplayEngineLimit {
		return planFileReference{}, errors.New("fixture engine requires a bounded regular source")
	}
	hash := sha256.New()
	var output io.Writer = hash
	reference.Path = path
	if before.Mode().Perm()&0111 == 0 || before.Mode().Perm()&0022 != 0 || before.Mode()&(os.ModeSetuid|os.ModeSetgid) != 0 || stat.Nlink != 1 {
		reference.Path = filepath.Join(directory, "fixture-engine")
		copied, err = os.OpenFile(reference.Path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return planFileReference{}, err
		}
		output = io.MultiWriter(copied, hash)
	}
	total, err := io.Copy(output, io.LimitReader(source, historicalReplayEngineLimit+1))
	if err != nil {
		return planFileReference{}, err
	}
	after, err := source.Stat()
	if err != nil {
		return planFileReference{}, err
	}
	final, ok := after.Sys().(*syscall.Stat_t)
	if !ok || stat.Dev != final.Dev || stat.Ino != final.Ino || stat.Mode != final.Mode || stat.Nlink != final.Nlink || stat.Mtim != final.Mtim || stat.Ctim != final.Ctim || total != before.Size() || after.Size() != total {
		return planFileReference{}, errors.New("fixture engine changed while retaining")
	}
	if copied != nil {
		if err := copied.Chmod(0500); err != nil {
			return planFileReference{}, err
		}
		info, err := copied.Stat()
		if err != nil {
			return planFileReference{}, err
		}
		owned, ok := info.Sys().(*syscall.Stat_t)
		if !ok || !info.Mode().IsRegular() || info.Mode().Perm() != 0500 || owned.Nlink != 1 || info.Size() != total || os.SameFile(before, info) {
			return planFileReference{}, errors.New("fixture engine copy is not privately owned")
		}
	}
	reference.Sha256 = "sha256:" + hex.EncodeToString(hash.Sum(nil))
	return reference, nil
}

// Exporting through a retained protected image keeps its exact path and bytes.
func TestHistoricalReplayFixturePreservesProtectedEngine(t *testing.T) {
	for _, mode := range []os.FileMode{0500, 0755} {
		path := filepath.Join(t.TempDir(), "selected-engine")
		raw := []byte("\x7fELFprotected fixture")
		if err := os.WriteFile(path, raw, mode); err != nil {
			t.Fatal(err)
		}
		before, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		directory := t.TempDir()
		reference, err := historicalReplayFixtureEngine(path, directory)
		if err != nil || reference.Path != path || reference.Sha256 != monitorReadDigest(raw) {
			t.Fatal("protected selected exporter engine was replaced", reference, err)
		}
		after, err := os.Stat(path)
		if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || before.Size() != after.Size() {
			t.Fatal("protected selected engine changed", err)
		}
		initial, final := before.Sys().(*syscall.Stat_t), after.Sys().(*syscall.Stat_t)
		if initial.Nlink != final.Nlink || initial.Mtim != final.Mtim || initial.Ctim != final.Ctim {
			t.Fatal("protected selected engine custody metadata changed")
		}
		entries, err := os.ReadDir(directory)
		if err != nil || len(entries) != 0 {
			t.Fatal("protected selected engine created an unnecessary copy", err)
		}
	}
}

// Every inadmissible permission/link field copies without altering the source.
func TestHistoricalReplayFixtureOwnsTemporaryEngine(t *testing.T) {
	for _, c := range []struct {
		name string
		mode os.FileMode
		link bool
	}{
		{name: "not-executable", mode: 0600},
		{name: "group-writable", mode: 0770},
		{name: "world-writable", mode: 0702},
		{name: "setuid", mode: 0700 | os.ModeSetuid},
		{name: "setgid", mode: 0700 | os.ModeSetgid},
		{name: "cache-hardlink", mode: 0500, link: true},
	} {
		root := t.TempDir()
		path := filepath.Join(root, "temporary-engine")
		raw := []byte("\x7fELFtemporary fixture")
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, c.mode); err != nil {
			t.Fatal(err)
		}
		if c.link {
			if err := os.Link(path, filepath.Join(root, "cache-link")); err != nil {
				t.Fatal(err)
			}
		}
		before, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		reference, err := historicalReplayFixtureEngine(path, t.TempDir())
		if err != nil {
			t.Fatal(c.name, err)
		}
		owned, err := os.Stat(reference.Path)
		if err != nil {
			t.Fatal(err)
		}
		stat, ok := owned.Sys().(*syscall.Stat_t)
		if !ok || reference.Path == path || os.SameFile(before, owned) || owned.Mode().Perm() != 0500 || stat.Nlink != 1 || reference.Sha256 != monitorReadDigest(raw) {
			t.Fatal("temporary engine did not acquire private custody", c.name, reference)
		}
		after, err := os.Stat(path)
		if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || before.Size() != after.Size() {
			t.Fatal("fixture changed its original temporary engine", c.name, err)
		}
		initial, final := before.Sys().(*syscall.Stat_t), after.Sys().(*syscall.Stat_t)
		if initial.Nlink != final.Nlink || initial.Mtim != final.Mtim || initial.Ctim != final.Ctim {
			t.Fatal("fixture changed its original custody metadata", c.name)
		}
		retained, err := os.ReadFile(reference.Path)
		if err != nil || !bytes.Equal(raw, retained) {
			t.Fatal("private engine copy changed executable bytes", c.name, err)
		}
	}
}

// Unsafe source types and sizes fail before any output file is allocated.
func TestHistoricalReplayFixtureRejectsUnboundedOrNonregularEngine(t *testing.T) {
	root := t.TempDir()
	short := filepath.Join(root, "short")
	large := filepath.Join(root, "large")
	link := filepath.Join(root, "symlink")
	if err := os.WriteFile(short, []byte("ELF"), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(large, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(file.Truncate(historicalReplayEngineLimit+1), file.Close()); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(short, link); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{short, large, root, link} {
		directory := t.TempDir()
		if reference, err := historicalReplayFixtureEngine(path, directory); err == nil || reference.Path != "" || reference.Sha256 != "" {
			t.Fatal("unsafe fixture source acquired engine authority", path, reference, err)
		}
		entries, err := os.ReadDir(directory)
		if err != nil || len(entries) != 0 {
			t.Fatal("unsafe fixture source allocated output before admission", path, err)
		}
	}
}
