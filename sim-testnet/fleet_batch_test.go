package main

// These tests lock the accelerated fleet partitions, ABI shape and expiry
// preconditions without depending on wall-clock timing or a live RPC.

import (
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/urfoundation/sn/protocol"
	"github.com/urfoundation/sn/stabi"
)

// Cover the full and final partial batch boundaries and every adjacent range
// mutation that could overlap or omit a fleet.
func TestFleetInstallActionRangeRequiresTheCanonicalPartition(t *testing.T) {
	cfg := testResolvedConfig(t)
	cfg.Config.Topology.HeadFleets = 23
	action := Action{Parameters: map[string]string{"first_fleet": "11", "last_fleet": "20", "generation": "1"}}
	firstFleet, lastFleet, err := fleetInstallActionRange(cfg, action, 2)
	if err != nil || firstFleet != 11 || lastFleet != 20 {
		t.Fatalf("canonical install range = %d..%d: %v", firstFleet, lastFleet, err)
	}
	finalAction := Action{Parameters: map[string]string{"first_fleet": "21", "last_fleet": "23", "generation": "1"}}
	firstFleet, lastFleet, err = fleetInstallActionRange(cfg, finalAction, 3)
	if err != nil || firstFleet != 21 || lastFleet != 23 {
		t.Fatalf("final install range = %d..%d: %v", firstFleet, lastFleet, err)
	}
	mutations := []Action{
		{Parameters: map[string]string{"first_fleet": "10", "last_fleet": "20", "generation": "1"}},
		{Parameters: map[string]string{"first_fleet": "11", "last_fleet": "19", "generation": "1"}},
		{Parameters: map[string]string{"first_fleet": "11", "last_fleet": "20", "generation": "2"}},
	}
	for _, mutation := range mutations {
		if _, _, err := fleetInstallActionRange(cfg, mutation, 2); err == nil {
			t.Errorf("noncanonical install range accepted: %+v", mutation.Parameters)
		}
	}
}

// Reject malformed, mixed, cyclic, oversized, and non-contiguous commitment
// concurrency groups before any executor can submit a transaction.
func TestFleetCommitmentParallelGroupsAreBoundedIndependentAndContiguous(t *testing.T) {
	first := Action{ID: "fleet.commitment.1", Kind: "substrate-extrinsic", Parameters: map[string]string{fleetCommitmentParallelGroupParameter: "install-1"}, DependsOn: []string{"barrier"}}
	second := Action{ID: "fleet.commitment.2", Kind: "substrate-extrinsic", Parameters: map[string]string{fleetCommitmentParallelGroupParameter: "install-1"}, DependsOn: []string{"barrier"}}
	actions := []Action{first, second}
	end, grouped, err := fleetCommitmentParallelRange(actions, 0)
	if err != nil || !grouped || end != 2 || validateFleetCommitmentParallelGroups(actions) != nil {
		t.Fatalf("valid parallel group rejected: end=%d grouped=%t error=%v", end, grouped, err)
	}

	internalDependency := append([]Action(nil), actions...)
	internalDependency[1].DependsOn = []string{first.ID}
	if err := validateFleetCommitmentParallelGroups(internalDependency); err == nil {
		t.Error("internally dependent parallel group was accepted")
	}
	mixed := append([]Action(nil), actions...)
	mixed[1].ID = "fleet.refresh.commitment.2"
	if err := validateFleetCommitmentParallelGroups(mixed); err == nil {
		t.Error("mixed-generation parallel group was accepted")
	}
	wrongKind := append([]Action(nil), actions...)
	wrongKind[0].Kind = "evm-read"
	if err := validateFleetCommitmentParallelGroups(wrongKind); err == nil {
		t.Error("non-native parallel action was accepted")
	}
	nonContiguous := []Action{first, {ID: "barrier"}, second}
	if err := validateFleetCommitmentParallelGroups(nonContiguous); err == nil {
		t.Error("non-contiguous duplicate group was accepted")
	}
	oversized := make([]Action, fleetRefreshBatchSize+1)
	for index := range oversized {
		oversized[index] = Action{ID: "fleet.commitment." + strconv.Itoa(index+1), Kind: "substrate-extrinsic", Parameters: map[string]string{fleetCommitmentParallelGroupParameter: "install-oversized"}}
	}
	if err := validateFleetCommitmentParallelGroups(oversized); err == nil {
		t.Error("oversized parallel group was accepted")
	}
}

