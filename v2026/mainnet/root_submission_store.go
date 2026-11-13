// A private one-shot journal fences local submission attempts across restart.
// It is independent from action/custody state and never creates a new allowance
// from a missing file. Global custody exclusion remains a separate authority.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

const rootSubmissionStoreLimit = 1024 * 1024

// Operations are serialized by the submission owner; close follows joined use.
type rootSubmissionStore struct {
	storage *mainnetDurableDirectory
	config  rootSubmissionConfig
	lock    *os.File
}

// Explicit creation is single-use, with an immutable approved-config marker.
// Reopen requires complete state, original approval and an exclusive process lock.
func openRootSubmissionStore(config rootSubmissionConfig, create bool, storageContexts ...context.Context) (*rootSubmissionStore, error) {
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
	storage, err := openMainnetDurableDirectory(mainnetStorageContext(storageContexts), filepath.Dir(path), durablevolume.ReadWrite)
	if err != nil {
		return nil, err
	}
	transferred := false
	defer func() {
		if !transferred {
			_ = storage.close()
		}
	}()
	if create {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			return nil, errors.Join(errors.New("root submission state exists or cannot be inspected"), err)
		}
	}
	lock, err := storage.openSnapshotMarker(path)
	if err != nil {
		return nil, fmt.Errorf("open root submission marker: %w", err)
	}
	fd := int(lock.Fd())
	store := &rootSubmissionStore{storage: storage, config: copyRootSubmissionConfig(config), lock: lock}
	transferred = true
	success := false
	defer func() {
		if !success {
			store.close()
		}
	}()
	if err := storage.bindMarker(lock, false); err != nil {
		return nil, err
	}
	info, err = lock.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.Join(errors.New("root submission marker must be a private regular file"), err)
	}
	if err := mainnetDurableFlock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil, fmt.Errorf("root submission already has an owner: %w", err)
	}
	configHash := rootObjectHash(config)
	if err := storage.bindSnapshot(path, "mainnet-root-submission", rootSubmissionStoreLimit, create); err != nil {
		return nil, err
	}
	if create {
		_, writeErr := storage.writeMarkerAt([]byte(configHash+"\n"), 0)
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
	if err := storage.bindMarker(store.lock, true); err != nil {
		return nil, err
	}
	success = true
	return store, nil
}

// Closing releases only the process lock; lifetime evidence is never removed.
func (self *rootSubmissionStore) close() error {
	if self == nil || self.lock == nil {
		return nil
	}
	err := errors.Join(self.storage.close(), self.lock.Close())
	self.lock = nil
	return err
}

// Strict bounded decoding rejects missing, replaced, duplicate or unknown data.
func (self *rootSubmissionStore) load() (rootSubmissionRecord, error) {
	var record rootSubmissionRecord
	if self.lock == nil {
		return record, errors.New("root submission store is closed")
	}
	raw, _, err := self.storage.readFile(context.Background(), self.config.Approval.StatePath, rootSubmissionStoreLimit)
	if err != nil {
		return record, err
	}
	if err := decodePlanJson(raw, &record); err != nil {
		return record, err
	}
	return record, record.validate(self.config)
}

// An atomic synced replacement publishes bytes and attempts together. Failure
// after rename is ambiguous and forces the submission owner to reopen.
func (self *rootSubmissionStore) save(record rootSubmissionRecord) error {
	if err := self.storage.checkWrite(nil); err != nil {
		return err
	}
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
	return self.storage.publish(self.config.Approval.StatePath, append(raw, '\n'), nil)
}
