// Real temporary filter files force both crash windows: completed removal
// before checkpoint and public evidence before its owner-authenticated update.
package main

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestLifecycleTerminalCleanupReconcilesRemovedRuleBeforeCheckpoint(t *testing.T) {
	f := newLifecycleCleanupTestFixture(t)
	_, driver, _, _ := fleetLifecycleFilterDriverFixture(t)
	// The signed succession fixture has a reviewed six-epoch config. Its
	// filter driver must use that same configuration, not the five-epoch default.
	driver.cfg = f.history.campaign.cfg
	specs, err := releaseFleetLifecycleFaults(f.history.campaign.cfg, f.window.EpochBlocks)
	if err != nil {
		t.Fatal(err)
	}
	for index, spec := range specs {
		processes, err := driver.Apply(t.Context(), spec)
		if err != nil {
			t.Fatal(err)
		}
		f.records[index].Processes = processes
	}
	attempt := f.history.attempt
	runDir := filepath.Join(f.history.campaign.stateDir, "runs", attempt.payload.RunID)
	for index := range f.records {
		for _, original := range attempt.payload.AcceptanceBoundary.Faults {
			if original.ID == f.records[index].ID {
				f.records[index].ArmedBlock, f.records[index].ArmedBlockHash = original.ArmedBlock, original.ArmedBlockHash
			}
		}
	}
	if err := appendObservation(filepath.Join(runDir, "observations.jsonl"), f.current); err != nil {
		t.Fatal(err)
	}
	failed := false
	checkpoint := func() error {
		full := cloneScenarioFaultRecords(attempt.payload.AcceptanceBoundary.Faults)
		for index := range full {
			for _, record := range f.records {
				if full[index].ID == record.ID {
					full[index] = record
				}
			}
		}
		if err := writeScenarioFaultEvidence(runDir, full); err != nil {
			return err
		}
		if !failed && len(f.records[0].LifecycleCleanup.RemovedProcesses) != 0 {
			failed = true
			return errors.New("synthetic owner checkpoint interruption after public evidence write")
		}
		return attempt.updateAuthenticatedRuntime(runDir, full)
	}
	if err := advanceScenarioLifecycleCleanup(t.Context(), f.history.campaign.cfg, f.window, f.current, f.binding, f.records, driver, checkpoint); err == nil || !failed {
		t.Fatal("interruption was not forced after exact removal", err)
	}
	_, _, _, _, signed, err := attempt.loadAuthenticatedRuntimeForensics(runDir)
	if err != nil {
		t.Fatal(err)
	}
	var authenticated []ScenarioFaultRecord
	for _, record := range signed {
		if lifecycleCleanupFault(record) {
			authenticated = append(authenticated, record)
		}
	}
	if len(authenticated) != 2 || authenticated[0].LifecycleCleanup == nil || len(authenticated[0].LifecycleCleanup.RemovedProcesses) != 0 {
		t.Fatal("unsigned removal evidence leaked into the authenticated request checkpoint")
	}
	var public struct {
		Schema string                `json:"schema"`
		Faults []ScenarioFaultRecord `json:"faults"`
	}
	if err := readJSONFile(filepath.Join(runDir, "faults.json"), &public); err != nil {
		t.Fatal(err)
	}
	unsignedRemoval := false
	for _, record := range public.Faults {
		if record.ID == authenticated[0].ID && record.LifecycleCleanup != nil && len(record.LifecycleCleanup.RemovedProcesses) != 0 {
			unsignedRemoval = true
		}
	}
	if !unsignedRemoval {
		t.Fatal("fixture did not write the unsigned completion before interruption")
	}
	operator, _, rule, err := driver.validatorViewRule(specs[0])
	if err != nil {
		t.Fatal(err)
	}
	receiptPath := validatorViewRestoreReceiptPath(driver.stateDir, operator, rule.RuleID)
	receipt, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := driver.Restore(t.Context(), specs[0]); err == nil {
		t.Fatal("ordinary restore unexpectedly authorized a missing active ledger entry")
	}
	// Retry from only the retained request. The unsigned public census is
	// ignored; the exact receipt and absent rule independently reconstruct it.
	f.records = cloneScenarioFaultRecords(authenticated)
	if err := advanceScenarioLifecycleCleanup(t.Context(), f.history.campaign.cfg, f.window, f.current, f.binding, f.records, driver, checkpoint); err != nil {
		t.Fatal("exact completed removal could not reconcile", err)
	}
	if faultsComplete(f.records) {
		t.Fatal("reconciliation invented its later complete observation")
	}
	retained, err := os.ReadFile(receiptPath)
	if err != nil || !slices.Equal(receipt, retained) {
		t.Fatal("reconciliation repeated or replaced the durable removal", err)
	}
	active, err := readActiveFaultFile(driver.activePath())
	if err != nil || len(active.Faults) != 0 {
		t.Fatal("exact filter cleanup left active controls", err)
	}
	f.nextObservation(t)
	if err := appendObservation(filepath.Join(runDir, "observations.jsonl"), f.current); err != nil {
		t.Fatal(err)
	}
	if err := advanceScenarioLifecycleCleanup(t.Context(), f.history.campaign.cfg, f.window, f.current, f.binding, f.records, driver, checkpoint); err != nil || !faultsComplete(f.records) {
		t.Fatal("reconciled cleanup did not complete from fresh evidence", err)
	}
}

