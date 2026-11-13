// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

// The trust boundary of settlement v2 against the "F2" report of 2026-11-05
// (silent downward restatement; VERSION2-FINAL.md §4.2 G2, §17). A root that
// lowers a finalized account finalizes only with privileged keys and no
// dispute inside vault2's window; every other path stops it.
//
// Scenario: finalized root 0 of pool NO1 (epoch FIRST_EPOCH) holds
//   [0] ALICE 600 (the victim), [1] BOB 400.
// Epoch NEXT is funded with 500 more (total 1,500). Its honest root is
//   [0] ALICE 700, [1] BOB 500, [2] CAROL 300;
// the restatement instead commits
//   [0] ALICE 0,   [1] BOB 400, [2] MALLORY 1,100   (MALLORY: never seen before).
// Amounts are alpha rao at 1e9 scale.
//
// Groups:
//   F2_*  the restatement under its precondition: the owner key, or the root
//         signer and the auditor quorum, and no dispute within the window
//   P_*   the precondition: which keys it needs
//   D_*   the on-chain defenses: free checkPath, demands, claims, the pause
//   T_*   what the event trace shows

import {Vm} from "forge-std/Vm.sol";
import {console2} from "forge-std/console2.sol";
import {IERC1967} from "@openzeppelin/contracts/interfaces/IERC1967.sol";

import {V2StackBase} from "./utils/V2StackBase.sol";
import {V2TreeHelper} from "./utils/V2TreeHelper.sol";
import {STSettlement2} from "../src/STSettlement2.sol";
import {ISTSettlement2} from "../src/interfaces/ISTSettlement2.sol";
import {ISTSettlementVault2} from "../src/interfaces/ISTSettlementVault2.sol";
import {LedgerTree} from "../src/LedgerTree.sol";

