//go:build linux

// Actual test-owned children exit at a proof-read barrier. The controller must
// retain the exact restart intent until a fresh approved generation produces.
package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// A pipe, rather than a timer or signal, owns the child's complete lifetime.
func startValidatorReadinessChild(t *testing.T) (int, uint64, func() error) {
	t.Helper()
	command := exec.Command("cat")
	command.Stdout, command.Stderr = io.Discard, io.Discard
	input, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		_ = input.Close()
		t.Fatal(err)
	}
	var once sync.Once
	var stopped error
	stop := func() error {
		once.Do(func() { stopped = errors.Join(input.Close(), command.Wait()) })
		return stopped
	}
	t.Cleanup(func() { _ = stop() })
	ticks, err := processStartTimeTicks(command.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	return command.Process.Pid, ticks, stop
}

// Publish only fixture supervisor bytes. The signed authority and intent stay
// exact, and any unexpected production signal would fail the test immediately.
func publishValidatorReadinessState(t *testing.T, fixture *validatorRestartFixture) {
	t.Helper()
	if err := writePublicJSON(filepath.Join(fixture.driver.stateDir, "supervisor.state.json"), fixture.state); err != nil {
		t.Fatal(err)
	}
}

// Park after a real complete proof read, change the independent supervisor
// observation, then join the original Restore call before inspecting evidence.
func runValidatorReadinessBarrier(t *testing.T, fixture *validatorRestartFixture, ctx context.Context, atRead func()) ([]FaultProcessEvidence, error) {
	t.Helper()
	reached, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var reachedOnce, releaseOnce sync.Once
	prior := fixture.driver.afterRestartProofReadForTest
	fixture.driver.afterRestartProofReadForTest = func() {
		reachedOnce.Do(func() { close(reached); <-release })
	}
	var processes []FaultProcessEvidence
	var restoreErr error
	go func() {
		processes, restoreErr = fixture.driver.Restore(ctx, fixture.fault)
		close(done)
	}()
	defer func() {
		releaseOnce.Do(func() { close(release) })
		<-done
		fixture.driver.afterRestartProofReadForTest = prior
	}()
	select {
	case <-reached:
	case <-done:
		t.Fatalf("restore returned before the actual proof-read barrier: %v", restoreErr)
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	}
	atRead()
	releaseOnce.Do(func() { close(release) })
	<-done
	return processes, restoreErr
}

// Even a healthy snapshot can outlive its child while signed proof bytes are
// read. Both the publication lag and explicit stopped state remain pending.
func TestValidatorRestartReplacementExitDuringProofReadRemainsPending(t *testing.T) {
	for _, publishStopped := range []bool{false, true} {
		fixture := newValidatorRestartFixture(t)
		pid, ticks, stop := startValidatorReadinessChild(t)
		fixture.state.Processes[0].PID, fixture.state.Processes[0].StartTimeTicks = pid, ticks
		publishValidatorReadinessState(t, fixture)
		for noId := range fixture.paths {
			fixture.proof(t, noId+1, fixture.started+2000, nil)
		}
		processes, err := runValidatorReadinessBarrier(t, fixture, t.Context(), func() {
			if err := stop(); err != nil {
				t.Fatal(err)
			}
			if publishStopped {
				fixture.state.Processes[0].PID, fixture.state.Processes[0].StartTimeTicks, fixture.state.Processes[0].Healthy = 0, 0, false
				publishValidatorReadinessState(t, fixture)
			}
		})
		retained, readErr := os.ReadFile(fixture.driver.activePath())
		if !processRestartPending(fixture.fault, err) || len(processes) != 0 || readErr != nil || !bytes.Equal(retained, fixture.intent) {
			t.Errorf("stopped=%t legitimate exit lost pending intent: processes=%+v err=%v read=%v", publishStopped, processes, err, readErr)
		}
	}
}

// A newer approved child cannot inherit proof freshness from the child checked
// at entry. A later Restore must read both new trails before clearing intent.
func TestValidatorRestartReplacementDuringProofReadRequiresFreshNextGeneration(t *testing.T) {
	fixture := newValidatorRestartFixture(t)
	pid, ticks, stop := startValidatorReadinessChild(t)
	nextPid, nextTicks, _ := startValidatorReadinessChild(t)
	fixture.state.Processes[0].PID, fixture.state.Processes[0].StartTimeTicks = pid, ticks
	publishValidatorReadinessState(t, fixture)
	for noId := range fixture.paths {
		fixture.proof(t, noId+1, fixture.started+2000, nil)
	}
	processes, err := runValidatorReadinessBarrier(t, fixture, t.Context(), func() {
		if err := stop(); err != nil {
			t.Fatal(err)
		}
		state := &fixture.state.Processes[0]
		state.PID, state.StartTimeTicks, state.Restarts = nextPid, nextTicks, state.Restarts+1
		state.StartedAt = time.UnixMilli(int64(fixture.started + 10000)).UTC().Format(time.RFC3339)
		publishValidatorReadinessState(t, fixture)
	})
	retained, readErr := os.ReadFile(fixture.driver.activePath())
	if !processRestartPending(fixture.fault, err) || len(processes) != 0 || readErr != nil || !bytes.Equal(retained, fixture.intent) {
		t.Fatalf("approved replacement lost pending intent: processes=%+v err=%v read=%v", processes, err, readErr)
	}
	if processes, err := fixture.driver.Restore(t.Context(), fixture.fault); !processRestartPending(fixture.fault, err) || len(processes) != 0 {
		t.Fatalf("old trails authorized the new generation: %+v %v", processes, err)
	}
	for noId := range fixture.paths {
		fixture.proof(t, noId+1, fixture.started+12000, nil)
	}
	processes, err = fixture.driver.Restore(t.Context(), fixture.fault)
	if err != nil || len(processes) != 1 || processes[0].PID != nextPid || processes[0].StartTimeTicks != nextTicks {
		t.Fatalf("fresh approved next generation could not restore: %+v %v", processes, err)
	}
}
