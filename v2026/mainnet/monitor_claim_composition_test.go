// Real public monitor workers retain role ownership while synthetic HTTP
// sources advance. All cross-goroutine changes follow explicit sample barriers.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/protocol"
)

// A timeout bounds a failed control; it never orders production work.
func (self *monitorClaimTestSink) next(t *testing.T, done <-chan struct{}) monitorClaimTestEvent {
	t.Helper()
	select {
	case event := <-self.events:
		return event
	case <-done:
		t.Fatal("public claim monitor exited before the expected sample")
	case <-time.After(10 * time.Second):
		t.Fatal("public claim sample did not complete")
	}
	return monitorClaimTestEvent{}
}

func TestMonitorClaimPublicUnsignedLeafAdvancesWithoutPayment(t *testing.T) {
	fixture := newMonitorServicesFixture(t)
	initial := fixture.clock.now()
	value := monitorClaimTestPending(monitorClaimTestValue(initial))
	var claimed atomic.Bool
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		candidate := cloneMonitorClaimProgress(&value)
		if claimed.Load() {
			candidate.Sequence = 2
			candidate.PublishedAt = fixture.clock.now().Format(time.RFC3339Nano)
			candidate.Entries[0].Observation.ObservedAt = candidate.PublishedAt
			*candidate.Entries[0].Observation.LeafClaimed = true
			candidate.Entries[0].QueueStatus = "finalized"
			candidate.FinalizedEntries, candidate.UnresolvedEntries = 1, 0
			candidate.OldestUnresolvedEpoch = nil
		}
		_ = json.NewEncoder(w).Encode(candidate)
	}))
	defer source.Close()
	policy := monitorClaimTestPolicy(t, value, source.URL+"/claim-progress", initial)
	fixture.policy.Claims = []monitorClaimPolicy{policy}
	fixture.writePolicy(t)
	url, _, _ := monitorServicesBlockedChain(t)
	ctx, cancel := context.WithCancel(t.Context())
	sink := &monitorClaimTestSink{events: make(chan monitorClaimTestEvent, 4)}
	resume := make(chan struct{}, 1)
	var diagnostic bytes.Buffer
	hooks := monitorServiceHooks{wait: func(ctx context.Context, role string, _ time.Duration) bool {
		if role != policy.Role {
			<-ctx.Done()
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-resume:
			return true
		}
	}}
	done := make(chan struct{})
	var exit int
	go func() {
		defer close(done)
		exit = runMonitorStorageTestWithHooks(t, ctx, fixture.args(url), sink, &diagnostic, fixture.clock.now, hooks)
	}()
	t.Cleanup(func() { cancel(); <-done })
	first := sink.next(t, done)
	if !first.Current || first.State.MerkleProofs != 1 || first.State.ClaimedLeaves != 0 || first.State.Overdue != 1 || first.State.ProgressAt != initial {
		t.Fatal("initial real worker did not retain unresolved proof", first)
	}
	fixture.clock.seconds.Add(1)
	claimed.Store(true)
	resume <- struct{}{}
	second := sink.next(t, done)
	if !second.Current || second.Status != "ok" || second.State.MerkleProofs != 1 || second.State.ClaimedLeaves != 1 || second.State.AcceptedReceipts != 0 || second.State.Deferred != 0 || second.State.Overdue != 0 || second.State.ProgressAt != fixture.clock.now() {
		t.Fatal("same-root unsigned acceptance was hidden or became paid receipt", second)
	}
	cancel()
	<-done
	if exit != 0 {
		t.Fatal("joined unsigned observer failed", exit, diagnostic.String())
	}
	checkpoint, metrics := monitorClaimPaths(fixture.checkpointPath, fixture.metricsPath, policy.Role)
	raw, err := os.ReadFile(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	var record monitorClaimCheckpointRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatal(err)
	}
	epoch := record.State.Epochs[0]
	if epoch.Proof == nil || epoch.Proof.LeafClaimed == nil || *epoch.Proof.LeafClaimed || epoch.Observation == nil || epoch.Observation.LeafClaimed == nil || !*epoch.Observation.LeafClaimed || epoch.Observation.PaymentStatus != "unknown" {
		t.Fatal("original proof or later accepted leaf was rewritten", epoch)
	}
	raw, err = os.ReadFile(metrics)
	if err != nil || !bytes.Contains(raw, []byte("per_epoch_payment_known{role=\"claim-a\"} 0")) {
		t.Fatal("unsigned acceptance acquired payment authority", err, string(raw))
	}
}

