package miner

// The minute-long outage clock advances only after completed real SDK reads.
// Local HTTP bodies, JSON parsing, command verification and cancellation stay real.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/docopt/docopt-go"
	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/sdk/v2026"
)

// Use the real native HTTP strategy without route racing or inner pauses so
// each completed SDK read is one independently observed request.
func finiteClaimTestHooks() finiteClaimHooks {
	settings := connect.DefaultClientStrategySettings()
	settings.EnableResilient = false
	settings.GetRetryCount = 0
	settings.RequestTimeout = 2 * time.Second
	settings.ConnectTimeout = time.Second
	return finiteClaimHooks{strategySettings: settings}
}

func TestFiniteClaimCommandRecoversMinuteOutagesInBothReads(t *testing.T) {
	setTestProviderJwt(t)
	var elapsed, poolStart atomic.Int64
	poolStart.Store(-1)
	var epochs, claims, mutations atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			mutations.Add(1)
			http.Error(w, "unexpected mutation", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path == "/hello" {
			return
		}
		switch r.URL.Path {
		case "/sn/epoch":
			epochs.Add(1)
			if time.Duration(elapsed.Load()) < 65*time.Second {
				http.Error(w, "synthetic epoch outage", http.StatusServiceUnavailable)
				return
			}
			_ = json.NewEncoder(w).Encode(&sdk.SnEpochResult{Epoch: 2})
		case "/sn/pool/claim":
			claims.Add(1)
			poolStart.CompareAndSwap(-1, elapsed.Load())
			if time.Duration(elapsed.Load()-poolStart.Load()) < 65*time.Second {
				http.Error(w, "synthetic claim outage", http.StatusBadGateway)
				return
			}
			_ = json.NewEncoder(w).Encode(finiteClaimTestResult())
		default:
			http.Error(w, "unexpected route", http.StatusNotFound)
		}
	}))
	defer server.Close()
	hooks := finiteClaimTestHooks()
	budgets := []time.Duration{}
	hooks.retry.withTimeout = func(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
		budgets = append(budgets, timeout)
		return context.WithTimeout(ctx, timeout)
	}
	hooks.retry.wait = func(ctx context.Context, delay time.Duration) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if delay < 2*time.Second || delay >= 3*time.Second {
			t.Fatal("retry delay no longer preserves minute outage headroom", delay)
		}
		elapsed.Add(int64(delay))
		return nil
	}
	err := runFiniteClaim(t.Context(), docopt.Opts{"--api_url": server.URL}, hooks)
	if err != nil || len(budgets) != 2 || budgets[0] != 300*time.Second || budgets[1] != 300*time.Second || epochs.Load() < 2 || claims.Load() < 2 || mutations.Load() != 0 || time.Duration(elapsed.Load()) < 130*time.Second {
		t.Fatal("complete finite command did not retain both minute-outage read owners", err, budgets, epochs.Load(), claims.Load(), elapsed.Load())
	}
}

// The declared finite budget ends the original request series, not a renewed
// deadline at each retry. Both observed status and deadline remain inspectable.
func TestFiniteClaimReadBudgetExpiresAtOriginalDeadline(t *testing.T) {
	elapsed, reads := time.Duration(0), 0
	owner := &ethRpcTestDeadline{Context: t.Context(), deadline: time.Now().Add(300 * time.Second), done: make(chan struct{})}
	hooks := claimReadRetryHooks{
		withTimeout: func(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
			if timeout != 300*time.Second {
				t.Fatal("wrong finite command budget", timeout)
			}
			return owner, owner.finish
		},
		wait: func(ctx context.Context, delay time.Duration) error {
			elapsed += delay
			if elapsed >= 300*time.Second {
				owner.finish()
				return ctx.Err()
			}
			return nil
		},
	}
	_, err := retryClaimApiRead(t.Context(), hooks, func(ctx context.Context) (string, error) {
		reads++
		if ctx != owner {
			t.Fatal("retry replaced its deadline owner")
		}
		return "", &connect.HttpStatusError{StatusCode: 503}
	})
	var status *connect.HttpStatusError
	if !errors.Is(err, context.DeadlineExceeded) || !errors.As(err, &status) || elapsed < 300*time.Second || elapsed >= 303*time.Second || reads < 100 || reads >= claimReadMaximumAttempts {
		t.Fatal("finite read renewed or lost its deadline", reads, elapsed, err)
	}
}

