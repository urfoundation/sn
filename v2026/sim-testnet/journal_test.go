package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
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
	loaded.LiveFacts.AlphaAvailableRao += 10
	loaded.LiveFacts.AlphaSourceStoredLockRao += 3
	loaded.LiveFacts.AlphaSourceCollateralRao += 5
	loaded.LiveFacts.WalletNetuidAlphaRao += 20
	loaded.LiveFacts.WalletNetuidCollateralRao += 7
	loaded.LiveFacts.AlphaTransferableRao += 5
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

func TestTopologyLaunchCannotUseAncestorProcessGeneration(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	journal, err := OpenJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()

	topology := Action{ID: "topology.launch", IntentHash: "topology-intent", Kind: "local"}
	durable := Action{ID: "config.render", IntentHash: "config-intent", Kind: "local"}
	after := Action{ID: "churn.tournament-complete", IntentHash: "churn-intent", DependsOn: []string{topology.ID}}
	ancestorPlanHash := "0x" + strings.Repeat("11", 32)
	activePlanHash := "0x" + strings.Repeat("22", 32)
	for _, action := range []Action{topology, durable} {
		if err := journal.Append(JournalEntry{
			DeploymentID: "deployment", PlanHash: ancestorPlanHash, ActionID: action.ID, IntentHash: action.IntentHash,
			Stage: StageVerified, PostconditionHash: "0xpost", PostconditionPath: "receipts/postconditions/" + action.ID + ".json",
		}); err != nil {
			t.Fatal(err)
		}
	}
	executor := &Executor{
		plan: &SetupPlan{
			PlanHash: activePlanHash, PriorPlanHashes: []string{ancestorPlanHash},
			Actions: []Action{durable, topology, after},
		},
		journal: journal,
	}
	if entry, ok := executor.verifiedActionEntry(durable); !ok || entry.PlanHash != ancestorPlanHash {
		t.Fatalf("durable ancestor verification was not retained: %+v, %t", entry, ok)
	}
	if entry, ok := executor.verifiedActionEntry(topology); ok {
		t.Fatalf("ancestor topology process generation was retained: %+v", entry)
	}
	if err := executor.verifyActionDependencies(after); err == nil || !strings.Contains(err.Error(), "topology.launch is not postcondition-verified") {
		t.Fatalf("post-topology action accepted an ancestor process generation: %v", err)
	}
	if err := journal.Append(JournalEntry{
		DeploymentID: "deployment", PlanHash: activePlanHash, ActionID: topology.ID, IntentHash: topology.IntentHash,
		Stage: StageVerified, PostconditionHash: "0xpost-current", PostconditionPath: "receipts/postconditions/" + strings.Repeat("22", 32) + "/topology.launch.json",
	}); err != nil {
		t.Fatal(err)
	}
	if entry, ok := executor.verifiedActionEntry(topology); !ok || entry.PlanHash != activePlanHash {
		t.Fatalf("current topology process generation was not accepted: %+v, %t", entry, ok)
	}
	if err := executor.verifyActionDependencies(after); err != nil {
		t.Fatalf("post-topology action rejected the current process generation: %v", err)
	}
}

