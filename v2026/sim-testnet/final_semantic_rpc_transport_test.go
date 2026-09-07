package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gsrpcgeth "github.com/centrifuge/go-substrate-rpc-client/v4/gethrpc"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
)

type scriptedFinalSemanticSubstrateClient struct {
	mu       sync.Mutex
	errors   []error
	value    string
	calls    int
	closed   bool
	contexts []context.Context
}

func (client *scriptedFinalSemanticSubstrateClient) Call(result any, method string, args ...any) error {
	return client.CallContext(context.Background(), result, method, args...)
}

func (client *scriptedFinalSemanticSubstrateClient) CallContext(ctx context.Context, result any, _ string, _ ...any) error {
	client.mu.Lock()
	defer client.mu.Unlock()
	client.calls++
	client.contexts = append(client.contexts, ctx)
	index := client.calls - 1
	if index < len(client.errors) && client.errors[index] != nil {
		return client.errors[index]
	}
	if target, ok := result.(*string); ok {
		*target = client.value
	}
	return nil
}

func (client *scriptedFinalSemanticSubstrateClient) Subscribe(context.Context, string, string, string, string, any, ...any) (*gsrpcgeth.ClientSubscription, error) {
	return nil, errors.New("unexpected subscription")
}

func (client *scriptedFinalSemanticSubstrateClient) URL() string { return "wss://dial.example.test" }

func (client *scriptedFinalSemanticSubstrateClient) Close() {
	client.mu.Lock()
	client.closed = true
	client.mu.Unlock()
}

func (client *scriptedFinalSemanticSubstrateClient) callCount() int {
	client.mu.Lock()
	defer client.mu.Unlock()
	return client.calls
}

func (client *scriptedFinalSemanticSubstrateClient) allCallsHaveDeadlines() bool {
	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.contexts) == 0 {
		return false
	}
	for _, ctx := range client.contexts {
		if _, ok := ctx.Deadline(); !ok {
			return false
		}
	}
	return true
}

type finalSemanticTestRPCError struct {
	code    int
	message string
}

func (err finalSemanticTestRPCError) Error() string  { return err.message }
func (err finalSemanticTestRPCError) ErrorCode() int { return err.code }

func immediateFinalSemanticRetryPolicy() finalSemanticRPCRetryPolicy {
	return finalSemanticRPCRetryPolicy{
		maximumAttempts:   finalSemanticRPCMaximumAttempts,
		attemptTimeout:    time.Second,
		initialRetryDelay: time.Nanosecond,
		maximumRetryDelay: time.Nanosecond,
		wait: func(ctx context.Context, _ time.Duration) error {
			return ctx.Err()
		},
	}
}

func TestFinalSemanticSubstrateRetriesTransientCallAndReturnsSuccess(t *testing.T) {
	base := &scriptedFinalSemanticSubstrateClient{errors: []error{io.ErrUnexpectedEOF, nil}, value: "final"}
	policy := immediateFinalSemanticRetryPolicy()
	var waits atomic.Int32
	policy.wait = func(context.Context, time.Duration) error {
		waits.Add(1)
		return nil
	}
	client := &resilientFinalSemanticSubstrateClient{
		root: context.Background(), canonical: "wss://canonical.example.test", base: base,
		gate: &rpcRequestGate{interval: time.Nanosecond}, policy: policy,
	}
	var result string
	if err := client.CallContext(context.Background(), &result, "state_getStorage", "0x01"); err != nil {
		t.Fatal(err)
	}
	if result != "final" || base.callCount() != 2 || waits.Load() != 1 || !base.allCallsHaveDeadlines() {
		t.Fatalf("transient retry result=%q calls=%d waits=%d", result, base.callCount(), waits.Load())
	}
	if client.URL() != "wss://canonical.example.test" {
		t.Fatalf("resilient client exposed dial identity %q", client.URL())
	}
}

