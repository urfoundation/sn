// No-trim fixtures reuse the one-role v5 composition and its real census. Their
// census policy lists no removals, so the retained review carries no selection.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// The golden two-role plan in each historical wire form: v1 has no production
// inspections or role labels, and neither v1 nor v2 has a root role.
func bootstrapChainLegacyGoldenPlan(schema string) bootstrapChainPlan {
	plan := bootstrapChainGoldenPlan(schema)
	plan.Config.RootValidator, plan.RootInspection = nil, nil
	if schema == bootstrapChainConfigSchemaV1 {
		plan.ValidatorInspections = nil
		for i := range plan.Config.Validators {
			role := &plan.Config.Validators[i]
			role.Role, role.Implementation, role.ApprovalPublicKey = "", "", ""
		}
	}
	return plan
}

// The golden one-role v5 plan, whose sole UR hotkey also holds the root seat.
func bootstrapChainSoleGoldenPlan() bootstrapChainPlan {
	plan := bootstrapChainGoldenPlan(bootstrapChainConfigSchemaV5)
	plan.Config.Validators, plan.ValidatorInspections = plan.Config.Validators[:1], plan.ValidatorInspections[:1]
	sole := &plan.Config.Validators[0]
	sole.Role = "sole"
	plan.Config.RootValidator.Hotkey, plan.Config.RootValidator.Coldkey = sole.Hotkey, sole.Coldkey
	policy := &plan.RootInspection.Plan.PassiveService.Policy
	policy.Hotkey, policy.Coldkey = sole.Hotkey, sole.Coldkey
	return plan
}

// These digests were computed before the owner-trim mode existed. Every
// trim-mode preparation, its journal and its child markers depend on them.
func TestBootstrapChainTrimModePlanHashesUnchanged(t *testing.T) {
	for _, item := range []struct {
		name   string
		plan   bootstrapChainPlan
		digest string
	}{
		{name: "v1", plan: bootstrapChainLegacyGoldenPlan(bootstrapChainConfigSchemaV1), digest: "sha256:877c6e34a23e66f822e4a2f07b0f1dfc740110b7bbb013c059a118607f976b06"},
		{name: "v2", plan: bootstrapChainLegacyGoldenPlan(bootstrapChainConfigSchemaV2), digest: "sha256:ec962b45eb2dd84f2be66930d7aaf8420ab42e374676f4fe0747acbb3b3d659e"},
		{name: "v3", plan: bootstrapChainGoldenPlan(bootstrapChainConfigSchema), digest: "sha256:f8b8991965be5b5e10ff5e826daf257282cd6bbba43e51da2bc9c01d214b6e29"},
		{name: "v4", plan: bootstrapChainGoldenPlan(bootstrapChainConfigSchemaV4), digest: "sha256:92d17e3e2047762bf662d73532e96c38950a71ecf79140ce631e5fce241e5ecd"},
		{name: "v5-trim", plan: bootstrapChainSoleGoldenPlan(), digest: "sha256:7b82e75eb563e3a7fc88f878682581eb3dc79b738c3694eeb649c1e15d5ac780"},
	} {
		raw, err := json.Marshal(item.plan)
		if err != nil || bytes.Contains(raw, []byte("owner_trim_mode")) || bytes.Contains(raw, []byte("owner_trim_phase")) {
			t.Fatal("trim-mode plan bytes gained an owner-trim mode or phase field", item.name, err)
		}
		if got := bootstrapChainPlanHash(item.plan); got != item.digest {
			t.Errorf("%s trim-mode plan hash changed: %s", item.name, got)
		}
	}
	// Every report that carries the phase names omits the decision in trim mode.
	for _, report := range []any{bootstrapChainResult{}, bootstrapChainReadiness{}, bootstrapChainContractReadiness{}, bootstrapContractRolePlan{},
		bootstrapContractReceiptAdmission{}, bootstrapContractCurrentAdmission{}} {
		raw, err := json.Marshal(report)
		if err != nil || bytes.Contains(raw, []byte("owner_trim_phase")) {
			t.Fatalf("trim-mode %T gained an owner-trim phase field: %v", report, err)
		}
	}
	trim := bootstrapChainSoleGoldenPlan()
	plan := bootstrapChainSoleGoldenPlan()
	plan.Config.OwnerTrimMode, plan.OwnerTrimPhase = bootstrapChainOwnerTrimNone, bootstrapChainOwnerTrimNotSelected
	raw, err := json.Marshal(plan)
	if err != nil || !bytes.Contains(raw, []byte(`"owner_trim_mode":"none"`)) || !bytes.Contains(raw, []byte(`"owner_trim_phase":"not-selected-owner-decision"`)) ||
		!slices.Equal(plan.PendingChainPhases, bootstrapChainSoleTestPhases()) || bootstrapChainPlanHash(plan) == bootstrapChainPlanHash(trim) {
		t.Fatal("no-trim selection is not sealed beside the unchanged phase names", err)
	}
}

