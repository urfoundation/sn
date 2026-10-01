// One root action owns its nonce and fee reservation from the first signing
// request through a verified finalized receipt or finalized mortal expiry.
// Live authority and globally fenced signing remain separate unqualified ports.
// The owned submission adapter also requires independent route/action approval;
// offline public signatures and read-only receipts cannot activate this owner.
package main

import (
	"context"
	"encoding/hex"
	"errors"
	"math"
	"strings"

	"golang.org/x/crypto/blake2b"

	"github.com/urfoundation/sn/v2026/crv4"
)

const rootActionStateSchema = "urnetwork-mainnet-root-action-state-v1"

// Only an authoritative custody lookup with the approved hotkey fence may
// report this. Missing files, timeouts and generic not-found replies may not.
var errRootSignatureNotIssued = errors.New("root custody attests this request has never issued a signature")

// A custody implementation must durably fence the entire hotkey across hosts,
// honor the one-request approval and return the same retained signature for
// the same request hash. Recovery only looks up; it must never silently sign.
// An authoritative never-issued response uses errRootSignatureNotIssued; the
// owner must recheck current authority before calling signOnce after that proof.
type rootActionSigner interface {
	signOnce(context.Context, rootAction) ([]byte, error)
	recoverSignature(context.Context, string) ([]byte, error)
}

// Admission is an independently implemented capability, not a boolean in a
// policy or a locally rehashed file. It must verify the complete action approval,
// source-to-Wasm mapping, eligibility, custody fence and enforceable exposure.
// The finalized observation is only an input; pending nonce ownership also needs
// custody reconciliation. No implementation is supplied by root-preview.
type rootActionAuthority interface {
	authorize(context.Context, rootAction, rootActionObservation) error
}

// Canonical observations must use the approved owned route and authenticate
// complete block bodies, events and dispatch outcomes. A timeout is an error,
// never an empty successful range. Retries remain within the caller's deadline.
type rootActionChain interface {
	reconcile(context.Context, rootAction, []byte) (rootActionReconciliation, error)
	submit(context.Context, []byte) error
}

// The current finalized chain/seat is separate from an older signed domain.
// An upgrade blocks new signing/broadcast but cannot erase an old receipt.
type rootActionObservation struct {
	NativeChain         string                      `json:"native_chain"`
	GenesisHash         string                      `json:"genesis_hash"`
	EvmChainId          uint64                      `json:"evm_chain_id"`
	FinalizedNumber     uint64                      `json:"finalized_number"`
	FinalizedHash       string                      `json:"finalized_hash"`
	RuntimeVersion      crv4.RuntimeVersionIdentity `json:"runtime_version"`
	RuntimeCodeHash     string                      `json:"runtime_code_hash"`
	RuntimeMetadataHash string                      `json:"runtime_metadata_hash"`
	Hotkey              string                      `json:"hotkey_account_id"`
	Coldkey             string                      `json:"coldkey_account_id"`
	Seat                rootSeatExpectation         `json:"seat"`
	StateUnavailable    bool                        `json:"state_unavailable,omitempty"`
	AccountNonce        uint32                      `json:"account_nonce"`
}

// Post-state is distinct from the exact action phase: later calls in the same
// block may overwrite it. A readback gap remains an explicit operational issue.
type rootReceiptPostState struct {
	WeightsScale          string `json:"weights_scale,omitempty"`
	LastUpdate            uint64 `json:"last_update,omitempty"`
	LastUpdateStorageHash string `json:"last_update_storage_hash,omitempty"`
	Issue                 string `json:"issue,omitempty"`
}

