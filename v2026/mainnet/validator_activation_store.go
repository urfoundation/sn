// Permanent host custody reuses the qualified protected-file and synced
// publication pattern. Missing state never renews either initial start.
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

// Each role retains its own installed bytes, consumed start and postcondition.
// A first role's completion cannot be mistaken for completion of the pair.
type validatorActivationUnitState struct {
	InstallIntent          bool                          `json:"install_intent"`
	Installed              bool                          `json:"installed"`
	StartAt                time.Time                     `json:"start_consumed_at"`
	StartMonotonicUsec     uint64                        `json:"start_consumed_monotonic_usec"`
	Readiness              *validatorActivationReadiness `json:"start_readiness,omitempty"`
	Generation             *repairValidatorGeneration    `json:"acknowledged_generation,omitempty"`
	Completed              *repairValidatorPostcondition `json:"process_postcondition,omitempty"`
	Status                 string                        `json:"status"`
	CurrentAuthorityHash   string                        `json:"current_authority_hash,omitempty"`
	RecheckedReadinessHash string                        `json:"rechecked_readiness_hash,omitempty"`
}

type validatorActivationRecord struct {
	Schema               string                                    `json:"schema"`
	Approval             validatorActivationApproval               `json:"approval"`
	PublicKey            string                                    `json:"independent_public_key"`
	HighWaterAt          time.Time                                 `json:"high_water_at"`
	Operations           uint32                                    `json:"operations"`
	Units                [2]validatorActivationUnitState           `json:"units"`
	Readiness            *validatorActivationReadiness             `json:"last_readiness,omitempty"`
	ProofCheckpoints     []*validatorActivationProofCheckpoint     `json:"completed_proof_checkpoints,omitempty"`
	CommittedCheckpoints []*validatorActivationCommittedCheckpoint `json:"completed_committed_checkpoints,omitempty"`
	CurrentAuthority     *validatorActivationCurrentRetained       `json:"current_authority,omitempty"`
	Status               string                                    `json:"status"`
	ContentHash          string                                    `json:"content_hash"`
}

// Only one synchronous invocation may hold this process lock. All external
// operations join before close; ambiguous publication poisons until reopen.
type validatorActivationStore struct {
	storage       *mainnetDurableDirectory
	path          string
	approval      validatorActivationApproval
	publicKey     string
	lock          *os.File
	directoryInfo os.FileInfo
	poisoned      error
	syncDirectory func(*os.File) error
}

