// One-role launch fixtures reuse the passive v4 composition and its two-operator
// producer configs. Two-role seals are pinned independently of those fixtures.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/validator"
)

// Every field is fixed so the digest covers the complete two-role wire shape,
// both role labels, the sealed phase names and the schema's hash domain.
func bootstrapChainGoldenPlan(schema string) bootstrapChainPlan {
	account := func(value byte) string { return "0x" + strings.Repeat(fmt.Sprintf("%02x", value), 32) }
	pin := func(name string, value byte) planFileReference {
		return planFileReference{Path: "/synthetic/golden/" + name, Sha256: "sha256:" + strings.Repeat(fmt.Sprintf("%02x", value), 32)}
	}
	block, netuid, take := uint64(42), uint16(0), uint16(1234)
	config := bootstrapChainConfig{Schema: schema, DeploymentId: "synthetic-golden", Netuid: 25,
		Network: planNetwork{NativeChain: "synthetic-mainnet", GenesisHash: account(0x01), EvmChainId: mainnetEvmChainId}, RunDirectory: "/synthetic/golden/run",
		OwnerTrimPolicy: pin("trim-policy.json", 0x02), OwnerTrimPlan: pin("trim-plan.json", 0x03), Contracts: pin("contracts.json", 0x04), Root: pin("root.json", 0x05),
		RootValidator: &bootstrapChainRootValidator{Role: "bittensor-root-validator", Netuid: &netuid, Implementation: "sn/mainnet/root-passive-service",
			Hotkey: account(0x11), Coldkey: account(0x22), Seat: &rootSeatExpectation{Uid: 0, RegistrationBlock: 40}, Strategy: rootPassiveStrategy,
			ApprovalPublicKey: account(0x7c), Approval: pin("root-approval.json", 0x06)}}
	var inspections []validator.ProductionBootstrapInspection
	for i, role := range []string{"majority", "secondary"} {
		identity := subnetIdentityExpectation{Hotkey: account(byte(0x43 + i)), Coldkey: account(byte(0x63 + i)), RegistrationBlock: &block}
		config.Validators = append(config.Validators, bootstrapChainValidator{ValidatorId: uint64(i + 1), subnetIdentityExpectation: identity,
			Config: pin(fmt.Sprintf("validator-%d.yml", i+1), byte(0x07+i)), Role: role, Implementation: "sn/validator", ApprovalPublicKey: account(byte(0x70 + i))})
		inspections = append(inspections, validator.ProductionBootstrapInspection{DeploymentId: config.DeploymentId, ValidatorId: uint64(i + 1),
			EvmChainId: mainnetEvmChainId, Netuid: 25, PolicyHash: account(0x31), DeclaredPaths: []string{fmt.Sprintf("/synthetic/golden/validator-%d/state", i+1)}})
	}
	root := bootstrapRootPlan{Schema: bootstrapRootPassivePlanSchema, Phase: bootstrapRootPassivePhase, DeploymentId: config.DeploymentId, Network: config.Network,
		RunDirectory: config.RunDirectory, ConfigPath: config.Root.Path, ConfigSha256: config.Root.Sha256, ServiceInput: pin("root-service.json", 0x09),
		PassiveService: &rootPassiveServiceConfig{Schema: rootPassiveServiceSchema, Policy: rootValidatorPolicy{Schema: rootPassivePolicySchema,
			Role: "bittensor-root-validator", Hotkey: account(0x11), Coldkey: account(0x22), ExpectedSeat: &rootSeatExpectation{Uid: 0, RegistrationBlock: 40},
			MinimumStakeRao: "80", ExpectedDelegateTake: &take}, RpcUrl: "https://root.example", CheckpointPath: "/synthetic/golden/run/checkpoint.json"}}
	return bootstrapChainPlan{Schema: bootstrapChainPlanSchemaForConfig(schema), ConfigPath: "/synthetic/golden/chain.json", ConfigSha256: pin("chain.json", 0x0a).Sha256,
		Config: config, OwnerTrimContentHash: pin("trim", 0x0b).Sha256, OwnerTrimBlockers: []string{"SYNTHETIC_TRIM_BLOCKER"},
		ContractPlanHash: pin("contract-plan", 0x0c).Sha256, RootPlanHash: pin("root-plan", 0x0d).Sha256, ValidatorInspections: inspections,
		RootInspection: &bootstrapChainRootInspection{Plan: root, Approval: bootstrapChainRootApproval{Schema: bootstrapChainRootPassiveApprovalSchema,
			DeploymentId: config.DeploymentId, RootPlanHash: pin("root-plan", 0x0d).Sha256, ServiceConfigHash: pin("root-service", 0x0e).Sha256}},
		PendingChainPhases: bootstrapChainPendingPhasesForSchema(schema)}
}

