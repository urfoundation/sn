package main

// These tests pin public native epoch decoding and the exact reader seam used
// by the final report.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"testing"

	gsrpcparser "github.com/centrifuge/go-substrate-rpc-client/v4/registry/parser"
	gsrpctypes "github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// Supplies an internally consistent public-chain fixture through the common
// test reader. Production uses the metadata-driven reader.
func (self *finalTestChainReader) NativeEpochPayout(_ context.Context, epoch uint64, block ChainHead) (finalNativeEpochPayoutRead, error) {
	rows, err := finalNativeEpochRewardRows(self.evidence, epoch)
	if err != nil {
		return finalNativeEpochPayoutRead{}, err
	}
	reveal, subnetEpoch, err := finalNativeEpochReveal(self.evidence, epoch)
	if err != nil || reveal != block {
		return finalNativeEpochPayoutRead{}, stateMismatchError(err, "fixture native payout has another reveal block")
	}
	parent := ChainHead{Number: block.Number - 1, Hash: finalTestHex(byte(block.Number - 1))}
	state := FinalNativeEpochPayoutState{SettlementEpoch: epoch, SubnetEpoch: subnetEpoch, Netuid: self.evidence.Netuid, Parent: parent, Block: block}
	for _, row := range rows {
		combined, ok := new(big.Int).SetString(row.AfterRao, 10)
		if !ok || combined.Sign() < 0 {
			return finalNativeEpochPayoutRead{}, errors.New("fixture native payout emission is invalid")
		}
		before := big.NewInt(1_000_000)
		after := new(big.Int).Set(before)
		server := new(big.Int).Set(combined)
		if row.Role == "validator" {
			server.SetInt64(0)
			if combined.Sign() > 0 {
				after.Add(after, big.NewInt(1))
			}
		} else {
			after.Add(after, server)
		}
		state.UIDs = append(state.UIDs, FinalNativeEpochPayoutUIDState{
			UID: row.UID, Hotkey: row.Hotkey, CombinedEmissionRao: combined.String(), ServerEmissionRao: server.String(),
			StakeBeforeRao: before.String(), StakeAfterRao: after.String(), IncentiveU16: row.AfterIncentiveU16, DividendsU16: row.AfterDividendsU16,
		})
	}
	return finalNativeEpochPayoutRead{State: state, ParentExchanges: self.exchange("substrate", "state_queryStorageAt", parent), BlockExchanges: self.exchange("substrate", "state_queryStorageAt", block)}, nil
}

// Wraps the common fixture reader with one deterministic decoded-state
// mutation.
type finalNativePayoutCorruptReader struct {
	*finalTestChainReader
	mutate func(*finalNativeEpochPayoutRead)
}

// Applies the configured mutation after obtaining the internally consistent
// fixture read.
func (self *finalNativePayoutCorruptReader) NativeEpochPayout(ctx context.Context, epoch uint64, block ChainHead) (finalNativeEpochPayoutRead, error) {
	read, err := self.finalTestChainReader.NativeEpochPayout(ctx, epoch, block)
	if err == nil && self.mutate != nil {
		self.mutate(&read)
	}
	return read, err
}

// Verifies transcript exchanges remain on the exact expected Substrate head.
func finalNativePayoutTestAppend(chain string, head ChainHead, exchanges []FinalRPCExchange) error {
	if chain != "substrate" || len(exchanges) == 0 {
		return errors.New("native payout test transcript is empty or on another chain")
	}
	for _, exchange := range exchanges {
		if exchange.Chain != chain || exchange.PinnedHead != head {
			return errors.New("native payout test transcript has another head")
		}
	}
	return nil
}

