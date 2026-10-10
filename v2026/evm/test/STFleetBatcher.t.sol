// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {ReleaseBase} from "./utils/ReleaseBase.sol";
import {STCoordinator} from "../src/STCoordinator.sol";
import {STFleetBatcher} from "../src/STFleetBatcher.sol";
import {SafeCast} from "@openzeppelin/contracts/utils/math/SafeCast.sol";

contract STFleetBatcherTest is ReleaseBase {
    event FleetMemberBound(bytes32 indexed fleetId, bytes16 indexed clientId, uint64 generation, uint16 uid);

    bytes32 internal constant FLEET = keccak256("batch-fleet");
    bytes32 internal constant HOTKEY = keccak256("batch-hotkey");
    bytes32 internal constant FIRST_COMMITMENT = keccak256("generation-one-manifest");
    bytes32 internal constant SECOND_COMMITMENT = keccak256("generation-two-manifest");
    bytes16 internal constant CLIENT_ONE = bytes16(uint128(1));
    bytes16 internal constant CLIENT_TWO = bytes16(uint128(2));
    bytes16 internal constant REVOKE_CLIENT = hex"33333333333333333333333333333333";
    bytes32 internal constant KEY_ONE = keccak256("batch-client-one");
    bytes32 internal constant KEY_TWO = keccak256("batch-client-two");

    STFleetBatcher internal batcher;

    function setUp() public override {
        super.setUp();
        batcher = new STFleetBatcher(coordinator, oracle);
    }

    function _signature(uint256 value) internal pure returns (bytes memory) {
        return abi.encodePacked(bytes32(value), bytes32(value + 1));
    }

    function _binding(
        bytes16 clientId,
        bytes32 clientKey,
        uint64 generation,
        uint64 validFrom,
        uint64 validTo,
        bytes32 commitment
    ) internal view returns (STCoordinator.FleetBinding memory) {
        return STCoordinator.FleetBinding({
            chainId: uint64(block.chainid),
            netuid: NETUID,
            coordinator: address(coordinator),
            fleetId: FLEET,
            hotkey: HOTKEY,
            clientId: clientId,
            clientKey: clientKey,
            generation: generation,
            validFromEpoch: validFrom,
            validToEpoch: validTo,
            commitmentHash: commitment
        });
    }

    function _installGenerationOne() internal {
        neuron.setUid(NETUID, HOTKEY, 42);
        vm.prank(oracle);
        coordinator.mirrorCommitment(
            HOTKEY, FIRST_COMMITMENT, uint64(block.number), keccak256("generation-one-block")
        );
        coordinator.bindFleetMember(
            _binding(CLIENT_ONE, KEY_ONE, 1, 1, 8, FIRST_COMMITMENT), _signature(1), _signature(2)
        );
        coordinator.bindFleetMember(
            _binding(CLIENT_TWO, KEY_TWO, 1, 1, 8, FIRST_COMMITMENT), _signature(3), _signature(4)
        );
    }

    function _activateBatcher() internal {
        vm.prank(owner);
        coordinator.scheduleCommitmentOracle(address(batcher), 1);
        vm.roll(_end(0));
        assertEq(coordinator.currentEpoch(), 1);
        assertEq(coordinator.activeCommitmentOracle(), address(batcher));
    }

    function _refresh(uint256 members) internal view returns (STFleetBatcher.FleetRefresh[] memory fleets) {
        STFleetBatcher.MemberRefresh[] memory replacements = new STFleetBatcher.MemberRefresh[](members);
        if (members > 0) {
            replacements[0] = STFleetBatcher.MemberRefresh({
                priorGeneration: 1,
                binding: _binding(CLIENT_ONE, KEY_ONE, 2, 2, 9, SECOND_COMMITMENT),
                revokeSignature: _signature(11),
                clientSignature: _signature(12),
                hotkeySignature: _signature(13)
            });
        }
        if (members > 1) {
            replacements[1] = STFleetBatcher.MemberRefresh({
                priorGeneration: 1,
                binding: _binding(CLIENT_TWO, KEY_TWO, 2, 2, 9, SECOND_COMMITMENT),
                revokeSignature: _signature(21),
                clientSignature: _signature(22),
                hotkeySignature: _signature(23)
            });
        }
        fleets = new STFleetBatcher.FleetRefresh[](1);
        fleets[0] = STFleetBatcher.FleetRefresh({
            hotkey: HOTKEY,
            commitmentHash: SECOND_COMMITMENT,
            finalizedBlock: uint64(block.number),
            finalizedBlockHash: keccak256("generation-two-block"),
            members: replacements
        });
    }

    function _install(uint256 members) internal view returns (STFleetBatcher.FleetRefresh[] memory fleets) {
        STFleetBatcher.MemberRefresh[] memory installs = new STFleetBatcher.MemberRefresh[](members);
        if (members > 0) {
            installs[0] = STFleetBatcher.MemberRefresh({
                priorGeneration: 0,
                binding: _binding(CLIENT_ONE, KEY_ONE, 1, 2, 9, FIRST_COMMITMENT),
                revokeSignature: bytes(""),
                clientSignature: _signature(1),
                hotkeySignature: _signature(2)
            });
        }
        if (members > 1) {
            installs[1] = STFleetBatcher.MemberRefresh({
                priorGeneration: 0,
                binding: _binding(CLIENT_TWO, KEY_TWO, 1, 2, 9, FIRST_COMMITMENT),
                revokeSignature: bytes(""),
                clientSignature: _signature(3),
                hotkeySignature: _signature(4)
            });
        }
        fleets = new STFleetBatcher.FleetRefresh[](1);
        fleets[0] = STFleetBatcher.FleetRefresh({
            hotkey: HOTKEY,
            commitmentHash: FIRST_COMMITMENT,
            finalizedBlock: uint64(block.number),
            finalizedBlockHash: keccak256("generation-one-block"),
            members: installs
        });
    }

    function _boundedBatch(uint256 fleetCount, uint256 memberCount, uint64 generation)
        internal
        view
        returns (STFleetBatcher.FleetRefresh[] memory fleets)
    {
        fleets = new STFleetBatcher.FleetRefresh[](fleetCount);
        for (uint256 fleetIndex; fleetIndex < fleetCount; fleetIndex++) {
            bytes32 fleetId = keccak256(abi.encodePacked("bounded-fleet", fleetIndex));
            bytes32 hotkey = keccak256(abi.encodePacked("bounded-hotkey", fleetIndex));
            bytes32 commitment = keccak256(abi.encodePacked("bounded-commitment", generation, fleetIndex));
            STFleetBatcher.MemberRefresh[] memory members = new STFleetBatcher.MemberRefresh[](memberCount);
            for (uint256 memberIndex; memberIndex < memberCount; memberIndex++) {
                uint256 identity = fleetIndex * memberCount + memberIndex + 1;
                members[memberIndex] = STFleetBatcher.MemberRefresh({
                    priorGeneration: generation - 1,
                    binding: STCoordinator.FleetBinding({
                        chainId: uint64(block.chainid),
                        netuid: NETUID,
                        coordinator: address(coordinator),
                        fleetId: fleetId,
                        hotkey: hotkey,
                        clientId: bytes16(SafeCast.toUint128(identity)),
                        clientKey: keccak256(abi.encodePacked("bounded-client", identity)),
                        generation: generation,
                        validFromEpoch: generation + 1,
                        validToEpoch: generation + 8,
                        commitmentHash: commitment
                    }),
                    revokeSignature: generation == 1 ? bytes("") : _signature(identity + 1000),
                    clientSignature: _signature(identity + 2000),
                    hotkeySignature: _signature(identity + 3000)
                });
            }
            fleets[fleetIndex] = STFleetBatcher.FleetRefresh({
                hotkey: hotkey,
                commitmentHash: commitment,
                finalizedBlock: uint64(block.number),
                finalizedBlockHash: keccak256(abi.encodePacked("bounded-block", generation, fleetIndex)),
                members: members
            });
        }
    }

    function test_installAtomicallyMirrorsAndDualSignsGenerationOne() public {
        neuron.setUid(NETUID, HOTKEY, 42);
        _activateBatcher();

        vm.expectEmit(true, true, false, true, address(batcher));
        emit FleetMemberBound(FLEET, CLIENT_ONE, 1, 42);
        vm.expectEmit(true, true, false, true, address(batcher));
        emit FleetMemberBound(FLEET, CLIENT_TWO, 1, 42);
        vm.prank(oracle);
        batcher.install(_install(2));

        (bytes32 commitment, bytes32 finalizedHash, uint64 finalizedBlock) =
            coordinator.mirroredCommitments(HOTKEY);
        assertEq(commitment, FIRST_COMMITMENT);
        assertEq(finalizedHash, keccak256("generation-one-block"));
        assertEq(finalizedBlock, block.number);
        assertEq(coordinator.bindingVersionCount(CLIENT_ONE), 1);
        assertEq(coordinator.bindingVersionCount(CLIENT_TWO), 1);
        (bool active, STCoordinator.BindingRecord memory first) = coordinator.bindingAt(CLIENT_ONE, 2);
        assertTrue(active);
        assertEq(first.generation, 1);
        assertEq(first.commitmentHash, FIRST_COMMITMENT);
        assertEq(coordinator.fleetMemberCount(FLEET), 2);
    }

    function test_installRejectsRefreshFieldsAndNonGenerationOneBindings() public {
        neuron.setUid(NETUID, HOTKEY, 42);
        _activateBatcher();
        STFleetBatcher.FleetRefresh[] memory fleets = _install(2);
        fleets[0].members[0].priorGeneration = 1;

        vm.prank(oracle);
        vm.expectRevert(STFleetBatcher.InvalidBatch.selector);
        batcher.install(fleets);

        fleets = _install(2);
        fleets[0].members[0].binding.generation = 2;
        vm.prank(oracle);
        vm.expectRevert(STFleetBatcher.InvalidBatch.selector);
        batcher.install(fleets);
    }

    function test_installEnforcesAuthorizationAndHardBatchBounds() public {
        neuron.setUid(NETUID, HOTKEY, 42);
        _activateBatcher();

        vm.prank(stranger);
        vm.expectRevert(STFleetBatcher.Unauthorized.selector);
        batcher.install(_install(2));

        STFleetBatcher.FleetRefresh[] memory empty = new STFleetBatcher.FleetRefresh[](0);
        vm.prank(oracle);
        vm.expectRevert(STFleetBatcher.InvalidBatch.selector);
        batcher.install(empty);

        STFleetBatcher.FleetRefresh[] memory tooManyFleets =
            new STFleetBatcher.FleetRefresh[](batcher.MAX_FLEETS_PER_BATCH() + 1);
        vm.prank(oracle);
        vm.expectRevert(STFleetBatcher.InvalidBatch.selector);
        batcher.install(tooManyFleets);

        STFleetBatcher.FleetRefresh[] memory tooManyMembers = _install(batcher.MAX_MEMBERS_PER_FLEET() + 1);
        vm.prank(oracle);
        vm.expectRevert(STFleetBatcher.InvalidBatch.selector);
        batcher.install(tooManyMembers);

        STFleetBatcher.FleetRefresh[] memory zeroFinalizedBlock = _install(2);
        zeroFinalizedBlock[0].finalizedBlock = 0;
        vm.prank(oracle);
        vm.expectRevert(STFleetBatcher.InvalidBatch.selector);
        batcher.install(zeroFinalizedBlock);
    }

    function test_installRollsBackEveryFleetMemberOnOneBadSignature() public {
        neuron.setUid(NETUID, HOTKEY, 42);
        _activateBatcher();
        STFleetBatcher.FleetRefresh[] memory fleets = _install(2);
        STCoordinator.FleetBinding memory bad = fleets[0].members[1].binding;
        ed.setBad(
            coordinator.fleetBindingDigest(bad), bad.clientKey, bytes32(uint256(3)), bytes32(uint256(4)), true
        );

        vm.prank(oracle);
        vm.expectRevert(STCoordinator.InvalidSignature.selector);
        batcher.install(fleets);

        (bytes32 commitment,,) = coordinator.mirroredCommitments(HOTKEY);
        assertEq(commitment, bytes32(0));
        assertEq(coordinator.bindingVersionCount(CLIENT_ONE), 0);
        assertEq(coordinator.bindingVersionCount(CLIENT_TWO), 0);
        assertEq(coordinator.fleetMemberCount(FLEET), 0);
    }

    function test_installRejectsDuplicateFleetAndMemberIdentitiesAtomically() public {
        neuron.setUid(NETUID, HOTKEY, 42);
        _activateBatcher();
        STFleetBatcher.FleetRefresh[] memory one = _install(2);
        STFleetBatcher.FleetRefresh[] memory duplicateFleets = new STFleetBatcher.FleetRefresh[](2);
        duplicateFleets[0] = one[0];
        duplicateFleets[1] = one[0];

        vm.prank(oracle);
        vm.expectRevert(STFleetBatcher.InvalidBatch.selector);
        batcher.install(duplicateFleets);
        assertEq(coordinator.bindingVersionCount(CLIENT_ONE), 0);

        STFleetBatcher.FleetRefresh[] memory duplicateMembers = _install(2);
        duplicateMembers[0].members[1].binding.clientId = CLIENT_ONE;
        vm.prank(oracle);
        vm.expectRevert(STFleetBatcher.InvalidBatch.selector);
        batcher.install(duplicateMembers);
        (bytes32 commitment,,) = coordinator.mirroredCommitments(HOTKEY);
        assertEq(commitment, bytes32(0));
        assertEq(coordinator.bindingVersionCount(CLIENT_ONE), 0);

        STFleetBatcher.FleetRefresh[] memory crossFleetMembers = _boundedBatch(2, 1, 1);
        for (uint256 fleetIndex; fleetIndex < crossFleetMembers.length; fleetIndex++) {
            neuron.setUid(NETUID, crossFleetMembers[fleetIndex].hotkey, SafeCast.toUint16(fleetIndex + 1));
        }
        crossFleetMembers[1].members[0].binding.clientKey = crossFleetMembers[0].members[0].binding.clientKey;
        vm.prank(oracle);
        vm.expectRevert(STFleetBatcher.InvalidBatch.selector);
        batcher.install(crossFleetMembers);
        assertEq(coordinator.bindingVersionCount(crossFleetMembers[0].members[0].binding.clientId), 0);

        crossFleetMembers = _boundedBatch(2, 1, 1);
        crossFleetMembers[1].members[0].binding.clientId = crossFleetMembers[0].members[0].binding.clientId;
        vm.prank(oracle);
        vm.expectRevert(STFleetBatcher.InvalidBatch.selector);
        batcher.install(crossFleetMembers);
        assertEq(coordinator.bindingVersionCount(crossFleetMembers[0].members[0].binding.clientId), 0);
    }

    function test_maximumTenByFourInstallAndRefreshRemainWithinApprovedGasCaps() public {
        uint256 fleetCount = batcher.MAX_FLEETS_PER_BATCH();
        uint256 memberCount = batcher.MAX_MEMBERS_PER_FLEET();
        for (uint256 fleetIndex; fleetIndex < fleetCount; fleetIndex++) {
            bytes32 hotkey = keccak256(abi.encodePacked("bounded-hotkey", fleetIndex));
            neuron.setUid(NETUID, hotkey, SafeCast.toUint16(fleetIndex + 1));
        }
        _activateBatcher();

        uint256 gasBefore = gasleft();
        vm.prank(oracle);
        batcher.install(_boundedBatch(fleetCount, memberCount, 1));
        uint256 installGas = gasBefore - gasleft();
        assertLt(installGas, 18_000_000);

        vm.roll(_end(1));
        gasBefore = gasleft();
        vm.prank(oracle);
        batcher.refresh(_boundedBatch(fleetCount, memberCount, 2));
        uint256 refreshGas = gasBefore - gasleft();
        assertLt(refreshGas, 24_000_000);

        assertEq(coordinator.bindingVersionCount(bytes16(uint128(1))), 2);
        assertEq(coordinator.bindingVersionCount(bytes16(SafeCast.toUint128(fleetCount * memberCount))), 2);
    }

    function test_refreshAtomicallyMirrorsRevokesAndDualSignsSuccessors() public {
        _installGenerationOne();
        _activateBatcher();

        vm.prank(oracle);
        batcher.refresh(_refresh(2));

        (bytes32 commitment, bytes32 finalizedHash, uint64 finalizedBlock) =
            coordinator.mirroredCommitments(HOTKEY);
        assertEq(commitment, SECOND_COMMITMENT);
        assertEq(finalizedHash, keccak256("generation-two-block"));
        assertEq(finalizedBlock, block.number);
        assertEq(coordinator.bindingVersionCount(CLIENT_ONE), 2);
        assertEq(coordinator.bindingVersionCount(CLIENT_TWO), 2);
        assertEq(coordinator.bindingVersionAt(CLIENT_ONE, 0).validToEpoch, 1);
        assertEq(coordinator.bindingVersionAt(CLIENT_TWO, 0).validToEpoch, 1);
        (bool firstActive, STCoordinator.BindingRecord memory first) = coordinator.bindingAt(CLIENT_ONE, 1);
        (bool secondActive, STCoordinator.BindingRecord memory second) = coordinator.bindingAt(CLIENT_ONE, 2);
        assertTrue(firstActive);
        assertTrue(secondActive);
        assertEq(first.generation, 1);
        assertEq(second.generation, 2);
        assertEq(second.commitmentHash, SECOND_COMMITMENT);
        assertEq(coordinator.fleetMemberCount(FLEET), 2);
    }

    function test_refreshRejectsAnyoneExceptTheImmutableOracle() public {
        _installGenerationOne();
        _activateBatcher();

        vm.prank(stranger);
        vm.expectRevert(STFleetBatcher.Unauthorized.selector);
        batcher.refresh(_refresh(2));
    }

    function test_refreshRejectsCrossFleetOrCrossCommitmentMembers() public {
        _installGenerationOne();
        _activateBatcher();
        STFleetBatcher.FleetRefresh[] memory fleets = _refresh(2);
        fleets[0].members[1].binding.hotkey = keccak256("foreign-hotkey");

        vm.prank(oracle);
        vm.expectRevert(STFleetBatcher.InvalidBatch.selector);
        batcher.refresh(fleets);

        fleets = _refresh(2);
        fleets[0].members[1].binding.commitmentHash = FIRST_COMMITMENT;
        vm.prank(oracle);
        vm.expectRevert(STFleetBatcher.InvalidBatch.selector);
        batcher.refresh(fleets);
    }

    function test_refreshRollsBackMirrorAndEveryMemberOnOneBadSignature() public {
        _installGenerationOne();
        _activateBatcher();
        STFleetBatcher.FleetRefresh[] memory fleets = _refresh(2);
        STCoordinator.FleetBinding memory bad = fleets[0].members[1].binding;
        ed.setBad(
            coordinator.fleetBindingDigest(bad),
            bad.clientKey,
            bytes32(uint256(22)),
            bytes32(uint256(23)),
            true
        );

        vm.prank(oracle);
        vm.expectRevert(STCoordinator.InvalidSignature.selector);
        batcher.refresh(fleets);

        (bytes32 commitment,,) = coordinator.mirroredCommitments(HOTKEY);
        assertEq(commitment, FIRST_COMMITMENT);
        assertEq(coordinator.bindingVersionCount(CLIENT_ONE), 1);
        assertEq(coordinator.bindingVersionCount(CLIENT_TWO), 1);
        assertEq(coordinator.bindingVersionAt(CLIENT_ONE, 0).validToEpoch, 8);
        assertEq(coordinator.bindingVersionAt(CLIENT_TWO, 0).validToEpoch, 8);
        assertEq(coordinator.fleetMemberCount(FLEET), 2);
    }

    function test_refreshEnforcesHardFleetAndMemberBatchBounds() public {
        _activateBatcher();
        STFleetBatcher.FleetRefresh[] memory empty = new STFleetBatcher.FleetRefresh[](0);
        vm.prank(oracle);
        vm.expectRevert(STFleetBatcher.InvalidBatch.selector);
        batcher.refresh(empty);

        STFleetBatcher.FleetRefresh[] memory tooManyFleets =
            new STFleetBatcher.FleetRefresh[](batcher.MAX_FLEETS_PER_BATCH() + 1);
        vm.prank(oracle);
        vm.expectRevert(STFleetBatcher.InvalidBatch.selector);
        batcher.refresh(tooManyFleets);

        STFleetBatcher.FleetRefresh[] memory tooManyMembers = _refresh(batcher.MAX_MEMBERS_PER_FLEET() + 1);
        vm.prank(oracle);
        vm.expectRevert(STFleetBatcher.InvalidBatch.selector);
        batcher.refresh(tooManyMembers);
    }

    function test_constructorRejectsMissingCoordinatorCodeAndOracle() public {
        vm.expectRevert(STFleetBatcher.InvalidConfiguration.selector);
        new STFleetBatcher(STCoordinator(payable(address(0))), oracle);

        vm.expectRevert(STFleetBatcher.InvalidConfiguration.selector);
        new STFleetBatcher(STCoordinator(payable(stranger)), oracle);

        vm.expectRevert(STFleetBatcher.InvalidConfiguration.selector);
        new STFleetBatcher(coordinator, address(0));
    }

    function test_fleetRevokeDigestUsesEveryPackedDomainCoordinate() public {
        vm.chainId(945);
        bytes32 digest = coordinator.fleetRevokeDigest(REVOKE_CLIENT, uint64(3), uint64(11));
        bytes32 expected = keccak256(
            abi.encodePacked(
                bytes("urnetwork/fleet-revoke/v1"),
                uint64(945),
                NETUID,
                address(coordinator),
                REVOKE_CLIENT,
                uint64(3),
                uint64(11)
            )
        );
        assertEq(digest, expected);
    }
}
