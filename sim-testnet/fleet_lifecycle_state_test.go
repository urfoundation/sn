package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fleetLifecyclePersistedStateFixture(t *testing.T) (*liveFleetLifecycle, *FleetLifecycleEvidence) {
	t.Helper()
	cfg := testResolvedConfig(t)
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	inputs := make([]FleetLifecyclePruneInput, 10)
	for uid := range inputs {
		var hotkey, coldkey [32]byte
		hotkey[0], hotkey[31] = 0xa1, byte(uid)
		coldkey[0], coldkey[31] = 0xb1, byte(uid)
		inputs[uid] = FleetLifecyclePruneInput{
			UID: uint16(uid), Hotkey: fleetLifecycleHex(hotkey), Coldkey: fleetLifecycleHex(coldkey),
			EmissionRao: 100, RegistrationBlock: uint64(100 + uid), Immune: true,
		}
	}
	inputs[0].Immortal = true
	inputs[1].Immune = false
	inputs[1].EmissionRao = 0
	for _, expected := range []struct {
		churn             int
		uid               uint16
		registrationBlock uint64
	}{{fleetLifecycleTargetChurn, fleetLifecycleTargetExpectedUID, 10}, {fleetLifecycleCompanionChurn, fleetLifecycleCompanionExpectedUID, 20}, {fleetLifecycleTerminalVictimChurn, fleetLifecycleTerminalVictimUID, 30}} {
		hotkey, hotkeyErr := roleBytes32(roles, churnHotkeyLabel(expected.churn))
		coldkey, coldkeyErr := roleBytes32(roles, churnColdkeyLabel(expected.churn))
		if hotkeyErr != nil || coldkeyErr != nil {
			t.Fatal(hotkeyErr, coldkeyErr)
		}
		inputs[expected.uid] = FleetLifecyclePruneInput{
			UID: expected.uid, Hotkey: fleetLifecycleHex(hotkey), Coldkey: fleetLifecycleHex(coldkey),
			RegistrationBlock: expected.registrationBlock, Immune: true,
		}
	}
	launch := FleetLifecyclePruneSnapshot{
		Head: ChainHead{Number: 500, Hash: "0x" + strings.Repeat("ab", 32)}, UIDCount: 10, MaximumUIDs: 10,
		ImmunityPeriodBlocks: 1_000, MinimumNonImmuneUIDs: 10, NonImmuneUIDs: 1,
		RuntimePruneUID: fleetLifecycleTargetExpectedUID, Inputs: inputs,
	}
	if err := validateFleetLifecycleLaunchSnapshot(launch, roles); err != nil {
		t.Fatalf("persisted-state launch fixture: %v", err)
	}
	executor := &Executor{cfg: cfg, plan: &SetupPlan{PlanHash: "plan"}, roles: roles}
	lifecycle := &liveFleetLifecycle{cfg: cfg, stateDir: t.TempDir(), executor: executor}
	evidence := &FleetLifecycleEvidence{
		Schema: fleetLifecycleEvidenceSchema, DeploymentID: cfg.Config.Deployment.DeploymentID,
		PlanHash: "plan", RunID: "run", Stage: fleetLifecycleStageAwaitingDemotion,
		TakeoverEffectiveEpoch: 10, LaunchPrune: &launch,
	}
	return lifecycle, evidence
}

func fleetLifecycleTestHash(seed byte) string {
	return "0x" + strings.Repeat(fmt.Sprintf("%02x", seed), 32)
}

func fleetLifecycleMilestoneStateFixture(t *testing.T, lifecycle *liveFleetLifecycle, nativeEpoch, settlementEpoch, evmSnapshot, nativeSnapshot, application uint64, identities map[uint16]string, rejected []uint16, milestone string) FleetLifecycleCandidateCensus {
	t.Helper()
	observation := fleetLifecycleDecisionFixture(nativeEpoch)
	rejectedSet := uint16Set(rejected)
	for validatorIndex := range observation.Validators {
		decision := &observation.Validators[validatorIndex].HeadDecisions[0]
		for uid, role := range identities {
			hotkey, err := roleBytes32(lifecycle.executor.roles, role)
			if err != nil {
				t.Fatal(err)
			}
			decision.CandidateFleetHotkeys[int(uid)-1] = fleetLifecycleHex(hotkey)
		}
		decision.SettlementEpoch = settlementEpoch
		decision.NativeSnapshot = ChainHead{Number: nativeSnapshot, Hash: fleetLifecycleTestHash(byte(30 + nativeEpoch + uint64(validatorIndex)))}
		decision.EVMSnapshot = ChainHead{Number: evmSnapshot, Hash: fleetLifecycleTestHash(byte(50 + nativeEpoch + uint64(validatorIndex)))}
		decision.FinalizedBlock = application - 20
		decision.FinalizedBlockHash = fleetLifecycleTestHash(byte(70 + nativeEpoch + uint64(validatorIndex)))
		decision.RevealBlock = application - 10
		decision.RevealBlockHash = fleetLifecycleTestHash(byte(90 + nativeEpoch + uint64(validatorIndex)))
		decision.ApplicationBlock = application + uint64(validatorIndex)
		decision.ApplicationBlockHash = fleetLifecycleTestHash(byte(110 + nativeEpoch + uint64(validatorIndex)))
		decision.SelectedHeadUIDs = nil
		decision.RejectedHeadUIDs = append([]uint16(nil), rejected...)
		for weightIndex := range decision.AppliedWeights {
			uid := decision.AppliedWeights[weightIndex].UID
			if rejectedSet[uid] {
				decision.AppliedWeights[weightIndex].Value = 0
			} else {
				decision.AppliedWeights[weightIndex].Value = 1
				decision.SelectedHeadUIDs = append(decision.SelectedHeadUIDs, uid)
			}
		}
	}
	observation.ObservationHash = fleetLifecycleTestHash(byte(130 + nativeEpoch))
	observation.Status.Contracts.CurrentEpoch = settlementEpoch
	observation.Status.Contracts.FinalizedHead = ChainHead{Number: evmSnapshot + 5, Hash: fleetLifecycleTestHash(byte(150 + nativeEpoch))}
	observation.NativeRewards.FinalizedHead = ChainHead{Number: application + 5, Hash: fleetLifecycleTestHash(byte(170 + nativeEpoch))}
	census, ready, err := fleetLifecycleDecisionCensusAt(observation, "release-1.0", nativeEpoch)
	if err != nil || !ready {
		t.Fatalf("build %s milestone ready=%t: %v", milestone, ready, err)
	}
	census.Milestone = milestone
	return census
}