func (self validatorActivationRecord) validate(approval validatorActivationApproval, publicKey string) error {
	hash := self.ContentHash
	self.ContentHash = ""
	if self.Schema != validatorActivationSchema || rootObjectHash(self.Approval) != rootObjectHash(approval) || self.PublicKey != publicKey || self.HighWaterAt.IsZero() || self.Operations > approval.Plan.MaximumOperations || hash != rootObjectHash(self) {
		return errors.New("validator activation journal authority, count or checksum differs")
	}
	if self.Readiness != nil && self.Readiness.validate(approval.Plan) != nil {
		return errors.New("validator activation retained readiness differs")
	}
	if self.CurrentAuthority != nil {
		retained := self.CurrentAuthority
		if retained.Approval.validateApproval(approval, publicKey, retained.PublicKey) != nil || !repairValidatorPath(retained.Reference.Path) || !planSha256(retained.Reference.Sha256) {
			return errors.New("validator activation lost retained current authority")
		}
	}
	validateCurrent := func(readiness *validatorActivationReadiness) error {
		if readiness != nil && readiness.Current != nil && (self.CurrentAuthority == nil || readiness.Current.validate(approval.Plan, *readiness, *self.CurrentAuthority) != nil) {
			return errors.New("validator activation current projection lost independent authority")
		}
		return nil
	}
	if err := validateCurrent(self.Readiness); err != nil {
		return err
	}
	if len(self.ProofCheckpoints) != 0 && len(self.ProofCheckpoints) != 2 {
		return errors.New("validator proof checkpoint census differs")
	}
	for i, checkpoint := range self.ProofCheckpoints {
		if checkpoint != nil && (checkpoint.ObservedAt.After(self.HighWaterAt) || checkpoint.validate(approval.Plan, i) != nil) {
			return errors.New("validator completed proof checkpoint differs")
		}
	}
	if len(self.CommittedCheckpoints) != 0 && len(self.CommittedCheckpoints) != 2 {
		return errors.New("validator committed checkpoint census differs")
	}
	for i, checkpoint := range self.CommittedCheckpoints {
		if checkpoint != nil && (checkpoint.ObservedAt.After(self.HighWaterAt) || checkpoint.validate(approval.Plan, i) != nil) {
			return errors.New("validator completed committed checkpoint differs")
		}
	}
	for _, unit := range self.Units {
		if err := validateCurrent(unit.Readiness); err != nil {
			return err
		}
		if unit.CurrentAuthorityHash != "" && (self.CurrentAuthority == nil || unit.StartAt.IsZero() || unit.Readiness == nil || unit.Readiness.Current == nil || unit.CurrentAuthorityHash != rootObjectHash(self.CurrentAuthority.Approval)) ||
			unit.Readiness != nil && unit.Readiness.Current != nil && unit.CurrentAuthorityHash == "" ||
			unit.RecheckedReadinessHash != "" && (unit.CurrentAuthorityHash == "" || !planSha256(unit.RecheckedReadinessHash)) ||
			unit.CurrentAuthorityHash != "" && unit.Generation != nil && unit.RecheckedReadinessHash == "" {
			return errors.New("validator activation current start lost its independent or counted admission")
		}
		if unit.Installed && !unit.InstallIntent || unit.InstallIntent && self.Operations == 0 || unit.StartAt.IsZero() != (unit.StartMonotonicUsec == 0) ||
			!unit.StartAt.IsZero() && (!unit.Installed || unit.Readiness == nil || unit.StartAt.After(self.HighWaterAt)) || unit.StartAt.IsZero() && (unit.Readiness != nil || unit.Generation != nil || unit.Completed != nil) {
			return errors.New("validator activation journal lost its installation or consumed start")
		}
		if unit.Readiness != nil && unit.Readiness.validate(approval.Plan) != nil {
			return errors.New("validator activation original start readiness differs")
		}
		if unit.Generation != nil && (!repairValidatorHex(unit.Generation.InvocationId, 16) || unit.Generation.Pid <= 1 || unit.Generation.StartedUsec < unit.StartMonotonicUsec) {
			return errors.New("validator activation acknowledged generation differs")
		}
		if unit.Completed != nil && (unit.Generation == nil || unit.Completed.ObservedAt.IsZero() || unit.Completed.ObservedAt.Before(unit.StartAt) || !repairValidatorHex(unit.Completed.InstanceId, 16) || !planSha256(unit.Completed.RecordHash)) {
			return errors.New("validator activation process postcondition differs")
		}
		if (unit.Completed != nil) != (unit.Status == "process-observed") {
			return errors.New("validator activation completion disposition differs")
		}
		switch unit.Status {
		case "", "installing", "installed", "start-consumed", "uncertain-consumed-start", "waiting-progress", "process-observed", "generation-changed":
		default:
			return errors.New("validator activation unit disposition is unknown")
		}
	}
	switch self.Status {
	case "claimed", "operation-reserved", "installed", "admitted-process-only", "observed-operator-and-contract-evidence", "admitted-stake-capacity", "observed-approved-prefix-and-worker-health", "observed-committed-prefix-and-worker-health", "admitted-current-authority", "activation-authority-unavailable", "source-refused", "authority-refused", "approval-window-closed", "operation-limit", "clock-rollback", "partial", "processes-observed":
	default:
		return errors.New("validator activation disposition is unknown")
	}
	if self.Status == "observed-operator-and-contract-evidence" && (self.Readiness == nil || self.Readiness.Production == nil) {
		return errors.New("validator activation production disposition lacks its complete observation")
	}
	if self.Status == "admitted-current-authority" && (self.CurrentAuthority == nil || self.Readiness == nil || self.Readiness.Current == nil) {
		return errors.New("validator activation current disposition lacks complete authority")
	}
	if self.Status == "admitted-stake-capacity" && (self.Readiness == nil || self.Readiness.Stake == nil) {
		return errors.New("validator stake disposition lacks its bounded capacity observation")
	}
	if (self.Status == "observed-approved-prefix-and-worker-health" || self.Status == "observed-committed-prefix-and-worker-health") && (self.Readiness == nil || self.Readiness.Health == nil) {
		return errors.New("validator health disposition lacks its bounded observation")
	}
	if self.Status == "observed-approved-prefix-and-worker-health" || self.Status == "observed-committed-prefix-and-worker-health" {
		if len(self.ProofCheckpoints) != 2 {
			return errors.New("validator health disposition lost its completed proof checkpoints")
		}
		for i, checkpoint := range self.ProofCheckpoints {
			if checkpoint == nil || rootObjectHash(*checkpoint) != rootObjectHash(self.Readiness.Health.Proofs[i]) {
				return errors.New("validator health completed checkpoint differs from its published observation")
			}
		}
	}
	if self.Status == "observed-committed-prefix-and-worker-health" {
		if self.Readiness.Health.Committed == nil || len(self.CommittedCheckpoints) != 2 {
			return errors.New("validator committed disposition lost its completed checkpoints")
		}
		for i, checkpoint := range self.CommittedCheckpoints {
			if checkpoint == nil || rootObjectHash(*checkpoint) != rootObjectHash(self.Readiness.Health.Committed[i]) {
				return errors.New("validator committed checkpoint differs from its published observation")
			}
		}
	}
	if self.Status == "processes-observed" && (self.Units[0].Completed == nil || self.Units[1].Completed == nil) {
		return errors.New("validator activation pair completion is incomplete")
	}
	return nil
}

