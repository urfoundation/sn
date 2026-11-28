//go:build urnetwork_control_composition_test

// Actual manager HTTP handlers, instance lifetimes and durable coordinator
// checkpoints compose here; no state-map transport grants completion.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/miner"
)

type minerManagerComposition struct {
	fixture   *minerControlTestFixture
	stateLock sync.Mutex
	managers  map[int]*miner.ProviderSwarmControlComposition
	urls      map[int]string
	requests  map[string]int
	lostReply func(http.ResponseWriter, *http.Request, *miner.ProviderSwarmControlComposition) bool
}

// Four actual managers each own 24 synthetic member lifetimes. The coordinator
// loads the same original process manifest/census as its normal public path.
func newMinerManagerComposition(t *testing.T) *minerManagerComposition {
	t.Helper()
	fixture := newMinerControlTestFixture(t)
	fixture.driver.cfg.Config.Topology.Miners = 96
	fixture.driver.cfg.Config.Topology.MinerSwarmProcesses = 4
	fixture.driver.minerControlParallel = 0
	self := &minerManagerComposition{fixture: fixture, managers: map[int]*miner.ProviderSwarmControlComposition{}, urls: map[int]string{}, requests: map[string]int{}}
	for swarm := 1; swarm <= 4; swarm++ {
		var ids []string
		for offset := 1; offset <= 24; offset++ {
			id := fmt.Sprintf("miner-%d", (swarm-1)*24+offset)
			ids = append(ids, id)
			fixture.fault.Targets = append(fixture.fault.Targets, id)
		}
		manager, err := miner.NewProviderSwarmControlComposition(t.Context(), ids)
		if err != nil {
			t.Fatal(err)
		}
		self.managers[swarm] = manager
		t.Cleanup(manager.Close)
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			self.stateLock.Lock()
			self.requests[request.Method+" "+request.URL.Path]++
			current, drop := self.managers[swarm], self.lostReply
			self.stateLock.Unlock()
			if drop != nil && drop(writer, request, current) {
				return
			}
			current.ServeHTTP(writer, request)
		}))
		t.Cleanup(server.Close)
		self.urls[swarm] = server.URL
	}
	installMinerControlTestSwarms(t, fixture)
	fixture.driver.minerControlURL = func(swarm int, target, action string) string {
		return self.urls[swarm] + "/control/" + target + "/" + action
	}
	fixture.driver.minerControlClient = &http.Client{}
	return self
}

func (self *minerManagerComposition) requestCounts() map[string]int {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	result := map[string]int{}
	for key, value := range self.requests {
		result[key] = value
	}
	return result
}

// The cancellation occurs only after the first complete parallel wave really
// reached the durable file. Subsequent waves have not acquired HTTP requests.
func (self *minerManagerComposition) interruptAfterFirstDurableWave(t *testing.T) []FaultProcessEvidence {
	t.Helper()
	round, cancel := context.WithCancel(t.Context())
	defer cancel()
	self.fixture.driver.minerControlRoundContext = func(context.Context) (context.Context, context.CancelFunc) { return round, cancel }
	interrupted := false
	self.fixture.driver.minerControlPersist = func(path string, raw []byte) error {
		if err := atomicWrite(path, raw, 0o600); err != nil {
			return err
		}
		var active activeFaultFile
		if err := json.Unmarshal(raw, &active); err != nil {
			return err
		}
		for _, progress := range active.MinerControls {
			if progress.Phase == "applying" && progress.CompletedCount == minerControlParallelLimit {
				interrupted = true
				cancel()
			}
		}
		return nil
	}
	_, err := self.fixture.driver.Apply(t.Context(), self.fixture.fault)
	progress := self.fixture.progress(t)
	if !interrupted || !minerControlPending(err) || progress.CompletedCount != 16 || len(progress.PendingTargets) != 0 || progress.Phase != "applying" {
		t.Fatalf("actual manager wave did not retain exact durable prefix: progress=%+v err=%v", progress, err)
	}
	self.fixture.driver.minerControlRoundContext = nil
	self.fixture.driver.minerControlPersist = nil
	return slices.Clone(progress.Completed)
}

