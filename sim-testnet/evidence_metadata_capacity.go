// Metadata capacity is an explicit hashed configuration grant. Legacy
// profiles retain their original limits; larger profiles own a finite margin.
package main

import "errors"

const maximumCampaignMetadataDocumentCapacity = 64 * 1024 * 1024 * 1024

// A complete grant belongs to the hashed launch configuration; it cannot be
// supplied by an artifact claiming to need more space.
type campaignMetadataCapacityConfig struct {
	MaximumDocumentBytes   uint64 `yaml:"maximum_document_bytes" json:"maximum_document_bytes"`
	MaximumRetainedBytes   uint64 `yaml:"maximum_retained_bytes" json:"maximum_retained_bytes"`
	MaximumSupplementBytes uint64 `yaml:"maximum_supplement_bytes" json:"maximum_supplement_bytes"`
}

// These implementation ceilings protect integer/allocation conversions. The
// smaller configured grant remains the actual authority for every document.
func (self *campaignMetadataCapacityConfig) validate() error {
	if self == nil {
		return nil
	}
	if self.MaximumDocumentBytes == 0 || self.MaximumDocumentBytes > maximumCampaignMetadataDocumentCapacity ||
		self.MaximumRetainedBytes == 0 || self.MaximumRetainedBytes > 256*1024*1024*1024 ||
		self.MaximumSupplementBytes == 0 || self.MaximumSupplementBytes > 128*1024*1024*1024 {
		return errors.New("evidence archive metadata requires complete finite document, retained and supplement limits")
	}
	return nil
}

// Absence does not silently enlarge previously approved archive authority.
func (self *campaignMetadataCapacityConfig) limits() campaignMetadataCapacityConfig {
	if self != nil {
		return *self
	}
	return campaignMetadataCapacityConfig{MaximumDocumentBytes: maximumCampaignMetadataDocumentV2,
		MaximumRetainedBytes: maximumCampaignRetainedMetadataV2, MaximumSupplementBytes: maximumCampaignSupplementMetadataV2}
}
