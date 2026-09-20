// Historical commitment consumption retains the original plan while accepting
// only authenticated, same-intent completion in its approved continuation.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// Holds a failed original consumer followed by its verified descendant.
type fleetCommitmentHistoryFixture struct {
	executor       *Executor
	approved       *SetupPlan
	consumer       Action
	consumerEntry  JournalEntry
	consumerRecord *ActionPostcondition
}

// Uses synthetic plan identities and persisted receipts without any chain.
func newFleetCommitmentHistoryFixture(t *testing.T, fleet int, generation uint64) fleetCommitmentHistoryFixture {
	t.Helper()
	cfg := testResolvedConfig(t)
	cfg.Config.Topology.HeadFleets = 10
	parameters := map[string]string{fleetCommitmentStorageParameter: fleetCommitmentStorageV2}
	commitmentId, target, consumerId := fmt.Sprintf("fleet.commitment.%d", fleet), fmt.Sprintf("head-fleet:%d", fleet), "fleet.install.batch.1"
	if generation == 2 {
		commitmentId, consumerId = fmt.Sprintf("fleet.refresh.commitment.%d", fleet), "fleet.refresh.batch.1"
		parameters["generation"] = "2"
		parameters[fleetCommitmentParallelGroupParameter] = "refresh-1"
	} else if fleet <= cfg.Config.Topology.HeadFleets {
		parameters[fleetCommitmentParallelGroupParameter] = "install-1"
	} else {
		target, consumerId = fmt.Sprintf("challenger-fleet:%d", fleet), fmt.Sprintf("fleet.mirror.%d", fleet)
	}
	commitment := testFleetSupersessionAction(t, Action{ID: commitmentId, Kind: "substrate-extrinsic", Target: target, Parameters: parameters})
	consumer := testFleetSupersessionAction(t, Action{ID: consumerId, Kind: "evm-transaction", Target: common.Address{7}.Hex(), Parameters: map[string]string{"generation": fmt.Sprint(generation)}})
	source := &SetupPlan{PlanHash: common.Hash{1}.Hex(), DeploymentID: cfg.Config.Deployment.DeploymentID, Actions: []Action{commitment, consumer}}
	approved := &SetupPlan{
		PlanHash: common.Hash{3}.Hex(), DeploymentID: source.DeploymentID,
		PriorPlanHashes: []string{source.PlanHash, common.Hash{2}.Hex()}, Actions: slices.Clone(source.Actions),
	}
	executor := &Executor{
		cfg: cfg, stateDir: t.TempDir(), plan: source,
		fleetCommitmentHistory: &fleetCommitmentHistoryScope{plan: approved, fleets: []int{fleet}},
	}
	entry := JournalEntry{Sequence: 4, PlanHash: common.Hash{2}.Hex(), ActionID: consumer.ID, IntentHash: consumer.IntentHash, Stage: StageVerified}
	record := testFleetSupersessionPostcondition(cfg, consumer, entry, 110, map[string]any{"generation": generation, "consumed": true})
	var err error
	entry.PostconditionPath, entry.PostconditionHash, err = executor.persistActionPostcondition(record)
	if err != nil {
		t.Fatal(err)
	}
	executor.journal = &Journal{entries: []JournalEntry{
		{Sequence: 1, PlanHash: source.PlanHash, ActionID: commitment.ID, IntentHash: commitment.IntentHash, Stage: StageVerified},
		{Sequence: 2, PlanHash: source.PlanHash, ActionID: consumer.ID, IntentHash: consumer.IntentHash, Stage: StageFailed},
		entry,
	}}
	return fleetCommitmentHistoryFixture{executor: executor, approved: approved, consumer: consumer, consumerEntry: entry, consumerRecord: record}
}

