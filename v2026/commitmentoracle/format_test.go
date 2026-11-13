package commitmentoracle

// Root-cause tests for the SN25 validator mirror incident (2026-10-15): a
// one-field Sha256 commitment does not say whose it is, so the oracle must
// identify a fleet commitment positively by its own format, one Raw40 field of
// "urfleet1" and the hash, and mirror nothing else. The decision must not
// depend on a validator permit, the shape of the writing extrinsic, or any
// read that can fail: the oracle reads only commitment storage and headers,
// which assertFormatReadsOnly checks against the fake's log of every read.

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"strconv"
	"strings"
	"testing"
)

const (
	// "urfleet1" in ASCII.
	fleetTagHex = "7572666c65657431"
	// The exact mainnet bytes of the 7906/7916 miner's commitment: one Sha256
	// field (variant 0x83), no tag. A legacy fleet commitment.
	legacyGoldenRegistrationHex = "0000000000000000" + "614b8d00" + "04" + "83" + goldenCommitmentHex
)

// A registration at block with the given CommitmentInfo hex.
func formatRegistration(block uint64, infoHex string) (string, string) {
	last := hex.EncodeToString(binary.LittleEndian.AppendUint32(nil, uint32(block)))
	return "0000000000000000" + last + infoHex, last
}

func taggedInfoHex(hash string) string {
	return "04" + "29" + fleetTagHex + hash
}

// The incident's finalized head with no commitments yet.
func formatChain() *fakeNative {
	native := newFakeNative(nativeHead{number: incidentFinalized, hash: goldenHeadHash()})
	return native
}

// Puts hotkey's registration, written at block (hash blockHash), at the head.
func formatPut(t *testing.T, native *fakeNative, hotkey [32]byte, block uint64, blockHash [32]byte, infoHex string) {
	registration, last := formatRegistration(block, infoHex)
	native.block(block, blockHash)
	native.put(goldenHeadHash(), hotkey, mustHex(t, registration), mustHex(t, last))
}

// The oracle's whole native input: commitment storage, block hashes and
// headers. No role, permit, registration or block body is ever read.
func assertFormatReadsOnly(t *testing.T, native *fakeNative) {
	t.Helper()
	allowed := map[string]bool{"finalizedHead": true, "commitments": true, "commitment": true, "lastCommitment": true, "blockHash": true, "headerNumber": true}
	for kind := range native.reads {
		if !allowed[kind] {
			t.Fatalf("the oracle read %s", kind)
		}
	}
}

func assertLegacy(t *testing.T, harness *testHarness, count int) {
	t.Helper()
	if harness.oracle.metrics.legacy != count || !strings.Contains(harness.oracle.metrics.render(harness.clock.now), metricsPrefix+"legacy_sha256_commitments "+strconv.Itoa(count)+"\n") {
		t.Fatalf("legacy %d, want %d", harness.oracle.metrics.legacy, count)
	}
}

func assertNothingMirrored(t *testing.T, harness *testHarness) {
	t.Helper()
	if len(harness.evm.broadcasts) != 0 || len(harness.evm.mined) != 0 {
		t.Fatalf("broadcast %d, mined %d", len(harness.evm.broadcasts), len(harness.evm.mined))
	}
	if harness.oracle.metrics.pending != 0 || harness.oracle.metrics.fleet != 0 {
		t.Fatalf("pending %d fleet %d", harness.oracle.metrics.pending, harness.oracle.metrics.fleet)
	}
}

func assertMirrored(t *testing.T, harness *testHarness, hotkey [32]byte, hash string, block uint64, blockHash [32]byte) {
	t.Helper()
	want := mirroredRecord{commitmentHash: mustHash(t, hash), finalizedBlockHash: blockHash, finalizedBlock: block}
	if record := harness.evm.recordAt(hotkey, harness.evm.latest); record != want {
		t.Fatalf("hotkey 0x%x record %+v, want %+v", hotkey, record, want)
	}
}

// The incident: the validator's source commitment (one Sha256 field, which
// it wrote by Utility.batch_all with its CRv4 commit) is never mirrored. No
// permit or extrinsic is an input, so this holds with the permit held or
// lapsed.
func TestFormatNeverMirrorsIncidentValidatorSourceCommitment(t *testing.T) {
	native := formatChain()
	validator := mustHash(t, validatorHotkeyHex)
	native.block(validatorBlock, mustHash(t, validatorBlockHashHex))
	native.put(goldenHeadHash(), validator, mustHex(t, validatorRegistrationHex), mustHex(t, validatorLastHex))
	harness := newIncidentHarness(t, native, false)
	if err := harness.oracle.cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertNothingMirrored(t, harness)
	assertLegacy(t, harness, 1)
	assertFormatReadsOnly(t, native)
}

// Any one-field Sha256 commitment, at any block, is legacy and never
// mirrored: the format alone keeps it out, where the f8a7c15e heuristic
// mirrored one written standalone from a hotkey without a permit.
func TestFormatNeverMirrorsStandaloneSha256Commitment(t *testing.T) {
	native := formatChain()
	validator := mustHash(t, validatorHotkeyHex)
	formatPut(t, native, validator, validatorBlock, mustHash(t, validatorBlockHashHex), "04"+"83"+validatorCommitmentHex)
	// A legacy registration with block 0 is still counted as legacy.
	native.put(goldenHeadHash(), [32]byte{0x66}, mustHex(t, "0000000000000000"+"00000000"+"04"+"83"+validatorCommitmentHex), mustHex(t, "00000000"))
	harness := newIncidentHarness(t, native, false)
	if err := harness.oracle.cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertNothingMirrored(t, harness)
	assertLegacy(t, harness, 2)
	if record := harness.evm.recordAt(validator, harness.evm.latest); record.finalizedBlock != 0 {
		t.Fatalf("validator record %+v", record)
	}
}

