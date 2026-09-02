package main

import (
	"encoding/json"
	"fmt"
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

func metadataBytecode(executable, digest string) string {
	return executable + solidityMetadataPrefix + digest + solidityMetadataSuffix
}

func TestNormalizeSolidityMetadataDigestReproducesFullGraphDrift(t *testing.T) {
	// Reproduce the release-gate failure: Foundry emitted identical executable
	// bytes and Solidity 0.8.24 framing with only a different IPFS digest.
	first := metadataBytecode("6001600055", strings.Repeat("11", 32))
	second := metadataBytecode("6001600055", strings.Repeat("22", 32))
	normalizedFirst, err := normalizeSolidityMetadataDigest(first)
	if err != nil {
		t.Fatal(err)
	}
	normalizedSecond, err := normalizeSolidityMetadataDigest(second)
	if err != nil {
		t.Fatal(err)
	}
	if normalizedFirst != normalizedSecond {
		t.Fatal("metadata-only compilation-graph drift changed semantic bytecode")
	}
}

func TestNormalizeSolidityMetadataDigestRetainsExecutableBytes(t *testing.T) {
	digest := strings.Repeat("33", 32)
	first, err := normalizeSolidityMetadataDigest(metadataBytecode("6001600055", digest))
	if err != nil {
		t.Fatal(err)
	}
	second, err := normalizeSolidityMetadataDigest(metadataBytecode("6002600055", digest))
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("executable bytecode drift was erased with compiler metadata")
	}
}

func TestNormalizeSolidityMetadataDigestRejectsAdjacentTrailerDrift(t *testing.T) {
	valid := metadataBytecode("6000", strings.Repeat("44", 32))
	cases := map[string]string{
		"truncated":        valid[:len(valid)-2],
		"hash framing":     strings.Replace(valid, solidityMetadataPrefix, "a2646970667358211220", 1),
		"compiler version": strings.Replace(valid, "64736f6c63430008180033", "64736f6c63430008190033", 1),
		"invalid hex":      valid[:len(valid)-1] + "z",
	}
	for name, encoded := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := normalizeSolidityMetadataDigest(encoded); err == nil {
				t.Fatal("non-digest metadata drift was accepted")
			}
		})
	}
}

func TestNormalizedGeneratedSourceRetainsABIAndLayout(t *testing.T) {
	contract := item{Name: "Demo"}
	makeSource := func(digest, abi, layout string) []byte {
		creation := metadataBytecode("6001", digest)
		runtime := metadataBytecode("6002", digest)
		return []byte(fmt.Sprintf(`package main
const DemoABI = %s
const DemoCreationBytecode = %q
const DemoRuntimeBytecode = %q
const DemoRuntimeBytecodeHash = %q
const DemoFoundryArtifactHash = %q
const DemoStorageLayoutHash = %q
`, abi, creation, runtime, "0x"+strings.Repeat("55", 32), "0x"+strings.Repeat("66", 32), layout))
	}
	first, err := normalizeGeneratedContractSource("first.go", makeSource(strings.Repeat("11", 32), "`abi-one`", "layout-one"), []item{contract})
	if err != nil {
		t.Fatal(err)
	}
	metadataOnly, err := normalizeGeneratedContractSource("second.go", makeSource(strings.Repeat("22", 32), "`abi-one`", "layout-one"), []item{contract})
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(metadataOnly) {
		t.Fatal("generated source did not normalize metadata-only drift")
	}
	changedABI, err := normalizeGeneratedContractSource("abi.go", makeSource(strings.Repeat("22", 32), "`abi-two`", "layout-one"), []item{contract})
	if err != nil {
		t.Fatal(err)
	}
	changedLayout, err := normalizeGeneratedContractSource("layout.go", makeSource(strings.Repeat("22", 32), "`abi-one`", "layout-two"), []item{contract})
	if err != nil {
		t.Fatal(err)
	}
	if string(first) == string(changedABI) || string(first) == string(changedLayout) {
		t.Fatal("ABI or storage-layout drift was erased with compiler metadata")
	}
}
