package commitmentoracle

import (
	"context"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
)

// The SN25 miner case of support items 7906/7916, read from public finalized
// mainnet state in factury support/plans/sn25-commitment-mirror-runbook.md:
// Commitments.CommitmentOf(25, hotkey) holds one Sha256 field at block
// 9,259,873, whose Substrate hash is goldenBlockHash. These are public chain
// values, not keys.
const (
	goldenHotkeyHex     = "ca2b48b25720aef399910675153afb697370e8983a45856fb220bb8a83a08d1d"
	goldenCommitmentHex = "f8cfc29663a79e23c8a41fe6a2ca21b7bc7f8a0463526607140ce2aa5766defb"
	goldenBlock         = uint64(9259873)
	goldenBlockHashHex  = "f692c0b5a15e4b9c6d232c4ed5dc516c9985a91a98d0f334f20974061f87c80b"
	// deposit u64 0, block u32 little-endian (0x008d4b61), vec length 1,
	// Sha256 variant 0x83, hash.
	goldenRegistrationHex = "0000000000000000" + "614b8d00" + "04" + "83" + goldenCommitmentHex
	goldenLastHex         = "614b8d00"
	// The finalized head of the runbook's reads.
	goldenFinalized = uint64(9286066)
)

func mustHex(t *testing.T, value string) []byte {
	decoded, err := hex.DecodeString(strings.TrimPrefix(value, "0x"))
	if err != nil {
		t.Fatal(err)
	}
	return decoded
}

func mustHash(t *testing.T, value string) [32]byte {
	decoded := mustHex(t, value)
	if len(decoded) != 32 {
		t.Fatalf("%s is not 32 bytes", value)
	}
	return [32]byte(decoded)
}

// A synthetic finalized head hash; the runbook did not record the head's hash.
func goldenHeadHash() [32]byte {
	return [32]byte(crypto.Keccak256([]byte("synthetic finalized head")))
}

// A fake native chain holding the golden commitment at its finalized head.
func goldenNative(t *testing.T) *fakeNative {
	native := newFakeNative(nativeHead{number: goldenFinalized, hash: goldenHeadHash()})
	native.block(goldenBlock, mustHash(t, goldenBlockHashHex))
	native.put(goldenHeadHash(), mustHash(t, goldenHotkeyHex), mustHex(t, goldenRegistrationHex), mustHex(t, goldenLastHex))
	native.roles[mustHash(t, goldenHotkeyHex)] = neuronRole{registered: true, uid: goldenUid}
	native.bodies[mustHash(t, goldenBlockHashHex)] = [][]byte{mustHex(t, goldenTimestampExtrinsicHex), mustHex(t, goldenFleetExtrinsicHex)}
	return native
}

// The exact mirrorCommitment calldata for the golden case, encoded by hand
// rather than through the generated binding.
func goldenCalldata(t *testing.T) []byte {
	selector := crypto.Keccak256([]byte("mirrorCommitment(bytes32,bytes32,uint64,bytes32)"))[:4]
	block := strings.Repeat("0", 64-6) + "8d4b61"
	return mustHex(t, hex.EncodeToString(selector)+goldenHotkeyHex+goldenCommitmentHex+block+goldenBlockHashHex)
}

func TestParseFleetCommitmentFormats(t *testing.T) {
	hotkey := [32]byte{7}
	hash := strings.Repeat("ab", 32)
	cases := []struct {
		name   string
		raw    string
		hotkey [32]byte
		ok     bool
		block  uint64
	}{
		{name: "golden one-field sha256", raw: goldenRegistrationHex, hotkey: mustHash(t, goldenHotkeyHex), ok: true, block: goldenBlock},
		{name: "one-field sha256", raw: "0500000000000000" + "0a000000" + "04" + "83" + hash, hotkey: hotkey, ok: true, block: 10},
		{name: "two fields ending in sha256", raw: "0000000000000000" + "0a000000" + "08" + "83" + hash + "83" + hash, hotkey: hotkey},
		{name: "raw data field", raw: "0000000000000000" + "0a000000" + "04" + "a1" + hash, hotkey: hotkey},
		{name: "old multi-byte raw entry", raw: "0000000000000000" + "40420f00" + "04" + "0b" + "68656c6c6f776f726c64", hotkey: hotkey},
		{name: "zero hash", raw: "0000000000000000" + "0a000000" + "04" + "83" + strings.Repeat("00", 32), hotkey: hotkey},
		{name: "zero block", raw: "0000000000000000" + "00000000" + "04" + "83" + hash, hotkey: hotkey},
		{name: "trailing byte", raw: "0000000000000000" + "0a000000" + "04" + "83" + hash + "00", hotkey: hotkey},
		{name: "truncated", raw: "0000000000000000" + "0a000000" + "04" + "83" + hash[:62], hotkey: hotkey},
		{name: "empty field list", raw: "0000000000000000" + "0a000000" + "00", hotkey: hotkey},
		{name: "zero hotkey", raw: "0000000000000000" + "0a000000" + "04" + "83" + hash, hotkey: [32]byte{}},
	}
	for _, c := range cases {
		commitment, err := parseFleetCommitment(rawCommitment{hotkey: c.hotkey, raw: mustHex(t, c.raw)})
		if c.ok != (err == nil) {
			t.Errorf("%s: accepted %t, want %t (%v)", c.name, err == nil, c.ok, err)
			continue
		}
		if c.ok && (commitment.commitmentBlock != c.block || commitment.hotkey != c.hotkey) {
			t.Errorf("%s: block %d hotkey 0x%x", c.name, commitment.commitmentBlock, commitment.hotkey)
		}
	}
	commitment, _ := parseFleetCommitment(rawCommitment{hotkey: mustHash(t, goldenHotkeyHex), raw: mustHex(t, goldenRegistrationHex)})
	if commitment.commitmentHash != mustHash(t, goldenCommitmentHex) {
		t.Fatalf("golden commitment hash 0x%x", commitment.commitmentHash)
	}
}

