package main

import (
	"bytes"
	"encoding/hex"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"

	"github.com/urfoundation/sn/ss58"
	"github.com/urfoundation/sn/stabi"
)

// mustHex decodes a hex string built from concatenated calldata words.
func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	raw, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("bad test hex: %v", err)
	}
	return raw
}

const (
	word1     = "0000000000000000000000000000000000000000000000000000000000000001"
	word2     = "0000000000000000000000000000000000000000000000000000000000000002"
	word10000 = "0000000000000000000000000000000000000000000000000000000000002710"
	wordA0    = "00000000000000000000000000000000000000000000000000000000000000a0"
)

// TestDepositCalldataGolden pins deposit(uint256,uint256): selector
// 0xe2bbb158 plus two hand-encoded uint256 words.
func TestDepositCalldataGolden(t *testing.T) {
	st := stabi.NewSTSubnet()

	want := mustHex(t, "e2bbb158"+word1+word2)
	got := st.PackDeposit(big.NewInt(1), big.NewInt(2))
	if !bytes.Equal(got, want) {
		t.Fatalf("PackDeposit(1, 2) = %x, want %x", got, want)
	}

	// selector cross-check against an independent keccak
	if sel := crypto.Keccak256([]byte("deposit(uint256,uint256)"))[:4]; !bytes.Equal(got[:4], sel) {
		t.Fatalf("deposit selector = %x, keccak says %x", got[:4], sel)
	}

	// flag -> calldata path
	fromFlags, err := depositCalldata(st, "1", "2")
	if err != nil {
		t.Fatalf("depositCalldata: %v", err)
	}
	if !bytes.Equal(fromFlags, want) {
		t.Fatalf("depositCalldata(\"1\", \"2\") = %x, want %x", fromFlags, want)
	}
}

// TestClaimMinerCalldataGolden pins
// claimMiner(uint256,uint256,bytes32,uint256,bytes32[]): selector
// 0x4c207962 plus the hand-derived head/tail encoding for fixed inputs
// (e=1, noId=2, coldkey=0x11..11, shareBps=10000, proof=[0xaa..aa, 0xbb..bb]).
func TestClaimMinerCalldataGolden(t *testing.T) {
	st := stabi.NewSTSubnet()

	coldkeyHex := strings.Repeat("11", 32)
	proofA := strings.Repeat("aa", 32)
	proofB := strings.Repeat("bb", 32)
	want := mustHex(t, "4c207962"+
		word1+ // e = 1
		word2+ // noId = 2
		coldkeyHex+ // coldkey
		word10000+ // shareBps = 10000
		wordA0+ // offset to proof tail = 5 words = 0xa0
		word2+ // proof length = 2
		proofA+
		proofB)

	var coldkey [32]byte
	copy(coldkey[:], mustHex(t, coldkeyHex))
	var nodeA, nodeB [32]byte
	copy(nodeA[:], mustHex(t, proofA))
	copy(nodeB[:], mustHex(t, proofB))

	got := st.PackClaimMiner(big.NewInt(1), big.NewInt(2), coldkey, big.NewInt(10_000), [][32]byte{nodeA, nodeB})
	if !bytes.Equal(got, want) {
		t.Fatalf("PackClaimMiner = %x, want %x", got, want)
	}

	// selector cross-check against an independent keccak
	if sel := crypto.Keccak256([]byte("claimMiner(uint256,uint256,bytes32,uint256,bytes32[])"))[:4]; !bytes.Equal(got[:4], sel) {
		t.Fatalf("claimMiner selector = %x, keccak says %x", got[:4], sel)
	}

	// flag -> calldata path, coldkey as 0x-hex
	fromFlags, err := claimMinerCalldata(st, "1", "2", "0x"+coldkeyHex, "10000", "0x"+proofA+",0x"+proofB)
	if err != nil {
		t.Fatalf("claimMinerCalldata: %v", err)
	}
	if !bytes.Equal(fromFlags, want) {
		t.Fatalf("claimMinerCalldata(hex coldkey) = %x, want %x", fromFlags, want)
	}

	// flag -> calldata path, coldkey as ss58 (prefix 42)
	coldkeySS58, err := ss58.Encode(coldkey, ss58.BittensorPrefix)
	if err != nil {
		t.Fatalf("ss58.Encode: %v", err)
	}
	fromSS58, err := claimMinerCalldata(st, "1", "2", coldkeySS58, "10000", proofA+","+proofB)
	if err != nil {
		t.Fatalf("claimMinerCalldata(ss58): %v", err)
	}
	if !bytes.Equal(fromSS58, want) {
		t.Fatalf("claimMinerCalldata(ss58 coldkey) = %x, want %x", fromSS58, want)
	}

	// shareBps over 10000 must be rejected at parse time
	if _, err := claimMinerCalldata(st, "1", "2", "0x"+coldkeyHex, "10001", ""); err == nil {
		t.Fatal("claimMinerCalldata accepted shareBps > 10000")
	}
}

