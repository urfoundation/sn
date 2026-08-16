// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {Test} from "forge-std/Test.sol";
import {ERC1967Proxy} from "@openzeppelin/contracts/proxy/ERC1967/ERC1967Proxy.sol";

import {STSubnet} from "../../src/STSubnet.sol";
import {Blake2b} from "../../src/lib/Blake2b.sol";
import {ISTAKING_ADDRESS} from "../../src/interfaces/stakingV2.sol";
import {INeuron_ADDRESS} from "../../src/interfaces/neuron.sol";
import {IMetagraph_ADDRESS} from "../../src/interfaces/metagraph.sol";
import {IED25519VERIFY_ADDRESS} from "../../src/interfaces/ed25519Verify.sol";
import {MockStakingV2, MockNeuron, MockMetagraph, MockEd25519} from "../mocks/PrecompileMocks.sol";
import {MerkleBuilder} from "./MerkleBuilder.sol";

/// @dev Shared harness for the comprehensive STSubnet suite. Mirrors the smoke
///      suite's deployment (test/STSubnet.t.sol) but adds a second operator /
///      validator, arbitrary-size Merkle helpers (MerkleBuilder) and epoch
///      block-math helpers. Precompile mocks are vm.etch'ed at the canonical
///      addresses; blake2f (0x09) is served natively by revm.
abstract contract STBase is Test {
    // epoch params (blocks)
    uint64 constant T_EPOCH = 100;
    uint64 constant COMMIT_W = 10;
    uint64 constant TRAILS_W = 20; // reserved dial (bounty phase); gates nothing in v1
    uint64 constant FINALIZE_OFF = 30;
    uint16 constant NETUID = 77;
    uint256 constant BPS = 10_000;

    uint64 constant START_BLOCK = 1_000;

    bytes32 constant TREASURY = keccak256("treasury-hotkey");
    bytes32 constant RESERVE = keccak256("reserve-hotkey"); // owner-validator hotkey (buyback reserve, §7.4)
    bytes32 constant NO_COLDKEY = keccak256("no-coldkey");
    bytes32 constant MINER_HOTKEY = keccak256("pool-1-hotkey");
    bytes32 constant NO2_COLDKEY = keccak256("no2-coldkey");
    bytes32 constant MINER2_HOTKEY = keccak256("pool-2-hotkey");
    bytes32 constant VAL_HOTKEY = keccak256("val-hotkey"); // generic non-pool live UID (head-binding tests etc.)
    bytes32 constant PROV_A = keccak256("provider-a-coldkey");
    bytes32 constant PROV_B = keccak256("provider-b-coldkey");

    uint256 constant NO_ID = 1;
    uint256 constant NO2_ID = 2;

    address owner = makeAddr("owner");
    address guardian = makeAddr("guardian");
    address noAddr = makeAddr("no-operator");
    address no2Addr = makeAddr("no2-operator");
    address valWallet = makeAddr("val-wallet");
    address val2Wallet = makeAddr("val2-wallet");
    address rando = makeAddr("rando");

    STSubnet st;
    bytes32 proxyMirror;
    bytes32 valMirror;

    MockStakingV2 staking = MockStakingV2(ISTAKING_ADDRESS);
    MockNeuron neuron = MockNeuron(INeuron_ADDRESS);
    MockMetagraph metagraph = MockMetagraph(IMetagraph_ADDRESS);
    MockEd25519 ed = MockEd25519(IED25519VERIFY_ADDRESS);

    function setUp() public virtual {
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
        valMirror = Blake2b.mirror(valWallet);

        staking.setColdkey(address(st), proxyMirror);

        // uid0 = someone else, uid1 = pool hotkey (contract's mirror),
        // uid2 = validator 1's UID; slots >= 3 free for per-test actors.
        metagraph.setNeuron(0, keccak256("other-hotkey"), keccak256("other-coldkey"));
        metagraph.setNeuron(1, MINER_HOTKEY, proxyMirror);
        metagraph.setNeuron(2, VAL_HOTKEY, valMirror);

        vm.startPrank(owner);
        st.registerOperator(NO_ID, NO_COLDKEY, MINER_HOTKEY);
        st.setOperatorAddress(NO_ID, noAddr);
        vm.stopPrank();
    }

    // ------------------------------------------------------------------
    // actors
    // ------------------------------------------------------------------

    /// @dev Any nonzero 64-byte sig passes the 0x402 mock unless flagged bad.
    function _sig() internal pure returns (bytes memory) {
        return abi.encodePacked(keccak256("sig-r"), keccak256("sig-s"));
    }

    function _registerOperator2() internal {
        metagraph.setNeuron(3, MINER2_HOTKEY, proxyMirror);
        vm.startPrank(owner);
        st.registerOperator(NO2_ID, NO2_COLDKEY, MINER2_HOTKEY);
        st.setOperatorAddress(NO2_ID, no2Addr);
        vm.stopPrank();
    }

    // ------------------------------------------------------------------
    // stake / deposit plumbing (mock-side pushes)
    // ------------------------------------------------------------------

    /// @dev Simulate a StakingV2.transferStake push onto the custody slot
    ///      (treasuryHotkey, selfColdkey) — the on-chain prerequisite of the
    ///      push-then-credit deposit flow.
    function _push(uint256 amount) internal {
        staking.setStake(TREASURY, proxyMirror, staking.stakes(TREASURY, proxyMirror) + amount);
    }

    /// @dev Push + credit a deposit from the NO's registered wallet.
    function _deposit(uint256 noId, address from, uint256 amount) internal {
        _push(amount);
        vm.prank(from);
        st.deposit(noId, amount);
    }

    /// @dev Yuma emission accrual on a pool's own hotkey (D-4 stake delta).
    function _accrue(bytes32 minerHotkey, uint256 amount) internal {
        staking.setStake(
            minerHotkey, proxyMirror, staking.stakes(minerHotkey, proxyMirror) + amount
        );
    }

    function _treasuryStake() internal view returns (uint256) {
        return staking.stakes(TREASURY, proxyMirror);
    }

    /// @dev The buyback reserve balance (deposits moved onto the reserve
    ///      hotkey; dividends would compound here on the live chain — §7.4).
    function _reserveStake() internal view returns (uint256) {
        return staking.stakes(RESERVE, proxyMirror);
    }

    // ------------------------------------------------------------------
    // epoch block math (intended boundaries; independent of lazy rolls)
    // ------------------------------------------------------------------

    function _closeBlock(uint256 e) internal pure returns (uint256) {
        return uint256(START_BLOCK) + (e + 1) * uint256(T_EPOCH);
    }

    /// @dev Roll to just after epoch e's close (inside every window).
    function _afterClose(uint256 e) internal {
        vm.roll(_closeBlock(e) + 1);
    }

    /// @dev Roll to epoch e's finalize offset.
    function _toFinalize(uint256 e) internal {
        vm.roll(_closeBlock(e) + FINALIZE_OFF);
    }

    // ------------------------------------------------------------------
    // miner payout trees
    // ------------------------------------------------------------------

    function _minerLeaves(bytes32[] memory coldkeys, uint256[] memory shares)
        internal
        view
        returns (bytes32[] memory leaves)
    {
        leaves = new bytes32[](coldkeys.length);
        for (uint256 i = 0; i < coldkeys.length; i++) {
            leaves[i] = st.minerLeafHash(coldkeys[i], shares[i]);
        }
    }

    /// @dev The smoke suite's 60/40 tree, kept for cross-suite parity.
    function _minerTreeAB()
        internal
        view
        returns (bytes32 root, bytes32[] memory proofA, bytes32[] memory proofB)
    {
        bytes32[] memory coldkeys = new bytes32[](2);
        uint256[] memory shares = new uint256[](2);
        (coldkeys[0], shares[0]) = (PROV_A, 6_000);
        (coldkeys[1], shares[1]) = (PROV_B, 4_000);
        bytes32[] memory leaves = _minerLeaves(coldkeys, shares);
        root = MerkleBuilder.root(leaves);
        proofA = MerkleBuilder.proof(leaves, 0);
        proofB = MerkleBuilder.proof(leaves, 1);
    }

    // (effort/trail-tree helpers: deferred with the bounty — §9.3/D23;
    //  parked with the v0.2 suite at docs/parked/.)
}
