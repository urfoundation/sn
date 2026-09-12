//go:build linux || darwin

package validator

import (
	"bytes"
	"context"
	"errors"

	"github.com/urfoundation/sn/payoutartifact"
)

// A vector active at campaign start can consume an earlier payout artifact.
// Return its original signed HTTP body from the closed replay owner. No live
// file, newly fetched artifact or reconstructed unsigned document is admitted.
func (self *ReleaseEvidenceV2Archive) PayoutSourceV2(ctx context.Context, measurementHash string, noID uint64) ([]byte, error) {
	artifact, _, err := self.Measurement(measurementHash)
	if err != nil {
		return nil, err
	}
	return self.payoutSourceForMeasurementV2(ctx, artifact, noID)
}

func (self *ReleaseEvidenceV2Archive) payoutSourceForMeasurementV2(ctx context.Context, artifact *ReleaseMeasurementArtifact, noID uint64) ([]byte, error) {
	if self == nil || self.owner == nil || artifact == nil || len(self.inputs) == 0 {
		return nil, errors.New("archive original payout owner is incomplete")
	}
	if err := self.owner.check(ctx); err != nil {
		return nil, err
	}
	var audit *DepositAudit
	for index := range artifact.DepositAudits {
		if artifact.DepositAudits[index].NoID == noID {
			audit = &artifact.DepositAudits[index]
		}
	}
	if audit == nil || audit.HttpObservationHash == "" {
		return nil, errors.New("archive payout has no original signed HTTP observation")
	}
	var origin string
	for _, operator := range self.owner.cfg.Operators {
		if operator.NoID == noID {
			origin = operator.APIURL
		}
	}
	reader, err := NewHTTPArtifactReader(origin, self.owner.cfg.DeploymentID, self.owner.cfg.Netuid)
	if err != nil {
		return nil, err
	}
	expected, path, err := releaseArtifactHttpRequestV2(&self.owner.cfg, self.inputs[0].Context.Activation.Hotkey, artifact, noID, audit.SourceEpoch, reader)
	if err != nil {
		return nil, err
	}
	maximum := min(self.owner.cfg.EvidenceV2.Bounds.MaxArtifactBytes, self.owner.cfg.EvidenceV2.Bounds.MaxControlBytes/8)
	raw, err := self.owner.readPrivate(ctx, path, maximum, false)
	if err != nil || ReleaseMeasurementContentHash(raw) != audit.HttpObservationHash {
		return nil, errors.Join(errors.New("archive payout HTTP source hash differs from signed decision"), err)
	}
	observation, err := decodeArtifactHttpObservationV2(ctx, raw, maximum, expected)
	if err != nil {
		return nil, err
	}
	payout, observedErr, err := replayArtifactHttpObservationV2(ctx, reader, observation)
	if err != nil || observedErr != nil || payout == nil || payout.ContentHash != audit.ArtifactHash {
		return nil, errors.Join(errors.New("archive payout source does not reproduce the original successful audit"), observedErr, err)
	}
	body := observation.Exchanges[len(observation.Exchanges)-1].Body
	decoded, err := payoutartifact.Decode(body)
	if err != nil || decoded.ContentHash != payout.ContentHash || decoded.NoID != noID || decoded.Epoch != audit.SourceEpoch {
		return nil, errors.Join(errors.New("archive payout body differs from its original decoded source"), err)
	}
	return bytes.Clone(body), self.owner.check(ctx)
}
