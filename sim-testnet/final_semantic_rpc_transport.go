package main

// This file separates the signed public RPC identity embedded in FINAL.md
// from the narrow transport route used to obtain those bytes. During a live
// public-RPC campaign the EVM dial may use only the fixed supervised egress;
// every other route dials the signed endpoint itself. Substrate calls are
// paced and retried below gsrpc so indirect event-decoder reads cannot bypass
// the same policy.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"syscall"
	"time"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	gsrpcclient "github.com/centrifuge/go-substrate-rpc-client/v4/client"
	gsrpcgeth "github.com/centrifuge/go-substrate-rpc-client/v4/gethrpc"
	gsrpcrpc "github.com/centrifuge/go-substrate-rpc-client/v4/rpc"
	gsrpctypes "github.com/centrifuge/go-substrate-rpc-client/v4/types"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
)

const (
	finalSemanticCanonicalRPCTransport  = "canonical-public-rpc-v1"
	finalSemanticSupervisedRPCTransport = "supervised-public-evm-egress-v1"

	finalSemanticDefaultEVMRequestsPerMinute       = 40
	finalSemanticDefaultSubstrateRequestsPerSecond = 2
	finalSemanticRPCMaximumAttempts                = 4
	finalSemanticRPCAttemptTimeout                 = 30 * time.Second
	finalSemanticRPCInitialRetryDelay              = time.Second
	finalSemanticRPCMaximumRetryDelay              = 4 * time.Second
)

// finalSemanticRPCTransport is deliberately not serialized. canonical* comes
// only from the authenticated public manifest and is what Endpoints reports;
// dial* is an I/O implementation detail admitted by one of the constructors
// below. Keeping the fields private prevents arbitrary endpoint substitution.
type finalSemanticRPCTransport struct {
	profile                    string
	canonicalSubstrateRPC      string
	canonicalEVMRPC            string
	dialSubstrateRPC           string
	dialEVMRPC                 string
	evmRequestsPerMinute       int
	substrateRequestsPerSecond int
}

func canonicalFinalSemanticRPCTransport(public *PublicDeploymentManifest, evmRequestsPerMinute, substrateRequestsPerSecond int) (finalSemanticRPCTransport, error) {
	if public == nil {
		return finalSemanticRPCTransport{}, errors.New("canonical final semantic RPC manifest is missing")
	}
	transport := finalSemanticRPCTransport{
		profile:               finalSemanticCanonicalRPCTransport,
		canonicalSubstrateRPC: public.SubstrateRPC, canonicalEVMRPC: public.EVMRPC,
		dialSubstrateRPC: public.SubstrateRPC, dialEVMRPC: public.EVMRPC,
		evmRequestsPerMinute: evmRequestsPerMinute, substrateRequestsPerSecond: substrateRequestsPerSecond,
	}
	return transport, validateFinalSemanticRPCTransport(public, transport)
}

func supervisedFinalSemanticRPCTransport(public *PublicDeploymentManifest, substrateRequestsPerSecond int) (finalSemanticRPCTransport, error) {
	if public == nil {
		return finalSemanticRPCTransport{}, errors.New("supervised final semantic RPC manifest is missing")
	}
	transport := finalSemanticRPCTransport{
		profile:               finalSemanticSupervisedRPCTransport,
		canonicalSubstrateRPC: public.SubstrateRPC, canonicalEVMRPC: public.EVMRPC,
		dialSubstrateRPC: public.SubstrateRPC, dialEVMRPC: "http://" + campaignEVMAuthority(),
		substrateRequestsPerSecond: substrateRequestsPerSecond,
	}
	return transport, validateFinalSemanticRPCTransport(public, transport)
}

func validateFinalSemanticRPCTransport(public *PublicDeploymentManifest, transport finalSemanticRPCTransport) error {
	if public == nil || transport.canonicalSubstrateRPC == "" || transport.canonicalEVMRPC == "" {
		return errors.New("final semantic RPC transport identity is incomplete")
	}
	if transport.canonicalSubstrateRPC != public.SubstrateRPC || transport.canonicalEVMRPC != public.EVMRPC {
		return errors.New("final semantic RPC canonical endpoints differ from the authenticated manifest")
	}
	if transport.dialSubstrateRPC != transport.canonicalSubstrateRPC {
		return errors.New("final semantic Substrate dial endpoint must remain canonical")
	}
	if transport.substrateRequestsPerSecond < 1 || transport.substrateRequestsPerSecond > 5 {
		return errors.New("final semantic Substrate request ceiling must be in [1,5] per second")
	}
	switch transport.profile {
	case finalSemanticCanonicalRPCTransport:
		if transport.dialEVMRPC != transport.canonicalEVMRPC {
			return errors.New("canonical final semantic EVM transport cannot substitute its dial endpoint")
		}
		if transport.evmRequestsPerMinute < 1 || transport.evmRequestsPerMinute > 60 {
			return errors.New("canonical final semantic EVM request ceiling must be in [1,60] per minute")
		}
	case finalSemanticSupervisedRPCTransport:
		if transport.dialEVMRPC != "http://"+campaignEVMAuthority() || transport.evmRequestsPerMinute != 0 {
			return errors.New("supervised final semantic EVM transport is not the fixed aggregate egress")
		}
	default:
		return fmt.Errorf("final semantic RPC transport profile %q is unsupported", transport.profile)
	}
	return nil
}

