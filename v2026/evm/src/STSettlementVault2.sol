// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {SafeCast} from "@openzeppelin/contracts/utils/math/SafeCast.sol";

import {ISTSettlementVault2} from "./interfaces/ISTSettlementVault2.sol";
import {IStaking, ISTAKING_ADDRESS} from "./interfaces/stakingV2.sol";
import {IStakingCollateral} from "./interfaces/stakingCollateral.sol";
import {INeuron, INeuron_ADDRESS} from "./interfaces/neuron.sol";
import {LedgerTree} from "./LedgerTree.sol";
import {NativeBalance} from "./NativeBalance.sol";

/// @title STSettlementVault2
/// @notice Immutable custody for settlement v2 (VERSION2-FINAL.md §7.1). It
/// captures each operator pool's emission into one escrow hotkey, keeps each
/// pool's pending root and every finalized root of its ledger sum tree, and
/// pays cumulative account balances against them. The coordinator decides when
/// epochs are captured and which roots are committed and finalized; whatever
/// it does, this vault enforces conservation, the funding binding of every
/// root, the challenge window, path challenges and demands, and the
/// destination of every payment (§4.2 G1-G6). Nothing here can be upgraded,
/// paused or redirected, and a valid claim cannot be stopped.
contract STSettlementVault2 is ISTSettlementVault2 {
    uint64 public constant CHALLENGE_BLOCKS = 14_400;

    // The staking precompile reads an amount of 2^64 − 1 or more as "the
    // whole position" (§7.3); every amount passed to it stays below this.
    uint256 private constant WHOLE_POSITION = type(uint64).max;

    // One rao in wei. The runtime truncates native value below a rao.
    uint256 private constant RAO_WEI = 1e9;

    // A pool, packed by use: a claim reads and writes the second slot, a
    // capture or deferral the third, and a commit, demand, answer or
    // finalization the fourth and rootCount. Amounts fit 128 bits: a capture
    // moves less than 2^64, and _fund refuses a total beyond 128 bits, which
    // no real stake reaches. The pool is registered exactly when firstEpoch
    // is nonzero (registerPool refuses 0), and its next unfunded epoch,
    // lastFundedEpoch + 1, is firstEpoch + fundedEpochs. While pendingSeq is
    // nonzero, the pending root is the pool's root record at index rootCount,
    // committed at lastCommitBlock.
    struct PoolState {
        bytes32 hotkey;
        uint128 claimedTotal;
        uint64 rootCount;
        uint128 capturedTotal;
        uint64 firstEpoch;
        uint64 fundedEpochs;
        uint64 commitSeq;
        uint64 lastCommitBlock;
        uint64 pendingSeq;
        uint64 openDemands;
    }

    // A Root as stored. Its total equals a funding snapshot, so it fits 128
    // bits, and a proof reads two slots.
    struct RootRecord {
        bytes32 root;
        uint128 total;
        uint64 leafCount;
        uint64 epoch;
        bytes32 ledgerHash;
    }

    uint16 public immutable netuid;
    bytes32 public immutable escrowHotkey;
    bytes32 public immutable selfColdkey;
    address public immutable bootstrap;
    uint256 public immutable demandFeeWei;

    address public coordinator;
    bool public escrowRegistered;
    // The reentrancy flag shares the slot every coordinator call reads.
    bool private _entered;

    // The vault's totals; escrowAccounted is their difference (§4.2 G1).
    uint128 private _totalCaptured;
    uint128 private _totalPaid;

    mapping(uint256 noId => PoolState pool) private _pools;
    mapping(bytes32 hotkey => bool used) public poolHotkeyUsed;
    // Records below rootCount are the finalized roots, append-only; the one
    // at rootCount is the pending root while there is one.
    mapping(uint256 noId => mapping(uint256 rootIndex => RootRecord root)) private _roots;
    // Two epochs' funding snapshots, and two accounts' claimed watermarks,
    // share a slot: entry i is half i % 2 of slot i / 2.
    mapping(uint256 noId => mapping(uint256 pair => uint128[2] amounts)) private _fundedThrough;
    mapping(uint256 noId => mapping(uint256 pair => uint128[2] amounts)) private _claimed;
    mapping(
        uint256 noId
            => mapping(
            uint64 seq => mapping(uint256 rootIndex => mapping(uint256 index => DemandState state))
        )
    ) public demanded;

    modifier onlyBootstrap() {
        if (msg.sender != bootstrap) revert Unauthorized();
        _;
    }

    modifier onlyCoordinator() {
        if (msg.sender != coordinator) revert Unauthorized();
        _;
    }

    // One guard shared by every state-changing entry point, so no runtime or
    // refund callback can enter the vault while another call is in progress.
    // It stays in persistent storage: runtime 475's EVM keeps a transient
    // write made in a call that reverts (scripts/runtime-rehearsal/README.md).
    modifier nonReentrant() {
        if (_entered) revert Reentrancy();
        _entered = true;
        _;
        _entered = false;
    }

    constructor(
        uint16 netuid_,
        bytes32 escrowHotkey_,
        bytes32 selfColdkey_,
        address bootstrap_,
        uint256 demandFeeWei_
    ) {
        if (
            netuid_ == 0 || escrowHotkey_ == bytes32(0) || selfColdkey_ == bytes32(0)
                || bootstrap_ == address(0) || demandFeeWei_ == 0 || demandFeeWei_ % RAO_WEI != 0
        ) revert InvalidConfiguration();
        netuid = netuid_;
        escrowHotkey = escrowHotkey_;
        selfColdkey = selfColdkey_;
        bootstrap = bootstrap_;
        demandFeeWei = demandFeeWei_;
    }

    function setCoordinatorOnce(address coordinator_) external onlyBootstrap nonReentrant {
        if (coordinator != address(0)) revert AlreadyInitialized();
        if (coordinator_ == address(0) || coordinator_.code.length == 0) revert InvalidConfiguration();
        coordinator = coordinator_;
        emit CoordinatorFixed(coordinator_);
    }

    /// @notice Limit-registers the immutable escrow hotkey under this vault's
    /// mapped coldkey. The bootstrap may do this exactly once; no external key
    /// ever controls the hotkey.
    function registerEscrow(uint64 maximumBurnRao)
        external
        payable
        onlyBootstrap
        nonReentrant
        returns (uint16 uid)
    {
        if (escrowRegistered) revert AlreadyInitialized();
        if (maximumBurnRao == 0) revert InvalidConfiguration();
        escrowRegistered = true;
        uid = _register(escrowHotkey, maximumBurnRao);
        emit EscrowRegistered(escrowHotkey, uid);
    }

    function registerPool(uint256 noId, bytes32 hotkey, uint64 maximumBurnRao, uint64 firstEpoch)
        external
        payable
        onlyCoordinator
        nonReentrant
        returns (uint16 uid)
    {
        PoolState storage pool = _pools[noId];
        if (
            !escrowRegistered || noId == 0 || noId > type(uint64).max || hotkey == bytes32(0)
                || hotkey == escrowHotkey || poolHotkeyUsed[hotkey] || maximumBurnRao == 0 || firstEpoch == 0
                || pool.firstEpoch != 0
        ) revert InvalidConfiguration();
        pool.hotkey = hotkey;
        pool.firstEpoch = firstEpoch;
        poolHotkeyUsed[hotkey] = true;
        uid = _register(hotkey, maximumBurnRao);
        emit PoolRegistered(noId, hotkey, uid, firstEpoch);
    }

    /// @notice Re-registers a pool hotkey that lost its UID to pruning. Pruning
    /// keeps the hotkey's stake and owner, and only this vault's coldkey can
    /// register it again (§7.3). The caller pays the burn.
    function registerPoolUid(uint256 noId, uint64 maximumBurnRao)
        external
        payable
        nonReentrant
        returns (uint16 uid)
    {
        bytes32 hotkey = _registeredPool(noId).hotkey;
        // Only whether a UID exists matters here.
        // slither-disable-next-line unused-return
        (bool exists,) = INeuron(INeuron_ADDRESS).getUid(netuid, hotkey);
        if (exists) revert UidExists();
        if (maximumBurnRao == 0) revert InvalidConfiguration();
        uid = _register(hotkey, maximumBurnRao);
        emit PoolUidRegistered(noId, hotkey, uid, msg.sender);
    }

    /// @notice Funds epoch `epoch` with all the stake this vault can move off
    /// the pool hotkey, moved into escrow. Nothing movable funds zero without a
    /// runtime call. Effects follow the move because the amount is escrow's
    /// gain; the shared guard keeps them atomic with it.
    function captureEmission(uint256 epoch, uint256 noId)
        external
        onlyCoordinator
        nonReentrant
        returns (uint256 amount)
    {
        PoolState storage pool = _advance(epoch, noId);
        bytes32 hotkey = pool.hotkey;
        (uint256 staked, uint256 observed) = _movable(hotkey);
        uint256 escrowStake;
        if (observed != 0) {
            (amount, escrowStake) = _moveToEscrow(hotkey, staked, observed);
            _fund(pool, amount);
        } else {
            escrowStake = _stake(escrowHotkey, selfColdkey);
        }
        uint256 funded = _snapshot(pool, noId, epoch);
        _requireBacking(escrowStake);
        emit EmissionCaptured(epoch, noId, observed, amount, funded);
    }

    /// @notice Funds epoch `epoch` with nothing. Its emission stays on the pool
    /// hotkey for the next capture.
    function deferEmission(uint256 epoch, uint256 noId) external onlyCoordinator nonReentrant {
        PoolState storage pool = _advance(epoch, noId);
        emit EmissionDeferred(epoch, noId, _snapshot(pool, noId, epoch));
    }

    /// @notice Replaces the pool's pending root, and with it every demand on
    /// it. The root's total must equal the funding snapshot of its epoch and
    /// exceed the latest finalized total; its accounts extend the latest
    /// finalized root's. Commits are spaced by the challenge window so that a
    /// demand sample cannot be re-rolled quickly (§4.2 G2).
    function commitPending(uint256 epoch, uint256 noId, uint256 prevRootCount, Commitment calldata commitment)
        external
        onlyCoordinator
        nonReentrant
        returns (uint64 seq)
    {
        PoolState storage pool = _registeredPool(noId);
        uint64 lastCommit = pool.lastCommitBlock;
        if (lastCommit != 0 && block.number < uint256(lastCommit) + CHALLENGE_BLOCKS) revert CommitTooSoon();
        uint64 count = pool.rootCount;
        if (prevRootCount != count) revert StaleRootCount();
        // With no finalized root, the pool starts empty before its first epoch.
        uint256 previousEpoch = pool.firstEpoch - 1;
        uint256 previousTotal = 0;
        uint256 previousLeafCount = 0;
        if (count != 0) {
            RootRecord storage previous = _roots[noId][count - 1];
            (previousEpoch, previousTotal, previousLeafCount) =
            (previous.epoch, previous.total, previous.leafCount);
        }
        if (epoch <= previousEpoch || epoch >= _nextEpoch(pool)) revert EpochOutOfRange();
        if (commitment.total != _fundedThrough[noId][epoch >> 1][epoch & 1]) revert TotalMismatch();
        if (commitment.total <= previousTotal) revert TotalNotIncreased();
        if (
            commitment.leafCount == 0 || commitment.leafCount > LedgerTree.MAX_LEAF_COUNT
                || commitment.leafCount < previousLeafCount
        ) revert InvalidLeafCount();
        if (commitment.root == bytes32(0) || commitment.ledgerHash == bytes32(0)) revert InvalidRoot();

        uint64 replacedSeq = pool.pendingSeq;
        seq = pool.commitSeq + 1;
        uint64 committedEpoch = SafeCast.toUint64(epoch);
        uint64 committedBlock = SafeCast.toUint64(block.number);
        // The total equals a funding snapshot, so its cast cannot fail.
        _roots[noId][count] = RootRecord({
            root: commitment.root,
            total: SafeCast.toUint128(commitment.total),
            leafCount: commitment.leafCount,
            epoch: committedEpoch,
            ledgerHash: commitment.ledgerHash
        });
        pool.commitSeq = seq;
        pool.lastCommitBlock = committedBlock;
        pool.pendingSeq = seq;
        pool.openDemands = 0;
        emit DistributionCommitted(
            noId,
            seq,
            committedEpoch,
            commitment.root,
            commitment.total,
            commitment.leafCount,
            commitment.ledgerHash,
            replacedSeq
        );
    }

    /// @notice Demands, during the challenge window, the on-chain opening in
    /// the pending root of an account proven against the finalized root
    /// `rootIndex`. Any finalized root will do, so withholding the newest tree
    /// cannot stop demands on the accounts of an older one (§4.2 G2). The
    /// pending root cannot finalize until checkPath answers it, so a withheld
    /// or garbage tree blocks itself (§2.2); only a provable account can be
    /// demanded, so an honest successor, which keeps every account at its
    /// value or more (§6.1), can always answer. Each account of each finalized
    /// root can be demanded once per pending root. The fee stays in this vault.
    function demandPath(
        uint256 noId,
        uint256 rootIndex,
        LedgerTree.Account calldata account,
        LedgerTree.ProofNode[] calldata oldProof
    ) external payable nonReentrant {
        if (msg.value != demandFeeWei) revert InvalidDemandFee();
        (PoolState storage pool, RootRecord storage previous) = _disputedRoots(noId, rootIndex);
        if (block.number >= uint256(pool.lastCommitBlock) + CHALLENGE_BLOCKS) revert ChallengeWindowClosed();
        _requireProven(previous, account, oldProof);
        uint64 seq = pool.pendingSeq;
        uint256 index = account.index;
        mapping(uint256 => DemandState) storage demands = demanded[noId][seq][rootIndex];
        if (demands[index] != DemandState.None) revert AlreadyDemanded();
        demands[index] = DemandState.Open;
        pool.openDemands++;
        emit PathDemanded(noId, seq, rootIndex, index, msg.sender);
    }

    /// @notice The §6.5 path check of an account of the finalized root
    /// `rootIndex` against the pending root. A fault deletes the pending root;
    /// an opening with no fault answers an open demand on that account of that
    /// root; anything else reverts. Works at any time while the root is
    /// pending, and never checks backing, so a deficit cannot freeze disputes.
    function checkPath(
        uint256 noId,
        uint256 rootIndex,
        LedgerTree.Account calldata account,
        LedgerTree.ProofNode[] calldata oldProof,
        LedgerTree.PathNode[] calldata newPath
    ) external nonReentrant returns (LedgerTree.Fault fault) {
        (PoolState storage pool, RootRecord storage previous) = _disputedRoots(noId, rootIndex);
        _requireProven(previous, account, oldProof);
        uint64 seq = pool.pendingSeq;
        uint256 index = account.index;
        RootRecord storage next = _roots[noId][pool.rootCount];
        // Leaf counts never decrease from one root to the next (commitPending),
        // so `index` is inside the pending root.
        uint256 level;
        (fault, level) = LedgerTree.walkPath(next.root, next.total, next.leafCount, account, newPath);
        if (fault != LedgerTree.Fault.None) {
            pool.pendingSeq = 0;
            emit DistributionDeleted(noId, seq, rootIndex, index, fault, level, msg.sender);
            return fault;
        }
        mapping(uint256 => DemandState) storage demands = demanded[noId][seq][rootIndex];
        if (demands[index] != DemandState.Open) revert NoFault();
        demands[index] = DemandState.Answered;
        pool.openDemands--;
        emit PathAnswered(noId, seq, rootIndex, index, msg.sender);
    }

    /// @notice Appends the pending root to the pool's finalized roots once its
    /// challenge window has passed with no open demand (§4.2 G6).
    function finalizePending(uint256 noId) external onlyCoordinator nonReentrant returns (uint256 rootIndex) {
        PoolState storage pool = _pools[noId];
        uint64 seq = pool.pendingSeq;
        if (seq == 0) revert NoPendingRoot();
        if (block.number < uint256(pool.lastCommitBlock) + CHALLENGE_BLOCKS) revert ChallengeWindowOpen();
        if (pool.openDemands != 0) revert DemandOpen();
        uint64 count = pool.rootCount;
        pool.rootCount = count + 1;
        pool.pendingSeq = 0;
        rootIndex = count;
        RootRecord storage next = _roots[noId][count];
        emit DistributionFinalized(
            noId, seq, rootIndex, next.epoch, next.root, next.total, next.leafCount, next.ledgerHash
        );
    }

    /// @notice Pays account `account.index` of any finalized root everything it
    /// accrued since its last claim, always to the coldkey in its leaf, whoever
    /// calls (§4.2 G3, G4). A pool never pays out more than its latest
    /// finalized total. Any refusal reverts the whole claim, which stays
    /// claimable.
    function claim(
        uint256 noId,
        uint256 rootIndex,
        LedgerTree.Account calldata account,
        LedgerTree.ProofNode[] calldata proof
    ) external nonReentrant returns (uint256 amount) {
        PoolState storage pool = _pools[noId];
        uint256 count = pool.rootCount;
        if (rootIndex >= count) revert UnknownRoot();
        RootRecord storage finalized = _roots[noId][rootIndex];
        bytes32 coldkey = account.coldkey;
        if (coldkey == bytes32(0) || coldkey == selfColdkey) revert InvalidColdkey();
        if (!LedgerTree.verifyLeaf(finalized.root, finalized.total, finalized.leafCount, account, proof)) {
            revert InvalidProof();
        }
        uint256 index = account.index;
        uint256 cumulative = account.cumulative;
        uint256 paid = _claimed[noId][index >> 1][index & 1];
        if (cumulative <= paid) revert NothingToClaim();
        amount = cumulative - paid;
        if (amount >= WHOLE_POSITION) revert AmountTooLarge();
        uint256 poolClaimed = pool.claimedTotal + amount;
        if (poolClaimed > _roots[noId][count - 1].total) revert PoolCapExceeded();

        // A proven cumulative is at most its root's total, and the pool's
        // claims at most its latest total, so no cast here can fail.
        _claimed[noId][index >> 1][index & 1] = SafeCast.toUint128(cumulative);
        pool.claimedTotal = SafeCast.toUint128(poolClaimed);
        _totalPaid += SafeCast.toUint128(amount);
        _requireBacking(_payFromEscrow(coldkey, amount));
        emit Claimed(noId, rootIndex, index, coldkey, cumulative, amount, msg.sender);
    }

    /// @notice Moves all the stake this vault's coldkey can move off a foreign
    /// hotkey, such as the v1 escrow, into escrow. Nothing is accounted: the
    /// stake becomes surplus that the coordinator may assign to a pool.
    function absorbStake(bytes32 hotkey) external nonReentrant returns (uint256 amount) {
        if (hotkey == bytes32(0) || hotkey == escrowHotkey || poolHotkeyUsed[hotkey]) revert InvalidHotkey();
        (uint256 staked, uint256 observed) = _movable(hotkey);
        if (observed == 0) revert NothingToAbsorb();
        uint256 escrowStake;
        (amount, escrowStake) = _moveToEscrow(hotkey, staked, observed);
        _requireBacking(escrowStake);
        emit StakeAbsorbed(hotkey, observed, amount, msg.sender);
    }

    /// @notice Adds escrow stake that no pool accounts for to a pool's funding.
    /// It enters the funding snapshot of the pool's next epoch.
    function assignSurplus(uint256 noId, uint256 amount) external onlyCoordinator nonReentrant {
        PoolState storage pool = _registeredPool(noId);
        uint256 live = _liveEscrowStake();
        uint256 accounted = escrowAccounted();
        if (amount == 0 || live < accounted || amount > live - accounted) {
            revert InvalidSurplus();
        }
        _fund(pool, amount);
        _requireBacking(_stake(escrowHotkey, selfColdkey));
        emit SurplusAssigned(noId, _nextEpoch(pool), amount);
    }

    function pools(uint256 noId) external view returns (Pool memory) {
        PoolState storage pool = _pools[noId];
        return Pool({hotkey: pool.hotkey, firstEpoch: pool.firstEpoch, registered: pool.firstEpoch != 0});
    }

    function lastFundedEpoch(uint256 noId) external view returns (uint256 epoch) {
        PoolState storage pool = _pools[noId];
        return pool.firstEpoch == 0 ? 0 : _nextEpoch(pool) - 1;
    }

    function fundedThrough(uint256 noId, uint256 epoch) external view returns (uint256 amount) {
        return _fundedThrough[noId][epoch >> 1][epoch & 1];
    }

    function capturedTotal(uint256 noId) external view returns (uint256 amount) {
        return _pools[noId].capturedTotal;
    }

    function claimedTotal(uint256 noId) external view returns (uint256 amount) {
        return _pools[noId].claimedTotal;
    }

    function finalizedTotal(uint256 noId) external view returns (uint256) {
        uint256 count = _pools[noId].rootCount;
        return count == 0 ? 0 : _roots[noId][count - 1].total;
    }

    function rootCount(uint256 noId) external view returns (uint256) {
        return _pools[noId].rootCount;
    }

    function roots(uint256 noId, uint256 rootIndex) external view returns (Root memory) {
        // Past the last finalized root this fails as a read past the end of an
        // array does (Panic 0x32), a stored pending root included.
        if (rootIndex >= _pools[noId].rootCount) return new Root[](0)[rootIndex];
        RootRecord storage stored = _roots[noId][rootIndex];
        return Root({
            root: stored.root,
            total: stored.total,
            leafCount: stored.leafCount,
            epoch: stored.epoch,
            ledgerHash: stored.ledgerHash
        });
    }

    function pending(uint256 noId) external view returns (Pending memory) {
        PoolState storage pool = _pools[noId];
        uint64 seq = pool.pendingSeq;
        // With no root pending, every field reads zero.
        if (seq == 0) {
            return Pending({
                seq: 0,
                root: bytes32(0),
                total: 0,
                leafCount: 0,
                epoch: 0,
                ledgerHash: bytes32(0),
                committedBlock: 0,
                openDemands: 0
            });
        }
        RootRecord storage stored = _roots[noId][pool.rootCount];
        return Pending({
            seq: seq,
            root: stored.root,
            total: stored.total,
            leafCount: stored.leafCount,
            epoch: stored.epoch,
            ledgerHash: stored.ledgerHash,
            committedBlock: pool.lastCommitBlock,
            openDemands: pool.openDemands
        });
    }

    function commitSeq(uint256 noId) external view returns (uint64 seq) {
        return _pools[noId].commitSeq;
    }

    function lastCommitBlock(uint256 noId) external view returns (uint64 blockNumber) {
        return _pools[noId].lastCommitBlock;
    }

    function claimed(uint256 noId, uint256 index) external view returns (uint256 amount) {
        return _claimed[noId][index >> 1][index & 1];
    }

    function totalCaptured() external view returns (uint256) {
        return _totalCaptured;
    }

    function totalPaid() external view returns (uint256) {
        return _totalPaid;
    }

    function escrowAccounted() public view returns (uint256) {
        return _totalCaptured - _totalPaid;
    }

    /// @notice Executable conservation identity (§4.2 G1). totalCaptured =
    /// totalPaid + escrowAccounted holds by construction, escrowAccounted
    /// being their difference; the live escrow stake must back it.
    function conservationHolds() external view returns (bool) {
        return _liveEscrowStake() >= escrowAccounted();
    }

    function liveEscrowStake() external view returns (uint256) {
        return _liveEscrowStake();
    }

    // Limit-registers `hotkey`, refunding to the caller whatever msg.value the
    // burn left. Runtime 452 charges the burn to this contract's SS58-mirror
    // balance, which msg.value funds before execution; forwarding it to the
    // precompile would move it away before the dispatch can burn it. The burn
    // may spend msg.value but never the balance the vault held before the
    // call, such as demand fees.
    function _register(bytes32 hotkey, uint64 maximumBurnRao) private returns (uint16 uid) {
        uint256 balanceBefore = NativeBalance.beforeSuppliedValue(address(this).balance, msg.value);
        INeuron(INeuron_ADDRESS).registerLimit(netuid, hotkey, maximumBurnRao);
        bool exists;
        (exists, uid) = INeuron(INeuron_ADDRESS).getUid(netuid, hotkey);
        if (!exists) revert UnknownPool();
        uint256 current = address(this).balance;
        if (current < balanceBefore) revert NativeBalanceDecreased();
        if (current > balanceBefore) {
            (bool refunded,) = payable(msg.sender).call{value: current - balanceBefore}("");
            if (!refunded) revert NativeRefundFailed();
        }
    }

    // The registered pool `noId`, or UnknownPool.
    function _registeredPool(uint256 noId) private view returns (PoolState storage pool) {
        pool = _pools[noId];
        if (pool.firstEpoch == 0) revert UnknownPool();
    }

    // The pool's next unfunded epoch, lastFundedEpoch + 1.
    function _nextEpoch(PoolState storage pool) private view returns (uint256) {
        return uint256(pool.firstEpoch) + pool.fundedEpochs;
    }

    // Records `epoch`, which must be the pool's next unfunded epoch, as funded
    // before any runtime call.
    function _advance(uint256 epoch, uint256 noId) private returns (PoolState storage pool) {
        pool = _registeredPool(noId);
        if (epoch != _nextEpoch(pool)) revert InvalidTransition();
        pool.fundedEpochs++;
    }

    // Adds `amount` to the pool's funding and the vault's. A total beyond 128
    // bits is refused, not truncated.
    function _fund(PoolState storage pool, uint256 amount) private {
        pool.capturedTotal = SafeCast.toUint128(pool.capturedTotal + amount);
        _totalCaptured = SafeCast.toUint128(_totalCaptured + amount);
    }

    // Writes the funding snapshot of the epoch just advanced to, once (§4.2 G5).
    function _snapshot(PoolState storage pool, uint256 noId, uint256 epoch) private returns (uint256) {
        uint128 funded = pool.capturedTotal;
        _fundedThrough[noId][epoch >> 1][epoch & 1] = funded;
        return funded;
    }

    // Moves `observed` of the vault's stake on `hotkey`, whose whole position
    // read `sourceBefore`, into escrow, and returns escrow's gain and its whole
    // position after the move. The runtime moves exactly the requested amount
    // out of the source, and escrow's share-pool read can be one rao short
    // (§7.3). Anything else reverts. The deltas compare whole positions, locks
    // included.
    function _moveToEscrow(bytes32 hotkey, uint256 sourceBefore, uint256 observed)
        private
        returns (uint256, uint256 escrowAfter)
    {
        if (observed >= WHOLE_POSITION) revert AmountTooLarge();
        uint256 escrowBefore = _stake(escrowHotkey, selfColdkey);
        IStaking(ISTAKING_ADDRESS).moveStake(hotkey, escrowHotkey, uint256(netuid), uint256(netuid), observed);
        escrowAfter = _stake(escrowHotkey, selfColdkey);
        if (
            _stake(hotkey, selfColdkey) + observed != sourceBefore || escrowAfter > escrowBefore + observed
                || escrowAfter + 1 < escrowBefore + observed
        ) revert RuntimeAccountingMismatch();
        return (escrowAfter - escrowBefore, escrowAfter);
    }

    // Transfers `amount` of escrow stake to `coldkey`, on the escrow hotkey, and
    // returns escrow's whole position after it. Escrow must lose exactly
    // `amount`; the destination's read can be one rao short (§7.3). Anything
    // else reverts.
    function _payFromEscrow(bytes32 coldkey, uint256 amount) private returns (uint256 escrowAfter) {
        uint256 escrowBefore = _stake(escrowHotkey, selfColdkey);
        uint256 destinationBefore = _stake(escrowHotkey, coldkey);
        IStaking(ISTAKING_ADDRESS)
            .transferStake(coldkey, escrowHotkey, uint256(netuid), uint256(netuid), amount);
        uint256 destinationAfter = _stake(escrowHotkey, coldkey);
        escrowAfter = _stake(escrowHotkey, selfColdkey);
        if (
            escrowAfter + amount != escrowBefore || destinationAfter > destinationBefore + amount
                || destinationAfter + 1 < destinationBefore + amount
        ) revert RuntimeAccountingMismatch();
    }

    // The pool, with a pending root, and the finalized root `rootIndex` whose
    // accounts the pending root must keep: what a demand or path check is about.
    function _disputedRoots(uint256 noId, uint256 rootIndex)
        private
        view
        returns (PoolState storage pool, RootRecord storage previous)
    {
        pool = _pools[noId];
        if (pool.pendingSeq == 0) revert NoPendingRoot();
        if (rootIndex >= pool.rootCount) revert UnknownRoot();
        previous = _roots[noId][rootIndex];
    }

    // Requires `account` to be an account of the finalized root `previous`.
    function _requireProven(
        RootRecord storage previous,
        LedgerTree.Account calldata account,
        LedgerTree.ProofNode[] calldata proof
    ) private view {
        if (account.index >= previous.leafCount) {
            revert IndexOutOfRange();
        }
        if (!LedgerTree.verifyLeaf(previous.root, previous.total, previous.leafCount, account, proof)) {
            revert InvalidProof();
        }
    }

    function _stake(bytes32 hotkey, bytes32 coldkey) private view returns (uint256) {
        return IStaking(ISTAKING_ADDRESS).getStake(hotkey, coldkey, uint256(netuid));
    }

    // The vault's whole position on `hotkey` and the stake it can move off it:
    // the position less the registration collateral that runtime 475 locks on
    // it. getStake includes the lock, and a move that digs into it fails
    // StakeUnavailable.
    function _movable(bytes32 hotkey) private view returns (uint256 staked, uint256 movable) {
        staked = _stake(hotkey, selfColdkey);
        movable = _unlocked(hotkey, staked);
    }

    // `staked`, the vault's whole position on `hotkey`, less the lock on it, or
    // 0 below the lock.
    function _unlocked(bytes32 hotkey, uint256 staked) private view returns (uint256) {
        // Only the locked amount limits a move; the other fields say how it drains.
        // slither-disable-next-line unused-return
        (, uint64 locked,,,) =
            IStakingCollateral(ISTAKING_ADDRESS).getMinerCollateral(netuid, hotkey, selfColdkey);
        return staked > locked ? staked - locked : 0;
    }

    // Escrow's movable stake: the backing of everything the vault accounts for.
    function _liveEscrowStake() private view returns (uint256) {
        return _unlocked(escrowHotkey, _stake(escrowHotkey, selfColdkey));
    }

    // Requires escrow's movable stake to back everything the vault accounts
    // for. `escrowStake` is escrow's whole position as read after the call's
    // last runtime call; nothing moves stake between that read and this check.
    function _requireBacking(uint256 escrowStake) private view {
        if (_unlocked(escrowHotkey, escrowStake) < escrowAccounted()) revert Underfunded();
    }
}
