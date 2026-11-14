// Independently configured policy growth retains every acknowledged basis.
// Digests reference local review artifacts; their syntax grants no signature,
// finality or payment authority. Owners acknowledge only durable publication.
package main

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const defaultMonitorProgressReviews = 32
const maximumMonitorProgressReviews = 128
const maximumMonitorRetainedClaimEpochs = 128

// Zero optional fields preserve the original policy's exact JSON hash.
type monitorProgressPolicyResources struct {
	FreshnessSeconds     uint64 `json:"freshness_seconds"`
	ReadBudgetSeconds    uint64 `json:"read_budget_seconds"`
	Epochs               uint64 `json:"epochs"`
	EpochCapacity        uint64 `json:"epoch_capacity"`
	ReviewHistoryEntries uint64 `json:"review_history_entries"`
}

func (self monitorProgressPolicyResources) readSeconds() uint64 {
	if self.ReadBudgetSeconds == 0 {
		return 300
	}
	return self.ReadBudgetSeconds
}

func (self monitorProgressPolicyResources) epochCapacity() uint64 {
	if self.EpochCapacity == 0 {
		return maxMonitorClaimEpochs
	}
	return self.EpochCapacity
}

func (self monitorProgressPolicyResources) reviews() uint64 {
	if self.ReviewHistoryEntries == 0 {
		return defaultMonitorProgressReviews
	}
	return self.ReviewHistoryEntries
}

func (self monitorProgressPolicyResources) validate(claim bool) error {
	if self.FreshnessSeconds < 1 || self.FreshnessSeconds > 300 || self.readSeconds() < 60 || self.readSeconds() > 900 || self.reviews() < defaultMonitorProgressReviews || self.reviews() > maximumMonitorProgressReviews {
		return errors.New("progress policy resources exceed bounded observation limits")
	}
	if claim && (self.Epochs == 0 || self.Epochs > self.epochCapacity() || self.epochCapacity() < maxMonitorClaimEpochs || self.epochCapacity() > maximumMonitorRetainedClaimEpochs) || !claim && (self.Epochs != 0 || self.EpochCapacity != 0) {
		return errors.New("progress expected epoch census exceeds declared capacity")
	}
	return nil
}

func (self monitorProgressPolicyResources) includes(prior monitorProgressPolicyResources) bool {
	return self.FreshnessSeconds >= prior.FreshnessSeconds && self.readSeconds() >= prior.readSeconds() && self.Epochs >= prior.Epochs && self.epochCapacity() >= prior.epochCapacity() && self.reviews() >= prior.reviews()
}

// A previous checkpoint checksum is used only for the first legacy import.
// Later changes name the acknowledged revision hash exported by the owner.
type monitorProgressPolicyRenewal struct {
	Original       monitorProgressPolicyResources `json:"original"`
	PreviousSha256 string                         `json:"previous_sha256"`
	ReviewSha256   string                         `json:"review_sha256"`
}

func validateMonitorProgressRenewal(current monitorProgressPolicyResources, renewal *monitorProgressPolicyRenewal, claim bool) error {
	if err := current.validate(claim); err != nil {
		return err
	}
	if renewal != nil && (renewal.Original.validate(claim) != nil || !current.includes(renewal.Original) || !planSha256(renewal.PreviousSha256) || !planSha256(renewal.ReviewSha256)) {
		return errors.New("progress policy renewal lacks its original basis or review reference")
	}
	return nil
}

type monitorProgressPolicyAcknowledgment struct {
	Resources      monitorProgressPolicyResources `json:"resources"`
	PolicyHash     string                         `json:"policy_hash"`
	ReviewSha256   string                         `json:"review_sha256,omitempty"`
	PreviousSha256 string                         `json:"previous_sha256"`
	ContentHash    string                         `json:"content_hash"`
}

func (self monitorProgressPolicyAcknowledgment) hash() string {
	self.ContentHash = ""
	return rootObjectHash(self)
}

type monitorProgressPolicyHistory struct {
	LegacyCheckpointSha256 string                                `json:"legacy_checkpoint_sha256,omitempty"`
	Entries                []monitorProgressPolicyAcknowledgment `json:"entries"`
}

func (self monitorProgressPolicyHistory) origin(policyHash string) string {
	return rootObjectHash(struct {
		PolicyHash             string `json:"policy_hash"`
		LegacyCheckpointSha256 string `json:"legacy_checkpoint_sha256,omitempty"`
	}{PolicyHash: policyHash, LegacyCheckpointSha256: self.LegacyCheckpointSha256})
}

