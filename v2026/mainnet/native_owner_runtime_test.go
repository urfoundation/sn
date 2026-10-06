// Synthetic successor artifacts exercise the actual planning, signing and
// observation boundaries. No fixture supplies deployed identities or authority.
package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/urfoundation/sn/v2026/crv4"
)

// Mutations must be encoded and independently pinned before exercising a reader.
func nativeOwnerTestMetadata(t *testing.T, metadata *types.Metadata) string {
	t.Helper()
	for i := range metadata.AsMetadataV14.Lookup.Types {
		entry := &metadata.AsMetadataV14.Lookup.Types[i]
		if value := metadata.AsMetadataV14.EfficientLookup[entry.ID.Int64()]; value != nil {
			entry.Type = *value
		}
	}
	raw, err := codec.Encode(metadata)
	if err != nil {
		t.Fatal(err)
	}
	return "0x" + hex.EncodeToString(raw)
}

// The new artifact needs its own approval; old signed requests stay byte-exact.
func TestTreasuryRuntimeSuccessorRequiresFreshApproval(t *testing.T) {
	f := newTreasuryFixture(t)
	original := f.config
	originalBytes := bytes.Clone(original.signingBytes())
	f.input.Action.Policy.RuntimeSourceCommit = crv4.NativeOwnerSource473
	f.input.Action.Policy.RuntimeVersion.SpecVersion = 473
	f.input.Observation = f.observation(t, f.input.Action, f.input.Action.BirthBlock, f.input.Action.BirthHash)
	f.input.Action.ObservationHash = f.input.Observation.ContentHash
	f.replan(t)
	request := f.request(t)
	if request.Config.Action.Call != original.Action.Call || request.Config.Action.Payload == original.Action.Payload || request.Config.Action.RequestHash == original.Action.RequestHash {
		t.Fatal("compatible runtime did not retain call while rebinding the signed artifact")
	}
	changed := f.config
	changed.Signature = original.Signature
	if err := changed.validate(f.key); err == nil {
		t.Fatal("new runtime borrowed the old independent approval")
	}
	if err := original.validate(f.key); err != nil || !bytes.Equal(originalBytes, original.signingBytes()) {
		t.Fatal("successor changed historical approval bytes", err)
	}
}

// Owner transition approval and native signing both retain the exact new tuple.
func TestOwnerRecycleRuntimeSuccessorRequiresFreshApproval(t *testing.T) {
	for _, ledger := range []bool{false, true} {
		f := newOwnerRecycleTestFixture(t, ledger)
		original := f.config
		input := ownerTrimTestCopy(t, f.input)
		input.Action.Policy.RuntimeSourceCommit = crv4.NativeOwnerSource473
		input.Action.Policy.RuntimeVersion.SpecVersion = 473
		input.Observation.PolicyHash = rootObjectHash(input.Action.Policy)
		input.Observation.ContentHash = ""
		input.Observation.ContentHash = rootObjectHash(input.Observation)
		input.Action.ObservationHash = input.Observation.ContentHash
		current, err := prepareOwnerRecyclePlan(input)
		if err != nil {
			t.Fatal("reviewed successor did not prepare", ledger, err)
		}
		current.Signature = original.Signature
		if err := current.validate(f.key); err == nil {
			t.Fatal("owner successor borrowed historical approval", ledger)
		}
		current.Signature = hex.EncodeToString(ed25519.Sign(f.approval, current.signingBytes()))
		request, err := newOwnerRecycleSigningRequest(current, f.key, input.Metadata, input.LedgerMetadata)
		if err != nil || request.Config.Action.Call != original.Action.Call || request.SigningBytes == original.Action.Payload {
			t.Fatal("owner successor failed exact signing reconstruction", ledger, err)
		}
		if err := original.validate(f.key); err != nil {
			t.Fatal("historical owner action no longer validates", ledger, err)
		}
	}
}

