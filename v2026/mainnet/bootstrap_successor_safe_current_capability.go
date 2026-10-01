// The current-only capability proves finalized storage and explicitly accepts
// scoped pending assertions. It never implements the complete-history interface.
package main

import (
	"context"
	"errors"

	"github.com/ethereum/go-ethereum/common"
)

// Route installation is an internal release decision, never a public flag or
// value inferred from a signed review file. Public construction supplies zero.
type bootstrapSuccessorSafeCurrentRoute uint8

const bootstrapSuccessorSafeCurrentNativeRoute bootstrapSuccessorSafeCurrentRoute = 1

// One adapter owns the immutable scope selected from its retained acceptance.
type bootstrapSuccessorSafeCurrentCapability struct {
	revisionHash string
	scope        safeCurrentStorageScope
}

// The execution owner checks this separate capability before any reservation.
// A generic model or the original history interface cannot substitute for it.
type bootstrapSuccessorSafeCurrentExecutionChain interface {
	currentPolicyReady(context.Context, bootstrapSuccessorExecutionPlan, string) error
}

// Installation still requires both review and acceptance signatures. The exact
// native client belongs to original custody; no replacement endpoint is accepted.
func (self *bootstrapSuccessorCanonicalChain) selectCurrentPolicy(ctx context.Context, route bootstrapSuccessorSafeCurrentRoute) error {
	if self == nil || self.owner == nil {
		return errBootstrapSuccessorSafeCurrentCapabilityUnavailable
	}
	self.currentPolicy = nil
	if route == 0 {
		return nil
	}
	if route != bootstrapSuccessorSafeCurrentNativeRoute || self.provenance != nil || self.owner.safeCurrentHistory.pendingHash != "" || self.owner.runtimeHistory.pendingHash != "" || len(self.owner.safeCurrentHistory.approvals) == 0 {
		return errBootstrapSuccessorSafeCurrentCapabilityUnavailable
	}
	approval := self.owner.safeCurrentHistory.approvals[len(self.owner.safeCurrentHistory.approvals)-1]
	scope, err := approval.validate(ctx, self.owner.planCopy(), self.approval, self.owner.runtimeHistory)
	if err != nil || approval.Authorization.Proposal.Authorization.RuntimeRevisionHash != self.runtimeRevisionHash {
		return errors.Join(errors.New("successor current-policy capability lacks exact complete runtime authority"), err)
	}
	self.currentPolicy = &bootstrapSuccessorSafeCurrentCapability{revisionHash: rootObjectHash(approval), scope: scope}
	return nil
}

// Every use rechecks the owner and signed journal. Adding runtime authority does
// not silently widen an earlier current-policy acceptance to a new artifact.
func (self *bootstrapSuccessorCanonicalChain) currentPolicyReady(ctx context.Context, plan bootstrapSuccessorExecutionPlan, hash string) error {
	if self == nil || self.currentPolicy == nil || self.provenance != nil || self.owner == nil || self.currentPolicy.revisionHash != hash || hash == "" ||
		self.currentRevisionHash != hash || self.owner.safeCurrentHistory.hash() != hash || self.owner.safeCurrentHistory.pendingHash != "" || self.owner.runtimeHistory.pendingHash != "" || self.owner.pending != "" {
		return errBootstrapSuccessorSafeCurrentCapabilityUnavailable
	}
	if err := self.checkpoint(ctx, plan); err != nil {
		return err
	}
	approval := self.owner.safeCurrentHistory.approvals[len(self.owner.safeCurrentHistory.approvals)-1]
	if approval.Authorization.Proposal.Authorization.RuntimeRevisionHash != self.runtimeRevisionHash {
		return errors.New("successor current-policy capability predates retained runtime authority")
	}
	return nil
}

