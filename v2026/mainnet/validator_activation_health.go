// Read-only health admission composes current native, executable, operator and
// stake observations with historical proof checkpoints and actual progress.
package main

import (
	"context"
	"errors"
	"os"
	"reflect"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/validator"
)

const validatorActivationHealthSchema = "urnetwork-mainnet-validator-prefix-health-v1"

// Completed checkpoints survive a later role's unavailable read. Their
// original timestamp is retained; they never satisfy fresh current admission.
type validatorActivationProofCheckpoint struct {
	ObservedAt time.Time                                      `json:"observed_at"`
	Proof      validator.ProductionBootstrapPrefixObservation `json:"proof"`
}

// The standard producer has no per-operator live worker attestation. Keep that
// limitation explicit even when its process and scheduler heartbeat are fresh.
type validatorActivationWorkerHealth struct {
	ObservedAt time.Time                   `json:"observed_at"`
	Record     *protocol.ValidatorProgress `json:"progress,omitempty"`
	Current    bool                        `json:"current"`
	Warnings   []string                    `json:"warnings"`
}

// This bounded projection is observational only. Original activation blockers,
// signer custody, current mutable prefixes and public start remain closed.
type validatorActivationHealthReadiness struct {
	Schema      string                                     `json:"schema"`
	Proofs      [2]validatorActivationProofCheckpoint      `json:"approved_prefixes"`
	Workers     [2]validatorActivationWorkerHealth         `json:"workers"`
	Committed   *[2]validatorActivationCommittedCheckpoint `json:"committed_prefixes,omitempty"`
	OpenGates   []string                                   `json:"open_gates"`
	ContentHash string                                     `json:"content_hash"`
}

// Fixed unresolved domains cannot be removed by a healthy heartbeat or a
// previously completed mathematical prefix. They are not runtime waivers.
func validatorActivationHealthOpenGates() []string {
	return []string{"CURRENT_MUTABLE_PROOF_PREFIX_UNVERIFIED", "PER_OPERATOR_LIVE_WORKER_UNVERIFIED", "GLOBAL_SIGNER_CUSTODY_UNVERIFIED", "APPLIED_WEIGHTS_INFLUENCE_UNVERIFIED", "SIGNED_LAUNCH_AUTHORITY_UNAVAILABLE"}
}

// Recheck exact domains when reopening custody; checksums alone are not scope.
func (self validatorActivationProofCheckpoint) validate(plan validatorActivationPlan, index int) error {
	p := self.Proof
	hash := p.ContentHash
	p.ContentHash = ""
	if index < 0 || index >= len(plan.Units) || self.ObservedAt.IsZero() || p.Schema != validator.ProductionBootstrapPrefixSchema || p.ConfigHash != plan.Units[index].Unit.Config.Sha256 ||
		!rootCanonicalHash(p.PolicyHash) || !planSha256(p.ClientDomainHash) || !planSha256(hash) || hash != rootObjectHash(p) ||
		!p.HistoricalSources || p.CurrentPrefixProven || p.Native.Block == 0 || p.Native.Hash == ([32]byte{}) || p.Native.Hotkey == ([32]byte{}) || p.EvmBlock == 0 || !rootCanonicalHash(p.EvmHash) || len(p.Prefixes) != 2 {
		return errors.New("validator proof checkpoint lacks its original bounded scope")
	}
	for i, prefix := range p.Prefixes {
		if prefix.NoId == 0 || i > 0 && prefix.NoId <= p.Prefixes[i-1].NoId || !rootCanonicalHash(prefix.ActivationHash) || !planSha256(prefix.HistoryHash) ||
			(prefix.LastSequence == 0 && (prefix.Root != "0x"+strings.Repeat("0", 64) || prefix.Generation != 1) || prefix.LastSequence > 0 && !rootCanonicalHash(prefix.Root)) ||
			prefix.Generation == 0 || !planSha256(prefix.PriorEmaHash) {
			return errors.New("validator proof checkpoint operator domain is incomplete")
		}
	}
	return nil
}

