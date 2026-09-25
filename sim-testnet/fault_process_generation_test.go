// Reused and legacy PID state is forced before capture, not after the signal
// guard has already selected a process. Every synthetic child is test-owned.
package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestProcessStateGenerationRejectsMissingOrStaleRestartProof(t *testing.T) {
	for _, missing := range []bool{true, false} {
		fixture := newProcessRestartIntentFixture(t, 2)
		fixture.state.Processes[1].StartTimeTicks++
		if missing {
			fixture.state.Processes[1].StartTimeTicks = 0
		}
		fixture.write(t)
		path := filepath.Join(fixture.driver.stateDir, "supervisor.state.json")
		before, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		// Omitting the optional field is the actual old wire format, not a
		// decoding error. Reading it must not synthesize current kernel proof.
		if missing && bytes.Count(before, []byte(`"start_time_ticks"`)) != 1 {
			t.Fatal("fixture did not preserve the legacy omitted child field")
		}
		states, _, err := fixture.driver.processSnapshot()
		if err != nil || states[fixture.commands[1].spec.ID].StartTimeTicks != fixture.state.Processes[1].StartTimeTicks {
			t.Fatalf("readback rejected or upgraded old child state: %+v, %v", states, err)
		}
		writes, signals := 0, 0
		fixture.driver.restartPersist = func(string, activeFaultFile, scenarioFaultSpec, []FaultProcessEvidence) error { writes++; return nil }
		fixture.driver.restartSignal = func(supervisedCommand, syscall.Signal) bool { signals++; return true }
		if _, err := fixture.driver.Apply(t.Context(), fixture.fault); err == nil || writes != 0 || signals != 0 {
			t.Fatalf("missing=%t state authorized a fresh original: writes=%d signals=%d error=%v", missing, writes, signals, err)
		}
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("refusal relabeled the retained supervisor state: %v", err)
		}
		if _, err := os.Stat(fixture.driver.activePath()); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("unproven generation produced restart intent: %v", err)
		}
		for _, command := range fixture.commands {
			if !supervisedCommandAlive(command) {
				t.Fatal("incomplete cohort changed an earlier live child")
			}
		}
	}
}

// Adoption grants no signal authority and remains available for an old exact
// intent. Read-only restoration can still report its unchanged child pending.
func TestProcessStateGenerationLegacyIntentAdoptionDoesNotSignal(t *testing.T) {
	fixture := newProcessRestartIntentFixture(t, 1)
	fixture.state.Processes[0].StartTimeTicks = 0
	fixture.write(t)
	original := FaultProcessEvidence{ID: fixture.commands[0].spec.ID, Role: fixture.commands[0].spec.Role, Identity: fixture.commands[0].spec.Identity, PID: fixture.commands[0].identity.PID}
	if err := appendActiveFault(fixture.driver.activePath(), activeFaultFile{Schema: "urnetwork-sim-active-faults-v1"}, fixture.fault, []FaultProcessEvidence{original}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(fixture.driver.activePath())
	if err != nil {
		t.Fatal(err)
	}
	fixture.driver.restartPersist = func(string, activeFaultFile, scenarioFaultSpec, []FaultProcessEvidence) error {
		t.Fatal("adoption rewrote legacy intent")
		return nil
	}
	fixture.driver.restartSignal = func(supervisedCommand, syscall.Signal) bool {
		t.Fatal("legacy adoption signaled an unproven child")
		return false
	}
	processes, err := fixture.driver.Apply(t.Context(), fixture.fault)
	if err != nil || len(processes) != 1 || processes[0] != original {
		t.Fatalf("exact legacy intent could not be adopted: %+v, %v", processes, err)
	}
	if _, err := fixture.driver.Restore(t.Context(), fixture.fault); !processRestartPending(fixture.fault, err) {
		t.Fatalf("legacy readback changed unchanged original into completion: %v", err)
	}
	after, err := os.ReadFile(fixture.driver.activePath())
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("legacy readback changed original intent: %v", err)
	}
}

// Both pause and continue must reject the whole cohort before any signal.
// A stopped first child would reveal the old sequential validation order.
func TestProcessStateGenerationProtectsPauseAndContinue(t *testing.T) {
	for _, missing := range []bool{true, false} {
		for _, value := range []syscall.Signal{syscall.SIGSTOP, syscall.SIGCONT} {
			fixture := newProcessRestartIntentFixture(t, 2)
			fixture.fault.Kind = "process-pause"
			fixture.state.Processes[1].StartTimeTicks++
			if missing {
				fixture.state.Processes[1].StartTimeTicks = 0
			}
			fixture.write(t)
			if _, err := fixture.driver.signal(t.Context(), fixture.fault, value); err == nil {
				t.Fatalf("missing=%t unproven later generation admitted signal %v", missing, value)
			}
			raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", fixture.commands[0].identity.PID))
			if err != nil {
				t.Fatal(err)
			}
			_, suffix, ok := strings.Cut(string(raw), ") ")
			if !ok || len(suffix) == 0 || suffix[0] == 'T' || suffix[0] == 't' {
				t.Fatalf("unproven later generation paused the earlier original: %s", raw)
			}
		}
	}
}
