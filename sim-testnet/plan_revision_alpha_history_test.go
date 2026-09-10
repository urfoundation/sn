// Exercise historical alpha retirement through the real revision builder with
// synthetic approvals; the original plans and journal entries stay immutable.
package main

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Holds a verified repair which an older revision omitted from both its active
// actions and its superseded spend, while retaining the approved source hash.
type retiredAlphaHistoryFixture struct {
	cfg           *ResolvedConfig
	stateDir      string
	ancestor      *SetupPlan
	prior         *SetupPlan
	omittedRepair Action
	entries       []JournalEntry
}

// Add a valid, visibly synthetic destination-floor repair to an approval.
func appendAlphaHistoryTestRepair(t *testing.T, plan *SetupPlan, baseId string) Action {
	t.Helper()
	base := actionByID(t, plan, baseId)
	minimum, err := minimumAlphaTransferRao(plan.LiveFacts.DefaultMinTransferRao, plan.LiveFacts.AlphaPriceQ9, plan.AlphaTransferMarginBPS)
	if err != nil {
		t.Fatal(err)
	}
	exact := minimum + 37
	parameters := alphaTransferActionParameters(exact, 0, minimum, &plan.LiveFacts, plan.AlphaTransferMarginBPS)
	parameters[alphaRepairForActionParameter] = base.ID
	parameters[alphaRepairMinimumDestinationParameter] = strconv.FormatUint(base.Spend.AlphaRao+minimum, 10)
	if strings.HasPrefix(base.ID, "alpha.transfer.operator-deposit.") {
		parameters["campaign_policy_hash"] = plan.PolicyHash
		parameters[deploymentManifestHashParameter] = base.Parameters[deploymentManifestHashParameter]
	}
	repair := Action{
		ID:   strings.Replace(base.ID, "alpha.transfer.", "alpha.repair.", 1),
		Kind: "substrate-extrinsic", Target: base.Target,
		Description: "synthetic historical destination repair",
		Parameters:  parameters, Spend: Spend{AlphaRao: exact}, DependsOn: []string{base.ID},
	}
	repair.IntentHash, err = actionIntentHash(repair)
	if err != nil {
		t.Fatal(err)
	}
	plan.Actions = append(plan.Actions, repair)
	plan.MaximumSpend, err = maximumActionSpend(plan.Actions)
	if err != nil {
		t.Fatal(err)
	}
	plan.PlanHash, err = plan.hash()
	if err != nil {
		t.Fatal(err)
	}
	if err := validatePlanBudget(plan); err != nil {
		t.Fatalf("synthetic repair approval is invalid: %v", err)
	}
	return repair
}

// Persist only hash-authenticated synthetic source plans, never live evidence.
func newRetiredAlphaHistoryFixture(t *testing.T) retiredAlphaHistoryFixture {
	t.Helper()
	cfg := testResolvedConfig(t)
	roles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ancestor, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	repair := appendAlphaHistoryTestRepair(t, ancestor, "alpha.transfer.operator-deposit.2")
	prior, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	prior.PriorPlanHashes = []string{ancestor.PlanHash}
	prior.PlanHash, err = prior.hash()
	if err != nil {
		t.Fatal(err)
	}
	stateDir := t.TempDir()
	persistFleetCommitmentRecoveryTestPlan(t, stateDir, ancestor)
	persistFleetCommitmentRecoveryTestPlan(t, stateDir, prior)
	entries := []JournalEntry{}
	for _, action := range ancestor.Actions {
		if action.Spend.AlphaRao != 0 {
			entries = append(entries, JournalEntry{
				DeploymentID: ancestor.DeploymentID, PlanHash: ancestor.PlanHash,
				ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageVerified,
			})
		}
	}
	return retiredAlphaHistoryFixture{cfg: cfg, stateDir: stateDir, ancestor: ancestor, prior: prior, omittedRepair: repair, entries: entries}
}

