package crv4

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"golang.org/x/crypto/blake2b"
)

func preparedFixture(t *testing.T) *PreparedSubmission {
	t.Helper()
	var hotkey [32]byte
	for index := range hotkey {
		hotkey[index] = byte(index + 1)
	}
	uids, values := []uint16{2, 9}, []uint16{123, 65535}
	payload, err := (&Payload{Hotkey: hotkey, Uids: uids, Values: values, VersionKey: 7}).Encode()
	if err != nil {
		t.Fatal(err)
	}
	ciphertext := []byte{0xde, 0xad, 0xbe, 0xef}
	body := append([]byte{0x84, 0xaa}, ciphertext...)
	raw := append([]byte{byte(len(body) << 2)}, body...)
	txHash := blake2b.Sum256(raw)
	cipherHash := sha256.Sum256(ciphertext)
	return &PreparedSubmission{
		Schema: PreparedSubmissionSchema, Netuid: 21, HotkeyHex: "0x" + hex.EncodeToString(hotkey[:]),
		VersionKey: 7, CommitRevealVersion: 4, AccountNonce: 3,
		PreparedAtBlock: 100, PreparedAtBlockHash: types.NewHash([]byte{1}).Hex(),
		RevealRound: 8, RevealBlock: 120, UIDs: uids, Values: values,
		PayloadHex: codec.HexEncodeToString(payload), CiphertextHex: codec.HexEncodeToString(ciphertext),
		CiphertextSHA256: "0x" + hex.EncodeToString(cipherHash[:]),
		ExtrinsicHex:     codec.HexEncodeToString(raw), ExtrinsicHash: types.Hash(txHash).Hex(),
	}
}

func TestPreparedSubmissionValidatesExactReplay(t *testing.T) {
	prepared := preparedFixture(t)
	if _, err := prepared.Validate(); err != nil {
		t.Fatal(err)
	}

	tests := map[string]func(*PreparedSubmission){
		"payload":    func(p *PreparedSubmission) { p.Values[0]++ },
		"ciphertext": func(p *PreparedSubmission) { p.CiphertextHex = "0x00" },
		"extrinsic":  func(p *PreparedSubmission) { p.ExtrinsicHash = types.Hash{}.Hex() },
		"schema":     func(p *PreparedSubmission) { p.Schema = "future" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			copy := *prepared
			copy.UIDs = append([]uint16(nil), prepared.UIDs...)
			copy.Values = append([]uint16(nil), prepared.Values...)
			mutate(&copy)
			if _, err := copy.Validate(); err == nil {
				t.Fatal("tampered prepared submission was accepted")
			}
		})
	}
}
