// Synthetic docker commands force timeout, lost-reply, and ownership changes
// through the real runtime and fault ledger without touching a docker daemon.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

// A command-owned state machine replaces external processes, never readiness.
type containerRestartTestState struct {
	dependency supervisorDependency
	pid        int
	running    bool
	ready      bool
	starts     int
	stops      int
	lostReply  bool
	startError error
}

// The original generation and active file remain independent of fake health.
type containerRestartTestFixture struct {
	driver    *liveScenarioFaultDriver
	runtime   *dockerScenarioContainerRuntime
	spec      scenarioFaultSpec
	stateKVs  map[string]*containerRestartTestState
	processes []FaultProcessEvidence
	raw       []byte
	commands  int
}

// Apply traverses the production stop/inspect path and persists its real ledger.
func newContainerRestartTestFixture(t *testing.T, targetIds ...string) *containerRestartTestFixture {
	t.Helper()
	cfg := testResolvedConfig(t)
	targetKVs, err := dependencyFaultTargets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	fixture := &containerRestartTestFixture{spec: scenarioFaultSpec{ID: "synthetic-dependency-restart", Kind: "container-restart", Targets: targetIds, TriggerOffsetBlocks: 2, DurationBlocks: 3}, stateKVs: map[string]*containerRestartTestState{}}
	runtime := &dockerScenarioContainerRuntime{dependencyKVs: map[string]supervisorDependency{}}
	var dependencies []supervisorDependency
	for index, targetId := range targetIds {
		target := targetKVs[targetId]
		hash, err := managedContainerSpecHash(target.spec)
		if err != nil {
			t.Fatal(err)
		}
		dependency := supervisorDependency{TargetId: targetId, ContainerId: fmt.Sprintf("%064x", index+1), Name: target.spec.Name, Image: target.spec.Image, SpecHash: hash}
		runtime.dependencyKVs[dependency.Name] = dependency
		dependencies = append(dependencies, dependency)
		fixture.stateKVs[dependency.ContainerId] = &containerRestartTestState{dependency: dependency, pid: 1200 + index, running: true}
	}
	runtime.command = func(ctx context.Context, args ...string) ([]byte, error) {
		fixture.commands++
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		identity := args[len(args)-1]
		if args[0] == "exec" {
			identity = args[1]
		}
		state := fixture.stateKVs[identity]
		if state == nil {
			t.Fatalf("docker command selected a name or foreign generation: %v", args)
		}
		switch args[0] {
		case "container":
			d := state.dependency
			return fmt.Appendf(nil, "%t|%d|%s|/%s|%s|%s|no\n", state.running, state.pid, d.ContainerId, d.Name, d.Image, d.SpecHash), nil
		case "stop":
			state.stops++
			state.running, state.pid = false, 0
			return nil, nil
		case "start":
			state.starts++
			if state.startError != nil {
				return nil, state.startError
			}
			state.running, state.pid = true, 2400+state.starts
			if state.lostReply {
				state.lostReply = false
				<-ctx.Done()
				return nil, ctx.Err()
			}
			return nil, nil
		case "exec":
			if !state.ready {
				return []byte("synthetic dependency still starting"), errors.New("synthetic readiness refusal")
			}
			return []byte(targetKVs[state.dependency.TargetId].spec.ReadyExpected), nil
		default:
			t.Fatalf("unexpected mutation: %v", args)
			return nil, nil
		}
	}
	fixture.runtime = runtime
	fixture.driver = &liveScenarioFaultDriver{stateDir: t.TempDir(), cfg: cfg, containers: runtime}
	manifest := SupervisorFile{Schema: "urnetwork-sim-supervisor-v1", Dependencies: dependencies}
	hash, err := canonicalHashHex(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := writePublicJSON(filepath.Join(fixture.driver.stateDir, "supervisor.json"), manifest); err != nil {
		t.Fatal(err)
	}
	if err := writePublicJSON(filepath.Join(fixture.driver.stateDir, "supervisor.state.json"), SupervisorState{Schema: "urnetwork-sim-supervisor-state-v1", ManifestHash: hash}); err != nil {
		t.Fatal(err)
	}
	fixture.processes, err = fixture.driver.Apply(t.Context(), fixture.spec)
	if err != nil {
		t.Fatal(err)
	}
	fixture.raw, err = os.ReadFile(fixture.driver.activePath())
	if err != nil {
		t.Fatal(err)
	}
	return fixture
}

// A pending round may change docker health but cannot rewrite original evidence.
func (self *containerRestartTestFixture) requireRetained(t *testing.T) {
	t.Helper()
	raw, err := os.ReadFile(self.driver.activePath())
	if err != nil || !bytes.Equal(raw, self.raw) {
		t.Fatalf("pending container restore changed original ledger: %v", err)
	}
}

// The actual readiness wait exhausts two bounded rounds before health changes.
// Both heartbeats continue with signed pending history and no completed evidence.
func TestContainerRestartReadinessTimeoutRetainsPendingUntilHealthy(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fixture := newContainerRestartTestFixture(t, "operator-1-postgres")
		records, err := initializeFaultRecords(100, []scenarioFaultSpec{fixture.spec})
		if err != nil {
			t.Fatal(err)
		}
		records[0].Status, records[0].AppliedBlock, records[0].AppliedBlockHash = "active", 102, "0x"+strings.Repeat("ab", 32)
		records[0].Processes = slices.Clone(fixture.processes)
		head := ChainHead{Number: 105, Hash: "0x" + strings.Repeat("cd", 32)}
		started := time.Now()
		for round := uint64(1); round <= 2; round++ {
			if err := advanceFaults(t.Context(), head, []scenarioFaultSpec{fixture.spec}, records, fixture.driver); err != nil {
				t.Fatalf("readiness timeout aborted heartbeat: %v", err)
			}
			if records[0].Status != "active" || records[0].RestorePendingRounds != round || records[0].RestoreStartedBlock != 105 || records[0].RestoredBlock != 0 || len(records[0].RestoredProcesses) != 0 {
				t.Fatalf("timeout granted restored evidence: %+v", records[0])
			}
			if err := validateScenarioCampaignFaultState(&ScenarioAcceptanceWindow{StartBlock: 100}, records[0]); err != nil {
				t.Fatal(err)
			}
			fixture.requireRetained(t)
			head.Number++
		}
		if time.Since(started) != 2*containerRestartRoundTimeout {
			t.Fatal("readiness checks escaped their bounded rounds")
		}
		before := cloneScenarioFaultRecords(records)
		for _, state := range fixture.stateKVs {
			if state.starts != 1 || state.stops != 1 {
				t.Fatalf("readiness retry repeated mutation: %+v", state)
			}
			state.ready = true
		}
		if err := advanceFaults(t.Context(), head, []scenarioFaultSpec{fixture.spec}, records, fixture.driver); err != nil {
			t.Fatal(err)
		}
		if records[0].Status != "restored" || records[0].Error != "" || records[0].RestorePendingRounds != 2 || records[0].RestoredBlock != head.Number || len(records[0].RestoredProcesses) != 1 || records[0].RestoredProcesses[0].PID == records[0].Processes[0].PID {
			t.Fatalf("exact replacement did not complete: %+v", records[0])
		}
		if err := validateScenarioFaultProgress(before, records); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(fixture.driver.activePath()); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("completed restore retained ledger: %v", err)
		}
	})
}

