// Runtime identity tests exercise exact-block RPC binding and the immutable
// metadata artifact cache without depending on a live Subtensor endpoint.
package crv4

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	gsrpcgeth "github.com/centrifuge/go-substrate-rpc-client/v4/gethrpc"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
)

// Supplies only the three runtime RPCs needed by these deterministic tests.
type runtimeIdentityTestClient struct {
	callContext func(context.Context, any, string, ...any) error
}

// Keeps accidental contextless production regressions visible in tests.
func (self *runtimeIdentityTestClient) Call(result any, method string, args ...any) error {
	return self.callContext(context.Background(), result, method, args...)
}

// Delegates one scripted RPC while preserving the exact caller context.
func (self *runtimeIdentityTestClient) CallContext(ctx context.Context, result any, method string, args ...any) error {
	return self.callContext(ctx, result, method, args...)
}

// Runtime identity reads never subscribe.
func (self *runtimeIdentityTestClient) Subscribe(context.Context, string, string, string, string, any, ...any) (*gsrpcgeth.ClientSubscription, error) {
	return nil, errors.New("unexpected runtime identity subscription")
}

// Gives diagnostics a stable non-secret endpoint identity.
func (self *runtimeIdentityTestClient) URL() string { return "wss://runtime-identity.test" }

// Test clients own no external connection.
func (self *runtimeIdentityTestClient) Close() {}

// Produces a small valid metadata-v14 response and its exact raw-byte digest.
func runtimeIdentityTestMetadata(t *testing.T) (string, string) {
	t.Helper()
	metadata := types.NewMetadataV14()
	metadata.MagicNumber = types.MagicNumber
	encoded, err := codec.EncodeToHex(metadata)
	if err != nil {
		t.Fatal(err)
	}
	_, digest, err := DecodeRuntimeMetadata(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return encoded, digest
}

// Assigns the concrete result shapes used by the raw GSRPC client interface.
func setRuntimeIdentityTestResult(result any, value any) error {
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
	case **string:
		if value == nil {
			*target = nil
			return nil
		}
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("test RPC nullable string result has type %T", value)
		}
		*target = &text
		return nil
	default:
		return fmt.Errorf("unexpected test RPC result type %T", result)
	}
}

// Both RPC names describe the same state layout; a proxy or runtime reply
// cannot choose a different layout by supplying contradictory aliases.
func TestRuntimeVersionRejectsContradictoryStateAliases(t *testing.T) {
	base := `{"specName":"node-subtensor","specVersion":471,"transactionVersion":1,"stateVersion":1,`
	for _, test := range []struct {
		name      string
		raw       string
		wantError bool
	}{
		{name: "matching", raw: base + `"systemVersion":1}`},
		{name: "contradictory", raw: base + `"systemVersion":0}`, wantError: true},
		{name: "invalid", raw: base + `"systemVersion":"1"}`, wantError: true},
		{name: "duplicate", raw: base + `"systemVersion":1,"systemVersion":0}`, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			version, err := DecodeRuntimeVersionIdentity(json.RawMessage(test.raw))
			if (err != nil) != test.wantError {
				t.Fatalf("decode aliases: version=%+v err=%v", version, err)
			}
			if err == nil && version.StateVersion != 1 {
				t.Fatalf("matching aliases changed state layout: %+v", version)
			}
		})
	}
}

