// Current launch authority is separate from installation and producer approval.
// Its explicit assumptions cannot be inferred from a chain read or heartbeat.
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const validatorActivationCurrentSchema = "urnetwork-mainnet-validator-current-admission-v1"
const validatorActivationCurrentDomain = "urnetwork-mainnet-validator-current-admission-approval-v1"
const validatorActivationCurrentPolicy = "authorize-one-initial-start-per-original-ur-unit-after-fresh-original-installation-native-eligibility-conservative-majority-stake-capacity-operator-client-key-and-complete-empty-tail-ledger-admission;accept-first-weight-activity-as-not-yet-applied-and-require-strict-majority-stake-capacity;accept-owned-rpc-finality-and-nonatomic-current-reads-with-no-exclusion-of-between-read-changes;accept-separately-approved-current-only-safe-policy-without-complete-governance-history;accept-no-prestart-worker-liveness-only-for-never-started-unit-and-require-attributable-responsive-steering-after-start;retain-applied-weights-and-emissions-as-unproven;no-root-service-authority-no-stop-no-restart-no-new-signature-or-transaction-by-this-controller"
const validatorActivationCurrentCustodyPolicy = "independently-attest-exclusive-custody-of-both-original-native-hotkeys-and-protocol-state-on-this-exact-host-release-and-unit-pair-for-the-lifetime-of-the-original-activation-and-acknowledged-invocations;no-custody-transfer-until-explicit-fenced-shutdown-and-new-independent-authority;all-other-processes-hosts-schedulers-signers-and-pending-signatures-for-these-hotkeys-are-excluded;privileged-host-administration-preserves-original-journals-unit-claims-and-no-rollback-no-copy-no-out-of-band-start;this-is-custodian-attestation-not-a-chain-or-distributed-lock-proof"

// Inputs reconstruct original executed custody, never a previously emitted
// readback report. Runtime and current-policy histories must already be retained.
type validatorActivationInstallationInputs struct {
	Request            planFileReference `json:"successor_request"`
	SafeRequest        planFileReference `json:"safe_request"`
	ExecutionRequest   planFileReference `json:"execution_request"`
	ExecutionApproval  planFileReference `json:"execution_approval"`
	CanonicalApproval  planFileReference `json:"canonical_approval"`
	ExecutionPlanHash  string            `json:"execution_plan_hash"`
	InstallationHash   string            `json:"installation_identity_hash"`
	AcceptedSafePolicy string            `json:"accepted_safe_current_policy_hash"`
}

// The independent key signs the full original process scope, exact installation
// custody, two signer identities, and finite acceptance of stated assumptions.
type validatorActivationCurrentAuthorization struct {
	Schema              string                                `json:"schema"`
	ActivationHash      string                                `json:"activation_approval_hash"`
	ActivationPublicKey string                                `json:"activation_public_key"`
	PreparationHash     string                                `json:"bootstrap_plan_hash"`
	Installation        validatorActivationInstallationInputs `json:"installation"`
	NativeHotkeys       [2]string                             `json:"exclusive_native_hotkeys"`
	CustodyEvidence     planFileReference                     `json:"custodian_review_evidence"`
	ValidFrom           time.Time                             `json:"valid_from"`
	ExpiresAt           time.Time                             `json:"expires_at"`
	Policy              string                                `json:"accepted_current_policy"`
	CustodyPolicy       string                                `json:"accepted_custody_attestation"`
}

// An externally supplied verification key remains outside both envelopes.
// Merely matching a producer key or a file digest never supplies this signature.
type validatorActivationCurrentApproval struct {
	Authorization validatorActivationCurrentAuthorization `json:"authorization"`
	Signature     string                                  `json:"signature_ed25519"`
}

