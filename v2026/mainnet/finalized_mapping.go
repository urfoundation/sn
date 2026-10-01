// A finalized mapping retains the native header commitment and exact EVM RLP.
// Finality and canonicality remain assertions of the owned RPC; no signer or
// source-to-Wasm approval is created by this read-only observation.
package main

import (
	"bytes"
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

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/urfoundation/sn/v2026/crv4"
)

const finalizedMappingSchema = "urnetwork-mainnet-finalized-mapping-v1"
const frontierMappingSourceCommit = "67dcf7f791dc495064c293f080a0702cb433e51e"
const maximumMappingHeaderBytes = 64 * 1024
const maximumMappingTransactionHashes = 2048

var errFinalizedMappingUnavailable = errors.New("finalized native/EVM mapping is unavailable")

// Variant1 carries the complete ordered transaction-hash vector; variant3
// carries only the block hash. The nil vector is intentional for variant3.
type frontierPostLog struct {
	DigestIndex       int      `json:"digest_index"`
	Variant           uint8    `json:"variant"`
	BlockHash         string   `json:"block_hash"`
	TransactionHashes []string `json:"transaction_hashes"`
}

// Exact retained RLP is the hash authority. The decoded number supplies the
// canonical EVM lookup; the native number never selects an EVM candidate.
type mappedEvmHeader struct {
	Hash      string `json:"hash"`
	Number    uint64 `json:"number"`
	HeaderRlp string `json:"header_rlp"`
	Format    string `json:"format"`
}

// SourceCommit identifies the reviewed codec semantics, never the deployed
// Wasm. CanonicalHash is the separately corroborating owned-EVM-RPC reply.
type finalizedMapping struct {
	Schema              string                      `json:"schema"`
	Admission           string                      `json:"admission"`
	SourceCommit        string                      `json:"codec_source_commit"`
	RuntimeSourceProven bool                        `json:"runtime_source_proven"`
	FinalityAuthority   string                      `json:"finality_authority"`
	Identity            chainIdentity               `json:"identity"`
	RuntimeVersion      crv4.RuntimeVersionIdentity `json:"runtime_version"`
	NativeHeader        rootReceiptHeader           `json:"native_header"`
	PostLog             frontierPostLog             `json:"frontier_post_log"`
	EvmHeader           mappedEvmHeader             `json:"evm_header"`
	EvmCanonicalHash    string                      `json:"evm_canonical_hash"`
}

// Binds the observation bytes without upgrading owned-RPC assertions to
// consensus proofs, release authority, or transaction approval.
type finalizedMappingEnvelope struct {
	Mapping     finalizedMapping `json:"mapping"`
	ContentHash string           `json:"content_hash"`
}

