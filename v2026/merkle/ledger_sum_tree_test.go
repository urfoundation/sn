package merkle

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/big"
	"math/bits"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/holiman/uint256"
)

func ledgerTestAccounts(n int) []LedgerAccount {
	accounts := make([]LedgerAccount, n)
	for i := range accounts {
		accounts[i].Coldkey[0] = byte(i + 1)
		accounts[i].Coldkey[31] = byte(i >> 8)
		accounts[i].Cumulative.SetUint64(uint64(1_000 + 7*i))
	}
	return accounts
}

// GT-2, GT-4 (smoke): every index of an honest tree proves, and its path
// shows no fault against itself.
func TestLedgerSumTreeRoundTrip(t *testing.T) {
	for _, n := range []int{1, 2, 3, 4, 5, 8, 9, 33} {
		accounts := ledgerTestAccounts(n)
		tree, err := BuildLedgerSumTree(context.Background(), accounts)
		if err != nil {
			t.Fatal(err)
		}
		root, total := tree.Root()
		var want uint256.Int
		for i := range accounts {
			want.Add(&want, &accounts[i].Cumulative)
		}
		if !total.Eq(&want) {
			t.Fatalf("n=%d total %s, want %s", n, total.Dec(), want.Dec())
		}
		for i := range accounts {
			proof, err := tree.Proof(uint64(i))
			if err != nil {
				t.Fatal(err)
			}
			if len(proof) != tree.Depth() {
				t.Fatalf("n=%d proof length %d, depth %d", n, len(proof), tree.Depth())
			}
			if !VerifyLedgerProof(root, &total, uint64(n), uint64(i), accounts[i].Coldkey, &accounts[i].Cumulative, proof) {
				t.Fatalf("n=%d index %d does not verify", n, i)
			}
			path, err := tree.Path(uint64(i))
			if err != nil {
				t.Fatal(err)
			}
			fault, level, err := WalkLedgerPath(root, &total, uint64(n), uint64(i), accounts[i].Coldkey, &accounts[i].Cumulative, path)
			if err != nil || fault != LedgerFaultNone || level != 0 {
				t.Fatalf("n=%d index %d: fault %v level %d err %v", n, i, fault, level, err)
			}
		}
	}
}

// --- accounts, an independent reference, and trees stated by hand ---

const ledgerVectorSeed = "urnetwork/sn/merkle ledger v2 vectors"

// ledgerDerivedAccounts derives n accounts from keccak256 of a constant tag,
// never from time or math/rand, so the vectors regenerate bit-identically:
//
//	coldkey_i = keccak256(seed + "|" + name + "|coldkey|" + itoa(i))
//	h_i       = keccak256(seed + "|" + name + "|value|" + itoa(i))
//	value_i   = the first 1 + h_i[31] % 16 bytes of h_i, big-endian (below 2^128)
func ledgerDerivedAccounts(name string, n int) []LedgerAccount {
	accounts := make([]LedgerAccount, n)
	for i := range accounts {
		accounts[i].Coldkey = keccak256([]byte(ledgerVectorSeed + "|" + name + "|coldkey|" + strconv.Itoa(i)))
		h := keccak256([]byte(ledgerVectorSeed + "|" + name + "|value|" + strconv.Itoa(i)))
		accounts[i].Cumulative.SetBytes(h[:1+h[31]%16])
	}
	return accounts
}

// ledgerSuccessor is an honest successor ledger of n accounts: account i
// keeps its coldkey and gains i, and new accounts are appended.
func ledgerSuccessor(accounts []LedgerAccount, n int) []LedgerAccount {
	next := ledgerDerivedAccounts("successor", n)
	for i := range accounts {
		next[i] = accounts[i]
		next[i].Cumulative.AddUint64(&next[i].Cumulative, uint64(i))
	}
	return next
}

func mustLedgerTree(t *testing.T, accounts []LedgerAccount) *LedgerSumTree {
	t.Helper()
	tree, err := BuildLedgerSumTree(context.Background(), accounts)
	if err != nil {
		t.Fatalf("BuildLedgerSumTree(%d accounts): %v", len(accounts), err)
	}
	return tree
}

// ledgerWord is v as a 32-byte big-endian word, through math/big.
func ledgerWord(v *uint256.Int) []byte {
	return v.ToBig().FillBytes(make([]byte, 32))
}

// naiveLedgerLeafHash and naiveLedgerNodeHash restate the §6.5 encodings
// byte by byte, independently of LedgerLeafHash and LedgerNodeHash.
func naiveLedgerLeafHash(coldkey [32]byte, value *uint256.Int) [32]byte {
	return keccak256([]byte{0x00}, coldkey[:], ledgerWord(value))
}

func naiveLedgerNodeHash(left, right LedgerNode) [32]byte {
	return keccak256([]byte{0x01}, left.Hash[:], ledgerWord(&left.Sum), right.Hash[:], ledgerWord(&right.Sum))
}

func ledgerLeaf(coldkey [32]byte, value uint256.Int) LedgerNode {
	return LedgerNode{Hash: naiveLedgerLeafHash(coldkey, &value), Sum: value}
}

// ledgerParent is the honest parent of two nodes. The sum wraps, as an
// adversary may state any sum.
func ledgerParent(left, right LedgerNode) LedgerNode {
	parent := LedgerNode{Hash: naiveLedgerNodeHash(left, right)}
	parent.Sum.Add(&left.Sum, &right.Sum)
	return parent
}

// ledgerTestTree holds every position of every level, padding included:
// levels[k] has 2^(d−k) positions. Built by naiveLedgerTree it is GT-1's
// independent reference; with some nodes stated dishonestly it is the
// adversarial tree P of a path check.
type ledgerTestTree struct {
	leafCount uint64
	levels    [][]LedgerNode
}

// naiveLedgerTree pads level 0 to 2^d with (0x00…00, 0), where d is the
// bit length of n − 1, and builds every level in full.
func naiveLedgerTree(accounts []LedgerAccount) *ledgerTestTree {
	n := uint64(len(accounts))
	level := make([]LedgerNode, uint64(1)<<bits.Len64(n-1))
	for i := range accounts {
		level[i] = ledgerLeaf(accounts[i].Coldkey, accounts[i].Cumulative)
	}
	t := &ledgerTestTree{leafCount: n, levels: [][]LedgerNode{level}}
	for len(level) > 1 {
		above := make([]LedgerNode, len(level)/2)
		for j := range above {
			above[j] = ledgerParent(level[2*j], level[2*j+1])
		}
		t.levels = append(t.levels, above)
		level = above
	}
	return t
}

