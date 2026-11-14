// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {Initializable} from "@openzeppelin/contracts-upgradeable/proxy/utils/Initializable.sol";
import {OwnableUpgradeable} from "@openzeppelin/contracts-upgradeable/access/OwnableUpgradeable.sol";
import {UUPSUpgradeable} from "@openzeppelin/contracts-upgradeable/proxy/utils/UUPSUpgradeable.sol";

import {ISTSettlement2} from "./interfaces/ISTSettlement2.sol";
import {ISTSettlementVault2} from "./interfaces/ISTSettlementVault2.sol";
import {STCoordinator} from "./STCoordinator.sol";
import {NativeBalance} from "./NativeBalance.sol";

/// @title STSettlement2
/// @notice Upgradeable settlement v2 coordinator. It holds no funds and only
/// gates the immutable vault2: operators, windows and pause come from the
/// unchanged v1 coordinator, and a pending root finalizes only with a quorum of
/// owner-admitted auditors approving its exact commit.
contract STSettlement2 is ISTSettlement2, Initializable, OwnableUpgradeable, UUPSUpgradeable {
    uint256 private constant MAX_AUDITORS = 16;

    address public v1Coordinator;
    address public vault;
    address[] private _auditors;
    uint256 public quorum;
    uint64 public auditorSetVersion;
    /// @dev Approvals are keyed by the pending root's seq and the auditor set
    /// version, so a replacement root or a new auditor set starts at zero.
    mapping(uint256 noId => mapping(uint64 seq => mapping(uint64 version => uint256 approvals))) private
        _approvalCount;
    mapping(
        uint256 noId => mapping(uint64 seq => mapping(uint64 version => mapping(address auditor => bool)))
    ) private _approved;

    modifier whenNotPaused() {
        if (_v1().paused()) revert SettlementPaused();
        _;
    }

    /// @custom:oz-upgrades-unsafe-allow constructor
    constructor() {
        _disableInitializers();
    }

    function initialize(
        address owner_,
        address v1Coordinator_,
        address vault_,
        address[] calldata auditors_,
        uint256 quorum_
    ) external initializer {
        if (owner_ == address(0) || v1Coordinator_.code.length == 0 || vault_.code.length == 0) {
            revert InvalidConfiguration();
        }
        __Ownable_init(owner_);
        v1Coordinator = v1Coordinator_;
        vault = vault_;
        _setAuditors(auditors_, quorum_);
    }

    /// @dev Only vault2's registration refund is accepted.
    receive() external payable {
        if (msg.sender != vault) revert Unauthorized();
    }

    function setAuditors(address[] calldata auditors_, uint256 quorum_) external onlyOwner {
        _setAuditors(auditors_, quorum_);
    }

    function auditors() external view returns (address[] memory) {
        return _auditors;
    }

    function registerOperatorPool(uint256 noId, bytes32 hotkey, uint64 maximumBurnRao, uint64 firstEpoch)
        external
        payable
        onlyOwner
        returns (uint16 uid)
    {
        STCoordinator v1 = _v1();
        uint256 epoch = v1.currentEpoch();
        if (!v1.operatorAt(noId, epoch).active) revert InactiveOperator();
        if (firstEpoch <= epoch) revert InvalidEpoch();
        ISTSettlementVault2 v = ISTSettlementVault2(vault);
        // A root committed at the end of its window must clear vault2's
        // challenge window before the next epoch ends, when it becomes replaceable.
        STCoordinator.PolicySnapshot memory p = v1.policyAt(firstEpoch);
        if (p.epochBlocks < uint256(p.rootCommitWindowBlocks) + v.CHALLENGE_BLOCKS()) {
            revert CadenceTooShort();
        }
        uint256 balanceBefore = NativeBalance.beforeSuppliedValue(address(this).balance, msg.value);
        uid = v.registerPool{value: msg.value}(noId, hotkey, maximumBurnRao, firstEpoch);
        uint256 current = address(this).balance;
        if (current > balanceBefore) {
            (bool refunded,) = payable(msg.sender).call{value: current - balanceBefore}("");
            if (!refunded) revert NativeRefundFailed();
        }
    }

    function settlementAt(uint256 noId, uint256 epoch)
        external
        view
        returns (address vault_, bytes32 hotkey, uint8 version)
    {
        ISTSettlementVault2.Pool memory pool = ISTSettlementVault2(vault).pools(noId);
        if (pool.registered && epoch >= pool.firstEpoch) return (vault, pool.hotkey, 2);
        STCoordinator v1 = _v1();
        return (address(v1.settlementVault()), v1.operatorAt(noId, epoch).poolHotkey, 1);
    }

    function closeOperatorEpoch(uint256 epoch, uint256 noId) external whenNotPaused returns (uint256 amount) {
        _requirePoolEpoch(epoch, noId);
        _requireRootWindow(epoch);
        ISTSettlementVault2 v = ISTSettlementVault2(vault);
        // Funding is recorded strictly in order: earlier unfunded epochs are
        // deferred, and the next root allocates each from its own usage.
        for (uint256 missed = v.lastFundedEpoch(noId) + 1; missed < epoch; missed++) {
            v.deferEmission(missed, noId);
        }
        return v.captureEmission(epoch, noId);
    }

    function commitDistribution(
        uint256 epoch,
        uint256 noId,
        uint256 prevRootCount,
        ISTSettlementVault2.Commitment calldata commitment
    ) external whenNotPaused returns (uint64 seq) {
        _requirePoolEpoch(epoch, noId);
        STCoordinator v1 = _v1();
        if (msg.sender != v1.operatorAt(noId, epoch).rootSigner) revert Unauthorized();
        _requireRootWindow(epoch);
        ISTSettlementVault2 v = ISTSettlementVault2(vault);
        // Once no approval can arrive, a pending root is replaceable; the
        // replacement covers its epochs.
        ISTSettlementVault2.Pending memory pending = v.pending(noId);
        if (pending.seq != 0 && block.number < v1.epochEndBlock(uint256(pending.epoch) + 1)) {
            revert PendingRootLive();
        }
        return v.commitPending(epoch, noId, prevRootCount, commitment);
    }

    function approveDistribution(uint256 noId, uint64 seq, bytes32 root, bytes32 ledgerHash) external {
        if (!_isAuditor(msg.sender)) revert Unauthorized();
        ISTSettlementVault2.Pending memory pending = ISTSettlementVault2(vault).pending(noId);
        if (pending.seq == 0) revert NoPendingRoot();
        if (seq != pending.seq || root != pending.root || ledgerHash != pending.ledgerHash) {
            revert PendingMismatch();
        }
        if (block.number >= _v1().epochEndBlock(uint256(pending.epoch) + 1)) revert ApprovalClosed();
        uint64 version = auditorSetVersion;
        if (_approved[noId][seq][version][msg.sender]) revert AlreadyApproved();
        _approved[noId][seq][version][msg.sender] = true;
        uint256 approvals = ++_approvalCount[noId][seq][version];
        emit DistributionApproved(noId, seq, msg.sender, version, approvals);
    }

    function finalizeDistribution(uint256 noId) external whenNotPaused returns (uint256 rootIndex) {
        ISTSettlementVault2 v = ISTSettlementVault2(vault);
        uint64 seq = v.pending(noId).seq;
        if (seq == 0) revert NoPendingRoot();
        if (_approvalCount[noId][seq][auditorSetVersion] < quorum) revert QuorumNotReached();
        // vault2 enforces the challenge window and open demands.
        return v.finalizePending(noId);
    }

    /// @dev No approval is recorded for seq 0, so with nothing pending both
    /// views read zero.
    function approvalCount(uint256 noId) external view returns (uint256) {
        return _approvalCount[noId][ISTSettlementVault2(vault).pending(noId).seq][auditorSetVersion];
    }

    function hasApproved(uint256 noId, address auditor) external view returns (bool) {
        return _approved[noId][ISTSettlementVault2(vault).pending(noId).seq][auditorSetVersion][auditor];
    }

    function _setAuditors(address[] calldata auditors_, uint256 quorum_) private {
        uint256 count = auditors_.length;
        if (quorum_ == 0 || quorum_ > count || count > MAX_AUDITORS) revert InvalidAuditors();
        for (uint256 i; i < count; i++) {
            if (auditors_[i] == address(0)) revert InvalidAuditors();
            for (uint256 j; j < i; j++) {
                if (auditors_[j] == auditors_[i]) revert InvalidAuditors();
            }
        }
        _auditors = auditors_;
        quorum = quorum_;
        uint64 version = ++auditorSetVersion;
        emit AuditorsSet(version, auditors_, quorum_);
    }

    function _isAuditor(address account) private view returns (bool) {
        uint256 count = _auditors.length;
        for (uint256 i; i < count; i++) {
            if (_auditors[i] == account) return true;
        }
        return false;
    }

    function _requirePoolEpoch(uint256 epoch, uint256 noId) private view {
        ISTSettlementVault2.Pool memory pool = ISTSettlementVault2(vault).pools(noId);
        if (!pool.registered) revert UnknownPool();
        if (epoch < pool.firstEpoch) revert InvalidEpoch();
    }

    /// @dev v2 closes and commits in [end(e), end(e) + rootCommitWindowBlocks];
    /// v1's close grace does not apply.
    function _requireRootWindow(uint256 epoch) private view {
        STCoordinator v1 = _v1();
        uint256 end = v1.epochEndBlock(epoch);
        if (block.number < end || block.number > end + v1.policyAt(epoch).rootCommitWindowBlocks) {
            revert InvalidWindow();
        }
    }

    function _v1() private view returns (STCoordinator) {
        return STCoordinator(payable(v1Coordinator));
    }

    function _authorizeUpgrade(address) internal override onlyOwner {}

    uint256[43] private __gap;
}
