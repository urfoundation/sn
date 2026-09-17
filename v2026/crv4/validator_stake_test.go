// Stake controls use the actual historical identity reader and SDK metadata/
// SCALE codec; scripted RPC responses never replace the eligibility decision.
package crv4

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	gsrpcgeth "github.com/centrifuge/go-substrate-rpc-client/v4/gethrpc"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
)

// All state is call-local; tests complete a read before changing its responses.
type validatorStakeTestFixture struct {
	identity     *validatorIdentityTestFixture
	fields       map[int][]byte
	runtimeRaw   json.RawMessage
	runtimeCalls int
}

// Independent SDK encoding avoids using the production compact decoder or
// argument encoder to manufacture its own expected representation.
func validatorStakeTestCompact(t *testing.T, value uint64) []byte {
	t.Helper()
	data, err := codec.Encode(types.NewUCompactFromUInt(value))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// The fixed layout is the reviewed runtime454 SelectiveMetagraph. Only the
// requested fields are present; callers may alter fields to test refusal.
func validatorStakeTestMetagraph(t *testing.T, netuid uint16, fields map[int][]byte) []byte {
	t.Helper()
	data := append([]byte{1}, validatorStakeTestCompact(t, uint64(netuid))...)
	for index := 1; index <= 76; index++ {
		value, present := fields[index]
		if !present {
			data = append(data, 0)
			continue
		}
		data = append(data, 1)
		data = append(data, value...)
	}
	return data
}

// Extends only this fixture's real metadata with a plain threshold and owner
// map. The original identity fixture and21 roots remain unchanged.
func newValidatorStakeTestFixture(t *testing.T) *validatorStakeTestFixture {
	t.Helper()
	identity := newValidatorIdentityTestFixture(t)
	identity.metadata.AsMetadataV14.Pallets[0].Storage.Items = append(identity.metadata.AsMetadataV14.Pallets[0].Storage.Items,
		types.StorageEntryMetadataV14{
			Name: "StakeThreshold", Modifier: types.StorageFunctionModifierV0{IsDefault: true},
			Type: types.StorageEntryTypeV14{IsPlainType: true},
		},
		types.StorageEntryMetadataV14{
			Name: "SubnetOwnerHotkey", Modifier: types.StorageFunctionModifierV0{IsDefault: true},
			Type: types.StorageEntryTypeV14{IsMap: true, AsMap: types.MapTypeV14{Hashers: []types.StorageHasherV10{{IsIdentity: true}}}},
		},
	)
	identity.publishMetadata(t)
	for _, entry := range []struct {
		name  string
		args  [][]byte
		value json.RawMessage
	}{
		{name: "StakeThreshold", value: validatorIdentityTestHex(binary.LittleEndian.AppendUint64(nil, 100))},
		{name: "SubnetOwnerHotkey", args: [][]byte{binary.LittleEndian.AppendUint16(nil, identity.query.Netuid)}, value: json.RawMessage("null")},
	} {
		key, err := types.CreateStorageKey(identity.metadata, PalletName, entry.name, entry.args...)
		if err != nil {
			t.Fatal(err)
		}
		identity.keyNames[key.Hex()] = entry.name
		identity.storage[entry.name] = entry.value
	}
	hotkeys := validatorStakeTestCompact(t, 3)
	for _, key := range [][32]byte{{31}, identity.hotkey, {33}} {
		hotkeys = append(hotkeys, key[:]...)
	}
	stakes := validatorStakeTestCompact(t, 3)
	for _, stake := range []uint64{0, 150, math.MaxInt64} {
		stakes = append(stakes, validatorStakeTestCompact(t, stake)...)
	}
	fixture := &validatorStakeTestFixture{identity: identity, fields: map[int][]byte{
		30: validatorStakeTestCompact(t, 3), 52: hotkeys,
		57: []byte{12, 0, 1, 0}, 69: stakes,
	}}
	fixture.publish(t)
	identity.hook = func(ctx context.Context, result any, method string, args ...any) (bool, error) {
		if method != "state_call" {
			return false, nil
		}
		fixture.runtimeCalls++
		expected := []any{"SubnetInfoRuntimeApi_get_selective_metagraph", "0x09021400001e00340039004500", identity.query.BlockHash.Hex()}
		if ctx != identity.ctx || !reflect.DeepEqual(args, expected) {
			return true, fmt.Errorf("stake runtime call context/args differ: %v", args)
		}
		target, ok := result.(*json.RawMessage)
		if !ok {
			return true, fmt.Errorf("unexpected stake runtime result %T", result)
		}
		*target = fixture.runtimeRaw
		return true, nil
	}
	return fixture
}

// Freezes the current synthetic field bytes into one actual RPC hex response.
func (self *validatorStakeTestFixture) publish(t *testing.T) {
	t.Helper()
	self.runtimeRaw = validatorIdentityTestHex(validatorStakeTestMetagraph(t, self.identity.query.Netuid, self.fields))
}

// Calls the complete public reader rather than a decoder-only shortcut.
func (self *validatorStakeTestFixture) read() (ValidatorStakeObservation, error) {
	return ReadValidatorStakeAtContext(self.identity.ctx, self.identity.chain, self.identity.query, self.identity.allowed...)
}

// Adds a distinct registered/unregistered owner without conflating its coldkey.
func (self *validatorStakeTestFixture) setOwner(t *testing.T, hotkey [32]byte, uid *uint16) {
	t.Helper()
	self.identity.storage["SubnetOwnerHotkey"] = validatorIdentityTestHex(hotkey[:])
	if hotkey == self.identity.hotkey {
		return
	}
	key, err := types.CreateStorageKey(self.identity.metadata, PalletName, "Uids", binary.LittleEndian.AppendUint16(nil, self.identity.query.Netuid), hotkey[:])
	if err != nil {
		t.Fatal(err)
	}
	self.identity.keyNames[key.Hex()] = "OwnerUID"
	self.identity.storage["OwnerUID"] = json.RawMessage("null")
	if uid != nil {
		self.identity.storage["OwnerUID"] = validatorIdentityTestHex(binary.LittleEndian.AppendUint16(nil, *uid))
	}
}

// Raw alpha differs deliberately from calculated stake: the latter must come
// from the real runtime API, not a local raw-alpha shortcut or validator list.
func TestRuntimeArtifactMetadataValidatorStakeReadsCalculatedStake(t *testing.T) {
	t.Parallel()
	fixture := newValidatorStakeTestFixture(t)
	meta, runtime := fixture.identity.chain.Meta, fixture.identity.chain.Runtime
	observed, err := fixture.read()
	if err != nil {
		t.Fatal(err)
	}
	if observed.TotalStakeRao != 150 || observed.StakeThresholdRao != 100 || observed.Identity.StakeAlphaRao != fixture.identity.stake ||
		observed.SubnetOwnerPresent || !observed.MeetsNonSelfStakeAndPermit() || fixture.runtimeCalls != 1 {
		t.Fatalf("unexpected stake observation: %+v, runtime calls=%d", observed, fixture.runtimeCalls)
	}
	if fixture.identity.chain.Meta != meta || fixture.identity.chain.Runtime != runtime {
		t.Fatal("stake read rebound dial-time metadata/runtime")
	}
}

// Actual submission accepts equality; the API's Validators list uses > and
// would incorrectly reject this registered permitted validator.
func TestRuntimeArtifactMetadataValidatorStakeAcceptsThresholdEquality(t *testing.T) {
	t.Parallel()
	fixture := newValidatorStakeTestFixture(t)
	fixture.identity.storage["StakeThreshold"] = validatorIdentityTestHex(binary.LittleEndian.AppendUint64(nil, 150))
	observed, err := fixture.read()
	if err != nil || !observed.MeetsNonSelfStakeAndPermit() {
		t.Fatalf("threshold equality refused: %+v, %v", observed, err)
	}
}

// Large raw alpha cannot overcome insufficient inherited weighted stake.
func TestRuntimeArtifactMetadataValidatorStakeRejectsBelowThreshold(t *testing.T) {
	t.Parallel()
	fixture := newValidatorStakeTestFixture(t)
	fixture.identity.storage["StakeThreshold"] = validatorIdentityTestHex(binary.LittleEndian.AppendUint64(nil, 151))
	observed, err := fixture.read()
	if err != nil || observed.MeetsNonSelfStakeAndPermit() {
		t.Fatalf("below-threshold verdict is wrong: %+v, %v", observed, err)
	}
}

// Non-self validation needs a permit even when the stake check passes.
func TestRuntimeArtifactMetadataValidatorStakeRejectsMissingPermit(t *testing.T) {
	t.Parallel()
	fixture := newValidatorStakeTestFixture(t)
	fixture.identity.storage["ValidatorPermit"] = validatorIdentityTestHex([]byte{12, 0, 0, 0})
	fixture.fields[57] = []byte{12, 0, 0, 0}
	fixture.publish(t)
	observed, err := fixture.read()
	if err != nil || observed.MeetsNonSelfStakeAndPermit() {
		t.Fatalf("unpermitted validator accepted: %+v, %v", observed, err)
	}
}

// The registered subnet-owner hotkey bypasses both prerequisites, not merely
// the permit. This does not grant the same privilege to its owning coldkey.
func TestRuntimeArtifactMetadataValidatorStakeAcceptsRegisteredSubnetOwner(t *testing.T) {
	t.Parallel()
	fixture := newValidatorStakeTestFixture(t)
	fixture.setOwner(t, fixture.identity.hotkey, nil)
	fixture.identity.storage["StakeThreshold"] = validatorIdentityTestHex(binary.LittleEndian.AppendUint64(nil, math.MaxUint64))
	fixture.identity.storage["ValidatorPermit"] = validatorIdentityTestHex([]byte{12, 0, 0, 0})
	fixture.fields[57] = []byte{12, 0, 0, 0}
	fixture.publish(t)
	observed, err := fixture.read()
	if err != nil || !observed.MeetsNonSelfStakeAndPermit() || !observed.SubnetOwnerRegistered || observed.SubnetOwnerUID != 1 {
		t.Fatalf("registered subnet owner refused: %+v, %v", observed, err)
	}
}

// Coldkey ownership and another registered owner UID are not validator powers.
func TestRuntimeArtifactMetadataValidatorStakeDoesNotUseCandidateColdkeyAsOwner(t *testing.T) {
	t.Parallel()
	fixture := newValidatorStakeTestFixture(t)
	ownerUID := uint16(0)
	fixture.setOwner(t, fixture.identity.coldkey, &ownerUID)
	fixture.identity.storage["StakeThreshold"] = validatorIdentityTestHex(binary.LittleEndian.AppendUint64(nil, 151))
	observed, err := fixture.read()
	if err != nil || observed.MeetsNonSelfStakeAndPermit() || !observed.SubnetOwnerRegistered {
		t.Fatalf("coldkey/other-UID owner bypassed stake: %+v, %v", observed, err)
	}
}

// An owner hotkey without a UID gives no native owner exception.
func TestRuntimeArtifactMetadataValidatorStakeUnregisteredOwnerDoesNotBypass(t *testing.T) {
	t.Parallel()
	fixture := newValidatorStakeTestFixture(t)
	fixture.setOwner(t, [32]byte{44}, nil)
	fixture.identity.storage["StakeThreshold"] = validatorIdentityTestHex(binary.LittleEndian.AppendUint64(nil, 151))
	observed, err := fixture.read()
	if err != nil || observed.MeetsNonSelfStakeAndPermit() || !observed.SubnetOwnerPresent || observed.SubnetOwnerRegistered {
		t.Fatalf("unregistered owner bypassed stake: %+v, %v", observed, err)
	}
}

// Only the two exact runtime defaults are admitted: missing owner means no
// exception, while a missing StakeThreshold value means threshold zero.
func TestRuntimeArtifactMetadataValidatorStakeUsesExactMissingDefaults(t *testing.T) {
	t.Parallel()
	fixture := newValidatorStakeTestFixture(t)
	fixture.identity.storage["StakeThreshold"] = json.RawMessage("null")
	observed, err := fixture.read()
	if err != nil || observed.StakeThresholdRao != 0 || observed.SubnetOwnerPresent || !observed.MeetsNonSelfStakeAndPermit() {
		t.Fatalf("runtime defaults differ: %+v, %v", observed, err)
	}
}

// A duplicate owner UID cannot contradict the previously authenticated
// two-way candidate registration and manufacture an owner exception.
func TestRuntimeArtifactMetadataValidatorStakeRejectsConflictingOwnerRegistration(t *testing.T) {
	t.Parallel()
	fixture := newValidatorStakeTestFixture(t)
	ownerUID := fixture.identity.query.UID
	fixture.setOwner(t, [32]byte{44}, &ownerUID)
	observed, err := fixture.read()
	if err == nil || observed != (ValidatorStakeObservation{}) || fixture.runtimeCalls != 0 {
		t.Fatalf("conflicting owner registration escaped: %+v, %v", observed, err)
	}
}

// Each additional storage field requires its exact width, including a
// registered owner UID that is outside the independently bounded subnet.
func TestRuntimeArtifactMetadataValidatorStakeRejectsMalformedStorage(t *testing.T) {
	t.Parallel()
	for _, example := range []struct {
		name  string
		value []byte
	}{
		{name: "StakeThreshold", value: []byte{1}},
		{name: "StakeThreshold", value: make([]byte, 9)},
		{name: "SubnetOwnerHotkey", value: make([]byte, 31)},
		{name: "SubnetOwnerHotkey", value: make([]byte, 33)},
		{name: "OwnerUID", value: []byte{0}},
		{name: "OwnerUID", value: []byte{3, 0}},
	} {
		fixture := newValidatorStakeTestFixture(t)
		fixture.setOwner(t, [32]byte{44}, nil)
		fixture.identity.storage[example.name] = validatorIdentityTestHex(example.value)
		observed, err := fixture.read()
		if err == nil || observed != (ValidatorStakeObservation{}) || fixture.runtimeCalls != 0 {
			t.Errorf("malformed %s escaped: %+v, %v", example.name, observed, err)
		}
	}
}

// The complete historical reader still rejects canceled work before any RPC.
func TestRuntimeArtifactMetadataValidatorStakeRejectsPreCanceledContext(t *testing.T) {
	t.Parallel()
	fixture := newValidatorStakeTestFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	fixture.identity.ctx = ctx
	observed, err := fixture.read()
	if !errors.Is(err, context.Canceled) || observed != (ValidatorStakeObservation{}) || len(fixture.identity.calls) != 0 {
		t.Fatalf("pre-canceled stake read escaped: %+v, %v", observed, err)
	}
}

// Cancellation at the real runtime-call boundary cannot publish a completed
// observation even when that RPC itself returns success and valid bytes.
func TestRuntimeArtifactMetadataValidatorStakeRejectsLateCancellation(t *testing.T) {
	t.Parallel()
	fixture := newValidatorStakeTestFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fixture.identity.ctx = ctx
	call := fixture.identity.hook
	fixture.identity.hook = func(ctx context.Context, result any, method string, args ...any) (bool, error) {
		handled, err := call(ctx, result, method, args...)
		if method == "state_call" {
			cancel()
		}
		return handled, err
	}
	observed, err := fixture.read()
	if !errors.Is(err, context.Canceled) || observed != (ValidatorStakeObservation{}) || fixture.runtimeCalls != 1 {
		t.Fatalf("late-canceled stake read escaped: %+v, %v", observed, err)
	}
}

// An allowed but unreviewed layout cannot reach the v454 runtime API decoder.
func TestRuntimeArtifactMetadataValidatorStakeRejectsUnreviewedLayout(t *testing.T) {
	t.Parallel()
	fixture := newValidatorStakeTestFixture(t)
	fixture.identity.version.SpecVersion = 453
	fixture.identity.allowed[0].Version = fixture.identity.version
	observed, err := fixture.read()
	if err == nil || observed != (ValidatorStakeObservation{}) || fixture.runtimeCalls != 0 {
		t.Fatalf("unreviewed stake layout escaped: %+v, %v", observed, err)
	}
}

// Exact runtime RPC failures remain visible rather than becoming zero stake.
func TestRuntimeArtifactMetadataValidatorStakePreservesRuntimeCallFailure(t *testing.T) {
	t.Parallel()
	fixture := newValidatorStakeTestFixture(t)
	sentinel := errors.New("runtime read unavailable")
	call := fixture.identity.hook
	fixture.identity.hook = func(ctx context.Context, result any, method string, args ...any) (bool, error) {
		if method == "state_call" {
			return true, sentinel
		}
		return call(ctx, result, method, args...)
	}
	observed, err := fixture.read()
	if !errors.Is(err, sentinel) || observed != (ValidatorStakeObservation{}) {
		t.Fatalf("runtime call failure disappeared: %+v, %v", observed, err)
	}
}

// A provider's changed canonical view cannot publish a mixed historical read.
func TestRuntimeArtifactMetadataValidatorStakeRejectsLateCanonicalChange(t *testing.T) {
	t.Parallel()
	fixture := newValidatorStakeTestFixture(t)
	call := fixture.identity.hook
	fixture.identity.hook = func(ctx context.Context, result any, method string, args ...any) (bool, error) {
		handled, err := call(ctx, result, method, args...)
		if method == "state_call" {
			fixture.identity.blockHashes[100] = types.Hash{88}
		}
		return handled, err
	}
	observed, err := fixture.read()
	if err == nil || observed != (ValidatorStakeObservation{}) || fixture.runtimeCalls != 1 {
		t.Fatalf("mixed canonical stake read escaped: %+v, %v", observed, err)
	}
}

// All fields, including non-selected peers, must retain exact vector counts.
func TestRuntimeArtifactMetadataValidatorStakeRejectsEveryCensusMismatch(t *testing.T) {
	t.Parallel()
	for _, index := range []int{30, 52, 57, 69} {
		for _, count := range []uint64{0, 2, 4, math.MaxUint64} {
			fixture := newValidatorStakeTestFixture(t)
			fixture.fields[index] = append(validatorStakeTestCompact(t, count), fixture.fields[index][1:]...)
			fixture.publish(t)
			observed, err := fixture.read()
			if err == nil || observed != (ValidatorStakeObservation{}) {
				t.Errorf("field%d count%d escaped: %+v, %v", index, count, observed, err)
			}
		}
	}
}

// Unselected optional identities/commitments/validator lists must never be
// silently ignored or trigger an unbounded generic SCALE allocation.
func TestRuntimeArtifactMetadataValidatorStakeRejectsUnselectedFields(t *testing.T) {
	t.Parallel()
	for _, index := range []int{1, 5, 72, 73, 76} {
		fixture := newValidatorStakeTestFixture(t)
		fixture.fields[index] = []byte{0}
		fixture.publish(t)
		observed, err := fixture.read()
		if err == nil || observed != (ValidatorStakeObservation{}) {
			t.Errorf("unselected field%d escaped: %+v, %v", index, observed, err)
		}
	}
}

// Malformed data at a different UID invalidates the entire observation.
func TestRuntimeArtifactMetadataValidatorStakeRejectsMalformedPeer(t *testing.T) {
	t.Parallel()
	for _, stakeFailure := range []bool{false, true} {
		fixture := newValidatorStakeTestFixture(t)
		if stakeFailure {
			fixture.fields[69] = append([]byte{12}, validatorStakeTestCompact(t, math.MaxUint64)...)
			fixture.fields[69] = append(fixture.fields[69], validatorStakeTestCompact(t, 150)...)
			fixture.fields[69] = append(fixture.fields[69], 0)
		} else {
			fixture.fields[57][1] = 2
		}
		fixture.publish(t)
		observed, err := fixture.read()
		if err == nil || observed != (ValidatorStakeObservation{}) {
			t.Errorf("malformed peer escaped, stake=%t: %+v, %v", stakeFailure, observed, err)
		}
	}
}

// Each selected identity cross-check is independent of a well-formed stake.
func TestRuntimeArtifactMetadataValidatorStakeRejectsIdentityDisagreement(t *testing.T) {
	t.Parallel()
	for _, field := range []string{"netuid", "hotkey", "permit"} {
		fixture := newValidatorStakeTestFixture(t)
		switch field {
		case "netuid":
			fixture.runtimeRaw = validatorIdentityTestHex(validatorStakeTestMetagraph(t, 522, fixture.fields))
		case "hotkey":
			fixture.fields[52][1+32] ^= 1
			fixture.publish(t)
		case "permit":
			fixture.fields[57][2] = 0
			fixture.publish(t)
		}
		observed, err := fixture.read()
		if err == nil || observed != (ValidatorStakeObservation{}) {
			t.Errorf("changed %s escaped: %+v, %v", field, observed, err)
		}
	}
}

// Every truncated prefix and trailing data is refused, not decoded partially.
func TestRuntimeArtifactMetadataValidatorStakeRejectsTruncationAndTrailingData(t *testing.T) {
	t.Parallel()
	fixture := newValidatorStakeTestFixture(t)
	identity := validatorIdentityReplayTestObservation(t)
	data := validatorStakeTestMetagraph(t, 521, fixture.fields)
	for size := 0; size < len(data); size++ {
		if _, err := decodeValidatorStakeMetagraph(validatorIdentityTestHex(data[:size]), identity); err == nil {
			t.Fatalf("truncated metagraph size%d accepted", size)
		}
	}
	if _, err := decodeValidatorStakeMetagraph(validatorIdentityTestHex(append(data, 0)), identity); err == nil {
		t.Fatal("trailing metagraph byte accepted")
	}
}

// JSON and SCALE outer framing are admitted before any census-based decode.
func TestRuntimeArtifactMetadataValidatorStakeRejectsMalformedEnvelope(t *testing.T) {
	t.Parallel()
	identity := validatorIdentityReplayTestObservation(t)
	for _, raw := range []json.RawMessage{
		json.RawMessage("null"), json.RawMessage(`"0x0"`), json.RawMessage(`"0xgg"`),
		json.RawMessage(`"0x\\u0031"`), json.RawMessage(`"0x00"`), json.RawMessage(`"0x02"`),
		validatorIdentityTestHex(make([]byte, 1000)),
	} {
		if _, err := decodeValidatorStakeMetagraph(raw, identity); err == nil {
			t.Errorf("malformed stake envelope accepted, bytes=%d", len(raw))
		}
	}
}

// Full-width thresholds and stake do not round through float64 or wrap when
// compared. The runtime's nonnegative I64F64 total cannot reach MaxUint64.
func TestRuntimeArtifactMetadataValidatorStakePreservesIntegerBoundary(t *testing.T) {
	t.Parallel()
	fixture := newValidatorStakeTestFixture(t)
	fixture.fields[69] = []byte{12, 0}
	fixture.fields[69] = append(fixture.fields[69], validatorStakeTestCompact(t, math.MaxInt64)...)
	fixture.fields[69] = append(fixture.fields[69], 0)
	fixture.publish(t)
	for _, threshold := range []uint64{math.MaxInt64, uint64(math.MaxInt64) + 1, math.MaxUint64} {
		fixture.identity.storage["StakeThreshold"] = validatorIdentityTestHex(binary.LittleEndian.AppendUint64(nil, threshold))
		observed, err := fixture.read()
		if err != nil || observed.TotalStakeRao != math.MaxInt64 || observed.MeetsNonSelfStakeAndPermit() != (threshold == math.MaxInt64) {
			t.Errorf("integer threshold%d differs: %+v, %v", threshold, observed, err)
		}
	}
}

// Compact boundaries are independently encoded by the SDK, including u64max;
// the primitive decoder itself is not restricted to I64F64 stake ranges.
func TestRuntimeArtifactMetadataValidatorStakeCompactBoundaries(t *testing.T) {
	t.Parallel()
	for _, value := range []uint64{0, 1, 63, 64, 16*1024 - 1, 16 * 1024, 1024*1024*1024 - 1, 1024 * 1024 * 1024, math.MaxInt64, math.MaxUint64} {
		encoded := validatorStakeTestCompact(t, value)
		decoded, width, err := decodeValidatorStakeCompact(append(bytes.Clone(encoded), 99))
		if err != nil || decoded != value || width != len(encoded) {
			t.Errorf("compact%d: decoded%d width%d error%v", value, decoded, width, err)
		}
		for size := 0; size < len(encoded); size++ {
			if _, _, err := decodeValidatorStakeCompact(encoded[:size]); err == nil {
				t.Errorf("truncated compact%d size%d accepted", value, size)
			}
		}
	}
}

// Wider encodings of small numbers, a zero high byte, and integers wider
// than u64 are not alternate representations of the same runtime evidence.
func TestRuntimeArtifactMetadataValidatorStakeCompactRejectsNoncanonical(t *testing.T) {
	t.Parallel()
	for _, encoded := range [][]byte{
		{1, 0}, {253, 0}, {2, 0, 0, 0}, {254, 255, 0, 0},
		{3, 0, 0, 0, 0}, {3, 255, 255, 255, 63},
		{7, 0, 0, 0, 64, 0}, {23, 0, 0, 0, 0, 0, 0, 0, 0, 1}, {255},
	} {
		if _, _, err := decodeValidatorStakeCompact(encoded); err == nil {
			t.Errorf("noncanonical compact%x accepted", encoded)
		}
	}
}

// An actual finalized public testnet response independently checks the frozen
// field ordering and compact encoding. This is a captured codec regression,
// not a fresh network query or proof of present validator eligibility.
func TestRuntimeArtifactMetadataValidatorStakeDecodesCapturedRuntime454(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/validator_stake_v454_net521_block7949341.json")
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		t.Fatal(err)
	}
	hotkey, err := hex.DecodeString("a20080205804bba26541104cbac4def8a0eba26f7a86f6f1e7d16ad37f0cd658")
	if err != nil {
		t.Fatal(err)
	}
	identity := ValidatorIdentityObservation{Netuid: 521, UID: 0, SubnetUIDs: 256, ValidatorPermit: true}
	copy(identity.Hotkey[:], hotkey)
	stake, err := decodeValidatorStakeMetagraph(response.Result, identity)
	if err != nil || stake != 105_521_899_315_868 {
		t.Fatalf("captured runtime454 stake=%d, want105521899315868: %v", stake, err)
	}
}