// Every component stays at the same current native/EVM point. Historical
// checkpoints retain separate clocks and cannot replace this current census.
func (self validatorActivationHealthReadiness) validate(plan validatorActivationPlan, readiness validatorActivationReadiness) error {
	hash := self.ContentHash
	self.ContentHash = ""
	gates := validatorActivationHealthOpenGates()
	if self.Committed != nil {
		gates = validatorActivationCommittedOpenGates()
	}
	if self.Schema != validatorActivationHealthSchema || !planSha256(hash) || hash != rootObjectHash(self) || readiness.Production == nil || readiness.Stake == nil ||
		!slices.Equal(self.OpenGates, gates) {
		return errors.New("validator health observation lacks its complete qualified composition")
	}
	for i, checkpoint := range self.Proofs {
		if err := checkpoint.validate(plan, i); err != nil {
			return err
		}
		observed := readiness.Production.Validators[i]
		if checkpoint.ObservedAt.Before(readiness.ObservedAt) || checkpoint.Proof.Native != observed.Native || checkpoint.Proof.EvmBlock != observed.EvmBlock || checkpoint.Proof.EvmHash != observed.EvmHash ||
			checkpoint.Proof.ClientDomainHash != rootObjectHash(observed.Operators) || len(observed.Operators) != 2 {
			return errors.New("validator proof checkpoint differs from the current operator observation")
		}
		for j, prefix := range checkpoint.Proof.Prefixes {
			if prefix.NoId != observed.Operators[j].NoId || prefix.ActivationHash != observed.Operators[j].ActivationHash || prefix.Epoch > observed.SettlementEpoch {
				return errors.New("validator historical prefix belongs to another current operator domain")
			}
		}
		if self.Committed != nil {
			current := self.Committed[i]
			if err := current.validate(plan, i); err != nil {
				return err
			}
			if current.ObservedAt.Before(checkpoint.ObservedAt) || current.Proof.ApprovedPrefixHash != checkpoint.Proof.ContentHash || current.Proof.Native != observed.Native ||
				current.Proof.EvmBlock != observed.EvmBlock || current.Proof.EvmHash != observed.EvmHash || current.Proof.ClientDomainHash != checkpoint.Proof.ClientDomainHash || current.Proof.PolicyHash != checkpoint.Proof.PolicyHash {
				return errors.New("validator committed checkpoint differs from its approved current observation")
			}
			for j, prefix := range current.Proof.Prefixes {
				original := checkpoint.Proof.Prefixes[j]
				if prefix.NoId != original.NoId || prefix.ActivationHash != original.ActivationHash || prefix.HistoryHash != original.HistoryHash || prefix.LastSequence < original.LastSequence || prefix.Epoch < original.Epoch || prefix.Epoch > observed.SettlementEpoch || prefix.Generation < original.Generation {
					return errors.New("validator committed checkpoint does not extend its original operator")
				}
			}
		}
		worker := self.Workers[i]
		if worker.ObservedAt.Before(readiness.ObservedAt) || len(worker.Warnings) == 0 || len(worker.Warnings) > 12 || worker.Current && worker.Record == nil ||
			worker.Warnings[0] != "PER_OPERATOR_LIVE_WORKER_UNVERIFIED" {
			return errors.New("validator worker observation is incomplete")
		}
		if worker.Record != nil {
			if err := worker.Record.Validate(); err != nil {
				return err
			}
			if worker.Record.Source != plan.Units[i].Source || monitorProgressMaximumTime(worker.Record).After(worker.ObservedAt) {
				return errors.New("validator worker source or clock differs")
			}
			if worker.Current && worker.ObservedAt.Sub(monitorProgressTime(worker.Record.HeartbeatAt)) > time.Duration(plan.MaximumSampleAgeSeconds)*time.Second {
				return errors.New("validator worker freshness is overstated")
			}
		}
	}
	return nil
}

// The original operation allowance and route deadline bound all stages. Each
// completed role is synced before observing the next, without issuing effects.
func observeValidatorActivationHealth(ctx context.Context, store *validatorActivationStore, host *validatorActivationHost, record *validatorActivationRecord, now func() time.Time) (*validatorActivationReadiness, error) {
	return observeValidatorActivationHealthScope(ctx, store, host, record, now, false)
}

// The additional scope authenticates service-owned committed histories. The
// existing health-only command retains its original observation and limits.
func observeValidatorActivationHealthScope(ctx context.Context, store *validatorActivationStore, host *validatorActivationHost, record *validatorActivationRecord, now func() time.Time, committed bool) (*validatorActivationReadiness, error) {
	return observeValidatorActivationHealthPending(ctx, store, host, record, now, committed, -1)
}

