// Command gencontracts converts reviewed Foundry artifacts into the committed
// Go deployment payload used by sim-testnet. It never compiles Solidity.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/crypto"
)

type artifact struct {
	ABI      json.RawMessage `json:"abi"`
	Bytecode struct {
		Object string `json:"object"`
	} `json:"bytecode"`
	DeployedBytecode struct {
		Object              string `json:"object"`
		ImmutableReferences map[string][]struct {
			Start  int `json:"start"`
			Length int `json:"length"`
		} `json:"immutableReferences"`
	} `json:"deployedBytecode"`
	StorageLayout     json.RawMessage   `json:"storageLayout"`
	MethodIdentifiers map[string]string `json:"methodIdentifiers"`
}
type item struct {
	Name, Path, ABI, Creation, Runtime, RuntimeHash, ArtifactHash, StorageLayoutHash string
	References                                                                       map[string][]int
	Release                                                                          bool
	Variable                                                                         string
	Artifact                                                                         artifact
}

var foundryTypeASTID = regexp.MustCompile(`(t_(?:struct|contract|enum|userDefinedValueType)\([^)]*\))\d+`)
var immutableDeclaration = regexp.MustCompile(`\bimmutable\s+([A-Za-z_][A-Za-z0-9_]*)\s*(?:=|;)`)

type artifactDefinition struct {
	name, path string
	release    bool
	// variable names a generated singleton for testnet-only tooling. Release
	// artifacts are emitted into ReleaseContractArtifacts instead.
	variable string
	// Solidity emits immutable-reference ids in source declaration order. Keep
	// the semantic names here, and independently verify that order against the
	// source files, so generated runtime verification never relies on an
	// unnamed/compiler-id ordering convention.
	immutableNames   []string
	immutableSources []string
}

var artifactDefinitions = []artifactDefinition{
	{"ReserveSink", "STReserveSink.sol/STReserveSink.json", true, "", []string{"netuid", "reserveHotkey", "selfColdkey", "bootstrap"}, []string{"src/STReserveSink.sol"}},
	{"SettlementVault", "STSettlementVault.sol/STSettlementVault.json", true, "", []string{"netuid", "escrowHotkey", "selfColdkey", "minimumClaimTTLBlocks", "minimumTransferTaoRao", "bootstrap"}, []string{"src/STSettlementVault.sol"}},
	{"Coordinator", "STCoordinator.sol/STCoordinator.json", true, "", []string{"__self"}, []string{"lib/openzeppelin-contracts/contracts/proxy/utils/UUPSUpgradeable.sol"}},
	{"ERC1967Proxy", "ERC1967Proxy.sol/ERC1967Proxy.json", true, "", nil, nil},
	{"CoordinatorAdversary", "STCoordinatorAdversary.sol/STCoordinatorAdversary.json", false, "TestnetGovernanceDrillArtifact", []string{"__self"}, []string{"lib/openzeppelin-contracts/contracts/proxy/utils/UUPSUpgradeable.sol"}},
	{"SubnetProbe", "STSubnetProbe.sol/STSubnetProbe.json", false, "TestnetPrecompileProbeArtifact", []string{"owner", "netuid"}, []string{"src/probe/STSubnetProbe.sol"}},
	{"FleetBatcher", "STFleetBatcher.sol/STFleetBatcher.json", false, "TestnetFleetBatcherArtifact", []string{"coordinator", "oracle"}, []string{"src/STFleetBatcher.sol"}},
}

// Foundry includes compilation-unit AST ids in storage entries and Solidity
// type identifiers. Those ids change when an unrelated source is added, even
// though every slot/type is identical. Remove only that compiler bookkeeping;
// retain labels, slots, offsets, lengths, encodings, and nested member shapes.
func normalizeFoundryStorageLayout(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, child := range typed {
			if key == "astId" || key == "contract" {
				continue
			}
			out[foundryTypeASTID.ReplaceAllString(key, "$1")] = normalizeFoundryStorageLayout(child)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i := range typed {
			out[i] = normalizeFoundryStorageLayout(typed[i])
		}
		return out
	case string:
		return foundryTypeASTID.ReplaceAllString(typed, "$1")
	default:
		return value
	}
}

