package main

import (
	"bytes"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/urfoundation/sn/ss58"
)

// TestFormatAlpha pins the rao <-> alpha rendering (1 alpha = 1e9 rao).
func TestFormatAlpha(t *testing.T) {
	cases := []struct {
		rao  string
		want string
	}{
		{"0", "0 rao (0.000000000 α)"},
		{"1", "1 rao (0.000000001 α)"},
		{"999999999", "999999999 rao (0.999999999 α)"},
		{"1000000000", "1000000000 rao (1.000000000 α)"},
		{"1234567890", "1234567890 rao (1.234567890 α)"},
		{"1234567890123", "1234567890123 rao (1234.567890123 α)"},
		{"-1500000000", "-1500000000 rao (-1.500000000 α)"},
	}
	for _, tc := range cases {
		rao, ok := new(big.Int).SetString(tc.rao, 10)
		if !ok {
			t.Fatalf("bad test rao %q", tc.rao)
		}
		if got := formatAlpha(rao); got != tc.want {
			t.Errorf("formatAlpha(%s) = %q, want %q", tc.rao, got, tc.want)
		}
	}
}

func TestParseUint256(t *testing.T) {
	if v, err := parseUint256("x", "12345"); err != nil || v.Cmp(big.NewInt(12345)) != 0 {
		t.Errorf("parseUint256(12345) = %v, %v", v, err)
	}
	if v, err := parseUint256("x", "0x10"); err != nil || v.Cmp(big.NewInt(16)) != 0 {
		t.Errorf("parseUint256(0x10) = %v, %v", v, err)
	}
	for _, bad := range []string{"", "-1", "1.5", "abc", "0x", "0xzz", "1e9"} {
		if _, err := parseUint256("x", bad); err == nil {
			t.Errorf("parseUint256(%q) succeeded", bad)
		}
	}
	// 2^256 does not fit
	tooBig := new(big.Int).Lsh(big.NewInt(1), 256)
	if _, err := parseUint256("x", tooBig.String()); err == nil {
		t.Error("parseUint256(2^256) succeeded")
	}
	// 2^256 - 1 fits
	maxU256 := new(big.Int).Sub(tooBig, big.NewInt(1))
	if _, err := parseUint256("x", maxU256.String()); err != nil {
		t.Errorf("parseUint256(2^256-1) failed: %v", err)
	}
}

func TestParseHex32(t *testing.T) {
	want := [32]byte{0xaa, 0xbb}
	in := "aabb" + strings.Repeat("00", 30)
	for _, s := range []string{in, "0x" + in} {
		got, err := parseHex32("x", s)
		if err != nil || got != want {
			t.Errorf("parseHex32(%q) = %x, %v", s, got, err)
		}
	}
	for _, bad := range []string{"", "aabb", strings.Repeat("00", 31), strings.Repeat("00", 33), strings.Repeat("zz", 32)} {
		if _, err := parseHex32("x", bad); err == nil {
			t.Errorf("parseHex32(%q) succeeded", bad)
		}
	}
}

func TestParseProof(t *testing.T) {
	empty, err := parseProof("x", "")
	if err != nil || len(empty) != 0 {
		t.Errorf("parseProof(\"\") = %v, %v", empty, err)
	}
	a := strings.Repeat("aa", 32)
	b := strings.Repeat("bb", 32)
	proof, err := parseProof("x", "0x"+a+" , "+b)
	if err != nil || len(proof) != 2 || proof[0][0] != 0xaa || proof[1][0] != 0xbb {
		t.Errorf("parseProof(two nodes) = %v, %v", proof, err)
	}
	if _, err := parseProof("x", a+",short"); err == nil {
		t.Error("parseProof accepted a short node")
	}
}

