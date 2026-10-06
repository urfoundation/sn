// A serial owner reconciles exact retained signatures before each possible send.
// The canonical adapter is deliberately not implemented by a JSON report reader.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"slices"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

const bootstrapSuccessorExecutionEventSchema = "urnetwork-mainnet-successor-execution-event-v1"

// Canonical adapters must authenticate native/EVM inclusion, exact original
// signed transaction bytes and historical postconditions for all eight records,
// selected Safe build/release authority, evidence immutable domain, outstanding
// original reservations, and exclusive signer cutover to this registry. Their
// returned seals must identify those exact ordered original full records.
//
// observe must read one current finalized native/EVM point and scoped pending
// state under the independently accepted signer cutover. reconcile must search
// the exact retained transaction and
// authenticate any inclusion and binding, independently of window/attempt caps.
// submit must perform at most one transport write of the supplied exact bytes;
// it must not retry internally, choose a route/account, sign, or replace fees.
// The production constructor requires an independent canonical authorization;
// caller-supplied observations and the offline review cannot supply it.
type bootstrapSuccessorExecutionChain interface {
	authenticate(context.Context, bootstrapSuccessorExecutionPlan) ([]string, error)
	observe(context.Context, bootstrapSuccessorExecutionPlan) (bootstrapSuccessorExecutionObservation, error)
	reconcile(context.Context, bootstrapSuccessorExecutionPlan) (bootstrapSuccessorExecutionReconciliation, error)
	submit(context.Context, bootstrapSuccessorExecutionPlan, []byte) error
}

// These are already authenticated adapter results, never decoded CLI inputs.
// Safe inner uint256 nonce and outer uint64 account nonces remain separate.
type bootstrapSuccessorExecutionObservation struct {
	NativeNumber         uint64
	NativeHash           common.Hash
	SafeProxyRuntimeHash common.Hash
	Singleton            common.Address
	SingletonRuntimeHash common.Hash
	Owners               []common.Address
	Threshold            uint64
	Modules              []common.Address
	Guard                common.Address
	ModuleGuard          common.Address
	FallbackHandler      common.Address
	SafeNonce            string
	PendingSafeDigests   []common.Hash
	RelayerNonce         uint64
	RelayerPendingNonce  uint64
	RelayerBalanceWei    string
	CoordinatorOwner     common.Address
	CoordinatorEvidence  common.Address
	EvidenceRuntimeHash  string
	EvidenceGetterHash   string
	// NativeNumber/Hash describe the latest admission window. These separate
	// observations retain the older exact proof root and its pending binding.
	SafeCurrentProof   *safeCurrentStorageObservation
	SafeCurrentPending *safeCurrentPendingObservation
}

// Inclusion contains the full Safe log witness and independently read exact
// coordinator/evidence state at inclusion. Gas estimates never release money.
type bootstrapSuccessorExecutionReceipt struct {
	NativeNumber        uint64               `json:"native_number"`
	NativeHash          common.Hash          `json:"native_hash"`
	Receipt             safeExecutionReceipt `json:"evm_receipt"`
	CoordinatorEvidence common.Address       `json:"coordinator_evidence"`
	EvidenceRuntimeHash string               `json:"evidence_runtime_hash"`
	EvidenceGetterHash  string               `json:"evidence_getter_hash"`
}

// Absence permits admission of the same signed transaction only. An unavailable
// canonical/pending search is an error, never absence or a fresh nonce.
type bootstrapSuccessorExecutionReconciliation struct {
	Status  string
	Receipt *bootstrapSuccessorExecutionReceipt
}

// Every snapshot conserves original attempts and full maximum liabilities.
// The predecessor seal prevents reordered or spliced custody from replaying.
type bootstrapSuccessorExecutionEvent struct {
	Schema                  string                              `json:"schema"`
	ApprovalHash            string                              `json:"approval_hash"`
	Sequence                uint16                              `json:"sequence"`
	PreviousHash            string                              `json:"previous_hash"`
	Phase                   string                              `json:"phase"`
	CumulativeAttempts      uint16                              `json:"cumulative_attempts"`
	ReservedLifetimeWei     string                              `json:"reserved_lifetime_wei"`
	Receipt                 *bootstrapSuccessorExecutionReceipt `json:"receipt,omitempty"`
	CanonicalAuthorityHash  string                              `json:"canonical_authority_hash,omitempty"`
	RuntimeRevisionHash     string                              `json:"runtime_revision_hash,omitempty"`
	SafeCurrentRevisionHash string                              `json:"safe_current_revision_hash,omitempty"`
	ContentHash             string                              `json:"content_hash"`
}