// Neither missing selected options nor extra unselected ones are silently
// treated as empty/default data in the exact requested runtime response.
func TestRuntimeArtifactMetadataValidatorStakeRejectsMissingSelectedField(t *testing.T) {
	t.Parallel()
	for _, index := range []int{30, 52, 57, 69} {
		fixture := newValidatorStakeTestFixture(t)
		delete(fixture.fields, index)
		fixture.publish(t)
		observed, err := fixture.read()
		if err == nil || observed != (ValidatorStakeObservation{}) {
			t.Errorf("missing selected field%d escaped: %+v, %v", index, observed, err)
		}
	}
}

// The independently admitted u16 census limit is inclusive, and the last UID
// must be decoded without a loop wrap or vector-length allocation from input.
func TestRuntimeArtifactMetadataValidatorStakeDecodesMaximumCensus(t *testing.T) {
	t.Parallel()
	const count = uint16(math.MaxUint16)
	identity := ValidatorIdentityObservation{Netuid: 521, UID: count - 1, SubnetUIDs: count, Hotkey: [32]byte{77}, ValidatorPermit: true}
	countBytes := validatorStakeTestCompact(t, uint64(count))
	hotkeys := append(bytes.Clone(countBytes), make([]byte, int(count)*32)...)
	copy(hotkeys[len(countBytes)+int(identity.UID)*32:], identity.Hotkey[:])
	permits := append(bytes.Clone(countBytes), make([]byte, int(count))...)
	permits[len(permits)-1] = 1
	stakes := append(bytes.Clone(countBytes), make([]byte, int(count)-1)...)
	stakes = append(stakes, validatorStakeTestCompact(t, 777)...)
	data := validatorStakeTestMetagraph(t, identity.Netuid, map[int][]byte{30: countBytes, 52: hotkeys, 57: permits, 69: stakes})
	stake, err := decodeValidatorStakeMetagraph(validatorIdentityTestHex(data), identity)
	if err != nil || stake != 777 {
		t.Fatalf("maximum census/lastUID stake=%d, error=%v", stake, err)
	}
}

