// Active repair retains independently consumed stop and start cuts. An uncertain
// write or lost marker is never a reason to mint another command allowance.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// Acknowledged generation is written only after a returned start command. Join
// is a bounded observation of the original generation's empty descendant group.
type repairActiveValidatorRecord struct {
	Schema              string                        `json:"schema"`
	Approval            repairActiveValidatorApproval `json:"approval"`
	PublicKey           string                        `json:"independent_public_key"`
	HighWaterAt         time.Time                     `json:"high_water_at"`
	Observations        uint32                        `json:"observations"`
	StopAt              time.Time                     `json:"stop_consumed_at"`
	StopMonotonicUsec   uint64                        `json:"stop_consumed_monotonic_usec"`
	JoinedAt            time.Time                     `json:"joined_at"`
	JoinedMonotonicUsec uint64                        `json:"joined_monotonic_usec"`
	StartAt             time.Time                     `json:"start_consumed_at"`
	StartMonotonicUsec  uint64                        `json:"start_consumed_monotonic_usec"`
	Generation          *repairValidatorGeneration    `json:"acknowledged_generation,omitempty"`
	Completed           *repairValidatorPostcondition `json:"completed,omitempty"`
	Status              string                        `json:"status"`
	ContentHash         string                        `json:"content_hash"`
}

// Serial callers share the physical journal owner with stopped repair, without
// admitting that older signature domain as stop authority.
type repairActiveValidatorStore struct {
	*repairValidatorFileOwner
	approval  repairActiveValidatorApproval
	publicKey string
}

// Counters and chronological cuts cannot be refunded or reordered on reopen.
func (self repairActiveValidatorRecord) validate(approval repairActiveValidatorApproval, key string) error {
	hash := self.ContentHash
	self.ContentHash = ""
	p := approval.Plan.Process
	if self.Schema != repairActiveValidatorSchema || rootObjectHash(self.Approval) != rootObjectHash(approval) || self.PublicKey != key || hash != rootObjectHash(self) || self.HighWaterAt.IsZero() || self.Observations > p.MaximumObservations {
		return errors.New("active repair journal authority, count or checksum differs")
	}
	if self.StopAt.IsZero() != (self.StopMonotonicUsec == 0) || self.JoinedAt.IsZero() != (self.JoinedMonotonicUsec == 0) || self.StartAt.IsZero() != (self.StartMonotonicUsec == 0) || self.StopAt.IsZero() && (!self.JoinedAt.IsZero() || !self.StartAt.IsZero()) || self.JoinedAt.IsZero() && !self.StartAt.IsZero() || self.StartAt.IsZero() && (self.Generation != nil || self.Completed != nil) || self.Observations == 0 && !self.StopAt.IsZero() {
		return errors.New("active repair journal lost or reordered a consumed cut")
	}
	if !self.StopAt.IsZero() && (self.StopAt.Before(p.ValidFrom) || !self.StopAt.Before(p.ExpiresAt) || self.StopAt.After(self.HighWaterAt) || self.StopMonotonicUsec <= p.Previous.StartedUsec) {
		return errors.New("active repair stop cut is outside authority")
	}
	if !self.JoinedAt.IsZero() && (self.JoinedAt.Before(self.StopAt) || self.JoinedAt.After(self.HighWaterAt) || self.JoinedMonotonicUsec < self.StopMonotonicUsec || self.JoinedAt.Sub(self.StopAt) > time.Duration(approval.Plan.JoinWindowSeconds)*time.Second || self.JoinedMonotonicUsec-self.StopMonotonicUsec > uint64(approval.Plan.JoinWindowSeconds)*1000000) {
		return errors.New("active repair join cut is outside its bound")
	}
	if !self.StartAt.IsZero() && (self.StartAt.Before(self.JoinedAt) || self.StartAt.Before(p.ValidFrom) || !self.StartAt.Before(p.ExpiresAt) || self.StartAt.After(self.HighWaterAt) || self.StartMonotonicUsec < self.JoinedMonotonicUsec) {
		return errors.New("active repair start cut is outside authority")
	}
	if self.Generation != nil && (!repairValidatorHex(self.Generation.InvocationId, 16) || self.Generation.InvocationId == p.Previous.InvocationId || self.Generation.Pid <= 1 || self.Generation.StartedUsec <= p.Previous.StartedUsec || self.Generation.StartedUsec < self.StartMonotonicUsec) {
		return errors.New("active repair acknowledged generation differs")
	}
	if self.Completed != nil && (self.Generation == nil || self.Completed.ObservedAt.Before(self.StartAt) || self.Completed.ObservedAt.After(self.HighWaterAt) || !repairValidatorHex(self.Completed.InstanceId, 16) || self.Completed.InstanceId == p.Original.State.Record.InstanceId || !validMonitorReadDigest(self.Completed.RecordHash)) {
		return errors.New("active repair completion differs")
	}
	if (self.Completed != nil) != (self.Status == "responsive-generation-observed") {
		return errors.New("active repair completion disposition differs")
	}
	switch self.Status {
	case "claimed", "observation-reserved", "stop-consumed", "waiting-join", "join-observed", "start-consumed", "uncertain-consumed-start", "waiting-progress", "responsive-generation-observed", "approval-window-closed", "observation-limit", "generation-changed", "source-refused", "clock-rollback", "join-window-closed":
	default:
		return errors.New("active repair journal disposition is unknown")
	}
	return nil
}

// The marker binds an independently verified approval; claim failure preserves
// every artifact for manual reconciliation instead of deleting capacity history.
func openRepairActiveValidatorStore(ctx context.Context, approval repairActiveValidatorApproval, key string, create bool, now time.Time) (*repairActiveValidatorStore, error) {
	if ctx == nil || now.IsZero() {
		return nil, errors.New("active repair store context or clock unavailable")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := approval.validate(key); err != nil {
		return nil, err
	}
	owner, err := openRepairValidatorFileOwner(ctx, approval.Plan.Process.StatePath, rootObjectHash(approval)+" "+key+"\n", create)
	if err != nil {
		return nil, err
	}
	self := &repairActiveValidatorStore{repairValidatorFileOwner: owner, approval: approval, publicKey: key}
	if create {
		err = self.save(repairActiveValidatorRecord{Schema: repairActiveValidatorSchema, Approval: approval, PublicKey: key, HighWaterAt: now, Status: "claimed"})
	} else {
		_, err = self.load(ctx)
	}
	if err != nil {
		return nil, errors.Join(err, self.close())
	}
	return self, nil
}

// Missing, altered or invalid state cannot become a fresh observation budget.
func (self *repairActiveValidatorStore) load(ctx context.Context) (repairActiveValidatorRecord, error) {
	var record repairActiveValidatorRecord
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

// Atomic publication may be ambiguous after rename; only a new owner can
// interpret retained bytes, and all already consumed effects stay consumed.
func (self *repairActiveValidatorStore) save(record repairActiveValidatorRecord) error {
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
		return errors.New("active repair journal exceeds its bound")
	}
	raw = append(raw, '\n')
	if err := self.storage.publish(self.path, raw, self.syncDirectory); err != nil {
		if mainnetDurableAdmissionPending(err) {
			return err
		}
		self.poisoned = errors.Join(errors.New("active repair publication is ambiguous; reopen required"), err)
		return self.poisoned
	}
	self.expectedHash = monitorReadDigest(raw)
	return nil
}
