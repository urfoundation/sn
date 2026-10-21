// An independently signed authorization accepts external build and signer
// cutover evidence for one exact execution. RPC observations cannot supply it.
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"os"
	"strings"
)

const bootstrapSuccessorCanonicalSchema = "urnetwork-mainnet-successor-canonical-authorization-v1"
const bootstrapSuccessorCanonicalDomain = "urnetwork-mainnet-successor-canonical-authorization-approval-v1"
const bootstrapSuccessorCanonicalFile = bootstrapSuccessorExecutionPrefix + ".canonical-authorization"
const bootstrapSuccessorCanonicalPolicy = "reviewed-safe-build-and-current-runtime;independently-signed-safe-deployment-and-complete-storage-provenance;canonical-provenance-authenticator-required-for-send;all-safe-and-relayer-signers-fenced-to-retained-registry;no-other-live-safe-signatures-or-relayer-transactions;original-unexecuted-reservations-retained;owned-rpc-finality-and-account-pending-state-assertions"

// Evidence files are external review inputs, not automatically proved build
// reproducibility or cross-host key exclusivity. The original independent key
// explicitly accepts these assumptions under a new signature domain.
type bootstrapSuccessorCanonicalAuthorization struct {
	Schema            string             `json:"schema"`
	ExecutionPlanHash string             `json:"execution_plan_hash"`
	SafeBuildEvidence planFileReference  `json:"safe_build_evidence"`
	CurrentRuntime    rootReceiptProfile `json:"current_runtime"`
	RuntimeEvidence   planFileReference  `json:"current_runtime_evidence"`
	CutoverEvidence   planFileReference  `json:"signer_cutover_evidence"`
	SafeProvenance    planFileReference  `json:"safe_deployment_provenance"`
	Policy            string             `json:"accepted_policy"`
}

// The signer is provisioned by the original approval; it is never learned
// from this envelope, the node, or a changed execution plan.
type bootstrapSuccessorCanonicalApproval struct {
	Authorization bootstrapSuccessorCanonicalAuthorization `json:"authorization"`
	Signature     string                                   `json:"signature_ed25519"`
}

// Canonical encoding makes this signature unusable in either prior domain.
func (self bootstrapSuccessorCanonicalAuthorization) signingBytes() ([]byte, error) {
	profile := self.CurrentRuntime
	if self.Schema != bootstrapSuccessorCanonicalSchema || !planSha256(self.ExecutionPlanHash) || self.Policy != bootstrapSuccessorCanonicalPolicy ||
		!bootstrapRootAbsolutePath(self.SafeBuildEvidence.Path) || !planSha256(self.SafeBuildEvidence.Sha256) ||
		!bootstrapRootAbsolutePath(self.RuntimeEvidence.Path) || !planSha256(self.RuntimeEvidence.Sha256) ||
		!bootstrapRootAbsolutePath(self.CutoverEvidence.Path) || !planSha256(self.CutoverEvidence.Sha256) ||
		!bootstrapRootAbsolutePath(self.SafeProvenance.Path) || !planSha256(self.SafeProvenance.Sha256) ||
		self.SafeProvenance.Path == self.SafeBuildEvidence.Path || self.SafeProvenance.Path == self.RuntimeEvidence.Path || self.SafeProvenance.Path == self.CutoverEvidence.Path ||
		self.SafeBuildEvidence.Path == self.CutoverEvidence.Path || self.RuntimeEvidence.Path == self.SafeBuildEvidence.Path || self.RuntimeEvidence.Path == self.CutoverEvidence.Path ||
		!mainnetRuntimeCodecSource(profile.RuntimeSourceCommit) || profile.RuntimeVersion.SpecName == "" || profile.RuntimeVersion.SpecVersion == 0 ||
		!rootCanonicalHash(profile.RuntimeCodeHash) || !rootCanonicalHash(profile.RuntimeMetadataHash) {
		return nil, errors.New("successor canonical authorization lacks exact external review and cutover scope")
	}
	raw, err := json.Marshal(self)
	return append([]byte(bootstrapSuccessorCanonicalDomain+"\x00"), raw...), err
}

// Validation reads all independently pinned evidence files afresh. Their
// contents are attestations accepted by the signer, not RPC-derived facts.
func (self bootstrapSuccessorCanonicalApproval) validate(ctx context.Context, plan bootstrapSuccessorExecutionPlan) error {
	if ctx == nil || self.Authorization.ExecutionPlanHash != plan.hash() {
		return errors.New("successor canonical authorization changed execution scope")
	}
	message, err := self.Authorization.signingBytes()
	key, keyErr := rootReceiptHex(plan.Review.Preparation.Approval.Plan.ApprovalPublicKey, ed25519.PublicKeySize)
	signature, signatureErr := rootOfflineSignatureBytes(self.Signature)
	if err != nil || keyErr != nil || signatureErr != nil || !ed25519.Verify(key, message, signature) {
		return errors.Join(errors.New("successor canonical authorization signature differs"), err, keyErr, signatureErr)
	}
	for _, reference := range []planFileReference{self.Authorization.SafeBuildEvidence, self.Authorization.RuntimeEvidence, self.Authorization.CutoverEvidence, self.Authorization.SafeProvenance} {
		for _, directory := range []string{plan.Review.Preparation.Approval.Plan.Proposal.OriginalRunDirectory, plan.Request.RegistryDirectory} {
			if reference.Path == directory || strings.HasPrefix(reference.Path, directory+"/") {
				return errors.New("successor canonical evidence overlaps retained execution custody")
			}
		}
		raw, hash, err := readBootstrapRootFile(ctx, reference.Path, 1024*1024)
		if err != nil || len(raw) == 0 || hash != reference.Sha256 {
			return errors.Join(errors.New("successor canonical evidence is absent or changed"), err)
		}
	}
	_, err = self.readProvenance(ctx, plan)
	return errors.Join(err, ctx.Err())
}

// The first production attempt permanently binds its independent authority.
// Changed or missing completed authority cannot be repaired from another input.
func (self *bootstrapSuccessorExecutionStore) retainCanonicalAuthority(ctx context.Context, approval bootstrapSuccessorCanonicalApproval) error {
	if self == nil || self.closed {
		return errors.New("successor canonical authorization requires an open owner")
	}
	if err := approval.validate(ctx, self.planCopy()); err != nil {
		return err
	}
	hash := rootObjectHash(approval)
	floor := self.approval.Plan.Review.Preparation.Approval.Plan.Proposal.Budget.RetainedAttempts
	if self.last.CumulativeAttempts > floor && self.last.CanonicalAuthorityHash != hash {
		return errors.New("successor canonical authority differs from the retained counted attempt")
	}
	if self.last.CanonicalAuthorityHash != "" {
		raw, err := self.local.read(bootstrapSuccessorCanonicalFile)
		var retained bootstrapSuccessorCanonicalApproval
		if err != nil || decodePlanJson(raw, &retained) != nil || rootObjectHash(retained) != hash {
			return errors.Join(errors.New("successor counted canonical authorization is missing or changed"), err)
		}
	} else if _, err := self.local.read(bootstrapSuccessorCanonicalFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	raw, err := json.Marshal(approval)
	if err != nil {
		return err
	}
	if err := self.local.publish(bootstrapSuccessorCanonicalFile, "canonical-authority", raw); err != nil {
		return err
	}
	self.canonicalAuthorityHash = hash
	self.canonicalAuthority = &approval
	return self.checkpoint("canonical-authority-retained")
}