// A consumed but not yet issued start is read as stopped only by its synchronous
// current-authority owner. Checkpoint writes always preserve the consumed record.
func observeValidatorActivationHealthPending(ctx context.Context, store *validatorActivationStore, host *validatorActivationHost, record *validatorActivationRecord, now func() time.Time, committed bool, pending int) (*validatorActivationReadiness, error) {
	plan := store.approval.Plan
	ctx, cancel := context.WithTimeout(ctx, time.Duration(plan.Route.ReadRetrySeconds)*time.Second)
	defer cancel()
	preparation, readiness, result, err := observeValidatorActivationWithProduction(ctx, store.approval, now, true)
	if err != nil {
		return nil, err
	}
	client, err := newOwnedSubmissionClient(plan.Route)
	if err != nil {
		return nil, err
	}
	defer client.httpClient.CloseIdleConnections()
	result.Stake, err = client.observeValidatorActivationStake(ctx, preparation, readiness, result.Native)
	if err != nil {
		return nil, err
	}
	health := &validatorActivationHealthReadiness{Schema: validatorActivationHealthSchema, OpenGates: validatorActivationHealthOpenGates()}
	if committed {
		health.Committed = &[2]validatorActivationCommittedCheckpoint{}
		health.OpenGates = validatorActivationCommittedOpenGates()
	}
	for i := range plan.Units {
		file := preparation.Plan.Config.Validators[i].Config
		raw, err := readBootstrapChainInput(ctx, file, 2*1024*1024)
		if err != nil {
			return nil, err
		}
		proof, err := validator.ObserveProductionBootstrapPrefix(ctx, file.Path, raw, result.Production.Validators[i])
		if err != nil {
			return nil, err
		}
		checkpoint := validatorActivationProofCheckpoint{ObservedAt: now(), Proof: *proof}
		if err := retainValidatorActivationProofCheckpoint(ctx, store, record, i, checkpoint); err != nil {
			return nil, err
		}
		health.Proofs[i] = checkpoint
		if committed {
			current, err := observeValidatorActivationCommitted(ctx, store, record, i, file, raw, result.Production.Validators[i], *proof, now)
			if err != nil {
				return nil, err
			}
			health.Committed[i] = *current
		}
		var previous *validatorActivationWorkerHealth
		if record.Readiness != nil && record.Readiness.Health != nil {
			previous = &record.Readiness.Health.Workers[i]
		}
		health.Workers[i], err = host.observeValidatorActivationWorker(ctx, plan, validatorActivationPreStartRecord(*record, pending).Units[i], i, previous, now())
		if err != nil {
			return nil, err
		}
	}
	// Historic proof observation may take time. Recheck the exact canonical
	// current mapping and host generations before publishing a combined result.
	mapping, err := client.readFinalizedMappingAtIdentity(ctx, readiness.Census.Observation.Identity)
	if err != nil {
		return nil, err
	}
	if mapping.EvmHeader.Number != result.Production.EvmBlock || mapping.EvmHeader.Hash != result.Production.EvmHash {
		return nil, errors.New("validator health current mapping changed")
	}
	if err := host.admit(ctx, plan, validatorActivationPreStartRecord(*record, pending)); err != nil {
		return nil, err
	}
	health.ContentHash = rootObjectHash(*health)
	result.Health = health
	return result, errors.Join(result.validate(plan), ctx.Err())
}

// Publication preserves the exact completed prefix across unavailable later
// reads. Re-observation can update clocks/client responses, never its history.
func retainValidatorActivationProofCheckpoint(ctx context.Context, store *validatorActivationStore, record *validatorActivationRecord, index int, checkpoint validatorActivationProofCheckpoint) error {
	if err := errors.Join(ctx.Err(), checkpoint.validate(store.approval.Plan, index)); err != nil {
		return err
	}
	if checkpoint.ObservedAt.Before(record.HighWaterAt) {
		return errors.New("validator proof checkpoint clock moved backwards")
	}
	if len(record.ProofCheckpoints) == 0 {
		record.ProofCheckpoints = make([]*validatorActivationProofCheckpoint, 2)
	}
	if len(record.ProofCheckpoints) != 2 {
		return errors.New("validator proof checkpoint census differs")
	}
	if prior := record.ProofCheckpoints[index]; prior != nil && (checkpoint.ObservedAt.Before(prior.ObservedAt) || prior.Proof.PolicyHash != checkpoint.Proof.PolicyHash || !reflect.DeepEqual(prior.Proof.Prefixes, checkpoint.Proof.Prefixes)) {
		return errors.New("validator original completed proof prefix changed")
	}
	record.ProofCheckpoints[index] = &checkpoint
	record.HighWaterAt = checkpoint.ObservedAt
	return store.save(*record)
}

