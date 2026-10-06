//go:build linux

// Real daemon GETs retain their original request identity while only the retry
// clock advances. No fixture grants signing or submission authority.
package miner

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/sdk/v2026"
)

// The local fixture keeps the production request allowance and real SDK parser.
// Route racing and the inner status retry are disabled for an exact GET census.
func claimGetHttpTestApi(t *testing.T, origin string) (*connect.ClientStrategy, *sdk.Api) {
	t.Helper()
	settings := defaultClaimReadStrategySettings()
	if settings.RequestTimeout != 60*time.Second {
		t.Fatalf("claim GET request allowance=%s, want sixty seconds", settings.RequestTimeout)
	}
	settings.EnableResilient = false
	settings.GetRetryCount = 0
	strategy := connect.NewClientStrategy(t.Context(), settings)
	api := sdk.NewApi(t.Context(), strategy, origin)
	api.SetByJwt("synthetic-daemon-get-token")
	t.Cleanup(func() {
		if err := api.CloseAndWait(context.Background()); err != nil {
			t.Error(err)
		}
		strategy.Close()
	})
	return strategy, api
}

// Expiry is an explicit transition after completed socket reads, never a sleep.
func claimGetHttpTestHooks(t *testing.T, elapsed *atomic.Int64, owners *int) claimReadRetryHooks {
	t.Helper()
	var owner *ethRpcTestDeadline
	return claimReadRetryHooks{
		withTimeout: func(ctx context.Context, duration time.Duration) (context.Context, context.CancelFunc) {
			if duration != 300*time.Second {
				t.Fatalf("daemon GET budget=%s, want five minutes", duration)
			}
			*owners++
			owner = &ethRpcTestDeadline{Context: ctx, deadline: time.Now().Add(duration), done: make(chan struct{})}
			return owner, owner.finish
		},
		wait: func(ctx context.Context, delay time.Duration) error {
			if delay < 2*time.Second || delay >= 3*time.Second || ctx != owner {
				t.Fatal("daemon GET replaced its retry clock or pacing")
			}
			if time.Duration(elapsed.Add(int64(75*time.Second))) >= 300*time.Second {
				owner.finish()
			}
			return owner.Err()
		},
	}
}

// Both real wire decoders recover beyond a minute without changing auth, URL,
// query epoch, request method or the one original operation deadline.
func TestClaimDaemonGetHttpRecoversBothPinnedReads(t *testing.T) {
	for _, pool := range []bool{false, true} {
		var elapsed atomic.Int64
		var calls, mutations atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if request.Method != http.MethodGet {
				mutations.Add(1)
			}
			if request.URL.Path == "/hello" {
				return
			}
			if request.Header.Get("Authorization") != "Bearer synthetic-daemon-get-token" || pool && (request.URL.Path != "/sn/pool/claim" || request.URL.RawQuery != "epoch=7") || !pool && (request.URL.Path != "/sn/epoch" || request.URL.RawQuery != "") {
				http.Error(writer, "synthetic pinned GET differs", http.StatusBadRequest)
				return
			}
			call := calls.Add(1)
			if call <= 2 {
				status := http.StatusRequestTimeout
				if call == 2 {
					status = http.StatusTooEarly
				}
				http.Error(writer, "synthetic expected-read outage", status)
				return
			}
			_ = json.NewEncoder(writer).Encode(&sdk.SnPoolClaimResult{Epoch: 7})
		}))
		strategy, api := claimGetHttpTestApi(t, server.URL)
		owners := 0
		hooks := claimGetHttpTestHooks(t, &elapsed, &owners)
		var err error
		var epoch int64
		if pool {
			var result *sdk.SnPoolClaimResult
			result, err = readClaimPoolWithRetry(t.Context(), api, 7, hooks)
			if result != nil {
				epoch = result.Epoch
			}
		} else {
			epoch, err = readClaimEpochWithRetry(t.Context(), strategy, server.URL, api.GetByJwt(), hooks)
		}
		server.Close()
		if err != nil || epoch != 7 || calls.Load() != 3 || mutations.Load() != 0 || owners != 1 || time.Duration(elapsed.Load()) != 150*time.Second {
			t.Fatalf("pool=%t lost retained GET recovery: epoch=%d calls=%d mutations=%d owners=%d elapsed=%s err=%v", pool, epoch, calls.Load(), mutations.Load(), owners, time.Duration(elapsed.Load()), err)
		}
	}
}

