// Domain publication uses one protected directory descriptor, exclusive links
// and exact retained-file readback. It cannot replace another launch's evidence.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// A partial export is resumable by exact bytes. The original preparation and
// provider state directories are never opened for writing by this exporter.
func publishBootstrapProviderDomains(ctx context.Context, path string, launches []bootstrapProviderRoleLaunch) error {
	if err := errors.Join(ctx.Err(), bootstrapRootDirectory(path)); err != nil {
		return err
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	directory := os.NewFile(uintptr(fd), path)
	defer directory.Close()
	original, err := directory.Stat()
	if err != nil || !original.IsDir() || original.Mode().Perm()&0077 != 0 {
		return errors.Join(errors.New("provider domain output directory is not private"), err)
	}
	checkDirectory := func() error {
		named, err := os.Lstat(path)
		if err != nil || !os.SameFile(original, named) || named.Mode() != original.Mode() {
			return errors.Join(errors.New("provider domain output directory identity changed"), err)
		}
		return ctx.Err()
	}
	for _, launch := range launches {
		if err := checkDirectory(); err != nil {
			return err
		}
		raw, err := json.Marshal(launch.Domain)
		if err != nil {
			return err
		}
		name := filepath.Base(launch.DomainFile.Path)
		if launch.DomainFile.Path != filepath.Join(path, name) || name == "." || name == ".." {
			return errors.New("provider domain output escaped its declared directory")
		}
		if err := readBootstrapProviderDomainAt(fd, name, raw); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		var nonce [16]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			return err
		}
		temporary := ".provider-domain-" + hex.EncodeToString(nonce[:])
		err = func() error {
			tempFd, err := unix.Openat(fd, temporary, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
			if err != nil {
				return err
			}
			file := os.NewFile(uintptr(tempFd), temporary)
			defer file.Close()
			defer unix.Unlinkat(fd, temporary, 0)
			if n, err := file.Write(raw); err != nil || n != len(raw) {
				return errors.Join(io.ErrShortWrite, err)
			}
			if err := errors.Join(file.Sync(), ctx.Err()); err != nil {
				return err
			}
			if err := unix.Linkat(fd, temporary, fd, name, 0); err != nil && !errors.Is(err, os.ErrExist) {
				return err
			}
			if err := unix.Unlinkat(fd, temporary, 0); err != nil {
				return err
			}
			return errors.Join(directory.Sync(), readBootstrapProviderDomainAt(fd, name, raw))
		}()
		if err != nil {
			return err
		}
	}
	return checkDirectory()
}

// Nonblocking open rejects a FIFO or device before reading. Public bytes need
// no secret-mode assumption, but retained output must have one stable inode.
func readBootstrapProviderDomainAt(directory int, name string, expected []byte) error {
	fd, err := unix.Openat(directory, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), name)
	defer file.Close()
	before, err := file.Stat()
	if err != nil || !before.Mode().IsRegular() || before.Size() != int64(len(expected)) {
		return errors.Join(errors.New("provider domain retained file is not the exact bounded regular source"), err)
	}
	raw, err := io.ReadAll(io.LimitReader(file, int64(len(expected))+1))
	after, statErr := file.Stat()
	var named unix.Stat_t
	namedErr := unix.Fstatat(directory, name, &named, unix.AT_SYMLINK_NOFOLLOW)
	var owned unix.Stat_t
	ownedErr := unix.Fstat(fd, &owned)
	if err != nil || statErr != nil || namedErr != nil || ownedErr != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) ||
		named.Dev != owned.Dev || named.Ino != owned.Ino || named.Mode != owned.Mode || named.Nlink != 1 || !bytes.Equal(raw, expected) {
		return errors.Join(errors.New("provider domain retained bytes or identity differ"), err, statErr, namedErr, ownedErr)
	}
	return nil
}