// Shares one metadata download across concurrent blocks carrying the same
// complete runtime artifact while retaining per-block version and code reads.
func TestRuntimeArtifactMetadataIsSingleflightedAcrossExactBlocks(t *testing.T) {
	metadataHex, metadataHash := runtimeIdentityTestMetadata(t)
	version := RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 452, TransactionVersion: 1, StateVersion: 1}
	codeHash := "0x" + fmt.Sprintf("%064x", 452)
	identity := RuntimeArtifactIdentity{Version: version, CodeHash: codeHash, MetadataHash: metadataHash}
	codeReached := make(chan struct{}, 8)
	metadataRelease := make(chan struct{})
	var versionCalls, codeCalls, metadataCalls atomic.Int64
	client := &runtimeIdentityTestClient{callContext: func(ctx context.Context, result any, method string, args ...any) error {
		switch method {
		case "state_getRuntimeVersion":
			versionCalls.Add(1)
			return setRuntimeIdentityTestResult(result, version)
		case "state_getStorageHash":
			codeCalls.Add(1)
			codeReached <- struct{}{}
			return setRuntimeIdentityTestResult(result, codeHash)
		case "state_getMetadata":
			if metadataCalls.Add(1) != 1 {
				return errors.New("Historical work rate limit exceeded")
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-metadataRelease:
				return setRuntimeIdentityTestResult(result, metadataHex)
			}
		default:
			return fmt.Errorf("unexpected method %s args=%v", method, args)
		}
	}}
	chain := &Chain{API: &gsrpc.SubstrateAPI{Client: client}}
	type result struct {
		artifact AuthenticatedRuntimeArtifact
		err      error
	}
	results := make(chan result, 8)
	for index := 1; index <= 8; index++ {
		blockHash := types.Hash{byte(index)}
		go func() {
			artifact, err := AuthenticateRuntimeArtifactAtContext(context.Background(), chain, blockHash, identity)
			results <- result{artifact: artifact, err: err}
		}()
	}
	for index := 0; index < 8; index++ {
		<-codeReached
	}
	close(metadataRelease)
	var sharedMetadata *types.Metadata
	for index := 0; index < 8; index++ {
		result := <-results
		if result.err != nil {
			t.Fatal(result.err)
		}
		if sharedMetadata == nil {
			sharedMetadata = result.artifact.Metadata
		} else if result.artifact.Metadata != sharedMetadata {
			t.Fatal("same runtime artifact returned distinct decoded metadata objects")
		}
	}
	if versionCalls.Load() != 8 || codeCalls.Load() != 8 || metadataCalls.Load() != 1 {
		t.Fatalf("runtime RPC calls version=%d code=%d metadata=%d, want 8/8/1", versionCalls.Load(), codeCalls.Load(), metadataCalls.Load())
	}
}

// A transient or malformed load cannot populate the immutable artifact cache.
func TestRuntimeArtifactMetadataFailureDoesNotPoisonCache(t *testing.T) {
	metadataHex, metadataHash := runtimeIdentityTestMetadata(t)
	version := RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 452, TransactionVersion: 1, StateVersion: 1}
	codeHash := "0x" + fmt.Sprintf("%064x", 452)
	identity := RuntimeArtifactIdentity{Version: version, CodeHash: codeHash, MetadataHash: metadataHash}
	var metadataCalls atomic.Int64
	client := &runtimeIdentityTestClient{callContext: func(_ context.Context, result any, method string, _ ...any) error {
		switch method {
		case "state_getRuntimeVersion":
			return setRuntimeIdentityTestResult(result, version)
		case "state_getStorageHash":
			return setRuntimeIdentityTestResult(result, codeHash)
		case "state_getMetadata":
			if metadataCalls.Add(1) == 1 {
				return errors.New("Historical work rate limit exceeded")
			}
			return setRuntimeIdentityTestResult(result, metadataHex)
		default:
			return errors.New("unexpected test RPC")
		}
	}}
	chain := &Chain{API: &gsrpc.SubstrateAPI{Client: client}}
	if _, err := AuthenticateRuntimeArtifactAtContext(context.Background(), chain, types.Hash{1}, identity); err == nil {
		t.Fatal("provider throttle was cached as a successful artifact")
	}
	if _, err := AuthenticateRuntimeArtifactAtContext(context.Background(), chain, types.Hash{2}, identity); err != nil {
		t.Fatal(err)
	}
	if _, err := AuthenticateRuntimeArtifactAtContext(context.Background(), chain, types.Hash{3}, identity); err != nil {
		t.Fatal(err)
	}
	if metadataCalls.Load() != 2 {
		t.Fatalf("metadata calls=%d, want one failed load and one cached success", metadataCalls.Load())
	}
}

