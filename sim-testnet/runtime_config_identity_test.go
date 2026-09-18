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

	"github.com/urfoundation/sn/v2026/crv4"
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

// Current rendering changes independently of the signed consent domain. Only
// its local receipt must be replaced; transaction and history intents survive.
func requireRuntimeConfigOnlyRerenderTest(t *testing.T, prior, current *SetupPlan) {
	t.Helper()
	if len(prior.Actions) != len(current.Actions) {
		t.Fatal("runtime migration changed the action census")
	}
	renders := 0
	for index, previous := range prior.Actions {
		action := current.Actions[index]
		if action.ID == "config.render" {
			renders++
			if previous.IntentHash == action.IntentHash || action.Parameters["native_runtime_hash"] == "" || previous.Parameters["native_runtime_hash"] == action.Parameters["native_runtime_hash"] {
				t.Fatal("changed current runtime reused the prior render approval")
			}
			previous.IntentHash, action.IntentHash = "", ""
			previous.Parameters, action.Parameters = cloneStrings(previous.Parameters), cloneStrings(action.Parameters)
			delete(previous.Parameters, "native_runtime_hash")
			delete(action.Parameters, "native_runtime_hash")
		}
		if !reflect.DeepEqual(previous, action) {
			t.Fatalf("runtime migration changed non-runtime action authority for %s", action.ID)
		}
	}
	if renders != 1 {
		t.Fatalf("runtime migration has %d render actions", renders)
	}
}

