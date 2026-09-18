package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const importedModuleStdioProbeEnv = "URNETWORK_SIM_TESTNET_STDIO_PROBE"

func TestMain(m *testing.M) {
	if os.Getenv(importedModuleStdioProbeEnv) == "1" {
		if os.Stdout == os.Stderr || os.Stdout.Fd() == os.Stderr.Fd() {
			os.Exit(97)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestImportedProductionModulesKeepJSONStdoutSeparateFromDiagnostics(t *testing.T) {
	command := exec.Command(os.Args[0], "-test.run=^$")
	command.Env = append(os.Environ(), importedModuleStdioProbeEnv+"=1")
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("isolated imported-module stdio probe: %v; stdout=%q stderr=%q", err, stdout.String(), stderr.String())
	}
}

func TestParseCLIReleaseCommandsAndWriteGuardFlags(t *testing.T) {
	for _, command := range []string{
		"doctor", "release-lock", "plan", "setup", "launch", "resume", "status", "inspect",
		"analyze", "scenario", "tail", "stop", "retire",
	} {
		got, options, err := parseCLI([]string{command, "--format", "json"})
		if err != nil {
			t.Fatalf("parse %s: %v", command, err)
		}
		if got != command || options.Format != "json" {
			t.Fatalf("parse %s = command %q format %q", command, got, options.Format)
		}
	}

	command, options, err := parseCLI([]string{
		"launch", "--apply", "--plan-hash", "sha256:approved", "--detach",
		"--operator-proxy-repo", "/release/operator-proxy",
	})
	if err != nil {
		t.Fatal(err)
	}
	if command != "launch" || !options.Apply || !options.Detach || options.PlanHash != "sha256:approved" || options.OperatorProxyRepo != "/release/operator-proxy" {
		t.Fatalf("write flags parsed incorrectly: command=%q options=%+v", command, options)
	}
	command, options, err = parseCLI([]string{"analyze", "--manifest", "https://operator.example/manifest", "--run-id", "20260903T010203.000000000Z-production-soak"})
	if err != nil || command != "analyze" || options.RunID != "20260903T010203.000000000Z-production-soak" {
		t.Fatalf("exact public analyze flags = command %q options=%+v err=%v", command, options, err)
	}
	if _, _, err := parseCLI([]string{"analyze", "--manifest", "https://operator.example/manifest"}); err == nil || !strings.Contains(err.Error(), "--run-id") {
		t.Fatalf("public analyze without exact run id was accepted: %v", err)
	}
	if _, _, err := parseCLI([]string{"inspect", "--run-id", "release-run"}); err == nil || !strings.Contains(err.Error(), "only") {
		t.Fatalf("run id on a non-campaign command was accepted: %v", err)
	}
}

// Only observation commands can switch to the live campaign's shared EVM
// egress; planning and every mutation retain their canonical authorization.
func TestCampaignEgressCommandSelectionIsReadOnly(t *testing.T) {
	for _, command := range []string{"status", "inspect", "analyze"} {
		if !commandUsesCampaignEgress(command) {
			t.Fatalf("read command %s did not select campaign egress", command)
		}
	}
	for _, command := range []string{"doctor", "plan", "setup", "launch", "resume", "scenario", "tail", "stop", "retire"} {
		if commandUsesCampaignEgress(command) {
			t.Fatalf("non-read command %s selected campaign egress", command)
		}
	}
}

// A stopped supervisor is a canonical-read case, while a live generation
// must expose exactly one healthy egress and may never fall back around it.
func TestSupervisedCampaignEgressFailsClosedForIncompleteLiveState(t *testing.T) {
	stateDir := t.TempDir()
	stopped := SupervisorState{Schema: "urnetwork-sim-supervisor-state-v1", SupervisorPID: 2147483647, SupervisorStartTimeTicks: 1}
	if err := writePublicJSON(filepath.Join(stateDir, "supervisor.state.json"), stopped); err != nil {
		t.Fatal(err)
	}
	active, err := supervisedCampaignEgressActive(context.Background(), stateDir)
	if err != nil || active {
		t.Fatalf("stopped supervisor selection active=%t err=%v", active, err)
	}
	ticks, err := processStartTimeTicks(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	live := SupervisorState{Schema: "urnetwork-sim-supervisor-state-v1", SupervisorPID: os.Getpid(), SupervisorStartTimeTicks: ticks}
	if err := writePublicJSON(filepath.Join(stateDir, "supervisor.state.json"), live); err != nil {
		t.Fatal(err)
	}
	if active, err := supervisedCampaignEgressActive(context.Background(), stateDir); err == nil || active {
		t.Fatalf("incomplete live supervisor selection active=%t err=%v", active, err)
	}
}

// Owns a real loopback listener and state directory for one probe, without
// taking the canonical port from a live campaign or another test process.
func newCampaignEgressListenerFixture(t *testing.T) (string, net.Listener) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	ticks, err := processStartTimeTicks(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	stateDir := t.TempDir()
	state := SupervisorState{
		Schema: "urnetwork-sim-supervisor-state-v1", SupervisorPID: os.Getpid(), SupervisorStartTimeTicks: ticks,
		Processes: []ProcessState{{ID: publicEVMEgressProcessID, Role: "dependency-rpc-proxy", PID: os.Getpid(), Healthy: true}},
	}
	if err := writePublicJSON(filepath.Join(stateDir, "supervisor.state.json"), state); err != nil {
		t.Fatal(err)
	}
	return stateDir, listener
}

// A healthy state must probe the exact canonical route through this fixture's
// private listener; another healthy endpoint cannot satisfy the dial witness.
func TestSupervisedCampaignEgressRequiresExactHealthyListener(t *testing.T) {
	stateDir, listener := newCampaignEgressListenerFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dialCalls := 0
	dialContext := func(ctx context.Context, network, address string) (net.Conn, error) {
		dialCalls++
		if network != "tcp" || address != publicEVMEgressAddress {
			return nil, fmt.Errorf("unexpected campaign dial %s %s", network, address)
		}
		return (&net.Dialer{}).DialContext(ctx, network, listener.Addr().String())
	}
	active, err := supervisedCampaignEgressActiveWithDial(ctx, stateDir, dialContext)
	if err != nil || !active || dialCalls != 1 {
		t.Fatalf("healthy supervised egress active=%t dials=%d err=%v", active, dialCalls, err)
	}
}

// Two probes reach an explicit barrier together, then use their own real
// listeners. No process-wide endpoint override or fixed port is shared.
func TestSupervisedCampaignEgressIndependentConcurrentOwners(t *testing.T) {
	type probe struct {
		stateDir string
		listener net.Listener
	}
	probes := make([]probe, 2)
	for i := range probes {
		stateDir, listener := newCampaignEgressListenerFixture(t)
		probes[i] = probe{stateDir: stateDir, listener: listener}
	}
	if probes[0].listener.Addr().String() == probes[1].listener.Addr().String() {
		t.Fatal("independent listener owners have the same endpoint")
	}
	type result struct {
		owner     int
		active    bool
		dialCalls int
		err       error
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	entered := make(chan int, len(probes))
	release := make(chan struct{})
	results := make(chan result, len(probes))
	var workers sync.WaitGroup
	defer func() {
		cancel()
		workers.Wait()
	}()
	for i, fixture := range probes {
		workers.Add(1)
		go func() {
			defer workers.Done()
			dialCalls := 0
			dialContext := func(ctx context.Context, network, address string) (net.Conn, error) {
				dialCalls++
				if network != "tcp" || address != publicEVMEgressAddress {
					return nil, fmt.Errorf("owner %d received unexpected campaign dial %s %s", i, network, address)
				}
				entered <- i
				select {
				case <-release:
				case <-ctx.Done():
					return nil, ctx.Err()
				}
				return (&net.Dialer{}).DialContext(ctx, network, fixture.listener.Addr().String())
			}
			active, err := supervisedCampaignEgressActiveWithDial(ctx, fixture.stateDir, dialContext)
			results <- result{owner: i, active: active, dialCalls: dialCalls, err: err}
		}()
	}
	seen := make(map[int]bool, len(probes))
	for range probes {
		select {
		case owner := <-entered:
			if seen[owner] {
				t.Fatalf("owner %d reached the dial barrier twice", owner)
			}
			seen[owner] = true
		case result := <-results:
			t.Fatalf("owner %d completed without its concurrent dial witness: %+v", result.owner, result)
		case <-ctx.Done():
			t.Fatal("independent probes did not jointly reach the dial barrier")
		}
	}
	close(release)
	for range probes {
		select {
		case result := <-results:
			if result.err != nil || !result.active || result.dialCalls != 1 {
				t.Fatalf("independent campaign egress result: %+v", result)
			}
		case <-ctx.Done():
			t.Fatal("independent probes did not finish after releasing their dial barrier")
		}
	}
}

// A genuinely live listener and healthy record cannot activate a stale
// supervisor generation, even when its reusable pid is still running.
func TestSupervisedCampaignEgressRejectsHealthyWrongGeneration(t *testing.T) {
	stateDir, listener := newCampaignEgressListenerFixture(t)
	statePath := filepath.Join(stateDir, "supervisor.state.json")
	var state SupervisorState
	if err := readJSONFile(statePath, &state); err != nil {
		t.Fatal(err)
	}
	state.SupervisorStartTimeTicks++
	if err := writePublicJSON(statePath, state); err != nil {
		t.Fatal(err)
	}
	dialCalls := 0
	dialContext := func(ctx context.Context, network, address string) (net.Conn, error) {
		dialCalls++
		return (&net.Dialer{}).DialContext(ctx, network, listener.Addr().String())
	}
	active, err := supervisedCampaignEgressActiveWithDial(context.Background(), stateDir, dialContext)
	if err != nil || active || dialCalls != 0 {
		t.Fatalf("wrong supervisor generation active=%t dials=%d err=%v", active, dialCalls, err)
	}
}

func TestRunMainHelpAndVersionDoNotLoadConfiguration(t *testing.T) {
	for _, args := range [][]string{{"help"}, {"-h"}, {"--help"}, {"version"}, {"--version"}} {
		if err := runMain(args); err != nil {
			t.Fatalf("runMain(%q): %v", args, err)
		}
	}
}

func TestRunMainListenerProbeChecksTheExactPacketBoundarySynchronously(t *testing.T) {
	listener, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.LocalAddr().String()
	arguments := []string{"__listener_probe", "--network=udp", "--address=" + address}
	if err := runMain(arguments); err == nil || !strings.Contains(err.Error(), address) {
		t.Fatalf("occupied hidden packet probe error = %v", err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	if err := runMain(arguments); err != nil {
		t.Fatalf("available hidden packet probe: %v", err)
	}
	for _, invalid := range [][]string{
		{"__listener_probe", "--network=tcp", "--address=" + address},
		{"__listener_probe", "--network=udp"},
		{"__listener_probe", "--network=udp", "--address=" + address, "extra"},
		{"__listener_probe", "--network=udp", "--address=" + address, "--address=" + address},
	} {
		if err := runMain(invalid); err == nil {
			t.Errorf("invalid hidden listener probe accepted: %q", invalid)
		}
	}
}

func TestRunMainServerModulesConfineTLSFallbackToConnectLoopback(t *testing.T) {
	for _, invalid := range [][]string{
		{"__server_connect", "--port=19081"},
		{"__server_connect", "--port=19081", "--tls-default-host=example.com"},
		{"__server_connect", "--port=19081", "--tls-default-host=192.0.2.1"},
		{"__server_connect", "--port=19081", "--tls-default-host=127.0.1.1"},
		{"__server_api", "--port=18081", "--tls-default-host=127.0.1.1"},
		{"__server_api", "--port=18081", "--direct-h3-loopback"},
		{"__server_taskworker", "--port=20081", "--workload-profile=subnet-operator", "--tls-default-host=127.0.1.1"},
		{"__server_taskworker", "--port=20081", "--workload-profile=subnet-operator", "--direct-h3-loopback"},
	} {
		if err := runMain(invalid); err == nil {
			t.Errorf("invalid internal server invocation accepted: %q", invalid)
		}
	}
}

// The hidden migration entrypoint accepts no alternate catalog or arguments;
// its injected callback proves it receives the caller's exact context.
func TestRunServerDatabaseMigrationUsesEmbeddedCatalog(t *testing.T) {
	type contextKey string
	ctx := context.WithValue(context.Background(), contextKey("catalog"), "workload")
	calls := 0
	err := runServerDatabaseMigration(ctx, nil, func(migrationCtx context.Context) {
		calls++
		if migrationCtx.Value(contextKey("catalog")) != "workload" {
			t.Fatal("migration lost its workload context")
		}
	})
	if err != nil || calls != 1 {
		t.Fatalf("embedded migration calls=%d error=%v", calls, err)
	}
	if err := runServerDatabaseMigration(ctx, []string{"db", "migrate"}, func(context.Context) { calls++ }); err == nil || calls != 1 {
		t.Fatalf("alternate migration surface calls=%d error=%v", calls, err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := runServerDatabaseMigration(canceled, nil, func(context.Context) { calls++ }); !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("canceled migration calls=%d error=%v", calls, err)
	}
}

func TestRunMainServerContractCleanupRejectsUnboundedInvocation(t *testing.T) {
	for _, invalid := range [][]string{
		{"__server_cleanup_contracts"},
		{"__server_cleanup_contracts", "--cutoff-unix-nano=1"},
		{"__server_cleanup_contracts", "--cutoff-unix-nano=0", "--result=/tmp/result.json"},
		{"__server_cleanup_contracts", "--cutoff-unix-nano=1", "--result=relative.json"},
		{"__server_cleanup_contracts", "--cutoff-unix-nano=1", "--result=/tmp/result.json", "extra"},
	} {
		if err := runMain(invalid); err == nil {
			t.Errorf("invalid internal server cleanup accepted: %q", invalid)
		}
	}
}

func TestLightExecutableSelectsLightnodeProfile(t *testing.T) {
	if got := configPathForExecutable("/release/build/sim-testnet-light"); got != "sim-testnet/testnet-light.yml" {
		t.Fatalf("light executable config = %q", got)
	}
	if got := configPathForExecutable(`C:\\release\\sim-testnet-light.exe`); got != "sim-testnet/testnet-light.yml" {
		t.Fatalf("Windows light executable config = %q", got)
	}
	if got := configPathForExecutable("/release/build/sim-testnet"); got != "sim-testnet/testnet.yml" {
		t.Fatalf("release executable config = %q", got)
	}
}

func TestParseCLIRejectsInvalidSurface(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"unknown"},
		{"doctor", "extra"},
		{"doctor", "--format", "yaml"},
	} {
		if _, _, err := parseCLI(args); err == nil {
			t.Fatalf("parseCLI(%q) unexpectedly succeeded", args)
		}
	}
}
