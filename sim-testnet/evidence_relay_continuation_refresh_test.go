//go:build linux || darwin

package main

// Real retained request files and the append-only journal exercise refresh
// ancestry. Clock/capacity controls keep complete work and lifetime independent.

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	validatorcomponent "github.com/urfoundation/sn/validator"
)

// Reuse the actual admitted signature instead of signing its header twice.
func newEvidenceRelayRefreshTest(t *testing.T) (*runtimeEvidenceProvisionV2TestFixture, *Executor) {
	t.Helper()
	fixture, executor, current := evidenceRelayContinuationTest(t)
	_, requests, err := readEvidenceRelayContinuationDebits(t.Context(), fixture.stateDir, executor.plan, executor.journal.Entries())
	if err != nil { t.Fatal(err) }
	for _, request := range requests {
		slot, err := request.Evidence.Header.SlotKey()
		if err != nil { t.Fatal(err) }
		for index, previous := range current.Retained {
			priorSlot, err := previous.Evidence.Header.SlotKey()
			if err != nil { t.Fatal(err) }
			if priorSlot == slot { current.Retained[index] = request }
		}
	}
	current.Retained, err = canonicalEvidenceRelayContinuationRequests(current.Retained)
	if err != nil { t.Fatal(err) }
	plan, err := appendEvidenceRelayContinuationPlan(executor.plan, current)
	if err != nil { t.Fatal(err) }
	if err := writeRunInputs(fixture.cfg, fixture.stateDir, plan, fixture.roles); err != nil { t.Fatal(err) }
	executor.plan, err = readValidatorEvidenceHistoricalPlan(fixture.stateDir, plan.PlanHash)
	if err != nil { t.Fatal(err) }
	return fixture, executor
}

// Construct the capture's bounded output from the real cumulative journal;
// production capture additionally authenticates both pinned chains and stores.
func evidenceRelayRefreshRequestTest(t *testing.T, executor *Executor) EvidenceRelayContinuation {
	t.Helper()
	raw, err := json.Marshal(executor.plan.EvidenceRelayContinuation)
	if err != nil { t.Fatal(err) }
	var current EvidenceRelayContinuation
	if err := json.Unmarshal(raw, &current); err != nil { t.Fatal(err) }
	current.Schema = evidenceRelayContinuationRefreshSchema
	current.SourcePlanHash = executor.plan.PlanHash
	entries := executor.journal.Entries()
	current.JournalHash = entries[len(entries)-1].EntryHash
	current.EVMHead.Number += 720
	current.NativeHead.Number += 720
	current.EndBlock += 720
	work, err := evidenceRelayConfiguredWork(executor.cfg)
	if err != nil { t.Fatal(err) }
	closed, err := evidenceRelayContinuationCeil(720, work.settlementCadence)
	if err != nil { t.Fatal(err) }
	native, err := evidenceRelayContinuationCeil(720, work.nativeCadence)
	if err != nil { t.Fatal(err) }
	current.SettlementEpoch += closed
	current.EndSettlementEpoch += closed
	current.NativeEpoch += native
	current.EndNativeEpoch += native
	var requests []validatorcomponent.ValidatorEvidenceTransactionV2Expected
	current.Debits, requests, err = readEvidenceRelayContinuationDebits(t.Context(), executor.stateDir, executor.plan, entries)
	if err != nil { t.Fatal(err) }
	current.Retained, err = canonicalEvidenceRelayContinuationRequests(append(current.Retained, requests...))
	if err != nil { t.Fatal(err) }
	current.NewSlots, current.HistoricalLiabilityWei, err = current.remainingSlots()
	if err != nil { t.Fatal(err) }
	if err := current.validateClocks(work); err != nil { t.Fatal(err) }
	return current
}