/// @dev What one owner-Safe signature (upgradeToAndCall) can install: an
///      STSettlement2 that forwards any call to vault2 as its coordinator,
///      skipping root signers, approvals, windows and pause.
contract V2F2HostileSettlement2 is STSettlement2 {
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

contract V2F2RestatementTest is V2StackBase {
    uint256 internal constant ALPHA = 1e9; // one alpha in rao
    uint256 internal constant NEXT = FIRST_EPOCH + 1;
    uint256 internal constant VICTIM = 0; // ALICE
    uint256 internal constant BYSTANDER = 1; // BOB
    uint256 internal constant NEW_LEAF = 2; // CAROL (honest) or MALLORY (theft)

    V2F2HostileSettlement2 internal hostile;

    function setUp() public override {
        super.setUp();
        _registerV2Pool(NO1, V2_POOL1, V2_POOL1_UID);
        // Root 0, through the reviewed path: root signer, auditor, window.
        _publishV2(FIRST_EPOCH, _keys(ALICE, BOB), _values(600 * ALPHA, 400 * ALPHA));
        assertEq(vault2.rootCount(NO1), 1);
        assertEq(vault2.finalizedTotal(NO1), 1_000 * ALPHA);
    }

    // ---------------------------------------------------------------- trees

    function _root0() internal pure returns (V2TreeHelper.Node[][] memory) {
        return V2TreeHelper.build(_keys(ALICE, BOB), _values(600 * ALPHA, 400 * ALPHA));
    }

    function _honest() internal pure returns (V2TreeHelper.Node[][] memory) {
        return V2TreeHelper.build(_keys(ALICE, BOB, CAROL), _values(700 * ALPHA, 500 * ALPHA, 300 * ALPHA));
    }

    function _restated() internal pure returns (V2TreeHelper.Node[][] memory) {
        return V2TreeHelper.build(_keys(ALICE, BOB, MALLORY), _values(0, 400 * ALPHA, 1_100 * ALPHA));
    }

    /// @dev The "maxYield" restatement: every finalized account zeroed.
    function _drained() internal pure returns (V2TreeHelper.Node[][] memory) {
        return V2TreeHelper.build(_keys(ALICE, BOB, MALLORY), _values(0, 0, 1_500 * ALPHA));
    }

    // -------------------------------------------------------------- helpers

    /// @dev The owner Safe (1-of-1 at launch) signs one upgrade.
    function _upgradeToHostile() internal {
        V2F2HostileSettlement2 implementation = new V2F2HostileSettlement2();
        vm.expectEmit(true, false, false, false, address(settlement));
        emit IERC1967.Upgraded(address(implementation));
        vm.prank(owner);
        settlement.upgradeToAndCall(address(implementation), "");
        hostile = V2F2HostileSettlement2(payable(address(settlement)));
    }

    function _captureNextHostile() internal {
        _accrueV2(V2_POOL1, 500 * ALPHA);
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

    function _finalizeHostile() internal {
        hostile.forward(abi.encodeCall(ISTSettlementVault2.finalizePending, (NO1)));
    }

    /// @dev Anyone may send a claim; it pays only the leaf's coldkey.
    function _claimAt(
        uint256 rootIndex,
        V2TreeHelper.Node[][] memory levels,
        uint256 index,
        bytes32 coldkey,
        uint256 cumulative
    ) internal returns (uint256) {
        LedgerTree.ProofNode[] memory proof = V2TreeHelper.proof(levels, index);
        vm.prank(stranger);
        return vault2.claim(NO1, rootIndex, _account(index, coldkey, cumulative), proof);
    }

    /// @dev Escrow stake a coldkey received from claims.
    function _paid(bytes32 coldkey) internal view returns (uint256) {
        return staking.stakes(V2_ESCROW, coldkey);
    }

    function _assertVaultInvariants() internal view {
        assertTrue(vault2.conservationHolds(), "conservationHolds");
        assertEq(vault2.totalCaptured(), vault2.totalPaid() + vault2.escrowAccounted(), "G1 identity");
        assertLe(vault2.claimedTotal(NO1), vault2.finalizedTotal(NO1), "claimed <= finalized");
        assertLe(vault2.finalizedTotal(NO1), vault2.capturedTotal(NO1), "finalized <= captured");
    }

    // ============================== F2: the restatement under its precondition

    /// F2-A. With the owner Safe's key: upgrade, restate Alice 600 -> 0,
    /// wait out the window unchallenged, finalize, and pay a fresh coldkey.
    function test_F2_A_ownerKeyUpgradeRestatesAFinalizedAccountAndTheTheftFinalizes() public {
        _upgradeToHostile();
        _captureNextHostile();
        assertEq(vault2.fundedThrough(NO1, NEXT), 1_500 * ALPHA);

        // vault2 holds roots, not accounts: nothing at commit compares
        // Alice's new value with her finalized one.
        uint64 seq = _commitHostile(NEXT, _restated(), 3);
        uint256 committed = block.number;
        assertEq(vault2.pending(NO1).seq, seq);

        // Nobody challenges or demands for 14,400 blocks (~48 h).
        vm.roll(committed + WINDOW);
        _finalizeHostile();
        assertEq(vault2.rootCount(NO1), 2);

        assertEq(_claimAt(1, _restated(), NEW_LEAF, MALLORY, 1_100 * ALPHA), 1_100 * ALPHA);
        assertEq(_claimAt(1, _restated(), BYSTANDER, BOB, 400 * ALPHA), 400 * ALPHA);
        assertEq(_paid(MALLORY), 1_100 * ALPHA);

        // The victim: zero in the restated root, and her finalized 600 in
        // root 0 no longer fits under the pool's cap.
        vm.expectRevert(ISTSettlementVault2.NothingToClaim.selector);
        _claimAt(1, _restated(), VICTIM, ALICE, 0);
        vm.expectRevert(ISTSettlementVault2.PoolCapExceeded.selector);
        _claimAt(0, _root0(), VICTIM, ALICE, 600 * ALPHA);
        assertEq(_paid(ALICE), 0);

        _assertVaultInvariants();
        assertEq(vault2.claimedTotal(NO1), vault2.finalizedTotal(NO1));
        console2.log("F2-A: restated root finalized after an unchallenged window");
        console2.log("  thief (fresh coldkey) paid, alpha:", _paid(MALLORY) / ALPHA);
        console2.log("  victim paid, alpha:", _paid(ALICE) / ALPHA, "(honest root would have owed 700)");
        console2.log("  conservationHolds:", vault2.conservationHolds());
    }

    /// F2-A'. The same theft with no owner action at all: the operator's root
    /// signer key plus the launch auditor key (quorum 1). Every call goes
    /// through the reviewed STSettlement2.
    function test_F2_A2_rootSignerPlusAuditorKeysDoItThroughTheReviewedCoordinator() public {
        _accrueV2(V2_POOL1, 500 * ALPHA);
        _closeV2(NEXT, NO1);
        _commitV2(NEXT, NO1, rootSigner1, _restated(), 3);
        _approveV2(NO1, auditor1);
        _finalizeV2(NO1);
        assertEq(vault2.rootCount(NO1), 2);

        assertEq(_claimAt(1, _restated(), NEW_LEAF, MALLORY, 1_100 * ALPHA), 1_100 * ALPHA);
        vm.expectRevert(ISTSettlementVault2.NothingToClaim.selector);
        _claimAt(1, _restated(), VICTIM, ALICE, 0);
        _assertVaultInvariants();
    }

    /// F2-B. A victim who already claimed is frozen: a successor built on the
    /// restated root credits her from 0 again, below her claimed 600, and the
    /// thief's 1,100 is paid from later funding once the cap allows it.
    function test_F2_B_aRestatementFreezesAVictimWhoAlreadyClaimed() public {
        assertEq(_claimAt(0, _root0(), VICTIM, ALICE, 600 * ALPHA), 600 * ALPHA);

        _upgradeToHostile();
        _captureNextHostile();
        _commitHostile(NEXT, _restated(), 3);
        vm.roll(block.number + WINDOW);
        _finalizeHostile();

        vm.expectRevert(ISTSettlementVault2.NothingToClaim.selector);
        _claimAt(1, _restated(), VICTIM, ALICE, 0);
        vm.expectRevert(ISTSettlementVault2.NothingToClaim.selector);
        _claimAt(0, _root0(), VICTIM, ALICE, 600 * ALPHA);
        // 600 already paid + 1,100 > the 1,500 cap: the thief waits.
        vm.expectRevert(ISTSettlementVault2.PoolCapExceeded.selector);
        _claimAt(1, _restated(), NEW_LEAF, MALLORY, 1_100 * ALPHA);

        // Next epoch, +500: a successor of the restated root credits Alice 200.
        _accrueV2(V2_POOL1, 500 * ALPHA);
        hostile.forward(abi.encodeCall(ISTSettlementVault2.captureEmission, (NEXT + 1, NO1)));
        V2TreeHelper.Node[][] memory later =
            V2TreeHelper.build(_keys(ALICE, BOB, MALLORY), _values(200 * ALPHA, 700 * ALPHA, 1_100 * ALPHA));
        _commitHostile(NEXT + 1, later, 3);
        vm.roll(block.number + WINDOW);
        _finalizeHostile();

        vm.expectRevert(ISTSettlementVault2.NothingToClaim.selector);
        _claimAt(2, later, VICTIM, ALICE, 200 * ALPHA);
        assertEq(_claimAt(2, later, NEW_LEAF, MALLORY, 1_100 * ALPHA), 1_100 * ALPHA);
        assertEq(_paid(ALICE), 600 * ALPHA);
        _assertVaultInvariants();
    }

    /// F2-Y. "maxYield": zero every finalized account and take the pool.
    function test_F2_Y_maxYieldTakesTheWholeUnclaimedPool() public {
        _upgradeToHostile();
        _captureNextHostile();
        _commitHostile(NEXT, _drained(), 3);
        vm.roll(block.number + WINDOW);
        _finalizeHostile();

        assertEq(_claimAt(1, _drained(), NEW_LEAF, MALLORY, 1_500 * ALPHA), 1_500 * ALPHA);
        vm.expectRevert(ISTSettlementVault2.PoolCapExceeded.selector);
        _claimAt(0, _root0(), VICTIM, ALICE, 600 * ALPHA);
        vm.expectRevert(ISTSettlementVault2.PoolCapExceeded.selector);
        _claimAt(0, _root0(), BYSTANDER, BOB, 400 * ALPHA);
        assertEq(vault2.claimedTotal(NO1), vault2.finalizedTotal(NO1));
        _assertVaultInvariants();
    }

    // ======================================================= precondition

    /// P-1. Without a privileged key the reviewed coordinator stops it: only
    /// the root signer commits, and only an auditor quorum finalizes.
    function test_P1_withoutPrivilegedKeysTheRestatementCannotFinalize() public {
        _accrueV2(V2_POOL1, 500 * ALPHA);
        _closeV2(NEXT, NO1);
        ISTSettlementVault2.Commitment memory c = _commitment(_restated(), 3);

        vm.expectRevert(ISTSettlement2.Unauthorized.selector);
        vm.prank(stranger);
        settlement.commitDistribution(NEXT, NO1, 1, c);
        vm.expectRevert(ISTSettlementVault2.Unauthorized.selector);
        vm.prank(stranger);
        vault2.commitPending(NEXT, NO1, 1, c);

        // A stolen root signer key alone commits it ...
        _commitV2(NEXT, NO1, rootSigner1, _restated(), 3);
        ISTSettlementVault2.Pending memory p = vault2.pending(NO1);
        vm.expectRevert(ISTSettlement2.Unauthorized.selector);
        vm.prank(stranger);
        settlement.approveDistribution(NO1, p.seq, p.root, p.ledgerHash);
        // ... but with no auditor approval it never finalizes.
        vm.roll(block.number + WINDOW);
        vm.expectRevert(ISTSettlement2.QuorumNotReached.selector);
        vm.prank(stranger);
        settlement.finalizeDistribution(NO1);
        assertEq(vault2.rootCount(NO1), 1);
    }

    /// P-2. The owner Safe alone can also replace the auditor set at once
    /// (no delay), then needs only the root signer to commit.
    function test_P2_theOwnerSafeSwapsTheAuditorSetWithNoDelay() public {
        address rogue = makeAddr("rogue-auditor");
        address[] memory auditors = new address[](1);
        auditors[0] = rogue;
        vm.prank(owner);
        settlement.setAuditors(auditors, 1);

        _accrueV2(V2_POOL1, 500 * ALPHA);
        _closeV2(NEXT, NO1);
        _commitV2(NEXT, NO1, rootSigner1, _restated(), 3);
        _approveV2(NO1, rogue);
        _finalizeV2(NO1);
        assertEq(_claimAt(1, _restated(), NEW_LEAF, MALLORY, 1_100 * ALPHA), 1_100 * ALPHA);
    }

    // =========================================================== defenses

    /// D-1. With the restated tree published (the operator API serves every
    /// root's objects), anyone deletes it with checkPath. No fee, no stake.
    function test_D1_anyoneDeletesAPublishedRestatementForFree() public {
        _upgradeToHostile();
        _captureNextHostile();
        uint64 seq = _commitHostile(NEXT, _restated(), 3);

        address watcher = makeAddr("unfunded-watcher");
        assertEq(watcher.balance, 0);
        vm.expectEmit(true, true, false, true, address(vault2));
        emit ISTSettlementVault2.DistributionDeleted(
            NO1, seq, 0, VICTIM, LedgerTree.Fault.LowerValue, 0, watcher
        );
        vm.prank(watcher);
        LedgerTree.Fault fault = vault2.checkPath(
            NO1,
            0,
            _account(VICTIM, ALICE, 600 * ALPHA),
            V2TreeHelper.proof(_root0(), VICTIM),
            V2TreeHelper.path(_restated(), VICTIM)
        );
        assertEq(uint8(fault), uint8(LedgerTree.Fault.LowerValue));
        assertEq(vault2.pending(NO1).seq, 0);

        vm.roll(block.number + WINDOW);
        vm.expectRevert(ISTSettlementVault2.NoPendingRoot.selector);
        _finalizeHostile();
        assertEq(_claimAt(0, _root0(), VICTIM, ALICE, 600 * ALPHA), 600 * ALPHA);
        _assertVaultInvariants();
    }

    /// D-2. With the tree withheld, one demand on the victim's account blocks
    /// finalization indefinitely: a forged opening does not answer it, and the
    /// true opening deletes the root.
    function test_D2_oneDemandBlocksAWithheldRestatement() public {
        _upgradeToHostile();
        _captureNextHostile();
        _commitHostile(NEXT, _restated(), 3);

        _demandV2(NO1, 0, _root0(), _account(VICTIM, ALICE, 600 * ALPHA));
        assertEq(vault2.pending(NO1).openDemands, 1);

        vm.roll(block.number + 10 * WINDOW);
        vm.expectRevert(ISTSettlementVault2.DemandOpen.selector);
        _finalizeHostile();

        LedgerTree.Account memory alice = _account(VICTIM, ALICE, 600 * ALPHA);
        LedgerTree.ProofNode[] memory proof = V2TreeHelper.proof(_root0(), VICTIM);
        vm.expectRevert(LedgerTree.MalformedPath.selector);
        vault2.checkPath(NO1, 0, alice, proof, V2TreeHelper.path(_honest(), VICTIM));

        LedgerTree.Fault fault = vault2.checkPath(NO1, 0, alice, proof, V2TreeHelper.path(_restated(), VICTIM));
        assertEq(uint8(fault), uint8(LedgerTree.Fault.LowerValue));
        assertEq(vault2.pending(NO1).seq, 0);
    }

    /// D-3. A demand on an untouched account is answered and does not block;
    /// this is the residual risk: a lowered account no one demands is lost
    /// (VERSION2-FINAL.md §4.2 G2, §17).
    function test_D3_aDemandSampleThatMissesTheVictimLetsTheRestatementThrough() public {
        _upgradeToHostile();
        _captureNextHostile();
        _commitHostile(NEXT, _restated(), 3);

        _demandV2(NO1, 0, _root0(), _account(BYSTANDER, BOB, 400 * ALPHA));
        // The operator answers Bob's demand truthfully: Bob is untouched.
        vault2.checkPath(
            NO1,
            0,
            _account(BYSTANDER, BOB, 400 * ALPHA),
            V2TreeHelper.proof(_root0(), BYSTANDER),
            V2TreeHelper.path(_restated(), BYSTANDER)
        );
        assertEq(vault2.pending(NO1).openDemands, 0);

        vm.roll(block.number + WINDOW);
        _finalizeHostile();
        assertEq(_claimAt(1, _restated(), NEW_LEAF, MALLORY, 1_100 * ALPHA), 1_100 * ALPHA);
    }

    /// D-4. The victim is not frozen if anyone claims her finalized balance
    /// while the restatement is pending; the thief is then over the cap.
    function test_D4_claimingTheVictimDuringTheWindowDefeatsTheTheft() public {
        _upgradeToHostile();
        _captureNextHostile();
        _commitHostile(NEXT, _restated(), 3);

        assertEq(_claimAt(0, _root0(), VICTIM, ALICE, 600 * ALPHA), 600 * ALPHA);
        assertEq(_claimAt(0, _root0(), BYSTANDER, BOB, 400 * ALPHA), 400 * ALPHA);

        vm.roll(block.number + WINDOW);
        _finalizeHostile();
        vm.expectRevert(ISTSettlementVault2.PoolCapExceeded.selector);
        _claimAt(1, _restated(), NEW_LEAF, MALLORY, 1_100 * ALPHA);
        assertEq(_paid(ALICE), 600 * ALPHA);
        assertEq(_paid(MALLORY), 0);
        _assertVaultInvariants();
    }

    /// D-5. The guardian (a separate key that can only pause) stops the
    /// hot-key path: the reviewed coordinator will not finalize while v1 is
    /// paused, and challenges still work. A hostile upgrade ignores the pause.
    function test_D5_theGuardianPauseStopsTheHotKeyPathButNotAHostileUpgrade() public {
        _accrueV2(V2_POOL1, 500 * ALPHA);
        _closeV2(NEXT, NO1);
        uint64 seq = _commitV2(NEXT, NO1, rootSigner1, _restated(), 3);
        _approveV2(NO1, auditor1);

        vm.prank(guardian);
        coordinator.setPaused(true);
        vm.roll(block.number + WINDOW);
        vm.expectRevert(ISTSettlement2.SettlementPaused.selector);
        vm.prank(stranger);
        settlement.finalizeDistribution(NO1);
        assertEq(vault2.pending(NO1).seq, seq);

        // A hostile implementation skips the pause.
        _upgradeToHostile();
        _finalizeHostile();
        assertEq(vault2.rootCount(NO1), 2);
    }

    // ============================================================== traces

    /// T-1. Through the reviewed coordinator (A'), the theft emits the same
    /// event types from the same contracts, in the same order, as the honest
    /// epoch. The emitted root and ledgerHash differ, and the published
    /// objects behind them are what a watcher checks.
    function test_T1_theRootSignerPlusAuditorTheftHasTheHonestEventSequence() public {
        uint256 snapshot = vm.snapshotState();
        Vm.Log[] memory honest = _recordReviewedEpoch(_honest(), CAROL, 300 * ALPHA, 500 * ALPHA);
        vm.revertToState(snapshot);
        Vm.Log[] memory theft = _recordReviewedEpoch(_restated(), MALLORY, 1_100 * ALPHA, 400 * ALPHA);

        assertEq(honest.length, theft.length, "event count");
        for (uint256 i; i < honest.length; i++) {
            assertEq(honest[i].emitter, theft[i].emitter, "emitter");
            assertEq(honest[i].topics[0], theft[i].topics[0], "event type");
        }
        _assertNoDisputeEvents(theft);
        console2.log("T-1: honest and theft epochs emit the same", theft.length, "event types in order");
    }

    /// T-2. The owner-key path is not traceless: it starts with an Upgraded
    /// event on the STSettlement2 proxy (P-2's starts with AuditorsSet).
    function test_T2_theOwnerKeyPathEmitsAnUpgradedEvent() public {
        vm.recordLogs();
        _upgradeToHostile();
        _captureNextHostile();
        _commitHostile(NEXT, _restated(), 3);
        vm.roll(block.number + WINDOW);
        _finalizeHostile();
        Vm.Log[] memory logs = vm.getRecordedLogs();
        bool upgraded;
        for (uint256 i; i < logs.length; i++) {
            if (logs[i].emitter == address(settlement) && logs[i].topics[0] == IERC1967.Upgraded.selector) {
                upgraded = true;
            }
        }
        assertTrue(upgraded, "Upgraded on the STSettlement2 proxy");
        _assertNoDisputeEvents(logs);
    }

    function _recordReviewedEpoch(
        V2TreeHelper.Node[][] memory levels,
        bytes32 newColdkey,
        uint256 newValue,
        uint256 bobValue
    ) internal returns (Vm.Log[] memory) {
        vm.recordLogs();
        _accrueV2(V2_POOL1, 500 * ALPHA);
        _closeV2(NEXT, NO1);
        _commitV2(NEXT, NO1, rootSigner1, levels, 3);
        _approveV2(NO1, auditor1);
        _finalizeV2(NO1);
        _claimAt(1, levels, NEW_LEAF, newColdkey, newValue);
        _claimAt(1, levels, BYSTANDER, BOB, bobValue);
        return vm.getRecordedLogs();
    }

    function _assertNoDisputeEvents(Vm.Log[] memory logs) internal pure {
        for (uint256 i; i < logs.length; i++) {
            bytes32 t = logs[i].topics[0];
            assertTrue(
                t != ISTSettlementVault2.DistributionDeleted.selector
                    && t != ISTSettlementVault2.PathDemanded.selector,
                "no dispute event"
            );
        }
    }
}
