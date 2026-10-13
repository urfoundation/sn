// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

/// @notice Helpers for Subtensor's reducible native-balance EVM semantics.
library NativeBalance {
    /// @notice Reconstructs the reducible balance that preceded a payable call.
    /// @dev Runtime 452 implements BALANCE with `reducible_balance` and
    ///      `Preservation::Preserve`. A first transfer to an account therefore
    ///      reports less than `suppliedValue` because the existential deposit is
    ///      not reducible. Saturation represents the correct zero baseline and
    ///      also preserves an existing reducible balance on subsequent calls.
    function beforeSuppliedValue(uint256 currentBalance, uint256 suppliedValue)
        internal
        pure
        returns (uint256)
    {
        if (currentBalance <= suppliedValue) return 0;
        return currentBalance - suppliedValue;
    }
}