func (t *ledgerTestTree) depth() int { return len(t.levels) - 1 }

func (t *ledgerTestTree) root() ([32]byte, uint256.Int) {
	root := t.levels[t.depth()][0]
	return root.Hash, root.Sum
}

func (t *ledgerTestTree) clone() *ledgerTestTree {
	c := &ledgerTestTree{leafCount: t.leafCount, levels: make([][]LedgerNode, len(t.levels))}
	for k := range t.levels {
		c.levels[k] = slices.Clone(t.levels[k])
	}
	return c
}

// proof is the claim proof of index as the tree states it.
func (t *ledgerTestTree) proof(index uint64) []LedgerNode {
	proof := make([]LedgerNode, t.depth())
	for k := range proof {
		proof[k] = t.levels[k][(index>>k)^1]
	}
	return proof
}

// path is the opening of index as the tree states it, root first.
func (t *ledgerTestTree) path(index uint64) []LedgerPathNode {
	d := t.depth()
	path := make([]LedgerPathNode, d)
	for i := range path {
		below := t.levels[d-i-1]
		j := index >> (d - i)
		left, right := below[2*j], below[2*j+1]
		path[i] = LedgerPathNode{LeftHash: left.Hash, LeftSum: left.Sum, RightHash: right.Hash, RightSum: right.Sum}
	}
	return path
}

// relink rehashes every ancestor of position j at level k and keeps every
// stated sum, so a node changed below stays reachable from the root.
func (t *ledgerTestTree) relink(k int, j uint64) {
	for ; k < t.depth(); k++ {
		j >>= 1
		t.levels[k+1][j].Hash = naiveLedgerNodeHash(t.levels[k][2*j], t.levels[k][2*j+1])
	}
}

// restate recomputes every ancestor of position j at level k honestly.
func (t *ledgerTestTree) restate(k int, j uint64) {
	for ; k < t.depth(); k++ {
		j >>= 1
		t.levels[k+1][j] = ledgerParent(t.levels[k][2*j], t.levels[k][2*j+1])
	}
}

// setLeaf states level-0 position index as node, with honest ancestors.
func (t *ledgerTestTree) setLeaf(index uint64, node LedgerNode) {
	t.levels[0][index] = node
	t.restate(0, index)
}

// breakSum states the node above index at level so that its children's
// sums no longer add up to its own: its left child's sum exceeds it, or its
// right child's sum is one off. Every node above stays consistent, so a
// walk finds the fault exactly there.
func (t *ledgerTestTree) breakSum(index uint64, level int, leftExceeds bool) {
	j := index >> level
	below := t.levels[level-1]
	if leftExceeds {
		below[2*j].Sum.AddUint64(&t.levels[level][j].Sum, 1)
		t.relink(level-1, 2*j)
	} else {
		below[2*j+1].Sum[0] ^= 1
		t.relink(level-1, 2*j+1)
	}
}

// wrapSums states the node above index at level with child sums that add up
// to its own only modulo 2^256: the left one exceeds it by one and the right
// one is 2^256 − 1.
func (t *ledgerTestTree) wrapSums(index uint64, level int) {
	j := index >> level
	below := t.levels[level-1]
	below[2*j].Sum.AddUint64(&t.levels[level][j].Sum, 1)
	below[2*j+1].Sum.SetAllOne()
	t.relink(level-1, 2*j)
}

// ledgerProofFromPath is the claim proof an opening implies, and the leaf
// node it ends at: (root, total) when the path is empty.
func ledgerProofFromPath(index uint64, root [32]byte, total uint256.Int, path []LedgerPathNode) ([]LedgerNode, LedgerNode) {
	d := len(path)
	proof := make([]LedgerNode, d)
	leaf := LedgerNode{Hash: root, Sum: total}
	for i, node := range path {
		level := d - i
		left := LedgerNode{Hash: node.LeftHash, Sum: node.LeftSum}
		right := LedgerNode{Hash: node.RightHash, Sum: node.RightSum}
		if (index>>(level-1))&1 == 1 {
			proof[level-1], leaf = left, right
		} else {
			proof[level-1], leaf = right, left
		}
	}
	return proof, leaf
}

// --- GT-1: roots, the depth rule and padding ---

var ledgerDepthCases = []struct {
	leafCount uint64
	depth     int
}{
	{1, 0}, {2, 1}, {3, 2}, {4, 2}, {5, 3}, {8, 3}, {9, 4}, {17, 5}, {1000, 10}, {1024, 10}, {1025, 11},
	{1 << 28, 28}, {1<<28 + 1, 29}, {MaxLedgerLeafCount - 1, 29}, {MaxLedgerLeafCount, 29},
}

// GT-1: d = 0 for one account, else ceil(log2 n).
func TestLedgerTreeDepth(t *testing.T) {
	for _, c := range ledgerDepthCases {
		if d, err := LedgerTreeDepth(c.leafCount); err != nil || d != c.depth {
			t.Fatalf("LedgerTreeDepth(%d) = %d, %v; want %d", c.leafCount, d, err, c.depth)
		}
	}
}

// GT-1: the leaf and node encodings against hashes computed independently
// with Foundry's `cast keccak`.
func TestLedgerHashKnownAnswers(t *testing.T) {
	var zero, ones [32]byte
	for i := range ones {
		ones[i] = 0xff
	}
	maxValue := new(uint256.Int).SubUint64(ledgerValueLimit, 1)
	for _, c := range []struct {
		name string
		got  [32]byte
		want string
	}{
		{"leafHash(0x00…00, 0)", LedgerLeafHash(zero, new(uint256.Int)), "0xae61b77b3e4cbac1353bfa4c59274e3ae531285c24e3cf57c11771ecbf72d9bf"},
		{"leafHash(0xff…ff, 2^128-1)", LedgerLeafHash(ones, maxValue), "0x40ecef9b4e8745f050e0a7b81631d5d21ecb11ce9ea0788568768d2ad12b4c18"},
		{"nodeHash(0x00…00, 0, 0x00…00, 0)", LedgerNodeHash(zero, new(uint256.Int), zero, new(uint256.Int)), "0x7afdc2ea6f8daaa4b430ce1424f59bcec401d00e34a99b1da457babc405a8607"},
	} {
		if hex32(c.got) != c.want {
			t.Errorf("%s = %s, want %s", c.name, hex32(c.got), c.want)
		}
	}
}