// A fresh reservation is fully determined by the prior immutable event. Even
// an interrupted pre-send reservation consumes capacity until separate migration.
func (self *bootstrapSuccessorExecutionStore) attemptEvent() bootstrapSuccessorExecutionEvent {
	event := bootstrapSuccessorExecutionEvent{Schema: bootstrapSuccessorExecutionEventSchema, ApprovalHash: rootObjectHash(self.approval),
		Sequence: self.last.Sequence + 1, PreviousHash: self.last.ContentHash, Phase: "attempt-reserved",
		CumulativeAttempts: self.last.CumulativeAttempts + 1, ReservedLifetimeWei: self.last.ReservedLifetimeWei,
		CanonicalAuthorityHash: self.canonicalAuthorityHash, RuntimeRevisionHash: self.runtimeHistory.hash(), SafeCurrentRevisionHash: self.safeCurrentHistory.hash()}
	event.ContentHash = rootObjectHash(event)
	return event
}

// Inner success and exact evidence binding, not outer status alone, establish
// installation. A reverted outer transaction keeps its still-valid Safe claim.
func (self bootstrapSuccessorExecutionReceipt) phase(plan bootstrapSuccessorExecutionPlan, profile *safeExecutionProfile) (string, error) {
	start := plan.Review.Request
	if self.NativeNumber < start.StartNativeNumber || self.NativeHash == (common.Hash{}) ||
		self.NativeNumber == start.StartNativeNumber && self.NativeHash.Hex() != start.StartNativeHash {
		return "", errors.New("successor execution inclusion contradicts the approved native point")
	}
	outcome, err := profile.classifyReceipt(plan.transaction(), plan.TransactionHash, self.Receipt)
	if err != nil {
		return "", err
	}
	if outcome.Outcome == "outer-reverted" {
		return "outer-reverted", nil
	}
	evidence := plan.Review.Preparation.Approval.Plan.Proposal.AdoptedActions[7].Receipt
	if outcome.Outcome != "safe-inner-success" || self.CoordinatorEvidence != plan.Review.Preparation.Approval.Plan.Proposal.Anchor.Evidence ||
		self.EvidenceRuntimeHash != evidence.RuntimeHash || self.EvidenceGetterHash != evidence.GetterHash || !rootCanonicalHash(evidence.RuntimeHash) || !planSha256(evidence.GetterHash) {
		return "", errors.New("successor canonical inner success lacks the exact coordinator/evidence domain binding")
	}
	// The getter alone can describe a different binding operation. Require the
	// one-shot event in this exact transaction before its Safe success outcome.
	anchor := plan.Review.Preparation.Approval.Plan.Proposal.Anchor
	eventId := crypto.Keccak256Hash([]byte("ValidatorEvidenceFixed(address)"))
	matched, succeeded := false, false
	for _, log := range self.Receipt.Logs {
		if log.Address == plan.Review.Transaction.Safe && len(log.Topics) == 2 && log.Topics[0] == profile.contractAbi.Events["ExecutionSuccess"].ID && log.Topics[1] == outcome.Digest {
			succeeded = true
		}
		if log.Address != anchor.Coordinator || len(log.Topics) == 0 || log.Topics[0] != eventId {
			continue
		}
		if matched || succeeded || len(log.Topics) != 2 || log.Topics[1] != common.BytesToHash(anchor.Evidence[:]) || len(log.Data) != 0 {
			return "", errors.New("successor evidence anchor event differs from the exact one-shot binding")
		}
		matched = true
	}
	if !matched {
		return "", errors.New("successor evidence anchor event is absent from the exact Safe execution")
	}
	return "installed", nil
}

