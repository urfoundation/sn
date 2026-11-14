// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {SafeCast} from "@openzeppelin/contracts/utils/math/SafeCast.sol";

import {ISTSettlementVault2} from "../../src/interfaces/ISTSettlementVault2.sol";

/// @dev Stand-in for vault2 behind STSettlement2 (S suite). It serves the calls
///      STSettlement2 makes and records each one's sender, value and calldata
///      in order. Pools and pending roots are kept as vault2 would keep them
///      and can be overwritten; funding must stay in order, as in vault2. A
///      configured failure makes every recorded call revert with exactly those
///      bytes, so vault2's refusals can be shown to propagate. Its own refusals
///      never share a selector with STSettlement2's.
contract MockSettlementVault2 {
    struct Call {
        address sender;
        uint256 value;
        bytes data;
    }

    uint64 public constant CHALLENGE_BLOCKS = 14_400;
    uint16 public constant UID = 21;

    mapping(uint256 noId => ISTSettlementVault2.Pool pool) private _pools;
    mapping(uint256 noId => ISTSettlementVault2.Pending pending) private _pending;
    mapping(uint256 noId => uint256 epoch) public lastFundedEpoch;
    mapping(uint256 noId => uint64 seq) public commitSeq;
    mapping(uint256 noId => uint256 count) public rootCount;
    Call[] private _calls;
    uint256 public refund;
    uint256 public captureAmount;
    bytes public failure;

    // --- test configuration ---
    function setRefund(uint256 amount) external {
        refund = amount;
    }

    function setCaptureAmount(uint256 amount) external {
        captureAmount = amount;
    }

    function setPending(uint256 noId, ISTSettlementVault2.Pending calldata pending_) external {
        _pending[noId] = pending_;
    }

    function setFailure(bytes calldata revertData) external {
        failure = revertData;
    }

    function callCount() external view returns (uint256) {
        return _calls.length;
    }

    function callAt(uint256 index) external view returns (Call memory) {
        return _calls[index];
    }

    // --- ISTSettlementVault2 surface used by STSettlement2 ---
    function pools(uint256 noId) external view returns (ISTSettlementVault2.Pool memory) {
        return _pools[noId];
    }

    function pending(uint256 noId) external view returns (ISTSettlementVault2.Pending memory) {
        return _pending[noId];
    }

    function registerPool(uint256 noId, bytes32 hotkey, uint64, uint64 firstEpoch)
        external
        payable
        returns (uint16)
    {
        _record();
        _pools[noId] = ISTSettlementVault2.Pool({hotkey: hotkey, firstEpoch: firstEpoch, registered: true});
        lastFundedEpoch[noId] = firstEpoch - 1;
        if (refund != 0) {
            (bool refunded,) = payable(msg.sender).call{value: refund}("");
            require(refunded, "mock: refund");
        }
        return UID;
    }

    function captureEmission(uint256 epoch, uint256 noId) external returns (uint256) {
        _fund(epoch, noId);
        return captureAmount;
    }

    function deferEmission(uint256 epoch, uint256 noId) external {
        _fund(epoch, noId);
    }

    function commitPending(
        uint256 epoch,
        uint256 noId,
        uint256,
        ISTSettlementVault2.Commitment calldata commitment
    ) external returns (uint64 seq) {
        _record();
        seq = ++commitSeq[noId];
        _pending[noId] = ISTSettlementVault2.Pending({
            seq: seq,
            root: commitment.root,
            total: commitment.total,
            leafCount: commitment.leafCount,
            epoch: SafeCast.toUint64(epoch),
            ledgerHash: commitment.ledgerHash,
            committedBlock: uint64(block.number),
            openDemands: 0
        });
    }

    function finalizePending(uint256 noId) external returns (uint256 rootIndex) {
        _record();
        delete _pending[noId];
        rootIndex = rootCount[noId]++;
    }

    function _fund(uint256 epoch, uint256 noId) private {
        _record();
        if (epoch != lastFundedEpoch[noId] + 1) revert ISTSettlementVault2.InvalidTransition();
        lastFundedEpoch[noId] = epoch;
    }

    function _record() private {
        bytes memory revertData = failure;
        if (revertData.length != 0) {
            assembly {
                revert(add(revertData, 32), mload(revertData))
            }
        }
        _calls.push(Call({sender: msg.sender, value: msg.value, data: msg.data}));
    }
}
