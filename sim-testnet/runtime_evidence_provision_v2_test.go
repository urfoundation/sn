//go:build linux || darwin

// Real signatures, immutable descriptors and the approved plan exercise the
// setup/rendering join. Separate transport tests authenticate chain history.
package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/protocol"
	validatorcomponent "github.com/urfoundation/sn/v2026/validator"
)

type runtimeEvidenceProvisionV2TestFixture struct {
	cfg           *ResolvedConfig
	plan          *SetupPlan
	roles         *RoleSecrets
	stateDir      string
	prepared      *runtimeEvidenceActivationPreparedV2
	preparedBytes []byte
	completed     *runtimeEvidenceActivationCompletedV2
}

// Existing simulator fixtures provide topology, generated deployment and
// test-only capacities. No historical chain authority is asserted here.
func newRuntimeEvidenceProvisionV2TestFixture(t *testing.T) *runtimeEvidenceProvisionV2TestFixture {
	t.Helper()
	return newRuntimeEvidenceProvisionV2ConfiguredTestFixture(t, nil)
}

// Configure test-owned origins and finite allowances before approving any
// plan or signing an activation. Existing callers retain their exact profile.
func newRuntimeEvidenceProvisionV2ConfiguredTestFixture(t *testing.T, configure func(*ResolvedConfig)) *runtimeEvidenceProvisionV2TestFixture {
	t.Helper()
	cfg, roles, payloads := validatorEvidenceInstallTest(t)
	configureRuntimeEvidenceV2Test(t, cfg, t.TempDir())
	cfg.Config.ValidatorEvidenceV2 = runtimeEvidenceTemplateV2(cfg.Config.ValidatorEvidenceV2)
	cfg.Config.ProvisionValidatorEvidenceV2 = true
	cfg.Config.ValidatorEvidenceActivationGasUnits = 1_000_000
	for index := range cfg.Config.ValidatorEvidenceV2 {
		bounds := &cfg.Config.ValidatorEvidenceV2[index].Evidence.Bounds
		// Real activation contexts use this allowance on the same public
		// metadata endpoint as page/manifest objects. Keep both owners coherent.
		bounds.Cut.MaxHeaderBytes = 64 * 1024
		bounds.Cut.Records.MaxPageBytes = bounds.Cut.MaxHeaderBytes
		if err := bounds.Validate(uint64(cfg.Config.Topology.Operators)); err != nil {
			t.Fatalf("provision fixture has unusable public evidence bounds: %v", err)
		}
	}
	if configure != nil {
		configure(cfg)
	}
	var err error
	cfg.ConfigHash, err = releaseConfigHash(cfg.Config, cfg.Public, cfg.Hyperparameters)
	if err != nil {
		t.Fatal(err)
	}
	publicRoles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	facts := *testSetupFacts()
	facts.DeployerNonce = payloads.Manifest.InitialNonce
	plan, err := buildPlan(cfg, &facts, publicRoles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	if err := validatePlanBudget(plan); err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(t.TempDir(), "state")
	if err := ensurePrivateDir(stateDir); err != nil {
		t.Fatal(err)
	}
	wire, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "plan.json"), append(wire, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	policy, err := decodeHex32("test activation policy", cfg.PolicyHash)
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
			activation := protocol.ValidatorEvidenceActivation{Domain: protocol.ValidatorEvidenceActivationDomain{ChainID: testnetChainID, GenesisHash: [32]byte(companion.GenesisHash), Netuid: cfg.Netuid,
				Coordinator: [20]byte(companion.Coordinator), SettlementVault: [20]byte(companion.SettlementVault), DeploymentIDHash: [32]byte(companion.DeploymentIDHash), PolicyHash: policy, Epoch: prepared.Epoch},
				Hotkey: hotkey.PublicKey(), NoID: uint64(noId), VPK: [32]byte(key[ed25519.SeedSize:]), FirstSequence: 1, NativeBlock: 100, NativeHash: [32]byte{0x31}, EVMBlock: 200, EVMHash: [32]byte{0x32}}
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
	return &runtimeEvidenceProvisionV2TestFixture{cfg: cfg, plan: plan, roles: roles, stateDir: stateDir, prepared: prepared, preparedBytes: encoded, completed: completed}
}

func TestRuntimeEvidenceProvisionV2FixedPlanBoundsAllFourKeeperActions(t *testing.T) {
	t.Parallel()
	fixture := newRuntimeEvidenceProvisionV2TestFixture(t)
	actions := map[string]Action{}
	for _, action := range fixture.plan.Actions {
		actions[action.ID] = action
	}
	boundary := actions[runtimeEvidenceActivationBoundaryActionId]
	if len(boundary.DependsOn) != 4 || !slices.Contains(actions["config.render"].DependsOn, boundary.ID) {
		t.Fatal("render can bypass the complete source activation boundary")
	}
	for validatorId := 1; validatorId <= 2; validatorId++ {
		for noId := 1; noId <= 2; noId++ {
			action := actions[runtimeEvidenceActivationActionId(validatorId, noId)]
			units, _, err := evmActionFeeEnvelope(action)
			if err != nil || units != fixture.cfg.Config.ValidatorEvidenceActivationGasUnits {
				t.Fatalf("fixed source quota differs: %d, %v", units, err)
			}
			if !slices.Contains(action.DependsOn, "evm.fund-keeper") || !slices.Contains(boundary.DependsOn, action.ID) {
				t.Fatal("activation has no approved keeper funding or complete boundary")
			}
			executor := &Executor{cfg: fixture.cfg, plan: fixture.plan}
			if _, err := executor.runtimeEvidenceActivationMemberV2(action, fixture.prepared); err != nil {
				t.Fatal(err)
			}
			action.Target = common.Address{0x91}.Hex()
			if _, err := executor.runtimeEvidenceActivationMemberV2(action, fixture.prepared); err == nil {
				t.Fatal("changed source action reused the original slot quota")
			}
		}
	}
}

// The actual fixture's allowed signed header must fit the same typed public
// metadata reader; one extra byte is refused by unchanged production admission.
func TestRuntimeEvidenceProvisionV2HeaderCapacityMatchesPublicReader(t *testing.T) {
	t.Parallel()
	fixture := newRuntimeEvidenceProvisionV2TestFixture(t)
	for _, configured := range fixture.cfg.Config.ValidatorEvidenceV2 {
		bounds := configured.Evidence.Bounds
		metadataBytes := max(bounds.Cut.Records.MaxPageBytes, bounds.Cut.Records.MaxManifestBytes, bounds.Cut.Proofs.MaxPageBytes, bounds.Cut.Proofs.MaxManifestBytes)
		if bounds.Cut.MaxHeaderBytes != 64*1024 || bounds.Cut.MaxHeaderBytes != metadataBytes {
			t.Fatalf("fixture header and actual public metadata owners differ: %+v", bounds.Cut)
		}
		if err := bounds.Validate(uint64(fixture.cfg.Config.Topology.Operators)); err != nil {
			t.Fatal(err)
		}
		bounds.Cut.MaxHeaderBytes++
		if err := bounds.Validate(uint64(fixture.cfg.Config.Topology.Operators)); err == nil || !strings.Contains(err.Error(), "header allowance exceeds its public metadata bound") {
			t.Fatalf("one-byte larger header reached public evidence admission: %v", err)
		}
	}
}

func TestRuntimeEvidenceProvisionV2RetainsActualFilesAndResolvesSameTemplate(t *testing.T) {
	t.Parallel()
	fixture := newRuntimeEvidenceProvisionV2TestFixture(t)
	executor := &Executor{cfg: fixture.cfg, plan: fixture.plan, roles: fixture.roles, stateDir: fixture.stateDir}
	if err := executor.retainRuntimeEvidenceInputsV2(t.Context(), fixture.prepared, fixture.preparedBytes, fixture.completed); err != nil {
		t.Fatal(err)
	}
	first, err := runtimeEvidenceV2ResolvedConfig(fixture.cfg, fixture.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := executor.retainRuntimeEvidenceInputsV2(t.Context(), fixture.prepared, fixture.preparedBytes, fixture.completed); err != nil {
		t.Fatal(err)
	}
	second, err := runtimeEvidenceV2ResolvedConfig(fixture.cfg, fixture.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first.Config.ValidatorEvidenceV2, second.Config.ValidatorEvidenceV2) {
		t.Fatal("restart selected different signed inputs")
	}
	if err := validateRuntimeEvidenceProvisionTemplateV2(fixture.cfg.Config); err != nil {
		t.Fatalf("resolver mutated the approved template: %v", err)
	}
	before, err := runtimeEvidenceV2Identity(fixture.cfg)
	if err != nil {
		t.Fatal(err)
	}
	after, err := runtimeEvidenceV2Identity(first)
	if err != nil || before != after {
		t.Fatal("resolved references changed the original explicit policy identity")
	}
	for _, validator := range first.Config.ValidatorEvidenceV2 {
		for _, operator := range validator.Evidence.Operators {
			for _, reference := range operator.Files() {
				info, err := os.Lstat(reference.Path)
				if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
					t.Fatalf("input is not a private immutable file: %v", err)
				}
			}
		}
	}
}

func TestRuntimeEvidenceProvisionV2InterruptedLastSourceResumesExactConsents(t *testing.T) {
	t.Parallel()
	fixture := newRuntimeEvidenceProvisionV2TestFixture(t)
	last, _, _ := runtimeEvidenceV2Paths(fixture.stateDir, 2, 2)
	if err := os.MkdirAll(filepath.Dir(last[4]), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(fixture.stateDir, "foreign-history"), last[4]); err != nil {
		t.Fatal(err)
	}
	executor := &Executor{cfg: fixture.cfg, plan: fixture.plan, roles: fixture.roles, stateDir: fixture.stateDir}
	if err := executor.retainRuntimeEvidenceInputsV2(t.Context(), fixture.prepared, fixture.preparedBytes, fixture.completed); err == nil {
		t.Fatal("last source symlink was accepted")
	}
	if _, err := os.Lstat(filepath.Join(fixture.stateDir, "evidence-v2-setup", "completed.json")); !os.IsNotExist(err) {
		t.Fatal("partial file census was marked complete")
	}
	first, _, _ := runtimeEvidenceV2Paths(fixture.stateDir, 1, 1)
	before, err := os.ReadFile(first[2])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(last[4]); err != nil {
		t.Fatal(err)
	}
	if err := executor.retainRuntimeEvidenceInputsV2(t.Context(), fixture.prepared, fixture.preparedBytes, fixture.completed); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(first[2])
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("restart re-signed an already retained hotkey consent")
	}
	if _, err := runtimeEvidenceV2ResolvedConfig(fixture.cfg, fixture.stateDir); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeEvidenceProvisionV2ChangedInputCannotBeRehashedIntoAuthority(t *testing.T) {
	t.Parallel()
	fixture := newRuntimeEvidenceProvisionV2TestFixture(t)
	executor := &Executor{cfg: fixture.cfg, plan: fixture.plan, roles: fixture.roles, stateDir: fixture.stateDir}
	if err := executor.retainRuntimeEvidenceInputsV2(t.Context(), fixture.prepared, fixture.preparedBytes, fixture.completed); err != nil {
		t.Fatal(err)
	}
	paths, _, _ := runtimeEvidenceV2Paths(fixture.stateDir, 2, 2)
	if err := os.WriteFile(paths[3], []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeEvidenceV2ResolvedConfig(fixture.cfg, fixture.stateDir); err == nil {
		t.Fatal("changed context became authority through a private locator")
	}
}

func TestRuntimeEvidenceProvisionV2SignedForeignSourceIsRefusedBeforeFiles(t *testing.T) {
	t.Parallel()
	fixture := newRuntimeEvidenceProvisionV2TestFixture(t)
	changed := *fixture.prepared
	changed.Members = slices.Clone(changed.Members)
	changed.Members[3].Activation.NoID = 1
	if _, _, err := runtimeEvidenceFixedInputsV2(fixture.cfg, fixture.plan, fixture.stateDir, fixture.roles, &changed, fixture.completed); err == nil {
		t.Fatal("foreign signed source was accepted")
	}
	if _, err := os.Lstat(filepath.Join(fixture.stateDir, "runtime")); !os.IsNotExist(err) {
		t.Fatal("source admission touched runtime files")
	}
}

func TestRuntimeEvidenceProvisionV2NoImplicitBudgetOrFabricatedReferences(t *testing.T) {
	t.Parallel()
	fixture := newRuntimeEvidenceProvisionV2TestFixture(t)
	fixture.cfg.Config.ValidatorEvidenceActivationGasUnits = 0
	if err := validateRuntimeEvidenceProvisionTemplateV2(fixture.cfg.Config); err == nil {
		t.Fatal("missing action gas was defaulted")
	}
	fixture.cfg.Config.ValidatorEvidenceActivationGasUnits = 1_000_000
	fixture.cfg.Config.ValidatorEvidenceV2[1].Evidence.Operators[1].History = validatorcomponent.ReleaseEvidenceV2File{Path: "/fabricated", Bytes: 1, SHA256: common.Hash{1}.Hex()}
	if err := validateRuntimeEvidenceProvisionTemplateV2(fixture.cfg.Config); err == nil {
		t.Fatal("fresh mode accepted a fabricated history reference")
	}
}

func TestRuntimeEvidenceProvisionV2FreshSetupPreservesExistingDiskHistory(t *testing.T) {
	t.Parallel()
	fixture := newRuntimeEvidenceProvisionV2TestFixture(t)
	path := filepath.Join(fixture.stateDir, "runtime", "validator-2", "state", "operators", "no-2", "attempt-ledger.records")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	before := []byte("retained signed disk history")
	if err := os.WriteFile(path, before, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := requireRuntimeEvidenceFreshStateV2(fixture.cfg, fixture.stateDir); err == nil {
		t.Fatal("fresh setup replaced retained disk history")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("refused setup changed existing history")
	}
}
