package crv4

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/centrifuge/go-substrate-rpc-client/v4/signature"
	"github.com/vedhavyas/go-subkey/v2"
	"github.com/vedhavyas/go-subkey/v2/sr25519"
)

// SS58PrefixSubstrate is the generic substrate ss58 prefix; bittensor uses it
// for all hotkey/coldkey addresses.
const SS58PrefixSubstrate uint16 = 42

// Keypair is a validator hotkey: an sr25519 keypair derived from a 32-byte
// mini secret seed the same way subkey/polkadot-js do (ExpandEd25519), so
// addresses match `btcli`/bittensor wallets created from the same seed.
type Keypair struct {
	seed [32]byte
	kp   subkey.KeyPair
	// Ring is the gsrpc keyring form used for extrinsic signing.
	Ring signature.KeyringPair
}

// KeypairFromSeed builds the sr25519 keypair from a raw 32-byte mini secret.
func KeypairFromSeed(seed [32]byte) (*Keypair, error) {
	uri := "0x" + hex.EncodeToString(seed[:])
	kp, err := subkey.DeriveKeyPair(sr25519.Scheme{}, uri)
	if err != nil {
		return nil, fmt.Errorf("crv4: derive sr25519 keypair: %w", err)
	}
	ring, err := signature.KeyringPairFromSecret(uri, SS58PrefixSubstrate)
	if err != nil {
		return nil, fmt.Errorf("crv4: keyring: %w", err)
	}
	return &Keypair{seed: seed, kp: kp, Ring: ring}, nil
}

// KeypairFromSeedHex accepts a 64-hex-char seed with optional 0x prefix.
func KeypairFromSeedHex(s string) (*Keypair, error) {
	seed, err := parseSeedHex(s)
	if err != nil {
		return nil, err
	}
	return KeypairFromSeed(seed)
}

// PublicKey returns the 32-byte sr25519 public key (the on-chain AccountId32
// of the hotkey; this is what goes in Payload.Hotkey).
func (k *Keypair) PublicKey() [32]byte {
	var pk [32]byte
	copy(pk[:], k.kp.Public())
	return pk
}

// SS58 returns the ss58 address of the hotkey under the given prefix
// (bittensor: 42).
func (k *Keypair) SS58(prefix uint16) string {
	return subkey.SS58Encode(k.kp.Public(), prefix)
}

// Address returns the ss58 address with the bittensor/substrate prefix 42.
func (k *Keypair) Address() string { return k.SS58(SS58PrefixSubstrate) }

// Sign signs msg with the sr25519 key using the "substrate" signing context
// (what the chain verifies for extrinsics). Payloads longer than 256 bytes
// must be blake2b-256 pre-hashed by the caller (gsrpc's extrinsic signing
// does this internally).
func (k *Keypair) Sign(msg []byte) ([]byte, error) {
	return k.kp.Sign(msg)
}

// Verify verifies an sr25519 signature made by this keypair.
func (k *Keypair) Verify(msg, sig []byte) bool {
	return k.kp.Verify(msg, sig)
}

// LoadOrCreateSeedFile loads a 32-byte seed from path, or (if the file does
// not exist) generates a fresh random seed and writes it as 0x-prefixed hex
// with 0600 permissions. Accepted file contents: 64 hex chars with optional
// 0x prefix and surrounding whitespace, or exactly 32 raw bytes.
func LoadOrCreateSeedFile(path string) (seed [32]byte, created bool, err error) {
	raw, err := os.ReadFile(path)
	switch {
	case err == nil:
		seed, err = parseSeedFile(raw)
		if err != nil {
			return seed, false, fmt.Errorf("crv4: %s: %w", path, err)
		}
		return seed, false, nil
	case os.IsNotExist(err):
		if _, err := rand.Read(seed[:]); err != nil {
			return seed, false, err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return seed, false, err
		}
		content := "0x" + hex.EncodeToString(seed[:]) + "\n"
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			return seed, false, err
		}
		return seed, true, nil
	default:
		return seed, false, err
	}
}

// LoadSeedFile loads a 32-byte seed from path (hex or raw; see
// LoadOrCreateSeedFile), failing if the file does not exist.
func LoadSeedFile(path string) ([32]byte, error) {
	var seed [32]byte
	raw, err := os.ReadFile(path)
	if err != nil {
		return seed, err
	}
	seed, err = parseSeedFile(raw)
	if err != nil {
		return seed, fmt.Errorf("crv4: %s: %w", path, err)
	}
	return seed, nil
}

func parseSeedFile(raw []byte) ([32]byte, error) {
	var seed [32]byte
	if len(raw) == 32 {
		copy(seed[:], raw)
		return seed, nil
	}
	return parseSeedHex(string(raw))
}

func parseSeedHex(s string) ([32]byte, error) {
	var seed [32]byte
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "0x")
	b, err := hex.DecodeString(s)
	if err != nil {
		return seed, errors.New("seed must be 32 raw bytes or 64 hex chars")
	}
	if len(b) != 32 {
		return seed, fmt.Errorf("seed must be 32 bytes, got %d", len(b))
	}
	copy(seed[:], b)
	return seed, nil
}
