package validator

import (
	"math/big"
	"slices"
	"testing"

	"github.com/urnetwork/connect"
)

func TestSelectHeadFleetsKeepsExactTopTwoHundred(t *testing.T) {
	inputs := make([]ExactWeightInput, 205)
	for i := range inputs {
		inputs[i] = ExactWeightInput{UID: uint16(500 - i), Score: new(big.Rat).SetInt64(int64(i + 1))}
	}
	selection, err := selectHeadFleets(inputs, 200)
	if err != nil {
		t.Fatal(err)
	}
	if len(selection.Selected) != 200 || len(selection.Rejected) != 5 {
		t.Fatalf("selected/rejected = %d/%d, want 200/5", len(selection.Selected), len(selection.Rejected))
	}
	if selection.Selected[0].UID != 296 || selection.Selected[199].UID != 495 || selection.Rejected[0].UID != 496 {
		t.Fatalf("unexpected exact boundary: selected first/last=%d/%d rejected first=%d", selection.Selected[0].UID, selection.Selected[199].UID, selection.Rejected[0].UID)
	}
}

// Complete score evidence must reproduce the same 200/2 boundary; changing a
// claimed selection without changing the validator's scores is detectable.
func TestValidateHeadSelectionEvidenceReconstructsTopTwoHundred(t *testing.T) {
	eligible := make([]uint16, 202)
	scores := make([]RationalJSON, 202)
	for index := range eligible {
		eligible[index] = uint16(20 + index)
		scores[index] = RationalJSON{Numerator: new(big.Int).SetInt64(int64(202 - index)).String(), Denominator: "1"}
	}
	selected := append([]uint16(nil), eligible[:200]...)
	rejected := append([]uint16(nil), eligible[200:]...)
	if err := ValidateHeadSelectionEvidence(eligible, scores, selected, rejected, 200); err != nil {
		t.Fatalf("canonical 200/2 evidence: %v", err)
	}
	selected[199], rejected[0] = rejected[0], selected[199]
	if err := ValidateHeadSelectionEvidence(eligible, scores, selected, rejected, 200); err == nil {
		t.Fatal("a substituted slot claimant was accepted against unchanged scores")
	}
	selected[199], rejected[0] = rejected[0], selected[199]
	scores[0].Numerator = "0202"
	if err := ValidateHeadSelectionEvidence(eligible, scores, selected, rejected, 200); err == nil {
		t.Fatal("non-canonical score evidence was accepted")
	}
}

func TestSelectHeadFleetsBreaksTiesByUIDAndRejectsZero(t *testing.T) {
	selection, err := selectHeadFleets([]ExactWeightInput{
		{UID: 9, Score: big.NewRat(3, 2)},
		{UID: 4, Score: big.NewRat(3, 2)},
		{UID: 7, Score: new(big.Rat)},
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(headSelectionUIDs(selection.Selected), []uint16{4}) || !slices.Equal(headSelectionUIDs(selection.Rejected), []uint16{9}) {
		t.Fatalf("tie boundary selected=%v rejected=%v", headSelectionUIDs(selection.Selected), headSelectionUIDs(selection.Rejected))
	}
}

func TestSelectHeadFleetsRejectsAmbiguousInputs(t *testing.T) {
	invalidInputs := [][]ExactWeightInput{
		{{UID: 1, Score: nil}},
		{{UID: 1, Score: big.NewRat(-1, 1)}},
		{{UID: 1, Score: big.NewRat(1, 1)}, {UID: 1, Score: big.NewRat(2, 1)}},
	}
	for _, inputs := range invalidInputs {
		if _, err := selectHeadFleets(inputs, 200); err == nil {
			t.Errorf("invalid head inputs were accepted: %+v", inputs)
		}
	}
	if _, err := selectHeadFleets([]ExactWeightInput{{UID: 1, Score: big.NewRat(1, 1)}}, 0); err == nil {
		t.Fatal("zero maximum was accepted")
	}
}

func TestLiveHeadMembersLeavePoolWhetherSelectedOrRejected(t *testing.T) {
	selectedClient := connect.Id{1}
	rejectedClient := connect.Id{2}
	bound := map[uint64]map[connect.Id]bool{1: {}, 2: {}}
	controlled := excludeLiveHeadMembers(bound, map[uint64]bool{2: true}, map[uint16][]releaseHeadMember{
		10: {{NoID: 1, ClientID: selectedClient}},
		11: {{NoID: 2, ClientID: rejectedClient}},
	})
	if !bound[1][selectedClient] || !bound[2][rejectedClient] {
		t.Fatalf("live fleet membership was not excluded from both pools: %+v", bound)
	}
	if controlled[10] || !controlled[11] {
		t.Fatalf("controlled head classification=%v, want only UID 11", controlled)
	}

	// Deregistration is represented by absence from the live member set. That
	// is the only transition which returns a client to pool accounting.
	bound = map[uint64]map[connect.Id]bool{1: {}, 2: {}}
	excludeLiveHeadMembers(bound, map[uint64]bool{2: true}, map[uint16][]releaseHeadMember{
		10: {{NoID: 1, ClientID: selectedClient}},
	})
	if bound[2][rejectedClient] {
		t.Fatal("deregistered fleet member remained excluded from its pool")
	}
}
