package crv4

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// aliceSeedHex is the well-known substrate dev account //Alice secret seed
// (subkey inspect //Alice). Its sr25519 public key and ss58 address (prefix
// 42) are canonical fixtures, also pinned in gsrpc's TestKeyringPairAlice.
const (
	aliceSeedHex   = "e5be9a5092b81bca64be81d212e7f2f9eba183bb7a90954f7b76361f6edb5c0a"
	alicePubkeyHex = "d43593c715fdd31c61141abd04a99fd6822c8558854ccde39a5684e7a56da27d"
	aliceAddress42 = "5GrwvaEF5zXb26Fz9rcQpDWS57CtERHpNehXCPcNoHGKutQY"
)

func TestKeypairAliceVector(t *testing.T) {
	kp, err := KeypairFromSeedHex("0x" + aliceSeedHex)
	if err != nil {
		t.Fatalf("KeypairFromSeedHex: %v", err)
	}
	pub := kp.PublicKey()
	if hex.EncodeToString(pub[:]) != alicePubkeyHex {
		t.Errorf("public key = %x, want %s", pub, alicePubkeyHex)
	}
	if got := kp.SS58(SS58PrefixSubstrate); got != aliceAddress42 {
		t.Errorf("ss58(42) = %s, want %s", got, aliceAddress42)
	}
	if got := kp.Address(); got != aliceAddress42 {
		t.Errorf("Address() = %s, want %s", got, aliceAddress42)
	}
	if kp.Ring.Address != aliceAddress42 {
		t.Errorf("Ring.Address = %s, want %s", kp.Ring.Address, aliceAddress42)
	}
}

func TestKeypairSignVerify(t *testing.T) {
	kp, err := KeypairFromSeedHex(aliceSeedHex)
	if err != nil {
		t.Fatalf("KeypairFromSeedHex: %v", err)
	}
	msg := []byte("crv4 test message")
	sig, err := kp.Sign(msg)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if len(sig) != 64 {
		t.Errorf("signature length %d, want 64", len(sig))
	}
	if !kp.Verify(msg, sig) {
		t.Error("signature does not verify")
	}
	if kp.Verify([]byte("other message"), sig) {
		t.Error("signature verifies for a different message")
	}
}

func TestLoadOrCreateSeedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keys", "hotkey.seed")

	seed1, created, err := LoadOrCreateSeedFile(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !created {
		t.Error("expected created=true on first call")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("seed file permissions %o, want 600", perm)
	}

	seed2, created, err := LoadOrCreateSeedFile(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if created {
		t.Error("expected created=false on second call")
	}
	if seed1 != seed2 {
		t.Error("reloaded seed differs")
	}

	kp1, err := KeypairFromSeed(seed1)
	if err != nil {
		t.Fatal(err)
	}
	kp2, err := KeypairFromSeed(seed2)
	if err != nil {
		t.Fatal(err)
	}
	if kp1.Address() != kp2.Address() {
		t.Error("addresses differ for the same seed")
	}
}

func TestSeedFileFormats(t *testing.T) {
	dir := t.TempDir()

	// Hex with 0x and trailing newline.
	hexPath := filepath.Join(dir, "hex.seed")
	if err := os.WriteFile(hexPath, []byte("0x"+aliceSeedHex+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	seed, err := LoadSeedFile(hexPath)
	if err != nil {
		t.Fatalf("hex seed: %v", err)
	}
	kp, err := KeypairFromSeed(seed)
	if err != nil {
		t.Fatal(err)
	}
	if kp.Address() != aliceAddress42 {
		t.Errorf("hex seed file address = %s, want alice", kp.Address())
	}

	// Raw 32 bytes.
	rawPath := filepath.Join(dir, "raw.seed")
	rawSeed, _ := hex.DecodeString(aliceSeedHex)
	if err := os.WriteFile(rawPath, rawSeed, 0o600); err != nil {
		t.Fatal(err)
	}
	seed, err = LoadSeedFile(rawPath)
	if err != nil {
		t.Fatalf("raw seed: %v", err)
	}
	kp, err = KeypairFromSeed(seed)
	if err != nil {
		t.Fatal(err)
	}
	if kp.Address() != aliceAddress42 {
		t.Errorf("raw seed file address = %s, want alice", kp.Address())
	}

	// Invalid content.
	badPath := filepath.Join(dir, "bad.seed")
	if err := os.WriteFile(badPath, []byte("not a seed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSeedFile(badPath); err == nil {
		t.Error("invalid seed file accepted")
	}

	// Missing file.
	if _, err := LoadSeedFile(filepath.Join(dir, "missing.seed")); err == nil {
		t.Error("missing seed file accepted")
	}
}
