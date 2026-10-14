// Committed protocol checkpoints use the original signed service uid and
// separate root custody scratch. No production start capability is supplied.
package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/urfoundation/sn/v2026/validator"
)

// Earlier completed roles remain durable when a later source is unavailable.
// Committed cuts and the separately scoped unsealed inventory remain distinct.
// Neither is a live worker or complete steering-intent attestation.
type validatorActivationCommittedCheckpoint struct {
	ObservedAt time.Time                                         `json:"observed_at"`
	Proof      validator.ProductionBootstrapCommittedObservation `json:"proof"`
}

// The remaining domains are explicit, not a positional rewrite of another
// scope's list. Complete committed cuts do not prove unsealed producer work.
func validatorActivationCommittedOpenGates() []string {
	return []string{"UNSEALED_LEDGER_AND_INTENT_STATE_UNVERIFIED", "PER_OPERATOR_LIVE_WORKER_UNVERIFIED", "GLOBAL_SIGNER_CUSTODY_UNVERIFIED", "APPLIED_WEIGHTS_INFLUENCE_UNVERIFIED", "SIGNED_LAUNCH_AUTHORITY_UNAVAILABLE"}
}

// Original config and service identity are checked again on journal reopen.
func (self validatorActivationCommittedCheckpoint) validate(plan validatorActivationPlan, index int) error {
	p := self.Proof
	hash := p.ContentHash
	p.ContentHash = ""
	if index < 0 || index >= len(plan.Units) || self.ObservedAt.IsZero() || p.Schema != validator.ProductionBootstrapCommittedSchema || p.ConfigHash != plan.Units[index].Unit.Config.Sha256 || p.ServiceUid != plan.Units[index].Unit.Uid ||
		!planSha256(hash) || hash != rootObjectHash(p) || !rootCanonicalHash(p.PolicyHash) || !planSha256(p.ClientDomainHash) || !planSha256(p.ApprovedPrefixHash) || !planSha256(p.CensusHash) ||
		p.PreviousHash != "" && !planSha256(p.PreviousHash) || !p.HistoricalSources || p.SourceCount < 12 || p.SourceCount > 8192 || len(p.Prefixes) != 2 ||
		p.Native.Block == 0 || p.Native.Hash == ([32]byte{}) || p.Native.Hotkey == ([32]byte{}) || p.EvmBlock == 0 || !rootCanonicalHash(p.EvmHash) {
		return errors.New("validator committed checkpoint lacks its original bounded service scope")
	}
	for i, prefix := range p.Prefixes {
		if prefix.NoId == 0 || i > 0 && prefix.NoId <= p.Prefixes[i-1].NoId || !rootCanonicalHash(prefix.ActivationHash) || !planSha256(prefix.HistoryHash) || !planSha256(prefix.PriorEmaHash) || prefix.Generation == 0 ||
			prefix.LastSequence == 0 && prefix.Root != "0x"+strings.Repeat("0", 64) || prefix.LastSequence > 0 && !rootCanonicalHash(prefix.Root) {
			return errors.New("validator committed checkpoint operator scope differs")
		}
	}
	if p.Unsealed != nil {
		if err := p.Unsealed.Validate(p.Prefixes); err != nil {
			return err
		}
		for _, ledger := range p.Unsealed.Ledgers {
			if ledger.TailBoundaryProof != nil {
				for _, member := range ledger.TailBoundaryProof.Boundaries {
					if member.Boundary.EVMBlock > p.EvmBlock || member.Boundary.EVMBlock == p.EvmBlock && member.Boundary.EVMBlockHash != p.EvmHash {
						return errors.New("validator unsealed tail boundary exceeds its observed chain point")
					}
				}
			}
		}
	}
	return nil
}

// Fresh private scratch is always outside the producer namespace. Removing
// it is permitted only while its exact initial inode and private mode remain.
func observeValidatorActivationCommitted(ctx context.Context, store *validatorActivationStore, record *validatorActivationRecord, index int, file planFileReference, raw []byte, observed validator.ProductionBootstrapObservation, approved validator.ProductionBootstrapPrefixObservation, now func() time.Time) (result *validatorActivationCommittedCheckpoint, resultErr error) {
	root, err := os.MkdirTemp(filepath.Dir(store.path), ".validator-committed-")
	if err != nil {
		return nil, err
	}
	anchor, err := os.Lstat(root)
	if err != nil {
		return nil, err
	}
	defer func() {
		current, err := os.Lstat(root)
		if err != nil || !os.SameFile(anchor, current) || current.Mode() != anchor.Mode() {
			resultErr = errors.Join(resultErr, errors.New("validator committed scratch ownership changed"), err)
		} else {
			resultErr = errors.Join(resultErr, os.RemoveAll(root))
		}
		if resultErr != nil {
			result = nil
		}
	}()
	var previous *validator.ProductionBootstrapCommittedObservation
	if len(record.CommittedCheckpoints) == 2 && record.CommittedCheckpoints[index] != nil {
		previous = &record.CommittedCheckpoints[index].Proof
	}
	proof, err := validator.ObserveProductionBootstrapCommittedPrefix(ctx, file.Path, raw, observed, approved, previous, store.approval.Plan.Units[index].Unit.Uid, root)
	if err != nil {
		return nil, err
	}
	result = &validatorActivationCommittedCheckpoint{ObservedAt: now(), Proof: *proof}
	if err := retainValidatorActivationCommittedCheckpoint(ctx, store, record, index, *result); err != nil {
		return nil, err
	}
	return result, nil
}