// The sealed one-role phase names. Only the UR validator phase differs from
// the two-role names that every v1-v4 plan retains.
func bootstrapChainSoleTestPhases() []string {
	return []string{"owner-trim-execution-and-generation-reconciliation", "complete-contract-installation-and-evidence-binding",
		"one-ur-validator-production-admission-and-healthy-operators", "root-current-authority-and-service-activation",
		"native-10-percent-allocation-and-90-percent-recycle-acceptance"}
}

// The digests were computed before the one-role schema existed. Existing v3/v4
// preparation and its child custody markers depend on exactly these preimages.
func TestBootstrapChainTwoRolePlanHashesUnchanged(t *testing.T) {
	original := []string{"owner-trim-execution-and-generation-reconciliation", "complete-contract-installation-and-evidence-binding",
		"two-ur-validator-production-admissions-and-healthy-operators", "root-current-authority-and-service-activation",
		"native-10-percent-allocation-and-90-percent-recycle-acceptance"}
	for _, schema := range []string{bootstrapChainConfigSchemaV1, bootstrapChainConfigSchemaV2, bootstrapChainConfigSchema, bootstrapChainConfigSchemaV4} {
		if !slices.Equal(bootstrapChainPendingPhasesForSchema(schema), original) || !slices.Equal(bootstrapChainValidatorRoles(schema), []string{"majority", "secondary"}) {
			t.Fatal("two-role schema changed its sealed phases or role labels", schema)
		}
	}
	if !slices.Equal(bootstrapChainPendingPhasesForSchema(bootstrapChainConfigSchemaV5), bootstrapChainSoleTestPhases()) || !slices.Equal(bootstrapChainPendingPhases(), original) {
		t.Fatal("one-role phases leaked into the original two-role names")
	}
	for _, item := range []struct{ schema, digest string }{
		{schema: bootstrapChainConfigSchema, digest: "sha256:f8b8991965be5b5e10ff5e826daf257282cd6bbba43e51da2bc9c01d214b6e29"},
		{schema: bootstrapChainConfigSchemaV4, digest: "sha256:92d17e3e2047762bf662d73532e96c38950a71ecf79140ce631e5fce241e5ecd"},
	} {
		if got := bootstrapChainPlanHash(bootstrapChainGoldenPlan(item.schema)); got != item.digest {
			t.Errorf("%s two-role plan hash changed: %s", item.schema, got)
		}
	}
}

// The remaining producer is reapproved for a one-validator census and policy
// minimum before custody exists. Its two signed operators stay unchanged.
func bootstrapChainSoleConvert(t *testing.T, f *bootstrapChainFixture) {
	t.Helper()
	v := f.validators[0]
	v.config.Policy.Safety.MinimumLiveValidatorCount = 1
	policyHash, err := v.config.Policy.Hash()
	if err != nil {
		t.Fatal(err)
	}
	v.config.PolicyHash, v.approval.Proposal.ParentPolicyHash = fmt.Sprintf("0x%x", policyHash), policyHash
	v.approval.Production.ValidatorHotkeys = [][32]byte{v.approval.ValidatorHotkey}
	f.validators, f.config.Validators = f.validators[:1], f.config.Validators[:1]
	f.config.Schema, f.config.Validators[0].Role = bootstrapChainConfigSchemaV5, "sole"
	f.config.Validators[0].Config = v.publish(t)
}

