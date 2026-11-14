package miner

import (
	"context"
	"encoding/json"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urnetwork/sdk/v2026"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// A request for readiness must never receive the legacy process-liveness
// response when no provider observation owner exists.
func TestProviderProgressStatusRefusesUnownedReadiness(t *testing.T) {
	response := httptest.NewRecorder()
	(&Status{}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/provider-progress", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatal("provider readiness request received unowned process liveness", response.Code, response.Body.String())
	}
}

type providerProgressTestDevice struct {
	identity                     *sdk.Id
	connected, registered, ready bool
	reads                        atomic.Int32
	beforeReady                  func()
}

func (self *providerProgressTestDevice) GetClientId() *sdk.Id {
	self.reads.Add(1)
	return self.identity
}
func (self *providerProgressTestDevice) GetProviderConnected() bool           { return self.connected }
func (self *providerProgressTestDevice) GetProviderClientKeyRegistered() bool { return self.registered }
func (self *providerProgressTestDevice) GetProviderReady() bool {
	if self.beforeReady != nil {
		self.beforeReady()
	}
	return self.ready
}

func newProviderProgressTestDevice() *providerProgressTestDevice {
	return &providerProgressTestDevice{identity: sdk.RequireIdFromBytes([]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}), connected: true, registered: true, ready: true}
}
func newProviderProgressTestOwner(t *testing.T) *providerProgressOwner {
	t.Helper()
	owner, err := newProviderProgressOwner("standalone", []providerProgressConfigMember{{Slot: "direct", ApiUrl: "https://synthetic.invalid", ConnectUrl: "wss://synthetic.invalid"}})
	if err != nil {
		t.Fatal(err)
	}
	return owner
}

func TestProviderProgressOwnedHttpUsesActualFactsAndFreshSequence(t *testing.T) {
	owner := newProviderProgressTestOwner(t)
	device := newProviderProgressTestDevice()
	if _, err := owner.attach(t.Context(), "direct", device); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	output := newProviderDiagnosticTestOwner(t, io.Discard)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	server := startProviderStatusServer(listener, output, cancel, providerStatusHooks{progress: owner})
	defer func() {
		if err := server.close(); err != nil {
			t.Fatal(err)
		}
	}()
	var previous uint64
	for index := 0; index < 2; index++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+listener.Addr().String()+"/provider-progress", nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := io.ReadAll(response.Body)
		closeErr := response.Body.Close()
		if err != nil || closeErr != nil {
			t.Fatal(err, closeErr)
		}
		value, err := protocol.DecodeProviderProgress(raw)
		if err != nil || response.StatusCode != 200 || response.Header.Get("Cache-Control") != "no-store" {
			t.Fatal("public owner did not expose bounded observations", string(raw), err)
		}
		if value.Sequence <= previous || value.Members[0].ClientId != device.identity.String() || !value.Members[0].Ready || !value.Members[0].Current || value.Proof != "unknown" || value.Settlement != "unknown" {
			t.Fatal("producer changed actual readiness provenance", value)
		}
		previous = value.Sequence
	}
	if device.reads.Load() != 2 {
		t.Fatal("HTTP did not sample the actual device", device.reads.Load())
	}
}

func TestProviderProgressRetiredGenerationCannotPublishReadiness(t *testing.T) {
	owner := newProviderProgressTestOwner(t)
	device := newProviderProgressTestDevice()
	generation, err := owner.attach(t.Context(), "direct", device)
	if err != nil {
		t.Fatal(err)
	}
	device.beforeReady = func() { owner.retire("direct", generation) }
	value, err := owner.snapshot(t.Context())
	if err != nil || value.Members[0].Ready || value.Members[0].Current || value.Members[0].Lifecycle != "stopped" {
		t.Fatal("retired device escaped its retained generation", value, err)
	}
	replacement := newProviderProgressTestDevice()
	next, err := owner.attach(t.Context(), "direct", replacement)
	if err != nil {
		t.Fatal(err)
	}
	owner.retire("direct", generation)
	value, err = owner.snapshot(t.Context())
	if err != nil || next <= generation || !value.Members[0].Ready || value.Members[0].Generation != next {
		t.Fatal("stale close retired the replacement", value, err)
	}
}

