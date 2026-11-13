package scale

import (
	"bytes"
	"errors"
	"math/big"
	"strings"
	"testing"
)

func boundedTestDecoder(t *testing.T, raw []byte, limits DecoderLimits) *Decoder {
	t.Helper()
	decoder, err := NewDecoderWithLimits(bytes.NewReader(raw), limits)
	if err != nil {
		t.Fatal(err)
	}
	return decoder
}

func boundedTestLimits() DecoderLimits {
	return DecoderLimits{MaxCollectionElements: 1024, MaxAllocationBytes: 64 * 1024, MaxDecodedValues: 1024, MaxDepth: 64}
}

// A short length prefix must be refused before publishing a backing allocation.
func TestBoundedDecoderRefusesVectorBeforeAllocation(t *testing.T) {
	decoder := boundedTestDecoder(t, []byte{2, 0, 4, 0}, boundedTestLimits())
	var values [][512]byte
	if err := decoder.Decode(&values); !errors.Is(err, ErrDecodeResourceLimit) || !strings.Contains(err.Error(), "collection elements") || values != nil {
		t.Fatalf("unbacked vector reached allocation: len=%d err=%v", len(values), err)
	}
}

// Every individual allocation fits; only the shared recursive total refuses.
func TestBoundedDecoderSharesNestedAllocationBudget(t *testing.T) {
	var wire bytes.Buffer
	if err := NewEncoder(&wire).Encode([][]byte{{1, 2, 3, 4, 5, 6, 7, 8}, {9, 10, 11, 12, 13, 14, 15, 16}}); err != nil {
		t.Fatal(err)
	}
	limits := boundedTestLimits()
	limits.MaxAllocationBytes = 768
	decoder := boundedTestDecoder(t, wire.Bytes(), limits)
	var values [][]byte
	err := decoder.Decode(&values)
	if !errors.Is(err, ErrDecodeResourceLimit) || !strings.Contains(err.Error(), "allocation bytes") || len(values) != 2 || len(values[0]) != 8 || values[1] != nil {
		t.Fatalf("nested allocation budget reset: values=%v err=%v", values, err)
	}
	if err := decoder.Decode(new(byte)); !errors.Is(err, ErrDecodeResourceLimit) {
		t.Fatalf("failed budget resumed: %v", err)
	}
	var independent [][]byte
	if err := boundedTestDecoder(t, wire.Bytes(), boundedTestLimits()).Decode(&independent); err != nil || len(independent) != 2 || len(independent[1]) != 8 {
		t.Fatalf("independent owner inherited an exhausted budget: %v", err)
	}
}

// Zero-sized elements can consume work without backing bytes or input reads.
func TestBoundedDecoderCountsZeroSizedWork(t *testing.T) {
	var wire bytes.Buffer
	if err := NewEncoder(&wire).Encode(make([]struct{}, 100)); err != nil {
		t.Fatal(err)
	}
	limits := boundedTestLimits()
	limits.MaxDecodedValues = 8
	var values []struct{}
	err := boundedTestDecoder(t, wire.Bytes(), limits).Decode(&values)
	if !errors.Is(err, ErrDecodeResourceLimit) || !strings.Contains(err.Error(), "decoded values") {
		t.Fatalf("zero-byte elements escaped the work budget: %v", err)
	}
}

type boundedRecursiveTestValue struct {
	Depth int
}

func (self *boundedRecursiveTestValue) Decode(decoder Decoder) error {
	value, err := decoder.ReadOneByte()
	if err != nil || value == 0 {
		return err
	}
	var child boundedRecursiveTestValue
	if err := decoder.Decode(&child); err != nil {
		return err
	}
	self.Depth = child.Depth + 1
	return nil
}

