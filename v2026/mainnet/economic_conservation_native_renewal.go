// Native approval adoption changes only an owned operational view. The original
// economic policy, key, checkpoint and acknowledged execution lineage stay exact.
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"slices"
)

const economicConservationNativeRenewalSchema = "urnetwork-economic-native-approval-adoption-v1"

// This signature admits an append-only set of already independently signed
// native runtime/resource reviews. It never signs amounts or individual blocks.
type economicConservationNativeRenewal struct {
	Schema       string                  `json:"schema"`
	PolicyHash   string                  `json:"original_conservation_policy_hash"`
	Original     monitorHistoryReference `json:"original_checkpoint"`
	Ordinal      uint64                  `json:"ordinal"`
	Previous     string                  `json:"previous_adoption_hash"`
	ReviewSha256 string                  `json:"adoption_review_sha256"`
	From         []planFileReference     `json:"previous_native_reviews"`
	To           []planFileReference     `json:"next_native_reviews"`
	Signature    string                  `json:"signature_ed25519,omitempty"`
}

// Exact signed predecessors move into original snapshots. The active head is
// reconstructed on archive admission, never trusted as a new signing authority.
type economicConservationNativeApprovalHead struct {
	Hash     string              `json:"adoption_hash"`
	Ordinal  uint64              `json:"ordinal"`
	Renewals []planFileReference `json:"native_reviews"`
}

func economicConservationNativeReferencesInclude(previous, next []planFileReference) bool {
	if len(next) < len(previous) || len(next) > maximumNativeProducerRenewals {
		return false
	}
	paths, hashes := map[string]bool{}, map[string]bool{}
	for index, reference := range next {
		if !bootstrapRootAbsolutePath(reference.Path) || !planSha256(reference.Sha256) || paths[reference.Path] || hashes[reference.Sha256] || index < len(previous) && reference != previous[index] {
			return false
		}
		paths[reference.Path], hashes[reference.Sha256] = true, true
	}
	return true
}

func (self economicConservationNativeRenewal) signingBytes() ([]byte, error) {
	if self.Schema != economicConservationNativeRenewalSchema || !planSha256(self.PolicyHash) || !planSha256(self.Previous) || !planSha256(self.ReviewSha256) || self.Ordinal == 0 || len(self.To) <= len(self.From) || !economicConservationNativeReferencesInclude(self.From, self.To) {
		return nil, errors.New("economic native adoption requires exact append-only bounded approval lineage")
	}
	if err := self.Original.validateLimit(economicConservationStorageMaximum); err != nil {
		return nil, err
	}
	self.Signature = ""
	raw, err := json.Marshal(self)
	if err != nil {
		return nil, err
	}
	return append([]byte(economicConservationNativeRenewalSchema+"\x00"), raw...), nil
}

// Effective resource/native views can only return to their private original
// basis. An externally supplied different policy has no such private authority.
func economicConservationNativeBasis(policy economicConservationPolicy) economicConservationPolicy {
	if policy.resourceBasis != nil {
		policy.Native = policy.resourceBasis.Native
	}
	return policy
}

func (self economicConservationNativeRenewal) verify(policy economicConservationPolicy) error {
	policy = economicConservationNativeBasis(policy)
	if err := policy.validateReference(self.Original); err != nil {
		return err
	}
	execution := policy.Native.Observation.Execution
	if execution == nil || execution.Producer == nil {
		return errors.New("legacy conservation policy cannot enroll a native producer approver")
	}
	message, err := self.signingBytes()
	key, keyErr := rootReceiptHex(execution.ApprovalPublicKey, ed25519.PublicKeySize)
	signature, signatureErr := rootOfflineSignatureBytes(self.Signature)
	if err != nil || keyErr != nil || signatureErr != nil || self.PolicyHash != policy.identityHash() || self.ReviewSha256 == execution.ReviewSha256 || !economicConservationNativeReferencesInclude(execution.Producer.Renewals, self.From) || !ed25519.Verify(key, message, signature) {
		return errors.New("economic native adoption lacks its original independent signature or policy basis")
	}
	return nil
}

