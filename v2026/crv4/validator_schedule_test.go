// Ordinary-history controls use genuine metadata and the actual historical
// registration/stake reader. RPC byte responses are not eligibility verdicts.
package crv4

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"math"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// Each test adds the real epoch map to its own stake fixture. Existing
// identity/stake fixtures and their independent metadata pins are unchanged.
func newValidatorScheduleTestFixture(t *testing.T) (*validatorStakeTestFixture, ValidatorScheduleQuery) {
	t.Helper()
	fixture := newValidatorStakeTestFixture(t)
	identity := fixture.identity
	identity.metadata.AsMetadataV14.Pallets[0].Storage.Items = append(identity.metadata.AsMetadataV14.Pallets[0].Storage.Items, types.StorageEntryMetadataV14{
		Name: "SubnetEpochIndex", Modifier: types.StorageFunctionModifierV0{IsDefault: true},
		Type: types.StorageEntryTypeV14{IsMap: true, AsMap: types.MapTypeV14{Hashers: []types.StorageHasherV10{{IsIdentity: true}}}},
	})
	identity.publishMetadata(t)
	key, err := types.CreateStorageKey(identity.metadata, PalletName, "SubnetEpochIndex", binary.LittleEndian.AppendUint16(nil, identity.query.Netuid))
	if err != nil {
		t.Fatal(err)
	}
	identity.keyNames[key.Hex()], identity.storage["SubnetEpochIndex"] = "SubnetEpochIndex", validatorIdentityTestHex(binary.LittleEndian.AppendUint64(nil, 77))
	query := ValidatorScheduleQuery{GenesisHash: identity.query.GenesisHash, BlockHash: identity.query.BlockHash, BlockNumber: identity.query.BlockNumber, Netuid: identity.query.Netuid, Hotkey: identity.hotkey, MaximumSubnetUIDs: identity.query.MaximumSubnetUIDs}
	return fixture, query
}

// The ledger's earlier UID is intentionally not part of this API. The real
// reverse registration yields UID1 under unrelated dial-time metadata.
func TestRuntimeArtifactMetadataValidatorScheduleResolvesActualHotkey(t *testing.T) {
	t.Parallel()
	fixture, query := newValidatorScheduleTestFixture(t)
	identity := fixture.identity
	meta, runtime := identity.chain.Meta, identity.chain.Runtime
	result, err := ReadValidatorScheduleAtContext(identity.ctx, identity.chain, query, identity.allowed...)
	if err != nil || result.SubnetEpochIndex != 77 || result.Stake.Identity.UID != 1 || result.Stake.Identity.Hotkey != query.Hotkey || result.Stake.Identity.BlockHash != query.BlockHash || !result.Stake.MeetsNonSelfStakeAndPermit() || fixture.runtimeCalls != 1 {
		t.Fatalf("real hotkey/schedule observation differs: %+v, %v", result, err)
	}
	if identity.chain.Meta != meta || identity.chain.Runtime != runtime {
		t.Fatal("historical schedule rebound current metadata or signing runtime")
	}
}

// Strict bounds precede every RPC; a selected UID is never truncated to u16.
func TestRuntimeArtifactMetadataValidatorScheduleRejectsInvalidIndependentPins(t *testing.T) {
	t.Parallel()
	fixture, valid := newValidatorScheduleTestFixture(t)
	for _, change := range []func(*ValidatorScheduleQuery){
		func(query *ValidatorScheduleQuery) { query.GenesisHash = types.Hash{} },
		func(query *ValidatorScheduleQuery) { query.BlockHash = types.Hash{} },
		func(query *ValidatorScheduleQuery) { query.BlockNumber = uint64(math.MaxUint32) + 1 },
		func(query *ValidatorScheduleQuery) { query.Netuid = 0 },
		func(query *ValidatorScheduleQuery) { query.Hotkey = [32]byte{} },
		func(query *ValidatorScheduleQuery) { query.MaximumSubnetUIDs = uint32(math.MaxUint16) + 1 },
	} {
		query := valid
		change(&query)
		result, err := ReadValidatorScheduleAtContext(fixture.identity.ctx, fixture.identity.chain, query, fixture.identity.allowed...)
		if err == nil || result != (ValidatorScheduleObservation{}) || len(fixture.identity.calls) != 0 {
			t.Fatalf("invalid independent pin reached RPC or escaped: %+v, %v", result, err)
		}
	}
}

// A reverse-map response must be complete, bounded and registered. A missing
// optional Uids entry must not silently become UID0.
func TestRuntimeArtifactMetadataValidatorScheduleRejectsMalformedRegistration(t *testing.T) {
	t.Parallel()
	for _, value := range []json.RawMessage{json.RawMessage("null"), json.RawMessage(`"0x01"`), json.RawMessage(`"0x010000"`), json.RawMessage(`"0x0001"`)} {
		fixture, query := newValidatorScheduleTestFixture(t)
		fixture.identity.storage["Uids"] = value
		result, err := ReadValidatorScheduleAtContext(fixture.identity.ctx, fixture.identity.chain, query, fixture.identity.allowed...)
		if err == nil || result != (ValidatorScheduleObservation{}) || fixture.runtimeCalls != 0 {
			t.Fatalf("malformed reverse mapping reached calculated stake: %+v, %v", result, err)
		}
	}
}