func TestFinalSemanticSubstrateFailsClosedOnPermanentArchiveError(t *testing.T) {
	base := &scriptedFinalSemanticSubstrateClient{errors: []error{
		finalSemanticTestRPCError{code: -32000, message: "State already discarded for block 0x1234"},
	}}
	policy := immediateFinalSemanticRetryPolicy()
	var waits atomic.Int32
	policy.wait = func(context.Context, time.Duration) error {
		waits.Add(1)
		return nil
	}
	client := &resilientFinalSemanticSubstrateClient{
		root: context.Background(), canonical: "wss://archive.example.test", base: base,
		gate: &rpcRequestGate{interval: time.Nanosecond}, policy: policy,
	}
	err := client.CallContext(context.Background(), new(string), "state_getStorage", "0x01", "0x02")
	if err == nil || !errors.Is(err, base.errors[0]) || base.callCount() != 1 || waits.Load() != 0 {
		t.Fatalf("permanent archive error=%v calls=%d waits=%d", err, base.callCount(), waits.Load())
	}
}

func TestFinalSemanticSubstrateCancellationInterruptsRetry(t *testing.T) {
	base := &scriptedFinalSemanticSubstrateClient{errors: []error{io.ErrUnexpectedEOF}}
	policy := immediateFinalSemanticRetryPolicy()
	retrying := make(chan struct{})
	policy.wait = func(ctx context.Context, _ time.Duration) error {
		close(retrying)
		<-ctx.Done()
		return ctx.Err()
	}
	client := &resilientFinalSemanticSubstrateClient{
		root: context.Background(), canonical: "wss://cancel.example.test", base: base,
		gate: &rpcRequestGate{interval: time.Nanosecond}, policy: policy,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- client.CallContext(ctx, new(string), "chain_getHeader")
	}()
	select {
	case <-retrying:
	case <-time.After(time.Second):
		t.Fatal("RPC retry did not reach its cancellable wait")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) || base.callCount() != 1 {
			t.Fatalf("canceled retry error=%v calls=%d", err, base.callCount())
		}
	case <-time.After(time.Second):
		t.Fatal("RPC retry ignored context cancellation")
	}
}

func TestFinalSemanticRPCRetriesAreBounded(t *testing.T) {
	base := &scriptedFinalSemanticSubstrateClient{errors: []error{
		io.ErrUnexpectedEOF, io.ErrUnexpectedEOF, io.ErrUnexpectedEOF, io.ErrUnexpectedEOF, io.ErrUnexpectedEOF,
	}}
	policy := immediateFinalSemanticRetryPolicy()
	policy.wait = func(context.Context, time.Duration) error { return nil }
	client := &resilientFinalSemanticSubstrateClient{
		root: context.Background(), canonical: "wss://bounded.example.test", base: base,
		gate: &rpcRequestGate{interval: time.Nanosecond}, policy: policy,
	}
	if err := client.CallContext(context.Background(), new(string), "chain_getHeader"); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("bounded retry error=%v", err)
	}
	if base.callCount() != finalSemanticRPCMaximumAttempts {
		t.Fatalf("bounded retry calls=%d want=%d", base.callCount(), finalSemanticRPCMaximumAttempts)
	}
}

func TestFinalSemanticRPCTransientClassificationFailsClosed(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		transient bool
	}{
		{name: "unexpected EOF", err: io.ErrUnexpectedEOF, transient: true},
		{name: "HTTP 429", err: gethrpc.HTTPError{StatusCode: 429, Status: "429 Too Many Requests"}, transient: true},
		{name: "RPC capacity", err: finalSemanticTestRPCError{code: -32005, message: "capacity"}, transient: true},
		{name: "archive beats generic code", err: finalSemanticTestRPCError{code: -32005, message: "archive state pruned"}},
		{name: "invalid params", err: finalSemanticTestRPCError{code: -32602, message: "invalid params"}},
		{name: "semantic mismatch", err: errors.New("canonical block hash mismatch")},
		{name: "canceled", err: context.Canceled},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := finalSemanticRPCErrorIsTransient(test.err); got != test.transient {
				t.Fatalf("transient=%t want=%t for %v", got, test.transient, test.err)
			}
		})
	}
}

