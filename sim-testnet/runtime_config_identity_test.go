// Synthetic configuration and signed-history fixtures cover the explicit
// runtime migration without borrowing deployment identities or live state.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// Each configuration owns its public/runtime identity while sharing unchanged
// synthetic campaign inputs. The historical manifest has no migration pin.
func runtimeConfigIdentityTestConfigs(t *testing.T) (*ResolvedConfig, *ResolvedConfig) {
	t.Helper()
	original := testResolvedConfig(t)
	original.Public.Chain.ExpectedRuntimeSpec = 455
	original.Public.Chain.ConfigIdentityRuntimeSpec = 0
	original.Release = validatorEvidenceRuntime455TestLock(t)
	var err error
	original.ConfigHash, err = releaseConfigHash(original.Config, original.Public, original.Hyperparameters)
	if err != nil {
		t.Fatal(err)
	}
	current := *original
	public := *original.Public
	public.Chain.ExpectedRuntimeSpec = 461
	public.Chain.ConfigIdentityRuntimeSpec = 455
	current.Public, current.Release = &public, testReleaseLockFixture(t)
	current.ConfigHash, err = releaseConfigHash(current.Config, current.Public, current.Hyperparameters)
	if err != nil {
		t.Fatal(err)
	}
	return original, &current
}

// An independent legacy wire image catches accidental zero-field emission,
// including a json tag whose spelling differs from the YAML omission rule.
func TestCoordinatorRepairCarryRuntimeIdentityRetainsLegacyWire(t *testing.T) {
	t.Parallel()
	wire := []byte(`{"SchemaVersion":1,"Profile":"synthetic-profile","Chain":{"Name":"synthetic-chain","ChainID":7,"GenesisHash":"synthetic-genesis","SS58Format":42,"TokenSymbol":"SYN","TokenDecimals":9,"ExpectedRuntimeSpec":455,"ExpectedTransactionVersion":1,"ExpectedStateVersion":1,"ExpectedBlockSeconds":12,"ExpectedDefaultMinTransferRao":100,"SubstratePublicReadEndpoint":"wss://native.example","EVMPublicReadEndpoint":"https://evm.example","PrivateAuthorityFrom":"synthetic-authority","FinalityMethod":"synthetic-finality","PublicFallbackAllowsEventIndexing":false},"Subnet":{"Mode":"existing","NetuidFrom":"synthetic-netuid"},"Governance":{"Testnet":"synthetic-owner","Mainnet":"synthetic-safe"},"Artifacts":{"Store":"synthetic-store","History":"synthetic-history","ContentAddressed":true}}`)
	var public PublicManifest
	if err := json.Unmarshal(wire, &public); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(public)
	if err != nil || !bytes.Equal(encoded, wire) {
		t.Fatalf("omitted runtime pin changed legacy public wire: %v", err)
	}
	yamlWire, err := yaml.Marshal(public)
	if err != nil || bytes.Contains(yamlWire, []byte("config_identity_runtime_spec")) {
		t.Fatalf("omitted runtime pin changed legacy YAML: %v", err)
	}
	config, hyper := &HarnessConfig{}, &Hyperparameters{}
	want, err := canonicalHashHex(struct {
		Config          *HarnessConfig   `json:"config"`
		Public          json.RawMessage  `json:"public"`
		Hyperparameters *Hyperparameters `json:"hyperparameters"`
	}{Config: config, Public: wire, Hyperparameters: hyper})
	if err != nil {
		t.Fatal(err)
	}
	got, err := releaseConfigHash(config, &public, hyper)
	if err != nil || got != want {
		t.Fatalf("legacy configuration hash changed: got=%s want=%s error=%v", got, want, err)
	}
}

