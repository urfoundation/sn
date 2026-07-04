// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {STBase} from "./utils/STBase.sol";
import {STSubnet} from "../src/STSubnet.sol";

/// @dev Area 1 — epoch lifecycle end to end: push-then-credit deposits and the
///      buyback reserve (v0.3/D23: every deposit moves in full to the reserve
///      hotkey; the escrow ledger tracks only claimable emission), commit
///      windows, missed-commit carry, stake-delta emission measurement, lazy
///      rolls (incl. multi-roll and the MAX_ROLLS_PER_CALL backlog), sweep
///      retry exactness, permissionless in-order finalize, and a 3-epoch E2E
///      with exact α conservation.
contract EpochLifecycleTest is STBase {
    uint256 constant DEPOSIT = 1_000e9;
    uint256 constant EMISSION = 500e9;

    event Deposited(uint256 indexed e, uint256 indexed noId, address from, uint256 amount);
    event BuybackReserved(
        uint256 indexed e, uint256 indexed noId, uint256 amount, uint256 buybackTotal
    );
    event EpochRolled(uint256 indexed closedEpoch, uint256 indexed newEpoch, uint64 closeBlock);
    event PoolSwept(uint256 indexed noId, uint256 measured, uint256 swept, bool moveOk);
    event PoolCarried(uint256 indexed e, uint256 indexed noId, uint256 carried);
    event PoolFinalized(uint256 indexed e, uint256 indexed noId, uint256 poolTotal);

    // ------------------------------------------------------------------
    // deposits: push-then-credit + the buyback reserve
    // ------------------------------------------------------------------

    function test_deposit_pushThenCredit_partialAttribution() public {
        _push(100e9); // one push, attributed in two slices
        vm.prank(noAddr);
        st.deposit(NO_ID, 60e9);
        assertEq(_reserveStake(), 60e9);
        assertEq(_treasuryStake(), 40e9, "unattributed remainder stays escrowed");

        vm.prank(noAddr);
        st.deposit(NO_ID, 40e9);
        assertEq(_reserveStake(), 100e9);
        assertEq(st.buybackTotal(), 100e9);
        assertEq(st.accountedStake(), 0, "reserved alpha never enters the escrow ledger");

        // push fully attributed (and reserved): nothing left to credit
        vm.prank(noAddr);
        vm.expectRevert("ST: stake not received");
        st.deposit(NO_ID, 1);
    }

    function test_deposit_doubleAttribution_reverts() public {
        _push(50e9);
        vm.prank(noAddr);
        st.deposit(NO_ID, 50e9);
        // same push cannot be attributed twice — the α already left for the reserve
        vm.prank(noAddr);
        vm.expectRevert("ST: stake not received");
        st.deposit(NO_ID, 50e9);
    }

    /// @dev Documented v1 trust posture (README deviation 3): a pushed but
    ///      not-yet-credited amount is attributable by ANY authorized NO
    ///      operator — acceptable for the owner-gated single-NO launch. Under
    ///      D25 the α is reserved either way; only the Deposited event's noId
    ///      (the sole attribution now) is mis-bookable.
    function test_deposit_unattributedPush_attributableByOtherAuthorizedNO() public {
        _registerOperator2();
        _push(50e9); // pushed by (say) NO1's wallet, never credited...
        // ...and NO2's operator attributes it first: the Deposited event books
        // it under NO2, not NO1 (the event log is the only attribution now, D25)
        vm.expectEmit(true, true, false, true, address(st));
        emit Deposited(0, NO2_ID, no2Addr, 50e9);
        vm.prank(no2Addr);
        st.deposit(NO2_ID, 50e9);
        assertEq(_reserveStake(), 50e9);
    }

    function test_deposit_ledgerAndEvents() public {
        uint256 amt = 1_000_000_000_007; // odd amount: no rounding anywhere (full amount reserved)
        _push(amt);
        vm.expectEmit(true, true, false, true, address(st));
        emit Deposited(0, NO_ID, noAddr, amt);
        vm.expectEmit(true, true, false, true, address(st));
        emit BuybackReserved(0, NO_ID, amt, amt);
        vm.prank(noAddr);
        st.deposit(NO_ID, amt);

        // per-deposit identity: gross deposit == reserved amount, to the rao
        assertEq(st.buybackTotal(), amt);
        assertEq(_reserveStake(), amt);
        assertEq(_treasuryStake(), 0);
    }

    function test_deposit_guards() public {
        _push(10e9);
        vm.prank(noAddr);
        vm.expectRevert("ST: amount 0");
        st.deposit(NO_ID, 0);

        vm.prank(rando);
        vm.expectRevert("ST: not operator");
        st.deposit(NO_ID, 1e9);

        vm.prank(owner);
        st.setOperatorActive(NO_ID, false);
        vm.prank(noAddr);
        vm.expectRevert("ST: operator inactive");
        st.deposit(NO_ID, 1e9);
        vm.prank(owner);
        st.setOperatorActive(NO_ID, true);

        vm.prank(guardian);
        st.setPaused(true);
        vm.prank(noAddr);
        vm.expectRevert("ST: paused");
        st.deposit(NO_ID, 1e9);
        vm.prank(guardian);
        st.setPaused(false);

        // owner may credit on the NO's behalf
        vm.prank(owner);
        st.deposit(NO_ID, 1e9);
        assertEq(st.buybackTotal(), 1e9);
        assertEq(_reserveStake(), 1e9);
    }

    /// @dev The one-way invariant's deposit leg: a failed reserve move rolls
    ///      back the entire credit (nothing reserved -> buybackTotal unchanged).
    function test_deposit_reserveMoveFailure_revertsWholeCredit() public {
        _push(10e9);
        staking.setFailMoveStake(true);
        vm.prank(noAddr);
        vm.expectRevert("mock: moveStake down");
        st.deposit(NO_ID, 10e9);
        assertEq(st.buybackTotal(), 0);
        assertEq(_treasuryStake(), 10e9, "push intact for a retry");
    }

    // ------------------------------------------------------------------
    // commitOperator windows
    // ------------------------------------------------------------------

    function test_commitOperator_insideWindow_andRecommit() public {
        _afterClose(0);
        vm.prank(noAddr);
        st.commitOperator(0, NO_ID, keccak256("root-1"), "ipfs://v1");
        (bytes32 root,) = st.noCommit(0, NO_ID);
        assertEq(root, keccak256("root-1"));

        // re-commit (ops fix-up) allowed anywhere inside the window
        vm.roll(_closeBlock(0) + COMMIT_W); // window boundary is inclusive
        vm.prank(owner); // owner may also act for the NO
        st.commitOperator(0, NO_ID, keccak256("root-2"), "ipfs://v2");
        (root,) = st.noCommit(0, NO_ID);
        assertEq(root, keccak256("root-2"));
    }

    function test_commitOperator_outsideWindow_reverts() public {
        vm.roll(_closeBlock(0) + COMMIT_W + 1);
        vm.prank(noAddr);
        vm.expectRevert("ST: commit window");
        st.commitOperator(0, NO_ID, keccak256("root"), "");
    }

    function test_commitOperator_openOrFutureEpoch_reverts() public {
        vm.prank(noAddr);
        vm.expectRevert("ST: epoch open");
        st.commitOperator(0, NO_ID, keccak256("root"), "");

        _afterClose(0); // epoch 1 open now
        vm.prank(noAddr);
        vm.expectRevert("ST: epoch open");
        st.commitOperator(1, NO_ID, keccak256("root"), "");
    }

    function test_commitOperator_guards() public {
        _afterClose(0);
        vm.prank(rando);
        vm.expectRevert("ST: not operator");
        st.commitOperator(0, NO_ID, keccak256("root"), "");

        vm.prank(noAddr);
        vm.expectRevert("ST: root 0");
        st.commitOperator(0, NO_ID, bytes32(0), "");

        vm.prank(owner);
        st.setOperatorActive(NO_ID, false);
        vm.prank(noAddr);
        vm.expectRevert("ST: operator inactive");
        st.commitOperator(0, NO_ID, keccak256("root"), "");
    }

    function test_commitOperator_afterFinalize_reverts() public {
        // widen the commit window to overlap the finalize offset so the
        // "finalized" guard (not the window guard) is what trips
        vm.prank(owner);
        st.setEpochParams(T_EPOCH, FINALIZE_OFF, FINALIZE_OFF, FINALIZE_OFF);
        _toFinalize(0);
        st.finalizeEpoch(0);
        vm.prank(noAddr);
        vm.expectRevert("ST: finalized");
        st.commitOperator(0, NO_ID, keccak256("root"), "");
    }

    // ------------------------------------------------------------------
    // missed commit -> carry (incl. compounding); pools are emission-only
    // ------------------------------------------------------------------

    function test_missedCommit_carryCompoundsAcrossEpochs() public {
        // e0: 1000 deposit (reserved, NOT pooled) + 500 emission, no commit
        _deposit(NO_ID, noAddr, DEPOSIT);
        _accrue(MINER_HOTKEY, EMISSION);
        _toFinalize(0);
        vm.expectEmit(true, true, false, true, address(st));
        emit PoolCarried(0, NO_ID, EMISSION);
        st.finalizeEpoch(0);
        assertEq(st.carry(NO_ID), EMISSION);
        assertEq(st.poolTotal(0, NO_ID), 0);
        assertEq(_reserveStake(), DEPOSIT, "deposit went to the reserve, not the carry");

        // e1: 200 deposit (reserved) + 100 emission, no commit again
        _deposit(NO_ID, noAddr, 200e9);
        _accrue(MINER_HOTKEY, 100e9);
        _toFinalize(1);
        st.finalizeEpoch(1);
        assertEq(st.carry(NO_ID), 600e9);

        // e2: commit -> the whole compounded EMISSION carry lands in poolTotal[2]
        _accrue(MINER_HOTKEY, 50e9);
        _afterClose(2);
        vm.prank(noAddr);
        st.commitOperator(2, NO_ID, keccak256("root"), "");
        _toFinalize(2);
        vm.expectEmit(true, true, false, true, address(st));
        emit PoolFinalized(2, NO_ID, 650e9);
        st.finalizeEpoch(2);
        assertEq(st.poolTotal(2, NO_ID), 650e9);
        assertEq(st.carry(NO_ID), 0);
        assertEq(_reserveStake(), 1_200e9);
    }

    // ------------------------------------------------------------------
    // stake-delta emission measurement (D-4)
    // ------------------------------------------------------------------

    function test_emission_stakeDelta_incrementalAndMidEpochSweep() public {
        _deposit(NO_ID, noAddr, DEPOSIT); // reserved; escrow stays empty
        _accrue(MINER_HOTKEY, 200e9);
        st.sweepPool(NO_ID); // mid-epoch consolidation
        assertEq(staking.stakes(MINER_HOTKEY, proxyMirror), 0, "swept to treasury");
        assertEq(st.poolAccrued(NO_ID), 200e9);
        assertEq(st.accountedStake(), 200e9, "escrow ledger = swept emission only");

        _accrue(MINER_HOTKEY, 300e9); // more emission after the sweep
        _afterClose(0);
        st.rollEpochs();
        assertEq(st.poolEmission(0, NO_ID), 500e9, "sum of both measurements");
        assertEq(st.poolAccrued(NO_ID), 0);
        assertEq(_treasuryStake(), 500e9);
        assertEq(st.accountedStake(), 500e9);
        assertEq(_reserveStake(), DEPOSIT);
    }

    function test_emission_sweepFailure_baselineMakesRetryExact() public {
        _accrue(MINER_HOTKEY, EMISSION);
        staking.setFailMoveStake(true);

        _afterClose(0);
        vm.expectEmit(true, false, false, true, address(st));
        emit PoolSwept(NO_ID, EMISSION, 0, false); // measured but unswept
        st.rollEpochs();

        assertEq(st.poolEmission(0, NO_ID), EMISSION, "measured despite move failure");
        assertEq(st.poolBaseline(NO_ID), EMISSION, "baseline remembers the level");
        assertEq(st.accountedStake(), 0, "unswept => not custody yet");
        assertEq(staking.stakes(MINER_HOTKEY, proxyMirror), EMISSION);

        // permissionless retry once the precompile works again: no double count
        staking.setFailMoveStake(false);
        st.sweepPool(NO_ID);
        assertEq(st.poolBaseline(NO_ID), 0);
        assertEq(st.poolAccrued(NO_ID), 0, "delta 0 on retry: measured once");
        assertEq(st.accountedStake(), EMISSION);
        assertEq(_treasuryStake(), EMISSION);

        _afterClose(1);
        st.rollEpochs();
        assertEq(st.poolEmission(1, NO_ID), 0, "retry did not re-attribute");
    }

    // ------------------------------------------------------------------
    // lazy rolls
    // ------------------------------------------------------------------

    function test_rollEpochs_multiRoll_attributesToFirstUnrolledEpoch() public {
        _deposit(NO_ID, noAddr, DEPOSIT);
        _accrue(MINER_HOTKEY, 700e9);

        vm.roll(_closeBlock(2) + 5); // three boundaries elapsed, zero touches
        assertEq(st.epoch(), 0);
        assertEq(st.pendingEpoch(), 3);

        st.rollEpochs();
        assertEq(st.epoch(), 3);
        // documented approximation: late-accrued emission lands in the FIRST
        // unrolled epoch
        assertEq(st.poolEmission(0, NO_ID), 700e9);
        assertEq(st.poolEmission(1, NO_ID), 0);
        assertEq(st.poolEmission(2, NO_ID), 0);
        // close blocks are the intended boundaries, not the roll block
        assertEq(st.epochCloseBlock(0), uint64(_closeBlock(0)));
        assertEq(st.epochCloseBlock(1), uint64(_closeBlock(1)));
        assertEq(st.epochCloseBlock(2), uint64(_closeBlock(2)));
    }

    function test_rollEpochs_maxRollsPerCall_backlogGating() public {
        vm.roll(_closeBlock(69) + 5); // 70 boundaries behind
        _push(1e9);

        st.rollEpochs(); // bounded catch-up chunk
        assertEq(st.epoch(), 32);

        // 38 boundaries still pending: deposit rolls up to 32 more inside its
        // own tx but then refuses to act half-rolled (and the revert undoes
        // its rolls)
        vm.prank(noAddr);
        vm.expectRevert("ST: roll backlog");
        st.deposit(NO_ID, 1e9);
        assertEq(st.epoch(), 32, "reverted call persisted nothing");

        st.rollEpochs();
        assertEq(st.epoch(), 64);

        // rolls the last 6 and succeeds; the deposit books into epoch 70 (the
        // Deposited event's epoch field is the only per-epoch record now, D25)
        vm.expectEmit(true, true, false, true, address(st));
        emit Deposited(70, NO_ID, noAddr, 1e9);
        vm.prank(noAddr);
        st.deposit(NO_ID, 1e9);
        assertEq(st.epoch(), 70);
        assertEq(_reserveStake(), 1e9);
    }

    function test_pendingEpoch_tracksChainTime() public {
        assertEq(st.pendingEpoch(), 0);
        vm.roll(START_BLOCK + T_EPOCH - 1);
        assertEq(st.pendingEpoch(), 0);
        vm.roll(START_BLOCK + T_EPOCH);
        assertEq(st.pendingEpoch(), 1);
        assertEq(st.epoch(), 0, "lazy counter unchanged until a roll");
        st.rollEpochs();
        assertEq(st.epoch(), 1);
    }

    function test_sweepPool_unknownOperator_reverts() public {
        vm.expectRevert("ST: no operator");
        st.sweepPool(99);
    }

    // ------------------------------------------------------------------
    // finalizeEpoch: permissionless + in-order + append-only
    // ------------------------------------------------------------------

    function test_finalize_permissionless_inOrder_appendOnly() public {
        vm.expectRevert("ST: epoch open");
        st.finalizeEpoch(0); // e == nextFinalizeEpoch but epoch 0 still open

        _toFinalize(0);
        vm.expectRevert("ST: finalize in order");
        st.finalizeEpoch(1); // skipping ahead is impossible

        vm.prank(rando); // permissionless
        st.finalizeEpoch(0);
        assertTrue(st.finalized(0));
        assertEq(st.nextFinalizeEpoch(), 1);

        vm.expectRevert("ST: finalize in order"); // append-only: never twice
        st.finalizeEpoch(0);

        // e1 not yet at its offset
        _afterClose(1);
        vm.expectRevert("ST: finalize window");
        st.finalizeEpoch(1);

        _toFinalize(1);
        st.finalizeEpoch(1);
        assertTrue(st.finalized(1));
    }

    function test_finalize_paused_reverts() public {
        _toFinalize(0);
        vm.prank(guardian);
        st.setPaused(true);
        vm.expectRevert("ST: paused");
        st.finalizeEpoch(0);
        // unpause -> proceeds
        vm.prank(guardian);
        st.setPaused(false);
        st.finalizeEpoch(0);
    }

    // (fundFeePool: deferred with the effort bounty, §9.3/D23.)

    // ------------------------------------------------------------------
    // multi-epoch E2E with exact conservation
    // ------------------------------------------------------------------

    function test_multiEpoch_e2e_exactConservation() public {
        (bytes32 payoutRoot, bytes32[] memory proofA, bytes32[] memory proofB) = _minerTreeAB();

        // ---- epoch 0: deposit 1000 (reserved) + 500 emission; commit ----
        _deposit(NO_ID, noAddr, 1_000e9);
        _accrue(MINER_HOTKEY, 500e9);
        _afterClose(0);
        vm.prank(noAddr);
        st.commitOperator(0, NO_ID, payoutRoot, "e0");
        _toFinalize(0);
        st.finalizeEpoch(0);
        assertEq(st.poolTotal(0, NO_ID), 500e9, "emission only");

        // ---- epoch 1: deposit 800 (reserved) + 300 emission; MISSED commit ----
        _deposit(NO_ID, noAddr, 800e9);
        _accrue(MINER_HOTKEY, 300e9);
        _toFinalize(1);
        st.finalizeEpoch(1);
        assertEq(st.carry(NO_ID), 300e9);
        assertEq(st.poolTotal(1, NO_ID), 0);

        // ---- epoch 2: deposit 200 (reserved) + 100 emission; commit ----
        _deposit(NO_ID, noAddr, 200e9);
        _accrue(MINER_HOTKEY, 100e9);
        _afterClose(2);
        vm.prank(noAddr);
        st.commitOperator(2, NO_ID, payoutRoot, "e2");
        _toFinalize(2);
        st.finalizeEpoch(2);
        assertEq(st.poolTotal(2, NO_ID), 100e9 + 300e9, "emission + carried emission");
        assertEq(st.carry(NO_ID), 0);

        // ---- claims for both finalized epochs ----
        st.claimMiner(0, NO_ID, PROV_A, 6_000, proofA);
        st.claimMiner(0, NO_ID, PROV_B, 4_000, proofB);
        st.claimMiner(2, NO_ID, PROV_A, 6_000, proofA);
        st.claimMiner(2, NO_ID, PROV_B, 4_000, proofB);

        assertEq(staking.stakes(TREASURY, PROV_A), 300e9 + 240e9);
        assertEq(staking.stakes(TREASURY, PROV_B), 200e9 + 160e9);

        // ---- conservation, to the rao ----
        // emission in: 500 + 300 + 100 = 900; all of it claimed out
        assertEq(st.accountedStake(), 0);
        assertEq(_treasuryStake(), 0);
        // deposits in: 2000; all of it locked in the reserve, forever
        assertEq(_reserveStake(), 2_000e9);
        assertEq(st.buybackTotal(), 2_000e9);
    }
}
