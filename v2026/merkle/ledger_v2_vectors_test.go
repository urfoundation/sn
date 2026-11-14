package merkle

// Shared Go↔Solidity vectors for the ledger sum tree
// `urnetwork-ledger-sum-tree-v1` (VERSION2-FINAL.md §6.5).
//
// testdata/ledger_v2_vectors.json is consumed by:
//   - this package: TestLedgerVectorsFileUpToDate regenerates it in memory
//     and requires byte equality, and TestLedgerVectorsFileRoundTrip checks
//     the committed file against the Go implementation;
//   - evm/test/V2LedgerTreeVectors.t.sol, against LedgerTree.sol (T-1, T-2,
//     T-4, X-1, X-2).
//
// Accounts derive from keccak256 of a constant tag (ledgerDerivedAccounts),
// never from time or math/rand, so regeneration is bit-identical.
//
// The layout suits Foundry's JSON cheatcodes. Every object's keys are in
// alphabetical order, the order vm.parseJson decodes struct fields in.
// Every uint256 (values, sums, totals, account indices) is a decimal
// string: vm.parseJson infers types from the text, reading a short decimal
// string as a string and a long one as a number, so the Solidity side
// decodes with vm.parseJsonType and explicit type descriptions, which
// coerce by the declared type. Counts below 2^30 (leafCount, depth, level,
// fault) are JSON numbers. Hashes and coldkeys are 0x-prefixed 32-byte hex.
// Empty proofs and paths are [].
//
//	depths             [{depth, leafCount}]: LedgerTreeDepth, LedgerTree.depth
//	faultCases         [{account, error, fault, leafCount, level, name, path, root, total}]:
//	                   WalkLedgerPath and LedgerTree.walkPath of account in the
//	                   tree (root, total, leafCount); fault is the LedgerTree.Fault
//	                   ordinal, and error is "", "MalformedPath" or "InvalidLeafCount"
//	invalidLeafCounts  leaf counts that both refuse
//	leafHashes         [{coldkey, hash, value}]
//	nodeHashes         [{hash, leftHash, leftSum, rightHash, rightSum}]
//	proofCases         [{account, error, leafCount, name, proof, root, total, valid}]:
//	                   VerifyLedgerProof and LedgerTree.verifyLeaf; error is "",
//	                   "InvalidLeafCount" or "ArithmeticOverflow", where verifyLeaf
//	                   reverts and VerifyLedgerProof returns false
//	scheme             "urnetwork-ledger-sum-tree-v1"
//	trees              [{accounts, depth, leafCount, name, paths, proofs, root, total}]:
//	                   proofs[i] and paths[i] are account i's claim proof
//	                   (bottom-up) and opening (root first)
//
// An account is {coldkey, cumulative, index}, a proof node {hash, sum} and a
// path node {leftHash, leftSum, rightHash, rightSum}, as in LedgerTree.sol.
//
// Run `go test ./merkle -run TestLedgerVectorsFileUpToDate -update` to
// rewrite the file after an intentional change, which LedgerTree.sol must
// make identically.

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strconv"
	"testing"

	"github.com/holiman/uint256"
)

const ledgerVectorsPath = "testdata/ledger_v2_vectors.json"

// Struct fields are declared in alphabetical order of their JSON keys.
type ledgerVectorFile struct {
	Depths            []ledgerVectorDepth    `json:"depths"`
	FaultCases        []ledgerVectorFault    `json:"faultCases"`
	InvalidLeafCounts []uint64               `json:"invalidLeafCounts"`
	LeafHashes        []ledgerVectorLeafHash `json:"leafHashes"`
	NodeHashes        []ledgerVectorNodeHash `json:"nodeHashes"`
	ProofCases        []ledgerVectorProof    `json:"proofCases"`
	Scheme            string                 `json:"scheme"`
	Trees             []ledgerVectorTree     `json:"trees"`
}

type ledgerVectorDepth struct {
	Depth     int    `json:"depth"`
	LeafCount uint64 `json:"leafCount"`
}

