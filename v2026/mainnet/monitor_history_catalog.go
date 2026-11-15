// Catalog revisions grant only bounded local retention capacity. The initial
// reviewed policy pins the approver; neither an imported approval nor a legacy
// checkpoint can enroll or replace that key. Original segments remain intact.
package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"

	"github.com/urnetwork/connect/v2026/durablevolume"
)

const monitorHistoryCatalogPolicySchema = "urnetwork-monitor-history-catalog-policy-v1"
const monitorHistoryCatalogRevisionSchema = "urnetwork-monitor-history-catalog-revision-v1"
const monitorHistoryCatalogApprovalSchema = "urnetwork-monitor-history-catalog-approval-v1"
const maximumReviewedMonitorHistorySegments = 512
const maximumMonitorHistoryCatalogBytes = 128 * 1024
const maximumMonitorHistoryRevisions = 16
const maximumMonitorHistoryRevisionBytes = 16 * 1024
const maximumMonitorHistoryApprovalBytes = maximumMonitorHistoryRevisionBytes + 1024

// Counts are independent of serialized metadata and of available disk space.
// HeldReaders counts guarded segment owners, each retaining its own descriptors.
type monitorHistoryCapacity struct {
	Segments     uint64 `json:"segments"`
	CatalogBytes uint64 `json:"catalog_bytes"`
	HeldReaders  uint64 `json:"held_readers"`
}

func (self monitorHistoryCapacity) validate() error {
	if self.Segments < maximumMonitorHistorySegments || self.Segments > maximumReviewedMonitorHistorySegments ||
		self.CatalogBytes < 64*1024 || self.CatalogBytes > maximumMonitorHistoryCatalogBytes ||
		self.HeldReaders < self.Segments || self.HeldReaders > maximumReviewedMonitorHistorySegments {
		return errors.New("monitor history capacity exceeds its separate segment, metadata or reader profile")
	}
	return nil
}

func (self monitorHistoryCapacity) grows(previous monitorHistoryCapacity) bool {
	return self != previous && self.Segments >= previous.Segments && self.CatalogBytes >= previous.CatalogBytes && self.HeldReaders >= previous.HeldReaders
}

// This key is independently provisioned in the original services policy. Its
// authority cannot sign a chain transaction, change economic scope or erase data.
type monitorHistoryCatalogPolicy struct {
	Schema            string                 `json:"schema"`
	ApprovalPublicKey string                 `json:"approval_ed25519_public_key"`
	ReviewSha256      string                 `json:"review_sha256"`
	InitialCapacity   monitorHistoryCapacity `json:"initial_capacity"`
}

func (self *monitorHistoryCatalogPolicy) validate() error {
	if self == nil {
		return nil
	}
	if self.Schema != monitorHistoryCatalogPolicySchema || !rootCanonicalHash(self.ApprovalPublicKey) || !planSha256(self.ReviewSha256) {
		return errors.New("monitor history policy lacks its original independent approver and review")
	}
	return self.InitialCapacity.validate()
}

// The original reference and progress hash bind the exact prior checkpoint.
// Previous chains all approvals, including approvals whose resources later grew.
type monitorHistoryCatalogRevision struct {
	Schema               string                  `json:"schema"`
	Role                 string                  `json:"role"`
	PolicyHash           string                  `json:"policy_hash"`
	Ordinal              uint64                  `json:"ordinal"`
	Previous             string                  `json:"previous"`
	Original             monitorHistoryReference `json:"original"`
	ProgressHash         string                  `json:"progress_hash"`
	From                 monitorHistoryCapacity  `json:"from"`
	To                   monitorHistoryCapacity  `json:"to"`
	FutureSegments       uint64                  `json:"future_segments"`
	RequiredSegments     uint64                  `json:"required_segments"`
	RequiredCatalogBytes uint64                  `json:"required_catalog_bytes"`
	RequiredBytes        uint64                  `json:"required_bytes"`
	RequiredInodes       uint64                  `json:"required_inodes"`
	Declaration          durablevolume.Reference `json:"declaration"`
	FormerWriterFence    planFileReference       `json:"former_writer_fence"`
}

func (self monitorHistoryCatalogRevision) signingBytes() ([]byte, error) {
	if self.Schema != monitorHistoryCatalogRevisionSchema || !monitorRolePattern.MatchString(self.Role) || !planSha256(self.PolicyHash) ||
		self.Ordinal == 0 || self.Ordinal > maximumMonitorHistoryRevisions || !planSha256(self.Previous) || !planSha256(self.ProgressHash) ||
		self.FutureSegments == 0 || self.FutureSegments > maximumReviewedMonitorHistorySegments/2 || !self.To.grows(self.From) ||
		self.RequiredSegments == 0 || self.RequiredSegments > self.To.Segments || self.RequiredSegments > self.To.HeldReaders ||
		self.RequiredCatalogBytes == 0 || self.RequiredCatalogBytes > self.To.CatalogBytes ||
		self.RequiredBytes == 0 || self.RequiredInodes == 0 {
		return nil, errors.New("monitor history revision lacks its bounded original progress and monotonic capacity")
	}
	if err := errors.Join(self.Original.validate(), self.From.validate(), self.To.validate()); err != nil {
		return nil, err
	}
	for _, reference := range []planFileReference{{Path: self.Declaration.Path, Sha256: self.Declaration.Sha256}, self.FormerWriterFence} {
		if !bootstrapRootAbsolutePath(reference.Path) || !planSha256(reference.Sha256) || monitorHistoryPathsAlias(reference.Path, self.Original.Path) {
			return nil, errors.New("monitor history revision declaration and fence must be independently pinned")
		}
	}
	raw, err := json.Marshal(self)
	if err != nil || len(raw) > maximumMonitorHistoryRevisionBytes {
		return nil, errors.Join(errors.New("monitor history revision exceeds its finite signature frame"), err)
	}
	return append([]byte(monitorHistoryCatalogRevisionSchema+"\x00"), raw...), nil
}

