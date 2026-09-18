//go:build linux || darwin

package main

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// The live relay writes an own receipt for a normal successful send. Final
// capture must accept that state without confusing it with a publication race.
func TestFinalRelayRetainedReceiptStateAcceptsEveryTransactionOutcome(t *testing.T) {
	winner := &types.Receipt{Status: types.ReceiptStatusSuccessful, TxHash: common.Hash{0x11}, BlockHash: common.Hash{0x22}, GasUsed: 30}
	ownSuccess := &types.Receipt{Status: types.ReceiptStatusSuccessful, TxHash: common.Hash{0x11}, BlockHash: common.Hash{0x22}, GasUsed: 30}
	failedRace := &types.Receipt{Status: types.ReceiptStatusFailed, TxHash: common.Hash{0x33}, BlockHash: common.Hash{0x44}, GasUsed: 40}
	valid := []struct {
		own  *types.Receipt
		lost bool
	}{
		{own: nil, lost: false},
		{own: ownSuccess, lost: false},
		{own: failedRace, lost: true},
	}
	for index, value := range valid {
		if err := validateFinalRelayRetainedReceiptState(winner, value.own, value.lost); err != nil {
			t.Fatalf("valid relay state %d rejected: %v", index, err)
		}
	}
}

// Each race claim retains a distinct failed transaction; each own success is
// the exact winning receipt. Crossed flags or altered receipts are incomplete.
func TestFinalRelayRetainedReceiptStateRejectsCrossedOutcomes(t *testing.T) {
	winner := &types.Receipt{Status: types.ReceiptStatusSuccessful, TxHash: common.Hash{0x11}, BlockHash: common.Hash{0x22}, GasUsed: 30}
	invalid := []struct {
		winner *types.Receipt
		own    *types.Receipt
		lost   bool
	}{
		{winner: nil},
		{winner: &types.Receipt{Status: types.ReceiptStatusFailed, TxHash: common.Hash{0x11}}},
		{winner: winner, lost: true},
		{winner: winner, own: &types.Receipt{Status: types.ReceiptStatusSuccessful, TxHash: common.Hash{0x33}}, lost: true},
		{winner: winner, own: &types.Receipt{Status: types.ReceiptStatusFailed, TxHash: common.Hash{0x11}}, lost: true},
		{winner: winner, own: &types.Receipt{Status: types.ReceiptStatusFailed, TxHash: common.Hash{0x33}}},
		{winner: winner, own: &types.Receipt{Status: types.ReceiptStatusSuccessful, TxHash: common.Hash{0x11}, BlockHash: common.Hash{0x99}, GasUsed: 30}},
		{winner: winner, own: &types.Receipt{Status: types.ReceiptStatusSuccessful, TxHash: common.Hash{0x33}, BlockHash: common.Hash{0x22}, GasUsed: 30}},
	}
	for index, value := range invalid {
		if err := validateFinalRelayRetainedReceiptState(value.winner, value.own, value.lost); err == nil {
			t.Fatalf("invalid relay state %d was accepted", index)
		}
	}
}

// A continuation spends the same aggregate reserve at its newly approved fee
// ceiling. Final capture must use that plan ceiling even while the immutable
// launch configuration keeps its smaller source-storage partition.
func TestFinalRelayCaptureMaximumSlotsUsesApprovedContinuationReserve(t *testing.T) {
	fixture, _, continuation := evidenceRelayContinuationTest(t)
	baseMaximum, err := finalRelayCaptureMaximumSlots(fixture.plan)
	if err != nil || baseMaximum != evidenceRelayOriginalSlots {
		t.Fatalf("base plan slot ceiling: maximum=%d error=%v", baseMaximum, err)
	}
	continued, err := appendEvidenceRelayContinuationPlan(fixture.plan, continuation)
	if err != nil {
		t.Fatal(err)
	}
	continuedMaximum, err := finalRelayCaptureMaximumSlots(continued)
	if err != nil || continuedMaximum != evidenceRelayContinuationSlots || continuation.NewSlots >= continuedMaximum || fixture.cfg.Config.ValidatorEvidenceRelay.MaxSlots != evidenceRelayOriginalSlots {
		t.Fatalf("continued plan slot ceiling: maximum=%d new=%d config=%d error=%v", continuedMaximum, continuation.NewSlots, fixture.cfg.Config.ValidatorEvidenceRelay.MaxSlots, err)
	}
}

// A continuation does not rewrite an already admitted request. Final capture
// must retain that request's predecessor plan as its transaction owner.
func TestFinalRelayCaptureRequestPreservesHistoricalContinuationOwner(t *testing.T) {
	fixture, executor, continuation := evidenceRelayContinuationTest(t)
	continued, err := appendEvidenceRelayContinuationPlan(fixture.plan, continuation)
	if err != nil {
		t.Fatal(err)
	}
	if len(continuation.Debits) != 1 {
		t.Fatalf("fixture has %d historical debits", len(continuation.Debits))
	}
	debit := continuation.Debits[0]
	owners := map[string]*SetupPlan{fixture.plan.PlanHash: fixture.plan, continued.PlanHash: continued}
	owner, request, raw, err := readFinalRelayCaptureRequest(t.Context(), fixture.stateDir, continued, executor.journal.Entries(), debit.ActionID, owners)
	if err != nil || owner.PlanHash != fixture.plan.PlanHash || request.PlanHash != fixture.plan.PlanHash || request.Action.ID != debit.ActionID || len(raw) == 0 {
		t.Fatalf("historical relay owner changed: owner=%v request=%+v bytes=%d error=%v", owner, request, len(raw), err)
	}
}
