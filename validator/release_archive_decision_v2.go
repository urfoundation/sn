//go:build linux || darwin

package validator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/urfoundation/sn/crv4"
)

// This is an observation to be authenticated against canonical historical
// chain state, not a verdict. Offline replay compares it with every signed
// decision; public verification must independently reproduce it using the
// concrete chain readers through ObserveSources below.
type ReleaseEvidenceV2DecisionObservation struct {
	MeasurementHash string                       `json:"measurement_hash"`
	Decision        ReleaseMeasurementV2Decision `json:"decision"`
	Bindings        []ReleaseBindingMeasurement  `json:"bindings"`
	Pools           []ReleasePoolMeasurement     `json:"pools"`
	DepositAudits   []DepositAudit               `json:"deposit_audits"`
}

type releaseEvidenceV2ArchiveMeasurement struct {
	encoded  []byte
	verified VerifiedReleaseMeasurementV2
}

// ObserveSources uses actual immutable EVM/native queries and the retained
// operator-signed local-key and HTTP observation files. Candidate pools and
// bindings never supply expected source observations. All source rows and both
// configured server-key origins must complete; no partial result is returned.
func (self *ReleaseEvidenceV2Archive) ObserveSources(ctx context.Context, chain *ChainClient, native *crv4.Chain) (result []ReleaseEvidenceV2DecisionObservation, resultErr error) {
	if self == nil || self.closed || chain == nil || native == nil {
		return nil, errors.New("archive historical source reader is absent")
	}
	if err := self.owner.check(ctx); err != nil {
		return nil, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			result = nil
		}
	}()
	owner, history := self.owner, self.history
	bounds := owner.cfg.EvidenceV2.Bounds
	runtime := releaseRuntimeIdentityV2(&owner.cfg)
	keys, err := readReleaseServerKeysV2(ctx, &owner.cfg)
	if err != nil {
		return nil, err
	}
	for noID, retained := range history.keys {
		for version, key := range retained {
			if !bytes.Equal(key, keys[noID][version]) {
				return nil, errors.New("archive server key differs from its independently configured public origin")
			}
		}
	}
	for _, input := range self.inputs {
		initial := input.Context
		_, err := chain.AuthenticateReleaseActivationV2Context(ctx, native, ReleaseActivationV2Authority{Expected: initial.Activation, Journal: initial.Journal, RuntimeHash: initial.RuntimeHash, ValidatorUID: initial.ValidatorUID, NativeRuntime: runtime}, input.Candidate, input.VPKSignature, input.HotkeySignature, initial.ObservedEVMBlock, initial.ObservedEVMHash)
		if err != nil {
			return nil, err
		}
		if err := chain.authenticateReleaseInitialBoundaryV2Context(ctx, initial, bounds.Cut.MaxHeaderBytes); err != nil {
			return nil, err
		}
	}
	for _, epoch := range slices.Sorted(maps.Keys(history.inputByEpoch)) {
		for _, noID := range slices.Sorted(maps.Keys(history.inputByEpoch[epoch])) {
			journal := history.inputByEpoch[epoch][noID]
			initial := history.initial[noID]
			if err := authenticateReleaseStartupNativeV2Context(ctx, native, initial, journal, runtime, false); err != nil {
				return nil, err
			}
			input := journal.MeasurementInput
			if err := chain.authenticateReleaseStartupBoundaryV2ContextWithRetainedHistory(ctx, initial.InitialCut.Activation.Domain, noID, AttemptBoundary{SettlementEpoch: input.SettlementEpoch, EVMBlock: input.CutEVMSnapshotBlock, EVMBlockHash: input.CutEVMSnapshotHash}, false, false); err != nil {
				return nil, err
			}
		}
	}
	for _, epoch := range slices.Sorted(maps.Keys(history.terminals)) {
		for _, transition := range history.terminals[epoch].Transitions {
			noID := transition.Identity.NoID
			if err := chain.authenticateReleaseStartupBoundaryV2ContextWithRetainedHistory(ctx, history.initial[noID].InitialCut.Activation.Domain, noID, transition.FromBoundary, true, false); err != nil {
				return nil, err
			}
		}
	}
	for _, item := range self.intents {
		artifact, err := decodeReleaseMeasurementV2Bytes(ctx, item.Measurement, bounds.MaxArtifactBytes, bounds.MaxOperators)
		if err != nil {
			return nil, err
		}
		custody := &releaseEvidenceV2StartupReferences{remaining: bounds.MaxHistoryBytes, archive: owner}
		sources, err := history.readIntentDecisionSourcesV2(ctx, chain, native, runtime, &item.Intent, artifact, custody)
		if err = errors.Join(err, custody.close()); err != nil {
			return nil, err
		}
		if err := authenticateReleaseNativeSourceReferenceV2(ctx, native, &owner.cfg, &item.Intent, artifact); err != nil {
			return nil, err
		}
		if item.Intent.Status == "applied" {
			if err := authenticateAdoptedIntentApplicationV2(ctx, native, &owner.cfg, &item.Intent); err != nil {
				return nil, err
			}
		}
		result = append(result, ReleaseEvidenceV2DecisionObservation{MeasurementHash: item.Intent.MeasurementArtifactHash, Decision: sources.decision, Bindings: sources.bindings, Pools: sources.pools, DepositAudits: sources.audits})
	}
	return result, owner.check(ctx)
}

