// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {OwnableUpgradeable} from
    "@openzeppelin/contracts-upgradeable/access/OwnableUpgradeable.sol";

import {STBase} from "./utils/STBase.sol";
import {STSubnet} from "../src/STSubnet.sol";
import {STSubnetV2Mock} from "./mocks/STSubnetV2Mock.sol";

/// @dev Area 5 — governance and the structural invariants (§6.4, D-12/D23):
///      upgrade-under-fire with genuinely changed logic, the exact guardian
///      pause surface (never claims of any epoch, never finalized state),
///      guardian privilege separation, parameter setter bounds, the absence
///      of any owner claw-back path for finalized funds, and the one-way
///      buyback reserve (no privileged path drains it).
contract GovernanceTest is STBase {
    uint256 constant DEPOSIT = 1_000e9;
    uint256 constant EMISSION = 500e9;
    uint256 constant POOL0 = 500e9; // EMISSION only (§8.3/D23)

    /// @dev Full epoch-0 flow: deposit (reserved) + emission + commit +
    ///      finalize. Returns the miner proofs for later claims.
    function _finalizedEpoch0()
        internal
        returns (bytes32 root, bytes32[] memory proofA, bytes32[] memory proofB)
    {
        (root, proofA, proofB) = _minerTreeAB();
        _deposit(NO_ID, noAddr, DEPOSIT);
        _accrue(MINER_HOTKEY, EMISSION);
        _afterClose(0);
        vm.prank(noAddr);
        st.commitOperator(0, NO_ID, root, "");
        _toFinalize(0);
        st.finalizeEpoch(0);
        assertEq(st.poolTotal(0, NO_ID), POOL0);
        assertEq(_reserveStake(), DEPOSIT);
    }

    // ------------------------------------------------------------------
    // upgrade under fire — changed logic cannot touch finalized claims
    // ------------------------------------------------------------------

    function test_upgradeUnderFire_changedLogic_claimsPayOldSnapshot() public {
        (bytes32 root, bytes32[] memory proofA, bytes32[] memory proofB) = _finalizedEpoch0();

        // upgrade to an implementation with CHANGED logic (0x402 always false,
        // poisoned mirror, new version() surface)
        STSubnetV2Mock v2 = new STSubnetV2Mock();
        vm.prank(owner);
        st.upgradeToAndCall(address(v2), "");

        // the new logic is live...
        assertEq(STSubnetV2Mock(payable(address(st))).version(), 2);
        // (v2's _ed25519Verify == false: the head-binding identity proof fails)
        metagraph.setNeuron(5, keccak256("post-hk"), Blake2bMirror(rando));
        vm.prank(rando);
        vm.expectRevert("ST: bad client sig");
        st.bindHead(keccak256("post-hk"), keccak256("post-client"), _sig());

        // ...but the finalized snapshot is byte-identical...
        assertEq(st.poolTotal(0, NO_ID), POOL0);
        (bytes32 rootAfter,) = st.noCommit(0, NO_ID);
        assertEq(rootAfter, root);
        assertTrue(st.finalized(0));

        // ...and epoch 0's claims pay EXACTLY per the old snapshot
        st.claimMiner(0, NO_ID, PROV_A, 6_000, proofA);
        st.claimMiner(0, NO_ID, PROV_B, 4_000, proofB);
        assertEq(staking.stakes(TREASURY, PROV_A), (POOL0 * 6_000) / BPS);
        assertEq(staking.stakes(TREASURY, PROV_B), (POOL0 * 4_000) / BPS);
        assertEq(st.accountedStake(), 0, "everything custodied was paid out");
        assertEq(_treasuryStake(), 0);
        // ...and the reserve survived the hostile upgrade untouched
        assertEq(_reserveStake(), DEPOSIT);
        assertEq(st.buybackTotal(), DEPOSIT);
    }

    function test_upgrade_storagePreservedExactly() public {
        _finalizedEpoch0();
        _deposit(NO_ID, noAddr, 123e9); // epoch-1 in-flight state

        uint256 epochBefore = st.epoch();
        uint256 accountedBefore = st.accountedStake();
        uint256 buybackBefore = st.buybackTotal();
        bytes32 selfCkBefore = st.selfColdkey();
        bytes32 reserveBefore = st.reserveHotkey();

        STSubnetV2Mock v2 = new STSubnetV2Mock();
        vm.prank(owner);
        st.upgradeToAndCall(address(v2), "");

        assertEq(st.epoch(), epochBefore);
        assertEq(st.accountedStake(), accountedBefore);
        assertEq(st.buybackTotal(), buybackBefore);
        assertEq(st.selfColdkey(), selfCkBefore);
        assertEq(st.reserveHotkey(), reserveBefore);
        assertEq(st.owner(), owner);
        assertEq(st.guardian(), guardian);
    }

    function test_upgrade_onlyOwner() public {
        STSubnetV2Mock v2 = new STSubnetV2Mock();
        vm.prank(guardian);
        vm.expectRevert(
            abi.encodeWithSelector(OwnableUpgradeable.OwnableUnauthorizedAccount.selector, guardian)
        );
        st.upgradeToAndCall(address(v2), "");

        vm.prank(rando);
        vm.expectRevert(
            abi.encodeWithSelector(OwnableUpgradeable.OwnableUnauthorizedAccount.selector, rando)
        );
        st.upgradeToAndCall(address(v2), "");
    }

    // ------------------------------------------------------------------
    // pause surface (D-12)
    // ------------------------------------------------------------------

    function test_pause_blocksExactlyTheDocumentedSurface() public {
        // epoch 0 finalized before the pause; epoch 1 mid-flight
        (, bytes32[] memory proofA,) = _finalizedEpoch0();
        _deposit(NO_ID, noAddr, 100e9);
        _afterClose(1);

        vm.prank(guardian);
        st.setPaused(true);

        // ---- blocked: deposit, finalize ----
        _push(1e9);
        vm.prank(noAddr);
        vm.expectRevert("ST: paused");
        st.deposit(NO_ID, 1e9);

        // ---- NOT blocked: rolls, sweeps, commits, claims ----
        st.rollEpochs();
        st.sweepPool(NO_ID);

        // commit for the closed epoch 1 still works while paused
        bytes32 cRoot = keccak256("commit-under-pause");
        vm.prank(noAddr);
        st.commitOperator(1, NO_ID, cRoot, "");
        (bytes32 gotRoot,) = st.noCommit(1, NO_ID);
        assertEq(gotRoot, cRoot);

        // finalized epoch 0 claims can NEVER be paused
        st.claimMiner(0, NO_ID, PROV_A, 6_000, proofA);
        assertEq(staking.stakes(TREASURY, PROV_A), (POOL0 * 6_000) / BPS);

        // finalize of the NEXT epoch is what the pause holds back
        _toFinalize(1);
        vm.expectRevert("ST: paused");
        st.finalizeEpoch(1);

        // unpause restores the gated surface
        vm.prank(guardian);
        st.setPaused(false);
        st.finalizeEpoch(1);
        assertTrue(st.finalized(1));
    }

    function test_setPaused_access() public {
        vm.prank(rando);
        vm.expectRevert("ST: not guardian");
        st.setPaused(true);

        vm.prank(guardian);
        st.setPaused(true);
        assertTrue(st.paused());

        vm.prank(owner); // owner shares the pause power
        st.setPaused(false);
        assertFalse(st.paused());
    }

    function test_guardian_hasNoOwnerPowers() public {
        bytes memory unauthorized = abi.encodeWithSelector(
            OwnableUpgradeable.OwnableUnauthorizedAccount.selector, guardian
        );
        vm.startPrank(guardian);
        vm.expectRevert(unauthorized);
        st.setEpochParams(100, 10, 20, 30);
        vm.expectRevert(unauthorized);
        st.setGuardian(guardian);
        vm.expectRevert(unauthorized);
        st.setOperatorAddress(NO_ID, guardian);
        vm.expectRevert(unauthorized);
        st.setOperatorActive(NO_ID, false);
        vm.expectRevert(unauthorized);
        st.setSelfColdkey(keccak256("x"));
        vm.expectRevert(unauthorized);
        st.registerOperator(9, keccak256("c"), keccak256("h"));
        vm.stopPrank();
    }

    // ------------------------------------------------------------------
    // parameter setter bounds
    // ------------------------------------------------------------------

    function test_setEpochParams_bounds() public {
        vm.startPrank(owner);
        vm.expectRevert("ST: tEpoch 0");
        st.setEpochParams(0, 10, 20, 30);
        vm.expectRevert("ST: window order");
        st.setEpochParams(100, 25, 20, 30); // commit > trails
        vm.expectRevert("ST: window order");
        st.setEpochParams(100, 10, 40, 30); // trails > finalize
        st.setEpochParams(1, 0, 0, 0); // degenerate-but-ordered is allowed
        st.setEpochParams(100, 30, 30, 30); // equal boundaries allowed
        assertEq(st.tEpoch(), 100);
        assertEq(st.commitWindowBlocks(), 30);
        vm.stopPrank();
    }

    function test_setSelfColdkey_bounds() public {
        vm.startPrank(owner);
        vm.expectRevert("ST: coldkey 0");
        st.setSelfColdkey(bytes32(0));
        st.setSelfColdkey(keccak256("corrected-mirror"));
        assertEq(st.selfColdkey(), keccak256("corrected-mirror"));
        vm.stopPrank();
    }

    // ------------------------------------------------------------------
    // no claw-back of finalized funds; the reserve is one-way
    // ------------------------------------------------------------------

    /// @dev There is no owner/guardian path that mutates a finalized epoch's
    ///      settlement, moves custodied α, or drains the reserve: exercise
    ///      EVERY privileged setter with hostile values, then verify the
    ///      snapshot, the custody, and the reserve are untouched and the
    ///      claims still pay exactly.
    function test_ownerCannotClawBackFinalizedFundsOrReserve() public {
        (bytes32 root, bytes32[] memory proofA,) = _finalizedEpoch0();
        uint256 accountedBefore = st.accountedStake();
        uint256 treasuryBefore = _treasuryStake();
        uint256 reserveBefore = _reserveStake();

        vm.startPrank(owner);
        st.setEpochParams(1, 0, 0, 0);
        st.setGuardian(owner);
        st.setPaused(true);
        st.setOperatorAddress(NO_ID, owner);
        st.setOperatorActive(NO_ID, false);
        st.setSelfColdkey(keccak256("hostile-mirror"));
        vm.stopPrank();

        // finalized settlement state: bit-for-bit identical
        assertEq(st.poolTotal(0, NO_ID), POOL0);
        assertTrue(st.finalized(0));
        (bytes32 r,) = st.noCommit(0, NO_ID);
        assertEq(r, root);
        assertEq(st.claimedMiner(0, NO_ID), 0);
        // custody: not one rao moved — escrow OR reserve
        assertEq(st.accountedStake(), accountedBefore);
        assertEq(_treasuryStake(), treasuryBefore);
        assertEq(_reserveStake(), reserveBefore, "no privileged path drains the reserve");

        // claims pay exactly, paused + de-listed + re-parameterized or not
        st.claimMiner(0, NO_ID, PROV_A, 6_000, proofA);
        assertEq(staking.stakes(TREASURY, PROV_A), (POOL0 * 6_000) / BPS);
        assertEq(_reserveStake(), reserveBefore, "claims never source from the reserve");
    }

    // ------------------------------------------------------------------
    // parameters reach only future state
    // ------------------------------------------------------------------

    function test_setGuardian_rotatesPausePower() public {
        address newGuardian = makeAddr("new-guardian");
        vm.prank(owner);
        st.setGuardian(newGuardian);

        vm.prank(guardian); // the old guardian lost the power
        vm.expectRevert("ST: not guardian");
        st.setPaused(true);

        vm.prank(newGuardian);
        st.setPaused(true);
        assertTrue(st.paused());
    }

    // ------------------------------------------------------------------
    // helpers
    // ------------------------------------------------------------------

    /// @dev Local alias so the hostile-upgrade test can mint a mirror for an
    ///      arbitrary actor without importing the library at call sites.
    function Blake2bMirror(address a) internal pure returns (bytes32) {
        // the V2 mock poisons _mirror, but the metagraph entry just needs to
        // NOT match — any distinct value works; use a tagged constant.
        return keccak256(abi.encodePacked("mirror-of", a));
    }
}
