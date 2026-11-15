// Operational growth is an explicit reviewed revision of the original policy,
// not a new contract/census identity. Previously acknowledged limits cannot shrink.
package main

import "errors"

type monitorEvmResources struct {
	ReadBudgetSeconds uint64 `json:"read_budget_seconds"`
	HistoryEntries    uint64 `json:"history_entries"`
	BatchBlocks       uint64 `json:"batch_blocks"`
	StallSeconds      uint64 `json:"stall_seconds"`
}

type monitorEvmResourceRevision struct {
	Original             monitorEvmResources `json:"original"`
	ReviewSha256         string              `json:"review_sha256"`
	ReviewHistoryEntries uint64              `json:"review_history_entries,omitempty"`
}

const defaultMonitorEvmReviewHistoryEntries = 32
const maximumMonitorEvmReviewHistoryEntries = 512

// Review digests reference independently managed local configuration. Merely
// accepting a digest's syntax does not verify a signature or review authority.
func (self monitorEconomicEvmPolicy) reviewHistoryEntries() uint64 {
	if self.ResourceRevision != nil && self.ResourceRevision.ReviewHistoryEntries != 0 {
		return self.ResourceRevision.ReviewHistoryEntries
	}
	return defaultMonitorEvmReviewHistoryEntries
}

func (self monitorEconomicEvmPolicy) resourceReview() string {
	if self.ResourceRevision != nil {
		return self.ResourceRevision.ReviewSha256
	}
	return ""
}

func (self monitorEconomicEvmPolicy) originalResources() monitorEvmResources {
	if self.ResourceRevision != nil {
		return self.ResourceRevision.Original
	}
	return self.resources()
}

func (self monitorEconomicEvmPolicy) resources() monitorEvmResources {
	return monitorEvmResources{ReadBudgetSeconds: self.ReadBudgetSeconds, HistoryEntries: self.HistoryEntries, BatchBlocks: self.BatchBlocks, StallSeconds: self.StallSeconds}
}

func (self monitorEvmResources) seconds() uint64 {
	if self.ReadBudgetSeconds == 0 {
		return 300
	}
	return self.ReadBudgetSeconds
}

func (self monitorEvmResources) validate() error {
	if self.seconds() < 60 || self.seconds() > 900 || self.HistoryEntries == 0 || self.HistoryEntries > maximumMonitorEconomicEvents || self.BatchBlocks == 0 || self.BatchBlocks > maximumMonitorEvmBlocks || self.StallSeconds < 60 || self.StallSeconds > 3600 {
		return errors.New("EVM economic resource revision exceeds reviewed bounds")
	}
	return nil
}

func (self monitorEvmResources) includes(prior monitorEvmResources) bool {
	return self.seconds() >= prior.seconds() && self.HistoryEntries >= prior.HistoryEntries && self.BatchBlocks >= prior.BatchBlocks && self.StallSeconds >= prior.StallSeconds
}

func (self monitorEconomicEvmPolicy) validateResourceRevision() error {
	if err := self.resources().validate(); err != nil {
		return err
	}
	if self.ResourceRevision == nil {
		return nil
	}
	if !planSha256(self.ResourceRevision.ReviewSha256) || self.ResourceRevision.Original.validate() != nil || !self.resources().includes(self.ResourceRevision.Original) || self.reviewHistoryEntries() < defaultMonitorEvmReviewHistoryEntries || self.reviewHistoryEntries() > maximumMonitorEvmReviewHistoryEntries {
		return errors.New("EVM economic resource renewal lacks review or shrinks its original basis")
	}
	return nil
}

// Each acknowledged change retains its exact predecessor and configuration.
// Capacity may grow under a new review; no acknowledged entry is evicted.
type monitorEvmResourceAcknowledgment struct {
	Resources            monitorEvmResources `json:"resources"`
	ReviewSha256         string              `json:"review_sha256,omitempty"`
	ReviewHistoryEntries uint64              `json:"review_history_entries"`
	PreviousSha256       string              `json:"previous_sha256"`
	ContentHash          string              `json:"content_hash"`
}

func (self monitorEvmResourceAcknowledgment) hash() string {
	self.ContentHash = ""
	return rootObjectHash(self)
}

// Importing an original 0fb checkpoint retains its exact checksum and latest
// known review. Earlier overwritten reviews remain explicitly unavailable.
type monitorEvmResourceHistory struct {
	LegacyCheckpointSha256 string                             `json:"legacy_checkpoint_sha256,omitempty"`
	Entries                []monitorEvmResourceAcknowledgment `json:"entries"`
}

func (self monitorEvmResourceHistory) origin(policy monitorEconomicEvmPolicy) string {
	return rootObjectHash(struct {
		PolicyHash             string              `json:"policy_hash"`
		Original               monitorEvmResources `json:"original"`
		LegacyCheckpointSha256 string              `json:"legacy_checkpoint_sha256,omitempty"`
	}{PolicyHash: policy.identityHash(), Original: policy.originalResources(), LegacyCheckpointSha256: self.LegacyCheckpointSha256})
}

