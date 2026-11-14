package merkle

// The ledger sum tree `urnetwork-ledger-sum-tree-v1` (VERSION2-FINAL.md §6.5)
// commits a pool's settlement v2 ledger: account i is the leaf
// (coldkey_i, cumulative_i) at position i, and every internal node commits to
// both children's hashes and stated sums. evm/src/LedgerTree.sol is the
// on-chain twin of this file; merkle/testdata/ledger_v2_vectors.json holds the
// vectors both are checked against.
//
//	leafHash(c, v)        = keccak256(0x00 ‖ c ‖ uint256(v))
//	nodeHash(hL,sL,hR,sR) = keccak256(0x01 ‖ hL ‖ uint256(sL) ‖ hR ‖ uint256(sR))
//	level 0, position i   = (leafHash(c_i, v_i), v_i) for i < n; (0x00…00, 0) for n <= i < 2^d
//	level k+1, position j = (nodeHash(L.h, L.s, R.h, R.s), L.s + R.s)
//	d = 0 if n == 1, else ceil(log2 n); the root is level d, position 0.
//
// A tree stores, per level, only the positions that cover at least one
// account. Every position past them is a padding subtree, whose hash depends
// only on its level and whose sum is zero.

import (
	"context"
	"errors"
	"fmt"

	"github.com/holiman/uint256"
	"golang.org/x/crypto/sha3"
)

const (
	// LedgerSumTreeScheme names the tree construction.
	LedgerSumTreeScheme = "urnetwork-ledger-sum-tree-v1"
	// MaxLedgerLeafCount is the largest account count a tree can hold.
	MaxLedgerLeafCount = 1 << 29
)

var (
	// ErrLedgerLeafCount is returned for a tree of zero or more than 2^29 accounts.
	ErrLedgerLeafCount = errors.New("merkle: ledger tree needs 1 to 2^29 accounts")
	// ErrLedgerValueRange is returned for an account value at or above 2^128.
	ErrLedgerValueRange = errors.New("merkle: ledger account value must be below 2^128")
	// ErrLedgerIndex is returned for an account index at or above the leaf count.
	ErrLedgerIndex = errors.New("merkle: ledger account index out of range")
	// ErrLedgerMalformedPath is returned when a path has the wrong length or a
	// preimage that does not hash to its node. LedgerTree.walkPath reverts
	// MalformedPath in exactly these cases.
	ErrLedgerMalformedPath = errors.New("merkle: ledger path is malformed")
)

// LedgerFault is a fault found by WalkLedgerPath, numbered as LedgerTree.Fault.
type LedgerFault uint8

const (
	LedgerFaultNone LedgerFault = iota
	// LedgerFaultInconsistentNode: a node's stated child sums do not add up to
	// the node's own sum.
	LedgerFaultInconsistentNode
	// LedgerFaultLowerValue: the account's stated value is below its previous value.
	LedgerFaultLowerValue
	// LedgerFaultWrongLeaf: the leaf hash does not commit to (previous coldkey, stated value).
	LedgerFaultWrongLeaf
)

func (f LedgerFault) String() string {
	switch f {
	case LedgerFaultNone:
		return "none"
	case LedgerFaultInconsistentNode:
		return "inconsistent_node"
	case LedgerFaultLowerValue:
		return "lower_value"
	case LedgerFaultWrongLeaf:
		return "wrong_leaf"
	default:
		return fmt.Sprintf("fault(%d)", uint8(f))
	}
}

// LedgerAccount is one account of a pool's ledger.
type LedgerAccount struct {
	Coldkey    [32]byte
	Cumulative uint256.Int
}

// LedgerNode is a tree node: its hash and its stated sum. A bottom-up proof is
// a list of sibling nodes, for levels 0 … d−1.
type LedgerNode struct {
	Hash [32]byte
	Sum  uint256.Int
}

// LedgerPathNode is the preimage of one internal node on a top-down path.
type LedgerPathNode struct {
	LeftHash  [32]byte
	LeftSum   uint256.Int
	RightHash [32]byte
	RightSum  uint256.Int
}

// LedgerSumTree is a built tree. It is immutable and safe for concurrent reads.
type LedgerSumTree struct {
	leafCount uint64
	depth     int
	// levels[k] holds positions 0 … ceil(n/2^k)−1 of level k.
	levels [][]LedgerNode
	// padding[k] is the hash of a padding subtree whose root is at level k.
	padding [][32]byte
}

