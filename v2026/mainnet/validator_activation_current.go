// Initial current admission composes live readers under a separately accepted
// policy. No retained projection or user-supplied observer is a public capability.
package main

import (
	"context"
	"errors"
	"slices"
	"time"
)

// The original signed acceptance remains immutable after its first completed
// admission. Reopening the process journal never renews authority or a start.
type validatorActivationCurrentRetained struct {
	Approval  validatorActivationCurrentApproval `json:"approval"`
	PublicKey string                             `json:"independent_public_key"`
	Reference planFileReference                  `json:"approval_file"`
}

// This bounded seal names a freshly authenticated original installation and
// the exact shared current snapshot. It is not an importable capability.
type validatorActivationInstallationObservation struct {
	InstallationHash         string    `json:"installation_identity_hash"`
	PreparationHash          string    `json:"bootstrap_plan_hash"`
	ContractPlanHash         string    `json:"contract_plan_hash"`
	ObservationHash          string    `json:"installation_observation_hash"`
	NativeNumber             uint64    `json:"native_number"`
	NativeHash               string    `json:"native_hash"`
	EvmNumber                uint64    `json:"evm_number"`
	EvmHash                  string    `json:"evm_hash"`
	EarliestOriginalEvmBlock uint64    `json:"earliest_original_evm_block"`
	DeclaredScanFloors       [2]uint64 `json:"declared_scan_floors"`
}

// Unresolved economic success is deliberately separate from current admission.
// The policy attests external custody; it cannot prove a distributed fence.
type validatorActivationCurrentObservation struct {
	Schema        string                                     `json:"schema"`
	AuthorityHash string                                     `json:"authority_hash"`
	DomainHash    string                                     `json:"current_domains_hash"`
	Installation  validatorActivationInstallationObservation `json:"installation"`
	ContentHash   string                                     `json:"content_hash"`
}

// Only the production constructor selects these concrete read-only functions.
// Per-instance seams let synthetic tests drive failures without global hooks.
type validatorActivationCurrentAuthority struct {
	retained     validatorActivationCurrentRetained
	observe      func(context.Context, *validatorActivationStore, *validatorActivationHost, *validatorActivationRecord, func() time.Time, int) (*validatorActivationReadiness, error)
	installation func(context.Context, *validatorActivationReadiness) (validatorActivationInstallationObservation, error)
	custody      func(context.Context) error
	close        func() error
}

// The legacy port cannot bypass the richer composition or durable policy seal.
func (self *validatorActivationCurrentAuthority) authorizeActivation(context.Context, validatorActivationApproval, bootstrapChainPreparation, bootstrapChainReadiness, int) error {
	return errors.New("validator current admission requires the complete owned observation path")
}

// Every source is pinned before networking and rechecked after it. Missing or
// changed review bytes are refusals, even if a retained signature remains valid.
func (self *validatorActivationCurrentAuthority) checkpoint(ctx context.Context, store *validatorActivationStore, record validatorActivationRecord, now time.Time) error {
	if ctx == nil || self == nil || self.observe == nil || self.installation == nil || store == nil {
		return errors.New("validator current admission owner is unavailable")
	}
	if err := errors.Join(store.validateOwner(), self.retained.Approval.window(ctx, record.HighWaterAt, now)); err != nil {
		return err
	}
	if self.custody != nil {
		if err := self.custody(ctx); err != nil {
			return err
		}
	}
	retainedRecord, err := store.load(ctx)
	record.ContentHash, retainedRecord.ContentHash = "", ""
	if err != nil || rootObjectHash(record) != rootObjectHash(retainedRecord) {
		store.poisoned = errors.Join(errors.New("validator current admission original counted journal changed"), err)
		return store.poisoned
	}
	preparation, err := loadValidatorActivationPreparation(ctx, store.approval)
	if err != nil {
		return err
	}
	if err := self.retained.Approval.validate(store.approval, store.publicKey, self.retained.PublicKey, preparation); err != nil {
		return err
	}
	if record.CurrentAuthority != nil && rootObjectHash(*record.CurrentAuthority) != rootObjectHash(self.retained) {
		return errors.New("validator current admission changed retained independent authority")
	}
	for _, file := range append(self.retained.Approval.Authorization.references(), self.retained.Reference) {
		raw, hash, err := readPlanFile(ctx, file.Path, 1024*1024)
		if err != nil || len(raw) == 0 || hash != file.Sha256 {
			return errors.Join(errors.New("validator current admission source was lost or changed"), err)
		}
	}
	if self.custody != nil {
		return self.custody(ctx)
	}
	return ctx.Err()
}

