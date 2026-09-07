package main

// final_semantic_deployment_anchor_test.go exercises the closed plan/lock/
// bytecode graph independently of a live testnet transaction.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// Loads one complete approved graph in the same decoded form used by the
// artifact verifier. Every caller receives detached fixture maps and values.
func finalDeploymentAnchorInputs(t *testing.T) (FinalSemanticEvidence, map[string][]byte, *SetupPlan, *ReleaseLock, finalContractDeploymentArtifact) {
	t.Helper()
	source, artifacts := finalSemanticFixture(t)
	evidence, err := BuildFinalSemanticEvidence(source)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := decodePersistedPlanBytes(artifacts[evidence.PlanArtifact.URI])
	if err != nil {
		t.Fatal(err)
	}
	lock, err := decodeReleaseLockBytes(artifacts[evidence.ReleaseLockArtifact.URI])
	if err != nil {
		t.Fatal(err)
	}
	var deployment finalContractDeploymentArtifact
	if err := decodeStrictJSONBytes(artifacts[evidence.Deployment.Artifact.URI], &deployment); err != nil {
		t.Fatal(err)
	}
	if err := verifyFinalDeploymentRuntimeAnchors(evidence, plan, lock, deployment); err != nil {
		t.Fatalf("fixture deployment anchor is invalid: %v", err)
	}
	return *evidence, artifacts, plan, lock, deployment
}

// Finds one named root while preserving the fixed signed-order invariant.
func finalDeploymentRootIndex(t *testing.T, roots []FinalReleaseRuntimeRoot, name string) int {
	t.Helper()
	for index := range roots {
		if roots[index].Name == name {
			return index
		}
	}
	t.Fatalf("fixture runtime root %q is missing", name)
	return 0
}

