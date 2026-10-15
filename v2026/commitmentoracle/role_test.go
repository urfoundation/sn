package commitmentoracle

// The 2026-10-15 incident: a manual mirror of the SN25 validator's own source
// commitment (UID 1, 5CyQFykVpfa9xgZVGgBsiNUpjbo1EihBTSkPvDC1FZbL1wxG, tx
// 0x4bb7941d1f344609b60f1ab0edb6548b18a9ee53cef1b5c7f7556f3886d222b5) halted
// its steering, because a validator source commitment has the same one-field
// Sha256 storage format as a fleet commitment. These fixtures are public
// finalized mainnet values read on 2026-10-15 at native finalized 9,286,811
// (lite and archive agreed): storage, the ValidatorPermit census and the
// exact extrinsic bytes of each commitment block.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

const (
	// The 7906/7916 miner's UID; it holds no validator permit.
	goldenUid = uint16(145)
	// Block 9,259,873 extrinsic 11: the miner's standalone signed
	// Commitments.set_commitment (call 0x1200, netuid 25, one Sha256 field).
	goldenFleetExtrinsicHex = "39028400ca2b48b25720aef399910675153afb697370e8983a45856fb220bb8a83a08d1d01921e05452556d68b3bdc3921265daf4d94a378c74938adcfb323a7e75793d842cd816a50c399e94eb6952c9eecf43a7c47de948f92c5647a35dc34b2651f5f82e605040000120019000483f8cfc29663a79e23c8a41fe6a2ca21b7bc7f8a0463526607140ce2aa5766defb"
	// Block 9,259,873 extrinsic 0, the bare v5 timestamp inherent.
	goldenTimestampExtrinsicHex = "280502000bc004e62aa101"

	// The SN25 validator, UID 1, with a validator permit.
	validatorHotkeyHex     = "2830c416368bc539694600e9ed2cb276764452814c591d6740cc737eb673c67b"
	validatorUid           = uint16(1)
	validatorCommitmentHex = "5877e91464305ea2f2c1299ff1bce3f2117d9e24f30a7a50a1bc9edd3ae79030"
	validatorBlock         = uint64(9286328)
	validatorBlockHashHex  = "74fc6cf9810ad4de80e9978d8930d4ff3ee3d3135187a1d7a908b6ba3644bfc0"
	// Block 9,286,328 encoded as a little-endian u32.
	validatorRegistrationHex = "0000000000000000" + "b8b28d00" + "04" + "83" + validatorCommitmentHex
	validatorLastHex         = "b8b28d00"
	// Block 9,286,328 extrinsic 0, the bare v5 timestamp inherent.
	validatorTimestampExtrinsicHex = "280502000bc041d23da101"
	// The native finalized head of the incident reads.
	incidentFinalized = uint64(9286811)
	// Block 9,286,328 extrinsic 12, the validator source write: Utility.batch_all
	// (0x0b02, two calls) of Commitments.set_commitment and SubtensorModule
	// commit_timelocked (0x0771), signed by the validator hotkey.
	validatorSourceExtrinsicHex = "" +
		"310784002830c416368bc539694600e9ed2cb276764452814c591d6740cc737eb673c67b01485bfd7b4de4455d0651502afe050529cd6a33669348958f2140d6" +
		"1caf87f27700167023980bb0b5c98de08299de71467e41c8c7211680d2a63a075fe1c81c8600210400000b02081200190004835877e91464305ea2f2c1299ff1" +
		"bce3f2117d9e24f30a7a50a1bc9edd3ae7903007711900ad04b3222556261c88849d20d29f6912c862e837afa26645be15aded38c4265743a59bf00cf6d8a4c4" +
		"f0a04460418fc942e20e0bda567d42db40d155ab6565e0482b65e2d8b0a9db4bdf943cee3947b06d190d661b6681fdc92b7adb7f5c640687ae20000000000000" +
		"00a412460b03366cfb0b4b66b25f355b569a8496fd594cdab993e490a7cf623f4720000000000000006b0f9b7c22dd308e4fcb7c9a9e58ab67f6e9811432427b" +
		"c66da355ff0c0f1c7d6300000000000000470000000000000055fd74177a944a4f441c3e4caea3cb576e9b11683f9bde7ca73ba90d5444f8367e2c312b6b304b" +
		"2597d412ee892408b48e74b3de181e554ff88261facc063c2492f235eb141efa0c0000000000000094acaafc10430f1628bc6f0608000000000000004145535f" +
		"47434d5f79c1f801000000000400"
)