func fleetLifecycleRegistrationStateFixture(t *testing.T, variant string, preBlock, block uint64) *FleetLifecycleRegistrationEvidence {
	t.Helper()
	evidence, _, _, _ := fleetLifecycleRegistrationLineageFixture(t, variant)
	evidence.PrePrune.Head = ChainHead{Number: preBlock, Hash: fleetLifecycleTestHash(byte(preBlock))}
	evidence.PostRegistration.Head = ChainHead{Number: block, Hash: fleetLifecycleTestHash(byte(block))}
	evidence.BlockNumber = block
	evidence.BlockHash = evidence.PostRegistration.Head.Hash
	evidence.TransactionHash = fleetLifecycleTestHash(byte(block + 1))
	for index := range evidence.PostRegistration.Inputs {
		if strings.EqualFold(evidence.PostRegistration.Inputs[index].Hotkey, evidence.ReplacementHotkey) {
			evidence.PostRegistration.Inputs[index].RegistrationBlock = block
		}
	}
	if err := validateFleetLifecycleRegistrationEvidence(evidence); err != nil {
		t.Fatalf("build %s registration: %v", variant, err)
	}
	return &evidence
}

func fleetLifecycleCleanupStateFixture(count int, firstBlock, epoch uint64) []FleetLifecycleCleanupEvidence {
	result := make([]FleetLifecycleCleanupEvidence, count)
	for index := range result {
		block := firstBlock + uint64(index)
		result[index] = FleetLifecycleCleanupEvidence{CleanedAtEpoch: epoch, BlockNumber: block}
	}
	return result
}

func fleetLifecyclePayoutStateFixture(t *testing.T, lifecycle *liveFleetLifecycle, epoch uint64, miners []int, disposition string) FleetLifecyclePayoutEvidence {
	t.Helper()
	clients, err := fleetLifecycleClientIDs(lifecycle.executor.roles, miners)
	if err != nil {
		t.Fatal(err)
	}
	return FleetLifecyclePayoutEvidence{
		Epoch: epoch, NoID: operatorForMiner(lifecycle.cfg, miners[0]), ContentHash: "sha256:" + strings.Repeat("ab", 32),
		PayoutRoot: fleetLifecycleTestHash(byte(epoch)), ClientIDs: clients, Disposition: disposition,
	}
}

func fleetLifecycleReleaseHandoffStateFixture(t *testing.T) (*liveFleetLifecycle, *FleetLifecycleEvidence) {
	t.Helper()
	lifecycle, evidence := fleetLifecyclePersistedStateFixture(t)
	clientMiners := append(fleetLifecycleMembers(lifecycle.cfg, fleetLifecycleTargetFleet), fleetLifecycleMembers(lifecycle.cfg, fleetLifecycleCompanionFleet)...)
	for member := 1; member <= lifecycle.cfg.Config.Topology.ClientsPerHeadFleet; member++ {
		miner, err := fleetLifecycleFallbackMinerIndex(lifecycle.cfg, member)
		if err != nil {
			t.Fatal(err)
		}
		clientMiners = append(clientMiners, miner)
	}
	for _, miner := range clientMiners {
		label := fmt.Sprintf("miner-%d", miner)
		client := lifecycle.executor.roles.Clients[label]
		client.Label = label
		client.ClientIDHex = strings.Repeat("0", 28) + hexUint16(uint16(miner))
		lifecycle.executor.roles.Clients[label] = client
	}
	evidence.Stage = fleetLifecycleStageReleaseHandoff
	evidence.FirstAcceptedEpoch = 101
	evidence.TakeoverEffectiveEpoch = 101
	evidence.AcceptanceStartBlock = 1_000
	evidence.AcceptanceEndBlock = 2_500
	evidence.AcceptanceTerminalBlock = 2_650
	evidence.ReleaseHandoffSchedule = fleetLifecycleNativeScheduleFixture(t, "release-1.0", evidence.AcceptanceStartBlock, evidence.AcceptanceTerminalBlock)
	releaseDeadline, err := fleetLifecycleExpectedEVMEvidenceDeadline(evidence.AcceptanceTerminalBlock, evidence.ReleaseHandoffSchedule)
	if err != nil {
		t.Fatal(err)
	}
	evidence.ReleaseEVMEvidenceDeadlineBlock = releaseDeadline
	evidence.FallbackEffectiveEpoch = 102
	evidence.ProviderEffectiveEpoch = 103
	evidence.TerminalEffectiveEpoch = 105
	evidence.FallbackRegistration = fleetLifecycleRegistrationStateFixture(t, fleetLifecycleVariantFallback, 1_250, 1_300)
	evidence.ProviderRegistration = fleetLifecycleRegistrationStateFixture(t, fleetLifecycleVariantProvider, 1_850, 1_900)
	evidence.TerminalRegistration = fleetLifecycleRegistrationStateFixture(t, fleetLifecycleVariantTerminal, 2_450, 2_500)
	evidence.TargetCleanup = fleetLifecycleCleanupStateFixture(lifecycle.cfg.Config.Topology.ClientsPerHeadFleet, 1_851, 102)
	evidence.CompanionCleanup = fleetLifecycleCleanupStateFixture(lifecycle.cfg.Config.Topology.ClientsPerHeadFleet, 2_451, 104)
	evidence.FallbackCleanup = fleetLifecycleCleanupStateFixture(lifecycle.cfg.Config.Topology.ClientsPerHeadFleet, 2_461, 104)
	evidence.PostRegistrationRewardBaseline = ChainHead{Number: 2_000, Hash: fleetLifecycleTestHash(201)}
	evidence.CandidateCensuses = []FleetLifecycleCandidateCensus{
		fleetLifecycleMilestoneStateFixture(t, lifecycle, 1, 101, 1_100, 1_150, 1_200, map[uint16]string{7: churnHotkeyLabel(6), 8: churnHotkeyLabel(7)}, []uint16{7, 8}, fleetLifecycleMilestoneTakeoverRejected),
		fleetLifecycleMilestoneStateFixture(t, lifecycle, 2, 102, 1_400, 1_700, 1_800, map[uint16]string{7: churnHotkeyLabel(1), 8: churnHotkeyLabel(7)}, []uint16{7, 8}, fleetLifecycleMilestoneFallbackActive),
		fleetLifecycleMilestoneStateFixture(t, lifecycle, 3, 104, 2_000, 2_300, 2_400, map[uint16]string{7: churnHotkeyLabel(1), 8: churnHotkeyLabel(6)}, []uint16{7, 8}, fleetLifecycleMilestoneProviderActive),
	}
	fallbackMembers, err := lifecycle.fallbackMembers()
	if err != nil {
		t.Fatal(err)
	}
	evidence.Payouts = []FleetLifecyclePayoutEvidence{
		fleetLifecyclePayoutStateFixture(t, lifecycle, 102, fleetLifecycleMembers(lifecycle.cfg, fleetLifecycleTargetFleet), "pruned-provider-returned-to-operator-pool"),
		fleetLifecyclePayoutStateFixture(t, lifecycle, 102, fallbackMembers, "fallback-provider-head-excluded"),
		fleetLifecyclePayoutStateFixture(t, lifecycle, 103, fleetLifecycleMembers(lifecycle.cfg, fleetLifecycleTargetFleet), "reregistered-provider-head-excluded"),
		fleetLifecyclePayoutStateFixture(t, lifecycle, 103, fleetLifecycleMembers(lifecycle.cfg, fleetLifecycleCompanionFleet), "second-pruned-provider-returned-to-operator-pool"),
	}
	lifecycle.evidence = evidence
	if err := lifecycle.validatePersistedStateForPhase("release-1.0", evidence.RunID, evidence); err != nil {
		t.Fatalf("release handoff fixture: %v", err)
	}
	return lifecycle, evidence
}