// Three successive exact appends retain old 100 gwei and continued 25 gwei
// debit owners, ordinary finalized actions, and every immutable ancestor end.
func TestEvidenceRelayContinuationRefreshPreservesEveryApprovalAndDebit(t *testing.T) {
	fixture, executor := newEvidenceRelayRefreshTest(t)
	first := executor.plan
	firstBytes, err := json.Marshal(first)
	if err != nil { t.Fatal(err) }
	ordinary, err := exactPlanActionByID(first, "evm.fund-deployer")
	if err != nil { t.Fatal(err) }
	if err := executor.journal.Append(JournalEntry{DeploymentID: first.DeploymentID, PlanHash: first.PlanHash, ActionID: ordinary.ID, IntentHash: ordinary.IntentHash, Stage: StageFinalized, TransactionHash: common.Hash{0x81}.Hex(), BlockNumber: 100, BlockHash: common.Hash{0x82}.Hex()}); err != nil { t.Fatal(err) }
	next := evidenceRelayLaunchRequestTest(t, fixture, fixture.prepared.Members[0], fixture.prepared.Epoch+1, false, 0)
	action, owner, err := executor.admitOwnedEvidenceRelayAction(t.Context(), next)
	if err != nil || owner != first.PlanHash || action.Spend.EVMGasWei != "25000000000000000" { t.Fatal("initial continuation admission changed", err) }
	journalPath := filepath.Join(fixture.stateDir, "journal.jsonl")
	journalBytes, err := os.ReadFile(journalPath)
	if err != nil { t.Fatal(err) }
	for generation := 0; generation < 3; generation++ {
		if generation == 1 {
			// An ordinary release revision carries the previous continuation
			// unchanged before the next explicit refresh names that new owner.
			raw, err := json.Marshal(executor.plan)
			if err != nil { t.Fatal(err) }
			var revised SetupPlan
			if err := json.Unmarshal(raw, &revised); err != nil { t.Fatal(err) }
			revised.PriorPlanHashes = append(revised.PriorPlanHashes, executor.plan.PlanHash)
			revised.GeneratedAt = "2026-09-15T22:00:00Z"
			revised.PlanHash = ""
			revised.PlanHash, err = revised.hash()
			if err != nil { t.Fatal(err) }
			if err := writeRunInputs(fixture.cfg, fixture.stateDir, &revised, fixture.roles); err != nil { t.Fatal(err) }
			executor.plan = &revised
		}
		previous := executor.plan
		current := evidenceRelayRefreshRequestTest(t, executor)
		refreshed, err := appendEvidenceRelayContinuationPlan(previous, current)
		if err != nil { t.Fatal(err) }
		if current.NewSlots != 1019 || current.HistoricalLiabilityWei != "125000000000000000" || len(current.Debits) != 2 || !reflect.DeepEqual(previous.Actions, refreshed.Actions) || !refreshed.allowedPlanHashes()[first.PlanHash] || !refreshed.allowedPlanHashes()[current.ActivationPlanHash] { t.Fatal("refresh reset liability, actions, or ancestry") }
		if err := writeRunInputs(fixture.cfg, fixture.stateDir, refreshed, fixture.roles); err != nil { t.Fatal(err) }
		executor.plan = refreshed
		if err := validateEvidenceRelayContinuationSource(fixture.stateDir, refreshed, executor.journal.Entries()); err != nil { t.Fatalf("generation %d lost its authenticated predecessor: %v", generation, err) }
		retained, retainedOwner, err := executor.admitOwnedEvidenceRelayAction(t.Context(), next)
		if err != nil || retainedOwner != first.PlanHash || retained.IntentHash != action.IntentHash { t.Fatal("refresh reassigned an existing 25 gwei retry", err) }
		for _, debit := range current.Debits {
			_, record, _, err := readOwnedEvidenceRelayRequest(t.Context(), fixture.stateDir, refreshed, executor.journal.Entries(), debit.ActionID)
			if err != nil { t.Fatal(err) }
			retained, originalOwner, err := executor.admitOwnedEvidenceRelayAction(t.Context(), record.Evidence)
			if err != nil || originalOwner != debit.PlanHash || retained.Spend.EVMGasWei != debit.AllowanceWei { t.Fatal("refresh rewrote an admitted signed liability", err) }
		}
	}
	retainedJournal, err := os.ReadFile(journalPath)
	if err != nil || !bytes.Equal(journalBytes, retainedJournal) { t.Fatal("refresh changed finalized history or re-debited a retry", err) }
	retainedFirst, err := readValidatorEvidenceHistoricalPlan(fixture.stateDir, first.PlanHash)
	if err != nil { t.Fatal(err) }
	retainedBytes, err := json.Marshal(retainedFirst)
	if err != nil || !bytes.Equal(firstBytes, retainedBytes) { t.Fatal("refresh replaced the original approval or its fixed end", err) }
	newRequest := evidenceRelayLaunchRequestTest(t, fixture, fixture.prepared.Members[0], fixture.prepared.Epoch+2, false, 0)
	newAction, newOwner, err := executor.admitOwnedEvidenceRelayAction(t.Context(), newRequest)
	if err != nil || newOwner != executor.plan.PlanHash || newAction.Spend.EVMGasWei != "25000000000000000" { t.Fatal("refreshed approval failed to own a bounded new subject", err) }
	remaining := evidenceRelayRefreshRequestTest(t, executor)
	if remaining.NewSlots != 1018 || remaining.HistoricalLiabilityWei != "150000000000000000" { t.Fatal("new subject was not charged cumulatively") }
}