// The golden miner plus the validator's source commitment at the incident's
// finalized head, with the real roles and block bodies.
func incidentNative(t *testing.T) *fakeNative {
	native := goldenNative(t)
	native.finalized.number = incidentFinalized
	native.block(incidentFinalized, goldenHeadHash())
	native.block(validatorBlock, mustHash(t, validatorBlockHashHex))
	validator := mustHash(t, validatorHotkeyHex)
	native.put(goldenHeadHash(), validator, mustHex(t, validatorRegistrationHex), mustHex(t, validatorLastHex))
	native.roles[validator] = neuronRole{registered: true, uid: validatorUid, validatorPermit: true}
	native.bodies[mustHash(t, validatorBlockHashHex)] = [][]byte{mustHex(t, validatorTimestampExtrinsicHex), mustHex(t, validatorSourceExtrinsicHex)}
	return native
}

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

func TestParseSignedExtrinsicGolden(t *testing.T) {
	fleet, ok, err := parseSignedExtrinsic(mustHex(t, goldenFleetExtrinsicHex))
	if err != nil || !ok {
		t.Fatalf("fleet extrinsic: ok %v err %v", ok, err)
	}
	if fleet.signer != mustHash(t, goldenHotkeyHex) || string(fleet.call) != string(fakeFleetCall(25, mustHash(t, goldenCommitmentHex))) {
		t.Fatalf("fleet extrinsic signer 0x%x call 0x%x", fleet.signer, fleet.call)
	}
	source, ok, err := parseSignedExtrinsic(mustHex(t, validatorSourceExtrinsicHex))
	if err != nil || !ok {
		t.Fatalf("source extrinsic: ok %v err %v", ok, err)
	}
	if source.signer != mustHash(t, validatorHotkeyHex) || !strings.HasPrefix(string(source.call), string([]byte{0x0b, 0x02, 0x08, 0x12, 0x00})) {
		t.Fatalf("source extrinsic signer 0x%x call 0x%x", source.signer, source.call[:8])
	}
	for _, inherent := range []string{goldenTimestampExtrinsicHex, validatorTimestampExtrinsicHex} {
		if _, ok, err := parseSignedExtrinsic(mustHex(t, inherent)); ok || err != nil {
			t.Fatalf("inherent %s: ok %v err %v", inherent, ok, err)
		}
	}
	// A truncated or mis-prefixed extrinsic is an error, not unsigned.
	raw := mustHex(t, goldenFleetExtrinsicHex)
	if _, _, err := parseSignedExtrinsic(raw[:len(raw)-1]); err == nil {
		t.Fatal("parsed a truncated extrinsic")
	}
}

func TestFleetEvidence(t *testing.T) {
	miner := mustHash(t, goldenHotkeyHex)
	validator := mustHash(t, validatorHotkeyHex)
	minerCall := fakeFleetCall(25, mustHash(t, goldenCommitmentHex))
	validatorCall := fakeFleetCall(25, mustHash(t, validatorCommitmentHex))
	minerBlock := [][]byte{mustHex(t, goldenTimestampExtrinsicHex), mustHex(t, goldenFleetExtrinsicHex)}
	validatorBlock := [][]byte{mustHex(t, validatorTimestampExtrinsicHex), mustHex(t, validatorSourceExtrinsicHex)}
	if err := fleetEvidence(minerBlock, miner, minerCall); err != nil {
		t.Fatalf("miner: %v", err)
	}
	// The golden incident: the validator's source write is a batch, not a
	// standalone fleet set_commitment, although its storage is fleet format.
	if err := fleetEvidence(validatorBlock, validator, validatorCall); err == nil {
		t.Fatal("the validator source batch counted as fleet evidence")
	}
	cases := map[string]struct {
		block  [][]byte
		hotkey [32]byte
		call   []byte
	}{
		"another hash":             {minerBlock, miner, validatorCall},
		"not signed by the hotkey": {minerBlock, validator, minerCall},
		"two by the hotkey":        {append(append([][]byte{}, minerBlock...), synthSignedExtrinsic(miner, []byte{0x07, 0x71, 0x00})), miner, minerCall},
		"unparseable naming it":    {append(append([][]byte{}, minerBlock...), append([]byte{0x08, 0x99, 0x00}, miner[:]...)), miner, minerCall},
		"empty body":               {nil, miner, minerCall},
	}
	for name, test := range cases {
		if err := fleetEvidence(test.block, test.hotkey, test.call); err == nil {
			t.Fatalf("%s: counted as fleet evidence", name)
		}
	}
	// An unparseable extrinsic that cannot be the hotkey's is ignored.
	other := append(append([][]byte{}, minerBlock...), []byte{0x08, 0x99, 0x00})
	if err := fleetEvidence(other, miner, minerCall); err != nil {
		t.Fatalf("unrelated unparseable extrinsic: %v", err)
	}
}

