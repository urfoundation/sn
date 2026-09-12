//go:build linux || darwin

package validator

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/urfoundation/sn/crv4"
)

// Capture independently reads complete native/EVM decision sources using the
// original immutable journal's bounded provider census. The journal is still
// an input here: offline full cut replay must authenticate every claimed
// statistic before these observations can contribute to an accepted decision.
func captureReleaseDecisionObservationsV2(ctx context.Context, cfg *ReleaseConfig, chain *ChainClient, native *crv4.Chain, initials map[uint64]ReleaseEvidenceV2ActivationContext, files *releaseEvidenceV2HistoryFiles, intents []ReleaseEvidenceV2CapturedIntent, emit func(ReleaseEvidenceV2CaptureSource, []byte) error) error {
	if cfg == nil || chain == nil || native == nil || files == nil || emit == nil {
		return errors.New("decision source capture owner is absent")
	}
	owned := *cfg
	owned.Coordinator, owned.SettlementVault = strings.ToLower(cfg.Coordinator), strings.ToLower(cfg.SettlementVault)
	history := &releaseEvidenceV2StartupHistory{cfg: owned, initial: initials, inputByEpoch: map[uint64]map[uint64]*releaseMeasurementInputJournal{}}
	for _, operator := range cfg.EvidenceV2.Operators {
		history.participants = append(history.participants, AttemptSettlementRuntimeV2Participant{NoID: operator.NoID})
	}
	for _, file := range files.inputs {
		if file.legacy {
			return errors.New("V2 decision capture cannot substitute legacy input authority")
		}
		journal, err := history.decodeInput(ctx, file)
		if err != nil {
			return err
		}
		if history.inputByEpoch[journal.SubnetEpoch] == nil {
			history.inputByEpoch[journal.SubnetEpoch] = map[uint64]*releaseMeasurementInputJournal{}
		}
		if history.inputByEpoch[journal.SubnetEpoch][file.noID] != nil {
			return errors.New("decision source capture repeats a native operator journal")
		}
		history.inputByEpoch[journal.SubnetEpoch][file.noID] = journal
	}
	bounds := cfg.EvidenceV2.Bounds
	for _, item := range intents {
		artifact, err := decodeReleaseMeasurementV2Bytes(ctx, item.Measurement, bounds.MaxArtifactBytes, bounds.MaxOperators)
		if err != nil {
			return err
		}
		custody := &releaseEvidenceV2StartupReferences{remaining: bounds.MaxHistoryBytes}
		sources, err := history.readIntentDecisionSourcesV2(ctx, chain, native, releaseRuntimeIdentityV2(cfg), &item.Intent, artifact, custody)
		if err := errors.Join(err, custody.close()); err != nil {
			return err
		}
		observation := ReleaseEvidenceV2DecisionObservation{MeasurementHash: item.Intent.MeasurementArtifactHash, Decision: sources.decision, Bindings: sources.bindings, Pools: sources.pools, DepositAudits: sources.audits}
		raw, err := marshalAttemptSettlementV2JSON(ctx, observation, bounds.MaxControlBytes, false, true)
		if err != nil {
			return err
		}
		if !json.Valid(raw) {
			return errors.New("decision source capture produced invalid JSON")
		}
		if err := emit(ReleaseEvidenceV2CaptureSource{Kind: "decision-observation", Name: item.Intent.MeasurementArtifactHash}, raw); err != nil {
			return err
		}
	}
	return ctx.Err()
}
