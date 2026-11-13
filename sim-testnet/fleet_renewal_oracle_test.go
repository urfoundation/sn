// Oracle renewal accepts effective restores and refuses inconsistent schedules.
package main

import (
	"context"
	"encoding/hex"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/stabi"
)

// The contract retains an effective schedule; both renewal callers use the
// same pinned reader to distinguish a completed restore from a future reroute.
func TestFleetRenewalOriginalOracleAcceptsCompletedRestore(t *testing.T) {
	parsed, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		t.Fatal(err)
	}
	coordinator := stabi.NewSTCoordinator()
	original := common.Address{19: 0x11}
	const observedBlock = uint64(42)
	outputs := map[string]string{
		"0x" + hex.EncodeToString(coordinator.PackCurrentEpoch()):                 fleetRefreshTestOutput(t, parsed, "currentEpoch", big.NewInt(7)),
		"0x" + hex.EncodeToString(coordinator.PackCommitmentOracle()):             fleetRefreshTestOutput(t, parsed, "commitmentOracle", original),
		"0x" + hex.EncodeToString(coordinator.PackActiveCommitmentOracle()):       fleetRefreshTestOutput(t, parsed, "activeCommitmentOracle", original),
		"0x" + hex.EncodeToString(coordinator.PackPendingCommitmentOracle()):      fleetRefreshTestOutput(t, parsed, "pendingCommitmentOracle", original),
		"0x" + hex.EncodeToString(coordinator.PackPendingCommitmentOracleEpoch()): fleetRefreshTestOutput(t, parsed, "pendingCommitmentOracleEpoch", uint64(3)),
	}
	server, recorder := newFleetRefreshRPCServer(t, outputs)
	defer server.Close()
	state, err := readFleetRefreshOracleStateAt(context.Background(), fleetRefreshTestManager(t, server), common.Address{19: 0x33}, coordinator, observedBlock)
	if err != nil {
		t.Fatal(err)
	}
	if !fleetRenewalOriginalOracleReady(state, original) {
		t.Fatal("completed original-oracle restore was rejected as a pending reroute")
	}
	requests, sizes, blocks := recorder.snapshot()
	if requests != 1 || len(sizes) != 1 || sizes[0] != 5 || len(blocks) != 5 {
		t.Fatalf("oracle observation was not one complete pinned batch: %d/%v/%v", requests, sizes, blocks)
	}
	for _, block := range blocks {
		if block != "0x"+new(big.Int).SetUint64(observedBlock).Text(16) {
			t.Fatalf("oracle observation used unexpected block %s", block)
		}
	}
	state.CurrentEpoch = state.PendingEpoch
	if !fleetRenewalOriginalOracleReady(state, original) {
		t.Fatal("original-oracle restore was rejected at its effective epoch")
	}
	state.Pending, state.PendingEpoch = common.Address{}, 0
	if !fleetRenewalOriginalOracleReady(state, original) {
		t.Fatal("original oracle without a schedule was rejected")
	}
}

// Every routing change and incomplete address/epoch pair must fail closed.
func TestFleetRenewalOriginalOracleRejectsChangedRouting(t *testing.T) {
	original := common.Address{19: 0x11}
	foreign := common.Address{19: 0x22}
	base := fleetRefreshOracleState{CurrentEpoch: 7, Immutable: original, Active: original, Pending: original, PendingEpoch: 3}
	for _, test := range []struct {
		name   string
		change func(*fleetRefreshOracleState)
	}{
		{name: "foreign immutable", change: func(s *fleetRefreshOracleState) { s.Immutable = foreign }},
		{name: "foreign active", change: func(s *fleetRefreshOracleState) { s.Active = foreign }},
		{name: "future original schedule", change: func(s *fleetRefreshOracleState) { s.PendingEpoch = 8 }},
		{name: "future foreign reroute", change: func(s *fleetRefreshOracleState) { s.Pending, s.PendingEpoch = foreign, 8 }},
		{name: "effective foreign reroute", change: func(s *fleetRefreshOracleState) { s.Pending = foreign }},
		{name: "missing pending epoch", change: func(s *fleetRefreshOracleState) { s.PendingEpoch = 0 }},
		{name: "missing pending address", change: func(s *fleetRefreshOracleState) { s.Pending = common.Address{} }},
		{name: "missing immutable", change: func(s *fleetRefreshOracleState) { s.Immutable = common.Address{} }},
		{name: "missing active", change: func(s *fleetRefreshOracleState) { s.Active = common.Address{} }},
	} {
		state := base
		test.change(&state)
		if fleetRenewalOriginalOracleReady(state, original) {
			t.Fatalf("%s: changed or inconsistent oracle route was accepted", test.name)
		}
	}
	if fleetRenewalOriginalOracleReady(base, foreign) {
		t.Fatal("a different retained oracle identity was accepted")
	}
	if fleetRenewalOriginalOracleReady(fleetRefreshOracleState{}, common.Address{}) {
		t.Fatal("an empty oracle identity was accepted")
	}
}
