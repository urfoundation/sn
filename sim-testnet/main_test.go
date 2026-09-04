package main

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImportedProductionModulesKeepJSONStdoutSeparateFromDiagnostics(t *testing.T) {
	if os.Stdout == os.Stderr || os.Stdout.Fd() == os.Stderr.Fd() {
		t.Fatal("an imported production module aliased stderr to stdout")
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

// A healthy state and the exact supervised listener jointly activate the
// shared route; neither a state-file assertion nor an arbitrary port suffices.
func TestSupervisedCampaignEgressRequiresExactHealthyListener(t *testing.T) {
	listener, err := net.Listen("tcp", publicEVMEgressAddress)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
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
	active, err := supervisedCampaignEgressActive(context.Background(), stateDir)
	if err != nil || !active {
		t.Fatalf("healthy supervised egress active=%t err=%v", active, err)
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
		{"__server_taskworker", "--port=20081", "--tls-default-host=127.0.1.1"},
		{"__server_taskworker", "--port=20081", "--direct-h3-loopback"},
	} {
		if err := runMain(invalid); err == nil {
			t.Errorf("invalid internal server invocation accepted: %q", invalid)
		}
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
