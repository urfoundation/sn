package main

// These tests pin direct native payout causality independently of broad
// before/after reward snapshots.

import (
	"strings"
	"testing"
)

// Builds the smallest complete boundary used by the payout verifier tests.
func finalNativePayoutTestEvidence() *FinalSemanticEvidence {
	reveal := ChainHead{Number: 200, Hash: finalTestHex(0x91)}
	cycle := FinalCRv4Cycle{SettlementEpoch: 10, SubnetEpoch: 50, Reveal: FinalNativeReceipt{Block: reveal}, Application: FinalNativeReceipt{Block: ChainHead{Number: 203, Hash: finalTestHex(0x94)}}}
	reward := func(role string, subjectID uint64, uid uint16, emission string, incentive, dividends uint16, expected string) FinalNativeRewardDelta {
		return FinalNativeRewardDelta{
			Epoch: 10, Role: role, SubjectID: subjectID, UID: uid, Hotkey: "0x" + strings.Repeat(string(rune('a'+uid)), 64),
			Before: ChainHead{Number: 190, Hash: finalTestHex(0x90)}, After: ChainHead{Number: 205, Hash: finalTestHex(0x92)},
			AfterRao: emission, AfterIncentiveU16: incentive, AfterDividendsU16: dividends, Expected: expected,
		}
	}
	return &FinalSemanticEvidence{
		Netuid: 521, Window: ScenarioAcceptanceWindow{FirstEpoch: 10, EpochCount: 1},
		Validators: []FinalValidatorIdentityEvidence{{ValidatorID: 1, Cycles: []FinalCRv4Cycle{cycle}}, {ValidatorID: 2, Cycles: []FinalCRv4Cycle{cycle}}},
		NativeRewards: []FinalNativeRewardDelta{
			reward("head", 1, 1, "30", 100, 0, "positive"),
			reward("head", 2, 2, "0", 0, 0, "zero"),
			reward("pool", 1, 3, "20", 80, 0, "positive"),
			reward("validator", 1, 4, "40", 0, 90, "positive"),
		},
	}
}

// Returns the exact direct payout observation for the minimal evidence
// fixture.
func finalNativePayoutTestState(evidence *FinalSemanticEvidence) FinalNativeEpochPayoutState {
	row := func(reward FinalNativeRewardDelta, server, before, after string) FinalNativeEpochPayoutUIDState {
		return FinalNativeEpochPayoutUIDState{
			UID: reward.UID, Hotkey: reward.Hotkey, CombinedEmissionRao: reward.AfterRao, ServerEmissionRao: server,
			StakeBeforeRao: before, StakeAfterRao: after, IncentiveU16: reward.AfterIncentiveU16, DividendsU16: reward.AfterDividendsU16,
		}
	}
	return FinalNativeEpochPayoutState{
		SettlementEpoch: 10, SubnetEpoch: 50, Netuid: 521,
		Parent: ChainHead{Number: 199, Hash: finalTestHex(0x93)}, Block: ChainHead{Number: 200, Hash: finalTestHex(0x91)},
		UIDs: []FinalNativeEpochPayoutUIDState{
			row(evidence.NativeRewards[0], "30", "100", "130"),
			row(evidence.NativeRewards[1], "0", "100", "100"),
			row(evidence.NativeRewards[2], "20", "200", "220"),
			row(evidence.NativeRewards[3], "0", "300", "340"),
		},
	}
}

// Accepts the complete miner/pool/validator boundary including a rejected
// unpaid miner.
func TestFinalNativeEpochPayoutProvesExactRuntimeCausality(t *testing.T) {
	evidence := finalNativePayoutTestEvidence()
	if err := verifyFinalNativeEpochPayout(evidence, finalNativePayoutTestState(evidence)); err != nil {
		t.Fatalf("exact native payout rejected: %v", err)
	}
}

// Reproduces the root issue: a positive stake delta alone cannot establish
// runtime payment.
func TestFinalNativeEpochPayoutRejectsManualStakeInflation(t *testing.T) {
	evidence := finalNativePayoutTestEvidence()
	state := finalNativePayoutTestState(evidence)
	state.ManualStakeMutations = 1
	if err := verifyFinalNativeEpochPayout(evidence, state); err == nil || !strings.Contains(err.Error(), "manual stake mutations") {
		t.Fatalf("manual stake inflation error=%v", err)
	}
}

