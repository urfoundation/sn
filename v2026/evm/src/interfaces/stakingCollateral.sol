// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

/// @notice Runtime 475's miner registration collateral view on the staking
/// precompile (0x805; upstream d1718c99 precompiles/src/staking.rs:1720-1745).
/// When a subnet sets a nonzero collateral lock share, registration stakes part
/// of the price to the registered hotkey and locks it on the registering
/// coldkey's position; only emission earned by that hotkey releases it, and a
/// move or transfer that would dig into it fails `StakeUnavailable`. getStake
/// reports the position including the lock. Kept apart from stakingV2.sol so
/// that file's bytes, and the v1 bytecode that imports it, stay unchanged.
interface IStakingCollateral {
    function getMinerCollateral(uint16 netuid, bytes32 hotkey, bytes32 coldkey)
        external
        view
        returns (bool exists, uint64 locked, uint128 drainRatio, uint64 minLocked, uint64 earned);
}
