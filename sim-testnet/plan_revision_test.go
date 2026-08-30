// Plan revision tests cover safe continuation after a locked release changes
// during an interrupted, partially registered testnet deployment.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

func actionByID(t *testing.T, plan *SetupPlan, id string) Action {
	t.Helper()
	for _, action := range plan.Actions {
		if action.ID == id {
			return action
		}
	}
	t.Fatalf("plan has no action %s", id)
	return Action{}
}

// Remove only the reserve-composition fields introduced by v9, preserving the
// exact authenticated v8 action semantics used by the live testnet ancestor.
func downgradeReserveEnvelopeToV8(t *testing.T, plan *SetupPlan) {
	t.Helper()
	plan.Schema = setupPlanSchemaV8
	for index := range plan.Actions {
		action := &plan.Actions[index]
		changed := false
		if strings.HasPrefix(action.ID, "alpha.transfer.operator-deposit.") {
			delete(action.Parameters, "reserve_calls")
			delete(action.Parameters, "reserve_rounding_allowance_per_call_rao")
			changed = true
		}
		if action.ID == "campaign.voluntary-conviction.1" || action.ID == dishonestDepositActionID {
			delete(action.Parameters, "reserve_runtime_share_transitions")
			delete(action.Parameters, "reserve_rounding_allowance_rao")
			changed = true
		}
		if changed {
			var err error
			action.IntentHash, err = actionIntentHash(*action)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	var err error
	plan.PlanHash, err = plan.hash()
	if err != nil {
		t.Fatal(err)
	}
}

func TestV9RevisionAuthenticatesPersistedV8AndBindsBothReserveTransitions(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	prior, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	downgradeReserveEnvelopeToV8(t, prior)
	if err := validatePlanBudget(prior); err != nil {
		t.Fatalf("valid v8 reserve envelope was rejected: %v", err)
	}

	stateDir := t.TempDir()
	wire, err := json.MarshalIndent(prior, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "plan.json"), append(wire, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := readPersistedPlan(stateDir)
	if err != nil {
		t.Fatalf("authenticated v8 plan was not loadable for revision: %v", err)
	}
	if loaded.Schema != setupPlanSchemaV8 || loaded.PlanHash != prior.PlanHash {
		t.Fatalf("loaded v8 identity changed: schema=%s hash=%s", loaded.Schema, loaded.PlanHash)
	}

	current := *testSetupFacts()
	revised, err := buildPlanRevisionFromFacts(cfg, stateDir, loaded, &current, nil, time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	if revised.Schema != currentSetupPlanSchema || revised.PriorPlanHashes[0] != prior.PlanHash {
		t.Fatalf("revision lineage=%s/%v, want v9 child of %s", revised.Schema, revised.PriorPlanHashes, prior.PlanHash)
	}
	for _, id := range []string{"campaign.voluntary-conviction.1", dishonestDepositActionID} {
		action := actionByID(t, revised, id)
		if action.Parameters["reserve_runtime_share_transitions"] != "2" || action.Parameters["reserve_rounding_allowance_rao"] != "2" {
			t.Fatalf("v9 campaign action %s lacks two-transition envelope: %+v", id, action.Parameters)
		}
	}
	for _, id := range []string{"alpha.transfer.operator-deposit.1", "alpha.transfer.operator-deposit.2"} {
		action := actionByID(t, revised, id)
		if action.Parameters["reserve_calls"] == "" || action.Parameters["reserve_rounding_allowance_per_call_rao"] != "2" {
			t.Fatalf("v9 operator funding %s lacks reserve-call envelope: %+v", id, action.Parameters)
		}
	}
}

func TestPlanRevisionFindsEveryUnverifiedTransactionAcrossLineage(t *testing.T) {
	prior := &SetupPlan{PlanHash: "0x" + strings.Repeat("11", 32), PriorPlanHashes: []string{"0x" + strings.Repeat("22", 32)}}
	ancestor := prior.PriorPlanHashes[0]
	entries := []JournalEntry{
		{PlanHash: prior.PlanHash, ActionID: "pending", IntentHash: "intent-pending", Stage: StageBroadcast, TransactionHash: "0x01", RecoveryBlock: 10, RecoveryBlockHash: "0xaa"},
		{PlanHash: prior.PlanHash, ActionID: "pending", IntentHash: "intent-pending", Stage: StageFailed, Error: "timeout"},
		{PlanHash: prior.PlanHash, ActionID: "verified", IntentHash: "intent-verified", Stage: StageBroadcast, TransactionHash: "0x02", RecoveryBlock: 10, RecoveryBlockHash: "0xaa"},
		{PlanHash: prior.PlanHash, ActionID: "verified", IntentHash: "intent-verified", Stage: StageVerified},
		{PlanHash: ancestor, ActionID: "reverted", IntentHash: "intent-reverted", Stage: StageBroadcast, TransactionHash: "0x03", RecoveryBlock: 11, RecoveryBlockHash: "0xbb"},
		{PlanHash: ancestor, ActionID: "reverted", IntentHash: "intent-reverted", Stage: StageIncluded, TransactionHash: "0x03", BlockNumber: 12, BlockHash: "0xcc"},
		{PlanHash: ancestor, ActionID: "reverted", IntentHash: "intent-reverted", Stage: StageFailed, Error: "dispatch failed"},
		{PlanHash: prior.PlanHash, ActionID: "local-failure", IntentHash: "intent-local", Stage: StageFailed, Error: "preflight"},
		{PlanHash: "0x" + strings.Repeat("33", 32), ActionID: "outsider", IntentHash: "intent-outsider", Stage: StageBroadcast, TransactionHash: "0x04"},
	}
	pending, err := pendingPlanRevisionTransactions(prior, entries)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]planRevisionTransaction{}
	for _, transaction := range pending {
		got[transaction.ActionID] = transaction
	}
	if len(got) != 2 || got["pending"].TransactionHash != "0x01" || got["reverted"].TransactionHash != "0x03" || got["reverted"].BlockNumber != 12 {
		t.Fatalf("unverified revision transactions=%+v", pending)
	}
}

func TestPlanRevisionRejectsMultipleTransactionsAndMissingArtifacts(t *testing.T) {
	prior := &SetupPlan{PlanHash: "0x" + strings.Repeat("11", 32)}
	entries := []JournalEntry{
		{PlanHash: prior.PlanHash, ActionID: "action", IntentHash: "intent", Stage: StageBroadcast, TransactionHash: "0x01"},
		{PlanHash: prior.PlanHash, ActionID: "action", IntentHash: "intent", Stage: StageIncluded, TransactionHash: "0x02"},
	}
	if _, err := pendingPlanRevisionTransactions(prior, entries); err == nil || !strings.Contains(err.Error(), "multiple transactions") {
		t.Fatalf("multiple prior transactions were accepted: %v", err)
	}
	entries = entries[:1]
	if err := validatePlanRevisionTransactions(context.Background(), testResolvedConfig(t), t.TempDir(), prior, entries); err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("missing transaction artifact was accepted: %v", err)
	}
}

// Pending, noncanonical, reverted, and successful receipts retain distinct
// revision outcomes; only a finalized revert is generically retryable.
func TestEVMRevisionTransactionOutcomeClassificationFailsClosed(t *testing.T) {
	transactionHash := common.HexToHash("0x1234")
	canonical := testEVMHead(20, 0x20)
	baseline := &ethTypes.Receipt{
		Status: ethTypes.ReceiptStatusFailed, TxHash: transactionHash,
		BlockNumber: big.NewInt(20), BlockHash: common.HexToHash(canonical.Hash),
	}
	transaction := planRevisionTransaction{TransactionHash: transactionHash.Hex(), BlockNumber: 20, BlockHash: canonical.Hash}
	if err := validateFailedEVMRevisionTransactionFromReader(context.Background(), &evmFinalityFixture{finalized: testEVMHead(21, 0x21), canonical: map[uint64]ChainHead{20: canonical}, receipt: baseline}, transaction); err != nil {
		t.Fatalf("canonical finalized revert was rejected: %v", err)
	}
	success := *baseline
	success.Status = ethTypes.ReceiptStatusSuccessful
	if err := validateFailedEVMRevisionTransactionFromReader(context.Background(), &evmFinalityFixture{finalized: testEVMHead(21, 0x21), canonical: map[uint64]ChainHead{20: canonical}, receipt: &success}, transaction); !errors.Is(err, errPriorEVMTransactionSucceeded) {
		t.Fatalf("successful receipt classification=%v", err)
	}
	orphan := *baseline
	orphan.BlockHash = common.HexToHash(testEVMHead(20, 0x22).Hash)
	cases := []*evmFinalityFixture{
		{finalized: testEVMHead(21, 0x21), canonical: map[uint64]ChainHead{20: canonical}, receiptError: ethereum.NotFound},
		{finalized: testEVMHead(19, 0x19), canonical: map[uint64]ChainHead{20: canonical}, receipt: baseline},
		{finalized: testEVMHead(21, 0x21), canonical: map[uint64]ChainHead{20: canonical}, receipt: &orphan},
	}
	for index, fixture := range cases {
		if err := validateFailedEVMRevisionTransactionFromReader(context.Background(), fixture, transaction); err == nil || errors.Is(err, errPriorEVMTransactionSucceeded) {
			t.Errorf("unsafe EVM outcome %d was retryable: %v", index, err)
		}
	}
	mismatched := transaction
	mismatched.BlockHash = testEVMHead(20, 0x23).Hash
	if err := validateFailedEVMRevisionTransactionFromReader(context.Background(), &evmFinalityFixture{finalized: testEVMHead(21, 0x21), canonical: map[uint64]ChainHead{20: canonical}, receipt: baseline}, mismatched); err == nil {
		t.Fatal("journal/receipt inclusion mismatch was accepted")
	}
}

func TestSuccessfulAncestorTransactionRequiresExactVerifiedDescendantReconciliation(t *testing.T) {
	ancestorHash := "0x" + strings.Repeat("11", 32)
	currentHash := "0x" + strings.Repeat("22", 32)
	transaction := planRevisionTransaction{
		PlanHash: ancestorHash, ActionID: "alpha.transfer.operator-deposit.2", IntentHash: "0x" + strings.Repeat("33", 32),
		TransactionHash: "0x" + strings.Repeat("44", 32), BlockNumber: 123, BlockHash: "0x" + strings.Repeat("55", 32),
	}
	baseAction := Action{
		ID: transaction.ActionID, Kind: "substrate-reconciliation", IntentHash: "0x" + strings.Repeat("66", 32),
		Parameters: map[string]string{
			alphaRecoveryPlanHashParameter:        transaction.PlanHash,
			alphaRecoveryIntentHashParameter:      transaction.IntentHash,
			alphaRecoveryTransactionHashParameter: transaction.TransactionHash,
			alphaRecoveryBlockParameter:           strconv.FormatUint(transaction.BlockNumber, 10),
			alphaRecoveryBlockHashParameter:       transaction.BlockHash,
		},
	}
	baseEntries := []JournalEntry{
		{PlanHash: ancestorHash, ActionID: transaction.ActionID, IntentHash: transaction.IntentHash, Stage: StageFinalized, TransactionHash: transaction.TransactionHash, BlockNumber: transaction.BlockNumber, BlockHash: transaction.BlockHash},
		{PlanHash: currentHash, ActionID: baseAction.ID, IntentHash: baseAction.IntentHash, Stage: StageVerified},
	}

	if !verifiedReconciliationForFinalizedAlphaTransaction(&SetupPlan{PlanHash: currentHash, PriorPlanHashes: []string{ancestorHash}, Actions: []Action{baseAction}}, baseEntries, transaction) {
		t.Fatal("exact verified descendant reconciliation was not recognized")
	}
	for _, test := range []struct {
		name   string
		mutate func(*Action, *[]JournalEntry)
	}{
		{"no descendant verification", func(_ *Action, entries *[]JournalEntry) { *entries = (*entries)[:1] }},
		{"foreign recovery plan", func(action *Action, _ *[]JournalEntry) {
			action.Parameters[alphaRecoveryPlanHashParameter] = "0x" + strings.Repeat("77", 32)
		}},
		{"wrong recovery intent", func(action *Action, _ *[]JournalEntry) {
			action.Parameters[alphaRecoveryIntentHashParameter] = "0x" + strings.Repeat("77", 32)
		}},
		{"wrong transaction", func(action *Action, _ *[]JournalEntry) {
			action.Parameters[alphaRecoveryTransactionHashParameter] = "0x" + strings.Repeat("77", 32)
		}},
		{"wrong block", func(action *Action, _ *[]JournalEntry) { action.Parameters[alphaRecoveryBlockParameter] = "124" }},
		{"wrong block hash", func(action *Action, _ *[]JournalEntry) {
			action.Parameters[alphaRecoveryBlockHashParameter] = "0x" + strings.Repeat("77", 32)
		}},
		{"unfinalized ancestor", func(_ *Action, entries *[]JournalEntry) { (*entries)[0].Stage = StageIncluded }},
		{"sibling action", func(action *Action, _ *[]JournalEntry) { action.ID = "alpha.transfer.operator-deposit.1" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			action := baseAction
			action.Parameters = cloneStrings(baseAction.Parameters)
			entries := append([]JournalEntry(nil), baseEntries...)
			test.mutate(&action, &entries)
			plan := &SetupPlan{PlanHash: currentHash, PriorPlanHashes: []string{ancestorHash}, Actions: []Action{action}}
			if verifiedReconciliationForFinalizedAlphaTransaction(plan, entries, transaction) {
				t.Fatal("inexact reconciliation closed a successful ancestor transaction")
			}
		})
	}
}

func TestPlanRevisionReconcilesFinalizedAlphaAndAddsOnlyMinimumRepair(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	prior, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	// Model the v7 planner which did not reserve one rao for the bootstrap
	// transfer's own destination-share floor.
	for index := range prior.Actions {
		if prior.Actions[index].ID != "alpha.transfer.operator-deposit.2" {
			continue
		}
		prior.Actions[index].Spend.AlphaRao--
		prior.Actions[index].Parameters["exact_amount_rao"] = strconv.FormatUint(prior.Actions[index].Spend.AlphaRao, 10)
		prior.Actions[index].Parameters["minimum_destination_credit_rao"] = strconv.FormatUint(prior.Actions[index].Spend.AlphaRao-1, 10)
		prior.Actions[index].IntentHash, err = actionIntentHash(prior.Actions[index])
		if err != nil {
			t.Fatal(err)
		}
		prior.MaximumSpend.AlphaRao--
	}
	prior.PlanHash, err = prior.hash()
	if err != nil {
		t.Fatal(err)
	}
	revised, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	revised.PriorPlanHashes = []string{prior.PlanHash}
	revised.PlanHash = "0x" + strings.Repeat("cc", 32)
	transfer := actionByID(t, prior, "alpha.transfer.operator-deposit.2")
	entries := []JournalEntry{
		{PlanHash: prior.PlanHash, ActionID: transfer.ID, IntentHash: transfer.IntentHash, Stage: StageBroadcast, TransactionHash: "0x" + strings.Repeat("aa", 32)},
		{PlanHash: prior.PlanHash, ActionID: transfer.ID, IntentHash: transfer.IntentHash, Stage: StageFinalized, TransactionHash: "0x" + strings.Repeat("aa", 32), BlockNumber: 123, BlockHash: "0x" + strings.Repeat("bb", 32)},
		{PlanHash: prior.PlanHash, ActionID: transfer.ID, IntentHash: transfer.IntentHash, Stage: StageFailed, Error: "one-rao destination share floor"},
	}
	beforeMaximum := revised.MaximumSpend.AlphaRao
	if err := reconcileFinalizedAlphaTransfers(revised, prior, entries); err != nil {
		t.Fatal(err)
	}
	reconciliation := actionByID(t, revised, transfer.ID)
	repair := actionByID(t, revised, "alpha.repair.operator-deposit.2")
	minimum, err := minimumAlphaTransferRao(revised.LiveFacts.DefaultMinTransferRao, revised.LiveFacts.AlphaPriceQ9, revised.AlphaTransferMarginBPS)
	if err != nil {
		t.Fatal(err)
	}
	if reconciliation.Kind != "substrate-reconciliation" || reconciliation.Spend.AlphaRao != transfer.Spend.AlphaRao || reconciliation.Parameters[alphaRecoveryTransactionHashParameter] != "0x"+strings.Repeat("aa", 32) {
		t.Fatalf("reconciliation did not bind the exact ancestor: %+v", reconciliation)
	}
	if repair.Spend.AlphaRao != minimum || len(repair.DependsOn) != 1 || repair.DependsOn[0] != transfer.ID || repair.Parameters[alphaRepairMinimumIncrementParameter] == "" {
		t.Fatalf("repair is not independently minimum-bounded: %+v", repair)
	}
	// The new fresh transfer is one rao larger than the ancestor; replacing it
	// with the exact ancestor and one runtime-minimum repair is the only change.
	wantMaximum := beforeMaximum - (transfer.Spend.AlphaRao + 1) + transfer.Spend.AlphaRao + minimum
	if revised.MaximumSpend.AlphaRao != wantMaximum {
		t.Fatalf("reconciled maximum=%d want=%d", revised.MaximumSpend.AlphaRao, wantMaximum)
	}
	remaining, err := remainingPlanSpend(revised, entries)
	if err != nil {
		t.Fatal(err)
	}
	if remaining.AlphaRao != revised.MaximumSpend.AlphaRao-transfer.Spend.AlphaRao {
		t.Fatalf("finalized reconciliation was not removed from remaining spend: %+v", remaining)
	}
	if err := validatePlanBudget(revised); err != nil {
		t.Fatalf("reconciled plan is invalid: %v", err)
	}
}

func TestPlanRevisionCarriesVerifiedOperatorRepairsAndAddsOnlyRequiredTopUp(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	prior, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	priorTransfer := actionByID(t, prior, "alpha.transfer.operator-deposit.1")
	reserveCalls, err := strconv.ParseUint(priorTransfer.Parameters["reserve_calls"], 10, 64)
	if err != nil || reserveCalls == 0 || priorTransfer.Spend.AlphaRao <= reserveCalls+1 {
		t.Fatalf("invalid reserve-call fixture: action=%+v error=%v", priorTransfer, err)
	}
	for index := range prior.Actions {
		if prior.Actions[index].ID != priorTransfer.ID {
			continue
		}
		prior.Actions[index].Spend.AlphaRao -= reserveCalls
		prior.Actions[index].Parameters["exact_amount_rao"] = strconv.FormatUint(prior.Actions[index].Spend.AlphaRao, 10)
		prior.Actions[index].Parameters["minimum_destination_credit_rao"] = strconv.FormatUint(prior.Actions[index].Spend.AlphaRao-1, 10)
		delete(prior.Actions[index].Parameters, "reserve_calls")
		delete(prior.Actions[index].Parameters, "reserve_rounding_allowance_per_call_rao")
		prior.Actions[index].IntentHash, err = actionIntentHash(prior.Actions[index])
		if err != nil {
			t.Fatal(err)
		}
		priorTransfer = prior.Actions[index]
	}
	prior.MaximumSpend, err = maximumActionSpend(prior.Actions)
	if err != nil {
		t.Fatal(err)
	}
	prior.PlanHash, err = prior.hash()
	if err != nil {
		t.Fatal(err)
	}
	entries := []JournalEntry{{PlanHash: prior.PlanHash, ActionID: priorTransfer.ID, IntentHash: priorTransfer.IntentHash, Stage: StageVerified}}
	revised, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	revised.PriorPlanHashes = []string{prior.PlanHash}
	freshTransfer := actionByID(t, revised, priorTransfer.ID)
	if err := preserveVerifiedOperatorAlphaTransfers(revised, prior, entries); err != nil {
		t.Fatal(err)
	}
	for index := range revised.Actions {
		revised.Actions[index].IntentHash, err = actionIntentHash(revised.Actions[index])
		if err != nil {
			t.Fatal(err)
		}
	}
	revised.PlanHash, err = revised.hash()
	if err != nil {
		t.Fatal(err)
	}
	repair := actionByID(t, revised, "alpha.repair.operator-deposit.1")
	requiredCredit := freshTransfer.Parameters["minimum_destination_credit_rao"]
	minimum, err := minimumAlphaTransferRao(revised.LiveFacts.DefaultMinTransferRao, revised.LiveFacts.AlphaPriceQ9, revised.AlphaTransferMarginBPS)
	if err != nil {
		t.Fatal(err)
	}
	if actionByID(t, revised, priorTransfer.ID).IntentHash != priorTransfer.IntentHash || repair.Spend.AlphaRao != minimum || repair.Parameters[alphaRepairMinimumDestinationParameter] != requiredCredit || repair.Parameters[alphaRepairMinimumIncrementParameter] != "" || repair.DependsOn[0] != priorTransfer.ID {
		t.Fatalf("verified allocation was not converted to one bounded top-up: transfer=%+v repair=%+v", actionByID(t, revised, priorTransfer.ID), repair)
	}
	if !slices.Contains(actionByID(t, revised, "campaign.voluntary-conviction.1").DependsOn, repair.ID) {
		t.Fatal("campaign dependency bypasses the operator top-up")
	}
	if err := validatePlanBudget(revised); err != nil {
		t.Fatalf("top-up revision is invalid: %v", err)
	}

	entries = append(entries, JournalEntry{PlanHash: revised.PlanHash, ActionID: repair.ID, IntentHash: repair.IntentHash, Stage: StageVerified})
	next, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(3, 0))
	if err != nil {
		t.Fatal(err)
	}
	next.PriorPlanHashes = append(append([]string(nil), revised.PriorPlanHashes...), revised.PlanHash)
	if err := preserveVerifiedOperatorAlphaTransfers(next, revised, entries); err != nil {
		t.Fatal(err)
	}
	repairCount := 0
	for _, action := range next.Actions {
		kind, operator, parseErr := alphaTransferTargetFromActionID(action.ID)
		if parseErr == nil && strings.HasPrefix(action.ID, "alpha.repair.") && kind == "operator-deposit" && operator == 1 {
			repairCount++
			if action.IntentHash != repair.IntentHash {
				t.Fatalf("verified repair intent was not carried: got=%s want=%s", action.IntentHash, repair.IntentHash)
			}
		}
	}
	if repairCount != 1 {
		t.Fatalf("verified repair was dropped or duplicated: count=%d", repairCount)
	}
}

func TestPlanRevisionCarriesVerifiedAncestorEVMGasReallocationAndOriginalSpend(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ancestor, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	releaseFutureReserve := func(plan *SetupPlan) {
		t.Helper()
		for index := range plan.Actions {
			action := &plan.Actions[index]
			if action.ID != "campaign.evm-gas-reserve" {
				continue
			}
			action.Spend.EVMGasWei, err = subtractDecimalUint(action.Spend.EVMGasWei, "1000000000000000000")
			if err != nil {
				t.Fatal(err)
			}
			action.IntentHash, err = actionIntentHash(*action)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	releaseFutureReserve(ancestor)
	gasUnits := map[string]uint64{
		"campaign.voluntary-conviction.1": 30_505_000,
		"operator.register.1":             700_000,
	}
	for index := range ancestor.Actions {
		action := &ancestor.Actions[index]
		units, ok := gasUnits[action.ID]
		if !ok {
			continue
		}
		action.Parameters[evmMaximumGasUnitsParameter] = strconv.FormatUint(units, 10)
		action.Spend.EVMGasWei = multiplyUint64Decimal(units, cfg.Config.Budgets.MaximumEVMFeePerGasWei)
		action.IntentHash, err = actionIntentHash(*action)
		if err != nil {
			t.Fatal(err)
		}
	}
	ancestor.MaximumSpend, err = maximumActionSpend(ancestor.Actions)
	if err != nil {
		t.Fatal(err)
	}
	ancestor.PlanHash, err = ancestor.hash()
	if err != nil || validatePlanBudget(ancestor) != nil {
		t.Fatalf("construct gas-incident ancestor: hash_error=%v validation=%v", err, validatePlanBudget(ancestor))
	}
	stateDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(stateDir, "plans"), 0o700); err != nil {
		t.Fatal(err)
	}
	wire, err := json.MarshalIndent(ancestor, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "plans", stringsTrim0x(ancestor.PlanHash)+".json"), append(wire, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	prior, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	releaseFutureReserve(prior)
	prior.MaximumSpend, err = maximumActionSpend(prior.Actions)
	if err != nil {
		t.Fatal(err)
	}
	prior.PriorPlanHashes = []string{ancestor.PlanHash}
	prior.PlanHash, err = prior.hash()
	if err != nil {
		t.Fatal(err)
	}
	revised, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(3, 0))
	if err != nil {
		t.Fatal(err)
	}
	releaseFutureReserve(revised)
	revised.MaximumSpend, err = maximumActionSpend(revised.Actions)
	if err != nil {
		t.Fatal(err)
	}
	beforeMaximum := revised.MaximumSpend.EVMGasWei
	entries := make([]JournalEntry, 0, len(gasUnits))
	for id := range gasUnits {
		action := actionByID(t, ancestor, id)
		entries = append(entries, JournalEntry{PlanHash: ancestor.PlanHash, ActionID: id, IntentHash: action.IntentHash, Stage: StageVerified})
	}
	if err := preserveVerifiedEVMGasReallocations(stateDir, revised, prior, entries); err != nil {
		t.Fatal(err)
	}
	for id := range gasUnits {
		got, want := actionByID(t, revised, id), actionByID(t, ancestor, id)
		if got.IntentHash != want.IntentHash || got.Spend != want.Spend || got.Parameters[evmMaximumGasUnitsParameter] != want.Parameters[evmMaximumGasUnitsParameter] {
			t.Errorf("verified gas reallocation %s was not carried exactly: got=%+v want=%+v", id, got, want)
		}
	}
	adjacent := actionByID(t, revised, "operator.register.2")
	if adjacent.Kind != "evm-transaction" || adjacent.Parameters[evmMaximumGasUnitsParameter] != "750000" {
		t.Fatal("unverified adjacent operator registration inherited the historical gas ceiling")
	}
	wantMaximum, err := maximumActionSpend(revised.Actions)
	if err != nil || revised.MaximumSpend != wantMaximum || revised.MaximumSpend.EVMGasWei == beforeMaximum {
		t.Fatalf("carried historical ceiling is not cumulative: got=%+v want=%+v before=%s error=%v", revised.MaximumSpend, wantMaximum, beforeMaximum, err)
	}
	if err := validatePlanBudget(revised); err != nil {
		t.Fatalf("gas-reallocation carry exceeds the approved plan: %v", err)
	}
}

func TestEVMGasReallocationEquivalenceRejectsEverySemanticChange(t *testing.T) {
	baseline := Action{
		ID: "campaign.voluntary-conviction.1", Kind: "evm-transaction", Target: "no:1", Description: "conviction",
		Parameters: map[string]string{evmMaximumGasUnitsParameter: "200000", evmMaximumFeePerGasParameter: "100000000000", "amount_rao": "1000000000"},
		Spend:      Spend{EVMGasWei: "20000000000000000"}, DependsOn: []string{"fund"},
	}
	onlyGas := baseline
	onlyGas.Parameters = cloneStrings(baseline.Parameters)
	onlyGas.Parameters[evmMaximumGasUnitsParameter] = "150000"
	onlyGas.Spend.EVMGasWei = "15000000000000000"
	if !sameEVMTransactionExceptGasUnits(baseline, onlyGas) {
		t.Fatal("an exact gas-unit-only reallocation was rejected")
	}
	cases := []Action{}
	changedTarget := onlyGas
	changedTarget.Target = "no:2"
	cases = append(cases, changedTarget)
	changedFee := onlyGas
	changedFee.Parameters = cloneStrings(onlyGas.Parameters)
	changedFee.Parameters[evmMaximumFeePerGasParameter] = "99999999999"
	cases = append(cases, changedFee)
	changedAmount := onlyGas
	changedAmount.Parameters = cloneStrings(onlyGas.Parameters)
	changedAmount.Parameters["amount_rao"] = "2000000000"
	cases = append(cases, changedAmount)
	changedDependency := onlyGas
	changedDependency.DependsOn = []string{"other-fund"}
	cases = append(cases, changedDependency)
	changedValue := onlyGas
	changedValue.Spend.TAORao = 1
	cases = append(cases, changedValue)
	changedKind := onlyGas
	changedKind.Kind = "evm-read"
	cases = append(cases, changedKind)
	for index, candidate := range cases {
		if sameEVMTransactionExceptGasUnits(baseline, candidate) {
			t.Errorf("semantic change %d was accepted as a gas-only reallocation: %+v", index, candidate)
		}
	}
}

// TestLegacyFleetWriteConversionRequiresExactReadProof ensures only the
// reviewed atomic-installer transition may retain an ancestor write receipt.
func TestLegacyFleetWriteConversionRequiresExactReadProof(t *testing.T) {
	ancestor := Action{
		ID: "fleet.mirror.1", Kind: "evm-transaction", Target: "head-fleet:1",
		Parameters: map[string]string{
			fleetCommitmentStorageParameter: fleetCommitmentStorageV2,
			deploymentManifestHashParameter: "0x1111",
			evmMaximumGasUnitsParameter:     "200000",
			evmMaximumFeePerGasParameter:    "100000000000",
		},
		Spend: Spend{EVMGasWei: "20000000000000000"},
	}
	proof := Action{
		ID: "fleet.mirror.1", Kind: "evm-read", Target: "head-fleet:1",
		Parameters: map[string]string{
			fleetCommitmentStorageParameter: fleetCommitmentStorageV2,
			deploymentManifestHashParameter: "0x1111",
			"batch_installed":               "true",
		},
	}
	if !sameVerifiedLegacyFleetWriteAsReadProof(ancestor, proof) {
		t.Fatal("exact legacy fleet write was not recognized")
	}
	mutations := []Action{proof, proof, proof, proof, proof, proof}
	mutations[0].ID = "fleet.mirror.2"
	mutations[1].Target = "head-fleet:2"
	mutations[2].Kind = "evm-transaction"
	mutations[3].Parameters = cloneStrings(proof.Parameters)
	delete(mutations[3].Parameters, "batch_installed")
	mutations[4].Parameters = cloneStrings(proof.Parameters)
	mutations[4].Parameters[deploymentManifestHashParameter] = "0x2222"
	mutations[5].Spend.TAORao = 1
	for index, mutation := range mutations {
		if sameVerifiedLegacyFleetWriteAsReadProof(ancestor, mutation) {
			t.Errorf("semantic mutation %d retained a legacy write", index)
		}
	}
}

// TestVerifiedLegacyFleetWriteRemainsChargedAcrossRevision proves the exact
// verified write, not the zero-cost replacement proof, remains in plan spend.
func TestVerifiedLegacyFleetWriteRemainsChargedAcrossRevision(t *testing.T) {
	ancestor := Action{
		ID: "fleet.bind.1.1", Kind: "evm-transaction", Target: "miner:1", Description: "legacy bind",
		Parameters: map[string]string{
			deploymentManifestHashParameter: "0x1111",
			evmMaximumGasUnitsParameter:     "400000",
			evmMaximumFeePerGasParameter:    "100000000000",
		},
		Spend: Spend{EVMGasWei: "40000000000000000"}, DependsOn: []string{"fleet.mirror.1"},
	}
	proof := Action{
		ID: "fleet.bind.1.1", Kind: "evm-read", Target: "miner:1", Description: "batch proof",
		Parameters: map[string]string{deploymentManifestHashParameter: "0x1111", "batch_installed": "true"},
		DependsOn:  []string{"fleet.install.batch.1"},
	}
	var err error
	ancestor.IntentHash, err = actionIntentHash(ancestor)
	if err != nil {
		t.Fatal(err)
	}
	proof.IntentHash, err = actionIntentHash(proof)
	if err != nil {
		t.Fatal(err)
	}
	prior := &SetupPlan{PlanHash: "0x" + strings.Repeat("11", 32), Actions: []Action{ancestor}}
	revised := &SetupPlan{PlanHash: "0x" + strings.Repeat("22", 32), Actions: []Action{proof}}
	entries := []JournalEntry{{PlanHash: prior.PlanHash, ActionID: ancestor.ID, IntentHash: ancestor.IntentHash, Stage: StageVerified}}
	if err := preserveVerifiedEVMGasReallocations(t.TempDir(), revised, prior, entries); err != nil {
		t.Fatal(err)
	}
	if revised.Actions[0].IntentHash != ancestor.IntentHash || revised.MaximumSpend.EVMGasWei != ancestor.Spend.EVMGasWei {
		t.Fatalf("verified legacy write was not retained exactly: action=%+v maximum=%+v", revised.Actions[0], revised.MaximumSpend)
	}
}

func TestPlanRevisionNeverReconcilesVerifiedOrNonAlphaTransactions(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, _ := derivePublicRoles(cfg)
	prior, _ := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0))
	for _, test := range []struct {
		name    string
		action  Action
		entries []JournalEntry
	}{
		{
			name:   "verified alpha",
			action: actionByID(t, prior, "alpha.transfer.operator-deposit.1"),
		},
		{
			name:   "non-alpha",
			action: actionByID(t, prior, "subnet.hyperparameter.max_allowed_uids"),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			revised, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(2, 0))
			if err != nil {
				t.Fatal(err)
			}
			revised.PriorPlanHashes = []string{prior.PlanHash}
			revised.PlanHash = "0x" + strings.Repeat("cc", 32)
			entries := []JournalEntry{{PlanHash: prior.PlanHash, ActionID: test.action.ID, IntentHash: test.action.IntentHash, Stage: StageFinalized, TransactionHash: "0x" + strings.Repeat("aa", 32), BlockNumber: 123, BlockHash: "0x" + strings.Repeat("bb", 32)}}
			if test.name == "verified alpha" {
				entries = append(entries, JournalEntry{PlanHash: prior.PlanHash, ActionID: test.action.ID, IntentHash: test.action.IntentHash, Stage: StageVerified})
			}
			if err := reconcileFinalizedAlphaTransfers(revised, prior, entries); err != nil {
				t.Fatal(err)
			}
			if actionByID(t, revised, test.action.ID).Kind == "substrate-reconciliation" {
				t.Fatalf("%s was reconciled", test.name)
			}
		})
	}
}

