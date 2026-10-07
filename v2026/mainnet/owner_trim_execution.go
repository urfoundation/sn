// The trim owner persists every external-effect boundary. A lost signature or
// send acknowledgement never creates a new nonce, era or broadcast allowance.
package main

import (
	"context"
	"encoding/hex"
	"errors"
	"strings"
)

const ownerTrimRecordSchema = "urnetwork-mainnet-owner-trim-state-v1"

// Transaction finality is separate from a verified partial post-state. Residual
// miners remain visible and this phase never claims a full native reset.
type ownerTrimStepResult struct {
	Phase                string  `json:"phase"`
	Status               string  `json:"status"`
	TransactionFinalized bool    `json:"transaction_finalized"`
	GenerationReconciled bool    `json:"generation_correspondence_observed"`
	ResidualOldCount     *uint16 `json:"residual_old_generation_count,omitempty"`
	FullResetCompleted   bool    `json:"full_reset_completed"`
	ActivationReady      bool    `json:"activation_ready"`
}

// A successful native dispatch is not an assertion that every old miner left.
func ownerTrimRetainedResult(record ownerTrimRecord) ownerTrimStepResult {
	result := ownerTrimStepResult{Phase: record.Phase, Status: "pending"}
	if record.Reconciliation == nil {
		return result
	}
	if record.Reconciliation.Receipt == nil {
		result.Status = "closed-without-trim-receipt"
		return result
	}
	result.TransactionFinalized, result.Status = true, "transaction-finalized-generation-outcome-unresolved"
	if census := record.Reconciliation.Census; census != nil && census.Issue == "" && census.Correspondence != nil {
		result.Status = "transaction-finalized-generation-conflict"
		if record.Reconciliation.Receipt.Success && census.Correspondence.Matches {
			result.GenerationReconciled, result.Status = true, "transaction-finalized-partial-trim-correspondence-observed"
		}
		count := uint16(len(census.Correspondence.ResidualOld))
		result.ResidualOldCount = &count
	}
	return result
}

var errOwnerTrimSignatureNotIssued = errors.New("owner custody authoritatively attests this exact request has never issued a signature")

// Implementations fence the whole coldkey across machines, retain the original
// public signature and never sign during lookup. Generic not-found is ambiguous.
type ownerTrimSigner interface {
	signOnce(context.Context, ownerTrimAction) ([]byte, error)
	recoverSignature(context.Context, string) ([]byte, error)
}

// Admission is specific to the original action domain. Strict v1/v2 require
// enforced future invariants; the distinct best-effort domain requires a separate
// signed residual-risk policy and current checks. Neither accepts proof booleans.
type ownerTrimAuthority interface {
	authorize(context.Context, ownerTrimExecutionConfig, ownerTrimActionReconciliation) error
}

// Reads authenticate the exact finalized interval and retain financial outcome
// even when a separate census cannot be read. Submissions have no retry loop.
type ownerTrimChain interface {
	reconcile(context.Context, ownerTrimAction, []byte) (ownerTrimActionReconciliation, error)
	submit(context.Context, ownerTrimExecutionConfig, []byte) error
}

// Nonce absence means unavailable, never zero. Census and bounded window
// evidence are separate from financial dispatch at an earlier finalized block.
type ownerTrimObservation struct {
	FinalizedNumber uint64                 `json:"finalized_number"`
	FinalizedHash   string                 `json:"finalized_hash"`
	AccountNonce    *uint32                `json:"account_nonce"`
	Census          *subnetPreviewEnvelope `json:"census,omitempty"`
	Issue           string                 `json:"issue,omitempty"`
}

// Before/after are whole-block observations. No per-removal runtime event
// exists, so correspondence cannot attribute every absence to this one call.
type ownerTrimReceiptCensus struct {
	Before         *subnetPreviewEnvelope `json:"before,omitempty"`
	After          *subnetPreviewEnvelope `json:"after,omitempty"`
	Correspondence *ownerTrimActualSubset `json:"generation_correspondence,omitempty"`
	Issue          string                 `json:"issue,omitempty"`
}

