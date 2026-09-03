// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {ReleaseBase} from "./utils/ReleaseBase.sol";
import {STCoordinator} from "../src/STCoordinator.sol";
import {SafeCast} from "@openzeppelin/contracts/utils/math/SafeCast.sol";
import {ERC1967Proxy} from "@openzeppelin/contracts/proxy/ERC1967/ERC1967Proxy.sol";

contract STCoordinatorChainIdHarness is STCoordinator {
    function checkedChainId(uint256 chainId_) external pure returns (uint64) {
        return _checkedChainId(chainId_);
    }
}

contract ReleaseBindingPolicyTest is ReleaseBase {
    bytes32 internal constant FLEET = keccak256("fleet-one");
    bytes32 internal constant HEAD_HOTKEY = keccak256("fleet-hotkey");
    bytes32 internal constant COMMITMENT = keccak256("fleet-manifest");

    function _signature(uint256 n) internal pure returns (bytes memory) {
        return abi.encodePacked(bytes32(n), bytes32(n + 1));
    }

    function _binding(bytes16 clientId, bytes32 clientKey, uint64 generation)
        internal
        view
        returns (STCoordinator.FleetBinding memory)
    {
        return STCoordinator.FleetBinding({
            chainId: uint64(block.chainid),
            netuid: NETUID,
            coordinator: address(coordinator),
            fleetId: FLEET,
            hotkey: HEAD_HOTKEY,
            clientId: clientId,
            clientKey: clientKey,
            generation: generation,
            validFromEpoch: uint64(coordinator.currentEpoch() + 1),
            validToEpoch: uint64(coordinator.currentEpoch() + 8),
            commitmentHash: COMMITMENT
        });
    }

    function _prepareFleet() internal {
        neuron.setUid(NETUID, HEAD_HOTKEY, 42);
        vm.prank(oracle);
        coordinator.mirrorCommitment(
            HEAD_HOTKEY, COMMITMENT, uint64(block.number), keccak256("finalized-block")
        );
    }

    function test_bindingDigestMatchesGoGoldenVector() public view {
        STCoordinator.FleetBinding memory fixture = STCoordinator.FleetBinding({
            chainId: 945,
            netuid: 17,
            coordinator: 0x1111111111111111111111111111111111111111,
            fleetId: 0x2222222222222222222222222222222222222222222222222222222222222222,
            hotkey: 0x94ad8d1ead1a2bff9bbbac89aa89b13df2fe9ec929a09c90bc5ddb1dff723b47,
            clientId: 0x33333333333333333333333333333333,
            clientKey: 0x03a107bff3ce10be1d70dd18e74bc09967e4d6309ba50d5f1ddc8664125531b8,
            generation: 3,
            validFromEpoch: 11,
            validToEpoch: 42,
            commitmentHash: 0x4444444444444444444444444444444444444444444444444444444444444444
        });
        assertEq(
            coordinator.fleetBindingDigest(fixture),
            0x0de356fd56fc28d72efe5724a81b2462a7f2bb3f041f48128e2d511b0ae05ba7
        );
    }

    function test_manyClientsCanBindOneFleetAndBecomeActiveAtBoundary() public {
        _prepareFleet();
        for (uint256 i = 1; i <= 3; i++) {
            bytes16 clientId = bytes16(keccak256(abi.encode("client-id", i)));
            STCoordinator.FleetBinding memory b =
                _binding(clientId, keccak256(abi.encode("client-key", i)), 1);
            coordinator.bindFleetMember(b, _signature(i), _signature(i + 10));
            (bool activeBefore,) = coordinator.bindingAt(clientId, 0);
            assertFalse(activeBefore);
            (bool activeAfter, STCoordinator.BindingRecord memory record) = coordinator.bindingAt(clientId, 1);
            assertTrue(activeAfter);
            assertEq(record.uid, 42);
            assertEq(record.fleetId, FLEET);
        }
        assertEq(coordinator.fleetMemberCount(FLEET), 3);
    }

    function test_bindingRejectsReplayWrongCommitmentStaleUidAndBadSignature() public {
        _prepareFleet();
        STCoordinator.FleetBinding memory good = _binding(bytes16(uint128(1)), keccak256("client-key"), 1);
        coordinator.bindFleetMember(good, _signature(1), _signature(2));

        vm.expectRevert(STCoordinator.InvalidBinding.selector);
        coordinator.bindFleetMember(good, _signature(1), _signature(2));

        STCoordinator.FleetBinding memory wrongCommitment =
            _binding(bytes16(uint128(2)), keccak256("client-key-2"), 1);
        wrongCommitment.commitmentHash = keccak256("other");
        vm.expectRevert(STCoordinator.StaleCommitment.selector);
        coordinator.bindFleetMember(wrongCommitment, _signature(1), _signature(2));

        bytes32 unknownHotkey = keccak256("unknown-hotkey");
        vm.prank(oracle);
        coordinator.mirrorCommitment(unknownHotkey, COMMITMENT, uint64(block.number), keccak256("block-2"));
        STCoordinator.FleetBinding memory unknown =
            _binding(bytes16(uint128(3)), keccak256("client-key-3"), 1);
        unknown.hotkey = unknownHotkey;
        vm.expectRevert(STCoordinator.RuntimeIdentityMissing.selector);
        coordinator.bindFleetMember(unknown, _signature(1), _signature(2));

        STCoordinator.FleetBinding memory badSig = _binding(bytes16(uint128(4)), keccak256("client-key-4"), 1);
        bytes32 digest = coordinator.fleetBindingDigest(badSig);
        bytes memory sig = _signature(7);
        ed.setBad(digest, badSig.clientKey, bytes32(uint256(7)), bytes32(uint256(8)), true);
        vm.expectRevert(STCoordinator.InvalidSignature.selector);
        coordinator.bindFleetMember(badSig, sig, _signature(8));
    }

    function test_commitmentMirrorIsMonotonicAndRejectsSameHeightEquivocation() public {
        bytes32 firstBlockHash = keccak256("finalized-block-1000");
        vm.prank(oracle);
        coordinator.mirrorCommitment(HEAD_HOTKEY, COMMITMENT, uint64(block.number), firstBlockHash);

        // An exact retry is idempotent, but finalized history cannot move
        // backward or name two states at one height.
        vm.prank(oracle);
        coordinator.mirrorCommitment(HEAD_HOTKEY, COMMITMENT, uint64(block.number), firstBlockHash);

        vm.prank(oracle);
        vm.expectRevert(STCoordinator.StaleCommitment.selector);
        coordinator.mirrorCommitment(
            HEAD_HOTKEY, COMMITMENT, uint64(block.number - 1), keccak256("older-block")
        );

        vm.prank(oracle);
        vm.expectRevert(STCoordinator.StaleCommitment.selector);
        coordinator.mirrorCommitment(
            HEAD_HOTKEY, keccak256("equivocating-commitment"), uint64(block.number), firstBlockHash
        );

        vm.prank(oracle);
        vm.expectRevert(STCoordinator.StaleCommitment.selector);
        coordinator.mirrorCommitment(
            HEAD_HOTKEY, COMMITMENT, uint64(block.number), keccak256("equivocating-block")
        );

        vm.roll(block.number + 1);
        bytes32 nextCommitment = keccak256("next-commitment");
        bytes32 nextBlockHash = keccak256("finalized-block-1001");
        vm.prank(oracle);
        coordinator.mirrorCommitment(HEAD_HOTKEY, nextCommitment, uint64(block.number), nextBlockHash);
        (bytes32 commitment, bytes32 finalizedHash, uint64 finalizedBlock) =
            coordinator.mirroredCommitments(HEAD_HOTKEY);
        assertEq(commitment, nextCommitment);
        assertEq(finalizedHash, nextBlockHash);
        assertEq(finalizedBlock, block.number);
    }

    function test_clientSignedRevocationIsNextEpochOnly() public {
        _prepareFleet();
        bytes16 clientId = bytes16(uint128(9));
        STCoordinator.FleetBinding memory b = _binding(clientId, keccak256("client-nine"), 1);
        coordinator.bindFleetMember(b, _signature(1), _signature(2));
        coordinator.revokeFleetBinding(clientId, 1, 2, _signature(3));
        (bool activeAtOne,) = coordinator.bindingAt(clientId, 1);
        (bool activeAtTwo,) = coordinator.bindingAt(clientId, 2);
        assertTrue(activeAtOne);
        assertFalse(activeAtTwo);
    }

    function test_bindingAtFailsClosedWhenRuntimeIdentityIsPrunedOrReassigned() public {
        _prepareFleet();
        bytes16 clientId = bytes16(uint128(12));
        STCoordinator.FleetBinding memory binding = _binding(clientId, keccak256("client-twelve"), 1);
        coordinator.bindFleetMember(binding, _signature(1), _signature(2));

        (bool activeBefore,) = coordinator.bindingAt(clientId, 1);
        assertTrue(activeBefore);

        neuron.clearUid(NETUID, HEAD_HOTKEY);
        (bool activeAfterPrune, STCoordinator.BindingRecord memory prunedRecord) =
            coordinator.bindingAt(clientId, 1);
        assertFalse(activeAfterPrune);
        assertEq(prunedRecord.uid, 42);

        neuron.setUid(NETUID, HEAD_HOTKEY, 43);
        (bool activeAfterReassignment, STCoordinator.BindingRecord memory reassignedRecord) =
            coordinator.bindingAt(clientId, 1);
        assertFalse(activeAfterReassignment);
        assertEq(reassignedRecord.uid, 42);

        neuron.setUid(NETUID, HEAD_HOTKEY, 42);
        (bool activeAfterExactRestore,) = coordinator.bindingAt(clientId, 1);
        assertTrue(activeAfterExactRestore);
    }

    function test_futureRotationPreservesPriorGenerationUntilBoundary() public {
        _prepareFleet();
        bytes16 clientId = bytes16(uint128(10));
        STCoordinator.FleetBinding memory first = _binding(clientId, keccak256("client-ten-v1"), 1);
        coordinator.bindFleetMember(first, _signature(1), _signature(2));

        vm.roll(_end(0));
        STCoordinator.FleetBinding memory second = _binding(clientId, keccak256("client-ten-v2"), 2);
        second.validFromEpoch = 9;
        second.validToEpoch = 16;
        coordinator.bindFleetMember(second, _signature(3), _signature(4));

        (bool activeAtOne, STCoordinator.BindingRecord memory atOne) = coordinator.bindingAt(clientId, 1);
        (bool activeAtEight, STCoordinator.BindingRecord memory atEight) = coordinator.bindingAt(clientId, 8);
        (bool activeAtNine, STCoordinator.BindingRecord memory atNine) = coordinator.bindingAt(clientId, 9);
        assertTrue(activeAtOne);
        assertTrue(activeAtEight);
        assertTrue(activeAtNine);
        assertEq(atOne.generation, 1);
        assertEq(atEight.generation, 1);
        assertEq(atNine.generation, 2);
        assertEq(coordinator.bindingVersionCount(clientId), 2);
        assertEq(coordinator.getFleetBinding(clientId).generation, 2);
        assertEq(coordinator.fleetMemberCount(FLEET), 1);
    }

    function test_cleanupAtEpochZeroUsesExplicitMarkerAndUidGeneration() public {
        _prepareFleet();
        bytes16 clientId = bytes16(uint128(11));
        STCoordinator.FleetBinding memory binding = _binding(clientId, keccak256("client-eleven"), 1);
        coordinator.bindFleetMember(binding, _signature(1), _signature(2));

        vm.expectRevert(STCoordinator.InvalidBinding.selector);
        coordinator.cleanupFleetBinding(clientId);

        // The same hotkey at a different UID is a different runtime identity.
        neuron.setUid(NETUID, HEAD_HOTKEY, 43);
        coordinator.cleanupFleetBinding(clientId);
        STCoordinator.BindingRecord memory cleaned = coordinator.getFleetBinding(clientId);
        assertTrue(cleaned.cleaned);
        assertEq(cleaned.cleanedAtEpoch, 0);
        (bool activeAtOne,) = coordinator.bindingAt(clientId, 1);
        assertFalse(activeAtOne);
        assertEq(coordinator.fleetMemberCount(FLEET), 0);

        vm.expectRevert(STCoordinator.InvalidBinding.selector);
        coordinator.cleanupFleetBinding(clientId);
    }

    function test_policySchedulePreservesCurrentEpochAndChangesFutureCadence() public {
        STCoordinator.PolicySnapshot memory next = _policy();
        next.policyHash = keccak256("release-policy-v2");
        next.effectiveEpoch = 2;
        next.epochBlocks = 200;
        vm.prank(owner);
        coordinator.schedulePolicy(next);

        assertEq(coordinator.epochStartBlock(2), START_BLOCK + 2 * EPOCH_BLOCKS);
        vm.roll(START_BLOCK + 2 * EPOCH_BLOCKS - 1);
        assertEq(coordinator.currentEpoch(), 1);
        vm.roll(START_BLOCK + 2 * EPOCH_BLOCKS);
        assertEq(coordinator.currentEpoch(), 2);
        assertEq(coordinator.epochEndBlock(2), START_BLOCK + 2 * EPOCH_BLOCKS + 200);
        assertEq(coordinator.policyAt(1).policyHash, keccak256("release-policy-v1"));
        assertEq(coordinator.policyAt(2).policyHash, keccak256("release-policy-v2"));
    }

    function test_policyScheduleRejectsEffectiveBlockDowncastOverflow() public {
        STCoordinator.PolicySnapshot memory next = _policy();
        next.policyHash = keccak256("overflowing-policy");
        next.effectiveEpoch = type(uint64).max;
        uint256 effectiveBlock = uint256(START_BLOCK) + uint256(type(uint64).max) * uint256(EPOCH_BLOCKS);

        vm.prank(owner);
        vm.expectRevert(
            abi.encodeWithSelector(SafeCast.SafeCastOverflowedUintDowncast.selector, 64, effectiveBlock)
        );
        coordinator.schedulePolicy(next);
    }

    function test_initialPolicyRejectsDeploymentBlockDowncastOverflow() public {
        uint256 overflowingBlock = uint256(type(uint64).max) + 1;
        vm.roll(overflowingBlock);
        STCoordinator freshImplementation = new STCoordinator();
        STCoordinator.PolicySnapshot memory policy = _policy();

        vm.expectRevert(
            abi.encodeWithSelector(SafeCast.SafeCastOverflowedUintDowncast.selector, 64, overflowingBlock)
        );
        new ERC1967Proxy(
            address(freshImplementation),
            abi.encodeCall(
                STCoordinator.initialize,
                (NETUID, owner, guardian, COORD_COLDKEY, vault, sink, oracle, policy)
            )
        );
    }

    function test_rootCommitRejectsBlockNumberDowncastOverflow() public {
        uint256 epoch = (uint256(type(uint64).max) - uint256(START_BLOCK)) / uint256(EPOCH_BLOCKS);
        uint256 epochEnd = uint256(START_BLOCK) + (epoch + 1) * uint256(EPOCH_BLOCKS);
        assertGt(epochEnd, type(uint64).max);
        vm.roll(epochEnd);

        vm.prank(rootSigner1);
        vm.expectRevert(
            abi.encodeWithSelector(SafeCast.SafeCastOverflowedUintDowncast.selector, 64, epochEnd)
        );
        coordinator.commitOperatorRoot(epoch, NO1, keccak256("overflow-root"), keccak256("overflow-artifact"));
    }

    function test_policyScheduleRejectsClaimWindowBelowImmutableVaultMinimum() public {
        STCoordinator.PolicySnapshot memory next = _policy();
        next.policyHash = keccak256("short-claim-policy");
        next.effectiveEpoch = 2;
        next.claimTTLEpochs = 1;
        next.claimGraceEpochs = 0;

        vm.prank(owner);
        vm.expectRevert(STCoordinator.InvalidPolicy.selector);
        coordinator.schedulePolicy(next);
    }

    function test_fleetRevokeDigestRejectsChainIDDowncastOverflow() public {
        STCoordinatorChainIdHarness harness = new STCoordinatorChainIdHarness();
        uint256 overflowingChainId = uint256(type(uint64).max) + 1;
        vm.expectRevert(
            abi.encodeWithSelector(SafeCast.SafeCastOverflowedUintDowncast.selector, 64, overflowingChainId)
        );
        harness.checkedChainId(overflowingChainId);
    }

    function test_releaseProductionCadenceIsExactAndFutureEffective() public {
        STCoordinator.PolicySnapshot memory next = _policy();
        next.effectiveEpoch = 1;
        next.epochBlocks = 50_400;
        next.rootCommitWindowBlocks = 1_200;
        next.finalizeOffsetBlocks = 14_400;
        next.closeGraceBlocks = 120;
        next.commitmentMaxAgeBlocks = 100_800;

        vm.prank(owner);
        coordinator.schedulePolicy(next);

        STCoordinator.PolicySnapshot memory current = coordinator.policyAt(0);
        assertEq(current.epochBlocks, EPOCH_BLOCKS);
        assertEq(current.rootCommitWindowBlocks, ROOT_WINDOW);
        assertEq(current.finalizeOffsetBlocks, FINALIZE_OFFSET);

        STCoordinator.PolicySnapshot memory production = coordinator.policyAt(1);
        assertEq(production.effectiveBlock, START_BLOCK + EPOCH_BLOCKS);
        assertEq(production.epochBlocks, 50_400);
        assertEq(production.rootCommitWindowBlocks, 1_200);
        assertEq(production.finalizeOffsetBlocks, 14_400);
        assertEq(production.closeGraceBlocks, 120);
    }

    function test_operatorRetirementIsFutureEffectiveAndPreservesPriorEpoch() public {
        vm.prank(owner);
        coordinator.scheduleOperator(NO1, DEPOSIT1, depositSigner1, rootSigner1, false, 1);

        assertTrue(coordinator.operatorAt(NO1, 0).active);
        assertFalse(coordinator.operatorAt(NO1, 1).active);
        _deposit(NO1, 10, 0);

        vm.roll(_end(0));
        _pushDeposit(NO1, 10);
        vm.prank(depositSigner1);
        vm.expectRevert(STCoordinator.InactiveOperator.selector);
        coordinator.deposit(NO1, 10, 1, uint64(block.number + 10));

        // Retirement never alters the immutable vault or the ability to close
        // the preceding active epoch.
        coordinator.closeOperatorEpoch(0, NO1);
        assertEq(uint8(vault.entitlement(0, NO1).status), 1);
    }

    function test_guardianPauseCannotUnpauseAndRotationIsFutureEpoch() public {
        address nextGuardian = makeAddr("next-guardian");
        vm.prank(owner);
        coordinator.scheduleGuardian(nextGuardian, 1);

        vm.prank(guardian);
        coordinator.setPaused(true);
        vm.prank(guardian);
        vm.expectRevert(STCoordinator.Unauthorized.selector);
        coordinator.setPaused(false);
        vm.prank(owner);
        coordinator.setPaused(false);

        vm.roll(_end(0));
        assertEq(coordinator.activeGuardian(), nextGuardian);
        vm.prank(nextGuardian);
        coordinator.setPaused(true);
        assertTrue(coordinator.paused());
    }
}
