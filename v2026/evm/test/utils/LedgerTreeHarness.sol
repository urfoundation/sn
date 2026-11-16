// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {LedgerTree} from "../../src/LedgerTree.sol";

/// @dev Exposes LedgerTree's internal functions with calldata arguments, as
///      vault2 calls them.
contract LedgerTreeHarness {
    function leafHash(bytes32 coldkey, uint256 value) external pure returns (bytes32) {
        return LedgerTree.leafHash(coldkey, value);
    }

    function nodeHash(bytes32 leftHash, uint256 leftSum, bytes32 rightHash, uint256 rightSum)
        external
        pure
        returns (bytes32)
    {
        return LedgerTree.nodeHash(leftHash, leftSum, rightHash, rightSum);
    }

    function depth(uint256 leafCount) external pure returns (uint256) {
        return LedgerTree.depth(leafCount);
    }

    function verifyLeaf(
        bytes32 root,
        uint256 total,
        uint256 leafCount,
        LedgerTree.Account calldata account,
        LedgerTree.ProofNode[] calldata proof
    ) external pure returns (bool) {
        return LedgerTree.verifyLeaf(root, total, leafCount, account, proof);
    }

    function walkPath(
        bytes32 root,
        uint256 total,
        uint256 leafCount,
        LedgerTree.Account calldata account,
        LedgerTree.PathNode[] calldata path
    ) external pure returns (LedgerTree.Fault fault, uint256 level) {
        return LedgerTree.walkPath(root, total, leafCount, account, path);
    }
}
