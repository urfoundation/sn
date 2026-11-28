// Original custody retains one signing request and its separate bounded send
// approval. Ambiguous durability always requires reopening the same custody.
package main

import (
	"context"
	"encoding/hex"
	"errors"
)

const ownerRecycleRecordSchema = "urnetwork-mainnet-owner-recycle-state-v1"

// Public metadata, original signature and exact extrinsic survive all recovery.
// Exported without a returned signature is unresolved, never safe unsigned expiry.
type ownerRecycleRecord struct {
	Schema         string                        `json:"schema"`
	Config         ownerRecycleConfig            `json:"config"`
	ApprovalKey    string                        `json:"approval_public_key_ed25519"`
	Phase          string                        `json:"phase"`
	Request        *ownerRecycleSigningRequest   `json:"signing_request,omitempty"`
	Signature      string                        `json:"signature,omitempty"`
	RawExtrinsic   string                        `json:"raw_extrinsic,omitempty"`
	ExtrinsicHash  string                        `json:"extrinsic_hash,omitempty"`
	Reconciliation *ownerRecycleReconciliation   `json:"reconciliation,omitempty"`
	Submission     *ownerRecycleSubmissionRecord `json:"submission,omitempty"`
	ContentHash    string                        `json:"content_hash"`
}

// Financial outcome and mode qualification are separate, with activation false.
type ownerRecycleResult struct {
	Phase                string `json:"phase"`
	TransactionFinalized bool   `json:"transaction_finalized"`
	RecycleModeObserved  bool   `json:"recycle_mode_observed_at_inclusion"`
	ActivationReady      bool   `json:"activation_ready"`
	ExtrinsicHash        string `json:"extrinsic_hash,omitempty"`
	ReadbackIssue        string `json:"readback_issue,omitempty"`
}

// An unchanged inclusion-block Burn is a conflict, not a successful transition.
func ownerRecyclePhase(request ownerRecycleSigningRequest, evidence ownerRecycleReconciliation) string {
	a := request.Config.Action
	if receipt := evidence.Receipt; receipt != nil {
		if receipt.ActualFeeRao > a.FeeReserveRao {
			return "fee-overrun"
		}
		if !receipt.Success {
			return "dispatch-failed"
		}
		if evidence.Readback == nil {
			return "finalized-readback-pending"
		}
		metadata, _, err := nativePinnedMetadata(request.Metadata, a.Policy.RuntimeMetadataHash)
		if err != nil {
			return "finalized-readback-pending"
		}
		window, err := evidence.Readback.window(a.Policy, metadata)
		if err != nil {
			return "finalized-readback-pending"
		}
		if window.Mode != 1 || window.RegistrationBlock != a.SubnetRegistrationBlock || window.LastUpdate != receipt.BlockNumber {
			return "finalized-state-conflict"
		}
		return "finalized"
	}
	if evidence.AccountNonce != nil {
		if *evidence.AccountNonce > a.Nonce {
			return "nonce-conflict"
		}
		if evidence.FinalizedNumber >= a.BirthBlock+a.Period && *evidence.AccountNonce == a.Nonce {
			return "expired"
		}
	}
	return "signed"
}