// A syntactically balanced refund is still invalid when the exact journal
// checkpoint contains a newer admission absent from the proposed debit census.
func TestEvidenceRelayContinuationRefreshRejectsOmittedCheckpointDebit(t *testing.T) {
	fixture, executor := newEvidenceRelayRefreshTest(t)
	next := evidenceRelayLaunchRequestTest(t, fixture, fixture.prepared.Members[0], fixture.prepared.Epoch+1, false, 0)
	admitted, _, err := executor.admitOwnedEvidenceRelayAction(t.Context(), next)
	if err != nil { t.Fatal(err) }
	current := evidenceRelayRefreshRequestTest(t, executor)
	valid, err := appendEvidenceRelayContinuationPlan(executor.plan, current)
	if err != nil { t.Fatal(err) }
	if err := writeRunInputs(fixture.cfg, fixture.stateDir, valid, fixture.roles); err != nil { t.Fatal(err) }
	if err := validateEvidenceRelayContinuationSource(fixture.stateDir, valid, executor.journal.Entries()); err != nil { t.Fatal(err) }
	for index, debit := range current.Debits {
		if debit.ActionID == admitted.ID { current.Debits = append(current.Debits[:index], current.Debits[index+1:]...); break }
	}
	current.NewSlots, current.HistoricalLiabilityWei, err = current.remainingSlots()
	if err != nil { t.Fatal(err) }
	omitted, err := appendEvidenceRelayContinuationPlan(executor.plan, current)
	if err != nil { t.Fatal("control must remain arithmetically valid before journal authentication", err) }
	if err := validateEvidenceRelayContinuationSource(fixture.stateDir, omitted, executor.journal.Entries()); err == nil || !strings.Contains(err.Error(), "omitted an admitted checkpoint liability") { t.Fatal("refresh refunded a post-predecessor journal admission", err) }
}

