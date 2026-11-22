// The exact runtime-478 metadata, captured read-only from Finney, must expose
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

const runtime478TestMetadataPath = "../crv4/testdata/runtime478-metadata.scale.gz.base64"
const runtime478TestMetadataSize = 357499
const runtime478TestMetadataSha256 = "2513780705c9e9c1b9fb7e701670fee88cf8419b945100fc26ab87218b03e35a"
const runtime478TestMetadataHash = "0x4f96a9cb954ebe3ae0e364fe7e34ec8a0e902239832f68dfe90aaa1e4511c747"

// Decompression is bounded and the exact size, SHA-256 and BLAKE2b-256 pins are
// checked before any interface is interpreted.
func runtime478TestMetadata(t *testing.T) ([]byte, *types.Metadata) {
	t.Helper()
	encoded, err := os.ReadFile(runtime478TestMetadataPath)
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
	raw, err := io.ReadAll(io.LimitReader(reader, runtime478TestMetadataSize+1))
	if err != nil || len(raw) != runtime478TestMetadataSize {
		t.Fatalf("runtime478 metadata size %d: %v", len(raw), err)
	}
	if rest, err := io.ReadAll(reader); err != nil || len(rest) != 0 || reader.Close() != nil {
		t.Fatal("runtime478 metadata fixture has trailing or malformed data", err)
	}
	if digest := sha256.Sum256(raw); hex.EncodeToString(digest[:]) != runtime478TestMetadataSha256 {
		t.Fatal("runtime478 metadata SHA-256 differs from its review pin")
	}
	metadata, hash, err := nativePinnedMetadata("0x"+hex.EncodeToString(raw), runtime478TestMetadataHash)
	if err != nil || hash != runtime478TestMetadataHash {
		t.Fatal("runtime478 metadata BLAKE2b-256 differs from its review pin", hash, err)
	}
	return raw, metadata
}

// The pinned bytes, not a version number, select every reviewed interface.
func TestNativeOwnerRuntime478MetadataCapabilities(t *testing.T) {
	raw, metadata := runtime478TestMetadata(t)
	nativeOwnerMetadataCapabilityChecks(t, metadata)
	nativeOwnerMetadataExtendedChecks(t, metadata)
	digest := sha256.Sum256(raw)
	report, err := inspectRootCurrentCapabilities(metadata, rootExtrinsicHash(raw), "sha256:"+hex.EncodeToString(digest[:]), crv4.NativeOwnerSource478)
	if err != nil || !report.MetadataInterfaceCompatible || !report.PlanningOnly || report.RuntimeSourceProven || report.CurrentRuntimeVerified || report.ActivationReady || report.NativeSigning {
		t.Fatal("runtime478 root capability report changed or invented authority", report, err)
	}
	if _, err := inspectRootCurrentCapabilities(metadata, rootExtrinsicHash(raw), "sha256:"+hex.EncodeToString(digest[:]), strings.Repeat("c", 40)); err == nil {
		t.Fatal("unreviewed source interpreted runtime478 root capabilities")
	}
}

// 478 changes no interface: its metadata is 477's with one byte of the spec
// version changed, so every item SN consumes keeps the encoding 477's has.
func TestNativeOwnerRuntime478MetadataChangesOnlyTheSpecVersion(t *testing.T) {
	previous, _ := runtime477TestMetadata(t)
	raw, _ := runtime478TestMetadata(t)
	if len(raw) != len(previous) {
		t.Fatal("runtime478 metadata size differs from its predecessor's", len(raw), len(previous))
	}
	var differing []int
	for offset := range raw {
		if raw[offset] != previous[offset] {
			differing = append(differing, offset)
		}
	}
	// The spec version is a little-endian u32: dd 01 before, de 01 now.
	if len(differing) != 1 || previous[differing[0]] != 0xdd || raw[differing[0]] != 0xde || raw[differing[0]+1] != 0x01 {
		t.Fatal("runtime478 metadata differs from its predecessor's in more than the spec version", differing)
	}
}