func TestV9RevisionCarriesVerifiedLegacyExactCreditFromV8WithoutDuplicateValidatorTransfer(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, _ := derivePublicRoles(cfg)
	prior, _ := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0))
	revised, _ := buildPlan(cfg, testSetupFacts(), roles, time.Unix(2, 0))
	prior.Schema = setupPlanSchemaV8
	prior.PriorPlanHashes = []string{"0x" + strings.Repeat("88", 32)}
	for index := range prior.Actions {
		if prior.Actions[index].ID != "alpha.transfer.validator.1" {
			continue
		}
		prior.Actions[index].Spend.AlphaRao--
		prior.Actions[index].Parameters["exact_amount_rao"] = strconv.FormatUint(prior.Actions[index].Spend.AlphaRao, 10)
		delete(prior.Actions[index].Parameters, "maximum_destination_rounding_shortfall_rao")
		delete(prior.Actions[index].Parameters, "minimum_destination_credit_rao")
		prior.Actions[index].IntentHash, _ = actionIntentHash(prior.Actions[index])
	}
	wire, err := json.Marshal(prior)
	if err != nil {
		t.Fatal(err)
	}
	var persisted SetupPlan
	if err := json.Unmarshal(wire, &persisted); err != nil {
		t.Fatal(err)
	}
	prior = &persisted
	verified := actionByID(t, prior, "alpha.transfer.validator.1")
	entries := []JournalEntry{{PlanHash: prior.PlanHash, ActionID: verified.ID, IntentHash: verified.IntentHash, Stage: StageVerified}}
	if err := preserveVerifiedValidatorAlphaTransfers(revised, prior, entries); err != nil {
		t.Fatal(err)
	}
	carried := actionByID(t, revised, verified.ID)
	if carried.IntentHash != verified.IntentHash || carried.Spend.AlphaRao != verified.Spend.AlphaRao {
		t.Fatalf("verified v7 exact-credit transfer was not carried: prior=%+v revised=%+v", verified, carried)
	}
	if adjacent := actionByID(t, revised, "alpha.transfer.validator.2"); adjacent.Spend.AlphaRao != actionByID(t, prior, adjacent.ID).Spend.AlphaRao {
		t.Fatal("unverified adjacent validator transfer was modified")
	}

	unsafe, _ := buildPlan(cfg, testSetupFacts(), roles, time.Unix(2, 0))
	for index := range prior.Actions {
		if prior.Actions[index].ID == verified.ID {
			prior.Actions[index].Spend.AlphaRao--
			prior.Actions[index].Parameters["exact_amount_rao"] = strconv.FormatUint(prior.Actions[index].Spend.AlphaRao, 10)
			prior.Actions[index].IntentHash, _ = actionIntentHash(prior.Actions[index])
			verified = prior.Actions[index]
		}
	}
	entries[0].IntentHash = verified.IntentHash
	if err := preserveVerifiedValidatorAlphaTransfers(unsafe, prior, entries); err != nil {
		t.Fatal(err)
	}
	if got := actionByID(t, unsafe, verified.ID); got.IntentHash == verified.IntentHash {
		t.Fatal("two-rao-smaller v7 validator transfer was carried")
	}
}

