package crv4

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/gorilla/websocket"
)

type substrateReadTestCapacityError struct{}

func (*substrateReadTestCapacityError) Error() string  { return substrateHistoricalCapacityMessage }
func (*substrateReadTestCapacityError) ErrorCode() int { return -32000 }

// The real client still performs every request. Deadline construction and
// pacing share one virtual clock, without a five-minute test sleep.
type substrateReadTestDeadline struct {
	context.Context
	expired atomic.Bool
}

func (self *substrateReadTestDeadline) Err() error {
	if self.expired.Load() {
		return context.DeadlineExceeded
	}
	return self.Context.Err()
}

type substrateReadTestBudget struct {
	mu       sync.Mutex
	elapsed  time.Duration
	attempts int
	owner    *substrateReadTestDeadline
	cancel   context.CancelFunc
}

func (self *substrateReadTestBudget) advance(delay time.Duration) error {
	self.mu.Lock()
	defer self.mu.Unlock()
	self.elapsed += delay
	if self.elapsed >= 300*time.Second {
		self.elapsed = 300 * time.Second
		self.owner.expired.Store(true)
		self.cancel()
	}
	return self.owner.Err()
}

func newSubstrateReadTestBudget(t *testing.T, reconnectStep time.Duration) (*substrateReadTestBudget, substrateRpcReadRetryHooks) {
	t.Helper()
	budget := &substrateReadTestBudget{}
	hooks := substrateRpcReadRetryHooks{
		withTimeout: func(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
			budget.mu.Lock()
			defer budget.mu.Unlock()
			if budget.owner == nil {
				if timeout != 300*time.Second {
					t.Fatalf("read retry owner has budget %s, want five minutes", timeout)
				}
				ctx, cancel := context.WithCancel(parent)
				budget.owner = &substrateReadTestDeadline{Context: ctx}
				budget.cancel = cancel
				t.Cleanup(cancel)
				return budget.owner, cancel
			}
			if timeout != 60*time.Second || parent != budget.owner {
				t.Fatalf("read attempt lost its minute or retry owner: timeout=%s", timeout)
			}
			budget.attempts++
			// The fixture releases real exchanges explicitly. Assert the
			// production allowance here without introducing a second clock
			// that could expire before the virtual owner advances.
			return context.WithCancel(parent)
		},
		wait: func(ctx context.Context, _ <-chan struct{}, delay time.Duration) error {
			if ctx != budget.owner || delay < time.Second || delay > 8*time.Second {
				t.Fatalf("read retry lost its owner or bounded pace: delay=%s", delay)
			}
			if reconnectStep > delay {
				delay = reconnectStep
			}
			return budget.advance(delay)
		},
		waitCapacity: func(ctx context.Context, _ <-chan struct{}, gate *substrateRPCCapacityGate) error {
			gate.mu.Lock()
			refused := !gate.until.IsZero()
			gate.until = time.Time{}
			gate.mu.Unlock()
			if refused {
				if ctx != budget.owner {
					t.Fatal("provider cooldown lost its retry owner")
				}
				// A recorded refusal spends one virtual cooldown. A real
				// scheduler delay must not shorten this logical interval.
				return budget.advance(time.Minute)
			}
			return nil
		},
	}
	return budget, hooks
}

func TestSubstrateReadNormalCloseRetriesToFullBudget(t *testing.T) {
	f := newSubstrateReconnectFixture(t, func(int, int, chainContextRPCRequest) string { return "normal_close" })
	budget, hooks := newSubstrateReadTestBudget(t, 75*time.Second)
	f.client.readRetry = hooks
	var result string
	err := f.client.CallContext(t.Context(), &result, "chain_getFinalizedHead")
	var closed *websocket.CloseError
	if !errors.Is(err, context.DeadlineExceeded) || !errors.As(err, &closed) || closed.Code != websocket.CloseNormalClosure {
		t.Fatalf("read exhaustion lost the actual normal close: %v", err)
	}
	requests, connections := f.snapshot()
	if budget.elapsed != 300*time.Second || budget.attempts != 4 || len(requests) != 4 || connections != 4 {
		t.Fatalf("normal close escaped its full read budget: elapsed=%s attempts=%d requests=%d connections=%d", budget.elapsed, budget.attempts, len(requests), connections)
	}
	if f.completedSendBarriers() != 4 {
		t.Fatalf("normal-close exhaustion skipped send completion: barriers=%d", f.completedSendBarriers())
	}
	for _, request := range requests {
		if request.Method != "chain_getFinalizedHead" || len(request.Params) != 0 {
			t.Fatalf("scheduler reconnect changed request: %+v", request)
		}
	}
}

func TestSubstrateReadNormalCloseRecoversBeyondFourCalls(t *testing.T) {
	f := newSubstrateReconnectFixture(t, func(_ int, count int, _ chainContextRPCRequest) string {
		if count <= 5 {
			return "normal_close"
		}
		return "success"
	})
	budget, hooks := newSubstrateReadTestBudget(t, 30*time.Second)
	f.client.readRetry = hooks
	var result string
	block := types.Hash{52}
	err := f.client.CallContext(t.Context(), &result, "state_getRuntimeVersion", block.Hex())
	requests, connections := f.snapshot()
	if err != nil || result != "0x2a00" || len(requests) != 6 || connections != 6 || budget.elapsed != 150*time.Second {
		t.Fatalf("read reconnect abandoned recoverable work: result=%q requests=%d connections=%d elapsed=%s err=%v", result, len(requests), connections, budget.elapsed, err)
	}
	if f.completedSendBarriers() != 5 {
		t.Fatalf("normal-close recovery skipped send completion: barriers=%d", f.completedSendBarriers())
	}
	for _, request := range requests {
		if request.Method != "state_getRuntimeVersion" || len(request.Params) != 1 || string(request.Params[0]) != fmt.Sprintf("%q", block.Hex()) {
			t.Fatalf("runtime replay changed pinned request: %+v", request)
		}
	}
}

