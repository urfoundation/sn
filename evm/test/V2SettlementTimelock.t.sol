// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {TimelockController} from "@openzeppelin/contracts/governance/TimelockController.sol";
import {IAccessControl} from "@openzeppelin/contracts/access/IAccessControl.sol";
import {ERC1967Utils} from "@openzeppelin/contracts/proxy/ERC1967/ERC1967Utils.sol";
import {OwnableUpgradeable} from "@openzeppelin/contracts-upgradeable/access/OwnableUpgradeable.sol";
import {UUPSUpgradeable} from "@openzeppelin/contracts-upgradeable/proxy/utils/UUPSUpgradeable.sol";

import {V2StackBase} from "./utils/V2StackBase.sol";
import {STSettlement2} from "../src/STSettlement2.sol";
import {STSettlement2PhaseC} from "../src/STSettlement2PhaseC.sol";
import {ISTSettlementVault2} from "../src/interfaces/ISTSettlementVault2.sol";

/// @dev Since 2026-11-06 STSettlement2's mainnet owner is OpenZeppelin
///      5.6.1's TimelockController, configured as here: a 72 h minimum delay,
///      the governance Safe (here `owner`) as its only proposer and canceller,
///      execution open to anyone, and no admin but itself. The proxy runs the
///      phase C implementation, as it did when the Safe transferred
///      ownership. A stolen Safe key can no longer upgrade, swap auditors or
///      assign surplus at once: it must schedule in public, the Safe can
///      cancel, and the delay outlasts vault2's challenge window. No
///      STSettlement2 code changes. TL-1 pins this build to the mainnet
///      timelock's code, TL-2 its configuration, TL-3 to TL-6 its behaviour.
contract V2SettlementTimelockTest is V2StackBase {
    uint256 internal constant DELAY = 72 hours;
    bytes32 internal constant PROPOSER_ROLE = keccak256("PROPOSER_ROLE");
    bytes32 internal constant CANCELLER_ROLE = keccak256("CANCELLER_ROLE");
    bytes32 internal constant EXECUTOR_ROLE = keccak256("EXECUTOR_ROLE");
    bytes32 internal constant DEFAULT_ADMIN_ROLE = 0x00;
    bytes32 internal constant READY = bytes32(uint256(1) << uint8(TimelockController.OperationState.Ready));

    // The mainnet timelock 0x5529dfe070f73A96C14b5aFCf4fb3DdeA6122fAc: the hash
    // of its 5,279-byte runtime, which sn/mainnet's reviews require
    // (settlement2TimelockCodeHash); the hash of its first 5,226 bytes, the
    // executable code before the CBOR metadata; and that metadata. The
    // metadata hashes the absolute library paths of the directory it was
    // built in, so a build elsewhere reproduces only the executable bytes.
    bytes32 internal constant MAINNET_RUNTIME_HASH =
        0xe3a14af165a6b22bbe7e2c4259833eed0f02e64acb782992c0c4f50ab32eb880;
    bytes32 internal constant MAINNET_EXECUTABLE_HASH =
        0x838d8661b1827ef5b1fdc74286dc58471798f98993db03c5ceadba9b94435ab5;
    bytes internal constant MAINNET_METADATA =
        hex"a264697066735822122005822dc92c778c13b13b822e19d00cc0c6b5919f0c86d97ba134de54c6d829dc64736f6c63430008180033";

    TimelockController internal timelock;
    STSettlement2PhaseC internal phaseC;
    address internal unreviewed;

    function setUp() public override {
        super.setUp();
        _registerV2Pool(NO1, V2_POOL1, V2_POOL1_UID);
        address next = _newPhaseCImplementation();
        vm.prank(owner);
        settlement.upgradeToAndCall(next, "");
        phaseC = STSettlement2PhaseC(payable(address(settlement)));
        address[] memory proposers = new address[](1);
        proposers[0] = owner;
        address[] memory executors = new address[](1);
        executors[0] = address(0);
        timelock = new TimelockController(DELAY, proposers, executors, address(0));
        vm.prank(owner);
        settlement.transferOwnership(address(timelock));
        // Any implementation nobody reviewed: here, back to phase A.
        unreviewed = address(new STSettlement2());
    }

    /// @dev The implementation the proxy runs, phase C's. The suite reruns
    ///      against the capture the flag upgrade by overriding this
    ///      (V2SettlementCtfRegression.t.sol).
    function _newPhaseCImplementation() internal virtual returns (address) {
        return address(new STSettlement2PhaseC());
    }

    function _implementation() internal view returns (address) {
        return address(uint160(uint256(vm.load(address(settlement), ERC1967Utils.IMPLEMENTATION_SLOT))));
    }

    function _schedule(bytes memory data) internal returns (bytes32 id) {
        id = timelock.hashOperation(address(settlement), 0, data, bytes32(0), bytes32(0));
        vm.expectEmit(true, true, true, true, address(timelock));
        emit TimelockController.CallScheduled(id, 0, address(settlement), 0, data, bytes32(0), DELAY);
        vm.prank(owner);
        timelock.schedule(address(settlement), 0, data, bytes32(0), bytes32(0), DELAY);
        assertEq(timelock.getTimestamp(id), block.timestamp + DELAY);
    }

    function _executeAs(address sender, bytes memory data) internal {
        vm.prank(sender);
        timelock.execute(address(settlement), 0, data, bytes32(0), bytes32(0));
    }

    // TL-1: the runtime of this build, with no immutables, has exactly the
    // mainnet timelock's executable bytes, and with its metadata its code.
    function test_theBuildIsTheMainnetTimelock() public view {
        bytes memory runtime = type(TimelockController).runtimeCode;
        assertEq(keccak256(address(timelock).code), keccak256(runtime), "an immutable");
        uint256 metadata =
            (uint256(uint8(runtime[runtime.length - 2])) << 8 | uint8(runtime[runtime.length - 1])) + 2;
        assertEq(metadata, MAINNET_METADATA.length);
        bytes memory executable = new bytes(runtime.length - metadata);
        for (uint256 i = 0; i < executable.length; i++) {
            executable[i] = runtime[i];
        }
        assertEq(executable.length, 5_226);
        assertEq(keccak256(executable), MAINNET_EXECUTABLE_HASH);
        assertEq(keccak256(bytes.concat(executable, MAINNET_METADATA)), MAINNET_RUNTIME_HASH);
    }

    // TL-2: the owner choices of 2026-11-06. The delay outlasts vault2's
    // 14,400-block challenge window at 12 s a block.
    function test_theMainnetConfiguration() public view {
        assertEq(settlement.owner(), address(timelock));
        assertEq(timelock.getMinDelay(), DELAY);
        assertGt(DELAY, uint256(WINDOW) * 12);
        assertTrue(timelock.hasRole(PROPOSER_ROLE, owner));
        assertTrue(timelock.hasRole(CANCELLER_ROLE, owner));
        assertTrue(timelock.hasRole(EXECUTOR_ROLE, address(0)), "execution is open");
        assertTrue(timelock.hasRole(DEFAULT_ADMIN_ROLE, address(timelock)), "self-administered");
        assertFalse(timelock.hasRole(DEFAULT_ADMIN_ROLE, owner));
        assertFalse(timelock.hasRole(EXECUTOR_ROLE, owner));
        assertFalse(timelock.hasRole(PROPOSER_ROLE, stranger));
        assertFalse(timelock.hasRole(CANCELLER_ROLE, stranger));
    }

    // TL-3: a stolen Safe key cannot upgrade, swap auditors, make the sweep's
    // owner calls or take the ownership back, and cannot shorten the delay,
    // grant itself a role or schedule below the delay.
    function test_aStolenSafeKeyCannotActAtOnce() public {
        address before = _implementation();
        address[] memory rogue = new address[](1);
        rogue[0] = makeAddr("rogue-auditor");
        bytes memory unauthorized =
            abi.encodeWithSelector(OwnableUpgradeable.OwnableUnauthorizedAccount.selector, owner);
        vm.startPrank(owner);
        vm.expectRevert(unauthorized);
        settlement.upgradeToAndCall(unreviewed, "");
        vm.expectRevert(unauthorized);
        settlement.setAuditors(rogue, 1);
        vm.expectRevert(unauthorized);
        phaseC.assignSurplus(NO1, FIRST_EPOCH, 1);
        vm.expectRevert(unauthorized);
        phaseC.recordV1Sweep(NO1, FIRST_EPOCH, keccak256("sweep record"));
        vm.expectRevert(unauthorized);
        settlement.transferOwnership(owner);

        vm.expectRevert(abi.encodeWithSelector(TimelockController.TimelockUnauthorizedCaller.selector, owner));
        timelock.updateDelay(0);
        vm.expectRevert(
            abi.encodeWithSelector(
                IAccessControl.AccessControlUnauthorizedAccount.selector, owner, DEFAULT_ADMIN_ROLE
            )
        );
        timelock.grantRole(EXECUTOR_ROLE, owner);
        bytes memory data = abi.encodeCall(UUPSUpgradeable.upgradeToAndCall, (unreviewed, ""));
        vm.expectRevert(
            abi.encodeWithSelector(TimelockController.TimelockInsufficientDelay.selector, DELAY - 1, DELAY)
        );
        timelock.schedule(address(settlement), 0, data, bytes32(0), bytes32(0), DELAY - 1);
        vm.stopPrank();
        assertEq(_implementation(), before);
        assertEq(settlement.owner(), address(timelock));
    }

    // TL-4: a scheduled upgrade is public and cannot run before the delay; a
    // stranger cannot cancel it, the Safe can, and then it never runs.
    function test_aScheduledCallIsPublicAndTheSafeCancelsIt() public {
        address before = _implementation();
        bytes memory data = abi.encodeCall(UUPSUpgradeable.upgradeToAndCall, (unreviewed, ""));
        bytes32 id = _schedule(data);

        vm.warp(block.timestamp + DELAY - 1);
        vm.expectRevert(
            abi.encodeWithSelector(TimelockController.TimelockUnexpectedOperationState.selector, id, READY)
        );
        _executeAs(stranger, data);

        vm.expectRevert(
            abi.encodeWithSelector(
                IAccessControl.AccessControlUnauthorizedAccount.selector, stranger, CANCELLER_ROLE
            )
        );
        vm.prank(stranger);
        timelock.cancel(id);
        vm.expectEmit(true, true, true, true, address(timelock));
        emit TimelockController.Cancelled(id);
        vm.prank(owner);
        timelock.cancel(id);

        vm.warp(block.timestamp + 1 days);
        vm.expectRevert(
            abi.encodeWithSelector(TimelockController.TimelockUnexpectedOperationState.selector, id, READY)
        );
        _executeAs(stranger, data);
        assertEq(_implementation(), before);
    }

    // TL-5: the carry's owner call, assignSurplus, scheduled by the Safe, runs
    // once its delay has passed, sent by anyone, exactly once.
    function test_theCarryAssignmentLandsAfterTheDelayAndAnyoneRunsIt() public {
        uint256 amount = 5_000_000_000;
        staking.setStake(ESCROW_HOTKEY, V2_SELF, staking.stakes(ESCROW_HOTKEY, V2_SELF) + amount);
        vm.prank(stranger);
        vault2.absorbStake(ESCROW_HOTKEY);
        bytes memory data = abi.encodeCall(STSettlement2PhaseC.assignSurplus, (NO1, FIRST_EPOCH, amount));
        bytes32 id = _schedule(data);

        vm.warp(block.timestamp + DELAY - 1);
        vm.expectRevert(
            abi.encodeWithSelector(TimelockController.TimelockUnexpectedOperationState.selector, id, READY)
        );
        _executeAs(stranger, data);

        vm.warp(block.timestamp + 1);
        vm.expectEmit(true, true, true, true, address(vault2));
        emit ISTSettlementVault2.SurplusAssigned(NO1, FIRST_EPOCH, amount);
        vm.expectEmit(true, true, true, true, address(timelock));
        emit TimelockController.CallExecuted(id, 0, address(settlement), 0, data);
        _executeAs(stranger, data);
        assertEq(vault2.capturedTotal(NO1), amount);
        assertTrue(timelock.isOperationDone(id));

        vm.expectRevert(
            abi.encodeWithSelector(TimelockController.TimelockUnexpectedOperationState.selector, id, READY)
        );
        _executeAs(stranger, data);
    }

    // TL-6: a reviewed upgrade still lands after the delay, and the timelock
    // keeps the ownership.
    function test_aReviewedUpgradeLandsAfterTheDelay() public {
        address reviewed = address(new STSettlement2PhaseC());
        bytes memory data = abi.encodeCall(UUPSUpgradeable.upgradeToAndCall, (reviewed, ""));
        _schedule(data);
        vm.warp(block.timestamp + DELAY);
        _executeAs(stranger, data);
        assertEq(_implementation(), reviewed);
        assertEq(settlement.owner(), address(timelock));
    }
}
