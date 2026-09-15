// Runtime460 keeps original approval identities and exact459 source provenance.
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// Exact source provenance is independent of the moving current constants.
func runtime460ReviewedTestLock() *ReleaseLock {
	return &ReleaseLock{SchemaVersion: 1, Release: "1.0", Runtime: ReleaseRuntimeLock{
		SourceRepository: "https://github.com/RaoFoundation/subtensor",
		SourceRefKind:    "commit", SourceRefName: "8d5f20ec1a5e5d90295d43046dacdefc54aaed06",
		SourceCommit:         "8d5f20ec1a5e5d90295d43046dacdefc54aaed06",
		CodeHash:             "0xa2ba599cc0ee97abaa078cf54498ad020957a32cdc2cb7c1e5b9fa14bf5cad3d",
		MetadataHash:         "0x98574118d8447c31b72c57402bdda481203f58273ae175a3b6c1da44400e934c",
		CompressedWasmSHA256: "0x12b9affec176cbb79c7e5db253d3d0e47f4cb575ce4501ef10de6578afbb817f",
		SpecVersion:          460, TransactionVersion: 1, StateVersion: 1,
	}}
}

// Reopening the actual persisted source carries its exact original bytes,
// intent hashes and spending limits across the former-current transition.
func TestRuntime460RestartsOriginal459CompanionWithoutRewritingApproval(t *testing.T) {
	fixture := newValidatorEvidenceRuntimeLockTestFixture(t, 459)
	restarted, err := loadPersistedPlan(fixture.config, fixture.stateDir)
	if err != nil || restarted == nil {
		t.Fatalf("runtime460 cannot reopen original459 companion: %v", err)
	}
	if restarted.ValidatorEvidenceSource.ReleaseLock.Runtime.SpecVersion != 459 || !finalJSONEqual(restarted.ValidatorEvidenceCarry, fixture.current.ValidatorEvidenceCarry) || restarted.MaximumSpend != fixture.current.MaximumSpend || restarted.Limits != fixture.current.Limits {
		t.Fatal("restart changed original source authority or approved budget")
	}
	for _, actionId := range []string{validatorEvidenceDeployActionID, validatorEvidenceAnchorActionID} {
		if !finalJSONEqual(actionByID(t, restarted, actionId), actionByID(t, fixture.original, actionId)) {
			t.Fatal("restart changed an original signed intent")
		}
	}
	archived, err := os.ReadFile(filepath.Join(fixture.stateDir, "plans", stringsTrim0x(fixture.original.PlanHash)+".json"))
	if err != nil || !bytes.Equal(archived, fixture.originalWire) {
		t.Fatal("restart rewrote the original approval")
	}
}

// An exact former-current lock remains historical evidence; mixed source,
// metadata, code or version fields never inherit that authority.
func TestRuntime460Archived459LockRetainsExactProvenance(t *testing.T) {
	lock := testReleaseLockFixture(t)
	image := lock.Runtime.Image
	lock.Runtime = runtime459ReviewedTestLock().Runtime
	lock.Runtime.Image = image
	if err := validateValidatorEvidenceHistoricalReleaseLock(lock); err != nil {
		t.Fatal(err)
	}
	if err := validateReviewedRuntimeIdentity(lock); err == nil {
		t.Fatal("original459 acquired current460 authority")
	}
	for _, change := range []func(*ReleaseRuntimeLock){
		func(value *ReleaseRuntimeLock) {
			value.SourceCommit = runtime460ReviewedTestLock().Runtime.SourceCommit
		},
		func(value *ReleaseRuntimeLock) { value.SourceRefName = "testnet" },
		func(value *ReleaseRuntimeLock) { value.CodeHash = runtime460ReviewedTestLock().Runtime.CodeHash },
		func(value *ReleaseRuntimeLock) {
			value.MetadataHash = runtime460ReviewedTestLock().Runtime.MetadataHash
		},
		func(value *ReleaseRuntimeLock) {
			value.CompressedWasmSHA256 = runtime460ReviewedTestLock().Runtime.CompressedWasmSHA256
		},
		func(value *ReleaseRuntimeLock) { value.TransactionVersion++ },
		func(value *ReleaseRuntimeLock) { value.StateVersion++ },
	} {
		changed := *lock
		change(&changed.Runtime)
		if err := validateValidatorEvidenceHistoricalReleaseLock(&changed); err == nil {
			t.Fatal("mixed459 source acquired archive authority")
		}
	}
}
