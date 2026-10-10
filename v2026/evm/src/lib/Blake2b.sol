// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

/// @title Subtensor custody mapping and a local EIP-152 hash reference.
///
/// @notice Purpose: compute the Subtensor EVM H160 -> AccountId32 "mirror":
///         `mirror(addr) = blake2b_256("evm:" || addr)` (24-byte message), the
///         Frontier `HashedAddressMapping` used by every subtensor precompile
///         to derive the coldkey of an EVM caller (PLAN.md §3.6, D-10).
///
/// @dev    `mirror` uses the runtime's addressMapping precompile at 0x080c.
///         Runtime 455 maps 0x09 to BN128 addition, not EIP-152. `hash256` is
///         only a single-block reference for local Ethereum/Foundry tooling;
///         it must not be used as a Subtensor runtime hashing primitive.
library Blake2b {
    /// @dev blake2b IV (RFC 7693).
    ///      h[0] is pre-XORed with the parameter block 0x01010020:
    ///      digest_length = 0x20, key_length = 0x00, fanout = 0x01, depth = 0x01.
    uint64 private constant IV0_XORED = 0x6a09e667f3bcc908 ^ 0x01010020;
    uint64 private constant IV1 = 0xbb67ae8584caa73b;
    uint64 private constant IV2 = 0x3c6ef372fe94f82b;
    uint64 private constant IV3 = 0xa54ff53a5f1d36f1;
    uint64 private constant IV4 = 0x510e527fade682d1;
    uint64 private constant IV5 = 0x9b05688c2b3e6c1f;
    uint64 private constant IV6 = 0x1f83d9abfb41bd6b;
    uint64 private constant IV7 = 0x5be0cd19137e2179;

    address private constant BLAKE2F = address(0x09);
    address private constant ADDRESS_MAPPING = address(0x080c);

    /// @notice Local Ethereum EIP-152 reference (unkeyed, data.length <= 128).
    function hash256(bytes memory data) internal view returns (bytes32 digest) {
        require(data.length <= 128, "Blake2b: >1 block");

        // Zero-padded 128-byte message block.
        bytes memory m = new bytes(128);
        for (uint256 i = 0; i < data.length; i++) {
            m[i] = data[i];
        }

        // EIP-152 input: rounds(4B BE) || h(64B, 8x u64 LE) || m(128B) ||
        //                t(16B, 2x u64 LE) || f(1B). Total 213 bytes.
        bytes memory input = abi.encodePacked(
            uint32(12),
            _le64(IV0_XORED),
            _le64(IV1),
            _le64(IV2),
            _le64(IV3),
            _le64(IV4),
            _le64(IV5),
            _le64(IV6),
            _le64(IV7),
            m,
            _le64(uint64(data.length)), // t0 = bytes compressed so far (final block)
            _le64(uint64(0)), // t1
            uint8(1) // final-block flag
        );

        (bool ok, bytes memory out) = BLAKE2F.staticcall(input);
        require(ok && out.length == 64, "Blake2b: blake2f failed");

        // Output = the 8x u64 LE state; the 256-bit digest is its first 32
        // bytes verbatim (h[0..3] little-endian — already the digest encoding).
        assembly ("memory-safe") {
            digest := mload(add(out, 32))
        }
    }

    /// @notice Runtime H160 -> AccountId32 mapping, checked against the
    ///         blake2b_256("evm:" || address) known answer by the probe.
    /// @dev The selector and raw bytes32 result match runtime 455's pinned
    ///      precompiles/src/address_mapping.rs. Missing/malformed calls fail.
    function mirror(address account) internal view returns (bytes32) {
        (bool ok, bytes memory out) =
            ADDRESS_MAPPING.staticcall(abi.encodeWithSignature("addressMapping(address)", account));
        require(ok && out.length == 32, "Blake2b: address mapping failed");
        return abi.decode(out, (bytes32));
    }

    /// @dev uint64 -> little-endian bytes8.
    function _le64(uint64 x) private pure returns (bytes8) {
        uint64 r = ((x & 0x00000000000000FF) << 56) | ((x & 0x000000000000FF00) << 40)
            | ((x & 0x0000000000FF0000) << 24) | ((x & 0x00000000FF000000) << 8)
            | ((x & 0x000000FF00000000) >> 8) | ((x & 0x0000FF0000000000) >> 24)
            | ((x & 0x00FF000000000000) >> 40) | ((x & 0xFF00000000000000) >> 56);
        return bytes8(r);
    }
}