// The endpoint writes real JSON-RPC envelopes through the pinned GSRPC HTTP
// decoder. The existing fixture still checks every historical argument and
// supplies real SDK metadata and the complete selective runtime response.
// Stop joins all handlers before callers inspect fixture-owned transcripts.
func useValidatorStakeHTTPFixture(t *testing.T, fixture *validatorStakeTestFixture, intercept func(http.ResponseWriter, *http.Request, chainContextRPCRequest, string) bool) func() {
	t.Helper()
	var stateLock sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var rpcRequest chainContextRPCRequest
		if err := json.NewDecoder(request.Body).Decode(&rpcRequest); err != nil {
			t.Errorf("decode stake transport request: %v", err)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		var args []any
		if len(rpcRequest.Params) != 0 {
			args = make([]any, len(rpcRequest.Params))
		}
		for index, encoded := range rpcRequest.Params {
			if rpcRequest.Method == "chain_getBlockHash" {
				var number uint64
				if err := json.Unmarshal(encoded, &number); err != nil {
					t.Errorf("decode exact block height: %v", err)
					writer.WriteHeader(http.StatusBadRequest)
					return
				}
				args[index] = number
			} else {
				var value string
				if err := json.Unmarshal(encoded, &value); err != nil {
					t.Errorf("decode exact RPC string: %v", err)
					writer.WriteHeader(http.StatusBadRequest)
					return
				}
				args[index] = value
			}
		}
		name := rpcRequest.Method
		if rpcRequest.Method == "state_getStorage" {
			if len(args) != 2 || args[1] != fixture.identity.query.BlockHash.Hex() {
				t.Error("stake transport storage lost its exact historical block")
				writer.WriteHeader(http.StatusBadRequest)
				return
			}
			name = fixture.identity.keyNames[args[0].(string)]
			if name == "" {
				t.Error("stake transport storage used an unknown authenticated key")
				writer.WriteHeader(http.StatusBadRequest)
				return
			}
		}
		if intercept != nil && intercept(writer, request, rpcRequest, name) {
			return
		}
		var raw json.RawMessage
		var header types.Header
		var result any = &raw
		if rpcRequest.Method == "chain_getHeader" {
			result = &header
		}
		err := func() error {
			stateLock.Lock()
			defer stateLock.Unlock()
			return fixture.identity.call(fixture.identity.ctx, result, rpcRequest.Method, args...)
		}()
		if err != nil {
			t.Errorf("unexpected exact stake RPC: %v", err)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		writeChainContextRPCResult(writer, rpcRequest, result)
	}))
	client, err := gsrpcgeth.DialContext(fixture.identity.ctx, server.URL)
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	fixture.identity.chain.API.Client = &contextSubstrateClient{Client: client, url: server.URL}
	var once sync.Once
	stop := func() { once.Do(func() { client.Close(); server.Close() }) }
	t.Cleanup(stop)
	return stop
}

