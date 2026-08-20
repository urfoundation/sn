// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {ReleaseBase} from "./utils/ReleaseBase.sol";
import {STCoordinator} from "../src/STCoordinator.sol";
import {STSettlementVault} from "../src/STSettlementVault.sol";
import {STCoordinatorAdversary} from "../src/testnet/STCoordinatorAdversary.sol";
import {ERC1967Proxy} from "@openzeppelin/contracts/proxy/ERC1967/ERC1967Proxy.sol";

contract MaliciousCoordinatorV2 is STCoordinator {
    function attackRewrite(
        STSettlementVault target,
        uint256 epoch,
        uint256 noId,
        bytes32 root,
        bytes32 artifact,
        uint64 expiry
    ) external {
        target.finalizeEntitlement(epoch, noId, root, artifact, expiry);
    }
}

contract ReleaseSettlementTest is ReleaseBase {
    function _finalizeSingle(uint256 epoch_, uint256 noId, uint256 amount)
        internal
        returns (bytes32[] memory proof)
    {
        _accrue(noId, amount);
        _close(epoch_, noId);
        (bytes32 root, bytes32[] memory singleProof) = _singleLeaf(PROVIDER1, 10_000);
        address signer = noId == NO1 ? rootSigner1 : rootSigner2;
        vm.prank(signer);
        coordinator.commitOperatorRoot(epoch_, noId, root, keccak256("artifact"));
        vm.roll(_end(epoch_) + FINALIZE_OFFSET);
        coordinator.finalizeOperatorEpoch(epoch_, noId);
        return singleProof;
    }

    function test_escrowIsRegisteredExactlyOnceByTheImmutableVault() public {
        assertTrue(vault.escrowRegistered());
        assertEq(neuron.registrants(NETUID, ESCROW_HOTKEY), address(vault));

        vm.expectRevert(STSettlementVault.AlreadyInitialized.selector);
        vault.registerEscrow();
        vm.prank(stranger);
        vm.expectRevert(STSettlementVault.Unauthorized.selector);
        vault.registerEscrow();
    }

    function test_coordinatorCannotInitializeAgainstAnUnregisteredEscrow() public {
        STSettlementVault unregistered =
            new STSettlementVault(NETUID, keccak256("unregistered-escrow"), VAULT_COLDKEY, 1, address(this));
        STCoordinator nextImplementation = new STCoordinator();
        vm.expectRevert(STCoordinator.InvalidConfiguration.selector);
        new ERC1967Proxy(
            address(nextImplementation),
            abi.encodeCall(
                STCoordinator.initialize,
                (NETUID, owner, guardian, COORD_COLDKEY, unregistered, sink, oracle, _policy())
            )
        );
    }

    function test_captureFinalizeClaimAndConservation() public {
        bytes32[] memory proof = _finalizeSingle(0, NO1, 1_000);
        STSettlementVault.Entitlement memory e = vault.entitlement(0, NO1);
        assertEq(uint256(e.status), uint256(STSettlementVault.EpochStatus.Finalized));
        assertEq(e.total, 1_000);
        assertEq(staking.stakes(ESCROW_HOTKEY, VAULT_COLDKEY), 1_000);

        vault.claim(0, NO1, PROVIDER1, 10_000, proof);
        assertEq(staking.stakes(ESCROW_HOTKEY, PROVIDER1), 1_000);
        assertEq(vault.totalPaid(), 1_000);
        assertTrue(vault.conservationHolds());
    }

    function test_partialClaimExpiryCarriesExactRemainderToSameOperator() public {
        _accrue(NO1, 1_001);
        _close(0, NO1);
        (bytes32 root, bytes32[] memory proof1,) = _twoLeafTree();
        vm.prank(rootSigner1);
        coordinator.commitOperatorRoot(0, NO1, root, keccak256("two-provider-artifact"));
        vm.roll(_end(0) + FINALIZE_OFFSET);
        coordinator.finalizeOperatorEpoch(0, NO1);

        vault.claim(0, NO1, PROVIDER1, 6_000, proof1);
        STSettlementVault.Entitlement memory beforeExpiry = vault.entitlement(0, NO1);
        assertEq(beforeExpiry.claimed, 600);
        vm.roll(uint256(beforeExpiry.expiryBlock) + 1);
        vault.expireEntitlement(0, NO1);
        assertEq(vault.carry(NO1), 401);
        assertEq(vault.carry(NO2), 0);
        assertTrue(vault.conservationHolds());
    }

    function test_missedRootCarriesOnlyItsOperator() public {
        _accrue(NO1, 333);
        _close(0, NO1);
        vm.roll(_end(0) + FINALIZE_OFFSET);
        coordinator.finalizeOperatorEpoch(0, NO1);
        assertEq(vault.carry(NO1), 333);
        assertEq(vault.carry(NO2), 0);
        assertTrue(vault.conservationHolds());
    }

    function test_lateKeeperNeverAttributesMultiEpochDeltaToFirstMissedEpoch() public {
        _accrue(NO1, 500);
        vm.roll(_end(0) + CLOSE_GRACE + 1);
        coordinator.deferMissedEmission(0, NO1);

        STSettlementVault.Entitlement memory missed = vault.entitlement(0, NO1);
        assertEq(missed.funded, 0);
        assertEq(staking.stakes(POOL1, VAULT_COLDKEY), 500);

        vm.roll(_end(1));
        coordinator.closeOperatorEpoch(1, NO1);
        STSettlementVault.Entitlement memory next = vault.entitlement(1, NO1);
        assertEq(next.funded, 500);
        assertEq(missed.funded, 0);
    }

    function test_pauseAndMaliciousUpgradeCannotRewriteOrBlockFinalizedClaim() public {
        bytes32[] memory proof = _finalizeSingle(0, NO1, 777);
        STSettlementVault.Entitlement memory immutableBefore = vault.entitlement(0, NO1);

        vm.prank(guardian);
        coordinator.setPaused(true);

        MaliciousCoordinatorV2 malicious = new MaliciousCoordinatorV2();
        vm.prank(owner);
        coordinator.upgradeToAndCall(address(malicious), bytes(""));
        MaliciousCoordinatorV2 upgraded = MaliciousCoordinatorV2(payable(address(coordinator)));

        vm.expectRevert(STSettlementVault.InvalidTransition.selector);
        upgraded.attackRewrite(
            vault, 0, NO1, keccak256("evil-root"), keccak256("evil-artifact"), immutableBefore.expiryBlock + 1
        );
        STSettlementVault.Entitlement memory immutableAfter = vault.entitlement(0, NO1);
        assertEq(immutableAfter.payoutRoot, immutableBefore.payoutRoot);
        assertEq(immutableAfter.total, immutableBefore.total);
        assertEq(immutableAfter.expiryBlock, immutableBefore.expiryBlock);

        vault.claim(0, NO1, PROVIDER1, 10_000, proof);
        assertEq(staking.stakes(ESCROW_HOTKEY, PROVIDER1), 777);
    }

    function test_testnetAdversaryProbesEveryImmutableCustodyBoundary() public {
        bytes32[] memory proof = _finalizeSingle(0, NO1, 777);
        STSettlementVault.Entitlement memory beforeEntitlement = vault.entitlement(0, NO1);
        uint256 beforePrincipal = sink.principal();
        uint256 beforeLiveStake = sink.liveStake();

        vm.prank(guardian);
        coordinator.setPaused(true);
        STCoordinatorAdversary adversary = new STCoordinatorAdversary();
        vm.prank(owner);
        coordinator.upgradeToAndCall(address(adversary), bytes(""));

        vm.prank(owner);
        uint256 unexpected = STCoordinatorAdversary(payable(address(coordinator)))
            .runCustodyProbes(
                0,
                NO1,
                keccak256("replacement-root"),
                keccak256("replacement-artifact"),
                beforeEntitlement.expiryBlock + 1,
                PROVIDER2,
                RESERVE_HOTKEY
            );
        assertEq(unexpected, 0);
        assertEq(vault.coordinator(), address(coordinator));
        assertEq(sink.recorder(), address(coordinator));
        assertEq(sink.principal(), beforePrincipal);
        assertEq(sink.liveStake(), beforeLiveStake);
        STSettlementVault.Entitlement memory afterEntitlement = vault.entitlement(0, NO1);
        assertEq(afterEntitlement.payoutRoot, beforeEntitlement.payoutRoot);
        assertEq(afterEntitlement.artifactHash, beforeEntitlement.artifactHash);
        assertEq(afterEntitlement.total, beforeEntitlement.total);
        assertEq(afterEntitlement.expiryBlock, beforeEntitlement.expiryBlock);

        // Claims are deliberately outside the coordinator pause/upgrade path.
        vault.claim(0, NO1, PROVIDER1, 10_000, proof);
        assertEq(staking.stakes(ESCROW_HOTKEY, PROVIDER1), 777);
    }

    function test_failedSweepCannotCreateUnderfundedFinalization() public {
        _accrue(NO1, 100);
        staking.setFailMoveStake(true);
        vm.roll(_end(0));
        vm.expectRevert("mock: moveStake down");
        coordinator.closeOperatorEpoch(0, NO1);
        STSettlementVault.Entitlement memory e = vault.entitlement(0, NO1);
        assertEq(uint256(e.status), uint256(STSettlementVault.EpochStatus.Unset));

        staking.setFailMoveStake(false);
        coordinator.closeOperatorEpoch(0, NO1);
        e = vault.entitlement(0, NO1);
        assertEq(e.funded, 100);
    }
}
