// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

/// @notice Runtime-475 staking precompile (0x805) views that
///         `src/interfaces/stakingV2.sol` does not vendor, declared as in
///         upstream d1718c99 `precompiles/src/solidity/stakingV2.sol`.
interface IRehearsalStaking {
    function getMinerCollateral(uint16 netuid, bytes32 hotkey, bytes32 coldkey)
        external
        view
        returns (bool exists, uint64 locked, uint128 drainRatio, uint64 minLocked, uint64 earned);

    function getRejectLockedAlpha(bytes32 coldkey) external view returns (bool);
}

/// @title RehearsalProbe
/// @notice Runtime-rehearsal smoke contract (VERSION2-FINAL.md §14 gate 2,
///         `rehearsal/`). It calls a Subtensor precompile as itself, so the
///         runtime acts for this contract's mapped coldkey
///         blake2b-256("evm:" ‖ address), and reports the gas the call used,
///         measured around the call inside this contract. Never deployed on a
///         public chain.
contract RehearsalProbe {
    /// @notice One successful precompile call made by `forward`.
    event Forwarded(address indexed target, uint256 gasUsed, bytes result);
    /// @notice The call value and this contract's balance, as `observeValue` sees them.
    event ValueObserved(uint256 value, uint256 balance);

    /// @notice The precompile refused the call; `result` is its return data.
    error ForwardFailed(bytes result);
    /// @notice `forwardThenRevert` made its call and then reverted.
    error RolledBack();

    /// @notice Native value sent here funds this contract's mapped coldkey.
    receive() external payable {}

    /// @notice Calls `target` with `data`. A refused call reverts the transaction.
    function forward(address target, bytes calldata data) external returns (bytes memory result) {
        uint256 start = gasleft();
        bool ok;
        (ok, result) = target.call(data);
        uint256 gasUsed = start - gasleft();
        if (!ok) revert ForwardFailed(result);
        emit Forwarded(target, gasUsed, result);
    }

    /// @notice Static call for view precompiles, returning the gas it used.
    function forwardView(address target, bytes calldata data)
        external
        view
        returns (bytes memory result, uint256 gasUsed)
    {
        uint256 start = gasleft();
        bool ok;
        (ok, result) = target.staticcall(data);
        gasUsed = start - gasleft();
        if (!ok) revert ForwardFailed(result);
    }

    /// @notice Makes a call that succeeds and then reverts, so its runtime
    ///         effect must roll back with the transaction.
    function forwardThenRevert(address target, bytes calldata data) external {
        (bool ok, bytes memory result) = target.call(data);
        if (!ok) revert ForwardFailed(result);
        revert RolledBack();
    }

    /// @notice Reports the call value and this contract's balance after it.
    function observeValue() external payable {
        emit ValueObserved(msg.value, address(this).balance);
    }
}