type ledgerVectorLeafHash struct {
	Coldkey string `json:"coldkey"`
	Hash    string `json:"hash"`
	Value   string `json:"value"`
}

type ledgerVectorNodeHash struct {
	Hash      string `json:"hash"`
	LeftHash  string `json:"leftHash"`
	LeftSum   string `json:"leftSum"`
	RightHash string `json:"rightHash"`
	RightSum  string `json:"rightSum"`
}

type ledgerVectorAccount struct {
	Coldkey    string `json:"coldkey"`
	Cumulative string `json:"cumulative"`
	Index      string `json:"index"`
}

type ledgerVectorNode struct {
	Hash string `json:"hash"`
	Sum  string `json:"sum"`
}

type ledgerVectorPathNode struct {
	LeftHash  string `json:"leftHash"`
	LeftSum   string `json:"leftSum"`
	RightHash string `json:"rightHash"`
	RightSum  string `json:"rightSum"`
}

type ledgerVectorTree struct {
	Accounts  []ledgerVectorAccount    `json:"accounts"`
	Depth     int                      `json:"depth"`
	LeafCount uint64                   `json:"leafCount"`
	Name      string                   `json:"name"`
	Paths     [][]ledgerVectorPathNode `json:"paths"`
	Proofs    [][]ledgerVectorNode     `json:"proofs"`
	Root      string                   `json:"root"`
	Total     string                   `json:"total"`
}

type ledgerVectorProof struct {
	Account   ledgerVectorAccount `json:"account"`
	Error     string              `json:"error"`
	LeafCount uint64              `json:"leafCount"`
	Name      string              `json:"name"`
	Proof     []ledgerVectorNode  `json:"proof"`
	Root      string              `json:"root"`
	Total     string              `json:"total"`
	Valid     bool                `json:"valid"`
}

type ledgerVectorFault struct {
	Account   ledgerVectorAccount    `json:"account"`
	Error     string                 `json:"error"`
	Fault     uint8                  `json:"fault"`
	LeafCount uint64                 `json:"leafCount"`
	Level     int                    `json:"level"`
	Name      string                 `json:"name"`
	Path      []ledgerVectorPathNode `json:"path"`
	Root      string                 `json:"root"`
	Total     string                 `json:"total"`
}

// ledgerVectorTreeAccounts are the accounts of vector tree nN. Tree n9 pins
// the value extremes: account 0 holds 0 and account 8 holds 2^128 − 1.
func ledgerVectorTreeAccounts(n int) []LedgerAccount {
	accounts := ledgerDerivedAccounts("n"+strconv.Itoa(n), n)
	if n == 9 {
		accounts[0].Cumulative.Clear()
		accounts[8].Cumulative.SubUint64(ledgerValueLimit, 1)
	}
	return accounts
}

func ledgerVectorAccountOf(index uint64, a LedgerAccount) ledgerVectorAccount {
	return ledgerVectorAccount{Coldkey: hex32(a.Coldkey), Cumulative: a.Cumulative.Dec(), Index: strconv.FormatUint(index, 10)}
}

func ledgerVectorProofOf(proof []LedgerNode) []ledgerVectorNode {
	out := make([]ledgerVectorNode, len(proof))
	for k, node := range proof {
		out[k] = ledgerVectorNode{Hash: hex32(node.Hash), Sum: node.Sum.Dec()}
	}
	return out
}

func ledgerVectorPathOf(path []LedgerPathNode) []ledgerVectorPathNode {
	out := make([]ledgerVectorPathNode, len(path))
	for i, node := range path {
		out[i] = ledgerVectorPathNode{
			LeftHash: hex32(node.LeftHash), LeftSum: node.LeftSum.Dec(),
			RightHash: hex32(node.RightHash), RightSum: node.RightSum.Dec(),
		}
	}
	return out
}

// ledgerVectorError names the LedgerTree revert that err stands for.
func ledgerVectorError(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrLedgerMalformedPath):
		return "MalformedPath"
	case errors.Is(err, ErrLedgerLeafCount):
		return "InvalidLeafCount"
	default:
		return err.Error()
	}
}