// finalSemanticRPCTransportForConfig selects the sole allowed live derivative.
// A stopped topology uses canonical endpoints. A live public-override topology
// must expose the healthy supervised gate and cannot fall back around it.
func finalSemanticRPCTransportForConfig(ctx context.Context, cfg *ResolvedConfig, stateDir string, public *PublicDeploymentManifest) (finalSemanticRPCTransport, error) {
	if ctx == nil || cfg == nil || cfg.Config == nil || cfg.Public == nil || public == nil {
		return finalSemanticRPCTransport{}, errors.New("configured final semantic RPC transport context is incomplete")
	}
	if public.SubstrateRPC != cfg.Public.Chain.SubstratePublicReadEndpoint || public.EVMRPC != cfg.Public.Chain.EVMPublicReadEndpoint {
		return finalSemanticRPCTransport{}, errors.New("authenticated public RPC endpoints differ from the canonical configuration")
	}
	substrateQPS := cfg.Config.Scenarios.Adversaries.MaximumRPCRequestsPerSec
	active, err := supervisedCampaignEgressActive(ctx, stateDir)
	if err != nil {
		return finalSemanticRPCTransport{}, err
	}
	if active && cfg.OperationalRPCMode == rpcModePublicOverride {
		runtime, err := campaignRPCConfig(cfg)
		if err != nil {
			return finalSemanticRPCTransport{}, err
		}
		if err := validateCampaignRPCTransport(cfg, runtime); err != nil {
			return finalSemanticRPCTransport{}, err
		}
		if runtime.Public.Chain.EVMPublicReadEndpoint != "http://"+campaignEVMAuthority() {
			return finalSemanticRPCTransport{}, errors.New("live final semantic EVM route is not the supervised campaign derivative")
		}
		return supervisedFinalSemanticRPCTransport(public, substrateQPS)
	}
	rpm := configuredEVMRequestsPerMinute(cfg, public.EVMRPC)
	return canonicalFinalSemanticRPCTransport(public, rpm, substrateQPS)
}

// finalSemanticRPCTransportForCapturedFiles is the portable async analyzer
// seam. The caller has already authenticated both files and the captured plan
// (including ResolvedInputsHash) through the owner-signed closed capture. A
// peer-review host cannot inherit the producer's loopback supervisor, so this
// path always dials the signed canonical endpoints through its own bounded
// gates. It deliberately performs no state-root read or live-state lookup.
func finalSemanticRPCTransportForCapturedFiles(cfg *ResolvedConfig, public *PublicDeploymentManifest, files map[string][]byte) (finalSemanticRPCTransport, error) {
	if cfg == nil || cfg.Config == nil || cfg.Public == nil || public == nil || len(files) == 0 {
		return finalSemanticRPCTransport{}, errors.New("captured final semantic RPC transport context is incomplete")
	}
	if public.SubstrateRPC != cfg.Public.Chain.SubstratePublicReadEndpoint || public.EVMRPC != cfg.Public.Chain.EVMPublicReadEndpoint {
		return finalSemanticRPCTransport{}, errors.New("captured public RPC endpoints differ from the configuration-bound canonical endpoints")
	}
	if cfg.OperationalRPCMode == rpcModePublicOverride && cfg.OperationalEVM != public.EVMRPC {
		return finalSemanticRPCTransport{}, errors.New("captured public EVM endpoint differs from the authorized operational endpoint")
	}
	return canonicalFinalSemanticRPCTransport(
		public,
		configuredEVMRequestsPerMinute(cfg, public.EVMRPC),
		cfg.Config.Scenarios.Adversaries.MaximumRPCRequestsPerSec,
	)
}

type finalSemanticRPCRetryPolicy struct {
	maximumAttempts   int
	attemptTimeout    time.Duration
	initialRetryDelay time.Duration
	maximumRetryDelay time.Duration
	wait              func(context.Context, time.Duration) error
}