// Receipt adapters must retain exact inclusion index/body, dispatch and fee
// evidence. The event digest refers to the canonical raw SCALE event bundle;
// this core checks correspondence, not storage trie or consensus proofs.
type rootActionReceipt struct {
	BlockNumber             uint64                      `json:"block_number"`
	BlockHash               string                      `json:"block_hash"`
	ExtrinsicIndex          uint32                      `json:"extrinsic_index"`
	RawExtrinsic            string                      `json:"raw_extrinsic"`
	EventHash               string                      `json:"event_hash"`
	Success                 bool                        `json:"success"`
	DispatchError           string                      `json:"dispatch_error,omitempty"`
	PostState               *rootReceiptPostState       `json:"post_state,omitempty"`
	ActualFeeRao            uint64                      `json:"actual_fee_rao"`
	ExecutionRuntimeVersion crv4.RuntimeVersionIdentity `json:"execution_runtime_version"`
	ExecutionCodeHash       string                      `json:"execution_code_hash"`
	ExecutionMetadataHash   string                      `json:"execution_metadata_hash"`
}

// Absence is usable only after a complete canonical scan from birth+1 through
// the specified height. AnchorHash must revalidate the retained era checkpoint.
// An adapter must not construct this result from subscription status alone.
type rootActionReconciliation struct {
	Observation    rootActionObservation `json:"observation"`
	AnchorHash     string                `json:"anchor_hash"`
	CheckedFrom    uint64                `json:"checked_from"`
	CheckedThrough uint64                `json:"checked_through"`
	Receipt        *rootActionReceipt    `json:"receipt,omitempty"`
}

// A record permits one action for its entire lifetime, including failure and
// expiry. It is never reset into a fresh signing allowance on the same path.
type rootActionRecord struct {
	Schema            string                    `json:"schema"`
	Action            rootAction                `json:"action"`
	Phase             string                    `json:"phase"`
	Signature         string                    `json:"signature,omitempty"`
	RawExtrinsic      string                    `json:"raw_extrinsic,omitempty"`
	ExtrinsicHash     string                    `json:"extrinsic_hash,omitempty"`
	Broadcasts        uint8                     `json:"broadcasts"`
	LastFinalized     uint64                    `json:"last_finalized"`
	LastFinalizedHash string                    `json:"last_finalized_hash,omitempty"`
	Reconciliation    *rootActionReconciliation `json:"terminal_evidence,omitempty"`
	ContentHash       string                    `json:"content_hash"`
}

// Storage implementations must atomically persist and fsync before returning.
// Any ambiguous durability error poisons the open owner until it is reopened.
type rootActionStorage interface {
	load() (rootActionRecord, error)
	save(rootActionRecord) error
}

// Each call does at most one external signing/recovery or submission action.
// The owner is deliberately serial; a single external supervisor owns its
// bounded retry cadence and context. There is no hidden watchdog signer loop.
type rootActionOwner struct {
	store     rootActionStorage
	authority rootActionAuthority
	signer    rootActionSigner
	chain     rootActionChain
	poisoned  bool
}

// Typed pending/blocked states cannot be confused with finalized dispatch
// failure. In particular, returned transport errors leave the same action live.
type rootActionStep struct {
	Phase  string `json:"phase"`
	Status string `json:"status"`
}

// Native transaction hashes cover the exact retained SCALE extrinsic bytes.
func rootExtrinsicHash(raw []byte) string {
	digest := blake2b.Sum256(raw)
	return "0x" + hex.EncodeToString(digest[:])
}

// Network contradictions always block; runtime and seat drift only prevent
// further side effects and do not prevent reconciliation of an older action.
func (self rootActionObservation) matches(action rootAction, signing bool) error {
	scope := action.Scope
	if self.NativeChain != scope.NativeChain || self.GenesisHash != scope.GenesisHash || self.EvmChainId != mainnetEvmChainId || self.Hotkey != scope.Hotkey || !rootCanonicalHash(self.FinalizedHash) || self.FinalizedNumber < action.BirthBlock || self.FinalizedNumber > math.MaxUint32 || self.FinalizedNumber == action.BirthBlock && self.FinalizedHash != action.BirthHash {
		return errors.New("root action reconciliation has a different network or invalid finalized position")
	}
	if signing && (self.StateUnavailable || self.RuntimeVersion != scope.RuntimeVersion || self.RuntimeCodeHash != scope.RuntimeCodeHash || self.RuntimeMetadataHash != scope.RuntimeMetadataHash || self.Hotkey != scope.Hotkey || self.Coldkey != scope.Coldkey || self.Seat != scope.Seat || self.AccountNonce != action.Nonce || self.FinalizedNumber >= action.BirthBlock+action.Period) {
		return errors.New("root action runtime, seat, nonce or mortal window no longer admits a side effect")
	}
	return nil
}

