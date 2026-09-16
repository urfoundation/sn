// Software-only revisions retain completed reserve funding and its historical
// target proof while the separate live majority floor remains mandatory.
package main

import (
	"bytes"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Build a real bounded repair and its completed journal identities, then
// supply the normal revision stage with the same ordered actions before the
// retained repair chain is reinserted. No live identity or chain is used.
func completedReserveSoftwareRevisionFixture(t *testing.T) (reserveRepairSuccessionFixture, *SetupPlan) {
	t.Helper()
	f := newReserveRepairSuccessionFixture(t)
	f.cfg.MaximumAlphaRao = f.prior.Limits.AlphaRao + 3_000_000_000_000
	f.prior = f.revise(t)
	f.repair = actionByID(t, f.prior, "alpha.repair.validator.1.3")
	if f.repair.Spend.AlphaRao != 6_000_000_000_000 || f.prior.MaximumSpend.AlphaRao+f.prior.SupersededSpend.AlphaRao != f.cfg.MaximumAlphaRao {
		t.Fatal("completed repair fixture did not exhaust its exact cumulative ceiling")
	}
	for _, id := range []string{f.repair.ID, "validator.reserve-majority"} {
		action := actionByID(t, f.prior, id)
		f.entries = append(f.entries, JournalEntry{DeploymentID: f.prior.DeploymentID, PlanHash: f.prior.PlanHash, ActionID: id, IntentHash: action.IntentHash, Stage: StageVerified})
	}
	f.current.RegisteredAlphaRao = 40_000_000_000_000
	f.current.ReserveValidatorAlphaRao = 25_999_000_000_000
	if !alphaShareMeets(f.current.RegisteredAlphaRao, f.current.ReserveValidatorAlphaRao, 6_000) || alphaShareMeets(f.current.RegisteredAlphaRao, f.current.ReserveValidatorAlphaRao, 6_500) {
		t.Fatal("fixture must pass the ongoing majority floor and fail the funding target")
	}
	raw, err := json.Marshal(f.prior)
	if err != nil {
		t.Fatal(err)
	}
	var revised SetupPlan
	if err := json.Unmarshal(raw, &revised); err != nil {
		t.Fatal(err)
	}
	revised.ReleaseLockHash = "0x" + strings.Repeat("c7", 32)
	if revised.ReleaseLockHash == f.prior.ReleaseLockHash {
		t.Fatal("fixture did not change the software release")
	}
	revised.PriorPlanHashes = append(revised.PriorPlanHashes, f.prior.PlanHash)
	revised.PlanHash = ""
	revised.Actions = slices.DeleteFunc(revised.Actions, func(action Action) bool {
		return strings.HasPrefix(action.ID, "alpha.repair.validator.1.")
	})
	for index := range revised.Actions {
		action := &revised.Actions[index]
		if action.ID != "validator.reserve-majority" {
			continue
		}
		action.DependsOn = []string{"alpha.transfer.validator.1", "alpha.transfer.validator.2"}
		action.IntentHash, err = actionIntentHash(*action)
		if err != nil {
			t.Fatal(err)
		}
	}
	revised.MaximumSpend, err = maximumActionSpend(revised.Actions)
	if err != nil {
		t.Fatal(err)
	}
	return f, &revised
}

// Invoke the same failing stage as the live revision; its chain input comes
// from the existing exact predecessor and executable-repair admission.
func applyCompletedReserveSoftwareRevision(f reserveRepairSuccessionFixture, revised *SetupPlan) error {
	repairs, err := priorReserveValidatorRepairChain(revised, f.prior, f.entries)
	if err != nil {
		return err
	}
	repairs, err = retainExecutableReserveValidatorRepairChain(f.cfg, f.prior, &f.current, f.entries, repairs)
	if err != nil {
		return err
	}
	return applyReserveValidatorMajorityRepair(f.cfg, revised, f.prior, &f.current, f.entries, repairs)
}

// JSON canonicalizes the DecimalUint Go zero value to "0". Compare every
// economic component while retaining raw fixture diagnostics for that boundary.
func requireUnchangedReserveSoftwareSpend(t *testing.T, prior, revised *SetupPlan) {
	t.Helper()
	for _, pair := range []struct {
		name           string
		prior, revised Spend
	}{
		{name: "active", prior: prior.MaximumSpend, revised: revised.MaximumSpend},
		{name: "retired", prior: prior.SupersededSpend, revised: revised.SupersededSpend},
		{name: "limits", prior: prior.Limits, revised: revised.Limits},
	} {
		if pair.prior != pair.revised {
			t.Logf("%s raw spending representations: prior=%#v revised=%#v", pair.name, pair.prior, pair.revised)
		}
		equal, err := equalSpend(pair.prior, pair.revised)
		if err != nil || !equal {
			t.Errorf("software-only carry changed %s spending: prior=%#v revised=%#v: %v", pair.name, pair.prior, pair.revised, err)
		}
	}
}

// Previously this exact completed state demanded another 65% transfer and
// failed with no repair capacity, although no funding or policy changed.
func TestPlanRevisionSoftwareOnlyCarriesCompletedReserveRepair(t *testing.T) {
	t.Parallel()
	f, revised := completedReserveSoftwareRevisionFixture(t)
	before, err := json.Marshal(f.prior)
	if err != nil {
		t.Fatal(err)
	}
	// The ordinary approval hash already excludes advancing observations.
	revised.LiveFacts.FinalizedBlock++
	revised.LiveFacts.EVMFinalizedBlock++
	revised.LiveFacts.WalletFreeTAORao++
	// One unlocked source credit also increases its coldkey total and capacity.
	revised.LiveFacts.AlphaAvailableRao++
	revised.LiveFacts.WalletNetuidAlphaRao++
	revised.LiveFacts.AlphaTransferableRao++
	if err := applyCompletedReserveSoftwareRevision(f, revised); err != nil {
		t.Fatalf("software-only release repeated completed reserve funding: %v", err)
	}
	requireUnchangedReserveSoftwareSpend(t, f.prior, revised)
	wantActions, wantErr := json.Marshal(f.prior.Actions)
	gotActions, gotErr := json.Marshal(revised.Actions)
	if wantErr != nil || gotErr != nil || !bytes.Equal(wantActions, gotActions) {
		t.Errorf("completed repair, barrier or ordered action history changed: prior=%v revised=%v", wantErr, gotErr)
	}
	after, err := json.Marshal(f.prior)
	if err != nil || !bytes.Equal(before, after) {
		t.Errorf("revision changed its immutable predecessor: %v", err)
	}
	newHash, err := revised.hash()
	if err != nil || newHash == f.prior.PlanHash {
		t.Errorf("new software release did not require a new approval: %v", err)
	}
	comparison := *revised
	comparison.ReleaseLockHash = f.prior.ReleaseLockHash
	comparison.PriorPlanHashes = f.prior.PriorPlanHashes
	comparison.PlanHash = f.prior.PlanHash
	hash, err := comparison.hash()
	if err != nil || hash != f.prior.PlanHash {
		t.Errorf("software-only carry changed another approval field: %v", err)
	}
	// This repair-stage fixture changes only the outer release identity; its
	// fresh companion still belongs to the predecessor. Validate the complete
	// reconstructed predecessor here; the full renderer below proves new carry.
	if err := validatePlanBudget(&comparison); err != nil {
		t.Errorf("carried exact exhausted predecessor budget is invalid: %v", err)
	}
}

// Exercise the complete revision renderer with an independently authenticated
// original companion, archived approvals and completed repair. This catches
// reconstruction drift which a direct repair-stage fixture cannot observe.
func TestPlanRevisionSoftwareOnlyRebuildsAuthenticatedCompletedReserve(t *testing.T) {
	t.Parallel()
	fixture := newValidatorEvidenceCarryTestFixture(t, false)
	observed := fixture.authenticate(t)
	executor := fixture.executor
	cfg := executor.cfg
	prior := *executor.plan
	prior.validatorEvidenceObserved = observed
	entries := executor.journal.Entries()
	for _, id := range []string{"alpha.transfer.validator.1", "alpha.transfer.validator.2", "validator.reserve-majority"} {
		action := actionByID(t, &prior, id)
		entries = append(entries, JournalEntry{DeploymentID: prior.DeploymentID, PlanHash: prior.PlanHash, ActionID: id, IntentHash: action.IntentHash, Stage: StageVerified})
	}
	cfg.Config.ValidatorBootstrap.MaximumReserveRepairAlphaRao = 6_000_000_000_000
	cfg.MaximumAlphaRao = prior.MaximumSpend.AlphaRao + 6_000_000_000_000
	var err error
	cfg.ConfigHash, err = releaseConfigHash(cfg.Config, cfg.Public, cfg.Hyperparameters)
	if err != nil {
		t.Fatal(err)
	}
	current := prior.LiveFacts
	current.DeployerNonce = prior.ValidatorEvidence.DeployerNonce + 1
	current.RegisteredAlphaRao = 30_000_000_000_000
	current.ReserveValidatorAlphaRao, err = strconv.ParseUint(actionByID(t, &prior, "alpha.transfer.validator.1").Parameters["planned_final_stake_rao"], 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	repaired, err := buildPlanRevisionFromFacts(cfg, executor.stateDir, &prior, &current, entries, time.Unix(2, 0))
	if err != nil {
		t.Fatalf("bounded repair prerequisite: %v", err)
	}
	repair := actionByID(t, repaired, "alpha.repair.validator.1.2")
	if repair.Spend.AlphaRao != 6_000_000_000_000 || repaired.MaximumSpend.AlphaRao+repaired.SupersededSpend.AlphaRao != cfg.MaximumAlphaRao || repaired.ValidatorEvidenceCarry == nil {
		t.Fatal("full renderer fixture lacks an exhausted repair or authenticated companion carry")
	}
	if err := writeRunInputs(cfg, executor.stateDir, repaired, executor.roles); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{repair.ID, "validator.reserve-majority"} {
		action := actionByID(t, repaired, id)
		entries = append(entries, JournalEntry{DeploymentID: repaired.DeploymentID, PlanHash: repaired.PlanHash, ActionID: id, IntentHash: action.IntentHash, Stage: StageVerified})
	}
	repaired.validatorEvidenceObserved = observed
	cfg.Release.Repositories["sn_go_source_hash"] = "sha256:" + strings.Repeat("c8", 32)
	current.RegisteredAlphaRao = 40_000_000_000_000
	current.ReserveValidatorAlphaRao = 25_999_000_000_000
	revised, err := buildPlanRevisionFromFacts(cfg, executor.stateDir, repaired, &current, entries, time.Unix(3, 0))
	if err != nil {
		t.Fatalf("complete software-only renderer repeated completed reserve funding: %v", err)
	}
	requireUnchangedReserveSoftwareSpend(t, repaired, revised)
	if revised.PlanHash == repaired.PlanHash || revised.ReleaseLockHash == repaired.ReleaseLockHash {
		t.Error("complete renderer failed to bind the new software")
	}
	if err := validatePlanBudget(revised); err != nil {
		t.Errorf("complete authenticated software revision is invalid: %v", err)
	}
	comparison := *revised
	comparison.ReleaseLockHash = repaired.ReleaseLockHash
	comparison.PriorPlanHashes = repaired.PriorPlanHashes
	hash, err := comparison.hash()
	if err != nil || hash != repaired.PlanHash {
		t.Errorf("complete renderer changed another approval field: %v", err)
	}
	if fixture.rpc.sends.Load() != 0 || fixture.independent != nil && fixture.independent.sends.Load() != 0 {
		t.Error("software-only preparation broadcast a transaction")
	}
}

// Equality with the configured live minimum uses the existing integer share
// rule; the historical target is neither relabeled nor newly proved here.
func TestPlanRevisionSoftwareOnlyAcceptsCurrentReserveFloor(t *testing.T) {
	t.Parallel()
	f, revised := completedReserveSoftwareRevisionFixture(t)
	f.current.ReserveValidatorAlphaRao = f.current.RegisteredAlphaRao * 6 / 10
	if err := applyCompletedReserveSoftwareRevision(f, revised); err != nil {
		t.Fatalf("completed software-only carry rejected the configured live floor: %v", err)
	}
}

// A later below-floor census restores the ordinary target-and-budget refusal
// even though every historical transfer and majority receipt is complete.
func TestPlanRevisionSoftwareOnlyRejectsDilutionBelowReserveFloor(t *testing.T) {
	t.Parallel()
	f, revised := completedReserveSoftwareRevisionFixture(t)
	f.current.ReserveValidatorAlphaRao = f.current.RegisteredAlphaRao*6/10 - 1
	if err := applyCompletedReserveSoftwareRevision(f, revised); err == nil || !strings.Contains(err.Error(), "no repair capacity") {
		t.Fatalf("below-floor software revision bypassed target or cumulative budget: %v", err)
	}
}

// Changes outside the software lock and ancestor list retain the original
// target repair policy, including exact ordered actions and retired spend.
func TestPlanRevisionSoftwareOnlyRequiresExactPriorApproval(t *testing.T) {
	t.Parallel()
	for _, fault := range []string{"same-release", "config", "policy", "inputs", "limits", "retired-spend", "funding", "action-order", "repair-terms"} {
		f, revised := completedReserveSoftwareRevisionFixture(t)
		switch fault {
		case "same-release":
			revised.ReleaseLockHash = f.prior.ReleaseLockHash
		case "config":
			revised.ConfigHash = "0x" + strings.Repeat("d1", 32)
		case "policy":
			revised.PolicyHash = "0x" + strings.Repeat("d2", 32)
		case "inputs":
			revised.ResolvedInputsHash = "0x" + strings.Repeat("d3", 32)
		case "limits":
			revised.Limits.TAORao++
		case "retired-spend":
			revised.SupersededSpend.TAORao++
		case "funding":
			for index := range revised.Actions {
				if revised.Actions[index].ID == "validator.fund.1" {
					revised.Actions[index].Spend.TAORao++
					var err error
					revised.Actions[index].IntentHash, err = actionIntentHash(revised.Actions[index])
					if err != nil {
						t.Fatal(err)
					}
				}
			}
		case "action-order":
			revised.Actions[0], revised.Actions[1] = revised.Actions[1], revised.Actions[0]
		case "repair-terms":
			f.cfg.Config.ValidatorBootstrap.ReserveTargetShareBPS++
		}
		if err := applyCompletedReserveSoftwareRevision(f, revised); err == nil || !strings.Contains(err.Error(), "no repair capacity") {
			t.Errorf("%s entered software-only reserve carry: %v", fault, err)
		}
	}
}

// A stale bootstrap/barrier receipt or foreign repair witness cannot turn
// unfinished work into the completed software-only exception.
func TestPlanRevisionSoftwareOnlyRequiresExactCompletedHistory(t *testing.T) {
	t.Parallel()
	for _, fault := range []string{"independent", "barrier", "foreign-barrier", "repair-finalized", "foreign-repair", "repair-intent"} {
		f, revised := completedReserveSoftwareRevisionFixture(t)
		for index := range f.entries {
			entry := &f.entries[index]
			if fault == "independent" && entry.ActionID == "alpha.transfer.validator.2" || fault == "barrier" && entry.ActionID == "validator.reserve-majority" {
				entry.Stage = StageFinalized
			}
			if fault == "foreign-barrier" && entry.ActionID == "validator.reserve-majority" || fault == "foreign-repair" && entry.ActionID == f.repair.ID {
				entry.PlanHash = "0x" + strings.Repeat("d4", 32)
			}
			if fault == "repair-finalized" && entry.ActionID == f.repair.ID {
				entry.Stage = StageFinalized
			}
			if fault == "repair-intent" && entry.ActionID == f.repair.ID {
				entry.IntentHash = "0x" + strings.Repeat("d5", 32)
			}
		}
		if strings.Contains(fault, "repair") {
			// An unfinished fixed tranche still has to reach 65%. Make its
			// prospective credit insufficient so the original refusal is observable.
			f.current.RegisteredAlphaRao = 200_000_000_000_000
			f.current.ReserveValidatorAlphaRao = 122_000_000_000_000
		}
		if err := applyCompletedReserveSoftwareRevision(f, revised); err == nil {
			t.Errorf("%s was treated as a completed reserve proof", fault)
		}
	}
}
