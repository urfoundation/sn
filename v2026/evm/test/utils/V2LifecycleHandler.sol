// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {Test} from "forge-std/Test.sol";
import {SafeCast} from "@openzeppelin/contracts/utils/math/SafeCast.sol";

import {STCoordinator} from "../../src/STCoordinator.sol";
import {STSettlement2} from "../../src/STSettlement2.sol";
import {STSettlementVault2} from "../../src/STSettlementVault2.sol";
import {ISTSettlementVault2} from "../../src/interfaces/ISTSettlementVault2.sol";
import {LedgerTree} from "../../src/LedgerTree.sol";
import {ISTAKING_ADDRESS} from "../../src/interfaces/stakingV2.sol";
import {MockStakingV2} from "../mocks/PrecompileMocks.sol";
import {V2TreeHelper} from "./V2TreeHelper.sol";

/// @dev STSettlement2 with the phase C wrapper for vault2's assignSurplus
///      (VERSION2-FINAL.md §16), as a test upgrade.
contract STSettlement2Surplus is STSettlement2 {
    function assignSurplus(uint256 noId, uint256 amount) external onlyOwner {
        ISTSettlementVault2(vault).assignSurplus(noId, amount);
    }
}

/// @dev Drives pools 1 and 2 through the whole settlement v2 stack in random
///      order for the I suite (VERSION2-FINAL.md §15.5): captures and
///      deferrals, honest and faulty commits, approvals, demands, answers,
///      challenges, finalizations, claims, absorption and surplus assignment,
///      with the runtime one rao short at the destination and refusing stake
///      moves on demand. Demands and challenges target the accounts of any
///      finalized root, mostly the latest one (§4.2 G2). Pool 1's root signer
///      commits honest successors only, so the pool cap is checked against
///      honest roots for a whole run; pool 2's may commit a lowered,
///      moved-coldkey, garbage-leaf or inconsistent-subtree successor. Every
///      vault2 refusal is caught: the ghosts record only what happened,
///      measured on the staking mock.
contract V2LifecycleHandler is Test {
    enum Kind {
        Honest,
        Lowered,
        Moved,
        Garbage,
        Inconsistent
    }

    /// @dev A committed ledger as its root signer built it. A faulty one aims
    ///      at the latest finalized root's account `victim`; an Inconsistent
    ///      one understates by `skew` the sum of the node at `level` on the
    ///      victim's path and gives `skew` to its last account instead.
    struct Tree {
        bytes32[] coldkeys;
        uint256[] values;
        Kind kind;
        uint256 victim;
        uint256 level;
        uint256 skew;
    }

    /// @dev Account `index` of finalized root `rootIndex`, demanded or opened.
    struct Target {
        uint256 rootIndex;
        uint256 index;
    }

    /// @dev What the handler saw when a root finalized (I-4): the root's seq,
    ///      the blocks of its commit and finalization, the demands then open
    ///      on it, whether a fault had been shown in it, and whether vault2
    ///      appended exactly that commit.
    struct Finalization {
        uint64 seq;
        uint256 committedBlock;
        uint256 finalizedBlock;
        uint256 openDemands;
        bool faultShown;
        bool asCommitted;
    }

    uint256 internal constant HONEST_POOL = 1;
    uint256 internal constant ROOT_WINDOW = 1_200;
    uint256 internal constant WINDOW = 14_400;
    uint256 internal constant EPOCH_BLOCKS = 50_400;
    uint256 internal constant MAX_ACCOUNTS = 32;
    uint256 internal constant MIN_EMISSION = 1e6;
    uint256 internal constant MAX_EMISSION = 1e12;
    bytes32 internal constant FOREIGN = keccak256("v1-escrow-hotkey");
    MockStakingV2 internal constant STAKING = MockStakingV2(ISTAKING_ADDRESS);

    STSettlement2Surplus internal immutable settlement;
    STSettlementVault2 internal immutable vault;
    STCoordinator internal immutable v1;
    bytes32 internal immutable escrow;
    bytes32 internal immutable self;
    address internal immutable owner;
    uint256 internal immutable fee;
    address internal immutable anyone = makeAddr("lifecycle-anyone");
    address[] internal _auditors;

    mapping(uint256 noId => mapping(uint64 seq => Tree tree)) internal _trees;
    mapping(uint256 noId => mapping(uint64 seq => ISTSettlementVault2.Commitment commitment)) internal
        _commitments;
    mapping(uint256 noId => mapping(uint64 seq => uint256 epoch)) internal _committedEpoch;
    mapping(uint256 noId => mapping(uint64 seq => uint256 blockNumber)) internal _committedBlock;
    mapping(uint256 noId => mapping(uint64 seq => Target[] targets)) internal _demanded;
    mapping(uint256 noId => uint64[] seqs) internal _finalizedSeqs;
    mapping(uint256 noId => Finalization[] records) internal _finalizations;
    mapping(uint256 noId => uint256 minted) internal _minted;

    // Ghosts, each measured when it happened.
    mapping(uint256 noId => uint256 amount) public ghostCaptured;
    mapping(uint256 noId => uint256 amount) public ghostClaimed;
    mapping(uint256 noId => mapping(uint256 epoch => uint256 amount)) internal _ghostFunded;
    uint256 public ghostSurplus;
    mapping(uint256 noId => mapping(uint64 seq => uint256 count)) public openDemands;
    mapping(uint256 noId => mapping(uint64 seq => bool shown)) public faultShown;
    mapping(uint256 noId => mapping(uint256 index => uint256 amount)) internal _maxCumulative;
    mapping(uint256 noId => uint256 bound) public indexBound;
    mapping(uint256 noId => bool faulty) public tainted;
    mapping(uint256 noId => uint256 count) public honestCapRefusals;

    // One-shot runtime failures, armed by `runtime`.
    bool internal _failNextMove;
    bool internal _failNextTransfer;
    bool internal _floorNextTransfer;

    uint256 public closes;
    uint256 public commits;
    uint256 public approvals;
    uint256 public demands;
    uint256 public answers;
    uint256 public challenges;
    uint256 public finalizations;
    uint256 public claims;
    uint256 public absorbs;
    uint256 public assigns;

    constructor(STSettlement2Surplus settlement_) {
        settlement = settlement_;
        vault = STSettlementVault2(settlement_.vault());
        v1 = STCoordinator(payable(settlement_.v1Coordinator()));
        escrow = vault.escrowHotkey();
        self = vault.selfColdkey();
        owner = settlement_.owner();
        fee = vault.demandFeeWei();
        _auditors = settlement_.auditors();
    }

    // ------------------------------------------------------------------
    // Fuzzed actions
    // ------------------------------------------------------------------

    /// @dev Emission arrives, possibly none or dust, and anyone closes an
    ///      epoch whose window is open, deferring up to two skipped epochs.
    function close(uint256 poolSeed, uint256 skip, uint256 amount) external {
        _close(_pool(poolSeed), skip % 3, amount % MAX_EMISSION);
    }

    /// @dev The root signer commits a successor of the latest finalized root,
    ///      closing a fresh epoch first when no commit is possible now.
    function commit(uint256 poolSeed, uint256 kindSeed, uint256 shapeSeed) external {
        uint256 noId = _pool(poolSeed);
        _commitFresh(noId, noId == HONEST_POOL ? Kind.Honest : Kind(kindSeed % 5), shapeSeed);
    }

    function approve(uint256 poolSeed, uint256 auditorSeed) external {
        uint256 noId = _pool(poolSeed);
        ISTSettlementVault2.Pending memory next = vault.pending(noId);
        if (next.seq != 0) _approve(noId, next, _auditors[auditorSeed % _auditors.length]);
    }

    /// @dev Anyone pays the fee and demands the opening of an account of a
    ///      finalized root, usually the one a faulty pending root hides.
    function demand(uint256 poolSeed, uint256 kindSeed, uint256 indexSeed) external {
        uint256 noId = _pool(poolSeed);
        if (!_dispute(noId, kindSeed, indexSeed)) return;
        uint64 seq = vault.pending(noId).seq;
        Target memory target = _aim(noId, seq, indexSeed);
        Tree memory f = _finalized(noId, target.rootIndex);
        LedgerTree.ProofNode[] memory proof = V2TreeHelper.proof(_levels(noId, f), target.index);
        vm.deal(anyone, fee);
        vm.prank(anyone);
        try vault.demandPath{value: fee}(noId, target.rootIndex, _accountOf(f, target.index), proof) {
            openDemands[noId][seq]++;
            _demanded[noId][seq].push(target);
            demands++;
        } catch {}
    }

    /// @dev The operator opens a demanded account of the pending root.
    function answer(uint256 poolSeed, uint256 seed) external {
        uint256 noId = _pool(poolSeed);
        uint64 seq = vault.pending(noId).seq;
        uint256 count = _finalizedSeqs[noId].length;
        if (seq == 0 || count == 0) return;
        Target[] storage demanded = _demanded[noId][seq];
        Target memory target = demanded.length != 0
            ? demanded[seed % demanded.length]
            : Target(count - 1, seed % _latestCount(noId));
        _open(noId, seq, target);
    }

    /// @dev A watcher opens an account of the pending root, usually its victim.
    function challenge(uint256 poolSeed, uint256 kindSeed, uint256 seed) external {
        uint256 noId = _pool(poolSeed);
        if (!_dispute(noId, kindSeed, seed)) return;
        uint64 seq = vault.pending(noId).seq;
        _open(noId, seq, _aim(noId, seq, seed));
    }

    /// @dev The server drives a root to finalization (§8.1).
    function finalize(uint256 poolSeed, uint256 seed) external {
        _driveFinalize(_pool(poolSeed), seed);
    }

    /// @dev Anyone tries to finalize after a random wait, ready or not.
    function finalizeNow(uint256 poolSeed, uint256 blocks) external {
        vm.roll(block.number + blocks % (2 * WINDOW));
        _finalize(_pool(poolSeed));
    }

    /// @dev Anyone claims an account with something left to claim, finalizing
    ///      a root first when there is none.
    function claim(uint256 poolSeed, uint256 seed) external {
        uint256 noId = _pool(poolSeed);
        (bool found, uint256 rootIndex, uint256 index) = _claimable(noId, seed);
        if (!found) {
            if (!_driveFinalize(noId, seed)) return;
            (found, rootIndex, index) = _claimable(noId, seed);
            if (!found) return;
        }
        Tree memory t = _trees[noId][_finalizedSeqs[noId][rootIndex]];
        _claim(noId, rootIndex, t, index, t.values[index]);
    }

    /// @dev Anyone claims a leaf of any committed root, finalized or not,
    ///      against any root index, sometimes overstated by one rao.
    function claimAny(uint256 poolSeed, uint256 sourceSeed, uint256 indexSeed, uint256 rootSeed) external {
        uint256 noId = _pool(poolSeed);
        uint256 seqs = vault.commitSeq(noId);
        if (seqs == 0) return;
        Tree memory t = _trees[noId][SafeCast.toUint64(1 + sourceSeed % seqs)];
        uint256 index = indexSeed % t.values.length;
        uint256 cumulative = t.values[index] + (indexSeed >> 128) % 2;
        _claim(noId, rootSeed % (_finalizedSeqs[noId].length + 1), t, index, cumulative);
    }

    /// @dev Stake vault2 owns on a foreign hotkey, such as the v1 escrow, is
    ///      absorbed into escrow as surplus.
    function absorb(uint256 amount) external {
        STAKING.setStake(FOREIGN, self, STAKING.stakes(FOREIGN, self) + amount % MAX_EMISSION);
        uint256 escrowBefore = STAKING.stakes(escrow, self);
        bool armed = _armMove();
        vm.prank(anyone);
        try vault.absorbStake(FOREIGN) {
            ghostSurplus += STAKING.stakes(escrow, self) - escrowBefore;
            absorbs++;
        } catch {}
        _disarmMove(armed);
    }

    /// @dev The owner assigns surplus through the test upgrade, at most one
    ///      rao more than there is.
    function assignSurplus(uint256 poolSeed, uint256 amountSeed) external {
        uint256 noId = _pool(poolSeed);
        uint256 amount = 1 + amountSeed % (ghostSurplus + 1);
        vm.prank(owner);
        try settlement.assignSurplus(noId, amount) {
            ghostCaptured[noId] += amount;
            ghostSurplus = amount > ghostSurplus ? 0 : ghostSurplus - amount;
            assigns++;
        } catch {}
    }

    /// @dev Sets the runtime's rounding at the destination of moves and
    ///      transfers, and arms a refusal of the next move or transfer.
    function runtime(uint256 seed) external {
        STAKING.setMoveStakeShortfall(seed % 2);
        STAKING.setTransferStakeShortfall((seed >> 1) % 2);
        uint256 failure = (seed >> 2) % 4;
        _failNextMove = failure == 1;
        _failNextTransfer = failure == 2;
        _floorNextTransfer = failure == 3;
    }

    /// @dev Time passes: windows, deadlines and whole epochs may lapse.
    function wait(uint256 blocks) external {
        vm.roll(block.number + blocks % (EPOCH_BLOCKS + WINDOW));
    }

    // ------------------------------------------------------------------
    // Views for the invariants
    // ------------------------------------------------------------------

    function finalizationsOf(uint256 noId) external view returns (Finalization[] memory) {
        return _finalizations[noId];
    }

    /// @dev The ghost funding snapshots of epochs `first` to `last`.
    function ghostFundedOf(uint256 noId, uint256 first, uint256 last)
        external
        view
        returns (uint256[] memory snapshots)
    {
        snapshots = new uint256[](last + 1 - first);
        for (uint256 i; i < snapshots.length; i++) {
            snapshots[i] = _ghostFunded[noId][first + i];
        }
    }

    /// @dev The largest cumulative a finalized root proves at each index any
    ///      committed root has.
    function maxCumulativeOf(uint256 noId) external view returns (uint256[] memory maxima) {
        maxima = new uint256[](indexBound[noId]);
        for (uint256 i; i < maxima.length; i++) {
            maxima[i] = _maxCumulative[noId][i];
        }
    }

    // ------------------------------------------------------------------
    // Lifecycle steps
    // ------------------------------------------------------------------

    function _pool(uint256 seed) internal pure returns (uint256) {
        return 1 + seed % 2;
    }

    // The earliest epoch whose close and commit window has not passed.
    function _openEpoch() internal view returns (uint256) {
        uint256 current = v1.currentEpoch();
        if (current != 0 && block.number <= v1.epochEndBlock(current - 1) + ROOT_WINDOW) return current - 1;
        return current;
    }

    function _close(uint256 noId, uint256 skip, uint256 amount) internal returns (bool closed) {
        uint256 firstMissed = vault.lastFundedEpoch(noId) + 1;
        uint256 epoch = firstMissed + skip;
        uint256 open = _openEpoch();
        if (epoch < open) epoch = open;
        uint256 end = v1.epochEndBlock(epoch);
        if (block.number < end) vm.roll(end);
        bytes32 hotkey = vault.pools(noId).hotkey;
        STAKING.setStake(hotkey, self, STAKING.stakes(hotkey, self) + amount);
        uint256 escrowBefore = STAKING.stakes(escrow, self);
        bool armed = _armMove();
        vm.prank(anyone);
        try settlement.closeOperatorEpoch(epoch, noId) {
            for (uint256 missed = firstMissed; missed < epoch; missed++) {
                _ghostFunded[noId][missed] = ghostCaptured[noId];
            }
            ghostCaptured[noId] += STAKING.stakes(escrow, self) - escrowBefore;
            _ghostFunded[noId][epoch] = ghostCaptured[noId];
            closes++;
            closed = true;
        } catch {}
        _disarmMove(armed);
    }

    // Whether the pool can commit now: its last funded epoch's window is open,
    // that epoch follows the latest finalized root and its funding grew, the
    // pending slot is free or dead, and vault2's commit spacing has passed.
    function _commitOpen(uint256 noId) internal view returns (bool) {
        uint256 epoch = vault.lastFundedEpoch(noId);
        uint256 end = v1.epochEndBlock(epoch);
        if (block.number < end || block.number > end + ROOT_WINDOW) return false;
        uint256 count = vault.rootCount(noId);
        if (count == 0 ? epoch < vault.pools(noId).firstEpoch : epoch <= vault.roots(noId, count - 1).epoch) {
            return false;
        }
        if (vault.fundedThrough(noId, epoch) <= vault.finalizedTotal(noId)) return false;
        ISTSettlementVault2.Pending memory next = vault.pending(noId);
        if (next.seq != 0 && block.number < v1.epochEndBlock(uint256(next.epoch) + 1)) return false;
        uint256 last = vault.lastCommitBlock(noId);
        return last == 0 || block.number >= last + WINDOW;
    }

    // A pending root over a finalized one, still open to demands: a root past
    // its window is finalized or replaced first, and the finalized root made
    // if there is none. Pool 2's new root may be faulty.
    function _dispute(uint256 noId, uint256 kindSeed, uint256 seed) internal returns (bool) {
        if (_finalizedSeqs[noId].length == 0 && !_driveFinalize(noId, seed)) return false;
        ISTSettlementVault2.Pending memory next = vault.pending(noId);
        if (next.seq != 0 && block.number < uint256(next.committedBlock) + WINDOW) return true;
        if (next.seq != 0) _driveFinalize(noId, seed);
        if (vault.pending(noId).seq != 0) return false;
        return _commitFresh(noId, noId == HONEST_POOL ? Kind.Honest : Kind(kindSeed % 5), seed);
    }

    function _commitFresh(uint256 noId, Kind kind, uint256 seed) internal returns (bool) {
        if (!_commitOpen(noId) && !_close(noId, 0, MIN_EMISSION + seed % MAX_EMISSION)) return false;
        return _commit(noId, kind, seed);
    }

    function _commit(uint256 noId, Kind kind, uint256 seed) internal returns (bool) {
        uint256 epoch = vault.lastFundedEpoch(noId);
        Tree memory tree = _successor(noId, _ghostFunded[noId][epoch], kind, seed);
        if (tree.values.length == 0) return false;
        ISTSettlementVault2.Commitment memory commitment = _commitment(noId, tree);
        uint256 rootCount = vault.rootCount(noId);
        vm.prank(v1.operatorAt(noId, epoch).rootSigner);
        try settlement.commitDistribution(epoch, noId, rootCount, commitment) returns (uint64 seq) {
            _trees[noId][seq] = tree;
            _commitments[noId][seq] = commitment;
            _committedEpoch[noId][seq] = epoch;
            _committedBlock[noId][seq] = block.number;
            if (tree.values.length > indexBound[noId]) indexBound[noId] = tree.values.length;
            commits++;
            return true;
        } catch {
            return false;
        }
    }

    function _approve(uint256 noId, ISTSettlementVault2.Pending memory next, address auditor) internal {
        vm.prank(auditor);
        try settlement.approveDistribution(noId, next.seq, next.root, next.ledgerHash) {
            approvals++;
        } catch {}
    }

    // Opens the target's account in the pending root `seq`. Returns false once
    // the opening shows a fault, which deletes it.
    function _open(uint256 noId, uint64 seq, Target memory target) internal returns (bool pending) {
        Tree memory f = _finalized(noId, target.rootIndex);
        LedgerTree.ProofNode[] memory oldProof = V2TreeHelper.proof(_levels(noId, f), target.index);
        LedgerTree.PathNode[] memory newPath =
            V2TreeHelper.path(_levels(noId, _trees[noId][seq]), target.index);
        vm.prank(anyone);
        try vault.checkPath(noId, target.rootIndex, _accountOf(f, target.index), oldProof, newPath) returns (
            LedgerTree.Fault fault
        ) {
            if (fault != LedgerTree.Fault.None) {
                faultShown[noId][seq] = true;
                challenges++;
                return false;
            }
            if (openDemands[noId][seq] != 0) openDemands[noId][seq]--;
            answers++;
        } catch {}
        return true;
    }

    // Finalizes the pool's pending root as the server would: it replaces a
    // root that can no longer reach a quorum, approves, waits out the window
    // and answers every open demand. A demand that shows a fault deletes the
    // root, and the drive tries once more with a fresh one.
    function _driveFinalize(uint256 noId, uint256 seed) internal returns (bool) {
        for (uint256 attempt; attempt < 2; attempt++) {
            ISTSettlementVault2.Pending memory next = vault.pending(noId);
            if (next.seq == 0 || !_approvable(noId, next)) {
                if (!_commitFresh(noId, Kind.Honest, seed ^ attempt)) return false;
                next = vault.pending(noId);
            }
            uint256 quorum = settlement.quorum();
            for (uint256 i; i < _auditors.length && settlement.approvalCount(noId) < quorum; i++) {
                if (!settlement.hasApproved(noId, _auditors[i])) _approve(noId, next, _auditors[i]);
            }
            uint256 opens = uint256(next.committedBlock) + WINDOW;
            if (block.number < opens) vm.roll(opens + seed % 64);
            if (_answerAll(noId, next.seq) && _finalize(noId)) return true;
        }
        return false;
    }

    function _approvable(uint256 noId, ISTSettlementVault2.Pending memory next) internal view returns (bool) {
        return settlement.approvalCount(noId) >= settlement.quorum()
            || block.number < v1.epochEndBlock(uint256(next.epoch) + 1);
    }

    function _answerAll(uint256 noId, uint64 seq) internal returns (bool) {
        Target[] storage demanded = _demanded[noId][seq];
        for (uint256 k; k < demanded.length; k++) {
            Target memory target = demanded[k];
            if (
                vault.demanded(noId, seq, target.rootIndex, target.index)
                        == ISTSettlementVault2.DemandState.Open && !_open(noId, seq, target)
            ) return false;
        }
        return true;
    }

    function _finalize(uint256 noId) internal returns (bool) {
        uint64 seq = vault.pending(noId).seq;
        vm.prank(anyone);
        try settlement.finalizeDistribution(noId) returns (uint256 rootIndex) {
            _finalizedSeqs[noId].push(seq);
            _finalizations[noId].push(
                Finalization({
                    seq: seq,
                    committedBlock: _committedBlock[noId][seq],
                    finalizedBlock: block.number,
                    openDemands: openDemands[noId][seq],
                    faultShown: faultShown[noId][seq],
                    asCommitted: rootIndex + 1 == _finalizedSeqs[noId].length
                        && _isCommit(noId, seq, rootIndex)
                })
            );
            Tree storage t = _trees[noId][seq];
            if (t.kind != Kind.Honest) tainted[noId] = true;
            for (uint256 i; i < t.values.length; i++) {
                if (_provable(t, i) && t.values[i] > _maxCumulative[noId][i]) {
                    _maxCumulative[noId][i] = t.values[i];
                }
            }
            finalizations++;
            return true;
        } catch {
            return false;
        }
    }

    // Whether finalized root `rootIndex` is exactly the commit of `seq`.
    function _isCommit(uint256 noId, uint64 seq, uint256 rootIndex) internal view returns (bool) {
        if (rootIndex >= vault.rootCount(noId)) return false;
        ISTSettlementVault2.Root memory root = vault.roots(noId, rootIndex);
        ISTSettlementVault2.Commitment storage c = _commitments[noId][seq];
        return root.root == c.root && root.total == c.total && root.leafCount == c.leafCount
            && root.epoch == _committedEpoch[noId][seq] && root.ledgerHash == c.ledgerHash;
    }

    // An account with something left to claim, in the latest finalized root
    // or, one time in four, in an older one.
    function _claimable(uint256 noId, uint256 seed)
        internal
        view
        returns (bool found, uint256 rootIndex, uint256 index)
    {
        uint256 count = _finalizedSeqs[noId].length;
        if (count == 0) return (false, 0, 0);
        rootIndex = seed % 4 == 0 ? (seed >> 2) % count : count - 1;
        (found, index) = _claimableIn(noId, rootIndex, seed);
        if (!found && rootIndex != count - 1) {
            rootIndex = count - 1;
            (found, index) = _claimableIn(noId, rootIndex, seed);
        }
    }

    function _claimableIn(uint256 noId, uint256 rootIndex, uint256 seed)
        internal
        view
        returns (bool, uint256)
    {
        Tree storage t = _trees[noId][_finalizedSeqs[noId][rootIndex]];
        uint256 m = t.values.length;
        for (uint256 k; k < m; k++) {
            uint256 i = ((seed >> 8) + k) % m;
            if (_provable(t, i) && t.values[i] > vault.claimed(noId, i)) return (true, i);
        }
        return (false, 0);
    }

    function _claim(uint256 noId, uint256 rootIndex, Tree memory t, uint256 index, uint256 cumulative)
        internal
    {
        LedgerTree.Account memory account = LedgerTree.Account(index, t.coldkeys[index], cumulative);
        LedgerTree.ProofNode[] memory proof = V2TreeHelper.proof(_levels(noId, t), index);
        uint256 escrowBefore = STAKING.stakes(escrow, self);
        bool armed = _armTransfer();
        vm.prank(anyone);
        try vault.claim(noId, rootIndex, account, proof) {
            ghostClaimed[noId] += escrowBefore - STAKING.stakes(escrow, self);
            claims++;
        } catch (bytes memory reason) {
            // Revert data starts with the refusal's 4-byte selector.
            // forge-lint: disable-next-line(unsafe-typecast)
            if (!tainted[noId] && bytes4(reason) == ISTSettlementVault2.PoolCapExceeded.selector) {
                honestCapRefusals[noId]++;
            }
        }
        _disarmTransfer(armed);
    }

    // ------------------------------------------------------------------
    // Ledgers
    // ------------------------------------------------------------------

    // A successor of the latest finalized root F with total `total`: every
    // provable account of F keeps its position and coldkey and at least its
    // value, new accounts are appended, and the new funding is spread at
    // random. An empty tree when the total does not exceed F's provable sum.
    function _successor(uint256 noId, uint256 total, Kind kind, uint256 seed)
        internal
        returns (Tree memory t)
    {
        bytes32[] memory keys;
        uint256[] memory base;
        uint256 sum;
        if (_finalizedSeqs[noId].length != 0) {
            Tree storage f = _latest(noId);
            keys = f.coldkeys;
            base = f.values;
            for (uint256 i; i < base.length; i++) {
                if (!_provable(f, i)) base[i] = 0;
                sum += base[i];
            }
        }
        if (total <= sum) return t;
        uint256 n = base.length;
        uint256 added = n == 0 ? 1 + seed % 3 : n < MAX_ACCOUNTS ? seed % 3 : 0;
        if (n == 0) kind = Kind.Honest;
        // A faulty successor appends an account for the value it diverts.
        if (kind != Kind.Honest) added++;
        if (added > total - sum) added = total - sum;
        uint256 m = n + added;
        t.coldkeys = new bytes32[](m);
        t.values = new uint256[](m);
        for (uint256 i; i < m; i++) {
            t.coldkeys[i] = i < n ? keys[i] : _freshColdkey(noId);
            t.values[i] = i < n ? base[i] : 1;
        }
        _spread(t.values, total - sum - added, seed);
        t.kind = kind;
        if (kind != Kind.Honest) _corrupt(noId, t, base, seed);
    }

    function _spread(uint256[] memory values, uint256 amount, uint256 seed) internal pure {
        uint256 m = values.length;
        uint256[] memory weights = new uint256[](m);
        uint256 weightSum;
        for (uint256 i; i < m; i++) {
            weights[i] = uint256(keccak256(abi.encode(seed, "weight", i))) % 4;
            weightSum += weights[i];
        }
        uint256 given;
        for (uint256 i; weightSum != 0 && i < m; i++) {
            uint256 share = amount * weights[i] / weightSum;
            values[i] += share;
            given += share;
        }
        values[m - 1] += amount - given;
    }

    // Turns the honest successor `t` faulty at a provable account of F.
    function _corrupt(uint256 noId, Tree memory t, uint256[] memory base, uint256 seed) internal {
        Tree storage f = _latest(noId);
        uint256 n = base.length;
        uint256 m = t.values.length;
        uint256 draw = uint256(keccak256(abi.encode(seed, "fault")));
        t.victim = n;
        for (uint256 k; k < n && t.victim == n; k++) {
            if (_provable(f, (draw % n + k) % n)) t.victim = (draw % n + k) % n;
        }
        if (t.victim == n) {
            t.kind = Kind.Honest;
            t.victim = 0;
            return;
        }
        uint256 victim = t.victim;
        if (t.kind == Kind.Lowered) {
            if (base[victim] != 0) {
                uint256 lowered = (draw >> 64) % base[victim];
                t.values[m - 1] += t.values[victim] - lowered;
                t.values[victim] = lowered;
                return;
            }
            t.kind = Kind.Moved;
        }
        if (t.kind == Kind.Inconsistent) {
            uint256 level = 1 + (draw >> 64) % LedgerTree.depth(m);
            uint256 first = (victim >> level) << level;
            uint256 stated;
            for (uint256 i = first; i < first + 2 ** level && i < m; i++) {
                stated += t.values[i];
            }
            if (stated != 0) {
                t.level = level;
                t.skew = 1 + (draw >> 128) % stated;
                t.values[m - 1] += t.skew;
                return;
            }
            t.kind = Kind.Moved;
        }
        if (t.kind == Kind.Moved) t.coldkeys[victim] = _freshColdkey(noId);
        // A Garbage tree states a hash at the victim that is no leaf hash.
    }

    function _levels(uint256 noId, Tree memory t)
        internal
        pure
        returns (V2TreeHelper.Node[][] memory levels)
    {
        levels = V2TreeHelper.build(t.coldkeys, t.values);
        if (t.kind == Kind.Garbage) {
            levels[0][t.victim].hash = keccak256(abi.encode("v2-invariant-garbage-leaf", noId, t.victim));
            V2TreeHelper.rehash(levels, 0);
        } else if (t.kind == Kind.Inconsistent) {
            levels[t.level][t.victim >> t.level].sum -= t.skew;
            V2TreeHelper.rehash(levels, t.level);
        }
    }

    function _commitment(uint256 noId, Tree memory t)
        internal
        pure
        returns (ISTSettlementVault2.Commitment memory)
    {
        V2TreeHelper.Node memory top = V2TreeHelper.root(_levels(noId, t));
        return ISTSettlementVault2.Commitment({
            root: top.hash,
            total: top.sum,
            leafCount: SafeCast.toUint64(t.values.length),
            ledgerHash: keccak256(abi.encode("v2-invariant-ledger", noId, top.hash))
        });
    }

    // Whether a claim proof for position `i` of `t` can verify: a garbage leaf
    // has no preimage, and nothing below an inconsistent node folds to the
    // sum its parent states for it.
    function _provable(Tree storage t, uint256 i) internal view returns (bool) {
        if (t.kind == Kind.Garbage) return i != t.victim;
        if (t.kind == Kind.Inconsistent) return i >> t.level != t.victim >> t.level;
        return true;
    }

    function _finalized(uint256 noId, uint256 rootIndex) internal view returns (Tree storage) {
        return _trees[noId][_finalizedSeqs[noId][rootIndex]];
    }

    function _latest(uint256 noId) internal view returns (Tree storage) {
        return _finalized(noId, _finalizedSeqs[noId].length - 1);
    }

    function _latestCount(uint256 noId) internal view returns (uint256) {
        return _latest(noId).values.length;
    }

    // An account to demand or open, of the latest finalized root three times
    // in four and otherwise of any finalized root: a faulty pending root's
    // victim three times in four when that root holds it, else any account.
    function _aim(uint256 noId, uint64 seq, uint256 seed) internal view returns (Target memory target) {
        uint256 count = _finalizedSeqs[noId].length;
        target.rootIndex = (seed >> 128) % 4 == 0 ? (seed >> 130) % count : count - 1;
        uint256 accounts = _finalized(noId, target.rootIndex).values.length;
        Tree storage p = _trees[noId][seq];
        bool victim = p.kind != Kind.Honest && seed % 4 != 0 && p.victim < accounts;
        target.index = victim ? p.victim : (seed >> 2) % accounts;
    }

    function _accountOf(Tree memory t, uint256 index) internal pure returns (LedgerTree.Account memory) {
        return LedgerTree.Account(index, t.coldkeys[index], t.values[index]);
    }

    function _freshColdkey(uint256 noId) internal returns (bytes32) {
        return keccak256(abi.encode("v2-invariant-coldkey", noId, ++_minted[noId]));
    }

    // ------------------------------------------------------------------
    // Runtime failures
    // ------------------------------------------------------------------

    function _armMove() internal returns (bool armed) {
        armed = _failNextMove;
        if (armed) {
            _failNextMove = false;
            STAKING.setFailMoveStake(true);
        }
    }

    function _disarmMove(bool armed) internal {
        if (armed) STAKING.setFailMoveStake(false);
    }

    function _armTransfer() internal returns (bool armed) {
        armed = _failNextTransfer || _floorNextTransfer;
        if (_failNextTransfer) STAKING.setFailTransferStake(true);
        if (_floorNextTransfer) STAKING.setMinimumTransferAmount(type(uint256).max);
        (_failNextTransfer, _floorNextTransfer) = (false, false);
    }

    function _disarmTransfer(bool armed) internal {
        if (armed) {
            STAKING.setFailTransferStake(false);
            STAKING.setMinimumTransferAmount(0);
        }
    }
}
