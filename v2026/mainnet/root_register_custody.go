// Root registration custody retains one coldkey request and its separate send
// approval. Ambiguous durability always requires reopening the same custody.
package main

import (
	"context"
	"encoding/hex"
	"errors"
)

const rootRegisterRecordSchema = "urnetwork-mainnet-root-register-state-v1"

// Public metadata, original signature and exact extrinsic survive all recovery.
// Exported without a returned signature is unresolved, never safe unsigned expiry.
type rootRegisterRecord struct {
	Schema         string                        `json:"schema"`
	Config         rootRegisterConfig            `json:"config"`
	ApprovalKey    string                        `json:"approval_public_key_ed25519"`
	Phase          string                        `json:"phase"`
	Request        *rootRegisterSigningRequest   `json:"signing_request,omitempty"`
	Signature      string                        `json:"signature,omitempty"`
	RawExtrinsic   string                        `json:"raw_extrinsic,omitempty"`
	ExtrinsicHash  string                        `json:"extrinsic_hash,omitempty"`
	Reconciliation *rootRegisterReconciliation   `json:"reconciliation,omitempty"`
	Submission     *rootRegisterSubmissionRecord `json:"submission,omitempty"`
	ContentHash    string                        `json:"content_hash"`
}

// A finalized registration identifies one seat generation, never activation.
type rootRegisterResult struct {
	Phase                string               `json:"phase"`
	TransactionFinalized bool                 `json:"transaction_finalized"`
	RootSeatObserved     bool                 `json:"root_seat_observed_at_inclusion"`
	ActivationReady      bool                 `json:"activation_ready"`
	ExtrinsicHash        string               `json:"extrinsic_hash,omitempty"`
	ReadbackIssue        string               `json:"readback_issue,omitempty"`
	Seat                 *rootSeatExpectation `json:"seat,omitempty"`
}

// Reopening authenticates independent approval, request metadata and signature;
// self-consistent journal edits cannot rebind the approved operator action.
func (self rootRegisterRecord) validate(config rootRegisterConfig, key string) error {
	if err := config.validate(key); err != nil {
		return err
	}
	claimed := self.ContentHash
	self.ContentHash = ""
	if self.Schema != rootRegisterRecordSchema || self.ApprovalKey != key || rootObjectHash(self.Config) != rootObjectHash(config) || claimed != rootObjectHash(self) {
		return errors.New("root registration custody config/key or checksum differs")
	}
	if self.Submission != nil {
		if self.Request == nil || self.Signature == "" || self.Submission.Attempts > self.Submission.Approval.MaximumAttempts {
			return errors.New("root registration submission lacks original signed custody or exceeds its immutable allowance")
		}
		if err := self.Submission.Approval.validate(self, self.Submission.ApprovalKey); err != nil {
			return err
		}
	}
	if self.Phase == "reserved" {
		if self.Request != nil || self.Signature != "" || self.RawExtrinsic != "" || self.ExtrinsicHash != "" || self.Reconciliation != nil {
			return errors.New("reserved root registration custody contains later progress")
		}
		return nil
	}
	if self.Request == nil || rootObjectHash(self.Request.Config) != rootObjectHash(config) {
		return errors.New("root registration custody lost original signing request")
	}
	if err := self.Request.validate(ownerSigningTrust{RequestHash: self.Request.ContentHash, ApprovalKey: key, Owner: config.Action.Policy.Operator, Genesis: config.Action.Policy.GenesisHash}); err != nil {
		return err
	}
	if self.Phase == "exported" {
		if self.Signature != "" || self.RawExtrinsic != "" || self.ExtrinsicHash != "" || self.Reconciliation != nil {
			return errors.New("unreturned root registration request contains signed or terminal progress")
		}
		return nil
	}
	signature, err := rootOfflineSignatureBytes(self.Signature)
	if err != nil {
		return err
	}
	raw, err := config.Action.signed(signature)
	if err != nil || self.RawExtrinsic != "0x"+hex.EncodeToString(raw) || self.ExtrinsicHash != rootExtrinsicHash(raw) {
		return errors.Join(errors.New("root registration original signature/extrinsic changed"), err)
	}
	if self.Reconciliation == nil {
		if self.Phase != "signed" {
			return errors.New("root registration terminal phase lacks evidence")
		}
		return nil
	}
	if err := self.Reconciliation.validate(*self.Request, raw); err != nil {
		return err
	}
	if self.Phase != rootRegisterPhase(*self.Request, *self.Reconciliation) {
		return errors.New("root registration phase contradicts original finalized evidence")
	}
	return nil
}

