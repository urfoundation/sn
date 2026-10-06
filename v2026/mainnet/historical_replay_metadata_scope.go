package main

import "errors"

// A metadata pin authenticates the original layout. Only explicit fee event
// callsites request the separate, still-unapproved fee candidate report.
func historicalProfileFeeEvents(profile *historicalReplayObservationProfile) bool {
	if profile == nil || profile.MetadataSha256 == nil {
		return false
	}
	for _, rule := range profile.Rules {
		if rule.Purpose == "fee-withdraw" || rule.Purpose == "fee-refund" || rule.Purpose == "ethereum-executed" {
			return true
		}
	}
	return false
}

// Pinning native storage metadata does not select whole-fee accounting. Fee
// authority still requires its pin, and unapproved fee paths remain refused.
func validateNativeExecutionMetadataScope(profile *historicalReplayObservationProfile, fees *nativeFeeCensusPolicy) error {
	if profile == nil || fees != nil && profile.MetadataSha256 == nil {
		return errors.New("native fee authority lacks original generated metadata")
	}
	if fees == nil {
		for _, rule := range profile.Rules {
			if nativeFeeCensusPurpose(rule.Purpose) {
				return errors.New("native execution cannot borrow unapproved complete fee paths")
			}
		}
	}
	return nil
}