// ReplayDecisions accepts separately captured source observations for the
// offline mathematical pass. Only ObserveSources and exact comparison with
// its result can establish historical chain authority. Full signed cut and
// terminal replay is mandatory here even if the source observations match.
func (self *ReleaseEvidenceV2Archive) ReplayDecisions(ctx context.Context, observations []ReleaseEvidenceV2DecisionObservation) (resultErr error) {
	if self == nil || self.closed || self.measurements != nil || len(observations) != len(self.intents) || len(observations) == 0 {
		return errors.New("archive decision replay owner or complete source census differs")
	}
	if err := self.owner.check(ctx); err != nil {
		return err
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			self.measurements = nil
		}
	}()
	// Own the complete finite observation wire before any stream callback.
	bounds := self.owner.cfg.EvidenceV2.Bounds
	var used uint64
	owned := make([]ReleaseEvidenceV2DecisionObservation, len(observations))
	for index, observation := range observations {
		raw, err := marshalAttemptSettlementV2JSON(ctx, observation, bounds.MaxControlBytes, false, false)
		if err != nil {
			return err
		}
		if uint64(len(raw)) > bounds.MaxHistoryBytes-used {
			return errors.New("archive decision observation census exceeds its historical byte bound")
		}
		used += uint64(len(raw))
		if err := json.Unmarshal(raw, &owned[index]); err != nil {
			return err
		}
	}
	measurements := make(map[string]releaseEvidenceV2ArchiveMeasurement, len(owned))
	for index, observation := range owned {
		item := &self.intents[index]
		if observation.MeasurementHash != item.Intent.MeasurementArtifactHash {
			return errors.New("archive observation order differs from the complete original intent history")
		}
		artifact, err := decodeReleaseMeasurementV2Bytes(ctx, item.Measurement, bounds.MaxArtifactBytes, bounds.MaxOperators)
		if err != nil {
			return err
		}
		options, err := self.decisionOptions(ctx, &item.Intent, artifact, observation)
		if err != nil {
			return err
		}
		envelope, err := DecodeReleaseMeasurementEnvelopeV2(ctx, item.Envelope, bounds.MaxControlBytes)
		if err != nil {
			return err
		}
		hotkey := self.inputs[0].Context.Activation.Hotkey
		_, verified, err := VerifyReleaseMeasurementEnvelopeV2(ctx, envelope, item.Measurement, hotkey, item.Intent.SelfUID, item.Intent.Prepared.ExtrinsicHash, options)
		if err != nil {
			return err
		}
		if err := VerifyReleaseMeasurementIntent(&item.Intent, artifact, verified.Decision); err != nil {
			return err
		}
		// The complete history owner already proved each ordinary input's exact
		// cursor, all interior folds, cumulative prefixes and lifetime trail IDs.
		// readIntents also binds every actual predecessor and HeadEMA transition.
		if measurements[observation.MeasurementHash].encoded != nil {
			return errors.New("archive decision repeats a measurement identity")
		}
		measurements[observation.MeasurementHash] = releaseEvidenceV2ArchiveMeasurement{encoded: bytes.Clone(item.Measurement), verified: cloneReleaseSubmissionResultV2(verified)}
	}
	if err := self.owner.check(ctx); err != nil {
		return err
	}
	self.measurements = measurements
	return nil
}