// Every successor retains the original request, signature, inclusion evidence
// and consumed send allowance. Only a missing inclusion readback may be enriched.
func rootRegisterContinuation(prior, next rootRegisterRecord) error {
	if prior.Request != nil && rootObjectHash(prior.Request) != rootObjectHash(next.Request) {
		return errors.New("root registration continuation replaces its original exported request")
	}
	if prior.Signature != "" && (next.Signature != prior.Signature || next.RawExtrinsic != prior.RawExtrinsic || next.ExtrinsicHash != prior.ExtrinsicHash) {
		return errors.New("root registration continuation replaces its original signature")
	}
	if prior.Submission != nil {
		if next.Submission == nil || next.Submission.ApprovalKey != prior.Submission.ApprovalKey || rootObjectHash(next.Submission.Approval) != rootObjectHash(prior.Submission.Approval) || next.Submission.Attempts < prior.Submission.Attempts {
			return errors.New("root registration continuation resets its original submission allowance")
		}
	}
	switch prior.Phase {
	case "reserved":
		if next.Phase != "reserved" && next.Phase != "exported" {
			return errors.New("root registration continuation skips its original export")
		}
	case "exported":
		if next.Phase != "exported" && next.Phase != "signed" {
			return errors.New("root registration continuation skips its original returned signature")
		}
	default:
		readbackPending := prior.Reconciliation != nil && prior.Reconciliation.Receipt != nil && prior.Reconciliation.Readback == nil
		if prior.Phase != "signed" && !readbackPending && (next.Phase != prior.Phase || rootObjectHash(next.Reconciliation) != rootObjectHash(prior.Reconciliation)) {
			return errors.New("root registration continuation changes its terminal evidence")
		}
	}
	if evidence := prior.Reconciliation; evidence != nil {
		current := next.Reconciliation
		if current == nil || current.FinalizedNumber < evidence.FinalizedNumber || current.FinalizedNumber == evidence.FinalizedNumber && current.FinalizedHash != evidence.FinalizedHash || current.FinalizedNumber > evidence.FinalizedNumber && current.FinalizedHash == evidence.FinalizedHash {
			return errors.New("root registration continuation changes finalized continuity")
		}
		if evidence.Receipt != nil && (rootObjectHash(current.Receipt) != rootObjectHash(evidence.Receipt) || current.BodyCount != evidence.BodyCount || current.RawEvents != evidence.RawEvents || rootObjectHash(current.Registration) != rootObjectHash(evidence.Registration)) {
			return errors.New("root registration continuation replaces its original financial receipt or registration event")
		}
		if evidence.Readback != nil && rootObjectHash(current.Readback) != rootObjectHash(evidence.Readback) {
			return errors.New("root registration continuation replaces its original inclusion readback")
		}
	}
	return nil
}

// Durability is explicit so fault tests force both pre- and post-rename failures.
type rootRegisterStorage interface {
	load() (rootRegisterRecord, error)
	save(rootRegisterRecord) error
}

// One local supervisor owns this object; its store holds the exclusive lock.
// Global operator-key exclusivity remains an independent operational requirement.
type rootRegisterCustody struct {
	config   rootRegisterConfig
	key      string
	store    rootRegisterStorage
	poisoned bool
	failure  error
}

