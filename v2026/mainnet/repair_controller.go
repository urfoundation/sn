// A finite controller consumes only separately signed incident envelopes. Its
// durable census never replaces their original generation, expiry or counters.
package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/urnetwork/connect/v2026/durablevolume"
)

const repairControllerSchema = "urnetwork-mainnet-repair-controller-v1"

var errRepairControllerPending = errors.New("repair controller original incident is pending")
var errRepairControllerHeld = errors.New("repair controller envelope is held")

// The key is supplied independently of each signed envelope. No private key,
// blanket action policy or future incident generator is accepted here.
type repairControllerEntry struct {
	Id        string            `json:"id"`
	Kind      string            `json:"kind"`
	Approval  planFileReference `json:"approval"`
	PublicKey string            `json:"independent_public_key"`
}

// The complete original manifest is an immutable launch input.
type repairControllerManifest struct {
	Schema          string                  `json:"schema"`
	MaximumParallel int                     `json:"maximum_parallel"`
	Entries         []repairControllerEntry `json:"entries"`
}

// Attempted is published before a claim can acquire any original allowance.
// An interrupted intent with no journal remains held, never fresh authority.
type repairControllerEntryState struct {
	Id           string    `json:"id"`
	ApprovalHash string    `json:"approval_sha256"`
	Attempted    bool      `json:"claim_attempted"`
	Status       string    `json:"status"`
	Disposition  string    `json:"original_disposition"`
	Cause        string    `json:"cause"`
	ObservedAt   time.Time `json:"observed_at"`
}

// Outcomes describe local process recovery only; completion is not chain health.
type repairControllerRecord struct {
	Schema       string                       `json:"schema"`
	ManifestHash string                       `json:"manifest_sha256"`
	HighWaterAt  time.Time                    `json:"high_water_at"`
	Entries      []repairControllerEntryState `json:"entries"`
	ContentHash  string                       `json:"content_hash"`
}

// A private callback permits deterministic scheduler tests without replacing
// the public signature, host custody or service-manager adapters.
type repairControllerStep func(context.Context, repairControllerEntry, bool, func() error) (string, bool, error)

// The state lock serializes all checkpoint publications, never host calls.
// Each unit lock spans its complete joined operation; workers are bounded.
type repairController struct {
	manifest  repairControllerManifest
	record    repairControllerRecord
	stateLock sync.Mutex
	unitLocks []*sync.Mutex
	step      repairControllerStep
	save      func(repairControllerRecord) error
	now       func() time.Time
}

// Finite unique entries cannot select another command or a mutable key source.
func (self repairControllerManifest) validate() error {
	if self.Schema != repairControllerSchema || self.MaximumParallel < 1 || self.MaximumParallel > 4 || len(self.Entries) < 1 || len(self.Entries) > 64 {
		return errors.New("repair controller requires 1..64 envelopes and 1..4 workers")
	}
	ids := map[string]bool{}
	for _, entry := range self.Entries {
		if len(entry.Id) < 1 || len(entry.Id) > 64 || ids[entry.Id] || entry.Kind != "validator" && entry.Kind != "active-validator" && entry.Kind != "root-passive" && entry.Kind != "operator" || !repairValidatorPath(entry.Approval.Path) || !validMonitorReadDigest(entry.Approval.Sha256) || !rootCanonicalHash(entry.PublicKey) {
			return errors.New("repair controller entry identity, kind or independent pin differs")
		}
		for _, character := range entry.Id {
			if character != '-' && character != '_' && (character < 'a' || character > 'z') && (character < '0' || character > '9') {
				return errors.New("repair controller entry id is not canonical")
			}
		}
		ids[entry.Id] = true
	}
	return nil
}

// The exact original ordered census and checksum survive every reopen.
func (self repairControllerRecord) validate(manifest repairControllerManifest, hash string) error {
	expected := self.ContentHash
	self.ContentHash = ""
	if self.Schema != repairControllerSchema || self.ManifestHash != hash || self.HighWaterAt.IsZero() || len(self.Entries) != len(manifest.Entries) || expected != rootObjectHash(self) {
		return errors.Join(errRpcIntegrity, errors.New("repair controller checkpoint census or checksum differs"))
	}
	for index, state := range self.Entries {
		entry := manifest.Entries[index]
		if state.Id != entry.Id || state.ApprovalHash != entry.Approval.Sha256 || state.ObservedAt.IsZero() || state.ObservedAt.After(self.HighWaterAt) || len(state.Disposition) > 96 {
			return errors.Join(errRpcIntegrity, errors.New("repair controller retained envelope differs"))
		}
		switch state.Status {
		case "pending", "held":
		case "active", "completed":
			if !state.Attempted {
				return errors.Join(errRpcIntegrity, errors.New("repair controller lost its claim intent"))
			}
		default:
			return errors.Join(errRpcIntegrity, errors.New("repair controller disposition is unknown"))
		}
		switch state.Cause {
		case "", "pending", "unavailable", "cancelled", "integrity", "held":
		default:
			return errors.Join(errRpcIntegrity, errors.New("repair controller cause is unknown"))
		}
	}
	return nil
}

