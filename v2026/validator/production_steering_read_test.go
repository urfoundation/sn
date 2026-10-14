// Read budgets and loop classifications are independent of cryptographic
// continuation; genuine-owner tests cover actual signed state and receipt I/O.
package validator

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	gethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/urfoundation/sn/v2026/crv4"
)

// A physical origin or the exact missing-evidence type is required. Text,
// local custody failures and complete contradictions cannot borrow retries.
func TestProductionSteeringReadCauseClassification(t *testing.T) {
	unavailable := &crv4.ReceiptEvidenceUnavailableError{Field: "block body"}
	hard := errors.New("canonical commitment differs")
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "deadline", err: context.DeadlineExceeded, want: true},
		{name: "unavailable body", err: unavailable, want: true},
		{name: "owned HTTP 500", err: gethrpc.HTTPError{StatusCode: 500}, want: true},
		{name: "owned HTTP 502", err: gethrpc.HTTPError{StatusCode: 502}, want: true},
		{name: "owned HTTP 401", err: gethrpc.HTTPError{StatusCode: 401}, want: false},
		{name: "physical EOF", err: &url.Error{Op: "Post", URL: "https://synthetic.invalid", Err: io.EOF}, want: true},
		{name: "unowned EOF", err: io.EOF, want: false},
		{name: "cancellation", err: context.Canceled, want: false},
		{name: "local custody", err: &os.PathError{Op: "read", Path: "intent.json", Err: context.DeadlineExceeded}, want: false},
		{name: "mixed contradiction", err: errors.Join(unavailable, hard, context.DeadlineExceeded), want: false},
		{name: "diagnostic text", err: errors.New("500 timeout EOF block unavailable"), want: false},
	}
	for _, item := range cases {
		if actual := retryableProductionSteeringRead(item.err); actual != item.want {
			t.Errorf("%s: retry=%t, want=%t", item.name, actual, item.want)
		}
	}
}

// The owner uses the real shared deadlines. Only the delay is cut short by a
// deterministic clock boundary; no callback grants a successful read verdict.
func TestProductionSteeringReadBudgetExhaustionRetainsPhase(t *testing.T) {
	self := &ReleaseSteerer{cfg: &ReleaseConfig{SchemaVersion: ReleaseMainnetProductionSchemaVersion}}
	var durations []time.Duration
	self.productionReadHooks.withTimeout = func(ctx context.Context, duration time.Duration) (context.Context, context.CancelFunc) {
		durations = append(durations, duration)
		return context.WithTimeout(ctx, duration)
	}
	self.productionReadHooks.wait = func(context.Context, time.Duration) error { return context.DeadlineExceeded }
	intent := &SteeringIntent{SubnetEpoch: 23, Prepared: &crv4.PreparedSubmission{ExtrinsicHash: "original-signed-identity"}}
	reads := 0
	err := self.productionRead(t.Context(), productionReadReceipt, intent, func(ctx context.Context) error {
		reads++
		if deadline, found := ctx.Deadline(); !found || time.Until(deadline) < 59*time.Second {
			t.Fatal("expected read lost its sixty-second attempt budget")
		}
		return &crv4.ReceiptEvidenceUnavailableError{Field: "block body"}
	})
	var wait *productionSteeringReadWait
	if !errors.As(err, &wait) || wait.phase != productionReadReceipt || wait.nativeEpoch != 23 || !wait.epochKnown || wait.extrinsicHash != intent.Prepared.ExtrinsicHash || reads != 1 || !slices.Equal(durations, []time.Duration{300 * time.Second, 60 * time.Second}) {
		t.Fatalf("bounded receipt read changed phase, identity or budget: reads=%d budgets=%v error=%v", reads, durations, err)
	}
	if !errors.Is(err, context.DeadlineExceeded) || !retryableProductionSteeringRead(wait.cause) {
		t.Fatal("budget exhaustion discarded its original retryable causes")
	}
}

// Exercise the real configured EVM HTTP client rather than constructing its
// error value. The same production read owner recovers from both 500 and 502.
func TestProductionSteeringReadRecoversActualEvmHttpFailure(t *testing.T) {
	for _, status := range []int{http.StatusInternalServerError, http.StatusBadGateway} {
		var requests atomic.Int32
		endpoint := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			var call struct {
				Id     json.RawMessage `json:"id"`
				Method string          `json:"method"`
			}
			if request.Method != http.MethodPost || json.NewDecoder(request.Body).Decode(&call) != nil || call.Method != "eth_blockNumber" {
				t.Error("production read changed its exact HTTP/RPC method")
				writer.WriteHeader(http.StatusBadRequest)
				return
			}
			if requests.Add(1) == 1 {
				writer.WriteHeader(status)
				return
			}
			writer.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": call.Id, "result": "0x64"})
		}))
		t.Cleanup(endpoint.Close)
		client, err := gethrpc.DialContext(t.Context(), endpoint.URL)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(client.Close)
		self := &ReleaseSteerer{cfg: &ReleaseConfig{SchemaVersion: ReleaseMainnetProductionSchemaVersion}}
		waits := 0
		self.productionReadHooks.wait = func(context.Context, time.Duration) error { waits++; return nil }
		var number string
		err = self.productionRead(t.Context(), productionReadPreparation, nil, func(ctx context.Context) error {
			return client.CallContext(ctx, &number, "eth_blockNumber")
		})
		if err != nil || number != "0x64" || requests.Load() != 2 || waits != 1 {
			t.Fatalf("actual HTTP %d failed to recover within its read owner: calls=%d waits=%d number=%q error=%v", status, requests.Load(), waits, number, err)
		}
	}
}