// Initial batches, refresh batches, and challenger mirrors share the defect.
func TestFleetCommitmentHistoryConsumesApprovedDescendant(t *testing.T) {
	for _, coordinates := range []struct {
		fleet      int
		generation uint64
	}{
		{fleet: 1, generation: 1},
		{fleet: 1, generation: 2},
		{fleet: 11, generation: 1},
	} {
		fixture := newFleetCommitmentHistoryFixture(t, coordinates.fleet, coordinates.generation)
		sourceBefore, err := json.Marshal(fixture.executor.plan)
		if err != nil {
			t.Fatal(err)
		}
		original := *fixture.executor
		original.fleetCommitmentHistory = nil
		if _, consumed, err := original.consumedFleetCommitmentGeneration(coordinates.fleet, coordinates.generation); err != nil || consumed {
			t.Fatalf("original plan unexpectedly sees descendant: consumed=%t err=%v", consumed, err)
		}
		for _, reader := range []*Executor{fixture.executor, fixture.executor.independentReadExecutor()} {
			consumerId, consumed, err := reader.consumedFleetCommitmentGeneration(coordinates.fleet, coordinates.generation)
			if err != nil || !consumed || consumerId != fixture.consumer.ID {
				t.Fatalf("fleet %d generation %d descendant consumer: id=%s consumed=%t err=%v", coordinates.fleet, coordinates.generation, consumerId, consumed, err)
			}
		}
		sourceAfter, err := json.Marshal(fixture.executor.plan)
		if err != nil || string(sourceBefore) != string(sourceAfter) {
			t.Fatal("consumer lookup mutated the original plan")
		}
		if exactVerifiedPlanAction(fixture.executor.plan, fixture.executor.journal.Entries(), fixture.consumer.ID) {
			t.Fatal("consumer lookup broadened the original plan ancestry")
		}
	}
}

// An unrelated plan, fleet, action, intent, or incomplete stage never consumes.
func TestFleetCommitmentHistoryRejectsAdjacentConsumers(t *testing.T) {
	for _, change := range []struct {
		name   string
		mutate func(*fleetCommitmentHistoryFixture)
	}{
		{name: "unapproved plan", mutate: func(f *fleetCommitmentHistoryFixture) { f.executor.journal.entries[2].PlanHash = common.Hash{9}.Hex() }},
		{name: "changed intent", mutate: func(f *fleetCommitmentHistoryFixture) {
			f.executor.journal.entries[2].IntentHash = common.Hash{9}.Hex()
		}},
		{name: "other consumer", mutate: func(f *fleetCommitmentHistoryFixture) {
			f.executor.journal.entries[2].ActionID = "fleet.install.batch.2"
		}},
		{name: "failed consumer", mutate: func(f *fleetCommitmentHistoryFixture) { f.executor.journal.entries[2].Stage = StageFailed }},
		{name: "only finalized", mutate: func(f *fleetCommitmentHistoryFixture) { f.executor.journal.entries[2].Stage = StageFinalized }},
		{name: "missing consumer", mutate: func(f *fleetCommitmentHistoryFixture) { f.executor.journal.entries = f.executor.journal.entries[:2] }},
		{name: "unrelated fleet", mutate: func(f *fleetCommitmentHistoryFixture) { f.executor.fleetCommitmentHistory.fleets = []int{2} }},
	} {
		fixture := newFleetCommitmentHistoryFixture(t, 1, 1)
		change.mutate(&fixture)
		if consumerId, consumed, err := fixture.executor.consumedFleetCommitmentGeneration(1, 1); err != nil || consumed || consumerId != fixture.consumer.ID {
			t.Fatalf("%s: id=%s consumed=%t err=%v", change.name, consumerId, consumed, err)
		}
	}
}

