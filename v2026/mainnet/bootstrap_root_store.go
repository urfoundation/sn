// Bootstrap progress records only local phase completion. The existing custody
// and service journals remain authoritative for signatures, nonces and budgets.
package main

import (
	"context"
	"encoding/json"
	"errors"
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
	plan          bootstrapRootPlan
	path          string
	lock          *os.File
	syncDirectory func(*os.File) error
}

// Apply claims a new plan once. Resume can finish an interrupted initial claim
// only before any child exists; completed claims require their full record.
func openBootstrapRootStore(plan bootstrapRootPlan, create bool) (*bootstrapRootStore, error) {
	return openBootstrapRootStoreWithClaimHook(plan, create, nil)
}

// The optional scoped hook exposes only durable pre-child claim boundaries.
// It cannot acknowledge a partial claim or change a child allowance.
func openBootstrapRootStoreWithClaimHook(plan bootstrapRootPlan, create bool, claimHook func(string) error) (*bootstrapRootStore, error) {
	if err := errors.Join(plan.validate(), bootstrapRootDirectory(plan.RunDirectory)); err != nil {
		return nil, err
	}
	path := filepath.Join(plan.RunDirectory, bootstrapRootProgressFile)
	flags := syscall.O_RDWR | syscall.O_CLOEXEC | syscall.O_NOFOLLOW | syscall.O_NONBLOCK
	if create {
		for _, candidate := range append([]string{path}, plan.custodyChildPaths()...) {
			for _, value := range []string{candidate, candidate + ".lock"} {
				if _, err := os.Lstat(value); !errors.Is(err, os.ErrNotExist) {
					return nil, errors.Join(errors.New("bootstrap root apply requires unused journal paths; retain existing owners for resume"), err)
				}
			}
		}
		flags |= syscall.O_CREAT | syscall.O_EXCL
	}
	fd, err := syscall.Open(path+".lock", flags, 0600)
	if err != nil {
		return nil, err
	}
	store := &bootstrapRootStore{plan: copyBootstrapRootPlan(plan), path: path, lock: os.NewFile(uintptr(fd), path+".lock")}
	success := false
	defer func() {
		if !success {
			store.close()
		}
	}()
	info, err := store.lock.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.Join(errors.New("bootstrap root marker is not a private regular file"), err)
	}
	if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil, errors.Join(errors.New("bootstrap root already has an owner"), err)
	}
	if create {
		marker := plan.ContentHash + "\n"
		written, writeErr := store.lock.WriteString(marker)
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
	success = true
	return store, nil
}

// Only an exact initializing marker reaches this bounded recovery. The complete
// marker is synced before a caller can open any child, so later record loss
// cannot be interpreted as an unused allowance.
func (self *bootstrapRootStore) finishInitialClaim(claimHook func(string) error) error {
	for _, path := range self.plan.custodyChildPaths() {
		for _, candidate := range []string{path, path + ".lock"} {
			if _, err := os.Lstat(candidate); !errors.Is(err, os.ErrNotExist) {
				return errors.Join(errors.New("bootstrap root interrupted claim has child state; recovery refused"), err)
			}
		}
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
	written, writeErr := self.lock.WriteAt([]byte(bootstrapRootClaimComplete), int64(len(self.plan.ContentHash)+1))
	if written != len(bootstrapRootClaimComplete) && writeErr == nil {
		writeErr = io.ErrShortWrite
	}
	return errors.Join(writeErr, self.lock.Sync())
}

// Closing releases ownership but never deletes an allowance or a phase marker.
func (self *bootstrapRootStore) close() error {
	if self == nil || self.lock == nil {
		return nil
	}
	err := self.lock.Close()
	self.lock = nil
	return err
}

// A missing or invalid record cannot become a fresh accepted plan.
func (self *bootstrapRootStore) load() (bootstrapRootRecord, error) {
	var record bootstrapRootRecord
	if self.lock == nil {
		return record, errors.New("bootstrap root progress store is closed")
	}
	raw, _, err := readBootstrapRootFile(context.Background(), self.path, 16*1024)
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
	file, err := os.CreateTemp(self.plan.RunDirectory, ".sn-mainnet-bootstrap-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	raw = append(raw, '\n')
	written, writeErr := file.Write(raw)
	if written != len(raw) && writeErr == nil {
		writeErr = io.ErrShortWrite
	}
	if err := errors.Join(writeErr, file.Sync(), file.Close()); err != nil {
		return err
	}
	if err := os.Rename(file.Name(), self.path); err != nil {
		return err
	}
	return self.syncParent()
}

// A parent sync makes newly created markers and renamed records durable before
// their acknowledgement. Tests can fail this exact boundary without a sleep.
func (self *bootstrapRootStore) syncParent() error {
	directory, err := os.Open(self.plan.RunDirectory)
	if err != nil {
		return err
	}
	syncDirectory := self.syncDirectory
	if syncDirectory == nil {
		syncDirectory = (*os.File).Sync
	}
	return errors.Join(syncDirectory(directory), directory.Close())
}
