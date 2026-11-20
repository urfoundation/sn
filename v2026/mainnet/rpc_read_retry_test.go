// Controlled wait and response boundaries qualify throttled reads without
// sleeping through real retry intervals or contacting any external endpoint.
package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Hints are untrusted durations, including duplicate JSON and overflow inputs.
// Exact dates use a fixed clock so these bounds do not depend on test timing.
func TestRpcReadRetryHintsRespectDeadline(t *testing.T) {
	now := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		header string
		body   string
		want   time.Duration
	}{
		{header: "60", body: `{}`, want: time.Minute},
		{header: now.Add(time.Minute).Format(http.TimeFormat), body: `{}`, want: time.Minute},
		{body: `{"retry_after_seconds":60}`, want: time.Minute},
		{header: "15", body: `{"retry_after_seconds":60}`, want: time.Minute},
		{header: "18446744073709551615", body: `{}`, want: 90 * time.Second},
		{body: `{"retry_after_seconds":18446744073709551615}`, want: 90 * time.Second},
		{header: now.Add(time.Hour).Format(http.TimeFormat), body: `{}`, want: 90 * time.Second},
		{header: "invalid", body: `{"retry_after_seconds":-1}`, want: time.Second},
		{header: "-1", body: `{"retry_after_seconds":1.5}`, want: time.Second},
		{header: "0", body: `{"retry_after_seconds":0}`, want: time.Second},
		{body: `{"retry_after_seconds":60,"retry_after_seconds":1}`, want: time.Second},
		{body: `{"retry_after_seconds":60} {}`, want: time.Second},
		{header: now.Add(-time.Hour).Format(http.TimeFormat), body: `not-json`, want: time.Second},
	} {
		header := http.Header{}
		if test.header != "" {
			header.Set("Retry-After", test.header)
		}
		if got := rpcReadRetryDelay(header, []byte(test.body), time.Second, now, now.Add(90*time.Second)); got != test.want {
			t.Fatalf("hint header=%q body=%q: delay=%v want=%v", test.header, test.body, got, test.want)
		}
	}
	if got := rpcReadRetryDelay(http.Header{}, nil, time.Second, now, now.Add(-time.Second)); got != 0 {
		t.Fatal("expired deadline gained a delay", got)
	}
}

// A blocked wait owns the retry: even a ready success response cannot be read
// until the wait boundary releases. Both throttling and service recovery use it.
func TestRpcReadRetryHonors429And503BeforeAnotherRequest(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		client, err := newRpcClient("http://rpc.example", 2*time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		var calls atomic.Int32
		client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if calls.Add(1) == 1 {
				header, body := http.Header{}, `{"retry_after_seconds":60}`
				if status == http.StatusServiceUnavailable {
					header.Set("Retry-After", "60")
					body = "temporarily unavailable"
				}
				return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body))}, nil
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"jsonrpc":"2.0","id":1,"result":"synthetic-chain"}`))}, nil
		})
		entered, release := make(chan time.Duration, 1), make(chan struct{})
		client.retryWait = func(ctx context.Context, delay time.Duration) error {
			entered <- delay
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-release:
				return nil
			}
		}
		finished := make(chan error, 1)
		go func() {
			var observed string
			err := client.call(t.Context(), "system_chain", []any{}, &observed)
			if err == nil && observed != "synthetic-chain" {
				err = errors.New("retried response lost its exact value")
			}
			finished <- err
		}()
		delay := <-entered
		if delay != time.Minute || calls.Load() != 1 {
			close(release)
			<-finished
			t.Fatalf("HTTP %d retried before its bounded wait: delay=%v calls=%d", status, delay, calls.Load())
		}
		close(release)
		if err := <-finished; err != nil || calls.Load() != 2 {
			t.Fatalf("HTTP %d retry failed: calls=%d err=%v", status, calls.Load(), err)
		}
	}
}

// Cancellation occurs at the wait barrier and must prevent the next RPC,
// even when the server asks for an interval longer than the total read window.
func TestRpcReadRetryCancellationStops429WithoutAnotherRequest(t *testing.T) {
	client, err := newRpcClient("http://rpc.example", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var calls atomic.Int32
	client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{"Retry-After": []string{"3600"}}, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	})
	client.retryWait = func(waitCtx context.Context, delay time.Duration) error {
		if deadline, ok := waitCtx.Deadline(); !ok || delay <= 0 || delay > time.Minute || delay > time.Until(deadline)+time.Second {
			t.Errorf("remote delay exceeded original window: %v", delay)
		}
		cancel()
		return waitRpcReadRetry(waitCtx, delay)
	}
	var observed string
	err = client.call(ctx, "system_chain", []any{}, &observed)
	if !errors.Is(err, context.Canceled) || calls.Load() != 1 || observed != "" {
		t.Fatalf("cancellation retried or lost its cause: calls=%d value=%q err=%v", calls.Load(), observed, err)
	}
}

// Transport timeout and ordinary gateway responses keep the original bounded
// backoff; retry support does not turn permanent failures into retry loops.
func TestRpcReadRetryKeepsTimeoutBackoffAndPermanentFailure(t *testing.T) {
	for _, status := range []int{http.StatusRequestTimeout, http.StatusBadGateway, http.StatusGatewayTimeout, http.StatusForbidden} {
		client, err := newRpcClient("http://rpc.example", time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		calls, waits := 0, 0
		client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("unavailable"))}, nil
		})
		client.retryWait = func(waitCtx context.Context, delay time.Duration) error {
			waits++
			if delay != 500*time.Millisecond {
				t.Errorf("HTTP %d changed fallback delay: %v", status, delay)
			}
			cancel()
			return waitCtx.Err()
		}
		var observed string
		err = client.call(ctx, "system_chain", []any{}, &observed)
		cancel()
		if calls != 1 || err == nil || status == http.StatusForbidden && waits != 0 || status != http.StatusForbidden && (waits != 1 || !errors.Is(err, context.Canceled)) {
			t.Fatalf("HTTP %d changed failure semantics: calls=%d waits=%d err=%v", status, calls, waits, err)
		}
	}
}

// A transport-level attempt timeout retains its cause through the bounded
// retry wait; canceling the total operation does not dispatch another attempt.
func TestRpcReadRetryRetainsTransportTimeoutCause(t *testing.T) {
	client, err := newRpcClient("http://rpc.example", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	calls := 0
	client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		return nil, context.DeadlineExceeded
	})
	client.retryWait = func(waitCtx context.Context, delay time.Duration) error {
		if delay != 500*time.Millisecond {
			t.Errorf("transport timeout changed fallback delay: %v", delay)
		}
		cancel()
		return waitCtx.Err()
	}
	var observed string
	err = client.call(ctx, "system_chain", []any{}, &observed)
	if calls != 1 || !errors.Is(err, context.Canceled) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("transport timeout lost its cause or retried: calls=%d err=%v", calls, err)
	}
}
