//go:build linux || darwin

package validator

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
)

// A missed native submission does not create an intermediate intent. Only a
// terminal real intent may be followed across a forward gap in this invocation.
func validateSteeringIntentSuccessorWithGapsV2(previous, current *SteeringIntent, allowGaps bool) error {
	if allowGaps && previous != nil && current != nil && current.SubnetEpoch > previous.SubnetEpoch {
		if current.SettlementEpoch < previous.SettlementEpoch {
			return errors.New("provisional intent settlement regresses")
		}
		if previous.Status != "applied" && previous.Status != "failed" {
			return errors.New("next-epoch successor follows an unfinished intent")
		}
		return nil
	}
	return validateSteeringIntentSuccessor(previous, current)
}

// Both artifacts have already passed readMeasurementV2, including the actual
// signed envelope, independent immutable input journal, scores and native source.
// This private join returns no public verified token or final-acceptance claim.
func (self *IntentStore) verifyMeasurementLineageV2(ctx context.Context, previousEncoded []byte, previousOptions ReleaseMeasurementV2Options, current *ReleaseMeasurementArtifact, currentOptions ReleaseMeasurementV2Options) error {
	if self.v2.provisionalEpochGaps || self.v2.historyAdoption != nil {
		previous, err := decodeReleaseMeasurementV2Bytes(ctx, previousEncoded, previousOptions.MaxArtifactBytes, previousOptions.MaxOperators)
		if err != nil {
			return err
		}
		if current != nil && (current.SubnetEpoch > previous.SubnetEpoch && current.SubnetEpoch-previous.SubnetEpoch > 1 || current.SettlementEpoch > previous.SettlementEpoch && current.SettlementEpoch-previous.SettlementEpoch > 1) {
			if self.v2.provisionalEpochGaps {
				return self.v2.runtime.verifyProvisionalMeasurementGapV2(ctx, previousEncoded, previous, previousOptions, current, currentOptions)
			}
			if !self.v2.historyAdoption.allowsArtifactEdge(ReleaseMeasurementContentHash(previousEncoded), previous, current) || self.v2.runtime.history == nil || self.v2.runtime.history.retainedStartup || self.v2.runtime.history.historyAdoption != self.v2.historyAdoption {
				return errors.New("strict measurement gap differs from its authenticated adoption edge")
			}
			return self.v2.runtime.verifyMeasurementGapHistoryV2(ctx, previousEncoded, previous, previousOptions, current, currentOptions, false)
		}
	}
	_, err := VerifyReleaseMeasurementLineageV2(ctx, previousEncoded, previousOptions, current, currentOptions)
	return err
}

// Reuse the real startup/live terminal census instead of inventing intervening
// measurements or replaying its remote history. Every terminal still has its
// existing signature, independent context, exact pool fold and local ledger root.
func (self *releaseRuntimeV2) verifyProvisionalMeasurementGapV2(ctx context.Context, previousEncoded []byte, previous *ReleaseMeasurementArtifact, previousOptions ReleaseMeasurementV2Options, current *ReleaseMeasurementArtifact, currentOptions ReleaseMeasurementV2Options) (resultErr error) {
	if self == nil || self.history == nil || !self.history.retainedStartup || !provisionalClosedNativeInputEnabled(&self.cfg) {
		return errors.New("provisional measurement gap lacks validated retained startup authority")
	}
	return self.verifyMeasurementGapHistoryV2(ctx, previousEncoded, previous, previousOptions, current, currentOptions, true)
}

