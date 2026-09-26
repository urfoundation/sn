//go:build linux

// A bounded completion RPC can outlive the replacement that supplied proof
// readiness. Recheck that exact generation before removing the durable intent.
package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

// The real completion reader callback is the barrier, after both signed trails
// were validated. Its head must not stamp readiness for an exited replacement.
func TestValidatorRestartReplacementDuringCompletionReadRetainsPendingIntent(t *testing.T) {
	fixture := newValidatorRestartFixture(t)
	pid, ticks, stop := startValidatorReadinessChild(t)
	nextPid, nextTicks, _ := startValidatorReadinessChild(t)
	fixture.state.Processes[0].PID, fixture.state.Processes[0].StartTimeTicks = pid, ticks
	publishValidatorReadinessState(t, fixture)
	for noId := range fixture.paths {
		fixture.proof(t, noId+1, fixture.started+2000, nil)
	}
	heads := 0
	fixture.driver.faultCompletionHead = func(context.Context) (ChainHead, error) {
		heads++
		if heads == 1 {
			if err := stop(); err != nil {
				t.Fatal(err)
			}
			state := &fixture.state.Processes[0]
			state.PID, state.StartTimeTicks, state.Restarts = nextPid, nextTicks, state.Restarts+1
			state.StartedAt = time.UnixMilli(int64(fixture.started + 10000)).UTC().Format(time.RFC3339)
			publishValidatorReadinessState(t, fixture)
		}
		return faultCompletionTestHead(uint64(160 + heads)), nil
	}
	processes, err := fixture.driver.Restore(t.Context(), fixture.fault)
	retained, readErr := os.ReadFile(fixture.driver.activePath())
	if !processRestartPending(fixture.fault, err) || len(processes) != 0 || readErr != nil || !bytes.Equal(retained, fixture.intent) || fixture.driver.faultCompleted.faultId != "" {
		t.Fatalf("head read stamped an exited replacement: processes=%+v err=%v read=%v completion=%+v", processes, err, readErr, fixture.driver.faultCompleted)
	}
	if processes, err := fixture.driver.Restore(t.Context(), fixture.fault); !processRestartPending(fixture.fault, err) || len(processes) != 0 || heads != 1 {
		t.Fatalf("old proofs crossed new generation's completion boundary: %+v %v heads=%d", processes, err, heads)
	}
	for noId := range fixture.paths {
		fixture.proof(t, noId+1, fixture.started+12000, nil)
	}
	processes, err = fixture.driver.Restore(t.Context(), fixture.fault)
	if err != nil || len(processes) != 1 || processes[0].PID != nextPid || processes[0].StartTimeTicks != nextTicks || heads != 2 || fixture.driver.faultCompleted.head.Number != 162 {
		t.Fatalf("fresh next generation lost its fresh completion head: %+v %v heads=%d completion=%+v", processes, err, heads, fixture.driver.faultCompleted)
	}
}

// A successful head does not weaken the kernel or approved-identity refusal.
// Rejected observations discard their completion stamp and retain the intent.
func TestValidatorRestartCompletionReadRejectsChangedAuthority(t *testing.T) {
	fixture := newValidatorRestartFixture(t)
	prior := fixture.state.Processes[0]
	for noId := range fixture.paths {
		fixture.proof(t, noId+1, fixture.started+2000, nil)
	}
	for _, test := range []struct {
		name string
		edit func(*ProcessState)
	}{
		{name: "identity", edit: func(state *ProcessState) { state.Identity = "synthetic-foreign-owner" }},
		{name: "kernel generation", edit: func(state *ProcessState) { state.StartTimeTicks++; state.Restarts++ }},
	} {
		fixture.state.Processes[0] = prior
		publishValidatorReadinessState(t, fixture)
		heads := 0
		fixture.driver.faultCompletionHead = func(context.Context) (ChainHead, error) {
			heads++
			test.edit(&fixture.state.Processes[0])
			publishValidatorReadinessState(t, fixture)
			return faultCompletionTestHead(160), nil
		}
		processes, err := fixture.driver.Restore(t.Context(), fixture.fault)
		retained, readErr := os.ReadFile(fixture.driver.activePath())
		if err == nil || processRestartPending(fixture.fault, err) || len(processes) != 0 || readErr != nil || !bytes.Equal(retained, fixture.intent) || heads != 1 || fixture.driver.faultCompleted.faultId != "" {
			t.Fatalf("%s changed after readiness without strict refusal: processes=%+v err=%v read=%v heads=%d completion=%+v", test.name, processes, err, readErr, heads, fixture.driver.faultCompleted)
		}
	}
}

// A head callback may complete as its owner cancels. No readiness verdict or
// stale completion stamp may survive that cancellation boundary.
func TestValidatorRestartCompletionReadPreservesOwnerCancellation(t *testing.T) {
	fixture := newValidatorRestartFixture(t)
	for noId := range fixture.paths {
		fixture.proof(t, noId+1, fixture.started+2000, nil)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	fixture.driver.faultCompletionHead = func(context.Context) (ChainHead, error) {
		cancel()
		return faultCompletionTestHead(160), nil
	}
	processes, err := fixture.driver.Restore(ctx, fixture.fault)
	retained, readErr := os.ReadFile(fixture.driver.activePath())
	if !errors.Is(err, context.Canceled) || processRestartPending(fixture.fault, err) || readErr != nil || !bytes.Equal(retained, fixture.intent) || fixture.driver.faultCompleted.faultId != "" {
		t.Fatalf("owner cancellation acquired completion: processes=%+v err=%v read=%v completion=%+v", processes, err, readErr, fixture.driver.faultCompleted)
	}
}