// Canceling a follower cannot cancel the leader or poison its eventual cache
// entry, and the follower does not wait for an unrelated caller's RPC.
func TestRuntimeArtifactMetadataCanceledFollowerLeavesSharedLoadIntact(t *testing.T) {
	metadataHex, metadataHash := runtimeIdentityTestMetadata(t)
	version := RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 452, TransactionVersion: 1, StateVersion: 1}
	codeHash := "0x" + fmt.Sprintf("%064x", 452)
	identity := RuntimeArtifactIdentity{Version: version, CodeHash: codeHash, MetadataHash: metadataHash}
	metadataStarted := make(chan struct{})
	metadataRelease := make(chan struct{})
	var metadataCalls atomic.Int64
	client := &runtimeIdentityTestClient{callContext: func(ctx context.Context, result any, method string, _ ...any) error {
		switch method {
		case "state_getRuntimeVersion":
			return setRuntimeIdentityTestResult(result, version)
		case "state_getStorageHash":
			return setRuntimeIdentityTestResult(result, codeHash)
		case "state_getMetadata":
			metadataCalls.Add(1)
			close(metadataStarted)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-metadataRelease:
				return setRuntimeIdentityTestResult(result, metadataHex)
			}
		default:
			return errors.New("unexpected test RPC")
		}
	}}
	chain := &Chain{API: &gsrpc.SubstrateAPI{Client: client}}
	leaderDone := make(chan error, 1)
	go func() {
		_, err := AuthenticateRuntimeArtifactAtContext(context.Background(), chain, types.Hash{1}, identity)
		leaderDone <- err
	}()
	<-metadataStarted
	followerCtx, cancelFollower := context.WithCancel(context.Background())
	followerDone := make(chan error, 1)
	go func() {
		_, err := AuthenticateRuntimeArtifactAtContext(followerCtx, chain, types.Hash{2}, identity)
		followerDone <- err
	}()
	cancelFollower()
	if err := <-followerDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled follower error=%v", err)
	}
	close(metadataRelease)
	if err := <-leaderDone; err != nil {
		t.Fatal(err)
	}
	if _, err := AuthenticateRuntimeArtifactAtContext(context.Background(), chain, types.Hash{3}, identity); err != nil {
		t.Fatal(err)
	}
	if metadataCalls.Load() != 1 {
		t.Fatalf("metadata calls=%d, want one intact shared load", metadataCalls.Load())
	}
}

// A caller cancellation reaches the raw metadata request itself; a provider
// cannot retain the goroutine until its own transport timeout expires.
func TestRuntimeMetadataAtContextCancelsProviderRead(t *testing.T) {
	metadataStarted := make(chan struct{})
	client := &runtimeIdentityTestClient{callContext: func(ctx context.Context, _ any, method string, args ...any) error {
		if method != "state_getMetadata" || len(args) != 1 || args[0] != (types.Hash{1}).Hex() {
			return fmt.Errorf("unexpected metadata RPC %s args=%v", method, args)
		}
		close(metadataStarted)
		<-ctx.Done()
		return ctx.Err()
	}}
	chain := &Chain{API: &gsrpc.SubstrateAPI{Client: client}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, _, err := RuntimeMetadataAtContext(ctx, chain, types.Hash{1})
		done <- err
	}()
	<-metadataStarted
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled runtime metadata read error=%v", err)
	}
}

// A cache hit never bypasses the requested block's version or code identity.
func TestRuntimeArtifactMetadataCacheRejectsBlockIdentityDrift(t *testing.T) {
	metadataHex, metadataHash := runtimeIdentityTestMetadata(t)
	version := RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 452, TransactionVersion: 1, StateVersion: 1}
	codeHash := "0x" + fmt.Sprintf("%064x", 452)
	identity := RuntimeArtifactIdentity{Version: version, CodeHash: codeHash, MetadataHash: metadataHash}
	var metadataCalls atomic.Int64
	client := &runtimeIdentityTestClient{callContext: func(_ context.Context, result any, method string, args ...any) error {
		blockHash, _ := args[len(args)-1].(string)
		switch method {
		case "state_getRuntimeVersion":
			if blockHash == (types.Hash{3}).Hex() {
				return setRuntimeIdentityTestResult(result, RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 454, TransactionVersion: 1, StateVersion: 1})
			}
			return setRuntimeIdentityTestResult(result, version)
		case "state_getStorageHash":
			if blockHash == (types.Hash{2}).Hex() {
				return setRuntimeIdentityTestResult(result, "0x"+fmt.Sprintf("%064x", 999))
			}
			return setRuntimeIdentityTestResult(result, codeHash)
		case "state_getMetadata":
			metadataCalls.Add(1)
			return setRuntimeIdentityTestResult(result, metadataHex)
		default:
			return errors.New("unexpected test RPC")
		}
	}}
	chain := &Chain{API: &gsrpc.SubstrateAPI{Client: client}}
	if _, err := AuthenticateRuntimeArtifactAtContext(context.Background(), chain, types.Hash{1}, identity); err != nil {
		t.Fatal(err)
	}
	if _, err := AuthenticateRuntimeArtifactAtContext(context.Background(), chain, types.Hash{2}, identity); err == nil {
		t.Fatal("wrong code hash reused cached metadata")
	}
	if _, err := AuthenticateRuntimeArtifactAtContext(context.Background(), chain, types.Hash{3}, identity); err == nil {
		t.Fatal("unreviewed version reused cached metadata")
	}
	if metadataCalls.Load() != 1 {
		t.Fatalf("identity drift reached metadata cache loader: calls=%d", metadataCalls.Load())
	}
}

