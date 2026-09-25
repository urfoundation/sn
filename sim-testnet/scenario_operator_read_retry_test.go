// Deterministic operator read tests exercise real request ownership while
// virtual time forces timeout, cancellation and recovery transitions.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

// Both the request child and the copied client must permit a full minute. The
// recovered response takes longer than the original shared client's timeout.
func TestScenarioOperatorReadRetryFreshAttemptAndLongResponse(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var first context.Context
		calls := 0
		client := &http.Client{Timeout: 30 * time.Second, Transport: scenarioOperatorTestTransport(func(request *http.Request) (*http.Response, error) {
			calls++
			if request.Method != http.MethodGet || request.URL.String() != "http://operator.example/verify/stats?limit=100000" {
				return nil, errors.New("retry changed its read identity")
			}
			deadline, ok := request.Context().Deadline()
			if !ok || time.Until(deadline) != 60*time.Second || request.Context().Err() != nil {
				return nil, fmt.Errorf("attempt lacks a fresh full minute: deadline=%s err=%v", time.Until(deadline), request.Context().Err())
			}
			if calls == 1 {
				first = request.Context()
				<-first.Done()
				return nil, first.Err()
			}
			if calls != 2 || first.Err() == nil || request.Context() == first {
				return nil, errors.New("retry reused an expired attempt or repeated success")
			}
			select {
			case <-time.After(45 * time.Second):
			case <-request.Context().Done():
				return nil, request.Context().Err()
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("fresh counters"))}, nil
		})}
		probe := &liveScenarioProbe{client: client}
		started := time.Now()
		result := probe.readOperatorSurface(t.Context(), "http://operator.example/verify/stats?limit=100000", 1024)
		if result.err != nil || string(result.data) != "fresh counters" || result.attempts != 2 || result.transientFailures != 1 || time.Since(started) != 105*time.Second+250*time.Millisecond || client.Timeout != 30*time.Second {
			t.Fatalf("fresh read budget or shared client changed: %+v elapsed=%s client=%s", result, time.Since(started), client.Timeout)
		}
	})
}

// Slow failures consume exactly one five-minute owner, never five minutes per
// attempt and never a new enclosing budget after expiry.
func TestScenarioOperatorReadRetryExhaustsOneOperationBudget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := 0
		probe := &liveScenarioProbe{client: &http.Client{Transport: scenarioOperatorTestTransport(func(request *http.Request) (*http.Response, error) {
			calls++
			deadline, ok := request.Context().Deadline()
			if !ok || time.Until(deadline) > 60*time.Second || time.Until(deadline) <= 0 {
				return nil, errors.New("attempt escaped the remaining operation budget")
			}
			<-request.Context().Done()
			return nil, request.Context().Err()
		})}}
		started := time.Now()
		result := probe.readOperatorSurface(t.Context(), "http://operator.example/verify/proofs?limit=10000", 1024)
		if !errors.Is(result.err, context.DeadlineExceeded) || result.data != nil || calls != 5 || result.attempts != 5 || result.transientFailures != 5 || time.Since(started) != 300*time.Second {
			t.Fatalf("operation retry budget differs: %+v calls=%d elapsed=%s", result, calls, time.Since(started))
		}
	})
}

// Only the failed surface retries. A concurrently completed response remains
// owned by its original operator, and attempts remain visible in observations.
func TestScenarioOperatorReadRetryRetainsCompletedSurfaceOwners(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var stateLock sync.Mutex
		calls := map[string]int{}
		probe := &liveScenarioProbe{cfg: testResolvedConfig(t), stateDir: t.TempDir(), client: &http.Client{Transport: scenarioOperatorTestTransport(func(request *http.Request) (*http.Response, error) {
			endpoint := request.URL.String()
			attempt := func() int {
				stateLock.Lock()
				defer stateLock.Unlock()
				calls[endpoint]++
				return calls[endpoint]
			}()
			if request.URL.Host == "operator-two.example" && request.URL.Path == "/verify/stats" && attempt == 1 {
				return nil, context.DeadlineExceeded
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(endpoint))}, nil
		})}}
		bases := []string{"http://operator-one.example", "http://operator-two.example"}
		responses := probe.readOperatorSurfaces(t.Context(), bases)
		paths := []string{"/status", "/verify/keys", "/verify/stats?limit=100000", "/verify/proofs?limit=10000"}
		for operator, surfaces := range responses {
			for surface, result := range surfaces {
				endpoint := bases[operator] + paths[surface]
				want := uint64(1)
				if operator == 1 && surface == scenarioOperatorStats {
					want = 2
				}
				if result.err != nil || string(result.data) != endpoint || result.attempts != want || result.transientFailures != want-1 || calls[endpoint] != int(want) {
					t.Fatalf("completed response was retried or changed owner: %s %+v calls=%d", endpoint, result, calls[endpoint])
				}
			}
			observation := probe.inspectOperatorWithSurfaces(t.Context(), nil, operator+1, "", bases[operator], nil, surfaces)
			want := uint64(scenarioOperatorSurfaceCount + operator)
			if observation.SurfaceReadAttempts != want || observation.SurfaceReadTransientFailures != uint64(operator) {
				t.Fatalf("observation lost retry accounting: attempts=%d transient=%d", observation.SurfaceReadAttempts, observation.SurfaceReadTransientFailures)
			}
		}
	})
}

