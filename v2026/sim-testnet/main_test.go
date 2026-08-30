package main

import (
	"os"
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