// Proves an unpaid miner cannot be made to look paid with a terminal balance
// mutation.
func TestFinalNativeEpochPayoutRejectsZeroWeightStakeInflation(t *testing.T) {
	evidence := finalNativePayoutTestEvidence()
	state := finalNativePayoutTestState(evidence)
	state.UIDs[1].StakeAfterRao = "101"
	if err := verifyFinalNativeEpochPayout(evidence, state); err == nil || !strings.Contains(err.Error(), "causally reflected") {
		t.Fatalf("zero-weight stake inflation error=%v", err)
	}
}

// Proves one miner's event amount cannot be moved onto another managed UID.
func TestFinalNativeEpochPayoutRejectsServerVectorSubstitution(t *testing.T) {
	evidence := finalNativePayoutTestEvidence()
	state := finalNativePayoutTestState(evidence)
	state.UIDs[0].ServerEmissionRao, state.UIDs[2].ServerEmissionRao = state.UIDs[2].ServerEmissionRao, state.UIDs[0].ServerEmissionRao
	if err := verifyFinalNativeEpochPayout(evidence, state); err == nil || !strings.Contains(err.Error(), "causally reflected") {
		t.Fatalf("server vector substitution error=%v", err)
	}
}

// Proves an event cannot be paired with a later or earlier Emission vector.
func TestFinalNativeEpochPayoutRejectsStaleStorageChannels(t *testing.T) {
	evidence := finalNativePayoutTestEvidence()
	state := finalNativePayoutTestState(evidence)
	state.UIDs[0].CombinedEmissionRao = "31"
	if err := verifyFinalNativeEpochPayout(evidence, state); err == nil || !strings.Contains(err.Error(), "runtime channels") {
		t.Fatalf("stale storage channels error=%v", err)
	}
}

// Requires the public replay to return the exact managed subject set in
// canonical order.
func TestFinalNativeEpochPayoutRejectsIncompleteOrAmbiguousUIDSet(t *testing.T) {
	evidence := finalNativePayoutTestEvidence()
	tests := []struct {
		name   string
		mutate func(*FinalNativeEpochPayoutState)
	}{
		{name: "missing", mutate: func(state *FinalNativeEpochPayoutState) { state.UIDs = state.UIDs[:len(state.UIDs)-1] }},
		{name: "duplicate", mutate: func(state *FinalNativeEpochPayoutState) { state.UIDs[1] = state.UIDs[0] }},
		{name: "unexpected", mutate: func(state *FinalNativeEpochPayoutState) { state.UIDs[3].UID = 99 }},
		{name: "noncanonical", mutate: func(state *FinalNativeEpochPayoutState) { state.UIDs[0], state.UIDs[1] = state.UIDs[1], state.UIDs[0] }},
	}
	for _, test := range tests {
		state := finalNativePayoutTestState(evidence)
		test.mutate(&state)
		if err := verifyFinalNativeEpochPayout(evidence, state); err == nil {
			t.Errorf("%s UID set was accepted", test.name)
		}
	}
}

// Proves validator agreement cannot be inferred across different automatic
// reveal blocks.
func TestFinalNativeEpochPayoutRejectsDivergentValidatorBoundary(t *testing.T) {
	evidence := finalNativePayoutTestEvidence()
	evidence.Validators[1].Cycles[0].Reveal.Block.Number++
	if _, _, err := finalNativeEpochReveal(evidence, 10); err == nil || !strings.Contains(err.Error(), "divergent") {
		t.Fatalf("divergent reveal boundary error=%v", err)
	}
}

// Pins the v453 hook ordering: a later polling observation cannot replace the
// reveal block where coinbase actually ran.
func TestFinalNativeEpochPayoutRejectsPollingApplicationAsPayoutBoundary(t *testing.T) {
	evidence := finalNativePayoutTestEvidence()
	state := finalNativePayoutTestState(evidence)
	state.Block = evidence.Validators[0].Cycles[0].Application.Block
	state.Parent.Number = state.Block.Number - 1
	if err := verifyFinalNativeEpochPayout(evidence, state); err == nil || !strings.Contains(err.Error(), "exact reveal boundary") {
		t.Fatalf("polling application boundary error=%v", err)
	}
}
