package validator

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urnetwork/connect"
	"github.com/urnetwork/connect/protocol"
)

// The real registration owner can be exercised separately from the packet
// exchange, whose exact incoming-source boundary has its own regression below.
type tunnelTestClient struct {
	ctx   context.Context
	post  func(context.Context, connect.Id, []byte) ([]byte, error)
	close func(context.Context) error
	owned tunnelTransportClient
}

func (self *tunnelTestClient) postVerify(ctx context.Context, hop connect.Id, body []byte) ([]byte, error) {
	return self.post(ctx, hop, body)
}

func (self *tunnelTestClient) done() <-chan struct{} {
	if self.owned != nil {
		return self.owned.done()
	}
	return self.ctx.Done()
}

func (self *tunnelTestClient) closeAndWait(ctx context.Context) error {
	if self.owned != nil {
		return self.owned.closeAndWait(ctx)
	}
	return self.close(ctx)
}

// Each test joins the constructor-owned cleanup worker, including assertion
// failure paths. Handlers must observe the supplied request cancellation.
func newTestTunnelTransport(t *testing.T) *TunnelTransport {
	t.Helper()
	transport := NewTunnelTransport(t.Context(), nil, TunnelTransportConfig{SourceClientId: connect.NewId()})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := transport.CloseAndWait(ctx); err != nil {
			t.Error(err)
		}
	})
	return transport
}

