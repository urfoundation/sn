// The exact runtime-475 metadata, captured read-only from Finney, must expose
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

const runtime475TestMetadataPath = "../crv4/testdata/runtime475-metadata.scale.gz.base64"
const runtime475TestMetadataSize = 357468
const runtime475TestMetadataSha256 = "e181ddacd13d1050e82a2afea8e58ba5d3fc84504fa92d2884c91df8c9dd8020"
const runtime475TestMetadataHash = "0x983cfdabc62b0c6b08faafb47f24303999236e0022598d1b5a3ec70fbde895ff"

// Decompression is bounded and the exact size, SHA-256 and BLAKE2b-256 pins are
// checked before any interface is interpreted.
func runtime475TestMetadata(t *testing.T) ([]byte, *types.Metadata) {
	t.Helper()
	encoded, err := os.ReadFile(runtime475TestMetadataPath)
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
	raw, err := io.ReadAll(io.LimitReader(reader, runtime475TestMetadataSize+1))
	if err != nil || len(raw) != runtime475TestMetadataSize {
		t.Fatalf("runtime475 metadata size %d: %v", len(raw), err)
	}
	if rest, err := io.ReadAll(reader); err != nil || len(rest) != 0 || reader.Close() != nil {
		t.Fatal("runtime475 metadata fixture has trailing or malformed data", err)
	}
	if digest := sha256.Sum256(raw); hex.EncodeToString(digest[:]) != runtime475TestMetadataSha256 {
		t.Fatal("runtime475 metadata SHA-256 differs from its review pin")
	}
	metadata, hash, err := nativePinnedMetadata("0x"+hex.EncodeToString(raw), runtime475TestMetadataHash)
	if err != nil || hash != runtime475TestMetadataHash {
		t.Fatal("runtime475 metadata BLAKE2b-256 differs from its review pin", hash, err)
	}
	return raw, metadata
}

// The pinned bytes, not a version number, select every reviewed interface.
func TestNativeOwnerRuntime475MetadataCapabilities(t *testing.T) {
	raw, metadata := runtime475TestMetadata(t)
	nativeOwnerMetadataCapabilityChecks(t, metadata)
	nativeOwnerMetadataExtendedChecks(t, metadata)
	digest := sha256.Sum256(raw)
	report, err := inspectRootCurrentCapabilities(metadata, rootExtrinsicHash(raw), "sha256:"+hex.EncodeToString(digest[:]), crv4.NativeOwnerSource475)
	if err != nil || !report.MetadataInterfaceCompatible || !report.PlanningOnly || report.RuntimeSourceProven || report.CurrentRuntimeVerified || report.ActivationReady || report.NativeSigning {
		t.Fatal("runtime475 root capability report changed or invented authority", report, err)
	}
	if _, err := inspectRootCurrentCapabilities(metadata, rootExtrinsicHash(raw), "sha256:"+hex.EncodeToString(digest[:]), strings.Repeat("c", 40)); err == nil {
		t.Fatal("unreviewed source interpreted runtime475 root capabilities")
	}
}

// The two consumed items that changed shape or default in 475 keep the exact
// encodings SN reads; the census takes the new default from authenticated metadata.
func TestNativeOwnerRuntime475ChangedConsumedItems(t *testing.T) {
	_, metadata := runtime475TestMetadata(t)
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
	if names["RecycleOrBurn"] != 24 || names["EpochConsensus"] != 35 || names["BurnRegistrationAllowed"] != 36 || len(names) != 37 {
		t.Fatal("runtime475 hyperparameter enum differs from the reviewed append-only change", names)
	}
	entries, err := observationStorageProfile(metadata, subnetStorageSpecs)
	if err != nil {
		t.Fatal("subnet census storage profile changed", err)
	}
	if pow, burn := entries["NetworkPowRegistrationAllowed"], entries["NetworkRegistrationAllowed"]; !bytes.Equal(pow.Fallback, []byte{0}) || !bytes.Equal(burn.Fallback, []byte{1}) {
		t.Fatal("runtime475 registration defaults differ from review", pow.Fallback, burn.Fallback)
	}
}

