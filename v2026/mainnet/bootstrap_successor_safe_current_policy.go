// A separately signed proposal describes a possible current-state authority
// policy. It does not replace retained history attestations or enable submission.
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/ethereum/go-ethereum/common"
)

const bootstrapSuccessorSafeCurrentPolicySchema = "urnetwork-mainnet-successor-safe-current-policy-proposal-v1"
const bootstrapSuccessorSafeCurrentPolicyDomain = "urnetwork-mainnet-successor-safe-current-policy-proposal-approval-v1"
const bootstrapSuccessorSafeCurrentPolicy = "proposed-current-authority-only;complete-finalized-safe-account-storage-prefix-and-exact-proxy-singleton-code-metadata-and-runtime;only-singleton-three-owner-links-two-of-three-threshold-nonce-and-empty-module-sentinel;all-other-storage-absent;no-deployment-or-delegatecall-history-claim;owned-rpc-finality-and-nonatomic-scoped-pending-assertions-not-complete-pending-proof;original-all-signer-cutover-no-other-live-signatures-and-relayer-transactions-required;retain-original-authority-history-receipts-signed-bytes-nonces-counted-attempts-window-fees-and-liabilities;production-submission-unavailable-pending-explicit-policy-approval-qualified-authenticator-and-custody-integration"

// Every execution/profile choice remains bound to original custody. Evidence is
// a separate reviewer input; it is neither storage proof nor a capability token.
type bootstrapSuccessorSafeCurrentPolicyAuthorization struct {
	Schema                 string             `json:"schema"`
	ExecutionPlanHash      string             `json:"execution_plan_hash"`
	CanonicalAuthorityHash string             `json:"canonical_authority_hash"`
	RuntimeRevisionHash    string             `json:"runtime_revision_hash,omitempty"`
	Safe                   common.Address     `json:"safe"`
	Singleton              common.Address     `json:"singleton"`
	Version                string             `json:"safe_version"`
	Variant                string             `json:"safe_variant"`
	SafeProxyRuntimeHash   common.Hash        `json:"safe_proxy_runtime_hash"`
	SingletonRuntimeHash   common.Hash        `json:"singleton_runtime_hash"`
	Runtime                rootReceiptProfile `json:"runtime"`
	ReviewEvidence         planFileReference  `json:"review_evidence"`
	Policy                 string             `json:"proposed_policy"`
}

// The only accepted signing key is the original independent approver's key.
type bootstrapSuccessorSafeCurrentPolicyApproval struct {
	Authorization bootstrapSuccessorSafeCurrentPolicyAuthorization `json:"authorization"`
	Signature     string                                           `json:"signature_ed25519"`
}

// The proposal's signature domain cannot reuse canonical, runtime or history
// approvals, and it explicitly carries the limitations that require review.
func (self bootstrapSuccessorSafeCurrentPolicyAuthorization) signingBytes() ([]byte, error) {
	if self.Schema != bootstrapSuccessorSafeCurrentPolicySchema || !planSha256(self.ExecutionPlanHash) || !planSha256(self.CanonicalAuthorityHash) ||
		self.RuntimeRevisionHash != "" && !planSha256(self.RuntimeRevisionHash) || self.Policy != bootstrapSuccessorSafeCurrentPolicy ||
		!bootstrapRootAbsolutePath(self.ReviewEvidence.Path) || !planSha256(self.ReviewEvidence.Sha256) ||
		self.Safe == (common.Address{}) || self.Singleton == (common.Address{}) || self.Safe == self.Singleton ||
		self.SafeProxyRuntimeHash == (common.Hash{}) || self.SingletonRuntimeHash == (common.Hash{}) ||
		!mainnetRuntimeCodecSource(self.Runtime.RuntimeSourceCommit) || self.Runtime.RuntimeVersion.SpecName == "" || self.Runtime.RuntimeVersion.SpecVersion == 0 ||
		!rootCanonicalHash(self.Runtime.RuntimeCodeHash) || !rootCanonicalHash(self.Runtime.RuntimeMetadataHash) {
		return nil, errors.New("Safe current policy proposal lacks complete explicit scope")
	}
	if _, err := loadSafeReleasePin(self.Version, self.Variant); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(self)
	return append([]byte(bootstrapSuccessorSafeCurrentPolicyDomain+"\x00"), raw...), err
}

