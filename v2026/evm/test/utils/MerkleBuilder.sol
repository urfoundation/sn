// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

/// @dev Arbitrary-size sorted-pair Merkle tree builder for tests.
///      Node rule: keccak256(sorted(a, b)); an odd trailing node is PROMOTED
///      to the next level. Every proof it emits verifies with OZ
///      `MerkleProof.verify` (each step is the same commutative hash), which
///      is all the contract requires — Go-tree parity is pinned separately by
///      the shared merkle_vectors.json fixtures in test/Claims.t.sol.
library MerkleBuilder {
    function hashPair(bytes32 a, bytes32 b) internal pure returns (bytes32) {
        return a < b ? keccak256(abi.encodePacked(a, b)) : keccak256(abi.encodePacked(b, a));
    }

    /// @dev All levels of the tree, level[0] = leaves, last level = [root].
    function levels(bytes32[] memory leaves) internal pure returns (bytes32[][] memory lv) {
        require(leaves.length > 0, "MerkleBuilder: empty");
        uint256 depth = 1;
        for (uint256 n = leaves.length; n > 1; n = (n + 1) / 2) {
            depth++;
        }
        lv = new bytes32[][](depth);
        lv[0] = leaves;
        for (uint256 d = 1; d < depth; d++) {
            bytes32[] memory prev = lv[d - 1];
            uint256 n = prev.length;
            bytes32[] memory next = new bytes32[]((n + 1) / 2);
            for (uint256 i = 0; i + 1 < n; i += 2) {
                next[i / 2] = hashPair(prev[i], prev[i + 1]);
            }
            if (n % 2 == 1) {
                next[n / 2] = prev[n - 1]; // promote the odd node
            }
            lv[d] = next;
        }
    }

    function root(bytes32[] memory leaves) internal pure returns (bytes32) {
        bytes32[][] memory lv = levels(leaves);
        return lv[lv.length - 1][0];
    }

    /// @dev Merkle proof for leaves[index] (siblings bottom-up; promoted
    ///      levels contribute no element).
    function proof(bytes32[] memory leaves, uint256 index)
        internal
        pure
        returns (bytes32[] memory p)
    {
        require(index < leaves.length, "MerkleBuilder: index");
        bytes32[][] memory lv = levels(leaves);
        uint256 depth = lv.length;
        bytes32[] memory scratch = new bytes32[](depth);
        uint256 k = 0;
        uint256 idx = index;
        for (uint256 d = 0; d + 1 < depth; d++) {
            uint256 n = lv[d].length;
            uint256 sib = idx ^ 1;
            if (sib < n) {
                scratch[k++] = lv[d][sib];
            }
            idx /= 2;
        }
        p = new bytes32[](k);
        for (uint256 i = 0; i < k; i++) {
            p[i] = scratch[i];
        }
    }
}