// Renewal uses the same disjoint partition but must never accept generation 1.
func TestFleetRefreshActionRangeRequiresTheCanonicalPartition(t *testing.T) {
	cfg := testResolvedConfig(t)
	cfg.Config.Topology.HeadFleets = 23
	action := Action{Parameters: map[string]string{"first_fleet": "21", "last_fleet": "23", "generation": "2"}}
	firstFleet, lastFleet, err := fleetRefreshActionRange(cfg, action, 3)
	if err != nil || firstFleet != 21 || lastFleet != 23 {
		t.Fatalf("canonical refresh range = %d..%d: %v", firstFleet, lastFleet, err)
	}
	action.Parameters["generation"] = "1"
	if _, _, err := fleetRefreshActionRange(cfg, action, 3); err == nil {
		t.Fatal("generation-1 refresh range was accepted")
	}
}

// Deterministically reproduce the expiry failure that long sequential setup
// could otherwise encounter, plus cleaned and non-future adjacent states.
func TestFleetRefreshPriorStateRejectsExpiredOrInexactGeneration(t *testing.T) {
	binding := protocol.FleetBinding{
		ChainID: 945, Netuid: 521, Generation: 1, ValidFromEpoch: 7, ValidToEpoch: 38,
		Coordinator: [20]byte{1}, FleetID: [32]byte{2}, Hotkey: [32]byte{3},
		ClientID: [16]byte{4}, ClientKey: [32]byte{5}, CommitmentHash: [32]byte{6},
	}
	evidence := FleetBindingEvidence{Generation: 1, ValidFromEpoch: 7, ValidToEpoch: 38, UID: 42}
	record := stabi.STCoordinatorBindingRecord{
		FleetId: binding.FleetID, Hotkey: binding.Hotkey, ClientKey: binding.ClientKey,
		CommitmentHash: binding.CommitmentHash, Generation: 1, ValidFromEpoch: 7,
		ValidToEpoch: 38, Uid: 42,
	}
	if err := validateFleetRefreshPriorState(20, 21, evidence, record, binding); err != nil {
		t.Fatalf("valid prior state rejected: %v", err)
	}
	if err := validateFleetRefreshPriorState(38, 39, evidence, record, binding); err == nil {
		t.Fatal("replacement after prior expiry was accepted")
	}
	if err := validateFleetRefreshPriorState(21, 21, evidence, record, binding); err == nil {
		t.Fatal("non-future replacement was accepted")
	}
	record.Cleaned = true
	if err := validateFleetRefreshPriorState(20, 21, evidence, record, binding); err == nil {
		t.Fatal("cleaned prior generation was accepted")
	}
}

// Prove the generated Go tuple can encode both helper entry points and that a
// selector swap cannot silently reinterpret an install as a refresh.
func TestFleetBatcherABIEncodesInstallAndRefreshTuples(t *testing.T) {
	parsed, err := abi.JSON(strings.NewReader(FleetBatcherABI))
	if err != nil {
		t.Fatal(err)
	}
	binding := stabi.STCoordinatorFleetBinding{
		ChainId: 945, Netuid: 521, Coordinator: common.HexToAddress("0x1111111111111111111111111111111111111111"),
		FleetId: [32]byte{1}, Hotkey: [32]byte{2}, ClientId: [16]byte{3}, ClientKey: [32]byte{4},
		Generation: 1, ValidFromEpoch: 2, ValidToEpoch: 33, CommitmentHash: [32]byte{5},
	}
	fleets := []fleetBatcherFleetRefresh{{
		Hotkey: [32]byte{2}, CommitmentHash: [32]byte{5}, FinalizedBlock: 100, FinalizedBlockHash: [32]byte{6},
		Members: []fleetBatcherMemberRefresh{{
			PriorGeneration: 0, Binding: binding, RevokeSignature: []byte{},
			ClientSignature: make([]byte, 64), HotkeySignature: make([]byte, 64),
		}},
	}}
	installData, err := parsed.Pack("install", fleets)
	if err != nil {
		t.Fatal(err)
	}
	refreshData, err := parsed.Pack("refresh", fleets)
	if err != nil {
		t.Fatal(err)
	}
	if string(installData[:4]) != string(parsed.Methods["install"].ID) || string(refreshData[:4]) != string(parsed.Methods["refresh"].ID) || string(installData[:4]) == string(refreshData[:4]) {
		t.Fatal("fleet batch method selectors are absent or aliased")
	}
	if _, err := parsed.Methods["install"].Inputs.Unpack(installData[4:]); err != nil {
		t.Fatalf("install tuple does not round-trip through ABI: %v", err)
	}
}

