// Runtime458 source provenance and artifact admission preserve historical
// decoding without inheriting a tag or mainnet proposal from runtime454.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/crv4"
	"gopkg.in/yaml.v3"
)

// Independent literals make a changed source constant fail the admission test.
func runtime458ReviewedTestLock() *ReleaseLock {
	return &ReleaseLock{SchemaVersion: 1, Release: "1.0", Runtime: ReleaseRuntimeLock{
		SourceRepository: "https://github.com/RaoFoundation/subtensor",
		SourceRefKind:    "commit", SourceRefName: "a7ae07e5dd37b552f27aa8e4d7716c522eef9aa7",
		SourceCommit:         "a7ae07e5dd37b552f27aa8e4d7716c522eef9aa7",
		CodeHash:             "0x2fdb28e5c3fe4e79844b25dee09ed960e90004432ea2bd98079aba4c5530c51a",
		MetadataHash:         "0x040088e73e34ed5561372aa51b07b56e41cf7f390312837b074434f30452593d",
		CompressedWasmSHA256: "0xd763c0210bbd113c065a4e8d538cdd3f5e9b40ba259a5136b77e0a495c364241",
		SpecVersion:          458, TransactionVersion: 1, StateVersion: 1,
	}}
}

// Pin459 independently while retaining the exact458 helper for archive controls.
func runtime459ReviewedTestLock() *ReleaseLock {
	return &ReleaseLock{SchemaVersion: 1, Release: "1.0", Runtime: ReleaseRuntimeLock{
		SourceRepository: "https://github.com/RaoFoundation/subtensor",
		SourceRefKind:    "commit", SourceRefName: "70378404b56c12a85bc8cd163aca2f32cf4d1b80",
		SourceCommit:         "70378404b56c12a85bc8cd163aca2f32cf4d1b80",
		CodeHash:             "0x558275958401c026fa4a4159466d49eabd08c761f0c801390593fcba91dee69b",
		MetadataHash:         "0xcf97fac54fee756137f42e53deeeca828959a74c6d87274898db2c36a33c4fef",
		CompressedWasmSHA256: "0xc78bef5489149655254d5fb01a0e8c5c61846b0b322a54cb9ca2c86a14df8284",
		SpecVersion:          459, TransactionVersion: 1, StateVersion: 1,
	}}
}

// Exact commit provenance cannot be expressed as a mutable branch, invented
// release tag or copied mainnet multisig proposal/timepoint.
func TestRuntime461CurrentLockSeparatesCommitFromMainnetProposal(t *testing.T) {
	lock := runtime461ReviewedTestLock()
	if err := validateReviewedRuntimeIdentity(lock); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*ReleaseRuntimeLock){
		func(value *ReleaseRuntimeLock) { value.SourceRefKind = "branch" },
		func(value *ReleaseRuntimeLock) { value.SourceRefName = "testnet" },
		func(value *ReleaseRuntimeLock) { value.SourceRefKind, value.SourceRefName = "", "" },
		func(value *ReleaseRuntimeLock) { value.SourceTag = "v459" },
		func(value *ReleaseRuntimeLock) { value.SourceCommit = "14cde6410fe8ec81a940e290c56f94a632a0988d" },
		func(value *ReleaseRuntimeLock) {
			value.UpstreamReleaseCallHash = "0xa555b212406469b24d3a370ac59675bad303e319274ebd7a7fd0804dede3315b"
		},
		func(value *ReleaseRuntimeLock) { value.UpstreamReleaseTimepoint = "8996567:7" },
	} {
		mutated := *lock
		mutate(&mutated.Runtime)
		if err := validateReviewedRuntimeIdentity(&mutated); err == nil {
			t.Fatalf("unreviewed/fabricated provenance was accepted: %+v", mutated.Runtime)
		}
	}
}

