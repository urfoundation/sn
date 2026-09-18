package main

import "testing"

// RootMissed is the explicit terminal result of finalizeOperatorEpoch when no
// root was committed. It carries value within that operator instead of making
// a claimable entitlement; it must not keep an otherwise complete run pending.
func TestAcceptedEpochsTerminalIncludesExplicitRootMissed(t *testing.T) {
	window := &ScenarioAcceptanceWindow{FirstEpoch: 7, EpochCount: 2}
	contracts := &ContractView{Epochs: []EpochView{
		{Epoch: 7, Operators: []EpochOperatorView{{NoID: 1, Status: 2}, {NoID: 2, Status: 3, FundedRao: "9", TotalRao: "9", ClaimedRao: "0"}}},
		{Epoch: 8, Operators: []EpochOperatorView{{NoID: 1, Status: 3, FundedRao: "0", TotalRao: "0", ClaimedRao: "0"}, {NoID: 2, Status: 2}}},
	}}
	if passed, detail := acceptedEpochsTerminal(contracts, window, 2); !passed {
		t.Fatalf("explicit finalized and missed-root positions rejected: %s", detail)
	}
	contracts.Epochs[0].Operators[1].NoID = 1
	if passed, _ := acceptedEpochsTerminal(contracts, window, 2); passed {
		t.Fatal("duplicate identity became acceptable with a missed root")
	}
}

// Missing, still-funded, expired, and unknown states cannot substitute for an
// accepted interval's explicit Finalized or RootMissed positions.
func TestAcceptedEpochsTerminalRejectsUnfinishedAndExpiredPositions(t *testing.T) {
	window := &ScenarioAcceptanceWindow{FirstEpoch: 12, EpochCount: 1}
	contracts := &ContractView{Epochs: []EpochView{{Epoch: 12, Operators: []EpochOperatorView{{NoID: 1, Status: 2}, {NoID: 2}}}}}
	for _, status := range []uint8{0, 1, 4, 5, 255} {
		contracts.Epochs[0].Operators[1].Status = status
		if passed, _ := acceptedEpochsTerminal(contracts, window, 2); passed {
			t.Fatalf("nonterminal or expired status %d was accepted", status)
		}
	}
	contracts.Epochs[0].Operators = contracts.Epochs[0].Operators[:1]
	if passed, _ := acceptedEpochsTerminal(contracts, window, 2); passed {
		t.Fatal("missing operator position was accepted")
	}
	window.EpochCount = 0
	if passed, _ := acceptedEpochsTerminal(contracts, window, 2); passed {
		t.Fatal("empty acceptance interval was accepted")
	}
}