// Additional consumed profiles; each passes on the reviewed 473 metadata too.
func nativeOwnerMetadataExtendedChecks(t *testing.T, metadata *types.Metadata) {
	t.Helper()
	if call, err := ownerRecycleCall(metadata); err != nil || call != [2]byte{19, 80} {
		t.Fatal("owner recycle dispatch moved", call, err)
	}
	if _, err := ownerRecycleRateEntry(metadata); err != nil {
		t.Fatal("owner recycle rate key changed", err)
	}
	if _, err := ownerRecycleEntries(metadata, "0x"+strings.Repeat("ab", 32)); err != nil {
		t.Fatal("owner recycle storage profile changed", err)
	}
	if err := nativeSigningProfile(metadata); err != nil {
		t.Fatal("Sr25519 native signing profile changed", err)
	}
	if _, err := economicEmissionEventProfile(metadata); err != nil {
		t.Fatal("economic emission events changed", err)
	}
	if _, err := rootReceiptEvents(metadata); err != nil {
		t.Fatal("native receipt events changed", err)
	}
	if _, err := rootRegisterReceiptEvents(metadata); err != nil {
		t.Fatal("root registration receipt events changed", err)
	}
	if err := rootAccountProfile(metadata); err != nil {
		t.Fatal("System.Account profile changed", err)
	}
	if call, err := rootRegisterCall(metadata); err != nil || call != [2]byte{7, 62} {
		t.Fatal("root_register dispatch moved", call, err)
	}
	if deposit, err := rootRegisterExistentialDeposit(metadata); err != nil || deposit != 500 {
		t.Fatal("existential deposit changed", deposit, err)
	}
	if _, err := observationStorageProfile(metadata, rootRegisterStorageSpecs()); err != nil {
		t.Fatal("root registration storage profile changed", err)
	}
	if _, err := observationStorageProfile(metadata, subnetStorageSpecs); err != nil {
		t.Fatal("subnet census storage profile changed", err)
	}
	if _, err := subnetMaximumImmunePercentage(metadata); err != nil {
		t.Fatal("immune owner percentage constant changed", err)
	}
	if _, err := subnetOwnerTrimCall(metadata); err != nil {
		t.Fatal("owner trim call changed", err)
	}
	if _, err := ownerTrimProxyEntry(metadata); err != nil {
		t.Fatal("Proxy.Proxies profile changed", err)
	}
	if _, err := ownerTrimWindowMetadata(metadata); err != nil {
		t.Fatal("owner trim window metadata changed", err)
	}
	if err := ownerTrimMultisigNestedProfile(metadata); err != nil {
		t.Fatal("owner trim multisig profile changed", err)
	}
	if _, err := nativeExecutionDrainKeys(metadata, 25); err != nil {
		t.Fatal("native execution drain keys changed", err)
	}
	if _, _, err := recycleModeStorage(metadata, 25); err != nil {
		t.Fatal("RecycleOrBurn storage changed", err)
	}
	if _, err := rootSystemEntry(metadata, "Account"); err != nil {
		t.Fatal("System.Account entry changed", err)
	}
}