// Rejected changes cover each authority independently; passing the old wire
// version cannot silently gain refresh or lower-fee authority.
func TestEvidenceRelayContinuationRefreshRejectsChangedAuthorityAndHistory(t *testing.T) {
	_, executor := newEvidenceRelayRefreshTest(t)
	for _, sample := range []struct { name string; mutate func(*EvidenceRelayContinuation) }{
		{name: "old wire", mutate: func(c *EvidenceRelayContinuation) { c.Schema = evidenceRelayContinuationSchema }},
		{name: "unrelated predecessor", mutate: func(c *EvidenceRelayContinuation) { c.SourcePlanHash = common.Hash{0xff}.Hex() }},
		{name: "config", mutate: func(c *EvidenceRelayContinuation) { c.ConfigHash = common.Hash{0xff}.Hex() }},
		{name: "original money", mutate: func(c *EvidenceRelayContinuation) { c.OriginalReserve.Spend.EVMGasWei = "25700000000000000000" }},
		{name: "activation", mutate: func(c *EvidenceRelayContinuation) { c.ActivationPlanHash = common.Hash{0xff}.Hex() }},
		{name: "source", mutate: func(c *EvidenceRelayContinuation) { c.Sources[0].Activation.EVMBlock++ }},
		{name: "namespace", mutate: func(c *EvidenceRelayContinuation) { c.Sources[0].CoordinatorStateDir += "-replacement" }},
		{name: "intent prefix", mutate: func(c *EvidenceRelayContinuation) { c.Sources[0].IntentPrefixSHA256 = bytesSHA256([]byte("changed")) }},
		{name: "signed prefix", mutate: func(c *EvidenceRelayContinuation) { c.Sources[0].Capacity.Head.Root = common.Hash{0xff}.Hex() }},
		{name: "retained subject", mutate: func(c *EvidenceRelayContinuation) { c.Retained = c.Retained[1:] }},
		{name: "old debit", mutate: func(c *EvidenceRelayContinuation) { c.Debits = nil }},
		{name: "evm snapshot", mutate: func(c *EvidenceRelayContinuation) { c.EVMHead = executor.plan.EvidenceRelayContinuation.EVMHead }},
		{name: "native snapshot", mutate: func(c *EvidenceRelayContinuation) { c.NativeHead = executor.plan.EvidenceRelayContinuation.NativeHead }},
		{name: "end", mutate: func(c *EvidenceRelayContinuation) { c.EndBlock = executor.plan.EvidenceRelayContinuation.EndBlock }},
		{name: "complete work", mutate: func(c *EvidenceRelayContinuation) { c.RequiredWorkBlocks-- }},
	} {
		current := evidenceRelayRefreshRequestTest(t, executor)
		sample.mutate(&current)
		if _, err := appendEvidenceRelayContinuationPlan(executor.plan, current); err == nil { t.Fatalf("refresh accepted changed %s", sample.name) }
	}
	current := evidenceRelayRefreshRequestTest(t, executor)
	base := *executor.plan
	prior := *base.EvidenceRelayContinuation
	prior.Schema = "urnetwork-sim-evidence-relay-continuation-v2"
	base.EvidenceRelayContinuation = &prior
	if err := validateEvidenceRelayContinuationRefresh(&base, &current); err == nil { t.Fatal("refresh reinterpreted a 50 gwei approval") }
}

// Original plus continued admissions can exceed 256 while remaining below
// the already approved 1024 calls and exact original 25.6 TAO reserve.
func TestEvidenceRelayContinuationRefreshCountsContinuedDebitCensus(t *testing.T) {
	current := EvidenceRelayContinuation{Schema: evidenceRelayContinuationRefreshSchema, OriginalReserve: Action{Spend: Spend{EVMGasWei: "25600000000000000000"}}}
	for index := uint64(0); index < 257; index++ {
		current.Debits = append(current.Debits, EvidenceRelayContinuationDebit{PlanHash: common.Hash{1}.Hex(), ActionID: evidenceRelayActionPrefix + stringsTrim0x(common.Hash{byte(index >> 8), byte(index), 1}.Hex()), AllowanceWei: "25000000000000000"})
	}
	slots, liability, err := current.remainingSlots()
	if err != nil || slots != 767 || liability != "6425000000000000000" { t.Fatal("refresh lost cumulative continued slots", slots, liability, err) }
	current.Schema = evidenceRelayContinuationSchema
	if _, _, err := current.remainingSlots(); err == nil { t.Fatal("legacy first continuation gained a larger original debit census") }
}

// Capacity alone can remain admissible after preparation consumed full-work
// slack. The separate startup gate accepts the exact boundary and no less.
func TestEvidenceRelayContinuationRefreshRunwayRetainsCompleteWork(t *testing.T) {
	_, _, current := evidenceRelayContinuationTest(t)
	cutoff := current.EndBlock - current.RequiredWorkBlocks
	if err := validateEvidenceRelayContinuationRunway(&current, cutoff); err != nil { t.Fatal(err) }
	cfg := runtimeEvidenceLaunchConfigTest(t)
	bounds := cfg.Config.ValidatorEvidenceV2[0].Evidence.Bounds
	if err := validateEvidenceRelayContinuationCapacity(cfg, bounds, current.EndBlock-cutoff-1, current.Sources[0].Capacity); err != nil { t.Fatal("control capacity must still admit the shortened runtime", err) }
	for _, block := range []uint64{cutoff+1, current.EndBlock, current.EndBlock+1, current.EVMHead.Number-1} {
		if err := validateEvidenceRelayContinuationRunway(&current, block); err == nil { t.Fatalf("startup admitted incomplete full work at %d", block) }
	}
}