func buildLedgerVectors(t *testing.T) ledgerVectorFile {
	t.Helper()
	file := ledgerVectorFile{Scheme: LedgerSumTreeScheme, InvalidLeafCounts: []uint64{0, MaxLedgerLeafCount + 1}}
	for _, c := range ledgerDepthCases {
		file.Depths = append(file.Depths, ledgerVectorDepth{Depth: c.depth, LeafCount: c.leafCount})
	}

	var zero, ones [32]byte
	for i := range ones {
		ones[i] = 0xff
	}
	var none uint256.Int
	all := new(uint256.Int).SetAllOne()
	derived := ledgerDerivedAccounts("hashes", 4)
	for _, a := range []LedgerAccount{
		{Coldkey: zero},
		{Coldkey: zero, Cumulative: *uint256.NewInt(1)},
		{Coldkey: ones, Cumulative: *new(uint256.Int).SubUint64(ledgerValueLimit, 1)},
		{Coldkey: ones, Cumulative: *all},
		derived[0],
		{Coldkey: derived[1].Coldkey, Cumulative: *new(uint256.Int).Lsh(uint256.NewInt(1), 64)},
	} {
		file.LeafHashes = append(file.LeafHashes, ledgerVectorLeafHash{
			Coldkey: hex32(a.Coldkey), Hash: hex32(LedgerLeafHash(a.Coldkey, &a.Cumulative)), Value: a.Cumulative.Dec(),
		})
	}
	padding := LedgerNodeHash(zero, &none, zero, &none)
	for _, node := range []LedgerPathNode{
		{},
		{LeftHash: padding, RightHash: padding},
		{
			LeftHash: LedgerLeafHash(derived[0].Coldkey, &derived[0].Cumulative), LeftSum: derived[0].Cumulative,
			RightHash: LedgerLeafHash(derived[1].Coldkey, &derived[1].Cumulative), RightSum: derived[1].Cumulative,
		},
		{LeftHash: derived[2].Coldkey, LeftSum: *all, RightHash: derived[3].Coldkey},
		{LeftHash: ones, RightHash: zero, RightSum: *new(uint256.Int).Lsh(uint256.NewInt(1), 157)},
	} {
		file.NodeHashes = append(file.NodeHashes, ledgerVectorNodeHash{
			Hash:     hex32(LedgerNodeHash(node.LeftHash, &node.LeftSum, node.RightHash, &node.RightSum)),
			LeftHash: hex32(node.LeftHash), LeftSum: node.LeftSum.Dec(),
			RightHash: hex32(node.RightHash), RightSum: node.RightSum.Dec(),
		})
	}

	for _, n := range []int{1, 2, 3, 4, 5, 8, 9, 17} {
		accounts := ledgerVectorTreeAccounts(n)
		tree := mustLedgerTree(t, accounts)
		root, total := tree.Root()
		vector := ledgerVectorTree{
			Name: "n" + strconv.Itoa(n), Depth: tree.Depth(), LeafCount: tree.LeafCount(), Root: hex32(root), Total: total.Dec(),
		}
		for i := range accounts {
			proof, err := tree.Proof(uint64(i))
			if err != nil {
				t.Fatal(err)
			}
			path, err := tree.Path(uint64(i))
			if err != nil {
				t.Fatal(err)
			}
			vector.Accounts = append(vector.Accounts, ledgerVectorAccountOf(uint64(i), accounts[i]))
			vector.Proofs = append(vector.Proofs, ledgerVectorProofOf(proof))
			vector.Paths = append(vector.Paths, ledgerVectorPathOf(path))
		}
		file.Trees = append(file.Trees, vector)
	}

	for _, c := range ledgerProofCases() {
		file.ProofCases = append(file.ProofCases, ledgerVectorProof{
			Account: ledgerVectorAccountOf(c.index, c.account), Error: c.err, LeafCount: c.leafCount, Name: c.name,
			Proof: ledgerVectorProofOf(c.proof), Root: hex32(c.root), Total: c.total.Dec(), Valid: c.valid,
		})
	}
	for _, c := range ledgerFaultCases() {
		file.FaultCases = append(file.FaultCases, ledgerVectorFault{
			Account: ledgerVectorAccountOf(c.index, c.account), Error: ledgerVectorError(c.err), Fault: uint8(c.fault),
			LeafCount: c.leafCount, Level: c.level, Name: c.name, Path: ledgerVectorPathOf(c.path), Root: hex32(c.root),
			Total: c.total.Dec(),
		})
	}
	return file
}