type monitorHistoryCatalogApproval struct {
	Schema    string                        `json:"schema"`
	Revision  monitorHistoryCatalogRevision `json:"revision"`
	Signature string                        `json:"signature_ed25519"`
}

func (self monitorHistoryCatalogApproval) validate(policy *monitorHistoryCatalogPolicy) error {
	if policy == nil {
		return errors.New("legacy monitor policy cannot enroll a catalog approval key")
	}
	if err := policy.validate(); err != nil {
		return err
	}
	message, messageErr := self.Revision.signingBytes()
	signature, signatureErr := rootOfflineSignatureBytes(self.Signature)
	key, keyErr := rootReceiptHex(policy.ApprovalPublicKey, ed25519.PublicKeySize)
	if self.Schema != monitorHistoryCatalogApprovalSchema || messageErr != nil || signatureErr != nil || keyErr != nil || !ed25519.Verify(key, message, signature) {
		return errors.New("monitor history catalog approval signature or original authority differs")
	}
	raw, err := json.Marshal(self)
	if err != nil || len(raw)+1 > maximumMonitorHistoryApprovalBytes {
		return errors.Join(errors.New("monitor history complete approval frame exceeds its import bound"), err)
	}
	return nil
}

// The synchronous role owns this immutable slice after admission. Hot samples
// reuse that admitted state; signature verification occurs at reopen or import.
type monitorHistoryCatalogState struct {
	Revisions []monitorHistoryCatalogApproval `json:"revisions"`
}

func (self *monitorHistoryCatalogState) capacity(policy *monitorHistoryCatalogPolicy) monitorHistoryCapacity {
	if self != nil && len(self.Revisions) != 0 {
		return self.Revisions[len(self.Revisions)-1].Revision.To
	}
	if policy != nil {
		return policy.InitialCapacity
	}
	return monitorHistoryCapacity{Segments: maximumMonitorHistorySegments, CatalogBytes: maxRpcReplyBytes, HeldReaders: maximumMonitorHistorySegments}
}

func (self *monitorHistoryCatalogState) previous(policyHash string) string {
	if self == nil || len(self.Revisions) == 0 {
		return policyHash
	}
	return rootObjectHash(self.Revisions[len(self.Revisions)-1])
}

// Admission validates the complete signed chain once. A legacy nil policy may
// retain original history, but cannot acquire unsigned capacity acknowledgments.
func (self *monitorHistoryCatalogState) validate(policy *monitorHistoryCatalogPolicy, role, policyHash, path string) error {
	if self == nil {
		return nil
	}
	if policy == nil || len(self.Revisions) == 0 || len(self.Revisions) > maximumMonitorHistoryRevisions {
		return errors.New("monitor history catalog lacks its original policy or retained approval chain")
	}
	capacity, previous := policy.InitialCapacity, policyHash
	for index, approval := range self.Revisions {
		revision := approval.Revision
		if revision.Role != role || revision.PolicyHash != policyHash || revision.Ordinal != uint64(index+1) || revision.Previous != previous || revision.From != capacity ||
			(path != "" && revision.Original.Path != path) {
			return errors.New("monitor history catalog changed role, predecessor or original checkpoint path")
		}
		if err := approval.validate(policy); err != nil {
			return err
		}
		capacity, previous = revision.To, rootObjectHash(approval)
	}
	raw, err := json.Marshal(self)
	if err != nil || uint64(len(raw)) > capacity.CatalogBytes {
		return errors.Join(errors.New("monitor history approval chain exceeds its signed byte capacity"), err)
	}
	return nil
}

func (self *monitorHistoryCatalogState) retains(previous *monitorHistoryCatalogState) bool {
	if previous == nil {
		return true
	}
	return self != nil && len(self.Revisions) >= len(previous.Revisions) && reflect.DeepEqual(self.Revisions[:len(previous.Revisions)], previous.Revisions)
}

func (self *monitorHistoryCatalogState) checkPath(path string) error {
	if self != nil {
		for _, approval := range self.Revisions {
			if approval.Revision.Original.Path != path {
				return errors.New("monitor history catalog moved to a different original checkpoint path")
			}
		}
	}
	return nil
}

// This helper emits public bytes only; production code never receives a key.
func monitorHistorySigningHex(revision monitorHistoryCatalogRevision) (string, error) {
	raw, err := revision.signingBytes()
	return hex.EncodeToString(raw), err
}