// The two policies remain distinct. Current proof never produces a history
// attestation; its observation retains history/pending/send booleans as false.
func (self *bootstrapSuccessorCanonicalChain) authenticateSafeAuthority(ctx context.Context, plan bootstrapSuccessorExecutionPlan, head chainIdentity) error {
	if self.currentPolicy == nil {
		return self.authenticateProvenance(ctx, plan, head)
	}
	if err := self.currentPolicyReady(ctx, plan, self.currentRevisionHash); err != nil {
		return err
	}
	proof, err := self.chain.client.readSafeCurrentStorage(ctx, self.currentPolicy.scope, head)
	if err != nil {
		return err
	}
	self.currentProof = proof
	return nil
}

// All expensive prefix, artifact and historical work precedes this last scoped
// readmission. The accepted policy explicitly acknowledges non-atomic RPC reads.
func (self *bootstrapSuccessorCanonicalChain) readmitCurrentPolicy(ctx context.Context, plan bootstrapSuccessorExecutionPlan, safeHash string, result *bootstrapSuccessorExecutionObservation) error {
	if self.currentPolicy == nil {
		return nil
	}
	if err := self.currentPolicyReady(ctx, plan, self.currentRevisionHash); err != nil {
		return err
	}
	pendingProof, err := self.readSafeCurrentPending(ctx, self.currentPolicy.scope, self.currentProof)
	if err != nil {
		return err
	}
	if _, _, err := self.contractsState(ctx, "pending", common.Address{}); err != nil {
		return err
	}
	pending, err := self.safeState(ctx, plan, "pending")
	if err != nil || rootObjectHash(pending) != safeHash {
		return errors.Join(errors.New("successor final current-policy Safe authority changed"), err)
	}
	var nonce, balance string
	for _, read := range []struct {
		method string
		value  *string
	}{{method: "eth_getTransactionCount", value: &nonce}, {method: "eth_getBalance", value: &balance}} {
		if err := self.chain.read(ctx, read.method, []any{plan.Review.Relayer.Sender.Hex(), "pending"}, read.value); err != nil {
			return err
		}
	}
	queued, nonceErr := evmQuantity(nonce, 64)
	available, balanceErr := evmQuantity(balance, 256)
	prior, priorErr := evmWei(result.RelayerBalanceWei)
	if err := errors.Join(nonceErr, balanceErr, priorErr); err != nil {
		return err
	}
	if prior.Cmp(available) < 0 {
		available = prior
	}
	result.RelayerPendingNonce, result.RelayerBalanceWei = queued.Uint64(), available.String()
	known, err := self.retainedTransactionKnown(ctx, plan)
	if err != nil || known {
		return errors.Join(errors.New("successor final current-policy transaction is pending or unresolved"), err)
	}
	// This final check fetches only identities and a code hash, never another
	// large proof/artifact. Equal reviewed code preserves its codec/metadata
	// semantics; changed same-version bytes are still explicitly refused.
	latest, err := self.identity(ctx, plan)
	if err != nil {
		return err
	}
	var codeHash string
	if err := self.chain.client.call(ctx, "state_getStorageHash", []any{runtimeCodeStorageKey, latest.FinalizedHash}, &codeHash); err != nil {
		return err
	}
	if latest.runtimeVersion != self.currentPolicy.scope.Runtime.RuntimeVersion || codeHash != self.currentPolicy.scope.Runtime.RuntimeCodeHash {
		return errors.New("successor final current-policy runtime artifact changed")
	}
	if err := self.chain.continuity(ctx, self.plans[0].Config.Plan, evmActionRecord{ScanNumber: self.currentProof.NativeNumber, ScanHash: self.currentProof.NativeHash}, latest); err != nil {
		return err
	}
	// Head advancement updates only window admission. It cannot relabel an
	// earlier storage proof as a statement about this later finalized root.
	result.SafeCurrentProof, result.SafeCurrentPending = self.currentProof, pendingProof
	result.NativeNumber, result.NativeHash = latest.FinalizedNumber, common.HexToHash(latest.FinalizedHash)
	return nil
}
