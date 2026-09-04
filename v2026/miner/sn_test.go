package miner

// sn_test.go — vectors for the head-tier bind intent (`bind-head`). The digest
// read from the contract is stubbed by recomputing headBindDigest locally
// exactly as the Solidity view does (the 168-byte domain-separated preimage
// pinned by sn/evm/test/HeadBinding.t.sol test_headBindDigest_exactBytes), so
// the test needs no RPC. The signature is produced by the provider's client
// Ed25519 key and must verify under that key with the (r,s) byte split the
// 0x402 precompile / contract uses.
//
// The claimMiner / headBindDigest / bindHead CALLDATA is no longer hand-rolled
// here — it is built by sn/stabi (and exercised by sn/stabi's own golden tests
// + miner/onchain). This file keeps only the miner-specific logic: the client
// Ed25519 signing of the bind digest and the argument parsers.

import (
	"crypto/ed25519"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// testKeccak256 is the EVM keccak-256 over the concatenated args (go-ethereum's
// crypto.Keccak256 — the same hash the contract uses).
func testKeccak256(data ...[]byte) [32]byte {
	var out [32]byte
	copy(out[:], crypto.Keccak256(data...))
	return out
}

// hex32 decodes a 32-byte hex string (0x-optional) into a [32]byte.
func hex32(t *testing.T, s string) [32]byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != 32 {
		t.Fatalf("hex32(%q): err %v len %d", s, err, len(b))
	}
	var out [32]byte
	copy(out[:], b)
	return out
}

const testPayoutColdkeyHex = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"

// snHeadBindDomain is keccak256("UR_ST_HEAD_BIND_V1") — HEAD_BIND_DOMAIN in the
// contract.
func snHeadBindDomain() [32]byte {
	return testKeccak256([]byte("UR_ST_HEAD_BIND_V1"))
}

// snHeadBindDigestLocal recomputes headBindDigest the way STSubnet.sol does:
//
//	keccak256(HEAD_BIND_DOMAIN ‖ chainid(32) ‖ contract(20) ‖ registrant(20)
//	          ‖ hotkey(32) ‖ clientId(32))   — a 168-byte preimage.
func snHeadBindDigestLocal(chainId uint64, contract common.Address, registrant common.Address, hotkey [32]byte, clientId [32]byte) [32]byte {
	domain := snHeadBindDomain()
	var chainWord [32]byte
	binary.BigEndian.PutUint64(chainWord[24:], chainId)
	preimage := make([]byte, 0, 168)
	preimage = append(preimage, domain[:]...)
	preimage = append(preimage, chainWord[:]...)
	preimage = append(preimage, contract[:]...)
	preimage = append(preimage, registrant[:]...)
	preimage = append(preimage, hotkey[:]...)
	preimage = append(preimage, clientId[:]...)
	if len(preimage) != 168 {
		panic("head-bind preimage must be 168 bytes")
	}
	return testKeccak256(preimage)
}

func TestSnSignBindHead(t *testing.T) {
	// Deterministic client key seed so the vector is reproducible. This is the
	// `.provider.key` identity; clientId is its Ed25519 public key.
	var seed [32]byte
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	privateKey := ed25519.NewKeyFromSeed(seed[:])
	var clientId [32]byte
	copy(clientId[:], privateKey.Public().(ed25519.PublicKey))

	var contract common.Address
	for i := range contract {
		contract[i] = 0x33
	}
	var registrant common.Address
	for i := range registrant {
		registrant[i] = 0x44
	}
	hotkey := hex32(t, "1234123412341234123412341234123412341234123412341234123412341234")

	// Stub the digest read: recompute it locally exactly as the contract view
	// would return it. chainid is folded into the digest by the contract, so
	// the provider signs whatever the eth_call returns.
	const chainId = uint64(945)
	digest := snHeadBindDigestLocal(chainId, contract, registrant, hotkey, clientId)

	intent := snSignBindHead(privateKey, registrant, hotkey, digest)

	if intent.clientId != clientId {
		t.Fatalf("intent client_id 0x%x; expected 0x%x", intent.clientId, clientId)
	}
	if intent.hotkey != hotkey || intent.registrant != registrant || intent.digest != digest {
		t.Fatalf("intent echoed inputs wrong: %+v", intent)
	}
	if len(intent.clientIdSig) != ed25519.SignatureSize {
		t.Fatalf("sig length %d; expected %d", len(intent.clientIdSig), ed25519.SignatureSize)
	}

	// The signature verifies under the client public key over the digest — this
	// is exactly what the contract's 0x402 verify(digest, clientId, r, s)
	// decides on-chain.
	if !ed25519.Verify(clientId[:], digest[:], intent.clientIdSig) {
		t.Fatalf("client_id signature does not verify under the client key")
	}

	// (r,s) mapping: the contract reads r = sig[0:32], s = sig[32:64] and
	// recombines them for the precompile. Reconstructing R‖S from those halves
	// must reproduce a verifying signature — i.e. ed25519.Sign's byte order is
	// the order the contract expects, no swap.
	r := intent.clientIdSig[0:32]
	s := intent.clientIdSig[32:64]
	recombined := append(append([]byte{}, r...), s...)
	if !ed25519.Verify(clientId[:], digest[:], recombined) {
		t.Fatalf("r‖s recombination does not verify — signature byte order mismatch")
	}

	// A signature is bound to the digest: any other digest (different
	// registrant, hotkey, contract, chain, or client_id) must fail, which is
	// why the submit path re-derives the digest under its own sender first.
	otherRegistrant := registrant
	otherRegistrant[0] ^= 0x01
	otherDigest := snHeadBindDigestLocal(chainId, contract, otherRegistrant, hotkey, clientId)
	if ed25519.Verify(clientId[:], otherDigest[:], intent.clientIdSig) {
		t.Fatalf("signature verified for a different registrant's digest")
	}
}

// TestSnBindHeadArgParsers covers the hotkey/registrant argument parsers
// (0x-optional hex, exact length enforced).
func TestSnBindHeadArgParsers(t *testing.T) {
	hk, err := parseBytes32Arg("--hotkey", "0x"+testPayoutColdkeyHex)
	if err != nil || fmt.Sprintf("%x", hk) != testPayoutColdkeyHex {
		t.Fatalf("parseBytes32Arg 0x-prefixed: %x err %v", hk, err)
	}
	if _, err := parseBytes32Arg("--hotkey", testPayoutColdkeyHex); err != nil {
		t.Fatalf("parseBytes32Arg bare hex: %v", err)
	}
	if _, err := parseBytes32Arg("--hotkey", "0x1234"); err == nil {
		t.Fatalf("parseBytes32Arg accepted short hex")
	}

	addr, err := parseEvmAddressArg("--registrant", "0x2222222222222222222222222222222222222222")
	if err != nil || fmt.Sprintf("%x", addr) != "2222222222222222222222222222222222222222" {
		t.Fatalf("parseEvmAddressArg: %x err %v", addr, err)
	}
	if _, err := parseEvmAddressArg("--registrant", "0x22"); err == nil {
		t.Fatalf("parseEvmAddressArg accepted short address")
	}
}
