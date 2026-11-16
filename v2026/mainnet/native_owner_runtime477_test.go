// The exact runtime-477 metadata, captured read-only from Finney, must expose
// every interface that a reviewed native owner source selects. The fixture
// carries no deployed identity, account state, signature, key or authority.
package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/validator"
)

const runtime477TestMetadataPath = "../crv4/testdata/runtime477-metadata.scale.gz.base64"
const runtime477TestMetadataSize = 357499
const runtime477TestMetadataSha256 = "1d9a056217f440400df497ad70d4a6a6148abc19871d1b28e52464c507c11260"
const runtime477TestMetadataHash = "0x5a268863a7f24c626e362103afa4f2856a589ee157ab915d91c289b1b5a6f11e"

// Decompression is bounded and the exact size, SHA-256 and BLAKE2b-256 pins are
// checked before any interface is interpreted.
func runtime477TestMetadata(t *testing.T) ([]byte, *types.Metadata) {
	t.Helper()
	encoded, err := os.ReadFile(runtime477TestMetadataPath)
	if err != nil {
		t.Fatal(err)
	}
	compressed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(encoded)))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatal(err)
	}
	reader.Multistream(false)
	raw, err := io.ReadAll(io.LimitReader(reader, runtime477TestMetadataSize+1))
	if err != nil || len(raw) != runtime477TestMetadataSize {
		t.Fatalf("runtime477 metadata size %d: %v", len(raw), err)
	}
	if rest, err := io.ReadAll(reader); err != nil || len(rest) != 0 || reader.Close() != nil {
		t.Fatal("runtime477 metadata fixture has trailing or malformed data", err)
	}
	if digest := sha256.Sum256(raw); hex.EncodeToString(digest[:]) != runtime477TestMetadataSha256 {
		t.Fatal("runtime477 metadata SHA-256 differs from its review pin")
	}
	metadata, hash, err := nativePinnedMetadata("0x"+hex.EncodeToString(raw), runtime477TestMetadataHash)
	if err != nil || hash != runtime477TestMetadataHash {
		t.Fatal("runtime477 metadata BLAKE2b-256 differs from its review pin", hash, err)
	}
	return raw, metadata
}

// The pinned bytes, not a version number, select every reviewed interface.
func TestNativeOwnerRuntime477MetadataCapabilities(t *testing.T) {
	raw, metadata := runtime477TestMetadata(t)
	nativeOwnerMetadataCapabilityChecks(t, metadata)
	nativeOwnerMetadataExtendedChecks(t, metadata)
	digest := sha256.Sum256(raw)
	report, err := inspectRootCurrentCapabilities(metadata, rootExtrinsicHash(raw), "sha256:"+hex.EncodeToString(digest[:]), crv4.NativeOwnerSource477)
	if err != nil || !report.MetadataInterfaceCompatible || !report.PlanningOnly || report.RuntimeSourceProven || report.CurrentRuntimeVerified || report.ActivationReady || report.NativeSigning {
		t.Fatal("runtime477 root capability report changed or invented authority", report, err)
	}
	if _, err := inspectRootCurrentCapabilities(metadata, rootExtrinsicHash(raw), "sha256:"+hex.EncodeToString(digest[:]), strings.Repeat("c", 40)); err == nil {
		t.Fatal("unreviewed source interpreted runtime477 root capabilities")
	}
}

// The one consumed item that changed shape in 477 keeps the exact encoding SN
// reads, and the registration defaults that 475 changed stay as reviewed.
func TestNativeOwnerRuntime477ChangedConsumedItems(t *testing.T) {
	_, metadata := runtime477TestMetadata(t)
	entry, err := ownerRecycleRateEntry(metadata)
	if err != nil {
		t.Fatal("RecycleOrBurn rate key no longer encodes as OwnerHyperparamUpdate(25, 24)", err)
	}
	key := metadata.AsMetadataV14.EfficientLookup[entry.Type.AsMap.Key.Int64()]
	names := map[string]uint8{}
	for _, variant := range key.Def.Variant.Variants {
		if variant.Name != "OwnerHyperparamUpdate" {
			continue
		}
		hyper := metadata.AsMetadataV14.EfficientLookup[variant.Fields[1].Type.Int64()]
		for _, parameter := range hyper.Def.Variant.Variants {
			names[string(parameter.Name)] = uint8(parameter.Index)
		}
	}
	if names["RecycleOrBurn"] != 24 || names["EpochConsensus"] != 35 || names["BurnRegistrationAllowed"] != 36 || names["MinDifficulty"] != 37 || names["Difficulty"] != 38 || len(names) != 39 {
		t.Fatal("runtime477 hyperparameter enum differs from the reviewed append-only change", names)
	}
	entries, err := observationStorageProfile(metadata, subnetStorageSpecs)
	if err != nil {
		t.Fatal("subnet census storage profile changed", err)
	}
	if pow, burn := entries["NetworkPowRegistrationAllowed"], entries["NetworkRegistrationAllowed"]; !bytes.Equal(pow.Fallback, []byte{0}) || !bytes.Equal(burn.Fallback, []byte{1}) {
		t.Fatal("runtime477 registration defaults differ from review", pow.Fallback, burn.Fallback)
	}
}