// A start timeout may hide a successful mutation. A fresh driver reloads the
// same captured generation, observes it running, and never repeats that start.
func TestContainerRestartLostStartReplyReconcilesAfterDriverRestart(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fixture := newContainerRestartTestFixture(t, "operator-1-redis")
		for _, state := range fixture.stateKVs {
			state.lostReply, state.ready = true, true
		}
		reads, waits := 0, 0
		processes, err := waitScenarioFaultRestore(t.Context(), fixture.spec, func() ([]FaultProcessEvidence, error) {
			reads++
			return fixture.driver.Restore(t.Context(), fixture.spec)
		}, func() error {
			waits++
			fixture.requireRetained(t)
			dependencyKVs, err := fixture.driver.containerDependencies()
			if err != nil {
				return err
			}
			fixture.driver = &liveScenarioFaultDriver{stateDir: fixture.driver.stateDir, cfg: fixture.driver.cfg, containers: &dockerScenarioContainerRuntime{dependencyKVs: dependencyKVs, command: fixture.runtime.command}}
			return nil
		})
		if err != nil || reads != 2 || waits != 1 || len(processes) != 1 {
			t.Fatalf("lost start reply abandoned exact reconciliation: reads=%d waits=%d err=%v", reads, waits, err)
		}
		for _, state := range fixture.stateKVs {
			if state.starts != 1 {
				t.Fatalf("lost reply repeated start %d times", state.starts)
			}
		}
	})
}