// Checks exact target-netuid event extraction without rejecting unrelated
// shared activity.
func TestFinalNativeEpochEventsBindEmissionAndRelevantStakeMutations(t *testing.T) {
	hotkey := [32]byte{0x11}
	coldkey := [32]byte{0x22}
	other := [32]byte{0x33}
	initialization := gsrpctypes.Phase{IsInitialization: true}
	apply := gsrpctypes.Phase{IsApplyExtrinsic: true, AsApplyExtrinsic: 4}
	emission := finalNativeTestEvent(finalNativeEpochEmissionEvent, initialization, gsrpctypes.NewU16(521), []any{gsrpctypes.NewU64(30), gsrpctypes.NewU64(0), gsrpctypes.NewU64(20)})
	unrelated := finalNativeTestEvent(finalNativeStakeAddedEvent, apply, finalNativeTestBytes(other[:]), finalNativeTestBytes(other[:]), gsrpctypes.NewU64(1), gsrpctypes.NewU64(1), gsrpctypes.NewU16(521), gsrpctypes.NewU64(0))
	relevant := finalNativeTestEvent(finalNativeStakeAddedEvent, apply, finalNativeTestBytes(coldkey[:]), finalNativeTestBytes(hotkey[:]), gsrpctypes.NewU64(1), gsrpctypes.NewU64(1), gsrpctypes.NewU16(521), gsrpctypes.NewU64(0))
	server, mutations, err := finalNativeEpochEvents([]*gsrpcparser.Event{emission, unrelated, relevant}, 521, map[[32]byte]bool{hotkey: true}, map[[32]byte]bool{coldkey: true})
	if err != nil || mutations != 1 || fmt.Sprint(server) != "[30 0 20]" {
		t.Fatalf("server/mutations=%v/%d error=%v", server, mutations, err)
	}
}

// Covers the adjacent duplicate-event and alternate-destination payout paths.
func TestFinalNativeEpochEventsRejectAmbiguityAndRedirectedAutoStake(t *testing.T) {
	hotkey := [32]byte{0x41}
	destination := [32]byte{0x42}
	owner := [32]byte{0x43}
	initialization := gsrpctypes.Phase{IsInitialization: true}
	emission := finalNativeTestEvent(finalNativeEpochEmissionEvent, initialization, gsrpctypes.NewU16(521), []any{gsrpctypes.NewU64(1)})
	auto := finalNativeTestEvent(finalNativeAutoStakeAddedEvent, initialization, gsrpctypes.NewU16(521), finalNativeTestBytes(destination[:]), finalNativeTestBytes(hotkey[:]), finalNativeTestBytes(owner[:]), gsrpctypes.NewU64(1))
	_, mutations, err := finalNativeEpochEvents([]*gsrpcparser.Event{emission, auto}, 521, map[[32]byte]bool{hotkey: true}, nil)
	if err != nil || mutations != 1 {
		t.Fatalf("redirected auto-stake mutations=%d error=%v", mutations, err)
	}
	if _, _, err := finalNativeEpochEvents([]*gsrpcparser.Event{emission, emission}, 521, map[[32]byte]bool{hotkey: true}, nil); err == nil || !strings.Contains(err.Error(), "multiple emission") {
		t.Fatalf("duplicate epoch emission error=%v", err)
	}
}

