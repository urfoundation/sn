// Runtime461 keeps original approval identities and exact460 source provenance.
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// Exact source provenance is independent of the moving current constants.
func runtime461ReviewedTestLock() *ReleaseLock {
	return &ReleaseLock{SchemaVersion: 1, Release: "1.0", Runtime: ReleaseRuntimeLock{
		SourceRepository: "https://github.com/RaoFoundation/subtensor",
		SourceRefKind:    "commit", SourceRefName: "7c9d45ebd423c7f6b0b477e11414fe2fe3a3794b",
		SourceCommit:         "7c9d45ebd423c7f6b0b477e11414fe2fe3a3794b",
		CodeHash:             "0x15cf19d2f4f8e2a8a6f46cb735db8f9f03ba3775866188fa93799ad3a040da2e",
		MetadataHash:         "0x98b2cfd0d6633488dfe5b3b70b869d5753aa3c42396533013df131e4e0e5ca68",
		CompressedWasmSHA256: "0xa236f7d2ac285615ee1789953e5e009464cc96f357d48278a848f82cdc771cc4",
		SpecVersion:          461, TransactionVersion: 1, StateVersion: 1,
	}}
}

// Reopening the actual persisted source carries its exact original bytes,
// intent hashes and spending limits across the former-current transition.
func TestRuntime461RestartsOriginal460CompanionWithoutRewritingApproval(t *testing.T) {
	fixture := newValidatorEvidenceRuntimeLockTestFixture(t, 460)
	restarted, err := loadPersistedPlan(fixture.config, fixture.stateDir)
	if err != nil || restarted == nil {
		t.Fatalf("runtime461 cannot reopen original460 companion: %v", err)
	}
	if restarted.ValidatorEvidenceSource.ReleaseLock.Runtime.SpecVersion != 460 || !finalJSONEqual(restarted.ValidatorEvidenceCarry, fixture.current.ValidatorEvidenceCarry) || restarted.MaximumSpend != fixture.current.MaximumSpend || restarted.Limits != fixture.current.Limits {
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
func TestRuntime461Archived460LockRetainsExactProvenance(t *testing.T) {
	lock := testReleaseLockFixture(t)
	image := lock.Runtime.Image
	lock.Runtime = runtime460ReviewedTestLock().Runtime
	lock.Runtime.Image = image
	if err := validateValidatorEvidenceHistoricalReleaseLock(lock); err != nil {
		t.Fatal(err)
	}
	if err := validateReviewedRuntimeIdentity(lock); err == nil {
		t.Fatal("original460 acquired current461 authority")
	}
	for _, change := range []func(*ReleaseRuntimeLock){
		func(value *ReleaseRuntimeLock) {
			value.SourceCommit = runtime461ReviewedTestLock().Runtime.SourceCommit
		},
		func(value *ReleaseRuntimeLock) { value.SourceRefName = "testnet" },
		func(value *ReleaseRuntimeLock) { value.CodeHash = runtime461ReviewedTestLock().Runtime.CodeHash },
		func(value *ReleaseRuntimeLock) {
			value.MetadataHash = runtime461ReviewedTestLock().Runtime.MetadataHash
		},
		func(value *ReleaseRuntimeLock) {
			value.CompressedWasmSHA256 = runtime461ReviewedTestLock().Runtime.CompressedWasmSHA256
		},
		func(value *ReleaseRuntimeLock) { value.TransactionVersion++ },
		func(value *ReleaseRuntimeLock) { value.StateVersion++ },
	} {
		changed := *lock
		change(&changed.Runtime)
		if err := validateValidatorEvidenceHistoricalReleaseLock(&changed); err == nil {
			t.Fatal("mixed460 source acquired archive authority")
		}
	}
}