// Any uncertainty stops this instance before another handoff; recovery reopens.
func (self *rootRegisterCustody) load() (rootRegisterRecord, error) {
	if self.poisoned || self.store == nil {
		return rootRegisterRecord{}, errors.Join(errors.New("root registration custody must reopen"), self.failure)
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
func (self *rootRegisterCustody) persist(record rootRegisterRecord) error {
	prior, err := self.load()
	if err != nil {
		return err
	}
	record.ContentHash = ""
	record.ContentHash = rootObjectHash(record)
	err = record.validate(self.config, self.key)
	if err == nil {
		err = rootRegisterContinuation(prior, record)
	}
	if err == nil {
		err = self.store.save(record)
	}
	if err != nil && !mainnetDurableAdmissionPending(err) {
		self.poisoned, self.failure = true, err
	}
	return err
}

// An ambiguous/lost output can only re-export the identical approved request.
// It must never cause the operator device to issue a second signature.
func (self *rootRegisterCustody) export(metadata, ledgerMetadata string) (rootRegisterSigningRequest, error) {
	record, err := self.load()
	if err != nil {
		return rootRegisterSigningRequest{}, err
	}
	request, err := newRootRegisterSigningRequest(self.config, self.key, metadata, ledgerMetadata)
	if err != nil {
		return rootRegisterSigningRequest{}, err
	}
	if record.Phase == "exported" {
		if rootObjectHash(record.Request) != rootObjectHash(request) {
			return rootRegisterSigningRequest{}, errors.New("root registration re-export changes original request")
		}
		if _, err := self.load(); err != nil {
			return rootRegisterSigningRequest{}, err
		}
		return *record.Request, nil
	}
	if record.Phase != "reserved" {
		return rootRegisterSigningRequest{}, errors.New("root registration request already has a signature or terminal result")
	}
	record.Phase, record.Request = "exported", &request
	if err := self.persist(record); err != nil {
		return rootRegisterSigningRequest{}, err
	}
	return request, nil
}

// A lost import acknowledgement accepts only the same original signature;
// another otherwise valid randomized sr25519 signature is still a replacement.
func (self *rootRegisterCustody) importSignature(requestHash string, signature []byte) (rootRegisterResult, error) {
	record, err := self.load()
	if err != nil {
		return rootRegisterResult{}, err
	}
	if record.Request == nil || record.Request.ContentHash != requestHash {
		return rootRegisterResult{}, errors.New("root registration signature import lacks the original exported request")
	}
	raw, err := record.Config.Action.signed(signature)
	if err != nil {
		return rootRegisterResult{}, err
	}
	if record.Signature != "" {
		if record.Signature != hex.EncodeToString(signature) || record.RawExtrinsic != "0x"+hex.EncodeToString(raw) {
			return rootRegisterResult{}, errors.New("root registration import attempts to replace original signature")
		}
		if _, err := self.load(); err != nil {
			return rootRegisterResult{}, err
		}
		return rootRegisterRetainedResult(record), nil
	}
	if record.Phase != "exported" {
		return rootRegisterResult{}, errors.New("root registration signature import has no reserved export")
	}
	record.Signature, record.RawExtrinsic, record.ExtrinsicHash = hex.EncodeToString(signature), "0x"+hex.EncodeToString(raw), rootExtrinsicHash(raw)
	record.Phase = "signed"
	if err := self.persist(record); err != nil {
		return rootRegisterResult{}, err
	}
	return rootRegisterRetainedResult(record), nil
}

// This adapter is intentionally read-only. No generic execution method can
// reinterpret a missing signature, expired era or failed receipt as new work.
type rootRegisterReconciler interface {
	reconcile(context.Context, rootRegisterSigningRequest, []byte) (rootRegisterReconciliation, error)
}

// Recovery may enrich a missing readback but never erase or change its receipt.
func (self *rootRegisterCustody) reconcile(ctx context.Context, chain rootRegisterReconciler) (rootRegisterResult, error) {
	record, err := self.load()
	if err != nil {
		return rootRegisterResult{}, err
	}
	if ctx == nil || chain == nil || record.Request == nil || record.Signature == "" {
		return rootRegisterRetainedResult(record), errors.New("root registration receipt recovery requires the original returned signature and read-only chain")
	}
	readbackPending := record.Reconciliation != nil && record.Reconciliation.Receipt != nil && record.Reconciliation.Readback == nil
	if record.Phase != "signed" && !readbackPending {
		return rootRegisterRetainedResult(record), ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return rootRegisterRetainedResult(record), err
	}
	raw, _ := hex.DecodeString(record.RawExtrinsic[2:])
	evidence, err := chain.reconcile(ctx, *record.Request, raw)
	if err != nil {
		return rootRegisterRetainedResult(record), err
	}
	if err := evidence.validate(*record.Request, raw); err != nil {
		return rootRegisterRetainedResult(record), err
	}
	record.Phase, record.Reconciliation = rootRegisterPhase(*record.Request, evidence), &evidence
	if err := self.persist(record); err != nil {
		return rootRegisterResult{}, err
	}
	return rootRegisterRetainedResult(record), nil
}

// A matching inclusion event and readback identify the original seat generation.
func rootRegisterRetainedResult(record rootRegisterRecord) rootRegisterResult {
	result := rootRegisterResult{Phase: record.Phase, ExtrinsicHash: record.ExtrinsicHash}
	if record.Reconciliation != nil {
		result.TransactionFinalized = record.Reconciliation.Receipt != nil
		if record.Request != nil {
			result.Seat = rootRegisterObservedSeat(*record.Request, *record.Reconciliation)
			result.RootSeatObserved = result.Seat != nil
		}
		result.ReadbackIssue = record.Reconciliation.ReadbackIssue
	}
	return result
}
