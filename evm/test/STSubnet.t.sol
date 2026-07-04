// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {Test} from "forge-std/Test.sol";
import {ERC1967Proxy} from "@openzeppelin/contracts/proxy/ERC1967/ERC1967Proxy.sol";

import {STSubnet} from "../src/STSubnet.sol";
import {Blake2b} from "../src/lib/Blake2b.sol";
import {ISTAKING_ADDRESS} from "../src/interfaces/stakingV2.sol";
import {INeuron_ADDRESS} from "../src/interfaces/neuron.sol";
import {IMetagraph_ADDRESS} from "../src/interfaces/metagraph.sol";
import {IED25519VERIFY_ADDRESS} from "../src/interfaces/ed25519Verify.sol";
import {MockStakingV2, MockNeuron, MockMetagraph, MockEd25519} from "./mocks/PrecompileMocks.sol";

/// @dev Smoke suite: deploy + init + one happy-path epoch end to end with
///      vm.etch-mocked precompiles, plus buyback-reserve invariants (v0.3/D23),
///      pause / upgrade-under-fire spot checks and blake2b known-answer
///      vectors. Deposits are buybacks: every deposit moves in full to the
///      reserve hotkey; poolTotal is EMISSION-ONLY.
contract STSubnetTest is Test {
    // epoch params (blocks)
    uint64 constant T_EPOCH = 100;
    uint64 constant COMMIT_W = 10;
    uint64 constant TRAILS_W = 20; // reserved dial (bounty phase)
    uint64 constant FINALIZE_OFF = 30;
    uint16 constant NETUID = 77;

    uint64 constant START_BLOCK = 1_000;

    bytes32 constant TREASURY = keccak256("treasury-hotkey");
    bytes32 constant RESERVE = keccak256("reserve-hotkey");
    bytes32 constant NO_COLDKEY = keccak256("no-coldkey");
    bytes32 constant MINER_HOTKEY = keccak256("pool-1-hotkey");
    bytes32 constant PROV_A = keccak256("provider-a-coldkey");
    bytes32 constant PROV_B = keccak256("provider-b-coldkey");

    uint256 constant NO_ID = 1;
    uint256 constant DEPOSIT = 1_000e9; // rao
    uint256 constant EMISSION = 500e9;

    address owner = makeAddr("owner");
    address guardian = makeAddr("guardian");
    address noAddr = makeAddr("no-operator");

    STSubnet st;
    bytes32 proxyMirror;

    MockStakingV2 staking = MockStakingV2(ISTAKING_ADDRESS);
    MockNeuron neuron = MockNeuron(INeuron_ADDRESS);
    MockMetagraph metagraph = MockMetagraph(IMetagraph_ADDRESS);
    MockEd25519 ed = MockEd25519(IED25519VERIFY_ADDRESS);

    event Deposited(uint256 indexed e, uint256 indexed noId, address from, uint256 amount);
    event BuybackReserved(
        uint256 indexed e, uint256 indexed noId, uint256 amount, uint256 buybackTotal
    );

    // ------------------------------------------------------------------
    // setup
    // ------------------------------------------------------------------

    function setUp() public {
        vm.roll(START_BLOCK);

        vm.etch(ISTAKING_ADDRESS, address(new MockStakingV2()).code);
        vm.etch(INeuron_ADDRESS, address(new MockNeuron()).code);
        vm.etch(IMetagraph_ADDRESS, address(new MockMetagraph()).code);
        vm.etch(IED25519VERIFY_ADDRESS, address(new MockEd25519()).code);

        STSubnet impl = new STSubnet();
        ERC1967Proxy proxy = new ERC1967Proxy(
            address(impl),
            abi.encodeCall(
                STSubnet.initialize,
                (
                    NETUID,
                    owner,
                    guardian,
                    TREASURY,
                    RESERVE,
                    T_EPOCH,
                    COMMIT_W,
                    TRAILS_W,
                    FINALIZE_OFF,
                    bytes32(0) // selfColdkey: compute on-chain via blake2f
                )
            )
        );
        st = STSubnet(payable(address(proxy)));

        proxyMirror = Blake2b.mirror(address(st));

        staking.setColdkey(address(st), proxyMirror);

        // metagraph: uid0 = someone else, uid1 = the pool hotkey (owned by the
        // contract's mirror)
        metagraph.setNeuron(0, keccak256("other-hotkey"), keccak256("other-coldkey"));
        metagraph.setNeuron(1, MINER_HOTKEY, proxyMirror);

        vm.startPrank(owner);
        st.registerOperator(NO_ID, NO_COLDKEY, MINER_HOTKEY);
        st.setOperatorAddress(NO_ID, noAddr);
        vm.stopPrank();
    }

    // ------------------------------------------------------------------
    // init / registry assertions
    // ------------------------------------------------------------------

    function test_initialize_state() public view {
        assertEq(st.netuid(), NETUID);
        assertEq(st.owner(), owner);
        assertEq(st.guardian(), guardian);
        assertEq(st.treasuryHotkey(), TREASURY);
        assertEq(st.reserveHotkey(), RESERVE);
        assertEq(st.buybackTotal(), 0);
        assertEq(st.tEpoch(), T_EPOCH);
        assertEq(st.epoch(), 0);
        assertEq(st.epochStartBlock(), START_BLOCK);
        // initializer computed mirror(proxy) on-chain via blake2f
        assertEq(st.selfColdkey(), proxyMirror);

        (bytes32 ck, uint16 uid, bytes32 hk, bool active) = st.operators(NO_ID);
        assertEq(ck, NO_COLDKEY);
        assertEq(uid, 1); // scanned out of the mock metagraph
        assertEq(hk, MINER_HOTKEY);
        assertTrue(active);
        assertEq(neuron.registerCount(), 1);
    }

    function test_initialize_guards_reserveHotkey() public {
        STSubnet impl = new STSubnet();

        // reserve hotkey must be set
        vm.expectRevert("ST: reserve hotkey 0");
        new ERC1967Proxy(
            address(impl),
            abi.encodeCall(
                STSubnet.initialize,
                (
                    NETUID,
                    owner,
                    guardian,
                    TREASURY,
                    bytes32(0),
                    T_EPOCH,
                    COMMIT_W,
                    TRAILS_W,
                    FINALIZE_OFF,
                    bytes32(0)
                )
            )
        );

        // ...and must differ from the escrow (dividends would corrupt the
        // push-then-credit attribution check)
        vm.expectRevert("ST: reserve==treasury");
        new ERC1967Proxy(
            address(impl),
            abi.encodeCall(
                STSubnet.initialize,
                (
                    NETUID,
                    owner,
                    guardian,
                    TREASURY,
                    TREASURY,
                    T_EPOCH,
                    COMMIT_W,
                    TRAILS_W,
                    FINALIZE_OFF,
                    bytes32(0)
                )
            )
        );
    }

    // ------------------------------------------------------------------
    // blake2b known-answer vectors (generated with python hashlib.blake2b)
    // ------------------------------------------------------------------

    function test_blake2b_vectors() public view {
        assertEq(
            Blake2b.hash256(""),
            bytes32(0x0e5751c026e543b2e8ab2eb06099daa1d1e5df47778f7787faab45cdf12fe3a8)
        );
        assertEq(
            Blake2b.hash256("abc"),
            bytes32(0xbddd813c634239723171ef3fee98579b94964e3bb1cb3e427262c8c068d52319)
        );
        assertEq(
            Blake2b.mirror(0x1111111111111111111111111111111111111111),
            bytes32(0x32f955c958e51189a4921aed41ef00818f7368dfaec8d9969f091006f8066228)
        );
        assertEq(
            Blake2b.mirror(0xDeaDbeefdEAdbeefdEadbEEFdeadbeEFdEaDbeeF),
            Blake2b.hash256(abi.encodePacked(bytes4(0x65766d3a), 0xDeaDbeefdEAdbeefdEadbEEFdeadbeEFdEaDbeeF))
        );
        // full single block (128 bytes)
        bytes memory full = new bytes(128);
        for (uint256 i = 0; i < 128; i++) {
            full[i] = 0xaa;
        }
        assertEq(
            Blake2b.hash256(full),
            bytes32(0x9d92efcb1dfd5882b7a7e6fa955ce225585a03d077044be3d855a118f736af5a)
        );
    }

    // ------------------------------------------------------------------
    // deposits are buybacks (§7.4/D23)
    // ------------------------------------------------------------------

    function test_deposit_movesFullAmountToReserve() public {
        staking.setStake(TREASURY, proxyMirror, DEPOSIT);

        vm.expectEmit(true, true, false, true);
        emit Deposited(0, NO_ID, noAddr, DEPOSIT);
        vm.expectEmit(true, true, false, true);
        emit BuybackReserved(0, NO_ID, DEPOSIT, DEPOSIT);
        vm.prank(noAddr);
        st.deposit(NO_ID, DEPOSIT);

        // the deposit is recorded by the Deposited event (expected above), and
        // the α left the escrow for the reserve, in full
        assertEq(staking.stakes(TREASURY, proxyMirror), 0, "escrow drained");
        assertEq(staking.stakes(RESERVE, proxyMirror), DEPOSIT, "reserve holds the buyback");
        assertEq(st.buybackTotal(), DEPOSIT);
        // the escrow ledger never counted it (it left in the same call)
        assertEq(st.accountedStake(), 0);
    }

    function test_deposit_revertsWhenReserveMoveFails() public {
        staking.setStake(TREASURY, proxyMirror, DEPOSIT);
        staking.setFailMoveStake(true);

        // strict move: a deposit either fully reserves or does not credit
        vm.prank(noAddr);
        vm.expectRevert("mock: moveStake down");
        st.deposit(NO_ID, DEPOSIT);

        assertEq(st.buybackTotal(), 0, "no credit on failed reserve move");
        assertEq(staking.stakes(TREASURY, proxyMirror), DEPOSIT, "push untouched");
    }

    // ------------------------------------------------------------------
    // happy-path epoch: pool pays EMISSION ONLY; the deposit stays reserved
    // ------------------------------------------------------------------

    function test_happyPath_fullEpoch() public {
        _depositAndAccrue();
        (bytes32 payoutRoot, bytes32[] memory proofA, bytes32[] memory proofB) = _minerTree();

        // --- epoch 0 closes at block 1100; commit within +10 ---
        vm.roll(START_BLOCK + T_EPOCH + 1); // 1101
        vm.prank(noAddr);
        st.commitOperator(0, NO_ID, payoutRoot, "ipfs://payouts-e0");

        assertEq(st.epoch(), 1, "rolled");
        assertEq(st.epochCloseBlock(0), START_BLOCK + T_EPOCH);
        assertEq(st.poolEmission(0, NO_ID), EMISSION, "stake-delta emission");
        // sweep consolidated the pool hotkey onto the treasury escrow; the
        // deposit is NOT there — it sits in the reserve
        assertEq(staking.stakes(MINER_HOTKEY, proxyMirror), 0);
        assertEq(staking.stakes(TREASURY, proxyMirror), EMISSION);
        assertEq(staking.stakes(RESERVE, proxyMirror), DEPOSIT);

        // --- finalize at +30: poolTotal = emission only (§8.3/D23) ---
        vm.roll(START_BLOCK + T_EPOCH + FINALIZE_OFF); // 1130
        st.finalizeEpoch(0);
        assertTrue(st.finalized(0));
        assertEq(st.poolTotal(0, NO_ID), EMISSION);

        // --- miner claims (permissionless, D-2) ---
        st.claimMiner(0, NO_ID, PROV_A, 6_000, proofA);
        assertEq(staking.stakes(TREASURY, PROV_A), (EMISSION * 6_000) / 10_000);

        vm.expectRevert("ST: claimed");
        st.claimMiner(0, NO_ID, PROV_A, 6_000, proofA);

        st.claimMiner(0, NO_ID, PROV_B, 4_000, proofB);
        assertEq(staking.stakes(TREASURY, PROV_B), (EMISSION * 4_000) / 10_000);
        assertEq(st.claimedMiner(0, NO_ID), EMISSION);

        // conservation: the escrow drained to the claimants; the reserve is
        // untouched and never claimable (the one-way invariant)
        assertEq(staking.stakes(TREASURY, proxyMirror), 0);
        assertEq(st.accountedStake(), 0);
        assertEq(staking.stakes(RESERVE, proxyMirror), DEPOSIT);
        assertEq(st.buybackTotal(), DEPOSIT);
    }

    // ------------------------------------------------------------------
    // guardian pause never blocks finalized claims; upgrade under fire
    // ------------------------------------------------------------------

    function test_pauseAndUpgrade_cannotBlockFinalizedClaims() public {
        _depositAndAccrue();
        (bytes32 payoutRoot, bytes32[] memory proofA,) = _minerTree();

        vm.roll(START_BLOCK + T_EPOCH + 1);
        vm.prank(noAddr);
        st.commitOperator(0, NO_ID, payoutRoot, "");
        vm.roll(START_BLOCK + T_EPOCH + FINALIZE_OFF);
        st.finalizeEpoch(0);

        // guardian pauses new activity...
        vm.prank(guardian);
        st.setPaused(true);
        vm.prank(noAddr);
        vm.expectRevert("ST: paused");
        st.deposit(NO_ID, 1);

        // ...and the owner upgrades the implementation mid-flight...
        STSubnet impl2 = new STSubnet();
        vm.prank(owner);
        st.upgradeToAndCall(address(impl2), "");

        // ...but the finalized epoch's claims still pay (structural invariant),
        // funded by emission only
        st.claimMiner(0, NO_ID, PROV_A, 6_000, proofA);
        assertEq(staking.stakes(TREASURY, PROV_A), (EMISSION * 6_000) / 10_000);
        // and the reserve is still intact
        assertEq(staking.stakes(RESERVE, proxyMirror), DEPOSIT);
    }

    // ------------------------------------------------------------------
    // missed commit -> pool total (emission only) carries into the next epoch
    // ------------------------------------------------------------------

    function test_missedCommit_carriesPoolTotal() public {
        _depositAndAccrue();

        // nobody commits for epoch 0
        vm.roll(START_BLOCK + T_EPOCH + FINALIZE_OFF);
        st.finalizeEpoch(0);
        assertEq(st.poolTotal(0, NO_ID), 0);
        assertEq(st.carry(NO_ID), EMISSION);

        // epoch 1: commit happens -> carry lands in poolTotal[1]
        (bytes32 payoutRoot,,) = _minerTree();
        vm.roll(START_BLOCK + 2 * T_EPOCH + 1); // 1201: epoch 1 closed
        vm.prank(noAddr);
        st.commitOperator(1, NO_ID, payoutRoot, "");
        vm.roll(START_BLOCK + 2 * T_EPOCH + FINALIZE_OFF);
        st.finalizeEpoch(1);
        assertEq(st.poolTotal(1, NO_ID), EMISSION);
        assertEq(st.carry(NO_ID), 0);
    }

    // ------------------------------------------------------------------
    // helpers
    // ------------------------------------------------------------------

    /// @dev NO pushes a deposit (simulated: stake appears on the treasury
    ///      hotkey under the contract's coldkey), credits it (which reserves
    ///      it in full), and the pool hotkey accrues emission.
    function _depositAndAccrue() internal {
        staking.setStake(TREASURY, proxyMirror, DEPOSIT);
        vm.prank(noAddr);
        st.deposit(NO_ID, DEPOSIT);
        assertEq(st.buybackTotal(), DEPOSIT);
        assertEq(staking.stakes(RESERVE, proxyMirror), DEPOSIT);

        // Yuma credits pool emission on the pool's own hotkey (D-4)
        staking.setStake(MINER_HOTKEY, proxyMirror, EMISSION);
    }

    function _hp(bytes32 a, bytes32 b) internal pure returns (bytes32) {
        return a < b ? keccak256(abi.encodePacked(a, b)) : keccak256(abi.encodePacked(b, a));
    }

    /// @dev two-leaf payout tree: (PROV_A, 60%), (PROV_B, 40%)
    function _minerTree()
        internal
        view
        returns (bytes32 root, bytes32[] memory proofA, bytes32[] memory proofB)
    {
        bytes32 la = st.minerLeafHash(PROV_A, 6_000);
        bytes32 lb = st.minerLeafHash(PROV_B, 4_000);
        root = _hp(la, lb);
        proofA = new bytes32[](1);
        proofA[0] = lb;
        proofB = new bytes32[](1);
        proofB[0] = la;
    }
}
