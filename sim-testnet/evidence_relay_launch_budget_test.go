//go:build linux || darwin

// Complete four-source header identities, actual signed requests and the
// durable journal prove the finite relay allowance. No transaction finality
// is claimed by these admission-only tests.
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/protocol"
	validatorcomponent "github.com/urfoundation/sn/v2026/validator"
)

// Each original operator consents separately even when both headers share
// one validator-level public census. Audit subjects own different slots.
func evidenceRelayLaunchRequestTest(t *testing.T, fixture *runtimeEvidenceProvisionV2TestFixture, member runtimeEvidenceActivationMemberV2, epoch uint64, audit bool, nativeEpoch uint64) validatorcomponent.ValidatorEvidenceTransactionV2Expected {
	t.Helper()
	activation := member.Activation
	domain, err := activation.EvidenceDomain()
	if err != nil {
		t.Fatal(err)
	}
	start := uint64(300) + (epoch-activation.Domain.Epoch)*300
	window := protocol.ValidatorEvidenceWindow{Epoch: epoch, StartBlock: start, EndBlock: start + 300, FinalizedBlock: start + 300}
	header := protocol.ValidatorEvidenceHeader{Domain: domain, Hotkey: activation.Hotkey, NoID: activation.NoID, Epoch: epoch,
		Kind: protocol.ValidatorEvidenceClosedCensus, VPK: activation.VPK, BoundaryBlock: window.EndBlock - 1,
		BoundaryHash: [32]byte{0x11}, CensusHash: [32]byte{0x12}, PayloadHash: [32]byte{0x13}, PayloadBytes: 100}
	if audit {
		window.Subject = protocol.ValidatorEvidenceSubject{ObservationEpoch: epoch + 1, NativeEpoch: nativeEpoch}
		window.FinalizedBlock = window.EndBlock + 1
		header.Kind, header.Subject, header.BoundaryBlock = protocol.ValidatorEvidenceDepositAudit, window.Subject, window.FinalizedBlock
	}
	hotkey, vpk, err := runtimeEvidenceActivationKeysV2(fixture.roles, member.ValidatorId, member.NoId)
	if err != nil {
		t.Fatal(err)
	}
	signed := validatorcomponent.ValidatorEvidenceSignedV2{Schema: validatorcomponent.ValidatorEvidenceSignedV2Schema, Header: header}
	signed.VPKSignature, err = header.SignVPK(vpk)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := header.Digest()
	if err != nil {
		t.Fatal(err)
	}
	signed.HotkeySignature, err = hotkey.Sign(digest[:])
	if err != nil {
		t.Fatal(err)
	}
	if err := header.Verify(domain, window, signed.VPKSignature, signed.HotkeySignature); err != nil {
		t.Fatal(err)
	}
	return validatorcomponent.ValidatorEvidenceTransactionV2Expected{Journal: fixture.plan.ValidatorEvidence.Address,
		RuntimeHash: [32]byte(fixture.plan.ValidatorEvidence.RuntimeCodeHash), Activation: activation, Window: window, Evidence: signed,
		MaxTransactionBytes: 64 * 1024, MaxReceiptLogs: 1024}
}

