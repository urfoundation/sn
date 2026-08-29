package main

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

func TestCanonicalArtifactHashIgnoresTopLevelFoundryNoise(t *testing.T) {
	left := []byte(`{
		"id": 1,
		"abi": [{"type":"function","name":"f","inputs":[]}],
		"bytecode": {"object":"0x6000"},
		"deployedBytecode": {"object":"0x6001","immutableReferences":{}},
		"metadata": {"compiler":{"version":"0.8.24"}},
		"methodIdentifiers": {"f()":"26121ff0"},
		"storageLayout": {"storage":[],"types":{}}
	}`)
	right := []byte(`{"rawMetadata":"ignored","storageLayout":{"types":{},"storage":[]},"methodIdentifiers":{"f()":"26121ff0"},"metadata":{"compiler":{"version":"0.8.24"}},"deployedBytecode":{"immutableReferences":{},"object":"0x6001"},"bytecode":{"object":"0x6000"},"abi":[{"inputs":[],"name":"f","type":"function"}],"id":999}`)
	var a, b artifact
	if err := json.Unmarshal(left, &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(right, &b); err != nil {
		t.Fatal(err)
	}
	if canonicalArtifactHash(a, nil) != canonicalArtifactHash(b, nil) {
		t.Fatal("non-semantic Foundry fields changed the canonical artifact hash")
	}
	b.Bytecode.Object = "0x6002"
	if canonicalArtifactHash(a, nil) == canonicalArtifactHash(b, nil) {
		t.Fatal("creation bytecode drift did not change the canonical artifact hash")
	}
}

func TestCanonicalArtifactHashIgnoresExpandedMetadataGraph(t *testing.T) {
	left := []byte(`{
		"abi": [],
		"bytecode": {"object":"0x6000"},
		"deployedBytecode": {"object":"0x6001","immutableReferences":{}},
		"metadata": {"settings":{"compilationTarget":{"src/A.sol":"A"}},"sources":{"src/A.sol":{"keccak256":"0x01"}}},
		"methodIdentifiers": {},
		"storageLayout": {"storage":[],"types":{}}
	}`)
	right := []byte(`{
		"abi": [],
		"bytecode": {"object":"0x6000"},
		"deployedBytecode": {"object":"0x6001","immutableReferences":{}},
		"metadata": {"settings":{"compilationTarget":{"test/A.t.sol":"ATest"}},"sources":{"src/A.sol":{"keccak256":"0x01"},"test/A.t.sol":{"keccak256":"0x02"}}},
		"methodIdentifiers": {},
		"storageLayout": {"storage":[],"types":{}}
	}`)
	var a, b artifact
	if err := json.Unmarshal(left, &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(right, &b); err != nil {
		t.Fatal(err)
	}
	if canonicalArtifactHash(a, nil) != canonicalArtifactHash(b, nil) {
		t.Fatal("expanded Foundry metadata graph changed the deployment artifact hash")
	}
}

func TestCanonicalArtifactHashIgnoresStorageASTIDsButPinsSlots(t *testing.T) {
	template := func(astID int, slot string) artifact {
		raw := `{"abi":[],"bytecode":{"object":"0x6000"},"deployedBytecode":{"object":"0x6001","immutableReferences":{}},"metadata":{},"methodIdentifiers":{},"storageLayout":{"storage":[{"astId":AST,"contract":"src/A.sol:A","label":"value","offset":0,"slot":"SLOT","type":"t_struct(Value)AST_storage"}],"types":{"t_struct(Value)AST_storage":{"encoding":"inplace","label":"struct A.Value","numberOfBytes":"32","members":[{"astId":AST,"contract":"src/A.sol:A","label":"inner","offset":0,"slot":"0","type":"t_uint256"}]},"t_uint256":{"encoding":"inplace","label":"uint256","numberOfBytes":"32"}}}}`
		raw = strings.ReplaceAll(strings.ReplaceAll(raw, "AST", strconv.Itoa(astID)), "SLOT", slot)
		var value artifact
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	a := template(12, "0")
	b := template(999, "0")
	if canonicalArtifactHash(a, nil) != canonicalArtifactHash(b, nil) {
		t.Fatal("compiler-only storage AST ids changed the artifact hash")
	}
	b = template(999, "1")
	if canonicalArtifactHash(a, nil) == canonicalArtifactHash(b, nil) {
		t.Fatal("storage slot drift did not change the artifact hash")
	}
}

func TestCanonicalArtifactHashUsesSemanticImmutableReferences(t *testing.T) {
	decode := func(raw string) artifact {
		var value artifact
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	a := decode(`{"abi":[],"bytecode":{"object":"0x6000"},"deployedBytecode":{"object":"0x6001","immutableReferences":{"12":[{"start":7,"length":32}]}},"methodIdentifiers":{},"storageLayout":{"storage":[],"types":{}}}`)
	b := decode(`{"abi":[],"bytecode":{"object":"0x6000"},"deployedBytecode":{"object":"0x6001","immutableReferences":{"999":[{"start":7,"length":32}]}},"methodIdentifiers":{},"storageLayout":{"storage":[],"types":{}}}`)
	refs := map[string][]int{"owner": {7}}
	if canonicalArtifactHash(a, refs) != canonicalArtifactHash(b, refs) {
		t.Fatal("compiler immutable-reference id changed the deployment artifact hash")
	}
	if canonicalArtifactHash(a, refs) == canonicalArtifactHash(b, map[string][]int{"owner": {8}}) {
		t.Fatal("semantic immutable-reference offset drift did not change the artifact hash")
	}
}
