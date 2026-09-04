package validator

// head_selection.go contains the pure top-slot boundary used before CRv4
// normalization. Ranking is exact-rational and deterministic: higher score
// first, then lower UID. Only positive live scores consume a head slot.

import (
	"errors"
	"fmt"
	"math/big"
	"slices"
	"sort"
)

type HeadSelection struct {
	Selected []ExactWeightInput
	Rejected []ExactWeightInput
}

// selectHeadFleets applies the signed maximum before either the head channel
// or pool exclusion is built. Duplicate UIDs are rejected because they would
// make the selected fleet identity ambiguous at the exact boundary.
func selectHeadFleets(inputs []ExactWeightInput, maximum uint16) (HeadSelection, error) {
	if maximum == 0 {
		return HeadSelection{}, errors.New("maximum head fleets is zero")
	}
	seen := map[uint16]bool{}
	ranked := make([]ExactWeightInput, 0, len(inputs))
	for _, input := range inputs {
		if input.Score == nil || input.Score.Sign() < 0 {
			return HeadSelection{}, errors.New("head score is nil or negative")
		}
		if seen[input.UID] {
			return HeadSelection{}, errors.New("duplicate head UID")
		}
		seen[input.UID] = true
		if input.Score.Sign() > 0 {
			ranked = append(ranked, ExactWeightInput{UID: input.UID, Score: new(big.Rat).Set(input.Score)})
		}
	}
	sort.Slice(ranked, func(i, j int) bool {
		if comparison := ranked[i].Score.Cmp(ranked[j].Score); comparison != 0 {
			return comparison > 0
		}
		return ranked[i].UID < ranked[j].UID
	})
	selectedCount := len(ranked)
	if selectedCount > int(maximum) {
		selectedCount = int(maximum)
	}
	return HeadSelection{
		Selected: append([]ExactWeightInput(nil), ranked[:selectedCount]...),
		Rejected: append([]ExactWeightInput(nil), ranked[selectedCount:]...),
	}, nil
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
	if maximum == 0 || len(eligibleUIDs) != len(eligibleScores) {
		return errors.New("head selection evidence is incomplete")
	}
	inputs := make([]ExactWeightInput, len(eligibleUIDs))
	for index, encoded := range eligibleScores {
		numerator, numeratorOK := new(big.Int).SetString(encoded.Numerator, 10)
		denominator, denominatorOK := new(big.Int).SetString(encoded.Denominator, 10)
		if !numeratorOK || !denominatorOK || numerator.Sign() <= 0 || denominator.Sign() <= 0 {
			return fmt.Errorf("eligible head score %d is not a positive rational", index)
		}
		score := new(big.Rat).SetFrac(numerator, denominator)
		if encoded.Numerator != score.Num().String() || encoded.Denominator != score.Denom().String() {
			return fmt.Errorf("eligible head score %d is not canonically encoded", index)
		}
		inputs[index] = ExactWeightInput{UID: eligibleUIDs[index], Score: score}
	}
	selection, err := selectHeadFleets(inputs, maximum)
	if err != nil {
		return err
	}
	ranked := append(headSelectionUIDs(selection.Selected), headSelectionUIDs(selection.Rejected)...)
	if !slices.Equal(eligibleUIDs, ranked) {
		return errors.New("eligible head evidence is not in deterministic score order")
	}
	if !slices.Equal(selectedUIDs, headSelectionUIDs(selection.Selected)) || !slices.Equal(rejectedUIDs, headSelectionUIDs(selection.Rejected)) {
		return errors.New("selected and rejected head UIDs do not match the reconstructed score boundary")
	}
	return nil
}
