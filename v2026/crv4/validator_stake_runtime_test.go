// Synthetic exact approvals drive the real public stake reader. A future
// spec is not provisional authority; only this fixture's block/code/metadata
// tuple is allowed, and the consumed API still has to match at that block.
package crv4

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// Every fixture owns its mutable Rpc transcript. The baseline's public wire
// shapes are copied into independent decoded metadata, never shared mutably.
type validatorStakeCapabilityTestFixture struct {
	stake        *validatorStakeTestFixture
	apiVersions  json.RawMessage
	versionReads int
	beforeRead   func(int)
}

// The current connection deliberately has unrelated dial-time metadata. Its
// query is historical and every actual read is required to use that block.
func newValidatorStakeCapabilityTestFixture(t *testing.T) *validatorStakeCapabilityTestFixture {
	t.Helper()
	self := &validatorStakeCapabilityTestFixture{
		stake: newValidatorStakeTestFixture(t), apiVersions: json.RawMessage(`[["0x8375104b299b74c5",2]]`),
	}
	identity := self.stake.identity
	identity.metadata, _, _ = provisionalRuntimeMetadataTest(t)
	identity.version.SpecVersion = ReviewedRuntimeSpecVersion + 17
	identity.allowed[0].Version = identity.version
	self.publish(t)
	prior := identity.hook
	identity.hook = func(ctx context.Context, result any, method string, args ...any) (bool, error) {
		if method != "state_getRuntimeVersion" {
			return prior(ctx, result, method, args...)
		}
		self.versionReads++
		if self.beforeRead != nil {
			self.beforeRead(self.versionReads)
		}
		if !validatorIdentityTestReadContext(ctx, identity.ctx) || !reflect.DeepEqual(args, []any{identity.query.BlockHash.Hex()}) {
			return true, errors.New("capability runtime version is not query-block bound")
		}
		return true, setRuntimeIdentityTestResult(result, map[string]any{
			"specName": identity.version.SpecName, "specVersion": identity.version.SpecVersion,
			"transactionVersion": identity.version.TransactionVersion, "stateVersion": identity.version.StateVersion,
			"apis": self.apiVersions,
		})
	}
	return self
}

// Publishes exactly the fixture's approved bytes and re-derives real storage
// keys so an incompatible hasher cannot be hidden by the scripted transport.
func (self *validatorStakeCapabilityTestFixture) publish(t *testing.T) {
	t.Helper()
	identity := self.stake.identity
	identity.publishMetadata(t)
	identity.keyNames = map[string]string{}
	netuid := binary.LittleEndian.AppendUint16(nil, identity.query.Netuid)
	uid := binary.LittleEndian.AppendUint16(nil, identity.query.UID)
	for _, field := range []struct {
		name string
		args [][]byte
	}{
		{name: "SubnetworkN", args: [][]byte{netuid}},
		{name: "Keys", args: [][]byte{netuid, uid}},
		{name: "Uids", args: [][]byte{netuid, identity.hotkey[:]}},
		{name: "Owner", args: [][]byte{identity.hotkey[:]}},
		{name: "TotalHotkeyAlpha", args: [][]byte{identity.hotkey[:], netuid}},
		{name: "ValidatorPermit", args: [][]byte{netuid}},
		{name: "StakeThreshold"},
		{name: "SubnetOwnerHotkey", args: [][]byte{netuid}},
	} {
		key, err := types.CreateStorageKey(identity.metadata, PalletName, field.name, field.args...)
		if err != nil {
			t.Fatal(err)
		}
		identity.keyNames[key.Hex()] = field.name
	}
}

// An independently approved compatible spec outside every compiled list must
// reach the real runtime API and retain its exact historical observation.
func TestValidatorStakeCapabilityAcceptsApprovedFutureSpec(t *testing.T) {
	t.Parallel()
	fixture := newValidatorStakeCapabilityTestFixture(t)
	identity := fixture.stake.identity
	beforeMeta, beforeRuntime := identity.chain.Meta, identity.chain.Runtime
	observed, err := fixture.stake.read()
	if err != nil {
		t.Fatal(err)
	}
	if observed.Identity.Runtime != identity.allowed[0] || observed.Identity.BlockHash != identity.query.BlockHash ||
		observed.TotalStakeRao != 150 || observed.StakeThresholdRao != 100 || !observed.MeetsNonSelfStakeAndPermit() ||
		fixture.stake.runtimeCalls != 1 || fixture.versionReads != 3 {
		t.Fatalf("compatible exact approval did not preserve the real read: %+v, versions=%d calls=%d", observed, fixture.versionReads, fixture.stake.runtimeCalls)
	}
	if identity.chain.ProvisionalRuntimeCompatibilityEnabled() || identity.chain.Meta != beforeMeta || identity.chain.Runtime != beforeRuntime {
		t.Fatal("read capability acquired provisional/signing authority or changed the shared view")
	}
}