// Pins every v453 event path that can alter or reassign a managed payout
// position in the same block as automatic coinbase. Without this census, an
// unrelated extrinsic or beta-basket transition could be mistaken for
// validator/miner emission.
func TestFinalNativeEpochEventsCoverAdjacentStakeMutationClasses(t *testing.T) {
	hotkey := [32]byte{0x51}
	otherHotkey := [32]byte{0x52}
	coldkey := [32]byte{0x53}
	otherColdkey := [32]byte{0x54}
	initialization := gsrpctypes.Phase{IsInitialization: true}
	apply := gsrpctypes.Phase{IsApplyExtrinsic: true, AsApplyExtrinsic: 7}
	account := func(value [32]byte) any { return finalNativeTestBytes(value[:]) }
	amount := func(value uint64) any { return gsrpctypes.NewU64(value) }
	network := func(value uint16) any { return gsrpctypes.NewU16(value) }
	tests := []struct {
		name  string
		event *gsrpcparser.Event
	}{
		{name: "stake-transferred", event: finalNativeTestEvent(finalNativeStakeTransferredEvent, apply, account(coldkey), account(otherColdkey), account(hotkey), network(521), network(522), amount(1))},
		{name: "stake-swapped", event: finalNativeTestEvent(finalNativeStakeSwappedEvent, apply, account(coldkey), account(hotkey), network(522), network(521), amount(1))},
		{name: "alpha-recycled", event: finalNativeTestEvent(finalNativeAlphaRecycledEvent, apply, account(coldkey), account(hotkey), amount(1), network(521))},
		{name: "alpha-burned", event: finalNativeTestEvent(finalNativeAlphaBurnedEvent, apply, account(coldkey), account(hotkey), amount(1), network(521))},
		{name: "hotkey-swapped", event: finalNativeTestEvent(finalNativeHotkeySwappedEvent, apply, account(coldkey), account(hotkey), account(otherHotkey))},
		{name: "hotkey-swapped-on-subnet", event: finalNativeTestEvent(finalNativeHotkeySwappedOnSubnetEvent, apply, account(coldkey), account(hotkey), account(otherHotkey), network(521))},
		{name: "coldkey-swapped", event: finalNativeTestEvent(finalNativeColdkeySwappedEvent, apply, account(coldkey), account(otherColdkey))},
		{name: "basket-deposited", event: finalNativeTestEvent(finalNativeBasketDepositedEvent, initialization, account(hotkey), amount(1), amount(1))},
		{name: "basket-staked-in", event: finalNativeTestEvent(finalNativeBasketStakedInEvent, apply, account(hotkey), account(otherColdkey), amount(1), amount(1), amount(1))},
		{name: "basket-claimed", event: finalNativeTestEvent(finalNativeBasketClaimedEvent, apply, account(hotkey), account(otherColdkey), amount(1))},
		{name: "basket-holding-converted", event: finalNativeTestEvent(finalNativeBasketHoldingConvertedEvent, initialization, account(hotkey), network(521), amount(1))},
		{name: "alpha-fee", event: finalNativeTestEvent(finalNativeAlphaFeeEvent, apply, account(coldkey), network(521), amount(1), amount(1))},
	}
	for _, test := range tests {
		relevant, err := finalNativeRelevantStakeMutation(test.event, 521, map[[32]byte]bool{hotkey: true}, map[[32]byte]bool{coldkey: true})
		if err != nil || !relevant {
			t.Errorf("%s relevant=%t error=%v", test.name, relevant, err)
		}
	}

	unrelated := finalNativeTestEvent(finalNativeStakeSwappedEvent, apply, account(otherColdkey), account(otherHotkey), network(521), network(522), amount(1))
	if relevant, err := finalNativeRelevantStakeMutation(unrelated, 521, map[[32]byte]bool{hotkey: true}, map[[32]byte]bool{coldkey: true}); err != nil || relevant {
		t.Fatalf("unrelated stake swap relevant=%t error=%v", relevant, err)
	}
	malformed := finalNativeTestEvent(finalNativeStakeSwappedEvent, apply, account(coldkey))
	if _, err := finalNativeRelevantStakeMutation(malformed, 521, map[[32]byte]bool{hotkey: true}, map[[32]byte]bool{coldkey: true}); err == nil || !strings.Contains(err.Error(), "invalid shape") {
		t.Fatalf("malformed stake swap error=%v", err)
	}
}