// GT-1: roots for n = 1, 2, 3, 4, 5, 8, 9 and 1,000 equal the independent
// reference, as does every node, padding included.
func TestLedgerSumTreeMatchesReference(t *testing.T) {
	for _, n := range []int{1, 2, 3, 4, 5, 8, 9, 1000} {
		accounts := ledgerDerivedAccounts("reference", n)
		tree := mustLedgerTree(t, accounts)
		ref := naiveLedgerTree(accounts)
		d := ref.depth()
		if tree.Depth() != d || tree.LeafCount() != uint64(n) {
			t.Fatalf("n=%d: depth %d leaf count %d, want %d and %d", n, tree.Depth(), tree.LeafCount(), d, n)
		}
		var sum uint256.Int
		for i := range accounts {
			sum.Add(&sum, &accounts[i].Cumulative)
		}
		root, total := tree.Root()
		refRoot, refTotal := ref.root()
		if root != refRoot || total != refTotal || total != sum {
			t.Fatalf("n=%d: root %s total %s, want %s and %s (sum %s)", n, hex32(root), total.Dec(), hex32(refRoot), refTotal.Dec(), sum.Dec())
		}
		for k := 0; k <= d; k++ {
			for j, want := range ref.levels[k] {
				if got := tree.Node(k, uint64(j)); got != want {
					t.Fatalf("n=%d: node (%d, %d) = (%s, %s), want (%s, %s)", n, k, j, hex32(got.Hash), got.Sum.Dec(), hex32(want.Hash), want.Sum.Dec())
				}
			}
		}
	}
}

// GT-1: a padding position is (0x00…00, 0) at level 0 and, above, the hash
// of two padding children with sum 0.
func TestLedgerSumTreePadding(t *testing.T) {
	accounts := ledgerDerivedAccounts("padding", 9) // d = 4: positions 9 … 15 pad
	tree := mustLedgerTree(t, accounts)
	var zero LedgerNode
	var pad [5][32]byte
	for k := 1; k < len(pad); k++ {
		pad[k] = naiveLedgerNodeHash(LedgerNode{Hash: pad[k-1]}, LedgerNode{Hash: pad[k-1]})
	}
	if hex32(pad[1]) != "0x7afdc2ea6f8daaa4b430ce1424f59bcec401d00e34a99b1da457babc405a8607" {
		t.Fatalf("padding hash of level 1 = %s", hex32(pad[1]))
	}
	for _, c := range []struct {
		k        int
		from, to uint64
	}{{0, 9, 16}, {1, 5, 8}, {2, 3, 4}} {
		for j := c.from; j < c.to; j++ {
			if tree.Node(c.k, j) != (LedgerNode{Hash: pad[c.k]}) {
				t.Fatalf("level %d position %d is not the padding node", c.k, j)
			}
		}
	}
	// The last account's parent pairs it with a level-0 padding node.
	if tree.Node(1, 4) != ledgerParent(ledgerLeaf(accounts[8].Coldkey, accounts[8].Cumulative), zero) {
		t.Fatal("level 1 position 4 does not pair account 8 with padding")
	}
}

// --- GT-2 and GT-3: claim proofs ---

// GT-2: every index's claim proof verifies, has exactly d siblings, and is
// the one the reference states.
func TestLedgerProofEveryIndex(t *testing.T) {
	for _, n := range []int{1, 2, 3, 4, 5, 8, 9, 1000} {
		accounts := ledgerDerivedAccounts("proof", n)
		tree := mustLedgerTree(t, accounts)
		ref := naiveLedgerTree(accounts)
		root, total := tree.Root()
		for i := range accounts {
			proof, err := tree.Proof(uint64(i))
			if err != nil || len(proof) != tree.Depth() || !slices.Equal(proof, ref.proof(uint64(i))) {
				t.Fatalf("n=%d index %d: proof of %d siblings (err %v) is not the reference's %d", n, i, len(proof), err, tree.Depth())
			}
			if !VerifyLedgerProof(root, &total, uint64(n), uint64(i), accounts[i].Coldkey, &accounts[i].Cumulative, proof) {
				t.Fatalf("n=%d index %d does not verify", n, i)
			}
		}
		if _, err := tree.Proof(uint64(n)); !errors.Is(err, ErrLedgerIndex) {
			t.Fatalf("n=%d: Proof(n) err = %v, want ErrLedgerIndex", n, err)
		}
	}
}

// ledgerProofCase is one claim check and the result both implementations
// give: GT-3 runs them in Go, and the vector file carries them to
// LedgerTree.verifyLeaf (T-2, T-4, X-1). err names where verifyLeaf
// reverts ("InvalidLeafCount" or "ArithmeticOverflow") and
// VerifyLedgerProof returns false.
type ledgerProofCase struct {
	name      string
	root      [32]byte
	total     uint256.Int
	leafCount uint64
	index     uint64
	account   LedgerAccount
	proof     []LedgerNode
	valid     bool
	err       string
}