// Independently dialed providers cannot inherit each other's authenticated
// bytes even when the expected artifact tuple is identical.
func TestRuntimeArtifactMetadataCacheSeparatesChains(t *testing.T) {
	metadataHex, metadataHash := runtimeIdentityTestMetadata(t)
	version := RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 452, TransactionVersion: 1, StateVersion: 1}
	codeHash := "0x" + fmt.Sprintf("%064x", 452)
	identity := RuntimeArtifactIdentity{Version: version, CodeHash: codeHash, MetadataHash: metadataHash}
	newChain := func(metadataErr error, metadataCalls *atomic.Int64) *Chain {
		client := &runtimeIdentityTestClient{callContext: func(_ context.Context, result any, method string, _ ...any) error {
			switch method {
			case "state_getRuntimeVersion":
				return setRuntimeIdentityTestResult(result, version)
			case "state_getStorageHash":
				return setRuntimeIdentityTestResult(result, codeHash)
			case "state_getMetadata":
				metadataCalls.Add(1)
				if metadataErr != nil {
					return metadataErr
				}
				return setRuntimeIdentityTestResult(result, metadataHex)
			default:
				return errors.New("unexpected test RPC")
			}
		}}
		return &Chain{API: &gsrpc.SubstrateAPI{Client: client}}
	}
	var firstMetadataCalls, secondMetadataCalls atomic.Int64
	first := newChain(nil, &firstMetadataCalls)
	second := newChain(errors.New("state already discarded"), &secondMetadataCalls)
	if _, err := AuthenticateRuntimeArtifactAtContext(context.Background(), first, types.Hash{1}, identity); err != nil {
		t.Fatal(err)
	}
	if _, err := AuthenticateRuntimeArtifactAtContext(context.Background(), second, types.Hash{2}, identity); err == nil {
		t.Fatal("second provider inherited the first provider's metadata cache")
	}
	if firstMetadataCalls.Load() != 1 || secondMetadataCalls.Load() != 1 {
		t.Fatalf("per-provider metadata calls=%d/%d, want 1/1", firstMetadataCalls.Load(), secondMetadataCalls.Load())
	}
}

// Rejects an oversized caller allowlist before any provider request or cache
// allocation can turn the release helper into an unbounded store.
func TestRuntimeArtifactMetadataCacheIsHardBounded(t *testing.T) {
	var calls atomic.Int64
	client := &runtimeIdentityTestClient{callContext: func(context.Context, any, string, ...any) error {
		calls.Add(1)
		return errors.New("unexpected test RPC")
	}}
	identities := make([]RuntimeArtifactIdentity, maximumRuntimeMetadataArtifactsPerChain+1)
	for index := range identities {
		identities[index] = RuntimeArtifactIdentity{
			Version:  RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: uint32(450 + index), TransactionVersion: 1, StateVersion: 1},
			CodeHash: "0x" + fmt.Sprintf("%064x", index+1), MetadataHash: "0x" + fmt.Sprintf("%064x", index+11),
		}
	}
	chain := &Chain{API: &gsrpc.SubstrateAPI{Client: client}}
	if _, err := AuthenticateRuntimeArtifactAtContext(context.Background(), chain, types.Hash{1}, identities...); err == nil {
		t.Fatal("oversized runtime artifact allowlist was accepted")
	}
	if calls.Load() != 0 {
		t.Fatalf("oversized allowlist made %d provider calls", calls.Load())
	}
}

