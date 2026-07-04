package validator

// Identity bundle tests: vpk seed persistence, EVM key loading (stctl
// format), mirror ss58 derivation.

import (
	"crypto/ed25519"
	"os"
	"path/filepath"
	"testing"

	"github.com/urnetwork/sn/ss58"
)

func TestVpkSeedCreateAndReload(t *testing.T) {
	dir := t.TempDir()
	identity1, err := LoadIdentity(IdentityOptions{StateDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if len(identity1.VpkSeed) != ed25519.SeedSize {
		t.Fatalf("seed size %d", len(identity1.VpkSeed))
	}
	// The seed file is the provider `.provider.key` convention: raw bytes,
	// owner-only.
	seedPath := filepath.Join(dir, vpkSeedFileName)
	info, err := os.Stat(seedPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("seed file mode %v", info.Mode().Perm())
	}

	identity2, err := LoadIdentity(IdentityOptions{StateDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if string(identity1.VpkSeed) != string(identity2.VpkSeed) {
		t.Fatal("seed changed across loads")
	}
	if string(identity1.Vpk) != string(identity2.Vpk) {
		t.Fatal("vpk changed across loads")
	}

	// A corrupted seed file must error, not silently regenerate.
	if err := os.WriteFile(seedPath, []byte("short"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadIdentity(IdentityOptions{StateDir: dir}); err == nil {
		t.Fatal("corrupt seed accepted")
	}
}

func TestEvmKeyLoadingAndMirror(t *testing.T) {
	dir := t.TempDir()
	// The well-known test key (address 0x7E5F4552091A69125d5DfCb7b8C2659029395Bdf).
	keyPath := filepath.Join(dir, "evm.key")
	if err := os.WriteFile(keyPath, []byte("0x0000000000000000000000000000000000000000000000000000000000000001\n"), 0600); err != nil {
		t.Fatal(err)
	}
	identity, err := LoadIdentity(IdentityOptions{StateDir: dir, EvmKeyFile: keyPath, RequireEvmKey: true})
	if err != nil {
		t.Fatal(err)
	}
	if identity.EvmAddress.Hex() != "0x7E5F4552091A69125d5DfCb7b8C2659029395Bdf" {
		t.Fatalf("address %s", identity.EvmAddress)
	}
	// The mirror ss58 must round-trip through the shared ss58 package
	// (blake2("evm:"||addr), prefix 42) — the validator's on-chain coldkey.
	mirror, err := identity.MirrorSs58()
	if err != nil {
		t.Fatal(err)
	}
	wantPubkey := ss58.EvmMirrorPubkey([20]byte(identity.EvmAddress))
	decoded, err := ss58.DecodeWithPrefix(mirror, 42)
	if err != nil {
		t.Fatal(err)
	}
	if decoded != wantPubkey {
		t.Fatal("mirror ss58 does not decode to the mirror pubkey")
	}

	// RequireEvmKey with a missing file errors with guidance (never
	// auto-generates).
	if _, err := LoadIdentity(IdentityOptions{StateDir: t.TempDir(), RequireEvmKey: true}); err == nil {
		t.Fatal("missing evm key accepted")
	}
}

func TestHotkeyLoadOrCreate(t *testing.T) {
	dir := t.TempDir()
	identity1, err := LoadIdentity(IdentityOptions{StateDir: dir, LoadHotkey: true})
	if err != nil {
		t.Fatal(err)
	}
	if identity1.Hotkey == nil {
		t.Fatal("hotkey not created")
	}
	identity2, err := LoadIdentity(IdentityOptions{StateDir: dir, LoadHotkey: true})
	if err != nil {
		t.Fatal(err)
	}
	if identity1.Hotkey.Address() != identity2.Hotkey.Address() {
		t.Fatal("hotkey changed across loads")
	}
	if identity1.Hotkey.Address() == "" {
		t.Fatal("empty hotkey address")
	}
}
