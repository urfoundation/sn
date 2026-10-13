// Retained-plan reconstruction distinguishes an interrupted computation from
// completed contradictory history. RPC failures retain their original causes.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// The second observation is inside the real planner's capacity evaluation.
// Its ordinary cancellation channel closes before that boundary returns.
type ownerTrimRebuildCancelContext struct {
	context.Context
	cancel context.CancelFunc
	checks atomic.Uint64
}

// Cancel through the real parent before returning the capacity-loop check.
func (self *ownerTrimRebuildCancelContext) Err() error {
	if self.checks.Add(1) == 2 {
		self.cancel()
	}
	return self.Context.Err()
}

// Historical census acquisition completes before the bounded reconstruction
// stops. Neither a canceled rebuild nor its empty result establishes a mismatch.
func TestOwnerTrimGuardReconstructionPreservesStopCause(t *testing.T) {
	for _, boundary := range []string{"canceled-before", "canceled-during-capacity", "deadline-before"} {
		func() {
			client, fixture, policy, policyHash, retained := newOwnerTrimGuardFixture(t, true)
			baseline, err := client.readSubnetPreviewAt(t.Context(), policy, policyHash, retained.Census.Observation.Identity.FinalizedHash)
			if err != nil {
				t.Fatal(err)
			}
			var ctx context.Context
			var observed *ownerTrimRebuildCancelContext
			want := context.Canceled
			if boundary == "deadline-before" {
				deadline, cancel := context.WithDeadline(t.Context(), time.Unix(1, 0))
				defer cancel()
				ctx, want = deadline, context.DeadlineExceeded
			} else {
				parent, cancel := context.WithCancel(t.Context())
				defer cancel()
				ctx = parent
				if boundary == "canceled-before" {
					cancel()
				} else {
					observed = &ownerTrimRebuildCancelContext{Context: parent, cancel: cancel}
					ctx = observed
				}
			}
			rebuilt, err := rebuildOwnerTrimGuardPlan(ctx, policy, baseline, retained)
			inflight, _ := fixture.counts()
			if !errors.Is(err, want) || errors.Is(err, errRpcIntegrity) || !reflect.DeepEqual(rebuilt, ownerTrimPlan{}) || inflight != 0 {
				t.Fatalf("interrupted reconstruction lost its cause or claimed contradictory history: boundary=%s err=%v inflight=%d", boundary, err, inflight)
			}
			if observed != nil && (observed.checks.Load() != 2 || observed.Context.Err() != context.Canceled) {
				t.Fatal("capacity boundary did not cancel the real context", observed.checks.Load())
			}
		}()
	}
}

// A changed retained selection still fails hard after the same complete census
// rebuild. Matching reconstruction keeps the original immutable seal.
func TestOwnerTrimGuardReconstructionRequiresExactHistoricalPlan(t *testing.T) {
	client, _, policy, policyHash, retained := newOwnerTrimGuardFixture(t, true)
	baseline, err := client.readSubnetPreviewAt(t.Context(), policy, policyHash, retained.Census.Observation.Identity.FinalizedHash)
	if err != nil {
		t.Fatal(err)
	}
	rebuilt, err := rebuildOwnerTrimGuardPlan(t.Context(), policy, baseline, retained)
	if err != nil || rebuilt.ContentHash != retained.ContentHash {
		t.Fatal("matching historical reconstruction changed its original seal", err)
	}
	retained.Residual = nil
	retained.ContentHash, err = ownerTrimPlanHash(retained)
	if err != nil {
		t.Fatal(err)
	}
	rebuilt, err = rebuildOwnerTrimGuardPlan(t.Context(), policy, baseline, retained)
	if !errors.Is(err, errRpcIntegrity) || !reflect.DeepEqual(rebuilt, ownerTrimPlan{}) {
		t.Fatal("completed contradictory reconstruction lost integrity classification", err)
	}
	result, err := client.readOwnerTrimGuard(t.Context(), policy, policyHash, retained, "recheck")
	if !errors.Is(err, errRpcIntegrity) || !reflect.DeepEqual(result, ownerTrimGuard{}) {
		t.Fatal("public recheck accepted a rehashed contradictory historical selection", err)
	}
}

// A real read-retry boundary cancels after a selected historical/current read
// fails. The guard must retain both the transient RPC cause and the joined stop.
func TestOwnerTrimGuardReadFailurePreservesOriginalCause(t *testing.T) {
	for _, phase := range []string{"historical", "current"} {
		for _, fault := range []string{"transport", "rpc-timeout"} {
			func() {
				client, fixture, policy, policyHash, retained := newOwnerTrimGuardFixture(t, true)
				base := client.httpClient.Transport
				block := retained.Census.Observation.Identity.FinalizedHash
				if phase == "current" {
					block = fixture.afterHash
				}
				original := errors.New("synthetic owner trim read transport unavailable")
				var calls, waits atomic.Uint64
				client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
					body, err := request.GetBody()
					if err != nil {
						return nil, err
					}
					var call struct {
						Method string            `json:"method"`
						Params []json.RawMessage `json:"params"`
					}
					err = json.NewDecoder(body).Decode(&call)
					body.Close()
					if err != nil {
						return nil, err
					}
					var at string
					if call.Method == "state_getStorage" && len(call.Params) == 2 && json.Unmarshal(call.Params[1], &at) == nil && at == block {
						calls.Add(1)
						if fault == "transport" {
							return nil, original
						}
						return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"Request timeout"}}`))}, nil
					}
					return base.RoundTrip(request)
				})
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				client.retryWait = func(waitCtx context.Context, _ time.Duration) error {
					waits.Add(1)
					cancel()
					return waitCtx.Err()
				}
				result, err := client.readOwnerTrimGuard(ctx, policy, policyHash, retained, "recheck")
				inflight, _ := fixture.counts()
				if !errors.Is(err, context.Canceled) || errors.Is(err, errRpcIntegrity) || !reflect.DeepEqual(result, ownerTrimGuard{}) || calls.Load() == 0 || waits.Load() == 0 || inflight != 0 {
					t.Fatalf("read failure lost its stop or acquired integrity authority: phase=%s fault=%s err=%v calls=%d waits=%d inflight=%d", phase, fault, err, calls.Load(), waits.Load(), inflight)
				}
				if fault == "transport" && !errors.Is(err, original) {
					t.Fatal("historical/current read lost original transport cause", err)
				}
				var rpcErr *rpcCallError
				if fault == "rpc-timeout" && (!errors.As(err, &rpcErr) || rpcErr.method != "state_getStorage" || rpcErr.code != -32000 || rpcErr.message != "Request timeout") {
					t.Fatal("historical/current read lost original RPC timeout cause", err)
				}
			}()
		}
	}
}
