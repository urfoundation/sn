// A distinct signed acceptance retains the reviewed current-only proposal and
// original history statement. It never claims that historical provenance proved.
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"slices"
)

const bootstrapSuccessorSafeCurrentRevisionSchema = "urnetwork-mainnet-successor-safe-current-revision-v1"
const bootstrapSuccessorSafeCurrentRevisionDomain = "urnetwork-mainnet-successor-safe-current-revision-approval-v1"
const bootstrapSuccessorSafeCurrentRevisionPolicy = "accept-separately-signed-current-only-safe-policy-and-its-owned-finality-nonatomic-pending-and-signer-cutover-assumptions;retain-original-history-statement-without-claiming-history-proven;immutable-predecessors-execution-receipts-signed-bytes-nonces-counted-attempts-window-fees-and-liabilities;complete-proof-before-final-scoped-pending-readmission;no-public-submission-without-separately-installed-qualified-current-policy-capability-route"
const bootstrapSuccessorSafeCurrentPublicRevisionSchema = "urnetwork-mainnet-successor-safe-current-revision-v2"
const bootstrapSuccessorSafeCurrentPublicRevisionDomain = "urnetwork-mainnet-successor-safe-current-revision-approval-v2"
const bootstrapSuccessorSafeCurrentPublicRevisionPolicy = "explicitly-accept-separately-signed-current-only-safe-policy-for-bounded-public-submission-on-original-owned-route;accept-owned-rpc-finality-nonatomic-scoped-pending-assertions-and-exclusive-all-signer-relayer-cutover-with-no-other-live-signatures-or-transactions;no-complete-deployment-delegatecall-history-or-complete-pending-storage-proof-and-no-exclusion-of-between-read-changes;retain-original-history-statement-without-claiming-history-proven;immutable-predecessors-execution-receipts-exact-signed-bytes-nonces-counted-attempts-window-fees-and-liabilities;complete-finalized-storage-proof-before-final-scoped-pending-readmission-before-and-after-counted-reservation;one-exact-write-per-invocation-no-automatic-retry;require-exact-accepted-revision-opt-in-and-complete-current-runtime-authority"
const maximumBootstrapSuccessorSafeCurrentRevisionBytes = 32 * 1024

var errBootstrapSuccessorSafeCurrentCapabilityUnavailable = errors.New("successor current-policy submission is unavailable until a separately qualified production capability route is installed")

// The nested proposal keeps its distinct review signature and limitations.
// This additional signature accepts that policy for one immutable revision.
type bootstrapSuccessorSafeCurrentRevisionAuthorization struct {
	Schema       string                                      `json:"schema"`
	Sequence     uint16                                      `json:"sequence"`
	PreviousHash string                                      `json:"previous_hash"`
	Proposal     bootstrapSuccessorSafeCurrentPolicyApproval `json:"proposal"`
	Policy       string                                      `json:"accepted_policy"`
}

// No envelope can replace the independent key, base authority or signed send.
type bootstrapSuccessorSafeCurrentRevisionApproval struct {
	Authorization bootstrapSuccessorSafeCurrentRevisionAuthorization `json:"authorization"`
	Signature     string                                             `json:"signature_ed25519"`
}

