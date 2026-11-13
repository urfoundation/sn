package protocol

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/urfoundation/sn/v2026/crv4"
)

func bindingFixture(t *testing.T) (FleetBinding, ed25519.PrivateKey, *crv4.Keypair) {
	t.Helper()
	clientSeed := make([]byte, ed25519.SeedSize)
	for i := range clientSeed {
		clientSeed[i] = byte(i)
	}
	clientPrivate := ed25519.NewKeyFromSeed(clientSeed)
	hotSeed := [32]byte{}
	for i := range hotSeed {
		hotSeed[i] = byte(0x80 + i)
	}
	hot, err := crv4.KeypairFromSeed(hotSeed)
	if err != nil {
		t.Fatal(err)
	}
	b := FleetBinding{ChainID: 945, Netuid: 17, Generation: 3, ValidFromEpoch: 11, ValidToEpoch: 42}
	copy(b.Coordinator[:], mustHex(t, "1111111111111111111111111111111111111111"))
	copy(b.FleetID[:], mustHex(t, "2222222222222222222222222222222222222222222222222222222222222222"))
	b.Hotkey = hot.PublicKey()
	copy(b.ClientID[:], mustHex(t, "33333333333333333333333333333333"))
	copy(b.ClientKey[:], clientPrivate.Public().(ed25519.PublicKey))
	copy(b.CommitmentHash[:], mustHex(t, "4444444444444444444444444444444444444444444444444444444444444444"))
	return b, clientPrivate, hot
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestFleetBindingGoldenAndDualSignatures(t *testing.T) {
	b, clientPrivate, hot := bindingFixture(t)
	payload, err := b.Payload()
	if err != nil {
		t.Fatal(err)
	}
	if len(payload) != FleetBindingPayloadSize {
		t.Fatalf("payload length %d", len(payload))
	}
	digest, err := b.Digest()
	if err != nil {
		t.Fatal(err)
	}
	clientSig, err := b.SignClient(clientPrivate)
	if err != nil {
		t.Fatal(err)
	}
	if !b.VerifyClient(clientSig) {
		t.Fatal("client signature rejected")
	}
	hotSig, err := hot.Sign(digest[:])
	if err != nil {
		t.Fatal(err)
	}
	if !hot.Verify(digest[:], hotSig) {
		t.Fatal("hotkey signature rejected")
	}
	b.Netuid++
	if b.VerifyClient(clientSig) {
		t.Fatal("cross-netuid replay accepted")
	}
}

func TestFleetBindingCommittedGoldenFixture(t *testing.T) {
	type fixture struct {
		Payload         string `json:"payload"`
		Digest          string `json:"digest"`
		ClientSignature string `json:"client_signature"`
		HotSignature    string `json:"hotkey_signature"`
	}
	raw, err := os.ReadFile(filepath.Join("testdata", "fleet-binding-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var f fixture
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	b, clientPrivate, _ := bindingFixture(t)
	payload, _ := b.Payload()
	digest, _ := b.Digest()
	clientSig, _ := b.SignClient(clientPrivate)
	if "0x"+hex.EncodeToString(payload) != f.Payload {
		t.Fatal("payload differs from committed fixture")
	}
	if "0x"+hex.EncodeToString(digest[:]) != f.Digest {
		t.Fatal("digest differs from committed fixture")
	}
	if "0x"+hex.EncodeToString(clientSig) != f.ClientSignature {
		t.Fatal("client signature differs from committed fixture")
	}
	hotSig, err := hex.DecodeString(f.HotSignature[2:])
	if err != nil || !b.VerifyHotkey(hotSig) {
		t.Fatal("committed sr25519 signature rejected")
	}
}

// Lock the exact abi.encodePacked widths shared with the coordinator and prove
// that consent cannot be replayed across any routing coordinate.
func TestFleetRevokeGoldenAndDomainSeparation(t *testing.T) {
	binding, clientPrivate, _ := bindingFixture(t)
	revoke := FleetRevoke{
		ChainID:        binding.ChainID,
		Netuid:         binding.Netuid,
		Coordinator:    binding.Coordinator,
		ClientID:       binding.ClientID,
		Generation:     binding.Generation,
		EffectiveEpoch: binding.ValidFromEpoch,
	}
	payload, err := revoke.Payload()
	if err != nil {
		t.Fatal(err)
	}
	wantPayload := "75726e6574776f726b2f666c6565742d7265766f6b652f763100000000000003b100111111111111111111111111111111111111111111333333333333333333333333333333330000000000000003000000000000000b"
	if hex.EncodeToString(payload) != wantPayload {
		t.Fatalf("fleet revoke payload = %x, want %s", payload, wantPayload)
	}
	digest, err := revoke.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(digest[:]) != "1bdf55fa80f81eb279395bced9f453f9408d0dc438e685cdfd2bd78ce8adb7f3" {
		t.Fatalf("fleet revoke digest = %x", digest)
	}
	signature, err := revoke.SignClient(clientPrivate)
	if err != nil {
		t.Fatal(err)
	}
	publicKey := clientPrivate.Public().(ed25519.PublicKey)
	if !revoke.VerifyClient(publicKey, signature) {
		t.Fatal("fleet revoke signature rejected")
	}

	candidates := []FleetRevoke{revoke, revoke, revoke, revoke, revoke, revoke}
	candidates[0].ChainID++
	candidates[1].Netuid++
	candidates[2].Coordinator[0] ^= 1
	candidates[3].ClientID[0] ^= 1
	candidates[4].Generation++
	candidates[5].EffectiveEpoch++
	for _, candidate := range candidates {
		if candidate.VerifyClient(publicKey, signature) {
			t.Errorf("cross-domain fleet revoke accepted: %+v", candidate)
		}
	}
}

// Invalid coordinates must fail before producing a digest or signature.
func TestFleetRevokeRejectsZeroCoordinates(t *testing.T) {
	binding, clientPrivate, _ := bindingFixture(t)
	valid := FleetRevoke{
		ChainID:        binding.ChainID,
		Netuid:         binding.Netuid,
		Coordinator:    binding.Coordinator,
		ClientID:       binding.ClientID,
		Generation:     binding.Generation,
		EffectiveEpoch: binding.ValidFromEpoch,
	}
	candidates := []FleetRevoke{valid, valid, valid, valid, valid, valid}
	candidates[0].ChainID = 0
	candidates[1].Netuid = 0
	candidates[2].Coordinator = [20]byte{}
	candidates[3].ClientID = [16]byte{}
	candidates[4].Generation = 0
	candidates[5].EffectiveEpoch = 0
	for _, candidate := range candidates {
		if _, err := candidate.SignClient(clientPrivate); err == nil {
			t.Errorf("invalid fleet revoke was signed: %+v", candidate)
		}
	}
}