// Availability statuses do not permit redirecting a request or concealing a
// malformed response. Mixed failures are terminal regardless of nesting.
func TestScenarioOperatorReadRetryRejectsPermanentAndMixedFailures(t *testing.T) {
	for _, failure := range []error{
		context.Canceled,
		errors.New("synthetic invalid artifact mentioning timeout"),
		&os.PathError{Op: "read", Path: "synthetic.json", Err: context.DeadlineExceeded},
		&evidenceRequestStatusError{status: http.StatusUnauthorized},
		&evidenceRequestStatusError{status: http.StatusNotFound},
		&evidenceRequestStatusError{status: http.StatusTemporaryRedirect},
		&evidenceRequestStatusError{status: http.StatusInternalServerError},
		errors.Join(context.DeadlineExceeded, errors.New("synthetic signature mismatch")),
		errors.Join(&evidenceRequestStatusError{status: http.StatusServiceUnavailable}, errors.New("synthetic body limit")),
		fmt.Errorf("outer: %w", errors.Join(context.DeadlineExceeded, errors.New("synthetic policy mismatch"))),
	} {
		if scenarioOperatorReadTransient(failure) {
			t.Errorf("permanent/mixed failure authorized retry: %v", failure)
		}
	}
	for _, status := range []int{http.StatusRequestTimeout, http.StatusTooEarly, http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout} {
		if !scenarioOperatorReadTransient(&evidenceRequestStatusError{status: status}) {
			t.Errorf("explicit transient status refused: %d", status)
		}
	}
}

// A complete response with bad bytes is returned once to its strict decoder;
// status, body capacity and close failures cannot turn into eventual success.
func TestScenarioOperatorReadRetryKeepsResponseValidationStrict(t *testing.T) {
	for _, sample := range []struct {
		status    int
		body      string
		limit     int64
		wantError bool
	}{
		{status: http.StatusOK, body: "malformed evidence", limit: 128},
		{status: http.StatusOK, body: "oversized evidence", limit: 4, wantError: true},
		{status: http.StatusForbidden, body: "refused", limit: 128, wantError: true},
		{status: http.StatusTemporaryRedirect, body: "redirect", limit: 128, wantError: true},
	} {
		calls := 0
		probe := &liveScenarioProbe{client: &http.Client{Transport: scenarioOperatorTestTransport(func(request *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: sample.status, Header: http.Header{"Location": []string{"http://foreign.example/changed"}}, Body: io.NopCloser(strings.NewReader(sample.body))}, nil
		})}}
		result := probe.readOperatorSurface(t.Context(), "http://operator.example/verify/keys", sample.limit)
		if calls != 1 || result.attempts != 1 || result.transientFailures != 0 || (result.err != nil) != sample.wantError {
			t.Fatalf("response validation changed: sample=%+v result=%+v calls=%d", sample, result, calls)
		}
	}
}

// Cancellation during a retry delay consumes no further request. A shorter
// parent deadline still governs the complete operation and all its children.
func TestScenarioOperatorReadRetryPreservesParentCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		var calls atomic.Int32
		probe := &liveScenarioProbe{client: &http.Client{Transport: scenarioOperatorTestTransport(func(request *http.Request) (*http.Response, error) {
			calls.Add(1)
			return nil, context.DeadlineExceeded
		})}}
		done := make(chan scenarioOperatorRead, 1)
		go func() { done <- probe.readOperatorSurface(ctx, "http://operator.example/status", 1024) }()
		synctest.Wait()
		cancel()
		result := <-done
		if !errors.Is(result.err, context.Canceled) || result.data != nil || calls.Load() != 1 {
			t.Fatalf("canceled delay admitted another request: %+v calls=%d", result, calls.Load())
		}
		ctx, cancel = context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		probe.client.Transport = scenarioOperatorTestTransport(func(request *http.Request) (*http.Response, error) {
			<-request.Context().Done()
			return nil, request.Context().Err()
		})
		started := time.Now()
		result = probe.readOperatorSurface(ctx, "http://operator.example/status", 1024)
		if !errors.Is(result.err, context.DeadlineExceeded) || result.attempts != 1 || time.Since(started) != 10*time.Second {
			t.Fatalf("retry extended parent deadline: %+v elapsed=%s", result, time.Since(started))
		}
	})
}

