package protocol

// Shared literal bytes pin the packed Go/Solidity boundary independently of
// either encoder. All keys are the established synthetic public test keys.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"math"
	"testing"
)

const validatorEvidenceActivationGoldenHex = "75726e6574776f726b2f76616c696461746f722d65766964656e63652d61637469766174696f6e2f76310000000000000003b1131313131313131313131313131313131313131313131313131313131313131300111111111111111111111111111111111111111111121212121212121212121212121212121212121214141414141414141414141414141414141414141414141414141414141414141515151515151515151515151515151515151515151515151515151515151515000000000000002a94ad8d1ead1a2bff9bbbac89aa89b13df2fe9ec929a09c90bc5ddb1dff723b47000000000000000703a107bff3ce10be1d70dd18e74bc09967e4d6309ba50d5f1ddc8664125531b80000000000000005210000000000000000000000000000000000000000000000000000000000000000000000000001f4220000000000000000000000000000000000000000000000000000000000000000000000000003d42300000000000000000000000000000000000000000000000000000000000000"

// The reference is literal-width input, not output copied from Payload.
func TestValidatorEvidenceActivationGoldenWire(t *testing.T) {
	t.Parallel()
	reference, err := hex.DecodeString(validatorEvidenceActivationGoldenHex)
	if err != nil {
		t.Fatal(err)
	}
	activation, _, _ := validatorEvidenceActivationFixture(t)
	payload, err := activation.Payload()
	if err != nil || len(reference) != 389 || !bytes.Equal(payload, reference) {
		t.Fatalf("activation differs from fixed Go/Solidity vector: %v", err)
	}
	digest, err := activation.Digest()
	if err != nil || digest != sha256.Sum256(reference) {
		t.Fatalf("activation digest differs from fixed byte vector: %v", err)
	}
	decoded, err := DecodeValidatorEvidenceActivationPayload(reference)
	if err != nil || decoded != activation {
		t.Fatalf("activation decoder differs from fixed byte vector: %v", err)
	}
}

// Every independently valid field change survives decoding, including unsigned
// maxima. Mutating the input afterward cannot alter any decoded field.
func TestValidatorEvidenceActivationPackedDecodeOwnsEveryField(t *testing.T) {
	t.Parallel()
	original, _, _ := validatorEvidenceActivationFixture(t)
	variants := []ValidatorEvidenceActivation{original}
	for _, mutation := range validatorEvidenceActivationMutations() {
		changed := original
		mutation.edit(&changed)
		variants = append(variants, changed)
	}
	maximum := original
	maximum.Domain.ChainID, maximum.Domain.Netuid, maximum.Domain.Epoch = math.MaxUint64, math.MaxUint16, math.MaxUint64
	maximum.NoID, maximum.FirstSequence, maximum.NativeBlock, maximum.EVMBlock = math.MaxUint64, math.MaxUint64-1, math.MaxUint64, math.MaxUint64
	variants = append(variants, maximum)
	empty := original
	empty.Domain.Epoch, empty.FirstSequence, empty.PriorRoot = 0, 1, [32]byte{}
	variants = append(variants, empty)
	for index, variant := range variants {
		payload, err := variant.Payload()
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := DecodeValidatorEvidenceActivationPayload(payload)
		if err != nil || decoded != variant {
			t.Fatalf("activation variant %d lost a field: %v", index, err)
		}
		for offset := range payload {
			payload[offset] ^= 0xff
		}
		if decoded != variant {
			t.Fatalf("activation variant %d aliases packed input", index)
		}
	}
}

// Truncation, suffixes and every domain-prefix byte refuse before field reads.
func TestValidatorEvidenceActivationPackedDecodeRejectsFraming(t *testing.T) {
	t.Parallel()
	reference, err := hex.DecodeString(validatorEvidenceActivationGoldenHex)
	if err != nil {
		t.Fatal(err)
	}
	variants := [][]byte{append(bytes.Clone(reference), 0), append(bytes.Clone(reference), reference...)}
	for length := 0; length < len(reference); length++ {
		variants = append(variants, bytes.Clone(reference[:length]))
	}
	for offset := 0; offset < 43; offset++ {
		changed := bytes.Clone(reference)
		changed[offset] ^= 1
		variants = append(variants, changed)
	}
	for index, variant := range variants {
		if decoded, err := DecodeValidatorEvidenceActivationPayload(variant); err == nil || decoded != (ValidatorEvidenceActivation{}) {
			t.Fatalf("activation framing %d returned an admitted or partial record", index)
		}
	}
}

// All required fixed fields must be present, even with exactly correct framing.
// These literal offsets are independent of the production encoder/decoder.
func TestValidatorEvidenceActivationPackedDecodeRejectsIncompleteFields(t *testing.T) {
	t.Parallel()
	reference, err := hex.DecodeString(validatorEvidenceActivationGoldenHex)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []struct{ offset, width int }{
		{offset: 43, width: 8}, {offset: 51, width: 32}, {offset: 83, width: 2},
		{offset: 85, width: 20}, {offset: 105, width: 20}, {offset: 125, width: 32},
		{offset: 157, width: 32}, {offset: 197, width: 32}, {offset: 229, width: 8},
		{offset: 237, width: 32}, {offset: 269, width: 8}, {offset: 277, width: 32},
		{offset: 309, width: 8}, {offset: 317, width: 32}, {offset: 349, width: 8},
		{offset: 357, width: 32},
	} {
		changed := bytes.Clone(reference)
		clear(changed[field.offset : field.offset+field.width])
		if decoded, err := DecodeValidatorEvidenceActivationPayload(changed); err == nil || decoded != (ValidatorEvidenceActivation{}) {
			t.Fatalf("activation zero field at %d returned an admitted or partial record", field.offset)
		}
	}
	aliased := bytes.Clone(reference)
	copy(aliased[105:125], aliased[85:105])
	exhausted := bytes.Clone(reference)
	for offset := 269; offset < 277; offset++ {
		exhausted[offset] = 0xff
	}
	inconsistent := bytes.Clone(reference)
	clear(inconsistent[269:277])
	inconsistent[276] = 1
	for _, invalid := range [][]byte{aliased, exhausted, inconsistent} {
		if decoded, err := DecodeValidatorEvidenceActivationPayload(invalid); err == nil || decoded != (ValidatorEvidenceActivation{}) {
			t.Fatal("activation accepted invalid identity or migration shape")
		}
	}
}