// All epoch bytes are consumed; neither null nor an extra byte can choose a
// valid-looking zero or truncated historical native decision number.
func TestRuntimeArtifactMetadataValidatorScheduleRejectsMalformedEpoch(t *testing.T) {
	t.Parallel()
	fixture, query := newValidatorScheduleTestFixture(t)
	for _, value := range []json.RawMessage{json.RawMessage("null"), json.RawMessage(`"0x4d000000000000"`), json.RawMessage(`"0x4d0000000000000000"`)} {
		fixture.identity.storage["SubnetEpochIndex"] = value
		result, err := ReadValidatorScheduleAtContext(fixture.identity.ctx, fixture.identity.chain, query, fixture.identity.allowed...)
		if err == nil || result != (ValidatorScheduleObservation{}) {
			t.Fatalf("malformed schedule value escaped: %+v, %v", result, err)
		}
	}
}

// The real calculated stake remains an observation. History's caller must
// apply the non-self prerequisite; large raw alpha is not substituted here.
func TestRuntimeArtifactMetadataValidatorSchedulePreservesIneligibleObservation(t *testing.T) {
	t.Parallel()
	fixture, query := newValidatorScheduleTestFixture(t)
	fixture.identity.storage["StakeThreshold"] = validatorIdentityTestHex(binary.LittleEndian.AppendUint64(nil, 151))
	result, err := ReadValidatorScheduleAtContext(fixture.identity.ctx, fixture.identity.chain, query, fixture.identity.allowed...)
	if err != nil || result.SubnetEpochIndex != 77 || result.Stake.MeetsNonSelfStakeAndPermit() || result.Stake.TotalStakeRao != 150 {
		t.Fatalf("schedule changed calculated-stake eligibility: %+v, %v", result, err)
	}
}

// The epoch callback changes the same block's final canonical response. The
// operation returns neither the otherwise valid epoch nor a partial identity.
func TestRuntimeArtifactMetadataValidatorScheduleRejectsLateCanonicalChange(t *testing.T) {
	t.Parallel()
	fixture, query := newValidatorScheduleTestFixture(t)
	fixture.identity.after = func(method string, args ...any) {
		if method == "state_getStorage" && fixture.identity.keyNames[args[0].(string)] == "SubnetEpochIndex" {
			fixture.identity.blockHashes[query.BlockNumber] = types.Hash{99}
		}
	}
	result, err := ReadValidatorScheduleAtContext(fixture.identity.ctx, fixture.identity.chain, query, fixture.identity.allowed...)
	if err == nil || result != (ValidatorScheduleObservation{}) || fixture.runtimeCalls != 1 {
		t.Fatalf("late canonical change escaped: %+v, %v", result, err)
	}
}

// Changing the queried hotkey's reverse registration after full identity
// replay cannot publish a mixed current-UID/native-epoch observation.
func TestRuntimeArtifactMetadataValidatorScheduleRejectsLateRegistrationChange(t *testing.T) {
	t.Parallel()
	fixture, query := newValidatorScheduleTestFixture(t)
	fixture.identity.after = func(method string, args ...any) {
		if method == "state_getStorage" && fixture.identity.keyNames[args[0].(string)] == "SubnetEpochIndex" {
			fixture.identity.storage["Uids"] = validatorIdentityTestHex([]byte{2, 0})
		}
	}
	result, err := ReadValidatorScheduleAtContext(fixture.identity.ctx, fixture.identity.chain, query, fixture.identity.allowed...)
	if err == nil || result != (ValidatorScheduleObservation{}) {
		t.Fatalf("late reverse registration change escaped: %+v, %v", result, err)
	}
}

// A callback receives no authority slice ownership. Mutating the caller's
// allowlist cannot change which artifact subsequent identity readers admit.
func TestRuntimeArtifactMetadataValidatorScheduleOwnsRuntimeAllowlist(t *testing.T) {
	t.Parallel()
	fixture, query := newValidatorScheduleTestFixture(t)
	want := fixture.identity.allowed[0]
	fixture.identity.after = func(method string, args ...any) {
		if method == "state_getStorage" && fixture.identity.keyNames[args[0].(string)] == "Uids" {
			fixture.identity.allowed[0] = RuntimeArtifactIdentity{}
		}
	}
	result, err := ReadValidatorScheduleAtContext(fixture.identity.ctx, fixture.identity.chain, query, fixture.identity.allowed...)
	if err != nil || result.Stake.Identity.Runtime != want || fixture.identity.allowed[0] == want {
		t.Fatalf("borrowed runtime policy changed later admission: %+v, %v", result, err)
	}
}

// Context cancellation immediately after the exact epoch read is joined and
// clears every observation, without a later contextless storage request.
func TestRuntimeArtifactMetadataValidatorScheduleJoinsCancellation(t *testing.T) {
	t.Parallel()
	fixture, query := newValidatorScheduleTestFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	fixture.identity.ctx = ctx
	fixture.identity.after = func(method string, args ...any) {
		if method == "state_getStorage" && fixture.identity.keyNames[args[0].(string)] == "SubnetEpochIndex" {
			cancel()
		}
	}
	result, err := ReadValidatorScheduleAtContext(ctx, fixture.identity.chain, query, fixture.identity.allowed...)
	if !errors.Is(err, context.Canceled) || result != (ValidatorScheduleObservation{}) {
		t.Fatalf("late canceled schedule escaped: %+v, %v", result, err)
	}
}
