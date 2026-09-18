package main

// Count actual historical decoder and journal reads over synthetic approvals.
// These regressions use explicit mutations, never timing or a replacement verdict.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Existing owned-route fixtures preserve authentic source plans and receipts.
func provisionalPreparationCacheTest(t *testing.T, count int) carriedPreparationTest {
	t.Helper()
	fixture := newCarriedPreparationTest(t, count)
	fixture.executor.cfg.provisionalResume = &provisionalResumeState{Record: &provisionalResumeRecord{PlanHash: fixture.executor.plan.PlanHash, Provisional: true}}
	return fixture
}

// The first invocation copies the journal twice and decodes each immutable
// source once. An unchanged later invocation validates the durable journal and
// file witnesses, then reuses the authenticated receipt audit.
func TestProvisionalPreparationReadsEachSourceOncePerInvocation(t *testing.T) {
	fixture := provisionalPreparationCacheTest(t, 64)
	executor := fixture.executor
	before := validatorNamespaceTreeSnapshot(t, executor.stateDir)
	journalReads, copied, sourceReads := 0, 0, 0
	readEntries := func() []JournalEntry {
		entries := executor.journal.Entries()
		journalReads++
		copied += len(entries)
		return entries
	}
	readSource := func(stateDir, hash string) (*SetupPlan, error) {
		sourceReads++
		return readValidatorEvidenceHistoricalPlan(stateDir, hash)
	}
	for invocation := 1; invocation <= 2; invocation++ {
		if err := executor.collectCarriedActionHistoryWithReaders(t.Context(), readEntries, readSource); err != nil {
			t.Fatal("actual provisional preparation failed", err)
		}
		if invocation == 1 && (journalReads != 2 || copied != 2*len(fixture.entries) || sourceReads != 1) {
			t.Fatalf("cold audit did not authenticate its complete source: journal=%d copied=%d source=%d", journalReads, copied, sourceReads)
		}
		if invocation == 2 && (journalReads != 3 || copied != 3*len(fixture.entries) || sourceReads != 1) {
			t.Fatalf("unchanged durable audit repeated receipt work: journal=%d copied=%d source=%d", journalReads, copied, sourceReads)
		}
	}
	if len(executor.carriedVerificationKeys) != 0 {
		t.Fatal("read-only provisional preparation installed execution shortcuts")
	}
	after := validatorNamespaceTreeSnapshot(t, executor.stateDir)
	for path, value := range before {
		if after[path] != value {
			t.Fatalf("read-only provisional preparation changed retained history at %s", path)
		}
	}
	for path := range after {
		if _, existed := before[path]; !existed && !strings.HasPrefix(path, provisionalPreparationPersistentCacheDir+"/") && path != provisionalPreparationPersistentCacheDir {
			t.Fatalf("read-only provisional preparation added unexpected state %s", path)
		}
	}
}

// A decoded source never authenticates another receipt's bytes or observer
// labels. Both independent corruptions must be reported in the same pass.
func TestProvisionalPreparationChecksEveryReceiptAfterSourceDecode(t *testing.T) {
	fixture := provisionalPreparationCacheTest(t, 3)
	executor := fixture.executor
	sourceReads := 0
	readSource := func(stateDir, hash string) (*SetupPlan, error) {
		sourceReads++
		plan, err := readValidatorEvidenceHistoricalPlan(stateDir, hash)
		if sourceReads == 1 {
			for index := 1; index <= 2; index++ {
				record := *fixture.records[index]
				if index == 1 {
					record.Observed = map[string]any{"synthetic": "replaced-after-source-read"}
				} else {
					record.OperationalRPCMode = "unapproved-observer"
				}
				raw, encodeErr := json.Marshal(record)
				if encodeErr != nil {
					t.Fatal(encodeErr)
				}
				if writeErr := atomicWrite(filepath.Join(stateDir, fixture.entries[index].PostconditionPath), raw, 0o600); writeErr != nil {
					t.Fatal(writeErr)
				}
			}
		}
		return plan, err
	}
	err := executor.collectCarriedActionHistoryWithReaders(t.Context(), executor.journal.Entries, readSource)
	if err == nil || !strings.Contains(err.Error(), fixture.entries[1].ActionID) || !strings.Contains(err.Error(), fixture.entries[2].ActionID) || sourceReads != 1 {
		t.Fatal("shared source skipped a later receipt or observer check", sourceReads, err)
	}
	if !reflect.DeepEqual(fixture.entries, executor.journal.Entries()) {
		t.Fatal("failed receipt authentication changed the original journal")
	}
}

