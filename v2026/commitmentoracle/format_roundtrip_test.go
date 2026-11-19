package commitmentoracle

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/crv4"
)

// Writer/reader round trip: the CommitmentInfo the miner writes
// (crv4.EncodeFleetCommitmentInfo, the info of `provider fleet publish`'s
// set_commitment) stored in a registration is what the oracle parses and
// mirrors, with the hash extracted unchanged.
func TestFormatWriterReaderRoundTrip(t *testing.T) {
	hash := sha256.Sum256([]byte("fleet manifest commitment"))
	info, err := crv4.EncodeFleetCommitmentInfo(hash)
	if err != nil {
		t.Fatal(err)
	}
	block := uint64(9286321)
	registration := append(binary.LittleEndian.AppendUint32(make([]byte, 8), uint32(block)), info...)
	if len(registration) != fleetRegistrationBytes {
		t.Fatalf("registration %d bytes, want %d", len(registration), fleetRegistrationBytes)
	}
	hotkey := [32]byte{0x5a}
	commitment, err := parseFleetCommitment(rawCommitment{hotkey: hotkey, raw: registration})
	if err != nil || commitment.commitmentHash != hash || commitment.commitmentBlock != block {
		t.Fatalf("parsed %+v: %v", commitment, err)
	}

	native := formatChain()
	blockHash := [32]byte{0xd1}
	native.block(block, blockHash)
	native.put(goldenHeadHash(), hotkey, registration, binary.LittleEndian.AppendUint32(nil, uint32(block)))
	harness := newIncidentHarness(t, native, false)
	if err := harness.oracle.cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if record := harness.evm.recordAt(hotkey, harness.evm.latest); record != (mirroredRecord{commitmentHash: hash, finalizedBlockHash: blockHash, finalizedBlock: block}) {
		t.Fatalf("record %+v", record)
	}

	// The validator's source writer output is never read as a fleet.
	source, err := crv4.EncodeSourceCommitmentInfo(hash)
	if err != nil {
		t.Fatal(err)
	}
	sourceRegistration := append(binary.LittleEndian.AppendUint32(make([]byte, 8), uint32(block)), source...)
	if _, err := parseFleetCommitment(rawCommitment{hotkey: hotkey, raw: sourceRegistration}); err == nil {
		t.Fatal("parsed a source commitment as a fleet")
	}
}

// One-field Sha256 commitments (validator sources and untagged fleets) are
// counted in legacy_sha256_commitments, not in fleet_commitments.
func TestFormatCountsLegacySha256Commitments(t *testing.T) {
	native := formatChain()
	native.block(validatorBlock, mustHash(t, validatorBlockHashHex))
	native.put(goldenHeadHash(), mustHash(t, validatorHotkeyHex), mustHex(t, validatorRegistrationHex), mustHex(t, validatorLastHex))
	native.block(goldenBlock, mustHash(t, goldenBlockHashHex))
	native.put(goldenHeadHash(), mustHash(t, goldenHotkeyHex), mustHex(t, legacyGoldenRegistrationHex), mustHex(t, goldenLastHex))
	formatPut(t, native, [32]byte{0x65}, 9286005, [32]byte{0xb5}, taggedInfoHex(strings.Repeat("f6", 32)))
	harness := newIncidentHarness(t, native, true)
	if err := harness.oracle.cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	metrics := harness.oracle.metrics
	if metrics.commitments != 3 || metrics.fleet != 1 || metrics.legacy != 2 || metrics.pending != 1 {
		t.Fatalf("commitments %d fleet %d legacy %d pending %d", metrics.commitments, metrics.fleet, metrics.legacy, metrics.pending)
	}
	if !strings.Contains(metrics.render(harness.clock.now), metricsPrefix+"legacy_sha256_commitments 2") {
		t.Fatal("legacy_sha256_commitments is not exported")
	}
}
