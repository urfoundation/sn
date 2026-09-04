package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
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

func TestValidateEmptyLinearStorageAcceptsNamespacedUpgradeImplementation(t *testing.T) {
	if err := validateEmptyLinearStorage("adversary", json.RawMessage(`{"storage":[],"types":{}}`)); err != nil {
		t.Fatal(err)
	}
}

func TestValidateEmptyLinearStorageRejectsDirectSlot(t *testing.T) {
	err := validateEmptyLinearStorage("adversary", json.RawMessage(`{"storage":[{"slot":"0"}],"types":{}}`))
	if err == nil || !strings.Contains(err.Error(), "declares 1 linear storage slots") {
		t.Fatalf("direct storage slot was accepted: %v", err)
	}
}

func TestValidateLinearStorageCoordinatesAcceptsDrillSlots(t *testing.T) {
	raw := json.RawMessage(`{"storage":[{"label":"netuid","slot":"0"},{"label":"selfColdkey","slot":"1"},{"label":"settlementVault","slot":"2"},{"label":"reserveSink","slot":"3"}]}`)
	want := map[string]string{"netuid": "0", "settlementVault": "2", "reserveSink": "3"}
	if err := validateLinearStorageCoordinates("coordinator", raw, want); err != nil {
		t.Fatal(err)
	}
}

