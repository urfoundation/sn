// Fresh synthetic v4 plans approve passive root observation independently of
// both UR validators and never reuse the legacy root's native-action approval.
package main

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/protocol"
)

// All changes precede local preparation. The fixture then signs a new full
// service/config domain and both new UR configs with generated test-only keys.
func newBootstrapRootPassiveFixture(t *testing.T) *bootstrapChainFixture {
	t.Helper()
	return newBootstrapRootPassiveFixtureWithCensus(t, nil, nil)
}

// Native profiles and producer domains are selected before passive metadata
// authentication, independent signatures, or the first custody claim.
func newBootstrapRootPassiveFixtureWithCensus(t *testing.T, configure func(*rootRpcFixture, *subnetCensusPolicy), configureApproval func(*bootstrapChainValidatorFixture)) *bootstrapChainFixture {
	t.Helper()
	f := newBootstrapChainFixture(t)
	bootstrapRootPassiveConvert(t, f, configure, configureApproval)
	return f
}

// Any complete v3 fixture, including one with the full evidence graph, can be
// reapproved for passive v4 observation before its first custody claim.
func bootstrapRootPassiveConvert(t *testing.T, f *bootstrapChainFixture, configure func(*rootRpcFixture, *subnetCensusPolicy), configureApproval func(*bootstrapChainValidatorFixture)) {
	t.Helper()
	var policy subnetCensusPolicy
	raw, err := readBootstrapChainInput(t.Context(), f.config.OwnerTrimPolicy, maxRpcReplyBytes)
	if err != nil || decodePlanJson(raw, &policy) != nil {
		t.Fatal("synthetic original policy unavailable", err)
	}
	if configure != nil {
		configure(f.census, &policy)
	}
	rootPassiveTestMetadata(t, f.census)
	policy.RuntimeSourceCommit, policy.RuntimeMetadataHash = rootPassiveSource, f.census.policy.RuntimeMetadataHash
	f.config.OwnerTrimPolicy = bootstrapRootTestWrite(t, f.config.OwnerTrimPolicy.Path, policy)
	scope := f.root.plan.identityScope()
	rootPolicy := rootValidatorPolicy{Schema: rootPassivePolicySchema, Role: scope.Role, NativeChain: scope.NativeChain, GenesisHash: scope.GenesisHash, EvmChainId: scope.EvmChainId,
		StorageProfile: rootPassiveStorageProfile, RuntimeSourceCommit: rootPassiveSource, RuntimeVersion: scope.RuntimeVersion, RuntimeCodeHash: scope.RuntimeCodeHash,
		RuntimeMetadataHash: policy.RuntimeMetadataHash, Hotkey: scope.Hotkey, Coldkey: scope.Coldkey, ExpectedSeat: new(scope.Seat), MinimumStakeRao: "80",
		ExpectedDelegateTake: new(uint16(1234)), BasketStrategy: "accumulate_in_place", DelegationStrategy: "observe_existing", ValidFromBlock: 90, ValidThroughBlock: 200}
	hotkey, _ := hex.DecodeString(scope.Hotkey[2:])
	f.census.set(t, "TotalHotkeyAlpha", binary.LittleEndian.AppendUint64(nil, 100), hotkey, []byte{0, 0})
	f.census.set(t, "Delegates", binary.LittleEndian.AppendUint16(nil, 1234), hotkey)
	f.census.set(t, "AutoParentDelegationEnabled", []byte{1}, hotkey)
	f.census.set(t, "ValidatorPermit", subnetTestVector([]byte{0, 0, 1, 1, 0, 0}, 1), []byte{25, 0})
	preview, err := f.client.readSubnetPreview(t.Context(), policy, f.config.OwnerTrimPolicy.Sha256)
	if err != nil {
		t.Fatal(err)
	}
	trim, err := buildOwnerTrimPlan(t.Context(), policy, preview)
	if err != nil {
		t.Fatal(err)
	}
	f.config.OwnerTrimPlan = bootstrapRootTestWrite(t, f.config.OwnerTrimPlan.Path, trim)
	f.contracts.config.Plan.Runtime.RuntimeSourceCommit, f.contracts.config.Plan.Runtime.RuntimeMetadataHash = rootPassiveSource, policy.RuntimeMetadataHash
	f.contracts.publishConfig()
	f.config.Contracts = bootstrapRootTestWrite(t, f.contracts.configPath, f.contracts.config)
	for i, v := range f.validators {
		v.config.RuntimeMetadataHash = policy.RuntimeMetadataHash
		v.approval.Proposal.Runtime.SourceCommit, v.approval.Proposal.Runtime.MetadataHash = rootPassiveSource, bootstrapChainTestAccount(t, policy.RuntimeMetadataHash)
		v.approval.ValidFromNativeBlock = 100
		v.approval.Production.ActivationNativeHash = bootstrapChainTestAccount(t, testFinalizedHash)
		if configureApproval != nil {
			configureApproval(v)
		}
		f.config.Validators[i].Config = v.publish(t)
	}
	server := rootFixtureServer(t, f.census)
	service := rootPassiveServiceConfig{Schema: rootPassiveServiceSchema, Policy: rootPolicy, RpcUrl: server.URL, ReadRetrySeconds: 60,
		CheckpointPath: filepath.Join(f.config.RunDirectory, "passive-root-checkpoint.json"), MaximumSamples: 1, IntervalSeconds: 1, StallAfterSeconds: 300}
	prepareMainnetSnapshotTest(t, service.CheckpointPath, "mainnet-monitor-checkpoint", maxRpcReplyBytes)
	f.root.config.Schema = bootstrapRootPassiveConfigSchema
	f.root.config.RootService = bootstrapRootTestWrite(t, f.root.config.RootService.Path, service)
	f.config.Root = bootstrapRootTestWrite(t, f.root.configPath, f.root.config)
	f.root.plan, err = loadBootstrapRootPlan(t.Context(), f.root.configPath)
	if err != nil {
		t.Fatal(err)
	}
	f.config.Schema = bootstrapChainConfigSchemaV4
	f.config.RootValidator.Strategy, f.config.RootValidator.Implementation, f.config.RootValidator.ActionApprovalPublicKey = rootPassiveStrategy, "sn/mainnet/root-passive-service", ""
	f.rootRole.approval = bootstrapChainRootApproval{Schema: bootstrapChainRootPassiveApprovalSchema, DeploymentId: f.config.DeploymentId,
		RootPlanHash: f.root.plan.ContentHash, ServiceConfigHash: f.root.plan.serviceHash()}
	f.rootRole.sign(t)
	bootstrapRootTestWrite(t, f.path, f.config)
	f.preparation, err = loadBootstrapChainPreparation(t.Context(), f.path)
	if err != nil {
		t.Fatal(err)
	}
}

