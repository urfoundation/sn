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
