// A private one-shot journal fences local submission attempts across restart.
// It is independent from action/custody state and never creates a new allowance
// from a missing file. Global custody exclusion remains a separate authority.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

const rootSubmissionStoreLimit = 1024 * 1024

// Operations are serialized by the submission owner; close follows joined use.
type rootSubmissionStore struct {
	config rootSubmissionConfig
	lock   *os.File
}

// Explicit creation is single-use, with an immutable approved-config marker.
// Reopen requires complete state, original approval and an exclusive process lock.
func openRootSubmissionStore(config rootSubmissionConfig, create bool) (*rootSubmissionStore, error) {
	if err := config.validate(); err != nil {
		return nil, err
	}
	path := config.Approval.StatePath
	directory := filepath.Dir(path)
	resolved, err := filepath.EvalSymlinks(directory)
	if err != nil || resolved != directory {
		return nil, errors.Join(errors.New("root submission directory cannot traverse symlinks"), err)
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.Join(errors.New("root submission needs a precreated private directory"), err)
	}
	flags := syscall.O_RDWR | syscall.O_CLOEXEC | syscall.O_NOFOLLOW | syscall.O_NONBLOCK
	if create {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			return nil, errors.Join(errors.New("root submission state exists or cannot be inspected"), err)
		}
		flags |= syscall.O_CREAT | syscall.O_EXCL
	}
	fd, err := syscall.Open(path+".lock", flags, 0600)
	if err != nil {
		return nil, fmt.Errorf("open root submission marker: %w", err)
	}
	lock := os.NewFile(uintptr(fd), path+".lock")
	store := &rootSubmissionStore{config: copyRootSubmissionConfig(config), lock: lock}
	success := false
	defer func() {
		if !success {
			lock.Close()
		}
	}()
	info, err = lock.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.Join(errors.New("root submission marker must be a private regular file"), err)
	}
	if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil, fmt.Errorf("root submission already has an owner: %w", err)
	}
	configHash := rootObjectHash(config)
	if create {
		_, writeErr := lock.WriteString(configHash + "\n")
		if err := errors.Join(writeErr, lock.Sync()); err != nil {
			return nil, err
		}
		record := rootSubmissionRecord{Schema: rootSubmissionStateSchema, Config: copyRootSubmissionConfig(config), Attempts: []rootSubmissionAttempt{}}
		record.ContentHash = rootObjectHash(record)
		if err := store.save(record); err != nil {
			return nil, err
		}
	} else {
		raw, err := io.ReadAll(io.LimitReader(lock, 73))
		if err != nil || string(raw) != configHash+"\n" {
			return nil, errors.Join(errors.New("root submission marker belongs to another config or is incomplete"), err)
		}
		if _, err := store.load(); err != nil {
			return nil, err
		}
	}
	success = true
	return store, nil
}

// Closing releases only the process lock; lifetime evidence is never removed.
func (self *rootSubmissionStore) close() error {
	if self == nil || self.lock == nil {
		return nil
	}
	err := self.lock.Close()
	self.lock = nil
	return err
}

// Strict bounded decoding rejects missing, replaced, duplicate or unknown data.
func (self *rootSubmissionStore) load() (rootSubmissionRecord, error) {
	var record rootSubmissionRecord
	if self.lock == nil {
		return record, errors.New("root submission store is closed")
	}
	path := self.config.Approval.StatePath
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return record, fmt.Errorf("retained root submission state is unavailable: %w", err)
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		file.Close()
		return record, errors.Join(errors.New("root submission state is not a private regular file"), err)
	}
	raw, readErr := io.ReadAll(io.LimitReader(file, rootSubmissionStoreLimit+1))
	if err := errors.Join(readErr, file.Close()); err != nil {
		return record, err
	}
	if len(raw) == 0 || len(raw) > rootSubmissionStoreLimit {
		return record, errors.New("root submission state is empty or exceeds its bound")
	}
	if err := decodePlanJson(raw, &record); err != nil {
		return record, err
	}
	return record, record.validate(self.config)
}

// An atomic synced replacement publishes bytes and attempts together. Failure
// after rename is ambiguous and forces the submission owner to reopen.
func (self *rootSubmissionStore) save(record rootSubmissionRecord) error {
	if self.lock == nil {
		return errors.New("root submission store is closed")
	}
	if err := record.validate(self.config); err != nil {
		return err
	}
	path := self.config.Approval.StatePath
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return errors.New("root submission target is not a private regular file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	raw, err := json.Marshal(record)
	if err != nil || len(raw)+1 > rootSubmissionStoreLimit {
		return errors.Join(errors.New("root submission state exceeds its encoded bound"), err)
	}
	directory := filepath.Dir(path)
	file, err := os.CreateTemp(directory, ".root-submission-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, writeErr := file.Write(append(raw, '\n'))
	if err := errors.Join(writeErr, file.Sync(), file.Close()); err != nil {
		return err
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return err
	}
	dir, err := os.Open(directory)
	if err != nil {
		return err
	}
	return errors.Join(dir.Sync(), dir.Close())
}