func partialRevisionFacts(t *testing.T, cfg *ResolvedConfig, count int) *SetupFacts {
	t.Helper()
	facts := *testSetupFacts()
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for churn := 1; churn <= count; churn++ {
		hotkey := roles.Substrate[churnHotkeyLabel(churn)]
		coldkey := roles.Substrate[churnColdkeyLabel(churn)]
		facts.ExistingUIDs = append(facts.ExistingUIDs, ExistingUIDFact{
			UID: uint16(len(facts.ExistingUIDs)), Hotkey: "0x" + hotkey.PublicKeyHex,
			Coldkey: "0x" + coldkey.PublicKeyHex, RegistrationBlock: uint64(100 + churn),
		})
	}
	facts.ExistingUIDCount = uint16(len(facts.ExistingUIDs))
	return &facts
}

func TestBuildPlanRevisionCarriesExactVerifiedIntentsAndRepairsFunding(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, _ := derivePublicRoles(cfg)
	prior, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	registration := actionByID(t, prior, "churn.register.1")
	entries := []JournalEntry{{
		DeploymentID: cfg.Config.Deployment.DeploymentID, PlanHash: prior.PlanHash,
		ActionID: registration.ID, IntentHash: registration.IntentHash, Stage: StageVerified,
	}}
	cfg.Config.Budgets.MaximumNativeTransactionFeeRao = 4_000_000
	current := partialRevisionFacts(t, cfg, 3)
	revised, err := buildPlanRevisionFromFacts(cfg, t.TempDir(), prior, current, entries, time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	if revised.Schema != currentSetupPlanSchema || revised.NativeTransactionFeeLimitRao != 4_000_000 || len(revised.PriorPlanHashes) != 1 || revised.PriorPlanHashes[0] != prior.PlanHash || !revised.allowedPlanHashes()[prior.PlanHash] {
		t.Fatalf("revision lineage/fee limit = schema=%s fee=%d prior=%v", revised.Schema, revised.NativeTransactionFeeLimitRao, revised.PriorPlanHashes)
	}
	if revised.LiveFacts.ExistingUIDCount != prior.LiveFacts.ExistingUIDCount || len(revised.LiveFacts.ExistingUIDs) != len(prior.LiveFacts.ExistingUIDs) {
		t.Fatalf("revision baseline drifted to partial live topology: count=%d facts=%d", revised.LiveFacts.ExistingUIDCount, len(revised.LiveFacts.ExistingUIDs))
	}
	if actionByID(t, revised, registration.ID).IntentHash != registration.IntentHash {
		t.Fatal("unchanged successful registration intent was not carryable")
	}
	if got := actionByID(t, revised, "churn.fund.1").Spend.TAORao; got != 5_000_500 {
		t.Fatalf("revised role funding = %d, want burn+fee+keep-alive 5000500", got)
	}
	if actionByID(t, revised, "churn.fund.1").IntentHash == actionByID(t, prior, "churn.fund.1").IntentHash {
		t.Fatal("funding without its own verified transfer evidence was treated as consumed")
	}
	remaining, err := remainingPlanSpend(revised, entries)
	if err != nil {
		t.Fatal(err)
	}
	if remaining.Registrations != revised.MaximumSpend.Registrations-1 {
		t.Fatalf("ancestor registration was not deducted: remaining=%d maximum=%d", remaining.Registrations, revised.MaximumSpend.Registrations)
	}
}

func TestPlanRevisionCarriesVerifiedV4OperatorAlphaButReplacesValidatorStake(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, _ := derivePublicRoles(cfg)
	prior, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	prior.Schema = "urnetwork-sim-plan-v4"
	prior.AlphaTransferMarginBPS = 0
	prior.MinimumSourceRemainingRao = 0
	legacyAmounts := map[string]uint64{
		"alpha.transfer.operator-deposit.1": 3_250_000_000,
		"alpha.transfer.operator-deposit.2": 2_250_000_000,
		"alpha.transfer.validator.1":        90_000_000,
		"alpha.transfer.validator.2":        90_000_000,
	}
	for index := range prior.Actions {
		action := &prior.Actions[index]
		amount, legacy := legacyAmounts[action.ID]
		if !legacy {
			continue
		}
		action.Spend.AlphaRao = amount
		for _, key := range []string{"exact_amount_rao", "campaign_requirement_rao", "minimum_alpha_at_approved_price_rao", "approved_alpha_price_q9", "runtime_initial_min_stake_tao_rao", "runtime_default_min_transfer_tao_rao", "minimum_tao_equivalent_margin_bps", "planned_existing_stake_rao", "planned_final_stake_rao", "registered_alpha_snapshot_rao", "reserve_target_share_bps", "reserve_minimum_share_bps"} {
			delete(action.Parameters, key)
		}
		action.IntentHash, _ = actionIntentHash(*action)
	}
	// v4 had no reserve-majority barrier.
	filtered := prior.Actions[:0]
	for _, action := range prior.Actions {
		if action.ID != "validator.reserve-majority" {
			dependencies := make([]string, 0, len(action.DependsOn)+1)
			for _, dependency := range action.DependsOn {
				if dependency == "validator.reserve-majority" {
					dependencies = append(dependencies, "alpha.transfer.validator.1", "alpha.transfer.validator.2")
				} else {
					dependencies = append(dependencies, dependency)
				}
			}
			action.DependsOn = dependencies
			action.IntentHash, _ = actionIntentHash(action)
			filtered = append(filtered, action)
		}
	}
	prior.Actions = filtered
	prior.MaximumSpend, err = maximumActionSpend(prior.Actions)
	if err != nil {
		t.Fatal(err)
	}
	prior.PlanHash, err = prior.hash()
	if err != nil || validatePlanBudget(prior) != nil {
		t.Fatalf("construct v4 ancestor: hash_error=%v validation=%v", err, validatePlanBudget(prior))
	}
	entries := make([]JournalEntry, 0, 2)
	for _, id := range []string{"alpha.transfer.operator-deposit.1", "alpha.transfer.operator-deposit.2"} {
		action := actionByID(t, prior, id)
		entries = append(entries, JournalEntry{DeploymentID: prior.DeploymentID, PlanHash: prior.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageVerified})
	}
	current := *testSetupFacts()
	current.AlphaAvailableRao -= 5_500_000_000
	current.AlphaTransferableRao -= 5_500_000_000
	current.WalletNetuidAlphaRao -= 5_500_000_000
	revised, err := buildPlanRevisionFromFacts(cfg, t.TempDir(), prior, &current, entries, time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"alpha.transfer.operator-deposit.1", "alpha.transfer.operator-deposit.2"} {
		if actionByID(t, revised, id).IntentHash != actionByID(t, prior, id).IntentHash {
			t.Fatalf("verified legacy operator transfer %s was not carried", id)
		}
	}
	validator := actionByID(t, revised, "alpha.transfer.validator.1")
	if validator.IntentHash == actionByID(t, prior, validator.ID).IntentHash || validator.Spend.AlphaRao <= 90_000_000 || validator.Parameters["reserve_target_share_bps"] != "6500" {
		t.Fatalf("defective v4 validator stake was carried: %+v", validator)
	}
	if actionByID(t, revised, "validator.reserve-majority").ID == "" {
		t.Fatal("v5 reserve-majority barrier is missing")
	}
}

func TestPlanRevisionV5AlphaApprovalIsStableAcrossEmissions(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	prior, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	entries := make([]JournalEntry, 0, 4)
	for _, id := range []string{
		"alpha.transfer.operator-deposit.1",
		"alpha.transfer.operator-deposit.2",
		"alpha.transfer.validator.1",
		"alpha.transfer.validator.2",
	} {
		action := actionByID(t, prior, id)
		entries = append(entries, JournalEntry{
			DeploymentID: prior.DeploymentID,
			PlanHash:     prior.PlanHash,
			ActionID:     action.ID,
			IntentHash:   action.IntentHash,
			Stage:        StageVerified,
		})
	}

	advance := func(alpha, price uint64) *SetupFacts {
		facts := *testSetupFacts()
		facts.RegisteredAlphaRao += alpha
		facts.AlphaPriceQ9 += price
		facts.AlphaAvailableRao += alpha
		facts.AlphaTransferableRao += alpha
		facts.WalletNetuidAlphaRao += alpha
		facts.ReserveValidatorAlphaRao = actionByID(t, prior, "alpha.transfer.validator.1").Spend.AlphaRao
		facts.IndependentValidatorAlphaRao = actionByID(t, prior, "alpha.transfer.validator.2").Spend.AlphaRao
		return &facts
	}
	first, err := buildPlanRevisionFromFacts(cfg, t.TempDir(), prior, advance(500_000_000_000, 100), entries, time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	second, err := buildPlanRevisionFromFacts(cfg, t.TempDir(), prior, advance(1_500_000_000_000, 300), entries, time.Unix(3, 0))
	if err != nil {
		t.Fatal(err)
	}
	if first.PlanHash != second.PlanHash {
		t.Fatalf("emissions changed a reviewed v5 plan hash: %s != %s", first.PlanHash, second.PlanHash)
	}
	for _, id := range []string{"alpha.transfer.validator.1", "alpha.transfer.validator.2"} {
		want := actionByID(t, prior, id)
		for _, revised := range []*SetupPlan{first, second} {
			got := actionByID(t, revised, id)
			if got.IntentHash != want.IntentHash || got.Spend.AlphaRao != want.Spend.AlphaRao {
				t.Fatalf("%s was resized into a duplicate transfer: got=%+v want=%+v", id, got, want)
			}
		}
	}
	if first.LiveFacts.RegisteredAlphaRao != prior.LiveFacts.RegisteredAlphaRao || first.LiveFacts.AlphaPriceQ9 != prior.LiveFacts.AlphaPriceQ9 {
		t.Fatalf("v5 sizing snapshot drifted: got=%+v prior=%+v", first.LiveFacts, prior.LiveFacts)
	}
	remaining, err := remainingPlanSpend(first, entries)
	if err != nil {
		t.Fatal(err)
	}
	verifiedAlpha := uint64(0)
	for _, entry := range entries {
		verifiedAlpha += actionByID(t, prior, entry.ActionID).Spend.AlphaRao
	}
	if first.MaximumSpend.AlphaRao < verifiedAlpha || remaining.AlphaRao != first.MaximumSpend.AlphaRao-verifiedAlpha {
		t.Fatalf("verified alpha was not deducted exactly: maximum=%d verified=%d remaining=%d", first.MaximumSpend.AlphaRao, verifiedAlpha, remaining.AlphaRao)
	}
}

func TestPlanRevisionCarriesOnlyExactVerifiedUnchangedV6ValidatorAlphaIntoV7(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	prior, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	prior.Schema = "urnetwork-sim-plan-v6"
	legacyMinimum, err := minimumAlphaTransferRao(prior.LiveFacts.InitialMinStakeRao, prior.LiveFacts.AlphaPriceQ9, prior.AlphaTransferMarginBPS)
	if err != nil {
		t.Fatal(err)
	}
	for index := range prior.Actions {
		action := &prior.Actions[index]
		if !strings.HasPrefix(action.ID, "alpha.transfer.") {
			continue
		}
		action.Parameters["runtime_initial_min_stake_tao_rao"] = strconv.FormatUint(prior.LiveFacts.InitialMinStakeRao, 10)
		action.Parameters["minimum_alpha_at_approved_price_rao"] = strconv.FormatUint(legacyMinimum, 10)
		delete(action.Parameters, "runtime_default_min_transfer_tao_rao")
		action.IntentHash, err = actionIntentHash(*action)
		if err != nil {
			t.Fatal(err)
		}
	}
	prior.PlanHash, err = prior.hash()
	if err != nil {
		t.Fatal(err)
	}
	if err := validatePlanBudget(prior); err != nil {
		t.Fatalf("v6 ancestor is invalid: %v", err)
	}
	verified := actionByID(t, prior, "alpha.transfer.validator.1")
	entries := []JournalEntry{{
		DeploymentID: prior.DeploymentID, PlanHash: prior.PlanHash,
		ActionID: verified.ID, IntentHash: verified.IntentHash, Stage: StageVerified,
	}}
	current := *testSetupFacts()
	current.AlphaAvailableRao -= verified.Spend.AlphaRao
	current.AlphaTransferableRao -= verified.Spend.AlphaRao
	current.WalletNetuidAlphaRao -= verified.Spend.AlphaRao
	current.ReserveValidatorAlphaRao = verified.Spend.AlphaRao
	revised, err := buildPlanRevisionFromFacts(cfg, t.TempDir(), prior, &current, entries, time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	carried := actionByID(t, revised, verified.ID)
	if revised.Schema != currentSetupPlanSchema || carried.IntentHash != verified.IntentHash || carried.Parameters["runtime_initial_min_stake_tao_rao"] == "" || carried.Parameters["runtime_default_min_transfer_tao_rao"] != "" {
		t.Fatalf("verified v6 validator transfer was not carried exactly: %+v", carried)
	}
	unverified := actionByID(t, revised, "alpha.transfer.validator.2")
	if unverified.IntentHash == actionByID(t, prior, unverified.ID).IntentHash || unverified.Parameters["runtime_default_min_transfer_tao_rao"] == "" || unverified.Parameters["runtime_initial_min_stake_tao_rao"] != "" {
		t.Fatalf("unverified adjacent validator transfer did not receive the v7 floor: %+v", unverified)
	}
	remaining, err := remainingPlanSpend(revised, entries)
	if err != nil {
		t.Fatal(err)
	}
	if remaining.AlphaRao != revised.MaximumSpend.AlphaRao-verified.Spend.AlphaRao {
		t.Fatalf("carried validator transfer was not deducted: maximum=%d verified=%d remaining=%d", revised.MaximumSpend.AlphaRao, verified.Spend.AlphaRao, remaining.AlphaRao)
	}
	if err := validateApprovedSetupFacts(revised, &current, remaining); err != nil {
		t.Fatalf("valid carried v6 transfer was rejected: %v", err)
	}
	driftedRuntime := current
	driftedRuntime.InitialMinStakeRao++
	if err := validateApprovedSetupFacts(revised, &driftedRuntime, remaining); err == nil || !strings.Contains(err.Error(), "InitialMinStake changed") {
		t.Fatalf("legacy floor runtime drift was accepted: %v", err)
	}

	changed, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(3, 0))
	if err != nil {
		t.Fatal(err)
	}
	for index := range changed.Actions {
		if changed.Actions[index].ID == verified.ID {
			changed.Actions[index].Parameters["reserve_target_share_bps"] = "6600"
			changed.Actions[index].IntentHash, _ = actionIntentHash(changed.Actions[index])
		}
	}
	changedIntent := actionByID(t, changed, verified.ID).IntentHash
	if err := preserveVerifiedValidatorAlphaTransfers(changed, prior, entries); err != nil {
		t.Fatal(err)
	}
	if actionByID(t, changed, verified.ID).IntentHash != changedIntent {
		t.Fatal("semantically changed validator transfer was carried")
	}

	for index := range revised.Actions {
		if revised.Actions[index].ID == verified.ID {
			revised.Actions[index].Parameters["minimum_alpha_at_approved_price_rao"] = "1"
			revised.Actions[index].IntentHash, _ = actionIntentHash(revised.Actions[index])
		}
	}
	if err := validatePlanBudget(revised); err == nil || !strings.Contains(err.Error(), "runtime floor and exact spend") {
		t.Fatalf("mutated carried legacy envelope was accepted: %v", err)
	}
}

func TestBuildPlanRevisionPreservesDurablyConsumedRegistrationFunding(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, _ := derivePublicRoles(cfg)
	prior, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	funding := actionByID(t, prior, "churn.fund.1")
	registration := actionByID(t, prior, "churn.register.1")
	entries := []JournalEntry{
		{DeploymentID: cfg.Config.Deployment.DeploymentID, PlanHash: prior.PlanHash, ActionID: funding.ID, IntentHash: funding.IntentHash, Stage: StageVerified},
		{DeploymentID: cfg.Config.Deployment.DeploymentID, PlanHash: prior.PlanHash, ActionID: registration.ID, IntentHash: registration.IntentHash, Stage: StageVerified},
	}
	cfg.Config.Budgets.MaximumNativeTransactionFeeRao = 4_000_000
	revised, err := buildPlanRevisionFromFacts(cfg, t.TempDir(), prior, partialRevisionFacts(t, cfg, 1), entries, time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	carried := actionByID(t, revised, funding.ID)
	if carried.IntentHash != funding.IntentHash || carried.Spend != funding.Spend {
		t.Fatalf("consumed funding was rewritten: prior=%+v revised=%+v", funding, carried)
	}
	if repaired := actionByID(t, revised, "churn.fund.2"); repaired.Spend.TAORao != 5_000_500 || repaired.IntentHash == actionByID(t, prior, repaired.ID).IntentHash {
		t.Fatalf("unconsumed adjacent funding was not repaired: %+v", repaired)
	}
	remaining, err := remainingPlanSpend(revised, entries)
	if err != nil {
		t.Fatal(err)
	}
	if remaining.TAORao != revised.MaximumSpend.TAORao-funding.Spend.TAORao || remaining.Registrations != revised.MaximumSpend.Registrations-1 {
		t.Fatalf("carried funding/registration spend was not deducted exactly: maximum=%+v remaining=%+v", revised.MaximumSpend, remaining)
	}
}

func TestBuildPlanRevisionRepairsFundingWhenRegistrationIntentChanges(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, _ := derivePublicRoles(cfg)
	prior, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	funding := actionByID(t, prior, "churn.fund.1")
	registration := actionByID(t, prior, "churn.register.1")
	entries := []JournalEntry{
		{DeploymentID: cfg.Config.Deployment.DeploymentID, PlanHash: prior.PlanHash, ActionID: funding.ID, IntentHash: funding.IntentHash, Stage: StageVerified},
		{DeploymentID: cfg.Config.Deployment.DeploymentID, PlanHash: prior.PlanHash, ActionID: registration.ID, IntentHash: registration.IntentHash, Stage: StageVerified},
	}
	cfg.Config.Budgets.MaximumRegistrationBurnRao = 900_000
	revised, err := buildPlanRevisionFromFacts(cfg, t.TempDir(), prior, partialRevisionFacts(t, cfg, 1), entries, time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	repairedFunding := actionByID(t, revised, funding.ID)
	repairedRegistration := actionByID(t, revised, registration.ID)
	if repairedRegistration.IntentHash == registration.IntentHash || repairedFunding.IntentHash == funding.IntentHash || repairedFunding.Spend.TAORao != 3_900_500 {
		t.Fatalf("changed registration retained consumed funding: registration=%+v funding=%+v", repairedRegistration, repairedFunding)
	}
}

func TestBuildPlanRevisionRequiresTheRevisedRemainingBudget(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, _ := derivePublicRoles(cfg)
	prior, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Config.Budgets.MaximumNativeTransactionFeeRao = 4_000_000
	current := partialRevisionFacts(t, cfg, 1)
	revised, err := buildPlanRevisionFromFacts(cfg, t.TempDir(), prior, current, nil, time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	if revised.MaximumSpend.TAORao == 0 {
		t.Fatal("revised plan has no TAO budget")
	}
	current.WalletFreeTAORao = revised.MaximumSpend.TAORao - 1
	if _, err := buildPlanRevisionFromFacts(cfg, t.TempDir(), prior, current, nil, time.Unix(2, 0)); err == nil || !strings.Contains(err.Error(), "revised plan is not affordable") {
		t.Fatalf("unaffordable revised plan was accepted: %v", err)
	}
}

func TestPlanRevisionRejectsEveryAdjacentTopologyDrift(t *testing.T) {
	cfg := testResolvedConfig(t)
	publicRoles, _ := derivePublicRoles(cfg)
	prior, err := buildPlan(cfg, testSetupFacts(), publicRoles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	// V4 did not persist per-hotkey stake; live v5 facts must be free to add it
	// without weakening any immutable identity comparison.
	for index := range prior.LiveFacts.ExistingUIDs {
		prior.LiveFacts.ExistingUIDs[index].TotalHotkeyAlphaRao = 0
	}
	tests := []struct {
		name   string
		mutate func(*SetupFacts)
	}{
		{name: "external insertion", mutate: func(facts *SetupFacts) { facts.ExistingUIDs[2].Hotkey = "0x" + strings.Repeat("ee", 32) }},
		{name: "wrong coldkey", mutate: func(facts *SetupFacts) { facts.ExistingUIDs[2].Coldkey = "0x" + strings.Repeat("ef", 32) }},
		{name: "out of order", mutate: func(facts *SetupFacts) {
			roles, roleErr := BuildRoleSecrets(cfg)
			if roleErr != nil {
				t.Fatal(roleErr)
			}
			facts.ExistingUIDs[2].Hotkey = "0x" + roles.Substrate[churnHotkeyLabel(2)].PublicKeyHex
			facts.ExistingUIDs[2].Coldkey = "0x" + roles.Substrate[churnColdkeyLabel(2)].PublicKeyHex
		}},
		{name: "bootstrap mutation", mutate: func(facts *SetupFacts) { facts.ExistingUIDs[1].RegistrationBlock++ }},
		{name: "owner mutation", mutate: func(facts *SetupFacts) { facts.SubnetOwnerHotkey = "0x" + strings.Repeat("aa", 32) }},
	}
	for _, test := range tests {
		facts := partialRevisionFacts(t, cfg, 1)
		test.mutate(facts)
		_, err := buildPlanRevisionFromFacts(cfg, t.TempDir(), prior, facts, nil, time.Unix(2, 0))
		if err == nil {
			t.Errorf("%s was accepted", test.name)
		}
	}
}

func TestPlanRevisionRejectsDifferentDeploymentIdentity(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, _ := derivePublicRoles(cfg)
	prior, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func(*SetupPlan)
	}{
		{name: "deployment", mutate: func(plan *SetupPlan) { plan.DeploymentID = "other" }},
		{name: "netuid", mutate: func(plan *SetupPlan) { plan.Netuid++ }},
		{name: "owner", mutate: func(plan *SetupPlan) { plan.Owner = fmt.Sprintf("%s-other", plan.Owner) }},
		{name: "policy", mutate: func(plan *SetupPlan) { plan.PolicyHash = "0x" + strings.Repeat("12", 32) }},
	}
	for _, test := range tests {
		changed := *prior
		test.mutate(&changed)
		if _, err := buildPlanRevisionFromFacts(cfg, t.TempDir(), &changed, partialRevisionFacts(t, cfg, 1), nil, time.Unix(2, 0)); err == nil {
			t.Errorf("different %s was accepted", test.name)
		}
	}
}

func TestPlanRevisionIdentityDoesNotReencodeAuthenticatedLegacyWireFormat(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	prior, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}

	// The caller has already authenticated a persisted v5 plan from its exact
	// JSON bytes. Its digest cannot be reproduced by the current struct because that
	// struct emits coordinator_upgrade, a field the historical bytes lack.
	prior.Schema = "urnetwork-sim-plan-v5"
	prior.LiveFacts.InitialMinStakeRao = prior.LiveFacts.DefaultMinTransferRao
	for index := range prior.Actions {
		if !strings.HasPrefix(prior.Actions[index].ID, "alpha.transfer.") {
			continue
		}
		prior.Actions[index].Parameters["runtime_initial_min_stake_tao_rao"] = strconv.FormatUint(prior.LiveFacts.InitialMinStakeRao, 10)
		delete(prior.Actions[index].Parameters, "runtime_default_min_transfer_tao_rao")
		prior.Actions[index].IntentHash, _ = actionIntentHash(prior.Actions[index])
	}
	prior.PlanHash = "0x" + strings.Repeat("ab", 32)
	if err := validatePlanRevisionIdentity(cfg, prior, roles); err != nil {
		t.Fatalf("authenticated legacy plan was re-encoded through v6: %v", err)
	}

	prior.Schema = currentSetupPlanSchema
	if err := validatePlanRevisionIdentity(cfg, prior, roles); err == nil || !strings.Contains(err.Error(), "does not authenticate") {
		t.Fatalf("corrupt native current plan was accepted: %v", err)
	}
}

func TestPolicyRevisionReplacesOperatorCampaignAlphaAndChargesVerifiedAncestor(t *testing.T) {
	priorCfg := testResolvedConfig(t)
	roles, err := derivePublicRoles(priorCfg)
	if err != nil {
		t.Fatal(err)
	}
	prior, err := buildPlan(priorCfg, testSetupFacts(), roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	entries := make([]JournalEntry, 0, 2)
	var verifiedAlpha uint64
	for _, id := range []string{"alpha.transfer.operator-deposit.1", "alpha.transfer.operator-deposit.2"} {
		action := actionByID(t, prior, id)
		verifiedAlpha += action.Spend.AlphaRao
		entries = append(entries, JournalEntry{DeploymentID: prior.DeploymentID, PlanHash: prior.PlanHash, ActionID: id, IntentHash: action.IntentHash, Stage: StageVerified})
	}
	revisedCfg := *priorCfg
	revisedCfg.PolicyHash = "0x" + strings.Repeat("ab", 32)
	current := *testSetupFacts()
	current.AlphaAvailableRao -= verifiedAlpha
	current.AlphaTransferableRao -= verifiedAlpha
	current.WalletNetuidAlphaRao -= verifiedAlpha
	revised, err := buildPlanRevisionFromFacts(&revisedCfg, t.TempDir(), prior, &current, entries, time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	if revised.Schema != currentSetupPlanSchema || revised.SupersededSpend.AlphaRao != verifiedAlpha {
		t.Fatalf("policy revision schema/superseded alpha = %s/%d, want %s/%d", revised.Schema, revised.SupersededSpend.AlphaRao, currentSetupPlanSchema, verifiedAlpha)
	}
	for _, id := range []string{"alpha.transfer.operator-deposit.1", "alpha.transfer.operator-deposit.2"} {
		before, after := actionByID(t, prior, id), actionByID(t, revised, id)
		if before.IntentHash == after.IntentHash || after.Parameters["campaign_policy_hash"] != revisedCfg.PolicyHash {
			t.Fatalf("operator campaign alpha %s was carried across policy change: before=%+v after=%+v", id, before, after)
		}
	}
	if err := validatePlanBudget(revised); err != nil {
		t.Fatalf("cumulative old-plus-new policy spend is not bounded: %v", err)
	}

	voluntary := actionByID(t, prior, "campaign.voluntary-conviction.1")
	unsafe := append(entries, JournalEntry{DeploymentID: prior.DeploymentID, PlanHash: prior.PlanHash, ActionID: voluntary.ID, IntentHash: voluntary.IntentHash, Stage: StageVerified})
	if _, err := buildPlanRevisionFromFacts(&revisedCfg, t.TempDir(), prior, &current, unsafe, time.Unix(2, 0)); err == nil || !strings.Contains(err.Error(), "policy revision is forbidden") {
		t.Fatalf("post-conviction policy migration was accepted: %v", err)
	}
}

func TestPlanRevisionRetainsAuditedCustodyBaselineAndReplacesUndeployedProbe(t *testing.T) {
	cfg := testResolvedConfig(t)
	publicRoles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	prior, err := buildPlan(cfg, testSetupFacts(), publicRoles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	roleSecrets, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	built, err := buildDeploymentPayloads(cfg, roleSecrets, prior.Deployment.InitialNonce)
	if err != nil {
		t.Fatal(err)
	}
	legacy := prior.Deployment
	legacy.RuntimeHashes = cloneStrings(prior.Deployment.RuntimeHashes)
	for index, address := range []common.Address{legacy.ReserveSink, legacy.SettlementVault, legacy.CoordinatorImplementation, legacy.GovernanceDrillImplementation, legacy.PrecompileProbe} {
		legacy.RuntimeHashes[address.Hex()] = fmt.Sprintf("0x%064x", index+1)
	}
	if err := rebindPlanDeployment(prior, legacy); err != nil {
		t.Fatal(err)
	}
	prior.PlanHash, err = prior.hash()
	if err != nil {
		t.Fatal(err)
	}
	existing := legacy
	existing.DeployBlock = 90
	existing.DeployBlockHash = "0x" + strings.Repeat("90", 32)
	existing.RuntimeHashes = cloneStrings(legacy.RuntimeHashes)
	delete(existing.RuntimeHashes, existing.PrecompileProbe.Hex())
	stateDir := t.TempDir()
	if err := saveContractDeployment(stateDir, existing); err != nil {
		t.Fatal(err)
	}
	rebound := contractDeploymentIdentity(legacy)
	rebound.RuntimeHashes = cloneStrings(legacy.RuntimeHashes)
	rebound.RuntimeHashes[rebound.PrecompileProbe.Hex()] = built.Manifest.RuntimeHashes[built.Manifest.PrecompileProbe.Hex()]
	priorHash, _ := contractDeploymentIdentityHash(legacy)
	releaseHash, _ := contractDeploymentIdentityHash(built.Manifest)
	reboundHash, _ := contractDeploymentIdentityHash(rebound)
	baseline := CoordinatorUpgradeBaseline{
		Schema: "urnetwork-coordinator-upgrade-baseline-v1", PriorDeploymentHash: priorHash,
		ReleaseDeploymentHash: releaseHash, ReboundDeploymentHash: reboundHash,
		ReserveSinkExecutableHash: "0x" + strings.Repeat("11", 32), SettlementVaultExecutableHash: "0x" + strings.Repeat("22", 32),
		GovernanceDrillVersion: crypto.Keccak256Hash([]byte("urnetwork/coordinator-adversary/v1")).Hex(), GovernanceProxiableUUID: erc1967ImplementationSlot,
		DeployerNonce: legacy.InitialNonce + 8, ProbeAddressEmpty: true, FinalizedBlock: 100, FinalizedBlockHash: "0x" + strings.Repeat("33", 32),
	}
	current := partialRevisionFacts(t, cfg, 1)
	current.DeployerNonce = baseline.DeployerNonce
	revised, err := buildPlanRevisionFromFactsWithMigration(cfg, stateDir, prior, current, nil, time.Unix(2, 0), &coordinatorUpgradeMigration{Deployment: rebound, Baseline: baseline})
	if err != nil {
		t.Fatal(err)
	}
	if len(revised.SupersededDeployments) != 0 || revised.CoordinatorUpgradeBaseline != baseline {
		t.Fatalf("audited in-place migration was not retained: superseded=%d baseline=%+v", len(revised.SupersededDeployments), revised.CoordinatorUpgradeBaseline)
	}
	if got := revised.Deployment.RuntimeHashes[revised.Deployment.PrecompileProbe.Hex()]; !strings.EqualFold(got, built.Manifest.RuntimeHashes[built.Manifest.PrecompileProbe.Hex()]) {
		t.Fatalf("probe runtime hash=%s, want current release %s", got, built.Manifest.RuntimeHashes[built.Manifest.PrecompileProbe.Hex()])
	}
	if got := revised.Deployment.RuntimeHashes[revised.Deployment.ReserveSink.Hex()]; got != legacy.RuntimeHashes[legacy.ReserveSink.Hex()] {
		t.Fatalf("retained reserve runtime hash=%s", got)
	}
	if err := validatePlanBudget(revised); err != nil {
		t.Fatalf("revised baseline plan is invalid: %v", err)
	}

	// A later release-lock revision must be able to continue after the exact
	// nonce+8 probe finalized but before nonce+9 was broadcast.
	deployed := rebound
	deployed.DeployBlock = 110
	deployed.DeployBlockHash = "0x" + strings.Repeat("66", 32)
	if err := saveContractDeployment(stateDir, deployed); err != nil {
		t.Fatal(err)
	}
	postProbe := *current
	postProbe.DeployerNonce = revised.CoordinatorUpgrade.DeployerNonce
	probe := actionByID(t, revised, "precompile.probe-deploy")
	entries := []JournalEntry{{DeploymentID: revised.DeploymentID, PlanHash: revised.PlanHash, ActionID: probe.ID, IntentHash: probe.IntentHash, Stage: StageVerified}}
	continued, err := buildPlanRevisionFromFactsWithMigration(cfg, stateDir, revised, &postProbe, entries, time.Unix(3, 0), &coordinatorUpgradeMigration{Deployment: rebound, Baseline: baseline})
	if err != nil {
		t.Fatalf("post-probe pre-upgrade continuation was rejected: %v", err)
	}
	if continued.CoordinatorUpgradeBaseline != baseline || continued.Deployment.RuntimeHashes[continued.Deployment.PrecompileProbe.Hex()] != rebound.RuntimeHashes[rebound.PrecompileProbe.Hex()] {
		t.Fatalf("post-probe continuation changed its authenticated baseline: %+v", continued.CoordinatorUpgradeBaseline)
	}
}

func TestPlanRevisionCarriesVerifiedCoordinatorImplementationIntoActivationRetry(t *testing.T) {
	cfg := testResolvedConfig(t)
	publicRoles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	prior, err := buildPlan(cfg, testSetupFacts(), publicRoles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	legacy := prior.Deployment
	legacy.RuntimeHashes = cloneStrings(prior.Deployment.RuntimeHashes)
	legacy.RuntimeHashes[legacy.CoordinatorImplementation.Hex()] = "0x" + strings.Repeat("aa", 32)
	if err := rebindPlanDeployment(prior, legacy); err != nil {
		t.Fatal(err)
	}
	manifestHash, err := contractDeploymentIdentityHash(prior.Deployment)
	if err != nil {
		t.Fatal(err)
	}
	roleSecrets, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	releasePayloads, err := buildDeploymentPayloadsWithRegistrationGeneration(cfg, roleSecrets, prior.Deployment.InitialNonce, prior.Deployment.RegistrationRoleGeneration)
	if err != nil {
		t.Fatal(err)
	}
	releaseHash, err := contractDeploymentIdentityHash(releasePayloads.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	baseline := CoordinatorUpgradeBaseline{
		Schema: "urnetwork-coordinator-upgrade-baseline-v1", PriorDeploymentHash: manifestHash,
		ReleaseDeploymentHash: releaseHash, ReboundDeploymentHash: manifestHash,
		ReserveSinkExecutableHash: "0x" + strings.Repeat("11", 32), SettlementVaultExecutableHash: "0x" + strings.Repeat("22", 32),
		GovernanceDrillVersion: crypto.Keccak256Hash([]byte("urnetwork/coordinator-adversary/v1")).Hex(), GovernanceProxiableUUID: erc1967ImplementationSlot,
		DeployerNonce: prior.Deployment.InitialNonce + 8, ProbeAddressEmpty: true,
		FinalizedBlock: 100, FinalizedBlockHash: "0x" + strings.Repeat("33", 32),
	}
	prior.CoordinatorUpgradeBaseline = baseline
	prior.PriorPlanHashes = []string{"0x" + strings.Repeat("44", 32)}
	prior.PlanHash, err = prior.hash()
	if err != nil {
		t.Fatal(err)
	}
	implementation := actionByID(t, prior, "evm.coordinator-upgrade-implementation")
	entries := []JournalEntry{{DeploymentID: prior.DeploymentID, PlanHash: prior.PlanHash, ActionID: implementation.ID, IntentHash: implementation.IntentHash, Stage: StageVerified}}
	deployed := prior.Deployment
	deployed.DeployBlock = 90
	deployed.DeployBlockHash = "0x" + strings.Repeat("55", 32)
	stateDir := t.TempDir()
	if err := saveContractDeployment(stateDir, deployed); err != nil {
		t.Fatal(err)
	}
	current := *testSetupFacts()
	current.DeployerNonce = prior.CoordinatorUpgrade.DeployerNonce + 1
	migration := &coordinatorUpgradeMigration{Deployment: prior.Deployment, Baseline: baseline, Upgrade: prior.CoordinatorUpgrade}
	revised, err := buildPlanRevisionFromFactsWithMigration(cfg, stateDir, prior, &current, entries, time.Unix(2, 0), migration)
	if err != nil {
		t.Fatal(err)
	}
	carried := actionByID(t, revised, implementation.ID)
	activation := actionByID(t, revised, "evm.coordinator-upgrade-activate")
	if revised.CoordinatorUpgrade != prior.CoordinatorUpgrade || carried.IntentHash != implementation.IntentHash || activation.Parameters["implementation"] != prior.CoordinatorUpgrade.Implementation.Hex() || len(revised.SupersededDeployments) != 0 {
		t.Fatalf("interrupted implementation was not carried into activation retry: upgrade=%+v implementation=%+v activation=%+v superseded=%d", revised.CoordinatorUpgrade, carried, activation, len(revised.SupersededDeployments))
	}
	priorActivation := actionByID(t, prior, "evm.coordinator-upgrade-activate")
	fullyVerified := append(append([]JournalEntry(nil), entries...), JournalEntry{DeploymentID: prior.DeploymentID, PlanHash: prior.PlanHash, ActionID: priorActivation.ID, IntentHash: priorActivation.IntentHash, Stage: StageVerified})
	carriedRelease, err := buildPlanRevisionFromFactsWithMigration(cfg, stateDir, prior, &current, fullyVerified, time.Unix(3, 0), migration)
	if err != nil {
		t.Fatal(err)
	}
	if carriedRelease.CoordinatorUpgrade != prior.CoordinatorUpgrade || actionByID(t, carriedRelease, implementation.ID).IntentHash != implementation.IntentHash || actionByID(t, carriedRelease, priorActivation.ID).IntentHash != priorActivation.IntentHash {
		t.Fatalf("fully activated byte-identical upgrade was not carried: upgrade=%+v implementation=%+v activation=%+v", carriedRelease.CoordinatorUpgrade, actionByID(t, carriedRelease, implementation.ID), actionByID(t, carriedRelease, priorActivation.ID))
	}
	withoutProof := append([]JournalEntry(nil), entries...)
	withoutProof[0].Stage = StageFinalized
	if _, err := buildPlanRevisionFromFactsWithMigration(cfg, stateDir, prior, &current, withoutProof, time.Unix(2, 0), migration); err == nil {
		t.Fatalf("implementation continuation without exact verification was accepted: %v", err)
	}
}

func TestRepeatedCoordinatorUpgradeBoundaryIncludesVerifiedFleetBatcher(t *testing.T) {
	cfg := testResolvedConfig(t)
	publicRoles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	prior, err := buildPlan(cfg, testSetupFacts(), publicRoles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	wantWithoutBatcher := prior.CoordinatorUpgrade.DeployerNonce + 1
	got, batcher, err := repeatedCoordinatorUpgradeBoundary(prior, nil)
	if err != nil || got != wantWithoutBatcher || batcher != nil {
		t.Fatalf("unverified batcher boundary=%d batcher=%v want=%d/nil: %v", got, batcher, wantWithoutBatcher, err)
	}

	batcherAction := actionByID(t, prior, "fleet.refresh.deploy-batcher")
	entries := []JournalEntry{{PlanHash: prior.PlanHash, ActionID: batcherAction.ID, IntentHash: batcherAction.IntentHash, Stage: StageVerified}}
	got, batcher, err = repeatedCoordinatorUpgradeBoundary(prior, entries)
	if err != nil || got != wantWithoutBatcher+1 || batcher == nil || batcher.IntentHash != batcherAction.IntentHash {
		t.Fatalf("verified batcher boundary=%d batcher=%v want=%d/exact: %v", got, batcher, wantWithoutBatcher+1, err)
	}

	malformed := *prior
	malformed.Actions = append([]Action(nil), prior.Actions...)
	for index := range malformed.Actions {
		if malformed.Actions[index].ID != batcherAction.ID {
			continue
		}
		malformed.Actions[index].Parameters = cloneStrings(malformed.Actions[index].Parameters)
		malformed.Actions[index].Parameters["expected_nonce"] = strconv.FormatUint(wantWithoutBatcher+1, 10)
		malformed.Actions[index].IntentHash, err = actionIntentHash(malformed.Actions[index])
		if err != nil {
			t.Fatal(err)
		}
		batcherAction = malformed.Actions[index]
	}
	malformed.PlanHash, err = malformed.hash()
	if err != nil {
		t.Fatal(err)
	}
	malformedEntry := []JournalEntry{{PlanHash: malformed.PlanHash, ActionID: batcherAction.ID, IntentHash: batcherAction.IntentHash, Stage: StageVerified}}
	if _, _, err := repeatedCoordinatorUpgradeBoundary(&malformed, malformedEntry); err == nil || !strings.Contains(err.Error(), "does not bind deployer nonce") {
		t.Fatalf("malformed verified batcher envelope was accepted: %v", err)
	}
}

func TestPlanRevisionChargesRetiredVerifiedEVMGasExactlyOnce(t *testing.T) {
	cfg := testResolvedConfig(t)
	publicRoles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	prior, err := buildPlan(cfg, testSetupFacts(), publicRoles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	retiredIDs := []string{"evm.coordinator-upgrade-implementation", "fleet.refresh.deploy-batcher"}
	entries := make([]JournalEntry, 0, len(retiredIDs))
	want := DecimalUint("17")
	for _, actionID := range retiredIDs {
		action := actionByID(t, prior, actionID)
		entries = append(entries, JournalEntry{PlanHash: prior.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageVerified})
		want, err = addDecimalUint(want, action.Spend.EVMGasWei)
		if err != nil {
			t.Fatal(err)
		}
	}
	revised := *prior
	revised.Actions = append([]Action(nil), prior.Actions...)
	for index := range revised.Actions {
		if !slices.Contains(retiredIDs, revised.Actions[index].ID) {
			continue
		}
		revised.Actions[index].Parameters = cloneStrings(revised.Actions[index].Parameters)
		revised.Actions[index].Parameters["release_generation"] = "next"
		revised.Actions[index].IntentHash, err = actionIntentHash(revised.Actions[index])
		if err != nil {
			t.Fatal(err)
		}
	}
	got, err := addRetiredVerifiedEVMGas(prior, &revised, entries, Spend{EVMGasWei: "17"})
	if err != nil || got.EVMGasWei != want {
		t.Fatalf("retired gas=%s want=%s: %v", got.EVMGasWei, want, err)
	}

	carried := revised
	carried.Actions = append([]Action(nil), revised.Actions...)
	implementation := actionByID(t, prior, retiredIDs[0])
	for index := range carried.Actions {
		if carried.Actions[index].ID != implementation.ID {
			continue
		}
		carried.Actions[index].AcceptedPriorIntentHashes = []string{implementation.IntentHash}
		carried.Actions[index].IntentHash, err = actionIntentHash(carried.Actions[index])
		if err != nil {
			t.Fatal(err)
		}
	}
	wantCarried, err := addDecimalUint("17", actionByID(t, prior, retiredIDs[1]).Spend.EVMGasWei)
	if err != nil {
		t.Fatal(err)
	}
	got, err = addRetiredVerifiedEVMGas(prior, &carried, entries, Spend{EVMGasWei: "17"})
	if err != nil || got.EVMGasWei != wantCarried {
		t.Fatalf("carried gas=%s want=%s: %v", got.EVMGasWei, wantCarried, err)
	}
	got, err = addRetiredVerifiedEVMGas(prior, &revised, nil, Spend{EVMGasWei: "17"})
	if err != nil || got.EVMGasWei != "17" {
		t.Fatalf("unverified gas=%s want=17: %v", got.EVMGasWei, err)
	}
}

func TestPlanRevisionReplacesOnlyVerifiedPreRegistrationCREATEPrefixAndChargesItOnce(t *testing.T) {
	cfg := testResolvedConfig(t)
	publicRoles, _ := derivePublicRoles(cfg)
	prior, err := buildPlan(cfg, testSetupFacts(), publicRoles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	obsolete := prior.Deployment
	obsolete.RuntimeHashes = maps.Clone(prior.Deployment.RuntimeHashes)
	obsolete.DeployBlock = 120
	obsolete.DeployBlockHash = "0x" + strings.Repeat("ab", 32)
	obsolete.RuntimeHashes[obsolete.SettlementVault.Hex()] = "0x" + strings.Repeat("cd", 32)
	stateDir := t.TempDir()
	if err := saveContractDeployment(stateDir, obsolete); err != nil {
		t.Fatal(err)
	}
	entries := make([]JournalEntry, 0, 6)
	for nonce, actionID := range abandonableDeploymentActions {
		action := actionByID(t, prior, actionID)
		entries = append(entries,
			JournalEntry{PlanHash: prior.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageBroadcast, Signer: prior.Roles.Deployer, Nonce: strconv.Itoa(nonce), TransactionHash: fmt.Sprintf("0x%064x", nonce+1)},
			JournalEntry{PlanHash: prior.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageVerified},
		)
	}
	current := partialRevisionFacts(t, cfg, cfg.Config.Topology.ChurnFloorUIDs)
	current.DeployerNonce = 3
	revised, err := buildPlanRevisionFromFacts(cfg, stateDir, prior, current, entries, time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	if revised.Deployment.InitialNonce != 3 || len(revised.SupersededDeployments) != 1 {
		t.Fatalf("replacement deployment = %+v superseded=%d", revised.Deployment, len(revised.SupersededDeployments))
	}
	wantSuperseded := DecimalUint("0")
	for _, actionID := range abandonableDeploymentActions {
		action := actionByID(t, prior, actionID)
		wantSuperseded, err = addDecimalUint(wantSuperseded, action.Spend.EVMGasWei)
		if err != nil {
			t.Fatal(err)
		}
	}
	if comparison, compareErr := revised.SupersededSpend.EVMGasWei.Cmp(wantSuperseded); compareErr != nil || comparison != 0 {
		t.Fatalf("superseded gas = %s, want exact verified CREATE sum %s: %v", revised.SupersededSpend.EVMGasWei, wantSuperseded, compareErr)
	}
	total, err := addSpends(revised.MaximumSpend, revised.SupersededSpend)
	if err != nil || total.EVMGasWei != cfg.MaximumEVMGasWei {
		t.Fatalf("active plus superseded gas = %s, want %s: %v", total.EVMGasWei, cfg.MaximumEVMGasWei, err)
	}
	if actionByID(t, revised, "churn.register.1").IntentHash != actionByID(t, prior, "churn.register.1").IntentHash || actionByID(t, revised, "evm.settlement-vault").IntentHash == actionByID(t, prior, "evm.settlement-vault").IntentHash {
		t.Fatal("replacement failed to preserve native churn and invalidate obsolete contract intent")
	}
}

func TestSupersededDeploymentSpendIgnoresStaleAncestorsAndCountsExactCreatesOnce(t *testing.T) {
	cfg := testResolvedConfig(t)
	publicRoles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	prior, err := buildPlan(cfg, testSetupFacts(), publicRoles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	entries := []JournalEntry{
		{PlanHash: prior.PlanHash, ActionID: "evm.fund-deployer", IntentHash: "0x" + strings.Repeat("aa", 32), Stage: StageVerified},
		{PlanHash: prior.PlanHash, ActionID: "churn.register.1", IntentHash: "0x" + strings.Repeat("bb", 32), Stage: StageVerified},
	}
	want := Spend{}
	for _, actionID := range abandonableDeploymentActions {
		action := actionByID(t, prior, actionID)
		entries = append(entries, JournalEntry{PlanHash: prior.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageVerified})
		if actionID == abandonableDeploymentActions[0] {
			entries = append(entries, JournalEntry{PlanHash: prior.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageVerified})
		}
		want, err = addSpends(want, action.Spend)
		if err != nil {
			t.Fatal(err)
		}
	}
	got, err := supersededVerifiedSpend(prior, entries)
	if err != nil {
		t.Fatal(err)
	}
	equal, err := equalSpend(got, want)
	if err != nil || !equal {
		t.Fatalf("superseded deployment spend = %+v, want %+v: %v", got, want, err)
	}
}

func TestSupersededDeploymentSpendRejectsMissingOrDuplicateCreateActions(t *testing.T) {
	cfg := testResolvedConfig(t)
	publicRoles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	prior, err := buildPlan(cfg, testSetupFacts(), publicRoles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	missing := *prior
	missing.Actions = make([]Action, 0, len(prior.Actions)-1)
	for _, action := range prior.Actions {
		if action.ID != abandonableDeploymentActions[0] {
			missing.Actions = append(missing.Actions, action)
		}
	}
	if _, err := supersededVerifiedSpend(&missing, nil); err == nil || !strings.Contains(err.Error(), "no abandoned deployment action") {
		t.Fatalf("missing CREATE action was accepted: %v", err)
	}
	duplicate := *prior
	duplicate.Actions = append(append([]Action(nil), prior.Actions...), actionByID(t, prior, abandonableDeploymentActions[0]))
	if _, err := supersededVerifiedSpend(&duplicate, nil); err == nil || !strings.Contains(err.Error(), "duplicate abandoned deployment action") {
		t.Fatalf("duplicate CREATE action was accepted: %v", err)
	}
}

func TestPlanRevisionRejectsDeploymentNonceBeyondVerifiedCREATEPrefix(t *testing.T) {
	cfg := testResolvedConfig(t)
	publicRoles, _ := derivePublicRoles(cfg)
	prior, err := buildPlan(cfg, testSetupFacts(), publicRoles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	obsolete := prior.Deployment
	obsolete.RuntimeHashes = maps.Clone(prior.Deployment.RuntimeHashes)
	obsolete.RuntimeHashes[obsolete.SettlementVault.Hex()] = "0x" + strings.Repeat("cd", 32)
	stateDir := t.TempDir()
	if err := saveContractDeployment(stateDir, obsolete); err != nil {
		t.Fatal(err)
	}
	current := partialRevisionFacts(t, cfg, cfg.Config.Topology.ChurnFloorUIDs)
	current.DeployerNonce = 4
	if _, err := buildPlanRevisionFromFacts(cfg, stateDir, prior, current, nil, time.Unix(2, 0)); err == nil || !strings.Contains(err.Error(), "only the 3 pre-registration CREATEs") {
		t.Fatalf("post-registration nonce was accepted for immutable replacement: %v", err)
	}
}

func TestPlanRevisionRejectsVerifiedObsoleteContractStateAfterCreatePrefix(t *testing.T) {
	cfg := testResolvedConfig(t)
	publicRoles, _ := derivePublicRoles(cfg)
	prior, err := buildPlan(cfg, testSetupFacts(), publicRoles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	entries := make([]JournalEntry, 0, 7)
	for offset, actionID := range abandonableDeploymentActions {
		action := actionByID(t, prior, actionID)
		entries = append(entries,
			JournalEntry{PlanHash: prior.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageBroadcast, Signer: prior.Roles.Deployer, Nonce: strconv.Itoa(offset)},
			JournalEntry{PlanHash: prior.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageVerified},
		)
	}
	registered := actionByID(t, prior, "evm.vault-register-escrow")
	entries = append(entries, JournalEntry{PlanHash: prior.PlanHash, ActionID: registered.ID, IntentHash: registered.IntentHash, Stage: StageVerified})
	current := partialRevisionFacts(t, cfg, cfg.Config.Topology.ChurnFloorUIDs)
	current.DeployerNonce = 3
	if err := validateAbandonableDeployment(cfg, prior, prior.Deployment, current, entries); err == nil || !strings.Contains(err.Error(), "already verified") {
		t.Fatalf("verified post-CREATE state was accepted: %v", err)
	}
}

func TestPlanRevisionRequiresExactIntentForEveryAbandonedNonce(t *testing.T) {
	cfg := testResolvedConfig(t)
	publicRoles, _ := derivePublicRoles(cfg)
	prior, err := buildPlan(cfg, testSetupFacts(), publicRoles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	entries := make([]JournalEntry, 0, 6)
	for offset, actionID := range abandonableDeploymentActions {
		action := actionByID(t, prior, actionID)
		entries = append(entries, JournalEntry{PlanHash: prior.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageBroadcast, Signer: prior.Roles.Deployer, Nonce: strconv.Itoa(offset)})
		intentHash := action.IntentHash
		if offset == 1 {
			intentHash = "0x" + strings.Repeat("ff", 32)
		}
		entries = append(entries, JournalEntry{PlanHash: prior.PlanHash, ActionID: action.ID, IntentHash: intentHash, Stage: StageVerified})
	}
	current := partialRevisionFacts(t, cfg, cfg.Config.Topology.ChurnFloorUIDs)
	current.DeployerNonce = 3
	if err := validateAbandonableDeployment(cfg, prior, prior.Deployment, current, entries); err == nil || !strings.Contains(err.Error(), "not the verified supersedable action") {
		t.Fatalf("mismatched verified intent was accepted: %v", err)
	}
}

func TestSupersededDeploymentObservationRejectsEveryAdjacentLiveMutation(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	payloads, err := buildDeploymentPayloads(cfg, roles, 0)
	if err != nil {
		t.Fatal(err)
	}
	manifest := payloads.Manifest
	manifest.DeployBlock = 120
	manifest.DeployBlockHash = "0x" + strings.Repeat("12", 32)
	current := testSetupFacts()
	current.DeployerNonce = 3
	current.EVMFinalizedBlock = 150
	current.EVMFinalizedBlockHash = "0x" + strings.Repeat("15", 32)
	valid := supersededDeploymentObservation{
		FinalizedHead:      ChainHead{Number: 160, Hash: "0x" + strings.Repeat("16", 32)},
		DeployBlockHash:    manifest.DeployBlockHash,
		DeployerNonce:      current.DeployerNonce,
		RuntimeCodeHashes:  map[common.Address]string{},
		Balances:           map[common.Address]*big.Int{},
		NativeFreeBalances: map[common.Address]uint64{},
	}
	expectedHashes, err := normalizedDeploymentRuntimeHashes(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for index, address := range contractDeploymentAddresses(manifest) {
		if index < 3 {
			valid.RuntimeCodeHashes[address] = expectedHashes[address]
		} else {
			valid.RuntimeCodeHashes[address] = ""
		}
		valid.Balances[address] = new(big.Int)
		valid.NativeFreeBalances[address] = 0
	}
	if err := validateSupersededDeploymentObservation(manifest, CoordinatorUpgrade{}, nil, current, valid); err != nil {
		t.Fatalf("valid inert deployment was rejected: %v", err)
	}
	clone := func(observation supersededDeploymentObservation) supersededDeploymentObservation {
		observation.RuntimeCodeHashes = maps.Clone(observation.RuntimeCodeHashes)
		observation.NativeFreeBalances = maps.Clone(observation.NativeFreeBalances)
		observation.Balances = make(map[common.Address]*big.Int, len(observation.Balances))
		for address, balance := range valid.Balances {
			observation.Balances[address] = new(big.Int).Set(balance)
		}
		return observation
	}
	addresses := contractDeploymentAddresses(manifest)
	tests := []struct {
		name   string
		mutate func(*supersededDeploymentObservation)
	}{
		{name: "older finalized head", mutate: func(observation *supersededDeploymentObservation) { observation.FinalizedHead.Number = 149 }},
		{name: "new deployer nonce", mutate: func(observation *supersededDeploymentObservation) { observation.DeployerNonce++ }},
		{name: "noncanonical checkpoint", mutate: func(observation *supersededDeploymentObservation) {
			observation.DeployBlockHash = "0x" + strings.Repeat("17", 32)
		}},
		{name: "deployed runtime drift", mutate: func(observation *supersededDeploymentObservation) {
			observation.RuntimeCodeHashes[addresses[1]] = "0x" + strings.Repeat("18", 32)
		}},
		{name: "later contract deployed", mutate: func(observation *supersededDeploymentObservation) {
			observation.RuntimeCodeHashes[addresses[3]] = expectedHashes[addresses[3]]
		}},
		{name: "retained balance", mutate: func(observation *supersededDeploymentObservation) { observation.Balances[addresses[0]].SetUint64(1) }},
		{name: "hidden native balance", mutate: func(observation *supersededDeploymentObservation) { observation.NativeFreeBalances[addresses[0]] = 500 }},
		{name: "missing balance", mutate: func(observation *supersededDeploymentObservation) { delete(observation.Balances, addresses[2]) }},
		{name: "reserve linked", mutate: func(observation *supersededDeploymentObservation) {
			observation.ReserveRecorder = common.HexToAddress("0x1")
		}},
		{name: "escrow registered", mutate: func(observation *supersededDeploymentObservation) { observation.EscrowRegistered = true }},
		{name: "vault linked", mutate: func(observation *supersededDeploymentObservation) {
			observation.VaultCoordinator = common.HexToAddress("0x2")
		}},
	}
	for _, test := range tests {
		observation := clone(valid)
		test.mutate(&observation)
		if err := validateSupersededDeploymentObservation(manifest, CoordinatorUpgrade{}, nil, current, observation); err == nil {
			t.Errorf("%s was accepted", test.name)
		}
	}
}

func TestFullyInstalledDeploymentIsSupersedableOnlyWhenEconomicallyInert(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	payloads, err := buildDeploymentPayloads(cfg, roles, 3)
	if err != nil {
		t.Fatal(err)
	}
	manifest, upgrade := payloads.Manifest, payloads.CoordinatorUpgrade
	manifest.DeployBlock = 120
	manifest.DeployBlockHash = "0x" + strings.Repeat("12", 32)
	current := testSetupFacts()
	current.DeployerNonce = manifest.InitialNonce + uint64(len(supersedableDeploymentNonceActions))
	current.EVMFinalizedBlock = 150
	current.EVMFinalizedBlockHash = "0x" + strings.Repeat("15", 32)
	zero := func() *big.Int { return new(big.Int) }
	expectedOperators := []supersededOperatorIdentity{
		{NoID: 1, Coldkey: [32]byte{1}, PoolHotkey: [32]byte{2}, DepositHotkey: [32]byte{3}, DepositSigner: common.HexToAddress("0x11"), RootSigner: common.HexToAddress("0x12")},
		{NoID: 2, Coldkey: [32]byte{4}, PoolHotkey: [32]byte{5}, DepositHotkey: [32]byte{6}, DepositSigner: common.HexToAddress("0x21"), RootSigner: common.HexToAddress("0x22")},
	}
	valid := supersededDeploymentObservation{
		FinalizedHead:        ChainHead{Number: 160, Hash: "0x" + strings.Repeat("16", 32)},
		DeployBlockHash:      manifest.DeployBlockHash,
		DeployerNonce:        current.DeployerNonce,
		RuntimeCodeHashes:    map[common.Address]string{},
		Balances:             map[common.Address]*big.Int{},
		NativeFreeBalances:   map[common.Address]uint64{},
		EscrowRegistered:     true,
		VaultCoordinator:     manifest.CoordinatorProxy,
		ReserveRecorder:      manifest.CoordinatorProxy,
		ProxyImplementation:  upgrade.Implementation,
		UpgradeRuntimeHash:   upgrade.RuntimeCodeHash,
		UpgradeBalance:       zero(),
		OperatorCount:        new(big.Int).SetUint64(uint64(len(expectedOperators))),
		CampaignReserved:     zero(),
		ReservePrincipal:     zero(),
		ReserveLiveStake:     zero(),
		TotalCaptured:        zero(),
		TotalPaid:            zero(),
		EscrowAccounted:      zero(),
		PendingFunding:       zero(),
		OutstandingLiability: zero(),
		LiveEscrowStake:      zero(),
	}
	for index, identity := range expectedOperators {
		valid.Operators = append(valid.Operators, supersededOperatorObservation{
			Identity: identity, VersionCount: 1, Active: true, PoolUID: uint16(51 + index), PoolActive: true,
			NextDepositNonce: zero(), CumulativeConviction: zero(), Carry: zero(), ReservePrincipal: zero(), PoolLiveStake: zero(),
		})
	}
	expectedHashes, err := normalizedDeploymentRuntimeHashes(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for _, address := range contractDeploymentAddresses(manifest) {
		valid.RuntimeCodeHashes[address] = expectedHashes[address]
		valid.Balances[address] = zero()
		expectedNative := uint64(0)
		if address == manifest.SettlementVault || address == manifest.CoordinatorProxy {
			expectedNative = current.ExistentialDepositRao
		}
		valid.NativeFreeBalances[address] = expectedNative
	}
	if err := validateSupersededDeploymentObservation(manifest, upgrade, expectedOperators, current, valid); err != nil {
		t.Fatalf("fully installed inert generation was rejected: %v", err)
	}
	clone := func(observation supersededDeploymentObservation) supersededDeploymentObservation {
		observation.RuntimeCodeHashes = maps.Clone(observation.RuntimeCodeHashes)
		observation.NativeFreeBalances = maps.Clone(observation.NativeFreeBalances)
		observation.Balances = make(map[common.Address]*big.Int, len(observation.Balances))
		for address, balance := range valid.Balances {
			observation.Balances[address] = new(big.Int).Set(balance)
		}
		for _, field := range []**big.Int{
			&observation.UpgradeBalance, &observation.OperatorCount, &observation.CampaignReserved,
			&observation.ReservePrincipal, &observation.ReserveLiveStake, &observation.TotalCaptured,
			&observation.TotalPaid, &observation.EscrowAccounted, &observation.PendingFunding,
			&observation.OutstandingLiability, &observation.LiveEscrowStake,
		} {
			*field = new(big.Int).Set(*field)
		}
		observation.Operators = append([]supersededOperatorObservation(nil), observation.Operators...)
		for index := range observation.Operators {
			operator := &observation.Operators[index]
			for _, field := range []**big.Int{
				&operator.NextDepositNonce, &operator.CumulativeConviction, &operator.Carry,
				&operator.ReservePrincipal, &operator.PoolLiveStake,
			} {
				*field = new(big.Int).Set(*field)
			}
		}
		return observation
	}
	tests := []struct {
		name   string
		mutate func(*supersededDeploymentObservation)
	}{
		{"escrow registration", func(value *supersededDeploymentObservation) { value.EscrowRegistered = false }},
		{"vault link", func(value *supersededDeploymentObservation) { value.VaultCoordinator = common.Address{} }},
		{"reserve link", func(value *supersededDeploymentObservation) { value.ReserveRecorder = common.Address{} }},
		{"proxy implementation", func(value *supersededDeploymentObservation) {
			value.ProxyImplementation = manifest.CoordinatorImplementation
		}},
		{"upgrade runtime", func(value *supersededDeploymentObservation) {
			value.UpgradeRuntimeHash = "0x" + strings.Repeat("99", 32)
		}},
		{"upgrade EVM balance", func(value *supersededDeploymentObservation) { value.UpgradeBalance.SetUint64(1) }},
		{"upgrade native balance", func(value *supersededDeploymentObservation) { value.UpgradeNativeFree = 1 }},
		{"missing vault existential deposit", func(value *supersededDeploymentObservation) { value.NativeFreeBalances[manifest.SettlementVault] = 0 }},
		{"proxy native surplus", func(value *supersededDeploymentObservation) { value.NativeFreeBalances[manifest.CoordinatorProxy]++ }},
		{"operator", func(value *supersededDeploymentObservation) { value.OperatorCount.SetUint64(1) }},
		{"operator identity", func(value *supersededDeploymentObservation) { value.Operators[0].Identity.PoolHotkey[0]++ }},
		{"operator version", func(value *supersededDeploymentObservation) { value.Operators[0].VersionCount++ }},
		{"operator inactive", func(value *supersededDeploymentObservation) { value.Operators[0].Active = false }},
		{"pool inactive", func(value *supersededDeploymentObservation) { value.Operators[0].PoolActive = false }},
		{"deposit nonce", func(value *supersededDeploymentObservation) { value.Operators[0].NextDepositNonce.SetUint64(1) }},
		{"operator conviction", func(value *supersededDeploymentObservation) { value.Operators[0].CumulativeConviction.SetUint64(1) }},
		{"operator carry", func(value *supersededDeploymentObservation) { value.Operators[0].Carry.SetUint64(1) }},
		{"operator principal", func(value *supersededDeploymentObservation) { value.Operators[0].ReservePrincipal.SetUint64(1) }},
		{"pool live stake", func(value *supersededDeploymentObservation) { value.Operators[0].PoolLiveStake.SetUint64(1) }},
		{"campaign reserve", func(value *supersededDeploymentObservation) { value.CampaignReserved.SetUint64(1) }},
		{"reserve principal", func(value *supersededDeploymentObservation) { value.ReservePrincipal.SetUint64(1) }},
		{"reserve live stake", func(value *supersededDeploymentObservation) { value.ReserveLiveStake.SetUint64(1) }},
		{"captured", func(value *supersededDeploymentObservation) { value.TotalCaptured.SetUint64(1) }},
		{"paid", func(value *supersededDeploymentObservation) { value.TotalPaid.SetUint64(1) }},
		{"escrow accounted", func(value *supersededDeploymentObservation) { value.EscrowAccounted.SetUint64(1) }},
		{"pending funding", func(value *supersededDeploymentObservation) { value.PendingFunding.SetUint64(1) }},
		{"liability", func(value *supersededDeploymentObservation) { value.OutstandingLiability.SetUint64(1) }},
		{"escrow live stake", func(value *supersededDeploymentObservation) { value.LiveEscrowStake.SetUint64(1) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			observation := clone(valid)
			test.mutate(&observation)
			if err := validateSupersededDeploymentObservation(manifest, upgrade, expectedOperators, current, observation); err == nil {
				t.Fatal("live mutation was accepted")
			}
		})
	}
}

func TestFullyInstalledDeploymentRequiresExactVerifiedNoncePrefixAndActivation(t *testing.T) {
	cfg := testResolvedConfig(t)
	publicRoles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	prior, err := buildPlan(cfg, testSetupFacts(), publicRoles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	entries := make([]JournalEntry, 0, 2*len(supersedableDeploymentNonceActions)+9)
	for offset, actionID := range supersedableDeploymentNonceActions {
		action := actionByID(t, prior, actionID)
		entries = append(entries,
			JournalEntry{PlanHash: prior.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageBroadcast, Signer: prior.Roles.Deployer, Nonce: strconv.FormatUint(prior.Deployment.InitialNonce+uint64(offset), 10)},
			JournalEntry{PlanHash: prior.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageVerified},
		)
	}
	for _, actionID := range []string{"evm.coordinator-upgrade-activate", "policy.schedule-bootstrap"} {
		action := actionByID(t, prior, actionID)
		entries = append(entries, JournalEntry{PlanHash: prior.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageVerified})
	}
	for _, actionID := range []string{
		"operator.register.1", "alpha.transfer.operator-deposit.1",
		"operator.register.2", "alpha.transfer.operator-deposit.2",
		"campaign.evm-gas-reserve", "config.render", "accounts.provision",
	} {
		action := actionByID(t, prior, actionID)
		entries = append(entries, JournalEntry{PlanHash: prior.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageVerified})
	}
	current := testSetupFacts()
	current.DeployerNonce = prior.Deployment.InitialNonce + uint64(len(supersedableDeploymentNonceActions))
	current.ExistingUIDCount = 256
	if err := validateAbandonableDeployment(cfg, prior, prior.Deployment, current, entries); err != nil {
		t.Fatalf("exact fully installed generation was rejected: %v", err)
	}
	missingActivation := make([]JournalEntry, 0, len(entries)-1)
	for _, entry := range entries {
		if entry.ActionID != "evm.coordinator-upgrade-activate" {
			missingActivation = append(missingActivation, entry)
		}
	}
	if err := validateAbandonableDeployment(cfg, prior, prior.Deployment, current, missingActivation); err == nil || !strings.Contains(err.Error(), "upgrade activation") {
		t.Fatalf("missing activation proof was accepted: %v", err)
	}
	partial := *current
	partial.DeployerNonce--
	if err := validateAbandonableDeployment(cfg, prior, prior.Deployment, &partial, entries); err == nil || !strings.Contains(err.Error(), "exact 10-nonce") {
		t.Fatalf("unsafe intermediate prefix was accepted: %v", err)
	}
	topology := actionByID(t, prior, "topology.launch")
	unsafe := append(append([]JournalEntry(nil), entries...), JournalEntry{PlanHash: prior.PlanHash, ActionID: topology.ID, IntentHash: topology.IntentHash, Stage: StageVerified})
	if err := validateAbandonableDeployment(cfg, prior, prior.Deployment, current, unsafe); err == nil || !strings.Contains(err.Error(), "topology.launch") {
		t.Fatalf("economically used generation was accepted: %v", err)
	}
	want := Spend{}
	wantActionIDs := append(append([]string(nil), supersedableDeploymentEffectActions...), "operator.register.1", "operator.register.2")
	for _, actionID := range wantActionIDs {
		action := actionByID(t, prior, actionID)
		want, err = addSpends(want, action.Spend)
		if err != nil {
			t.Fatal(err)
		}
	}
	got, err := supersededVerifiedSpend(prior, entries)
	if equal, equalErr := equalSpend(got, want); err != nil || equalErr != nil || !equal {
		t.Fatalf("superseded full-generation spend=%+v want=%+v errors=%v/%v", got, want, err, equalErr)
	}
}

func TestPlanRevisionTopologyAcceptsOnlyTheApprovedTournamentSequence(t *testing.T) {
	cfg := testResolvedConfig(t)
	publicRoles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	prior, err := buildPlan(cfg, testSetupFacts(), publicRoles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	for index := range prior.LiveFacts.ExistingUIDs {
		prior.LiveFacts.ExistingUIDs[index].TotalHotkeyAlphaRao = 0
	}
	stateDir := t.TempDir()
	deployment := ContractDeployment{
		Schema: "urnetwork-contract-deployment-v1", DeploymentID: cfg.Config.Deployment.DeploymentID,
		SettlementVault: common.HexToAddress("0x0000000000000000000000000000000000000521"),
	}
	if err := saveContractDeployment(stateDir, deployment); err != nil {
		t.Fatal(err)
	}
	roleSecrets, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	initialLabels := initialTopologyRoleLabels(cfg.Config.Topology, 0)
	allLabels := append([]string(nil), initialLabels...)
	for challenger := 1; challenger <= cfg.Config.Topology.ChallengerFleets; challenger++ {
		allLabels = append(allLabels, fleetHotkeyLabel(cfg.Config.Topology.HeadFleets+challenger))
	}
	identities, err := expectedRegistrationIdentities(cfg, prior, 0, roleSecrets, allLabels)
	if err != nil {
		t.Fatal(err)
	}
	fullFacts := func(replacements int) *SetupFacts {
		facts := *testSetupFacts()
		labels := append([]string(nil), initialLabels...)
		for index := 0; index < replacements; index++ {
			labels[index] = fleetHotkeyLabel(cfg.Config.Topology.HeadFleets + index + 1)
		}
		for index, label := range labels {
			identity := identities[label]
			facts.ExistingUIDs = append(facts.ExistingUIDs, ExistingUIDFact{
				UID: uint16(len(prior.LiveFacts.ExistingUIDs) + index), Hotkey: fmt.Sprintf("0x%x", identity.hotkey),
				Coldkey: fmt.Sprintf("0x%x", identity.coldkey), RegistrationBlock: uint64(1_000 + index),
			})
		}
		facts.ExistingUIDCount = uint16(len(facts.ExistingUIDs))
		return &facts
	}
	for replacements := 0; replacements <= cfg.Config.Topology.ChallengerFleets; replacements++ {
		if err := validatePlanRevisionTopology(cfg, stateDir, prior, fullFacts(replacements), roleSecrets); err != nil {
			t.Errorf("approved tournament prefix with %d replacements was rejected: %v", replacements, err)
		}
	}
	for name, mutate := range map[string]func(*ExistingUIDFact){
		"uid":                func(fact *ExistingUIDFact) { fact.UID++ },
		"hotkey":             func(fact *ExistingUIDFact) { fact.Hotkey = "0x" + strings.Repeat("11", 32) },
		"coldkey":            func(fact *ExistingUIDFact) { fact.Coldkey = "0x" + strings.Repeat("22", 32) },
		"registration block": func(fact *ExistingUIDFact) { fact.RegistrationBlock++ },
		"owner status":       func(fact *ExistingUIDFact) { fact.SubnetOwner = !fact.SubnetOwner },
	} {
		drifted := fullFacts(0)
		mutate(&drifted.ExistingUIDs[0])
		if err := validatePlanRevisionTopology(cfg, stateDir, prior, drifted, roleSecrets); err == nil {
			t.Errorf("bootstrap %s mutation was accepted", name)
		}
	}
	outOfOrder := fullFacts(0)
	challenger := identities[fleetHotkeyLabel(cfg.Config.Topology.HeadFleets+1)]
	outOfOrder.ExistingUIDs[len(prior.LiveFacts.ExistingUIDs)+1].Hotkey = fmt.Sprintf("0x%x", challenger.hotkey)
	outOfOrder.ExistingUIDs[len(prior.LiveFacts.ExistingUIDs)+1].Coldkey = fmt.Sprintf("0x%x", challenger.coldkey)
	if err := validatePlanRevisionTopology(cfg, stateDir, prior, outOfOrder, roleSecrets); err == nil {
		t.Fatal("out-of-order challenger replacement was accepted")
	}
	extra := fullFacts(0)
	extra.ExistingUIDs = append(extra.ExistingUIDs, ExistingUIDFact{UID: uint16(len(extra.ExistingUIDs)), Hotkey: "0x" + strings.Repeat("ab", 32), Coldkey: "0x" + strings.Repeat("cd", 32), RegistrationBlock: 2_000})
	extra.ExistingUIDCount++
	if err := validatePlanRevisionTopology(cfg, stateDir, prior, extra, roleSecrets); err == nil {
		t.Fatal("unapproved post-topology insertion was accepted")
	}
}
