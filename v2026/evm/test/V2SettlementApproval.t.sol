// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {V2SettlementBase} from "./utils/V2SettlementBase.sol";
import {ISTSettlement2} from "../src/interfaces/ISTSettlement2.sol";
import {ISTSettlementVault2} from "../src/interfaces/ISTSettlementVault2.sol";

/// @dev Auditor approval and finalization of pending roots
///      (VERSION2-FINAL.md §7.2, §9.3, §15.4: S-2, S-7, S-8, S-EV).
contract V2SettlementApprovalTest is V2SettlementBase {
    uint64 internal seq;

    function setUp() public override {
        super.setUp();
        _registerPool1();
        _setAuditors(_list(auditor1, auditor2, auditor3), 2);
        seq = _commit(FIRST_EPOCH, ROOT);
    }

    // S-7, S-EV: each auditor counts once, at the current auditor set version.
    function test_approveCountsEachAuditor() public {
        assertEq(settlement.approvalCount(NO1), 0);
        vm.expectEmit(true, true, true, true, address(settlement));
        emit ISTSettlement2.DistributionApproved(NO1, seq, auditor1, 2, 1);
        _approve(auditor1, seq, ROOT);
        vm.expectEmit(true, true, true, true, address(settlement));
        emit ISTSettlement2.DistributionApproved(NO1, seq, auditor3, 2, 2);
        _approve(auditor3, seq, ROOT);

        assertEq(settlement.approvalCount(NO1), 2);
        assertTrue(settlement.hasApproved(NO1, auditor1));
        assertFalse(settlement.hasApproved(NO1, auditor2));
        assertTrue(settlement.hasApproved(NO1, auditor3));
    }

    // S-7
    function test_approveIsAuditorOnly() public {
        address[3] memory others = [owner, rootSigner1, stranger];
        for (uint256 i; i < others.length; i++) {
            vm.prank(others[i]);
            vm.expectRevert(ISTSettlement2.Unauthorized.selector);
            settlement.approveDistribution(NO1, seq, ROOT, LEDGER);
        }
    }

    // S-7: a removed auditor can no longer approve.
    function test_approveRefusesARemovedAuditor() public {
        _setAuditors(_list(auditor2), 1);
        vm.prank(auditor1);
        vm.expectRevert(ISTSettlement2.Unauthorized.selector);
        settlement.approveDistribution(NO1, seq, ROOT, LEDGER);
    }

    // S-7
    function test_approveRefusesWithNothingPending() public {
        vm.prank(auditor1);
        vm.expectRevert(ISTSettlement2.NoPendingRoot.selector);
        settlement.approveDistribution(NO2, 1, ROOT, LEDGER);
    }

    // S-7
    function test_approveRefusesAnotherSeq() public {
        vm.prank(auditor1);
        vm.expectRevert(ISTSettlement2.PendingMismatch.selector);
        settlement.approveDistribution(NO1, seq + 1, ROOT, LEDGER);
    }

    // S-7
    function test_approveRefusesAnotherRoot() public {
        vm.prank(auditor1);
        vm.expectRevert(ISTSettlement2.PendingMismatch.selector);
        settlement.approveDistribution(NO1, seq, keccak256("other-root"), LEDGER);
    }

    // S-7
    function test_approveRefusesAnotherLedgerHash() public {
        vm.prank(auditor1);
        vm.expectRevert(ISTSettlement2.PendingMismatch.selector);
        settlement.approveDistribution(NO1, seq, ROOT, keccak256("other-ledger"));
    }

    // S-7: the deadline is end(pending.epoch + 1), exclusive.
    function test_approveAcceptsTheLastBlockBeforeTheDeadline() public {
        vm.roll(_end(FIRST_EPOCH + 1) - 1);
        _approve(auditor1, seq, ROOT);
        assertEq(settlement.approvalCount(NO1), 1);
    }

    // S-7
    function test_approveRefusesAtTheDeadline() public {
        vm.roll(_end(FIRST_EPOCH + 1));
        vm.prank(auditor1);
        vm.expectRevert(ISTSettlement2.ApprovalClosed.selector);
        settlement.approveDistribution(NO1, seq, ROOT, LEDGER);
    }

    // S-7
    function test_approveTwiceRevertsAlreadyApproved() public {
        _approve(auditor1, seq, ROOT);
        vm.prank(auditor1);
        vm.expectRevert(ISTSettlement2.AlreadyApproved.selector);
        settlement.approveDistribution(NO1, seq, ROOT, LEDGER);
    }

    // S-7, S5: approvals never stop for a pause.
    function test_approvalsContinueWhileV1IsPaused() public {
        _pause();
        _approve(auditor1, seq, ROOT);
        assertEq(settlement.approvalCount(NO1), 1);
    }

    // S-8, S2: anyone finalizes a root with a quorum of approvals.
    function test_finalizeWithAQuorum() public {
        _approve(auditor1, seq, ROOT);
        _approve(auditor2, seq, ROOT);
        vm.prank(stranger);
        assertEq(settlement.finalizeDistribution(NO1), 0);
        assertEq(vault2.callCount(), 3);
        _assertVaultCall(2, 0, abi.encodeCall(ISTSettlementVault2.finalizePending, (NO1)));
        assertEq(vault2.pending(NO1).seq, 0);
        assertEq(settlement.approvalCount(NO1), 0);
        assertFalse(settlement.hasApproved(NO1, auditor1));
    }

    // S-8
    function test_finalizeRefusesWithNothingPending() public {
        vm.expectRevert(ISTSettlement2.NoPendingRoot.selector);
        settlement.finalizeDistribution(NO2);
    }

    // S-8
    function test_finalizeRefusesBelowQuorum() public {
        _approve(auditor1, seq, ROOT);
        vm.expectRevert(ISTSettlement2.QuorumNotReached.selector);
        settlement.finalizeDistribution(NO1);
    }

    // S-8, S-2: a new auditor set version voids earlier approvals, even from
    // the same auditors.
    function test_finalizeIgnoresApprovalsFromAnEarlierAuditorSet() public {
        _approve(auditor1, seq, ROOT);
        _approve(auditor2, seq, ROOT);
        _setAuditors(_list(auditor1, auditor2, auditor3), 2);
        assertEq(settlement.approvalCount(NO1), 0);
        assertFalse(settlement.hasApproved(NO1, auditor1));
        vm.expectRevert(ISTSettlement2.QuorumNotReached.selector);
        settlement.finalizeDistribution(NO1);

        vm.expectEmit(true, true, true, true, address(settlement));
        emit ISTSettlement2.DistributionApproved(NO1, seq, auditor1, 3, 1);
        _approve(auditor1, seq, ROOT);
        _approve(auditor2, seq, ROOT);
        assertEq(settlement.finalizeDistribution(NO1), 0);
    }

    // S-8: an approval of an earlier seq never counts for its replacement.
    function test_finalizeIgnoresApprovalsOfAnEarlierSeq() public {
        _approve(auditor1, seq, ROOT);
        _approve(auditor2, seq, ROOT);
        bytes32 next = keccak256("next-root");
        uint64 nextSeq = _commit(FIRST_EPOCH + 1, next);
        assertEq(settlement.approvalCount(NO1), 0);
        vm.expectRevert(ISTSettlement2.QuorumNotReached.selector);
        settlement.finalizeDistribution(NO1);

        vm.prank(auditor3);
        vm.expectRevert(ISTSettlement2.PendingMismatch.selector);
        settlement.approveDistribution(NO1, seq, ROOT, LEDGER);

        _approve(auditor1, nextSeq, next);
        _approve(auditor2, nextSeq, next);
        assertEq(settlement.finalizeDistribution(NO1), 0);
    }

    // S-8: vault2 enforces the challenge window and open demands.
    function test_finalizePropagatesVault2Refusals() public {
        _approve(auditor1, seq, ROOT);
        _approve(auditor2, seq, ROOT);
        bytes4[2] memory refusals =
            [ISTSettlementVault2.ChallengeWindowOpen.selector, ISTSettlementVault2.DemandOpen.selector];
        for (uint256 i; i < refusals.length; i++) {
            vault2.setFailure(abi.encodeWithSelector(refusals[i]));
            vm.expectRevert(refusals[i]);
            settlement.finalizeDistribution(NO1);
        }
    }

    // S-8, S5
    function test_finalizeIsRefusedWhileV1IsPaused() public {
        _approve(auditor1, seq, ROOT);
        _approve(auditor2, seq, ROOT);
        _pause();
        vm.expectRevert(ISTSettlement2.SettlementPaused.selector);
        settlement.finalizeDistribution(NO1);
    }
}
