// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {LedgerTree} from "../LedgerTree.sol";

/// @title ISTSettlementVault2
/// @notice The external surface of the immutable settlement v2 vault
/// (VERSION2-FINAL.md §7.1). C marks coordinator-only functions, B
/// bootstrap-only, A anyone. Amounts are rao of the subnet's alpha.
interface ISTSettlementVault2 {
    struct Pool {
        bytes32 hotkey;
        uint64 firstEpoch;
        bool registered;
    }

    /// @notice A finalized root. `roots` is append-only.
    struct Root {
        bytes32 root;
        uint256 total;
        uint64 leafCount;
        uint64 epoch;
        bytes32 ledgerHash;
    }

    /// @notice The pool's single pending root; `seq == 0` means none.
    struct Pending {
        uint64 seq;
        bytes32 root;
        uint256 total;
        uint64 leafCount;
        uint64 epoch;
        bytes32 ledgerHash;
        uint64 committedBlock;
        uint64 openDemands;
    }

    /// @notice A path demand on one account of a finalized root, opened in
    /// the pending root: none, open until a clean opening answers it, or
    /// answered. Each account of each finalized root can be demanded once per
    /// pending root.
    enum DemandState {
        None,
        Open,
        Answered
    }

    /// @notice A root as committed by the coordinator.
    struct Commitment {
        bytes32 root;
        uint256 total;
        uint64 leafCount;
        bytes32 ledgerHash;
    }

    event CoordinatorFixed(address indexed coordinator);
    event EscrowRegistered(bytes32 indexed hotkey, uint16 uid);
    event PoolRegistered(uint256 indexed noId, bytes32 indexed hotkey, uint16 uid, uint64 firstEpoch);
    event PoolUidRegistered(uint256 indexed noId, bytes32 indexed hotkey, uint16 uid, address payer);
    event EmissionCaptured(
        uint256 indexed epoch, uint256 indexed noId, uint256 observed, uint256 amount, uint256 fundedThrough
    );
    event EmissionDeferred(uint256 indexed epoch, uint256 indexed noId, uint256 fundedThrough);
    event DistributionCommitted(
        uint256 indexed noId,
        uint64 indexed seq,
        uint64 epoch,
        bytes32 root,
        uint256 total,
        uint64 leafCount,
        bytes32 ledgerHash,
        uint64 replacedSeq
    );
    event PathDemanded(
        uint256 indexed noId, uint64 indexed seq, uint256 rootIndex, uint256 index, address demander
    );
    event PathAnswered(
        uint256 indexed noId, uint64 indexed seq, uint256 rootIndex, uint256 index, address answerer
    );
    event DistributionDeleted(
        uint256 indexed noId,
        uint64 indexed seq,
        uint256 rootIndex,
        uint256 index,
        LedgerTree.Fault fault,
        uint256 level,
        address challenger
    );
    event DistributionFinalized(
        uint256 indexed noId,
        uint64 indexed seq,
        uint256 rootIndex,
        uint64 epoch,
        bytes32 root,
        uint256 total,
        uint64 leafCount,
        bytes32 ledgerHash
    );
    event Claimed(
        uint256 indexed noId,
        uint256 rootIndex,
        uint256 indexed index,
        bytes32 indexed coldkey,
        uint256 cumulative,
        uint256 amount,
        address caller
    );
    event StakeAbsorbed(bytes32 indexed hotkey, uint256 observed, uint256 amount, address caller);
    event SurplusAssigned(uint256 indexed noId, uint256 indexed epoch, uint256 amount);

    error Unauthorized();
    error AlreadyInitialized();
    error InvalidConfiguration();
    error Reentrancy();
    error Underfunded();
    error NativeRefundFailed();
    error NativeBalanceDecreased();
    error UnknownPool();
    error UidExists();
    error InvalidTransition();
    error EpochOutOfRange();
    error TotalMismatch();
    error TotalNotIncreased();
    error InvalidLeafCount();
    error InvalidRoot();
    error CommitTooSoon();
    error StaleRootCount();
    error NoPendingRoot();
    error NoFinalizedRoot();
    error ChallengeWindowOpen();
    error ChallengeWindowClosed();
    error DemandOpen();
    error InvalidDemandFee();
    error AlreadyDemanded();
    error IndexOutOfRange();
    error InvalidProof();
    error NoFault();
    error UnknownRoot();
    error InvalidColdkey();
    error NothingToClaim();
    error PoolCapExceeded();
    error AmountTooLarge();
    error InvalidHotkey();
    error NothingToAbsorb();
    error InvalidSurplus();
    error RuntimeAccountingMismatch();

