// Barrier-driven control rounds prove parallel bounds, ambiguous mutation
// recovery and exact fault windows without relying on wall-clock timing.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
)

// Install the exact configured owning swarms for a multi-swarm test census.
func installMinerControlTestSwarms(t *testing.T, fixture *minerControlTestFixture) {
	t.Helper()
	manifest := SupervisorFile{Schema: "urnetwork-sim-supervisor-v1", DeploymentID: "synthetic-control-test", BinaryHash: "synthetic-binary-hash"}
	state := SupervisorState{Schema: "urnetwork-sim-supervisor-state-v1", SupervisorPID: os.Getpid()}
	for swarm := 1; swarm <= fixture.driver.cfg.Config.Topology.MinerSwarmProcesses; swarm++ {
		process := ProcessSpec{ID: fmt.Sprintf("miner-swarm-%d", swarm), Role: "miner-swarm", Identity: fmt.Sprintf("synthetic-miner-swarm-%d", swarm)}
		manifest.Specs = append(manifest.Specs, process)
		state.Processes = append(state.Processes, ProcessState{ID: process.ID, Role: process.Role, Identity: process.Identity, PID: 1234 + swarm, Healthy: true})
	}
	var err error
	state.ManifestHash, err = canonicalHashHex(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := writePublicJSON(filepath.Join(fixture.driver.stateDir, "supervisor.json"), manifest); err != nil {
		t.Fatal(err)
	}
	if err := writePublicJSON(filepath.Join(fixture.driver.stateDir, "supervisor.state.json"), state); err != nil {
		t.Fatal(err)
	}
}

// Four independent swarms may make progress while each retains a four-member
// bound; the seventeenth request cannot precede a completed worker.
func TestMinerControlParallelBoundsPersistEveryInFlightTarget(t *testing.T) {
	fixture := newMinerControlTestFixture(t)
	perSwarm := fixture.driver.cfg.Config.Topology.Miners / fixture.driver.cfg.Config.Topology.MinerSwarmProcesses
	for swarm := 0; swarm < 5; swarm++ {
		for member := 1; member <= 5; member++ {
			target := fmt.Sprintf("miner-%d", swarm*perSwarm+member)
			fixture.fault.Targets = append(fixture.fault.Targets, target)
			fixture.states[target] = "running"
		}
	}
	installMinerControlTestSwarms(t, fixture)
	fixture.driver.minerControlParallel = 0
	entered := make(chan string, len(fixture.fault.Targets))
	release := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	var stateLock sync.Mutex
	seenKVs := map[string]bool{}
	fixture.driver.minerControlClient.Transport = minerControlTestTransport(func(request *http.Request) (*http.Response, error) {
		target := strings.Split(strings.Trim(request.URL.Path, "/"), "/")[1]
		first := func() bool {
			stateLock.Lock()
			defer stateLock.Unlock()
			if request.Method != http.MethodGet || seenKVs[target] {
				return false
			}
			seenKVs[target] = true
			return true
		}()
		if first {
			entered <- target
			select {
			case <-release:
			case <-request.Context().Done():
				return nil, request.Context().Err()
			}
		}
		return fixture.roundTrip(request)
	})
	done := make(chan error, 1)
	go func() { _, err := fixture.driver.Apply(ctx, fixture.fault); done <- err }()
	var initial []string
	for len(initial) < minerControlParallelLimit {
		select {
		case target := <-entered:
			initial = append(initial, target)
		case err := <-done:
			t.Fatalf("round ended before the barrier: %v; entered=%v", err, initial)
		}
	}
	progress := fixture.progress(t)
	slices.Sort(initial)
	if !reflect.DeepEqual(progress.PendingTargets, initial) || progress.Attempts != 0 || progress.CompletedCount != 0 {
		t.Fatalf("parallel census preceded durable intent: %+v, entered=%v", progress, initial)
	}
	swarmCounts := map[int]int{}
	for _, target := range initial {
		var miner int
		_, _ = fmt.Sscanf(target, "miner-%d", &miner)
		swarm, err := minerSwarmFor(fixture.driver.cfg, miner)
		if err != nil {
			t.Fatal(err)
		}
		swarmCounts[swarm]++
		if swarmCounts[swarm] > minerControlSwarmLimit {
			t.Fatalf("unbounded swarm dispatch: %v", swarmCounts)
		}
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	progress = fixture.progress(t)
	if progress.Phase != "active" || progress.CompletedCount != len(fixture.fault.Targets) || len(progress.PendingTargets) != 0 || int(progress.Attempts) != len(fixture.fault.Targets) {
		t.Fatalf("incomplete parallel round: %+v", progress)
	}
	fixture.stateLock.Lock()
	defer fixture.stateLock.Unlock()
	if len(fixture.posts) != len(fixture.fault.Targets) {
		t.Fatalf("mutations=%v", fixture.posts)
	}
}

// Every ambiguous in-flight mutation survives the round boundary; a new
// driver confirms live disabled state without replaying any disable.
func TestMinerControlParallelCanceledRoundResumesAllAmbiguousTargets(t *testing.T) {
	fixture := newMinerControlTestFixture(t, "miner-1", "miner-2", "miner-3", "miner-4")
	fixture.driver.minerControlParallel = 4
	roundCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	fixture.driver.minerControlRoundContext = func(context.Context) (context.Context, context.CancelFunc) { return roundCtx, cancel }
	entered := make(chan string, 4)
	fixture.driver.minerControlClient.Transport = minerControlTestTransport(func(request *http.Request) (*http.Response, error) {
		response, err := fixture.roundTrip(request)
		if request.Method == http.MethodPost {
			_ = response.Body.Close()
			entered <- request.URL.Path
			<-request.Context().Done()
			return nil, request.Context().Err()
		}
		return response, err
	})
	done := make(chan error, 1)
	go func() { _, err := fixture.driver.Apply(context.Background(), fixture.fault); done <- err }()
	for count := 0; count < 4; count++ {
		select {
		case <-entered:
		case err := <-done:
			t.Fatalf("round ended before all ambiguous mutations: %v", err)
		}
	}
	if progress := fixture.progress(t); len(progress.PendingTargets) != 4 || progress.Attempts != 4 || progress.CompletedCount != 0 {
		t.Fatalf("missing parallel recovery intent: %+v", progress)
	}
	cancel()
	if err := <-done; !minerControlPending(err) || !errors.Is(err, context.Canceled) {
		t.Fatalf("bounded round became terminal: %v", err)
	}
	reopened := *fixture.driver
	reopened.minerControlRoundContext = nil
	reopened.minerControlClient = &http.Client{Transport: minerControlTestTransport(fixture.roundTrip)}
	if _, err := reopened.Apply(context.Background(), fixture.fault); err != nil {
		t.Fatal(err)
	}
	progress := fixture.progress(t)
	if progress.Phase != "active" || progress.CompletedCount != 4 || len(progress.PendingTargets) != 0 || progress.Attempts != 4 {
		t.Fatalf("restart did not reconcile complete durable intent: %+v", progress)
	}
	fixture.stateLock.Lock()
	defer fixture.stateLock.Unlock()
	if len(fixture.posts) != 4 {
		t.Fatalf("restart duplicated an ambiguous mutation: %v", fixture.posts)
	}
}

// A bounded transient round must leave the scheduler alive, and the complete
// configured fault duration begins at actual finalized control completion.
func TestMinerControlPendingRoundContinuesSchedulerAndPreservesActualWindow(t *testing.T) {
	fixture := newMinerControlTestFixture(t, "miner-1")
	fixture.fault.TriggerOffsetBlocks = 1
	fixture.fault.DurationBlocks = 20
	specs := []scenarioFaultSpec{fixture.fault}
	records, err := initializeFaultRecords(100, specs)
	if err != nil {
		t.Fatal(err)
	}
	fixture.driver.minerControlClient.Transport = minerControlTestTransport(func(request *http.Request) (*http.Response, error) {
		if request.Method == http.MethodPost {
			return nil, context.DeadlineExceeded
		}
		return fixture.roundTrip(request)
	})
	head := ChainHead{Number: 101, Hash: "0x" + strings.Repeat("1", 64)}
	if err := advanceFaults(context.Background(), head, specs, records, fixture.driver); err != nil {
		t.Fatal(err)
	}
	if records[0].Status != "pending" || records[0].ControlStartedBlock != 101 || records[0].ControlPendingRounds != 1 || records[0].AppliedBlock != 0 || faultsComplete(records) {
		t.Fatalf("pending control was lost or certified: %+v", records[0])
	}
	if got := scenarioFaultTargets(records, 102, false); !reflect.DeepEqual(got, []string{"miner-1"}) {
		t.Fatalf("partial control attribution=%v", got)
	}
	if scopes := activeProcessLogFaultScopes(records); len(scopes) != 1 {
		t.Fatalf("partial control process scope=%+v", scopes)
	}
	fixture.driver.minerControlClient.Transport = minerControlTestTransport(fixture.roundTrip)
	completedHead := ChainHead{Number: 108, Hash: "0x" + strings.Repeat("2", 64)}
	fixture.driver.faultCompletionHead = func(context.Context) (ChainHead, error) { return completedHead, nil }
	head.Number = 102
	if err := advanceFaults(context.Background(), head, specs, records, fixture.driver); err != nil {
		t.Fatal(err)
	}
	if records[0].Status != "active" || records[0].AppliedBlock != 108 || records[0].AppliedBlockHash != completedHead.Hash || records[0].TriggerBlock != 101 || records[0].RestoreBlock != 121 || records[0].Error != "" {
		t.Fatalf("completed control backdated its window: %+v", records[0])
	}
	head.Number = 127
	if err := advanceFaults(context.Background(), head, specs, records, fixture.driver); err != nil {
		t.Fatal(err)
	}
	if records[0].Status != "active" {
		t.Fatalf("scheduled restore erased actual fault duration: %+v", records[0])
	}
	completedHead = ChainHead{Number: 129, Hash: "0x" + strings.Repeat("3", 64)}
	head.Number = 128
	if err := advanceFaults(context.Background(), head, specs, records, fixture.driver); err != nil {
		t.Fatal(err)
	}
	if !faultsComplete(records) || records[0].RestoredBlock != 129 || records[0].RestoredBlockHash != completedHead.Hash || records[0].RestoredBlock-records[0].AppliedBlock < 20 {
		t.Fatalf("actual restoration lacks complete fault window: %+v", records[0])
	}
	fixture.requirePosts(t, "/control/miner-1/disable", "/control/miner-1/enable")
}

// Completion observation can retry independently of already-applied controls.
func TestMinerControlHeadTimeoutRetainsReconciliationWithoutDuplicateMutation(t *testing.T) {
	fixture := newMinerControlTestFixture(t, "miner-1")
	fixture.driver.faultCompletionContext = expiredFaultCompletionTestContext
	fixture.driver.faultCompletionHead = func(context.Context) (ChainHead, error) { return ChainHead{}, context.DeadlineExceeded }
	if _, err := fixture.driver.Apply(context.Background(), fixture.fault); !minerControlPending(err) {
		t.Fatalf("completion head timeout=%v", err)
	}
	if progress := fixture.progress(t); progress.Phase != "applying" || progress.CompletedCount != 1 {
		t.Fatalf("head timeout lost completed controls: %+v", progress)
	}
	fixture.driver.faultCompletionHead = func(context.Context) (ChainHead, error) {
		return ChainHead{Number: 123, Hash: "0x" + strings.Repeat("a", 64)}, nil
	}
	fixture.driver.faultCompletionContext = nil
	if _, err := fixture.driver.Apply(context.Background(), fixture.fault); err != nil {
		t.Fatal(err)
	}
	fixture.requirePosts(t, "/control/miner-1/disable")
	fixture.driver.faultCompletionContext = expiredFaultCompletionTestContext
	fixture.driver.faultCompletionHead = func(context.Context) (ChainHead, error) { return ChainHead{}, context.DeadlineExceeded }
	if _, err := fixture.driver.Restore(context.Background(), fixture.fault); !minerControlPending(err) {
		t.Fatalf("restore completion head timeout=%v", err)
	}
	if progress := fixture.progress(t); progress.Phase != "restoring" || progress.CompletedCount != 1 {
		t.Fatalf("restore head timeout lost recovery: %+v", progress)
	}
	fixture.driver.faultCompletionHead = func(context.Context) (ChainHead, error) {
		return ChainHead{Number: 124, Hash: "0x" + strings.Repeat("b", 64)}, nil
	}
	fixture.driver.faultCompletionContext = nil
	if _, err := fixture.driver.Restore(context.Background(), fixture.fault); err != nil {
		t.Fatal(err)
	}
	fixture.requirePosts(t, "/control/miner-1/disable", "/control/miner-1/enable")
	if _, err := os.Stat(fixture.driver.activePath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("completed restore retained recovery intent: %v", err)
	}
}

// Finalized completion must not substitute a different block at the same height.
func TestMinerControlCompletionRejectsSameHeightSubstitution(t *testing.T) {
	spec := scenarioFaultSpec{ID: "synthetic-cohort", Kind: "miner-control"}
	hash, err := canonicalHashHex(spec)
	if err != nil {
		t.Fatal(err)
	}
	driver := &liveScenarioFaultDriver{faultCompleted: faultCompletedTransition{faultId: spec.ID, faultHash: hash, action: "disable", head: ChainHead{Number: 123, Hash: "different"}}}
	if _, err := faultCompletedHead(driver, spec, "disable", ChainHead{Number: 123, Hash: "observed"}); err == nil {
		t.Fatal("accepted a substituted finalized block")
	}
}

// Only the exact pending wrapper is provisional; a joined disk or integrity
// failure must remain terminal even when it also includes a transport cause.
func TestMinerControlPendingDoesNotMaskIndependentHardFailure(t *testing.T) {
	pending := &minerControlPendingError{cause: context.DeadlineExceeded}
	if !minerControlPending(pending) || minerControlPending(errors.Join(pending, errors.New("synthetic invalid state"))) {
		t.Fatal("pending classification absorbed an independent failure")
	}
}

// The prior singleton schema remains resumable, including its ambiguous
// target and completed prefix, without discarding its mutation counter.
func TestMinerControlParallelMigratesLegacyPendingWithoutRepeatingMutation(t *testing.T) {
	fixture := newMinerControlTestFixture(t, "miner-1", "miner-2")
	fixture.apply(t)
	active, err := readActiveFaultFile(fixture.driver.activePath())
	if err != nil {
		t.Fatal(err)
	}
	active.Schema = "urnetwork-sim-active-faults-v2"
	progress := &active.MinerControls[0]
	progress.Phase = "applying"
	progress.Completed = progress.Completed[:1]
	progress.CompletedCount = 1
	progress.Pending = "miner-2"
	progress.PendingTargets = nil
	progress.LastError = "synthetic ambiguous legacy reply"
	if err := writePublicJSON(fixture.driver.activePath(), active); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.driver.Apply(context.Background(), fixture.fault); err != nil {
		t.Fatal(err)
	}
	active, err = readActiveFaultFile(fixture.driver.activePath())
	if err != nil {
		t.Fatal(err)
	}
	progress = &active.MinerControls[0]
	if active.Schema != minerControlProgressSchema || progress.Phase != "active" || progress.Attempts != 2 || progress.CompletedCount != 2 || progress.Pending != "" || len(progress.PendingTargets) != 0 {
		t.Fatalf("legacy migration lost progress: %+v", active)
	}
	fixture.requirePosts(t)
}

// An absent ledger authorizes a new intent, but an existing empty or damaged
// ledger may hide an unfinished mutation and must never be treated as absent.
func TestMinerControlEmptyRecoveryLedgerRejectsBeforeAnyRequest(t *testing.T) {
	fixture := newMinerControlTestFixture(t, "miner-1")
	if err := os.WriteFile(fixture.driver.activePath(), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	fixture.driver.minerControlClient.Transport = minerControlTestTransport(func(*http.Request) (*http.Response, error) {
		t.Error("corrupt local recovery evidence reached the network")
		return nil, errors.New("unexpected request")
	})
	if _, err := fixture.driver.Apply(context.Background(), fixture.fault); err == nil {
		t.Fatal("empty recovery ledger authorized a new mutation")
	}
	if err := fixture.driver.Recover(context.Background()); err == nil {
		t.Fatal("empty recovery ledger was discarded")
	}
	fixture.requirePosts(t)
}
