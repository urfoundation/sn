//go:build linux || darwin

package validator

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"maps"
	"slices"
)

// Replays retain their scratch directories. Each complete verification gets
// new private child names; only path custody changes, never source authority.
func (self *releaseRuntimeV2) measurementReplayOptionsV2(ctx context.Context, options ReleaseMeasurementV2Options, purpose string) (result ReleaseMeasurementV2Options, resultErr error) {
	if ctx == nil || self == nil || options.MaxOperators == 0 || len(options.Operators) == 0 || uint64(len(options.Operators)) > options.MaxOperators || !validAttemptPrivateLeaf(purpose) {
		return result, errors.New("V2 replay namespace owner or finite operator census is absent")
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			result = ReleaseMeasurementV2Options{}
		}
	}()
	result = options
	result.Operators = maps.Clone(options.Operators)
	for noId, operator := range result.Operators {
		if operator.Expected.Identity.NoID != noId {
			return result, errors.New("V2 replay operator namespace differs from its independent context")
		}
		path, err := self.scratch(ctx, noId, false, purpose+"-ordinary")
		if err != nil {
			return result, err
		}
		operator.Measurement.Replay.ScratchDirectory = path
		result.Operators[noId] = operator
	}
	if options.Settlement != nil {
		if len(options.Settlement.Operators) != len(options.Operators) {
			return result, errors.New("V2 replay terminal operator census differs")
		}
		settlement := *options.Settlement
		settlement.Operators = maps.Clone(options.Settlement.Operators)
		result.Settlement = &settlement
		for noId, operator := range settlement.Operators {
			if _, found := result.Operators[noId]; !found || operator.Expected.Identity.NoID != noId {
				return result, errors.New("V2 terminal replay namespace differs from its independent context")
			}
			path, err := self.scratch(ctx, noId, false, purpose+"-terminal")
			if err != nil {
				return result, err
			}
			operator.Measurement.Replay.ScratchDirectory = path
			settlement.Operators[noId] = operator
		}
	}
	return result, nil
}

// Intent replay receives independently retained ordinary/terminal contexts
// from semantic startup or the actual live cut owner. Every binding, key and
// payout observation is reconstructed from native/EVM/retained HTTP sources;
// candidate maps are not promoted into verifier authority.
func (self *releaseRuntimeV2) measurementOptionsForIntent(ctx context.Context, intent *SteeringIntent, artifact *ReleaseMeasurementArtifact) (result ReleaseMeasurementV2Options, resultErr error) {
	release, err := self.acquire(ctx)
	if err != nil {
		return result, err
	}
	defer release()
	if self.history == nil || artifact == nil || intent == nil || artifact.Schema != ReleaseMeasurementSchemaV2 {
		return result, errors.New("V2 intent replay has no independently reconstructed history")
	}
	bounds := self.cfg.EvidenceV2.Bounds
	contexts := self.history.inputContextsByEpoch[intent.SubnetEpoch]
	inputs := self.history.inputByEpoch[intent.SubnetEpoch]
	if len(contexts) != len(self.history.participants) || len(inputs) != len(contexts) || len(artifact.Inputs) != len(contexts) {
		return result, errors.New("V2 intent independent native-cut census is incomplete")
	}
	custody := &releaseEvidenceV2StartupReferences{remaining: bounds.MaxHistoryBytes}
	defer func() {
		resultErr = errors.Join(resultErr, custody.close(), ctx.Err())
		if resultErr != nil {
			result = ReleaseMeasurementV2Options{}
		}
	}()
	sources, err := self.history.readIntentDecisionSourcesV2(ctx, self.chain, self.native, releaseRuntimeIdentityV2(&self.cfg), intent, artifact, custody)
	if err != nil {
		return result, err
	}
	controlled := slices.Clone(self.cfg.ControlledNOIDs)
	slices.Sort(controlled)
	result = ReleaseMeasurementV2Options{Expected: sources.decision, Policy: self.cfg.Policy, ControlledNOIDs: controlled, Bindings: sources.bindings, Pools: sources.pools, DepositAudits: sources.audits, Operators: make(map[uint64]ReleaseMeasurementV2OperatorOptions, len(contexts)), MaxOperators: bounds.MaxOperators, MaxHeadEntries: bounds.MaxHeadEntries, MaxArtifactBytes: bounds.MaxArtifactBytes, MaxControlBytes: bounds.MaxControlBytes}
	for index, participant := range self.history.participants {
		input := inputs[participant.NoID]
		expected, found := contexts[participant.NoID]
		if input == nil || !found || artifact.Inputs[index].NoID != participant.NoID {
			return result, errors.New("V2 intent input order or independent context differs")
		}
		want, err := marshalAttemptSettlementV2JSON(ctx, input.MeasurementInput, bounds.MaxInputJournalBytes, false, false)
		if err != nil {
			return result, err
		}
		actual, err := marshalAttemptSettlementV2JSON(ctx, artifact.Inputs[index], bounds.MaxInputJournalBytes, false, false)
		if err != nil || !bytes.Equal(want, actual) {
			return result, errors.Join(errors.New("V2 intent cut bytes differ from the actual immutable native journal"), err)
		}
		operator, err := self.operator(ctx, expected, "intent-ordinary")
		if err != nil {
			return result, err
		}
		// Retained keys remain value-owned after releasing the runtime gate.
		keys := make(map[byte]ed25519.PublicKey, len(operator.Measurement.Replay.ServerKeys))
		for id, key := range operator.Measurement.Replay.ServerKeys {
			keys[id] = bytes.Clone(key)
		}
		operator.Measurement.Replay.ServerKeys = keys
		result.Operators[participant.NoID] = ReleaseMeasurementV2OperatorOptions{Expected: operator.Expected, CutNativeBlock: input.MeasurementInput.CutNativeBlock, CutNativeBlockHash: input.MeasurementInput.CutNativeBlockHash, Bounds: operator.Bounds, Measurement: operator.Measurement}
	}
	if closure := artifact.SettlementClosureV2; closure != nil {
		known := self.history.terminals[closure.Epoch]
		if known == nil || closure.Epoch == ^uint64(0) || closure.Epoch+1 != artifact.SettlementEpoch {
			return result, errors.New("V2 intent closure has no actual terminal predecessor")
		}
		want, err := marshalAttemptSettlementV2JSON(ctx, known, bounds.MaxClosureBytes, false, true)
		if err != nil {
			return result, err
		}
		actual, err := marshalAttemptSettlementV2JSON(ctx, closure, bounds.MaxClosureBytes, false, true)
		if err != nil || !bytes.Equal(want, actual) {
			return result, errors.Join(errors.New("V2 intent changes the actual terminal closure"), err)
		}
		authority, err := self.authority(ctx, self.history.terminalContexts[closure.Epoch], "intent-terminal")
		if err != nil {
			return result, err
		}
		result.Settlement = &authority
	} else if artifact.SettlementEpoch > 0 && self.history.terminals[artifact.SettlementEpoch-1] != nil {
		return result, errors.New("V2 intent omits its complete terminal predecessor")
	}
	// This copy selects only already independent source fields for the common
	// eligibility projection. Statistics inputs were matched to retained bytes.
	projection := *artifact
	projection.Bindings, projection.Pools, projection.DepositAudits = sources.bindings, sources.pools, sources.audits
	if err := installReleaseBindingKeysV2(ctx, &projection, &result); err != nil {
		return result, err
	}
	return result, custody.check()
}
