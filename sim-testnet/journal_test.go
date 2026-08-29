package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestJournalHashChainAndExclusiveLock(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	j, err := OpenJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := OpenJournal(dir); err == nil {
		t.Fatal("second deployment journal acquired the same lock")
	}
	for _, stage := range []JournalStage{StageIntent, StageBroadcast, StageIncluded, StageFinalized, StageVerified} {
		entry := JournalEntry{DeploymentID: "d", PlanHash: "p", ActionID: "a", IntentHash: "i", Stage: stage}
		switch stage {
		case StageBroadcast:
			entry.Signer, entry.Nonce, entry.TransactionHash = "signer", "1", "0xtx"
			entry.RecoveryBlock, entry.RecoveryBlockHash = 1, "0xcheckpoint"
		case StageIncluded, StageFinalized:
			entry.TransactionHash, entry.BlockNumber, entry.BlockHash = "0xtx", 1, "0xblock"
		case StageVerified:
			entry.PostconditionHash = "0xpost"
			entry.PostconditionPath = "receipts/postconditions/a.json"
		}
		if err := j.Append(entry); err != nil {
			t.Fatal(err)
		}
	}
	entries := j.Entries()
	if len(entries) != 5 || entries[4].PreviousHash != entries[3].EntryHash {
		t.Fatalf("journal chain = %+v", entries)
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := readJournal(filepath.Join(dir, "journal.jsonl")); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "journal.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	b = []byte(strings.Replace(string(b), `"stage":"intent"`, `"stage":"failed"`, 1))
	if err := os.WriteFile(filepath.Join(dir, "journal.jsonl"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenJournal(dir); err == nil {
		t.Fatal("tampered journal was accepted")
	}
}

func TestJournalRejectsUnlinkedOrEscapingPostcondition(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	j, err := OpenJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	base := JournalEntry{DeploymentID: "d", PlanHash: "p", ActionID: "safe.action", IntentHash: "i", Stage: StageVerified, PostconditionHash: "0xpost"}
	if err := j.Append(base); err == nil {
		t.Fatal("verified stage without a postcondition path was accepted")
	}
	base.PostconditionPath = "../escape.json"
	if err := j.Append(base); err == nil {
		t.Fatal("escaping postcondition path was accepted")
	}
	base.PostconditionPath = "receipts/postconditions/safe.action.json"
	if err := j.Append(base); err != nil {
		t.Fatal(err)
	}
}

func TestPersistedPlanSurvivesChangedLiveBalances(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, _ := derivePublicRoles(cfg)
	plan, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	b, _ := json.Marshal(plan)
	if err := atomicWrite(filepath.Join(dir, "plan.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadPersistedPlan(cfg, dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.PlanHash != plan.PlanHash || loaded.LiveFacts.AlphaAvailableRao != plan.LiveFacts.AlphaAvailableRao {
		t.Fatalf("persisted plan changed: %+v", loaded)
	}
	originalRelease := cfg.Release
	changedRelease := *cfg.Release
	changedRelease.Release = "1.0-drift"
	cfg.Release = &changedRelease
	if _, err := loadPersistedPlan(cfg, dir); err == nil {
		t.Fatal("persisted plan survived release-lock drift")
	}
	cfg.Release = originalRelease
	originalAuthority := cfg.Authority
	cfg.Authority = "changed-private-rpc.example:9944"
	if _, err := loadPersistedPlan(cfg, dir); err == nil {
		t.Fatal("persisted plan survived resolved launch-input drift")
	}
	cfg.Authority = originalAuthority
	loaded.LiveFacts.FinalizedBlock++
	loaded.LiveFacts.FinalizedBlockHash = "0x" + strings.Repeat("ef", 32)
	loaded.LiveFacts.AlphaAvailableRao++
	loaded.LiveFacts.WalletFreeTAORao++
	loaded.LiveFacts.BurnRao++
	b, _ = json.Marshal(loaded)
	if err := os.WriteFile(filepath.Join(dir, "plan.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	observed, err := loadPersistedPlan(cfg, dir)
	if err != nil {
		t.Fatalf("observation-only drift invalidated the approved plan: %v", err)
	}
	if observed.PlanHash != plan.PlanHash || observed.LiveFacts.BurnRao != plan.LiveFacts.BurnRao+1 {
		t.Fatalf("persisted observation drift was not preserved: %+v", observed.LiveFacts)
	}
}

func TestPersistedPlanRejectsApprovalBoundTampering(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, _ := derivePublicRoles(cfg)
	plan, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	plan.LiveFacts.MinBurnRao++
	dir := t.TempDir()
	b, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(dir, "plan.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadPersistedPlan(cfg, dir); err == nil {
		t.Fatal("approval-bound registration economics tampering was accepted")
	}
}

func TestPersistedPlanRejectsSelfConsistentConfiguredLimitDrift(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, _ := derivePublicRoles(cfg)
	plan, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	plan.Limits.TAORao++
	plan.PlanHash, err = plan.hash()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	b, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(dir, "plan.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadPersistedPlan(cfg, dir); !errors.Is(err, errPersistedPlanIdentityMismatch) {
		t.Fatalf("self-consistent configured-limit drift error=%v, want identity mismatch", err)
	}
}

func TestPersistedPlanRefreshIsLimitedToEmptyPrewriteState(t *testing.T) {
	if !mayRefreshPersistedPlan(os.ErrNotExist, nil) {
		t.Fatal("missing plan with an empty journal was not refreshable")
	}
	if !mayRefreshPersistedPlan(errPersistedPlanIdentityMismatch, nil) {
		t.Fatal("release-drifted plan with an empty journal was not refreshable")
	}
	for name, planErr := range map[string]error{
		"malformed plan": errors.New("invalid JSON"),
		"valid plan":     nil,
	} {
		if mayRefreshPersistedPlan(planErr, nil) {
			t.Errorf("%s was refreshable", name)
		}
	}
	for _, stage := range []JournalStage{StageIntent, StageFailed, StageBroadcast, StageIncluded, StageFinalized, StageVerified} {
		if mayRefreshPersistedPlan(errPersistedPlanIdentityMismatch, []JournalEntry{{Stage: stage}}) {
			t.Errorf("plan was refreshable after journal stage %s", stage)
		}
	}
}

func TestRemainingPlanSpendSubtractsVerifiedWritesButPreservesReserves(t *testing.T) {
	plan := &SetupPlan{PlanHash: "plan", MaximumSpend: Spend{TAORao: 100, AlphaRao: 80, EVMGasWei: decimalUint64(70)}}
	plan.Actions = []Action{
		{ID: "written", IntentHash: "write-intent", Kind: "substrate-transaction", Spend: Spend{TAORao: 25, AlphaRao: 30, EVMGasWei: decimalUint64(10)}},
		{ID: "reserved", IntentHash: "reserve-intent", Kind: "budget-reserve", Spend: Spend{TAORao: 40, EVMGasWei: decimalUint64(20)}},
	}
	entries := []JournalEntry{
		{PlanHash: "plan", ActionID: "written", IntentHash: "write-intent", Stage: StageVerified},
		{PlanHash: "plan", ActionID: "reserved", IntentHash: "reserve-intent", Stage: StageVerified},
	}
	remaining, err := remainingPlanSpend(plan, entries)
	if err != nil {
		t.Fatal(err)
	}
	if remaining.TAORao != 75 || remaining.AlphaRao != 50 || remaining.EVMGasWei != decimalUint64(60) {
		t.Fatalf("remaining spend = %+v", remaining)
	}
	entries = append(entries, JournalEntry{PlanHash: "plan", ActionID: "written", IntentHash: "write-intent", Stage: StageFailed})
	remaining, err = remainingPlanSpend(plan, entries)
	if err != nil {
		t.Fatal(err)
	}
	if remaining.TAORao != 75 || remaining.AlphaRao != 50 || remaining.EVMGasWei != decimalUint64(60) {
		t.Fatalf("later unrelated stage hid verified spend: %+v", remaining)
	}
}

func TestRemainingPlanSpendCarriesOnlyExactApprovedAncestorIntents(t *testing.T) {
	plan := &SetupPlan{
		PlanHash: "active", PriorPlanHashes: []string{"ancestor"}, MaximumSpend: Spend{TAORao: 100},
		Actions: []Action{{ID: "action", IntentHash: "exact", Spend: Spend{TAORao: 25}}},
	}
	entries := []JournalEntry{
		{PlanHash: "unapproved", ActionID: "action", IntentHash: "exact", Stage: StageVerified},
		{PlanHash: "ancestor", ActionID: "action", IntentHash: "wrong", Stage: StageVerified},
	}
	remaining, err := remainingPlanSpend(plan, entries)
	if err != nil {
		t.Fatal(err)
	}
	if remaining.TAORao != 100 {
		t.Fatalf("unapproved or inexact evidence reduced remaining spend to %d", remaining.TAORao)
	}
	entries = append(entries, JournalEntry{PlanHash: "ancestor", ActionID: "action", IntentHash: "exact", Stage: StageVerified})
	remaining, err = remainingPlanSpend(plan, entries)
	if err != nil {
		t.Fatal(err)
	}
	if remaining.TAORao != 75 {
		t.Fatalf("exact approved ancestor evidence left %d rao", remaining.TAORao)
	}
}

// Remove fields introduced after the requested legacy schema and rebuild the
// exact action and plan hashes used by a persisted ancestor.
func downgradePlanForCompatibilityTest(t *testing.T, plan *SetupPlan, schema string) {
	t.Helper()
	plan.Schema = schema
	plan.MaximumEVMFeePerGasWei = 0
	if schema == "urnetwork-sim-plan-v1" {
		plan.NativeTransactionFeeLimitRao = 0
		plan.BootstrapBurnHalfLifeBlocks = 0
		plan.ProductionBurnHalfLifeBlocks = 0
	}
	plan.PriorPlanHashes = nil
	for index := range plan.Actions {
		if plan.Actions[index].Kind != "evm-transaction" {
			continue
		}
		parameters := cloneStrings(plan.Actions[index].Parameters)
		delete(parameters, evmMaximumGasUnitsParameter)
		delete(parameters, evmMaximumFeePerGasParameter)
		if len(parameters) == 0 {
			parameters = nil
		}
		plan.Actions[index].Parameters = parameters
		intentHash, err := actionIntentHash(plan.Actions[index])
		if err != nil {
			t.Fatal(err)
		}
		plan.Actions[index].IntentHash = intentHash
	}
	var err error
	plan.PlanHash, err = plan.hash()
	if err != nil {
		t.Fatal(err)
	}
}

func TestReadPersistedPlanAcceptsSelfHashedV1OnlyAsRevisionAncestor(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	downgradePlanForCompatibilityTest(t, plan, "urnetwork-sim-plan-v1")
	stateDir := t.TempDir()
	encoded, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(stateDir, "plan.json"), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	ancestor, err := readPersistedPlan(stateDir)
	if err != nil || ancestor.PlanHash != plan.PlanHash {
		t.Fatalf("v1 ancestor = %+v, %v", ancestor, err)
	}
	if _, err := loadPersistedPlan(cfg, stateDir); !errors.Is(err, errPersistedPlanIdentityMismatch) {
		t.Fatalf("v1 ancestor was treated as an active v3 plan: %v", err)
	}
}

func TestReadPersistedPlanAcceptsSelfHashedV2AsRevisionAncestor(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	downgradePlanForCompatibilityTest(t, plan, "urnetwork-sim-plan-v2")
	stateDir := t.TempDir()
	encoded, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(stateDir, "plan.json"), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	ancestor, err := readPersistedPlan(stateDir)
	if err != nil || ancestor.PlanHash != plan.PlanHash {
		t.Fatalf("v2 ancestor = %+v, %v", ancestor, err)
	}
	if _, err := loadPersistedPlan(cfg, stateDir); !errors.Is(err, errPersistedPlanIdentityMismatch) {
		t.Fatalf("v2 ancestor was treated as an active v3 plan: %v", err)
	}
}

func TestJournalMakesVerifiedIntentTerminal(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	j, err := OpenJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	verified := JournalEntry{DeploymentID: "d", PlanHash: "p", ActionID: "action", IntentHash: "intent", Stage: StageVerified, PostconditionHash: "0xpost", PostconditionPath: "receipts/postconditions/action.json"}
	if err := j.Append(verified); err != nil {
		t.Fatal(err)
	}
	failed := verified
	failed.Stage = StageFailed
	failed.PostconditionHash = ""
	failed.PostconditionPath = ""
	if err := j.Append(failed); err == nil {
		t.Fatal("entry after terminal postcondition verification was accepted")
	}
}

func TestJournalRejectsMultipleIntentsForOnePlannedAction(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	j, err := OpenJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	first := JournalEntry{DeploymentID: "d", PlanHash: "p", ActionID: "action", IntentHash: "first", Stage: StageIntent}
	if err := j.Append(first); err != nil {
		t.Fatal(err)
	}
	second := first
	second.IntentHash = "second"
	if err := j.Append(second); err == nil {
		t.Fatal("one plan/action pair accepted a second intent hash")
	}
}

func TestLatestTransactionSurvivesRetryIntentAndRejectsStaleIntent(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	j, err := OpenJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	for _, entry := range []JournalEntry{
		{DeploymentID: "d", PlanHash: "old-plan", ActionID: "deposit", IntentHash: "old", Stage: StageBroadcast, Signer: "s", Nonce: "1", TransactionHash: "0xold", RecoveryBlock: 1, RecoveryBlockHash: "0xcheckpoint"},
		{DeploymentID: "d", PlanHash: "p", ActionID: "deposit", IntentHash: "current", Stage: StageBroadcast, Signer: "s", Nonce: "2", TransactionHash: "0xcurrent", RecoveryBlock: 1, RecoveryBlockHash: "0xcheckpoint"},
		{DeploymentID: "d", PlanHash: "p", ActionID: "deposit", IntentHash: "current", Stage: StageIntent},
	} {
		if err := j.Append(entry); err != nil {
			t.Fatal(err)
		}
	}
	got, ok := j.LatestTransaction("p", "deposit", "current")
	if !ok || got.TransactionHash != "0xcurrent" {
		t.Fatalf("latest transaction = %+v, %t", got, ok)
	}
	if _, ok := j.LatestTransaction("p", "deposit", "missing"); ok {
		t.Fatal("stale transaction matched a different intent")
	}
}

func TestJournalRejectsMixedDeploymentButScopesOneIntentByPlan(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	j, err := OpenJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	base := JournalEntry{DeploymentID: "deployment-a", PlanHash: "plan-a", ActionID: "action", IntentHash: "intent", Stage: StageIntent}
	if err := j.Append(base); err != nil {
		t.Fatal(err)
	}
	mixedDeployment := base
	mixedDeployment.DeploymentID = "deployment-b"
	if err := j.Append(mixedDeployment); err == nil {
		t.Fatal("mixed deployment journal entry was accepted")
	}
	mixedPlan := base
	mixedPlan.PlanHash = "plan-b"
	if err := j.Append(mixedPlan); err != nil {
		t.Fatalf("a revised plan could not carry the same exact intent: %v", err)
	}
	missing := base
	missing.IntentHash = ""
	if err := j.Append(missing); err == nil {
		t.Fatal("incomplete journal identity was accepted")
	}
	broadcast := JournalEntry{DeploymentID: "deployment-a", PlanHash: "plan-a", ActionID: "tx", IntentHash: "intent", Stage: StageBroadcast, Signer: "signer", Nonce: "4", TransactionHash: "0xaaa", RecoveryBlock: 9, RecoveryBlockHash: "0xcheckpoint"}
	if err := j.Append(broadcast); err != nil {
		t.Fatal(err)
	}
	replacement := broadcast
	replacement.TransactionHash = "0xbbb"
	if err := j.Append(replacement); err == nil {
		t.Fatal("one action intent accepted a replacement transaction")
	}
	revised := replacement
	revised.PlanHash = "plan-b"
	if err := j.Append(revised); err != nil {
		t.Fatalf("revised plan transaction was not scoped independently: %v", err)
	}
	if got, ok := j.LatestTransaction("plan-a", "tx", "intent"); !ok || got.TransactionHash != "0xaaa" {
		t.Fatalf("original plan transaction = %+v, %t", got, ok)
	}
	if got, ok := j.LatestTransaction("plan-b", "tx", "intent"); !ok || got.TransactionHash != "0xbbb" {
		t.Fatalf("revised plan transaction = %+v, %t", got, ok)
	}
}

func TestActionDependenciesRequireExactVerifiedPlanIntent(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	j, err := OpenJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	dependency := Action{ID: "first", IntentHash: "intent-a"}
	action := Action{ID: "second", IntentHash: "intent-b", DependsOn: []string{"first"}}
	e := &Executor{plan: &SetupPlan{PlanHash: "plan-a", Actions: []Action{dependency, action}}, journal: j}
	if err := e.verifyActionDependencies(action); err == nil {
		t.Fatal("unverified dependency was accepted")
	}
	wrong := JournalEntry{DeploymentID: "d", PlanHash: "plan-wrong", ActionID: dependency.ID, IntentHash: "wrong-intent", Stage: StageVerified, PostconditionHash: "0xpost", PostconditionPath: "receipts/postconditions/first.json"}
	if err := j.Append(wrong); err != nil {
		t.Fatal(err)
	}
	if err := e.verifyActionDependencies(action); err == nil {
		t.Fatal("dependency verified for a different intent was accepted")
	}
	exact := wrong
	exact.PlanHash = "plan-a"
	exact.IntentHash = dependency.IntentHash
	if err := j.Append(exact); err != nil {
		t.Fatal(err)
	}
	if err := e.verifyActionDependencies(action); err != nil {
		t.Fatalf("exact verified dependency was rejected: %v", err)
	}
	if got, ok := j.LastStage(dependency.ID, dependency.IntentHash, "plan-a"); !ok || got.Stage != StageVerified {
		t.Fatalf("exact last stage lookup failed: %+v, %t", got, ok)
	}
	if _, ok := j.LastStage(dependency.ID, dependency.IntentHash, "plan-b"); ok {
		t.Fatal("last-stage lookup crossed plan hashes")
	}
}

func TestConsumedActionHistoryUsesItsVerifiedAncestorPlan(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	journal, err := OpenJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	action := Action{ID: "evm.fund-owner", IntentHash: "same-intent", Kind: "substrate-extrinsic"}
	finalized := JournalEntry{
		DeploymentID: "deployment", PlanHash: "ancestor", ActionID: action.ID, IntentHash: action.IntentHash,
		Stage: StageFinalized, TransactionHash: "0xancestor-transaction", BlockNumber: 10, BlockHash: "0xancestor-block",
	}
	if err := journal.Append(finalized); err != nil {
		t.Fatal(err)
	}
	verified := JournalEntry{
		DeploymentID: "deployment", PlanHash: "ancestor", ActionID: action.ID, IntentHash: action.IntentHash,
		Stage: StageVerified, PostconditionHash: "0xpost", PostconditionPath: "receipts/postconditions/evm.fund-owner.json",
	}
	if err := journal.Append(verified); err != nil {
		t.Fatal(err)
	}
	executor := &Executor{plan: &SetupPlan{PlanHash: "active", PriorPlanHashes: []string{"ancestor"}}, journal: journal}
	transaction, err := executor.consumedActionTransaction(action, verified)
	if err != nil {
		t.Fatal(err)
	}
	if transaction.PlanHash != "ancestor" || transaction.TransactionHash != finalized.TransactionHash {
		t.Fatalf("ancestor transaction = %+v", transaction)
	}
	wrongPlan := verified
	wrongPlan.PlanHash = "unapproved"
	if _, err := executor.consumedActionTransaction(action, wrongPlan); err == nil {
		t.Fatal("unapproved ancestor evidence was accepted")
	}
}

func TestCarriedActionHistoryFailsBeforeMutationWhenReceiptIsMissing(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	journal, err := OpenJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	action := Action{ID: "subnet.verify-owner", IntentHash: "same-intent", Kind: "substrate-read"}
	verified := JournalEntry{
		DeploymentID: "deployment", PlanHash: "ancestor", ActionID: action.ID, IntentHash: action.IntentHash,
		Stage: StageVerified, PostconditionHash: "0xmissing", PostconditionPath: "receipts/postconditions/subnet.verify-owner.json",
	}
	if err := journal.Append(verified); err != nil {
		t.Fatal(err)
	}
	cfg := testResolvedConfig(t)
	executor := &Executor{
		cfg: cfg, stateDir: dir, journal: journal,
		plan: &SetupPlan{PlanHash: "active", PriorPlanHashes: []string{"ancestor"}, Actions: []Action{action}},
	}
	if err := executor.verifyCarriedActionHistory(context.Background()); err == nil || !strings.Contains(err.Error(), "no such file") {
		t.Fatalf("missing ancestor receipt was not rejected before chain access: %v", err)
	}
}