// Scope cannot approve a foreign source or reinterpret its consumer action.
func TestFleetCommitmentHistoryRejectsChangedApproval(t *testing.T) {
	for _, change := range []struct {
		name   string
		mutate func(*fleetCommitmentHistoryFixture)
	}{
		{name: "missing scope plan", mutate: func(f *fleetCommitmentHistoryFixture) { f.executor.fleetCommitmentHistory.plan = nil }},
		{name: "unapproved source", mutate: func(f *fleetCommitmentHistoryFixture) {
			f.approved.PriorPlanHashes = []string{f.consumerEntry.PlanHash}
		}},
		{name: "foreign deployment", mutate: func(f *fleetCommitmentHistoryFixture) { f.approved.DeploymentID = "different-synthetic-deployment" }},
		{name: "changed current intent", mutate: func(f *fleetCommitmentHistoryFixture) { f.approved.Actions[1].IntentHash = common.Hash{9}.Hex() }},
		{name: "missing current consumer", mutate: func(f *fleetCommitmentHistoryFixture) { f.approved.Actions = f.approved.Actions[:1] }},
		{name: "duplicate current consumer", mutate: func(f *fleetCommitmentHistoryFixture) { f.approved.Actions = append(f.approved.Actions, f.consumer) }},
		{name: "missing original consumer", mutate: func(f *fleetCommitmentHistoryFixture) { f.executor.plan.Actions = f.executor.plan.Actions[:1] }},
		{name: "duplicate original consumer", mutate: func(f *fleetCommitmentHistoryFixture) {
			f.executor.plan.Actions = append(f.executor.plan.Actions, f.consumer)
		}},
	} {
		fixture := newFleetCommitmentHistoryFixture(t, 1, 1)
		change.mutate(&fixture)
		if _, consumed, err := fixture.executor.consumedFleetCommitmentGeneration(1, 1); err == nil || consumed {
			t.Fatalf("%s: consumed=%t err=%v", change.name, consumed, err)
		}
	}
}

// Receipt identity, bytes, and canonical journal binding remain mandatory.
func TestFleetCommitmentHistoryAuthenticatesConsumerReceipt(t *testing.T) {
	for _, change := range []string{"missing path", "missing file", "changed hash", "changed observation", "changed identity"} {
		fixture := newFleetCommitmentHistoryFixture(t, 1, 1)
		path := filepath.Join(fixture.executor.stateDir, filepath.FromSlash(fixture.consumerEntry.PostconditionPath))
		switch change {
		case "missing path":
			fixture.executor.journal.entries[2].PostconditionPath = ""
		case "missing file":
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		case "changed hash":
			fixture.executor.journal.entries[2].PostconditionHash = common.Hash{9}.Hex()
		case "changed observation":
			fixture.consumerRecord.Observed["consumed"] = false
			if err := writePublicJSON(path, fixture.consumerRecord); err != nil {
				t.Fatal(err)
			}
		case "changed identity":
			fixture.consumerRecord.IntentHash = common.Hash{9}.Hex()
			if err := writePublicJSON(path, fixture.consumerRecord); err != nil {
				t.Fatal(err)
			}
		}
		if _, consumed, err := fixture.executor.consumedFleetCommitmentGeneration(1, 1); err == nil || consumed {
			t.Fatalf("%s: consumed=%t err=%v", change, consumed, err)
		}
	}
}

// A carried intent alias cannot replace the exact original consumer intent.
func TestFleetCommitmentHistoryRequiresOriginalConsumerIntent(t *testing.T) {
	fixture := newFleetCommitmentHistoryFixture(t, 1, 1)
	alias := common.Hash{9}.Hex()
	fixture.executor.plan.Actions[1].AcceptedPriorIntentHashes = []string{alias}
	fixture.approved.Actions[1].AcceptedPriorIntentHashes = []string{alias}
	fixture.executor.journal.entries[2].IntentHash = alias
	if _, consumed, err := fixture.executor.consumedFleetCommitmentGeneration(1, 1); err != nil || consumed {
		t.Fatalf("aliased consumer: consumed=%t err=%v", consumed, err)
	}
	aliasEntry := fixture.executor.journal.entries[2]
	aliasEntry.Sequence = fixture.consumerEntry.Sequence + 1
	fixture.executor.journal.entries[2] = fixture.consumerEntry
	fixture.executor.journal.entries = append(fixture.executor.journal.entries, aliasEntry)
	if _, consumed, err := fixture.executor.consumedFleetCommitmentGeneration(1, 1); err != nil || !consumed {
		t.Fatalf("exact consumer before later alias: consumed=%t err=%v", consumed, err)
	}
	if len(fixture.executor.plan.Actions[1].AcceptedPriorIntentHashes) != 1 {
		t.Fatal("exact lookup mutated source intent aliases")
	}
}