func (self *minerManagerComposition) assertAllInstances(t *testing.T, starts, stops int) {
	t.Helper()
	for _, target := range self.fixture.fault.Targets {
		var number int
		if _, err := fmt.Sscanf(target, "miner-%d", &number); err != nil {
			t.Fatal(err)
		}
		swarm, err := minerSwarmFor(self.fixture.driver.cfg, number)
		if err != nil {
			t.Fatal(err)
		}
		self.stateLock.Lock()
		manager := self.managers[swarm]
		self.stateLock.Unlock()
		actualStarts, actualStops := manager.Counts(target)
		if actualStarts != starts || actualStops != stops {
			t.Fatalf("actual manager instance %s acquired=%d closed=%d want=%d/%d", target, actualStarts, actualStops, starts, stops)
		}
	}
}

func TestMinerControlActualManagersKeepNinetySixMemberCompletedPrefix(t *testing.T) {
	self := newMinerManagerComposition(t)
	prefix := self.interruptAfterFirstDurableWave(t)
	before := self.requestCounts()
	if _, err := self.fixture.driver.Apply(t.Context(), self.fixture.fault); err != nil {
		t.Fatal(err)
	}
	after := self.requestCounts()
	for _, process := range prefix {
		for _, operation := range []string{"GET /control/" + process.ID + "/status", "POST /control/" + process.ID + "/disable"} {
			if before[operation] != after[operation] {
				t.Fatalf("same owner reread or replayed completed actual manager prefix: %s %d/%d", operation, before[operation], after[operation])
			}
		}
	}
	progress := self.fixture.progress(t)
	if progress.Phase != "active" || progress.CompletedCount != 96 || progress.Attempts != 96 {
		t.Fatalf("actual manager census did not complete durably: %+v", progress)
	}
	self.assertAllInstances(t, 1, 1)
	if _, err := self.fixture.driver.Restore(t.Context(), self.fixture.fault); err != nil {
		t.Fatal(err)
	}
	self.assertAllInstances(t, 2, 1)
	for _, target := range self.fixture.fault.Targets {
		counts := self.requestCounts()
		if counts["POST /control/"+target+"/disable"] != 1 || counts["POST /control/"+target+"/enable"] != 1 {
			t.Fatalf("actual manager replayed original or opposite action for %s: %+v", target, counts)
		}
	}
}

func TestMinerControlActualManagersReopenPrefixWithoutRepeatingMutation(t *testing.T) {
	self := newMinerManagerComposition(t)
	prefix := self.interruptAfterFirstDurableWave(t)
	before := self.requestCounts()
	original := self.fixture.driver
	self.fixture.driver = &liveScenarioFaultDriver{stateDir: original.stateDir, cfg: original.cfg, minerControlURL: original.minerControlURL, minerControlClient: original.minerControlClient, minerControlWait: original.minerControlWait}
	if _, err := self.fixture.driver.Apply(t.Context(), self.fixture.fault); err != nil {
		t.Fatal(err)
	}
	after := self.requestCounts()
	for _, process := range prefix {
		get, post := "GET /control/"+process.ID+"/status", "POST /control/"+process.ID+"/disable"
		if after[get] != before[get]+1 || after[post] != before[post] {
			t.Fatalf("reopened owner did not reconcile actual manager without replay: %s get=%d/%d post=%d/%d", process.ID, before[get], after[get], before[post], after[post])
		}
	}
	self.assertAllInstances(t, 1, 1)
	if progress := self.fixture.progress(t); progress.Phase != "active" || progress.CompletedCount != 96 || progress.Attempts != 96 {
		t.Fatalf("reopened actual managers lost prefix/accounted actions: %+v", progress)
	}
}

