// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {Test} from "forge-std/Test.sol";
import {stdError} from "forge-std/StdError.sol";
import {stdJson} from "forge-std/StdJson.sol";

import {LedgerTree} from "../src/LedgerTree.sol";
import {LedgerTreeBuilder} from "./utils/LedgerTreeBuilder.sol";
import {LedgerTreeHarness} from "./utils/LedgerTreeHarness.sol";

/// @dev LedgerTree.sol against the shared Go vectors,
///      sn/merkle/testdata/ledger_v2_vectors.json, which
///      merkle/ledger_v2_vectors_test.go writes and documents. Uint256 fields
///      are decimal strings, decoded with vm.parseJsonType and explicit type
///      descriptions, which match JSON keys by name and coerce by the declared
///      type. The descriptions' type names are unique so that Foundry does not
///      resolve them to the project's structs of the same name.
contract V2LedgerTreeVectorsTest is Test {
    using stdJson for string;
    using LedgerTreeBuilder for LedgerTreeBuilder.Tree;

    struct LeafHashVector {
        bytes32 coldkey;
        bytes32 hash;
        uint256 value;
    }

    struct NodeHashVector {
        bytes32 hash;
        bytes32 leftHash;
        uint256 leftSum;
        bytes32 rightHash;
        uint256 rightSum;
    }

    struct DepthVector {
        uint256 depth;
        uint256 leafCount;
    }

    struct TreeVector {
        LedgerTree.Account[] accounts;
        uint256 depth;
        uint256 leafCount;
        string name;
        LedgerTree.PathNode[][] paths;
        LedgerTree.ProofNode[][] proofs;
        bytes32 root;
        uint256 total;
    }

    struct ProofCase {
        LedgerTree.Account account;
        string error;
        uint256 leafCount;
        string name;
        LedgerTree.ProofNode[] proof;
        bytes32 root;
        uint256 total;
        bool valid;
    }

    struct FaultCase {
        LedgerTree.Account account;
        string error;
        uint8 fault;
        uint256 leafCount;
        uint256 level;
        string name;
        LedgerTree.PathNode[] path;
        bytes32 root;
        uint256 total;
    }

    string internal constant ACCOUNT = "JsonAccount(uint256 index,bytes32 coldkey,uint256 cumulative)";
    string internal constant PATH_NODE =
        "JsonPathNode(bytes32 leftHash,uint256 leftSum,bytes32 rightHash,uint256 rightSum)";
    string internal constant PROOF_NODE = "JsonProofNode(bytes32 hash,uint256 sum)";

    LedgerTreeHarness internal tree;

    function setUp() public {
        tree = new LedgerTreeHarness();
    }

    function _vectors() internal view returns (string memory) {
        return vm.readFile(string.concat(vm.projectRoot(), "/../merkle/testdata/ledger_v2_vectors.json"));
    }

    function _trees() internal view returns (TreeVector[] memory) {
        string memory description = string.concat(
            "JsonTree(JsonAccount[] accounts,uint256 depth,uint256 leafCount,string name,",
            "JsonPathNode[][] paths,JsonProofNode[][] proofs,bytes32 root,uint256 total)",
            ACCOUNT,
            PATH_NODE,
            PROOF_NODE
        );
        return abi.decode(vm.parseJsonTypeArray(_vectors(), ".trees", description), (TreeVector[]));
    }

    function _is(string memory value, string memory want) internal pure returns (bool) {
        return keccak256(bytes(value)) == keccak256(bytes(want));
    }

    /// T-1: leafHash, nodeHash and depth equal the vectors, and depth reverts
    /// InvalidLeafCount on each invalid leaf count.
    function test_hashAndDepthVectors() public {
        string memory json = _vectors();
        LeafHashVector[] memory leaves = abi.decode(
            vm.parseJsonTypeArray(
                json, ".leafHashes", "JsonLeafHash(bytes32 coldkey,bytes32 hash,uint256 value)"
            ),
            (LeafHashVector[])
        );
        assertEq(leaves.length, 6);
        for (uint256 i; i < leaves.length; i++) {
            assertEq(tree.leafHash(leaves[i].coldkey, leaves[i].value), leaves[i].hash, "leafHash");
        }
        NodeHashVector[] memory nodes = abi.decode(
            vm.parseJsonTypeArray(
                json,
                ".nodeHashes",
                "JsonNodeHash(bytes32 hash,bytes32 leftHash,uint256 leftSum,bytes32 rightHash,uint256 rightSum)"
            ),
            (NodeHashVector[])
        );
        assertEq(nodes.length, 5);
        for (uint256 i; i < nodes.length; i++) {
            NodeHashVector memory v = nodes[i];
            assertEq(tree.nodeHash(v.leftHash, v.leftSum, v.rightHash, v.rightSum), v.hash, "nodeHash");
        }
        DepthVector[] memory depths = abi.decode(
            vm.parseJsonTypeArray(json, ".depths", "JsonDepth(uint256 depth,uint256 leafCount)"),
            (DepthVector[])
        );
        assertEq(depths.length, 15);
        for (uint256 i; i < depths.length; i++) {
            assertEq(tree.depth(depths[i].leafCount), depths[i].depth, "depth");
        }
        uint256[] memory invalid = json.readUintArray(".invalidLeafCounts");
        assertEq(invalid.length, 2);
        for (uint256 i; i < invalid.length; i++) {
            vm.expectRevert(LedgerTree.InvalidLeafCount.selector);
            tree.depth(invalid[i]);
        }
    }

    /// X-1, T-2: every vector tree, rebuilt here from its accounts, has the Go
    /// root, total and depth; each account's proof and path are the rebuilt
    /// tree's; every proof verifies and every path walks with no fault.
    function test_treeVectors() public view {
        TreeVector[] memory trees = _trees();
        assertEq(trees.length, 8);
        for (uint256 t; t < trees.length; t++) {
            TreeVector memory v = trees[t];
            LedgerTreeBuilder.Tree memory built = LedgerTreeBuilder.build(v.accounts);
            (bytes32 root, uint256 total) = built.root();
            assertEq(root, v.root, v.name);
            assertEq(total, v.total, v.name);
            assertEq(v.accounts.length, v.leafCount, v.name);
            assertEq(built.depth(), v.depth, v.name);
            assertEq(tree.depth(v.leafCount), v.depth, v.name);
            for (uint256 i; i < v.accounts.length; i++) {
                assertEq(abi.encode(built.proof(i)), abi.encode(v.proofs[i]), v.name);
                assertEq(abi.encode(built.path(i)), abi.encode(v.paths[i]), v.name);
                assertTrue(tree.verifyLeaf(v.root, v.total, v.leafCount, v.accounts[i], v.proofs[i]), v.name);
                (LedgerTree.Fault fault, uint256 level) =
                    tree.walkPath(v.root, v.total, v.leafCount, v.accounts[i], v.paths[i]);
                assertEq(uint8(fault), uint8(LedgerTree.Fault.None), v.name);
                assertEq(level, 0, v.name);
            }
        }
    }

    /// T-2: each single change to every vector claim is rejected: a sibling
    /// hash or sum, an index bit, the coldkey, the cumulative, the proof
    /// length, an index at or above n, and the total one above or below.
    function test_treeVectorsRejectEverySingleChange() public view {
        TreeVector[] memory trees = _trees();
        for (uint256 t; t < trees.length; t++) {
            for (uint256 i; i < trees[t].accounts.length; i++) {
                _rejectEverySingleChange(trees[t], trees[t].accounts[i], trees[t].proofs[i]);
            }
        }
    }

    function _rejects(
        TreeVector memory v,
        LedgerTree.Account memory account,
        LedgerTree.ProofNode[] memory proof,
        string memory change
    ) internal view {
        assertFalse(
            tree.verifyLeaf(v.root, v.total, v.leafCount, account, proof), string.concat(v.name, " ", change)
        );
    }

    function _rejectEverySingleChange(
        TreeVector memory v,
        LedgerTree.Account memory a,
        LedgerTree.ProofNode[] memory proof
    ) internal view {
        for (uint256 k; k < proof.length; k++) {
            proof[k].hash ^= bytes32(uint256(1));
            _rejects(v, a, proof, "sibling hash");
            proof[k].hash ^= bytes32(uint256(1));
            proof[k].sum ^= 1;
            _rejects(v, a, proof, "sibling sum");
            proof[k].sum ^= 1;
        }
        for (uint256 k; k < 64; k++) {
            _rejects(v, LedgerTree.Account(a.index ^ (2 ** k), a.coldkey, a.cumulative), proof, "index bit");
        }
        _rejects(
            v, LedgerTree.Account(a.index, a.coldkey ^ bytes32(uint256(1)), a.cumulative), proof, "coldkey"
        );
        _rejects(v, LedgerTree.Account(a.index, a.coldkey, a.cumulative + 1), proof, "cumulative above");
        _rejects(v, LedgerTree.Account(a.index, a.coldkey, a.cumulative ^ 1), proof, "cumulative changed");
        LedgerTree.ProofNode[] memory longer = new LedgerTree.ProofNode[](proof.length + 1);
        for (uint256 k; k < proof.length; k++) {
            longer[k] = proof[k];
        }
        longer[proof.length] = LedgerTree.ProofNode(v.root, 0);
        _rejects(v, a, longer, "long proof");
        if (proof.length > 0) {
            LedgerTree.ProofNode[] memory shorter = new LedgerTree.ProofNode[](proof.length - 1);
            for (uint256 k; k < shorter.length; k++) {
                shorter[k] = proof[k];
            }
            _rejects(v, a, shorter, "short proof");
        }
        _rejects(v, LedgerTree.Account(v.leafCount, a.coldkey, a.cumulative), proof, "index at n");
        _rejects(v, LedgerTree.Account(type(uint256).max, a.coldkey, a.cumulative), proof, "index max");
        v.total += 1;
        _rejects(v, a, proof, "total above");
        v.total -= 2;
        _rejects(v, a, proof, "total below");
        v.total += 1;
        // The unchanged claim still verifies.
        assertTrue(tree.verifyLeaf(v.root, v.total, v.leafCount, a, proof), v.name);
    }

    /// X-1, T-2, T-4: each shared claim case gives the Go result. Where Go
    /// returns false for an invalid leaf count or an overflowing sum,
    /// verifyLeaf reverts InvalidLeafCount or with an arithmetic panic.
    function test_proofCases() public {
        ProofCase[] memory cases = abi.decode(
            vm.parseJsonTypeArray(
                _vectors(),
                ".proofCases",
                string.concat(
                    "JsonProofCase(JsonAccount account,string error,uint256 leafCount,string name,",
                    "JsonProofNode[] proof,bytes32 root,uint256 total,bool valid)",
                    ACCOUNT,
                    PROOF_NODE
                )
            ),
            (ProofCase[])
        );
        assertEq(cases.length, 39);
        uint256 reverts;
        for (uint256 i; i < cases.length; i++) {
            ProofCase memory c = cases[i];
            if (_is(c.error, "InvalidLeafCount")) {
                vm.expectRevert(LedgerTree.InvalidLeafCount.selector);
                reverts++;
            } else if (_is(c.error, "ArithmeticOverflow")) {
                vm.expectRevert(stdError.arithmeticError);
                reverts++;
            } else {
                assertEq(c.error, "", c.name);
            }
            assertEq(tree.verifyLeaf(c.root, c.total, c.leafCount, c.account, c.proof), c.valid, c.name);
        }
        assertEq(reverts, 7);
    }

    /// X-2, T-3: each shared path case gives the Go fault kind at the Go
    /// level, and walkPath reverts MalformedPath or InvalidLeafCount exactly
    /// where Go refuses the path with ErrLedgerMalformedPath or
    /// ErrLedgerLeafCount.
    function test_faultCases() public {
        FaultCase[] memory cases = abi.decode(
            vm.parseJsonTypeArray(
                _vectors(),
                ".faultCases",
                string.concat(
                    "JsonFaultCase(JsonAccount account,string error,uint8 fault,uint256 leafCount,uint256 level,",
                    "string name,JsonPathNode[] path,bytes32 root,uint256 total)",
                    ACCOUNT,
                    PATH_NODE
                )
            ),
            (FaultCase[])
        );
        assertEq(cases.length, 36);
        // found[fault][level]: every kind, and an inconsistent node at every level of the d = 4 tree.
        bool[5][4] memory found;
        uint256 malformed;
        uint256 invalid;
        for (uint256 i; i < cases.length; i++) {
            FaultCase memory c = cases[i];
            if (_is(c.error, "MalformedPath")) {
                vm.expectRevert(LedgerTree.MalformedPath.selector);
                malformed++;
            } else if (_is(c.error, "InvalidLeafCount")) {
                vm.expectRevert(LedgerTree.InvalidLeafCount.selector);
                invalid++;
            } else {
                assertEq(c.error, "", c.name);
                found[c.fault][c.level] = true;
            }
            (LedgerTree.Fault fault, uint256 level) =
                tree.walkPath(c.root, c.total, c.leafCount, c.account, c.path);
            assertEq(uint8(fault), c.fault, c.name);
            assertEq(level, c.level, c.name);
        }
        assertTrue(found[uint8(LedgerTree.Fault.None)][0]);
        assertTrue(found[uint8(LedgerTree.Fault.LowerValue)][0]);
        assertTrue(found[uint8(LedgerTree.Fault.WrongLeaf)][0]);
        for (uint256 level = 1; level <= 4; level++) {
            assertTrue(found[uint8(LedgerTree.Fault.InconsistentNode)][level]);
        }
        assertEq(malformed, 10);
        assertEq(invalid, 2);
    }
}