func (self *ReleaseEvidenceV2Archive) decisionOptions(ctx context.Context, intent *SteeringIntent, artifact *ReleaseMeasurementArtifact, observation ReleaseEvidenceV2DecisionObservation) (ReleaseMeasurementV2Options, error) {
	history, bounds := self.history, self.owner.cfg.EvidenceV2.Bounds
	contexts, inputs := history.inputContextsByEpoch[intent.SubnetEpoch], history.inputByEpoch[intent.SubnetEpoch]
	if len(contexts) != len(history.participants) || len(inputs) != len(contexts) || len(artifact.Inputs) != len(contexts) {
		return ReleaseMeasurementV2Options{}, errors.New("archive decision lacks a complete independently replayed native cut")
	}
	controlled := slices.Clone(history.cfg.ControlledNOIDs)
	slices.Sort(controlled)
	result := ReleaseMeasurementV2Options{Expected: observation.Decision, Policy: history.cfg.Policy, ControlledNOIDs: controlled, Bindings: observation.Bindings, Pools: observation.Pools, DepositAudits: observation.DepositAudits, Operators: map[uint64]ReleaseMeasurementV2OperatorOptions{}, MaxOperators: bounds.MaxOperators, MaxHeadEntries: bounds.MaxHeadEntries, MaxArtifactBytes: bounds.MaxArtifactBytes, MaxControlBytes: bounds.MaxControlBytes}
	for _, participant := range history.participants {
		expected, found := contexts[participant.NoID]
		input := inputs[participant.NoID]
		if !found || input == nil {
			return ReleaseMeasurementV2Options{}, errors.New("archive decision input context is absent")
		}
		cursor := releaseEvidenceV2StartupCursor{epoch: expected.Boundary.SettlementEpoch, first: expected.FirstSequence, egressFirst: expected.EgressFirstSequence, generation: expected.EgressGeneration, priorRoot: expected.PriorRoot}
		operator, err := history.operator(ctx, participant.NoID, cursor, expected.Boundary, 0, "decision")
		if err != nil {
			return ReleaseMeasurementV2Options{}, err
		}
		result.Operators[participant.NoID] = ReleaseMeasurementV2OperatorOptions{Expected: operator.Expected, CutNativeBlock: input.MeasurementInput.CutNativeBlock, CutNativeBlockHash: input.MeasurementInput.CutNativeBlockHash, Bounds: operator.Bounds, Measurement: operator.Measurement}
	}
	if closure := artifact.SettlementClosureV2; closure != nil {
		contexts := history.terminalContexts[closure.Epoch]
		if len(contexts) != len(history.participants) {
			return ReleaseMeasurementV2Options{}, errors.New("archive decision terminal authority is incomplete")
		}
		cursors := make(map[uint64]releaseEvidenceV2StartupCursor, len(contexts))
		for noID, expected := range contexts {
			cursors[noID] = releaseEvidenceV2StartupCursor{epoch: expected.Boundary.SettlementEpoch, first: expected.FirstSequence, egressFirst: expected.EgressFirstSequence, generation: expected.EgressGeneration, priorRoot: expected.PriorRoot}
		}
		authority, err := history.authority(ctx, cursors, closure.Transitions[0].FromBoundary, 0, "decision-terminal")
		if err != nil {
			return ReleaseMeasurementV2Options{}, err
		}
		result.Settlement = &authority
	} else if artifact.SettlementEpoch > 0 && history.terminals[artifact.SettlementEpoch-1] != nil {
		return ReleaseMeasurementV2Options{}, errors.New("archive decision omits its complete terminal predecessor")
	}
	projection := *artifact
	projection.Bindings, projection.Pools, projection.DepositAudits = observation.Bindings, observation.Pools, observation.DepositAudits
	if err := installReleaseBindingKeysV2(ctx, &projection, &result); err != nil {
		return ReleaseMeasurementV2Options{}, err
	}
	return result, self.owner.check(ctx)
}

// Measurement returns owned value copies only after the complete mathematical
// decision census succeeds. This does not turn an observation into chain proof.
func (self *ReleaseEvidenceV2Archive) Measurement(hash string) (*ReleaseMeasurementArtifact, *VerifiedReleaseMeasurement, error) {
	if self == nil || self.closed || self.measurements == nil {
		return nil, nil, errors.New("archive decision replay has not completed")
	}
	value, found := self.measurements[hash]
	if !found {
		return nil, nil, fmt.Errorf("archive decision %s is absent from its original history", hash)
	}
	artifact, err := decodeReleaseMeasurementV2Bytes(self.owner.ctx, value.encoded, self.owner.cfg.EvidenceV2.Bounds.MaxArtifactBytes, self.owner.cfg.EvidenceV2.Bounds.MaxOperators)
	if err != nil {
		return nil, nil, err
	}
	return artifact, cloneReleaseSubmissionResultV2(value.verified).Decision, self.owner.check(self.owner.ctx)
}
