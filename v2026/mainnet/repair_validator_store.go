// Repair custody uses the existing protected-file and synced publication owners.
// A permanent marker prevents missing state from restoring a start allowance.
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
	"time"
)

// A successful start command must retain its observed invocation before a later
// caller can attribute any new process to that command.
type repairValidatorRecord struct {
	Schema             string                        `json:"schema"`
	Approval           repairValidatorApproval       `json:"approval"`
	PublicKey          string                        `json:"independent_public_key"`
	HighWaterAt        time.Time                     `json:"high_water_at"`
	Observations       uint32                        `json:"observations"`
	StartAt            time.Time                     `json:"start_consumed_at"`
	StartMonotonicUsec uint64                        `json:"start_consumed_monotonic_usec"`
	Generation         *repairValidatorGeneration    `json:"acknowledged_generation,omitempty"`
	Completed          *repairValidatorPostcondition `json:"completed,omitempty"`
	Status             string                        `json:"status"`
	ContentHash        string                        `json:"content_hash"`
}

// This is process/source recovery evidence, not chain or economic success.
type repairValidatorPostcondition struct {
	ObservedAt time.Time `json:"observed_at"`
	InstanceId string    `json:"instance_id"`
	RecordHash string    `json:"record_hash"`
}

// Serial use is joined before close. Any ambiguous write poisons this owner.
type repairValidatorStore struct {
	*repairValidatorFileOwner
	approval  repairValidatorApproval
	publicKey string
}

// Both independently scoped repair journals share physical custody only.
// The marker and last read bytes are rechecked before each authoritative access.
type repairValidatorFileOwner struct {
	storage       *mainnetDurableDirectory
	path          string
	lock          *os.File
	directoryInfo os.FileInfo
	poisoned      error
	syncDirectory func(*os.File) error
	marker        string
	expectedHash  string
}

// The original signed request and independent key survive every reopen.
func (self repairValidatorRecord) validate(approval repairValidatorApproval, publicKey string) error {
	hash := self.ContentHash
	self.ContentHash = ""
	if self.Schema != repairValidatorSchema || rootObjectHash(self.Approval) != rootObjectHash(approval) || self.PublicKey != publicKey || self.HighWaterAt.IsZero() || self.Observations > approval.Plan.MaximumObservations || hash != rootObjectHash(self) {
		return errors.New("validator repair journal authority, count or checksum differs")
	}
	if self.StartAt.IsZero() != (self.StartMonotonicUsec == 0) || self.StartAt.IsZero() && (self.Generation != nil || self.Completed != nil) || self.Observations == 0 && !self.StartAt.IsZero() || self.StartAt.After(self.HighWaterAt) {
		return errors.New("validator repair journal lost its consumed start")
	}
	if self.Generation != nil && (!repairValidatorHex(self.Generation.InvocationId, 16) || self.Generation.InvocationId == approval.Plan.Previous.InvocationId || self.Generation.Pid <= 1 || self.Generation.StartedUsec <= approval.Plan.Previous.StartedUsec || self.Generation.StartedUsec < self.StartMonotonicUsec) {
		return errors.New("validator repair journal generation differs")
	}
	if self.Completed != nil && (self.Generation == nil || self.Completed.ObservedAt.IsZero() || !repairValidatorHex(self.Completed.InstanceId, 16) || self.Completed.InstanceId == approval.Plan.Original.State.Record.InstanceId || !validMonitorReadDigest(self.Completed.RecordHash)) {
		return errors.New("validator repair journal postcondition differs")
	}
	if (self.Completed != nil) != (self.Status == "resumed-generation-observed") {
		return errors.New("validator repair completion disposition differs")
	}
	switch self.Status {
	case "claimed", "observation-reserved", "start-consumed", "uncertain-consumed-start", "waiting-progress", "resumed-generation-observed", "approval-window-closed", "observation-limit", "generation-changed", "source-refused", "clock-rollback":
	default:
		return errors.New("validator repair journal disposition is unknown")
	}
	return nil
}

