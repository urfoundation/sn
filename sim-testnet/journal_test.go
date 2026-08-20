package main

import (
	"encoding/json"
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
	loaded.LiveFacts.BurnRao++
	b, _ = json.Marshal(loaded)
	if err := os.WriteFile(filepath.Join(dir, "plan.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadPersistedPlan(cfg, dir); err == nil {
		t.Fatal("tampered persisted plan was accepted")
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
		{DeploymentID: "d", PlanHash: "p", ActionID: "deposit", IntentHash: "old", Stage: StageBroadcast, Signer: "s", Nonce: "1", TransactionHash: "0xold", RecoveryBlock: 1, RecoveryBlockHash: "0xcheckpoint"},
		{DeploymentID: "d", PlanHash: "p", ActionID: "deposit", IntentHash: "current", Stage: StageBroadcast, Signer: "s", Nonce: "2", TransactionHash: "0xcurrent", RecoveryBlock: 1, RecoveryBlockHash: "0xcheckpoint"},
		{DeploymentID: "d", PlanHash: "p", ActionID: "deposit", IntentHash: "current", Stage: StageIntent},
	} {
		if err := j.Append(entry); err != nil {
			t.Fatal(err)
		}
	}
	got, ok := j.LatestTransaction("deposit", "current")
	if !ok || got.TransactionHash != "0xcurrent" {
		t.Fatalf("latest transaction = %+v, %t", got, ok)
	}
	if _, ok := j.LatestTransaction("deposit", "missing"); ok {
		t.Fatal("stale transaction matched a different intent")
	}
}

func TestJournalRejectsMixedDeploymentAndPlanForOneIntent(t *testing.T) {
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
	if err := j.Append(mixedPlan); err == nil {
		t.Fatal("one intent crossed plan hashes")
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
	wrong := JournalEntry{DeploymentID: "d", PlanHash: "plan-a", ActionID: dependency.ID, IntentHash: "wrong-intent", Stage: StageVerified, PostconditionHash: "0xpost", PostconditionPath: "receipts/postconditions/first.json"}
	if err := j.Append(wrong); err != nil {
		t.Fatal(err)
	}
	if err := e.verifyActionDependencies(action); err == nil {
		t.Fatal("dependency verified for a different intent was accepted")
	}
	exact := wrong
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
