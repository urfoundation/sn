package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func precompileRecoveryHistoryTestFixture(t *testing.T) (*precompileRecoveryTestFixture, *ResolvedConfig, *SetupPlan) {
	t.Helper()
	f := precompileRecoveryTestFixtureWithBase(t, false, newArchivedPrecompileProbeSuccessorFixture(t), func(f *precompileRecoveryTestFixture) {
		old := f.plan.PlanHash
		var err error
		f.plan.MaximumSpend, err = maximumActionSpend(f.plan.Actions)
		if err != nil {
			t.Fatal(err)
		}
		f.plan.PlanHash, err = f.plan.hash()
		if err != nil {
			t.Fatal(err)
		}
		for index := range f.entries {
			if f.entries[index].PlanHash == old {
				f.entries[index].PlanHash = f.plan.PlanHash
			}
		}
	})
	persistFleetCommitmentRecoveryTestPlan(t, f.stateDir, f.plan)
	if _, err := readValidatorEvidenceHistoricalPlan(f.stateDir, f.plan.PlanHash); err != nil {
		t.Fatal(err)
	}
	if err := writeCoordinatorRepairFile(filepath.Join(f.stateDir, precompileRecoveryCompletionFilename), f.completion); err != nil {
		t.Fatal(err)
	}
	cfg := *f.cfg
	cfg.Policy = rateAmendmentTestPolicy(t, f.cfg.Policy)
	var err error
	cfg.PolicyHash, err = cfg.Policy.HashHex()
	if err != nil {
		t.Fatal(err)
	}
	current := clonePrecompileProbeSuccessorPlan(t, f.plan)
	current.PriorPlanHashes = append(current.PriorPlanHashes, f.plan.PlanHash)
	current.PolicyHash = cfg.PolicyHash
	current.PolicyRateAmendment = &PolicyRateAmendment{Schema: policyRateAmendmentSchema, PriorPlanHash: f.plan.PlanHash, Previous: *f.cfg.Policy, Next: *cfg.Policy}
	current.PlanHash, err = current.hash()
	if err != nil {
		t.Fatal(err)
	}
	return f, &cfg, current
}

func TestPrecompileRecoveryHistoryReplaysExactSourceAcrossRateSuccessor(t *testing.T) {
	f, cfg, current := precompileRecoveryHistoryTestFixture(t)
	before, _ := json.Marshal(f.evidence)
	source, err := readPrecompileRecoveryHistoryPlan(cfg, f.stateDir, current, f.entries, f.evidence)
	if err != nil || source.PlanHash != f.plan.PlanHash {
		t.Fatalf("source recovery replay: %v", err)
	}
	nonce := f.plan.PrecompileProbeSuccessor.DeployerNonce + uint64(len(f.entries)-len(f.base.entries))
	if err := verifyPrecompileProbeSuccessorCalls(t.Context(), cfg, f.stateDir, current, f.entries, f.reader, f.reader.finalized, nonce); err != nil {
		t.Fatal(err)
	}
	owner := &Executor{cfg: cfg, stateDir: f.stateDir, plan: current, journal: &Journal{entries: f.entries}}
	observer := &liveScenarioProbe{cfg: cfg, stateDir: f.stateDir, precompilePlan: current, precompileJournal: owner.journal}
	if err := owner.validatePrecompileEvidence(approvedPrecompileProbe(current), f.evidence); err != nil {
		t.Fatal(err)
	}
	if err := observer.validatePrecompileEvidence(approvedPrecompileProbe(current), f.evidence); err != nil {
		t.Fatal(err)
	}
	if err := owner.loadPrecompileRecoveryAuthorization(f.evidence); err == nil {
		t.Fatal("historical replay authorized new recovery spending")
	}
	after, _ := json.Marshal(f.evidence)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("history replay relabelled the original evidence")
	}
	// Independent chain replay still authenticates the signed calldata.
	step := f.evidence.Recovery.Steps[0]
	delete(f.reader.transactions, common.HexToHash(step.Move.TransactionHash))
	if err := verifyPrecompileProbeSuccessorCalls(t.Context(), cfg, f.stateDir, current, f.entries, f.reader, f.reader.finalized, nonce); err == nil {
		t.Fatal("history replay accepted a missing signed chain transaction")
	}
}

func TestPrecompileRecoveryHistoryRejectsChangedSourceCustodyAndCompletion(t *testing.T) {
	f, cfg, current := precompileRecoveryHistoryTestFixture(t)
	for _, mutation := range []string{"unapproved source", "foreign policy", "custody", "probe", "runtime", "input", "incomplete", "journal"} {
		t.Run(mutation, func(t *testing.T) {
			changed := clonePrecompileProbeSuccessorPlan(t, current)
			evidence := *f.evidence
			entries := append([]JournalEntry(nil), f.entries...)
			switch mutation {
			case "unapproved source":
				changed.PriorPlanHashes = nil
			case "foreign policy":
				changed.PolicyRateAmendment = nil
			case "custody":
				changed.Roles.Deployer = common.Address{99}.Hex()
			case "probe":
				changed.PrecompileProbeSuccessor.Probe = common.Address{99}.Hex()
			case "runtime":
				changed.PrecompileProbeSuccessor.RuntimeHash = common.Hash{99}.Hex()
			case "input":
				changed.LiveFacts.ProbeTAORao++
			case "incomplete":
				evidence.Complete = false
			case "journal":
				entries = entries[:len(entries)-1]
			}
			if _, err := readPrecompileRecoveryHistoryPlan(cfg, f.stateDir, changed, entries, &evidence); err == nil {
				t.Fatal("changed completed recovery authority accepted")
			}
		})
	}
	completionPath := filepath.Join(f.stateDir, precompileRecoveryCompletionFilename)
	if err := os.WriteFile(completionPath, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readPrecompileRecoveryHistoryPlan(cfg, f.stateDir, current, f.entries, f.evidence); err == nil {
		t.Fatal("changed signed completion accepted")
	}
}
