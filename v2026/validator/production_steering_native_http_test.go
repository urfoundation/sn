// Native phase waits exercise the configured HTTP client and real physical
// response ownership. The existing clock seam controls only deadline expiry.
package validator

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/v2026/crv4"
)

// A separate done channel forces derived contexts to observe this clock's
// DeadlineExceeded cause instead of the underlying cancel implementation.
type productionNativeReadDeadline struct {
	context.Context
	done      chan struct{}
	stateLock sync.Mutex
	cause     error
}

func (self *productionNativeReadDeadline) Done() <-chan struct{} { return self.done }

func (self *productionNativeReadDeadline) Err() error {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return self.cause
}

func (self *productionNativeReadDeadline) finish(cause error) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if self.cause == nil {
		self.cause = cause
		close(self.done)
	}
}

// Retain the actual requested deadline as an upper bound. A test barrier may
// advance its logical clock, and cleanup joins the sole propagation callback.
func newProductionNativeReadDeadline(parent context.Context, duration time.Duration) (*productionNativeReadDeadline, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(parent, duration)
	self := &productionNativeReadDeadline{Context: ctx, done: make(chan struct{})}
	joined := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		defer close(joined)
		self.finish(ctx.Err())
	})
	var once sync.Once
	return self, func() {
		once.Do(func() {
			stopped := stop()
			self.finish(context.Canceled)
			cancel()
			if !stopped {
				<-joined
			}
		})
	}
}

// Initialize real GSRPC metadata/genesis/runtime through the public native
// constructor. Only the selected finalized-head HTTP response is faulted.
func newProductionNativeReadHttpFixture(t *testing.T, reply func(http.ResponseWriter, *http.Request, json.RawMessage)) *crv4.Chain {
	t.Helper()
	metadata := validatorRuntimeIdentityTestMetadata(t)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var call struct {
			Id     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if request.Method != http.MethodPost || json.NewDecoder(request.Body).Decode(&call) != nil {
			t.Error("native configured client changed its HTTP request")
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		var result any
		switch call.Method {
		case "state_getMetadata":
			result = metadata
		case "chain_getBlockHash":
			result = (types.Hash{0x37}).Hex()
		case "state_getRuntimeVersion":
			result = map[string]any{"specName": "synthetic-native-read", "specVersion": 9_001, "transactionVersion": 1, "stateVersion": 1}
		case "chain_getFinalizedHead":
			reply(writer, request, call.Id)
			return
		default:
			t.Errorf("unexpected configured native read: %s", call.Method)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": call.Id, "result": result})
	}))
	t.Cleanup(server.Close)
	native, err := crv4.DialChainContext(t.Context(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(native.API.Client.Close)
	return native
}

// A physical response reaches its body barrier before the sixty-second clock
// is advanced. The five-minute phase then exhausts while the caller stays live.
// Its real typed cause remains observable without spending the hard budget.
func TestProductionSteeringNativeHttpExhaustionBecomesObservableWait(t *testing.T) {
	entered, stopped := make(chan struct{}), make(chan struct{})
	native := newProductionNativeReadHttpFixture(t, func(writer http.ResponseWriter, request *http.Request, _ json.RawMessage) {
		defer close(stopped)
		writer.Header().Set("Content-Length", "1000")
		_, _ = writer.Write([]byte(`{"jsonrpc":`))
		writer.(http.Flusher).Flush()
		close(entered)
		<-request.Context().Done()
	})
	self := &ReleaseSteerer{cfg: &ReleaseConfig{SchemaVersion: ReleaseMainnetProductionSchemaVersion}}
	var durations []time.Duration
	var operation *productionNativeReadDeadline
	attemptReady := make(chan *productionNativeReadDeadline, 1)
	self.productionReadHooks.withTimeout = func(ctx context.Context, duration time.Duration) (context.Context, context.CancelFunc) {
		durations = append(durations, duration)
		clock, cancel := newProductionNativeReadDeadline(ctx, duration)
		if operation == nil {
			operation = clock
		} else {
			attemptReady <- clock
		}
		return clock, cancel
	}
	self.productionReadHooks.wait = func(context.Context, time.Duration) error {
		operation.finish(context.DeadlineExceeded)
		return operation.Err()
	}
	intent := &SteeringIntent{SubnetEpoch: 23, Prepared: &crv4.PreparedSubmission{ExtrinsicHash: "synthetic-original-signed-identity"}}
	var physical error
	done := make(chan error, 1)
	go func() {
		done <- self.productionRead(t.Context(), productionReadReceipt, intent, func(ctx context.Context) error {
			var result string
			physical = native.API.Client.CallContext(ctx, &result, "chain_getFinalizedHead")
			return physical
		})
	}()
	attempt := <-attemptReady
	<-entered
	attempt.finish(context.DeadlineExceeded)
	err := <-done
	<-stopped
	var wait *productionSteeringReadWait
	if !errors.As(err, &wait) || wait.phase != productionReadReceipt || wait.nativeEpoch != intent.SubnetEpoch || wait.extrinsicHash != intent.Prepared.ExtrinsicHash || !crv4.HasSubstrateReadTransportCause(physical) || !crv4.RetryableSubstrateReadTransportError(physical) || t.Context().Err() != nil || !slices.Equal(durations, []time.Duration{300 * time.Second, 60 * time.Second}) {
		t.Fatalf("configured native physical deadline became a hard or unowned result: budgets=%v physical=%v result=%v", durations, physical, err)
	}
	progress := &releaseProgress{now: func() time.Time { return time.Unix(2_000_000_000, 0) }}
	polls := 0
	loopErr := runReleaseProductionSteeringLoopWithWait(t.Context(), func() error { polls++; return err }, func() bool { return polls < releaseSteeringFailureLimit+2 }, progress)
	if loopErr != nil || polls != releaseSteeringFailureLimit+2 || progress.value.Steering == nil || progress.value.Steering.Current || progress.value.Steering.Outcome != "receipt_transport_wait" {
		t.Fatalf("configured native read exhaustion spent the hard budget or became healthy: polls=%d result=%+v error=%v", polls, progress.value.Steering, loopErr)
	}
	hard := errors.New("synthetic canonical evidence contradiction")
	if retryableProductionSteeringRead(errors.Join(physical, hard)) {
		t.Fatal("configured native origin hid a joined integrity contradiction")
	}
}

// A complete malformed response is a protocol defect. Adding a phase deadline
// must not relabel it as the physical interrupted body exercised above.
func TestProductionSteeringNativeHttpCompleteMalformedBodyStaysHard(t *testing.T) {
	native := newProductionNativeReadHttpFixture(t, func(writer http.ResponseWriter, _ *http.Request, _ json.RawMessage) {
		_, _ = writer.Write([]byte(`{"jsonrpc":`))
	})
	self := &ReleaseSteerer{cfg: &ReleaseConfig{SchemaVersion: ReleaseMainnetProductionSchemaVersion}}
	waits := 0
	self.productionReadHooks.wait = func(context.Context, time.Duration) error { waits++; return nil }
	err := self.productionRead(t.Context(), productionReadReceipt, nil, func(ctx context.Context) error {
		var result string
		return errors.Join(native.API.Client.CallContext(ctx, &result, "chain_getFinalizedHead"), context.DeadlineExceeded)
	})
	var wait *productionSteeringReadWait
	if err == nil || errors.As(err, &wait) || crv4.HasSubstrateReadTransportCause(err) || retryableProductionSteeringRead(err) || waits != 0 {
		t.Fatalf("complete malformed native response borrowed phase retry: waits=%d error=%v", waits, err)
	}
}
