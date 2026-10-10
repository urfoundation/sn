// The real read-only admission rejects network, artifact and historical drift
// before it can rebind a private view or publish a successful observation.
package validator

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/v2026/crv4"
)

// Every coordinate matters independently, including a compatible-looking spec
// and a correct artifact observed outside its expressly approved block window.
func TestMainnetRuntimeObservationRejectsFreshIdentityAndArtifactDrift(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		change func(*mainnetRuntimeTestFixture)
	}{
		{"chain name", func(value *mainnetRuntimeTestFixture) { value.nativeChain = "Other Network" }},
		{"fresh genesis", func(value *mainnetRuntimeTestFixture) { value.genesis = types.Hash{4} }},
		{"connection genesis", func(value *mainnetRuntimeTestFixture) { value.native.GenesisHash = types.Hash{4} }},
		{"EVM chain", func(value *mainnetRuntimeTestFixture) { value.evmChainId = "0x3b1" }},
		{"runtime name", func(value *mainnetRuntimeTestFixture) { value.versions[1].SpecName = "other" }},
		{"spec", func(value *mainnetRuntimeTestFixture) { value.versions[1].SpecVersion++ }},
		{"transaction", func(value *mainnetRuntimeTestFixture) { value.versions[1].TransactionVersion++ }},
		{"state", func(value *mainnetRuntimeTestFixture) { value.versions[1].StateVersion++ }},
		{"code", func(value *mainnetRuntimeTestFixture) { value.codes[1] = (types.Hash{0x99}).Hex() }},
		{"metadata", func(value *mainnetRuntimeTestFixture) { value.metadata = "0x00" }},
		{"outside approved interval", func(value *mainnetRuntimeTestFixture) { value.head = 201 }},
		{"noncanonical head", func(value *mainnetRuntimeTestFixture) { value.canonicalHashKVs[150] = types.Hash{9} }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newMainnetRuntimeTestFixture(t)
			cfg, err := LoadMainnetRuntimeObservationConfig(fixture.path)
			if err != nil {
				t.Fatal(err)
			}
			oldMetadata, oldRuntime := fixture.native.Meta, fixture.native.Runtime
			testCase.change(fixture)
			if got, err := ObserveMainnetRuntimeAtContext(context.Background(), fixture.native, cfg, types.Hash{}); err == nil || got != nil {
				t.Fatalf("drift produced an observation: %+v err=%v", got, err)
			}
			if fixture.native.Meta != oldMetadata || fixture.native.Runtime != oldRuntime {
				t.Fatal("failed observation rebound the native connection")
			}
		})
	}
}