// An executor's previous successful invocation cannot hide replaced source
// bytes at the same canonical path in a later read-only invocation.
func TestProvisionalPreparationReauthenticatesChangedSource(t *testing.T) {
	fixture := provisionalPreparationCacheTest(t, 2)
	executor := fixture.executor
	if err := executor.verifyProvisionalActionHistory(t.Context()); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(executor.stateDir, "plans", stringsTrim0x(fixture.source.PlanHash)+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	changed := bytes.Replace(raw, []byte("synthetic-reserve"), []byte("changed-source-target"), 1)
	if bytes.Equal(raw, changed) {
		t.Fatal("synthetic source mutation did not reach its target")
	}
	if err := atomicWrite(path, changed, 0o600); err != nil {
		t.Fatal(err)
	}
	before := validatorNamespaceTreeSnapshot(t, executor.stateDir)
	if err := executor.verifyProvisionalActionHistory(t.Context()); err == nil || !strings.Contains(err.Error(), "persisted setup plan hash mismatch") {
		t.Fatal("later invocation reused a previous authenticated source", err)
	}
	if !reflect.DeepEqual(before, validatorNamespaceTreeSnapshot(t, executor.stateDir)) {
		t.Fatal("failed fresh source check changed retained evidence")
	}
}

// Even an unrelated appended row invalidates this read-only reconciliation;
// it cannot silently become authority for subsequent action execution.
func TestProvisionalPreparationRejectsConcurrentJournalAppend(t *testing.T) {
	fixture := provisionalPreparationCacheTest(t, 2)
	executor := fixture.executor
	readSource := func(stateDir, hash string) (*SetupPlan, error) {
		plan, err := readValidatorEvidenceHistoricalPlan(stateDir, hash)
		if appendErr := executor.journal.Append(JournalEntry{DeploymentID: executor.plan.DeploymentID, PlanHash: executor.plan.PlanHash,
			ActionID: fixture.entries[0].ActionID, IntentHash: fixture.entries[0].IntentHash, Stage: StageFailed, Error: "synthetic concurrent observation"}); appendErr != nil {
			t.Fatal(appendErr)
		}
		return plan, err
	}
	if err := executor.collectCarriedActionHistoryWithReaders(t.Context(), executor.journal.Entries, readSource); err == nil || !strings.Contains(err.Error(), "journal changed during reconciliation") {
		t.Fatal("changed journal inherited the earlier receipt snapshot", err)
	}
	if len(executor.carriedVerificationKeys) != 0 {
		t.Fatal("changed snapshot installed verified execution keys")
	}
}

// Cancellation after a real decode remains an error, and the next call owns
// a fresh cache rather than inheriting the canceled reader's partial result.
func TestProvisionalPreparationCancellationKeepsFreshRetry(t *testing.T) {
	fixture := provisionalPreparationCacheTest(t, 2)
	executor := fixture.executor
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	reads := 0
	readSource := func(stateDir, hash string) (*SetupPlan, error) {
		reads++
		plan, err := readValidatorEvidenceHistoricalPlan(stateDir, hash)
		cancel()
		return plan, err
	}
	if err := executor.collectCarriedActionHistoryWithReaders(ctx, executor.journal.Entries, readSource); !errors.Is(err, context.Canceled) || reads != 1 {
		t.Fatal("canceled source authentication was reused", reads, err)
	}
	if err := executor.verifyProvisionalActionHistory(t.Context()); err != nil {
		t.Fatal("fresh retry inherited canceled preparation", err)
	}
}

// Live adoption authenticates only its complete setup prefix. Its fresh
// snapshot cannot waive a missing predecessor or inherit a later journal row.
func TestProvisionalSetupPrefixUsesFreshBoundedHistory(t *testing.T) {
	fixture := provisionalPreparationCacheTest(t, 16)
	executor := fixture.executor
	topology := Action{ID: "topology.launch", Kind: "local"}
	var err error
	topology.IntentHash, err = actionIntentHash(topology)
	if err != nil {
		t.Fatal(err)
	}
	executor.plan.Actions = append(executor.plan.Actions, topology, Action{ID: "synthetic.pending.after-topology", Kind: "local"})
	executor.plan.PlanHash, err = executor.plan.hash()
	if err != nil {
		t.Fatal(err)
	}
	executor.cfg.provisionalResume.Record.PlanHash = executor.plan.PlanHash
	journalReads, sourceReads := 0, 0
	readEntries := func() []JournalEntry { journalReads++; return executor.journal.Entries() }
	readSource := func(stateDir, hash string) (*SetupPlan, error) {
		sourceReads++
		return readValidatorEvidenceHistoricalPlan(stateDir, hash)
	}
	for invocation := 1; invocation <= 2; invocation++ {
		got, err := executor.authenticateProvisionalSetupPrefix(t.Context(), executor.plan, readEntries, readSource)
		if err != nil || got == nil || got.ID != topology.ID || journalReads != 2*invocation || sourceReads != invocation {
			t.Fatal("setup prefix did not keep bounded fresh authentication", journalReads, sourceReads, err)
		}
	}
	appendDuringRead := func(stateDir, hash string) (*SetupPlan, error) {
		plan, err := readValidatorEvidenceHistoricalPlan(stateDir, hash)
		if appendErr := executor.journal.Append(JournalEntry{DeploymentID: executor.plan.DeploymentID, PlanHash: executor.plan.PlanHash,
			ActionID: fixture.entries[0].ActionID, IntentHash: fixture.entries[0].IntentHash, Stage: StageFailed, Error: "synthetic concurrent observation"}); appendErr != nil {
			t.Fatal(appendErr)
		}
		return plan, err
	}
	if _, err := executor.authenticateProvisionalSetupPrefix(t.Context(), executor.plan, executor.journal.Entries, appendDuringRead); err == nil || !strings.Contains(err.Error(), "journal changed during reconciliation") {
		t.Fatal("adoption accepted a changed setup snapshot", err)
	}
	executor.plan.Actions[0].IntentHash = "0x" + strings.Repeat("ef", 32)
	executor.plan.PlanHash, err = executor.plan.hash()
	if err != nil {
		t.Fatal(err)
	}
	executor.cfg.provisionalResume.Record.PlanHash = executor.plan.PlanHash
	if _, err := executor.authenticateProvisionalSetupPrefix(t.Context(), executor.plan, executor.journal.Entries, readSource); err == nil || !strings.Contains(err.Error(), "already verified setup action") {
		t.Fatal("adoption inherited an old intent or ignored a missing prefix action", err)
	}
}
