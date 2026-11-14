// The production parsers and real GET routes execute before controlled waits.
// Clock acceleration represents elapsed budget, never wall-clock qualification.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

type monitorProgressReadFixture struct {
	body []byte
	read func(context.Context, *http.Client, string, monitorProgressReadClock) (bool, string)
}

func newMonitorProgressReadFixture(t *testing.T, kind string) monitorProgressReadFixture {
	t.Helper()
	now := time.Now().UTC()
	if kind == "provider" {
		value := monitorProviderTestValue(now)
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return monitorProgressReadFixture{body: raw, read: func(ctx context.Context, client *http.Client, endpoint string, clock monitorProgressReadClock) (bool, string) {
			policy := monitorProviderTestPolicy(value, endpoint+"/provider-progress")
			got, code := readMonitorProviderWithBudget(ctx, client, policy, defaultMonitorProgressReadBudget, clock)
			return got != nil, code
		}}
	}
	value := monitorClaimTestValue(now)
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return monitorProgressReadFixture{body: raw, read: func(ctx context.Context, client *http.Client, endpoint string, clock monitorProgressReadClock) (bool, string) {
		policy := monitorClaimTestPolicy(t, value, endpoint+"/claim-progress", now)
		got, code := readMonitorClaimWithBudget(ctx, client, policy, defaultMonitorProgressReadBudget, clock)
		return got != nil, code
	}}
}

func TestMonitorProgressTransientStatusesUseOwnedRetryAfter(t *testing.T) {
	for _, kind := range []string{"provider", "claim"} {
		for _, status := range []int{408, 429, 500, 502, 503, 504} {
			fixture := newMonitorProgressReadFixture(t, kind)
			var calls atomic.Int32
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				if request.Method != http.MethodGet {
					t.Error("unexpected non-GET progress request")
				}
				if calls.Add(1) == 1 {
					w.Header().Set("Retry-After", "60")
					w.WriteHeader(status)
					return
				}
				_, _ = w.Write(fixture.body)
			}))
			entered, release := make(chan time.Duration, 1), make(chan struct{})
			clock := monitorProgressReadClock{wait: func(ctx context.Context, delay time.Duration) error {
				entered <- delay
				select {
				case <-release:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}}
			done := make(chan string, 1)
			go func() {
				got, code := fixture.read(t.Context(), newMonitorProviderClient(), source.URL, clock)
				if !got && code == "ok" {
					code = "absent"
				}
				done <- code
			}()
			select {
			case delay := <-entered:
				if delay != time.Minute || calls.Load() != 1 {
					t.Error("transient response bypassed its bounded pacing", kind, status, delay, calls.Load())
				}
			case code := <-done:
				source.Close()
				t.Fatal("transient progress response did not retry", kind, status, code)
			case <-time.After(10 * time.Second):
				t.Fatal("progress retry wait not reached", kind, status)
			}
			close(release)
			code := <-done
			source.Close()
			if code != "ok" || calls.Load() != 2 {
				t.Fatal("actual progress GET did not recover", kind, status, code, calls.Load())
			}
		}
	}
}

func TestMonitorProgressRetryKeepsSingleBudgetBeyondSixtySeconds(t *testing.T) {
	for _, kind := range []string{"provider", "claim"} {
		fixture := newMonitorProgressReadFixture(t, kind)
		var calls atomic.Int32
		source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if calls.Add(1) <= 2 {
				w.WriteHeader(503)
				return
			}
			_, _ = w.Write(fixture.body)
		}))
		started, waits := time.Now(), 0
		syntheticNow := started
		var originalDeadline time.Time
		clock := monitorProgressReadClock{now: func() time.Time { return syntheticNow }, wait: func(ctx context.Context, _ time.Duration) error {
			deadline, present := ctx.Deadline()
			if !present || deadline.Sub(started) < 299*time.Second || deadline.Sub(started) > 301*time.Second {
				return errors.New("default caller budget is not300seconds")
			}
			if waits != 0 && deadline != originalDeadline {
				return errors.New("progress retry reset original deadline")
			}
			originalDeadline = deadline
			waits++
			syntheticNow = syntheticNow.Add(65 * time.Second)
			return nil
		}}
		got, code := fixture.read(t.Context(), newMonitorProviderClient(), source.URL, clock)
		source.Close()
		if !got || code != "ok" || waits != 2 || calls.Load() != 3 || syntheticNow.Sub(started) != 130*time.Second {
			t.Fatal("progress GET stopped below owned transient budget or reset it", kind, code, waits, calls.Load())
		}
	}
}

