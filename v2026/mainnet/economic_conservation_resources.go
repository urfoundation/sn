// Operational growth retains the original economic policy. The independent
// key may enlarge finite local resources; it cannot change routes, obligations,
// runtime admission or an original source boundary. No private key is read.
package main

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
)

const economicConservationResourcesSchema = "urnetwork-economic-conservation-resources-v1"
const economicConservationRenewalSchema = "urnetwork-economic-conservation-renewal-v1"

type economicConservationResources struct {
	HeadBytes         uint64 `json:"head_bytes,omitempty"`
	ActiveFacts       uint64 `json:"active_facts"`
	ReadBudgetSeconds uint64 `json:"read_budget_seconds"`
	ArchiveSegments   uint64 `json:"archive_segments"`
	IndexEntries      uint64 `json:"index_entries"`
	IndexBytes        uint64 `json:"index_bytes"`
}

func (self economicConservationResources) validate() error {
	maximumFacts := uint64(65536)
	if self.HeadBytes > maxRpcReplyBytes {
		maximumFacts = economicConservationMaximumFacts
	}
	if self.HeadBytes != 0 && (self.HeadBytes < maxRpcReplyBytes || self.HeadBytes > economicConservationStorageMaximum) || self.ActiveFacts < 16 || self.ActiveFacts > maximumFacts || self.ReadBudgetSeconds < 60 || self.ReadBudgetSeconds > 900 || self.ArchiveSegments < 128 || self.ArchiveSegments > 512 || self.IndexEntries < 8192 || self.IndexEntries > 1024*1024 || self.IndexBytes < 1024*1024 || self.IndexBytes > 256*1024*1024 {
		return errors.New("economic conservation resources exceed their separate finite profiles")
	}
	return nil
}

func (self economicConservationResources) includes(prior economicConservationResources) bool {
	return self.headBytes() >= prior.headBytes() && self.ActiveFacts >= prior.ActiveFacts && self.ReadBudgetSeconds >= prior.ReadBudgetSeconds && self.ArchiveSegments >= prior.ArchiveSegments && self.IndexEntries >= prior.IndexEntries && self.IndexBytes >= prior.IndexBytes
}

type economicConservationContinuationPolicy struct {
	Schema            string                        `json:"schema"`
	ApprovalPublicKey string                        `json:"approval_ed25519_public_key"`
	ReviewSha256      string                        `json:"review_sha256"`
	Initial           economicConservationResources `json:"initial"`
}

func (self economicConservationPolicy) initialResources() economicConservationResources {
	if self.resourceBasis != nil {
		return self.resourceBasis.initialResources()
	}
	if self.Continuation != nil {
		return self.Continuation.Initial
	}
	headBytes := uint64(0)
	if self.StorageProfile != nil {
		headBytes = self.storageMaximum()
	}
	return economicConservationResources{HeadBytes: headBytes, ActiveFacts: self.MaximumFacts, ReadBudgetSeconds: monitorEconomicReadSeconds(self.ReadBudgetSeconds), ArchiveSegments: 128, IndexEntries: 65536, IndexBytes: 64 * 1024 * 1024}
}

func (self economicConservationPolicy) identityHash() string {
	if self.resourceBasis != nil {
		self.MaximumFacts, self.ReadBudgetSeconds = self.resourceBasis.MaximumFacts, self.resourceBasis.ReadBudgetSeconds
		self.Claims = self.resourceBasis.Claims
		self.Native = self.resourceBasis.Native
	}
	return rootObjectHash(self)
}

func (self *economicConservationContinuationPolicy) validate(policy economicConservationPolicy) error {
	if self == nil {
		return nil
	}
	if self.Schema != economicConservationResourcesSchema || !rootCanonicalHash(self.ApprovalPublicKey) || !planSha256(self.ReviewSha256) || self.Initial.ActiveFacts != policy.MaximumFacts || self.Initial.ReadBudgetSeconds != monitorEconomicReadSeconds(policy.ReadBudgetSeconds) {
		return errors.New("economic conservation continuation must pin its approver and initial resource basis at original admission")
	}
	return self.Initial.validatePolicy(policy)
}

