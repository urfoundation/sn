// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {V2VaultBase} from "./utils/V2VaultBase.sol";
import {V2TreeHelper} from "./utils/V2TreeHelper.sol";
import {ISTSettlementVault2} from "../src/interfaces/ISTSettlementVault2.sol";
import {LedgerTree} from "../src/LedgerTree.sol";
import {IStaking, ISTAKING_ADDRESS} from "../src/interfaces/stakingV2.sol";

/// @dev Calls claim for someone else; it holds no coldkey of its own.
contract V2ClaimRelayer {
    function relay(
        ISTSettlementVault2 target,
        uint256 noId,
        uint256 rootIndex,
        LedgerTree.Account calldata account,
        LedgerTree.ProofNode[] calldata proof
    ) external returns (uint256) {
        return target.claim(noId, rootIndex, account, proof);
    }
}

/// @dev Claims (VERSION2-FINAL.md §15.3 V-12 to V-17, V-21). Root 0 of pool
///      NO1 holds Alice 600 and Bob 400.
contract V2VaultClaimsTest is V2VaultBase {
    uint256 internal constant WHOLE_POSITION = type(uint64).max;

    function _root0() internal returns (V2TreeHelper.Node[][] memory) {
        return _publish(NO1, _keys(ALICE, BOB), _values(600, 400));
    }

    function _assertConserved() internal view {
        assertTrue(vault.conservationHolds(), "conservation");
        assertEq(vault.totalCaptured(), vault.totalPaid() + vault.escrowAccounted());
        assertLe(vault.claimedTotal(NO1), vault.finalizedTotal(NO1));
        assertLe(vault.finalizedTotal(NO1), vault.capturedTotal(NO1));
    }

    // ------------------------------------------------------------------
    // V-12: claim against the latest root
    // ------------------------------------------------------------------

    // V-12, V-EV
    function test_V12_aClaimPaysExactlyTheAccruedAmount() public {
        V2TreeHelper.Node[][] memory levels = _root0();
        LedgerTree.Account memory alice = _account(0, ALICE, 600);
        LedgerTree.ProofNode[] memory proof = V2TreeHelper.proof(levels, 0);
        vm.expectCall(
            ISTAKING_ADDRESS,
            abi.encodeCall(IStaking.transferStake, (ALICE, ESCROW, uint256(NETUID), uint256(NETUID), 600)),
            1
        );
        vm.expectEmit(address(vault));
        emit ISTSettlementVault2.Claimed(NO1, 0, 0, ALICE, 600, 600, address(this));
        assertEq(vault.claim(NO1, 0, alice, proof), 600);

        assertEq(staking.stakes(ESCROW, ALICE), 600);
        assertEq(vault.liveEscrowStake(), 400);
        assertEq(vault.claimed(NO1, 0), 600);
        assertEq(vault.claimed(NO1, 1), 0);
        assertEq(vault.claimedTotal(NO1), 600);
        assertEq(vault.totalPaid(), 600);
        assertEq(vault.escrowAccounted(), 400);
        _assertConserved();
    }

    // V-12
    function test_V12_aDestinationOneRaoShortIsAccepted() public {
        V2TreeHelper.Node[][] memory levels = _root0();
        staking.setTransferStakeShortfall(1);
        assertEq(_claim(NO1, 0, levels, 0, ALICE, 600), 600);
        assertEq(staking.stakes(ESCROW, ALICE), 599);
        assertEq(vault.claimed(NO1, 0), 600);
        assertEq(vault.liveEscrowStake(), 400);
        _assertConserved();
    }

    // V-12
    function test_V12_aDestinationTwoRaoShortReverts() public {
        V2TreeHelper.Node[][] memory levels = _root0();
        staking.setTransferStakeShortfall(2);
        LedgerTree.Account memory alice = _account(0, ALICE, 600);
        LedgerTree.ProofNode[] memory proof = V2TreeHelper.proof(levels, 0);
        vm.expectRevert(ISTSettlementVault2.RuntimeAccountingMismatch.selector);
        vault.claim(NO1, 0, alice, proof);
    }

    // V-12
    function test_V12_aDestinationGainAboveTheAmountReverts() public {
        V2TreeHelper.Node[][] memory levels = _root0();
        _creditDuringNextStakeCall(ESCROW, ALICE, 1);
        LedgerTree.Account memory alice = _account(0, ALICE, 600);
        LedgerTree.ProofNode[] memory proof = V2TreeHelper.proof(levels, 0);
        vm.expectRevert(ISTSettlementVault2.RuntimeAccountingMismatch.selector);
        vault.claim(NO1, 0, alice, proof);
    }

    // V-12
    function test_V12_anEscrowLossBelowTheAmountReverts() public {
        V2TreeHelper.Node[][] memory levels = _root0();
        staking.setTransferStakeSourceResidue(1);
        LedgerTree.Account memory alice = _account(0, ALICE, 600);
        LedgerTree.ProofNode[] memory proof = V2TreeHelper.proof(levels, 0);
        vm.expectRevert(ISTSettlementVault2.RuntimeAccountingMismatch.selector);
        vault.claim(NO1, 0, alice, proof);
    }

    // V-12
    function test_V12_anEscrowLossAboveTheAmountReverts() public {
        V2TreeHelper.Node[][] memory levels = _root0();
        staking.setTransferStakeSourceExtraDebit(1);
        LedgerTree.Account memory alice = _account(0, ALICE, 600);
        LedgerTree.ProofNode[] memory proof = V2TreeHelper.proof(levels, 0);
        vm.expectRevert(ISTSettlementVault2.RuntimeAccountingMismatch.selector);
        vault.claim(NO1, 0, alice, proof);
    }

    // V-12: a claim below the runtime's transfer floor reverts and stays
    // claimable.
    function test_V12_aFloorRefusalRevertsAndLeavesTheAccountClaimable() public {
        V2TreeHelper.Node[][] memory levels = _root0();
        staking.setMinimumTransferAmount(601);
        LedgerTree.Account memory alice = _account(0, ALICE, 600);
        LedgerTree.ProofNode[] memory proof = V2TreeHelper.proof(levels, 0);
        vm.expectRevert(bytes("mock: transfer amount too low"));
        vault.claim(NO1, 0, alice, proof);
        assertEq(vault.claimed(NO1, 0), 0);
        assertEq(vault.claimedTotal(NO1), 0);

        staking.setMinimumTransferAmount(600);
        assertEq(vault.claim(NO1, 0, alice, proof), 600);
    }

    // V-12: a runtime refusal such as TooManyStakingHotkeys reverts the claim,
    // which stays claimable.
    function test_V12_aStakingHotkeyCapRefusalRevertsAndLeavesTheAccountClaimable() public {
        V2TreeHelper.Node[][] memory levels = _root0();
        staking.setFailTransferStake(true);
        LedgerTree.Account memory alice = _account(0, ALICE, 600);
        LedgerTree.ProofNode[] memory proof = V2TreeHelper.proof(levels, 0);
        vm.expectRevert(bytes("mock: transferStake down"));
        vault.claim(NO1, 0, alice, proof);
        assertEq(vault.claimed(NO1, 0), 0);
        assertEq(vault.totalPaid(), 0);

        staking.setFailTransferStake(false);
        assertEq(vault.claim(NO1, 0, alice, proof), 600);
        _assertConserved();
    }

    // V-12
    function test_V12_aDepthZeroRootIsClaimedWithAnEmptyProof() public {
        V2TreeHelper.Node[][] memory levels = _publish(NO1, _keys(ALICE), _values(1_000));
        LedgerTree.ProofNode[] memory proof = V2TreeHelper.proof(levels, 0);
        assertEq(proof.length, 0);
        assertEq(vault.claim(NO1, 0, _account(0, ALICE, 1_000), proof), 1_000);
        assertEq(staking.stakes(ESCROW, ALICE), 1_000);
        assertEq(vault.escrowAccounted(), 0);
        _assertConserved();
    }

    // V-12 (A3): the escrow's own registration collateral stays locked and
    // outside every account; claims pay out exactly the accounted stake.
    function test_V12_claimsLeaveEscrowCollateralInPlace() public {
        staking.setStake(ESCROW, SELF, 100);
        staking.setCollateral(ESCROW, SELF, 100);
        V2TreeHelper.Node[][] memory levels = _root0();
        assertEq(vault.liveEscrowStake(), 1_000);
        assertEq(_claim(NO1, 0, levels, 0, ALICE, 600), 600);
        assertEq(_claim(NO1, 0, levels, 1, BOB, 400), 400);
        assertEq(staking.stakes(ESCROW, SELF), 100);
        assertEq(vault.liveEscrowStake(), 0);
        assertEq(vault.escrowAccounted(), 0);
        _assertConserved();
    }

    // ------------------------------------------------------------------
    // V-13, V-14: cumulative claims across roots
    // ------------------------------------------------------------------

    // V-13
    function test_V13_aRepeatClaimOnTheSameRootIsRefused() public {
        V2TreeHelper.Node[][] memory levels = _root0();
        _claim(NO1, 0, levels, 0, ALICE, 600);
        LedgerTree.Account memory alice = _account(0, ALICE, 600);
        LedgerTree.ProofNode[] memory proof = V2TreeHelper.proof(levels, 0);
        vm.expectRevert(ISTSettlementVault2.NothingToClaim.selector);
        vault.claim(NO1, 0, alice, proof);
    }

    // V-13: after a newer root only the increment is paid.
    function test_V13_aNewerRootPaysOnlyTheIncrement() public {
        V2TreeHelper.Node[][] memory old = _root0();
        _claim(NO1, 0, old, 0, ALICE, 600);
        V2TreeHelper.Node[][] memory levels = _publish(NO1, _keys(ALICE, BOB, CAROL), _values(900, 600, 500));
        LedgerTree.Account memory alice = _account(0, ALICE, 900);
        LedgerTree.ProofNode[] memory proof = V2TreeHelper.proof(levels, 0);

        vm.expectEmit(address(vault));
        emit ISTSettlementVault2.Claimed(NO1, 1, 0, ALICE, 900, 300, address(this));
        assertEq(vault.claim(NO1, 1, alice, proof), 300);
        assertEq(staking.stakes(ESCROW, ALICE), 900);
        assertEq(vault.claimed(NO1, 0), 900);
        _assertConserved();
    }

    // V-13: neighbouring accounts keep their own watermarks, whichever of
    // them claims first, and each later claim pays only its own increment.
    function test_V13_neighbouringAccountsAreClaimedIndependently() public {
        V2TreeHelper.Node[][] memory old = _root0();
        assertEq(_claim(NO1, 0, old, 1, BOB, 400), 400);
        assertEq(vault.claimed(NO1, 0), 0);
        assertEq(vault.claimed(NO1, 1), 400);
        assertEq(_claim(NO1, 0, old, 0, ALICE, 600), 600);
        assertEq(vault.claimed(NO1, 0), 600);
        assertEq(vault.claimed(NO1, 1), 400);

        V2TreeHelper.Node[][] memory levels = _publish(NO1, _keys(ALICE, BOB, CAROL), _values(900, 600, 500));
        assertEq(_claim(NO1, 1, levels, 1, BOB, 600), 200);
        assertEq(vault.claimed(NO1, 0), 600);
        assertEq(_claim(NO1, 1, levels, 0, ALICE, 900), 300);
        assertEq(vault.claimed(NO1, 1), 600);
        assertEq(vault.claimed(NO1, 2), 0);
        assertEq(vault.claimed(NO1, 3), 0);
        LedgerTree.Account memory bob = _account(1, BOB, 600);
        LedgerTree.ProofNode[] memory proof = V2TreeHelper.proof(levels, 1);
        vm.expectRevert(ISTSettlementVault2.NothingToClaim.selector);
        vault.claim(NO1, 1, bob, proof);
        _assertConserved();
    }

    // V-14
    function test_V14_anOlderRootAfterANewerClaimIsRefused() public {
        V2TreeHelper.Node[][] memory old = _root0();
        V2TreeHelper.Node[][] memory levels = _publish(NO1, _keys(ALICE, BOB, CAROL), _values(900, 600, 500));
        _claim(NO1, 1, levels, 0, ALICE, 900);
        LedgerTree.Account memory alice = _account(0, ALICE, 600);
        LedgerTree.ProofNode[] memory proof = V2TreeHelper.proof(old, 0);
        vm.expectRevert(ISTSettlementVault2.NothingToClaim.selector);
        vault.claim(NO1, 0, alice, proof);
    }

    // V-14: an older root stays claimable, and the newer root then pays the
    // difference.
    function test_V14_theOlderRootFirstThenTheNewerPaysTheDifference() public {
        V2TreeHelper.Node[][] memory old = _root0();
        V2TreeHelper.Node[][] memory levels = _publish(NO1, _keys(ALICE, BOB, CAROL), _values(900, 600, 500));
        assertEq(_claim(NO1, 0, old, 1, BOB, 400), 400);
        assertEq(_claim(NO1, 1, levels, 1, BOB, 600), 200);
        assertEq(staking.stakes(ESCROW, BOB), 600);
        assertEq(vault.claimed(NO1, 1), 600);
        _assertConserved();
    }

    // ------------------------------------------------------------------
    // V-15: claim refusals
    // ------------------------------------------------------------------

    // V-15
    function test_V15_claimRefusesAnInvalidProof() public {
        V2TreeHelper.Node[][] memory levels = _root0();
        LedgerTree.Account memory inflated = _account(0, ALICE, 601);
        LedgerTree.ProofNode[] memory proof = V2TreeHelper.proof(levels, 0);
        vm.expectRevert(ISTSettlementVault2.InvalidProof.selector);
        vault.claim(NO1, 0, inflated, proof);
    }

    // V-15
    function test_V15_claimRefusesAnUnknownRootIndex() public {
        V2TreeHelper.Node[][] memory levels = _root0();
        LedgerTree.Account memory alice = _account(0, ALICE, 600);
        LedgerTree.ProofNode[] memory proof = V2TreeHelper.proof(levels, 0);
        vm.expectRevert(ISTSettlementVault2.UnknownRoot.selector);
        vault.claim(NO1, 1, alice, proof);
    }

    // V-15
    function test_V15_claimRefusesAPoolWithNoRoot() public {
        vm.expectRevert(ISTSettlementVault2.UnknownRoot.selector);
        vault.claim(NO2, 0, _account(0, ALICE, 600), new LedgerTree.ProofNode[](0));
    }

    // V-15: even a validly proven zero-coldkey account is never paid.
    function test_V15_claimRefusesAZeroColdkey() public {
        V2TreeHelper.Node[][] memory levels = _publish(NO1, _keys(ALICE, bytes32(0)), _values(600, 400));
        LedgerTree.Account memory zero = _account(1, bytes32(0), 400);
        LedgerTree.ProofNode[] memory proof = V2TreeHelper.proof(levels, 1);
        vm.expectRevert(ISTSettlementVault2.InvalidColdkey.selector);
        vault.claim(NO1, 0, zero, proof);
    }

    // V-15: nor is the vault's own coldkey, whose transfer would be a no-op.
    function test_V15_claimRefusesTheVaultsOwnColdkey() public {
        V2TreeHelper.Node[][] memory levels = _publish(NO1, _keys(ALICE, SELF), _values(600, 400));
        LedgerTree.Account memory own = _account(1, SELF, 400);
        LedgerTree.ProofNode[] memory proof = V2TreeHelper.proof(levels, 1);
        vm.expectRevert(ISTSettlementVault2.InvalidColdkey.selector);
        vault.claim(NO1, 0, own, proof);
    }

    // V-15: an amount the precompile would read as the whole position is
    // refused; one rao less is paid. Funding arrives in captures below the
    // sentinel.
    function test_V15_claimRefusesTheWholePositionSentinel() public {
        _capture(NO1, WHOLE_POSITION - 1);
        _capture(NO1, 1 << 63);
        _capture(NO1, (1 << 63) - 1);
        V2TreeHelper.Node[][] memory levels =
            _publish(NO1, _keys(ALICE, BOB), _values(WHOLE_POSITION, WHOLE_POSITION - 1));
        LedgerTree.Account memory alice = _account(0, ALICE, WHOLE_POSITION);
        LedgerTree.ProofNode[] memory proof = V2TreeHelper.proof(levels, 0);
        vm.expectRevert(ISTSettlementVault2.AmountTooLarge.selector);
        vault.claim(NO1, 0, alice, proof);

        assertEq(_claim(NO1, 0, levels, 1, BOB, WHOLE_POSITION - 1), WHOLE_POSITION - 1);
        _assertConserved();
    }

    // ------------------------------------------------------------------
    // V-16, V-21: the pool cap
    // ------------------------------------------------------------------

    // Root 1 lowers Alice, who already claimed 600 under root 0, to 100 and
    // gives Carol 1,500. Nobody challenged it.
    function _unchallengedDecrease(uint256 noId, bytes32 newcomer)
        internal
        returns (V2TreeHelper.Node[][] memory)
    {
        return _publish(noId, _keys(ALICE, BOB, newcomer), _values(100, 400, 1_500));
    }

    // V-16
    function test_V16_afterAnUnchallengedDecreaseTheCapStopsTheSecondClaimant() public {
        V2TreeHelper.Node[][] memory old = _root0();
        _claim(NO1, 0, old, 0, ALICE, 600);
        V2TreeHelper.Node[][] memory levels = _unchallengedDecrease(NO1, CAROL);
        assertEq(vault.finalizedTotal(NO1), 2_000);

        LedgerTree.Account memory carol = _account(2, CAROL, 1_500);
        LedgerTree.ProofNode[] memory proof = V2TreeHelper.proof(levels, 2);
        vm.expectRevert(ISTSettlementVault2.PoolCapExceeded.selector);
        vault.claim(NO1, 1, carol, proof);

        // Bob's claim fits under the cap; Carol's never does.
        assertEq(_claim(NO1, 1, levels, 1, BOB, 400), 400);
        vm.expectRevert(ISTSettlementVault2.PoolCapExceeded.selector);
        vault.claim(NO1, 1, carol, proof);
        assertEq(vault.claimedTotal(NO1), 1_000);
        _assertConserved();
    }

    // V-16: with honest roots every account claims everything and the cap
    // is met exactly, never exceeded.
    function test_V16_withHonestRootsTheCapNeverBinds() public {
        V2TreeHelper.Node[][] memory old = _root0();
        _claim(NO1, 0, old, 0, ALICE, 600);
        V2TreeHelper.Node[][] memory levels = _publish(NO1, _keys(ALICE, BOB, CAROL), _values(900, 600, 500));
        assertEq(_claim(NO1, 1, levels, 2, CAROL, 500), 500);
        assertEq(_claim(NO1, 1, levels, 1, BOB, 600), 600);
        assertEq(_claim(NO1, 1, levels, 0, ALICE, 900), 300);
        assertEq(vault.claimedTotal(NO1), vault.finalizedTotal(NO1));
        assertEq(vault.escrowAccounted(), 0);
        _assertConserved();
    }

    // V-16, G4 (review M1): the cap is the latest finalized total, not the
    // total of the root a claim is proven against. Once Carol and Alice have
    // claimed 1,400 from root 1, more than root 0's whole total of 1,000, Bob
    // still claims his root-0 balance, and then root 1's increment.
    function test_V16_theCapIsTheLatestFinalizedTotalWhicheverRootIsClaimed() public {
        V2TreeHelper.Node[][] memory old = _root0();
        V2TreeHelper.Node[][] memory levels = _publish(NO1, _keys(ALICE, BOB, CAROL), _values(900, 600, 500));
        assertEq(_claim(NO1, 1, levels, 2, CAROL, 500), 500);
        assertEq(_claim(NO1, 1, levels, 0, ALICE, 900), 900);
        assertGt(vault.claimedTotal(NO1), vault.roots(NO1, 0).total);

        assertEq(_claim(NO1, 0, old, 1, BOB, 400), 400);
        assertEq(_claim(NO1, 1, levels, 1, BOB, 600), 200);
        assertEq(vault.claimedTotal(NO1), vault.finalizedTotal(NO1));
        assertEq(vault.escrowAccounted(), 0);
        _assertConserved();
    }

    // V-16: the cap is the latest finalized total also while a successor is
    // pending. vault2 keeps the pending root at index rootCount, beside the
    // finalized roots (§7.1), and its larger total lifts the cap only once it
    // is finalized: Carol's 1,500 is refused under root 1's 2,000 while root
    // 2 is pending with 2,500, and paid after root 2 is finalized.
    function test_V16_theCapIsTheFinalizedTotalWhileASuccessorIsPending() public {
        V2TreeHelper.Node[][] memory old = _root0();
        _claim(NO1, 0, old, 0, ALICE, 600);
        V2TreeHelper.Node[][] memory levels = _unchallengedDecrease(NO1, CAROL);

        bytes32[] memory keys = new bytes32[](4);
        uint256[] memory values = new uint256[](4);
        (keys[0], keys[1], keys[2], keys[3]) = (ALICE, BOB, CAROL, DAVE);
        (values[0], values[1], values[2], values[3]) = (100, 400, 1_500, 500);
        _propose(NO1, V2TreeHelper.build(keys, values), 4);
        assertEq(vault.pending(NO1).total, 2_500);
        assertEq(vault.finalizedTotal(NO1), 2_000);

        LedgerTree.Account memory carol = _account(2, CAROL, 1_500);
        LedgerTree.ProofNode[] memory proof = V2TreeHelper.proof(levels, 2);
        vm.expectRevert(ISTSettlementVault2.PoolCapExceeded.selector);
        vault.claim(NO1, 1, carol, proof);
        assertEq(vault.claimedTotal(NO1), 600);

        _finalize(NO1);
        assertEq(vault.finalizedTotal(NO1), 2_500);
        assertEq(_claim(NO1, 1, levels, 2, CAROL, 1_500), 1_500);
        assertEq(vault.claimedTotal(NO1), 2_100);
        _assertConserved();
    }

    // V-21: each pool's claims are capped by its own finalized total, however
    // much escrow holds for other pools.
    function test_V21_eachPoolIsCappedByItsOwnFinalizedTotal() public {
        _registerPool2();
        V2TreeHelper.Node[][] memory pool2 = _publish(NO2, _keys(DAVE), _values(5_000));
        V2TreeHelper.Node[][] memory old = _root0();
        _claim(NO1, 0, old, 0, ALICE, 600);
        V2TreeHelper.Node[][] memory levels = _unchallengedDecrease(NO1, CAROL);
        assertEq(vault.liveEscrowStake(), 6_400);

        LedgerTree.Account memory carol = _account(2, CAROL, 1_500);
        LedgerTree.ProofNode[] memory proof = V2TreeHelper.proof(levels, 2);
        vm.expectRevert(ISTSettlementVault2.PoolCapExceeded.selector);
        vault.claim(NO1, 1, carol, proof);

        assertEq(_claim(NO2, 0, pool2, 0, DAVE, 5_000), 5_000);
        assertEq(vault.claimedTotal(NO2), 5_000);
        assertEq(vault.claimedTotal(NO1), 600);
        assertTrue(vault.conservationHolds());
    }

    // V-21: the same coldkey at the same index in two pools is two accounts.
    function test_V21_claimsInOnePoolLeaveTheOtherPoolsAccountsAlone() public {
        _registerPool2();
        V2TreeHelper.Node[][] memory pool2 = _publish(NO2, _keys(ALICE), _values(700));
        V2TreeHelper.Node[][] memory pool1 = _publish(NO1, _keys(ALICE), _values(300));
        assertEq(_claim(NO1, 0, pool1, 0, ALICE, 300), 300);
        assertEq(vault.claimed(NO2, 0), 0);
        assertEq(_claim(NO2, 0, pool2, 0, ALICE, 700), 700);
        assertEq(staking.stakes(ESCROW, ALICE), 1_000);
        assertTrue(vault.conservationHolds());
    }

    // ------------------------------------------------------------------
    // V-17: destination
    // ------------------------------------------------------------------

    // V-17: whoever sends the claim, the stake goes to the leaf's coldkey.
    function test_V17_thePaymentGoesToTheLeafsColdkeyWhoeverCalls() public {
        V2TreeHelper.Node[][] memory levels = _root0();
        V2ClaimRelayer relayer = new V2ClaimRelayer();
        LedgerTree.Account memory alice = _account(0, ALICE, 600);
        LedgerTree.ProofNode[] memory aliceProof = V2TreeHelper.proof(levels, 0);

        vm.expectEmit(address(vault));
        emit ISTSettlementVault2.Claimed(NO1, 0, 0, ALICE, 600, 600, address(relayer));
        relayer.relay(vault, NO1, 0, alice, aliceProof);

        LedgerTree.Account memory bob = _account(1, BOB, 400);
        LedgerTree.ProofNode[] memory bobProof = V2TreeHelper.proof(levels, 1);
        vm.expectEmit(address(vault));
        emit ISTSettlementVault2.Claimed(NO1, 0, 1, BOB, 400, 400, stranger);
        vm.prank(stranger);
        vault.claim(NO1, 0, bob, bobProof);

        assertEq(staking.stakes(ESCROW, ALICE), 600);
        assertEq(staking.stakes(ESCROW, BOB), 400);
        assertEq(vault.liveEscrowStake(), 0);
        _assertConserved();
    }

    // V-17: a leaf binds its coldkey; another coldkey cannot claim it.
    function test_V17_anotherColdkeyCannotTakeTheAccount() public {
        V2TreeHelper.Node[][] memory levels = _root0();
        LedgerTree.Account memory taken = _account(0, MALLORY, 600);
        LedgerTree.ProofNode[] memory proof = V2TreeHelper.proof(levels, 0);
        vm.expectRevert(ISTSettlementVault2.InvalidProof.selector);
        vault.claim(NO1, 0, taken, proof);
    }
}
