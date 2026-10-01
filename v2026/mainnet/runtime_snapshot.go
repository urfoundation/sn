// A runtime snapshot retains raw finalized artifacts before launch authority is selected.
// It is read-only evidence; neither a node reply nor its content hash is approval.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/urfoundation/sn/v2026/crv4"
	"golang.org/x/crypto/blake2b"
)

const runtimeSnapshotSchema = "urnetwork-mainnet-runtime-snapshot-v1"

// Raw artifact admission and JSON wire admission are separate bounds. Hex
// doubles the raw bytes; the prefix, quotes and envelope still need room.
const maximumRuntimeSnapshotCodeBytes = 8 * 1024 * 1024
const maximumRuntimeSnapshotMetadataBytes = 4 * 1024 * 1024
const maximumRuntimeCodeRpcReplyBytes = 2*maximumRuntimeSnapshotCodeBytes + maxRpcReplyBytes
const maximumRuntimeMetadataSnapshotRpcReplyBytes = 2*maximumRuntimeSnapshotMetadataBytes + maxRpcReplyBytes
const runtimeCodeStorageKey = "0x3a636f6465"

// The exact code and metadata bytes let an independent reviewer reproduce both
// hashes and inspect the source-to-Wasm mapping without trusting our parser.
type runtimeSnapshot struct {
	Schema       string                      `json:"schema"`
	Admission    string                      `json:"admission"`
	Identity     chainIdentity               `json:"identity"`
	Version      crv4.RuntimeVersionIdentity `json:"runtime_version"`
	CodeHash     string                      `json:"runtime_code_hash"`
	MetadataHash string                      `json:"runtime_metadata_hash"`
	CodeHex      string                      `json:"runtime_code_hex"`
	MetadataHex  string                      `json:"runtime_metadata_hex"`
}

// Binds the entire raw observation but does not authenticate the RPC itself.
type runtimeSnapshotEnvelope struct {
	Snapshot    runtimeSnapshot `json:"snapshot"`
	ContentHash string          `json:"content_hash"`
}

// Canonicalizing raw hex prevents equivalent RPC spellings from producing
// different snapshot identities and rejects missing or oversized artifacts.
func decodeRuntimeSnapshotHex(label, encoded string, maximumBytes int) ([]byte, string, error) {
	if !strings.HasPrefix(encoded, "0x") || len(encoded) <= 2 || len(encoded)%2 != 0 || len(encoded) > 2+2*maximumBytes {
		return nil, "", fmt.Errorf("%w: %s has invalid hex length", errRpcIntegrity, label)
	}
	raw, err := hex.DecodeString(encoded[2:])
	if err != nil {
		return nil, "", fmt.Errorf("%w: %s has invalid hex: %v", errRpcIntegrity, label, err)
	}
	return raw, "0x" + hex.EncodeToString(raw), nil
}

// Samples one finalized native state. Expectations, when supplied, are checked
// before downloading large artifacts; the result never becomes signing authority.
func (self *rpcClient) readRuntimeSnapshot(ctx context.Context, expected *identityExpectation) (runtimeSnapshot, error) {
	sampleCtx, cancel := context.WithTimeout(ctx, self.retryWindow)
	defer cancel()
	identity, err := self.readIdentity(sampleCtx)
	if err != nil {
		return runtimeSnapshot{}, err
	}
	if expected != nil {
		if err := expected.match(identity); err != nil {
			return runtimeSnapshot{}, err
		}
	}
	return self.readRuntimeSnapshotAtIdentity(sampleCtx, identity)
}