// Copies an address-keyed observation before a negative test changes it.
func finalDeploymentRuntimeMapCopy(source map[string]string) map[string]string {
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

// Demonstrates that an archive reader agreeing with every substituted
// evidence/artifact map cannot authorize foreign runtime code.
func TestFinalSemanticDeploymentRejectsMutuallyConsistentSubstitutedRuntime(t *testing.T) {
	t.Parallel()
	evidence, _, plan, lock, deployment := finalDeploymentAnchorInputs(t)
	rootIndex := finalDeploymentRootIndex(t, evidence.Deployment.RuntimeRoots, "coordinator_proxy")
	foreign := finalTestHex(0xe1)
	evidence.Deployment.RuntimeRoots[rootIndex].RuntimeCodeHash = foreign
	evidence.Deployment.CoordinatorProxyCodeHash = foreign
	plan.Deployment.RuntimeHashes = finalDeploymentRuntimeMapCopy(plan.Deployment.RuntimeHashes)
	plan.Deployment.RuntimeHashes[plan.Deployment.CoordinatorProxy.Hex()] = foreign
	deployment.Deployment = plan.Deployment
	deployment.RuntimeCodeHashes = finalDeploymentRuntimeMapCopy(deployment.RuntimeCodeHashes)
	deployment.RuntimeCodeHashes[strings.ToLower(plan.Deployment.CoordinatorProxy.Hex())] = foreign
	reader := &finalTestChainReader{evidence: &evidence}
	state, _, err := reader.CoordinatorRuntime(context.Background(), evidence.EVMTerminalHead)
	if err != nil || !finalJSONEqual(state.RuntimeRoots, evidence.Deployment.RuntimeRoots) {
		t.Fatalf("substituted reader did not agree with altered runtime roots: state=%+v err=%v", state, err)
	}
	if err := verifyFinalDeploymentRuntimeAnchors(&evidence, plan, lock, deployment); err == nil || !strings.Contains(err.Error(), "exact linked release bytecode") {
		t.Fatalf("mutually consistent substituted runtime was accepted: %v", err)
	}
}

// Exercises the artifact entry point so a runtime map cannot be present in a
// valid JSON object yet silently ignored by final semantic verification.
func TestFinalSemanticDeploymentArtifactRejectsIgnoredRuntimeMap(t *testing.T) {
	t.Parallel()
	source, artifacts := finalSemanticFixture(t)
	var deployment finalContractDeploymentArtifact
	if err := decodeStrictJSONBytes(artifacts[source.Deployment.Artifact.URI], &deployment); err != nil {
		t.Fatal(err)
	}
	deployment.RuntimeCodeHashes = finalDeploymentRuntimeMapCopy(deployment.RuntimeCodeHashes)
	deployment.RuntimeCodeHashes["0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"] = finalTestHex(0xe2)
	wire, err := json.Marshal(deployment)
	if err != nil {
		t.Fatal(err)
	}
	artifacts[source.Deployment.Artifact.URI] = wire
	source.Deployment.Artifact.ContentHash = bytesSHA256(wire)
	source.Deployment.Artifact.SizeBytes = uint64(len(wire))
	evidence, err := BuildFinalSemanticEvidence(source)
	if err != nil {
		t.Fatal(err)
	}
	load := func(_ context.Context, locator FinalArtifactLocator) ([]byte, error) {
		value, ok := artifacts[locator.URI]
		if !ok {
			return nil, fmt.Errorf("missing fixture artifact %s", locator.URI)
		}
		return append([]byte(nil), value...), nil
	}
	if err := VerifyFinalSemanticArtifacts(context.Background(), evidence, load); err == nil || !strings.Contains(err.Error(), "omits, adds, or substitutes") {
		t.Fatalf("ignored terminal runtime map was accepted: %v", err)
	}
}

// Confirms the recorded activation cannot point to another implementation or
// carry a different runtime digest than the authenticated plan.
func TestFinalSemanticDeploymentRejectsWrongUpgradeAddressAndHash(t *testing.T) {
	t.Parallel()
	evidence, _, plan, lock, deployment := finalDeploymentAnchorInputs(t)
	deployment.Upgrade.Implementation = common.HexToAddress("0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee")
	if err := verifyFinalDeploymentRuntimeAnchors(&evidence, plan, lock, deployment); err == nil || !strings.Contains(err.Error(), "coordinator upgrade") {
		t.Fatalf("foreign upgrade address was accepted: %v", err)
	}
	evidence, _, plan, lock, deployment = finalDeploymentAnchorInputs(t)
	deployment.Upgrade.RuntimeCodeHash = finalTestHex(0xe3)
	if err := verifyFinalDeploymentRuntimeAnchors(&evidence, plan, lock, deployment); err == nil || !strings.Contains(err.Error(), "coordinator upgrade") {
		t.Fatalf("foreign upgrade hash was accepted: %v", err)
	}
}

// Covers missing, extra, noncanonical, and duplicate address forms so every
// reviewed executable has one unambiguous evidence key.
func TestFinalSemanticDeploymentRejectsRootCardinalityAndAliasMutations(t *testing.T) {
	t.Parallel()
	evidence, _, plan, lock, deployment := finalDeploymentAnchorInputs(t)
	evidence.Deployment.RuntimeRoots = evidence.Deployment.RuntimeRoots[:len(evidence.Deployment.RuntimeRoots)-1]
	if err := verifyFinalDeploymentRuntimeAnchors(&evidence, plan, lock, deployment); err == nil || !strings.Contains(err.Error(), "root count") {
		t.Fatalf("missing runtime root was accepted: %v", err)
	}
	evidence, _, plan, lock, deployment = finalDeploymentAnchorInputs(t)
	evidence.Deployment.RuntimeRoots = append(evidence.Deployment.RuntimeRoots, evidence.Deployment.RuntimeRoots[0])
	if err := verifyFinalDeploymentRuntimeAnchors(&evidence, plan, lock, deployment); err == nil || !strings.Contains(err.Error(), "root count") {
		t.Fatalf("extra runtime root was accepted: %v", err)
	}
	evidence, _, plan, lock, deployment = finalDeploymentAnchorInputs(t)
	evidence.Deployment.RuntimeRoots[0].Address = strings.ToUpper(evidence.Deployment.RuntimeRoots[0].Address)
	if err := verifyFinalDeploymentRuntimeAnchors(&evidence, plan, lock, deployment); err == nil || !strings.Contains(err.Error(), "not canonical") {
		t.Fatalf("uppercase runtime-root address was accepted: %v", err)
	}
	evidence, _, plan, lock, deployment = finalDeploymentAnchorInputs(t)
	deployment.RuntimeCodeHashes = finalDeploymentRuntimeMapCopy(deployment.RuntimeCodeHashes)
	address := strings.ToLower(plan.Deployment.CoordinatorProxy.Hex())
	deployment.RuntimeCodeHashes[strings.ToUpper(address)] = deployment.RuntimeCodeHashes[address]
	if err := verifyFinalDeploymentRuntimeAnchors(&evidence, plan, lock, deployment); err == nil || !strings.Contains(err.Error(), "duplicates address") {
		t.Fatalf("case-aliased terminal runtime map was accepted: %v", err)
	}
	evidence, _, plan, lock, deployment = finalDeploymentAnchorInputs(t)
	deployment.RuntimeCodeHashes = finalDeploymentRuntimeMapCopy(deployment.RuntimeCodeHashes)
	delete(deployment.RuntimeCodeHashes, strings.ToLower(plan.Deployment.CoordinatorProxy.Hex()))
	if err := verifyFinalDeploymentRuntimeAnchors(&evidence, plan, lock, deployment); err == nil || !strings.Contains(err.Error(), "omits, adds, or substitutes") {
		t.Fatalf("terminal map missing a reviewed root was accepted: %v", err)
	}
}

// Keeps the two trust-root schema upgrades fail-closed and confirms the
// release-lock locator cannot be replaced by a noncanonical or unrelated plan.
func TestFinalSemanticDeploymentRejectsLegacySchemaAndPlanLockSubstitution(t *testing.T) {
	t.Parallel()
	evidence, artifacts, plan, lock, _ := finalDeploymentAnchorInputs(t)
	evidence.Schema = "urnetwork-final-semantic-evidence-v5"
	evidence.EvidenceHash = ""
	if err := VerifyFinalSemanticEvidence(&evidence); err == nil || !strings.Contains(err.Error(), "unsupported final semantic evidence schema") {
		t.Fatalf("legacy final evidence schema was accepted: %v", err)
	}
	source, _ := finalSemanticFixture(t)
	draft, err := BuildFinalSemanticEvidence(source)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := SealFinalSemanticEvidenceOnChain(context.Background(), draft, &finalTestChainReader{evidence: draft})
	if err != nil {
		t.Fatal(err)
	}
	legacyVerification := *sealed.PublicVerification
	legacyVerification.Schema = "urnetwork-final-public-chain-verification-v5"
	if err := finalizePublicChainVerification(&legacyVerification, evidence.ChainID, evidence.GenesisHash); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("otherwise complete legacy public-verification schema was accepted: %v", err)
	}
	plan.ReleaseLockHash = finalTestHex(0xe4)
	if _, err := finalReleaseRuntimeRootsForPlan(plan, lock); err == nil || !strings.Contains(err.Error(), "does not bind") {
		t.Fatalf("plan substituted away from release lock was accepted: %v", err)
	}
	lockData := append([]byte(nil), artifacts[evidence.ReleaseLockArtifact.URI]...)
	lockData = append(lockData, ' ')
	if _, err := verifyFinalReleaseLockArtifact(&evidence, plan, lockData); err == nil || !strings.Contains(err.Error(), "not canonical") {
		t.Fatalf("noncanonical release-lock artifact was accepted: %v", err)
	}
}

