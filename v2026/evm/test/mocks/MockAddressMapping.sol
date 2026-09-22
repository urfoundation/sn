// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {Blake2b} from "../../src/lib/Blake2b.sol";

/// @dev Models runtime 455's 0x080c ABI using Foundry's Ethereum-only EIP-152
///      reference. Tests that reject 0x09 install explicit 0x080c responses
///      first, so the production mapping route is tested independently.
contract MockAddressMapping {
    function addressMapping(address account) external view returns (bytes32) {
        return Blake2b.hash256(abi.encodePacked(bytes4(0x65766d3a), account));
    }
}
