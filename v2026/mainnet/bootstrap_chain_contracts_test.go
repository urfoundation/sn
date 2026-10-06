// Offline prerequisite regressions retain real public custody and canonical
// local-EVM receipts. Synthetic approvals never authorize production actions.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// Rebuild from the original independently signed input before each observation.
func bootstrapContractTestInspection(t *testing.T, f *evmCreateFixture) (bootstrapChainContractReadiness, []evmCreatePlan) {
	t.Helper()
	reserve, err := loadEvmCreatePlan(t.Context(), f.configPath)
	if err != nil {
		t.Fatal(err)
	}
	result, plans, err := prepareBootstrapContractReadiness(t.Context(), reserve, f.configPath)
	if err != nil {
		t.Fatal(err)
	}
	return result, plans
}

// Compare every original file and marker, including absent future destinations.
func bootstrapContractTestJournals(t *testing.T, directory string) map[string]string {
	t.Helper()
	result := map[string]string{}
	for i := range 8 {
		for _, suffix := range []string{"", ".lock"} {
			path := filepath.Join(directory, bootstrapContractStateFile(i)+suffix)
			raw, err := os.ReadFile(path)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				t.Fatal(err)
			}
			result[path] = string(raw)
		}
	}
	return result
}

// The public result must carry its complete seal without adding execution scope.
func bootstrapContractTestResult(t *testing.T, raw []byte) bootstrapChainContractReadiness {
	t.Helper()
	var result bootstrapChainContractReadiness
	if err := decodePlanJson(raw, &result); err != nil {
		t.Fatal(err)
	}
	claimed := result.ContentHash
	result.ContentHash = ""
	if claimed != rootObjectHash(result) || result.Signing || result.NetworkEffects || result.CurrentChainVerified || result.SafeAuthorityVerified || result.InstallationComplete || result.ActivationReady || result.SuccessorRequirements.Implemented {
		t.Fatalf("offline report lost its seal or granted authority: %+v", result)
	}
	result.ContentHash = claimed
	return result
}

// Planning exposes incomplete approval and the original cap before custody or
// imported transaction bytes exist. The unchanged preparation remains usable.
func TestBootstrapContractPlanPrecedesCustodyAndSigning(t *testing.T) {
	f := newBootstrapChainFixture(t)
	originalPlan := f.preparation.Plan.ContentHash
	prepared := mainnetNamespaceTest(t, f.config.RunDirectory)
	var stdout, stderr bytes.Buffer
	if code := runMain(f.storageContext(t.Context()), []string{"bootstrap-chain", "contract-plan", "--config", f.path}, &stdout, &stderr); code != 3 {
		t.Fatalf("contract plan exit %d: %s", code, stderr.String())
	}
	result := bootstrapContractTestResult(t, stdout.Bytes())
	if result.PlanHash != originalPlan || result.ContractPlanHash != f.contracts.config.Plan.hash() || result.Scope != "approved-plan" || result.Status != "blocked" || !result.PlanInspectionComplete || result.CustodyInspectionComplete || result.LocalPreparation != nil || result.RemainingOriginalAttempts != nil ||
		result.MinimumFreshInstallationAttempts != 9 || result.OriginalMaximumAttempts != 2 || !slices.Contains(result.Blockers, "NINE_FRESH_INSTALLATION_SENDS_EXCEED_ORIGINAL_ATTEMPT_CAP") || !slices.Contains(result.Blockers, "FULL_INSTALLATION_ACTION_APPROVAL_MISSING") ||
		len(result.Actions) != 9 || !result.Actions[0].SemanticsVerified || result.Actions[1].Approved || result.Actions[8].ExecutorImplemented || !reflect.DeepEqual(prepared, mainnetNamespaceTest(t, f.config.RunDirectory)) || len(f.contracts.counts) != 0 {
		t.Fatalf("plan concealed original scope or opened custody: %+v", result)
	}
	if prepared := f.result(t, "apply"); prepared.PlanHash != originalPlan || !prepared.LocalPreparationComplete {
		t.Fatal("missing anchor invalidated the original approved preparation")
	}
}