// The final actual HTTP refusal and the exact original deadline both survive.
func TestClaimDaemonGetHttpExpiresAtOriginalDeadline(t *testing.T) {
	for _, pool := range []bool{false, true} {
		var calls atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if request.URL.Path == "/hello" {
				return
			}
			calls.Add(1)
			http.Error(writer, "synthetic gateway outage", http.StatusGatewayTimeout)
		}))
		strategy, api := claimGetHttpTestApi(t, server.URL)
		var elapsed atomic.Int64
		owners := 0
		hooks := claimGetHttpTestHooks(t, &elapsed, &owners)
		var err error
		if pool {
			var result *sdk.SnPoolClaimResult
			result, err = readClaimPoolWithRetry(t.Context(), api, 7, hooks)
			if result != nil {
				t.Fatal("expired GET published a pool claim")
			}
		} else {
			var epoch int64
			epoch, err = readClaimEpochWithRetry(t.Context(), strategy, server.URL, api.GetByJwt(), hooks)
			if epoch != 0 {
				t.Fatal("expired GET published an epoch")
			}
		}
		server.Close()
		var status *connect.HttpStatusError
		if !errors.Is(err, context.DeadlineExceeded) || !errors.As(err, &status) || status.StatusCode != http.StatusGatewayTimeout || calls.Load() != 4 || owners != 1 || time.Duration(elapsed.Load()) != 300*time.Second {
			t.Fatalf("pool=%t renewed or lost GET budget: calls=%d owners=%d elapsed=%s err=%v", pool, calls.Load(), owners, time.Duration(elapsed.Load()), err)
		}
	}
}

// Actual authorization and complete malformed JSON responses remain immediate.
func TestClaimDaemonGetHttpRejectsTerminalResponses(t *testing.T) {
	for _, pool := range []bool{false, true} {
		for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusPaymentRequired, http.StatusOK} {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.URL.Path == "/hello" {
					return
				}
				calls.Add(1)
				writer.WriteHeader(status)
				_, _ = io.WriteString(writer, "{")
			}))
			strategy, api := claimGetHttpTestApi(t, server.URL)
			waits := 0
			hooks := claimReadRetryHooks{wait: func(context.Context, time.Duration) error { waits++; return errors.New("unexpected terminal retry") }}
			var err error
			if pool {
				_, err = readClaimPoolWithRetry(t.Context(), api, 7, hooks)
			} else {
				_, err = readClaimEpochWithRetry(t.Context(), strategy, server.URL, api.GetByJwt(), hooks)
			}
			server.Close()
			if err == nil || retryableClaimApiRead(err) || calls.Load() != 1 || waits != 0 {
				t.Fatalf("pool=%t status=%d retried a permanent GET refusal: calls=%d waits=%d err=%v", pool, status, calls.Load(), waits, err)
			}
		}
	}
}