func TestFleetLifecyclePersistedStateBindsExactRunAndRejectsFutureWave(t *testing.T) {
	lifecycle, evidence := fleetLifecyclePersistedStateFixture(t)
	if err := lifecycle.validatePersistedState("run", evidence); err != nil {
		t.Fatalf("exact awaiting state: %v", err)
	}
	if err := lifecycle.validatePersistedState("foreign-run", evidence); err == nil {
		t.Fatal("lifecycle state from another run was accepted")
	}
	evidence.FallbackEffectiveEpoch = 11
	if err := lifecycle.validatePersistedState("run", evidence); err == nil {
		t.Fatal("pre-acceptance lifecycle state carrying a future fallback wave was accepted")
	}
}

func TestFleetLifecycleAuthenticatesCanonicalReleaseHandoffBytes(t *testing.T) {
	lifecycle, evidence := fleetLifecycleReleaseHandoffStateFixture(t)
	encoded, err := fleetLifecycleCanonicalBytes(evidence)
	if err != nil {
		t.Fatal(err)
	}
	hash := bytesSHA256(encoded)
	if err := lifecycle.AuthenticateReleaseHandoff(encoded, hash, evidence.RunID); err != nil {
		t.Fatal(err)
	}
	if lifecycle.authenticatedReleaseHandoff == evidence || lifecycle.authenticatedReleaseHandoffHash != hash || !fleetLifecycleCanonicalEqual(lifecycle.authenticatedReleaseHandoff, evidence) {
		t.Fatal("release handoff authentication did not retain an independent exact canonical copy")
	}
}

func TestFleetLifecycleReleaseHandoffRejectsByteOrHashDrift(t *testing.T) {
	lifecycle, evidence := fleetLifecycleReleaseHandoffStateFixture(t)
	encoded, err := fleetLifecycleCanonicalBytes(evidence)
	if err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.AuthenticateReleaseHandoff(encoded, "sha256:"+strings.Repeat("00", 32), evidence.RunID); err == nil {
		t.Fatal("release handoff accepted a foreign authenticated hash")
	}
	drifted := append(append([]byte(nil), encoded...), ' ')
	if err := lifecycle.AuthenticateReleaseHandoff(drifted, bytesSHA256(drifted), evidence.RunID); err == nil {
		t.Fatal("release handoff accepted noncanonical bytes under their own hash")
	}
	if lifecycle.authenticatedReleaseHandoff != nil || lifecycle.authenticatedReleaseHandoffHash != "" {
		t.Fatal("failed handoff authentication retained an earlier trusted predecessor")
	}
}

