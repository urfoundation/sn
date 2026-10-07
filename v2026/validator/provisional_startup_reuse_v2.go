//go:build linux || darwin

package validator

// Explicit provisional continuation retains completed local results. These
// helpers authenticate available local bytes; they do not produce remote
// replay/chain observations, new consents or transferable verified tokens.

import (
	"context"
	"crypto/sha256"
	"errors"
	"slices"
)

func admitProvisionalRetainedStatsV2(ctx context.Context, measurement ReleaseStatsMeasurement, cut AttemptCutV2, operator AttemptSettlementV2OperatorOptions) error {
	if err := cut.VerifyHeader(operator.Expected, operator.Bounds); err != nil {
		return err
	}
	_, err := newAttemptCutV2StatsProjection(ctx, measurement, operator.Expected, operator.Policy, AttemptCutV2StatsOptions{
		ExpectedConfig: operator.Measurement.ExpectedConfig, MaxProviders: operator.Measurement.MaxProviders,
		MaxEgressHashes: operator.Measurement.MaxEgressHashes, Replay: operator.Measurement.Replay,
	}, nil)
	return err
}

func admitProvisionalRetainedSettlementV2(ctx context.Context, closure *AttemptSettlementClosureV2, authority AttemptSettlementV2Options) (*AttemptSettlementClosureV2, error) {
	operation, err := admitAttemptSettlementV2(ctx, closure, authority, true)
	if err != nil {
		return nil, err
	}
	for _, transition := range operation.closure.Transitions {
		operator := operation.options.Operators[transition.Identity.NoID]
		projection, err := newAttemptCutV2StatsProjection(ctx, transition.PreFold, operator.Expected, operator.Policy, AttemptCutV2StatsOptions{
			ExpectedConfig: operator.Measurement.ExpectedConfig, MaxProviders: operator.Measurement.MaxProviders,
			MaxEgressHashes: operator.Measurement.MaxEgressHashes, Replay: operator.Measurement.Replay,
		}, nil)
		if err != nil {
			return nil, err
		}
		if !slices.Equal(attemptSettlementV2Qualities(projection.verified), transition.PostFold) {
			return nil, errors.New("retained compact settlement post-fold differs from its signed statistics")
		}
	}
	return operation.closure, ctx.Err()
}

// Strict recovery retains complete remote replay. Only the private startup
// authority derived from a validated provisional handoff selects local reuse.
func decodeAttemptSettlementV2RecoveryClosure(ctx context.Context, raw []byte, authority AttemptSettlementV2Options) (*AttemptSettlementClosureV2, error) {
	if !authority.retainedStartup {
		closure, _, err := DecodeAttemptSettlementClosureV2(ctx, raw, authority)
		return closure, err
	}
	closure, err := decodeAttemptSettlementClosureV2Bytes(ctx, raw, authority.MaxClosureBytes, authority.MaxParticipants)
	if err != nil {
		return nil, err
	}
	return admitProvisionalRetainedSettlementV2(ctx, closure, authority)
}

// A previously completed local publication is left intact. Reconstruct its
// exact unsigned census from admitted signed closure bytes and keep the
// original consent locators; no old stream fetch, chain view or signing occurs.
// Missing locators continue through the ordinary real publisher.
func (self *releaseRuntimeV2) resumeProvisionalRetainedPublication(ctx context.Context, epoch uint64, closure *AttemptSettlementClosureV2, hooks releaseMeasurementInputV2ReadHooks) (bool, error) {
	if !self.history.retainedStartup || epoch >= self.retainedStartupEpoch {
		return false, nil
	}
	bounds := self.cfg.EvidenceV2.Bounds
	path, err := ValidatorEvidencePublicationV2ManifestPath(self.cfg.StateDir, epoch)
	if err != nil {
		return false, err
	}
	manifest, err := readValidatorEvidencePublicationV2Manifest(ctx, path, bounds.MaxClosureBytes, bounds.MaxParticipants, hooks)
	if releaseMeasurementInputV2InitialAbsence(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if closure == nil || closure.Epoch != epoch || len(closure.Transitions) != len(self.history.participants) || len(closure.Transitions) == 0 || closure.Transitions[0] == nil || manifest.Origins != self.origins || len(manifest.Members) != len(closure.Transitions) {
		return false, errors.New("retained publication differs from its configured complete closure")
	}
	census := ValidatorEvidenceCensusV2{Schema: ValidatorEvidenceCensusV2Schema, Hotkey: self.hotkey.PublicKey(), Boundary: closure.Transitions[0].FromBoundary, Members: make([]ValidatorEvidenceCensusV2Member, len(closure.Transitions))}
	for index, transition := range closure.Transitions {
		noID := self.history.participants[index].NoID
		context, found := self.publicationContexts[epoch][noID]
		if transition == nil || transition.Identity.NoID != noID || manifest.Members[index].NoId != noID || !found || transition.Cut.Context != context || self.sources[noID] == nil {
			return false, errors.New("retained publication member differs from its original local owner")
		}
		vpk, err := canonicalAttemptHex32("retained publication validator", transition.Identity.ValidatorVPK, false)
		if err != nil {
			return false, err
		}
		payload, err := marshalAttemptSettlementV2JSON(ctx, transition, bounds.MaxTransitionBytes, false, true)
		if err != nil {
			return false, err
		}
		census.Members[index] = ValidatorEvidenceCensusV2Member{Domain: context.Activation.Domain, NoID: noID, VPK: vpk,
			PayloadHash: sha256.Sum256(payload), PayloadBytes: uint64(len(payload)), RecordCount: transition.Cut.RecordCount,
			CompleteCount: transition.Cut.CompleteCount, FailedCount: transition.Cut.FailedCount}
	}
	encoded, err := marshalAttemptSettlementV2JSON(ctx, census, bounds.MaxClosureBytes, false, true)
	if err != nil {
		return false, err
	}
	if uint64(len(encoded)) != manifest.CensusBytes || sha256.Sum256(encoded) != manifest.CensusHash {
		return false, errors.New("retained publication census differs from its original signed closure")
	}
	return true, ctx.Err()
}
