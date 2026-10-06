// Best-effort submission is a fresh, explicit approval domain. It preserves
// original owner-signed bytes and never supplies the strict window enforcer.
package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
)

const ownerTrimBestEffortApprovalSchema = "urnetwork-mainnet-owner-trim-best-effort-submission-v1"
const ownerTrimRequirePruningImmunity = "require-public-pruning-immunity-through-original-expiry"
const ownerTrimAcceptPruningRisk = "accept-public-pruning-and-netuid-reuse-risk"
const ownerTrimRequireClosedRegistration = "require-observed-closed-registration"
const ownerTrimAcceptRegistrationRisk = "accept-competing-registration-and-reentry-risk"

const ownerTrimPublicPruningResidual = "Public subnet registration may prune the original subnet and reuse its netuid before inclusion; this call has no atomic subnet-generation predicate and may fail or affect a replacement subnet. Current owner and generation checks do not fence that interval."
const ownerTrimRegistrationResidual = "Competing registration, owner takeover, hotkey swaps and re-entry may change membership after the final read, including earlier actions in the inclusion block. The owner cannot close all registration routes on this runtime; the native call cannot bind the reviewed protected generations atomically."

// Every risk is mandatory; removing one requires a different reviewed protocol.
func ownerTrimBestEffortResiduals() []string {
	return []string{
		"Governance, privileged runtime changes, subnet removal and root generation changes cannot be fenced through inclusion by this owner call.",
		"The capacity-only call cannot atomically bind protected generations; emissions, immunity, epoch transitions and swap-and-pop selection can change after the final read and remove a different generation.",
		"The fee reserve is local accounting, not an on-chain maximum; inclusion and failed dispatch can charge more than the reserve.",
		"Local custody does not fence other machines, rollback, pending owner transactions, multisig or other holders of the coldkey; the original nonce and signature must remain under external exclusive custody.",
		"RPC finality and storage are trusted observations, not independently verified consensus or state proofs; exact source-to-Wasm provenance and collateral, stake, claims and history require separate review.",
		"A successful dispatch is only a partial trim; remaining miners and protected-generation outcomes require canonical receipt and census reconciliation before any separate activation approval.",
	}
}

// Approval is independently signed after the original public signature exists.
// It cannot extend mortality, change bytes or replenish consumed attempts.
type ownerTrimBestEffortApproval struct {
	Schema              string             `json:"schema"`
	ConfigHash          string             `json:"original_action_config_hash"`
	ExtrinsicHash       string             `json:"original_signed_extrinsic_hash"`
	Runtime             rootReceiptProfile `json:"exact_runtime"`
	BaselineCensusHash  string             `json:"reviewed_baseline_census_hash"`
	ProtectedScopeHash  string             `json:"reviewed_protected_generation_scope_hash"`
	InitialBroadcasts   uint8              `json:"already_consumed_broadcasts"`
	ValidFromBlock      uint64             `json:"valid_from_finalized_block"`
	ValidThroughBlock   uint64             `json:"valid_through_finalized_block"`
	ResidualRisks       []string           `json:"explicitly_accepted_residual_risks"`
	PublicPruningPolicy string             `json:"public_pruning_policy,omitempty"`
	RegistrationPolicy  string             `json:"registration_policy,omitempty"`
	Signature           string             `json:"approval_signature_ed25519"`
}

// Empty fields preserve already signed conservative policies byte for byte.
// Each weaker choice requires its own exact residual and independent signature.
func (self ownerTrimBestEffortApproval) residuals() []string {
	risks := ownerTrimBestEffortResiduals()
	if self.PublicPruningPolicy == ownerTrimAcceptPruningRisk {
		risks = append(risks, ownerTrimPublicPruningResidual)
	}
	if self.RegistrationPolicy == ownerTrimAcceptRegistrationRisk {
		risks = append(risks, ownerTrimRegistrationResidual)
	}
	return risks
}

// The record holds proof and consumed post reservations, never a trust root.
// A fresh sender still requires its policy and key from independent inputs.
type ownerTrimBestEffortSubmissionRecord struct {
	Approval            ownerTrimBestEffortApproval `json:"approval"`
	ApprovalKey         string                      `json:"approval_public_key_ed25519"`
	SubmittedBroadcasts uint8                       `json:"consumed_post_reservations"`
}

// Signatures cannot cross into the strict action or any other approval domain.
func (self ownerTrimBestEffortApproval) signingBytes() []byte {
	self.Signature = ""
	raw, _ := json.Marshal(self)
	return append([]byte(ownerTrimBestEffortApprovalSchema+"\x00"), raw...)
}

