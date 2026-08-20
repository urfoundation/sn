package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

type fakeFaultDriver struct {
	applied   []string
	restored  []string
	recovered int
}

func (d *fakeFaultDriver) Apply(_ context.Context, spec scenarioFaultSpec) ([]FaultProcessEvidence, error) {
	d.applied = append(d.applied, spec.ID)
	return []FaultProcessEvidence{{ID: spec.Targets[0], Role: "test", PID: 123}}, nil
}

func (d *fakeFaultDriver) Restore(_ context.Context, spec scenarioFaultSpec) ([]FaultProcessEvidence, error) {
	d.restored = append(d.restored, spec.ID)
	return []FaultProcessEvidence{{ID: spec.Targets[0], Role: "test", PID: 123}}, nil
}

func (d *fakeFaultDriver) Recover(context.Context) error { d.recovered++; return nil }

func TestFaultStateMachineUsesFinalizedBlocks(t *testing.T) {
	specs := []scenarioFaultSpec{{ID: "miner-offline", Kind: "process-pause", Targets: []string{"miner-8"}, TriggerOffsetBlocks: 2, DurationBlocks: 3}}
	records, err := initializeFaultRecords(100, specs)
	if err != nil {
		t.Fatal(err)
	}
	driver := &fakeFaultDriver{}
	for _, block := range []uint64{101, 102, 104, 105} {
		if err := advanceFaults(context.Background(), ChainHead{Number: block, Hash: "hash"}, specs, records, driver); err != nil {
			t.Fatal(err)
		}
	}
	if !faultsComplete(records) || len(driver.applied) != 1 || len(driver.restored) != 1 || records[0].AppliedBlock != 102 || records[0].RestoredBlock != 105 {
		t.Fatalf("records=%+v driver=%+v", records, driver)
	}
}

func TestReleaseQualityFaultSelectsOnlyTailCohort(t *testing.T) {
	cfg := testResolvedConfig(t)
	fault, err := releaseQualityFault(cfg)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"miner-8"}
	if len(fault.Targets) != len(want) {
		t.Fatalf("targets = %v", fault.Targets)
	}
	for i := range want {
		if fault.Targets[i] != want[i] {
			t.Fatalf("targets = %v, want %v", fault.Targets, want)
		}
	}
}

func TestProductionRollingFaultsCoverEveryPersistentRoleWithoutOverlap(t *testing.T) {
	cfg := testResolvedConfig(t)
	faults := productionRollingFaults(cfg)
	want := 3*cfg.Config.Topology.Operators + 2*cfg.Config.Topology.Miners + cfg.Config.Topology.Validators
	if len(faults) != want {
		t.Fatalf("fault count = %d, want %d", len(faults), want)
	}
	seen := map[string]bool{}
	var priorEnd uint64
	for _, fault := range faults {
		if fault.Kind != "process-restart" || len(fault.Targets) != 1 || seen[fault.Targets[0]] || fault.TriggerOffsetBlocks <= priorEnd {
			t.Fatalf("overlapping or duplicate fault: %+v prior_end=%d", fault, priorEnd)
		}
		seen[fault.Targets[0]] = true
		priorEnd = fault.TriggerOffsetBlocks + fault.DurationBlocks
	}
}

func TestReleaseCampaignCombinesQualityFaultAndEveryPersistentRestart(t *testing.T) {
	cfg := testResolvedConfig(t)
	faults, err := releaseCampaignFaults(cfg)
	if err != nil {
		t.Fatal(err)
	}
	want := 1 + 3*cfg.Config.Topology.Operators + 2*cfg.Config.Topology.Miners + cfg.Config.Topology.Validators
	if len(faults) != want || faults[0].ID != "quality-cohort" {
		t.Fatalf("release faults=%d first=%+v want=%d", len(faults), faults[0], want)
	}
	priorEnd := faults[0].TriggerOffsetBlocks + faults[0].DurationBlocks
	for _, fault := range faults[1:] {
		if fault.Kind != "process-restart" || fault.TriggerOffsetBlocks <= priorEnd {
			t.Fatalf("release faults overlap: %+v prior_end=%d", fault, priorEnd)
		}
		priorEnd = fault.TriggerOffsetBlocks + fault.DurationBlocks
	}
}

func TestRestartFaultAssertionRequiresReplacementPID(t *testing.T) {
	base := ScenarioFaultRecord{
		ID: "restart", Kind: "process-restart", Targets: []string{"validator-1"},
		TriggerBlock: 10, RestoreBlock: 12, AppliedBlock: 10, RestoredBlock: 12, Status: "restored",
		Processes: []FaultProcessEvidence{{ID: "validator-1", PID: 100}},
	}
	observation := &ScenarioObservation{ObservationHash: "0xobservation"}
	replaced := base
	replaced.RestoredProcesses = []FaultProcessEvidence{{ID: "validator-1", PID: 101}}
	assertions := appendFaultAssertions(nil, []ScenarioFaultRecord{replaced}, time.Now(), observation)
	if len(assertions) != 1 || !assertions[0].Passed {
		t.Fatalf("replacement PID was not accepted: %+v", assertions)
	}
	unchanged := base
	unchanged.RestoredProcesses = []FaultProcessEvidence{{ID: "validator-1", PID: 100}}
	assertions = appendFaultAssertions(nil, []ScenarioFaultRecord{unchanged}, time.Now(), observation)
	if len(assertions) != 1 || assertions[0].Passed {
		t.Fatalf("unchanged PID was accepted: %+v", assertions)
	}
}

func TestLiveFaultDriverStopsAndRecoversExactManifestProcess(t *testing.T) {
	dir := t.TempDir()
	cmd := exec.Command("sleep", "30")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGCONT)
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_, _ = cmd.Process.Wait()
	}()
	spec := ProcessSpec{ID: "miner-8", Role: "miner", Identity: "client"}
	manifest := SupervisorFile{Schema: "urnetwork-sim-supervisor-v1", DeploymentID: "test", BinaryHash: "hash", Specs: []ProcessSpec{spec}}
	manifestHash, _ := canonicalHashHex(manifest)
	state := SupervisorState{Schema: "urnetwork-sim-supervisor-state-v1", SupervisorPID: os.Getpid(), ManifestHash: manifestHash, Processes: []ProcessState{{ID: spec.ID, Role: spec.Role, Identity: spec.Identity, PID: cmd.Process.Pid, Healthy: true}}}
	manifestBytes, _ := json.Marshal(manifest)
	stateBytes, _ := json.Marshal(state)
	if err := os.WriteFile(filepath.Join(dir, "supervisor.json"), manifestBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "supervisor.state.json"), stateBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	driver := &liveScenarioFaultDriver{stateDir: dir}
	fault := scenarioFaultSpec{ID: "quality", Kind: "process-pause", Targets: []string{spec.ID}, TriggerOffsetBlocks: 1, DurationBlocks: 1}
	if processes, err := driver.Apply(context.Background(), fault); err != nil || len(processes) != 1 || processes[0].PID != cmd.Process.Pid {
		t.Fatalf("apply = %+v, %v", processes, err)
	}
	if _, err := os.Stat(driver.activePath()); err != nil {
		t.Fatal(err)
	}
	if err := driver.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(driver.activePath()); !os.IsNotExist(err) {
		t.Fatalf("active fault file survived recovery: %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && syscall.Kill(cmd.Process.Pid, syscall.Signal(0)) != nil {
		time.Sleep(time.Millisecond)
	}
	if err := syscall.Kill(cmd.Process.Pid, syscall.Signal(0)); err != nil {
		t.Fatalf("recovered child is not alive: %v", err)
	}
}
