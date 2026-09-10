// Real generated deployment bytes, an approved plan and the ordinary signed
// sender provision renderer prerequisites. They do not authorize future keys.
package main

import (
	"bytes"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/crv4"
	validatorpkg "github.com/urfoundation/sn/validator"
	"github.com/urnetwork/server/controller"
	"github.com/urnetwork/server/model"
)

// Independent creation history remains separate from the earlier coordinator
// event-sync boundary and from the later complete deployment observation.
type runtimeReservedRenderTestFixture struct {
	plan       *SetupPlan
	deployment ContractDeployment
	creation   JournalEntry
	transport  *validatorEvidenceExecutorRPC
}

// Config pins come from generated immutable bytes and the reviewed native
// profile. Empty discovery references are not a validator staging allowlist.
func prepareRuntimeReservedRenderTest(t *testing.T, cfg *ResolvedConfig, stateDir string, roles *RoleSecrets) *runtimeReservedRenderTestFixture {
	t.Helper()
	// testing.TempDir creates its numbered child with 0777 before umask;
	// this fixture explicitly owns a private state root before journal setup.
	if err := os.Chmod(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := ensurePrivateDir(stateDir); err != nil {
		t.Fatal(err)
	}
	payloads, err := buildDeploymentPayloads(cfg, roles, 41)
	if err != nil || payloads == nil || payloads.ValidatorEvidence == nil {
		t.Fatalf("generated renderer companion is unavailable: %v", err)
	}
	companion := payloads.ValidatorEvidence.Manifest
	runtime := crv4.RuntimeArtifactIdentity{Version: crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: cfg.Public.Chain.ExpectedRuntimeSpec,
		TransactionVersion: cfg.Public.Chain.ExpectedTransactionVersion, StateVersion: cfg.Public.Chain.ExpectedStateVersion}, CodeHash: cfg.Release.Runtime.CodeHash, MetadataHash: cfg.Release.Runtime.MetadataHash}
	domain := validatorpkg.ValidatorUploadDeployment{ChainID: cfg.ChainID, GenesisHash: [32]byte(companion.GenesisHash), Netuid: cfg.Netuid,
		Coordinator: [20]byte(companion.Coordinator), SettlementVault: [20]byte(companion.SettlementVault), DeploymentIDHash: [32]byte(companion.DeploymentIDHash),
		Journal: [20]byte(companion.Address), RuntimeHash: [32]byte(companion.RuntimeCodeHash), DeploymentBlock: 100, NativeRuntime: runtime, MaximumSubnetUIDs: 256}
	if !cfg.Config.ProvisionValidatorEvidenceV2 {
		cfg.Config.Artifacts.ReservedAttemptUploads = nil
		for noId := 1; noId <= cfg.Config.Topology.Operators; noId++ {
			cfg.Config.Artifacts.ReservedAttemptUploads = append(cfg.Config.Artifacts.ReservedAttemptUploads, controller.StReservedAttemptUploadConfig{
				Admission: validatorpkg.ValidatorUploadAdmissionConfig{Deployment: domain, ReplicaNoID: uint64(noId), ActivationContexts: []validatorpkg.ReleaseEvidenceV2File{},
					MaximumContextBytes: 64 * 1024, MaximumOwners: 4, BlocksPerRange: 100, MaximumRanges: 16, MaximumEventsPerRange: 16,
					RefreshSeconds: 10, MaximumRefreshSeconds: 20, MaximumHeadAgeSeconds: 60, MaximumIntentSeconds: 60, FreshActivePerOwner: 1, RetryActivePerOwner: 1},
				Budget:        model.StReservedAttemptUploadBudget{RetryRequestsPerHour: 64, ObjectsPerHour: 128, BytesPerHour: 16 * 1024 * 1024},
				NativeRPCURLs: []string{"ws://" + workloadSubstrateRPCAuthority()},
			})
		}
		for index := range cfg.Config.ValidatorEvidenceV2 {
			cfg.Config.ValidatorEvidenceV2[index].Evidence.UploadIntentSeconds = 30
		}
	}
	if err := validateRuntimeReservedAttemptUploadCensus(cfg.Config); err != nil {
		t.Fatal(err)
	}
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
	if err := validateValidatorEvidenceDeployment(plan.ValidatorEvidence, payloads.ValidatorEvidence); err != nil {
		t.Fatal(err)
	}
	if err := writeRunInputs(cfg, stateDir, plan, roles); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadPersistedPlan(cfg, stateDir)
	if err != nil || loaded.PlanHash != plan.PlanHash {
		t.Fatalf("renderer plan did not survive actual current approval admission: %v", err)
	}
	action, err := exactPlanActionByID(plan, validatorEvidenceDeployActionID)
	if err != nil {
		t.Fatal(err)
	}
	role, err := roles.EVMKey("deployer")
	if err != nil {
		t.Fatal(err)
	}
	key, err := crypto.HexToECDSA(role.PrivateKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	transport := &validatorEvidenceExecutorRPC{base: validatorEvidenceInstallRPCTest(t, payloads.ValidatorEvidence), owner: crypto.PubkeyToAddress(key.PublicKey), nonce: companion.DeployerNonce}
	client := transport.client(t)
	journal, err := OpenJournal(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := journal.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := journal.Append(JournalEntry{DeploymentID: plan.DeploymentID, PlanHash: plan.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageIntent}); err != nil {
		t.Fatal(err)
	}
	manager := &EvmTxManager{client: client, chainID: new(big.Int).SetUint64(cfg.ChainID), deploymentID: plan.DeploymentID, stateDir: stateDir, journal: journal, key: key}
	receipt, err := manager.Send(t.Context(), plan.PlanHash, action, nil, new(big.Int), payloads.ValidatorEvidence.Creation)
	if err != nil || receipt == nil || receipt.Status != types.ReceiptStatusSuccessful || transport.sendCalls.Load() != 1 {
		t.Fatalf("actual signed renderer companion creation failed: %v", err)
	}
	creation, found := journal.LatestTransaction(plan.PlanHash, action.ID, action.IntentHash)
	if !found || creation.Stage != StageFinalized || creation.TransactionHash != receipt.TxHash.Hex() || creation.BlockNumber != receipt.BlockNumber.Uint64() || creation.BlockHash != receipt.BlockHash.Hex() {
		t.Fatal("renderer companion has no exact canonical finalized journal observation")
	}
	raw, err := os.ReadFile(filepath.Join(stateDir, "transactions", stringsTrim0x(receipt.TxHash.Hex())+".rlp"))
	if err != nil {
		t.Fatal(err)
	}
	var transaction types.Transaction
	if err := transaction.UnmarshalBinary(raw); err != nil {
		t.Fatal(err)
	}
	signer, err := types.Sender(types.LatestSignerForChainID(manager.chainID), &transaction)
	if err != nil || transaction.Hash() != receipt.TxHash || transaction.To() != nil || !transaction.Protected() ||
		transaction.ChainId().Cmp(manager.chainID) != 0 || signer != companion.Deployer || transaction.Nonce() != companion.DeployerNonce ||
		crypto.CreateAddress(signer, transaction.Nonce()) != companion.Address || transaction.Value().Sign() != 0 || !bytes.Equal(transaction.Data(), payloads.ValidatorEvidence.Creation) {
		t.Fatalf("retained renderer creation differs from actual generated signed bytes: %v", err)
	}
	if cfg.OperationalRPCMode != rpcModePublicOverride {
		t.Fatal("renderer source fixture requires the explicit shared-provider test profile")
	}
	executor := &Executor{cfg: cfg, stateDir: stateDir, plan: plan, roles: roles, payloads: payloads, journal: journal, deployer: manager}
	head := ChainHead{Number: creation.BlockNumber, Hash: creation.BlockHash}
	observed, err := executor.actionPostState(t.Context(), action, head)
	if err != nil {
		t.Fatalf("renderer source postcondition did not read actual generated code and immutable getters: %v", err)
	}
	shared, err := cloneObservedPostState(observed)
	if err != nil {
		t.Fatal(err)
	}
	nativeHead := ChainHead{Number: facts.FinalizedBlock, Hash: facts.FinalizedBlockHash}
	record := &ActionPostcondition{Schema: "urnetwork-sim-action-postcondition-v4", DeploymentID: plan.DeploymentID, PlanHash: plan.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash,
		OperationalRPCMode: cfg.OperationalRPCMode, IndependentRPC: false, SubstrateFinalized: nativeHead, EVMFinalized: head, EVMHashDomain: "evm-rpc", Observed: observed,
		IndependentSubstrateFinalized: nativeHead, IndependentEVMFinalized: head, IndependentEVMHashDomain: "evm-rpc", IndependentObserved: shared}
	if err := validateActionPostconditionV4(record); err != nil {
		t.Fatal(err)
	}
	postconditionPath, postconditionHash, err := executor.persistActionPostcondition(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.Append(JournalEntry{DeploymentID: plan.DeploymentID, PlanHash: plan.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash,
		Stage: StageVerified, PostconditionPath: postconditionPath, PostconditionHash: postconditionHash}); err != nil {
		t.Fatal(err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	retained, err := readJournalEntries(stateDir)
	if err != nil || !slices.ContainsFunc(retained, func(value JournalEntry) bool { return reflect.DeepEqual(value, creation) }) {
		t.Fatalf("actual renderer creation did not survive journal readback: %v", err)
	}
	deployment := payloads.Manifest
	deployment.DeployBlock, deployment.DeployBlockHash = creation.BlockNumber, creation.BlockHash
	deployment.CoordinatorEventStartBlock, deployment.CoordinatorEventStartBlockHash = transport.base.head.Number, transport.base.head.Hash
	if err := saveContractDeployment(stateDir, deployment); err != nil {
		t.Fatal(err)
	}
	values, err := runtimeReservedAttemptUploads(cfg, stateDir, &deployment)
	if err != nil || len(values) != len(cfg.Config.Artifacts.ReservedAttemptUploads) {
		t.Fatalf("complete generated renderer authority was not admitted: %v", err)
	}
	for index, value := range values {
		if runtimeReservedAttemptUploadIsTemplate(cfg.Config.Artifacts.ReservedAttemptUploads[index]) {
			value.Admission.Deployment = validatorpkg.ValidatorUploadDeployment{MaximumSubnetUIDs: value.Admission.Deployment.MaximumSubnetUIDs}
		}
		if !reflect.DeepEqual(value, cfg.Config.Artifacts.ReservedAttemptUploads[index]) {
			t.Fatal("actual source resolution changed an approved capacity or discovery field")
		}
	}
	return &runtimeReservedRenderTestFixture{plan: plan, deployment: deployment, creation: creation, transport: transport}
}

// The real top-level renderer must stop before overlays or output mutation
// when any retained setup source or explicit destination authority is absent.
func TestRuntimeEvidenceV2ReservedRendererRejectsMissingOrChangedSetup(t *testing.T) {
	t.Parallel()
	cfg := testResolvedConfig(t)
	cfg.OperationalRPCMode = rpcModePublicOverride
	cfg.Public.Chain.EVMPublicReadEndpoint = "https://test.chain.opentensor.ai"
	budget, err := requiredRuntimeClientKeyUploadBudget(cfg)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Config.Artifacts.AttemptUpload = &budget
	stateDir := t.TempDir()
	configureRuntimeEvidenceV2Test(t, cfg, stateDir)
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	fixture := prepareRuntimeReservedRenderTest(t, cfg, stateDir, roles)
	for _, fault := range []string{"capacity", "plan", "creation", "runtime", "companion", "late-discovery", "intent-zero", "intent-long"} {
		candidate, harness := *cfg, *cfg.Config
		harness.Artifacts.ReservedAttemptUploads = slices.Clone(cfg.Config.Artifacts.ReservedAttemptUploads)
		harness.ValidatorEvidenceV2 = slices.Clone(cfg.Config.ValidatorEvidenceV2)
		candidate.Config = &harness
		hidden := ""
		switch fault {
		case "capacity":
			harness.Artifacts.ReservedAttemptUploads = nil
		case "plan":
			hidden = filepath.Join(stateDir, "plan.json")
		case "creation":
			hidden = filepath.Join(stateDir, "journal.jsonl")
		case "runtime":
			harness.Artifacts.ReservedAttemptUploads[1].Admission.Deployment.NativeRuntime.CodeHash = common.Hash{0xd1}.Hex()
		case "companion":
			harness.Artifacts.ReservedAttemptUploads[1].Admission.Deployment.Journal[0] ^= 1
		case "late-discovery":
			harness.Artifacts.ReservedAttemptUploads[1].Admission.Deployment.DeploymentBlock = fixture.creation.BlockNumber + 1
		case "intent-zero":
			harness.ValidatorEvidenceV2[1].Evidence.UploadIntentSeconds = 0
		case "intent-long":
			harness.ValidatorEvidenceV2[1].Evidence.UploadIntentSeconds = harness.Artifacts.ReservedAttemptUploads[1].Admission.MaximumIntentSeconds + 1
		}
		if hidden != "" {
			if err := os.Rename(hidden, hidden+".withheld"); err != nil {
				t.Fatal(err)
			}
		}
		before := validatorNamespaceTreeSnapshot(t, stateDir)
		renderErr := RenderRuntimeConfigs(&candidate, stateDir, roles)
		after := validatorNamespaceTreeSnapshot(t, stateDir)
		if hidden != "" {
			if err := os.Rename(hidden+".withheld", hidden); err != nil {
				t.Fatal(err)
			}
		}
		if renderErr == nil || !reflect.DeepEqual(before, after) {
			t.Fatalf("%s invalid staging source reached rendering or changed its retained namespace: %v", fault, renderErr)
		}
		if fault != "plan" && !strings.Contains(renderErr.Error(), "reserved staging") {
			t.Fatalf("%s did not reach the actual protected-source admission: %v", fault, renderErr)
		}
	}
	if values, err := runtimeReservedAttemptUploads(cfg, stateDir, &fixture.deployment); err != nil || len(values) != cfg.Config.Topology.Operators {
		t.Fatalf("negative rendering poisoned the original generated plan or creation: %v", err)
	}
}
