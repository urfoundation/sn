package main

import (
	"strconv"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/protocol"
)

func fleetLifecycleRegistrationActionID(variantName string) string {
	switch variantName {
	case fleetLifecycleVariantFallback:
		return "lifecycle.fallback.register"
	case fleetLifecycleVariantProvider:
		return "lifecycle.provider.register"
	case fleetLifecycleVariantTerminal:
		return "lifecycle.terminal.register"
	default:
		return ""
	}
}

func fleetLifecycleRegistrationLineageFixture(t *testing.T, variantName string) (FleetLifecycleRegistrationEvidence, Action, *RoleSecrets, JournalEntry) {
	t.Helper()
	cfg := testResolvedConfig(t)
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := fleetLifecycleRegistrationExpectationFor(variantName)
	if err != nil {
		t.Fatal(err)
	}
	victimHotkey, err := roleBytes32(roles, expected.victimHotkeyLabel)
	if err != nil {
		t.Fatal(err)
	}
	victimColdkey, err := roleBytes32(roles, expected.victimColdkeyLabel)
	if err != nil {
		t.Fatal(err)
	}
	replacementHotkey, err := roleBytes32(roles, expected.replacementHotkeyLabel)
	if err != nil {
		t.Fatal(err)
	}
	replacementColdkey, err := roleBytes32(roles, expected.replacementColdkeyLabel)
	if err != nil {
		t.Fatal(err)
	}
	inputs := make([]FleetLifecyclePruneInput, 10)
	for index := range inputs {
		var hotkey, coldkey [32]byte
		hotkey[0], hotkey[1] = 0xf0, byte(index+1)
		coldkey[0], coldkey[1] = 0xe0, byte(index+1)
		inputs[index] = FleetLifecyclePruneInput{
			UID: uint16(index), Hotkey: fleetLifecycleHex(hotkey), Coldkey: fleetLifecycleHex(coldkey),
			EmissionRao: 100, RegistrationBlock: uint64(20 + index), Immune: true,
		}
	}
	inputs[0].Immortal = true
	inputs[expected.expectedUID] = FleetLifecyclePruneInput{
		UID: uint16(expected.expectedUID), Hotkey: fleetLifecycleHex(victimHotkey), Coldkey: fleetLifecycleHex(victimColdkey),
		RegistrationBlock: 10, Immune: true,
	}
	pre := FleetLifecyclePruneSnapshot{
		Head: ChainHead{Number: 100, Hash: "0x" + strings.Repeat("11", 32)}, UIDCount: 10, MaximumUIDs: 10,
		ImmunityPeriodBlocks: 1000, MinimumNonImmuneUIDs: 1, RuntimePruneUID: uint16(expected.expectedUID), Inputs: inputs,
	}
	postInputs := append([]FleetLifecyclePruneInput(nil), inputs...)
	postInputs[expected.expectedUID] = FleetLifecyclePruneInput{
		UID: uint16(expected.expectedUID), Hotkey: fleetLifecycleHex(replacementHotkey), Coldkey: fleetLifecycleHex(replacementColdkey),
		RegistrationBlock: 200, Immune: true,
	}
	post := pre
	post.Head = ChainHead{Number: 200, Hash: "0x" + strings.Repeat("22", 32)}
	post.Inputs = postInputs
	post.RuntimePruneUID = 0
	action := Action{
		ID: fleetLifecycleRegistrationActionID(variantName), Kind: "substrate-extrinsic", Target: expected.replacementHotkeyLabel, IntentHash: "intent-" + variantName,
		Parameters: map[string]string{
			"expected_pruned_fleet": strconv.Itoa(expected.victimFleet), "expected_pruned_hotkey": expected.victimHotkeyLabel,
			"expected_pruned_uid": strconv.Itoa(expected.expectedUID), "expected_replacement_hotkey": expected.replacementHotkeyLabel,
		},
	}
	evidence := FleetLifecycleRegistrationEvidence{
		Schema: "urnetwork-sim-fleet-registration-replacement-v1", DeploymentID: cfg.Config.Deployment.DeploymentID,
		PlanHash: "plan", ActionID: action.ID, IntentHash: action.IntentHash,
		VictimFleet: expected.victimFleet, VictimRole: expected.victimHotkeyLabel, VictimUID: uint16(expected.expectedUID),
		VictimHotkey: fleetLifecycleHex(victimHotkey), VictimColdkey: fleetLifecycleHex(victimColdkey),
		ReplacementHotkey: fleetLifecycleHex(replacementHotkey), ReplacementColdkey: fleetLifecycleHex(replacementColdkey),
		PrePrune: pre, PostRegistration: post, TransactionHash: "0x" + strings.Repeat("33", 32), BlockNumber: 200, BlockHash: post.Head.Hash,
	}
	transaction := JournalEntry{
		PlanHash: evidence.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageFinalized,
		TransactionHash: evidence.TransactionHash, BlockNumber: evidence.BlockNumber, BlockHash: evidence.BlockHash,
		RecoveryBlock: pre.Head.Number, RecoveryBlockHash: pre.Head.Hash,
	}
	return evidence, action, roles, transaction
}

