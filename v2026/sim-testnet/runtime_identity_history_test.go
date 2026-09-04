// Release runtime history tests reproduce public-provider throttling and prove
// that bounded retry never widens the exact reviewed artifact boundary.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	gsrpcgeth "github.com/centrifuge/go-substrate-rpc-client/v4/gethrpc"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"

	"github.com/urfoundation/sn/v2026/crv4"
)

// Supplies the exact raw runtime RPC surface used by the release authenticator.
type releaseRuntimeTestClient struct {
	callContext func(context.Context, any, string, ...any) error
}

// Retains compatibility with GSRPC helpers outside cancellable tests.
func (self *releaseRuntimeTestClient) Call(result any, method string, args ...any) error {
	return self.callContext(context.Background(), result, method, args...)
}

// Preserves cancellation and exact arguments for each scripted response.
func (self *releaseRuntimeTestClient) CallContext(ctx context.Context, result any, method string, args ...any) error {
	return self.callContext(ctx, result, method, args...)
}

// Runtime identity authentication never subscribes.
func (self *releaseRuntimeTestClient) Subscribe(context.Context, string, string, string, string, any, ...any) (*gsrpcgeth.ClientSubscription, error) {
	return nil, errors.New("unexpected release runtime subscription")
}

// Gives diagnostics a stable provider identity.
func (self *releaseRuntimeTestClient) URL() string { return "wss://release-runtime.test" }

// Test clients own no external connection.
func (self *releaseRuntimeTestClient) Close() {}

// Produces a compact valid metadata-v14 response and its exact digest.
func releaseRuntimeTestMetadata(t *testing.T) (string, string) {
	t.Helper()
	metadata := types.NewMetadataV14()
	metadata.MagicNumber = types.MagicNumber
	encoded, err := codec.EncodeToHex(metadata)
	if err != nil {
		t.Fatal(err)
	}
	_, digest, err := crv4.DecodeRuntimeMetadata(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return encoded, digest
}

// Assigns the concrete result shapes used by raw GSRPC calls.
func setReleaseRuntimeTestResult(result any, value any) error {
	switch target := result.(type) {
	case *json.RawMessage:
		encoded, err := json.Marshal(value)
		if err != nil {
			return err
		}
		*target = encoded
		return nil
	case *string:
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("release runtime test string has type %T", value)
		}
		*target = text
		return nil
	default:
		return fmt.Errorf("unexpected release runtime result type %T", result)
	}
}

// Uses deterministic, zero-wall-clock waits while retaining production's
// maximum-attempt and cancellation behavior.
func releaseRuntimeImmediateRetryPolicy(wait func(context.Context, time.Duration) error) finalSemanticRPCRetryPolicy {
	return finalSemanticRPCRetryPolicy{
		maximumAttempts: 4, attemptTimeout: time.Second,
		initialRetryDelay: time.Nanosecond, maximumRetryDelay: time.Nanosecond,
		wait: wait,
	}
}

// Reproduces the exact public error: one retry succeeds, then every later block
// rechecks version/code without issuing another large metadata request.
func TestReleaseRuntimeRPCRetryRecognizesHistoricalWorkLimit(t *testing.T) {
	metadataHex, metadataHash := releaseRuntimeTestMetadata(t)
	version := crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 452, TransactionVersion: 1, StateVersion: 1}
	codeHash := "0x" + fmt.Sprintf("%064x", 452)
	identity := crv4.RuntimeArtifactIdentity{Version: version, CodeHash: codeHash, MetadataHash: metadataHash}
	var versionCalls, codeCalls, metadataCalls, waits atomic.Int64
	client := &releaseRuntimeTestClient{callContext: func(_ context.Context, result any, method string, _ ...any) error {
		switch method {
		case "state_getRuntimeVersion":
			versionCalls.Add(1)
			return setReleaseRuntimeTestResult(result, version)
		case "state_getStorageHash":
			codeCalls.Add(1)
			return setReleaseRuntimeTestResult(result, codeHash)
		case "state_getMetadata":
			if metadataCalls.Add(1) == 1 {
				return errors.New("Historical work rate limit exceeded")
			}
			return setReleaseRuntimeTestResult(result, metadataHex)
		default:
			return errors.New("unexpected release runtime RPC")
		}
	}}
	chain := &crv4.Chain{API: &gsrpc.SubstrateAPI{Client: client}}
	policy := releaseRuntimeImmediateRetryPolicy(func(context.Context, time.Duration) error {
		waits.Add(1)
		return nil
	})
	if _, err := readRuntimeArtifactWithPolicy(context.Background(), chain, types.Hash{1}, []crv4.RuntimeArtifactIdentity{identity}, policy); err != nil {
		t.Fatal(err)
	}
	if _, err := readRuntimeArtifactWithPolicy(context.Background(), chain, types.Hash{2}, []crv4.RuntimeArtifactIdentity{identity}, policy); err != nil {
		t.Fatal(err)
	}
	if metadataCalls.Load() != 2 || waits.Load() != 1 || versionCalls.Load() != 3 || codeCalls.Load() != 3 {
		t.Fatalf("runtime calls version=%d code=%d metadata=%d waits=%d, want 3/3/2/1", versionCalls.Load(), codeCalls.Load(), metadataCalls.Load(), waits.Load())
	}
}

