// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {Test} from "forge-std/Test.sol";

import {ReleaseBase} from "./utils/ReleaseBase.sol";
import {STCoordinator} from "../src/STCoordinator.sol";
import {STSettlementVault} from "../src/STSettlementVault.sol";
import {STReserveSink} from "../src/STReserveSink.sol";
import {MockStakingV2} from "./mocks/PrecompileMocks.sol";

contract ReleaseInvariantHandler is Test {
    STCoordinator internal coordinator;
    STSettlementVault internal vault;
    STReserveSink internal sink;
    MockStakingV2 internal staking;
    address internal signer1;
    address internal signer2;
    bytes32 internal coordColdkey;
    bytes32 internal deposit1;
    bytes32 internal deposit2;

    uint256 public ghostReserved;

    constructor(
        STCoordinator coordinator_,
        STSettlementVault vault_,
        STReserveSink sink_,
        MockStakingV2 staking_,
        address signer1_,
        address signer2_,
        bytes32 coordColdkey_,
        bytes32 deposit1_,
        bytes32 deposit2_
    ) {
        coordinator = coordinator_;
        vault = vault_;
        sink = sink_;
        staking = staking_;
        signer1 = signer1_;
        signer2 = signer2_;
        coordColdkey = coordColdkey_;
        deposit1 = deposit1_;
        deposit2 = deposit2_;
    }

    function reserveForOperator1(uint96 rawAmount, bool convictionOnly) external {
        _reserve(1, signer1, deposit1, rawAmount, convictionOnly);
    }

    function reserveForOperator2(uint96 rawAmount, bool convictionOnly) external {
        _reserve(2, signer2, deposit2, rawAmount, convictionOnly);
    }

    function _reserve(
        uint256 noId,
        address signer,
        bytes32 depositHotkey,
        uint96 rawAmount,
        bool convictionOnly
    ) internal {
        uint256 amount = bound(uint256(rawAmount), 1, 10_000);
        STCoordinator.PolicySnapshot memory p = coordinator.policyAt(coordinator.currentEpoch());
        if (coordinator.campaignReserved() + amount > p.campaignDepositCapRao) return;
        if (
            !convictionOnly
                && coordinator.epochDeposits(coordinator.currentEpoch(), noId) + amount > p.epochDepositCapRao
        ) return;
        staking.setStake(
            depositHotkey,
            coordColdkey,
            staking.stakes(depositHotkey, coordColdkey) + amount
                + coordinator.RESERVE_ROUNDING_ALLOWANCE_RAO()
        );
        uint256 nonce = coordinator.nextDepositNonce(noId);
        vm.prank(signer);
        if (convictionOnly) {
            coordinator.addConviction(noId, amount, nonce, uint64(block.number + 1));
        } else {
            coordinator.deposit(noId, amount, nonce, uint64(block.number + 1));
        }
        ghostReserved += amount;
    }
}

contract ReleaseInvariantTest is ReleaseBase {
    ReleaseInvariantHandler internal handler;

    function setUp() public override {
        super.setUp();
        staking.setMoveStakeShortfall(1);
        handler = new ReleaseInvariantHandler(
            coordinator,
            vault,
            sink,
            staking,
            depositSigner1,
            depositSigner2,
            COORD_COLDKEY,
            DEPOSIT1,
            DEPOSIT2
        );
        targetContract(address(handler));
    }

    function invariant_reserveIsOneWayAndExactlyAccounted() public view {
        assertEq(sink.principal(), handler.ghostReserved());
        assertEq(sink.operatorPrincipal(NO1) + sink.operatorPrincipal(NO2), sink.principal());
        assertGe(staking.stakes(RESERVE_HOTKEY, SINK_COLDKEY), sink.principal());
        assertEq(staking.stakes(RESERVE_HOTKEY, COORD_COLDKEY), 0);
    }

    function invariant_vaultConservationAlwaysHolds() public view {
        assertTrue(vault.conservationHolds());
        assertEq(vault.totalCaptured(), vault.totalPaid() + vault.escrowAccounted());
        assertEq(vault.escrowAccounted(), vault.pendingFunding() + vault.outstandingLiability());
    }
}
