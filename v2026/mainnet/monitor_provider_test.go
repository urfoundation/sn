package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/diagnostics"
	"github.com/urfoundation/sn/v2026/protocol"
)

func monitorProviderTestValue(now time.Time) protocol.ProviderProgress {
	return protocol.ProviderProgress{Schema: protocol.ProviderProgressSchema, Source: protocol.ProviderProgressSource{Mode: "swarm", ConfigHash: strings.Repeat("1", 64)}, InstanceId: strings.Repeat("2", 32), StartedAt: now.Add(-time.Hour).Format(time.RFC3339Nano), ObservedAt: now.Format(time.RFC3339Nano), Sequence: 1, Members: []protocol.ProviderMemberProgress{{Slot: "member-a", Generation: 1, ClientId: "11111111-1111-1111-1111-111111111111", Lifecycle: "running", Current: true, Connected: true, KeyRegistered: true, Ready: true}}, Proof: "unknown", Settlement: "unknown"}
}
func monitorProviderTestPolicy(value protocol.ProviderProgress, endpoint string) monitorProviderPolicy {
	return monitorProviderPolicy{Role: "provider-a", Endpoint: endpoint, ExpectedSource: value.Source, Members: []monitorExpectedProviderMember{{Slot: value.Members[0].Slot, ClientId: value.Members[0].ClientId}}, FreshnessSeconds: 60}
}

func TestMonitorProviderExpectedRosterCannotComeFromCandidate(t *testing.T) {
	value := monitorProviderTestValue(time.Now().UTC())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _ = json.NewEncoder(w).Encode(value) }))
	defer server.Close()
	policy := monitorProviderTestPolicy(value, server.URL+"/provider-progress")
	policy.Members[0].ClientId = "33333333-3333-3333-3333-333333333333"
	candidate, code := readMonitorProvider(t.Context(), newMonitorProviderClient(), policy)
	if code != "identity" || candidate == nil {
		t.Fatal("HTTP success selected its own expected client", code)
	}
	state := &monitorProviderState{}
	state.observe(policy, candidate, code, time.Now().UTC())
	if state.current || state.Record != nil || !monitorProviderTerminal(state.Status) {
		t.Fatal("foreign ready member refreshed expected provider", state)
	}
}

type monitorProviderTestRoundTripper struct{ response *http.Response }

func (self monitorProviderTestRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return self.response, nil
}

type monitorProviderTestBody struct {
	*bytes.Reader
	closes atomic.Int32
}

func (self *monitorProviderTestBody) Close() error { self.closes.Add(1); return syscall.EIO }

func TestMonitorProviderIdentityAndAuthenticationDominateCloseFailure(t *testing.T) {
	value := monitorProviderTestValue(time.Now().UTC())
	policy := monitorProviderTestPolicy(value, "https://synthetic.invalid/provider-progress")
	value.Source.ConfigHash = strings.Repeat("3", 64)
	raw, _ := json.Marshal(value)
	body := &monitorProviderTestBody{Reader: bytes.NewReader(raw)}
	client := &http.Client{Transport: monitorProviderTestRoundTripper{response: &http.Response{StatusCode: 200, Body: body}}}
	_, code := readMonitorProvider(t.Context(), client, policy)
	if code != "identity" || body.closes.Load() != 1 {
		t.Fatal("close unavailability erased complete identity mismatch", code, body.closes.Load())
	}
	body = &monitorProviderTestBody{Reader: bytes.NewReader(nil)}
	client.Transport = monitorProviderTestRoundTripper{response: &http.Response{StatusCode: 403, Body: body}}
	_, code = readMonitorProvider(t.Context(), client, policy)
	if code != "authentication" || body.closes.Load() != 1 {
		t.Fatal("close unavailability erased authentication refusal", code, body.closes.Load())
	}
}