// The lost historical ceiling must reduce the new fixed tranche before sizing;
// adding it only after sizing would reject an otherwise affordable approval.
func TestPlanRevisionRestoresOmittedRetiredAlphaBeforeReserveSizing(t *testing.T) {
	f := newRetiredAlphaHistoryFixture(t)
	f.cfg.MaximumAlphaRao = f.prior.MaximumSpend.AlphaRao + f.cfg.Config.ValidatorBootstrap.MaximumReserveRepairAlphaRao
	current := *testSetupFacts()
	current.RegisteredAlphaRao = 30_000_000_000_000
	base := actionByID(t, f.prior, "alpha.transfer.validator.1")
	var err error
	current.ReserveValidatorAlphaRao, err = strconv.ParseUint(base.Parameters["planned_final_stake_rao"], 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	before, err := json.Marshal(f.prior)
	if err != nil {
		t.Fatal(err)
	}
	revised, err := buildPlanRevisionFromFacts(f.cfg, f.stateDir, f.prior, &current, f.entries, time.Unix(3, 0))
	if err != nil {
		t.Fatal(err)
	}
	if revised.SupersededSpend.AlphaRao != f.omittedRepair.Spend.AlphaRao {
		t.Fatalf("retired alpha repair was omitted from cumulative approval: got=%d want=%d", revised.SupersededSpend.AlphaRao, f.omittedRepair.Spend.AlphaRao)
	}
	repair := actionByID(t, revised, "alpha.repair.validator.1.2")
	want := f.cfg.Config.ValidatorBootstrap.MaximumReserveRepairAlphaRao - f.omittedRepair.Spend.AlphaRao
	if repair.Spend.AlphaRao != want || repair.Parameters[alphaRepairCumulativeBeforeParameter] != strconv.FormatUint(f.prior.MaximumSpend.AlphaRao+f.omittedRepair.Spend.AlphaRao, 10) {
		t.Fatalf("restored historical spend did not bound the new reserve tranche: got=%d want=%d", repair.Spend.AlphaRao, want)
	}
	for _, action := range revised.Actions {
		if action.ID == f.omittedRepair.ID {
			t.Fatal("retired repair was replayed instead of charged to historical spend")
		}
	}
	after, err := json.Marshal(f.prior)
	if err != nil || string(after) != string(before) {
		t.Fatalf("retirement rewrote the prior approval: %v", err)
	}
	persistFleetCommitmentRecoveryTestPlan(t, f.stateDir, revised)
	again, err := buildPlanRevisionFromFacts(f.cfg, f.stateDir, revised, &current, f.entries, time.Unix(4, 0))
	if err != nil {
		t.Fatal(err)
	}
	if again.SupersededSpend.AlphaRao != revised.SupersededSpend.AlphaRao || again.MaximumSpend.AlphaRao != revised.MaximumSpend.AlphaRao || actionByID(t, again, repair.ID).IntentHash != repair.IntentHash {
		t.Fatal("repeated revision duplicated the historical repair or changed its pending replacement tranche")
	}
	stored, err := readValidatorEvidenceHistoricalPlan(f.stateDir, f.prior.PlanHash)
	if err != nil || stored.SupersededSpend.AlphaRao != 0 {
		t.Fatalf("archived omitted-spend approval was rewritten: %v", err)
	}
}

// A policy change retires both the original operator allocations and their
// verified repairs; validator custody and all original journal rows are kept.
func TestPolicyRevisionRetiresVerifiedOperatorRepairExactlyOnce(t *testing.T) {
	f := newRetiredAlphaHistoryFixture(t)
	f.cfg.PolicyHash = "0x" + strings.Repeat("ad", 32)
	current := *testSetupFacts()
	base := actionByID(t, f.ancestor, "alpha.transfer.validator.1")
	var err error
	current.ReserveValidatorAlphaRao, err = strconv.ParseUint(base.Parameters["planned_final_stake_rao"], 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	want := f.omittedRepair.Spend.AlphaRao
	for _, action := range f.ancestor.Actions {
		if strings.HasPrefix(action.ID, "alpha.transfer.operator-deposit.") {
			want += action.Spend.AlphaRao
		}
	}
	revised, err := buildPlanRevisionFromFacts(f.cfg, f.stateDir, f.ancestor, &current, f.entries, time.Unix(3, 0))
	if err != nil {
		t.Fatal(err)
	}
	if revised.SupersededSpend.AlphaRao != want || actionByID(t, revised, base.ID).IntentHash != base.IntentHash {
		t.Fatalf("policy retirement lost verified alpha or replayed validator custody: retired=%d want=%d", revised.SupersededSpend.AlphaRao, want)
	}
	for _, action := range revised.Actions {
		if action.ID == f.omittedRepair.ID {
			t.Fatal("old-policy operator repair remained executable")
		}
	}
	persistFleetCommitmentRecoveryTestPlan(t, f.stateDir, revised)
	again, err := buildPlanRevisionFromFacts(f.cfg, f.stateDir, revised, &current, f.entries, time.Unix(4, 0))
	if err != nil {
		t.Fatal(err)
	}
	if again.SupersededSpend.AlphaRao != want || !slices.Contains(again.PriorPlanHashes, f.ancestor.PlanHash) {
		t.Fatal("second policy revision duplicated retirement or dropped source lineage")
	}
}