func TestFleetLifecycleRegistrationLineageAcceptsEveryExactWave(t *testing.T) {
	for _, variantName := range []string{fleetLifecycleVariantFallback, fleetLifecycleVariantProvider, fleetLifecycleVariantTerminal} {
		evidence, action, roles, transaction := fleetLifecycleRegistrationLineageFixture(t, variantName)
		if err := validateFleetLifecycleRegistrationLineage(evidence, action, variantName, evidence.DeploymentID, evidence.PlanHash, roles, transaction); err != nil {
			t.Fatalf("%s exact lineage: %v", variantName, err)
		}
	}
}

func TestFleetLifecycleRegistrationRecoveryCheckpointAcceptsEveryExactWave(t *testing.T) {
	for _, variantName := range []string{fleetLifecycleVariantFallback, fleetLifecycleVariantProvider, fleetLifecycleVariantTerminal} {
		evidence, _, roles, _ := fleetLifecycleRegistrationLineageFixture(t, variantName)
		if err := validateFleetLifecycleRegistrationRecoverySnapshot(evidence.PrePrune, variantName, roles); err != nil {
			t.Fatalf("%s exact recovery checkpoint: %v", variantName, err)
		}
	}
}

func TestFleetLifecycleRegistrationRecoveryCheckpointRejectsPruneOrderDrift(t *testing.T) {
	evidence, _, roles, _ := fleetLifecycleRegistrationLineageFixture(t, fleetLifecycleVariantFallback)
	evidence.PrePrune.Inputs[1].EmissionRao = 0
	evidence.PrePrune.Inputs[1].RegistrationBlock = 5
	evidence.PrePrune.RuntimePruneUID = 1
	if err := validateFleetLifecycleRegistrationRecoverySnapshot(evidence.PrePrune, fleetLifecycleVariantFallback, roles); err == nil {
		t.Fatal("recovery checkpoint selecting a foreign earlier prune victim was accepted")
	}
}

func TestFleetLifecycleRegistrationRecoveryCheckpointRejectsAlreadyLiveReplacement(t *testing.T) {
	evidence, _, roles, _ := fleetLifecycleRegistrationLineageFixture(t, fleetLifecycleVariantFallback)
	replacementHotkey, err := roleBytes32(roles, churnHotkeyLabel(fleetLifecycleFallbackChurn))
	if err != nil {
		t.Fatal(err)
	}
	replacementColdkey, err := roleBytes32(roles, churnColdkeyLabel(fleetLifecycleFallbackChurn))
	if err != nil {
		t.Fatal(err)
	}
	evidence.PrePrune.Inputs[9] = FleetLifecyclePruneInput{
		UID: 9, Hotkey: fleetLifecycleHex(replacementHotkey), Coldkey: fleetLifecycleHex(replacementColdkey),
		EmissionRao: 100, RegistrationBlock: 99, Immune: true,
	}
	if err := validateFleetLifecycleRegistrationRecoverySnapshot(evidence.PrePrune, fleetLifecycleVariantFallback, roles); err == nil {
		t.Fatal("recovery checkpoint containing the replacement as an already-live UID was accepted")
	}
}

