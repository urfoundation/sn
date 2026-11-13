// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

/// @notice Runtime 486's account settings on the staking precompile (0x805;
/// upstream d665b0eb precompiles/src/staking.rs:540-582, 731-742 and
/// 1170-1183, 1595-1612). Each setter dispatches for the caller's own coldkey,
/// the mapping of its address.
///
/// With RejectStakingHotkeyGrowth set, a transfer from another coldkey may
/// land only on a hotkey already on the coldkey's StakingHotkeys list, or one
/// it owns (`AccountRejectsStakingHotkeyGrowth`, upstream
/// pallets/subtensor/src/staking/stake_utils.rs:1567-1586), and a coldkey swap
/// into it may bring no new staking hotkey (swap/swap_coldkey.rs:645-666). The
/// coldkey's own stake operations are unaffected. Every coldkey rejects
/// incoming conviction-locked alpha unless it opts in (staking/lock.rs:499-532);
/// setRejectLockedAlpha(true) keeps that.
///
/// A coldkey's auto-stake destination on a subnet must be a hotkey holding a
/// UID there; it receives the incentive of hotkeys the coldkey owns on that
/// subnet, and it stays on the coldkey's StakingHotkeys list without stake
/// for as long as it is set (staking_hotkey_must_remain, stake_utils.rs:921-
/// 930). Pruning never clears it, and a coldkey swap never overwrites it
/// (swap_coldkey.rs:937-967). Kept apart from stakingV2.sol so that file's
/// bytes, and the v1 bytecode that imports it, stay unchanged.
interface IStakingAccountSettings {
    function setRejectStakingHotkeyGrowth(bool enabled) external;

    function getRejectStakingHotkeyGrowth(bytes32 coldkey) external view returns (bool);

    function setRejectLockedAlpha(bool enabled) external payable;

    function getRejectLockedAlpha(bytes32 coldkey) external view returns (bool);

    function setColdkeyAutoStakeHotkey(uint16 netuid, bytes32 hotkey) external;

    function getAutoStakeDestination(bytes32 coldkey, uint16 netuid) external view returns (bool exists, bytes32 hotkey);
}