func TestSubstrateReadDisconnectRejectsMixedAndPermanentClose(t *testing.T) {
	closed := &websocket.CloseError{Code: websocket.CloseNormalClosure}
	for _, cause := range []error{
		errors.Join(closed, errors.New("local state close failed")),
		errors.Join(closed, context.Canceled),
		&websocket.CloseError{Code: websocket.ClosePolicyViolation},
		&websocket.CloseError{Code: websocket.CloseProtocolError},
		&websocket.CloseError{Code: websocket.CloseInvalidFramePayloadData},
		errors.New(closed.Error()),
		io.ErrUnexpectedEOF,
	} {
		if substrateRPCDisconnected(cause) {
			t.Fatalf("unowned or permanent disconnect authorized a replay: %v", cause)
		}
	}
	if !substrateRPCDisconnected(&url.Error{Op: "Post", URL: "http://rpc.invalid", Err: io.ErrUnexpectedEOF}) {
		t.Fatal("actual HTTP RPC read lost its transport EOF origin")
	}
	for _, code := range []int{websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseAbnormalClosure, websocket.CloseInternalServerErr, websocket.CloseServiceRestart, websocket.CloseTryAgainLater} {
		if !substrateRPCDisconnected(fmt.Errorf("read: %w", &websocket.CloseError{Code: code})) {
			t.Fatalf("transport close %d could not reconnect", code)
		}
	}
}

func TestSubstrateReadAttemptDeadlineCanRetryWithoutReplacingCaller(t *testing.T) {
	f := newSubstrateReconnectFixture(t, func(int, int, chainContextRPCRequest) string { return "success" })
	budget, hooks := newSubstrateReadTestBudget(t, 0)
	withTimeout := hooks.withTimeout
	hooks.withTimeout = func(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
		if budget.owner != nil && budget.attempts == 0 {
			if timeout != 60*time.Second || parent != budget.owner {
				t.Fatalf("first read did not own a minute: timeout=%s", timeout)
			}
			budget.attempts++
			return context.WithDeadline(parent, time.Now().Add(-time.Second))
		}
		return withTimeout(parent, timeout)
	}
	f.client.readRetry = hooks
	var result string
	err := f.client.CallContext(t.Context(), &result, "chain_getFinalizedHead")
	requests, _ := f.snapshot()
	if err != nil || result != "0x2a00" || budget.attempts != 2 || len(requests) != 1 || budget.elapsed != time.Second || t.Context().Err() != nil {
		t.Fatalf("expired attempt poisoned its live caller: result=%q attempts=%d requests=%d elapsed=%s err=%v", result, budget.attempts, len(requests), budget.elapsed, err)
	}
}

func TestSubstrateReadCapacityRejectsIndependentFailures(t *testing.T) {
	capacity := &substrateReadTestCapacityError{}
	if !substrateRPCHistoricalCapacity(fmt.Errorf("read: %w", capacity)) {
		t.Fatal("typed provider capacity lost its retry scope")
	}
	for _, cause := range []error{
		errors.Join(capacity, errors.New("local read close failed")),
		errors.Join(capacity, context.Canceled),
		errors.New(capacity.Error()),
	} {
		if substrateRPCHistoricalCapacity(cause) || substrateRPCDisconnected(cause) {
			t.Fatalf("capacity refusal hid an independent cause: %v", cause)
		}
	}
}

// A real owner cancellation is independent of virtual retry time. Force it
// after one retry step and require the original disconnect to remain visible;
// the five-minute budget does not authorize another request after cancellation.
func TestSubstrateReadVirtualBudgetPreservesCallerCancellation(t *testing.T) {
	f := newSubstrateReconnectFixture(t, func(int, int, chainContextRPCRequest) string { return "drop" })
	budget, hooks := newSubstrateReadTestBudget(t, 75*time.Second)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	wait := hooks.wait
	hooks.wait = func(ctx context.Context, closed <-chan struct{}, delay time.Duration) error {
		if err := wait(ctx, closed, delay); err != nil {
			return err
		}
		cancel()
		return nil // Force a ready retry alongside the owner cancellation.
	}
	f.client.readRetry = hooks
	var result string
	err := f.client.CallContext(ctx, &result, "state_getStorage", "0x0102", types.Hash{53}.Hex())
	requests, connections := f.snapshot()
	if !errors.Is(err, context.Canceled) || budget.elapsed != 75*time.Second || budget.attempts != 1 || len(requests) != 1 || connections != 1 || f.completedSendBarriers() != 1 {
		t.Fatalf("caller cancellation lost authority over virtual time: elapsed=%s attempts=%d requests=%d connections=%d barriers=%d err=%v", budget.elapsed, budget.attempts, len(requests), connections, f.completedSendBarriers(), err)
	}
	if !errors.Is(err, syscall.ECONNRESET) && !errors.Is(err, net.ErrClosed) {
		var closed *websocket.CloseError
		if !errors.As(err, &closed) || closed.Code != websocket.CloseAbnormalClosure {
			t.Fatalf("caller cancellation erased the peer disconnect: %v", err)
		}
	}
}
