// The preparation journal retains local progress only. Child custody journals
// remain authoritative for original signed bytes, receipts, nonces and limits.
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

const bootstrapChainStateSchema = "urnetwork-mainnet-bootstrap-chain-progress-v1"

// Observed signature hashes detect disappearance on later preparation resumes.
// They are consistency checks, never a second signing or financial allowance.
type bootstrapChainRecord struct {
	Schema                  string             `json:"schema"`
	Plan                    bootstrapChainPlan `json:"plan"`
	Phase                   string             `json:"phase"`
	ContractTransactionHash string             `json:"contract_transaction_hash,omitempty"`
	RootExtrinsicHash       string             `json:"root_extrinsic_hash,omitempty"`
	ContentHash             string             `json:"content_hash"`
}

// A checked complete plan survives stdout loss and cannot be rebound on resume.
func (self bootstrapChainRecord) validate(plan bootstrapChainPlan) error {
	claimed := self.ContentHash
	self.ContentHash = ""
	if err := self.Plan.validate(); err != nil {
		return err
	}
	if self.Schema != bootstrapChainStateSchema || self.Plan.ContentHash != plan.ContentHash || claimed != rootObjectHash(self) ||
		self.ContractTransactionHash != "" && !rootCanonicalHash(self.ContractTransactionHash) || self.RootExtrinsicHash != "" && !rootCanonicalHash(self.RootExtrinsicHash) {
		return errors.New("bootstrap chain progress differs from the accepted preparation plan")
	}
	switch self.Phase {
	case "claimed":
		if self.ContractTransactionHash != "" || self.RootExtrinsicHash != "" {
			return errors.New("bootstrap chain initial progress has unearned signature observations")
		}
	case "contracts-retained":
		if self.RootExtrinsicHash != "" {
			return errors.New("bootstrap chain root signature precedes local root preparation")
		}
	case "prepared":
	default:
		return errors.New("bootstrap chain progress phase is unknown")
	}
	return nil
}

// One invocation owns the local flock and serializes all methods until close.
// A failed save poisons the instance; reopening resolves durability ambiguity.
type bootstrapChainStore struct {
	preparation   bootstrapChainPreparation
	path          string
	lock          *os.File
	failed        error
	syncDirectory func(*os.File) error
}

// Apply claims unused paths. Resume can recover a complete initial marker only
// before any child exists; missing completed progress is always refused.
func openBootstrapChainStore(preparation bootstrapChainPreparation, create bool, claimHook func(string) error) (*bootstrapChainStore, error) {
	if err := errors.Join(preparation.validate(), bootstrapRootDirectory(preparation.Plan.Config.RunDirectory)); err != nil {
		return nil, err
	}
	path := filepath.Join(preparation.Plan.Config.RunDirectory, bootstrapChainStateFile)
	flags := syscall.O_RDWR | syscall.O_CLOEXEC | syscall.O_NOFOLLOW | syscall.O_NONBLOCK
	if create {
		for _, path := range preparation.protectedPaths() {
			for _, candidate := range []string{path, path + ".lock"} {
				if _, err := os.Lstat(candidate); !errors.Is(err, os.ErrNotExist) {
					return nil, errors.Join(errors.New("bootstrap chain apply requires unused journals; retain existing custody for resume"), err)
				}
			}
		}
		flags |= syscall.O_CREAT | syscall.O_EXCL
	}
	fd, err := syscall.Open(path+".lock", flags, 0600)
	if err != nil {
		return nil, err
	}
	store := &bootstrapChainStore{preparation: preparation, path: path, lock: os.NewFile(uintptr(fd), path+".lock")}
	success := false
	defer func() {
		if !success {
			store.close()
		}
	}()
	info, err := store.lock.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.Join(errors.New("bootstrap chain marker is not a private regular file"), err)
	}
	if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil, errors.Join(errors.New("bootstrap chain already has a preparation owner"), err)
	}
	marker := preparation.Plan.ContentHash + "\n"
	if create {
		written, err := store.lock.WriteString(marker)
		if written != len(marker) && err == nil {
			err = io.ErrShortWrite
		}
		if err := errors.Join(err, store.lock.Sync(), store.syncParent()); err != nil {
			return nil, err
		}
		if claimHook != nil {
			if err := claimHook("marker-synced"); err != nil {
				return nil, err
			}
		}
	} else {
		raw, err := io.ReadAll(io.LimitReader(store.lock, int64(len(marker)+len(bootstrapRootClaimComplete)+1)))
		if err != nil {
			return nil, err
		}
		if string(raw) == marker+bootstrapRootClaimComplete {
			if _, err := store.load(); err != nil {
				return nil, err
			}
			success = true
			return store, nil
		}
		if string(raw) != marker {
			return nil, errors.New("bootstrap chain marker differs from the accepted plan")
		}
	}
	for _, path := range preparation.protectedPaths()[1:] {
		for _, candidate := range []string{path, path + ".lock"} {
			if _, err := os.Lstat(candidate); !errors.Is(err, os.ErrNotExist) {
				return nil, errors.Join(errors.New("bootstrap chain interrupted claim has child state; recovery refused"), err)
			}
		}
	}
	record, err := store.load()
	if errors.Is(err, os.ErrNotExist) {
		record = bootstrapChainRecord{Schema: bootstrapChainStateSchema, Plan: preparation.Plan, Phase: "claimed"}
		if err := store.save(record); err != nil {
			return nil, err
		}
	} else if err != nil || record.Phase != "claimed" {
		return nil, errors.Join(errors.New("bootstrap chain interrupted claim has invalid or advanced progress"), err)
	}
	if claimHook != nil {
		if err := claimHook("progress-synced"); err != nil {
			return nil, err
		}
	}
	written, err := store.lock.WriteAt([]byte(bootstrapRootClaimComplete), int64(len(marker)))
	if written != len(bootstrapRootClaimComplete) && err == nil {
		err = io.ErrShortWrite
	}
	if err := errors.Join(err, store.lock.Sync()); err != nil {
		return nil, err
	}
	success = true
	return store, nil
}

