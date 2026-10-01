package validator

// An explicit source successor carries its original finite lifetime alongside
// one exact doubling. Per-record limits, policy and signed inputs stay fixed.

import (
	"errors"
	"reflect"
)

// The private simulator handoff pins this pair to its approved successor.
type ReleaseEvidenceV2SourceBounds struct {
	Original ReleaseEvidenceV2Bounds `json:"original"`
	Approved ReleaseEvidenceV2Bounds `json:"approved"`
}

// Construct only the lifetime transition supported by a V6 relay approval.
func DoubledReleaseEvidenceV2SourceBounds(original ReleaseEvidenceV2Bounds) (ReleaseEvidenceV2Bounds, error) {
	if err := original.Validate(2); err != nil {
		return ReleaseEvidenceV2Bounds{}, err
	}
	next := original
	for _, value := range []*uint64{
		&next.Disk.MaxRecordCount, &next.Disk.MaxTrailCount,
		&next.Disk.MaxRawRecordBytes, &next.Disk.MaxStorageBytes,
		&next.Disk.MaxStorageFiles, &next.Disk.MaxProofBytes,
		&next.Cut.Records.MaxDataBytes, &next.Cut.Records.MaxItems,
		&next.Cut.Records.MaxChunks, &next.Cut.Records.MaxPages,
		&next.Cut.Proofs.MaxDataBytes, &next.Cut.Proofs.MaxItems,
		&next.Cut.Proofs.MaxChunks, &next.Cut.Proofs.MaxPages,
		&next.Replay.MaxTrails, &next.Replay.MaxScratchBytes,
		&next.Replay.MaxScratchFiles,
	} {
		if *value > ^uint64(0)/2 {
			return ReleaseEvidenceV2Bounds{}, errors.New("relay source lifetime doubling overflows")
		}
		*value *= 2
	}
	if err := next.Validate(2); err != nil {
		return ReleaseEvidenceV2Bounds{}, err
	}
	return next, nil
}

// A retained config may be original or already rendered at the same approved
// capacity. Neither case may change another limit or multiply the approval.
func (self *ReleaseEvidenceV2SourceBounds) apply(configured ReleaseEvidenceV2Bounds) (ReleaseEvidenceV2Bounds, error) {
	if self == nil {
		return configured, nil
	}
	expected, err := DoubledReleaseEvidenceV2SourceBounds(self.Original)
	if err != nil || !reflect.DeepEqual(self.Approved, expected) || (!reflect.DeepEqual(configured, self.Original) && !reflect.DeepEqual(configured, self.Approved)) {
		return ReleaseEvidenceV2Bounds{}, errors.Join(errors.New("provisional source lifetime differs from its original and approved doubling"), err)
	}
	return self.Approved, nil
}