// Coverage must include every canonical body through expiry or the finalized
// head. Another transaction's nonce consumption is never successful expiry.
type ownerTrimActionReconciliation struct {
	Observation    ownerTrimObservation    `json:"observation"`
	AnchorHash     string                  `json:"anchor_hash"`
	CheckedFrom    uint64                  `json:"checked_from"`
	CheckedThrough uint64                  `json:"checked_through"`
	Receipt        *rootActionReceipt      `json:"receipt,omitempty"`
	Census         *ownerTrimReceiptCensus `json:"receipt_census,omitempty"`
}

// Validate envelope correspondence before any owner trusts an adapter result.
func (self ownerTrimActionReconciliation) validate(action ownerTrimAction, raw []byte) error {
	o := self.Observation
	if !rootCanonicalHash(o.FinalizedHash) || o.FinalizedNumber < action.BirthBlock || o.FinalizedNumber-action.BirthBlock > rootAncestryLimit ||
		o.FinalizedNumber == action.BirthBlock && o.FinalizedHash != action.BirthHash || self.AnchorHash != action.BirthHash ||
		self.CheckedFrom != action.BirthBlock+1 || self.CheckedThrough != min(o.FinalizedNumber, action.BirthBlock+action.Period-1) {
		return errors.New("owner trim reconciliation has incomplete canonical coverage or another anchor")
	}
	if receipt := self.Receipt; receipt != nil {
		if len(raw) == 0 || receipt.RawExtrinsic != "0x"+hex.EncodeToString(raw) || receipt.BlockNumber < self.CheckedFrom || receipt.BlockNumber > self.CheckedThrough ||
			!rootCanonicalHash(receipt.BlockHash) || !rootCanonicalHash(receipt.EventHash) || receipt.PostState != nil ||
			receipt.Success == (receipt.DispatchError != "") || receipt.ExecutionRuntimeVersion.SpecName == "" || receipt.ExecutionRuntimeVersion.SpecVersion == 0 ||
			receipt.ExecutionRuntimeVersion.TransactionVersion == 0 || receipt.ExecutionRuntimeVersion.StateVersion != 1 ||
			!rootCanonicalHash(receipt.ExecutionCodeHash) || !rootCanonicalHash(receipt.ExecutionMetadataHash) {
			return errors.New("owner trim exact dispatch/fee evidence is invalid")
		}
		if receipt.BlockNumber == o.FinalizedNumber && receipt.BlockHash != o.FinalizedHash {
			return errors.New("owner trim inclusion contradicts current finalized mapping")
		}
		if census := self.Census; census != nil && census.Correspondence != nil {
			if census.Issue != "" || census.Before == nil || census.After == nil {
				return errors.New("owner trim generation correspondence lacks complete before/after evidence")
			}
			before, after := census.Before.Observation, census.After.Observation
			sealedBefore, _ := sealSubnetPreview(before)
			sealedAfter, _ := sealSubnetPreview(after)
			if sealedBefore.ContentHash != census.Before.ContentHash || sealedAfter.ContentHash != census.After.ContentHash ||
				!before.CensusComplete || !after.CensusComplete || before.Identity.FinalizedNumber+1 != receipt.BlockNumber || after.Identity.FinalizedNumber != receipt.BlockNumber || after.Identity.FinalizedHash != receipt.BlockHash ||
				census.Correspondence.FullReset || census.Correspondence.AttributedToTrim || census.Correspondence.Matches != (len(census.Correspondence.Blockers) == 0) {
				return errors.New("owner trim generation correspondence differs from exact inclusion or claims unproved reset")
			}
		}
	} else if self.Census != nil {
		return errors.New("owner trim receipt census has no exact inclusion")
	}
	return nil
}

// Original approval, public signature, attempt count and terminal evidence are
// retained together. A completed or expired journal cannot be repurposed.
type ownerTrimRecord struct {
	Schema            string                               `json:"schema"`
	Config            ownerTrimExecutionConfig             `json:"config"`
	ApprovalKey       string                               `json:"approval_public_key_ed25519"`
	Phase             string                               `json:"phase"`
	Signature         string                               `json:"signature,omitempty"`
	RawExtrinsic      string                               `json:"raw_extrinsic,omitempty"`
	ExtrinsicHash     string                               `json:"extrinsic_hash,omitempty"`
	Broadcasts        uint8                                `json:"broadcasts"`
	LastFinalized     uint64                               `json:"last_finalized"`
	LastFinalizedHash string                               `json:"last_finalized_hash,omitempty"`
	Reconciliation    *ownerTrimActionReconciliation       `json:"terminal_evidence,omitempty"`
	Submission        *ownerTrimBestEffortSubmissionRecord `json:"best_effort_submission,omitempty"`
	ContentHash       string                               `json:"content_hash"`
}

