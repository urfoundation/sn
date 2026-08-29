package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
	gethRPC "github.com/ethereum/go-ethereum/rpc"
)

const (
	publicEVMMaximum429Retries = 3
	publicEVMMaximumRetryAfter = 90 * time.Second
)

// rpcRequestGate spaces requests from every ethclient in one process. The
// executor has distinct signing clients by design, so client-local throttles
// would still burst against a provider's source-wide policy.
type rpcRequestGate struct {
	mu            sync.Mutex
	next          time.Time
	cooldownUntil time.Time
	interval      time.Duration
}

func newRPCRequestGate(requestsPerMinute int) (*rpcRequestGate, error) {
	if requestsPerMinute < 1 || requestsPerMinute > 60 {
		return nil, fmt.Errorf("public EVM request ceiling %d must be in [1,60] per minute", requestsPerMinute)
	}
	return &rpcRequestGate{interval: time.Minute / time.Duration(requestsPerMinute)}, nil
}

func (gate *rpcRequestGate) wait(ctx context.Context) error {
	if gate == nil || gate.interval <= 0 {
		return errors.New("public EVM request gate is unavailable")
	}
	for {
		gate.mu.Lock()
		now := time.Now()
		slot := gate.next
		if gate.cooldownUntil.After(slot) {
			slot = gate.cooldownUntil
		}
		if !slot.After(now) {
			gate.next = now.Add(gate.interval)
			gate.mu.Unlock()
			return nil
		}
		gate.mu.Unlock()
		timer := time.NewTimer(slot.Sub(now))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (gate *rpcRequestGate) cooldown(until time.Time) {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if until.After(gate.cooldownUntil) {
		gate.cooldownUntil = until
	}
}

type rateLimitedRetryTransport struct {
	base              http.RoundTripper
	gate              *rpcRequestGate
	maximum429Retries int
	defaultRetryAfter time.Duration
	maximumRetryAfter time.Duration
	now               func() time.Time
}

func (transport *rateLimitedRetryTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request == nil {
		return nil, errors.New("nil public EVM HTTP request")
	}
	var err error
	request, err = replayableRPCRequest(request)
	if err != nil {
		return nil, err
	}
	base := transport.base
	if base == nil {
		base = http.DefaultTransport
	}
	now := transport.now
	if now == nil {
		now = time.Now
	}
	for attempt := 0; ; attempt++ {
		if err := transport.gate.wait(request.Context()); err != nil {
			return nil, err
		}
		current := request
		if attempt > 0 {
			if request.GetBody == nil {
				return nil, errors.New("public EVM RPC request body cannot be replayed")
			}
			body, err := request.GetBody()
			if err != nil {
				return nil, err
			}
			current = request.Clone(request.Context())
			current.Body = body
		}
		response, err := base.RoundTrip(current)
		if err != nil {
			return nil, err
		}
		if response.StatusCode != http.StatusTooManyRequests {
			return response, nil
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		response.Body.Close()
		response.Body = io.NopCloser(bytes.NewReader(body))
		if readErr != nil {
			return nil, fmt.Errorf("read public EVM rate-limit response: %w", readErr)
		}
		if attempt >= transport.maximum429Retries {
			return response, nil
		}
		delay, err := rpcRetryAfter(response.Header, body, now(), transport.defaultRetryAfter, transport.maximumRetryAfter)
		if err != nil {
			response.Body.Close()
			return nil, err
		}
		transport.gate.cooldown(now().Add(delay))
	}
}

func replayableRPCRequest(request *http.Request) (*http.Request, error) {
	if request == nil || request.Body == nil || request.GetBody != nil {
		return request, nil
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, (4<<20)+1))
	request.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("buffer public EVM RPC request: %w", err)
	}
	if len(body) > 4<<20 {
		return nil, errors.New("public EVM RPC request exceeds 4 MiB replay limit")
	}
	cloned := request.Clone(request.Context())
	cloned.ContentLength = int64(len(body))
	cloned.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(body)), nil
	}
	cloned.Body, _ = cloned.GetBody()
	return cloned, nil
}

