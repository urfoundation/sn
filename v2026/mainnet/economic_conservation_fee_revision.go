// Original fee authority can advance through independently signed revisions.
// Every step retains the exact predecessor checkpoint, policy and review;
// neither a restart nor an archive compaction installs a replacement key.
package main

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
)

const economicConservationFeeRevisionSchema = "urnetwork-economic-native-fee-revision-v1"

var errEconomicNativeFeeUnadmittedPolicy = errors.New("economic fee request differs from original-key retained policy authority")

// Signatures use the NativeFee v1 canonical bare lowercase 128-hex grammar.
// The revision signs authority and exact lineage, never an observed fee amount.
type economicConservationFeeRevision struct {
	Schema       string                  `json:"schema"`
	PolicyHash   string                  `json:"original_conservation_policy_hash"`
	Original     monitorHistoryReference `json:"original_checkpoint"`
	Ordinal      uint64                  `json:"ordinal"`
	Previous     string                  `json:"previous_revision_hash"`
	ReviewSha256 string                  `json:"revision_review_sha256"`
	From         economicNativeFeePolicy `json:"from"`
	To           economicNativeFeePolicy `json:"to"`
	Signature    string                  `json:"signature_ed25519,omitempty"`
}

// Only the cumulative head remains hot after the signed original revision
// itself moves into an exact snapshot. Reopen reconstructs it from that source.
type economicConservationFeeRevisionHead struct {
	Hash    string                  `json:"revision_hash"`
	Ordinal uint64                  `json:"ordinal"`
	Policy  economicNativeFeePolicy `json:"fee_policy"`
}

func economicConservationValidFeePolicy(value economicNativeFeePolicy) bool {
	return rootCanonicalHash(value.ApprovalPublicKey) && rootCanonicalHash(value.Genesis) && value.EvmChainId != 0 && planSha256(value.EngineSha256) && planSha256(value.CheckpointSha256) && planSha256(value.ReviewSha256) && planSha256(value.ProfileSha256)
}

func (self economicConservationFeeRevision) signingBytes() ([]byte, error) {
	if self.Schema != economicConservationFeeRevisionSchema || !planSha256(self.PolicyHash) || self.Ordinal == 0 || !planSha256(self.Previous) || !planSha256(self.ReviewSha256) || self.From == self.To || !economicConservationValidFeePolicy(self.From) || !economicConservationValidFeePolicy(self.To) || self.From.ApprovalPublicKey != self.To.ApprovalPublicKey || self.From.Genesis != self.To.Genesis || self.From.EvmChainId != self.To.EvmChainId {
		return nil, errors.New("economic fee revision changed the original key/network or lacks exact policy lineage")
	}
	if err := self.Original.validateLimit(economicConservationStorageMaximum); err != nil {
		return nil, err
	}
	self.Signature = ""
	raw, err := json.Marshal(self)
	if err != nil {
		return nil, err
	}
	return append([]byte(economicConservationFeeRevisionSchema+"\x00"), raw...), nil
}

func (self economicConservationFeeRevision) verify(policy economicConservationPolicy) error {
	if policy.FeeAuthority == nil {
		return errors.New("legacy conservation policy cannot enroll a fee revision approver")
	}
	if err := policy.validateReference(self.Original); err != nil {
		return err
	}
	message, err := self.signingBytes()
	key, keyErr := rootReceiptHex(policy.FeeAuthority.ApprovalPublicKey, ed25519.PublicKeySize)
	signature, signatureErr := rootOfflineSignatureBytes(self.Signature)
	if err != nil || keyErr != nil || signatureErr != nil || self.PolicyHash != policy.identityHash() || self.From.ApprovalPublicKey != policy.FeeAuthority.ApprovalPublicKey || self.From.Genesis != policy.FeeAuthority.Genesis || self.From.EvmChainId != policy.FeeAuthority.EvmChainId || self.ReviewSha256 == policy.FeeAuthority.ReviewSha256 || !ed25519.Verify(key, message, signature) {
		return errors.New("economic fee revision lacks its original independent signature or fresh review")
	}
	return nil
}