// Every earlier policy is reconstructed from the retained expectation prefix.
// A replacement deadline, share, pool, member or endpoint changes that digest.
func (self monitorProgressPolicyHistory) validate(claim bool, originalHash string, hashAt func(monitorProgressPolicyResources) string) error {
	if len(self.Entries) == 0 || len(self.Entries) > maximumMonitorProgressReviews || self.LegacyCheckpointSha256 != "" && !rootCanonicalHash("0x"+self.LegacyCheckpointSha256) {
		return errors.New("progress policy history is absent or exceeds its bound")
	}
	previous := self.origin(originalHash)
	seen := make(map[string]bool, len(self.Entries))
	var prior monitorProgressPolicyResources
	for index, entry := range self.Entries {
		if entry.Resources.validate(claim) != nil || entry.PolicyHash != hashAt(entry.Resources) || !rootCanonicalHash("0x"+entry.PolicyHash) || entry.PreviousSha256 != previous || entry.ContentHash != entry.hash() || uint64(index+1) > entry.Resources.reviews() {
			return errors.New("progress acknowledged policy history differs from its original census")
		}
		if index == 0 {
			if entry.PolicyHash != originalHash || entry.ReviewSha256 != "" {
				return errors.New("progress policy origin differs")
			}
		} else if !entry.Resources.includes(prior) || entry.Resources == prior || !planSha256(entry.ReviewSha256) || seen[entry.ReviewSha256] {
			return errors.New("progress policy revision shrank, repeated, or lost its review reference")
		}
		if entry.ReviewSha256 != "" {
			seen[entry.ReviewSha256] = true
		}
		prior, previous = entry.Resources, entry.ContentHash
	}
	return nil
}

func newMonitorProgressPolicyHistory(resources monitorProgressPolicyResources, policyHash, legacyHash string) *monitorProgressPolicyHistory {
	history := &monitorProgressPolicyHistory{LegacyCheckpointSha256: legacyHash}
	entry := monitorProgressPolicyAcknowledgment{Resources: resources, PolicyHash: policyHash, PreviousSha256: history.origin(policyHash)}
	entry.ContentHash = entry.hash()
	history.Entries = []monitorProgressPolicyAcknowledgment{entry}
	return history
}

// A returned candidate remains unacknowledged until the owning checkpoint's
// publication and physical guard both succeed. Failure never edits the input.
func renewMonitorProgressPolicy(claim bool, current monitorProgressPolicyResources, renewal *monitorProgressPolicyRenewal, originalHash, checkpointHash string, retained *monitorProgressPolicyHistory, hashAt func(monitorProgressPolicyResources) string) (*monitorProgressPolicyHistory, monitorProgressPolicyResources, int, error) {
	if err := validateMonitorProgressRenewal(current, renewal, claim); err != nil {
		return nil, current, 0, err
	}
	acknowledged := 0
	if retained == nil {
		original := current
		if renewal != nil {
			original = renewal.Original
		}
		if hashAt(original) != originalHash {
			return nil, current, 0, errors.New("legacy progress checkpoint differs from original policy")
		}
		retained = newMonitorProgressPolicyHistory(original, originalHash, checkpointHash)
	} else {
		acknowledged = len(retained.Entries)
	}
	if err := retained.validate(claim, originalHash, hashAt); err != nil {
		return nil, current, 0, err
	}
	last := retained.Entries[len(retained.Entries)-1]
	if !current.includes(last.Resources) {
		return nil, last.Resources, acknowledged, errors.New("progress policy cannot shrink acknowledged resources or expectations")
	}
	if renewal != nil && renewal.Original != retained.Entries[0].Resources {
		return nil, last.Resources, acknowledged, errors.New("progress policy renewal changed its original basis")
	}
	if current == last.Resources {
		if len(retained.Entries) > 1 {
			previous := last.PreviousSha256
			if len(retained.Entries) == 2 && retained.LegacyCheckpointSha256 != "" {
				previous = "sha256:" + retained.LegacyCheckpointSha256
			}
			if renewal == nil || renewal.ReviewSha256 != last.ReviewSha256 || renewal.PreviousSha256 != previous {
				return nil, last.Resources, acknowledged, errors.New("progress policy restart lost its acknowledged review")
			}
		} else if renewal != nil {
			return nil, last.Resources, acknowledged, errors.New("progress review does not describe a policy change")
		}
		return retained, last.Resources, acknowledged, nil
	}
	if renewal == nil {
		return nil, last.Resources, acknowledged, errors.New("progress policy change requires an explicit retained renewal")
	}
	previous := last.ContentHash
	if len(retained.Entries) == 1 && retained.LegacyCheckpointSha256 != "" {
		previous = "sha256:" + retained.LegacyCheckpointSha256
	}
	if renewal.PreviousSha256 != previous {
		return nil, last.Resources, acknowledged, errors.New("progress policy renewal names a stale predecessor")
	}
	if uint64(len(retained.Entries)+1) > current.reviews() {
		return nil, last.Resources, acknowledged, errors.New("progress review history capacity requires reviewed growth or archive")
	}
	candidate := &monitorProgressPolicyHistory{LegacyCheckpointSha256: retained.LegacyCheckpointSha256, Entries: append([]monitorProgressPolicyAcknowledgment(nil), retained.Entries...)}
	entry := monitorProgressPolicyAcknowledgment{Resources: current, PolicyHash: hashAt(current), ReviewSha256: renewal.ReviewSha256, PreviousSha256: last.ContentHash}
	entry.ContentHash = entry.hash()
	candidate.Entries = append(candidate.Entries, entry)
	if err := candidate.validate(claim, originalHash, hashAt); err != nil {
		return nil, last.Resources, acknowledged, err
	}
	return candidate, last.Resources, acknowledged, nil
}