// This is the actual live failure: a present result:null member for the exact
// threshold key must select runtime454's zero, then retain absent owner state.
func TestRuntimeArtifactMetadataValidatorStakeTransportNullDefaultAndOwnerAbsence(t *testing.T) {
	t.Parallel()
	fixture := newValidatorStakeTestFixture(t)
	fixture.identity.storage["StakeThreshold"] = json.RawMessage("null")
	meta, runtime := fixture.identity.chain.Meta, fixture.identity.chain.Runtime
	stop := useValidatorStakeHTTPFixture(t, fixture, nil)
	observed, err := fixture.read()
	stop()
	if err != nil || observed.StakeThresholdRao != 0 || observed.TotalStakeRao != 150 ||
		observed.SubnetOwnerPresent || observed.SubnetOwnerRegistered || !observed.MeetsNonSelfStakeAndPermit() ||
		fixture.runtimeCalls != 1 || fixture.identity.chain.Meta != meta || fixture.identity.chain.Runtime != runtime {
		t.Fatalf("real transport null default differs: %+v, calls=%d, error=%v", observed, fixture.runtimeCalls, err)
	}
}

// The preceding identity reader has the same transport boundary for its sole
// optional zero-alpha field; registration, ownership and permit remain required.
func TestRuntimeArtifactMetadataValidatorStakeTransportNullAlphaDefault(t *testing.T) {
	t.Parallel()
	fixture := newValidatorStakeTestFixture(t)
	fixture.identity.storage["TotalHotkeyAlpha"] = json.RawMessage("null")
	stop := useValidatorStakeHTTPFixture(t, fixture, nil)
	observed, err := fixture.read()
	stop()
	if err != nil || observed.Identity.StakeAlphaRao != 0 || observed.TotalStakeRao != 150 ||
		observed.StakeThresholdRao != 100 || !observed.MeetsNonSelfStakeAndPermit() || fixture.runtimeCalls != 1 {
		t.Fatalf("real transport optional alpha differs: %+v, error=%v", observed, err)
	}
}