func TestProductionSteeringReadRejectsMixedAndCancellation(t *testing.T) {
	self := &ReleaseSteerer{cfg: &ReleaseConfig{SchemaVersion: ReleaseMainnetProductionSchemaVersion}}
	waits := 0
	self.productionReadHooks.wait = func(context.Context, time.Duration) error { waits++; return nil }
	hard := errors.New("authenticated body commitment mismatch")
	err := self.productionRead(t.Context(), productionReadReceipt, nil, func(context.Context) error { return errors.Join(hard, context.DeadlineExceeded) })
	var wait *productionSteeringReadWait
	if errors.As(err, &wait) || !errors.Is(err, hard) || waits != 0 {
		t.Fatalf("mixed integrity defect borrowed read retry: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	reads := 0
	err = self.productionRead(ctx, productionReadIntent, nil, func(context.Context) error { reads++; cancel(); return context.Canceled })
	if !errors.Is(err, context.Canceled) || errors.As(err, &wait) || reads != 1 || waits != 0 {
		t.Fatalf("service cancellation became a read wait: %v", err)
	}
}

// These are loop classification controls, not restart/inclusion proof. Real
// persisted continuation is exercised separately without these callbacks.
func TestProductionSteeringLoopKeepsReadWaitObservable(t *testing.T) {
	progress := &releaseProgress{now: func() time.Time { return time.Unix(2_000_000_000, 0) }}
	attempts := 0
	err := runReleaseProductionSteeringLoopWithWait(t.Context(), func() error {
		attempts++
		return &productionSteeringReadWait{phase: productionReadReceipt, nativeEpoch: 7, epochKnown: true, cause: context.DeadlineExceeded}
	}, func() bool { return attempts < releaseSteeringFailureLimit+3 }, progress)
	if err != nil || attempts != releaseSteeringFailureLimit+3 || progress.value.Steering == nil || progress.value.Steering.Current || progress.value.Steering.LastSuccessAt != "" || progress.value.Steering.Outcome != "receipt_transport_wait" || progress.value.Steering.NativeEpoch != 7 {
		t.Fatalf("pure read waits consumed failure budget or became healthy: attempts=%d value=%+v error=%v", attempts, progress.value.Steering, err)
	}
}

func TestProductionSteeringLoopPreservesPriorAndJoinedHardFailures(t *testing.T) {
	hard := errors.New("retained native integrity incident")
	for _, joined := range []bool{false, true} {
		attempts := 0
		progress := &releaseProgress{now: func() time.Time { return time.Unix(2_000_000_000, 0) }}
		err := runReleaseProductionSteeringLoopWithWait(t.Context(), func() error {
			attempts++
			wait := &productionSteeringReadWait{phase: productionReadReceipt, cause: context.DeadlineExceeded}
			if joined {
				return errors.Join(wait, hard)
			}
			if attempts == 1 {
				return hard
			}
			return wait
		}, func() bool { return attempts < releaseSteeringFailureLimit+2 }, progress)
		wantAttempts := releaseSteeringFailureLimit + 2
		if joined {
			wantAttempts = releaseSteeringFailureLimit
		}
		if !errors.Is(err, hard) || attempts != wantAttempts || progress.value.Steering.Current {
			t.Fatalf("joined=%t hid a hard cause: attempts=%d error=%v", joined, attempts, err)
		}
	}
}

// Run must reach its owned V2 startup refusal, not ask a fresh scheduler for
// authority first. This is routing evidence, not a claim of full activation.
func TestProductionSteeringRunReachesIntentOwnerBeforeFreshSchedule(t *testing.T) {
	fixture := newProductionContinuationTestFixture(t)
	native := installProductionContinuationNative(t, fixture)
	fixture.production.head = 102
	native.currentReadError = context.DeadlineExceeded
	before, err := json.Marshal(fixture.steerer.cfg)
	if err != nil {
		t.Fatal(err)
	}
	// Cancel at the first observed actual branch. The old scheduler path also
	// terminates at its physical current-runtime call, preserving a causal test.
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	native.beforeCurrentRead = cancel
	fixture.runtime.progress = &releaseProgress{now: func() time.Time { cancel(); return time.Unix(2_000_000_000, 0) }}
	err = fixture.steerer.Run(ctx)
	after, marshalErr := json.Marshal(fixture.steerer.cfg)
	if err == nil || !strings.Contains(err.Error(), "V2 production startup and submission ownership is incomplete") || native.currentReads != 0 || marshalErr != nil || !reflect.DeepEqual(before, after) || fixture.runtime.progress.value.Steering == nil || fixture.runtime.progress.value.Steering.Outcome != "hard_error" {
		t.Fatalf("production owner routing read fresh runtime or changed config: reads=%d error=%v", native.currentReads, err)
	}
}
