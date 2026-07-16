// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {OwnableUpgradeable} from
    "@openzeppelin/contracts-upgradeable/access/OwnableUpgradeable.sol";
import {Initializable} from "@openzeppelin/contracts-upgradeable/proxy/utils/Initializable.sol";
import {ERC1967Proxy} from "@openzeppelin/contracts/proxy/ERC1967/ERC1967Proxy.sol";

import {STBase} from "./utils/STBase.sol";
import {STSubnet} from "../src/STSubnet.sol";
import {Blake2b} from "../src/lib/Blake2b.sol";

/// @dev Area 4 — registration: registerOperator via mocked 0x804
///      burnedRegister (incl. the treasury/reserve hotkey-collision guards)
///      and initializer guards. (The validator registry + server-key registry
///      are deferred with the effort bounty, §9.3/D23 — their v0.2 suite is
///      parked at docs/parked/.) blake2b known-answer vectors live in the
///      smoke suite (test_blake2b_vectors).
contract RegistrationTest is STBase {
    event OperatorRegistered(
        uint256 indexed noId, bytes32 coldkey, uint16 minerUid, bytes32 minerHotkey
    );

    // ------------------------------------------------------------------
    // registerOperator — burnedRegister via mocked 0x804
    // ------------------------------------------------------------------

    function test_registerOperator_burnedRegistersAndScansUid() public {
        bytes32 hk = keccak256("new-pool-hotkey");
        metagraph.setNeuron(5, hk, proxyMirror);
        uint256 burnsBefore = neuron.registerCount();

        vm.expectEmit(true, false, false, true, address(st));
        emit OperatorRegistered(9, keccak256("no9-ck"), 5, hk);
        vm.prank(owner);
        st.registerOperator(9, keccak256("no9-ck"), hk);

        assertEq(neuron.registerCount(), burnsBefore + 1, "burnedRegister called");
        assertEq(neuron.lastNetuid(), NETUID);
        assertEq(neuron.lastHotkey(), hk);
        (bytes32 ck, uint16 uid, bytes32 mhk, bool active) = st.operators(9);
        assertEq(ck, keccak256("no9-ck"));
        assertEq(uid, 5, "uid found by linear metagraph scan");
        assertEq(mhk, hk);
        assertTrue(active);
        assertEq(st.operatorCount(), 2);
        assertTrue(st.minerHotkeyUsed(hk));
    }

    function test_registerOperator_guards() public {
        vm.prank(rando);
        vm.expectRevert(
            abi.encodeWithSelector(OwnableUpgradeable.OwnableUnauthorizedAccount.selector, rando)
        );
        st.registerOperator(9, keccak256("c"), keccak256("h"));

        vm.startPrank(owner);
        vm.expectRevert("ST: noId exists");
        st.registerOperator(NO_ID, keccak256("c"), keccak256("h"));
        vm.expectRevert("ST: hotkey 0");
        st.registerOperator(9, keccak256("c"), bytes32(0));
        // finding C: a pool hotkey == treasury hotkey would double-count
        // accountedStake via the emission sweep — rejected
        vm.expectRevert("ST: hotkey==treasury");
        st.registerOperator(9, keccak256("c"), TREASURY);
        // same family (§7.4): a pool hotkey == reserve hotkey would book the
        // whole buyback reserve as this pool's emission at the first sweep
        vm.expectRevert("ST: hotkey==reserve");
        st.registerOperator(9, keccak256("c"), RESERVE);
        vm.expectRevert("ST: hotkey used");
        st.registerOperator(9, keccak256("c"), MINER_HOTKEY);
        // hotkey burned but absent from the metagraph => scan fails, reverts
        vm.expectRevert("ST: uid not found");
        st.registerOperator(9, keccak256("c"), keccak256("not-in-metagraph"));
        vm.stopPrank();
    }

    // ------------------------------------------------------------------
    // registry setters
    // ------------------------------------------------------------------

    function test_registrySetters_unknownIds_revert() public {
        vm.startPrank(owner);
        vm.expectRevert("ST: no operator");
        st.setOperatorAddress(99, rando);
        vm.expectRevert("ST: no operator");
        st.setOperatorActive(99, true);
        vm.stopPrank();
    }

    // ------------------------------------------------------------------
    // initialize guards
    // ------------------------------------------------------------------

    function _initData(
        bytes32 treasury,
        bytes32 reserve,
        uint64 tEpoch_,
        uint64 commitW,
        uint64 trailsW,
        uint64 finOff,
        bytes32 selfCk
    ) internal view returns (bytes memory) {
        return abi.encodeCall(
            STSubnet.initialize,
            (NETUID, owner, guardian, treasury, reserve, tEpoch_, commitW, trailsW, finOff, selfCk)
        );
    }

    function test_initialize_paramValidation() public {
        STSubnet impl = new STSubnet();

        vm.expectRevert("ST: treasury hotkey 0");
        new ERC1967Proxy(
            address(impl), _initData(bytes32(0), RESERVE, 100, 10, 20, 30, bytes32(0))
        );

        vm.expectRevert("ST: reserve hotkey 0");
        new ERC1967Proxy(
            address(impl), _initData(TREASURY, bytes32(0), 100, 10, 20, 30, bytes32(0))
        );

        // §7.4: dividends compound on the reserve hotkey; sharing it with the
        // escrow would break the exact push-then-credit deposit check
        vm.expectRevert("ST: reserve==treasury");
        new ERC1967Proxy(
            address(impl), _initData(TREASURY, TREASURY, 100, 10, 20, 30, bytes32(0))
        );

        vm.expectRevert("ST: tEpoch 0");
        new ERC1967Proxy(address(impl), _initData(TREASURY, RESERVE, 0, 10, 20, 30, bytes32(0)));

        vm.expectRevert("ST: window order");
        new ERC1967Proxy(address(impl), _initData(TREASURY, RESERVE, 100, 25, 20, 30, bytes32(0)));

        vm.expectRevert("ST: window order");
        new ERC1967Proxy(address(impl), _initData(TREASURY, RESERVE, 100, 10, 40, 30, bytes32(0)));
    }

    function test_initialize_explicitSelfColdkeyOverride() public {
        // SP-1 fallback: if blake2f is unavailable on the runtime, the mirror
        // is passed in instead of computed on-chain
        STSubnet impl = new STSubnet();
        bytes32 explicitCk = keccak256("explicit-mirror");
        ERC1967Proxy p = new ERC1967Proxy(
            address(impl), _initData(TREASURY, RESERVE, 100, 10, 20, 30, explicitCk)
        );
        assertEq(STSubnet(payable(address(p))).selfColdkey(), explicitCk);
    }

    function test_initialize_onlyOnce_andImplLockedDown() public {
        vm.expectRevert(Initializable.InvalidInitialization.selector);
        st.initialize(NETUID, owner, guardian, TREASURY, RESERVE, 100, 10, 20, 30, bytes32(0));

        // the raw implementation has initializers disabled forever
        STSubnet impl = new STSubnet();
        vm.expectRevert(Initializable.InvalidInitialization.selector);
        impl.initialize(NETUID, owner, guardian, TREASURY, RESERVE, 100, 10, 20, 30, bytes32(0));
    }

    function test_receive_acceptsNativeTao() public {
        vm.deal(rando, 5 ether);
        vm.prank(rando);
        (bool ok,) = address(st).call{value: 3 ether}("");
        assertTrue(ok, "burnedRegister burns are funded via plain transfers");
        assertEq(address(st).balance, 3 ether);
    }
}
