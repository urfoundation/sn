package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/urnetwork/server/v2026/controller"
)

func TestValidatorStateNamespacePreparationCollectsEveryValidatorFailure(t *testing.T) {
	cfg := testResolvedConfig(t)
	stateDir := t.TempDir()
	fixture := readValidatorNamespaceStoreFixture(t)
	for _, name := range []string{"validator-1", "validator-2"} {
		installValidatorNamespaceStoreFixture(t, filepath.Join(stateDir, "runtime", name, "state", "operators", "no-1", "attempt-ledger.records"), fixture)
	}
	before := validatorNamespaceTreeSnapshot(t, stateDir)
	err := preflightSignedAttemptStateNamespaces(cfg, stateDir)
	if err == nil || !strings.Contains(err.Error(), "validator 1 namespace") || !strings.Contains(err.Error(), "validator 2 namespace") {
		t.Fatalf("namespace preparation hid independent protected state: %v", err)
	}
	if !reflect.DeepEqual(before, validatorNamespaceTreeSnapshot(t, stateDir)) {
		t.Fatal("read-only namespace collection migrated or reset state")
	}
}

func TestRuntimeEvidenceV2PreparationCollectsEveryBoundReferenceFailure(t *testing.T) {
	cfg := testResolvedConfig(t)
	stateDir := t.TempDir()
	configureRuntimeEvidenceV2Test(t, cfg, stateDir)
	for _, path := range []string{cfg.Config.ValidatorEvidenceV2[0].Evidence.Operators[0].Context.Path, cfg.Config.ValidatorEvidenceV2[0].Evidence.Operators[1].History.Path, cfg.Config.ValidatorEvidenceV2[1].Evidence.Operators[0].VPKSignature.Path} {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	cfg.Config.ValidatorEvidenceV2[1].Evidence.Operators[1].Activation.Path = filepath.Join(t.TempDir(), "unapproved-reference")
	before := validatorNamespaceTreeSnapshot(t, stateDir)
	err := preflightRuntimeEvidenceV2(cfg, stateDir)
	if err == nil {
		t.Fatal("incomplete runtime evidence references passed")
	}
	previous := -1
	for _, name := range []string{"validator 1 no_id 1 evidence reference 3", "validator 1 no_id 2 evidence reference 4", "validator 2 no_id 1 evidence reference 1", "validator 2 no_id 2 evidence reference 0 blocked by incorrect role path"} {
		position := strings.Index(err.Error(), name)
		if position <= previous {
			t.Fatalf("reference failure %s omitted or reordered: %v", name, err)
		}
		previous = position
	}
	if strings.Contains(err.Error(), "unapproved-reference") {
		t.Fatalf("invalid role path reached the file reader: %v", err)
	}
	if !reflect.DeepEqual(before, validatorNamespaceTreeSnapshot(t, stateDir)) {
		t.Fatal("reference preparation rendered or rewrote inputs")
	}
}

func TestRuntimeEvidenceV2PreparationBlocksInvalidBoundsAndContinuesOtherValidator(t *testing.T) {
	cfg := testResolvedConfig(t)
	stateDir := t.TempDir()
	configureRuntimeEvidenceV2Test(t, cfg, stateDir)
	cfg.Config.ValidatorEvidenceV2[0].Evidence.Bounds.Persistence.MaxJournalBytes = 0
	for _, index := range []int{0, 1} {
		if err := os.Remove(cfg.Config.ValidatorEvidenceV2[index].Evidence.Operators[0].Context.Path); err != nil {
			t.Fatal(err)
		}
	}
	err := preflightRuntimeEvidenceV2(cfg, stateDir)
	if err == nil || !strings.Contains(err.Error(), "validator 1 references blocked by evidence configuration") || !strings.Contains(err.Error(), "validator 2 no_id 1 evidence reference 3") || strings.Contains(err.Error(), "validator 1 no_id 1 evidence reference") {
		t.Fatalf("invalid bounds were read or independent validator was skipped: %v", err)
	}
}

// A repair-only setup can reuse its authenticated render receipt. The same
// source must expose future namespace/reference failures for actual launch.
func TestLaunchPreparationRuntimeChecksDoNotBlockCompletedSetupRepair(t *testing.T) {
	executor := launchPreparationTestExecutor(t)
	configureRuntimeEvidenceV2Test(t, executor.cfg, executor.stateDir)
	appendLaunchPreparationTestReceipt(t, executor, Action{ID: "config.render", Kind: "local", IntentHash: "approved-render"})
	fixture := readValidatorNamespaceStoreFixture(t)
	installValidatorNamespaceStoreFixture(t, filepath.Join(executor.stateDir, "runtime", "validator-1", "state", "operators", "no-1", "attempt-ledger.records"), fixture)
	if err := os.Remove(executor.cfg.Config.ValidatorEvidenceV2[1].Evidence.Operators[1].Context.Path); err != nil {
		t.Fatal(err)
	}
	before := validatorNamespaceTreeSnapshot(t, executor.stateDir)
	setup := &launchPreparationReport{Ready: true}
	collectLaunchRuntimePreparation(setup, "setup", executor)
	if err := setup.Error(); err != nil || !setup.Ready || len(setup.Checks) != 1 || setup.Checks[0].Hard || !strings.Contains(setup.Checks[0].Detail, "already verified") {
		t.Fatalf("future launch authority blocked approved setup repair: %+v %v", setup, err)
	}
	launch := &launchPreparationReport{Ready: true}
	collectLaunchRuntimePreparation(launch, "launch", executor)
	err := launch.Error()
	if err == nil || !strings.Contains(err.Error(), "signed-attempt-namespaces") || !strings.Contains(err.Error(), "validator 2 no_id 2 evidence reference 3") || !strings.Contains(err.Error(), "reserved-attempt-uploads: blocked by runtime-deployment-inputs") {
		t.Fatalf("full launch preparation stopped before hidden render checks: %v", err)
	}
	if !reflect.DeepEqual(before, validatorNamespaceTreeSnapshot(t, executor.stateDir)) {
		t.Fatal("preparation rendered roles or changed signed namespaces")
	}
}

func TestLaunchPreparationBinaryBuildReportsWorkloadTargetFailure(t *testing.T) {
	cfg := testResolvedConfig(t)
	cfg.Repos.SN, cfg.Repos.Server = t.TempDir(), t.TempDir()
	toolDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(toolDir, "go"), []byte("#!/bin/sh\nprintf 'synthetic build refusal\\n' >&2\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", toolDir)
	binaries, err := buildReleaseBinaries(t.Context(), cfg, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "build sim-testnet") || !strings.Contains(err.Error(), "connect server binary blocked by simulator build") || strings.Contains(err.Error(), "server-ctl") || len(binaries) != 0 {
		t.Fatalf("workload build failure admitted a divergent migration binary: binaries=%v error=%v", binaries, err)
	}
}

func TestLaunchPreparationReservedUploadCensusCollectsEveryOperatorFailure(t *testing.T) {
	cfg := testResolvedConfig(t)
	cfg.Config.Artifacts.ReservedAttemptUploads = make([]controller.StReservedAttemptUploadConfig, cfg.Config.Topology.Operators)
	err := validateRuntimeReservedAttemptUploadCensus(cfg.Config)
	if err == nil || !strings.Contains(err.Error(), "operator 1 reserved staging") || !strings.Contains(err.Error(), "operator 2 reserved staging") {
		t.Fatalf("reserved capacity preparation stopped at one operator: %v", err)
	}
}