func (self monitorEvmResourceHistory) validate(policy monitorEconomicEvmPolicy) error {
	if len(self.Entries) == 0 || len(self.Entries) > maximumMonitorEvmReviewHistoryEntries || self.LegacyCheckpointSha256 != "" && !planSha256(self.LegacyCheckpointSha256) {
		return errors.New("EVM economic resource history is absent or exceeds its bound")
	}
	previous := self.origin(policy)
	resources, capacity := policy.originalResources(), uint64(defaultMonitorEvmReviewHistoryEntries)
	reviews := make(map[string]bool, len(self.Entries))
	for index, entry := range self.Entries {
		if entry.Resources.validate() != nil || !entry.Resources.includes(resources) || entry.ReviewHistoryEntries < capacity || entry.ReviewHistoryEntries > maximumMonitorEvmReviewHistoryEntries || uint64(index+1) > entry.ReviewHistoryEntries || entry.PreviousSha256 != previous || entry.ContentHash != entry.hash() {
			return errors.New("EVM economic acknowledged resource history changed or shrank")
		}
		if entry.ReviewSha256 != "" && !planSha256(entry.ReviewSha256) || entry.ReviewSha256 == "" && (index != 0 || entry.Resources != policy.originalResources() || entry.ReviewHistoryEntries != defaultMonitorEvmReviewHistoryEntries) {
			return errors.New("EVM economic resource growth lacks its retained review reference")
		}
		if entry.ReviewSha256 != "" {
			if reviews[entry.ReviewSha256] {
				return errors.New("EVM economic resource review reference was already acknowledged")
			}
			reviews[entry.ReviewSha256] = true
		}
		if index != 0 {
			prior := self.Entries[index-1]
			if prior.Resources == entry.Resources && prior.ReviewHistoryEntries == entry.ReviewHistoryEntries || prior.ReviewSha256 == entry.ReviewSha256 {
				return errors.New("EVM economic resource review was relabelled or reused")
			}
		}
		previous, resources, capacity = entry.ContentHash, entry.Resources, entry.ReviewHistoryEntries
	}
	if !policy.resources().includes(resources) || policy.reviewHistoryEntries() < capacity {
		return errors.New("EVM economic policy shrank acknowledged review history or resources")
	}
	return nil
}

// Build a candidate without mutating the retained acknowledgment. Only a
// successful durable publication installs the candidate in the worker.
func (self *monitorEvmResourceHistory) advance(policy monitorEconomicEvmPolicy) (*monitorEvmResourceHistory, error) {
	next := &monitorEvmResourceHistory{}
	if self != nil {
		if err := self.validate(policy); err != nil {
			return nil, err
		}
		next.LegacyCheckpointSha256 = self.LegacyCheckpointSha256
		next.Entries = append([]monitorEvmResourceAcknowledgment(nil), self.Entries...)
		last := next.Entries[len(next.Entries)-1]
		if last.Resources == policy.resources() && last.ReviewHistoryEntries == policy.reviewHistoryEntries() {
			if last.ReviewSha256 != policy.resourceReview() {
				return nil, errors.New("EVM economic acknowledged review cannot be relabelled")
			}
			return next, nil
		}
		if last.ReviewSha256 == policy.resourceReview() {
			return nil, errors.New("EVM economic changed resources require their own review reference")
		}
	}
	if uint64(len(next.Entries)) >= policy.reviewHistoryEntries() {
		return nil, errors.Join(errMonitorEconomicCapacity, errors.New("EVM economic review history requires reviewed capacity growth"))
	}
	previous := next.origin(policy)
	if len(next.Entries) != 0 {
		previous = next.Entries[len(next.Entries)-1].ContentHash
	}
	entry := monitorEvmResourceAcknowledgment{Resources: policy.resources(), ReviewSha256: policy.resourceReview(), ReviewHistoryEntries: policy.reviewHistoryEntries(), PreviousSha256: previous}
	entry.ContentHash = entry.hash()
	next.Entries = append(next.Entries, entry)
	if err := next.validate(policy); err != nil {
		return nil, err
	}
	return next, nil
}

// A fixed summary exposes a capacity warning before the next revision is
// refused. It does not export arbitrary review digests as metric labels.
type monitorEvmResourceHistorySummary struct {
	Entries              uint64 `json:"entries"`
	AcknowledgedCapacity uint64 `json:"acknowledged_capacity"`
	ConfiguredCapacity   uint64 `json:"configured_capacity"`
	Remaining            uint64 `json:"remaining"`
	CapacityWarning      bool   `json:"capacity_warning"`
	HeadSha256           string `json:"head_sha256,omitempty"`
	LegacyLatestOnly     bool   `json:"legacy_latest_only"`
	Authority            string `json:"authority"`
}

func (self *monitorEvmResourceHistory) summary(policy monitorEconomicEvmPolicy) monitorEvmResourceHistorySummary {
	value := monitorEvmResourceHistorySummary{ConfiguredCapacity: policy.reviewHistoryEntries(), Authority: "local-config-review-reference"}
	if self == nil || len(self.Entries) == 0 {
		return value
	}
	last := self.Entries[len(self.Entries)-1]
	value.Entries, value.AcknowledgedCapacity, value.HeadSha256 = uint64(len(self.Entries)), last.ReviewHistoryEntries, last.ContentHash
	value.LegacyLatestOnly = self.LegacyCheckpointSha256 != ""
	value.Remaining = value.AcknowledgedCapacity - value.Entries
	value.CapacityWarning = value.Remaining <= 2
	return value
}

func (self *monitorEconomicEvmPolicy) setResources(value monitorEvmResources) {
	self.ReadBudgetSeconds, self.HistoryEntries, self.BatchBlocks, self.StallSeconds = value.ReadBudgetSeconds, value.HistoryEntries, value.BatchBlocks, value.StallSeconds
}

func (self monitorEconomicEvmPolicy) identityHash() string {
	if self.ResourceRevision != nil {
		self.setResources(self.ResourceRevision.Original)
		self.ResourceRevision = nil
	}
	return rootObjectHash(self)
}
