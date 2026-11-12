// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

/// @notice Runtime 486's account flags on the staking precompile (0x805;
/// upstream d665b0eb precompiles/src/staking.rs:540-582 and 731-742). Each
/// setter dispatches for the caller's own coldkey, the mapping of its address.
///
/// With RejectStakingHotkeyGrowth set, a transfer from another coldkey may
/// land only on a hotkey already on the coldkey's StakingHotkeys list, or one
/// it owns (`AccountRejectsStakingHotkeyGrowth`, upstream
/// pallets/subtensor/src/staking/stake_utils.rs:1567-1586), and a coldkey swap
/// into it may bring no new hotkey (swap/swap_coldkey.rs:645-666). The
/// coldkey's own stake operations are unaffected. Every coldkey rejects
/// incoming conviction-locked alpha unless it opts in (staking/lock.rs:499-532);
/// setRejectLockedAlpha(true) keeps that. Kept apart from stakingV2.sol so that
/// file's bytes, and the v1 bytecode that imports it, stay unchanged.
interface IStakingAccountFlags {
    function setRejectStakingHotkeyGrowth(bool enabled) external;

    function getRejectStakingHotkeyGrowth(bytes32 coldkey) external view returns (bool);

    function setRejectLockedAlpha(bool enabled) external payable;

    function getRejectLockedAlpha(bytes32 coldkey) external view returns (bool);
}