// Independently reapprove the passive service for another seat identity. The
// expected seat and observation window stay those of the original approval.
func bootstrapChainSoleRootIdentity(t *testing.T, f *bootstrapChainFixture, hotkey, coldkey string) {
	t.Helper()
	service := *f.root.plan.PassiveService
	service.Policy = copyRootPassivePolicy(service.Policy)
	service.Policy.Hotkey, service.Policy.Coldkey = hotkey, coldkey
	f.root.config.RootService = bootstrapRootTestWrite(t, f.root.config.RootService.Path, service)
	f.config.Root = bootstrapRootTestWrite(t, f.root.configPath, f.root.config)
	var err error
	f.root.plan, err = loadBootstrapRootPlan(t.Context(), f.root.configPath)
	if err != nil {
		t.Fatal(err)
	}
	f.config.RootValidator.Hotkey, f.config.RootValidator.Coldkey = hotkey, coldkey
	f.rootRole.approval.RootPlanHash, f.rootRole.approval.ServiceConfigHash = f.root.plan.ContentHash, f.root.plan.serviceHash()
	f.rootRole.sign(t)
}

// SN25 launches with one hotkey on netuid 0 and netuid 25. The first UR role's
// own hotkey and owner take root UID 0; the retained trim census is rebuilt.
func bootstrapChainSoleShareRoot(t *testing.T, f *bootstrapChainFixture) {
	t.Helper()
	role, rootArg := f.config.Validators[0], []byte{0, 0}
	hotkey, previous := bootstrapChainTestAccount(t, role.Hotkey), bootstrapChainTestAccount(t, f.config.RootValidator.Hotkey)
	subnetTestDelete(t, f.census, "Uids", rootArg, previous[:])
	f.census.set(t, "Keys", hotkey[:], rootArg, rootArg)
	f.census.set(t, "Uids", rootArg, rootArg, hotkey[:])
	f.census.set(t, "IsNetworkMember", []byte{1}, hotkey[:], rootArg)
	f.census.set(t, "TotalHotkeyAlpha", binary.LittleEndian.AppendUint64(nil, 100), hotkey[:], rootArg)
	f.census.set(t, "Delegates", binary.LittleEndian.AppendUint16(nil, 1234), hotkey[:])
	f.census.set(t, "AutoParentDelegationEnabled", []byte{1}, hotkey[:])
	var policy subnetCensusPolicy
	raw, err := readBootstrapChainInput(t.Context(), f.config.OwnerTrimPolicy, maxRpcReplyBytes)
	if err != nil || decodePlanJson(raw, &policy) != nil {
		t.Fatal("synthetic original policy unavailable", err)
	}
	preview, err := f.client.readSubnetPreview(t.Context(), policy, f.config.OwnerTrimPolicy.Sha256)
	if err != nil {
		t.Fatal(err)
	}
	trim, err := buildOwnerTrimPlan(t.Context(), policy, preview)
	if err != nil {
		t.Fatal(err)
	}
	f.config.OwnerTrimPlan = bootstrapRootTestWrite(t, f.config.OwnerTrimPlan.Path, trim)
	bootstrapChainSoleRootIdentity(t, f, role.Hotkey, role.Coldkey)
}

// Accepted only after every independently signed input has been rewritten.
func bootstrapChainSoleLoad(t *testing.T, f *bootstrapChainFixture) {
	t.Helper()
	bootstrapRootTestWrite(t, f.path, f.config)
	var err error
	f.preparation, err = loadBootstrapChainPreparation(t.Context(), f.path)
	if err != nil {
		t.Fatal("one-role preparation refused", err)
	}
}

// Fresh v5 preparation starts from the complete passive v4 fixture. A shared
// root seat uses the sole UR hotkey on netuid 0, as the SN25 launch does.
func newBootstrapChainSoleFixture(t *testing.T, shared bool, configureApproval func(*bootstrapChainValidatorFixture)) *bootstrapChainFixture {
	t.Helper()
	f := newBootstrapRootPassiveFixtureWithCensus(t, nil, configureApproval)
	bootstrapChainSoleConvert(t, f)
	if shared {
		bootstrapChainSoleShareRoot(t, f)
	}
	bootstrapChainSoleLoad(t, f)
	return f
}