// Persisted transitions are checked afresh on every reopen, including receipt
// shape and inner success. Canonicality is reauthenticated by the adapter later.
func (self bootstrapSuccessorExecutionEvent) validate(approval bootstrapSuccessorExecutionApproval, profile *safeExecutionProfile, previous *bootstrapSuccessorExecutionEvent) error {
	claimed := self.ContentHash
	self.ContentHash = ""
	budget := approval.Plan.Review.Preparation.Approval.Plan.Proposal.Budget
	if self.Schema != bootstrapSuccessorExecutionEventSchema || self.ApprovalHash != rootObjectHash(approval) || !planSha256(claimed) || rootObjectHash(self) != claimed ||
		self.ReservedLifetimeWei != approval.Plan.Review.Relayer.CumulativeLiabilityWei || self.CumulativeAttempts < budget.RetainedAttempts || self.CumulativeAttempts > budget.ProposedMaximumCumulativeAttempts {
		return errors.New("successor execution event lost approval, seal or cumulative floors")
	}
	if previous == nil {
		if self.Sequence != 0 || self.PreviousHash != self.ApprovalHash || self.Phase != "adopted" || self.Receipt != nil || self.CumulativeAttempts != budget.RetainedAttempts || self.CanonicalAuthorityHash != "" || self.RuntimeRevisionHash != "" || self.SafeCurrentRevisionHash != "" {
			return errors.New("successor execution lacks its exact original adoption event")
		}
		return nil
	}
	if self.CanonicalAuthorityHash != "" && !planSha256(self.CanonicalAuthorityHash) || previous.CanonicalAuthorityHash != "" && self.CanonicalAuthorityHash != previous.CanonicalAuthorityHash {
		return errors.New("successor execution event changed counted canonical authority")
	}
	if self.RuntimeRevisionHash != "" && (!planSha256(self.RuntimeRevisionHash) || self.CanonicalAuthorityHash == "") {
		return errors.New("successor execution event has unscoped runtime revision authority")
	}
	if self.SafeCurrentRevisionHash != "" && (!planSha256(self.SafeCurrentRevisionHash) || self.CanonicalAuthorityHash == "") {
		return errors.New("successor execution event has unscoped current-policy authority")
	}
	if self.Sequence != previous.Sequence+1 || self.PreviousHash != previous.ContentHash || previous.Phase != "adopted" && previous.Phase != "attempt-reserved" {
		return errors.New("successor execution event changed predecessor or reopened terminal custody")
	}
	if self.Phase == "attempt-reserved" {
		if self.Receipt != nil || self.CumulativeAttempts != previous.CumulativeAttempts+1 {
			return errors.New("successor execution attempt did not retain its cumulative reservation")
		}
		return nil
	}
	if self.Receipt == nil || self.CumulativeAttempts != previous.CumulativeAttempts || previous.Phase != "attempt-reserved" {
		return errors.New("successor execution outcome lacks its prior counted attempt")
	}
	phase, err := self.Receipt.phase(approval.Plan, profile)
	if err != nil || self.Phase != phase {
		return errors.Join(errors.New("successor execution terminal event differs from its canonical outcome"), err)
	}
	return nil
}

// Current authority, nonce and funding checks are deliberately separate from
// historical receipt reconciliation. Expiry can refuse sending but not custody.
func (self bootstrapSuccessorExecutionPlan) admit(observation bootstrapSuccessorExecutionObservation) error {
	r := self.Review
	p := r.Preparation.Approval.Plan.Proposal
	if observation.NativeHash == (common.Hash{}) || observation.NativeNumber < r.Request.StartNativeNumber || observation.NativeNumber > r.Request.ValidThroughNative ||
		observation.NativeNumber == r.Request.StartNativeNumber && observation.NativeHash.Hex() != r.Request.StartNativeHash ||
		observation.Singleton != self.Request.Singleton || !slices.Equal(observation.Owners, self.Request.Owners) || observation.Threshold != 2 ||
		len(observation.Modules) != 0 || observation.Guard != (common.Address{}) || observation.ModuleGuard != (common.Address{}) || observation.FallbackHandler != (common.Address{}) ||
		len(observation.PendingSafeDigests) != 0 || observation.SafeNonce != r.Transaction.Nonce ||
		observation.RelayerNonce != r.Relayer.Nonce || observation.RelayerPendingNonce != r.Relayer.Nonce ||
		observation.CoordinatorOwner != r.Transaction.Safe || observation.CoordinatorEvidence != (common.Address{}) ||
		observation.EvidenceRuntimeHash != p.AdoptedActions[7].Receipt.RuntimeHash || observation.EvidenceGetterHash != p.AdoptedActions[7].Receipt.GetterHash ||
		!rootCanonicalHash(observation.EvidenceRuntimeHash) || !planSha256(observation.EvidenceGetterHash) {
		return errors.New("successor execution current Safe authority, independent nonces, evidence state or native window differs")
	}
	profile, err := loadSafeReleasePin(r.Request.Version, r.Request.Variant)
	if err != nil {
		return err
	}
	proxyMatched, singletonMatched := false, false
	for _, artifact := range profile.Artifacts {
		proxyMatched = proxyMatched || artifact.Name == "SafeProxy" && artifact.RuntimeKeccak256 == observation.SafeProxyRuntimeHash.Hex()
		singletonMatched = singletonMatched || artifact.Name == r.Request.Variant && artifact.RuntimeKeccak256 == observation.SingletonRuntimeHash.Hex()
	}
	if !proxyMatched || !singletonMatched {
		return errors.New("successor execution current Safe proxy or singleton runtime differs")
	}
	needed, _ := evmWei(r.Relayer.MaximumLiabilityWei)
	for _, original := range p.OriginalUnexecutedActions {
		if original.Sender == r.Relayer.Sender {
			tx, err := original.unsigned()
			if err != nil {
				return err
			}
			needed.Add(needed, new(big.Int).Add(tx.Value(), new(big.Int).Mul(new(big.Int).SetUint64(tx.Gas()), tx.GasFeeCap())))
		}
	}
	balance, err := evmWei(observation.RelayerBalanceWei)
	if err != nil || needed.BitLen() > 256 || balance.Cmp(needed) < 0 {
		return errors.Join(errors.New("successor execution funding does not preserve original and outer maximum liabilities"), err)
	}
	return nil
}