// A close failure belongs to the same attempt as its status, so an apparent
// availability response cannot conceal a simultaneous integrity failure.
type scenarioOperatorRetryCloseBody struct {
	io.Reader
	close func() error
}

func (self *scenarioOperatorRetryCloseBody) Close() error { return self.close() }

// Response close can reveal failure or cancel the request after all bytes have
// arrived. Neither ordering permits retrying integrity or accepting late bytes.
func TestScenarioOperatorReadRetryRejectsCloseFailureAndLateSuccess(t *testing.T) {
	for _, cancelOnClose := range []bool{false, true} {
		ctx, cancel := context.WithCancel(t.Context())
		calls, closes := 0, 0
		failure := errors.New("synthetic response integrity failure")
		probe := &liveScenarioProbe{client: &http.Client{Transport: scenarioOperatorTestTransport(func(request *http.Request) (*http.Response, error) {
			calls++
			status := http.StatusServiceUnavailable
			if cancelOnClose {
				status = http.StatusOK
			}
			body := &scenarioOperatorRetryCloseBody{Reader: strings.NewReader("unaccepted bytes"), close: func() error {
				closes++
				if cancelOnClose {
					cancel()
					return nil
				}
				return errors.Join(context.DeadlineExceeded, failure)
			}}
			return &http.Response{StatusCode: status, Body: body}, nil
		})}}
		result := probe.readOperatorSurface(ctx, "http://operator.example/verify/proofs?limit=10000", 1024)
		cancel()
		if calls != 1 || closes != 1 || result.data != nil || result.err == nil || result.transientFailures != 0 || cancelOnClose && !errors.Is(result.err, context.Canceled) || !cancelOnClose && !errors.Is(result.err, failure) {
			t.Fatalf("close/lifecycle failure became retryable success: cancel=%t result=%+v calls=%d closes=%d", cancelOnClose, result, calls, closes)
		}
	}
}

// Transport recovery never retries a syntactically successful bad surface, and
// projection keeps absent counters at zero until the next independent snapshot.
func TestScenarioOperatorReadRetryPreservesMalformedProjection(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := testResolvedConfig(t)
		var statsCalls atomic.Int32
		probe := &liveScenarioProbe{cfg: cfg, stateDir: t.TempDir(), client: &http.Client{Transport: scenarioOperatorTestTransport(func(request *http.Request) (*http.Response, error) {
			body := "{}"
			switch request.URL.Path {
			case "/verify/keys":
				body = `{"keys":[{"server_key_id":1,"public_key":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="}]}`
			case "/verify/stats":
				attempt := statsCalls.Add(1)
				if attempt == 1 {
					return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader("temporary outage"))}, nil
				}
				body = `{"schema":"foreign-stats","rows":[{"assignments":999,"confirmations":999}]}`
			case "/verify/proofs":
				body = `{"schema":"urnetwork-verify-proof-index-v1","rows":[{"server_key_id":1}]}`
			case "/sn/artifacts":
				body = `{"schema":"urnetwork-payout-artifact-history-v1","objects":[]}`
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
		})}}
		surfaces := probe.readOperatorSurfaces(t.Context(), []string{"http://operator.example"})
		observation := probe.inspectOperatorWithSurfaces(t.Context(), nil, 1, "", "http://operator.example", nil, surfaces[0])
		if !strings.Contains(observation.Error, "stats: invalid schema") || observation.Assignments != 0 || observation.Confirmations != 0 || observation.StatsRows != 0 || observation.ProofRows != 1 || statsCalls.Load() != 2 || observation.SurfaceReadAttempts != 5 || observation.SurfaceReadTransientFailures != 1 {
			t.Fatalf("transport recovery bypassed strict surface projection: %+v calls=%d", observation, statsCalls.Load())
		}
	})
}
