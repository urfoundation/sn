package miner

// Control regressions force ownership transitions with barriers, including
// caller cancellation while the swarm continues the admitted operation.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/urnetwork/connect/v2026"
)

// Exposes the exact point at which a handler subscribes to caller cancellation.
type swarmControlWaitContext struct {
	context.Context
	waiting chan struct{}
	once    sync.Once
}

// The admission check uses Err; Done is first read when the operation is owned.
func (self *swarmControlWaitContext) Done() <-chan struct{} {
	self.once.Do(func() { close(self.waiting) })
	return self.Context.Done()
}

// Allows a fake subscription to force a real instance close to remain pending.
type swarmControlTestSub struct {
	close func()
}

// Runs the test's explicit close boundary.
func (self *swarmControlTestSub) Close() {
	self.close()
}

// Provides one releaseable boundary; cleanup releases it even after a failure.
type swarmControlTestBarrier struct {
	entered     chan struct{}
	released    chan struct{}
	enterOnce   sync.Once
	releaseOnce sync.Once
}

// Registers release after the swarm's cleanup, so cleanup never strands work.
func newSwarmControlTestBarrier(t *testing.T) *swarmControlTestBarrier {
	t.Helper()
	barrier := &swarmControlTestBarrier{entered: make(chan struct{}), released: make(chan struct{})}
	t.Cleanup(barrier.release)
	return barrier
}

// Announces entry before waiting for an explicit release.
func (self *swarmControlTestBarrier) block() {
	self.enterOnce.Do(func() { close(self.entered) })
	<-self.released
}

// Releases once, including repeated explicit and cleanup releases.
func (self *swarmControlTestBarrier) release() {
	self.releaseOnce.Do(func() { close(self.released) })
}

// Builds only synthetic member state and gives its operations a joined owner.
func newSwarmControlTestSwarm(t *testing.T) *ProviderSwarm {
	t.Helper()
	config := validProviderSwarmConfig(t)
	swarm, err := NewProviderSwarm(&config)
	if err != nil {
		t.Fatal(err)
	}
	swarm.runCtx, swarm.runCancel = context.WithCancel(context.Background())
	swarm.terminalErrors = make(chan error, 1)
	swarm.disabled["miner-1"] = true
	t.Cleanup(swarm.stopMembers)
	return swarm
}

// Waits for positive evidence; the timeout only diagnoses a missing transition.
func waitSwarmControlTestSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatal("control did not reach its expected boundary")
	}
}

// Launches an in-process HTTP handler without introducing client timeout races.
func startSwarmControlTestRequest(swarm *ProviderSwarm, ctx context.Context, action string) <-chan *httptest.ResponseRecorder {
	result := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/control/miner-1/"+action, nil).WithContext(ctx)
		swarm.ServeHTTP(response, request)
		result <- response
	}()
	return result
}

// Checks the HTTP result after the forced operation boundary has been released.
func waitSwarmControlTestResponse(t *testing.T, result <-chan *httptest.ResponseRecorder, code int) *httptest.ResponseRecorder {
	t.Helper()
	select {
	case response := <-result:
		if response.Code != code {
			t.Fatalf("control status = %d, want %d: %s", response.Code, code, response.Body.String())
		}
		return response
	case <-time.After(5 * time.Second):
		t.Fatal("control request did not return")
		return nil
	}
}

