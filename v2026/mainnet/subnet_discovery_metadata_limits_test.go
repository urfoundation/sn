package main

import (
	"bytes"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/scale"
	"github.com/urfoundation/sn/v2026/crv4"
	"golang.org/x/crypto/blake2b"
)

// Caller-computed artifact hashes and envelope seals must not admit an
// allocation claim unsupported by the retained metadata bytes.
func TestSubnetDiscoveryRejectsSelfSealedMetadataVectorsBeforeRpc(t *testing.T) {
	for _, raw := range [][]byte{
		{'m', 'e', 't', 'a', 14, 2, 0, 4, 0},
		{'m', 'e', 't', 'a', 14, 0, 2, 0, 4, 0},
		{'m', 'e', 't', 'a', 14, 4, 0, 2, 0, 4, 0},
	} {
		_, fixture, _, snapshot := newSubnetDiscoveryFixture(t)
		digest := blake2b.Sum256(raw)
		snapshot.MetadataHex = "0x" + hex.EncodeToString(raw)
		snapshot.MetadataHash = "0x" + hex.EncodeToString(digest[:])
		sealed, err := sealRuntimeSnapshot(snapshot)
		if err != nil {
			t.Fatal(err)
		}
		path := subnetDiscoveryTestInput(t, sealed)
		if _, hash, err := readSubnetDiscoveryInput(path); !errors.Is(err, scale.ErrDecodeResourceLimit) || hash != "" {
			t.Fatalf("self-consistent snapshot escaped resource admission: %q %v", hash, err)
		}
		server := rootFixtureServer(t, fixture)
		var stdout, stderr bytes.Buffer
		if code := runMain(t.Context(), []string{"subnet-discover", "--rpc", server.URL, "--snapshot", path}, &stdout, &stderr); code != 2 || stdout.Len() != 0 || fixture.count("system_chain") != 0 || !strings.Contains(stderr.String(), scale.ErrDecodeResourceLimit.Error()) {
			t.Fatalf("self-sealed metadata reached RPC/output: exit=%d %s", code, &stderr)
		}
	}
}

// The combined snapshot's valid native identity and fresh seal do not approve
// its runtime artifact or permit bypassing the same central decoder boundary.
func TestSubnetDiscoveryBoundsSelfSealedCombinedMetadata(t *testing.T) {
	client, fixture := newFinalizedMappingFixture(t, 1, 0)
	fixture.runtimeMetadata = subnetRuntime470TestMetadata(t)
	fixture.fault = func(method string, _ []any, _ int) (any, bool) {
		return crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 470, TransactionVersion: 1, StateVersion: 1}, method == "state_getRuntimeVersion"
	}
	snapshot, err := client.readFinalizedSnapshot(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte{'m', 'e', 't', 'a', 14, 2, 0, 4, 0}
	digest := blake2b.Sum256(raw)
	snapshot.Runtime.MetadataHex = "0x" + hex.EncodeToString(raw)
	snapshot.Runtime.MetadataHash = "0x" + hex.EncodeToString(digest[:])
	sealed, err := sealFinalizedSnapshot(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if _, hash, err := readSubnetDiscoveryInput(subnetDiscoveryTestInput(t, sealed)); !errors.Is(err, scale.ErrDecodeResourceLimit) || hash != "" {
		t.Fatalf("combined envelope bypassed resource admission: %q %v", hash, err)
	}
}

// A wrong independent pin must still fail before examining attacker-controlled
// compact lengths. Resource admission is not a replacement for authority.
func TestNativeMetadataPinRemainsBeforeResourceDecode(t *testing.T) {
	encoded := "0x6d6574610e02000400"
	metadata, hash, err := nativePinnedMetadata(encoded, "0x"+strings.Repeat("71", 32))
	if !errors.Is(err, errNativeMetadataPin) || metadata != nil || hash != "" || errors.Is(err, scale.ErrDecodeResourceLimit) {
		t.Fatalf("independent metadata approval changed: %p %q %v", metadata, hash, err)
	}
}