func TestMinerControlActualManagerReplacementInvalidatesOnlyItsPrefix(t *testing.T) {
	self := newMinerManagerComposition(t)
	prefix := self.interruptAfterFirstDurableWave(t)
	before := self.requestCounts()
	var ids []string
	for number := 1; number <= 24; number++ {
		ids = append(ids, fmt.Sprintf("miner-%d", number))
	}
	replacement, err := miner.NewProviderSwarmControlComposition(t.Context(), ids)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(replacement.Close)
	self.stateLock.Lock()
	original := self.managers[1]
	self.managers[1] = replacement
	self.stateLock.Unlock()
	original.Close()
	path := filepath.Join(self.fixture.driver.stateDir, "supervisor.state.json")
	var state SupervisorState
	if err := readJSONFile(path, &state); err != nil {
		t.Fatal(err)
	}
	state.Processes[0].StartedAt = "2000-01-01T00:00:01Z"
	state.Processes[0].Restarts++
	if err := writePublicJSON(path, state); err != nil {
		t.Fatal(err)
	}
	if _, err := self.fixture.driver.Apply(t.Context(), self.fixture.fault); err != nil {
		t.Fatal(err)
	}
	after := self.requestCounts()
	changed := 0
	for _, process := range prefix {
		var number int
		if _, err := fmt.Sscanf(process.ID, "miner-%d", &number); err != nil {
			t.Fatal(err)
		}
		delta := 0
		if number <= 24 {
			delta = 1
			changed++
		}
		for _, operation := range []string{"GET /control/" + process.ID + "/status", "POST /control/" + process.ID + "/disable"} {
			if after[operation] != before[operation]+delta {
				t.Fatalf("manager replacement skipped new generation or repeated sibling: %s %d/%d", operation, before[operation], after[operation])
			}
		}
	}
	if changed != 4 {
		t.Fatalf("fixture did not cross all four managers: prefix=%+v", prefix)
	}
	self.assertAllInstances(t, 1, 1)
}

func TestMinerControlActualHttpWaitsForDurableDispatch(t *testing.T) {
	self := newMinerManagerComposition(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	var checkpointLock sync.Mutex
	checkpointHeld := false
	violations := 0
	transport := http.DefaultTransport.(*http.Transport).Clone()
	defer transport.CloseIdleConnections()
	self.fixture.driver.minerControlClient = &http.Client{Transport: minerControlTestTransport(func(request *http.Request) (*http.Response, error) {
		checkpointLock.Lock()
		if checkpointHeld {
			violations++
		}
		checkpointLock.Unlock()
		return transport.RoundTrip(request)
	})}
	blocked := false
	self.fixture.driver.minerControlPersist = func(path string, raw []byte) error {
		var active activeFaultFile
		if err := json.Unmarshal(raw, &active); err != nil {
			return err
		}
		if !blocked && len(active.MinerControls) == 1 && len(active.MinerControls[0].PendingTargets) == 16 {
			blocked = true
			checkpointLock.Lock()
			checkpointHeld = true
			checkpointLock.Unlock()
			close(entered)
			<-release
			if len(self.requestCounts()) != 0 {
				return errors.New("HTTP preceded original durable wave")
			}
			if err := atomicWrite(path, raw, 0o600); err != nil {
				return err
			}
			checkpointLock.Lock()
			checkpointHeld = false
			checkpointLock.Unlock()
			return nil
		}
		return atomicWrite(path, raw, 0o600)
	}
	done := make(chan error, 1)
	joined := false
	defer func() {
		cancel()
		once.Do(func() { close(release) })
		if !joined {
			select {
			case <-done:
			case <-time.After(time.Minute):
				t.Error("canceled durable dispatch did not join")
			}
		}
	}()
	go func() { _, err := self.fixture.driver.Apply(ctx, self.fixture.fault); done <- err }()
	select {
	case <-entered:
	case err := <-done:
		joined = true
		t.Fatalf("dispatch ended before durable barrier: %v", err)
	case <-time.After(time.Minute):
		t.Fatal("durable dispatch barrier was not reached")
	}
	if requests := self.requestCounts(); !reflect.DeepEqual(requests, map[string]int{}) {
		t.Fatalf("actual manager received requests before fsync: %+v", requests)
	}
	once.Do(func() { close(release) })
	select {
	case err := <-done:
		joined = true
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Minute):
		t.Fatal("actual dispatched manager requests did not join")
	}
	checkpointLock.Lock()
	count := violations
	checkpointLock.Unlock()
	if count != 0 {
		t.Fatalf("actual HTTP request overlapped preceding durable checkpoint: %d", count)
	}
	self.assertAllInstances(t, 1, 1)
	for request, count := range self.requestCounts() {
		if strings.HasPrefix(request, "POST ") && count != 1 {
			t.Fatalf("durable dispatch repeated %s: %d", request, count)
		}
	}
}