// Counts and limits have fixed labels. Review digests belong in bounded events,
// never metric labels. Zero acknowledged entries means publication is pending.
type monitorProgressPolicyStatus struct {
	Acknowledged       int    `json:"acknowledged_revisions"`
	Pending            bool   `json:"revision_pending"`
	RemainingReviews   uint64 `json:"remaining_reviews"`
	RemainingEpochs    uint64 `json:"remaining_epochs"`
	CapacityWarning    bool   `json:"capacity_warning"`
	AcknowledgedSha256 string `json:"acknowledged_sha256,omitempty"`
	ReviewAuthority    string `json:"review_authority"`
}

func progressPolicyStatus(history *monitorProgressPolicyHistory, acknowledged int) monitorProgressPolicyStatus {
	value := monitorProgressPolicyStatus{Acknowledged: acknowledged, ReviewAuthority: "local-config-review-reference"}
	if history == nil || len(history.Entries) == 0 {
		return value
	}
	last := history.Entries[len(history.Entries)-1].Resources
	value.Pending = acknowledged != len(history.Entries)
	value.RemainingReviews = last.reviews() - uint64(len(history.Entries))
	if last.Epochs > 0 {
		value.RemainingEpochs = last.epochCapacity() - last.Epochs
	}
	value.CapacityWarning = value.RemainingReviews <= 2 || last.Epochs > 0 && value.RemainingEpochs <= 2
	if acknowledged > 0 && acknowledged <= len(history.Entries) {
		value.AcknowledgedSha256 = history.Entries[acknowledged-1].ContentHash
	}
	return value
}

func appendMonitorProgressPolicyMetrics(raw []byte, prefix, role string, history *monitorProgressPolicyHistory, acknowledged int) []byte {
	status := progressPolicyStatus(history, acknowledged)
	flag := func(value bool) int {
		if value {
			return 1
		}
		return 0
	}
	var output strings.Builder
	for _, metric := range []struct {
		name  string
		value any
	}{
		{name: "policy_acknowledged_revisions", value: status.Acknowledged},
		{name: "policy_revision_pending", value: flag(status.Pending)},
		{name: "policy_remaining_reviews", value: status.RemainingReviews},
		{name: "policy_remaining_epochs", value: status.RemainingEpochs},
		{name: "policy_capacity_warning", value: flag(status.CapacityWarning)},
		{name: "policy_review_signature_verified", value: 0},
	} {
		fmt.Fprintf(&output, "# TYPE %s_%s gauge\n%s_%s{role=%q} %v\n", prefix, metric.name, prefix, metric.name, role, metric.value)
	}
	return append(raw, output.String()...)
}

func (self monitorClaimPolicy) resources() monitorProgressPolicyResources {
	return monitorProgressPolicyResources{FreshnessSeconds: self.FreshnessSeconds, ReadBudgetSeconds: self.ReadBudgetSeconds, Epochs: uint64(len(self.Epochs)), EpochCapacity: self.EpochCapacity, ReviewHistoryEntries: self.ReviewHistoryEntries}
}

func (self monitorClaimPolicy) atResources(resources monitorProgressPolicyResources) monitorClaimPolicy {
	self.FreshnessSeconds, self.ReadBudgetSeconds = resources.FreshnessSeconds, resources.ReadBudgetSeconds
	self.EpochCapacity, self.ReviewHistoryEntries, self.Renewal = resources.EpochCapacity, resources.ReviewHistoryEntries, nil
	if resources.Epochs > uint64(len(self.Epochs)) {
		self.Epochs = nil
	} else {
		self.Epochs = self.Epochs[:resources.Epochs]
	}
	return self
}

func (self monitorClaimPolicy) policyHashAt(resources monitorProgressPolicyResources) string {
	if resources.Epochs > uint64(len(self.Epochs)) {
		return ""
	}
	self.observeWork("policy-hash", 1)
	self.observeWork("policy-hashed-epoch", resources.Epochs)
	return self.atResources(resources).hash()
}

func (self monitorProviderPolicy) resources() monitorProgressPolicyResources {
	return monitorProgressPolicyResources{FreshnessSeconds: self.FreshnessSeconds, ReadBudgetSeconds: self.ReadBudgetSeconds, ReviewHistoryEntries: self.ReviewHistoryEntries}
}

func (self monitorProviderPolicy) atResources(resources monitorProgressPolicyResources) monitorProviderPolicy {
	self.FreshnessSeconds, self.ReadBudgetSeconds, self.ReviewHistoryEntries, self.Renewal = resources.FreshnessSeconds, resources.ReadBudgetSeconds, resources.ReviewHistoryEntries, nil
	return self
}

func (self monitorProviderPolicy) policyHashAt(resources monitorProgressPolicyResources) string {
	return self.atResources(resources).hash()
}

func (self monitorProgressPolicyResources) readBudget() time.Duration {
	return time.Duration(self.readSeconds()) * time.Second
}