// ledgerProofCases changes one thing at a time in account 2's claim of
// vector tree n5 (d = 3), then in account 0's claim of tree n1 (d = 0).
func ledgerProofCases() []ledgerProofCase {
	var cases []ledgerProofCase
	for _, n := range []int{5, 1} {
		accounts := ledgerVectorTreeAccounts(n)
		tree := naiveLedgerTree(accounts)
		root, total := tree.root()
		index := uint64(min(2, n-1))
		add := func(name string, valid bool, err string, change func(c *ledgerProofCase)) {
			c := ledgerProofCase{
				name: fmt.Sprintf("n%d_%s", n, name), root: root, total: total, leafCount: tree.leafCount,
				index: index, account: accounts[index], proof: tree.proof(index), valid: valid, err: err,
			}
			change(&c)
			cases = append(cases, c)
		}
		add("valid", true, "", func(*ledgerProofCase) {})
		for k := range tree.depth() {
			add(fmt.Sprintf("sibling_hash_level_%d", k), false, "", func(c *ledgerProofCase) { c.proof[k].Hash[0] ^= 1 })
			add(fmt.Sprintf("sibling_sum_level_%d", k), false, "", func(c *ledgerProofCase) { c.proof[k].Sum[0] ^= 1 })
			add(fmt.Sprintf("index_bit_%d", k), false, "", func(c *ledgerProofCase) { c.index ^= 1 << k })
		}
		add("coldkey", false, "", func(c *ledgerProofCase) { c.account.Coldkey[31] ^= 1 })
		add("cumulative_above", false, "", func(c *ledgerProofCase) { c.account.Cumulative.AddUint64(&c.account.Cumulative, 1) })
		add("cumulative_below", false, "", func(c *ledgerProofCase) { c.account.Cumulative.SubUint64(&c.account.Cumulative, 1) })
		add("proof_long", false, "", func(c *ledgerProofCase) { c.proof = append(c.proof, LedgerNode{Hash: root}) })
		add("index_at_leaf_count", false, "", func(c *ledgerProofCase) { c.index = c.leafCount })
		add("index_max", false, "", func(c *ledgerProofCase) { c.index = math.MaxUint64 })
		add("root", false, "", func(c *ledgerProofCase) { c.root[0] ^= 1 })
		add("total_above", false, "", func(c *ledgerProofCase) { c.total.AddUint64(&c.total, 1) })
		add("total_below", false, "", func(c *ledgerProofCase) { c.total.SubUint64(&c.total, 1) })
		add("leaf_count_zero", false, "InvalidLeafCount", func(c *ledgerProofCase) { c.leafCount = 0 })
		add("leaf_count_above_max", false, "InvalidLeafCount", func(c *ledgerProofCase) { c.leafCount = MaxLedgerLeafCount + 1 })
		if n == 1 {
			continue
		}
		add("proof_short", false, "", func(c *ledgerProofCase) { c.proof = c.proof[:len(c.proof)-1] })
		add("index_in_padding", false, "", func(c *ledgerProofCase) { c.index = 7 })
		add("leaf_count_other_depth", false, "", func(c *ledgerProofCase) { c.leafCount = 9 })
		add("sum_overflow_level_0", false, "ArithmeticOverflow", func(c *ledgerProofCase) { c.proof[0].Sum.SetAllOne() })
		add("sum_overflow_level_1", false, "ArithmeticOverflow", func(c *ledgerProofCase) { c.proof[1].Sum.SetAllOne() })
	}
	// Two values whose sum wraps around 2^256 to the stated total: the fold
	// reproduces the root and, modulo 2^256, the total, so only the overflow
	// check refuses it.
	wrapped := ledgerDerivedAccounts("wrap", 2)
	wrapped[0].Cumulative.Lsh(uint256.NewInt(1), 255)
	wrapped[1].Cumulative.AddUint64(&wrapped[0].Cumulative, 5)
	tree := naiveLedgerTree(wrapped)
	root, total := tree.root()
	cases = append(cases, ledgerProofCase{
		name: "wrapping_values", root: root, total: total, leafCount: 2, index: 0,
		account: wrapped[0], proof: tree.proof(0), err: "ArithmeticOverflow",
	})
	return cases
}

// GT-3, T-4: each shared proof case gives its result, and a sum that
// overflows 256 bits fails where verifyLeaf reverts.
func TestLedgerProofCases(t *testing.T) {
	for _, c := range ledgerProofCases() {
		got := VerifyLedgerProof(c.root, &c.total, c.leafCount, c.index, c.account.Coldkey, &c.account.Cumulative, c.proof)
		if got != (c.valid && c.err == "") {
			t.Fatalf("%s: VerifyLedgerProof = %v", c.name, got)
		}
		if c.err == "ArithmeticOverflow" {
			// The fold must overflow, not merely miss the root.
			sum, limit, overflows := c.account.Cumulative.ToBig(), new(big.Int).Lsh(big.NewInt(1), 256), false
			for _, sibling := range c.proof {
				overflows = overflows || sum.Add(sum, sibling.Sum.ToBig()).Cmp(limit) >= 0
			}
			if !overflows {
				t.Fatalf("%s: the fold does not overflow", c.name)
			}
		}
		if c.name == "wrapping_values" {
			h, s := LedgerLeafHash(c.account.Coldkey, &c.account.Cumulative), c.account.Cumulative
			h = LedgerNodeHash(h, &s, c.proof[0].Hash, &c.proof[0].Sum)
			if s.Add(&s, &c.proof[0].Sum); h != c.root || s != c.total {
				t.Fatalf("%s: the fold modulo 2^256 does not reproduce the root and total", c.name)
			}
		}
	}
}

// GT-3: each single change to every valid claim of several trees fails: a
// sibling hash or sum, an index bit, the coldkey, the cumulative, the proof
// length, an index at or above n, the root, the total and the leaf count.
func TestLedgerProofRejectsEverySingleChange(t *testing.T) {
	for _, n := range []int{1, 2, 3, 5, 8, 9, 33} {
		accounts := ledgerDerivedAccounts("change", n)
		tree := mustLedgerTree(t, accounts)
		root, total := tree.Root()
		d, leafCount := tree.Depth(), uint64(n)
		for i := range accounts {
			index, a := uint64(i), accounts[i]
			proof, err := tree.Proof(index)
			if err != nil {
				t.Fatal(err)
			}
			reject := func(change string, root [32]byte, total uint256.Int, leafCount, index uint64, a LedgerAccount, proof []LedgerNode) {
				t.Helper()
				if VerifyLedgerProof(root, &total, leafCount, index, a.Coldkey, &a.Cumulative, proof) {
					t.Fatalf("n=%d index %d: %s verified", n, i, change)
				}
			}
			for k := range d {
				changed := slices.Clone(proof)
				changed[k].Hash[0] ^= 1
				reject(fmt.Sprintf("sibling hash %d", k), root, total, leafCount, index, a, changed)
				changed = slices.Clone(proof)
				changed[k].Sum[0] ^= 1
				reject(fmt.Sprintf("sibling sum %d", k), root, total, leafCount, index, a, changed)
			}
			for k := range 64 {
				reject(fmt.Sprintf("index bit %d", k), root, total, leafCount, index^(1<<k), a, proof)
			}
			moved := a
			moved.Coldkey[i%32] ^= 0x80
			reject("coldkey", root, total, leafCount, index, moved, proof)
			for _, delta := range []uint64{1, 2} {
				above, below := a, a
				above.Cumulative.AddUint64(&a.Cumulative, delta)
				below.Cumulative.SubUint64(&a.Cumulative, delta)
				reject("cumulative above", root, total, leafCount, index, above, proof)
				reject("cumulative below", root, total, leafCount, index, below, proof)
			}
			if d > 0 {
				reject("short proof", root, total, leafCount, index, a, proof[:d-1])
			}
			reject("long proof", root, total, leafCount, index, a, append(slices.Clone(proof), LedgerNode{}))
			for _, beyond := range []uint64{leafCount, uint64(1) << d, math.MaxUint64} {
				if beyond >= leafCount {
					reject("index at or above n", root, total, leafCount, beyond, a, proof)
				}
			}
			changedRoot := root
			changedRoot[31] ^= 1
			reject("root", changedRoot, total, leafCount, index, a, proof)
			reject("total above", root, *new(uint256.Int).AddUint64(&total, 1), leafCount, index, a, proof)
			reject("total below", root, *new(uint256.Int).SubUint64(&total, 1), leafCount, index, a, proof)
			for _, other := range []uint64{0, 2*leafCount + 1, MaxLedgerLeafCount + 1} {
				reject("leaf count", root, total, other, index, a, proof)
			}
		}
	}
}

