// Synthetic partial renewal state reproduces cut-off leases without changing
// the original client/hotkey signatures or relying on external chain state.
package main

import (
	"context"
	"encoding/json"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/stabi"
)

// Historical source compaction may omit the earlier deterministic revoke
// action; restore it before validating its successor's cutoff reference.
func TestFleetRenewalPartialPrestateRestoresCompactedPredecessor(t *testing.T) {
	fixture, base, observation := fleetRenewalPartialPrestateFixture(t)
	renewal, err := prepareFleetRenewal(fixture.cfg, fixture.stateDir, base, fixture.roles, observation)
	if err != nil {
		t.Fatal(err)
	}
	approved, err := appendFleetRenewalPlan(base, renewal)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(approved)
	if err != nil {
		t.Fatal(err)
	}
	var compacted SetupPlan
	if err := json.Unmarshal(raw, &compacted); err != nil {
		t.Fatal(err)
	}
	proof := renewal.Fleets[fleetLifecycleTargetFleet-1].Members[0].PriorRevocation
	compacted.Actions = nil
	for _, action := range approved.Actions {
		if action.ID != proof.ActionId {
			compacted.Actions = append(compacted.Actions, action)
		}
	}
	restored, err := restoreFleetRenewalActions(&compacted)
	if err != nil {
		t.Fatalf("successor cutoff could not restore its compacted predecessor: %v", err)
	}
	if err := validateFleetRenewalPlan(restored); err != nil {
		t.Fatal(err)
	}
	if len(compacted.Actions)+1 != len(restored.Actions) {
		t.Fatal("historical restoration mutated its source or changed the action set")
	}
}