// Economic deviation is an outcome to retain, never a reason to lose a receipt.
func ownerTrimTerminalPhase(action ownerTrimAction, evidence ownerTrimActionReconciliation, signed bool) string {
	if receipt := evidence.Receipt; receipt != nil {
		if receipt.ExecutionRuntimeVersion != action.Runtime.RuntimeVersion || receipt.ExecutionCodeHash != action.Runtime.RuntimeCodeHash || receipt.ExecutionMetadataHash != action.Runtime.RuntimeMetadataHash {
			return "runtime-deviation"
		}
		if receipt.ActualFeeRao > action.FeeReserveRao {
			return "fee-overrun"
		}
		if !receipt.Success {
			return "dispatch-failed"
		}
		return "finalized"
	}
	o := evidence.Observation
	if o.FinalizedNumber >= action.BirthBlock+action.Period && o.AccountNonce != nil {
		if *o.AccountNonce == action.Nonce {
			if !signed {
				return "expired-unsigned"
			}
			return "expired"
		}
		if *o.AccountNonce > action.Nonce {
			return "nonce-conflict"
		}
	}
	return ""
}

// The independent key is supplied on reopen; retained self-signed state is not
// authority. Original public bytes are checked even for a terminal result.
func (self ownerTrimRecord) validate(config ownerTrimExecutionConfig, key string) error {
	if err := config.validate(key); err != nil {
		return err
	}
	claimed := self.ContentHash
	self.ContentHash = ""
	action := config.Action
	if self.Schema != ownerTrimRecordSchema || self.ApprovalKey != key || rootObjectHash(self.Config) != rootObjectHash(config) || claimed != rootObjectHash(self) ||
		self.Broadcasts > action.MaxBroadcasts || self.LastFinalized == 0 && self.LastFinalizedHash != "" ||
		self.LastFinalized != 0 && (self.LastFinalized < action.BirthBlock || !rootCanonicalHash(self.LastFinalizedHash)) {
		return errors.New("owner trim journal approval, checksum, position or allowance differs")
	}
	var raw []byte
	if self.Signature == "" {
		if self.RawExtrinsic != "" || self.ExtrinsicHash != "" || self.Broadcasts != 0 || self.Phase != "reserved" && self.Phase != "signing" && self.Phase != "expired-unsigned" && self.Phase != "nonce-conflict" {
			return errors.New("owner trim unsigned phase carries signed or submitted state")
		}
	} else {
		signature, err := rootOfflineSignatureBytes(self.Signature)
		if err != nil {
			return err
		}
		raw, err = action.signed(signature)
		if err != nil || self.RawExtrinsic != "0x"+hex.EncodeToString(raw) || self.ExtrinsicHash != rootExtrinsicHash(raw) || self.Phase == "reserved" || self.Phase == "signing" || self.Phase == "expired-unsigned" {
			return errors.Join(errors.New("owner trim original signed bytes or phase differ"), err)
		}
	}
	if self.Submission != nil {
		if self.Signature == "" || self.Submission.SubmittedBroadcasts < self.Submission.Approval.InitialBroadcasts || self.Submission.SubmittedBroadcasts > self.Broadcasts {
			return errors.New("owner trim submission policy has lost its signed bytes or consumed attempts")
		}
		if err := self.Submission.Approval.validate(config, self.Submission.ApprovalKey, self.ExtrinsicHash); err != nil {
			return err
		}
	}
	switch self.Phase {
	case "reserved", "signing", "signed", "pending":
		if self.Reconciliation != nil || self.Phase == "signed" && self.Broadcasts != 0 || self.Phase == "pending" && self.Broadcasts == 0 {
			return errors.New("owner trim pending state has contradictory progress")
		}
	case "finalized", "dispatch-failed", "runtime-deviation", "fee-overrun", "expired", "expired-unsigned", "nonce-conflict":
		if self.Reconciliation == nil {
			return errors.New("owner trim terminal phase lacks canonical evidence")
		}
		if err := self.Reconciliation.validate(action, raw); err != nil {
			return err
		}
		if ownerTrimTerminalPhase(action, *self.Reconciliation, len(raw) != 0) != self.Phase || self.LastFinalized != self.Reconciliation.Observation.FinalizedNumber || self.LastFinalizedHash != self.Reconciliation.Observation.FinalizedHash {
			return errors.New("owner trim terminal phase contradicts finalized evidence")
		}
	default:
		return errors.New("owner trim journal phase is unknown")
	}
	return nil
}