// Each revision binds the exact old checkpoint and previous signed revision.
// Old revisions move only into exact archive snapshots; ordinal is cumulative,
// not a fixed in-memory review window which eventually resets history.
type economicConservationRenewal struct {
	Schema       string                        `json:"schema"`
	PolicyHash   string                        `json:"policy_hash"`
	Original     monitorHistoryReference       `json:"original"`
	Ordinal      uint64                        `json:"ordinal"`
	Previous     string                        `json:"previous"`
	ReviewSha256 string                        `json:"review_sha256"`
	From         economicConservationResources `json:"from"`
	To           economicConservationResources `json:"to"`
	Signature    string                        `json:"signature_ed25519,omitempty"`
}

func (self economicConservationRenewal) signingBytes() ([]byte, error) {
	if self.Schema != economicConservationRenewalSchema || !planSha256(self.PolicyHash) || self.Ordinal == 0 || !planSha256(self.Previous) || !planSha256(self.ReviewSha256) || self.From == self.To || !self.To.includes(self.From) {
		return nil, errors.New("economic conservation renewal lacks monotonic resources or original review lineage")
	}
	if err := errors.Join(self.Original.validateLimit(self.From.headBytes()), self.From.validate(), self.To.validate()); err != nil {
		return nil, err
	}
	self.Signature = ""
	raw, err := json.Marshal(self)
	if err != nil {
		return nil, err
	}
	return append([]byte(economicConservationRenewalSchema+"\x00"), raw...), nil
}

func (self economicConservationRenewal) verify(policy economicConservationPolicy) error {
	if policy.Continuation == nil {
		return errors.New("legacy economic policy cannot enroll a continuation approver")
	}
	if err := errors.Join(self.From.validatePolicy(policy), self.To.validatePolicy(policy), policy.validateReference(self.Original)); err != nil {
		return err
	}
	message, err := self.signingBytes()
	key, keyErr := rootReceiptHex(policy.Continuation.ApprovalPublicKey, ed25519.PublicKeySize)
	signature, signatureErr := rootOfflineSignatureBytes(self.Signature)
	if err != nil || keyErr != nil || signatureErr != nil || self.PolicyHash != policy.identityHash() || !ed25519.Verify(key, message, signature) {
		return errors.New("economic conservation renewal lacks its original independent signature")
	}
	return nil
}

func (self *economicConservationState) resources(policy economicConservationPolicy) (economicConservationResources, error) {
	resources, previous, ordinal := policy.initialResources(), policy.identityHash(), uint64(0)
	if self.Archive != nil {
		if len(self.Archive.Segments) == 0 {
			return resources, errors.New("economic archive has no retained segment")
		}
		resources, previous, ordinal = self.Archive.Resources, self.Archive.LastRenewalHash, self.Archive.LastRenewalOrdinal
		if !resources.includes(policy.initialResources()) || !planSha256(previous) || ordinal == 0 && previous != policy.identityHash() {
			return resources, errors.New("economic archive changed its original capacity or review basis")
		}
	}
	if self.Renewal != nil {
		if self.Archive == nil || ordinal == ^uint64(0) || self.Renewal.Ordinal != ordinal+1 || self.Renewal.Previous != previous || self.Renewal.From != resources || self.Renewal.Original.Sha256 != self.Archive.Segments[len(self.Archive.Segments)-1].Sha256 || self.Renewal.Original.Bytes != self.Archive.Segments[len(self.Archive.Segments)-1].Bytes {
			return resources, errors.New("economic resource renewal replaced original progress or predecessor")
		}
		if err := self.Renewal.verify(policy); err != nil {
			return resources, err
		}
		resources = self.Renewal.To
	}
	return resources, resources.validatePolicy(policy)
}

func (self *economicConservationState) operatingPolicy(policy economicConservationPolicy) (economicConservationPolicy, error) {
	resources, err := self.resources(policy)
	if err != nil {
		return policy, err
	}
	if policy.resourceBasis == nil {
		original := policy
		policy.resourceBasis = &original
	}
	policy.MaximumFacts, policy.ReadBudgetSeconds = resources.ActiveFacts, resources.ReadBudgetSeconds
	policy.Native, err = self.nativeOperatingPolicy(policy)
	if err != nil {
		return policy, err
	}
	heads, err := self.claimHeads(policy)
	if err != nil {
		return policy, err
	}
	policy.Claims = make([]monitorClaimPolicy, len(heads))
	for index, head := range heads {
		policy.Claims[index] = head.Policy
		if self.archiveView != nil && self.archiveView.claimWork != nil {
			policy.Claims[index].work = func(stage string, units uint64) { self.archiveView.claimWork(head.Role, stage, units) }
		}
	}
	policy.operatingHeadBytes = resources.headBytes()
	return policy.withStorageProfile(), nil
}
