// Bootstrap progress records only local phase completion. The existing custody
// and service journals remain authoritative for signatures, nonces and budgets.
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
)

const bootstrapRootStateSchema = "urnetwork-mainnet-bootstrap-root-progress-v1"
const bootstrapRootClaimComplete = "claimed\n"

// The marker binds the accepted plan; a completed child may never be recreated.
// SignatureHash is a consistency check against custody, not another receipt.
type bootstrapRootRecord struct {
	Schema        string `json:"schema"`
	PlanHash      string `json:"plan_hash"`
	Phase         string `json:"phase"`
	SignatureHash string `json:"extrinsic_hash,omitempty"`
	ContentHash   string `json:"content_hash"`
}

// Progress cannot renew a native action or encode a successful chain outcome.
func (self bootstrapRootRecord) validate(plan bootstrapRootPlan) error {
	claimed := self.ContentHash
	self.ContentHash = ""
	if self.Schema != bootstrapRootStateSchema || self.PlanHash != plan.ContentHash || claimed != rootObjectHash(self) {
		return errors.New("bootstrap root progress differs from the independently accepted plan")
	}
	if plan.PassiveService != nil {
		if self.SignatureHash != "" || self.Phase != "claimed" && self.Phase != "passive-service-retained" {
			return errors.New("passive root progress cannot retain a native signature or legacy custody phase")
		}
		return nil
	}
	switch self.Phase {
	case "claimed", "custody-retained", "service-retained":
		if self.SignatureHash != "" {
			return errors.New("bootstrap root progress has an unearned signature observation")
		}
	case "signature-retained":
		if !rootCanonicalHash(self.SignatureHash) {
			return errors.New("bootstrap root signature observation is incomplete")
		}
	default:
		return errors.New("bootstrap root progress phase is unknown")
	}
	return nil
}

// Storage is serialized by one local phase owner and never calls a child.
type bootstrapRootStorage interface {
	load() (bootstrapRootRecord, error)
	save(bootstrapRootRecord) error
}

// The caller joins phase operations before close. A failed save poisons the
// owner; reopen rechecks progress and each child's actual durable state.
type bootstrapRootStore struct {
	storage       *mainnetDurableDirectory
	plan          bootstrapRootPlan
	path          string
	lock          *os.File
	syncDirectory func(*os.File) error
}

// Apply claims a new plan once. Resume can finish an interrupted initial claim
// only before any child exists; completed claims require their full record.
func openBootstrapRootStore(plan bootstrapRootPlan, create bool, storageContexts ...context.Context) (*bootstrapRootStore, error) {
	return openBootstrapRootStoreWithClaimHook(plan, create, nil, storageContexts...)
}

// The optional scoped hook exposes only durable pre-child claim boundaries.
// It cannot acknowledge a partial claim or change a child allowance.
func openBootstrapRootStoreWithClaimHook(plan bootstrapRootPlan, create bool, claimHook func(string) error, storageContexts ...context.Context) (*bootstrapRootStore, error) {
	if err := errors.Join(plan.validate(), bootstrapRootDirectory(plan.RunDirectory)); err != nil {
		return nil, err
	}
	path := filepath.Join(plan.RunDirectory, bootstrapRootProgressFile)
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
		if err := requireFreshSnapshotPaths(append([]string{path}, plan.custodyChildPaths()...)); err != nil {
			return nil, err
		}
		if err := requireFreshBootstrapPassiveCheckpoint(storage.ctx, plan); err != nil {
			return nil, err
		}
	}
	lock, err := storage.openSnapshotMarker(path)
	if err != nil {
		return nil, err
	}
	fd := int(lock.Fd())
	store := &bootstrapRootStore{storage: storage, plan: copyBootstrapRootPlan(plan), path: path, lock: lock}
	transferred = true
	success := false
	defer func() {
		if !success {
			store.close()
		}
	}()
	if err := storage.bindMarker(store.lock, false); err != nil {
		return nil, err
	}
	info, err := store.lock.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.Join(errors.New("bootstrap root marker is not a private regular file"), err)
	}
	if err := mainnetDurableFlock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil, errors.Join(errors.New("bootstrap root already has an owner"), err)
	}
	if err := storage.bindSnapshot(path, "mainnet-bootstrap-root", 16*1024, create); err != nil {
		return nil, err
	}
	if create {
		marker := plan.ContentHash + "\n"
		written, writeErr := storage.writeMarkerAt([]byte(marker), 0)
		if written != len(marker) && writeErr == nil {
			writeErr = io.ErrShortWrite
		}
		if err := errors.Join(writeErr, store.lock.Sync(), store.syncParent()); err != nil {
			return nil, err
		}
		if claimHook != nil {
			if err := claimHook("marker-synced"); err != nil {
				return nil, err
			}
		}
		if err := store.finishInitialClaim(claimHook); err != nil {
			return nil, err
		}
	} else {
		marker := plan.ContentHash + "\n"
		raw, err := io.ReadAll(io.LimitReader(store.lock, int64(len(marker)+len(bootstrapRootClaimComplete)+1)))
		if err != nil {
			return nil, err
		}
		switch string(raw) {
		case marker:
			if err := store.finishInitialClaim(claimHook); err != nil {
				return nil, err
			}
		case marker + bootstrapRootClaimComplete:
			if _, err := store.load(); err != nil {
				return nil, err
			}
		default:
			return nil, errors.Join(errors.New("bootstrap root marker differs from the accepted plan"), err)
		}
	}
	if err := storage.bindMarker(store.lock, true); err != nil {
		return nil, err
	}
	success = true
	return store, nil
}

