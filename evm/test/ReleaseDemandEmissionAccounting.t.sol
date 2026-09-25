// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {ReleaseBase} from "./utils/ReleaseBase.sol";
import {STSettlementVault} from "../src/STSettlementVault.sol";

/// @dev Demand contributions and captured miner emission are distinct custody ledgers.
contract ReleaseDemandEmissionAccountingTest is ReleaseBase {
    function test_subminimumDemandKeepsReserveAndEmissionCarrySeparateAcrossEpochs() public {
        uint256 skippedDemand = 500;
        uint256 laterDemand = 900;
        uint256 priorEmission = 700;
        uint256 laterEmission = 600;
        uint256 otherOperatorEmission = 900;
        uint256 staged = skippedDemand + coordinator.RESERVE_ROUNDING_ALLOWANCE_RAO();
        staking.setMinimumMoveAmount(staged + 1);

        // The operator preflight avoids staging this amount. Even a previously
        // staged amount that reaches the runtime cannot create partial credit.
        _pushDeposit(NO1, skippedDemand);
        vm.prank(depositSigner1);
        vm.expectRevert(bytes("mock: move amount too low"));
        coordinator.deposit(NO1, skippedDemand, 0, uint64(block.number + 1));
        assertEq(staking.stakes(DEPOSIT1, COORD_COLDKEY), staged);
        assertEq(coordinator.nextDepositNonce(NO1), 0);
        assertEq(coordinator.epochDeposits(0, NO1), 0);
        assertEq(coordinator.cumulativeConviction(NO1), 0);
        assertEq(coordinator.campaignReserved(), 0);
        assertEq(sink.principal(), 0);
        assertEq(vault.totalCaptured(), 0);
        assertEq(vault.carry(NO1), 0);
        assertTrue(vault.conservationHolds());

        // Native emission can still exist with no demand deposit. A missing
        // payout root carries only the captured same-operator emission.
        _accrue(NO1, priorEmission);
        _accrue(NO2, otherOperatorEmission);
        assertEq(_close(0, NO1), priorEmission);
        assertEq(_close(0, NO2), otherOperatorEmission);
        (bytes32 otherRoot,) = _singleLeaf(PROVIDER2, 10_000);
        vm.prank(rootSigner2);
        coordinator.commitOperatorRoot(0, NO2, otherRoot, keccak256("other-artifact"));
        vm.roll(_end(0) + FINALIZE_OFFSET);
        coordinator.finalizeOperatorEpoch(0, NO1);
        coordinator.finalizeOperatorEpoch(0, NO2);
        assertEq(vault.carry(NO1), priorEmission);
        assertEq(vault.carry(NO2), 0);
        assertEq(vault.outstandingLiability(), priorEmission + otherOperatorEmission);
        assertEq(vault.pendingFunding(), 0);
        assertEq(sink.operatorPrincipal(NO1), 0);
        assertTrue(vault.conservationHolds());

        // A later exact deposit creates only its own epoch's demand. It does
        // not retrospectively credit skipped usage or enter the payout vault.
        _deposit(NO1, laterDemand, 0);
        assertEq(coordinator.epochDeposits(0, NO1), 0);
        assertEq(coordinator.epochDeposits(1, NO1), laterDemand);
        assertEq(coordinator.cumulativeConviction(NO1), laterDemand);
        assertEq(coordinator.campaignReserved(), laterDemand);
        assertEq(coordinator.nextDepositNonce(NO1), 1);
        assertEq(sink.operatorPrincipal(NO1), laterDemand);
        assertEq(staking.stakes(DEPOSIT1, COORD_COLDKEY), staged);
        assertEq(vault.carry(NO1), priorEmission);
        assertEq(vault.totalCaptured(), priorEmission + otherOperatorEmission);
        assertTrue(vault.conservationHolds());

        _accrue(NO1, laterEmission);
        assertEq(_close(1, NO1), laterEmission);
        (bytes32 root, bytes32[] memory proof) = _singleLeaf(PROVIDER1, 10_000);
        vm.prank(rootSigner1);
        coordinator.commitOperatorRoot(1, NO1, root, keccak256("later-artifact"));
        vm.roll(_end(1) + FINALIZE_OFFSET);
        coordinator.finalizeOperatorEpoch(1, NO1);
        STSettlementVault.Entitlement memory entitlement = vault.entitlement(1, NO1);
        assertEq(entitlement.funded, laterEmission);
        assertEq(entitlement.total, priorEmission + laterEmission);
        assertEq(vault.carry(NO1), 0);
        assertEq(vault.entitlement(0, NO2).total, otherOperatorEmission);
        assertEq(vault.claim(1, NO1, PROVIDER1, 10_000, proof), priorEmission + laterEmission);
        assertEq(staking.stakes(ESCROW_HOTKEY, PROVIDER1), priorEmission + laterEmission);
        assertEq(vault.totalPaid(), priorEmission + laterEmission);
        assertEq(vault.escrowAccounted(), otherOperatorEmission);
        assertEq(vault.outstandingLiability(), otherOperatorEmission);
        assertEq(sink.operatorPrincipal(NO1), laterDemand);
        assertTrue(vault.conservationHolds());
    }
}