// Reopening authenticates independent approval, request metadata and signature;
// self-consistent journal edits cannot rebind the approved owner action.
func (self ownerRecycleRecord) validate(config ownerRecycleConfig, key string) error {
	if err := config.validate(key); err != nil {
		return err
	}
	claimed := self.ContentHash
	self.ContentHash = ""
	if self.Schema != ownerRecycleRecordSchema || self.ApprovalKey != key || rootObjectHash(self.Config) != rootObjectHash(config) || claimed != rootObjectHash(self) {
		return errors.New("recycle custody config/key or checksum differs")
	}
	if self.Submission != nil {
		if self.Request == nil || self.Signature == "" || self.Submission.Attempts > self.Submission.Approval.MaximumAttempts {
			return errors.New("recycle submission lacks original signed custody or exceeds its immutable allowance")
		}
		if err := self.Submission.Approval.validate(self, self.Submission.ApprovalKey); err != nil {
			return err
		}
	}
	if self.Phase == "reserved" {
		if self.Request != nil || self.Signature != "" || self.RawExtrinsic != "" || self.ExtrinsicHash != "" || self.Reconciliation != nil {
			return errors.New("reserved recycle custody contains later progress")
		}
		return nil
	}
	if self.Request == nil || rootObjectHash(self.Request.Config) != rootObjectHash(config) {
		return errors.New("recycle custody lost original signing request")
	}
	if err := self.Request.validate(ownerSigningTrust{RequestHash: self.Request.ContentHash, ApprovalKey: key, Owner: config.Action.Owner, Genesis: config.Action.Policy.GenesisHash}); err != nil {
		return err
	}
	if self.Phase == "exported" {
		if self.Signature != "" || self.RawExtrinsic != "" || self.ExtrinsicHash != "" || self.Reconciliation != nil {
			return errors.New("unreturned recycle request contains signed or terminal progress")
		}
		return nil
	}
	signature, err := rootOfflineSignatureBytes(self.Signature)
	if err != nil {
		return err
	}
	raw, err := config.Action.signed(signature)
	if err != nil || self.RawExtrinsic != "0x"+hex.EncodeToString(raw) || self.ExtrinsicHash != rootExtrinsicHash(raw) {
		return errors.Join(errors.New("recycle original signature/extrinsic changed"), err)
	}
	if self.Reconciliation == nil {
		if self.Phase != "signed" {
			return errors.New("recycle terminal phase lacks evidence")
		}
		return nil
	}
	if err := self.Reconciliation.validate(*self.Request, raw); err != nil {
		return err
	}
	if self.Phase != ownerRecyclePhase(*self.Request, *self.Reconciliation) {
		return errors.New("recycle phase contradicts original finalized evidence")
	}
	return nil
}

// Durability is explicit so fault tests force both pre- and post-rename failures.
type ownerRecycleStorage interface {
	load() (ownerRecycleRecord, error)
	save(ownerRecycleRecord) error
}

// One local supervisor owns this object; its store holds the exclusive lock.
// Global owner-key exclusivity remains an independent operational requirement.
type ownerRecycleCustody struct {
	config   ownerRecycleConfig
	key      string
	store    ownerRecycleStorage
	poisoned bool
	failure  error
}

// Any uncertainty stops this instance before another handoff; recovery reopens.
func (self *ownerRecycleCustody) load() (ownerRecycleRecord, error) {
	if self.poisoned || self.store == nil {
		return ownerRecycleRecord{}, errors.Join(errors.New("recycle custody must reopen"), self.failure)
	}
	record, err := self.store.load()
	if err == nil {
		err = record.validate(self.config, self.key)
	}
	if err != nil && !mainnetDurableAdmissionPending(err) {
		self.poisoned, self.failure = true, err
	}
	return record, err
}

// File and parent-directory durability precede a public request or result.
func (self *ownerRecycleCustody) persist(record ownerRecycleRecord) error {
	record.ContentHash = ""
	record.ContentHash = rootObjectHash(record)
	err := record.validate(self.config, self.key)
	if err == nil {
		err = self.store.save(record)
	}
	if err != nil && !mainnetDurableAdmissionPending(err) {
		self.poisoned, self.failure = true, err
	}
	return err
}

// An ambiguous/lost output can only re-export the identical approved request.
// It must never cause the owner device to issue a second signature.
func (self *ownerRecycleCustody) export(metadata, ledgerMetadata string) (ownerRecycleSigningRequest, error) {
	record, err := self.load()
	if err != nil {
		return ownerRecycleSigningRequest{}, err
	}
	request, err := newOwnerRecycleSigningRequest(self.config, self.key, metadata, ledgerMetadata)
	if err != nil {
		return ownerRecycleSigningRequest{}, err
	}
	if record.Phase == "exported" {
		if rootObjectHash(record.Request) != rootObjectHash(request) {
			return ownerRecycleSigningRequest{}, errors.New("recycle re-export changes original request")
		}
		if _, err := self.load(); err != nil {
			return ownerRecycleSigningRequest{}, err
		}
		return *record.Request, nil
	}
	if record.Phase != "reserved" {
		return ownerRecycleSigningRequest{}, errors.New("recycle request already has a signature or terminal result")
	}
	record.Phase, record.Request = "exported", &request
	if err := self.persist(record); err != nil {
		return ownerRecycleSigningRequest{}, err
	}
	return request, nil
}

