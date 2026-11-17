// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {ReleaseBase} from "./utils/ReleaseBase.sol";
import {STSettlementVault} from "../src/STSettlementVault.sol";

/// @dev Carry follows finalized transaction order even when application epochs finalize late.
contract ReleaseCarryChronologyTest is ReleaseBase {
    function test_lateRootConsumesNewerMissedEpochWithoutRelabellingItsFunding() public {
        _close(0, NO1);
        (bytes32 root, bytes32[] memory proof) = _singleLeaf(PROVIDER1, 10_000);
        vm.prank(rootSigner1);
        coordinator.commitOperatorRoot(0, NO1, root, keccak256("original-delayed-root"));

        _accrue(NO1, 101);
        _accrue(NO2, 77);
        _close(1, NO1);
        _close(1, NO2);
        vm.roll(_end(1) + FINALIZE_OFFSET);
        coordinator.finalizeOperatorEpoch(1, NO1);
        coordinator.finalizeOperatorEpoch(1, NO2);
        assertEq(vault.carry(NO1), 101);
        assertEq(vault.carry(NO2), 77);

        // This permissionless call consumes carry that already exists. Its
        // original root was committed before that newer epoch earned anything.
        vm.prank(stranger);
        coordinator.finalizeOperatorEpoch(0, NO1);
        STSettlementVault.Entitlement memory target = vault.entitlement(0, NO1);
        STSettlementVault.Entitlement memory source = vault.entitlement(1, NO1);
        assertEq(target.funded, 0);
        assertEq(target.total, 101);
        assertEq(target.payoutRoot, root);
        assertEq(source.funded, 101);
        assertEq(source.total, 101);
        assertEq(uint256(source.status), uint256(STSettlementVault.EpochStatus.RootMissed));
        assertEq(vault.carry(NO1), 0);
        assertEq(vault.carry(NO2), 77);
        assertEq(vault.claim(0, NO1, PROVIDER1, 10_000, proof), 101);
        assertEq(vault.totalCaptured(), 178);
        assertEq(vault.totalPaid(), 101);
        assertEq(vault.outstandingLiability(), 77);
        assertTrue(vault.conservationHolds());
    }

    function test_lateRootConsumesNewerExpiredRemainderWithoutRepeatingAcceptedClaim() public {
        _close(0, NO1);
        (bytes32 delayedRoot, bytes32[] memory delayedProof) = _singleLeaf(PROVIDER2, 10_000);
        vm.prank(rootSigner1);
        coordinator.commitOperatorRoot(0, NO1, delayedRoot, keccak256("original-delayed-expiry-root"));

        _accrue(NO1, 1_001);
        _close(1, NO1);
        (bytes32 root, bytes32[] memory proof,) = _twoLeafTree();
        vm.prank(rootSigner1);
        coordinator.commitOperatorRoot(1, NO1, root, keccak256("original-newer-expiring-root"));
        vm.roll(_end(1) + FINALIZE_OFFSET);
        coordinator.finalizeOperatorEpoch(1, NO1);
        assertEq(vault.claim(1, NO1, PROVIDER1, 6_000, proof), 600);
        vm.roll(uint256(vault.entitlement(1, NO1).expiryBlock) + 1);
        vault.expireEntitlement(1, NO1);
        assertEq(vault.carry(NO1), 401);

        coordinator.finalizeOperatorEpoch(0, NO1);
        assertEq(vault.entitlement(0, NO1).funded, 0);
        assertEq(vault.entitlement(0, NO1).total, 401);
        assertEq(vault.entitlement(1, NO1).claimed, 600);
        assertEq(vault.claim(0, NO1, PROVIDER2, 10_000, delayedProof), 401);
        assertEq(vault.totalCaptured(), 1_001);
        assertEq(vault.totalPaid(), 1_001);
        assertEq(vault.outstandingLiability(), 0);
        assertEq(vault.carry(NO1), 0);
        assertTrue(vault.conservationHolds());
    }

    function test_earlierFinalizationCannotConsumeCarryCreatedAfterItsReceipt() public {
        _close(0, NO1);
        (bytes32 root, bytes32[] memory proof) = _singleLeaf(PROVIDER1, 10_000);
        vm.prank(rootSigner1);
        coordinator.commitOperatorRoot(0, NO1, root, keccak256("original-no-future-carry-root"));
        _accrue(NO1, 101);
        _close(1, NO1);
        vm.roll(_end(1) + FINALIZE_OFFSET);

        coordinator.finalizeOperatorEpoch(0, NO1);
        coordinator.finalizeOperatorEpoch(1, NO1);
        assertEq(vault.entitlement(0, NO1).total, 0);
        assertEq(vault.carry(NO1), 101);
        assertEq(vault.claim(0, NO1, PROVIDER1, 10_000, proof), 0);
        vm.expectRevert(STSettlementVault.InvalidTransition.selector);
        coordinator.finalizeOperatorEpoch(0, NO1);
        assertEq(vault.totalPaid(), 0);
        assertTrue(vault.conservationHolds());
    }
}