// This fixed 128-slot admission control is not the campaign horizon forecast.
// It preserves closed/audit/failed debit and exact-retry coverage independently
// of the larger actual profile checked by the horizon/full-plan tests.
func TestEvidenceRelayLaunchSlotCensusKeepsEverySourceAndFailureDebit(t *testing.T) {
	launch := runtimeEvidenceLaunchConfigTest(t)
	if launch.Config.Topology.Miners != 1000 || launch.Config.Topology.HeadSlots != 200 || launch.Config.Topology.Validators != 2 || launch.Config.Topology.Operators != 2 || launch.Config.ValidatorEvidenceRelay.MaxSlots != 256 {
		t.Fatal("actual launch source/population/slot geometry changed")
	}
	fixture := newRuntimeEvidenceProvisionV2ConfiguredTestFixture(t, func(cfg *ResolvedConfig) {
		cfg.Config.ValidatorEvidenceRelay = launch.Config.ValidatorEvidenceRelay
		cfg.Config.ValidatorEvidenceRelay.MaxSlots = 128
	})
	journal, err := OpenJournal(fixture.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	executor := &Executor{cfg: fixture.cfg, plan: fixture.plan, roles: fixture.roles, stateDir: fixture.stateDir, journal: journal}
	t.Cleanup(func() {
		if executor.journal != nil {
			if err := executor.journal.Close(); err != nil {
				t.Error(err)
			}
		}
	})
	var requests []validatorcomponent.ValidatorEvidenceTransactionV2Expected
	for index := uint64(0); index < 15; index++ {
		for _, member := range fixture.prepared.Members {
			requests = append(requests, evidenceRelayLaunchRequestTest(t, fixture, member, member.Activation.Domain.Epoch+index, false, 0))
		}
	}
	for index := uint64(0); index < 13; index++ {
		for _, member := range fixture.prepared.Members {
			requests = append(requests, evidenceRelayLaunchRequestTest(t, fixture, member, member.Activation.Domain.Epoch+index, true, 100+index))
		}
	}
	if len(requests) != 112 {
		t.Fatal("validator-level censuses replaced per-operator immutable slots")
	}
	for index := uint64(0); index < 4; index++ {
		for _, member := range fixture.prepared.Members {
			requests = append(requests, evidenceRelayLaunchRequestTest(t, fixture, member, member.Activation.Domain.Epoch+15+index, false, 0))
		}
	}
	if len(requests) != 128 {
		t.Fatal("failed-admission headroom is not explicitly finite")
	}
	reserve, err := exactPlanActionByID(fixture.plan, evidenceRelayReserveId)
	if err != nil {
		t.Fatal(err)
	}
	slots := map[string]bool{}
	var actions []Action
	for index, request := range requests {
		expected, err := buildEvidenceRelayAction(fixture.plan, reserve, request)
		if err != nil {
			t.Fatal(err)
		}
		if slots[expected.ID] {
			t.Fatal("distinct source/epoch/kind/audit subject collapsed into one slot")
		}
		if index == 64 {
			if _, _, err := evidenceRelayAdmissionCount(executor.journal.Entries(), fixture.plan.PlanHash, expected, 64); err == nil {
				t.Fatal("the original 64-slot bound unexpectedly covers the complete four-source census")
			}
		}
		action, err := executor.admitEvidenceRelayAction(t.Context(), request)
		if err != nil || action.IntentHash != expected.IntentHash {
			t.Fatalf("actual admission %d: %v", index, err)
		}
		slots[action.ID] = true
		actions = append(actions, action)
		if index >= 112 {
			if err := executor.journal.Append(JournalEntry{DeploymentID: fixture.plan.DeploymentID, PlanHash: fixture.plan.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageFailed}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(slots) != 128 || len(executor.journal.Entries()) != 144 {
		t.Fatal("full source and failed-admission journal census differs")
	}
	if err := executor.journal.Close(); err != nil {
		t.Fatal(err)
	}
	executor.journal, err = OpenJournal(fixture.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	for index, request := range requests {
		action, err := executor.admitEvidenceRelayAction(t.Context(), request)
		if err != nil || action.IntentHash != actions[index].IntentHash {
			t.Fatalf("original retry after reopen %d: %v", index, err)
		}
	}
	if len(executor.journal.Entries()) != 144 {
		t.Fatal("exact retries consumed extra budget or refunded failed slots")
	}
	last := fixture.prepared.Members[0]
	oneOver := evidenceRelayLaunchRequestTest(t, fixture, last, last.Activation.Domain.Epoch+19, false, 0)
	oneOverAction, err := buildEvidenceRelayAction(fixture.plan, reserve, oneOver)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(fixture.stateDir, "journal.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := executor.admitEvidenceRelayAction(t.Context(), oneOver); err == nil || !strings.Contains(err.Error(), "exhausted its approved slot allowance") {
		t.Fatal("the 129th distinct call escaped the absolute slot bound", err)
	}
	after, err := os.ReadFile(filepath.Join(fixture.stateDir, "journal.jsonl"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("one-over refusal changed durable admission", err)
	}
	path := filepath.Join(fixture.stateDir, "evidence-relay", strings.TrimPrefix(oneOverAction.ID, evidenceRelayActionPrefix)+".json")
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatal("one-over refusal retained an unapproved request", err)
	}
	t.Logf("actual signed source admissions=128: closed=60 later_audit=52 failed_headroom=16; exact durable retries=128; unchanged journal rows=144; no chain transaction claimed")
}

// Build the actual 1000-miner plan twice with identical absolute ceilings.
// The correction moves 12.8 test Tao from remaining campaign gas to the existing
// keeper; it cannot increase total gas, Tao, alpha or create another wallet.
func TestEvidenceRelayLaunchPlanFitsUnchangedAbsoluteCaps(t *testing.T) {
	cfg := runtimeEvidenceLaunchConfigTest(t)
	ceiling := configuredPlanLimits(cfg)
	if cfg.MaximumEVMGasWei != DecimalUint("160000000000000000000") || cfg.MaximumTAORao != 200_000_000_000 || cfg.MaximumAlphaRao != 28_250_000_000_000 {
		t.Fatal("test silently expanded the resolved absolute launch ceilings")
	}
	roles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	build := func(slots uint64) *SetupPlan {
		resolved := *cfg
		harness := *cfg.Config
		resolved.Config = &harness
		harness.ValidatorEvidenceRelay.MaxSlots = slots
		resolved.ConfigHash, err = releaseConfigHash(resolved.Config, resolved.Public, resolved.Hyperparameters)
		if err != nil {
			t.Fatal(err)
		}
		plan, err := buildPlan(&resolved, testSetupFacts(), roles, time.Unix(1, 0))
		if err != nil {
			t.Fatalf("complete %d-slot launch plan: %v", slots, err)
		}
		if err := validatePlanBudget(plan); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(plan.Limits, ceiling) || plan.MaximumSpend.EVMGasWei != cfg.MaximumEVMGasWei || plan.MaximumSpend.TAORao > cfg.MaximumTAORao || plan.MaximumSpend.AlphaRao > cfg.MaximumAlphaRao || cfg.Config.Budgets.MaximumRegistrations < 0 || uint64(plan.MaximumSpend.Registrations) > uint64(cfg.Config.Budgets.MaximumRegistrations) || plan.MaximumSpend.SubnetCreations != 0 {
			t.Fatal("relay correction exceeded an original absolute spending cap")
		}
		return plan
	}
	before, after := build(128), build(cfg.Config.ValidatorEvidenceRelay.MaxSlots)
	if cfg.Config.ValidatorEvidenceRelay.MaxSlots != 256 || before.PlanHash == after.PlanHash || len(before.Actions) != len(after.Actions) {
		t.Fatal("finite reserve revision lost its own approval identity or added another funding role")
	}
	find := func(plan *SetupPlan, id string) Action {
		value, err := exactPlanActionByID(plan, id)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	oldReserve, newReserve := find(before, evidenceRelayReserveId), find(after, evidenceRelayReserveId)
	if newReserve.Spend.EVMGasWei != DecimalUint("25600000000000000000") {
		t.Fatal("corrected per-source reserve is not exactly 25.6 test Tao")
	}
	delta, err := subtractDecimalUint(newReserve.Spend.EVMGasWei, oldReserve.Spend.EVMGasWei)
	if err != nil || delta != DecimalUint("12800000000000000000") {
		t.Fatal("source-slot correction changed the finite call-price product", delta, err)
	}
	campaignDelta, err := subtractDecimalUint(find(before, "campaign.evm-gas-reserve").Spend.EVMGasWei, find(after, "campaign.evm-gas-reserve").Spend.EVMGasWei)
	if err != nil || campaignDelta != delta {
		t.Fatal("relay reserve was not subtracted exactly once from existing gas", err)
	}
	oldKeeper, newKeeper := find(before, "evm.fund-keeper"), find(after, "evm.fund-keeper")
	if oldKeeper.Target != newKeeper.Target || newKeeper.Spend.TAORao <= oldKeeper.Spend.TAORao {
		t.Fatal("correction did not fund the same actual keeper")
	}
	if !reflect.DeepEqual(before.Roles, after.Roles) {
		t.Fatal("relay capacity introduced another account")
	}
	for validatorId := 1; validatorId <= 2; validatorId++ {
		for noId := 1; noId <= 2; noId++ {
			action := find(after, runtimeEvidenceActivationActionId(validatorId, noId))
			if action.Spend.EVMGasWei != DecimalUint("100000000000000000") {
				t.Fatal("fixed activation cost was hidden in the dynamic reserve")
			}
		}
	}
	t.Logf("complete launch plan: sources=4 slots=256 relay_gas=%s total_gas=%s tao_rao=%d/%d alpha_rao=%d/%d", newReserve.Spend.EVMGasWei, after.MaximumSpend.EVMGasWei, after.MaximumSpend.TAORao, cfg.MaximumTAORao, after.MaximumSpend.AlphaRao, cfg.MaximumAlphaRao)
}

// A too-small external ceiling remains a plan error; the corrected internal
// reserve must never silently widen it or borrow a new spending authority.
func TestEvidenceRelayLaunchPlanRejectsInsufficientAbsoluteGas(t *testing.T) {
	cfg := runtimeEvidenceLaunchConfigTest(t)
	reserve, err := evidenceRelayMaximumGas(cfg)
	if err != nil {
		t.Fatal(err)
	}
	roles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaximumEVMGasWei = reserve
	if _, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0)); err == nil {
		t.Fatal("relay-only gas ceiling also paid all fixed setup and remaining campaign actions")
	}
	if cfg.MaximumEVMGasWei != reserve || cfg.Config.ValidatorEvidenceRelay.MaxSlots != 256 {
		t.Fatal("failed planning widened its external ceiling or dropped source slots")
	}
}

// Registration approval keeps its signed lower bound and exact uint32
// ceiling. Widening the comparison must not wrap either invalid input.
func TestEvidenceRelayLaunchRegistrationApprovalBounds(t *testing.T) {
	cfg := runtimeEvidenceLaunchConfigTest(t)
	for _, maximum := range []int{-1, 0} {
		harness := *cfg.Config
		harness.Budgets.MaximumRegistrations = maximum
		if err := harness.Validate(); err == nil || !strings.Contains(err.Error(), "registration budget") {
			t.Fatalf("invalid signed registration approval %d escaped actual config admission: %v", maximum, err)
		}
	}
	maximum := uint64(^uint32(0))
	if uint64(^uint(0)>>1) > maximum {
		harness := *cfg.Config
		harness.Budgets.MaximumRegistrations = int(maximum)
		if err := harness.Validate(); err != nil {
			t.Fatal("exact uint32 approval ceiling was refused", err)
		}
		harness.Budgets.MaximumRegistrations = int(maximum + 1)
		if err := harness.Validate(); err == nil || !strings.Contains(err.Error(), "registration budget") {
			t.Fatal("one-over registration approval wrapped into uint32", err)
		}
	}
}
