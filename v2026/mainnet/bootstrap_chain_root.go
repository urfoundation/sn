// Root admission authenticates the full offline service configuration against
// independently selected role and approval keys. It supplies no live authority.
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
)

const bootstrapChainRootApprovalSchema = "urnetwork-mainnet-root-service-approval-v1"
const bootstrapChainRootPassiveApprovalSchema = "urnetwork-mainnet-root-passive-service-approval-v2"
const maximumBootstrapChainRootApprovalBytes = 16 * 1024

// Both approval keys come from the independent preparation input. The action
// approver may differ from the service-config approver; neither is a native key.
// A pointer requires an explicit netuid 0 rather than accepting a missing field.
type bootstrapChainRootValidator struct {
	Role                    string               `json:"role"`
	Netuid                  *uint16              `json:"netuid"`
	Implementation          string               `json:"implementation"`
	Hotkey                  string               `json:"hotkey_account_id"`
	Coldkey                 string               `json:"coldkey_account_id"`
	Seat                    *rootSeatExpectation `json:"seat"`
	Strategy                string               `json:"strategy"`
	ActionApprovalPublicKey string               `json:"action_approval_public_key_ed25519"`
	ApprovalPublicKey       string               `json:"approval_public_key_ed25519"`
	Approval                planFileReference    `json:"approval"`
}

// Each strategy selects its exact implementation and independent approval
// domain. The root role cannot count as either UR validator.
func (self bootstrapChainRootValidator) validate() error {
	if self.Strategy == rootPassiveStrategy {
		if self.Role != "bittensor-root-validator" || self.Netuid == nil || *self.Netuid != 0 || self.Implementation != "sn/mainnet/root-passive-service" ||
			!rootCanonicalHash(self.Hotkey) || !rootCanonicalHash(self.Coldkey) || self.Seat == nil || self.Seat.RegistrationBlock == 0 ||
			self.ActionApprovalPublicKey != "" || !rootCanonicalHash(self.ApprovalPublicKey) || !bootstrapRootAbsolutePath(self.Approval.Path) || !planSha256(self.Approval.Sha256) {
			return errors.New("passive root role requires exact observation identity and service approver without native-action authority")
		}
		return nil
	}
	if self.Role != "bittensor-root-validator" || self.Netuid == nil || *self.Netuid != 0 || self.Implementation != "sn/mainnet/root-service" ||
		!rootCanonicalHash(self.Hotkey) || !rootCanonicalHash(self.Coldkey) || self.Seat == nil || self.Seat.RegistrationBlock == 0 ||
		self.Strategy != "explicit_root_weights" || !rootCanonicalHash(self.ActionApprovalPublicKey) || !rootCanonicalHash(self.ApprovalPublicKey) ||
		!bootstrapRootAbsolutePath(self.Approval.Path) || !planSha256(self.Approval.Sha256) {
		return errors.New("bootstrap chain root role requires explicit netuid 0, implementation, generation, strategy, independent approval keys and exact approval input")
	}
	return nil
}

// The child plan seals exact config/service bytes and all action/custody scope.
// Signing its hash also binds observation allowance, paths, deployment and
// network; the separate service hash makes the complete config identity explicit.
type bootstrapChainRootApproval struct {
	Schema            string `json:"schema"`
	DeploymentId      string `json:"deployment_id"`
	RootPlanHash      string `json:"root_plan_hash"`
	ServiceConfigHash string `json:"service_config_hash"`
	Signature         string `json:"approval_signature_ed25519"`
}

// Signatures cover canonical Go JSON with an empty signature under a new domain.
// A native action approval cannot be reused as full service-config approval.
func (self bootstrapChainRootApproval) signingBytes() ([]byte, error) {
	self.Signature = ""
	raw, err := json.Marshal(self)
	return append([]byte(self.Schema+"\x00"), raw...), err
}

// The accepted parent plan retains the complete reviewable child configuration
// and independent config approval, not a boolean that can outlive those facts.
type bootstrapChainRootInspection struct {
	Plan     bootstrapRootPlan          `json:"root_plan"`
	Approval bootstrapChainRootApproval `json:"approval"`
}

// Rechecks the public signatures and their independent role bindings even when
// reading retained progress. Current eligibility and custody remain external.
func (self bootstrapChainRootInspection) validate(config bootstrapChainConfig, rootPlanHash string) error {
	role := config.RootValidator
	if role == nil {
		return errors.New("bootstrap chain requires an independently selected root role")
	}
	if err := errors.Join(role.validate(), self.Plan.validate()); err != nil {
		return err
	}
	root, approval := self.Plan, self.Approval
	scope := root.identityScope()
	passive := config.Schema == bootstrapChainConfigSchemaV4
	if passive != (root.PassiveService != nil) || passive != (role.Strategy == rootPassiveStrategy) {
		return errors.New("bootstrap chain schema cannot convert legacy root action authority to passive observation")
	}
	if root.ContentHash != rootPlanHash || root.ConfigPath != config.Root.Path || root.ConfigSha256 != config.Root.Sha256 ||
		root.DeploymentId != config.DeploymentId || root.Network != config.Network || root.RunDirectory != config.RunDirectory ||
		scope.Role != role.Role || scope.Netuid != *role.Netuid || scope.Hotkey != role.Hotkey || scope.Coldkey != role.Coldkey ||
		scope.Seat != *role.Seat || scope.Strategy != role.Strategy || !passive && root.Service.CustodyTrust.ApprovalPublicKey != role.ActionApprovalPublicKey {
		return errors.New("bootstrap chain root service differs from its independent role, action approver or child scope")
	}
	for _, validator := range config.Validators {
		if validator.Hotkey == role.Hotkey {
			return errors.New("bootstrap chain root role cannot count as a UR validator")
		}
	}
	approvalSchema := bootstrapChainRootApprovalSchema
	if passive {
		approvalSchema = bootstrapChainRootPassiveApprovalSchema
	}
	if approval.Schema != approvalSchema || approval.DeploymentId != config.DeploymentId || approval.RootPlanHash != root.ContentHash ||
		approval.ServiceConfigHash != root.serviceHash() {
		return errors.New("bootstrap chain root config approval differs from the complete service configuration or deployment")
	}
	key, _ := hex.DecodeString(role.ApprovalPublicKey[2:])
	signature, err := rootOfflineSignatureBytes(approval.Signature)
	message, messageErr := approval.signingBytes()
	if err != nil || messageErr != nil || !ed25519.Verify(key, message, signature) {
		return errors.New("bootstrap chain root config approval signature differs from the independent signer or service approval domain")
	}
	return nil
}

// The bounded approval source is read on every plan/apply/resume invocation.
// Retained journal data never substitutes for missing or changed source bytes.
func loadBootstrapChainRootInspection(ctx context.Context, config bootstrapChainConfig, root bootstrapRootPlan) (*bootstrapChainRootInspection, error) {
	if config.RootValidator == nil {
		return nil, errors.New("bootstrap chain requires an independently selected root role")
	}
	if err := config.RootValidator.validate(); err != nil {
		return nil, err
	}
	raw, err := readBootstrapChainInput(ctx, config.RootValidator.Approval, maximumBootstrapChainRootApprovalBytes)
	if err != nil {
		return nil, err
	}
	inspection := &bootstrapChainRootInspection{Plan: copyBootstrapRootPlan(root)}
	if err := decodePlanJson(raw, &inspection.Approval); err != nil {
		return nil, err
	}
	if err := inspection.validate(config, root.ContentHash); err != nil {
		return nil, err
	}
	return inspection, nil
}