// Both roles reach real durable publications. A soft read outage recovers on
// the same owner; subsequent hard loss retires only the affected role.
func monitorClaimPublicPeerControl(t *testing.T, loseCheckpoint bool) {
	t.Helper()
	fixture := newMonitorServicesFixture(t)
	value := monitorClaimTestValue(fixture.clock.now())
	var failing atomic.Bool
	failing.Store(true)
	var foreign atomic.Bool
	var requestsA, requestsB atomic.Uint64
	sourceA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestsA.Add(1)
		if failing.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		candidate := cloneMonitorClaimProgress(&value)
		candidate.Sequence = requestsA.Load()
		if foreign.Load() {
			candidate.Member = "unapproved-member"
		}
		_ = json.NewEncoder(w).Encode(candidate)
	}))
	defer sourceA.Close()
	peer := cloneMonitorClaimProgress(&value)
	peer.Member, peer.DeclaredPool.NoId = "peer", "8"
	peer.Entries[0].Observation.Pool = *peer.DeclaredPool
	sourceB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		candidate := cloneMonitorClaimProgress(peer)
		candidate.Sequence = requestsB.Add(1)
		_ = json.NewEncoder(w).Encode(candidate)
	}))
	defer sourceB.Close()
	one := monitorClaimTestPolicy(t, value, sourceA.URL+"/claim-progress", fixture.clock.now())
	two := monitorClaimTestPolicy(t, *peer, sourceB.URL+"/claim-progress", fixture.clock.now())
	two.Role = "claim-b"
	fixture.policy.Claims = []monitorClaimPolicy{one, two}
	fixture.writePolicy(t)
	url, _, _ := monitorServicesBlockedChain(t)
	ctx, cancel := context.WithCancel(t.Context())
	sink := &monitorClaimTestSink{events: make(chan monitorClaimTestEvent, 8)}
	resume := map[string]chan struct{}{one.Role: make(chan struct{}, 1), two.Role: make(chan struct{}, 1)}
	retired := make(chan string, 2)
	// Exhaust only the actual transient sample at its owned wait boundary.
	hooks := monitorServiceHooks{
		rpcWait: func(context.Context, string, time.Duration) error { return context.DeadlineExceeded },
		wait: func(ctx context.Context, role string, _ time.Duration) bool {
			select {
			case <-ctx.Done():
				return false
			case <-resume[role]:
				return true
			}
		},
		afterWorker: func(role string, _ int) {
			if role == one.Role {
				retired <- role
			}
		},
	}
	var diagnostic bytes.Buffer
	var exit int
	done := make(chan struct{})
	go func() {
		defer close(done)
		exit = runMonitorStorageTestWithHooks(t, ctx, fixture.args(url), sink, &diagnostic, fixture.clock.now, hooks)
	}()
	t.Cleanup(func() { cancel(); <-done })
	observed := map[string]monitorClaimTestEvent{}
	for range 2 {
		event := sink.next(t, done)
		observed[event.Role] = event
	}
	if observed[one.Role].Status != "unavailable" || observed[one.Role].Current || !observed[two.Role].Current {
		t.Fatal("outage stopped independently expected peer", observed)
	}
	peerPath, _ := monitorClaimPaths(fixture.checkpointPath, fixture.metricsPath, two.Role)
	before, err := os.ReadFile(peerPath)
	if err != nil {
		t.Fatal(err)
	}
	failing.Store(false)
	resume[one.Role] <- struct{}{}
	recovered := sink.next(t, done)
	if recovered.Role != one.Role || !recovered.Current || recovered.State.AcceptedReceipts != 1 {
		t.Fatal("soft outage lost owner continuation", recovered)
	}
	after, err := os.ReadFile(peerPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("affected recovery reset healthy peer checkpoint", err)
	}
	if loseCheckpoint {
		path, _ := monitorClaimPaths(fixture.checkpointPath, fixture.metricsPath, one.Role)
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	} else {
		foreign.Store(true)
	}
	resume[one.Role] <- struct{}{}
	lost := sink.next(t, done)
	if lost.Role != one.Role || lost.Current || loseCheckpoint && lost.CheckpointCurrent || !loseCheckpoint && lost.Status != "identity" {
		t.Fatal("hard loss remained a current claim sample", lost)
	}
	select {
	case <-retired:
	case <-done:
		t.Fatal("affected retirement stopped all roles")
	case <-time.After(10 * time.Second):
		t.Fatal("affected role did not close")
	}
	resume[two.Role] <- struct{}{}
	live := sink.next(t, done)
	if live.Role != two.Role || !live.Current || live.State.Sequence != 2 {
		t.Fatal("healthy peer did not keep its original lifetime", live)
	}
	cancel()
	<-done
	if exit != 3 {
		t.Fatal("hard affected-role loss was omitted from joined result", exit, diagnostic.String())
	}
}