// --- GT-4 and GT-5: path checks ---

// GT-4: no fault on any index of an honest tree, against itself or in an
// honest successor that raised values and appended accounts.
func TestLedgerPathHonestTrees(t *testing.T) {
	for _, n := range []int{1, 2, 3, 4, 5, 8, 9, 1000} {
		accounts := ledgerDerivedAccounts("honest", n)
		tree := mustLedgerTree(t, accounts)
		ref := naiveLedgerTree(accounts)
		next := mustLedgerTree(t, ledgerSuccessor(accounts, n+n/2+1))
		for i := range accounts {
			index := uint64(i)
			if path, err := tree.Path(index); err != nil || !slices.Equal(path, ref.path(index)) {
				t.Fatalf("n=%d index %d: path (err %v) is not the reference's", n, i, err)
			}
			for _, p := range []*LedgerSumTree{tree, next} {
				path, err := p.Path(index)
				if err != nil || len(path) != p.Depth() {
					t.Fatalf("n=%d index %d: path of %d nodes, err %v", n, i, len(path), err)
				}
				root, total := p.Root()
				fault, level, err := WalkLedgerPath(root, &total, p.LeafCount(), index, accounts[i].Coldkey, &accounts[i].Cumulative, path)
				if err != nil || fault != LedgerFaultNone || level != 0 {
					t.Fatalf("n=%d index %d in a tree of %d: fault %v level %d err %v", n, i, p.LeafCount(), fault, level, err)
				}
			}
		}
		if _, err := tree.Path(uint64(n)); !errors.Is(err, ErrLedgerIndex) {
			t.Fatalf("n=%d: Path(n) err = %v, want ErrLedgerIndex", n, err)
		}
	}
}

// ledgerFaultCase is one path check of a previous account in a tree P and
// the result both implementations give: GT-5 runs them in Go, and the
// vector file carries them to LedgerTree.walkPath (X-2).
type ledgerFaultCase struct {
	name      string
	root      [32]byte
	total     uint256.Int
	leafCount uint64
	index     uint64
	account   LedgerAccount
	path      []LedgerPathNode
	fault     LedgerFault
	level     int
	err       error
}

// ledgerFaultCases opens account 2 of a five-account ledger F in a
// nine-account successor P (d = 4) stated with each fault, then the
// malformed openings, then a one-account pair (d = 0).
func ledgerFaultCases() []ledgerFaultCase {
	f := ledgerDerivedAccounts("fault", 5)
	honest := naiveLedgerTree(ledgerSuccessor(f, 9))
	const i = 2
	a, d := f[i], honest.depth()
	var cases []ledgerFaultCase
	add := func(name string, p *ledgerTestTree, a LedgerAccount, index uint64, path []LedgerPathNode, fault LedgerFault, level int, err error) {
		root, total := p.root()
		cases = append(cases, ledgerFaultCase{name, root, total, p.leafCount, index, a, path, fault, level, err})
	}
	lowered := a.Cumulative
	lowered.SubUint64(&lowered, 1)
	moved := a.Coldkey
	moved[0] ^= 1
	garbage := keccak256([]byte("garbage"))

	add("no_fault", honest, a, i, honest.path(i), LedgerFaultNone, 0, nil)
	unchanged := naiveLedgerTree(f)
	add("no_fault_unchanged_tree", unchanged, a, i, unchanged.path(i), LedgerFaultNone, 0, nil)

	lower := honest.clone()
	lower.setLeaf(i, ledgerLeaf(a.Coldkey, lowered))
	add("lower_value", lower, a, i, lower.path(i), LedgerFaultLowerValue, 0, nil)
	changed := honest.clone()
	changed.setLeaf(i, ledgerLeaf(moved, honest.levels[0][i].Sum))
	add("changed_coldkey", changed, a, i, changed.path(i), LedgerFaultWrongLeaf, 0, nil)
	garbageLeaf := honest.clone()
	garbageLeaf.setLeaf(i, LedgerNode{Hash: garbage, Sum: honest.levels[0][i].Sum})
	add("garbage_leaf", garbageLeaf, a, i, garbageLeaf.path(i), LedgerFaultWrongLeaf, 0, nil)
	garbageLower := honest.clone()
	garbageLower.setLeaf(i, LedgerNode{Hash: garbage, Sum: lowered})
	add("garbage_leaf_lower_value", garbageLower, a, i, garbageLower.path(i), LedgerFaultLowerValue, 0, nil)

	for level := d; level >= 1; level-- {
		for _, leftExceeds := range []bool{false, true} {
			p := honest.clone()
			p.breakSum(i, level, leftExceeds)
			name := fmt.Sprintf("inconsistent_level_%d_right_sum_off", level)
			if leftExceeds {
				name = fmt.Sprintf("inconsistent_level_%d_left_sum_exceeds", level)
			}
			add(name, p, a, i, p.path(i), LedgerFaultInconsistentNode, level, nil)
		}
	}
	for level := d; level >= 1; level-- {
		p := honest.clone()
		p.wrapSums(i, level)
		add(fmt.Sprintf("inconsistent_level_%d_sums_wrap", level), p, a, i, p.path(i), LedgerFaultInconsistentNode, level, nil)
	}
	for _, name := range []string{"root_sums_above_total", "root_sums_below_total"} {
		p := honest.clone()
		if name == "root_sums_above_total" {
			p.levels[d][0].Sum.SubUint64(&p.levels[d][0].Sum, 1)
		} else {
			p.levels[d][0].Sum.AddUint64(&p.levels[d][0].Sum, 1)
		}
		add(name, p, a, i, p.path(i), LedgerFaultInconsistentNode, d, nil)
	}
	both := lower.clone()
	both.breakSum(i, 2, false)
	add("inconsistent_above_lower_value", both, a, i, both.path(i), LedgerFaultInconsistentNode, 2, nil)

	path := honest.path(i)
	tampered := slices.Clone(path)
	tampered[0].LeftSum[0] ^= 1
	add("malformed_root_preimage", honest, a, i, tampered, LedgerFaultNone, 0, ErrLedgerMalformedPath)
	tampered = slices.Clone(path)
	tampered[d-1].RightHash[0] ^= 1
	add("malformed_lowest_preimage", honest, a, i, tampered, LedgerFaultNone, 0, ErrLedgerMalformedPath)
	withheld := honest.clone()
	withheld.levels[2][i>>2].Hash = garbage
	withheld.relink(2, i>>2)
	add("malformed_garbage_node", withheld, a, i, withheld.path(i), LedgerFaultNone, 0, ErrLedgerMalformedPath)
	hidden := honest.clone()
	hidden.breakSum(i, 3, false)
	tampered = hidden.path(i)
	tampered[d-3].LeftHash[0] ^= 1
	add("malformed_preimage_before_sums", hidden, a, i, tampered, LedgerFaultNone, 0, ErrLedgerMalformedPath)
	add("malformed_short_path", honest, a, i, path[:d-1], LedgerFaultNone, 0, ErrLedgerMalformedPath)
	add("malformed_long_path", honest, a, i, append(slices.Clone(path), path[d-1]), LedgerFaultNone, 0, ErrLedgerMalformedPath)
	add("malformed_empty_path", honest, a, i, []LedgerPathNode{}, LedgerFaultNone, 0, ErrLedgerMalformedPath)
	add("malformed_index_at_leaf_count", honest, a, 9, honest.path(9), LedgerFaultNone, 0, ErrLedgerMalformedPath)
	add("malformed_index_max", honest, a, math.MaxUint64, path, LedgerFaultNone, 0, ErrLedgerMalformedPath)
	for _, leafCount := range []uint64{0, MaxLedgerLeafCount + 1} {
		p := honest.clone()
		p.leafCount = leafCount
		add(fmt.Sprintf("invalid_leaf_count_%d", leafCount), p, a, i, path, LedgerFaultNone, 0, ErrLedgerLeafCount)
	}

	f0 := ledgerDerivedAccounts("fault-depth-0", 1)[0]
	raised := f0
	raised.Cumulative.AddUint64(&raised.Cumulative, 1)
	p0 := naiveLedgerTree([]LedgerAccount{raised})
	add("depth_0_no_fault", p0, f0, 0, []LedgerPathNode{}, LedgerFaultNone, 0, nil)
	lower0 := f0
	lower0.Cumulative.SubUint64(&lower0.Cumulative, 1)
	add("depth_0_lower_value", naiveLedgerTree([]LedgerAccount{lower0}), f0, 0, []LedgerPathNode{}, LedgerFaultLowerValue, 0, nil)
	moved0 := raised
	moved0.Coldkey[0] ^= 1
	add("depth_0_wrong_leaf", naiveLedgerTree([]LedgerAccount{moved0}), f0, 0, []LedgerPathNode{}, LedgerFaultWrongLeaf, 0, nil)
	add("depth_0_malformed_path", p0, f0, 0, []LedgerPathNode{{}}, LedgerFaultNone, 0, ErrLedgerMalformedPath)
	return cases
}