// Reads the wire contract rather than treating whole-swarm readiness as state.
func readSwarmControlTestStatus(t *testing.T, swarm *ProviderSwarm, id, state string) providerSwarmMemberStatus {
	t.Helper()
	response := httptest.NewRecorder()
	swarm.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/control/"+id+"/status", nil))
	var status providerSwarmMemberStatus
	if err := json.Unmarshal(response.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || status.Schema != providerSwarmMemberSchema || status.Id != id || status.State != state {
		t.Fatalf("member status = %d %+v, want %s", response.Code, status, state)
	}
	return status
}

// Teardown used to remove its slot before close, allowing a replacement to
// start while the old identity still owned its resources.
func TestProviderSwarmControlEnableWaitsForTeardown(t *testing.T) {
	swarm := newSwarmControlTestSwarm(t)
	teardown := newSwarmControlTestBarrier(t)
	var starts atomic.Int32
	swarm.startMember = func(context.Context, ProviderSwarmMember, func(error)) (*providerSwarmInstance, error) {
		instance := &providerSwarmInstance{}
		if starts.Add(1) == 1 {
			instance.refreshSub = &swarmControlTestSub{close: teardown.block}
		} else {
			select {
			case <-teardown.released:
			default:
				t.Error("replacement started before teardown released the member")
			}
		}
		return instance, nil
	}
	waitSwarmControlTestResponse(t, startSwarmControlTestRequest(swarm, context.Background(), "enable"), http.StatusOK)
	disabled := startSwarmControlTestRequest(swarm, context.Background(), "disable")
	waitSwarmControlTestSignal(t, teardown.entered)
	readSwarmControlTestStatus(t, swarm, "miner-1", "stopping")
	waitCtx := &swarmControlWaitContext{Context: context.Background(), waiting: make(chan struct{})}
	enabled := startSwarmControlTestRequest(swarm, waitCtx, "enable")
	waitSwarmControlTestSignal(t, waitCtx.waiting)
	if starts.Load() != 1 {
		t.Fatalf("pending teardown admitted %d starts", starts.Load())
	}
	teardown.release()
	waitSwarmControlTestResponse(t, disabled, http.StatusOK)
	waitSwarmControlTestResponse(t, enabled, http.StatusOK)
	readSwarmControlTestStatus(t, swarm, "miner-1", "running")
	if starts.Load() != 2 {
		t.Fatalf("restore started %d members", starts.Load())
	}
}

// Both duplicate and opposite callers must be able to leave a blocked close.
// A timed-out enable never queues a replacement after that waiter has left.
func TestProviderSwarmControlCanceledWaitersDoNotOutliveTeardown(t *testing.T) {
	swarm := newSwarmControlTestSwarm(t)
	teardown := newSwarmControlTestBarrier(t)
	var starts atomic.Int32
	swarm.startMember = func(context.Context, ProviderSwarmMember, func(error)) (*providerSwarmInstance, error) {
		starts.Add(1)
		return &providerSwarmInstance{refreshSub: &swarmControlTestSub{close: teardown.block}}, nil
	}
	waitSwarmControlTestResponse(t, startSwarmControlTestRequest(swarm, context.Background(), "enable"), http.StatusOK)
	disabled := startSwarmControlTestRequest(swarm, context.Background(), "disable")
	waitSwarmControlTestSignal(t, teardown.entered)
	for _, action := range []string{"disable", "enable"} {
		ctx, cancel := context.WithCancel(context.Background())
		waitCtx := &swarmControlWaitContext{Context: ctx, waiting: make(chan struct{})}
		result := startSwarmControlTestRequest(swarm, waitCtx, action)
		waitSwarmControlTestSignal(t, waitCtx.waiting)
		cancel()
		waitSwarmControlTestResponse(t, result, http.StatusServiceUnavailable)
		readSwarmControlTestStatus(t, swarm, "miner-1", "stopping")
	}
	teardown.release()
	waitSwarmControlTestResponse(t, disabled, http.StatusOK)
	readSwarmControlTestStatus(t, swarm, "miner-1", "disabled")
	if starts.Load() != 1 {
		t.Fatalf("canceled enable admitted %d starts", starts.Load())
	}
}

// The accepted enable survives its HTTP caller, while duplicate requests share
// its result and opposite requests remain cancelable during wallet setup.
func TestProviderSwarmControlStartupSurvivesCallerCancellation(t *testing.T) {
	swarm := newSwarmControlTestSwarm(t)
	startup := newSwarmControlTestBarrier(t)
	memberContexts := make(chan context.Context, 1)
	var starts atomic.Int32
	swarm.startMember = func(ctx context.Context, _ ProviderSwarmMember, _ func(error)) (*providerSwarmInstance, error) {
		starts.Add(1)
		memberContexts <- ctx
		startup.block()
		return &providerSwarmInstance{}, nil
	}
	ownerCtx, cancelOwner := context.WithCancel(context.Background())
	t.Cleanup(cancelOwner)
	owner := startSwarmControlTestRequest(swarm, ownerCtx, "enable")
	waitSwarmControlTestSignal(t, startup.entered)
	memberCtx := <-memberContexts
	readSwarmControlTestStatus(t, swarm, "miner-1", "starting")
	cancelOwner()
	waitSwarmControlTestResponse(t, owner, http.StatusServiceUnavailable)
	if err := memberCtx.Err(); err != nil {
		t.Fatalf("HTTP cancellation canceled owned startup: %v", err)
	}
	for _, action := range []string{"enable", "disable"} {
		ctx, cancel := context.WithCancel(context.Background())
		waitCtx := &swarmControlWaitContext{Context: ctx, waiting: make(chan struct{})}
		result := startSwarmControlTestRequest(swarm, waitCtx, action)
		waitSwarmControlTestSignal(t, waitCtx.waiting)
		cancel()
		waitSwarmControlTestResponse(t, result, http.StatusServiceUnavailable)
	}
	duplicateCtx := &swarmControlWaitContext{Context: context.Background(), waiting: make(chan struct{})}
	duplicate := startSwarmControlTestRequest(swarm, duplicateCtx, "enable")
	waitSwarmControlTestSignal(t, duplicateCtx.waiting)
	startup.release()
	response := waitSwarmControlTestResponse(t, duplicate, http.StatusOK)
	var legacyStatus providerSwarmStatus
	if err := json.Unmarshal(response.Body.Bytes(), &legacyStatus); err != nil || legacyStatus.Schema != ProviderSwarmSchema {
		t.Fatalf("successful POST lost the whole-swarm body: %s, %v", response.Body.String(), err)
	}
	readSwarmControlTestStatus(t, swarm, "miner-1", "running")
	if starts.Load() != 1 || memberCtx.Err() != nil {
		t.Fatalf("duplicate enable replaced its pending startup: starts=%d err=%v", starts.Load(), memberCtx.Err())
	}
}

// An opposite disable joins startup and then closes exactly that new instance.
func TestProviderSwarmControlDisableWaitsForStartup(t *testing.T) {
	swarm := newSwarmControlTestSwarm(t)
	startup := newSwarmControlTestBarrier(t)
	var closes atomic.Int32
	swarm.startMember = func(context.Context, ProviderSwarmMember, func(error)) (*providerSwarmInstance, error) {
		startup.block()
		return &providerSwarmInstance{cancel: func() { closes.Add(1) }}, nil
	}
	enabled := startSwarmControlTestRequest(swarm, context.Background(), "enable")
	waitSwarmControlTestSignal(t, startup.entered)
	waitCtx := &swarmControlWaitContext{Context: context.Background(), waiting: make(chan struct{})}
	disabled := startSwarmControlTestRequest(swarm, waitCtx, "disable")
	waitSwarmControlTestSignal(t, waitCtx.waiting)
	readSwarmControlTestStatus(t, swarm, "miner-1", "starting")
	startup.release()
	waitSwarmControlTestResponse(t, enabled, http.StatusOK)
	waitSwarmControlTestResponse(t, disabled, http.StatusOK)
	readSwarmControlTestStatus(t, swarm, "miner-1", "disabled")
	if closes.Load() != 1 {
		t.Fatalf("opposite operation closed %d instances", closes.Load())
	}
}

// A request already canceled at admission has no operation or callback owner.
func TestProviderSwarmControlRejectsCanceledRequestBeforeMutation(t *testing.T) {
	swarm := newSwarmControlTestSwarm(t)
	var starts atomic.Int32
	swarm.startMember = func(context.Context, ProviderSwarmMember, func(error)) (*providerSwarmInstance, error) {
		starts.Add(1)
		return &providerSwarmInstance{}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	waitSwarmControlTestResponse(t, startSwarmControlTestRequest(swarm, ctx, "enable"), http.StatusServiceUnavailable)
	readSwarmControlTestStatus(t, swarm, "miner-1", "disabled")
	if starts.Load() != 0 {
		t.Fatal("canceled request started a member")
	}
}

// Callbacks are invoked explicitly after replacement and during teardown,
// reproducing the old identity-only callback's ability to kill a newer member.
func TestProviderSwarmControlRejectsStaleGenerationCallbacks(t *testing.T) {
	swarm := newSwarmControlTestSwarm(t)
	var callbacks []func(error)
	swarm.startMember = func(_ context.Context, _ ProviderSwarmMember, failed func(error)) (*providerSwarmInstance, error) {
		callbacks = append(callbacks, failed)
		return &providerSwarmInstance{refreshSub: &swarmControlTestSub{close: func() {
			failed(errors.New("retired callback during close"))
		}}}, nil
	}
	waitSwarmControlTestResponse(t, startSwarmControlTestRequest(swarm, context.Background(), "enable"), http.StatusOK)
	waitSwarmControlTestResponse(t, startSwarmControlTestRequest(swarm, context.Background(), "disable"), http.StatusOK)
	waitSwarmControlTestResponse(t, startSwarmControlTestRequest(swarm, context.Background(), "enable"), http.StatusOK)
	callbacks[0](errors.New("retired authentication callback"))
	readSwarmControlTestStatus(t, swarm, "miner-1", "running")
	if swarm.runCtx.Err() != nil {
		t.Fatal("retired callback stopped the replacement swarm")
	}
	callbacks[1](errors.New("current authentication callback"))
	status := readSwarmControlTestStatus(t, swarm, "miner-1", "failed")
	if status.Failure != "current authentication callback" || !errors.Is(swarm.runCtx.Err(), context.Canceled) {
		t.Fatalf("current generation did not terminate: %+v, %v", status, swarm.runCtx.Err())
	}
}

// Lifecycle GET must not ask even its own instance for readiness, and reading
// a different member must never wait for another carrier to finish connecting.
func TestProviderSwarmControlStatusDoesNotConsultCarrierReadiness(t *testing.T) {
	swarm := newSwarmControlTestSwarm(t)
	swarm.startMember = func(context.Context, ProviderSwarmMember, func(error)) (*providerSwarmInstance, error) {
		return &providerSwarmInstance{}, nil
	}
	if err := swarm.controlMember(context.Background(), "miner-1", true); err != nil {
		t.Fatal(err)
	}
	swarm.instances["miner-1"].connectedOverride = func() bool {
		t.Error("member lifecycle GET consulted its carrier")
		return false
	}
	swarm.members["miner-2"] = ProviderSwarmMember{ID: "miner-2"}
	swarm.instances["miner-2"] = &providerSwarmInstance{connectedOverride: func() bool {
		t.Error("member lifecycle GET consulted an unrelated carrier")
		return false
	}}
	readSwarmControlTestStatus(t, swarm, "miner-1", "running")
	readSwarmControlTestStatus(t, swarm, "miner-2", "running")
	unknown := httptest.NewRecorder()
	swarm.ServeHTTP(unknown, httptest.NewRequest(http.MethodGet, "/control/missing/status", nil))
	if unknown.Code != http.StatusNotFound {
		t.Fatalf("unknown member GET = %d", unknown.Code)
	}
}

// A typed startup callback must fail its operation and close a returned
// instance, without converting a temporary startup failure into swarm death.
func TestProviderSwarmControlStartupCallbackFailureIsRetryable(t *testing.T) {
	swarm := newSwarmControlTestSwarm(t)
	var starts, closes atomic.Int32
	var oldCallback func(error)
	swarm.startMember = func(ctx context.Context, _ ProviderSwarmMember, failed func(error)) (*providerSwarmInstance, error) {
		if starts.Add(1) == 1 {
			oldCallback = failed
			failed(&url.Error{Op: "Post", URL: "https://api.example/sn/wallet", Err: context.DeadlineExceeded})
			if ctx.Err() == nil {
				t.Error("startup failure did not cancel its own work")
			}
		}
		return &providerSwarmInstance{cancel: func() { closes.Add(1) }}, nil
	}
	waitSwarmControlTestResponse(t, startSwarmControlTestRequest(swarm, context.Background(), "enable"), http.StatusServiceUnavailable)
	readSwarmControlTestStatus(t, swarm, "miner-1", "failed")
	if closes.Load() != 1 || swarm.runCtx.Err() != nil {
		t.Fatalf("startup failure ownership: closes=%d swarm=%v", closes.Load(), swarm.runCtx.Err())
	}
	waitSwarmControlTestResponse(t, startSwarmControlTestRequest(swarm, context.Background(), "enable"), http.StatusOK)
	oldCallback(errors.New("late callback from rejected startup"))
	readSwarmControlTestStatus(t, swarm, "miner-1", "running")
	if swarm.runCtx.Err() != nil {
		t.Fatal("rejected startup callback canceled its replacement")
	}
}

// The operation deadline cancels cooperative startup even without an HTTP
// caller deadline. A zero budget forces this boundary without a timing race.
func TestProviderSwarmControlStartupHasIndependentDeadline(t *testing.T) {
	swarm := newSwarmControlTestSwarm(t)
	swarm.startTimeout = 0
	swarm.startMember = func(ctx context.Context, _ ProviderSwarmMember, _ func(error)) (*providerSwarmInstance, error) {
		<-ctx.Done()
		return nil, context.Cause(ctx)
	}
	response := waitSwarmControlTestResponse(t, startSwarmControlTestRequest(swarm, context.Background(), "enable"), http.StatusServiceUnavailable)
	if !strings.Contains(response.Body.String(), context.DeadlineExceeded.Error()) || swarm.runCtx.Err() != nil {
		t.Fatalf("startup deadline did not retain swarm lifetime: %s, %v", response.Body.String(), swarm.runCtx.Err())
	}
}

// The HTTP distinction depends on typed causes, never on a refusal's text.
func TestProviderSwarmControlClassifiesTypedTransientFailures(t *testing.T) {
	for _, testCase := range []struct {
		err  error
		code int
	}{
		{err: &url.Error{Op: "Post", URL: "https://api.example/sn/wallet", Err: context.DeadlineExceeded}, code: http.StatusServiceUnavailable},
		{err: &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}, code: http.StatusServiceUnavailable},
		{err: io.ErrUnexpectedEOF, code: http.StatusServiceUnavailable},
		{err: &connect.HttpStatusError{StatusCode: http.StatusServiceUnavailable}, code: http.StatusServiceUnavailable},
		{err: &connect.HttpStatusError{StatusCode: http.StatusUnauthorized}, code: http.StatusConflict},
		{err: errors.New("wallet challenge deadline exceeded; authorization refused"), code: http.StatusConflict},
	} {
		swarm := newSwarmControlTestSwarm(t)
		swarm.startMember = func(context.Context, ProviderSwarmMember, func(error)) (*providerSwarmInstance, error) {
			return nil, fmt.Errorf("set wallet: %w", testCase.err)
		}
		waitSwarmControlTestResponse(t, startSwarmControlTestRequest(swarm, context.Background(), "enable"), testCase.code)
		readSwarmControlTestStatus(t, swarm, "miner-1", "failed")
	}
}

// Shutdown must cancel before joining an in-flight startup, and cannot return
// until that startup's returned resources finish closing.
func TestProviderSwarmControlShutdownJoinsStartupCleanup(t *testing.T) {
	swarm := newSwarmControlTestSwarm(t)
	startupEntered := make(chan struct{})
	teardown := newSwarmControlTestBarrier(t)
	var closes atomic.Int32
	swarm.startMember = func(ctx context.Context, _ ProviderSwarmMember, _ func(error)) (*providerSwarmInstance, error) {
		close(startupEntered)
		<-ctx.Done()
		return &providerSwarmInstance{refreshSub: &swarmControlTestSub{close: func() {
			closes.Add(1)
			teardown.block()
		}}}, nil
	}
	request := startSwarmControlTestRequest(swarm, context.Background(), "enable")
	waitSwarmControlTestSignal(t, startupEntered)
	stopped := make(chan struct{})
	go func() {
		swarm.stopMembers()
		close(stopped)
	}()
	waitSwarmControlTestSignal(t, teardown.entered)
	waitSwarmControlTestResponse(t, request, http.StatusServiceUnavailable)
	select {
	case <-stopped:
		t.Fatal("shutdown returned while startup still owned cleanup")
	default:
	}
	teardown.release()
	waitSwarmControlTestSignal(t, stopped)
	if closes.Load() != 1 || len(swarm.instances) != 0 || len(swarm.memberOperations) != 0 {
		t.Fatalf("shutdown ownership: closes=%d instances=%d operations=%d", closes.Load(), len(swarm.instances), len(swarm.memberOperations))
	}
}

// An admitted disable retains exclusive close ownership through shutdown.
func TestProviderSwarmControlShutdownJoinsDisable(t *testing.T) {
	swarm := newSwarmControlTestSwarm(t)
	teardown := newSwarmControlTestBarrier(t)
	var closes atomic.Int32
	swarm.startMember = func(context.Context, ProviderSwarmMember, func(error)) (*providerSwarmInstance, error) {
		return &providerSwarmInstance{refreshSub: &swarmControlTestSub{close: func() {
			closes.Add(1)
			teardown.block()
		}}}, nil
	}
	waitSwarmControlTestResponse(t, startSwarmControlTestRequest(swarm, context.Background(), "enable"), http.StatusOK)
	disabled := startSwarmControlTestRequest(swarm, context.Background(), "disable")
	waitSwarmControlTestSignal(t, teardown.entered)
	stopped := make(chan struct{})
	runCtx := swarm.runCtx
	go func() {
		swarm.stopMembers()
		close(stopped)
	}()
	waitSwarmControlTestSignal(t, runCtx.Done())
	select {
	case <-stopped:
		t.Fatal("shutdown returned while disable still owned close")
	default:
	}
	teardown.release()
	waitSwarmControlTestSignal(t, stopped)
	select {
	case <-disabled:
	case <-time.After(5 * time.Second):
		t.Fatal("disable handler survived shutdown")
	}
	if closes.Load() != 1 || len(swarm.instances) != 0 {
		t.Fatalf("shutdown closed the same member %d times", closes.Load())
	}
}

// Instance cancellation must happen before any external subscription close
// can join work that needs that same lifetime to end.
func TestProviderSwarmControlInstanceCancelsBeforeJoining(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	instance := &providerSwarmInstance{
		cancel: cancel,
		refreshSub: &swarmControlTestSub{close: func() {
			if ctx.Err() == nil {
				t.Error("subscription close preceded member cancellation")
			}
		}},
	}
	instance.close()
}

// A later initial-member failure must cancel the run before the first
// instance's close joins any work tied to that run context.
func TestProviderSwarmControlRunCancelsBeforeClosingStartedMembers(t *testing.T) {
	config := validProviderSwarmConfig(t)
	second := validProviderSwarmConfig(t).Members[0]
	second.ID = "miner-2"
	second.SourceIP = "127.64.0.2"
	config.Members = append(config.Members, second)
	swarm, err := NewProviderSwarm(&config)
	if err != nil {
		t.Fatal(err)
	}
	// The in-process server needs no known port; zero is only assigned after
	// validation of the public configuration contract.
	config.ListenAddress = "127.0.0.1:0"
	var closes atomic.Int32
	swarm.startMember = func(ctx context.Context, member ProviderSwarmMember, _ func(error)) (*providerSwarmInstance, error) {
		if member.ID == "miner-2" {
			return nil, errors.New("synthetic second member failure")
		}
		return &providerSwarmInstance{refreshSub: &swarmControlTestSub{close: func() {
			closes.Add(1)
			if ctx.Err() == nil {
				t.Error("run cleanup joined a member before canceling its lifetime")
			}
		}}}, nil
	}
	if err := swarm.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "synthetic second member failure") {
		t.Fatalf("run error = %v", err)
	}
	if closes.Load() != 1 || len(swarm.instances) != 0 || len(swarm.memberOperations) != 0 {
		t.Fatalf("run retained member ownership: closes=%d instances=%d operations=%d", closes.Load(), len(swarm.instances), len(swarm.memberOperations))
	}
}