// Exact config identity also binds network, owner, route, fee reserve and budget.
func (self ownerTrimBestEffortApproval) validate(config ownerTrimExecutionConfig, key, extrinsicHash string) error {
	action := config.Action
	if self.Schema != ownerTrimBestEffortApprovalSchema || config.Schema != ownerTrimBestEffortExecutionSchema || action.Schema != ownerTrimBestEffortActionSchema ||
		self.ConfigHash != rootObjectHash(config) || !rootCanonicalHash(extrinsicHash) || self.ExtrinsicHash != extrinsicHash || self.Runtime != action.Runtime ||
		!planSha256(self.BaselineCensusHash) || !planSha256(self.ProtectedScopeHash) || self.InitialBroadcasts >= action.MaxBroadcasts ||
		self.ValidFromBlock < action.BirthBlock || self.ValidFromBlock > self.ValidThroughBlock || self.ValidThroughBlock >= action.BirthBlock+action.Period ||
		self.PublicPruningPolicy != "" && self.PublicPruningPolicy != ownerTrimRequirePruningImmunity && self.PublicPruningPolicy != ownerTrimAcceptPruningRisk ||
		self.RegistrationPolicy != "" && self.RegistrationPolicy != ownerTrimRequireClosedRegistration && self.RegistrationPolicy != ownerTrimAcceptRegistrationRisk ||
		!slices.Equal(self.ResidualRisks, self.residuals()) || !rootCanonicalHash(key) {
		return errors.New("owner trim best-effort approval differs from original signed action, bounds or mandatory residual risks")
	}
	public, _ := hex.DecodeString(key[2:])
	signature, err := rootOfflineSignatureBytes(self.Signature)
	if err != nil || !ed25519.Verify(public, self.signingBytes(), signature) {
		return errors.New("owner trim independent best-effort submission approval is invalid")
	}
	return nil
}

// Bind protected roles and every root registration, including their original
// owner, uid and generation. The full census hash binds the other seats too.
func ownerTrimBestEffortProtectedScope(review ownerTrimPlan) string {
	census := review.Census.Observation
	protected := []subnetRegistration{}
	for _, seat := range census.Seats {
		if len(seat.ProtectionReasons) != 0 || seat.OwnerRecognized || seat.ValidatorPermit {
			protected = append(protected, seat.subnetRegistration)
		}
	}
	return rootObjectHash(struct {
		Owner        string               `json:"owner"`
		Registration uint64               `json:"subnet_registration_block"`
		Generation   uint64               `json:"subnet_generation"`
		Protected    []subnetRegistration `json:"protected"`
		Root         []subnetRegistration `json:"root"`
	}{Owner: census.SubnetOwnerColdkey, Registration: census.SubnetRegistrationBlock, Generation: census.SubnetGeneration,
		Protected: protected, Root: census.RootRegistrations})
}

// Planning is local and unsigned. It requires already retained public bytes;
// exporting a template never installs fresh-send authority.
func (self *ownerTrimStore) bestEffortApprovalTemplate() (ownerTrimBestEffortApproval, error) {
	record, err := self.load()
	if err != nil {
		return ownerTrimBestEffortApproval{}, err
	}
	if record.Config.Schema != ownerTrimBestEffortExecutionSchema || record.Signature == "" || record.Reconciliation != nil ||
		record.Submission != nil || record.Broadcasts >= record.Config.Action.MaxBroadcasts || record.LastFinalized >= record.Config.Action.BirthBlock+record.Config.Action.Period {
		return ownerTrimBestEffortApproval{}, errors.New("owner trim best-effort planning requires original signed pending custody without a retained submission policy")
	}
	action := record.Config.Action
	return ownerTrimBestEffortApproval{Schema: ownerTrimBestEffortApprovalSchema, ConfigHash: rootObjectHash(record.Config),
		ExtrinsicHash: record.ExtrinsicHash, Runtime: action.Runtime, BaselineCensusHash: self.review.Census.ContentHash,
		ProtectedScopeHash: ownerTrimBestEffortProtectedScope(self.review), InitialBroadcasts: record.Broadcasts,
		ValidFromBlock: max(action.BirthBlock, record.LastFinalized), ValidThroughBlock: action.BirthBlock + action.Period - 1,
		ResidualRisks: ownerTrimBestEffortResiduals()}, nil
}

// The immutable review is checked again on every admission and direct send.
func (self *ownerTrimStore) validateBestEffortApproval(approval ownerTrimBestEffortApproval, key string, record ownerTrimRecord) error {
	if err := approval.validate(self.config, key, record.ExtrinsicHash); err != nil {
		return err
	}
	if approval.BaselineCensusHash != self.review.Census.ContentHash || approval.ProtectedScopeHash != ownerTrimBestEffortProtectedScope(self.review) {
		return errors.New("owner trim submission approval names another original census or protected-generation scope")
	}
	return nil
}
