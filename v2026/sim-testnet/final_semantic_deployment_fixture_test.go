package main

// final_semantic_deployment_fixture_test.go attaches a realistic approved
// deployment graph to the release-scale semantic fixture.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Attaches the fixture's canonical lock and complete approved-plan census to
// the synthetic evidence while retaining its real deterministic deployment.
func configureFinalSemanticFixtureDeploymentAnchors(t *testing.T, cfg *ResolvedConfig, source *FinalSemanticEvidence, artifacts map[string][]byte, plan *SetupPlan) {
	t.Helper()
	if cfg == nil || cfg.Release == nil || source == nil || plan == nil {
		t.Fatal("deployment anchor fixture context is incomplete")
	}
	lockData, err := os.ReadFile(filepath.Join("..", "deploy", "testnet", "release.lock.yml"))
	if err != nil {
		t.Fatal(err)
	}
	lock, err := decodeReleaseLockBytes(lockData)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateReleaseLockStatic(lock); err != nil {
		t.Fatal(err)
	}
	cfg.Release = lock
	lockHash, err := canonicalHashHex(lock)
	if err != nil {
		t.Fatal(err)
	}
	plan.ReleaseLockHash = lockHash
	plan.PlanHash = ""
	planHash, err := plan.hash()
	if err != nil {
		t.Fatal(err)
	}
	plan.PlanHash = planHash
	lockBytes, err := canonicalReleaseLockBytes(cfg.Release)
	if err != nil {
		t.Fatal(err)
	}
	lockURI := "final-derived/release-lock.yml"
	artifacts[lockURI] = append([]byte(nil), lockBytes...)
	source.ReleaseLockArtifact = FinalArtifactLocator{Kind: "release-lock", URI: lockURI, ContentHash: bytesSHA256(lockBytes), SizeBytes: uint64(len(lockBytes))}
	roots, err := finalReleaseRuntimeRootsForPlan(plan, cfg.Release)
	if err != nil {
		t.Fatal(err)
	}
	source.Deployment.RuntimeRoots = roots
}

// Rewrites the one synthetic terminal observation after the fixture has its
// final plan hash. Production construction performs the equivalent write from
// the closed archive, while this avoids stale placeholder artifact bytes.
func finalizeFinalSemanticFixtureDeploymentAnchorArtifact(t *testing.T, source *FinalSemanticEvidence, artifacts map[string][]byte, plan SetupPlan) {
	t.Helper()
	if source == nil {
		t.Fatal("nil deployment anchor fixture evidence")
	}
	runtimeHashes, err := finalReleaseRuntimeHashStrings(source.Deployment.RuntimeRoots)
	if err != nil {
		t.Fatal(err)
	}
	artifact := finalContractDeploymentArtifact{
		Deployment:                   plan.Deployment,
		Upgrade:                      plan.CoordinatorUpgrade,
		Terminal:                     source.Deployment.Snapshot,
		RuntimeCodeHashes:            runtimeHashes,
		Policy:                       PolicyView{},
		PlanHash:                     source.PlanHash,
		PlanDefaultMinTransferTaoRao: source.Deployment.PlanDefaultMinTransferTaoRao,
		ExpectedGuardian:             source.Deployment.CoordinatorGuardian,
		ExpectedCommitmentOracle:     source.Deployment.CoordinatorCommitmentOracle,
		Custody: ContractCustodyView{
			CoordinatorNetuid:                 source.Deployment.CoordinatorNetuid,
			CoordinatorSelfColdkey:            source.Deployment.CoordinatorSelfColdkey,
			CoordinatorVault:                  source.Deployment.CoordinatorSettlementVault,
			CoordinatorReserve:                source.Deployment.CoordinatorReserveSink,
			CoordinatorGuardian:               source.Deployment.CoordinatorGuardian,
			CoordinatorActiveGuardian:         source.Deployment.CoordinatorActiveGuardian,
			CoordinatorPaused:                 source.Deployment.CoordinatorPaused,
			CoordinatorCommitmentOracle:       source.Deployment.CoordinatorCommitmentOracle,
			CoordinatorActiveCommitmentOracle: source.Deployment.CoordinatorActiveCommitmentOracle,
			VaultCoordinator:                  source.Deployment.VaultCoordinator,
			VaultNetuid:                       source.Deployment.VaultNetuid,
			VaultSelfColdkey:                  source.Deployment.VaultSelfColdkey,
			VaultEscrowHotkey:                 source.Deployment.VaultEscrowHotkey,
			VaultEscrowRegistered:             source.Deployment.VaultEscrowRegistered,
			VaultMinimumClaimTTLBlocks:        source.Deployment.VaultMinimumClaimTTLBlocks,
			VaultMinimumTransferRao:           source.Deployment.VaultMinimumTransferTaoRao,
			ReserveRecorder:                   source.Deployment.ReserveRecorder,
			ReserveNetuid:                     source.Deployment.ReserveNetuid,
			ReserveSelfColdkey:                source.Deployment.ReserveSelfColdkey,
			ReserveHotkey:                     source.Deployment.ReserveHotkey,
		},
	}
	data, err := json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	uri := source.Deployment.Artifact.URI
	if uri == "" {
		uri = "final-derived/contract-deployment.json"
	}
	artifacts[uri] = append([]byte(nil), data...)
	source.Deployment.Artifact = FinalArtifactLocator{Kind: "contract-deployment", URI: uri, ContentHash: bytesSHA256(data), SizeBytes: uint64(len(data))}
	if !strings.EqualFold(artifact.Upgrade.Implementation.Hex(), source.Deployment.CoordinatorImplementation) {
		t.Fatalf("fixture upgrade implementation=%s does not match signed deployment=%s", artifact.Upgrade.Implementation.Hex(), source.Deployment.CoordinatorImplementation)
	}
}
