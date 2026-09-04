package crv4

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func seqHotkey() [32]byte {
	var hk [32]byte
	for i := range hk {
		hk[i] = byte(i + 1)
	}
	return hk
}

func mustEncode(t *testing.T, p *Payload) []byte {
	t.Helper()
	b, err := p.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	return b
}

// TestPayloadGolden pins Payload.Encode against parity-scale-codec
// WeightsTlockPayload::encode() outputs generated with the Rust reference
// (see golden_test.go).
func TestPayloadGolden(t *testing.T) {
	cases := []struct {
		name    string
		payload Payload
		wantHex string
	}{
		{
			// compact(32)=0x80, hotkey 01..20, compact(4)=0x10 + uids LE,
			// compact(4)=0x10 + values LE, version 841 = 0x349 LE.
			name:    "basic",
			payload: Payload{Hotkey: seqHotkey(), Uids: []uint16{0, 1, 2, 7}, Values: []uint16{10, 20, 30, 65535}, VersionKey: 841},
			wantHex: goldenPayload1Hex,
		},
		{
			name:    "empty_vectors",
			payload: Payload{Hotkey: seqHotkey(), Uids: []uint16{}, Values: []uint16{}, VersionKey: 0},
			wantHex: goldenPayload2Hex,
		},
		{
			name: "extremes",
			payload: Payload{
				Hotkey:     [32]byte{0xAB, 0xAB, 0xAB, 0xAB, 0xAB, 0xAB, 0xAB, 0xAB, 0xAB, 0xAB, 0xAB, 0xAB, 0xAB, 0xAB, 0xAB, 0xAB, 0xAB, 0xAB, 0xAB, 0xAB, 0xAB, 0xAB, 0xAB, 0xAB, 0xAB, 0xAB, 0xAB, 0xAB, 0xAB, 0xAB, 0xAB, 0xAB},
				Uids:       []uint16{65535},
				Values:     []uint16{1},
				VersionKey: ^uint64(0),
			},
			wantHex: goldenPayload3Hex,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mustEncode(t, &tc.payload)
			if hex.EncodeToString(got) != tc.wantHex {
				t.Errorf("payload mismatch\n got %s\nwant %s", hex.EncodeToString(got), tc.wantHex)
			}
		})
	}
}

// TestPayloadGolden70Uids exercises the 2-byte compact length mode
// (70 entries) with values i*937 and version key 0x0102030405060708.
func TestPayloadGolden70Uids(t *testing.T) {
	p := Payload{Hotkey: seqHotkey(), VersionKey: 0x0102030405060708}
	for i := uint16(0); i < 70; i++ {
		p.Uids = append(p.Uids, i)
		p.Values = append(p.Values, i*937)
	}
	got := mustEncode(t, &p)
	if hex.EncodeToString(got) != goldenPayload4Hex {
		t.Errorf("payload mismatch\n got %s\nwant %s", hex.EncodeToString(got), goldenPayload4Hex)
	}
}

func TestPayloadLengthMismatch(t *testing.T) {
	p := Payload{Hotkey: seqHotkey(), Uids: []uint16{1}, Values: []uint16{}}
	if _, err := p.Encode(); err == nil {
		t.Fatal("expected error for uids/values length mismatch")
	}
}

func TestCompactEncoding(t *testing.T) {
	cases := []struct {
		v    uint64
		want []byte
	}{
		{0, []byte{0x00}},
		{1, []byte{0x04}},
		{32, []byte{0x80}},                              // hotkey length
		{63, []byte{0xfc}},                              // last 1-byte value
		{64, []byte{0x01, 0x01}},                        // first 2-byte value
		{70, []byte{0x19, 0x01}},                        // 70 uids
		{256, []byte{0x01, 0x04}},                       // max metagraph size
		{16383, []byte{0xfd, 0xff}},                     // last 2-byte value
		{16384, []byte{0x02, 0x00, 0x01, 0x00}},         // first 4-byte value
		{1 << 30, []byte{0x03, 0x00, 0x00, 0x00, 0x40}}, // first big-int value
	}
	for _, tc := range cases {
		got := appendCompact(nil, tc.v)
		if !bytes.Equal(got, tc.want) {
			t.Errorf("compact(%d) = %x, want %x", tc.v, got, tc.want)
		}
	}
}