// Missing ledger state is never enough: a retained exact receipt and absence
// of the same rule are both required, and ordinary filters remain excluded.
func TestLifecycleTerminalCleanupRejectsUnownedRemovalReconciliation(t *testing.T) {
	for _, mutation := range []string{"missing-receipt", "foreign-receipt", "live-rule", "foreign-filter"} {
		_, driver, boundary, companion := fleetLifecycleFilterDriverFixture(t)
		if _, err := driver.Apply(t.Context(), companion); err != nil {
			t.Fatal(err)
		}
		if _, err := driver.Restore(t.Context(), companion); err != nil {
			t.Fatal(err)
		}
		operator, _, rule, err := driver.validatorViewRule(companion)
		if err != nil {
			t.Fatal(err)
		}
		receiptPath := validatorViewRestoreReceiptPath(driver.stateDir, operator, rule.RuleID)
		var receipt validatorViewRestoreReceipt
		if err := readJSONFile(receiptPath, &receipt); err != nil {
			t.Fatal(err)
		}
		spec := companion
		switch mutation {
		case "missing-receipt":
			err = os.Remove(receiptPath)
		case "foreign-receipt":
			receipt.OperatorNo++
			err = writeValidatorViewRestoreReceipt(receiptPath, receipt)
		case "live-rule":
			err = writePublicJSON(verifyAssignmentFilterPath(driver.stateDir, operator), receipt.PreFilter)
		case "foreign-filter":
			spec = boundary
		}
		if err != nil {
			t.Fatal(err)
		}
		if _, err := driver.restoreLifecycleCleanup(t.Context(), spec); err == nil {
			t.Fatalf("%s acquired cleanup authority", mutation)
		}
	}
}

func TestLifecycleTerminalCleanupRejectsMissingTerminalEvidence(t *testing.T) {
	f := newLifecycleCleanupTestFixture(t)
	driver := &lifecycleCleanupTestDriver{}
	if err := advanceScenarioLifecycleCleanup(t.Context(), f.history.campaign.cfg, nil, f.current, f.binding, f.records, driver, func() error { return nil }); err == nil {
		t.Fatal("missing signed window admitted cleanup")
	}
	if err := advanceScenarioLifecycleCleanup(t.Context(), f.history.campaign.cfg, f.window, nil, f.binding, f.records, driver, func() error { return nil }); err == nil {
		t.Fatal("missing terminal observation admitted cleanup")
	}
	if len(driver.restored) != 0 {
		t.Fatal("missing evidence caused a control mutation")
	}
}
