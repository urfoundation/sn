// Explicit supervisor transitions reproduce restart races before control.
// Integrity failures never acquire readiness or retained-progress authority.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// Change only the public supervisor observation, preserving the locked manifest.
func changeMinerControlTestOwners(t *testing.T, fixture *minerControlTestFixture, change func(*SupervisorState)) {
	t.Helper()
	path := filepath.Join(fixture.driver.stateDir, "supervisor.state.json")
	var state SupervisorState
	if err := readJSONFile(path, &state); err != nil {
		t.Fatal(err)
	}
	change(&state)
	if err := writePublicJSON(path, state); err != nil {
		t.Fatal(err)
	}
}

// The missing process window precedes durable dispatch and any member mutation.
func TestMinerControlOwnerRecoveryWaitsBeforeFirstMutation(t *testing.T) {
	fixture := newMinerControlTestFixture(t, "miner-1")
	changeMinerControlTestOwners(t, fixture, func(state *SupervisorState) {
		state.Processes[0].PID, state.Processes[0].Healthy = 0, false
	})
	waits := 0
	fixture.driver.minerControlWait = func(ctx context.Context, delay time.Duration) error {
		waits++
		deadline, bounded := ctx.Deadline()
		if !bounded || time.Until(deadline) > minerControlTargetTimeout || delay != minerControlRetryDelay {
			t.Fatal("owner readiness lost its finite budget")
		}
		if _, err := os.Stat(fixture.driver.activePath()); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("dispatch evidence preceded actual owner readiness: %v", err)
		}
		fixture.requirePosts(t)
		changeMinerControlTestOwners(t, fixture, func(state *SupervisorState) {
			state.Processes[0].PID, state.Processes[0].Healthy = 2234, true
			state.Processes[0].Restarts++
		})
		return nil
	}
	processes, err := fixture.driver.Apply(t.Context(), fixture.fault)
	if err != nil || waits != 1 || len(processes) != 1 || processes[0].PID != 2234 {
		t.Fatalf("known owner restart interrupted activation: waits=%d processes=%+v error=%v", waits, processes, err)
	}
	if progress := fixture.progress(t); progress.Phase != "active" || progress.CompletedCount != 1 {
		t.Fatalf("recovered control was not durably completed: %+v", progress)
	}
	fixture.requirePosts(t, "/control/miner-1/disable")
}

// A new owning generation requires fresh member reconciliation while retaining
// the prior durable prefix until the corresponding replacement is available.
func TestMinerControlOwnerRecoveryRetainsPartialProgress(t *testing.T) {
	fixture := newMinerControlTestFixture(t, "miner-1", "miner-2")
	fixture.driver.minerControlClient.Transport = minerControlTestTransport(func(request *http.Request) (*http.Response, error) {
		if strings.Contains(request.URL.Path, "miner-2") {
			return nil, context.DeadlineExceeded
		}
		return fixture.roundTrip(request)
	})
	if _, err := fixture.driver.Apply(t.Context(), fixture.fault); !minerControlPending(err) {
		t.Fatalf("initial partial dispatch=%v", err)
	}
	changeMinerControlTestOwners(t, fixture, func(state *SupervisorState) {
		state.Processes[0].PID, state.Processes[0].Healthy = 0, false
	})
	waits := 0
	fixture.driver.minerControlWait = func(context.Context, time.Duration) error {
		waits++
		if progress := fixture.progress(t); progress.CompletedCount != 1 || progress.Phase != "applying" {
			t.Fatalf("readiness discarded the completed prefix: %+v", progress)
		}
		fixture.requirePosts(t, "/control/miner-1/disable")
		changeMinerControlTestOwners(t, fixture, func(state *SupervisorState) {
			state.Processes[0].PID, state.Processes[0].Healthy = 2234, true
			state.Processes[0].Restarts++
		})
		fixture.stateLock.Lock()
		fixture.states["miner-1"] = "running"
		fixture.stateLock.Unlock()
		return nil
	}
	fixture.driver.minerControlClient.Transport = minerControlTestTransport(fixture.roundTrip)
	if _, err := fixture.driver.Apply(t.Context(), fixture.fault); err != nil || waits != 1 {
		t.Fatalf("partial controls did not resume after restart: waits=%d error=%v", waits, err)
	}
	if progress := fixture.progress(t); progress.Phase != "active" || progress.CompletedCount != 2 {
		t.Fatalf("replacement census did not complete: %+v", progress)
	}
	fixture.stateLock.Lock()
	posts := slices.Clone(fixture.posts)
	fixture.stateLock.Unlock()
	slices.Sort(posts)
	if !slices.Equal(posts, []string{"/control/miner-1/disable", "/control/miner-1/disable", "/control/miner-2/disable"}) {
		t.Fatalf("replacement was not freshly reconciled: %v", posts)
	}
}

