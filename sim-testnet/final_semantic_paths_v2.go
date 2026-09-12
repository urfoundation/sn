//go:build linux || darwin

package main

import (
	"errors"
	"fmt"

	validatorpkg "github.com/urfoundation/sn/validator"
)

type finalValidatorPathEpochV2 struct {
	Epoch       uint64                                `json:"epoch"`
	Boundary    ChainHead                             `json:"boundary"`
	ProofCount  uint64                                `json:"proof_count"`
	ProofStream validatorpkg.AttemptStreamV2Reference `json:"proof_stream"`
	Closure     FinalArtifactLocator                  `json:"closure"`
}

type finalValidatorPathSummaryV2 struct {
	Schema      string                      `json:"schema"`
	ValidatorID uint64                      `json:"validator_id"`
	NoID        uint64                      `json:"no_id"`
	TrailDepth  int                         `json:"trail_depth"`
	Epochs      []finalValidatorPathEpochV2 `json:"epochs"`
}

func finalValidatorPathSummaryFromReplayV2(evidence *FinalSemanticEvidence, owner *finalValidatorReplayOwnerV2, noID uint64) (finalValidatorPathSummaryV2, error) {
	if evidence == nil || owner == nil || owner.archive == nil || evidence.Window.EpochCount == 0 {
		return finalValidatorPathSummaryV2{}, errors.New("final V2 path owner is absent")
	}
	result := finalValidatorPathSummaryV2{Schema: "urnetwork-final-validator-path-proofs-v2", ValidatorID: owner.manifest.ValidatorID, NoID: noID, TrailDepth: owner.config.TrailDepth}
	for offset := uint64(0); offset < evidence.Window.EpochCount; offset++ {
		epoch, ok := checkedAdd(evidence.Window.FirstEpoch, offset)
		if !ok {
			return finalValidatorPathSummaryV2{}, errors.New("final V2 path epochs overflow")
		}
		closure, err := owner.archive.TerminalClosure(epoch)
		if err != nil {
			return finalValidatorPathSummaryV2{}, err
		}
		var transition *validatorpkg.AttemptSettlementTransitionV2
		for _, item := range closure.Transitions {
			if item.Identity.NoID == noID {
				if transition != nil {
					return finalValidatorPathSummaryV2{}, errors.New("final V2 terminal repeats operator")
				}
				transition = item
			}
		}
		if transition == nil || transition.Identity.ValidatorID != result.ValidatorID || transition.Cut.Proofs.ItemCount == 0 {
			return finalValidatorPathSummaryV2{}, errors.New("final V2 accepted settlement has no actual complete path proofs for its operator")
		}
		var locator FinalArtifactLocator
		boundary := ChainHead{Number: transition.FromBoundary.EVMBlock, Hash: transition.FromBoundary.EVMBlockHash}
		for _, item := range owner.manifest.Capture.Closures {
			if item.Epoch == epoch {
				if locator.URI != "" || item.Boundary != boundary {
					return finalValidatorPathSummaryV2{}, errors.New("final V2 captured terminal differs from signed closure")
				}
				locator = item.Artifact
			}
		}
		if locator.URI == "" {
			return finalValidatorPathSummaryV2{}, errors.New("final V2 accepted closure is omitted from its complete source census")
		}
		span, ok := checkedMul(offset+1, evidence.Window.EpochBlocks)
		if !ok {
			return finalValidatorPathSummaryV2{}, errors.New("final V2 terminal span overflows")
		}
		end, ok := checkedAdd(evidence.Window.StartBlock, span)
		if !ok || end == 0 || boundary.Number != end-1 {
			return finalValidatorPathSummaryV2{}, errors.New("final V2 terminal is not its complete settlement boundary")
		}
		result.Epochs = append(result.Epochs, finalValidatorPathEpochV2{Epoch: epoch, Boundary: boundary, ProofCount: transition.Cut.Proofs.ItemCount, ProofStream: transition.Cut.Proofs, Closure: locator})
	}
	return result, nil
}