// Creating another path is not implicit authority: the exact path is signed.
// The host custody owner must protect this directory from rollback or copying.
func openRepairValidatorStore(ctx context.Context, approval repairValidatorApproval, publicKey string, create bool, now time.Time) (*repairValidatorStore, error) {
	if ctx == nil || now.IsZero() {
		return nil, errors.New("validator repair store context or clock is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := approval.validate(publicKey); err != nil {
		return nil, err
	}
	owner, err := openRepairValidatorFileOwner(ctx, approval.Plan.StatePath, rootObjectHash(approval)+" "+publicKey+"\n", create)
	if err != nil {
		return nil, err
	}
	self := &repairValidatorStore{repairValidatorFileOwner: owner, approval: approval, publicKey: publicKey}
	if create {
		err = self.save(repairValidatorRecord{Schema: repairValidatorSchema, Approval: approval, PublicKey: publicKey, HighWaterAt: now, Status: "claimed"})
	} else {
		_, err = self.load(ctx)
	}
	if err != nil {
		return nil, errors.Join(err, self.close())
	}
	return self, nil
}

// The exact signed pathname is immutable authority. An existing marker with
// missing state cannot create another budget, regardless of the journal schema.
func openRepairValidatorFileOwner(ctx context.Context, path, marker string, create bool) (result *repairValidatorFileOwner, resultErr error) {
	if ctx == nil {
		return nil, errors.New("validator repair file owner context unavailable")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	directory := filepath.Dir(path)
	resolved, err := filepath.EvalSymlinks(directory)
	info, statErr := os.Lstat(directory)
	if err := errors.Join(err, statErr, ctx.Err()); err != nil {
		return nil, repairValidatorObservationError("cannot inspect validator repair directory", err, false)
	}
	if resolved != directory || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("validator repair needs an existing private physical directory")
	}
	storage, err := openMainnetDurableDirectory(ctx, directory, durablevolume.ReadWrite)
	if err != nil {
		return nil, err
	}
	transferred := false
	defer func() {
		if !transferred {
			resultErr = errors.Join(resultErr, storage.close())
		}
	}()
	if create {
		if _, err := os.Lstat(path); err == nil {
			return nil, errors.New("validator repair state already exists")
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, repairValidatorObservationError("cannot inspect validator repair state", err, false)
		}
	}
	lock, err := storage.openSnapshotMarker(path)
	if err != nil {
		return nil, err
	}
	fd := int(lock.Fd())
	self := &repairValidatorFileOwner{storage: storage, path: path, lock: lock, directoryInfo: info}
	transferred = true
	success := false
	defer func() {
		if !success {
			resultErr = errors.Join(resultErr, self.close())
		}
	}()
	if err := self.storage.bindMarker(self.lock, false); err != nil {
		return nil, err
	}
	if err := self.validateOwner(); err != nil {
		return nil, err
	}
	if err := mainnetDurableFlock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil, errors.Join(errors.New("validator repair already has a process owner"), err)
	}
	if err := storage.bindSnapshot(path, "mainnet-host-action", 64*1024, create); err != nil {
		return nil, err
	}
	if create {
		_, writeErr := storage.writeMarkerAt([]byte(marker), 0)
		if err := errors.Join(writeErr, self.lock.Sync()); err != nil {
			return nil, err
		}
	} else {
		raw, err := io.ReadAll(io.LimitReader(self.lock, 256))
		if err := repairValidatorObservation(ctx, "owner-open-marker", err); err != nil {
			return nil, repairValidatorObservationError("cannot read validator repair marker", err, false)
		}
		if string(raw) != marker {
			return nil, errors.New("validator repair marker is incomplete or belongs to another approval")
		}
	}
	self.marker = marker
	if err := self.storage.bindMarker(self.lock, true); err != nil {
		return nil, err
	}
	success = true
	return self, nil
}