// Typed custody and publication failures dominate availability in mixed errors.
func repairControllerCause(err error) (string, string) {
	if err == nil {
		return "pending", ""
	}
	if errors.Is(err, durablevolume.ErrIdentity) || errors.Is(err, errRpcIntegrity) || errors.Is(err, errMainnetDurablePublicationUncertain) {
		return "held", "integrity"
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return "pending", "cancelled"
	}
	if errors.Is(err, errRepairControllerPending) || errors.Is(err, errRepairProcessPending) {
		return "pending", "pending"
	}
	if repairValidatorObservationPending(err) {
		return "pending", "unavailable"
	}
	return "held", "held"
}

// Before returning, every worker and its service-manager child has joined.
// One envelope's pending or held result does not cancel other envelopes.
func (self *repairController) cycle(ctx context.Context) error {
	owner, cancel := context.WithCancel(ctx)
	defer cancel()
	stamp := self.now()
	if stamp.IsZero() || stamp.Before(self.record.HighWaterAt) {
		return errors.Join(errRpcIntegrity, errors.New("repair controller clock moved backwards"))
	}
	var workers sync.WaitGroup
	var fatal error
	next := 0
	for range self.manifest.MaximumParallel {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for {
				self.stateLock.Lock()
				if owner.Err() != nil || next == len(self.manifest.Entries) {
					self.stateLock.Unlock()
					return
				}
				index := next
				next++
				state := self.record.Entries[index]
				self.stateLock.Unlock()
				if state.Status == "held" || state.Status == "completed" {
					continue
				}
				persist := func(change func(*repairControllerEntryState)) error {
					self.stateLock.Lock()
					defer self.stateLock.Unlock()
					if owner.Err() != nil {
						return owner.Err()
					}
					current := self.now()
					if current.IsZero() || current.Before(self.record.HighWaterAt) {
						fatal = errors.Join(fatal, errRpcIntegrity, errors.New("repair controller clock changed during an operation"))
						cancel()
						return fatal
					}
					change(&self.record.Entries[index])
					self.record.Entries[index].ObservedAt = current
					self.record.HighWaterAt = current
					self.record.ContentHash = ""
					self.record.ContentHash = rootObjectHash(self.record)
					if err := self.save(self.record); err != nil {
						fatal = errors.Join(fatal, err)
						cancel()
						return err
					}
					return nil
				}
				if !self.unitLocks[index].TryLock() {
					_ = persist(func(value *repairControllerEntryState) {
						value.Status, value.Cause, value.Disposition = "pending", "pending", "original-unit-busy"
					})
					continue
				}
				if owner.Err() != nil {
					self.unitLocks[index].Unlock()
					return
				}
				operation, stop := context.WithTimeout(owner, 10*time.Minute)
				disposition, complete, err := self.step(operation, self.manifest.Entries[index], state.Attempted, func() error {
					return persist(func(value *repairControllerEntryState) {
						value.Attempted = true
						value.Status = "active"
						value.Cause = ""
						value.Disposition = "claim-intent-durable"
					})
				})
				stop()
				self.unitLocks[index].Unlock()
				status, cause := repairControllerCause(err)
				if err == nil && complete {
					status = "completed"
				}
				_ = persist(func(value *repairControllerEntryState) {
					value.Status, value.Cause, value.Disposition = status, cause, disposition
				})
			}
		}()
	}
	workers.Wait()
	if err := errors.Join(fatal, ctx.Err()); err != nil {
		return err
	}
	stamp = self.now()
	if stamp.Before(self.record.HighWaterAt) {
		return errors.Join(errRpcIntegrity, errors.New("repair controller cycle clock moved backwards"))
	}
	self.record.HighWaterAt = stamp
	self.record.ContentHash = ""
	self.record.ContentHash = rootObjectHash(self.record)
	return self.save(self.record)
}

// All samples are scalars without incident, key, service or path labels.
func (self repairControllerRecord) metrics() []byte {
	counts := map[string]int{"pending": 0, "held": 0, "completed": 0, "active": 0}
	for _, entry := range self.Entries {
		counts[entry.Status]++
	}
	return []byte(fmt.Sprintf("sn_mainnet_repair_controller_sample_timestamp_seconds %d\nsn_mainnet_repair_controller_pending %d\nsn_mainnet_repair_controller_held %d\nsn_mainnet_repair_controller_completed %d\nsn_mainnet_repair_controller_active %d\n", self.HighWaterAt.Unix(), counts["pending"], counts["held"], counts["completed"], counts["active"]))
}
