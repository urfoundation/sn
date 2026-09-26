package main

// The historical census must include an owner-signed corrective action that
// was approved after its source plan, without accepting a lookalike journal row.
import (
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// Reproduces the refusal of a completed repair whose exact signed request and
// result are carried by the active plan, while retaining a foreign-row control.
func TestFinalCaptureReleaseContractCensusAcceptsExactCoordinatorRepairCarry(t *testing.T) {
	fixture := newCoordinatorRepairCarryFixture(t)
	source := fixture.executor.plan
	source.Deployment.DeployBlock = 200
	current := *source
	current.PlanHash = "0x" + strings.Repeat("42", 32)
	current.PriorPlanHashes = append(append([]string(nil), source.PriorPlanHashes...), source.PlanHash)
	current.CoordinatorUpgrade = fixture.reference.Request.Request.Upgrade
	carry := fixture.reference
	current.CoordinatorRepairCarry = &carry
	plans := map[string]*SetupPlan{source.PlanHash: source, current.PlanHash: &current}
	entries := []JournalEntry{carry.Result.Result.Deploy, carry.Result.Result.Activate}
	batcher := common.HexToAddress("0x9000000000000000000000000000000000000009")
	if _, err := finalCaptureReleaseContractCensusForLineage(&current, &current.Deployment, batcher, plans, entries); err != nil {
		t.Fatalf("exact signed correction was refused: %v", err)
	}
	evidence := &FinalSemanticEvidence{DeploymentID: source.DeploymentID}
	evidence.EVMCampaignStartHead.Number = 300
	mutations, err := finalHistoricalCoordinatorJournalActionsWithSources(evidence, &current, plans, entries, finalHistoricalJournalSources{})
	if err != nil || len(mutations) != 1 {
		t.Fatalf("historical coordinator reader omitted signed activation: %v, %d actions", err, len(mutations))
	}
	if mutation, found := mutations[entries[1].TransactionHash]; !found || mutation.action.ID != entries[1].ActionID {
		t.Fatal("historical coordinator reader selected another transaction")
	}
	post, err := finalHistoricalCoordinatorActionPostIdentity(&current, source, carry.Request.Request.Activate, false)
	if err != nil || post.Implementation != strings.ToLower(current.CoordinatorUpgrade.Implementation.Hex()) || post.RuntimeHash != strings.ToLower(current.CoordinatorUpgrade.RuntimeCodeHash) {
		t.Fatalf("corrective transition used predecessor runtime: %+v, %v", post, err)
	}
	changed := append([]JournalEntry(nil), entries...)
	changed[0].IntentHash = finalTestHex(0x83)
	if _, err := finalCaptureReleaseContractCensusForLineage(&current, &current.Deployment, batcher, plans, changed); err == nil {
		t.Fatal("changed corrective intent was admitted")
	}
	if _, err := finalHistoricalCoordinatorJournalActionsWithSources(evidence, &current, plans, changed, finalHistoricalJournalSources{}); err == nil {
		t.Fatal("historical coordinator reader admitted changed corrective intent")
	}
	duplicate := append(append([]JournalEntry(nil), entries...), entries[0])
	if _, err := finalCaptureReleaseContractCensusForLineage(&current, &current.Deployment, batcher, plans, duplicate); err == nil {
		t.Fatal("duplicate corrective finalization was admitted")
	}
	missing := current
	missing.CoordinatorRepairCarry = nil
	plans[missing.PlanHash] = &missing
	if _, err := finalCaptureReleaseContractCensusForLineage(&missing, &missing.Deployment, batcher, plans, entries); err == nil {
		t.Fatal("unsigned corrective action was admitted")
	}
}
