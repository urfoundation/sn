// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {LedgerTree} from "../../src/LedgerTree.sol";

/// @dev Builds ledger sum trees (LedgerTree.sol; VERSION2-FINAL.md §6.5) in
///      memory for the vault2 suite, honest or deliberately faulty. levels[0]
///      holds the leaves, padded to 2^d with (0, 0); levels[d][0] is the root.
///      To commit to a fault, change a node and rehash every level above it.
library V2TreeHelper {
    struct Node {
        bytes32 hash;
        uint256 sum;
    }

    function leaf(bytes32 coldkey, uint256 value) internal pure returns (Node memory) {
        return Node(LedgerTree.leafHash(coldkey, value), value);
    }

    /// @dev The honest tree of accounts (coldkeys[i], values[i]).
    function build(bytes32[] memory coldkeys, uint256[] memory values)
        internal
        pure
        returns (Node[][] memory levels)
    {
        uint256 d = LedgerTree.depth(coldkeys.length);
        levels = new Node[][](d + 1);
        levels[0] = new Node[](2 ** d);
        for (uint256 i; i < coldkeys.length; i++) {
            levels[0][i] = leaf(coldkeys[i], values[i]);
        }
        rehash(levels, 0);
    }

    /// @dev Recomputes every level above `level` from the level below it.
    function rehash(Node[][] memory levels, uint256 level) internal pure {
        for (uint256 k = level; k + 1 < levels.length; k++) {
            Node[] memory below = levels[k];
            Node[] memory above = new Node[](below.length / 2);
            for (uint256 j; j < above.length; j++) {
                Node memory left = below[2 * j];
                Node memory right = below[2 * j + 1];
                above[j] = Node(
                    LedgerTree.nodeHash(left.hash, left.sum, right.hash, right.sum), left.sum + right.sum
                );
            }
            levels[k + 1] = above;
        }
    }

    function root(Node[][] memory levels) internal pure returns (Node memory) {
        return levels[levels.length - 1][0];
    }

    /// @dev The bottom-up claim proof of account `index`: its sibling at each
    ///      level 0 … d−1.
    function proof(Node[][] memory levels, uint256 index)
        internal
        pure
        returns (LedgerTree.ProofNode[] memory siblings)
    {
        uint256 d = levels.length - 1;
        siblings = new LedgerTree.ProofNode[](d);
        for (uint256 k; k < d; k++) {
            Node memory sibling = levels[k][(index >> k) ^ 1];
            siblings[k] = LedgerTree.ProofNode(sibling.hash, sibling.sum);
        }
    }

    /// @dev The top-down path to account `index`: the preimage of the node on
    ///      its path at each level d … 1.
    function path(Node[][] memory levels, uint256 index)
        internal
        pure
        returns (LedgerTree.PathNode[] memory preimages)
    {
        uint256 d = levels.length - 1;
        preimages = new LedgerTree.PathNode[](d);
        for (uint256 i; i < d; i++) {
            uint256 level = d - i;
            uint256 left = (index >> level) << 1;
            Node memory l = levels[level - 1][left];
            Node memory r = levels[level - 1][left + 1];
            preimages[i] = LedgerTree.PathNode(l.hash, l.sum, r.hash, r.sum);
        }
    }
}