// One signed role is sealed under its own domain and phases, then prepared
// through the real passive custody owners with no network or signing effect.
func TestBootstrapChainSolePreparesOneRoleOffline(t *testing.T) {
	for _, shared := range []bool{true, false} {
		f := newBootstrapChainSoleFixture(t, shared, nil)
		f.requireFreshJournals(t)
		plan := f.preparation.Plan
		role := plan.Config.Validators[0]
		if plan.Schema != bootstrapChainPlanSchemaV5 || len(plan.Config.Validators) != 1 || role.Role != "sole" || len(plan.ValidatorInspections) != 1 ||
			!slices.Equal(plan.PendingChainPhases, bootstrapChainSoleTestPhases()) || (plan.Config.RootValidator.Hotkey == role.Hotkey) != shared ||
			plan.RootInspection == nil || f.preparation.Root.PassiveService == nil || plan.NetworkEffects || plan.NativeSigning || plan.ActivationReady {
			t.Fatalf("one-role preparation lost its scope: %+v", plan)
		}
		unsealed := plan
		unsealed.ContentHash = ""
		raw, err := json.Marshal(unsealed)
		digest := sha256.Sum256(append([]byte("urnetwork-mainnet-bootstrap-chain-preparation-v5\x00"), raw...))
		if err != nil || plan.ContentHash != "sha256:"+hex.EncodeToString(digest[:]) || bytes.Contains(raw, []byte("two-ur-validator")) {
			t.Fatal("one-role seal differs from its own domain or kept the two-role phase", err)
		}
		var stdout, stderr bytes.Buffer
		if code := f.command(t.Context(), "plan", &stdout, &stderr); code != 0 {
			t.Fatalf("plan exit %d: %s", code, stderr.String())
		}
		var decoded bootstrapChainPlan
		if err := decodePlanJson(stdout.Bytes(), &decoded); err != nil || !reflect.DeepEqual(decoded, plan) {
			t.Fatal("public plan differs from the accepted one-role preparation", err)
		}
		contract := bootstrapContractTestResult(t, bootstrapPlanTestCommand(t, []string{"bootstrap-chain", "contract-plan", "--config", f.path}, 3))
		if contract.PlanHash != plan.ContentHash || !slices.Equal(contract.PendingChainPhases, plan.PendingChainPhases) || contract.LocalPreparation != nil {
			t.Fatal("contract review lost the one-role preparation or its phases", contract)
		}
		first := f.result(t, "apply")
		before := f.journals(t)
		if first.Schema != "urnetwork-mainnet-bootstrap-chain-result-v5" || first.PlanHash != plan.ContentHash || !first.LocalPreparationComplete ||
			first.UrValidatorsStatus != "one-signed-production-config-verified-live-admission-pending" || !first.UrValidatorConfigsVerified ||
			first.RootValidatorStatus != "signed-passive-root-service-config-verified-observation-pending" || !first.RootValidatorConfigVerified ||
			!slices.Equal(first.PendingChainPhases, plan.PendingChainPhases) || first.CurrentEconomicAcceptance != "" || first.NetworkEffects || first.ActivationReady ||
			first.Root.SignatureStatus != "not-applicable-passive-observation" || first.Root.Broadcasts != 0 || len(before) != 6 {
			t.Fatalf("one-role apply changed scope or claimed authority: %+v", first)
		}
		if again := f.result(t, "resume"); !reflect.DeepEqual(first, again) || !reflect.DeepEqual(before, f.journals(t)) || len(f.contracts.counts) != 0 {
			t.Fatal("one-role resume changed original custody or contacted a chain")
		}
	}
}

// The launch's receiving approval is the sole producer's own selection; the
// unsealed outcome report follows it exactly as it follows two agreeing roles.
func TestBootstrapChainSoleReportsTreasuryAcceptance(t *testing.T) {
	f := newBootstrapChainSoleFixture(t, true, bootstrapChainTreasuryApproval([32]byte(bytes.Repeat([]byte{0x77}, 32))))
	result := f.result(t, "apply")
	if f.preparation.Plan.ValidatorInspections[0].Approval.Proposal.Treasury == nil || !slices.Equal(result.PendingChainPhases, bootstrapChainSoleTestPhases()) ||
		result.CurrentEconomicAcceptance != "native-10-percent-provider-allocation-and-90-percent-native-treasury-acceptance" {
		t.Fatalf("one-role treasury preparation lost its sealed phases or outcome report: %+v", result)
	}
}

