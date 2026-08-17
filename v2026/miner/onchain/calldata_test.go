package onchain

import (
	"bytes"
	"encoding/hex"
	"math/big"
	"strings"
	"testing"

	"golang.org/x/crypto/sha3"
)

// keccak256x is an independent keccak-256 (x/crypto/sha3), deliberately not
// go-ethereum's crypto.Keccak256, so the golden cross-check shares no code
// with the stabi packing path under test.
func keccak256x(data []byte) []byte {
	h := sha3.NewLegacyKeccak256()
	h.Write(data)
	return h.Sum(nil)
}

// word returns the 32-byte big-endian ABI word for v.
func word(v uint64) []byte {
	b := make([]byte, 32)
	new(big.Int).SetUint64(v).FillBytes(b)
	return b
}

const (
	aliceHex  = "d43593c715fdd31c61141abd04a99fd6822c8558854ccde39a5684e7a56da27d"
	aliceSS58 = "5GrwvaEF5zXb26Fz9rcQpDWS57CtERHpNehXCPcNoHGKutQY"

	// goldenClaimCalldata is claimMiner(7, 3, alice, 1234, [0x11..11, 0x22..22]):
	// selector ‖ e ‖ noId ‖ coldkey ‖ shareBps ‖ proof offset (0xa0) ‖
	// proof len ‖ proof[0] ‖ proof[1].
	goldenClaimCalldata = "0x4c207962" +
		"0000000000000000000000000000000000000000000000000000000000000007" +
		"0000000000000000000000000000000000000000000000000000000000000003" +
		"d43593c715fdd31c61141abd04a99fd6822c8558854ccde39a5684e7a56da27d" +
		"00000000000000000000000000000000000000000000000000000000000004d2" +
		"00000000000000000000000000000000000000000000000000000000000000a0" +
		"0000000000000000000000000000000000000000000000000000000000000002" +
		"1111111111111111111111111111111111111111111111111111111111111111" +
		"2222222222222222222222222222222222222222222222222222222222222222"
)

func goldenIntent(t *testing.T) *claimIntent {
	t.Helper()
	coldkey, err := parseColdkey("0x" + aliceHex)
	if err != nil {
		t.Fatalf("parseColdkey(alice hex): %v", err)
	}
	var n1, n2 [32]byte
	for i := range n1 {
		n1[i] = 0x11
		n2[i] = 0x22
	}
	return &claimIntent{
		E:        big.NewInt(7),
		NoID:     big.NewInt(3),
		Coldkey:  coldkey,
		ShareBps: big.NewInt(1234),
		Proof:    [][32]byte{n1, n2},
	}
}

// TestPackClaimMinerGolden pins the structured-mode packing to exact bytes,
// cross-checked against (a) an independently keccak-derived selector and
// (b) a hand-built ABI encoding assembled word by word in this test.
func TestPackClaimMinerGolden(t *testing.T) {
	got, err := buildClaimCalldata(goldenIntent(t))
	if err != nil {
		t.Fatalf("buildClaimCalldata: %v", err)
	}

	// (a) selector: keccak256 of the canonical signature, computed with
	// x/crypto/sha3 rather than go-ethereum.
	wantSel := keccak256x([]byte("claimMiner(uint256,uint256,bytes32,uint256,bytes32[])"))[:4]
	if hex.EncodeToString(wantSel) != "4c207962" {
		t.Fatalf("independent selector = %x, want 4c207962", wantSel)
	}
	if !bytes.Equal(got[:4], wantSel) {
		t.Fatalf("packed selector = %x, want %x", got[:4], wantSel)
	}
	if [4]byte(got[:4]) != claimMinerSelector {
		t.Fatalf("claimMinerSelector constant = %x disagrees with packed %x", claimMinerSelector[:], got[:4])
	}

	// (b) hand-built encoding: 5 head words (dynamic proof as offset 0xa0),
	// then the proof tail (length + nodes).
	alice, err := hex.DecodeString(aliceHex)
	if err != nil {
		t.Fatal(err)
	}
	var hand []byte
	hand = append(hand, wantSel...)
	hand = append(hand, word(7)...)    // e
	hand = append(hand, word(3)...)    // noId
	hand = append(hand, alice...)      // coldkey (bytes32)
	hand = append(hand, word(1234)...) // shareBps
	hand = append(hand, word(0xa0)...) // offset of proof = 5*32
	hand = append(hand, word(2)...)    // proof length
	hand = append(hand, bytes.Repeat([]byte{0x11}, 32)...)
	hand = append(hand, bytes.Repeat([]byte{0x22}, 32)...)
	if !bytes.Equal(got, hand) {
		t.Fatalf("packed calldata != hand-built encoding\n got: 0x%x\nwant: 0x%x", got, hand)
	}

	// Pinned golden hex.
	if gotHex := "0x" + hex.EncodeToString(got); gotHex != goldenClaimCalldata {
		t.Fatalf("golden mismatch\n got: %s\nwant: %s", gotHex, goldenClaimCalldata)
	}
}

