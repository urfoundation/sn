package main

import (
	"encoding/hex"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/urnetwork/sn/ss58"
)

// TestParseColdkeyAlice checks the ss58 <-> hex32 equivalence on the Alice
// dev vector (d43593c7... <-> 5GrwvaEF...) and hex passthrough forms.
func TestParseColdkeyAlice(t *testing.T) {
	want, err := hex.DecodeString(aliceHex)
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range []string{
		aliceSS58,              // ss58, prefix 42
		"0x" + aliceHex,        // 0x hex32
		aliceHex,               // bare hex32
		" " + aliceSS58 + "\n", // surrounding whitespace
	} {
		got, err := parseColdkey(in)
		if err != nil {
			t.Errorf("parseColdkey(%q): %v", in, err)
			continue
		}
		if !strings.EqualFold(hex.EncodeToString(got[:]), hex.EncodeToString(want)) {
			t.Errorf("parseColdkey(%q) = %x, want %s", in, got, aliceHex)
		}
	}

	// renderColdkey round-trips to the same ss58 address.
	ck, err := parseColdkey(aliceSS58)
	if err != nil {
		t.Fatal(err)
	}
	if r := renderColdkey(ck); !strings.Contains(r, aliceSS58) || !strings.Contains(r, aliceHex) {
		t.Errorf("renderColdkey = %q, want it to contain %s and %s", r, aliceSS58, aliceHex)
	}
}

// TestParseColdkeyRejects: wrong ss58 network prefix, bad hex lengths, the
// zero coldkey, and garbage must all fail.
func TestParseColdkeyRejects(t *testing.T) {
	var alice [32]byte
	b, err := hex.DecodeString(aliceHex)
	if err != nil {
		t.Fatal(err)
	}
	copy(alice[:], b)
	polkadotAlice, err := ss58.Encode(alice, 0) // prefix 0, not Bittensor's 42
	if err != nil {
		t.Fatal(err)
	}

	for _, in := range []string{
		polkadotAlice,                   // valid ss58, wrong prefix
		"0x1234",                        // short hex
		"0x" + aliceHex + "ff",          // long hex
		"0x" + strings.Repeat("00", 32), // zero coldkey
		strings.Repeat("00", 32),        // zero coldkey, bare
		"not-a-key",
		"",
	} {
		if _, err := parseColdkey(in); err == nil {
			t.Errorf("parseColdkey(%q) unexpectedly succeeded", in)
		}
	}
}

func TestParseProof(t *testing.T) {
	empty, err := parseProof("")
	if err != nil || len(empty) != 0 {
		t.Fatalf("parseProof(\"\") = %v, %v; want empty proof, nil", empty, err)
	}

	two, err := parseProof("0x" + strings.Repeat("11", 32) + ", " + strings.Repeat("22", 32))
	if err != nil {
		t.Fatalf("parseProof(two nodes): %v", err)
	}
	if len(two) != 2 || two[0][0] != 0x11 || two[1][0] != 0x22 {
		t.Fatalf("parseProof(two nodes) = %x", two)
	}

	for _, in := range []string{
		"0x1111",                       // short node
		strings.Repeat("11", 32) + ",", // trailing empty node
		"zz",                           // junk
	} {
		if _, err := parseProof(in); err == nil {
			t.Errorf("parseProof(%q) unexpectedly succeeded", in)
		}
	}
}

// Hardhat/Anvil dev account #0 — a well-known key/address golden pair.
const (
	testKeyHex  = "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
	testKeyAddr = "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266"
)

// TestLoadKeyFile checks key parsing (bare, 0x-prefixed, whitespace) and the
// derived EVM address against the golden pair.
func TestLoadKeyFile(t *testing.T) {
	dir := t.TempDir()
	want := common.HexToAddress(testKeyAddr)

	for name, content := range map[string]string{
		"bare":     testKeyHex,
		"prefixed": "0x" + testKeyHex + "\n",
		"padded":   "  " + testKeyHex + " \n\n",
	} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		key, err := loadKeyFile(path)
		if err != nil {
			t.Errorf("%s: loadKeyFile: %v", name, err)
			continue
		}
		if got := crypto.PubkeyToAddress(key.PublicKey); got != want {
			t.Errorf("%s: derived address %s, want %s", name, got, want)
		}
	}

	for name, content := range map[string]string{
		"short":  testKeyHex[:62],
		"junk":   strings.Repeat("zz", 32),
		"padded": testKeyHex + "ff",
	} {
		path := filepath.Join(dir, "bad-"+name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadKeyFile(path); err == nil {
			t.Errorf("bad-%s: loadKeyFile unexpectedly succeeded", name)
		}
	}

	if _, err := loadKeyFile(filepath.Join(dir, "does-not-exist")); err == nil {
		t.Error("missing key file unexpectedly succeeded")
	}
}

func TestFormatAlpha(t *testing.T) {
	cases := []struct {
		rao  int64
		want string
	}{
		{0, "0.000000000 α (0 rao)"},
		{5, "0.000000005 α (5 rao)"},
		{raoPerAlpha, "1.000000000 α (1000000000 rao)"},
		{12_345_678_901, "12.345678901 α (12345678901 rao)"},
	}
	for _, tc := range cases {
		if got := formatAlpha(big.NewInt(tc.rao)); got != tc.want {
			t.Errorf("formatAlpha(%d) = %q, want %q", tc.rao, got, tc.want)
		}
	}
}

func TestParseBig(t *testing.T) {
	v, err := parseBig("--epoch", "42")
	if err != nil || v.Int64() != 42 {
		t.Fatalf("parseBig(42) = %v, %v", v, err)
	}
	v, err = parseBig("--epoch", "0x2a")
	if err != nil || v.Int64() != 42 {
		t.Fatalf("parseBig(0x2a) = %v, %v", v, err)
	}
	for _, in := range []string{"-1", "", "12.5", "abc"} {
		if _, err := parseBig("--epoch", in); err == nil {
			t.Errorf("parseBig(%q) unexpectedly succeeded", in)
		}
	}
}