// Strict adoption uses the ordinary full terminal decoder at every interior
// settlement. Only the separate existing provisional caller may reuse its
// local startup comparisons. Both endpoints already passed full intent replay.
func (self *releaseRuntimeV2) verifyMeasurementGapHistoryV2(ctx context.Context, previousEncoded []byte, previous *ReleaseMeasurementArtifact, previousOptions ReleaseMeasurementV2Options, current *ReleaseMeasurementArtifact, currentOptions ReleaseMeasurementV2Options, retained bool) (resultErr error) {
	if self == nil || self.history == nil {
		return errors.New("measurement gap has no independently replayed startup history")
	}
	release, err := self.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()
	defer func() { resultErr = errors.Join(resultErr, ctx.Err()) }()
	previous, _, err = ownReleaseMeasurementV2(ctx, previous, previousOptions)
	if err != nil {
		return err
	}
	current, _, err = ownReleaseMeasurementV2(ctx, current, currentOptions)
	if err != nil {
		return err
	}
	if current.PreviousArtifactHash != ReleaseMeasurementContentHash(previousEncoded) || current.DeploymentID != previous.DeploymentID || current.ChainID != previous.ChainID || current.GenesisHash != previous.GenesisHash || current.Coordinator != previous.Coordinator || current.SettlementVault != previous.SettlementVault || current.ValidatorID != previous.ValidatorID || current.Netuid != previous.Netuid || current.SelfUID != previous.SelfUID || current.PolicyHash != previous.PolicyHash {
		return errors.New("provisional measurement lineage identity or predecessor hash differs")
	}
	if current.SubnetEpoch < previous.SubnetEpoch || current.SettlementEpoch < previous.SettlementEpoch || !releaseBlockAtOrBefore(previous.NativeSnapshotBlock, previous.NativeSnapshotHash, current.NativeSnapshotBlock, current.NativeSnapshotHash) || !releaseBlockAtOrBefore(previous.EVMSnapshotBlock, previous.EVMSnapshotHash, current.EVMSnapshotBlock, current.EVMSnapshotHash) {
		return errors.New("provisional measurement lineage regresses its epoch or boundary")
	}
	if len(current.Inputs) != len(previous.Inputs) || len(current.Inputs) != len(self.history.participants) {
		return errors.New("provisional measurement lineage changes operator coverage")
	}
	if err := verifyReleaseMeasurementHeadLineage(previous, current); err != nil {
		return err
	}
	for index, input := range current.Inputs {
		prior := previous.Inputs[index]
		if input.NoID != prior.NoID || input.NoID != self.history.participants[index].NoID || !releaseBlockAtOrBefore(prior.CutNativeBlock, prior.CutNativeBlockHash, input.CutNativeBlock, input.CutNativeBlockHash) {
			return errors.New("provisional measurement operator or native cut regresses")
		}
		for _, cut := range []*AttemptCutV2{prior.AttemptCutV2, input.AttemptCutV2} {
			if cut == nil {
				return errors.New("provisional measurement omits its actual cut")
			}
			if err := self.history.matchLedgerCut(ctx, input.NoID, cut.LastSequence, cut.Root); err != nil {
				return err
			}
		}
	}
	if current.SettlementEpoch == previous.SettlementEpoch {
		for index, input := range current.Inputs {
			prior := previous.Inputs[index]
			if err := verifyProvisionalMeasurementPrefixV2(prior.Stats, prior.AttemptCutV2, input.Stats, input.AttemptCutV2); err != nil {
				return err
			}
		}
		return nil
	}
	if current.SettlementClosureV2 == nil || current.SettlementEpoch-previous.SettlementEpoch > uint64(len(self.history.terminals)) {
		return errors.New("provisional measurement gap lacks its actual terminal history")
	}
	var priorTerminal *AttemptSettlementClosureV2
	for epoch := previous.SettlementEpoch; epoch < current.SettlementEpoch; epoch++ {
		known := self.history.terminals[epoch]
		if known == nil || known.Epoch != epoch {
			return fmt.Errorf("provisional measurement gap lacks actual terminal %d", epoch)
		}
		authority, err := self.authority(ctx, self.history.terminalContexts[epoch], "intent-gap-terminal")
		if err != nil {
			return err
		}
		var terminal *AttemptSettlementClosureV2
		if retained {
			terminal, err = admitProvisionalRetainedSettlementV2(ctx, known, authority)
		} else {
			var raw []byte
			raw, err = marshalAttemptSettlementV2JSON(ctx, known, authority.MaxClosureBytes, false, true)
			if err == nil {
				terminal, _, err = DecodeAttemptSettlementClosureV2(ctx, raw, authority)
			}
		}
		if err != nil {
			return err
		}
		if len(terminal.Transitions) != len(previous.Inputs) {
			return errors.New("provisional terminal changes operator coverage")
		}
		for index, transition := range terminal.Transitions {
			if transition.Identity.NoID != previous.Inputs[index].NoID {
				return errors.New("provisional terminal changes operator order")
			}
			if priorTerminal == nil {
				prior := previous.Inputs[index]
				if err := verifyProvisionalMeasurementPrefixV2(prior.Stats, prior.AttemptCutV2, transition.PreFold, &transition.Cut); err != nil {
					return err
				}
			} else if err := verifyAttemptSettlementV2Successor(priorTerminal.Transitions[index], transition.PreFold, transition.Cut, false); err != nil {
				return err
			}
			if err := self.history.matchLedgerCut(ctx, transition.Identity.NoID, transition.Cut.LastSequence, transition.Cut.Root); err != nil {
				return err
			}
		}
		priorTerminal = terminal
	}
	limit := self.cfg.EvidenceV2.Bounds.MaxClosureBytes
	want, err := marshalAttemptSettlementV2JSON(ctx, priorTerminal, limit, false, true)
	if err != nil {
		return err
	}
	actual, err := marshalAttemptSettlementV2JSON(ctx, current.SettlementClosureV2, limit, false, true)
	if err != nil || !bytes.Equal(actual, want) {
		return errors.Join(errors.New("provisional measurement changes its actual last terminal"), err)
	}
	for index, input := range current.Inputs {
		if err := verifyAttemptSettlementV2Successor(priorTerminal.Transitions[index], input.Stats, *input.AttemptCutV2, false); err != nil {
			return err
		}
	}
	return nil
}

func verifyProvisionalMeasurementPrefixV2(priorStats ReleaseStatsMeasurement, oldCut *AttemptCutV2, nextStats ReleaseStatsMeasurement, newCut *AttemptCutV2) error {
	if oldCut == nil || newCut == nil || !maps.Equal(releasePriorQualityState(priorStats), releasePriorQualityState(nextStats)) || oldCut.Context.Identity != newCut.Context.Identity || oldCut.Context.Activation != newCut.Context.Activation || oldCut.Context.Boundary.SettlementEpoch != newCut.Context.Boundary.SettlementEpoch || oldCut.Context.FirstSequence != newCut.Context.FirstSequence || oldCut.Context.PriorRoot != newCut.Context.PriorRoot || oldCut.LastSequence > newCut.LastSequence || !releaseBlockAtOrBefore(oldCut.Context.Boundary.EVMBlock, oldCut.Context.Boundary.EVMBlockHash, newCut.Context.Boundary.EVMBlock, newCut.Context.Boundary.EVMBlockHash) || oldCut.Context.EgressGeneration > newCut.Context.EgressGeneration || oldCut.Context.EgressFirstSequence > newCut.Context.EgressFirstSequence || oldCut.Context.EgressGeneration == newCut.Context.EgressGeneration && oldCut.Context.EgressFirstSequence != newCut.Context.EgressFirstSequence {
		return errors.New("provisional measurement changes or regresses its actual cumulative prefix or pool EMA")
	}
	return nil
}
