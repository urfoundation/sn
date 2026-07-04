package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"strings"
	"testing"
)

// TestPackBindHeadGolden pins bindHead(bytes32,bytes32,bytes) packing to exact
// bytes, cross-checked against (a) an independently keccak-derived selector and
// (b) a hand-built ABI encoding, exactly like TestPackClaimMinerGolden.
func TestPackBindHeadGolden(t *testing.T) {
	var hotkey, clientID [32]byte
	for i := range hotkey {
		hotkey[i] = 0x11
		clientID[i] = 0x22
	}
	sig := make([]byte, 64)
	for i := 0; i < 32; i++ {
		sig[i] = 0x33
		sig[32+i] = 0x44
	}

	got, err := stSubnet.TryPackBindHead(hotkey, clientID, sig)
	if err != nil {
		t.Fatalf("TryPackBindHead: %v", err)
	}

	// (a) selector: keccak256 of the canonical signature via x/crypto/sha3.
	wantSel := keccak256x([]byte("bindHead(bytes32,bytes32,bytes)"))[:4]
	if hex.EncodeToString(wantSel) != "55d756e7" {
		t.Fatalf("independent selector = %x, want 55d756e7", wantSel)
	}
	if !bytes.Equal(got[:4], wantSel) {
		t.Fatalf("packed selector = %x, want %x", got[:4], wantSel)
	}

	// (b) hand-built encoding: three head words (hotkey, clientId, the dynamic
	// bytes offset 3*32 = 0x60), then the tail (length 64 = 0x40, then the two
	// 32-byte sig halves). 4 + 32*6 = 196 bytes.
	var hand []byte
	hand = append(hand, wantSel...)
	hand = append(hand, hotkey[:]...)
	hand = append(hand, clientID[:]...)
	hand = append(hand, word(0x60)...) // offset of clientIdSig bytes
	hand = append(hand, word(0x40)...) // clientIdSig length = 64
	hand = append(hand, sig...)        // 64 bytes = exactly 2 words, no padding
	if !bytes.Equal(got, hand) {
		t.Fatalf("packed bindHead != hand-built\n got: 0x%x\nwant: 0x%x", got, hand)
	}
	if len(got) != 4+32*6 {
		t.Fatalf("bindHead calldata = %d bytes, want %d", len(got), 4+32*6)
	}
}

// TestPackUnbindHeadGolden pins unbindHead(bytes32): selector then the hotkey
// word.
func TestPackUnbindHeadGolden(t *testing.T) {
	var hotkey [32]byte
	for i := range hotkey {
		hotkey[i] = 0x11
	}
	got, err := stSubnet.TryPackUnbindHead(hotkey)
	if err != nil {
		t.Fatalf("TryPackUnbindHead: %v", err)
	}
	wantSel := keccak256x([]byte("unbindHead(bytes32)"))[:4]
	if hex.EncodeToString(wantSel) != "fa88f380" {
		t.Fatalf("independent selector = %x, want fa88f380", wantSel)
	}
	hand := append(append([]byte{}, wantSel...), hotkey[:]...)
	if !bytes.Equal(got, hand) {
		t.Fatalf("packed unbindHead != hand-built\n got: 0x%x\nwant: 0x%x", got, hand)
	}
}

// TestParseHotkey covers hex (0x-optional) and ss58 acceptance and the zero
// rejection.
func TestParseHotkey(t *testing.T) {
	fromHex, err := parseHotkey("0x" + aliceHex)
	if err != nil {
		t.Fatalf("parseHotkey(0x hex): %v", err)
	}
	fromBare, err := parseHotkey(aliceHex)
	if err != nil {
		t.Fatalf("parseHotkey(bare hex): %v", err)
	}
	fromSs58, err := parseHotkey(aliceSS58)
	if err != nil {
		t.Fatalf("parseHotkey(ss58): %v", err)
	}
	if fromHex != fromBare || fromHex != fromSs58 {
		t.Fatalf("hotkey forms disagree: hex %x bare %x ss58 %x", fromHex, fromBare, fromSs58)
	}
	if hex.EncodeToString(fromHex[:]) != aliceHex {
		t.Fatalf("parseHotkey = %x, want %s", fromHex, aliceHex)
	}
	if _, err := parseHotkey("0x" + strings.Repeat("00", 32)); err == nil {
		t.Fatalf("parseHotkey accepted the zero hotkey")
	}
	if _, err := parseHotkey("0x1234"); err == nil {
		t.Fatalf("parseHotkey accepted short hex")
	}
}

// TestParseHex32AndSig covers the client_id / sig parsers and their length and
// zero checks.
func TestParseHex32AndSig(t *testing.T) {
	if _, err := parseHex32("--client_id", "0x"+aliceHex); err != nil {
		t.Fatalf("parseHex32(alice): %v", err)
	}
	if _, err := parseHex32("--client_id", "0x"+strings.Repeat("00", 32)); err == nil {
		t.Fatalf("parseHex32 accepted the zero key")
	}
	if _, err := parseHex32("--client_id", "0x1234"); err == nil {
		t.Fatalf("parseHex32 accepted short hex")
	}

	good := "0x" + strings.Repeat("ab", 64)
	sig, err := parseSig(good)
	if err != nil || len(sig) != 64 {
		t.Fatalf("parseSig(64 bytes) = %d bytes, err %v", len(sig), err)
	}
	if _, err := parseSig("0x" + strings.Repeat("ab", 32)); err == nil {
		t.Fatalf("parseSig accepted a 32-byte signature")
	}
}

// TestBindHeadSigRsMapping documents the (r,s) split the snclaim preflight and
// the contract share: ed25519.Sign returns R‖S; the contract reads r=sig[0:32],
// s=sig[32:64] and the 0x402 precompile verifies (message, pubkey, r, s). So a
// stdlib ed25519.Verify over the 32-byte digest is the same decision the chain
// makes — the snclaim bind-head preflight relies on exactly this.
func TestBindHeadSigRsMapping(t *testing.T) {
	var seed [32]byte
	for i := range seed {
		seed[i] = byte(i + 7)
	}
	priv := ed25519.NewKeyFromSeed(seed[:])
	pub := priv.Public().(ed25519.PublicKey)

	var digest [32]byte
	for i := range digest {
		digest[i] = byte(0xA0 + i)
	}
	sig := ed25519.Sign(priv, digest[:])
	if len(sig) != 64 {
		t.Fatalf("sig length %d, want 64", len(sig))
	}
	if !ed25519.Verify(pub, digest[:], sig) {
		t.Fatalf("stdlib verify failed on a fresh signature")
	}
	// Recombine from the contract's r/s halves — must still verify.
	r, s := sig[0:32], sig[32:64]
	if !ed25519.Verify(pub, digest[:], append(append([]byte{}, r...), s...)) {
		t.Fatalf("r‖s recombination does not verify")
	}
	// A different digest must not verify (binds the signature to the digest).
	digest[0] ^= 0x01
	if ed25519.Verify(pub, digest[:], sig) {
		t.Fatalf("signature verified for a tampered digest")
	}
}