// A lost import acknowledgement accepts only the same original signature;
// another otherwise valid randomized Sr25519 signature is still a replacement.
func (self *ownerRecycleCustody) importSignature(requestHash string, signature []byte) (ownerRecycleResult, error) {
	record, err := self.load()
	if err != nil {
		return ownerRecycleResult{}, err
	}
	if record.Request == nil || record.Request.ContentHash != requestHash {
		return ownerRecycleResult{}, errors.New("recycle signature import lacks the original exported request")
	}
	raw, err := record.Config.Action.signed(signature)
	if err != nil {
		return ownerRecycleResult{}, err
	}
	if record.Signature != "" {
		if record.Signature != hex.EncodeToString(signature) || record.RawExtrinsic != "0x"+hex.EncodeToString(raw) {
			return ownerRecycleResult{}, errors.New("recycle import attempts to replace original signature")
		}
		if _, err := self.load(); err != nil {
			return ownerRecycleResult{}, err
		}
		return ownerRecycleRetainedResult(record), nil
	}
	if record.Phase != "exported" {
		return ownerRecycleResult{}, errors.New("recycle signature import has no reserved export")
	}
	record.Signature, record.RawExtrinsic, record.ExtrinsicHash = hex.EncodeToString(signature), "0x"+hex.EncodeToString(raw), rootExtrinsicHash(raw)
	record.Phase = "signed"
	if err := self.persist(record); err != nil {
		return ownerRecycleResult{}, err
	}
	return ownerRecycleRetainedResult(record), nil
}

// This adapter is intentionally read-only. No generic execution method can
// reinterpret a missing signature, expired era or failed receipt as new work.
type ownerRecycleReconciler interface {
	reconcile(context.Context, ownerRecycleSigningRequest, []byte) (ownerRecycleReconciliation, error)
}

// Recovery may enrich a missing readback but never erase or change its receipt.
func (self *ownerRecycleCustody) reconcile(ctx context.Context, chain ownerRecycleReconciler) (ownerRecycleResult, error) {
	record, err := self.load()
	if err != nil {
		return ownerRecycleResult{}, err
	}
	if ctx == nil || chain == nil || record.Request == nil || record.Signature == "" {
		return ownerRecycleRetainedResult(record), errors.New("recycle receipt recovery requires the original returned signature and read-only chain")
	}
	if record.Phase != "signed" && record.Phase != "finalized-readback-pending" {
		return ownerRecycleRetainedResult(record), ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return ownerRecycleRetainedResult(record), err
	}
	raw, _ := hex.DecodeString(record.RawExtrinsic[2:])
	evidence, err := chain.reconcile(ctx, *record.Request, raw)
	if err != nil {
		return ownerRecycleRetainedResult(record), err
	}
	if err := evidence.validate(*record.Request, raw); err != nil {
		return ownerRecycleRetainedResult(record), err
	}
	if prior := record.Reconciliation; prior != nil {
		if evidence.FinalizedNumber < prior.FinalizedNumber || evidence.FinalizedNumber == prior.FinalizedNumber && evidence.FinalizedHash != prior.FinalizedHash || evidence.FinalizedNumber > prior.FinalizedNumber && evidence.FinalizedHash == prior.FinalizedHash || prior.Receipt != nil && rootObjectHash(prior.Receipt) != rootObjectHash(evidence.Receipt) {
			return ownerRecycleRetainedResult(record), errors.New("recycle continuation changed finalized continuity or original financial receipt")
		}
	}
	record.Phase, record.Reconciliation = ownerRecyclePhase(*record.Request, evidence), &evidence
	if err := self.persist(record); err != nil {
		return ownerRecycleResult{}, err
	}
	return ownerRecycleRetainedResult(record), nil
}

// Successful mode readback remains distinct from interval emission activation.
func ownerRecycleRetainedResult(record ownerRecycleRecord) ownerRecycleResult {
	result := ownerRecycleResult{Phase: record.Phase, ExtrinsicHash: record.ExtrinsicHash}
	if record.Reconciliation != nil {
		result.TransactionFinalized = record.Reconciliation.Receipt != nil
		result.RecycleModeObserved = record.Phase == "finalized"
		result.ReadbackIssue = record.Reconciliation.ReadbackIssue
	}
	return result
}
