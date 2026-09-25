//go:build linux || darwin

// Real archived approvals, signed transactions and closed attempts exercise
// diagnostic reads without converting retained failures into strict acceptance.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	validatorpkg "github.com/urfoundation/sn/validator"
)

// Enter through the actual external diagnostic admission, writing provenance
// only in a separate test-owned directory.
func finalDiagnosticCaptureTestConfigV2(t *testing.T, cfg *ResolvedConfig, plan *SetupPlan, stateDir string) *ResolvedConfig {
	t.Helper()
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(stateDir, "secrets"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writePublicJSON(filepath.Join(stateDir, "secrets", "roles.json"), roles); err != nil {
		t.Fatal(err)
	}
	provenance, _, _, _ := provisionalResumeTestContext(t)
	copy := *cfg
	copy.provisionalResume = &provisionalResumeState{Driver: provenance.provisionalResume.Driver}
	options := cliOptions{ProvisionalResume: true, PlanHash: plan.PlanHash, RunID: "synthetic-run", Name: "release-1.0", DiagnosticOutput: filepath.Join(t.TempDir(), "diagnostic"), OwnedRPCAuthority: "192.0.2.1:1234"}
	reader, retained, _, err := prepareTerminalDiagnosticConfig(t.Context(), &copy, stateDir, options)
	if err != nil || retained.PlanHash != plan.PlanHash || !finalDiagnosticCaptureV2(reader) {
		t.Fatalf("exact external diagnostic admission failed: %v", err)
	}
	return reader
}

// Both companion and dynamic-relay capture must retain the admitted driver
// identity while still refusing a missing original journal before any Rpc.
func TestFinalCaptureV2DiagnosticRetainsPlanWithoutGrantingStrictAcceptance(t *testing.T) {
	fixture := newRuntimeEvidenceProvisionV2TestFixture(t)
	lock := *fixture.cfg.Release
	lock.Repositories = maps.Clone(lock.Repositories)
	lock.Repositories["sn"] = map[string]any{"commit": strings.Repeat("ab", 20)}
	fixture.cfg.Release = &lock
	reader := finalDiagnosticCaptureTestConfigV2(t, fixture.cfg, fixture.plan, fixture.stateDir)
	before := validatorNamespaceTreeSnapshot(t, fixture.stateDir)
	retained, err := loadFinalCapturePlanV2(reader, fixture.stateDir)
	if err != nil || retained.PlanHash != fixture.plan.PlanHash || retained.ReleaseLockHash != fixture.plan.ReleaseLockHash {
		t.Fatalf("diagnostic relabeled its retained approval: %v", err)
	}
	terminal := &ScenarioObservation{Status: &DeploymentStatus{Contracts: &ContractView{}}}
	if _, err := captureFinalCompanionInputsV2(t.Context(), reader, fixture.stateDir, t.TempDir(), terminal); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("companion did not reach original journal custody: %v", err)
	}
	err = captureFinalValidatorRelayV2(t.Context(), reader, fixture.stateDir, []validatorpkg.ReleaseEvidenceV2CapturedPublication{{}}, func(context.Context, validatorpkg.ReleaseEvidenceV2CaptureSource, []byte) error {
		t.Fatal("missing original journal reached retained output")
		return nil
	})
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("relay did not reach original journal custody: %v", err)
	}
	if _, err := loadPersistedPlan(reader, fixture.stateDir); !errors.Is(err, errPersistedPlanIdentityMismatch) {
		t.Fatalf("external diagnostic granted strict release acceptance: %v", err)
	}
	if !reflect.DeepEqual(before, validatorNamespaceTreeSnapshot(t, fixture.stateDir)) || reader.provisionalResume.Record.FinalAcceptance || !reader.provisionalResume.Record.ReadOnly {
		t.Fatal("capture changed retained evidence or accepting authority")
	}
}

// Neither a nearby provisional command nor changed operational authority may
// borrow the external diagnostic's retained-plan reader.
func TestFinalCaptureV2DiagnosticRejectsScopeAndOperationalDrift(t *testing.T) {
	cfg, plan, stateDir, _, original := provisionalRuntimePlanFixture(t)
	reader := finalDiagnosticCaptureTestConfigV2(t, cfg, plan, stateDir)
	for _, field := range []string{"command", "schema", "read-only-audit", "read-only-record", "provisional", "acceptance", "approval", "config", "policy", "rpc", "record-config", "record-deployment", "record-release"} {
		changed := *reader
		provenance := *reader.provisionalResume
		record := *provenance.Record
		provenance.Record, changed.provisionalResume = &record, &provenance
		switch field {
		case "command":
			record.Command = "scenario"
		case "schema":
			record.Schema += "-unknown"
		case "read-only-audit":
			changed.readOnlyAudit = false
		case "read-only-record":
			record.ReadOnly = false
		case "provisional":
			record.Provisional = false
		case "acceptance":
			record.FinalAcceptance = true
		case "approval":
			record.PlanHash = "0x" + strings.Repeat("ac", 32)
		case "config":
			changed.ConfigHash = "0x" + strings.Repeat("bc", 32)
		case "policy":
			changed.PolicyHash = "0x" + strings.Repeat("cd", 32)
		case "rpc":
			changed.OperationalEVM += "/other"
		case "record-config":
			record.ConfigHash = "0x" + strings.Repeat("ba", 32)
		case "record-deployment":
			record.DeploymentID += "-foreign"
		case "record-release":
			record.ReleaseLockHash = "0x" + strings.Repeat("cb", 32)
		}
		if _, err := loadFinalCapturePlanV2(&changed, stateDir); err == nil {
			t.Errorf("%s drift borrowed diagnostic authority", field)
		}
	}
	after, err := os.ReadFile(filepath.Join(stateDir, "plan.json"))
	if err != nil || !bytes.Equal(original, after) {
		t.Fatal("diagnostic rejection rewrote original plan", err)
	}
}

