// Real fault ledgers and checksum-bound supervisor snapshots force readiness
// transitions without sleeping, signaling a process, or granting fake health.
package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The original pid is synthetic and never signaled. The replacement probe
// only tests this test process's existence with signal zero.
type processRestartRetryFixture struct {
	driver *liveScenarioFaultDriver
	spec   scenarioFaultSpec
	state  SupervisorState
	raw    []byte
}

// Apply evidence is already durable; the test enters the actual Restore path.
func newProcessRestartRetryFixture(t *testing.T) *processRestartRetryFixture {
	t.Helper()
	dir := t.TempDir()
	process := ProcessSpec{ID: "synthetic-swarm", Role: "miner-swarm", Identity: "synthetic-members:1-2"}
	manifest := SupervisorFile{Schema: "urnetwork-sim-supervisor-v1", Specs: []ProcessSpec{process}}
	hash, err := canonicalHashHex(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := writePublicJSON(filepath.Join(dir, "supervisor.json"), manifest); err != nil {
		t.Fatal(err)
	}
	state := SupervisorState{Schema: "urnetwork-sim-supervisor-state-v1", SupervisorPID: os.Getpid(), ManifestHash: hash, Processes: []ProcessState{{ID: process.ID, Role: process.Role, Identity: process.Identity, PID: 1234, Healthy: false}}}
	if err := writePublicJSON(filepath.Join(dir, "supervisor.state.json"), state); err != nil {
		t.Fatal(err)
	}
	driver := &liveScenarioFaultDriver{stateDir: dir}
	spec := scenarioFaultSpec{ID: "synthetic-restart", Kind: "process-restart", Targets: []string{process.ID}, TriggerOffsetBlocks: 2, DurationBlocks: 3}
	active := activeFaultFile{Schema: "urnetwork-sim-active-faults-v1"}
	if err := appendActiveFault(driver.activePath(), active, spec, []FaultProcessEvidence{{ID: process.ID, Role: process.Role, Identity: process.Identity, PID: 1234}}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(driver.activePath())
	if err != nil {
		t.Fatal(err)
	}
	return &processRestartRetryFixture{driver: driver, spec: spec, state: state, raw: raw}
}

// A pending restore has no side effects on the exact original process intent.
func (self *processRestartRetryFixture) requireRetained(t *testing.T) {
	t.Helper()
	encoded, err := os.ReadFile(self.driver.activePath())
	if err != nil || !bytes.Equal(encoded, self.raw) {
		t.Fatalf("pending recovery changed the durable original generation: %v", err)
	}
}

// Heartbeats checkpoint pending replacement health and continue. A new live
// pid plus independently read health completes exactly one restoration later.
func TestProcessRestartRestoreCheckpointsUntilExactReplacementHealthy(t *testing.T) {
	fixture := newProcessRestartRetryFixture(t)
	records, err := initializeFaultRecords(100, []scenarioFaultSpec{fixture.spec})
	if err != nil {
		t.Fatal(err)
	}
	active, err := readActiveFaultFile(fixture.driver.activePath())
	if err != nil {
		t.Fatal(err)
	}
	records[0].Status, records[0].AppliedBlock, records[0].AppliedBlockHash = "active", 102, "0x"+strings.Repeat("11", 32)
	records[0].Processes = active.Processes
	head := ChainHead{Number: 105, Hash: "0x" + strings.Repeat("22", 32)}
	for round := uint64(1); round <= 2; round++ {
		if err := advanceFaults(t.Context(), head, []scenarioFaultSpec{fixture.spec}, records, fixture.driver); err != nil {
			t.Fatalf("readiness wait terminated heartbeat: %v", err)
		}
		if records[0].Status != "active" || records[0].RestorePendingRounds != round || records[0].RestoreStartedBlock != 105 || records[0].RestoredBlock != 0 || len(records[0].RestoredProcesses) != 0 {
			t.Fatalf("pending restore acquired completed evidence: %+v", records[0])
		}
		if err := validateScenarioCampaignFaultState(&ScenarioAcceptanceWindow{StartBlock: 100}, records[0]); err != nil {
			t.Fatal(err)
		}
		fixture.requireRetained(t)
		fixture.state.Processes[0].PID = os.Getpid()
		if err := writePublicJSON(filepath.Join(fixture.driver.stateDir, "supervisor.state.json"), fixture.state); err != nil {
			t.Fatal(err)
		}
		head.Number++
	}
	before := cloneScenarioFaultRecords(records)
	fixture.state.Processes[0].Healthy = true
	if err := writePublicJSON(filepath.Join(fixture.driver.stateDir, "supervisor.state.json"), fixture.state); err != nil {
		t.Fatal(err)
	}
	if err := advanceFaults(t.Context(), head, []scenarioFaultSpec{fixture.spec}, records, fixture.driver); err != nil {
		t.Fatal(err)
	}
	if records[0].Status != "restored" || records[0].Error != "" || records[0].RestorePendingRounds != 2 || records[0].RestoredBlock != head.Number || len(records[0].RestoredProcesses) != 1 || records[0].RestoredProcesses[0].PID != os.Getpid() {
		t.Fatalf("actual replacement did not complete retained restoration: %+v", records[0])
	}
	if err := validateScenarioCampaignFaultState(&ScenarioAcceptanceWindow{StartBlock: 100}, records[0]); err != nil {
		t.Fatal(err)
	}
	if err := validateScenarioFaultProgress(before, records); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(fixture.driver.activePath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("successful exact restore did not clear its own ledger: %v", err)
	}
}

// The bounded cleanup owner retries the same recorded transition, then clears
// it only after the actual replacement observation. Cancellation retains it.
func TestProcessRestartRestoreRecoveryRetriesAndHonorsCancellation(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		fixture := newProcessRestartRetryFixture(t)
		if timeout := fixture.driver.RecoveryTimeout(); timeout != 30*time.Second+supervisorStartupPhaseTimeout || timeout > scenarioFaultRecoveryMaximumTimeout {
			t.Fatalf("restart recovery lost its bounded replacement startup allowance: %s", timeout)
		}
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		reads, waits := 0, 0
		processes, err := waitScenarioFaultRestore(ctx, fixture.spec, func() ([]FaultProcessEvidence, error) {
			reads++
			return fixture.driver.Restore(ctx, fixture.spec)
		}, func() error {
			waits++
			fixture.requireRetained(t)
			if canceled {
				cancel()
				return nil
			}
			fixture.state.Processes[0].PID, fixture.state.Processes[0].Healthy = os.Getpid(), true
			return writePublicJSON(filepath.Join(fixture.driver.stateDir, "supervisor.state.json"), fixture.state)
		})
		if canceled {
			if !errors.Is(err, context.Canceled) || reads != 1 || waits != 1 || len(processes) != 0 {
				t.Fatalf("cancellation authorized another restore: reads=%d waits=%d err=%v", reads, waits, err)
			}
			fixture.requireRetained(t)
		} else if err != nil || reads != 2 || waits != 1 || len(processes) != 1 || processes[0].PID != os.Getpid() {
			t.Fatalf("cleanup abandoned recoverable readiness: reads=%d waits=%d err=%v", reads, waits, err)
		}
	}
}

// Missing or substituted owner identity is not health. Unknown/mixed failures
// cannot turn into a pending transition or erase the original restart record.
func TestProcessRestartRestoreRejectsChangedOwnerAndMixedFailure(t *testing.T) {
	fixture := newProcessRestartRetryFixture(t)
	for _, fault := range []string{"identity", "role", "missing", "manifest", "pid"} {
		state := fixture.state
		state.Processes = append([]ProcessState(nil), state.Processes...)
		switch fault {
		case "identity":
			state.Processes[0].Identity = "another-instance"
		case "role":
			state.Processes[0].Role = "another-role"
		case "missing":
			state.Processes = nil
		case "manifest":
			state.ManifestHash = "0x" + strings.Repeat("33", 32)
		case "pid":
			state.Processes[0].PID = -1
		}
		if err := writePublicJSON(filepath.Join(fixture.driver.stateDir, "supervisor.state.json"), state); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.driver.Restore(t.Context(), fixture.spec); err == nil || processRestartPending(fixture.spec, err) {
			t.Fatalf("%s acquired readiness retry authority: %v", fault, err)
		}
		fixture.requireRetained(t)
	}
	pending := &processRestartPendingError{faultId: fixture.spec.ID, targets: fixture.spec.Targets}
	for _, cause := range []error{errors.New(pending.Error()), errors.Join(pending, errors.New("signed state changed")), &processRestartPendingError{faultId: "another-fault", targets: fixture.spec.Targets}} {
		waits := 0
		_, err := waitScenarioFaultRestore(t.Context(), fixture.spec, func() ([]FaultProcessEvidence, error) { return nil, cause }, func() error { waits++; return nil })
		if err != cause || waits != 0 {
			t.Fatalf("foreign/mixed failure was retried: waits=%d err=%v", waits, err)
		}
	}
}
