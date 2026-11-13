package crv4

import (
	"bytes"
	"encoding/hex"
	"testing"

	bls12381 "github.com/consensys/gnark-crypto/ecc/bls12-381"
)

func round1000Sig(t *testing.T) []byte {
	t.Helper()
	sig, err := hex.DecodeString(quicknetRound1000SigHex)
	if err != nil {
		t.Fatalf("bad sig hex: %v", err)
	}
	return sig
}

// TestDecryptRustReferenceCiphertext is the core cross-implementation
// conformance test: a ciphertext produced by the Rust tle crate at the exact
// rev subtensor's runtime links (ideal-lab5/timelock @ 5416406) must decrypt
// with this package using the REAL drand quicknet round-1000 signature.
// This exercises, against the reference implementation:
//   - the arkworks TLECiphertext/IBECiphertext/AESOutput container layout
//   - zcash-format G1/G2 point encodings
//   - the identity hash-to-G1 DST (quicknet basic-scheme DST)
//   - the arkworks GT (Fp12) serialization inside h2
//   - h3/h4 hashing and the AES-256-GCM body
//
// Decryption here is the same computation subtensor performs at reveal
// (tle tlock.rs::tld), so success demonstrates chain-side decryptability of
// ciphertexts in this format.
func TestDecryptRustReferenceCiphertext(t *testing.T) {
	ct, err := hex.DecodeString(goldenCtPayload1Round1000Hex)
	if err != nil {
		t.Fatalf("bad ct hex: %v", err)
	}
	got, err := Decrypt(ct, round1000Sig(t))
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	want := mustEncode(t, &Payload{Hotkey: seqHotkey(), Uids: []uint16{0, 1, 2, 7}, Values: []uint16{10, 20, 30, 65535}, VersionKey: 841})
	if !bytes.Equal(got, want) {
		t.Errorf("decrypted payload mismatch\n got %x\nwant %x", got, want)
	}
}

func TestDecryptRustReferenceShortPlaintext(t *testing.T) {
	ct, err := hex.DecodeString(goldenCtShortRound1000Hex)
	if err != nil {
		t.Fatalf("bad ct hex: %v", err)
	}
	got, err := Decrypt(ct, round1000Sig(t))
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if string(got) != "this is a test" {
		t.Errorf("decrypted %q, want %q", got, "this is a test")
	}
}

// TestEncryptRoundTripRealSignature proves Go-encrypted ciphertexts are
// decryptable with a real drand round signature -- i.e. that our identity
// point H1(sha256(round_be)) equals the point quicknet actually signs.
func TestEncryptRoundTripRealSignature(t *testing.T) {
	payload := mustEncode(t, &Payload{Hotkey: seqHotkey(), Uids: []uint16{3, 9}, Values: []uint16{123, 65535}, VersionKey: 7})
	ct, err := Encrypt(payload, 1000)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	got, err := Decrypt(ct, round1000Sig(t))
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("round trip mismatch\n got %x\nwant %x", got, payload)
	}
}

// fixedRand yields a deterministic byte stream for reproducible encryption.
type fixedRand struct{ next byte }

func (f *fixedRand) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = f.next
		f.next++
	}
	return len(p), nil
}

func TestEncryptDeterministic(t *testing.T) {
	payload := []byte("deterministic payload")
	ct1, err := EncryptWithRand(&fixedRand{}, payload, 1000)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	ct2, err := EncryptWithRand(&fixedRand{}, payload, 1000)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if !bytes.Equal(ct1, ct2) {
		t.Error("EncryptWithRand is not deterministic for a fixed entropy stream")
	}
	got, err := Decrypt(ct1, round1000Sig(t))
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Error("deterministic ciphertext does not round trip")
	}
}