func (a *finalSemanticArchive) buildValidatorPathProofsV2(evidence *FinalSemanticEvidence) error {
	for _, entry := range evidence.ValidatorReplayV2 {
		owner := a.validatorReplayV2[entry.ValidatorID]
		for noID := uint64(1); noID <= uint64(evidence.ExpectedOperators); noID++ {
			summary, err := finalValidatorPathSummaryFromReplayV2(evidence, owner, noID)
			if err != nil {
				return err
			}
			locator, err := a.derived("validator-path-proofs-v2", fmt.Sprintf("validator-%d-no-%d-paths-v2.json", entry.ValidatorID, noID), summary)
			if err != nil {
				return err
			}
			proof := FinalValidatorPathProofEvidence{ValidatorID: entry.ValidatorID, NoID: noID, FirstEpoch: evidence.Window.FirstEpoch, LastEpoch: evidence.Window.FirstEpoch + evidence.Window.EpochCount - 1, TrailDepth: summary.TrailDepth, ProofsHash: locator.ContentHash, Artifact: locator}
			for _, epoch := range summary.Epochs {
				var ok bool
				proof.ProofCount, ok = checkedAdd(proof.ProofCount, epoch.ProofCount)
				if !ok {
					return errors.New("final V2 complete proof count overflows")
				}
				proof.SettlementClosures = append(proof.SettlementClosures, FinalCollectedSettlementClosure{Epoch: epoch.Epoch, Boundary: epoch.Boundary, Artifact: epoch.Closure})
			}
			evidence.PathProofs = append(evidence.PathProofs, proof)
		}
	}
	return nil
}

func verifyFinalValidatorPathProofV2(evidence *FinalSemanticEvidence, proof *FinalValidatorPathProofEvidence, raw []byte, owner *finalValidatorReplayOwnerV2) error {
	if proof == nil || owner == nil || proof.Artifact.Kind != "validator-path-proofs-v2" {
		return errors.New("final V2 path proof has no full replay owner")
	}
	var declared finalValidatorPathSummaryV2
	if err := decodeStrictJSONBytes(raw, &declared); err != nil {
		return err
	}
	expected, err := finalValidatorPathSummaryFromReplayV2(evidence, owner, proof.NoID)
	if err != nil {
		return err
	}
	if !finalJSONEqual(declared, expected) || proof.ValidatorID != expected.ValidatorID || proof.TrailDepth != expected.TrailDepth || len(proof.SettlementClosures) != len(expected.Epochs) {
		return errors.New("final V2 path summary differs from fully replayed signed terminal streams")
	}
	var count uint64
	for index, epoch := range expected.Epochs {
		var ok bool
		count, ok = checkedAdd(count, epoch.ProofCount)
		if !ok {
			return errors.New("final V2 path count overflows")
		}
		if !finalJSONEqual(proof.SettlementClosures[index], FinalCollectedSettlementClosure{Epoch: epoch.Epoch, Boundary: epoch.Boundary, Artifact: epoch.Closure}) {
			return errors.New("final V2 path closure locator differs")
		}
	}
	if proof.ProofCount != count {
		return errors.New("final V2 path count differs from authenticated stream records")
	}
	return nil
}

func verifyFinalValidatorClosureLocatorsV2(closures []FinalCollectedSettlementClosure, window ScenarioAcceptanceWindow) error {
	if uint64(len(closures)) != window.EpochCount || window.EpochCount == 0 {
		return errors.New("final V2 closure census is incomplete")
	}
	for index, closure := range closures {
		epoch, ok := checkedAdd(window.FirstEpoch, uint64(index))
		if !ok || closure.Epoch != epoch {
			return errors.New("final V2 closure epochs are incomplete")
		}
		span, ok := checkedMul(uint64(index)+1, window.EpochBlocks)
		if !ok {
			return errors.New("final V2 closure span overflows")
		}
		end, ok := checkedAdd(window.StartBlock, span)
		if !ok || end == 0 || closure.Boundary.Number != end-1 {
			return errors.New("final V2 closure is not at its complete settlement boundary")
		}
		if err := verifyFinalHead("final V2 closure", closure.Boundary); err != nil {
			return err
		}
		if err := verifyFinalArtifact("final V2 closure", closure.Artifact, "validator-evidence-v2-source"); err != nil {
			return err
		}
	}
	return nil
}
