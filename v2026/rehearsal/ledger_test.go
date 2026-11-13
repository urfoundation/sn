//go:build runtime_rehearsal

package rehearsal

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"

	"github.com/urfoundation/sn/v2026/merkle"
)

// fillerColdkey owns every filler account of a ledger.
var fillerColdkey = [32]byte(crypto.Keccak256Hash([]byte("urnetwork rehearsal filler account")))

// ledger is a pool's ledger tree (merkle/ledger_sum_tree.go) whose first
// accounts are given and whose other accounts, up to leafCount, are filler
// accounts (fillerColdkey, 0). A filler subtree's hash depends only on its
// level, so a tree of 2^29 accounts, the deepest vault2 takes, builds at once.
type ledger struct {
	accounts  []merkle.LedgerAccount
	leafCount uint64
	depth     int
	// filler[k] and padding[k] are the hashes of a level-k subtree of filler
	// accounts and of padding positions.
	filler, padding [][32]byte
}

// account, proofNode and pathNode are LedgerTree.Account, ProofNode and PathNode.
type account struct {
	Index      *big.Int
	Coldkey    [32]byte
	Cumulative *big.Int
}

type proofNode struct {
	Hash [32]byte
	Sum  *big.Int
}

type pathNode struct {
	LeftHash  [32]byte
	LeftSum   *big.Int
	RightHash [32]byte
	RightSum  *big.Int
}

// commitment is ISTSettlementVault2.Commitment.
type commitment struct {
	Root       [32]byte
	Total      *big.Int
	LeafCount  uint64
	LedgerHash [32]byte
}

func newLedger(t *testing.T, accounts []merkle.LedgerAccount, leafCount uint64) *ledger {
	t.Helper()
	depth, err := merkle.LedgerTreeDepth(leafCount)
	if err != nil || uint64(len(accounts)) > leafCount {
		t.Fatalf("ledger of %d accounts in %d leaves: %v", len(accounts), leafCount, err)
	}
	l := &ledger{accounts: accounts, leafCount: leafCount, depth: depth, filler: make([][32]byte, depth+1), padding: make([][32]byte, depth+1)}
	zero := new(uint256.Int)
	l.filler[0] = merkle.LedgerLeafHash(fillerColdkey, zero)
	for k := 0; k < depth; k++ {
		l.filler[k+1] = merkle.LedgerNodeHash(l.filler[k], zero, l.filler[k], zero)
		l.padding[k+1] = merkle.LedgerNodeHash(l.padding[k], zero, l.padding[k], zero)
	}
	return l
}

// node returns position j of level k.
func (self *ledger) node(k int, j uint64) merkle.LedgerNode {
	first, real := j<<k, uint64(len(self.accounts))
	switch {
	case first >= self.leafCount:
		return merkle.LedgerNode{Hash: self.padding[k]}
	case first >= real && first+(uint64(1)<<k) <= self.leafCount:
		return merkle.LedgerNode{Hash: self.filler[k]}
	case k == 0:
		if j < real {
			a := &self.accounts[j]
			return merkle.LedgerNode{Hash: merkle.LedgerLeafHash(a.Coldkey, &a.Cumulative), Sum: a.Cumulative}
		}
		return merkle.LedgerNode{Hash: self.filler[0]}
	}
	left, right := self.node(k-1, 2*j), self.node(k-1, 2*j+1)
	node := merkle.LedgerNode{Hash: merkle.LedgerNodeHash(left.Hash, &left.Sum, right.Hash, &right.Sum)}
	node.Sum.Add(&left.Sum, &right.Sum)
	return node
}

func (self *ledger) root() ([32]byte, *big.Int) {
	root := self.node(self.depth, 0)
	return root.Hash, root.Sum.ToBig()
}

// commitment returns the root to commit, with ledgerHash standing for the
// sha256 of the ledger header, which vault2 only requires to be nonzero.
func (self *ledger) commitment(ledgerHash string) commitment {
	root, total := self.root()
	return commitment{Root: root, Total: total, LeafCount: self.leafCount, LedgerHash: [32]byte(crypto.Keccak256Hash([]byte(ledgerHash)))}
}

func (self *ledger) account(index uint64) account {
	a := &self.accounts[index]
	return account{Index: id(index), Coldkey: a.Coldkey, Cumulative: a.Cumulative.ToBig()}
}

// proof returns account index's bottom-up siblings.
func (self *ledger) proof(index uint64) []proofNode {
	proof := make([]proofNode, self.depth)
	for k := range self.depth {
		sibling := self.node(k, (index>>k)^1)
		proof[k] = proofNode{Hash: sibling.Hash, Sum: sibling.Sum.ToBig()}
	}
	return proof
}

// path returns the top-down preimages of the nodes above account index.
func (self *ledger) path(index uint64) []pathNode {
	path := make([]pathNode, self.depth)
	for i := range self.depth {
		level := self.depth - i
		j := index >> level
		left, right := self.node(level-1, 2*j), self.node(level-1, 2*j+1)
		path[i] = pathNode{LeftHash: left.Hash, LeftSum: left.Sum.ToBig(), RightHash: right.Hash, RightSum: right.Sum.ToBig()}
	}
	return path
}

func ledgerAccount(coldkey [32]byte, cumulative uint64) merkle.LedgerAccount {
	return merkle.LedgerAccount{Coldkey: coldkey, Cumulative: *uint256.NewInt(cumulative)}
}

// TestLedgerMatchesMerkle checks the R battery's ledger against the reference
// tree: with filler accounts written out, and with padding.
func TestLedgerMatchesMerkle(t *testing.T) {
	accounts := []merkle.LedgerAccount{ledgerAccount([32]byte{1}, 5), ledgerAccount([32]byte{2}, 7), ledgerAccount([32]byte{3}, 11)}
	for _, leafCount := range []uint64{3, 4, 6, 13, 16} {
		dense := append([]merkle.LedgerAccount{}, accounts...)
		for uint64(len(dense)) < leafCount {
			dense = append(dense, ledgerAccount(fillerColdkey, 0))
		}
		reference, err := merkle.BuildLedgerSumTree(context.Background(), dense)
		if err != nil {
			t.Fatal(err)
		}
		l := newLedger(t, accounts, leafCount)
		wantRoot, wantTotal := reference.Root()
		root, total := l.root()
		if root != wantRoot || total.Cmp(wantTotal.ToBig()) != 0 {
			t.Fatalf("%d leaves: root %x total %s, reference %x %s", leafCount, root, total, wantRoot, wantTotal.ToBig())
		}
		for index := uint64(0); index < leafCount; index++ {
			proof, err := reference.Proof(index)
			if err != nil {
				t.Fatal(err)
			}
			path, err := reference.Path(index)
			if err != nil {
				t.Fatal(err)
			}
			for k := range proof {
				if got := l.proof(index)[k]; got.Hash != proof[k].Hash || got.Sum.Cmp(proof[k].Sum.ToBig()) != 0 {
					t.Fatalf("%d leaves: proof of %d differs at level %d", leafCount, index, k)
				}
				if got := l.path(index)[k]; got.LeftHash != path[k].LeftHash || got.RightHash != path[k].RightHash ||
					got.LeftSum.Cmp(path[k].LeftSum.ToBig()) != 0 || got.RightSum.Cmp(path[k].RightSum.ToBig()) != 0 {
					t.Fatalf("%d leaves: path of %d differs at node %d", leafCount, index, k)
				}
			}
		}
	}
}