// X-1, X-2: regenerates every vector in memory and requires byte equality
// with the committed file, so any drift in the encodings, the tree shape,
// proofs, paths or fault reporting fails here and in LedgerTree.sol's tests.
func TestLedgerVectorsFileUpToDate(t *testing.T) {
	got, err := json.MarshalIndent(buildLedgerVectors(t), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	if *update {
		if err := os.WriteFile(ledgerVectorsPath, got, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("rewrote %s (%d bytes)", ledgerVectorsPath, len(got))
		return
	}
	want, err := os.ReadFile(ledgerVectorsPath)
	if err != nil {
		t.Fatalf("%v (generate it with: go test ./merkle -run TestLedgerVectorsFileUpToDate -update)", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("regenerated vectors differ from %s: the ledger tree drifted; if intentional "+
			"(with LedgerTree.sol changed identically), rerun with -update", ledgerVectorsPath)
	}
}

func mustLedgerUint(t *testing.T, s string) *uint256.Int {
	t.Helper()
	v, err := uint256.FromDecimal(s)
	if err != nil {
		t.Fatalf("bad decimal %q: %v", s, err)
	}
	return v
}

func mustLedgerAccount(t *testing.T, v ledgerVectorAccount) (uint64, LedgerAccount) {
	t.Helper()
	index, err := strconv.ParseUint(v.Index, 10, 64)
	if err != nil {
		t.Fatalf("bad index %q: %v", v.Index, err)
	}
	return index, LedgerAccount{Coldkey: mustHex32(t, v.Coldkey), Cumulative: *mustLedgerUint(t, v.Cumulative)}
}

func mustLedgerProof(t *testing.T, v []ledgerVectorNode) []LedgerNode {
	t.Helper()
	proof := make([]LedgerNode, len(v))
	for k, node := range v {
		proof[k] = LedgerNode{Hash: mustHex32(t, node.Hash), Sum: *mustLedgerUint(t, node.Sum)}
	}
	return proof
}

func mustLedgerPath(t *testing.T, v []ledgerVectorPathNode) []LedgerPathNode {
	t.Helper()
	path := make([]LedgerPathNode, len(v))
	for i, node := range v {
		path[i] = LedgerPathNode{
			LeftHash: mustHex32(t, node.LeftHash), LeftSum: *mustLedgerUint(t, node.LeftSum),
			RightHash: mustHex32(t, node.RightHash), RightSum: *mustLedgerUint(t, node.RightSum),
		}
	}
	return path
}

// X-1, X-2: the committed file, read the way an external implementation
// reads it, agrees with the Go implementation: hashes, depths, every tree
// rebuilt from its accounts with every proof and path, and every case.
func TestLedgerVectorsFileRoundTrip(t *testing.T) {
	raw, err := os.ReadFile(ledgerVectorsPath)
	if err != nil {
		t.Fatalf("%v (generate it with: go test ./merkle -run TestLedgerVectorsFileUpToDate -update)", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var file ledgerVectorFile
	if err := decoder.Decode(&file); err != nil {
		t.Fatal(err)
	}
	if file.Scheme != LedgerSumTreeScheme || len(file.Trees) != 8 || len(file.ProofCases) == 0 || len(file.FaultCases) == 0 {
		t.Fatalf("scheme %q with %d trees, %d proof cases and %d fault cases", file.Scheme, len(file.Trees), len(file.ProofCases), len(file.FaultCases))
	}
	for _, v := range file.LeafHashes {
		if got := hex32(LedgerLeafHash(mustHex32(t, v.Coldkey), mustLedgerUint(t, v.Value))); got != v.Hash {
			t.Fatalf("leafHash(%s, %s) = %s, want %s", v.Coldkey, v.Value, got, v.Hash)
		}
	}
	for _, v := range file.NodeHashes {
		got := LedgerNodeHash(mustHex32(t, v.LeftHash), mustLedgerUint(t, v.LeftSum), mustHex32(t, v.RightHash), mustLedgerUint(t, v.RightSum))
		if hex32(got) != v.Hash {
			t.Fatalf("nodeHash of %+v = %s", v, hex32(got))
		}
	}
	for _, v := range file.Depths {
		if d, err := LedgerTreeDepth(v.LeafCount); err != nil || d != v.Depth {
			t.Fatalf("LedgerTreeDepth(%d) = %d, %v; want %d", v.LeafCount, d, err, v.Depth)
		}
	}
	for _, leafCount := range file.InvalidLeafCounts {
		if _, err := LedgerTreeDepth(leafCount); !errors.Is(err, ErrLedgerLeafCount) {
			t.Fatalf("LedgerTreeDepth(%d) err = %v", leafCount, err)
		}
	}
	for _, v := range file.Trees {
		accounts := make([]LedgerAccount, len(v.Accounts))
		for i, va := range v.Accounts {
			index, a := mustLedgerAccount(t, va)
			if index != uint64(i) {
				t.Fatalf("%s: account %d has index %d", v.Name, i, index)
			}
			accounts[i] = a
		}
		tree := mustLedgerTree(t, accounts)
		root, total := tree.Root()
		if hex32(root) != v.Root || total.Dec() != v.Total || tree.Depth() != v.Depth || tree.LeafCount() != v.LeafCount {
			t.Fatalf("%s: rebuilt root %s total %s depth %d", v.Name, hex32(root), total.Dec(), tree.Depth())
		}
		if len(v.Proofs) != len(accounts) || len(v.Paths) != len(accounts) {
			t.Fatalf("%s: %d proofs and %d paths for %d accounts", v.Name, len(v.Proofs), len(v.Paths), len(accounts))
		}
		for i, a := range accounts {
			index := uint64(i)
			proof, path := mustLedgerProof(t, v.Proofs[i]), mustLedgerPath(t, v.Paths[i])
			built, _ := tree.Proof(index)
			opened, _ := tree.Path(index)
			if !slices.Equal(proof, built) || !slices.Equal(path, opened) {
				t.Fatalf("%s: account %d's proof or path is not the rebuilt tree's", v.Name, i)
			}
			if !VerifyLedgerProof(root, &total, v.LeafCount, index, a.Coldkey, &a.Cumulative, proof) {
				t.Fatalf("%s: account %d's proof does not verify", v.Name, i)
			}
			if fault, level, err := WalkLedgerPath(root, &total, v.LeafCount, index, a.Coldkey, &a.Cumulative, path); err != nil || fault != LedgerFaultNone || level != 0 {
				t.Fatalf("%s: account %d's path: fault %v level %d err %v", v.Name, i, fault, level, err)
			}
		}
	}
	for _, v := range file.ProofCases {
		index, a := mustLedgerAccount(t, v.Account)
		got := VerifyLedgerProof(mustHex32(t, v.Root), mustLedgerUint(t, v.Total), v.LeafCount, index, a.Coldkey, &a.Cumulative, mustLedgerProof(t, v.Proof))
		if got != (v.Valid && v.Error == "") {
			t.Fatalf("proof case %s: VerifyLedgerProof = %v", v.Name, got)
		}
	}
	for _, v := range file.FaultCases {
		index, a := mustLedgerAccount(t, v.Account)
		fault, level, err := WalkLedgerPath(mustHex32(t, v.Root), mustLedgerUint(t, v.Total), v.LeafCount, index, a.Coldkey, &a.Cumulative, mustLedgerPath(t, v.Path))
		if ledgerVectorError(err) != v.Error || uint8(fault) != v.Fault || level != v.Level {
			t.Fatalf("fault case %s: fault %v level %d err %v; want %d at %d, %q", v.Name, fault, level, err, v.Fault, v.Level, v.Error)
		}
	}
}