// Two different hops use one actual processed registration. A rejected first
// exchange must not revoke the registered identity before the second hop.
func TestTunnelTransportReusesProcessedRegistrationAcrossHops(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	derivedId := connect.NewId()
	claims, err := json.Marshal(map[string]string{"client_id": derivedId.String()})
	if err != nil {
		t.Fatal(err)
	}
	token := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`)) + "." + base64.RawURLEncoding.EncodeToString(claims) + "."
	var authCalls, registrationCalls, removeCalls, wrongRemoval atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/network/auth-client":
			authCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]string{"by_client_jwt": token})
		case "/network/remove-client":
			var args connect.RemoveNetworkClientArgs
			if json.NewDecoder(r.Body).Decode(&args) != nil || args.ClientId != derivedId {
				wrongRemoval.Add(1)
			}
			removeCalls.Add(1)
			_, _ = io.WriteString(w, `{}`)
		case "/connect/control":
			var args connect.ConnectControlArgs
			if json.NewDecoder(r.Body).Decode(&args) != nil {
				http.Error(w, "invalid control", http.StatusBadRequest)
				return
			}
			raw, err := base64.StdEncoding.DecodeString(args.Pack)
			var pack protocol.Pack
			if err != nil || connect.ProtoUnmarshal(raw, &pack) != nil {
				http.Error(w, "invalid pack", http.StatusBadRequest)
				return
			}
			for _, frame := range pack.Frames {
				message, err := connect.FromFrame(frame)
				if err != nil {
					http.Error(w, "invalid frame", http.StatusBadRequest)
					return
				}
				if _, ok := message.(*protocol.ClientKey); ok {
					registrationCalls.Add(1)
				}
			}
			_, _ = io.WriteString(w, `{"pack":"","error":null}`)
		default:
			_, _ = io.WriteString(w, `{}`)
		}
	}))
	defer endpoint.Close()
	settings := connect.DefaultClientStrategySettings()
	settings.EnableResilient = false
	strategy := connect.NewClientStrategy(ctx, settings)
	defer strategy.Close()
	transport := NewTunnelTransport(ctx, strategy, TunnelTransportConfig{ApiUrl: endpoint.URL, ConnectUrl: endpoint.URL, ByClientJwt: func() string { return token }, SourceClientId: connect.NewId()})
	defer func() {
		if err := transport.CloseAndWait(context.Background()); err != nil {
			t.Error(err)
		}
	}()
	firstHop, secondHop := connect.NewId(), connect.NewId()
	applicationErr := errors.New("synthetic verify application rejection")
	var usedHops []connect.Id
	newClient := transport.newClient
	transport.newClient = func(callCtx context.Context, hop connect.Id) (tunnelTransportClient, error) {
		owned, err := newClient(callCtx, hop)
		if err != nil {
			return owned, err
		}
		registered := owned.(*registeredTunnelClient)
		return &tunnelTestClient{owned: owned, post: func(callCtx context.Context, actualHop connect.Id, body []byte) ([]byte, error) {
			if !registered.client.ClientKeyManager().Registered() || registered.client.ClientId() != derivedId {
				return nil, errors.New("unregistered or substituted client admitted")
			}
			usedHops = append(usedHops, actualHop)
			if actualHop == firstHop {
				return nil, applicationErr
			}
			return bytes.Clone(body), callCtx.Err()
		}}, nil
	}
	if _, err := transport.PostVerify(ctx, firstHop, []byte("first")); !errors.Is(err, applicationErr) {
		t.Fatalf("first exchange = %v", err)
	}
	if body, err := transport.PostVerify(ctx, secondHop, []byte("second")); err != nil || string(body) != "second" {
		t.Fatalf("second exchange = %q, %v", body, err)
	}
	if !reflect.DeepEqual(usedHops, []connect.Id{firstHop, secondHop}) || authCalls.Load() != 1 || registrationCalls.Load() != 1 || removeCalls.Load() != 0 {
		t.Fatalf("hops=%v auth=%d registration=%d premature removal=%d", usedHops, authCalls.Load(), registrationCalls.Load(), removeCalls.Load())
	}
	if err := transport.CloseAndWait(ctx); err != nil {
		t.Fatal(err)
	}
	if removeCalls.Load() == 0 || wrongRemoval.Load() != 0 {
		t.Fatalf("exact identity removal count=%d wrong=%d", removeCalls.Load(), wrongRemoval.Load())
	}
}

// A request cancellation ends only its packet exchange. The already admitted
// registration survives for the next caller with its own unchanged deadline.
func TestTunnelTransportRequestCancellationPreservesClient(t *testing.T) {
	transport := newTestTunnelTransport(t)
	entered := make(chan struct{})
	var creations, uses, retirements atomic.Int32
	client := &tunnelTestClient{ctx: t.Context(), close: func(context.Context) error { retirements.Add(1); return nil }}
	client.post = func(ctx context.Context, _ connect.Id, body []byte) ([]byte, error) {
		if uses.Add(1) == 1 {
			close(entered)
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return bytes.Clone(body), nil
	}
	transport.newClient = func(context.Context, connect.Id) (tunnelTransportClient, error) { creations.Add(1); return client, nil }
	firstCtx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { _, err := transport.PostVerify(firstCtx, connect.NewId(), nil); done <- err }()
	<-entered
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Minute)
	secondCtx, secondCancel := context.WithDeadline(t.Context(), deadline)
	defer secondCancel()
	client.post = func(ctx context.Context, _ connect.Id, body []byte) ([]byte, error) {
		if actual, ok := ctx.Deadline(); !ok || actual != deadline {
			return nil, errors.New("request deadline changed")
		}
		return body, nil
	}
	if _, err := transport.PostVerify(secondCtx, connect.NewId(), nil); err != nil {
		t.Fatal(err)
	}
	if creations.Load() != 1 || retirements.Load() != 0 {
		t.Fatalf("created=%d retired=%d", creations.Load(), retirements.Load())
	}
}

// An expired waiter cannot enter or cancel another caller's active lease.
func TestTunnelTransportLeaseDeadlineLeavesActiveRequestAlone(t *testing.T) {
	transport := newTestTunnelTransport(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var uses atomic.Int32
	client := &tunnelTestClient{ctx: t.Context(), close: func(context.Context) error { return nil }}
	client.post = func(ctx context.Context, _ connect.Id, _ []byte) ([]byte, error) {
		uses.Add(1)
		close(entered)
		select {
		case <-release:
			return nil, ctx.Err()
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	transport.newClient = func(context.Context, connect.Id) (tunnelTransportClient, error) { return client, nil }
	done := make(chan error, 1)
	go func() { _, err := transport.PostVerify(t.Context(), connect.NewId(), nil); done <- err }()
	<-entered
	expired, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancel()
	if _, err := transport.PostVerify(expired, connect.NewId(), nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expired lease = %v", err)
	}
	close(release)
	if err := <-done; err != nil || uses.Load() != 1 {
		t.Fatalf("active request = %v, uses=%d", err, uses.Load())
	}
}

// Partial registration is retired before the next factory call; it never
// reaches the packet exchange, even if it returned an allocated client owner.
func TestTunnelTransportFailedSetupNeverAdmitted(t *testing.T) {
	transport := newTestTunnelTransport(t)
	setupErr := errors.New("processed registration failed")
	var creations, failedUses, retired atomic.Int32
	failed := &tunnelTestClient{ctx: t.Context(), post: func(context.Context, connect.Id, []byte) ([]byte, error) { failedUses.Add(1); return nil, nil }, close: func(context.Context) error { retired.Add(1); return nil }}
	ready := &tunnelTestClient{ctx: t.Context(), post: func(context.Context, connect.Id, []byte) ([]byte, error) { return []byte("ready"), nil }, close: func(context.Context) error { return nil }}
	transport.newClient = func(context.Context, connect.Id) (tunnelTransportClient, error) {
		if creations.Add(1) == 1 {
			return failed, setupErr
		}
		if retired.Load() != 1 {
			return nil, errors.New("replacement overtook failed-client retirement")
		}
		return ready, nil
	}
	if _, err := transport.PostVerify(t.Context(), connect.NewId(), nil); !errors.Is(err, setupErr) {
		t.Fatal(err)
	}
	if body, err := transport.PostVerify(t.Context(), connect.NewId(), nil); err != nil || string(body) != "ready" {
		t.Fatalf("replacement = %q, %v", body, err)
	}
	if creations.Load() != 2 || failedUses.Load() != 0 {
		t.Fatalf("created=%d failed uses=%d", creations.Load(), failedUses.Load())
	}
}

// Unlike an application rejection, actual client cancellation must retire the
// failed identity before a later request creates its replacement.
func TestTunnelTransportEvictsFailedRegisteredClient(t *testing.T) {
	transport := newTestTunnelTransport(t)
	clientCtx, cancelClient := context.WithCancel(t.Context())
	defer cancelClient()
	var creations, retired atomic.Int32
	failed := &tunnelTestClient{ctx: clientCtx, close: func(context.Context) error { retired.Add(1); return nil }}
	failed.post = func(context.Context, connect.Id, []byte) ([]byte, error) {
		cancelClient()
		return nil, errors.New("underlying client failed")
	}
	ready := &tunnelTestClient{ctx: t.Context(), post: func(context.Context, connect.Id, []byte) ([]byte, error) { return []byte("replacement"), nil }, close: func(context.Context) error { return nil }}
	transport.newClient = func(context.Context, connect.Id) (tunnelTransportClient, error) {
		if creations.Add(1) == 1 {
			return failed, nil
		}
		if retired.Load() != 1 {
			return nil, errors.New("replacement overtook failed-client retirement")
		}
		return ready, nil
	}
	if _, err := transport.PostVerify(t.Context(), connect.NewId(), nil); err == nil {
		t.Fatal("underlying failure was hidden")
	}
	if body, err := transport.PostVerify(t.Context(), connect.NewId(), nil); err != nil || string(body) != "replacement" {
		t.Fatalf("replacement=%q err=%v", body, err)
	}
	if creations.Load() != 2 || retired.Load() != 1 {
		t.Fatalf("created=%d retired=%d", creations.Load(), retired.Load())
	}
}

// A successful factory response racing its expired setup caller cannot install
// a reusable entry or emit the request's first packet.
func TestTunnelTransportCanceledSetupCompletionIsRetired(t *testing.T) {
	transport := newTestTunnelTransport(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var uses, retired atomic.Int32
	client := &tunnelTestClient{ctx: t.Context(), post: func(context.Context, connect.Id, []byte) ([]byte, error) { uses.Add(1); return nil, nil }, close: func(context.Context) error { retired.Add(1); return nil }}
	transport.newClient = func(context.Context, connect.Id) (tunnelTransportClient, error) {
		cancel()
		return client, nil
	}
	if _, err := transport.PostVerify(ctx, connect.NewId(), nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if uses.Load() != 0 || retired.Load() != 1 {
		t.Fatalf("expired setup uses=%d retired=%d", uses.Load(), retired.Load())
	}
}

// An earlier bounded packet join is still owned by final shutdown; retirement
// of the reusable identity cannot hide that outstanding per-call worker.
func TestTunnelRegisteredClientJoinsRetainedPacketCleanup(t *testing.T) {
	pending := make(chan struct{})
	var packetJoins, identityJoins atomic.Int32
	client := &registeredTunnelClient{
		owner: &TunnelTransport{},
		packetCleanup: &tunnelAttempt{packetClient: tunnelAttemptCloseAndWaitFunc(func(ctx context.Context) error {
			packetJoins.Add(1)
			select {
			case <-pending:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})},
		cleanup: &tunnelAttempt{retireClient: func(context.Context) error { identityJoins.Add(1); return nil }},
	}
	expired, cancel := context.WithCancel(t.Context())
	cancel()
	if err := client.closeAndWait(expired); !errors.Is(err, context.Canceled) || client.packetCleanup == nil {
		t.Fatalf("incomplete packet cleanup was lost: %v", err)
	}
	close(pending)
	if err := client.closeAndWait(t.Context()); err != nil || client.packetCleanup != nil || packetJoins.Load() != 2 || identityJoins.Load() != 2 {
		t.Fatalf("retained packet cleanup=%v packet joins=%d identity joins=%d", err, packetJoins.Load(), identityJoins.Load())
	}
}

// Explicit entry/release barriers keep concurrent packet paths observable.
// No two callers may own the reusable client at the same time.
func TestTunnelTransportConcurrentRequestsHaveExclusiveOwnership(t *testing.T) {
	transport := newTestTunnelTransport(t)
	const count = 8
	entered, release := make(chan struct{}, count), make(chan struct{})
	var active, maximum, creations atomic.Int32
	client := &tunnelTestClient{ctx: t.Context(), close: func(context.Context) error { return nil }}
	client.post = func(ctx context.Context, _ connect.Id, _ []byte) ([]byte, error) {
		current := active.Add(1)
		defer active.Add(-1)
		for old := maximum.Load(); old < current && !maximum.CompareAndSwap(old, current); old = maximum.Load() {
		}
		entered <- struct{}{}
		select {
		case <-release:
			return nil, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	transport.newClient = func(context.Context, connect.Id) (tunnelTransportClient, error) { creations.Add(1); return client, nil }
	done := make(chan error, count)
	for range count {
		go func() { _, err := transport.PostVerify(t.Context(), connect.NewId(), nil); done <- err }()
	}
	for range count {
		<-entered
		release <- struct{}{}
	}
	for range count {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if maximum.Load() != 1 || creations.Load() != 1 {
		t.Fatalf("concurrent owners=%d registrations=%d", maximum.Load(), creations.Load())
	}
}

// Parent cancellation stops the current request, then joins actual identity
// cleanup. A canceled close waiter cannot advertise completion or abandon it.
func TestTunnelTransportParentShutdownJoinsRequestAndRetirement(t *testing.T) {
	parent, cancel := context.WithCancel(t.Context())
	defer cancel()
	transport := NewTunnelTransport(parent, nil, TunnelTransportConfig{SourceClientId: connect.NewId()})
	entered, useJoined, retirementEntered, retirementRelease := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	var retirementCalls atomic.Int32
	client := &tunnelTestClient{ctx: t.Context()}
	client.post = func(ctx context.Context, _ connect.Id, _ []byte) ([]byte, error) {
		close(entered)
		<-ctx.Done()
		close(useJoined)
		return nil, ctx.Err()
	}
	client.close = func(ctx context.Context) error {
		retirementCalls.Add(1)
		select {
		case <-useJoined:
		default:
			return errors.New("identity retired before its packet use joined")
		}
		close(retirementEntered)
		select {
		case <-retirementRelease:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	transport.newClient = func(context.Context, connect.Id) (tunnelTransportClient, error) { return client, nil }
	done := make(chan error, 1)
	go func() { _, err := transport.PostVerify(t.Context(), connect.NewId(), nil); done <- err }()
	<-entered
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	<-retirementEntered
	waitCtx, waitCancel := context.WithCancel(t.Context())
	waitCancel()
	if err := transport.CloseAndWait(waitCtx); !errors.Is(err, context.Canceled) {
		t.Fatalf("unfinished shutdown = %v", err)
	}
	close(retirementRelease)
	if err := transport.CloseAndWait(t.Context()); err != nil || retirementCalls.Load() != 1 {
		t.Fatalf("joined shutdown=%v retirements=%d", err, retirementCalls.Load())
	}
	if _, err := transport.PostVerify(t.Context(), connect.NewId(), nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("closed owner admitted demand: %v", err)
	}
}

// A delayed packet from another hop, or a closed request, cannot enter the
// next per-call netstack even though both requests share one client identity.
func TestTunnelPacketReceiverRejectsPreviousHopAndCanceledRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	hop := connect.NewId()
	var output bytes.Buffer
	receive := newTunnelPacketReceiver(ctx, hop, &output)
	receive(connect.SourceId(connect.NewId()), protocol.ProvideMode_Network, nil, []byte("wrong"))
	receive(connect.SourceId(hop), protocol.ProvideMode_Network, nil, []byte("expected"))
	cancel()
	receive(connect.SourceId(hop), protocol.ProvideMode_Network, nil, []byte("late"))
	if output.String() != "expected" {
		t.Fatalf("cross-request packet reached netstack: %q", output.String())
	}
}