// Three real preparation journals retain contracts and passive config. No
// root native custody store, action signature or allowance is fabricated.
func TestBootstrapRootPassiveComposesAndResumesBothRoles(t *testing.T) {
	f := newBootstrapRootPassiveFixture(t)
	f.requireFreshJournals(t)
	if len(f.preparation.Plan.ValidatorInspections) != 2 {
		t.Fatal("planning omitted UR roles")
	}
	first := f.result(t, "apply")
	before := f.journals(t)
	if first.Schema != "urnetwork-mainnet-bootstrap-chain-result-v4" || !first.RootValidatorConfigVerified || !first.UrValidatorConfigsVerified ||
		first.Root.SignatureStatus != "not-applicable-passive-observation" || first.Root.ExtrinsicHash != "" || first.Root.Broadcasts != 0 ||
		first.NetworkEffects || first.ActivationReady || len(before) != 6 || !reflect.DeepEqual(first, f.result(t, "resume")) || !reflect.DeepEqual(before, f.journals(t)) {
		t.Fatal("passive preparation changed old authority, native liabilities or retained state", first)
	}
	if _, err := os.Stat(f.root.plan.PassiveService.CheckpointPath); !os.IsNotExist(err) {
		t.Fatal("preparation created a monitor checkpoint")
	}
}