func (self *economicConservationState) nativeFeeAuthority(policy economicConservationPolicy) (*economicNativeFeePolicy, error) {
	if policy.FeeAuthority == nil {
		if self.FeeRevision != nil || self.Archive != nil && self.Archive.FeeRevisionHead != nil {
			return nil, errors.New("economic state enrolled fee authority after original admission")
		}
		return nil, nil
	}
	authority, previous, ordinal := *policy.FeeAuthority, rootObjectHash(policy.FeeAuthority), uint64(0)
	if self.Archive != nil && self.Archive.FeeRevisionHead != nil {
		head := self.Archive.FeeRevisionHead
		if head.Ordinal == 0 || !planSha256(head.Hash) || !economicConservationValidFeePolicy(head.Policy) || head.Policy.ApprovalPublicKey != authority.ApprovalPublicKey || head.Policy.Genesis != authority.Genesis || head.Policy.EvmChainId != authority.EvmChainId {
			return nil, errors.New("economic archived fee revision changed original authority")
		}
		authority, previous, ordinal = head.Policy, head.Hash, head.Ordinal
	}
	if self.FeeRevision != nil {
		revision := self.FeeRevision
		if self.Archive == nil || len(self.Archive.Segments) == 0 || ordinal == ^uint64(0) || revision.Ordinal != ordinal+1 || revision.Previous != previous || revision.From != authority {
			return nil, errors.New("economic fee revision replaced its exact original predecessor")
		}
		original := self.Archive.Segments[len(self.Archive.Segments)-1]
		if revision.Original.Sha256 != original.Sha256 || revision.Original.Bytes != original.Bytes {
			return nil, errors.New("economic fee revision lost its exact pre-revision checkpoint")
		}
		if err := revision.verify(policy); err != nil {
			return nil, err
		}
		authority = revision.To
	}
	return &authority, nil
}

// Historical proof admission is distinct from permission for a new verifier
// dispatch. Old reports retain the policy actually admitted for their inputs.
func (self *economicConservationState) nativeFeePolicyKnown(policy economicConservationPolicy, candidate economicNativeFeePolicy) bool {
	if policy.FeeAuthority != nil && candidate == *policy.FeeAuthority {
		return true
	}
	if self.FeeRevision != nil && candidate == self.FeeRevision.To || self.Archive != nil && self.Archive.FeeRevisionHead != nil && candidate == self.Archive.FeeRevisionHead.Policy {
		return true
	}
	if self.archiveView != nil {
		value, known := self.archiveView.feePolicies[rootObjectHash(candidate)]
		return known && value == candidate
	}
	return false
}

// Revisions extend retained policy authority. Historical backlog can use its
// original admitted engine/profile, but only with a newly verified exact
// per-context signature. A decoded archive head alone never admits dispatch.
func (self *economicConservationState) admittedNativeFeePolicies(policy economicConservationPolicy) (map[string]economicNativeFeePolicy, error) {
	current, err := self.nativeFeeAuthority(policy)
	if err != nil {
		return nil, err
	}
	if self.Archive != nil && (self.Archive.FeeRevisionHead != nil || len(self.Archive.FeeRetirements) != 0) && self.archiveView == nil {
		return nil, errors.New("economic fee dispatch requires admitted original archive custody")
	}
	if err := self.archiveView.check(); err != nil {
		return nil, err
	}
	values := map[string]economicNativeFeePolicy{}
	if policy.FeeAuthority != nil {
		values[rootObjectHash(policy.FeeAuthority)] = *policy.FeeAuthority
	}
	if current != nil {
		values[rootObjectHash(current)] = *current
	}
	if self.archiveView != nil {
		for hash, value := range self.archiveView.feePolicies {
			values[hash] = value
		}
	}
	return values, nil
}

func (self *economicConservationArchiveView) retainFeeRevision(original *economicConservationState) error {
	if original.FeeRevision == nil {
		return nil
	}
	revision := original.FeeRevision
	if self.feeReviews[revision.ReviewSha256] {
		return errors.New("economic fee revision reused an original or archived review")
	}
	if err := self.charge(revision); err != nil {
		return err
	}
	self.feeReviews[revision.ReviewSha256] = true
	self.feePolicies[rootObjectHash(revision.To)] = revision.To
	return nil
}
