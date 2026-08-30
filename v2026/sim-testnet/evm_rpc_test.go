package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestPublicEVMTransportReplays429RequestAfterBoundedDelay(t *testing.T) {
	payload := []byte(`{"jsonrpc":"2.0","method":"eth_chainId","id":1}`)
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://rpc.example", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	transport := &rateLimitedRetryTransport{
		gate: &rpcRequestGate{interval: time.Nanosecond}, maximum429Retries: 1,
		defaultRetryAfter: time.Millisecond, maximumRetryAfter: time.Second,
		base: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			calls++
			body, readErr := io.ReadAll(request.Body)
			if readErr != nil || !bytes.Equal(body, payload) {
				t.Fatalf("replayed request body = %q, error=%v", body, readErr)
			}
			if calls == 1 {
				return &http.Response{StatusCode: http.StatusTooManyRequests, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"retry_after_seconds":0}`))}, nil
			}
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"jsonrpc":"2.0","result":"0x3b1","id":1}`))}, nil
		}),
	}
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || calls != 2 {
		t.Fatalf("response/calls = %d/%d, want 200/2", response.StatusCode, calls)
	}
}

func TestPublicEVMTransportHonorsCancellationDuringProviderCooldown(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://rpc.example", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatal(err)
	}
	transport := &rateLimitedRetryTransport{
		gate: &rpcRequestGate{interval: time.Nanosecond}, maximum429Retries: 3,
		defaultRetryAfter: time.Second, maximumRetryAfter: 2 * time.Second,
		base: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{"Retry-After": []string{"1"}}, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
		}),
	}
	if _, err := transport.RoundTrip(request); err == nil || ctx.Err() == nil {
		t.Fatalf("provider cooldown ignored request cancellation: %v", err)
	}
}

func TestPublicEVMTransportStopsAfterExactRetryBound(t *testing.T) {
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://rpc.example", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	transport := &rateLimitedRetryTransport{
		gate: &rpcRequestGate{interval: time.Nanosecond}, maximum429Retries: 1,
		defaultRetryAfter: time.Nanosecond, maximumRetryAfter: time.Second,
		base: roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: http.StatusTooManyRequests, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"error":"still limited"}`))}, nil
		}),
	}
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if calls != 2 || response.StatusCode != http.StatusTooManyRequests || !strings.Contains(string(body), "still limited") {
		t.Fatalf("bounded response calls=%d status=%d body=%q", calls, response.StatusCode, body)
	}
}

func TestPublicEVMRetryAfterParsesProviderPolicyAndRejectsUnboundedWait(t *testing.T) {
	now := time.Unix(100, 0)
	if delay, err := rpcRetryAfter(http.Header{"Retry-After": []string{"7"}}, nil, now, time.Second, 10*time.Second); err != nil || delay != 7*time.Second {
		t.Fatalf("header retry delay = %s, %v", delay, err)
	}
	if delay, err := rpcRetryAfter(make(http.Header), []byte(`{"retry_after_seconds":6}`), now, time.Second, 10*time.Second); err != nil || delay != 6*time.Second {
		t.Fatalf("JSON retry delay = %s, %v", delay, err)
	}
	if _, err := rpcRetryAfter(http.Header{"Retry-After": []string{"11"}}, nil, now, time.Second, 10*time.Second); err == nil {
		t.Fatal("unbounded provider retry delay was accepted")
	}
}

func TestPublicEVMRequestGatePacesAndSharesCooldown(t *testing.T) {
	gate := &rpcRequestGate{interval: 5 * time.Millisecond}
	if err := gate.wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if err := gate.wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if time.Since(started) < 3*time.Millisecond {
		t.Fatal("consecutive public EVM requests were not paced")
	}
	gate.cooldown(time.Now().Add(10 * time.Millisecond))
	started = time.Now()
	if err := gate.wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if time.Since(started) < 8*time.Millisecond {
		t.Fatal("provider cooldown was not shared by the request gate")
	}
}

func TestSharedProviderPostStateCloneIsDetached(t *testing.T) {
	original := map[string]any{"nested": map[string]any{"value": "before"}}
	cloned, err := cloneObservedPostState(original)
	if err != nil {
		t.Fatal(err)
	}
	cloned["nested"].(map[string]any)["value"] = "after"
	if original["nested"].(map[string]any)["value"] != "before" {
		t.Fatal("shared-provider evidence reused a mutable observation map")
	}
}

func TestConfiguredEVMRateLimitAppliesOnlyToPublicRoute(t *testing.T) {
	cfg := testResolvedConfig(t)
	public := cfg.Public.Chain.EVMPublicReadEndpoint
	if configuredEVMRequestsPerMinute(cfg, public) != 40 || configuredEVMRequestsPerMinute(cfg, cfg.OperationalEVM) != 0 {
		t.Fatal("private operational and public observer EVM routes were not distinguished")
	}
	cfg.OperationalEVM = public
	cfg.OperationalRPCMode = rpcModePublicOverride
	if configuredEVMRequestsPerMinute(cfg, cfg.OperationalEVM) != 40 {
		t.Fatal("public operational EVM route was not rate limited")
	}
}