func TestFleetLifecycleRegistrationLineageRejectsCrossVariantEvidence(t *testing.T) {
	evidence, action, roles, transaction := fleetLifecycleRegistrationLineageFixture(t, fleetLifecycleVariantFallback)
	if err := validateFleetLifecycleRegistrationLineage(evidence, action, fleetLifecycleVariantProvider, evidence.DeploymentID, evidence.PlanHash, roles, transaction); err == nil {
		t.Fatal("fallback registration evidence was accepted as the provider restoration")
	}
}

func TestFleetLifecycleRegistrationLineageRejectsWrongActionTarget(t *testing.T) {
	evidence, action, roles, transaction := fleetLifecycleRegistrationLineageFixture(t, fleetLifecycleVariantProvider)
	action.Target = churnHotkeyLabel(fleetLifecycleCompanionChurn)
	if err := validateFleetLifecycleRegistrationLineage(evidence, action, fleetLifecycleVariantProvider, evidence.DeploymentID, evidence.PlanHash, roles, transaction); err == nil {
		t.Fatal("provider registration accepted an action targeting another hotkey")
	}
}

func TestFleetLifecycleRegistrationLineageRejectsCoherentForeignRoles(t *testing.T) {
	evidence, action, roles, transaction := fleetLifecycleRegistrationLineageFixture(t, fleetLifecycleVariantFallback)
	foreignHotkey, err := roleBytes32(roles, churnHotkeyLabel(9))
	if err != nil {
		t.Fatal(err)
	}
	foreignColdkey, err := roleBytes32(roles, churnColdkeyLabel(9))
	if err != nil {
		t.Fatal(err)
	}
	evidence.VictimRole = churnHotkeyLabel(9)
	evidence.VictimHotkey = fleetLifecycleHex(foreignHotkey)
	evidence.VictimColdkey = fleetLifecycleHex(foreignColdkey)
	evidence.PrePrune.Inputs[evidence.VictimUID].Hotkey = evidence.VictimHotkey
	evidence.PrePrune.Inputs[evidence.VictimUID].Coldkey = evidence.VictimColdkey
	if err := validateFleetLifecycleRegistrationLineage(evidence, action, fleetLifecycleVariantFallback, evidence.DeploymentID, evidence.PlanHash, roles, transaction); err == nil {
		t.Fatal("internally coherent registration evidence for foreign roles was accepted")
	}
}

func TestFleetLifecycleRegistrationLineageRejectsWrongVictimUID(t *testing.T) {
	evidence, action, roles, transaction := fleetLifecycleRegistrationLineageFixture(t, fleetLifecycleVariantFallback)
	evidence.VictimUID++
	if err := validateFleetLifecycleRegistrationLineage(evidence, action, fleetLifecycleVariantFallback, evidence.DeploymentID, evidence.PlanHash, roles, transaction); err == nil {
		t.Fatal("registration evidence with the wrong victim UID was accepted")
	}
}

func TestFleetLifecycleRegistrationLineageRejectsTransactionOrCheckpointDrift(t *testing.T) {
	for _, mutation := range []struct {
		name   string
		mutate func(*JournalEntry)
	}{
		{name: "missing", mutate: func(transaction *JournalEntry) { *transaction = JournalEntry{} }},
		{name: "transaction", mutate: func(transaction *JournalEntry) { transaction.TransactionHash = "0x" + strings.Repeat("44", 32) }},
		{name: "inclusion", mutate: func(transaction *JournalEntry) { transaction.BlockNumber++ }},
		{name: "recovery", mutate: func(transaction *JournalEntry) { transaction.RecoveryBlock-- }},
		{name: "recovery hash", mutate: func(transaction *JournalEntry) { transaction.RecoveryBlockHash = "0x" + strings.Repeat("55", 32) }},
	} {
		evidence, action, roles, transaction := fleetLifecycleRegistrationLineageFixture(t, fleetLifecycleVariantFallback)
		mutation.mutate(&transaction)
		if err := validateFleetLifecycleRegistrationLineage(evidence, action, fleetLifecycleVariantFallback, evidence.DeploymentID, evidence.PlanHash, roles, transaction); err == nil {
			t.Fatalf("%s journal drift was accepted", mutation.name)
		}
	}
}