// GT-5: each fault is found with its exact kind and level: a lower value, a
// changed coldkey, a garbage leaf, an inconsistent node at every level and
// root sums not equal to the total; and each malformed path is refused.
func TestLedgerPathFaultCases(t *testing.T) {
	for _, c := range ledgerFaultCases() {
		fault, level, err := WalkLedgerPath(c.root, &c.total, c.leafCount, c.index, c.account.Coldkey, &c.account.Cumulative, c.path)
		if !errors.Is(err, c.err) || fault != c.fault || level != c.level {
			t.Fatalf("%s: fault %v level %d err %v; want %v, %d, %v", c.name, fault, level, err, c.fault, c.level, c.err)
		}
	}
}

// GT-5: at every index of F and every level of P, each fault is found with
// its kind at its level.
func TestLedgerPathFaultAtEveryIndexAndLevel(t *testing.T) {
	f := ledgerDerivedAccounts("fault-sweep", 11)
	honest := naiveLedgerTree(ledgerSuccessor(f, 23))
	expect := func(p *ledgerTestTree, i int, what string, want LedgerFault, wantLevel int) {
		t.Helper()
		root, total := p.root()
		fault, level, err := WalkLedgerPath(root, &total, p.leafCount, uint64(i), f[i].Coldkey, &f[i].Cumulative, p.path(uint64(i)))
		if err != nil || fault != want || level != wantLevel {
			t.Fatalf("index %d, %s: fault %v level %d err %v; want %v at %d", i, what, fault, level, err, want, wantLevel)
		}
	}
	for i := range f {
		index := uint64(i)
		for level := 1; level <= honest.depth(); level++ {
			for _, leftExceeds := range []bool{false, true} {
				p := honest.clone()
				p.breakSum(index, level, leftExceeds)
				expect(p, i, fmt.Sprintf("inconsistent at %d (left exceeds %v)", level, leftExceeds), LedgerFaultInconsistentNode, level)
			}
			p := honest.clone()
			p.wrapSums(index, level)
			expect(p, i, fmt.Sprintf("sums wrap at %d", level), LedgerFaultInconsistentNode, level)
		}
		lowered := f[i].Cumulative
		lowered.SubUint64(&lowered, 1)
		moved := f[i].Coldkey
		moved[31] ^= 1
		stated := honest.levels[0][index].Sum
		garbage := keccak256([]byte("garbage"), f[i].Coldkey[:])
		for _, c := range []struct {
			what  string
			leaf  LedgerNode
			fault LedgerFault
		}{
			{"lower value", ledgerLeaf(f[i].Coldkey, lowered), LedgerFaultLowerValue},
			{"changed coldkey", ledgerLeaf(moved, stated), LedgerFaultWrongLeaf},
			{"garbage leaf", LedgerNode{Hash: garbage, Sum: stated}, LedgerFaultWrongLeaf},
			{"garbage leaf, lower value", LedgerNode{Hash: garbage, Sum: lowered}, LedgerFaultLowerValue},
		} {
			p := honest.clone()
			p.setLeaf(index, c.leaf)
			expect(p, i, c.what, c.fault, 0)
		}
	}
}

// GT-5: the fault kinds' names, numbered as LedgerTree.Fault.
func TestLedgerFaultString(t *testing.T) {
	for fault, want := range map[LedgerFault]string{
		0: "none", 1: "inconsistent_node", 2: "lower_value", 3: "wrong_leaf", 4: "fault(4)",
	} {
		if got := fault.String(); got != want {
			t.Errorf("LedgerFault(%d).String() = %q, want %q", uint8(fault), got, want)
		}
	}
}

// --- GT-6: bounds, and the build's other refusals ---

