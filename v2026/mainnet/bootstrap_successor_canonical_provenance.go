// A signed provenance statement is mandatory review input, not canonical proof
// of all storage authority. Live sends additionally require a separate history
// authenticator; no production implementation is installed in this release.
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"strings"

	"github.com/ethereum/go-ethereum/common"
)

const bootstrapSuccessorSafeProvenanceSchema = "urnetwork-mainnet-successor-safe-provenance-v1"
const bootstrapSuccessorSafeProvenanceDomain = "urnetwork-mainnet-successor-safe-provenance-approval-v1"
const bootstrapSuccessorSafeProvenancePolicy = "clean-deployment-and-initialization;complete-delegatecall-and-storage-mutation-history;no-unlisted-enabled-owner-or-module-mapping-entries"

var errBootstrapSuccessorSafeProvenanceUnavailable = errors.New("successor submission is unavailable until a canonical Safe deployment and storage-provenance authenticator is implemented")

// The original independent approver binds the exact deployed account and
// selected runtime pair to complete storage-history review at a named native
// snapshot. A linked-list census cannot supply these claims by itself.
type bootstrapSuccessorSafeProvenance struct {
	Schema                    string            `json:"schema"`
	ExecutionPlanHash         string            `json:"execution_plan_hash"`
	Safe                      common.Address    `json:"safe"`
	Version                   string            `json:"safe_version"`
	Variant                   string            `json:"safe_variant"`
	Singleton                 common.Address    `json:"singleton"`
	SafeProxyRuntimeHash      common.Hash       `json:"safe_proxy_runtime_hash"`
	SingletonRuntimeHash      common.Hash       `json:"singleton_runtime_hash"`
	DeploymentTransactionHash common.Hash       `json:"deployment_transaction_hash"`
	ThroughNativeNumber       uint64            `json:"through_native_number"`
	ThroughNativeHash         common.Hash       `json:"through_native_hash"`
	HistoryEvidence           planFileReference `json:"history_evidence"`
	Policy                    string            `json:"accepted_policy"`
}

// The key comes from original approved custody, never from this envelope.
type bootstrapSuccessorSafeProvenanceApproval struct {
	Provenance bootstrapSuccessorSafeProvenance `json:"provenance"`
	Signature  string                           `json:"signature_ed25519"`
}

// A domain-separated complete statement cannot reuse another review signature.
func (self bootstrapSuccessorSafeProvenance) signingBytes() ([]byte, error) {
	if self.Schema != bootstrapSuccessorSafeProvenanceSchema || !planSha256(self.ExecutionPlanHash) ||
		self.Safe == (common.Address{}) || self.Singleton == (common.Address{}) || self.Safe == self.Singleton ||
		self.SafeProxyRuntimeHash == (common.Hash{}) || self.SingletonRuntimeHash == (common.Hash{}) ||
		self.DeploymentTransactionHash == (common.Hash{}) || self.ThroughNativeNumber == 0 || self.ThroughNativeHash == (common.Hash{}) ||
		!bootstrapRootAbsolutePath(self.HistoryEvidence.Path) || !planSha256(self.HistoryEvidence.Sha256) || self.Policy != bootstrapSuccessorSafeProvenancePolicy {
		return nil, errors.New("successor Safe provenance lacks complete deployment and storage-history scope")
	}
	raw, err := json.Marshal(self)
	return append([]byte(bootstrapSuccessorSafeProvenanceDomain+"\x00"), raw...), err
}

