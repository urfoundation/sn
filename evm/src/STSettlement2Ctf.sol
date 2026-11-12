// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {Initializable} from "@openzeppelin/contracts-upgradeable/proxy/utils/Initializable.sol";
import {OwnableUpgradeable} from "@openzeppelin/contracts-upgradeable/access/OwnableUpgradeable.sol";
import {UUPSUpgradeable} from "@openzeppelin/contracts-upgradeable/proxy/utils/UUPSUpgradeable.sol";
import {SafeCast} from "@openzeppelin/contracts/utils/math/SafeCast.sol";

import {ISTSettlement2} from "./interfaces/ISTSettlement2.sol";
import {ISTSettlementVault2} from "./interfaces/ISTSettlementVault2.sol";
import {STCoordinator} from "./STCoordinator.sol";
import {NativeBalance} from "./NativeBalance.sol";

/// @title STSettlement2Ctf
/// @notice The capture the flag upgrade of STSettlement2 (CAPTURETHEFLAG.md
/// §3.1), built on the live phase C implementation. Each network operator `n`
/// gains a second vault2 pool, its CTF pool, at the pool id
/// `ctfPoolId(n) = 2^32 + n`:
///
/// - `registerCtfPool` registers it with vault2 under a hotkey derived at
///   execution, so no hotkey is public before its block;
/// - `commitDistribution` takes the CTF pool's roots from `n`'s root signer;
/// - `commitCtfCensus` records each epoch's census hash once, before any draw
///   randomness exists, and `ctfCensus` reads it;
/// - `ctfSettlementAt` resolves the pool for validators;
/// - `registerOperatorPool` refuses an id from 2^32, so no provider pool can
///   take a CTF pool's id.
///
/// Close, approval and finalization already take vault2 pool ids and serve
/// the CTF pool unchanged.
///
/// STSettlement2's registerOperatorPool and commitDistribution are not
/// virtual, and STSettlement2PhaseC's source, with STSettlement2's under it,
/// cannot change: the phase C upgrade fixed its creation code by CREATE2
/// address. So this contract restates both implementations instead of
/// inheriting them. Every function, event, error and storage slot of phase C
/// is kept, unchanged except as noted here, with the state declared in the
/// same order: slots 0 to 50 are phase C's, and the census mapping follows at
/// slot 51 (evm/layout/STSettlement2Ctf.json).
contract STSettlement2Ctf is ISTSettlement2, Initializable, OwnableUpgradeable, UUPSUpgradeable {
    uint256 private constant MAX_AUDITORS = 16;

    /// @dev The phase C sweep root's single leaf holds the whole entitlement.
    uint256 private constant SWEEP_SHARE_BPS = 10_000;

    /// @dev Operator n's CTF pool is vault2 pool CTF_POOL_BASE + n. Above every
    /// v1 operator id and far below 2^53, so JavaScript clients keep it exact.
    uint256 private constant CTF_POOL_BASE = 1 << 32;

    /// @dev A pool's recorded v1 sweep, as phase C keeps it. The epoch is zero
    /// while none is recorded: a sweep epoch is at least the pool's firstEpoch,
    /// which is never zero.
    struct V1Sweep {
        uint64 epoch;
        bytes32 payoutRoot;
        bytes32 artifactHash;
    }

    /// @dev An epoch's committed census. The block is zero while none is
    /// committed: no census is committed at block zero.
    struct CtfCensus {
        bytes32 censusHash;
        uint64 blockNumber;
    }

    // STSettlement2's storage, in its order (evm/layout/STSettlement2.json).
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
    uint256[43] private __gap;

    /// @dev Phase C's mapping, at slot 50 (evm/layout/STSettlement2PhaseC.json).
    mapping(uint256 noId => V1Sweep sweep) private _v1Sweeps;

    /// @dev Each operator's census commitments by epoch, at slot 51.
    mapping(uint256 noId => mapping(uint256 epoch => CtfCensus census)) private _ctfCensuses;

    event V1SweepRecorded(
        uint256 indexed noId, uint256 indexed epoch, bytes32 payoutRoot, bytes32 artifactHash
    );
    event CtfCensusCommitted(
        uint256 indexed noId, uint256 indexed epoch, bytes32 censusHash, uint64 blockNumber
    );

    error NotV1SweepRoot();
    error V1SweepAlreadyRecorded();
    error NoIdOutOfRange();
    error InvalidCensusHash();
    error CensusAlreadyCommitted();
    error BlockHashUnavailable();

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

    /// @notice O, payable. As phase A's, and refuses `noId` from 2^32
    /// (NoIdOutOfRange), the CTF pools' ids.
    function registerOperatorPool(uint256 noId, bytes32 hotkey, uint64 maximumBurnRao, uint64 firstEpoch)
        external
        payable
        onlyOwner
        returns (uint16 uid)
    {
        if (noId >= CTF_POOL_BASE) revert NoIdOutOfRange();
        _requireRegistration(noId, firstEpoch);
        uid = _registerPool(noId, hotkey, maximumBurnRao, firstEpoch);
    }

    /// @notice O, payable. Registers operator `noId`'s CTF pool with vault2 as
    /// pool `ctfPoolId(noId)`, from `firstEpoch`, under a hotkey derived here:
    /// keccak256(abi.encode(this proxy, ctfPoolId(noId), block.number,
    /// blockhash(block.number - 1))). A hotkey named in the scheduled call
    /// would be public for the timelock's delay, and anyone could take it on
    /// the subnet first. This one is known only once the parent block is
    /// sealed, so it can be taken first only inside the executing block, and
    /// a refused registration can run again in a later block under another.
    /// Without the parent's hash it would be known in advance, so a zero hash
    /// is refused (BlockHashUnavailable). It makes registerOperatorPool's
    /// checks, with its cadence check, and requires `noId` below 2^32 and its
    /// provider pool registered with vault2. `msg.value` pays the burn, capped
    /// at `maximumBurnRao`, and the surplus vault2 refunds goes back to the
    /// caller. Executed by the timelock as the call of upgradeToAndCall, the
    /// caller is the timelock.
    function registerCtfPool(uint256 noId, uint64 maximumBurnRao, uint64 firstEpoch)
        external
        payable
        onlyOwner
        returns (uint16 uid)
    {
        uint256 poolId = ctfPoolId(noId);
        _requireRegistration(noId, firstEpoch);
        if (!ISTSettlementVault2(vault).pools(noId).registered) revert UnknownPool();
        bytes32 parent = blockhash(block.number - 1);
        // Zero is the runtime giving no hash at all, not a value anyone steers.
        // slither-disable-next-line incorrect-equality
        if (parent == bytes32(0)) revert BlockHashUnavailable();
        bytes32 hotkey = keccak256(abi.encode(address(this), poolId, block.number, parent));
        uid = _registerPool(poolId, hotkey, maximumBurnRao, firstEpoch);
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

    /// @notice Operator `noId`'s CTF pool for `epoch`: (vault2, its hotkey, its
    /// first epoch) once vault2 holds the pool and `epoch` is at or after its
    /// first epoch, otherwise zeros. A validator checks the hotkey against
    /// vault2's own `pools(ctfPoolId(noId))`.
    function ctfSettlementAt(uint256 noId, uint256 epoch)
        external
        view
        returns (address vault_, bytes32 hotkey, uint64 firstEpoch)
    {
        if (noId >= CTF_POOL_BASE) return (address(0), bytes32(0), 0);
        ISTSettlementVault2.Pool memory pool = ISTSettlementVault2(vault).pools(CTF_POOL_BASE + noId);
        if (!pool.registered || epoch < pool.firstEpoch) return (address(0), bytes32(0), 0);
        return (vault, pool.hotkey, pool.firstEpoch);
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

    /// @notice R, not paused. As phase A's, except that a CTF pool's root
    /// signer is its operator's: for pool id `2^32 + n`, the root signer of
    /// `operatorAt(n, epoch)`.
    function commitDistribution(
        uint256 epoch,
        uint256 noId,
        uint256 prevRootCount,
        ISTSettlementVault2.Commitment calldata commitment
    ) external whenNotPaused returns (uint64 seq) {
        _requirePoolEpoch(epoch, noId);
        STCoordinator v1 = _v1();
        uint256 operatorId = noId >= CTF_POOL_BASE ? noId - CTF_POOL_BASE : noId;
        if (msg.sender != v1.operatorAt(operatorId, epoch).rootSigner) revert Unauthorized();
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

    /// @notice R, not paused, once per operator and epoch. Records `censusHash`,
    /// the sha256 of epoch `epoch`'s census header, for operator `noId`'s CTF
    /// draw. The caller is the root signer of `operatorAt(noId, epoch)`, active
    /// or not; the CTF pool must be registered with `epoch` at or after its
    /// first epoch; and the block must lie in [end(epoch), end(epoch) +
    /// rootCommitWindowBlocks / 2]. The hash is nonzero, and a second
    /// commitment for the epoch is refused, so the candidates are fixed on
    /// chain before the draw's drand round exists.
    function commitCtfCensus(uint256 epoch, uint256 noId, bytes32 censusHash) external whenNotPaused {
        _requirePoolEpoch(epoch, ctfPoolId(noId));
        STCoordinator v1 = _v1();
        if (msg.sender != v1.operatorAt(noId, epoch).rootSigner) revert Unauthorized();
        uint256 end = v1.epochEndBlock(epoch);
        if (block.number < end || block.number > end + v1.policyAt(epoch).rootCommitWindowBlocks / 2) {
            revert InvalidWindow();
        }
        if (censusHash == bytes32(0)) revert InvalidCensusHash();
        CtfCensus storage census = _ctfCensuses[noId][epoch];
        if (census.blockNumber != 0) revert CensusAlreadyCommitted();
        uint64 committedBlock = SafeCast.toUint64(block.number);
        census.censusHash = censusHash;
        census.blockNumber = committedBlock;
        emit CtfCensusCommitted(noId, epoch, censusHash, committedBlock);
    }

    /// @notice Operator `noId`'s census commitment for `epoch`, or zeros while
    /// none is committed.
    function ctfCensus(uint256 noId, uint256 epoch)
        external
        view
        returns (bytes32 censusHash, uint64 blockNumber)
    {
        CtfCensus storage census = _ctfCensuses[noId][epoch];
        return (census.censusHash, census.blockNumber);
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

    /// @notice O. Phase C's: adds `amount` of vault2's unaccounted escrow stake
    /// to pool `noId`'s funding. It enters the funding snapshot of `epoch`,
    /// which must be the pool's next unfunded epoch, `lastFundedEpoch(noId) +
    /// 1`. vault2 refuses a zero amount and one above its surplus,
    /// `liveEscrowStake() - escrowAccounted()` (InvalidSurplus), and emits
    /// SurplusAssigned(noId, epoch, amount).
    function assignSurplus(uint256 noId, uint256 epoch, uint256 amount) external onlyOwner {
        ISTSettlementVault2 v = ISTSettlementVault2(vault);
        if (!v.pools(noId).registered) revert UnknownPool();
        if (epoch != v.lastFundedEpoch(noId) + 1) revert InvalidEpoch();
        v.assignSurplus(noId, amount);
    }

    /// @notice O, once per pool. Phase C's: records `epoch` as pool `noId`'s v1
    /// sweep epoch. The pool must be registered and `epoch` at or after its
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
        STCoordinator v1 = _v1();
        // With one leaf, the root is the leaf, and v1's claim takes an empty proof.
        bytes32 sweepRoot = v1.settlementVault().payoutLeaf(v.selfColdkey(), SWEEP_SHARE_BPS);
        // The checks are the payout root and the artifact hash; the coordinator
        // already restricts who commits.
        // slither-disable-next-line unused-return
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

    /// @notice Operator `noId`'s CTF pool id, 2^32 + noId, for `noId` below
    /// 2^32 (NoIdOutOfRange).
    function ctfPoolId(uint256 noId) public pure returns (uint256) {
        if (noId >= CTF_POOL_BASE) revert NoIdOutOfRange();
        return CTF_POOL_BASE + noId;
    }

    /// @dev registerOperatorPool's checks: v1 operator `noId` active now, a
    /// future first epoch, and the first epoch's cadence, the only place it is
    /// checked: a root committed at the end of its window must clear vault2's
    /// challenge window before the next epoch ends, when it becomes replaceable.
    function _requireRegistration(uint256 noId, uint64 firstEpoch) private view {
        STCoordinator v1 = _v1();
        uint256 epoch = v1.currentEpoch();
        if (!v1.operatorAt(noId, epoch).active) revert InactiveOperator();
        if (firstEpoch <= epoch) revert InvalidEpoch();
        STCoordinator.PolicySnapshot memory p = v1.policyAt(firstEpoch);
        if (p.epochBlocks < uint256(p.rootCommitWindowBlocks) + ISTSettlementVault2(vault).CHALLENGE_BLOCKS())
        {
            revert CadenceTooShort();
        }
    }

    /// @dev Registers vault2 pool `poolId` with `msg.value` and passes vault2's
    /// refund of the burn surplus on to the caller. This call is the pool's
    /// commitment to v2 from `firstEpoch`.
    function _registerPool(uint256 poolId, bytes32 hotkey, uint64 maximumBurnRao, uint64 firstEpoch)
        private
        returns (uint16 uid)
    {
        uint256 balanceBefore = NativeBalance.beforeSuppliedValue(address(this).balance, msg.value);
        uid = ISTSettlementVault2(vault).registerPool{value: msg.value}(
            poolId, hotkey, maximumBurnRao, firstEpoch
        );
        uint256 current = address(this).balance;
        if (current > balanceBefore) {
            (bool refunded,) = payable(msg.sender).call{value: current - balanceBefore}("");
            if (!refunded) revert NativeRefundFailed();
        }
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
}