// Matching a spec number never grants a source capability or repairs bad pins.
func TestNativeOwnerRuntimeSuccessorRejectsUnreviewedIdentity(t *testing.T) {
	f := newTreasuryFixture(t)
	base := f.config.Action.Policy
	base.RuntimeSourceCommit = crv4.NativeOwnerSource473
	base.RuntimeVersion.SpecVersion = 473
	if err := base.validate(); err != nil {
		t.Fatal("reviewed exact synthetic profile rejected", err)
	}
	for i, change := range []func(*treasuryChainPolicy){
		func(p *treasuryChainPolicy) { p.RuntimeSourceCommit = strings.Repeat("a", 40) },
		func(p *treasuryChainPolicy) { p.RuntimeSourceCommit = frontierMappingSourceCommit },
		func(p *treasuryChainPolicy) { p.RuntimeVersion.SpecName = "synthetic-foreign-runtime" },
		func(p *treasuryChainPolicy) { p.RuntimeVersion.SpecVersion = 0 },
		func(p *treasuryChainPolicy) { p.RuntimeVersion.TransactionVersion = 2 },
		func(p *treasuryChainPolicy) { p.RuntimeVersion.StateVersion = 2 },
		func(p *treasuryChainPolicy) { p.RuntimeCodeHash = "" },
		func(p *treasuryChainPolicy) { p.RuntimeMetadataHash = "" },
		func(p *treasuryChainPolicy) { p.GenesisHash = "" },
		func(p *treasuryChainPolicy) { p.EvmChainId++ },
	} {
		p := base
		change(&p)
		if err := p.validate(); err == nil {
			t.Fatal("runtime number substituted for exact reviewed authority", i)
		}
	}
}

// A freshly hashed artifact still cannot change call or signing interpretation.
func TestTreasuryRuntimeSuccessorRejectsChangedCapability(t *testing.T) {
	for _, mode := range []string{"call-index", "field-name", "signed-extension"} {
		f := newTreasuryFixture(t)
		action := f.input.Action
		action.Policy.RuntimeSourceCommit = crv4.NativeOwnerSource473
		action.Policy.RuntimeVersion.SpecVersion = 473
		changed := false
		if mode == "signed-extension" {
			f.metadata.AsMetadataV14.Extrinsic.SignedExtensions[0].Identifier = "SyntheticForeignExtension"
			changed = true
		} else {
			for _, p := range f.metadata.AsMetadataV14.Pallets {
				if p.Name != "Multisig" {
					continue
				}
				entry := f.metadata.AsMetadataV14.EfficientLookup[p.Calls.Type.Int64()]
				for i := range entry.Def.Variant.Variants {
					v := &entry.Def.Variant.Variants[i]
					if v.Name == "approve_as_multi" {
						if mode == "call-index" {
							v.Index = 99
						} else {
							v.Fields[0].Name = "synthetic_other_threshold"
						}
						changed = true
					}
				}
			}
		}
		if !changed {
			t.Fatal("missing mutation target", mode)
		}
		encoded := nativeOwnerTestMetadata(t, f.metadata)
		raw, _ := hex.DecodeString(encoded[2:])
		action.Policy.RuntimeMetadataHash = rootExtrinsicHash(raw)
		if _, err := prepareTreasuryAction(action, encoded); err == nil {
			t.Fatal("reviewed source bypassed consumed capability check", mode)
		}
	}
}

// The source assertion cannot authorize changed enum dispatch or wrong bytes.
func TestOwnerRecycleRuntimeSuccessorRejectsChangedCapability(t *testing.T) {
	f := newOwnerRecycleTestFixture(t, false)
	action := f.input.Action
	action.Policy.RuntimeSourceCommit = crv4.NativeOwnerSource473
	action.Policy.RuntimeVersion.SpecVersion = 473
	if _, err := prepareOwnerRecycleAction(action, "0x6d6574610e"); !errors.Is(err, errNativeMetadataPin) {
		t.Fatal("new source bypassed pin-before-decode", err)
	}
	changed := false
	for _, p := range f.metadata.AsMetadataV14.Pallets {
		if p.Name != "AdminUtils" {
			continue
		}
		entry := f.metadata.AsMetadataV14.EfficientLookup[p.Calls.Type.Int64()]
		for i := range entry.Def.Variant.Variants {
			v := &entry.Def.Variant.Variants[i]
			if v.Name == "sudo_set_recycle_or_burn" {
				v.Index = 99
				changed = true
			}
		}
	}
	if !changed {
		t.Fatal("missing admin call mutation target")
	}
	encoded := nativeOwnerTestMetadata(t, f.metadata)
	raw, _ := hex.DecodeString(encoded[2:])
	action.Policy.RuntimeMetadataHash = rootExtrinsicHash(raw)
	if _, err := prepareOwnerRecycleAction(action, encoded); err == nil {
		t.Fatal("new source authorized changed owner dispatch")
	}
}

