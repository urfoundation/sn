// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {STCoordinator} from "./STCoordinator.sol";

/// @notice Testnet migration helper for atomically replacing short-lived fleet
/// commitments and bindings without weakening either client or hotkey consent.
/// The coordinator owner must temporarily schedule this contract as the active
/// commitment oracle; only the immutable original oracle may submit batches.
contract STFleetBatcher {
    uint256 public constant MAX_FLEETS_PER_BATCH = 10;
    uint256 public constant MAX_MEMBERS_PER_FLEET = 4;

    struct MemberRefresh {
        uint64 priorGeneration;
        STCoordinator.FleetBinding binding;
        bytes revokeSignature;
        bytes clientSignature;
        bytes hotkeySignature;
    }

    struct FleetRefresh {
        bytes32 hotkey;
        bytes32 commitmentHash;
        uint64 finalizedBlock;
        bytes32 finalizedBlockHash;
        MemberRefresh[] members;
    }

    STCoordinator public immutable coordinator;
    address public immutable oracle;

    event FleetRefreshed(bytes32 indexed hotkey, bytes32 indexed commitmentHash, uint256 members);
    event FleetInstalled(bytes32 indexed hotkey, bytes32 indexed commitmentHash, uint256 members);
    event FleetMemberBound(bytes32 indexed fleetId, bytes16 indexed clientId, uint64 generation, uint16 uid);

    error Unauthorized();
    error InvalidConfiguration();
    error InvalidBatch();

    constructor(STCoordinator coordinator_, address oracle_) {
        if (
            address(coordinator_) == address(0) || address(coordinator_).code.length == 0
                || oracle_ == address(0)
        ) {
            revert InvalidConfiguration();
        }
        coordinator = coordinator_;
        oracle = oracle_;
    }

    /// @notice Install fresh generation-1 fleets after one finalized native
    /// commitment per hotkey. This removes hundreds of testnet finality waits
    /// without changing either signature or coordinator validation.
    function install(FleetRefresh[] calldata fleets) external {
        if (msg.sender != oracle) revert Unauthorized();
        if (fleets.length == 0 || fleets.length > MAX_FLEETS_PER_BATCH) revert InvalidBatch();

        for (uint256 fleetIndex; fleetIndex < fleets.length; fleetIndex++) {
            FleetRefresh calldata fleet = fleets[fleetIndex];
            if (
                fleet.hotkey == bytes32(0) || fleet.commitmentHash == bytes32(0) || fleet.finalizedBlock == 0
                    || fleet.finalizedBlockHash == bytes32(0) || fleet.members.length == 0
                    || fleet.members.length > MAX_MEMBERS_PER_FLEET
            ) revert InvalidBatch();

            bytes32 fleetId = fleet.members[0].binding.fleetId;
            _validateUniqueFleet(fleets, fleetIndex, fleetId);
            coordinator.mirrorCommitment(
                fleet.hotkey, fleet.commitmentHash, fleet.finalizedBlock, fleet.finalizedBlockHash
            );
            for (uint256 memberIndex; memberIndex < fleet.members.length; memberIndex++) {
                MemberRefresh calldata member = fleet.members[memberIndex];
                _validateUniqueMember(fleets, fleetIndex, memberIndex);
                if (
                    member.priorGeneration != 0 || member.revokeSignature.length != 0
                        || member.binding.generation != 1 || member.binding.hotkey != fleet.hotkey
                        || member.binding.commitmentHash != fleet.commitmentHash
                        || member.binding.fleetId != fleetId || member.binding.validFromEpoch == 0
                ) revert InvalidBatch();

                uint16 uid = coordinator.bindFleetMember(
                    member.binding, member.clientSignature, member.hotkeySignature
                );
                emit FleetMemberBound(fleetId, member.binding.clientId, member.binding.generation, uid);
            }
            emit FleetInstalled(fleet.hotkey, fleet.commitmentHash, fleet.members.length);
        }
    }

    /// @notice Mirror each finalized native commitment, revoke every prior
    /// generation at the replacement boundary, and install its dual-signed
    /// successor. Any malformed member or failed signature reverts the batch.
    function refresh(FleetRefresh[] calldata fleets) external {
        if (msg.sender != oracle) revert Unauthorized();
        if (fleets.length == 0 || fleets.length > MAX_FLEETS_PER_BATCH) revert InvalidBatch();

        for (uint256 fleetIndex; fleetIndex < fleets.length; fleetIndex++) {
            FleetRefresh calldata fleet = fleets[fleetIndex];
            if (
                fleet.hotkey == bytes32(0) || fleet.commitmentHash == bytes32(0) || fleet.finalizedBlock == 0
                    || fleet.finalizedBlockHash == bytes32(0) || fleet.members.length == 0
                    || fleet.members.length > MAX_MEMBERS_PER_FLEET
            ) revert InvalidBatch();

            bytes32 fleetId = fleet.members[0].binding.fleetId;
            _validateUniqueFleet(fleets, fleetIndex, fleetId);
            coordinator.mirrorCommitment(
                fleet.hotkey, fleet.commitmentHash, fleet.finalizedBlock, fleet.finalizedBlockHash
            );
            for (uint256 memberIndex; memberIndex < fleet.members.length; memberIndex++) {
                MemberRefresh calldata member = fleet.members[memberIndex];
                _validateUniqueMember(fleets, fleetIndex, memberIndex);
                if (
                    member.priorGeneration == 0 || member.priorGeneration == type(uint64).max
                        || member.binding.generation != member.priorGeneration + 1
                        || member.binding.hotkey != fleet.hotkey
                        || member.binding.commitmentHash != fleet.commitmentHash
                        || member.binding.fleetId != fleetId || member.binding.validFromEpoch == 0
                ) revert InvalidBatch();

                coordinator.revokeFleetBinding(
                    member.binding.clientId,
                    member.priorGeneration,
                    member.binding.validFromEpoch,
                    member.revokeSignature
                );
                uint16 uid = coordinator.bindFleetMember(
                    member.binding, member.clientSignature, member.hotkeySignature
                );
                emit FleetMemberBound(fleetId, member.binding.clientId, member.binding.generation, uid);
            }
            emit FleetRefreshed(fleet.hotkey, fleet.commitmentHash, fleet.members.length);
        }
    }

    /// @dev Reject duplicate hotkeys or logical fleet IDs before either can
    /// overwrite a mirror established earlier in the same atomic batch.
    function _validateUniqueFleet(FleetRefresh[] calldata fleets, uint256 fleetIndex, bytes32 fleetId)
        private
        pure
    {
        if (fleetId == bytes32(0)) revert InvalidBatch();
        for (uint256 priorIndex; priorIndex < fleetIndex; priorIndex++) {
            if (
                fleets[priorIndex].hotkey == fleets[fleetIndex].hotkey
                    || fleets[priorIndex].members[0].binding.fleetId == fleetId
            ) revert InvalidBatch();
        }
    }

    /// @dev Reject empty or repeated member identities across the complete
    /// batch before forwarding the current signature to the coordinator.
    function _validateUniqueMember(FleetRefresh[] calldata fleets, uint256 fleetIndex, uint256 memberIndex)
        private
        pure
    {
        STCoordinator.FleetBinding calldata binding = fleets[fleetIndex].members[memberIndex].binding;
        if (binding.clientId == bytes16(0) || binding.clientKey == bytes32(0)) revert InvalidBatch();
        for (uint256 priorIndex; priorIndex < memberIndex; priorIndex++) {
            STCoordinator.FleetBinding calldata prior = fleets[fleetIndex].members[priorIndex].binding;
            if (prior.clientId == binding.clientId || prior.clientKey == binding.clientKey) {
                revert InvalidBatch();
            }
        }
        for (uint256 priorFleetIndex; priorFleetIndex < fleetIndex; priorFleetIndex++) {
            for (
                uint256 priorMemberIndex;
                priorMemberIndex < fleets[priorFleetIndex].members.length;
                priorMemberIndex++
            ) {
                STCoordinator.FleetBinding calldata
                    prior = fleets[priorFleetIndex].members[priorMemberIndex].binding;
                if (prior.clientId == binding.clientId || prior.clientKey == binding.clientKey) {
                    revert InvalidBatch();
                }
            }
        }
    }
}
