// Audits run beside a retained writer and aggregate defects without changing
// the journal, deployment, supervisor or reusable verification caches.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Observe real retained bytes; output belongs to stdout, never this tree.
func historicalAuditTree(t *testing.T, root string) map[string]string {
	t.Helper()
	result := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			result[path] = "directory"
			return nil
		}
		wire, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		result[path] = bytesSHA256(wire)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// The real CLI rejects every mutation mode and accepts the approved LAN route.
func TestHistoricalAuditCliIsReadOnly(t *testing.T) {
	command, options, err := parseCLI([]string{"audit", "--owned-rpc-authority", "192.168.1.162:9944", "--format", "json"})
	if err != nil || command != "audit" || options.Apply || !commandUsesCampaignEgress(command) {
		t.Fatalf("read-only audit CLI: %s %+v %v", command, options, err)
	}
	for _, option := range []string{"--apply", "--detach", "--provisional-resume", "--prepare-only"} {
		if _, _, err := parseCLI([]string{"audit", option}); err == nil {
			t.Fatalf("audit accepted mutation mode %s", option)
		}
	}
}

// Preserve the live writer lease and independently report release and reader
// defects while still reaching the complete journal/action-history stages.
func TestHistoricalAuditAggregatesBesideWriterWithoutMutation(t *testing.T) {
	cfg, plan, stateDir, _, _ := provisionalRuntimePlanFixture(t)
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := json.Marshal(roles)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(stateDir, "secrets", "roles.json"), wire, 0o600); err != nil {
		t.Fatal(err)
	}
	writer, err := OpenJournal(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if err := os.WriteFile(filepath.Join(stateDir, "supervisor.state.json"), []byte(`{"retained":"unchanged"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	before := historicalAuditTree(t, stateDir)
	readerFailure := errors.New("synthetic unavailable historical reader")
	calls := 0
	report, err := runHistoricalAuditWithFactory(t.Context(), cfg, cfg, stateDir, plan.PlanHash,
		func(_ context.Context, canonical, transport *ResolvedConfig, stateDir string, plan *SetupPlan, entries []JournalEntry, roles *RoleSecrets) (*Executor, func(), error) {
			calls++
			if !canonical.readOnlyAudit || !transport.readOnlyAudit || canonical.provisionalResume != nil || transport.provisionalResume != nil {
				t.Fatal("audit inherited provisional acceptance or mutable configuration")
			}
			return &Executor{cfg: transport, auditAuthorizedConfig: canonical, plan: plan, stateDir: stateDir, roles: roles,
				journal: &Journal{entries: entries}, preparationIncomplete: true}, func() {}, readerFailure
		})
	if calls != 1 || err == nil || !errors.Is(err, readerFailure) || !errors.Is(err, errPersistedPlanIdentityMismatch) || report.Passed || !report.ReadOnly || report.FinalAcceptance {
		t.Fatalf("independent findings were not retained: calls=%d report=%+v err=%v", calls, report, err)
	}
	for _, name := range []string{"verified-action-history", "journal-snapshot-retained", "plan-snapshot-retained"} {
		found := false
		for _, check := range report.Checks {
			found = found || check.Name == name && check.OK
		}
		if !found {
			t.Fatalf("earlier defect suppressed independent stage %s: %+v", name, report.Checks)
		}
	}
	if after := historicalAuditTree(t, stateDir); !reflect.DeepEqual(before, after) {
		t.Fatal("read-only audit changed retained state")
	}
	if another, err := OpenJournal(stateDir); err == nil {
		another.Close()
		t.Fatal("audit released the active writer lease")
	}
}

// A running campaign may append after the audit snapshot without invalidating
// the immutable prefix that this report actually checked.
func TestHistoricalAuditAllowsConcurrentJournalAppend(t *testing.T) {
	cfg, plan, stateDir, _, _ := provisionalRuntimePlanFixture(t)
	writer, err := OpenJournal(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	report, _ := runHistoricalAuditWithFactory(t.Context(), cfg, cfg, stateDir, plan.PlanHash,
		func(_ context.Context, canonical, transport *ResolvedConfig, stateDir string, plan *SetupPlan, entries []JournalEntry, roles *RoleSecrets) (*Executor, func(), error) {
			action := plan.Actions[0]
			if err := writer.Append(JournalEntry{DeploymentID: plan.DeploymentID, PlanHash: plan.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageIntent}); err != nil {
				t.Fatal(err)
			}
			return &Executor{cfg: transport, auditAuthorizedConfig: canonical, stateDir: stateDir, plan: plan, journal: &Journal{entries: entries}, roles: roles}, nil, nil
		})
	found := false
	for _, check := range report.Checks {
		found = found || check.Name == "journal-snapshot-retained" && check.OK
	}
	if !found || report.JournalEntries != 0 || len(writer.Entries()) != 1 {
		t.Fatalf("append-only progress invalidated historical snapshot: %+v", report)
	}
}

// A current plan's failed receipts must be audited too, rather than silently
// skipped by the preparation-only ancestor filter.
func TestHistoricalAuditCollectsCurrentAndAncestorReceiptDefects(t *testing.T) {
	fixture := newCarriedPreparationTest(t, 2)
	fixture.executor.cfg.readOnlyAudit = true
	fixture.executor.plan = fixture.source
	for _, entry := range fixture.entries {
		if err := os.WriteFile(filepath.Join(fixture.executor.stateDir, filepath.FromSlash(entry.PostconditionPath)), []byte(`{"invalid":true}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	err := fixture.executor.collectCarriedActionHistory(t.Context())
	if err == nil {
		t.Fatal("audit skipped current-plan receipts")
	}
	for _, action := range fixture.source.Actions {
		if !strings.Contains(err.Error(), action.ID) {
			t.Fatalf("audit stopped before independent action %s: %v", action.ID, err)
		}
	}
}

// Accidental dispatch or append cannot gain mutation rights from the snapshot.
func TestHistoricalAuditSnapshotRejectsExecutionAndAppend(t *testing.T) {
	journal := &Journal{}
	if err := journal.Append(JournalEntry{}); err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("snapshot append was not rejected: %v", err)
	}
	executor := &Executor{cfg: &ResolvedConfig{readOnlyAudit: true}, journal: journal}
	if err := executor.Execute(t.Context(), Action{ID: "topology.launch"}); err == nil || !strings.Contains(err.Error(), "cannot execute") {
		t.Fatalf("audit action dispatch was not rejected: %v", err)
	}
	if _, _, err := executor.persistActionPostcondition(nil); err == nil || !strings.Contains(err.Error(), "cannot persist") {
		t.Fatalf("audit postcondition write was not rejected: %v", err)
	}
}

// Cache reads retain their proof semantics, but misses cannot create state.
func TestHistoricalAuditDoesNotPersistVerificationSuccesses(t *testing.T) {
	executor := historicalAuditCacheTestExecutor(t)
	executor.cfg.readOnlyAudit = true
	before := historicalAuditTree(t, executor.stateDir)
	calls := 0
	for repeat := 0; repeat < 2; repeat++ {
		hit, err := executor.withHistoricalAuditCache(t.Context(), "native-receipt", historicalAuditCacheTestInput(), func(context.Context) error {
			calls++
			return nil
		})
		if hit || err != nil {
			t.Fatalf("read-only miss was improperly persisted: %t %v", hit, err)
		}
	}
	if calls != 2 || !reflect.DeepEqual(before, historicalAuditTree(t, executor.stateDir)) {
		t.Fatal("read-only audit published cache state")
	}
}

// The normal recovery path can repair this exact missing observation. Audit
// authenticates the same CREATE and boundary while preserving persisted bytes.
func TestHistoricalAuditDoesNotPersistRecoveredDeploymentBoundary(t *testing.T) {
	executor, reader := deploymentBoundaryTestExecution(t)
	executor.cfg.readOnlyAudit = true
	executor.journal = &Journal{entries: executor.journal.Entries()}
	before := historicalAuditTree(t, executor.stateDir)
	if err := executor.ensurePayloads(t.Context()); err != nil {
		t.Fatal(err)
	}
	if executor.payloads.Manifest.CoordinatorEventStartBlock != 100 || reader.unexpectedRequests.Load() != 0 {
		t.Fatal("audit did not authenticate the retained deployment boundary")
	}
	if !reflect.DeepEqual(before, historicalAuditTree(t, executor.stateDir)) {
		t.Fatal("audit persisted a recovered deployment boundary")
	}
}