// Repeated command observations preserve imported original signature custody;
// no incomplete graph can be mistaken for a fresh execution allowance.
func TestBootstrapContractReadinessRetainsOriginalSignedCustody(t *testing.T) {
	f := newBootstrapChainFixture(t)
	f.result(t, "apply")
	if _, code, diagnostic := f.contracts.command("resume", "--signed-transaction", f.contracts.signedPath, "--signed-transaction-hash", f.contracts.signedHash); code != 0 {
		t.Fatal(diagnostic)
	}
	before := f.journals(t)
	var first []byte
	for range 2 {
		var stdout, stderr bytes.Buffer
		if code := f.command(t.Context(), "contract-readiness", &stdout, &stderr); code != 3 {
			t.Fatalf("readiness exit %d: %s", code, stderr.String())
		}
		result := bootstrapContractTestResult(t, stdout.Bytes())
		if !result.CustodyInspectionComplete || result.LocalPreparation == nil || result.LocalPreparation.ContractTransactionHash != f.contracts.tx.Hash().Hex() || result.Actions[0].TransactionHash != f.contracts.tx.Hash().Hex() || result.Actions[0].CustodyStatus != "retained-signature-reconciliation-pending" || result.Actions[0].Receipt != nil || result.RetainedAttempts != 0 || result.RemainingOriginalAttempts == nil || *result.RemainingOriginalAttempts != 2 || len(result.SuccessorRequirements.UnfinishedActions) != 9 {
			t.Fatalf("readiness changed retained liability: %+v", result)
		}
		if first != nil && !bytes.Equal(first, stdout.Bytes()) {
			t.Fatal("unchanged original custody produced different offline observations")
		}
		first = append([]byte(nil), stdout.Bytes()...)
	}
	if !maps.Equal(before, f.journals(t)) || len(f.contracts.counts) != 0 || len(f.contracts.writes) != 0 {
		t.Fatal("offline readiness changed custody or opened RPC")
	}
	f.result(t, "resume")
}

