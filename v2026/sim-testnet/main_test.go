package main

import (
	"net"
	"os"
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
		"doctor", "plan", "setup", "launch", "resume", "status", "inspect",
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
	})
	if err != nil {
		t.Fatal(err)
	}
	if command != "launch" || !options.Apply || !options.Detach || options.PlanHash != "sha256:approved" {
		t.Fatalf("write flags parsed incorrectly: command=%q options=%+v", command, options)
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
