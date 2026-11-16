//go:build linux || darwin

// Exact reviewed metadata with synthetic accounts reproduces a continuation
// snapshot that predates the current runtime approval.
package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
)

// Uses the actual Http schedule reader; no test callback supplies eligibility.
func newRelayContinuationRuntime461Test(t *testing.T) (*runtimeEvidenceActivationRpcV2TestFixture, *evidenceRelayRuntime, protocol.ValidatorEvidenceActivation) {
	t.Helper()
	fixture := newRuntimeEvidenceActivationRpcV2TestFixture(t)
	encoded, err := os.ReadFile("../miner/testdata/runtime461-metadata.scale.gz.base64")
	if err != nil {
		t.Fatal(err)
	}
	compressed, err := base64.StdEncoding.DecodeString(string(encoded))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(io.LimitReader(reader, 344268))
	if err := errors.Join(err, reader.Close()); err != nil {
		t.Fatal(err)
	}
	identity, found := crv4.ReviewedRuntimeArtifact(crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 461, TransactionVersion: 1, StateVersion: 1})
	metadataHex := hexutil.Encode(raw)
	_, hash, err := crv4.DecodeRuntimeMetadata(metadataHex)
	if err != nil || !found || len(raw) != 344267 || hash != identity.MetadataHash {
		t.Fatalf("reviewed461 metadata differs: %s %v", hash, err)
	}
	fixture.metadataHex, fixture.nativeRuntime = metadataHex, identity
	fixture.base.cfg.Release = testReleaseLockFixture(t)
	fixture.base.cfg.Public.Chain.ExpectedRuntimeSpec = reviewedRuntimeSpecVersion
	chain := fixture.executor.substrate.chain
	if err := chain.EnableProvisionalRuntimeCompatibility(fixture.genesis, func(crv4.AuthenticatedRuntimeArtifact) error {
		return errors.New("historical461 must use its reviewed identity, not a new compatibility observation")
	}); err != nil {
		t.Fatal(err)
	}
	activation := protocol.ValidatorEvidenceActivation{NativeBlock: fixture.nativeNumber, NativeHash: [32]byte(fixture.nativeHash), Hotkey: fixture.hotkeys[1]}
	return fixture, &evidenceRelayRuntime{executor: fixture.executor}, activation
}

func TestRelayContinuationRuntime461SnapshotSurvivesCurrentApproval(t *testing.T) {
	fixture, relay, activation := newRelayContinuationRuntime461Test(t)
	chain := fixture.executor.substrate.chain
	metadata, runtime, owner := chain.Meta, chain.Runtime, relay.executor.runtimeEvidenceNativeIdentityV2()
	for _, mode := range []evidenceRelayNativeReadMode{evidenceRelayNativeOriginalSnapshot, evidenceRelayNativeContinuationSnapshot} {
		if epoch, err := relay.readHorizonNative(t.Context(), activation, mode); err != nil || epoch != 77 {
			t.Fatalf("retained461 mode%d: epoch=%d error=%v", mode, epoch, err)
		}
	}
	for _, mode := range []evidenceRelayNativeReadMode{evidenceRelayNativeCurrentHead, evidenceRelayNativeCurrentSnapshot} {
		if _, err := relay.readHorizonNative(t.Context(), activation, mode); err == nil {
			t.Fatalf("fresh mode%d inherited old runtime authority", mode)
		}
	}
	if chain.Meta != metadata || chain.Runtime != runtime || relay.executor.runtimeEvidenceNativeIdentityV2() != owner {
		t.Fatal("historical continuation changed current signing authority")
	}
	if fixture.callCount("state_call") == 0 {
		t.Fatal("historical snapshot skipped independent eligibility")
	}
}

func TestRelayContinuationRuntime461SnapshotRetainsAllIdentityChecks(t *testing.T) {
	fixture, relay, activation := newRelayContinuationRuntime461Test(t)
	if _, err := relay.readHorizonNative(t.Context(), activation, evidenceRelayNativeContinuationSnapshot); err != nil {
		t.Fatal(err)
	}
	originalIdentity := fixture.nativeRuntime
	for _, change := range []struct {
		name string
		edit func()
	}{
		{name: "code", edit: func() { fixture.nativeRuntime.CodeHash = types.Hash{99}.Hex() }},
		{name: "transaction", edit: func() { fixture.nativeRuntime.Version.TransactionVersion++ }},
		{name: "state", edit: func() { fixture.nativeRuntime.Version.StateVersion++ }},
		{name: "name", edit: func() { fixture.nativeRuntime.Version.SpecName = "synthetic-foreign" }},
		{name: "unreviewed predecessor", edit: func() { fixture.nativeRuntime.Version.SpecVersion = 462 }},
		{name: "permit", edit: func() { fixture.permits[1] = false }},
		{name: "stake", edit: func() { fixture.totalStake[1] = 0 }},
	} {
		fixture.stateLock.Lock()
		fixture.nativeRuntime, fixture.permits[1], fixture.totalStake[1] = originalIdentity, true, 150
		change.edit()
		fixture.stateLock.Unlock()
		if _, err := relay.readHorizonNative(t.Context(), activation, evidenceRelayNativeContinuationSnapshot); err == nil {
			t.Fatalf("warm continuation admitted changed %s", change.name)
		}
	}
	fixture.stateLock.Lock()
	fixture.nativeRuntime, fixture.permits[1], fixture.totalStake[1] = originalIdentity, true, 150
	fixture.stateLock.Unlock()
	for _, changed := range []protocol.ValidatorEvidenceActivation{
		{NativeBlock: activation.NativeBlock + 1, NativeHash: activation.NativeHash, Hotkey: activation.Hotkey},
		{NativeBlock: activation.NativeBlock, NativeHash: [32]byte{99}, Hotkey: activation.Hotkey},
		{NativeBlock: activation.NativeBlock, NativeHash: activation.NativeHash, Hotkey: [32]byte{99}},
	} {
		if _, err := relay.readHorizonNative(t.Context(), changed, evidenceRelayNativeContinuationSnapshot); err == nil {
			t.Fatal("continuation admitted changed canonical block or hotkey")
		}
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := relay.readHorizonNative(cancelled, activation, evidenceRelayNativeContinuationSnapshot); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}