// Unpinned current/future manifests retain ordinary hashing. Only one explicit
// predecessor/current tuple can preserve signed configuration identity.
func TestCoordinatorRepairCarryRuntimeIdentityRequiresExplicitReviewedPin(t *testing.T) {
	t.Parallel()
	original, current := runtimeConfigIdentityTestConfigs(t)
	for _, version := range []uint32{451, 454, 458, 459, 460, 461, 462} {
		public := *current.Public
		public.Chain.ExpectedRuntimeSpec, public.Chain.ConfigIdentityRuntimeSpec = version, 0
		hash, err := releaseConfigHash(current.Config, &public, current.Hyperparameters)
		if err != nil || hash == original.ConfigHash {
			t.Fatalf("unpinned runtime%d inherited runtime455 identity: %v", version, err)
		}
	}
	for _, item := range []struct {
		name   string
		change func(*PublicManifest)
	}{
		{name: "another predecessor", change: func(public *PublicManifest) { public.Chain.ConfigIdentityRuntimeSpec = 454 }},
		{name: "current as predecessor", change: func(public *PublicManifest) { public.Chain.ConfigIdentityRuntimeSpec = 458 }},
		{name: "future", change: func(public *PublicManifest) { public.Chain.ExpectedRuntimeSpec = 462 }},
		{name: "historical", change: func(public *PublicManifest) { public.Chain.ExpectedRuntimeSpec = 455 }},
		{name: "transaction", change: func(public *PublicManifest) { public.Chain.ExpectedTransactionVersion++ }},
		{name: "state", change: func(public *PublicManifest) { public.Chain.ExpectedStateVersion++ }},
		{name: "chain", change: func(public *PublicManifest) { public.Chain.ChainID++ }},
		{name: "genesis", change: func(public *PublicManifest) { public.Chain.GenesisHash = "synthetic-other-genesis" }},
		{name: "profile", change: func(public *PublicManifest) { public.Profile = "synthetic-other-profile" }},
		{name: "schema", change: func(public *PublicManifest) { public.SchemaVersion++ }},
	} {
		public := *current.Public
		item.change(&public)
		if _, err := releaseConfigHash(current.Config, &public, current.Hyperparameters); err == nil {
			t.Fatalf("%s acquired an unreviewed legacy hash", item.name)
		}
	}
}

// Hashing owns its normalization copy and keeps every non-runtime input in
// the signed domain. The explicit pin survives both public wire encodings.
func TestCoordinatorRepairCarryRuntimeIdentityPreservesHashAndInputOwnership(t *testing.T) {
	t.Parallel()
	original, current := runtimeConfigIdentityTestConfigs(t)
	before, err := json.Marshal(current.Public)
	if err != nil {
		t.Fatal(err)
	}
	got, err := releaseConfigHash(current.Config, current.Public, current.Hyperparameters)
	if err != nil || got != original.ConfigHash || current.ConfigHash != original.ConfigHash {
		t.Fatalf("explicit runtime migration changed signed configuration identity: %v", err)
	}
	after, err := json.Marshal(current.Public)
	if err != nil || !bytes.Equal(before, after) || current.Public.Chain.ExpectedRuntimeSpec != 461 || !bytes.Contains(after, []byte(`"ConfigIdentityRuntimeSpec":455`)) {
		t.Fatalf("hashing mutated or omitted current runtime authority: %v", err)
	}
	publicWire, err := yaml.Marshal(current.Public)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "public.yml")
	if err := os.WriteFile(path, publicWire, 0o600); err != nil {
		t.Fatal(err)
	}
	var reopened PublicManifest
	if err := strictYAML(path, &reopened); err != nil || !reflect.DeepEqual(&reopened, current.Public) {
		t.Fatalf("explicit public pin did not round-trip strict YAML: %v", err)
	}
	for _, change := range []func(*PublicManifest){
		func(public *PublicManifest) { public.Chain.ExpectedBlockSeconds++ },
		func(public *PublicManifest) { public.Chain.ExpectedDefaultMinTransferRao++ },
		func(public *PublicManifest) { public.Chain.EVMPublicReadEndpoint = "https://changed-evm.example" },
	} {
		public := *current.Public
		change(&public)
		hash, err := releaseConfigHash(current.Config, &public, current.Hyperparameters)
		if err != nil || hash == original.ConfigHash {
			t.Fatalf("non-runtime manifest drift inherited activation identity: %v", err)
		}
	}
}

