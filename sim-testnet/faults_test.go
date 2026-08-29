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

type fakeContainerRuntime struct {
	stopped []string
	started []string
	nextPID int
}

func (runtime *fakeContainerRuntime) Stop(_ context.Context, spec managedContainerSpec) (int, error) {
	runtime.stopped = append(runtime.stopped, spec.Name)
	runtime.nextPID++
	return 100 + runtime.nextPID, nil
}

func (runtime *fakeContainerRuntime) Start(_ context.Context, spec managedContainerSpec) (int, error) {
	runtime.started = append(runtime.started, spec.Name)
	runtime.nextPID++
	return 200 + runtime.nextPID, nil
}

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

func TestFaultImpactAttributionIncludesDependencyConsumersOnlyWhileActive(t *testing.T) {
	specs := []scenarioFaultSpec{{
		ID: "postgres", Kind: "container-restart", Targets: []string{"operator-1-postgres"},
		Impacts: []string{"operator-1-api", "validator-1"}, TriggerOffsetBlocks: 2, DurationBlocks: 3,
	}}
	records, err := initializeFaultRecords(100, specs)
	if err != nil {
		t.Fatal(err)
	}
	if got := scenarioFaultTargets(records, 101, true); len(got) != 0 {
		t.Fatalf("pre-fault targets=%v", got)
	}
	got := scenarioFaultTargets(records, 102, true)
	want := []string{"operator-1-api", "operator-1-postgres", "validator-1"}
	if len(got) != len(want) {
		t.Fatalf("due fault targets=%v, want=%v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("due fault targets=%v, want=%v", got, want)
		}
	}
	records[0].Status = "active"
	observation := &ScenarioObservation{ObservationHash: "old"}
	if err := annotateScenarioExpectedFaults(observation, records); err != nil {
		t.Fatal(err)
	}
	if len(observation.ExpectedFaultTargets) != len(want) || observation.ObservationHash == "old" {
		t.Fatalf("annotated observation=%+v", observation)
	}
	records[0].Status = "restored"
	if got := scenarioFaultTargets(records, 200, false); len(got) != 0 {
		t.Fatalf("restored fault targets=%v", got)
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
	dependencyCount := 2*cfg.Config.Topology.Operators + 1
	persistentCount := 2 + 3*cfg.Config.Topology.Operators + 2*cfg.Config.Topology.Miners + cfg.Config.Topology.Validators
	want := dependencyCount + persistentCount
	if len(faults) != want {
		t.Fatalf("fault count = %d, want %d", len(faults), want)
	}
	seen := map[string]bool{}
	var priorEnd uint64
	for index, fault := range faults {
		wantKind := "container-restart"
		if index == dependencyCount-1 {
			wantKind = "process-pause"
		} else if index >= dependencyCount {
			wantKind = "process-restart"
		}
		wantTargets := 1
		if index == dependencyCount-1 {
			wantTargets = 2
		}
		if fault.Kind != wantKind || len(fault.Targets) != wantTargets || (index >= dependencyCount && seen[fault.Targets[0]]) || fault.TriggerOffsetBlocks <= priorEnd {
			t.Fatalf("overlapping or duplicate fault: %+v prior_end=%d", fault, priorEnd)
		}
		if index >= dependencyCount {
			seen[fault.Targets[0]] = true
		}
		priorEnd = fault.TriggerOffsetBlocks + fault.DurationBlocks
	}
	if !seen[workloadRPCProxyProcessID] {
		t.Fatal("persistent workload EVM RPC proxy was not restart-tested")
	}
	if !seen[workloadSubstrateProcessID] {
		t.Fatal("persistent workload Substrate RPC proxy was not restart-tested")
	}
}

func TestReleaseCampaignCombinesQualityFaultAndEveryPersistentRestart(t *testing.T) {
	cfg := testResolvedConfig(t)
	faults, err := releaseCampaignFaults(cfg)
	if err != nil {
		t.Fatal(err)
	}
	dependencyCount := 2*cfg.Config.Topology.Operators + 1
	persistentCount := 2 + 3*cfg.Config.Topology.Operators + 2*cfg.Config.Topology.Miners + cfg.Config.Topology.Validators
	want := 1 + dependencyCount + persistentCount
	if len(faults) != want || faults[0].ID != "quality-cohort" {
		t.Fatalf("release faults=%d first=%+v want=%d", len(faults), faults[0], want)
	}
	priorEnd := faults[0].TriggerOffsetBlocks + faults[0].DurationBlocks
	for _, fault := range faults[1:] {
		if fault.TriggerOffsetBlocks <= priorEnd {
			t.Fatalf("release faults overlap: %+v prior_end=%d", fault, priorEnd)
		}
		priorEnd = fault.TriggerOffsetBlocks + fault.DurationBlocks
	}
}

func TestRestartFaultAssertionRequiresReplacementPID(t *testing.T) {
	for _, kind := range []string{"process-restart", "container-restart"} {
		base := ScenarioFaultRecord{
			ID: "restart", Kind: kind, Targets: []string{"validator-1"},
			TriggerBlock: 10, RestoreBlock: 12, AppliedBlock: 10, RestoredBlock: 12, Status: "restored",
			Processes: []FaultProcessEvidence{{ID: "validator-1", PID: 100}},
		}
		observation := &ScenarioObservation{ObservationHash: "0xobservation"}
		replaced := base
		replaced.RestoredProcesses = []FaultProcessEvidence{{ID: "validator-1", PID: 101}}
		assertions := appendFaultAssertions(nil, []ScenarioFaultRecord{replaced}, time.Now(), observation)
		if len(assertions) != 1 || !assertions[0].Passed {
			t.Fatalf("%s replacement PID was not accepted: %+v", kind, assertions)
		}
		unchanged := base
		unchanged.RestoredProcesses = []FaultProcessEvidence{{ID: "validator-1", PID: 100}}
		assertions = appendFaultAssertions(nil, []ScenarioFaultRecord{unchanged}, time.Now(), observation)
		if len(assertions) != 1 || assertions[0].Passed {
			t.Fatalf("%s unchanged PID was accepted: %+v", kind, assertions)
		}
	}
}

func TestLiveFaultDriverRestartsOnlySimulatorOwnedDependency(t *testing.T) {
	cfg := testResolvedConfig(t)
	runtime := &fakeContainerRuntime{}
	driver := &liveScenarioFaultDriver{stateDir: t.TempDir(), cfg: cfg, containers: runtime}
	fault := scenarioFaultSpec{
		ID: "postgres-outage", Kind: "container-restart", Targets: []string{"operator-1-postgres"},
		Impacts: operatorDependencyImpacts(cfg, 1), TriggerOffsetBlocks: 1, DurationBlocks: 1,
	}
	before, err := driver.Apply(context.Background(), fault)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 1 || before[0].ID != "operator-1-postgres" || before[0].Role != "postgresql" || before[0].PID <= 1 || len(runtime.stopped) != 1 {
		t.Fatalf("container apply evidence=%+v runtime=%+v", before, runtime)
	}
	after, err := driver.Restore(context.Background(), fault)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 1 || after[0].PID == before[0].PID || len(runtime.started) != 1 || after[0].Identity != before[0].Identity {
		t.Fatalf("container restore evidence=%+v runtime=%+v", after, runtime)
	}
	if _, err := os.Stat(driver.activePath()); !os.IsNotExist(err) {
		t.Fatalf("active dependency fault survived restore: %v", err)
	}
	bad := fault
	bad.Targets = []string{"shared-subtensor"}
	if _, err := driver.Apply(context.Background(), bad); err == nil {
		t.Fatal("non-simulator dependency was accepted as a container fault target")
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
