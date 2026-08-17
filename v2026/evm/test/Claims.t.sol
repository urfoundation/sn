// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {stdJson} from "forge-std/StdJson.sol";
import {MerkleProof} from "@openzeppelin/contracts/utils/cryptography/MerkleProof.sol";

import {STBase} from "./utils/STBase.sol";
import {MerkleBuilder} from "./utils/MerkleBuilder.sol";

/// @dev Area 2 — claims. v0.3/D23: poolTotal is EMISSION-ONLY (deposits are
///      buybacks, reserved and never claimable), and the validator bounty
///      claim is deferred with the bounty phase.
///
///      Miner claims run against the SHARED Go<->Solidity Merkle vectors
///      (sn/merkle/testdata/merkle_vectors.json, OZ double-hash sorted-pair):
///      for payout_5 / payout_33 the vector root is committed and EVERY leaf
///      is claimed with its vector proof. Those vectors' shareBps sum to
///      24_691 / 190_258 bps (> 10_000 by construction), so the claims that
///      overflow the pool MUST revert on the cumulative cap ("ST: pool
///      over-drained") and never on the proof — proof acceptance for every
///      leaf is additionally pinned with a direct MerkleProof.verify. Design
///      note: values are used verbatim (all individual values are <= 10_000
///      bps, verified here), NOT re-mapped mod 10_001, so the bytes proven on
///      chain are exactly the vector bytes the Go side hashes.
contract ClaimsTest is STBase {
    using stdJson for string;

    uint256 constant DEPOSIT = 1_000e9;
    uint256 constant EMISSION = 500e9;
    uint256 constant POOL_TOTAL = 500e9; // EMISSION only (§8.3/D23)

    string internal vectorsJson;

    event MinerClaimed(
        uint256 indexed e,
        uint256 indexed noId,
        bytes32 indexed coldkey,
        uint256 shareBps,
        uint256 amount,
        address caller
    );

    function setUp() public override {
        super.setUp();
        vectorsJson = vm.readFile(string.concat(vm.projectRoot(), "/../merkle/testdata/merkle_vectors.json"));
    }

    // ------------------------------------------------------------------
    // vector plumbing
    // ------------------------------------------------------------------

    struct VectorLeaf {
        bytes32 coldkey;
        uint256 shareBps;
        bytes32[] proof;
    }

    function _vectorIndex(string memory name) internal view returns (uint256) {
        for (uint256 i = 0; i < 64; i++) {
            string memory key = string.concat("$[", vm.toString(i), "].name");
            if (!vm.keyExistsJson(vectorsJson, key)) break;
            if (keccak256(bytes(vectorsJson.readString(key))) == keccak256(bytes(name))) {
                return i;
            }
        }
        revert("vector not found");
    }

    function _loadVector(string memory name)
        internal
        view
        returns (bytes32 root, VectorLeaf[] memory leaves)
    {
        uint256 vi = _vectorIndex(name);
        string memory base = string.concat("$[", vm.toString(vi), "]");
        root = vectorsJson.readBytes32(string.concat(base, ".root"));

        uint256 n = 0;
        while (
            vm.keyExistsJson(
                vectorsJson, string.concat(base, ".leaves[", vm.toString(n), "].coldkey")
            )
        ) {
            n++;
        }
        leaves = new VectorLeaf[](n);
        for (uint256 i = 0; i < n; i++) {
            string memory li = vm.toString(i);
            leaves[i].coldkey =
                vectorsJson.readBytes32(string.concat(base, ".leaves[", li, "].coldkey"));
            leaves[i].shareBps =
                vm.parseUint(vectorsJson.readString(string.concat(base, ".leaves[", li, "].value")));
            leaves[i].proof =
                vectorsJson.readBytes32Array(string.concat(base, ".proofs[", li, "]"));
        }
    }

    /// @dev Fund + close + commit `root` for epoch 0 and finalize it, so
    ///      poolTotal(0, NO_ID) == POOL_TOTAL (emission only; the deposit
    ///      goes to the reserve and must never enter the pool).
    function _finalizedEpochWithRoot(bytes32 root) internal {
        _deposit(NO_ID, noAddr, DEPOSIT);
        _accrue(MINER_HOTKEY, EMISSION);
        _afterClose(0);
        vm.prank(noAddr);
        st.commitOperator(0, NO_ID, root, "vector");
        _toFinalize(0);
        st.finalizeEpoch(0);
        assertEq(st.poolTotal(0, NO_ID), POOL_TOTAL);
        assertEq(_reserveStake(), DEPOSIT, "deposit reserved, not pooled");
    }

    /// @dev Claim EVERY vector leaf in order. Each proof must be accepted:
    ///      a claim either pays out or reverts EXACTLY on the cumulative
    ///      over-drain cap (which sits after proof + dedup verification).
    function _claimAllVectorLeaves(string memory name)
        internal
        returns (uint256 paid, uint256 successes, uint256 capReverts)
    {
        (bytes32 root, VectorLeaf[] memory leaves) = _loadVector(name);
        _finalizedEpochWithRoot(root);

        for (uint256 i = 0; i < leaves.length; i++) {
            VectorLeaf memory l = leaves[i];
            assertGt(l.shareBps, 0, "vector value 0");
            assertLe(l.shareBps, BPS, "vector value > 10000 bps");
            // 1) pin proof acceptance for EVERY leaf, independent of the cap
            assertTrue(
                MerkleProof.verify(l.proof, root, st.minerLeafHash(l.coldkey, l.shareBps)),
                "vector proof must verify"
            );

            // 2) exercise the claim path
            uint256 amount = (l.shareBps * POOL_TOTAL) / BPS;
            if (st.claimedMiner(0, NO_ID) + amount <= POOL_TOTAL) {
                vm.expectEmit(true, true, true, true, address(st));
                emit MinerClaimed(0, NO_ID, l.coldkey, l.shareBps, amount, address(this));
                st.claimMiner(0, NO_ID, l.coldkey, l.shareBps, l.proof);
                assertEq(staking.stakes(TREASURY, l.coldkey), amount, "paid as stake");
                paid += amount;
                successes++;
            } else {
                // proof + dedup pass; the revert is the cumulative cap
                vm.expectRevert("ST: pool over-drained");
                st.claimMiner(0, NO_ID, l.coldkey, l.shareBps, l.proof);
                capReverts++;
            }
        }
        assertEq(st.claimedMiner(0, NO_ID), paid);
        assertLe(st.claimedMiner(0, NO_ID), POOL_TOTAL, "cap: sum of claims <= poolTotal");
    }

    // ------------------------------------------------------------------
    // claimMiner across the shared merkle vectors
    // ------------------------------------------------------------------

    function test_vectors_payout5_everyLeaf_capEnforced() public {
        (uint256 paid, uint256 successes, uint256 capReverts) = _claimAllVectorLeaves("payout_5");
        // Σ shareBps = 24_691 > 10_000: both behaviors must occur
        assertGt(successes, 0);
        assertGt(capReverts, 0);
        assertEq(successes + capReverts, 5);
        assertLe(paid, POOL_TOTAL);
        // conservation: the escrow still backs the un-drained remainder;
        // the reserve is untouched by claims (one-way invariant)
        assertEq(st.accountedStake(), _treasuryStake());
        assertEq(st.accountedStake(), POOL_TOTAL - paid);
        assertEq(_reserveStake(), DEPOSIT);
    }

    function test_vectors_payout33_everyLeaf_capEnforced() public {
        (uint256 paid, uint256 successes, uint256 capReverts) = _claimAllVectorLeaves("payout_33");
        assertGt(successes, 0);
        assertGt(capReverts, 0);
        assertEq(successes + capReverts, 33);
        assertLe(paid, POOL_TOTAL);
        assertEq(st.accountedStake(), _treasuryStake());
    }

    function test_vectors_payout5_dedupEnforced() public {
        (bytes32 root, VectorLeaf[] memory leaves) = _loadVector("payout_5");
        _finalizedEpochWithRoot(root);
        st.claimMiner(0, NO_ID, leaves[0].coldkey, leaves[0].shareBps, leaves[0].proof);
        vm.expectRevert("ST: claimed");
        st.claimMiner(0, NO_ID, leaves[0].coldkey, leaves[0].shareBps, leaves[0].proof);
    }

    /// @dev payout_1: single-leaf tree (empty proof, root == leaf); 3319 bps
    ///      claimed => shares < 10_000 leave a remainder that simply STAYS in
    ///      epoch-0 custody. v1 has no TTL: claims never expire, so there is
    ///      no rollover of a finalized pool's unclaimed remainder (WHITEPAPER
    ///      §5.2's TTL sketch is deliberately not implemented — README).
    function test_vectors_payout1_sharesUnder10000_remainderStaysCustodied() public {
        (bytes32 root, VectorLeaf[] memory leaves) = _loadVector("payout_1");
        assertEq(leaves.length, 1);
        assertEq(leaves[0].proof.length, 0, "single leaf: empty proof");
        _finalizedEpochWithRoot(root);

        st.claimMiner(0, NO_ID, leaves[0].coldkey, leaves[0].shareBps, leaves[0].proof);
        uint256 amount = (leaves[0].shareBps * POOL_TOTAL) / BPS;
        assertEq(staking.stakes(TREASURY, leaves[0].coldkey), amount);

        uint256 remainder = POOL_TOTAL - amount;
        assertGt(remainder, 0);
        // the remainder stays as attributed custody of the finalized epoch
        assertEq(st.accountedStake(), remainder);
        assertEq(_treasuryStake(), st.accountedStake());
        // poolTotal/claimedMiner remain the permanent record
        assertEq(st.poolTotal(0, NO_ID) - st.claimedMiner(0, NO_ID), remainder);

        // rolling more epochs does NOT re-attribute the remainder to carry
        _toFinalize(1);
        st.finalizeEpoch(1);
        assertEq(st.carry(NO_ID), 0);
        assertEq(st.poolTotal(0, NO_ID) - st.claimedMiner(0, NO_ID), remainder);
    }

    // ------------------------------------------------------------------
    // claimMiner unit behaviors
    // ------------------------------------------------------------------

    function test_claimMiner_sharesSumExactly10000_floorDustRemains() public {
        // odd pool total => floor division leaves dust even at Σ = 10_000.
        // The pool is funded by EMISSION (deposits never fund pools, §8.3).
        uint256 pt = 1_000_000_000_003;
        _accrue(MINER_HOTKEY, pt);

        bytes32[] memory cks = new bytes32[](3);
        uint256[] memory shares = new uint256[](3);
        (cks[0], shares[0]) = (keccak256("p0"), 3_333);
        (cks[1], shares[1]) = (keccak256("p1"), 3_333);
        (cks[2], shares[2]) = (keccak256("p2"), 3_334);
        bytes32[] memory leaves = _minerLeaves(cks, shares);
        bytes32 root = MerkleBuilder.root(leaves);

        _afterClose(0);
        vm.prank(noAddr);
        st.commitOperator(0, NO_ID, root, "");
        _toFinalize(0);
        st.finalizeEpoch(0);
        assertEq(st.poolTotal(0, NO_ID), pt);

        uint256 paid;
        for (uint256 i = 0; i < 3; i++) {
            st.claimMiner(0, NO_ID, cks[i], shares[i], MerkleBuilder.proof(leaves, i));
            paid += (shares[i] * pt) / BPS;
        }
        assertEq(st.claimedMiner(0, NO_ID), paid);
        assertLt(paid, pt, "floor dust stays");
        assertEq(st.accountedStake(), pt - paid);
    }

    function test_claimMiner_dedupIsPerNoIdColdkey() public {
        _registerOperator2();
        bytes32 ck = keccak256("multi-pool-provider");
        bytes32[] memory cks = new bytes32[](1);
        uint256[] memory shares = new uint256[](1);
        (cks[0], shares[0]) = (ck, 10_000);
        bytes32 root = MerkleBuilder.root(_minerLeaves(cks, shares));

        _accrue(MINER_HOTKEY, 100e9);
        _accrue(MINER2_HOTKEY, 200e9);
        _afterClose(0);
        vm.prank(noAddr);
        st.commitOperator(0, NO_ID, root, "");
        vm.prank(no2Addr);
        st.commitOperator(0, NO2_ID, root, "");
        _toFinalize(0);
        st.finalizeEpoch(0);

        bytes32[] memory emptyProof = new bytes32[](0);
        // same coldkey claims from BOTH pools (dedup key includes noId)
        st.claimMiner(0, NO_ID, ck, 10_000, emptyProof);
        st.claimMiner(0, NO2_ID, ck, 10_000, emptyProof);
        assertEq(staking.stakes(TREASURY, ck), 100e9 + 200e9);

        vm.expectRevert("ST: claimed");
        st.claimMiner(0, NO_ID, ck, 10_000, emptyProof);
    }

    function test_claimMiner_guards() public {
        (bytes32 root, bytes32[] memory proofA,) = _minerTreeAB();

        // epoch not finalized
        vm.expectRevert("ST: not finalized");
        st.claimMiner(0, NO_ID, PROV_A, 6_000, proofA);

        _accrue(MINER_HOTKEY, EMISSION);
        _registerOperator2(); // never commits
        _afterClose(0);
        vm.prank(noAddr);
        st.commitOperator(0, NO_ID, root, "");
        _toFinalize(0);
        st.finalizeEpoch(0);

        vm.expectRevert("ST: coldkey 0");
        st.claimMiner(0, NO_ID, bytes32(0), 6_000, proofA);
        vm.expectRevert("ST: shareBps");
        st.claimMiner(0, NO_ID, PROV_A, 0, proofA);
        vm.expectRevert("ST: shareBps");
        st.claimMiner(0, NO_ID, PROV_A, BPS + 1, proofA);
        vm.expectRevert("ST: no commit");
        st.claimMiner(0, NO2_ID, PROV_A, 6_000, proofA);
        // wrong share for the committed leaf => proof rejection
        vm.expectRevert("ST: bad proof");
        st.claimMiner(0, NO_ID, PROV_A, 5_999, proofA);
        // wrong claimer coldkey
        vm.expectRevert("ST: bad proof");
        st.claimMiner(0, NO_ID, keccak256("not-in-tree"), 6_000, proofA);
    }

    function test_claimMiner_zeroPoolTotal_claimsZero() public {
        _registerOperator2(); // no emission => poolTotal 0
        bytes32[] memory cks = new bytes32[](1);
        uint256[] memory shares = new uint256[](1);
        (cks[0], shares[0]) = (PROV_A, 10_000);
        bytes32 root = MerkleBuilder.root(_minerLeaves(cks, shares));

        _afterClose(0);
        vm.prank(no2Addr);
        st.commitOperator(0, NO2_ID, root, "");
        _toFinalize(0);
        st.finalizeEpoch(0);
        assertEq(st.poolTotal(0, NO2_ID), 0);

        bytes32[] memory emptyProof = new bytes32[](0);
        st.claimMiner(0, NO2_ID, PROV_A, 10_000, emptyProof); // amount 0: no transfer
        assertEq(staking.stakes(TREASURY, PROV_A), 0);
        vm.expectRevert("ST: claimed"); // but the dedup is burned
        st.claimMiner(0, NO2_ID, PROV_A, 10_000, emptyProof);
    }

    // (claimValidator: deferred with the effort bounty, §9.3/D23 — the v0.2
    //  bounty-claim suite is parked at docs/parked/.)

    // ------------------------------------------------------------------
    // α conservation across a whole epoch (claims + carry + reserve)
    // ------------------------------------------------------------------

    function test_epochConservation_paidPlusCarryPlusCustodyPlusReserve() public {
        _registerOperator2();
        (bytes32 root, bytes32[] memory proofA, bytes32[] memory proofB) = _minerTreeAB();

        // NO1 commits; NO2 misses (carry). Deposits go to the reserve in full.
        _deposit(NO_ID, noAddr, 1_000e9);
        _deposit(NO2_ID, no2Addr, 400e9);
        _accrue(MINER_HOTKEY, 500e9);
        _accrue(MINER2_HOTKEY, 250e9);
        _afterClose(0);
        vm.prank(noAddr);
        st.commitOperator(0, NO_ID, root, "");
        _toFinalize(0);
        st.finalizeEpoch(0);

        // pools carry EMISSION only; every deposited rao is in the reserve
        assertEq(st.poolTotal(0, NO_ID), 500e9);
        assertEq(st.carry(NO2_ID), 250e9);
        assertEq(_reserveStake(), 1_400e9);
        assertEq(st.buybackTotal(), 1_400e9);

        st.claimMiner(0, NO_ID, PROV_A, 6_000, proofA);
        st.claimMiner(0, NO_ID, PROV_B, 4_000, proofB);
        uint256 paid = 300e9 + 200e9;

        // Σ paid + carry + custody-of-remainders + reserve == Σ in, to the rao
        uint256 totalIn = 1_000e9 + 400e9 + 500e9 + 250e9;
        assertEq(
            paid + st.carry(NO2_ID) + (st.poolTotal(0, NO_ID) - st.claimedMiner(0, NO_ID))
                + _reserveStake(),
            totalIn
        );
        assertEq(st.accountedStake(), st.carry(NO2_ID));
        assertEq(_treasuryStake(), st.accountedStake());
    }
}
