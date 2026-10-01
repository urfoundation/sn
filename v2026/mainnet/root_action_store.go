// The root action store is private and single-owner, with an explicit creation
// marker. Missing or empty retained state is never interpreted as a new spend
// allowance. Checksums detect corruption; custody must prevent rollback/replay
// of an older but internally consistent local directory.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"github.com/urfoundation/sn/v2026/protocol"
)

const rootActionStoreLimit = 256 * 1024

// A lock remains held from open through close. Methods are serial and must not
// run concurrently; storage errors force the action owner to close and reopen.
type rootActionStore struct {
	path        string
	requestHash string
	lock        *os.File
}

// Creates exactly one action, or opens that same action for recovery. Creation
// cannot reuse a prior marker even if its state file was lost or truncated.
func openRootActionStore(path string, create *rootAction) (*rootActionStore, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" {
		return nil, errors.New("root action store requires a canonical absolute path")
	}
	directory := filepath.Dir(path)
	resolved, err := filepath.EvalSymlinks(directory)
	if err != nil || resolved != directory {
		return nil, errors.Join(errors.New("root action store directory cannot traverse symlinks"), err)
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.Join(errors.New("root action store requires a precreated private directory"), err)
	}
	flags := syscall.O_RDWR | syscall.O_CLOEXEC | syscall.O_NOFOLLOW
	if create != nil {
		if err := create.validate(); err != nil {
			return nil, err
		}
		if create.Scope.StatePath != path {
			return nil, errors.New("root action state path differs from approval")
		}
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			return nil, errors.Join(errors.New("root action state already exists or cannot be inspected"), err)
		}
		flags |= syscall.O_CREAT | syscall.O_EXCL
	}
	fd, err := syscall.Open(path+".lock", flags, 0600)
	if err != nil {
		return nil, fmt.Errorf("open root action ownership marker: %w", err)
	}
	lock := os.NewFile(uintptr(fd), path+".lock")
	store := &rootActionStore{path: path, lock: lock}
	success := false
	defer func() {
		if !success {
			lock.Close()
		}
	}()
	info, err = lock.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.Join(errors.New("root ownership marker is not a private regular file"), err)
	}
	if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil, fmt.Errorf("root action already has an owner: %w", err)
	}
	if create != nil {
		store.requestHash = create.RequestHash
		_, err := lock.WriteString(create.RequestHash + "\n")
		if err := errors.Join(err, lock.Sync()); err != nil {
			return nil, err
		}
		record := rootActionRecord{Schema: rootActionStateSchema, Action: *create, Phase: "reserved"}
		record.ContentHash = rootObjectHash(record)
		if err := store.save(record); err != nil {
			return nil, err
		}
	} else {
		raw, err := io.ReadAll(io.LimitReader(lock, 73))
		if err != nil || len(raw) != 72 || raw[71] != '\n' {
			return nil, errors.Join(errors.New("root ownership marker is missing or incomplete"), err)
		}
		store.requestHash = string(raw[:71])
		if _, err := store.load(); err != nil {
			return nil, err
		}
	}
	success = true
	return store, nil
}

// Releases the local lock without removing the durable ownership marker.
func (self *rootActionStore) close() error {
	if self == nil || self.lock == nil {
		return nil
	}
	err := self.lock.Close()
	self.lock = nil
	return err
}

// Strict private-file loading rejects truncated, duplicate-key or rehashed
// replacement actions. The immutable ownership marker binds the original hash.
func (self *rootActionStore) load() (rootActionRecord, error) {
	var record rootActionRecord
	if self.lock == nil {
		return record, errors.New("root action store is closed")
	}
	fd, err := syscall.Open(self.path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return record, fmt.Errorf("retained root action state is unavailable; refusing new allowance: %w", err)
	}
	file := os.NewFile(uintptr(fd), self.path)
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		file.Close()
		return record, errors.Join(errors.New("root action state is not a private regular file"), err)
	}
	raw, readErr := io.ReadAll(io.LimitReader(file, rootActionStoreLimit+1))
	if err := errors.Join(readErr, file.Close()); err != nil {
		return record, err
	}
	if len(raw) == 0 || len(raw) > rootActionStoreLimit {
		return record, errors.New("root action state is empty or exceeds its resource bound")
	}
	if err := protocol.ValidateUniqueJsonKeys(raw); err != nil {
		return record, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&record); err != nil {
		return record, err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return record, errors.New("root action state has trailing JSON")
	}
	if record.Action.RequestHash != self.requestHash || record.Action.Scope.StatePath != self.path {
		return record, errors.New("root action differs from the original ownership marker or approved state path")
	}
	return record, record.validate()
}

// Fully synced replacement leaves either complete record readable after a
// crash. A sync error after rename is ambiguous and poisons the owner above.
func (self *rootActionStore) save(record rootActionRecord) error {
	if self.lock == nil || record.Action.RequestHash != self.requestHash || record.Action.Scope.StatePath != self.path {
		return errors.New("root action store ownership differs")
	}
	if err := record.validate(); err != nil {
		return err
	}
	if info, err := os.Lstat(self.path); err == nil {
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return errors.New("root action target is not a private regular file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	raw, err := json.Marshal(record)
	if err != nil || len(raw) > rootActionStoreLimit {
		return errors.Join(errors.New("root action state cannot be encoded within its resource bound"), err)
	}
	directory := filepath.Dir(self.path)
	file, err := os.CreateTemp(directory, ".root-action-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, writeErr := file.Write(append(raw, '\n'))
	if err := errors.Join(writeErr, file.Sync(), file.Close()); err != nil {
		return err
	}
	if err := os.Rename(file.Name(), self.path); err != nil {
		return err
	}
	dir, err := os.Open(directory)
	if err != nil {
		return err
	}
	return errors.Join(dir.Sync(), dir.Close())
}