// The new optional reference fields round-trip current locks and stay absent
// from historical bytes. Old tag/proposal fields retain their literal meaning.
func TestRuntime458CanonicalLockRetainsHistoricalProvenance(t *testing.T) {
	current := runtime458ReviewedTestLock()
	historical := &ReleaseLock{SchemaVersion: 1, Release: "1.0", Runtime: ReleaseRuntimeLock{
		SourceRepository: "https://github.com/RaoFoundation/subtensor", SourceTag: "v454",
		SourceCommit:            "14cde6410fe8ec81a940e290c56f94a632a0988d",
		CodeHash:                "0x725e3d1eca8d5c29c1f0fa6476d5360661b852f52aebad979d6636e227a431ef",
		MetadataHash:            "0x4d17516b694ef8d18f8a565dcb2df0117e7a0018a3ffa40812c91a1621225702",
		CompressedWasmSHA256:    "0xa55e76b4f4620bcdb4c787e499c87a35abb9913ba4cde001b08a00d1945ac4db",
		UpstreamReleaseCallHash: "0x5a1c30f0387796da59522d4b84a71395533a4ee676e06c52eedb14262ae9c3c6", UpstreamReleaseTimepoint: "8996567:7",
		SpecVersion: 454, TransactionVersion: 1, StateVersion: 1,
	}}
	for _, lock := range []*ReleaseLock{current, historical} {
		raw, err := yaml.Marshal(lock)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "release.lock.yml")
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		var decoded ReleaseLock
		if err := strictYAML(path, &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded.Runtime != lock.Runtime {
			t.Fatalf("runtime provenance changed through strict decoding: %+v", decoded.Runtime)
		}
		encoded, err := json.Marshal(decoded.Runtime)
		if err != nil {
			t.Fatal(err)
		}
		if lock == historical && (bytes.Contains(raw, []byte("source_ref_")) || bytes.Contains(encoded, []byte("source_ref_"))) {
			t.Fatal("empty new source fields changed historical wire shape")
		}
	}
	if err := validateReviewedRuntimeIdentity(historical); err == nil {
		t.Fatal("historical454 lock was reinterpreted as current458")
	}
}