// Only an exact initializing marker reaches this bounded recovery. The complete
// marker is synced before a caller can open any child, so later record loss
// cannot be interpreted as an unused allowance.
func (self *bootstrapRootStore) finishInitialClaim(claimHook func(string) error) error {
	if err := requireFreshSnapshotPaths(self.plan.custodyChildPaths()); err != nil {
		return err
	}
	if err := requireFreshBootstrapPassiveCheckpoint(self.storage.ctx, self.plan); err != nil {
		return err
	}
	record, err := self.load()
	if errors.Is(err, os.ErrNotExist) {
		record = bootstrapRootRecord{Schema: bootstrapRootStateSchema, PlanHash: self.plan.ContentHash, Phase: "claimed"}
		record.ContentHash = rootObjectHash(record)
		if err := self.save(record); err != nil {
			return err
		}
	} else if err != nil || record.Phase != "claimed" {
		return errors.Join(errors.New("bootstrap root interrupted claim has invalid or advanced progress"), err)
	}
	if claimHook != nil {
		if err := claimHook("progress-synced"); err != nil {
			return err
		}
	}
	written, writeErr := self.storage.writeMarkerAt([]byte(bootstrapRootClaimComplete), int64(len(self.plan.ContentHash)+1))
	if written != len(bootstrapRootClaimComplete) && writeErr == nil {
		writeErr = io.ErrShortWrite
	}
	return errors.Join(writeErr, self.lock.Sync())
}

// Passive preparation never opens the monitor as a writer or repairs it. The
// separate monitor must already have explicit fresh custody for its own root.
func requireFreshBootstrapPassiveCheckpoint(ctx context.Context, plan bootstrapRootPlan) error {
	if plan.PassiveService == nil {
		return nil
	}
	owner, err := openBootstrapUnclaimedSnapshot(ctx, plan.PassiveService.CheckpointPath, "mainnet-monitor-checkpoint", maxRpcReplyBytes)
	if err != nil {
		return err
	}
	return owner.close()
}

// Closing releases ownership but never deletes an allowance or a phase marker.
func (self *bootstrapRootStore) close() error {
	if self == nil || self.lock == nil {
		return nil
	}
	err := errors.Join(self.storage.close(), self.lock.Close())
	self.lock = nil
	return err
}

// A missing or invalid record cannot become a fresh accepted plan.
func (self *bootstrapRootStore) load() (bootstrapRootRecord, error) {
	var record bootstrapRootRecord
	if self.lock == nil {
		return record, errors.New("bootstrap root progress store is closed")
	}
	raw, _, err := self.storage.readFile(context.Background(), self.path, 16*1024)
	if err != nil {
		return record, err
	}
	if err := decodePlanJson(raw, &record); err != nil {
		return record, err
	}
	return record, record.validate(self.plan)
}

// File sync, atomic rename and directory sync precede any published progress.
// An error after rename is ambiguous and requires a fresh owning instance.
func (self *bootstrapRootStore) save(record bootstrapRootRecord) error {
	if err := self.storage.checkWrite(nil); err != nil {
		return err
	}
	if self.lock == nil {
		return errors.New("bootstrap root progress store is closed")
	}
	if err := record.validate(self.plan); err != nil {
		return err
	}
	if info, err := os.Lstat(self.path); err == nil {
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return errors.New("bootstrap root progress target is not a private regular file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	raw, err := json.Marshal(record)
	if err != nil || len(raw) > 16*1024 {
		return errors.Join(errors.New("bootstrap root progress exceeds its bound"), err)
	}
	return self.storage.publish(self.path, append(raw, '\n'), self.syncDirectory)
}

// A parent sync makes newly created markers and renamed records durable before
// their acknowledgement. Tests can fail this exact boundary without a sleep.
func (self *bootstrapRootStore) syncParent() error {
	if err := self.storage.checkWrite(nil); err != nil {
		return err
	}
	syncDirectory := self.syncDirectory
	if syncDirectory == nil {
		syncDirectory = (*os.File).Sync
	}
	return errors.Join(syncDirectory(self.storage.directory.File()), self.storage.checkWrite(nil))
}
