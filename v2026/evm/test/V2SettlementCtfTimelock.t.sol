// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {TimelockController} from "@openzeppelin/contracts/governance/TimelockController.sol";
import {IERC1967} from "@openzeppelin/contracts/interfaces/IERC1967.sol";
import {ERC1967Utils} from "@openzeppelin/contracts/proxy/ERC1967/ERC1967Utils.sol";
import {Errors} from "@openzeppelin/contracts/utils/Errors.sol";
import {UUPSUpgradeable} from "@openzeppelin/contracts-upgradeable/proxy/utils/UUPSUpgradeable.sol";

import {V2StackBase} from "./utils/V2StackBase.sol";
import {STSettlement2PhaseC} from "../src/STSettlement2PhaseC.sol";
import {STSettlement2Ctf} from "../src/STSettlement2Ctf.sol";
import {ISTSettlement2} from "../src/interfaces/ISTSettlement2.sol";
import {ISTSettlementVault2} from "../src/interfaces/ISTSettlementVault2.sol";

/// @dev The capture the flag upgrade and the CTF pool's registration as one
///      operation of the TimelockController that owns STSettlement2 on
///      mainnet (CAPTURETHEFLAG.md §3.1, §11.2): OpenZeppelin 5.6.1's, as
///      sn/evm builds it, with mainnet's configuration (V2SettlementTimelock.t.sol
///      TL-1, TL-2). The proxy runs phase C. The operation's call is
///      upgradeToAndCall(STSettlement2Ctf, registerCtfPool(1, maxBurn, E)) and
///      its value maxBurn × 10^9 wei, so the call's msg.sender is the timelock,
///      the owner, and its msg.value the burn. CT-1 and CT-2 run it with the
///      value sent by the executor and held by the timelock, CT-3 retries a
///      refused registration in a later block, CT-4 and CT-5 cover executions
///      that must fail, and CT-6 why the two steps cannot go as two operations.
contract V2SettlementCtfTimelockTest is V2StackBase {
    uint256 internal constant DELAY = 72 hours;
    uint256 internal constant CTF1 = (1 << 32) + 1;
    uint64 internal constant CTF_FIRST_EPOCH = FIRST_EPOCH + 1;
    uint16 internal constant CTF1_UID = 31;
    uint256 internal constant BURN_WEI = uint256(V2_BURN_LIMIT) * 1e9;
    bytes32 internal constant READY = bytes32(uint256(1) << uint8(TimelockController.OperationState.Ready));

    TimelockController internal timelock;
    address internal phaseCImplementation;
    address internal ctfImplementation;

    function setUp() public override {
        super.setUp();
        _registerV2Pool(NO1, V2_POOL1, V2_POOL1_UID);
        phaseCImplementation = address(new STSettlement2PhaseC());
        vm.prank(owner);
        settlement.upgradeToAndCall(phaseCImplementation, "");
        address[] memory proposers = new address[](1);
        proposers[0] = owner;
        address[] memory executors = new address[](1);
        executors[0] = address(0);
        timelock = new TimelockController(DELAY, proposers, executors, address(0));
        vm.prank(owner);
        settlement.transferOwnership(address(timelock));
        ctfImplementation = address(new STSettlement2Ctf());
    }

    function _implementation() internal view returns (address) {
        return address(uint160(uint256(vm.load(address(settlement), ERC1967Utils.IMPLEMENTATION_SLOT))));
    }

    /// @dev The folded call: the upgrade, with the registration as its call.
    function _folded(uint64 firstEpoch) internal view returns (bytes memory) {
        return abi.encodeCall(
            UUPSUpgradeable.upgradeToAndCall,
            (
                ctfImplementation,
                abi.encodeCall(STSettlement2Ctf.registerCtfPool, (NO1, V2_BURN_LIMIT, firstEpoch))
            )
        );
    }

    /// @dev The Safe (owner) schedules the call with the burn as its value,
    ///      and the delay passes.
    function _scheduleAndWait(bytes memory data) internal returns (bytes32 id) {
        id = timelock.hashOperation(address(settlement), BURN_WEI, data, bytes32(0), bytes32(0));
        vm.expectEmit(true, true, true, true, address(timelock));
        emit TimelockController.CallScheduled(id, 0, address(settlement), BURN_WEI, data, bytes32(0), DELAY);
        vm.prank(owner);
        timelock.schedule(address(settlement), BURN_WEI, data, bytes32(0), bytes32(0), DELAY);
        vm.warp(block.timestamp + DELAY - 1);
        vm.expectRevert(
            abi.encodeWithSelector(TimelockController.TimelockUnexpectedOperationState.selector, id, READY)
        );
        vm.prank(stranger);
        timelock.execute(address(settlement), BURN_WEI, data, bytes32(0), bytes32(0));
        vm.warp(block.timestamp + 1);
        assertTrue(timelock.isOperationReady(id));
    }

    /// @dev The hotkey the execution derives in the current block, whose UID
    ///      the runtime assigns as it registers.
    function _derived() internal returns (bytes32 hotkey) {
        hotkey = keccak256(abi.encode(address(settlement), CTF1, block.number, blockhash(block.number - 1)));
        neuron.setUid(NETUID, hotkey, CTF1_UID);
    }

    function _executeAs(address sender, uint256 value, bytes memory data) internal {
        vm.prank(sender);
        timelock.execute{value: value}(address(settlement), BURN_WEI, data, bytes32(0), bytes32(0));
    }

    function _assertRegistered(bytes32 hotkey) internal view {
        assertEq(_implementation(), ctfImplementation);
        assertEq(STSettlement2Ctf(payable(address(settlement))).owner(), address(timelock));
        ISTSettlementVault2.Pool memory pool = vault2.pools(CTF1);
        assertTrue(pool.registered);
        assertEq(pool.hotkey, hotkey);
        assertEq(pool.firstEpoch, CTF_FIRST_EPOCH);
        (address v, bytes32 resolved, uint64 first) =
            STSettlement2Ctf(payable(address(settlement))).ctfSettlementAt(NO1, CTF_FIRST_EPOCH);
        assertEq(v, address(vault2));
        assertEq(resolved, hotkey);
        assertEq(first, CTF_FIRST_EPOCH);
    }

    // CT-1: the Safe schedules the folded call with the burn as its value; it
    // cannot run before the delay; then anyone executes it, sending the value.
    // One transaction upgrades the proxy and registers the CTF pool under the
    // hotkey derived in the execution block; vault2's refund of the burn
    // surplus returns to the timelock, the caller of registerCtfPool.
    function test_CT1_oneOperationUpgradesAndRegistersWithTheExecutorsValue() public {
        bytes memory data = _folded(CTF_FIRST_EPOCH);
        bytes32 id = _scheduleAndWait(data);
        vm.roll(block.number + 1);
        vm.setBlockhash(block.number - 1, keccak256("execution parent"));
        bytes32 hotkey = _derived();
        assertEq(
            hotkey,
            keccak256(abi.encode(address(settlement), CTF1, block.number, keccak256("execution parent")))
        );
        neuron.setRegistrationBurn(BURN_WEI / 4);
        vm.deal(stranger, BURN_WEI);

        vm.expectEmit(true, true, true, true, address(settlement));
        emit IERC1967.Upgraded(ctfImplementation);
        vm.expectEmit(true, true, true, true, address(vault2));
        emit ISTSettlementVault2.PoolRegistered(CTF1, hotkey, CTF1_UID, CTF_FIRST_EPOCH);
        vm.expectEmit(true, true, true, true, address(timelock));
        emit TimelockController.CallExecuted(id, 0, address(settlement), BURN_WEI, data);
        _executeAs(stranger, BURN_WEI, data);
        uint256 gas = vm.lastCallGas().gasTotalUsed;

        _assertRegistered(hotkey);
        assertEq(neuron.lastRegistrant(), address(vault2));
        assertEq(neuron.lastLimitPrice(), V2_BURN_LIMIT);
        assertEq(address(timelock).balance, BURN_WEI - BURN_WEI / 4, "the surplus returns to the timelock");
        assertEq(stranger.balance, 0);
        assertEq(address(settlement).balance, 0);
        assertEq(address(vault2).balance, 0);
        assertTrue(timelock.isOperationDone(id));
        vm.expectRevert(
            abi.encodeWithSelector(TimelockController.TimelockUnexpectedOperationState.selector, id, READY)
        );
        _executeAs(stranger, 0, data);
        emit log_named_uint("timelock execute of the folded call gas", gas);
    }

    // CT-2: funded beforehand, as review prepares it, the timelock pays the
    // value from its own balance: any key executes with no value, and the
    // surplus stays with the timelock.
    function test_CT2_aFundedTimelockPaysTheValueAndAnyoneExecutes() public {
        address funder = makeAddr("funder");
        vm.deal(funder, BURN_WEI);
        vm.prank(funder);
        (bool funded,) = address(timelock).call{value: BURN_WEI}("");
        assertTrue(funded);
        bytes memory data = _folded(CTF_FIRST_EPOCH);
        bytes32 id = _scheduleAndWait(data);
        neuron.setRegistrationBurn(BURN_WEI / 2);
        bytes32 hotkey = _derived();
        _executeAs(stranger, 0, data);
        _assertRegistered(hotkey);
        assertTrue(timelock.isOperationDone(id));
        assertEq(address(timelock).balance, BURN_WEI / 2);
    }

    // CT-3, §3.1: an execution whose registration the runtime refuses, as it
    // refuses a hotkey someone else registered first, reverts as a whole: the
    // proxy stays on phase C and the operation stays ready. A later block
    // derives another hotkey, and the same operation then succeeds.
    function test_CT3_aRefusedRegistrationRetriesInALaterBlockUnderAnotherHotkey() public {
        vm.deal(address(timelock), BURN_WEI);
        bytes memory data = _folded(CTF_FIRST_EPOCH);
        bytes32 id = _scheduleAndWait(data);
        bytes32 refused = _derived();
        neuron.setFailRegistration(true);
        vm.expectRevert("mock: registration refused");
        _executeAs(stranger, 0, data);
        assertEq(_implementation(), phaseCImplementation);
        assertTrue(timelock.isOperationReady(id));
        assertFalse(vault2.pools(CTF1).registered);
        assertFalse(vault2.poolHotkeyUsed(refused));
        assertEq(address(timelock).balance, BURN_WEI);

        neuron.setFailRegistration(false);
        vm.roll(block.number + 1);
        bytes32 hotkey = _derived();
        assertTrue(hotkey != refused);
        _executeAs(stranger, 0, data);
        _assertRegistered(hotkey);
        assertTrue(timelock.isOperationDone(id));
    }

    // CT-4: without the value, from the executor or the timelock's balance,
    // the call cannot run: the execution reverts and the operation stays
    // ready for a funded one.
    function test_CT4_anUnfundedExecutionRevertsAndStaysReady() public {
        bytes memory data = _folded(CTF_FIRST_EPOCH);
        bytes32 id = _scheduleAndWait(data);
        _derived();
        vm.expectRevert(Errors.FailedCall.selector);
        _executeAs(stranger, 0, data);
        vm.deal(stranger, BURN_WEI - 1);
        vm.expectRevert(Errors.FailedCall.selector);
        _executeAs(stranger, BURN_WEI - 1, data);
        assertEq(_implementation(), phaseCImplementation);
        assertTrue(timelock.isOperationReady(id));
        vm.deal(stranger, BURN_WEI);
        _executeAs(stranger, BURN_WEI, data);
        assertTrue(timelock.isOperationDone(id));
    }

    // CT-5, §11.2: registerCtfPool requires the first epoch to be in the
    // future, so an operation executed once that epoch has started reverts
    // with InvalidEpoch, and the start moves to a new operation.
    function test_CT5_anExecutionAfterTheFirstEpochStartsReverts() public {
        vm.deal(address(timelock), BURN_WEI);
        bytes memory data = _folded(CTF_FIRST_EPOCH);
        bytes32 id = _scheduleAndWait(data);
        vm.roll(coordinator.epochStartBlock(CTF_FIRST_EPOCH));
        _derived();
        vm.expectRevert(ISTSettlement2.InvalidEpoch.selector);
        _executeAs(stranger, 0, data);
        assertEq(_implementation(), phaseCImplementation);
        assertTrue(timelock.isOperationReady(id));
    }

    // CT-6, §3.1: the registration cannot be scheduled on its own before the
    // upgrade: phase C has no registerCtfPool, so the call from the timelock
    // reverts, which is why review folds it into the upgrade.
    function test_CT6_phaseCHasNoRegistrationToScheduleAlone() public {
        vm.deal(address(timelock), BURN_WEI);
        vm.prank(address(timelock));
        (bool ok,) = address(settlement).call{value: BURN_WEI}(
            abi.encodeCall(STSettlement2Ctf.registerCtfPool, (NO1, V2_BURN_LIMIT, CTF_FIRST_EPOCH))
        );
        assertFalse(ok);
        assertFalse(vault2.pools(CTF1).registered);
    }
}