// Current finalized readiness reads both roles at one exact block and retains
// separate root observations. The result cannot approve a native root action.
func TestBootstrapRootPassiveReadinessUsesCurrentSupportedProfile(t *testing.T) {
	f := newBootstrapRootPassiveFixture(t)
	f.result(t, "apply")
	before := f.journals(t)
	result, err := f.client.observeBootstrapChainReadiness(f.storageContext(t.Context()), f.preparation)
	if err != nil || !result.ObservationComplete || result.Status != "observed-prerequisites" || result.PassiveRoot == nil ||
		!result.PassiveRoot.Observation.ReadOnlyReady || result.ActivationReady || result.NativeSigning || result.NetworkEffects || result.RootValidator.Observed == nil ||
		!reflect.DeepEqual(before, f.journals(t)) {
		t.Fatalf("passive readiness: %+v %v", result, err)
	}
	if result.PassiveRoot.Observation.Identity.FinalizedHash != result.Census.Observation.Identity.FinalizedHash {
		t.Fatal("roles used different finalized state")
	}
}

// The public service invokes the real monitor and persists its own checkpoint.
// Native submission/signing flags have no command or configuration surface.
func TestRootPassiveServiceCommandRunsApprovedObserver(t *testing.T) {
	f := newBootstrapRootPassiveFixture(t)
	f.result(t, "apply")
	config := rootPassiveRuntimeConfig{Schema: rootPassiveRuntimeSchema, Root: f.config.Root, Role: *f.config.RootValidator}
	ref := bootstrapRootTestWrite(t, filepath.Join(filepath.Dir(f.path), "passive-runtime.json"), config)
	var output, diagnostics bytes.Buffer
	args := []string{"root-passive-service", "run", "--config", ref.Path, "--accept-runtime-sha256", ref.Sha256}
	if code := runMain(f.storageContext(t.Context()), args, &output, &diagnostics); code != 0 {
		t.Fatal("passive run failed", code, diagnostics.String(), output.String())
	}
	var event rootMonitorEvent
	if err := json.Unmarshal(output.Bytes(), &event); err != nil || event.Status != "ready" || event.Observation == nil {
		t.Fatal("passive service did not observe root", err, output.String())
	}
	before, err := os.ReadFile(f.root.plan.PassiveService.CheckpointPath)
	if err != nil {
		t.Fatal(err)
	}
	if code := runMain(f.storageContext(t.Context()), args, io.Discard, &diagnostics); code != 0 {
		t.Fatal("passive restart failed", code, diagnostics.String())
	}
	after, err := os.ReadFile(f.root.plan.PassiveService.CheckpointPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("same finalized state changed checkpoint", err)
	}
	for _, extra := range [][]string{{"--submit"}, {"--signature-file", "synthetic-signature"}, {"--samples", "2"}} {
		if code := runMain(f.storageContext(t.Context()), append(append([]string(nil), args...), extra...), io.Discard, io.Discard); code != 2 {
			t.Fatal("passive service accepted native/allowance override", extra, code)
		}
	}
}

// A valid new passive approval cannot be attached to any retained v1/v2/v3
// composition. Existing explicit-root-weight signatures keep their old domain.
func TestBootstrapRootPassiveCannotConvertLegacySchemas(t *testing.T) {
	for _, schema := range []string{bootstrapChainConfigSchemaV1, bootstrapChainConfigSchemaV2, bootstrapChainConfigSchema} {
		f := newBootstrapRootPassiveFixture(t)
		f.config.Schema = schema
		bootstrapRootTestWrite(t, f.path, f.config)
		if _, err := loadBootstrapChainPreparation(t.Context(), f.path); err == nil {
			t.Fatal("passive role entered legacy composition", schema)
		}
	}
	f := newBootstrapRootPassiveFixture(t)
	f.config.RootValidator.ActionApprovalPublicKey = f.config.RootValidator.ApprovalPublicKey
	bootstrapRootTestWrite(t, f.path, f.config)
	if _, err := loadBootstrapChainPreparation(t.Context(), f.path); err == nil {
		t.Fatal("passive role acquired native-action approver")
	}
}

