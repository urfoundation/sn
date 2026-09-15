// Runtime459 source and observation provenance extend the existing exact gate.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Retains all62 reviewed paths and the nine changed native dependencies.
// The exact manifest digest binds every source row, including unchanged ones.
func TestRuntime459SourceAttestationPreservesScopeAndChangedNativeDependencies(t *testing.T) {
	raw, err := os.ReadFile("../docs/spec/runtime-v459-source.sha256")
	if err != nil { t.Fatal(err) }
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != "e8a920ba20bd44204856b64f517f18f4e9b6e42abaca84531a449063cd6e1bcf" { t.Fatal("runtime459 exact source scope changed") }
	paths := map[string]bool{}
	for _, row := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		fields := strings.Fields(row)
		if len(fields) != 2 || paths[fields[1]] { t.Fatal("runtime459 source rows are malformed or duplicated") }
		paths[fields[1]] = true
	}
	if len(paths) != 71 { t.Fatal("runtime459 source scope is incomplete") }
	prior, err := os.ReadFile("../docs/spec/runtime-v458-source.sha256")
	if err != nil { t.Fatal(err) }
	for _, row := range strings.Split(strings.TrimSpace(string(prior)), "\n") {
		if !paths[strings.Fields(row)[1]] { t.Fatal("runtime459 omitted previously reviewed native scope") }
	}
	for _, path := range []string{"Cargo.lock", "pallets/subtensor/src/migrations/migrate_fix_root_pot_shortfall.rs", "pallets/subtensor/src/migrations/mod.rs", "pallets/subtensor/src/rpc_info/delegate_info.rs", "pallets/subtensor/src/staking/move_stake.rs", "pallets/subtensor/src/staking/remove_stake.rs", "pallets/subtensor/src/staking/set_children.rs", "pallets/subtensor/src/subnets/registration.rs", "pallets/subtensor/src/tests/destroy_alpha_tests.rs"} {
		if !paths[path] { t.Fatalf("runtime459 omitted changed native dependency %s", path) }
	}
	checker, err := os.ReadFile("../scripts/check-runtime-v454-source.sh")
	if err != nil { t.Fatal(err) }
	for _, required := range []string{"for current_spec in 455 458 459", "runtime-v459-source.sha256", "current_commit=\"70378404b56c12a85bc8cd163aca2f32cf4d1b80\"", "expected_current_files=71", "SUBTENSOR_RUNTIME459_SOURCE", "expected_metadata_files=21"} {
		if !strings.Contains(string(checker), required) { t.Fatalf("runtime459 source checker omits %s", required) }
	}
}

// Runs the real checker with fake transport/compiler executables. The exact
// accepted459 observation reaches the post-genesis boundary; altered ownership
// or the loss of historical458 fails before any provider or compiler call.
func TestRuntime459ArtifactCheckerRequiresOwnedObservationAndCompleteHistory(t *testing.T) {
	raw, err := os.ReadFile("../docs/spec/runtime-metadata-artifacts.json")
	if err != nil { t.Fatal(err) }
	for _, item := range []struct {
		name string
		change func(map[string]any, []any)
	}{
		{name: "exact"},
		{name: "independent claim", change: func(_ map[string]any, artifacts []any) { artifacts[6].(map[string]any)["independent_rpc"] = true }},
		{name: "missing disclosure", change: func(_ map[string]any, artifacts []any) { delete(artifacts[6].(map[string]any), "independent_rpc") }},
		{name: "foreign observation", change: func(_ map[string]any, artifacts []any) { artifacts[6].(map[string]any)["observation_rpc_url"] = "http://archive.example" }},
		{name: "future version", change: func(_ map[string]any, artifacts []any) { artifacts[6].(map[string]any)["spec_version"] = 460 }},
		{name: "missing458", change: func(manifest map[string]any, artifacts []any) { manifest["artifacts"] = append(artifacts[:5:5], artifacts[6]) }},
	} {
		var manifest map[string]any
		if err := json.Unmarshal(raw, &manifest); err != nil { t.Fatal(err) }
		artifacts := manifest["artifacts"].([]any)
		if len(artifacts) != 7 { t.Fatal("runtime459 fixture is incomplete") }
		observation := artifacts[6].(map[string]any)
		endpoint := observation["observation_rpc_url"].(string)
		response, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "result": manifest["genesis_hash"]})
		if err != nil { t.Fatal(err) }
		dir := runtime458ArtifactShellFixture(t, string(response))
		for _, relative := range []string{"scripts/check-runtime-metadata-artifacts.sh", "docs/spec/runtime-metadata-artifacts.json", "tools/runtime-metadata-probe/Cargo.toml", "tools/runtime-metadata-probe/Cargo.lock", "tools/runtime-metadata-probe/rust-toolchain.toml"} {
			contents, err := os.ReadFile(filepath.Join("..", relative))
			if err != nil { t.Fatal(err) }
			destination := filepath.Join(dir, relative)
			if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil { t.Fatal(err) }
			if err := os.WriteFile(destination, contents, 0o600); err != nil { t.Fatal(err) }
		}
		if item.change != nil { item.change(manifest, artifacts) }
		changed, err := json.Marshal(manifest)
		if err != nil { t.Fatal(err) }
		if err := os.WriteFile(filepath.Join(dir, "docs/spec/runtime-metadata-artifacts.json"), changed, 0o600); err != nil { t.Fatal(err) }
		output, err := runRuntime458ArtifactShell(t, dir, "0", filepath.Join(dir, "scripts/check-runtime-metadata-artifacts.sh"))
		if err == nil { t.Fatalf("%s escaped the mocked stopping boundary", item.name) }
		if item.change == nil {
			if _, err := os.Stat(filepath.Join(dir, "cargo")); err != nil { t.Fatalf("reviewed459 did not reach the exact-genesis boundary: %s %v", output, err) }
			checkRuntime458ArtifactArguments(t, dir, endpoint)
		} else {
			if !strings.Contains(string(output), "manifest is malformed or unreviewed") { t.Fatalf("%s failed outside manifest admission: %s", item.name, output) }
			for _, filename := range []string{"cargo", "calls", "sleep"} {
				if _, err := os.Stat(filepath.Join(dir, filename)); !os.IsNotExist(err) { t.Fatalf("%s reached %s before rejection", item.name, filename) }
			}
		}
	}
}
