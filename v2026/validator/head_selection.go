package validator

// head_selection.go contains the pure top-slot boundary used before CRv4
// normalization. Ranking is exact-rational and deterministic: higher score
// first, then lower UID. Only positive live scores consume a head slot.

import (
	"errors"
	"math/big"
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
