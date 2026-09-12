//go:build linux || darwin

package validator

import (
	"context"
	"errors"

	"github.com/urfoundation/sn/crv4"
)

// SourceReadBoundsV2 returns only the authenticated archive's original finite
// read bounds. It cannot create an archive owner or change source authority.
func (self *ReleaseEvidenceV2Archive) SourceReadBoundsV2(ctx context.Context) (ReleaseEvidenceV2Bounds, error) {
	if ctx == nil || self == nil || self.closed || self.owner == nil {
		return ReleaseEvidenceV2Bounds{}, errors.New("archive source read owner is absent")
	}
	if err := self.owner.check(ctx); err != nil {
		return ReleaseEvidenceV2Bounds{}, err
	}
	return self.owner.cfg.EvidenceV2.Bounds, nil
}

// ObserveSourcesWithNativeReadsV2 retains every actual native response through
// the same bounded adapter used by capture. The caller still supplies real EVM
// and native clients; this adds no observation or eligibility callback.
func (self *ReleaseEvidenceV2Archive) ObserveSourcesWithNativeReadsV2(ctx context.Context, chain *ChainClient, native *crv4.Chain, retain func(context.Context, ReleaseEvidenceV2NativeRead) error) ([]ReleaseEvidenceV2DecisionObservation, error) {
	bounds, err := self.SourceReadBoundsV2(ctx)
	if err != nil {
		return nil, err
	}
	owned, err := releaseNativeCaptureChainV2(ctx, native, bounds.MaxControlBytes, retain)
	if err != nil {
		return nil, err
	}
	return self.ObserveSources(ctx, chain, owned)
}
