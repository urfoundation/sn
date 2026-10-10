package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

type monitorEconomicTestEvent struct {
	Schema            string                       `json:"schema"`
	Role              string                       `json:"role"`
	Status            string                       `json:"status"`
	Current           bool                         `json:"current"`
	CheckpointCurrent bool                         `json:"checkpoint_current"`
	State             monitorEconomicNativeSummary `json:"state"`
	Issue             string                       `json:"issue,omitempty"`
}

type monitorEconomicTestSink struct {
	economics chan monitorEconomicTestEvent
	peers     chan monitorServiceEvent
}

func (self *monitorEconomicTestSink) Write(raw []byte) (int, error) {
	return self.WriteContext(context.Background(), raw)
}
func (self *monitorEconomicTestSink) WriteContext(ctx context.Context, raw []byte) (int, error) {
	var event monitorEconomicTestEvent
	if err := json.Unmarshal(raw, &event); err != nil {
		return 0, err
	}
	switch event.Schema {
	case "urnetwork-mainnet-native-economic-event-v1":
		select {
		case self.economics <- event:
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	case "urnetwork-mainnet-validator-event-v1":
		var peer monitorServiceEvent
		if err := json.Unmarshal(raw, &peer); err != nil {
			return 0, err
		}
		select {
		case self.peers <- peer:
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}
	return len(raw), nil
}

type monitorEconomicTestFixture struct {
	source      *economicEmissionFixture
	services    *monitorServicesFixture
	policy      monitorEconomicNativePolicy
	url         string
	ctx         context.Context
	unavailable atomic.Bool
}

// The wire fee is independently encoded through the fixture metadata. Its
// actual body/header changes together; no fake production decoder is installed.
func economicNativeFeeFixture(t *testing.T, source *economicEmissionFixture, tip uint64) []byte {
	t.Helper()
	economicEmissionContextBody(t, source)
	payer := bytes.Repeat([]byte{0x11}, 32)
	source.policy.FeePayers = []string{"0x" + hex.EncodeToString(payer)}
	fee := rootReceiptEventFixture(t, source.chain.metadata, "TransactionPayment.TransactionFeePaid", 0, payer, binary.LittleEndian.AppendUint64(nil, 12), binary.LittleEndian.AppendUint64(nil, tip))
	terminal := rootReceiptEventFixture(t, source.chain.metadata, "System.ExtrinsicSuccess", 0)
	raw := append(append([]byte{8}, fee...), terminal...)
	source.set(t, 102, "Events", raw)
	return raw
}

func newMonitorEconomicTestFixture(t *testing.T, peer bool) *monitorEconomicTestFixture {
	t.Helper()
	roles := []string{}
	if peer {
		roles = append(roles, "validator-a")
	}
	fixture := &monitorEconomicTestFixture{source: newEconomicEmissionFixture(t), services: newMonitorServicesFixture(t, roles...)}
	economicNativeFeeFixture(t, fixture.source, 3)
	source := fixture.source
	source.policy.Network.NativeChain = "fixture-mainnet"
	source.chain.action.Scope.NativeChain = "fixture-mainnet"
	prior := source.chain.fault
	source.chain.fault = func(method string, params []json.RawMessage, count int) (any, bool) {
		if method == "system_version" {
			return "synthetic-node", true
		}
		return prior(method, params, count)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		raw, err := io.ReadAll(io.LimitReader(request.Body, 1<<20))
		if err != nil {
			http.Error(w, "read", 400)
			return
		}
		var call struct {
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(raw, &call); err != nil {
			http.Error(w, "json", 400)
			return
		}
		if fixture.unavailable.Load() && call.Method == "state_getStorage" && len(call.Params) == 2 {
			var key, hash string
			_ = json.Unmarshal(call.Params[0], &key)
			_ = json.Unmarshal(call.Params[1], &hash)
			if key == source.eventsKey && hash == source.policy.Through.Hash {
				http.Error(w, "synthetic archive unavailable", 503)
				return
			}
		}
		request.Body = io.NopCloser(bytes.NewReader(raw))
		source.chain.serve(w, request)
	}))
	t.Cleanup(server.Close)
	fixture.url = server.URL
	fixture.policy = monitorEconomicNativePolicy{Role: "native-a", Observation: source.policy, BatchBlocks: 2, HistoryEntries: 32, StallSeconds: 60, HistoricalFinality: "owned-rpc-assertion"}
	return fixture
}

func (self *monitorEconomicTestFixture) prepare(t *testing.T) {
	t.Helper()
	self.services.policy.NativeEconomics = []monitorEconomicNativePolicy{self.policy}
	self.services.writePolicy(t)
	checkpoint, _ := monitorEconomicNativePaths(self.services.checkpointPath, self.services.metricsPath, self.policy.Role)
	provisionMonitorTestCustody(t, checkpoint)
	self.ctx = monitorTestStorageContext(t, t.Context(), self.services.args(self.url))
}

type monitorEconomicTestRun struct {
	fixture    *monitorEconomicTestFixture
	sink       *monitorEconomicTestSink
	cancel     context.CancelFunc
	done       chan struct{}
	resume     chan struct{}
	peerResume chan struct{}
	exit       int
	diagnostic bytes.Buffer
}

func (self *monitorEconomicTestFixture) start(t *testing.T, hooks monitorServiceHooks) *monitorEconomicTestRun {
	t.Helper()
	if self.ctx == nil {
		self.prepare(t)
	}
	ctx, cancel := context.WithCancel(self.ctx)
	run := &monitorEconomicTestRun{fixture: self, sink: &monitorEconomicTestSink{economics: make(chan monitorEconomicTestEvent, 8), peers: make(chan monitorServiceEvent, 8)}, cancel: cancel, done: make(chan struct{}), resume: make(chan struct{}, 2), peerResume: make(chan struct{}, 2)}
	if hooks.wait == nil {
		hooks.wait = func(ctx context.Context, role string, _ time.Duration) bool {
			var resume <-chan struct{}
			if role == self.policy.Role {
				resume = run.resume
			} else if role == "validator-a" {
				resume = run.peerResume
			}
			select {
			case <-ctx.Done():
				return false
			case <-resume:
				return true
			}
		}
	}
	go func() {
		defer close(run.done)
		run.exit = runMainWithMonitorHooks(ctx, self.services.args(self.url), run.sink, &run.diagnostic, self.services.clock.now, hooks)
	}()
	t.Cleanup(func() { run.stop(t) })
	return run
}

func (self *monitorEconomicTestRun) next(t *testing.T) monitorEconomicTestEvent {
	t.Helper()
	select {
	case event := <-self.sink.economics:
		return event
	case <-self.done:
		t.Fatal("public native economic monitor ended before its sample", self.exit, self.diagnostic.String())
	case <-time.After(60 * time.Second):
		t.Fatal("public native economic sample did not arrive")
	}
	return monitorEconomicTestEvent{}
}

func (self *monitorEconomicTestRun) stop(t *testing.T) {
	t.Helper()
	self.cancel()
	select {
	case <-self.done:
	case <-time.After(10 * time.Second):
		t.Fatal("native economic command failed to join")
	}
}

func (self *monitorEconomicTestFixture) record(t *testing.T) monitorEconomicNativeCheckpoint {
	t.Helper()
	checkpoint, _ := monitorEconomicNativePaths(self.services.checkpointPath, self.services.metricsPath, self.policy.Role)
	raw, err := os.ReadFile(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	var record monitorEconomicNativeCheckpoint
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatal(err)
	}
	return record
}

func monitorEconomicTestFee(event monitorEconomicTestEvent) string {
	if event.State.ObservedFeesRao == nil {
		return "unknown"
	}
	return *event.State.ObservedFeesRao
}