// The replay proves prefix ancestry. Custody independently refuses clock or
// chain regression and an attempt to replace rather than extend the journal.
func retainValidatorActivationCommittedCheckpoint(ctx context.Context, store *validatorActivationStore, record *validatorActivationRecord, index int, checkpoint validatorActivationCommittedCheckpoint) error {
	if err := errors.Join(ctx.Err(), checkpoint.validate(store.approval.Plan, index)); err != nil {
		return err
	}
	if checkpoint.ObservedAt.Before(record.HighWaterAt) {
		return errors.New("validator committed checkpoint clock moved backwards")
	}
	if len(record.CommittedCheckpoints) == 0 {
		record.CommittedCheckpoints = make([]*validatorActivationCommittedCheckpoint, 2)
	}
	if len(record.CommittedCheckpoints) != 2 {
		return errors.New("validator committed checkpoint census differs")
	}
	p := checkpoint.Proof
	if prior := record.CommittedCheckpoints[index]; prior != nil {
		if checkpoint.ObservedAt.Before(prior.ObservedAt) || p.PreviousHash != prior.Proof.ContentHash || p.PolicyHash != prior.Proof.PolicyHash || p.Native.Hotkey != prior.Proof.Native.Hotkey || p.Native.Block < prior.Proof.Native.Block || p.Native.Epoch < prior.Proof.Native.Epoch || p.EvmBlock < prior.Proof.EvmBlock ||
			p.Native.Block == prior.Proof.Native.Block && p.Native.Hash != prior.Proof.Native.Hash || p.EvmBlock == prior.Proof.EvmBlock && p.EvmHash != prior.Proof.EvmHash {
			return errors.New("validator committed checkpoint continuity differs")
		}
		for i, prefix := range p.Prefixes {
			old := prior.Proof.Prefixes[i]
			if prefix.NoId != old.NoId || prefix.ActivationHash != old.ActivationHash || prefix.HistoryHash != old.HistoryHash || prefix.LastSequence < old.LastSequence || prefix.Generation < old.Generation || prefix.Epoch < old.Epoch || prefix.LastSequence == old.LastSequence && prefix.Root != old.Root || prefix.Epoch == old.Epoch && prefix.PriorEmaHash != old.PriorEmaHash {
				return errors.New("validator committed operator checkpoint regressed")
			}
		}
		if prior.Proof.Unsealed != nil {
			if p.Unsealed == nil || prior.Proof.Unsealed.IntentFilePresent && (!p.Unsealed.IntentFilePresent || p.Unsealed.IntentFileHash != prior.Proof.Unsealed.IntentFileHash) {
				return errors.New("validator committed checkpoint discarded retained unsealed liability")
			}
			for i, ledger := range p.Unsealed.Ledgers {
				old := prior.Proof.Unsealed.Ledgers[i]
				if ledger.NoId != old.NoId || ledger.Head.LastSequence < old.Head.LastSequence || ledger.Head.RecordBytes < old.Head.RecordBytes || ledger.Head.TrailCount < old.Head.TrailCount || ledger.Head.LastSequence == old.Head.LastSequence && (ledger.Head != old.Head || ledger.PendingTrails != old.PendingTrails || ledger.PendingHash != old.PendingHash) {
					return errors.New("validator unsealed ledger checkpoint regressed")
				}
				if old.TailBoundaryProof != nil && (ledger.TailBoundaryProof == nil || ledger.Head == old.Head && p.Prefixes[i].LastSequence == prior.Proof.Prefixes[i].LastSequence && !reflect.DeepEqual(ledger.TailBoundaryProof, old.TailBoundaryProof)) {
					return errors.New("validator unsealed tail boundary checkpoint regressed")
				}
			}
		}
	} else if p.PreviousHash != "" {
		return errors.New("validator committed checkpoint invents earlier custody")
	}
	record.CommittedCheckpoints[index] = &checkpoint
	record.HighWaterAt = checkpoint.ObservedAt
	return store.save(*record)
}
