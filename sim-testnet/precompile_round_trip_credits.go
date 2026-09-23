// Probe receipts authenticate each call's local conservation. Native stake can
// grow between those receipts, so preparation records those credits separately
// while final acceptance continues to require recovery of every probe position.
package main

import (
	"errors"
	"fmt"
)

// These are observed credits between authenticated receipts, not a claim about
// their cause. In particular, they are not the explicit dividend-window proof.
type PrecompileRoundTripCredits struct {
	SeedToForwardSampleRao uint64 `json:"seed_to_forward_sample_rao"`
	ForwardToBackSampleRao uint64 `json:"forward_to_back_sample_rao"`
	ForwardToBackMoveRao   uint64 `json:"forward_to_back_move_rao"`
	UnrecoveredMoveRao     uint64 `json:"unrecovered_move_rao"`
}

// Only nondecreasing inter-block observations are admissible. Within either
// transaction the approved amount and existing one-unit conversion bounds hold.
func derivePrecompileRoundTripCredits(evidence *PrecompileConformanceEvidence) (PrecompileRoundTripCredits, error) {
	if evidence == nil || evidence.Forward.AmountRao != evidence.Seed.DeltaRao/2 || !precompileMoveBackValid(evidence) ||
		evidence.Seed.BlockNumber > evidence.Forward.BlockNumber || evidence.Forward.BlockNumber > evidence.Back.BlockNumber ||
		evidence.Forward.ToBeforeRao != 0 || evidence.Forward.FromBeforeRao < evidence.Seed.AfterRao ||
		evidence.Back.FromBeforeRao < evidence.Forward.ToAfterRao || evidence.Back.ToBeforeRao < evidence.Forward.FromAfterRao ||
		evidence.Back.NativeShareResidueRao != 0 {
		return PrecompileRoundTripCredits{}, errors.New("round-trip receipts do not account for principal and native share conversion")
	}
	credits := PrecompileRoundTripCredits{
		SeedToForwardSampleRao: evidence.Forward.FromBeforeRao - evidence.Seed.AfterRao,
		ForwardToBackSampleRao: evidence.Back.ToBeforeRao - evidence.Forward.FromAfterRao,
		ForwardToBackMoveRao:   evidence.Back.FromBeforeRao - evidence.Forward.ToAfterRao,
		UnrecoveredMoveRao:     evidence.Back.FromAfterRao,
	}
	if (credits.SeedToForwardSampleRao > 0 && evidence.Seed.BlockNumber == evidence.Forward.BlockNumber) ||
		((credits.ForwardToBackSampleRao > 0 || credits.ForwardToBackMoveRao > 0) && evidence.Forward.BlockNumber == evidence.Back.BlockNumber) ||
		credits.UnrecoveredMoveRao != credits.ForwardToBackMoveRao {
		return PrecompileRoundTripCredits{}, errors.New("round-trip credits exceed their receipt boundaries or leave principal unrecovered")
	}
	creditRounding := evidence.Forward.NativeShareCreditRoundingRao + evidence.Back.NativeShareCreditRoundingRao
	if evidence.Back.ToAfterRao < credits.ForwardToBackSampleRao || evidence.Forward.FromBeforeRao < creditRounding ||
		evidence.Back.ToAfterRao-credits.ForwardToBackSampleRao != evidence.Forward.FromBeforeRao-creditRounding {
		return PrecompileRoundTripCredits{}, errors.New("round-trip sample balance does not reconcile to principal, credits and conversion")
	}
	return credits, nil
}

// Existing zero-credit evidence keeps its original canonical encoding. A saved
// credit record is immutable and must match the exact retained receipt values.
func recordPrecompileRoundTripCredits(evidence *PrecompileConformanceEvidence) error {
	credits, err := derivePrecompileRoundTripCredits(evidence)
	if err != nil {
		return err
	}
	if evidence.RoundTripCredits != nil {
		if *evidence.RoundTripCredits != credits {
			return errors.New("round-trip credit record changed from its authenticated receipts")
		}
	} else if credits != (PrecompileRoundTripCredits{}) {
		evidence.RoundTripCredits = &credits
	}
	return nil
}

// Preparation may proceed with explicitly accounted earnings still in the move
// position. This predicate does not satisfy the final zero-custody gate.
func precompileRoundTripAccounted(evidence *PrecompileConformanceEvidence) bool {
	credits, err := derivePrecompileRoundTripCredits(evidence)
	if err != nil {
		return false
	}
	if evidence.RoundTripCredits == nil {
		return credits == (PrecompileRoundTripCredits{})
	}
	return *evidence.RoundTripCredits == credits
}

// Snapshot calldata contains only the hotkey. Its baseline is a receipt output,
// so credits after the pre-send read are retained rather than treated as drift.
func reconcilePrecompileSnapshotEvent(step PrecompileSnapshotStep, values map[string]any, blockNumber uint64) (PrecompileSnapshotStep, error) {
	baseline, baselineOk := conformanceEventUint64(values, "baseline")
	since, sinceOk := conformanceEventUint64(values, "blockNumber")
	if !baselineOk || !sinceOk || baseline == 0 || baseline < step.BaselineRao || blockNumber == 0 || since != blockNumber {
		return PrecompileSnapshotStep{}, fmt.Errorf("invalid DividendSnapshot event baseline=%d since=%d receipt_block=%d", baseline, since, blockNumber)
	}
	step.BaselineRao, step.SinceBlock = baseline, since
	return step, nil
}
