package main

// Full rendering must discover protected validator authority before touching
// any operator configuration, copied credential or deployment artifact.

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// The namespace classifier is already fixed in the intended causal baseline;
// only its late position in the full renderer still permits earlier writes.
func TestValidatorStateNamespaceRenderRejectsBeforeAnyMutation(t *testing.T) {
	fixture := readValidatorNamespaceStoreFixture(t)
	cfg := testResolvedConfig(t)
	cfg.Config.Topology.Miners = 2
	cfg.Config.Topology.MinerSwarmProcesses = 1
	cfg.Public.Chain.EVMPublicReadEndpoint = "https://test.chain.opentensor.ai"
	cfg.Repos.PlatformConfig = testOperatorConfigSources(t)
	cfg.Repos.Vault = t.TempDir()
	writeValidatorStateTestFile(t, filepath.Join(cfg.Repos.Vault, "local", "provider_egress.yml"), "ingest_secret: retained-test-credential\n")
	writeValidatorStateTestFile(t, filepath.Join(cfg.Repos.Vault, "main", "minio.yml"), "authority: fixture.invalid:23900\ntls: true\nbucket: blob\naccess_key: test-access\nsecret_key: test-secret\n")
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	stateDir := t.TempDir()
	deployment := ContractDeployment{
		Schema: "urnetwork-contract-deployment-v1", DeploymentID: cfg.Config.Deployment.DeploymentID,
		ReserveSink:               common.HexToAddress("0x1000000000000000000000000000000000000001"),
		SettlementVault:           common.HexToAddress("0x2000000000000000000000000000000000000002"),
		CoordinatorImplementation: common.HexToAddress("0x3000000000000000000000000000000000000003"),
		CoordinatorProxy:          common.HexToAddress("0x4000000000000000000000000000000000000004"),
		DeployBlock:               123, DeployBlockHash: "0x" + strings.Repeat("ab", 32), RuntimeHashes: map[string]string{},
		CoordinatorEventStartBlock: 100, CoordinatorEventStartBlockHash: "0x" + strings.Repeat("cd", 32),
	}
	if err := saveContractDeployment(stateDir, deployment); err != nil {
		t.Fatal(err)
	}
	if err := saveRoleSecrets(filepath.Join(stateDir, "role-secrets.json"), roles); err != nil {
		t.Fatal(err)
	}
	writeValidatorStateTestFile(t, filepath.Join(stateDir, "journal.jsonl"), "retained deployment journal\n")
	writeValidatorStateTestFile(t, filepath.Join(stateDir, "runtime", "operator-1", "vault", "st.yml"), "retained operator configuration\n")
	writeValidatorStateTestFile(t, filepath.Join(stateDir, "runtime", "operator-1", "vault", "provider_egress.yml"), "retained original credential\n")
	writeValidatorStateTestFile(t, filepath.Join(stateDir, "runtime", "validator-1", "state", "operators", "no-1", "stats.json"), `{"assignments":1}`)
	installValidatorNamespaceStoreFixture(t, filepath.Join(stateDir, "runtime", "validator-2", "state", "operators", "no-1", "attempt-ledger.records"), fixture)
	before := validatorNamespaceTreeSnapshot(t, stateDir)
	err = RenderRuntimeConfigs(cfg, stateDir, roles)
	if err == nil || !strings.Contains(err.Error(), "protected disk attempt-ledger state") {
		t.Fatalf("full renderer did not reach protected disk refusal: %v", err)
	}
	if !reflect.DeepEqual(before, validatorNamespaceTreeSnapshot(t, stateDir)) {
		t.Fatal("full renderer changed configuration, credentials or deployment state before refusing protected disk authority")
	}
}
