package main

import (
	"net/netip"
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

// Every simulated source must resolve into BestAvailable's US candidate pool
// without changing the raw address used for /29 diversity accounting.
func TestOperatorSimulationSiteSettingsLocateEveryLoopbackPeerInUS(t *testing.T) {
	encoded, err := yaml.Marshal(operatorSimulationSiteSettings())
	if err != nil {
		t.Fatal(err)
	}
	var settings struct {
		EnvVars     map[string]string `yaml:"env_vars"`
		IPOverrides []struct {
			Subnet      string  `yaml:"subnet"`
			CountryCode string  `yaml:"country_code"`
			Country     string  `yaml:"country"`
			Region      string  `yaml:"region"`
			City        string  `yaml:"city"`
			Latitude    float64 `yaml:"latitude"`
			Longitude   float64 `yaml:"longitude"`
			Timezone    string  `yaml:"timezone"`
			Hosting     bool    `yaml:"hosting"`
			Privacy     bool    `yaml:"privacy"`
			Virtual     bool    `yaml:"virtual"`
		} `yaml:"ip_overrides"`
	}
	if err := yaml.Unmarshal(encoded, &settings); err != nil {
		t.Fatal(err)
	}
	if settings.EnvVars["URNETWORK_ST_PROFILE"] != "testnet" || len(settings.IPOverrides) != 1 {
		t.Fatalf("simulation settings = %+v", settings)
	}
	override := settings.IPOverrides[0]
	prefix, err := netip.ParsePrefix(override.Subnet)
	if err != nil {
		t.Fatal(err)
	}
	if override.CountryCode != "us" || override.Country != "United States" || override.Region == "" || override.City == "" || override.Latitude == 0 || override.Longitude == 0 || override.Timezone == "" || override.Hosting || override.Privacy || override.Virtual {
		t.Fatalf("simulation location is not a complete clean US candidate: %+v", override)
	}
	for miner := 1; miner <= 1_000; miner++ {
		address, parseErr := netip.ParseAddr(minerTestEgressSourceIP(miner))
		if parseErr != nil || !prefix.Contains(address) {
			t.Fatalf("miner %d source %q is outside simulation override %s: %v", miner, address, prefix, parseErr)
		}
	}
	if !prefix.Contains(netip.MustParseAddr("127.0.0.1")) {
		t.Fatal("validator and internal API loopback peer is outside simulation override")
	}
}

func TestRenderOperatorBlobConfigUsesConfiguredArtifactPrefix(t *testing.T) {
	cfg := testResolvedConfig(t)
	cfg.Config.Artifacts.MinioPrefix = "blob/sim-testnet-light/${deployment_id}"
	vault := t.TempDir()
	if err := os.MkdirAll(filepath.Join(vault, "main"), 0o700); err != nil {
		t.Fatal(err)
	}
	source := []byte("authority: minio.example:23900\ntls: true\nbucket: blob\naccess_key: test-access\nsecret_key: test-secret\n")
	if err := atomicWrite(filepath.Join(vault, "main", "minio.yml"), source, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg.Repos.Vault = vault
	destination := filepath.Join(t.TempDir(), "minio.yml")
	if err := renderOperatorBlobConfig(cfg, 2, destination); err != nil {
		t.Fatal(err)
	}
	var rendered renderedOperatorBlobConfig
	if err := strictYAML(destination, &rendered); err != nil {
		t.Fatal(err)
	}
	want, err := operatorArtifactPrefix(cfg.Config, 2)
	if err != nil {
		t.Fatal(err)
	}
	if rendered.Prefix != want || rendered.Authority != "minio.example:23900" || !rendered.TLS || rendered.Bucket != "blob" {
		t.Fatalf("rendered light artifact store = %+v, want prefix %q", rendered, want)
	}
}

// Private mode follows block cadence within the supported range; public mode
// reserves the remaining shared request budget for settlement and claims.
func TestWorkloadPollSecondsFitOperationalRPCMode(t *testing.T) {
	cfg := testResolvedConfig(t)
	cfg.Public.Chain.ExpectedBlockSeconds = 12
	if got := validatorPollSeconds(cfg); got != 15 {
		t.Fatalf("12-second chain poll = %d, want 15", got)
	}
	cfg.Public.Chain.ExpectedBlockSeconds = 30
	if got := validatorPollSeconds(cfg); got != 30 {
		t.Fatalf("30-second chain poll = %d, want 30", got)
	}
	cfg.Public.Chain.ExpectedBlockSeconds = 90
	if got := validatorPollSeconds(cfg); got != 60 {
		t.Fatalf("long chain poll = %d, want supported maximum 60", got)
	}
	if got := claimPollSeconds(cfg); got != 10 {
		t.Fatalf("private claim poll = %d, want 10", got)
	}
	cfg.OperationalRPCMode = rpcModePublicOverride
	if got := validatorPollSeconds(cfg); got != 60 {
		t.Fatalf("public validator poll = %d, want 60", got)
	}
	if got := claimPollSeconds(cfg); got != 60 {
		t.Fatalf("public claim poll = %d, want 60", got)
	}
}

func TestRenderRuntimeConfigsAreAcceptedByReleaseLoaders(t *testing.T) {
	cfg := testResolvedConfig(t)
	cfg.Authority = "http://127.0.0.1:9944"
	cfg.OperationalRPCMode = rpcModePublicOverride
	stateDir := t.TempDir()
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for label, client := range roles.Clients {
		client.ClientIDHex = strings.Repeat("01", 16)
		roles.Clients[label] = client
	}
	for i := 1; i <= cfg.Config.Topology.Miners; i++ {
		state := filepath.Join(stateDir, "runtime", "miner-"+strconv.Itoa(i), "state")
		for _, fixture := range []struct{ name, contents string }{
			{name: "jwt", contents: "fixture-network-jwt\n"},
			{name: ".provider.jwt", contents: "fixture-provider-jwt\n"},
			{name: ".provider.key", contents: strings.Repeat("01", 32) + "\n"},
		} {
			if err := atomicWrite(filepath.Join(state, fixture.name), []byte(fixture.contents), 0o600); err != nil {
				t.Fatal(err)
			}
		}
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
		if len(loaded.Operators) != cfg.Config.Topology.Operators || loaded.PolicyHash != cfg.PolicyHash || loaded.Policy.ProductionCadence.EpochBlocks != 360 || loaded.Policy.Settlement.CloseGraceBlocks != 5 || loaded.PollSeconds != validatorPollSeconds(cfg) {
			t.Fatalf("validator %d config incomplete: %+v", i, loaded)
		}
		if len(loaded.RPC) != 1 || loaded.RPC[0] != "http://"+workloadRPCAuthority() || len(loaded.Substrate) != 1 || loaded.Substrate[0] != "ws://"+workloadSubstrateRPCAuthority() {
			t.Fatalf("validator %d bypasses the simulator-owned RPC proxy: rpc=%v substrate=%v", i, loaded.RPC, loaded.Substrate)
		}
		wantControlled := controlledNOIDsForValidator(i)
		if len(loaded.ControlledNOIDs) != len(wantControlled) || (len(wantControlled) != 0 && loaded.ControlledNOIDs[0] != wantControlled[0]) {
			t.Fatalf("validator %d controlled NOs = %v, want %v", i, loaded.ControlledNOIDs, wantControlled)
		}
		for _, operator := range loaded.Operators {
			wantConnectURL := "ws://" + operatorConnectHostIP(int(operator.NoID)) + ":" + strconv.Itoa(19080+int(operator.NoID))
			if operator.ConnectURL != wantConnectURL {
				t.Fatalf("validator %d operator %d connect URL = %q, want %q", i, operator.NoID, operator.ConnectURL, wantConnectURL)
			}
		}
	}
	for swarm := 1; swarm <= cfg.Config.Topology.MinerSwarmProcesses; swarm++ {
		path := filepath.Join(stateDir, "runtime", "miner-swarm-"+strconv.Itoa(swarm), "swarm.json")
		loaded, err := minerpkg.LoadProviderSwarmConfig(path)
		if err != nil {
			t.Fatalf("provider swarm %d rendered config: %v", swarm, err)
		}
		for _, member := range loaded.Members {
			miner, parseErr := strconv.Atoi(strings.TrimPrefix(member.ID, "miner-"))
			if parseErr != nil || miner < 1 || miner > cfg.Config.Topology.Miners {
				t.Fatalf("provider swarm %d member id = %q", swarm, member.ID)
			}
			operator := operatorForMiner(cfg, miner)
			wantConnectURL := "ws://" + operatorConnectHostIP(operator) + ":" + strconv.Itoa(19080+operator)
			if member.ConnectURL != wantConnectURL {
				t.Fatalf("provider %s connect URL = %q, want %q", member.ID, member.ConnectURL, wantConnectURL)
			}
			if member.DNSPumpHost != operatorConnectHostIP(operator) {
				t.Fatalf("provider %s DNS pump host = %q, want provisioned ingress %q", member.ID, member.DNSPumpHost, operatorConnectHostIP(operator))
			}
		}
	}
	for i := 1; i <= cfg.Config.Topology.Miners; i++ {
		path := filepath.Join(stateDir, "runtime", "miner-"+strconv.Itoa(i), "claim-daemon.yml")
		loaded, err := minerpkg.LoadClaimDaemonConfig(path)
		if err != nil {
			t.Fatalf("miner %d claim daemon rendered config: %v", i, err)
		}
		if loaded.LookbackEpochs == 0 || len(loaded.RPC) != 1 || loaded.RPC[0] != "http://"+workloadRPCAuthority() || loaded.JWTFile == "" || loaded.PollSeconds != claimPollSeconds(cfg) {
			t.Fatalf("miner %d claim config incomplete: %+v", i, loaded)
		}
		operator := operatorForMiner(cfg, i)
		wantKey := filepath.Join(stateDir, "secrets", "operator-"+strconv.Itoa(operator)+"-claim-relayer.key")
		if loaded.KeyFile != wantKey {
			t.Fatalf("miner %d claim key = %q, want %q", i, loaded.KeyFile, wantKey)
		}
		keyBytes, readErr := os.ReadFile(wantKey)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if strings.TrimSpace(string(keyBytes)) != "0x"+roles.EVM["operator-"+strconv.Itoa(operator)+"-claim-relayer"].PrivateKeyHex {
			t.Fatalf("miner %d does not use its operator-scoped claim relayer", i)
		}
	}
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		path := filepath.Join(stateDir, "runtime", "claim-relayer-"+strconv.Itoa(operator), "swarm.json")
		loaded, err := minerpkg.LoadClaimSwarmConfig(path)
		if err != nil {
			t.Fatalf("operator %d claim swarm rendered config: %v", operator, err)
		}
		wantMembers := 0
		for miner := 1; miner <= cfg.Config.Topology.Miners; miner++ {
			if operatorForMiner(cfg, miner) == operator {
				wantMembers++
			}
		}
		if len(loaded.Members) != wantMembers || loaded.ListenAddress != "127.0.0.1:"+strconv.Itoa(22080+operator) {
			t.Fatalf("operator %d claim swarm = %+v, want %d members", operator, loaded, wantMembers)
		}
	}
	stPath := filepath.Join(stateDir, "runtime", "operator-1", "vault", "st.yml")
	b, err := os.ReadFile(stPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	if !strings.Contains(text, "testnet-coordinator-address") || !strings.Contains(text, "testnet-deposit-tiers") || !strings.Contains(text, "rate_numerator_rao_per_gib") || !strings.Contains(text, workloadRPCAuthority()) || strings.Contains(text, cfg.Authority) || strings.Contains(text, "\ndeposit_key:") || strings.Contains(text, "\nops_key:") {
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
	stateDir := t.TempDir()
	env := operatorBaseEnv(cfg, stateDir, 1, "127.0.0.11")
	want := operatorConfigHome(stateDir, 1)
	if env["WARP_CONFIG_HOME"] != want {
		t.Fatalf("WARP_CONFIG_HOME = %q, want %q", env["WARP_CONFIG_HOME"], want)
	}
	if env["WARP_ENV"] != operatorEnvironment(1) || env["WARP_ENV"] == "local" {
		t.Fatalf("operator environment lost its isolated non-local identity: %q", env["WARP_ENV"])
	}
	if env["URNETWORK_ST_PROFILE"] != "testnet" || env["URNETWORK_SIM_TESTNET"] != "1" {
		t.Fatalf("operator environment lost its explicit simulator scope: %+v", env)
	}
	if env["BRINGYOUR_SUBTENSOR_HOSTNAME"] != workloadRPCAuthority() {
		t.Fatalf("operator bypasses workload RPC proxy: %q", env["BRINGYOUR_SUBTENSOR_HOSTNAME"])
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