// Completed renewal is the only producer of descendant consumption scope.
func TestFleetCommitmentHistoryScopeRequiresCompletedRenewal(t *testing.T) {
	fixture := newFleetCommitmentRecoveryFixture(t, 1, 1)
	current := *fixture.plan
	current.PlanHash = common.Hash{3}.Hex()
	current.PriorPlanHashes = append(slices.Clone(current.PriorPlanHashes), fixture.plan.PlanHash, common.Hash{2}.Hex())
	current.Actions = slices.Clone(current.Actions)
	current.FleetRenewals = []FleetRenewal{{Round: 1, Fleets: []FleetRenewalFleet{{Fleet: 1, Members: []FleetRenewalMember{{Miner: 1}}}}}}
	executor := &Executor{cfg: fixture.cfg, stateDir: fixture.stateDir, plan: &current, journal: &Journal{}}
	sourceEntry := fixture.entries[1]
	sourceRecord := testFleetSupersessionPostcondition(fixture.cfg, fixture.action, sourceEntry, 100, map[string]any{"commitment_block": uint64(100)})
	var err error
	sourceEntry.PostconditionPath, sourceEntry.PostconditionHash, err = executor.persistActionPostcondition(sourceRecord)
	if err != nil {
		t.Fatal(err)
	}
	consumer := actionByID(t, fixture.plan, "fleet.install.batch.1")
	consumerEntry := JournalEntry{Sequence: 4, PlanHash: common.Hash{2}.Hex(), ActionID: consumer.ID, IntentHash: consumer.IntentHash, Stage: StageVerified}
	consumerRecord := testFleetSupersessionPostcondition(fixture.cfg, consumer, consumerEntry, 110, map[string]any{"consumed": true})
	consumerEntry.PostconditionPath, consumerEntry.PostconditionHash, err = executor.persistActionPostcondition(consumerRecord)
	if err != nil {
		t.Fatal(err)
	}
	executor.journal.entries = []JournalEntry{sourceEntry,
		{Sequence: 3, PlanHash: fixture.plan.PlanHash, ActionID: consumer.ID, IntentHash: consumer.IntentHash, Stage: StageFailed}, consumerEntry}
	for index, operation := range []string{"commitment", "mirror", "bind"} {
		member := 0
		if operation == "bind" {
			member = 1
		}
		next := testFleetSupersessionAction(t, Action{ID: fleetRenewalActionID(1, 1, operation, member), Kind: "fixture", Target: "synthetic-renewal"})
		current.Actions = append(current.Actions, next)
		entry := JournalEntry{Sequence: uint64(10 + 2*index), PlanHash: current.PlanHash, ActionID: next.ID, IntentHash: next.IntentHash, Stage: StageFinalized,
			TransactionHash: common.Hash{byte(10 + index)}.Hex(), BlockNumber: 120, BlockHash: common.Hash{5}.Hex()}
		executor.journal.entries = append(executor.journal.entries, entry)
		entry.Sequence++
		entry.Stage = StageVerified
		record := testFleetSupersessionPostcondition(fixture.cfg, next, entry, 120, map[string]any{"complete": true})
		entry.PostconditionPath, entry.PostconditionHash, err = executor.persistActionPostcondition(record)
		if err != nil {
			t.Fatal(err)
		}
		executor.journal.entries = append(executor.journal.entries, entry)
	}
	before, err := json.Marshal(sourceRecord)
	if err != nil {
		t.Fatal(err)
	}
	completeEntries := slices.Clone(executor.journal.entries)
	for index, entry := range completeEntries {
		if entry.Stage != StageVerified || entry.Sequence < 10 {
			continue
		}
		executor.journal.entries = slices.Clone(completeEntries)
		executor.journal.entries[index].Stage = StageFailed
		if source, handled, err := executor.fleetRenewalHistoricalSource(fixture.action, sourceEntry, sourceRecord); err != nil || handled || source != nil {
			t.Fatalf("incomplete %s granted historical scope: handled=%t err=%v", entry.ActionID, handled, err)
		}
	}
	executor.journal.entries = completeEntries
	source, handled, err := executor.fleetRenewalHistoricalSource(fixture.action, sourceEntry, sourceRecord)
	if err != nil || !handled || source == nil {
		t.Fatalf("completed renewal refused original history: handled=%t err=%v", handled, err)
	}
	if source.plan.PlanHash != fixture.plan.PlanHash || source.fleetCommitmentHistory == nil || source.fleetCommitmentHistory.plan != &current || !slices.Equal(source.fleetCommitmentHistory.fleets, []int{1}) || executor.fleetCommitmentHistory != nil {
		t.Fatal("historical source changed plan identity or leaked its consumer scope")
	}
	if _, consumed, err := source.consumedFleetCommitmentGeneration(1, 1); err != nil || !consumed {
		t.Fatalf("completed renewal lost the descendant consumer: consumed=%t err=%v", consumed, err)
	}
	after, err := json.Marshal(sourceRecord)
	if err != nil || string(before) != string(after) {
		t.Fatal("historical source changed the recorded observation")
	}
	if hash, err := canonicalHashHex(sourceRecord); err != nil || hash != sourceEntry.PostconditionHash {
		t.Fatal("historical source changed the exact receipt hash")
	}
	// A completed immutable resolution is safe to reuse inside this collector:
	// its cache must reconstruct a fresh scoped executor instead of rescanning
	// the complete successor journal for every carried predecessor.
	executor.journal = nil
	cached, handled, err := executor.fleetRenewalHistoricalSource(fixture.action, sourceEntry, sourceRecord)
	if err != nil || !handled || cached == nil || cached == source || cached.plan.PlanHash != fixture.plan.PlanHash || cached.fleetCommitmentHistory == nil || cached.fleetCommitmentHistory.plan != &current || !slices.Equal(cached.fleetCommitmentHistory.fleets, []int{1}) {
		t.Fatalf("completed renewal cache lost immutable source scope: handled=%t err=%v", handled, err)
	}
}

