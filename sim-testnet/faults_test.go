package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"syscall"
	"testing"
	"time"

	servercontroller "github.com/urnetwork/server/controller"
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

func TestFaultStateMachineRestoresOnEvidenceAfterMinimumDuration(t *testing.T) {
	spec := scenarioFaultSpec{
		ID: "conditional", Kind: "process-pause", Targets: []string{"validator-1"},
		RestoreCondition: "test-evidence", MinimumDurationBlocks: 3, TriggerOffsetBlocks: 2, DurationBlocks: 10,
	}
	records, err := initializeFaultRecords(100, []scenarioFaultSpec{spec})
	if err != nil {
		t.Fatal(err)
	}
	driver := &fakeFaultDriver{}
	readyCalls := 0
	ready := func(scenarioFaultSpec) (bool, error) { readyCalls++; return true, nil }
	for _, block := range []uint64{102, 104, 105} {
		if err := advanceFaultsWhen(context.Background(), ChainHead{Number: block, Hash: "hash"}, []scenarioFaultSpec{spec}, records, driver, ready); err != nil {
			t.Fatal(err)
		}
	}
	if records[0].Status != "restored" || records[0].RestoredBlock != 105 || records[0].RestoreBlock != 112 || !records[0].RestoreConditionMet || records[0].RestoreConditionBlock != 105 || readyCalls != 1 {
		t.Fatalf("conditional fault record=%+v ready_calls=%d", records[0], readyCalls)
	}
	assertions := appendFaultAssertions(nil, records, time.Now(), &ScenarioObservation{ObservationHash: "hash"})
	if len(assertions) != 1 || !assertions[0].Passed {
		t.Fatalf("early evidence restoration assertion=%+v", assertions)
	}
}

func TestFaultStateMachineUsesDeadlineWhenConditionIsAbsent(t *testing.T) {
	spec := scenarioFaultSpec{
		ID: "conditional-deadline", Kind: "process-pause", Targets: []string{"validator-1"},
		RestoreCondition: "test-evidence", MinimumDurationBlocks: 3, TriggerOffsetBlocks: 2, DurationBlocks: 10,
	}
	records, err := initializeFaultRecords(100, []scenarioFaultSpec{spec})
	if err != nil {
		t.Fatal(err)
	}
	driver := &fakeFaultDriver{}
	ready := func(scenarioFaultSpec) (bool, error) { return false, nil }
	for _, block := range []uint64{102, 105, 111, 112} {
		if err := advanceFaultsWhen(context.Background(), ChainHead{Number: block, Hash: "hash"}, []scenarioFaultSpec{spec}, records, driver, ready); err != nil {
			t.Fatal(err)
		}
	}
	if records[0].Status != "restored" || records[0].RestoredBlock != 112 || records[0].RestoreConditionMet {
		t.Fatalf("deadline restoration record=%+v", records[0])
	}
	assertions := appendFaultAssertions(nil, records, time.Now(), &ScenarioObservation{ObservationHash: "hash"})
	if len(assertions) != 1 || !assertions[0].Passed {
		t.Fatalf("deadline restoration assertion=%+v", assertions)
	}
}