// A legacy untagged fleet commitment, the 7906/7916 miner's real mainnet
// bytes, is not mirrored: the oracle accepts no legacy format. The miner
// republishes.
func TestFormatNeverMirrorsLegacyFleetCommitment(t *testing.T) {
	native := formatChain()
	miner := mustHash(t, goldenHotkeyHex)
	native.block(goldenBlock, mustHash(t, goldenBlockHashHex))
	native.put(goldenHeadHash(), miner, mustHex(t, legacyGoldenRegistrationHex), mustHex(t, goldenLastHex))
	harness := newIncidentHarness(t, native, false)
	if err := harness.oracle.cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertNothingMirrored(t, harness)
	assertLegacy(t, harness, 1)
}

// A tagged fleet commitment is mirrored whichever hotkey wrote it, including
// the validator's own hotkey (UID 1, which holds a permit): the oracle reads
// commitment storage and headers only, so neither a permit nor how the write
// was batched or proxied, nor a failing role read, can keep a tagged fleet
// out.
func TestFormatMirrorsTaggedFleetsReadingOnlyCommitmentState(t *testing.T) {
	native := formatChain()
	type fleet struct {
		hotkey    [32]byte
		hash      string
		block     uint64
		blockHash [32]byte
	}
	fleets := []fleet{
		{hotkey: mustHash(t, validatorHotkeyHex), hash: strings.Repeat("a1", 32), block: 9286001, blockHash: [32]byte{0xb1}},
		{hotkey: [32]byte{0x62}, hash: strings.Repeat("b2", 32), block: 9286002, blockHash: [32]byte{0xb2}},
		{hotkey: [32]byte{0x63}, hash: strings.Repeat("c3", 32), block: 9286003, blockHash: [32]byte{0xb3}},
	}
	for _, f := range fleets {
		formatPut(t, native, f.hotkey, f.block, f.blockHash, taggedInfoHex(f.hash))
	}
	harness := newIncidentHarness(t, native, false)
	if err := harness.oracle.cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, f := range fleets {
		assertMirrored(t, harness, f.hotkey, f.hash, f.block, f.blockHash)
	}
	if len(harness.evm.mined) != 3 || harness.oracle.metrics.fleet != 3 {
		t.Fatalf("mined %d fleet %d", len(harness.evm.mined), harness.oracle.metrics.fleet)
	}
	assertLegacy(t, harness, 0)
	assertFormatReadsOnly(t, native)
	if native.reads["commitments"] == 0 || native.reads["lastCommitment"] != 3 {
		t.Fatalf("reads %v", native.reads)
	}
}

// Malformed tags, variants and lengths are never mirrored; the one valid
// tagged commitment among them is.
func TestFormatRejectsMalformedTagsAndLengths(t *testing.T) {
	native := formatChain()
	hash := strings.Repeat("e5", 32)
	malformed := map[string]string{
		"other tag":             "04" + "29" + "7572666c65657432" + hash,
		"upper-case tag":        "04" + "29" + "5552464c45455431" + hash,
		"tag without hash":      "04" + "09" + fleetTagHex,
		"raw32 hash, no tag":    "04" + "21" + hash,
		"raw39 short hash":      "04" + "28" + fleetTagHex + hash[:62],
		"raw41 extra byte":      "04" + "2a" + fleetTagHex + hash + "00",
		"raw40 trailing byte":   "04" + "29" + fleetTagHex + hash + "00",
		"raw40 truncated":       "04" + "29" + fleetTagHex + hash[:62],
		"two tagged fields":     "08" + "29" + fleetTagHex + hash + "29" + fleetTagHex + hash,
		"sha256 then tagged":    "08" + "83" + hash + "29" + fleetTagHex + hash,
		"zero hash":             "04" + "29" + fleetTagHex + strings.Repeat("00", 32),
		"tag after hash":        "04" + "29" + hash + fleetTagHex,
		"one-field sha256 only": "04" + "83" + hash,
	}
	index := byte(0)
	for _, info := range malformed {
		index += 1
		hotkey := [32]byte{0x70, index}
		formatPut(t, native, hotkey, 9286100+uint64(index), [32]byte{0xc0, index}, info)
	}
	valid := [32]byte{0x7f}
	formatPut(t, native, valid, 9286200, [32]byte{0xcf}, taggedInfoHex(hash))
	harness := newIncidentHarness(t, native, false)
	if err := harness.oracle.cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertMirrored(t, harness, valid, hash, 9286200, [32]byte{0xcf})
	if len(harness.evm.mined) != 1 || harness.oracle.metrics.fleet != 1 {
		t.Fatalf("mined %d fleet %d", len(harness.evm.mined), harness.oracle.metrics.fleet)
	}
	// A registration whose u64/u32 prefix is cut short is not fleet format.
	for name, raw := range map[string]string{
		"no deposit": "614b8d00" + taggedInfoHex(hash),
		"short":      "00000000000000" + "614b8d00" + taggedInfoHex(hash),
		"long":       "000000000000000000" + "614b8d00" + taggedInfoHex(hash),
	} {
		if _, err := parseFleetCommitment(rawCommitment{hotkey: valid, raw: mustHex(t, raw)}); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
}