// Mirrors, member bindings, lifecycle, and batches retain exact fleet scope;
// the newly approved renewal itself never becomes historical through this path.
func TestFleetCommitmentHistoryScopeRetainsAdjacentActionFamilies(t *testing.T) {
	cfg := testResolvedConfig(t)
	cfg.Config.Topology.HeadFleets = 10
	for _, fixture := range []struct {
		actionId string
		fleets   []int
	}{
		{actionId: "fleet.commitment.1", fleets: []int{1}},
		{actionId: "fleet.refresh.commitment.1", fleets: []int{1}},
		{actionId: "fleet.mirror.11", fleets: []int{11}},
		{actionId: "fleet.bind.1.1", fleets: []int{1}},
		{actionId: "fleet.install.batch.1", fleets: []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}},
		{actionId: "fleet.refresh.batch.1", fleets: []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}},
		{actionId: "lifecycle.prepare.target.commitment", fleets: []int{fleetLifecycleTargetFleet}},
		{actionId: "lifecycle.prepare.companion.mirror", fleets: []int{fleetLifecycleCompanionFleet}},
		{actionId: "lifecycle.provider.bind.1", fleets: []int{fleetLifecycleTargetFleet}},
		{actionId: "lifecycle.terminal.installed", fleets: []int{fleetLifecycleCompanionFleet}},
		{actionId: fleetRenewalActionID(1, 1, "commitment", 0)},
		{actionId: fleetRenewalActionID(1, 1, "mirror", 0)},
		{actionId: fleetRenewalActionID(1, 1, "bind", 1)},
		{actionId: "production.launch"},
	} {
		fleets, err := fleetRenewalHistoricalActionFleets(cfg, Action{ID: fixture.actionId})
		if err != nil || !slices.Equal(fleets, fixture.fleets) {
			t.Fatalf("%s: fleets=%v err=%v, want %v", fixture.actionId, fleets, err, fixture.fleets)
		}
	}
}