// GT-6: values at or above 2^128 are refused, and 2^128 − 1 builds.
func TestLedgerSumTreeValueBound(t *testing.T) {
	accounts := ledgerDerivedAccounts("bounds", 3)
	accounts[2].Cumulative.SubUint64(ledgerValueLimit, 1)
	if _, err := BuildLedgerSumTree(context.Background(), accounts); err != nil {
		t.Fatalf("2^128 - 1 refused: %v", err)
	}
	for _, v := range []*uint256.Int{ledgerValueLimit, new(uint256.Int).AddUint64(ledgerValueLimit, 1), new(uint256.Int).SetAllOne()} {
		accounts[2].Cumulative = *v
		tree, err := BuildLedgerSumTree(context.Background(), accounts)
		if tree != nil || !errors.Is(err, ErrLedgerValueRange) || !strings.HasSuffix(err.Error(), ": account 2") {
			t.Fatalf("value %s: tree %v err %v, want ErrLedgerValueRange for account 2", v.Dec(), tree != nil, err)
		}
	}
}

// GT-6: more than 2^29 accounts, or none, are refused. The build's guard is
// LedgerTreeDepth of the account count, so it is checked at 2^29 + 1
// through LedgerTreeDepth, and through the build with no accounts, without
// allocating 2^29 accounts.
func TestLedgerSumTreeLeafCountBound(t *testing.T) {
	if d, err := LedgerTreeDepth(MaxLedgerLeafCount); err != nil || d != 29 {
		t.Fatalf("LedgerTreeDepth(2^29) = %d, %v", d, err)
	}
	for _, leafCount := range []uint64{0, MaxLedgerLeafCount + 1, math.MaxUint64} {
		if _, err := LedgerTreeDepth(leafCount); !errors.Is(err, ErrLedgerLeafCount) {
			t.Fatalf("LedgerTreeDepth(%d) err = %v, want ErrLedgerLeafCount", leafCount, err)
		}
		var root [32]byte
		var zero uint256.Int
		if VerifyLedgerProof(root, &zero, leafCount, 0, root, &zero, nil) {
			t.Fatalf("VerifyLedgerProof with leaf count %d verified", leafCount)
		}
		if _, _, err := WalkLedgerPath(root, &zero, leafCount, 0, root, &zero, nil); !errors.Is(err, ErrLedgerLeafCount) {
			t.Fatalf("WalkLedgerPath with leaf count %d err = %v, want ErrLedgerLeafCount", leafCount, err)
		}
	}
	for _, accounts := range [][]LedgerAccount{nil, {}} {
		if tree, err := BuildLedgerSumTree(context.Background(), accounts); tree != nil || !errors.Is(err, ErrLedgerLeafCount) {
			t.Fatalf("BuildLedgerSumTree(no accounts) err = %v, want ErrLedgerLeafCount", err)
		}
	}
}

// GT-6: the build also refuses a nil context, and stops with the context's
// error both before the leaves and between levels.
func TestLedgerSumTreeBuildContext(t *testing.T) {
	accounts := ledgerDerivedAccounts("context", 5)
	if tree, err := BuildLedgerSumTree(nil, accounts); tree != nil || err == nil {
		t.Fatal("nil context was admitted")
	}
	for stop := int32(1); stop <= 2; stop++ {
		ctx := treeCancelAt(t, stop)
		tree, err := BuildLedgerSumTree(ctx, accounts)
		if tree != nil || !errors.Is(err, context.Canceled) || ctx.calls.Load() != stop {
			t.Fatalf("cancel at check %d: tree %v err %v after %d checks", stop, tree != nil, err, ctx.calls.Load())
		}
	}
}

// --- GT-7 and GT-8: properties on adversarial trees ---

func randomLedgerHash(r *rand.Rand) (h [32]byte) {
	for i := 0; i < len(h); i += 8 {
		binary.BigEndian.PutUint64(h[i:], r.Uint64())
	}
	return h
}

// randomLedgerValue is below 2^128, of any magnitude.
func randomLedgerValue(r *rand.Rand) (v uint256.Int) {
	v[0], v[1] = r.Uint64(), r.Uint64()
	v.Rsh(&v, uint(r.IntN(128)))
	return v
}

func randomLedgerAccount(r *rand.Rand) LedgerAccount {
	return LedgerAccount{Coldkey: randomLedgerHash(r), Cumulative: randomLedgerValue(r)}
}

// ledgerLie changes a stated sum: up or down by a little, wrapping, or to
// any uint256.
func ledgerLie(r *rand.Rand, s *uint256.Int) {
	switch delta := 1 + r.Uint64N(1000); r.IntN(3) {
	case 0:
		s.AddUint64(s, delta)
	case 1:
		s.SubUint64(s, delta)
	default:
		s[0], s[1], s[2], s[3] = r.Uint64(), r.Uint64(), r.Uint64(), r.Uint64()
	}
}

// adversarialLedgerTree states n accounts the way a dishonest operator
// could: garbage hashes, stated sums that disagree with what they cover,
// filled padding, repeated leaves and values of any width, each at a
// per-tree rate, rate 0 being an honest tree. leaves[j] is the account
// behind level-0 position j.
func adversarialLedgerTree(r *rand.Rand, n uint64) (t *ledgerTestTree, leaves []*LedgerAccount, honest bool) {
	d := bits.Len64(n - 1)
	rate := r.IntN(4)
	mutate := func() bool { return r.IntN(16) < rate*rate }
	t = &ledgerTestTree{leafCount: n, levels: make([][]LedgerNode, d+1)}
	t.levels[0] = make([]LedgerNode, uint64(1)<<d)
	leaves = make([]*LedgerAccount, len(t.levels[0]))
	for j := range t.levels[0] {
		node := &t.levels[0][j]
		switch {
		case uint64(j) >= n && !mutate():
			// padding
		case mutate():
			*node = LedgerNode{Hash: randomLedgerHash(r), Sum: randomLedgerValue(r)}
		case j > 0 && leaves[j-1] != nil && mutate():
			leaves[j], *node = leaves[j-1], t.levels[0][j-1]
		default:
			a := randomLedgerAccount(r)
			if mutate() {
				// Any uint256, so that sums wrap.
				a.Cumulative[2], a.Cumulative[3] = r.Uint64(), r.Uint64()
			}
			leaves[j], *node = &a, ledgerLeaf(a.Coldkey, a.Cumulative)
		}
		if mutate() {
			ledgerLie(r, &node.Sum)
		}
	}
	for k := range d {
		below := t.levels[k]
		above := make([]LedgerNode, len(below)/2)
		for j := range above {
			above[j] = ledgerParent(below[2*j], below[2*j+1])
			if mutate() {
				above[j].Hash = randomLedgerHash(r)
			}
			if mutate() {
				ledgerLie(r, &above[j].Sum)
			}
		}
		t.levels[k+1] = above
	}
	return t, leaves, rate == 0
}