func TestMonitorClaimPublicOutageAndForeignIdentityPreservePeer(t *testing.T) {
	monitorClaimPublicPeerControl(t, false)
}
func TestMonitorClaimPublicCompletedCheckpointLossPreservesPeer(t *testing.T) {
	monitorClaimPublicPeerControl(t, true)
}

func TestMonitorClaimIdentityAndAuthDominateBodyCloseOutage(t *testing.T) {
	value := monitorClaimTestValue(time.Now().UTC())
	policy := monitorClaimTestPolicy(t, value, "https://synthetic.invalid/claim-progress", time.Now().UTC())
	value.Member = "foreign"
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	body := &monitorProviderTestBody{Reader: bytes.NewReader(raw)}
	client := &http.Client{Transport: monitorProviderTestRoundTripper{response: &http.Response{StatusCode: 200, Body: body}}}
	if _, code := readMonitorClaim(t.Context(), client, policy); code != "identity" || body.closes.Load() != 1 {
		t.Fatal("observed identity lost to close outage", code, body.closes.Load())
	}
	body = &monitorProviderTestBody{Reader: bytes.NewReader(nil)}
	client.Transport = monitorProviderTestRoundTripper{response: &http.Response{StatusCode: 403, Body: body}}
	if _, code := readMonitorClaim(t.Context(), client, policy); code != "authentication" || body.closes.Load() != 1 {
		t.Fatal("auth rejection lost to close outage", code, body.closes.Load())
	}
}

