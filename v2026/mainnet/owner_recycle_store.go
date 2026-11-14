// A private exclusive journal marks initial claim completion durably. Missing
// or corrupt completed state is never an unused owner nonce or signing allowance.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

const ownerRecycleStoreLimit = ownerSigningRequestLimit + 1024*1024

// Local locking is not global coldkey custody. The caller serializes methods;
// failed writes poison the store, including failures after an atomic rename.
type ownerRecycleStore struct {
	storage       *mainnetDurableDirectory
	config        ownerRecycleConfig
	key           string
	lock          *os.File
	directory     *os.File
	marker        *bootstrapContractReadinessMarker
	complete      bool
	expectedHash  string
	failed        error
	syncDirectory func(*os.File) error
}

// Reserve uses exclusive creation; resume accepts only the same approved marker.
// No path comes from a portable request on an owner's separate signing computer.
func openOwnerRecycleStore(config ownerRecycleConfig, key string, create bool, storageContexts ...context.Context) (_ *ownerRecycleStore, resultErr error) {
	return openOwnerRecycleStoreWithClaimHook(config, key, create, nil, storageContexts...)
}

// Tests interrupt actual durable claim boundaries before completion. The
// ordinary opener never installs a hook or changes the physical head contract.
func openOwnerRecycleStoreWithClaimHook(config ownerRecycleConfig, key string, create bool, claimHook func(string) error, storageContexts ...context.Context) (_ *ownerRecycleStore, resultErr error) {
	path := config.Action.StatePath
	if err := errors.Join(config.validate(key), bootstrapRootDirectory(filepath.Dir(path))); err != nil {
		return nil, err
	}
	storage, err := openMainnetDurableDirectory(mainnetStorageContext(storageContexts), filepath.Dir(path), durablevolume.ReadWrite)
	if err != nil {
		return nil, err
	}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, storage.close())
		}
	}()
	root, err := bootstrapSuccessorPhysicalRoot(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	directoryFd, err := unix.Open(filepath.Dir(path), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	self := &ownerRecycleStore{storage: storage, config: config, key: key, directory: os.NewFile(uintptr(directoryFd), filepath.Dir(path))}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, self.close())
		}
	}()
	if err := self.storage.checkWrite(self.directory); err != nil {
		return nil, err
	}
	if create {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			return nil, errors.Join(errors.New("recycle custody already exists; reopen original state"), err)
		}
	}
	self.lock, err = storage.openSnapshotMarker(path)
	if err != nil {
		return nil, err
	}
	fd := int(self.lock.Fd())
	self.marker = &bootstrapContractReadinessMarker{storage: self.storage, file: self.lock, path: path + ".lock", root: root}
	if err := bootstrapSuccessorPrivateRegular(self.lock); err != nil {
		return nil, errors.Join(errors.New("recycle marker is not a private regular file"), err)
	}
	if err := mainnetDurableFlock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil, errors.Join(errors.New("recycle custody has another local owner"), err)
	}
	marker := rootObjectHash(config) + "\n" + key + "\n"
	if err := self.storage.bindMarker(self.lock, false); err != nil {
		return nil, err
	}
	if err := self.storage.bindSnapshot(path, "mainnet-owner-recycle", ownerRecycleStoreLimit, create); err != nil {
		return nil, err
	}
	if create {
		if err := self.checkpoint(); err != nil {
			return nil, err
		}
		written, err := self.storage.writeMarkerAt([]byte(marker), 0)
		if written != len(marker) && err == nil {
			err = io.ErrShortWrite
		}
		self.marker.expected = marker
		if err := errors.Join(err, self.lock.Sync(), self.syncParent(), self.checkpoint()); err != nil {
			return nil, err
		}
		if claimHook != nil {
			if err := claimHook("marker-synced"); err != nil {
				return nil, err
			}
		}
	} else {
		raw, err := io.ReadAll(io.LimitReader(self.lock, int64(len(marker)+len(bootstrapRootClaimComplete)+1)))
		if err != nil {
			return nil, err
		}
		if string(raw) == marker+bootstrapRootClaimComplete {
			self.marker.expected, self.complete = string(raw), true
			if err := self.storage.bindMarker(self.lock, true); err != nil {
				return nil, err
			}
			if _, err := self.load(); err != nil {
				return nil, err
			}
			return self, nil
		}
		if string(raw) != marker {
			return nil, errors.New("recycle marker differs from original independently approved config")
		}
		self.marker.expected = marker
	}
	record, err := self.load()
	if errors.Is(err, os.ErrNotExist) {
		record = ownerRecycleRecord{Schema: ownerRecycleRecordSchema, Config: config, ApprovalKey: key, Phase: "reserved"}
		record.ContentHash = rootObjectHash(record)
		if err := self.save(record); err != nil {
			return nil, err
		}
	} else if err != nil || record.Phase != "reserved" {
		return nil, errors.Join(errors.New("recycle interrupted claim has invalid or advanced progress"), err)
	}
	if _, err := self.load(); err != nil {
		return nil, err
	}
	if claimHook != nil {
		if err := claimHook("progress-synced"); err != nil {
			return nil, err
		}
	}
	written, err := self.storage.writeMarkerAt([]byte(bootstrapRootClaimComplete), int64(len(marker)))
	if written != len(bootstrapRootClaimComplete) && err == nil {
		err = io.ErrShortWrite
	}
	self.marker.expected, self.complete = marker+bootstrapRootClaimComplete, true
	if err := errors.Join(err, self.lock.Sync(), self.checkpoint()); err != nil {
		return nil, err
	}
	if err := self.storage.bindMarker(self.lock, true); err != nil {
		return nil, err
	}
	return self, nil
}

