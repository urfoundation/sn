// Completion anchors follow forced off-chain transitions, never the older
// scheduler cut. Synthetic expiry exercises pending adoption without sleeps.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"syscall"
	"testing"
	"time"
)

// The operation already completed when this independent read owner expires.
func expiredFaultCompletionTestContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithDeadline(ctx, time.Unix(1, 0))
}

// Synthetic block hashes remain explicit and distinct at every height.
func faultCompletionTestHead(number uint64) ChainHead {
	return ChainHead{Number: number, Hash: fmt.Sprintf("0x%064x", number)}
}

// Immediate health absence performs no completion read. A later replacement
// must anchor after actual readback even when the caller's snapshot is old.
func TestFaultCompletionProcessRestoreUsesPostReadinessHead(t *testing.T) {
	fixture := newProcessRestartRetryFixture(t)
	specs := []scenarioFaultSpec{fixture.spec}
	records, err := initializeFaultRecords(100, specs)
	if err != nil {
		t.Fatal(err)
	}
	active, err := readActiveFaultFile(fixture.driver.activePath())
	if err != nil {
		t.Fatal(err)
	}
	records[0].Status, records[0].AppliedBlock, records[0].AppliedBlockHash, records[0].Processes = "active", 102, faultCompletionTestHead(102).Hash, active.Processes
	reads := 0
	fixture.driver.faultCompletionHead = func(context.Context) (ChainHead, error) {
		reads++
		if !fixture.state.Processes[0].Healthy {
			t.Fatal("completion head was read before replacement health")
		}
		return faultCompletionTestHead(126), nil
	}
	if err := advanceFaults(t.Context(), faultCompletionTestHead(105), specs, records, fixture.driver); err != nil || reads != 0 {
		t.Fatalf("pending readiness acquired a completion: reads=%d error=%v", reads, err)
	}
	fixture.state.Processes[0].PID, fixture.state.Processes[0].Healthy = os.Getpid(), true
	if err := writePublicJSON(filepath.Join(fixture.driver.stateDir, "supervisor.state.json"), fixture.state); err != nil {
		t.Fatal(err)
	}
	if err := advanceFaults(t.Context(), faultCompletionTestHead(105), specs, records, fixture.driver); err != nil {
		t.Fatal(err)
	}
	if reads != 1 || records[0].Status != "restored" || records[0].RestoredBlock != 126 || records[0].RestoredBlockHash != faultCompletionTestHead(126).Hash {
		t.Fatalf("restoration was backdated to the pre-readiness snapshot: %+v reads=%d", records[0], reads)
	}
}

// A channel forces readiness to straddle the old scheduler head, reproducing
// the observed restart chronology without timing assumptions or real Docker.
type faultCompletionBlockedContainer struct {
	fakeContainerRuntime
	entered chan struct{}
	release chan struct{}
}