func TestMonitorProgressBudgetExhaustionDoesNotInventSample(t *testing.T) {
	for _, kind := range []string{"provider", "claim"} {
		fixture := newMonitorProgressReadFixture(t, kind)
		var calls atomic.Int32
		source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			calls.Add(1)
			w.Header().Set("Retry-After", "3600")
			w.WriteHeader(429)
		}))
		now := time.Now()
		clock := monitorProgressReadClock{now: func() time.Time { return now }, wait: func(ctx context.Context, delay time.Duration) error {
			deadline, _ := ctx.Deadline()
			if delay <= 0 || delay > 300*time.Second || delay > deadline.Sub(now) {
				return errors.New("remote Retry-After exceeded the owner deadline")
			}
			now = now.Add(301 * time.Second)
			return nil
		}}
		got, code := fixture.read(t.Context(), newMonitorProviderClient(), source.URL, clock)
		source.Close()
		if got || code != "unavailable" || calls.Load() != 1 {
			t.Fatal("exhausted progress sample gained another read or invented data", kind, got, code, calls.Load())
		}
	}
}

func TestMonitorProgressRetryCancellationJoinsWithoutNextRequest(t *testing.T) {
	for _, kind := range []string{"provider", "claim"} {
		fixture := newMonitorProgressReadFixture(t, kind)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		var calls atomic.Int32
		source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			calls.Add(1)
			w.Header().Set("Retry-After", "60")
			w.WriteHeader(429)
		}))
		entered := make(chan struct{})
		clock := monitorProgressReadClock{wait: func(waitCtx context.Context, delay time.Duration) error {
			close(entered)
			return waitRpcReadRetry(waitCtx, delay)
		}}
		done := make(chan string, 1)
		go func() { _, code := fixture.read(ctx, newMonitorProviderClient(), source.URL, clock); done <- code }()
		select {
		case <-entered:
		case <-time.After(10 * time.Second):
			cancel()
			t.Fatal("owned retry not reached")
		}
		cancel()
		select {
		case code := <-done:
			if code != "unavailable" {
				t.Error(code)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("canceled progress read did not join")
		}
		source.Close()
		if calls.Load() != 1 {
			t.Fatal("canceled progress read issued another request", kind, calls.Load())
		}
	}
}

type monitorProgressCloseBody struct {
	*bytes.Reader
	closes *atomic.Int32
	err    error
}

func (self *monitorProgressCloseBody) Close() error { self.closes.Add(1); return self.err }

func TestMonitorProgressResponseBodiesCloseBeforeRetry(t *testing.T) {
	for _, kind := range []string{"provider", "claim"} {
		fixture := newMonitorProgressReadFixture(t, kind)
		var closes, calls atomic.Int32
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			err := error(nil)
			if calls.Add(1) == 1 {
				err = syscall.EIO
			}
			return &http.Response{StatusCode: 200, Body: &monitorProgressCloseBody{Reader: bytes.NewReader(fixture.body), closes: &closes, err: err}}, nil
		})}
		waits := 0
		clock := monitorProgressReadClock{wait: func(context.Context, time.Duration) error {
			waits++
			if closes.Load() != 1 || calls.Load() != 1 {
				return errors.New("progress body was not joined before wait")
			}
			return nil
		}}
		got, code := fixture.read(t.Context(), client, "https://synthetic.invalid", clock)
		if !got || code != "ok" || waits != 1 || calls.Load() != 2 || closes.Load() != 2 {
			t.Fatal("failed body-close produced a fresh sample or leaked a body", kind, got, code, waits, calls.Load(), closes.Load())
		}
	}
}

