// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {Math} from "@openzeppelin/contracts/utils/math/Math.sol";

/// @title LedgerTree
/// @notice The position-binding Merkle sum tree `urnetwork-ledger-sum-tree-v1`
/// (VERSION2-FINAL.md §6.5). Account i of a pool's ledger is the leaf
/// (coldkey_i, cumulative_i) at position i. Every internal node commits to both
/// children's hashes and stated sums, so a proof binds an account's position,
/// coldkey and value to the root and its total. merkle/ledger_sum_tree.go is
/// the reference implementation; both are checked against
/// merkle/testdata/ledger_v2_vectors.json.
///
///   leafHash(c, v)        = keccak256(0x00 ‖ c ‖ uint256(v))
///   nodeHash(hL,sL,hR,sR) = keccak256(0x01 ‖ hL ‖ uint256(sL) ‖ hR ‖ uint256(sR))
///   level 0, position i   = (leafHash(c_i, v_i), v_i) for i < n; (0x00…00, 0) for n <= i < 2^d
///   level k+1, position j = (nodeHash(L.h, L.s, R.h, R.s), L.s + R.s)
///   d = 0 if n == 1, else ceil(log2 n); the root is level d, position 0.
library LedgerTree {
    /// @notice Largest account count a tree can hold.
    uint256 internal constant MAX_LEAF_COUNT = 1 << 29;

    /// @notice A fault found by walkPath, listed in check order.
    enum Fault {
        None,
        // A node's stated child sums do not add up to the node's own sum.
        InconsistentNode,
        // The account's stated value is below its previous value.
        LowerValue,
        // The leaf hash does not commit to (previous coldkey, stated value).
        WrongLeaf
    }

    /// @notice An account at a fixed position of a pool's ledger.
    struct Account {
        uint256 index;
        bytes32 coldkey;
        uint256 cumulative;
    }

    /// @notice One sibling of a bottom-up claim proof, for levels 0 … d−1.
    struct ProofNode {
        bytes32 hash;
        uint256 sum;
    }

    /// @notice The preimage of one internal node of a top-down path, root first.
    struct PathNode {
        bytes32 leftHash;
        uint256 leftSum;
        bytes32 rightHash;
        uint256 rightSum;
    }

    error InvalidLeafCount();
    error MalformedPath();

    function leafHash(bytes32 coldkey, uint256 value) internal pure returns (bytes32) {
        return keccak256(abi.encodePacked(bytes1(0x00), coldkey, value));
    }

    function nodeHash(bytes32 leftHash, uint256 leftSum, bytes32 rightHash, uint256 rightSum)
        internal
        pure
        returns (bytes32)
    {
        return keccak256(abi.encodePacked(bytes1(0x01), leftHash, leftSum, rightHash, rightSum));
    }

    /// @notice The depth of a tree with `leafCount` accounts: the least d with
    /// 2^d >= leafCount, which is log2(leafCount) rounded up.
    function depth(uint256 leafCount) internal pure returns (uint256) {
        if (leafCount == 0 || leafCount > MAX_LEAF_COUNT) revert InvalidLeafCount();
        return Math.log2(leafCount, Math.Rounding.Ceil);
    }

    /// @notice True exactly when `proof` folds `account` at its index into
    /// the root and the total of the tree (root, total, leafCount).
    /// @dev Bit k of the index says whether the running node is the right
    /// child at level k. A sum that overflows uint256 reverts.
    function verifyLeaf(
        bytes32 root,
        uint256 total,
        uint256 leafCount,
        Account calldata account,
        ProofNode[] calldata proof
    ) internal pure returns (bool) {
        uint256 d = depth(leafCount);
        uint256 index = account.index;
        if (index >= leafCount || proof.length != d) return false;
        bytes32 h = leafHash(account.coldkey, account.cumulative);
        uint256 s = account.cumulative;
        for (uint256 k; k < d; k++) {
            ProofNode calldata sibling = proof[k];
            if ((index >> k) & 1 == 1) {
                h = nodeHash(sibling.hash, sibling.sum, h, s);
            } else {
                h = nodeHash(h, s, sibling.hash, sibling.sum);
            }
            s = sibling.sum + s;
        }
        return h == root && s == total;
    }

    /// @notice Opens the path to `account.index` in the tree (root, total,
    /// leafCount) and reports the first fault against the previous account
    /// (index, coldkey, cumulative), with the level where it was found
    /// (d at the root, 0 at the leaf). Reverts MalformedPath when a preimage
    /// does not hash to its node, the path length is not d, or the index is
    /// out of range.
    function walkPath(
        bytes32 root,
        uint256 total,
        uint256 leafCount,
        Account calldata account,
        PathNode[] calldata path
    ) internal pure returns (Fault fault, uint256 level) {
        uint256 d = depth(leafCount);
        uint256 index = account.index;
        if (index >= leafCount || path.length != d) revert MalformedPath();
        bytes32 h = root;
        uint256 s = total;
        for (uint256 i; i < d; i++) {
            PathNode calldata node = path[i];
            level = d - i;
            if (nodeHash(node.leftHash, node.leftSum, node.rightHash, node.rightSum) != h) {
                revert MalformedPath();
            }
            if (node.leftSum > s || node.rightSum != s - node.leftSum) {
                return (Fault.InconsistentNode, level);
            }
            if ((index >> (level - 1)) & 1 == 1) {
                (h, s) = (node.rightHash, node.rightSum);
            } else {
                (h, s) = (node.leftHash, node.leftSum);
            }
        }
        if (s < account.cumulative) return (Fault.LowerValue, 0);
        if (h != leafHash(account.coldkey, s)) return (Fault.WrongLeaf, 0);
        return (Fault.None, 0);
    }
}