// Releasing local ownership preserves the permanent original claim marker.
func (self *ownerRecycleStore) close() error {
	if self == nil {
		return nil
	}
	err := self.storage.close()
	for _, file := range []*os.File{self.lock, self.directory} {
		if file != nil {
			err = errors.Join(err, file.Close())
		}
	}
	self.lock, self.directory = nil, nil
	return err
}

// Bounded, no-follow reads reject truncation, unknown JSON and altered approval.
func (self *ownerRecycleStore) load() (ownerRecycleRecord, error) {
	var record ownerRecycleRecord
	raw, err := self.readRecord()
	if err != nil {
		return record, err
	}
	if err := decodePlanJson(raw, &record); err != nil {
		return ownerRecycleRecord{}, self.failIntegrity(err)
	}
	if err := record.validate(self.config, self.key); err != nil {
		return ownerRecycleRecord{}, self.failIntegrity(err)
	}
	self.expectedHash = monitorReadDigest(raw)
	return record, nil
}

// File sync, atomic rename and parent sync precede acknowledgment. The original
// bytes may already be durable on error; never roll back or retry in this instance.
func (self *ownerRecycleStore) save(record ownerRecycleRecord) (resultErr error) {
	if err := self.storage.checkWrite(self.directory); err != nil {
		return err
	}
	defer func() {
		if resultErr != nil && (self.storage == nil || self.storage.failed != nil || !mainnetDurableAdmissionPending(resultErr)) {
			self.failed = resultErr
		}
	}()
	if err := self.checkpoint(); err != nil {
		return err
	}
	if err := record.validate(self.config, self.key); err != nil {
		return err
	}
	raw, err := json.Marshal(record)
	if err != nil || len(raw)+1 > ownerRecycleStoreLimit {
		return errors.Join(errors.New("recycle journal exceeds bound"), err)
	}
	return self.publishRecord(record, append(raw, '\n'))
}

// The scoped hook makes post-rename failure deterministic in qualification.
func (self *ownerRecycleStore) syncParent() error {
	if self.directory == nil {
		return errors.New("recycle directory is closed")
	}
	syncDirectory := self.syncDirectory
	if syncDirectory == nil {
		syncDirectory = (*os.File).Sync
	}
	return errors.Join(syncDirectory(self.directory), self.storage.checkWrite(self.directory))
}