func TestDecodeNeuronRole(t *testing.T) {
	validator := mustHash(t, validatorHotkeyHex)
	// 256 UIDs; the incident's permit census.
	permits := make([]byte, 256)
	for _, uid := range []int{1, 55, 73, 110, 114, 140, 169, 170, 178, 204, 212, 218, 219, 250} {
		permits[uid] = 1
	}
	permitsRaw := append([]byte{0x01, 0x04}, permits...)
	subnetworkN := []byte{0x00, 0x01}
	keys := map[uint16][32]byte{validatorUid: validator, goldenUid: mustHash(t, goldenHotkeyHex)}
	keyOfUid := func(uid uint16) ([]byte, error) {
		key, ok := keys[uid]
		if !ok {
			return nil, nil
		}
		return key[:], nil
	}
	role, err := decodeNeuronRole(validator, subnetworkN, []byte{0x01, 0x00}, keyOfUid, permitsRaw)
	if err != nil || role != (neuronRole{registered: true, uid: 1, validatorPermit: true}) {
		t.Fatalf("validator role %+v err %v", role, err)
	}
	role, err = decodeNeuronRole(mustHash(t, goldenHotkeyHex), subnetworkN, []byte{0x91, 0x00}, keyOfUid, permitsRaw)
	if err != nil || role != (neuronRole{registered: true, uid: 145}) {
		t.Fatalf("miner role %+v err %v", role, err)
	}
	role, err = decodeNeuronRole([32]byte{0x77}, subnetworkN, nil, keyOfUid, permitsRaw)
	if err != nil || role.registered {
		t.Fatalf("unregistered role %+v err %v", role, err)
	}
	bad := map[string]func() (neuronRole, error){
		"short census": func() (neuronRole, error) {
			return decodeNeuronRole(validator, []byte{1}, []byte{1, 0}, keyOfUid, permitsRaw)
		},
		"permits too short": func() (neuronRole, error) {
			return decodeNeuronRole(validator, subnetworkN, []byte{1, 0}, keyOfUid, permitsRaw[:200])
		},
		"permits absent": func() (neuronRole, error) {
			return decodeNeuronRole(validator, subnetworkN, []byte{1, 0}, keyOfUid, nil)
		},
		"census mismatch": func() (neuronRole, error) {
			return decodeNeuronRole(validator, []byte{0xff, 0}, []byte{1, 0}, keyOfUid, permitsRaw)
		},
		"permit not a bool": func() (neuronRole, error) {
			return decodeNeuronRole(validator, subnetworkN, []byte{1, 0}, keyOfUid, append([]byte{0x01, 0x04, 2}, permits[1:]...))
		},
		"uid outside census": func() (neuronRole, error) {
			return decodeNeuronRole(validator, []byte{1, 0}, []byte{1, 0}, keyOfUid, []byte{0x04, 0})
		},
		"keys mismatch": func() (neuronRole, error) {
			return decodeNeuronRole(validator, subnetworkN, []byte{0x91, 0}, keyOfUid, permitsRaw)
		},
		"keys read error": func() (neuronRole, error) {
			return decodeNeuronRole(validator, subnetworkN, []byte{1, 0}, func(uint16) ([]byte, error) { return nil, errors.New("rpc") }, permitsRaw)
		},
	}
	for name, decode := range bad {
		if _, err := decode(); err == nil {
			t.Fatalf("%s: decoded", name)
		}
	}
}

// Golden: the validator hotkey's one-field Sha256 commitment is never
// mirrored, while the miner's fleet commitment in the same cycle still is.
func TestCycleNeverMirrorsValidatorSourceCommitment(t *testing.T) {
	harness := newIncidentHarness(t, incidentNative(t), false)
	if err := harness.oracle.cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	validator := mustHash(t, validatorHotkeyHex)
	for _, mined := range harness.evm.mined {
		if strings.Contains(string(mined.transaction.Data()), string(validator[:])) {
			t.Fatalf("mirrored the validator hotkey: 0x%x", mined.transaction.Data())
		}
	}
	if record := harness.evm.recordAt(validator, harness.evm.latest); record.finalizedBlock != 0 {
		t.Fatalf("validator record %+v", record)
	}
	if len(harness.evm.mined) != 1 || string(harness.evm.mined[0].transaction.Data()) != string(goldenCalldata(t)) {
		t.Fatalf("mined %d transactions", len(harness.evm.mined))
	}
	if harness.oracle.metrics.fleet != 2 || harness.oracle.metrics.excluded != 1 || !harness.log.contains("validator") {
		t.Fatalf("fleet %d excluded %d", harness.oracle.metrics.fleet, harness.oracle.metrics.excluded)
	}
	if !strings.Contains(harness.oracle.metrics.render(harness.clock.now), metricsPrefix+"fleet_format_excluded 1") {
		t.Fatal("fleet_format_excluded is not exported")
	}
}