func TestMirrorDecision(t *testing.T) {
	commitment := fleetCommitment{hotkey: [32]byte{1}, commitmentHash: [32]byte{2}, commitmentBlock: 1000}
	maxAge := uint64(100)
	cases := []struct {
		name     string
		head     uint64
		mirrored mirroredRecord
		want     skipReason
	}{
		{name: "never mirrored", head: 1050, want: skipNone},
		{name: "older record mirrored", head: 1050, mirrored: mirroredRecord{commitmentHash: [32]byte{9}, finalizedBlockHash: [32]byte{9}, finalizedBlock: 900}, want: skipNone},
		{name: "same record mirrored", head: 1050, mirrored: mirroredRecord{commitmentHash: [32]byte{2}, finalizedBlockHash: [32]byte{3}, finalizedBlock: 1000}, want: skipMirrored},
		{name: "same block other hash", head: 1050, mirrored: mirroredRecord{commitmentHash: [32]byte{8}, finalizedBlockHash: [32]byte{3}, finalizedBlock: 1000}, want: skipConflict},
		{name: "newer record mirrored", head: 1050, mirrored: mirroredRecord{commitmentHash: [32]byte{8}, finalizedBlockHash: [32]byte{3}, finalizedBlock: 1001}, want: skipNewerMirrored},
		{name: "exactly max age", head: 1100, want: skipNone},
		{name: "past max age", head: 1101, want: skipExpired},
		{name: "mirrored and past max age", head: 1200, mirrored: mirroredRecord{commitmentHash: [32]byte{2}, finalizedBlockHash: [32]byte{3}, finalizedBlock: 1000}, want: skipMirrored},
		{name: "block above finalized head", head: 999, want: skipInconsistent},
	}
	for _, c := range cases {
		if got := mirrorDecision(commitment, c.head, maxAge, c.mirrored); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

// The golden miner case: the arguments derived from the finalized fixture are
// exactly the runbook's mirrorCommitment arguments.
func TestDeriveMirrorArgsGolden(t *testing.T) {
	native := goldenNative(t)
	entries, err := native.commitments(context.Background(), 25, goldenHeadHash())
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries %d: %v", len(entries), err)
	}
	commitment, err := parseFleetCommitment(entries[0])
	if err != nil {
		t.Fatal(err)
	}
	if reason := mirrorDecision(commitment, goldenFinalized, 100800, mirroredRecord{}); reason != skipNone {
		t.Fatalf("decision %q", reason)
	}
	blockHash, _ := native.blockHash(context.Background(), commitment.commitmentBlock)
	headerNumber, _ := native.headerNumber(context.Background(), blockHash)
	last, _ := native.lastCommitment(context.Background(), 25, commitment.hotkey, goldenHeadHash())
	args, err := deriveMirrorArgs(commitment, goldenFinalized, last, blockHash, headerNumber)
	if err != nil {
		t.Fatal(err)
	}
	want := mirrorArgs{hotkey: mustHash(t, goldenHotkeyHex), commitmentHash: mustHash(t, goldenCommitmentHex), finalizedBlock: goldenBlock, finalizedBlockHash: mustHash(t, goldenBlockHashHex)}
	if args != want {
		t.Fatalf("args %s, want %s", args, want)
	}
	if got := mirrorCalldata(args); hex.EncodeToString(got) != hex.EncodeToString(goldenCalldata(t)) {
		t.Fatalf("calldata 0x%x, want 0x%x", got, goldenCalldata(t))
	}
	// Freshness deadline of the runbook: block 9,360,673.
	if reason := mirrorDecision(commitment, 9360673, 100800, mirroredRecord{}); reason != skipNone {
		t.Fatalf("at the deadline: %q", reason)
	}
	if reason := mirrorDecision(commitment, 9360674, 100800, mirroredRecord{}); reason != skipExpired {
		t.Fatalf("after the deadline: %q", reason)
	}
}

func TestDeriveMirrorArgsRejectsInconsistentEvidence(t *testing.T) {
	commitment := fleetCommitment{hotkey: [32]byte{1}, commitmentHash: [32]byte{2}, commitmentBlock: 1000}
	last := []byte{0xe8, 0x03, 0x00, 0x00}
	cases := []struct {
		name         string
		head         uint64
		last         []byte
		blockHash    [32]byte
		headerNumber uint64
	}{
		{name: "LastCommitment names another block", head: 1050, last: []byte{0xe9, 0x03, 0x00, 0x00}, blockHash: [32]byte{3}, headerNumber: 1000},
		{name: "LastCommitment missing", head: 1050, last: nil, blockHash: [32]byte{3}, headerNumber: 1000},
		{name: "LastCommitment wrong width", head: 1050, last: []byte{0xe8, 0x03}, blockHash: [32]byte{3}, headerNumber: 1000},
		{name: "no canonical hash", head: 1050, last: last, headerNumber: 1000},
		{name: "header of another height", head: 1050, last: last, blockHash: [32]byte{3}, headerNumber: 1001},
		{name: "block above the finalized head", head: 999, last: last, blockHash: [32]byte{3}, headerNumber: 1000},
	}
	for _, c := range cases {
		if _, err := deriveMirrorArgs(commitment, c.head, c.last, c.blockHash, c.headerNumber); err == nil {
			t.Errorf("%s: accepted", c.name)
		}
	}
	if _, err := deriveMirrorArgs(commitment, 1050, last, [32]byte{3}, 1000); err != nil {
		t.Fatal(err)
	}
}
