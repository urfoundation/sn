package headscore

import (
	"cmp"
	"errors"
	"fmt"
	"math/big"
	"slices"
)

// UIDScore is one UID's exact head score.
type UIDScore struct {
	UID   uint16
	Score *big.Rat
}

// Selection is the ranked head: the first seats, then every other eligible
// UID, each in rank order.
type Selection struct {
	Selected []UIDScore
	Rejected []UIDScore
}

// Ranked is every eligible UID in rank order: Selected, then Rejected.
func (self Selection) Ranked() []UIDScore {
	return append(append([]UIDScore(nil), self.Selected...), self.Rejected...)
}

var (
	errZeroMaximum  = errors.New("maximum head fleets is zero")
	errInvalidScore = errors.New("head score is nil or negative")
	errDuplicateUID = errors.New("duplicate head UID")
)

// Compare is the head's total rank order: the higher score first, then the
// lower UID. It returns a negative number when a ranks before b. UIDs are
// unique among eligible scores, so no two of them compare equal.
func Compare(a, b UIDScore) int {
	if comparison := a.Score.Cmp(b.Score); comparison != 0 {
		return -comparison
	}
	return cmp.Compare(a.UID, b.UID)
}

// Select ranks every positive score by Compare and gives the first maximum
// of them a head seat. A zero score takes no seat and is left out. Scores are
// copied; a nil or negative score, a duplicate UID or a zero maximum is
// refused, since each would make the boundary ambiguous.
func Select(inputs []UIDScore, maximum uint16) (Selection, error) {
	if maximum == 0 {
		return Selection{}, errZeroMaximum
	}
	seen := map[uint16]bool{}
	ranked := make([]UIDScore, 0, len(inputs))
	for _, input := range inputs {
		if input.Score == nil || input.Score.Sign() < 0 {
			return Selection{}, errInvalidScore
		}
		if seen[input.UID] {
			return Selection{}, errDuplicateUID
		}
		seen[input.UID] = true
		if input.Score.Sign() > 0 {
			ranked = append(ranked, UIDScore{UID: input.UID, Score: new(big.Rat).Set(input.Score)})
		}
	}
	slices.SortFunc(ranked, Compare)
	selected := min(len(ranked), int(maximum))
	return Selection{
		Selected: append([]UIDScore(nil), ranked[:selected]...),
		Rejected: append([]UIDScore(nil), ranked[selected:]...),
	}, nil
}

// Rank is every positive score in rank order, with no seat limit.
func Rank(scores map[uint16]*big.Rat) ([]UIDScore, error) {
	inputs := make([]UIDScore, 0, len(scores))
	for uid, score := range scores {
		inputs = append(inputs, UIDScore{UID: uid, Score: score})
	}
	selection, err := Select(inputs, ^uint16(0))
	if err != nil {
		return nil, err
	}
	return selection.Ranked(), nil
}

// UIDs lists the UIDs of scores in their order.
func UIDs(scores []UIDScore) []uint16 {
	uids := make([]uint16, len(scores))
	for index, score := range scores {
		uids[index] = score.UID
	}
	return uids
}

// ValidateSelection independently reconstructs the ranked boundary from a
// published head result: every eligible UID with its exact score, in rank
// order, and the selected and rejected UIDs at the given maximum. Scores must
// be positive and canonically encoded, and the list order is part of the
// evidence, so no selection can be substituted under unchanged scores.
func ValidateSelection(eligibleUIDs []uint16, eligibleScores []Rational, selectedUIDs, rejectedUIDs []uint16, maximum uint16) error {
	if maximum == 0 || len(eligibleUIDs) != len(eligibleScores) {
		return errors.New("head selection evidence is incomplete")
	}
	inputs := make([]UIDScore, len(eligibleUIDs))
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
		inputs[index] = UIDScore{UID: eligibleUIDs[index], Score: score}
	}
	selection, err := Select(inputs, maximum)
	if err != nil {
		return err
	}
	if !slices.Equal(eligibleUIDs, UIDs(selection.Ranked())) {
		return errors.New("eligible head evidence is not in deterministic score order")
	}
	if !slices.Equal(selectedUIDs, UIDs(selection.Selected)) || !slices.Equal(rejectedUIDs, UIDs(selection.Rejected)) {
		return errors.New("selected and rejected head UIDs do not match the reconstructed score boundary")
	}
	return nil
}