// checkLedgerProvableBound is GT-7 for one tree. A position is provable when
// a claim proof for it verifies, and the only candidate is the proof the
// tree states for the account behind the position: any other would break
// keccak256. It returns the number of provable positions of a dishonest
// tree, and 0 for an honest one.
func checkLedgerProvableBound(t *testing.T, seed, n uint64) int {
	t.Helper()
	r := rand.New(rand.NewPCG(seed, n))
	tree, leaves, honest := adversarialLedgerTree(r, n)
	root, total := tree.root()
	provable, count := new(big.Int), 0
	for i := range n {
		if a := leaves[i]; a != nil && VerifyLedgerProof(root, &total, n, i, a.Coldkey, &a.Cumulative, tree.proof(i)) {
			provable.Add(provable, a.Cumulative.ToBig())
			count++
		}
	}
	if provable.Cmp(total.ToBig()) > 0 {
		t.Fatalf("seed %d n %d: provable values sum to %s, above the total %s", seed, n, provable, total.Dec())
	}
	if !honest {
		return count
	}
	if count != int(n) || provable.Cmp(total.ToBig()) != 0 {
		t.Fatalf("seed %d n %d: honest tree proves %d positions summing to %s of %s", seed, n, count, provable, total.Dec())
	}
	return 0
}

// GT-7: on random adversarial trees (garbage hashes, inconsistent sums,
// filled padding), the values of all provable positions sum to at most the
// total.
func TestLedgerProvableValuesAtMostTotal(t *testing.T) {
	provable := 0
	for seed := range uint64(2000) {
		provable += checkLedgerProvableBound(t, seed, seed%64+1)
	}
	// 4,681 positions of the dishonest trees are provable; fewer would mean
	// the generator lost its teeth.
	if provable < 2_000 {
		t.Fatalf("only %d provable positions in dishonest trees: the property is near vacuous", provable)
	}
}

// GT-7, fuzzed.
func FuzzLedgerProvableValuesAtMostTotal(f *testing.F) {
	for seed := range uint64(8) {
		f.Add(seed, uint8(seed*9))
	}
	f.Fuzz(func(t *testing.T, seed uint64, n uint8) {
		checkLedgerProvableBound(t, seed, uint64(n)%64+1)
	})
}

// adversarialLedgerSuccessor is an honest successor of F with up to three
// changes a dishonest operator could make: a garbage hash, a stated sum
// moved, a leaf restated at another value, or another coldkey at a
// position, filling it if it was padding.
func adversarialLedgerSuccessor(r *rand.Rand, f []LedgerAccount) *ledgerTestTree {
	accounts := make([]LedgerAccount, len(f)+r.IntN(len(f)+1))
	for i := range accounts {
		if i < len(f) {
			accounts[i] = f[i]
			accounts[i].Cumulative.AddUint64(&accounts[i].Cumulative, r.Uint64N(3))
		} else {
			accounts[i] = randomLedgerAccount(r)
		}
	}
	p := naiveLedgerTree(accounts)
	for changes := r.IntN(4); changes > 0; changes-- {
		k := r.IntN(p.depth() + 1)
		j := r.Uint64N(uint64(len(p.levels[k])))
		switch leaf := j << k; r.IntN(4) {
		case 0:
			p.levels[k][j].Hash = randomLedgerHash(r)
			p.relink(k, j)
		case 1:
			ledgerLie(r, &p.levels[k][j].Sum)
			p.relink(k, j)
		case 2:
			if leaf < uint64(len(accounts)) {
				value := p.levels[0][leaf].Sum
				ledgerLie(r, &value)
				p.setLeaf(leaf, ledgerLeaf(accounts[leaf].Coldkey, value))
			}
		default:
			p.setLeaf(leaf, ledgerLeaf(randomLedgerHash(r), randomLedgerValue(r)))
		}
	}
	return p
}

// checkLedgerUnfaultedProvable is GT-8 for one pair of trees: every account
// of F with no fault in P is provable in P, with F's coldkey, at at least
// F's value. It returns how many accounts had no fault and how many did.
func checkLedgerUnfaultedProvable(t *testing.T, seed, n uint64) (clean, faulted int) {
	t.Helper()
	r := rand.New(rand.NewPCG(seed, n))
	f := make([]LedgerAccount, n)
	for i := range f {
		f[i] = randomLedgerAccount(r)
	}
	p := adversarialLedgerSuccessor(r, f)
	root, total := p.root()
	for i := range f {
		index := uint64(i)
		path := p.path(index)
		fault, _, err := WalkLedgerPath(root, &total, p.leafCount, index, f[i].Coldkey, &f[i].Cumulative, path)
		if err != nil || fault != LedgerFaultNone {
			faulted++
			continue
		}
		clean++
		proof, leaf := ledgerProofFromPath(index, root, total, path)
		if leaf.Sum.Lt(&f[i].Cumulative) || !VerifyLedgerProof(root, &total, p.leafCount, index, f[i].Coldkey, &leaf.Sum, proof) {
			t.Fatalf("seed %d n %d: account %d has no fault but is not provable at %s >= %s", seed, n, i, leaf.Sum.Dec(), f[i].Cumulative.Dec())
		}
	}
	return clean, faulted
}

// GT-8: an account of an honest tree F with no fault in an adversarial tree
// P is provable in P with F's coldkey at at least F's value.
func TestLedgerUnfaultedAccountsStayProvable(t *testing.T) {
	clean, faulted := 0, 0
	for seed := range uint64(2000) {
		c, f := checkLedgerUnfaultedProvable(t, seed, seed%40+1)
		clean, faulted = clean+c, faulted+f
	}
	// 31,713 accounts have no fault and 9,287 do; far fewer of either would
	// mean the generator lost its teeth.
	if clean < 15_000 || faulted < 4_000 {
		t.Fatalf("%d clean and %d faulted accounts: the property is near vacuous", clean, faulted)
	}
}

// GT-8, fuzzed.
func FuzzLedgerUnfaultedAccountsStayProvable(f *testing.F) {
	for seed := range uint64(8) {
		f.Add(seed, uint8(seed*5))
	}
	f.Fuzz(func(t *testing.T, seed uint64, n uint8) {
		checkLedgerUnfaultedProvable(t, seed, uint64(n)%40+1)
	})
}
