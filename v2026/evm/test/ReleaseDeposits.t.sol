// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {ReleaseBase} from "./utils/ReleaseBase.sol";
import {STCoordinator} from "../src/STCoordinator.sol";
import {STReserveSink} from "../src/STReserveSink.sol";

contract ReleaseDepositsTest is ReleaseBase {
    function setUp() public override {
        super.setUp();
        // Runtime 453 may floor each destination share issue by one rao. The
        // reserve path crosses both a move and a transfer share pool.
        staking.setMoveStakeShortfall(1);
        staking.setTransferStakeShortfall(1);
    }

    function test_atomicDepositCannotBeCreditedAcrossOperators() public {
        _pushDeposit(NO1, 500);

        vm.prank(depositSigner2);
        vm.expectRevert(STCoordinator.FundsNotReceived.selector);
        coordinator.deposit(NO2, 500, 0, uint64(block.number + 1));

        vm.prank(depositSigner1);
        coordinator.deposit(NO1, 500, 0, uint64(block.number + 1));

        assertEq(coordinator.epochDeposits(0, NO1), 500);
        assertEq(coordinator.epochDeposits(0, NO2), 0);
        assertEq(staking.stakes(DEPOSIT1, COORD_COLDKEY), 0);
        assertEq(staking.stakes(RESERVE_HOTKEY, SINK_COLDKEY), 500);
        assertEq(sink.principal(), 500);
        assertEq(sink.operatorPrincipal(NO1), 500);
    }

    function test_depositReplayDeadlineAndAuthorizationFailClosed() public {
        _deposit(NO1, 100, 0);

        _pushDeposit(NO1, 100);
        vm.prank(depositSigner1);
        vm.expectRevert(STCoordinator.InvalidNonce.selector);
        coordinator.deposit(NO1, 100, 0, uint64(block.number + 1));

        vm.prank(stranger);
        vm.expectRevert(STCoordinator.Unauthorized.selector);
        coordinator.deposit(NO1, 100, 1, uint64(block.number + 1));

        vm.prank(depositSigner1);
        vm.expectRevert(STCoordinator.DeadlineExpired.selector);
        coordinator.deposit(NO1, 100, 1, uint64(block.number - 1));
    }

    function test_epochEndDeadlineCannotCreditFollowingEpoch() public {
        uint256 end = coordinator.epochEndBlock(0);
        _pushDeposit(NO1, 100);
        vm.roll(end - 1);
        vm.prank(depositSigner1);
        // Safe because epochEndBlock is contract-bounded to uint64.
        // forge-lint: disable-next-line(unsafe-typecast)
        coordinator.deposit(NO1, 100, 0, uint64(end - 1));
        assertEq(coordinator.epochDeposits(0, NO1), 100);

        _pushDeposit(NO1, 100);
        vm.roll(end);
        vm.prank(depositSigner1);
        vm.expectRevert(STCoordinator.DeadlineExpired.selector);
        // Safe because epochEndBlock is contract-bounded to uint64.
        // forge-lint: disable-next-line(unsafe-typecast)
        coordinator.deposit(NO1, 100, 1, uint64(end - 1));

        assertEq(coordinator.currentEpoch(), 1);
        assertEq(coordinator.epochDeposits(1, NO1), 0);
        assertEq(coordinator.nextDepositNonce(NO1), 1);
    }

    function test_voluntaryConvictionIsNotCurrentEpochDemand() public {
        _pushDeposit(NO1, 250);
        vm.prank(depositSigner1);
        coordinator.addConviction(NO1, 250, 0, uint64(block.number + 1));

        assertEq(coordinator.epochDeposits(0, NO1), 0);
        assertEq(coordinator.epochConvictionAdded(0, NO1), 250);
        assertEq(coordinator.cumulativeConviction(NO1), 250);
        assertEq(sink.operatorPrincipal(NO1), 250);
    }

    function test_epochAndCampaignCapsAreEnforcedBeforeTransfer() public {
        STCoordinator.PolicySnapshot memory p = coordinator.policyAt(0);
        _pushDeposit(NO1, p.epochDepositCapRao + 1);
        vm.prank(depositSigner1);
        vm.expectRevert(STCoordinator.CapExceeded.selector);
        coordinator.deposit(NO1, p.epochDepositCapRao + 1, 0, uint64(block.number + 1));
        assertEq(sink.principal(), 0);
        assertEq(
            staking.stakes(DEPOSIT1, COORD_COLDKEY),
            p.epochDepositCapRao + 1 + coordinator.RESERVE_ROUNDING_ALLOWANCE_RAO()
        );
    }

    function test_runtime453TwoSharePoolFloorsStillLockExactPrincipal() public {
        uint256 amount = 50_000_000;
        _pushDeposit(NO1, amount);

        vm.prank(depositSigner1);
        coordinator.addConviction(NO1, amount, 0, uint64(block.number + 1));

        assertEq(staking.stakes(DEPOSIT1, COORD_COLDKEY), 0);
        assertEq(staking.stakes(RESERVE_HOTKEY, COORD_COLDKEY), 0);
        assertEq(staking.stakes(RESERVE_HOTKEY, SINK_COLDKEY), amount);
        assertEq(coordinator.campaignReserved(), amount);
        assertEq(coordinator.cumulativeConviction(NO1), amount);
        assertEq(sink.principal(), amount);
    }

    function test_exactRuntimeTransitionsDonateOnlyTheBoundedAllowance() public {
        uint256 amount = 500;
        staking.setMoveStakeShortfall(0);
        staking.setTransferStakeShortfall(0);
        _pushDeposit(NO1, amount);

        vm.prank(depositSigner1);
        coordinator.deposit(NO1, amount, 0, uint64(block.number + 1));

        assertEq(staking.stakes(DEPOSIT1, COORD_COLDKEY), 0);
        assertEq(staking.stakes(RESERVE_HOTKEY, COORD_COLDKEY), 0);
        assertEq(staking.stakes(RESERVE_HOTKEY, SINK_COLDKEY), amount + 2);
        assertEq(sink.principal(), amount);
    }

    function test_eitherSingleRuntimeFloorDonatesOnlyOneRao() public {
        uint256 amount = 500;
        staking.setMoveStakeShortfall(0);
        _pushDeposit(NO1, amount);

        vm.prank(depositSigner1);
        coordinator.deposit(NO1, amount, 0, uint64(block.number + 1));

        assertEq(staking.stakes(RESERVE_HOTKEY, SINK_COLDKEY), amount + 1);
        assertEq(sink.principal(), amount);

        staking.setMoveStakeShortfall(1);
        staking.setTransferStakeShortfall(0);
        _pushDeposit(NO2, amount);
        vm.prank(depositSigner2);
        coordinator.deposit(NO2, amount, 0, uint64(block.number + 1));

        assertEq(staking.stakes(RESERVE_HOTKEY, SINK_COLDKEY), 2 * amount + 2);
        assertEq(sink.principal(), 2 * amount);
    }

    function test_runtimeShortfallBeyondAllowanceRevertsAllState() public {
        uint256 amount = 500;
        staking.setMoveStakeShortfall(2);
        _pushDeposit(NO1, amount);

        vm.prank(depositSigner1);
        vm.expectRevert(STCoordinator.RuntimeAccountingMismatch.selector);
        coordinator.deposit(NO1, amount, 0, uint64(block.number + 1));

        assertEq(staking.stakes(DEPOSIT1, COORD_COLDKEY), amount + 2);
        assertEq(staking.stakes(RESERVE_HOTKEY, COORD_COLDKEY), 0);
        assertEq(staking.stakes(RESERVE_HOTKEY, SINK_COLDKEY), 0);
        assertEq(coordinator.nextDepositNonce(NO1), 0);
        assertEq(coordinator.campaignReserved(), 0);
        assertEq(sink.principal(), 0);
    }

    function test_runtimeTransferFloorBeyondOneRaoRevertsAllState() public {
        uint256 amount = 500;
        staking.setTransferStakeShortfall(2);
        _pushDeposit(NO1, amount);

        vm.prank(depositSigner1);
        vm.expectRevert(STCoordinator.RuntimeAccountingMismatch.selector);
        coordinator.deposit(NO1, amount, 0, uint64(block.number + 1));

        assertEq(staking.stakes(DEPOSIT1, COORD_COLDKEY), amount + 2);
        assertEq(staking.stakes(RESERVE_HOTKEY, COORD_COLDKEY), 0);
        assertEq(staking.stakes(RESERVE_HOTKEY, SINK_COLDKEY), 0);
        assertEq(coordinator.nextDepositNonce(NO1), 0);
        assertEq(coordinator.campaignReserved(), 0);
        assertEq(sink.principal(), 0);
    }

    function test_runtimeTransferOneRaoSourceResidueLocksExactPrincipal() public {
        uint256 amount = 500;
        // Observed runtime transition: staged amount + 2, moved amount + 1,
        // full moved stake at the sink, and one rao left on the coordinator.
        staking.setTransferStakeShortfall(0);
        staking.setTransferStakeSourceResidue(1);
        _pushDeposit(NO1, amount);

        vm.prank(depositSigner1);
        coordinator.deposit(NO1, amount, 0, uint64(block.number + 1));

        assertEq(staking.stakes(DEPOSIT1, COORD_COLDKEY), 0);
        assertEq(staking.stakes(RESERVE_HOTKEY, COORD_COLDKEY), 1);
        assertEq(staking.stakes(RESERVE_HOTKEY, SINK_COLDKEY), amount + 1);
        assertEq(coordinator.nextDepositNonce(NO1), 1);
        assertEq(coordinator.epochDeposits(0, NO1), amount);
        assertEq(coordinator.campaignReserved(), amount);
        assertEq(sink.principal(), amount);
        assertEq(sink.operatorPrincipal(NO1), amount);
    }

    function test_runtimeTransferSourceResidueBeyondOneRaoRevertsAllState() public {
        uint256 amount = 500;
        staking.setTransferStakeSourceResidue(2);
        _pushDeposit(NO1, amount);

        vm.prank(depositSigner1);
        vm.expectRevert(STCoordinator.RuntimeAccountingMismatch.selector);
        coordinator.deposit(NO1, amount, 0, uint64(block.number + 1));

        assertEq(staking.stakes(DEPOSIT1, COORD_COLDKEY), amount + 2);
        assertEq(staking.stakes(RESERVE_HOTKEY, COORD_COLDKEY), 0);
        assertEq(staking.stakes(RESERVE_HOTKEY, SINK_COLDKEY), 0);
        assertEq(coordinator.nextDepositNonce(NO1), 0);
        assertEq(coordinator.campaignReserved(), 0);
        assertEq(sink.principal(), 0);
    }

    function test_runtimeTransferCoordinatorReserveDecreaseRevertsAllState() public {
        uint256 amount = 500;
        staking.setStake(RESERVE_HOTKEY, COORD_COLDKEY, 7);
        staking.setTransferStakeSourceExtraDebit(1);
        _pushDeposit(NO1, amount);

        _assertReserveAccountingRevert(amount, 7, 0);
    }

    function test_runtimeTransferSinkReserveDecreaseRevertsAllState() public {
        uint256 amount = 500;
        staking.setStake(RESERVE_HOTKEY, SINK_COLDKEY, 7);
        staking.setTransferStakeDestinationExtraDebit(amount + 1);
        _pushDeposit(NO1, amount);

        _assertReserveAccountingRevert(amount, 0, 7);
    }

    function test_runtimeTransferOneRaoResidueCannotCreditUnderfundedPrincipal() public {
        uint256 amount = 500;
        staking.setTransferStakeSourceResidue(1);
        staking.setTransferStakeShortfall(2);
        _pushDeposit(NO1, amount);

        _assertReserveAccountingRevert(amount, 0, 0);
    }

    function test_runtimeTransferSinkAndResidueCannotExceedStagedAmount() public {
        uint256 amount = 500;
        staking.setMoveStakeShortfall(0);
        staking.setTransferStakeShortfall(0);
        staking.setTransferStakeSourceResidue(1);
        _pushDeposit(NO1, amount);

        _assertReserveAccountingRevert(amount, 0, 0);
    }

    function _assertReserveAccountingRevert(
        uint256 amount,
        uint256 coordinatorReserveBefore,
        uint256 sinkReserveBefore
    ) private {
        vm.prank(depositSigner1);
        vm.expectRevert(STCoordinator.RuntimeAccountingMismatch.selector);
        coordinator.deposit(NO1, amount, 0, uint64(block.number + 1));

        assertEq(staking.stakes(DEPOSIT1, COORD_COLDKEY), amount + 2);
        assertEq(staking.stakes(RESERVE_HOTKEY, COORD_COLDKEY), coordinatorReserveBefore);
        assertEq(staking.stakes(RESERVE_HOTKEY, SINK_COLDKEY), sinkReserveBefore);
        assertEq(coordinator.nextDepositNonce(NO1), 0);
        assertEq(coordinator.epochDeposits(0, NO1), 0);
        assertEq(coordinator.campaignReserved(), 0);
        assertEq(coordinator.cumulativeConviction(NO1), 0);
        assertEq(sink.principal(), 0);
        assertEq(sink.operatorPrincipal(NO1), 0);
    }

    function test_runtimeMinimumRejectsSubthresholdDepositAtomically() public {
        uint256 amount = 500;
        staking.setMinimumMoveAmount(amount + 3);
        _pushDeposit(NO1, amount);

        vm.prank(depositSigner1);
        vm.expectRevert(bytes("mock: move amount too low"));
        coordinator.deposit(NO1, amount, 0, uint64(block.number + 1));

        assertEq(staking.stakes(DEPOSIT1, COORD_COLDKEY), amount + 2);
        assertEq(coordinator.nextDepositNonce(NO1), 0);
        assertEq(coordinator.campaignReserved(), 0);
        assertEq(sink.principal(), 0);
    }

    function test_runtimeTransferMinimumRejectsSubthresholdReserveAtomically() public {
        uint256 amount = 500;
        staking.setMinimumTransferAmount(amount + 2);
        _pushDeposit(NO1, amount);

        vm.prank(depositSigner1);
        vm.expectRevert(bytes("mock: transfer amount too low"));
        coordinator.deposit(NO1, amount, 0, uint64(block.number + 1));

        assertEq(staking.stakes(DEPOSIT1, COORD_COLDKEY), amount + 2);
        assertEq(staking.stakes(RESERVE_HOTKEY, COORD_COLDKEY), 0);
        assertEq(staking.stakes(RESERVE_HOTKEY, SINK_COLDKEY), 0);
        assertEq(coordinator.nextDepositNonce(NO1), 0);
        assertEq(coordinator.campaignReserved(), 0);
        assertEq(sink.principal(), 0);
    }

    function test_sinkInitializationIsOneShotAndHasNoOutboundSelector() public {
        vm.expectRevert(STReserveSink.AlreadyInitialized.selector);
        sink.setRecorderOnce(stranger);

        (bool ok,) = address(sink)
            .call(
                abi.encodeWithSignature(
                    "transferStake(bytes32,bytes32,uint256,uint256,uint256)",
                    COLD1,
                    RESERVE_HOTKEY,
                    NETUID,
                    NETUID,
                    1
                )
            );
        assertFalse(ok);
    }
}
