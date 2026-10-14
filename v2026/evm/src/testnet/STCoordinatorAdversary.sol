// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {OwnableUpgradeable} from "@openzeppelin/contracts-upgradeable/access/OwnableUpgradeable.sol";
import {UUPSUpgradeable} from "@openzeppelin/contracts-upgradeable/proxy/utils/UUPSUpgradeable.sol";
import {StorageSlot} from "@openzeppelin/contracts/utils/StorageSlot.sol";
import {STReserveSink} from "../STReserveSink.sol";
import {STSettlementVault} from "../STSettlementVault.sol";
import {ISTAKING_ADDRESS} from "../interfaces/stakingV2.sol";

/// @notice Testnet-only hostile UUPS implementation used by sim-testnet after
/// an entitlement has finalized. It deliberately attempts every custody
/// boundary through ordinary calls and records the revert result without
/// making the drill transaction itself revert. This artifact is never part of
/// the production deployment artifact list.
contract STCoordinatorAdversary is OwnableUpgradeable, UUPSUpgradeable {
    bytes32 public constant DRILL_VERSION = keccak256("urnetwork/coordinator-adversary/v1");
    bytes32 private constant NETUID_SLOT = bytes32(uint256(0));
    bytes32 private constant SETTLEMENT_VAULT_SLOT = bytes32(uint256(2));
    bytes32 private constant RESERVE_SINK_SLOT = bytes32(uint256(3));

    event CustodyProbe(bytes32 indexed probe, bool callSucceeded, bytes32 returnDataHash);

    /// @custom:oz-upgrades-unsafe-allow constructor
    constructor() {
        _disableInitializers();
    }

    /// @dev The v1 call and event ABI is deliberately stable so an interrupted
    /// testnet campaign can reuse its release-locked drill implementation. The
    /// current compact build declares no linear storage: it reads only the
    /// three coordinator slots whose exact positions are generator- and
    /// upgrade-test-locked. Production never deploys this implementation.
    function runCustodyProbes(
        uint256 epoch,
        uint256 noId,
        bytes32 replacementRoot,
        bytes32 replacementArtifact,
        uint64 replacementExpiry,
        bytes32 reserveDestinationColdkey,
        bytes32 reserveHotkey
    ) external onlyOwner returns (uint256 unexpectedSuccesses) {
        address settlementVault = StorageSlot.getAddressSlot(SETTLEMENT_VAULT_SLOT).value;
        address reserveSink = StorageSlot.getAddressSlot(RESERVE_SINK_SLOT).value;
        uint256 netuid = StorageSlot.getUint256Slot(NETUID_SLOT).value & type(uint16).max;
        bool ok;
        bytes memory result;

        (ok, result) = settlementVault.call(
            abi.encodeCall(
                STSettlementVault.finalizeEntitlement,
                (epoch, noId, replacementRoot, replacementArtifact, replacementExpiry)
            )
        );
        unexpectedSuccesses += _record("rewrite-finalized-entitlement", ok, result);

        (ok, result) =
            settlementVault.call(abi.encodeCall(STSettlementVault.setCoordinatorOnce, (address(this))));
        unexpectedSuccesses += _record("reset-vault-coordinator", ok, result);

        (ok, result) = reserveSink.call(abi.encodeCall(STReserveSink.setRecorderOnce, (address(this))));
        unexpectedSuccesses += _record("reset-reserve-recorder", ok, result);

        (ok, result) = ISTAKING_ADDRESS.call(
            abi.encodeWithSignature(
                "transferStake(bytes32,bytes32,uint256,uint256,uint256)",
                reserveDestinationColdkey,
                reserveHotkey,
                netuid,
                netuid,
                uint256(1)
            )
        );
        unexpectedSuccesses += _record("source-reserve-principal", ok, result);
    }

    function _authorizeUpgrade(address) internal override onlyOwner {}

    function _record(bytes32 probe, bool ok, bytes memory result) private returns (uint256) {
        emit CustodyProbe(probe, ok, keccak256(result));
        return ok ? 1 : 0;
    }
}
