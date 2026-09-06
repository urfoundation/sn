package main

// Keeps unsigned-wire reuse tied to the actual transcript type closure and
// preserves the legacy invalid-UTF-8 normalization fallback.

import (
	"bytes"
	"encoding"
	"encoding/json"
	"reflect"
	"testing"
)

// New custom codecs or dynamically typed values need an explicit wire
// normalization review before the one-pass transcript fast path can use them.
func TestFinalPublicTranscriptNormalizationTypeClosure(t *testing.T) {
	t.Parallel()
	rawMessageType := reflect.TypeOf(json.RawMessage{})
	codecTypes := []reflect.Type{
		reflect.TypeOf((*json.Marshaler)(nil)).Elem(),
		reflect.TypeOf((*json.Unmarshaler)(nil)).Elem(),
		reflect.TypeOf((*encoding.TextMarshaler)(nil)).Elem(),
		reflect.TypeOf((*encoding.TextUnmarshaler)(nil)).Elem(),
	}
	seenTypes := make(map[reflect.Type]bool)
	var inspect func(reflect.Type)
	inspect = func(valueType reflect.Type) {
		if valueType == rawMessageType || seenTypes[valueType] {
			return
		}
		seenTypes[valueType] = true
		for _, codecType := range codecTypes {
			if valueType.Implements(codecType) || reflect.PointerTo(valueType).Implements(codecType) {
				t.Fatalf("transcript type %s requires a custom normalization review", valueType)
			}
		}
		switch valueType.Kind() {
		case reflect.Pointer, reflect.Array, reflect.Slice:
			inspect(valueType.Elem())
		case reflect.Struct:
			for index := range valueType.NumField() {
				field := valueType.Field(index)
				if field.IsExported() && field.Tag.Get("json") != "-" {
					inspect(field.Type)
				}
			}
		case reflect.String, reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		default:
			t.Fatalf("transcript type %s is outside the reviewed normalization closure", valueType)
		}
	}
	inspect(reflect.TypeOf(FinalPublicChainVerification{}))
}

// An invalid typed byte requires the legacy second marshal, while the already
// decoded replacement rune has the same hash and takes the ordinary one pass.
func TestFinalPublicTranscriptNormalizationRechecksReplacementEscapes(t *testing.T) {
	t.Parallel()
	verification, chainID, genesisHash := finalTranscriptWorkFixture(t)
	verification.NativePayouts[0].UIDs[0].Hotkey = "invalid-\xff"
	verification.TranscriptHash = finalTranscriptLegacyNormalizedHash(t, verification, chainID, genesisHash)
	for _, entry := range []struct {
		hotkey       string
		marshalCount int
	}{
		{hotkey: "invalid-\xff", marshalCount: 2},
		{hotkey: "invalid-\ufffd", marshalCount: 1},
	} {
		verification.NativePayouts[0].UIDs[0].Hotkey = entry.hotkey
		marshalCount := 0
		err := verifyFinalPublicChainVerificationWithMarshal(verification, chainID, genesisHash, func(value any) ([]byte, error) {
			marshalCount++
			return json.Marshal(value)
		})
		if err != nil || marshalCount != entry.marshalCount {
			t.Fatalf("hotkey=%q marshals=%d want=%d error=%v", entry.hotkey, marshalCount, entry.marshalCount, err)
		}
	}
}

// Raw JSON retains its lexical identity through the one-pass path too. This
// control has no invalid typed string that could select the legacy fallback.
func TestFinalPublicTranscriptNormalizationPreservesOnePassRawWire(t *testing.T) {
	t.Parallel()
	verification, chainID, genesisHash := finalTranscriptWorkFixture(t)
	for _, probe := range []struct {
		params string
		result json.RawMessage
	}{
		{params: ` [ { "z": -0, "a": 1e+3, "a": 2, "large": 9007199254740993 } ] `, result: json.RawMessage(` { "last": 1.0, "first": 1e-7 } `)},
		{params: `["<>&", "\u2028\u2029", "\ud800"]`, result: json.RawMessage("\"<>&\u2028\u2029\" ")},
		{params: "[\"invalid-\xff\"]", result: json.RawMessage("\"invalid-\xfe\"")},
		{params: `[]`, result: nil},
		{params: `[null,true,false,0.000001,1e21]`, result: json.RawMessage(` null `)},
	} {
		finalTranscriptAppendRawProbe(t, verification, json.RawMessage(probe.params), probe.result)
	}
	verification.TranscriptHash = finalTranscriptLegacyNormalizedHash(t, verification, chainID, genesisHash)
	unsigned := *verification
	unsigned.TranscriptHash = ""
	firstWire, err := json.Marshal(unsigned)
	if err != nil || bytes.Contains(firstWire, []byte(`\ufffd`)) {
		t.Fatalf("raw control unexpectedly requires the typed replacement fallback: %v", err)
	}
	var normalized FinalPublicChainVerification
	if err := json.Unmarshal(firstWire, &normalized); err != nil {
		t.Fatal(err)
	}
	secondWire, err := json.Marshal(normalized)
	if err != nil || !bytes.Equal(firstWire, secondWire) {
		t.Fatalf("unsigned raw wire changed under the legacy round trip: %v", err)
	}
	marshalCount := 0
	err = verifyFinalPublicChainVerificationWithMarshal(verification, chainID, genesisHash, func(value any) ([]byte, error) {
		marshalCount++
		return json.Marshal(value)
	})
	if err != nil || marshalCount != 1 {
		t.Fatalf("one-pass raw verification marshals=%d want=1 error=%v", marshalCount, err)
	}
}
