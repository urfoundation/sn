// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

/// @title Blake2b — minimal single-block blake2b-256 via the EIP-152 `blake2f`
///        compression-function precompile (address 0x09, Istanbul+).
///
/// @notice Purpose: compute the Subtensor EVM H160 -> AccountId32 "mirror":
///         `mirror(addr) = blake2b_256("evm:" || addr)` (24-byte message), the
///         Frontier `HashedAddressMapping` used by every subtensor precompile
///         to derive the coldkey of an EVM caller (PLAN.md §3.6, D-10).
///
/// @dev    Only messages of <= 128 bytes (one compression block) are supported —
///         that is all the mirror needs. Whether 0x09 exists on the subtensor
///         runtime is UNVERIFIED (SP-1); callers must treat failure as
///         "on-chain mirror unavailable" and fall back to the owner-gated path.
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

    /// @notice blake2b-256 of `data` (data.length <= 128; unkeyed).
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

    /// @notice Subtensor/Frontier H160 -> AccountId32 mirror:
    ///         blake2b_256("evm:" || address).
    function mirror(address account) internal view returns (bytes32) {
        return hash256(abi.encodePacked(bytes4(0x65766d3a), account)); // "evm:"
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
