package main

import (
	"context"
	"encoding/json"
	"net/netip"
	"os"
	"path/filepath"
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

func TestRestartBackoffIsBounded(t *testing.T) {
	if restartBackoff(1) != time.Second || restartBackoff(100) != 30*time.Second {
		t.Fatalf("unexpected restart backoff: %s, %s", restartBackoff(1), restartBackoff(100))
	}
}

func TestProvisioningStartsOnlyOperatorAPIs(t *testing.T) {
	specs := []ProcessSpec{
		{ID: "operator-1-api", Role: "operator-api"},
		{ID: "operator-1-connect", Role: "operator-connect"},
		{ID: "operator-1-taskworker", Role: "operator-taskworker"},
		{ID: "operator-2-api", Role: "operator-api"},
	}
	got := selectProvisioningServerSpecs(specs)
	if len(got) != 2 || got[0].ID != "operator-1-api" || got[1].ID != "operator-2-api" {
		t.Fatalf("provisioning specs = %+v", got)
	}
}

func TestClientSpecsGiveExactAddressesOneSharedHeadPrefix(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	specs := buildClientSpecs(cfg, t.TempDir(), map[string]string{"miner": "/bin/miner", "validator": "/bin/validator"}, roles)
	sources := map[string]bool{}
	var headPrefix netip.Prefix
	miners := 0
	for _, spec := range specs {
		if spec.Role != "miner" {
			continue
		}
		miners++
		var source string
		for _, arg := range spec.Args {
			if strings.HasPrefix(arg, "--test-egress-source-ip=") {
				source = strings.TrimPrefix(arg, "--test-egress-source-ip=")
			}
		}
		addr, err := netip.ParseAddr(source)
		if err != nil || !addr.Is4() || !addr.IsLoopback() || sources[source] {
			t.Fatalf("%s source identity = %q, err=%v duplicate=%t", spec.ID, source, err, sources[source])
		}
		sources[source] = true
		if miners <= cfg.Config.Topology.HeadFleets*cfg.Config.Topology.ClientsPerHeadFleet {
			prefix := netip.PrefixFrom(addr, cfg.Policy.Verify.EgressIPv4Prefix).Masked()
			if !headPrefix.IsValid() {
				headPrefix = prefix
			} else if prefix != headPrefix {
				t.Fatalf("head miner %s prefix = %s, want shared %s", spec.ID, prefix, headPrefix)
			}
		} else if netip.PrefixFrom(addr, cfg.Policy.Verify.EgressIPv4Prefix).Masked() == headPrefix {
			t.Fatalf("tail miner %s leaked into shared head prefix", spec.ID)
		}
	}
	if miners != cfg.Config.Topology.Miners || len(sources) != miners {
		t.Fatalf("miner/source count = %d/%d, want %d", miners, len(sources), cfg.Config.Topology.Miners)
	}
}

func TestBalancedOperatorAssignmentKeepsHeadFleetsIsolated(t *testing.T) {
	cfg := testResolvedConfig(t)
	want := []int{1, 1, 1, 2, 2, 2, 1, 2}
	for miner, operator := range want {
		if got := operatorForMiner(cfg, miner+1); got != operator {
			t.Fatalf("miner %d operator = %d, want %d", miner+1, got, operator)
		}
	}
	if operatorForMiner(cfg, 0) != 0 || operatorForMiner(cfg, cfg.Config.Topology.Miners+1) != 0 {
		t.Fatal("invalid miner index was assigned to a live operator")
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