// Retained completion is reported only after fresh canonical reconciliation.
// No behavior in this machine upgrades an offline claim into live authority.
type bootstrapSuccessorExecutionResult struct {
	PlanHash                   string `json:"execution_plan_hash"`
	Status                     string `json:"status"`
	CumulativeAttempts         uint16 `json:"cumulative_attempts"`
	ReservedLifetimeWei        string `json:"reserved_lifetime_wei"`
	ExecutionApprovalVerified  bool   `json:"execution_approval_verified"`
	LocalCustodyComplete       bool   `json:"local_custody_complete"`
	CanonicalAdoptionVerified  bool   `json:"canonical_adoption_verified"`
	SubmissionAttempted        bool   `json:"submission_attempted"`
	InstallationComplete       bool   `json:"installation_complete"`
	ActivationReady            bool   `json:"activation_ready"`
	RuntimeRevisionHash        string `json:"runtime_revision_hash,omitempty"`
	RuntimeRevisionCount       int    `json:"runtime_revision_count,omitempty"`
	RuntimeRevisionPending     bool   `json:"runtime_revision_pending,omitempty"`
	SafeCurrentRevisionHash    string `json:"safe_current_revision_hash,omitempty"`
	SafeCurrentRevisionCount   int    `json:"safe_current_revision_count,omitempty"`
	SafeCurrentRevisionPending bool   `json:"safe_current_revision_pending,omitempty"`
	PhysicalRebindPending      bool   `json:"physical_rebind_pending,omitempty"`
}

// Adapters receive independent copies so their nested slices cannot mutate the
// owner, the expected seal census or a later approval comparison.
func (self *bootstrapSuccessorExecutionStore) planCopy() bootstrapSuccessorExecutionPlan {
	raw, _ := json.Marshal(self.approval.Plan)
	var copied bootstrapSuccessorExecutionPlan
	_ = json.Unmarshal(raw, &copied)
	return copied
}

// Local status cannot infer canonical success from a retained terminal record.
func (self *bootstrapSuccessorExecutionStore) result() bootstrapSuccessorExecutionResult {
	return bootstrapSuccessorExecutionResult{PlanHash: self.approval.Plan.hash(), Status: "execution-custody-retained-canonical-adapter-required",
		CumulativeAttempts: self.last.CumulativeAttempts, ReservedLifetimeWei: self.last.ReservedLifetimeWei, ExecutionApprovalVerified: true, LocalCustodyComplete: !self.deferredLocalRebind, PhysicalRebindPending: self.deferredLocalRebind,
		RuntimeRevisionHash: self.runtimeHistory.hash(), RuntimeRevisionCount: len(self.runtimeHistory.approvals), RuntimeRevisionPending: self.runtimeHistory.pendingHash != "",
		SafeCurrentRevisionHash: self.safeCurrentHistory.hash(), SafeCurrentRevisionCount: len(self.safeCurrentHistory.approvals), SafeCurrentRevisionPending: self.safeCurrentHistory.pendingHash != ""}
}