// Cancellation is delivered after a real response has started. The borrowed
// thirty-second caller deadline survives the five-minute retry admission.
func TestClaimDaemonGetHttpCancellationJoinsBothBodies(t *testing.T) {
	for _, pool := range []bool{false, true} {
		entered, released := make(chan struct{}), make(chan struct{})
		var calls atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if request.URL.Path == "/hello" {
				return
			}
			calls.Add(1)
			_, _ = io.WriteString(writer, "{")
			writer.(http.Flusher).Flush()
			close(entered)
			<-request.Context().Done()
			close(released)
		}))
		strategy, api := claimGetHttpTestApi(t, server.URL)
		deadline := time.Now().Add(30 * time.Second)
		ctx, cancel := context.WithDeadline(t.Context(), deadline)
		hooks := claimReadRetryHooks{withTimeout: func(parent context.Context, duration time.Duration) (context.Context, context.CancelFunc) {
			owner, stop := context.WithTimeout(parent, duration)
			if actual, ok := owner.Deadline(); !ok || !actual.Equal(deadline) || duration != 300*time.Second {
				t.Error("GET extended the earlier caller deadline")
			}
			return owner, stop
		}}
		done := make(chan error, 1)
		go func() {
			if pool {
				result, err := readClaimPoolWithRetry(ctx, api, 7, hooks)
				if result != nil {
					t.Error("canceled GET published a pool claim")
				}
				done <- err
			} else {
				epoch, err := readClaimEpochWithRetry(ctx, strategy, server.URL, api.GetByJwt(), hooks)
				if epoch != 0 {
					t.Error("canceled GET published an epoch")
				}
				done <- err
			}
		}()
		select {
		case <-entered:
		case <-time.After(10 * time.Second):
			cancel()
			t.Fatal("real claim GET did not open its body")
		}
		cancel()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatal("GET lost caller cancellation", err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("claim GET did not join canceled body")
		}
		select {
		case <-released:
		case <-time.After(10 * time.Second):
			t.Fatal("server still owns the canceled GET")
		}
		server.Close()
		if calls.Load() != 1 {
			t.Fatal("cancellation admitted another GET", calls.Load())
		}
	}
}

// The public daemon call sites use the new read owner before any semantic
// result or write. Recovery waits here are the actual production pacing.
func TestClaimDaemonGetProductionCallSitesRecoverWithoutSubmission(t *testing.T) {
	for _, operation := range []string{"epoch", "reconcile", "submit"} {
		var calls, mutations atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if request.Method != http.MethodGet {
				mutations.Add(1)
			}
			if request.URL.Path == "/hello" {
				return
			}
			if calls.Add(1) == 1 {
				http.Error(writer, "synthetic retryable timeout status", http.StatusRequestTimeout)
				return
			}
			epoch := int64(7)
			if operation == "submit" {
				epoch++
			}
			_ = json.NewEncoder(writer).Encode(&sdk.SnPoolClaimResult{Epoch: epoch})
		}))
		strategy, api := claimGetHttpTestApi(t, server.URL)
		cfg := &ClaimDaemonConfig{}
		entry := &ClaimQueueEntry{Epoch: 7, Status: "pending"}
		var err error
		if operation == "epoch" {
			var epoch int64
			epoch, err = readClaimEpoch(t.Context(), strategy, server.URL, api.GetByJwt())
			if epoch != 7 {
				t.Error("epoch discovery lost recovered value", epoch)
			}
		} else {
			store := claimRetainedTestStore(t, cfg, entry)
			if operation == "reconcile" {
				var status string
				status, err = reconcileClaimEntry(t.Context(), cfg, api, entry, store)
				if status != "no-claim" {
					t.Error("reconciliation lost recovered zero payout", status)
				}
			} else {
				err = submitClaimDirect(t.Context(), cfg, api, entry, store, nil, nil)
				if err == nil || !strings.Contains(err.Error(), "epoch differs") {
					t.Error("preflight did not retain terminal epoch validation", err)
				}
			}
			if closeErr := store.close(); closeErr != nil {
				t.Error(closeErr)
			}
		}
		server.Close()
		if operation != "submit" && err != nil || calls.Load() != 2 || mutations.Load() != 0 || entry.TxHash != "" || entry.RawTxHex != "" {
			t.Fatalf("%s crossed GET ownership or write boundary: calls=%d mutations=%d entry=%+v err=%v", operation, calls.Load(), mutations.Load(), entry, err)
		}
	}
}