// A review signature alone cannot stand in for this separate policy acceptance.
func (self bootstrapSuccessorSafeCurrentRevisionAuthorization) signingBytes() ([]byte, error) {
	domain := bootstrapSuccessorSafeCurrentRevisionDomain
	if self.permitsPublicSubmission() {
		domain = bootstrapSuccessorSafeCurrentPublicRevisionDomain
	} else if self.Schema != bootstrapSuccessorSafeCurrentRevisionSchema || self.Policy != bootstrapSuccessorSafeCurrentRevisionPolicy {
		return nil, errors.New("successor current-policy revision has an unknown acceptance version or policy")
	}
	if self.Sequence == 0 || !planSha256(self.PreviousHash) {
		return nil, errors.New("successor current-policy revision lacks explicit acceptance and predecessor")
	}
	if _, err := self.Proposal.Authorization.signingBytes(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(self)
	return append([]byte(domain+"\x00"), raw...), err
}

// The legacy acceptance remains read-only publicly. This discriminator grants
// nothing without validation of both signatures, exact custody and caller opt-in.
func (self bootstrapSuccessorSafeCurrentRevisionAuthorization) permitsPublicSubmission() bool {
	return self.Schema == bootstrapSuccessorSafeCurrentPublicRevisionSchema && self.Policy == bootstrapSuccessorSafeCurrentPublicRevisionPolicy
}

// A retained proposal refers to the exact completed runtime prefix it reviewed.
// Later additive runtimes cannot invalidate that historical policy signature.
func (self bootstrapSuccessorRuntimeHistory) prefix(hash string) (bootstrapSuccessorRuntimeHistory, error) {
	if hash == "" {
		return bootstrapSuccessorRuntimeHistory{}, nil
	}
	for i, approval := range self.approvals {
		if rootObjectHash(approval) == hash {
			return bootstrapSuccessorRuntimeHistory{approvals: slices.Clone(self.approvals[:i+1])}, nil
		}
	}
	return bootstrapSuccessorRuntimeHistory{}, errors.New("successor current policy lost its retained runtime prefix")
}

// Both independent signatures, exact original evidence and every predecessor
// remain required. This validation derives scope, not an installed send route.
func (self bootstrapSuccessorSafeCurrentRevisionApproval) validate(ctx context.Context, plan bootstrapSuccessorExecutionPlan, base bootstrapSuccessorCanonicalApproval, history bootstrapSuccessorRuntimeHistory) (safeCurrentStorageScope, error) {
	var scope safeCurrentStorageScope
	if ctx == nil {
		return scope, errors.New("successor current-policy revision requires a caller context")
	}
	message, err := self.Authorization.signingBytes()
	key, keyErr := rootReceiptHex(plan.Review.Preparation.Approval.Plan.ApprovalPublicKey, ed25519.PublicKeySize)
	signature, signatureErr := rootOfflineSignatureBytes(self.Signature)
	if err != nil || keyErr != nil || signatureErr != nil || !ed25519.Verify(key, message, signature) {
		return scope, errors.Join(errors.New("successor current-policy acceptance signature differs"), err, keyErr, signatureErr)
	}
	for _, revision := range history.approvals {
		if revision.Authorization.RuntimeEvidence.Path == self.Authorization.Proposal.Authorization.ReviewEvidence.Path {
			return scope, errors.New("successor current policy reuses retained runtime review evidence")
		}
	}
	prefix, err := history.prefix(self.Authorization.Proposal.Authorization.RuntimeRevisionHash)
	if err != nil {
		return scope, err
	}
	return self.Authorization.Proposal.validate(ctx, plan, base, prefix)
}

// One fixed chain prevents an alternative signed acceptance from replacing a
// retained revision. Runtime context may advance but never roll back its prefix.
func (self bootstrapSuccessorSafeCurrentRevisionApproval) extends(base bootstrapSuccessorCanonicalApproval, history bootstrapSuccessorRuntimeHistory, previous []bootstrapSuccessorSafeCurrentRevisionApproval) error {
	predecessor, priorRuntime := rootObjectHash(base), 0
	for _, revision := range previous {
		predecessor = rootObjectHash(revision)
		prefix, err := history.prefix(revision.Authorization.Proposal.Authorization.RuntimeRevisionHash)
		if err != nil {
			return err
		}
		priorRuntime = len(prefix.approvals)
		if self.Authorization.Proposal.Authorization.ReviewEvidence.Path == revision.Authorization.Proposal.Authorization.ReviewEvidence.Path {
			return errors.New("successor current-policy revision reuses predecessor review evidence")
		}
	}
	prefix, err := history.prefix(self.Authorization.Proposal.Authorization.RuntimeRevisionHash)
	if err != nil || len(prefix.approvals) < priorRuntime || int(self.Authorization.Sequence) != len(previous)+1 || self.Authorization.PreviousHash != predecessor {
		return errors.Join(errors.New("successor current-policy revision changed sequence, predecessor or runtime order"), err)
	}
	return nil
}
