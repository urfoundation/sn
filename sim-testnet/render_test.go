package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	minerpkg "github.com/urfoundation/sn/miner"
	validatorpkg "github.com/urfoundation/sn/validator"
	"gopkg.in/yaml.v3"
)

func TestRenderRuntimeConfigsAreAcceptedByReleaseLoaders(t *testing.T) {
	cfg := testResolvedConfig(t)
	cfg.Authority = "http://127.0.0.1:9944"
	stateDir := t.TempDir()
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for label, client := range roles.Clients {
		client.ClientIDHex = strings.Repeat("01", 16)
		roles.Clients[label] = client
	}
	deployment := ContractDeployment{
		Schema: "urnetwork-contract-deployment-v1", DeploymentID: cfg.Config.Deployment.DeploymentID,
		ReserveSink:               common.HexToAddress("0x1000000000000000000000000000000000000001"),
		SettlementVault:           common.HexToAddress("0x2000000000000000000000000000000000000002"),
		CoordinatorImplementation: common.HexToAddress("0x3000000000000000000000000000000000000003"),
		CoordinatorProxy:          common.HexToAddress("0x4000000000000000000000000000000000000004"),
		DeployBlock:               123, DeployBlockHash: "0x" + strings.Repeat("ab", 32), RuntimeHashes: map[string]string{},
	}
	if err := saveContractDeployment(stateDir, deployment); err != nil {
		t.Fatal(err)
	}
	if err := RenderRuntimeConfigs(cfg, stateDir, roles); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= cfg.Config.Topology.Validators; i++ {
		path := filepath.Join(stateDir, "runtime", "validator-"+strconv.Itoa(i), "validator.yml")
		loaded, err := validatorpkg.LoadReleaseConfig(path)
		if err != nil {
			t.Fatalf("validator %d rendered config: %v", i, err)
		}
		if len(loaded.Operators) != cfg.Config.Topology.Operators || loaded.PolicyHash != cfg.PolicyHash || loaded.Policy.ProductionCadence.EpochBlocks != 50_400 || loaded.Policy.Settlement.CloseGraceBlocks != 5 {
			t.Fatalf("validator %d config incomplete: %+v", i, loaded)
		}
		wantControlled := controlledNOIDsForValidator(i)
		if len(loaded.ControlledNOIDs) != len(wantControlled) || (len(wantControlled) != 0 && loaded.ControlledNOIDs[0] != wantControlled[0]) {
			t.Fatalf("validator %d controlled NOs = %v, want %v", i, loaded.ControlledNOIDs, wantControlled)
		}
	}
	for i := 1; i <= cfg.Config.Topology.Miners; i++ {
		path := filepath.Join(stateDir, "runtime", "miner-"+strconv.Itoa(i), "claim-daemon.yml")
		loaded, err := minerpkg.LoadClaimDaemonConfig(path)
		if err != nil {
			t.Fatalf("miner %d claim daemon rendered config: %v", i, err)
		}
		if loaded.LookbackEpochs == 0 || len(loaded.RPC) != 1 {
			t.Fatalf("miner %d claim config incomplete: %+v", i, loaded)
		}
		wantKey := filepath.Join(stateDir, "secrets", "miner-"+strconv.Itoa(i)+"-claim-relayer.key")
		if loaded.KeyFile != wantKey {
			t.Fatalf("miner %d claim key = %q, want %q", i, loaded.KeyFile, wantKey)
		}
		keyBytes, readErr := os.ReadFile(wantKey)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if strings.TrimSpace(string(keyBytes)) != "0x"+roles.EVM["miner-"+strconv.Itoa(i)+"-claim-relayer"].PrivateKeyHex {
			t.Fatalf("miner %d does not use its isolated claim relayer", i)
		}
	}
	stPath := filepath.Join(stateDir, "runtime", "operator-1", "vault", "st.yml")
	b, err := os.ReadFile(stPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	if !strings.Contains(text, "testnet-coordinator-address") || !strings.Contains(text, "testnet-deposit-tiers") || !strings.Contains(text, "rate_numerator_rao_per_gib") || strings.Contains(text, "\ndeposit_key:") || strings.Contains(text, "\nops_key:") {
		t.Fatalf("operator st config did not stay testnet-scoped:\n%s", text)
	}
	minio, err := os.ReadFile(filepath.Join(stateDir, "runtime", "operator-1", "vault", "minio.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(minio), "blob/sim-testnet/"+cfg.Config.Deployment.DeploymentID+"/operator-1") {
		t.Fatalf("operator MinIO prefix is not deployment-isolated: %s", minio)
	}
}

func TestOperatorEnvironmentUsesDiscoveredPlatformConfig(t *testing.T) {
	cfg := testResolvedConfig(t)
	cfg.Repos.PlatformConfig = filepath.Join(t.TempDir(), "portable-config")
	env := operatorBaseEnv(cfg, t.TempDir(), 1, "127.0.0.11")
	want := filepath.Join(cfg.Repos.PlatformConfig, "local")
	if env["WARP_CONFIG_HOME"] != want {
		t.Fatalf("WARP_CONFIG_HOME = %q, want %q", env["WARP_CONFIG_HOME"], want)
	}
}

func TestOperatorVerifyConfigRotatesWithoutDroppingHistoricalKey(t *testing.T) {
	cfg := testResolvedConfig(t)
	type key struct {
		ServerKeyID int    `yaml:"server_key_id"`
		Seed        string `yaml:"seed"`
	}
	decode := func(rotated bool) ([]key, map[byte]string) {
		b, public, err := operatorVerifyConfig(cfg, 1, rotated)
		if err != nil {
			t.Fatal(err)
		}
		var value struct {
			Keys []key `yaml:"keys"`
		}
		if err := yaml.Unmarshal(b, &value); err != nil {
			t.Fatal(err)
		}
		return value.Keys, public
	}
	initial, initialPublic := decode(false)
	rotated, rotatedPublic := decode(true)
	if len(initial) != 1 || initial[0].ServerKeyID != 0 || len(initialPublic) != 1 {
		t.Fatalf("initial keys = %+v public=%v", initial, initialPublic)
	}
	if len(rotated) != 2 || rotated[0].ServerKeyID != 1 || rotated[1].ServerKeyID != 0 || rotated[1].Seed != initial[0].Seed || rotated[0].Seed == rotated[1].Seed || rotatedPublic[0] != initialPublic[0] || rotatedPublic[1] == "" {
		t.Fatalf("rotated keys = %+v public=%v", rotated, rotatedPublic)
	}
}