// Validation authenticates the attestation and its exact evidence. Canonical
// deployment, complete internal/delegatecall history and continuation beyond
// this snapshot remain the separate authenticator's responsibility.
func (self bootstrapSuccessorSafeProvenanceApproval) validate(ctx context.Context, plan bootstrapSuccessorExecutionPlan) error {
	p := self.Provenance
	if ctx == nil || p.ExecutionPlanHash != plan.hash() || p.Safe != plan.Review.Transaction.Safe || p.Singleton != plan.Request.Singleton ||
		p.Version != plan.Review.Request.Version || p.Variant != plan.Review.Request.Variant ||
		p.ThroughNativeNumber < plan.Review.Request.StartNativeNumber ||
		p.ThroughNativeNumber == plan.Review.Request.StartNativeNumber && p.ThroughNativeHash.Hex() != plan.Review.Request.StartNativeHash {
		return errors.New("successor Safe provenance differs from the exact execution account or profile")
	}
	message, err := p.signingBytes()
	key, keyErr := rootReceiptHex(plan.Review.Preparation.Approval.Plan.ApprovalPublicKey, ed25519.PublicKeySize)
	signature, signatureErr := rootOfflineSignatureBytes(self.Signature)
	if err != nil || keyErr != nil || signatureErr != nil || !ed25519.Verify(key, message, signature) {
		return errors.Join(errors.New("successor Safe provenance independent signature differs"), err, keyErr, signatureErr)
	}
	pin, err := loadSafeReleasePin(p.Version, p.Variant)
	if err != nil {
		return err
	}
	proxyMatched, singletonMatched := false, false
	for _, artifact := range pin.Artifacts {
		proxyMatched = proxyMatched || artifact.Name == "SafeProxy" && artifact.RuntimeKeccak256 == p.SafeProxyRuntimeHash.Hex()
		singletonMatched = singletonMatched || artifact.Name == p.Variant && artifact.RuntimeKeccak256 == p.SingletonRuntimeHash.Hex()
	}
	if !proxyMatched || !singletonMatched {
		return errors.New("successor Safe provenance runtime pins differ")
	}
	for _, directory := range []string{plan.Review.Preparation.Approval.Plan.Proposal.OriginalRunDirectory, plan.Request.RegistryDirectory} {
		if p.HistoryEvidence.Path == directory || strings.HasPrefix(p.HistoryEvidence.Path, directory+"/") {
			return errors.New("successor Safe provenance history overlaps retained custody")
		}
	}
	raw, hash, err := readBootstrapRootFile(ctx, p.HistoryEvidence.Path, 1024*1024)
	if err != nil || len(raw) == 0 || hash != p.HistoryEvidence.Sha256 {
		return errors.Join(errors.New("successor Safe provenance history is absent or changed"), err)
	}
	return ctx.Err()
}

// An implementation must authenticate deployment/initialization and every
// authority-relevant storage mutation through the selected finalized state and
// scoped pending state, including entries unreachable from owner/module lists.
// It must bind the exact approved route, account, profile and attestation, and
// cannot infer success from that attestation or ordinary Safe getters alone.
// Only explicit local fixtures implement this interface until that independent
// canonical-history capability is implemented and qualified.
type bootstrapSuccessorSafeProvenanceAuthenticator interface {
	authenticate(context.Context, bootstrapSuccessorExecutionPlan, bootstrapSuccessorSafeProvenanceApproval, *evmOwnedChain, chainIdentity) error
}

// This read-only gate never treats a signed file as the missing history adapter.
func (self *bootstrapSuccessorCanonicalChain) authenticateProvenance(ctx context.Context, plan bootstrapSuccessorExecutionPlan, head chainIdentity) error {
	if self == nil || self.provenance == nil {
		return errBootstrapSuccessorSafeProvenanceUnavailable
	}
	approval, err := self.approval.readProvenance(ctx, plan)
	if err != nil {
		return err
	}
	return self.provenance.authenticate(ctx, plan, approval, self.chain, head)
}

// Both the canonical envelope and separate provenance signature bind these
// exact bytes. Swapping a report or its evidence cannot retain either authority.
func (self bootstrapSuccessorCanonicalApproval) readProvenance(ctx context.Context, plan bootstrapSuccessorExecutionPlan) (bootstrapSuccessorSafeProvenanceApproval, error) {
	var approval bootstrapSuccessorSafeProvenanceApproval
	reference := self.Authorization.SafeProvenance
	raw, hash, err := readBootstrapRootFile(ctx, reference.Path, 16*1024)
	if err != nil || hash != reference.Sha256 {
		return approval, errors.Join(errors.New("successor signed Safe provenance file is absent or changed"), err)
	}
	if err := decodePlanJson(raw, &approval); err != nil {
		return approval, err
	}
	return approval, approval.validate(ctx, plan)
}
