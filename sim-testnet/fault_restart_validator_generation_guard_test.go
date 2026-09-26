//go:build linux

// Replacement retry is narrower than identity or integrity refusal. Force each
// independent invalid state at the same read barrier as legitimate churn.
package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A joined, approved snapshot never turns a wrong kernel generation, changed
// durable owner or backward restart history into an availability exception.
func TestValidatorRestartReplacementReadRejectsChangedAuthority(t *testing.T) {
	fixture := newValidatorRestartFixture(t)
	fixture.state.Processes[0].Restarts = 2
	prior := fixture.state.Processes[0]
	nextPid, nextTicks, _ := startValidatorReadinessChild(t)
	for noId := range fixture.paths {
		fixture.proof(t, noId+1, fixture.started+2000, nil)
	}
	signals, completedHeads := 0, 0
	fixture.driver.restartSignal = func(supervisedCommand, syscall.Signal) bool { signals++; return false }
	fixture.driver.faultCompletionHead = func(context.Context) (ChainHead, error) {
		completedHeads++
		return faultCompletionTestHead(160), nil
	}
	for _, test := range []struct {
		name string
		edit func(*ProcessState)
	}{
		{name: "foreign role", edit: func(state *ProcessState) { state.Role = "operator-api" }},
		{name: "foreign identity", edit: func(state *ProcessState) { state.Identity = "synthetic-foreign-owner" }},
		{name: "foreign process", edit: func(state *ProcessState) { state.ID = "validator-99" }},
		{name: "missing ticks", edit: func(state *ProcessState) { state.StartTimeTicks = 0 }},
		{name: "wrong ticks", edit: func(state *ProcessState) { state.StartTimeTicks++; state.Restarts++ }},
		{name: "unchanged restart count", edit: func(state *ProcessState) { state.PID, state.StartTimeTicks = nextPid, nextTicks }},
		{name: "backward restart count", edit: func(state *ProcessState) { state.Restarts-- }},
		{name: "backward start", edit: func(state *ProcessState) {
			state.PID, state.StartTimeTicks, state.Restarts = nextPid, nextTicks, prior.Restarts+1
			state.StartedAt = time.UnixMilli(int64(fixture.started - 1000)).UTC().Format(time.RFC3339)
		}},
		{name: "same generation changed start", edit: func(state *ProcessState) {
			state.StartedAt = time.UnixMilli(int64(fixture.started + 1000)).UTC().Format(time.RFC3339)
		}},
		{name: "same generation changed count", edit: func(state *ProcessState) { state.Restarts++ }},
		{name: "invalid pid", edit: func(state *ProcessState) { state.PID = 1 }},
		{name: "stopped still healthy", edit: func(state *ProcessState) { state.PID, state.StartTimeTicks = 0, 0 }},
		{name: "stopped retained ticks", edit: func(state *ProcessState) { state.PID, state.Healthy = 0, false }},
	} {
		fixture.state.Processes[0] = prior
		publishValidatorReadinessState(t, fixture)
		processes, err := runValidatorReadinessBarrier(t, fixture, t.Context(), func() {
			test.edit(&fixture.state.Processes[0])
			publishValidatorReadinessState(t, fixture)
		})
		retained, readErr := os.ReadFile(fixture.driver.activePath())
		if err == nil || processRestartPending(fixture.fault, err) || len(processes) != 0 || readErr != nil || !bytes.Equal(retained, fixture.intent) {
			t.Fatalf("%s borrowed pending or completed authority: processes=%+v err=%v read=%v", test.name, processes, err, readErr)
		}
	}
	fixture.state.Processes[0] = prior
	publishValidatorReadinessState(t, fixture)
	processes, err := runValidatorReadinessBarrier(t, fixture, t.Context(), func() {
		fixture.state.ManifestHash = "0x" + strings.Repeat("e1", 32)
		publishValidatorReadinessState(t, fixture)
	})
	if err == nil || processRestartPending(fixture.fault, err) || len(processes) != 0 || signals != 0 || completedHeads != 0 {
		t.Fatalf("changed manifest acquired authority or mutation: processes=%+v err=%v signals=%d heads=%d", processes, err, signals, completedHeads)
	}
}

// Cancellation after a successful read still belongs to the operation owner;
// no availability marker, extra signal or post-action head can hide it.
func TestValidatorRestartReplacementReadPreservesOwnerCancellation(t *testing.T) {
	fixture := newValidatorRestartFixture(t)
	for noId := range fixture.paths {
		fixture.proof(t, noId+1, fixture.started+2000, nil)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	processes, err := runValidatorReadinessBarrier(t, fixture, ctx, cancel)
	retained, readErr := os.ReadFile(fixture.driver.activePath())
	if !errors.Is(err, context.Canceled) || processRestartPending(fixture.fault, err) || len(processes) != 0 || readErr != nil || !bytes.Equal(retained, fixture.intent) {
		t.Fatalf("owner cancellation became readiness: processes=%+v err=%v read=%v", processes, err, readErr)
	}
}
