package types

import (
	"bytes"
	"errors"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/scale"
)

// The first empty-registry compact consumes the available reservation. Even
// the derived lookup map must be refused before construction; omitting its
// charge would create that map and fail only at a later pallet read.
func TestMetadataV14DerivedLookupSharesDecoderBudget(t *testing.T) {
	decoder, err := scale.NewDecoderWithLimits(bytes.NewReader([]byte{0, 0, 0, 0, 0}), scale.DecoderLimits{
		MaxCollectionElements: 8, MaxAllocationBytes: 256, MaxDecodedValues: 64, MaxDepth: 64,
	})
	if err != nil {
		t.Fatal(err)
	}
	var metadata MetadataV14
	if err := metadata.Decode(*decoder); !errors.Is(err, scale.ErrDecodeResourceLimit) || metadata.EfficientLookup != nil {
		t.Fatalf("derived lookup bypassed allocation admission: %v %v", metadata.EfficientLookup, err)
	}
}