// All retained runtime predecessors and original attestations remain required.
// This function only derives observation scope. Separate acceptance, custody
// and capability checks decide whether that scope can admit an exact send.
func (self bootstrapSuccessorSafeCurrentPolicyApproval) validate(ctx context.Context, plan bootstrapSuccessorExecutionPlan, base bootstrapSuccessorCanonicalApproval, history bootstrapSuccessorRuntimeHistory) (safeCurrentStorageScope, error) {
	var scope safeCurrentStorageScope
	p := self.Authorization
	if ctx == nil || p.ExecutionPlanHash != plan.hash() || p.CanonicalAuthorityHash != rootObjectHash(base) ||
		p.RuntimeRevisionHash != history.hash() || history.pendingHash != "" ||
		p.Safe != plan.Review.Transaction.Safe || p.Singleton != plan.Request.Singleton || p.Version != plan.Review.Request.Version || p.Variant != plan.Review.Request.Variant {
		return scope, errors.New("Safe current policy proposal changed retained execution or authority")
	}
	if err := base.validate(ctx, plan); err != nil {
		return scope, err
	}
	profiles := []rootReceiptProfile{base.Authorization.CurrentRuntime}
	references := []planFileReference{base.Authorization.SafeBuildEvidence, base.Authorization.RuntimeEvidence, base.Authorization.CutoverEvidence, base.Authorization.SafeProvenance}
	provenance, err := base.readProvenance(ctx, plan)
	if err != nil {
		return scope, err
	}
	references = append(references, provenance.Provenance.HistoryEvidence)
	for i, revision := range history.approvals {
		if err := errors.Join(revision.validate(ctx, plan, base), revision.extends(base, history.approvals[:i])); err != nil {
			return scope, err
		}
		profiles = append(profiles, revision.Authorization.Runtime)
		references = append(references, revision.Authorization.RuntimeEvidence)
	}
	if !slices.Contains(profiles, p.Runtime) {
		return scope, errors.New("Safe current policy proposal introduces unapproved runtime semantics")
	}
	message, err := p.signingBytes()
	key, keyErr := rootReceiptHex(plan.Review.Preparation.Approval.Plan.ApprovalPublicKey, ed25519.PublicKeySize)
	signature, signatureErr := rootOfflineSignatureBytes(self.Signature)
	if err != nil || keyErr != nil || signatureErr != nil || !ed25519.Verify(key, message, signature) {
		return scope, errors.Join(errors.New("Safe current policy proposal independent signature differs"), err, keyErr, signatureErr)
	}
	for _, reference := range references {
		if reference.Path == p.ReviewEvidence.Path {
			return scope, errors.New("Safe current policy proposal reuses retained evidence")
		}
	}
	for _, directory := range []string{plan.Review.Preparation.Approval.Plan.Proposal.OriginalRunDirectory, plan.Request.RegistryDirectory} {
		if p.ReviewEvidence.Path == directory || strings.HasPrefix(p.ReviewEvidence.Path, directory+"/") {
			return scope, errors.New("Safe current policy proposal evidence overlaps custody")
		}
	}
	raw, hash, err := readBootstrapRootFile(ctx, p.ReviewEvidence.Path, 1024*1024)
	if err != nil || len(raw) == 0 || hash != p.ReviewEvidence.Sha256 {
		return scope, errors.Join(errors.New("Safe current policy proposal evidence is absent or changed"), err)
	}
	scope = safeCurrentStorageScope{Safe: p.Safe, Singleton: p.Singleton, Owners: slices.Clone(plan.Request.Owners), Nonce: plan.Review.Transaction.Nonce,
		Version: p.Version, Variant: p.Variant, SafeProxyRuntimeHash: p.SafeProxyRuntimeHash, SingletonRuntimeHash: p.SingletonRuntimeHash, Runtime: p.Runtime}
	if err := errors.Join(scope.validate(), ctx.Err()); err != nil {
		return safeCurrentStorageScope{}, err
	}
	return scope, nil
}
