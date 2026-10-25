// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {VmSafe} from "forge-std/Vm.sol";

import {V2VaultBase} from "./utils/V2VaultBase.sol";
import {V2TreeHelper} from "./utils/V2TreeHelper.sol";
import {STSettlementVault2} from "../src/STSettlementVault2.sol";
import {ISTSettlementVault2} from "../src/interfaces/ISTSettlementVault2.sol";
import {LedgerTree} from "../src/LedgerTree.sol";

/// @dev V-EV: every vault2 event, with its exact fields, over one pool's life;
///      V-SIZE: vault2's deployed sizes.
contract V2VaultEventsTest is V2VaultBase {
    bytes32 internal constant FOREIGN = keccak256("v1-escrow-hotkey");

    // V-EV
    function test_VEV_everyEventCarriesItsExactFields() public {
        // Installation: a second vault, driven through the base helpers.
        vault = new STSettlementVault2(NETUID, ESCROW, SELF, address(this), DEMAND_FEE);
        staking.setColdkey(address(vault), SELF);
        vm.expectEmit(address(vault));
        emit ISTSettlementVault2.CoordinatorFixed(address(this));
        vault.setCoordinatorOnce(address(this));
        vm.expectEmit(address(vault));
        emit ISTSettlementVault2.EscrowRegistered(ESCROW, ESCROW_UID);
        vault.registerEscrow(BURN_LIMIT);
        vm.expectEmit(address(vault));
        emit ISTSettlementVault2.PoolRegistered(NO1, POOL1, POOL1_UID, FIRST_EPOCH);
        vault.registerPool(NO1, POOL1, BURN_LIMIT, FIRST_EPOCH);

        // The pool hotkey is pruned and anyone re-registers it.
        neuron.clearUid(NETUID, POOL1);
        _uidOnRegistration(POOL1, 42);
        vm.expectEmit(address(vault));
        emit ISTSettlementVault2.PoolUidRegistered(NO1, POOL1, 42, stranger);
        vm.prank(stranger);
        vault.registerPoolUid(NO1, BURN_LIMIT);
        neuron.clearReentry();

        // Funding: a capture one rao short, then a deferral.
        staking.setStake(POOL1, SELF, 1_001);
        staking.setMoveStakeShortfall(1);
        vm.expectEmit(address(vault));
        emit ISTSettlementVault2.EmissionCaptured(FIRST_EPOCH, NO1, 1_001, 1_000, 1_000);
        vault.captureEmission(FIRST_EPOCH, NO1);
        staking.setMoveStakeShortfall(0);
        vm.expectEmit(address(vault));
        emit ISTSettlementVault2.EmissionDeferred(FIRST_EPOCH + 1, NO1, 1_000);
        vault.deferEmission(FIRST_EPOCH + 1, NO1);

        // The first root: committed and finalized.
        V2TreeHelper.Node[][] memory f = V2TreeHelper.build(_keys(ALICE, BOB), _values(600, 400));
        ISTSettlementVault2.Commitment memory first = _commitment(f, 2);
        vm.expectEmit(address(vault));
        emit ISTSettlementVault2.DistributionCommitted(
            NO1, 1, FIRST_EPOCH + 1, first.root, 1_000, 2, first.ledgerHash, 0
        );
        vault.commitPending(FIRST_EPOCH + 1, NO1, 0, first);
        vm.roll(block.number + WINDOW);
        vm.expectEmit(address(vault));
        emit ISTSettlementVault2.DistributionFinalized(
            NO1, 1, 0, FIRST_EPOCH + 1, first.root, 1_000, 2, first.ledgerHash
        );
        vault.finalizePending(NO1);

        // A successor that keeps Alice whole but lowers Bob: a demand on
        // Alice is answered, and Bob's account deletes the root.
        staking.setStake(POOL1, SELF, 500);
        vault.captureEmission(FIRST_EPOCH + 2, NO1);
        V2TreeHelper.Node[][] memory p = V2TreeHelper.build(_keys(ALICE, BOB, CAROL), _values(700, 300, 500));
        ISTSettlementVault2.Commitment memory second = _commitment(p, 3);
        vm.expectEmit(address(vault));
        emit ISTSettlementVault2.DistributionCommitted(
            NO1, 2, FIRST_EPOCH + 2, second.root, 1_500, 3, second.ledgerHash, 0
        );
        vault.commitPending(FIRST_EPOCH + 2, NO1, 1, second);

        LedgerTree.Account memory alice = _account(0, ALICE, 600);
        LedgerTree.ProofNode[] memory aliceProof = V2TreeHelper.proof(f, 0);
        LedgerTree.PathNode[] memory alicePath = V2TreeHelper.path(p, 0);
        vm.deal(stranger, DEMAND_FEE);
        vm.expectEmit(address(vault));
        emit ISTSettlementVault2.PathDemanded(NO1, 2, 0, 0, stranger);
        vm.prank(stranger);
        vault.demandPath{value: DEMAND_FEE}(NO1, 0, alice, aliceProof);

        vm.expectEmit(address(vault));
        emit ISTSettlementVault2.PathAnswered(NO1, 2, 0, 0, address(this));
        vault.checkPath(NO1, 0, alice, aliceProof, alicePath);

        LedgerTree.Account memory bob = _account(1, BOB, 400);
        LedgerTree.ProofNode[] memory bobProof = V2TreeHelper.proof(f, 1);
        LedgerTree.PathNode[] memory bobPath = V2TreeHelper.path(p, 1);
        vm.expectEmit(address(vault));
        emit ISTSettlementVault2.DistributionDeleted(NO1, 2, 0, 1, LedgerTree.Fault.LowerValue, 0, stranger);
        vm.prank(stranger);
        vault.checkPath(NO1, 0, bob, bobProof, bobPath);

        // Claims, absorption and surplus.
        vm.expectEmit(address(vault));
        emit ISTSettlementVault2.Claimed(NO1, 0, 1, BOB, 400, 400, stranger);
        vm.prank(stranger);
        vault.claim(NO1, 0, bob, bobProof);

        staking.setStake(FOREIGN, SELF, 250);
        vm.expectEmit(address(vault));
        emit ISTSettlementVault2.StakeAbsorbed(FOREIGN, 250, 250, stranger);
        vm.prank(stranger);
        vault.absorbStake(FOREIGN);

        vm.expectEmit(address(vault));
        emit ISTSettlementVault2.SurplusAssigned(NO1, FIRST_EPOCH + 3, 250);
        vault.assignSurplus(NO1, 250);

        assertTrue(vault.conservationHolds());
        assertEq(vault.totalCaptured(), 1_750);
        assertEq(vault.totalPaid(), 400);
        assertEq(vault.escrowAccounted(), 1_350);
    }

    // V-SIZE: sizes from the default profile, which is the release build
    // (`forge build --sizes`). A coverage build is unoptimized, so it is skipped.
    function test_VSIZE_vault2FitsTheContractSizeLimits() public {
        vm.skip(vm.isContext(VmSafe.ForgeContext.Coverage));
        uint256 runtime = address(vault).code.length;
        uint256 creation = type(STSettlementVault2).creationCode.length;
        emit log_named_uint("vault2 runtime bytes", runtime);
        emit log_named_uint("vault2 runtime margin", 24_576 - runtime);
        emit log_named_uint("vault2 creation bytes", creation);
        emit log_named_uint("vault2 creation margin", 49_152 - creation);
        assertLt(runtime, 24_576);
        assertLt(creation, 49_152);
    }
}