// Frontier's reviewed source emits Consensus(fron, PostLog) from on_finalize.
// Parse both SCALE lengths completely; neither a hash substring nor matching
// native/EVM numbers establishes this relationship.
func finalizedFrontierPostLog(header rootReceiptHeader) (frontierPostLog, error) {
	var result frontierPostLog
	found := false
	for index, encoded := range header.Digest.Logs {
		raw, err := rootReceiptHex(strings.ToLower(encoded), 65536)
		if err != nil || rootReceiptDigest(raw) != nil {
			return frontierPostLog{}, fmt.Errorf("%w: malformed native digest at %d", errRpcIntegrity, index)
		}
		if len(raw) < 5 || raw[0] == 0 || raw[0] == 8 || !bytes.Equal(raw[1:5], []byte("fron")) {
			continue
		}
		if found {
			return frontierPostLog{}, fmt.Errorf("%w: multiple Frontier digest items", errRpcIntegrity)
		}
		found = true
		if raw[0] != 4 {
			return frontierPostLog{}, fmt.Errorf("%w: Frontier digest kind %d is outside the post-log profile", errFinalizedMappingUnavailable, raw[0])
		}
		reader := rootScaleReader{data: raw, offset: 5}
		length, err := reader.compact()
		if err != nil || length != uint64(len(raw)-reader.offset) || length == 0 {
			return frontierPostLog{}, fmt.Errorf("%w: Frontier payload length differs", errRpcIntegrity)
		}
		tag, err := reader.take(1)
		if err != nil {
			return frontierPostLog{}, fmt.Errorf("%w: Frontier post-log variant is missing", errRpcIntegrity)
		}
		if tag[0] != 1 && tag[0] != 3 {
			return frontierPostLog{}, fmt.Errorf("%w: Frontier post-log variant %d is outside the reviewed hash profile", errFinalizedMappingUnavailable, tag[0])
		}
		blockHash, err := reader.take(32)
		if err != nil {
			return frontierPostLog{}, fmt.Errorf("%w: Frontier block hash is truncated", errRpcIntegrity)
		}
		result = frontierPostLog{DigestIndex: index, Variant: tag[0], BlockHash: "0x" + hex.EncodeToString(blockHash)}
		if !rootCanonicalHash(result.BlockHash) {
			return frontierPostLog{}, fmt.Errorf("%w: Frontier block hash is zero", errRpcIntegrity)
		}
		if result.Variant == 1 {
			count, err := reader.compact()
			if err != nil || count > maximumMappingTransactionHashes || count*32 != uint64(len(raw)-reader.offset) {
				return frontierPostLog{}, fmt.Errorf("%w: Frontier transaction vector is truncated, oversized or has trailing bytes", errRpcIntegrity)
			}
			result.TransactionHashes = make([]string, int(count))
			for transactionIndex := range result.TransactionHashes {
				hash, _ := reader.take(32)
				result.TransactionHashes[transactionIndex] = "0x" + hex.EncodeToString(hash)
			}
		}
		if reader.offset != len(raw) {
			return frontierPostLog{}, fmt.Errorf("%w: Frontier post-log has trailing bytes", errRpcIntegrity)
		}
	}
	if !found {
		return frontierPostLog{}, fmt.Errorf("%w: native header has no Frontier post-log", errFinalizedMappingUnavailable)
	}
	return result, nil
}

// The stored Frontier header has fifteen RLP fields. Hash exact bytes before
// decoding, including bytes recovered from the reviewed public projection.
func authenticateMappedEvmHeader(encoded, expectedHash string) (mappedEvmHeader, error) {
	raw, normalized, err := decodeRuntimeSnapshotHex("raw EVM header", encoded, maximumMappingHeaderBytes)
	if err != nil {
		return mappedEvmHeader{}, err
	}
	if crypto.Keccak256Hash(raw).Hex() != expectedHash {
		return mappedEvmHeader{}, fmt.Errorf("%w: raw EVM header hash differs from the native digest", errRpcIntegrity)
	}
	content, rest, err := rlp.SplitList(raw)
	if err != nil || len(rest) != 0 {
		return mappedEvmHeader{}, fmt.Errorf("%w: raw EVM header is not one RLP list", errRpcIntegrity)
	}
	count, err := rlp.CountValues(content)
	if err != nil {
		return mappedEvmHeader{}, fmt.Errorf("%w: raw EVM header has invalid RLP fields", errRpcIntegrity)
	}
	if count != 15 {
		return mappedEvmHeader{}, fmt.Errorf("%w: raw EVM header field count %d is outside the reviewed Frontier profile", errFinalizedMappingUnavailable, count)
	}
	var header types.Header
	if err := rlp.DecodeBytes(raw, &header); err != nil || header.Number == nil || !header.Number.IsUint64() {
		return mappedEvmHeader{}, fmt.Errorf("%w: raw EVM header cannot be decoded with a bounded block number: %v", errRpcIntegrity, err)
	}
	return mappedEvmHeader{Hash: expectedHash, Number: header.Number.Uint64(), HeaderRlp: normalized, Format: "frontier-legacy-rlp15"}, nil
}

// Only this verifier admits these additional signer-free RPC methods. Null
// is a precise unavailable mapping, not an empty successful header. Existing
// read profiles cannot invoke these methods through their ordinary whitelist.
func (self *rpcClient) callFinalizedMappingRead(ctx context.Context, method string, params []any, result any) error {
	switch method {
	case "debug_getRawHeader", "eth_getBlockByHash", "eth_getBlockByNumber":
	default:
		return errors.New("RPC method is outside the finalized mapping read profile")
	}
	var raw json.RawMessage
	err := self.callAdmittedRead(ctx, method, params, &raw, true, maxRpcReplyBytes)
	if err != nil {
		var rpcErr *rpcCallError
		if errors.As(err, &rpcErr) && (rpcErr.code == -32601 || rpcErr.code == -32602) {
			return fmt.Errorf("%w: required %s capability is unsupported: %w", errFinalizedMappingUnavailable, method, err)
		}
		return err
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return fmt.Errorf("%w: %s has no canonical block for the requested identity", errFinalizedMappingUnavailable, method)
	}
	if err := json.Unmarshal(raw, result); err != nil {
		return fmt.Errorf("%w: %s mapping result is malformed: %v", errRpcIntegrity, method, err)
	}
	return nil
}