// Every original child remains mandatory. Restoring the exact missing bytes
// makes the same owner resumable; inspection never repairs or recreates them.
func TestBootstrapContractReadinessLeavesMissingOriginalCustodyUnresolved(t *testing.T) {
	f := newBootstrapChainFixture(t)
	f.result(t, "apply")
	original := f.journals(t)
	for _, path := range f.preparation.childPaths() {
		for _, suffix := range []string{"", ".lock"} {
			missing := path + suffix
			if err := os.Remove(missing); err != nil {
				t.Fatal(err)
			}
			before := f.journals(t)
			var stdout, stderr bytes.Buffer
			code := f.command(t.Context(), "contract-readiness", &stdout, &stderr)
			result := bootstrapContractTestResult(t, stdout.Bytes())
			if code != 1 || result.Status != "unresolved" || result.CustodyInspectionComplete || result.RemainingOriginalAttempts != nil || !maps.Equal(before, f.journals(t)) {
				t.Fatalf("missing %s became authority: %d %s %+v", missing, code, stderr.String(), result)
			}
			if err := os.WriteFile(missing, []byte(original[missing]), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	f.result(t, "resume")
}

// The whole nine-envelope reservation can be approved while its Safe semantics
// and full-graph attempt policy remain absent. Eight builders still pass.
func TestBootstrapContractPlanReportsNineActionCapBeforeSigning(t *testing.T) {
	f := newEvmEvidenceFixture(t)
	target := common.Address{19: 77}
	f.config.Plan.Actions = append(f.config.Plan.Actions, evmPhaseAction{Id: "evidence-anchor", Sender: f.config.Plan.Actions[0].Sender, Nonce: 8, To: &target, Data: "0x01", ValueWei: "0", Gas: 21000, FeeCapWei: "10", TipCapWei: "1"})
	f.config.Plan.MaximumTotalWei = "2244000000"
	f.publishConfig()
	result, plans := bootstrapContractTestInspection(t, f)
	if len(plans) != 8 || !result.Actions[8].Approved || result.Actions[8].SemanticsVerified || result.Actions[8].ExecutorImplemented || result.Actions[8].CustodyStatus != "sealed-reservation-only" || result.OriginalMaximumAttempts != 8 || result.MinimumFreshInstallationAttempts != 9 ||
		!slices.Contains(result.Blockers, "APPROVED_PREFIX_INITIAL_SENDS_EXCEED_ORIGINAL_ATTEMPT_CAP") || slices.Contains(result.Blockers, "FULL_INSTALLATION_ACTION_APPROVAL_MISSING") || !slices.Contains(result.Blockers, "SAFE_INNER_AUTHORITY_NONCE_AND_SIGNATURES_UNVERIFIED") || len(bootstrapContractTestJournals(t, f.config.Plan.RunDirectory)) != 0 || len(f.counts) != 0 {
		t.Fatalf("nine-action reservation acquired semantics or hid its cap: %+v", result)
	}
	for i := range plans {
		if !result.Actions[i].SemanticsVerified || plans[i].ActionIndex != i || plans[i].Config.Plan.hash() != f.config.Plan.hash() {
			t.Fatalf("approved executable prefix changed at %d", i)
		}
	}
	revised := copyEvmPhaseConfig(f.config)
	revised.Plan.MaximumAttempts = 9
	if err := revised.validateStructure(); err == nil {
		t.Fatal("readiness silently extended the original v1 attempt schema")
	}
}

// The new plan phase must reach the last implemented constructor; validating
// only reserve would hide a separately approved wrong evidence domain.
func TestBootstrapContractPlanChecksEveryApprovedProjection(t *testing.T) {
	f := newEvmEvidenceFixture(t)
	action := &f.config.Plan.Actions[7]
	action.Data = action.Data[:len(action.Data)-2] + "00"
	if action.Data == f.plan.Config.Plan.Actions[7].Data {
		action.Data = action.Data[:len(action.Data)-2] + "01"
	}
	f.publishConfig()
	if _, _, err := prepareBootstrapContractReadiness(t.Context(), f.plan, f.configPath); err == nil || !strings.Contains(err.Error(), "evidence") || len(bootstrapContractTestJournals(t, f.config.Plan.RunDirectory)) != 0 || len(f.counts) != 0 {
		t.Fatal("full-prefix inspection skipped the changed evidence constructor", err)
	}
}

// A missing future journal is unclaimed; a durable partial marker is unresolved.
// The already authenticated reserve receipt survives both observations verbatim.
func TestBootstrapContractReadinessDistinguishesUnclaimedAndPartialChild(t *testing.T) {
	f := newEvmVaultFixture(t)
	f.prepareVaultPrerequisite()
	result, plans := bootstrapContractTestInspection(t, f)
	before := bootstrapContractTestJournals(t, f.config.Plan.RunDirectory)
	if err := inspectBootstrapContractCustody(f.storage.Context, plans, &result); err != nil || !result.CustodyInspectionComplete || result.RetainedCompletedActions != 1 || result.Actions[1].CustodyStatus != "not-claimed" || result.Actions[0].ReceiptObservation != "retained" || !maps.Equal(before, bootstrapContractTestJournals(t, f.config.Plan.RunDirectory)) {
		t.Fatalf("unclaimed child changed its completed ancestor: %+v %v", result, err)
	}
	retainedReceipt := *result.Actions[0].Receipt
	reserveStore, err := openEvmActionStore(f.config, false, nil, f.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reserveStore.close() })
	reserveRecord, err := reserveStore.load()
	if err != nil {
		t.Fatal(err)
	}
	injected := errors.New("synthetic interruption after child marker durability")
	child, err := openEvmVaultActionStore(plans[1], reserveRecord, true, func(boundary string) error {
		if boundary == "marker-synced" {
			return injected
		}
		return nil
	}, f.storage.Context)
	if child != nil || !errors.Is(err, injected) {
		t.Fatal("child claim did not stop at its exact durable boundary", err)
	}
	if err := reserveStore.close(); err != nil {
		t.Fatal(err)
	}
	before = bootstrapContractTestJournals(t, f.config.Plan.RunDirectory)
	result, plans = bootstrapContractTestInspection(t, f)
	if err := inspectBootstrapContractCustody(f.storage.Context, plans, &result); err == nil || result.Status != "unresolved" || result.CustodyInspectionComplete || result.RemainingOriginalAttempts != nil || result.RetainedCompletedActions != 1 || result.Actions[0].Receipt == nil || *result.Actions[0].Receipt != retainedReceipt || result.Actions[1].CustodyStatus != "unresolved" || !maps.Equal(before, bootstrapContractTestJournals(t, f.config.Plan.RunDirectory)) {
		t.Fatalf("partial child repaired itself or erased its predecessor: %+v %v", result, err)
	}
	if resumed, code, diagnostic := f.command("resume", "--action", "vault-create"); code != 0 || resumed.Status != "signature-awaiting-import" {
		t.Fatalf("original owner could not recover its partial claim: %+v %d %s", resumed, code, diagnostic)
	}
}

// A fresh signature over a higher cap cannot reinterpret old markers or adopt
// their receipts. Restoring the exact approval preserves normal offline resume.
func TestBootstrapContractReadinessRequiresSuccessorForCapRevision(t *testing.T) {
	f := newEvmVaultFixture(t)
	f.prepareVaultPrerequisite()
	original := copyEvmPhaseConfig(f.config)
	before := bootstrapContractTestJournals(t, f.config.Plan.RunDirectory)
	f.config.Plan.MaximumAttempts++
	f.publishConfig()
	result, plans := bootstrapContractTestInspection(t, f)
	if err := inspectBootstrapContractCustody(f.storage.Context, plans, &result); err == nil || !strings.Contains(err.Error(), "another original approval") || result.CustodyInspectionComplete || result.RetainedCompletedActions != 0 || result.SuccessorRequirements.Implemented || !maps.Equal(before, bootstrapContractTestJournals(t, f.config.Plan.RunDirectory)) {
		t.Fatalf("new cap adopted old custody without a successor: %+v %v", result, err)
	}
	f.config = original
	f.publishConfig()
	result, plans = bootstrapContractTestInspection(t, f)
	if err := inspectBootstrapContractCustody(f.storage.Context, plans, &result); err != nil || result.RetainedCompletedActions != 1 || result.RetainedAttempts != 1 || result.RemainingOriginalAttempts == nil || *result.RemainingOriginalAttempts != 3 {
		t.Fatalf("restoring original approval lost completed work: %+v %v", result, err)
	}
}

// Eight real local-EVM canonical receipts form the adoptable historical prefix.
// Inspection keeps them intact and reports only the anchor as unfinished work.
func TestBootstrapContractReadinessReusesAuthenticatedPrefixWithoutReplay(t *testing.T) {
	f := newEvmEvidenceFixture(t)
	f.prepareEvidenceSigned()
	if _, code, diagnostic := f.command("resume", "--action", "evidence-create", "--online", "--submit"); code != 0 {
		t.Fatal(diagnostic)
	}
	completed, code, diagnostic := f.command("resume", "--action", "evidence-create", "--online")
	if code != 0 || completed.Status != "evidence-created-unanchored" {
		t.Fatalf("canonical evidence prerequisite: %+v %d %s", completed, code, diagnostic)
	}
	before := bootstrapContractTestJournals(t, f.config.Plan.RunDirectory)
	f.stateLock.Lock()
	counts, writes := maps.Clone(f.counts), len(f.writes)
	f.stateLock.Unlock()
	for range 2 {
		result, plans := bootstrapContractTestInspection(t, f)
		if err := inspectBootstrapContractCustody(f.storage.Context, plans, &result); err != nil || !result.CustodyInspectionComplete || result.RetainedCompletedActions != 8 || result.RetainedAttempts != 8 || result.RemainingOriginalAttempts == nil || *result.RemainingOriginalAttempts != 0 || !slices.Equal(result.SuccessorRequirements.UnfinishedActions, []string{"evidence-anchor"}) || result.Actions[8].Approved || result.InstallationComplete || result.ActivationReady {
			t.Fatalf("completed prefix became new sends or lost anchor scope: %+v %v", result, err)
		}
		for i := range 8 {
			var original evmActionRecord
			if err := json.Unmarshal([]byte(before[filepath.Join(f.config.Plan.RunDirectory, bootstrapContractStateFile(i))]), &original); err != nil {
				t.Fatal(err)
			}
			action := result.Actions[i]
			if action.CustodyStatus != "retained-complete" || action.ReceiptObservation != "retained" || action.JournalHash != original.ContentHash || action.CustodyHash != rootObjectHash(original) || action.TransactionHash != original.TransactionHash || action.Receipt == nil || *action.Receipt != *original.Receipt {
				t.Fatalf("authenticated action %d lost exact retained lineage: %+v", i, action)
			}
		}
	}
	f.stateLock.Lock()
	unchanged := maps.Equal(counts, f.counts) && len(f.writes) == writes
	f.stateLock.Unlock()
	if !maps.Equal(before, bootstrapContractTestJournals(t, f.config.Plan.RunDirectory)) || !unchanged {
		t.Fatal("inspection rewrote, reobserved or replayed completed custody")
	}
}

// A still-valid completed record cannot replace the exact predecessor already
// sealed by its child. Changed ancestor spend never refreshes the child budget.
func TestBootstrapContractReadinessRejectsChangedPredecessorLineage(t *testing.T) {
	f := newEvmVaultFixture(t)
	f.prepareVaultSigned()
	path := filepath.Join(f.config.Plan.RunDirectory, evmCreateStateFile)
	before := bootstrapContractTestJournals(t, f.config.Plan.RunDirectory)
	var record evmActionRecord
	if err := json.Unmarshal([]byte(before[path]), &record); err != nil {
		t.Fatal(err)
	}
	record.Attempts++
	record.ContentHash = ""
	record.ContentHash = rootObjectHash(record)
	bootstrapRootTestWrite(t, path, record)
	changed := bootstrapContractTestJournals(t, f.config.Plan.RunDirectory)
	result, plans := bootstrapContractTestInspection(t, f)
	if err := inspectBootstrapContractCustody(f.storage.Context, plans, &result); err == nil || !strings.Contains(err.Error(), "predecessor") || result.CustodyInspectionComplete || result.RetainedCompletedActions != 1 || result.RetainedAttempts != 2 || result.Actions[1].CustodyStatus != "unresolved" || !maps.Equal(changed, bootstrapContractTestJournals(t, f.config.Plan.RunDirectory)) {
		t.Fatalf("child adopted changed ancestor custody: %+v %v", result, err)
	}
	if err := os.WriteFile(path, []byte(before[path]), 0600); err != nil {
		t.Fatal(err)
	}
	if _, code, diagnostic := f.command("resume", "--action", "vault-create"); code != 0 {
		t.Fatal(diagnostic)
	}
}

// A recomputed record seal cannot substitute status one for constructor facts.
// The original bytes remain sufficient to resume after the invalid edit is gone.
func TestBootstrapContractReadinessChecksRetainedCompletionPostconditions(t *testing.T) {
	f := newEvmVaultFixture(t)
	f.prepareVaultPrerequisite()
	path := filepath.Join(f.config.Plan.RunDirectory, evmCreateStateFile)
	before := bootstrapContractTestJournals(t, f.config.Plan.RunDirectory)
	var record evmActionRecord
	if err := json.Unmarshal([]byte(before[path]), &record); err != nil {
		t.Fatal(err)
	}
	record.Receipt.GetterHash = "sha256:" + strings.Repeat("ab", 32)
	record.ContentHash = ""
	record.ContentHash = rootObjectHash(record)
	bootstrapRootTestWrite(t, path, record)
	corrupt := bootstrapContractTestJournals(t, f.config.Plan.RunDirectory)
	result, plans := bootstrapContractTestInspection(t, f)
	if err := inspectBootstrapContractCustody(f.storage.Context, plans, &result); err == nil || result.Actions[0].CustodyStatus != "unresolved" || result.RetainedCompletedActions != 0 || result.CustodyInspectionComplete || !maps.Equal(corrupt, bootstrapContractTestJournals(t, f.config.Plan.RunDirectory)) {
		t.Fatalf("status one bypassed exact constructor facts: %+v %v", result, err)
	}
	if err := os.WriteFile(path, []byte(before[path]), 0600); err != nil {
		t.Fatal(err)
	}
	if _, code, diagnostic := f.command("resume"); code != 0 {
		t.Fatal(diagnostic)
	}
}

// A simultaneous writer is refused synchronously. Closing it permits a fresh
// inspection, and cancellation/output errors leave every original owner usable.
func TestBootstrapContractReadinessReleasesLocksOnAllExits(t *testing.T) {
	f := newBootstrapChainFixture(t)
	f.result(t, "apply")
	before := f.journals(t)
	store, err := openEvmActionStore(f.contracts.config, false, nil, f.storageContext(t.Context()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.close() })
	var stdout, stderr bytes.Buffer
	if code := f.command(t.Context(), "contract-readiness", &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), "active custody owner") {
		t.Fatalf("reader crossed active custody writer: %d %s", code, stderr.String())
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
	if code := f.command(t.Context(), "contract-readiness", bootstrapRootFailedWriter{}, &stderr); code != 1 || !strings.Contains(stderr.String(), "output") {
		t.Fatalf("output failure was lost: %d %s", code, stderr.String())
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if code := f.command(ctx, "contract-readiness", io.Discard, &stderr); code == 0 || !maps.Equal(before, f.journals(t)) {
		t.Fatal("cancellation changed custody", code)
	}
	f.result(t, "resume")
}

// The new phase accepts no execution or signature flags and no changed accepted
// hash. Invalid admission produces no report and cannot inspect old-scope custody.
func TestBootstrapContractReadinessRejectsAuthorityAndScopeChanges(t *testing.T) {
	f := newBootstrapChainFixture(t)
	f.result(t, "apply")
	before := f.journals(t)
	for _, extra := range [][]string{{"--online"}, {"--submit"}, {"--rpc", "https://rpc.example"}, {"--signed-transaction", "/synthetic.bin"}, {"--accept-plan-hash", "sha256:" + strings.Repeat("ab", 32)}} {
		var stdout, stderr bytes.Buffer
		if code := f.command(t.Context(), "contract-readiness", &stdout, &stderr, extra...); code != 2 && code != 3 || stdout.Len() != 0 || !maps.Equal(before, f.journals(t)) {
			t.Fatalf("authority or scope override was admitted: %v %d %s", extra, code, stderr.String())
		}
	}
	legacy := f.config
	legacy.Schema, legacy.RootValidator = bootstrapChainConfigSchemaV2, nil
	bootstrapRootTestWrite(t, f.path, legacy)
	var stdout, stderr bytes.Buffer
	if code := runMain(f.storageContext(t.Context()), []string{"bootstrap-chain", "contract-plan", "--config", f.path}, &stdout, &stderr); code != 3 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "v3 scope") || !reflect.DeepEqual(before, f.journals(t)) {
		t.Fatalf("old preparation acquired inspection scope: %d %s", code, stderr.String())
	}
}
