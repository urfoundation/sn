package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestSupervisorChild(t *testing.T) {
	if os.Getenv("SIM_TEST_CHILD") != "1" {
		return
	}
	counter := os.Getenv("SIM_TEST_COUNTER")
	if _, err := os.Stat(counter); os.IsNotExist(err) {
		_ = os.WriteFile(counter, []byte("crashed once\n"), 0o600)
		os.Exit(17)
	}
	for {
		time.Sleep(time.Second)
	}
}

func TestSupervisorRestartsOncePublishesReadyAndStopsChildren(t *testing.T) {
	dir := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binaryHash, err := fileSHA256(executable)
	if err != nil {
		t.Fatal(err)
	}
	counter := filepath.Join(dir, "child.counter")
	spec := ProcessSpec{
		ID: "restart-child", Role: "test", Identity: "test-child", Command: executable,
		Args: []string{"-test.run=TestSupervisorChild"}, WorkDir: dir,
		Env:        map[string]string{"SIM_TEST_CHILD": "1", "SIM_TEST_COUNTER": counter},
		StdoutPath: filepath.Join(dir, "child.stdout"), StderrPath: filepath.Join(dir, "child.stderr"), RestartLimit: 2,
	}
	manifest := SupervisorFile{Schema: "urnetwork-sim-supervisor-v1", DeploymentID: "test", BinaryHash: binaryHash, Specs: []ProcessSpec{spec}}
	b, _ := json.Marshal(manifest)
	manifestPath := filepath.Join(dir, "supervisor.json")
	if err := os.WriteFile(manifestPath, b, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- supervise(ctx, dir, manifestPath) }()
	if err := waitSupervisorReady(ctx, dir, manifest, 12*time.Second); err != nil {
		cancel()
		<-done
		t.Fatal(err)
	}
	ready, err := supervisorReadyNow(dir, manifest)
	if err != nil || !ready {
		t.Fatalf("supervisor ready = %v, %v", ready, err)
	}
	stateBytes, err := os.ReadFile(filepath.Join(dir, "supervisor.state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state SupervisorState
	if err := json.Unmarshal(stateBytes, &state); err != nil {
		t.Fatal(err)
	}
	if len(state.Processes) != 1 || state.Processes[0].Restarts != 1 || state.Processes[0].PID <= 1 {
		t.Fatalf("unexpected restart state: %+v", state)
	}
	childPID := state.Processes[0].PID
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && syscall.Kill(childPID, syscall.Signal(0)) == nil {
		time.Sleep(50 * time.Millisecond)
	}
	if syscall.Kill(childPID, syscall.Signal(0)) == nil {
		t.Fatalf("child process %d survived supervisor cancellation", childPID)
	}
}

func TestSupervisorServiceNamesAndArgumentsAreSafe(t *testing.T) {
	if got := serviceToken("Testnet / Release_1.0"); got != "testnet---release-1-0" {
		t.Fatalf("service token = %q", got)
	}
	if _, err := systemdQuote("bad\npath"); err == nil {
		t.Fatal("systemd argument accepted a newline")
	}
	if got, err := systemdQuote("/tmp/path with spaces"); err != nil || got != `"/tmp/path with spaces"` {
		t.Fatalf("quoted argument = %q, %v", got, err)
	}
}

func TestPersistentSupervisorRequiresExplicitResumeAfterReboot(t *testing.T) {
	cfg := testResolvedConfig(t)
	cfg.ConfigPath = "/release/testnet.yml"
	unit, err := persistentSupervisorUnit(cfg, "/release/sim-testnet", "/release/state", "/release/supervisor.json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(unit, "[Install]") || strings.Contains(unit, "WantedBy=") {
		t.Fatalf("supervisor unit is boot-installable:\n%s", unit)
	}
	actions := persistentSupervisorSystemctlActions("urnetwork-sim-test.service")
	joined := ""
	for _, action := range actions {
		joined += strings.Join(action, " ") + "\n"
	}
	if strings.Contains(joined, "enable") || strings.Contains(joined, "--now") || !strings.Contains(joined, "disable") || !strings.Contains(joined, "start") {
		t.Fatalf("supervisor actions can persist across reboot:\n%s", joined)
	}
}

func TestReleaseHostPreflightRejectsFullDiskAndOccupiedPort(t *testing.T) {
	if err := validateReleaseStateFreeBytes(minimumReleaseStateFreeBytes); err != nil {
		t.Fatal(err)
	}
	if err := validateReleaseStateFreeBytes(minimumReleaseStateFreeBytes - 1); err == nil {
		t.Fatal("undersized release filesystem was accepted")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := validateAvailableListenAddresses([]string{address}); err == nil || !strings.Contains(err.Error(), address) {
		t.Fatalf("occupied simulator listener was accepted: %v", err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	if err := validateAvailableListenAddresses([]string{address}); err != nil {
		t.Fatal(err)
	}
}

func TestReleaseProcessListenAddressesCoverTopologyWithoutDuplicates(t *testing.T) {
	cfg := testResolvedConfig(t)
	addresses := releaseProcessListenAddresses(cfg)
	want := 4 + 4*cfg.Config.Topology.Operators + cfg.Config.Topology.MinerSwarmProcesses
	if len(addresses) != want {
		t.Fatalf("release listener count=%d, want %d: %v", len(addresses), want, addresses)
	}
	seen := map[string]bool{}
	for _, address := range addresses {
		if seen[address] {
			t.Fatalf("duplicate release listener %s", address)
		}
		seen[address] = true
		host, _, err := net.SplitHostPort(address)
		if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
			t.Fatalf("release listener is not loopback-confined: %q (%v)", address, err)
		}
	}
}

func TestRestartBackoffIsBounded(t *testing.T) {
	if restartBackoff(1) != time.Second || restartBackoff(100) != 30*time.Second {
		t.Fatalf("unexpected restart backoff: %s, %s", restartBackoff(1), restartBackoff(100))
	}
}

func TestProvisioningStartsOnlyOperatorAPIs(t *testing.T) {
	specs := []ProcessSpec{
		{ID: workloadRPCProxyProcessID, Role: "dependency-rpc-proxy"},
		{ID: workloadSubstrateProcessID, Role: "dependency-rpc-proxy"},
		{ID: "operator-1-api", Role: "operator-api"},
		{ID: "operator-1-connect", Role: "operator-connect"},
		{ID: "operator-1-taskworker", Role: "operator-taskworker"},
		{ID: "operator-2-api", Role: "operator-api"},
	}
	got := selectProvisioningServerSpecs(specs)
	if len(got) != 4 || got[0].ID != workloadRPCProxyProcessID || got[1].ID != workloadSubstrateProcessID || got[2].ID != "operator-1-api" || got[3].ID != "operator-2-api" {
		t.Fatalf("provisioning specs = %+v", got)
	}
}

func TestPostTopologyTournamentSelectionIsExactAndBounded(t *testing.T) {
	plan := &SetupPlan{Actions: []Action{
		{ID: "accounts.provision"},
		{ID: "topology.launch"},
		{ID: "fleet.register.201"},
		{ID: "fleet.commitment.201"},
		{ID: "fleet.mirror.201"},
		{ID: "fleet.bind.201.1"},
		{ID: "churn.tournament-complete"},
		{ID: "precompile.commitment-write"},
	}}
	actions, err := postTopologyTournamentActions(plan)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"fleet.register.201", "fleet.commitment.201", "fleet.mirror.201", "fleet.bind.201.1", "churn.tournament-complete"}
	if len(actions) != len(want) {
		t.Fatalf("selected %d tournament actions, want %d: %+v", len(actions), len(want), actions)
	}
	for i := range want {
		if actions[i].ID != want[i] {
			t.Fatalf("selected action %d=%s, want %s", i, actions[i].ID, want[i])
		}
	}
}

func TestPostTopologyTournamentSelectionRejectsMalformedBoundaries(t *testing.T) {
	tests := []struct {
		name    string
		actions []Action
		want    string
	}{
		{name: "nil plan", want: "unavailable"},
		{name: "missing topology", actions: []Action{{ID: "churn.tournament-complete"}}, want: "topology.launch"},
		{name: "missing barrier", actions: []Action{{ID: "topology.launch"}, {ID: "fleet.register.201"}}, want: "churn.tournament-complete"},
		{name: "unexpected action", actions: []Action{{ID: "topology.launch"}, {ID: "precompile.commitment-write"}, {ID: "churn.tournament-complete"}}, want: "unexpected post-topology action"},
	}
	for _, test := range tests {
		var plan *SetupPlan
		if test.actions != nil {
			plan = &SetupPlan{Actions: test.actions}
		}
		_, err := postTopologyTournamentActions(plan)
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatalf("%s: error=%v, want substring %q", test.name, err, test.want)
		}
	}
}

func TestServerSpecsRouteWorkloadsThroughSimulatorOwnedRPCProxy(t *testing.T) {
	cfg := testResolvedConfig(t)
	cfg.OperationalEVM = "https://evm-rpc.example"
	cfg.Public.Chain.EVMPublicReadEndpoint = cfg.OperationalEVM
	cfg.OperationalRPCMode = rpcModePublicOverride
	cfg.OperationalSubstrate = "wss://substrate-rpc.example:443"
	specs, err := buildServerSpecs(cfg, t.TempDir(), map[string]string{"sim-testnet": "/release/sim-testnet"})
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) < 2 || specs[0].ID != workloadRPCProxyProcessID || specs[0].HealthURL != "http://"+workloadRPCProxyHealthAddress+"/healthz" || specs[1].ID != workloadSubstrateProcessID || specs[1].HealthURL != "http://"+workloadSubstrateHealthAddress+"/healthz" {
		t.Fatalf("missing workload RPC proxy spec: %+v", specs)
	}
	evmArgs := strings.Join(specs[0].Args, " ")
	substrateArgs := strings.Join(specs[1].Args, " ")
	if !strings.Contains(evmArgs, "--upstream=evm-rpc.example:443") || !strings.Contains(evmArgs, "--tls-server-name=evm-rpc.example") || !strings.Contains(evmArgs, "--http") || !strings.Contains(evmArgs, "--maximum-requests-per-minute=40") || !strings.Contains(substrateArgs, "--upstream=substrate-rpc.example:443") || !strings.Contains(substrateArgs, "--tls-server-name=substrate-rpc.example") || strings.Contains(substrateArgs, "--http") || strings.Contains(substrateArgs, "--maximum-requests-per-minute") {
		t.Fatalf("RPC proxies lost protocol-specific upstream TLS identities: evm=%q substrate=%q", evmArgs, substrateArgs)
	}
	for _, spec := range specs[2:] {
		if spec.Env["BRINGYOUR_SUBTENSOR_HOSTNAME"] != workloadRPCAuthority() {
			t.Fatalf("%s bypasses workload RPC proxy: %q", spec.ID, spec.Env["BRINGYOUR_SUBTENSOR_HOSTNAME"])
		}
		switch spec.Role {
		case "operator-api":
			if spec.Command != "/release/sim-testnet" || len(spec.Args) != 2 || spec.Args[0] != "__server_api" {
				t.Fatalf("%s bypasses the production API module runner: %+v", spec.ID, spec)
			}
		case "operator-connect":
			if spec.Command != "/release/sim-testnet" || len(spec.Args) != 2 || spec.Args[0] != "__server_connect" {
				t.Fatalf("%s bypasses the production connect module runner: %+v", spec.ID, spec)
			}
		case "operator-taskworker":
			if spec.Command != "/release/sim-testnet" || len(spec.Args) != 2 || spec.Args[0] != "__server_taskworker" {
				t.Fatalf("%s bypasses the production taskworker module runner: %+v", spec.ID, spec)
			}
		}
	}
}

func TestClientSpecsUseProductionModuleSwarmsAndDistinctPrefixes(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	specs := buildClientSpecs(cfg, t.TempDir(), map[string]string{"sim-testnet": "/bin/sim-testnet"}, roles)
	providerSwarms := 0
	claimSwarms := 0
	for _, spec := range specs {
		switch spec.Role {
		case "miner-swarm":
			providerSwarms++
			if spec.Command != "/bin/sim-testnet" || len(spec.Args) != 2 || spec.Args[0] != "__miner_swarm" {
				t.Fatalf("provider swarm %s bypasses the production miner module wrapper: %+v", spec.ID, spec)
			}
		case "claim-relayer":
			claimSwarms++
			if spec.Command != "/bin/sim-testnet" || len(spec.Args) != 2 || spec.Args[0] != "__claim_swarm" {
				t.Fatalf("claim swarm %s bypasses the production miner module wrapper: %+v", spec.ID, spec)
			}
		}
	}
	if providerSwarms != cfg.Config.Topology.MinerSwarmProcesses || claimSwarms != cfg.Config.Topology.Operators {
		t.Fatalf("provider/claim swarm count = %d/%d, want %d/%d", providerSwarms, claimSwarms, cfg.Config.Topology.MinerSwarmProcesses, cfg.Config.Topology.Operators)
	}
	sources := map[string]bool{}
	prefixes := map[netip.Prefix][]int{}
	for miner := 1; miner <= cfg.Config.Topology.Miners; miner++ {
		source := minerTestEgressSourceIP(miner)
		addr, err := netip.ParseAddr(source)
		if err != nil || !addr.Is4() || !addr.IsLoopback() || sources[source] {
			t.Fatalf("miner-%d source identity = %q, err=%v duplicate=%t", miner, source, err, sources[source])
		}
		sources[source] = true
		prefix := netip.PrefixFrom(addr, cfg.Policy.Verify.EgressIPv4Prefix).Masked()
		prefixes[prefix] = append(prefixes[prefix], miner)
	}
	wantPrefixes := cfg.Config.Topology.Miners - 7
	if len(sources) != cfg.Config.Topology.Miners || len(prefixes) != wantPrefixes {
		t.Fatalf("source/prefix count = %d/%d, want %d/%d", len(sources), len(prefixes), cfg.Config.Topology.Miners, wantPrefixes)
	}
	multi := map[string]bool{"1,5": true, "801,802,803,804": true, "805,806,807,808": true}
	for prefix, miners := range prefixes {
		if len(miners) == 1 {
			continue
		}
		parts := make([]string, len(miners))
		for index, miner := range miners {
			parts[index] = fmt.Sprint(miner)
		}
		key := strings.Join(parts, ",")
		if !multi[key] {
			t.Fatalf("unexpected shared prefix %s miners=%v", prefix, miners)
		}
		delete(multi, key)
	}
	if len(multi) != 0 {
		t.Fatalf("expected shared-prefix cohorts were absent: %v", multi)
	}
}

func TestBalancedOperatorAssignmentKeepsHeadFleetsIsolated(t *testing.T) {
	cfg := testResolvedConfig(t)
	want := map[int]int{1: 1, 4: 1, 5: 2, 8: 2, 801: 1, 804: 1, 805: 2, 808: 2, 809: 1, 810: 2, 1_000: 2}
	for miner, operator := range want {
		if got := operatorForMiner(cfg, miner); got != operator {
			t.Fatalf("miner %d operator = %d, want %d", miner, got, operator)
		}
	}
	if operatorForMiner(cfg, 0) != 0 || operatorForMiner(cfg, cfg.Config.Topology.Miners+1) != 0 {
		t.Fatal("invalid miner index was assigned to a live operator")
	}
}

func TestTestEgressGeometryKeepsSharedFleetsAboveChallengers(t *testing.T) {
	cfg := testResolvedConfig(t)
	fleetPrefixes := map[int]map[netip.Prefix]bool{}
	claims := map[netip.Prefix]int{}
	for fleet := 1; fleet <= cfg.Config.Topology.fleetCandidates(); fleet++ {
		fleetPrefixes[fleet] = map[netip.Prefix]bool{}
		for member := 1; member <= cfg.Config.Topology.ClientsPerHeadFleet; member++ {
			miner := fleetMemberMinerIndex(cfg, fleet, member)
			address, err := netip.ParseAddr(minerTestEgressSourceIP(miner))
			if err != nil {
				t.Fatal(err)
			}
			fleetPrefixes[fleet][netip.PrefixFrom(address, cfg.Policy.Verify.EgressIPv4Prefix).Masked()] = true
		}
		for prefix := range fleetPrefixes[fleet] {
			claims[prefix]++
		}
	}
	type rankedFleet struct {
		fleet int
		score *big.Rat
	}
	ranked := make([]rankedFleet, 0, len(fleetPrefixes))
	for fleet, prefixes := range fleetPrefixes {
		score := new(big.Rat)
		for prefix := range prefixes {
			score.Add(score, new(big.Rat).SetFrac64(1, int64(claims[prefix])))
		}
		ranked = append(ranked, rankedFleet{fleet: fleet, score: score})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if comparison := ranked[i].score.Cmp(ranked[j].score); comparison != 0 {
			return comparison > 0
		}
		return ranked[i].fleet < ranked[j].fleet
	})
	selected := map[int]bool{}
	for _, fleet := range ranked[:cfg.Config.Topology.HeadSlots] {
		selected[fleet.fleet] = true
	}
	for fleet := 1; fleet <= cfg.Config.Topology.HeadFleets; fleet++ {
		if !selected[fleet] {
			t.Fatalf("initial fleet %d fell below the top-200 boundary", fleet)
		}
	}
	if ranked[0].score.Cmp(big.NewRat(4, 1)) != 0 || ranked[198].score.Cmp(big.NewRat(7, 2)) != 0 || ranked[199].score.Cmp(big.NewRat(7, 2)) != 0 || ranked[200].fleet != 201 || ranked[200].score.Cmp(big.NewRat(1, 1)) != 0 || ranked[201].fleet != 202 {
		t.Fatalf("unexpected boundary geometry: top=%+v boundary=%+v", ranked[0], ranked[198:])
	}
}

