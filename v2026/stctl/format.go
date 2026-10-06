package main

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/urfoundation/sn/v2026/ss58"
)

// blockTimeSeconds is the subtensor block time used for window ETAs.
const blockTimeSeconds = 12

// raoPerAlpha is the Bittensor unit scale: 1 alpha = 1e9 rao.
var raoPerAlpha = big.NewInt(1_000_000_000)

// parseUint256 parses a non-negative integer flag: decimal, or hex with a
// 0x prefix. The value must fit in 256 bits.
func parseUint256(name, s string) (*big.Int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("%s: empty value", name)
	}
	base := 10
	digits := s
	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
		base = 16
		digits = s[2:]
	}
	v, ok := new(big.Int).SetString(digits, base)
	if !ok {
		return nil, fmt.Errorf("%s: %q is not a valid integer", name, s)
	}
	if v.Sign() < 0 {
		return nil, fmt.Errorf("%s: %q must be non-negative", name, s)
	}
	if v.BitLen() > 256 {
		return nil, fmt.Errorf("%s: %q does not fit in uint256", name, s)
	}
	return v, nil
}

// parseUint64 parses a non-negative integer flag (decimal, or hex with a
// 0x prefix) that must fit in 64 bits.
func parseUint64(name, s string) (uint64, error) {
	v, err := parseUint256(name, s)
	if err != nil {
		return 0, err
	}
	if v.BitLen() > 64 {
		return 0, fmt.Errorf("%s: %q does not fit in uint64", name, s)
	}
	return v.Uint64(), nil
}

// parseH160 parses a 20-byte 0x-hex EVM address flag.
func parseH160(name, s string) (common.Address, error) {
	s = strings.TrimSpace(s)
	if !common.IsHexAddress(s) {
		return common.Address{}, fmt.Errorf("%s: %q is not a valid 20-byte hex EVM address", name, s)
	}
	return common.HexToAddress(s), nil
}

// parseHex32 parses a 32-byte hex value (0x-optional).
func parseHex32(name, s string) ([32]byte, error) {
	var out [32]byte
	raw, err := parseHexBytes(name, s)
	if err != nil {
		return out, err
	}
	if len(raw) != 32 {
		return out, fmt.Errorf("%s: want 32 bytes, got %d", name, len(raw))
	}
	copy(out[:], raw)
	return out, nil
}

// parseHexBytes parses even-length hex bytes (0x-optional; empty ok).
func parseHexBytes(name, s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "0X")
	if s == "" {
		return []byte{}, nil
	}
	raw, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("%s: invalid hex %q: %w", name, s, err)
	}
	return raw, nil
}

// parseProof parses a comma-separated list of 32-byte hex nodes
// (0x-optional). An empty string is the empty proof (single-leaf tree).
func parseProof(name, s string) ([][32]byte, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return [][32]byte{}, nil
	}
	parts := strings.Split(s, ",")
	proof := make([][32]byte, 0, len(parts))
	for i, part := range parts {
		node, err := parseHex32(fmt.Sprintf("%s[%d]", name, i), strings.TrimSpace(part))
		if err != nil {
			return nil, err
		}
		proof = append(proof, node)
	}
	return proof, nil
}

// parseAccount32 parses a 32-byte account key flag: either raw 32-byte hex
// (0x-optional) or an ss58 address with the Bittensor prefix (42).
func parseAccount32(name, s string) ([32]byte, error) {
	s = strings.TrimSpace(s)
	trimmed := strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "0X")
	if len(trimmed) == 64 {
		if raw, err := hex.DecodeString(trimmed); err == nil {
			var out [32]byte
			copy(out[:], raw)
			return out, nil
		}
	}
	pubkey, err := ss58.DecodeWithPrefix(s, ss58.BittensorPrefix)
	if err != nil {
		return pubkey, fmt.Errorf(
			"%s: %q is neither 32-byte hex nor an ss58 (prefix 42) address: %w",
			name, s, err,
		)
	}
	return pubkey, nil
}

// formatAlpha renders a rao amount with its 9-decimal alpha equivalent,
// e.g. "1234567890 rao (1.234567890 α)".
func formatAlpha(rao *big.Int) string {
	sign := ""
	v := new(big.Int).Set(rao)
	if v.Sign() < 0 {
		sign = "-"
		v.Neg(v)
	}
	whole, frac := new(big.Int).DivMod(v, raoPerAlpha, new(big.Int))
	return fmt.Sprintf("%s rao (%s%s.%09d α)", rao.String(), sign, whole.String(), frac)
}

// formatKey32 renders a 32-byte key as 0x-hex with its ss58 (prefix 42) form.
func formatKey32(key [32]byte) string {
	address, err := ss58.Encode(key, ss58.BittensorPrefix)
	if err != nil {
		// unreachable for prefix 42; keep the hex either way
		return fmt.Sprintf("0x%x", key)
	}
	return fmt.Sprintf("0x%x (%s)", key, address)
}

// mirrorSS58 renders the substrate mirror account of an EVM H160 as its
// ss58 (prefix 42) address.
func mirrorSS58(h160 [20]byte) string {
	address, err := ss58.EvmMirrorAddress(h160, ss58.BittensorPrefix)
	if err != nil {
		// unreachable for prefix 42
		return fmt.Sprintf("0x%x", ss58.EvmMirrorPubkey(h160))
	}
	return address
}

// formatBlockETA renders a target block relative to the current block with
// an ETA at the nominal block time.
func formatBlockETA(current, target uint64) string {
	if target == current {
		return fmt.Sprintf("block %d (now)", target)
	}
	if target < current {
		return fmt.Sprintf("block %d (passed, %d blocks ago)", target, current-target)
	}
	blocks := target - current
	eta := time.Duration(blocks) * blockTimeSeconds * time.Second
	return fmt.Sprintf("block %d (in %d blocks, ~%s @%ds/block)",
		target, blocks, eta, blockTimeSeconds)
}
