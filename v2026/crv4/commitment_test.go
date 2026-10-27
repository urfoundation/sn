package crv4

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
)

// The fleet commitment is one Data::Raw40 field: "urfleet1" then the hash.
func TestFleetCommitmentInfoIsTaggedRaw40(t *testing.T) {
	var hash [32]byte
	for i := range hash {
		hash[i] = byte(i + 1)
	}
	got, err := EncodeFleetCommitmentInfo(hash)
	if err != nil {
		t.Fatal(err)
	}
	want := append([]byte{0x04, 0x29, 'u', 'r', 'f', 'l', 'e', 'e', 't', '1'}, hash[:]...)
	if len(want) != 42 || FleetCommitmentFieldBytes != 40 {
		t.Fatalf("fleet info is %d bytes, field %d", len(want), FleetCommitmentFieldBytes)
	}
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
	prefix := make([]byte, registrationFixedU32PrefixBytes)
	binary.LittleEndian.PutUint64(prefix[:8], 25_000_000)
	binary.LittleEndian.PutUint32(prefix[8:12], 7_827_242)
	info, err := EncodeFleetCommitmentInfo(hash)
	if err != nil {
		t.Fatal(err)
	}
	canonical := append(append([]byte(nil), prefix...), info...)
	got, block, err := decodeFleetCommitmentRegistrationFixedU32(canonical)
	if err != nil || got != hash {
		t.Fatalf("canonical registration hash=%x error=%v", got, err)
	}
	if block != 7_827_242 {
		t.Fatalf("canonical registration block=%d, want 7827242", block)
	}
	legacy, err := DecodeFleetCommitmentRegistrationV452(canonical)
	if err != nil || legacy != hash {
		t.Fatalf("persisted v452 compatibility decoder hash=%x error=%v", legacy, err)
	}
	current, err := DecodeFleetCommitmentRegistrationV454(canonical)
	if err != nil || current != hash {
		t.Fatalf("current v454 decoder hash=%x error=%v", current, err)
	}

	// Each input is a valid-looking or near-valid alternative that a generic
	// metagraph parser can encounter. In particular, 0x87 is the v452-v454
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
		if decoded, decodeErr := DecodeFleetCommitmentRegistrationV454(encoded); decodeErr == nil {
			t.Errorf("%s decoded as %x", name, decoded)
		}
	}
}