// Only protected real progress bytes can update a worker observation. Missing
// or stale status is a warning; aliases, owner/source/generation contradictions
// and semantic clock rewrites refuse. No progress age is reset on read failure.
func (self *validatorActivationHost) observeValidatorActivationWorker(ctx context.Context, plan validatorActivationPlan, unit validatorActivationUnitState, index int, previous *validatorActivationWorkerHealth, now time.Time) (validatorActivationWorkerHealth, error) {
	result := validatorActivationWorkerHealth{ObservedAt: now, Warnings: []string{"PER_OPERATOR_LIVE_WORKER_UNVERIFIED"}}
	if ctx == nil || self == nil || self.host == nil || index < 0 || index >= len(plan.Units) || now.IsZero() {
		return result, errors.New("validator health worker owner is absent")
	}
	if previous != nil {
		result.Record = previous.Record
		if now.Before(previous.ObservedAt) {
			return result, errors.New("validator health observation clock moved backwards")
		}
	}
	profile := plan.Units[index]
	raw, err := self.host.read(ctx, profile.Unit.ProgressFile, profile.Unit.Uid, protocol.MaxValidatorProgressBytes, false)
	if err != nil {
		if ctx.Err() != nil {
			return result, errors.Join(err, ctx.Err())
		}
		if validatorActivationWorkerReadUnavailable(err) {
			result.Warnings = append(result.Warnings, "PROGRESS_UNAVAILABLE")
			return result, nil
		}
		// A fresh service need not have created its state directory. Its
		// protected existing parent still has to belong to the approved host.
		if unit.StartAt.IsZero() {
			if _, absent := os.Lstat(profile.Unit.StateDirectory); errors.Is(absent, os.ErrNotExist) && self.host.parents(profile.Unit.StateDirectory, profile.Unit.Uid) == nil {
				result.Warnings = append(result.Warnings, "WORKER_NOT_STARTED")
				return result, nil
			}
		}
		return result, err
	}
	value, err := protocol.DecodeValidatorProgress(raw)
	if err != nil {
		return result, err
	}
	if value.Source != profile.Source || unit.StartAt.IsZero() || unit.Generation == nil || monitorProgressTime(value.StartedAt).Before(unit.StartAt) ||
		unit.Completed != nil && unit.Completed.InstanceId != value.InstanceId || monitorProgressMaximumTime(value).After(now) || previous != nil && previous.Record != nil && previous.Record.InstanceId != value.InstanceId {
		return result, errors.New("validator health progress source, generation or clock differs")
	}
	if prior := result.Record; prior != nil && (prior.StartedAt != value.StartedAt || monitorProgressTime(value.HeartbeatAt).Before(monitorProgressTime(prior.HeartbeatAt))) {
		return result, errors.New("validator health worker rewrote its original generation clocks")
	}
	continuity := monitorValidatorState{Record: result.Record}
	continuity.observe(unit.StartAt, now, value, "ok")
	if !continuity.readCurrent {
		return result, errors.New("validator health progress contradicts retained semantic clocks")
	}
	result.Record = value
	result.Current = now.Sub(monitorProgressTime(value.HeartbeatAt)) <= time.Duration(plan.MaximumSampleAgeSeconds)*time.Second
	if !result.Current {
		result.Warnings = append(result.Warnings, "PROGRESS_STALE")
	}
	if value.Publisher.Outcome != "published" {
		result.Warnings = append(result.Warnings, "PROGRESS_PUBLICATION_WAIT")
	}
	if value.Native == nil || !value.Native.Current || now.Sub(monitorProgressTime(value.Native.LastSuccessAt)) > time.Duration(plan.MaximumSampleAgeSeconds)*time.Second {
		result.Warnings = append(result.Warnings, "NATIVE_PROGRESS_UNAVAILABLE")
	}
	if value.Settlement == nil || !value.Settlement.Current || now.Sub(monitorProgressTime(value.Settlement.LastSuccessAt)) > time.Duration(plan.MaximumSampleAgeSeconds)*time.Second {
		result.Warnings = append(result.Warnings, "SETTLEMENT_PROGRESS_UNAVAILABLE")
	}
	if value.Steering != nil && value.Steering.Outcome == "hard_error" {
		return result, errors.New("validator worker reports a hard integrity error")
	}
	if value.Steering == nil || !value.Steering.Current || now.Sub(monitorProgressTime(value.Steering.LastSuccessAt)) > time.Duration(plan.MaximumSampleAgeSeconds)*time.Second {
		result.Warnings = append(result.Warnings, "STEERING_PROGRESS_UNAVAILABLE")
	} else if value.Steering.Outcome == "read_wait" || value.Steering.Outcome == "receipt_transport_wait" {
		result.Warnings = append(result.Warnings, "STEERING_TRANSPORT_WAIT")
	}
	return result, ctx.Err()
}

// The shared file reader classifies an ownership-hook error as unavailable.
// Only pure missing/transient operating-system causes are soft here; mixed
// ownership, close, alias or content failures cannot borrow that classification.
func validatorActivationWorkerReadUnavailable(err error) bool {
	if err == nil {
		return false
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		causes := joined.Unwrap()
		if len(causes) == 0 {
			return false
		}
		for _, cause := range causes {
			if !validatorActivationWorkerReadUnavailable(cause) {
				return false
			}
		}
		return true
	}
	if classified, ok := err.(*monitorServiceReadError); ok && classified.code != "missing" && classified.code != "unavailable" {
		return false
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return validatorActivationWorkerReadUnavailable(wrapped.Unwrap())
	}
	return err == syscall.ENOENT || err == syscall.EIO || err == syscall.EINTR || err == syscall.ESTALE || err == syscall.ETIMEDOUT
}