func canonicalArtifactHash(a artifact, immutableReferences map[string][]int) string {
	return canonicalArtifactHashForBytecode(a, immutableReferences, a.Bytecode.Object, a.DeployedBytecode.Object)
}

func canonicalArtifactHashForBytecode(a artifact, immutableReferences map[string][]int, creation, runtime string) string {
	if strings.HasPrefix(a.Bytecode.Object, "0x") && !strings.HasPrefix(creation, "0x") {
		creation = "0x" + creation
	}
	if strings.HasPrefix(a.DeployedBytecode.Object, "0x") && !strings.HasPrefix(runtime, "0x") {
		runtime = "0x" + runtime
	}
	decode := func(raw json.RawMessage) any {
		if len(raw) == 0 {
			return nil
		}
		var value any
		must(json.Unmarshal(raw, &value))
		return value
	}
	projection := struct {
		ABI                 any               `json:"abi"`
		CreationBytecode    string            `json:"creationBytecode"`
		RuntimeBytecode     string            `json:"runtimeBytecode"`
		ImmutableReferences map[string][]int  `json:"immutableReferences"`
		MethodIdentifiers   map[string]string `json:"methodIdentifiers"`
		StorageLayout       any               `json:"storageLayout"`
	}{
		ABI: decode(a.ABI), CreationBytecode: creation, RuntimeBytecode: runtime,
		ImmutableReferences: immutableReferences, MethodIdentifiers: a.MethodIdentifiers,
		StorageLayout: normalizeFoundryStorageLayout(decode(a.StorageLayout)),
	}
	canonical, err := json.Marshal(projection)
	must(err)
	sum := sha256.Sum256(canonical)
	return "0x" + hex.EncodeToString(sum[:])
}

func main() {
	if len(os.Args) == 4 && os.Args[1] == "--check" {
		must(checkGeneratedContracts(os.Args[2], os.Args[3]))
		return
	}
	root := "evm/out"
	out := "sim-testnet/contracts_gen.go"
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	if len(os.Args) > 2 {
		out = os.Args[2]
	}
	formatted, err := renderContractArtifacts(loadContractItems(root))
	must(err)
	must(os.WriteFile(out, formatted, 0o644))
}