// Reuses one in-memory identity already authenticated by this route. This
// helper never samples a new finalized head or imports identity-file authority.
func (self *rpcClient) readRuntimeSnapshotAtIdentity(ctx context.Context, identity chainIdentity) (result runtimeSnapshot, resultErr error) {
	if ctx == nil {
		return runtimeSnapshot{}, errors.New("runtime snapshot context is unavailable")
	}
	if err := self.validateSnapshotIdentity(identity); err != nil {
		return runtimeSnapshot{}, err
	}
	sampleCtx, cancel := context.WithTimeout(ctx, self.retryWindow)
	defer cancel()
	defer func() {
		resultErr = errors.Join(resultErr, sampleCtx.Err())
		if resultErr != nil {
			result = runtimeSnapshot{}
		}
	}()
	blockHash := identity.FinalizedHash
	var rawVersion json.RawMessage
	if err := self.call(sampleCtx, "state_getRuntimeVersion", []any{blockHash}, &rawVersion); err != nil {
		return runtimeSnapshot{}, err
	}
	version, err := crv4.DecodeRuntimeVersionIdentity(rawVersion)
	if err != nil || version != identity.runtimeVersion {
		return runtimeSnapshot{}, fmt.Errorf("%w: finalized runtime version changed between identity and artifact reads: %v", errRpcIntegrity, err)
	}
	var reportedCodeHash, encodedCode, encodedMetadata string
	if err := self.call(sampleCtx, "state_getStorageHash", []any{runtimeCodeStorageKey, blockHash}, &reportedCodeHash); err != nil {
		return runtimeSnapshot{}, err
	}
	if !validHash(reportedCodeHash) {
		return runtimeSnapshot{}, fmt.Errorf("%w: runtime code hash is malformed", errRpcIntegrity)
	}
	if err := self.callBoundedRead(sampleCtx, "state_getStorage", []any{runtimeCodeStorageKey, blockHash}, &encodedCode, false, maximumRuntimeCodeRpcReplyBytes); err != nil {
		return runtimeSnapshot{}, err
	}
	code, codeHex, err := decodeRuntimeSnapshotHex("runtime code", encodedCode, maximumRuntimeSnapshotCodeBytes)
	if err != nil {
		return runtimeSnapshot{}, err
	}
	codeDigest := blake2b.Sum256(code)
	codeHash := "0x" + hex.EncodeToString(codeDigest[:])
	if !strings.EqualFold(codeHash, reportedCodeHash) {
		return runtimeSnapshot{}, fmt.Errorf("%w: runtime code bytes differ from storage hash", errRpcIntegrity)
	}
	if err := self.callBoundedRead(sampleCtx, "state_getMetadata", []any{blockHash}, &encodedMetadata, false, maximumRuntimeMetadataSnapshotRpcReplyBytes); err != nil {
		return runtimeSnapshot{}, err
	}
	metadata, metadataHex, err := decodeRuntimeSnapshotHex("runtime metadata", encodedMetadata, maximumRuntimeSnapshotMetadataBytes)
	if err != nil {
		return runtimeSnapshot{}, err
	}
	metadataDigest := blake2b.Sum256(metadata)
	metadataHash := "0x" + hex.EncodeToString(metadataDigest[:])
	for _, step := range []struct {
		method string
		params []any
		want   string
	}{
		{method: "chain_getBlockHash", params: []any{0}, want: identity.GenesisHash},
		{method: "chain_getBlockHash", params: []any{identity.FinalizedNumber}, want: blockHash},
	} {
		var observedHash string
		if err := self.call(sampleCtx, step.method, step.params, &observedHash); err != nil {
			return runtimeSnapshot{}, err
		}
		if !validHash(observedHash) || !strings.EqualFold(observedHash, step.want) {
			return runtimeSnapshot{}, fmt.Errorf("%w: runtime snapshot route or canonical block changed", errRpcIntegrity)
		}
	}
	var evmChainHex, nativeChain string
	if err := self.call(sampleCtx, "eth_chainId", []any{}, &evmChainHex); err != nil {
		return runtimeSnapshot{}, err
	}
	if err := self.call(sampleCtx, "system_chain", []any{}, &nativeChain); err != nil {
		return runtimeSnapshot{}, err
	}
	evmChainId, err := parseHexNumber(evmChainHex)
	if err != nil || evmChainId != identity.EvmChainId || nativeChain != identity.NativeChain {
		return runtimeSnapshot{}, fmt.Errorf("%w: runtime snapshot network identity changed: %v", errRpcIntegrity, err)
	}
	return runtimeSnapshot{
		Schema: runtimeSnapshotSchema, Admission: "unapproved_observation", Identity: identity,
		Version: version, CodeHash: codeHash, MetadataHash: metadataHash,
		CodeHex: codeHex, MetadataHex: metadataHex,
	}, nil
}

// Seals all observed bytes; a matching digest proves file integrity only.
func sealRuntimeSnapshot(snapshot runtimeSnapshot) (runtimeSnapshotEnvelope, error) {
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return runtimeSnapshotEnvelope{}, err
	}
	digest := sha256.Sum256(append([]byte(runtimeSnapshotSchema+"\x00"), encoded...))
	return runtimeSnapshotEnvelope{Snapshot: snapshot, ContentHash: "sha256:" + hex.EncodeToString(digest[:])}, nil
}

// Emits one self-contained read-only artifact with optional independent
// expectation matching; neither mode approves a runtime or enables mutation.
func runRuntimeSnapshotCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("runtime-snapshot", flag.ContinueOnError)
	flags.SetOutput(stderr)
	rpcUrl := flags.String("rpc", "", "explicit owned HTTP(S) RPC URL")
	expectedChain := flags.String("expected-chain", "", "independently approved native chain name")
	expectedGenesis := flags.String("expected-genesis", "", "independently approved native genesis hash")
	expectedEvmChainId := flags.Uint64("expected-evm-chain-id", 0, "independently approved EVM chain ID")
	retryWindow := flags.Duration("retry-window", 300*time.Second, "total retry window for one snapshot")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || *rpcUrl == "" {
		fmt.Fprintln(stderr, "runtime-snapshot requires --rpc and no positional arguments")
		return 2
	}
	var expected *identityExpectation
	if *expectedChain != "" || *expectedGenesis != "" || *expectedEvmChainId != 0 {
		expected = &identityExpectation{NativeChain: *expectedChain, GenesisHash: *expectedGenesis, EvmChainId: *expectedEvmChainId}
		if expected.NativeChain == "" || !validHash(expected.GenesisHash) || expected.EvmChainId == 0 {
			fmt.Fprintln(stderr, "approved chain, genesis and EVM chain ID must all be supplied")
			return 2
		}
	}
	client, err := newRpcClient(*rpcUrl, *retryWindow)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	snapshot, err := client.readRuntimeSnapshot(ctx, expected)
	if err != nil {
		fmt.Fprintln(stderr, "runtime snapshot:", err)
		if errors.Is(err, errRpcIdentityMismatch) {
			return 3
		}
		return 1
	}
	envelope, err := sealRuntimeSnapshot(snapshot)
	if err != nil {
		fmt.Fprintln(stderr, "seal runtime snapshot:", err)
		return 1
	}
	if err := json.NewEncoder(stdout).Encode(envelope); err != nil {
		fmt.Fprintln(stderr, "write runtime snapshot:", err)
		return 1
	}
	return 0
}
