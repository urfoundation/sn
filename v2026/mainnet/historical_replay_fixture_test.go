// Test-engine custody preserves retained exporter paths and owns temporary copies.
package main

import (
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
	var stat unix.Stat_t
	statErr := unix.Fstat(int(source.Fd()), &stat)
	if statErr != nil || !before.Mode().IsRegular() || before.Size() < 4 || before.Size() > historicalReplayEngineLimit {
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
	var final unix.Stat_t
	finalErr := unix.Fstat(int(source.Fd()), &final)
	if finalErr != nil || stat.Dev != final.Dev || stat.Ino != final.Ino || stat.Mode != final.Mode || stat.Nlink != final.Nlink || stat.Mtim != final.Mtim || stat.Ctim != final.Ctim || total != before.Size() || after.Size() != total {
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
