// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {V2VaultBase} from "./utils/V2VaultBase.sol";
import {V2TreeHelper} from "./utils/V2TreeHelper.sol";
import {ISTSettlementVault2} from "../src/interfaces/ISTSettlementVault2.sol";
import {LedgerTree} from "../src/LedgerTree.sol";
import {SafeCast} from "@openzeppelin/contracts/utils/math/SafeCast.sol";
import {stdError} from "forge-std/StdError.sol";

/// @dev Commits, path checks, demands and finalization (VERSION2-FINAL.md
///      §15.3 V-7 to V-11). The finalized root F has five accounts (depth 3,
///      total 150). Its honest successor P keeps them in place, raises three
///      and appends a sixth (total 200). Faulty successors keep P's total, as
///      vault2's funding binding forces them to. The last V-8 and V-10 tests
///      dispute accounts of several finalized roots.
contract V2VaultRootsTest is V2VaultBase {
    // F's root index: the roots tests finalize F as root 0.
    uint256 internal constant F_ROOT = 0;
    uint256 internal constant CAROL_INDEX = 2;
    uint256 internal constant F_TOTAL = 150;
    uint256 internal constant P_TOTAL = 200;

    function _oldKeys() internal pure returns (bytes32[] memory keys) {
        keys = new bytes32[](5);
        (keys[0], keys[1], keys[2], keys[3], keys[4]) = (ALICE, BOB, CAROL, DAVE, ERIN);
    }

    function _oldValues() internal pure returns (uint256[] memory values) {
        values = new uint256[](5);
        (values[0], values[1], values[2], values[3], values[4]) = (10, 20, 30, 40, 50);
    }

    function _newKeys() internal pure returns (bytes32[] memory) {
        return _keys(ALICE, BOB, CAROL, DAVE, ERIN, FRANK);
    }

    function _newValues() internal pure returns (uint256[] memory) {
        return _values(15, 20, 35, 40, 60, 30);
    }

    // F, finalized as root 0 for FIRST_EPOCH.
    function _publishF() internal returns (V2TreeHelper.Node[][] memory) {
        return _publish(NO1, _oldKeys(), _oldValues());
    }

    function _honestP() internal pure returns (V2TreeHelper.Node[][] memory) {
        return V2TreeHelper.build(_newKeys(), _newValues());
    }

    // Carol is lowered from 30 to 29 and Frank takes the rao.
    function _loweredP() internal pure returns (V2TreeHelper.Node[][] memory) {
        uint256[] memory values = _newValues();
        (values[2], values[5]) = (29, 36);
        return V2TreeHelper.build(_newKeys(), values);
    }

    // Carol's position goes to Mallory.
    function _movedP() internal pure returns (V2TreeHelper.Node[][] memory) {
        bytes32[] memory keys = _newKeys();
        keys[2] = MALLORY;
        return V2TreeHelper.build(keys, _newValues());
    }

    // Carol's position holds a hash that is no leaf hash at all.
    function _garbageLeafP() internal pure returns (V2TreeHelper.Node[][] memory levels) {
        levels = _honestP();
        levels[0][CAROL_INDEX].hash = keccak256("garbage-leaf");
        V2TreeHelper.rehash(levels, 0);
    }

    // The node at `level` on Carol's path states 5 more than its children
    // hold, and Frank holds 5 less, so the root still sums to P_TOTAL. At
    // level 3 the root's own sum, the committed total, is the inconsistent one.
    function _inconsistentP(uint256 level) internal pure returns (V2TreeHelper.Node[][] memory levels) {
        uint256[] memory values = _newValues();
        values[5] = 25;
        levels = V2TreeHelper.build(_newKeys(), values);
        levels[level][CAROL_INDEX >> level].sum += 5;
        V2TreeHelper.rehash(levels, level);
    }

    // The node above Carol and Dave is withheld: its hash has no known preimage.
    function _withheldNodeP() internal pure returns (V2TreeHelper.Node[][] memory levels) {
        levels = _honestP();
        levels[1][CAROL_INDEX >> 1].hash = keccak256("withheld-node");
        V2TreeHelper.rehash(levels, 1);
    }

    function _check(
        V2TreeHelper.Node[][] memory f,
        V2TreeHelper.Node[][] memory p,
        uint256 index,
        bytes32 coldkey,
        uint256 cumulative
    ) internal returns (LedgerTree.Fault) {
        return vault.checkPath(
            NO1,
            F_ROOT,
            _account(index, coldkey, cumulative),
            V2TreeHelper.proof(f, index),
            V2TreeHelper.path(p, index)
        );
    }

    function _checkCarol(V2TreeHelper.Node[][] memory f, V2TreeHelper.Node[][] memory p)
        internal
        returns (LedgerTree.Fault)
    {
        return _check(f, p, CAROL_INDEX, CAROL, 30);
    }

    // F's account at `index` and its proof.
    function _fAccount(uint256 index) internal pure returns (LedgerTree.Account memory) {
        return _account(index, _oldKeys()[index], _oldValues()[index]);
    }

    function _fProof(uint256 index) internal pure returns (LedgerTree.ProofNode[] memory) {
        return V2TreeHelper.proof(V2TreeHelper.build(_oldKeys(), _oldValues()), index);
    }

    // A stranger pays the fee and demands F's account at `index`.
    function _demand(uint256 index) internal {
        LedgerTree.Account memory account = _fAccount(index);
        LedgerTree.ProofNode[] memory proof = _fProof(index);
        vm.deal(stranger, DEMAND_FEE);
        vm.prank(stranger);
        vault.demandPath{value: DEMAND_FEE}(NO1, F_ROOT, account, proof);
    }

    function _expectDemandRefused(
        uint256 rootIndex,
        LedgerTree.Account memory account,
        LedgerTree.ProofNode[] memory proof,
        uint256 fee,
        bytes4 refusal
    ) internal {
        vm.deal(stranger, fee);
        vm.prank(stranger);
        vm.expectRevert(refusal);
        vault.demandPath{value: fee}(NO1, rootIndex, account, proof);
    }

    function _demandState(uint64 seq, uint256 rootIndex, uint256 index) internal view returns (uint8) {
        return uint8(vault.demanded(NO1, seq, rootIndex, index));
    }

    function _raw(bytes32 root, uint256 total, uint64 leafCount)
        internal
        pure
        returns (ISTSettlementVault2.Commitment memory)
    {
        return ISTSettlementVault2.Commitment({
            root: root, total: total, leafCount: leafCount, ledgerHash: _ledgerHash(root)
        });
    }

    // Shows `fault` at `level` on Carol's path deleting the pending root `p`.
    function _assertCarolDeletes(
        V2TreeHelper.Node[][] memory f,
        V2TreeHelper.Node[][] memory p,
        LedgerTree.Fault fault,
        uint256 level
    ) internal {
        uint64 seq = _propose(NO1, p, 6);
        uint64 committedBlock = vault.lastCommitBlock(NO1);
        LedgerTree.Account memory carol = _account(CAROL_INDEX, CAROL, 30);
        LedgerTree.ProofNode[] memory oldProof = V2TreeHelper.proof(f, CAROL_INDEX);
        LedgerTree.PathNode[] memory newPath = V2TreeHelper.path(p, CAROL_INDEX);

        vm.expectEmit(address(vault));
        emit ISTSettlementVault2.DistributionDeleted(NO1, seq, F_ROOT, CAROL_INDEX, fault, level, stranger);
        vm.prank(stranger);
        assertEq(uint8(vault.checkPath(NO1, F_ROOT, carol, oldProof, newPath)), uint8(fault));

        ISTSettlementVault2.Pending memory cleared = vault.pending(NO1);
        assertEq(cleared.seq, 0);
        assertEq(cleared.root, bytes32(0));
        assertEq(cleared.total, 0);
        assertEq(cleared.leafCount, 0);
        assertEq(cleared.epoch, 0);
        assertEq(cleared.ledgerHash, bytes32(0));
        assertEq(cleared.committedBlock, 0);
        assertEq(cleared.openDemands, 0);
        assertEq(vault.rootCount(NO1), 1);
        assertEq(vault.finalizedTotal(NO1), F_TOTAL);
        assertEq(vault.lastCommitBlock(NO1), committedBlock, "a deletion keeps the commit spacing");
        assertEq(vault.commitSeq(NO1), seq);
    }

    // ------------------------------------------------------------------
    // V-7: commitPending
    // ------------------------------------------------------------------

    // V-7, V-EV
    function test_V7_commitRecordsThePendingRoot() public {
        uint256 epoch = _capture(NO1, F_TOTAL);
        V2TreeHelper.Node[][] memory f = V2TreeHelper.build(_oldKeys(), _oldValues());
        ISTSettlementVault2.Commitment memory commitment = _commitment(f, 5);

        vm.expectEmit(address(vault));
        emit ISTSettlementVault2.DistributionCommitted(
            NO1, 1, SafeCast.toUint64(epoch), commitment.root, F_TOTAL, 5, commitment.ledgerHash, 0
        );
        assertEq(vault.commitPending(epoch, NO1, 0, commitment), 1);

        ISTSettlementVault2.Pending memory next = vault.pending(NO1);
        assertEq(next.seq, 1);
        assertEq(next.root, commitment.root);
        assertEq(next.total, F_TOTAL);
        assertEq(next.leafCount, 5);
        assertEq(next.epoch, epoch);
        assertEq(next.ledgerHash, commitment.ledgerHash);
        assertEq(next.committedBlock, block.number);
        assertEq(next.openDemands, 0);
        assertEq(vault.commitSeq(NO1), 1);
        assertEq(vault.lastCommitBlock(NO1), block.number);
        assertEq(vault.rootCount(NO1), 0);
    }

    // V-7
    function test_V7_commitRefusesAnUnknownPool() public {
        ISTSettlementVault2.Commitment memory commitment = _raw(keccak256("root"), 100, 1);
        vm.expectRevert(ISTSettlementVault2.UnknownPool.selector);
        vault.commitPending(FIRST_EPOCH, NO2, 0, commitment);
    }

    // V-7
    function test_V7_commitRefusesWithinTheWindowOfThePreviousCommit() public {
        _publishF();
        V2TreeHelper.Node[][] memory p = _honestP();
        _propose(NO1, p, 6);
        uint256 committed = block.number;
        ISTSettlementVault2.Commitment memory commitment = _commitment(p, 6);

        vm.roll(committed + WINDOW - 1);
        vm.expectRevert(ISTSettlementVault2.CommitTooSoon.selector);
        vault.commitPending(FIRST_EPOCH + 1, NO1, 1, commitment);

        vm.roll(committed + WINDOW);
        assertEq(vault.commitPending(FIRST_EPOCH + 1, NO1, 1, commitment), 3);
    }

    // V-7: an operator that proves a fault in its own root cannot re-roll the
    // demand sample by committing again at once.
    function test_V7_commitRefusesWithinTheWindowAfterASelfProvenFault() public {
        V2TreeHelper.Node[][] memory f = _publishF();
        _propose(NO1, _loweredP(), 6);
        uint256 committed = block.number;
        _checkCarol(f, _loweredP());
        assertEq(vault.pending(NO1).seq, 0);
        ISTSettlementVault2.Commitment memory commitment = _commitment(_honestP(), 6);

        vm.roll(committed + WINDOW - 1);
        vm.expectRevert(ISTSettlementVault2.CommitTooSoon.selector);
        vault.commitPending(FIRST_EPOCH + 1, NO1, 1, commitment);

        vm.roll(committed + WINDOW);
        vault.commitPending(FIRST_EPOCH + 1, NO1, 1, commitment);
    }

    // V-7
    function test_V7_commitRefusesAStaleRootCount() public {
        _capture(NO1, F_TOTAL);
        ISTSettlementVault2.Commitment memory commitment =
            _commitment(V2TreeHelper.build(_oldKeys(), _oldValues()), 5);
        vm.expectRevert(ISTSettlementVault2.StaleRootCount.selector);
        vault.commitPending(FIRST_EPOCH, NO1, 1, commitment);
    }

    // V-7: a finalize between build and commit leaves the build's predecessor stale.
    function test_V7_commitRefusesARootCountFromBeforeAFinalization() public {
        _publishF();
        _capture(NO1, P_TOTAL - F_TOTAL);
        ISTSettlementVault2.Commitment memory commitment = _commitment(_honestP(), 6);
        vm.expectRevert(ISTSettlementVault2.StaleRootCount.selector);
        vault.commitPending(FIRST_EPOCH + 1, NO1, 0, commitment);
    }

    // V-7
    function test_V7_commitRefusesTheEpochBeforeThePoolsFirst() public {
        _capture(NO1, F_TOTAL);
        ISTSettlementVault2.Commitment memory commitment = _raw(keccak256("root"), 0, 1);
        vm.expectRevert(ISTSettlementVault2.EpochOutOfRange.selector);
        vault.commitPending(FIRST_EPOCH - 1, NO1, 0, commitment);
    }

    // V-7
    function test_V7_commitRefusesAnUnfundedEpoch() public {
        _capture(NO1, F_TOTAL);
        ISTSettlementVault2.Commitment memory commitment = _raw(keccak256("root"), F_TOTAL, 1);
        vm.expectRevert(ISTSettlementVault2.EpochOutOfRange.selector);
        vault.commitPending(FIRST_EPOCH + 1, NO1, 0, commitment);
    }

    // V-7
    function test_V7_commitRefusesAnEpochNotAfterTheFinalizedRoot() public {
        _publishF();
        _capture(NO1, P_TOTAL - F_TOTAL);
        ISTSettlementVault2.Commitment memory commitment = _commitment(_honestP(), 6);
        vm.expectRevert(ISTSettlementVault2.EpochOutOfRange.selector);
        vault.commitPending(FIRST_EPOCH, NO1, 1, commitment);
    }

    // V-7
    function test_V7_commitRefusesATotalOtherThanTheEpochsSnapshot() public {
        _capture(NO1, 100);
        _capture(NO1, 50);
        ISTSettlementVault2.Commitment memory commitment = _raw(keccak256("root"), 100, 1);
        vm.expectRevert(ISTSettlementVault2.TotalMismatch.selector);
        vault.commitPending(FIRST_EPOCH + 1, NO1, 0, commitment);
    }

    // V-7
    function test_V7_commitRefusesATotalNotAboveTheFinalizedTotal() public {
        _publishF();
        uint256 epoch = _defer(NO1);
        ISTSettlementVault2.Commitment memory commitment = _raw(keccak256("root"), F_TOTAL, 5);
        vm.expectRevert(ISTSettlementVault2.TotalNotIncreased.selector);
        vault.commitPending(epoch, NO1, 1, commitment);
    }

    // V-7
    function test_V7_commitRefusesAnUnfundedFirstRoot() public {
        _capture(NO1, 0);
        ISTSettlementVault2.Commitment memory commitment = _raw(keccak256("root"), 0, 1);
        vm.expectRevert(ISTSettlementVault2.TotalNotIncreased.selector);
        vault.commitPending(FIRST_EPOCH, NO1, 0, commitment);
    }

    // V-7
    function test_V7_commitRefusesZeroLeaves() public {
        _capture(NO1, 100);
        ISTSettlementVault2.Commitment memory commitment = _raw(keccak256("root"), 100, 0);
        vm.expectRevert(ISTSettlementVault2.InvalidLeafCount.selector);
        vault.commitPending(FIRST_EPOCH, NO1, 0, commitment);
    }

    // V-7
    function test_V7_commitRefusesMoreThan2To29Leaves() public {
        _capture(NO1, 100);
        ISTSettlementVault2.Commitment memory commitment = _raw(keccak256("root"), 100, (1 << 29) + 1);
        vm.expectRevert(ISTSettlementVault2.InvalidLeafCount.selector);
        vault.commitPending(FIRST_EPOCH, NO1, 0, commitment);
    }

    // V-7
    function test_V7_commitAccepts2To29Leaves() public {
        _capture(NO1, 100);
        vault.commitPending(FIRST_EPOCH, NO1, 0, _raw(keccak256("root"), 100, 1 << 29));
        assertEq(vault.pending(NO1).leafCount, 1 << 29);
    }

    // V-7
    function test_V7_commitRefusesFewerLeavesThanTheFinalizedRoot() public {
        _publishF();
        _capture(NO1, P_TOTAL - F_TOTAL);
        ISTSettlementVault2.Commitment memory commitment = _raw(keccak256("root"), P_TOTAL, 4);
        vm.expectRevert(ISTSettlementVault2.InvalidLeafCount.selector);
        vault.commitPending(FIRST_EPOCH + 1, NO1, 1, commitment);
    }

    // V-7
    function test_V7_commitRefusesAZeroRoot() public {
        _capture(NO1, 100);
        ISTSettlementVault2.Commitment memory commitment = _raw(bytes32(0), 100, 1);
        vm.expectRevert(ISTSettlementVault2.InvalidRoot.selector);
        vault.commitPending(FIRST_EPOCH, NO1, 0, commitment);
    }

    // V-7: an epoch that does not fit the root's uint64 field is refused, not
    // truncated. Only a pool registered at the last uint64 epoch reaches one.
    function test_V7_commitRefusesAnEpochBeyondUint64() public {
        neuron.setUid(NETUID, POOL2, POOL2_UID);
        vault.registerPool(NO2, POOL2, BURN_LIMIT, type(uint64).max);
        _capture(NO2, 100);
        uint256 epoch = _capture(NO2, 50);
        assertEq(epoch, uint256(type(uint64).max) + 1);
        ISTSettlementVault2.Commitment memory commitment = _raw(keccak256("root"), 150, 1);
        vm.expectRevert(abi.encodeWithSelector(SafeCast.SafeCastOverflowedUintDowncast.selector, 64, epoch));
        vault.commitPending(epoch, NO2, 0, commitment);
    }

    // V-7: likewise a block number beyond uint64.
    function test_V7_commitRefusesABlockNumberBeyondUint64() public {
        _capture(NO1, 100);
        ISTSettlementVault2.Commitment memory commitment = _raw(keccak256("root"), 100, 1);
        uint256 blockNumber = uint256(type(uint64).max) + 1;
        vm.roll(blockNumber);
        vm.expectRevert(
            abi.encodeWithSelector(SafeCast.SafeCastOverflowedUintDowncast.selector, 64, blockNumber)
        );
        vault.commitPending(FIRST_EPOCH, NO1, 0, commitment);
    }

    // V-7 (A5)
    function test_V7_commitRefusesAZeroLedgerHash() public {
        _capture(NO1, 100);
        ISTSettlementVault2.Commitment memory commitment = ISTSettlementVault2.Commitment({
            root: keccak256("root"), total: 100, leafCount: 1, ledgerHash: bytes32(0)
        });
        vm.expectRevert(ISTSettlementVault2.InvalidRoot.selector);
        vault.commitPending(FIRST_EPOCH, NO1, 0, commitment);
    }

    // V-7, V-EV: a replacement takes the next seq, names the seq it replaced,
    // and starts with no demand; the replaced root's demands no longer block.
    function test_V7_aReplacementBumpsSeqAndDiscardsDemands() public {
        _publishF();
        uint64 replaced = _propose(NO1, _honestP(), 6);
        _demand(CAROL_INDEX);
        assertEq(vault.pending(NO1).openDemands, 1);

        vm.roll(block.number + WINDOW);
        uint256 epoch = _defer(NO1);
        V2TreeHelper.Node[][] memory p = _honestP();
        ISTSettlementVault2.Commitment memory commitment = _commitment(p, 6);
        vm.expectEmit(address(vault));
        emit ISTSettlementVault2.DistributionCommitted(
            NO1,
            replaced + 1,
            SafeCast.toUint64(epoch),
            commitment.root,
            P_TOTAL,
            6,
            commitment.ledgerHash,
            replaced
        );
        uint64 seq = vault.commitPending(epoch, NO1, 1, commitment);

        assertEq(seq, replaced + 1);
        assertEq(vault.pending(NO1).openDemands, 0);
        assertEq(_demandState(seq, F_ROOT, CAROL_INDEX), uint8(ISTSettlementVault2.DemandState.None));
        _finalize(NO1);
        assertEq(vault.rootCount(NO1), 2);
    }

    // V-7: seq never repeats, across finalization, deletion and replacement.
    function test_V7_seqAfterAFinalizationOrDeletionExceedsEveryEarlierSeq() public {
        V2TreeHelper.Node[][] memory f = _publishF();
        uint64 first = vault.commitSeq(NO1);
        assertEq(first, 1);

        uint64 deleted = _propose(NO1, _loweredP(), 6);
        _checkCarol(f, _loweredP());
        assertGt(deleted, first);

        vm.roll(block.number + WINDOW);
        uint256 epoch = _defer(NO1);
        ISTSettlementVault2.Commitment memory commitment = _commitment(_honestP(), 6);
        vm.expectEmit(address(vault));
        emit ISTSettlementVault2.DistributionCommitted(
            NO1, deleted + 1, SafeCast.toUint64(epoch), commitment.root, P_TOTAL, 6, commitment.ledgerHash, 0
        );
        uint64 next = vault.commitPending(epoch, NO1, 1, commitment);
        assertGt(next, deleted);
        assertEq(vault.commitSeq(NO1), next);
    }

    // ------------------------------------------------------------------
    // V-8: checkPath deletes the pending root for each fault
    // ------------------------------------------------------------------

    // V-8
    function test_V8_aLoweredAccountDeletesThePendingRoot() public {
        _assertCarolDeletes(_publishF(), _loweredP(), LedgerTree.Fault.LowerValue, 0);
    }

    // V-8
    function test_V8_aMovedColdkeyDeletesThePendingRoot() public {
        _assertCarolDeletes(_publishF(), _movedP(), LedgerTree.Fault.WrongLeaf, 0);
    }

    // V-8
    function test_V8_aGarbageLeafDeletesThePendingRoot() public {
        _assertCarolDeletes(_publishF(), _garbageLeafP(), LedgerTree.Fault.WrongLeaf, 0);
    }

    // V-8
    function test_V8_anInconsistentNodeAtLevel1DeletesThePendingRoot() public {
        _assertCarolDeletes(_publishF(), _inconsistentP(1), LedgerTree.Fault.InconsistentNode, 1);
    }

    // V-8
    function test_V8_anInconsistentNodeAtLevel2DeletesThePendingRoot() public {
        _assertCarolDeletes(_publishF(), _inconsistentP(2), LedgerTree.Fault.InconsistentNode, 2);
    }

    // V-8: the root's children do not add up to the committed total.
    function test_V8_anInconsistentRootDeletesThePendingRoot() public {
        _assertCarolDeletes(_publishF(), _inconsistentP(3), LedgerTree.Fault.InconsistentNode, 3);
    }

    // V-8: a fault deletes the root whatever demands are open on it.
    function test_V8_aFaultDeletesThePendingRootDespiteOpenDemands() public {
        V2TreeHelper.Node[][] memory f = _publishF();
        _propose(NO1, _loweredP(), 6);
        _demand(0);
        _demand(CAROL_INDEX);
        assertEq(uint8(_checkCarol(f, _loweredP())), uint8(LedgerTree.Fault.LowerValue));
        assertEq(vault.pending(NO1).seq, 0);
        assertEq(vault.pending(NO1).openDemands, 0);
        vm.roll(block.number + WINDOW);
        vm.expectRevert(ISTSettlementVault2.NoPendingRoot.selector);
        vault.finalizePending(NO1);
    }

    // V-8, V-10: demands still open on a deleted root block nothing after it.
    // The next pending root starts with none and finalizes.
    function test_V8_demandsOnADeletedRootNeverBlockTheNextRoot() public {
        V2TreeHelper.Node[][] memory f = _publishF();
        uint64 deleted = _propose(NO1, _loweredP(), 6);
        _demand(0);
        _demand(CAROL_INDEX);
        _checkCarol(f, _loweredP());

        vm.roll(block.number + WINDOW);
        uint64 seq = _commit(NO1, _honestP(), 6);
        assertEq(seq, deleted + 1);
        assertEq(vault.pending(NO1).openDemands, 0);
        assertEq(_demandState(seq, F_ROOT, 0), uint8(ISTSettlementVault2.DemandState.None));
        assertEq(_demandState(seq, F_ROOT, CAROL_INDEX), uint8(ISTSettlementVault2.DemandState.None));
        assertEq(_finalize(NO1), 1);
        assertEq(vault.rootCount(NO1), 2);
    }

    // V-8: at depth 0 the leaf is (root, total). A lower value cannot occur
    // there: both roots hold one account and the new total must exceed the old.
    function test_V8_aMovedColdkeyAtDepthZeroDeletesThePendingRoot() public {
        V2TreeHelper.Node[][] memory f = _publish(NO1, _keys(ALICE), _values(100));
        V2TreeHelper.Node[][] memory p = V2TreeHelper.build(_keys(MALLORY), _values(150));
        uint64 seq = _propose(NO1, p, 1);
        LedgerTree.Account memory alice = _account(0, ALICE, 100);
        LedgerTree.ProofNode[] memory oldProof = V2TreeHelper.proof(f, 0);
        LedgerTree.PathNode[] memory newPath = V2TreeHelper.path(p, 0);
        assertEq(oldProof.length, 0);
        assertEq(newPath.length, 0);

        vm.expectEmit(address(vault));
        emit ISTSettlementVault2.DistributionDeleted(
            NO1, seq, F_ROOT, 0, LedgerTree.Fault.WrongLeaf, 0, address(this)
        );
        assertEq(
            uint8(vault.checkPath(NO1, F_ROOT, alice, oldProof, newPath)), uint8(LedgerTree.Fault.WrongLeaf)
        );
        assertEq(vault.pending(NO1).seq, 0);
    }

    // V-8
    function test_V8_aGarbageRootAtDepthZeroDeletesThePendingRoot() public {
        V2TreeHelper.Node[][] memory f = _publish(NO1, _keys(ALICE), _values(100));
        V2TreeHelper.Node[][] memory p = V2TreeHelper.build(_keys(ALICE), _values(150));
        p[0][0].hash = keccak256("garbage-root");
        uint64 seq = _propose(NO1, p, 1);
        LedgerTree.Account memory alice = _account(0, ALICE, 100);
        LedgerTree.ProofNode[] memory oldProof = V2TreeHelper.proof(f, 0);
        LedgerTree.PathNode[] memory newPath = V2TreeHelper.path(p, 0);

        vm.expectEmit(address(vault));
        emit ISTSettlementVault2.DistributionDeleted(
            NO1, seq, F_ROOT, 0, LedgerTree.Fault.WrongLeaf, 0, address(this)
        );
        vault.checkPath(NO1, F_ROOT, alice, oldProof, newPath);
        assertEq(vault.pending(NO1).seq, 0);
    }

    // ------------------------------------------------------------------
    // V-9: checkPath refusals
    // ------------------------------------------------------------------

    // V-9: no index of an honest successor shows a fault, at any shape. The
    // successor keeps every account in place, never lowers one and appends
    // new ones.
    function testFuzz_V9_checkPathFindsNoFaultOnAnHonestIndex(uint256 seed, uint8 oldCount, uint8 added)
        public
    {
        uint256 n = bound(oldCount, 1, 17);
        uint256 m = n + bound(added, 0, 17);
        bytes32[] memory keys = new bytes32[](m);
        bytes32[] memory oldKeys = new bytes32[](n);
        uint256[] memory oldValues = new uint256[](n);
        uint256[] memory newValues = new uint256[](m);
        for (uint256 i; i < m; i++) {
            uint256 draw = uint256(keccak256(abi.encode(seed, i)));
            keys[i] = keccak256(abi.encode("fuzz-coldkey", seed, i));
            newValues[i] = 1 + draw % 1e12;
            if (i < n) {
                oldKeys[i] = keys[i];
                oldValues[i] = 1 + (draw >> 64) % 1e12;
                newValues[i] = oldValues[i] + (draw >> 128) % 1e9;
            }
        }
        newValues[0] += 1;
        V2TreeHelper.Node[][] memory f = _publish(NO1, oldKeys, oldValues);
        V2TreeHelper.Node[][] memory p = V2TreeHelper.build(keys, newValues);
        _propose(NO1, p, m);

        for (uint256 index; index < n; index++) {
            LedgerTree.Account memory account = _account(index, keys[index], oldValues[index]);
            LedgerTree.ProofNode[] memory oldProof = V2TreeHelper.proof(f, index);
            LedgerTree.PathNode[] memory newPath = V2TreeHelper.path(p, index);
            vm.expectRevert(ISTSettlementVault2.NoFault.selector);
            vault.checkPath(NO1, F_ROOT, account, oldProof, newPath);
        }
        assertGt(vault.pending(NO1).seq, 0);
    }

    // V-9
    function test_V9_checkPathRefusesAPreimageThatDoesNotHashToItsNode() public {
        V2TreeHelper.Node[][] memory f = _publishF();
        _propose(NO1, _loweredP(), 6);
        LedgerTree.Account memory carol = _account(CAROL_INDEX, CAROL, 30);
        LedgerTree.ProofNode[] memory oldProof = V2TreeHelper.proof(f, CAROL_INDEX);
        LedgerTree.PathNode[] memory newPath = V2TreeHelper.path(_loweredP(), CAROL_INDEX);
        newPath[1].leftSum += 1;
        vm.expectRevert(LedgerTree.MalformedPath.selector);
        vault.checkPath(NO1, F_ROOT, carol, oldProof, newPath);
    }

    // V-9
    function test_V9_checkPathRefusesAPathOfTheWrongLength() public {
        V2TreeHelper.Node[][] memory f = _publishF();
        _propose(NO1, _loweredP(), 6);
        LedgerTree.Account memory carol = _account(CAROL_INDEX, CAROL, 30);
        LedgerTree.ProofNode[] memory oldProof = V2TreeHelper.proof(f, CAROL_INDEX);
        LedgerTree.PathNode[] memory full = V2TreeHelper.path(_loweredP(), CAROL_INDEX);
        LedgerTree.PathNode[] memory short = new LedgerTree.PathNode[](2);
        (short[0], short[1]) = (full[0], full[1]);
        vm.expectRevert(LedgerTree.MalformedPath.selector);
        vault.checkPath(NO1, F_ROOT, carol, oldProof, short);
    }

    // V-9
    function test_V9_checkPathRefusesAnInvalidOldProof() public {
        V2TreeHelper.Node[][] memory f = _publishF();
        _propose(NO1, _loweredP(), 6);
        LedgerTree.Account memory inflated = _account(CAROL_INDEX, CAROL, 31);
        LedgerTree.ProofNode[] memory oldProof = V2TreeHelper.proof(f, CAROL_INDEX);
        LedgerTree.PathNode[] memory newPath = V2TreeHelper.path(_loweredP(), CAROL_INDEX);
        vm.expectRevert(ISTSettlementVault2.InvalidProof.selector);
        vault.checkPath(NO1, F_ROOT, inflated, oldProof, newPath);
    }

    // V-9: an account the finalized root does not hold has nothing to protect.
    function test_V9_checkPathRefusesAnIndexOutsideTheFinalizedRoot() public {
        _publishF();
        V2TreeHelper.Node[][] memory p = _honestP();
        _propose(NO1, p, 6);
        LedgerTree.Account memory frank = _account(5, FRANK, 30);
        LedgerTree.ProofNode[] memory oldProof = V2TreeHelper.proof(p, 5);
        LedgerTree.PathNode[] memory newPath = V2TreeHelper.path(p, 5);
        vm.expectRevert(ISTSettlementVault2.IndexOutOfRange.selector);
        vault.checkPath(NO1, F_ROOT, frank, oldProof, newPath);
    }

    // V-9
    function test_V9_checkPathRefusesWithNoPendingRoot() public {
        V2TreeHelper.Node[][] memory f = _publishF();
        LedgerTree.Account memory carol = _account(CAROL_INDEX, CAROL, 30);
        LedgerTree.ProofNode[] memory oldProof = V2TreeHelper.proof(f, CAROL_INDEX);
        LedgerTree.PathNode[] memory newPath = V2TreeHelper.path(f, CAROL_INDEX);
        vm.expectRevert(ISTSettlementVault2.NoPendingRoot.selector);
        vault.checkPath(NO1, F_ROOT, carol, oldProof, newPath);
    }

    // V-9: a pool's first root has no finalized predecessor to protect, so
    // every root index is unknown.
    function test_V9_checkPathRefusesWithNoFinalizedRoot() public {
        V2TreeHelper.Node[][] memory f = V2TreeHelper.build(_oldKeys(), _oldValues());
        _propose(NO1, f, 5);
        LedgerTree.Account memory carol = _account(CAROL_INDEX, CAROL, 30);
        LedgerTree.ProofNode[] memory oldProof = V2TreeHelper.proof(f, CAROL_INDEX);
        LedgerTree.PathNode[] memory newPath = V2TreeHelper.path(f, CAROL_INDEX);
        vm.expectRevert(ISTSettlementVault2.UnknownRoot.selector);
        vault.checkPath(NO1, F_ROOT, carol, oldProof, newPath);
    }

    // V-9: a root index at or above the pool's root count is refused, even
    // with an account and proof that hold in the latest root.
    function test_V9_checkPathRefusesAnUnknownRootIndex() public {
        V2TreeHelper.Node[][] memory f = _publishF();
        V2TreeHelper.Node[][] memory p = _loweredP();
        _propose(NO1, p, 6);
        LedgerTree.Account memory carol = _account(CAROL_INDEX, CAROL, 30);
        LedgerTree.ProofNode[] memory oldProof = V2TreeHelper.proof(f, CAROL_INDEX);
        LedgerTree.PathNode[] memory newPath = V2TreeHelper.path(p, CAROL_INDEX);
        vm.expectRevert(ISTSettlementVault2.UnknownRoot.selector);
        vault.checkPath(NO1, 1, carol, oldProof, newPath);
        vm.expectRevert(ISTSettlementVault2.UnknownRoot.selector);
        vault.checkPath(NO1, type(uint256).max, carol, oldProof, newPath);
        assertEq(vault.pending(NO1).seq, 2);
    }

    // ------------------------------------------------------------------
    // V-10: demandPath
    // ------------------------------------------------------------------

    // V-10, V-EV
    function test_V10_aDemandIsRecordedAndItsFeeKept() public {
        _publishF();
        uint64 seq = _propose(NO1, _honestP(), 6);
        LedgerTree.Account memory carol = _fAccount(CAROL_INDEX);
        LedgerTree.ProofNode[] memory proof = _fProof(CAROL_INDEX);
        vm.deal(stranger, DEMAND_FEE);

        vm.expectEmit(address(vault));
        emit ISTSettlementVault2.PathDemanded(NO1, seq, F_ROOT, CAROL_INDEX, stranger);
        vm.prank(stranger);
        vault.demandPath{value: DEMAND_FEE}(NO1, F_ROOT, carol, proof);

        assertEq(_demandState(seq, F_ROOT, CAROL_INDEX), uint8(ISTSettlementVault2.DemandState.Open));
        assertEq(_demandState(seq, F_ROOT, 0), uint8(ISTSettlementVault2.DemandState.None));
        assertEq(vault.pending(NO1).openDemands, 1);
        assertEq(address(vault).balance, DEMAND_FEE);
        assertEq(stranger.balance, 0);
    }

    // V-10, V-EV: a clean opening answers the demand, and anyone may give it.
    function test_V10_aCleanOpeningAnswersTheDemand() public {
        V2TreeHelper.Node[][] memory f = _publishF();
        V2TreeHelper.Node[][] memory p = _honestP();
        uint64 seq = _propose(NO1, p, 6);
        _demand(CAROL_INDEX);
        _demand(0);
        LedgerTree.Account memory carol = _account(CAROL_INDEX, CAROL, 30);
        LedgerTree.ProofNode[] memory oldProof = V2TreeHelper.proof(f, CAROL_INDEX);
        LedgerTree.PathNode[] memory newPath = V2TreeHelper.path(p, CAROL_INDEX);
        address answerer = makeAddr("answerer");

        vm.expectEmit(address(vault));
        emit ISTSettlementVault2.PathAnswered(NO1, seq, F_ROOT, CAROL_INDEX, answerer);
        vm.prank(answerer);
        assertEq(uint8(vault.checkPath(NO1, F_ROOT, carol, oldProof, newPath)), uint8(LedgerTree.Fault.None));

        assertEq(_demandState(seq, F_ROOT, CAROL_INDEX), uint8(ISTSettlementVault2.DemandState.Answered));
        assertEq(_demandState(seq, F_ROOT, 0), uint8(ISTSettlementVault2.DemandState.Open));
        assertEq(vault.pending(NO1).openDemands, 1);
        assertEq(vault.pending(NO1).seq, seq);
    }

    // V-10 (A1): an answer counts once. Opening the answered index again
    // reverts NoFault and leaves the other open demand in place.
    function test_V10_aRepeatedCleanAnswerRevertsNoFault() public {
        V2TreeHelper.Node[][] memory f = _publishF();
        V2TreeHelper.Node[][] memory p = _honestP();
        uint64 seq = _propose(NO1, p, 6);
        _demand(CAROL_INDEX);
        _demand(0);
        _checkCarol(f, p);
        LedgerTree.Account memory carol = _account(CAROL_INDEX, CAROL, 30);
        LedgerTree.ProofNode[] memory oldProof = V2TreeHelper.proof(f, CAROL_INDEX);
        LedgerTree.PathNode[] memory newPath = V2TreeHelper.path(p, CAROL_INDEX);

        vm.expectRevert(ISTSettlementVault2.NoFault.selector);
        vault.checkPath(NO1, F_ROOT, carol, oldProof, newPath);

        assertEq(vault.pending(NO1).openDemands, 1);
        assertEq(_demandState(seq, F_ROOT, CAROL_INDEX), uint8(ISTSettlementVault2.DemandState.Answered));
        assertEq(_demandState(seq, F_ROOT, 0), uint8(ISTSettlementVault2.DemandState.Open));
        vm.roll(block.number + WINDOW);
        vm.expectRevert(ISTSettlementVault2.DemandOpen.selector);
        vault.finalizePending(NO1);
    }

    // V-10
    function test_V10_demandRefusesTheSameIndexTwicePerSeq() public {
        _publishF();
        _propose(NO1, _honestP(), 6);
        _demand(CAROL_INDEX);
        _expectDemandRefused(
            F_ROOT,
            _fAccount(CAROL_INDEX),
            _fProof(CAROL_INDEX),
            DEMAND_FEE,
            ISTSettlementVault2.AlreadyDemanded.selector
        );
    }

    // V-10: once answered, an index cannot be demanded again under the same seq.
    function test_V10_demandRefusesAnAnsweredIndexUnderTheSameSeq() public {
        V2TreeHelper.Node[][] memory f = _publishF();
        V2TreeHelper.Node[][] memory p = _honestP();
        _propose(NO1, p, 6);
        _demand(CAROL_INDEX);
        _checkCarol(f, p);
        _expectDemandRefused(
            F_ROOT,
            _fAccount(CAROL_INDEX),
            _fProof(CAROL_INDEX),
            DEMAND_FEE,
            ISTSettlementVault2.AlreadyDemanded.selector
        );
    }

    // V-10 (A1): an opening with no fault and no open demand changes nothing.
    function test_V10_checkPathRefusesACleanOpeningWithNoOpenDemand() public {
        V2TreeHelper.Node[][] memory f = _publishF();
        V2TreeHelper.Node[][] memory p = _honestP();
        uint64 seq = _propose(NO1, p, 6);
        LedgerTree.Account memory carol = _account(CAROL_INDEX, CAROL, 30);
        LedgerTree.ProofNode[] memory oldProof = V2TreeHelper.proof(f, CAROL_INDEX);
        LedgerTree.PathNode[] memory newPath = V2TreeHelper.path(p, CAROL_INDEX);
        vm.expectRevert(ISTSettlementVault2.NoFault.selector);
        vault.checkPath(NO1, F_ROOT, carol, oldProof, newPath);
        assertEq(_demandState(seq, F_ROOT, CAROL_INDEX), uint8(ISTSettlementVault2.DemandState.None));
    }

    // V-10
    function test_V10_demandRefusesAfterTheWindow() public {
        _publishF();
        _propose(NO1, _honestP(), 6);
        vm.roll(block.number + WINDOW);
        _expectDemandRefused(
            F_ROOT,
            _fAccount(CAROL_INDEX),
            _fProof(CAROL_INDEX),
            DEMAND_FEE,
            ISTSettlementVault2.ChallengeWindowClosed.selector
        );
    }

    // V-10, V-11: a demand in the window's last block blocks finalization
    // until it is answered after the window.
    function test_V10_aDemandInTheWindowsLastBlockIsAnsweredAfterIt() public {
        V2TreeHelper.Node[][] memory f = _publishF();
        V2TreeHelper.Node[][] memory p = _honestP();
        _propose(NO1, p, 6);
        uint256 committed = block.number;

        vm.roll(committed + WINDOW - 1);
        _demand(CAROL_INDEX);
        vm.roll(committed + WINDOW);
        vm.expectRevert(ISTSettlementVault2.DemandOpen.selector);
        vault.finalizePending(NO1);

        _checkCarol(f, p);
        assertEq(vault.finalizePending(NO1), 1);
    }

    // V-10: a withheld or garbage node can never be opened, so a demand under
    // it can never be answered or turned into a fault, and it blocks the root.
    function test_V10_anUnanswerableDemandBlocksFinalization() public {
        V2TreeHelper.Node[][] memory f = _publishF();
        V2TreeHelper.Node[][] memory p = _withheldNodeP();
        _propose(NO1, p, 6);
        _demand(CAROL_INDEX);
        _demand(0);
        _check(f, p, 0, ALICE, 10);

        LedgerTree.Account memory carol = _account(CAROL_INDEX, CAROL, 30);
        LedgerTree.ProofNode[] memory oldProof = V2TreeHelper.proof(f, CAROL_INDEX);
        LedgerTree.PathNode[] memory newPath = V2TreeHelper.path(p, CAROL_INDEX);
        vm.expectRevert(LedgerTree.MalformedPath.selector);
        vault.checkPath(NO1, F_ROOT, carol, oldProof, newPath);

        vm.roll(block.number + WINDOW);
        vm.expectRevert(ISTSettlementVault2.DemandOpen.selector);
        vault.finalizePending(NO1);
    }

    // V-10: an unpublished honest tree blocks until its operator opens the path.
    function test_V10_aWithheldTreeBlocksFinalizationUntilAnswered() public {
        V2TreeHelper.Node[][] memory f = _publishF();
        V2TreeHelper.Node[][] memory p = _honestP();
        _propose(NO1, p, 6);
        _demand(CAROL_INDEX);
        vm.roll(block.number + WINDOW * 3);
        vm.expectRevert(ISTSettlementVault2.DemandOpen.selector);
        vault.finalizePending(NO1);
        _checkCarol(f, p);
        vault.finalizePending(NO1);
    }

    // V-10
    function test_V10_demandRefusesAnIndexOutsideTheFinalizedRoot() public {
        _publishF();
        V2TreeHelper.Node[][] memory p = _honestP();
        _propose(NO1, p, 6);
        _expectDemandRefused(
            F_ROOT,
            _account(5, FRANK, 30),
            V2TreeHelper.proof(p, 5),
            DEMAND_FEE,
            ISTSettlementVault2.IndexOutOfRange.selector
        );
    }

    // V-10 (A2)
    function test_V10_demandRefusesAnAccountNotProvenAgainstTheFinalizedRoot() public {
        _publishF();
        _propose(NO1, _honestP(), 6);
        _expectDemandRefused(
            F_ROOT,
            _account(CAROL_INDEX, CAROL, 31),
            _fProof(CAROL_INDEX),
            DEMAND_FEE,
            ISTSettlementVault2.InvalidProof.selector
        );
    }

    // V-10 (A2): a position of the finalized root that no proof reaches (here
    // a garbage leaf) cannot be demanded, so it cannot block every later root.
    function test_V10_anUnprovablePositionCannotBlockLaterRoots() public {
        _capture(NO1, F_TOTAL);
        V2TreeHelper.Node[][] memory f = V2TreeHelper.build(_oldKeys(), _oldValues());
        f[0][3].hash = keccak256("garbage-finalized-leaf");
        V2TreeHelper.rehash(f, 0);
        _commit(NO1, f, 5);
        _finalize(NO1);
        _propose(NO1, _honestP(), 6);

        _expectDemandRefused(
            F_ROOT,
            _account(3, DAVE, 40),
            V2TreeHelper.proof(f, 3),
            DEMAND_FEE,
            ISTSettlementVault2.InvalidProof.selector
        );
        vm.roll(block.number + WINDOW);
        assertEq(vault.finalizePending(NO1), 1);
    }

    // V-10
    function test_V10_demandRefusesWithNoPendingRoot() public {
        _publishF();
        _expectDemandRefused(
            F_ROOT, _fAccount(0), _fProof(0), DEMAND_FEE, ISTSettlementVault2.NoPendingRoot.selector
        );
    }

    // V-10: with no finalized root, every root index is unknown.
    function test_V10_demandRefusesWithNoFinalizedRoot() public {
        _propose(NO1, V2TreeHelper.build(_oldKeys(), _oldValues()), 5);
        _expectDemandRefused(
            F_ROOT, _fAccount(0), _fProof(0), DEMAND_FEE, ISTSettlementVault2.UnknownRoot.selector
        );
    }

    // V-10: a root index at or above the pool's root count is refused, even
    // with an account and proof that hold in the latest root.
    function test_V10_demandRefusesAnUnknownRootIndex() public {
        _publishF();
        _propose(NO1, _honestP(), 6);
        _expectDemandRefused(
            1, _fAccount(0), _fProof(0), DEMAND_FEE, ISTSettlementVault2.UnknownRoot.selector
        );
        _expectDemandRefused(
            type(uint256).max, _fAccount(0), _fProof(0), DEMAND_FEE, ISTSettlementVault2.UnknownRoot.selector
        );
        assertEq(vault.pending(NO1).openDemands, 0);
    }

    // V-10
    function test_V10_demandRefusesALowerFee() public {
        _publishF();
        _propose(NO1, _honestP(), 6);
        _expectDemandRefused(
            F_ROOT, _fAccount(0), _fProof(0), DEMAND_FEE - 1, ISTSettlementVault2.InvalidDemandFee.selector
        );
    }

    // V-10
    function test_V10_demandRefusesAHigherFee() public {
        _publishF();
        _propose(NO1, _honestP(), 6);
        _expectDemandRefused(
            F_ROOT, _fAccount(0), _fProof(0), DEMAND_FEE + 1, ISTSettlementVault2.InvalidDemandFee.selector
        );
    }

    // V-10
    function test_V10_demandRefusesNoFee() public {
        _publishF();
        _propose(NO1, _honestP(), 6);
        _expectDemandRefused(
            F_ROOT, _fAccount(0), _fProof(0), 0, ISTSettlementVault2.InvalidDemandFee.selector
        );
    }

    // ------------------------------------------------------------------
    // V-8, V-10: disputes against any finalized root (§4.2 G2)
    // ------------------------------------------------------------------

    function _six() internal pure returns (bytes32[] memory) {
        return _keys(ALICE, BOB, CAROL, DAVE, ERIN, FRANK);
    }

    // A successor of six accounts of 110 that keeps Alice and Bob, lowers the
    // other four to 1 rao and gives Mallory the rest of 700.
    function _fourLowered() internal pure returns (V2TreeHelper.Node[][] memory) {
        bytes32[] memory keys = new bytes32[](7);
        uint256[] memory values = new uint256[](7);
        for (uint256 i; i < 6; i++) {
            (keys[i], values[i]) = (_six()[i], i < 2 ? 110 : 1);
        }
        (keys[6], values[6]) = (MALLORY, 476);
        return V2TreeHelper.build(keys, values);
    }

    // A stranger pays the fee and demands `account` of the finalized root
    // `rootIndex`, the tree `levels`.
    function _demandAt(
        uint256 rootIndex,
        V2TreeHelper.Node[][] memory levels,
        LedgerTree.Account memory account
    ) internal {
        LedgerTree.ProofNode[] memory proof = V2TreeHelper.proof(levels, account.index);
        vm.deal(stranger, DEMAND_FEE);
        vm.prank(stranger);
        vault.demandPath{value: DEMAND_FEE}(NO1, rootIndex, account, proof);
    }

    // Opens `account` of the finalized root `rootIndex`, the tree `levels`, in
    // the pending tree `p`.
    function _checkAt(
        uint256 rootIndex,
        V2TreeHelper.Node[][] memory levels,
        V2TreeHelper.Node[][] memory p,
        LedgerTree.Account memory account
    ) internal returns (LedgerTree.Fault) {
        LedgerTree.ProofNode[] memory proof = V2TreeHelper.proof(levels, account.index);
        LedgerTree.PathNode[] memory path = V2TreeHelper.path(p, account.index);
        return vault.checkPath(NO1, rootIndex, account, proof, path);
    }

    // V-10, G2 (review H1): withholding the newest root's tree cannot shield
    // the accounts of an older one. Root 0, published, holds six accounts of
    // 100. Root 1 adds 10 to each, but its tree is withheld: watchers demand
    // a root-0 account, the operator answers it, and root 1 finalizes. Its
    // successor keeps Alice and Bob, whose root-1 values that answer revealed,
    // and lowers the other four to 1 rao for Mallory. Carol's root-0 account
    // still demands her opening, which blocks the successor until an opening
    // shows the fault and deletes it. A vault that took demands against the
    // latest root only refused this demand InvalidProof.
    function test_V10_aWithheldRootCannotShieldTheAccountsOfAnOlderOne() public {
        V2TreeHelper.Node[][] memory f0 = _publish(NO1, _six(), _values(100, 100, 100, 100, 100, 100));
        V2TreeHelper.Node[][] memory f1 = V2TreeHelper.build(_six(), _values(110, 110, 110, 110, 110, 110));
        _propose(NO1, f1, 6);
        _demandAt(0, f0, _account(0, ALICE, 100));
        _checkAt(0, f0, f1, _account(0, ALICE, 100));
        assertEq(_finalize(NO1), 1);

        V2TreeHelper.Node[][] memory p = _fourLowered();
        uint64 seq = _propose(NO1, p, 7);
        LedgerTree.Account memory carol = _account(CAROL_INDEX, CAROL, 100);
        vm.expectEmit(address(vault));
        emit ISTSettlementVault2.PathDemanded(NO1, seq, 0, CAROL_INDEX, stranger);
        _demandAt(0, f0, carol);
        vm.roll(block.number + WINDOW);
        vm.expectRevert(ISTSettlementVault2.DemandOpen.selector);
        vault.finalizePending(NO1);

        vm.expectEmit(address(vault));
        emit ISTSettlementVault2.DistributionDeleted(
            NO1, seq, 0, CAROL_INDEX, LedgerTree.Fault.LowerValue, 0, address(this)
        );
        assertEq(uint8(_checkAt(0, f0, p, carol)), uint8(LedgerTree.Fault.LowerValue));
        assertEq(vault.pending(NO1).seq, 0);
        assertEq(vault.rootCount(NO1), 2);
        assertEq(_claim(NO1, 0, f0, CAROL_INDEX, CAROL, 100), 100);
    }

    // V-10, G2 (review H1, M1): an honest successor answers demands on the
    // accounts of every finalized root. Demands on one account against two
    // roots are two demands, each answered against its own root, and an
    // account that root 1 appended is demanded against root 1 but is out of
    // range of root 0.
    function test_V10_anHonestSuccessorAnswersDemandsAgainstEveryFinalizedRoot() public {
        V2TreeHelper.Node[][] memory f0 = _publish(NO1, _keys(ALICE, BOB), _values(100, 100));
        V2TreeHelper.Node[][] memory f1 = _publish(NO1, _keys(ALICE, BOB, CAROL), _values(150, 100, 50));
        V2TreeHelper.Node[][] memory p = V2TreeHelper.build(_keys(ALICE, BOB, CAROL), _values(200, 120, 80));
        uint64 seq = _propose(NO1, p, 3);

        vm.expectEmit(address(vault));
        emit ISTSettlementVault2.PathDemanded(NO1, seq, 0, 0, stranger);
        _demandAt(0, f0, _account(0, ALICE, 100));
        vm.expectEmit(address(vault));
        emit ISTSettlementVault2.PathDemanded(NO1, seq, 1, 0, stranger);
        _demandAt(1, f1, _account(0, ALICE, 150));
        _demandAt(1, f1, _account(2, CAROL, 50));
        _expectDemandRefused(
            0,
            _account(2, CAROL, 50),
            V2TreeHelper.proof(f1, 2),
            DEMAND_FEE,
            ISTSettlementVault2.IndexOutOfRange.selector
        );
        assertEq(vault.pending(NO1).openDemands, 3);

        vm.expectEmit(address(vault));
        emit ISTSettlementVault2.PathAnswered(NO1, seq, 1, 0, address(this));
        _checkAt(1, f1, p, _account(0, ALICE, 150));
        assertEq(_demandState(seq, 1, 0), uint8(ISTSettlementVault2.DemandState.Answered));
        assertEq(_demandState(seq, 0, 0), uint8(ISTSettlementVault2.DemandState.Open));
        assertEq(vault.pending(NO1).openDemands, 2);
        vm.expectEmit(address(vault));
        emit ISTSettlementVault2.PathAnswered(NO1, seq, 0, 0, address(this));
        _checkAt(0, f0, p, _account(0, ALICE, 100));
        _checkAt(1, f1, p, _account(2, CAROL, 50));
        assertEq(_demandState(seq, 0, 0), uint8(ISTSettlementVault2.DemandState.Answered));
        assertEq(_demandState(seq, 1, 2), uint8(ISTSettlementVault2.DemandState.Answered));
        assertEq(vault.pending(NO1).openDemands, 0);
        assertEq(_finalize(NO1), 2);
    }

    // V-8, V-10, G2 (review M1): each finalized root protects its own values.
    // A successor that keeps Alice above her root-0 value but lowers her below
    // her root-1 value answers her root-0 demand cleanly. That answer neither
    // answers nor bars a demand on her root-1 account, whose opening shows the
    // fault and deletes the successor.
    function test_V8_aValueLoweredBelowALaterRootIsFaultedAgainstThatRoot() public {
        V2TreeHelper.Node[][] memory f0 = _publish(NO1, _keys(ALICE), _values(10));
        V2TreeHelper.Node[][] memory f1 = _publish(NO1, _keys(ALICE, BOB), _values(50, 20));
        V2TreeHelper.Node[][] memory p = V2TreeHelper.build(_keys(ALICE, BOB, CAROL), _values(20, 20, 80));
        uint64 seq = _propose(NO1, p, 3);

        _demandAt(0, f0, _account(0, ALICE, 10));
        assertEq(uint8(_checkAt(0, f0, p, _account(0, ALICE, 10))), uint8(LedgerTree.Fault.None));
        assertEq(_demandState(seq, 0, 0), uint8(ISTSettlementVault2.DemandState.Answered));
        _demandAt(1, f1, _account(0, ALICE, 50));
        assertEq(_demandState(seq, 1, 0), uint8(ISTSettlementVault2.DemandState.Open));

        vm.expectEmit(address(vault));
        emit ISTSettlementVault2.DistributionDeleted(NO1, seq, 1, 0, LedgerTree.Fault.LowerValue, 0, stranger);
        vm.prank(stranger);
        assertEq(uint8(_checkAt(1, f1, p, _account(0, ALICE, 50))), uint8(LedgerTree.Fault.LowerValue));
        assertEq(vault.pending(NO1).seq, 0);
        assertEq(vault.pending(NO1).openDemands, 0);
    }

    // ------------------------------------------------------------------
    // V-11: finalizePending
    // ------------------------------------------------------------------

    // V-11, V-EV
    function test_V11_finalizeAppendsTheRootAndClearsTheSlot() public {
        _publishF();
        V2TreeHelper.Node[][] memory p = _honestP();
        uint256 epoch = _capture(NO1, P_TOTAL - F_TOTAL);
        ISTSettlementVault2.Commitment memory commitment = _commitment(p, 6);
        uint64 seq = vault.commitPending(epoch, NO1, 1, commitment);
        vm.roll(block.number + WINDOW);

        vm.expectEmit(address(vault));
        emit ISTSettlementVault2.DistributionFinalized(
            NO1, seq, 1, SafeCast.toUint64(epoch), commitment.root, P_TOTAL, 6, commitment.ledgerHash
        );
        assertEq(vault.finalizePending(NO1), 1);

        ISTSettlementVault2.Root memory root = vault.roots(NO1, 1);
        assertEq(root.root, commitment.root);
        assertEq(root.total, P_TOTAL);
        assertEq(root.leafCount, 6);
        assertEq(root.epoch, epoch);
        assertEq(root.ledgerHash, commitment.ledgerHash);
        assertEq(vault.rootCount(NO1), 2);
        assertEq(vault.finalizedTotal(NO1), P_TOTAL);
        assertEq(vault.roots(NO1, 0).total, F_TOTAL, "every finalized root is kept");

        ISTSettlementVault2.Pending memory cleared = vault.pending(NO1);
        assertEq(cleared.seq, 0);
        assertEq(cleared.root, bytes32(0));
        assertEq(cleared.total, 0);
        assertEq(cleared.leafCount, 0);
        assertEq(cleared.epoch, 0);
        assertEq(cleared.ledgerHash, bytes32(0));
        assertEq(cleared.committedBlock, 0);
        assertEq(cleared.openDemands, 0);
    }

    // V-11
    function test_V11_finalizeRefusesWithNoPendingRoot() public {
        _publishF();
        vm.expectRevert(ISTSettlementVault2.NoPendingRoot.selector);
        vault.finalizePending(NO1);
    }

    // V-11
    function test_V11_finalizeRefusesInsideTheWindow() public {
        _publishF();
        _propose(NO1, _honestP(), 6);
        vm.roll(block.number + WINDOW - 1);
        vm.expectRevert(ISTSettlementVault2.ChallengeWindowOpen.selector);
        vault.finalizePending(NO1);
    }

    // V-11
    function test_V11_finalizeRefusesWithAnOpenDemand() public {
        _publishF();
        _propose(NO1, _honestP(), 6);
        _demand(CAROL_INDEX);
        vm.roll(block.number + WINDOW);
        vm.expectRevert(ISTSettlementVault2.DemandOpen.selector);
        vault.finalizePending(NO1);
    }

    // V-7, V-11: each commit replaces every field of the pending root, after
    // a deleted root and while another is pending, and finalization appends
    // exactly the last one.
    function test_V11_finalizeAppendsTheLastCommitAfterADeletionAndAReplacement() public {
        V2TreeHelper.Node[][] memory f = _publishF();
        _propose(NO1, _loweredP(), 6);
        _checkCarol(f, _loweredP());

        // Seven accounts and 210 rao for the next epoch, committed...
        vm.roll(block.number + WINDOW);
        uint256 seventh = _capture(NO1, 10);
        bytes32[] memory keys = new bytes32[](7);
        uint256[] memory values = new uint256[](7);
        (keys[0], keys[1], keys[2], keys[3], keys[4], keys[5], keys[6]) =
        (ALICE, BOB, CAROL, DAVE, ERIN, FRANK, MALLORY);
        (values[0], values[1], values[2], values[3], values[4], values[5], values[6]) =
        (15, 20, 35, 40, 60, 30, 10);
        vault.commitPending(seventh, NO1, 1, _commitment(V2TreeHelper.build(keys, values), 7));

        // ...then replaced by six accounts for the epoch after.
        vm.roll(block.number + WINDOW);
        uint256 eighth = _defer(NO1);
        uint256[] memory sixValues = _values(15, 20, 35, 40, 60, 40);
        ISTSettlementVault2.Commitment memory last = _commitment(V2TreeHelper.build(_newKeys(), sixValues), 6);
        uint64 seq = vault.commitPending(eighth, NO1, 1, last);

        ISTSettlementVault2.Pending memory next = vault.pending(NO1);
        assertEq(next.seq, seq);
        assertEq(next.root, last.root);
        assertEq(next.total, 210);
        assertEq(next.leafCount, 6);
        assertEq(next.epoch, eighth);
        assertEq(next.ledgerHash, last.ledgerHash);
        assertEq(next.committedBlock, block.number);
        assertEq(next.openDemands, 0);

        assertEq(_finalize(NO1), 1);
        ISTSettlementVault2.Root memory root = vault.roots(NO1, 1);
        assertEq(root.root, last.root);
        assertEq(root.total, 210);
        assertEq(root.leafCount, 6);
        assertEq(root.epoch, eighth);
        assertEq(root.ledgerHash, last.ledgerHash);
        assertEq(vault.finalizedTotal(NO1), 210);
    }

    // V-11: roots reads finalized roots only. An index at or past the root
    // count fails as a read past the end of an array, also while a pending
    // root waits to take that index.
    function test_V11_rootsRefusesAnIndexPastTheFinalizedRoots() public {
        vm.expectRevert(stdError.indexOOBError);
        vault.roots(NO1, 0);
        _publishF();
        _propose(NO1, _honestP(), 6);
        vm.expectRevert(stdError.indexOOBError);
        vault.roots(NO1, 1);
        vm.expectRevert(stdError.indexOOBError);
        vault.roots(NO1, type(uint256).max);
        assertEq(vault.roots(NO1, 0).total, F_TOTAL);
    }

    // V-11: the finalized total of a pool with no root is zero.
    function test_V11_aPoolWithNoRootHasNoFinalizedTotal() public view {
        assertEq(vault.finalizedTotal(NO1), 0);
        assertEq(vault.rootCount(NO1), 0);
    }
}
