// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {V2StackBase} from "./utils/V2StackBase.sol";
import {V2TreeHelper} from "./utils/V2TreeHelper.sol";
import {STSettlement2} from "../src/STSettlement2.sol";
import {ISTSettlementVault2} from "../src/interfaces/ISTSettlementVault2.sol";
import {LedgerTree} from "../src/LedgerTree.sol";

/// @dev A hostile STSettlement2 upgrade. It keeps the proxy's storage and
///      makes vault2 run any call as its coordinator, for anyone, skipping
///      every STSettlement2 check: windows, root signers, approvals and pause.
contract HostileSettlement2 is STSettlement2 {
    function forward(bytes calldata data) external payable returns (bytes memory result) {
        bool ok;
        (ok, result) = vault.call{value: msg.value}(data);
        if (!ok) {
            assembly ("memory-safe") {
                revert(add(result, 32), mload(result))
            }
        }
    }
}

/// @dev S-10: after the owner upgrades STSettlement2 to a hostile
///      implementation (pattern: ReleaseSettlement.t.sol:277-350), vault2 alone
///      still enforces the window, demands, the funding binding, commit
///      spacing, the surplus bound, funding order and every claim's payment
///      (VERSION2-FINAL.md §4.2 G1-G6). Root 0 of pool NO1, for FIRST_EPOCH,
///      holds Alice 600 and Bob 400; then the upgrade lands.
contract V2SettlementHostileTest is V2StackBase {
    uint256 internal constant NEXT = FIRST_EPOCH + 1;
    bytes32 internal constant FOREIGN = keccak256("v1-escrow-hotkey");

    HostileSettlement2 internal hostile;

    function setUp() public override {
        super.setUp();
        _registerV2Pool(NO1, V2_POOL1, V2_POOL1_UID);
        _publishV2(FIRST_EPOCH, _keys(ALICE, BOB), _values(600, 400));
        HostileSettlement2 implementation = new HostileSettlement2();
        vm.prank(owner);
        settlement.upgradeToAndCall(address(implementation), "");
        hostile = HostileSettlement2(payable(address(settlement)));
    }

    function _root0() internal pure returns (V2TreeHelper.Node[][] memory) {
        return V2TreeHelper.build(_keys(ALICE, BOB), _values(600, 400));
    }

    // NEXT's honest successor: Alice and Bob gain, Carol joins (total 1,500).
    function _honest() internal pure returns (V2TreeHelper.Node[][] memory) {
        return V2TreeHelper.build(_keys(ALICE, BOB, CAROL), _values(700, 500, 300));
    }

    // The hostile coordinator captures NEXT with 500 more, at once.
    function _captureNext() internal {
        _accrueV2(V2_POOL1, 500);
        hostile.forward(abi.encodeCall(ISTSettlementVault2.captureEmission, (NEXT, NO1)));
    }

    function _commitHostile(uint256 epoch_, V2TreeHelper.Node[][] memory levels, uint256 leafCount)
        internal
        returns (uint64 seq)
    {
        bytes memory result = hostile.forward(
            abi.encodeCall(
                ISTSettlementVault2.commitPending,
                (epoch_, NO1, vault2.rootCount(NO1), _commitment(levels, leafCount))
            )
        );
        return abi.decode(result, (uint64));
    }

    function _forwardFinalize() internal {
        hostile.forward(abi.encodeCall(ISTSettlementVault2.finalizePending, (NO1)));
    }

    function _raw(uint256 total) internal pure returns (ISTSettlementVault2.Commitment memory) {
        bytes32 root = keccak256(abi.encode("hostile-root", total));
        return ISTSettlementVault2.Commitment({
            root: root, total: total, leafCount: 3, ledgerHash: keccak256(abi.encode("ledger-header", root))
        });
    }

    // S-10, G6: no approval is needed by vault2, but no root finalizes before
    // 14,400 blocks have passed since its commit.
    function test_S10_aHostileUpgradeCannotFinalizeEarly() public {
        _captureNext();
        uint64 seq = _commitHostile(NEXT, _honest(), 3);
        uint256 committed = block.number;

        vm.expectRevert(ISTSettlementVault2.ChallengeWindowOpen.selector);
        _forwardFinalize();
        vm.roll(committed + WINDOW - 1);
        vm.expectRevert(ISTSettlementVault2.ChallengeWindowOpen.selector);
        _forwardFinalize();
        assertEq(vault2.pending(NO1).seq, seq);
        assertEq(vault2.rootCount(NO1), 1);

        vm.roll(committed + WINDOW);
        _forwardFinalize();
        assertEq(vault2.rootCount(NO1), 2);
    }

    // S-10, G6: an open demand blocks finalization however long it stays open.
    function test_S10_aHostileUpgradeCannotFinalizeWithAnOpenDemand() public {
        _captureNext();
        uint64 seq = _commitHostile(NEXT, _honest(), 3);
        _demandV2(NO1, 0, _root0(), _account(1, BOB, 400));

        vm.roll(block.number + WINDOW * 10);
        vm.expectRevert(ISTSettlementVault2.DemandOpen.selector);
        _forwardFinalize();
        assertEq(vault2.pending(NO1).seq, seq);
        assertEq(vault2.pending(NO1).openDemands, 1);
        assertEq(vault2.rootCount(NO1), 1);
    }

    // S-10, G5: a root's total is exactly its epoch's funding snapshot. Neither
    // a larger nor a smaller total, nor an unfunded epoch, nor surplus assigned
    // after the epoch was funded, gets in.
    function test_S10_aHostileUpgradeCannotCommitATotalOtherThanTheFundingSnapshot() public {
        _captureNext();
        assertEq(vault2.fundedThrough(NO1, NEXT), 1_500);
        bytes memory larger = abi.encodeCall(ISTSettlementVault2.commitPending, (NEXT, NO1, 1, _raw(1_501)));
        vm.expectRevert(ISTSettlementVault2.TotalMismatch.selector);
        hostile.forward(larger);
        bytes memory smaller = abi.encodeCall(ISTSettlementVault2.commitPending, (NEXT, NO1, 1, _raw(1_499)));
        vm.expectRevert(ISTSettlementVault2.TotalMismatch.selector);
        hostile.forward(smaller);
        bytes memory unfunded =
            abi.encodeCall(ISTSettlementVault2.commitPending, (NEXT + 1, NO1, 1, _raw(1_500)));
        vm.expectRevert(ISTSettlementVault2.EpochOutOfRange.selector);
        hostile.forward(unfunded);

        staking.setStake(FOREIGN, V2_SELF, 300);
        vault2.absorbStake(FOREIGN);
        hostile.forward(abi.encodeCall(ISTSettlementVault2.assignSurplus, (NO1, 300)));
        assertEq(vault2.capturedTotal(NO1), 1_800);
        bytes memory withSurplus =
            abi.encodeCall(ISTSettlementVault2.commitPending, (NEXT, NO1, 1, _raw(1_800)));
        vm.expectRevert(ISTSettlementVault2.TotalMismatch.selector);
        hostile.forward(withSurplus);
        assertEq(vault2.pending(NO1).seq, 0);
    }

    // S-10, G2: commits are 14,400 blocks apart, so a demand sample cannot be
    // re-rolled by replacing the pending root.
    function test_S10_aHostileUpgradeCannotCommitWithinTheWindowOfThePreviousCommit() public {
        _captureNext();
        uint64 seq = _commitHostile(NEXT, _honest(), 3);
        uint256 committed = block.number;
        V2TreeHelper.Node[][] memory other =
            V2TreeHelper.build(_keys(ALICE, BOB, MALLORY), _values(600, 400, 500));
        bytes memory replacement =
            abi.encodeCall(ISTSettlementVault2.commitPending, (NEXT, NO1, 1, _commitment(other, 3)));

        vm.roll(committed + WINDOW - 1);
        vm.expectRevert(ISTSettlementVault2.CommitTooSoon.selector);
        hostile.forward(replacement);
        assertEq(vault2.pending(NO1).seq, seq);

        vm.roll(committed + WINDOW);
        hostile.forward(replacement);
        assertEq(vault2.pending(NO1).seq, seq + 1);
    }

    // S-10, G2: nor by proving a fault in its own root and committing again.
    function test_S10_aHostileUpgradeCannotRecommitAtOnceAfterDeletingItsOwnRoot() public {
        _captureNext();
        V2TreeHelper.Node[][] memory lowered =
            V2TreeHelper.build(_keys(ALICE, BOB, CAROL), _values(599, 500, 401));
        _commitHostile(NEXT, lowered, 3);
        uint256 committed = block.number;
        LedgerTree.Fault fault = vault2.checkPath(
            NO1, 0, _account(0, ALICE, 600), V2TreeHelper.proof(_root0(), 0), V2TreeHelper.path(lowered, 0)
        );
        assertEq(uint8(fault), uint8(LedgerTree.Fault.LowerValue));
        assertEq(vault2.pending(NO1).seq, 0);

        bytes memory again =
            abi.encodeCall(ISTSettlementVault2.commitPending, (NEXT, NO1, 1, _commitment(_honest(), 3)));
        vm.roll(committed + WINDOW - 1);
        vm.expectRevert(ISTSettlementVault2.CommitTooSoon.selector);
        hostile.forward(again);

        vm.roll(committed + WINDOW);
        hostile.forward(again);
        assertEq(vault2.pending(NO1).total, 1_500);
    }

    // S-10, G1: only escrow stake no pool accounts for can be assigned.
    function test_S10_aHostileUpgradeCannotAssignMoreThanTheSurplus() public {
        bytes memory one = abi.encodeCall(ISTSettlementVault2.assignSurplus, (NO1, 1));
        vm.expectRevert(ISTSettlementVault2.InvalidSurplus.selector);
        hostile.forward(one);

        staking.setStake(FOREIGN, V2_SELF, 300);
        vault2.absorbStake(FOREIGN);
        bytes memory above = abi.encodeCall(ISTSettlementVault2.assignSurplus, (NO1, 301));
        vm.expectRevert(ISTSettlementVault2.InvalidSurplus.selector);
        hostile.forward(above);

        hostile.forward(abi.encodeCall(ISTSettlementVault2.assignSurplus, (NO1, 300)));
        vm.expectRevert(ISTSettlementVault2.InvalidSurplus.selector);
        hostile.forward(one);
        assertEq(vault2.capturedTotal(NO1), 1_300);
        assertEq(vault2.escrowAccounted(), vault2.liveEscrowStake());
        assertTrue(vault2.conservationHolds());
    }

    // S-10, G5: epochs are funded strictly in order, and a funded epoch is
    // never funded again.
    function test_S10_aHostileUpgradeCannotCaptureOutOfOrder() public {
        _accrueV2(V2_POOL1, 500);
        bytes memory skipped = abi.encodeCall(ISTSettlementVault2.captureEmission, (NEXT + 1, NO1));
        vm.expectRevert(ISTSettlementVault2.InvalidTransition.selector);
        hostile.forward(skipped);
        bytes memory again = abi.encodeCall(ISTSettlementVault2.captureEmission, (FIRST_EPOCH, NO1));
        vm.expectRevert(ISTSettlementVault2.InvalidTransition.selector);
        hostile.forward(again);
        bytes memory deferred = abi.encodeCall(ISTSettlementVault2.deferEmission, (FIRST_EPOCH, NO1));
        vm.expectRevert(ISTSettlementVault2.InvalidTransition.selector);
        hostile.forward(deferred);

        assertEq(vault2.lastFundedEpoch(NO1), FIRST_EPOCH);
        assertEq(vault2.fundedThrough(NO1, FIRST_EPOCH), 1_000);
        assertEq(staking.stakes(V2_POOL1, V2_SELF), 500);
    }

    // S-10, G3: the hostile coordinator cannot take over vault2, cannot get a
    // moved coldkey past the window, and its own claim pays the leaf's coldkey.
    function test_S10_aHostileUpgradeCannotRedirectAClaim() public {
        bytes memory takeover = abi.encodeCall(ISTSettlementVault2.setCoordinatorOnce, (stranger));
        vm.expectRevert(ISTSettlementVault2.Unauthorized.selector);
        hostile.forward(takeover);

        // Alice's position goes to Mallory; a watcher shows the fault in time.
        _captureNext();
        V2TreeHelper.Node[][] memory moved =
            V2TreeHelper.build(_keys(MALLORY, BOB, CAROL), _values(700, 500, 300));
        uint64 seq = _commitHostile(NEXT, moved, 3);
        vm.expectRevert(ISTSettlementVault2.ChallengeWindowOpen.selector);
        _forwardFinalize();
        vm.expectEmit(address(vault2));
        emit ISTSettlementVault2.DistributionDeleted(NO1, seq, 0, 0, LedgerTree.Fault.WrongLeaf, 0, stranger);
        vm.prank(stranger);
        vault2.checkPath(
            NO1, 0, _account(0, ALICE, 600), V2TreeHelper.proof(_root0(), 0), V2TreeHelper.path(moved, 0)
        );
        vm.roll(block.number + WINDOW);
        vm.expectRevert(ISTSettlementVault2.NoPendingRoot.selector);
        _forwardFinalize();

        LedgerTree.ProofNode[] memory proof = V2TreeHelper.proof(_root0(), 0);
        bytes memory renamed =
            abi.encodeCall(ISTSettlementVault2.claim, (NO1, 0, _account(0, MALLORY, 600), proof));
        vm.expectRevert(ISTSettlementVault2.InvalidProof.selector);
        hostile.forward(renamed);

        vm.expectEmit(address(vault2));
        emit ISTSettlementVault2.Claimed(NO1, 0, 0, ALICE, 600, 600, address(hostile));
        hostile.forward(abi.encodeCall(ISTSettlementVault2.claim, (NO1, 0, _account(0, ALICE, 600), proof)));
        assertEq(staking.stakes(V2_ESCROW, ALICE), 600);
        assertEq(staking.stakes(V2_ESCROW, MALLORY), 0);
    }

    // S-10, S5, G4: with v1 paused and the hostile coordinator in place, the
    // escrow cannot be drained or the cap lowered, and even an unchallenged
    // root that lowers Alice leaves her root-0 balance claimable.
    function test_S10_aHostileUpgradeCannotStopAClaim() public {
        vm.prank(guardian);
        coordinator.setPaused(true);

        bytes memory drain = abi.encodeCall(ISTSettlementVault2.absorbStake, (V2_ESCROW));
        vm.expectRevert(ISTSettlementVault2.InvalidHotkey.selector);
        hostile.forward(drain);
        bytes memory escrowAsPool =
            abi.encodeCall(ISTSettlementVault2.registerPool, (NO2, V2_ESCROW, V2_BURN_LIMIT, FIRST_EPOCH));
        vm.expectRevert(ISTSettlementVault2.InvalidConfiguration.selector);
        hostile.forward(escrowAsPool);
        hostile.forward(abi.encodeCall(ISTSettlementVault2.deferEmission, (NEXT, NO1)));
        bytes memory lowerCap = abi.encodeCall(ISTSettlementVault2.commitPending, (NEXT, NO1, 1, _raw(1_000)));
        vm.expectRevert(ISTSettlementVault2.TotalNotIncreased.selector);
        hostile.forward(lowerCap);

        // Nobody watches: a root lowering Alice to 100 finalizes after the window.
        _accrueV2(V2_POOL1, 500);
        hostile.forward(abi.encodeCall(ISTSettlementVault2.captureEmission, (NEXT + 1, NO1)));
        V2TreeHelper.Node[][] memory lowered =
            V2TreeHelper.build(_keys(ALICE, BOB, MALLORY), _values(100, 400, 1_000));
        _commitHostile(NEXT + 1, lowered, 3);
        vm.roll(block.number + WINDOW);
        _forwardFinalize();
        assertEq(vault2.rootCount(NO1), 2);

        vm.prank(stranger);
        assertEq(vault2.claim(NO1, 0, _account(0, ALICE, 600), V2TreeHelper.proof(_root0(), 0)), 600);
        assertEq(staking.stakes(V2_ESCROW, ALICE), 600);
        assertTrue(coordinator.paused());
        assertTrue(vault2.conservationHolds());
    }
}
