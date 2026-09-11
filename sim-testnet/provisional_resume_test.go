package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func provisionalResumeTestContext(t *testing.T) (*ResolvedConfig, *SetupPlan, string, cliOptions) {
	t.Helper()
	cfg := testResolvedConfig(t)
	cfg.provisionalResume = &provisionalResumeState{Driver: provisionalDriverProvenance{
		ExecutablePath: "/reviewed/new/sim-testnet", ExecutableSHA256: "sha256:" + strings.Repeat("12", 32),
		Build: releaseExecutableBuildIdentity{PackagePath: "github.com/urfoundation/sn/sim-testnet", ModulePath: "github.com/urfoundation/sn/v2026", Revision: strings.Repeat("34", 20), Modified: true},
	}}
	plan := &SetupPlan{PlanHash: "0x" + strings.Repeat("56", 32), ReleaseLockHash: "0x" + strings.Repeat("78", 32), ConfigHash: cfg.ConfigHash, DeploymentID: cfg.Config.Deployment.DeploymentID, ChainID: testnetChainID, GenesisHash: testnetGenesis}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return cfg, plan, dir, cliOptions{Apply: true, ProvisionalResume: true, PlanHash: plan.PlanHash, Name: releaseCandidateCampaignName}
}

func TestProvisionalResumeRequiresExplicitExactApprovalAndCommand(t *testing.T) {
	_, plan, _, options := provisionalResumeTestContext(t)
	for _, command := range []string{"resume", "scenario"} {
		if err := validateProvisionalResumeOptions(command, options); err != nil {
			t.Fatal(err)
		}
		if got := executableAttestationModeForCommand(command, options); got != executableAttestationProvisionalResume {
			t.Fatalf("provisional mode=%d", got)
		}
		if got := executableAttestationModeForCommand(command, cliOptions{Apply: true, PlanHash: plan.PlanHash}); got != executableAttestationLockedSource {
			t.Fatalf("strict default changed to %d", got)
		}
	}
	for _, command := range []string{"setup", "launch", "release-lock", "retire", "stop", "doctor"} {
		if err := validateProvisionalResumeOptions(command, options); err == nil {
			t.Fatalf("provisional flag accepted on %s", command)
		}
	}
	for _, changed := range []cliOptions{{ProvisionalResume: true, PlanHash: plan.PlanHash}, {ProvisionalResume: true, Apply: true}, {ProvisionalResume: true, Apply: true, PlanHash: "approximate"}} {
		if err := validateProvisionalResumeOptions("resume", changed); err == nil {
			t.Fatal("incomplete provisional approval accepted")
		}
	}
	if _, parsed, err := parseCLI([]string{"scenario", "--name", "release-candidate", "--provisional-resume", "--apply", "--plan-hash", plan.PlanHash}); err != nil || !parsed.ProvisionalResume {
		t.Fatalf("provisional CLI flag: %+v %v", parsed, err)
	}
}