// Requires exact coverage, byte inclusion and outcome evidence; apparent nonce
// consumption by another action cannot be turned into successful local expiry.
func (self rootActionReconciliation) validate(action rootAction, raw []byte) error {
	if err := self.Observation.matches(action, false); err != nil {
		return err
	}
	through := min(self.Observation.FinalizedNumber, action.BirthBlock+action.Period-1)
	if self.AnchorHash != action.BirthHash || self.CheckedFrom != action.BirthBlock+1 || self.CheckedThrough < through || self.CheckedThrough > self.Observation.FinalizedNumber {
		return errors.New("root action finalized history is missing, discontinuous or has another era anchor")
	}
	if self.Receipt != nil {
		receipt := self.Receipt
		if len(raw) == 0 || receipt.RawExtrinsic != "0x"+hex.EncodeToString(raw) || !rootCanonicalHash(receipt.BlockHash) || receipt.BlockNumber <= action.BirthBlock || receipt.BlockNumber >= action.BirthBlock+action.Period || receipt.BlockNumber > self.CheckedThrough || !rootCanonicalHash(receipt.EventHash) || receipt.Success && receipt.DispatchError != "" || !receipt.Success && receipt.DispatchError == "" {
			return errors.New("root action receipt lacks exact bytes, finalized inclusion or dispatch evidence")
		}
		if receipt.BlockNumber == self.Observation.FinalizedNumber && receipt.BlockHash != self.Observation.FinalizedHash || !rootCanonicalHash(receipt.ExecutionCodeHash) || !rootCanonicalHash(receipt.ExecutionMetadataHash) || receipt.ExecutionRuntimeVersion.SpecName == "" || receipt.ExecutionRuntimeVersion.SpecVersion == 0 || receipt.ExecutionRuntimeVersion.TransactionVersion == 0 || receipt.ExecutionRuntimeVersion.StateVersion == 0 {
			return errors.New("root receipt execution runtime or canonical inclusion hash is incomplete")
		}
	}
	return nil
}

// Rechecks persisted signed bytes, phase and terminal evidence on every open.
func (self rootActionRecord) validate() error {
	if self.Schema != rootActionStateSchema {
		return errors.New("unknown root action state schema")
	}
	if err := self.Action.validate(); err != nil {
		return err
	}
	claimed := self.ContentHash
	self.ContentHash = ""
	if claimed != rootObjectHash(self) || self.Broadcasts > self.Action.Scope.MaxBroadcasts || self.LastFinalized == 0 && self.LastFinalizedHash != "" || self.LastFinalized != 0 && (!rootCanonicalHash(self.LastFinalizedHash) || self.LastFinalized < self.Action.BirthBlock) {
		return errors.New("root action state checksum, position or attempt bound differs")
	}
	unsigned := self.Phase == "reserved" || self.Phase == "signing"
	if unsigned {
		if self.Signature != "" || self.RawExtrinsic != "" || self.ExtrinsicHash != "" || self.Broadcasts != 0 || self.Reconciliation != nil {
			return errors.New("unsigned root action carries signed or terminal state")
		}
		return nil
	}
	signature, err := hex.DecodeString(self.Signature)
	if err != nil {
		return err
	}
	raw, err := self.Action.signed(signature)
	if err != nil || self.RawExtrinsic != "0x"+hex.EncodeToString(raw) || self.ExtrinsicHash != rootExtrinsicHash(raw) {
		return errors.Join(errors.New("root action signed bytes differ"), err)
	}
	switch self.Phase {
	case "signed", "pending":
		if self.Reconciliation != nil || self.Phase == "signed" && self.Broadcasts != 0 || self.Phase == "pending" && self.Broadcasts == 0 {
			return errors.New("root action pending/attempt state differs")
		}
	case "finalized", "dispatch-failed", "fee-overrun", "runtime-deviation", "expired":
		if self.Reconciliation == nil {
			return errors.New("terminal root action has no finalized evidence")
		}
		if err := self.Reconciliation.validate(self.Action, raw); err != nil {
			return err
		}
		if self.Phase != rootTerminalPhase(self.Action, *self.Reconciliation) || self.LastFinalized != self.Reconciliation.Observation.FinalizedNumber || self.LastFinalizedHash != self.Reconciliation.Observation.FinalizedHash {
			return errors.New("root action terminal phase contradicts finalized evidence")
		}
	default:
		return errors.New("unknown root action phase")
	}
	return nil
}