// Read admission is purpose-specific: changed calls, events, signed extensions
// and unrelated pallets cannot revoke a compatible stake observation.
func TestValidatorStakeCapabilityIgnoresUnconsumedInterfaces(t *testing.T) {
	t.Parallel()
	fixture := newValidatorStakeCapabilityTestFixture(t)
	metadata := fixture.stake.identity.metadata
	var pallets []types.PalletMetadataV14
	for _, pallet := range metadata.AsMetadataV14.Pallets {
		if pallet.Name == PalletName {
			pallet.HasCalls, pallet.HasEvents = false, false
			pallets = append(pallets, pallet)
		}
	}
	metadata.AsMetadataV14.Pallets = pallets
	metadata.AsMetadataV14.Extrinsic.SignedExtensions = nil
	fixture.publish(t)
	observed, err := fixture.stake.read()
	if err != nil || observed.TotalStakeRao != 150 || fixture.stake.runtimeCalls != 1 {
		t.Fatalf("unconsumed interface rejected a read: %+v, %v", observed, err)
	}
}

// A matching future spec and even valid-looking runtime output cannot admit
// changed storage width, hashers, defaults or key prefix under this decoder.
func TestValidatorStakeCapabilityRejectsConsumedStorageChanges(t *testing.T) {
	t.Parallel()
	for _, fault := range []string{"width", "hasher", "default", "prefix", "modifier"} {
		fixture := newValidatorStakeCapabilityTestFixture(t)
		for palletIndex := range fixture.stake.identity.metadata.AsMetadataV14.Pallets {
			pallet := &fixture.stake.identity.metadata.AsMetadataV14.Pallets[palletIndex]
			if pallet.Name != PalletName {
				continue
			}
			if fault == "prefix" {
				pallet.Storage.Prefix = "SyntheticChangedPrefix"
			}
			for itemIndex := range pallet.Storage.Items {
				item := &pallet.Storage.Items[itemIndex]
				if item.Name != "Uids" {
					continue
				}
				switch fault {
				case "width":
					item.Type.AsMap.Value = item.Type.AsMap.Key
				case "hasher":
					item.Type.AsMap.Hashers[1] = types.StorageHasherV10{IsIdentity: true}
				case "default":
					item.Fallback = []byte{0x02, 0x01}
				case "modifier":
					item.Modifier = types.StorageFunctionModifierV0{IsDefault: true}
				}
			}
		}
		if fault == "prefix" {
			// The capability must refuse this metadata before key creation.
			fixture.stake.identity.publishMetadata(t)
		} else {
			fixture.publish(t)
		}
		observed, err := fixture.stake.read()
		if err == nil || !strings.Contains(err.Error(), "validator stake consumed interface") || observed != (ValidatorStakeObservation{}) || fixture.stake.runtimeCalls != 0 || len(fixture.stake.identity.storageCalls) != 0 {
			t.Errorf("changed %s reached the runtime decoder: %+v, %v", fault, observed, err)
		}
	}
}

// Metadata14 alone cannot establish a custom runtime API's wire version.
func TestValidatorStakeCapabilityRejectsApiDrift(t *testing.T) {
	t.Parallel()
	for _, apis := range []string{`[]`, `[["0x8375104b299b74c5",3]]`, `[["0x8375104b299b74c5",2],["0x8375104b299b74c5",2]]`, `[["0x8375104b299b74c5","2"]]`} {
		fixture := newValidatorStakeCapabilityTestFixture(t)
		fixture.apiVersions = json.RawMessage(apis)
		observed, err := fixture.stake.read()
		if err == nil || !strings.Contains(err.Error(), "selective-metagraph capability") || observed != (ValidatorStakeObservation{}) || fixture.stake.runtimeCalls != 0 {
			t.Errorf("API %s escaped: %+v, %v", apis, observed, err)
		}
	}
}

// A declared API version never replaces exact response decoding, even after
// an otherwise supported future artifact passes its metadata capability.
func TestValidatorStakeCapabilityRejectsMalformedApiResponse(t *testing.T) {
	t.Parallel()
	fixture := newValidatorStakeCapabilityTestFixture(t)
	fixture.stake.fields[69] = append(validatorStakeTestCompact(t, 2), 0, 0)
	fixture.stake.publish(t)
	observed, err := fixture.stake.read()
	if err == nil || observed != (ValidatorStakeObservation{}) || fixture.stake.runtimeCalls != 1 {
		t.Fatalf("malformed future runtime response escaped: %+v, %v", observed, err)
	}
}