// An owned cancellation ends reconciliation, retains its durable fault, and
// cannot be mistaken for a retryable heartbeat deadline or fake success.
func TestContainerRestartRecoveryCancellationRetainsExactLedger(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fixture := newContainerRestartTestFixture(t, "operator-1-postgres")
		if fixture.driver.RecoveryTimeout() != 30*time.Second+supervisorStartupPhaseTimeout {
			t.Fatal("cleanup lacks bounded dependency startup allowance")
		}
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		reads, waits := 0, 0
		_, err := waitScenarioFaultRestore(ctx, fixture.spec, func() ([]FaultProcessEvidence, error) {
			reads++
			return fixture.driver.Restore(ctx, fixture.spec)
		}, func() error { waits++; cancel(); return nil })
		if !errors.Is(err, context.Canceled) || reads != 1 || waits != 1 {
			t.Fatalf("canceled cleanup made another attempt: reads=%d waits=%d err=%v", reads, waits, err)
		}
		fixture.requireRetained(t)
	})
}

// Replacement containers, changed specs, malformed lifecycle fields and unknown
// start failures remain terminal refusals and preserve the original active file.
func TestContainerRestartRejectsChangedOwnerAndUnknownFailure(t *testing.T) {
	for _, change := range []string{"container", "name", "image", "hash", "pid", "start", "role", "identity", "unchanged-pid"} {
		fixture := newContainerRestartTestFixture(t, "operator-1-redis")
		var state *containerRestartTestState
		for _, candidate := range fixture.stateKVs {
			state = candidate
		}
		switch change {
		case "container":
			state.dependency.ContainerId = strings.Repeat("f", 64)
		case "name":
			state.dependency.Name = "synthetic-foreign-container"
		case "image":
			state.dependency.Image = "synthetic-foreign-image"
		case "hash":
			state.dependency.SpecHash = strings.Repeat("e", 64)
		case "pid":
			state.pid = 1
		case "start":
			state.startError = errors.New("synthetic daemon integrity refusal")
		case "role", "identity":
			active, err := readActiveFaultFile(fixture.driver.activePath())
			if err != nil {
				t.Fatal(err)
			}
			if change == "role" {
				active.Processes[0].Role = "synthetic-foreign-role"
			} else {
				active.Processes[0].Identity = "synthetic-foreign-instance"
			}
			if err := writePublicJSON(fixture.driver.activePath(), active); err != nil {
				t.Fatal(err)
			}
			fixture.raw, _ = os.ReadFile(fixture.driver.activePath())
		case "unchanged-pid":
			state.running, state.ready, state.pid = true, true, fixture.processes[0].PID
		}
		if _, err := fixture.driver.Restore(t.Context(), fixture.spec); err == nil || containerRestartPending(fixture.spec, err) {
			t.Fatalf("%s acquired restoration retry authority: %v", change, err)
		}
		fixture.requireRetained(t)
	}
}

// A slow first target must not hide a later identity violation. Marker text or
// wrapping cannot turn an unrelated error into permission to retry restoration.
func TestContainerRestartPendingDoesNotHideMixedOrForeignFailures(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fixture := newContainerRestartTestFixture(t, "operator-1-postgres", "operator-2-redis")
		for _, state := range fixture.stateKVs {
			if state.dependency.TargetId == "operator-2-redis" {
				state.dependency.Image = "synthetic-foreign-image"
			}
		}
		if _, err := fixture.driver.Restore(t.Context(), fixture.spec); err == nil || containerRestartPending(fixture.spec, err) {
			t.Fatalf("pending first member hid foreign second member: %v", err)
		}
		fixture.requireRetained(t)
		pending := &containerRestartPendingError{faultId: fixture.spec.ID, targets: slices.Clone(fixture.spec.Targets)}
		for _, cause := range []error{errors.New(pending.Error()), errors.Join(pending, errors.New("synthetic integrity failure")), &containerRestartPendingError{faultId: "synthetic-other-fault", targets: pending.targets}} {
			waits := 0
			_, err := waitScenarioFaultRestore(t.Context(), fixture.spec, func() ([]FaultProcessEvidence, error) { return nil, cause }, func() error { waits++; return nil })
			if err != cause || waits != 0 {
				t.Fatalf("foreign failure was retried: waits=%d err=%v", waits, err)
			}
		}
	})
}