func TestMonitorClaimBodyCancellationAndBoundsJoin(t *testing.T) {
	entered, joined := make(chan struct{}), make(chan struct{})
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"schema":`)
		w.(http.Flusher).Flush()
		close(entered)
		<-r.Context().Done()
		close(joined)
	}))
	defer source.Close()
	policy := monitorClaimTestPolicy(t, monitorClaimTestValue(time.Now().UTC()), source.URL+"/claim-progress", time.Now().UTC())
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan string, 1)
	client := newMonitorProviderClient()
	defer client.CloseIdleConnections()
	go func() { _, code := readMonitorClaim(ctx, client, policy); done <- code }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("partial body did not start")
	}
	cancel()
	select {
	case code := <-done:
		if code != "unavailable" {
			t.Fatal(code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("claim read did not join cancellation")
	}
	select {
	case <-joined:
	case <-time.After(5 * time.Second):
		t.Fatal("claim body remained active")
	}
	oversized := &http.Client{Transport: monitorProviderTestRoundTripper{response: &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", protocol.MaxClaimProgressBytes+1)))}}}
	if _, code := readMonitorClaim(t.Context(), oversized, policy); code != "invalid" {
		t.Fatal("oversized claim body admitted", code)
	}
}

func TestMonitorClaimPolicyRequiresIndependentBoundedExpectations(t *testing.T) {
	fixture := newMonitorServicesFixture(t)
	value := monitorClaimTestValue(fixture.clock.now())
	base := monitorClaimTestPolicy(t, value, "https://synthetic.invalid/claim-progress", fixture.clock.now())
	fixture.policy.Claims = []monitorClaimPolicy{base}
	fixture.writePolicy(t)
	if loaded, err := loadMonitorServices(t.Context(), fixture.policyPath, monitorTestExpectation(), fixture.checkpointPath, fixture.metricsPath); err != nil || loaded == nil || len(loaded.Claims) != 1 {
		t.Fatal("independently specified expected pool was refused", err)
	}
	for _, change := range []func(*monitorClaimPolicy){
		func(p *monitorClaimPolicy) { p.ExpectedMember = "" },
		func(p *monitorClaimPolicy) { p.ExpectedPool = protocol.ClaimProgressPool{} },
		func(p *monitorClaimPolicy) { p.ExpectedPool.ChainId = 945 },
		func(p *monitorClaimPolicy) { p.Endpoint = "https://user:secret@synthetic.invalid/claim-progress" },
		func(p *monitorClaimPolicy) { p.Endpoint += "?id=candidate" },
		func(p *monitorClaimPolicy) { p.Endpoint = "http://synthetic.invalid/claim-progress" },
		func(p *monitorClaimPolicy) { p.FreshnessSeconds = 301 },
		func(p *monitorClaimPolicy) { p.Epochs = nil },
		func(p *monitorClaimPolicy) { p.Epochs = append(p.Epochs, p.Epochs[0]) },
		func(p *monitorClaimPolicy) {
			for len(p.Epochs) <= maxMonitorClaimEpochs {
				next := p.Epochs[0]
				next.Epoch += int64(len(p.Epochs))
				p.Epochs = append(p.Epochs, next)
			}
		},
	} {
		candidate := base
		candidate.Epochs = append([]monitorClaimEpochPolicy(nil), base.Epochs...)
		change(&candidate)
		fixture.policy.Claims = []monitorClaimPolicy{candidate}
		fixture.writePolicy(t)
		if loaded, err := loadMonitorServices(t.Context(), fixture.policyPath, monitorTestExpectation(), fixture.checkpointPath, fixture.metricsPath); err == nil || loaded != nil {
			t.Fatal("implicit, unsafe or unbounded expectation admitted", candidate)
		}
	}
	duplicate := base
	duplicate.Role = "claim-b"
	fixture.policy.Claims = []monitorClaimPolicy{base, duplicate}
	fixture.writePolicy(t)
	if _, err := loadMonitorServices(t.Context(), fixture.policyPath, monitorTestExpectation(), fixture.checkpointPath, fixture.metricsPath); err == nil {
		t.Fatal("same expected pool became two independent observers")
	}
}

func TestMonitorClaimPublicLostAckReopensExactCheckpoint(t *testing.T) {
	fixture := newMonitorServicesFixture(t)
	value := monitorClaimTestValue(fixture.clock.now())
	var requests atomic.Uint64
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		candidate := cloneMonitorClaimProgress(&value)
		candidate.Sequence = requests.Add(1)
		_ = json.NewEncoder(w).Encode(candidate)
	}))
	defer source.Close()
	policy := monitorClaimTestPolicy(t, value, source.URL+"/claim-progress", fixture.clock.now())
	fixture.policy.Claims = []monitorClaimPolicy{policy}
	fixture.writePolicy(t)
	url, _, _ := monitorServicesBlockedChain(t)
	ctx, cancel := context.WithCancel(t.Context())
	resume, closed := make(chan struct{}, 1), make(chan struct{}, 2)
	var failed atomic.Bool
	hooks := monitorServiceHooks{
		syncDirectory: func(role, kind string, file *os.File) error {
			err := file.Sync()
			if role == policy.Role && kind == "checkpoint" && failed.CompareAndSwap(false, true) {
				return errors.Join(err, syscall.EIO)
			}
			return err
		},
		afterClose: func(role, kind string, file *os.File) error {
			if role == policy.Role && kind == "checkpoint" {
				if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
					return errors.New("claim owner not closed before recovery")
				}
				closed <- struct{}{}
			}
			return nil
		},
		wait: func(ctx context.Context, role string, _ time.Duration) bool {
			if role != policy.Role {
				<-ctx.Done()
				return false
			}
			select {
			case <-ctx.Done():
				return false
			case <-resume:
				return true
			}
		},
	}
	sink := &monitorClaimTestSink{events: make(chan monitorClaimTestEvent, 4)}
	var diagnostic bytes.Buffer
	var exit int
	done := make(chan struct{})
	go func() {
		defer close(done)
		exit = runMonitorStorageTestWithHooks(t, ctx, fixture.args(url), sink, &diagnostic, fixture.clock.now, hooks)
	}()
	t.Cleanup(func() { cancel(); <-done })
	first := sink.next(t, done)
	if first.Current || first.CheckpointCurrent || first.State.AcceptedReceipts != 1 {
		t.Fatal("uncertain save became acknowledged current history", first)
	}
	select {
	case <-closed:
	case <-time.After(10 * time.Second):
		t.Fatal("uncertain owner did not close")
	}
	checkpoint, _ := monitorClaimPaths(fixture.checkpointPath, fixture.metricsPath, policy.Role)
	raw, err := os.ReadFile(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	var original monitorClaimCheckpointRecord
	if err := json.Unmarshal(raw, &original); err != nil || original.State.Record == nil || original.State.Record.Sequence != 1 || original.State.Epochs[0].Observation.AcceptedAmountRao != "25" {
		t.Fatal("lost acknowledgement lost actual completed bytes", err, original)
	}
	resume <- struct{}{}
	second := sink.next(t, done)
	if !second.Current || !second.CheckpointCurrent || second.State.Sequence != 2 || second.State.AcceptedReceipts != 1 || second.State.ProgressAt != first.State.ProgressAt {
		t.Fatal("reconciliation reset original semantic progress", second)
	}
	cancel()
	<-done
	if exit != 0 {
		t.Fatal("joined claim recovery failed", exit, diagnostic.String())
	}
}
