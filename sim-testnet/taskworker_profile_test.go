package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// TestServerTaskworkerProfileSurvivesSupervisorSerialization binds the workload
// to both operator specs persisted and reused by the existing restart loop.
func TestServerTaskworkerProfileSurvivesSupervisorSerialization(t *testing.T) {
	cfg := testResolvedConfig(t)
	cfg.Config.Topology.Operators = 2
	specs, err := buildServerSpecs(cfg, t.TempDir(), map[string]string{
		"sim-testnet":           "/fixture/sim-testnet",
		connectServerBinaryName: "/fixture/sim-testnet-connect",
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(SupervisorFile{Specs: specs})
	if err != nil {
		t.Fatal(err)
	}
	var persisted SupervisorFile
	if err := json.Unmarshal(encoded, &persisted); err != nil {
		t.Fatal(err)
	}
	workers := 0
	for i, spec := range persisted.Specs {
		if !reflect.DeepEqual(spec.Args, specs[i].Args) {
			t.Fatalf("restart specification changed arguments for %s", spec.ID)
		}
		profileCount := 0
		for _, arg := range spec.Args {
			if strings.HasPrefix(arg, "--workload-profile=") {
				if arg != "--workload-profile=subnet-operator" {
					t.Fatalf("unexpected workload profile for %s: %q", spec.ID, arg)
				}
				profileCount++
			}
		}
		if spec.Role == "operator-taskworker" {
			workers++
			if profileCount != 1 || spec.RestartLimit < 1 || spec.Args[0] != "__server_taskworker" {
				t.Fatalf("operator taskworker lost explicit production-module workload scope: %+v", spec)
			}
		} else if profileCount != 0 {
			t.Fatalf("taskworker-only workload scope leaked to %s", spec.ID)
		}
	}
	if workers != 2 {
		t.Fatalf("profile was not bound to both operators: %d", workers)
	}
}

// TestRunMainTaskworkerRequiresExplicitSubnetProfile rejects old or misspelled
// restart invocations before any environment access, and confines the option.
func TestRunMainTaskworkerRequiresExplicitSubnetProfile(t *testing.T) {
	for _, args := range [][]string{
		{"__server_taskworker", "--port=20081"},
		{"__server_taskworker", "--port=20081", "--workload-profile=unknown-fixture"},
		{"__server_api", "--port=18081", "--workload-profile=subnet-operator"},
		{"__server_connect", "--port=19081", "--workload-profile=subnet-operator"},
	} {
		if err := runMain(args); err == nil || !strings.Contains(err.Error(), "profile") {
			t.Errorf("unscoped or misplaced taskworker profile was accepted: %v error=%v", args, err)
		}
	}
	// Reach production RunOptions validation without starting any service.
	err := runMain([]string{"__server_taskworker", "--port=20081", "--workload-profile=subnet-operator", "--count=0"})
	if err == nil || !strings.Contains(err.Error(), "taskworker count") {
		t.Fatalf("valid explicit profile did not reach the production runner: %v", err)
	}
}
