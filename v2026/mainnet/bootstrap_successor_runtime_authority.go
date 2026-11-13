// Additive runtime approvals preserve the original canonical authority and
// exact execution. A new artifact never supplies a nonce, attempt or Safe policy.
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"strings"
)

const bootstrapSuccessorRuntimeSchema = "urnetwork-mainnet-successor-runtime-revision-v1"
const bootstrapSuccessorRuntimeDomain = "urnetwork-mainnet-successor-runtime-revision-approval-v1"
const bootstrapSuccessorRuntimePolicy = "add-one-independently-reviewed-runtime-artifact-and-existing-codec;retain-original-canonical-authority-and-all-predecessors;unchanged-execution-signed-bytes-safe-provenance-signers-nonces-window-attempts-fees-and-liabilities;no-provisional-runtime-compatibility"
const maximumBootstrapSuccessorRuntimeBytes = 16 * 1024

// Sequence and predecessor bind a single immutable chain under the original
// independent key. RuntimeSourceCommit names reviewed codec semantics, not
// source-to-Wasm provenance; the separate pinned evidence supplies that review.
type bootstrapSuccessorRuntimeAuthorization struct {
	Schema                 string             `json:"schema"`
	ExecutionPlanHash      string             `json:"execution_plan_hash"`
	CanonicalAuthorityHash string             `json:"canonical_authority_hash"`
	Sequence               uint16             `json:"sequence"`
	PreviousHash           string             `json:"previous_hash"`
	Runtime                rootReceiptProfile `json:"runtime"`
	RuntimeEvidence        planFileReference  `json:"runtime_evidence"`
	Policy                 string             `json:"accepted_policy"`
}

// The envelope carries no replacement signing key or execution authority.
type bootstrapSuccessorRuntimeApproval struct {
	Authorization bootstrapSuccessorRuntimeAuthorization `json:"authorization"`
	Signature     string                                 `json:"signature_ed25519"`
}

// Domain-separated canonical bytes cannot reuse an execution or base signature.
func (self bootstrapSuccessorRuntimeAuthorization) signingBytes() ([]byte, error) {
	profile := self.Runtime
	if self.Schema != bootstrapSuccessorRuntimeSchema || !planSha256(self.ExecutionPlanHash) || !planSha256(self.CanonicalAuthorityHash) ||
		self.Sequence == 0 || !planSha256(self.PreviousHash) || self.Policy != bootstrapSuccessorRuntimePolicy ||
		!bootstrapRootAbsolutePath(self.RuntimeEvidence.Path) || !planSha256(self.RuntimeEvidence.Sha256) ||
		!mainnetRuntimeCodecSource(profile.RuntimeSourceCommit) || profile.RuntimeVersion.SpecName == "" || profile.RuntimeVersion.SpecVersion == 0 ||
		!rootCanonicalHash(profile.RuntimeCodeHash) || !rootCanonicalHash(profile.RuntimeMetadataHash) {
		return nil, errors.New("successor runtime revision lacks exact additive artifact authority")
	}
	raw, err := json.Marshal(self)
	return append([]byte(bootstrapSuccessorRuntimeDomain+"\x00"), raw...), err
}

// A valid signature still requires unchanged private review evidence and exact
// base scope. Node observations cannot create or extend this authorization.
func (self bootstrapSuccessorRuntimeApproval) validate(ctx context.Context, plan bootstrapSuccessorExecutionPlan, base bootstrapSuccessorCanonicalApproval) error {
	if ctx == nil || self.Authorization.ExecutionPlanHash != plan.hash() || self.Authorization.CanonicalAuthorityHash != rootObjectHash(base) || base.Authorization.ExecutionPlanHash != plan.hash() {
		return errors.New("successor runtime revision changed execution or base authority")
	}
	message, err := self.Authorization.signingBytes()
	key, keyErr := rootReceiptHex(plan.Review.Preparation.Approval.Plan.ApprovalPublicKey, ed25519.PublicKeySize)
	signature, signatureErr := rootOfflineSignatureBytes(self.Signature)
	if err != nil || keyErr != nil || signatureErr != nil || !ed25519.Verify(key, message, signature) {
		return errors.Join(errors.New("successor runtime revision signature differs"), err, keyErr, signatureErr)
	}
	reference := self.Authorization.RuntimeEvidence
	for _, directory := range []string{plan.Review.Preparation.Approval.Plan.Proposal.OriginalRunDirectory, plan.Request.RegistryDirectory} {
		if reference.Path == directory || strings.HasPrefix(reference.Path, directory+"/") {
			return errors.New("successor runtime evidence overlaps retained execution custody")
		}
	}
	for _, retained := range []planFileReference{base.Authorization.SafeBuildEvidence, base.Authorization.RuntimeEvidence, base.Authorization.CutoverEvidence, base.Authorization.SafeProvenance} {
		if reference.Path == retained.Path {
			return errors.New("successor runtime evidence reuses original canonical evidence")
		}
	}
	raw, hash, err := readBootstrapRootFile(ctx, reference.Path, 1024*1024)
	if err != nil || len(raw) == 0 || hash != reference.Sha256 {
		return errors.Join(errors.New("successor runtime evidence is absent or changed"), err)
	}
	return ctx.Err()
}

// A complete version identity names one approved artifact for this reader.
// Conflicting same-version code or metadata requires a separately reviewed
// artifact-pair reader; neither the CRv4 allowlist nor old authority is weakened.
func (self bootstrapSuccessorRuntimeApproval) extends(base bootstrapSuccessorCanonicalApproval, previous []bootstrapSuccessorRuntimeApproval) error {
	predecessor := rootObjectHash(base)
	profiles := []rootReceiptProfile{base.Authorization.CurrentRuntime}
	for _, revision := range previous {
		predecessor = rootObjectHash(revision)
		profiles = append(profiles, revision.Authorization.Runtime)
		if self.Authorization.RuntimeEvidence.Path == revision.Authorization.RuntimeEvidence.Path {
			return errors.New("successor runtime revision reuses predecessor evidence")
		}
	}
	if int(self.Authorization.Sequence) != len(previous)+1 || self.Authorization.PreviousHash != predecessor {
		return errors.New("successor runtime revision changed sequence or predecessor")
	}
	for _, profile := range profiles {
		if self.Authorization.Runtime.RuntimeVersion == profile.RuntimeVersion {
			return errors.New("successor runtime revision has duplicate or incompatible same-version artifact authority")
		}
	}
	return nil
}
