// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {SafeCast} from "@openzeppelin/contracts/utils/math/SafeCast.sol";

import {STSettlement2} from "./STSettlement2.sol";
import {ISTSettlementVault2} from "./interfaces/ISTSettlementVault2.sol";
import {STCoordinator} from "./STCoordinator.sol";

/// @title STSettlement2PhaseC
/// @notice The phase C upgrade of STSettlement2 (VERSION2-FINAL.md §7.2, §16;
/// meta review A5). It adds two owner calls for the v1 carry sweep and changes
/// nothing else: every phase A function, event and storage slot is
/// STSettlement2's, and the one new mapping follows its storage gap.
///
/// - `assignSurplus` wraps vault2's coordinator-only `assignSurplus`. Escrow
///   stake that no pool accounts for, such as the swept v1 carry after
///   `absorbStake`, joins the funding snapshot of the pool's next epoch. The
///   owner names that epoch, so a close that lands first refuses the call
///   instead of funding a later epoch.
/// - `recordV1Sweep` records the pool's one sanctioned v1 sweep root. That v1
///   root, committed for an epoch that `settlementAt` reports as v2, pays
///   vault2's coldkey the whole v1 entitlement. Claim, replay and
///   conservation readers discover it here, beside the v2 distribution of the
///   same epoch.
contract STSettlement2PhaseC is STSettlement2 {
    /// @dev The sweep root's single leaf holds the whole entitlement.
    uint256 private constant SWEEP_SHARE_BPS = 10_000;

    /// @dev A pool's recorded v1 sweep. The epoch is zero while none is
    /// recorded: a sweep epoch is at least the pool's firstEpoch, which is
    /// never zero.
    struct V1Sweep {
        uint64 epoch;
        bytes32 payoutRoot;
        bytes32 artifactHash;
    }

    /// @dev Appended after STSettlement2's storage, its gap included, so it
    /// takes slot 50; no phase A slot moves (evm/layout/STSettlement2PhaseC.json).
    mapping(uint256 noId => V1Sweep sweep) private _v1Sweeps;

    event V1SweepRecorded(
        uint256 indexed noId, uint256 indexed epoch, bytes32 payoutRoot, bytes32 artifactHash
    );

    error NotV1SweepRoot();
    error V1SweepAlreadyRecorded();

    /// @notice O. Adds `amount` of vault2's unaccounted escrow stake to pool
    /// `noId`'s funding. It enters the funding snapshot of `epoch`, which must
    /// be the pool's next unfunded epoch, `lastFundedEpoch(noId) + 1`.
    /// vault2 refuses a zero amount and one above its surplus,
    /// `liveEscrowStake() - escrowAccounted()` (InvalidSurplus), and emits
    /// SurplusAssigned(noId, epoch, amount).
    function assignSurplus(uint256 noId, uint256 epoch, uint256 amount) external onlyOwner {
        ISTSettlementVault2 v = ISTSettlementVault2(vault);
        if (!v.pools(noId).registered) revert UnknownPool();
        if (epoch != v.lastFundedEpoch(noId) + 1) revert InvalidEpoch();
        v.assignSurplus(noId, amount);
    }

    /// @notice O, once per pool. Records `epoch` as pool `noId`'s v1 sweep
    /// epoch. The pool must be registered and `epoch` at or after its
    /// firstEpoch, a v2 epoch. The v1 coordinator must hold, for `epoch` and
    /// `noId`, the committed root of the single leaf paying vault2's coldkey
    /// 10,000 bps, and `artifactHash` must be that commitment's nonzero
    /// artifact hash, the sha256 of the published sweep record.
    function recordV1Sweep(uint256 noId, uint256 epoch, bytes32 artifactHash) external onlyOwner {
        ISTSettlementVault2 v = ISTSettlementVault2(vault);
        ISTSettlementVault2.Pool memory pool = v.pools(noId);
        if (!pool.registered) revert UnknownPool();
        if (epoch < pool.firstEpoch) revert InvalidEpoch();
        if (_v1Sweeps[noId].epoch != 0) revert V1SweepAlreadyRecorded();
        STCoordinator v1 = STCoordinator(payable(v1Coordinator));
        // With one leaf, the root is the leaf, and v1's claim takes an empty proof.
        bytes32 sweepRoot = v1.settlementVault().payoutLeaf(v.selfColdkey(), SWEEP_SHARE_BPS);
        (bytes32 payoutRoot, bytes32 committedArtifact,,) = v1.rootCommitments(epoch, noId);
        if (payoutRoot != sweepRoot || artifactHash == bytes32(0) || committedArtifact != artifactHash) {
            revert NotV1SweepRoot();
        }
        _v1Sweeps[noId] =
            V1Sweep({epoch: SafeCast.toUint64(epoch), payoutRoot: payoutRoot, artifactHash: artifactHash});
        emit V1SweepRecorded(noId, epoch, payoutRoot, artifactHash);
    }

    /// @notice Pool `noId`'s recorded v1 sweep, or zeros while none is recorded.
    function v1Sweep(uint256 noId)
        external
        view
        returns (uint64 epoch, bytes32 payoutRoot, bytes32 artifactHash)
    {
        V1Sweep storage sweep = _v1Sweeps[noId];
        return (sweep.epoch, sweep.payoutRoot, sweep.artifactHash);
    }
}
