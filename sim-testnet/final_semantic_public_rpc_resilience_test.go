package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	gsrpcregistrytest "github.com/centrifuge/go-substrate-rpc-client/v4/registry/test"
	"github.com/centrifuge/go-substrate-rpc-client/v4/scale"
	gsrpctypes "github.com/centrifuge/go-substrate-rpc-client/v4/types"
	gsrpccodec "github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
)

// The transport reader must not re-read a block or its events through a
// contextless helper just to prove dispatch. This fixture exercises the same
// metadata-driven parser against the exact already-fetched System.Events wire
// value used by NativeEvent.
func finalSemanticSuccessfulDispatchFixture(t *testing.T, index uint32) (*gsrpctypes.Metadata, json.RawMessage) {
	t.Helper()
	var metadata gsrpctypes.Metadata
	if err := gsrpccodec.DecodeFromHex(gsrpcregistrytest.CentrifugeMetadataHex, &metadata); err != nil {
		t.Fatal(err)
	}
	var buffer bytes.Buffer
	encoder := scale.NewEncoder(&buffer)
	values := []any{
		gsrpctypes.NewUCompactFromUInt(1),
		gsrpctypes.Phase{IsApplyExtrinsic: true, AsApplyExtrinsic: index},
		gsrpctypes.EventID{0, 0}, // System.ExtrinsicSuccess in this metadata.
		gsrpctypes.DispatchInfo{
			Weight: gsrpctypes.Weight{
				RefTime:   gsrpctypes.NewUCompactFromUInt(1),
				ProofSize: gsrpctypes.NewUCompactFromUInt(2),
			},
			Class:   gsrpctypes.DispatchClass{IsNormal: true},
			PaysFee: gsrpctypes.Pays{IsYes: true},
		},
		[]gsrpctypes.Hash{},
	}
	for _, value := range values {
		if err := encoder.Encode(value); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := json.Marshal(gsrpccodec.HexEncodeToString(buffer.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	return &metadata, raw
}

func TestFinalSemanticSubstrateDispatchUsesPinnedTranscriptBytes(t *testing.T) {
	metadata, raw := finalSemanticSuccessfulDispatchFixture(t, 3)
	receipt := FinalNativeReceipt{
		ExtrinsicHash: finalTestHex(0x71),
		Block:         ChainHead{Number: 10, Hash: finalTestHex(0x72)},
	}
	if err := verifyFinalSemanticSubstrateDispatch(metadata, raw, 3, receipt); err != nil {
		t.Fatalf("pinned success dispatch was rejected: %v", err)
	}
	if err := verifyFinalSemanticSubstrateDispatch(metadata, raw, 4, receipt); err == nil || !strings.Contains(err.Error(), "no System.ExtrinsicSuccess") {
		t.Fatalf("wrong extrinsic phase was accepted: %v", err)
	}
	if err := verifyFinalSemanticSubstrateDispatch(metadata, json.RawMessage(`"0x01"`), 3, receipt); err == nil {
		t.Fatal("malformed pinned events were accepted")
	}
}
