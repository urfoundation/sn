// Synthetic current-runtime metadata retires only the root-weight call/gates.
// The fixtures exercise the real reader without production accounts or routes.
package main

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/urfoundation/sn/v2026/crv4"
)

// Re-encode the changed protocol schema so normal artifact authentication and
// SCALE lookup construction observe the same bytes as the HTTP fixture.
func rootPassiveTestMetadata(t *testing.T, fixture *rootRpcFixture) {
	t.Helper()
	for i := range fixture.metadata.AsMetadataV14.Pallets {
		pallet := &fixture.metadata.AsMetadataV14.Pallets[i]
		if pallet.Name != "SubtensorModule" {
			continue
		}
		items := make([]types.StorageEntryMetadataV14, 0, len(pallet.Storage.Items))
		for _, item := range pallet.Storage.Items {
			if item.Name != "RootWeightSettingEnabled" && item.Name != "RootWeightsCap" {
				items = append(items, item)
			}
		}
		pallet.Storage.Items = items
		entry := fixture.metadata.AsMetadataV14.EfficientLookup[pallet.Calls.Type.Int64()]
		variants := make([]types.Si1Variant, 0, len(entry.Def.Variant.Variants))
		for _, variant := range entry.Def.Variant.Variants {
			if variant.Name != "set_root_weights" {
				variants = append(variants, variant)
			}
		}
		entry.Def.Variant.Variants = variants
		for index := range fixture.metadata.AsMetadataV14.Lookup.Types {
			portable := &fixture.metadata.AsMetadataV14.Lookup.Types[index]
			if portable.ID.Int64() == pallet.Calls.Type.Int64() {
				portable.Type = *entry
			}
		}
	}
	raw, err := codec.Encode(fixture.metadata)
	if err != nil {
		t.Fatal(err)
	}
	fixture.metadataHex = "0x" + hex.EncodeToString(raw)
	fixture.metadata, fixture.policy.RuntimeMetadataHash, err = crv4.DecodeRuntimeMetadata(fixture.metadataHex)
	if err != nil {
		t.Fatal(err)
	}
}

// Existing delegation is observed as state, not turned off by passive policy.
func newRootPassiveFixture(t *testing.T) (*rpcClient, *rootRpcFixture) {
	t.Helper()
	client, fixture := newRootFixture(t)
	rootPassiveTestMetadata(t, fixture)
	fixture.policy.Schema, fixture.policy.StorageProfile, fixture.policy.RuntimeSourceCommit = rootPassivePolicySchema, rootPassiveStorageProfile, rootPassiveSource
	fixture.policy.DelegationStrategy = "observe_existing"
	fixture.policy.ValidFromBlock, fixture.policy.ValidThroughBlock = 90, 200
	hotkey, _ := hex.DecodeString(fixture.policy.Hotkey[2:])
	fixture.set(t, "AutoParentDelegationEnabled", []byte{1}, hotkey)
	link := append(binary.LittleEndian.AppendUint64([]byte{4}, 1), bytes.Repeat([]byte{0x77}, 32)...)
	fixture.set(t, "ChildKeys", link, hotkey, []byte{25, 0})
	return client, fixture
}

// The removed call and storage fail the old service, while the passive reader
// completes the exact membership/stake/delegation census with no native action.
func TestRootPassiveObservesAfterRootWeightRemoval(t *testing.T) {
	client, f := newRootPassiveFixture(t)
	if _, err := rootWeightsSigningCall(f.metadata); err == nil {
		t.Fatal("retired root call remained available")
	}
	if _, err := rootStorageProfile(f.metadata); err == nil {
		t.Fatal("legacy root storage silently adapted")
	}
	preview, err := client.readRootPreview(t.Context(), f.policy, rootObjectHash(f.policy))
	if err != nil || !preview.ReadOnlyReady || preview.ActivationReady || preview.SelectedSeat == nil || preview.WeightEligibility != "retired-no-root-weight-call" {
		t.Fatalf("passive observation failed: %+v %v", preview, err)
	}
	for _, value := range preview.Storage {
		if rootRetiredWeightStorage(value.Name) {
			t.Fatalf("passive observer read retired weight storage %s", value.Name)
		}
	}
	if preview.DelegationStrategy != "observe_existing" || preview.SelectedSeat.Hotkey != f.policy.Hotkey {
		t.Fatal("passive role lost exact ownership")
	}
	for method := range f.methodCounts {
		if strings.Contains(method, "submit") || strings.Contains(method, "send") {
			t.Fatal("passive observation exposed mutation", method)
		}
	}
}

// Renaming only the policy cannot make an old mutable basket profile passive.
func TestRootPassiveRejectsRetainedRootWeightCapability(t *testing.T) {
	_, f := newRootFixture(t)
	if _, err := rootPassiveStorageMetadata(f.metadata); err == nil {
		t.Fatal("legacy weight capability entered passive strategy")
	}
}

// Exact runtime pins, membership generation and finite approval time remain
// mandatory even though the selected role has no signer or spend allowance.
func TestRootPassiveRejectsChangedRuntimeWindowAndGeneration(t *testing.T) {
	for _, change := range []string{"runtime", "window", "generation", "owner"} {
		client, f := newRootPassiveFixture(t)
		switch change {
		case "runtime":
			f.policy.RuntimeMetadataHash = "0x" + strings.Repeat("ed", 32)
		case "window":
			f.policy.ValidThroughBlock = 99
		case "generation":
			f.policy.ExpectedSeat.RegistrationBlock++
		case "owner":
			f.policy.Coldkey = "0x" + strings.Repeat("ee", 32)
		}
		preview, err := client.readRootPreview(t.Context(), f.policy, rootObjectHash(f.policy))
		if err == nil && preview.ReadOnlyReady {
			t.Fatal("passive observer accepted changed", change)
		}
	}
}

// New approval fields must not expand an older policy's interpretation.
func TestRootPassivePolicyCannotConvertLegacyApproval(t *testing.T) {
	_, f := newRootFixture(t)
	f.policy.ValidThroughBlock = 200
	if err := f.policy.validate(); err == nil {
		t.Fatal("legacy policy acquired passive window")
	}
	_, current := newRootPassiveFixture(t)
	current.policy.DelegationStrategy = "none"
	if err := current.policy.validate(); err == nil {
		t.Fatal("passive approval accepted another delegation strategy")
	}
}