// A missing reverse owner registration is distinct from encoded UID0. Both
// calls retain a different selected UID and cannot create an owner exemption.
func TestRuntimeArtifactMetadataValidatorStakeTransportOwnerRegistrationNullAndZero(t *testing.T) {
	t.Parallel()
	for _, registered := range []bool{false, true} {
		fixture := newValidatorStakeTestFixture(t)
		var uid *uint16
		if registered {
			value := uint16(0)
			uid = &value
		}
		fixture.setOwner(t, [32]byte{44}, uid)
		fixture.identity.storage["StakeThreshold"] = validatorIdentityTestHex(binary.LittleEndian.AppendUint64(nil, 151))
		stop := useValidatorStakeHTTPFixture(t, fixture, nil)
		observed, err := fixture.read()
		stop()
		if err != nil || !observed.SubnetOwnerPresent || observed.SubnetOwnerRegistered != registered ||
			observed.SubnetOwnerUID != 0 || observed.MeetsNonSelfStakeAndPermit() || fixture.runtimeCalls != 1 {
			t.Fatalf("real owner registration present=%t differs: %+v, error=%v", registered, observed, err)
		}
	}
}

// UID0 itself receives the exception only when the exact owner hotkey and
// reverse UID are present. Null owner storage must not turn default UID0 into
// privilege for an otherwise unpermitted, below-threshold signing identity.
func TestRuntimeArtifactMetadataValidatorStakeTransportSelectedOwnerUIDZero(t *testing.T) {
	t.Parallel()
	for _, ownerPresent := range []bool{false, true} {
		fixture := newValidatorStakeTestFixture(t)
		identity := fixture.identity
		identity.query.UID = 0
		identity.storage["Uids"] = validatorIdentityTestHex([]byte{0, 0})
		key, err := types.CreateStorageKey(identity.metadata, PalletName, "Keys", binary.LittleEndian.AppendUint16(nil, identity.query.Netuid), []byte{0, 0})
		if err != nil {
			t.Fatal(err)
		}
		identity.keyNames[key.Hex()] = "Keys"
		hotkeys := validatorStakeTestCompact(t, 3)
		for _, hotkey := range [][32]byte{identity.hotkey, {32}, {33}} {
			hotkeys = append(hotkeys, hotkey[:]...)
		}
		fixture.fields[52] = hotkeys
		fixture.fields[57] = []byte{12, 0, 0, 0}
		identity.storage["ValidatorPermit"] = validatorIdentityTestHex(fixture.fields[57])
		identity.storage["StakeThreshold"] = validatorIdentityTestHex(binary.LittleEndian.AppendUint64(nil, math.MaxUint64))
		if ownerPresent {
			fixture.setOwner(t, identity.hotkey, nil)
		}
		fixture.publish(t)
		stop := useValidatorStakeHTTPFixture(t, fixture, nil)
		observed, err := fixture.read()
		stop()
		if err != nil || observed.Identity.UID != 0 || observed.SubnetOwnerPresent != ownerPresent ||
			observed.SubnetOwnerRegistered != ownerPresent || observed.SubnetOwnerUID != 0 ||
			observed.MeetsNonSelfStakeAndPermit() != ownerPresent {
			t.Fatalf("selected UID0 owner present=%t differs: %+v, error=%v", ownerPresent, observed, err)
		}
	}
}