// A long-lived provider keeps admitting exact upgrades while evicting its
// least recently used metadata. Historical reads reauthenticate after eviction.
func TestRuntimeArtifactMetadataCacheEvictsWithoutBlockingUpgrades(t *testing.T) {
	metadataHex, metadataHash := runtimeIdentityTestMetadata(t)
	var metadataCalls atomic.Int64
	client := &runtimeIdentityTestClient{callContext: func(_ context.Context, result any, method string, args ...any) error {
		blockHex, ok := args[len(args)-1].(string)
		if !ok {
			return errors.New("runtime identity block argument is not a string")
		}
		blockHash, err := types.NewHashFromHexString(blockHex)
		if err != nil {
			return err
		}
		spec := uint32(450) + uint32(blockHash[0])
		switch method {
		case "state_getRuntimeVersion":
			return setRuntimeIdentityTestResult(result, RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: spec, TransactionVersion: 1, StateVersion: 1})
		case "state_getStorageHash":
			return setRuntimeIdentityTestResult(result, "0x"+fmt.Sprintf("%064x", spec))
		case "state_getMetadata":
			metadataCalls.Add(1)
			return setRuntimeIdentityTestResult(result, metadataHex)
		default:
			return errors.New("unexpected test RPC")
		}
	}}
	chain := &Chain{API: &gsrpc.SubstrateAPI{Client: client}}
	read := func(offset int) {
		t.Helper()
		spec := uint32(450) + uint32(offset)
		identity := RuntimeArtifactIdentity{
			Version:  RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: spec, TransactionVersion: 1, StateVersion: 1},
			CodeHash: "0x" + fmt.Sprintf("%064x", spec), MetadataHash: metadataHash,
		}
		_, err := AuthenticateRuntimeArtifactAtContext(context.Background(), chain, types.Hash{byte(offset)}, identity)
		if err != nil {
			t.Fatalf("artifact %d failed authentication: %v", offset, err)
		}
	}
	for offset := 1; offset <= maximumRuntimeMetadataCacheEntries; offset++ {
		read(offset)
	}
	read(1) // Keep the oldest version hot before admitting a successor.
	read(maximumRuntimeMetadataCacheEntries + 1)
	read(1)
	if metadataCalls.Load() != int64(maximumRuntimeMetadataCacheEntries+1) {
		t.Fatalf("hot artifact was evicted: metadata calls=%d", metadataCalls.Load())
	}
	read(2) // The least recently used historical artifact must be fetched again.
	if metadataCalls.Load() != int64(maximumRuntimeMetadataCacheEntries+2) {
		t.Fatalf("evicted artifact was not reauthenticated: metadata calls=%d", metadataCalls.Load())
	}
	if got := len(chain.runtimeMetadataArtifactCache().identityEntries); got != maximumRuntimeMetadataCacheEntries {
		t.Fatalf("resident metadata entries=%d, want %d", got, maximumRuntimeMetadataCacheEntries)
	}
}

// An all-in-flight cache must not reject a new finalized runtime or evict an
// unfinished load. An uncached read still checks its exact metadata digest.
func TestRuntimeArtifactMetadataCacheAllowsUncachedReadWhenAllSlotsLoad(t *testing.T) {
	cache := newRuntimeMetadataArtifactCache()
	encoded, metadataHash := runtimeIdentityTestMetadata(t)
	metadata, _, err := DecodeRuntimeMetadata(encoded)
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{}, maximumRuntimeMetadataCacheEntries)
	release := make(chan struct{})
	done := make(chan error, maximumRuntimeMetadataCacheEntries)
	for index := 0; index < maximumRuntimeMetadataCacheEntries; index++ {
		identity := RuntimeArtifactIdentity{Version: RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: uint32(index + 1)}, MetadataHash: metadataHash}
		go func() {
			_, _, err := cache.load(context.Background(), identity, func(context.Context) (*types.Metadata, string, error) {
				started <- struct{}{}
				<-release
				return metadata, metadataHash, nil
			})
			done <- err
		}()
	}
	for index := 0; index < maximumRuntimeMetadataCacheEntries; index++ {
		<-started
	}
	identity := RuntimeArtifactIdentity{Version: RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 999}, MetadataHash: metadataHash}
	if _, _, err := cache.load(context.Background(), identity, func(context.Context) (*types.Metadata, string, error) {
		return metadata, "0x" + fmt.Sprintf("%064x", 2), nil
	}); err == nil {
		t.Fatal("uncached admission accepted changed metadata")
	}
	if _, _, err := cache.load(context.Background(), identity, func(context.Context) (*types.Metadata, string, error) {
		return metadata, metadataHash, nil
	}); err != nil {
		t.Fatalf("uncached exact artifact was rejected: %v", err)
	}
	if got := len(cache.identityEntries); got != maximumRuntimeMetadataCacheEntries {
		t.Fatalf("in-flight cache entries=%d, want %d", got, maximumRuntimeMetadataCacheEntries)
	}
	close(release)
	for index := 0; index < maximumRuntimeMetadataCacheEntries; index++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
}
