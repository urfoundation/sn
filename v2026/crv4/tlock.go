package crv4

import (
	"crypto/aes"
	"crypto/cipher"
	crand "crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/big"

	bls12381 "github.com/consensys/gnark-crypto/ecc/bls12-381"
	"github.com/consensys/gnark-crypto/ecc/bls12-381/fr"
)

// Drand quicknet timelock encryption, wire-compatible with the Rust `tle`
// crate (ideal-lab5/timelock @ 5416406cfd32799e31e1795393d4916894de4468 —
// the exact rev subtensor's runtime links for chain-side decryption, and the
// rev bittensor-drand v2.0.0 uses client-side).
//
// NOTE: this is NOT the age-based github.com/drand/tlock format. Subtensor
// deserializes the commit as an arkworks CanonicalSerialize-compressed
// TLECiphertext<TinyBLS381>:
//
//	struct TLECiphertext { header: IBECiphertext, body: Vec<u8>, cipher_suite: Vec<u8> }
//	struct IBECiphertext { u: G2 (compressed, 96B), v: Vec<u8> (32B), w: Vec<u8> (32B) }
//
// arkworks encodes Vec<u8> as u64 little-endian length + raw bytes, and G2
// points in the zcash/IETF compressed format (which gnark-crypto shares).
// body is the CanonicalSerialize of AESOutput { ciphertext: Vec<u8>, nonce: Vec<u8> }.
//
// The IBE scheme is Boneh-Franklin FullIdent on BLS12-381 with signatures on
// G1 / public keys on G2 (drand quicknet "bls-unchained-g1-rfc9380"):
//
//	identity  Q_id = HashToG1(sha256(round_be8), DST)   [tle curves/drand.rs QUICKNET_CTX]
//	sigma     = sha256(t)[0:32], t random 32 bytes
//	r         = Fr(sha256(sigma || esk))                [h3, big-endian mod order]
//	U         = r * G2generator
//	g_id      = e(Q_id, r * P_pub)                      [GT, final-exponentiated]
//	V         = sigma XOR sha256(ark_gt_bytes(g_id))    [h2]
//	W         = esk XOR sha256(sigma)                   [h4]
//	body      = AES-256-GCM(key=esk, nonce=rand12, aad="") of the payload
const (
	// QuicknetChainHash is the drand quicknet chain hash (verified against
	// bittensor-drand src/constants.rs and subtensor's drand pallet usage).
	QuicknetChainHash = "52db9ba70e0cc0f6eaf7803dd07447a1f5477735fd3f661792ba94600c84e971"

	// QuicknetPublicKeyHex is the quicknet group public key (G2, 96 bytes
	// compressed). Pinned from bittensor-drand src/constants.rs and
	// https://api.drand.sh/<chain-hash>/info.
	QuicknetPublicKeyHex = "83cf0f2896adee7eb8b5f01fcad3912212c437e0073e911fb90022d3e760183c8c4b450b6a0a6c3ac6a5776a2d1064510d1fec758c921cc22b0e17e63aaf4bcb5ed66304de9cf809bd274ca73bab4af5a6e9c76a4bc09e76eae8991ef5ece45a"

	// QuicknetDST is the hash-to-G1 domain separation tag used for identity
	// hashing, pinned from tle curves/drand.rs QUICKNET_CTX (== the drand
	// quicknet BLS "basic" scheme DST, so real drand round signatures act as
	// IBE decryption keys).
	QuicknetDST = "BLS_SIG_BLS12381G1_XMD:SHA-256_SSWU_RO_NUL_"

	// cipherSuite is tle's AESGCMStreamCipherProvider::CIPHER_SUITE.
	cipherSuite = "AES_GCM_"

	// MaxCommitSizeBytes is subtensor's MAX_CRV3_COMMIT_SIZE_BYTES bound on
	// the commit BoundedVec (pallets/subtensor/src/lib.rs).
	MaxCommitSizeBytes = 5000
)