// Releasing ownership never removes retained progress or the accepted marker.
func (self *bootstrapChainStore) close() error {
	if self == nil || self.lock == nil {
		return nil
	}
	err := self.lock.Close()
	self.lock = nil
	return err
}

// A missing, malformed or rebound journal cannot become a new preparation.
func (self *bootstrapChainStore) load() (bootstrapChainRecord, error) {
	var record bootstrapChainRecord
	if self.lock == nil || self.failed != nil {
		return record, errors.Join(errors.New("bootstrap chain store is closed or requires reopen"), self.failed)
	}
	raw, _, err := readBootstrapRootFile(context.Background(), self.path, rootServiceStoreLimit)
	if err != nil {
		return record, err
	}
	if err := decodePlanJson(raw, &record); err != nil {
		return record, err
	}
	return record, record.validate(self.preparation.Plan)
}

// Publish only after file sync, atomic rename and directory sync. Any failed
// publication ends this instance, including an error after a successful rename.
func (self *bootstrapChainStore) save(record bootstrapChainRecord) (resultErr error) {
	if self.lock == nil || self.failed != nil {
		return errors.Join(errors.New("bootstrap chain store is closed or requires reopen"), self.failed)
	}
	defer func() {
		if resultErr != nil {
			self.failed = resultErr
		}
	}()
	record.ContentHash = ""
	record.ContentHash = rootObjectHash(record)
	if err := record.validate(self.preparation.Plan); err != nil {
		return err
	}
	if info, err := os.Lstat(self.path); err == nil {
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return errors.New("bootstrap chain progress target is not a private regular file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	raw, err := json.Marshal(record)
	if err != nil || len(raw) >= rootServiceStoreLimit {
		return errors.Join(errors.New("bootstrap chain progress exceeds its bound"), err)
	}
	file, err := os.CreateTemp(self.preparation.Plan.Config.RunDirectory, ".sn-mainnet-bootstrap-chain-*")
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

// A scoped test hook can interrupt the actual post-rename durability boundary.
func (self *bootstrapChainStore) syncParent() error {
	directory, err := os.Open(self.preparation.Plan.Config.RunDirectory)
	if err != nil {
		return err
	}
	syncDirectory := self.syncDirectory
	if syncDirectory == nil {
		syncDirectory = (*os.File).Sync
	}
	return errors.Join(syncDirectory(directory), directory.Close())
}
