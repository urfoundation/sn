// SPDX-License-Identifier: GPL-3.0
pragma solidity ^0.8.0;

address constant ISR25519VERIFY_ADDRESS = 0x0000000000000000000000000000000000000403;

/// @notice Runtime-447 sr25519 signature verifier.
interface ISR25519Verify {
    function verify(bytes32 message, bytes32 publicKey, bytes32 r, bytes32 s) external pure returns (bool);
}