// One invocation performs at most one send. Any unavailable read, pending
// outcome, exhausted allowance or expired window leaves all custody in place.
// All errors terminate ownership, so a caller cannot reuse stale admission.
func advanceBootstrapSuccessorExecution(ctx context.Context, self *bootstrapSuccessorExecutionStore, chain bootstrapSuccessorExecutionChain, submit bool) (result bootstrapSuccessorExecutionResult, resultErr error) {
	if ctx == nil || self == nil || self.closed || chain == nil {
		return result, errors.Join(errors.New("successor execution requires an open owner and canonical adapter"), self.close())
	}
	result = self.result()
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, self.close())
		}
	}()
	if err := errors.Join(ctx.Err(), self.checkpoint("canonical-adoption-begin")); err != nil {
		return result, err
	}
	plan := self.planCopy()
	seals, err := chain.authenticate(ctx, self.planCopy())
	if err != nil {
		return result, err
	}
	if len(seals) != 8 {
		return result, errors.New("successor canonical adoption omitted an original receipt")
	}
	for i, action := range plan.Review.Preparation.Approval.Plan.Proposal.AdoptedActions {
		if seals[i] != action.CustodyHash {
			return result, errors.New("successor canonical adoption changed original receipt or predecessor custody")
		}
	}
	result.CanonicalAdoptionVerified = true
	reconciliation, err := chain.reconcile(ctx, self.planCopy())
	if err != nil {
		return result, err
	}
	if reconciliation.Status == "included" {
		if reconciliation.Receipt == nil {
			return result, errors.New("successor canonical inclusion has no receipt")
		}
		phase, err := reconciliation.Receipt.phase(plan, self.profile)
		if err != nil {
			return result, err
		}
		if self.last.Phase == "installed" || self.last.Phase == "outer-reverted" {
			if self.last.Phase != phase || rootObjectHash(self.last.Receipt) != rootObjectHash(reconciliation.Receipt) {
				return result, errors.New("successor retained terminal receipt changed canonical identity")
			}
		} else {
			event := bootstrapSuccessorExecutionEvent{Schema: bootstrapSuccessorExecutionEventSchema, ApprovalHash: rootObjectHash(self.approval),
				Sequence: self.last.Sequence + 1, PreviousHash: self.last.ContentHash, Phase: phase,
				CumulativeAttempts: self.last.CumulativeAttempts, ReservedLifetimeWei: self.last.ReservedLifetimeWei, Receipt: reconciliation.Receipt,
				CanonicalAuthorityHash: self.last.CanonicalAuthorityHash, RuntimeRevisionHash: self.runtimeHistory.hash(), SafeCurrentRevisionHash: self.last.SafeCurrentRevisionHash}
			event.ContentHash = rootObjectHash(event)
			if err := self.append(event); err != nil {
				return result, err
			}
		}
		if err := self.checkpoint("canonical-result-retained"); err != nil {
			return result, err
		}
		result.Status, result.InstallationComplete = phase, phase == "installed"
		result.LocalCustodyComplete, result.PhysicalRebindPending = !self.deferredLocalRebind, self.deferredLocalRebind
		return result, nil
	}
	if reconciliation.Receipt != nil || reconciliation.Status != "absent" && reconciliation.Status != "pending" {
		return result, errors.New("successor canonical reconciliation has an invalid disposition")
	}
	if self.last.Phase == "installed" || self.last.Phase == "outer-reverted" || self.pending != "" || self.deferredLocalRebind {
		return result, errors.New("successor retained or interrupted outcome is not canonically reconciled")
	}
	result.Status = "signature-retained-" + reconciliation.Status
	if !submit || reconciliation.Status == "pending" {
		return result, nil
	}
	if self.runtimeHistory.pendingHash != "" {
		return result, errors.New("successor execution cannot send with an incomplete runtime revision")
	}
	if self.safeCurrentHistory.pendingHash != "" {
		return result, errors.New("successor execution cannot send with an incomplete current-policy revision")
	}
	if self.safeCurrentHistory.hash() != "" {
		current, ok := chain.(bootstrapSuccessorSafeCurrentExecutionChain)
		if !ok {
			return result, errBootstrapSuccessorSafeCurrentCapabilityUnavailable
		}
		if err := current.currentPolicyReady(ctx, self.planCopy(), self.safeCurrentHistory.hash()); err != nil {
			return result, err
		}
	}
	observation, err := chain.observe(ctx, self.planCopy())
	if err != nil {
		return result, err
	}
	if err := plan.admit(observation); err != nil {
		return result, err
	}
	if err := self.append(self.attemptEvent()); err != nil {
		return result, err
	}
	result.CumulativeAttempts = self.last.CumulativeAttempts
	// Publication can outlive the native window or account state. A second
	// complete admission follows the durable attempt and precedes the one send.
	observation, err = chain.observe(ctx, self.planCopy())
	if err != nil {
		return result, err
	}
	if err := errors.Join(ctx.Err(), plan.admit(observation), self.checkpoint("before-exact-send")); err != nil {
		return result, err
	}
	result.SubmissionAttempted, result.Status = true, "submission-outcome-unresolved"
	err = chain.submit(ctx, self.planCopy(), common.FromHex(plan.SignedRelayer))
	return result, err
}