// Successful save means file and directory durability; an error poisons use.
type ownerTrimStorage interface {
	load() (ownerTrimRecord, error)
	save(ownerTrimRecord) error
}

// One supervisor serializes calls while the store holds original v3 custody
// locks. Ports receive value-only actions; no shared call vector can be mutated.
type ownerTrimExecutor struct {
	config    ownerTrimExecutionConfig
	key       string
	store     ownerTrimStorage
	chain     ownerTrimChain
	authority ownerTrimAuthority
	signer    ownerTrimSigner
	poisoned  bool
	failure   error
}

// Ambiguous persistence always requires reopen before another external effect.
func (self *ownerTrimExecutor) persist(record ownerTrimRecord) error {
	record.ContentHash = ""
	record.ContentHash = rootObjectHash(record)
	if err := record.validate(self.config, self.key); err != nil {
		self.poisoned, self.failure = true, err
		return err
	}
	if err := self.store.save(record); err != nil {
		if !mainnetDurableAdmissionPending(err) {
			self.poisoned, self.failure = true, err
		}
		return err
	}
	return nil
}

// Each step makes at most one custody or transport request. Recovery precedes
// fresh authority admission, and canonical history precedes any new send.
func (self *ownerTrimExecutor) step(ctx context.Context) (ownerTrimStepResult, error) {
	result := ownerTrimStepResult{Status: "blocked"}
	if ctx == nil || self.poisoned || self.store == nil {
		return result, errors.Join(errors.New("owner trim requires context and an open durable owner"), self.failure)
	}
	record, err := self.store.load()
	if err == nil {
		err = record.validate(self.config, self.key)
	}
	if err != nil {
		if !mainnetDurableAdmissionPending(err) {
			self.poisoned, self.failure = true, err
		}
		return result, err
	}
	result.Phase = record.Phase
	if record.Reconciliation != nil {
		result = ownerTrimRetainedResult(record)
		census := record.Reconciliation.Census
		if self.chain == nil || record.Reconciliation.Receipt == nil || census != nil && census.Issue == "" && census.Correspondence != nil {
			return result, ctx.Err()
		}
		// Financial settlement is immutable. A census-only continuation may
		// enrich it but can never return to signing or submission after a gap.
		raw, _ := hex.DecodeString(strings.TrimPrefix(record.RawExtrinsic, "0x"))
		updated, err := self.chain.reconcile(ctx, record.Config.Action, raw)
		if err != nil {
			return result, err
		}
		if err := updated.validate(record.Config.Action, raw); err != nil {
			return result, err
		}
		if rootObjectHash(updated.Receipt) != rootObjectHash(record.Reconciliation.Receipt) || updated.Observation.FinalizedNumber < record.LastFinalized ||
			updated.Observation.FinalizedNumber == record.LastFinalized && updated.Observation.FinalizedHash != record.LastFinalizedHash ||
			updated.Observation.FinalizedNumber > record.LastFinalized && updated.Observation.FinalizedHash == record.LastFinalizedHash {
			return result, errors.New("owner trim readback continuation changed original receipt or finalized continuity")
		}
		record.Reconciliation = &updated
		record.LastFinalized, record.LastFinalizedHash = updated.Observation.FinalizedNumber, updated.Observation.FinalizedHash
		if err := self.persist(record); err != nil {
			return result, err
		}
		return ownerTrimRetainedResult(record), nil
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	issueSignature := record.Phase == "reserved"
	if record.Phase == "signing" {
		if self.signer == nil {
			return result, errors.New("owner trim original-signature custody recovery is unavailable")
		}
		signature, err := self.signer.recoverSignature(ctx, record.Config.Action.RequestHash)
		if err == errOwnerTrimSignatureNotIssued && len(signature) == 0 {
			issueSignature = true
		} else if err != nil || len(signature) == 0 {
			return result, errors.Join(errors.New("owner trim signing outcome is unknown; no replacement request is allowed"), err)
		} else {
			return self.retainSignature(record, signature)
		}
	}
	if self.chain == nil {
		return result, errors.New("owner trim canonical receipt adapter is unavailable")
	}
	action := record.Config.Action
	raw, _ := hex.DecodeString(strings.TrimPrefix(record.RawExtrinsic, "0x"))
	evidence, err := self.chain.reconcile(ctx, action, append([]byte(nil), raw...))
	if err != nil {
		return result, err
	}
	if err := evidence.validate(action, raw); err != nil {
		return result, err
	}
	o := evidence.Observation
	if o.FinalizedNumber < record.LastFinalized || o.FinalizedNumber == record.LastFinalized && record.LastFinalizedHash != "" && o.FinalizedHash != record.LastFinalizedHash || o.FinalizedNumber > record.LastFinalized && o.FinalizedHash == record.LastFinalizedHash {
		return result, errors.New("owner trim finalized continuity changed")
	}
	record.LastFinalized, record.LastFinalizedHash = o.FinalizedNumber, o.FinalizedHash
	if phase := ownerTrimTerminalPhase(action, evidence, len(raw) != 0); phase != "" {
		record.Phase, record.Reconciliation = phase, &evidence
		if err := self.persist(record); err != nil {
			return result, err
		}
		return ownerTrimRetainedResult(record), nil
	}
	if err := self.persist(record); err != nil {
		return result, err
	}
	if o.AccountNonce == nil || *o.AccountNonce != action.Nonce || o.FinalizedNumber >= action.BirthBlock+action.Period || o.Census == nil || o.Issue != "" {
		return result, errors.New("owner trim current nonce, runtime, census or mortal window is unresolved or changed")
	}
	if self.authority == nil {
		return result, errors.New("owner trim independent owner/governance window, source provenance and global custody authority is unavailable")
	}
	if err := self.authority.authorize(ctx, self.config, evidence); err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if issueSignature {
		if self.signer == nil {
			return result, errors.New("owner trim globally fenced coldkey signer is unavailable")
		}
		record.Phase = "signing"
		if err := self.persist(record); err != nil {
			return result, err
		}
		signature, err := self.signer.signOnce(ctx, action)
		if err != nil {
			return ownerTrimStepResult{Phase: "signing", Status: "pending"}, err
		}
		return self.retainSignature(record, signature)
	}
	if record.Broadcasts >= action.MaxBroadcasts {
		return result, errors.New("owner trim broadcast allowance exhausted; receipt recovery remains available")
	}
	record.Broadcasts++
	record.Phase = "pending"
	if err := self.persist(record); err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return ownerTrimStepResult{Phase: "pending", Status: "pending"}, err
	}
	err = self.chain.submit(ctx, self.config, append([]byte(nil), raw...))
	return ownerTrimStepResult{Phase: "pending", Status: "pending"}, err
}

// Even a valid alternative signature cannot replace the original after signing.
func (self *ownerTrimExecutor) retainSignature(record ownerTrimRecord, signature []byte) (ownerTrimStepResult, error) {
	result := ownerTrimStepResult{Phase: record.Phase, Status: "blocked"}
	if record.Phase != "signing" {
		return result, errors.New("owner trim signature was returned outside reserved signing")
	}
	raw, err := record.Config.Action.signed(signature)
	if err != nil {
		return result, err
	}
	record.Phase, record.Signature = "signed", hex.EncodeToString(signature)
	record.RawExtrinsic, record.ExtrinsicHash = "0x"+hex.EncodeToString(raw), rootExtrinsicHash(raw)
	if err := self.persist(record); err != nil {
		return result, err
	}
	return ownerTrimStepResult{Phase: "signed", Status: "pending"}, nil
}
