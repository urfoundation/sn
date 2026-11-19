// The composite service journal retains decision, native intent and canonical
// outcome together. A one-shot private marker binds independently supplied
// configuration; local persistence does not replace a distributed custody fence.
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

const rootServiceStoreLimit = 1024 * 1024

// Load/save are serialized by the service. Close follows joined operations and
// releases only the process lock, retaining all intent and allowance evidence.
type rootServiceStore struct {
	storage              *mainnetDurableDirectory
	config               rootServiceConfig
	lock                 *os.File
	syncDirectoryForTest func(*os.File) error
}

// Explicit creation is allowed once at the approved action state path. Reopen
// requires the original marker and complete state; missing files never mean new.
func openRootServiceStore(config rootServiceConfig, create bool, storageContexts ...context.Context) (*rootServiceStore, error) {
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
			return nil, errors.Join(errors.New("root service state already exists or cannot be inspected"), err)
		}
	}
	lock, err := storage.openSnapshotMarker(path)
	if err != nil {
		return nil, fmt.Errorf("open root service ownership marker: %w", err)
	}
	fd := int(lock.Fd())
	store := &rootServiceStore{storage: storage, config: copyRootServiceConfig(config), lock: lock}
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
		return nil, errors.Join(errors.New("root service marker is not a private regular file"), err)
	}
	if err := mainnetDurableFlock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil, fmt.Errorf("root service already has an owner: %w", err)
	}
	configHash := rootObjectHash(config)
	if err := storage.bindSnapshot(path, "mainnet-root-service", rootServiceStoreLimit, create); err != nil {
		return nil, err
	}
	if create {
		_, writeErr := storage.writeMarkerAt([]byte(configHash+"\n"), 0)
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
	if err := storage.bindMarker(store.lock, true); err != nil {
		return nil, err
	}
	success = true
	return store, nil
}

// A closed owner never removes its lifetime request or observation allowance.
func (self *rootServiceStore) close() error {
	if self == nil || self.lock == nil {
		return nil
	}
	err := errors.Join(self.storage.close(), self.lock.Close())
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
	raw, _, err := self.storage.readFile(context.Background(), self.config.Packet.Action.Scope.StatePath, rootServiceStoreLimit)
	if err != nil {
		return record, err
	}
	if err := decodePlanJson(raw, &record); err != nil {
		return record, err
	}
	return record, record.validate(self.config)
}

// Atomic replacement syncs the complete composite intent before publication.
// A post-rename error is ambiguous and poisons the owner until a fresh reopen.
func (self *rootServiceStore) save(record rootServiceRecord) error {
	if err := self.storage.checkWrite(nil); err != nil {
		return err
	}
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
	return self.storage.publish(self.config.Packet.Action.Scope.StatePath, append(raw, '\n'), self.syncDirectoryForTest)
}