func TestFaultStateMachineConditionalDeadlineCannotBypassMinimumDuration(t *testing.T) {
	spec := scenarioFaultSpec{
		ID: "conditional", Kind: "process-pause", Targets: []string{"validator-1"},
		RestoreCondition: "test-evidence", MinimumDurationBlocks: 5, TriggerOffsetBlocks: 2, DurationBlocks: 5,
	}
	records, err := initializeFaultRecords(100, []scenarioFaultSpec{spec})
	if err != nil {
		t.Fatal(err)
	}
	driver := &fakeFaultDriver{}
	ready := func(scenarioFaultSpec) (bool, error) { return true, nil }
	// A delayed poll applies after the scheduled deadline. The hard deadline
	// cannot erase the mandatory active interval measured from actual apply.
	for _, block := range []uint64{108, 112} {
		if err := advanceFaultsWhen(context.Background(), ChainHead{Number: block, Hash: "hash"}, []scenarioFaultSpec{spec}, records, driver, ready); err != nil {
			t.Fatal(err)
		}
	}
	if records[0].Status != "active" {
		t.Fatalf("conditional fault restored before actual minimum duration: %+v", records[0])
	}
	if err := advanceFaultsWhen(context.Background(), ChainHead{Number: 113, Hash: "hash"}, []scenarioFaultSpec{spec}, records, driver, ready); err != nil {
		t.Fatal(err)
	}
	if records[0].Status != "restored" || records[0].RestoredBlock != 113 {
		t.Fatalf("conditional fault did not restore at actual minimum duration: %+v", records[0])
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
	if len(fault.Targets) == 0 {
		t.Fatalf("targets = %v", fault.Targets)
	}
	if fault.Kind != "miner-control" {
		t.Fatalf("quality fault kind = %s, want miner-control", fault.Kind)
	}
	for _, target := range fault.Targets {
		var miner int
		if _, scanErr := fmt.Sscanf(target, "miner-%d", &miner); scanErr != nil || miner <= cfg.Config.Topology.fleetCandidateMiners() || operatorForMiner(cfg, miner) != cfg.Config.Scenarios.QualityFaultOperator {
			t.Fatalf("quality target %q is not in the configured operator tail", target)
		}
	}
}

func TestReleaseHeadBoundaryFaultTargetsOneSelectedFleetForExactDecayWindow(t *testing.T) {
	cfg := testResolvedConfig(t)
	fault, err := releaseHeadBoundaryFault(cfg, 30)
	if err != nil {
		t.Fatal(err)
	}
	if fault.ID != "head-boundary" || fault.Kind != "miner-control" || fault.TriggerOffsetBlocks != 30 || len(fault.Targets) != cfg.Config.Topology.ClientsPerHeadFleet {
		t.Fatalf("head boundary fault=%+v", fault)
	}
	wantDuration := 2*hyperparameterUint64(cfg.Hyperparameters.OwnerControlled["tempo"]) + cfg.Config.Scenarios.QualityFaultDurationBlocks
	if fault.DurationBlocks != wantDuration {
		t.Fatalf("duration=%d want=%d", fault.DurationBlocks, wantDuration)
	}
	for member, target := range fault.Targets {
		want := fmt.Sprintf("miner-%d", fleetMemberMinerIndex(cfg, 3, member+1))
		if target != want {
			t.Fatalf("target[%d]=%s want=%s", member, target, want)
		}
	}
}

func TestHeadBoundaryDecayTemposTracksApprovedEMAExactly(t *testing.T) {
	tests := []struct {
		name        string
		numerator   uint64
		denominator uint64
		selected    *big.Rat
		challenger  *big.Rat
		want        uint64
	}{
		{name: "wide quarter", numerator: 1, denominator: 4, selected: big.NewRat(4, 1), challenger: big.NewRat(1, 1), want: 5},
		{name: "release boundary", numerator: 1, denominator: 4, selected: big.NewRat(4, 1), challenger: big.NewRat(3, 1), want: 1},
		{name: "faster half", numerator: 1, denominator: 2, selected: big.NewRat(4, 1), challenger: big.NewRat(3, 1), want: 1},
		{name: "immediate", numerator: 1, denominator: 1, selected: big.NewRat(4, 1), challenger: big.NewRat(3, 1), want: 1},
	}
	for _, test := range tests {
		got, err := headBoundaryDecayTempos(test.numerator, test.denominator, test.selected, test.challenger)
		if err != nil || got != test.want {
			t.Fatalf("%s headBoundaryDecayTempos(%d/%d,%s,%s)=(%d,%v), want (%d,nil)", test.name, test.numerator, test.denominator, test.selected, test.challenger, got, err, test.want)
		}
	}
}

func TestHeadBoundaryRecoveryTemposTracksPostDecayEMAExactly(t *testing.T) {
	tests := []struct {
		name        string
		numerator   uint64
		denominator uint64
		selected    *big.Rat
		challenger  *big.Rat
		decay       uint64
		want        uint64
	}{
		{name: "wide quarter", numerator: 1, denominator: 4, selected: big.NewRat(4, 1), challenger: big.NewRat(1, 1), decay: 5, want: 1},
		{name: "release boundary", numerator: 1, denominator: 4, selected: big.NewRat(4, 1), challenger: big.NewRat(3, 1), decay: 1, want: 1},
		{name: "faster half", numerator: 1, denominator: 2, selected: big.NewRat(4, 1), challenger: big.NewRat(3, 1), decay: 1, want: 2},
		{name: "immediate", numerator: 1, denominator: 1, selected: big.NewRat(4, 1), challenger: big.NewRat(3, 1), decay: 1, want: 1},
	}
	for _, test := range tests {
		got, err := headBoundaryRecoveryTempos(test.numerator, test.denominator, test.selected, test.challenger, test.decay)
		if err != nil || got != test.want {
			t.Fatalf("%s recovery=(%d,%v), want (%d,nil)", test.name, got, err, test.want)
		}
	}
	if got, err := headBoundaryRecoveryTempos(1, 4, big.NewRat(4, 1), big.NewRat(2, 1), 1); err == nil || got != 0 {
		t.Fatalf("non-crossing decay recovery=(%d,%v), want rejection", got, err)
	}
}

func TestReleaseHeadBoundaryFaultProvesDecayAndRecoveryInsideTerminalWindow(t *testing.T) {
	cfg := testResolvedConfig(t)
	fault, err := releaseHeadBoundaryFault(cfg, 30)
	if err != nil {
		t.Fatal(err)
	}
	tempo := hyperparameterUint64(cfg.Hyperparameters.OwnerControlled["tempo"])
	recoveryGrace, err := headBoundaryRecoveryGraceBlocks(cfg)
	if err != nil {
		t.Fatal(err)
	}
	terminal := uint64(cfg.Config.Scenarios.ShortEpochs)*cfg.Policy.Settlement.EpochBlocks + cfg.Policy.Settlement.FinalizeOffsetBlocks
	faultEnd := fault.TriggerOffsetBlocks + fault.DurationBlocks
	if faultEnd > uint64(cfg.Config.Scenarios.ShortEpochs)*cfg.Policy.Settlement.EpochBlocks || terminal-faultEnd < tempo+recoveryGrace {
		t.Fatalf("fault=%d..%d terminal=%d recovery_grace=%d", fault.TriggerOffsetBlocks, faultEnd, terminal, recoveryGrace)
	}

	tooShort := testResolvedConfig(t)
	tooShort.Config.Scenarios.ShortEpochs = 2
	if _, err := releaseHeadBoundaryFault(tooShort, 30); err == nil {
		t.Fatal("head-boundary fault accepted a campaign with no complete decay/recovery window")
	}
}

func TestHeadBoundaryDecayTemposRejectsNonDecayingOrInvalidEMA(t *testing.T) {
	for _, value := range [][2]uint64{{0, 1}, {1, 0}, {2, 1}} {
		if got, err := headBoundaryDecayTempos(value[0], value[1], big.NewRat(4, 1), big.NewRat(3, 1)); err == nil || got != 0 {
			t.Fatalf("headBoundaryDecayTempos(%d/%d)=(%d,%v), want rejection", value[0], value[1], got, err)
		}
	}
	invalidScores := [][2]*big.Rat{{nil, big.NewRat(3, 1)}, {big.NewRat(4, 1), nil}, {big.NewRat(0, 1), big.NewRat(3, 1)}, {big.NewRat(3, 1), big.NewRat(3, 1)}}
	for _, scores := range invalidScores {
		if got, err := headBoundaryDecayTempos(1, 4, scores[0], scores[1]); err == nil || got != 0 {
			t.Fatalf("headBoundaryDecayTempos invalid scores %v=(%d,%v), want rejection", scores, got, err)
		}
	}
}

func TestReleaseHeadBoundaryFaultAdaptsToPolicyEMA(t *testing.T) {
	cfg := testResolvedConfig(t)
	cfg.Policy.Steering.HeadScoreEMA.Numerator = 1
	cfg.Policy.Steering.HeadScoreEMA.Denominator = 2
	fault, err := releaseHeadBoundaryFault(cfg, 30)
	if err != nil {
		t.Fatal(err)
	}
	want := 2*hyperparameterUint64(cfg.Hyperparameters.OwnerControlled["tempo"]) + cfg.Config.Scenarios.QualityFaultDurationBlocks
	if fault.DurationBlocks != want {
		t.Fatalf("duration=%d want=%d", fault.DurationBlocks, want)
	}
}

func TestLiveFaultDriverControlsLogicalMinerThroughOwningProductionSwarm(t *testing.T) {
	cfg := testResolvedConfig(t)
	dir := t.TempDir()
	processSpec := ProcessSpec{ID: "miner-swarm-1", Role: "miner-swarm", Identity: "miners:1-50"}
	manifest := SupervisorFile{Schema: "urnetwork-sim-supervisor-v1", DeploymentID: "test", BinaryHash: "hash", Specs: []ProcessSpec{processSpec}}
	manifestHash, err := canonicalHashHex(manifest)
	if err != nil {
		t.Fatal(err)
	}
	state := SupervisorState{Schema: "urnetwork-sim-supervisor-state-v1", SupervisorPID: os.Getpid(), ManifestHash: manifestHash, Processes: []ProcessState{{ID: processSpec.ID, Role: processSpec.Role, Identity: processSpec.Identity, PID: 1234, Healthy: true}}}
	if err := writePublicJSON(filepath.Join(dir, "supervisor.json"), manifest); err != nil {
		t.Fatal(err)
	}
	if err := writePublicJSON(filepath.Join(dir, "supervisor.state.json"), state); err != nil {
		t.Fatal(err)
	}
	var actions []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		actions = append(actions, request.URL.Path)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"schema":"urnetwork-provider-swarm-v1"}`))
	}))
	defer server.Close()
	driver := &liveScenarioFaultDriver{
		stateDir: dir, cfg: cfg,
		minerControlURL: func(_ int, target, action string) string { return server.URL + "/control/" + target + "/" + action },
	}
	fault := scenarioFaultSpec{ID: "logical-miner", Kind: "miner-control", Targets: []string{"miner-1"}, TriggerOffsetBlocks: 1, DurationBlocks: 1}
	before, err := driver.Apply(context.Background(), fault)
	if err != nil || len(before) != 1 || before[0].ID != "miner-1" || before[0].PID != 1234 {
		t.Fatalf("logical miner apply = %+v, %v", before, err)
	}
	after, err := driver.Restore(context.Background(), fault)
	if err != nil || len(after) != 1 || after[0].PID != before[0].PID {
		t.Fatalf("logical miner restore = %+v, %v", after, err)
	}
	want := []string{"/control/miner-1/disable", "/control/miner-1/enable"}
	if len(actions) != len(want) || actions[0] != want[0] || actions[1] != want[1] {
		t.Fatalf("miner control actions = %v, want %v", actions, want)
	}
}

func TestProductionRollingFaultsCoverEveryPersistentRoleWithoutOverlap(t *testing.T) {
	cfg := testResolvedConfig(t)
	faults := productionRollingFaults(cfg)
	dependencyCount := 2*cfg.Config.Topology.Operators + 1
	persistentCount := 2 + 4*cfg.Config.Topology.Operators + cfg.Config.Topology.MinerSwarmProcesses + cfg.Config.Topology.Validators
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
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		if !seen[fmt.Sprintf("claim-relayer-%d", operator)] {
			t.Fatalf("operator %d production claim relayer was not restart-tested", operator)
		}
	}
}

func TestReleaseCampaignCombinesQualityFaultAndEveryPersistentRestart(t *testing.T) {
	cfg := testResolvedConfig(t)
	faults, err := releaseCampaignFaults(cfg)
	if err != nil {
		t.Fatal(err)
	}
	dependencyCount := 2*cfg.Config.Topology.Operators + 1
	persistentCount := 2 + 4*cfg.Config.Topology.Operators + cfg.Config.Topology.MinerSwarmProcesses + cfg.Config.Topology.Validators
	want := 3 + dependencyCount + persistentCount
	if len(faults) != want || faults[0].ID != "quality-cohort" || faults[1].ID != validatorLocalHeadBoundaryFaultID || faults[2].ID != "head-boundary" {
		t.Fatalf("release faults=%d first=%+v want=%d", len(faults), faults[0], want)
	}
	view := faults[1]
	head := faults[2]
	headEnd := head.TriggerOffsetBlocks + head.DurationBlocks
	if view.Kind != validatorLocalHeadBoundaryFaultKind || view.TriggerOffsetBlocks+1 != head.TriggerOffsetBlocks || view.TriggerOffsetBlocks+view.DurationBlocks != headEnd+1 {
		t.Fatalf("validator-local view fault=%+v does not envelope head=%+v", view, head)
	}
	if qualityEnd := faults[0].TriggerOffsetBlocks + faults[0].DurationBlocks; qualityEnd >= head.TriggerOffsetBlocks {
		t.Fatalf("quality fault ends at %d, head starts at %d", qualityEnd, head.TriggerOffsetBlocks)
	}
	seen := map[string]bool{}
	var laneEnd uint64
	for _, fault := range faults[3:] {
		if fault.TriggerOffsetBlocks <= laneEnd || fault.TriggerOffsetBlocks <= head.TriggerOffsetBlocks {
			t.Fatalf("background fault is not sequential: %+v lane_end=%d head_start=%d", fault, laneEnd, head.TriggerOffsetBlocks)
		}
		laneEnd = fault.TriggerOffsetBlocks + fault.DurationBlocks
		if fault.Kind == "process-restart" {
			seen[fault.Targets[0]] = true
		}
	}
	deferred := faults[len(faults)-1]
	headSwarm, mapErr := minerSwarmFor(cfg, fleetMemberMinerIndex(cfg, 3, 1))
	if mapErr != nil {
		t.Fatal(mapErr)
	}
	wantDeferred := fmt.Sprintf("miner-swarm-%d", headSwarm)
	if deferred.Kind != "process-restart" || len(deferred.Targets) != 1 || deferred.Targets[0] != wantDeferred || deferred.TriggerOffsetBlocks <= headEnd {
		t.Fatalf("head owner restart was not deferred: %+v head_end=%d", deferred, headEnd)
	}
	seen[deferred.Targets[0]] = true
	if len(seen) != persistentCount {
		t.Fatalf("persistent restart coverage=%d want=%d targets=%v", len(seen), persistentCount, seen)
	}
	campaignBlocks := uint64(cfg.Config.Scenarios.ShortEpochs) * cfg.Policy.Settlement.EpochBlocks
	maximumEnd := uint64(0)
	for _, fault := range faults {
		if end := fault.TriggerOffsetBlocks + fault.DurationBlocks; end > maximumEnd {
			maximumEnd = end
		}
	}
	if maximumEnd > campaignBlocks {
		t.Fatalf("release fault schedule ends at block %d after %d-block campaign", maximumEnd, campaignBlocks)
	}
}

// The complete rolling production rehearsal must restore every dependency and
// process before the third accepted 360-block epoch ends.
func TestProductionFaultScheduleFitsThreeEpochWindow(t *testing.T) {
	cfg := testResolvedConfig(t)
	faults := productionRollingFaults(cfg)
	wantEnd := uint64(cfg.Config.Scenarios.ProductionEpochs) * cfg.Policy.ProductionCadence.EpochBlocks
	lastEnd := uint64(0)
	for _, fault := range faults {
		end := fault.TriggerOffsetBlocks + fault.DurationBlocks
		if end > lastEnd {
			lastEnd = end
		}
	}
	if len(faults) == 0 || lastEnd > wantEnd {
		t.Fatalf("production fault schedule ends at %d, want <= %d across %d faults", lastEnd, wantEnd, len(faults))
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

// Concurrent release lanes must retain every active mutation in one durable
// recovery ledger and restore either lane without erasing the other.
func TestLiveFaultDriverPreservesConcurrentDisjointRecovery(t *testing.T) {
	cfg := testResolvedConfig(t)
	runtime := &fakeContainerRuntime{}
	driver := &liveScenarioFaultDriver{stateDir: t.TempDir(), cfg: cfg, containers: runtime}
	first := scenarioFaultSpec{ID: "postgres-one", Kind: "container-restart", Targets: []string{"operator-1-postgres"}, TriggerOffsetBlocks: 1, DurationBlocks: 2}
	second := scenarioFaultSpec{ID: "redis-two", Kind: "container-restart", Targets: []string{"operator-2-redis"}, TriggerOffsetBlocks: 1, DurationBlocks: 2}
	if _, err := driver.Apply(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	overlap := first
	overlap.ID = "duplicate-target"
	if _, err := driver.Apply(context.Background(), overlap); err == nil || len(runtime.stopped) != 1 {
		t.Fatalf("overlapping target mutation was not rejected before stop: err=%v stopped=%v", err, runtime.stopped)
	}
	if _, err := driver.Apply(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	active, err := readActiveFaultFile(driver.activePath())
	if err != nil || len(active.Faults) != 2 || len(active.Processes) != 2 {
		t.Fatalf("concurrent active ledger=%+v error=%v", active, err)
	}
	if _, err := driver.Restore(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	active, err = readActiveFaultFile(driver.activePath())
	if err != nil || len(active.Faults) != 1 || active.Faults[0].ID != second.ID || len(active.Processes) != 1 || active.Processes[0].ID != second.Targets[0] {
		t.Fatalf("first restore erased concurrent fault: active=%+v error=%v", active, err)
	}
	if _, err := driver.Restore(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	active, err = readActiveFaultFile(driver.activePath())
	if err != nil || len(active.Faults) != 0 || len(active.Processes) != 0 {
		t.Fatalf("final restore left active state: active=%+v error=%v", active, err)
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

func validatorViewFaultDriverFixture(t *testing.T) (*ResolvedConfig, *liveScenarioFaultDriver, scenarioFaultSpec, *RoleSecrets) {
	t.Helper()
	cfg := testResolvedConfig(t)
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for member := 1; member <= cfg.Config.Topology.ClientsPerHeadFleet; member++ {
		miner := fleetMemberMinerIndex(cfg, validatorLocalHeadBoundaryFleet, member)
		role := roles.Clients[fmt.Sprintf("miner-%d", miner)]
		role.ClientIDHex = fmt.Sprintf("%032x", miner)
		roles.Clients[fmt.Sprintf("miner-%d", miner)] = role
	}
	head, err := releaseHeadBoundaryFault(cfg, 30)
	if err != nil {
		t.Fatal(err)
	}
	fault, err := validatorLocalHeadBoundaryFault(cfg, head)
	if err != nil {
		t.Fatal(err)
	}
	operator := operatorForMiner(cfg, fleetMemberMinerIndex(cfg, validatorLocalHeadBoundaryFleet, 1))
	if operator != 2 {
		t.Fatalf("validator-view fixture fleet operator=%d, want 2", operator)
	}
	stateDir := t.TempDir()
	if err := saveRoleSecrets(filepath.Join(stateDir, "secrets", "roles.json"), roles); err != nil {
		t.Fatal(err)
	}
	process := ProcessSpec{
		ID: "operator-2-api", Role: "operator-api", Identity: "no:2",
		Env: map[string]string{servercontroller.VerifySimulationAssignmentFilterFileEnv: verifyAssignmentFilterPath(stateDir, operator)},
	}
	manifest := SupervisorFile{Schema: "urnetwork-sim-supervisor-v1", DeploymentID: cfg.Config.Deployment.DeploymentID, BinaryHash: "hash", Specs: []ProcessSpec{process}}
	manifestHash, err := canonicalHashHex(manifest)
	if err != nil {
		t.Fatal(err)
	}
	state := SupervisorState{
		Schema: "urnetwork-sim-supervisor-state-v1", SupervisorPID: os.Getpid(), ManifestHash: manifestHash,
		Processes: []ProcessState{{ID: process.ID, Role: process.Role, Identity: process.Identity, PID: os.Getpid(), Healthy: true}},
	}
	if err := writePublicJSON(filepath.Join(stateDir, "supervisor.json"), manifest); err != nil {
		t.Fatal(err)
	}
	if err := writePublicJSON(filepath.Join(stateDir, "supervisor.state.json"), state); err != nil {
		t.Fatal(err)
	}
	return cfg, &liveScenarioFaultDriver{stateDir: stateDir, cfg: cfg}, fault, roles
}

func TestValidatorViewFaultWritesExactPrivateFilterAndRestores(t *testing.T) {
	_, driver, fault, roles := validatorViewFaultDriverFixture(t)
	processes, err := driver.Apply(context.Background(), fault)
	if err != nil {
		t.Fatal(err)
	}
	if len(processes) != 1 || processes[0].ID != fault.Targets[0] || processes[0].PID != os.Getpid() {
		t.Fatalf("validator-view apply evidence=%+v", processes)
	}
	path := verifyAssignmentFilterPath(driver.stateDir, 2)
	var filter validatorViewFilterFile
	if err := readJSONFile(path, &filter); err != nil {
		t.Fatal(err)
	}
	if filter.Schema != validatorViewFilterSchema || filter.ValidatorVPK != roles.Clients["validator-1-no-2"].PublicKeyHex || len(filter.ExcludedClientIDs) != driver.cfg.Config.Topology.ClientsPerHeadFleet || !sort.StringsAreSorted(filter.ExcludedClientIDs) {
		t.Fatalf("validator-view filter=%+v", filter)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("validator-view filter mode=%v error=%v", info, err)
	}
	active, err := readActiveFaultFile(driver.activePath())
	if err != nil || len(active.Faults) != 1 || active.Faults[0].ValidatorID != 1 || active.Faults[0].FleetIndex != validatorLocalHeadBoundaryFleet {
		t.Fatalf("validator-view active ledger=%+v error=%v", active, err)
	}
	if _, err := driver.Restore(context.Background(), fault); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("validator-view filter survived restore: %v", err)
	}
	if _, err := os.Stat(driver.activePath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("validator-view active ledger survived restore: %v", err)
	}
}

func TestValidatorViewFaultRejectsMutationBeforeRestore(t *testing.T) {
	_, driver, fault, _ := validatorViewFaultDriverFixture(t)
	if _, err := driver.Apply(context.Background(), fault); err != nil {
		t.Fatal(err)
	}
	path := verifyAssignmentFilterPath(driver.stateDir, 2)
	if err := atomicWrite(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.Restore(context.Background(), fault); err == nil {
		t.Fatal("mutated validator-view filter was silently removed")
	}
	if _, err := os.Stat(driver.activePath()); err != nil {
		t.Fatalf("failed restore erased recovery ledger: %v", err)
	}
	_, _, expected, err := driver.validatorViewFilter(fault)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(path, expected, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.Restore(context.Background(), fault); err != nil {
		t.Fatal(err)
	}
}

func TestValidatorViewFaultRecoveryRemovesOnlySimulatorOwnedOrphan(t *testing.T) {
	_, driver, _, _ := validatorViewFaultDriverFixture(t)
	path := verifyAssignmentFilterPath(driver.stateDir, 2)
	if err := atomicWrite(path, []byte("orphan\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	unrelated := filepath.Join(driver.stateDir, "runtime", "operator-2", "keep")
	if err := atomicWrite(unrelated, []byte("keep\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := driver.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("orphan validator-view filter survived recovery: %v", err)
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Fatalf("recovery removed unrelated runtime state: %v", err)
	}
}