// The real planner binds migration approval without changing resolved launch
// inputs, transaction intents, deterministic roles, custody or allowances.
func TestCoordinatorRepairCarryRuntimeIdentityBindsPlanWithoutChangingActions(t *testing.T) {
	t.Parallel()
	original, current := runtimeConfigIdentityTestConfigs(t)
	roles, err := derivePublicRoles(original)
	if err != nil {
		t.Fatal(err)
	}
	prior, err := buildPlan(original, testSetupFacts(), roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildPlan(current, testSetupFacts(), roles, time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	if prior.ConfigIdentityRuntimeSpec != 0 || plan.ConfigIdentityRuntimeSpec != 455 || prior.PlanHash == plan.PlanHash || prior.ReleaseLockHash == plan.ReleaseLockHash || prior.ConfigHash != plan.ConfigHash || prior.ResolvedInputsHash != plan.ResolvedInputsHash {
		t.Fatal("migration approval lost its explicit pin or changed retained activation inputs")
	}
	if !reflect.DeepEqual(prior.Actions, plan.Actions) || !reflect.DeepEqual(prior.Roles, plan.Roles) || !reflect.DeepEqual(prior.Deployment, plan.Deployment) || prior.MaximumSpend != plan.MaximumSpend || prior.Limits != plan.Limits {
		t.Fatal("runtime identity migration changed action intents, custody or approved budgets")
	}
	withoutPin := *plan
	withoutPin.ConfigIdentityRuntimeSpec = 0
	withoutHash, err := withoutPin.hash()
	if err != nil || withoutHash == plan.PlanHash {
		t.Fatalf("plan approval did not bind the explicit runtime pin: %v", err)
	}
	for _, approved := range []*SetupPlan{prior, plan} {
		wire, err := json.Marshal(approved)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(wire, []byte(`"config_identity_runtime_spec"`)) != (approved == plan) {
			t.Fatal("plan runtime pin has the wrong legacy omission behavior")
		}
		hash, err := persistedSetupPlanHash(wire, approved.Schema)
		if err != nil || hash != approved.PlanHash {
			t.Fatalf("persisted approval did not reproduce its original wire hash: %v", err)
		}
	}
	stateDir := t.TempDir()
	wire, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(stateDir, "plan.json"), wire, 0o600); err != nil {
		t.Fatal(err)
	}
	if reopened, err := loadPersistedPlan(current, stateDir); err != nil || reopened.ConfigIdentityRuntimeSpec != 455 {
		t.Fatalf("current pinned approval could not restart: %v", err)
	}
}

// Current launch refuses an omitted or substituted pin before dialing, even
// when a caller recomputes the outer plan hash. Runtime authority stays461.
func TestCoordinatorRepairCarryRuntimeIdentityRejectsCurrentAuthoritySubstitution(t *testing.T) {
	t.Parallel()
	_, current := runtimeConfigIdentityTestConfigs(t)
	roles, err := derivePublicRoles(current)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildPlan(current, testSetupFacts(), roles, time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	for _, pin := range []uint32{0, 454, 458} {
		changed := *plan
		changed.ConfigIdentityRuntimeSpec = pin
		changed.PlanHash, err = changed.hash()
		if err != nil {
			t.Fatal(err)
		}
		wire, err := json.Marshal(changed)
		if err != nil {
			t.Fatal(err)
		}
		stateDir := t.TempDir()
		if err := atomicWrite(filepath.Join(stateDir, "plan.json"), wire, 0o600); err != nil {
			t.Fatal(err)
		}
		if got, err := loadPersistedPlan(current, stateDir); got != nil || !errors.Is(err, errPersistedPlanIdentityMismatch) {
			t.Fatalf("pin%d reopened as current authority: %v", pin, err)
		}
		if got, err := NewExecutor(t.Context(), current, stateDir, &changed, nil, nil); got != nil || err == nil || !strings.Contains(err.Error(), "runtime configuration identity differs") {
			t.Fatalf("pin%d reached executor transport admission: %v", pin, err)
		}
	}
	for _, item := range []struct {
		name   string
		change func(*ReleaseRuntimeLock)
	}{
		{name: "historical runtime", change: func(runtime *ReleaseRuntimeLock) { *runtime = validatorEvidenceRuntime455TestLock(t).Runtime }},
		{name: "code", change: func(runtime *ReleaseRuntimeLock) { runtime.CodeHash = "0x" + strings.Repeat("53", 32) }},
		{name: "metadata", change: func(runtime *ReleaseRuntimeLock) { runtime.MetadataHash = "0x" + strings.Repeat("64", 32) }},
		{name: "source", change: func(runtime *ReleaseRuntimeLock) { runtime.SourceCommit = strings.Repeat("75", 20) }},
	} {
		changed, lock := *current, *current.Release
		item.change(&lock.Runtime)
		changed.Release = &lock
		if err := validateRuntimeConfigIdentityPlan(&changed, plan); err == nil {
			t.Fatalf("%s inherited current runtime authority", item.name)
		}
	}
}

// The actual completed-repair reader receives a genuine omitted-pin455
// source and current461 configuration. No signed source bytes are rewritten.
func TestCoordinatorRepairCarryRuntimeIdentityAuthenticatesOriginalRepair(t *testing.T) {
	var originalPublic PublicManifest
	var originalInputs resolvedPlanPublicInputs
	original := newValidatorEvidenceCarryConfiguredTestFixture(t, false, true, false, func(cfg *ResolvedConfig) {
		cfg.Public.Chain.ExpectedRuntimeSpec = 455
		cfg.Public.Chain.ConfigIdentityRuntimeSpec = 0
		cfg.Release.Runtime = validatorEvidenceRuntime455TestLock(t).Runtime
		originalPublic = *cfg.Public
		originalInputs = resolvedPlanInputs(cfg)
	})
	fixture := newCoordinatorRepairCarrySourceFixture(t, original, nil)
	executor := fixture.executor
	current, public := *executor.cfg, originalPublic
	public.Chain.ExpectedRuntimeSpec, public.Chain.ConfigIdentityRuntimeSpec = 461, 455
	current.Public, current.Release = &public, testReleaseLockFixture(t)
	// The HTTP fixtures inject clients after approval. Their transport address
	// must not replace the immutable endpoint used by the original planner.
	current.OperationalEVM = originalInputs.OperationalEVM
	var err error
	current.ConfigHash, err = releaseConfigHash(current.Config, current.Public, current.Hyperparameters)
	if err != nil {
		t.Fatal(err)
	}
	retained := map[string][]byte{}
	for _, name := range []string{"journal.jsonl", "plans/" + stringsTrim0x(executor.plan.PlanHash) + ".json", coordinatorRepairDirectory + "/request.json", coordinatorRepairDirectory + "/result.json"} {
		retained[name], err = os.ReadFile(filepath.Join(executor.stateDir, name))
		if err != nil {
			t.Fatal(err)
		}
	}
	planWire := retained["plans/"+stringsTrim0x(executor.plan.PlanHash)+".json"]
	if bytes.Contains(planWire, []byte(`"config_identity_runtime_spec"`)) {
		t.Fatal("synthetic predecessor unexpectedly contains a later migration pin")
	}
	if err := atomicWrite(filepath.Join(executor.stateDir, "plan.json"), planWire, 0o600); err != nil {
		t.Fatal(err)
	}
	prior, err := readPersistedPlan(executor.stateDir)
	if err != nil || prior.PlanHash != executor.plan.PlanHash || prior.ValidatorEvidenceSource.ReleaseLock.Runtime.SpecVersion != 455 {
		t.Fatalf("actual setup predecessor reader lost original455 approval: %v", err)
	}
	originalInputsHash, err := canonicalHashHex(originalInputs)
	if err != nil || originalInputsHash != prior.ResolvedInputsHash {
		t.Fatalf("fixture snapshot does not reproduce original resolved inputs: got=%s want=%s error=%v", originalInputsHash, prior.ResolvedInputsHash, err)
	}
	if got, err := loadPersistedPlan(&current, executor.stateDir); got != nil || !errors.Is(err, errPersistedPlanIdentityMismatch) {
		t.Fatalf("unmigrated predecessor became current launch authority: %v", err)
	}
	entries := executor.journal.Entries()
	observed, err := authenticateCoordinatorRepairCarry(t.Context(), &current, executor.stateDir, prior, entries, executor.deployer.client, executor.independentEVM)
	if err != nil || observed == nil || !reflect.DeepEqual(observed.reference, fixture.reference) {
		t.Fatalf("original signed repair lost authority after explicit runtime migration: %v", err)
	}
	for _, change := range []func(*ResolvedConfig){
		func(cfg *ResolvedConfig) { cfg.Public.Chain.ConfigIdentityRuntimeSpec = 0 },
		func(cfg *ResolvedConfig) { cfg.Public.Chain.ExpectedBlockSeconds++ },
	} {
		changed, changedPublic := current, *current.Public
		changed.Public = &changedPublic
		change(&changed)
		changed.ConfigHash, err = releaseConfigHash(changed.Config, changed.Public, changed.Hyperparameters)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := authenticateCoordinatorRepairCarry(t.Context(), &changed, executor.stateDir, prior, entries, executor.deployer.client, executor.independentEVM); got != nil || err == nil || !strings.Contains(err.Error(), "configured strict domain differs") {
			t.Fatalf("changed configuration inherited original signed repair: %v", err)
		}
	}
	roles, err := derivePublicRoles(&current)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildPlan(&current, &prior.LiveFacts, roles, time.Unix(2, 0))
	if err != nil {
		t.Fatalf("build current migration approval: %v", err)
	}
	if plan.ConfigIdentityRuntimeSpec != 455 || plan.ConfigHash != prior.ConfigHash {
		t.Fatalf("setup migration changed activation identity: pin=%d config=%s want=%s", plan.ConfigIdentityRuntimeSpec, plan.ConfigHash, prior.ConfigHash)
	}
	if plan.ResolvedInputsHash != prior.ResolvedInputsHash {
		t.Fatalf("setup migration changed approved resolved inputs: got=%s want=%s", plan.ResolvedInputsHash, prior.ResolvedInputsHash)
	}
	if len(plan.Actions) != len(executor.plan.Actions) {
		t.Fatalf("setup migration changed action count: got=%d want=%d", len(plan.Actions), len(executor.plan.Actions))
	}
	for index, action := range plan.Actions {
		if !reflect.DeepEqual(action, executor.plan.Actions[index]) {
			t.Fatalf("setup migration changed original action %s at index %d", action.ID, index)
		}
	}
	if plan.MaximumSpend != prior.MaximumSpend || plan.Limits != prior.Limits {
		t.Fatalf("setup migration changed budget authority: maximum=%+v want=%+v limits=%+v want=%+v", plan.MaximumSpend, prior.MaximumSpend, plan.Limits, prior.Limits)
	}
	changedRoute := current
	changedRoute.OperationalEVM = "http://changed-rpc.example"
	rerender, err := buildPlan(&changedRoute, &prior.LiveFacts, roles, time.Unix(2, 0))
	if err != nil {
		t.Fatalf("build synthetic changed-route approval: %v", err)
	}
	if rerender.ResolvedInputsHash == plan.ResolvedInputsHash {
		t.Fatal("changed transport reused the original resolved launch inputs")
	}
	if rerender.ConfigHash != plan.ConfigHash {
		t.Fatalf("changed transport changed activation identity: got=%s want=%s", rerender.ConfigHash, plan.ConfigHash)
	}
	if len(rerender.Actions) != len(plan.Actions) {
		t.Fatalf("changed transport changed action count: got=%d want=%d", len(rerender.Actions), len(plan.Actions))
	}
	renderCount := 0
	for index, action := range rerender.Actions {
		originalAction := plan.Actions[index]
		if action.ID == "config.render" {
			renderCount++
			if action.IntentHash == originalAction.IntentHash || action.Parameters["resolved_inputs_hash"] != rerender.ResolvedInputsHash {
				t.Fatal("changed transport reused the original config.render approval")
			}
		} else if !reflect.DeepEqual(action, originalAction) {
			t.Fatalf("changed transport altered unrelated action %s", action.ID)
		}
	}
	if renderCount != 1 {
		t.Fatalf("changed transport has %d config.render actions, want 1", renderCount)
	}
	for name, before := range retained {
		after, err := os.ReadFile(filepath.Join(executor.stateDir, name))
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("runtime migration rewrote retained %s: %v", name, err)
		}
	}
	if !reflect.DeepEqual(entries, executor.journal.Entries()) || fixture.reader.sends.Load() != 0 || fixture.independent.sends.Load() != 0 {
		t.Fatal("runtime identity replay changed history or sent a transaction")
	}
}
