// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {Initializable} from "@openzeppelin/contracts-upgradeable/proxy/utils/Initializable.sol";
import {OwnableUpgradeable} from "@openzeppelin/contracts-upgradeable/access/OwnableUpgradeable.sol";
import {IERC1967} from "@openzeppelin/contracts/interfaces/IERC1967.sol";
import {ERC1967Utils} from "@openzeppelin/contracts/proxy/ERC1967/ERC1967Utils.sol";

import {V2StackBase} from "./utils/V2StackBase.sol";
import {V2TreeHelper} from "./utils/V2TreeHelper.sol";
import {STSettlement2} from "../src/STSettlement2.sol";
import {STSettlement2PhaseC} from "../src/STSettlement2PhaseC.sol";
import {ISTSettlement2} from "../src/interfaces/ISTSettlement2.sol";
import {ISTSettlementVault2} from "../src/interfaces/ISTSettlementVault2.sol";
import {STSettlementVault} from "../src/STSettlementVault.sol";

/// @dev The phase C upgrade of STSettlement2 (VERSION2-FINAL.md §7.2, §16;
///      meta review A5) on the whole stack: the real v1 coordinator and vault,
///      the real vault2 and the proxy upgraded from the phase A
///      implementation. SC-1 to SC-6 cover assignSurplus, SC-7 to SC-11
///      recordV1Sweep, SC-12 to SC-15 the upgrade itself, and SC-16 the whole
///      sweep from the v1 close to the next v2 funding snapshot.
contract V2SettlementPhaseCTest is V2StackBase {
    // keccak256 of a literal is a constant. sha256 would be a precompile call,
    // which takes a pending prank or expectRevert.
    bytes32 internal constant ARTIFACT = keccak256("v1 sweep record");
    bytes32 internal constant OTHER_ARTIFACT = keccak256("another sweep record");
    // STSettlement2's layout ends with its gap at slots 7 to 49
    // (evm/layout/STSettlement2.json); the phase C mapping follows it.
    uint256 internal constant SWEEPS_SLOT = 50;

    STSettlement2PhaseC internal phaseC;
    address internal phaseAImplementation;

    function setUp() public virtual override {
        super.setUp();
        _registerV2Pool(NO1, V2_POOL1, V2_POOL1_UID);
        phaseAImplementation =
            address(uint160(uint256(vm.load(address(settlement), ERC1967Utils.IMPLEMENTATION_SLOT))));
        STSettlement2PhaseC next = new STSettlement2PhaseC();
        vm.prank(owner);
        settlement.upgradeToAndCall(address(next), "");
        phaseC = STSettlement2PhaseC(payable(address(settlement)));
    }

    // ------------------------------------------------------------------
    // Helpers
    // ------------------------------------------------------------------

    /// @dev vault2's coldkey holds `amount` on the v1 escrow hotkey, as the sweep
    ///      claim leaves it, and anyone absorbs it into vault2's escrow.
    function _absorb(uint256 amount) internal {
        staking.setStake(ESCROW_HOTKEY, V2_SELF, staking.stakes(ESCROW_HOTKEY, V2_SELF) + amount);
        vm.prank(stranger);
        vault2.absorbStake(ESCROW_HOTKEY);
    }

    function _surplus() internal view returns (uint256) {
        return vault2.liveEscrowStake() - vault2.escrowAccounted();
    }

    function _next() internal view returns (uint256) {
        return vault2.lastFundedEpoch(NO1) + 1;
    }

    /// @dev The sweep root: v1's single leaf paying vault2's coldkey 10,000 bps.
    function _sweepRoot() internal view returns (bytes32) {
        return vault.payoutLeaf(V2_SELF, 10_000);
    }

    /// @dev Commits a v1 root for `epoch_` at its end, as the root signer.
    function _commitV1(uint256 epoch_, bytes32 root, bytes32 artifact) internal {
        vm.roll(_end(epoch_));
        vm.prank(rootSigner1);
        coordinator.commitOperatorRoot(epoch_, NO1, root, artifact);
    }

    function _assertSweep(uint256 noId, uint64 epoch_, bytes32 root, bytes32 artifact) internal view {
        (uint64 recordedEpoch, bytes32 recordedRoot, bytes32 recordedArtifact) = phaseC.v1Sweep(noId);
        assertEq(recordedEpoch, epoch_);
        assertEq(recordedRoot, root);
        assertEq(recordedArtifact, artifact);
    }

    // ------------------------------------------------------------------
    // SC-1 to SC-6: assignSurplus
    // ------------------------------------------------------------------

    // SC-1: the owner assigns absorbed surplus to the pool's next epoch, which
    // vault2's SurplusAssigned names; the next close snapshots it with its own
    // capture, and the epoch already funded keeps its snapshot.
    function test_SC1_ownerAssignsSurplusToTheNextEpochSnapshot() public {
        _accrueV2(V2_POOL1, 1_000);
        _closeV2(FIRST_EPOCH, NO1);
        _absorb(300);
        assertEq(_surplus(), 300);
        assertEq(_next(), FIRST_EPOCH + 1);

        vm.record();
        vm.expectEmit(address(vault2));
        emit ISTSettlementVault2.SurplusAssigned(NO1, FIRST_EPOCH + 1, 300);
        vm.prank(owner);
        phaseC.assignSurplus(NO1, FIRST_EPOCH + 1, 300);
        (, bytes32[] memory writes) = vm.accesses(address(settlement));
        assertEq(writes.length, 0, "assignSurplus writes no coordinator storage");

        assertEq(_surplus(), 0);
        assertEq(vault2.capturedTotal(NO1), 1_300);
        assertEq(vault2.escrowAccounted(), 1_300);
        assertEq(vault2.fundedThrough(NO1, FIRST_EPOCH), 1_000);
        assertTrue(vault2.conservationHolds());

        _accrueV2(V2_POOL1, 50);
        _closeV2(FIRST_EPOCH + 1, NO1);
        assertEq(vault2.fundedThrough(NO1, FIRST_EPOCH + 1), 1_350);
        assertEq(vault2.fundedThrough(NO1, FIRST_EPOCH), 1_000);
        assertTrue(vault2.conservationHolds());
    }

    // SC-2: only the owner assigns.
    function test_SC2_assignSurplusIsOwnerOnly() public {
        _absorb(300);
        uint256 next = _next();
        vm.prank(stranger);
        vm.expectRevert(
            abi.encodeWithSelector(OwnableUpgradeable.OwnableUnauthorizedAccount.selector, stranger)
        );
        phaseC.assignSurplus(NO1, next, 300);
        vm.prank(rootSigner1);
        vm.expectRevert(
            abi.encodeWithSelector(OwnableUpgradeable.OwnableUnauthorizedAccount.selector, rootSigner1)
        );
        phaseC.assignSurplus(NO1, next, 300);
        assertEq(_surplus(), 300);
    }

    // SC-3: a pool vault2 does not hold is refused, whatever the epoch.
    function test_SC3_assignSurplusRefusesAnUnregisteredPool() public {
        _absorb(300);
        uint256 next = _next();
        vm.startPrank(owner);
        vm.expectRevert(ISTSettlement2.UnknownPool.selector);
        phaseC.assignSurplus(NO2, 1, 300);
        vm.expectRevert(ISTSettlement2.UnknownPool.selector);
        phaseC.assignSurplus(NO2, next, 300);
        vm.stopPrank();
        assertEq(_surplus(), 300);
    }

    // SC-4: the named epoch must be the pool's next unfunded epoch. A close that
    // lands between the owner's signature and the call turns the assignment
    // into a refusal instead of funding the epoch after.
    function test_SC4_assignSurplusRefusesAnyEpochButTheNext() public {
        _absorb(300);
        uint256 next = _next();
        assertEq(next, FIRST_EPOCH);
        vm.startPrank(owner);
        vm.expectRevert(ISTSettlement2.InvalidEpoch.selector);
        phaseC.assignSurplus(NO1, next + 1, 300);
        vm.expectRevert(ISTSettlement2.InvalidEpoch.selector);
        phaseC.assignSurplus(NO1, next - 1, 300);
        vm.expectRevert(ISTSettlement2.InvalidEpoch.selector);
        phaseC.assignSurplus(NO1, 0, 300);
        vm.stopPrank();

        _accrueV2(V2_POOL1, 1_000);
        _closeV2(FIRST_EPOCH, NO1);
        vm.prank(owner);
        vm.expectRevert(ISTSettlement2.InvalidEpoch.selector);
        phaseC.assignSurplus(NO1, next, 300);
        assertEq(vault2.fundedThrough(NO1, FIRST_EPOCH), 1_000);
        assertEq(_surplus(), 300);
    }

    // SC-5: the same surplus is assigned once; a second assignment of it is
    // refused by vault2, and so is any part of it already assigned.
    function test_SC5_assignSurplusTwiceIsRefused() public {
        _absorb(300);
        uint256 next = _next();
        vm.startPrank(owner);
        phaseC.assignSurplus(NO1, next, 300);
        vm.expectRevert(ISTSettlementVault2.InvalidSurplus.selector);
        phaseC.assignSurplus(NO1, next, 300);
        vm.expectRevert(ISTSettlementVault2.InvalidSurplus.selector);
        phaseC.assignSurplus(NO1, next, 1);
        vm.stopPrank();
        assertEq(vault2.capturedTotal(NO1), 300);
        assertTrue(vault2.conservationHolds());
    }

    // SC-6: nothing is assigned without absorbed surplus: before absorbStake,
    // zero, or one rao more than the surplus.
    function test_SC6_assignSurplusRefusesWithoutAbsorbedSurplus() public {
        // vault1 pays vault2's coldkey on the v1 escrow hotkey, not yet absorbed.
        staking.setStake(ESCROW_HOTKEY, V2_SELF, 300);
        uint256 next = _next();
        vm.startPrank(owner);
        vm.expectRevert(ISTSettlementVault2.InvalidSurplus.selector);
        phaseC.assignSurplus(NO1, next, 300);
        vm.stopPrank();

        vm.prank(stranger);
        vault2.absorbStake(ESCROW_HOTKEY);
        vm.startPrank(owner);
        vm.expectRevert(ISTSettlementVault2.InvalidSurplus.selector);
        phaseC.assignSurplus(NO1, next, 0);
        vm.expectRevert(ISTSettlementVault2.InvalidSurplus.selector);
        phaseC.assignSurplus(NO1, next, 301);
        phaseC.assignSurplus(NO1, next, 300);
        vm.stopPrank();
        assertEq(vault2.capturedTotal(NO1), 300);
    }

    // ------------------------------------------------------------------
    // SC-7 to SC-11: recordV1Sweep
    // ------------------------------------------------------------------

    // SC-7: the owner records the committed sweep root of a v2 epoch, with its
    // artifact hash; readers find it beside settlementAt's v2 identity.
    function test_SC7_ownerRecordsTheCommittedSweepRoot() public {
        uint256 sweep = FIRST_EPOCH + 1;
        bytes32 root = _sweepRoot();
        _assertSweep(NO1, 0, bytes32(0), bytes32(0));
        _commitV1(sweep, root, ARTIFACT);

        vm.record();
        vm.expectEmit(address(settlement));
        emit STSettlement2PhaseC.V1SweepRecorded(NO1, sweep, root, ARTIFACT);
        vm.prank(owner);
        phaseC.recordV1Sweep(NO1, sweep, ARTIFACT);
        (, bytes32[] memory writes) = vm.accesses(address(settlement));

        _assertSweep(NO1, uint64(sweep), _sweepRoot(), ARTIFACT);
        _assertSweep(NO2, 0, bytes32(0), bytes32(0));
        (address vault_, bytes32 hotkey, uint8 version) = phaseC.settlementAt(NO1, sweep);
        assertEq(vault_, address(vault2));
        assertEq(hotkey, V2_POOL1);
        assertEq(version, 2);

        // The record is written only after STSettlement2's gap: the three words
        // of _v1Sweeps[NO1], slot 50.
        bytes32 base = keccak256(abi.encode(NO1, SWEEPS_SLOT));
        assertEq(writes.length, 3);
        for (uint256 i; i < writes.length; i++) {
            uint256 offset = uint256(writes[i]) - uint256(base);
            assertLt(offset, 3);
        }
        assertEq(uint256(vm.load(address(settlement), base)), sweep);
        assertEq(vm.load(address(settlement), bytes32(uint256(base) + 1)), _sweepRoot());
        assertEq(vm.load(address(settlement), bytes32(uint256(base) + 2)), ARTIFACT);
    }

    // SC-8: only the owner records.
    function test_SC8_recordV1SweepIsOwnerOnly() public {
        _commitV1(FIRST_EPOCH, _sweepRoot(), ARTIFACT);
        vm.prank(stranger);
        vm.expectRevert(
            abi.encodeWithSelector(OwnableUpgradeable.OwnableUnauthorizedAccount.selector, stranger)
        );
        phaseC.recordV1Sweep(NO1, FIRST_EPOCH, ARTIFACT);
        vm.prank(rootSigner1);
        vm.expectRevert(
            abi.encodeWithSelector(OwnableUpgradeable.OwnableUnauthorizedAccount.selector, rootSigner1)
        );
        phaseC.recordV1Sweep(NO1, FIRST_EPOCH, ARTIFACT);
        _assertSweep(NO1, 0, bytes32(0), bytes32(0));
    }

    // SC-9: the pool must be registered and the epoch a v2 epoch of it; an
    // epoch before firstEpoch has an ordinary v1 root and needs no record.
    function test_SC9_recordV1SweepRefusesAnUnknownPoolOrAV1Epoch() public {
        bytes32 root = _sweepRoot();
        vm.roll(_end(1));
        vm.prank(rootSigner1);
        coordinator.commitOperatorRoot(1, NO1, root, ARTIFACT);
        vm.prank(rootSigner2);
        coordinator.commitOperatorRoot(1, NO2, root, ARTIFACT);

        vm.startPrank(owner);
        vm.expectRevert(ISTSettlement2.UnknownPool.selector);
        phaseC.recordV1Sweep(NO2, 1, ARTIFACT);
        vm.expectRevert(ISTSettlement2.InvalidEpoch.selector);
        phaseC.recordV1Sweep(NO1, 1, ARTIFACT);
        vm.expectRevert(ISTSettlement2.InvalidEpoch.selector);
        phaseC.recordV1Sweep(NO1, 0, ARTIFACT);
        vm.stopPrank();
        _assertSweep(NO1, 0, bytes32(0), bytes32(0));
    }

    // SC-10: the v1 commitment must be exactly the sweep root with the given
    // nonzero artifact hash: none, another root, another coldkey or share, or
    // another artifact hash is refused.
    function test_SC10_recordV1SweepRefusesAnythingButTheSweepCommitment() public {
        uint256 sweep = FIRST_EPOCH;
        vm.startPrank(owner);
        vm.expectRevert(STSettlement2PhaseC.NotV1SweepRoot.selector);
        phaseC.recordV1Sweep(NO1, sweep, ARTIFACT);
        vm.stopPrank();

        bytes32[4] memory wrongRoots = [
            vault.payoutLeaf(V2_SELF, 9_999),
            vault.payoutLeaf(ALICE, 10_000),
            vault.payoutLeaf(V2_ESCROW, 10_000),
            keccak256("a two-leaf root")
        ];
        for (uint256 i; i < wrongRoots.length; i++) {
            _commitV1(sweep + i, wrongRoots[i], ARTIFACT);
            vm.prank(owner);
            vm.expectRevert(STSettlement2PhaseC.NotV1SweepRoot.selector);
            phaseC.recordV1Sweep(NO1, sweep + i, ARTIFACT);
        }

        uint256 right = sweep + wrongRoots.length;
        _commitV1(right, _sweepRoot(), ARTIFACT);
        vm.startPrank(owner);
        vm.expectRevert(STSettlement2PhaseC.NotV1SweepRoot.selector);
        phaseC.recordV1Sweep(NO1, right, OTHER_ARTIFACT);
        vm.expectRevert(STSettlement2PhaseC.NotV1SweepRoot.selector);
        phaseC.recordV1Sweep(NO1, right, bytes32(0));
        vm.expectRevert(STSettlement2PhaseC.NotV1SweepRoot.selector);
        phaseC.recordV1Sweep(NO1, right + 1, ARTIFACT);
        phaseC.recordV1Sweep(NO1, right, ARTIFACT);
        vm.stopPrank();
        _assertSweep(NO1, uint64(right), _sweepRoot(), ARTIFACT);
    }

    // SC-11: a pool records one sweep, ever: the same epoch again and any later
    // sweep-shaped root are refused.
    function test_SC11_recordV1SweepRefusesASecondRecord() public {
        _commitV1(FIRST_EPOCH, _sweepRoot(), ARTIFACT);
        _commitV1(FIRST_EPOCH + 1, _sweepRoot(), OTHER_ARTIFACT);
        vm.startPrank(owner);
        phaseC.recordV1Sweep(NO1, FIRST_EPOCH, ARTIFACT);
        vm.expectRevert(STSettlement2PhaseC.V1SweepAlreadyRecorded.selector);
        phaseC.recordV1Sweep(NO1, FIRST_EPOCH, ARTIFACT);
        vm.expectRevert(STSettlement2PhaseC.V1SweepAlreadyRecorded.selector);
        phaseC.recordV1Sweep(NO1, FIRST_EPOCH + 1, OTHER_ARTIFACT);
        vm.stopPrank();
        _assertSweep(NO1, FIRST_EPOCH, _sweepRoot(), ARTIFACT);
    }

    // ------------------------------------------------------------------
    // SC-12 to SC-15: the upgrade
    // ------------------------------------------------------------------

    // SC-12, S-9: an owner upgrade from the phase A implementation keeps every
    // stored value, approvals of a pending root included, and every phase A
    // call keeps working through the new implementation.
    function test_SC12_upgradeFromPhaseAKeepsEveryStoredValueAndCall() public {
        // Back to phase A, with a pending root half approved under version 2.
        STSettlement2 phaseA = STSettlement2(payable(address(settlement)));
        vm.prank(owner);
        phaseA.upgradeToAndCall(phaseAImplementation, "");
        address auditor2 = makeAddr("auditor-2");
        address[] memory list = new address[](2);
        (list[0], list[1]) = (auditor1, auditor2);
        vm.prank(owner);
        phaseA.setAuditors(list, 2);
        V2TreeHelper.Node[][] memory levels = V2TreeHelper.build(_keys(ALICE, BOB), _values(600, 400));
        _accrueV2(V2_POOL1, 1_000);
        _closeV2(FIRST_EPOCH, NO1);
        uint64 seq = _commitV2(FIRST_EPOCH, NO1, rootSigner1, levels, 2);
        _approveV2(NO1, auditor1);
        bytes32[] memory before = new bytes32[](SWEEPS_SLOT + 1);
        for (uint256 i; i <= SWEEPS_SLOT; i++) {
            before[i] = vm.load(address(settlement), bytes32(i));
        }

        STSettlement2PhaseC next = new STSettlement2PhaseC();
        vm.expectEmit(true, true, true, true, address(settlement));
        emit IERC1967.Upgraded(address(next));
        vm.prank(owner);
        phaseA.upgradeToAndCall(address(next), "");

        assertEq(
            address(uint160(uint256(vm.load(address(settlement), ERC1967Utils.IMPLEMENTATION_SLOT)))),
            address(next)
        );
        for (uint256 i; i <= SWEEPS_SLOT; i++) {
            assertEq(vm.load(address(settlement), bytes32(i)), before[i]);
        }
        assertEq(phaseC.owner(), owner);
        assertEq(phaseC.v1Coordinator(), address(coordinator));
        assertEq(phaseC.vault(), address(vault2));
        assertEq(phaseC.auditors(), list);
        assertEq(phaseC.quorum(), 2);
        assertEq(phaseC.auditorSetVersion(), 2);
        assertEq(phaseC.approvalCount(NO1), 1);
        assertTrue(phaseC.hasApproved(NO1, auditor1));
        assertEq(vault2.coordinator(), address(settlement));

        _approveV2(NO1, auditor2);
        assertEq(phaseC.approvalCount(NO1), 2);
        assertEq(_finalizeV2(NO1), 0);
        assertEq(vault2.finalizedTotal(NO1), 1_000);
        assertEq(vault2.roots(NO1, 0).epoch, FIRST_EPOCH);
        assertEq(vault2.commitSeq(NO1), seq);
        _accrueV2(V2_POOL1, 70);
        assertEq(_closeV2(FIRST_EPOCH + 1, NO1), 70);
        assertEq(vault2.fundedThrough(NO1, FIRST_EPOCH + 1), 1_070);
    }

    // SC-13, S-9: only the owner upgrades to the phase C implementation, and it
    // stays upgradeable by the owner alone.
    function test_SC13_upgradeIsOwnerOnly() public {
        STSettlement2PhaseC next = new STSettlement2PhaseC();
        vm.prank(stranger);
        vm.expectRevert(
            abi.encodeWithSelector(OwnableUpgradeable.OwnableUnauthorizedAccount.selector, stranger)
        );
        phaseC.upgradeToAndCall(address(next), "");

        vm.prank(owner);
        phaseC.upgradeToAndCall(address(next), "");
        assertEq(
            address(uint160(uint256(vm.load(address(settlement), ERC1967Utils.IMPLEMENTATION_SLOT)))),
            address(next)
        );
    }

    // SC-14, S-1: the implementation's initializers stay disabled, and the
    // upgraded proxy cannot be initialized again.
    function test_SC14_initializersStayDisabled() public {
        vm.expectEmit(
            true, true, true, true, vm.computeCreateAddress(address(this), vm.getNonce(address(this)))
        );
        emit Initializable.Initialized(type(uint64).max);
        STSettlement2PhaseC implementation_ = new STSettlement2PhaseC();
        address[] memory list = new address[](1);
        list[0] = auditor1;
        vm.expectRevert(Initializable.InvalidInitialization.selector);
        implementation_.initialize(owner, address(coordinator), address(vault2), list, 1);
        vm.expectRevert(Initializable.InvalidInitialization.selector);
        phaseC.initialize(stranger, address(coordinator), address(vault2), list, 1);
        assertEq(phaseC.owner(), owner);
    }

    // SC-15, S-SIZE: within EIP-170 and EIP-3860.
    function test_SC15_implementationFitsTheCodeSizeLimits() public {
        assertLt(address(new STSettlement2PhaseC()).code.length, 24_576);
        assertLt(type(STSettlement2PhaseC).creationCode.length, 49_152);
    }

    // ------------------------------------------------------------------
    // SC-16: the sweep, end to end
    // ------------------------------------------------------------------

    // SC-16, §16 phase C: the v1 sweep epoch is the first v2 epoch. Its v1 close
    // captures the reveal-lag stake, the single-leaf root folds the carry into
    // it at finalization, its claim pays vault2's coldkey on the v1 escrow
    // hotkey, absorbStake turns that into vault2 surplus, and the owner records
    // the sweep and assigns the surplus to the next v2 epoch, whose close
    // snapshots it. Conservation holds in both vaults.
    function test_SC16_theSweepFromTheV1CloseToTheNextV2Snapshot() public {
        // v1 epoch 1 misses its root: its funding becomes carry.
        _accrue(NO1, 5_000);
        vm.roll(_end(1));
        coordinator.closeOperatorEpoch(1, NO1);
        vm.roll(_end(1) + 14_400);
        coordinator.finalizeOperatorEpoch(1, NO1);
        assertEq(vault.carry(NO1), 5_000);

        // Epoch 2 is v2; reveal-lag emission still reaches the v1 pool hotkey.
        _accrueV2(V2_POOL1, 1_000);
        _accrue(NO1, 70);
        uint256 sweep = FIRST_EPOCH;
        _closeV2(sweep, NO1);
        vm.roll(_end(sweep));
        assertEq(coordinator.closeOperatorEpoch(sweep, NO1), 70);
        _commitV1(sweep, _sweepRoot(), ARTIFACT);
        vm.roll(_end(sweep) + 14_400);
        coordinator.finalizeOperatorEpoch(sweep, NO1);
        STSettlementVault.Entitlement memory entitlement = vault.entitlement(sweep, NO1);
        assertEq(entitlement.total, 5_070);
        assertEq(vault.carry(NO1), 0);

        vm.prank(stranger);
        vault.claim(sweep, NO1, V2_SELF, 10_000, new bytes32[](0));
        assertEq(staking.stakes(ESCROW_HOTKEY, V2_SELF), 5_070);
        vm.prank(stranger);
        assertEq(vault2.absorbStake(ESCROW_HOTKEY), 5_070);
        assertEq(_surplus(), 5_070);

        vm.startPrank(owner);
        phaseC.recordV1Sweep(NO1, sweep, ARTIFACT);
        uint256 next = _next();
        assertEq(next, sweep + 1);
        vm.expectEmit(address(vault2));
        emit ISTSettlementVault2.SurplusAssigned(NO1, next, 5_070);
        phaseC.assignSurplus(NO1, next, 5_070);
        vm.stopPrank();
        _assertSweep(NO1, uint64(sweep), _sweepRoot(), ARTIFACT);

        _accrueV2(V2_POOL1, 400);
        _closeV2(next, NO1);
        assertEq(vault2.fundedThrough(NO1, sweep), 1_000);
        assertEq(vault2.fundedThrough(NO1, next), 6_470);
        assertTrue(vault.conservationHolds());
        assertTrue(vault2.conservationHolds());
        assertEq(_surplus(), 0);
    }
}
