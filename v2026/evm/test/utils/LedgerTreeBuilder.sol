// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {LedgerTree} from "../../src/LedgerTree.sol";

/// @dev Builds ledger sum trees (`urnetwork-ledger-sum-tree-v1`) in memory
///      for tests. Every position of every level is held as a (hash, sum)
///      node, level 0 padded to 2^d with (0x00…00, 0), so a test can read any
///      claim proof or opening and also state any node dishonestly. Go parity
///      is pinned by the shared vectors in test/V2LedgerTreeVectors.t.sol.
library LedgerTreeBuilder {
    struct Tree {
        uint256 leafCount;
        /// @dev levels[k][j] is position j of level k; levels[d][0] is the root.
        LedgerTree.ProofNode[][] levels;
    }

    /// @dev Accounts 0 … n−1 from a seed: distinct coldkeys, and values from 1
    ///      to 2^127 of every magnitude.
    function deriveAccounts(uint256 seed, uint256 n)
        internal
        pure
        returns (LedgerTree.Account[] memory accounts)
    {
        accounts = new LedgerTree.Account[](n);
        for (uint256 i; i < n; i++) {
            uint256 h = uint256(keccak256(abi.encode(seed, "value", i)));
            accounts[i] = LedgerTree.Account(
                i, keccak256(abi.encode(seed, "coldkey", i)), 1 + (h >> (129 + (h & 127)))
            );
        }
    }

    /// @dev An honest successor of n accounts: account i keeps its coldkey and
    ///      gains `raise * i`, and accounts derived from `seed` are appended.
    function successor(LedgerTree.Account[] memory accounts, uint256 raise, uint256 seed, uint256 n)
        internal
        pure
        returns (LedgerTree.Account[] memory next)
    {
        next = deriveAccounts(seed, n);
        for (uint256 i; i < accounts.length; i++) {
            next[i] = LedgerTree.Account(i, accounts[i].coldkey, accounts[i].cumulative + raise * i);
        }
    }

    function build(LedgerTree.Account[] memory accounts) internal pure returns (Tree memory t) {
        uint256 n = accounts.length;
        require(n > 0, "LedgerTreeBuilder: empty");
        uint256 d;
        while ((uint256(1) << d) < n) {
            d++;
        }
        t.leafCount = n;
        t.levels = new LedgerTree.ProofNode[][](d + 1);
        LedgerTree.ProofNode[] memory level = new LedgerTree.ProofNode[](uint256(1) << d);
        for (uint256 i; i < level.length; i++) {
            if (i < n) {
                require(accounts[i].index == i, "LedgerTreeBuilder: index");
                level[i] = leaf(accounts[i].coldkey, accounts[i].cumulative);
            } else {
                level[i] = LedgerTree.ProofNode(bytes32(0), 0);
            }
        }
        t.levels[0] = level;
        for (uint256 k; k < d; k++) {
            LedgerTree.ProofNode[] memory below = t.levels[k];
            LedgerTree.ProofNode[] memory above = new LedgerTree.ProofNode[](below.length / 2);
            for (uint256 j; j < above.length; j++) {
                above[j] = parent(below[2 * j], below[2 * j + 1]);
            }
            t.levels[k + 1] = above;
        }
    }

    function leaf(bytes32 coldkey, uint256 value) internal pure returns (LedgerTree.ProofNode memory) {
        return LedgerTree.ProofNode(LedgerTree.leafHash(coldkey, value), value);
    }

    function parent(LedgerTree.ProofNode memory left, LedgerTree.ProofNode memory right)
        internal
        pure
        returns (LedgerTree.ProofNode memory)
    {
        return LedgerTree.ProofNode(
            LedgerTree.nodeHash(left.hash, left.sum, right.hash, right.sum), left.sum + right.sum
        );
    }

    function depth(Tree memory t) internal pure returns (uint256) {
        return t.levels.length - 1;
    }

    function root(Tree memory t) internal pure returns (bytes32 hash, uint256 total) {
        LedgerTree.ProofNode memory r = t.levels[t.levels.length - 1][0];
        return (r.hash, r.sum);
    }

    /// @dev The claim proof of `index` as the tree states it, bottom-up.
    function proof(Tree memory t, uint256 index) internal pure returns (LedgerTree.ProofNode[] memory p) {
        p = new LedgerTree.ProofNode[](depth(t));
        for (uint256 k; k < p.length; k++) {
            LedgerTree.ProofNode memory sibling = t.levels[k][(index >> k) ^ 1];
            p[k] = LedgerTree.ProofNode(sibling.hash, sibling.sum);
        }
    }

    /// @dev The opening of `index` as the tree states it, root first.
    function path(Tree memory t, uint256 index) internal pure returns (LedgerTree.PathNode[] memory p) {
        uint256 d = depth(t);
        p = new LedgerTree.PathNode[](d);
        for (uint256 i; i < d; i++) {
            uint256 j = index >> (d - i);
            LedgerTree.ProofNode memory left = t.levels[d - i - 1][2 * j];
            LedgerTree.ProofNode memory right = t.levels[d - i - 1][2 * j + 1];
            p[i] = LedgerTree.PathNode(left.hash, left.sum, right.hash, right.sum);
        }
    }

    /// @dev Rehashes every ancestor of position j at level k and keeps every
    ///      stated sum, so a node changed below stays reachable from the root.
    function relink(Tree memory t, uint256 k, uint256 j) internal pure {
        for (uint256 d = depth(t); k < d; k++) {
            j >>= 1;
            LedgerTree.ProofNode memory left = t.levels[k][2 * j];
            LedgerTree.ProofNode memory right = t.levels[k][2 * j + 1];
            t.levels[k + 1][j].hash = LedgerTree.nodeHash(left.hash, left.sum, right.hash, right.sum);
        }
    }

    /// @dev Recomputes every ancestor of position j at level k honestly.
    function restate(Tree memory t, uint256 k, uint256 j) internal pure {
        for (uint256 d = depth(t); k < d; k++) {
            j >>= 1;
            t.levels[k + 1][j] = parent(t.levels[k][2 * j], t.levels[k][2 * j + 1]);
        }
    }

    /// @dev States level-0 position `index` as (hash, sum), with honest ancestors.
    function setLeaf(Tree memory t, uint256 index, bytes32 hash, uint256 sum) internal pure {
        t.levels[0][index] = LedgerTree.ProofNode(hash, sum);
        restate(t, 0, index);
    }

    /// @dev States the node above `index` at `level` so that its children's
    ///      sums no longer add up to its own: its left child's sum exceeds it,
    ///      or its right child's sum is one off. Every node above stays
    ///      consistent, so a walk finds the fault exactly there.
    function breakSum(Tree memory t, uint256 index, uint256 level, bool leftExceeds) internal pure {
        uint256 j = index >> level;
        if (leftExceeds) {
            t.levels[level - 1][2 * j].sum = t.levels[level][j].sum + 1;
            relink(t, level - 1, 2 * j);
        } else {
            t.levels[level - 1][2 * j + 1].sum ^= 1;
            relink(t, level - 1, 2 * j + 1);
        }
    }

    /// @dev States the node above `index` at `level` with child sums that add
    ///      up to its own only modulo 2^256: the left one exceeds it by one and
    ///      the right one is 2^256 − 1.
    function wrapSums(Tree memory t, uint256 index, uint256 level) internal pure {
        uint256 j = index >> level;
        t.levels[level - 1][2 * j].sum = t.levels[level][j].sum + 1;
        t.levels[level - 1][2 * j + 1].sum = type(uint256).max;
        relink(t, level - 1, 2 * j);
    }
}
