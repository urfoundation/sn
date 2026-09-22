// Fleet renewal must keep the same retained approval and runtime capability
// authority through planning, pre-apply doctor and restart, without activating
// a plan early or admitting a changed source journal.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// Planning binds the active source; apply separately binds the reviewed
// successor. Neither form may acquire topology or route substitution flags.
func TestProvisionalFleetRenewalRequiresExactScope(t *testing.T) {
	hash := "0x" + strings.Repeat("bd", 32)
	planning := []string{"fleet-renew", "--provisional-resume", "--plan-hash", hash, "--renewal-valid-from-epoch", "41"}
	apply := []string{"fleet-renew", "--provisional-resume", "--apply", "--plan-hash", hash, "--renewal-plan", "/fixture/renewal.json"}
	authority := net.JoinHostPort(net.IPv4(10, 231, 0, 51).String(), "29944")
	for _, args := range [][]string{planning, apply} {
		command, options, err := parseCLI(append(append([]string{}, args...), "--owned-rpc-authority", authority))
		if err != nil || executableAttestationModeForCommand(command, options) != executableAttestationProvisionalResume {
			t.Fatalf("exact renewal scope rejected: %v: %v", args, err)
		}
	}
	if err := validateOwnedRPCOptions("coordinator-repair", cliOptions{Apply: true, ProvisionalResume: true, PlanHash: hash, OwnedRPCAuthority: authority}); err != nil {
		t.Fatal("adjacent coordinator repair lost the shared owned-route authority", err)
	}
	for _, extra := range [][]string{
		{"--plan-hash", ""}, {"--plan-hash", "approximate"}, {"--apply"},
		{"--detach"}, {"--prepare-only"}, {"--then-release-candidate"},
		{"--name", "release-candidate"}, {"--manifest", "https://review.example/manifest"},
		{"--strict-history-adoption", "/fixture/history.json"},
		{"--provisional-rpc-authority", "192.0.2.81:19944"},
		{"--renewal-plan", "/fixture/renewal.json"},
	} {
		if _, _, err := parseCLI(append(append([]string{}, planning...), extra...)); err == nil {
			t.Errorf("planning admitted incompatible scope %v", extra)
		}
	}
	_, strict, err := parseCLI([]string{"fleet-renew", "--apply", "--plan-hash", hash, "--renewal-plan", "/fixture/renewal.json"})
	if err != nil || executableAttestationModeForCommand("fleet-renew", strict) != executableAttestationLockedSource {
		t.Fatal("ordinary renewal lost strict executable admission", err)
	}
}