var ledgerValueLimit = new(uint256.Int).Lsh(uint256.NewInt(1), 128)

// LedgerLeafHash returns keccak256(0x00 ‖ coldkey ‖ uint256(value)).
func LedgerLeafHash(coldkey [32]byte, value *uint256.Int) [32]byte {
	var buf [65]byte
	copy(buf[1:33], coldkey[:])
	v := value.Bytes32()
	copy(buf[33:65], v[:])
	return keccak(buf[:])
}

// LedgerNodeHash returns keccak256(0x01 ‖ hL ‖ uint256(sL) ‖ hR ‖ uint256(sR)).
func LedgerNodeHash(leftHash [32]byte, leftSum *uint256.Int, rightHash [32]byte, rightSum *uint256.Int) [32]byte {
	var buf [129]byte
	buf[0] = 0x01
	ls, rs := leftSum.Bytes32(), rightSum.Bytes32()
	copy(buf[1:33], leftHash[:])
	copy(buf[33:65], ls[:])
	copy(buf[65:97], rightHash[:])
	copy(buf[97:129], rs[:])
	return keccak(buf[:])
}

func keccak(b []byte) [32]byte {
	h := sha3.NewLegacyKeccak256()
	h.Write(b)
	var out [32]byte
	h.Sum(out[:0])
	return out
}

// LedgerTreeDepth returns d for a tree of leafCount accounts.
func LedgerTreeDepth(leafCount uint64) (int, error) {
	if leafCount == 0 || leafCount > MaxLedgerLeafCount {
		return 0, ErrLedgerLeafCount
	}
	d := 0
	for uint64(1)<<d < leafCount {
		d++
	}
	return d, nil
}

// BuildLedgerSumTree builds the tree of accounts in position order.
func BuildLedgerSumTree(ctx context.Context, accounts []LedgerAccount) (*LedgerSumTree, error) {
	if ctx == nil {
		return nil, errors.New("merkle: ledger tree build requires a context")
	}
	n := uint64(len(accounts))
	d, err := LedgerTreeDepth(n)
	if err != nil {
		return nil, err
	}
	level := make([]LedgerNode, n)
	for i := range accounts {
		if i%65536 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		a := &accounts[i]
		if !a.Cumulative.Lt(ledgerValueLimit) {
			return nil, fmt.Errorf("%w: account %d", ErrLedgerValueRange, i)
		}
		level[i] = LedgerNode{Hash: LedgerLeafHash(a.Coldkey, &a.Cumulative), Sum: a.Cumulative}
	}
	t := &LedgerSumTree{leafCount: n, depth: d, levels: make([][]LedgerNode, d+1), padding: make([][32]byte, d+1)}
	t.levels[0] = level
	zero := new(uint256.Int)
	for k := 0; k < d; k++ {
		t.padding[k+1] = LedgerNodeHash(t.padding[k], zero, t.padding[k], zero)
		below := t.levels[k]
		above := make([]LedgerNode, (len(below)+1)/2)
		for j := range above {
			if j%65536 == 0 {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
			}
			left := below[2*j]
			right := LedgerNode{Hash: t.padding[k]}
			if 2*j+1 < len(below) {
				right = below[2*j+1]
			}
			above[j].Hash = LedgerNodeHash(left.Hash, &left.Sum, right.Hash, &right.Sum)
			// Values are below 2^128 and there are at most 2^29 of them, so
			// no sum can overflow 256 bits.
			above[j].Sum.Add(&left.Sum, &right.Sum)
		}
		t.levels[k+1] = above
	}
	return t, nil
}

// Root returns the root hash and the total.
func (t *LedgerSumTree) Root() ([32]byte, uint256.Int) {
	root := t.levels[t.depth][0]
	return root.Hash, root.Sum
}

// LeafCount returns n.
func (t *LedgerSumTree) LeafCount() uint64 { return t.leafCount }

// Depth returns d.
func (t *LedgerSumTree) Depth() int { return t.depth }

// Node returns position j of level k, including padding positions.
func (t *LedgerSumTree) Node(k int, j uint64) LedgerNode {
	if j < uint64(len(t.levels[k])) {
		return t.levels[k][j]
	}
	return LedgerNode{Hash: t.padding[k]}
}

