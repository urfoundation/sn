// The composite service journal retains decision, native intent and canonical
// outcome together. A one-shot private marker binds independently supplied
// configuration; local persistence does not replace a distributed custody fence.
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

const rootServiceStoreLimit = 1024 * 1024

// Load/save are serialized by the service. Close follows joined operations and
// releases only the process lock, retaining all intent and allowance evidence.
type rootServiceStore struct {
	config rootServiceConfig
	lock   *os.File
}

// Explicit creation is allowed once at the approved action state path. Reopen
// requires the original marker and complete state; missing files never mean new.
func openRootServiceStore(config rootServiceConfig, create bool) (*rootServiceStore, error) {
	if err := config.validate(); err != nil {
		return nil, err
	}
	path := config.Packet.Action.Scope.StatePath
	directory := filepath.Dir(path)
	resolved, err := filepath.EvalSymlinks(directory)
	if err != nil || resolved != directory {
		return nil, errors.Join(errors.New("root service directory cannot traverse symlinks"), err)
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.Join(errors.New("root service requires a precreated private directory"), err)
	}
	flags := syscall.O_RDWR | syscall.O_CLOEXEC | syscall.O_NOFOLLOW | syscall.O_NONBLOCK
	if create {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			return nil, errors.Join(errors.New("root service state already exists or cannot be inspected"), err)
		}
		flags |= syscall.O_CREAT | syscall.O_EXCL
	}
	fd, err := syscall.Open(path+".lock", flags, 0600)
	if err != nil {
		return nil, fmt.Errorf("open root service ownership marker: %w", err)
	}
	lock := os.NewFile(uintptr(fd), path+".lock")
	store := &rootServiceStore{config: copyRootServiceConfig(config), lock: lock}
	success := false
	defer func() {
		if !success {
			lock.Close()
		}
	}()
	info, err = lock.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.Join(errors.New("root service marker is not a private regular file"), err)
	}
	if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil, fmt.Errorf("root service already has an owner: %w", err)
	}
	configHash := rootObjectHash(config)
	if create {
		_, writeErr := lock.WriteString(configHash + "\n")
		if err := errors.Join(writeErr, lock.Sync()); err != nil {
			return nil, err
		}
		action := rootActionRecord{Schema: rootActionStateSchema, Action: copyRootAction(config.Packet.Action), Phase: "reserved"}
		action.ContentHash = rootObjectHash(action)
		record := rootServiceRecord{Schema: rootServiceStateSchema, Config: copyRootServiceConfig(config), Phase: "observing", Action: action}
		record.ContentHash = rootObjectHash(record)
		if err := store.save(record); err != nil {
			return nil, err
		}
	} else {
		raw, err := io.ReadAll(io.LimitReader(lock, 73))
		if err != nil || string(raw) != configHash+"\n" {
			return nil, errors.Join(errors.New("root service marker is incomplete or belongs to another configuration"), err)
		}
		if _, err := store.load(); err != nil {
			return nil, err
		}
	}
	success = true
	return store, nil
}

// A closed owner never removes its lifetime request or observation allowance.
func (self *rootServiceStore) close() error {
	if self == nil || self.lock == nil {
		return nil
	}
	err := self.lock.Close()
	self.lock = nil
	return err
}

// Strict bounded json and independent config prevent state from approving its
// own replacement key, request, target basket or replenished observation budget.
func (self *rootServiceStore) load() (rootServiceRecord, error) {
	var record rootServiceRecord
	if self.lock == nil {
		return record, errors.New("root service store is closed")
	}
	path := self.config.Packet.Action.Scope.StatePath
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return record, fmt.Errorf("retained root service state is unavailable: %w", err)
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		file.Close()
		return record, errors.Join(errors.New("root service state is not a private regular file"), err)
	}
	raw, readErr := io.ReadAll(io.LimitReader(file, rootServiceStoreLimit+1))
	if err := errors.Join(readErr, file.Close()); err != nil {
		return record, err
	}
	if len(raw) == 0 || len(raw) > rootServiceStoreLimit {
		return record, errors.New("root service state is empty or exceeds its resource bound")
	}
	if err := decodePlanJson(raw, &record); err != nil {
		return record, err
	}
	return record, record.validate(self.config)
}

// Atomic replacement syncs the complete composite intent before publication.
// A post-rename error is ambiguous and poisons the owner until a fresh reopen.
func (self *rootServiceStore) save(record rootServiceRecord) error {
	if self.lock == nil {
		return errors.New("root service store is closed")
	}
	if err := record.validate(self.config); err != nil {
		return err
	}
	path := self.config.Packet.Action.Scope.StatePath
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return errors.New("root service target is not a private regular file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	raw, err := json.Marshal(record)
	if err != nil || len(raw)+1 > rootServiceStoreLimit {
		return errors.Join(errors.New("root service state cannot be encoded within its resource bound"), err)
	}
	directory := filepath.Dir(path)
	file, err := os.CreateTemp(directory, ".root-service-*")
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