func rpcRetryAfter(header http.Header, body []byte, now time.Time, fallback, maximum time.Duration) (time.Duration, error) {
	delay := time.Duration(0)
	if text := strings.TrimSpace(header.Get("Retry-After")); text != "" {
		if seconds, err := strconv.ParseUint(text, 10, 31); err == nil {
			delay = time.Duration(seconds) * time.Second
		} else if retryAt, dateErr := http.ParseTime(text); dateErr == nil && retryAt.After(now) {
			delay = retryAt.Sub(now)
		}
	}
	if delay == 0 {
		var payload struct {
			RetryAfterSeconds uint64 `json:"retry_after_seconds"`
		}
		if json.Unmarshal(body, &payload) == nil && payload.RetryAfterSeconds > 0 {
			if maximum > 0 && payload.RetryAfterSeconds > uint64(maximum/time.Second)+1 {
				return 0, fmt.Errorf("public EVM retry-after %d seconds exceeds the bounded wait", payload.RetryAfterSeconds)
			}
			delay = time.Duration(payload.RetryAfterSeconds) * time.Second
		}
	}
	if delay == 0 {
		delay = fallback
	}
	if delay < 0 || maximum <= 0 || delay > maximum {
		return 0, fmt.Errorf("public EVM retry-after %s exceeds maximum %s", delay, maximum)
	}
	return delay, nil
}

var publicEVMRequestGates = struct {
	sync.Mutex
	values map[string]*rpcRequestGate
}{values: map[string]*rpcRequestGate{}}

func sharedPublicEVMRequestGate(endpoint string, requestsPerMinute int) (*rpcRequestGate, error) {
	key := fmt.Sprintf("%s\x00%d", endpoint, requestsPerMinute)
	publicEVMRequestGates.Lock()
	defer publicEVMRequestGates.Unlock()
	if existing := publicEVMRequestGates.values[key]; existing != nil {
		return existing, nil
	}
	gate, err := newRPCRequestGate(requestsPerMinute)
	if err != nil {
		return nil, err
	}
	publicEVMRequestGates.values[key] = gate
	return gate, nil
}

func dialEVMClient(ctx context.Context, endpoint string, requestsPerMinute int) (*ethclient.Client, error) {
	if requestsPerMinute == 0 {
		return ethclient.DialContext(ctx, endpoint)
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("rate-limited public EVM endpoint %q must use HTTP(S)", endpoint)
	}
	gate, err := sharedPublicEVMRequestGate(endpoint, requestsPerMinute)
	if err != nil {
		return nil, err
	}
	transport := &rateLimitedRetryTransport{
		base: http.DefaultTransport, gate: gate, maximum429Retries: publicEVMMaximum429Retries,
		defaultRetryAfter: 5 * time.Second, maximumRetryAfter: publicEVMMaximumRetryAfter,
	}
	httpClient := &http.Client{Transport: transport}
	client, err := gethRPC.DialOptions(ctx, endpoint, gethRPC.WithHTTPClient(httpClient))
	if err != nil {
		return nil, err
	}
	return ethclient.NewClient(client), nil
}

func configuredEVMRequestsPerMinute(cfg *ResolvedConfig, endpoint string) int {
	if cfg == nil || cfg.Config == nil || cfg.Public == nil || cfg.Config.LaunchInputs.PublicEVMMaximumRequestsPerMinute == 0 {
		return 0
	}
	if strings.EqualFold(strings.TrimSpace(endpoint), strings.TrimSpace(cfg.Public.Chain.EVMPublicReadEndpoint)) ||
		(cfg.OperationalRPCMode == rpcModePublicOverride && strings.EqualFold(strings.TrimSpace(endpoint), strings.TrimSpace(cfg.OperationalEVM))) {
		return cfg.Config.LaunchInputs.PublicEVMMaximumRequestsPerMinute
	}
	return 0
}

func dialConfiguredEVMClient(ctx context.Context, cfg *ResolvedConfig, endpoint string) (*ethclient.Client, error) {
	return dialEVMClient(ctx, endpoint, configuredEVMRequestsPerMinute(cfg, endpoint))
}