func TestFleetCommitmentBlockStorageIsFixedWidthNotHeaderCompact(t *testing.T) {
	raw := []byte{0x9d, 0x7c, 0x78, 0x00}
	got, err := decodeFleetCommitmentBlockFixedU32(raw)
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
		if decoded, decodeErr := decodeFleetCommitmentBlockFixedU32(malformed); decodeErr == nil {
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

// The commitment oracle's decoders return the registration's fixed-width u32
// block and LastCommitment's block, including a block whose low bits would
// select a compact SCALE mode.
func TestDecodeFleetCommitmentRegistrationWithBlock(t *testing.T) {
	hash := [32]byte{0xf8, 0xcf, 0x01}
	raw := append(append([]byte{0, 0, 0, 0, 0, 0, 0, 0, 0x61, 0x4b, 0x8d, 0x00, 0x04, 0x29}, FleetCommitmentTag...), hash[:]...)
	decoded, block, err := DecodeFleetCommitmentRegistrationWithBlockV454(raw)
	if err != nil || decoded != hash || block != 9259873 {
		t.Fatalf("decoded 0x%x block %d: %v", decoded, block, err)
	}
	if last, err := DecodeFleetCommitmentLastBlock([]byte{0x61, 0x4b, 0x8d, 0x00}); err != nil || last != 9259873 {
		t.Fatalf("last %d: %v", last, err)
	}
	if _, err := DecodeFleetCommitmentLastBlock([]byte{0x61, 0x4b, 0x8d}); err == nil {
		t.Fatal("accepted a three-byte LastCommitment")
	}
	if _, _, err := DecodeFleetCommitmentRegistrationWithBlockV454(append(raw, 0)); err == nil {
		t.Fatal("accepted a trailing byte")
	}
}

// The validator source commitment keeps its exact one-field Sha256 bytes.
func TestSourceCommitmentInfoRetainsExactSha256Scale(t *testing.T) {
	var hash [32]byte
	for i := range hash {
		hash[i] = byte(i + 1)
	}
	got, err := EncodeSourceCommitmentInfo(hash)
	if err != nil {
		t.Fatal(err)
	}
	want := append([]byte{0x04, 0x83}, hash[:]...)
	if !bytes.Equal(got, want) {
		t.Fatalf("SCALE = %x, want %x", got, want)
	}
	if _, err := EncodeSourceCommitmentInfo([32]byte{}); err == nil {
		t.Fatal("expected zero hash rejection")
	}
	prefix := []byte{0, 0, 0, 0, 0, 0, 0, 0, 0x61, 0x4b, 0x8d, 0x00}
	decoded, block, err := DecodeSourceCommitmentRegistrationWithBlock(append(append([]byte(nil), prefix...), want...))
	if err != nil || decoded != hash || block != 9259873 {
		t.Fatalf("source registration 0x%x block %d: %v", decoded, block, err)
	}
	fleet, err := EncodeFleetCommitmentInfo(hash)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := DecodeSourceCommitmentRegistrationWithBlock(append(append([]byte(nil), prefix...), fleet...)); err == nil {
		t.Fatal("the source decoder accepted a tagged fleet commitment")
	}
}

// Writer/reader round trip of the tagged fleet registration, and every
// malformed tag, variant or length is refused. A one-field Sha256 (a legacy
// fleet or a validator source commitment) is ErrLegacyFleetCommitment.
func TestFleetCommitmentTaggedRoundTripAndRejections(t *testing.T) {
	hash := sha256.Sum256([]byte("fleet manifest"))
	prefix := make([]byte, registrationFixedU32PrefixBytes)
	binary.LittleEndian.PutUint64(prefix[:8], 0)
	binary.LittleEndian.PutUint32(prefix[8:], 9_300_000)
	info, err := EncodeFleetCommitmentInfo(hash)
	if err != nil {
		t.Fatal(err)
	}
	registration := append(append([]byte(nil), prefix...), info...)
	if len(registration) != 54 {
		t.Fatalf("registration is %d bytes, want 54", len(registration))
	}
	got, block, err := DecodeFleetCommitmentRegistrationWithBlockV454(registration)
	if err != nil || got != hash || block != 9_300_000 {
		t.Fatalf("round trip 0x%x block %d: %v", got, block, err)
	}
	if direct, err := DecodeFleetCommitmentInfo(info); err != nil || direct != hash {
		t.Fatalf("info round trip 0x%x: %v", direct, err)
	}

	with := func(info ...[]byte) []byte {
		out := append([]byte(nil), prefix...)
		for _, part := range info {
			out = append(out, part...)
		}
		return out
	}
	source, err := EncodeSourceCommitmentInfo(hash)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeFleetCommitmentRegistrationV454(with(source)); !errors.Is(err, ErrLegacyFleetCommitment) {
		t.Fatalf("one-field Sha256: %v, want ErrLegacyFleetCommitment", err)
	}
	raw39 := append([]byte{0x04, 0x28}, append([]byte(FleetCommitmentTag), hash[:31]...)...)
	raw41 := append([]byte{0x04, 0x2a}, append([]byte(FleetCommitmentTag), append(hash[:], 0)...)...)
	for name, encoded := range map[string][]byte{
		"wrong tag":                    with([]byte{0x04, 0x29}, []byte("urfleet2"), hash[:]),
		"tag case":                     with([]byte{0x04, 0x29}, []byte("URFLEET1"), hash[:]),
		"no tag, raw32 hash":           with([]byte{0x04, 0x21}, hash[:]),
		"tag in raw39 truncated hash":  with(raw39),
		"tag in raw41 extra byte":      with(raw41),
		"raw40 variant label wrong":    with([]byte{0x04, 0x28}, []byte(FleetCommitmentTag), hash[:]),
		"two fields":                   with([]byte{0x08, 0x29}, []byte(FleetCommitmentTag), hash[:], []byte{0x83}, hash[:]),
		"sha256 then tagged":           with([]byte{0x08, 0x83}, hash[:], []byte{0x29}, []byte(FleetCommitmentTag), hash[:]),
		"zero hash":                    with([]byte{0x04, 0x29}, []byte(FleetCommitmentTag), make([]byte, 32)),
		"trailing byte":                append(append([]byte(nil), registration...), 0),
		"truncated":                    registration[:len(registration)-1],
		"zero block":                   append(make([]byte, registrationFixedU32PrefixBytes), info...),
		"empty field list":             with([]byte{0x00}),
		"prefix only":                  append([]byte(nil), prefix...),
		"bigraw carrying tagged bytes": with([]byte{0x04, 0x88, 0xa0}, []byte(FleetCommitmentTag), hash[:]),
	} {
		if decoded, _, err := DecodeFleetCommitmentRegistrationWithBlockV454(encoded); err == nil {
			t.Errorf("%s decoded as 0x%x", name, decoded)
		}
	}
}

// Validator-side guard: no source commitment, for any hash (including one
// that starts with the tag bytes), encodes as a fleet commitment, and the
// guard itself refuses tagged bytes.
func TestSourceCommitmentWriterNeverProducesFleetTag(t *testing.T) {
	for index := 0; index < 4096; index++ {
		hash := sha256.Sum256(binary.LittleEndian.AppendUint32(nil, uint32(index)))
		if index == 0 {
			copy(hash[:], FleetCommitmentTag)
		}
		info, err := EncodeSourceCommitmentInfo(hash)
		if err != nil {
			t.Fatalf("hash %d: %v", index, err)
		}
		if _, err := DecodeFleetCommitmentInfo(info); err == nil {
			t.Fatalf("source info %x decodes as a fleet commitment", info)
		}
		if info[1] != dataSHA256Variant || len(info) != 34 {
			t.Fatalf("source info %x is not one Sha256 field", info)
		}
	}
	fleet, err := EncodeFleetCommitmentInfo(sha256.Sum256([]byte("fleet")))
	if err != nil {
		t.Fatal(err)
	}
	if checkNotFleetCommitment(fleet) == nil {
		t.Fatal("the source guard accepted a tagged fleet commitment")
	}
}

// Against the exact runtime-478 metadata, the fleet call is
// Commitments.set_commitment (0x12 0x00) with one Data::Raw40 field (variant
// 41) of the tag and hash, and the source anchor keeps its Sha256 bytes.
func TestFleetAndSourceCommitmentCallsRuntime478(t *testing.T) {
	metadata, _ := runtime478TestMetadata(t)
	chain := &Chain{Meta: metadata}
	hash := sha256.Sum256([]byte("fleet manifest"))
	fleet, err := chain.NewSetFleetCommitmentCall(25, hash)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := codec.Encode(fleet)
	if err != nil {
		t.Fatal(err)
	}
	want := append(append([]byte{0x12, 0x00, 25, 0, 0x04, 0x29}, FleetCommitmentTag...), hash[:]...)
	if !bytes.Equal(encoded, want) {
		t.Fatalf("fleet call %x, want %x", encoded, want)
	}
	source, err := chain.NewSetSourceCommitmentCall(25, hash)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err = codec.Encode(source)
	if err != nil {
		t.Fatal(err)
	}
	want = append([]byte{0x12, 0x00, 25, 0, 0x04, 0x83}, hash[:]...)
	if !bytes.Equal(encoded, want) {
		t.Fatalf("source call %x, want %x", encoded, want)
	}
}

// The pallet_commitments Data enum in the runtime-478 metadata: the fleet
// format's Raw40 is variant 41 holding [u8; 40], and the source format's
// Sha256 is variant 131. Runtime 479 (release-479, e66c7eed) has the same
// commitments types.rs.
func TestCommitmentDataVariantsRuntime478(t *testing.T) {
	metadata, _ := runtime478TestMetadata(t)
	found := false
	for _, entry := range metadata.AsMetadataV14.Lookup.Types {
		path := entry.Type.Path
		if len(path) != 3 || path[0] != "pallet_commitments" || path[1] != "types" || path[2] != "Data" {
			continue
		}
		found = true
		variants := map[string]types.Si1Variant{}
		for _, variant := range entry.Type.Def.Variant.Variants {
			variants[string(variant.Name)] = variant
		}
		raw40, sha := variants["Raw40"], variants["Sha256"]
		if uint8(raw40.Index) != dataRaw40Variant || uint8(sha.Index) != dataSHA256Variant {
			t.Fatalf("Raw40 index %d, Sha256 index %d", raw40.Index, sha.Index)
		}
		if len(raw40.Fields) != 1 {
			t.Fatalf("Raw40 has %d fields", len(raw40.Fields))
		}
		field := metadata.AsMetadataV14.EfficientLookup[raw40.Fields[0].Type.Int64()]
		if field == nil || !field.Def.IsArray || uint32(field.Def.Array.Len) != uint32(FleetCommitmentFieldBytes) {
			t.Fatalf("Raw40 field is not [u8; %d]", FleetCommitmentFieldBytes)
		}
	}
	if !found {
		t.Fatal("pallet_commitments::types::Data is absent")
	}
}

// A one-field Sha256 registration is legacy whatever its block, including 0.
func TestLegacyFleetCommitmentDetectedBeforeBlockCheck(t *testing.T) {
	hash := sha256.Sum256([]byte("legacy"))
	info, err := EncodeSourceCommitmentInfo(hash)
	if err != nil {
		t.Fatal(err)
	}
	registration := append(make([]byte, registrationFixedU32PrefixBytes), info...)
	if _, _, err := DecodeFleetCommitmentRegistrationWithBlockV454(registration); !errors.Is(err, ErrLegacyFleetCommitment) {
		t.Fatalf("block-0 legacy registration: %v", err)
	}
}
