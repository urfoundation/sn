// Forced per-member failures prove bounded rounds advance through a large
// census, while new owners and generations still reconcile retained telemetry.
package main

import (
	"context"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
)

// A completed live reconciliation survives sibling timeouts. Every old version
// re-read the first member here, preventing bounded batches from moving forward.
func TestMinerControlRoundsDoNotRepeatCompletedPrefix(t *testing.T) {
	fixture := newMinerControlTestFixture(t, "miner-1", "miner-2", "miner-3")
	var round atomic.Int64
	round.Store(1)
	fixture.driver.minerControlClient.Transport = minerControlTestTransport(func(request *http.Request) (*http.Response, error) {
		target := strings.Split(strings.Trim(request.URL.Path, "/"), "/")[1]
		current := round.Load()
		if current > 1 && target == "miner-1" || current > 2 && target == "miner-2" {
			t.Errorf("round %d repeated completed %s", current, target)
		}
		if current == 1 && target != "miner-1" || current == 2 && target == "miner-3" {
			return nil, context.DeadlineExceeded
		}
		return fixture.roundTrip(request)
	})
	for current := int64(1); current <= 3; current++ {
		round.Store(current)
		_, err := fixture.driver.Apply(t.Context(), fixture.fault)
		if current < 3 && !minerControlPending(err) || current == 3 && err != nil {
			t.Fatalf("round %d: %v", current, err)
		}
		if progress := fixture.progress(t); progress.CompletedCount != int(current) {
			t.Fatalf("round %d lost its completed prefix: %+v", current, progress)
		}
	}
	fixture.requirePosts(t, "/control/miner-1/disable", "/control/miner-2/disable", "/control/miner-3/disable")
	if len(fixture.driver.minerControlReconciled) != 0 {
		t.Fatal("completed transition retained retry authority")
	}
	// The opposite action must query and mutate all three members again.
	fixture.driver.minerControlClient.Transport = minerControlTestTransport(fixture.roundTrip)
	if _, err := fixture.driver.Restore(t.Context(), fixture.fault); err != nil {
		t.Fatal(err)
	}
	fixture.requirePosts(t, "/control/miner-1/disable", "/control/miner-2/disable", "/control/miner-3/disable", "/control/miner-1/enable", "/control/miner-2/enable", "/control/miner-3/enable")
}

// A fresh driver or a restarted owning worker invalidates only the in-memory
// cursor; it must not trust an earlier completion after state has changed.
func TestMinerControlRoundsReconcileAfterOwnerOrGenerationChange(t *testing.T) {
	for _, restartOwner := range []bool{false, true} {
		name := "worker-generation"
		if restartOwner {
			name = "driver-owner"
		}
		t.Run(name, func(t *testing.T) {
			fixture := newMinerControlTestFixture(t, "miner-1", "miner-2")
			fixture.driver.minerControlClient.Transport = minerControlTestTransport(func(request *http.Request) (*http.Response, error) {
				if strings.Contains(request.URL.Path, "miner-2") {
					return nil, context.DeadlineExceeded
				}
				return fixture.roundTrip(request)
			})
			if _, err := fixture.driver.Apply(t.Context(), fixture.fault); !minerControlPending(err) {
				t.Fatal(err)
			}
			if progress := fixture.progress(t); progress.CompletedCount != 1 {
				t.Fatalf("missing partial checkpoint: %+v", progress)
			}
			fixture.stateLock.Lock()
			fixture.states["miner-1"] = "running"
			fixture.stateLock.Unlock()
			if restartOwner {
				fixture.driver = &liveScenarioFaultDriver{
					stateDir: fixture.driver.stateDir, cfg: fixture.driver.cfg,
					minerControlURL: fixture.driver.minerControlURL, minerControlWait: fixture.driver.minerControlWait,
					minerControlParallel: 1,
				}
			} else {
				path := filepath.Join(fixture.driver.stateDir, "supervisor.state.json")
				var state SupervisorState
				if err := readJSONFile(path, &state); err != nil {
					t.Fatal(err)
				}
				// Same-PID reuse still differs by generation.
				state.Processes[0].StartedAt = "2026-09-22T20:00:00Z"
				state.Processes[0].Restarts++
				if err := writePublicJSON(path, state); err != nil {
					t.Fatal(err)
				}
			}
			fixture.driver.minerControlClient = &http.Client{Transport: minerControlTestTransport(fixture.roundTrip)}
			if _, err := fixture.driver.Apply(t.Context(), fixture.fault); err != nil {
				t.Fatal(err)
			}
			posts := func() []string {
				fixture.stateLock.Lock()
				defer fixture.stateLock.Unlock()
				return slices.Clone(fixture.posts)
			}()
			slices.Sort(posts)
			if !slices.Equal(posts, []string{"/control/miner-1/disable", "/control/miner-1/disable", "/control/miner-2/disable"}) {
				t.Fatalf("reconciliation lost a changed member or duplicated a mutation: %v", posts)
			}
		})
	}
}