// Captures one finalized native hash, follows its exact Frontier commitment,
// and corroborates EVM canonicality using the raw header's own decoded number.
func (self *rpcClient) readFinalizedMapping(ctx context.Context, expected *identityExpectation) (result finalizedMapping, resultErr error) {
	if ctx == nil {
		return finalizedMapping{}, errors.New("mapping context is unavailable")
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			result = finalizedMapping{}
		}
	}()
	sampleCtx, cancel := context.WithTimeout(ctx, self.retryWindow)
	defer cancel()
	identity, err := self.readIdentity(sampleCtx)
	if err != nil {
		return finalizedMapping{}, err
	}
	if expected != nil {
		if err := expected.match(identity); err != nil {
			return finalizedMapping{}, err
		}
	}
	return self.readFinalizedMappingAtIdentity(sampleCtx, identity)
}

// Follows an authenticated in-memory finalized identity on the same route;
// head advancement cannot replace the selected block during composition.
func (self *rpcClient) readFinalizedMappingAtIdentity(ctx context.Context, identity chainIdentity) (result finalizedMapping, resultErr error) {
	if ctx == nil {
		return finalizedMapping{}, errors.New("mapping context is unavailable")
	}
	if err := self.validateSnapshotIdentity(identity); err != nil {
		return finalizedMapping{}, err
	}
	sampleCtx, cancel := context.WithTimeout(ctx, self.retryWindow)
	defer cancel()
	defer func() {
		resultErr = errors.Join(resultErr, sampleCtx.Err())
		if resultErr != nil {
			result = finalizedMapping{}
		}
	}()
	var nativeHeader rootReceiptHeader
	if err := self.call(sampleCtx, "chain_getHeader", []any{identity.FinalizedHash}, &nativeHeader); err != nil {
		return finalizedMapping{}, err
	}
	nativeHeader.normalizeHashes()
	if number, err := nativeHeader.authenticate(identity.FinalizedHash); err != nil || number != identity.FinalizedNumber {
		return finalizedMapping{}, fmt.Errorf("%w: retained native header differs: %v", errRpcIntegrity, err)
	}
	postLog, err := finalizedFrontierPostLog(nativeHeader)
	if err != nil {
		return finalizedMapping{}, err
	}
	evmHeader, err := self.readMappedEvmHeader(sampleCtx, postLog.BlockHash)
	if err != nil {
		return finalizedMapping{}, err
	}
	checkCanonical := func() (string, error) {
		var canonical struct {
			Hash         string    `json:"hash"`
			Number       string    `json:"number"`
			Transactions *[]string `json:"transactions"`
		}
		if err := self.callFinalizedMappingRead(sampleCtx, "eth_getBlockByNumber", []any{fmt.Sprintf("0x%x", evmHeader.Number), false}, &canonical); err != nil {
			return "", err
		}
		canonicalNumber, err := parseHexNumber(canonical.Number)
		if err != nil || canonicalNumber != evmHeader.Number || !validHash(canonical.Hash) || !strings.EqualFold(canonical.Hash, evmHeader.Hash) {
			return "", fmt.Errorf("%w: canonical EVM number/hash differs from the native commitment", errRpcIntegrity)
		}
		if postLog.Variant == 1 {
			if canonical.Transactions == nil || len(*canonical.Transactions) != len(postLog.TransactionHashes) {
				return "", fmt.Errorf("%w: canonical EVM transaction vector differs from the native commitment", errRpcIntegrity)
			}
			for index, hash := range *canonical.Transactions {
				if !strings.EqualFold(hash, postLog.TransactionHashes[index]) {
					return "", fmt.Errorf("%w: canonical EVM transaction hash differs at index %d", errRpcIntegrity, index)
				}
			}
		}
		return strings.ToLower(canonical.Hash), nil
	}
	if _, err := checkCanonical(); err != nil {
		return finalizedMapping{}, err
	}
	for _, lookup := range []struct {
		number uint64
		hash   string
	}{
		{number: 0, hash: identity.GenesisHash},
		{number: identity.FinalizedNumber, hash: identity.FinalizedHash},
	} {
		var hash string
		if err := self.call(sampleCtx, "chain_getBlockHash", []any{lookup.number}, &hash); err != nil {
			return finalizedMapping{}, err
		}
		if !strings.EqualFold(hash, lookup.hash) {
			return finalizedMapping{}, fmt.Errorf("%w: native canonical identity changed during mapping", errRpcIntegrity)
		}
	}
	var nativeChain, evmChainHex string
	if err := self.call(sampleCtx, "system_chain", []any{}, &nativeChain); err != nil {
		return finalizedMapping{}, err
	}
	if err := self.call(sampleCtx, "eth_chainId", []any{}, &evmChainHex); err != nil {
		return finalizedMapping{}, err
	}
	evmChainId, err := parseHexNumber(evmChainHex)
	if err != nil || nativeChain != identity.NativeChain || evmChainId != identity.EvmChainId {
		return finalizedMapping{}, fmt.Errorf("%w: mapping RPC route identity changed", errRpcIntegrity)
	}
	canonicalHash, err := checkCanonical()
	if err != nil {
		return finalizedMapping{}, err
	}
	return finalizedMapping{
		Schema: finalizedMappingSchema, Admission: "unapproved_observation", SourceCommit: frontierMappingSourceCommit,
		FinalityAuthority: "owned-rpc-assertion", Identity: identity, RuntimeVersion: identity.runtimeVersion,
		NativeHeader: nativeHeader, PostLog: postLog, EvmHeader: evmHeader, EvmCanonicalHash: canonicalHash,
	}, sampleCtx.Err()
}