// Neither form borrows the other's role count, labels, root strategy or shared
// root hotkey. Every refusal happens at review, before any custody claim.
func TestBootstrapChainSoleSchemaBoundaries(t *testing.T) {
	for _, item := range []struct {
		name       string
		build      func(*testing.T) *bootstrapChainFixture
		diagnostic string
	}{
		{name: "v4-one-role", diagnostic: "exactly that schema's UR roles", build: func(t *testing.T) *bootstrapChainFixture {
			f := newBootstrapChainSoleFixture(t, false, nil)
			f.config.Schema = bootstrapChainConfigSchemaV4
			return f
		}},
		{name: "v5-two-roles", diagnostic: "exactly that schema's UR roles", build: func(t *testing.T) *bootstrapChainFixture {
			f := newBootstrapRootPassiveFixture(t)
			f.config.Schema = bootstrapChainConfigSchemaV5
			return f
		}},
		{name: "v5-majority-label", diagnostic: "one sole sn/validator role", build: func(t *testing.T) *bootstrapChainFixture {
			f := newBootstrapChainSoleFixture(t, true, nil)
			f.config.Validators[0].Role = "majority"
			return f
		}},
		{name: "v5-legacy-root", diagnostic: "cannot convert legacy root action authority", build: func(t *testing.T) *bootstrapChainFixture {
			f := newBootstrapChainFixture(t)
			bootstrapChainSoleConvert(t, f)
			return f
		}},
		{name: "v4-shared-root", diagnostic: "root role cannot count as a UR validator", build: func(t *testing.T) *bootstrapChainFixture {
			f := newBootstrapRootPassiveFixture(t)
			bootstrapChainSoleShareRoot(t, f)
			return f
		}},
		{name: "v5-shared-root-foreign-owner", diagnostic: "must name one coldkey", build: func(t *testing.T) *bootstrapChainFixture {
			f := newBootstrapChainSoleFixture(t, true, nil)
			bootstrapChainSoleRootIdentity(t, f, f.config.Validators[0].Hotkey, "0x"+strings.Repeat("77", 32))
			return f
		}},
	} {
		t.Run(item.name, func(t *testing.T) {
			item.build(t).rejectValidatorPlan(t, item.diagnostic)
		})
	}
}

// The sole validator and its root seat are observed separately at one
// finalized block. Its stake share remains an explicit activation blocker.
func TestBootstrapChainSoleReadinessRetainsStakeMajorityBlocker(t *testing.T) {
	f := newBootstrapChainSoleFixture(t, true, nil)
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
		!slices.Equal(result.PendingChainPhases, bootstrapChainSoleTestPhases()) || len(result.UrValidators) != 1 || result.PassiveRoot == nil ||
		result.ActivationReady || result.CurrentAuthorityVerified || result.NativeSigning || result.NetworkEffects || !reflect.DeepEqual(before, f.journals(t)) {
		t.Fatalf("one-role readiness changed scope or claimed authority: %+v", result)
	}
	sole, root := result.UrValidators[0], result.RootValidator
	if sole.Role != "sole" || sole.Observed == nil || len(sole.ObservationBlockers) != 0 || !slices.Contains(sole.ActivationBlockers, "EFFECTIVE_STAKE_MAJORITY_UNVERIFIED") ||
		root.Observed == nil || len(root.ObservationBlockers) != 0 || root.Expected.Hotkey != sole.Expected.Hotkey || root.Observed.Uid != 0 || sole.Observed.Uid != 2 {
		t.Fatalf("shared hotkey merged its root and UR observations or dropped the stake blocker: %+v %+v", sole, root)
	}
}