func TestMonitorProviderCancellationJoinsPartialHttpBody(t *testing.T) {
	entered, joined := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"schema":`))
		w.(http.Flusher).Flush()
		close(entered)
		<-r.Context().Done()
		close(joined)
	}))
	defer server.Close()
	policy := monitorProviderTestPolicy(monitorProviderTestValue(time.Now().UTC()), server.URL+"/provider-progress")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan string, 1)
	go func() { _, code := readMonitorProvider(ctx, newMonitorProviderClient(), policy); done <- code }()
	<-entered
	cancel()
	select {
	case code := <-done:
		if code != "unavailable" {
			t.Fatal(code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled read did not join")
	}
	select {
	case <-joined:
	case <-time.After(5 * time.Second):
		t.Fatal("canceled response body remained live")
	}
}

func TestMonitorProviderBoundsAndRedirectDoNotReachReplacement(t *testing.T) {
	var calls atomic.Int32
	replacement := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(200) }))
	defer replacement.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, replacement.URL+"/provider-progress", 302)
	}))
	defer source.Close()
	policy := monitorProviderTestPolicy(monitorProviderTestValue(time.Now().UTC()), source.URL+"/provider-progress")
	_, code := readMonitorProvider(t.Context(), newMonitorProviderClient(), policy)
	if code == "ok" || calls.Load() != 0 {
		t.Fatal("provider source followed a redirect", code, calls.Load())
	}
	large := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.CopyN(w, strings.NewReader(strings.Repeat("x", protocol.MaxProviderProgressBytes+1)), int64(protocol.MaxProviderProgressBytes+1))
	}))
	defer large.Close()
	policy.Endpoint = large.URL + "/provider-progress"
	if _, code := readMonitorProvider(t.Context(), newMonitorProviderClient(), policy); code != "invalid" {
		t.Fatal("oversized observation escaped bound", code)
	}
}

func TestMonitorProviderSequenceAndRestartCannotRefreshStaleSuccess(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	value := monitorProviderTestValue(now)
	policy := monitorProviderTestPolicy(value, "https://synthetic.invalid/provider-progress")
	state := &monitorProviderState{}
	state.observe(policy, &value, "ok", now)
	if !state.current || state.ready != 1 {
		t.Fatal("fresh actual observation refused", state)
	}
	state.observe(policy, &value, "ok", now.Add(time.Second))
	if state.current || state.Status != "stale" || state.Record.Sequence != 1 {
		t.Fatal("cached HTTP success refreshed readiness", state)
	}
	value.Sequence = 2
	value.ObservedAt = now.Add(2 * time.Second).Format(time.RFC3339Nano)
	state.observe(policy, &value, "ok", now.Add(2*time.Second))
	if !state.current || state.Incidents != 1 {
		t.Fatal("same instance did not recover without losing incident", state)
	}
	prior := *state.Record
	value.InstanceId = strings.Repeat("3", 32)
	value.Sequence = 1
	state.observe(policy, &value, "ok", now.Add(3*time.Second))
	if state.current || state.Record.InstanceId != prior.InstanceId {
		t.Fatal("old restart generation was accepted", state)
	}
	value.StartedAt = now.Add(3 * time.Second).Format(time.RFC3339Nano)
	value.ObservedAt = value.StartedAt
	state.observe(policy, &value, "ok", now.Add(3*time.Second))
	if !state.current || state.Restarts != 1 {
		t.Fatal("observed new generation did not retain restart census", state)
	}
	value.Sequence = 2
	value.Members[0].Generation = 0
	state.observe(policy, &value, "ok", now.Add(4*time.Second))
	if state.current || state.Status != "stale" {
		t.Fatal("same process erased member generation", state)
	}
}

func TestMonitorProviderOutageDoesNotInventReadinessOrProof(t *testing.T) {
	now := time.Now().UTC()
	value := monitorProviderTestValue(now)
	policy := monitorProviderTestPolicy(value, "https://synthetic.invalid/provider-progress")
	state := &monitorProviderState{}
	state.observe(policy, &value, "ok", now)
	state.observe(policy, nil, "unavailable", now.Add(time.Second))
	raw := string(renderMonitorProviderMetrics(policy, state, true))
	for _, name := range []string{"read_current", "ready_members", "all_expected_ready", "proof_progress_known", "settlement_progress_known"} {
		if !strings.Contains(raw, fmt.Sprintf("sn_mainnet_provider_%s{role=\"provider-a\"} 0", name)) {
			t.Fatal("outage exported fabricated progress", name, raw)
		}
	}
	if state.Record == nil || state.Record.Sequence != 1 || state.OutageSince.IsZero() {
		t.Fatal("outage erased original evidence", state)
	}
	if strings.Contains(raw, value.Members[0].ClientId) || strings.Contains(raw, value.Source.ConfigHash) || strings.Contains(raw, value.Members[0].Slot) {
		t.Fatal("candidate value became a metric label", raw)
	}
}

type monitorProviderTestEvent struct {
	Schema, Role, Status string
	Current              bool
	CheckpointCurrent    bool `json:"checkpoint_current"`
	State                *monitorProviderEventState
}
type monitorProviderTestSink struct{ events chan monitorProviderTestEvent }

func (self *monitorProviderTestSink) Write(raw []byte) (int, error) {
	return self.WriteContext(context.Background(), raw)
}
func (self *monitorProviderTestSink) WriteContext(ctx context.Context, raw []byte) (int, error) {
	var event monitorProviderTestEvent
	if err := json.Unmarshal(raw, &event); err != nil {
		return 0, err
	}
	if event.Schema == "urnetwork-mainnet-provider-event-v1" {
		select {
		case self.events <- event:
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}
	return len(raw), nil
}
func (self *monitorProviderTestSink) next(t *testing.T) monitorProviderTestEvent {
	t.Helper()
	select {
	case event := <-self.events:
		return event
	case <-time.After(10 * time.Second):
		t.Fatal("provider public command emitted no sample")
		return monitorProviderTestEvent{}
	}
}

func TestMonitorProviderPublicRolesRecoverWithoutResettingPeer(t *testing.T) {
	fixture := newMonitorServicesFixture(t)
	now := fixture.clock.now()
	firstValue := monitorProviderTestValue(now)
	secondValue := monitorProviderTestValue(now)
	secondValue.Source.ConfigHash = strings.Repeat("4", 64)
	secondValue.Members[0].ClientId = "44444444-4444-4444-4444-444444444444"
	var failing atomic.Bool
	failing.Store(true)
	var firstSequence, secondSequence atomic.Uint64
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failing.Load() {
			w.WriteHeader(503)
			return
		}
		value := firstValue
		value.Sequence = firstSequence.Add(1)
		_ = json.NewEncoder(w).Encode(value)
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		value := secondValue
		value.Sequence = secondSequence.Add(1)
		_ = json.NewEncoder(w).Encode(value)
	}))
	defer second.Close()
	one, two := monitorProviderTestPolicy(firstValue, first.URL+"/provider-progress"), monitorProviderTestPolicy(secondValue, second.URL+"/provider-progress")
	two.Role = "provider-b"
	fixture.policy.Providers = []monitorProviderPolicy{one, two}
	fixture.writePolicy(t)
	url, _, _ := monitorServicesBlockedChain(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	sink := &monitorProviderTestSink{events: make(chan monitorProviderTestEvent, 8)}
	resume := map[string]chan struct{}{one.Role: make(chan struct{}), two.Role: make(chan struct{})}
	// Exhaust only the real failing sample at its owned wait boundary.
	hooks := monitorServiceHooks{rpcWait: func(context.Context, string, time.Duration) error { return context.DeadlineExceeded }, wait: func(ctx context.Context, role string, _ time.Duration) bool {
		select {
		case <-resume[role]:
			return true
		case <-ctx.Done():
			return false
		}
	}}
	args := fixture.args(url)
	ctx = monitorTestStorageContext(t, ctx, args)
	done := make(chan int, 1)
	go func() {
		defer close(done)
		done <- runMainWithMonitorHooks(ctx, args, sink, nil, fixture.clock.now, hooks)
	}()
	t.Cleanup(func() { joinMonitorTestWorker(t, cancel, done) })
	observed := map[string]monitorProviderTestEvent{}
	for range 2 {
		event := monitorTestEvent(t, done, sink.events)
		observed[event.Role] = event
	}
	if observed[one.Role].Status != "unavailable" || observed[one.Role].Current || observed[two.Role].Status != "ok" || !observed[two.Role].Current {
		t.Fatal("provider outage suppressed its independently expected peer", observed)
	}
	peerCheckpoint, _ := monitorProviderPaths(fixture.checkpointPath, fixture.metricsPath, two.Role)
	before, err := os.ReadFile(peerCheckpoint)
	if err != nil {
		t.Fatal(err)
	}
	failing.Store(false)
	resumeMonitorTestWorker(t, done, resume[one.Role])
	event := monitorTestEvent(t, done, sink.events)
	if event.Role != one.Role || event.Status != "ok" || !event.Current || event.State.Incidents != 1 {
		t.Fatal("provider recovery lost its original outage", event)
	}
	after, err := os.ReadFile(peerCheckpoint)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("affected recovery reset peer checkpoint", err)
	}
	resumeMonitorTestWorker(t, done, resume[two.Role])
	event = monitorTestEvent(t, done, sink.events)
	if event.Role != two.Role || event.State.Sequence != 2 || !event.Current {
		t.Fatal("healthy provider did not retain its live lifetime", event)
	}
}

func TestMonitorProviderCheckpointReopenCannotAcceptCachedOrLostEvidence(t *testing.T) {
	fixture := newMonitorServicesFixture(t)
	value := monitorProviderTestValue(fixture.clock.now())
	policy := monitorProviderTestPolicy(value, "https://synthetic.invalid/provider-progress")
	fixture.policy.Providers = []monitorProviderPolicy{policy}
	fixture.writePolicy(t)
	ctx := monitorTestStorageContext(t, t.Context(), fixture.args("https://synthetic.invalid"))
	worker, err := openMonitorProviderWorker(ctx, policy, monitorTestExpectation(), fixture.checkpointPath, fixture.metricsPath, monitorServiceHooks{})
	if err != nil {
		t.Fatal(err)
	}
	worker.state.observe(policy, &value, "ok", fixture.clock.now())
	if err := worker.save(); err != nil {
		t.Fatal(err)
	}
	if err := worker.close(monitorServiceHooks{}); err != nil {
		t.Fatal(err)
	}
	next, err := openMonitorProviderWorker(ctx, policy, monitorTestExpectation(), fixture.checkpointPath, fixture.metricsPath, monitorServiceHooks{})
	if err != nil {
		t.Fatal(err)
	}
	if next.state.current || next.state.Record.Sequence != 1 {
		t.Fatal("observer restart invented current readiness", next.state)
	}
	next.state.observe(policy, &value, "ok", fixture.clock.now())
	if next.state.current || next.state.Status != "stale" {
		t.Fatal("observer restart accepted cached HTTP success", next.state)
	}
	path := next.checkpoint.path
	if err := next.close(monitorServiceHooks{}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if owner, err := openMonitorProviderWorker(ctx, policy, monitorTestExpectation(), fixture.checkpointPath, fixture.metricsPath, monitorServiceHooks{}); err == nil {
		owner.close(monitorServiceHooks{})
		t.Fatal("lost checkpoint silently reset provider history")
	}
}

func TestMonitorProviderPolicyRejectsImplicitRosterAndUnsafeEndpoint(t *testing.T) {
	fixture := newMonitorServicesFixture(t)
	value := monitorProviderTestValue(fixture.clock.now())
	policy := monitorProviderTestPolicy(value, "https://synthetic.invalid/provider-progress")
	fixture.policy.Providers = []monitorProviderPolicy{policy}
	fixture.writePolicy(t)
	loaded, err := loadMonitorServices(t.Context(), fixture.policyPath, monitorTestExpectation(), fixture.checkpointPath, fixture.metricsPath)
	if err != nil || len(loaded.Providers) != 1 {
		t.Fatal("explicit provider census was not admitted", err)
	}
	for _, endpoint := range []string{"http://synthetic.invalid/provider-progress", "https://user:pass@synthetic.invalid/provider-progress", "https://synthetic.invalid/provider-progress?source=other"} {
		candidate := policy
		candidate.Endpoint = endpoint
		if candidate.validate() == nil {
			t.Fatal("unsafe provider route admitted", endpoint)
		}
	}
	policy.Members = nil
	if policy.validate() == nil {
		t.Fatal("candidate can supply its own expected roster")
	}
}

func TestMonitorProviderSharedOutputBoundsAreAdmittedBeforeOwners(t *testing.T) {
	fixture := newMonitorServicesFixture(t)
	fixture.policy.Validators = make([]monitorValidatorPolicy, 8)
	fixture.policy.Operators = make([]monitorOperatorPolicy, 4)
	fixture.policy.Providers = make([]monitorProviderPolicy, 3)
	fixture.writePolicy(t)
	if _, err := loadMonitorServices(t.Context(), fixture.policyPath, monitorTestExpectation(), fixture.checkpointPath, fixture.metricsPath); !errors.Is(err, errMonitorServicesCensus) {
		t.Fatal("policy exceeded shared output domain capacity before role validation", err)
	}
	value := monitorProviderTestValue(time.Now().UTC())
	policy := monitorProviderTestPolicy(value, "https://synthetic.invalid/provider-progress")
	value.Members = make([]protocol.ProviderMemberProgress, maxMonitorProviderMembers)
	for index := range value.Members {
		value.Members[index] = protocol.ProviderMemberProgress{Slot: strings.Repeat("x", 128), ClientId: "11111111-1111-1111-1111-111111111111"}
	}
	worker := &monitorProviderWorker{policy: policy, state: &monitorProviderState{Record: &value, Incidents: 1}}
	raw, err := json.Marshal(worker.eventState(false))
	if err != nil || len(raw) > diagnostics.MaximumRecordBytes/2 || bytes.Contains(raw, []byte(value.Members[0].ClientId)) {
		t.Fatal("provider diagnostic expanded with candidate census", len(raw), err)
	}
}

// The largest admitted roster passes through the real public command, owned
// checkpoint and bounded exporter. Its full wire census must not become one
// oversized diagnostic record that suppresses the only readiness event.
func TestMonitorProviderPublicMaximumRosterRetainsBoundedEvent(t *testing.T) {
	fixture := newMonitorServicesFixture(t)
	value := monitorProviderTestValue(fixture.clock.now())
	policy := monitorProviderTestPolicy(value, "")
	value.Members = nil
	policy.Members = nil
	for index := range maxMonitorProviderMembers {
		slot := fmt.Sprintf("%03d%s", index, strings.Repeat("x", 125))
		identity := fmt.Sprintf("%08x-1111-1111-1111-111111111111", index+1)
		value.Members = append(value.Members, protocol.ProviderMemberProgress{Slot: slot, Generation: 1, ClientId: identity, Lifecycle: "running", Current: true, Connected: true, KeyRegistered: true, Ready: true})
		policy.Members = append(policy.Members, monitorExpectedProviderMember{Slot: slot, ClientId: identity})
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _ = json.NewEncoder(w).Encode(value) }))
	defer server.Close()
	policy.Endpoint = server.URL + "/provider-progress"
	fixture.policy.Providers = []monitorProviderPolicy{policy}
	fixture.writePolicy(t)
	url, _, _ := monitorServicesBlockedChain(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	sink := &monitorProviderTestSink{events: make(chan monitorProviderTestEvent, 2)}
	done := make(chan int, 1)
	attempted := make(chan struct{}, 2)
	resume := make(chan struct{})
	hooks := monitorServiceHooks{afterEvent: func(_ context.Context, role string) {
		if role == policy.Role {
			attempted <- struct{}{}
		}
	}, wait: func(ctx context.Context, role string, _ time.Duration) bool {
		if role != policy.Role {
			<-ctx.Done()
			return false
		}
		select {
		case <-resume:
			return true
		case <-ctx.Done():
			return false
		}
	}}
	args := fixture.args(url)
	ctx = monitorTestStorageContext(t, ctx, args)
	go func() {
		defer close(done)
		done <- runMainWithMonitorHooks(ctx, args, sink, nil, fixture.clock.now, hooks)
	}()
	t.Cleanup(func() { joinMonitorTestWorker(t, cancel, done) })
	// The second completed sample exports the actual first offer's drop count.
	// An oversized diagnostic is best effort and does not terminate the owner.
	monitorTestEvent(t, done, attempted)
	resumeMonitorTestWorker(t, done, resume)
	monitorTestEvent(t, done, attempted)
	checkpoint, metrics := monitorProviderPaths(fixture.checkpointPath, fixture.metricsPath, policy.Role)
	metricBytes, err := os.ReadFile(metrics)
	want := fmt.Sprintf("sn_mainnet_provider_output_dropped_total{role=%q,stream=\"events\"} 0\n", policy.Role)
	if err != nil || !strings.Contains(string(metricBytes), want) {
		t.Fatal("maximum-roster event was dropped by actual bounded exporter", string(metricBytes), err)
	}
	event := monitorTestEvent(t, done, sink.events)
	if !event.Current || event.State.ReadyMembers != maxMonitorProviderMembers || event.State.ExpectedMembers != maxMonitorProviderMembers {
		t.Fatal("maximum expected roster lost public readiness event", event)
	}
	raw, err := os.ReadFile(checkpoint)
	if err != nil || len(raw) <= diagnostics.MaximumRecordBytes || len(raw) > maxMonitorProviderCheckpointBytes {
		t.Fatal("control did not exercise a roster larger than the diagnostic queue record", len(raw), err)
	}
}

// A lost post-sync acknowledgement reopens the exact original checkpoint only
// after its descriptor joins. The next real response advances the retained
// sequence, without treating the incomplete publication as a fresh owner.
func TestMonitorProviderPublicLostCheckpointAckReconcilesOriginal(t *testing.T) {
	fixture := newMonitorServicesFixture(t)
	value := monitorProviderTestValue(fixture.clock.now())
	var sequence atomic.Uint64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		copyValue := value
		copyValue.Sequence = sequence.Add(1)
		_ = json.NewEncoder(w).Encode(copyValue)
	}))
	defer server.Close()
	policy := monitorProviderTestPolicy(value, server.URL+"/provider-progress")
	fixture.policy.Providers = []monitorProviderPolicy{policy}
	fixture.writePolicy(t)
	url, _, _ := monitorServicesBlockedChain(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	sink := &monitorProviderTestSink{events: make(chan monitorProviderTestEvent, 3)}
	done := make(chan int, 1)
	resume := make(chan struct{})
	var once sync.Once
	var closed atomic.Int32
	hooks := monitorServiceHooks{syncDirectory: func(role, kind string, file *os.File) error {
		err := file.Sync()
		if role == policy.Role && kind == "checkpoint" {
			once.Do(func() { err = errors.Join(err, syscall.EIO) })
		}
		return err
	}, afterClose: func(role, kind string, file *os.File) error {
		if role == policy.Role && kind == "checkpoint" {
			if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
				return errors.New("provider close notification preceded descriptor join")
			}
			closed.Add(1)
		}
		return nil
	}, wait: func(ctx context.Context, role string, _ time.Duration) bool {
		if role != policy.Role {
			<-ctx.Done()
			return false
		}
		select {
		case <-resume:
			return true
		case <-ctx.Done():
			return false
		}
	}}
	args := fixture.args(url)
	ctx = monitorTestStorageContext(t, ctx, args)
	go func() {
		defer close(done)
		done <- runMainWithMonitorHooks(ctx, args, sink, nil, fixture.clock.now, hooks)
	}()
	t.Cleanup(func() { joinMonitorTestWorker(t, cancel, done) })
	event := monitorTestEvent(t, done, sink.events)
	if event.Current || event.CheckpointCurrent || event.State.Sequence != 1 {
		t.Fatal("uncertain first publication claimed completed readiness", event)
	}
	resumeMonitorTestWorker(t, done, resume)
	event = monitorTestEvent(t, done, sink.events)
	if !event.Current || !event.CheckpointCurrent || event.State.Sequence != 2 || closed.Load() != 1 {
		t.Fatal("provider lost acknowledgement did not join/reopen original custody", event, closed.Load())
	}
}
