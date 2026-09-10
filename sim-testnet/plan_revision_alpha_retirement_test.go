// Bound alpha retirement across verified stages, authenticated ancestors, and
// reconciliation aliases without turning historical evidence into replay work.
package main

import (
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Every original and repair family uses the same exact-effect accounting;
// duplicate verification and later journal stages do not multiply a ceiling.
func TestRetiredAlphaSpendCountsEveryTransferAndRepairFamilyOnce(t *testing.T) {
	f := newRetiredAlphaHistoryFixture(t)
	for _, baseId := range []string{"alpha.transfer.operator-deposit.1", "alpha.transfer.validator.1", "alpha.transfer.validator.2"} {
		appendAlphaHistoryTestRepair(t, f.ancestor, baseId)
	}
	entries := []JournalEntry{}
	var want uint64
	for _, action := range f.ancestor.Actions {
		if action.Spend.AlphaRao == 0 {
			continue
		}
		want += action.Spend.AlphaRao
		for _, stage := range []JournalStage{StageIntent, StageBroadcast, StageIncluded, StageFinalized, StageVerified, StageVerified, StageFailed} {
			entries = append(entries, JournalEntry{PlanHash: f.ancestor.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: stage})
		}
	}
	got, err := retiredVerifiedAlphaSpend(t.TempDir(), f.ancestor, nil, entries, 0)
	if err != nil || got != want {
		t.Fatalf("alpha families were omitted or counted repeatedly: got=%d want=%d error=%v", got, want, err)
	}
	got, err = retiredVerifiedAlphaSpend(t.TempDir(), f.ancestor, f.ancestor.Actions, entries, 0)
	if err != nil || got != 0 {
		t.Fatalf("exact active alpha ceilings were also retired: got=%d error=%v", got, err)
	}
}

// Old conservative reservations and newly retired active transfers are separate
// terms; a complete-generation retirement can impose a still higher floor.
func TestRetiredAlphaSpendPreservesOlderAndFullGenerationFloors(t *testing.T) {
	f := newRetiredAlphaHistoryFixture(t)
	f.prior.SupersededSpend.AlphaRao = 2 * f.omittedRepair.Spend.AlphaRao
	retired := actionByID(t, f.prior, "alpha.transfer.operator-deposit.1")
	active := []Action{}
	for _, action := range f.prior.Actions {
		if action.ID != retired.ID {
			active = append(active, action)
		}
	}
	want := f.prior.SupersededSpend.AlphaRao + retired.Spend.AlphaRao
	for _, floor := range []uint64{0, want - 1, want, want + 71} {
		got, err := retiredVerifiedAlphaSpend(f.stateDir, f.prior, active, f.entries, floor)
		if err != nil || got != max64(want, floor) {
			t.Fatalf("retirement floor=%d got=%d want=%d error=%v", floor, got, max64(want, floor), err)
		}
	}
	f.prior.SupersededSpend.AlphaRao = ^uint64(0)
	if _, err := retiredVerifiedAlphaSpend(f.stateDir, f.prior, active, f.entries, 0); err == nil || !strings.Contains(err.Error(), "overflows") {
		t.Fatalf("overflowing historical floor was accepted: %v", err)
	}
}

// Pending, failed, foreign-deployment, and foreign-plan rows cannot authorize
// retirement or force reads of an unauthenticated history directory.
func TestRetiredAlphaSpendIgnoresUnverifiedAndForeignJournalRows(t *testing.T) {
	f := newRetiredAlphaHistoryFixture(t)
	verified := JournalEntry{DeploymentID: f.prior.DeploymentID, PlanHash: f.ancestor.PlanHash, ActionID: f.omittedRepair.ID, IntentHash: f.omittedRepair.IntentHash, Stage: StageVerified}
	for _, stage := range []JournalStage{StageIntent, StageBroadcast, StageIncluded, StageFinalized, StageFailed} {
		entry := verified
		entry.Stage = stage
		got, err := retiredVerifiedAlphaSpend(t.TempDir(), f.prior, f.prior.Actions, []JournalEntry{entry}, 0)
		if err != nil || got != 0 {
			t.Fatalf("unverified stage %s charged alpha: got=%d error=%v", stage, got, err)
		}
	}
	for _, foreign := range []string{"plan", "deployment"} {
		entry := verified
		if foreign == "plan" {
			entry.PlanHash = "0x" + strings.Repeat("d7", 32)
		} else {
			entry.DeploymentID = "foreign-synthetic-deployment"
		}
		got, err := retiredVerifiedAlphaSpend(t.TempDir(), f.prior, f.prior.Actions, []JournalEntry{entry}, 0)
		if err != nil || got != 0 {
			t.Fatalf("foreign %s charged alpha: got=%d error=%v", foreign, got, err)
		}
	}
	got, err := retiredVerifiedAlphaSpend(t.TempDir(), f.prior, f.ancestor.Actions, f.entries, 0)
	if err != nil || got != 0 {
		t.Fatalf("fully carried history required an unnecessary archive read: got=%d error=%v", got, err)
	}
}

// An allowed plan hash alone is insufficient: the exact action and intent must
// exist in its authenticated source image, and missing evidence fails closed.
func TestRetiredAlphaSpendRejectsMissingOrMismatchedSourceEvidence(t *testing.T) {
	f := newRetiredAlphaHistoryFixture(t)
	verified := JournalEntry{PlanHash: f.ancestor.PlanHash, ActionID: f.omittedRepair.ID, IntentHash: f.omittedRepair.IntentHash, Stage: StageVerified}
	mutations := []struct {
		name   string
		mutate func(*JournalEntry)
	}{
		{name: "intent", mutate: func(entry *JournalEntry) { entry.IntentHash = "0x" + strings.Repeat("d8", 32) }},
		{name: "action", mutate: func(entry *JournalEntry) { entry.ActionID = "alpha.repair.operator-deposit.99" }},
		{name: "owner", mutate: func(entry *JournalEntry) { entry.PlanHash = f.prior.PlanHash }},
	}
	for _, mutation := range mutations {
		entry := verified
		mutation.mutate(&entry)
		if _, err := retiredVerifiedAlphaSpend(f.stateDir, f.prior, f.prior.Actions, []JournalEntry{entry}, 0); err == nil || !strings.Contains(err.Error(), "no exact source intent") {
			t.Fatalf("mismatched source %s was accepted: %v", mutation.name, err)
		}
	}
	if _, err := retiredVerifiedAlphaSpend(t.TempDir(), f.prior, f.prior.Actions, []JournalEntry{verified}, 0); err == nil || !strings.Contains(err.Error(), "read retired alpha source plan") {
		t.Fatalf("missing retired source approval was accepted: %v", err)
	}
	raw, err := json.Marshal(f.ancestor)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	wire["owner"] = "synthetic-corrupted-owner"
	raw, err = json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(f.stateDir, "plans", stringsTrim0x(f.ancestor.PlanHash)+".json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := retiredVerifiedAlphaSpend(f.stateDir, f.prior, f.prior.Actions, []JournalEntry{verified}, 0); err == nil || !strings.Contains(err.Error(), "hash mismatch") {
		t.Fatalf("hand-edited retired source approval was accepted: %v", err)
	}
}

// Build a local reconciliation which names one exact finalized source transfer;
// its changed intent must not turn the same amount into a second expense.
func alphaHistoryReconciliationTestPlan(t *testing.T, f retiredAlphaHistoryFixture) (*SetupPlan, Action, JournalEntry) {
	t.Helper()
	original := actionByID(t, f.prior, "alpha.transfer.operator-deposit.2")
	receipt := JournalEntry{
		Sequence: 1, PlanHash: f.prior.PlanHash, ActionID: original.ID, IntentHash: original.IntentHash,
		Stage: StageFinalized, TransactionHash: "0x" + strings.Repeat("31", 32), BlockNumber: 17, BlockHash: "0x" + strings.Repeat("42", 32),
	}
	reconciliation := original
	reconciliation.Kind = "substrate-reconciliation"
	reconciliation.Description = "synthetic local alpha reconciliation"
	reconciliation.Parameters = cloneStrings(original.Parameters)
	reconciliation.Parameters[alphaRecoveryPlanHashParameter] = receipt.PlanHash
	reconciliation.Parameters[alphaRecoveryIntentHashParameter] = receipt.IntentHash
	reconciliation.Parameters[alphaRecoveryTransactionHashParameter] = receipt.TransactionHash
	reconciliation.Parameters[alphaRecoveryBlockParameter] = strconv.FormatUint(receipt.BlockNumber, 10)
	reconciliation.Parameters[alphaRecoveryBlockHashParameter] = receipt.BlockHash
	var err error
	reconciliation.IntentHash, err = actionIntentHash(reconciliation)
	if err != nil {
		t.Fatal(err)
	}
	plan := *f.prior
	plan.PriorPlanHashes = append(append([]string(nil), f.prior.PriorPlanHashes...), f.prior.PlanHash)
	plan.Actions = append([]Action(nil), f.prior.Actions...)
	for index := range plan.Actions {
		if plan.Actions[index].ID == original.ID {
			plan.Actions[index] = reconciliation
		}
	}
	plan.PlanHash, err = plan.hash()
	if err != nil {
		t.Fatal(err)
	}
	if err := validatePlanBudget(&plan); err != nil {
		t.Fatalf("synthetic reconciliation approval is invalid: %v", err)
	}
	persistFleetCommitmentRecoveryTestPlan(t, f.stateDir, &plan)
	return &plan, reconciliation, receipt
}

// Verified originals, local reconciliations, and carried forms all refer to one
// source ceiling even when both intents have durable verification markers.
func TestRetiredAlphaSpendDeduplicatesVerifiedReconciliationAliases(t *testing.T) {
	f := newRetiredAlphaHistoryFixture(t)
	plan, reconciliation, receipt := alphaHistoryReconciliationTestPlan(t, f)
	original := actionByID(t, f.prior, reconciliation.ID)
	entries := []JournalEntry{
		receipt,
		{Sequence: 2, PlanHash: plan.PlanHash, ActionID: reconciliation.ID, IntentHash: reconciliation.IntentHash, Stage: StageVerified},
		{Sequence: 3, PlanHash: f.prior.PlanHash, ActionID: original.ID, IntentHash: original.IntentHash, Stage: StageVerified},
	}
	for _, journal := range [][]JournalEntry{entries[:2], entries, {entries[0], entries[2], entries[1], entries[1]}} {
		got, err := retiredVerifiedAlphaSpend(f.stateDir, plan, nil, journal, 0)
		if err != nil || got != original.Spend.AlphaRao {
			t.Fatalf("local reconciliation duplicated source expense: got=%d want=%d error=%v", got, original.Spend.AlphaRao, err)
		}
		for _, active := range []Action{original, reconciliation} {
			got, err := retiredVerifiedAlphaSpend(f.stateDir, plan, []Action{active}, journal, 0)
			if err != nil || got != 0 {
				t.Fatalf("carried reconciliation/source was also retired: got=%d error=%v", got, err)
			}
		}
	}
}

// Neither a reduced carried ceiling nor a changed recovery tuple may conceal
// the authenticated amount of an already-finalized source transfer.
func TestRetiredAlphaSpendRejectsConcealedCeilingAndRecoveryDrift(t *testing.T) {
	f := newRetiredAlphaHistoryFixture(t)
	plan, reconciliation, receipt := alphaHistoryReconciliationTestPlan(t, f)
	original := actionByID(t, f.prior, reconciliation.ID)
	originalVerified := JournalEntry{PlanHash: f.prior.PlanHash, ActionID: original.ID, IntentHash: original.IntentHash, Stage: StageVerified}
	changed := reconciliation
	changed.Spend.AlphaRao--
	var err error
	changed.IntentHash, err = actionIntentHash(changed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := retiredVerifiedAlphaSpend(f.stateDir, plan, []Action{changed}, []JournalEntry{originalVerified}, 0); err == nil || !strings.Contains(err.Error(), "differs from its source ceiling") {
		t.Fatalf("smaller carried alpha ceiling concealed historical expense: %v", err)
	}
	verified := JournalEntry{Sequence: 2, PlanHash: plan.PlanHash, ActionID: reconciliation.ID, IntentHash: reconciliation.IntentHash, Stage: StageVerified}
	mutations := []func(*JournalEntry){
		func(entry *JournalEntry) { entry.PlanHash = "0x" + strings.Repeat("51", 32) },
		func(entry *JournalEntry) { entry.IntentHash = "0x" + strings.Repeat("52", 32) },
		func(entry *JournalEntry) { entry.TransactionHash = "0x" + strings.Repeat("53", 32) },
		func(entry *JournalEntry) { entry.BlockNumber++ },
		func(entry *JournalEntry) { entry.BlockHash = "0x" + strings.Repeat("54", 32) },
		func(entry *JournalEntry) { entry.Stage = StageIncluded },
	}
	for index, mutate := range mutations {
		changedReceipt := receipt
		mutate(&changedReceipt)
		if _, err := retiredVerifiedAlphaSpend(f.stateDir, plan, nil, []JournalEntry{changedReceipt, verified}, 0); err == nil || !strings.Contains(err.Error(), "no exact finalized source") {
			t.Fatalf("changed reconciliation source tuple %d was accepted: %v", index, err)
		}
	}
	if _, err := retiredVerifiedAlphaSpend(f.stateDir, plan, []Action{original, original}, nil, 0); err == nil || !strings.Contains(err.Error(), "duplicate active alpha effect") {
		t.Fatalf("duplicate active alpha ceiling was accepted: %v", err)
	}
}

// Plan-local journal uniqueness does not exclude a second physical send under
// a different approved plan. Refuse that ambiguity instead of counting it once.
func TestRetiredAlphaSpendRejectsDistinctFinalizedTransactionsAcrossPlans(t *testing.T) {
	f := newRetiredAlphaHistoryFixture(t)
	action := actionByID(t, f.prior, "alpha.transfer.operator-deposit.1")
	first := JournalEntry{
		PlanHash: f.ancestor.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageFinalized,
		TransactionHash: "0x" + strings.Repeat("61", 32), BlockNumber: 21, BlockHash: "0x" + strings.Repeat("62", 32),
	}
	second := first
	second.PlanHash = f.prior.PlanHash
	verified := JournalEntry{PlanHash: f.prior.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageVerified}
	for _, active := range [][]Action{nil, f.prior.Actions} {
		got, err := retiredVerifiedAlphaSpend(f.stateDir, f.prior, active, []JournalEntry{first, second, verified}, 0)
		want := action.Spend.AlphaRao
		if len(active) != 0 {
			want = 0
		}
		if err != nil || got != want {
			t.Fatalf("duplicate finalized observation changed one ceiling: got=%d want=%d error=%v", got, want, err)
		}
		changed := second
		changed.TransactionHash = "0x" + strings.Repeat("63", 32)
		changed.BlockNumber++
		changed.BlockHash = "0x" + strings.Repeat("64", 32)
		if _, err := retiredVerifiedAlphaSpend(f.stateDir, f.prior, active, []JournalEntry{first, changed, verified}, 0); err == nil || !strings.Contains(err.Error(), "distinct finalized transactions across its lineage") {
			t.Fatalf("two finalized sends collapsed into one alpha ceiling: %v", err)
		}
	}
	plan, reconciliation, receipt := alphaHistoryReconciliationTestPlan(t, f)
	second = receipt
	second.PlanHash = f.ancestor.PlanHash
	second.TransactionHash = "0x" + strings.Repeat("65", 32)
	verified = JournalEntry{PlanHash: plan.PlanHash, ActionID: reconciliation.ID, IntentHash: reconciliation.IntentHash, Stage: StageVerified}
	for _, active := range [][]Action{nil, {reconciliation}} {
		if _, err := retiredVerifiedAlphaSpend(f.stateDir, plan, active, []JournalEntry{receipt, second, verified}, 0); err == nil || !strings.Contains(err.Error(), "distinct finalized transactions across its lineage") {
			t.Fatalf("reconciliation hid two finalized source sends: %v", err)
		}
	}
}
