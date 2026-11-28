// The original policy selects a physical snapshot format. Signed resource
// revisions may grow its logical budget but cannot replace that owner format.
package main

import (
	"context"
	"errors"
)

const economicConservationStorageSchema = "urnetwork-economic-conservation-storage-v1"
const economicConservationStorageKind = "mainnet-economic-conservation-checkpoint-v1"
const economicConservationStorageMaximum = 32 * 1024 * 1024
const economicConservationMaximumFacts = 1024 * 1024

// Nil preserves the original one-MiB owner and its checksum grammar. This
// review concerns local retention only; it grants no economic/runtime authority.
type economicConservationStorageProfile struct {
	Schema       string `json:"schema"`
	Kind         string `json:"kind"`
	MaximumBytes uint64 `json:"maximum_bytes"`
	ReviewSha256 string `json:"review_sha256"`
}

func (self *economicConservationStorageProfile) validate() error {
	if self == nil {
		return nil
	}
	if self.Schema != economicConservationStorageSchema || self.Kind != economicConservationStorageKind || self.MaximumBytes != economicConservationStorageMaximum || !planSha256(self.ReviewSha256) {
		return errors.New("economic storage profile differs from the reviewed fixed physical owner")
	}
	return nil
}

func (self economicConservationPolicy) storageMaximum() uint64 {
	if self.StorageProfile == nil {
		return maxRpcReplyBytes
	}
	return self.StorageProfile.MaximumBytes
}

func (self economicConservationPolicy) storageKind() string {
	if self.StorageProfile == nil {
		return "mainnet-monitor-checkpoint"
	}
	return self.StorageProfile.Kind
}

func (self economicConservationResources) headBytes() uint64 {
	if self.HeadBytes == 0 {
		return maxRpcReplyBytes
	}
	return self.HeadBytes
}

func (self economicConservationPolicy) headBytes() uint64 {
	if self.operatingHeadBytes != 0 {
		return self.operatingHeadBytes
	}
	return self.initialResources().headBytes()
}

func (self economicConservationPolicy) validateReference(reference monitorHistoryReference) error {
	return errors.Join(self.StorageProfile.validate(), reference.validateLimit(self.storageMaximum()))
}

func (self economicConservationResources) validatePolicy(policy economicConservationPolicy) error {
	if err := errors.Join(self.validate(), policy.StorageProfile.validate()); err != nil {
		return err
	}
	if self.headBytes() > policy.storageMaximum() || policy.StorageProfile == nil && self.ActiveFacts > 65536 {
		return errors.New("economic resources exceed the original physical storage profile")
	}
	return nil
}

// Component summaries borrow only combined checkpoint references. These
// private bounds cannot be supplied in a standalone native or vault policy.
func (self economicConservationPolicy) withStorageProfile() economicConservationPolicy {
	self.Native.archiveReferenceBytes = self.storageMaximum()
	self.Vault.archiveReferenceBytes = self.storageMaximum()
	return self
}

func (self economicConservationPolicy) openCheckpoint(ctx context.Context, path string, expected identityExpectation) (*monitorCheckpointStore, error) {
	if err := self.StorageProfile.validate(); err != nil {
		return nil, err
	}
	return openMonitorCheckpointProfile(path, expected, self.storageKind(), int(self.storageMaximum()), ctx)
}

func (self economicConservationPolicy) openHistorySnapshot(ctx context.Context, path string, write bool) (*monitorHistorySnapshot, error) {
	if err := self.StorageProfile.validate(); err != nil {
		return nil, err
	}
	return openMonitorHistorySnapshotProfile(ctx, path, write, self.storageKind(), int(self.storageMaximum()))
}

func (self economicConservationPolicy) openHistoryReader(ctx context.Context, reference monitorHistoryReference) (*monitorHistorySnapshot, []byte, error) {
	if err := self.validateReference(reference); err != nil {
		return nil, nil, err
	}
	return openMonitorHistoryReaderProfile(ctx, reference, self.storageKind(), int(self.storageMaximum()))
}

// Keep the generic monitor wrapper at its original fixed profile. Both live
// publication and storage preparation use this exact pair, never a parser cap.
func validateMonitorCheckpointProfile(kind string, maximum int) error {
	if kind == "mainnet-monitor-checkpoint" && maximum == maxRpcReplyBytes || kind == economicConservationStorageKind && maximum == economicConservationStorageMaximum {
		return nil
	}
	return errors.New("checkpoint kind and byte limit differ from the fixed physical profile")
}