// Reapprove the census policy with no removals at the observed capacity, then
// rebuild the retained review from the real census and select no trim.
func bootstrapChainNoTrimConvert(t *testing.T, f *bootstrapChainFixture, configure func(*subnetCensusPolicy)) {
	t.Helper()
	var policy subnetCensusPolicy
	var retained ownerTrimPlan
	policyRaw, policyErr := readBootstrapChainInput(t.Context(), f.config.OwnerTrimPolicy, maxRpcReplyBytes)
	trimRaw, trimErr := readBootstrapChainInput(t.Context(), f.config.OwnerTrimPlan, maximumOwnerTrimPlanBytes)
	if err := errors.Join(policyErr, trimErr); err != nil || decodePlanJson(policyRaw, &policy) != nil || decodePlanJson(trimRaw, &retained) != nil {
		t.Fatal("synthetic original policy or review unavailable", err)
	}
	capacity := retained.Census.Observation.MaximumUids
	policy.Remove, policy.TrimMaximumUids = []subnetIdentityExpectation{}, &capacity
	if configure != nil {
		configure(&policy)
	}
	f.config.OwnerTrimPolicy = bootstrapRootTestWrite(t, f.config.OwnerTrimPolicy.Path, policy)
	preview, err := f.client.readSubnetPreview(t.Context(), policy, f.config.OwnerTrimPolicy.Sha256)
	if err != nil {
		t.Fatal(err)
	}
	trim, err := buildOwnerTrimPlan(t.Context(), policy, preview)
	if err != nil {
		t.Fatal(err)
	}
	f.config.OwnerTrimPlan = bootstrapRootTestWrite(t, f.config.OwnerTrimPlan.Path, trim)
	f.config.OwnerTrimMode = bootstrapChainOwnerTrimNone
}

// The launch form: one sole role and, when shared, its hotkey on root UID 0.
func newBootstrapChainNoTrimFixture(t *testing.T, shared bool) *bootstrapChainFixture {
	t.Helper()
	f := newBootstrapChainSoleFixture(t, shared, nil)
	bootstrapChainNoTrimConvert(t, f, nil)
	bootstrapChainSoleLoad(t, f)
	return f
}

// The retained review the accepted no-trim plan pins, decoded from its file.
func bootstrapChainNoTrimReview(t *testing.T, f *bootstrapChainFixture) ownerTrimPlan {
	t.Helper()
	var trim ownerTrimPlan
	raw, err := readBootstrapChainInput(t.Context(), f.config.OwnerTrimPlan, maximumOwnerTrimPlanBytes)
	if err != nil || decodePlanJson(raw, &trim) != nil {
		t.Fatal("retained no-trim review unavailable", err)
	}
	return trim
}

