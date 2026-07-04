package validator

// identity.go — the validator's identity bundle (PLAN.md §7.2):
//
//   - vpk        the validator's long-lived client Ed25519 path key. The seed
//                persists in <state_dir>/.validator.key exactly like the
//                provider's ~/.urnetwork/.provider.key (raw 32-byte seed,
//                0600). Loaded into every connect.Client this process makes
//                via ClientSettings.ClientKeySeed, so the platform-registered
//                client key (ckey_<clientId>) is always this vpk — the value
//                the /verify server checks SEED bodies against. (On-chain vpk
//                binding is deferred to the bounty phase — WHITEPAPER §9.3,
//                D23; implementation parked at docs/parked/.)
//   - jwt        the network JWT written by `validator auth` (same file and
//                shape as `provider auth`: ~/.urnetwork/jwt).
//   - evm key    secp256k1 key hex in --evm_key_file (stctl key_file format).
//                Its address is the contract caller; mirror(address) is the
//                validator's substrate coldkey (D-10).
//   - hotkey     sr25519 keypair via crv4.LoadOrCreateSeedFile — signs the
//                per-tempo CRv4 weight commits.

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/urnetwork/sn/crv4"
	"github.com/urnetwork/sn/ss58"
)

const vpkSeedFileName = ".validator.key"
const defaultHotkeySeedFileName = "hotkey.seed"

// Identity is the loaded key bundle. Optional members are nil when their
// source was not configured; command entry points check what they need.
type Identity struct {
	StateDir string

	// vpk — always present (created on first use).
	VpkSeed []byte
	Vsk     ed25519.PrivateKey
	Vpk     ed25519.PublicKey

	// EVM contract key — nil unless an evm key file exists.
	EvmKey     *ecdsa.PrivateKey
	EvmAddress common.Address

	// sr25519 hotkey — nil unless a hotkey seed was loaded/created.
	Hotkey *crv4.Keypair
}

// defaultStateDir is ~/.urnetwork/validator (a peer of the provider state
// files under ~/.urnetwork).
func defaultStateDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".urnetwork", "validator"), nil
}

// networkJwtPath is the shared ~/.urnetwork/jwt written by `validator auth`
// (and `provider auth` — the credential is the same network JWT).
func networkJwtPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".urnetwork", "jwt"), nil
}

// readNetworkJwt loads the network JWT, mirroring provider/sn.go.
func readNetworkJwt() (string, error) {
	jwtPath, err := networkJwtPath()
	if err != nil {
		return "", err
	}
	byJwtBytes, err := os.ReadFile(jwtPath)
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("jwt does not exist at %s. Run `validator auth` first", jwtPath)
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(byJwtBytes)), nil
}

// loadOrCreateVpkSeed returns the raw 32-byte Ed25519 seed from
// <stateDir>/.validator.key, generating and persisting a fresh one (0600)
// when absent — the provider `.provider.key` convention.
func loadOrCreateVpkSeed(stateDir string) ([]byte, bool, error) {
	p := filepath.Join(stateDir, vpkSeedFileName)
	seed, err := os.ReadFile(p)
	if err == nil {
		if len(seed) != ed25519.SeedSize {
			return nil, false, fmt.Errorf("%s: expected a raw %d-byte seed, found %d bytes", p, ed25519.SeedSize, len(seed))
		}
		return seed, false, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, false, err
	}
	seed = make([]byte, ed25519.SeedSize)
	if _, err := rand.Read(seed); err != nil {
		return nil, false, err
	}
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		return nil, false, err
	}
	if err := os.WriteFile(p, seed, 0600); err != nil {
		return nil, false, err
	}
	return seed, true, nil
}

// loadEvmKey reads a hex-encoded 32-byte secp256k1 private key (stctl
// key_file format: optional 0x prefix, surrounding whitespace ignored).
func loadEvmKey(path string) (*ecdsa.PrivateKey, common.Address, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, common.Address{}, fmt.Errorf("evm key file: %w", err)
	}
	hexKey := strings.TrimSpace(string(raw))
	hexKey = strings.TrimPrefix(strings.TrimPrefix(hexKey, "0x"), "0X")
	key, err := crypto.HexToECDSA(hexKey)
	if err != nil {
		return nil, common.Address{}, fmt.Errorf("evm key file %s: expected a hex-encoded 32-byte key: %w", path, err)
	}
	return key, crypto.PubkeyToAddress(key.PublicKey), nil
}