// Restore may repair an unhealthy live owner, but cannot mutate an absent PID.
func TestMinerControlOwnerRecoveryRestoresThroughReplacement(t *testing.T) {
	fixture := newMinerControlTestFixture(t, "miner-1")
	fixture.apply(t)
	changeMinerControlTestOwners(t, fixture, func(state *SupervisorState) {
		state.Processes[0].PID, state.Processes[0].Healthy = 0, false
	})
	waits := 0
	fixture.driver.minerControlWait = func(context.Context, time.Duration) error {
		waits++
		fixture.requirePosts(t)
		changeMinerControlTestOwners(t, fixture, func(state *SupervisorState) {
			state.Processes[0].PID = 2234
			state.Processes[0].Restarts++
		})
		return nil
	}
	if _, err := fixture.driver.Restore(t.Context(), fixture.fault); err != nil || waits != 1 {
		t.Fatalf("restore could not use a replacement owner: waits=%d error=%v", waits, err)
	}
	fixture.requirePosts(t, "/control/miner-1/enable")
	if _, err := os.Stat(fixture.driver.activePath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("completed restoration retained active intent: %v", err)
	}
}

// A valid unavailable row cannot mask an invalid later owner or checksum.
func TestMinerControlOwnerRecoveryRejectsMixedIntegrityBeforeWaiting(t *testing.T) {
	changes := []func(*SupervisorState){
		func(state *SupervisorState) { state.Processes[1].Identity = "synthetic-substituted-owner" },
		func(state *SupervisorState) { state.Processes[1].Role = "synthetic-other-role" },
		func(state *SupervisorState) { state.Processes[1].PID = -1 },
		func(state *SupervisorState) { state.Processes[1].PID = 1 },
		func(state *SupervisorState) { state.Processes[1].Restarts = -1 },
		func(state *SupervisorState) { state.Processes = state.Processes[:1] },
		func(state *SupervisorState) { state.ManifestHash = "synthetic-wrong-checksum" },
	}
	for index, change := range changes {
		fixture := newMinerControlTestFixture(t, "miner-1")
		perSwarm := fixture.driver.cfg.Config.Topology.Miners / fixture.driver.cfg.Config.Topology.MinerSwarmProcesses
		fixture.fault.Targets = append(fixture.fault.Targets, fmt.Sprintf("miner-%d", perSwarm+1))
		installMinerControlTestSwarms(t, fixture)
		changeMinerControlTestOwners(t, fixture, func(state *SupervisorState) {
			state.Processes[0].PID, state.Processes[0].Healthy = 0, false
			change(state)
		})
		fixture.driver.minerControlWait = func(context.Context, time.Duration) error {
			t.Errorf("case %d retried an invalid owner", index)
			return context.Canceled
		}
		if _, err := fixture.driver.Apply(t.Context(), fixture.fault); err == nil || minerControlPending(err) {
			t.Fatalf("case %d admitted invalid owner: %v", index, err)
		}
		fixture.requirePosts(t)
	}
}

// Parent cancellation and exhausted readiness keep both first dispatch and
// mutation absent, without inventing a PID or removing a prior checkpoint.
func TestMinerControlOwnerRecoveryHonorsCancellationAndDeadline(t *testing.T) {
	for _, failure := range []error{context.Canceled, context.DeadlineExceeded} {
		fixture := newMinerControlTestFixture(t, "miner-1")
		changeMinerControlTestOwners(t, fixture, func(state *SupervisorState) {
			state.Processes[0].Healthy = false
		})
		waits := 0
		fixture.driver.minerControlWait = func(context.Context, time.Duration) error {
			waits++
			return failure
		}
		if _, err := fixture.driver.Apply(t.Context(), fixture.fault); !errors.Is(err, failure) || waits != 1 {
			t.Fatalf("readiness escaped its owner: waits=%d error=%v", waits, err)
		}
		fixture.requirePosts(t)
		if _, err := os.Stat(fixture.driver.activePath()); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("failed readiness created active control: %v", err)
		}
	}
}