// Parent and marker identity are rechecked before every authoritative access.
func (self *repairValidatorFileOwner) validateOwner() error {
	if self == nil || self.lock == nil || self.storage == nil {
		return errors.New("validator repair store is closed")
	}
	if self.poisoned != nil {
		return self.poisoned
	}
	if err := self.storage.check(nil); err != nil {
		return err
	}
	ctx := self.storage.ctx
	parent, err := os.Lstat(filepath.Dir(self.path))
	if err := repairValidatorObservation(ctx, "owner-parent-stat", err); err != nil {
		return self.observationError("cannot inspect validator repair parent", err, true)
	}
	opened, openErr := self.lock.Stat()
	if err := repairValidatorObservation(ctx, "owner-open-stat", openErr); err != nil {
		return self.observationError("cannot inspect validator repair descriptor", err, false)
	}
	named, nameErr := os.Lstat(self.path + ".lock")
	if err := repairValidatorObservation(ctx, "owner-name-stat", nameErr); err != nil {
		return self.observationError("cannot inspect named validator repair marker", err, true)
	}
	if !parent.IsDir() || parent.Mode().Perm()&0077 != 0 || !os.SameFile(parent, self.directoryInfo) || !named.Mode().IsRegular() || named.Mode().Perm()&0077 != 0 || !os.SameFile(opened, named) {
		return self.identity("validator repair physical owner changed")
	}
	if self.marker != "" {
		raw := make([]byte, len(self.marker)+1)
		n, err := self.lock.ReadAt(raw, 0)
		if err == io.EOF {
			err = nil
		}
		if err := repairValidatorObservation(ctx, "owner-marker-read", err); err != nil {
			return self.observationError("cannot read validator repair marker", err, false)
		}
		if string(raw[:n]) != self.marker {
			return self.identity("validator repair marker content changed")
		}
	}
	if self.expectedHash != "" {
		raw, _, err := self.storage.readFile(ctx, self.path, 64*1024)
		if err := repairValidatorObservation(ctx, "owner-journal-read", err); err != nil {
			return self.observationError("cannot read validator repair retained journal", err, true)
		}
		if monitorReadDigest(raw) != self.expectedHash {
			return self.identity("validator repair retained journal changed")
		}
	}
	return nil
}

// A confirmed loss stays refused in this owner even if its name is restored.
func (self *repairValidatorFileOwner) identity(message string) error {
	self.poisoned = errors.Join(durablevolume.ErrIdentity, errors.New(message))
	return self.poisoned
}

// Unavailable observations do not poison an otherwise unchanged owner.
func (self *repairValidatorFileOwner) observationError(message string, cause error, retainedName bool) error {
	err := repairValidatorObservationError(message, cause, retainedName)
	if errors.Is(err, durablevolume.ErrIdentity) {
		self.poisoned = err
	}
	return err
}

// Missing state is a lost liability, never a fresh claim.
func (self *repairValidatorStore) load(ctx context.Context) (repairValidatorRecord, error) {
	var record repairValidatorRecord
	if err := self.validateOwner(); err != nil {
		return record, err
	}
	raw, _, err := self.storage.readFile(ctx, self.path, 64*1024)
	if err != nil {
		return record, err
	}
	if err := decodePlanJson(raw, &record); err != nil {
		return record, err
	}
	if err := record.validate(self.approval, self.publicKey); err != nil {
		return record, err
	}
	self.expectedHash = monitorReadDigest(raw)
	return record, nil
}

// This existing atomic/fsync primitive may fail after rename; stop and reopen.
func (self *repairValidatorStore) save(record repairValidatorRecord) error {
	if err := self.validateOwner(); err != nil {
		return err
	}
	record.ContentHash = ""
	record.ContentHash = rootObjectHash(record)
	if err := record.validate(self.approval, self.publicKey); err != nil {
		return err
	}
	raw, err := json.Marshal(record)
	if err != nil || len(raw) > 64*1024 {
		return errors.New("validator repair journal exceeds its bound")
	}
	raw = append(raw, '\n')
	if err := self.storage.publish(self.path, raw, self.syncDirectory); err != nil {
		if mainnetDurableAdmissionPending(err) {
			return err
		}
		self.poisoned = errors.Join(errors.New("validator repair publication is ambiguous; reopen required"), err)
		return self.poisoned
	}
	self.expectedHash = monitorReadDigest(raw)
	return nil
}

// No lifetime marker or incident evidence is deleted during shutdown.
func (self *repairValidatorFileOwner) close() error {
	if self == nil || self.lock == nil {
		return nil
	}
	err := errors.Join(self.storage.close(), self.lock.Close())
	self.lock = nil
	return err
}