// The real transport exposes omitted result separately as ErrNoResult. A
// default seed must not conceal it, an RPC/HTTP error, malformed JSON/types,
// or either side of an exact storage width. All observations stay atomic.
func TestRuntimeArtifactMetadataValidatorStakeTransportRejectsMissingAndMalformedOptional(t *testing.T) {
	t.Parallel()
	for _, field := range []struct {
		name  string
		width int
	}{
		{name: "StakeThreshold", width: 8}, {name: "TotalHotkeyAlpha", width: 8},
		{name: "SubnetOwnerHotkey", width: 32}, {name: "OwnerUID", width: 2},
	} {
		for _, kind := range []string{"missing result", "RPC error", "HTTP error", "bad JSON", "boolean", "quoted null", "short", "long"} {
			fixture := newValidatorStakeTestFixture(t)
			fixture.setOwner(t, [32]byte{44}, nil)
			var hits atomic.Int32
			stop := useValidatorStakeHTTPFixture(t, fixture, func(writer http.ResponseWriter, request *http.Request, rpcRequest chainContextRPCRequest, name string) bool {
				if name != field.name {
					return false
				}
				hits.Add(1)
				switch kind {
				case "missing result":
					_ = json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": rpcRequest.ID})
				case "RPC error":
					_ = json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": rpcRequest.ID, "error": map[string]any{"code": -32080, "message": "fixture storage error"}})
				case "HTTP error":
					writer.WriteHeader(http.StatusServiceUnavailable)
				case "bad JSON":
					_, _ = writer.Write([]byte("{"))
				case "boolean":
					writeChainContextRPCResult(writer, rpcRequest, true)
				case "quoted null":
					writeChainContextRPCResult(writer, rpcRequest, "null")
				case "short":
					writeChainContextRPCResult(writer, rpcRequest, validatorIdentityTestHex(make([]byte, field.width-1)))
				case "long":
					writeChainContextRPCResult(writer, rpcRequest, validatorIdentityTestHex(make([]byte, field.width+1)))
				}
				return true
			})
			observed, err := fixture.read()
			stop()
			if err == nil || observed != (ValidatorStakeObservation{}) || fixture.runtimeCalls != 0 || hits.Load() != 1 {
				t.Errorf("%s %s escaped transport refusal: %+v, error=%v", field.name, kind, observed, err)
			}
			if kind == "missing result" && !errors.Is(err, gsrpcgeth.ErrNoResult) {
				t.Errorf("%s lost real omitted-result discriminator: %v", field.name, err)
			}
		}
	}
}

