// Receipt scans admit complete canonical headers and committed block bodies.
// Missing or truncated RPC results are never evidence of transaction absence.
package crv4

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"golang.org/x/crypto/blake2b"
)

const receiptHeaderBytesLimit = 256 * 1024

// Raw digest logs preserve RuntimeEnvironmentUpdated, which the older SDK
// header decoder does not support. Required fields cannot default to zero.
type receiptHeader struct {
	ParentHash     string `json:"parentHash"`
	Number         string `json:"number"`
	StateRoot      string `json:"stateRoot"`
	ExtrinsicsRoot string `json:"extrinsicsRoot"`
	Digest         struct {
		Logs []string `json:"logs"`
	} `json:"digest"`
}

// Private decoded values are issued only after the complete commitment checks.
type receiptBlock struct {
	header     receiptHeader
	number     uint64
	extrinsics []string
}

// Requires a full 32-byte hex hash; the SDK constructor alone accepts short
// values by padding, which cannot authenticate an announced canonical block.
func receiptHash(value string) (types.Hash, error) {
	if len(value) != 66 || !strings.HasPrefix(value, "0x") {
		return types.Hash{}, errors.New("crv4: receipt hash is not exactly 32 bytes")
	}
	raw, err := hex.DecodeString(value[2:])
	if err != nil {
		return types.Hash{}, fmt.Errorf("crv4: receipt hash: %w", err)
	}
	var hash types.Hash
	copy(hash[:], raw)
	return hash, nil
}

// Hashes every SCALE header field after bounded structural admission. The
// caller supplies the canonical hash independently of the returned JSON.
func (self receiptHeader) authenticate(expected types.Hash) (uint64, error) {
	for _, field := range []struct {
		name  string
		value string
	}{{name: "header.number", value: self.Number}, {name: "header.parentHash", value: self.ParentHash}, {name: "header.stateRoot", value: self.StateRoot}, {name: "header.extrinsicsRoot", value: self.ExtrinsicsRoot}} {
		if field.value == "" {
			return 0, &ReceiptEvidenceUnavailableError{BlockHash: expected, Field: field.name}
		}
	}
	if self.Digest.Logs == nil {
		return 0, &ReceiptEvidenceUnavailableError{BlockHash: expected, Field: "header.digest.logs"}
	}
	if expected == (types.Hash{}) || len(self.Number) < 3 || len(self.Number) > 10 || !strings.HasPrefix(self.Number, "0x") {
		return 0, errors.New("crv4: receipt header hash or native u32 number is missing")
	}
	number, err := strconv.ParseUint(self.Number[2:], 16, 32)
	if err != nil || number > math.MaxUint32 {
		return 0, errors.New("crv4: receipt header native number is invalid")
	}
	parent, parentErr := receiptHash(self.ParentHash)
	state, stateErr := receiptHash(self.StateRoot)
	extrinsics, extrinsicsErr := receiptHash(self.ExtrinsicsRoot)
	if parentErr != nil || stateErr != nil || extrinsicsErr != nil || number != 0 && parent == (types.Hash{}) || state == (types.Hash{}) || extrinsics == (types.Hash{}) || len(self.Digest.Logs) > 256 {
		return 0, errors.New("crv4: receipt header fields are missing or invalid")
	}
	raw := appendCompact(append([]byte(nil), parent[:]...), number)
	raw = append(raw, state[:]...)
	raw = append(raw, extrinsics[:]...)
	raw = appendCompact(raw, uint64(len(self.Digest.Logs)))
	for _, encoded := range self.Digest.Logs {
		if len(encoded) < 4 || len(encoded) > 2+2*65536 || !strings.HasPrefix(encoded, "0x") {
			return 0, errors.New("crv4: receipt header digest is missing or exceeds its bound")
		}
		digest, err := hex.DecodeString(encoded[2:])
		if err != nil || len(raw)+len(digest) > receiptHeaderBytesLimit {
			return 0, errors.New("crv4: receipt header digest is malformed or exceeds its bound")
		}
		if err := validateReceiptDigest(digest); err != nil {
			return 0, err
		}
		raw = append(raw, digest...)
	}
	if types.Hash(blake2b.Sum256(raw)) != expected {
		return 0, errors.New("crv4: receipt header SCALE hash differs from the canonical block")
	}
	return number, nil
}

