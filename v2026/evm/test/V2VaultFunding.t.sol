// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {V2VaultBase} from "./utils/V2VaultBase.sol";
import {V2TreeHelper} from "./utils/V2TreeHelper.sol";
import {ISTSettlementVault2} from "../src/interfaces/ISTSettlementVault2.sol";
import {LedgerTree} from "../src/LedgerTree.sol";
import {IStaking, ISTAKING_ADDRESS} from "../src/interfaces/stakingV2.sol";
import {SafeCast} from "@openzeppelin/contracts/utils/math/SafeCast.sol";

/// @dev Capture, deferral, absorption, surplus and backing (VERSION2-FINAL.md
///      §15.3 V-4, V-5, V-6, V-18, V-19, V-22).
contract V2VaultFundingTest is V2VaultBase {
    bytes32 internal constant FOREIGN = keccak256("v1-escrow-hotkey");
    uint256 internal constant WHOLE_POSITION = type(uint64).max;

    function _accrue(uint256 amount) internal {
        staking.setStake(POOL1, SELF, staking.stakes(POOL1, SELF) + amount);
    }

    function _assertConserved() internal view {
        assertTrue(vault.conservationHolds(), "conservation");
        assertEq(vault.totalCaptured(), vault.totalPaid() + vault.escrowAccounted());
    }

    // ------------------------------------------------------------------
    // V-4: strict order
    // ------------------------------------------------------------------

    // V-4
    function test_V4_captureAndDeferFundEpochsStrictlyInOrder() public {
        _accrue(100);
        vault.captureEmission(FIRST_EPOCH, NO1);
        assertEq(vault.lastFundedEpoch(NO1), FIRST_EPOCH);
        vault.deferEmission(FIRST_EPOCH + 1, NO1);
        assertEq(vault.lastFundedEpoch(NO1), FIRST_EPOCH + 1);
        _accrue(50);
        vault.captureEmission(FIRST_EPOCH + 2, NO1);
        assertEq(vault.lastFundedEpoch(NO1), FIRST_EPOCH + 2);
        assertEq(vault.fundedThrough(NO1, FIRST_EPOCH), 100);
        assertEq(vault.fundedThrough(NO1, FIRST_EPOCH + 1), 100);
        assertEq(vault.fundedThrough(NO1, FIRST_EPOCH + 2), 150);
    }

    // V-4
    function test_V4_captureRefusesASkippedEpoch() public {
        vm.expectRevert(ISTSettlementVault2.InvalidTransition.selector);
        vault.captureEmission(FIRST_EPOCH + 1, NO1);
    }

    // V-4
    function test_V4_captureRefusesAnEpochBeforeTheFirst() public {
        vm.expectRevert(ISTSettlementVault2.InvalidTransition.selector);
        vault.captureEmission(FIRST_EPOCH - 1, NO1);
    }

    // V-4
    function test_V4_deferRefusesASkippedEpoch() public {
        vm.expectRevert(ISTSettlementVault2.InvalidTransition.selector);
        vault.deferEmission(FIRST_EPOCH + 1, NO1);
    }

    // V-4
    function test_V4_deferRefusesAnEpochBeforeTheFirst() public {
        vm.expectRevert(ISTSettlementVault2.InvalidTransition.selector);
        vault.deferEmission(FIRST_EPOCH - 1, NO1);
    }

    // V-4
    function test_V4_captureRefusesAnUnknownPool() public {
        vm.expectRevert(ISTSettlementVault2.UnknownPool.selector);
        vault.captureEmission(1, NO2);
    }

    // V-4
    function test_V4_deferRefusesAnUnknownPool() public {
        vm.expectRevert(ISTSettlementVault2.UnknownPool.selector);
        vault.deferEmission(1, NO2);
    }

    // V-4, V-EV
    function test_V4_deferSnapshotsTheCapturedTotal() public {
        _capture(NO1, 70);
        vm.expectEmit(address(vault));
        emit ISTSettlementVault2.EmissionDeferred(FIRST_EPOCH + 1, NO1, 70);
        vault.deferEmission(FIRST_EPOCH + 1, NO1);
        assertEq(vault.fundedThrough(NO1, FIRST_EPOCH + 1), 70);
        assertEq(staking.stakes(POOL1, SELF), 0);
    }

    // ------------------------------------------------------------------
    // V-5: capture deltas
    // ------------------------------------------------------------------

    // V-5
    function test_V5_zeroStakeFundsZeroWithoutARuntimeCall() public {
        vm.expectCall(ISTAKING_ADDRESS, abi.encodeWithSelector(IStaking.moveStake.selector), 0);
        vm.expectEmit(address(vault));
        emit ISTSettlementVault2.EmissionCaptured(FIRST_EPOCH, NO1, 0, 0, 0);
        assertEq(vault.captureEmission(FIRST_EPOCH, NO1), 0);
        assertEq(vault.lastFundedEpoch(NO1), FIRST_EPOCH);
        assertEq(vault.fundedThrough(NO1, FIRST_EPOCH), 0);
        assertEq(vault.totalCaptured(), 0);
    }

    // V-5
    function test_V5_captureMovesTheExactObservedStakeIntoEscrow() public {
        _accrue(1_000);
        vm.expectCall(
            ISTAKING_ADDRESS,
            abi.encodeCall(IStaking.moveStake, (POOL1, ESCROW, uint256(NETUID), uint256(NETUID), 1_000)),
            1
        );
        vm.expectEmit(address(vault));
        emit ISTSettlementVault2.EmissionCaptured(FIRST_EPOCH, NO1, 1_000, 1_000, 1_000);
        assertEq(vault.captureEmission(FIRST_EPOCH, NO1), 1_000);

        assertEq(staking.stakes(POOL1, SELF), 0);
        assertEq(staking.stakes(ESCROW, SELF), 1_000);
        assertEq(vault.liveEscrowStake(), 1_000);
        assertEq(vault.capturedTotal(NO1), 1_000);
        assertEq(vault.totalCaptured(), 1_000);
        assertEq(vault.escrowAccounted(), 1_000);
        assertEq(vault.fundedThrough(NO1, FIRST_EPOCH), 1_000);
        _assertConserved();
    }

    // V-5
    function test_V5_captureAccountsAnEscrowGainOneRaoShort() public {
        _accrue(1_000);
        staking.setMoveStakeShortfall(1);
        vm.expectEmit(address(vault));
        emit ISTSettlementVault2.EmissionCaptured(FIRST_EPOCH, NO1, 1_000, 999, 999);
        assertEq(vault.captureEmission(FIRST_EPOCH, NO1), 999);

        assertEq(staking.stakes(POOL1, SELF), 0);
        assertEq(vault.liveEscrowStake(), 999);
        assertEq(vault.capturedTotal(NO1), 999);
        assertEq(vault.totalCaptured(), 999);
        assertEq(vault.escrowAccounted(), 999);
        assertEq(vault.fundedThrough(NO1, FIRST_EPOCH), 999);
        _assertConserved();
    }

    // V-5
    function test_V5_captureRefusesAnEscrowGainTwoRaoShort() public {
        _accrue(1_000);
        staking.setMoveStakeShortfall(2);
        vm.expectRevert(ISTSettlementVault2.RuntimeAccountingMismatch.selector);
        vault.captureEmission(FIRST_EPOCH, NO1);
    }

    // V-5
    function test_V5_captureRefusesAnEscrowGainAboveTheObservedStake() public {
        _accrue(1_000);
        _creditDuringNextStakeCall(ESCROW, SELF, 1);
        vm.expectRevert(ISTSettlementVault2.RuntimeAccountingMismatch.selector);
        vault.captureEmission(FIRST_EPOCH, NO1);
    }

    // V-5
    function test_V5_captureRefusesAPoolThatLosesLessThanTheObservedStake() public {
        _accrue(1_000);
        staking.setMoveStakeSourceResidue(1);
        vm.expectRevert(ISTSettlementVault2.RuntimeAccountingMismatch.selector);
        vault.captureEmission(FIRST_EPOCH, NO1);
    }

    // V-5
    function test_V5_captureRevertsOnARuntimeRefusal() public {
        _accrue(1_000);
        staking.setFailMoveStake(true);
        vm.expectRevert(bytes("mock: moveStake down"));
        vault.captureEmission(FIRST_EPOCH, NO1);
    }

    // V-5: dust below the runtime's transfer floor reverts; the epoch is left
    // for a later close, which defers it (§6.4).
    function test_V5_captureRevertsBelowTheRuntimeTransferFloor() public {
        _accrue(99);
        staking.setMinimumMoveAmount(100);
        vm.expectRevert(bytes("mock: move amount too low"));
        vault.captureEmission(FIRST_EPOCH, NO1);
        vault.deferEmission(FIRST_EPOCH, NO1);
        assertEq(staking.stakes(POOL1, SELF), 99);
    }

    // V-5
    function test_V5_captureRefusesTheWholePositionSentinel() public {
        _accrue(WHOLE_POSITION);
        vm.expectRevert(ISTSettlementVault2.AmountTooLarge.selector);
        vault.captureEmission(FIRST_EPOCH, NO1);
    }

    // V-5
    function test_V5_captureRefusesMoreThanTheWholePositionSentinel() public {
        _accrue(WHOLE_POSITION + 1);
        vm.expectRevert(ISTSettlementVault2.AmountTooLarge.selector);
        vault.captureEmission(FIRST_EPOCH, NO1);
    }

    // V-5
    function test_V5_captureAcceptsTheLargestAmountBelowTheSentinel() public {
        _accrue(WHOLE_POSITION - 1);
        assertEq(vault.captureEmission(FIRST_EPOCH, NO1), WHOLE_POSITION - 1);
        assertEq(vault.capturedTotal(NO1), WHOLE_POSITION - 1);
        _assertConserved();
    }

    // V-5 (A3): registration collateral locks part of the pool's position.
    // Moving the whole position would dig into it, which the runtime refuses;
    // capture moves exactly the movable stake and leaves the lock in place.
    function test_V5_captureMovesOnlyTheMovableStake() public {
        _accrue(1_000);
        staking.setCollateral(POOL1, SELF, 300);
        vm.prank(address(vault));
        vm.expectRevert(bytes("mock: StakeUnavailable"));
        staking.moveStake(POOL1, ESCROW, NETUID, NETUID, 1_000);

        vm.expectCall(
            ISTAKING_ADDRESS,
            abi.encodeCall(IStaking.moveStake, (POOL1, ESCROW, uint256(NETUID), uint256(NETUID), 700)),
            1
        );
        vm.expectEmit(address(vault));
        emit ISTSettlementVault2.EmissionCaptured(FIRST_EPOCH, NO1, 700, 700, 700);
        assertEq(vault.captureEmission(FIRST_EPOCH, NO1), 700);
        assertEq(staking.stakes(POOL1, SELF), 300);
        assertEq(vault.liveEscrowStake(), 700);
        _assertConserved();
    }

    // V-5 (A3): the delta checks compare whole positions, locks included.
    function test_V5_captureWithCollateralAcceptsAnEscrowGainOneRaoShort() public {
        _accrue(1_000);
        staking.setCollateral(POOL1, SELF, 300);
        staking.setMoveStakeShortfall(1);
        assertEq(vault.captureEmission(FIRST_EPOCH, NO1), 699);
        assertEq(staking.stakes(POOL1, SELF), 300);
        _assertConserved();
    }

    // V-5 (A3)
    function test_V5_captureWithCollateralRefusesAPoolThatLosesLess() public {
        _accrue(1_000);
        staking.setCollateral(POOL1, SELF, 300);
        staking.setMoveStakeSourceResidue(1);
        vm.expectRevert(ISTSettlementVault2.RuntimeAccountingMismatch.selector);
        vault.captureEmission(FIRST_EPOCH, NO1);
    }

    // V-5 (A3): a pool holding only its lock funds zero without a runtime call.
    function test_V5_aPoolHoldingOnlyCollateralFundsZero() public {
        _accrue(300);
        staking.setCollateral(POOL1, SELF, 300);
        vm.expectCall(ISTAKING_ADDRESS, abi.encodeWithSelector(IStaking.moveStake.selector), 0);
        vm.expectEmit(address(vault));
        emit ISTSettlementVault2.EmissionCaptured(FIRST_EPOCH, NO1, 0, 0, 0);
        assertEq(vault.captureEmission(FIRST_EPOCH, NO1), 0);
        assertEq(staking.stakes(POOL1, SELF), 300);
    }

    // V-5 (A3): a position read below its lock saturates at nothing movable.
    function test_V5_aLockAboveThePositionFundsZero() public {
        _accrue(100);
        staking.setCollateral(POOL1, SELF, 150);
        vm.expectCall(ISTAKING_ADDRESS, abi.encodeWithSelector(IStaking.moveStake.selector), 0);
        assertEq(vault.captureEmission(FIRST_EPOCH, NO1), 0);
    }

    // ------------------------------------------------------------------
    // V-6: funding snapshots
    // ------------------------------------------------------------------

    // V-6
    function test_V6_aFundedEpochIsNeverFundedAgain() public {
        uint256 epoch = _capture(NO1, 100);
        _accrue(50);
        vm.expectRevert(ISTSettlementVault2.InvalidTransition.selector);
        vault.captureEmission(epoch, NO1);
        vm.expectRevert(ISTSettlementVault2.InvalidTransition.selector);
        vault.deferEmission(epoch, NO1);
        assertEq(vault.fundedThrough(NO1, epoch), 100);
    }

    // V-6: each snapshot is written once, when its epoch is funded, never
    // changes afterwards, and the series never decreases.
    function testFuzz_V6_fundedThroughIsWrittenOnceAndMonotone(uint256 seed) public {
        uint256 steps = 1 + seed % 16;
        uint256[] memory snapshots = new uint256[](steps);
        staking.setStake(FOREIGN, SELF, 1_000_000);
        vault.absorbStake(FOREIGN);
        for (uint256 i; i < steps; i++) {
            uint256 draw = uint256(keccak256(abi.encode(seed, i)));
            if (draw % 7 == 0) vault.assignSurplus(NO1, 1 + draw % 1_000);
            uint256 epoch = draw % 3 == 0 ? _defer(NO1) : _capture(NO1, draw % 1_000_000);
            assertEq(epoch, FIRST_EPOCH + i);
            snapshots[i] = vault.fundedThrough(NO1, epoch);
            assertEq(snapshots[i], vault.capturedTotal(NO1));
            if (i != 0) assertGe(snapshots[i], snapshots[i - 1]);
            for (uint256 j; j <= i; j++) {
                assertEq(vault.fundedThrough(NO1, FIRST_EPOCH + j), snapshots[j]);
            }
        }
        _assertConserved();
    }

    // ------------------------------------------------------------------
    // V-18: absorbStake
    // ------------------------------------------------------------------

    // V-18
    function test_V18_absorbMovesVaultStakeFromAForeignHotkeyWithoutAccounting() public {
        _capture(NO1, 1_000);
        staking.setStake(FOREIGN, SELF, 500);

        vm.expectEmit(address(vault));
        emit ISTSettlementVault2.StakeAbsorbed(FOREIGN, 500, 500, stranger);
        vm.prank(stranger);
        assertEq(vault.absorbStake(FOREIGN), 500);

        assertEq(staking.stakes(FOREIGN, SELF), 0);
        assertEq(vault.liveEscrowStake(), 1_500);
        assertEq(vault.totalCaptured(), 1_000);
        assertEq(vault.totalPaid(), 0);
        assertEq(vault.escrowAccounted(), 1_000);
        assertEq(vault.capturedTotal(NO1), 1_000);
        assertEq(vault.claimedTotal(NO1), 0);
        assertEq(vault.fundedThrough(NO1, FIRST_EPOCH), 1_000);
        assertEq(vault.lastFundedEpoch(NO1), FIRST_EPOCH);
        _assertConserved();
    }

    // V-18
    function test_V18_absorbAcceptsAnEscrowGainOneRaoShort() public {
        staking.setStake(FOREIGN, SELF, 500);
        staking.setMoveStakeShortfall(1);
        vm.expectEmit(address(vault));
        emit ISTSettlementVault2.StakeAbsorbed(FOREIGN, 500, 499, address(this));
        assertEq(vault.absorbStake(FOREIGN), 499);
        assertEq(vault.liveEscrowStake(), 499);
    }

    // V-18
    function test_V18_absorbRefusesAnEscrowGainTwoRaoShort() public {
        staking.setStake(FOREIGN, SELF, 500);
        staking.setMoveStakeShortfall(2);
        vm.expectRevert(ISTSettlementVault2.RuntimeAccountingMismatch.selector);
        vault.absorbStake(FOREIGN);
    }

    // V-18
    function test_V18_absorbRefusesASourceThatLosesLessThanItsStake() public {
        staking.setStake(FOREIGN, SELF, 500);
        staking.setMoveStakeSourceResidue(1);
        vm.expectRevert(ISTSettlementVault2.RuntimeAccountingMismatch.selector);
        vault.absorbStake(FOREIGN);
    }

    // V-18
    function test_V18_absorbRevertsOnARuntimeRefusal() public {
        staking.setStake(FOREIGN, SELF, 500);
        staking.setFailMoveStake(true);
        vm.expectRevert(bytes("mock: moveStake down"));
        vault.absorbStake(FOREIGN);
    }

    // V-18
    function test_V18_absorbRefusesTheEscrowHotkey() public {
        _capture(NO1, 1_000);
        vm.expectRevert(ISTSettlementVault2.InvalidHotkey.selector);
        vault.absorbStake(ESCROW);
    }

    // V-18
    function test_V18_absorbRefusesAPoolHotkey() public {
        _accrue(1_000);
        vm.expectRevert(ISTSettlementVault2.InvalidHotkey.selector);
        vault.absorbStake(POOL1);
    }

    // V-18
    function test_V18_absorbRefusesAZeroHotkey() public {
        staking.setStake(bytes32(0), SELF, 500);
        vm.expectRevert(ISTSettlementVault2.InvalidHotkey.selector);
        vault.absorbStake(bytes32(0));
    }

    // V-18
    function test_V18_absorbRefusesZeroStake() public {
        staking.setStake(FOREIGN, keccak256("someone-else"), 500);
        vm.expectRevert(ISTSettlementVault2.NothingToAbsorb.selector);
        vault.absorbStake(FOREIGN);
    }

    // V-18 (A3)
    function test_V18_absorbMovesOnlyTheMovableStake() public {
        staking.setStake(FOREIGN, SELF, 500);
        staking.setCollateral(FOREIGN, SELF, 200);
        vm.expectEmit(address(vault));
        emit ISTSettlementVault2.StakeAbsorbed(FOREIGN, 300, 300, address(this));
        assertEq(vault.absorbStake(FOREIGN), 300);
        assertEq(staking.stakes(FOREIGN, SELF), 200);
        assertEq(vault.liveEscrowStake(), 300);
    }

    // V-18 (A3)
    function test_V18_absorbRefusesStakeThatIsAllCollateral() public {
        staking.setStake(FOREIGN, SELF, 200);
        staking.setCollateral(FOREIGN, SELF, 200);
        vm.expectRevert(ISTSettlementVault2.NothingToAbsorb.selector);
        vault.absorbStake(FOREIGN);
    }

    // V-18
    function test_V18_absorbRefusesTheWholePositionSentinel() public {
        staking.setStake(FOREIGN, SELF, WHOLE_POSITION);
        vm.expectRevert(ISTSettlementVault2.AmountTooLarge.selector);
        vault.absorbStake(FOREIGN);
    }

    // ------------------------------------------------------------------
    // V-19: assignSurplus
    // ------------------------------------------------------------------

    // V-19: surplus is attributed to the pool's next unfunded epoch and enters
    // that epoch's snapshot, never an earlier one.
    function test_V19_assignSurplusFundsThePoolsNextEpoch() public {
        uint256 epoch = _capture(NO1, 1_000);
        staking.setStake(FOREIGN, SELF, 300);
        vault.absorbStake(FOREIGN);

        vm.expectEmit(address(vault));
        emit ISTSettlementVault2.SurplusAssigned(NO1, epoch + 1, 300);
        vault.assignSurplus(NO1, 300);

        assertEq(vault.capturedTotal(NO1), 1_300);
        assertEq(vault.totalCaptured(), 1_300);
        assertEq(vault.escrowAccounted(), 1_300);
        assertEq(vault.fundedThrough(NO1, epoch), 1_000);
        _defer(NO1);
        assertEq(vault.fundedThrough(NO1, epoch + 1), 1_300);
        _assertConserved();
    }

    // V-19
    function test_V19_assignSurplusBeforeTheFirstEpochIsAttributedToIt() public {
        staking.setStake(FOREIGN, SELF, 300);
        vault.absorbStake(FOREIGN);
        vm.expectEmit(address(vault));
        emit ISTSettlementVault2.SurplusAssigned(NO1, FIRST_EPOCH, 120);
        vault.assignSurplus(NO1, 120);
        _capture(NO1, 80);
        assertEq(vault.fundedThrough(NO1, FIRST_EPOCH), 200);
        _assertConserved();
    }

    // V-19
    function test_V19_assignSurplusRefusesZero() public {
        staking.setStake(ESCROW, SELF, 300);
        vm.expectRevert(ISTSettlementVault2.InvalidSurplus.selector);
        vault.assignSurplus(NO1, 0);
    }

    // V-19
    function test_V19_assignSurplusRefusesMoreThanTheSurplus() public {
        _capture(NO1, 1_000);
        staking.setStake(ESCROW, SELF, 1_300);
        vm.expectRevert(ISTSettlementVault2.InvalidSurplus.selector);
        vault.assignSurplus(NO1, 301);
    }

    // V-19
    function test_V19_assignSurplusRefusesAnUnknownPool() public {
        staking.setStake(ESCROW, SELF, 300);
        vm.expectRevert(ISTSettlementVault2.UnknownPool.selector);
        vault.assignSurplus(NO2, 300);
    }

    // V-5, V-19: funding is counted in 128 bits, far above any real stake: a
    // capture moves less than 2^64. A pool total beyond them is refused, not
    // truncated, whether surplus or a capture would reach it.
    function test_V19_aPoolTotalBeyond128BitsIsRefused() public {
        uint256 limit = type(uint128).max;
        staking.setStake(ESCROW, SELF, limit + 1);
        vm.expectRevert(
            abi.encodeWithSelector(SafeCast.SafeCastOverflowedUintDowncast.selector, 128, limit + 1)
        );
        vault.assignSurplus(NO1, limit + 1);
        vault.assignSurplus(NO1, limit);
        assertEq(vault.capturedTotal(NO1), limit);
        assertEq(vault.totalCaptured(), limit);

        _accrue(1);
        vm.expectRevert(
            abi.encodeWithSelector(SafeCast.SafeCastOverflowedUintDowncast.selector, 128, limit + 1)
        );
        vault.captureEmission(FIRST_EPOCH, NO1);
    }

    // V-19: likewise the vault's total over its pools.
    function test_V19_aVaultTotalBeyond128BitsIsRefused() public {
        _registerPool2();
        uint256 half = uint256(1) << 127;
        staking.setStake(ESCROW, SELF, 2 * half);
        vault.assignSurplus(NO1, half);
        vm.expectRevert(
            abi.encodeWithSelector(SafeCast.SafeCastOverflowedUintDowncast.selector, 128, 2 * half)
        );
        vault.assignSurplus(NO2, half);
        vault.assignSurplus(NO2, half - 1);
        assertEq(vault.totalCaptured(), 2 * half - 1);
        assertEq(vault.escrowAccounted(), 2 * half - 1);
        _assertConserved();
    }

    // Registration locked collateral on the escrow position itself.
    function _lockEscrowCollateral(uint64 locked) internal {
        staking.setStake(ESCROW, SELF, staking.stakes(ESCROW, SELF) + locked);
        staking.setCollateral(ESCROW, SELF, locked);
    }

    // V-19 (A3): escrow collateral is never surplus.
    function test_V19_escrowCollateralIsNotSurplus() public {
        _lockEscrowCollateral(100);
        _capture(NO1, 1_000);
        assertEq(staking.stakes(ESCROW, SELF), 1_100);
        vm.expectRevert(ISTSettlementVault2.InvalidSurplus.selector);
        vault.assignSurplus(NO1, 1);

        staking.setStake(FOREIGN, SELF, 40);
        vault.absorbStake(FOREIGN);
        vm.expectRevert(ISTSettlementVault2.InvalidSurplus.selector);
        vault.assignSurplus(NO1, 41);
        vault.assignSurplus(NO1, 40);
        _assertConserved();
    }

    // ------------------------------------------------------------------
    // V-22: backing
    // ------------------------------------------------------------------

    // V-22 (A3): escrow collateral backs nothing. liveEscrowStake and the
    // backing check count only movable stake, so a shortfall of movable stake
    // is caught even while the whole position would cover the accounts.
    function test_V22_escrowCollateralIsNotBacking() public {
        _lockEscrowCollateral(100);
        assertEq(vault.liveEscrowStake(), 0);
        _capture(NO1, 1_000);
        assertEq(vault.liveEscrowStake(), 1_000);
        _assertConserved();

        staking.setStake(ESCROW, SELF, 1_050);
        assertFalse(vault.conservationHolds());
        vm.expectRevert(ISTSettlementVault2.Underfunded.selector);
        vault.captureEmission(FIRST_EPOCH + 1, NO1);
    }

    // Escrow stake falls `deficit` below what the vault accounts for.
    function _underfund(uint256 deficit) internal {
        staking.setStake(ESCROW, SELF, vault.escrowAccounted() - deficit);
        assertFalse(vault.conservationHolds());
    }

    // V-22
    function test_V22_aDeficitRefusesAZeroCapture() public {
        _capture(NO1, 1_000);
        _underfund(1);
        vm.expectRevert(ISTSettlementVault2.Underfunded.selector);
        vault.captureEmission(FIRST_EPOCH + 1, NO1);
    }

    // V-22
    function test_V22_aDeficitRefusesACapture() public {
        _capture(NO1, 1_000);
        _underfund(1);
        _accrue(500);
        vm.expectRevert(ISTSettlementVault2.Underfunded.selector);
        vault.captureEmission(FIRST_EPOCH + 1, NO1);
    }

    // V-22
    function test_V22_aDeficitRefusesAClaim() public {
        V2TreeHelper.Node[][] memory levels = _publish(NO1, _keys(ALICE, BOB), _values(600, 400));
        _underfund(1);
        vm.expectRevert(ISTSettlementVault2.Underfunded.selector);
        _claim(NO1, 0, levels, 0, ALICE, 600);
    }

    // V-22: absorbing vault-owned stake restores backing only when it covers
    // the whole deficit.
    function test_V22_aDeficitRefusesAnAbsorbThatDoesNotCoverIt() public {
        _capture(NO1, 1_000);
        _underfund(100);
        staking.setStake(FOREIGN, SELF, 99);
        vm.expectRevert(ISTSettlementVault2.Underfunded.selector);
        vault.absorbStake(FOREIGN);

        staking.setStake(FOREIGN, SELF, 100);
        vault.absorbStake(FOREIGN);
        _assertConserved();
    }

    // V-22: there is no surplus to assign while escrow is short.
    function test_V22_aDeficitLeavesNoSurplus() public {
        _capture(NO1, 1_000);
        _underfund(1);
        vm.expectRevert(ISTSettlementVault2.InvalidSurplus.selector);
        vault.assignSurplus(NO1, 1);
    }

    // V-22: demands and path checks move no stake and keep working.
    function test_V22_aDeficitNeverFreezesDemandsOrChallenges() public {
        V2TreeHelper.Node[][] memory finalized = _publish(NO1, _keys(ALICE, BOB), _values(600, 400));
        // The pending root keeps Alice whole but lowers Bob.
        V2TreeHelper.Node[][] memory next =
            V2TreeHelper.build(_keys(ALICE, BOB, CAROL), _values(700, 300, 200));
        uint64 seq = _propose(NO1, next, 3);
        _underfund(1);

        LedgerTree.Account memory alice = _account(0, ALICE, 600);
        LedgerTree.ProofNode[] memory aliceProof = V2TreeHelper.proof(finalized, 0);
        vm.deal(stranger, DEMAND_FEE);
        vm.prank(stranger);
        vault.demandPath{value: DEMAND_FEE}(NO1, 0, alice, aliceProof);
        vault.checkPath(NO1, 0, alice, aliceProof, V2TreeHelper.path(next, 0));
        assertEq(uint8(vault.demanded(NO1, seq, 0, 0)), uint8(ISTSettlementVault2.DemandState.Answered));

        LedgerTree.Fault fault = vault.checkPath(
            NO1, 0, _account(1, BOB, 400), V2TreeHelper.proof(finalized, 1), V2TreeHelper.path(next, 1)
        );
        assertEq(uint8(fault), uint8(LedgerTree.Fault.LowerValue));
        assertEq(vault.pending(NO1).seq, 0);
    }
}
