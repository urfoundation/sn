// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {ReleaseBase} from "./utils/ReleaseBase.sol";
import {STCoordinator} from "../src/STCoordinator.sol";
import {STReserveSink} from "../src/STReserveSink.sol";

contract ReleaseDepositsTest is ReleaseBase {
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
        assertEq(staking.stakes(DEPOSIT1, COORD_COLDKEY), p.epochDepositCapRao + 1);
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