// Permanent archive failures and content mismatches fail once rather than
// consuming the bounded transient retry budget.
func TestReleaseRuntimeRPCRetryRejectsPermanentHistoryFailures(t *testing.T) {
	metadataHex, metadataHash := releaseRuntimeTestMetadata(t)
	version := crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 452, TransactionVersion: 1, StateVersion: 1}
	codeHash := "0x" + fmt.Sprintf("%064x", 452)
	for _, test := range []struct {
		name         string
		metadataHash string
		metadataErr  error
	}{
		{name: "discarded archive state", metadataHash: metadataHash, metadataErr: errors.New("state already discarded")},
		{name: "metadata digest mismatch", metadataHash: "0x" + fmt.Sprintf("%064x", 999)},
	} {
		var metadataCalls, waits atomic.Int64
		client := &releaseRuntimeTestClient{callContext: func(_ context.Context, result any, method string, _ ...any) error {
			switch method {
			case "state_getRuntimeVersion":
				return setReleaseRuntimeTestResult(result, version)
			case "state_getStorageHash":
				return setReleaseRuntimeTestResult(result, codeHash)
			case "state_getMetadata":
				metadataCalls.Add(1)
				if test.metadataErr != nil {
					return test.metadataErr
				}
				return setReleaseRuntimeTestResult(result, metadataHex)
			default:
				return errors.New("unexpected release runtime RPC")
			}
		}}
		chain := &crv4.Chain{API: &gsrpc.SubstrateAPI{Client: client}}
		identity := crv4.RuntimeArtifactIdentity{Version: version, CodeHash: codeHash, MetadataHash: test.metadataHash}
		policy := releaseRuntimeImmediateRetryPolicy(func(context.Context, time.Duration) error {
			waits.Add(1)
			return nil
		})
		if _, err := readRuntimeArtifactWithPolicy(context.Background(), chain, types.Hash{1}, []crv4.RuntimeArtifactIdentity{identity}, policy); err == nil {
			t.Fatalf("%s: permanent historical runtime failure was accepted", test.name)
		}
		if metadataCalls.Load() != 1 || waits.Load() != 0 {
			t.Fatalf("%s: permanent failure calls=%d waits=%d, want 1/0", test.name, metadataCalls.Load(), waits.Load())
		}
	}
}

