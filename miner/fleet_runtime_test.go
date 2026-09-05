package miner

// fleet_runtime_test.go deterministically exercises the release fleet runtime
// boundary without contacting a public RPC endpoint.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	gsrpcgeth "github.com/centrifuge/go-substrate-rpc-client/v4/gethrpc"
	gsrpcrpc "github.com/centrifuge/go-substrate-rpc-client/v4/rpc"
	gsrpcchain "github.com/centrifuge/go-substrate-rpc-client/v4/rpc/chain"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"gopkg.in/yaml.v3"

	"github.com/urfoundation/sn/crv4"
)

// Supplies only the RPC methods used by the deterministic fleet runtime gate.
type fleetRuntimeTestClient struct {
	callContext func(context.Context, any, string, ...any) error
	close       func()
}

// Lets a deterministic failover test release one synthetic endpoint deadline
// without waiting for wall-clock time.
type fleetRuntimeDeadlineContext struct {
	context.Context
	done <-chan struct{}
}

func (self *fleetRuntimeDeadlineContext) Done() <-chan struct{} {
	return self.done
}

func (self *fleetRuntimeDeadlineContext) Err() error {
	select {
	case <-self.done:
		return context.DeadlineExceeded
	default:
		return nil
	}
}

// Keeps the contextless finalized-head adapter covered by the same fixture.
func (self *fleetRuntimeTestClient) Call(result any, method string, args ...any) error {
	return self.callContext(context.Background(), result, method, args...)
}

// Preserves the caller context through exact-block runtime authentication.
func (self *fleetRuntimeTestClient) CallContext(ctx context.Context, result any, method string, args ...any) error {
	return self.callContext(ctx, result, method, args...)
}

// Fleet runtime reads never subscribe.
func (self *fleetRuntimeTestClient) Subscribe(context.Context, string, string, string, string, any, ...any) (*gsrpcgeth.ClientSubscription, error) {
	return nil, errors.New("unexpected fleet runtime subscription")
}

// Gives failures a stable non-secret endpoint label.
func (self *fleetRuntimeTestClient) URL() string { return "wss://fleet-runtime.test" }

// The test client owns no network connection unless a test records cleanup.
func (self *fleetRuntimeTestClient) Close() {
	if self.close != nil {
		self.close()
	}
}

// Builds valid, deliberately non-release metadata to exercise a hash mismatch.
func fleetRuntimeTestMetadata(t *testing.T) (string, string) {
	t.Helper()
	metadata := types.NewMetadataV14()
	metadata.MagicNumber = types.MagicNumber
	encoded, err := codec.EncodeToHex(metadata)
	if err != nil {
		t.Fatal(err)
	}
	_, hash, err := crv4.DecodeRuntimeMetadata(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return encoded, hash
}

// Sets exactly the raw JSON and string result shapes used by the RPC client.
func setFleetRuntimeTestResult(result any, value any) error {
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
			return fmt.Errorf("test RPC string result has type %T", value)
		}
		*target = text
		return nil
	default:
		return fmt.Errorf("unexpected fleet runtime RPC result type %T", result)
	}
}

// Assembles enough GSRPC surface for a real exact-head read and raw state RPCs.
func fleetRuntimeTestChain(client *fleetRuntimeTestClient) *crv4.Chain {
	return &crv4.Chain{API: &gsrpc.SubstrateAPI{
		Client: client,
		RPC:    &gsrpcrpc.RPC{Chain: gsrpcchain.NewChain(client)},
	}}
}

