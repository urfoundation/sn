// Independent progress roles use real HTTP and durable publication while one
// peer remains inside its owned retry wait. No scheduler sleep proves ordering.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

type monitorProgressRetryEvent struct {
	Schema            string `json:"schema"`
	Role              string `json:"role"`
	Status            string `json:"status"`
	Current           bool   `json:"current"`
	CheckpointCurrent bool   `json:"checkpoint_current"`
}
type monitorProgressRetrySink struct {
	events chan monitorProgressRetryEvent
}

func (self *monitorProgressRetrySink) Write(raw []byte) (int, error) {
	return self.WriteContext(context.Background(), raw)
}
func (self *monitorProgressRetrySink) WriteContext(ctx context.Context, raw []byte) (int, error) {
	var event monitorProgressRetryEvent
	if err := json.Unmarshal(raw, &event); err != nil {
		return 0, err
	}
	if event.Schema == "urnetwork-mainnet-provider-event-v1" || event.Schema == "urnetwork-mainnet-claim-event-v1" {
		select {
		case self.events <- event:
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}
	return len(raw), nil
}

func monitorProgressPublicRetryPeer(t *testing.T, kind string) {
	t.Helper()
	fixture := newMonitorServicesFixture(t)
	var firstBody, secondBody []byte
	var callsA, callsB atomic.Int32
	var healthy atomic.Bool
	sourceA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callsA.Add(1)
		if !healthy.Load() {
			w.Header().Set("Retry-After", "60")
			w.WriteHeader(429)
			return
		}
		_, _ = w.Write(firstBody)
	}))
	defer sourceA.Close()
	sourceB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { callsB.Add(1); _, _ = w.Write(secondBody) }))
	defer sourceB.Close()
	roleA, roleB := kind+"-a", kind+"-b"
	peerPath := ""
	if kind == "provider" {
		one, two := monitorProviderTestValue(fixture.clock.now()), monitorProviderTestValue(fixture.clock.now())
		two.Source.ConfigHash = strings.Repeat("4", 64)
		two.Members[0].ClientId = "44444444-4444-4444-4444-444444444444"
		firstBody, _ = json.Marshal(one)
		secondBody, _ = json.Marshal(two)
		onePolicy, twoPolicy := monitorProviderTestPolicy(one, sourceA.URL+"/provider-progress"), monitorProviderTestPolicy(two, sourceB.URL+"/provider-progress")
		twoPolicy.Role = roleB
		fixture.policy.Providers = []monitorProviderPolicy{onePolicy, twoPolicy}
		peerPath, _ = monitorProviderPaths(fixture.checkpointPath, fixture.metricsPath, roleB)
	} else {
		one, two := monitorClaimTestValue(fixture.clock.now()), monitorClaimTestValue(fixture.clock.now())
		two.Member = "peer"
		two.DeclaredPool.NoId = "8"
		two.Entries[0].Observation.Pool = *two.DeclaredPool
		firstBody, _ = json.Marshal(one)
		secondBody, _ = json.Marshal(two)
		onePolicy, twoPolicy := monitorClaimTestPolicy(t, one, sourceA.URL+"/claim-progress", fixture.clock.now()), monitorClaimTestPolicy(t, two, sourceB.URL+"/claim-progress", fixture.clock.now())
		twoPolicy.Role = roleB
		fixture.policy.Claims = []monitorClaimPolicy{onePolicy, twoPolicy}
		peerPath, _ = monitorClaimPaths(fixture.checkpointPath, fixture.metricsPath, roleB)
	}
	fixture.writePolicy(t)
	url, chainEntered, _ := monitorServicesBlockedChain(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	entered, release := make(chan time.Duration, 1), make(chan struct{})
	hooks := monitorServiceHooks{rpcWait: func(waitCtx context.Context, role string, delay time.Duration) error {
		if role != roleA {
			return waitRpcReadRetry(waitCtx, delay)
		}
		entered <- delay
		select {
		case <-release:
			return nil
		case <-waitCtx.Done():
			return waitCtx.Err()
		}
	}, wait: func(waitCtx context.Context, _ string, _ time.Duration) bool { <-waitCtx.Done(); return false }}
	sink := &monitorProgressRetrySink{events: make(chan monitorProgressRetryEvent, 8)}
	var diagnostic bytes.Buffer
	done := make(chan int, 1)
	go func() {
		done <- runMonitorStorageTestWithHooks(t, ctx, fixture.args(url), sink, &diagnostic, fixture.clock.now, hooks)
	}()
	joined := false
	t.Cleanup(func() {
		cancel()
		if !joined {
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				t.Error("public progress retry did not join")
			}
		}
	})
	select {
	case delay := <-entered:
		if delay != time.Minute {
			t.Fatal("public progress role ignored Retry-After", delay)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("public retry barrier not reached")
	}
	select {
	case <-chainEntered:
	case <-time.After(10 * time.Second):
		t.Fatal("actual chain peer did not finish checkpoint admission")
	}
	select {
	case peer := <-sink.events:
		if peer.Role != roleB || !peer.Current || !peer.CheckpointCurrent {
			t.Fatal("retry published missing sample or stopped independent peer", peer)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("healthy progress peer did not publish while other GET retried")
	}
	before, err := os.ReadFile(peerPath)
	if err != nil {
		t.Fatal(err)
	}
	if callsA.Load() != 1 || callsB.Load() != 1 {
		t.Fatal("progress role issued unpaced or repeated peer reads", callsA.Load(), callsB.Load())
	}
	healthy.Store(true)
	close(release)
	select {
	case recovered := <-sink.events:
		if recovered.Role != roleA || !recovered.Current || recovered.Status != "ok" || !recovered.CheckpointCurrent {
			t.Fatal("owned progress retry did not recover same role", recovered)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("public progress recovery did not publish")
	}
	after, err := os.ReadFile(peerPath)
	if err != nil || !bytes.Equal(before, after) || callsA.Load() != 2 || callsB.Load() != 1 {
		t.Fatal("affected progress recovery reset or resampled peer", err, callsA.Load(), callsB.Load())
	}
	cancel()
	select {
	case exit := <-done:
		joined = true
		if exit != 0 {
			t.Fatal("public progress retry cancellation did not join", exit, diagnostic.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("progress retry workers were abandoned")
	}
}

func TestMonitorProviderPublicRateLimitKeepsPeerCurrent(t *testing.T) {
	monitorProgressPublicRetryPeer(t, "provider")
}
func TestMonitorClaimPublicRateLimitKeepsPeerCurrent(t *testing.T) {
	monitorProgressPublicRetryPeer(t, "claim")
}

// Cancellation occurs only after the real parser has received the complete
// foreign identity or authentication response, at its joined body-close seam.
type monitorProgressCancelBody struct {
	*bytes.Reader
	cancel context.CancelFunc
	closes *atomic.Int32
}

func (self *monitorProgressCancelBody) Close() error {
	self.closes.Add(1)
	self.cancel()
	return syscall.EIO
}

type monitorProgressCanceledOutput struct{}

func (monitorProgressCanceledOutput) Write([]byte) (int, error) { return 0, context.Canceled }

func TestMonitorProgressOwnedWorkersRetainHardCauseDuringCancellation(t *testing.T) {
	for _, kind := range []string{"provider", "claim"} {
		for _, status := range []int{200, 403} {
			for _, failedOutput := range []bool{false, true} {
				fixture := newMonitorServicesFixture(t)
				var providerPolicy monitorProviderPolicy
				var claimPolicy monitorClaimPolicy
				var raw []byte
				var err error
				if kind == "provider" {
					value := monitorProviderTestValue(fixture.clock.now())
					providerPolicy = monitorProviderTestPolicy(value, "https://monitor.example/provider-progress")
					fixture.policy.Providers = []monitorProviderPolicy{providerPolicy}
					value.Source.ConfigHash = strings.Repeat("4", 64)
					raw, err = json.Marshal(value)
				} else {
					value := monitorClaimTestValue(fixture.clock.now())
					claimPolicy = monitorClaimTestPolicy(t, value, "https://monitor.example/claim-progress", fixture.clock.now())
					fixture.policy.Claims = []monitorClaimPolicy{claimPolicy}
					value.Member = "foreign"
					raw, err = json.Marshal(value)
				}
				if err != nil {
					t.Fatal(err)
				}
				fixture.writePolicy(t)
				ctx, cancel := context.WithCancel(monitorTestStorageContext(t, t.Context(), fixture.args("https://rpc.example")))
				defer cancel()
				var calls, closes atomic.Int32
				client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
					calls.Add(1)
					return &http.Response{StatusCode: status, Body: &monitorProgressCancelBody{Reader: bytes.NewReader(raw), cancel: cancel, closes: &closes}}, nil
				})}
				var output io.Writer = io.Discard
				if failedOutput {
					output = monitorProgressCanceledOutput{}
				}
				exit, observed := 0, ""
				if kind == "provider" {
					worker, err := openMonitorProviderWorker(ctx, providerPolicy, monitorTestExpectation(), fixture.checkpointPath, fixture.metricsPath, monitorServiceHooks{})
					if err != nil {
						t.Fatal(err)
					}
					worker.client = client
					exit = worker.run(ctx, time.Minute, output, io.Discard, fixture.clock.now, monitorServiceHooks{})
					observed = worker.state.Status
					if err := worker.close(monitorServiceHooks{}); err != nil {
						t.Fatal(err)
					}
				} else {
					worker, err := openMonitorClaimWorker(ctx, claimPolicy, monitorTestExpectation(), fixture.checkpointPath, fixture.metricsPath, monitorServiceHooks{})
					if err != nil {
						t.Fatal(err)
					}
					worker.client = client
					exit = worker.run(ctx, time.Minute, output, io.Discard, fixture.clock.now, monitorServiceHooks{})
					observed = worker.state.Status
					if err := worker.close(monitorServiceHooks{}); err != nil {
						t.Fatal(err)
					}
				}
				want := "identity"
				if status == 403 {
					want = "authentication"
				}
				if exit != 3 || observed != want || calls.Load() != 1 || closes.Load() != 1 {
					t.Fatal("parent cancellation erased an observed hard progress cause", kind, status, failedOutput, exit, observed, calls.Load(), closes.Load())
				}
			}
		}
	}
}