// The read capability supplies no artifact authority and cannot bless a
// version/code/metadata tuple absent from the caller's independent allowance.
func TestValidatorStakeCapabilityKeepsExactArtifactAuthority(t *testing.T) {
	t.Parallel()
	for _, fault := range []string{"version", "code", "metadata"} {
		fixture := newValidatorStakeCapabilityTestFixture(t)
		identity := fixture.stake.identity
		switch fault {
		case "version":
			identity.version.SpecVersion++
		case "code":
			identity.codeHash = types.Hash{0x73}.Hex()
		case "metadata":
			identity.allowed[0].MetadataHash = types.Hash{0x74}.Hex()
		}
		observed, err := fixture.stake.read()
		if err == nil || observed != (ValidatorStakeObservation{}) || fixture.stake.runtimeCalls != 0 {
			t.Errorf("unapproved %s escaped: %+v, %v", fault, observed, err)
		}
	}
}

// A provider changing the selected block's runtime between authentication and
// API observation must not publish a mixed view, even if both specs are allowed.
func TestValidatorStakeCapabilityRejectsMixedBlockIdentity(t *testing.T) {
	t.Parallel()
	for _, boundary := range []int{2, 3} {
		fixture := newValidatorStakeCapabilityTestFixture(t)
		identity := fixture.stake.identity
		successor := identity.allowed[0]
		successor.Version.SpecVersion++
		identity.allowed = append(identity.allowed, successor)
		fixture.beforeRead = func(read int) {
			if read == boundary {
				identity.version = successor.Version
			}
		}
		observed, err := fixture.stake.read()
		if err == nil || !strings.Contains(err.Error(), "runtime changed") || observed != (ValidatorStakeObservation{}) || fixture.stake.runtimeCalls != 0 {
			t.Errorf("mixed identity at read %d escaped: %+v, %v", boundary, observed, err)
		}
	}
}

// Cancellation inside capability observation cannot publish a successful
// read or proceed to the custom runtime API. No timing assumptions are used.
func TestValidatorStakeCapabilityPreservesCancellation(t *testing.T) {
	t.Parallel()
	fixture := newValidatorStakeCapabilityTestFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	fixture.stake.identity.ctx = ctx
	fixture.beforeRead = func(read int) {
		if read == 2 {
			cancel()
		}
	}
	observed, err := fixture.stake.read()
	if !errors.Is(err, context.Canceled) || observed != (ValidatorStakeObservation{}) || fixture.stake.runtimeCalls != 0 {
		t.Fatalf("canceled capability escaped: %+v, %v", observed, err)
	}
}

// The schedule reader additionally consumes an epoch field. Its profile must
// refuse an incompatible epoch before any storage while stake-only work can
// still use the compatible subset at the same approved block.
func TestValidatorStakeCapabilityScheduleKeepsItsOwnPurpose(t *testing.T) {
	t.Parallel()
	fixture := newValidatorStakeCapabilityTestFixture(t)
	identity := fixture.stake.identity
	query := ValidatorScheduleQuery{
		GenesisHash: identity.query.GenesisHash, BlockHash: identity.query.BlockHash, BlockNumber: identity.query.BlockNumber,
		Netuid: identity.query.Netuid, Hotkey: identity.hotkey, MaximumSubnetUIDs: identity.query.MaximumSubnetUIDs,
	}
	key, err := types.CreateStorageKey(identity.metadata, PalletName, "SubnetEpochIndex", binary.LittleEndian.AppendUint16(nil, query.Netuid))
	if err != nil {
		t.Fatal(err)
	}
	identity.keyNames[key.Hex()] = "SubnetEpochIndex"
	identity.storage["SubnetEpochIndex"] = validatorIdentityTestHex(binary.LittleEndian.AppendUint64(nil, 37))
	observed, err := ReadValidatorScheduleAtContext(identity.ctx, identity.chain, query, identity.allowed...)
	if err != nil || observed.SubnetEpochIndex != 37 || observed.Stake.Identity.Runtime != identity.allowed[0] {
		t.Fatalf("future schedule failed its exact purpose: %+v, %v", observed, err)
	}
	for palletIndex := range identity.metadata.AsMetadataV14.Pallets {
		pallet := &identity.metadata.AsMetadataV14.Pallets[palletIndex]
		if pallet.Name != PalletName {
			continue
		}
		for itemIndex := range pallet.Storage.Items {
			item := &pallet.Storage.Items[itemIndex]
			if item.Name == "SubnetEpochIndex" {
				item.Type.AsMap.Value = item.Type.AsMap.Key
			}
		}
	}
	identity.publishMetadata(t)
	stake, err := fixture.stake.read()
	if err != nil || stake.TotalStakeRao != 150 {
		t.Fatalf("unconsumed epoch change revoked the stake-only capability: %+v, %v", stake, err)
	}
	identity.storageCalls = nil
	observed, err = ReadValidatorScheduleAtContext(identity.ctx, identity.chain, query, identity.allowed...)
	if err == nil || !strings.Contains(err.Error(), "validator schedule consumed interface storage/SubtensorModule.SubnetEpochIndex changed") || observed != (ValidatorScheduleObservation{}) || len(identity.storageCalls) != 0 {
		t.Fatalf("changed epoch escaped the schedule purpose: %+v, %v", observed, err)
	}
}
