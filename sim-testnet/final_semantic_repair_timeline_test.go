package main

// A signed correction follows the retained upgrade, not the older baseline
// that authorized that upgrade. Both source and artifact replay need this edge.
import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// Retains a constructor, ordinary activation and signed corrective activation
// with distinct implementation identities and deterministic block ordering.
type finalRepairTimelineTestFixture struct {
	evidence        *FinalSemanticEvidence
	current         *SetupPlan
	source          *SetupPlan
	planHashPlans   map[string]*SetupPlan
	journalEntries  []JournalEntry
	transactionLogs map[string][]finalCanonicalEVMLog
	baselines       []FinalCollectedCoordinatorBaseline
}

// Builds a valid repeated-upgrade baseline before the corrective request is
// signed, then supplies only the pure chronology inputs under test.
func newFinalRepairTimelineTestFixture(t *testing.T) finalRepairTimelineTestFixture {
	t.Helper()
	fixture := newCoordinatorRepairCarryPreparedFixture(t, func(original validatorEvidenceCarryTestFixture) {
		executor := original.executor
		source := executor.plan
		payloads, err := buildDeploymentPayloadsWithRegistrationGeneration(executor.cfg, executor.roles, source.Deployment.InitialNonce, source.Deployment.RegistrationRoleGeneration)
		if err != nil {
			t.Fatal(err)
		}
		if err := configureCoordinatorUpgradeNonce(payloads, source.CoordinatorUpgrade.DeployerNonce+2); err != nil {
			t.Fatal(err)
		}
		if err := rebindPlanCoordinatorUpgrade(source, payloads); err != nil {
			t.Fatal(err)
		}
		deploymentHash, err := contractDeploymentIdentityHash(source.Deployment)
		if err != nil {
			t.Fatal(err)
		}
		source.CoordinatorUpgradeBaseline = CoordinatorUpgradeBaseline{
			Schema: "urnetwork-coordinator-upgrade-baseline-v2", PriorDeploymentHash: deploymentHash,
			ReleaseDeploymentHash: deploymentHash, ReboundDeploymentHash: deploymentHash,
			ReserveSinkExecutableHash: finalTestHex(0x81), SettlementVaultExecutableHash: finalTestHex(0x82),
			GovernanceDrillVersion: crypto.Keccak256Hash([]byte("urnetwork/coordinator-adversary/v1")).Hex(), GovernanceProxiableUUID: erc1967ImplementationSlot,
			DeployerNonce:        source.CoordinatorUpgrade.DeployerNonce,
			ActiveImplementation: source.Deployment.CoordinatorImplementation.Hex(), ActiveImplementationHash: source.Deployment.RuntimeHashes[source.Deployment.CoordinatorImplementation.Hex()],
			PrecompileProbeExecutableHash: finalTestHex(0x83), FinalizedBlock: 20, FinalizedBlockHash: finalTestHex(0x84),
		}
		if err := validateCoordinatorUpgradeBaseline(source.CoordinatorUpgradeBaseline, source.Deployment, source.CoordinatorUpgrade); err != nil {
			t.Fatal(err)
		}
		source.PlanHash, err = source.hash()
		if err != nil {
			t.Fatal(err)
		}
	})
	source := fixture.executor.plan
	current := *source
	current.PlanHash = finalTestHex(0x42)
	current.PriorPlanHashes = append(append([]string(nil), source.PriorPlanHashes...), source.PlanHash)
	current.CoordinatorUpgrade = fixture.reference.Request.Request.Upgrade
	carry := fixture.reference
	current.CoordinatorRepairCarry = &carry
	initialAction := actionByID(t, source, "evm.coordinator-proxy")
	upgradeAction := actionByID(t, source, "evm.coordinator-upgrade-activate")
	initial := JournalEntry{DeploymentID: source.DeploymentID, PlanHash: source.PlanHash, ActionID: initialAction.ID, IntentHash: initialAction.IntentHash,
		Stage: StageFinalized, Sequence: 1, TransactionHash: finalTestHex(0x51), BlockNumber: 10, BlockHash: finalTestHex(0x61)}
	upgrade := JournalEntry{DeploymentID: source.DeploymentID, PlanHash: source.PlanHash, ActionID: upgradeAction.ID, IntentHash: upgradeAction.IntentHash,
		Stage: StageFinalized, Sequence: 2, TransactionHash: finalTestHex(0x52), BlockNumber: 30, BlockHash: finalTestHex(0x62)}
	proxy := strings.ToLower(source.Deployment.CoordinatorProxy.Hex())
	transactionLogs := make(map[string][]finalCanonicalEVMLog)
	for _, transition := range []struct {
		entry          JournalEntry
		implementation common.Address
	}{
		{entry: initial, implementation: source.Deployment.CoordinatorImplementation},
		{entry: upgrade, implementation: source.CoordinatorUpgrade.Implementation},
		{entry: carry.Result.Result.Activate, implementation: carry.Request.Request.Upgrade.Implementation},
	} {
		head := ChainHead{Number: transition.entry.BlockNumber, Hash: transition.entry.BlockHash}
		transactionLogs[transition.entry.TransactionHash] = []finalCanonicalEVMLog{finalPayloadTestEvent(t, CoordinatorABI, "Upgraded", proxy, transition.entry.TransactionHash, head, 0, map[string]any{"implementation": transition.implementation})}
	}
	baseline := FinalCollectedCoordinatorBaseline{Proxy: proxy, Head: ChainHead{Number: initial.BlockNumber, Hash: initial.BlockHash},
		Implementation: strings.ToLower(source.Deployment.CoordinatorImplementation.Hex()), ImplementationRuntimeHash: source.Deployment.RuntimeHashes[source.Deployment.CoordinatorImplementation.Hex()],
		ProxyRuntimeHash: source.Deployment.RuntimeHashes[source.Deployment.CoordinatorProxy.Hex()]}
	return finalRepairTimelineTestFixture{
		evidence: &FinalSemanticEvidence{DeploymentID: source.DeploymentID, EVMCampaignStartHead: ChainHead{Number: 200, Hash: finalTestHex(0x71)}},
		current:  &current, source: source, planHashPlans: map[string]*SetupPlan{source.PlanHash: source, current.PlanHash: &current},
		journalEntries: []JournalEntry{initial, upgrade, carry.Result.Result.Deploy, carry.Result.Result.Activate}, transactionLogs: transactionLogs,
		baselines: []FinalCollectedCoordinatorBaseline{baseline},
	}
}