func loadContractItems(root string) []item {
	sourceRoot := filepath.Dir(root)
	items := make([]item, 0, len(artifactDefinitions))
	for _, d := range artifactDefinitions {
		validateImmutableSourceOrder(sourceRoot, d)
		path := filepath.Join(root, d.path)
		raw, err := os.ReadFile(path)
		must(err)
		var a artifact
		must(json.Unmarshal(raw, &a))
		abi, err := json.Marshal(a.ABI)
		must(err)
		creation := trim0x(a.Bytecode.Object)
		runtime := trim0x(a.DeployedBytecode.Object)
		if creation == "" || runtime == "" {
			panic("empty bytecode in " + path)
		}
		rb, err := hex.DecodeString(runtime)
		must(err)
		rh := crypto.Keccak256Hash(rb)
		var layout any
		if len(a.StorageLayout) == 0 {
			// Some dependency artifacts (notably ERC1967Proxy) omit the
			// optional output entirely. Hash canonical JSON null so absence is
			// explicit and still changes if a future compiler emits a layout.
			layout = nil
		} else {
			must(json.Unmarshal(a.StorageLayout, &layout))
			layout = normalizeFoundryStorageLayout(layout)
		}
		canonicalLayout, err := json.Marshal(layout)
		must(err)
		lh := sha256.Sum256(canonicalLayout)
		refKeys := make([]string, 0, len(a.DeployedBytecode.ImmutableReferences))
		for k := range a.DeployedBytecode.ImmutableReferences {
			refKeys = append(refKeys, k)
		}
		sort.Slice(refKeys, func(i, j int) bool {
			left, err := strconv.ParseUint(refKeys[i], 10, 64)
			must(err)
			right, err := strconv.ParseUint(refKeys[j], 10, 64)
			must(err)
			return left < right
		})
		if len(refKeys) != len(d.immutableNames) {
			panic(fmt.Sprintf("%s: artifact has %d immutable ids, source contract expects %d", d.name, len(refKeys), len(d.immutableNames)))
		}
		refs := make(map[string][]int, len(refKeys))
		for i, k := range refKeys {
			var offsets []int
			for _, ref := range a.DeployedBytecode.ImmutableReferences[k] {
				if ref.Length != 32 {
					panic("immutable reference is not 32 bytes")
				}
				offsets = append(offsets, ref.Start)
			}
			sort.Ints(offsets)
			refs[d.immutableNames[i]] = offsets
		}
		// Foundry's top-level artifact contains non-semantic fields such as a
		// compilation-order id, compiler AST ids, and JSON whitespace/key
		// order. Hash the canonical deployment-relevant projection instead:
		// ABI, creation/runtime bytecode, semantically named immutable offsets,
		// method identifiers, and storage layout. The expanded metadata object
		// is intentionally excluded because Foundry can vary it with the
		// compilation graph even when emitted bytecode is identical. Exact
		// source and compiler settings have independent release-lock hashes,
		// while the bytecode still contains Solidity's metadata suffix.
		artifactHash := canonicalArtifactHash(a, refs)
		items = append(items, item{
			Name: d.name, Path: path, ABI: string(abi), Creation: creation, Runtime: runtime,
			RuntimeHash: rh.Hex(), ArtifactHash: artifactHash, StorageLayoutHash: "sha256:" + hex.EncodeToString(lh[:]),
			References: refs, Release: d.release, Variable: d.variable, Artifact: a,
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items
}

func renderContractArtifacts(items []item) ([]byte, error) {
	var b strings.Builder
	b.WriteString("// Code generated by go generate; DO NOT EDIT.\npackage main\n\n")
	b.WriteString("//go:generate go run ./gencontracts ../evm/out contracts_gen.go\n\n")
	b.WriteString("type ContractArtifact struct { Name, ABI, CreationBytecode, RuntimeBytecode, RuntimeBytecodeHash, FoundryArtifactHash, StorageLayoutHash string; ImmutableReferences map[string][]int }\n\n")
	for _, v := range items {
		fmt.Fprintf(&b, "const %sABI = `%s`\nconst %sCreationBytecode = \"%s\"\nconst %sRuntimeBytecode = \"%s\"\nconst %sRuntimeBytecodeHash = \"%s\"\nconst %sFoundryArtifactHash = \"%s\"\nconst %sStorageLayoutHash = \"%s\"\n\n", v.Name, v.ABI, v.Name, v.Creation, v.Name, v.Runtime, v.Name, v.RuntimeHash, v.Name, v.ArtifactHash, v.Name, v.StorageLayoutHash)
	}
	b.WriteString("var ReleaseContractArtifacts = []ContractArtifact{\n")
	for _, v := range items {
		if !v.Release {
			continue
		}
		fmt.Fprintf(&b, "{Name: %q, ABI: %sABI, CreationBytecode: %sCreationBytecode, RuntimeBytecode: %sRuntimeBytecode, RuntimeBytecodeHash: %sRuntimeBytecodeHash, FoundryArtifactHash: %sFoundryArtifactHash, StorageLayoutHash: %sStorageLayoutHash, ImmutableReferences: %#v},\n", v.Name, v.Name, v.Name, v.Name, v.Name, v.Name, v.Name, v.References)
	}
	b.WriteString("}\n")
	for _, v := range items {
		if !v.Release {
			if v.Variable == "" {
				panic("testnet-only artifact has no generated variable: " + v.Name)
			}
			fmt.Fprintf(&b, "\nvar %s = ContractArtifact{Name: %q, ABI: %sABI, CreationBytecode: %sCreationBytecode, RuntimeBytecode: %sRuntimeBytecode, RuntimeBytecodeHash: %sRuntimeBytecodeHash, FoundryArtifactHash: %sFoundryArtifactHash, StorageLayoutHash: %sStorageLayoutHash, ImmutableReferences: %#v}\n", v.Variable, v.Name, v.Name, v.Name, v.Name, v.Name, v.Name, v.Name, v.References)
		}
	}
	formatted, err := format.Source([]byte(b.String()))
	return formatted, err
}

const (
	solidityMetadataPrefix = "a2646970667358221220"
	solidityMetadataSuffix = "64736f6c63430008180033"
)

// normalizeSolidityMetadataDigest removes only the graph-sensitive IPFS digest
// from Solidity 0.8.24's canonical CBOR trailer. The executable bytes, CBOR
// shape, hash function marker, compiler version and trailer length all remain
// comparison inputs. This lets a clean full-project build authenticate the
// exact deployment payload even when Foundry chooses a different compilation
// graph for otherwise identical source and settings.
func normalizeSolidityMetadataDigest(encoded string) (string, error) {
	raw, err := hex.DecodeString(trim0x(encoded))
	if err != nil {
		return "", fmt.Errorf("decode bytecode: %w", err)
	}
	prefix, _ := hex.DecodeString(solidityMetadataPrefix)
	suffix, _ := hex.DecodeString(solidityMetadataSuffix)
	metadataLength := len(prefix) + sha256.Size + len(suffix)
	if len(raw) < metadataLength {
		return "", errors.New("bytecode has no complete Solidity 0.8.24 metadata trailer")
	}
	start := len(raw) - metadataLength
	if !bytes.Equal(raw[start:start+len(prefix)], prefix) || !bytes.Equal(raw[len(raw)-len(suffix):], suffix) {
		return "", errors.New("bytecode has an unexpected Solidity metadata trailer")
	}
	result := append([]byte(nil), raw...)
	digestStart := start + len(prefix)
	clear(result[digestStart : digestStart+sha256.Size])
	return hex.EncodeToString(result), nil
}

func generatedStringConstants(path string, source []byte) (map[string]string, error) {
	parsed, err := parser.ParseFile(token.NewFileSet(), path, source, 0)
	if err != nil {
		return nil, err
	}
	result := map[string]string{}
	for _, declaration := range parsed.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.CONST {
			continue
		}
		for _, specification := range general.Specs {
			values, ok := specification.(*ast.ValueSpec)
			if !ok || len(values.Names) != len(values.Values) {
				continue
			}
			for index, name := range values.Names {
				literal, ok := values.Values[index].(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					continue
				}
				value, unquoteErr := strconv.Unquote(literal.Value)
				if unquoteErr != nil {
					return nil, fmt.Errorf("decode generated constant %s: %w", name.Name, unquoteErr)
				}
				if _, duplicate := result[name.Name]; duplicate {
					return nil, fmt.Errorf("duplicate generated constant %s", name.Name)
				}
				result[name.Name] = value
			}
		}
	}
	return result, nil
}

func requiredGeneratedConstant(constants map[string]string, name string) (string, error) {
	value, ok := constants[name]
	if !ok {
		return "", fmt.Errorf("generated payload has no %s constant", name)
	}
	return value, nil
}

func replaceGeneratedConstant(source []byte, name, before, after string) ([]byte, error) {
	oldLine := []byte(fmt.Sprintf("const %s = %q", name, before))
	newLine := []byte(fmt.Sprintf("const %s = %q", name, after))
	if bytes.Count(source, oldLine) != 1 {
		return nil, fmt.Errorf("generated payload has an ambiguous %s declaration", name)
	}
	return bytes.Replace(source, oldLine, newLine, 1), nil
}

func normalizeGeneratedContractSource(path string, source []byte, items []item) ([]byte, error) {
	constants, err := generatedStringConstants(path, source)
	if err != nil {
		return nil, err
	}
	result := append([]byte(nil), source...)
	for _, contract := range items {
		for _, suffix := range []string{"CreationBytecode", "RuntimeBytecode"} {
			name := contract.Name + suffix
			value, err := requiredGeneratedConstant(constants, name)
			if err != nil {
				return nil, err
			}
			normalized, err := normalizeSolidityMetadataDigest(value)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", name, err)
			}
			result, err = replaceGeneratedConstant(result, name, value, normalized)
			if err != nil {
				return nil, err
			}
		}
		for _, suffix := range []string{"RuntimeBytecodeHash", "FoundryArtifactHash"} {
			name := contract.Name + suffix
			value, err := requiredGeneratedConstant(constants, name)
			if err != nil {
				return nil, err
			}
			result, err = replaceGeneratedConstant(result, name, value, "0x"+strings.Repeat("0", 64))
			if err != nil {
				return nil, err
			}
		}
	}
	return result, nil
}

func checkGeneratedContracts(root, existingPath string) error {
	items := loadContractItems(root)
	expected, err := renderContractArtifacts(items)
	if err != nil {
		return err
	}
	existing, err := os.ReadFile(existingPath)
	if err != nil {
		return err
	}
	constants, err := generatedStringConstants(existingPath, existing)
	if err != nil {
		return err
	}
	for _, contract := range items {
		creation, err := requiredGeneratedConstant(constants, contract.Name+"CreationBytecode")
		if err != nil {
			return err
		}
		runtime, err := requiredGeneratedConstant(constants, contract.Name+"RuntimeBytecode")
		if err != nil {
			return err
		}
		lockedRuntimeHash, err := requiredGeneratedConstant(constants, contract.Name+"RuntimeBytecodeHash")
		if err != nil {
			return err
		}
		runtimeBytes, err := hex.DecodeString(trim0x(runtime))
		if err != nil || !strings.EqualFold(crypto.Keccak256Hash(runtimeBytes).Hex(), lockedRuntimeHash) {
			return fmt.Errorf("%s locked runtime hash does not authenticate its bytecode", contract.Name)
		}
		lockedArtifactHash, err := requiredGeneratedConstant(constants, contract.Name+"FoundryArtifactHash")
		if err != nil {
			return err
		}
		if got := canonicalArtifactHashForBytecode(contract.Artifact, contract.References, creation, runtime); !strings.EqualFold(got, lockedArtifactHash) {
			return fmt.Errorf("%s locked artifact hash does not match the rebuilt ABI, selectors, layout, references and locked bytecode", contract.Name)
		}
	}
	normalizedExisting, err := normalizeGeneratedContractSource(existingPath, existing, items)
	if err != nil {
		return err
	}
	normalizedExpected, err := normalizeGeneratedContractSource("generated-contracts.go", expected, items)
	if err != nil {
		return err
	}
	if !bytes.Equal(normalizedExisting, normalizedExpected) {
		return errors.New("generated contract payload differs outside Solidity's graph-sensitive IPFS metadata digest")
	}
	if bytes.Equal(existing, expected) {
		fmt.Println("generated contract payload exactly matches the reviewed Foundry output")
	} else {
		fmt.Println("generated contract payload is semantically current; graph-sensitive Solidity metadata differs and the locked deployment bytes are preserved")
	}
	return nil
}

func validateImmutableSourceOrder(root string, d artifactDefinition) {
	var names []string
	for _, source := range d.immutableSources {
		raw, err := os.ReadFile(filepath.Join(root, source))
		must(err)
		for _, match := range immutableDeclaration.FindAllSubmatch(raw, -1) {
			names = append(names, string(match[1]))
		}
	}
	if strings.Join(names, "\x00") != strings.Join(d.immutableNames, "\x00") {
		panic(fmt.Sprintf("%s: immutable source order %q does not match generator contract %q", d.name, names, d.immutableNames))
	}
}
func trim0x(s string) string { return strings.TrimPrefix(s, "0x") }
func must(err error) {
	if err != nil {
		panic(err)
	}
}