// Both roles are re-read before each start and after its synced reservation.
// Completed checkpoints survive later refusal; observations never refund claims.
func (self *validatorActivationCurrentAuthority) admit(ctx context.Context, store *validatorActivationStore, host *validatorActivationHost, record *validatorActivationRecord, now func() time.Time, pending int) (*validatorActivationReadiness, error) {
	if pending != -1 && !validatorActivationCurrentPending(*record, pending) {
		return nil, errors.New("validator current admission cannot reclassify an uncertain consumed start")
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(store.approval.Plan.Route.ReadRetrySeconds)*time.Second)
	defer cancel()
	if err := self.checkpoint(ctx, store, *record, now()); err != nil {
		return nil, err
	}
	result, err := self.observe(ctx, store, host, record, now, pending)
	if err != nil || result == nil {
		return nil, errors.Join(errors.New("validator current admission lacks complete live health"), err)
	}
	if err := validatorActivationCurrentDomains(store.approval.Plan, *record, *result, pending); err != nil {
		return nil, err
	}
	for i, unit := range record.Units {
		if unit.StartAt.IsZero() || i == pending && validatorActivationCurrentPending(*record, pending) {
			if err := host.absentCurrentProgress(ctx, store.approval.Plan.Units[i].Unit); err != nil {
				return nil, err
			}
		}
	}
	installation, err := self.installation(ctx, result)
	if err != nil {
		return nil, err
	}
	value := &validatorActivationCurrentObservation{Schema: validatorActivationCurrentSchema, AuthorityHash: rootObjectHash(self.retained.Approval), DomainHash: rootObjectHash(*result), Installation: installation}
	value.ContentHash = rootObjectHash(*value)
	result.Current = value
	if err := errors.Join(value.validate(store.approval.Plan, *result, self.retained), self.checkpoint(ctx, store, *record, now()), validatorActivationWindow(ctx, store.approval.Plan, record.HighWaterAt, now(), result)); err != nil {
		return nil, err
	}
	if record.CurrentAuthority == nil {
		retained := self.retained
		record.CurrentAuthority = &retained
		if err := store.save(*record); err != nil {
			return nil, err
		}
	}
	return result, nil
}

// Shape validation on reopen preserves exact observation/approval binding.
// The fresh producer owns source authentication; checksums supply no authority.
func (self validatorActivationCurrentObservation) validate(plan validatorActivationPlan, readiness validatorActivationReadiness, retained validatorActivationCurrentRetained) error {
	hash := self.ContentHash
	self.ContentHash = ""
	p, installation := retained.Approval.Authorization, self.Installation
	readiness.Current = nil
	if self.Schema != validatorActivationCurrentSchema || !planSha256(hash) || hash != rootObjectHash(self) || self.AuthorityHash != rootObjectHash(retained.Approval) ||
		self.DomainHash != rootObjectHash(readiness) ||
		installation.EarliestOriginalEvmBlock == 0 || installation.EarliestOriginalEvmBlock > installation.EvmNumber || installation.DeclaredScanFloors[0] == 0 || installation.DeclaredScanFloors[1] == 0 ||
		installation.DeclaredScanFloors[0] > installation.EarliestOriginalEvmBlock || installation.DeclaredScanFloors[1] > installation.EarliestOriginalEvmBlock ||
		installation.InstallationHash != p.Installation.InstallationHash || installation.PreparationHash != plan.PlanHash || !planSha256(installation.ObservationHash) ||
		readiness.Production == nil || readiness.Health == nil || readiness.Health.Committed == nil || readiness.Stake == nil ||
		installation.ContractPlanHash != readiness.Production.ContractPlanHash || installation.NativeNumber != readiness.FinalizedNumber || installation.NativeHash != readiness.FinalizedHash ||
		installation.EvmNumber != readiness.Production.EvmBlock || installation.EvmHash != readiness.Production.EvmHash {
		return errors.New("validator current admission lost its exact installation or shared finalized snapshot")
	}
	return validatorActivationCurrentProofs(readiness)
}

// Only complete empty tails are in scope. An absent worker is accepted only
// before its own first start, or this same owner's not-yet-issued reservation.
func validatorActivationCurrentDomains(plan validatorActivationPlan, record validatorActivationRecord, readiness validatorActivationReadiness, pending int) error {
	if err := readiness.validate(plan); err != nil {
		return err
	}
	if err := validatorActivationCurrentProofs(readiness); err != nil {
		return err
	}
	for i := range readiness.Health.Committed {
		worker, unit := readiness.Health.Workers[i], record.Units[i]
		fresh := unit.StartAt.IsZero() || i == pending && validatorActivationCurrentPending(record, pending)
		if fresh {
			if worker.Record != nil || worker.Current || !slices.Contains(worker.Warnings, "WORKER_NOT_STARTED") && !slices.Contains(worker.Warnings, "PROGRESS_UNAVAILABLE") {
				return errors.New("validator current admission cannot treat existing progress as an unstarted worker")
			}
		} else if !worker.Current || worker.Record == nil || worker.Record.Steering == nil || worker.Record.Steering.Outcome == "starting" || worker.Record.Steering.Outcome == "hard_error" ||
			worker.Record.Publisher.Outcome != "published" || worker.ObservedAt.Sub(monitorProgressTime(worker.Record.Steering.ObservedAt)) > time.Duration(plan.MaximumSampleAgeSeconds)*time.Second {
			return errors.New("validator current admission requires responsive acknowledged prior worker")
		}
	}
	return nil
}

// Journal reopening preserves the finite initial-launch scope independently of
// an outer rehash. The actual source reader refuses all nonempty intent graphs.
func validatorActivationCurrentProofs(readiness validatorActivationReadiness) error {
	if readiness.Production == nil || readiness.Stake == nil || readiness.Health == nil || readiness.Health.Committed == nil || !readiness.Stake.CapacityAdmitted {
		return errors.New("validator current admission requires complete operator, proof and conservative majority capacity")
	}
	for _, committed := range readiness.Health.Committed {
		inventory := committed.Proof.Unsealed
		if inventory == nil || inventory.Validate(committed.Proof.Prefixes) != nil || len(inventory.Ledgers) != 2 {
			return errors.New("validator current admission lacks complete ledger and empty-intent observation")
		}
		for _, ledger := range inventory.Ledgers {
			if ledger.TailBoundaryProof == nil || ledger.UnsealedRecords != 0 || ledger.PendingTrails != 0 {
				return errors.New("validator initial current admission requires complete empty authenticated tails; unfinished work needs separate recovery authority")
			}
		}
	}
	return nil
}
