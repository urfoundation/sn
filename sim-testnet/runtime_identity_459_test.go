// The459 transition preserves approved hashes and original458 source bytes.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// Hash normalization is explicitly limited to the same original455 domain;
// a historical458 lock remains insufficient for current plan execution.
func TestRuntime459ConfigHashPreserves455And458Domains(t *testing.T) {
	public := PublicManifest{SchemaVersion: 1, Profile: releaseProfile}
	public.Chain.ChainID, public.Chain.GenesisHash = testnetChainID, testnetGenesis
	public.Chain.ExpectedRuntimeSpec, public.Chain.ExpectedTransactionVersion, public.Chain.ExpectedStateVersion = 455, 1, 1
	config, hyper := &HarnessConfig{}, &Hyperparameters{}
	originalHash, err := releaseConfigHash(config, &public, hyper)
	if err != nil {
		t.Fatal(err)
	}
	public.Chain.ConfigIdentityRuntimeSpec = 455
	for _, spec := range []uint32{458, 459, 460, 461} {
		public.Chain.ExpectedRuntimeSpec = spec
		before, err := json.Marshal(public)
		if err != nil {
			t.Fatal(err)
		}
		hash, err := releaseConfigHash(config, &public, hyper)
		after, marshalErr := json.Marshal(public)
		if err != nil || marshalErr != nil || hash != originalHash || !bytes.Equal(before, after) {
			t.Fatalf("runtime%d changed original activation identity or input bytes: %v", spec, err)
		}
	}
	plan := &SetupPlan{ConfigIdentityRuntimeSpec: 455}
	cfg := &ResolvedConfig{Public: &public, Release: runtime461ReviewedTestLock()}
	if err := validateRuntimeConfigIdentityPlan(cfg, plan); err != nil {
		t.Fatal(err)
	}
	public.Chain.ExpectedRuntimeSpec = 458
	if err := validateRuntimeConfigIdentityPlan(cfg, plan); err == nil {
		t.Fatal("historical458 expectation gained current459 plan authority")
	}
	cfg.Release = runtime458ReviewedTestLock()
	if err := validateRuntimeConfigIdentityPlan(cfg, plan); err == nil {
		t.Fatal("historical458 lock gained current plan authority")
	}
	for _, spec := range []uint32{456, 457, 462} {
		public.Chain.ExpectedRuntimeSpec = spec
		if _, err := releaseConfigHash(config, &public, hyper); err == nil {
			t.Fatalf("unreviewed%d acquired the original hash", spec)
		}
	}
}

// The real persisted carry reader reopens a459 plan with its original458
// companion source, intents and spend. No archived wire is regenerated.
func TestRuntime459RestartsOriginal458CompanionWithoutRewritingApproval(t *testing.T) {
	fixture := newValidatorEvidenceRuntimeLockTestFixture(t, 458)
	restarted, err := loadPersistedPlan(fixture.config, fixture.stateDir)
	if err != nil || restarted == nil {
		t.Fatalf("current459 cannot reopen original458 companion: %v", err)
	}
	if restarted.ReleaseLockHash == restarted.ValidatorEvidenceCarry.SourceReleaseLockHash || restarted.ValidatorEvidenceSource.ReleaseLock.Runtime.SpecVersion != 458 || !finalJSONEqual(restarted.ValidatorEvidenceCarry, fixture.current.ValidatorEvidenceCarry) {
		t.Fatal("restart replaced current or historical source authority")
	}
	for _, actionId := range []string{validatorEvidenceDeployActionID, validatorEvidenceAnchorActionID} {
		if !finalJSONEqual(actionByID(t, restarted, actionId), actionByID(t, fixture.original, actionId)) {
			t.Fatalf("restart changed original %s intent or spend", actionId)
		}
	}
	if restarted.MaximumSpend != fixture.current.MaximumSpend || restarted.Limits != fixture.current.Limits {
		t.Fatal("restart changed approved budgets")
	}
	for _, item := range []struct {
		name string
		load func(string, *SetupPlan, string) (*SetupPlan, error)
	}{
		{name: "fleet mirror", load: loadFleetMirrorLineagePlan},
		{name: "native funding", load: loadSubstrateFundingLineagePlan},
		{name: "conviction", load: loadVoluntaryConvictionLineagePlan},
	} {
		plan, err := item.load(fixture.stateDir, fixture.current, fixture.original.PlanHash)
		if err != nil || plan == nil || !finalJSONEqual(plan.ValidatorEvidenceSource, fixture.original.ValidatorEvidenceSource) {
			t.Fatalf("%s lost original458 source: %v", item.name, err)
		}
		foreign := *fixture.current
		foreign.PriorPlanHashes = nil
		if plan, err := item.load(fixture.stateDir, &foreign, fixture.original.PlanHash); plan != nil || err == nil {
			t.Fatalf("%s admitted an unapproved ancestor", item.name)
		}
	}
	archived, err := os.ReadFile(filepath.Join(fixture.stateDir, "plans", stringsTrim0x(fixture.original.PlanHash)+".json"))
	if err != nil || !bytes.Equal(archived, fixture.originalWire) {
		t.Fatal("restart rewrote the original approval")
	}
	if err := atomicWrite(filepath.Join(fixture.stateDir, "plan.json"), fixture.originalWire, 0o600); err != nil {
		t.Fatal(err)
	}
	if plan, err := loadPersistedPlan(fixture.config, fixture.stateDir); plan != nil || !errors.Is(err, errPersistedPlanIdentityMismatch) {
		t.Fatalf("original458 plan became current authority: %v", err)
	}
}

// Archived458 source metadata and provenance remain an exact tuple. Both
// versions are accepted only through their appropriate admission boundary.
func TestRuntime459Archived458LockRejectsCrossArtifactAndProvenanceDrift(t *testing.T) {
	lock := testReleaseLockFixture(t)
	runtimeImage := lock.Runtime.Image
	lock.Runtime = runtime458ReviewedTestLock().Runtime
	lock.Runtime.Image = runtimeImage
	if err := validateValidatorEvidenceHistoricalReleaseLock(lock); err != nil {
		t.Fatal(err)
	}
	if err := validateReviewedRuntimeIdentity(lock); err == nil {
		t.Fatal("historical458 lock became current authority")
	}
	for _, change := range []func(*ReleaseRuntimeLock){
		func(runtime *ReleaseRuntimeLock) {
			runtime.SourceRefName = runtime459ReviewedTestLock().Runtime.SourceRefName
		},
		func(runtime *ReleaseRuntimeLock) {
			runtime.SourceCommit = runtime459ReviewedTestLock().Runtime.SourceCommit
		},
		func(runtime *ReleaseRuntimeLock) { runtime.CodeHash = runtime459ReviewedTestLock().Runtime.CodeHash },
		func(runtime *ReleaseRuntimeLock) {
			runtime.MetadataHash = runtime459ReviewedTestLock().Runtime.MetadataHash
		},
		func(runtime *ReleaseRuntimeLock) {
			runtime.CompressedWasmSHA256 = runtime459ReviewedTestLock().Runtime.CompressedWasmSHA256
		},
		func(runtime *ReleaseRuntimeLock) { runtime.SourceTag = "v458" },
		func(runtime *ReleaseRuntimeLock) { runtime.TransactionVersion++ },
		func(runtime *ReleaseRuntimeLock) { runtime.StateVersion++ },
	} {
		changed := *lock
		change(&changed.Runtime)
		if err := validateValidatorEvidenceHistoricalReleaseLock(&changed); err == nil {
			t.Fatal("mixed458 provenance acquired historical authority")
		}
	}
}
