// The reviewed Frontier public projection recovers only the exact committed
// RLP15 preimage. Rendered hash echoes and JSON timestamps are never proof alone.
package payoutartifact

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/urfoundation/sn/v2026/protocol"
)

const FrontierWindowClockProfile = "frontier-legacy-rlp15-milliseconds"

// This is the same finite renderer profile as the production finalized mapping:
// runtime base fee is external, omitted mix hash is zero, and only the 1,000
// possible millisecond remainders are tried. Canonical/finality authority remains
// the independently admitted expected boundary, not the rendered response.
func RecoverFrontierWindowHeader(ctx context.Context, rendered []byte, expected Boundary) ([]byte, error) {
	if ctx == nil {
		return nil, errors.New("Frontier window header requires an owner")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if expected.Number == 0 || !IsDigest(expected.Hash, "0x") {
		return nil, ErrClosedWorkIntegrity
	}
	if len(rendered) == 0 {
		return nil, ErrClosedWorkUnavailable
	}
	if len(rendered) > 1024*1024 {
		return nil, ErrClosedWorkCapacity
	}
	if err := protocol.ValidateUniqueJsonKeys(rendered); err != nil {
		return nil, errors.Join(ErrClosedWorkIntegrity, err)
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(rendered, &fields) != nil || fields == nil {
		return nil, ErrClosedWorkIntegrity
	}
	for _, field := range []string{"hash", "parentHash", "sha3Uncles", "miner", "stateRoot", "transactionsRoot", "receiptsRoot", "logsBloom", "difficulty", "number", "gasLimit", "gasUsed", "timestamp", "extraData", "nonce"} {
		if value, ok := fields[field]; !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, ErrClosedWorkUnavailable
		}
	}
	for field, value := range fields {
		switch field {
		case "hash", "parentHash", "sha3Uncles", "miner", "stateRoot", "transactionsRoot", "receiptsRoot", "logsBloom", "difficulty", "number", "gasLimit", "gasUsed", "timestamp", "extraData", "nonce":
		case "author", "mixHash":
			if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				return nil, ErrClosedWorkUnavailable
			}
		case "baseFeePerGas", "size", "totalDifficulty", "transactions", "uncles":
		case "withdrawalsRoot", "blobGasUsed", "excessBlobGas", "parentBeaconBlockRoot", "requestsHash":
			if !bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				return nil, ErrClosedWorkUnavailable
			}
		default:
			return nil, ErrClosedWorkUnavailable
		}
	}
	var echoed common.Hash
	if json.Unmarshal(fields["hash"], &echoed) != nil || echoed.Hex() != expected.Hash {
		return nil, ErrClosedWorkIntegrity
	}
	var header types.Header
	if json.Unmarshal(rendered, &header) != nil || header.Number == nil || !header.Number.IsUint64() || header.Number.Uint64() != expected.Number || header.Time > ^uint64(0)/1000 || len(header.Extra) > 64*1024 {
		return nil, ErrClosedWorkIntegrity
	}
	if encoded, ok := fields["author"]; ok {
		var author common.Address
		if json.Unmarshal(encoded, &author) != nil || author != header.Coinbase {
			return nil, ErrClosedWorkIntegrity
		}
	}
	header.BaseFee = nil
	first := header.Time * 1000
	for remainder := uint64(0); remainder < 1000 && remainder <= ^uint64(0)-first; remainder++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		header.Time = first + remainder
		raw, err := rlp.EncodeToBytes(&header)
		if err != nil || len(raw) > 64*1024 {
			return nil, ErrClosedWorkCapacity
		}
		content, rest, err := rlp.SplitList(raw)
		count, countErr := rlp.CountValues(content)
		if err != nil || countErr != nil || len(rest) != 0 || count != 15 {
			return nil, ErrClosedWorkIntegrity
		}
		if crypto.Keccak256Hash(raw).Hex() == expected.Hash {
			return raw, nil
		}
	}
	return nil, ErrClosedWorkIntegrity
}