// Two revoked members and two unchanged members force the partial-wave case.
func fleetRenewalPartialPrestateFixture(t *testing.T) (fleetRenewalTestFixture, *SetupPlan, fleetRenewalObservation) {
	t.Helper()
	fixture := newFleetRenewalTestFixture(t)
	base, err := appendFleetRenewalPlan(fixture.base, fixture.renewal)
	if err != nil {
		t.Fatal(err)
	}
	observation := fixture.observed
	observation.Records = make(map[[16]byte]fleetBindingVersionRead, len(fixture.observed.Records))
	for clientId, record := range fixture.observed.Records {
		observation.Records[clientId] = record
	}
	observation.Renewal.Round++
	observation.Renewal.SourcePlanHash = base.PlanHash
	observation.Renewal.ObservedEpoch = fixture.renewal.ValidFromEpoch
	observation.Renewal.ValidFromEpoch += 4
	observation.Renewal.ValidToEpoch += 4
	observation.Renewal.EVMHead.Number += 10
	for _, action := range base.Actions {
		if action.ID == "campaign.evm-gas-reserve" {
			observation.Renewal.CampaignReserveBeforeWei = action.Spend.EVMGasWei
		}
	}
	planned := fixture.renewal.Fleets[fleetLifecycleTargetFleet-1]
	manifest, err := protocol.ParseFleetManifest(planned.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	for memberIndex := range 2 {
		clientId := manifest.Members[memberIndex].ClientID
		record := observation.Records[clientId]
		record.Record.ValidToEpoch = fixture.renewal.ValidFromEpoch - 1
		observation.Records[clientId] = record
		action := actionByID(t, base, fleetRenewalActionID(fixture.renewal.Round, fleetLifecycleTargetFleet, "revoke", memberIndex+1))
		observation.JournalEntries = append(observation.JournalEntries, JournalEntry{PlanHash: base.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageFinalized,
			TransactionHash: common.Hash{0x70, byte(memberIndex + 1)}.Hex(), BlockNumber: observation.Renewal.EVMHead.Number - 1, BlockHash: common.Hash{0x71}.Hex()})
	}
	return fixture, base, observation
}

// Old code required signed validTo to equal the already-revoked chain value.
func TestFleetRenewalPlansSuccessorAfterPartialRevocation(t *testing.T) {
	fixture, base, observation := fleetRenewalPartialPrestateFixture(t)
	renewal, err := prepareFleetRenewal(fixture.cfg, fixture.stateDir, base, fixture.roles, observation)
	if err != nil {
		t.Fatal(err)
	}
	fleet := renewal.Fleets[fleetLifecycleTargetFleet-1]
	for index, member := range fleet.Members {
		original := fixture.renewal.Fleets[fleetLifecycleTargetFleet-1].Members[index].Prior
		if !finalJSONEqual(member.Prior, original) {
			t.Fatal("successor rewrote the original dual-signed predecessor")
		}
		if index < 2 {
			if member.PriorRevocation == nil || member.PriorRevocation.EffectiveEpoch != fixture.renewal.ValidFromEpoch || member.RevokeSignature != "" {
				t.Fatal("finalized predecessor cutoff was lost or revoked again")
			}
		} else if member.PriorRevocation != nil || member.RevokeSignature == "" {
			t.Fatal("unrevoked sibling borrowed another member's cutoff")
		}
	}
	actions, err := fleetRenewalActions(base, renewal)
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range actions {
		if action.ID == fleetRenewalActionID(renewal.Round, fleetLifecycleTargetFleet, "revoke", 1) || action.ID == fleetRenewalActionID(renewal.Round, fleetLifecycleTargetFleet, "revoke", 2) {
			t.Fatal("successor planned a redundant invalid revoke of the shortened lease")
		}
	}
	if err := validateFleetRenewalFreshPrestate(renewal, observation); err != nil {
		t.Fatal(err)
	}
	manifest, err := protocol.ParseFleetManifest(fleet.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	record := observation.Records[manifest.Members[0].ClientID]
	record.Record.ValidToEpoch--
	observation.Records[manifest.Members[0].ClientID] = record
	if err := validateFleetRenewalFreshPrestate(renewal, observation); err == nil {
		t.Fatal("successor accepted another unexplained cutoff after approval")
	}
}

// Observed state cannot manufacture consent or finality for a partial action.
func TestFleetRenewalPartialPredecessorRequiresExactFinalizedConsent(t *testing.T) {
	fixture, base, original := fleetRenewalPartialPrestateFixture(t)
	for _, fault := range []string{"missing", "unfinalized", "foreign-plan", "wrong-intent", "future-block"} {
		observation := original
		observation.JournalEntries = append([]JournalEntry(nil), original.JournalEntries...)
		switch fault {
		case "missing":
			observation.JournalEntries = nil
		case "unfinalized":
			observation.JournalEntries[0].Stage = StageBroadcast
		case "foreign-plan":
			observation.JournalEntries[0].PlanHash = common.Hash{0x79}.Hex()
		case "wrong-intent":
			observation.JournalEntries[0].IntentHash = common.Hash{0x7a}.Hex()
		case "future-block":
			observation.JournalEntries[0].BlockNumber = observation.Renewal.EVMHead.Number + 1
		}
		if _, err := prepareFleetRenewal(fixture.cfg, fixture.stateDir, base, fixture.roles, observation); err == nil {
			t.Fatalf("partial predecessor accepted %s evidence", fault)
		}
	}
	renewal, err := prepareFleetRenewal(fixture.cfg, fixture.stateDir, base, fixture.roles, original)
	if err != nil {
		t.Fatal(err)
	}
	renewal.Fleets[fleetLifecycleTargetFleet-1].Members[0].PriorRevocation.ClientSignature = "0x" + strings.Repeat("00", 64)
	if _, err := fleetRenewalActions(base, renewal); err == nil {
		t.Fatal("approved successor accepted a forged client revoke signature")
	}
}

// The chain proof must reproduce the exact original-generation revoke event.
func TestFleetRenewalPredecessorRechecksRevocationReceipt(t *testing.T) {
	fixture := newFleetPredecessorReadFixture(t, 1)
	prior := fixture.renewal.Fleets[0].Members[0].Prior
	proof := &FleetRenewalPriorRevocation{PlanHash: fixture.base.PlanHash, ActionId: "fleet.renew.1.1.revoke.1", IntentHash: common.Hash{0x61}.Hex(), EffectiveEpoch: 15,
		TransactionHash: common.Hash{0x62}.Hex(), BlockNumber: 10, BlockHash: common.Hash{0x63}.Hex()}
	fixture.renewal.EVMHead.Number = 20
	fixture.renewal.Fleets[0].Members[0].PriorRevocation = proof
	fixture.executor.journal.entries = append(fixture.executor.journal.entries, JournalEntry{PlanHash: proof.PlanHash, ActionID: proof.ActionId, IntentHash: proof.IntentHash, Stage: StageFinalized,
		TransactionHash: proof.TransactionHash, BlockNumber: proof.BlockNumber, BlockHash: proof.BlockHash})
	contractAbi, err := stabi.STCoordinatorMetaData.ParseABI()
	if err != nil {
		t.Fatal(err)
	}
	event := contractAbi.Events["FleetBindingRevoked"]
	data, err := event.Inputs.NonIndexed().Pack(prior.Generation, proof.EffectiveEpoch)
	if err != nil {
		t.Fatal(err)
	}
	receipt := &ethTypes.Receipt{Status: ethTypes.ReceiptStatusSuccessful, TxHash: common.HexToHash(proof.TransactionHash), BlockNumber: new(big.Int).SetUint64(proof.BlockNumber), BlockHash: common.HexToHash(proof.BlockHash),
		Logs: []*ethTypes.Log{{Address: fixture.executor.plan.Deployment.CoordinatorProxy, Topics: []common.Hash{event.ID, {1}}, Data: data}}}
	fixture.receiptKVs[proof.TransactionHash] = receipt
	fixture.blockHashKVs[proof.BlockNumber] = proof.BlockHash
	if err := fixture.executor.verifyFleetRenewalPredecessors(fixture.ctx, fixture.renewal, fixture.base); err != nil {
		t.Fatal(err)
	}
	receipt.Logs = []*ethTypes.Log{}
	if err := fixture.executor.verifyFleetRenewalPredecessors(context.WithoutCancel(fixture.ctx), fixture.renewal, fixture.base); err == nil || !strings.Contains(err.Error(), "FleetBindingRevoked") {
		t.Fatalf("missing historical revoke event accepted: %v", err)
	}
}