// The complete evidence graph binds exactly one signed producer declaration,
// and every later path check covers that role's config without a second role.
func TestBootstrapContractRolePlanBindsSoleValidator(t *testing.T) {
	f := newBootstrapSuccessorCommandFixture(t)
	bootstrapRootPassiveConvert(t, f, nil, nil)
	bootstrapChainSoleConvert(t, f)
	bootstrapChainSoleShareRoot(t, f)
	bootstrapChainSoleLoad(t, f)
	evidence := bootstrapContractRoleBind(t, f)
	before := f.journals(t)
	var stdout, stderr bytes.Buffer
	if code := runMain(f.storageContext(t.Context()), []string{"bootstrap-chain", "contract-role-plan", "--config", f.path}, &stdout, &stderr); code != 0 {
		t.Fatal("one-role contract-role plan refused", code, stderr.String())
	}
	var result bootstrapContractRolePlan
	if err := decodePlanJson(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	seal := result.ContentHash
	result.ContentHash = ""
	proxy, inspection := evidence.priorPlan(4), f.preparation.Plan.ValidatorInspections[0]
	if seal != rootObjectHash(result) || result.PreparationHash != f.preparation.Plan.ContentHash || result.CoordinatorProxy != proxy.Address ||
		result.InitialPolicyHash != common.HexToHash(inspection.PolicyHash) || !slices.Equal(result.PendingChainPhases, bootstrapChainSoleTestPhases()) || len(result.Validators) != 1 ||
		!result.DeclarationsVerified || result.CanonicalReceiptsVerified || result.CurrentStateVerified || result.InstallationComplete || result.ActivationReady || result.NetworkEffects {
		t.Fatal("one-role contract-role output lost its declaration or claimed live authority", result)
	}
	binding := result.Validators[0]
	if binding.Role != "sole" || binding.ValidatorId != inspection.ValidatorId || binding.Config != f.config.Validators[0].Config ||
		binding.ApprovalHash != rootObjectHash(inspection.Approval) || binding.DeclaredDeployBlock != inspection.DeployBlock || !reflect.DeepEqual(before, f.journals(t)) {
		t.Fatal("one-role contract-role output changed its signed producer declaration or custody", binding)
	}
	_, plans, err := prepareBootstrapContractReadiness(t.Context(), f.preparation.Contracts, f.config.Contracts.Path)
	if err != nil || len(plans) != 8 {
		t.Fatal("one-role contract graph lost its eight projections", err)
	}
	request := filepath.Join(filepath.Dir(f.path), "successor-request.json")
	if err := errors.Join(validateBootstrapContractReadinessPaths(f.preparation, plans), validateBootstrapSuccessorPreparationPaths(f.preparation, plans, request, "")); err != nil {
		t.Fatal("one-role path admission refused distinct inputs", err)
	}
	if err := validateBootstrapSuccessorPreparationPaths(f.preparation, plans, f.config.Validators[0].Config.Path, ""); err == nil {
		t.Fatal("successor request aliased the sole validator config")
	}
}

// The sole role's declared floor is checked like each two-role floor; an empty
// declaration set cannot certify the deployment's indexing history.
func TestBootstrapContractReceiptScanFloorCoversSoleRole(t *testing.T) {
	records := make([]evmActionRecord, 8)
	for i := range records {
		records[i].Receipt = &evmCreateReceipt{Status: 1, BlockNumber: 38 + uint64(i), NativeNumber: 101 + uint64(i)}
	}
	roles := []bootstrapContractValidatorBinding{{Role: "sole", DeclaredDeployBlock: 38}}
	if earliest, err := bootstrapContractReceiptScanFloor(roles, records); err != nil || earliest != 38 {
		t.Fatal("inclusive one-role deployment floor refused", earliest, err)
	}
	roles[0].DeclaredDeployBlock = 39
	if _, err := bootstrapContractReceiptScanFloor(roles, records); err == nil {
		t.Fatal("one-role scan floor above earliest EVM inclusion was admitted")
	}
	if _, err := bootstrapContractReceiptScanFloor(nil, records); err == nil {
		t.Fatal("scan floor was certified without any declared role")
	}
}

// The unchanged two-unit activation owner pairs its majority and secondary
// units with roles by index; its first unit already refuses the sole role.
func TestValidatorActivationRefusesSolePreparation(t *testing.T) {
	f := newBootstrapChainSoleFixture(t, true, nil)
	approval := validatorActivationApproval{Plan: validatorActivationPlan{Preparation: planFileReference{Path: f.path, Sha256: f.preparation.Plan.ConfigSha256},
		PlanHash: f.preparation.Plan.ContentHash, Units: []validatorActivationUnit{{Role: "majority"}, {Role: "secondary"}}}}
	if _, err := loadValidatorActivationPreparation(t.Context(), approval); err == nil || !strings.Contains(err.Error(), "unit differs from its signed producer config, role") {
		t.Fatal("two-unit activation admitted a one-role preparation", err)
	}
}
