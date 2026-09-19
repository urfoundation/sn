package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func reserveRepairPreSignFixture(t *testing.T) reserveRepairSuccessionFixture {
	t.Helper()
	f := newReserveRepairSuccessionFixture(t)
	// Synthetic emission growth needs about 3,250 alpha. The reviewed 3,750
	// tranche keeps about 500 alpha of headroom within the existing 6,000 cap.
	f.current.RegisteredAlphaRao = (f.current.ReserveValidatorAlphaRao + 3_250_000_000_000) * 10_000 / 6_500
	f.cfg.MaximumAlphaRao = f.prior.MaximumSpend.AlphaRao + f.prior.SupersededSpend.AlphaRao - f.repair.Spend.AlphaRao + 3_750_000_000_000
	journal, err := OpenJournal(f.state)
	if err != nil {
		t.Fatal(err)
	}
	intent := JournalEntry{DeploymentID: f.prior.DeploymentID, PlanHash: f.prior.PlanHash,
		ActionID: f.repair.ID, IntentHash: f.repair.IntentHash, Stage: StageIntent}
	if err := journal.Append(intent); err != nil {
		t.Fatal(err)
	}
	failure := intent
	failure.Stage = StageFailed
	failure.Error = fmt.Sprintf(reserveRepairPreSignFailureFormat, f.current.ReserveValidatorAlphaRao, f.repair.Spend.AlphaRao,
		uint64(1), uint64(f.cfg.Config.ValidatorBootstrap.ReserveTargetShareBPS), f.current.RegisteredAlphaRao)
	if err := journal.Append(failure); err != nil {
		t.Fatal(err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	f.entries, err = readJournalEntries(f.state)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestReserveRepairPreSignFailureCreatesFreshBoundedSuccessor(t *testing.T) {
	f := reserveRepairPreSignFixture(t)
	before, err := os.ReadFile(filepath.Join(f.state, "journal.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	revised := f.revise(t)
	for _, action := range revised.Actions {
		if action.ID == f.repair.ID {
			t.Fatal("pre-sign failed repair remained executable")
		}
	}
	replacement := actionByID(t, revised, "alpha.repair.validator.1.3")
	failure := f.entries[len(f.entries)-1]
	if replacement.Parameters["retired_pre_sign_action_id"] != f.repair.ID ||
		replacement.Parameters["retired_pre_sign_plan_hash"] != f.prior.PlanHash ||
		replacement.Parameters["retired_pre_sign_intent_hash"] != f.repair.IntentHash ||
		replacement.Parameters["retired_pre_sign_failure_hash"] != failure.EntryHash ||
		replacement.Parameters["retired_pre_sign_failure_sequence"] != strconv.FormatUint(failure.Sequence, 10) ||
		!slices.Equal(replacement.DependsOn, f.repair.DependsOn) || !slices.Contains(revised.PriorPlanHashes, f.prior.PlanHash) {
		t.Fatal("replacement did not preserve explicit retirement provenance and dependencies", replacement)
	}
	if revised.MaximumSpend.AlphaRao+revised.SupersededSpend.AlphaRao != f.cfg.MaximumAlphaRao ||
		revised.SupersededSpend.AlphaRao != f.prior.SupersededSpend.AlphaRao || replacement.Spend.AlphaRao > f.cfg.Config.ValidatorBootstrap.MaximumReserveRepairAlphaRao {
		t.Fatal("replacement changed cumulative allowance or charged the unbroadcast repair twice")
	}
	after, err := os.ReadFile(filepath.Join(f.state, "journal.jsonl"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("revision rewrote original failure evidence", err)
	}
	if err := validatePlanBudget(revised); err != nil {
		t.Fatal(err)
	}
	required, _, err := reserveValidatorTransferRao(f.current.RegisteredAlphaRao, f.current.ReserveValidatorAlphaRao, 1, 6_500)
	if err != nil {
		t.Fatal(err)
	}
	credit, err := alphaTransferMinimumCreditRao(replacement.Spend.AlphaRao)
	if err != nil {
		t.Fatal(err)
	}
	if replacement.Spend.AlphaRao != 3_750_000_000_000 || replacement.Spend.AlphaRao-required < 499_000_000_000 ||
		!alphaShareMeets(f.current.RegisteredAlphaRao, f.current.ReserveValidatorAlphaRao+credit, 6_500) {
		t.Fatal("replacement lost the approved bounded tranche or emission-dilution cushion")
	}
	// A snapshot diluted further than the newly approved ceiling remains a
	// blocking budget error; no stale amount or reduced target is substituted.
	f.current.RegisteredAlphaRao += 20_000_000_000_000
	if _, err := retainExecutableReserveValidatorRepairChain(f.cfg, f.prior, &f.current, f.entries, []Action{f.repair}); err != nil {
		t.Fatal(err)
	}
	if _, err := buildPlanRevisionFromFacts(f.cfg, f.state, f.prior, &f.current, f.entries, time.Unix(4, 0)); err == nil {
		t.Fatal("additional dilution silently exceeded the approved repair allowance")
	}
}

func TestReserveRepairPreSignFailureRejectsEverySigningAndAmbiguousHistory(t *testing.T) {
	f := reserveRepairPreSignFixture(t)
	for _, fault := range []string{"transaction", "signer", "nonce", "checkpoint", "foreign-plan", "foreign-intent", "unpaired-intent", "second-attempt", "wrong-error", "forged-hash", "wrong-amount", "success-disguised-as-failure"} {
		entries := append([]JournalEntry(nil), f.entries...)
		index := len(entries) - 1
		switch fault {
		case "transaction":
			entries[index].TransactionHash = "0x" + strings.Repeat("11", 32)
		case "signer":
			entries[index].Signer = "synthetic-signer"
		case "nonce":
			entries[index].Nonce = "7"
		case "checkpoint":
			entries[index].RecoveryBlock = 7
		case "foreign-plan":
			entries[index].PlanHash = "0x" + strings.Repeat("22", 32)
		case "foreign-intent":
			entries[index].IntentHash = "0x" + strings.Repeat("33", 32)
		case "unpaired-intent":
			entries = entries[:index]
		case "second-attempt":
			entries = append(entries, entries[index-1], entries[index])
		case "wrong-error":
			entries[index].Error = "unclassified dispatch failure"
		case "forged-hash":
			entries[index].EntryHash = "0x" + strings.Repeat("44", 32)
		case "wrong-amount":
			entries[index].Error = fmt.Sprintf(reserveRepairPreSignFailureFormat, f.current.ReserveValidatorAlphaRao, f.repair.Spend.AlphaRao+1, 1, 6500, f.current.RegisteredAlphaRao)
		case "success-disguised-as-failure":
			entries[index].Error = fmt.Sprintf(reserveRepairPreSignFailureFormat, f.current.RegisteredAlphaRao-f.repair.Spend.AlphaRao, f.repair.Spend.AlphaRao, 1, 6500, f.current.RegisteredAlphaRao)
		}
		if fault == "wrong-error" || fault == "wrong-amount" || fault == "success-disguised-as-failure" {
			entries[index].EntryHash = ""
			hash, err := canonicalHashHex(entries[index])
			if err != nil {
				t.Fatal(err)
			}
			entries[index].EntryHash = hash
		}
		if _, err := reserveRepairPreSignFailure(f.prior, f.repair, entries); err == nil {
			t.Fatalf("%s history retired a potentially signed repair", fault)
		}
	}
}
