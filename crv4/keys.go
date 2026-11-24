package crv4

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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
// not exist) durably publishes a fresh random seed without replacing any
// occupied entry. Private regular files accept 64 hex chars with optional 0x
// and whitespace, or exactly 32 raw bytes, within the 4096-byte file bound.
func LoadOrCreateSeedFile(path string) (seed [32]byte, created bool, err error) {
	return loadSeedFileOwned(path, true, false, seedFileHooks{})
}

// LoadSeedFile loads a 32-byte seed from path (hex or raw; see
// LoadOrCreateSeedFile), failing if the file does not exist.
func LoadSeedFile(path string) ([32]byte, error) {
	seed, _, err := loadSeedFileOwned(path, false, false, seedFileHooks{})
	return seed, err
}

// EnsureSeedFile durably publishes an already owned seed when the path is
// absent. An occupied file must contain that exact seed and is never replaced.
func EnsureSeedFile(path string, expected [32]byte) error {
	seed, _, err := loadSeedFileOwned(path, true, false, seedFileHooks{random: bytes.NewReader(expected[:])})
	if err != nil {
		return err
	}
	if seed != expected {
		return errors.New("seed file differs from the provisioned identity")
	}
	return nil
}

// Raw-only seed custody is shared with the validator's Ed25519 identity. The
// same non-replacing publication and strict private-file checks apply.
func LoadOrCreateRawSeedFile(path string) ([32]byte, bool, error) {
	return loadSeedFileOwned(path, true, true, seedFileHooks{})
}

func parseSeedFile(raw []byte) ([32]byte, error) {
	var seed [32]byte
	if len(raw) == 32 {
		copy(seed[:], raw)
		return seed, nil
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) != 0 && trimmed[0] == '{' {
		return parseBittensorKeyfile(trimmed)
	}
	for _, marker := range []string{"$NACL", "$BTWALLET", "$ANSIBLE_VAULT", "gAAAAA"} {
		if bytes.HasPrefix(trimmed, []byte(marker)) {
			return seed, errors.New("seed file is an encrypted Bittensor keyfile; decrypt it first, or use an unencrypted hotkey file, which btcli creates by default")
		}
	}
	return parseSeedHex(string(raw))
}

// A Bittensor wallet keyfile, as btcli and bittensor-wallet write a hotkey:
// JSON holding the sr25519 mini secret in secretSeed. Its public fields, when
// present, must name the key the seed derives, so an edited or mismatched
// file is refused rather than signing as an unexpected hotkey.
type bittensorKeyfile struct {
	SecretSeed  string `json:"secretSeed"`
	PublicKey   string `json:"publicKey"`
	AccountID   string `json:"accountId"`
	SS58Address string `json:"ss58Address"`
}

func parseBittensorKeyfile(raw []byte) ([32]byte, error) {
	var seed [32]byte
	var keyfile bittensorKeyfile
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(&keyfile); err != nil {
		return seed, errors.New("seed file is not a readable Bittensor keyfile")
	}
	if decoder.More() {
		return seed, errors.New("seed file holds more than one Bittensor keyfile")
	}
	if keyfile.SecretSeed == "" {
		return seed, errors.New("Bittensor keyfile has no secretSeed, so it cannot sign; a public keyfile such as coldkeypub.txt holds no secret")
	}
	seed, err := parseSeedHex(keyfile.SecretSeed)
	if err != nil {
		return seed, fmt.Errorf("Bittensor keyfile secretSeed: %w", err)
	}
	keypair, err := KeypairFromSeed(seed)
	if err != nil {
		return [32]byte{}, err
	}
	public := keypair.PublicKey()
	for _, field := range []struct{ name, value string }{{"publicKey", keyfile.PublicKey}, {"accountId", keyfile.AccountID}} {
		if field.value != "" && !strings.EqualFold(strings.TrimPrefix(field.value, "0x"), hex.EncodeToString(public[:])) {
			return [32]byte{}, fmt.Errorf("Bittensor keyfile %s differs from the key its secretSeed derives", field.name)
		}
	}
	if keyfile.SS58Address != "" && keyfile.SS58Address != keypair.Address() {
		return [32]byte{}, errors.New("Bittensor keyfile ss58Address differs from the key its secretSeed derives")
	}
	return seed, nil
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