// A partition must cover every fleet exactly once, whether carried or newly
// installed.
func TestFleetInstallPartitionsRejectGapsDuplicatesAndOutOfRange(t *testing.T) {
	valid := FleetInstallBatchEvidence{FirstFleet: 11, LastFleet: 13, InstalledFleets: []int{12, 13}, CarriedFleets: []int{11}}
	if err := validateFleetInstallPartitions(valid); err != nil {
		t.Fatalf("valid partition rejected: %v", err)
	}
	duplicate := valid
	duplicate.CarriedFleets = []int{11, 12}
	if err := validateFleetInstallPartitions(duplicate); err == nil {
		t.Fatal("duplicate fleet partition was accepted")
	}
	gap := valid
	gap.InstalledFleets = []int{13}
	if err := validateFleetInstallPartitions(gap); err == nil {
		t.Fatal("gapped fleet partition was accepted")
	}
	outOfRange := valid
	outOfRange.InstalledFleets = []int{12, 14}
	if err := validateFleetInstallPartitions(outOfRange); err == nil {
		t.Fatal("out-of-range fleet partition was accepted")
	}
}

// Prepared calldata is an exact crash-recovery artifact; changing its bytes,
// plan identity or range must invalidate it before transaction recovery.
func TestFleetInstallPreparedEvidenceBindsCalldataAndPlanIdentity(t *testing.T) {
	cfg := testResolvedConfig(t)
	cfg.Config.Topology.HeadFleets = 10
	plan := &SetupPlan{PlanHash: "0x" + strings.Repeat("11", 32)}
	action := Action{ID: "fleet.install.batch.1", IntentHash: "0x" + strings.Repeat("22", 32)}
	data := []byte{1, 2, 3, 4}
	prepared := &fleetInstallPreparedEvidence{
		Schema: fleetInstallPreparedSchema, DeploymentID: cfg.Config.Deployment.DeploymentID,
		PlanHash: plan.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash,
		Batch: 1, FirstFleet: 1, LastFleet: 10, Generation: 1, EffectiveEpoch: 2, ValidToEpoch: 33,
		Calldata: "0x01020304", CalldataHash: crypto.Keccak256Hash(data).Hex(),
		Fleets: []fleetInstallPreparedFleet{{Fleet: 1}},
	}
	decoded, err := validateFleetInstallPrepared(prepared, cfg, plan, action, 1, 1, 10)
	if err != nil || string(decoded) != string(data) {
		t.Fatalf("valid prepared evidence rejected: %x %v", decoded, err)
	}
	tampered := *prepared
	tampered.Calldata = "0x01020305"
	if _, err := validateFleetInstallPrepared(&tampered, cfg, plan, action, 1, 1, 10); err == nil {
		t.Fatal("tampered prepared calldata was accepted")
	}
	tampered = *prepared
	tampered.PlanHash = "0x" + strings.Repeat("33", 32)
	if _, err := validateFleetInstallPrepared(&tampered, cfg, plan, action, 1, 1, 10); err == nil {
		t.Fatal("cross-plan prepared calldata was accepted")
	}
}