// Fee overrun is retained as a real outcome, never hidden by rejecting evidence.
func rootTerminalPhase(action rootAction, result rootActionReconciliation) string {
	if result.Receipt != nil {
		if result.Receipt.ExecutionRuntimeVersion != action.Scope.RuntimeVersion || result.Receipt.ExecutionCodeHash != action.Scope.RuntimeCodeHash || result.Receipt.ExecutionMetadataHash != action.Scope.RuntimeMetadataHash {
			return "runtime-deviation"
		}
		if result.Receipt.ActualFeeRao > action.Scope.FeeReserveRao {
			return "fee-overrun"
		}
		if !result.Receipt.Success {
			return "dispatch-failed"
		}
		return "finalized"
	}
	if !result.Observation.StateUnavailable && result.Observation.FinalizedNumber >= action.BirthBlock+action.Period && result.Observation.AccountNonce == action.Nonce {
		return "expired"
	}
	return ""
}

// Failure after rename may already be durable; no further side effect is safe
// in this process until reopening resolves which complete record survived.
func (self *rootActionOwner) persist(record rootActionRecord) error {
	record.ContentHash = ""
	record.ContentHash = rootObjectHash(record)
	if err := record.validate(); err != nil {
		return err
	}
	if err := self.store.save(record); err != nil {
		self.poisoned = true
		return err
	}
	return nil
}

// Ports borrow independent vectors and cannot mutate the owner's retained call.
func copyRootAction(action rootAction) rootAction {
	action.Dests = append([]uint16(nil), action.Dests...)
	action.Weights = append([]uint16(nil), action.Weights...)
	return action
}

