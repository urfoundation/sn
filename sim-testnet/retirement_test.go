package main

import (
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/urfoundation/sn/stabi"
)

func TestRetirementPlanIsSeparateFutureEffectiveAndBounded(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	setup, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	deployment := &ContractDeployment{CoordinatorProxy: common.HexToAddress("0x0000000000000000000000000000000000001234")}
	operators := []stabi.STCoordinatorOperatorVersion{
		{DepositHotkey: [32]byte{1}, DepositSigner: common.HexToAddress(roles.OperatorDepositSigners[0]), RootSigner: common.HexToAddress(roles.OperatorRootSigners[0]), Active: true},
		{DepositHotkey: [32]byte{2}, DepositSigner: common.HexToAddress(roles.OperatorDepositSigners[1]), RootSigner: common.HexToAddress(roles.OperatorRootSigners[1]), Active: true},
	}
	a, err := buildRetirementPlan(cfg, setup, deployment, 25, operators, time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	b, err := buildRetirementPlan(cfg, setup, deployment, 25, operators, time.Unix(3, 0))
	if err != nil {
		t.Fatal(err)
	}
	if a.PlanHash == setup.PlanHash || a.PlanHash != b.PlanHash || a.EffectiveEpoch != 26 || len(a.Actions) != 2 || a.MaximumSpend.EVMGasWei != a.ReservedGasWei {
		t.Fatalf("retirement plan = %+v", a)
	}
	for noID, action := range a.Actions {
		maximumGasUnits, maximumFeePerGas, envelopeErr := evmActionFeeEnvelope(action)
		if envelopeErr != nil || maximumGasUnits == 0 || maximumFeePerGas != setup.MaximumEVMFeePerGasWei {
			t.Fatalf("retirement EVM envelope = %d/%d, %v", maximumGasUnits, maximumFeePerGas, envelopeErr)
		}
		if action.Parameters["effective_epoch"] != "26" || action.Parameters["no_id"] != string(rune('1'+noID)) || !strings.Contains(action.Description, "preserving prior entitlements") || action.IntentHash == "" {
			t.Fatalf("retirement action = %+v", action)
		}
		parsedNO, effective, hotkey, deposit, root, parseErr := parseRetirementAction(action)
		if parseErr != nil || parsedNO != uint64(noID+1) || effective != 26 || hotkey != operators[noID].DepositHotkey || deposit != operators[noID].DepositSigner || root != operators[noID].RootSigner {
			t.Fatalf("parsed retirement action = %d %d %x %s %s %v", parsedNO, effective, hotkey, deposit, root, parseErr)
		}
	}
	changed, err := buildRetirementPlan(cfg, setup, deployment, 26, operators, time.Unix(3, 0))
	if err != nil || changed.PlanHash == a.PlanHash {
		t.Fatalf("epoch did not invalidate approval: plan=%+v err=%v", changed, err)
	}
}

func TestRetirementPlanRejectsIncompleteOrInactiveState(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, _ := derivePublicRoles(cfg)
	setup, _ := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0))
	deployment := &ContractDeployment{CoordinatorProxy: common.HexToAddress("0x0000000000000000000000000000000000001234")}
	operators := []stabi.STCoordinatorOperatorVersion{{Active: false}, {Active: true}}
	if _, err := buildRetirementPlan(cfg, setup, deployment, 1, operators, time.Now()); err == nil {
		t.Fatal("inactive/incomplete operator state accepted")
	}
	bad := Action{Parameters: map[string]string{"no_id": "0"}}
	if _, _, _, _, _, err := parseRetirementAction(bad); err == nil {
		t.Fatal("invalid retirement action accepted")
	}
}

func TestRetirementPostconditionRequiresExactInactiveVersion(t *testing.T) {
	hotkey := [32]byte{7}
	deposit := common.HexToAddress("0x0000000000000000000000000000000000000011")
	root := common.HexToAddress("0x0000000000000000000000000000000000000022")
	version := stabi.STCoordinatorOperatorVersion{EffectiveEpoch: 9, DepositHotkey: hotkey, DepositSigner: deposit, RootSigner: root, Active: false}
	if !retirementVersionMatches(version, 9, hotkey, deposit, root) {
		t.Fatal("exact inactive retirement version was rejected")
	}
	mutations := []stabi.STCoordinatorOperatorVersion{version, version, version, version, version}
	mutations[0].Active = true
	mutations[1].EffectiveEpoch++
	mutations[2].DepositHotkey[0]++
	mutations[3].DepositSigner = root
	mutations[4].RootSigner = deposit
	for i, mutation := range mutations {
		if retirementVersionMatches(mutation, 9, hotkey, deposit, root) {
			t.Fatalf("retirement mutation %d was accepted: %+v", i, mutation)
		}
	}
}