func defaultFinalSemanticRPCRetryPolicy() finalSemanticRPCRetryPolicy {
	return finalSemanticRPCRetryPolicy{
		maximumAttempts: finalSemanticRPCMaximumAttempts, attemptTimeout: finalSemanticRPCAttemptTimeout,
		initialRetryDelay: finalSemanticRPCInitialRetryDelay, maximumRetryDelay: finalSemanticRPCMaximumRetryDelay,
		wait: waitFinalSemanticRPCRetry,
	}
}

func waitFinalSemanticRPCRetry(ctx context.Context, delay time.Duration) error {
	if ctx == nil {
		return errors.New("final semantic RPC retry context is nil")
	}
	if delay <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (policy finalSemanticRPCRetryPolicy) validate() error {
	if policy.maximumAttempts < 1 || policy.maximumAttempts > finalSemanticRPCMaximumAttempts || policy.attemptTimeout <= 0 || policy.initialRetryDelay <= 0 || policy.maximumRetryDelay < policy.initialRetryDelay || policy.wait == nil {
		return errors.New("final semantic RPC retry policy is invalid")
	}
	return nil
}

func retryFinalSemanticRPCCall(ctx context.Context, gate *rpcRequestGate, policy finalSemanticRPCRetryPolicy, call func(context.Context) error) error {
	if ctx == nil || call == nil {
		return errors.New("final semantic RPC call context is incomplete")
	}
	if err := policy.validate(); err != nil {
		return err
	}
	delay := policy.initialRetryDelay
	var last error
	for attempt := 1; attempt <= policy.maximumAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if gate != nil {
			if err := gate.wait(ctx); err != nil {
				return err
			}
		}
		attemptCtx, cancel := context.WithTimeout(ctx, policy.attemptTimeout)
		last = call(attemptCtx)
		cancel()
		if last == nil {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if attempt == policy.maximumAttempts || !finalSemanticRPCErrorIsTransient(last) {
			return last
		}
		if err := policy.wait(ctx, delay); err != nil {
			return err
		}
		if delay < policy.maximumRetryDelay {
			delay *= 2
			if delay > policy.maximumRetryDelay {
				delay = policy.maximumRetryDelay
			}
		}
	}
	return last
}

type finalSemanticRPCCodeError interface {
	ErrorCode() int
}

func finalSemanticRPCErrorIsTransient(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	message := strings.ToLower(strings.TrimSpace(err.Error()))
	for _, permanent := range []string{"pruned", "archive", "state already discarded", "unknown block", "header not found", "missing trie", "state unavailable"} {
		if strings.Contains(message, permanent) {
			return false
		}
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed) || errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.EPIPE) {
		return true
	}
	var networkError net.Error
	if errors.As(err, &networkError) && (networkError.Timeout() || networkError.Temporary()) {
		return true
	}
	var httpError gethrpc.HTTPError
	if errors.As(err, &httpError) {
		switch httpError.StatusCode {
		case http.StatusRequestTimeout, http.StatusTooEarly, http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			return true
		default:
			return false
		}
	}
	var rpcError finalSemanticRPCCodeError
	if errors.As(err, &rpcError) {
		switch rpcError.ErrorCode() {
		case -32002, -32005, -32016:
			return true
		}
	}
	for _, transient := range []string{"upstream overloaded", "rate limit", "too many requests", "temporarily unavailable", "try again", "request timed out", "timeout", "429", "502 bad gateway", "503 service unavailable", "504 gateway timeout", "connection reset by peer", "broken pipe", "unexpected eof", "use of closed network connection", "websocket: close 1006"} {
		if strings.Contains(message, transient) {
			return true
		}
	}
	return false
}

var finalSemanticSubstrateRequestGates = struct {
	sync.Mutex
	values map[string]*rpcRequestGate
}{values: map[string]*rpcRequestGate{}}

func sharedFinalSemanticSubstrateRequestGate(canonicalEndpoint string, requestsPerSecond int) (*rpcRequestGate, error) {
	if canonicalEndpoint == "" || requestsPerSecond < 1 || requestsPerSecond > 5 {
		return nil, errors.New("final semantic Substrate request gate configuration is invalid")
	}
	key := fmt.Sprintf("%s\x00%d", canonicalEndpoint, requestsPerSecond)
	finalSemanticSubstrateRequestGates.Lock()
	defer finalSemanticSubstrateRequestGates.Unlock()
	if existing := finalSemanticSubstrateRequestGates.values[key]; existing != nil {
		return existing, nil
	}
	gate := &rpcRequestGate{interval: time.Second / time.Duration(requestsPerSecond)}
	finalSemanticSubstrateRequestGates.values[key] = gate
	return gate, nil
}