func TestManagedDependencySpecsMirrorServerLocalAndIsolateOperators(t *testing.T) {
	cfg := testResolvedConfig(t)
	cfg.Release = &ReleaseLock{Dependencies: map[string]string{
		"postgres": "postgres:18@sha256:" + strings.Repeat("1", 64),
		"redis":    "redis:8-alpine@sha256:" + strings.Repeat("2", 64),
	}}
	specs, err := dependencyContainerSpecs(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 4 {
		t.Fatalf("dependency spec count = %d, want 4", len(specs))
	}
	postgresOne := specs[0]
	redisOne := specs[1]
	postgresTwo := specs[2]
	redisTwo := specs[3]
	if postgresOne.Name != "unit-test-deployment-pg-1" || postgresTwo.Name != "unit-test-deployment-pg-2" || redisOne.Name != "unit-test-deployment-redis-1" || redisTwo.Name != "unit-test-deployment-redis-2" {
		t.Fatalf("dependency names are not isolated: %+v", []string{postgresOne.Name, redisOne.Name, postgresTwo.Name, redisTwo.Name})
	}
	postgresArgs := strings.Join(postgresOne.RunArgs, " ")
	if !strings.Contains(postgresArgs, "127.0.0.11:5432:5432") || !strings.Contains(strings.Join(postgresTwo.RunArgs, " "), "127.0.0.12:5432:5432") {
		t.Fatalf("PostgreSQL operator bindings are not isolated: %q / %q", postgresArgs, strings.Join(postgresTwo.RunArgs, " "))
	}
	if !strings.Contains(postgresArgs, "/server/local/postgres/initdb") || !strings.Contains(postgresArgs, "POSTGRES_INITDB_ARGS=--locale=en_US.UTF-8") || !strings.Contains(postgresArgs, "APP_DB_USER=bringyour") || !strings.Contains(postgresArgs, "APP_DB_NAME=bringyour") {
		t.Fatalf("PostgreSQL spec drifted from server/local: %q", postgresArgs)
	}
	if strings.Join(postgresOne.Command, " ") != "postgres -c max_connections=512 -c shared_buffers=256MB" {
		t.Fatalf("PostgreSQL server settings drifted: %q", postgresOne.Command)
	}
	if len(postgresOne.DataVolumes) != 1 || postgresOne.DataVolumes[0] != "unit-test-deployment-pg-1-data" || postgresOne.ReadyExpected != "512:256MB:en_US.UTF-8" {
		t.Fatalf("PostgreSQL durable/readiness contract drifted: volumes=%v expected=%q", postgresOne.DataVolumes, postgresOne.ReadyExpected)
	}
	if postgresOne.RestartPolicy != "no" || redisOne.RestartPolicy != "no" || postgresTwo.RestartPolicy != "no" || redisTwo.RestartPolicy != "no" {
		t.Fatalf("managed dependencies can restart after reboot: %q/%q/%q/%q", postgresOne.RestartPolicy, redisOne.RestartPolicy, postgresTwo.RestartPolicy, redisTwo.RestartPolicy)
	}
	postgresProbe := strings.Join(postgresOne.ReadyProbe, " ")
	if !strings.Contains(postgresProbe, "PGPASSWORD=") || !strings.Contains(postgresProbe, "psql -h 127.0.0.1 -U bringyour -d bringyour") || !strings.Contains(postgresProbe, "max_connections") {
		t.Fatalf("PostgreSQL readiness is not an authenticated settings query: %q", postgresProbe)
	}
	redisArgs := strings.Join(redisOne.RunArgs, " ")
	redisCommand := strings.Join(redisOne.Command, " ")
	if !strings.Contains(redisArgs, "127.0.0.11:6379:6379") || !strings.Contains(strings.Join(redisTwo.RunArgs, " "), "127.0.0.12:6379:6379") || !strings.Contains(redisArgs, "nofile=65536:65536") {
		t.Fatalf("Redis operator bindings/settings are not isolated: %q / %q", redisArgs, strings.Join(redisTwo.RunArgs, " "))
	}
	for _, required := range []string{"--io-threads 8", "--maxclients 32768", "--tcp-backlog 65535", "--save  --appendonly no"} {
		if !strings.Contains(redisCommand, required) {
			t.Fatalf("Redis command %q lacks server/local setting %q", redisCommand, required)
		}
	}
	if redisOne.ReadyExpected != "PONG" || len(redisOne.DataVolumes) != 0 {
		t.Fatalf("Redis readiness/durability contract drifted: expected=%q volumes=%v", redisOne.ReadyExpected, redisOne.DataVolumes)
	}
}

func TestMutationPreflightRunsBeforeWriteCapableExecution(t *testing.T) {
	for command, want := range map[string]struct {
		dependencies bool
		binaries     bool
	}{
		"setup":    {},
		"launch":   {dependencies: true, binaries: true},
		"resume":   {dependencies: true, binaries: true},
		"scenario": {dependencies: true},
	} {
		if got := requiresManagedDependencies(command); got != want.dependencies {
			t.Fatalf("%s managed-dependency preflight = %t, want %t", command, got, want.dependencies)
		}
		if got := requiresReleaseBinaries(command); got != want.binaries {
			t.Fatalf("%s binary preflight = %t, want %t", command, got, want.binaries)
		}
	}
}

func TestManagedContainerSpecHashCoversCreationSettings(t *testing.T) {
	spec := managedContainerSpec{
		Name:              "one",
		Image:             "image@sha256:" + strings.Repeat("a", 64),
		ConfigurationHash: "sha256:" + strings.Repeat("b", 64),
		RestartPolicy:     "no",
		RunArgs:           []string{"-p", "127.0.0.11:5432:5432"},
		Command:           []string{"server", "--limit", "1"},
		DataVolumes:       []string{"one-data"},
		ReadyProbe:        []string{"ready"},
		ReadyExpected:     "ready",
		ReadyTimeout:      time.Second,
	}
	first, err := managedContainerSpecHash(spec)
	if err != nil {
		t.Fatal(err)
	}
	second, err := managedContainerSpecHash(spec)
	if err != nil || first != second {
		t.Fatalf("container spec hash is not deterministic: %q %q %v", first, second, err)
	}
	changed := spec
	changed.Command = append([]string(nil), spec.Command...)
	changed.Command[len(changed.Command)-1] = "2"
	third, err := managedContainerSpecHash(changed)
	if err != nil {
		t.Fatal(err)
	}
	if third == first {
		t.Fatal("container creation command drift did not change the spec hash")
	}
	changed = spec
	changed.DataVolumes = []string{"stale-data"}
	third, err = managedContainerSpecHash(changed)
	if err != nil {
		t.Fatal(err)
	}
	if third == first {
		t.Fatal("container data-volume drift did not change the spec hash")
	}
	changed = spec
	changed.ConfigurationHash = "sha256:" + strings.Repeat("c", 64)
	third, err = managedContainerSpecHash(changed)
	if err != nil {
		t.Fatal(err)
	}
	if third == first {
		t.Fatal("server/local configuration drift did not change the spec hash")
	}
	changed = spec
	changed.RestartPolicy = "unless-stopped"
	third, err = managedContainerSpecHash(changed)
	if err != nil {
		t.Fatal(err)
	}
	if third == first {
		t.Fatal("container restart-policy drift did not change the spec hash")
	}
	dataFirst, err := managedContainerDataSpecHash(spec)
	if err != nil {
		t.Fatal(err)
	}
	dataThird, err := managedContainerDataSpecHash(changed)
	if err != nil {
		t.Fatal(err)
	}
	if dataThird != dataFirst {
		t.Fatal("container-only restart policy invalidated compatible persistent data")
	}
	changed.ConfigurationHash = "sha256:" + strings.Repeat("c", 64)
	dataThird, err = managedContainerDataSpecHash(changed)
	if err != nil {
		t.Fatal(err)
	}
	if dataThird == dataFirst {
		t.Fatal("data-affecting configuration drift preserved the volume hash")
	}
}

func TestManagedContainerReadinessRequiresExactSemanticOutput(t *testing.T) {
	docker := filepath.Join(t.TempDir(), "docker")
	if err := os.WriteFile(docker, []byte("#!/bin/sh\nprintf 'PONG\\n'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	spec := managedContainerSpec{Name: "redis", ReadyProbe: []string{"redis-cli", "ping"}, ReadyExpected: "PONG", ReadyTimeout: time.Second}
	cli := dockerCLI{Executable: docker}
	if err := waitContainerReady(context.Background(), cli, spec); err != nil {
		t.Fatal(err)
	}
	spec.ReadyExpected = "AUTHENTICATED"
	spec.ReadyTimeout = 20 * time.Millisecond
	if err := waitContainerReady(context.Background(), cli, spec); err == nil || !strings.Contains(err.Error(), "want \"AUTHENTICATED\"") {
		t.Fatalf("incorrect readiness output was accepted: %v", err)
	}
}

func TestDockerResolutionUsesOnlyPasswordlessSudoFallback(t *testing.T) {
	bin := t.TempDir()
	docker := filepath.Join(bin, "docker")
	sudo := filepath.Join(bin, "sudo")
	if err := os.WriteFile(docker, []byte("#!/bin/sh\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sudo, []byte("#!/bin/sh\nprintf '29.7.2\\n'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	cli, err := resolveDockerCLI(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if cli.Executable != sudo || len(cli.Prefix) != 2 || cli.Prefix[0] != "-n" || cli.Prefix[1] != docker || cli.ServerVersion != "29.7.2" {
		t.Fatalf("Docker sudo fallback = %+v", cli)
	}
}

func TestLiveManagedDependenciesMirrorServerLocal(t *testing.T) {
	if os.Getenv("SIM_TESTNET_LIVE_DEPENDENCIES") != "1" {
		t.Skip("set SIM_TESTNET_LIVE_DEPENDENCIES=1 to create/check the digest-pinned local PG/Redis pairs")
	}
	cfg, err := LoadResolved(LoadOptions{ConfigPath: "testnet.yml", RequireSecrets: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := startDependencies(ctx, cfg); err != nil {
		t.Fatal(err)
	}
}