// Binds release-lock code digests to the generated contract bytes rather than
// accepting a syntactically valid lock with an unreviewed executable hash.
func TestFinalSemanticDeploymentRejectsUnreviewedReleaseRuntimeHash(t *testing.T) {
	t.Parallel()
	_, _, _, lock, _ := finalDeploymentAnchorInputs(t)
	build := make(map[string]any, len(lock.EVMBuild))
	for key, value := range lock.EVMBuild {
		build[key] = value
	}
	lock.EVMBuild = build
	lock.EVMBuild["fleet_batcher_runtime_hash"] = finalTestHex(0xe5)
	if err := verifyFinalReleaseLockRuntimeBuild(lock); err == nil || !strings.Contains(err.Error(), "differs from generated") {
		t.Fatalf("unreviewed release runtime hash was accepted: %v", err)
	}
}

// Makes a missing canonical lock explicit before artifact loading can turn it
// into an ambiguous local-path fallback.
func TestFinalSemanticDeploymentRejectsMissingReleaseLockArtifact(t *testing.T) {
	t.Parallel()
	evidence, _, plan, _, _ := finalDeploymentAnchorInputs(t)
	if _, err := verifyFinalReleaseLockArtifact(&evidence, plan, nil); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("missing release-lock artifact was accepted: %v", err)
	}
}