// The narrow read/read race retries one retained round, with no stale-owner
// request or fabricated completion; the next attempt reconciles its new PID.
func TestMinerControlOwnerRecoveryDefersGenerationRace(t *testing.T) {
	fixture := newMinerControlTestFixture(t, "miner-1")
	processes, err := fixture.driver.minerControlProcesses(fixture.fault, false)
	if err != nil {
		t.Fatal(err)
	}
	faultHash, err := canonicalHashHex(fixture.fault)
	if err != nil {
		t.Fatal(err)
	}
	progress := minerFaultControlProgress{FaultId: fixture.fault.ID, FaultHash: faultHash, Phase: "applying", Total: len(processes)}
	if err := appendActiveFault(fixture.driver.activePath(), activeFaultFile{Schema: minerControlProgressSchema, MinerControls: []minerFaultControlProgress{progress}}, fixture.fault, processes); err != nil {
		t.Fatal(err)
	}
	changeMinerControlTestOwners(t, fixture, func(state *SupervisorState) {
		state.Processes[0].PID = 2234
		state.Processes[0].Restarts++
	})
	err = fixture.driver.controlMinerRound(t.Context(), processes, "disable", &progress, func() error {
		t.Error("stale owner admitted a dispatch before reconciliation")
		return nil
	})
	if !minerControlPending(err) || progress.Attempts != 0 || progress.CompletedCount != 0 {
		t.Fatalf("generation turnover became terminal or completed: progress=%+v error=%v", progress, err)
	}
	fixture.requirePosts(t)
	if _, err := fixture.driver.Apply(t.Context(), fixture.fault); err != nil {
		t.Fatal(err)
	}
	if progress := fixture.progress(t); progress.CompletedCount != 1 || progress.Completed[0].PID != 2234 {
		t.Fatalf("next round lost replacement ownership: %+v", progress)
	}
	fixture.requirePosts(t, "/control/miner-1/disable")
}

// A PID transition cannot downgrade an unrelated identity failure in the same
// pre-dispatch census, even though the first member is normally recoverable.
func TestMinerControlOwnerRecoveryGenerationIntegrityStaysHard(t *testing.T) {
	fixture := newMinerControlTestFixture(t, "miner-1")
	perSwarm := fixture.driver.cfg.Config.Topology.Miners / fixture.driver.cfg.Config.Topology.MinerSwarmProcesses
	fixture.fault.Targets = append(fixture.fault.Targets, fmt.Sprintf("miner-%d", perSwarm+1))
	installMinerControlTestSwarms(t, fixture)
	processes, err := fixture.driver.minerControlProcesses(fixture.fault, false)
	if err != nil {
		t.Fatal(err)
	}
	changeMinerControlTestOwners(t, fixture, func(state *SupervisorState) {
		state.Processes[0].PID = 0
		state.Processes[1].Identity = "synthetic-other-owner"
	})
	if _, err := fixture.driver.minerControlGenerations(processes); err == nil || minerControlPending(err) {
		t.Fatalf("generation readiness masked an owner substitution: %v", err)
	}
	fixture.requirePosts(t)
}

// A newly self-consistent manifest cannot change the owner already pinned by
// a readiness wait, even when both its state and checksum are substituted.
func TestMinerControlOwnerRecoveryRejectsRebindingDuringWait(t *testing.T) {
	fixture := newMinerControlTestFixture(t, "miner-1")
	changeMinerControlTestOwners(t, fixture, func(state *SupervisorState) {
		state.Processes[0].Healthy = false
	})
	waits := 0
	fixture.driver.minerControlWait = func(context.Context, time.Duration) error {
		waits++
		path := filepath.Join(fixture.driver.stateDir, "supervisor.json")
		var manifest SupervisorFile
		if err := readJSONFile(path, &manifest); err != nil {
			t.Fatal(err)
		}
		manifest.Specs[0].Identity = "synthetic-rebound-owner"
		hash, err := canonicalHashHex(manifest)
		if err != nil {
			t.Fatal(err)
		}
		if err := writePublicJSON(path, manifest); err != nil {
			t.Fatal(err)
		}
		changeMinerControlTestOwners(t, fixture, func(state *SupervisorState) {
			state.ManifestHash = hash
			state.Processes[0].Identity = manifest.Specs[0].Identity
			state.Processes[0].Healthy = true
		})
		return nil
	}
	if _, err := fixture.driver.Apply(t.Context(), fixture.fault); err == nil || !strings.Contains(err.Error(), "owner identity changed") || waits != 1 {
		t.Fatalf("readiness adopted substituted authority: waits=%d error=%v", waits, err)
	}
	fixture.requirePosts(t)
}