// The explicit selection is sealed under v5's domain with all five phase names;
// unlisted miners stay unresolved while the sole validator stays preserved.
// Local preparation needs no owner-trim marker, journal or custody.
func TestBootstrapChainNoTrimSealsCensusAndPreparesOffline(t *testing.T) {
	for _, shared := range []bool{true, false} {
		f := newBootstrapChainNoTrimFixture(t, shared)
		f.requireFreshJournals(t)
		plan, trim := f.preparation.Plan, bootstrapChainNoTrimReview(t, f)
		census := trim.Census.Observation
		if plan.Schema != bootstrapChainPlanSchemaV5 || plan.Config.OwnerTrimMode != "none" || plan.OwnerTrimPhase != "not-selected-owner-decision" ||
			!slices.Equal(plan.PendingChainPhases, bootstrapChainSoleTestPhases()) || plan.OwnerTrimContentHash != trim.ContentHash ||
			!slices.Equal(plan.OwnerTrimBlockers, trim.ExecutionBlockers) || !slices.Contains(plan.OwnerTrimBlockers, "OWNER_TRIM_NO_SAFE_REMOVAL_CANDIDATE") ||
			trim.Best != nil || len(census.Remove) != 0 || !slices.Contains(census.Blockers, "NO_EXPLICIT_REMOVAL_SCOPE") ||
			!slices.ContainsFunc(census.Blockers, func(blocker string) bool { return strings.HasPrefix(blocker, "IDENTITY_ROLE_UNRESOLVED:") }) ||
			!bootstrapChainNoTrimCensusPreserves(census, plan.Config.Validators[0]) || plan.NetworkEffects || plan.NativeSigning || plan.ActivationReady {
			t.Fatalf("no-trim preparation lost its census, protection or sealed decision: %+v", plan)
		}
		unsealed := plan
		unsealed.ContentHash = ""
		raw, err := json.Marshal(unsealed)
		digest := sha256.Sum256(append([]byte("urnetwork-mainnet-bootstrap-chain-preparation-v5\x00"), raw...))
		if err != nil || plan.ContentHash != "sha256:"+hex.EncodeToString(digest[:]) || !bytes.Contains(raw, []byte(`"owner_trim_mode":"none"`)) {
			t.Fatal("no-trim selection is outside the v5 seal", err)
		}
		var stdout, stderr bytes.Buffer
		if code := f.command(t.Context(), "plan", &stdout, &stderr); code != 0 {
			t.Fatalf("plan exit %d: %s", code, stderr.String())
		}
		var decoded bootstrapChainPlan
		if err := decodePlanJson(stdout.Bytes(), &decoded); err != nil || !reflect.DeepEqual(decoded, plan) {
			t.Fatal("public plan differs from the accepted no-trim preparation", err)
		}
		contract := bootstrapContractTestResult(t, bootstrapPlanTestCommand(t, []string{"bootstrap-chain", "contract-plan", "--config", f.path}, 3))
		if contract.PlanHash != plan.ContentHash || !slices.Equal(contract.PendingChainPhases, plan.PendingChainPhases) || contract.OwnerTrimPhase != plan.OwnerTrimPhase {
			t.Fatal("contract review lost the no-trim decision or its phases", contract)
		}
		first := f.result(t, "apply")
		before := f.journals(t)
		if first.Schema != "urnetwork-mainnet-bootstrap-chain-result-v5" || first.PlanHash != plan.ContentHash || !first.LocalPreparationComplete ||
			first.OwnerTrimStatus != "retained-census-trim-not-selected" || first.OwnerTrimPhase != "not-selected-owner-decision" ||
			!slices.Equal(first.PendingChainPhases, plan.PendingChainPhases) || first.UrValidatorsStatus != "one-signed-production-config-verified-live-admission-pending" ||
			first.NetworkEffects || first.ActivationReady || first.Root.Broadcasts != 0 || len(before) != 6 {
			t.Fatalf("no-trim apply changed scope or claimed authority: %+v", first)
		}
		if again := f.result(t, "resume"); !reflect.DeepEqual(first, again) || !reflect.DeepEqual(before, f.journals(t)) || len(f.contracts.counts) != 0 {
			t.Fatal("no-trim resume changed original custody or contacted a chain")
		}
		stdout.Reset()
		if code := f.command(t.Context(), "contract-readiness", &stdout, &stderr); code != 3 {
			t.Fatalf("contract readiness exit %d: %s", code, stderr.String())
		}
		retained := bootstrapContractTestResult(t, stdout.Bytes())
		if !retained.CustodyInspectionComplete || retained.LocalPreparation == nil || retained.OwnerTrimPhase != plan.OwnerTrimPhase ||
			!slices.Equal(retained.PendingChainPhases, plan.PendingChainPhases) || !reflect.DeepEqual(before, f.journals(t)) {
			t.Fatalf("no-trim contract readiness lost retained custody or the decision: %+v", retained)
		}
		state := filepath.Join(f.config.RunDirectory, ownerTrimStateFile)
		for _, path := range []string{state, state + ".lock"} {
			if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) || slices.Contains(f.preparation.protectedPaths(), path) {
				t.Fatal("no-trim preparation required owner-trim custody", path, err)
			}
		}
	}
}