// A readiness reply cannot authorize a container replaced during that probe.
func TestContainerRestartRechecksOwnershipAfterReadiness(t *testing.T) {
	fixture := newContainerRestartTestFixture(t, "operator-1-redis")
	for _, state := range fixture.stateKVs {
		state.ready = true
	}
	command := fixture.runtime.command
	fixture.runtime.command = func(ctx context.Context, args ...string) ([]byte, error) {
		output, err := command(ctx, args...)
		if args[0] == "exec" {
			fixture.stateKVs[args[1]].dependency.ContainerId = strings.Repeat("e", 64)
		}
		return output, err
	}
	if _, err := fixture.driver.Restore(t.Context(), fixture.spec); err == nil || containerRestartPending(fixture.spec, err) {
		t.Fatalf("successful readiness hid changed readback owner: %v", err)
	}
	fixture.requireRetained(t)
}

// Driver recreation can reuse only the checksum-bound launch generation, never
// a name-only or malformed manifest that invents replacement container authority.
func TestContainerRestartRequiresCapturedSupervisorGeneration(t *testing.T) {
	for _, change := range []string{"missing", "checksum", "identity", "duplicate"} {
		fixture := newContainerRestartTestFixture(t, "operator-1-postgres")
		path := filepath.Join(fixture.driver.stateDir, "supervisor.json")
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var manifest SupervisorFile
		if err := decodeStrictJSONBytes(raw, &manifest); err != nil {
			t.Fatal(err)
		}
		switch change {
		case "missing":
			manifest.Dependencies = nil
		case "checksum":
			manifest.Dependencies[0].ContainerId = strings.Repeat("e", 64)
		case "identity":
			manifest.Dependencies[0].ContainerId = "synthetic-name-only"
		case "duplicate":
			manifest.Dependencies = append(manifest.Dependencies, manifest.Dependencies[0])
		}
		if err := writePublicJSON(path, manifest); err != nil {
			t.Fatal(err)
		}
		if change != "checksum" {
			hash, err := canonicalHashHex(manifest)
			if err != nil {
				t.Fatal(err)
			}
			if err := writePublicJSON(filepath.Join(fixture.driver.stateDir, "supervisor.state.json"), SupervisorState{Schema: "urnetwork-sim-supervisor-state-v1", ManifestHash: hash}); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := fixture.driver.containerDependencies(); err == nil {
			t.Fatalf("%s supervisor identity granted container recovery", change)
		}
		fixture.requireRetained(t)
	}
}

// An adjacent apply rollback also consumes Start. It must reconcile a lost
// reply before returning the original rejected-target error, without restopping.
func TestContainerRestartApplyRollbackReconcilesLostStartReply(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fixture := newContainerRestartTestFixture(t, "operator-1-redis")
		for _, state := range fixture.stateKVs {
			state.ready = true
		}
		if _, err := fixture.driver.Restore(t.Context(), fixture.spec); err != nil {
			t.Fatal(err)
		}
		for _, state := range fixture.stateKVs {
			state.lostReply = true
		}
		invalid := fixture.spec
		invalid.Targets = append(slices.Clone(invalid.Targets), "synthetic-unknown-dependency")
		if _, err := fixture.driver.Apply(t.Context(), invalid); err == nil || !strings.Contains(err.Error(), "not a simulator-owned") {
			t.Fatalf("rollback lost original apply failure: %v", err)
		}
		for _, state := range fixture.stateKVs {
			if !state.running || state.starts != 2 || state.stops != 2 {
				t.Fatalf("rollback did not reconcile exact mutation: %+v", state)
			}
		}
		if _, err := os.Stat(fixture.driver.activePath()); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("failed apply manufactured an active fault: %v", err)
		}
	})
}