    /// B, once. A nonzero address with code.
    function setCoordinatorOnce(address coordinator_) external;

    /// B, once, payable. Registers the escrow hotkey; refunds the surplus.
    function registerEscrow(uint64 maximumBurnRao) external payable returns (uint16 uid);

    /// C, payable. Requires 0 < noId < 2^64, firstEpoch >= 1, and a fresh hotkey
    /// that is not the escrow; sets lastFundedEpoch = firstEpoch − 1.
    function registerPool(uint256 noId, bytes32 hotkey, uint64 maximumBurnRao, uint64 firstEpoch)
        external
        payable
        returns (uint16 uid);

    /// A, payable. Re-registers a registered pool hotkey that has no UID.
    function registerPoolUid(uint256 noId, uint64 maximumBurnRao) external payable returns (uint16 uid);

    /// C. Requires epoch == lastFundedEpoch + 1.
    function captureEmission(uint256 epoch, uint256 noId) external returns (uint256 amount);

    /// C. Requires epoch == lastFundedEpoch + 1.
    function deferEmission(uint256 epoch, uint256 noId) external;

    /// C. Replaces any pending root; returns the new seq.
    function commitPending(uint256 epoch, uint256 noId, uint256 prevRootCount, Commitment calldata commitment)
        external
        returns (uint64 seq);

    /// A, payable with exactly demandFeeWei, inside the challenge window.
    /// `account` must be proven against the finalized root `rootIndex` (any
    /// finalized root, so a withheld newest tree cannot stop demands), and
    /// only an existing account can be demanded.
    function demandPath(
        uint256 noId,
        uint256 rootIndex,
        LedgerTree.Account calldata account,
        LedgerTree.ProofNode[] calldata oldProof
    ) external payable;

    /// A. `account` is an account of the finalized root `rootIndex` with
    /// `oldProof` against it; `newPath` opens the pending root along
    /// account.index. A fault deletes the pending root; a clean opening
    /// answers the open demand at (seq, rootIndex, index).
    function checkPath(
        uint256 noId,
        uint256 rootIndex,
        LedgerTree.Account calldata account,
        LedgerTree.ProofNode[] calldata oldProof,
        LedgerTree.PathNode[] calldata newPath
    ) external returns (LedgerTree.Fault fault);

    /// C. Requires the window passed and no open demand; returns the root index.
    function finalizePending(uint256 noId) external returns (uint256 rootIndex);

    /// A. Pays cumulative − claimed to account.coldkey, or reverts.
    function claim(
        uint256 noId,
        uint256 rootIndex,
        LedgerTree.Account calldata account,
        LedgerTree.ProofNode[] calldata proof
    ) external returns (uint256 amount);

    /// A. Moves all stake this vault holds on a foreign hotkey into escrow.
    function absorbStake(bytes32 hotkey) external returns (uint256 amount);

    /// C. Adds unaccounted escrow stake to a pool's funding.
    function assignSurplus(uint256 noId, uint256 amount) external;

    function CHALLENGE_BLOCKS() external view returns (uint64);
    function netuid() external view returns (uint16);
    function escrowHotkey() external view returns (bytes32);
    function selfColdkey() external view returns (bytes32);
    function bootstrap() external view returns (address);
    function demandFeeWei() external view returns (uint256);
    function coordinator() external view returns (address);
    function escrowRegistered() external view returns (bool);

    function pools(uint256 noId) external view returns (Pool memory);
    function poolHotkeyUsed(bytes32 hotkey) external view returns (bool);
    function lastFundedEpoch(uint256 noId) external view returns (uint256);
    function fundedThrough(uint256 noId, uint256 epoch) external view returns (uint256);
    function capturedTotal(uint256 noId) external view returns (uint256);
    function claimedTotal(uint256 noId) external view returns (uint256);
    function finalizedTotal(uint256 noId) external view returns (uint256);
    function rootCount(uint256 noId) external view returns (uint256);
    function roots(uint256 noId, uint256 rootIndex) external view returns (Root memory);
    function pending(uint256 noId) external view returns (Pending memory);
    function commitSeq(uint256 noId) external view returns (uint64);
    function lastCommitBlock(uint256 noId) external view returns (uint64);
    function demanded(uint256 noId, uint64 seq, uint256 rootIndex, uint256 index)
        external
        view
        returns (DemandState);
    function claimed(uint256 noId, uint256 index) external view returns (uint256);

    function totalCaptured() external view returns (uint256);
    function totalPaid() external view returns (uint256);
    function escrowAccounted() external view returns (uint256);
    function conservationHolds() external view returns (bool);
    function liveEscrowStake() external view returns (uint256);
}
