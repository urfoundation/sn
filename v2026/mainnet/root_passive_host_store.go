// A permanent root-owned claim retains one initial process allowance. Missing
// state, an uncertain command or a changed boot never replenishes that allowance.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

type rootPassiveHostObservation struct {
	ObservedAt time.Time              `json:"observed_at"`
	Root       rootMonitorObservation `json:"root"`
}

type rootPassiveHostRecord struct {
	Schema             string                       `json:"schema"`
	Approval           rootPassiveHostApproval      `json:"approval"`
	PublicKey          string                       `json:"independent_public_key"`
	Custody            bootstrapChainReadinessState `json:"original_preparation_custody"`
	PolicyHash         string                       `json:"policy_hash"`
	HighWaterAt        time.Time                    `json:"high_water_at"`
	Operations         uint32                       `json:"operations"`
	InstallIntent      bool                         `json:"install_intent"`
	Installed          bool                         `json:"installed"`
	Observation        *rootPassiveHostObservation  `json:"last_read_only_observation,omitempty"`
	StartObservation   *rootPassiveHostObservation  `json:"start_read_only_observation,omitempty"`
	StartAt            time.Time                    `json:"start_consumed_at"`
	StartMonotonicUsec uint64                       `json:"start_consumed_monotonic_usec"`
	Generation         *repairValidatorGeneration   `json:"acknowledged_generation,omitempty"`
	Status             string                       `json:"status"`
	ContentHash        string                       `json:"content_hash"`
}

type rootPassiveHostStore struct {
	*repairValidatorFileOwner
	approval rootPassiveHostApproval
	key      string
	custody  *bootstrapChainReadinessState
}

// The host journal cannot authorize an effect after original preparation loses
// its borrowed physical markers, even when their replacement bytes match.
func (self *rootPassiveHostStore) validateOwner() error {
	if err := self.custody.checkpoint(context.Background()); err != nil {
		return err
	}
	return self.repairValidatorFileOwner.validateOwner()
}

// The host store owns the borrowed preparation until its own effects finish.
func (self *rootPassiveHostStore) close() error {
	return errors.Join(self.repairValidatorFileOwner.close(), self.custody.close())
}

func (self rootPassiveHostRecord) validate(approval rootPassiveHostApproval, key string) error {
	hash := self.ContentHash
	self.ContentHash = ""
	if self.Schema != rootPassiveHostSchema || rootObjectHash(self.Approval) != rootObjectHash(approval) || self.PublicKey != key || hash != rootObjectHash(self) ||
		!self.Custody.validRootSeals() || self.Custody.RootStrategy != rootPassiveStrategy || !planSha256(self.Custody.PreparationHash) || !planSha256(self.Custody.ContractsHash) ||
		!planSha256(self.PolicyHash) || self.HighWaterAt.IsZero() || self.Operations > approval.Plan.MaximumOperations ||
		self.Installed && !self.InstallIntent || self.InstallIntent && self.Operations == 0 || self.StartAt.IsZero() != (self.StartMonotonicUsec == 0) ||
		self.StartAt.IsZero() && (self.Generation != nil || self.StartObservation != nil) || !self.StartAt.IsZero() && (!self.Installed || self.StartObservation == nil || self.StartAt.After(self.HighWaterAt)) {
		return errors.New("passive host journal lost authority, preparation or consumed-start history")
	}
	for _, observation := range []*rootPassiveHostObservation{self.Observation, self.StartObservation} {
		if observation != nil && (observation.ObservedAt.IsZero() || observation.ObservedAt.After(self.HighWaterAt) || observation.Root.PolicyHash != self.PolicyHash || !planSha256(observation.Root.ContentHash) || !validHash(observation.Root.FinalizedHash) || observation.Root.FinalizedNumber == 0 || !observation.Root.ReadOnlyReady || observation.Root.ActivationReady) {
			return errors.New("passive host read-only observation differs")
		}
	}
	if self.StartObservation != nil && (self.StartObservation.ObservedAt.After(self.StartAt) || self.StartAt.Sub(self.StartObservation.ObservedAt) > time.Duration(approval.Plan.MaximumSampleAgeSeconds)*time.Second) {
		return errors.New("passive host start has no fresh original observation")
	}
	if self.Generation != nil && (!repairValidatorHex(self.Generation.InvocationId, 16) || self.Generation.Pid <= 1 || self.Generation.StartedUsec < self.StartMonotonicUsec) {
		return errors.New("passive host acknowledged generation differs")
	}
	switch self.Status {
	case "claimed", "operation-reserved", "installing", "installed", "admitted-read-only", "start-consumed", "uncertain-consumed-start", "acknowledged-running", "acknowledged-stopped", "source-refused", "generation-changed", "approval-window-closed", "clock-rollback", "operation-limit":
	default:
		return errors.New("passive host journal disposition is unknown")
	}
	if (self.Status == "acknowledged-running" || self.Status == "acknowledged-stopped") && self.Generation == nil {
		return errors.New("passive host disposition has no acknowledged invocation")
	}
	return nil
}