// TestParseClaimCalldataRoundTrip checks raw-calldata mode accepts the golden
// calldata byte-for-byte (0x-optional) and decodes the display intent.
func TestParseClaimCalldataRoundTrip(t *testing.T) {
	want := goldenIntent(t)
	for _, in := range []string{goldenClaimCalldata, strings.TrimPrefix(goldenClaimCalldata, "0x")} {
		data, intent, err := parseClaimCalldata(in)
		if err != nil {
			t.Fatalf("parseClaimCalldata(%.16s...): %v", in, err)
		}
		if gotHex := "0x" + hex.EncodeToString(data); gotHex != goldenClaimCalldata {
			t.Fatalf("bytes not passed through exactly: %s", gotHex)
		}
		if intent.E.Cmp(want.E) != 0 || intent.NoID.Cmp(want.NoID) != 0 ||
			intent.ShareBps.Cmp(want.ShareBps) != 0 || intent.Coldkey != want.Coldkey {
			t.Fatalf("decoded intent = %+v, want %+v", intent, want)
		}
		if len(intent.Proof) != 2 || intent.Proof[0] != want.Proof[0] || intent.Proof[1] != want.Proof[1] {
			t.Fatalf("decoded proof = %x, want %x", intent.Proof, want.Proof)
		}
	}
}

// TestParseClaimCalldataRejects checks the raw-calldata validation: wrong
// selector (a real deposit packing), odd-length hex, junk hex, and
// truncated input are all refused.
func TestParseClaimCalldataRejects(t *testing.T) {
	deposit, err := stSubnet.TryPackDeposit(big.NewInt(1), big.NewInt(2))
	if err != nil {
		t.Fatalf("TryPackDeposit: %v", err)
	}

	cases := []struct {
		name, in string
	}{
		{"wrong selector", "0x" + hex.EncodeToString(deposit)},
		{"odd hex", goldenClaimCalldata[:len(goldenClaimCalldata)-1]},
		{"junk hex", "0x4c2079zz"},
		{"short", "0x4c20"},
		{"empty", ""},
		{"truncated args", goldenClaimCalldata[:20]},
	}
	for _, tc := range cases {
		_, _, err := parseClaimCalldata(tc.in)
		if err == nil {
			t.Errorf("%s: parseClaimCalldata accepted %q", tc.name, tc.in)
		}
	}

	// Error text spot checks (selector + odd length are the load-bearing ones).
	if _, _, err := parseClaimCalldata("0x" + hex.EncodeToString(deposit)); err == nil ||
		!strings.Contains(err.Error(), "selector") || !strings.Contains(err.Error(), "4c207962") {
		t.Errorf("wrong-selector error unhelpful: %v", err)
	}
	if _, _, err := parseClaimCalldata("0x4c2079621"); err == nil || !strings.Contains(err.Error(), "odd-length") {
		t.Errorf("odd-length error unhelpful: %v", err)
	}
}

// TestMinerClaimedByKey pins the status-command dedup key to the contract's
// keccak256(abi.encode(noId, coldkey)), derived independently here.
func TestMinerClaimedByKey(t *testing.T) {
	in := goldenIntent(t)
	var pre []byte
	pre = append(pre, word(3)...) // noId as a 32-byte word
	pre = append(pre, in.Coldkey[:]...)
	want := keccak256x(pre)
	got := minerClaimedByKey(in.NoID, in.Coldkey)
	if !bytes.Equal(got[:], want) {
		t.Fatalf("minerClaimedByKey = %x, want %x", got, want)
	}
}
