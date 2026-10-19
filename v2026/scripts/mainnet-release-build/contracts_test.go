// Synthetic complete catalogues exercise selection without a compiler or chain.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Independent retained and selected files make silent substitution observable.
func buildFixtureContractCatalogues(t *testing.T) (buildConfig, []releaseContract, []releaseContract) {
	t.Helper()
	root := t.TempDir()
	config := buildConfig{ContractCatalog: "fresh", Workspace: filepath.Join(root, "source"), Output: filepath.Join(root, "output")}
	retained, selected := []releaseContract{}, []releaseContract{}
	for _, name := range []string{"Coordinator", "ERC1967Proxy", "ReserveSink", "SettlementVault", "ValidatorEvidence"} {
		contract := releaseContract{Name: name, Abi: `[{"type":"constructor","inputs":[{"name":"count","type":"uint16"}]}]`, Creation: "6000", Runtime: "6001", RuntimeHash: "0x" + strings.Repeat("1", 64), ArtifactHash: "0x" + strings.Repeat("2", 64), StorageLayoutHash: buildFixtureDigest(name + "-layout"), ImmutableReferences: map[string][]int{"synthetic": {0}}}
		retained = append(retained, contract)
		contract.Creation, contract.Runtime = "6002", "6003"
		contract.RuntimeHash, contract.ArtifactHash = "0x"+strings.Repeat("3", 64), "0x"+strings.Repeat("4", 64)
		selected = append(selected, contract)
		path := filepath.Join(config.Workspace, "sn/evm/out", releaseContractPaths()[name])
		buildFixtureContractFile(t, path, []byte(`{"bytecode":{"object":"0x6002"},"deployedBytecode":{"object":"0x6003"}}`))
	}
	buildFixtureContractBinding(t, filepath.Join(config.Workspace, "sn/sim-testnet/contracts_gen.go"), retained)
	buildFixtureContractBinding(t, filepath.Join(config.Output, "inputs/contracts-fresh-binding.go"), selected)
	buildFixtureContractExport(t, filepath.Join(config.Output, "inputs/contracts-retained.json"), retained)
	buildFixtureContractExport(t, filepath.Join(config.Output, "inputs/contracts-release.json"), selected)
	return config, retained, selected
}

// Fixtures use only regular private files, matching the real capture boundary.
func buildFixtureContractFile(t *testing.T, path string, raw []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
}

// Bindings are parsed as source data, never imported or executed.
func buildFixtureContractBinding(t *testing.T, path string, contracts []releaseContract) {
	t.Helper()
	var raw strings.Builder
	raw.WriteString("package fixture\n")
	for _, contract := range contracts {
		for suffix, value := range map[string]string{"ABI": contract.Abi, "CreationBytecode": contract.Creation, "RuntimeBytecode": contract.Runtime, "RuntimeBytecodeHash": contract.RuntimeHash, "FoundryArtifactHash": contract.ArtifactHash, "StorageLayoutHash": contract.StorageLayoutHash} {
			fmt.Fprintf(&raw, "const %s%s = %q\n", contract.Name, suffix, value)
		}
	}
	buildFixtureContractFile(t, path, []byte(raw.String()))
}

// The existing bootstrap wire schema carries both kinds of catalogue.
func buildFixtureContractExport(t *testing.T, path string, contracts []releaseContract) {
	t.Helper()
	raw, err := json.Marshal(struct {
		Schema    string            `json:"schema"`
		Artifacts []releaseContract `json:"artifacts"`
	}{Schema: "urnetwork-contract-release-artifacts-v1", Artifacts: contracts})
	if err != nil {
		t.Fatal(err)
	}
	buildFixtureContractFile(t, path, raw)
}