var quicknetPubKey bls12381.G2Affine

func init() {
	raw, err := hex.DecodeString(QuicknetPublicKeyHex)
	if err != nil {
		panic("crv4: bad quicknet public key hex: " + err.Error())
	}
	if _, err := quicknetPubKey.SetBytes(raw); err != nil {
		panic("crv4: bad quicknet public key point: " + err.Error())
	}
}

// RoundIdentity returns the drand quicknet identity message for a round:
// sha256 of the round number as 8 big-endian bytes. This is the message
// quicknet signs each round.
func RoundIdentity(round uint64) []byte {
	var be [8]byte
	binary.BigEndian.PutUint64(be[:], round)
	d := sha256.Sum256(be[:])
	return d[:]
}

// Encrypt timelock-encrypts payload to the given drand quicknet round,
// producing ciphertext bytes exactly as subtensor's
// commit_timelocked_weights expects (TLECiphertext<TinyBLS381>, arkworks
// compressed). Network-free: the quicknet public key is compiled in.
func Encrypt(payload []byte, round uint64) ([]byte, error) {
	return EncryptWithRand(crand.Reader, payload, round)
}

// EncryptWithRand is Encrypt with an explicit entropy source (for
// deterministic tests). Draw order matches bittensor-drand
// generate_commit_v2: esk (32), then IBE t (32), then AES nonce (12).
func EncryptWithRand(rng io.Reader, payload []byte, round uint64) ([]byte, error) {
	var esk [32]byte
	if _, err := io.ReadFull(rng, esk[:]); err != nil {
		return nil, fmt.Errorf("crv4: entropy: %w", err)
	}

	// --- IBE header: encrypt esk to the round identity ---
	var t [32]byte
	if _, err := io.ReadFull(rng, t[:]); err != nil {
		return nil, fmt.Errorf("crv4: entropy: %w", err)
	}
	sigma := sha256.Sum256(t[:]) // h4(t): sha256 truncated to input length (32)

	u, v, w, err := ibeEncrypt(esk, sigma, round)
	if err != nil {
		return nil, err
	}

	// --- body: AES-256-GCM of the payload under esk ---
	var nonce [12]byte
	if _, err := io.ReadFull(rng, nonce[:]); err != nil {
		return nil, fmt.Errorf("crv4: entropy: %w", err)
	}
	block, err := aes.NewCipher(esk[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	sealed := gcm.Seal(nil, nonce[:], payload, nil) // ciphertext || 16B tag

	// AESOutput { ciphertext: Vec<u8>, nonce: Vec<u8> }
	body := make([]byte, 0, 8+len(sealed)+8+len(nonce))
	body = appendArkVec(body, sealed)
	body = appendArkVec(body, nonce[:])

	// TLECiphertext { header: { u, v, w }, body, cipher_suite }
	ubytes := u.Bytes()
	out := make([]byte, 0, len(ubytes)+8+len(v)+8+len(w)+8+len(body)+8+len(cipherSuite))
	out = append(out, ubytes[:]...)
	out = appendArkVec(out, v)
	out = appendArkVec(out, w)
	out = appendArkVec(out, body)
	out = appendArkVec(out, []byte(cipherSuite))
	return out, nil
}

// ibeEncrypt performs BF-IBE FullIdent encryption of the 32-byte esk for the
// round identity, mirroring tle ibe/fullident.rs Identity::encrypt.
func ibeEncrypt(esk, sigma [32]byte, round uint64) (u bls12381.G2Affine, v, w []byte, err error) {
	// r = h3(sigma, esk): sha256 big-endian reduced mod the Fr order.
	h3 := sha256.New()
	h3.Write(sigma[:])
	h3.Write(esk[:])
	var rEl fr.Element
	rEl.SetBytes(h3.Sum(nil))
	rBig := rEl.BigInt(new(big.Int))

	// U = r * P (G2 generator).
	_, _, _, g2Gen := bls12381.Generators()
	u.ScalarMultiplication(&g2Gen, rBig)

	// Q_id = HashToG1(sha256(round_be), DST).
	qid, err := bls12381.HashToG1(RoundIdentity(round), []byte(QuicknetDST))
	if err != nil {
		return u, nil, nil, fmt.Errorf("crv4: hash to G1: %w", err)
	}

	// g_id = e(Q_id, r * P_pub).
	var ppubR bls12381.G2Affine
	ppubR.ScalarMultiplication(&quicknetPubKey, rBig)
	gid, err := bls12381.Pair([]bls12381.G1Affine{qid}, []bls12381.G2Affine{ppubR})
	if err != nil {
		return u, nil, nil, fmt.Errorf("crv4: pairing: %w", err)
	}

	// V = sigma XOR h2(g_id); W = esk XOR h4(sigma).
	h2 := sha256.Sum256(gtArkBytes(&gid))
	v = xor32(sigma[:], h2[:])
	h4 := sha256.Sum256(sigma[:])
	w = xor32(esk[:], h4[:])
	return u, v, w, nil
}

// Decrypt reverses Encrypt given the BLS signature of the target round
// (48-byte compressed G1, e.g. from
// https://api.drand.sh/<chain-hash>/public/<round>). This mirrors the exact
// operation subtensor performs at reveal (tle tlock.rs::tld), so a
// successful round-trip demonstrates chain-side decryptability.
func Decrypt(ciphertext, signature []byte) ([]byte, error) {
	ct, err := parseTLECiphertext(ciphertext)
	if err != nil {
		return nil, err
	}
	if string(ct.cipherSuite) != cipherSuite {
		return nil, fmt.Errorf("crv4: unsupported cipher suite %q", ct.cipherSuite)
	}

	var sig bls12381.G1Affine
	if _, err := sig.SetBytes(signature); err != nil {
		return nil, fmt.Errorf("crv4: bad signature point: %w", err)
	}

	// sigma = V XOR h2(e(sig, U)).
	gid, err := bls12381.Pair([]bls12381.G1Affine{sig}, []bls12381.G2Affine{ct.u})
	if err != nil {
		return nil, fmt.Errorf("crv4: pairing: %w", err)
	}
	h2 := sha256.Sum256(gtArkBytes(&gid))
	sigma := xor32(ct.v, h2[:])

	// esk = W XOR h4(sigma).
	h4 := sha256.Sum256(sigma)
	esk := xor32(ct.w, h4[:])

	// Check U == h3(sigma, esk) * P.
	h3 := sha256.New()
	h3.Write(sigma)
	h3.Write(esk)
	var rEl fr.Element
	rEl.SetBytes(h3.Sum(nil))
	_, _, _, g2Gen := bls12381.Generators()
	var uCheck bls12381.G2Affine
	uCheck.ScalarMultiplication(&g2Gen, rEl.BigInt(new(big.Int)))
	if !uCheck.Equal(&ct.u) {
		return nil, errors.New("crv4: decryption failed (U check)")
	}

	block, err := aes.NewCipher(esk)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(ct.nonce) != gcm.NonceSize() {
		return nil, errors.New("crv4: bad nonce size")
	}
	pt, err := gcm.Open(nil, ct.nonce, ct.aesCiphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("crv4: aes-gcm open: %w", err)
	}
	return pt, nil
}

type tleCiphertext struct {
	u             bls12381.G2Affine
	v             []byte
	w             []byte
	aesCiphertext []byte
	nonce         []byte
	cipherSuite   []byte
}

// maxArkVecLen bounds arkworks Vec lengths while parsing untrusted input.
const maxArkVecLen = 1 << 20

func parseTLECiphertext(raw []byte) (*tleCiphertext, error) {
	rd := arkReader{buf: raw}

	uBytes, err := rd.take(96)
	if err != nil {
		return nil, err
	}
	var ct tleCiphertext
	if _, err := ct.u.SetBytes(uBytes); err != nil {
		return nil, fmt.Errorf("crv4: bad U point: %w", err)
	}
	if ct.v, err = rd.vec(); err != nil {
		return nil, err
	}
	if ct.w, err = rd.vec(); err != nil {
		return nil, err
	}
	if len(ct.v) != 32 || len(ct.w) != 32 {
		return nil, errors.New("crv4: bad V/W length")
	}
	body, err := rd.vec()
	if err != nil {
		return nil, err
	}
	if ct.cipherSuite, err = rd.vec(); err != nil {
		return nil, err
	}
	if rd.remaining() != 0 {
		return nil, errors.New("crv4: trailing bytes in ciphertext")
	}

	brd := arkReader{buf: body}
	if ct.aesCiphertext, err = brd.vec(); err != nil {
		return nil, err
	}
	if ct.nonce, err = brd.vec(); err != nil {
		return nil, err
	}
	if brd.remaining() != 0 {
		return nil, errors.New("crv4: trailing bytes in ciphertext body")
	}
	return &ct, nil
}

type arkReader struct {
	buf []byte
	off int
}

func (r *arkReader) remaining() int { return len(r.buf) - r.off }

func (r *arkReader) take(n int) ([]byte, error) {
	if r.remaining() < n {
		return nil, errors.New("crv4: truncated ciphertext")
	}
	out := r.buf[r.off : r.off+n]
	r.off += n
	return out, nil
}

// vec reads an arkworks-serialized Vec<u8>: u64 little-endian length + bytes.
func (r *arkReader) vec() ([]byte, error) {
	lenBytes, err := r.take(8)
	if err != nil {
		return nil, err
	}
	n := binary.LittleEndian.Uint64(lenBytes)
	if n > maxArkVecLen {
		return nil, errors.New("crv4: ciphertext vector too large")
	}
	return r.take(int(n))
}

// appendArkVec appends the arkworks CanonicalSerialize encoding of a Vec<u8>.
func appendArkVec(out, b []byte) []byte {
	out = binary.LittleEndian.AppendUint64(out, uint64(len(b)))
	return append(out, b...)
}

// gtArkBytes serializes a GT (Fp12) element exactly as arkworks
// CanonicalSerialize does: 12 base-field elements of 48 bytes each in
// little-endian, in tower order
// c0.c0.c0, c0.c0.c1, c0.c1.c0, c0.c1.c1, c0.c2.c0, c0.c2.c1, then c1.*.
// gnark-crypto and ark-bls12-381 use the same Fp12 tower
// (Fp2=Fp[u]/(u^2+1), Fp6=Fp2[v]/(v^3-(u+1)), Fp12=Fp6[w]/(w^2-v)), so
// coefficients map 1:1 (gnark Ci.Bj.Ak == ark ci.cj.ck); only the per-element
// byte order differs (gnark Bytes() is big-endian).
func gtArkBytes(gt *bls12381.GT) []byte {
	out := make([]byte, 0, 12*48)
	for _, el := range [12][48]byte{
		gt.C0.B0.A0.Bytes(), gt.C0.B0.A1.Bytes(),
		gt.C0.B1.A0.Bytes(), gt.C0.B1.A1.Bytes(),
		gt.C0.B2.A0.Bytes(), gt.C0.B2.A1.Bytes(),
		gt.C1.B0.A0.Bytes(), gt.C1.B0.A1.Bytes(),
		gt.C1.B1.A0.Bytes(), gt.C1.B1.A1.Bytes(),
		gt.C1.B2.A0.Bytes(), gt.C1.B2.A1.Bytes(),
	} {
		// reverse big-endian -> little-endian
		for i := 47; i >= 0; i-- {
			out = append(out, el[i])
		}
	}
	return out
}

func xor32(a, b []byte) []byte {
	out := make([]byte, 32)
	subtle.XORBytes(out, a[:32], b[:32])
	return out
}