// Validates the modern SDK digest variants without reinterpreting their opaque
// engine payload. Compact lengths must consume exactly the supplied log.
func validateReceiptDigest(raw []byte) error {
	if len(raw) == 1 && raw[0] == 8 {
		return nil
	}
	if len(raw) == 0 {
		return errors.New("crv4: receipt digest is empty")
	}
	offset := 1
	switch raw[0] {
	case 0:
	case 4, 5, 6:
		offset += 4
	default:
		return errors.New("crv4: receipt digest variant is unsupported")
	}
	if offset > len(raw) {
		return errors.New("crv4: receipt digest engine is truncated")
	}
	length, width, err := decodeValidatorStakeCompact(raw[offset:])
	if err != nil || length != uint64(len(raw)-offset-width) {
		return errors.New("crv4: receipt digest payload is incomplete or noncanonical")
	}
	return nil
}

// A JSON null or omitted field remains structurally invalid before hashing.
func (self *Chain) receiptHeaderAt(ctx context.Context, hash types.Hash) (receiptHeader, uint64, error) {
	var raw json.RawMessage
	if err := self.API.Client.CallContext(ctx, &raw, "chain_getHeader", hash.Hex()); err != nil {
		return receiptHeader{}, 0, err
	}
	if len(raw) == 0 {
		return receiptHeader{}, 0, &ReceiptEvidenceUnavailableError{BlockHash: hash, Field: "header"}
	}
	if len(raw) > 2*receiptHeaderBytesLimit+4096 {
		return receiptHeader{}, 0, errors.New("crv4: receipt header JSON exceeds its bound")
	}
	var header receiptHeader
	if err := json.Unmarshal(raw, &header); err != nil {
		return header, 0, fmt.Errorf("crv4: decode receipt header: %w", err)
	}
	number, err := header.authenticate(hash)
	return header, number, err
}

// A complete extrinsics vector is authenticated against its ordered trie
// commitment, so an explicit empty/truncated vector cannot prove absence.
func (self *Chain) receiptBlockAt(ctx context.Context, hash types.Hash) (*receiptBlock, error) {
	var raw json.RawMessage
	if err := self.API.Client.CallContext(ctx, &raw, "chain_getBlock", hash.Hex()); err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, &ReceiptEvidenceUnavailableError{BlockHash: hash, Field: "block"}
	}
	if len(raw) > 2*receiptBodyBytesLimit+2*receiptHeaderBytesLimit+receiptBodyCountLimit*8+4096 {
		return nil, errors.New("crv4: receipt block JSON exceeds its bound")
	}
	var decoded struct {
		Block *struct {
			Header     receiptHeader `json:"header"`
			Extrinsics []string      `json:"extrinsics"`
		} `json:"block"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("crv4: decode receipt block: %w", err)
	}
	if decoded.Block == nil {
		return nil, &ReceiptEvidenceUnavailableError{BlockHash: hash, Field: "block"}
	}
	if decoded.Block.Extrinsics == nil {
		return nil, &ReceiptEvidenceUnavailableError{BlockHash: hash, Field: "block.extrinsics"}
	}
	if len(decoded.Block.Extrinsics) > receiptBodyCountLimit {
		return nil, errors.New("crv4: receipt block extrinsics vector exceeds its bound")
	}
	number, err := decoded.Block.Header.authenticate(hash)
	if err != nil {
		return nil, err
	}
	extrinsics := make([][]byte, len(decoded.Block.Extrinsics))
	total := 0
	for index, encoded := range decoded.Block.Extrinsics {
		if len(encoded) < 4 || !strings.HasPrefix(encoded, "0x") || len(encoded)%2 != 0 || (len(encoded)-2)/2 > receiptBodyBytesLimit-total {
			return nil, fmt.Errorf("crv4: receipt extrinsic %d is malformed or exceeds its bound", index)
		}
		extrinsics[index], err = hex.DecodeString(encoded[2:])
		if err != nil {
			return nil, fmt.Errorf("crv4: decode receipt extrinsic %d: %w", index, err)
		}
		total += len(extrinsics[index])
	}
	expected, _ := receiptHash(decoded.Block.Header.ExtrinsicsRoot)
	matched := false
	// Layout recognition is a byte-commitment check, not runtime execution
	// authority. Exact event decoding still requires its approved block view.
	for layout := uint8(0); layout <= 1; layout++ {
		root, err := receiptExtrinsicsRoot(extrinsics, layout)
		if err != nil {
			return nil, err
		}
		if root == expected {
			matched = true
			break
		}
	}
	if !matched {
		return nil, errors.New("crv4: receipt block body differs from its ordered trie commitment")
	}
	return &receiptBlock{header: decoded.Block.Header, number: number, extrinsics: decoded.Block.Extrinsics}, nil
}