// Seals exact native digest bytes and EVM RLP for independent hash replay.
func sealFinalizedMapping(mapping finalizedMapping) (finalizedMappingEnvelope, error) {
	raw, err := json.Marshal(mapping)
	if err != nil {
		return finalizedMappingEnvelope{}, err
	}
	digest := sha256.Sum256(append([]byte(finalizedMappingSchema+"\x00"), raw...))
	return finalizedMappingEnvelope{Mapping: mapping, ContentHash: "sha256:" + hex.EncodeToString(digest[:])}, nil
}

// Missing codec/header capabilities produce exit4 with no partial proof;
// expected network mismatches produce exit3. Neither success mode approves it.
func runFinalizedMappingCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("finalized-mapping", flag.ContinueOnError)
	flags.SetOutput(stderr)
	rpcUrl := flags.String("rpc", "", "explicit owned HTTP(S) RPC URL")
	expectedChain := flags.String("expected-chain", "", "independently approved native chain name")
	expectedGenesis := flags.String("expected-genesis", "", "independently approved native genesis hash")
	expectedEvmChainId := flags.Uint64("expected-evm-chain-id", 0, "independently approved EVM chain ID")
	retryWindow := flags.Duration("retry-window", 300*time.Second, "total transient retry window for the complete mapping")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || *rpcUrl == "" || *retryWindow < time.Minute || *retryWindow > 15*time.Minute {
		fmt.Fprintln(stderr, "finalized-mapping requires --rpc, no positional arguments, and a 60s to 15m retry window")
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
	mapping, err := client.readFinalizedMapping(ctx, expected)
	if err != nil {
		fmt.Fprintln(stderr, "finalized mapping:", err)
		if errors.Is(err, errRpcIdentityMismatch) {
			return 3
		}
		if errors.Is(err, errFinalizedMappingUnavailable) {
			return 4
		}
		return 1
	}
	envelope, err := sealFinalizedMapping(mapping)
	if err != nil {
		fmt.Fprintln(stderr, "seal finalized mapping:", err)
		return 1
	}
	if err := json.NewEncoder(stdout).Encode(envelope); err != nil {
		fmt.Fprintln(stderr, "write finalized mapping:", err)
		return 1
	}
	return 0
}
