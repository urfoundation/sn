// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {ISTSettlementVault2} from "./ISTSettlementVault2.sol";

/// @title ISTSettlement2
/// @notice The external surface of the owner-upgradeable settlement v2
/// coordinator (VERSION2-FINAL.md §7.2). O marks owner-only functions, R the
/// root signer of `operatorAt(noId, epoch)` on the v1 coordinator, A anyone.
/// "Not paused" means the v1 coordinator's `paused()` is false.
interface ISTSettlement2 {
    event AuditorsSet(uint64 indexed version, address[] auditors, uint256 quorum);
    event DistributionApproved(
        uint256 indexed noId,
        uint64 indexed seq,
        address indexed auditor,
        uint64 auditorSetVersion,
        uint256 approvals
    );

    error Unauthorized();
    error InvalidConfiguration();
    error InvalidAuditors();
    error SettlementPaused();
    error InactiveOperator();
    error InvalidEpoch();
    error CadenceTooShort();
    error UnknownPool();
    error InvalidWindow();
    error PendingRootLive();
    error NoPendingRoot();
    error PendingMismatch();
    error ApprovalClosed();
    error AlreadyApproved();
    error QuorumNotReached();
    error NativeRefundFailed();

    /// Proxy only, once.
    function initialize(
        address owner_,
        address v1Coordinator_,
        address vault_,
        address[] calldata auditors_,
        uint256 quorum_
    ) external;

    /// O. 1 <= quorum <= auditors.length <= 16; no zero or duplicate address.
    /// Bumps auditorSetVersion, so earlier approvals stop counting.
    function setAuditors(address[] calldata auditors_, uint256 quorum_) external;

    /// O, payable. The pool's commitment to v2 from `firstEpoch`.
    function registerOperatorPool(uint256 noId, bytes32 hotkey, uint64 maximumBurnRao, uint64 firstEpoch)
        external
        payable
        returns (uint16 uid);

    /// (vault2, vault2 pool hotkey, 2) for a registered pool at or after its
    /// firstEpoch; otherwise (v1 vault, v1 operatorAt pool hotkey, 1).
    function settlementAt(uint256 noId, uint256 epoch)
        external
        view
        returns (address vault_, bytes32 hotkey, uint8 version);

    /// A, not paused, in [end(epoch), end(epoch) + rootCommitWindowBlocks].
    function closeOperatorEpoch(uint256 epoch, uint256 noId) external returns (uint256 amount);

    /// R, not paused, in [end(epoch), end(epoch) + rootCommitWindowBlocks], with
    /// no pending root or block.number >= end(pending.epoch + 1).
    function commitDistribution(
        uint256 epoch,
        uint256 noId,
        uint256 prevRootCount,
        ISTSettlementVault2.Commitment calldata commitment
    ) external returns (uint64 seq);

    /// Auditor; the arguments must equal the pending root; before end(pending.epoch + 1).
    function approveDistribution(uint256 noId, uint64 seq, bytes32 root, bytes32 ledgerHash) external;

    /// A, not paused, with at least `quorum` approvals at the current auditor set version.
    function finalizeDistribution(uint256 noId) external returns (uint256 rootIndex);

    function approvalCount(uint256 noId) external view returns (uint256);
    function hasApproved(uint256 noId, address auditor) external view returns (bool);
    function auditors() external view returns (address[] memory);
    function quorum() external view returns (uint256);
    function auditorSetVersion() external view returns (uint64);
    function vault() external view returns (address);
    function v1Coordinator() external view returns (address);
}