// Reproduce the actual failing finalized-manager call after source admission.
// The strict caller retains its rejection and planning creates no journal.
func TestProvisionalFleetRenewalPlannerAuthenticatesSuccessor(t *testing.T) {
	authority := net.JoinHostPort(net.IPv4(10, 231, 0, 52).String(), "29944")
	cfg, plan, stateDir, options, before := provisionalRuntimePlanFixtureWithOwnedRpc(t, authority)
	options.Apply, options.Name, options.RenewalValidFrom = false, "", 41
	strict := &SubstrateManager{cfg: cfg, chain: provisionalRuntimeChainTest(t, cfg)}
	if _, _, _, err := strict.finalizedManagerContext(t.Context()); err == nil {
		t.Fatal("ordinary renewal accepted an unreviewed runtime")
	}
	reader, err := prepareProvisionalRetainedReader(t.Context(), cfg, stateDir, "fleet-renew", options)
	if err != nil {
		t.Fatal(err)
	}
	manager := &SubstrateManager{cfg: reader, chain: provisionalRuntimeChainTest(t, reader)}
	view, hash, number, err := manager.finalizedManagerContext(t.Context())
	if err != nil || hash != (types.Hash{2}) || number != 200 || view.chain.Runtime.SpecVersion != types.U32(provisionalRuntimeSuccessorTestSpec) {
		t.Fatalf("retained renewal failed current capability authentication: hash=%s number=%d err=%v", hash.Hex(), number, err)
	}
	record := reader.provisionalResume.Record
	if reader == cfg || reader.provisionalResume == cfg.provisionalResume || provisionalResumeEnabled(cfg) || !reader.readOnlyAudit || !record.ReadOnly || record.Command != "fleet-renew" || record.PlanHash != plan.PlanHash || record.ReleaseLockHash != plan.ReleaseLockHash || record.FinalAcceptance {
		t.Fatal("planning changed or promoted retained authority")
	}
	if err := validateOwnedRPCPlan(reader, plan); err != nil || verificationSubstrateEndpoint(reader) != "ws://"+authority || verificationEVMEndpoint(reader) != "http://"+authority || configuredEVMRequestsPerMinute(reader, reader.OperationalEVM) != 0 {
		t.Fatal("planning changed the approved unpaced route", err)
	}
	if after, err := os.ReadFile(filepath.Join(stateDir, "plan.json")); err != nil || !bytes.Equal(before, after) {
		t.Fatal("planning changed the active approval", err)
	}
	for _, name := range []string{"journal.jsonl", "deployment.lock", "supervisor.json", "runtime-config-manifest.json", "plans"} {
		if _, err := os.Lstat(filepath.Join(stateDir, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("planning changed deployment path %s: %v", name, err)
		}
	}
}

// Reject operational drift before persisting compatibility authority; a
// retained observation can never authorize an approximate or different plan.
func TestProvisionalFleetRenewalPlannerRejectsChangedAuthority(t *testing.T) {
	for _, fault := range []string{"plan", "config", "route", "allowance", "driver"} {
		cfg, _, stateDir, options, before := provisionalRuntimePlanFixture(t)
		options.Apply, options.Name = false, ""
		switch fault {
		case "plan":
			options.PlanHash = "0x" + strings.Repeat("ea", 32)
		case "config":
			cfg.ConfigHash += "changed"
		case "route":
			cfg.OperationalEVM += "/unapproved"
		case "allowance":
			cfg.MaximumTAORao++
		case "driver":
			cfg.provisionalResume = nil
		}
		if _, err := prepareProvisionalRetainedReader(t.Context(), cfg, stateDir, "fleet-renew", options); err == nil {
			t.Errorf("%s changed authority was accepted", fault)
		}
		if _, err := os.Lstat(filepath.Join(stateDir, "provisional-resumes")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s persisted unadmitted provenance: %v", fault, err)
		}
		if after, err := os.ReadFile(filepath.Join(stateDir, "plan.json")); err != nil || !bytes.Equal(before, after) {
			t.Fatalf("%s changed active approval: %v", fault, err)
		}
	}
}

// Build a complete renewal under a generated-code lock. All source, role and
// transaction facts are synthetic and remain local to this test directory.
func provisionalFleetRenewalFixture(t *testing.T) (fleetRenewalTestFixture, *SetupPlan, cliOptions, []JournalEntry) {
	t.Helper()
	fixture := newFleetRenewalTestFixture(t)
	fixture.cfg.Release = testReleaseLockFixture(t)
	roles, err := derivePublicRoles(fixture.cfg)
	if err != nil {
		t.Fatal(err)
	}
	fixture.base, err = buildPlan(fixture.cfg, testSetupFacts(), roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	fixture.renewal.SourcePlanHash = fixture.base.PlanHash
	plan, err := appendFleetRenewalPlan(fixture.base, fixture.renewal)
	if err != nil {
		t.Fatal(err)
	}
	plan, err = bindFleetRenewalRuntimeIdentity(fixture.cfg, plan)
	if err != nil {
		t.Fatal(err)
	}
	provenance, _, _, _ := provisionalResumeTestContext(t)
	fixture.cfg.provisionalResume = provenance.provisionalResume
	raw, err := json.Marshal(fixture.base)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture.stateDir, "plan.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	options := cliOptions{Apply: true, ProvisionalResume: true, PlanHash: plan.PlanHash, RenewalPlan: "/fixture/renewal.json"}
	return fixture, plan, options, []JournalEntry{{EntryHash: fixture.renewal.JournalHash}}
}

// Readiness must re-open the exact reviewed successor before it is active.
// Re-entry retains the same actions and uses a new authenticated invocation.
func TestProvisionalFleetRenewalApplySharesRetainedDoctorAdmission(t *testing.T) {
	fixture, plan, options, entries := provisionalFleetRenewalFixture(t)
	cfg, stateDir := fixture.cfg, fixture.stateDir
	before, err := os.ReadFile(filepath.Join(stateDir, "plan.json"))
	if err != nil {
		t.Fatal(err)
	}
	authenticate := func(ctx context.Context, observed *ResolvedConfig, mode executableAttestationMode) error {
		if mode != executableAttestationProvisionalResume {
			return errors.New("unexpected executable admission")
		}
		observed.provisionalResume = &provisionalResumeState{Driver: cfg.provisionalResume.Driver}
		return ctx.Err()
	}
	firstRecord := ""
	for range 2 {
		if err := prepareFleetRenewalInvocation(t.Context(), cfg, stateDir, options, fixture.base, plan, entries); err != nil {
			t.Fatal(err)
		}
		record := cfg.provisionalResume.Record
		if record.ReadOnly || record.FinalAcceptance || record.Command != "fleet-renew" || record.PlanHash != plan.PlanHash || record.ReleaseLockHash != fixture.base.ReleaseLockHash || cfg.provisionalResume.RecordPath == firstRecord {
			t.Fatal("repair lost exact independent invocation authority")
		}
		firstRecord = cfg.provisionalResume.RecordPath
		if err := validateProvisionalDoctorReleaseLock(t.Context(), cfg, &doctorPlanBudget{Plan: plan, StateDir: stateDir}, authenticate); err != nil {
			t.Fatal("doctor restored strict or predecessor-only admission", err)
		}
		manager := &SubstrateManager{cfg: cfg, chain: provisionalRuntimeChainTest(t, cfg)}
		if _, _, _, err := manager.finalizedManagerContext(t.Context()); err != nil {
			t.Fatal("repair lost planner's runtime capability admission", err)
		}
	}
	if after, err := os.ReadFile(filepath.Join(stateDir, "plan.json")); err != nil || !bytes.Equal(before, after) {
		t.Fatal("repair activated the approval before readiness", err)
	}
	archive := filepath.Join(stateDir, "plans", stringsTrim0x(plan.PlanHash)+".json")
	if err := os.WriteFile(archive, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateProvisionalDoctorReleaseLock(t.Context(), cfg, &doctorPlanBudget{Plan: plan, StateDir: stateDir}, authenticate); err == nil {
		t.Fatal("doctor accepted a missing reviewed approval from an in-memory record")
	}
}

// An exact source checkpoint is mandatory even with a compatible runtime and
// correct executable; failures must occur before publishing repair authority.
func TestProvisionalFleetRenewalRejectsForeignSourceBeforeAdmission(t *testing.T) {
	fixture, plan, options, entries := provisionalFleetRenewalFixture(t)
	for _, fault := range []string{"approval", "checkpoint", "journal advance", "read-only"} {
		changedCfg, changedOptions := *fixture.cfg, options
		changedEntries := append([]JournalEntry{}, entries...)
		switch fault {
		case "approval":
			changedOptions.PlanHash = "0x" + strings.Repeat("bc", 32)
		case "checkpoint":
			changedEntries = nil
		case "journal advance":
			changedEntries = append(changedEntries, JournalEntry{PlanHash: fixture.base.PlanHash, ActionID: "foreign-action"})
		case "read-only":
			changedCfg.readOnlyAudit = true
		}
		if err := prepareFleetRenewalInvocation(t.Context(), &changedCfg, fixture.stateDir, changedOptions, fixture.base, plan, changedEntries); err == nil {
			t.Errorf("%s admitted foreign repair authority", fault)
		}
		for _, name := range []string{"plans", "provisional-resumes"} {
			if _, err := os.Lstat(filepath.Join(fixture.stateDir, name)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("%s wrote %s before source admission: %v", fault, name, err)
			}
		}
	}
}
