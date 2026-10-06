//go:build linux

package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"golang.org/x/sys/unix"
)

// Existing fixture composition can share an already prepared marker. This is
// explicit test enrollment before an owner opens; it never runs on a restart.
func prepareMainnetSnapshotTest(t *testing.T, path, kind string, maximum int) {
	t.Helper()
	if _, err := os.Lstat(path + ".lock"); err == nil {
		return
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	name := filepath.Base(path)
	durablefixture.ProvisionSnapshot(t, filepath.Dir(path), kind, name, int64(maximum), name+".lock", map[string][]byte{name + ".lock": nil})
}

// Read-only operations must preserve the explicitly prepared namespace,
// including marker identity and checkpoint attributes, not just payload bytes.
type mainnetNamespaceFileTest struct {
	Device     uint64
	Inode      uint64
	Mode       os.FileMode
	Data       string
	Attributes map[string]string
}

func mainnetNamespaceTest(t *testing.T, directory string) map[string]mainnetNamespaceFileTest {
	t.Helper()
	result := map[string]mainnetNamespaceFileTest{}
	err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		stat := info.Sys().(*syscall.Stat_t)
		file := mainnetNamespaceFileTest{Device: uint64(stat.Dev), Inode: stat.Ino, Mode: info.Mode(), Attributes: map[string]string{}}
		if info.Mode().IsRegular() {
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			file.Data = string(raw)
		} else if info.Mode()&os.ModeSymlink != 0 {
			file.Data, err = os.Readlink(path)
			if err != nil {
				return err
			}
		}
		names := make([]byte, 64*1024)
		n, err := unix.Llistxattr(path, names)
		if err != nil {
			return err
		}
		for _, name := range strings.Split(string(names[:n]), "\x00") {
			if name == "" {
				continue
			}
			value := make([]byte, 64*1024)
			n, err := unix.Lgetxattr(path, name, value)
			if err != nil {
				return err
			}
			file.Attributes[name] = string(value[:n])
		}
		relative, err := filepath.Rel(directory, path)
		if err != nil {
			return err
		}
		result[relative] = file
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