// The corrective execution must chain from the signed retained implementation
// even when the source plan's ordinary upgrade has an older repeated baseline.
func TestFinalSemanticHistoricalCoordinatorTimelineCarriesSignedRepairAfterUpgrade(t *testing.T) {
	fixture := newFinalRepairTimelineTestFixture(t)
	timeline, err := finalHistoricalCoordinatorBuildTimeline(fixture.evidence, fixture.current, fixture.planHashPlans, fixture.journalEntries, fixture.transactionLogs, fixture.baselines)
	if err != nil {
		t.Fatalf("signed correction after its retained upgrade was rejected: %v", err)
	}
	fixture.evidence.HistoricalCoordinatorTimeline = timeline.evidence()
	if len(fixture.evidence.HistoricalCoordinatorTimeline) != 1 || len(fixture.evidence.HistoricalCoordinatorTimeline[0].Upgrades) != 2 {
		t.Fatal("timeline omitted an approved upgrade")
	}
	upgrades := fixture.evidence.HistoricalCoordinatorTimeline[0].Upgrades
	if upgrades[1].Execution != upgrades[0].Post || upgrades[1].Execution.Implementation != strings.ToLower(fixture.source.CoordinatorUpgrade.Implementation.Hex()) || upgrades[1].Post.Implementation != strings.ToLower(fixture.current.CoordinatorUpgrade.Implementation.Hex()) {
		t.Fatal("corrective transition lost its retained execution or signed post identity")
	}
	artifact, err := finalHistoricalCoordinatorTimelineArtifactFromSource(timeline, fixture.baselines, fixture.transactionLogs)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyFinalHistoricalCoordinatorTimelineArtifact(fixture.evidence, fixture.current, fixture.planHashPlans, fixture.journalEntries, data); err != nil {
		t.Fatalf("artifact replay rejected the signed corrective transition: %v", err)
	}
}

// Removing the retained activation must not let the correction execute from
// the older original baseline merely because that baseline is approved too.
func TestFinalSemanticHistoricalCoordinatorTimelineRejectsRepairWithoutRetainedUpgrade(t *testing.T) {
	fixture := newFinalRepairTimelineTestFixture(t)
	delete(fixture.transactionLogs, fixture.journalEntries[1].TransactionHash)
	fixture.journalEntries = append(fixture.journalEntries[:1], fixture.journalEntries[2:]...)
	if _, err := finalHistoricalCoordinatorBuildTimeline(fixture.evidence, fixture.current, fixture.planHashPlans, fixture.journalEntries, fixture.transactionLogs, fixture.baselines); err == nil {
		t.Fatal("signed correction was accepted without the retained upgrade transition")
	}
}

// An approved earlier plan with the same implementation address cannot supply
// another runtime in place of the corrective request's signed retained bytes.
func TestFinalSemanticHistoricalCoordinatorTimelineRejectsRepairAfterDifferentRetainedRuntime(t *testing.T) {
	fixture := newFinalRepairTimelineTestFixture(t)
	predecessor := *fixture.source
	predecessor.CoordinatorUpgrade.RuntimeCodeHash = finalTestHex(0x91)
	predecessor.Actions = append([]Action(nil), predecessor.Actions...)
	for index := range predecessor.Actions {
		action := &predecessor.Actions[index]
		if action.ID != "evm.coordinator-upgrade-activate" && action.ID != "evm.coordinator-upgrade-implementation" {
			continue
		}
		action.Parameters = cloneStrings(action.Parameters)
		action.Parameters["runtime_code_hash"] = predecessor.CoordinatorUpgrade.RuntimeCodeHash
		var err error
		action.IntentHash, err = actionIntentHash(*action)
		if err != nil {
			t.Fatal(err)
		}
		if action.ID == "evm.coordinator-upgrade-activate" {
			fixture.journalEntries[1].IntentHash = action.IntentHash
		}
	}
	var err error
	predecessor.PlanHash, err = predecessor.hash()
	if err != nil {
		t.Fatal(err)
	}
	fixture.planHashPlans[predecessor.PlanHash] = &predecessor
	fixture.current.PriorPlanHashes = append(fixture.current.PriorPlanHashes, predecessor.PlanHash)
	fixture.journalEntries[1].PlanHash = predecessor.PlanHash
	if _, err := finalHistoricalCoordinatorBuildTimeline(fixture.evidence, fixture.current, fixture.planHashPlans, fixture.journalEntries, fixture.transactionLogs, fixture.baselines); err == nil {
		t.Fatal("signed correction was accepted after another retained runtime")
	}
}
