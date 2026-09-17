// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {ReleaseBase} from "./utils/ReleaseBase.sol";
import {STSettlementVault} from "../src/STSettlementVault.sol";

contract ReleaseSettlementMinimumsTest is ReleaseBase {
    uint16 private _standaloneNonce;

    function _standaloneVault(uint64 minimumTransferTaoRao)
        internal
        returns (STSettlementVault standalone, bytes32 escrow, bytes32 vaultColdkey, bytes32 pool)
    {
        _standaloneNonce++;
        escrow = keccak256(abi.encode("standalone-escrow", _standaloneNonce));
        vaultColdkey = keccak256(abi.encode("standalone-coldkey", _standaloneNonce));
        pool = keccak256(abi.encode("standalone-pool", _standaloneNonce));
        standalone =
            new STSettlementVault(NETUID, escrow, vaultColdkey, 1, minimumTransferTaoRao, address(this));
        staking.setColdkey(address(standalone), vaultColdkey);
        neuron.setUid(NETUID, escrow, 100 + _standaloneNonce * 2);
        standalone.registerEscrow(REGISTRATION_BURN_LIMIT);
        standalone.setCoordinatorOnce(address(this));
        neuron.setUid(NETUID, pool, 101 + _standaloneNonce * 2);
        standalone.registerPool(NO1, pool, REGISTRATION_BURN_LIMIT);
    }

    function _captureAndFinalize(
        STSettlementVault standalone,
        bytes32 pool,
        bytes32 vaultColdkey,
        uint256 epoch,
        uint256 amount,
        bytes32 provider,
        uint256 shareBps,
        uint64 expiryBlock
    ) internal returns (bytes32[] memory proof) {
        staking.setStake(pool, vaultColdkey, amount);
        standalone.captureEmission(epoch, NO1);
        bytes32 root = standalone.payoutLeaf(provider, shareBps);
        standalone.finalizeEntitlement(
            epoch, NO1, root, keccak256(abi.encode("artifact", epoch)), expiryBlock
        );
        return new bytes32[](0);
    }

    function test_captureDefersBelowMinimumAndCapturesCumulativeExactBoundary() public {
        (STSettlementVault standalone, bytes32 escrow, bytes32 vaultColdkey, bytes32 pool) =
            _standaloneVault(100);
        staking.setMinimumMoveAmount(100);

        staking.setStake(pool, vaultColdkey, 99);
        assertEq(standalone.captureEmission(0, NO1), 0);
        STSettlementVault.Entitlement memory dust = standalone.entitlement(0, NO1);
        assertEq(uint256(dust.status), uint256(STSettlementVault.EpochStatus.Funded));
        assertEq(dust.funded, 0);
        assertEq(staking.stakes(pool, vaultColdkey), 99);
        assertEq(staking.stakes(escrow, vaultColdkey), 0);

        staking.setStake(pool, vaultColdkey, 100);
        assertEq(standalone.captureEmission(1, NO1), 100);
        assertEq(staking.stakes(pool, vaultColdkey), 0);
        assertEq(staking.stakes(escrow, vaultColdkey), 100);

        staking.setStake(pool, vaultColdkey, 101);
        assertEq(standalone.captureEmission(2, NO1), 101);
        assertEq(standalone.totalCaptured(), 201);
        assertTrue(standalone.conservationHolds());
    }

    function test_captureUsesConservativeLivePriceFloorAtBoundary() public {
        (STSettlementVault standalone,, bytes32 vaultColdkey, bytes32 pool) = _standaloneVault(100);
        alpha.setAlphaPrice(NETUID, 0.5 ether);
        staking.setMinimumMoveAmount(200);

        staking.setStake(pool, vaultColdkey, 199);
        assertEq(standalone.captureEmission(0, NO1), 0);
        assertEq(staking.stakes(pool, vaultColdkey), 199);

        staking.setStake(pool, vaultColdkey, 200);
        assertEq(standalone.captureEmission(1, NO1), 200);
        assertEq(staking.stakes(pool, vaultColdkey), 0);
    }

    function test_capturePriceOutageLeavesEpochAndStakeRetryable() public {
        (STSettlementVault standalone,, bytes32 vaultColdkey, bytes32 pool) = _standaloneVault(100);
        staking.setStake(pool, vaultColdkey, 100);
        alpha.setFailPrice(true);

        vm.expectRevert(STSettlementVault.RuntimePriceUnavailable.selector);
        standalone.captureEmission(0, NO1);
        STSettlementVault.Entitlement memory record = standalone.entitlement(0, NO1);
        assertEq(uint256(record.status), uint256(STSettlementVault.EpochStatus.Unset));
        assertEq(staking.stakes(pool, vaultColdkey), 100);

        alpha.setFailPrice(false);
        assertEq(standalone.captureEmission(0, NO1), 100);
    }

    function test_captureRejectsDestinationAndSourceRuntimeDriftAtomically() public {
        (STSettlementVault standalone, bytes32 escrow, bytes32 vaultColdkey, bytes32 pool) =
            _standaloneVault(1);
        staking.setStake(pool, vaultColdkey, 100);
        staking.setMoveStakeShortfall(1);

        vm.expectRevert(STSettlementVault.RuntimeAccountingMismatch.selector);
        standalone.captureEmission(0, NO1);
        assertEq(staking.stakes(pool, vaultColdkey), 100);
        assertEq(staking.stakes(escrow, vaultColdkey), 0);
        assertEq(standalone.totalCaptured(), 0);

        staking.setMoveStakeShortfall(0);
        staking.setMoveStakeSourceResidue(1);
        vm.expectRevert(STSettlementVault.RuntimeAccountingMismatch.selector);
        standalone.captureEmission(0, NO1);
        assertEq(staking.stakes(pool, vaultColdkey), 100);
        assertEq(staking.stakes(escrow, vaultColdkey), 0);
        assertEq(standalone.totalCaptured(), 0);
    }

    function test_hostileRuntimeCannotReenterCaptureOrClaimSettlement() public {
        (STSettlementVault standalone, bytes32 escrow, bytes32 vaultColdkey, bytes32 pool) =
            _standaloneVault(100);
        staking.setMinimumMoveAmount(100);
        staking.setMinimumTransferAmount(100);
        staking.setStake(pool, vaultColdkey, 1_000);
        staking.setReentry(
            address(standalone),
            abi.encodeCall(STSettlementVault.claim, (0, NO1, PROVIDER1, 10_000, new bytes32[](0)))
        );

        assertEq(standalone.captureEmission(0, NO1), 1_000);
        assertFalse(staking.reentrySucceeded());
        assertEq(staking.reentryFailureSelector(), STSettlementVault.Reentrancy.selector);
        assertEq(standalone.totalCaptured(), 1_000);
        assertEq(standalone.escrowAccounted(), 1_000);

        standalone.finalizeEntitlement(
            0,
            NO1,
            standalone.payoutLeaf(PROVIDER1, 1_000),
            keccak256("reentrant-runtime-artifact"),
            uint64(block.number + 100)
        );
        staking.setReentry(
            address(standalone), abi.encodeCall(STSettlementVault.withdrawClaimCredit, (PROVIDER1))
        );
        standalone.claim(0, NO1, PROVIDER1, 1_000, new bytes32[](0));

        assertFalse(staking.reentrySucceeded());
        assertEq(staking.reentryFailureSelector(), STSettlementVault.Reentrancy.selector);
        assertEq(staking.stakes(escrow, PROVIDER1), 100);
        assertEq(standalone.claimCredit(PROVIDER1), 0);
        assertEq(standalone.totalPaid(), 100);
        assertEq(standalone.escrowAccounted(), 900);
        assertTrue(standalone.conservationHolds());
    }

    function test_subthresholdClaimCreditSurvivesEntitlementExpiry() public {
        (STSettlementVault standalone,, bytes32 vaultColdkey, bytes32 pool) = _standaloneVault(100);
        staking.setMinimumMoveAmount(100);
        staking.setMinimumTransferAmount(100);
        uint64 expiry = uint64(block.number + 1);
        bytes32[] memory proof =
            _captureAndFinalize(standalone, pool, vaultColdkey, 0, 1_000, PROVIDER1, 500, expiry);

        assertEq(standalone.claim(0, NO1, PROVIDER1, 500, proof), 50);
        assertEq(standalone.claimCredit(PROVIDER1), 50);
        assertEq(standalone.totalPaid(), 0);
        assertEq(standalone.outstandingLiability(), 1_000);

        vm.roll(uint256(expiry) + 1);
        standalone.expireEntitlement(0, NO1);
        assertEq(standalone.carry(NO1), 950);
        assertEq(standalone.claimCredit(PROVIDER1), 50);
        assertEq(standalone.outstandingLiability(), 1_000);
        assertTrue(standalone.conservationHolds());
    }

    function test_claimCreditsAggregateAcrossEpochsAndAutoPayAtExactMinimum() public {
        (STSettlementVault standalone, bytes32 escrow, bytes32 vaultColdkey, bytes32 pool) =
            _standaloneVault(100);
        staking.setMinimumMoveAmount(100);
        staking.setMinimumTransferAmount(100);
        bytes32[] memory proof0 = _captureAndFinalize(
            standalone, pool, vaultColdkey, 0, 1_000, PROVIDER1, 500, uint64(block.number + 100)
        );
        standalone.claim(0, NO1, PROVIDER1, 500, proof0);
        assertEq(standalone.claimCredit(PROVIDER1), 50);

        bytes32[] memory proof1 = _captureAndFinalize(
            standalone, pool, vaultColdkey, 1, 1_000, PROVIDER1, 500, uint64(block.number + 100)
        );
        standalone.claim(1, NO1, PROVIDER1, 500, proof1);
        assertEq(standalone.claimCredit(PROVIDER1), 0);
        assertEq(staking.stakes(escrow, PROVIDER1), 100);
        assertEq(standalone.totalPaid(), 100);
        assertEq(standalone.outstandingLiability(), 1_900);
        assertTrue(standalone.conservationHolds());
    }

    function test_claimPriceAndRuntimeOutagesPreserveAcceptedCreditForRetry() public {
        (STSettlementVault standalone, bytes32 escrow, bytes32 vaultColdkey, bytes32 pool) =
            _standaloneVault(100);
        staking.setMinimumMoveAmount(100);
        staking.setMinimumTransferAmount(100);
        bytes32[] memory proof0 = _captureAndFinalize(
            standalone, pool, vaultColdkey, 0, 1_000, PROVIDER1, 1_000, uint64(block.number + 100)
        );

        alpha.setFailPrice(true);
        standalone.claim(0, NO1, PROVIDER1, 1_000, proof0);
        assertEq(standalone.claimCredit(PROVIDER1), 100);
        assertEq(standalone.totalPaid(), 0);

        alpha.setFailPrice(false);
        staking.setFailTransferStake(true);
        vm.expectRevert(STSettlementVault.RuntimeTransferFailed.selector);
        standalone.withdrawClaimCredit(PROVIDER1);
        assertEq(standalone.claimCredit(PROVIDER1), 100);

        staking.setFailTransferStake(false);
        assertEq(standalone.withdrawClaimCredit(PROVIDER1), 100);
        assertEq(staking.stakes(escrow, PROVIDER1), 100);
        assertEq(standalone.claimCredit(PROVIDER1), 0);
        assertTrue(standalone.conservationHolds());
    }

    function test_claimRuntimeFailureAtAcceptanceDefersWithoutLosingLeaf() public {
        (STSettlementVault standalone, bytes32 escrow, bytes32 vaultColdkey, bytes32 pool) =
            _standaloneVault(100);
        staking.setMinimumMoveAmount(100);
        staking.setMinimumTransferAmount(100);
        bytes32[] memory proof = _captureAndFinalize(
            standalone, pool, vaultColdkey, 0, 1_000, PROVIDER1, 1_000, uint64(block.number + 100)
        );
        staking.setFailTransferStake(true);

        standalone.claim(0, NO1, PROVIDER1, 1_000, proof);
        assertEq(standalone.claimCredit(PROVIDER1), 100);
        assertEq(standalone.totalPaid(), 0);
        assertEq(staking.stakes(escrow, PROVIDER1), 0);

        staking.setFailTransferStake(false);
        standalone.withdrawClaimCredit(PROVIDER1);
        assertEq(staking.stakes(escrow, PROVIDER1), 100);
    }

    function test_claimRejectsDestinationAndSourceRuntimeDriftAtomically() public {
        (STSettlementVault standalone, bytes32 escrow, bytes32 vaultColdkey, bytes32 pool) =
            _standaloneVault(100);
        staking.setMinimumMoveAmount(100);
        staking.setMinimumTransferAmount(100);
        bytes32[] memory proof = _captureAndFinalize(
            standalone, pool, vaultColdkey, 0, 1_000, PROVIDER1, 1_000, uint64(block.number + 100)
        );
        staking.setTransferStakeShortfall(1);

        vm.expectRevert(STSettlementVault.RuntimeAccountingMismatch.selector);
        standalone.claim(0, NO1, PROVIDER1, 1_000, proof);
        STSettlementVault.Entitlement memory record = standalone.entitlement(0, NO1);
        assertEq(record.claimed, 0);
        assertEq(standalone.claimCredit(PROVIDER1), 0);
        assertEq(staking.stakes(escrow, vaultColdkey), 1_000);
        assertEq(staking.stakes(escrow, PROVIDER1), 0);

        staking.setTransferStakeShortfall(0);
        staking.setTransferStakeSourceResidue(1);
        vm.expectRevert(STSettlementVault.RuntimeAccountingMismatch.selector);
        standalone.claim(0, NO1, PROVIDER1, 1_000, proof);
        record = standalone.entitlement(0, NO1);
        assertEq(record.claimed, 0);
        assertEq(standalone.claimCredit(PROVIDER1), 0);
        assertEq(staking.stakes(escrow, vaultColdkey), 1_000);
    }

    function test_withdrawBelowMinimumAndVaultSelfClaimFailClosed() public {
        (STSettlementVault standalone,, bytes32 vaultColdkey, bytes32 pool) = _standaloneVault(100);
        staking.setMinimumMoveAmount(100);
        bytes32[] memory proof = _captureAndFinalize(
            standalone, pool, vaultColdkey, 0, 1_000, PROVIDER1, 500, uint64(block.number + 100)
        );
        standalone.claim(0, NO1, PROVIDER1, 500, proof);
        vm.expectRevert(STSettlementVault.TransferBelowMinimum.selector);
        standalone.withdrawClaimCredit(PROVIDER1);

        bytes32[] memory selfProof = _captureAndFinalize(
            standalone, pool, vaultColdkey, 1, 1_000, vaultColdkey, 10_000, uint64(block.number + 100)
        );
        vm.expectRevert(STSettlementVault.InvalidConfiguration.selector);
        standalone.claim(1, NO1, vaultColdkey, 10_000, selfProof);
    }
}
