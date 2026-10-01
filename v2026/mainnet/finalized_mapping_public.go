// The public Frontier projection can recover the exact legacy header only by
// matching its native-committed hash. No rendered field or RPC hash echo alone
// proves a header, and the caller must still check canonicality by EVM number.
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/urfoundation/sn/v2026/protocol"
)

// Raw RLP remains primary. Only explicit method/selector incompatibility may
// use the public projection; null, corrupt data, transport failure and canceled
// reads keep their original refusal. Neither path guesses an EVM block height.
func (self *rpcClient) readMappedEvmHeader(ctx context.Context, hash string) (mappedEvmHeader, error) {
	if ctx == nil || !rootCanonicalHash(hash) {
		return mappedEvmHeader{}, errors.New("mapped header requires a context and exact EVM hash")
	}
	var encoded string
	err := self.callFinalizedMappingRead(ctx, "debug_getRawHeader", []any{map[string]any{"blockHash": hash, "requireCanonical": true}}, &encoded)
	if err == nil {
		return authenticateMappedEvmHeader(encoded, hash)
	}
	var rpcErr *rpcCallError
	if !errors.As(err, &rpcErr) || rpcErr.code != -32601 && rpcErr.code != -32602 {
		return mappedEvmHeader{}, err
	}
	var rendered json.RawMessage
	if err := self.callFinalizedMappingRead(ctx, "eth_getBlockByHash", []any{hash, false}, &rendered); err != nil {
		return mappedEvmHeader{}, err
	}
	return authenticateRenderedFrontierHeader(ctx, rendered, hash)
}

// The reviewed renderer copies every legacy field except mix_hash (omitted),
// floors the millisecond timestamp to seconds, and adds a runtime base fee
// outside the stored header. The pallet fixes omitted mix_hash to zero. Try
// only the 1,000 possible timestamp remainders in this one RLP15 profile, with
// no alternate units, discarded serialized fields, or guessed nonzero values.
func authenticateRenderedFrontierHeader(ctx context.Context, rendered []byte, expectedHash string) (mappedEvmHeader, error) {
	if ctx == nil || !rootCanonicalHash(expectedHash) {
		return mappedEvmHeader{}, errors.New("public header requires a context and exact EVM hash")
	}
	if err := ctx.Err(); err != nil {
		return mappedEvmHeader{}, err
	}
	if len(rendered) > maxRpcReplyBytes {
		return mappedEvmHeader{}, fmt.Errorf("%w: public EVM header exceeds reply bound", errRpcIntegrity)
	}
	if err := protocol.ValidateUniqueJsonKeys(rendered); err != nil {
		return mappedEvmHeader{}, fmt.Errorf("%w: public EVM header JSON: %v", errRpcIntegrity, err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(rendered, &fields); err != nil || fields == nil {
		return mappedEvmHeader{}, fmt.Errorf("%w: public EVM header is not an object", errRpcIntegrity)
	}
	for _, field := range []string{"hash", "parentHash", "sha3Uncles", "miner", "stateRoot", "transactionsRoot", "receiptsRoot", "logsBloom", "difficulty", "number", "gasLimit", "gasUsed", "timestamp", "extraData", "nonce"} {
		if value, ok := fields[field]; !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return mappedEvmHeader{}, fmt.Errorf("%w: public EVM header lacks %s", errFinalizedMappingUnavailable, field)
		}
	}
	for field, value := range fields {
		switch field {
		case "hash", "parentHash", "sha3Uncles", "miner", "stateRoot", "transactionsRoot", "receiptsRoot", "logsBloom", "difficulty", "number", "gasLimit", "gasUsed", "timestamp", "extraData", "nonce":
		case "author", "mixHash":
			if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				return mappedEvmHeader{}, fmt.Errorf("%w: public EVM header has null %s", errFinalizedMappingUnavailable, field)
			}
		case "baseFeePerGas", "size", "totalDifficulty", "transactions", "uncles":
			// These are RPC block annotations, outside the stored RLP15 header.
		case "withdrawalsRoot", "blobGasUsed", "excessBlobGas", "parentBeaconBlockRoot", "requestsHash":
			if !bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				return mappedEvmHeader{}, fmt.Errorf("%w: public EVM header field %s is outside the reviewed RLP15 profile", errFinalizedMappingUnavailable, field)
			}
		default:
			return mappedEvmHeader{}, fmt.Errorf("%w: public EVM header field %s is outside the reviewed projection", errFinalizedMappingUnavailable, field)
		}
	}
	var echoedHash common.Hash
	if err := json.Unmarshal(fields["hash"], &echoedHash); err != nil || echoedHash.Hex() != expectedHash {
		return mappedEvmHeader{}, fmt.Errorf("%w: public EVM hash differs from the native commitment", errRpcIntegrity)
	}
	var header types.Header
	if err := json.Unmarshal(rendered, &header); err != nil {
		return mappedEvmHeader{}, fmt.Errorf("%w: public EVM header is malformed: %v", errRpcIntegrity, err)
	}
	if header.Number == nil || !header.Number.IsUint64() || header.Time > ^uint64(0)/1000 || len(header.Extra) > maximumMappingHeaderBytes {
		return mappedEvmHeader{}, fmt.Errorf("%w: public EVM number, timestamp or extra data exceeds the reviewed bounds", errRpcIntegrity)
	}
	if encoded, ok := fields["author"]; ok {
		var author common.Address
		if err := json.Unmarshal(encoded, &author); err != nil || author != header.Coinbase {
			return mappedEvmHeader{}, fmt.Errorf("%w: public EVM author and miner differ", errRpcIntegrity)
		}
	}
	// Base fee is always external to this stored profile, never conditionally
	// removed after a failed hash. All fifteen serialized fields remain present.
	header.BaseFee = nil
	firstMillisecond := header.Time * 1000
	for remainder := uint64(0); remainder < 1000 && remainder <= ^uint64(0)-firstMillisecond; remainder++ {
		if err := ctx.Err(); err != nil {
			return mappedEvmHeader{}, err
		}
		header.Time = firstMillisecond + remainder
		raw, err := rlp.EncodeToBytes(&header)
		if err != nil || len(raw) > maximumMappingHeaderBytes {
			return mappedEvmHeader{}, fmt.Errorf("%w: recovered EVM header exceeds the RLP15 encoding bounds: %v", errRpcIntegrity, err)
		}
		if crypto.Keccak256Hash(raw).Hex() == expectedHash {
			return authenticateMappedEvmHeader("0x"+hex.EncodeToString(raw), expectedHash)
		}
	}
	return mappedEvmHeader{}, fmt.Errorf("%w: public EVM fields cannot reproduce the native commitment within the reviewed RLP15 timestamp interval", errRpcIntegrity)
}