func TestReleaseBuildFreshCaptureKeepsHistoricalBytesAndExactSelection(t *testing.T) {
	config, retained, selected := buildFixtureContractCatalogues(t)
	history := []string{filepath.Join(config.Workspace, "sn/sim-testnet/contracts_gen.go"), filepath.Join(config.Output, "inputs/contracts-retained.json")}
	before := map[string][]byte{}
	for _, path := range history {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		before[path] = raw
	}
	contracts, artifacts, err := captureBuildContracts(config, filepath.Join(config.Output, "inputs/contracts-release.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(contracts) != 5 || len(artifacts) != 5 {
		t.Fatalf("incomplete production census: %d contracts, %d artifacts", len(contracts), len(artifacts))
	}
	for index, contract := range contracts {
		oldCreation, _, _ := hashBuildBytecode(retained[index].Creation, 49152)
		oldRuntime, _, _ := hashBuildBytecode(retained[index].Runtime, 24576)
		newCreation, _, _ := hashBuildBytecode(selected[index].Creation, 49152)
		newRuntime, _, _ := hashBuildBytecode(selected[index].Runtime, 24576)
		if contract.Name != selected[index].Name || !contract.ExactBytes || contract.SelectedCreationSha256 != newCreation || contract.RebuiltCreationSha256 != newCreation || contract.SelectedRuntimeSha256 != newRuntime || contract.RebuiltRuntimeSha256 != newRuntime || contract.RetainedCreationSha256 != oldCreation || contract.RetainedRuntimeSha256 != oldRuntime || contract.SelectedRuntimeHash != selected[index].RuntimeHash || contract.SelectedArtifactHash != selected[index].ArtifactHash || contract.RetainedRuntimeHash != retained[index].RuntimeHash || contract.RetainedArtifactHash != retained[index].ArtifactHash {
			t.Fatalf("historical, selected or compiler identity collapsed: %+v", contract)
		}
	}
	for _, path := range history {
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(before[path], after) {
			t.Fatalf("capture rewrote historical catalogue %s: %v", path, err)
		}
	}
}

func TestReleaseBuildDefaultCaptureRetainsHistoricalCatalogue(t *testing.T) {
	for _, mode := range []string{"", "retained"} {
		config, retained, _ := buildFixtureContractCatalogues(t)
		config.ContractCatalog = mode
		export := filepath.Join(config.Output, "inputs/contracts-release.json")
		buildFixtureContractExport(t, export, retained)
		contracts, _, err := captureBuildContracts(config, export)
		if err != nil {
			t.Fatalf("retained mode %q: %v", mode, err)
		}
		for _, contract := range contracts {
			if contract.ExactBytes || contract.SelectedCreationSha256 != contract.RetainedCreationSha256 || contract.SelectedRuntimeSha256 != contract.RetainedRuntimeSha256 {
				t.Fatalf("retained selection changed or drift claimed exact: %+v", contract)
			}
		}
	}
}

func TestReleaseBuildFreshCaptureRejectsCreationOrRuntimeMismatch(t *testing.T) {
	for _, field := range []string{"bytecode", "deployedBytecode"} {
		config, _, _ := buildFixtureContractCatalogues(t)
		compiler := map[string]map[string]string{"bytecode": {"object": "6002"}, "deployedBytecode": {"object": "6003"}}
		compiler[field]["object"] = "6004"
		raw, err := json.Marshal(compiler)
		if err != nil {
			t.Fatal(err)
		}
		buildFixtureContractFile(t, filepath.Join(config.Workspace, "sn/evm/out", releaseContractPaths()["Coordinator"]), raw)
		if _, _, err := captureBuildContracts(config, filepath.Join(config.Output, "inputs/contracts-release.json")); err == nil || !strings.Contains(err.Error(), "exact compiler bytes") {
			t.Fatalf("fresh %s drift was not refused at equality boundary: %v", field, err)
		}
	}
}

func TestReleaseBuildFreshCaptureRejectsInterfaceDrift(t *testing.T) {
	for _, field := range []string{"abi", "constructor", "storage-layout", "immutable-offset", "immutable-name"} {
		config, _, selected := buildFixtureContractCatalogues(t)
		switch field {
		case "abi":
			selected[0].Abi = `[]`
		case "constructor":
			selected[0].Abi = strings.ReplaceAll(selected[0].Abi, "uint16", "uint32")
		case "storage-layout":
			selected[0].StorageLayoutHash = buildFixtureDigest("changed-layout")
		case "immutable-offset":
			selected[0].ImmutableReferences = map[string][]int{"synthetic": {1}}
		case "immutable-name":
			selected[0].ImmutableReferences = map[string][]int{"different": {0}}
		}
		buildFixtureContractBinding(t, filepath.Join(config.Output, "inputs/contracts-fresh-binding.go"), selected)
		buildFixtureContractExport(t, filepath.Join(config.Output, "inputs/contracts-release.json"), selected)
		if _, _, err := captureBuildContracts(config, filepath.Join(config.Output, "inputs/contracts-release.json")); err == nil || !strings.Contains(err.Error(), "interface differs") {
			t.Fatalf("fresh %s drift was not refused at interface boundary: %v", field, err)
		}
	}
}

func TestReleaseBuildFreshCaptureRequiresHistoricalExport(t *testing.T) {
	for _, missing := range []bool{true, false} {
		config, retained, _ := buildFixtureContractCatalogues(t)
		path := filepath.Join(config.Output, "inputs/contracts-retained.json")
		if missing {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		} else {
			retained[0].Creation = "6009"
			buildFixtureContractExport(t, path, retained)
		}
		if _, _, err := captureBuildContracts(config, filepath.Join(config.Output, "inputs/contracts-release.json")); err == nil {
			t.Fatalf("accepted missing/substituted history; missing=%t", missing)
		}
	}
}

func TestReleaseBuildCatalogueModeIsExplicit(t *testing.T) {
	config := buildConfig{Schema: buildSchema, CandidateId: "synthetic-candidate", Version: "synthetic-v1", SourceDateEpoch: 1, Workspace: "/synthetic/source", Output: "/synthetic/output"}
	for _, name := range []string{"sn", "server", "proxy", "glog", "goidenticons", "userwireguard", "warp", "forge-std", "openzeppelin-contracts", "openzeppelin-contracts-upgradeable"} {
		path := name
		if strings.HasPrefix(name, "openzeppelin-") || name == "forge-std" {
			path = "sn/evm/lib/" + name
		}
		config.Repositories = append(config.Repositories, repositoryPin{Name: name, Path: path, Commit: strings.Repeat("a", 40), Tree: strings.Repeat("b", 40)})
	}
	for _, mode := range []string{"", "retained", "fresh"} {
		config.ContractCatalog = mode
		if err := config.validate(); err != nil {
			t.Fatalf("valid catalogue mode %q rejected: %v", mode, err)
		}
	}
	for _, mode := range []string{"Fresh", "fresh ", "metadata-equivalent"} {
		config.ContractCatalog = mode
		if err := config.validate(); err == nil {
			t.Fatalf("unknown mode %q accepted", mode)
		}
		manifest := buildFixtureManifest()
		manifest.ContractCatalog = mode
		if err := sealBuildManifest(&manifest); err == nil {
			t.Fatalf("unknown manifest mode %q sealed", mode)
		}
	}
}

func TestReleaseBuildFreshManifestCannotSealDrift(t *testing.T) {
	manifest := buildFixtureManifest()
	manifest.ContractCatalog = "fresh"
	manifest.Contracts[0].RetainedRuntimeSha256 = buildFixtureDigest("historical-runtime")
	if err := sealBuildManifest(&manifest); err != nil {
		t.Fatalf("exact fresh selection with separate history rejected: %v", err)
	}
	manifest.Contracts[0].RebuiltRuntimeSha256 = buildFixtureDigest("unselected-runtime")
	manifest.Contracts[0].ExactBytes = false
	manifest.SourceToBytecodeExact = false
	if err := sealBuildManifest(&manifest); err == nil {
		t.Fatal("fresh mode sealed a nonexact selection")
	}
}

func TestReleaseBuildRetainedManifestRejectsSelectionSubstitution(t *testing.T) {
	for _, field := range []string{"creation", "runtime", "runtime-hash", "artifact-hash"} {
		manifest := buildFixtureManifest()
		switch field {
		case "creation":
			manifest.Contracts[0].SelectedCreationSha256 = buildFixtureDigest("other-creation")
			manifest.Contracts[0].RebuiltCreationSha256 = manifest.Contracts[0].SelectedCreationSha256
		case "runtime":
			manifest.Contracts[0].SelectedRuntimeSha256 = buildFixtureDigest("other-runtime")
			manifest.Contracts[0].RebuiltRuntimeSha256 = manifest.Contracts[0].SelectedRuntimeSha256
		case "runtime-hash":
			manifest.Contracts[0].SelectedRuntimeHash = "0x" + strings.Repeat("3", 64)
		case "artifact-hash":
			manifest.Contracts[0].SelectedArtifactHash = "0x" + strings.Repeat("4", 64)
		}
		if err := sealBuildManifest(&manifest); err == nil {
			t.Fatalf("retained catalogue silently substituted %s", field)
		}
	}
}

func TestReleaseBuildCataloguesRejectAmbiguousOrIncompleteExports(t *testing.T) {
	for _, field := range []string{"duplicate-contract", "unknown-contract", "missing-contract", "unknown-field", "duplicate-field", "schema"} {
		config, _, selected := buildFixtureContractCatalogues(t)
		path := filepath.Join(config.Output, "inputs/contracts-release.json")
		switch field {
		case "duplicate-contract":
			selected[4] = selected[0]
		case "unknown-contract":
			selected[4].Name = "SyntheticUnknown"
		case "missing-contract":
			selected = selected[:4]
		}
		buildFixtureContractExport(t, path, selected)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		switch field {
		case "unknown-field":
			raw = bytes.Replace(raw, []byte(`"artifacts":`), []byte(`"unknown":true,"artifacts":`), 1)
		case "duplicate-field":
			raw = bytes.Replace(raw, []byte(`"name":"Coordinator"`), []byte(`"name":"Coordinator","name":"Coordinator"`), 1)
		case "schema":
			raw = bytes.Replace(raw, []byte("urnetwork-contract-release-artifacts-v1"), []byte("synthetic-unknown-schema"), 1)
		}
		buildFixtureContractFile(t, path, raw)
		if _, err := readReleaseContracts(path); err == nil {
			t.Fatalf("accepted malformed catalogue: %s", field)
		}
	}
}
