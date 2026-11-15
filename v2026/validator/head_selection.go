package validator

// head_selection.go contains the pure top-slot boundary used before CRv4
// normalization. Ranking is exact-rational and deterministic: higher score
// first, then lower UID. Only positive live scores consume a head slot. The
// order and boundary are protocol/headscore's, shared with the operator.

import (
	"github.com/urfoundation/sn/v2026/protocol/headscore"
)

type HeadSelection struct {
	Selected []ExactWeightInput
	Rejected []ExactWeightInput
}

// selectHeadFleets applies the signed maximum before either the head channel
// or pool exclusion is built. Duplicate UIDs are rejected because they would
// make the selected fleet identity ambiguous at the exact boundary.
func selectHeadFleets(inputs []ExactWeightInput, maximum uint16) (HeadSelection, error) {
	scores := make([]headscore.UIDScore, len(inputs))
	for index, input := range inputs {
		scores[index] = headscore.UIDScore{UID: input.UID, Score: input.Score}
	}
	selection, err := headscore.Select(scores, maximum)
	if err != nil {
		return HeadSelection{}, err
	}
	return HeadSelection{Selected: headSelectionInputs(selection.Selected), Rejected: headSelectionInputs(selection.Rejected)}, nil
}

// headSelectionInputs carries the selection's own score copies.
func headSelectionInputs(scores []headscore.UIDScore) []ExactWeightInput {
	inputs := make([]ExactWeightInput, len(scores))
	for index, score := range scores {
		inputs[index] = ExactWeightInput{UID: score.UID, Score: score.Score}
	}
	return inputs
}

func headSelectionUIDs(inputs []ExactWeightInput) []uint16 {
	uids := make([]uint16, len(inputs))
	for i, input := range inputs {
		uids[i] = input.UID
	}
	return uids
}

// ValidateHeadSelectionEvidence independently reconstructs the exact ranked
// boundary from every positive eligible score. Canonical rational encoding and
// list order are part of the evidence so a validator cannot substitute an
// arbitrary selected set while preserving the submitted weight vector.
func ValidateHeadSelectionEvidence(eligibleUIDs []uint16, eligibleScores []RationalJSON, selectedUIDs, rejectedUIDs []uint16, maximum uint16) error {
	return headscore.ValidateSelection(eligibleUIDs, eligibleScores, selectedUIDs, rejectedUIDs, maximum)
}