// Required registration/owner/permit and the complete runtime response have
// no null default. Successful JSON-RPC transport does not grant missing state.
func TestRuntimeArtifactMetadataValidatorStakeTransportRejectsRequiredNull(t *testing.T) {
	t.Parallel()
	for _, field := range []string{"SubnetworkN", "Keys", "Uids", "Owner", "ValidatorPermit", "state_call"} {
		fixture := newValidatorStakeTestFixture(t)
		fixture.setOwner(t, fixture.identity.hotkey, nil)
		var hits atomic.Int32
		stop := useValidatorStakeHTTPFixture(t, fixture, func(writer http.ResponseWriter, request *http.Request, rpcRequest chainContextRPCRequest, name string) bool {
			if name != field {
				return false
			}
			hits.Add(1)
			writeChainContextRPCResult(writer, rpcRequest, nil)
			return true
		})
		observed, err := fixture.read()
		stop()
		if err == nil || observed != (ValidatorStakeObservation{}) || hits.Load() != 1 {
			t.Errorf("required real transport null %s escaped: %+v, error=%v", field, observed, err)
		}
	}
}

// Two complete observations share the actual client, but each storage read
// starts fresh. The direct calls then pin the SDK's untouched-destination
// behavior, including stale hex, nil payload and an omitted response member.
func TestRuntimeArtifactMetadataValidatorStakeTransportFreshDefaultsAndReusedResult(t *testing.T) {
	t.Parallel()
	fixture := newValidatorStakeTestFixture(t)
	var thresholdCalls atomic.Int32
	stop := useValidatorStakeHTTPFixture(t, fixture, func(writer http.ResponseWriter, request *http.Request, rpcRequest chainContextRPCRequest, name string) bool {
		if name == "StakeThreshold" {
			count := thresholdCalls.Add(1)
			if count == 1 {
				return false
			}
			if count == 6 {
				_ = json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": rpcRequest.ID})
			} else {
				writeChainContextRPCResult(writer, rpcRequest, nil)
			}
			return true
		}
		if name == "SubnetOwnerHotkey" {
			if thresholdCalls.Load() == 1 {
				writeChainContextRPCResult(writer, rpcRequest, validatorIdentityTestHex(fixture.identity.hotkey[:]))
			} else {
				writeChainContextRPCResult(writer, rpcRequest, nil)
			}
			return true
		}
		return false
	})
	first, firstErr := fixture.read()
	again, againErr := fixture.read()
	key, err := types.CreateStorageKey(fixture.identity.metadata, PalletName, "StakeThreshold")
	if err != nil {
		t.Fatal(err)
	}
	call := func(raw *json.RawMessage) error {
		return fixture.identity.chain.API.Client.CallContext(fixture.identity.ctx, raw, "state_getStorage", key.Hex(), fixture.identity.query.BlockHash.Hex())
	}
	old := validatorIdentityTestHex(binary.LittleEndian.AppendUint64(nil, 999))
	raw := json.RawMessage(bytes.Clone(old))
	staleErr := call(&raw)
	stalePreserved := bytes.Equal(raw, old)
	raw = json.RawMessage("null")
	nullErr := call(&raw)
	nullPreserved := bytes.Equal(raw, []byte("null"))
	raw = nil
	nilErr := call(&raw)
	nilPreserved := raw == nil
	_, nilDecodeErr := decodeValidatorIdentityHexResult(raw, 8, true)
	raw = json.RawMessage("null")
	missingErr := call(&raw)
	stop()
	if firstErr != nil || againErr != nil || first.StakeThresholdRao != 100 || !first.SubnetOwnerRegistered ||
		again.StakeThresholdRao != 0 || again.SubnetOwnerPresent || again.SubnetOwnerRegistered || fixture.runtimeCalls != 2 {
		t.Fatalf("repeated real observations retained stale state: first=%+v error=%v again=%+v error=%v", first, firstErr, again, againErr)
	}
	if staleErr != nil || !stalePreserved || nullErr != nil || !nullPreserved || nilErr != nil || !nilPreserved || nilDecodeErr == nil {
		t.Fatalf("SDK null/nil boundary differs: stale=%v null=%v nil=%v decode=%v", staleErr, nullErr, nilErr, nilDecodeErr)
	}
	if !errors.Is(missingErr, gsrpcgeth.ErrNoResult) {
		t.Fatalf("real omitted result was confused with null: %v", missingErr)
	}
}

