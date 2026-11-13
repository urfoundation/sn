package main

import (
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/validator"
)

// Source481 preserves the reviewed registration behavior, but a prior policy
// or observation cannot authorize its distinct runtime identity.
func TestRootRegisterRuntime481RequiresOwnPolicyAndObservation(t *testing.T) {
	f := newRootRegisterTestFixture(t, false)
	successor := f.config.Action.Policy
	successor.RuntimeSourceCommit, successor.RuntimeVersion.SpecVersion = crv4.NativeOwnerSource481, 481
	if err := successor.validate(); err != nil {
		t.Fatal("reviewed runtime481 registration policy refused", err)
	}
	for _, change := range []func(*rootRegisterPolicy){
		func(p *rootRegisterPolicy) { p.RuntimeSourceCommit = crv4.NativeOwnerSource479 },
		func(p *rootRegisterPolicy) { p.RuntimeSourceCommit = strings.Repeat("d", 40) },
		func(p *rootRegisterPolicy) { p.RuntimeSourceCommit = strings.ToUpper(crv4.NativeOwnerSource481) },
		func(p *rootRegisterPolicy) { p.RuntimeVersion.SpecVersion = 479 },
		func(p *rootRegisterPolicy) { p.RuntimeVersion.SpecVersion = 480 },
		func(p *rootRegisterPolicy) { p.RuntimeVersion.SpecVersion = 482 },
		func(p *rootRegisterPolicy) { p.RuntimeVersion.SpecName = "other-runtime" },
		func(p *rootRegisterPolicy) { p.RuntimeVersion.TransactionVersion = 2 },
		func(p *rootRegisterPolicy) { p.RuntimeVersion.StateVersion = 2 },
		func(p *rootRegisterPolicy) { p.RuntimeCodeHash = "" },
		func(p *rootRegisterPolicy) { p.RuntimeMetadataHash = "" },
		func(p *rootRegisterPolicy) { p.Operator = p.Reserve },
	} {
		changed := successor
		change(&changed)
		if err := changed.validate(); err == nil {
			t.Fatal("runtime481 source bypassed exact identity or operator separation", changed)
		}
	}
	observation := f.input.Observation
	if _, err := observation.eligibility(successor, f.metadata); err == nil {
		t.Fatal("the predecessor observation seal served runtime481")
	}
	observation.PolicyHash = rootObjectHash(successor)
	observation.ContentHash = ""
	observation.ContentHash = rootObjectHash(observation)
	eligibility, err := observation.eligibility(successor, f.metadata)
	if err != nil || !eligibility.Eligible || eligibility.ExistingSeat != nil || eligibility.BurnRao != 10 || eligibility.ConservativeReducibleRao != 99500 {
		t.Fatalf("runtime481 registration differs from the shared reviewed rules: %+v %v", eligibility, err)
	}
	_, metadata, digest, fileHash := rootCurrentTestMetadata(t)
	report, err := inspectRootCurrentCapabilities(metadata, digest, fileHash, crv4.NativeOwnerSource481)
	if err != nil {
		t.Fatal(err)
	}
	registration := rootCurrentTestCall(t, report, "root_register")
	if !strings.Contains(registration.MutationAdapter, "requires-separate-481-policy") || !strings.Contains(report.MutationExecution, "separate-481-root-register-domain") ||
		report.NativeSigning || report.NetworkEffects || report.ActivationReady || report.CurrentRuntimeVerified || report.RuntimeSourceProven || !report.PlanningOnly {
		t.Fatal("runtime481 source review acquired registration execution authority", registration, report)
	}
}

// Post-Alpha source membership never substitutes for the original activation's
// actual storage proof, exact code or independently signed opening authority.
func TestNativeTreasuryMigrationRuntime481RequiresCompletionProof(t *testing.T) {
	authority, policy := nativeMigrationTestAuthorityFor(t, crv4.NativeOwnerSource481, 481)
	if err := authority.validateScope(policy, policy.Through, nil); err != nil {
		t.Fatal("completed runtime481 opening with fresh approval refused", err)
	}
	if nativeExecutionRuntimeSource(authority) != crv4.NativeOwnerSource481 {
		t.Fatal("producer lost the independently approved runtime481 source")
	}
	missing := *authority
	missing.MigrationComplete = nil
	if err := missing.validateScope(policy, policy.Through, nil); err == nil {
		t.Fatal("runtime481 acquired economic authority without a completion proof")
	}
	previous, previousPolicy := nativeMigrationTestAuthorityFor(t, crv4.NativeOwnerSource479, 479)
	if err := previous.validateScope(policy, policy.Through, nil); err == nil {
		t.Fatal("runtime479 authority served a runtime481 policy")
	}
	if err := authority.validateScope(previousPolicy, previousPolicy.Through, nil); err == nil {
		t.Fatal("runtime481 authority served a runtime479 policy")
	}
	for index, marker := range [][]byte{nil, {}, {0}, {1, 0}, {2}} {
		witness, code := nativeMigrationTestWitness(t, marker)
		incomplete := &nativeTreasuryAuthority{MigrationComplete: &witness, Deployment: nativeTreasuryDeployment{Activation: economicEmissionBoundary{Number: 703, Hash: witness.At}}}
		if err := incomplete.validateMigration(validator.OwnerRecycleRuntimePin{SourceCommit: crv4.NativeOwnerSource481, CodeHash: code}); err == nil {
			t.Fatal("incomplete original state accepted for runtime481", index)
		}
	}
	witness, code := nativeMigrationTestWitness(t, []byte{1})
	complete := &nativeTreasuryAuthority{MigrationComplete: &witness, Deployment: nativeTreasuryDeployment{Activation: economicEmissionBoundary{Number: 703, Hash: witness.At}}}
	if err := complete.validateMigration(validator.OwnerRecycleRuntimePin{SourceCommit: crv4.NativeOwnerSource481, CodeHash: code}); err != nil {
		t.Fatal("bounded runtime481 completion proof refused", err)
	}
	wrongCode := code
	wrongCode[0] ^= 1
	if err := complete.validateMigration(validator.OwnerRecycleRuntimePin{SourceCommit: crv4.NativeOwnerSource481, CodeHash: wrongCode}); err == nil {
		t.Fatal("runtime481 completion proof served another runtime artifact")
	}
	if err := complete.validateMigration(validator.OwnerRecycleRuntimePin{SourceCommit: crv4.NativeOwnerSource470, CodeHash: code}); err == nil {
		t.Fatal("historical runtime470 authority inherited runtime481 migration completion")
	}
}