// finalSemanticSubstrateBaseClient is the context-aware equivalent of
// gsrpc/client.Connect. The upstream package's concrete adapter is private,
// so this small wrapper exposes the same public Client interface.
type finalSemanticSubstrateBaseClient struct {
	raw *gsrpcgeth.Client
	url string
}

func (client *finalSemanticSubstrateBaseClient) Call(result any, method string, args ...any) error {
	return client.raw.Call(result, method, args...)
}

func (client *finalSemanticSubstrateBaseClient) CallContext(ctx context.Context, result any, method string, args ...any) error {
	return client.raw.CallContext(ctx, result, method, args...)
}

func (client *finalSemanticSubstrateBaseClient) Subscribe(ctx context.Context, namespace, subscribeMethodSuffix, unsubscribeMethodSuffix, notificationMethodSuffix string, channel any, args ...any) (*gsrpcgeth.ClientSubscription, error) {
	return client.raw.Subscribe(ctx, namespace, subscribeMethodSuffix, unsubscribeMethodSuffix, notificationMethodSuffix, channel, args...)
}

func (client *finalSemanticSubstrateBaseClient) URL() string { return client.url }
func (client *finalSemanticSubstrateBaseClient) Close()      { client.raw.Close() }

// resilientFinalSemanticSubstrateClient sits below gsrpc.RPC. Consequently
// every typed helper and event-decoder read shares the same cancellation,
// pacing, and retry boundary. Subscriptions are outside the immutable replay
// protocol and are rejected rather than allowed to bypass the call gate.
type resilientFinalSemanticSubstrateClient struct {
	root      context.Context
	canonical string
	base      gsrpcclient.Client
	gate      *rpcRequestGate
	policy    finalSemanticRPCRetryPolicy
}

func (client *resilientFinalSemanticSubstrateClient) Call(result any, method string, args ...any) error {
	return client.CallContext(client.root, result, method, args...)
}

func (client *resilientFinalSemanticSubstrateClient) CallContext(ctx context.Context, result any, method string, args ...any) error {
	if client == nil || client.base == nil {
		return errors.New("final semantic Substrate client is closed")
	}
	return retryFinalSemanticRPCCall(ctx, client.gate, client.policy, func(attemptCtx context.Context) error {
		return client.base.CallContext(attemptCtx, result, method, args...)
	})
}

func (client *resilientFinalSemanticSubstrateClient) Subscribe(context.Context, string, string, string, string, any, ...any) (*gsrpcgeth.ClientSubscription, error) {
	return nil, errors.New("final semantic Substrate subscriptions are disabled")
}

func (client *resilientFinalSemanticSubstrateClient) URL() string {
	if client == nil {
		return ""
	}
	return client.canonical
}

func (client *resilientFinalSemanticSubstrateClient) Close() {
	if client != nil && client.base != nil {
		client.base.Close()
	}
}

func dialFinalSemanticSubstrate(ctx context.Context, transport finalSemanticRPCTransport, policy finalSemanticRPCRetryPolicy) (*gsrpc.SubstrateAPI, gsrpctypes.Hash, error) {
	gate, err := sharedFinalSemanticSubstrateRequestGate(transport.canonicalSubstrateRPC, transport.substrateRequestsPerSecond)
	if err != nil {
		return nil, gsrpctypes.Hash{}, err
	}
	var raw *gsrpcgeth.Client
	err = retryFinalSemanticRPCCall(ctx, nil, policy, func(attemptCtx context.Context) error {
		candidate, dialErr := gsrpcgeth.DialContext(attemptCtx, transport.dialSubstrateRPC)
		if dialErr != nil {
			if candidate != nil {
				candidate.Close()
			}
			return dialErr
		}
		raw = candidate
		return nil
	})
	if err != nil {
		return nil, gsrpctypes.Hash{}, fmt.Errorf("dial public Substrate RPC: %w", err)
	}
	base := &finalSemanticSubstrateBaseClient{raw: raw, url: transport.dialSubstrateRPC}
	client := &resilientFinalSemanticSubstrateClient{root: ctx, canonical: transport.canonicalSubstrateRPC, base: base, gate: gate, policy: policy}
	rpcAPI, err := gsrpcrpc.NewRPC(client)
	if err != nil {
		client.Close()
		return nil, gsrpctypes.Hash{}, fmt.Errorf("initialize public Substrate RPC: %w", err)
	}
	api := &gsrpc.SubstrateAPI{RPC: rpcAPI, Client: client}
	genesis, err := api.RPC.Chain.GetBlockHash(0)
	if err != nil {
		client.Close()
		return nil, gsrpctypes.Hash{}, fmt.Errorf("public Substrate genesis hash: %w", err)
	}
	return api, genesis, nil
}