// The built release plan must deploy/activate once, use twenty atomic install
// and refresh batches, and leave all head per-member actions read-only.
func TestBuildPlanBatchesEveryHeadFleetBeforeTopologyLaunch(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	actions := map[string]Action{}
	positions := map[string]int{}
	for position, action := range plan.Actions {
		actions[action.ID] = action
		positions[action.ID] = position
	}
	wantBatches := cfg.Config.Topology.HeadFleets / fleetRefreshBatchSize
	for batch := 1; batch <= wantBatches; batch++ {
		installID := "fleet.install.batch." + strconv.Itoa(batch)
		refreshID := "fleet.refresh.batch." + strconv.Itoa(batch)
		if actions[installID].Kind != "evm-transaction" || actions[refreshID].Kind != "evm-transaction" {
			t.Fatalf("batch %d is absent: install=%+v refresh=%+v", batch, actions[installID], actions[refreshID])
		}
		first := (batch-1)*fleetRefreshBatchSize + 1
		last := first + fleetRefreshBatchSize - 1
		for fleetIndex := first; fleetIndex <= last; fleetIndex++ {
			installCommitment := actions["fleet.commitment."+strconv.Itoa(fleetIndex)]
			refreshCommitment := actions["fleet.refresh.commitment."+strconv.Itoa(fleetIndex)]
			if installCommitment.Parameters[fleetCommitmentParallelGroupParameter] != "install-"+strconv.Itoa(batch) || refreshCommitment.Parameters[fleetCommitmentParallelGroupParameter] != "refresh-"+strconv.Itoa(batch) {
				t.Fatalf("fleet %d commitment groups are invalid: install=%+v refresh=%+v", fleetIndex, installCommitment, refreshCommitment)
			}
			if !slices.Contains(actions[installID].DependsOn, installCommitment.ID) || !slices.Contains(actions[refreshID].DependsOn, refreshCommitment.ID) {
				t.Fatalf("batch %d does not depend on fleet %d commitments", batch, fleetIndex)
			}
			for _, dependency := range installCommitment.DependsOn {
				if strings.HasPrefix(dependency, "fleet.commitment.") {
					t.Fatalf("parallel install commitment %s depends on group member %s", installCommitment.ID, dependency)
				}
			}
			for _, dependency := range refreshCommitment.DependsOn {
				if strings.HasPrefix(dependency, "fleet.refresh.commitment.") {
					t.Fatalf("parallel refresh commitment %s depends on group member %s", refreshCommitment.ID, dependency)
				}
			}
		}
	}
	for fleetIndex := 1; fleetIndex <= cfg.Config.Topology.HeadFleets; fleetIndex++ {
		mirror := actions["fleet.mirror."+strconv.Itoa(fleetIndex)]
		if mirror.Kind != "evm-read" || !mirror.Spend.EVMGasWei.IsZero() || mirror.Parameters["batch_installed"] != "true" {
			t.Fatalf("head fleet %d mirror is not a strict batch proof: %+v", fleetIndex, mirror)
		}
		for memberIndex := 1; memberIndex <= cfg.Config.Topology.ClientsPerHeadFleet; memberIndex++ {
			id := "fleet.bind." + strconv.Itoa(fleetIndex) + "." + strconv.Itoa(memberIndex)
			binding := actions[id]
			if binding.Kind != "evm-read" || !binding.Spend.EVMGasWei.IsZero() || binding.Parameters["batch_installed"] != "true" {
				t.Fatalf("%s is not a strict batch proof: %+v", id, binding)
			}
		}
	}
	if positions["fleet.refresh.deploy-batcher"] >= positions["fleet.commitment.1"] || positions["fleet.refresh.oracle-await-active"] >= positions["fleet.install.batch.1"] || positions["fleet.refresh.oracle-await-restored"] >= positions["topology.launch"] {
		t.Fatal("fleet batcher activation/install/restore topology order is invalid")
	}
}

// A repeated upgrade changes the helper CREATE nonce/address. Every reference
// must move together; retaining even one old batch target would strand the
// live plan after a valid formal revision.
func TestCoordinatorUpgradeRebindMovesEveryFleetBatcherReference(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	secrets, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	payloads, err := buildDeploymentPayloads(cfg, secrets, plan.Deployment.InitialNonce)
	if err != nil {
		t.Fatal(err)
	}
	oldBatcher := payloads.FleetBatcherAddress
	if err := configureCoordinatorUpgradeNonce(payloads, payloads.CoordinatorUpgrade.DeployerNonce+7); err != nil {
		t.Fatal(err)
	}
	if payloads.FleetBatcherAddress == oldBatcher {
		t.Fatal("test nonce did not change the predicted fleet batcher")
	}
	if err := rebindPlanCoordinatorUpgrade(plan, payloads); err != nil {
		t.Fatal(err)
	}
	for _, action := range plan.Actions {
		switch {
		case action.ID == "fleet.refresh.deploy-batcher":
			if common.HexToAddress(action.Target) != payloads.FleetBatcherAddress || common.HexToAddress(action.Parameters["expected_created_address"]) != payloads.FleetBatcherAddress {
				t.Fatalf("fleet batcher deployment was not rebound: %+v", action)
			}
		case action.ID == "fleet.refresh.oracle-activate":
			if common.HexToAddress(action.Parameters["oracle"]) != payloads.FleetBatcherAddress {
				t.Fatalf("fleet batcher activation was not rebound: %+v", action)
			}
		case action.ID == "fleet.refresh.oracle-await-active", strings.HasPrefix(action.ID, "fleet.install.batch."), strings.HasPrefix(action.ID, "fleet.refresh.batch."):
			if common.HexToAddress(action.Target) != payloads.FleetBatcherAddress {
				t.Fatalf("fleet batcher reference %s retained %s, want %s", action.ID, action.Target, payloads.FleetBatcherAddress)
			}
		}
	}
}