func TestValidateLinearStorageCoordinatesRejectsMovedMissingAndDuplicateDrillSlots(t *testing.T) {
	want := map[string]string{"netuid": "0", "settlementVault": "2", "reserveSink": "3"}
	invalid := []json.RawMessage{
		json.RawMessage(`{"storage":[{"label":"netuid","slot":"0"},{"label":"settlementVault","slot":"9"},{"label":"reserveSink","slot":"3"}]}`),
		json.RawMessage(`{"storage":[{"label":"netuid","slot":"0"},{"label":"settlementVault","slot":"2"}]}`),
		json.RawMessage(`{"storage":[{"label":"netuid","slot":"0"},{"label":"netuid","slot":"0"},{"label":"settlementVault","slot":"2"},{"label":"reserveSink","slot":"3"}]}`),
	}
	for index, raw := range invalid {
		if err := validateLinearStorageCoordinates("coordinator", raw, want); err == nil {
			t.Errorf("invalid storage-coordinate fixture %d was accepted", index)
		}
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

func TestPreserveReviewedBytecodeSelectsOnlyMetadataEquivalentPayload(t *testing.T) {
	digestOld := strings.Repeat("11", 32)
	digestNew := strings.Repeat("22", 32)
	metadataOnly := testContractItem(t, "MetadataOnly", "6001", digestNew)
	changed := testContractItem(t, "Changed", "6002", digestNew)
	oldMetadataCreation := metadataBytecode("600101", digestOld)
	oldMetadataRuntime := metadataBytecode("600102", digestOld)
	oldChangedCreation := metadataBytecode("600301", digestOld)
	oldChangedRuntime := metadataBytecode("600302", digestOld)
	existingItems := []item{
		{
			Name: "MetadataOnly", ABI: metadataOnly.ABI, Creation: oldMetadataCreation, Runtime: oldMetadataRuntime,
			RuntimeHash:  crypto.Keccak256Hash(mustDecodeHex(t, oldMetadataRuntime)).Hex(),
			ArtifactHash: canonicalArtifactHashForBytecode(metadataOnly.Artifact, metadataOnly.References, oldMetadataCreation, oldMetadataRuntime),
			References:   map[string][]int{}, Release: true,
		},
		{
			Name: "Changed", ABI: changed.ABI, Creation: oldChangedCreation, Runtime: oldChangedRuntime,
			RuntimeHash:  crypto.Keccak256Hash(mustDecodeHex(t, oldChangedRuntime)).Hex(),
			ArtifactHash: canonicalArtifactHashForBytecode(changed.Artifact, changed.References, oldChangedCreation, oldChangedRuntime),
			References:   map[string][]int{}, Release: true,
		},
	}
	existing, err := renderContractArtifacts(existingItems)
	if err != nil {
		t.Fatal(err)
	}
	preserved, err := preserveReviewedBytecode("contracts_gen.go", existing, []item{changed, metadataOnly})
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]item{}
	for _, contract := range preserved {
		byName[contract.Name] = contract
	}
	if byName["MetadataOnly"].Creation != oldMetadataCreation || byName["MetadataOnly"].Runtime != oldMetadataRuntime {
		t.Fatal("metadata-equivalent reviewed payload was not preserved")
	}
	if byName["Changed"].Creation != changed.Creation || byName["Changed"].Runtime != changed.Runtime {
		t.Fatal("executable change preserved the old deployment payload")
	}
}

func TestPreserveReviewedBytecodeRejectsInterfaceDrift(t *testing.T) {
	digestOld := strings.Repeat("33", 32)
	digestNew := strings.Repeat("44", 32)
	candidate := testContractItem(t, "InterfaceDrift", "6004", digestNew)
	oldCreation := metadataBytecode("600401", digestOld)
	oldRuntime := metadataBytecode("600402", digestOld)
	reviewed := reviewedContractItem(t, candidate, oldCreation, oldRuntime)
	existing, err := renderContractArtifacts([]item{reviewed})
	if err != nil {
		t.Fatal(err)
	}
	candidate.Artifact.ABI = json.RawMessage(`[{"type":"function","name":"g","inputs":[]}]`)
	candidate.ABI = string(candidate.Artifact.ABI)
	preserved, err := preserveReviewedBytecode("contracts_gen.go", existing, []item{candidate})
	if err != nil {
		t.Fatal(err)
	}
	if preserved[0].Creation != candidate.Creation || preserved[0].Runtime != candidate.Runtime {
		t.Fatal("interface drift preserved bytecode authenticated against the old interface")
	}
}

func TestPreserveReviewedBytecodeRejectsUnauthenticatedRuntime(t *testing.T) {
	digestOld := strings.Repeat("55", 32)
	digestNew := strings.Repeat("66", 32)
	candidate := testContractItem(t, "BadRuntimeHash", "6005", digestNew)
	reviewed := reviewedContractItem(t, candidate, metadataBytecode("600501", digestOld), metadataBytecode("600502", digestOld))
	reviewed.RuntimeHash = "0x" + strings.Repeat("ff", 32)
	existing, err := renderContractArtifacts([]item{reviewed})
	if err != nil {
		t.Fatal(err)
	}
	preserved, err := preserveReviewedBytecode("contracts_gen.go", existing, []item{candidate})
	if err != nil {
		t.Fatal(err)
	}
	if preserved[0].Creation != candidate.Creation || preserved[0].Runtime != candidate.Runtime {
		t.Fatal("unauthenticated reviewed runtime was preserved")
	}
}

func TestPreserveReviewedBytecodeRejectsUnauthenticatedArtifact(t *testing.T) {
	digestOld := strings.Repeat("77", 32)
	digestNew := strings.Repeat("88", 32)
	candidate := testContractItem(t, "BadArtifactHash", "6006", digestNew)
	reviewed := reviewedContractItem(t, candidate, metadataBytecode("600601", digestOld), metadataBytecode("600602", digestOld))
	reviewed.ArtifactHash = "0x" + strings.Repeat("ee", 32)
	existing, err := renderContractArtifacts([]item{reviewed})
	if err != nil {
		t.Fatal(err)
	}
	preserved, err := preserveReviewedBytecode("contracts_gen.go", existing, []item{candidate})
	if err != nil {
		t.Fatal(err)
	}
	if preserved[0].Creation != candidate.Creation || preserved[0].Runtime != candidate.Runtime {
		t.Fatal("unauthenticated reviewed artifact was preserved")
	}
}

func testContractItem(t *testing.T, name, executable, digest string) item {
	t.Helper()
	creation := metadataBytecode(executable+"01", digest)
	runtime := metadataBytecode(executable+"02", digest)
	a := artifact{
		ABI:               json.RawMessage(`[{"type":"function","name":"f","inputs":[]}]`),
		StorageLayout:     json.RawMessage(`{"storage":[],"types":{}}`),
		MethodIdentifiers: map[string]string{"f()": "26121ff0"},
	}
	a.Bytecode.Object = creation
	a.DeployedBytecode.Object = runtime
	return item{
		Name: name, ABI: string(a.ABI), Creation: creation, Runtime: runtime,
		RuntimeHash:  crypto.Keccak256Hash(mustDecodeHex(t, runtime)).Hex(),
		ArtifactHash: canonicalArtifactHash(a, map[string][]int{}), References: map[string][]int{},
		Release: true, Artifact: a,
	}
}

func reviewedContractItem(t *testing.T, candidate item, creation, runtime string) item {
	t.Helper()
	return item{
		Name: candidate.Name, ABI: candidate.ABI, Creation: creation, Runtime: runtime,
		RuntimeHash:  crypto.Keccak256Hash(mustDecodeHex(t, runtime)).Hex(),
		ArtifactHash: canonicalArtifactHashForBytecode(candidate.Artifact, candidate.References, creation, runtime),
		References:   candidate.References, Release: true,
	}
}

func mustDecodeHex(t *testing.T, value string) []byte {
	t.Helper()
	decoded, err := hex.DecodeString(trim0x(value))
	if err != nil {
		t.Fatal(err)
	}
	return decoded
}