// A runtime-478 treasury approval also opens only on a proof, at its own
// original activation, that the Alpha migration completed; its predecessor's
// authority and a historical 470 authority cannot be relabeled to it.
func TestNativeTreasuryMigrationRuntime478RequiresCompletionProof(t *testing.T) {
	authority, policy := nativeMigrationTestAuthorityFor(t, crv4.NativeOwnerSource478, 478)
	if err := authority.validateScope(policy, policy.Through, nil); err != nil {
		t.Fatal("completed runtime478 opening with fresh approval refused", err)
	}
	if nativeExecutionRuntimeSource(authority) != crv4.NativeOwnerSource478 {
		t.Fatal("producer lost the independently approved runtime478 source")
	}
	missing := *authority
	missing.MigrationComplete = nil
	if err := missing.validateScope(policy, policy.Through, nil); err == nil {
		t.Fatal("runtime478 acquired economic authority without a completion proof")
	}
	previous, previousPolicy := nativeMigrationTestAuthorityFor(t, crv4.NativeOwnerSource477, 477)
	if err := previous.validateScope(policy, policy.Through, nil); err == nil {
		t.Fatal("the predecessor's approval served the runtime478 policy")
	}
	if err := authority.validateScope(previousPolicy, previousPolicy.Through, nil); err == nil {
		t.Fatal("runtime478 approval served its predecessor's policy")
	}
	for index, marker := range [][]byte{nil, {}, {0}, {1, 0}, {2}} {
		witness, code := nativeMigrationTestWitness(t, marker)
		incomplete := &nativeTreasuryAuthority{MigrationComplete: &witness, Deployment: nativeTreasuryDeployment{Activation: economicEmissionBoundary{Number: 703, Hash: witness.At}}}
		if err := incomplete.validateMigration(validator.OwnerRecycleRuntimePin{SourceCommit: crv4.NativeOwnerSource478, CodeHash: code}); err == nil {
			t.Fatal("incomplete original state accepted for runtime478", index)
		}
	}
	witness, code := nativeMigrationTestWitness(t, []byte{1})
	complete := &nativeTreasuryAuthority{MigrationComplete: &witness, Deployment: nativeTreasuryDeployment{Activation: economicEmissionBoundary{Number: 703, Hash: witness.At}}}
	if err := complete.validateMigration(validator.OwnerRecycleRuntimePin{SourceCommit: crv4.NativeOwnerSource478, CodeHash: code}); err != nil {
		t.Fatal("bounded runtime478 completion proof refused", err)
	}
	if err := complete.validateMigration(validator.OwnerRecycleRuntimePin{SourceCommit: crv4.NativeOwnerSource470, CodeHash: code}); err == nil {
		t.Fatal("historical runtime470 authority inherited a migration completion")
	}
	wrongCode := code
	wrongCode[0] ^= 1
	if err := complete.validateMigration(validator.OwnerRecycleRuntimePin{SourceCommit: crv4.NativeOwnerSource478, CodeHash: wrongCode}); err == nil {
		t.Fatal("runtime478 completion proof served another runtime artifact")
	}
}

// Root registration admits 478 only on its own spec number. A policy or
// observation sealed for 473 never serves the 478 registration domain.
func TestRootRegisterRuntime478PolicyAndEligibility(t *testing.T) {
	f := newRootRegisterTestFixture(t, false)
	base := f.config.Action.Policy
	successor := base
	successor.RuntimeSourceCommit, successor.RuntimeVersion.SpecVersion = crv4.NativeOwnerSource478, 478
	if err := successor.validate(); err != nil {
		t.Fatal("reviewed runtime478 registration policy refused", err)
	}
	for _, mismatch := range []struct {
		source string
		spec   uint32
	}{
		{crv4.NativeOwnerSource478, 477}, {crv4.NativeOwnerSource477, 478}, {crv4.NativeOwnerSource478, 473}, {crv4.NativeOwnerSource473, 478},
		{crv4.NativeOwnerSource470, 470}, {crv4.NativeOwnerSource470, 478}, {strings.Repeat("d", 40), 478}, {crv4.NativeOwnerSource478, 476}, {crv4.NativeOwnerSource478, 479},
	} {
		policy := base
		policy.RuntimeSourceCommit, policy.RuntimeVersion.SpecVersion = mismatch.source, mismatch.spec
		if err := policy.validate(); err == nil {
			t.Fatal("registration source and spec were not bound together", mismatch.source, mismatch.spec)
		}
	}
	observation := f.input.Observation
	if _, err := observation.eligibility(successor, f.metadata); err == nil {
		t.Fatal("runtime473 observation seal served the runtime478 policy")
	}
	observation.PolicyHash = rootObjectHash(successor)
	observation.ContentHash = ""
	observation.ContentHash = rootObjectHash(observation)
	eligibility, err := observation.eligibility(successor, f.metadata)
	if err != nil || !eligibility.Eligible || eligibility.ExistingSeat != nil || eligibility.BurnRao != 10 || eligibility.ConservativeReducibleRao != 99500 {
		t.Fatalf("runtime478 free root slot differs from the shared reviewed rules: %+v %v", eligibility, err)
	}
	_, metadata, digest, fileHash := rootCurrentTestMetadata(t)
	report, err := inspectRootCurrentCapabilities(metadata, digest, fileHash, crv4.NativeOwnerSource478)
	if err != nil {
		t.Fatal(err)
	}
	registration := rootCurrentTestCall(t, report, "root_register")
	if !strings.Contains(registration.MutationAdapter, "requires-separate-478-policy") || !strings.Contains(report.MutationExecution, "separate-478-root-register-domain") ||
		report.NativeSigning || report.NetworkEffects || report.ActivationReady || report.CurrentRuntimeVerified || !report.PlanningOnly {
		t.Fatal("runtime478 registration catalogue lost its separate execution boundary", registration, report)
	}
}