func TestProvisionalResumeRecordsActualDriverBeforeJournalAndKeepsInputs(t *testing.T) {
	cfg, plan, dir, options := provisionalResumeTestContext(t)
	before, err := resolvedInputsHash(cfg)
	if err != nil {
		t.Fatal(err)
	}
	used := filepath.Join(dir, "used-plan-input.json")
	if err := os.WriteFile(used, []byte("retained exact bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := prepareProvisionalResume(context.Background(), cfg, dir, "scenario", options, plan); err != nil {
		t.Fatal(err)
	}
	firstPath := cfg.provisionalResume.RecordPath
	encoded, err := os.ReadFile(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	var record provisionalResumeRecord
	if err := json.Unmarshal(encoded, &record); err != nil {
		t.Fatal(err)
	}
	if !record.Provisional || record.FinalAcceptance || record.PlanHash != plan.PlanHash || record.ReleaseLockHash != plan.ReleaseLockHash || record.Driver.ExecutableSHA256 != cfg.provisionalResume.Driver.ExecutableSHA256 || !record.Driver.Build.Modified || record.Driver.Build.Trimpath || record.RetainedSNRepo != cfg.Repos.SN {
		t.Fatal("provenance lost retained approval or actual changed driver identity")
	}
	if info, err := os.Stat(firstPath); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("provenance is not private: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "journal.jsonl")); !os.IsNotExist(err) {
		t.Fatal("provenance preparation opened a journal")
	}
	if err := prepareProvisionalResume(context.Background(), cfg, dir, "scenario", options, plan); err != nil {
		t.Fatal(err)
	}
	if cfg.provisionalResume.RecordPath == firstPath {
		t.Fatal("new invocation overwrote prior provenance")
	}
	if first, err := os.ReadFile(firstPath); err != nil || !bytes.Equal(first, encoded) {
		t.Fatal("prior invocation provenance changed")
	}
	after, err := resolvedInputsHash(cfg)
	if err != nil || before != after {
		t.Fatalf("provisional mode changed resolved plan inputs: %v", err)
	}
	if value, err := os.ReadFile(used); err != nil || string(value) != "retained exact bytes" {
		t.Fatal("retained input was rewritten")
	}
	runtime, err := campaignRPCConfig(cfg)
	if err != nil || !provisionalResumeEnabled(runtime) || runtime.provisionalResume != cfg.provisionalResume {
		t.Fatalf("campaign configuration lost explicit provisional provenance: %v", err)
	}
}

func TestProvisionalResumeRejectsWrongPlanAndMainnetBeforeProvenance(t *testing.T) {
	for _, change := range []func(*ResolvedConfig, *SetupPlan, *cliOptions){
		func(_ *ResolvedConfig, _ *SetupPlan, o *cliOptions) { o.PlanHash = "0x" + strings.Repeat("99", 32) },
		func(cfg *ResolvedConfig, _ *SetupPlan, _ *cliOptions) { cfg.ChainID = 1 },
		func(_ *ResolvedConfig, plan *SetupPlan, _ *cliOptions) {
			plan.GenesisHash = "0x" + strings.Repeat("99", 32)
		},
		func(cfg *ResolvedConfig, _ *SetupPlan, _ *cliOptions) { cfg.provisionalResume = nil },
	} {
		cfg, plan, dir, options := provisionalResumeTestContext(t)
		change(cfg, plan, &options)
		if err := prepareProvisionalResume(context.Background(), cfg, dir, "resume", options, plan); err == nil {
			t.Fatal("invalid provisional identity accepted")
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 0 {
			t.Fatalf("invalid approval wrote state: %v", err)
		}
	}
}

func provisionalVerifiedExecutor(t *testing.T, actionID string, ancestor bool) (*Executor, Action, JournalEntry) {
	t.Helper()
	cfg, plan, dir, options := provisionalResumeTestContext(t)
	if err := prepareProvisionalResume(context.Background(), cfg, dir, "resume", options, plan); err != nil {
		t.Fatal(err)
	}
	journal, err := OpenJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { journal.Close() })
	action := Action{ID: actionID, IntentHash: "approved-original-intent", Spend: Spend{TAORao: 10, AlphaRao: 20}}
	plan.Actions = []Action{action}
	receiptPlan := plan.PlanHash
	if ancestor {
		receiptPlan = "0x" + strings.Repeat("90", 32)
		plan.PriorPlanHashes = []string{receiptPlan}
	}
	e := &Executor{cfg: cfg, stateDir: dir, plan: plan, journal: journal}
	head := ChainHead{Number: 10, Hash: "0x" + strings.Repeat("ab", 32)}
	record := &ActionPostcondition{Schema: "urnetwork-sim-action-postcondition-v4", DeploymentID: cfg.Config.Deployment.DeploymentID, PlanHash: receiptPlan, ActionID: action.ID, IntentHash: action.IntentHash,
		OperationalRPCMode: cfg.OperationalRPCMode, IndependentRPC: independentRPCRequired(cfg), SubstrateFinalized: head, EVMFinalized: head, EVMHashDomain: "evm-rpc", Observed: map[string]any{"verified": true},
		IndependentSubstrateFinalized: head, IndependentEVMFinalized: head, IndependentEVMHashDomain: "evm-rpc", IndependentObserved: map[string]any{"verified": true}}
	path, hash, err := e.persistActionPostcondition(record)
	if err != nil {
		t.Fatal(err)
	}
	entry := JournalEntry{DeploymentID: cfg.Config.Deployment.DeploymentID, PlanHash: receiptPlan, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageVerified, PostconditionPath: path, PostconditionHash: hash}
	if err := journal.Append(entry); err != nil {
		t.Fatal(err)
	}
	return e, action, entry
}

func TestProvisionalResumeReusesCurrentAndAncestorVerifiedWithoutDispatch(t *testing.T) {
	for _, ancestor := range []bool{false, true} {
		e, action, _ := provisionalVerifiedExecutor(t, "evidence.activate.1", ancestor)
		before := len(e.journal.Entries())
		// No chain clients exist: either RPC replay or dispatch would fail.
		if err := e.verifyCarriedActionHistory(context.Background()); err != nil {
			t.Fatal(err)
		}
		copy := *e
		if err := copy.Execute(context.Background(), action); err != nil {
			t.Fatalf("verified receipt was not reused: %v", err)
		}
		if len(e.journal.Entries()) != before {
			t.Fatal("reuse appended a new transaction intent or receipt")
		}
	}
}

func TestProvisionalResumeTamperedReceiptFailsWithoutDispatch(t *testing.T) {
	e, action, entry := provisionalVerifiedExecutor(t, "evidence.activate.1", false)
	if err := os.WriteFile(filepath.Join(e.stateDir, entry.PostconditionPath), []byte(`{"tampered":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	before := len(e.journal.Entries())
	if err := e.verifyCarriedActionHistory(context.Background()); err == nil {
		t.Fatal("tampered receipt passed provisional preflight")
	}
	if err := e.Execute(context.Background(), action); err == nil {
		t.Fatal("tampered verified receipt was reused")
	}
	if len(e.journal.Entries()) != before {
		t.Fatal("tampered verified receipt fell through to a new intent")
	}
}

func TestProvisionalResumeKeepsDependenciesTopologyAndPendingStages(t *testing.T) {
	e, action, entry := provisionalVerifiedExecutor(t, "evidence.activate.1", false)
	action.DependsOn = []string{"missing-approved-dependency"}
	if err := e.Execute(context.Background(), action); err == nil || !strings.Contains(err.Error(), "dependencies") {
		t.Fatalf("verified reuse bypassed dependencies: %v", err)
	}
	for _, stage := range []JournalStage{StageIntent, StageBroadcast, StageIncluded, StageFinalized, StageFailed} {
		changed := entry
		changed.Stage = stage
		if err := e.authenticateProvisionalReceipt(action, changed); err == nil {
			t.Fatalf("provisional reuse promoted %s", stage)
		}
	}
	topology, launch, _ := provisionalVerifiedExecutor(t, "topology.launch", false)
	before := len(topology.journal.Entries())
	if err := topology.Execute(context.Background(), launch); err == nil {
		t.Fatal("missing live topology was accepted from a local receipt")
	}
	if len(topology.journal.Entries()) != before {
		t.Fatal("failed readiness dispatched a verified topology action")
	}
}

func TestProvisionalResumeHonestBuildAndNoFinalAcceptance(t *testing.T) {
	info := testReleaseExecutableBuildInfo(strings.Repeat("cd", 20))
	info.Settings[2].Value = "true"
	info.Settings[3].Value = "false"
	build, err := parseExecutableBuildInfo(info, false)
	if err != nil || !build.Modified || build.Trimpath {
		t.Fatalf("provisional build metadata was not honest: %+v %v", build, err)
	}
	if _, err := parseReleaseExecutableBuildInfo(info); err == nil {
		t.Fatal("strict attestation accepted the provisional dirty build")
	}
	cfg, plan, dir, options := provisionalResumeTestContext(t)
	if err := prepareProvisionalResume(context.Background(), cfg, dir, "scenario", options, plan); err != nil {
		t.Fatal(err)
	}
	result := &ScenarioResult{Result: "pass"}
	applyProvisionalScenarioProvenance(cfg, result)
	if !result.Provisional || result.FinalAcceptance == nil || *result.FinalAcceptance || result.Result != "pass" || result.ProvisionalDriver == nil {
		t.Fatal("provisional result lost operational status or claimed final acceptance")
	}
	if err := validateScenarioFinalSemanticSource(nil, nil, result, nil); err == nil || !strings.Contains(err.Error(), "provisional") {
		t.Fatalf("provisional result qualified for release: %v", err)
	}
	if _, _, err := finalSemanticSupplementRoots(context.Background(), nil, nil, "", "", result); err == nil || !strings.Contains(err.Error(), "provisional") {
		t.Fatalf("provisional result entered final supplement publication: %v", err)
	}
	if err := runFinalSemanticCampaignAnalyzer(context.Background(), cfg, dir, "", nil, result); err != nil {
		t.Fatalf("provisional qualification skip canceled the live campaign: %v", err)
	}
	if err := validateScenarioCampaignResult(nil, result, "release-1.0"); err == nil || !strings.Contains(err.Error(), "provisional") {
		t.Fatalf("strict campaign adopted provisional completion: %v", err)
	}
	if err := runFinalSemanticCampaignAnalyzer(context.Background(), nil, dir, "", nil, result); err == nil || !strings.Contains(err.Error(), "provisional") {
		t.Fatalf("strict analyzer silently accepted provisional completion: %v", err)
	}
	strict := &ScenarioResult{Result: "pass"}
	applyProvisionalScenarioProvenance(nil, strict)
	encoded, err := json.Marshal(strict)
	if err != nil || strings.Contains(string(encoded), "provisional") || strings.Contains(string(encoded), "final_acceptance") {
		t.Fatalf("strict result wire format changed: %s %v", encoded, err)
	}
}
