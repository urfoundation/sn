// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {Test} from "forge-std/Test.sol";
import {stdError} from "forge-std/StdError.sol";

import {LedgerTree} from "../src/LedgerTree.sol";
import {LedgerTreeBuilder} from "./utils/LedgerTreeBuilder.sol";
import {LedgerTreeHarness} from "./utils/LedgerTreeHarness.sol";

/// @dev LedgerTree.sol on trees built and stated here (VERSION2-FINAL.md
///      §6.5, §15.1). The Go vectors are checked in V2LedgerTreeVectors.t.sol.
contract V2LedgerTreeTest is Test {
    using LedgerTreeBuilder for LedgerTreeBuilder.Tree;

    LedgerTreeHarness internal tree;

    function setUp() public {
        tree = new LedgerTreeHarness();
    }

    function _walk(LedgerTreeBuilder.Tree memory p, LedgerTree.Account memory account)
        internal
        view
        returns (LedgerTree.Fault, uint256)
    {
        (bytes32 root, uint256 total) = p.root();
        return tree.walkPath(root, total, p.leafCount, account, p.path(account.index));
    }

    function _assertWalk(
        LedgerTreeBuilder.Tree memory p,
        LedgerTree.Account memory account,
        LedgerTree.Fault want,
        uint256 wantLevel,
        string memory what
    ) internal view {
        (LedgerTree.Fault fault, uint256 level) = _walk(p, account);
        assertEq(uint8(fault), uint8(want), what);
        assertEq(level, wantLevel, what);
    }

    function _expectMalformed(
        LedgerTreeBuilder.Tree memory p,
        LedgerTree.Account memory account,
        LedgerTree.PathNode[] memory path
    ) internal {
        (bytes32 root, uint256 total) = p.root();
        vm.expectRevert(LedgerTree.MalformedPath.selector);
        tree.walkPath(root, total, p.leafCount, account, path);
    }

    /// @dev Five accounts F and a nine-account honest successor P (d = 4).
    function _pair()
        internal
        pure
        returns (LedgerTree.Account[] memory f, LedgerTree.Account[] memory next, LedgerTree.Account memory a)
    {
        f = LedgerTreeBuilder.deriveAccounts(1, 5);
        next = LedgerTreeBuilder.successor(f, 1, 2, 9);
        a = f[2];
    }

    /// T-1: depth refuses no accounts and more than 2^29, up to the uint256
    /// maximum, and gives 29 at 2^29.
    function test_depthRefusesOutOfRangeLeafCounts() public {
        assertEq(tree.depth(1 << 29), 29);
        uint256[3] memory invalid = [uint256(0), (1 << 29) + 1, type(uint256).max];
        for (uint256 i; i < invalid.length; i++) {
            vm.expectRevert(LedgerTree.InvalidLeafCount.selector);
            tree.depth(invalid[i]);
        }
    }

    /// T-1: depth is the least d with 2^d at least the leaf count, at every
    /// power of two up to 2^29 and next to it.
    function test_depthAtEveryPowerOfTwo() public view {
        assertEq(tree.depth(1), 0);
        for (uint256 d = 1; d <= 29; d++) {
            uint256 power = uint256(1) << d;
            assertEq(tree.depth(power), d);
            assertEq(tree.depth(power / 2 + 1), d);
            if (d < 29) assertEq(tree.depth(power + 1), d + 1);
        }
    }

    /// T-1: likewise for any leaf count.
    function testFuzz_depthIsTheLeastDepthHoldingTheLeaves(uint256 leafCount) public view {
        leafCount = bound(leafCount, 1, LedgerTree.MAX_LEAF_COUNT);
        uint256 d = tree.depth(leafCount);
        assertGe(uint256(1) << d, leafCount);
        if (d != 0) assertLt(uint256(1) << (d - 1), leafCount);
    }

    /// T-3: each GT-5 fault, stated in a tree built here, is found with its
    /// kind at its level: a lower value, a changed coldkey, a garbage leaf, an
    /// inconsistent node at every level (child sums off, exceeding, or adding
    /// up only modulo 2^256) and root sums not equal to the total; a fault
    /// higher up is found first, and a lower value before a wrong leaf.
    function test_walkPathFindsEachFault() public view {
        (LedgerTree.Account[] memory f, LedgerTree.Account[] memory next, LedgerTree.Account memory a) =
            _pair();
        _assertWalk(LedgerTreeBuilder.build(next), a, LedgerTree.Fault.None, 0, "successor");
        _assertWalk(LedgerTreeBuilder.build(f), a, LedgerTree.Fault.None, 0, "unchanged tree");

        uint256 lowered = a.cumulative - 1;
        uint256 stated = next[2].cumulative;
        bytes32 garbage = keccak256("garbage");
        LedgerTreeBuilder.Tree memory p = LedgerTreeBuilder.build(next);
        p.setLeaf(2, LedgerTree.leafHash(a.coldkey, lowered), lowered);
        _assertWalk(p, a, LedgerTree.Fault.LowerValue, 0, "lower value");
        p.breakSum(2, 2, false);
        _assertWalk(p, a, LedgerTree.Fault.InconsistentNode, 2, "inconsistent above a lower value");
        p = LedgerTreeBuilder.build(next);
        p.setLeaf(2, LedgerTree.leafHash(a.coldkey ^ bytes32(uint256(1)), stated), stated);
        _assertWalk(p, a, LedgerTree.Fault.WrongLeaf, 0, "changed coldkey");
        p = LedgerTreeBuilder.build(next);
        p.setLeaf(2, garbage, stated);
        _assertWalk(p, a, LedgerTree.Fault.WrongLeaf, 0, "garbage leaf");
        p = LedgerTreeBuilder.build(next);
        p.setLeaf(2, garbage, lowered);
        _assertWalk(p, a, LedgerTree.Fault.LowerValue, 0, "garbage leaf with a lower value");

        for (uint256 level = 1; level <= 4; level++) {
            p = LedgerTreeBuilder.build(next);
            p.breakSum(2, level, false);
            _assertWalk(p, a, LedgerTree.Fault.InconsistentNode, level, "right sum off");
            p = LedgerTreeBuilder.build(next);
            p.breakSum(2, level, true);
            _assertWalk(p, a, LedgerTree.Fault.InconsistentNode, level, "left sum exceeds");
            p = LedgerTreeBuilder.build(next);
            p.wrapSums(2, level);
            _assertWalk(p, a, LedgerTree.Fault.InconsistentNode, level, "sums wrap");
        }
        p = LedgerTreeBuilder.build(next);
        p.levels[4][0].sum -= 1;
        _assertWalk(p, a, LedgerTree.Fault.InconsistentNode, 4, "root sums above the total");
        p.levels[4][0].sum += 2;
        _assertWalk(p, a, LedgerTree.Fault.InconsistentNode, 4, "root sums below the total");
    }

    /// T-3: with one account (d = 0) the leaf level is (root, total).
    function test_walkPathAtDepthZero() public view {
        LedgerTree.Account[] memory f = LedgerTreeBuilder.deriveAccounts(3, 1);
        LedgerTree.Account memory a = f[0];
        _assertWalk(LedgerTreeBuilder.build(f), a, LedgerTree.Fault.None, 0, "unchanged");
        LedgerTree.Account[] memory p = new LedgerTree.Account[](1);
        p[0] = LedgerTree.Account(0, a.coldkey, a.cumulative + 1);
        _assertWalk(LedgerTreeBuilder.build(p), a, LedgerTree.Fault.None, 0, "raised");
        p[0].cumulative = a.cumulative - 1;
        _assertWalk(LedgerTreeBuilder.build(p), a, LedgerTree.Fault.LowerValue, 0, "lower value");
        p[0] = LedgerTree.Account(0, a.coldkey ^ bytes32(uint256(1)), a.cumulative);
        _assertWalk(LedgerTreeBuilder.build(p), a, LedgerTree.Fault.WrongLeaf, 0, "wrong leaf");
    }

    /// T-3: walkPath reverts MalformedPath on a preimage that does not hash to
    /// its node (at the root, at the lowest level, at a garbage node, and on an
    /// inconsistent node, hashes being checked first), on a path of the wrong
    /// length, and on an index at or above the leaf count; and InvalidLeafCount
    /// on an invalid leaf count.
    function test_walkPathRevertsOnMalformedPath() public {
        (, LedgerTree.Account[] memory next, LedgerTree.Account memory a) = _pair();
        LedgerTreeBuilder.Tree memory p = LedgerTreeBuilder.build(next);
        LedgerTree.PathNode[] memory path = p.path(2);

        path[0].leftSum ^= 1;
        _expectMalformed(p, a, path);
        path[0].leftSum ^= 1;
        path[3].rightHash ^= bytes32(uint256(1));
        _expectMalformed(p, a, path);
        path[3].rightHash ^= bytes32(uint256(1));

        LedgerTreeBuilder.Tree memory withheld = LedgerTreeBuilder.build(next);
        withheld.levels[2][0].hash = keccak256("garbage");
        withheld.relink(2, 0);
        _expectMalformed(withheld, a, withheld.path(2));

        LedgerTreeBuilder.Tree memory hidden = LedgerTreeBuilder.build(next);
        hidden.breakSum(2, 3, false);
        _assertWalk(hidden, a, LedgerTree.Fault.InconsistentNode, 3, "inconsistent before tampering");
        LedgerTree.PathNode[] memory tampered = hidden.path(2);
        tampered[1].leftHash ^= bytes32(uint256(1));
        _expectMalformed(hidden, a, tampered);

        LedgerTree.PathNode[] memory shorter = new LedgerTree.PathNode[](3);
        LedgerTree.PathNode[] memory longer = new LedgerTree.PathNode[](5);
        for (uint256 i; i < 4; i++) {
            if (i < 3) shorter[i] = path[i];
            longer[i] = path[i];
        }
        longer[4] = path[3];
        _expectMalformed(p, a, shorter);
        _expectMalformed(p, a, longer);
        _expectMalformed(p, a, new LedgerTree.PathNode[](0));

        // A genuine opening of a padding position, and an index far above n.
        _expectMalformed(p, LedgerTree.Account(9, a.coldkey, a.cumulative), p.path(9));
        _expectMalformed(p, LedgerTree.Account(type(uint256).max, a.coldkey, a.cumulative), path);

        LedgerTree.Account[] memory one = LedgerTreeBuilder.deriveAccounts(3, 1);
        _expectMalformed(LedgerTreeBuilder.build(one), one[0], new LedgerTree.PathNode[](1));

        (bytes32 root, uint256 total) = p.root();
        uint256[2] memory invalid = [uint256(0), (1 << 29) + 1];
        for (uint256 i; i < invalid.length; i++) {
            vm.expectRevert(LedgerTree.InvalidLeafCount.selector);
            tree.walkPath(root, total, invalid[i], a, path);
        }
    }

    /// T-3: every index of a fuzzed honest tree verifies and walks with no
    /// fault, in the tree itself and in an honest successor that raised
    /// values and appended accounts.
    function testFuzz_honestTreesHaveNoFault(uint256 seed, uint256 n, uint256 extra, uint64 raise)
        public
        view
    {
        n = bound(n, 1, 40);
        extra = bound(extra, 0, 24);
        LedgerTree.Account[] memory f = LedgerTreeBuilder.deriveAccounts(seed, n);
        LedgerTreeBuilder.Tree memory t = LedgerTreeBuilder.build(f);
        LedgerTreeBuilder.Tree memory p =
            LedgerTreeBuilder.build(LedgerTreeBuilder.successor(f, raise, ~seed, n + extra));
        (bytes32 root, uint256 total) = t.root();
        for (uint256 i; i < n; i++) {
            assertTrue(tree.verifyLeaf(root, total, n, f[i], t.proof(i)), "proof");
            _assertWalk(t, f[i], LedgerTree.Fault.None, 0, "same tree");
            _assertWalk(p, f[i], LedgerTree.Fault.None, 0, "successor");
        }
    }

    /// T-3: a fault stated at a fuzzed index, and for an inconsistent node at
    /// a fuzzed level, of a fuzzed successor is found with its kind there.
    function testFuzz_statedFaultIsFound(uint256 seed, uint256 n, uint256 index, uint256 level, uint256 kind)
        public
        view
    {
        n = bound(n, 1, 40);
        index = bound(index, 0, n - 1);
        LedgerTree.Account[] memory f = LedgerTreeBuilder.deriveAccounts(seed, n);
        LedgerTree.Account memory a = f[index];
        LedgerTreeBuilder.Tree memory p =
            LedgerTreeBuilder.build(LedgerTreeBuilder.successor(f, 1, ~seed, n + n / 2));
        uint256 stated = p.levels[0][index].sum;
        uint256 d = p.depth();
        kind = bound(kind, 0, d > 0 ? 5 : 2);
        if (kind == 0) {
            p.setLeaf(index, LedgerTree.leafHash(a.coldkey, a.cumulative - 1), a.cumulative - 1);
            _assertWalk(p, a, LedgerTree.Fault.LowerValue, 0, "lower value");
        } else if (kind == 1) {
            p.setLeaf(index, LedgerTree.leafHash(a.coldkey ^ bytes32(seed | 1), stated), stated);
            _assertWalk(p, a, LedgerTree.Fault.WrongLeaf, 0, "changed coldkey");
        } else if (kind == 2) {
            p.setLeaf(index, keccak256(abi.encode(seed)), stated);
            _assertWalk(p, a, LedgerTree.Fault.WrongLeaf, 0, "garbage leaf");
        } else if (kind == 3) {
            p.levels[d][0].sum ^= 1 + (seed % 1000);
            _assertWalk(p, a, LedgerTree.Fault.InconsistentNode, d, "root sums not the total");
        } else {
            level = bound(level, 1, d);
            if (kind == 4) {
                p.breakSum(index, level, seed & 1 == 1);
            } else {
                p.wrapSums(index, level);
            }
            _assertWalk(p, a, LedgerTree.Fault.InconsistentNode, level, "inconsistent node");
        }
    }

    /// T-4: a claim proof whose running sum overflows uint256 reverts, at the
    /// first level and at a later one, while the largest sum that does not
    /// overflow is only a mismatch.
    function test_verifyLeafRevertsOnSumOverflow() public {
        LedgerTree.Account[] memory f = LedgerTreeBuilder.deriveAccounts(4, 4);
        LedgerTreeBuilder.Tree memory t = LedgerTreeBuilder.build(f);
        (bytes32 root, uint256 total) = t.root();
        for (uint256 k; k < 2; k++) {
            LedgerTree.ProofNode[] memory proof = t.proof(1);
            proof[k].sum = type(uint256).max;
            vm.expectRevert(stdError.arithmeticError);
            tree.verifyLeaf(root, total, 4, f[1], proof);
        }
        LedgerTree.ProofNode[] memory last = new LedgerTree.ProofNode[](1);
        last[0] = LedgerTree.ProofNode(bytes32(0), type(uint256).max - f[1].cumulative);
        assertFalse(tree.verifyLeaf(root, total, 2, f[1], last));
    }

    /// T-4: two values whose sum wraps around 2^256 to the stated total fold
    /// to the root, and only the overflow refuses the claim.
    function test_verifyLeafRevertsOnSumsThatWrapToTheTotal() public {
        uint256 half = 1 << 255;
        LedgerTree.Account memory a = LedgerTree.Account(0, keccak256("a"), half);
        LedgerTree.ProofNode[] memory proof = new LedgerTree.ProofNode[](1);
        proof[0] = LedgerTreeBuilder.leaf(keccak256("b"), half + 5);
        bytes32 root =
            LedgerTree.nodeHash(LedgerTree.leafHash(a.coldkey, half), half, proof[0].hash, proof[0].sum);
        vm.expectRevert(stdError.arithmeticError);
        tree.verifyLeaf(root, 5, 2, a, proof);
    }
}
