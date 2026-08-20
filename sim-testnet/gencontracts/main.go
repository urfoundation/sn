// Command gencontracts converts reviewed Foundry artifacts into the committed
// Go deployment payload used by sim-testnet. It never compiles Solidity.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
		ABI: decode(a.ABI), CreationBytecode: a.Bytecode.Object, RuntimeBytecode: a.DeployedBytecode.Object,
		ImmutableReferences: immutableReferences, MethodIdentifiers: a.MethodIdentifiers,
		StorageLayout: normalizeFoundryStorageLayout(decode(a.StorageLayout)),
	}
	canonical, err := json.Marshal(projection)
	must(err)
	sum := sha256.Sum256(canonical)
	return "0x" + hex.EncodeToString(sum[:])
}

func main() {
	root := "evm/out"
	out := "sim-testnet/contracts_gen.go"
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	if len(os.Args) > 2 {
		out = os.Args[2]
	}
	defs := []artifactDefinition{
		{"ReserveSink", "STReserveSink.sol/STReserveSink.json", true, "", []string{"netuid", "reserveHotkey", "selfColdkey", "bootstrap"}, []string{"src/STReserveSink.sol"}},
		{"SettlementVault", "STSettlementVault.sol/STSettlementVault.json", true, "", []string{"netuid", "escrowHotkey", "selfColdkey", "minimumClaimTTLBlocks", "bootstrap"}, []string{"src/STSettlementVault.sol"}},
		{"Coordinator", "STCoordinator.sol/STCoordinator.json", true, "", []string{"__self"}, []string{"lib/openzeppelin-contracts/contracts/proxy/utils/UUPSUpgradeable.sol"}},
		{"ERC1967Proxy", "ERC1967Proxy.sol/ERC1967Proxy.json", true, "", nil, nil},
		{"CoordinatorAdversary", "STCoordinatorAdversary.sol/STCoordinatorAdversary.json", false, "TestnetGovernanceDrillArtifact", []string{"__self"}, []string{"lib/openzeppelin-contracts/contracts/proxy/utils/UUPSUpgradeable.sol"}},
		{"SubnetProbe", "STSubnetProbe.sol/STSubnetProbe.json", false, "TestnetPrecompileProbeArtifact", []string{"owner", "netuid"}, []string{"src/probe/STSubnetProbe.sol"}},
	}
	sourceRoot := filepath.Dir(root)
	items := make([]item, 0, len(defs))
	for _, d := range defs {
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
		items = append(items, item{d.name, path, string(abi), creation, runtime, rh.Hex(), artifactHash, "sha256:" + hex.EncodeToString(lh[:]), refs, d.release, d.variable})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
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
	must(os.WriteFile(out, []byte(b.String()), 0o644))
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
