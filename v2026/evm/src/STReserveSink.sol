// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {IStaking, ISTAKING_ADDRESS} from "./interfaces/stakingV2.sol";

/// @title STReserveSink
/// @notice Immutable, one-way custody endpoint for demand deposits and
/// conviction. Stake is transferred to this contract's Substrate coldkey on a
/// single configured reserve hotkey. This bytecode has no operation that can
/// remove, move, transfer, proxy, delegatecall, or otherwise source that stake.
contract STReserveSink {
    uint16 public immutable netuid;
    bytes32 public immutable reserveHotkey;
    bytes32 public immutable selfColdkey;
    address public immutable bootstrap;

    address public recorder;
    uint256 public principal;
    mapping(uint256 noId => uint256 amount) public operatorPrincipal;

    event RecorderFixed(address indexed recorder);
    event ReservePrincipalAdded(
        uint256 indexed epoch,
        uint256 indexed noId,
        uint256 amount,
        uint256 operatorPrincipal,
        uint256 totalPrincipal,
        uint256 liveStake
    );

    error Unauthorized();
    error AlreadyInitialized();
    error InvalidConfiguration();
    error ReserveUnderfunded();

    constructor(uint16 netuid_, bytes32 reserveHotkey_, bytes32 selfColdkey_, address bootstrap_) {
        if (
            netuid_ == 0 || reserveHotkey_ == bytes32(0) || selfColdkey_ == bytes32(0)
                || bootstrap_ == address(0)
        ) revert InvalidConfiguration();
        netuid = netuid_;
        reserveHotkey = reserveHotkey_;
        selfColdkey = selfColdkey_;
        bootstrap = bootstrap_;
    }

    /// @notice Permanently fixes the coordinator proxy allowed to record
    /// already-transferred principal. It can be called only once.
    function setRecorderOnce(address recorder_) external {
        if (msg.sender != bootstrap) revert Unauthorized();
        if (recorder != address(0)) revert AlreadyInitialized();
        if (recorder_ == address(0) || recorder_.code.length == 0) revert InvalidConfiguration();
        recorder = recorder_;
        emit RecorderFixed(recorder_);
    }

    /// @notice Accounts a transfer only after the runtime reports enough
    /// stake at the immutable sink identity. A compromised coordinator can at
    /// worst under-report principal; it cannot source reserve stake.
    function recordPrincipal(uint256 epoch, uint256 noId, uint256 amount) external {
        if (msg.sender != recorder) revert Unauthorized();
        if (noId == 0 || amount == 0) revert InvalidConfiguration();
        uint256 nextPrincipal = principal + amount;
        uint256 live = IStaking(ISTAKING_ADDRESS).getStake(reserveHotkey, selfColdkey, uint256(netuid));
        if (live < nextPrincipal) revert ReserveUnderfunded();
        principal = nextPrincipal;
        operatorPrincipal[noId] += amount;
        emit ReservePrincipalAdded(epoch, noId, amount, operatorPrincipal[noId], nextPrincipal, live);
    }

    function liveStake() external view returns (uint256) {
        return IStaking(ISTAKING_ADDRESS).getStake(reserveHotkey, selfColdkey, uint256(netuid));
    }
}