// A runtime-477 treasury approval also opens only on a proof, at its own
// original activation, that the Alpha migration completed; 475 authority and a
// historical 470 authority cannot be relabeled to it.
func TestNativeTreasuryMigrationRuntime477RequiresCompletionProof(t *testing.T) {
	authority, policy := nativeMigrationTestAuthorityFor(t, crv4.NativeOwnerSource477, 477)
	if err := authority.validateScope(policy, policy.Through, nil); err != nil {
		t.Fatal("completed runtime477 opening with fresh approval refused", err)
	}
	if nativeExecutionRuntimeSource(authority) != crv4.NativeOwnerSource477 {
		t.Fatal("producer lost the independently approved runtime477 source")
	}
	missing := *authority
	missing.MigrationComplete = nil
	if err := missing.validateScope(policy, policy.Through, nil); err == nil {
		t.Fatal("runtime477 acquired economic authority without a completion proof")
	}
	previous, previousPolicy := nativeMigrationTestAuthorityFor(t, crv4.NativeOwnerSource475, 475)
	if err := previous.validateScope(policy, policy.Through, nil); err == nil {
		t.Fatal("runtime475 approval served the runtime477 policy")
	}
	if err := authority.validateScope(previousPolicy, previousPolicy.Through, nil); err == nil {
		t.Fatal("runtime477 approval served the runtime475 policy")
	}
	for index, marker := range [][]byte{nil, {}, {0}, {1, 0}, {2}} {
		witness, code := nativeMigrationTestWitness(t, marker)
		incomplete := &nativeTreasuryAuthority{MigrationComplete: &witness, Deployment: nativeTreasuryDeployment{Activation: economicEmissionBoundary{Number: 703, Hash: witness.At}}}
		if err := incomplete.validateMigration(validator.OwnerRecycleRuntimePin{SourceCommit: crv4.NativeOwnerSource477, CodeHash: code}); err == nil {
			t.Fatal("incomplete original state accepted for runtime477", index)
		}
	}
	witness, code := nativeMigrationTestWitness(t, []byte{1})
	complete := &nativeTreasuryAuthority{MigrationComplete: &witness, Deployment: nativeTreasuryDeployment{Activation: economicEmissionBoundary{Number: 703, Hash: witness.At}}}
	if err := complete.validateMigration(validator.OwnerRecycleRuntimePin{SourceCommit: crv4.NativeOwnerSource477, CodeHash: code}); err != nil {
		t.Fatal("bounded runtime477 completion proof refused", err)
	}
	if err := complete.validateMigration(validator.OwnerRecycleRuntimePin{SourceCommit: crv4.NativeOwnerSource470, CodeHash: code}); err == nil {
		t.Fatal("historical runtime470 authority inherited a migration completion")
	}
	wrongCode := code
	wrongCode[0] ^= 1
	if err := complete.validateMigration(validator.OwnerRecycleRuntimePin{SourceCommit: crv4.NativeOwnerSource477, CodeHash: wrongCode}); err == nil {
		t.Fatal("runtime477 completion proof served another runtime artifact")
	}
}

// Root registration admits 477 only on its own spec number. A policy or
// observation sealed for 473 never serves the 477 registration domain.
func TestRootRegisterRuntime477PolicyAndEligibility(t *testing.T) {
	f := newRootRegisterTestFixture(t, false)
	base := f.config.Action.Policy
	successor := base
	successor.RuntimeSourceCommit, successor.RuntimeVersion.SpecVersion = crv4.NativeOwnerSource477, 477
	if err := successor.validate(); err != nil {
		t.Fatal("reviewed runtime477 registration policy refused", err)
	}
	for _, mismatch := range []struct {
		source string
		spec   uint32
	}{
		{crv4.NativeOwnerSource477, 475}, {crv4.NativeOwnerSource475, 477}, {crv4.NativeOwnerSource477, 473}, {crv4.NativeOwnerSource473, 477},
		{crv4.NativeOwnerSource470, 470}, {crv4.NativeOwnerSource470, 477}, {strings.Repeat("d", 40), 477}, {crv4.NativeOwnerSource477, 476}, {crv4.NativeOwnerSource477, 478},
	} {
		policy := base
		policy.RuntimeSourceCommit, policy.RuntimeVersion.SpecVersion = mismatch.source, mismatch.spec
		if err := policy.validate(); err == nil {
			t.Fatal("registration source and spec were not bound together", mismatch.source, mismatch.spec)
		}
	}
	observation := f.input.Observation
	if _, err := observation.eligibility(successor, f.metadata); err == nil {
		t.Fatal("runtime473 observation seal served the runtime477 policy")
	}
	observation.PolicyHash = rootObjectHash(successor)
	observation.ContentHash = ""
	observation.ContentHash = rootObjectHash(observation)
	eligibility, err := observation.eligibility(successor, f.metadata)
	if err != nil || !eligibility.Eligible || eligibility.ExistingSeat != nil || eligibility.BurnRao != 10 || eligibility.ConservativeReducibleRao != 99500 {
		t.Fatalf("runtime477 free root slot differs from the shared reviewed rules: %+v %v", eligibility, err)
	}
	_, metadata, digest, fileHash := rootCurrentTestMetadata(t)
	report, err := inspectRootCurrentCapabilities(metadata, digest, fileHash, crv4.NativeOwnerSource477)
	if err != nil {
		t.Fatal(err)
	}
	registration := rootCurrentTestCall(t, report, "root_register")
	if !strings.Contains(registration.MutationAdapter, "requires-separate-477-policy") || !strings.Contains(report.MutationExecution, "separate-477-root-register-domain") ||
		report.NativeSigning || report.NetworkEffects || report.ActivationReady || report.CurrentRuntimeVerified || !report.PlanningOnly {
		t.Fatal("runtime477 registration catalogue lost its separate execution boundary", registration, report)
	}
}