// Keeps the standalone fleet executable aligned with both checked-in release
// manifests rather than merely with another package's copied literal.
func TestFleetRuntimeArtifactMatchesReleaseManifests(t *testing.T) {
	var lock struct {
		Runtime struct {
			SourceTag          string `yaml:"source_tag"`
			SpecVersion        uint32 `yaml:"spec_version"`
			TransactionVersion uint32 `yaml:"transaction_version"`
			StateVersion       uint8  `yaml:"state_version"`
			CodeHash           string `yaml:"code_hash"`
			MetadataHash       string `yaml:"metadata_hash"`
		} `yaml:"runtime"`
	}
	lockBytes, err := os.ReadFile(filepath.Join("..", "deploy", "testnet", "release.lock.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(lockBytes, &lock); err != nil {
		t.Fatal(err)
	}

	var public struct {
		Chain struct {
			ExpectedRuntimeSpec        uint32 `yaml:"expected_runtime_spec"`
			ExpectedTransactionVersion uint32 `yaml:"expected_transaction_version"`
			ExpectedStateVersion       uint8  `yaml:"expected_state_version"`
			ExpectedBlockSeconds       int    `yaml:"expected_block_seconds"`
		} `yaml:"chain"`
	}
	publicBytes, err := os.ReadFile(filepath.Join("..", "deploy", "testnet", "public.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(publicBytes, &public); err != nil {
		t.Fatal(err)
	}

	artifact := fleetReleaseRuntimeArtifact()
	if lock.Runtime.SourceTag != "v454" || artifact.Version.SpecName != fleetReleaseRuntimeSpecName ||
		artifact.Version.SpecVersion != lock.Runtime.SpecVersion ||
		artifact.Version.TransactionVersion != lock.Runtime.TransactionVersion ||
		artifact.Version.StateVersion != lock.Runtime.StateVersion ||
		artifact.CodeHash != lock.Runtime.CodeHash || artifact.MetadataHash != lock.Runtime.MetadataHash {
		t.Fatalf("fleet artifact %+v does not match release lock runtime %+v", artifact, lock.Runtime)
	}
	if artifact.Version.SpecVersion != public.Chain.ExpectedRuntimeSpec ||
		artifact.Version.TransactionVersion != public.Chain.ExpectedTransactionVersion ||
		artifact.Version.StateVersion != public.Chain.ExpectedStateVersion {
		t.Fatalf("fleet runtime %d/%d/%d does not match public %d/%d/%d", artifact.Version.SpecVersion, artifact.Version.TransactionVersion, artifact.Version.StateVersion, public.Chain.ExpectedRuntimeSpec, public.Chain.ExpectedTransactionVersion, public.Chain.ExpectedStateVersion)
	}
	if public.Chain.ExpectedBlockSeconds != fleetReleaseExpectedBlockSeconds {
		t.Fatalf("fleet expected block seconds %d does not match public %d", fleetReleaseExpectedBlockSeconds, public.Chain.ExpectedBlockSeconds)
	}
	wantEndpointBudget := time.Duration(public.Chain.ExpectedBlockSeconds*fleetNativeAuthenticationBlockBudget) * time.Second
	if fleetNativeEndpointTimeout != wantEndpointBudget {
		t.Fatalf("fleet endpoint budget %s, want %s from %d public blocks", fleetNativeEndpointTimeout, wantEndpointBudget, fleetNativeAuthenticationBlockBudget)
	}
}

// Rejects every adjacent identity deviation before the stale fleet client can
// construct a call or decode storage under unreviewed metadata.
func TestFleetRuntimeAuthenticationRejectsAdjacentArtifactDrift(t *testing.T) {
	metadata, _ := fleetRuntimeTestMetadata(t)
	expected := fleetReleaseRuntimeArtifact()
	cases := []struct {
		name              string
		version           crv4.RuntimeVersionIdentity
		codeHash          string
		metadata          string
		wantCodeCalls     int
		wantMetadataCalls int
	}{
		{name: "former 447 spec", version: crv4.RuntimeVersionIdentity{SpecName: fleetReleaseRuntimeSpecName, SpecVersion: 447, TransactionVersion: 1, StateVersion: 1}, codeHash: expected.CodeHash, metadata: metadata},
		{name: "preceding 453 spec", version: crv4.RuntimeVersionIdentity{SpecName: fleetReleaseRuntimeSpecName, SpecVersion: 453, TransactionVersion: 1, StateVersion: 1}, codeHash: expected.CodeHash, metadata: metadata},
		{name: "transaction version drift", version: crv4.RuntimeVersionIdentity{SpecName: fleetReleaseRuntimeSpecName, SpecVersion: 454, TransactionVersion: 2, StateVersion: 1}, codeHash: expected.CodeHash, metadata: metadata},
		{name: "state version drift", version: crv4.RuntimeVersionIdentity{SpecName: fleetReleaseRuntimeSpecName, SpecVersion: 454, TransactionVersion: 1, StateVersion: 2}, codeHash: expected.CodeHash, metadata: metadata},
		{name: "code hash drift", version: expected.Version, codeHash: "0x825e3d1eca8d5c29c1f0fa6476d5360661b852f52aebad979d6636e227a431ef", metadata: metadata, wantCodeCalls: 1},
		{name: "metadata hash drift", version: expected.Version, codeHash: expected.CodeHash, metadata: metadata, wantCodeCalls: 1, wantMetadataCalls: 1},
	}
	for index, testCase := range cases {
		codeCalls, metadataCalls := 0, 0
		blockHash := types.Hash{byte(index + 1)}
		client := &fleetRuntimeTestClient{callContext: func(_ context.Context, result any, method string, args ...any) error {
			switch method {
			case "state_getRuntimeVersion":
				if len(args) != 1 || args[0] != blockHash.Hex() {
					return fmt.Errorf("runtime version args=%v, want %s", args, blockHash.Hex())
				}
				return setFleetRuntimeTestResult(result, testCase.version)
			case "state_getStorageHash":
				codeCalls++
				if len(args) != 2 || args[0] != "0x3a636f6465" || args[1] != blockHash.Hex() {
					return fmt.Errorf("code hash args=%v, want System.Code at %s", args, blockHash.Hex())
				}
				return setFleetRuntimeTestResult(result, testCase.codeHash)
			case "state_getMetadata":
				metadataCalls++
				if len(args) != 1 || args[0] != blockHash.Hex() {
					return fmt.Errorf("metadata args=%v, want %s", args, blockHash.Hex())
				}
				return setFleetRuntimeTestResult(result, testCase.metadata)
			default:
				return fmt.Errorf("unexpected RPC method %s", method)
			}
		}}
		if _, err := authenticateFleetRuntimeAtContext(context.Background(), fleetRuntimeTestChain(client), blockHash); err == nil {
			t.Errorf("%s was accepted", testCase.name)
		}
		if codeCalls != testCase.wantCodeCalls || metadataCalls != testCase.wantMetadataCalls {
			t.Errorf("%s calls code/metadata=%d/%d, want %d/%d", testCase.name, codeCalls, metadataCalls, testCase.wantCodeCalls, testCase.wantMetadataCalls)
		}
	}
}

// Exercises the exact finalized-head adapter so the former spec-only dial path
// cannot be reintroduced without rejecting it before metadata replacement.
func TestFleetFinalizedRuntimeGateRejectsFormerSpecOnlyPin(t *testing.T) {
	finalized := types.Hash{9}
	oldMetadata := types.NewMetadataV14()
	oldRuntime := &types.RuntimeVersion{SpecName: "node-subtensor", SpecVersion: 447, TransactionVersion: 1}
	codeCalls := 0
	client := &fleetRuntimeTestClient{callContext: func(_ context.Context, result any, method string, args ...any) error {
		switch method {
		case "chain_getFinalizedHead":
			return setFleetRuntimeTestResult(result, finalized.Hex())
		case "state_getRuntimeVersion":
			if len(args) != 1 || args[0] != finalized.Hex() {
				return fmt.Errorf("runtime version args=%v, want %s", args, finalized.Hex())
			}
			return setFleetRuntimeTestResult(result, crv4.RuntimeVersionIdentity{SpecName: fleetReleaseRuntimeSpecName, SpecVersion: 447, TransactionVersion: 1, StateVersion: 1})
		case "state_getStorageHash", "state_getMetadata":
			codeCalls++
			return errors.New("unreviewed runtime reached a later identity RPC")
		default:
			return fmt.Errorf("unexpected RPC method %s", method)
		}
	}}
	chain := fleetRuntimeTestChain(client)
	chain.Meta, chain.Runtime = oldMetadata, oldRuntime
	if _, err := authenticateAndBindFleetRuntimeFinalizedContext(context.Background(), chain); err == nil {
		t.Fatal("former spec-only runtime was accepted at the finalized head")
	}
	if codeCalls != 0 {
		t.Fatalf("former runtime made %d code or metadata reads", codeCalls)
	}
	if chain.Meta != oldMetadata || chain.Runtime != oldRuntime {
		t.Fatal("failed runtime gate mutated the existing chain binding")
	}
}

// Keeps a successful authenticated artifact's metadata and signing versions
// together without trusting a caller-supplied partial RuntimeVersion.
func TestBindFleetRuntimeUsesAuthenticated454Artifact(t *testing.T) {
	metadata := types.NewMetadataV14()
	artifact := crv4.AuthenticatedRuntimeArtifact{
		BlockHash:    types.Hash{1},
		Version:      fleetReleaseRuntimeArtifact().Version,
		CodeHash:     fleetReleaseRuntimeCodeHash,
		MetadataHash: fleetReleaseRuntimeMetadataHash,
		Metadata:     metadata,
	}
	chain := &crv4.Chain{}
	if err := bindFleetRuntime(chain, artifact); err != nil {
		t.Fatal(err)
	}
	if chain.Meta != metadata || chain.Runtime == nil || chain.Runtime.SpecName != fleetReleaseRuntimeSpecName ||
		uint32(chain.Runtime.SpecVersion) != fleetReleaseRuntimeSpecVersion || uint32(chain.Runtime.TransactionVersion) != fleetReleaseRuntimeTransactionVersion {
		t.Fatalf("bound fleet chain metadata/runtime = %p/%+v", chain.Meta, chain.Runtime)
	}
}

// A rejected artifact must leave the last reviewed metadata and signing
// version intact, including an adjacent code-hash mismatch.
func TestBindFleetRuntimeRejectsUnreviewedArtifactWithoutMutation(t *testing.T) {
	oldMetadata, newMetadata := types.NewMetadataV14(), types.NewMetadataV14()
	oldRuntime := &types.RuntimeVersion{SpecName: "old-runtime", SpecVersion: 7, TransactionVersion: 8}
	chain := &crv4.Chain{Meta: oldMetadata, Runtime: oldRuntime}
	artifact := crv4.AuthenticatedRuntimeArtifact{
		BlockHash:    types.Hash{1},
		Version:      fleetReleaseRuntimeArtifact().Version,
		CodeHash:     "0x825e3d1eca8d5c29c1f0fa6476d5360661b852f52aebad979d6636e227a431ef",
		MetadataHash: fleetReleaseRuntimeMetadataHash,
		Metadata:     newMetadata,
	}
	if err := bindFleetRuntime(chain, artifact); err == nil {
		t.Fatal("unreviewed code hash bound fleet metadata")
	}
	if chain.Meta != oldMetadata || chain.Runtime != oldRuntime {
		t.Fatal("unreviewed artifact changed the prior chain binding")
	}
}

// Concurrent exact-block authentication shares one immutable metadata decode;
// every block still makes its own version and code check before the shared
// result can be returned. Explicit barriers make the race proof deterministic.
func TestFleetRuntimeArtifactAuthenticationSharesMetadataAcrossConcurrentHeads(t *testing.T) {
	metadata, metadataHash := fleetRuntimeTestMetadata(t)
	expected := fleetReleaseRuntimeArtifact()
	expected.MetadataHash = metadataHash
	const callers = 8
	codeReached := make(chan struct{}, callers)
	metadataStarted := make(chan struct{})
	metadataRelease := make(chan struct{})
	var metadataCalls atomic.Int64
	client := &fleetRuntimeTestClient{callContext: func(ctx context.Context, result any, method string, args ...any) error {
		switch method {
		case "state_getRuntimeVersion":
			return setFleetRuntimeTestResult(result, expected.Version)
		case "state_getStorageHash":
			codeReached <- struct{}{}
			return setFleetRuntimeTestResult(result, expected.CodeHash)
		case "state_getMetadata":
			if metadataCalls.Add(1) != 1 {
				return errors.New("metadata was fetched more than once")
			}
			close(metadataStarted)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-metadataRelease:
				return setFleetRuntimeTestResult(result, metadata)
			}
		default:
			return fmt.Errorf("unexpected RPC method %s args=%v", method, args)
		}
	}}
	chain := fleetRuntimeTestChain(client)
	resultErrors := make(chan error, callers)
	for index := 1; index <= callers; index++ {
		blockHash := types.Hash{byte(index)}
		go func() {
			_, err := crv4.AuthenticateRuntimeArtifactAtContext(context.Background(), chain, blockHash, expected)
			resultErrors <- err
		}()
	}
	for index := 0; index < callers; index++ {
		<-codeReached
	}
	<-metadataStarted
	close(metadataRelease)
	for index := 0; index < callers; index++ {
		if err := <-resultErrors; err != nil {
			t.Fatal(err)
		}
	}
	if metadataCalls.Load() != 1 {
		t.Fatalf("metadata calls=%d, want one", metadataCalls.Load())
	}
}

// Each ordered endpoint gets a finite dial/authentication budget, and the
// exact finalized-head RPC sees caller cancellation rather than Background.
func TestDialFleetNativeContextBindsDeadlineThroughRuntimeAuthentication(t *testing.T) {
	authStarted := make(chan struct{})
	authDeadline := make(chan time.Time, 1)
	transportClosed := make(chan struct{}, 1)
	client := &fleetRuntimeTestClient{callContext: func(ctx context.Context, _ any, method string, args ...any) error {
		if method != "chain_getFinalizedHead" || len(args) != 0 {
			return fmt.Errorf("unexpected dial runtime RPC %s args=%v", method, args)
		}
		deadline, ok := ctx.Deadline()
		if !ok {
			return errors.New("runtime authentication received no endpoint deadline")
		}
		authDeadline <- deadline
		close(authStarted)
		<-ctx.Done()
		return ctx.Err()
	}, close: func() { transportClosed <- struct{}{} }}
	dial := func(_ context.Context, endpoint string) (*crv4.Chain, error) {
		if endpoint != "wss://bounded-fleet.test" {
			return nil, fmt.Errorf("unexpected endpoint %s", endpoint)
		}
		return fleetRuntimeTestChain(client), nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, _, err := dialFleetNativeWithContext(ctx, []string{"wss://bounded-fleet.test"}, time.Minute, dial)
		done <- err
	}()
	<-authStarted
	if deadline := <-authDeadline; deadline.IsZero() {
		t.Fatal("runtime authentication deadline is zero")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled fleet dial error=%v", err)
	}
	select {
	case <-transportClosed:
	default:
		t.Fatal("failed runtime authentication left its endpoint transport open")
	}
}

// A provider whose dial/authentication work exceeds its own budget must not
// consume the operation context or prevent the next ordered provider from
// becoming usable. The manual deadline makes this failover deterministic.
func TestDialFleetNativeContextFailsOverAfterSlowEndpointDeadline(t *testing.T) {
	firstStarted := make(chan struct{})
	firstExpired := make(chan struct{})
	endpointContexts := 0
	var endpointTimeouts []time.Duration
	endpointContext := func(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
		endpointContexts++
		endpointTimeouts = append(endpointTimeouts, timeout)
		if endpointContexts == 1 {
			return &fleetRuntimeDeadlineContext{Context: parent, done: firstExpired}, func() {}
		}
		return context.WithCancel(parent)
	}
	var endpoints []string
	dial := func(ctx context.Context, endpoint string) (*crv4.Chain, error) {
		endpoints = append(endpoints, endpoint)
		switch endpoint {
		case "wss://slow-fleet.test":
			close(firstStarted)
			<-ctx.Done()
			return nil, ctx.Err()
		case "wss://healthy-fleet.test":
			return &crv4.Chain{}, nil
		default:
			return nil, fmt.Errorf("unexpected endpoint %s", endpoint)
		}
	}
	done := make(chan struct {
		chain    *crv4.Chain
		endpoint string
		err      error
	}, 1)
	go func() {
		chain, endpoint, err := dialFleetNativeWithEndpointContext(context.Background(), []string{"wss://slow-fleet.test", "wss://healthy-fleet.test"}, fleetNativeEndpointTimeout, endpointContext, dial, func(context.Context, *crv4.Chain) error {
			return nil
		})
		done <- struct {
			chain    *crv4.Chain
			endpoint string
			err      error
		}{chain: chain, endpoint: endpoint, err: err}
	}()
	<-firstStarted
	close(firstExpired)
	result := <-done
	if result.err != nil || result.chain == nil || result.endpoint != "wss://healthy-fleet.test" {
		t.Fatalf("failover result chain=%p endpoint=%q err=%v", result.chain, result.endpoint, result.err)
	}
	if endpointContexts != 2 || len(endpointTimeouts) != 2 || endpointTimeouts[0] != fleetNativeEndpointTimeout || endpointTimeouts[1] != fleetNativeEndpointTimeout || len(endpoints) != 2 || endpoints[0] != "wss://slow-fleet.test" || endpoints[1] != "wss://healthy-fleet.test" {
		t.Fatalf("failover contexts=%d timeouts=%v endpoints=%v", endpointContexts, endpointTimeouts, endpoints)
	}
}

// The operation deadline is an upper bound even when the per-provider budget
// is larger. A canceled parent must also prevent a fallback dial from starting.
func TestDialFleetNativeContextClampsEndpointDeadlineToParent(t *testing.T) {
	parentDeadline := time.Now().Add(time.Hour)
	ctx, cancel := context.WithDeadline(context.Background(), parentDeadline)
	defer cancel()
	started := make(chan struct{})
	endpointDeadline := make(chan time.Time, 1)
	dialCalls := 0
	dial := func(endpointCtx context.Context, endpoint string) (*crv4.Chain, error) {
		dialCalls++
		if endpoint != "wss://parent-deadline.test" {
			return nil, fmt.Errorf("unexpected fallback endpoint %s", endpoint)
		}
		deadline, ok := endpointCtx.Deadline()
		if !ok {
			return nil, errors.New("endpoint omitted parent deadline")
		}
		endpointDeadline <- deadline
		close(started)
		<-endpointCtx.Done()
		return nil, endpointCtx.Err()
	}
	done := make(chan error, 1)
	go func() {
		_, _, err := dialFleetNativeWithContext(ctx, []string{"wss://parent-deadline.test", "wss://must-not-run.test"}, 2*time.Hour, dial)
		done <- err
	}()
	<-started
	if deadline := <-endpointDeadline; !deadline.Equal(parentDeadline) {
		t.Fatalf("endpoint deadline=%s, want parent deadline=%s", deadline, parentDeadline)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("parent-canceled fleet dial error=%v", err)
	}
	if dialCalls != 1 {
		t.Fatalf("parent cancellation started %d dials, want one", dialCalls)
	}
}