// Binds the one-block stake delta to the parent hash returned by the exact
// reveal header.
func TestFinalNativeParentHeadRejectsSubstitution(t *testing.T) {
	block := ChainHead{Number: 200, Hash: finalTestHex(0x71)}
	raw, _ := json.Marshal(map[string]string{"number": "0xc8", "parentHash": finalTestHex(0x70)})
	parent, err := finalNativeParentHead(raw, block)
	if err != nil || parent.Number != 199 || parent.Hash != finalTestHex(0x70) {
		t.Fatalf("parent=%+v error=%v", parent, err)
	}
	wrong, _ := json.Marshal(map[string]string{"number": "0xc7", "parentHash": finalTestHex(0x70)})
	if _, err := finalNativeParentHead(wrong, block); err == nil {
		t.Fatal("substituted reveal header was accepted")
	}
}

// Exercises the complete semantic reader seam and rejects a UID-channel
// substitution after capture.
func TestFinalNativeEpochPayoutPublicReplayRejectsTampering(t *testing.T) {
	evidence := finalNativePayoutTestEvidence()
	reader := &finalTestChainReader{evidence: evidence}
	states, err := verifyFinalSemanticNativeEpochPayouts(context.Background(), evidence, reader, finalNativePayoutTestAppend)
	if err != nil {
		t.Fatalf("exact native payout public replay rejected: %v", err)
	}
	if len(states) != 1 || states[0].SettlementEpoch != evidence.Window.FirstEpoch {
		t.Fatalf("retained native payout states=%+v", states)
	}
	corrupt := &finalNativePayoutCorruptReader{finalTestChainReader: reader, mutate: func(read *finalNativeEpochPayoutRead) { read.State.UIDs[0].ServerEmissionRao = "1" }}
	if _, err := verifyFinalSemanticNativeEpochPayouts(context.Background(), evidence, corrupt, finalNativePayoutTestAppend); err == nil {
		t.Fatal("tampered native payout public replay was accepted")
	}
}

// Proves offline verification cannot accept a valid-looking v5 transcript that
// omits or truncates this mandatory on-chain payout replay.
func TestFinalPublicNativePayoutAuditSealsExactScope(t *testing.T) {
	evidence := finalNativePayoutTestEvidence()
	audit, err := finalPublicNativePayoutAuditForEvidence(evidence)
	if err != nil {
		t.Fatal(err)
	}
	if audit.Schema != finalPublicNativePayoutAuditSchema || audit.Epochs != 1 || audit.ParentTransitions != 1 || audit.UIDRows != 4 {
		t.Fatalf("native payout audit=%+v", audit)
	}
	if err := verifyFinalPublicNativePayoutAudit(evidence, audit); err != nil {
		t.Fatalf("exact native payout audit rejected: %v", err)
	}
	state := finalNativePayoutTestState(evidence)
	if err := verifyFinalPublicNativePayoutStates(evidence, audit, []FinalNativeEpochPayoutState{state}); err != nil {
		t.Fatalf("exact retained native payout state rejected: %v", err)
	}
	tamperedState := state
	tamperedState.UIDs = append([]FinalNativeEpochPayoutUIDState(nil), state.UIDs...)
	tamperedState.UIDs[0].StakeAfterRao = "131"
	if err := verifyFinalPublicNativePayoutStates(evidence, audit, []FinalNativeEpochPayoutState{tamperedState}); err == nil {
		t.Fatal("tampered retained native payout state was accepted")
	}
	if err := verifyFinalPublicNativePayoutStates(evidence, audit, nil); err == nil || !strings.Contains(err.Error(), "observations") {
		t.Fatalf("missing retained native payout states error=%v", err)
	}
	mutated := *evidence
	mutated.NativeRewards = append([]FinalNativeRewardDelta(nil), evidence.NativeRewards...)
	mutated.NativeRewards[0].AfterRao = "31"
	if err := verifyFinalPublicNativePayoutAudit(&mutated, audit); err == nil || !strings.Contains(err.Error(), "differs") {
		t.Fatalf("stale native payout projection error=%v", err)
	}
	if err := verifyFinalPublicNativePayoutAudit(evidence, FinalPublicNativePayoutAudit{}); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("missing native payout projection error=%v", err)
	}
}
