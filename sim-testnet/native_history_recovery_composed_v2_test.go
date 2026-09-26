//go:build linux || darwin

// These synthetic boundaries exercise the actual launch owners and the signed
// old-config/new-plan seam. They never start a supervisor or send chain writes.
package main

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

func TestNativeHistoryRecoveryV2MigratedSealedPredecessor(t *testing.T) {
	x := newNativeHistoryRecoveryConfiguredTestV2(t, campaignConfigMigrationOldTestConfig)
	sealEarlyNativeRecoveryTestV2(t, x)
	sealed, err := os.ReadFile(x.p.Terminal.Path)
	if err != nil {
		t.Fatal(err)
	}
	cfg, request, current, _, _ := prepareCampaignConfigMigrationTest(t, x.f)
	if _, _, _, err := authenticateNativeRecoveryTerminalV2(t.Context(), cfg, x.f.stateDir, current, x.p.Terminal.Path); err == nil {
		t.Fatal("unsigned config migration admitted the old failed predecessor")
	}
	receiptPath, receiptBytes := signCampaignConfigMigrationTest(t, x.f, *request, current)
	x.p.Terminal, x.p.Result, x.p.RunId, err = authenticateNativeRecoveryTerminalV2(t.Context(), cfg, x.f.stateDir, current, x.p.Terminal.Path)
	if err != nil {
		t.Fatal("signed config migration lost the genuine sealed partial predecessor", err)
	}
	sourceCfg, source, pin, err := nativeRecoveryPredecessorScopeV2(t.Context(), cfg, x.f.stateDir, current)
	if err != nil || sourceCfg.ConfigHash != x.f.cfg.ConfigHash || source.PlanHash != x.f.plan.PlanHash || sourceCfg.Config.Scenarios.Adversaries.RequestTimeoutMilliseconds != 10_000 || pin.PlanHash != current.PlanHash || pin.SourcePlanHash != source.PlanHash {
		t.Fatal("native recovery substituted historical config or predecessor identity", err)
	}
	x.p.ConfigMigration, x.p.BasePlanHash, x.p.ConfigHash = pin, current.PlanHash, cfg.ConfigHash
	for index := range x.p.Validators {
		x.p.Validators[index].Adoption.ApprovedPlanHash = current.PlanHash
	}
	x.p.PlanHash, err = x.p.hash()
	if err != nil {
		t.Fatal(err)
	}
	if err := validateNativeHistoryRecoveryPlanV2(t.Context(), cfg, x.f.stateDir, current, x.h, x.p, true); err != nil {
		t.Fatal("composed approval lost exact selected generation and old failed source", err)
	}
	recovery, err := publishNativeHistoryRecoveryV2(t.Context(), cfg, x.f.stateDir, current, x.f.roles, x.h, x.p, func(context.Context, *nativeHistoryRecoveryPlanV2) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := readNativeHistoryRecoveryHandoffV2(t.Context(), cfg, x.f.stateDir, current, x.h, recovery.Path, recovery.SHA256); err != nil {
		t.Fatal("composed signed restart lost migration receipt", err)
	}
	for _, change := range []func(*nativeHistoryRecoveryPlanV2){
		func(p *nativeHistoryRecoveryPlanV2) { p.ConfigMigration = nil },
		func(p *nativeHistoryRecoveryPlanV2) { p.ConfigMigration.ReceiptHash = "0x" + strings.Repeat("cd", 32) },
		func(p *nativeHistoryRecoveryPlanV2) { p.ConfigMigration.SourcePlanHash = current.PlanHash },
	} {
		raw, _ := json.Marshal(x.p)
		var changed nativeHistoryRecoveryPlanV2
		if err := json.Unmarshal(raw, &changed); err != nil {
			t.Fatal(err)
		}
		change(&changed)
		changed.PlanHash, _ = changed.hash()
		if err := validateNativeHistoryRecoveryPlanV2(t.Context(), cfg, x.f.stateDir, current, x.h, &changed, true); err == nil {
			t.Fatal("rehashing replaced independent migration authority")
		}
	}
	if err := os.WriteFile(receiptPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readNativeHistoryRecoveryHandoffV2(t.Context(), cfg, x.f.stateDir, current, x.h, recovery.Path, recovery.SHA256); err == nil {
		t.Fatal("native approval bypassed a missing signed config receipt")
	}
	if err := os.WriteFile(receiptPath, receiptBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(x.p.Terminal.Path)
	if err != nil || !bytes.Equal(sealed, after) {
		t.Fatal("migration rewrote sealed historical source", err)
	}
	for path, original := range x.original {
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(original, got) {
			t.Fatal("migration changed generation-2 custody", path, err)
		}
	}
}

func TestNativeHistoryRecoveryV2FreshLaunchRejectsBeforeMigration(t *testing.T) {
	cfg, stateDir, plan, roles, bins, marker := validatorNamespaceLaunchBoundaryFixture(t)
	cfg.nativeHistoryRecoveryV2 = &nativeHistoryRecoverySelectionV2{path: "/synthetic/unissued-handoff", sha256: "0x" + strings.Repeat("ba", 32)}
	before := validatorNamespaceTreeSnapshot(t, stateDir)
	err := LaunchDeployment(t.Context(), cfg, stateDir, plan, roles, nil, bins, false)
	if _, markerErr := os.Stat(marker); !errors.Is(markerErr, os.ErrNotExist) {
		t.Fatal("unapproved native selection reached the real migration executable", markerErr)
	}
	if err == nil || !strings.Contains(err.Error(), "native recovery") {
		t.Fatal("fresh launch ignored explicit native selection", err)
	}
	if !reflect.DeepEqual(before, validatorNamespaceTreeSnapshot(t, stateDir)) {
		t.Fatal("native selection refusal changed retained state")
	}
}

func TestNativeHistoryRecoveryV2RetainedLaunchRequiresGeneration(t *testing.T) {
	for _, explicit := range []bool{true, false} {
		f := newRuntimeEvidenceProvisionV2TestFixture(t)
		specs := []ProcessSpec{{ID: "validator-1", Role: "validator", Args: []string{"__validator"}}}
		if explicit {
			f.cfg.nativeHistoryRecoveryV2 = &nativeHistoryRecoverySelectionV2{path: "/synthetic/unissued-handoff", sha256: "0x" + strings.Repeat("ba", 32)}
		} else {
			specs[0].Args = append(specs[0].Args, "--native-history-recovery=/synthetic/unissued.json", "--native-history-recovery-sha256=sha256:"+strings.Repeat("bb", 32))
		}
		before := validatorNamespaceTreeSnapshot(t, f.stateDir)
		if err := attachRetainedProvisionalProcessHandoff(t.Context(), f.cfg, f.stateDir, f.plan, f.roles, specs); err == nil || !strings.Contains(err.Error(), "native recovery") {
			t.Fatal("retained launch ignored missing native generation authority", err)
		}
		if !reflect.DeepEqual(before, validatorNamespaceTreeSnapshot(t, f.stateDir)) {
			t.Fatal("retained refusal created another startup authority")
		}
	}
}

func TestNativeHistoryRecoveryV2LiveEntryRejectsIgnoredSelection(t *testing.T) {
	f := newProvisionalStartupRetentionTestFixture(t)
	f.cfg.nativeHistoryRecoveryV2 = &nativeHistoryRecoverySelectionV2{path: "/synthetic/unissued-handoff", sha256: "0x" + strings.Repeat("ba", 32)}
	path := filepath.Join(filepath.Dir(f.cfg.provisionalResume.RecordPath), "live-topology.json")
	if _, err := prepareProvisionalLiveTopology(t.Context(), f.cfg, f.stateDir, "resume", nil); err == nil || !strings.Contains(err.Error(), "native recovery") {
		t.Fatal("live topology adoption ignored explicit native selection", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("live adoption wrote provenance before native selection refusal", err)
	}
}

func TestNativeHistoryRecoveryV2LiveArgumentsRequireExactRetainedSelection(t *testing.T) {
	x := newNativeHistoryRecoveryTestV2(t)
	f := x.f
	receipt, err := publishNativeHistoryRecoveryV2(t.Context(), f.cfg, f.stateDir, f.plan, f.roles, x.h, x.p, func(context.Context, *nativeHistoryRecoveryPlanV2) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	f.cfg.nativeHistoryRecoveryV2 = &nativeHistoryRecoverySelectionV2{path: receipt.Path, sha256: receipt.SHA256}
	specs := []ProcessSpec{{ID: "validator-1", Role: "validator", Args: []string{"__validator", "--config=" + x.p.Validators[0].ConfigPath}}, {ID: "validator-2", Role: "validator", Args: []string{"__validator", "--config=" + x.p.Validators[1].ConfigPath}}}
	if err := attachNativeHistoryRecoveryV2(t.Context(), f.cfg, f.stateDir, f.plan, x.h, specs); err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(specs)
	if err := checkNativeHistoryRecoveryLiveArgumentsV2(t.Context(), f.cfg, f.stateDir, f.plan, x.h, specs); err != nil {
		t.Fatal("unchanged explicit native selection could not adopt live topology", err)
	}
	f.cfg.nativeHistoryRecoveryV2 = nil
	if err := checkNativeHistoryRecoveryLiveArgumentsV2(t.Context(), f.cfg, f.stateDir, f.plan, x.h, specs); err != nil {
		t.Fatal("unchanged retained native pins could not adopt live topology", err)
	}
	f.cfg.nativeHistoryRecoveryV2 = &nativeHistoryRecoverySelectionV2{path: receipt.Path, sha256: receipt.SHA256}
	for _, missing := range []bool{true, false} {
		var changed []ProcessSpec
		if err := json.Unmarshal(before, &changed); err != nil {
			t.Fatal(err)
		}
		if missing {
			changed[0].Args = changed[0].Args[:2]
		} else {
			changed[0].Args[len(changed[0].Args)-1] = "--native-history-recovery-sha256=sha256:" + strings.Repeat("cb", 32)
		}
		if err := checkNativeHistoryRecoveryLiveArgumentsV2(t.Context(), f.cfg, f.stateDir, f.plan, x.h, changed); err == nil {
			t.Fatal("explicit selection rewrote missing or tampered running arguments")
		}
	}
	x.p.FirstNativeEpoch++
	for index := range x.p.Validators {
		x.p.Validators[index].Adoption.FirstNativeEpoch = x.p.FirstNativeEpoch
	}
	x.p.PlanHash, _ = x.p.hash()
	other, err := publishNativeHistoryRecoveryV2(t.Context(), f.cfg, f.stateDir, f.plan, f.roles, x.h, x.p, func(context.Context, *nativeHistoryRecoveryPlanV2) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	f.cfg.nativeHistoryRecoveryV2 = &nativeHistoryRecoverySelectionV2{path: other.Path, sha256: other.SHA256}
	if err := checkNativeHistoryRecoveryLiveArgumentsV2(t.Context(), f.cfg, f.stateDir, f.plan, x.h, specs); err == nil {
		t.Fatal("a different valid receipt was substituted for running native authority")
	}
	after, _ := json.Marshal(specs)
	if !bytes.Equal(before, after) {
		t.Fatal("live selector checks mutated running manifest arguments")
	}
}
