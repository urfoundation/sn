package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestPublicEVMHistoricalWorkCooldownFloor(t *testing.T) {
	const historical = `{"jsonrpc":"2.0","error":{"message":"Historical work rate limit exceeded"},"id":1}`
	for _, fixture := range []struct {
		name, header, body      string
		fallback, maximum, want time.Duration
		reject                  bool
	}{
		{"historical_default", "", historical, 5 * time.Second, 90 * time.Second, time.Minute, false},
		{"historical_case_space", "", `{"error":{"message":"  HISTORICAL WORK RATE LIMIT EXCEEDED  "}}`, 5 * time.Second, 90 * time.Second, time.Minute, false},
		{"historical_partial_batch", "", `[{"result":"0x01","id":2},` + historical + `]`, 5 * time.Second, 90 * time.Second, time.Minute, false},
		{"short_header", "5", historical, 5 * time.Second, 90 * time.Second, time.Minute, false},
		{"longer_header", "80", historical, 5 * time.Second, 90 * time.Second, 80 * time.Second, false},
		{"maximum_header", "90", historical, 5 * time.Second, 90 * time.Second, 90 * time.Second, false},
		{"unbounded_header", "91", historical, 5 * time.Second, 90 * time.Second, 0, true},
		{"json_delay", "", `{"retry_after_seconds":75,"error":{"message":"Historical work rate limit exceeded"}}`, 5 * time.Second, 90 * time.Second, 75 * time.Second, false},
		{"unbounded_json_delay", "", `{"retry_after_seconds":91,"error":{"message":"Historical work rate limit exceeded"}}`, 5 * time.Second, 90 * time.Second, 0, true},
		{"fixture_maximum", "", historical, time.Nanosecond, 20 * time.Millisecond, 20 * time.Millisecond, false},
		{"invalid_maximum", "", historical, time.Nanosecond, 0, 0, true},
		{"other_overload", "", `{"error":{"message":"upstream overloaded"}}`, 5 * time.Second, 90 * time.Second, 5 * time.Second, false},
		{"near_match", "", `{"error":{"message":"Historical work rate limit exceeded: invalid proof"}}`, 5 * time.Second, 90 * time.Second, 5 * time.Second, false},
		{"permanent_history", "", `{"error":{"message":"Historical state unavailable"}}`, 5 * time.Second, 90 * time.Second, 5 * time.Second, false},
		{"malformed_response", "", `{"error":`, 5 * time.Second, 90 * time.Second, 5 * time.Second, false},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			delay, err := rpcRetryAfter(http.Header{"Retry-After": []string{fixture.header}}, []byte(fixture.body), time.Unix(100, 0), fixture.fallback, fixture.maximum)
			if (err != nil) != fixture.reject || (!fixture.reject && delay != fixture.want) {
				t.Fatalf("historical cooldown policy delay=%s want=%s error=%v reject=%t", delay, fixture.want, err, fixture.reject)
			}
		})
	}
}

func TestPublicEVMHistoricalWorkCooldownIsSharedAndCancelable(t *testing.T) {
	// The existing injected clock exposes the policy without sleeping a minute.
	// The transport still installs the real shared FIFO cooldown and waits on it.
	anchor := time.Now().Add(time.Hour)
	gate := &rpcRequestGate{interval: time.Nanosecond, changed: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://rpc.example", strings.NewReader(`{"jsonrpc":"2.0","method":"eth_call","params":[{},"0x789e77"],"id":1}`))
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	transport := &rateLimitedRetryTransport{
		gate: gate, maximumRetries: 3, defaultRetryAfter: 5 * time.Millisecond, maximumRetryAfter: 2 * time.Second,
		now: func() time.Time { return anchor },
		base: roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"error":{"message":"Historical work rate limit exceeded"},"id":1}`))}, nil
		}),
	}
	done := make(chan error, 1)
	go func() {
		response, err := transport.RoundTrip(request)
		if response != nil {
			response.Body.Close()
		}
		done <- err
	}()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	var until time.Time
	for until.IsZero() {
		gate.stateLock.Lock()
		until = gate.cooldownUntil
		changed := gate.changed
		gate.stateLock.Unlock()
		if !until.IsZero() {
			break
		}
		select {
		case <-changed:
		case <-deadline.C:
			cancel()
			<-done
			t.Fatal("historical response did not install a shared cooldown")
		}
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Errorf("cooldown caller did not join canceled: %v", err)
	}
	if calls != 1 {
		t.Errorf("canceled historical request replayed %d times", calls)
	}
	if !until.Equal(anchor.Add(2 * time.Second)) {
		t.Errorf("shared historical cooldown=%s want=%s", until, anchor.Add(2*time.Second))
	}
	other := gate.enqueue()
	defer gate.remove(other)
	front, delay, _ := gate.waiterState(other, anchor)
	if !front || delay != 2*time.Second || gate.admit(other, anchor.Add(2*time.Second-time.Nanosecond)) {
		t.Fatalf("another waiter bypassed retained historical cooldown: front=%t delay=%s", front, delay)
	}
}
