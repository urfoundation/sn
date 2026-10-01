// Trim custody holds all original v3 journals read-only and one exclusive action
// journal. Neither an expired action nor a missing file grants a new allowance.
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

const ownerTrimStoreLimit = 64 * 1024 * 1024

// A failed publication poisons this instance, including failure after rename.
// The external custody fence remains responsible for rollback and cross-host use.
type ownerTrimStore struct {
	config        ownerTrimExecutionConfig
	key           string
	policy        subnetCensusPolicy
	review        ownerTrimPlan
	retained      *bootstrapChainReadinessState
	lock          *os.File
	failed        error
	syncDirectory func(*os.File) error
}

// Claim a fixed sixth journal only after revalidating the exact original v3
// preparation and separately signed trim approval. Resume never adopts another.
func openOwnerTrimStore(ctx context.Context, preparation bootstrapChainPreparation, config ownerTrimExecutionConfig, key string, create bool) (_ *ownerTrimStore, resultErr error) {
	if err := config.validate(key); err != nil {
		return nil, err
	}
	retained, err := openBootstrapChainReadinessState(ctx, preparation)
	if err != nil {
		return nil, err
	}
	self := &ownerTrimStore{config: config, key: key, retained: retained}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, self.close())
		}
	}()
	policyRaw, err := readBootstrapChainInput(ctx, preparation.Plan.Config.OwnerTrimPolicy, maxRpcReplyBytes)
	if err != nil {
		return nil, err
	}
	if err := decodePlanJson(policyRaw, &self.policy); err != nil {
		return nil, err
	}
	reviewRaw, err := readBootstrapChainInput(ctx, preparation.Plan.Config.OwnerTrimPlan, maximumOwnerTrimPlanBytes)
	if err != nil {
		return nil, err
	}
	if err := decodePlanJson(reviewRaw, &self.review); err != nil {
		return nil, err
	}
	if err := validateOwnerTrimGuardPlan(self.policy, preparation.Plan.Config.OwnerTrimPolicy.Sha256, self.review); err != nil {
		return nil, err
	}
	action, policy := config.Action, self.policy
	expectedRuntime := rootReceiptProfile{RuntimeSourceCommit: policy.RuntimeSourceCommit, RuntimeVersion: policy.RuntimeVersion,
		RuntimeCodeHash: policy.RuntimeCodeHash, RuntimeMetadataHash: policy.RuntimeMetadataHash}
	if action.PreparationHash != preparation.Plan.ContentHash || action.PreparationStateHash != rootObjectHash(retained) ||
		action.PolicyHash != preparation.Plan.Config.OwnerTrimPolicy.Sha256 || action.ReviewHash != preparation.Plan.OwnerTrimContentHash || action.ReviewHash != self.review.ContentHash ||
		action.Network != preparation.Plan.Config.Network || action.Runtime != expectedRuntime || action.Coldkey != policy.SubnetOwnerColdkey ||
		action.SubnetGeneration != *policy.SubnetGeneration || action.SubnetRegistrationBlock != *policy.SubnetRegistrationBlock ||
		action.MaximumUids != self.review.Best.MaximumUids || action.StatePath != filepath.Join(preparation.Plan.Config.RunDirectory, ownerTrimStateFile) {
		return nil, errors.New("owner trim action differs from original v3 custody, reviewed capacity or protected scope")
	}
	// The new fixed journal must also be disjoint from every original input and
	// role custody namespace; the original v3 paths themselves are unchanged.
	for _, path := range append(preparation.protectedPaths(), preparation.Plan.ConfigPath, preparation.Plan.Config.OwnerTrimPolicy.Path, preparation.Plan.Config.OwnerTrimPlan.Path,
		preparation.Plan.Config.Contracts.Path, preparation.Plan.Config.Root.Path, preparation.Plan.Config.Validators[0].Config.Path,
		preparation.Plan.Config.Validators[1].Config.Path, preparation.Plan.Config.RootValidator.Approval.Path,
		preparation.Root.ServiceInput.Path, preparation.Contracts.Config.Plan.Artifacts.Path) {
		if action.StatePath == path || action.StatePath+".lock" == path || path+".lock" == action.StatePath {
			return nil, errors.New("owner trim fixed custody path overlaps original preparation")
		}
	}
	seen := map[string]bool{action.StatePath: true, action.StatePath + ".lock": true}
	if err := validateBootstrapChainValidatorPaths(preparation.Plan.ValidatorInspections, seen); err != nil {
		return nil, err
	}
	flags := syscall.O_RDWR | syscall.O_CLOEXEC | syscall.O_NOFOLLOW | syscall.O_NONBLOCK
	if create {
		if _, err := os.Lstat(action.StatePath); !errors.Is(err, os.ErrNotExist) {
			return nil, errors.Join(errors.New("owner trim action already exists; resume original custody"), err)
		}
		flags |= syscall.O_CREAT | syscall.O_EXCL
	}
	fd, err := syscall.Open(action.StatePath+".lock", flags, 0600)
	if err != nil {
		return nil, err
	}
	self.lock = os.NewFile(uintptr(fd), action.StatePath+".lock")
	info, err := self.lock.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.Join(errors.New("owner trim marker is not a private regular file"), err)
	}
	if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil, errors.Join(errors.New("owner trim action already has a local owner"), err)
	}
	marker := rootObjectHash(config) + "\n" + key + "\n"
	if create {
		written, err := self.lock.WriteString(marker)
		if written != len(marker) && err == nil {
			err = io.ErrShortWrite
		}
		if err := errors.Join(err, self.lock.Sync(), self.syncParent()); err != nil {
			return nil, err
		}
	} else {
		raw, err := io.ReadAll(io.LimitReader(self.lock, int64(len(marker)+len(bootstrapRootClaimComplete)+1)))
		if err != nil {
			return nil, err
		}
		if string(raw) == marker+bootstrapRootClaimComplete {
			if _, err := self.load(); err != nil {
				return nil, err
			}
			return self, nil
		}
		if string(raw) != marker {
			return nil, errors.New("owner trim marker differs from independently approved original action")
		}
	}
	// An interrupted initial claim can recover only a reserved, effect-free row.
	// Once the complete marker exists, missing state is permanently refused.
	record, err := self.load()
	if errors.Is(err, os.ErrNotExist) {
		record = ownerTrimRecord{Schema: ownerTrimRecordSchema, Config: config, ApprovalKey: key, Phase: "reserved"}
		record.ContentHash = rootObjectHash(record)
		if err := self.save(record); err != nil {
			return nil, err
		}
	} else if err != nil || record.Phase != "reserved" {
		return nil, errors.Join(errors.New("owner trim interrupted claim has advanced or invalid progress"), err)
	}
	written, err := self.lock.WriteAt([]byte(bootstrapRootClaimComplete), int64(len(marker)))
	if written != len(bootstrapRootClaimComplete) && err == nil {
		err = io.ErrShortWrite
	}
	if err := errors.Join(err, self.lock.Sync()); err != nil {
		return nil, err
	}
	return self, nil
}

