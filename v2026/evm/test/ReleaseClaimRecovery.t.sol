// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {ReleaseBase} from "./utils/ReleaseBase.sol";
import {STSettlementVault} from "../src/STSettlementVault.sol";

/// @dev A rejected runtime payment must not return an accepted provider claim to operator carry.
contract ReleaseClaimRecoveryTest is ReleaseBase {
    event ClaimPaymentDeferred(
        bytes32 indexed coldkey,
        uint256 creditAlphaRao,
        uint256 taoEquivalentRao,
        uint64 minimumTransferTaoRao,
        STSettlementVault.PaymentDeferralReason reason
    );

    /// @dev Includes an unrelated missed root and a fractional claim with one rao of entitlement dust.
    function _finalizeRecoveryClaim() internal returns (bytes32[] memory proof) {
        _accrue(NO1, 1_001);
        _accrue(NO2, 900);
        _close(0, NO1);
        _close(0, NO2);
        (bytes32 root, bytes32[] memory providerProof,) = _twoLeafTree();
        vm.prank(rootSigner1);
        coordinator.commitOperatorRoot(0, NO1, root, keccak256("recovery-artifact"));
        vm.roll(_end(0) + FINALIZE_OFFSET);
        coordinator.finalizeOperatorEpoch(0, NO1);
        coordinator.finalizeOperatorEpoch(0, NO2);
        return providerProof;
    }

    /// @dev A successful precompile with a short destination delta must roll back only payment.
    function test_destinationMismatchKeepsAcceptedClaimPastExpiry() public {
        bytes32[] memory proof = _finalizeRecoveryClaim();
        uint64 expiry = vault.entitlement(0, NO1).expiryBlock;
        vm.roll(expiry);
        staking.setTransferStakeShortfall(1);

        vm.expectEmit(true, false, false, true, address(vault));
        emit ClaimPaymentDeferred(
            PROVIDER1,
            600,
            600,
            MINIMUM_TRANSFER_TAO_RAO,
            STSettlementVault.PaymentDeferralReason.RuntimeFailure
        );
        assertEq(vault.claim(0, NO1, PROVIDER1, 6_000, proof), 600);
        assertEq(vault.entitlement(0, NO1).claimed, 600);
        assertEq(vault.claimCredit(PROVIDER1), 600);
        assertEq(vault.totalPaid(), 0);
        assertEq(staking.stakes(ESCROW_HOTKEY, VAULT_COLDKEY), 1_901);
        assertEq(staking.stakes(ESCROW_HOTKEY, PROVIDER1), 0);
        assertTrue(vault.conservationHolds());
        vm.expectRevert(STSettlementVault.AlreadyClaimed.selector);
        vault.claim(0, NO1, PROVIDER1, 6_000, proof);

        vm.roll(uint256(expiry) + 1);
        vault.expireEntitlement(0, NO1);
        assertEq(vault.carry(NO1), 401);
        assertEq(vault.carry(NO2), 900);
        assertEq(vault.claimCredit(PROVIDER1), 600);
        staking.setTransferStakeShortfall(0);
        vm.prank(stranger);
        assertEq(vault.withdrawClaimCredit(PROVIDER1), 600);
        assertEq(staking.stakes(ESCROW_HOTKEY, PROVIDER1), 600);
        assertEq(staking.stakes(ESCROW_HOTKEY, VAULT_COLDKEY), 1_301);
        assertEq(vault.claimCredit(PROVIDER1), 0);
        assertEq(vault.totalPaid(), 600);
        assertEq(vault.outstandingLiability(), 1_301);
        assertEq(vault.escrowAccounted(), 1_301);
        assertEq(vault.pendingFunding(), 0);
        assertEq(sink.principal(), 0);
        assertTrue(vault.conservationHolds());
    }

    /// @dev A source residue cannot mint provider value or consume a second claim on retry.
    function test_sourceMismatchPreservesCreditAndExactRetry() public {
        bytes32[] memory proof = _finalizeRecoveryClaim();
        staking.setTransferStakeSourceResidue(1);

        assertEq(vault.claim(0, NO1, PROVIDER1, 6_000, proof), 600);
        assertEq(vault.claimCredit(PROVIDER1), 600);
        assertEq(vault.entitlement(0, NO1).claimed, 600);
        assertEq(vault.totalPaid(), 0);
        assertEq(staking.stakes(ESCROW_HOTKEY, VAULT_COLDKEY), 1_901);
        assertEq(staking.stakes(ESCROW_HOTKEY, PROVIDER1), 0);
        vm.expectRevert(STSettlementVault.RuntimeAccountingMismatch.selector);
        vault.withdrawClaimCredit(PROVIDER1);
        assertEq(vault.claimCredit(PROVIDER1), 600);
        assertTrue(vault.conservationHolds());

        staking.setTransferStakeSourceResidue(0);
        assertEq(vault.withdrawClaimCredit(PROVIDER1), 600);
        assertEq(staking.stakes(ESCROW_HOTKEY, PROVIDER1), 600);
        assertEq(vault.claimCredit(PROVIDER1), 0);
        assertEq(vault.entitlement(0, NO1).claimed, 600);
        assertEq(vault.carry(NO2), 900);
        assertTrue(vault.conservationHolds());
    }

    /// @dev Neither governance nor a fabricated self caller can enter the unlocked payment frame.
    function test_paymentFrameRefusesEveryExternalAuthority() public {
        bytes32[] memory proof = _finalizeRecoveryClaim();
        staking.setFailTransferStake(true);
        vault.claim(0, NO1, PROVIDER1, 6_000, proof);
        address[4] memory callers = [stranger, owner, address(coordinator), address(vault)];
        for (uint256 i; i < callers.length; i++) {
            vm.prank(callers[i]);
            vm.expectRevert(STSettlementVault.Unauthorized.selector);
            vault.transferClaimCredit(PROVIDER1);
        }
        assertEq(vault.claimCredit(PROVIDER1), 600);
        assertEq(vault.totalPaid(), 0);
        assertEq(staking.stakes(ESCROW_HOTKEY, PROVIDER1), 0);
        assertTrue(vault.conservationHolds());
    }

    /// @dev A runtime callback sees the active guard but cannot acquire the vault's self authority.
    function test_hostileRuntimeCannotReenterPaymentFrame() public {
        bytes32[] memory proof = _finalizeRecoveryClaim();
        staking.setReentry(address(vault), abi.encodeCall(STSettlementVault.transferClaimCredit, (PROVIDER1)));
        assertEq(vault.claim(0, NO1, PROVIDER1, 6_000, proof), 600);
        assertFalse(staking.reentrySucceeded());
        assertEq(staking.reentryFailureSelector(), STSettlementVault.Unauthorized.selector);
        assertEq(vault.claimCredit(PROVIDER1), 0);
        assertEq(vault.totalPaid(), 600);
        assertEq(staking.stakes(ESCROW_HOTKEY, PROVIDER1), 600);
        assertTrue(vault.conservationHolds());
    }

    /// @dev Extra debits must restore both an existing recipient position and the shared escrow.
    function test_paymentMismatchPreservesExistingRecipientStake() public {
        bytes32[] memory proof = _finalizeRecoveryClaim();
        staking.setStake(ESCROW_HOTKEY, PROVIDER1, 77);
        staking.setTransferStakeDestinationExtraDebit(1);
        vault.claim(0, NO1, PROVIDER1, 6_000, proof);
        assertEq(vault.claimCredit(PROVIDER1), 600);
        assertEq(staking.stakes(ESCROW_HOTKEY, PROVIDER1), 77);
        assertEq(staking.stakes(ESCROW_HOTKEY, VAULT_COLDKEY), 1_901);

        staking.setTransferStakeDestinationExtraDebit(0);
        staking.setTransferStakeSourceExtraDebit(1);
        vm.expectRevert(STSettlementVault.RuntimeAccountingMismatch.selector);
        vault.withdrawClaimCredit(PROVIDER1);
        assertEq(vault.claimCredit(PROVIDER1), 600);
        assertEq(vault.totalPaid(), 0);
        assertEq(staking.stakes(ESCROW_HOTKEY, PROVIDER1), 77);
        assertEq(staking.stakes(ESCROW_HOTKEY, VAULT_COLDKEY), 1_901);

        staking.setTransferStakeSourceExtraDebit(0);
        assertEq(vault.withdrawClaimCredit(PROVIDER1), 600);
        assertEq(staking.stakes(ESCROW_HOTKEY, PROVIDER1), 677);
        assertTrue(vault.conservationHolds());
    }

    /// @dev A later operator claim cannot erase prior credit when its automatic combined payment fails.
    function test_failedAggregatePaymentPreservesBothOperatorClaims() public {
        _accrue(NO1, 1_001);
        _accrue(NO2, 900);
        _close(0, NO1);
        _close(0, NO2);
        (bytes32 root, bytes32[] memory proof) = _singleLeaf(PROVIDER1, 10_000);
        vm.prank(rootSigner1);
        coordinator.commitOperatorRoot(0, NO1, root, keccak256("first-recovery-artifact"));
        vm.prank(rootSigner2);
        coordinator.commitOperatorRoot(0, NO2, root, keccak256("second-recovery-artifact"));
        vm.roll(_end(0) + FINALIZE_OFFSET);
        coordinator.finalizeOperatorEpoch(0, NO1);
        coordinator.finalizeOperatorEpoch(0, NO2);

        staking.setFailTransferStake(true);
        assertEq(vault.claim(0, NO1, PROVIDER1, 10_000, proof), 1_001);
        assertEq(vault.claimCredit(PROVIDER1), 1_001);
        staking.setFailTransferStake(false);
        staking.setTransferStakeShortfall(1);
        assertEq(vault.claim(0, NO2, PROVIDER1, 10_000, proof), 900);
        assertEq(vault.entitlement(0, NO1).claimed, 1_001);
        assertEq(vault.entitlement(0, NO2).claimed, 900);
        assertEq(vault.claimCredit(PROVIDER1), 1_901);
        assertEq(vault.totalPaid(), 0);
        assertEq(staking.stakes(ESCROW_HOTKEY, VAULT_COLDKEY), 1_901);
        assertEq(staking.stakes(ESCROW_HOTKEY, PROVIDER1), 0);

        staking.setTransferStakeShortfall(0);
        assertEq(vault.withdrawClaimCredit(PROVIDER1), 1_901);
        assertEq(vault.claimCredit(PROVIDER1), 0);
        assertEq(vault.outstandingLiability(), 0);
        assertEq(vault.escrowAccounted(), 0);
        assertEq(staking.stakes(ESCROW_HOTKEY, PROVIDER1), 1_901);
        assertEq(vault.totalPaid(), vault.totalCaptured());
        assertEq(sink.principal(), 0);
        assertTrue(vault.conservationHolds());
    }
}
