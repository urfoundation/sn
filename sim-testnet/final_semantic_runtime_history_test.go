package main

import (
	"bytes"
	"encoding/json"
	"maps"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The old source varies only genuine compiler metadata, as in the existing
// signed original-companion fixture. Current executable admission stays exact.
func finalHistoricalArtifactPlanTest(t *testing.T, plan *SetupPlan) {
	t.Helper()
	artifact := validatorEvidenceCarryHistoricalArtifactTest(t, plan.ValidatorEvidenceSource.Artifact)
	lock := *plan.ValidatorEvidenceSource.ReleaseLock
	lock.EVMBuild = maps.Clone(lock.EVMBuild)
	lock.EVMBuild["validator_evidence_artifact_hash"] = artifact.FoundryArtifactHash
	lock.EVMBuild["validator_evidence_runtime_hash"] = artifact.RuntimeBytecodeHash
	lock.EVMBuild["validator_evidence_storage_layout_hash"] = artifact.StorageLayoutHash
	var err error
	plan.ValidatorEvidenceSource, err = newValidatorEvidenceSource(&lock, artifact)
	if err != nil {
		t.Fatal(err)
	}
	plan.ReleaseLockHash, err = canonicalHashHex(&lock)
	if err != nil {
		t.Fatal(err)
	}
	plan.validatorEvidenceHistorical = true
	if err := rebindValidatorEvidencePlan(plan); err != nil {
		t.Fatal(err)
	}
	plan.PlanHash, err = plan.hash()
	if err != nil {
		t.Fatal(err)
	}
	if err := validatePlanBudget(plan); err != nil {
		t.Fatalf("original artifact approval prerequisite: %v", err)
	}
}

func TestFinalSemanticHistoricalRuntimeNativeArchiveRetainsOriginalArtifactAuthority(t *testing.T) {
	fixture := newValidatorEvidenceHistoricalLockTestFixture(t)
	original := fixture.original
	finalHistoricalArtifactPlanTest(t, original)
	wire, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodePersistedPlanBytes(wire); err == nil || !strings.Contains(err.Error(), "fresh validator evidence source differs") {
		t.Fatalf("original artifact became a current source: %v", err)
	}
	archive := &finalSemanticArchive{files: map[string][]byte{"launch-foundation/plan.json": wire}}
	action := original.Actions[0]
	opened, current, err := archive.nativeActionPlan(original.PlanHash, action.ID, action.IntentHash)
	if err != nil || opened.PlanHash != original.PlanHash || current.PlanHash != original.PlanHash || !finalJSONEqual(opened.ValidatorEvidenceSource, original.ValidatorEvidenceSource) {
		t.Fatalf("closed original native archive: %v", err)
	}
	successor := *original
	successor.PriorPlanHashes = []string{original.PlanHash}
	successor.PlanHash, err = successor.hash()
	if err != nil {
		t.Fatal(err)
	}
	successorBytes, err := json.Marshal(&successor)
	if err != nil {
		t.Fatal(err)
	}
	priorPath := "plan-history/" + stringsTrim0x(original.PlanHash) + ".json"
	archive.files[priorPath], archive.files["launch-foundation/plan.json"] = wire, successorBytes
	opened, current, err = archive.nativeActionPlan(original.PlanHash, action.ID, action.IntentHash)
	if err != nil || opened.PlanHash != original.PlanHash || current.PlanHash != successor.PlanHash {
		t.Fatalf("closed original ancestor authority: %v", err)
	}
	archive.files[priorPath] = successorBytes
	if _, _, err := archive.nativeActionPlan(original.PlanHash, action.ID, action.IntentHash); err == nil {
		t.Fatal("closed archive accepted a substituted ancestor path")
	}
	archive.files[priorPath] = wire
	if _, _, err := archive.nativeActionPlan("0x"+strings.Repeat("14", 32), action.ID, action.IntentHash); err == nil {
		t.Fatal("closed archive accepted an unapproved source")
	}
	changed := bytes.Clone(wire)
	changed[0] = '!'
	archive.files["launch-foundation/plan.json"] = changed
	if _, _, err := archive.nativeActionPlan(original.PlanHash, action.ID, action.IntentHash); err == nil {
		t.Fatal("warm archived plan cache ignored changed bytes")
	}
}

func TestFinalSemanticHistoricalRuntimeLockRetainsExactOriginalProvenance(t *testing.T) {
	fixture := newValidatorEvidenceHistoricalLockTestFixture(t)
	// Retain the original YAML-domain object. The companion's JSON snapshot
	// uses json.Number, whose YAML spelling would turn numbers into strings.
	lock := validatorEvidenceRuntime455TestLock(t)
	lockHash, err := canonicalHashHex(lock)
	if err != nil || lockHash != fixture.original.ReleaseLockHash || !finalJSONEqual(lock, fixture.original.ValidatorEvidenceSource.ReleaseLock) {
		t.Fatalf("original YAML lock does not reproduce approved source: hash=%s error=%v", lockHash, err)
	}
	wire, err := yaml.Marshal(lock)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := canonicalReleaseLockBytes(lock); err == nil {
		t.Fatal("current lock writer admitted original455")
	}
	evidence := &FinalSemanticEvidence{PlanHash: fixture.original.PlanHash}
	opened, err := verifyFinalReleaseLockArtifact(evidence, fixture.original, wire)
	if err != nil || !finalJSONEqual(opened, lock) {
		t.Fatalf("closed original455 release lock: %v", err)
	}
	if _, err := finalReleaseRuntimeRootsForPlan(fixture.original, opened); err != nil {
		t.Fatalf("original runtime root census: %v", err)
	}
	for _, test := range []struct {
		name   string
		mutate func(*ReleaseRuntimeLock)
	}{
		{name: "code", mutate: func(r *ReleaseRuntimeLock) { r.CodeHash = "0x" + strings.Repeat("21", 32) }},
		{name: "metadata", mutate: func(r *ReleaseRuntimeLock) { r.MetadataHash = "0x" + strings.Repeat("22", 32) }},
		{name: "source", mutate: func(r *ReleaseRuntimeLock) { r.SourceCommit = strings.Repeat("23", 20) }},
	} {
		changed := *lock
		test.mutate(&changed.Runtime)
		encoded, err := yaml.Marshal(&changed)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := verifyFinalReleaseLockArtifact(evidence, fixture.original, encoded); err == nil {
			t.Fatalf("changed original %s lock passed", test.name)
		}
	}
	other := *fixture.original
	other.ReleaseLockHash = "0x" + strings.Repeat("24", 32)
	if _, err := verifyFinalReleaseLockArtifact(evidence, &other, wire); err == nil {
		t.Fatal("original lock escaped canonical approval hash")
	}
}

// Rebuild one real 202-fleet renewal from its exact original artifact approval.
// The copy must keep historical admission without transferring it to live work.
func TestFinalSemanticHistoricalRuntimeRenewalReconstructsOriginalApproval(t *testing.T) {
	fixture := newFleetRenewalTestFixture(t)
	finalHistoricalArtifactPlanTest(t, fixture.base)
	fixture.renewal.SourcePlanHash = fixture.base.PlanHash
	baseBytes, err := json.Marshal(fixture.base)
	if err != nil {
		t.Fatal(err)
	}
	base, err := decodeFinalHistoricalPlanBytes(baseBytes)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := appendFleetRenewalPlan(base, fixture.renewal); err == nil || !strings.Contains(err.Error(), "fresh validator evidence source differs") {
		t.Fatalf("historical artifact became live renewal authority: %v", err)
	}
	approved, err := appendFleetRenewalPlanForHistory(base, fixture.renewal, true)
	if err != nil {
		t.Fatalf("reconstruct original renewal approval: %v", err)
	}
	approvedBytes, err := json.Marshal(approved)
	if err != nil {
		t.Fatal(err)
	}
	basePath := "plan-history/" + stringsTrim0x(base.PlanHash) + ".json"
	archive := &finalSemanticArchive{files: map[string][]byte{basePath: baseBytes, "launch-foundation/plan.json": approvedBytes}, artifactDeriver: func(kind, uri string, raw []byte) (FinalArtifactLocator, error) {
		return FinalArtifactLocator{Kind: kind, URI: uri, ContentHash: bytesSHA256(raw), SizeBytes: uint64(len(raw))}, nil
	}}
	source := &finalFleetGenerationSource{archive: archive, current: approved, plans: map[string]*SetupPlan{base.PlanHash: base, approved.PlanHash: approved}, planPaths: map[string]string{base.PlanHash: basePath, approved.PlanHash: "launch-foundation/plan.json"}, raw: map[string][]byte{}}
	round, retained, err := source.renewalApproval(fixture.renewal)
	if err != nil || retained.PlanHash != approved.PlanHash || round.Approval.ContentHash != bytesSHA256(approvedBytes) {
		t.Fatalf("archived original renewal source: %v", err)
	}
	retained, err = finalFleetLifecycleRenewalApproval(approved, archive.files)
	if err != nil || retained == nil || retained.PlanHash != approved.PlanHash {
		t.Fatalf("archived original lifecycle renewal: %v", err)
	}
	changed := cloneFleetRenewalForTest(t, fixture.renewal)
	changed.MaximumFeePerGasWei++
	if _, _, err := source.renewalApproval(changed); err == nil {
		t.Fatal("historical renewal accepted changed fee authority")
	}
	if after, err := json.Marshal(base); err != nil || !bytes.Equal(baseBytes, after) {
		t.Fatalf("historical reconstruction changed original bytes: %v", err)
	}
}