// Typed transport errors can retry; parser, credential, local-file and
// integrity errors remain terminal even when joined to a transient cause.
func TestFiniteClaimReadClassifiesEveryJoinedCause(t *testing.T) {
	transient := &connect.HttpStatusError{StatusCode: 503}
	for _, err := range []error{transient, &connect.HttpStatusError{StatusCode: 429}, &url.Error{Op: "Get", URL: "https://api.example", Err: syscall.ECONNRESET}, errors.Join(transient, context.DeadlineExceeded)} {
		if !retryableClaimApiRead(err) {
			t.Error("lost typed read recovery", err)
		}
	}
	for _, err := range []error{nil, context.Canceled, io.EOF, &json.SyntaxError{}, &connect.HttpStatusError{StatusCode: 401}, &connect.HttpStatusError{StatusCode: 403}, errors.Join(transient, &connect.HttpStatusError{StatusCode: 402}), errors.Join(transient, errors.New("synthetic identity refusal")), errors.Join(transient, &json.SyntaxError{})} {
		if retryableClaimApiRead(err) {
			t.Error("hard read refusal was retried", err)
		}
	}
}

// The actual SDK parser/status boundary must not turn terminal bodies into
// repeated network reads. No test swaps the API object for a fake decoder.
func TestFiniteClaimCommandRejectsTerminalResponsesWithoutRetry(t *testing.T) {
	setTestProviderJwt(t)
	for _, response := range []struct {
		status int
		body   string
	}{
		{status: 401, body: "unauthorized"}, {status: 403, body: "forbidden"}, {status: 402, body: "payment required"}, {status: 404, body: "missing"},
		{status: 200, body: "{"}, {status: 200, body: "null"}, {status: 200, body: `{"error":{"message":"not available"}}`}, {status: 200, body: `{"epoch":2}`},
	} {
		var reads, mutations atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				mutations.Add(1)
			}
			if r.URL.Path == "/hello" {
				return
			}
			reads.Add(1)
			w.WriteHeader(response.status)
			_, _ = io.WriteString(w, response.body)
		}))
		hooks := finiteClaimTestHooks()
		waits := 0
		hooks.retry.wait = func(context.Context, time.Duration) error { waits++; return errors.New("unexpected wait") }
		err := runFiniteClaim(t.Context(), docopt.Opts{"--api_url": server.URL, "--epoch": "1"}, hooks)
		server.Close()
		if err == nil || reads.Load() != 1 || mutations.Load() != 0 || waits != 0 {
			t.Fatalf("terminal SDK response was retried: status=%d body=%q reads=%d waits=%d err=%v", response.status, response.body, reads.Load(), waits, err)
		}
	}
}

func TestFiniteClaimCommandCanceledBeforeEffects(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := runFiniteClaim(ctx, nil, finiteClaimHooks{})
	if !errors.Is(err, context.Canceled) {
		t.Fatal("canceled command read config or credentials first", err)
	}
}

func TestFiniteClaimCommandCancellationJoinsActiveSdkRead(t *testing.T) {
	setTestProviderJwt(t)
	entered, released := make(chan struct{}), make(chan struct{})
	var reads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/hello" {
			return
		}
		if reads.Add(1) == 1 {
			close(entered)
			<-r.Context().Done()
			close(released)
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- runFiniteClaim(ctx, docopt.Opts{"--api_url": server.URL, "--epoch": "1"}, finiteClaimTestHooks())
	}()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("real SDK read did not enter")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("finite command lost cancellation", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("finite command did not join its canceled SDK")
	}
	select {
	case <-released:
	case <-time.After(10 * time.Second):
		t.Fatal("canceled request body remained live")
	}
	if reads.Load() != 1 {
		t.Fatal("cancellation started another read", reads.Load())
	}
}

func TestFiniteClaimReadCanceledDuringRetryWait(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	reads := 0
	hooks := claimReadRetryHooks{wait: func(ctx context.Context, delay time.Duration) error { cancel(); return ctx.Err() }}
	_, err := retryClaimApiRead(ctx, hooks, func(context.Context) (string, error) { reads++; return "", &connect.HttpStatusError{StatusCode: 503} })
	if !errors.Is(err, context.Canceled) || reads != 1 || strings.Contains(err.Error(), "deadline") {
		t.Fatal("canceled wait restarted its read", reads, err)
	}
}
