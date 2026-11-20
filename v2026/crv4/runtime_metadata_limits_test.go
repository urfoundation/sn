package crv4

import (
	"context"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	"github.com/centrifuge/go-substrate-rpc-client/v4/scale"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
)

// Each forged count is 65,536: the old path's allocation is finite in causal
// controls, while the input itself supplies only nine through eleven bytes.
func runtimeMetadataForgedVectors() [][]byte {
	return [][]byte{
		{'m', 'e', 't', 'a', 14, 2, 0, 4, 0},
		{'m', 'e', 't', 'a', 14, 0, 2, 0, 4, 0},
		{'m', 'e', 't', 'a', 14, 4, 0, 2, 0, 4, 0},
	}
}

func TestRuntimeMetadataBoundsUnbackedNestedVectors(t *testing.T) {
	for _, raw := range runtimeMetadataForgedVectors() {
		metadata, digest, err := DecodeRuntimeMetadata("0x" + hex.EncodeToString(raw))
		if !errors.Is(err, scale.ErrDecodeResourceLimit) || metadata != nil || digest != "" {
			t.Fatalf("forged collection %x reached unbounded decoding: metadata=%p digest=%q err=%v", raw, metadata, digest, err)
		}
	}
}

// Input admission precedes raw hex allocation, even if the large text is invalid.
func TestRuntimeMetadataBoundsHexBeforeAllocation(t *testing.T) {
	encoded := "0x" + strings.Repeat("zz", maximumRuntimeMetadataBytes+1)
	metadata, digest, err := DecodeRuntimeMetadata(encoded)
	if !errors.Is(err, ErrRuntimeMetadataInputLimit) || metadata != nil || digest != "" {
		t.Fatalf("oversized hex reached parsing: metadata=%p digest=%q err=%v", metadata, digest, err)
	}
}

func TestRuntimeMetadataTruncatedCompactReturnsError(t *testing.T) {
	for _, suffix := range [][]byte{nil, {1}, {2, 0}, {3, 0, 0}} {
		raw := append([]byte{'m', 'e', 't', 'a', 14}, suffix...)
		metadata, digest, err := DecodeRuntimeMetadata("0x" + hex.EncodeToString(raw))
		if err == nil || metadata != nil || digest != "" {
			t.Fatalf("truncated metadata published a result: %x %p %q %v", raw, metadata, digest, err)
		}
	}
}

// The shared RPC path may see unpinned bytes before its caller checks a hash.
func TestRuntimeMetadataRpcUsesBoundedDecoder(t *testing.T) {
	block := types.Hash{1}
	for _, raw := range runtimeMetadataForgedVectors() {
		calls := 0
		client := &runtimeIdentityTestClient{callContext: func(ctx context.Context, result any, method string, args ...any) error {
			calls++
			if method != "state_getMetadata" || len(args) != 1 || args[0] != block.Hex() {
				t.Fatal("metadata read changed exact RPC selection")
			}
			return setRuntimeIdentityTestResult(result, "0x"+hex.EncodeToString(raw))
		}}
		chain := &Chain{API: &gsrpc.SubstrateAPI{Client: client}}
		metadata, digest, err := RuntimeMetadataAtContext(t.Context(), chain, block)
		if !errors.Is(err, scale.ErrDecodeResourceLimit) || metadata != nil || digest != "" || calls != 1 {
			t.Fatalf("RPC metadata bypassed admission: %p %q %v calls=%d", metadata, digest, err, calls)
		}
	}
}

// Resource limits preserve supported legacy versions, exact hashes and the
// existing full-consumption rule. Metadata15 remains a separate SDK protocol.
func TestRuntimeMetadataLimitsPreserveVersionsAndConsumption(t *testing.T) {
	for _, metadata := range []*types.Metadata{types.NewMetadataV4(), types.NewMetadataV7(), types.NewMetadataV8(), types.NewMetadataV9(), types.NewMetadataV10(), types.NewMetadataV11(), types.NewMetadataV12(), types.NewMetadataV13(), types.NewMetadataV14()} {
		metadata.MagicNumber = types.MagicNumber
		encoded, err := codec.EncodeToHex(metadata)
		if err != nil {
			t.Fatal(err)
		}
		decoded, digest, err := DecodeRuntimeMetadata(encoded)
		if err != nil || decoded.Version != metadata.Version || digest == "" {
			t.Fatalf("valid metadata version %d refused: %v", metadata.Version, err)
		}
		roundtrip, err := codec.EncodeToHex(decoded)
		if err != nil || roundtrip != encoded {
			t.Fatalf("metadata version %d bytes changed: %v", metadata.Version, err)
		}
		if value, hash, err := DecodeRuntimeMetadata(encoded + "00"); err == nil || value != nil || hash != "" || !strings.Contains(err.Error(), "trailing") {
			t.Fatalf("version %d trailing bytes admitted: %v", metadata.Version, err)
		}
	}
	if _, _, err := DecodeRuntimeMetadata("0x6d6574610f"); err == nil || !strings.Contains(err.Error(), "unsupported metadata version 15") {
		t.Fatalf("resource hardening broadened supported metadata protocols: %v", err)
	}
}