// Proof returns the bottom-up siblings of account index, for levels 0 … d−1.
func (t *LedgerSumTree) Proof(index uint64) ([]LedgerNode, error) {
	if index >= t.leafCount {
		return nil, ErrLedgerIndex
	}
	proof := make([]LedgerNode, t.depth)
	for k := 0; k < t.depth; k++ {
		proof[k] = t.Node(k, (index>>k)^1)
	}
	return proof, nil
}

// Path returns the top-down preimages of the internal nodes above account
// index, root first: one per level d … 1.
func (t *LedgerSumTree) Path(index uint64) ([]LedgerPathNode, error) {
	if index >= t.leafCount {
		return nil, ErrLedgerIndex
	}
	path := make([]LedgerPathNode, t.depth)
	for i := 0; i < t.depth; i++ {
		level := t.depth - i
		j := index >> level
		left, right := t.Node(level-1, 2*j), t.Node(level-1, 2*j+1)
		path[i] = LedgerPathNode{LeftHash: left.Hash, LeftSum: left.Sum, RightHash: right.Hash, RightSum: right.Sum}
	}
	return path, nil
}

// VerifyLedgerProof reports whether proof folds (coldkey, value) at index into
// root and total, exactly as LedgerTree.verifyLeaf does. An invalid leaf count
// gives false where LedgerTree.verifyLeaf reverts InvalidLeafCount, and a sum
// that overflows 256 bits gives false where it reverts on the overflow.
func VerifyLedgerProof(root [32]byte, total *uint256.Int, leafCount uint64, index uint64, coldkey [32]byte, value *uint256.Int, proof []LedgerNode) bool {
	d, err := LedgerTreeDepth(leafCount)
	if err != nil || index >= leafCount || len(proof) != d {
		return false
	}
	h := LedgerLeafHash(coldkey, value)
	s := *value
	for k := 0; k < d; k++ {
		sibling := &proof[k]
		if (index>>k)&1 == 1 {
			h = LedgerNodeHash(sibling.Hash, &sibling.Sum, h, &s)
		} else {
			h = LedgerNodeHash(h, &s, sibling.Hash, &sibling.Sum)
		}
		if _, overflow := s.AddOverflow(&s, &sibling.Sum); overflow {
			return false
		}
	}
	return h == root && s.Eq(total)
}

// WalkLedgerPath opens the path to index in the tree (root, total, leafCount)
// and reports the first fault against the previous account (index, coldkey,
// value), with the level where it was found: d at the root, 0 at the leaf. It
// mirrors LedgerTree.walkPath, whose reverts are errors here: an invalid leaf
// count gives ErrLedgerLeafCount where walkPath reverts InvalidLeafCount, and
// ErrLedgerMalformedPath stands for its MalformedPath revert.
func WalkLedgerPath(root [32]byte, total *uint256.Int, leafCount uint64, index uint64, coldkey [32]byte, value *uint256.Int, path []LedgerPathNode) (LedgerFault, int, error) {
	d, err := LedgerTreeDepth(leafCount)
	if err != nil {
		return LedgerFaultNone, 0, err
	}
	if index >= leafCount || len(path) != d {
		return LedgerFaultNone, 0, ErrLedgerMalformedPath
	}
	h := root
	s := *total
	for i := 0; i < d; i++ {
		node := &path[i]
		level := d - i
		if LedgerNodeHash(node.LeftHash, &node.LeftSum, node.RightHash, &node.RightSum) != h {
			return LedgerFaultNone, 0, ErrLedgerMalformedPath
		}
		if node.LeftSum.Gt(&s) || !new(uint256.Int).Sub(&s, &node.LeftSum).Eq(&node.RightSum) {
			return LedgerFaultInconsistentNode, level, nil
		}
		if (index>>(level-1))&1 == 1 {
			h, s = node.RightHash, node.RightSum
		} else {
			h, s = node.LeftHash, node.LeftSum
		}
	}
	if s.Lt(value) {
		return LedgerFaultLowerValue, 0, nil
	}
	if h != LedgerLeafHash(coldkey, &s) {
		return LedgerFaultWrongLeaf, 0, nil
	}
	return LedgerFaultNone, 0, nil
}