// A cached successful metadata read is no substitute for current chain identity
// or exact code authentication at the next requested finalized block.
func TestMainnetRuntimeObservationCacheCannotAuthorizeNetworkDrift(t *testing.T) {
	fixture := newMainnetRuntimeTestFixture(t)
	cfg, err := LoadMainnetRuntimeObservationConfig(fixture.path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ObserveMainnetRuntimeAtContext(context.Background(), fixture.native, cfg, types.Hash{}); err != nil {
		t.Fatal(err)
	}
	fixture.genesis = types.Hash{7}
	if got, err := ObserveMainnetRuntimeAtContext(context.Background(), fixture.native, cfg, types.Hash{}); err == nil || got != nil {
		t.Fatal("warm metadata cache authorized changed genesis")
	}
	fixture.genesis = fixture.native.GenesisHash
	fixture.codes[1] = (types.Hash{8}).Hex()
	if got, err := ObserveMainnetRuntimeAtContext(context.Background(), fixture.native, cfg, types.Hash{}); err == nil || got != nil {
		t.Fatal("warm metadata cache authorized changed runtime code")
	}
	if fixture.callKVs["state_getMetadata"] != 1 || fixture.callKVs["state_getStorageHash"] != 2 || fixture.callKVs["system_chain"] != 3 {
		t.Fatalf("unexpected fresh checks or metadata reuse: %v", fixture.callKVs)
	}
}

// History never grants the current artifact to earlier blocks, and a header
// ahead of finality or a changed closing canonical hash yields no evidence.
func TestMainnetRuntimeObservationRejectsHistoricalRelabelingAndFinalityDrift(t *testing.T) {
	fixture := newMainnetRuntimeTestFixture(t)
	cfg, err := LoadMainnetRuntimeObservationConfig(fixture.path)
	if err != nil {
		t.Fatal(err)
	}
	fixture.versions[0] = fixture.versions[1]
	fixture.codes[0] = fixture.codes[1]
	if got, err := ObserveMainnetRuntimeAtContext(context.Background(), fixture.native, cfg, mainnetRuntimeTestBlock(100)); err == nil || got != nil {
		t.Fatal("successor artifact relabeled earlier approval interval")
	}
	if got, err := ObserveMainnetRuntimeAtContext(context.Background(), fixture.native, cfg, mainnetRuntimeTestBlock(151)); err == nil || got != nil {
		t.Fatal("unfinalized block gained observation authority")
	}
	client := fixture.native.API.Client.(*validatorRuntimeIdentityTestClient)
	canonicalReads := 0
	client.callContext = func(ctx context.Context, result any, method string, args ...any) error {
		if method == "chain_getBlockHash" && args[0] == uint64(150) {
			canonicalReads++
			if canonicalReads == 3 {
				return setReleaseHistoricalTestResult(result, (types.Hash{5}).Hex())
			}
		}
		return fixture.callContext(ctx, result, method, args...)
	}
	if got, err := ObserveMainnetRuntimeAtContext(context.Background(), fixture.native, cfg, types.Hash{}); err == nil || got != nil || canonicalReads != 3 {
		t.Fatalf("closing canonical change accepted: got=%+v err=%v checks=%d", got, err, canonicalReads)
	}
}

// An exact approved rollback is read under its own artifact and interval;
// specVersion ordering grants no additional history and is not an admission gate.
func TestMainnetRuntimeObservationAcceptsExplicitApprovedRollback(t *testing.T) {
	fixture := newMainnetRuntimeTestFixture(t)
	fixture.approvals[1].RuntimeVersion.SpecVersion = 8_999
	fixture.versions[1] = fixture.approvals[1].RuntimeVersion
	fixture.cfg.RuntimeSpec = 8_999
	fixture.cfg.MainnetRuntimeApprovals[1] = mainnetRuntimeTestWriteApproval(t, fixture.cfg.MainnetRuntimeApprovals[1].Path, fixture.approvals[1])
	cfg, err := LoadMainnetRuntimeObservationConfig(writeReleaseConfig(t, fixture.cfg))
	if err != nil {
		t.Fatal(err)
	}
	got, err := ObserveMainnetRuntimeAtContext(context.Background(), fixture.native, cfg, types.Hash{})
	if err != nil || got == nil || got.Runtime.Version.SpecVersion != 8_999 || got.Revision != 2 {
		t.Fatalf("independently approved rollback rejected: got=%+v err=%v", got, err)
	}
}

// Joining cancellation before result inspection proves the exact-block read
// neither completes observation nor changes the prior native view afterward.
func TestMainnetRuntimeObservationCancellationJoinsWithoutAuthority(t *testing.T) {
	fixture := newMainnetRuntimeTestFixture(t)
	cfg, err := LoadMainnetRuntimeObservationConfig(fixture.path)
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	fixture.native.API.Client.(*validatorRuntimeIdentityTestClient).callContext = func(ctx context.Context, result any, method string, args ...any) error {
		if method == "state_getRuntimeVersion" {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		}
		return fixture.callContext(ctx, result, method, args...)
	}
	oldMetadata, oldRuntime := fixture.native.Meta, fixture.native.Runtime
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		observation, err := ObserveMainnetRuntimeAtContext(ctx, fixture.native, cfg, types.Hash{})
		if observation != nil {
			err = fmt.Errorf("canceled read returned authority: %+v", observation)
		}
		done <- err
	}()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("runtime cancellation lost caller cause: %v", err)
	}
	if fixture.native.Meta != oldMetadata || fixture.native.Runtime != oldRuntime || fixture.callKVs["state_getMetadata"] != 0 {
		t.Fatal("canceled read retained or rebound runtime authority")
	}
}

// A provisional-enabled connection cannot import its observation behavior into
// independently approved mainnet reads, even with a matching genesis field.
func TestMainnetRuntimeObservationRejectsProvisionalConnection(t *testing.T) {
	fixture := newMainnetRuntimeTestFixture(t)
	cfg, err := LoadMainnetRuntimeObservationConfig(fixture.path)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.native.EnableProvisionalRuntimeCompatibility(fixture.genesis, func(crv4.AuthenticatedRuntimeArtifact) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if got, err := ObserveMainnetRuntimeAtContext(context.Background(), fixture.native, cfg, types.Hash{}); err == nil || got != nil || len(fixture.callKVs) != 0 {
		t.Fatalf("provisional connection reached production observation: got=%+v err=%v calls=%v", got, err, fixture.callKVs)
	}
}

// A separately valid config does not authorize an unlisted connection merely
// because that endpoint would report the same network and exact artifact.
func TestMainnetRuntimeObservationRejectsUnapprovedRouteBeforeRpc(t *testing.T) {
	fixture := newMainnetRuntimeTestFixture(t)
	fixture.cfg.Substrate = []string{"wss://other.example"}
	cfg, err := LoadMainnetRuntimeObservationConfig(writeReleaseConfig(t, fixture.cfg))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := ObserveMainnetRuntimeAtContext(context.Background(), fixture.native, cfg, types.Hash{}); err == nil || got != nil || len(fixture.callKVs) != 0 {
		t.Fatalf("unapproved route reached Rpc: got=%+v err=%v calls=%v", got, err, fixture.callKVs)
	}
}
