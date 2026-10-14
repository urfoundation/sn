// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {V2StackBase} from "./utils/V2StackBase.sol";
import {V2LifecycleHandler, STSettlement2Surplus} from "./utils/V2LifecycleHandler.sol";
import {ISTSettlementVault2} from "../src/interfaces/ISTSettlementVault2.sol";

/// @dev The settlement v2 invariants (VERSION2-FINAL.md §15.5) over random
///      lifecycles of pools 1 and 2 through the whole stack, with the auditor
///      set at quorum 2 of 3 and STSettlement2 upgraded to expose
///      assignSurplus. Registration collateral is locked on the escrow and on
///      pool 2's hotkey (A3). One campaign checks all six invariants after
///      every call: Foundry would replay the identical campaign for each
///      invariant function. The default profile runs it short and the
///      v2_invariant profile deep, with metrics:
///        FOUNDRY_PROFILE=v2_invariant forge test --match-path test/V2Invariant.t.sol
///      Runs are deep in both, so that every run reaches finalizations and
///      claims (afterInvariant).
/// forge-config: default.invariant.runs = 2
/// forge-config: default.invariant.depth = 200
/// forge-config: v2_invariant.invariant.runs = 128
/// forge-config: v2_invariant.invariant.depth = 500
contract V2InvariantTest is V2StackBase {
    V2LifecycleHandler internal handler;

    function setUp() public override {
        super.setUp();
        _registerV2Pool(NO1, V2_POOL1, V2_POOL1_UID);
        _registerV2Pool(NO2, V2_POOL2, V2_POOL2_UID);
        address[] memory auditors = new address[](3);
        (auditors[0], auditors[1], auditors[2]) = (auditor1, makeAddr("auditor-2"), makeAddr("auditor-3"));
        STSettlement2Surplus upgrade = new STSettlement2Surplus();
        vm.startPrank(owner);
        settlement.setAuditors(auditors, 2);
        settlement.upgradeToAndCall(address(upgrade), "");
        vm.stopPrank();
        staking.setStake(V2_ESCROW, V2_SELF, 500);
        staking.setCollateral(V2_ESCROW, V2_SELF, 500);
        staking.setStake(V2_POOL2, V2_SELF, 1_000);
        staking.setCollateral(V2_POOL2, V2_SELF, 1_000);

        handler = new V2LifecycleHandler(STSettlement2Surplus(payable(address(settlement))));
        bytes4[] memory selectors = new bytes4[](14);
        selectors[0] = V2LifecycleHandler.close.selector;
        selectors[1] = V2LifecycleHandler.commit.selector;
        selectors[2] = V2LifecycleHandler.approve.selector;
        selectors[3] = V2LifecycleHandler.demand.selector;
        selectors[4] = V2LifecycleHandler.answer.selector;
        selectors[5] = V2LifecycleHandler.challenge.selector;
        selectors[6] = V2LifecycleHandler.finalize.selector;
        selectors[7] = V2LifecycleHandler.finalizeNow.selector;
        selectors[8] = V2LifecycleHandler.claim.selector;
        selectors[9] = V2LifecycleHandler.claimAny.selector;
        selectors[10] = V2LifecycleHandler.absorb.selector;
        selectors[11] = V2LifecycleHandler.assignSurplus.selector;
        selectors[12] = V2LifecycleHandler.runtime.selector;
        selectors[13] = V2LifecycleHandler.wait.selector;
        targetContract(address(handler));
        targetSelector(FuzzSelector({addr: address(handler), selectors: selectors}));
    }

    // I-1, I-2, I-3, I-4, I-5, I-6
    function invariant_I1ToI6() public view {
        _assertI1Conservation();
        _assertI2ClaimedWithinFinalizedWithinCaptured();
        _assertI3ClaimedWithinTheLargestFinalizedCumulative();
        _assertI4RootsFinalizeOnlyAfterTheWindowWithoutDemandOrFault();
        _assertI5FundingSnapshotsAreMonotoneAndImmutable();
        _assertI6ThePoolCapNeverRefusesAClaimOnHonestRoots();
    }

    // §15.5: a run that never finalizes or claims proves nothing.
    function afterInvariant() public view {
        assertGe(handler.finalizations(), 1, "no root finalized");
        assertGe(handler.claims(), 1, "no claim paid");
    }

    // I-1, G1: totalCaptured == totalPaid + escrowAccounted; escrowAccounted
    // is the pools' captured less claimed; live escrow stake backs it. Each
    // pool's capturedTotal and claimedTotal equal the ghost sums of what
    // entered and left escrow, and the surplus is exactly what was absorbed
    // and not assigned.
    function _assertI1Conservation() internal view {
        uint256 captured;
        uint256 claimed;
        for (uint256 noId = NO1; noId <= NO2; noId++) {
            assertEq(vault2.capturedTotal(noId), handler.ghostCaptured(noId), "I-1: capturedTotal");
            assertEq(vault2.claimedTotal(noId), handler.ghostClaimed(noId), "I-1: claimedTotal");
            captured += handler.ghostCaptured(noId);
            claimed += handler.ghostClaimed(noId);
        }
        assertEq(vault2.totalCaptured(), captured, "I-1: totalCaptured");
        assertEq(vault2.totalPaid(), claimed, "I-1: totalPaid");
        assertEq(vault2.escrowAccounted(), captured - claimed, "I-1: escrowAccounted");
        assertEq(vault2.totalCaptured(), vault2.totalPaid() + vault2.escrowAccounted(), "I-1: identity");
        assertGe(vault2.liveEscrowStake(), vault2.escrowAccounted(), "I-1: backing");
        assertEq(vault2.liveEscrowStake() - vault2.escrowAccounted(), handler.ghostSurplus(), "I-1: surplus");
        assertTrue(vault2.conservationHolds(), "I-1: conservationHolds");
    }

    // I-2, G1
    function _assertI2ClaimedWithinFinalizedWithinCaptured() internal view {
        for (uint256 noId = NO1; noId <= NO2; noId++) {
            assertLe(vault2.claimedTotal(noId), vault2.finalizedTotal(noId), "I-2: claimed above finalized");
            assertLe(vault2.finalizedTotal(noId), vault2.capturedTotal(noId), "I-2: finalized above captured");
        }
    }

    // I-3, G4: no account index, whatever coldkey a root gives it, has been
    // paid beyond the largest cumulative a finalized root can prove for it.
    // Loops assert only on a mismatch, to keep each check cheap.
    function _assertI3ClaimedWithinTheLargestFinalizedCumulative() internal view {
        for (uint256 noId = NO1; noId <= NO2; noId++) {
            uint256[] memory maxima = handler.maxCumulativeOf(noId);
            for (uint256 index; index < maxima.length; index++) {
                uint256 claimed = vault2.claimed(noId, index);
                if (claimed > maxima[index]) assertLe(claimed, maxima[index], "I-3");
            }
        }
    }

    // I-4, G6: every root finalized at least 14,400 blocks after its commit,
    // with no demand open and no fault shown, and vault2 appended exactly that
    // commit; the pending root's open demands are the ones nobody answered.
    function _assertI4RootsFinalizeOnlyAfterTheWindowWithoutDemandOrFault() internal view {
        for (uint256 noId = NO1; noId <= NO2; noId++) {
            V2LifecycleHandler.Finalization[] memory records = handler.finalizationsOf(noId);
            assertEq(records.length, vault2.rootCount(noId), "I-4: finalizations");
            for (uint256 k; k < records.length; k++) {
                V2LifecycleHandler.Finalization memory f = records[k];
                if (f.finalizedBlock < f.committedBlock + WINDOW) {
                    assertGe(f.finalizedBlock, f.committedBlock + WINDOW, "I-4: inside the window");
                }
                if (f.openDemands != 0) assertEq(f.openDemands, 0, "I-4: with an open demand");
                if (f.faultShown) assertFalse(f.faultShown, "I-4: after a fault was shown");
                if (!f.asCommitted) assertTrue(f.asCommitted, "I-4: not the commit");
            }
            ISTSettlementVault2.Pending memory next = vault2.pending(noId);
            assertEq(next.openDemands, handler.openDemands(noId, next.seq), "I-4: open demands");
        }
    }

    // I-5, G5: each epoch's funding snapshot is what was captured through it
    // when it was funded, never changes, never decreases from one epoch to
    // the next, and is not written ahead of funding.
    function _assertI5FundingSnapshotsAreMonotoneAndImmutable() internal view {
        for (uint256 noId = NO1; noId <= NO2; noId++) {
            uint256 last = vault2.lastFundedEpoch(noId);
            uint256[] memory ghost = handler.ghostFundedOf(noId, FIRST_EPOCH, last);
            uint256 previous;
            for (uint256 i; i < ghost.length; i++) {
                uint256 funded = vault2.fundedThrough(noId, FIRST_EPOCH + i);
                if (funded != ghost[i]) assertEq(funded, ghost[i], "I-5: snapshot changed");
                if (funded < previous) assertGe(funded, previous, "I-5: snapshot decreased");
                previous = funded;
            }
            assertEq(vault2.fundedThrough(noId, last + 1), 0, "I-5: written ahead");
        }
    }

    // I-6: pool 1 only ever finalizes honest roots, and no claim on a pool
    // whose finalized roots are all honest is refused PoolCapExceeded.
    function _assertI6ThePoolCapNeverRefusesAClaimOnHonestRoots() internal view {
        assertFalse(handler.tainted(NO1), "I-6: pool 1 is honest");
        for (uint256 noId = NO1; noId <= NO2; noId++) {
            assertEq(handler.honestCapRefusals(noId), 0, "I-6");
        }
    }
}
