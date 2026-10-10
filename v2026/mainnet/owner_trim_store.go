// Trim custody holds all original v3 journals read-only and one exclusive action
// journal. Neither an expired action nor a missing file grants a new allowance.
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

const ownerTrimStoreLimit = 64 * 1024 * 1024

// A failed publication poisons this instance, including failure after rename.
// The external custody fence remains responsible for rollback and cross-host use.
type ownerTrimStore struct {
	storage       *mainnetDurableDirectory
	config        ownerTrimExecutionConfig
	key           string
	policy        subnetCensusPolicy
	review        ownerTrimPlan
	retained      *bootstrapChainReadinessState
	lock          *os.File
	directory     *os.File
	marker        *bootstrapContractReadinessMarker
	complete      bool
	expectedHash  string
	failed        error
	syncDirectory func(*os.File) error
}

// Claim a fixed sixth journal only after revalidating the exact original v3
// preparation and separately signed trim approval. Resume never adopts another.
func openOwnerTrimStore(ctx context.Context, preparation bootstrapChainPreparation, config ownerTrimExecutionConfig, key string, create bool) (_ *ownerTrimStore, resultErr error) {
	return openOwnerTrimStoreWithClaimHook(ctx, preparation, config, key, create, nil)
}

// The scoped test hook stops only after real marker or reservation durability.
// It cannot change the accepted preparation, checkpoint or effect allowance.
func openOwnerTrimStoreWithClaimHook(ctx context.Context, preparation bootstrapChainPreparation, config ownerTrimExecutionConfig, key string, create bool, claimHook func(string) error) (_ *ownerTrimStore, resultErr error) {
	if preparation.Plan.Config.noOwnerTrim() {
		return nil, errors.New("owner trim custody refused: the accepted preparation sealed no owner trim")
	}
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
	original := append(preparation.protectedPaths(), preparation.Plan.ConfigPath, preparation.Plan.Config.OwnerTrimPolicy.Path, preparation.Plan.Config.OwnerTrimPlan.Path,
		preparation.Plan.Config.Contracts.Path, preparation.Plan.Config.Root.Path)
	original = append(original, preparation.Plan.Config.validatorConfigPaths()...)
	for _, path := range append(original, preparation.Plan.Config.RootValidator.Approval.Path, preparation.Root.ServiceInput.Path, preparation.Contracts.Config.Plan.Artifacts.Path) {
		if action.StatePath == path || action.StatePath+".lock" == path || path+".lock" == action.StatePath {
			return nil, errors.New("owner trim fixed custody path overlaps original preparation")
		}
	}
	seen := map[string]bool{action.StatePath: true, action.StatePath + ".lock": true}
	if err := validateBootstrapChainValidatorPaths(preparation.Plan.ValidatorInspections, seen); err != nil {
		return nil, err
	}
	self.storage, err = openMainnetDurableDirectory(ctx, filepath.Dir(config.Action.StatePath), durablevolume.ReadWrite)
	if err != nil {
		return nil, err
	}
	root, err := bootstrapSuccessorPhysicalRoot(filepath.Dir(action.StatePath))
	if err != nil {
		return nil, err
	}
	directoryFd, err := unix.Open(filepath.Dir(action.StatePath), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	self.directory = os.NewFile(uintptr(directoryFd), filepath.Dir(action.StatePath))
	if err := self.storage.checkWrite(self.directory); err != nil {
		return nil, err
	}
	if create {
		if _, err := os.Lstat(action.StatePath); !errors.Is(err, os.ErrNotExist) {
			return nil, errors.Join(errors.New("owner trim action already exists; resume original custody"), err)
		}
	}
	self.lock, err = self.storage.openSnapshotMarker(action.StatePath)
	if err != nil {
		return nil, err
	}
	fd := int(self.lock.Fd())
	self.marker = &bootstrapContractReadinessMarker{storage: self.storage, file: self.lock, path: action.StatePath + ".lock", root: root}
	if err := bootstrapSuccessorPrivateRegular(self.lock); err != nil {
		return nil, errors.Join(errors.New("owner trim marker is not a private regular file"), err)
	}
	if err := mainnetDurableFlock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil, errors.Join(errors.New("owner trim action already has a local owner"), err)
	}
	marker := rootObjectHash(config) + "\n" + key + "\n"
	if err := self.storage.bindMarker(self.lock, false); err != nil {
		return nil, err
	}
	if err := self.storage.bindSnapshot(action.StatePath, "mainnet-owner-trim", ownerTrimStoreLimit, create); err != nil {
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
			return nil, errors.New("owner trim marker differs from independently approved original action")
		}
		self.marker.expected = marker
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

// All borrowed locks are released even if the trim marker itself failed to open.
func (self *ownerTrimStore) close() error {
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
	if self.retained != nil {
		err = errors.Join(err, self.retained.close())
		self.retained = nil
	}
	return err
}

// Missing, changed, oversized or partial state is never a fresh reservation.
func (self *ownerTrimStore) load() (ownerTrimRecord, error) {
	var record ownerTrimRecord
	raw, err := self.readRecord()
	if err != nil {
		return record, err
	}
	if err := decodePlanJson(raw, &record); err != nil {
		return ownerTrimRecord{}, self.failIntegrity(err)
	}
	if err := record.validate(self.config, self.key); err != nil {
		return ownerTrimRecord{}, self.failIntegrity(err)
	}
	self.expectedHash = monitorReadDigest(raw)
	return record, nil
}

// Full file sync, atomic rename and directory sync precede every side effect.
func (self *ownerTrimStore) save(record ownerTrimRecord) (resultErr error) {
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
	if err != nil || len(raw)+1 > ownerTrimStoreLimit {
		return errors.Join(errors.New("owner trim retained evidence exceeds bound"), err)
	}
	return self.publishRecord(record, append(raw, '\n'))
}

// The scoped hook exercises the real post-rename durability boundary.
func (self *ownerTrimStore) syncParent() error {
	if self.directory == nil {
		return errors.New("owner trim directory is closed")
	}
	syncDirectory := self.syncDirectory
	if syncDirectory == nil {
		syncDirectory = (*os.File).Sync
	}
	return errors.Join(syncDirectory(self.directory), self.storage.checkWrite(self.directory))
}