func (self *economicConservationState) nativeApprovalHead(policy economicConservationPolicy) (economicConservationNativeApprovalHead, error) {
	policy = economicConservationNativeBasis(policy)
	var head economicConservationNativeApprovalHead
	execution := policy.Native.Observation.Execution
	if execution == nil || execution.Producer == nil {
		if self.NativeRenewal != nil || self.Archive != nil && self.Archive.NativeApprovalHead != nil {
			return head, errors.New("economic state enrolled native producer approval after original admission")
		}
		return head, nil
	}
	head = economicConservationNativeApprovalHead{Hash: rootObjectHash(execution.Producer), Renewals: slices.Clone(execution.Producer.Renewals)}
	if self.Archive != nil && self.Archive.NativeApprovalHead != nil {
		archived := self.Archive.NativeApprovalHead
		if archived.Ordinal == 0 || !planSha256(archived.Hash) || !economicConservationNativeReferencesInclude(head.Renewals, archived.Renewals) || len(archived.Renewals) <= len(head.Renewals) {
			return head, errors.New("economic archived native approval replaced original review lineage")
		}
		head = *archived
		head.Renewals = slices.Clone(head.Renewals)
	}
	if self.NativeRenewal != nil {
		adoption := self.NativeRenewal
		if self.Archive == nil || len(self.Archive.Segments) == 0 || head.Ordinal == ^uint64(0) || adoption.Ordinal != head.Ordinal+1 || adoption.Previous != head.Hash || !slices.Equal(adoption.From, head.Renewals) {
			return head, errors.New("economic native adoption lost its exact predecessor")
		}
		original := self.Archive.Segments[len(self.Archive.Segments)-1]
		if adoption.Original.Sha256 != original.Sha256 || adoption.Original.Bytes != original.Bytes {
			return head, errors.New("economic native adoption lost its exact pre-adoption checkpoint")
		}
		if err := adoption.verify(policy); err != nil {
			return head, err
		}
		head = economicConservationNativeApprovalHead{Hash: rootObjectHash(adoption), Ordinal: adoption.Ordinal, Renewals: slices.Clone(adoption.To)}
	}
	return head, nil
}

// Clones retain pending jobs and original engines in producer state. The next
// native observer independently verifies every configured signed review and
// its execution-time ancestor before actually adopting an engine or profile.
func (self *economicConservationState) nativeOperatingPolicy(policy economicConservationPolicy) (monitorEconomicNativePolicy, error) {
	head, err := self.nativeApprovalHead(policy)
	if err != nil {
		return policy.Native, err
	}
	result := economicConservationNativeBasis(policy).Native
	result.archiveReferenceBytes = policy.storageMaximum()
	if result.Observation.Execution == nil || result.Observation.Execution.Producer == nil {
		return result, nil
	}
	execution := *result.Observation.Execution
	producer := *execution.Producer
	producer.Renewals = slices.Clone(head.Renewals)
	execution.Producer = &producer
	result.Observation.Execution = &execution
	return result, nil
}

// Pure lineage presence is separate from the physical fence at each caller's
// dependent operation. No value from an absent original index is admissible.
func (self *economicConservationState) validateNativeApprovalHistory() error {
	if (self.NativeRenewal != nil || self.Archive != nil && self.Archive.NativeApprovalHead != nil) && self.archiveView == nil {
		return errors.New("economic native dispatch requires admitted original approval history")
	}
	return nil
}

// Standalone users require both lineage admission and fresh original custody.
func (self *economicConservationState) requireNativeApprovalHistory() error {
	if err := self.validateNativeApprovalHistory(); err != nil {
		return err
	}
	return self.archiveView.check()
}

func (self *economicConservationArchiveView) retainNativeApproval(original *economicConservationState) error {
	if original.NativeRenewal == nil {
		return nil
	}
	if self == nil || self.nativeReviews == nil {
		return errors.New("economic native approval requires an admitted original review index")
	}
	if err := self.checkAdmission(); err != nil {
		return err
	}
	adoption := original.NativeRenewal
	if self.nativeReviews[adoption.ReviewSha256] {
		return errors.New("economic native adoption reused an original or archived review")
	}
	if err := self.charge(adoption); err != nil {
		return err
	}
	self.nativeReviews[adoption.ReviewSha256] = true
	return nil
}

// The offline command validates actual independently signed review files. No
// file path, syntax-valid digest or archive summary alone supplies authority.
func applyEconomicConservationNativeRenewal(ctx context.Context, policy economicConservationPolicy, next *economicConservationState, adoption *economicConservationNativeRenewal) error {
	if adoption == nil {
		return nil
	}
	if next.archiveView == nil || next.archiveView.nativeReviews == nil || next.archiveView.nativeReviews[adoption.ReviewSha256] {
		return errors.New("economic native adoption requires original history and a fresh review")
	}
	if err := next.archiveView.check(); err != nil {
		return err
	}
	next.NativeRenewal = adoption
	operating, err := next.nativeOperatingPolicy(policy)
	if err != nil {
		return err
	}
	if _, err := loadNativeProducerAuthorities(ctx, operating.Observation); err != nil {
		return err
	}
	// Configuration adoption cannot alter an already acknowledged completion.
	if err := next.Native.ExecutionProducer.validate(operating.Observation, next.Native.Cursor); err != nil {
		return err
	}
	next.ContentHash = next.hash()
	return errors.Join(ctx.Err(), next.validate(ctx, policy))
}