// The existing-seat observer authenticates the successor without gaining writes.
func TestRootPassiveRuntimeSuccessorKeepsReadOnlyScope(t *testing.T) {
	client, f := newRootPassiveFixture(t)
	f.policy.RuntimeSourceCommit = crv4.NativeOwnerSource473
	f.policy.RuntimeVersion.SpecVersion = 473
	f.version = f.policy.RuntimeVersion
	preview, err := client.readRootPreview(t.Context(), f.policy, rootObjectHash(f.policy))
	if err != nil || !preview.ReadOnlyReady || preview.ActivationReady || preview.RuntimeSourceCommit != crv4.NativeOwnerSource473 || preview.RuntimeVersion != f.version {
		t.Fatalf("successor passive observation lost its exact read scope: %+v %v", preview, err)
	}
	for method := range f.methodCounts {
		if strings.Contains(method, "submit") || strings.Contains(method, "send") {
			t.Fatal("passive successor exposed mutation", method)
		}
	}
}

// Discovery checks actual interfaces and retains unapproved status across upgrades.
func TestSubnetDiscoveryRuntimeSuccessorRemainsUnapproved(t *testing.T) {
	_, f, _, snapshot := newSubnetDiscoveryFixture(t)
	snapshot.Version.SpecVersion = 473
	snapshot.Identity.RuntimeSpec = 473
	f.policy.RuntimeVersion = snapshot.Version
	f.version = snapshot.Version
	server := rootFixtureServer(t, f)
	sealed, err := sealRuntimeSnapshot(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	path := subnetDiscoveryTestInput(t, sealed)
	var output, diagnostic bytes.Buffer
	if code := runMain(t.Context(), []string{"subnet-discover", "--rpc", server.URL, "--snapshot", path}, &output, &diagnostic); code != 0 {
		t.Fatal("compatible successor discovery rejected", code, diagnostic.String())
	}
	var envelope subnetDiscoveryEnvelope
	if err := json.Unmarshal(output.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	d := envelope.Discovery
	if !d.MembershipComplete || d.RuntimeVersion != snapshot.Version || d.Admission != "unapproved_observation" || d.RuntimeSourceProven || d.ApplyAuthority || d.ResetReady {
		t.Fatal("successor discovery inferred authority or changed the artifact", d)
	}
}

// The source-based capability report remains a plan, including for new artifacts.
func TestRootCapabilitiesRuntimeSuccessorRemainsPlanningOnly(t *testing.T) {
	_, metadata, digest, fileHash := rootCurrentTestMetadata(t)
	report, err := inspectRootCurrentCapabilities(metadata, digest, fileHash, crv4.NativeOwnerSource473)
	if err != nil || !report.MetadataInterfaceCompatible || !report.PlanningOnly || report.RuntimeSourceProven || report.CurrentRuntimeVerified || report.ActivationReady || report.NativeSigning {
		t.Fatal("successor report invented current authority", report, err)
	}
	if _, err := inspectRootCurrentCapabilities(metadata, digest, fileHash, strings.Repeat("b", 40)); err == nil {
		t.Fatal("unknown source acquired current root semantics")
	}
}

// Qualification may supply a retained public protocol artifact outside the repo.
// Its exact independently selected pin is required; this test grants no authority.
func TestNativeOwnerRetainedRuntimeMetadataCapabilities(t *testing.T) {
	path := os.Getenv("SN_NATIVE_OWNER_METADATA_FILE")
	digest := os.Getenv("SN_NATIVE_OWNER_METADATA_BLAKE2B256")
	if path == "" && digest == "" {
		t.Skip("retained native protocol metadata was not selected")
	}
	if path == "" || !rootCanonicalHash(digest) {
		t.Fatal("retained metadata needs both an exact path and independent pin")
	}
	raw, _, err := readPlanFile(t.Context(), path, maxMetadataRpcReplyBytes)
	if err != nil {
		t.Fatal(err)
	}
	metadata, _, err := nativePinnedMetadata("0x"+hex.EncodeToString(raw), digest)
	if err != nil {
		t.Fatal(err)
	}
	if err := nativeSigningProfileForSignature(metadata, "Ed25519", 0); err != nil {
		t.Fatal("native signing interface changed", err)
	}
	if _, err := ownerRecycleCall(metadata); err != nil {
		t.Fatal("owner dispatch interface changed", err)
	}
	if _, err := rootPassiveStorageMetadata(metadata); err != nil {
		t.Fatal("passive root interface changed", err)
	}
	f := newTreasuryFixture(t)
	for _, kind := range []string{"register_limit", "remove_stake_limit", "transfer_keep_alive"} {
		for _, operation := range []string{"as_multi", "approve_as_multi", "cancel_as_multi"} {
			action := f.input.Action
			action.Inner.Kind, action.Operation = kind, operation
			if _, err := treasuryMetadataProfile(metadata, action); err != nil {
				t.Fatal("treasury interface changed", kind, operation, err)
			}
		}
	}
}