// Creating another path is not implicit authority: the exact path is signed.
// The host custody owner must protect this directory from rollback or copying.
func openValidatorActivationStore(ctx context.Context, approval validatorActivationApproval, publicKey string, create bool, now time.Time) (*validatorActivationStore, error) {
	if ctx == nil || ctx.Err() != nil || now.IsZero() {
		return nil, errors.New("validator activation store context or clock is unavailable")
	}
	if err := approval.validate(publicKey); err != nil {
		return nil, err
	}
	path := approval.Plan.StatePath
	directory := filepath.Dir(path)
	resolved, err := filepath.EvalSymlinks(directory)
	info, statErr := os.Lstat(directory)
	if err != nil || statErr != nil || resolved != directory || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("validator activation needs an existing private physical directory")
	}
	storage, err := openMainnetDurableDirectory(ctx, directory, durablevolume.ReadWrite)
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
			return nil, errors.New("validator activation state already exists or is unavailable")
		}
	}
	lock, err := storage.openSnapshotMarker(path)
	if err != nil {
		return nil, err
	}
	fd := int(lock.Fd())
	self := &validatorActivationStore{storage: storage, path: path, approval: approval, publicKey: publicKey, lock: lock, directoryInfo: info}
	transferred = true
	success := false
	defer func() {
		if !success {
			self.close()
		}
	}()
	if err := self.storage.bindMarker(self.lock, false); err != nil {
		return nil, err
	}
	if err := self.validateOwner(); err != nil {
		return nil, err
	}
	if err := mainnetDurableFlock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil, errors.Join(errors.New("validator activation already has a process owner"), err)
	}
	marker := rootObjectHash(approval) + " " + publicKey + "\n"
	if err := storage.bindSnapshot(path, "mainnet-validator-activation", 128*1024, create); err != nil {
		return nil, err
	}
	if create {
		_, writeErr := storage.writeMarkerAt([]byte(marker), 0)
		if err := errors.Join(writeErr, self.lock.Sync()); err != nil {
			return nil, err
		}
		if err := self.save(validatorActivationRecord{Schema: validatorActivationSchema, Approval: approval, PublicKey: publicKey, HighWaterAt: now, Status: "claimed"}); err != nil {
			return nil, err
		}
	} else {
		raw, err := io.ReadAll(io.LimitReader(self.lock, 256))
		if err != nil || string(raw) != marker {
			return nil, errors.New("validator activation marker is incomplete or belongs to another approval")
		}
		if _, err := self.load(ctx); err != nil {
			return nil, err
		}
	}
	if err := self.storage.bindMarker(self.lock, true); err != nil {
		return nil, err
	}
	success = true
	return self, nil
}

// Parent and marker identity are rechecked before every authoritative access.
func (self *validatorActivationStore) validateOwner() error {
	if self == nil || self.lock == nil {
		return errors.New("validator activation store is closed")
	}
	if err := self.storage.check(nil); err != nil {
		return err
	}
	if self.poisoned != nil {
		return self.poisoned
	}
	parent, err := os.Lstat(filepath.Dir(self.path))
	opened, openErr := self.lock.Stat()
	named, nameErr := os.Lstat(self.path + ".lock")
	if err != nil || openErr != nil || nameErr != nil || !parent.IsDir() || parent.Mode().Perm()&0077 != 0 || !os.SameFile(parent, self.directoryInfo) || !named.Mode().IsRegular() || named.Mode().Perm()&0077 != 0 || !os.SameFile(opened, named) {
		return errors.New("validator activation physical owner changed")
	}
	return nil
}

// Missing state is a lost liability, never a fresh claim.
func (self *validatorActivationStore) load(ctx context.Context) (validatorActivationRecord, error) {
	var record validatorActivationRecord
	if err := self.validateOwner(); err != nil {
		return record, err
	}
	raw, _, err := self.storage.readFile(ctx, self.path, 128*1024)
	if err != nil {
		return record, err
	}
	if err := decodePlanJson(raw, &record); err != nil {
		return record, err
	}
	return record, record.validate(self.approval, self.publicKey)
}

// This existing atomic/fsync primitive may fail after rename; stop and reopen.
func (self *validatorActivationStore) save(record validatorActivationRecord) error {
	if err := self.validateOwner(); err != nil {
		return err
	}
	record.ContentHash = ""
	record.ContentHash = rootObjectHash(record)
	if err := record.validate(self.approval, self.publicKey); err != nil {
		return err
	}
	raw, err := json.Marshal(record)
	if err != nil || len(raw) > 128*1024 {
		return errors.New("validator activation journal exceeds its bound")
	}
	if err := self.storage.publish(self.path, append(raw, '\n'), self.syncDirectory); err != nil {
		if mainnetDurableAdmissionPending(err) {
			return err
		}
		self.poisoned = errors.Join(errors.New("validator activation publication is ambiguous; reopen required"), err)
		return self.poisoned
	}
	return nil
}

// No lifetime marker or incident evidence is deleted during shutdown.
func (self *validatorActivationStore) close() error {
	if self == nil || self.lock == nil {
		return nil
	}
	err := errors.Join(self.storage.close(), self.lock.Close())
	self.lock = nil
	return err
}