// A runtime-475 treasury approval also opens only on a proof, at its own
// original activation, that the Alpha migration completed; 473 authority and a
// historical 470 authority cannot be relabeled to it.
func TestNativeTreasuryMigrationRuntime475RequiresCompletionProof(t *testing.T) {
	authority, policy := nativeMigrationTestAuthorityFor(t, crv4.NativeOwnerSource475, 475)
	if err := authority.validateScope(policy, policy.Through, nil); err != nil {
		t.Fatal("completed runtime475 opening with fresh approval refused", err)
	}
	if nativeExecutionRuntimeSource(authority) != crv4.NativeOwnerSource475 {
		t.Fatal("producer lost the independently approved runtime475 source")
	}
	missing := *authority
	missing.MigrationComplete = nil
	if err := missing.validateScope(policy, policy.Through, nil); err == nil {
		t.Fatal("runtime475 acquired economic authority without a completion proof")
	}
	previous, previousPolicy := nativeMigrationTestAuthority(t)
	if err := previous.validateScope(policy, policy.Through, nil); err == nil {
		t.Fatal("runtime473 approval served the runtime475 policy")
	}
	if err := authority.validateScope(previousPolicy, previousPolicy.Through, nil); err == nil {
		t.Fatal("runtime475 approval served the runtime473 policy")
	}
	for index, marker := range [][]byte{nil, {}, {0}, {1, 0}, {2}} {
		witness, code := nativeMigrationTestWitness(t, marker)
		incomplete := &nativeTreasuryAuthority{MigrationComplete: &witness, Deployment: nativeTreasuryDeployment{Activation: economicEmissionBoundary{Number: 703, Hash: witness.At}}}
		if err := incomplete.validateMigration(validator.OwnerRecycleRuntimePin{SourceCommit: crv4.NativeOwnerSource475, CodeHash: code}); err == nil {
			t.Fatal("incomplete original state accepted for runtime475", index)
		}
	}
	witness, code := nativeMigrationTestWitness(t, []byte{1})
	complete := &nativeTreasuryAuthority{MigrationComplete: &witness, Deployment: nativeTreasuryDeployment{Activation: economicEmissionBoundary{Number: 703, Hash: witness.At}}}
	if err := complete.validateMigration(validator.OwnerRecycleRuntimePin{SourceCommit: crv4.NativeOwnerSource475, CodeHash: code}); err != nil {
		t.Fatal("bounded runtime475 completion proof refused", err)
	}
	if err := complete.validateMigration(validator.OwnerRecycleRuntimePin{SourceCommit: crv4.NativeOwnerSource470, CodeHash: code}); err == nil {
		t.Fatal("historical runtime470 authority inherited a migration completion")
	}
	wrongCode := code
	wrongCode[0] ^= 1
	if err := complete.validateMigration(validator.OwnerRecycleRuntimePin{SourceCommit: crv4.NativeOwnerSource475, CodeHash: wrongCode}); err == nil {
		t.Fatal("runtime475 completion proof served another runtime artifact")
	}
}

// Root registration admits 475 only on its own spec number. A policy or
// observation sealed for 473 never serves the 475 registration domain.
func TestRootRegisterRuntime475PolicyAndEligibility(t *testing.T) {
	f := newRootRegisterTestFixture(t, false)
	base := f.config.Action.Policy
	successor := base
	successor.RuntimeSourceCommit, successor.RuntimeVersion.SpecVersion = crv4.NativeOwnerSource475, 475
	if err := successor.validate(); err != nil {
		t.Fatal("reviewed runtime475 registration policy refused", err)
	}
	for _, mismatch := range []struct {
		source string
		spec   uint32
	}{
		{crv4.NativeOwnerSource475, 473}, {crv4.NativeOwnerSource473, 475}, {crv4.NativeOwnerSource470, 470},
		{crv4.NativeOwnerSource470, 475}, {strings.Repeat("d", 40), 475}, {crv4.NativeOwnerSource475, 476},
	} {
		policy := base
		policy.RuntimeSourceCommit, policy.RuntimeVersion.SpecVersion = mismatch.source, mismatch.spec
		if err := policy.validate(); err == nil {
			t.Fatal("registration source and spec were not bound together", mismatch.source, mismatch.spec)
		}
	}
	observation := f.input.Observation
	if _, err := observation.eligibility(successor, f.metadata); err == nil {
		t.Fatal("runtime473 observation seal served the runtime475 policy")
	}
	observation.PolicyHash = rootObjectHash(successor)
	observation.ContentHash = ""
	observation.ContentHash = rootObjectHash(observation)
	eligibility, err := observation.eligibility(successor, f.metadata)
	if err != nil || !eligibility.Eligible || eligibility.ExistingSeat != nil || eligibility.BurnRao != 10 || eligibility.ConservativeReducibleRao != 99500 {
		t.Fatalf("runtime475 free root slot differs from the shared reviewed rules: %+v %v", eligibility, err)
	}
	_, metadata, digest, fileHash := rootCurrentTestMetadata(t)
	report, err := inspectRootCurrentCapabilities(metadata, digest, fileHash, crv4.NativeOwnerSource475)
	if err != nil {
		t.Fatal(err)
	}
	registration := rootCurrentTestCall(t, report, "root_register")
	if !strings.Contains(registration.MutationAdapter, "requires-separate-475-policy") || !strings.Contains(report.MutationExecution, "separate-475-root-register-domain") ||
		report.NativeSigning || report.NetworkEffects || report.ActivationReady || report.CurrentRuntimeVerified || !report.PlanningOnly {
		t.Fatal("runtime475 registration catalogue lost its separate execution boundary", registration, report)
	}
}