// TestClaimMinerEmptyProof pins the empty-proof (single-leaf tree) encoding:
// length word 0 and no tail nodes.
func TestClaimMinerEmptyProof(t *testing.T) {
	st := stabi.NewSTSubnet()
	coldkeyHex := strings.Repeat("11", 32)
	want := mustHex(t, "4c207962"+
		word1+
		word2+
		coldkeyHex+
		word10000+
		wordA0+
		strings.Repeat("00", 32)) // proof length = 0
	got, err := claimMinerCalldata(st, "1", "2", coldkeyHex, "10000", "")
	if err != nil {
		t.Fatalf("claimMinerCalldata: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("claimMinerCalldata(empty proof) = %x, want %x", got, want)
	}
}

// TestInitializeCalldataGolden pins initialize(uint16,address,address,
// bytes32,bytes32,uint64,uint64,uint64,uint64,bytes32): selector 0xd7a9b3db
// plus ten hand-encoded static words, with the testnet window-profile
// defaults (300/50/100/150) filled for the omitted flags and guardian/
// selfColdkey zero.
func TestInitializeCalldataGolden(t *testing.T) {
	st := stabi.NewSTSubnet()

	ownerHex := "00112233445566778899aabbccddeeff00112233"
	treasuryHex := strings.Repeat("aa", 32)
	reserveHex := strings.Repeat("bb", 32)
	zeroWord := strings.Repeat("00", 32)
	want := mustHex(t, "d7a9b3db"+
		"000000000000000000000000000000000000000000000000000000000000015e"+ // netuid 350
		"000000000000000000000000"+ownerHex+ // owner
		zeroWord+ // guardian = none
		treasuryHex+ // treasuryHotkey
		reserveHex+ // reserveHotkey
		"000000000000000000000000000000000000000000000000000000000000012c"+ // tEpoch 300
		"0000000000000000000000000000000000000000000000000000000000000032"+ // commitWindow 50
		"0000000000000000000000000000000000000000000000000000000000000064"+ // trailsWindow 100
		"0000000000000000000000000000000000000000000000000000000000000096"+ // finalizeOffset 150
		zeroWord) // selfColdkey = 0 (computed on-chain)

	flags := initializeFlags{
		owner:          "0x" + ownerHex,
		treasuryHotkey: "0x" + treasuryHex,
		reserveHotkey:  "0x" + reserveHex,
	}
	got, err := initializeCalldata(st, 350, flags, false)
	if err != nil {
		t.Fatalf("initializeCalldata: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("initializeCalldata = %x, want %x", got, want)
	}

	// selector cross-check against an independent keccak
	sig := "initialize(uint16,address,address,bytes32,bytes32,uint64,uint64,uint64,uint64,bytes32)"
	if sel := crypto.Keccak256([]byte(sig))[:4]; !bytes.Equal(got[:4], sel) {
		t.Fatalf("initialize selector = %x, keccak says %x", got[:4], sel)
	}

	// the reserve hotkey accepts the same ss58 encoding as the treasury one
	var reserve [32]byte
	copy(reserve[:], mustHex(t, reserveHex))
	reserveSS58, err := ss58.Encode(reserve, ss58.BittensorPrefix)
	if err != nil {
		t.Fatalf("ss58.Encode: %v", err)
	}
	ss58Flags := flags
	ss58Flags.reserveHotkey = reserveSS58
	fromSS58, err := initializeCalldata(st, 350, ss58Flags, false)
	if err != nil {
		t.Fatalf("initializeCalldata(ss58 reserve): %v", err)
	}
	if !bytes.Equal(fromSS58, want) {
		t.Fatalf("initializeCalldata(ss58 reserve) = %x, want %x", fromSS58, want)
	}
}

// TestParseInitializeArgs pins the pre-send validation and the profile
// defaults: mainnet windows, reserve == treasury / zero-key rejection, and
// the commit <= trails <= finalize window-order check.
func TestParseInitializeArgs(t *testing.T) {
	ownerHex := "0x00112233445566778899aabbccddeeff00112233"
	treasuryHex := strings.Repeat("aa", 32)
	reserveHex := strings.Repeat("bb", 32)
	base := initializeFlags{
		owner:          ownerHex,
		treasuryHotkey: treasuryHex,
		reserveHotkey:  reserveHex,
	}

	// mainnet profile defaults (Deploy.s.sol)
	args, err := parseInitializeArgs(base, true)
	if err != nil {
		t.Fatalf("parseInitializeArgs(mainnet): %v", err)
	}
	if args.tEpoch != 50_400 || args.commitWindow != 1_200 ||
		args.trailsWindow != 7_200 || args.finalizeOffset != 14_400 {
		t.Fatalf("mainnet profile = %+v, want 50400/1200/7200/14400", args)
	}

	rejects := []struct {
		name    string
		mutate  func(*initializeFlags)
		wantSub string
	}{
		{
			name:    "reserve == treasury",
			mutate:  func(f *initializeFlags) { f.reserveHotkey = f.treasuryHotkey },
			wantSub: "differ",
		},
		{
			name:    "zero reserve",
			mutate:  func(f *initializeFlags) { f.reserveHotkey = strings.Repeat("00", 32) },
			wantSub: "zero",
		},
		{
			name:    "zero treasury",
			mutate:  func(f *initializeFlags) { f.treasuryHotkey = strings.Repeat("00", 32) },
			wantSub: "zero",
		},
		{
			name:    "zero tEpoch",
			mutate:  func(f *initializeFlags) { f.tEpoch = "0" },
			wantSub: ">= 1",
		},
		{
			name:    "bad window order",
			mutate:  func(f *initializeFlags) { f.commitWindow = "500"; f.trailsWindow = "100" },
			wantSub: "window order",
		},
		{
			name:    "bad owner",
			mutate:  func(f *initializeFlags) { f.owner = "not-an-address" },
			wantSub: "--owner",
		},
	}
	for _, tc := range rejects {
		flags := base
		tc.mutate(&flags)
		_, err := parseInitializeArgs(flags, false)
		if err == nil {
			t.Errorf("%s: parseInitializeArgs accepted %+v", tc.name, flags)
			continue
		}
		if !strings.Contains(err.Error(), tc.wantSub) {
			t.Errorf("%s: error %q does not mention %q", tc.name, err, tc.wantSub)
		}
	}
}

// TestTransferStakeCalldataGolden pins the hand-packed StakingV2
// transferStake(bytes32,bytes32,uint256,uint256,uint256) call used by
// `deposit --push`: selector 0x17ce5f62 plus five static words.
func TestTransferStakeCalldataGolden(t *testing.T) {
	destHex := strings.Repeat("cc", 32)
	hotkeyHex := strings.Repeat("dd", 32)
	want := mustHex(t, "17ce5f62"+
		destHex+
		hotkeyHex+
		"000000000000000000000000000000000000000000000000000000000000015e"+ // netuid 350
		"000000000000000000000000000000000000000000000000000000000000015e"+
		"0000000000000000000000000000000000000000000000000000000000000005") // amount 5 rao

	var dest, hotkey [32]byte
	copy(dest[:], mustHex(t, destHex))
	copy(hotkey[:], mustHex(t, hotkeyHex))
	got, err := packTransferStake(dest, hotkey, big.NewInt(350), big.NewInt(350), big.NewInt(5))
	if err != nil {
		t.Fatalf("packTransferStake: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("packTransferStake = %x, want %x", got, want)
	}

	// selector cross-check against an independent keccak
	if sel := crypto.Keccak256([]byte(transferStakeSignature))[:4]; !bytes.Equal(got[:4], sel) {
		t.Fatalf("transferStake selector = %x, keccak says %x", got[:4], sel)
	}
}
