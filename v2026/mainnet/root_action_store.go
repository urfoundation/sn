// The root action store is private and single-owner, with an explicit creation
// marker. Missing or empty retained state is never interpreted as a new spend
// allowance. Checksums detect corruption; custody must prevent rollback/replay
// of an older but internally consistent local directory.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/urnetwork/connect/v2026/durablevolume"
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
	storage     *mainnetDurableDirectory
	path        string
	requestHash string
	lock        *os.File
}

// Creates exactly one action, or opens that same action for recovery. Creation
// cannot reuse a prior marker even if its state file was lost or truncated.
func openRootActionStore(path string, create *rootAction, storageContexts ...context.Context) (*rootActionStore, error) {
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
	}
	lock, err := storage.openSnapshotMarker(path)
	if err != nil {
		return nil, fmt.Errorf("open root action ownership marker: %w", err)
	}
	fd := int(lock.Fd())
	store := &rootActionStore{storage: storage, path: path, lock: lock}
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
		return nil, errors.Join(errors.New("root ownership marker is not a private regular file"), err)
	}
	if err := mainnetDurableFlock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil, fmt.Errorf("root action already has an owner: %w", err)
	}
	if err := storage.bindSnapshot(path, "mainnet-root-action", rootActionStoreLimit, create != nil); err != nil {
		return nil, err
	}
	if create != nil {
		store.requestHash = create.RequestHash
		_, err := storage.writeMarkerAt([]byte(create.RequestHash+"\n"), 0)
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
	if err := storage.bindMarker(store.lock, true); err != nil {
		return nil, err
	}
	success = true
	return store, nil
}

// Releases the local lock without removing the durable ownership marker.
func (self *rootActionStore) close() error {
	if self == nil || self.lock == nil {
		return nil
	}
	err := errors.Join(self.storage.close(), self.lock.Close())
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
	raw, _, err := self.storage.readFile(context.Background(), self.path, rootActionStoreLimit)
	if err != nil {
		return record, err
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
	if err := self.storage.checkWrite(nil); err != nil {
		return err
	}
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
	return self.storage.publish(self.path, append(raw, '\n'), nil)
}
