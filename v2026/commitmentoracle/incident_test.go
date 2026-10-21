package commitmentoracle

// The 2026-10-15 incident: a manual mirror of the SN25 validator's own source
// commitment (UID 1, 5CyQFykVpfa9xgZVGgBsiNUpjbo1EihBTSkPvDC1FZbL1wxG, tx
// 0x4bb7941d1f344609b60f1ab0edb6548b18a9ee53cef1b5c7f7556f3886d222b5) halted
// its steering, because a validator source commitment had the same one-field
// Sha256 storage format as a fleet commitment. These fixtures are public
// finalized mainnet values read on 2026-10-15 at native finalized 9,286,811
// (lite and archive agreed). format_test.go uses them. The oracle reads no
// block body, so the extrinsic bytes the f8a7c15e tests pinned are not kept.

import (
	"context"
	"testing"
	"time"
)

const (
	// The SN25 validator, UID 1, with a validator permit. Its source
	// commitment is one Sha256 field, written at block 9,286,328 by a
	// Utility.batch_all of Commitments.set_commitment and commit_timelocked.
	validatorHotkeyHex     = "2830c416368bc539694600e9ed2cb276764452814c591d6740cc737eb673c67b"
	validatorCommitmentHex = "5877e91464305ea2f2c1299ff1bce3f2117d9e24f30a7a50a1bc9edd3ae79030"
	validatorBlock         = uint64(9286328)
	validatorBlockHashHex  = "74fc6cf9810ad4de80e9978d8930d4ff3ee3d3135187a1d7a908b6ba3644bfc0"
	// Block 9,286,328 encoded as a little-endian u32.
	validatorRegistrationHex = "0000000000000000" + "b8b28d00" + "04" + "83" + validatorCommitmentHex
	validatorLastHex         = "b8b28d00"
	// The native finalized head of the incident reads.
	incidentFinalized = uint64(9286811)
)

func newIncidentHarness(t *testing.T, native *fakeNative, dryRun bool) *testHarness {
	settings := testSettings(t.TempDir())
	settings.DryRun = dryRun
	key := testOracleKey(t)
	address := keyAddress(key)
	evm := newFakeEvm(t, settings.Coordinator, address, incidentFinalized)
	harness := newTestHarness(t, native, evm, settings, key, address, &testClock{now: time.Unix(1760500000, 0)}, &testLog{})
	harness.oracle.sleep = func(ctx context.Context, duration time.Duration) error {
		harness.clock.now = harness.clock.now.Add(duration)
		harness.evm.mine()
		harness.evm.finalize()
		return nil
	}
	return harness
}