func TestProviderProgressCanceledObservationDoesNotReadOrRefresh(t *testing.T) {
	owner := newProviderProgressTestOwner(t)
	device := newProviderProgressTestDevice()
	memberCtx, cancelMember := context.WithCancel(t.Context())
	defer cancelMember()
	if _, err := owner.attach(memberCtx, "direct", device); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := owner.snapshot(ctx); err == nil || device.reads.Load() != 0 {
		t.Fatal("canceled observation read a provider", err)
	}
	device.beforeReady = cancelMember
	value, err := owner.snapshot(t.Context())
	if err != nil || value.Members[0].Current || value.Members[0].Ready {
		t.Fatal("canceled member retained current readiness", value, err)
	}
}

func TestProviderProgressConcurrentSamplesKeepDistinctSequence(t *testing.T) {
	owner := newProviderProgressTestOwner(t)
	if _, err := owner.attach(t.Context(), "direct", newProviderProgressTestDevice()); err != nil {
		t.Fatal(err)
	}
	values := make(chan uint64, 16)
	var workers sync.WaitGroup
	for range 16 {
		workers.Go(func() {
			value, err := owner.snapshot(t.Context())
			if err != nil {
				t.Error(err)
				return
			}
			values <- value.Sequence
		})
	}
	workers.Wait()
	close(values)
	seen := map[uint64]bool{}
	for value := range values {
		if seen[value] {
			t.Fatal("sequence reused under concurrent HTTP", value)
		}
		seen[value] = true
	}
	if len(seen) != 16 {
		t.Fatal("sample census differs", len(seen))
	}
}

func TestProviderProgressPublicSwarmWiresRealDeviceObservation(t *testing.T) {
	config := validProviderSwarmConfig(t)
	swarm, err := NewProviderSwarm(&config)
	if err != nil {
		t.Fatal(err)
	}
	config.ListenAddress = "127.0.0.1:0"
	device := newProviderProgressTestDevice()
	entered := make(chan struct{})
	swarm.startMember = func(context.Context, ProviderSwarmMember, func(error)) (*providerSwarmInstance, error) {
		close(entered)
		return &providerSwarmInstance{progressDevice: device}, nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- swarm.Run(ctx) }()
	defer func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("public swarm did not join")
		}
	}()
	waitSwarmControlTestSignal(t, entered)
	if err := swarm.controlMember(ctx, config.Members[0].ID, true); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	swarm.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/provider-progress", nil))
	value, err := protocol.DecodeProviderProgress(response.Body.Bytes())
	if err != nil || len(value.Members) != 1 || !value.Members[0].Ready || value.Members[0].ClientId != device.identity.String() {
		t.Fatal("public Run omitted actual device identity", response.Body.String(), err)
	}
	if err := swarm.controlMember(ctx, config.Members[0].ID, false); err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	swarm.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/provider-progress", nil))
	value, err = protocol.DecodeProviderProgress(response.Body.Bytes())
	if err != nil || value.Members[0].Ready || value.Members[0].Current {
		t.Fatal("disabled public member retained readiness", response.Body.String(), err)
	}
}

// The actual public swarm lifetime exposes each configured member even when
// an instance has no SDK observation. Running count is not identity evidence.
func TestProviderProgressSwarmExposesMemberCensus(t *testing.T) {
	fixture := newSwarmPublishedFixture(t)
	response := httptest.NewRecorder()
	fixture.swarm.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/provider-progress", nil))
	var value struct {
		Schema  string `json:"schema"`
		Members []struct {
			Slot  string `json:"slot"`
			Ready bool   `json:"ready"`
		}
	}
	if err := json.Unmarshal(response.Body.Bytes(), &value); err != nil || response.Code != http.StatusOK || value.Schema != "urnetwork-provider-progress-v1" || len(value.Members) != 2 {
		t.Fatal("public provider progress omitted actual member census", response.Code, response.Body.String(), err)
	}
	if value.Members[0].Ready || value.Members[1].Ready {
		t.Fatal("lifecycle count fabricated provider readiness")
	}
}
