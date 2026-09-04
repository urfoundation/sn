package main

// This file proves that a V4 replacement resume authenticates the actual
// persisted plan and journal records at every permitted CREATE boundary.

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// Serializes and reloads an immutable plan snapshot before its journal is
// consumed, matching the plan-revision resume boundary.
func writeReadPersistedV4ReplacementPlan(t *testing.T, stateDir string, plan *SetupPlan) *SetupPlan {
	t.Helper()
	hash, err := plan.hash()
	if err != nil {
		t.Fatal(err)
	}
	plan.PlanHash = hash
	wire, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(stateDir, "plans", stringsTrim0x(plan.PlanHash)+".json")
	if err := atomicWrite(path, append(wire, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := readPersistedPlanFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return loaded
}

// Appends one durable journal shape so replay covers the append-only wire
// format rather than a manually assembled in-memory entry slice.
func appendPersistedV4ReplacementJournalEntry(t *testing.T, journal *Journal, plan *SetupPlan, action Action, planHash string, stage JournalStage) {
	t.Helper()
	entry := JournalEntry{
		DeploymentID: plan.DeploymentID,
		PlanHash:     planHash,
		ActionID:     action.ID,
		IntentHash:   action.IntentHash,
		Stage:        stage,
	}
	switch stage {
	case StageVerified:
		path, err := postconditionRelativePath(planHash, action.ID)
		if err != nil {
			t.Fatal(err)
		}
		entry.PostconditionPath = path
		entry.PostconditionHash = "0x" + strings.Repeat("a5", 32)
	case StageFinalized:
		entry.TransactionHash = "0x" + strings.Repeat("b6", 32)
		entry.BlockNumber = 200
		entry.BlockHash = "0x" + strings.Repeat("c7", 32)
	default:
		t.Fatalf("unsupported persisted V4 journal stage %q", stage)
	}
	if err := journal.Append(entry); err != nil {
		t.Fatal(err)
	}
}

// Covers each persisted V4 CREATE checkpoint and mutations that must fail
// after plan/journal serialization, before a replacement revision is allowed.
func TestPersistedV4ReplacementPlanAndJournalReplayRejectsBoundaryDrift(t *testing.T) {
	cfg, payloads, retained, baseline, _ := replacementPrecompileProbeFixture(t)
	secrets, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := buildDeploymentPayloads(cfg, secrets, payloads.Manifest.InitialNonce)
	if err != nil {
		t.Fatal(err)
	}
	roles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	facts := *testSetupFacts()
	facts.DeployerNonce = payloads.Manifest.InitialNonce
	basePlan, err := buildPlan(cfg, &facts, roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	if err := rebindPlanDeployment(basePlan, retained); err != nil {
		t.Fatal(err)
	}
	basePlan.CoordinatorUpgradeBaseline = baseline
	if err := rebindPlanCoordinatorUpgrade(basePlan, payloads); err != nil {
		t.Fatal(err)
	}
	basePlan.PriorPlanHashes = []string{"0x" + strings.Repeat("71", 32)}

	baseCodes := map[common.Address][]byte{}
	for _, address := range contractDeploymentAddresses(retained) {
		baseCodes[address] = append([]byte(nil), initial.ExpectedRuntime[address]...)
	}
	baseCodes[retained.PrecompileProbe] = []byte{1}
	baseCodes[common.HexToAddress(baseline.ActiveImplementation)] = append([]byte(nil), initial.ExpectedRuntime[common.HexToAddress(baseline.ActiveImplementation)]...)
	actionIDs := []string{
		"precompile.probe-deploy",
		"evm.coordinator-upgrade-implementation",
		"evm.coordinator-upgrade-activate",
		"fleet.refresh.deploy-batcher",
	}
	type boundaryCase struct {
		name                string
		nonce               uint64
		verifiedActionIDs   []string
		activated           bool
		stageOverrides      map[string]JournalStage
		foreignPlanActionID string
		mutateCodes         func(map[common.Address][]byte)
		wantError           string
	}
	cases := []boundaryCase{
		{name: "nonce 29 before replacement probe", nonce: payloads.PrecompileProbeNonce},
		{name: "nonce 30 after replacement probe", nonce: payloads.CoordinatorUpgrade.DeployerNonce, verifiedActionIDs: actionIDs[:1]},
		{name: "nonce 31 after coordinator implementation", nonce: payloads.FleetBatcherNonce, verifiedActionIDs: actionIDs[:2]},
		{name: "nonce 32 after fleet batcher", nonce: payloads.FleetBatcherNonce + 1, verifiedActionIDs: actionIDs, activated: true},
		{
			name: "retired probe code drift", nonce: payloads.PrecompileProbeNonce,
			mutateCodes: func(codes map[common.Address][]byte) {
				codes[retained.PrecompileProbe] = []byte{2}
			}, wantError: "immutable runtime mismatch",
		},
		{
			name: "coordinator implementation code drift", nonce: payloads.FleetBatcherNonce, verifiedActionIDs: actionIDs[:2],
			mutateCodes: func(codes map[common.Address][]byte) {
				codes[payloads.CoordinatorUpgrade.Implementation] = []byte{3}
			}, wantError: "consumed coordinator implementation runtime/verification",
		},
		{
			name: "fleet batcher code drift", nonce: payloads.FleetBatcherNonce + 1, verifiedActionIDs: actionIDs, activated: true,
			mutateCodes: func(codes map[common.Address][]byte) {
				codes[payloads.FleetBatcherAddress] = []byte{4}
			}, wantError: "consumed fleet batcher runtime/verification",
		},
		{
			name: "finalized probe is not verified", nonce: payloads.CoordinatorUpgrade.DeployerNonce, verifiedActionIDs: actionIDs[:1],
			stageOverrides: map[string]JournalStage{"precompile.probe-deploy": StageFinalized},
			wantError:      "replacement precompile probe runtime/verification",
		},
		{
			name: "foreign verified probe record", nonce: payloads.CoordinatorUpgrade.DeployerNonce, verifiedActionIDs: actionIDs[:1],
			foreignPlanActionID: "precompile.probe-deploy",
			wantError:           "replacement precompile probe runtime/verification",
		},
	}
	for _, test := range cases {
		encoded, err := json.Marshal(basePlan)
		if err != nil {
			t.Fatal(err)
		}
		var plan SetupPlan
		if err := json.Unmarshal(encoded, &plan); err != nil {
			t.Fatal(err)
		}
		stateDir := t.TempDir()
		if err := os.Chmod(stateDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := saveContractDeployment(stateDir, retained); err != nil {
			t.Fatal(err)
		}
		loadedPlan := writeReadPersistedV4ReplacementPlan(t, stateDir, &plan)
		journal, err := OpenJournal(stateDir)
		if err != nil {
			t.Fatal(err)
		}
		for _, actionID := range test.verifiedActionIDs {
			action := actionByID(t, loadedPlan, actionID)
			planHash := loadedPlan.PlanHash
			if actionID == test.foreignPlanActionID {
				planHash = "0x" + strings.Repeat("d8", 32)
			}
			stage := StageVerified
			if override, ok := test.stageOverrides[actionID]; ok {
				stage = override
			}
			appendPersistedV4ReplacementJournalEntry(t, journal, loadedPlan, action, planHash, stage)
		}
		if err := journal.Close(); err != nil {
			t.Fatal(err)
		}
		entries, err := readJournalEntries(stateDir)
		if err != nil {
			t.Fatal(err)
		}
		codes := make(map[common.Address][]byte, len(baseCodes)+3)
		for address, code := range baseCodes {
			codes[address] = append([]byte(nil), code...)
		}
		for _, create := range []struct {
			nonce   uint64
			address common.Address
			code    []byte
		}{
			{payloads.PrecompileProbeNonce, payloads.PrecompileProbeAddress, payloads.ExpectedRuntime[payloads.PrecompileProbeAddress]},
			{payloads.CoordinatorUpgrade.DeployerNonce, payloads.CoordinatorUpgrade.Implementation, payloads.ExpectedRuntime[payloads.CoordinatorUpgrade.Implementation]},
			{payloads.FleetBatcherNonce, payloads.FleetBatcherAddress, payloads.FleetBatcherRuntime},
		} {
			if test.nonce > create.nonce {
				codes[create.address] = append([]byte(nil), create.code...)
			}
		}
		if test.mutateCodes != nil {
			test.mutateCodes(codes)
		}
		active := common.HexToAddress(baseline.ActiveImplementation)
		if test.activated {
			active = payloads.CoordinatorUpgrade.Implementation
		}
		server := httptest.NewServer(&replacementBoundaryRPC{t: t, nonce: test.nonce, block: 200, codes: codes, activeImplementation: active})
		cfg.OperationalRPCMode = rpcModePrivateAuthority
		cfg.OperationalEVM = server.URL
		current := *testSetupFacts()
		current.DeployerNonce = test.nonce
		current.EVMFinalizedBlock = 150
		migration, observeErr := observeCoordinatorUpgradeMigration(context.Background(), cfg, stateDir, loadedPlan, &current, entries, secrets)
		server.Close()
		if test.wantError == "" {
			if observeErr != nil || migration == nil || migration.Baseline != baseline || migration.Upgrade != loadedPlan.CoordinatorUpgrade {
				t.Errorf("%s replay rejected or changed: migration=%+v error=%v", test.name, migration, observeErr)
			}
			continue
		}
		if observeErr == nil || !strings.Contains(observeErr.Error(), test.wantError) {
			t.Errorf("%s replay error=%v, want %q", test.name, observeErr, test.wantError)
		}
	}
}