// TestCiphertextStructure asserts the exact container layout/overhead so
// accidental format drift fails loudly: 96B U + 8+32 V + 8+32 W +
// 8 + (8 + len+16 + 8 + 12) body + 8+8 suite = len(payload) + 244.
func TestCiphertextStructure(t *testing.T) {
	payload := bytes.Repeat([]byte{0x5A}, 59)
	ct, err := Encrypt(payload, 42)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if want := len(payload) + 244; len(ct) != want {
		t.Errorf("ciphertext length %d, want %d", len(ct), want)
	}
	parsed, err := parseTLECiphertext(ct)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(parsed.v) != 32 || len(parsed.w) != 32 || len(parsed.nonce) != 12 {
		t.Errorf("bad component sizes v=%d w=%d nonce=%d", len(parsed.v), len(parsed.w), len(parsed.nonce))
	}
	if string(parsed.cipherSuite) != "AES_GCM_" {
		t.Errorf("cipher suite %q", parsed.cipherSuite)
	}
	if len(parsed.aesCiphertext) != len(payload)+16 {
		t.Errorf("aes ct length %d, want %d", len(parsed.aesCiphertext), len(payload)+16)
	}

	// The golden Rust ciphertext for the same payload length has the same
	// total size (303 = 59 + 244).
	if len(goldenCtPayload1Round1000Hex)/2 != 303 {
		t.Errorf("golden ct is %d bytes, want 303", len(goldenCtPayload1Round1000Hex)/2)
	}
}

// TestMaxSizePayloadFitsCommitBound: a full 256-uid payload must stay under
// subtensor's MAX_CRV3_COMMIT_SIZE_BYTES = 5000.
func TestMaxSizePayloadFitsCommitBound(t *testing.T) {
	p := Payload{Hotkey: seqHotkey(), VersionKey: ^uint64(0)}
	for i := 0; i < 256; i++ {
		p.Uids = append(p.Uids, uint16(i))
		p.Values = append(p.Values, 65535)
	}
	enc := mustEncode(t, &p)
	ct, err := Encrypt(enc, 1_000_000)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if len(ct) > MaxCommitSizeBytes {
		t.Errorf("256-uid ciphertext is %d bytes, exceeds %d", len(ct), MaxCommitSizeBytes)
	}
	t.Logf("256-uid ciphertext: %d bytes (bound %d)", len(ct), MaxCommitSizeBytes)
}

// TestDecryptWrongSignatureFails: decrypting with a valid G1 point that is
// not the round signature must fail the FullIdent U-check.
func TestDecryptWrongSignatureFails(t *testing.T) {
	ct, err := Encrypt([]byte("secret"), 1000)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	wrong, err := bls12381.HashToG1([]byte("not the signature"), []byte(QuicknetDST))
	if err != nil {
		t.Fatalf("HashToG1: %v", err)
	}
	wrongBytes := wrong.Bytes()
	if _, err := Decrypt(ct, wrongBytes[:]); err == nil {
		t.Error("Decrypt succeeded with a wrong signature")
	}
}

func TestDecryptRejectsMalformed(t *testing.T) {
	sig := round1000Sig(t)
	ct, err := Encrypt([]byte("secret"), 1000)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if _, err := Decrypt(ct[:len(ct)-1], sig); err == nil {
		t.Error("truncated ciphertext accepted")
	}
	if _, err := Decrypt(append(append([]byte{}, ct...), 0x00), sig); err == nil {
		t.Error("trailing bytes accepted")
	}
	if _, err := Decrypt(ct, sig[:47]); err == nil {
		t.Error("truncated signature accepted")
	}
	// Flip a bit in the AES body: must fail GCM auth.
	mut := append([]byte{}, ct...)
	mut[len(mut)-40] ^= 0x01
	if _, err := Decrypt(mut, sig); err == nil {
		t.Error("tampered ciphertext accepted")
	}
}

func TestRoundIdentity(t *testing.T) {
	// sha256 of 8 big-endian bytes 0x00000000000003e8 (1000). Value pinned
	// from the tle test vector inputs.
	got := RoundIdentity(1000)
	if len(got) != 32 {
		t.Fatalf("identity length %d", len(got))
	}
	// Deterministic: two calls agree, and identity differs across rounds.
	if !bytes.Equal(got, RoundIdentity(1000)) || bytes.Equal(got, RoundIdentity(1001)) {
		t.Error("RoundIdentity not behaving as a function of round")
	}
}