func TestFinalSemanticSubstrateGateIsSharedAndFIFO(t *testing.T) {
	endpoint := fmt.Sprintf("wss://shared-%s.example.test", t.Name())
	const callers = 32
	results := make(chan *rpcRequestGate, callers)
	errorsFound := make(chan error, callers)
	for range callers {
		go func() {
			gate, err := sharedFinalSemanticSubstrateRequestGate(endpoint, 2)
			results <- gate
			errorsFound <- err
		}()
	}
	var shared *rpcRequestGate
	for range callers {
		gate := <-results
		if err := <-errorsFound; err != nil {
			t.Fatal(err)
		}
		if shared == nil {
			shared = gate
		} else if gate != shared {
			t.Fatal("same canonical endpoint acquired more than one Substrate gate")
		}
	}
	other, err := sharedFinalSemanticSubstrateRequestGate(endpoint+"-other", 2)
	if err != nil {
		t.Fatal(err)
	}
	if other == shared {
		t.Fatal("distinct canonical endpoints shared a Substrate gate")
	}

	now := time.Unix(1_000, 0)
	first := shared.enqueue()
	second := shared.enqueue()
	third := shared.enqueue()
	if front, delay, _ := shared.waiterState(first, now); !front || delay != 0 {
		t.Fatalf("first FIFO state front=%t delay=%s", front, delay)
	}
	if front, _, _ := shared.waiterState(second, now); front {
		t.Fatal("second Substrate request bypassed FIFO front")
	}
	shared.remove(second)
	if !shared.admit(first, now) {
		t.Fatal("first Substrate request was not admitted")
	}
	if front, delay, _ := shared.waiterState(third, now); !front || delay != 500*time.Millisecond {
		t.Fatalf("third FIFO state front=%t delay=%s", front, delay)
	}
	if !shared.admit(third, now.Add(500*time.Millisecond)) {
		t.Fatal("third Substrate request was not admitted at its exact slot")
	}
}

func TestFinalSemanticRPCTransportKeepsCanonicalIdentitySeparateFromDial(t *testing.T) {
	public := &PublicDeploymentManifest{
		SubstrateRPC: "wss://canonical-substrate.example.test",
		EVMRPC:       "https://canonical-evm.example.test",
	}
	direct, err := canonicalFinalSemanticRPCTransport(public, 40, 2)
	if err != nil {
		t.Fatal(err)
	}
	if direct.dialSubstrateRPC != public.SubstrateRPC || direct.dialEVMRPC != public.EVMRPC || direct.evmRequestsPerMinute != finalSemanticDefaultEVMRequestsPerMinute {
		t.Fatalf("canonical transport substituted a dial endpoint: %+v", direct)
	}
	supervised, err := supervisedFinalSemanticRPCTransport(public, 2)
	if err != nil {
		t.Fatal(err)
	}
	if supervised.dialEVMRPC != "http://"+campaignEVMAuthority() || supervised.dialEVMRPC == public.EVMRPC || supervised.dialSubstrateRPC != public.SubstrateRPC {
		t.Fatalf("supervised transport routes are not exact: %+v", supervised)
	}
	reader := &PublicFinalSemanticChainReader{
		canonicalSubstrateRPC: supervised.canonicalSubstrateRPC,
		canonicalEVMRPC:       supervised.canonicalEVMRPC,
		evidenceURI:           "https://operator.example.test/evidence.json",
	}
	substrate, evm, evidence := reader.Endpoints()
	if substrate != public.SubstrateRPC || evm != public.EVMRPC || evidence != reader.evidenceURI || evm == supervised.dialEVMRPC {
		t.Fatalf("transcript endpoints substrate=%q evm=%q evidence=%q", substrate, evm, evidence)
	}

	for name, mutate := range map[string]func(*finalSemanticRPCTransport){
		"arbitrary supervised EVM":  func(value *finalSemanticRPCTransport) { value.dialEVMRPC = "http://127.0.0.1:8545" },
		"substituted Substrate":     func(value *finalSemanticRPCTransport) { value.dialSubstrateRPC = "wss://attacker.example.test" },
		"substituted canonical EVM": func(value *finalSemanticRPCTransport) { value.canonicalEVMRPC = "https://attacker.example.test" },
		"unknown profile":           func(value *finalSemanticRPCTransport) { value.profile = "arbitrary-v1" },
	} {
		t.Run(name, func(t *testing.T) {
			tampered := supervised
			mutate(&tampered)
			if err := validateFinalSemanticRPCTransport(public, tampered); err == nil {
				t.Fatal("tampered transport was accepted")
			}
		})
	}
	tamperedDirect := direct
	tamperedDirect.dialEVMRPC = "http://" + campaignEVMAuthority()
	if err := validateFinalSemanticRPCTransport(public, tamperedDirect); err == nil {
		t.Fatal("canonical profile accepted supervised endpoint substitution")
	}
}