// Even if the validator's permit were missing (an off-permit validator, or a
// role read that misses it), its source batch is not fleet evidence.
func TestCycleNeverMirrorsSourceCommitmentWithoutPermit(t *testing.T) {
	native := incidentNative(t)
	native.roles[mustHash(t, validatorHotkeyHex)] = neuronRole{registered: true, uid: validatorUid}
	harness := newIncidentHarness(t, native, true)
	if err := harness.oracle.cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if harness.oracle.metrics.pending != 1 || harness.oracle.metrics.excluded != 1 || !harness.log.contains(string(skipNoFleetEvidence)) {
		t.Fatalf("pending %d excluded %d", harness.oracle.metrics.pending, harness.oracle.metrics.excluded)
	}
	if harness.log.contains("would send mirrorCommitment(hotkey=0x"+validatorHotkeyHex) || !harness.log.contains("would send mirrorCommitment(hotkey=0x"+goldenHotkeyHex) {
		t.Fatal("would mirror the validator, or not the miner")
	}
}

// A hotkey without a UID is never mirrored.
func TestCycleSkipsUnregisteredHotkey(t *testing.T) {
	native := incidentNative(t)
	delete(native.roles, mustHash(t, goldenHotkeyHex))
	native.roles[mustHash(t, validatorHotkeyHex)] = neuronRole{}
	harness := newIncidentHarness(t, native, false)
	if err := harness.oracle.cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(harness.evm.broadcasts) != 0 || harness.oracle.metrics.excluded != 2 {
		t.Fatalf("broadcasts %d excluded %d", len(harness.evm.broadcasts), harness.oracle.metrics.excluded)
	}
}

// Fail closed: a role that cannot be read, on the primary or any secondary
// endpoint, mirrors nothing in that cycle, not even the miner's.
func TestCycleRoleReadFailureMirrorsNothing(t *testing.T) {
	harness := newIncidentHarness(t, incidentNative(t), false)
	harness.native.roleErr = errors.New("ValidatorPermit read timed out")
	if err := harness.oracle.cycle(context.Background()); err == nil || !strings.Contains(err.Error(), "role") {
		t.Fatalf("cycle: %v", err)
	}
	if len(harness.evm.broadcasts) != 0 || harness.oracle.metrics.lastSuccess != 0 {
		t.Fatalf("broadcasts %d", len(harness.evm.broadcasts))
	}

	secondary := incidentNative(t)
	secondary.roleErr = errors.New("ValidatorPermit read timed out")
	harness = newIncidentHarness(t, incidentNative(t), false)
	harness.oracle.connections.secondaries = []nativeReader{secondary}
	if err := harness.oracle.cycle(context.Background()); err == nil || !strings.Contains(err.Error(), "endpoint 2 role") {
		t.Fatalf("cycle with a failing secondary: %v", err)
	}
	if len(harness.evm.broadcasts) != 0 {
		t.Fatalf("broadcasts %d", len(harness.evm.broadcasts))
	}
}

// Any endpoint reporting a permit excludes the hotkey; endpoints that
// disagree on the registration fail the cycle.
func TestCycleRoleUsesMostRestrictiveEndpoint(t *testing.T) {
	secondary := incidentNative(t)
	secondary.roles[mustHash(t, goldenHotkeyHex)] = neuronRole{registered: true, uid: goldenUid, validatorPermit: true}
	harness := newIncidentHarness(t, incidentNative(t), false)
	harness.oracle.connections.secondaries = []nativeReader{secondary}
	if err := harness.oracle.cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(harness.evm.broadcasts) != 0 || harness.oracle.metrics.excluded != 2 {
		t.Fatalf("broadcasts %d excluded %d", len(harness.evm.broadcasts), harness.oracle.metrics.excluded)
	}

	secondary = incidentNative(t)
	secondary.roles[mustHash(t, goldenHotkeyHex)] = neuronRole{registered: true, uid: 7}
	harness = newIncidentHarness(t, incidentNative(t), false)
	harness.oracle.connections.secondaries = []nativeReader{secondary}
	if err := harness.oracle.cycle(context.Background()); err == nil || !strings.Contains(err.Error(), "disagree") {
		t.Fatalf("cycle: %v", err)
	}
	if len(harness.evm.broadcasts) != 0 {
		t.Fatalf("broadcasts %d", len(harness.evm.broadcasts))
	}
}

// A commitment block body that cannot be read mirrors nothing for that
// hotkey.
func TestCycleMissingBlockBodyMirrorsNothing(t *testing.T) {
	native := goldenNative(t)
	delete(native.bodies, mustHash(t, goldenBlockHashHex))
	harness := newIncidentHarness(t, native, false)
	harness.native.finalized.number = goldenFinalized
	if err := harness.oracle.cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(harness.evm.broadcasts) != 0 || harness.oracle.metrics.pending != 0 {
		t.Fatalf("broadcasts %d pending %d", len(harness.evm.broadcasts), harness.oracle.metrics.pending)
	}
}
