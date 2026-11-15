// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {V2StackBase} from "./utils/V2StackBase.sol";
import {V2TreeHelper} from "./utils/V2TreeHelper.sol";
import {ISTSettlement2} from "../src/interfaces/ISTSettlement2.sol";
import {ISTSettlementVault2} from "../src/interfaces/ISTSettlementVault2.sol";

/// @dev STSettlement2 wired to the real vault2 (VERSION2-FINAL.md §5, §15
///      rules: one wiring test per layer above). vault2's own refusals
///      propagate through finalizeDistribution (S-8), and one honest root
///      goes from registration to claims.
contract V2SettlementStackTest is V2StackBase {
    // §5, wiring of S-3, S-5, S-6, S-7, S-8 and V-12, V-17, V-EV, S-EV: an
    // honest root of one epoch is registered, closed, committed, approved,
    // finalized and claimed through the whole stack.
    function test_anHonestRootGoesFromRegistrationToClaims() public {
        // Registration: vault2 burns 0.4 ether of the 1 ether sent and refunds
        // the rest through STSettlement2 to the owner.
        neuron.setUid(NETUID, V2_POOL1, V2_POOL1_UID);
        neuron.setRegistrationBurn(0.4 ether);
        vm.deal(owner, 1 ether);
        vm.expectEmit(address(vault2));
        emit ISTSettlementVault2.PoolRegistered(NO1, V2_POOL1, V2_POOL1_UID, FIRST_EPOCH);
        vm.prank(owner);
        uint16 uid =
            settlement.registerOperatorPool{value: 1 ether}(NO1, V2_POOL1, V2_BURN_LIMIT, FIRST_EPOCH);
        assertEq(uid, V2_POOL1_UID);
        assertEq(owner.balance, 0.6 ether);
        assertEq(address(settlement).balance, 0);
        assertEq(address(vault2).balance, 0);
        assertEq(neuron.registrants(NETUID, V2_POOL1), address(vault2));
        (address vault_, bytes32 hotkey, uint8 version) = settlement.settlementAt(NO1, FIRST_EPOCH);
        assertEq(vault_, address(vault2));
        assertEq(hotkey, V2_POOL1);
        assertEq(version, 2);

        // Close: anyone captures the epoch's emission at its end.
        _accrueV2(V2_POOL1, 1_000);
        vm.roll(_end(FIRST_EPOCH));
        vm.expectEmit(address(vault2));
        emit ISTSettlementVault2.EmissionCaptured(FIRST_EPOCH, NO1, 1_000, 1_000, 1_000);
        vm.prank(stranger);
        assertEq(settlement.closeOperatorEpoch(FIRST_EPOCH, NO1), 1_000);
        assertEq(staking.stakes(V2_ESCROW, V2_SELF), 1_000);
        assertEq(staking.stakes(V2_POOL1, V2_SELF), 0);

        // Commit: the epoch's root signer commits Alice 600 and Bob 400.
        V2TreeHelper.Node[][] memory levels = V2TreeHelper.build(_keys(ALICE, BOB), _values(600, 400));
        ISTSettlementVault2.Commitment memory c = _commitment(levels, 2);
        vm.expectEmit(address(vault2));
        emit ISTSettlementVault2.DistributionCommitted(NO1, 1, FIRST_EPOCH, c.root, 1_000, 2, c.ledgerHash, 0);
        vm.prank(rootSigner1);
        assertEq(settlement.commitDistribution(FIRST_EPOCH, NO1, 0, c), 1);
        uint256 committed = block.number;

        // Approve: the launch auditor approves the exact commit.
        vm.expectEmit(address(settlement));
        emit ISTSettlement2.DistributionApproved(NO1, 1, auditor1, 1, 1);
        vm.prank(auditor1);
        settlement.approveDistribution(NO1, 1, c.root, c.ledgerHash);

        // Finalize: anyone, once the challenge window has passed.
        vm.roll(committed + WINDOW);
        vm.expectEmit(address(vault2));
        emit ISTSettlementVault2.DistributionFinalized(NO1, 1, 0, FIRST_EPOCH, c.root, 1_000, 2, c.ledgerHash);
        vm.prank(stranger);
        assertEq(settlement.finalizeDistribution(NO1), 0);
        assertEq(vault2.finalizedTotal(NO1), 1_000);

        // Claims: each account is paid to its own coldkey, whoever claims.
        vm.expectEmit(address(vault2));
        emit ISTSettlementVault2.Claimed(NO1, 0, 0, ALICE, 600, 600, stranger);
        vm.prank(stranger);
        assertEq(vault2.claim(NO1, 0, _account(0, ALICE, 600), V2TreeHelper.proof(levels, 0)), 600);
        vm.expectEmit(address(vault2));
        emit ISTSettlementVault2.Claimed(NO1, 0, 1, BOB, 400, 400, address(this));
        assertEq(vault2.claim(NO1, 0, _account(1, BOB, 400), V2TreeHelper.proof(levels, 1)), 400);

        assertEq(staking.stakes(V2_ESCROW, ALICE), 600);
        assertEq(staking.stakes(V2_ESCROW, BOB), 400);
        assertEq(vault2.claimedTotal(NO1), 1_000);
        assertEq(vault2.totalPaid(), 1_000);
        assertEq(vault2.escrowAccounted(), 0);
        assertEq(vault2.liveEscrowStake(), 0);
        assertTrue(vault2.conservationHolds());
    }

    // S-8, G6: with a quorum of approvals, vault2 still refuses a finalization
    // inside the challenge window, and the refusal propagates unchanged.
    function test_S8_vault2RefusesAFinalizationInsideTheWindow() public {
        _registerV2Pool(NO1, V2_POOL1, V2_POOL1_UID);
        _accrueV2(V2_POOL1, 1_000);
        _closeV2(FIRST_EPOCH, NO1);
        V2TreeHelper.Node[][] memory levels = V2TreeHelper.build(_keys(ALICE, BOB), _values(600, 400));
        uint64 seq = _commitV2(FIRST_EPOCH, NO1, rootSigner1, levels, 2);
        _approveV2(NO1, auditor1);
        assertEq(settlement.approvalCount(NO1), settlement.quorum());
        uint256 committed = block.number;

        vm.roll(committed + WINDOW - 1);
        vm.expectRevert(ISTSettlementVault2.ChallengeWindowOpen.selector);
        settlement.finalizeDistribution(NO1);
        assertEq(vault2.pending(NO1).seq, seq);
        assertEq(vault2.rootCount(NO1), 0);

        vm.roll(committed + WINDOW);
        assertEq(settlement.finalizeDistribution(NO1), 0);
    }

    // S-8, G6: with a quorum of approvals, vault2 refuses a finalization while
    // a demand is open, even long after the window, until it is answered.
    function test_S8_vault2RefusesAFinalizationWithAnOpenDemand() public {
        _registerV2Pool(NO1, V2_POOL1, V2_POOL1_UID);
        V2TreeHelper.Node[][] memory f = _publishV2(FIRST_EPOCH, _keys(ALICE, BOB), _values(600, 400));
        V2TreeHelper.Node[][] memory p = V2TreeHelper.build(_keys(ALICE, BOB, CAROL), _values(700, 400, 400));
        _accrueV2(V2_POOL1, 500);
        _closeV2(FIRST_EPOCH + 1, NO1);
        uint64 seq = _commitV2(FIRST_EPOCH + 1, NO1, rootSigner1, p, 3);
        _approveV2(NO1, auditor1);
        _demandV2(NO1, 0, f, _account(1, BOB, 400));
        assertEq(vault2.pending(NO1).openDemands, 1);

        vm.roll(block.number + WINDOW * 3);
        vm.expectRevert(ISTSettlementVault2.DemandOpen.selector);
        settlement.finalizeDistribution(NO1);
        assertEq(vault2.pending(NO1).seq, seq);
        assertEq(vault2.rootCount(NO1), 1);

        vault2.checkPath(NO1, 0, _account(1, BOB, 400), V2TreeHelper.proof(f, 1), V2TreeHelper.path(p, 1));
        assertEq(settlement.finalizeDistribution(NO1), 1);
        assertEq(vault2.finalizedTotal(NO1), 1_500);
    }
}
