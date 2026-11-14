// Synthetic test enrollment is explicit and excluded from runtime admission.
// It binds any deliberately retained format fixture before the command starts.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/urfoundation/sn/v2026/internal/durablehead"
)

// Existing attributes are never overwritten, including negative fixtures.
func provisionMonitorTestCustody(t testing.TB, path string) {
	t.Helper()
	provisionMonitorTestCustodyProfile(t, path, "mainnet-monitor-checkpoint", maxRpcReplyBytes)
}

func provisionMonitorTestCustodyProfile(t testing.TB, path, kind string, maximum int) {
	t.Helper()
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return
	}
	parent, name := filepath.Dir(path), filepath.Base(path)
	physical, err := filepath.EvalSymlinks(parent)
	if err != nil || physical != parent {
		return
	}
	lockPath := path + ".lock"
	attribute := durablehead.Attribute(kind, name)
	if _, err := unix.Getxattr(lockPath, attribute, nil); err == nil {
		return
	}
	lock, err := os.OpenFile(lockPath, os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return
	}
	defer lock.Close()
	var rootStat, lockStat syscall.Stat_t
	if err := syscall.Stat(parent, &rootStat); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Fstat(int(lock.Fd()), &lockStat); err != nil {
		t.Fatal(err)
	}
	if lockStat.Mode&syscall.S_IFMT != syscall.S_IFREG || lockStat.Mode&0077 != 0 {
		return
	}
	checkpoint := durablehead.Checkpoint{Schema: durablehead.Schema, Kind: kind, Name: name, MaximumBytes: int64(maximum), DirectoryInode: rootStat.Ino, LockName: name + ".lock", Auxiliaries: []durablehead.Auxiliary{{Name: name + ".lock", Inode: lockStat.Ino}}}
	var stat syscall.Stat_t
	err = syscall.Lstat(path, &stat)
	if err == nil {
		if stat.Mode&syscall.S_IFMT != syscall.S_IFREG || stat.Mode&0077 != 0 || stat.Size > int64(maximum) {
			return
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(raw)
		checkpoint.Committed = durablehead.Member{Present: true, Inode: stat.Ino, Size: int64(len(raw)), Sha256: hex.EncodeToString(digest[:])}
	} else if !errors.Is(err, os.ErrNotExist) {
		return
	}
	raw, err := json.Marshal(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.Setxattr(lockPath, attribute, raw, unix.XATTR_CREATE); err != nil {
		t.Fatal(err)
	}
	if err := lock.Sync(); err != nil {
		t.Fatal(err)
	}
	directory, err := os.Open(parent)
	if err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(directory.Sync(), directory.Close()); err != nil {
		t.Fatal(err)
	}
}
