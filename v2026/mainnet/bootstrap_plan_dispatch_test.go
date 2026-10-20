// Portable planning must reach the real public handlers before deployment
// custody exists. Retained observations and effect modes keep their own gate.
package main

import (
	"bytes"
	"context"
	"io"
	"maps"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Call the public dispatcher with no storage reference or synthetic host in
// its context. The fixture may supply only the explicit command input files.
func bootstrapPlanTestCommand(t *testing.T, args []string, expectedCode int) []byte {
	t.Helper()
	var output, diagnostic bytes.Buffer
	if code := runMain(t.Context(), args, &output, &diagnostic); code != expectedCode {
		t.Fatalf("portable %v exited %d, wanted %d: %s", args[:2], code, expectedCode, diagnostic.String())
	}
	return bytes.Clone(output.Bytes())
}

// Start from a complete valid review command so refusal proves its effect
// boundary, not merely the absence of unrelated mandatory configuration.
func bootstrapPlanTestRefuseEffects(t *testing.T, command []string) {
	t.Helper()
	for _, extra := range [][]string{
		{"--online"}, {"--submit"},
		{"--signed-transaction", "synthetic-public-signature", "--signed-transaction-hash", "sha256:" + strings.Repeat("3", 64)},
		{"--signature-file", "synthetic-public-signature", "--signature-sha256", "sha256:" + strings.Repeat("3", 64)},
		{"--durable-volumes", "/synthetic/unpaired-declaration.json"},
	} {
		args := append(append([]string(nil), command...), extra...)
		var output, diagnostic bytes.Buffer
		if code := runMain(t.Context(), args, &output, &diagnostic); code != 2 || output.Len() != 0 {
			t.Fatal("portable plan accepted effects or incomplete custody input", args, code, diagnostic.String())
		}
	}
}

// The current passive-root composition remains a blocked review even when all
// signed local inputs are valid. Neither plan may require deployment storage.
func TestBootstrapChainReviewPlansWithoutDurableDeclaration(t *testing.T) {
	f := newBootstrapRootPassiveFixture(t)
	before := mainnetNamespaceTest(t, f.config.RunDirectory)
	f.census.stateLock.Lock()
	reads := maps.Clone(f.census.methodCounts)
	f.census.stateLock.Unlock()
	for _, mode := range []string{"plan", "contract-plan"} {
		code := 0
		if mode == "contract-plan" {
			code = 3
		}
		args := []string{"bootstrap-chain", mode, "--config", f.path}
		raw := bootstrapPlanTestCommand(t, args, code)
		if mode == "plan" {
			var result bootstrapChainPlan
			if err := decodePlanJson(raw, &result); err != nil || !reflect.DeepEqual(result, f.preparation.Plan) || result.NativeSigning || result.NetworkEffects || result.ActivationReady {
				t.Fatal("portable chain plan changed its accepted scope or granted authority", err)
			}
		} else {
			result := bootstrapContractTestResult(t, raw)
			if result.PlanHash != f.preparation.Plan.ContentHash || result.Scope != "approved-plan" || result.Status != "blocked" || !result.PlanInspectionComplete || result.CustodyInspectionComplete || result.LocalPreparation != nil || result.RemainingOriginalAttempts != nil {
				t.Fatal("portable contract plan acquired retained custody or installation authority", result)
			}
		}
		bootstrapPlanTestRefuseEffects(t, args)
		if !reflect.DeepEqual(before, mainnetNamespaceTest(t, f.config.RunDirectory)) {
			t.Fatal("portable review changed custody bytes, markers or physical checkpoints", mode)
		}
	}
	f.census.stateLock.Lock()
	unchanged := maps.Equal(reads, f.census.methodCounts)
	f.census.stateLock.Unlock()
	f.contracts.stateLock.Lock()
	untouched := len(f.contracts.counts) == 0 && len(f.contracts.writes) == 0
	f.contracts.stateLock.Unlock()
	if !unchanged || !untouched {
		t.Fatal("portable chain review invoked a configured RPC")
	}
}

// Standalone aliases must preserve the existing pure handler's exact output.
// Their signed configurations do not make a plan a custody or service command.
func TestBootstrapStandaloneReviewPlansWithoutDurableDeclaration(t *testing.T) {
	f := newBootstrapRootPassiveFixture(t)
	legacy := newRootServiceRuntimeFixture(t)
	passive := bootstrapRootTestWrite(t, filepath.Join(filepath.Dir(f.path), "passive-plan-runtime.json"), rootPassiveRuntimeConfig{
		Schema: rootPassiveRuntimeSchema, Root: f.config.Root, Role: *f.config.RootValidator,
	})
	unsigned := copyEvmPhaseConfig(f.contracts.config)
	unsigned.Signature = ""
	draft := bootstrapRootTestWrite(t, filepath.Join(filepath.Dir(f.path), "unsigned-contract-plan.json"), unsigned)
	before := mainnetNamespaceTest(t, f.config.RunDirectory)
	legacyBefore := mainnetNamespaceTest(t, legacy.root.config.RunDirectory)
	f.census.stateLock.Lock()
	reads := maps.Clone(f.census.methodCounts)
	f.census.stateLock.Unlock()
	legacy.submission.receipt.stateLock.Lock()
	legacyReads := maps.Clone(legacy.submission.receipt.counts)
	legacy.submission.receipt.stateLock.Unlock()
	for _, item := range []struct {
		args    []string
		handler func(context.Context, []string, io.Writer, io.Writer) int
		prefix  int
	}{
		{args: []string{"bootstrap", "plan", "--config", f.root.configPath}, handler: runBootstrapRootCommand},
		{args: []string{"root-passive-service", "plan", "--config", passive.Path}, handler: runRootPassiveServiceCommand, prefix: 1},
		{args: []string{"root-service", "plan", "--config", legacy.input.Path}, handler: runRootServiceCommand, prefix: 1},
		{args: []string{"bootstrap-contracts", "plan", "--config", f.contracts.configPath}, handler: runBootstrapContractCommand},
		{args: []string{"bootstrap-contracts", "preview", "--config", draft.Path}, handler: runBootstrapContractCommand},
	} {
		var expected, diagnostic bytes.Buffer
		if code := item.handler(t.Context(), item.args[item.prefix:], &expected, &diagnostic); code != 0 || expected.Len() == 0 {
			t.Fatal("portable handler fixture failed before dispatch", item.args[:2], code, diagnostic.String())
		}
		actual := bootstrapPlanTestCommand(t, item.args, 0)
		bootstrapPlanTestRefuseEffects(t, item.args)
		if !bytes.Equal(actual, expected.Bytes()) || !reflect.DeepEqual(before, mainnetNamespaceTest(t, f.config.RunDirectory)) || !reflect.DeepEqual(legacyBefore, mainnetNamespaceTest(t, legacy.root.config.RunDirectory)) {
			t.Fatal("standalone plan changed the reviewed output or touched custody", item.args[:2])
		}
	}
	f.census.stateLock.Lock()
	unchanged := maps.Equal(reads, f.census.methodCounts)
	f.census.stateLock.Unlock()
	legacy.submission.receipt.stateLock.Lock()
	legacyUnchanged := maps.Equal(legacyReads, legacy.submission.receipt.counts)
	legacy.submission.receipt.stateLock.Unlock()
	f.contracts.stateLock.Lock()
	untouched := len(f.contracts.counts) == 0 && len(f.contracts.writes) == 0
	f.contracts.stateLock.Unlock()
	if !unchanged || !legacyUnchanged || !untouched || len(legacy.submission.sent()) != 0 {
		t.Fatal("standalone plan reached observation or submission")
	}
}

// The complete signed declaration graph can be reviewed before its original
// transaction journals exist. Its output still proves no installation state.
func TestBootstrapContractRoleReviewWithoutDurableDeclaration(t *testing.T) {
	f, _ := newBootstrapContractRoleFixture(t)
	before := mainnetNamespaceTest(t, f.config.RunDirectory)
	f.contracts.stateLock.Lock()
	reads := maps.Clone(f.contracts.counts)
	f.contracts.stateLock.Unlock()
	args := []string{"bootstrap-chain", "contract-role-plan", "--config", f.path}
	raw := bootstrapPlanTestCommand(t, args, 0)
	bootstrapPlanTestRefuseEffects(t, args)
	var result bootstrapContractRolePlan
	if err := decodePlanJson(raw, &result); err != nil || result.PreparationHash != f.preparation.Plan.ContentHash || !result.DeclarationsVerified || len(result.Validators) != 2 ||
		result.CanonicalReceiptsVerified || result.CurrentStateVerified || result.InstallationComplete || result.ActivationReady || result.NetworkEffects {
		t.Fatal("portable role review changed declarations or acquired live authority", err, result)
	}
	claimed := result.ContentHash
	result.ContentHash = ""
	if claimed != rootObjectHash(result) || !reflect.DeepEqual(before, mainnetNamespaceTest(t, f.config.RunDirectory)) {
		t.Fatal("portable role review lost its seal or changed custody")
	}
	f.contracts.stateLock.Lock()
	unchanged := maps.Equal(reads, f.contracts.counts)
	f.contracts.stateLock.Unlock()
	if !unchanged {
		t.Fatal("portable role review reached RPC")
	}
}

// Exact mode names are deliberate: retained read-only inspections still own
// custody, and unknown or malformed modes cannot inherit a planning exemption.
func TestBootstrapCustodyCommandsStillRequireDurableDeclaration(t *testing.T) {
	for _, item := range []struct {
		command string
		modes   []string
	}{
		{command: "bootstrap-chain", modes: []string{"", "apply", "resume", "readiness", "contract-readiness", "contract-successor-plan", "contract-successor-preview", "contract-successor-prepare", "contract-successor-resume", "contract-successor-execution-readback", "trim-plan", "trim-apply", "trim-resume", "trim-export", "trim-import", "trim-import-reply", "trim-reconcile", "trim-submit-plan", "trim-submit", "plan-extra"}},
		{command: "bootstrap", modes: []string{"", "apply", "resume", "plan-extra"}},
		{command: "bootstrap-contracts", modes: []string{"", "apply", "resume", "plan-extra"}},
		{command: "root-service", modes: []string{"", "prepare", "status", "run", "activate", "prepare-recovery", "run-recovery", "plan-extra"}},
		{command: "root-passive-service", modes: []string{"", "run", "plan-extra"}},
		{command: "activate-root-passive", modes: []string{"claim", "install", "admit", "start", "resume", "status"}},
	} {
		for _, mode := range item.modes {
			args := []string{item.command}
			if mode != "" {
				args = append(args, mode)
			}
			var output, diagnostic bytes.Buffer
			if code := runMain(t.Context(), args, &output, &diagnostic); code != 2 || output.Len() != 0 || !strings.Contains(diagnostic.String(), "durable custody declaration") {
				t.Fatal("retained or effect command bypassed durable admission", args, code, diagnostic.String())
			}
		}
	}
}
