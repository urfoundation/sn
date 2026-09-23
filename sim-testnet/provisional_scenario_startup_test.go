//go:build linux

// A stopped generation remains immutable history. The release command starts
// its authenticated successor before consuming that successor's live gate.
package main

import (
	"bytes"
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Every process and service observation is synthetic. An impossible Linux pid
// reproduces an absent historical owner without sleeps or scheduler races.
func stoppedScenarioStartupFixture(t *testing.T) (*Executor, *provisionalStoppedTopology, SupervisorFile, SupervisorState, *provisionalLiveTopology) {
	t.Helper()
	self, original := provisionalAllowanceAdoptionFixture(t)
	if err := self.activateProvisionalSetupRevision(t.Context(), original, func(context.Context, Action) error {
		return errors.New("unexpected setup dispatch")
	}); err != nil {
		t.Fatal(err)
	}
	return stoppedScenarioTopologyFixture(t, self)
}

// Both local-only and transaction-bearing approvals use the same stopped
// process generation, explicit scenario provenance, and service observation.
func stoppedScenarioTopologyFixture(t *testing.T, self *Executor) (*Executor, *provisionalStoppedTopology, SupervisorFile, SupervisorState, *provisionalLiveTopology) {
	t.Helper()
	options := cliOptions{Apply: true, ProvisionalResume: true, PlanHash: self.plan.PlanHash, Name: "release-1.0"}
	if err := prepareProvisionalResume(t.Context(), self.cfg, self.stateDir, "scenario", options, self.plan); err != nil {
		t.Fatal(err)
	}
	processDir := filepath.Join(self.stateDir, "processes")
	if err := os.MkdirAll(processDir, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := SupervisorFile{Schema: "urnetwork-sim-supervisor-v1", DeploymentID: self.plan.DeploymentID,
		BinaryHash: self.cfg.provisionalResume.Driver.ExecutableSHA256,
		Specs: []ProcessSpec{{ID: "synthetic-miner", Role: "miner", Identity: "synthetic-miner",
			StdoutPath: filepath.Join(processDir, "synthetic-miner.stdout.log"), StderrPath: filepath.Join(processDir, "synthetic-miner.stderr.log")}}}
	for _, path := range []string{manifest.Specs[0].StdoutPath, manifest.Specs[0].StderrPath} {
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	hash, err := canonicalHashHex(manifest)
	if err != nil {
		t.Fatal(err)
	}
	state := SupervisorState{Schema: "urnetwork-sim-supervisor-state-v1", UpdatedAt: "2026-01-01T00:00:00Z", ContractCleanupCutoff: "2026-01-01T00:00:00Z",
		ManifestHash: hash, SupervisorPID: math.MaxInt32, SupervisorStartTimeTicks: 1,
		Processes: []ProcessState{{ID: "synthetic-miner", Role: "miner", Identity: "synthetic-miner", StartedAt: "2026-01-01T00:00:00Z", ExitError: "supervisor stopped"}}}
	adoption := &provisionalLiveTopology{Schema: "urnetwork-sim-provisional-live-topology-v1", Provisional: true,
		PlanHash: self.plan.PlanHash, CompletedAt: "2026-01-01T00:00:00Z", ManifestHash: hash,
		SupervisorPID: state.SupervisorPID, SupervisorStartTimeTicks: state.SupervisorStartTimeTicks,
		ProcessLogGatePath: filepath.Join(filepath.Dir(self.cfg.provisionalResume.RecordPath), "old-process-log-gate.json"), manifest: manifest}
	if _, err := newProvisionalProcessLogGate(self.stateDir, adoption); err != nil {
		t.Fatal(err)
	}
	for path, value := range map[string]any{"supervisor.json": manifest, "supervisor.state.json": state, "provisional-resumes/live-topology.json": adoption} {
		if err := writePublicJSON(filepath.Join(self.stateDir, path), value); err != nil {
			t.Fatal(err)
		}
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "systemctl"), []byte("#!/bin/sh\nprintf '%s\\n' 'ActiveState=inactive' 'SubState=dead' 'Result=success' 'ExecMainCode=1' 'ExecMainStatus=0'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	stopped, err := prepareStoppedProvisionalTopology(t.Context(), self.cfg, self.stateDir, "scenario")
	if err != nil || stopped == nil {
		t.Fatal("exact stopped release scenario was not admitted", err)
	}
	return self, stopped, manifest, state, adoption
}

// Reproduce the missing historical /proc owner and prove actual orchestration
// enters startup before the unchanged strict live-generation reader.
func TestProvisionalScenarioStartsStoppedSuccessorBeforeLogGate(t *testing.T) {
	self, stopped, _, state, adoption := stoppedScenarioStartupFixture(t)
	if _, err := loadProvisionalOrStrictProcessLogGate(self.cfg, self.stateDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("fixture did not reproduce the absent old process owner", err)
	}
	oldGate, err := os.ReadFile(adoption.ProcessLogGatePath)
	if err != nil {
		t.Fatal(err)
	}
	entries := self.journal.Entries()
	var order []string
	start := func(context.Context, *Executor, *provisionalStoppedTopology, map[string]string) error {
		order = append(order, "start")
		state.SupervisorPID, state.SupervisorStartTimeTicks = os.Getpid(), currentProcessStartTimeTicks(t)
		state.Processes[0].PID, state.Processes[0].Healthy, state.Processes[0].ExitError = os.Getpid(), true, ""
		if err := writePublicJSON(filepath.Join(self.stateDir, "supervisor.state.json"), state); err != nil {
			return err
		}
		next := *adoption
		next.SupervisorPID, next.SupervisorStartTimeTicks = state.SupervisorPID, state.SupervisorStartTimeTicks
		next.ProcessLogGatePath = filepath.Join(filepath.Dir(adoption.ProcessLogGatePath), "successor-process-log-gate.json")
		if _, err := newProvisionalProcessLogGate(self.stateDir, &next); err != nil {
			return err
		}
		return writePublicJSON(filepath.Join(self.stateDir, "provisional-resumes/live-topology.json"), &next)
	}
	run := func() error {
		order = append(order, "gate")
		gate, err := loadProvisionalOrStrictProcessLogGate(self.cfg, self.stateDir)
		if err != nil {
			return err
		}
		if gate.state.SupervisorPID != os.Getpid() || gate.path == adoption.ProcessLogGatePath {
			return errors.New("scenario consumed its predecessor gate")
		}
		return nil
	}
	if err := runScenarioAfterRetainedStartup(t.Context(), self, stopped, nil, start, run); err != nil {
		t.Fatal("stopped successor could not reach its live gate", err)
	}
	if !reflect.DeepEqual(order, []string{"start", "gate"}) || !reflect.DeepEqual(entries, self.journal.Entries()) {
		t.Fatal("restart changed action history or read the old gate first", order)
	}
	retained, err := os.ReadFile(adoption.ProcessLogGatePath)
	if err != nil || !bytes.Equal(oldGate, retained) {
		t.Fatal("successor rewrote its historical process-log fence", err)
	}
}

// Startup failure leaves the old generation and its gate intact and never
// starts traffic. Cancellation after a successful start has the same ordering.
func TestProvisionalScenarioFailedStartupDoesNotEnterTraffic(t *testing.T) {
	self, stopped, _, _, _ := stoppedScenarioStartupFixture(t)
	for _, canceled := range []bool{false, true} {
		ctx, cancel := context.WithCancel(t.Context())
		starts, runs := 0, 0
		start := func(context.Context, *Executor, *provisionalStoppedTopology, map[string]string) error {
			starts++
			if canceled {
				cancel()
				return nil
			}
			return errors.New("synthetic service start failure")
		}
		err := runScenarioAfterRetainedStartup(ctx, self, stopped, nil, start, func() error { runs++; return nil })
		cancel()
		if err == nil || starts != 1 || runs != 0 {
			t.Fatalf("unsuccessful start entered traffic: canceled=%t starts=%d runs=%d err=%v", canceled, starts, runs, err)
		}
	}
}

// A live owner is never treated as stopped, even with stale inactive service
// metadata. Its stale handoff stays a gate failure rather than being discarded.
func TestProvisionalScenarioLiveOwnerKeepsGenerationGate(t *testing.T) {
	self, _, _, state, _ := stoppedScenarioStartupFixture(t)
	state.SupervisorPID, state.SupervisorStartTimeTicks = os.Getpid(), currentProcessStartTimeTicks(t)
	state.Processes[0].PID, state.Processes[0].Healthy = os.Getpid(), true
	if err := writePublicJSON(filepath.Join(self.stateDir, "supervisor.state.json"), state); err != nil {
		t.Fatal(err)
	}
	stopped, err := prepareStoppedProvisionalTopology(t.Context(), self.cfg, self.stateDir, "scenario")
	if err != nil || stopped != nil {
		t.Fatal("live owner was admitted to stopped restart", err)
	}
	starts := 0
	err = runScenarioAfterRetainedStartup(t.Context(), self, nil, nil, func(context.Context, *Executor, *provisionalStoppedTopology, map[string]string) error {
		starts++
		return nil
	}, func() error {
		_, err := loadProvisionalOrStrictProcessLogGate(self.cfg, self.stateDir)
		return err
	})
	if err == nil || starts != 0 {
		t.Fatal("live stale process gate was waived or restarted", starts, err)
	}
}

// Stopped topology alone grants no restart authority to read-only observation,
// strict acceptance, a different command, or a service already transitioning.
func TestProvisionalScenarioRestartRequiresExactScope(t *testing.T) {
	self, _, manifest, state, _ := stoppedScenarioStartupFixture(t)
	record := *self.cfg.provisionalResume.Record
	for _, changed := range []provisionalResumeRecord{
		{Command: "scenario", Scenario: "epoch", Provisional: true},
		{Command: "scenario", Scenario: "release-1.0", Provisional: true, ReadOnly: true},
		{Command: "scenario", Scenario: "release-1.0", Provisional: true, FinalAcceptance: true},
		{Command: "scenario", Scenario: "release-1.0"},
		{Command: "setup", Provisional: true},
	} {
		if provisionalRetainedStartupAllowed(&changed) {
			t.Errorf("unapproved startup scope was admitted: %+v", changed)
		}
	}
	for _, service := range []supervisorServiceStatus{{ActiveState: "active", SubState: "running"}, {ActiveState: "activating", SubState: "start"}, {ActiveState: "inactive", SubState: "failed"}} {
		if err := provisionalStoppedTopologyEligible(self.cfg, "scenario", manifest, state.ManifestHash, state, service); err == nil {
			t.Errorf("non-stopped service was admitted: %+v", service)
		}
	}
	for _, name := range []string{"release-1.0", "production-soak", releaseCandidateCampaignName} {
		changed := record
		changed.Scenario = name
		if !provisionalRetainedStartupAllowed(&changed) {
			t.Errorf("explicit provisional release %s cannot continue", name)
		}
	}
}
