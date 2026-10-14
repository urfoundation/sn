package onchain

import (
	"crypto/ecdsa"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"os"
	"strings"
	"sync"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/urfoundation/sn/v2026/ss58"
	"github.com/urfoundation/sn/v2026/stabi"
)

// claimSelector is the release-1.0 immutable-vault claim selector:
// claim(uint256,uint256,bytes32,uint256,bytes32[]) = 0xce479a1b.
var claimSelector = func() [4]byte {
	data := stSettlementVault.PackClaim(big.NewInt(0), big.NewInt(0), [32]byte{}, big.NewInt(0), nil)
	var out [4]byte
	copy(out[:], data[:4])
	return out
}()

// raoPerAlpha: 1 α = 1e9 rao. All contract amounts are rao.
const raoPerAlpha = 1_000_000_000

// stSettlementVault is the only release claim target. It is immutable and is
// returned separately from the upgradeable coordinator by the server API.
var stSettlementVault = stabi.NewSTSettlementVault()
var stCoordinator = stabi.NewSTCoordinator()

// parsedABI lazily parses the embedded settlement-vault ABI (for raw-calldata
// decoding and custom-error rendering).
var parsedABI = sync.OnceValues(func() (*abi.ABI, error) {
	return stabi.STSettlementVaultMetaData.ParseABI()
})

// claimIntent is a decoded STSettlementVault.claim(e, noId, coldkey, shareBps,
// proof) call.
type claimIntent struct {
	E        *big.Int
	NoID     *big.Int
	Coldkey  [32]byte
	ShareBps *big.Int
	Proof    [][32]byte
}

// buildClaimCalldata ABI-packs the intent via the stabi bindings.
func buildClaimCalldata(in *claimIntent) ([]byte, error) {
	return stSettlementVault.TryPackClaim(in.E, in.NoID, in.Coldkey, in.ShareBps, in.Proof)
}

// parseClaimCalldata validates user-supplied raw calldata: even-length hex,
// at least a selector, and the selector must be the settlement vault's claim —
// anything else is rejected (no --force escape hatch). The returned bytes are
// exactly the input bytes; the decoded intent is for display only.
func parseClaimCalldata(s string) ([]byte, *claimIntent, error) {
	h := strings.TrimSpace(s)
	h = strings.TrimPrefix(strings.TrimPrefix(h, "0x"), "0X")
	if len(h)%2 != 0 {
		return nil, nil, fmt.Errorf("calldata: odd-length hex (%d chars)", len(h))
	}
	data, err := hex.DecodeString(h)
	if err != nil {
		return nil, nil, fmt.Errorf("calldata: %w", err)
	}
	if len(data) < 4 {
		return nil, nil, fmt.Errorf("calldata: %d bytes, need at least the 4-byte selector", len(data))
	}
	if [4]byte(data[:4]) != claimSelector {
		return nil, nil, fmt.Errorf("calldata: selector 0x%x is not settlement-vault claim 0x%x — refusing to sign",
			data[:4], claimSelector[:])
	}
	intent, err := decodeClaimCalldata(data)
	if err != nil {
		return nil, nil, fmt.Errorf("calldata: selector ok but arguments do not decode as claim: %w", err)
	}
	return data, intent, nil
}

// decodeClaimCalldata decodes settlement-vault claim calldata into its
// arguments.
func decodeClaimCalldata(data []byte) (*claimIntent, error) {
	pabi, err := parsedABI()
	if err != nil {
		return nil, err
	}
	method, err := pabi.MethodById(data[:4])
	if err != nil {
		return nil, err
	}
	if method.Name != "claim" {
		return nil, fmt.Errorf("method %q, want claim", method.Name)
	}
	vals, err := method.Inputs.Unpack(data[4:])
	if err != nil {
		return nil, err
	}
	if len(vals) != 5 {
		return nil, fmt.Errorf("decoded %d arguments, want 5", len(vals))
	}
	return &claimIntent{
		E:        abi.ConvertType(vals[0], new(big.Int)).(*big.Int),
		NoID:     abi.ConvertType(vals[1], new(big.Int)).(*big.Int),
		Coldkey:  *abi.ConvertType(vals[2], new([32]byte)).(*[32]byte),
		ShareBps: abi.ConvertType(vals[3], new(big.Int)).(*big.Int),
		Proof:    *abi.ConvertType(vals[4], new([][32]byte)).(*[][32]byte),
	}, nil
}

