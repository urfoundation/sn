package main

// A corrective activation closes with its owner-signed result. The offline
// journal reader must preserve that proof without fabricating StageVerified.
import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// Reproduces the ordinary-postcondition assumption on the exact finalized
// corrective action and rejects a substituted signed-result coordinate.
func TestFinalHistoricalCoordinatorJournalArtifactAcceptsSignedRepairResult(t *testing.T) {
	fixture := newCoordinatorRepairCarryFixture(t)
	source := fixture.executor.plan
	current := *source
	current.PlanHash = "0x" + strings.Repeat("42", 32)
	current.PriorPlanHashes = append(append([]string(nil), source.PriorPlanHashes...), source.PlanHash)
	current.CoordinatorUpgrade = fixture.reference.Request.Request.Upgrade
	carry := fixture.reference
	current.CoordinatorRepairCarry = &carry
	final := carry.Result.Result.Activate
	evidence := &FinalSemanticEvidence{DeploymentID: source.DeploymentID}
	row := &FinalHistoricalCoordinatorReceiptEvidence{
		PlanHash: source.PlanHash, ActionID: final.ActionID, IntentHash: final.IntentHash,
		Receipt: FinalEVMReceipt{TransactionHash: final.TransactionHash, Block: ChainHead{Number: final.BlockNumber, Hash: final.BlockHash}},
	}
	entries := []JournalEntry{carry.Result.Result.Deploy, final}
	action, got, verified, err := finalHistoricalCoordinatorJournalArtifactAction(evidence, &current, source, entries, row)
	if err != nil || action.ID != final.ActionID || got != final || verified != (JournalEntry{}) {
		t.Fatalf("signed corrective finalization was refused or turned into a verified row: %v", err)
	}
	changed := *row
	changed.Receipt.Block.Hash = finalTestHex(0x89)
	if _, _, _, err := finalHistoricalCoordinatorJournalArtifactAction(evidence, &current, source, entries, &changed); err == nil {
		t.Fatal("substituted corrective block was admitted")
	}
	noCarry := current
	noCarry.CoordinatorRepairCarry = nil
	if _, _, _, err := finalHistoricalCoordinatorJournalArtifactAction(evidence, &noCarry, source, entries, row); err == nil {
		t.Fatal("corrective action without an approved carry was admitted")
	}
	forgedVerified := append(append([]JournalEntry(nil), entries...), JournalEntry{
		DeploymentID: final.DeploymentID, PlanHash: final.PlanHash, ActionID: final.ActionID,
		IntentHash: final.IntentHash, Stage: StageVerified, Sequence: final.Sequence + 1,
	})
	if _, _, _, err := finalHistoricalCoordinatorJournalArtifactAction(evidence, &current, source, forgedVerified, row); err == nil {
		t.Fatal("invented ordinary postcondition was admitted for corrective action")
	}
	resultData, err := json.Marshal(carry.Result)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyFinalHistoricalCoordinatorRepairResultArtifact(&current, resultData); err != nil {
		t.Fatalf("exact signed result artifact was refused: %v", err)
	}
	altered := carry.Result
	altered.Result.Activate.BlockHash = finalTestHex(0x90)
	alteredData, err := json.Marshal(altered)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyFinalHistoricalCoordinatorRepairResultArtifact(&current, alteredData); err == nil {
		t.Fatal("substituted signed result artifact was admitted")
	}
	parsed, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		t.Fatal(err)
	}
	input, err := parsed.Pack("upgradeToAndCall", carry.Request.Request.Upgrade.Implementation, []byte{})
	if err != nil {
		t.Fatal(err)
	}
	block := ChainHead{Number: final.BlockNumber, Hash: final.BlockHash}
	logs := []finalCanonicalEVMLog{finalPayloadTestEvent(t, CoordinatorABI, "Upgraded", source.Deployment.CoordinatorProxy.Hex(), final.TransactionHash, block, 0, map[string]any{"implementation": carry.Request.Request.Upgrade.Implementation})}
	receipt := finalPayloadTestReceipt(t, logs)
	transaction := FinalCollectedEVMTransaction{TransactionHash: receipt.TransactionHash, Block: receipt.Block, From: strings.ToLower(source.Roles.Owner), To: strings.ToLower(source.Deployment.CoordinatorProxy.Hex()), Input: hexutil.Encode(input), ValueWei: "0"}
	emitters, err := finalHistoricalCoordinatorEmitterGraph(logs)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyFinalHistoricalCoordinatorActionWithRepair(evidence, nil, source, carry.Request.Request.Activate, receipt, transaction, logs, emitters, &carry); err != nil {
		t.Fatalf("exact corrective calldata and event were refused: %v", err)
	}
	transaction.Input = hexutil.Encode(append(input, 1))
	if err := verifyFinalHistoricalCoordinatorActionWithRepair(evidence, nil, source, carry.Request.Request.Activate, receipt, transaction, logs, emitters, &carry); err == nil {
		t.Fatal("changed corrective calldata was admitted")
	}
}
