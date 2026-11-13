// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {ISTSettlementVault2} from "./interfaces/ISTSettlementVault2.sol";
import {LedgerTree} from "./LedgerTree.sol";

/// @title STClaimBatcher
/// @notice Claims many settlement v2 accounts in one transaction, the optional
/// batching helper of VERSION2-FINAL.md §10.2. Each item is one vault2 `claim`
/// called with exactly `gasPerClaim` gas inside try/catch, so a refused item
/// never reverts the others. The runtime consumes all the gas forwarded to a
/// precompile call it refuses; the per-item cap keeps such an item from
/// starving the rest. An item that the gas left can no longer cover, with the
/// batcher's own bookkeeping, is not attempted, and neither is any later one.
/// The batcher has no owner, storage or funds, and it cannot redirect a
/// payment: vault2 pays each leaf's coldkey whoever calls.
contract STClaimBatcher {
    /// @notice The arguments of one vault2 `claim`.
    struct Claim {
        uint256 noId;
        uint256 rootIndex;
        LedgerTree.Account account;
        LedgerTree.ProofNode[] proof;
    }

    /// @notice The gas an item needs beyond its cap for the batcher's own
    /// work: encoding the call, recording its outcome and, after it, reporting
    /// the items left.
    uint256 public constant BOOKKEEPING_GAS = 20_000;

    /// @notice Item `item` paid `amount` to its leaf's coldkey.
    event ClaimPaid(uint256 item, uint256 amount);
    /// @notice Item `item` reverted. `selector` is the first four bytes of its
    /// revert data, zero-padded: zero for a call that ran out of its gas, as
    /// one does when the runtime refuses a precompile call.
    event ClaimRefused(uint256 item, bytes4 selector);
    /// @notice Items from `item` to the last were not attempted.
    event ClaimsNotAttempted(uint256 item);

    /// @notice Calls `vault.claim` for each item in order and returns the
    /// number of items attempted. An item is attempted only while the gas left
    /// covers `gasPerClaim` at the call, which keeps 1/64 of the gas left
    /// (EIP-150), plus BOOKKEEPING_GAS.
    function claimMany(ISTSettlementVault2 vault, Claim[] calldata claims, uint256 gasPerClaim)
        external
        returns (uint256 attempted)
    {
        uint256 needed = gasPerClaim + gasPerClaim / 63 + BOOKKEEPING_GAS;
        for (; attempted < claims.length; ++attempted) {
            if (gasleft() < needed) {
                emit ClaimsNotAttempted(attempted);
                return attempted;
            }
            Claim calldata item = claims[attempted];
            try vault.claim{gas: gasPerClaim}(item.noId, item.rootIndex, item.account, item.proof) returns (
                uint256 amount
            ) {
                emit ClaimPaid(attempted, amount);
            } catch (bytes memory reason) {
                // Only the selector is reported; the cast keeps its four bytes.
                // forge-lint: disable-next-line(unsafe-typecast)
                emit ClaimRefused(attempted, bytes4(reason));
            }
        }
    }
}