// Read-only public history admits each exact reviewed predecessor. The same
// object cannot become the current launch identity, even with a mutated config.
func TestRuntime461HistoricalPublicationsRemainEvidenceOnly(t *testing.T) {
	cfg := testResolvedConfig(t)
	artifacts, err := releaseHistoryRuntimeArtifacts(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != len(crv4.ReviewedRuntimeArtifacts()) || artifacts[0].Version.SpecVersion != 461 {
		t.Fatal("current461 plus complete451–455/458/459/460 history is absent")
	}
	for _, artifact := range artifacts {
		public := &PublicDeploymentManifest{RuntimeSpec: artifact.Version.SpecVersion, TransactionVersion: artifact.Version.TransactionVersion, StateVersion: artifact.Version.StateVersion, RuntimeCodeHash: artifact.CodeHash, RuntimeMetadataHash: artifact.MetadataHash}
		if err := validatePublishedRuntimeIdentityShape(public); err != nil {
			t.Fatalf("reviewed history%d refused: %v", artifact.Version.SpecVersion, err)
		}
		currentErr := validatePublishedRuntimeIdentity(public, cfg)
		if artifact.Version.SpecVersion == 461 {
			if currentErr != nil {
				t.Fatal(currentErr)
			}
			continue
		}
		if currentErr == nil {
			t.Fatalf("historical%d became current authority", artifact.Version.SpecVersion)
		}
		historicalCfg, historicalLock, historicalPublic := *cfg, *cfg.Release, *cfg.Public
		historicalLock.Runtime.SpecVersion, historicalLock.Runtime.CodeHash, historicalLock.Runtime.MetadataHash = artifact.Version.SpecVersion, artifact.CodeHash, artifact.MetadataHash
		historicalPublic.Chain.ExpectedRuntimeSpec = artifact.Version.SpecVersion
		historicalPublic.Chain.ConfigIdentityRuntimeSpec = 0
		historicalCfg.Release, historicalCfg.Public = &historicalLock, &historicalPublic
		if err := validatePublishedRuntimeIdentity(public, &historicalCfg); err == nil {
			t.Fatal("mutated historical config bypassed current lock admission")
		}
	}
}

// Version selection never permits another reviewed artifact's code or metadata
// hash, a future version, or a changed transaction/state domain.
func TestRuntime458PublicationsRejectCrossArtifactPairs(t *testing.T) {
	artifacts, err := releaseHistoryRuntimeArtifacts(testResolvedConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	for index, artifact := range artifacts {
		public := PublicDeploymentManifest{RuntimeSpec: artifact.Version.SpecVersion, TransactionVersion: 1, StateVersion: 1, RuntimeCodeHash: artifact.CodeHash, RuntimeMetadataHash: artifact.MetadataHash}
		for otherIndex, other := range artifacts {
			if otherIndex == index {
				continue
			}
			wrongCode, wrongMetadata := public, public
			wrongCode.RuntimeCodeHash, wrongMetadata.RuntimeMetadataHash = other.CodeHash, other.MetadataHash
			if validatePublishedRuntimeIdentityShape(&wrongCode) == nil || validatePublishedRuntimeIdentityShape(&wrongMetadata) == nil {
				t.Fatalf("artifact%d accepted hashes from%d", artifact.Version.SpecVersion, other.Version.SpecVersion)
			}
		}
		for _, mutate := range []func(*PublicDeploymentManifest){
			func(value *PublicDeploymentManifest) { value.RuntimeSpec = 456 },
			func(value *PublicDeploymentManifest) { value.TransactionVersion = 2 },
			func(value *PublicDeploymentManifest) { value.StateVersion = 2 },
		} {
			mutated := public
			mutate(&mutated)
			if err := validatePublishedRuntimeIdentityShape(&mutated); err == nil {
				t.Fatal("unreviewed public runtime domain was accepted")
			}
		}
	}
}

// The source-linked CI build has different compile-time seeds. Only the exact
// LAN artifact is current authority; source similarity cannot normalize hashes.
func TestRuntime458HistoricalLockRejectsOfficialSeedVariant(t *testing.T) {
	lock := testReleaseLockFixture(t)
	runtimeImage := lock.Runtime.Image
	lock.Runtime = runtime458ReviewedTestLock().Runtime
	lock.Runtime.Image = runtimeImage
	if err := validateValidatorEvidenceHistoricalReleaseLock(lock); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*ReleaseRuntimeLock){
		func(value *ReleaseRuntimeLock) {
			value.CodeHash = "0x3708442dc6aae2ea654d827d8b9985d36b6640b2447cfd48125a1a0205c8f1d3"
		},
		func(value *ReleaseRuntimeLock) {
			value.CompressedWasmSHA256 = "0x94e85d3d0ca077a8a8f8e1e65edfe18036895a20313f7195bb17060b145610c6"
		},
	} {
		changed := *lock
		mutate(&changed.Runtime)
		if validateValidatorEvidenceHistoricalReleaseLock(&changed) == nil {
			t.Fatal("source-equivalent seed variant acquired exact current authority")
		}
	}
	public := PublicDeploymentManifest{RuntimeSpec: 458, TransactionVersion: 1, StateVersion: 1,
		RuntimeCodeHash: "0x3708442dc6aae2ea654d827d8b9985d36b6640b2447cfd48125a1a0205c8f1d3", RuntimeMetadataHash: lock.Runtime.MetadataHash}
	if validatePublishedRuntimeIdentityShape(&public) == nil {
		t.Fatal("official seed variant became a reviewed public artifact")
	}
}

// The actual release-history constructor must reach the artifact reader with
// all seven authorities; a permanent synthetic read error stops before metadata.
func TestRuntime458HistoryAllowlistReachesArtifactReader(t *testing.T) {
	cfg := testResolvedConfig(t)
	want := errors.New("synthetic exact runtime read boundary")
	calls := 0
	block := types.Hash{19}
	client := &releaseRuntimeTestClient{callContext: func(_ context.Context, _ any, method string, args ...any) error {
		calls++
		if method != "state_getRuntimeVersion" || len(args) != 1 || args[0] != block.Hex() {
			return errors.New("release history changed the synthetic exact read")
		}
		return want
	}}
	chain := &crv4.Chain{API: &gsrpc.SubstrateAPI{Client: client}}
	_, err := readReleaseHistoryRuntimeMetadataAtContext(context.Background(), chain, cfg, block)
	if calls != 1 || !errors.Is(err, want) {
		t.Fatalf("complete release history did not reach its exact artifact reader: calls=%d error=%v", calls, err)
	}
}