// The real manager completes teardown but the owned socket closes before any
// response. A reopened coordinator must read that result without reposting.
func TestMinerControlActualManagerLostReplySurvivesCoordinatorRestart(t *testing.T) {
	self := newMinerManagerComposition(t)
	self.fixture.fault.Targets = []string{"miner-1", "miner-25"}
	self.fixture.driver.minerControlParallel = 1
	round, cancel := context.WithCancel(t.Context())
	defer cancel()
	self.fixture.driver.minerControlRoundContext = func(context.Context) (context.Context, context.CancelFunc) { return round, cancel }
	lost := make(chan struct{})
	self.stateLock.Lock()
	self.lostReply = func(writer http.ResponseWriter, request *http.Request, manager *miner.ProviderSwarmControlComposition) bool {
		if request.Method != http.MethodPost || request.URL.Path != "/control/miner-1/disable" {
			return false
		}
		defer close(lost)
		defer cancel()
		recorder := httptest.NewRecorder()
		manager.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Errorf("real manager did not complete before lost reply: %d %s", recorder.Code, recorder.Body.String())
		}
		connection, _, err := writer.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return true
		}
		cancel()
		if err := connection.Close(); err != nil {
			t.Error(err)
		}
		return true
	}
	self.stateLock.Unlock()
	_, err := self.fixture.driver.Apply(t.Context(), self.fixture.fault)
	select {
	case <-lost:
	case <-time.After(time.Minute):
		t.Fatal("actual manager lost-reply boundary was not reached")
	}
	progress := self.fixture.progress(t)
	if !minerControlPending(err) || !errors.Is(err, context.Canceled) || progress.Attempts != 1 || progress.CompletedCount != 0 || !slices.Equal(progress.PendingTargets, []string{"miner-1"}) {
		t.Fatalf("lost actual reply dropped original pending action: %+v %v", progress, err)
	}
	before := self.requestCounts()
	self.stateLock.Lock()
	self.lostReply = nil
	self.stateLock.Unlock()
	original := self.fixture.driver
	self.fixture.driver = &liveScenarioFaultDriver{stateDir: original.stateDir, cfg: original.cfg, minerControlURL: original.minerControlURL, minerControlClient: original.minerControlClient, minerControlWait: original.minerControlWait, minerControlParallel: 1}
	if _, err := self.fixture.driver.Apply(t.Context(), self.fixture.fault); err != nil {
		t.Fatal(err)
	}
	after := self.requestCounts()
	next := self.fixture.progress(t)
	if after["POST /control/miner-1/disable"] != before["POST /control/miner-1/disable"] || next.Attempts != 2 || next.CompletedCount != 2 || next.Phase != "active" || next.FaultHash != progress.FaultHash || len(next.PendingTargets) != 0 {
		t.Fatalf("restarted coordinator repeated ambiguous manager action or changed intent: before=%+v after=%+v progress=%+v", before, after, next)
	}
	for _, item := range []struct {
		swarm int
		id    string
		stops int
	}{{swarm: 1, id: "miner-1", stops: 1}, {swarm: 2, id: "miner-25", stops: 1}, {swarm: 1, id: "miner-2", stops: 0}, {swarm: 2, id: "miner-26", stops: 0}} {
		starts, stops := self.managers[item.swarm].Counts(item.id)
		if starts != 1 || stops != item.stops {
			t.Fatalf("manager lost-reply changed selected/sibling lifecycle %s: %d/%d", item.id, starts, stops)
		}
	}
}