// Cancellation is forced after the actual request reaches the threshold
// endpoint and before its null response is released. Every HTTP handler joins
// before the empty result and original cancellation cause are inspected.
func TestRuntimeArtifactMetadataValidatorStakeTransportCancellationBeforeNullResponse(t *testing.T) {
	t.Parallel()
	fixture := newValidatorStakeTestFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	fixture.identity.ctx = ctx
	entered, release := make(chan struct{}), make(chan struct{})
	stop := useValidatorStakeHTTPFixture(t, fixture, func(writer http.ResponseWriter, request *http.Request, rpcRequest chainContextRPCRequest, name string) bool {
		if name != "StakeThreshold" {
			return false
		}
		close(entered)
		select {
		case <-release:
		case <-request.Context().Done():
		}
		writeChainContextRPCResult(writer, rpcRequest, nil)
		return true
	})
	type outcome struct {
		observation ValidatorStakeObservation
		err         error
	}
	done := make(chan outcome, 1)
	go func() { observed, err := fixture.read(); done <- outcome{observation: observed, err: err} }()
	select {
	case <-entered:
	case completed := <-done:
		close(release)
		stop()
		t.Fatalf("threshold transport barrier was not reached: %v", completed.err)
	}
	cancel()
	close(release)
	completed := <-done
	stop()
	if !errors.Is(completed.err, context.Canceled) || completed.observation != (ValidatorStakeObservation{}) || fixture.runtimeCalls != 0 {
		t.Fatalf("canceled real null response published defaults: %+v, error=%v", completed.observation, completed.err)
	}
}