// The message is domain separated from process approval and Safe acceptance.
func (self validatorActivationCurrentAuthorization) signingBytes() ([]byte, error) {
	if self.Schema != validatorActivationCurrentSchema || !planSha256(self.ActivationHash) || !rootCanonicalHash(self.ActivationPublicKey) || !planSha256(self.PreparationHash) ||
		!planSha256(self.Installation.ExecutionPlanHash) || !planSha256(self.Installation.InstallationHash) || !planSha256(self.Installation.AcceptedSafePolicy) ||
		!rootCanonicalHash(self.NativeHotkeys[0]) || !rootCanonicalHash(self.NativeHotkeys[1]) || self.NativeHotkeys[0] == self.NativeHotkeys[1] ||
		self.ValidFrom.IsZero() || !self.ExpiresAt.After(self.ValidFrom) || self.ExpiresAt.Sub(self.ValidFrom) > 24*time.Hour ||
		self.Policy != validatorActivationCurrentPolicy || self.CustodyPolicy != validatorActivationCurrentCustodyPolicy {
		return nil, errors.New("validator current admission lacks exact installation, independent custody and finite policy acceptance")
	}
	paths := map[string]bool{}
	for _, file := range self.references() {
		if !repairValidatorPath(file.Path) || !planSha256(file.Sha256) || paths[file.Path] {
			return nil, errors.New("validator current admission input pin is absent or overlaps")
		}
		paths[file.Path] = true
	}
	raw, err := json.Marshal(self)
	return append([]byte(validatorActivationCurrentDomain+"\x00"), raw...), err
}

// All public inputs are re-read around admission; retained approval does not
// excuse replacement or loss of independently reviewed custody evidence.
func (self validatorActivationCurrentAuthorization) references() []planFileReference {
	p := self.Installation
	return []planFileReference{p.Request, p.SafeRequest, p.ExecutionRequest, p.ExecutionApproval, p.CanonicalApproval, self.CustodyEvidence}
}

// Validation binds the independent signature to both already signed producer
// identities. External custody remains an explicitly signed trust assumption.
func (self validatorActivationCurrentApproval) validate(activation validatorActivationApproval, activationKey, key string, preparation bootstrapChainPreparation) error {
	if err := self.validateApproval(activation, activationKey, key); err != nil {
		return err
	}
	p := self.Authorization
	if p.PreparationHash != preparation.Plan.ContentHash || len(preparation.Plan.Config.Validators) != 2 || len(preparation.Plan.ValidatorInspections) != 2 {
		return errors.New("validator current admission differs from original preparation")
	}
	for i, role := range preparation.Plan.Config.Validators {
		if p.NativeHotkeys[i] != role.Hotkey || key == p.NativeHotkeys[i] || key == preparation.Plan.ValidatorInspections[i].ApprovalSigner {
			return errors.New("validator current custody signer or original hotkey differs")
		}
	}
	for _, file := range p.references() {
		for _, reserved := range []string{activation.Plan.StatePath, activation.Plan.StatePath + ".lock"} {
			if file.Path == reserved || strings.HasPrefix(file.Path, reserved+"/") || strings.HasPrefix(reserved, file.Path+"/") {
				return errors.New("validator current admission input overlaps activation custody")
			}
		}
	}
	return nil
}

// Journal reopening verifies the signature and immutable outer scope before
// any source reads. Fresh admission additionally rechecks original hotkeys.
func (self validatorActivationCurrentApproval) validateApproval(activation validatorActivationApproval, activationKey, key string) error {
	p := self.Authorization
	message, err := p.signingBytes()
	if err != nil || !rootCanonicalHash(key) || key == activationKey || p.ActivationPublicKey != activationKey || p.ActivationHash != rootObjectHash(activation) ||
		p.PreparationHash != activation.Plan.PlanHash || p.ValidFrom.Before(activation.Plan.ValidFrom) || p.ExpiresAt.After(activation.Plan.ExpiresAt) {
		return errors.Join(errors.New("validator current admission differs from independent original activation"), err)
	}
	public, _ := hex.DecodeString(key[2:])
	signature, err := rootOfflineSignatureBytes(self.Signature)
	if err != nil || !ed25519.Verify(public, message, signature) {
		return errors.New("validator current admission independent signature differs")
	}
	return nil
}

// Expiry and rollback apply to every current read and immediately before start.
func (self validatorActivationCurrentApproval) window(ctx context.Context, highWater, now time.Time) error {
	p := self.Authorization
	if ctx == nil || now.IsZero() || now.Before(highWater) || now.Before(p.ValidFrom) || !now.Before(p.ExpiresAt) {
		return errors.New("validator current admission window or monotonic clock differs")
	}
	return ctx.Err()
}