// Cancellation interrupts the retry wait and a later independent invocation
// can populate the cache from a healthy response.
func TestReleaseRuntimeRPCRetryCancellationDoesNotPoisonCache(t *testing.T) {
	metadataHex, metadataHash := releaseRuntimeTestMetadata(t)
	version := crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 452, TransactionVersion: 1, StateVersion: 1}
	codeHash := "0x" + fmt.Sprintf("%064x", 452)
	identity := crv4.RuntimeArtifactIdentity{Version: version, CodeHash: codeHash, MetadataHash: metadataHash}
	var metadataCalls atomic.Int64
	healthy := atomic.Bool{}
	client := &releaseRuntimeTestClient{callContext: func(_ context.Context, result any, method string, _ ...any) error {
		switch method {
		case "state_getRuntimeVersion":
			return setReleaseRuntimeTestResult(result, version)
		case "state_getStorageHash":
			return setReleaseRuntimeTestResult(result, codeHash)
		case "state_getMetadata":
			metadataCalls.Add(1)
			if !healthy.Load() {
				return errors.New("Historical work rate limit exceeded")
			}
			return setReleaseRuntimeTestResult(result, metadataHex)
		default:
			return errors.New("unexpected release runtime RPC")
		}
	}}
	chain := &crv4.Chain{API: &gsrpc.SubstrateAPI{Client: client}}
	retryReached := make(chan struct{})
	policy := releaseRuntimeImmediateRetryPolicy(func(ctx context.Context, _ time.Duration) error {
		close(retryReached)
		<-ctx.Done()
		return ctx.Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := readRuntimeArtifactWithPolicy(ctx, chain, types.Hash{1}, []crv4.RuntimeArtifactIdentity{identity}, policy)
		done <- err
	}()
	<-retryReached
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled history retry error=%v", err)
	}
	healthy.Store(true)
	if _, err := readRuntimeArtifactWithPolicy(context.Background(), chain, types.Hash{2}, []crv4.RuntimeArtifactIdentity{identity}, policy); err != nil {
		t.Fatal(err)
	}
	if metadataCalls.Load() != 2 {
		t.Fatalf("metadata calls=%d, want canceled load plus fresh success", metadataCalls.Load())
	}
}

// A historical cache entry cannot satisfy a strict current-runtime allowlist,
// even when both identities use the same provider connection.
func TestCurrentRuntimeAuthenticationCannotUseHistoricalCache(t *testing.T) {
	metadataHex, metadataHash := releaseRuntimeTestMetadata(t)
	historicalVersion := crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 452, TransactionVersion: 1, StateVersion: 1}
	currentVersion := crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 453, TransactionVersion: 1, StateVersion: 1}
	historicalCodeHash := "0x" + fmt.Sprintf("%064x", 452)
	currentMode := atomic.Bool{}
	var metadataCalls atomic.Int64
	client := &releaseRuntimeTestClient{callContext: func(_ context.Context, result any, method string, _ ...any) error {
		switch method {
		case "state_getRuntimeVersion":
			if currentMode.Load() {
				return setReleaseRuntimeTestResult(result, currentVersion)
			}
			return setReleaseRuntimeTestResult(result, historicalVersion)
		case "state_getStorageHash":
			if currentMode.Load() {
				return setReleaseRuntimeTestResult(result, reviewedRuntimeCodeHash)
			}
			return setReleaseRuntimeTestResult(result, historicalCodeHash)
		case "state_getMetadata":
			metadataCalls.Add(1)
			if currentMode.Load() {
				return errors.New("current metadata download required")
			}
			return setReleaseRuntimeTestResult(result, metadataHex)
		default:
			return errors.New("unexpected release runtime RPC")
		}
	}}
	chain := &crv4.Chain{API: &gsrpc.SubstrateAPI{Client: client}}
	policy := releaseRuntimeImmediateRetryPolicy(func(context.Context, time.Duration) error { return nil })
	historical := crv4.RuntimeArtifactIdentity{Version: historicalVersion, CodeHash: historicalCodeHash, MetadataHash: metadataHash}
	if _, err := readRuntimeArtifactWithPolicy(context.Background(), chain, types.Hash{1}, []crv4.RuntimeArtifactIdentity{historical}, policy); err != nil {
		t.Fatal(err)
	}
	currentMode.Store(true)
	cfg := testResolvedConfig(t)
	if _, err := readAuthenticatedRuntimeMetadataAtContext(context.Background(), chain, cfg, types.Hash{2}); err == nil || !strings.Contains(err.Error(), "current metadata download required") {
		t.Fatal("historical runtime cache satisfied the strict current allowlist")
	}
	if metadataCalls.Load() != 2 {
		t.Fatalf("strict current entrypoint metadata calls=%d, want historical plus independent current load", metadataCalls.Load())
	}
}
