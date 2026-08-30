package crv4

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
)

func TestFleetCommitmentInfoExactRuntime452Scale(t *testing.T) {
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
	prefix := make([]byte, registrationV452PrefixBytes)
	binary.LittleEndian.PutUint64(prefix[:8], 25_000_000)
	binary.LittleEndian.PutUint32(prefix[8:12], 7_827_242)
	info, err := EncodeFleetCommitmentInfo(hash)
	if err != nil {
		t.Fatal(err)
	}
	canonical := append(append([]byte(nil), prefix...), info...)
	got, block, err := decodeFleetCommitmentRegistrationV452(canonical)
	if err != nil || got != hash {
		t.Fatalf("canonical registration hash=%x error=%v", got, err)
	}
	if block != 7_827_242 {
		t.Fatalf("canonical registration block=%d, want 7827242", block)
	}

	// Each input is a valid-looking or near-valid alternative that a generic
	// metagraph parser can encounter. In particular, 0x87 is the runtime-452
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
		if decoded, decodeErr := DecodeFleetCommitmentRegistrationV452(encoded); decodeErr == nil {
			t.Errorf("%s decoded as %x", name, decoded)
		}
	}
}

func TestFleetCommitmentBlockStorageIsFixedWidthNotHeaderCompact(t *testing.T) {
	raw := []byte{0x9d, 0x7c, 0x78, 0x00}
	got, err := decodeFleetCommitmentBlockV452(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got != 7_896_221 {
		t.Fatalf("fixed-width block=%d, want 7896221", got)
	}

	for _, malformed := range [][]byte{
		{},
		{0, 0, 0, 0},
		{0x9d, 0x7c, 0x78},
		{0x9d, 0x7c, 0x78, 0, 0},
	} {
		if decoded, decodeErr := decodeFleetCommitmentBlockV452(malformed); decodeErr == nil {
			t.Errorf("malformed fixed-width block decoded as %d", decoded)
		}
	}
}

func TestFleetCommitmentWriteRejectsUnchangedStaleRegistration(t *testing.T) {
	expected := sha256.Sum256([]byte("unchanged fleet manifest"))
	blockHash := types.NewHash([]byte{1})
	stale := &FinalizedCommitment{Hash: expected, CommitmentBlock: 7_896_221, FinalizedAt: 7_897_200, FinalizedHash: blockHash}
	if err := ValidateFleetCommitmentWrite(expected, 7_897_200, stale); err == nil {
		t.Fatal("an unchanged stale registration was accepted as proof of a fresh write")
	}

	fresh := &FinalizedCommitment{Hash: expected, CommitmentBlock: 7_897_200, FinalizedAt: 7_897_200, FinalizedHash: blockHash}
	if err := ValidateFleetCommitmentWrite(expected, 7_897_200, fresh); err != nil {
		t.Fatalf("exact finalized write rejected: %v", err)
	}
}

func TestFleetCommitmentWriteRejectsAdjacentMalformedProofs(t *testing.T) {
	expected := sha256.Sum256([]byte("fleet manifest"))
	wrong := sha256.Sum256([]byte("other manifest"))
	blockHash := types.NewHash([]byte{1})
	for _, observed := range []*FinalizedCommitment{
		nil,
		{Hash: wrong, CommitmentBlock: 100, FinalizedAt: 100, FinalizedHash: blockHash},
		{Hash: expected, CommitmentBlock: 99, FinalizedAt: 100, FinalizedHash: blockHash},
		{Hash: expected, CommitmentBlock: 100, FinalizedAt: 101, FinalizedHash: blockHash},
		{Hash: expected, CommitmentBlock: 100, FinalizedAt: 100},
	} {
		if err := ValidateFleetCommitmentWrite(expected, 100, observed); err == nil {
			t.Errorf("malformed finalized write proof was accepted: %+v", observed)
		}
	}
}