// All borrowed locks are released even if the trim marker itself failed to open.
func (self *ownerTrimStore) close() error {
	var err error
	if self.lock != nil {
		err = self.lock.Close()
		self.lock = nil
	}
	if self.retained != nil {
		err = errors.Join(err, self.retained.close())
		self.retained = nil
	}
	return err
}

// Missing, changed, oversized or partial state is never a fresh reservation.
func (self *ownerTrimStore) load() (ownerTrimRecord, error) {
	var record ownerTrimRecord
	if self.lock == nil || self.failed != nil {
		return record, errors.Join(errors.New("owner trim store must reopen"), self.failed)
	}
	raw, _, err := readBootstrapRootFile(context.Background(), self.config.Action.StatePath, ownerTrimStoreLimit)
	if err != nil {
		return record, err
	}
	if err := decodePlanJson(raw, &record); err != nil {
		return record, err
	}
	return record, record.validate(self.config, self.key)
}

// Full file sync, atomic rename and directory sync precede every side effect.
func (self *ownerTrimStore) save(record ownerTrimRecord) (resultErr error) {
	if self.lock == nil || self.failed != nil {
		return errors.Join(errors.New("owner trim store must reopen"), self.failed)
	}
	defer func() {
		if resultErr != nil {
			self.failed = resultErr
		}
	}()
	if err := record.validate(self.config, self.key); err != nil {
		return err
	}
	path := self.config.Action.StatePath
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return errors.New("owner trim target is not a private regular file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	raw, err := json.Marshal(record)
	if err != nil || len(raw)+1 > ownerTrimStoreLimit {
		return errors.Join(errors.New("owner trim retained evidence exceeds bound"), err)
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".owner-trim-action-*")
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
	if err := os.Rename(file.Name(), path); err != nil {
		return err
	}
	return self.syncParent()
}

// The scoped hook exercises the real post-rename durability boundary.
func (self *ownerTrimStore) syncParent() error {
	directory, err := os.Open(filepath.Dir(self.config.Action.StatePath))
	if err != nil {
		return err
	}
	syncDirectory := self.syncDirectory
	if syncDirectory == nil {
		syncDirectory = (*os.File).Sync
	}
	return errors.Join(syncDirectory(directory), directory.Close())
}