// parseColdkey accepts an ss58 address (Bittensor network prefix 42) or a
// 32-byte hex pubkey (0x-optional). The zero coldkey is rejected (the
// contract requires coldkey != 0).
func parseColdkey(s string) ([32]byte, error) {
	var ck [32]byte
	v := strings.TrimSpace(s)
	if v == "" {
		return ck, errors.New("coldkey: empty")
	}
	hexish := strings.HasPrefix(v, "0x") || strings.HasPrefix(v, "0X")
	if !hexish && len(v) == 64 {
		if _, err := hex.DecodeString(v); err == nil {
			hexish = true // bare hex32; cannot be ss58 (wrong decoded length)
		}
	}
	if hexish {
		h := strings.TrimPrefix(strings.TrimPrefix(v, "0x"), "0X")
		b, err := hex.DecodeString(h)
		if err != nil {
			return ck, fmt.Errorf("coldkey: %w", err)
		}
		if len(b) != 32 {
			return ck, fmt.Errorf("coldkey: %d hex bytes, want 32", len(b))
		}
		copy(ck[:], b)
	} else {
		pk, err := ss58.DecodeWithPrefix(v, ss58.BittensorPrefix)
		if err != nil {
			return ck, fmt.Errorf("coldkey: %w", err)
		}
		ck = pk
	}
	if ck == ([32]byte{}) {
		return ck, errors.New("coldkey: zero coldkey is not claimable")
	}
	return ck, nil
}

// parseProof parses a comma-separated list of 32-byte hex Merkle nodes.
// The empty string is a valid zero-node proof (single-leaf tree).
func parseProof(s string) ([][32]byte, error) {
	v := strings.TrimSpace(s)
	if v == "" {
		return [][32]byte{}, nil
	}
	parts := strings.Split(v, ",")
	proof := make([][32]byte, 0, len(parts))
	for i, p := range parts {
		h := strings.TrimSpace(p)
		h = strings.TrimPrefix(strings.TrimPrefix(h, "0x"), "0X")
		b, err := hex.DecodeString(h)
		if err != nil {
			return nil, fmt.Errorf("proof[%d]: %w", i, err)
		}
		if len(b) != 32 {
			return nil, fmt.Errorf("proof[%d]: %d bytes, want 32", i, len(b))
		}
		var node [32]byte
		copy(node[:], b)
		proof = append(proof, node)
	}
	return proof, nil
}

// parseBig parses a non-negative integer (decimal, or 0x hex).
func parseBig(flag, s string) (*big.Int, error) {
	v, ok := new(big.Int).SetString(strings.TrimSpace(s), 0)
	if !ok || v.Sign() < 0 {
		return nil, fmt.Errorf("%s: %q is not a non-negative integer", flag, s)
	}
	return v, nil
}

// parseContract parses --contract as a 20-byte hex address.
func parseContract(s string) (common.Address, error) {
	if !common.IsHexAddress(s) {
		return common.Address{}, fmt.Errorf("--contract: %q is not a 20-byte hex address", s)
	}
	return common.HexToAddress(s), nil
}

// leafClaimKey computes the immutable vault's per-epoch claim key:
// keccak256(abi.encode(noId, coldkey)).
func leafClaimKey(noID *big.Int, coldkey [32]byte) [32]byte {
	return [32]byte(crypto.Keccak256Hash(common.BigToHash(noID).Bytes(), coldkey[:]))
}

// loadKeyFile reads a hex-encoded 32-byte secp256k1 EVM private key
// (0x-optional, surrounding whitespace ignored).
func loadKeyFile(path string) (*ecdsa.PrivateKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("key_file: %w", err)
	}
	h := strings.TrimSpace(string(raw))
	h = strings.TrimPrefix(strings.TrimPrefix(h, "0x"), "0X")
	if len(h) != 64 {
		return nil, fmt.Errorf("key_file %s: %d hex chars, want 64 (32-byte secp256k1 key)", path, len(h))
	}
	key, err := crypto.HexToECDSA(h)
	if err != nil {
		return nil, fmt.Errorf("key_file %s: %w", path, err)
	}
	return key, nil
}

// formatAlpha renders a rao amount in α units (1 α = 1e9 rao).
func formatAlpha(rao *big.Int) string {
	q, r := new(big.Int).QuoRem(rao, big.NewInt(raoPerAlpha), new(big.Int))
	return fmt.Sprintf("%d.%09d α (%d rao)", q, r, rao)
}

// renderColdkey shows a coldkey as ss58 (prefix 42) plus raw hex.
func renderColdkey(ck [32]byte) string {
	addr, err := ss58.Encode(ck, ss58.BittensorPrefix)
	if err != nil {
		return fmt.Sprintf("0x%x", ck)
	}
	return fmt.Sprintf("%s (0x%x)", addr, ck)
}
