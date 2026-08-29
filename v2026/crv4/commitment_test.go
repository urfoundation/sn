package crv4

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
)

func TestFleetCommitmentInfoExactRuntime451Scale(t *testing.T) {
	var hash [32]byte
	for i := range hash {
		hash[i] = byte(i + 1)
	}
	got, err := EncodeFleetCommitmentInfo(hash)
	if err != nil {
		t.Fatal(err)
	}
	want := append([]byte{0x04, 0x83}, hash[:]...)
	if !bytes.Equal(got, want) {
		t.Fatalf("SCALE = %x, want %x", got, want)
	}

	// Pin the type's direct encoder as well as the public helper so a future
	// struct-tag/reflection change cannot silently alter the dispatch bytes.
	info, err := NewFleetCommitmentInfo(hash)
	if err != nil {
		t.Fatal(err)
	}
	direct, err := codec.Encode(info)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(direct, want) {
		t.Fatalf("direct SCALE = %x, want %x", direct, want)
	}
}

func TestFleetCommitmentInfoRejectsZero(t *testing.T) {
	if _, err := NewFleetCommitmentInfo([32]byte{}); err == nil {
		t.Fatal("expected zero hash rejection")
	}
	if _, err := EncodeFleetCommitmentInfo([32]byte{}); err == nil {
		t.Fatal("expected zero hash rejection")
	}
}

func TestDecodeFleetCommitmentRegistrationRejectsAmbiguousFields(t *testing.T) {
	var hash [32]byte
	for index := range hash {
		hash[index] = byte(index + 1)
	}
	prefix := make([]byte, registrationV451PrefixBytes)
	binary.LittleEndian.PutUint64(prefix[:8], 25_000_000)
	binary.LittleEndian.PutUint32(prefix[8:12], 7_827_242)
	info, err := EncodeFleetCommitmentInfo(hash)
	if err != nil {
		t.Fatal(err)
	}
	canonical := append(append([]byte(nil), prefix...), info...)
	got, err := DecodeFleetCommitmentRegistrationV451(canonical)
	if err != nil || got != hash {
		t.Fatalf("canonical registration hash=%x error=%v", got, err)
	}

	// Each input is a valid-looking or near-valid alternative that a generic
	// metagraph parser can encounter. In particular, 0x87 is the runtime-451
	// ResetBondsFlag variant and the first mutation is a two-field vector that
	// deliberately ends in the otherwise canonical Sha256 bytes.
	twoFieldsEndingInSHA := append(append(append([]byte(nil), prefix...), 0x08, 0x87, 0x83), hash[:]...)
	resetBondsOnly := append(append([]byte(nil), prefix...), 0x04, 0x87)
	zeroHash := append(append(append([]byte(nil), prefix...), 0x04, 0x83), make([]byte, 32)...)
	trailing := append(append([]byte(nil), canonical...), 0)
	for name, encoded := range map[string][]byte{
		"two fields ending in sha256": twoFieldsEndingInSHA,
		"reset bonds flag":            resetBondsOnly,
		"zero sha256":                 zeroHash,
		"trailing byte":               trailing,
		"truncated registration":      canonical[:len(canonical)-1],
	} {
		if decoded, decodeErr := DecodeFleetCommitmentRegistrationV451(encoded); decodeErr == nil {
			t.Errorf("%s decoded as %x", name, decoded)
		}
	}
}
