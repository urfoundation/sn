// The offline custody journal durably binds a separately provisioned trust and
// one approved packet before it can be exported. This local ownership marker is
// not a distributed hotkey fence or protection against hostile-host rollback.
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

const rootOfflineStoreLimit = 512 * 1024

// The custody owner serializes load/save. The caller joins all operations before
// close; the process lock stays held throughout that owner's lifetime.
type rootOfflineCustodyStore struct {
	storage    *mainnetDurableDirectory
	trust      rootOfflineCustodyTrust
	packetHash string
	lock       *os.File
}

// Creation is explicit and one-shot. A lost or partially created record leaves
// its marker reserved; neither recovery nor another create may invent a request.
func openRootOfflineCustodyStore(trust rootOfflineCustodyTrust, create *rootOfflineCustodyPacket, storageContexts ...context.Context) (*rootOfflineCustodyStore, error) {
	if err := trust.validate(); err != nil {
		return nil, err
	}
	path := trust.StatePath
	directory := filepath.Dir(path)
	resolved, err := filepath.EvalSymlinks(directory)
	if err != nil || resolved != directory {
		return nil, errors.Join(errors.New("root offline custody directory cannot traverse symlinks"), err)
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.Join(errors.New("root offline custody requires a precreated private directory"), err)
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
		if err := create.validate(trust); err != nil {
			return nil, err
		}
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			return nil, errors.Join(errors.New("root offline custody state already exists or cannot be inspected"), err)
		}
	}
	lock, err := storage.openSnapshotMarker(path)
	if err != nil {
		return nil, fmt.Errorf("open root offline custody ownership marker: %w", err)
	}
	fd := int(lock.Fd())
	store := &rootOfflineCustodyStore{storage: storage, trust: trust, lock: lock}
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
		return nil, errors.Join(errors.New("root offline custody marker is not a private regular file"), err)
	}
	if err := mainnetDurableFlock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil, fmt.Errorf("root offline custody already has an owner: %w", err)
	}
	trustHash := rootObjectHash(trust)
	if err := storage.bindSnapshot(path, "mainnet-root-offline", rootOfflineStoreLimit, create != nil); err != nil {
		return nil, err
	}
	if create != nil {
		store.packetHash = create.ContentHash
		_, writeErr := storage.writeMarkerAt([]byte(trustHash+"\n"+store.packetHash+"\n"), 0)
		if err := errors.Join(writeErr, lock.Sync()); err != nil {
			return nil, err
		}
		record := rootOfflineCustodyRecord{Schema: rootOfflineStateSchema, Packet: *create, Phase: "requested"}
		record.ContentHash = rootObjectHash(record)
		if err := store.save(record); err != nil {
			return nil, err
		}
	} else {
		raw, err := io.ReadAll(io.LimitReader(lock, 145))
		if err != nil || len(raw) != 144 || raw[71] != '\n' || raw[143] != '\n' || string(raw[:71]) != trustHash || !planSha256(string(raw[72:143])) {
			return nil, errors.Join(errors.New("root offline custody marker is incomplete or belongs to another trust"), err)
		}
		store.packetHash = string(raw[72:143])
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

// Closing releases ownership while preserving both original immutable hashes.
func (self *rootOfflineCustodyStore) close() error {
	if self == nil || self.lock == nil {
		return nil
	}
	err := errors.Join(self.storage.close(), self.lock.Close())
	self.lock = nil
	return err
}

// Missing, truncated or malformed state cannot mean that no signature exists.
// Only a complete bounded private file matching the original packet is loaded.
func (self *rootOfflineCustodyStore) load() (rootOfflineCustodyRecord, error) {
	var record rootOfflineCustodyRecord
	if self.lock == nil {
		return record, errors.New("root offline custody store is closed")
	}
	raw, _, err := self.storage.readFile(context.Background(), self.trust.StatePath, rootOfflineStoreLimit)
	if err != nil {
		return record, err
	}
	if err := decodePlanJson(raw, &record); err != nil {
		return record, err
	}
	if record.Packet.ContentHash != self.packetHash {
		return record, errors.New("root offline custody packet differs from the original ownership marker")
	}
	return record, record.validate(self.trust)
}

// Syncing the file and directory precedes receipt publication. A failure after
// rename is ambiguous; the owner must reopen rather than retry this instance.
func (self *rootOfflineCustodyStore) save(record rootOfflineCustodyRecord) error {
	if err := self.storage.checkWrite(nil); err != nil {
		return err
	}
	if self.lock == nil || record.Packet.ContentHash != self.packetHash {
		return errors.New("root offline custody store ownership differs")
	}
	if err := record.validate(self.trust); err != nil {
		return err
	}
	if info, err := os.Lstat(self.trust.StatePath); err == nil {
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return errors.New("root offline custody target is not a private regular file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	raw, err := json.Marshal(record)
	if err != nil || len(raw)+1 > rootOfflineStoreLimit {
		return errors.Join(errors.New("root offline custody state cannot be encoded within its resource bound"), err)
	}
	return self.storage.publish(self.trust.StatePath, append(raw, '\n'), nil)
}