func TestFinalSemanticRPCTransportForStoppedCampaignUsesCanonicalQuota(t *testing.T) {
	cfg := testResolvedConfig(t)
	cfg.OperationalRPCMode = rpcModePublicOverride
	cfg.Public.Chain.SubstratePublicReadEndpoint = "wss://canonical-substrate.example.test"
	cfg.Public.Chain.EVMPublicReadEndpoint = "https://canonical-evm.example.test"
	cfg.OperationalSubstrate = cfg.Public.Chain.SubstratePublicReadEndpoint
	cfg.OperationalEVM = cfg.Public.Chain.EVMPublicReadEndpoint
	public := &PublicDeploymentManifest{SubstrateRPC: cfg.Public.Chain.SubstratePublicReadEndpoint, EVMRPC: cfg.Public.Chain.EVMPublicReadEndpoint}
	transport, err := finalSemanticRPCTransportForConfig(context.Background(), cfg, t.TempDir(), public)
	if err != nil {
		t.Fatal(err)
	}
	if transport.profile != finalSemanticCanonicalRPCTransport || transport.dialEVMRPC != public.EVMRPC || transport.evmRequestsPerMinute != 40 || transport.substrateRequestsPerSecond != 2 {
		t.Fatalf("stopped campaign transport=%+v", transport)
	}
}

func TestFinalSemanticCapturedReplayUsesPortableCanonicalTransport(t *testing.T) {
	cfg := testResolvedConfig(t)
	cfg.OperationalRPCMode = rpcModePublicOverride
	cfg.Public.Chain.SubstratePublicReadEndpoint = "wss://canonical-substrate.example.test"
	cfg.Public.Chain.EVMPublicReadEndpoint = "https://canonical-evm.example.test"
	cfg.OperationalSubstrate = cfg.Public.Chain.SubstratePublicReadEndpoint
	cfg.OperationalEVM = cfg.Public.Chain.EVMPublicReadEndpoint
	public := &PublicDeploymentManifest{SubstrateRPC: cfg.Public.Chain.SubstratePublicReadEndpoint, EVMRPC: cfg.Public.Chain.EVMPublicReadEndpoint}
	// The authenticated archive may describe the producer's former loopback
	// supervisor. It is provenance, not a portable peer-review dial target.
	files := map[string][]byte{"launch-foundation/supervisor.state.json": []byte(`{"producer":"127.0.0.10:19948"}`)}
	transport, err := finalSemanticRPCTransportForCapturedFiles(cfg, public, files)
	if err != nil {
		t.Fatal(err)
	}
	if transport.profile != finalSemanticCanonicalRPCTransport || transport.dialEVMRPC != public.EVMRPC || transport.canonicalEVMRPC != public.EVMRPC || transport.evmRequestsPerMinute != 40 || transport.dialEVMRPC == "http://"+campaignEVMAuthority() {
		t.Fatalf("captured transport=%+v", transport)
	}

	tamperedPublic := *public
	tamperedPublic.EVMRPC = "https://attacker.example.test"
	if _, err := finalSemanticRPCTransportForCapturedFiles(cfg, &tamperedPublic, files); err == nil {
		t.Fatal("captured replay accepted a canonical endpoint not bound by configuration")
	}

	tamperedConfig := *cfg
	tamperedConfig.OperationalEVM = "https://attacker.example.test"
	if _, err := finalSemanticRPCTransportForCapturedFiles(&tamperedConfig, public, files); err == nil {
		t.Fatal("captured replay accepted a substituted operational endpoint")
	}
}
