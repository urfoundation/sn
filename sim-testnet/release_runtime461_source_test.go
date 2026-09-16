// Runtime461 retains earlier source scope and pins the reviewed basket delta.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfoundation/sn/crv4"
)

// Every prior path remains attested, including unchanged consumed interfaces;
// new root basket and migration paths explain the actual upstream change.
func TestRuntime461SourceAttestationPreservesReviewed460Scope(t *testing.T) {
	raw, err := os.ReadFile("../docs/spec/runtime-v461-source.sha256")
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != "9c5a558924993e796449198c081e62d165b36c1d7d853cc9a72d604e625ec8d5" {
		t.Fatal("runtime461 exact source scope changed")
	}
	paths := map[string]string{}
	for _, row := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		fields := strings.Fields(row)
		if len(fields) != 2 || paths[fields[1]] != "" {
			t.Fatal("runtime461 source rows are malformed or duplicated")
		}
		paths[fields[1]] = fields[0]
	}
	if len(paths) != 94 {
		t.Fatal("runtime461 lost reviewed source scope")
	}
	prior, err := os.ReadFile("../docs/spec/runtime-v460-source.sha256")
	if err != nil {
		t.Fatal(err)
	}
	unchanged := 0
	for _, row := range strings.Split(strings.TrimSpace(string(prior)), "\n") {
		fields := strings.Fields(row)
		if paths[fields[1]] == "" {
			t.Fatal("runtime461 omitted prior reviewed source")
		}
		if paths[fields[1]] == fields[0] {
			unchanged++
		}
	}
	if unchanged != 53 {
		t.Fatal("runtime461 delta differs from reviewed53 unchanged/18 changed predecessors")
	}
	for _, path := range []string{"common/src/proxy.rs", "pallets/subtensor/src/staking/basket_trade.rs", "pallets/subtensor/src/staking/basket_flush.rs", "pallets/subtensor/src/staking/basket_views.rs", "pallets/subtensor/src/migrations/migrate_remove_root_weights.rs", "pallets/subtensor/src/rpc_info/basket_info.rs", "pallets/subtensor/src/macros/events.rs"} {
		if paths[path] == "" {
			t.Fatalf("runtime461 omitted changed dependency %s", path)
		}
	}
	checker, err := os.ReadFile("../scripts/check-runtime-v454-source.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"for current_spec in 455 458 459 460 461", "runtime-v461-source.sha256", "current_commit=\"7c9d45ebd423c7f6b0b477e11414fe2fe3a3794b\"", "expected_current_files=94", "SUBTENSOR_RUNTIME461_SOURCE", "expected_metadata_files=27"} {
		if !strings.Contains(string(checker), required) {
			t.Fatalf("runtime461 source checker omits %s", required)
		}
	}
}

// Current461 observation requires owned provenance and its complete predecessor catalog.
func TestRuntime461ArtifactCheckerRequiresOwnedObservationAndCompleteHistory(t *testing.T) {
	raw, err := os.ReadFile("../docs/spec/runtime-metadata-artifacts.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		name   string
		change func(map[string]any, []any)
	}{
		{name: "exact"},
		{name: "current independent claim", change: func(_ map[string]any, artifacts []any) { artifacts[8].(map[string]any)["independent_rpc"] = true }},
		{name: "current missing disclosure", change: func(_ map[string]any, artifacts []any) { delete(artifacts[8].(map[string]any), "independent_rpc") }},
		{name: "current foreign observation", change: func(_ map[string]any, artifacts []any) {
			artifacts[8].(map[string]any)["observation_rpc_url"] = "http://archive.example"
		}},
		{name: "missing460", change: func(manifest map[string]any, artifacts []any) {
			manifest["artifacts"] = append(artifacts[:7:7], artifacts[8])
		}},
		{name: "independent claim", change: func(_ map[string]any, artifacts []any) { artifacts[7].(map[string]any)["independent_rpc"] = true }},
		{name: "missing disclosure", change: func(_ map[string]any, artifacts []any) { delete(artifacts[7].(map[string]any), "independent_rpc") }},
		{name: "foreign observation", change: func(_ map[string]any, artifacts []any) {
			artifacts[7].(map[string]any)["observation_rpc_url"] = "http://archive.example"
		}},
		{name: "future version", change: func(_ map[string]any, artifacts []any) { artifacts[7].(map[string]any)["spec_version"] = 462 }},
		{name: "missing459", change: func(manifest map[string]any, artifacts []any) {
			manifest["artifacts"] = append(artifacts[:6:6], artifacts[7:]...)
		}},
	} {
		var manifest map[string]any
		if err := json.Unmarshal(raw, &manifest); err != nil {
			t.Fatal(err)
		}
		artifacts := manifest["artifacts"].([]any)
		if len(artifacts) != len(crv4.ReviewedRuntimeArtifacts()) {
			t.Fatal("runtime461 fixture is incomplete")
		}
		observation := artifacts[8].(map[string]any)
		endpoint := observation["observation_rpc_url"].(string)
		response, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "result": manifest["genesis_hash"]})
		if err != nil {
			t.Fatal(err)
		}
		dir := runtime458ArtifactShellFixture(t, string(response))
		for _, relative := range []string{"scripts/check-runtime-metadata-artifacts.sh", "docs/spec/runtime-metadata-artifacts.json", "tools/runtime-metadata-probe/Cargo.toml", "tools/runtime-metadata-probe/Cargo.lock", "tools/runtime-metadata-probe/rust-toolchain.toml"} {
			contents, err := os.ReadFile(filepath.Join("..", relative))
			if err != nil {
				t.Fatal(err)
			}
			destination := filepath.Join(dir, relative)
			if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(destination, contents, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if item.change != nil {
			item.change(manifest, artifacts)
		}
		changed, err := json.Marshal(manifest)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "docs/spec/runtime-metadata-artifacts.json"), changed, 0o600); err != nil {
			t.Fatal(err)
		}
		output, err := runRuntime458ArtifactShell(t, dir, "0", filepath.Join(dir, "scripts/check-runtime-metadata-artifacts.sh"))
		if err == nil {
			t.Fatalf("%s escaped the mocked stopping boundary", item.name)
		}
		if item.change == nil {
			if _, err := os.Stat(filepath.Join(dir, "cargo")); err != nil {
				t.Fatalf("reviewed461 did not reach the exact-genesis boundary: %s %v", output, err)
			}
			checkRuntime458ArtifactArguments(t, dir, endpoint)
		} else {
			if !strings.Contains(string(output), "manifest is malformed or unreviewed") {
				t.Fatalf("%s failed outside manifest admission: %s", item.name, output)
			}
			for _, filename := range []string{"cargo", "calls", "sleep"} {
				if _, err := os.Stat(filepath.Join(dir, filename)); !os.IsNotExist(err) {
					t.Fatalf("%s reached %s before rejection", item.name, filename)
				}
			}
		}
	}
}
