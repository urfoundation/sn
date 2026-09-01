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
	publicEVMMaximumRetries       = 3
	publicEVMMaximumRetryAfter    = 90 * time.Second
	publicEVMRPCBodyReplayLimit   = 4 * 1024 * 1024
	publicEVMRPCResponseReadLimit = 4 * 1024 * 1024
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
	maximumRetries    int
	defaultRetryAfter time.Duration
	maximumRetryAfter time.Duration
	now               func() time.Time
}

// Minimal request envelope used to classify replay safety without trusting or
// interpreting method parameters.
type publicEVMRPCRequest struct {
	Method string `json:"method"`
}

// Minimal response envelope used to recognize the provider's exact capacity
// sentinel while leaving all other JSON-RPC errors untouched.
type publicEVMRPCResponse struct {
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
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
	readOnly := publicEVMRPCRequestIsReadOnly(request)
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
			if response != nil && response.Body != nil {
				response.Body.Close()
			}
			if !readOnly || attempt >= transport.maximumRetries {
				return nil, err
			}
			delay, delayErr := rpcRetryAfter(nil, nil, now(), transport.defaultRetryAfter, transport.maximumRetryAfter)
			if delayErr != nil {
				return nil, errors.Join(err, delayErr)
			}
			transport.gate.cooldown(now().Add(delay))
			continue
		}
		retry, body, err := publicEVMResponseNeedsRetry(response, readOnly)
		if err != nil {
			return nil, err
		}
		if !retry {
			return response, nil
		}
		if attempt >= transport.maximumRetries {
			return response, nil
		}
		delay, err := rpcRetryAfter(response.Header, body, now(), transport.defaultRetryAfter, transport.maximumRetryAfter)
		if err != nil {
			response.Body.Close()
			return nil, err
		}
		response.Body.Close()
		transport.gate.cooldown(now().Add(delay))
	}
}

// Only idempotent observation methods may replay an ambiguous HTTP-success
// overload response. A transaction submission is never sent twice here.
func publicEVMRPCRequestIsReadOnly(request *http.Request) bool {
	if request == nil || request.GetBody == nil {
		return false
	}
	bodyReader, err := request.GetBody()
	if err != nil {
		return false
	}
	body, err := io.ReadAll(io.LimitReader(bodyReader, publicEVMRPCBodyReplayLimit+1))
	bodyReader.Close()
	if err != nil || len(body) > publicEVMRPCBodyReplayLimit {
		return false
	}
	var requests []publicEVMRPCRequest
	if bytes.HasPrefix(bytes.TrimSpace(body), []byte("[")) {
		if json.Unmarshal(body, &requests) != nil || len(requests) == 0 {
			return false
		}
	} else {
		var single publicEVMRPCRequest
		if json.Unmarshal(body, &single) != nil {
			return false
		}
		requests = []publicEVMRPCRequest{single}
	}
	for _, rpcRequest := range requests {
		method := strings.TrimSpace(rpcRequest.Method)
		if strings.HasPrefix(method, "eth_get") {
			continue
		}
		switch method {
		case "eth_blockNumber", "eth_call", "eth_chainId", "eth_estimateGas", "eth_feeHistory", "eth_gasPrice", "eth_maxPriorityFeePerGas", "eth_syncing", "net_version", "web3_clientVersion":
		default:
			return false
		}
	}
	return true
}

// Public providers sometimes encode capacity failures as JSON-RPC errors with
// HTTP 200. Retry only the observed sentinel and standard transient statuses.
func publicEVMResponseNeedsRetry(response *http.Response, readOnly bool) (bool, []byte, error) {
	if response == nil || response.Body == nil {
		return false, nil, errors.New("public EVM RPC returned an empty HTTP response")
	}
	retryStatus := readOnly && (response.StatusCode == http.StatusRequestTimeout ||
		response.StatusCode == http.StatusTooEarly ||
		response.StatusCode == http.StatusTooManyRequests ||
		response.StatusCode == http.StatusBadGateway ||
		response.StatusCode == http.StatusServiceUnavailable ||
		response.StatusCode == http.StatusGatewayTimeout)
	if !retryStatus && (!readOnly || response.StatusCode != http.StatusOK) {
		return false, nil, nil
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, publicEVMRPCResponseReadLimit+1))
	response.Body.Close()
	response.Body = io.NopCloser(bytes.NewReader(body))
	if err != nil {
		response.Body.Close()
		return false, nil, fmt.Errorf("read public EVM RPC response: %w", err)
	}
	if len(body) > publicEVMRPCResponseReadLimit {
		response.Body.Close()
		return false, nil, errors.New("public EVM RPC response exceeds replay limit")
	}
	if retryStatus {
		return true, body, nil
	}
	return publicEVMRPCResponseIsOverloaded(body), body, nil
}

// Match the exact capacity signal observed from the official public testnet
// endpoint without turning contract reverts or arbitrary server errors transient.
func publicEVMRPCResponseIsOverloaded(body []byte) bool {
	var responses []publicEVMRPCResponse
	if bytes.HasPrefix(bytes.TrimSpace(body), []byte("[")) {
		if json.Unmarshal(body, &responses) != nil || len(responses) == 0 {
			return false
		}
	} else {
		var single publicEVMRPCResponse
		if json.Unmarshal(body, &single) != nil {
			return false
		}
		responses = []publicEVMRPCResponse{single}
	}
	for _, response := range responses {
		if response.Error != nil && strings.EqualFold(strings.TrimSpace(response.Error.Message), "upstream overloaded") {
			return true
		}
	}
	return false
}

func replayableRPCRequest(request *http.Request) (*http.Request, error) {
	if request == nil || request.Body == nil || request.GetBody != nil {
		return request, nil
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, publicEVMRPCBodyReplayLimit+1))
	request.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("buffer public EVM RPC request: %w", err)
	}
	if len(body) > publicEVMRPCBodyReplayLimit {
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
		base: http.DefaultTransport, gate: gate, maximumRetries: publicEVMMaximumRetries,
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