func TestFleetLifecycleCleanupLineageAcceptsExactFinalizedAction(t *testing.T) {
	action := Action{ID: "lifecycle.provider.cleanup.1", Kind: "evm-transaction", IntentHash: "intent"}
	evidence := FleetLifecycleCleanupEvidence{
		Schema: "urnetwork-sim-fleet-binding-cleanup-v2", DeploymentID: "deployment", PlanHash: "plan", ActionID: action.ID, IntentHash: action.IntentHash,
		ClientID: "0x" + strings.Repeat("11", 16), FleetID: "0x" + strings.Repeat("22", 32), Generation: 3, CleanedAtEpoch: 8,
		MemberCountBefore: 4, MemberCountAfter: 3, TransactionHash: "0x" + strings.Repeat("33", 32), BeforeBlock: ChainHead{Number: 99, Hash: "0x" + strings.Repeat("55", 32)}, BlockNumber: 100, BlockHash: "0x" + strings.Repeat("44", 32),
	}
	transaction := JournalEntry{PlanHash: evidence.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageFinalized, TransactionHash: evidence.TransactionHash, BlockNumber: evidence.BlockNumber, BlockHash: evidence.BlockHash, RecoveryBlock: 99, RecoveryBlockHash: "0x" + strings.Repeat("55", 32)}
	if err := validateFleetLifecycleCleanupLineage(evidence, action, evidence.DeploymentID, evidence.PlanHash, transaction); err != nil {
		t.Fatal(err)
	}
}

func TestFleetLifecycleCleanupEvidenceRequiresCanonicalParentHead(t *testing.T) {
	var clientID [16]byte
	var fleetID [32]byte
	clientID[0], fleetID[0] = 0x11, 0x22
	member := protocol.FleetMember{ClientID: clientID}
	evidence := FleetLifecycleCleanupEvidence{
		Schema: "urnetwork-sim-fleet-binding-cleanup-v2", DeploymentID: "deployment", PlanHash: "plan", ActionID: "lifecycle.provider.cleanup.1", IntentHash: "intent",
		ClientID: fleetLifecycleHex16(clientID), FleetID: fleetLifecycleHex(fleetID), Generation: 3, CleanedAtEpoch: 8,
		MemberCountBefore: 4, MemberCountAfter: 3, TransactionHash: "0x" + strings.Repeat("33", 32), BeforeBlock: ChainHead{Number: 99, Hash: "0x" + strings.Repeat("55", 32)}, BlockNumber: 100, BlockHash: "0x" + strings.Repeat("44", 32),
	}
	if err := validateFleetLifecycleCleanupEvidence(evidence, member, fleetID, 3); err != nil {
		t.Fatal(err)
	}
	evidence.BeforeBlock.Number--
	if err := validateFleetLifecycleCleanupEvidence(evidence, member, fleetID, 3); err == nil {
		t.Fatal("cleanup evidence with a non-parent pre-state block was accepted")
	}
}

