package main

// Retained startup uses the real disk proof and log gates with a controlled
// clock. No elapsed-time assertion or wall-clock sleep decides these tests.
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/urnetwork/connect/v2026"
)

// Every fixture has both validators, a provider, four retained proof domains,
// and the same immutable supervisor generation throughout its wait.
type retainedStartupReadinessFixture struct {
	processLogGateFixture
	cfg      *ResolvedConfig
	baseline map[string]int
	paths    map[string]string
}

// Uses real generation ownership and append-only files without child services.
func newRetainedStartupReadinessFixture(t *testing.T) retainedStartupReadinessFixture {
	t.Helper()
	fixture := newProcessLogGateFixture(t, "", "")
	fixture.manifest.Specs[0].Role = "miner-swarm"
	fixture.supervisor.Processes[0].Role = "miner-swarm"
	for id := 1; id <= 2; id++ {
		identity := fmt.Sprintf("validator-%d", id)
		stdoutPath := filepath.Join(fixture.dir, "processes", identity+".stdout.log")
		stderrPath := filepath.Join(fixture.dir, "processes", identity+".stderr.log")
		for _, path := range []string{stdoutPath, stderrPath} {
			if err := os.WriteFile(path, nil, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		fixture.manifest.Specs = append(fixture.manifest.Specs, ProcessSpec{ID: identity, Role: "validator", Identity: identity, StdoutPath: stdoutPath, StderrPath: stderrPath})
		fixture.supervisor.Processes = append(fixture.supervisor.Processes, ProcessState{ID: identity, Role: "validator", Identity: identity, PID: os.Getpid(), Healthy: true})
	}
	var err error
	fixture.supervisor.ManifestHash, err = canonicalHashHex(fixture.manifest)
	if err != nil {
		t.Fatal(err)
	}
	fixture.gate, err = initializeProcessLogGate(fixture.dir, fixture.manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.gate.Bind(fixture.supervisor); err != nil {
		t.Fatal(err)
	}
	cfg := &ResolvedConfig{Config: &HarnessConfig{Topology: TopologyConfig{Validators: 2, Operators: 2}}, OperationalRPCMode: rpcModeOwnedNode, strictHistoryAdoption: &strictHistoryAdoptionState{}}
	result := retainedStartupReadinessFixture{processLogGateFixture: fixture, cfg: cfg, baseline: map[string]int{}, paths: releaseTopologyProofPaths(cfg, fixture.dir)}
	result.writeState(t)
	for identity, path := range result.paths {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(releaseProofTestLine(t, 1, connect.Id{1}, 3, 1)+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		result.baseline[identity] = 1
	}
	return result
}

// Mutations publish the same snapshot format the real supervisor reader uses.
func (self *retainedStartupReadinessFixture) writeState(t *testing.T) {
	t.Helper()
	raw, err := json.Marshal(self.supervisor)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(self.dir, "supervisor.state.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

// Each operator domain receives a new complete trail exactly once.
func (self *retainedStartupReadinessFixture) publish(t *testing.T, omitted string) {
	t.Helper()
	for identity, path := range self.paths {
		if identity != omitted {
			appendProcessLog(t, path, releaseProofTestLine(t, 1, connect.Id{2}, 3, 2)+"\n")
		}
	}
}

// A poll may resume at a supplied later instant; all intervening deadline
// decisions still use the production loop and its actual disk readers.
func (self *retainedStartupReadinessFixture) wait(t *testing.T, ctx context.Context, timeout, step time.Duration, onWait func(int)) (int, error) {
	t.Helper()
	now := time.Unix(1_800_000_000, 0)
	waits := 0
	err := waitReleaseTopologyReadyWithClock(ctx, self.cfg, self.dir, self.manifest, self.supervisor.SupervisorPID, self.supervisor.SupervisorStartTimeTicks, self.baseline, self.gate, timeout, func() time.Time { return now }, func(ctx context.Context, delay time.Duration) error {
		if delay <= 0 || delay > 500*time.Millisecond {
			t.Fatalf("poll delay = %s", delay)
		}
		waits++
		now = now.Add(step)
		if onWait != nil {
			onWait(waits)
		}
		return ctx.Err()
	})
	return waits, err
}

// A complete retained replay can exceed five minutes on the owned LAN just as
// on public RPC. Readiness still requires every fresh proof after replay.
func TestReleaseTopologyReadinessRetainedHistoryWarmupIsRouteIndependent(t *testing.T) {
	for _, mode := range []string{rpcModeOwnedNode, rpcModePrivateAuthority, rpcModePublicOverride, ""} {
		fixture := newRetainedStartupReadinessFixture(t)
		fixture.cfg.OperationalRPCMode = mode
		waits, err := fixture.wait(t, context.Background(), releaseTopologyStartupReadinessTimeout(fixture.cfg), 6*time.Minute, func(int) { fixture.publish(t, "") })
		if err != nil || waits != 1 {
			t.Fatalf("retained route %q canceled before the first complete fresh proofs: waits=%d err=%v", mode, waits, err)
		}
	}
	for _, cfg := range []*ResolvedConfig{nil, {}, {OperationalRPCMode: rpcModeOwnedNode}, {OperationalRPCMode: rpcModePrivateAuthority}} {
		if got := releaseTopologyStartupReadinessTimeout(cfg); got != 5*time.Minute {
			t.Fatalf("fresh ordinary startup budget = %s, want five minutes", got)
		}
	}
	if got := releaseTopologyStartupReadinessTimeout(&ResolvedConfig{OperationalRPCMode: rpcModePublicOverride}); got != 30*time.Minute {
		t.Fatalf("existing public startup budget = %s", got)
	}
}

// More warmup time cannot turn old, partial or deadline-late proofs into
// semantic readiness. This checks the independent bound, not its selector.
func TestReleaseTopologyReadinessRetainedHistoryWaitKeepsProofDeadline(t *testing.T) {
	for _, name := range []string{"stale", "one domain stale", "all proofs at deadline"} {
		fixture := newRetainedStartupReadinessFixture(t)
		waits, err := fixture.wait(t, context.Background(), 30*time.Minute, 6*time.Minute, func(poll int) {
			if name == "one domain stale" && poll == 1 {
				fixture.publish(t, "validator-2/no-2")
			}
			if name == "all proofs at deadline" && poll == 5 {
				fixture.publish(t, "")
			}
		})
		if err == nil || !strings.Contains(err.Error(), "semantic readiness timeout") || waits != 5 {
			t.Fatalf("%s accepted incomplete or late proofs: waits=%d err=%v", name, waits, err)
		}
	}
}

// Cancellation precedes both initial admission and any later proof snapshot;
// retained replay does not acquire an uncancelable extended readiness budget.
func TestReleaseTopologyReadinessRetainedHistoryWaitCancellationPrecedesAdmission(t *testing.T) {
	for _, alreadyCanceled := range []bool{true, false} {
		fixture := newRetainedStartupReadinessFixture(t)
		ctx, cancel := context.WithCancel(context.Background())
		if alreadyCanceled {
			fixture.publish(t, "")
			cancel()
		}
		waits, err := fixture.wait(t, ctx, 30*time.Minute, 6*time.Minute, func(int) {
			fixture.publish(t, "")
			cancel()
		})
		cancel()
		wantWaits := 1
		if alreadyCanceled {
			wantWaits = 0
		}
		if !errors.Is(err, context.Canceled) || waits != wantWaits {
			t.Fatalf("already canceled=%t: waits=%d err=%v", alreadyCanceled, waits, err)
		}
	}
}

// Health, restart, generation, log and proof-integrity refusals stay active
// after the old five-minute boundary even when all four counts have advanced.
func TestReleaseTopologyReadinessRetainedHistoryWaitKeepsSafetyGates(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*retainedStartupReadinessFixture)
		want   string
	}{
		{name: "validator unhealthy", mutate: func(f *retainedStartupReadinessFixture) { f.supervisor.Processes[1].Healthy = false; f.writeState(t) }, want: "semantic readiness timeout"},
		{name: "provider unhealthy", mutate: func(f *retainedStartupReadinessFixture) { f.supervisor.Processes[0].Healthy = false; f.writeState(t) }, want: "semantic readiness timeout"},
		{name: "child restarted", mutate: func(f *retainedStartupReadinessFixture) { f.supervisor.Processes[2].Restarts = 1; f.writeState(t) }, want: "restarted 1"},
		{name: "supervisor replaced", mutate: func(f *retainedStartupReadinessFixture) { f.supervisor.SupervisorStartTimeTicks++; f.writeState(t) }, want: "generation changed"},
		{name: "blocking log", mutate: func(f *retainedStartupReadinessFixture) {
			appendProcessLog(t, f.stderrPath, "error: readiness fixture failure\n")
		}, want: "release-blocking"},
		{name: "durable proof corruption", mutate: func(f *retainedStartupReadinessFixture) { appendProcessLog(t, f.paths["validator-2/no-2"], "{\n") }, want: "is malformed"},
	}
	for _, test := range tests {
		fixture := newRetainedStartupReadinessFixture(t)
		waits, err := fixture.wait(t, context.Background(), 30*time.Minute, 6*time.Minute, func(poll int) {
			if poll == 1 {
				fixture.publish(t, "")
				test.mutate(&fixture)
			}
		})
		if err == nil || !strings.Contains(err.Error(), test.want) || waits < 1 || waits > 5 {
			t.Fatalf("%s gate: waits=%d err=%v", test.name, waits, err)
		}
	}
	fixture := newRetainedStartupReadinessFixture(t)
	fixture.gate = nil
	fixture.publish(t, "")
	if waits, err := fixture.wait(t, context.Background(), 30*time.Minute, 6*time.Minute, nil); err == nil || !strings.Contains(err.Error(), "requires the process log gate") || waits != 0 {
		t.Fatalf("absent log gate admitted fresh proofs: waits=%d err=%v", waits, err)
	}
}
