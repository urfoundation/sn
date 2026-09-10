// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {STSubnet} from "../../src/STSubnet.sol";

/// @dev Upgrade-under-fire target: a V2 implementation with genuinely CHANGED
///      logic (not a byte-identical redeploy) but the same storage layout.
///      Changes:
///        - new `version()` surface (proves the new code is live),
///        - `_ed25519Verify` hard-wired to false (kills permissionless
///          registration and future sample proofs — an aggressive future-epoch
///          behavior change),
///        - `_mirror` returns a poisoned constant (future registrations bind
///          differently).
///      The structural invariant under test: none of this can change what a
///      FINALIZED epoch's claims pay, because those amounts derive only from
///      snapshot storage (poolTotal/feePool/effort/roots) held by the proxy.
contract STSubnetV2Mock is STSubnet {
    function version() external pure returns (uint256) {
        return 2;
    }

    function _ed25519Verify(bytes32, bytes32, bytes32, bytes32)
        internal
        view
        virtual
        override
        returns (bool)
    {
        return false;
    }

    function _mirror(address) internal view virtual override returns (bytes32) {
        return keccak256("v2-poisoned-mirror");
    }
}
