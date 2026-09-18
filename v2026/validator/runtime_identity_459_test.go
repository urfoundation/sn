//go:build linux || darwin

// Exact459 admission preserves the two original companion runtime domains.
package validator

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"reflect"
	"testing"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/v2026/crv4"
)

// The predecessor tuple is independent of the current product constants.
func releaseHistorical458TestArtifact() crv4.RuntimeArtifactIdentity {
	cfg := runtime458ValidatorTestConfig()
	return crv4.RuntimeArtifactIdentity{
		Version:  crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 458, TransactionVersion: 1, StateVersion: 1},
		CodeHash: cfg.RuntimeCodeHash, MetadataHash: cfg.RuntimeMetadataHash,
	}
}

// Original458 approvals still reach455 receipts, while459 can reach both.
// Neither archive configuration can become current signing authority.
func TestReleaseRuntime459RetainsBothOriginalCompanionDomains(t *testing.T) {
	previous := releaseHistorical458TestArtifact()
	want := []crv4.RuntimeArtifactIdentity{previous, releaseHistoricalTestArtifact()}
	if got := HistoricalReleaseRuntimeArtifacts(previous); !reflect.DeepEqual(got, want) {
		t.Fatalf("original458 replay domain changed: %+v", got)
	}
	cfg := runtime459ValidatorTestConfig()
	current := crv4.RuntimeArtifactIdentity{Version: crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: cfg.RuntimeSpec, TransactionVersion: cfg.TransactionVersion, StateVersion: cfg.StateVersion}, CodeHash: cfg.RuntimeCodeHash, MetadataHash: cfg.RuntimeMetadataHash}
	want = []crv4.RuntimeArtifactIdentity{current, releaseHistoricalTestArtifact(), previous}
	if got := HistoricalReleaseRuntimeArtifacts(current); !reflect.DeepEqual(got, want) {
		t.Fatalf("current459 lost an exact predecessor: %+v", got)
	}
	for _, artifact := range want[1:] {
		cfg := ReleaseConfig{RuntimeSpec: artifact.Version.SpecVersion, TransactionVersion: 1, StateVersion: 1, RuntimeCodeHash: artifact.CodeHash, RuntimeMetadataHash: artifact.MetadataHash}
		if validateReleaseHistoricalNativeRuntimeConfig(&cfg) != nil || validateReleaseNativeRuntimeConfig(&cfg) == nil {
			t.Fatalf("historical%d crossed the current authority boundary", cfg.RuntimeSpec)
		}
		cfg.RuntimeCodeHash = current.CodeHash
		if validateReleaseHistoricalNativeRuntimeConfig(&cfg) == nil {
			t.Fatal("mixed-version code became archived authority")
		}
	}
}

// A finalized458 receipt authenticates its real metadata under a459 owner.
// The same block fails current admission before metadata or binding changes.
func TestReleaseRuntime461AuthenticatesOriginal458BlockWithoutCurrentAuthority(t *testing.T) {
	encoded, err := os.ReadFile("../miner/testdata/runtime458-metadata.scale.gz.base64")
	if err != nil {
		t.Fatal(err)
	}
	compressed, err := base64.StdEncoding.DecodeString(string(encoded))
	if err != nil {
		t.Fatal(err)
	}
	input := bytes.NewReader(compressed)
	reader, err := gzip.NewReader(input)
	if err != nil {
		t.Fatal(err)
	}
	reader.Multistream(false)
	raw, readErr := io.ReadAll(io.LimitReader(reader, 335298))
	if err := errors.Join(readErr, reader.Close()); err != nil || input.Len() != 0 || len(raw) != 335297 {
		t.Fatalf("historical metadata framing differs: %v", err)
	}
	artifact := releaseHistorical458TestArtifact()
	metadataHex := "0x" + hex.EncodeToString(raw)
	_, metadataHash, err := crv4.DecodeRuntimeMetadata(metadataHex)
	if err != nil || metadataHash != artifact.MetadataHash {
		t.Fatalf("historical metadata differs: %v", err)
	}
	block := types.Hash{41}
	var calls []string
	client := &validatorRuntimeIdentityTestClient{callContext: func(_ context.Context, result any, method string, args ...any) error {
		if len(args) == 0 || args[len(args)-1] != block.Hex() {
			return errors.New("historical request lost its exact block")
		}
		calls = append(calls, method)
		switch method {
		case "state_getRuntimeVersion":
			return setReleaseHistoricalTestResult(result, artifact.Version)
		case "state_getStorageHash":
			if len(args) != 2 || args[0] != "0x3a636f6465" {
				return errors.New("historical request changed storage key")
			}
			return setReleaseHistoricalTestResult(result, artifact.CodeHash)
		case "state_getMetadata":
			return setReleaseHistoricalTestResult(result, metadataHex)
		default:
			return errors.New("unexpected historical method")
		}
	}}
	chain := &crv4.Chain{API: &gsrpc.SubstrateAPI{Client: client}, Meta: types.NewMetadataV14(), Runtime: &types.RuntimeVersion{SpecName: "synthetic-retained", SpecVersion: 7}}
	cfg := runtime461ValidatorTestConfig()
	priorMetadata, priorRuntime := chain.Meta, chain.Runtime
	if err := authenticatePinnedNativeRuntimeAtContext(t.Context(), chain, &cfg, block); err == nil || chain.Meta != priorMetadata || chain.Runtime != priorRuntime || !reflect.DeepEqual(calls, []string{"state_getRuntimeVersion"}) {
		t.Fatalf("historical block became current authority: %v calls=%v", err, calls)
	}
	calls = nil
	if err := authenticateHistoricalNativeRuntimeAtContext(t.Context(), chain, &cfg, block); err != nil || chain.Runtime.SpecVersion != 458 || chain.Meta == priorMetadata || !reflect.DeepEqual(calls, []string{"state_getRuntimeVersion", "state_getStorageHash", "state_getMetadata"}) {
		t.Fatalf("original458 receipt lost its exact artifact: %v calls=%v", err, calls)
	}
}
