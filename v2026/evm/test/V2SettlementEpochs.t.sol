// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {OwnableUpgradeable} from "@openzeppelin/contracts-upgradeable/access/OwnableUpgradeable.sol";

import {V2SettlementBase} from "./utils/V2SettlementBase.sol";
import {STCoordinator} from "../src/STCoordinator.sol";
import {ISTSettlement2} from "../src/interfaces/ISTSettlement2.sol";
import {ISTSettlementVault2} from "../src/interfaces/ISTSettlementVault2.sol";

/// @dev Rejects every native transfer, so a refund to it fails.
contract RefundRejector {}

/// @dev Pool registration, identities, closes and commits
///      (VERSION2-FINAL.md §5, §7.2, §15.4: S-3 to S-6).
contract V2SettlementEpochsTest is V2SettlementBase {
    // S-3
    function test_registerOperatorPoolRegistersThePoolInVault2() public {
        vm.deal(owner, 1 ether);
        vm.prank(owner);
        uint16 uid =
            settlement.registerOperatorPool{value: 1 ether}(NO1, V2_POOL1, V2_BURN_LIMIT, FIRST_EPOCH);
        assertEq(uid, vault2.UID());
        assertEq(vault2.callCount(), 1);
        _assertVaultCall(
            0,
            1 ether,
            abi.encodeCall(ISTSettlementVault2.registerPool, (NO1, V2_POOL1, V2_BURN_LIMIT, FIRST_EPOCH))
        );
        assertEq(address(vault2).balance, 1 ether);
        assertEq(address(settlement).balance, 0);
        assertEq(owner.balance, 0);
    }

    // S-3: exactly the refund vault2 returns goes back to the owner; a balance
    // held before the call is not paid out.
    function test_registerOperatorPoolForwardsTheVaultRefund() public {
        vault2.setRefund(0.25 ether);
        vm.deal(address(settlement), 0.1 ether);
        vm.deal(owner, 1 ether);
        vm.prank(owner);
        settlement.registerOperatorPool{value: 1 ether}(NO1, V2_POOL1, V2_BURN_LIMIT, FIRST_EPOCH);
        assertEq(address(vault2).balance, 0.75 ether);
        assertEq(address(settlement).balance, 0.1 ether);
        assertEq(owner.balance, 0.25 ether);
    }

    // S-3: native value is accepted only as vault2's registration refund.
    function test_receiveRefusesNativeValueFromAnyoneButTheVault() public {
        address[3] memory senders = [stranger, owner, address(coordinator)];
        for (uint256 i; i < senders.length; i++) {
            vm.deal(senders[i], 1 ether);
            vm.prank(senders[i]);
            (bool sent, bytes memory revertData) = address(settlement).call{value: 1 ether}("");
            assertFalse(sent);
            assertEq(revertData, abi.encodeWithSelector(ISTSettlement2.Unauthorized.selector));
        }
        assertEq(address(settlement).balance, 0);
    }

    // S-3
    function test_registerOperatorPoolRefusesAFailedRefund() public {
        RefundRejector rejector = new RefundRejector();
        vm.prank(owner);
        settlement.transferOwnership(address(rejector));
        vault2.setRefund(0.25 ether);
        vm.deal(address(rejector), 1 ether);
        vm.prank(address(rejector));
        vm.expectRevert(ISTSettlement2.NativeRefundFailed.selector);
        settlement.registerOperatorPool{value: 1 ether}(NO1, V2_POOL1, V2_BURN_LIMIT, FIRST_EPOCH);
    }

    // S-3
    function test_registerOperatorPoolIsOwnerOnly() public {
        vm.prank(stranger);
        vm.expectRevert(
            abi.encodeWithSelector(OwnableUpgradeable.OwnableUnauthorizedAccount.selector, stranger)
        );
        settlement.registerOperatorPool(NO1, V2_POOL1, V2_BURN_LIMIT, FIRST_EPOCH);
    }

    // S-3: the v1 operator must be active now.
    function test_registerOperatorPoolRefusesAnInactiveOperator() public {
        vm.prank(owner);
        coordinator.scheduleOperator(NO1, DEPOSIT1, depositSigner1, rootSigner1, false, 1);
        vm.roll(_end(0));
        assertEq(coordinator.currentEpoch(), 1);
        vm.prank(owner);
        vm.expectRevert(ISTSettlement2.InactiveOperator.selector);
        settlement.registerOperatorPool(NO1, V2_POOL1, V2_BURN_LIMIT, FIRST_EPOCH);
    }

    // S-3: an unknown v1 operator is refused by the v1 coordinator.
    function test_registerOperatorPoolRefusesAnUnknownOperator() public {
        vm.prank(owner);
        vm.expectRevert(STCoordinator.UnknownOperator.selector);
        settlement.registerOperatorPool(3, V2_POOL1, V2_BURN_LIMIT, FIRST_EPOCH);
    }

    // S-3: firstEpoch must be in the future.
    function test_registerOperatorPoolRefusesTheCurrentEpoch() public {
        vm.roll(_end(0));
        vm.prank(owner);
        vm.expectRevert(ISTSettlement2.InvalidEpoch.selector);
        settlement.registerOperatorPool(NO1, V2_POOL1, V2_BURN_LIMIT, 1);
    }

    // S-3
    function test_registerOperatorPoolRefusesAPastEpoch() public {
        vm.roll(_end(0));
        vm.prank(owner);
        vm.expectRevert(ISTSettlement2.InvalidEpoch.selector);
        settlement.registerOperatorPool(NO1, V2_POOL1, V2_BURN_LIMIT, 0);
    }

    // S-3: epochBlocks >= rootCommitWindowBlocks + 14,400 at firstEpoch.
    function test_registerOperatorPoolRefusesACadenceTooShort() public {
        _schedulePolicy(3, V2_ROOT_WINDOW + 14_400 - 1);
        vm.prank(owner);
        vm.expectRevert(ISTSettlement2.CadenceTooShort.selector);
        settlement.registerOperatorPool(NO1, V2_POOL1, V2_BURN_LIMIT, 3);
    }

    // S-3
    function test_registerOperatorPoolAcceptsTheShortestCadence() public {
        _schedulePolicy(3, V2_ROOT_WINDOW + 14_400);
        vm.prank(owner);
        settlement.registerOperatorPool(NO1, V2_POOL1, V2_BURN_LIMIT, 3);
        assertEq(vault2.pools(NO1).firstEpoch, 3);
    }

    // S-3, S5: the pause gates only close, commit and finalize.
    function test_registerOperatorPoolIgnoresThePause() public {
        _pause();
        _registerPool1();
        assertTrue(vault2.pools(NO1).registered);
    }

    // S-4
    function test_settlementAtGivesV1IdentitiesForAnUnregisteredPool() public view {
        (address vault_, bytes32 hotkey, uint8 version) = settlement.settlementAt(NO2, FIRST_EPOCH);
        assertEq(vault_, address(vault));
        assertEq(hotkey, POOL2);
        assertEq(version, 1);
    }

    // S-4
    function test_settlementAtSwitchesToV2AtFirstEpoch() public {
        _registerPool1();
        (address vault_, bytes32 hotkey, uint8 version) = settlement.settlementAt(NO1, FIRST_EPOCH - 1);
        assertEq(vault_, address(vault));
        assertEq(hotkey, POOL1);
        assertEq(version, 1);

        (vault_, hotkey, version) = settlement.settlementAt(NO1, FIRST_EPOCH);
        assertEq(vault_, address(vault2));
        assertEq(hotkey, V2_POOL1);
        assertEq(version, 2);

        (vault_, hotkey, version) = settlement.settlementAt(NO1, FIRST_EPOCH + 1);
        assertEq(vault_, address(vault2));
        assertEq(hotkey, V2_POOL1);
        assertEq(version, 2);
    }

    // S-4: an unknown v1 operator is refused by the v1 coordinator.
    function test_settlementAtRefusesAnUnknownOperator() public {
        vm.expectRevert(STCoordinator.UnknownOperator.selector);
        settlement.settlementAt(3, FIRST_EPOCH);
    }

    // S-5: anyone closes; vault2 captures the epoch.
    function test_closeCapturesTheEpoch() public {
        _registerPool1();
        vault2.setCaptureAmount(777);
        vm.roll(_end(FIRST_EPOCH));
        vm.prank(stranger);
        assertEq(settlement.closeOperatorEpoch(FIRST_EPOCH, NO1), 777);
        assertEq(vault2.callCount(), 2);
        _assertVaultCall(1, 0, abi.encodeCall(ISTSettlementVault2.captureEmission, (FIRST_EPOCH, NO1)));
    }

    // S-5: the window opens at end(e).
    function test_closeRefusesTheBlockBeforeTheEpochEnd() public {
        _registerPool1();
        vm.roll(_end(FIRST_EPOCH) - 1);
        vm.expectRevert(ISTSettlement2.InvalidWindow.selector);
        settlement.closeOperatorEpoch(FIRST_EPOCH, NO1);
    }

    // S-5: v2 closes inside the 1,200-block commit window, not v1's 120-block grace.
    function test_closeAcceptsTheLastBlockOfTheCommitWindow() public {
        _registerPool1();
        vm.roll(_end(FIRST_EPOCH) + V2_ROOT_WINDOW);
        settlement.closeOperatorEpoch(FIRST_EPOCH, NO1);
        assertEq(vault2.lastFundedEpoch(NO1), FIRST_EPOCH);
    }

    // S-5
    function test_closeRefusesAfterTheCommitWindow() public {
        _registerPool1();
        vm.roll(_end(FIRST_EPOCH) + V2_ROOT_WINDOW + 1);
        vm.expectRevert(ISTSettlement2.InvalidWindow.selector);
        settlement.closeOperatorEpoch(FIRST_EPOCH, NO1);
    }

    // S-5: earlier unfunded epochs are deferred in order, then e is captured.
    function test_closeDefersEveryEarlierUnfundedEpoch() public {
        _registerPool1();
        vm.roll(_end(FIRST_EPOCH + 2));
        settlement.closeOperatorEpoch(FIRST_EPOCH + 2, NO1);
        assertEq(vault2.callCount(), 4);
        _assertVaultCall(1, 0, abi.encodeCall(ISTSettlementVault2.deferEmission, (FIRST_EPOCH, NO1)));
        _assertVaultCall(2, 0, abi.encodeCall(ISTSettlementVault2.deferEmission, (FIRST_EPOCH + 1, NO1)));
        _assertVaultCall(3, 0, abi.encodeCall(ISTSettlementVault2.captureEmission, (FIRST_EPOCH + 2, NO1)));
    }

    // S-5
    function test_closeRefusesAnEpochBeforeFirstEpoch() public {
        _registerPool1();
        vm.roll(_end(FIRST_EPOCH - 1));
        vm.expectRevert(ISTSettlement2.InvalidEpoch.selector);
        settlement.closeOperatorEpoch(FIRST_EPOCH - 1, NO1);
    }

    // S-5
    function test_closeRefusesAnUnregisteredPool() public {
        vm.roll(_end(FIRST_EPOCH));
        vm.expectRevert(ISTSettlement2.UnknownPool.selector);
        settlement.closeOperatorEpoch(FIRST_EPOCH, NO2);
    }

    // S-5: a deactivated operator's emission is still captured.
    function test_closeCapturesAnInactiveOperatorsEpoch() public {
        _registerPool1();
        vm.prank(owner);
        coordinator.scheduleOperator(NO1, DEPOSIT1, depositSigner1, rootSigner1, false, FIRST_EPOCH);
        assertFalse(coordinator.operatorAt(NO1, FIRST_EPOCH).active);
        vm.roll(_end(FIRST_EPOCH));
        settlement.closeOperatorEpoch(FIRST_EPOCH, NO1);
        assertEq(vault2.lastFundedEpoch(NO1), FIRST_EPOCH);
    }

    // S-5, S5
    function test_closeIsRefusedWhileV1IsPaused() public {
        _registerPool1();
        _pause();
        vm.roll(_end(FIRST_EPOCH));
        vm.expectRevert(ISTSettlement2.SettlementPaused.selector);
        settlement.closeOperatorEpoch(FIRST_EPOCH, NO1);
    }

    // S-5: a funded epoch cannot be closed again; vault2's refusal propagates.
    function test_closeOfAFundedEpochIsRefusedByVault2() public {
        _registerPool1();
        vm.roll(_end(FIRST_EPOCH));
        settlement.closeOperatorEpoch(FIRST_EPOCH, NO1);
        vm.expectRevert(ISTSettlementVault2.InvalidTransition.selector);
        settlement.closeOperatorEpoch(FIRST_EPOCH, NO1);
    }

    // S-6, S1
    function test_commitByTheEpochRootSigner() public {
        _registerPool1();
        ISTSettlementVault2.Commitment memory commitment = _commitment(ROOT);
        vm.roll(_end(FIRST_EPOCH));
        vm.prank(rootSigner1);
        assertEq(settlement.commitDistribution(FIRST_EPOCH, NO1, 4, commitment), 1);
        assertEq(vault2.callCount(), 2);
        _assertVaultCall(
            1, 0, abi.encodeCall(ISTSettlementVault2.commitPending, (FIRST_EPOCH, NO1, 4, commitment))
        );
    }

    // S-6
    function test_commitRefusesAnyoneButTheRootSigner() public {
        _registerPool1();
        vm.roll(_end(FIRST_EPOCH));
        address[3] memory others = [rootSigner2, depositSigner1, owner];
        for (uint256 i; i < others.length; i++) {
            vm.prank(others[i]);
            vm.expectRevert(ISTSettlement2.Unauthorized.selector);
            settlement.commitDistribution(FIRST_EPOCH, NO1, 0, _commitment(ROOT));
        }
    }

    // S-6: the signer is the epoch's operator version, active or not, not the
    // version current when the commit lands.
    function test_commitUsesTheRootSignerOfTheEpochsVersion() public {
        _registerPool1();
        address nextSigner = makeAddr("root-signer-1-next");
        vm.prank(owner);
        coordinator.scheduleOperator(NO1, DEPOSIT1, depositSigner1, nextSigner, false, FIRST_EPOCH + 1);

        vm.roll(_end(FIRST_EPOCH));
        assertEq(coordinator.currentEpoch(), FIRST_EPOCH + 1);
        vm.prank(nextSigner);
        vm.expectRevert(ISTSettlement2.Unauthorized.selector);
        settlement.commitDistribution(FIRST_EPOCH, NO1, 0, _commitment(ROOT));
        vm.prank(rootSigner1);
        settlement.commitDistribution(FIRST_EPOCH, NO1, 0, _commitment(ROOT));

        vm.roll(_end(FIRST_EPOCH + 1));
        vm.prank(rootSigner1);
        vm.expectRevert(ISTSettlement2.Unauthorized.selector);
        settlement.commitDistribution(FIRST_EPOCH + 1, NO1, 0, _commitment(ROOT));
        vm.prank(nextSigner);
        assertEq(settlement.commitDistribution(FIRST_EPOCH + 1, NO1, 0, _commitment(ROOT)), 2);
    }

    // S-6
    function test_commitRefusesTheBlockBeforeTheEpochEnd() public {
        _registerPool1();
        vm.roll(_end(FIRST_EPOCH) - 1);
        vm.prank(rootSigner1);
        vm.expectRevert(ISTSettlement2.InvalidWindow.selector);
        settlement.commitDistribution(FIRST_EPOCH, NO1, 0, _commitment(ROOT));
    }

    // S-6
    function test_commitAcceptsTheLastBlockOfTheCommitWindow() public {
        _registerPool1();
        vm.roll(_end(FIRST_EPOCH) + V2_ROOT_WINDOW);
        vm.prank(rootSigner1);
        assertEq(settlement.commitDistribution(FIRST_EPOCH, NO1, 0, _commitment(ROOT)), 1);
    }

    // S-6
    function test_commitRefusesAfterTheCommitWindow() public {
        _registerPool1();
        vm.roll(_end(FIRST_EPOCH) + V2_ROOT_WINDOW + 1);
        vm.prank(rootSigner1);
        vm.expectRevert(ISTSettlement2.InvalidWindow.selector);
        settlement.commitDistribution(FIRST_EPOCH, NO1, 0, _commitment(ROOT));
    }

    // S-6
    function test_commitRefusesAnEpochBeforeFirstEpoch() public {
        _registerPool1();
        vm.roll(_end(FIRST_EPOCH - 1));
        vm.prank(rootSigner1);
        vm.expectRevert(ISTSettlement2.InvalidEpoch.selector);
        settlement.commitDistribution(FIRST_EPOCH - 1, NO1, 0, _commitment(ROOT));
    }

    // S-6
    function test_commitRefusesAnUnregisteredPool() public {
        vm.roll(_end(FIRST_EPOCH));
        vm.prank(rootSigner2);
        vm.expectRevert(ISTSettlement2.UnknownPool.selector);
        settlement.commitDistribution(FIRST_EPOCH, NO2, 0, _commitment(ROOT));
    }

    // S-6: a pending root is live until end(pending.epoch + 1).
    function test_commitRefusesWhileThePendingRootIsLive() public {
        _registerPool1();
        _commit(FIRST_EPOCH, ROOT);
        vm.roll(_end(FIRST_EPOCH) + 1);
        vm.prank(rootSigner1);
        vm.expectRevert(ISTSettlement2.PendingRootLive.selector);
        settlement.commitDistribution(FIRST_EPOCH, NO1, 0, _commitment(keccak256("second")));
    }

    // S-6, S3: at end(pending.epoch + 1) the next epoch's root replaces it.
    function test_commitReplacesThePendingRootAtTheNextEpochEnd() public {
        _registerPool1();
        _commit(FIRST_EPOCH, ROOT);
        bytes32 next = keccak256("next-root");
        assertEq(_commit(FIRST_EPOCH + 1, next), 2);
        assertEq(block.number, _end(FIRST_EPOCH + 1));
        _assertVaultCall(
            2,
            0,
            abi.encodeCall(ISTSettlementVault2.commitPending, (FIRST_EPOCH + 1, NO1, 0, _commitment(next)))
        );
        ISTSettlementVault2.Pending memory pending = vault2.pending(NO1);
        assertEq(pending.seq, 2);
        assertEq(pending.root, next);
    }

    // S-6, S3: even an approved root with an open demand is replaced.
    function test_commitReplacesAnApprovedRootWithAnOpenDemand() public {
        _registerPool1();
        uint64 seq = _commit(FIRST_EPOCH, ROOT);
        _approve(auditor1, seq, ROOT);
        assertEq(settlement.approvalCount(NO1), 1);
        ISTSettlementVault2.Pending memory pending = vault2.pending(NO1);
        pending.openDemands = 1;
        vault2.setPending(NO1, pending);

        assertEq(_commit(FIRST_EPOCH + 1, keccak256("next-root")), seq + 1);
        assertEq(settlement.approvalCount(NO1), 0);
        assertFalse(settlement.hasApproved(NO1, auditor1));
    }

    // S-6, S5
    function test_commitIsRefusedWhileV1IsPaused() public {
        _registerPool1();
        _pause();
        vm.roll(_end(FIRST_EPOCH));
        vm.prank(rootSigner1);
        vm.expectRevert(ISTSettlement2.SettlementPaused.selector);
        settlement.commitDistribution(FIRST_EPOCH, NO1, 0, _commitment(ROOT));
    }
}
