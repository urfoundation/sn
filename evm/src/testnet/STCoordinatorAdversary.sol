// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {STCoordinator} from "../STCoordinator.sol";
import {STReserveSink} from "../STReserveSink.sol";
import {STSettlementVault} from "../STSettlementVault.sol";
import {ISTAKING_ADDRESS} from "../interfaces/stakingV2.sol";

/// @notice Testnet-only hostile UUPS implementation used by sim-testnet after
/// an entitlement has finalized. It deliberately attempts every custody
/// boundary through ordinary calls and records the revert result without
/// making the drill transaction itself revert. This artifact is never part of
/// the production deployment artifact list.
contract STCoordinatorAdversary is STCoordinator {
    bytes32 public constant DRILL_VERSION = keccak256("urnetwork/coordinator-adversary/v1");

    event CustodyProbe(bytes32 indexed probe, bool callSucceeded, bytes32 returnDataHash);

    function runCustodyProbes(
        uint256 epoch,
        uint256 noId,
        bytes32 replacementRoot,
        bytes32 replacementArtifact,
        uint64 replacementExpiry,
        bytes32 reserveDestinationColdkey,
        bytes32 reserveHotkey
    ) external onlyOwner returns (uint256 unexpectedSuccesses) {
        bool ok;
        bytes memory result;

        (ok, result) = address(settlementVault)
            .call(
                abi.encodeCall(
                    STSettlementVault.finalizeEntitlement,
                    (epoch, noId, replacementRoot, replacementArtifact, replacementExpiry)
                )
            );
        unexpectedSuccesses += _record("rewrite-finalized-entitlement", ok, result);

        (ok, result) = address(settlementVault)
            .call(abi.encodeCall(STSettlementVault.setCoordinatorOnce, (address(this))));
        unexpectedSuccesses += _record("reset-vault-coordinator", ok, result);

        (ok, result) =
            address(reserveSink).call(abi.encodeCall(STReserveSink.setRecorderOnce, (address(this))));
        unexpectedSuccesses += _record("reset-reserve-recorder", ok, result);

        // The staking precompile derives the source coldkey from msg.sender.
        // Delegatecall keeps msg.sender at the proxy, so this can never source
        // stake owned by the immutable reserve-sink H160 mapping.
        (ok, result) = ISTAKING_ADDRESS.call(
            abi.encodeWithSignature(
                "transferStake(bytes32,bytes32,uint256,uint256,uint256)",
                reserveDestinationColdkey,
                reserveHotkey,
                uint256(netuid),
                uint256(netuid),
                uint256(1)
            )
        );
        unexpectedSuccesses += _record("source-reserve-principal", ok, result);
    }

    function _record(bytes32 probe, bool ok, bytes memory result) private returns (uint256) {
        emit CustodyProbe(probe, ok, keccak256(result));
        return ok ? 1 : 0;
    }
}