// Custom decoders receive value copies, but cannot restart recursive limits.
func TestBoundedDecoderSharesCustomDepthBudget(t *testing.T) {
	limits := boundedTestLimits()
	limits.MaxDepth = 2
	var value boundedRecursiveTestValue
	err := boundedTestDecoder(t, []byte{1, 1, 1, 0}, limits).Decode(&value)
	if !errors.Is(err, ErrDecodeResourceLimit) || !strings.Contains(err.Error(), "depth") || value.Depth != 0 {
		t.Fatalf("custom depth was reset or partially published: %+v %v", value, err)
	}
	if err := boundedTestDecoder(t, []byte{1, 1, 1, 0}, boundedTestLimits()).Decode(&value); err != nil || value.Depth != 3 {
		t.Fatalf("valid recursive decoding changed: %+v %v", value, err)
	}
}

// A custom decoder's derived storage joins the same finite allocation owner.
func TestBoundedDecoderRejectsDerivedAllocationOverflow(t *testing.T) {
	decoder := boundedTestDecoder(t, nil, boundedTestLimits())
	if err := decoder.ReserveAllocation(^uint64(0), 2); !errors.Is(err, ErrDecodeResourceLimit) {
		t.Fatalf("derived size multiplication wrapped: %v", err)
	}
}

// Every truncated compact form returns an error in both bounded and legacy mode.
func TestDecoderTruncatedVectorLengthsReturnErrors(t *testing.T) {
	for _, raw := range [][]byte{nil, {1}, {2}, {2, 0}, {2, 0, 0}, {3}, {3, 0, 0, 0}} {
		for _, bounded := range []bool{false, true} {
			decoder := NewDecoder(bytes.NewReader(raw))
			if bounded {
				decoder = boundedTestDecoder(t, raw, boundedTestLimits())
			}
			var value []byte
			if err := decoder.Decode(&value); err == nil || value != nil {
				t.Fatalf("truncated compact %x accepted, bounded=%v: %v", raw, bounded, err)
			}
		}
	}
}

// A value above u64 must not truncate to an apparently empty protocol vector.
func TestDecoderRejectsVectorLengthIntegerTruncation(t *testing.T) {
	var wire bytes.Buffer
	length := new(big.Int).Lsh(big.NewInt(1), 64)
	if err := NewEncoder(&wire).EncodeUintCompact(*length); err != nil {
		t.Fatal(err)
	}
	var value []byte
	if err := NewDecoder(bytes.NewReader(wire.Bytes())).Decode(&value); err == nil || value != nil {
		t.Fatalf("out-of-protocol length truncated: len=%d err=%v", len(value), err)
	}
}

func TestDecoderRequiresOptionDiscriminant(t *testing.T) {
	hasValue := true
	var value byte
	if err := NewDecoder(bytes.NewReader(nil)).DecodeOption(&hasValue, &value); err == nil || !hasValue {
		t.Fatalf("absent option discriminant manufactured None: %v", err)
	}
}

type boundedArrayTestValue [2]byte

func (self *boundedArrayTestValue) Decode(decoder Decoder) error {
	return decoder.Read(self[:])
}

// An array's fixed holder is not a dynamically sized slice allocation.
func TestBoundedDecoderPreservesCustomArray(t *testing.T) {
	var value boundedArrayTestValue
	if err := boundedTestDecoder(t, []byte{1, 2}, boundedTestLimits()).Decode(&value); err != nil || value != (boundedArrayTestValue{1, 2}) {
		t.Fatalf("fixed custom array changed: %v %v", value, err)
	}
}

func TestBoundedDecoderRequiresFiniteLimits(t *testing.T) {
	for _, field := range []string{"elements", "allocation", "values", "depth"} {
		limits := boundedTestLimits()
		switch field {
		case "elements":
			limits.MaxCollectionElements = 0
		case "allocation":
			limits.MaxAllocationBytes = 0
		case "values":
			limits.MaxDecodedValues = 0
		case "depth":
			limits.MaxDepth = 0
		}
		if _, err := NewDecoderWithLimits(bytes.NewReader(nil), limits); err == nil {
			t.Fatal("zero limit enabled unbounded decoding:", field)
		}
	}
}