func TestCarriedTopologyAuthenticatesReceiptWithoutRequiringStoppedProcessLiveness(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	journal, err := OpenJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()

	cfg := testResolvedConfig(t)
	topology := Action{ID: "topology.launch", IntentHash: "topology-intent", Kind: "local"}
	ancestorPlanHash := "0x" + strings.Repeat("11", 32)
	activePlanHash := "0x" + strings.Repeat("22", 32)
	ancestorPlan := &SetupPlan{PlanHash: ancestorPlanHash, Actions: []Action{topology}}
	record := &ActionPostcondition{
		Schema: "urnetwork-sim-action-postcondition-v4", DeploymentID: cfg.Config.Deployment.DeploymentID,
		PlanHash: ancestorPlanHash, ActionID: topology.ID, IntentHash: topology.IntentHash,
		OperationalRPCMode: cfg.OperationalRPCMode, IndependentRPC: independentRPCRequired(cfg),
		SubstrateFinalized: ChainHead{Number: 10, Hash: "0x" + strings.Repeat("31", 32)},
		EVMFinalized:       ChainHead{Number: 11, Hash: "0x" + strings.Repeat("32", 32)}, EVMHashDomain: "evm-rpc",
		Observed:                      map[string]any{"kind": "local", "processes": 33},
		IndependentSubstrateFinalized: ChainHead{Number: 12, Hash: "0x" + strings.Repeat("33", 32)},
		IndependentEVMFinalized:       ChainHead{Number: 13, Hash: "0x" + strings.Repeat("34", 32)}, IndependentEVMHashDomain: "evm-rpc",
		IndependentObserved: map[string]any{"kind": "local", "processes": 33},
	}
	receiptPath, receiptHash, err := (&Executor{cfg: cfg, stateDir: dir, plan: ancestorPlan}).persistActionPostcondition(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.Append(JournalEntry{
		DeploymentID: cfg.Config.Deployment.DeploymentID, PlanHash: ancestorPlanHash,
		ActionID: topology.ID, IntentHash: topology.IntentHash, Stage: StageVerified,
		PostconditionHash: receiptHash, PostconditionPath: receiptPath,
	}); err != nil {
		t.Fatal(err)
	}
	executor := &Executor{
		cfg: cfg, stateDir: dir, journal: journal,
		plan: &SetupPlan{PlanHash: activePlanHash, PriorPlanHashes: []string{ancestorPlanHash}, Actions: []Action{topology}},
	}
	if err := executor.verifyCarriedActionHistory(context.Background()); err != nil {
		t.Fatalf("stopped ancestor topology receipt was not authenticated independently of liveness: %v", err)
	}
	if len(executor.carriedVerificationKeys) != 0 {
		t.Fatal("ancestor topology liveness entered the carried verification cache")
	}
	if err := os.Remove(filepath.Join(dir, filepath.FromSlash(receiptPath))); err != nil {
		t.Fatal(err)
	}
	if err := executor.verifyCarriedActionHistory(context.Background()); err == nil || !strings.Contains(err.Error(), "persisted ancestor process receipt") {
		t.Fatalf("missing ancestor topology receipt was accepted: %v", err)
	}
}

func TestSamePlanTopologyResumeRequiresCurrentSupervisorGeneration(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	journal, err := OpenJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()

	cfg := testResolvedConfig(t)
	topology := Action{ID: "topology.launch", IntentHash: "topology-intent", Kind: "local"}
	planHash := "0x" + strings.Repeat("44", 32)
	plan := &SetupPlan{PlanHash: planHash, Actions: []Action{topology}}
	record := &ActionPostcondition{
		Schema: "urnetwork-sim-action-postcondition-v4", DeploymentID: cfg.Config.Deployment.DeploymentID,
		PlanHash: planHash, ActionID: topology.ID, IntentHash: topology.IntentHash,
		OperationalRPCMode: cfg.OperationalRPCMode, IndependentRPC: independentRPCRequired(cfg),
		SubstrateFinalized: ChainHead{Number: 10, Hash: "0x" + strings.Repeat("51", 32)},
		EVMFinalized:       ChainHead{Number: 11, Hash: "0x" + strings.Repeat("52", 32)}, EVMHashDomain: "evm-rpc",
		Observed:                      map[string]any{"kind": "local", "processes": 0},
		IndependentSubstrateFinalized: ChainHead{Number: 12, Hash: "0x" + strings.Repeat("53", 32)},
		IndependentEVMFinalized:       ChainHead{Number: 13, Hash: "0x" + strings.Repeat("54", 32)}, IndependentEVMHashDomain: "evm-rpc",
		IndependentObserved: map[string]any{"kind": "local", "processes": 0},
	}
	executor := &Executor{cfg: cfg, stateDir: dir, plan: plan, journal: journal}
	receiptPath, receiptHash, err := executor.persistActionPostcondition(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.Append(JournalEntry{
		DeploymentID: cfg.Config.Deployment.DeploymentID, PlanHash: planHash,
		ActionID: topology.ID, IntentHash: topology.IntentHash, Stage: StageVerified,
		PostconditionHash: receiptHash, PostconditionPath: receiptPath,
	}); err != nil {
		t.Fatal(err)
	}
	manifest := SupervisorFile{Schema: "urnetwork-sim-supervisor-v1", DeploymentID: cfg.Config.Deployment.DeploymentID, BinaryHash: "sha256:" + strings.Repeat("61", 32)}
	manifestHash, err := canonicalHashHex(manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifestWire, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(dir, "supervisor.json"), manifestWire, 0o600); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(dir, "supervisor.state.json")
	stoppedWire, err := json.Marshal(SupervisorState{
		Schema: "urnetwork-sim-supervisor-state-v1", ManifestHash: manifestHash,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(statePath, stoppedWire, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := executor.Execute(context.Background(), topology); err == nil || !strings.Contains(err.Error(), "supervisor postcondition ready=false") {
		t.Fatalf("same-plan stopped topology receipt bypassed current liveness: %v", err)
	}
	if got := len(journal.Entries()); got != 1 {
		t.Fatalf("failed same-plan liveness recheck changed terminal journal: entries=%d", got)
	}
	liveWire, err := json.Marshal(SupervisorState{
		Schema: "urnetwork-sim-supervisor-state-v1", ManifestHash: manifestHash,
		SupervisorPID: os.Getpid(), SupervisorStartTimeTicks: currentProcessStartTimeTicks(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(statePath, liveWire, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := executor.Execute(context.Background(), topology); err != nil {
		t.Fatalf("same-plan live supervisor generation was rejected: %v", err)
	}
	if got := len(journal.Entries()); got != 1 {
		t.Fatalf("same-plan liveness recheck duplicated terminal journal entry: entries=%d", got)
	}
	tamperedWire, err := json.Marshal(SupervisorState{
		Schema: "urnetwork-sim-supervisor-state-v1", ManifestHash: "0x" + strings.Repeat("ff", 32),
		SupervisorPID: os.Getpid(), SupervisorStartTimeTicks: currentProcessStartTimeTicks(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(statePath, tamperedWire, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := executor.Execute(context.Background(), topology); err == nil || !strings.Contains(err.Error(), "supervisor postcondition ready=false") {
		t.Fatalf("same-plan wrong supervisor manifest bypassed current liveness: %v", err)
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
	action := Action{
		ID: "evm.fund-owner", IntentHash: "current-intent", Kind: "substrate-extrinsic",
		AcceptedPriorIntentHashes: []string{"ancestor-intent"},
	}
	finalized := JournalEntry{
		DeploymentID: "deployment", PlanHash: "ancestor", ActionID: action.ID, IntentHash: "ancestor-intent",
		Stage: StageFinalized, TransactionHash: "0xancestor-transaction", BlockNumber: 10, BlockHash: "0xancestor-block",
	}
	if err := journal.Append(finalized); err != nil {
		t.Fatal(err)
	}
	verified := JournalEntry{
		DeploymentID: "deployment", PlanHash: "ancestor", ActionID: action.ID, IntentHash: "ancestor-intent",
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

// Stops at the first failed batch so an invalid release cannot continue
// consuming public archive quota on work that cannot affect the outcome.
func TestOrderedConcurrentAuditsStopDispatchAfterFailure(t *testing.T) {
	called := make([]bool, 10)
	err := runOrderedConcurrentAudits(len(called), 1, func(index int) error {
		called[index] = true
		if index == 3 {
			return errors.New("stop")
		}
		return nil
	})
	if err == nil || err.Error() != "stop" {
		t.Fatalf("ordered audit failure=%v", err)
	}
	for index, wasCalled := range called {
		wantCalled := index <= 3
		if wasCalled != wantCalled {
			t.Fatalf("audit %d called=%t, want %t", index, wasCalled, wantCalled)
		}
	}
}

// Preserves the bounded-batch guarantee with real concurrency: all work in
// the failing batch may finish, but no index in the next batch can start.
func TestOrderedConcurrentAuditsStopBeforeNextConcurrentBatch(t *testing.T) {
	const workers = 3
	called := make([]atomic.Bool, 9)
	started := make(chan int, workers)
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- runOrderedConcurrentAudits(len(called), workers, func(index int) error {
			called[index].Store(true)
			if index < workers {
				started <- index
				<-release
				if index == 1 {
					return errors.New("stop concurrent batch")
				}
			}
			return nil
		})
	}()
	for index := 0; index < workers; index++ {
		startedIndex := <-started
		if startedIndex >= workers {
			t.Fatalf("later audit %d started before the first batch completed", startedIndex)
		}
	}
	close(release)
	if err := <-done; err == nil || err.Error() != "stop concurrent batch" {
		t.Fatalf("ordered concurrent audit failure=%v", err)
	}
	for index := workers; index < len(called); index++ {
		if called[index].Load() {
			t.Fatalf("later-batch audit %d ran after a prior batch failed", index)
		}
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

func TestOrderedConcurrentAuditsAreBoundedAndReportPlanOrder(t *testing.T) {
	const workers = 4
	started := make(chan int, workers)
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- runOrderedConcurrentAudits(workers, workers, func(index int) error {
			started <- index
			<-release
			return nil
		})
	}()
	for index := 0; index < workers; index++ {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("carried audits did not make bounded concurrent progress")
		}
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	err := runOrderedConcurrentAudits(6, 3, func(index int) error {
		switch index {
		case 1:
			return errors.New("first-in-plan")
		case 4:
			return errors.New("later-in-plan")
		default:
			return nil
		}
	})
	if err == nil || err.Error() != "first-in-plan" {
		t.Fatalf("concurrent audit returned a nondeterministic error: %v", err)
	}
}

func TestCarriedActionAuditCacheAvoidsSecondLiveVerificationPass(t *testing.T) {
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
		Stage: StageVerified, PostconditionHash: "0xpost", PostconditionPath: "receipts/postconditions/subnet.verify-owner.json",
	}
	if err := journal.Append(verified); err != nil {
		t.Fatal(err)
	}
	plan := &SetupPlan{PlanHash: "active", PriorPlanHashes: []string{"ancestor"}, Actions: []Action{action}}
	uncached := &Executor{cfg: testResolvedConfig(t), stateDir: dir, plan: plan, journal: journal}
	if err := uncached.Execute(context.Background(), action); err == nil || !strings.Contains(err.Error(), "no such file") {
		t.Fatalf("pre-fix duplicate verification failure was not reproduced: %v", err)
	}
	cached := &Executor{
		cfg: testResolvedConfig(t), stateDir: dir, plan: plan, journal: journal,
		carriedVerificationKeys: map[string]bool{carriedVerificationKey(verified): true},
	}
	if err := cached.Execute(context.Background(), action); err != nil {
		t.Fatalf("already audited ancestor was verified twice: %v", err)
	}
}

func TestCarriedActionAuditCacheCannotBypassCurrentOrDifferentEvidence(t *testing.T) {
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
		DeploymentID: "deployment", PlanHash: "active", ActionID: action.ID, IntentHash: action.IntentHash,
		Stage: StageVerified, PostconditionHash: "0xpost", PostconditionPath: "receipts/postconditions/subnet.verify-owner.json",
	}
	if err := journal.Append(verified); err != nil {
		t.Fatal(err)
	}
	executor := &Executor{
		cfg: testResolvedConfig(t), stateDir: dir,
		plan: &SetupPlan{PlanHash: "active", PriorPlanHashes: []string{"ancestor"}, Actions: []Action{action}}, journal: journal,
		carriedVerificationKeys: map[string]bool{carriedVerificationKey(verified): true},
	}
	if err := executor.Execute(context.Background(), action); err == nil || !strings.Contains(err.Error(), "no such file") {
		t.Fatalf("current-plan evidence bypassed resume verification: %v", err)
	}
}