func TestFleetLifecycleCleanupLineageRejectsSwappedMemberOrReceipt(t *testing.T) {
	action := Action{ID: "lifecycle.provider.cleanup.1", Kind: "evm-transaction", IntentHash: "intent"}
	evidence := FleetLifecycleCleanupEvidence{DeploymentID: "deployment", PlanHash: "plan", ActionID: action.ID, IntentHash: action.IntentHash, TransactionHash: "0x" + strings.Repeat("33", 32), BeforeBlock: ChainHead{Number: 99, Hash: "0x" + strings.Repeat("66", 32)}, BlockNumber: 100, BlockHash: "0x" + strings.Repeat("44", 32)}
	base := JournalEntry{PlanHash: evidence.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageFinalized, TransactionHash: evidence.TransactionHash, BlockNumber: evidence.BlockNumber, BlockHash: evidence.BlockHash, RecoveryBlock: 99, RecoveryBlockHash: "0x" + strings.Repeat("66", 32)}
	for _, mutation := range []struct {
		name   string
		mutate func(*FleetLifecycleCleanupEvidence, *Action, *JournalEntry)
	}{
		{name: "member", mutate: func(_ *FleetLifecycleCleanupEvidence, action *Action, transaction *JournalEntry) {
			action.ID = "lifecycle.provider.cleanup.2"
			transaction.ActionID = action.ID
		}},
		{name: "transaction", mutate: func(_ *FleetLifecycleCleanupEvidence, _ *Action, transaction *JournalEntry) {
			transaction.TransactionHash = "0x" + strings.Repeat("55", 32)
		}},
		{name: "block", mutate: func(_ *FleetLifecycleCleanupEvidence, _ *Action, transaction *JournalEntry) {
			transaction.BlockNumber++
		}},
		{name: "recovery", mutate: func(_ *FleetLifecycleCleanupEvidence, _ *Action, transaction *JournalEntry) {
			transaction.RecoveryBlock = transaction.BlockNumber
		}},
		{name: "recovery hash", mutate: func(_ *FleetLifecycleCleanupEvidence, _ *Action, transaction *JournalEntry) {
			transaction.RecoveryBlockHash = ""
		}},
		{name: "missing", mutate: func(_ *FleetLifecycleCleanupEvidence, _ *Action, transaction *JournalEntry) {
			*transaction = JournalEntry{}
		}},
	} {
		candidateEvidence, candidateAction, transaction := evidence, action, base
		mutation.mutate(&candidateEvidence, &candidateAction, &transaction)
		if err := validateFleetLifecycleCleanupLineage(candidateEvidence, candidateAction, candidateEvidence.DeploymentID, candidateEvidence.PlanHash, transaction); err == nil {
			t.Fatalf("cleanup %s drift was accepted", mutation.name)
		}
	}
}

func TestFleetLifecycleCommitmentLineageRejectsCrossWaveOrMissingJournal(t *testing.T) {
	action := Action{ID: "lifecycle.provider.commitment", Kind: "substrate-extrinsic", Target: fleetProviderHotkeyLabel(fleetLifecycleTargetFleet), IntentHash: "intent", Parameters: map[string]string{"generation": "4"}}
	evidence := FleetCommitmentEvidence{DeploymentID: "deployment", PlanHash: "plan", ActionID: action.ID, IntentHash: action.IntentHash, ExtrinsicHash: "0x" + strings.Repeat("11", 32), FinalizedBlock: 20, FinalizedBlockHash: "0x" + strings.Repeat("22", 32)}
	transaction := JournalEntry{PlanHash: evidence.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageFinalized, TransactionHash: evidence.ExtrinsicHash, BlockNumber: evidence.FinalizedBlock, BlockHash: evidence.FinalizedBlockHash, RecoveryBlock: 10, RecoveryBlockHash: "0x" + strings.Repeat("33", 32)}
	if err := validateFleetLifecycleCommitmentLineage(evidence, action, fleetLifecycleVariantProvider, evidence.DeploymentID, evidence.PlanHash, transaction); err != nil {
		t.Fatal(err)
	}
	if err := validateFleetLifecycleCommitmentLineage(evidence, action, fleetLifecycleVariantTerminal, evidence.DeploymentID, evidence.PlanHash, transaction); err == nil {
		t.Fatal("provider commitment was accepted as the terminal wave")
	}
	if err := validateFleetLifecycleCommitmentLineage(evidence, action, fleetLifecycleVariantProvider, evidence.DeploymentID, evidence.PlanHash, JournalEntry{}); err == nil {
		t.Fatal("commitment without journal lineage was accepted")
	}
	action.Target = churnHotkeyLabel(fleetLifecycleCompanionChurn)
	if err := validateFleetLifecycleCommitmentLineage(evidence, action, fleetLifecycleVariantProvider, evidence.DeploymentID, evidence.PlanHash, transaction); err == nil {
		t.Fatal("provider commitment accepted an action targeting another hotkey")
	}
}