// The readiness receipt records the owner's decision on the unexecuted phase
// while observing the sole validator and root seat at one finalized block.
func TestBootstrapChainNoTrimReadinessRecordsOwnerDecision(t *testing.T) {
	f := newBootstrapChainNoTrimFixture(t, true)
	f.result(t, "apply")
	before := f.journals(t)
	server := bootstrapReadinessTestServer(t, f.census)
	var stdout, stderr bytes.Buffer
	if code := f.command(t.Context(), "readiness", &stdout, &stderr, "--rpc", server.URL); code != 0 {
		t.Fatalf("readiness exit %d: %s", code, stderr.String())
	}
	var result bootstrapChainReadiness
	if err := decodePlanJson(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	claimed := result.ContentHash
	result.ContentHash = ""
	if claimed != rootObjectHash(result) || result.Status != "observed-prerequisites" || !result.ObservationComplete || result.PlanHash != f.preparation.Plan.ContentHash ||
		result.OwnerTrimPhase != "not-selected-owner-decision" || !slices.Equal(result.PendingChainPhases, bootstrapChainSoleTestPhases()) || result.Census == nil ||
		len(result.UrValidators) != 1 || result.UrValidators[0].Observed == nil || len(result.UrValidators[0].ObservationBlockers) != 0 || result.RootValidator.Observed == nil ||
		result.ActivationReady || result.CurrentAuthorityVerified || result.NativeSigning || result.NetworkEffects || !reflect.DeepEqual(before, f.journals(t)) {
		t.Fatalf("no-trim readiness lost the owner decision, a role or custody: %+v", result)
	}
}

// The single-role evidence graph and its declaration carry the same decision.
func TestBootstrapChainNoTrimContractRolePlan(t *testing.T) {
	f := newBootstrapSuccessorCommandFixture(t)
	bootstrapRootPassiveConvert(t, f, nil, nil)
	bootstrapChainSoleConvert(t, f)
	bootstrapChainSoleShareRoot(t, f)
	bootstrapChainNoTrimConvert(t, f, nil)
	bootstrapChainSoleLoad(t, f)
	bootstrapContractRoleBind(t, f)
	if !f.preparation.Plan.Config.noOwnerTrim() {
		t.Fatal("role binding lost the explicit no-trim selection")
	}
	var stdout, stderr bytes.Buffer
	if code := runMain(f.storageContext(t.Context()), []string{"bootstrap-chain", "contract-role-plan", "--config", f.path}, &stdout, &stderr); code != 0 {
		t.Fatal("no-trim contract-role plan refused", code, stderr.String())
	}
	var result bootstrapContractRolePlan
	if err := decodePlanJson(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	seal := result.ContentHash
	result.ContentHash = ""
	if seal != rootObjectHash(result) || result.PreparationHash != f.preparation.Plan.ContentHash || result.OwnerTrimPhase != "not-selected-owner-decision" ||
		!slices.Equal(result.PendingChainPhases, bootstrapChainSoleTestPhases()) || len(result.Validators) != 1 || result.Validators[0].Role != "sole" ||
		!result.DeclarationsVerified || result.InstallationComplete || result.ActivationReady || result.NetworkEffects {
		t.Fatal("no-trim contract-role output lost its decision or declaration", result)
	}
}

// The no-trim selection is explicit, v5-only and never a way to ignore a trim
// request; it keeps every protected-identity proof the census supplies.
func TestBootstrapChainNoTrimRefusesRemovalsAndUnprovenScope(t *testing.T) {
	unregistered, block := "0x"+strings.Repeat("77", 32), uint64(40)
	for _, item := range []struct {
		name       string
		diagnostic string
		build      func(*testing.T) *bootstrapChainFixture
	}{
		{name: "listed-removals-with-trim-review", diagnostic: "lists removals", build: func(t *testing.T) *bootstrapChainFixture {
			f := newBootstrapChainSoleFixture(t, true, nil)
			f.config.OwnerTrimMode = bootstrapChainOwnerTrimNone
			return f
		}},
		{name: "listed-removals-rebuilt-census", diagnostic: "lists removals", build: func(t *testing.T) *bootstrapChainFixture {
			f := newBootstrapChainSoleFixture(t, true, nil)
			var original subnetCensusPolicy
			raw, err := readBootstrapChainInput(t.Context(), f.config.OwnerTrimPolicy, maxRpcReplyBytes)
			if err != nil || decodePlanJson(raw, &original) != nil || len(original.Remove) == 0 {
				t.Fatal("synthetic removal scope unavailable", err)
			}
			bootstrapChainNoTrimConvert(t, f, func(policy *subnetCensusPolicy) { policy.Remove = original.Remove })
			return f
		}},
		{name: "trimmed-capacity", diagnostic: "observed capacity", build: func(t *testing.T) *bootstrapChainFixture {
			f := newBootstrapChainSoleFixture(t, true, nil)
			bootstrapChainNoTrimConvert(t, f, func(policy *subnetCensusPolicy) { *policy.TrimMaximumUids-- })
			return f
		}},
		{name: "absent-mode-is-trim", diagnostic: "sealed safe partial plan", build: func(t *testing.T) *bootstrapChainFixture {
			f := newBootstrapChainNoTrimFixture(t, true)
			f.config.OwnerTrimMode = ""
			return f
		}},
		{name: "other-spelling", diagnostic: "v5-only and its sole explicit value", build: func(t *testing.T) *bootstrapChainFixture {
			f := newBootstrapChainNoTrimFixture(t, true)
			f.config.OwnerTrimMode = "None"
			return f
		}},
		{name: "two-role-v4", diagnostic: "v5-only and its sole explicit value", build: func(t *testing.T) *bootstrapChainFixture {
			f := newBootstrapRootPassiveFixture(t)
			bootstrapChainNoTrimConvert(t, f, nil)
			return f
		}},
		{name: "two-role-v3", diagnostic: "v5-only and its sole explicit value", build: func(t *testing.T) *bootstrapChainFixture {
			f := newBootstrapChainFixture(t)
			bootstrapChainNoTrimConvert(t, f, nil)
			return f
		}},
		{name: "sole-not-declared-ur-validator", diagnostic: "preserved in the retained no-trim census", build: func(t *testing.T) *bootstrapChainFixture {
			f := newBootstrapChainSoleFixture(t, true, nil)
			sole := f.config.Validators[0].Hotkey
			bootstrapChainNoTrimConvert(t, f, func(policy *subnetCensusPolicy) {
				policy.Preserve = slices.DeleteFunc(policy.Preserve, func(identity subnetProtectedIdentity) bool { return identity.Hotkey == sole })
			})
			return f
		}},
		{name: "protected-identity-not-registered", diagnostic: "PROTECTED_GENERATION_NOT_REGISTERED", build: func(t *testing.T) *bootstrapChainFixture {
			f := newBootstrapChainSoleFixture(t, true, nil)
			bootstrapChainNoTrimConvert(t, f, func(policy *subnetCensusPolicy) {
				policy.Preserve = append(policy.Preserve, subnetProtectedIdentity{subnetIdentityExpectation: subnetIdentityExpectation{Hotkey: unregistered,
					Coldkey: "0x" + strings.Repeat("78", 32), RegistrationBlock: &block}, Roles: []string{"reserve"}})
			})
			return f
		}},
		{name: "protected-generation-changed", diagnostic: "SCOPED_IDENTITY_OWNER_OR_REGISTRATION_GENERATION_CHANGED", build: func(t *testing.T) *bootstrapChainFixture {
			f := newBootstrapChainSoleFixture(t, true, nil)
			bootstrapChainNoTrimConvert(t, f, func(policy *subnetCensusPolicy) {
				changed := *policy.Preserve[1].RegistrationBlock + 1
				policy.Preserve[1].RegistrationBlock = &changed
			})
			return f
		}},
	} {
		t.Run(item.name, func(t *testing.T) {
			item.build(t).rejectValidatorPlan(t, item.diagnostic)
		})
	}
}

// The seal check covers the decision itself: a resealed plan cannot drop,
// add or move the not-selected phase, and no older schema can carry it.
func TestBootstrapChainNoTrimPlanValidationBindsDecision(t *testing.T) {
	f := newBootstrapChainNoTrimFixture(t, true)
	trimMode := newBootstrapChainSoleFixture(t, true, nil).preparation.Plan
	if err := f.preparation.Plan.validate(); err != nil || trimMode.validate() != nil || trimMode.OwnerTrimPhase != "" || trimMode.Config.OwnerTrimMode != "" {
		t.Fatal("accepted no-trim or trim-mode plan refused", err)
	}
	for _, item := range []struct {
		name   string
		change func(*bootstrapChainPlan)
	}{
		{name: "phase-dropped", change: func(plan *bootstrapChainPlan) { plan.OwnerTrimPhase = "" }},
		{name: "mode-dropped", change: func(plan *bootstrapChainPlan) { plan.Config.OwnerTrimMode = "" }},
		{name: "phase-renamed", change: func(plan *bootstrapChainPlan) { plan.OwnerTrimPhase = "executed" }},
		{name: "trim-mode-phase-added", change: func(plan *bootstrapChainPlan) {
			*plan = trimMode
			plan.OwnerTrimPhase = bootstrapChainOwnerTrimNotSelected
		}},
		{name: "older-schema", change: func(plan *bootstrapChainPlan) {
			plan.Config.Schema, plan.Schema = bootstrapChainConfigSchemaV4, bootstrapChainPlanSchemaV4
			plan.PendingChainPhases = bootstrapChainPendingPhasesForSchema(bootstrapChainConfigSchemaV4)
		}},
	} {
		plan := f.preparation.Plan
		item.change(&plan)
		plan.ContentHash = bootstrapChainPlanHash(plan)
		if err := plan.validate(); err == nil || item.name != "older-schema" && !strings.Contains(err.Error(), "owner-trim mode") {
			t.Fatalf("%s: resealed plan kept or changed the decision: %v", item.name, err)
		}
	}
}

// Trim tooling has no review, action or custody for a sealed no-trim decision:
// every mode refuses before reading trim input, and even a prepared owner-trim
// marker stays unclaimed.
func TestBootstrapChainNoTrimRefusesOwnerTrimCommands(t *testing.T) {
	f := newBootstrapChainNoTrimFixture(t, true)
	f.result(t, "apply")
	state := filepath.Join(f.config.RunDirectory, ownerTrimStateFile)
	prepareMainnetSnapshotTest(t, state, "mainnet-owner-trim", ownerTrimStoreLimit)
	directory := filepath.Dir(f.path)
	metadata := filepath.Join(directory, "trim-metadata.hex")
	if err := os.WriteFile(metadata, []byte(f.census.metadataHex), 0600); err != nil {
		t.Fatal(err)
	}
	trimConfig, key := filepath.Join(directory, "absent-trim-config.json"), "0x"+strings.Repeat("7a", 32)
	before := f.journals(t)
	for _, mode := range [][]string{{"trim-plan", "--metadata", metadata}, {"trim-apply"}, {"trim-resume"}, {"trim-reconcile"},
		{"trim-multisig-plan", "--metadata", metadata, "--multisig-template", trimConfig}} {
		var stdout, stderr bytes.Buffer
		code := f.command(t.Context(), mode[0], &stdout, &stderr, append([]string{"--trim-config", trimConfig, "--trim-approval-key", key}, mode[1:]...)...)
		if code != 3 || stdout.Len() != 0 || !strings.Contains(stderr.String(), `sealed owner_trim_mode "none"`) || !strings.Contains(stderr.String(), "not-selected-owner-decision") {
			t.Fatalf("%s did not refuse the no-trim preparation clearly: %d %s", mode[0], code, stderr.String())
		}
	}
	if _, err := openOwnerTrimStore(f.storageContext(t.Context()), f.preparation, ownerTrimExecutionConfig{}, key, true); err == nil || !strings.Contains(err.Error(), "sealed no owner trim") {
		t.Fatal("owner trim custody opened for a no-trim preparation", err)
	}
	marker, err := os.ReadFile(state + ".lock")
	if _, stateErr := os.Lstat(state); err != nil || len(marker) != 0 || !errors.Is(stateErr, os.ErrNotExist) || !reflect.DeepEqual(before, f.journals(t)) {
		t.Fatal("refused trim tooling claimed owner-trim custody or changed preparation", err, stateErr)
	}
}