// The real planner replaces only the derived render approval while preserving
// resolved inputs, transaction intents, deterministic roles and allowances.
func TestCoordinatorRepairCarryRuntimeIdentityRerendersOnlyLocalConfigs(t *testing.T) {
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
	requireRuntimeConfigOnlyRerenderTest(t, prior, plan)
	if !reflect.DeepEqual(prior.Roles, plan.Roles) || !reflect.DeepEqual(prior.Deployment, plan.Deployment) || prior.MaximumSpend != plan.MaximumSpend || prior.Limits != plan.Limits {
		t.Fatal("runtime identity migration changed custody or approved budgets")
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

// The last reviewed transition retains the normalized consent hash while
// replacing the exact local intent that carried stale operator runtime pins.
func TestRuntimeConfigNativeRefreshBindsReviewed460Migration(t *testing.T) {
	t.Parallel()
	_, current := runtimeConfigIdentityTestConfigs(t)
	previous, public, release := *current, *current.Public, *current.Release
	public.Chain.ExpectedRuntimeSpec = 460
	release.Runtime = runtime460ReviewedTestLock().Runtime
	release.Runtime.Image = current.Release.Runtime.Image
	previous.Public, previous.Release = &public, &release
	var err error
	previous.ConfigHash, err = releaseConfigHash(previous.Config, previous.Public, previous.Hyperparameters)
	if err != nil {
		t.Fatal(err)
	}
	roles, err := derivePublicRoles(current)
	if err != nil {
		t.Fatal(err)
	}
	// Construct archive bytes before persistence, as the existing historical
	// source fixtures do. The current planner cannot execute a 460 migration.
	prior, err := buildPlan(current, testSetupFacts(), roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	rebindValidatorEvidenceReleaseLockTest(t, prior, &release)
	previousRuntime, ok := crv4.ReviewedRuntimeArtifact(crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 460, TransactionVersion: 1, StateVersion: 1})
	if !ok {
		t.Fatal("former reviewed runtime fixture is absent")
	}
	previousRuntimeHash, err := canonicalHashHex(previousRuntime)
	if err != nil {
		t.Fatal(err)
	}
	for index := range prior.Actions {
		action := &prior.Actions[index]
		if action.ID == "config.render" {
			action.Parameters["native_runtime_hash"] = previousRuntimeHash
			action.IntentHash, err = actionIntentHash(*action)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	prior.PlanHash, err = prior.hash()
	if err != nil {
		t.Fatal(err)
	}
	priorWire, err := json.Marshal(prior)
	if err != nil {
		t.Fatal(err)
	}
	archived, err := decodePersistedPlanBytesForHistory(priorWire, true)
	if err != nil || archived == nil || archived.PlanHash != prior.PlanHash {
		t.Fatalf("synthetic 460 archive is invalid: %v", err)
	}
	if err := validateRuntimeConfigIdentityPlan(&previous, archived); err == nil {
		t.Fatal("historical 460 fixture became current execution authority")
	}
	plan, err := buildPlan(current, testSetupFacts(), roles, time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	if prior.ConfigHash != previous.ConfigHash || prior.ConfigHash != plan.ConfigHash || prior.ResolvedInputsHash != plan.ResolvedInputsHash || !reflect.DeepEqual(prior.Roles, plan.Roles) || prior.MaximumSpend != plan.MaximumSpend || prior.Limits != plan.Limits {
		t.Fatal("current runtime refresh changed consent, custody or spending limits")
	}
	requireRuntimeConfigOnlyRerenderTest(t, prior, plan)
}

// A durable ancestor success cannot hide a failed current render on restart,
// and preparation must leave that local work for the ordinary action executor.
func TestCoordinatorRepairCarryRuntimeIdentityInterruptedRenderRemainsPending(t *testing.T) {
	t.Parallel()
	original, current := runtimeConfigIdentityTestConfigs(t)
	roles, err := BuildRoleSecrets(current)
	if err != nil {
		t.Fatal(err)
	}
	publicRoles, err := derivePublicRoles(current)
	if err != nil {
		t.Fatal(err)
	}
	prior, err := buildPlan(original, testSetupFacts(), publicRoles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	// Reproduce an already sealed legacy action, which predates this field.
	for index := range prior.Actions {
		action := &prior.Actions[index]
		if action.ID == "config.render" {
			delete(action.Parameters, "native_runtime_hash")
			action.IntentHash, err = actionIntentHash(*action)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	prior.PlanHash, err = prior.hash()
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildPlan(current, testSetupFacts(), publicRoles, time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	plan.PriorPlanHashes = []string{prior.PlanHash}
	plan.PlanHash, err = plan.hash()
	if err != nil {
		t.Fatal(err)
	}
	executor := launchPreparationTestExecutor(t)
	executor.cfg, executor.roles, executor.plan = current, roles, plan
	if err := writeRunInputs(original, executor.stateDir, prior, roles); err != nil {
		t.Fatal(err)
	}
	if historical, err := readValidatorEvidenceHistoricalPlan(executor.stateDir, prior.PlanHash); err != nil || historical == nil || historical.PlanHash != prior.PlanHash {
		t.Fatalf("original render approval lost historical readability: %v", err)
	}
	if err := writeRunInputs(current, executor.stateDir, plan, roles); err != nil {
		t.Fatal(err)
	}
	previous := actionByID(t, prior, "config.render")
	appendLaunchPreparationTestReceipt(t, executor, previous)
	executor.plan.Actions = executor.plan.Actions[:len(executor.plan.Actions)-1]
	action := actionByID(t, plan, "config.render")
	for _, stage := range []JournalStage{StageIntent, StageFailed} {
		if err := executor.journal.Append(JournalEntry{DeploymentID: plan.DeploymentID, PlanHash: plan.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: stage}); err != nil {
			t.Fatal(err)
		}
	}
	if err := executor.journal.Close(); err != nil {
		t.Fatal(err)
	}
	executor.journal, err = OpenJournal(executor.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { executor.journal.Close() })
	before := validatorNamespaceTreeSnapshot(t, executor.stateDir)
	if _, found := executor.verifiedActionEntry(action); found {
		t.Fatal("interrupted current render inherited its ancestor's success")
	}
	if err := executor.collectCarriedActionHistory(t.Context()); err != nil {
		t.Fatalf("preparation verified stale derived inputs instead of scheduling their render: %v", err)
	}
	if err := executor.verifyActionDependencies(actionByID(t, plan, "accounts.provision")); err == nil || !strings.Contains(err.Error(), "config.render is not postcondition-verified") {
		t.Fatalf("a consumer could start before current rendering completed: %v", err)
	}
	if !reflect.DeepEqual(before, validatorNamespaceTreeSnapshot(t, executor.stateDir)) {
		t.Fatal("read-only preparation changed historical receipts or incomplete current work")
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
		// Operational timing may be independently revised; it is not a
		// coordinator-repair custody field.
		func(cfg *ResolvedConfig) { cfg.Public.Chain.ExpectedBlockSeconds++ },
	} {
		changed, changedPublic := current, *current.Public
		changed.Public = &changedPublic
		change(&changed)
		changed.ConfigHash, err = releaseConfigHash(changed.Config, changed.Public, changed.Hyperparameters)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := authenticateCoordinatorRepairCarry(t.Context(), &changed, executor.stateDir, prior, entries, executor.deployer.client, executor.independentEVM); got == nil || err != nil {
			t.Fatalf("safe configuration revision lost original signed repair: %v", err)
		}
	}
	changed, changedConfig := current, *current.Config
	changed.Config = &changedConfig
	changed.Config.Deployment.DeploymentID += "-other"
	changed.ConfigHash, err = releaseConfigHash(changed.Config, changed.Public, changed.Hyperparameters)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := authenticateCoordinatorRepairCarry(t.Context(), &changed, executor.stateDir, prior, entries, executor.deployer.client, executor.independentEVM); got != nil || err == nil || !strings.Contains(err.Error(), "configured strict domain differs") {
		t.Fatalf("changed deployment configuration inherited original signed repair: %v", err)
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
	requireRuntimeConfigOnlyRerenderTest(t, executor.plan, plan)
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
