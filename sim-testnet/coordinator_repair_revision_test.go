package main

import (
	"bytes"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// The actual setup gate must consume the same completed repair observation
// as revision rendering. Signed files alone never authorize repair carry;
// unrelated unresolved successes cannot inherit the repair's authentication.
func TestCoordinatorRepairCarryRevisionAdmitsOnlyAuthenticatedOriginalTransactions(t *testing.T) {
	fixture := newCoordinatorRepairCarryFixture(t)
	e := fixture.executor
	entries := e.journal.Entries()
	journalPath := filepath.Join(e.stateDir, "journal.jsonl")
	journalBefore, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := pendingPlanRevisionTransactions(e.plan, entries)
	if err != nil || len(pending) != 2 {
		t.Fatalf("repair fixture does not retain its two unverified successes: %+v %v", pending, err)
	}
	if _, err := planRevisionTransactionRecoveries(t.Context(), e.cfg, e.stateDir, e.plan, entries); !errors.Is(err, errPriorEVMTransactionSucceeded) {
		t.Fatalf("unobserved successful repair bypassed the transaction gate: %v", err)
	}
	filesOnly, err := readCoordinatorRepairCarry(e.stateDir, e.plan, entries)
	if err != nil {
		t.Fatal(err)
	}
	e.plan.coordinatorRepairObserved = filesOnly
	if _, err := planRevisionTransactionRecoveries(t.Context(), e.cfg, e.stateDir, e.plan, entries); !errors.Is(err, errPriorEVMTransactionSucceeded) {
		t.Fatalf("file-only repair issued chain postcondition authority: %v", err)
	}
	observed, err := authenticateCoordinatorRepairCarry(t.Context(), e.cfg, e.stateDir, e.plan, entries, e.deployer.client, e.independentEVM)
	if err != nil {
		t.Fatal(err)
	}
	e.plan.coordinatorRepairObserved = observed
	independentReads := fixture.independent.calls.Load()
	if got, err := planRevisionTransactionRecoveries(t.Context(), e.cfg, e.stateDir, e.plan, entries); err != nil || !reflect.DeepEqual(got, planRevisionRecoveries{}) {
		t.Fatalf("exact completed repair was not admitted without another write: %+v %v", got, err)
	}
	if fixture.independent.calls.Load() != independentReads {
		t.Fatal("transaction classification repeated the completed independent source audit")
	}
	transaction := pending[0]
	index := 0
	if transaction.ActionID == observed.reference.Result.Result.Activate.ActionID {
		index = 1
	}
	signed := observed.transactions[index]
	receipt, err := fixture.reader.GetTransactionReceipt(t.Context(), signed.Hash())
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		mutate func(*planRevisionTransaction)
	}{
		{"source", func(value *planRevisionTransaction) { value.PlanHash = common.Hash{1}.Hex() }},
		{"action", func(value *planRevisionTransaction) { value.ActionID = "repair.unrelated-success" }},
		{"intent", func(value *planRevisionTransaction) { value.IntentHash = common.Hash{2}.Hex() }},
		{"hash", func(value *planRevisionTransaction) { value.TransactionHash = common.Hash{3}.Hex() }},
		{"inclusion", func(value *planRevisionTransaction) { value.BlockNumber++ }},
		{"block", func(value *planRevisionTransaction) { value.BlockHash = common.Hash{4}.Hex() }},
		{"signer", func(value *planRevisionTransaction) { value.Signer = common.Address{5}.Hex() }},
		{"nonce", func(value *planRevisionTransaction) { value.Nonce = "999" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := transaction
			test.mutate(&changed)
			if err := validateCoordinatorRepairRevisionTransaction(e.plan, entries, signed, receipt, changed); err == nil {
				t.Fatal("changed transaction inherited completed repair authority")
			}
		})
	}
	changedPlan := *e.plan
	changedPlan.GeneratedAt += " changed"
	if err := validateCoordinatorRepairRevisionTransaction(&changedPlan, entries, signed, receipt, transaction); err == nil {
		t.Fatal("changed plan reused a prior source audit")
	}
	changedEntries := append([]JournalEntry(nil), entries...)
	changedEntries[0].Error += " changed"
	if err := validateCoordinatorRepairRevisionTransaction(e.plan, changedEntries, signed, receipt, transaction); err == nil {
		t.Fatal("changed journal reused a prior source audit")
	}
	changedReceipt := *receipt
	changedReceipt.Status = types.ReceiptStatusFailed
	if err := validateCoordinatorRepairRevisionTransaction(e.plan, entries, signed, &changedReceipt, transaction); err == nil {
		t.Fatal("reverted receipt inherited successful repair authority")
	}
	otherSigned := types.NewTx(&types.LegacyTx{Nonce: signed.Nonce() + 1, Value: new(big.Int), Gas: signed.Gas(), GasPrice: signed.GasPrice()})
	if err := validateCoordinatorRepairRevisionTransaction(e.plan, entries, otherSigned, receipt, transaction); err == nil {
		t.Fatal("different signed bytes inherited repair authority")
	}
	// A successful receipt without a durable finalization remains unresolved.
	// Ordinary finalized history may use generic carry, independently of the
	// repair authentication, but an explicit failure must still block recovery.
	for _, test := range []struct {
		name        string
		stages      []JournalStage
		wantPending bool
	}{
		{"included", []JournalStage{StageIncluded}, true},
		{"finalized", []JournalStage{StageFinalized}, false},
		{"failed-after-finalization", []JournalStage{StageFinalized, StageFailed}, true},
		{"finalized-after-failure", []JournalStage{StageFailed, StageFinalized}, true},
	} {
		extraEntries := append([]JournalEntry(nil), entries...)
		for _, stage := range test.stages {
			extra := observed.reference.Result.Result.Activate
			extra.ActionID = "unrelated.success"
			extra.Stage = stage
			extra.Sequence = extraEntries[len(extraEntries)-1].Sequence + 1
			extraEntries = append(extraEntries, extra)
		}
		pending, err := pendingPlanRevisionTransactions(e.plan, extraEntries)
		if err != nil || len(pending) != 2 && !test.wantPending || len(pending) != 3 && test.wantPending {
			t.Fatalf("%s: unexpected pending transactions: %+v %v", test.name, pending, err)
		}
		withExtra, err := authenticateCoordinatorRepairCarry(t.Context(), e.cfg, e.stateDir, e.plan, extraEntries, e.deployer.client, e.independentEVM)
		if err != nil {
			t.Fatalf("%s: authenticate repair: %v", test.name, err)
		}
		e.plan.coordinatorRepairObserved = withExtra
		recoveries, err := planRevisionTransactionRecoveries(t.Context(), e.cfg, e.stateDir, e.plan, extraEntries)
		if test.wantPending {
			if !errors.Is(err, errPriorEVMTransactionSucceeded) {
				t.Fatalf("%s: repair carry accepted an unrelated unresolved success: %v", test.name, err)
			}
		} else if err != nil || !reflect.DeepEqual(recoveries, planRevisionRecoveries{}) {
			t.Fatalf("%s: ordinary finalized predecessor was not carried: %+v %v", test.name, recoveries, err)
		}
	}
	journalAfter, err := os.ReadFile(journalPath)
	if err != nil || !bytes.Equal(journalBefore, journalAfter) || !reflect.DeepEqual(entries, e.journal.Entries()) || fixture.reader.sends.Load() != 0 || fixture.independent.sends.Load() != 0 {
		t.Fatal("repair admission changed original history or sent a transaction", err)
	}
}