// Advances one durable boundary; callers may retry pending errors without
// replacing the request, reassigning its nonce, or spending a fresh allowance.
func (self *rootActionOwner) step(ctx context.Context) (rootActionStep, error) {
	if self.poisoned || self.store == nil {
		return rootActionStep{Status: "blocked"}, errors.New("root action owner must be reopened after unavailable or ambiguous storage")
	}
	record, err := self.store.load()
	if err != nil {
		self.poisoned = true
		return rootActionStep{Status: "blocked"}, err
	}
	if err := record.validate(); err != nil {
		self.poisoned = true
		return rootActionStep{Status: "blocked"}, err
	}
	result := rootActionStep{Phase: record.Phase, Status: "pending"}
	if record.Reconciliation != nil {
		result.Status = "complete"
		return result, nil
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	issueSignature := record.Phase == "reserved"
	if record.Phase == "signing" {
		if self.signer == nil {
			result.Status = "blocked"
			return result, errors.New("root custody recovery adapter is absent")
		}
		signature, err := self.signer.recoverSignature(ctx, record.Action.RequestHash)
		if err == errRootSignatureNotIssued && len(signature) == 0 {
			issueSignature = true
		} else if err != nil || len(signature) == 0 {
			return result, errors.Join(errors.New("root signing outcome remains unknown; no replacement signature is allowed"), err)
		} else {
			return self.retainSignature(record, signature)
		}
	}
	if self.chain == nil {
		result.Status = "blocked"
		return result, errors.New("root canonical receipt and submission adapter is absent")
	}
	raw, _ := hex.DecodeString(strings.TrimPrefix(record.RawExtrinsic, "0x"))
	reconciliation, err := self.chain.reconcile(ctx, copyRootAction(record.Action), append([]byte(nil), raw...))
	if err != nil {
		return result, err
	}
	if err := reconciliation.validate(record.Action, raw); err != nil {
		result.Status = "blocked"
		return result, err
	}
	observed := reconciliation.Observation
	if observed.FinalizedNumber < record.LastFinalized || observed.FinalizedNumber == record.LastFinalized && record.LastFinalizedHash != "" && record.LastFinalizedHash != observed.FinalizedHash {
		result.Status = "blocked"
		return result, errors.New("root action finalized continuity changed")
	}
	if record.LastFinalized != observed.FinalizedNumber || record.LastFinalizedHash != observed.FinalizedHash {
		record.LastFinalized, record.LastFinalizedHash = observed.FinalizedNumber, observed.FinalizedHash
		if err := self.persist(record); err != nil {
			return result, err
		}
	}
	if len(raw) != 0 {
		if phase := rootTerminalPhase(record.Action, reconciliation); phase != "" {
			record.Phase, record.Reconciliation = phase, &reconciliation
			if err := self.persist(record); err != nil {
				return result, err
			}
			return rootActionStep{Phase: phase, Status: "complete"}, nil
		}
	}
	if err := observed.matches(record.Action, true); err != nil {
		result.Status = "blocked"
		return result, err
	}
	if self.authority == nil {
		result.Status = "blocked"
		return result, errors.New("root independent authority/eligibility/custody admission adapter is absent")
	}
	if err := self.authority.authorize(ctx, copyRootAction(record.Action), observed); err != nil {
		result.Status = "blocked"
		return result, err
	}
	if issueSignature {
		if self.signer == nil {
			result.Status = "blocked"
			return result, errors.New("root idempotent custody signer is absent")
		}
		record.Phase = "signing"
		if err := self.persist(record); err != nil {
			return result, err
		}
		signature, err := self.signer.signOnce(ctx, copyRootAction(record.Action))
		if err != nil {
			return rootActionStep{Phase: "signing", Status: "pending"}, err
		}
		return self.retainSignature(record, signature)
	}
	if record.Broadcasts >= record.Action.Scope.MaxBroadcasts {
		result.Status = "blocked"
		return result, errors.New("root broadcast bound is exhausted; receipt reconciliation remains active")
	}
	record.Broadcasts++
	record.Phase = "pending"
	if err := self.persist(record); err != nil {
		return result, err
	}
	// An ambiguous send retains its exact bytes and reservation. Never classify
	// dropped/invalid/usurped/subscription timeout as a finalized dispatch result.
	err = self.chain.submit(ctx, append([]byte(nil), raw...))
	return rootActionStep{Phase: "pending", Status: "pending"}, err
}

// Persistence succeeds before any possible broadcast. Signature replacement is
// impossible once the record leaves its signing phase.
func (self *rootActionOwner) retainSignature(record rootActionRecord, signature []byte) (rootActionStep, error) {
	raw, err := record.Action.signed(signature)
	if err != nil {
		return rootActionStep{Phase: "signing", Status: "blocked"}, err
	}
	if record.Phase != "signing" {
		return rootActionStep{Phase: record.Phase, Status: "blocked"}, errors.New("root signature returned outside its reserved signing phase")
	}
	record.Phase = "signed"
	record.Signature = hex.EncodeToString(signature)
	record.RawExtrinsic = "0x" + hex.EncodeToString(raw)
	record.ExtrinsicHash = rootExtrinsicHash(raw)
	if err := self.persist(record); err != nil {
		return rootActionStep{Phase: "signing", Status: "blocked"}, err
	}
	return rootActionStep{Phase: "signed", Status: "pending"}, nil
}