func TestParseAccount32(t *testing.T) {
	var key [32]byte
	for i := range key {
		key[i] = 0x11
	}
	hexIn := strings.Repeat("11", 32)
	got, err := parseAccount32("x", "0x"+hexIn)
	if err != nil || got != key {
		t.Errorf("parseAccount32(hex) = %x, %v", got, err)
	}
	address, err := ss58.Encode(key, ss58.BittensorPrefix)
	if err != nil {
		t.Fatalf("ss58.Encode: %v", err)
	}
	got, err = parseAccount32("x", address)
	if err != nil || got != key {
		t.Errorf("parseAccount32(ss58) = %x, %v", got, err)
	}
	// a prefix-0 (Polkadot) address must be rejected
	polkadot, err := ss58.Encode(key, 0)
	if err != nil {
		t.Fatalf("ss58.Encode prefix 0: %v", err)
	}
	if _, err := parseAccount32("x", polkadot); err == nil {
		t.Error("parseAccount32 accepted a non-42-prefix ss58 address")
	}
	if _, err := parseAccount32("x", "garbage"); err == nil {
		t.Error("parseAccount32 accepted garbage")
	}
}

// TestEvmAddressReport pins the evm-address output for a fixed H160 against
// values derived through the ss58 package (PLAN.md §3.6 funding helper).
func TestEvmAddressReport(t *testing.T) {
	const h160 = "0x00112233445566778899aAbBcCdDeEfF00112233"
	addr := common.HexToAddress(h160)

	wantPubkey := ss58.EvmMirrorPubkey(addr)
	wantSS58, err := ss58.EvmMirrorAddress(addr, ss58.BittensorPrefix)
	if err != nil {
		t.Fatalf("ss58.EvmMirrorAddress: %v", err)
	}
	// golden literal: pins the ss58 pipeline end to end
	const goldenSS58 = "5GpbPoSiydTo7NeV1LiMzk7CyGoLMrSJTpU97XiUmEUVuUN8"
	if wantSS58 != goldenSS58 {
		t.Fatalf("ss58.EvmMirrorAddress(%s) = %s, want %s", h160, wantSS58, goldenSS58)
	}

	report, err := evmAddressReport(h160)
	if err != nil {
		t.Fatalf("evmAddressReport: %v", err)
	}
	wantPubkeyHex := "0x" + common.Bytes2Hex(wantPubkey[:])
	if !strings.Contains(report, wantPubkeyHex) {
		t.Errorf("report missing mirror pubkey %s:\n%s", wantPubkeyHex, report)
	}
	if !strings.Contains(report, wantSS58) {
		t.Errorf("report missing mirror ss58 %s:\n%s", wantSS58, report)
	}
	if !strings.Contains(report, addr.String()) {
		t.Errorf("report missing h160 %s:\n%s", addr, report)
	}

	if _, err := evmAddressReport("0x1234"); err == nil {
		t.Error("evmAddressReport accepted a short address")
	}
	if _, err := evmAddressReport("nothex"); err == nil {
		t.Error("evmAddressReport accepted garbage")
	}
}

// TestMinerDedupKey checks the hand-rolled keccak256(abi.encode(noId,
// coldkey)) against a go-ethereum abi.Arguments encoding.
func TestMinerDedupKey(t *testing.T) {
	uint256Type, err := abi.NewType("uint256", "", nil)
	if err != nil {
		t.Fatalf("abi.NewType: %v", err)
	}
	bytes32Type, err := abi.NewType("bytes32", "", nil)
	if err != nil {
		t.Fatalf("abi.NewType: %v", err)
	}
	args := abi.Arguments{{Type: uint256Type}, {Type: bytes32Type}}

	noId := big.NewInt(7)
	var coldkey [32]byte
	coldkey[0] = 0xab
	coldkey[31] = 0xcd

	packed, err := args.Pack(noId, coldkey)
	if err != nil {
		t.Fatalf("args.Pack: %v", err)
	}
	want := crypto.Keccak256(packed)
	got := minerDedupKey(noId, coldkey)
	if !bytes.Equal(got[:], want) {
		t.Fatalf("minerDedupKey = %x, want %x", got, want)
	}
}

func TestFormatBlockETA(t *testing.T) {
	if got := formatBlockETA(100, 100); got != "block 100 (now)" {
		t.Errorf("formatBlockETA(100, 100) = %q", got)
	}
	if got := formatBlockETA(100, 90); got != "block 90 (passed, 10 blocks ago)" {
		t.Errorf("formatBlockETA(100, 90) = %q", got)
	}
	if got := formatBlockETA(100, 110); got != "block 110 (in 10 blocks, ~2m0s @12s/block)" {
		t.Errorf("formatBlockETA(100, 110) = %q", got)
	}
}