func TestFleetLifecycleBindingLineageRejectsCrossMemberOrForgedReceipt(t *testing.T) {
	action := Action{ID: "lifecycle.provider.bind.1", Kind: "evm-transaction", IntentHash: "intent"}
	evidence := FleetBindingEvidence{DeploymentID: "deployment", PlanHash: "plan", ActionID: action.ID, IntentHash: action.IntentHash, TransactionHash: "0x" + strings.Repeat("11", 32), BlockNumber: 20, BlockHash: "0x" + strings.Repeat("22", 32)}
	transaction := JournalEntry{PlanHash: evidence.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageFinalized, TransactionHash: evidence.TransactionHash, BlockNumber: evidence.BlockNumber, BlockHash: evidence.BlockHash, RecoveryBlock: 10, RecoveryBlockHash: "0x" + strings.Repeat("33", 32)}
	if err := validateFleetLifecycleBindingLineage(evidence, action, fleetLifecycleVariantProvider, 1, evidence.DeploymentID, evidence.PlanHash, transaction); err != nil {
		t.Fatal(err)
	}
	if err := validateFleetLifecycleBindingLineage(evidence, action, fleetLifecycleVariantProvider, 2, evidence.DeploymentID, evidence.PlanHash, transaction); err == nil {
		t.Fatal("member-one binding was accepted as member two")
	}
	transaction.TransactionHash = "0x" + strings.Repeat("44", 32)
	if err := validateFleetLifecycleBindingLineage(evidence, action, fleetLifecycleVariantProvider, 1, evidence.DeploymentID, evidence.PlanHash, transaction); err == nil {
		t.Fatal("binding with a forged receipt was accepted")
	}
}

func TestFleetLifecycleMirrorLineageRejectsCrossWaveOrForgedReceipt(t *testing.T) {
	action := Action{ID: "lifecycle.provider.mirror", Kind: "evm-transaction", IntentHash: "intent"}
	evidence := FleetLifecycleMirrorEvidence{
		Schema: "urnetwork-sim-fleet-commitment-mirror-v1", DeploymentID: "deployment", PlanHash: "plan", ActionID: action.ID, IntentHash: action.IntentHash,
		Hotkey: "0x" + strings.Repeat("11", 32), CommitmentHash: "0x" + strings.Repeat("22", 32), FinalizedBlock: 10, FinalizedBlockHash: "0x" + strings.Repeat("33", 32),
		TransactionHash: "0x" + strings.Repeat("44", 32), BlockNumber: 20, BlockHash: "0x" + strings.Repeat("55", 32),
	}
	transaction := JournalEntry{PlanHash: evidence.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageFinalized, TransactionHash: evidence.TransactionHash, BlockNumber: evidence.BlockNumber, BlockHash: evidence.BlockHash, RecoveryBlock: 15, RecoveryBlockHash: "0x" + strings.Repeat("66", 32)}
	if err := validateFleetLifecycleMirrorLineage(evidence, action, fleetLifecycleVariantProvider, evidence.DeploymentID, evidence.PlanHash, transaction); err != nil {
		t.Fatal(err)
	}
	if err := validateFleetLifecycleMirrorLineage(evidence, action, fleetLifecycleVariantFallback, evidence.DeploymentID, evidence.PlanHash, transaction); err == nil {
		t.Fatal("provider mirror was accepted as the fallback wave")
	}
	transaction.TransactionHash = "0x" + strings.Repeat("77", 32)
	if err := validateFleetLifecycleMirrorLineage(evidence, action, fleetLifecycleVariantProvider, evidence.DeploymentID, evidence.PlanHash, transaction); err == nil {
		t.Fatal("mirror with a forged receipt was accepted")
	}
}