// Missing authority and reapproved config are different from resuming the
// exact original preparation. Neither may renew or replace its retained state.
func TestRootPassiveServiceRejectsChangedApprovalAndIncompletePreparation(t *testing.T) {
	f := newBootstrapRootPassiveFixture(t)
	config := rootPassiveRuntimeConfig{Schema: rootPassiveRuntimeSchema, Root: f.config.Root, Role: *f.config.RootValidator}
	ref := bootstrapRootTestWrite(t, filepath.Join(filepath.Dir(f.path), "passive-runtime.json"), config)
	args := []string{"root-passive-service", "run", "--config", ref.Path, "--accept-runtime-sha256", ref.Sha256}
	if code := runMain(f.storageContext(t.Context()), args, io.Discard, io.Discard); code != 3 {
		t.Fatal("unprepared passive service ran", code)
	}
	f.result(t, "apply")
	before := f.journals(t)
	if err := os.WriteFile(config.Role.Approval.Path, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if code := runMain(f.storageContext(t.Context()), args, io.Discard, io.Discard); code != 2 || !reflect.DeepEqual(before, f.journals(t)) {
		t.Fatal("changed approval reused retained preparation", code)
	}
}

// A monitor checkpoint cannot overlap contract preparation, a UR writer or
// any authority input even though passive observation carries no spending key.
func TestBootstrapRootPassiveCheckpointCannotAliasAnotherOwner(t *testing.T) {
	f := newBootstrapRootPassiveFixture(t)
	service := *f.root.plan.PassiveService
	service.CheckpointPath = filepath.Join(f.config.RunDirectory, bootstrapChainStateFile)
	f.root.config.RootService = bootstrapRootTestWrite(t, f.root.config.RootService.Path, service)
	f.config.Root = bootstrapRootTestWrite(t, f.root.configPath, f.root.config)
	var err error
	f.root.plan, err = loadBootstrapRootPlan(t.Context(), f.root.configPath)
	if err != nil {
		t.Fatal("valid independently reapproved child rejected before namespace check", err)
	}
	f.rootRole.approval.RootPlanHash, f.rootRole.approval.ServiceConfigHash = f.root.plan.ContentHash, f.root.plan.serviceHash()
	f.rootRole.sign(t)
	bootstrapRootTestWrite(t, f.path, f.config)
	if _, err := loadBootstrapChainPreparation(t.Context(), f.path); err == nil || !strings.Contains(err.Error(), "journals or markers overlap") {
		t.Fatal("passive checkpoint alias did not reach namespace rejection", err)
	}
}

// Repinning an envelope after changing its signature reaches cryptographic
// verification; byte identity alone cannot confer service authority.
func TestRootPassiveServiceRejectsRepinnedForgedApproval(t *testing.T) {
	f := newBootstrapRootPassiveFixture(t)
	f.result(t, "apply")
	before := f.journals(t)
	f.rootRole.approval.Signature = strings.Repeat("ab", 64)
	f.rootRole.write(t)
	config := rootPassiveRuntimeConfig{Schema: rootPassiveRuntimeSchema, Root: f.config.Root, Role: *f.config.RootValidator}
	ref := bootstrapRootTestWrite(t, filepath.Join(filepath.Dir(f.path), "passive-runtime.json"), config)
	var diagnostic bytes.Buffer
	if code := runMain(f.storageContext(t.Context()), []string{"root-passive-service", "run", "--config", ref.Path, "--accept-runtime-sha256", ref.Sha256}, io.Discard, &diagnostic); code != 2 ||
		!strings.Contains(diagnostic.String(), "independent signer") || !reflect.DeepEqual(before, f.journals(t)) {
		t.Fatal("repinned passive approval bypassed signature binding", code, diagnostic.String())
	}
	if _, err := os.Stat(f.root.plan.PassiveService.CheckpointPath); !os.IsNotExist(err) {
		t.Fatal("forged service opened checkpoint", err)
	}
}

// Service start cannot repair missing/corrupt preparation or an interrupted
// claim. Each refusal retains exactly the incomplete state it encountered.
func TestRootPassiveServiceCannotRecreatePreparation(t *testing.T) {
	for _, change := range []string{"progress-missing", "progress-corrupt", "marker-incomplete", "marker-missing"} {
		f := newBootstrapRootPassiveFixture(t)
		f.result(t, "apply")
		path := filepath.Join(f.root.plan.RunDirectory, bootstrapRootProgressFile)
		var err error
		switch change {
		case "progress-missing":
			err = os.Remove(path)
		case "progress-corrupt":
			err = os.WriteFile(path, []byte("{}"), 0600)
		case "marker-incomplete":
			err = os.WriteFile(path+".lock", []byte(f.root.plan.ContentHash+"\n"), 0600)
		case "marker-missing":
			err = os.Remove(path + ".lock")
		}
		if err != nil {
			t.Fatal(err)
		}
		before := f.journals(t)
		config := rootPassiveRuntimeConfig{Schema: rootPassiveRuntimeSchema, Root: f.config.Root, Role: *f.config.RootValidator}
		ref := bootstrapRootTestWrite(t, filepath.Join(filepath.Dir(f.path), "passive-runtime.json"), config)
		if code := runMain(f.storageContext(t.Context()), []string{"root-passive-service", "run", "--config", ref.Path, "--accept-runtime-sha256", ref.Sha256}, io.Discard, io.Discard); code != 3 || !reflect.DeepEqual(before, f.journals(t)) {
			t.Fatal("passive start repaired incomplete preparation", change, code)
		}
		if _, err := os.Stat(f.root.plan.PassiveService.CheckpointPath); !os.IsNotExist(err) {
			t.Fatal("incomplete preparation opened checkpoint", change, err)
		}
	}
}

// Downstream UR admission must consume real passive preparation without
// inventing the legacy root's missing native custody. Its role checks remain.
func TestRootPassiveReadinessSupportsIndependentValidatorAdmission(t *testing.T) {
	f := newBootstrapRootPassiveFixture(t)
	f.result(t, "apply")
	observed, err := f.client.observeBootstrapChainReadiness(f.storageContext(t.Context()), f.preparation)
	if err != nil || !observed.ObservationComplete || observed.LocalPreparation == nil {
		t.Fatal("real composed observation unavailable", err)
	}
	plan := validatorActivationPlan{PlanHash: f.preparation.Plan.ContentHash}
	for _, role := range f.config.Validators {
		plan.Units = append(plan.Units, validatorActivationUnit{Role: role.Role, Source: protocol.ValidatorProgressSource{ValidatorId: role.ValidatorId}})
	}
	identity := observed.Census.Observation.Identity
	readiness := validatorActivationReadiness{ObservedAt: time.Date(2026, 1, 3, 4, 5, 6, 0, time.UTC), PlanHash: plan.PlanHash,
		EvidenceHash: rootObjectHash(observed), FinalizedHash: identity.FinalizedHash, FinalizedNumber: identity.FinalizedNumber,
		Local: *observed.LocalPreparation, Roles: observed.UrValidators}
	if err := readiness.validate(plan); err != nil || readiness.Local.RootStrategy != rootPassiveStrategy || readiness.Local.RootCustodyHash != "" {
		t.Fatal("passive root imposed legacy custody on UR admission", err)
	}
	for _, change := range []string{"implicit-passive", "invented-custody", "invented-transaction"} {
		changed := readiness
		switch change {
		case "implicit-passive":
			changed.Local.RootStrategy = ""
		case "invented-custody":
			changed.Local.RootCustodyHash = rootObjectHash("synthetic invalid native custody")
		case "invented-transaction":
			changed.Local.RootExtrinsicHash = testFinalizedHash
		}
		if err := changed.validate(plan); err == nil {
			t.Fatal("UR readiness accepted substituted root scope", change)
		}
	}
}

// A contract successor retains the actual original preparation type. Missing
// legacy custody stays invalid unless the fresh passive strategy is explicit.
func TestRootPassiveReadinessSupportsContractSuccessorSeals(t *testing.T) {
	f := newBootstrapRootPassiveFixture(t)
	f.result(t, "apply")
	state, err := openBootstrapChainReadinessState(f.storageContext(t.Context()), f.preparation)
	if err != nil {
		t.Fatal(err)
	}
	defer state.close()
	approval, _ := newBootstrapSuccessorPreparationTestApproval(t)
	approval.Plan.Proposal.LocalPreparation = state
	approval.Plan.Proposal.ContentHash = ""
	approval.Plan.Proposal.ContentHash = rootObjectHash(approval.Plan.Proposal)
	if err := approval.Plan.validate(); err != nil {
		t.Fatal("passive preparation blocked contract successor review", err)
	}
	state.RootStrategy = ""
	approval.Plan.Proposal.ContentHash = ""
	approval.Plan.Proposal.ContentHash = rootObjectHash(approval.Plan.Proposal)
	if err := approval.Plan.validate(); err == nil || !strings.Contains(err.Error(), "root seals") {
		t.Fatal("missing legacy root custody acquired passive meaning", err)
	}
}