// Pending relay preview provenance remains pending. Diagnostic admission can
// inspect its original approval without granting strict reconciliation.
func TestFinalCaptureV2DiagnosticReadsPendingRelayWithoutReconciliation(t *testing.T) {
	fixture, _ := provisionalRelayContinuationRuntimeTest(t)
	executor := fixture.executor
	// The owned-route policy needs a generated private address; this local
	// identity test never opens a connection to that synthetic authority.
	authority := net.JoinHostPort(net.IPv4(10, 255, 254, 253).String(), "1234")
	cfg, err := prepareOwnedRPCConfiguration(executor.cfg, authority)
	if err != nil {
		t.Fatal(err)
	}
	locked := *executor.plan.ValidatorEvidenceSource.ReleaseLock
	cfg.Release, cfg.provisionalResume = &locked, nil
	plan := *executor.plan
	plan.OwnedRPCAuthority = authority
	plan.ResolvedInputsHash, err = resolvedInputsHash(cfg)
	if err != nil {
		t.Fatal(err)
	}
	plan.ReleaseLockHash, err = canonicalHashHex(cfg.Release)
	if err != nil {
		t.Fatal(err)
	}
	provenance, _, _, _ := provisionalResumeTestContext(t)
	provenance.ConfigHash, provenance.Release, provenance.ChainID = cfg.ConfigHash, cfg.Release, cfg.ChainID
	source := plan
	source.PlanHash = plan.EvidenceRelayContinuation.SourcePlanHash
	options := cliOptions{ProvisionalCapture: true, PlanHash: source.PlanHash, OwnedRPCAuthority: authority, RelayEndBlock: 10000}
	if err := prepareProvisionalResume(t.Context(), provenance, executor.stateDir, "relay-continuation", options, &source); err != nil {
		t.Fatal(err)
	}
	continuation := *plan.EvidenceRelayContinuation
	continuation.ProvisionalCapture = &EvidenceRelayProvisionalCapture{Record: *provenance.provisionalResume.Record, RecordPath: provenance.provisionalResume.RecordPath, RecordSHA256: provenance.provisionalResume.RecordHash}
	plan.EvidenceRelayContinuation = &continuation
	plan.PlanHash, err = plan.hash()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(&plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(executor.stateDir, "plan.json"), append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	reader := finalDiagnosticCaptureTestConfigV2(t, cfg, &plan, executor.stateDir)
	before := validatorNamespaceTreeSnapshot(t, executor.stateDir)
	retained, err := loadFinalCapturePlanV2(reader, executor.stateDir)
	if err != nil || retained.PlanHash != plan.PlanHash || !reflect.DeepEqual(retained.EvidenceRelayContinuation.ProvisionalCapture, continuation.ProvisionalCapture) {
		t.Fatalf("diagnostic lost exact pending relay provenance: %v", err)
	}
	if _, err := loadPersistedPlan(reader, executor.stateDir); err == nil || !strings.Contains(err.Error(), "relay_capture_requires_strict_reconciliation") {
		t.Fatalf("diagnostic became strict reconciliation: %v", err)
	}
	if !reflect.DeepEqual(before, validatorNamespaceTreeSnapshot(t, executor.stateDir)) {
		t.Fatal("diagnostic mutated pending relay evidence")
	}
}

// Complete the actual revision planner before entering diagnostic admission;
// an unapproved fixture projection must not stand in for a persisted successor.
func finalCompanionDiagnosticHistoryTestV2(t *testing.T) (*runtimeEvidenceProvisionV2TestFixture, *SetupPlan, []JournalEntry, *ResolvedConfig) {
	t.Helper()
	fixture, _, entries, _ := newRuntimeEvidenceSetupClosedAttemptsV2Test(t)
	facts := fixture.plan.LiveFacts
	facts.DeployerNonce = fixture.plan.ValidatorEvidence.DeployerNonce + 1
	revised, err := buildPlanRevisionFromFacts(fixture.cfg, fixture.stateDir, fixture.plan, &facts, entries, time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(revised)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(fixture.stateDir, "plan.json"), append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	reader := finalDiagnosticCaptureTestConfigV2(t, fixture.cfg, revised, fixture.stateDir)
	return fixture, revised, entries, reader
}

// Original creation, anchor and all four activations survive a successor; six
// closed predecessor rows remain intact and cannot become transaction evidence.
func TestFinalCaptureV2DiagnosticSelectsOriginalCompanionHistory(t *testing.T) {
	fixture, revised, entries, reader := finalCompanionDiagnosticHistoryTestV2(t)
	names := []string{validatorEvidenceDeployActionID, validatorEvidenceAnchorActionID, runtimeEvidenceActivationActionId(1, 1), runtimeEvidenceActivationActionId(1, 2), runtimeEvidenceActivationActionId(2, 1), runtimeEvidenceActivationActionId(2, 2)}
	before := validatorNamespaceTreeSnapshot(t, fixture.stateDir)
	original := slices.Clone(entries)
	selected, err := finalCompanionActionJournalV2(reader, fixture.stateDir, revised, entries, names)
	if err != nil || len(selected) != len(names) {
		t.Fatalf("successor lost original companion receipts: %v", err)
	}
	for _, name := range names {
		entry := selected[name]
		if entry.PlanHash == revised.PlanHash || !revised.allowedPlanHashes()[entry.PlanHash] || entry.Sequence == 0 || entry != entries[entry.Sequence-1] {
			t.Fatalf("%s was relabeled or synthesized: %+v", name, entry)
		}
	}
	strict := *reader
	strict.provisionalResume = nil
	if _, err := finalCompanionActionJournalV2(&strict, fixture.stateDir, revised, entries, names); err == nil {
		t.Fatal("retained diagnostic relaxed the ordinary collector")
	}
	if !reflect.DeepEqual(original, entries) || !reflect.DeepEqual(before, validatorNamespaceTreeSnapshot(t, fixture.stateDir)) {
		t.Fatal("diagnostic rewrote historical rows, signatures or receipts")
	}
}

// Approval ancestry is necessary but insufficient: original archive, signed
// bytes, verification hash, deployment and one finalized owner must all agree.
func TestFinalCaptureV2DiagnosticRejectsChangedCompanionHistory(t *testing.T) {
	fixture, revised, entries, reader := finalCompanionDiagnosticHistoryTestV2(t)
	name := runtimeEvidenceActivationActionId(1, 1)
	selected, err := finalCompanionActionJournalV2(reader, fixture.stateDir, revised, entries, []string{name})
	if err != nil {
		t.Fatal(err)
	}
	finalized := selected[name]
	for _, field := range []string{"archive", "transaction", "postcondition", "unapproved", "deployment", "intent", "duplicate", "unfinished-attempt"} {
		changed := slices.Clone(entries)
		root := runtimeReservedCreationStateTest(t, fixture.stateDir, changed)
		switch field {
		case "archive":
			path := filepath.Join(root, "plans", stringsTrim0x(finalized.PlanHash)+".json")
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var source SetupPlan
			if err := json.Unmarshal(raw, &source); err != nil {
				t.Fatal(err)
			}
			source.Owner += "-changed"
			raw, err = json.Marshal(source)
			if err != nil || os.WriteFile(path, raw, 0o600) != nil {
				t.Fatal("could not tamper test archive", err)
			}
		case "transaction":
			if err := os.WriteFile(filepath.Join(root, "transactions", stringsTrim0x(finalized.TransactionHash)+".rlp"), []byte{0x80}, 0o600); err != nil {
				t.Fatal(err)
			}
		case "postcondition":
			for _, entry := range changed {
				if entry.PlanHash == finalized.PlanHash && entry.ActionID == name && entry.Stage == StageVerified {
					if err := os.WriteFile(filepath.Join(root, entry.PostconditionPath), []byte("{}\n"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
			}
		case "unapproved":
			changed[finalized.Sequence-1].PlanHash = "0x" + strings.Repeat("ad", 32)
		case "deployment":
			changed[finalized.Sequence-1].DeploymentID += "-foreign"
		case "intent":
			changed[finalized.Sequence-1].IntentHash = "0x" + strings.Repeat("ae", 32)
		case "duplicate":
			changed = append(changed, finalized)
		case "unfinished-attempt":
			found := false
			for index, entry := range changed {
				if entry.ActionID == name && entry.Stage == StageFailed && entry.PlanHash != finalized.PlanHash {
					changed[index].Stage, found = StageIntent, true
					break
				}
			}
			if !found {
				t.Fatal("fixture lacks its closed predecessor attempt")
			}
		}
		if _, err := finalCompanionActionJournalV2(reader, root, revised, changed, []string{name}); err == nil {
			t.Errorf("%s lost exact original receipt authority", field)
		}
	}
}