func TestFleetLifecycleProductionAdoptsOnlyExactReleaseSuccessor(t *testing.T) {
	lifecycle, evidence := fleetLifecycleReleaseHandoffStateFixture(t)
	if err := os.MkdirAll(filepath.Join(lifecycle.stateDir, "public"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writePublicJSON(filepath.Join(lifecycle.stateDir, "public", "fleet-lifecycle.json"), evidence); err != nil {
		t.Fatal(err)
	}
	encoded, err := fleetLifecycleCanonicalBytes(evidence)
	if err != nil {
		t.Fatal(err)
	}
	hash := bytesSHA256(encoded)
	if err := lifecycle.AuthenticateReleaseHandoff(encoded, hash, evidence.RunID); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.BeginPhase("production-soak", "production-run"); err != nil {
		t.Fatal(err)
	}
	if lifecycle.evidence.ProductionRunID != "production-run" || lifecycle.evidence.ReleaseHandoffHash != hash || !fleetLifecycleCanonicalEqual(fleetLifecycleReleaseProjection(lifecycle.evidence), evidence) {
		t.Fatalf("production successor was not exactly bound: %+v", lifecycle.evidence)
	}

	retry := &liveFleetLifecycle{cfg: lifecycle.cfg, stateDir: lifecycle.stateDir, executor: lifecycle.executor}
	if err := retry.AuthenticateReleaseHandoff(encoded, hash, evidence.RunID); err != nil {
		t.Fatal(err)
	}
	if err := retry.BeginPhase("production-soak", "production-run"); err != nil {
		t.Fatalf("same production attempt could not resume: %v", err)
	}
	if err := retry.BeginPhase("production-soak", "foreign-production-run"); err == nil {
		t.Fatal("production successor was rebound to a fresh run identity")
	}
}

func TestFleetLifecycleProductionAdvancePersistsAndResumesDistinctEVMEvidenceDeadline(t *testing.T) {
	lifecycle, release := fleetLifecycleReleaseHandoffStateFixture(t)
	if err := os.MkdirAll(filepath.Join(lifecycle.stateDir, "public"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writePublicJSON(filepath.Join(lifecycle.stateDir, "public", "fleet-lifecycle.json"), release); err != nil {
		t.Fatal(err)
	}
	encoded, err := fleetLifecycleCanonicalBytes(release)
	if err != nil {
		t.Fatal(err)
	}
	hash := bytesSHA256(encoded)
	if err := lifecycle.AuthenticateReleaseHandoff(encoded, hash, release.RunID); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.BeginPhase("production-soak", "production-run"); err != nil {
		t.Fatal(err)
	}
	window := &ScenarioAcceptanceWindow{EpochCount: 3, EpochBlocks: 360, FinalizeOffsetBlocks: 180, FirstEpoch: release.TerminalEffectiveEpoch, StartBlock: 5_000, EndBlock: 6_080, TerminalBlock: 6_260}
	if err := lifecycle.BindAcceptanceWindowForPhase("production-soak", window); err != nil {
		t.Fatal(err)
	}
	schedule := fleetLifecycleNativeScheduleFixture(t, "production-soak", window.StartBlock, window.TerminalBlock)
	deadline, err := fleetLifecycleExpectedEVMEvidenceDeadline(window.TerminalBlock, schedule)
	if err != nil {
		t.Fatal(err)
	}
	if schedule.ApplicationDeadlineBlock >= window.TerminalBlock || deadline != window.TerminalBlock {
		t.Fatalf("production native/EVM deadlines=%d/%d, want native below terminal %d and EVM at terminal", schedule.ApplicationDeadlineBlock, deadline, window.TerminalBlock)
	}
	lifecycle.evidence.ProductionNativeSchedule = schedule
	lifecycle.evidence.ProductionEVMEvidenceDeadlineBlock = deadline
	// The test isolates the schedule persistence boundary; immutable on-chain
	// lineage replay is covered separately and has already succeeded for this
	// fixture before a production poll may run.
	lifecycle.resumeValidated = true
	observation := &ScenarioObservation{Status: &DeploymentStatus{Contracts: &ContractView{}}}
	if err := lifecycle.Advance(context.Background(), observation, nil); err != nil {
		t.Fatalf("production advance with distinct EVM/native bounds: %v", err)
	}
	persisted, err := loadFleetLifecycleEvidence(lifecycle.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.ProductionEVMEvidenceDeadlineBlock != window.TerminalBlock || persisted.ProductionNativeSchedule == nil || persisted.ProductionNativeSchedule.ApplicationDeadlineBlock != schedule.ApplicationDeadlineBlock {
		t.Fatalf("persisted production deadlines=%d/%+v", persisted.ProductionEVMEvidenceDeadlineBlock, persisted.ProductionNativeSchedule)
	}

	retry := &liveFleetLifecycle{cfg: lifecycle.cfg, stateDir: lifecycle.stateDir, executor: lifecycle.executor}
	if err := retry.AuthenticateReleaseHandoff(encoded, hash, release.RunID); err != nil {
		t.Fatal(err)
	}
	if err := retry.BeginPhase("production-soak", "production-run"); err != nil {
		t.Fatalf("production schedule could not resume after persistence: %v", err)
	}
	for _, drift := range []uint64{schedule.ApplicationDeadlineBlock, deadline - 1, deadline + 1} {
		candidate := *persisted
		candidate.ProductionEVMEvidenceDeadlineBlock = drift
		if err := retry.validatePersistedStateForPhase("production-soak", candidate.ProductionRunID, &candidate); err == nil {
			t.Fatalf("off-by-one production EVM deadline %d was accepted", drift)
		}
	}
}

func TestFleetLifecycleProductionRejectsReleaseProjectionDrift(t *testing.T) {
	lifecycle, evidence := fleetLifecycleReleaseHandoffStateFixture(t)
	if err := os.MkdirAll(filepath.Join(lifecycle.stateDir, "public"), 0o700); err != nil {
		t.Fatal(err)
	}
	encoded, err := fleetLifecycleCanonicalBytes(evidence)
	if err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.AuthenticateReleaseHandoff(encoded, bytesSHA256(encoded), evidence.RunID); err != nil {
		t.Fatal(err)
	}
	drift := *evidence
	drift.FallbackEffectiveEpoch++
	if err := writePublicJSON(filepath.Join(lifecycle.stateDir, "public", "fleet-lifecycle.json"), &drift); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.BeginPhase("production-soak", "production-run"); err == nil {
		t.Fatal("production adopted a lifecycle whose release fields drifted from the signed handoff")
	}
}

func TestFleetLifecycleProductionRejectsInternallyRehashedReleaseDrift(t *testing.T) {
	lifecycle, evidence := fleetLifecycleReleaseHandoffStateFixture(t)
	encoded, err := fleetLifecycleCanonicalBytes(evidence)
	if err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.AuthenticateReleaseHandoff(encoded, bytesSHA256(encoded), evidence.RunID); err != nil {
		t.Fatal(err)
	}
	successor := *evidence
	successor.ProductionRunID = "production-run"
	successor.Payouts = append([]FleetLifecyclePayoutEvidence(nil), evidence.Payouts...)
	successor.Payouts[0].PayoutRoot = fleetLifecycleTestHash(243)
	successor.ReleaseHandoffHash, err = fleetLifecycleReleaseHandoffHash(&successor)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle.evidence = &successor
	if err := lifecycle.validatePersistedStateForPhase("production-soak", successor.ProductionRunID, &successor); err == nil {
		t.Fatal("production accepted release drift after an attacker recomputed its internally consistent projection hash")
	}
}

func TestFleetLifecycleProductionRequiresPriorHandoffAuthentication(t *testing.T) {
	lifecycle, evidence := fleetLifecycleReleaseHandoffStateFixture(t)
	if err := os.MkdirAll(filepath.Join(lifecycle.stateDir, "public"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writePublicJSON(filepath.Join(lifecycle.stateDir, "public", "fleet-lifecycle.json"), evidence); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.BeginPhase("production-soak", "production-run"); err == nil {
		t.Fatal("production adopted release lifecycle state before authenticating the immutable predecessor")
	}
}

func TestFleetLifecycleReleaseProjectionStripsOnlyProductionExtension(t *testing.T) {
	_, release := fleetLifecycleReleaseHandoffStateFixture(t)
	successor := *release
	successor.Payouts = append([]FleetLifecyclePayoutEvidence(nil), release.Payouts...)
	successor.Stage = fleetLifecycleStageComplete
	successor.ProductionRunID = "production-run"
	successor.ReleaseHandoffHash = "sha256:" + strings.Repeat("cc", 32)
	successor.ProductionFirstSettlementEpoch = 200
	successor.ProductionAcceptanceStartBlock = 4_000
	successor.ProductionAcceptanceEndBlock = 5_080
	successor.ProductionAcceptanceTerminalBlock = 5_260
	successor.ProductionNativeSchedule = fleetLifecycleNativeScheduleFixture(t, "production-soak", 4_000, 5_260)
	production := successor.CandidateCensuses[len(successor.CandidateCensuses)-1]
	production.Phase = "production-soak"
	production.Milestone = fleetLifecycleMilestoneTerminalActive
	successor.CandidateCensuses = append(successor.CandidateCensuses, production)
	if !fleetLifecycleCanonicalEqual(fleetLifecycleReleaseProjection(&successor), release) {
		t.Fatal("append-only production fields changed the projected release handoff")
	}
	successor.Payouts[0].PayoutRoot = fleetLifecycleTestHash(244)
	if fleetLifecycleCanonicalEqual(fleetLifecycleReleaseProjection(&successor), release) {
		t.Fatal("release evidence mutation was hidden by the production projection")
	}
}

func TestFleetLifecyclePersistedStateRejectsShiftedAcceptedEpoch(t *testing.T) {
	lifecycle, evidence := fleetLifecyclePersistedStateFixture(t)
	evidence.FirstAcceptedEpoch = 11
	if err := lifecycle.validatePersistedState("run", evidence); err == nil {
		t.Fatal("persisted lifecycle state shifted away from the generation-3 takeover epoch")
	}
}

func TestFleetLifecycleLaunchSnapshotRejectsAlreadyConsumedRoleBecomingLive(t *testing.T) {
	lifecycle, evidence := fleetLifecyclePersistedStateFixture(t)
	hotkey, err := roleBytes32(lifecycle.executor.roles, churnHotkeyLabel(1))
	if err != nil {
		t.Fatal(err)
	}
	coldkey, err := roleBytes32(lifecycle.executor.roles, churnColdkeyLabel(1))
	if err != nil {
		t.Fatal(err)
	}
	evidence.LaunchPrune.Inputs[2].Hotkey = fleetLifecycleHex(hotkey)
	evidence.LaunchPrune.Inputs[2].Coldkey = fleetLifecycleHex(coldkey)
	if err := validateFleetLifecycleLaunchSnapshot(*evidence.LaunchPrune, lifecycle.executor.roles); err == nil {
		t.Fatal("launch snapshot accepted already-consumed churn-1 as live")
	}
}

func TestFleetLifecycleLaunchSnapshotRejectsShiftedTargetUID(t *testing.T) {
	lifecycle, evidence := fleetLifecyclePersistedStateFixture(t)
	left, right := &evidence.LaunchPrune.Inputs[6], &evidence.LaunchPrune.Inputs[7]
	left.Hotkey, right.Hotkey = right.Hotkey, left.Hotkey
	left.Coldkey, right.Coldkey = right.Coldkey, left.Coldkey
	left.RegistrationBlock, right.RegistrationBlock = right.RegistrationBlock, left.RegistrationBlock
	left.EmissionRao, right.EmissionRao = right.EmissionRao, left.EmissionRao
	evidence.LaunchPrune.RuntimePruneUID = 6
	if err := validateFleetLifecycleLaunchSnapshot(*evidence.LaunchPrune, lifecycle.executor.roles); err == nil {
		t.Fatal("launch snapshot accepted churn-6 at a foreign UID")
	}
}

func TestFleetLifecycleLaunchSnapshotRejectsTargetOwnerDrift(t *testing.T) {
	lifecycle, evidence := fleetLifecyclePersistedStateFixture(t)
	evidence.LaunchPrune.Inputs[fleetLifecycleTargetExpectedUID].Coldkey = "0x" + strings.Repeat("cd", 32)
	if err := validateFleetLifecycleLaunchSnapshot(*evidence.LaunchPrune, lifecycle.executor.roles); err == nil {
		t.Fatal("launch snapshot accepted churn-6 under a foreign coldkey")
	}
}

func TestFleetLifecycleLaunchSnapshotProvesMinimumFloorProtectsUIDOne(t *testing.T) {
	lifecycle, evidence := fleetLifecyclePersistedStateFixture(t)
	launch := *evidence.LaunchPrune
	if launch.Inputs[1].Immune || launch.Inputs[1].Immortal || launch.Inputs[1].EmissionRao != 0 || launch.NonImmuneUIDs != 1 || launch.MinimumNonImmuneUIDs != 10 || launch.RuntimePruneUID != fleetLifecycleTargetExpectedUID {
		t.Fatalf("minimum-floor fixture does not model the live runtime boundary: %+v", launch)
	}
	if err := validateFleetLifecycleLaunchSnapshot(launch, lifecycle.executor.roles); err != nil {
		t.Fatalf("minimum floor did not protect the only non-immune UID: %v", err)
	}
}

func TestFleetLifecycleLaunchSnapshotDefersInternallyConsistentForeignPruneCandidate(t *testing.T) {
	lifecycle, evidence := fleetLifecyclePersistedStateFixture(t)
	launch := *evidence.LaunchPrune
	launch.Inputs = append([]FleetLifecyclePruneInput(nil), launch.Inputs...)
	launch.Inputs[2].Immune = false
	launch.NonImmuneUIDs = 2
	launch.MinimumNonImmuneUIDs = 1
	launch.RuntimePruneUID = 1
	err := validateFleetLifecycleLaunchSnapshot(launch, lifecycle.executor.roles)
	var pending *fleetLifecyclePruneTargetPendingError
	if !errors.As(err, &pending) || pending.computedUid != 1 || pending.target.UID != fleetLifecycleTargetExpectedUID {
		t.Fatalf("valid pre-target prune census was not deferred: %v", err)
	}
}

func TestFleetLifecycleContextRetriesPendingPruneWithoutReplayingCampaign(t *testing.T) {
	attempts, waits := 0, 0
	pending := &fleetLifecyclePruneTargetPendingError{target: FleetLifecyclePruneInput{UID: fleetLifecycleTargetExpectedUID}, computedUid: 1, recordedUid: 1}
	err := retryFleetLifecyclePhaseContext(t.Context(), func() error {
		attempts++
		if attempts == 1 {
			return pending
		}
		return nil
	}, func(context.Context) error {
		waits++
		return nil
	})
	if err != nil || attempts != 2 || waits != 1 {
		t.Fatalf("contextual retry error=%v attempts=%d waits=%d", err, attempts, waits)
	}
}

// Reproduces an aged full subnet where the runtime would prune a different
// nonimmune UID, while preserving the exact census and sending no mutation.
func fleetLifecycleProvisionalBypassFixture(t *testing.T) (*liveFleetLifecycle, *FleetLifecycleEvidence) {
	t.Helper()
	lifecycle, evidence := fleetLifecyclePersistedStateFixture(t)
	planHash := "0x" + strings.Repeat("42", 32)
	lifecycle.executor.plan.PlanHash = planHash
	evidence.PlanHash = planHash
	lifecycle.cfg.provisionalResume = &provisionalResumeState{
		RecordPath: filepath.Join(t.TempDir(), "provenance.json"),
		Record: &provisionalResumeRecord{
			Schema: "urnetwork-sim-provisional-resume-v1", Provisional: true, FinalAcceptance: false,
			ConfigHash: lifecycle.cfg.ConfigHash, DeploymentID: lifecycle.cfg.Config.Deployment.DeploymentID, PlanHash: planHash,
		},
	}
	launch := *evidence.LaunchPrune
	launch.Inputs = append([]FleetLifecyclePruneInput(nil), launch.Inputs...)
	launch.MinimumNonImmuneUIDs = 1
	launch.NonImmuneUIDs = uint16(len(launch.Inputs) - 1)
	for index := 1; index < len(launch.Inputs); index++ {
		launch.Inputs[index].Immune = false
		launch.Inputs[index].EmissionRao = 100
	}
	launch.Inputs[1].EmissionRao = 0
	launch.RuntimePruneUID = 1
	evidence.LaunchPrune = &launch
	bypass, err := fleetLifecycleProvisionalBypass(lifecycle.cfg, lifecycle.executor.plan, lifecycle.executor.roles, launch)
	if err != nil {
		t.Fatal(err)
	}
	evidence.Stage = fleetLifecycleStageReleaseHandoff
	evidence.ProvisionalBypass = bypass
	lifecycle.evidence = evidence
	lifecycle.phase = "release-1.0"
	lifecycle.resumeValidated = true
	return lifecycle, evidence
}

// Models the deployed failure: the public lifecycle handoff belongs to the
// signed root release while a later pre-acceptance recovery owns the new run.
func fleetLifecycleProvisionalRecoveryFixture(t *testing.T) (*liveFleetLifecycle, *FleetLifecycleEvidence, *scenarioCampaignAttempt, []byte) {
	t.Helper()
	fixture := newCampaignSuccessionFixture(t)
	root, _ := bindCampaignRecoveryFixture(t, fixture)
	first, err := loadOrCreateScenarioCampaignAttempt(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, "release-1.0", nil, fixture.now.Add(time.Hour), fixture.journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.updateProgress(false, true); err != nil {
		t.Fatal(err)
	}
	_ = bindPreAcceptanceFailedRecoveryGeneration(t, fixture, first)
	current, err := loadOrCreateScenarioCampaignAttempt(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, "release-1.0", nil, fixture.now.Add(2*time.Hour), fixture.journal)
	if err != nil {
		t.Fatal(err)
	}

	lifecycle, evidence := fleetLifecycleProvisionalBypassFixture(t)
	fixture.cfg.provisionalResume = &provisionalResumeState{
		RecordPath: filepath.Join(t.TempDir(), "provenance.json"),
		Record: &provisionalResumeRecord{
			Schema: "urnetwork-sim-provisional-resume-v1", Provisional: true, FinalAcceptance: false,
			ConfigHash: fixture.cfg.ConfigHash, DeploymentID: fixture.cfg.Config.Deployment.DeploymentID, PlanHash: fixture.current.PlanHash,
		},
	}
	lifecycle.cfg = fixture.cfg
	lifecycle.stateDir = fixture.stateDir
	lifecycle.executor = &Executor{cfg: fixture.cfg, stateDir: fixture.stateDir, plan: fixture.current, roles: fixture.roles}
	lifecycle.attempt = current
	evidence.DeploymentID = fixture.cfg.Config.Deployment.DeploymentID
	evidence.PlanHash = fixture.current.PlanHash
	evidence.RunID = root.payload.RunID
	evidence.Renewal = cloneFleetLifecycleRenewal(fixture.current.FleetLifecycleRenewal)
	for _, identity := range []struct {
		uid   uint16
		churn int
	}{{fleetLifecycleTargetExpectedUID, fleetLifecycleTargetChurn}, {fleetLifecycleCompanionExpectedUID, fleetLifecycleCompanionChurn}, {fleetLifecycleTerminalVictimUID, fleetLifecycleTerminalVictimChurn}} {
		hotkey, hotkeyErr := roleBytes32(fixture.roles, churnHotkeyLabel(identity.churn))
		coldkey, coldkeyErr := roleBytes32(fixture.roles, churnColdkeyLabel(identity.churn))
		if hotkeyErr != nil || coldkeyErr != nil {
			t.Fatal(hotkeyErr, coldkeyErr)
		}
		evidence.LaunchPrune.Inputs[identity.uid].Hotkey = fleetLifecycleHex(hotkey)
		evidence.LaunchPrune.Inputs[identity.uid].Coldkey = fleetLifecycleHex(coldkey)
	}
	evidence.FirstAcceptedEpoch = 101
	evidence.AcceptanceStartBlock = 1_000
	evidence.AcceptanceEndBlock = 2_500
	evidence.AcceptanceTerminalBlock = 2_650
	bypass, err := fleetLifecycleProvisionalBypass(fixture.cfg, fixture.current, fixture.roles, *evidence.LaunchPrune)
	if err != nil {
		t.Fatal(err)
	}
	evidence.ProvisionalBypass = bypass
	if err := os.MkdirAll(filepath.Join(fixture.stateDir, "public"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writePublicJSON(filepath.Join(fixture.stateDir, "public", "fleet-lifecycle.json"), evidence); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(fixture.stateDir, "public", "fleet-lifecycle.json"))
	if err != nil {
		t.Fatal(err)
	}
	return lifecycle, evidence, current, raw
}

func TestFleetLifecycleProvisionalRecoveryRetainsAuthenticatedReleaseHandoff(t *testing.T) {
	lifecycle, evidence, current, before := fleetLifecycleProvisionalRecoveryFixture(t)
	if !current.payload.PreparationComplete {
		t.Fatal("recovery fixture did not carry its signed preparation checkpoint")
	}
	if err := lifecycle.ValidatePhaseResume("release-1.0", current.payload.RunID); err != nil {
		t.Fatalf("preparation-time resume validation: %v", err)
	}
	if err := lifecycle.BeginPhase("release-1.0", current.payload.RunID); err != nil {
		t.Fatalf("begin retained release lifecycle: %v", err)
	}
	after, err := os.ReadFile(filepath.Join(lifecycle.stateDir, "public", "fleet-lifecycle.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) || lifecycle.evidence.RunID != evidence.RunID || lifecycle.evidence.RunID == current.payload.RunID || !lifecycle.retainedProvisionalRelease || !lifecycle.Complete() {
		t.Fatalf("retained lifecycle changed: retained=%t evidence_run=%q current_run=%q", lifecycle.retainedProvisionalRelease, lifecycle.evidence.RunID, current.payload.RunID)
	}
	window, present := lifecycle.RetainedAcceptanceWindowForPhase("release-1.0")
	if !present || window.FirstEpoch != evidence.FirstAcceptedEpoch || window.StartBlock != evidence.AcceptanceStartBlock || window.TerminalBlock != evidence.AcceptanceTerminalBlock {
		t.Fatalf("retained acceptance window=%+v present=%t", window, present)
	}
	if err := lifecycle.BindAcceptanceWindowForPhase("release-1.0", window); err != nil {
		t.Fatal(err)
	}
	changed := *window
	changed.StartBlock++
	if err := lifecycle.BindAcceptanceWindowForPhase("release-1.0", &changed); err == nil || !strings.Contains(err.Error(), "differs from its authenticated predecessor") {
		t.Fatalf("changed retained window error=%v", err)
	}
	if err := lifecycle.Advance(t.Context(), &ScenarioObservation{Status: &DeploymentStatus{Contracts: &ContractView{}}}, nil); err != nil {
		t.Fatal(err)
	}
	finalBytes, err := os.ReadFile(filepath.Join(lifecycle.stateDir, "public", "fleet-lifecycle.json"))
	if err != nil || !bytes.Equal(before, finalBytes) {
		t.Fatalf("retained lifecycle bytes changed after no-op advancement: err=%v", err)
	}
}

func TestFleetLifecycleProvisionalRecoveryArchiveBindsBothRunIdentities(t *testing.T) {
	lifecycle, evidence, current, original := fleetLifecycleProvisionalRecoveryFixture(t)
	if err := lifecycle.BeginPhase("release-1.0", current.payload.RunID); err != nil {
		t.Fatal(err)
	}
	runDir := t.TempDir()
	binding, err := captureScenarioLifecycleHandoff(lifecycle.cfg, lifecycle.stateDir, runDir, current.payload.RunID, current)
	if err != nil {
		t.Fatal(err)
	}
	copied, err := os.ReadFile(filepath.Join(runDir, scenarioLifecycleHandoffFilename))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(original, copied) || binding.ReleaseRunID != current.payload.RunID || binding.CurrentRunID != current.payload.RunID || binding.InheritedReleaseRunID != evidence.RunID || binding.PlanHash != evidence.PlanHash || binding.ConfigHash != lifecycle.cfg.ConfigHash || binding.PolicyHash != lifecycle.cfg.PolicyHash {
		t.Fatalf("inherited lifecycle binding=%+v", binding)
	}
	if err := validateScenarioLifecycleHandoffBinding(lifecycle.cfg, *binding, copied); err != nil {
		t.Fatal(err)
	}
	gate := &ReleaseCampaignGate{
		Schema: releaseCampaignGateSchema, RunID: current.payload.RunID,
		ResultHash: "0x" + strings.Repeat("71", 32), CompleteContentHash: "sha256:" + strings.Repeat("72", 32),
		StartEpoch: 101, EndEpoch: 106, LifecycleHandoff: *binding,
	}
	if err := validateReleaseCampaignGateShape(lifecycle.cfg, gate); err != nil {
		t.Fatalf("inherited lifecycle release gate: %v", err)
	}
	strict := *lifecycle.cfg
	strict.provisionalResume = nil
	if err := validateScenarioLifecycleHandoffBinding(&strict, *binding, copied); err == nil {
		t.Fatal("strict lifecycle binding accepted provisional inherited provenance")
	}
	changed := *binding
	changed.InheritedReleaseRunID = "foreign-release"
	if err := validateScenarioLifecycleHandoffBinding(lifecycle.cfg, changed, copied); err == nil {
		t.Fatal("lifecycle binding accepted changed inherited run identity")
	}
	changed = *binding
	changed.PlanHash = "0x" + strings.Repeat("73", 32)
	if err := validateScenarioLifecycleHandoffBinding(lifecycle.cfg, changed, copied); err == nil {
		t.Fatal("lifecycle binding accepted changed recovery plan")
	}
	if _, err := captureScenarioLifecycleHandoff(lifecycle.cfg, lifecycle.stateDir, t.TempDir(), current.payload.RunID, nil); err == nil {
		t.Fatal("lifecycle archive accepted inherited bytes without signed recovery ancestry")
	}
}

func TestFleetLifecycleProvisionalRecoveryRejectsForeignAndMutatedPredecessors(t *testing.T) {
	lifecycle, evidence, current, _ := fleetLifecycleProvisionalRecoveryFixture(t)
	foreign := *current
	foreign.payload = current.payload
	foreign.payload.RunID = "foreign-recovery-run"
	lifecycle.attempt = &foreign
	if err := lifecycle.BeginPhase("release-1.0", foreign.payload.RunID); err == nil {
		t.Fatal("foreign unsigned recovery adopted the retained release handoff")
	}
	lifecycle.attempt = current
	mutated := *evidence
	mutated.Stage = fleetLifecycleStageComplete
	if err := writePublicJSON(filepath.Join(lifecycle.stateDir, "public", "fleet-lifecycle.json"), &mutated); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.BeginPhase("release-1.0", current.payload.RunID); err == nil || !strings.Contains(err.Error(), "handoff stage") {
		t.Fatalf("completed-stage predecessor error=%v", err)
	}
	mutated = *evidence
	mutated.ProductionRunID = "production-run"
	if err := writePublicJSON(filepath.Join(lifecycle.stateDir, "public", "fleet-lifecycle.json"), &mutated); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.BeginPhase("release-1.0", current.payload.RunID); err == nil || !strings.Contains(err.Error(), "production successor state") {
		t.Fatalf("production-state predecessor error=%v", err)
	}
}

func TestFleetLifecycleProvisionalBypassPreservesUnsafePruneCensusWithoutMutation(t *testing.T) {
	lifecycle, evidence := fleetLifecycleProvisionalBypassFixture(t)
	if err := lifecycle.validatePersistedStateForPhase("release-1.0", evidence.RunID, evidence); err != nil {
		t.Fatal(err)
	}
	if passed, detail := lifecycle.CompletionStatus(); !passed || !strings.Contains(detail, "final_acceptance=false") {
		t.Fatalf("provisional completion passed=%t detail=%q", passed, detail)
	}
	window := &ScenarioAcceptanceWindow{FirstEpoch: 101, EpochCount: 5, EpochBlocks: 300, StartBlock: 1_000, EndBlock: 2_500, FinalizeOffsetBlocks: 150, TerminalBlock: 2_650}
	if err := lifecycle.BindAcceptanceWindowForPhase("release-1.0", window); err != nil {
		t.Fatal(err)
	}
	if lifecycle.Complete() {
		t.Fatal("window mutation remained validated without an evidence pass")
	}
	observation := &ScenarioObservation{Status: &DeploymentStatus{Contracts: &ContractView{}}}
	if err := lifecycle.Advance(t.Context(), observation, nil); err != nil {
		t.Fatal(err)
	}
	if !lifecycle.Complete() || evidence.FallbackRegistration != nil || evidence.ProviderRegistration != nil || evidence.TerminalRegistration != nil || len(evidence.TargetCleanup) != 0 || len(evidence.CompanionCleanup) != 0 || len(evidence.FallbackCleanup) != 0 {
		t.Fatal("provisional lifecycle bypass performed or claimed a mutation")
	}
}

func TestFleetLifecycleProvisionalBypassCannotAuthorizeStrictOrChangedEvidence(t *testing.T) {
	lifecycle, evidence := fleetLifecycleProvisionalBypassFixture(t)
	lifecycle.cfg.provisionalResume = nil
	if err := lifecycle.validatePersistedStateForPhase("release-1.0", evidence.RunID, evidence); err == nil {
		t.Fatal("strict lifecycle accepted a provisional registration bypass")
	}
	lifecycle.cfg.provisionalResume = &provisionalResumeState{
		RecordPath: filepath.Join(t.TempDir(), "provenance.json"),
		Record: &provisionalResumeRecord{
			Schema: "urnetwork-sim-provisional-resume-v1", Provisional: true,
			ConfigHash: lifecycle.cfg.ConfigHash, DeploymentID: lifecycle.cfg.Config.Deployment.DeploymentID, PlanHash: lifecycle.executor.plan.PlanHash,
		},
	}
	evidence.ProvisionalBypass.RuntimePruneUid++
	if err := lifecycle.validatePersistedStateForPhase("release-1.0", evidence.RunID, evidence); err == nil {
		t.Fatal("changed runtime prune candidate retained bypass authority")
	}
}

func TestFleetLifecycleProvisionalBypassCarriesExactReleaseHandoffIntoProduction(t *testing.T) {
	lifecycle, evidence := fleetLifecycleProvisionalBypassFixture(t)
	window := &ScenarioAcceptanceWindow{FirstEpoch: 101, EpochCount: 5, EpochBlocks: 300, StartBlock: 1_000, EndBlock: 2_500, FinalizeOffsetBlocks: 150, TerminalBlock: 2_650}
	if err := lifecycle.BindAcceptanceWindowForPhase("release-1.0", window); err != nil {
		t.Fatal(err)
	}
	lifecycle.resumeValidated = true
	encoded, err := fleetLifecycleCanonicalBytes(evidence)
	if err != nil {
		t.Fatal(err)
	}
	hash := bytesSHA256(encoded)
	if err := lifecycle.AuthenticateReleaseHandoff(encoded, hash, evidence.RunID); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.BeginPhase("production-soak", "production-run"); err != nil {
		t.Fatal(err)
	}
	production := &ScenarioAcceptanceWindow{FirstEpoch: 106, EpochCount: 3, EpochBlocks: 360, StartBlock: 3_000, EndBlock: 4_080, FinalizeOffsetBlocks: 180, TerminalBlock: 4_260}
	if err := lifecycle.BindAcceptanceWindowForPhase("production-soak", production); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.Advance(t.Context(), &ScenarioObservation{Status: &DeploymentStatus{Contracts: &ContractView{}}}, nil); err != nil {
		t.Fatal(err)
	}
	if !lifecycle.Complete() || lifecycle.evidence.Stage != fleetLifecycleStageComplete || lifecycle.evidence.ReleaseHandoffHash != hash || lifecycle.evidence.ProductionRunID != "production-run" {
		t.Fatalf("production bypass did not retain exact release handoff: %+v", lifecycle.evidence)
	}
}

func TestFleetLifecyclePersistedCensusRetainsExactAppliedWeights(t *testing.T) {
	observation := fleetLifecycleDecisionFixture(10)
	census, ready, err := fleetLifecycleDecisionCensus(observation)
	if err != nil || !ready {
		t.Fatalf("build exact census ready=%t error=%v", ready, err)
	}
	if len(census.Validators) != 2 || len(census.Validators[0].AppliedWeights) != 202 || len(census.Validators[1].AppliedWeights) != 202 {
		t.Fatalf("persisted applied-weight census is incomplete: %+v", census.Validators)
	}
	if err := validateFleetLifecyclePersistedCensus(census); err != nil {
		t.Fatalf("exact persisted census: %v", err)
	}
	for index := range census.Validators[0].AppliedWeights {
		if census.Validators[0].AppliedWeights[index].UID == fleetLifecycleTargetExpectedUID {
			census.Validators[0].AppliedWeights[index].Value = 1
			break
		}
	}
	if err := validateFleetLifecyclePersistedCensus(census); err == nil {
		t.Fatal("persisted census accepted a positive on-chain weight for a rejected provider")
	}
}

func TestFleetLifecyclePersistedCensusRejectsSwappedValidatorOrder(t *testing.T) {
	observation := fleetLifecycleDecisionFixture(10)
	census, ready, err := fleetLifecycleDecisionCensus(observation)
	if err != nil || !ready {
		t.Fatalf("build exact census ready=%t error=%v", ready, err)
	}
	census.Validators[0], census.Validators[1] = census.Validators[1], census.Validators[0]
	if err := validateFleetLifecyclePersistedCensus(census); err == nil {
		t.Fatal("swapped validator order was accepted as a distinct persisted application pair")
	}
}

func TestFleetLifecycleCompleteRequiresResumeLineageValidation(t *testing.T) {
	lifecycle := &liveFleetLifecycle{phase: "production-soak", evidence: &FleetLifecycleEvidence{Stage: fleetLifecycleStageComplete}}
	if lifecycle.Complete() {
		t.Fatal("complete marker bypassed immutable resume-lineage validation")
	}
	if passed, _ := fleetLifecycleCompletionStatus(lifecycle); passed {
		t.Fatal("scenario completion assertion accepted an unvalidated lifecycle marker")
	}
	lifecycle.resumeValidated = true
	if !lifecycle.Complete() {
		t.Fatal("validated complete lifecycle did not report completion")
	}
	if passed, detail := fleetLifecycleCompletionStatus(lifecycle); !passed {
		t.Fatalf("scenario completion assertion rejected validated lifecycle: %s", detail)
	}
}