func openRootPassiveHostStore(ctx context.Context, approval rootPassiveHostApproval, key string, create bool, now time.Time, custody *bootstrapChainReadinessState, policyHash string) (*rootPassiveHostStore, error) {
	if now.IsZero() {
		return nil, errors.New("passive host clock unavailable")
	}
	if err := approval.validate(key); err != nil {
		return nil, err
	}
	if err := custody.checkpoint(ctx); err != nil {
		return nil, err
	}
	owner, err := openRepairValidatorFileOwner(ctx, approval.Plan.StatePath, rootObjectHash(approval)+" "+key+"\n", create)
	if err != nil {
		return nil, err
	}
	self := &rootPassiveHostStore{repairValidatorFileOwner: owner, approval: approval, key: key, custody: custody}
	if create {
		err = self.save(rootPassiveHostRecord{Schema: rootPassiveHostSchema, Approval: approval, PublicKey: key, Custody: *custody, PolicyHash: policyHash, HighWaterAt: now, Status: "claimed"})
	} else {
		var record rootPassiveHostRecord
		record, err = self.load(ctx)
		if err == nil && (record.PolicyHash != policyHash || record.Custody.PreparationHash != custody.PreparationHash || record.Custody.RootProgressHash != custody.RootProgressHash || record.Custody.RootServiceHash != custody.RootServiceHash) {
			err = errors.New("passive host retained preparation or policy changed")
		}
	}
	if err != nil {
		return nil, errors.Join(err, self.close())
	}
	return self, nil
}

func (self *rootPassiveHostStore) load(ctx context.Context) (rootPassiveHostRecord, error) {
	var record rootPassiveHostRecord
	if err := self.validateOwner(); err != nil {
		return record, err
	}
	raw, _, err := self.storage.readFile(ctx, self.path, 64*1024)
	if err != nil {
		return record, err
	}
	if err = decodePlanJson(raw, &record); err != nil {
		return record, err
	}
	if err = record.validate(self.approval, self.key); err != nil {
		return record, err
	}
	self.expectedHash = monitorReadDigest(raw)
	return record, self.validateOwner()
}

func (self *rootPassiveHostStore) save(record rootPassiveHostRecord) error {
	if err := self.validateOwner(); err != nil {
		return err
	}
	record.ContentHash = ""
	record.ContentHash = rootObjectHash(record)
	if err := record.validate(self.approval, self.key); err != nil {
		return err
	}
	raw, err := json.Marshal(record)
	if err != nil || len(raw) > 64*1024-1 {
		return errors.New("passive host journal exceeds its bound")
	}
	raw = append(raw, '\n')
	if err := self.storage.publish(self.path, raw, self.syncDirectory); err != nil {
		if mainnetDurableAdmissionPending(err) {
			return err
		}
		self.poisoned = errors.Join(errors.New("passive host publication is ambiguous; reopen required"), err)
		return self.poisoned
	}
	self.expectedHash = monitorReadDigest(raw)
	return self.validateOwner()
}