// IdentityOptions selects which identity members to load. StateDir empty
// means the default. EvmKeyFile/HotkeySeedFile empty mean their defaults
// under the state dir; RequireEvmKey/LoadHotkey control whether a missing
// source is an error / is created.
type IdentityOptions struct {
	StateDir       string
	EvmKeyFile     string
	HotkeySeedFile string
	RequireEvmKey  bool
	LoadHotkey     bool
}

// LoadIdentity assembles the identity bundle. The vpk is always
// loaded-or-created. The EVM key is loaded when its file exists (error if
// RequireEvmKey and missing). The hotkey is loaded-or-created only when
// LoadHotkey is set (it is only needed for steering).
func LoadIdentity(opts IdentityOptions) (*Identity, error) {
	stateDir := opts.StateDir
	if stateDir == "" {
		var err error
		stateDir, err = defaultStateDir()
		if err != nil {
			return nil, err
		}
	}
	stateDir = expandHome(stateDir)

	seed, created, err := loadOrCreateVpkSeed(stateDir)
	if err != nil {
		return nil, err
	}
	if created {
		fmt.Printf("generated new validator path key (vpk) seed at %s\n", filepath.Join(stateDir, vpkSeedFileName))
	}
	vsk := ed25519.NewKeyFromSeed(seed)
	identity := &Identity{
		StateDir: stateDir,
		VpkSeed:  seed,
		Vsk:      vsk,
		Vpk:      vsk.Public().(ed25519.PublicKey),
	}

	evmKeyFile := opts.EvmKeyFile
	if evmKeyFile == "" {
		evmKeyFile = filepath.Join(stateDir, "evm.key")
	}
	evmKeyFile = expandHome(evmKeyFile)
	if _, err := os.Stat(evmKeyFile); err == nil {
		key, addr, err := loadEvmKey(evmKeyFile)
		if err != nil {
			return nil, err
		}
		identity.EvmKey = key
		identity.EvmAddress = addr
	} else if opts.RequireEvmKey {
		return nil, fmt.Errorf(
			"evm key file %s does not exist. Write a funded secp256k1 private key hex there "+
				"(or pass --evm_key_file). The key is never generated automatically because its "+
				"mirror account is the validator's on-chain coldkey and must be funded deliberately",
			evmKeyFile,
		)
	}

	if opts.LoadHotkey {
		hotkeySeedFile := opts.HotkeySeedFile
		if hotkeySeedFile == "" {
			hotkeySeedFile = filepath.Join(stateDir, defaultHotkeySeedFileName)
		}
		hotkeySeedFile = expandHome(hotkeySeedFile)
		hotkeySeed, created, err := crv4.LoadOrCreateSeedFile(hotkeySeedFile)
		if err != nil {
			return nil, err
		}
		if created {
			fmt.Printf("generated new sr25519 hotkey seed at %s\n", hotkeySeedFile)
		}
		kp, err := crv4.KeypairFromSeed(hotkeySeed)
		if err != nil {
			return nil, err
		}
		identity.Hotkey = kp
	}

	return identity, nil
}

// MirrorSs58 returns the ss58 (prefix 42) encoding of the EVM address's
// substrate mirror pubkey — the validator's on-chain coldkey under D-10.
func (self *Identity) MirrorSs58() (string, error) {
	if self.EvmKey == nil {
		return "", fmt.Errorf("no evm key loaded")
	}
	pubkey := ss58.EvmMirrorPubkey([20]byte(self.EvmAddress))
	return ss58.Encode(pubkey, 42)
}

// Vpk32 returns the vpk as a [32]byte word for contract calls.
func (self *Identity) Vpk32() [32]byte {
	var out [32]byte
	copy(out[:], self.Vpk)
	return out
}

// expandHome resolves a leading ~/ against the user home directory.
func expandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(path, "~"), "/"))
		}
	}
	return path
}
