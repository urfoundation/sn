//go:build linux || darwin

// The checked-in launch template reaches the real plan, retained constructor
// admission and renderer. Test-owned consents are not historical eligibility;
// the separate native transport roots exercise that authentication boundary.
package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/protocol"
	validatorpkg "github.com/urfoundation/sn/validator"
	"github.com/urnetwork/server/controller"
	"gopkg.in/yaml.v3"
)

// Decode the actual launch file, then supply inert resolved secrets and the
// current reviewed fixture lock. No workstation vault or wallet is opened.
func runtimeEvidenceLaunchConfigTest(t *testing.T) *ResolvedConfig {
	t.Helper()
	var harness HarnessConfig
	if err := strictYAML("testnet.yml", &harness); err != nil {
		t.Fatal(err)
	}
	if err := harness.Validate(); err != nil {
		t.Fatalf("actual launch template cannot pass pre-plan admission: %v", err)
	}
	cfg := testResolvedConfig(t)
	cfg.Config = &harness
	cfg.Netuid = 521
	cfg.MaximumAlphaRao = 28_250_000_000_000
	cfg.OperatorAPIOrigins = []string{"http://127.0.0.1:18081", "http://127.0.0.1:18082"}
	cfg.OperationalRPCMode = rpcModePublicOverride
	cfg.Authority = "http://127.0.0.1:9944"
	cfg.OperationalSubstrate = harness.LaunchInputs.PublicSubstrateRPCOverride
	cfg.OperationalEVM = harness.LaunchInputs.PublicEVMRPCOverride
	cfg.Public.Chain.EVMPublicReadEndpoint = harness.LaunchInputs.PublicEVMRPCOverride
	var err error
	cfg.ConfigHash, err = releaseConfigHash(cfg.Config, cfg.Public, cfg.Hyperparameters)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// Use the real original source keys and generated contract domain. These
// explicitly test-only checkpoints are confined to signed fixture files.
func retainRuntimeEvidenceLaunchInputsTest(t *testing.T, cfg *ResolvedConfig, stateDir string, roles *RoleSecrets, plan *SetupPlan) {
	t.Helper()
	policy, err := decodeHex32("launch fixture policy", cfg.PolicyHash)
	if err != nil {
		t.Fatal(err)
	}
	prepared := &runtimeEvidenceActivationPreparedV2{Schema: "urnetwork-sim-evidence-activation-prepared-v2", PlanHash: plan.PlanHash, ConfigHash: cfg.ConfigHash, PolicyHash: cfg.PolicyHash, Epoch: 9,
		Native: ChainHead{Number: 100, Hash: common.Hash{0x31}.Hex()}, Evm: ChainHead{Number: 200, Hash: common.Hash{0x32}.Hex()}}
	for validatorId := 1; validatorId <= cfg.Config.Topology.Validators; validatorId++ {
		for noId := 1; noId <= cfg.Config.Topology.Operators; noId++ {
			hotkey, key, err := runtimeEvidenceActivationKeysV2(roles, uint64(validatorId), uint64(noId))
			if err != nil {
				t.Fatal(err)
			}
			companion := plan.ValidatorEvidence
			activation := protocol.ValidatorEvidenceActivation{Domain: protocol.ValidatorEvidenceActivationDomain{ChainID: cfg.ChainID, GenesisHash: [32]byte(companion.GenesisHash), Netuid: cfg.Netuid,
				Coordinator: [20]byte(companion.Coordinator), SettlementVault: [20]byte(companion.SettlementVault), DeploymentIDHash: [32]byte(companion.DeploymentIDHash), PolicyHash: policy, Epoch: prepared.Epoch},
				Hotkey: hotkey.PublicKey(), NoID: uint64(noId), VPK: [32]byte(key[ed25519.SeedSize:]), FirstSequence: 1, NativeBlock: prepared.Native.Number, NativeHash: [32]byte{0x31}, EVMBlock: prepared.Evm.Number, EVMHash: [32]byte{0x32}}
			vpkSignature, err := activation.SignVPK(key)
			if err != nil {
				t.Fatal(err)
			}
			digest, err := activation.Digest()
			if err != nil {
				t.Fatal(err)
			}
			hotkeySignature, err := hotkey.Sign(digest[:])
			if err != nil {
				t.Fatal(err)
			}
			prepared.Members = append(prepared.Members, runtimeEvidenceActivationMemberV2{ValidatorId: uint64(validatorId), NoId: uint64(noId), ValidatorUid: uint16(validatorId), Activation: activation, VpkSignature: vpkSignature, HotkeySignature: hotkeySignature})
		}
	}
	limit, err := runtimeEvidenceProvisionLimit(cfg)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := writeRuntimeEvidenceSetupV2(t.Context(), filepath.Join(stateDir, "evidence-v2-setup", "prepared.json"), prepared, limit)
	if err != nil {
		t.Fatal(err)
	}
	completed := &runtimeEvidenceActivationCompletedV2{Schema: "urnetwork-sim-evidence-activation-completed-v2", PlanHash: plan.PlanHash, PreparedHash: fmt.Sprintf("0x%x", sha256.Sum256(encoded)), Boundary: ChainHead{Number: 210, Hash: common.Hash{0x33}.Hex()}}
	executor := &Executor{cfg: cfg, stateDir: stateDir, roles: roles, plan: plan}
	if err := executor.retainRuntimeEvidenceInputsV2(t.Context(), prepared, encoded, completed); err != nil {
		t.Fatal(err)
	}
}

// Actual launch capacities must not be replaced by the much smaller generic
// fixture. Both API destinations and both validator loaders see exact outputs.
func TestRuntimeEvidenceLaunchV2TemplateReachesGeneratedSetupAndRender(t *testing.T) {
	t.Parallel()
	cfg := runtimeEvidenceLaunchConfigTest(t)
	approvedHash := cfg.ConfigHash
	cfg.Repos.PlatformConfig = testOperatorConfigSources(t)
	cfg.Repos.Vault = filepath.Join(t.TempDir(), "vault")
	for _, source := range []struct{ path, contents string }{
		{path: "local/st.yml", contents: "profile: fixture-overwritten\n"},
		{path: "local/pg.yml", contents: "authority: fixture-overwritten.invalid\n"},
		{path: "local/provider_egress.yml", contents: "ingest_secret: fixture-must-not-survive\n"},
		{path: "main/minio.yml", contents: "authority: fixture.invalid:23900\ntls: true\nbucket: blob\naccess_key: fixture-access\nsecret_key: fixture-secret\n"},
	} {
		if err := atomicWrite(filepath.Join(cfg.Repos.Vault, filepath.FromSlash(source.path)), []byte(source.contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	stateDir := t.TempDir()
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for label, client := range roles.Clients {
		client.ClientIDHex = strings.Repeat("01", 16)
		roles.Clients[label] = client
	}
	for miner := 1; miner <= cfg.Config.Topology.Miners; miner++ {
		state := filepath.Join(stateDir, "runtime", "miner-"+strconv.Itoa(miner), "state")
		for _, source := range []struct{ name, contents string }{
			{name: "jwt", contents: "fixture-network-jwt\n"},
			{name: ".provider.jwt", contents: "fixture-provider-jwt\n"},
			{name: ".provider.key", contents: strings.Repeat("01", 32) + "\n"},
		} {
			if err := atomicWrite(filepath.Join(state, source.name), []byte(source.contents), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	fixture := prepareRuntimeReservedRenderTest(t, cfg, stateDir, roles)
	if cfg.ConfigHash != approvedHash || fixture.plan.ConfigHash != approvedHash {
		t.Fatal("generated setup rewrote the approved static template")
	}
	retainRuntimeEvidenceLaunchInputsTest(t, cfg, stateDir, roles, fixture.plan)
	resolved, err := runtimeEvidenceV2ResolvedConfig(cfg, stateDir)
	if err != nil {
		t.Fatal(err)
	}
	wantReserved, err := runtimeReservedAttemptUploads(cfg, stateDir, &fixture.deployment)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := RenderRuntimeConfigs(cfg, stateDir, roles); err != nil {
			t.Fatalf("actual launch template cannot render or restart: %v", err)
		}
		if _, err := verifyRuntimeConfigManifest(cfg, stateDir); err != nil {
			t.Fatal(err)
		}
	}
	for index, expected := range wantReserved {
		path := filepath.Join(stateDir, "runtime", fmt.Sprintf("operator-%d", index+1), "vault", "st.yml")
		encoded, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var rendered struct {
			Reserved *controller.StReservedAttemptUploadConfig `yaml:"testnet-reserved-attempt-upload"`
		}
		if err := yaml.Unmarshal(encoded, &rendered); err != nil || rendered.Reserved == nil || !reflect.DeepEqual(*rendered.Reserved, expected) {
			t.Fatalf("operator %d changed resolved capacity or constructor authority: %v", index+1, err)
		}
		profile := &controller.StConfig{Enabled: true, Profile: "testnet", DeploymentId: cfg.Config.Deployment.DeploymentID, ChainId: cfg.ChainID, GenesisHash: expected.Admission.Deployment.GenesisHash,
			Netuid: uint64(cfg.Netuid), NoId: uint64(index + 1), ContractAddress: fixture.deployment.CoordinatorProxy, SettlementVault: fixture.deployment.SettlementVault}
		if err := rendered.Reserved.Validate(profile); err != nil || expected.Admission.Deployment.DeploymentBlock != fixture.creation.BlockNumber || len(expected.Admission.ActivationContexts) != 0 {
			t.Fatalf("operator %d cannot perform unchanged full deployment admission: %v", index+1, err)
		}
		if expected.Admission.Deployment.NativeRuntime.Version.SpecVersion != cfg.Public.Chain.ExpectedRuntimeSpec || expected.Admission.Deployment.NativeRuntime.CodeHash != cfg.Release.Runtime.CodeHash || expected.Admission.Deployment.NativeRuntime.MetadataHash != cfg.Release.Runtime.MetadataHash {
			t.Fatal("renderer substituted a stale native profile")
		}
	}
	for validatorId := 1; validatorId <= cfg.Config.Topology.Validators; validatorId++ {
		loaded, err := validatorpkg.LoadReleaseConfig(filepath.Join(stateDir, "runtime", fmt.Sprintf("validator-%d", validatorId), "validator.yml"))
		if err != nil || !reflect.DeepEqual(loaded.EvidenceV2, resolved.Config.ValidatorEvidenceV2[validatorId-1].Evidence) {
			t.Fatalf("validator %d loader changed signed source files or capacity: %v", validatorId, err)
		}
		if loaded.PollSeconds != 60 || loaded.Policy.Settlement.EpochBlocks != 300 || loaded.Policy.ProductionCadence.EpochBlocks != 360 {
			t.Fatal("public polling or approved two-phase cadence changed")
		}
		for _, operator := range loaded.Operators {
			if operator.APIURL != cfg.OperatorAPIOrigins[int(operator.NoID)-1] {
				t.Fatal("rendered source changed its approved loopback API origin")
			}
		}
	}
	if err := validateRuntimeEvidenceProvisionTemplateV2(cfg.Config); err != nil || cfg.ConfigHash != approvedHash {
		t.Fatalf("rendering mutated the approved fresh template: %v", err)
	}
	if fixture.transport.sendCalls.Load() != 1 {
		t.Fatal("rendering replayed the original constructor transaction")
	}
	// A manifest-scoped resolved view must not survive a later operation.
	// Both an original bounded reference and its prepared source stay live
	// authentication boundaries after the unchanged full render/restart pair.
	manifestPath := runtimeConfigManifestPath(stateDir)
	manifestBefore, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{resolved.Config.ValidatorEvidenceV2[0].Evidence.Operators[0].Context.Path, filepath.Join(stateDir, "evidence-v2-setup", "prepared.json")} {
		original, err := os.ReadFile(path)
		if err != nil || len(original) == 0 {
			t.Fatalf("missing original manifest source: %v", err)
		}
		changed := slices.Clone(original)
		changed[0] ^= 1
		if err := os.WriteFile(path, changed, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := verifyRuntimeConfigManifest(cfg, stateDir); err == nil {
			t.Fatal("later manifest verification reused stale resolved source authority")
		}
		if err := writeRuntimeConfigManifest(cfg, stateDir); err == nil {
			t.Fatal("later manifest rebuild adopted a changed authenticated source")
		}
		after, err := os.ReadFile(manifestPath)
		if err != nil || string(after) != string(manifestBefore) {
			t.Fatalf("failed manifest rebuild changed the original manifest: %v", err)
		}
		if err := os.WriteFile(path, original, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// Partial generated identity, private allowlists and omitted finite fields
// fail during actual pre-plan validation, without fabricated placeholders.
func TestRuntimeEvidenceLaunchV2TemplateRejectsPartialIdentityAndImplicitCapacity(t *testing.T) {
	cfg := runtimeEvidenceLaunchConfigTest(t)
	for _, fault := range []string{"provision", "chain", "journal", "native", "uid", "allowlist", "discovery", "owners", "bytes", "native-provider", "record-count", "gas"} {
		candidate := *cfg.Config
		candidate.Artifacts.ReservedAttemptUploads = slices.Clone(candidate.Artifacts.ReservedAttemptUploads)
		candidate.ValidatorEvidenceV2 = slices.Clone(candidate.ValidatorEvidenceV2)
		value := &candidate.Artifacts.ReservedAttemptUploads[1]
		switch fault {
		case "provision":
			candidate.ProvisionValidatorEvidenceV2 = false
			candidate.ValidatorEvidenceActivationGasUnits = 0
		case "chain":
			value.Admission.Deployment.ChainID = cfg.ChainID
		case "journal":
			value.Admission.Deployment.Journal = [20]byte{1}
		case "native":
			value.Admission.Deployment.NativeRuntime.CodeHash = cfg.Release.Runtime.CodeHash
		case "uid":
			value.Admission.Deployment.MaximumSubnetUIDs = 0
		case "allowlist":
			value.Admission.ActivationContexts = []validatorpkg.ReleaseEvidenceV2File{{Path: "/fixture/context.json", Bytes: 1, SHA256: common.Hash{1}.Hex()}}
		case "discovery":
			value.Admission.ActivationContexts = nil
		case "owners":
			value.Admission.MaximumOwners = 3
		case "bytes":
			value.Budget.BytesPerHour = 0
		case "native-provider":
			value.NativeRPCURLs = nil
		case "record-count":
			candidate.ValidatorEvidenceV2[1].Evidence.Bounds.Disk.MaxRecordCount = 0
		case "gas":
			candidate.ValidatorEvidenceActivationGasUnits = 0
		}
		if err := candidate.Validate(); err == nil {
			t.Fatalf("%s incomplete template passed actual pre-plan admission", fault)
		}
	}
	for _, value := range cfg.Config.Artifacts.ReservedAttemptUploads {
		if err := value.ValidateCapacity(); err != nil {
			t.Fatal(err)
		}
		if err := value.Admission.Validate(); err == nil {
			t.Fatal("capacity-only template became live validator staging authority")
		}
	}
}

// A valid template does not make missing or foreign retained constructor
// evidence authoritative. The ordinary journal writer creates each control.
func TestRuntimeEvidenceLaunchV2TemplateRejectsMissingAndForeignCreation(t *testing.T) {
	t.Parallel()
	cfg := runtimeEvidenceLaunchConfigTest(t)
	stateDir := t.TempDir()
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	fixture := prepareRuntimeReservedRenderTest(t, cfg, stateDir, roles)
	entries, err := readJournalEntries(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	calls := fixture.transport.rpcCalls.Load()
	for _, fault := range []string{"missing", "foreign", "intent", "raw"} {
		candidate := slices.Clone(entries)
		if fault == "missing" {
			candidate = nil
		}
		for index := range candidate {
			if fault == "foreign" {
				candidate[index].DeploymentID += "-foreign"
			} else if fault == "intent" {
				candidate[index].IntentHash = common.Hash{0xd1}.Hex()
			}
		}
		other := runtimeReservedCreationStateTest(t, stateDir, candidate)
		if fault == "raw" {
			path := filepath.Join(other, "transactions", stringsTrim0x(fixture.creation.TransactionHash)+".rlp")
			if err := os.Rename(path, path+".withheld"); err != nil {
				t.Fatal(err)
			}
		}
		before := validatorNamespaceTreeSnapshot(t, other)
		if _, err := runtimeReservedAttemptUploads(cfg, other, &fixture.deployment); err == nil {
			t.Fatalf("%s source became generated template authority", fault)
		}
		if !reflect.DeepEqual(before, validatorNamespaceTreeSnapshot(t, other)) || fixture.transport.rpcCalls.Load() != calls {
			t.Fatal("template source rejection changed retained state or attempted network recovery")
		}
	}
}

// Count admissions are distinct from expected encoded size. This pins the
// two exact phase geometries and the shared activation-anchored finite horizon.
func TestRuntimeEvidenceLaunchV2CapacityCoversBothApprovedPhases(t *testing.T) {
	cfg := runtimeEvidenceLaunchConfigTest(t)
	if cfg.Config.Scenarios.ShortEpochs != 5 || cfg.Config.Scenarios.ProductionEpochs != 3 || cfg.Policy.Verify.TrailDepth != 8 || cfg.Policy.Verify.HardSeedPerMinutePerSource != 40 || cfg.Public.Chain.ExpectedBlockSeconds != 12 {
		t.Fatal("capacity receipt no longer describes the approved launch geometry")
	}
	var totalBlocks, totalTrails uint64
	for _, phase := range []struct{ epochs, blocks, trails uint64 }{
		{epochs: 5, blocks: 300, trails: 12000},
		{epochs: 3, blocks: 360, trails: 8640},
	} {
		blocks := phase.epochs * phase.blocks
		trails := blocks * uint64(cfg.Public.Chain.ExpectedBlockSeconds) / 60 * uint64(cfg.Policy.Verify.HardSeedPerMinutePerSource)
		if trails != phase.trails {
			t.Fatal("phase count was replaced by a total-campaign estimate")
		}
		totalBlocks += blocks
		totalTrails += trails
		for _, source := range cfg.Config.ValidatorEvidenceV2 {
			bounds := source.Evidence.Bounds
			if trails > bounds.Disk.MaxTrailCount || trails*uint64(cfg.Policy.Verify.TrailDepth) > bounds.Disk.MaxRecordCount {
				t.Fatal("one source cannot retain its complete phase census")
			}
		}
	}
	if totalBlocks != 2580 || totalTrails != 20640 {
		t.Fatal("approved campaign geometry or cumulative production prefix changed")
	}
	for _, source := range cfg.Config.ValidatorEvidenceV2 {
		bounds := source.Evidence.Bounds
		minimum, err := requiredRuntimeEvidenceSourceCapacity(cfg, bounds)
		if err != nil {
			t.Fatal(err)
		}
		if bounds.Disk.MaxTrailCount < minimum.trails || bounds.Disk.MaxRecordCount < minimum.records ||
			bounds.Disk.MaxRawRecordBytes < bounds.Disk.MaxRecordCount*bounds.Disk.MaxRecordBytes || bounds.Disk.MaxProofBytes < bounds.Disk.MaxTrailCount*bounds.Replay.MaxProofBytes ||
			bounds.MaxProviders < uint64(cfg.Config.Topology.Miners) || bounds.MaxHeadEntries < uint64(cfg.Config.Topology.fleetCandidateMiners()) || bounds.MaxOperators != 2 || bounds.MaxParticipants != 2 {
			t.Fatal("configured count, accepted row bytes or actual topology exceeds its source owner")
		}
		for _, stream := range []validatorpkg.AttemptStreamV2Bounds{bounds.Cut.Records, bounds.Cut.Proofs} {
			if stream.MaxChunkBytes > 32*1024*1024 || stream.MaxPageBytes > 2*1024*1024 || stream.MaxManifestBytes > 2*1024*1024 ||
				stream.MaxChunks > stream.MaxPages*stream.MaxDescriptorsPerPage || stream.MaxDataBytes > stream.MaxChunks*stream.MaxChunkBytes {
				t.Fatal("accepted stream cannot fit its own descriptor census or typed public endpoint")
			}
		}
		for _, destination := range cfg.Config.Artifacts.ReservedAttemptUploads {
			if destination.Budget.ObjectsPerHour < minimum.objectsPerHour || destination.Budget.BytesPerHour < minimum.bytesPerHour || destination.Budget.RetryRequestsPerHour < minimum.retryRequestsPerHour || destination.Admission.MaximumOwners < 4 ||
				destination.Admission.BlocksPerRange*destination.Admission.MaximumRanges < minimum.span || source.Evidence.UploadIntentSeconds > destination.Admission.MaximumIntentSeconds {
				t.Fatal("whole retained prefix/catch-up workload does not fit each original-owner reservation")
			}
		}
	}
	if err := validateRuntimeEvidenceSourceCapacity(cfg); err != nil {
		t.Fatal(err)
	}
	gas, err := evidenceRelayMaximumGas(cfg)
	if err != nil || gas != DecimalUint("25600000000000000000") || cfg.Config.ValidatorEvidenceRelay.MaxSlots != 256 || cfg.Config.ValidatorEvidenceActivationGasUnits != 1000000 {
		t.Fatalf("finite keeper relay/activation reserve changed: %s, %v", gas, err)
	}
}