func TestMonitorProgressPermanentResponsesNeverRetry(t *testing.T) {
	for _, kind := range []string{"provider", "claim"} {
		for _, status := range []int{400, 401, 403, 404, 422} {
			fixture := newMonitorProgressReadFixture(t, kind)
			var calls, closes atomic.Int32
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls.Add(1)
				return &http.Response{StatusCode: status, Header: http.Header{"Retry-After": []string{"60"}}, Body: &monitorProgressCloseBody{Reader: bytes.NewReader(nil), closes: &closes, err: syscall.EIO}}, nil
			})}
			waits := 0
			got, code := fixture.read(t.Context(), client, "https://synthetic.invalid", monitorProgressReadClock{wait: func(context.Context, time.Duration) error { waits++; return context.DeadlineExceeded }})
			want := "invalid"
			if status == 401 || status == 403 {
				want = "authentication"
			}
			if got || code != want || calls.Load() != 1 || closes.Load() != 1 || waits != 0 {
				t.Fatal("permanent progress refusal became a transient retry", kind, status, got, code, waits)
			}
		}
	}
}

func TestMonitorProgressEarlyTransportOutageRetainsOwnerBudget(t *testing.T) {
	for _, kind := range []string{"provider", "claim"} {
		fixture := newMonitorProgressReadFixture(t, kind)
		for _, cause := range []error{io.EOF, syscall.ECONNRESET, context.DeadlineExceeded} {
			calls, waits := 0, 0
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					return nil, cause
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(fixture.body))}, nil
			})}
			got, code := fixture.read(t.Context(), client, "https://synthetic.invalid", monitorProgressReadClock{wait: func(context.Context, time.Duration) error { waits++; return nil }})
			if !got || code != "ok" || calls != 2 || waits != 1 {
				t.Fatal("early read transport outage escaped its owner budget", kind, cause, code, calls, waits)
			}
		}
	}
}

func TestMonitorProgressJoinedPermanentTransportCauseDoesNotBorrowTimeout(t *testing.T) {
	for _, kind := range []string{"provider", "claim"} {
		for _, hard := range []error{context.Canceled, errors.New("synthetic permanent transport refusal")} {
			fixture := newMonitorProgressReadFixture(t, kind)
			calls, waits := 0, 0
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					return nil, fmt.Errorf("read progress: %w", errors.Join(context.DeadlineExceeded, hard))
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(fixture.body))}, nil
			})}
			got, code := fixture.read(t.Context(), client, "https://monitor.example", monitorProgressReadClock{wait: func(context.Context, time.Duration) error { waits++; return nil }})
			if got || code != "unavailable" || calls != 1 || waits != 0 {
				t.Fatal("joined hard or canceled read borrowed transient retry permission", kind, hard, got, code, calls, waits)
			}
		}
	}
}

func TestMonitorProgressCompleteMalformedBodyDominatesCloseOutage(t *testing.T) {
	for _, kind := range []string{"provider", "claim"} {
		fixture := newMonitorProgressReadFixture(t, kind)
		var closes atomic.Int32
		calls, waits := 0, 0
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: 200, Body: &monitorProgressCloseBody{Reader: bytes.NewReader([]byte(`{"schema":"foreign"}`)), closes: &closes, err: syscall.EIO}}, nil
		})}
		got, code := fixture.read(t.Context(), client, "https://monitor.example", monitorProgressReadClock{wait: func(context.Context, time.Duration) error { waits++; return context.DeadlineExceeded }})
		if got || code != "invalid" || calls != 1 || closes.Load() != 1 || waits != 0 {
			t.Fatal("complete malformed reply became retryable after body close", kind, got, code, calls, closes.Load(), waits)
		}
	}
}