func (self *faultCompletionBlockedContainer) Start(ctx context.Context, spec managedContainerSpec) (int, error) {
	close(self.entered)
	select {
	case <-self.release:
		return self.fakeContainerRuntime.Start(ctx, spec)
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

func TestFaultCompletionContainerReadFollowsBlockedReadiness(t *testing.T) {
	runtime := &faultCompletionBlockedContainer{entered: make(chan struct{}), release: make(chan struct{})}
	driver := &liveScenarioFaultDriver{stateDir: t.TempDir(), cfg: testResolvedConfig(t), containers: runtime}
	spec := scenarioFaultSpec{ID: "synthetic-container-completion", Kind: "container-restart", Targets: []string{"operator-1-postgres"}, TriggerOffsetBlocks: 2, DurationBlocks: 3}
	specs := []scenarioFaultSpec{spec}
	records, err := initializeFaultRecords(100, specs)
	if err != nil {
		t.Fatal(err)
	}
	currentHead := faultCompletionTestHead(107)
	driver.faultCompletionHead = func(context.Context) (ChainHead, error) { return currentHead, nil }
	if err := advanceFaults(t.Context(), faultCompletionTestHead(102), specs, records, driver); err != nil || records[0].AppliedBlock != 107 {
		t.Fatalf("container apply retained its pre-stop cut: %+v error=%v", records[0], err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- advanceFaults(ctx, faultCompletionTestHead(110), specs, records, driver) }()
	select {
	case <-runtime.entered:
	case err := <-done:
		t.Fatalf("restore did not reach readiness barrier: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	currentHead = faultCompletionTestHead(135)
	close(runtime.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if records[0].RestoredBlock != 135 || records[0].RestoredBlockHash != currentHead.Hash || len(runtime.stopped) != 1 || len(runtime.started) != 1 {
		t.Fatalf("delayed restoration inherited a pre-completion block: %+v", records[0])
	}
}

// A new driver adopts the persisted SIGTERM intent after read-budget expiry.
// Pending signed metadata is explicit; no previous signal is dispatched twice.
func TestFaultCompletionApplyExpiryAdoptsWithoutRepeatingSignal(t *testing.T) {
	fixture := newProcessRestartIntentFixture(t, 1)
	fixture.fault.TriggerOffsetBlocks = 2
	specs := []scenarioFaultSpec{fixture.fault}
	records, err := initializeFaultRecords(100, specs)
	if err != nil {
		t.Fatal(err)
	}
	signals := 0
	fixture.driver.restartSignal = func(supervisedCommand, syscall.Signal) bool { signals++; return true }
	fixture.driver.faultCompletionHead = func(context.Context) (ChainHead, error) {
		t.Fatal("expired read owner issued RPC")
		return ChainHead{}, nil
	}
	fixture.driver.faultCompletionContext = expiredFaultCompletionTestContext
	if err := advanceFaults(t.Context(), faultCompletionTestHead(102), specs, records, fixture.driver); err != nil {
		t.Fatal(err)
	}
	if records[0].Status != "pending" || records[0].AppliedBlock != 0 || records[0].ApplyPendingRounds != 1 || signals != 1 {
		t.Fatalf("completion expiry invalidated or repeated the signal: %+v signals=%d", records[0], signals)
	}
	if err := validateScenarioCampaignFaultState(&ScenarioAcceptanceWindow{StartBlock: 100}, records[0]); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(scenarioFaultTargets(records, 103, false), fixture.fault.Targets) || len(activeProcessLogFaultScopes(records)) != 1 {
		t.Fatal("durable mutation lost its expected fault attribution while completion was pending")
	}
	observation := &ScenarioObservation{Schema: "urnetwork-sim-scenario-observation-v1"}
	if err := annotateScenarioExpectedFaults(observation, records); err != nil || !slices.Equal(observation.ExpectedFaultIDs, []string{fixture.fault.ID}) {
		t.Fatalf("pending completion lost observation fault identity: %+v error=%v", observation, err)
	}
	fixture.retained(t)
	before := cloneScenarioFaultRecords(records)
	resumed := &liveScenarioFaultDriver{
		stateDir: fixture.driver.stateDir,
		restartSignal: func(supervisedCommand, syscall.Signal) bool {
			t.Fatal("adopted intent repeated termination")
			return false
		},
		faultCompletionHead: func(context.Context) (ChainHead, error) { return faultCompletionTestHead(108), nil },
	}
	if err := advanceFaults(t.Context(), faultCompletionTestHead(103), specs, records, resumed); err != nil {
		t.Fatal(err)
	}
	if records[0].Status != "active" || records[0].AppliedBlock != 108 || records[0].ApplyPendingRounds != 1 || records[0].Error != "" || signals != 1 {
		t.Fatalf("adoption lost original progress: %+v signals=%d", records[0], signals)
	}
	if err := validateScenarioCampaignFaultState(&ScenarioAcceptanceWindow{StartBlock: 100}, records[0]); err != nil {
		t.Fatal(err)
	}
	if err := validateScenarioFaultProgress(before, records); err != nil {
		t.Fatal(err)
	}
}

// Missing completion RPC cannot erase a healthy replacement's retained fault.
// Cleanup can retry the read without redispatching any termination signal.
func TestFaultCompletionRestoreExpiryKeepsDurableRecovery(t *testing.T) {
	fixture := newProcessRestartRetryFixture(t)
	fixture.state.Processes[0].PID, fixture.state.Processes[0].Healthy = os.Getpid(), true
	if err := writePublicJSON(filepath.Join(fixture.driver.stateDir, "supervisor.state.json"), fixture.state); err != nil {
		t.Fatal(err)
	}
	fixture.driver.faultCompletionHead = func(context.Context) (ChainHead, error) { return faultCompletionTestHead(140), nil }
	fixture.driver.faultCompletionContext = expiredFaultCompletionTestContext
	if _, err := fixture.driver.Restore(t.Context(), fixture.spec); !faultCompletionPending(fixture.spec, "enable", err) {
		t.Fatalf("completion expiry became permanent: %v", err)
	}
	fixture.requireRetained(t)
	fixture.driver.faultCompletionContext = nil
	if _, err := waitScenarioFaultRestore(t.Context(), fixture.spec, func() ([]FaultProcessEvidence, error) { return fixture.driver.Restore(t.Context(), fixture.spec) }, func() error { t.Fatal("successful retry waited again"); return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(fixture.driver.activePath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("completed post-readiness checkpoint retained intent: %v", err)
	}
}

// Filter removal already owns a durable exact receipt. A head timeout keeps
// that receipt and the fault, so adoption does not recreate the removed rule.
func TestFaultCompletionViewRestoreExpiryRetainsRemoval(t *testing.T) {
	_, driver, spec, _ := validatorViewFaultDriverFixture(t)
	if _, err := driver.Apply(t.Context(), spec); err != nil {
		t.Fatal(err)
	}
	driver.faultCompletionHead = func(context.Context) (ChainHead, error) { return faultCompletionTestHead(180), nil }
	driver.faultCompletionContext = expiredFaultCompletionTestContext
	if _, err := driver.Restore(t.Context(), spec); !faultCompletionPending(spec, "enable", err) {
		t.Fatalf("view completion failure lost pending scope: %v", err)
	}
	if _, err := os.Stat(driver.activePath()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(verifyAssignmentFilterPath(driver.stateDir, 2)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("completed filter removal was rolled back: %v", err)
	}
	driver.faultCompletionContext = nil
	if _, err := driver.Restore(t.Context(), spec); err != nil {
		t.Fatal(err)
	}
	if head, err := faultCompletedHead(driver, spec, "enable", faultCompletionTestHead(140)); err != nil || head.Number != 180 {
		t.Fatalf("view completion acquired old head: %+v error=%v", head, err)
	}
}

// Fast RPC failures must not consume the whole read policy in a few attempts.
// The fixed owner deadline survives repeated bounded retry batches.
func TestFaultCompletionRetriesFastTransientBeyondOneBatch(t *testing.T) {
	spec := scenarioFaultSpec{ID: "synthetic-retry-head", Kind: "process-restart", Targets: []string{"synthetic-worker"}}
	reads, waits := 0, 0
	policy := defaultFinalSemanticRPCRetryPolicy()
	policy.wait = func(context.Context, time.Duration) error { waits++; return nil }
	driver := &liveScenarioFaultDriver{faultCompletionRetryPolicy: &policy}
	var ownerDeadline time.Time
	driver.faultCompletionContext = func(ctx context.Context) (context.Context, context.CancelFunc) {
		readCtx, cancel := context.WithTimeout(ctx, faultCompletionTimeout)
		ownerDeadline, _ = readCtx.Deadline()
		return readCtx, cancel
	}
	driver.faultCompletionHead = func(ctx context.Context) (ChainHead, error) {
		reads++
		deadline, ok := ctx.Deadline()
		if !ok || deadline.After(ownerDeadline) {
			t.Fatal("nested read multiplied its completion owner budget")
		}
		if reads <= 7 {
			return ChainHead{}, context.DeadlineExceeded
		}
		return faultCompletionTestHead(210), nil
	}
	if err := driver.captureFaultCompletion(t.Context(), spec, "enable"); err != nil || reads != 8 || waits != 7 {
		t.Fatalf("fast transient recovery stopped early: reads=%d waits=%d error=%v", reads, waits, err)
	}
	if faultCompletionTimeout < time.Minute {
		t.Fatal("completion retry budget lost minimum expected-read allowance")
	}
}

// A mixed permanent failure cannot borrow retry permission or be hidden by a
// prior success. Pending ownership includes the full spec and exact action.
func TestFaultCompletionRejectsMixedFailureAndForeignPending(t *testing.T) {
	spec := scenarioFaultSpec{ID: "synthetic-completion-scope", Kind: "container-restart", Targets: []string{"synthetic-container"}}
	hard := errors.New("synthetic invalid finalized state")
	reads := 0
	driver := &liveScenarioFaultDriver{faultCompleted: faultCompletedTransition{head: faultCompletionTestHead(250)}, faultCompletionHead: func(context.Context) (ChainHead, error) {
		reads++
		return ChainHead{}, errors.Join(context.DeadlineExceeded, hard)
	}}
	if err := driver.captureFaultCompletion(t.Context(), spec, "enable"); !errors.Is(err, hard) || faultCompletionPending(spec, "enable", err) || reads != 1 || driver.faultCompleted.head.Number != 0 {
		t.Fatalf("mixed error acquired completion or retry: reads=%d error=%v", reads, err)
	}
	driver.faultCompletionContext = expiredFaultCompletionTestContext
	err := driver.captureFaultCompletion(t.Context(), spec, "enable")
	changed := spec
	changed.Targets = []string{"another-synthetic-container"}
	if !faultCompletionPending(spec, "enable", err) || faultCompletionPending(changed, "enable", err) || faultCompletionPending(spec, "disable", err) || faultCompletionPending(spec, "enable", errors.Join(err, hard)) {
		t.Fatal("completion pending scope widened")
	}
	if _, err := faultCompletedHead(driver, spec, "enable", faultCompletionTestHead(200)); err == nil {
		t.Fatal("missing completion proof silently inherited a scheduler head")
	}
}

// A condition was authenticated before its mutation. Read-only retries must
// retain that first proof even if a later heartbeat has no full observation.
func TestFaultCompletionConditionalViewRetainsBothConditionCuts(t *testing.T) {
	_, driver, spec, _ := validatorViewFaultDriverFixture(t)
	spec.TriggerOffsetBlocks, spec.DurationBlocks, spec.MinimumDurationBlocks = 2, 100, 3
	spec.ActivationCondition = "synthetic-activation-cut"
	specs := []scenarioFaultSpec{spec}
	records, err := initializeFaultRecords(100, specs)
	if err != nil {
		t.Fatal(err)
	}
	driver.faultCompletionHead = func(context.Context) (ChainHead, error) { return faultCompletionTestHead(108), nil }
	driver.faultCompletionContext = expiredFaultCompletionTestContext
	conditionReads := 0
	condition := func(scenarioFaultSpec) (bool, error) { conditionReads++; return true, nil }
	if err := advanceFaultsWithConditions(t.Context(), faultCompletionTestHead(102), specs, records, driver, condition, nil); err != nil || records[0].Status != "pending" {
		t.Fatalf("conditional apply did not retain pending completion: %+v error=%v", records[0], err)
	}
	before := cloneScenarioFaultRecords(records)
	driver.faultCompletionContext = nil
	if err := advanceFaultsWithConditions(t.Context(), faultCompletionTestHead(103), specs, records, driver, nil, nil); err != nil || records[0].Status != "active" || records[0].ActivationConditionBlock != 102 {
		t.Fatalf("heartbeat lost the authenticated activation cut: %+v error=%v", records[0], err)
	}
	if err := validateScenarioFaultProgress(before, records); err != nil {
		t.Fatal(err)
	}
	driver.faultCompletionContext = expiredFaultCompletionTestContext
	if err := advanceFaultsWithConditions(t.Context(), faultCompletionTestHead(111), specs, records, driver, nil, condition); err != nil || records[0].Status != "active" || records[0].RestorePendingRounds != 1 {
		t.Fatalf("conditional restore did not retain pending completion: %+v error=%v", records[0], err)
	}
	before = cloneScenarioFaultRecords(records)
	driver.faultCompletionContext = nil
	driver.faultCompletionHead = func(context.Context) (ChainHead, error) { return faultCompletionTestHead(117), nil }
	if err := advanceFaultsWithConditions(t.Context(), faultCompletionTestHead(112), specs, records, driver, nil, nil); err != nil || records[0].Status != "restored" || records[0].RestoredBlock != 117 || records[0].RestoreConditionBlock != 111 || conditionReads != 2 {
		t.Fatalf("heartbeat lost the authenticated restoration cut: %+v reads=%d error=%v", records[0], conditionReads, err)
	}
	if err := validateScenarioFaultProgress(before, records); err != nil {
		t.Fatal(err)
	}
	if err := validateScenarioCampaignFaultState(&ScenarioAcceptanceWindow{StartBlock: 100}, records[0]); err != nil {
		t.Fatal(err)
	}
}

// An active/prearmed miner intent is adopted without a new control request,
// but its acceptance-time completion anchor still requires a current read.
func TestFaultCompletionAdoptedMinerUsesFreshHeadWithoutControlReplay(t *testing.T) {
	fixture := newMinerControlTestFixture(t, "miner-1")
	reads := 0
	fixture.driver.faultCompletionHead = func(context.Context) (ChainHead, error) {
		reads++
		return faultCompletionTestHead(uint64(100 + reads)), nil
	}
	for round := 0; round < 2; round++ {
		if _, err := fixture.driver.Apply(t.Context(), fixture.fault); err != nil {
			t.Fatal(err)
		}
	}
	fixture.requirePosts(t, "/control/miner-1/disable")
	head, err := faultCompletedHead(fixture.driver, fixture.fault, "disable", faultCompletionTestHead(100))
	if err != nil || reads != 2 || head.Number != 102 {
		t.Fatalf("adopted miner reused an earlier completion: head=%+v reads=%d error=%v", head, reads, err)
	}
}

// A snapshot cut in the context is intentionally immutable. The independent
// scheduler/completion probe must explicitly request a newer finalized head.
func TestFaultCompletionProbeBypassesSnapshotHead(t *testing.T) {
	want := faultCompletionTestHead(202)
	requests := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var call struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params []any           `json:"params"`
		}
		if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
			t.Error(err)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		requests <- call.Method
		if call.Method != "eth_getBlockByNumber" || len(call.Params) != 2 || call.Params[0] != "finalized" || call.Params[1] != false {
			t.Errorf("completion probe changed its finalized selector: %+v", call)
		}
		writer.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": call.ID, "result": map[string]any{"number": fmt.Sprintf("0x%x", want.Number), "hash": want.Hash}}); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	ctx, err := withFinalizedEVMHead(t.Context(), faultCompletionTestHead(101))
	if err != nil {
		t.Fatal(err)
	}
	probe := &liveScenarioProbe{cfg: &ResolvedConfig{OperationalEVM: server.URL}}
	head, err := probe.FinalizedHead(ctx)
	if err != nil || head != want {
		t.Fatalf("post-transition read inherited the snapshot cut: %+v error=%v", head, err)
	}
	select {
	case <-requests:
	default:
		t.Fatal("completion probe did not read current finalized state")
	}
}

// A predecessor can delay application beyond the nominal restore block.
// The immutable schedule remains visible while actual dwell is enforced.
func TestFaultCompletionDelayedRestartKeepsActualDwell(t *testing.T) {
	spec := scenarioFaultSpec{ID: "synthetic-delayed-restart", Kind: "process-restart", Targets: []string{"synthetic-worker"}, TriggerOffsetBlocks: 5, DurationBlocks: 20}
	specs := []scenarioFaultSpec{spec}
	records, err := initializeFaultRecords(100, specs)
	if err != nil {
		t.Fatal(err)
	}
	driver := &fakeFaultDriver{}
	for _, head := range []uint64{150, 169} {
		if err := advanceFaults(t.Context(), faultCompletionTestHead(head), specs, records, driver); err != nil {
			t.Fatal(err)
		}
		if records[0].Status != "active" || len(driver.restored) != 0 {
			t.Fatalf("nominal restore deadline erased actual dwell at %d: %+v", head, records[0])
		}
	}
	if err := advanceFaults(t.Context(), faultCompletionTestHead(170), specs, records, driver); err != nil {
		t.Fatal(err)
	}
	if records[0].Status != "restored" || records[0].TriggerBlock != 105 || records[0].RestoreBlock != 125 || records[0].AppliedBlock != 150 || records[0].RestoredBlock != 170 || len(driver.applied) != 1 || len(driver.restored) != 1 {
		t.Fatalf("delayed restart changed nominal schedule or actual duration: %+v", records[0])
	}
}

// A retained application owns its original target census before the completion
// read succeeds. New retry fields must not admit a foreign or rewritten effect.
func TestFaultCompletionApplyHistoryRejectsMalformedAndRewrittenOwnership(t *testing.T) {
	record := ScenarioFaultRecord{
		ID: "synthetic-pending-restart", Kind: "process-restart", Targets: []string{"synthetic-first", "synthetic-second"},
		TriggerBlock: 10, RestoreBlock: 30, Status: "pending", Error: "synthetic completion read is pending",
		ApplyStartedBlock: 12, ApplyStartedBlockHash: faultCompletionTestHead(12).Hash, ApplyPendingRounds: 2,
		Processes: []FaultProcessEvidence{
			{ID: "synthetic-first", Role: "worker", Identity: "synthetic-instance-first", PID: 100, StartTimeTicks: 1000},
			{ID: "synthetic-second", Role: "worker", Identity: "synthetic-instance-second", PID: 101, StartTimeTicks: 1001},
		},
	}
	window := &ScenarioAcceptanceWindow{StartBlock: 10}
	for _, kind := range []string{"process-restart", "process-pause", "container-restart", "validator-view-filter"} {
		valid := record
		valid.Kind = kind
		if err := validateScenarioCampaignFaultState(window, valid); err != nil {
			t.Fatalf("supported pending application %s was rejected: %v", kind, err)
		}
	}
	for _, mutation := range []struct {
		name   string
		change func(*ScenarioFaultRecord)
	}{
		{name: "foreign kind", change: func(r *ScenarioFaultRecord) { r.Kind = "unknown-control" }},
		{name: "wrong miner namespace", change: func(r *ScenarioFaultRecord) { r.Kind = "miner-control" }},
		{name: "missing round", change: func(r *ScenarioFaultRecord) { r.ApplyPendingRounds = 0 }},
		{name: "early cut", change: func(r *ScenarioFaultRecord) { r.ApplyStartedBlock = 9 }},
		{name: "missing hash", change: func(r *ScenarioFaultRecord) { r.ApplyStartedBlockHash = "" }},
		{name: "missing cause", change: func(r *ScenarioFaultRecord) { r.Error = "" }},
		{name: "empty census", change: func(r *ScenarioFaultRecord) { r.Targets, r.Processes = nil, nil }},
		{name: "empty target", change: func(r *ScenarioFaultRecord) { r.Targets[0], r.Processes[0].ID = "", "" }},
		{name: "duplicate target", change: func(r *ScenarioFaultRecord) { r.Targets[1] = r.Targets[0] }},
		{name: "missing process", change: func(r *ScenarioFaultRecord) { r.Processes = r.Processes[:1] }},
		{name: "duplicate process", change: func(r *ScenarioFaultRecord) { r.Processes[1] = r.Processes[0] }},
		{name: "foreign process", change: func(r *ScenarioFaultRecord) { r.Processes[0].ID = "foreign-synthetic-worker" }},
		{name: "missing role", change: func(r *ScenarioFaultRecord) { r.Processes[0].Role = "" }},
		{name: "missing identity", change: func(r *ScenarioFaultRecord) { r.Processes[0].Identity = "" }},
		{name: "non-process pid", change: func(r *ScenarioFaultRecord) { r.Processes[0].PID = 1 }},
		{name: "completion before attempt", change: func(r *ScenarioFaultRecord) {
			r.Status, r.Error, r.AppliedBlock, r.AppliedBlockHash = "active", "", 11, faultCompletionTestHead(11).Hash
		}},
	} {
		invalid := cloneScenarioFaultRecords([]ScenarioFaultRecord{record})[0]
		mutation.change(&invalid)
		if err := validateScenarioCampaignFaultState(window, invalid); err == nil {
			t.Errorf("accepted malformed pending application: %s", mutation.name)
		}
	}
	for _, mutation := range []struct {
		name   string
		change func(*ScenarioFaultRecord)
	}{
		{name: "counter rollback", change: func(r *ScenarioFaultRecord) { r.ApplyPendingRounds-- }},
		{name: "cut substitution", change: func(r *ScenarioFaultRecord) { r.ApplyStartedBlock++ }},
		{name: "hash substitution", change: func(r *ScenarioFaultRecord) { r.ApplyStartedBlockHash = faultCompletionTestHead(13).Hash }},
		{name: "process substitution", change: func(r *ScenarioFaultRecord) { r.Processes[0].PID++ }},
		{name: "generation substitution", change: func(r *ScenarioFaultRecord) { r.Processes[0].StartTimeTicks++ }},
		{name: "role substitution", change: func(r *ScenarioFaultRecord) { r.Processes[0].Role = "another-synthetic-role" }},
		{name: "identity substitution", change: func(r *ScenarioFaultRecord) { r.Processes[0].Identity = "another-synthetic-instance" }},
	} {
		after := cloneScenarioFaultRecords([]ScenarioFaultRecord{record})[0]
		mutation.change(&after)
		if err := validateScenarioFaultProgress([]ScenarioFaultRecord{record}, []ScenarioFaultRecord{after}); err == nil {
			t.Errorf("accepted pending application ownership rewrite: %s", mutation.name)
		}
	}
	after := cloneScenarioFaultRecords([]ScenarioFaultRecord{record})[0]
	after.ApplyPendingRounds++
	if err := validateScenarioFaultProgress([]ScenarioFaultRecord{record}, []ScenarioFaultRecord{after}); err != nil {
		t.Fatalf("exact pending retry could not advance: %v", err)
	}
	after.Status, after.Error, after.AppliedBlock, after.AppliedBlockHash = "active", "", 14, faultCompletionTestHead(14).Hash
	if err := validateScenarioFaultProgress([]ScenarioFaultRecord{record}, []ScenarioFaultRecord{after}); err != nil {
		t.Fatalf("exact pending application could not complete: %v", err)
	}
	completed := after
	after.ApplyPendingRounds++
	if err := validateScenarioFaultProgress([]ScenarioFaultRecord{completed}, []ScenarioFaultRecord{after}); err == nil {
		t.Fatal("completed application acquired another pending attempt")
	}
}

// The new optional checkpoint is additive: old signed record bytes retain
// their canonical representation, and unrecognized fields remain rejected.
func TestFaultCompletionApplyHistoryPreservesLegacyWireBytes(t *testing.T) {
	legacy := `{"id":"synthetic-restart","kind":"process-restart","targets":["synthetic-worker"],"trigger_block":10,"restore_block":20,"status":"pending"}`
	withPending := legacy[:len(legacy)-1] + fmt.Sprintf(`,"apply_started_block":12,"apply_started_block_hash":%q,"apply_pending_rounds":2}`, faultCompletionTestHead(12).Hash)
	for _, raw := range []string{legacy, withPending} {
		var record ScenarioFaultRecord
		if err := decodeStrictJSONBytes([]byte(raw), &record); err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(record)
		if err != nil || string(encoded) != raw {
			t.Fatalf("optional completion checkpoint changed signed wire bytes: error=%v got=%s want=%s", err, encoded, raw)
		}
	}
	unknown := withPending[:len(withPending)-1] + `,"apply_unreviewed_authority":true}`
	var record ScenarioFaultRecord
	if err := decodeStrictJSONBytes([]byte(unknown), &record); err == nil {
		t.Fatal("foreign application authority was silently discarded")
	}
}
