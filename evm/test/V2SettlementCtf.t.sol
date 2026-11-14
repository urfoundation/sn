// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {Initializable} from "@openzeppelin/contracts-upgradeable/proxy/utils/Initializable.sol";
import {OwnableUpgradeable} from "@openzeppelin/contracts-upgradeable/access/OwnableUpgradeable.sol";
import {IERC1967} from "@openzeppelin/contracts/interfaces/IERC1967.sol";
import {ERC1967Utils} from "@openzeppelin/contracts/proxy/ERC1967/ERC1967Utils.sol";

import {V2CtfBase} from "./utils/V2CtfBase.sol";
import {V2TreeHelper} from "./utils/V2TreeHelper.sol";
import {STCoordinator} from "../src/STCoordinator.sol";
import {STSettlement2PhaseC} from "../src/STSettlement2PhaseC.sol";
import {STSettlement2Ctf} from "../src/STSettlement2Ctf.sol";
import {ISTSettlement2} from "../src/interfaces/ISTSettlement2.sol";
import {ISTSettlementVault2} from "../src/interfaces/ISTSettlementVault2.sol";

/// @dev Rejects every native transfer, so a refund to it fails.
contract CtfRefundRejector {}

/// @dev STSettlement2Ctf, the capture the flag upgrade (CAPTURETHEFLAG.md
///      §3.1), on the whole stack. CS-1 and CS-2 cover the upgrade from
///      phase C and the layout, CS-3 to CS-7 registerCtfPool and the derived
///      hotkey, CS-8 registerOperatorPool's new refusal, CS-9 and CS-10 the CTF
///      pool's roots, CS-11 to CS-15 the census commitment, and CS-16 the views.
///      V2SettlementCtfTimelock.t.sol runs the registration as the call of
///      upgradeToAndCall through the timelock, and V2SettlementCtfRegression.t.sol
///      reruns the phase A and phase C suites against this implementation.
contract V2SettlementCtfTest is V2CtfBase {
    // keccak256 of a literal is a constant. sha256 would be a precompile call,
    // which takes a pending prank or expectRevert.
    bytes32 internal constant CENSUS = keccak256("ctf census header");
    bytes32 internal constant OTHER_CENSUS = keccak256("another census header");
    bytes32 internal constant ARTIFACT = keccak256("v1 sweep record");
    // STSettlement2's slots 0 to 49, phase C's mapping at 50, the census at 51.
    uint256 internal constant SWEEPS_SLOT = 50;
    uint256 internal constant CENSUS_SLOT = 51;

    // ------------------------------------------------------------------
    // CS-1 and CS-2: the upgrade
    // ------------------------------------------------------------------

    // CS-1, S-9: the upgrade from phase C, the implementation mainnet runs,
    // keeps every stored value: the auditor set, a pending root's approvals,
    // the v1 sweep record and the owner. Every phase A and phase C call works
    // through the new implementation, and the census lands at slot 51.
    function test_CS1_upgradeFromPhaseCKeepsEveryStoredValueAndCall() public {
        STSettlement2PhaseC phaseC = STSettlement2PhaseC(payable(address(settlement)));
        vm.prank(owner);
        ctf.upgradeToAndCall(phaseCImplementation, "");
        address auditor2 = makeAddr("auditor-2");
        address[] memory list = new address[](2);
        (list[0], list[1]) = (auditor1, auditor2);
        vm.prank(owner);
        phaseC.setAuditors(list, 2);
        // The sweep record, as phase C keeps it.
        bytes32 sweepLeaf = vault.payoutLeaf(V2_SELF, 10_000);
        vm.roll(_end(FIRST_EPOCH));
        vm.prank(rootSigner1);
        coordinator.commitOperatorRoot(FIRST_EPOCH, NO1, sweepLeaf, ARTIFACT);
        vm.prank(owner);
        phaseC.recordV1Sweep(NO1, FIRST_EPOCH, ARTIFACT);
        // A pending root, half approved under version 2.
        V2TreeHelper.Node[][] memory levels = V2TreeHelper.build(_keys(ALICE, BOB), _values(600, 400));
        _accrueV2(V2_POOL1, 1_000);
        _closeV2(FIRST_EPOCH, NO1);
        uint64 seq = _commitV2(FIRST_EPOCH, NO1, rootSigner1, levels, 2);
        _approveV2(NO1, auditor1);
        bytes32[] memory before = new bytes32[](CENSUS_SLOT + 1);
        for (uint256 i; i <= CENSUS_SLOT; i++) {
            before[i] = vm.load(address(settlement), bytes32(i));
        }

        STSettlement2Ctf next = new STSettlement2Ctf();
        vm.expectEmit(true, true, true, true, address(settlement));
        emit IERC1967.Upgraded(address(next));
        vm.prank(owner);
        phaseC.upgradeToAndCall(address(next), "");

        assertEq(_implementation(), address(next));
        for (uint256 i; i <= CENSUS_SLOT; i++) {
            assertEq(vm.load(address(settlement), bytes32(i)), before[i]);
        }
        assertEq(ctf.owner(), owner);
        assertEq(ctf.v1Coordinator(), address(coordinator));
        assertEq(ctf.vault(), address(vault2));
        assertEq(ctf.auditors(), list);
        assertEq(ctf.quorum(), 2);
        assertEq(ctf.auditorSetVersion(), 2);
        assertEq(ctf.approvalCount(NO1), 1);
        assertTrue(ctf.hasApproved(NO1, auditor1));
        (uint64 sweepEpoch, bytes32 sweepRoot, bytes32 sweepArtifact) = ctf.v1Sweep(NO1);
        assertEq(sweepEpoch, FIRST_EPOCH);
        assertEq(sweepRoot, sweepLeaf);
        assertEq(sweepArtifact, ARTIFACT);
        (address v, bytes32 hotkey, uint8 version) = ctf.settlementAt(NO1, FIRST_EPOCH);
        assertEq(v, address(vault2));
        assertEq(hotkey, V2_POOL1);
        assertEq(version, 2);

        // The pending root finalizes through the new implementation.
        _approveV2(NO1, auditor2);
        assertEq(ctf.approvalCount(NO1), 2);
        assertEq(_finalizeV2(NO1), 0);
        assertEq(vault2.finalizedTotal(NO1), 1_000);
        assertEq(vault2.commitSeq(NO1), seq);
        _accrueV2(V2_POOL1, 70);
        assertEq(_closeV2(FIRST_EPOCH + 1, NO1), 70);

        // The census mapping is slot 51: noId, then epoch. Every slot's own
        // word, the mappings' included, is unchanged.
        uint64 first = uint64(coordinator.currentEpoch() + 1);
        _registerCtf(NO1, first, CTF1_UID);
        _commitCensus(first, CENSUS, _end(first));
        bytes32 entry = keccak256(abi.encode(uint256(first), keccak256(abi.encode(NO1, CENSUS_SLOT))));
        assertEq(vm.load(address(settlement), entry), CENSUS);
        assertEq(uint256(vm.load(address(settlement), bytes32(uint256(entry) + 1))), _end(first));
        for (uint256 i; i <= CENSUS_SLOT; i++) {
            assertEq(vm.load(address(settlement), bytes32(i)), before[i]);
        }
    }

    // CS-2, S-1, S-9, S-SIZE: the implementation's initializers are disabled
    // and the proxy cannot be initialized again; only the owner upgrades, and
    // only to a UUPS implementation; it fits EIP-170 and EIP-3860.
    function test_CS2_implementationInitializersUpgradesAndSize() public {
        vm.expectEmit(
            true, true, true, true, vm.computeCreateAddress(address(this), vm.getNonce(address(this)))
        );
        emit Initializable.Initialized(type(uint64).max);
        STSettlement2Ctf implementation_ = new STSettlement2Ctf();
        address[] memory list = new address[](1);
        list[0] = auditor1;
        vm.expectRevert(Initializable.InvalidInitialization.selector);
        implementation_.initialize(owner, address(coordinator), address(vault2), list, 1);
        vm.expectRevert(Initializable.InvalidInitialization.selector);
        ctf.initialize(stranger, address(coordinator), address(vault2), list, 1);

        vm.prank(stranger);
        vm.expectRevert(
            abi.encodeWithSelector(OwnableUpgradeable.OwnableUnauthorizedAccount.selector, stranger)
        );
        ctf.upgradeToAndCall(address(implementation_), "");
        vm.prank(owner);
        vm.expectRevert(
            abi.encodeWithSelector(ERC1967Utils.ERC1967InvalidImplementation.selector, address(vault2))
        );
        ctf.upgradeToAndCall(address(vault2), "");
        assertEq(_implementation(), ctfImplementation);
        assertEq(ctf.owner(), owner);

        uint256 runtime = address(implementation_).code.length;
        uint256 creation = type(STSettlement2Ctf).creationCode.length;
        assertLt(runtime, 24_576);
        assertLt(creation, 49_152);
        emit log_named_uint("STSettlement2Ctf runtime bytes", runtime);
        emit log_named_uint("STSettlement2Ctf runtime margin", 24_576 - runtime);
        emit log_named_uint("STSettlement2Ctf creation bytes", creation);
    }

    // ------------------------------------------------------------------
    // CS-3 to CS-7: registerCtfPool
    // ------------------------------------------------------------------

    // CS-3: the owner registers NO1's CTF pool as vault2 pool 2^32 + 1 under
    // the hotkey derived from the proxy, the pool id, the block and its
    // parent's hash. vault2 burns from the call value and its refund of the
    // surplus is passed back exactly; vault2's PoolRegistered names the pool.
    function test_CS3_ownerRegistersTheCtfPoolUnderAHotkeyDerivedAtExecution() public {
        assertEq(CTF1, 4_294_967_297);
        vm.setBlockhash(block.number - 1, keccak256("parent block"));
        bytes32 hotkey = _ctfHotkey(NO1);
        assertEq(
            hotkey, keccak256(abi.encode(address(settlement), CTF1, block.number, keccak256("parent block")))
        );
        neuron.setUid(NETUID, hotkey, CTF1_UID);
        neuron.setRegistrationBurn(BURN_WEI / 4);
        vm.deal(owner, BURN_WEI);

        vm.expectEmit(true, true, true, true, address(vault2));
        emit ISTSettlementVault2.PoolRegistered(CTF1, hotkey, CTF1_UID, CTF_FIRST_EPOCH);
        vm.prank(owner);
        uint16 uid = ctf.registerCtfPool{value: BURN_WEI}(NO1, V2_BURN_LIMIT, CTF_FIRST_EPOCH);
        uint256 gas = vm.lastCallGas().gasTotalUsed;

        assertEq(uid, CTF1_UID);
        assertEq(owner.balance, BURN_WEI - BURN_WEI / 4, "the surplus comes back");
        assertEq(address(settlement).balance, 0);
        assertEq(address(vault2).balance, 0);
        assertEq(neuron.lastRegistrant(), address(vault2));
        assertEq(neuron.lastHotkey(), hotkey);
        assertEq(neuron.lastLimitPrice(), V2_BURN_LIMIT);
        assertEq(neuron.lastRegistrationCallValue(), 0, "the burn comes from vault2's balance");
        ISTSettlementVault2.Pool memory pool = vault2.pools(CTF1);
        assertTrue(pool.registered);
        assertEq(pool.hotkey, hotkey);
        assertEq(pool.firstEpoch, CTF_FIRST_EPOCH);
        assertEq(vault2.lastFundedEpoch(CTF1), CTF_FIRST_EPOCH - 1);
        assertTrue(vault2.poolHotkeyUsed(hotkey));
        // The provider pool is untouched.
        assertEq(vault2.pools(NO1).hotkey, V2_POOL1);
        emit log_named_uint("registerCtfPool gas", gas);
    }

    // CS-4, S-3: registerCtfPool is the owner's, and makes registerOperatorPool's
    // checks: the v1 operator active now, a future first epoch and the
    // cadence; it also needs the operator's provider pool and an id below
    // 2^32.
    function test_CS4_registerCtfPoolRefusals() public {
        vm.prank(stranger);
        vm.expectRevert(
            abi.encodeWithSelector(OwnableUpgradeable.OwnableUnauthorizedAccount.selector, stranger)
        );
        ctf.registerCtfPool(NO1, V2_BURN_LIMIT, CTF_FIRST_EPOCH);
        vm.prank(rootSigner1);
        vm.expectRevert(
            abi.encodeWithSelector(OwnableUpgradeable.OwnableUnauthorizedAccount.selector, rootSigner1)
        );
        ctf.registerCtfPool(NO1, V2_BURN_LIMIT, CTF_FIRST_EPOCH);

        // NO1 is active now and inactive from epoch CTF_FIRST_EPOCH + 1.
        vm.prank(owner);
        coordinator.scheduleOperator(NO1, DEPOSIT1, depositSigner1, rootSigner1, false, CTF_FIRST_EPOCH + 1);

        vm.startPrank(owner);
        vm.expectRevert(STSettlement2Ctf.NoIdOutOfRange.selector);
        ctf.registerCtfPool(CTF_BASE, V2_BURN_LIMIT, CTF_FIRST_EPOCH);
        vm.expectRevert(STSettlement2Ctf.NoIdOutOfRange.selector);
        ctf.registerCtfPool(type(uint256).max, V2_BURN_LIMIT, CTF_FIRST_EPOCH);
        // No v1 operator 3: the v1 coordinator's refusal passes through.
        vm.expectRevert(STCoordinator.UnknownOperator.selector);
        ctf.registerCtfPool(3, V2_BURN_LIMIT, CTF_FIRST_EPOCH);
        // The first epoch must be after the current one, epoch 0.
        vm.expectRevert(ISTSettlement2.InvalidEpoch.selector);
        ctf.registerCtfPool(NO1, V2_BURN_LIMIT, 0);
        // NO2 is an active v1 operator without a provider pool in vault2.
        vm.expectRevert(ISTSettlement2.UnknownPool.selector);
        ctf.registerCtfPool(NO2, V2_BURN_LIMIT, CTF_FIRST_EPOCH);
        // vault2 refuses a zero burn cap.
        bytes32 hotkey = _ctfHotkey(NO1);
        neuron.setUid(NETUID, hotkey, CTF1_UID);
        vm.expectRevert(ISTSettlementVault2.InvalidConfiguration.selector);
        ctf.registerCtfPool(NO1, 0, CTF_FIRST_EPOCH);
        vm.stopPrank();

        // A first epoch that has started.
        vm.roll(coordinator.epochStartBlock(CTF_FIRST_EPOCH));
        vm.prank(owner);
        vm.expectRevert(ISTSettlement2.InvalidEpoch.selector);
        ctf.registerCtfPool(NO1, V2_BURN_LIMIT, CTF_FIRST_EPOCH);
        vm.roll(coordinator.epochStartBlock(CTF_FIRST_EPOCH) - 1);
        vm.prank(owner);
        vm.expectRevert(ISTSettlement2.InvalidEpoch.selector);
        ctf.registerCtfPool(NO1, V2_BURN_LIMIT, CTF_FIRST_EPOCH - 1);

        // An operator inactive now.
        vm.roll(coordinator.epochStartBlock(CTF_FIRST_EPOCH + 1));
        vm.prank(owner);
        vm.expectRevert(ISTSettlement2.InactiveOperator.selector);
        ctf.registerCtfPool(NO1, V2_BURN_LIMIT, CTF_FIRST_EPOCH + 2);
        assertFalse(vault2.pools(CTF1).registered);
    }

    // CS-4, S-3: the first epoch's policy must leave a root committed at the
    // end of its window time to clear vault2's challenge window before the
    // next epoch ends: 1,200 + 14,400 blocks at least.
    function test_CS4_registerCtfPoolRefusesAShortCadence() public {
        _schedulePolicy(5, V2_ROOT_WINDOW + WINDOW - 1);
        vm.prank(owner);
        vm.expectRevert(ISTSettlement2.CadenceTooShort.selector);
        ctf.registerCtfPool(NO1, V2_BURN_LIMIT, 5);
        _schedulePolicy(6, V2_ROOT_WINDOW + WINDOW);
        _registerCtf(NO1, 6, CTF1_UID);
        assertEq(vault2.pools(CTF1).firstEpoch, 6);
    }

    // CS-5, S-3: a registered CTF pool is refused by vault2, whatever hotkey a
    // later block derives; the runtime's refusal of the registration and a
    // burn above the value revert the whole call; a refund the caller cannot
    // take reverts it; the proxy takes native value only from vault2.
    function test_CS5_registerCtfPoolRefusesARegisteredPoolAndFailedPayments() public {
        bytes32 first = _registerCtf(NO1, CTF_FIRST_EPOCH, CTF1_UID);
        vm.roll(block.number + 1);
        bytes32 second = _ctfHotkey(NO1);
        assertTrue(second != first);
        neuron.setUid(NETUID, second, CTF2_UID);
        vm.prank(owner);
        vm.expectRevert(ISTSettlementVault2.InvalidConfiguration.selector);
        ctf.registerCtfPool(NO1, V2_BURN_LIMIT, CTF_FIRST_EPOCH + 1);
        assertEq(vault2.pools(CTF1).hotkey, first);

        _registerProviderPool2();
        neuron.setUid(NETUID, _ctfHotkey(NO2), CTF2_UID);
        neuron.setFailRegistration(true);
        vm.deal(owner, BURN_WEI);
        vm.prank(owner);
        vm.expectRevert("mock: registration refused");
        ctf.registerCtfPool{value: BURN_WEI}(NO2, V2_BURN_LIMIT, CTF_FIRST_EPOCH);
        neuron.setFailRegistration(false);
        neuron.setRegistrationBurn(BURN_WEI + 1);
        vm.prank(owner);
        vm.expectRevert("mock: burn exceeds balance");
        ctf.registerCtfPool{value: BURN_WEI}(NO2, V2_BURN_LIMIT, CTF_FIRST_EPOCH);
        assertEq(owner.balance, BURN_WEI);

        // vault2 refunds the whole value of a free registration, which the
        // caller cannot take. (The neuron mock's burn is a balance write that
        // a revert does not undo, so this refusal burns nothing.)
        neuron.setRegistrationBurn(0);
        CtfRefundRejector rejector = new CtfRefundRejector();
        vm.prank(owner);
        ctf.transferOwnership(address(rejector));
        vm.deal(address(rejector), BURN_WEI);
        vm.prank(address(rejector));
        vm.expectRevert(ISTSettlement2.NativeRefundFailed.selector);
        ctf.registerCtfPool{value: BURN_WEI}(NO2, V2_BURN_LIMIT, CTF_FIRST_EPOCH);
        // With the whole value burned there is nothing to refund.
        neuron.setRegistrationBurn(BURN_WEI);
        vm.prank(address(rejector));
        ctf.registerCtfPool{value: BURN_WEI}(NO2, V2_BURN_LIMIT, CTF_FIRST_EPOCH);
        assertTrue(vault2.pools(CTF2).registered);

        vm.deal(stranger, 1 ether);
        vm.prank(stranger);
        (bool sent,) = address(settlement).call{value: 1 ether}("");
        assertFalse(sent, "the proxy takes native value only from vault2");
    }

    // CS-6: the derived hotkey is unique per operator in a block, changes with
    // the block and its parent's hash, and a registration the runtime refuses
    // in one block succeeds in a later one under another hotkey.
    function test_CS6_theDerivedHotkeyIsUniqueAndARefusedRegistrationRetriesInALaterBlock() public {
        _registerProviderPool2();
        assertTrue(_ctfHotkey(NO1) != _ctfHotkey(NO2));
        bytes32 atParent = _ctfHotkey(NO1);
        vm.setBlockhash(block.number - 1, keccak256("another parent"));
        assertTrue(_ctfHotkey(NO1) != atParent);

        // The runtime refuses this block's hotkey, as it does one that another
        // coldkey registered first: nothing is registered.
        bytes32 refused = _ctfHotkey(NO1);
        neuron.setUid(NETUID, refused, CTF1_UID);
        neuron.setFailRegistration(true);
        vm.prank(owner);
        vm.expectRevert("mock: registration refused");
        ctf.registerCtfPool(NO1, V2_BURN_LIMIT, CTF_FIRST_EPOCH);
        assertFalse(vault2.pools(CTF1).registered);
        assertFalse(vault2.poolHotkeyUsed(refused));
        neuron.setFailRegistration(false);

        vm.roll(block.number + 1);
        bytes32 retried = _registerCtf(NO1, CTF_FIRST_EPOCH, CTF1_UID);
        assertTrue(retried != refused);
        assertEq(vault2.pools(CTF1).hotkey, retried);
        bytes32 other = _registerCtf(NO2, CTF_FIRST_EPOCH, CTF2_UID);
        assertTrue(other != retried);
        assertEq(vault2.pools(CTF2).hotkey, other);
        assertEq(ctf.ctfPoolId(NO2), CTF2);
    }

    // CS-6: without the parent block's hash the hotkey would be known before
    // its block, so a registration whose BLOCKHASH answers zero fails closed;
    // once it answers, the same call registers.
    function test_CS6_aZeroParentHashIsRefused() public {
        vm.setBlockhash(block.number - 1, bytes32(0));
        neuron.setUid(NETUID, _ctfHotkey(NO1), CTF1_UID);
        vm.prank(owner);
        vm.expectRevert(STSettlement2Ctf.BlockHashUnavailable.selector);
        ctf.registerCtfPool(NO1, V2_BURN_LIMIT, CTF_FIRST_EPOCH);
        assertFalse(vault2.pools(CTF1).registered);

        vm.setBlockhash(block.number - 1, keccak256("a parent"));
        bytes32 hotkey = _registerCtf(NO1, CTF_FIRST_EPOCH, CTF1_UID);
        assertEq(
            hotkey, keccak256(abi.encode(address(settlement), CTF1, block.number, keccak256("a parent")))
        );
        assertEq(vault2.pools(CTF1).hotkey, hotkey);
    }

    // CS-7: a CTF pool and its provider pool are separate vault2 pools: each
    // keeps its own hotkey, epochs and funding.
    function test_CS7_theCtfPoolIsASeparateVault2Pool() public {
        bytes32 hotkey = _registerCtf(NO1, CTF_FIRST_EPOCH, CTF1_UID);
        _accrueV2(V2_POOL1, 1_000);
        _accrueV2(hotkey, 300);
        assertEq(_closeV2(FIRST_EPOCH, NO1), 1_000);
        vm.prank(stranger);
        vm.expectRevert(ISTSettlement2.InvalidEpoch.selector);
        ctf.closeOperatorEpoch(FIRST_EPOCH, CTF1);
        assertEq(_closeV2(CTF_FIRST_EPOCH, CTF1), 300);
        assertEq(_closeV2(CTF_FIRST_EPOCH, NO1), 0);
        assertEq(vault2.fundedThrough(CTF1, CTF_FIRST_EPOCH), 300);
        assertEq(vault2.fundedThrough(NO1, CTF_FIRST_EPOCH), 1_000);
        assertTrue(vault2.conservationHolds());
    }

    // ------------------------------------------------------------------
    // CS-8: registerOperatorPool
    // ------------------------------------------------------------------

    // CS-8: registerOperatorPool refuses an id from 2^32, so no provider pool
    // can take a CTF pool's id, and registers below it as before.
    function test_CS8_registerOperatorPoolRefusesCtfIds() public {
        vm.startPrank(owner);
        vm.expectRevert(STSettlement2Ctf.NoIdOutOfRange.selector);
        ctf.registerOperatorPool(CTF_BASE, V2_POOL2, V2_BURN_LIMIT, FIRST_EPOCH);
        vm.expectRevert(STSettlement2Ctf.NoIdOutOfRange.selector);
        ctf.registerOperatorPool(CTF2, V2_POOL2, V2_BURN_LIMIT, FIRST_EPOCH);
        vm.expectRevert(STSettlement2Ctf.NoIdOutOfRange.selector);
        ctf.registerOperatorPool(type(uint256).max, V2_POOL2, V2_BURN_LIMIT, FIRST_EPOCH);
        vm.stopPrank();
        _registerProviderPool2();
        assertEq(vault2.pools(NO2).hotkey, V2_POOL2);
        assertEq(ctf.ctfPoolId(CTF_BASE - 1), 2 * CTF_BASE - 1);
        vm.expectRevert(STSettlement2Ctf.NoIdOutOfRange.selector);
        ctf.ctfPoolId(CTF_BASE);
    }

    // ------------------------------------------------------------------
    // CS-9 and CS-10: the CTF pool's roots
    // ------------------------------------------------------------------

    // CS-9, S-6: the CTF pool's root signer is its operator's, the root signer
    // of operatorAt(n, epoch); another operator's signer and a stranger are
    // refused. Close, commit, approval, finalization and a claim run unchanged.
    function test_CS9_theOperatorsRootSignerCommitsTheCtfPoolsRoots() public {
        _registerProviderPool2();
        bytes32 hotkey = _registerCtf(NO1, CTF_FIRST_EPOCH, CTF1_UID);
        V2TreeHelper.Node[][] memory levels = V2TreeHelper.build(_keys(ALICE, BOB), _values(700, 300));
        _accrueV2(hotkey, 1_000);
        _closeV2(CTF_FIRST_EPOCH, CTF1);
        ISTSettlementVault2.Commitment memory commitment = _commitment(levels, 2);
        vm.prank(rootSigner2);
        vm.expectRevert(ISTSettlement2.Unauthorized.selector);
        ctf.commitDistribution(CTF_FIRST_EPOCH, CTF1, 0, commitment);
        vm.prank(stranger);
        vm.expectRevert(ISTSettlement2.Unauthorized.selector);
        ctf.commitDistribution(CTF_FIRST_EPOCH, CTF1, 0, commitment);
        vm.prank(rootSigner1);
        uint64 seq = ctf.commitDistribution(CTF_FIRST_EPOCH, CTF1, 0, commitment);
        uint256 gas = vm.lastCallGas().gasTotalUsed;
        assertEq(seq, 1);
        assertEq(vault2.pending(CTF1).root, commitment.root);

        vm.expectEmit(true, true, true, true, address(settlement));
        emit ISTSettlement2.DistributionApproved(CTF1, seq, auditor1, 1, 1);
        vm.prank(auditor1);
        ctf.approveDistribution(CTF1, seq, commitment.root, commitment.ledgerHash);
        assertEq(_finalizeV2(CTF1), 0);
        vm.prank(stranger);
        vault2.claim(CTF1, 0, _account(0, ALICE, 700), V2TreeHelper.proof(levels, 0));
        assertEq(staking.stakes(V2_ESCROW, ALICE), 700);
        assertTrue(vault2.conservationHolds());

        // NO2's CTF pool takes NO2's signer, not NO1's.
        bytes32 hotkey2 = _registerCtfAfter(NO2, CTF_FIRST_EPOCH + 2, CTF2_UID);
        _accrueV2(hotkey2, 500);
        _closeV2(CTF_FIRST_EPOCH + 2, CTF2);
        levels = V2TreeHelper.build(_keys(CAROL, BOB), _values(250, 250));
        commitment = _commitment(levels, 2);
        vm.prank(rootSigner1);
        vm.expectRevert(ISTSettlement2.Unauthorized.selector);
        ctf.commitDistribution(CTF_FIRST_EPOCH + 2, CTF2, 0, commitment);
        vm.prank(rootSigner2);
        ctf.commitDistribution(CTF_FIRST_EPOCH + 2, CTF2, 0, commitment);
        emit log_named_uint("commitDistribution of a CTF pool gas", gas);
    }

    // CS-10, S-6: the commit takes the signer of the epoch's operator version,
    // active or not; the window edges, a live pending root and pause apply as
    // for every pool, and an epoch before the CTF pool's first is refused.
    function test_CS10_ctfCommitWindowSignerVersionAndPause() public {
        bytes32 hotkey = _registerCtf(NO1, CTF_FIRST_EPOCH, CTF1_UID);
        address nextSigner = makeAddr("next-root-signer");
        vm.prank(owner);
        coordinator.scheduleOperator(NO1, DEPOSIT1, depositSigner1, nextSigner, false, CTF_FIRST_EPOCH + 1);
        V2TreeHelper.Node[][] memory levels = V2TreeHelper.build(_keys(ALICE, BOB), _values(600, 400));
        ISTSettlementVault2.Commitment memory commitment = _commitment(levels, 2);
        _accrueV2(hotkey, 1_000);
        _closeV2(CTF_FIRST_EPOCH, CTF1);

        vm.roll(_end(CTF_FIRST_EPOCH) - 1);
        vm.prank(rootSigner1);
        vm.expectRevert(ISTSettlement2.InvalidWindow.selector);
        ctf.commitDistribution(CTF_FIRST_EPOCH, CTF1, 0, commitment);
        vm.roll(_end(CTF_FIRST_EPOCH) + V2_ROOT_WINDOW + 1);
        vm.prank(rootSigner1);
        vm.expectRevert(ISTSettlement2.InvalidWindow.selector);
        ctf.commitDistribution(CTF_FIRST_EPOCH, CTF1, 0, commitment);
        vm.prank(rootSigner1);
        vm.expectRevert(ISTSettlement2.InvalidEpoch.selector);
        ctf.commitDistribution(FIRST_EPOCH, CTF1, 0, commitment);
        _pause();
        vm.roll(_end(CTF_FIRST_EPOCH) + V2_ROOT_WINDOW);
        vm.prank(rootSigner1);
        vm.expectRevert(ISTSettlement2.SettlementPaused.selector);
        ctf.commitDistribution(CTF_FIRST_EPOCH, CTF1, 0, commitment);
        vm.prank(owner);
        coordinator.setPaused(false);
        // The epoch's version: rootSigner1, though NO1 is inactive from the next.
        vm.prank(nextSigner);
        vm.expectRevert(ISTSettlement2.Unauthorized.selector);
        ctf.commitDistribution(CTF_FIRST_EPOCH, CTF1, 0, commitment);
        vm.prank(rootSigner1);
        ctf.commitDistribution(CTF_FIRST_EPOCH, CTF1, 0, commitment);
        // Until end(k + 1) the pending root is live.
        vm.prank(rootSigner1);
        vm.expectRevert(ISTSettlement2.PendingRootLive.selector);
        ctf.commitDistribution(CTF_FIRST_EPOCH, CTF1, 0, commitment);

        // Epoch k + 1 is the inactive version's: its signer commits, replacing
        // the root no approval can reach any more.
        _accrueV2(hotkey, 100);
        _closeV2(CTF_FIRST_EPOCH + 1, CTF1);
        levels = V2TreeHelper.build(_keys(ALICE, BOB), _values(650, 450));
        vm.prank(rootSigner1);
        vm.expectRevert(ISTSettlement2.Unauthorized.selector);
        ctf.commitDistribution(CTF_FIRST_EPOCH + 1, CTF1, 0, _commitment(levels, 2));
        vm.prank(nextSigner);
        assertEq(ctf.commitDistribution(CTF_FIRST_EPOCH + 1, CTF1, 0, _commitment(levels, 2)), 2);
        assertEq(vault2.pending(CTF1).epoch, CTF_FIRST_EPOCH + 1);
    }

    // ------------------------------------------------------------------
    // CS-11 to CS-15: the census commitment
    // ------------------------------------------------------------------

    // CS-11: NO1's root signer commits epoch k's census hash once, in
    // [end(k), end(k) + 600]; ctfCensus reads (hash, block), and
    // CtfCensusCommitted names both.
    function test_CS11_theRootSignerCommitsTheCensusOnceInsideItsWindow() public {
        _registerCtf(NO1, CTF_FIRST_EPOCH, CTF1_UID);
        uint256 end = _end(CTF_FIRST_EPOCH);
        (bytes32 hash, uint64 committed) = ctf.ctfCensus(NO1, CTF_FIRST_EPOCH);
        assertEq(hash, bytes32(0));
        assertEq(committed, 0);

        vm.roll(end - 1);
        vm.prank(rootSigner1);
        vm.expectRevert(ISTSettlement2.InvalidWindow.selector);
        ctf.commitCtfCensus(CTF_FIRST_EPOCH, NO1, CENSUS);
        vm.roll(end + CENSUS_WINDOW + 1);
        vm.prank(rootSigner1);
        vm.expectRevert(ISTSettlement2.InvalidWindow.selector);
        ctf.commitCtfCensus(CTF_FIRST_EPOCH, NO1, CENSUS);

        vm.roll(end + CENSUS_WINDOW);
        vm.expectEmit(true, true, true, true, address(settlement));
        emit STSettlement2Ctf.CtfCensusCommitted(NO1, CTF_FIRST_EPOCH, CENSUS, uint64(end + CENSUS_WINDOW));
        vm.prank(rootSigner1);
        ctf.commitCtfCensus(CTF_FIRST_EPOCH, NO1, CENSUS);
        uint256 gas = vm.lastCallGas().gasTotalUsed;
        (hash, committed) = ctf.ctfCensus(NO1, CTF_FIRST_EPOCH);
        assertEq(hash, CENSUS);
        assertEq(committed, end + CENSUS_WINDOW);
        assertEq(CENSUS_WINDOW, 600);

        // Write-once: the same epoch again, with any hash, is refused.
        vm.prank(rootSigner1);
        vm.expectRevert(STSettlement2Ctf.CensusAlreadyCommitted.selector);
        ctf.commitCtfCensus(CTF_FIRST_EPOCH, NO1, OTHER_CENSUS);
        vm.prank(rootSigner1);
        vm.expectRevert(STSettlement2Ctf.CensusAlreadyCommitted.selector);
        ctf.commitCtfCensus(CTF_FIRST_EPOCH, NO1, CENSUS);
        (hash, committed) = ctf.ctfCensus(NO1, CTF_FIRST_EPOCH);
        assertEq(hash, CENSUS);
        assertEq(committed, end + CENSUS_WINDOW);

        // The next epoch's window opens at its own end, from its first block.
        uint256 next = _end(CTF_FIRST_EPOCH + 1);
        vm.roll(next);
        vm.prank(rootSigner1);
        ctf.commitCtfCensus(CTF_FIRST_EPOCH + 1, NO1, OTHER_CENSUS);
        (hash, committed) = ctf.ctfCensus(NO1, CTF_FIRST_EPOCH + 1);
        assertEq(hash, OTHER_CENSUS);
        assertEq(committed, next);
        emit log_named_uint("commitCtfCensus gas", gas);
    }

    // CS-12: only the root signer of operatorAt(n, k), active or not, commits;
    // another operator's signer, the auditor, the owner and a stranger are
    // refused.
    function test_CS12_onlyTheEpochsRootSignerCommitsTheCensus() public {
        _registerCtf(NO1, CTF_FIRST_EPOCH, CTF1_UID);
        address nextSigner = makeAddr("next-root-signer");
        vm.prank(owner);
        coordinator.scheduleOperator(NO1, DEPOSIT1, depositSigner1, nextSigner, false, CTF_FIRST_EPOCH + 1);
        vm.roll(_end(CTF_FIRST_EPOCH));
        address[5] memory refused = [rootSigner2, auditor1, owner, stranger, nextSigner];
        for (uint256 i; i < refused.length; i++) {
            vm.prank(refused[i]);
            vm.expectRevert(ISTSettlement2.Unauthorized.selector);
            ctf.commitCtfCensus(CTF_FIRST_EPOCH, NO1, CENSUS);
        }
        vm.prank(rootSigner1);
        ctf.commitCtfCensus(CTF_FIRST_EPOCH, NO1, CENSUS);
        // The next epoch is the inactive version's: its signer commits.
        vm.roll(_end(CTF_FIRST_EPOCH + 1));
        vm.prank(rootSigner1);
        vm.expectRevert(ISTSettlement2.Unauthorized.selector);
        ctf.commitCtfCensus(CTF_FIRST_EPOCH + 1, NO1, CENSUS);
        vm.prank(nextSigner);
        ctf.commitCtfCensus(CTF_FIRST_EPOCH + 1, NO1, CENSUS);
    }

    // CS-13: a census needs the CTF pool registered, an epoch at or after its
    // first epoch, an id below 2^32 and a nonzero hash.
    function test_CS13_theCensusNeedsTheCtfPoolItsEpochAndAHash() public {
        _registerProviderPool2();
        _registerCtf(NO1, CTF_FIRST_EPOCH, CTF1_UID);
        vm.roll(_end(CTF_FIRST_EPOCH));
        // NO2 has a provider pool but no CTF pool.
        vm.prank(rootSigner2);
        vm.expectRevert(ISTSettlement2.UnknownPool.selector);
        ctf.commitCtfCensus(CTF_FIRST_EPOCH, NO2, CENSUS);
        vm.prank(rootSigner1);
        vm.expectRevert(STSettlement2Ctf.NoIdOutOfRange.selector);
        ctf.commitCtfCensus(CTF_FIRST_EPOCH, CTF1, CENSUS);

        vm.roll(_end(FIRST_EPOCH));
        vm.prank(rootSigner1);
        vm.expectRevert(ISTSettlement2.InvalidEpoch.selector);
        ctf.commitCtfCensus(FIRST_EPOCH, NO1, CENSUS);
        vm.roll(_end(CTF_FIRST_EPOCH));
        vm.prank(rootSigner1);
        vm.expectRevert(STSettlement2Ctf.InvalidCensusHash.selector);
        ctf.commitCtfCensus(CTF_FIRST_EPOCH, NO1, bytes32(0));
        vm.prank(rootSigner1);
        ctf.commitCtfCensus(CTF_FIRST_EPOCH, NO1, CENSUS);
    }

    // CS-14, S5: pause stops census commitments; the window does not wait.
    function test_CS14_pauseStopsTheCensus() public {
        _registerCtf(NO1, CTF_FIRST_EPOCH, CTF1_UID);
        vm.roll(_end(CTF_FIRST_EPOCH));
        _pause();
        vm.prank(rootSigner1);
        vm.expectRevert(ISTSettlement2.SettlementPaused.selector);
        ctf.commitCtfCensus(CTF_FIRST_EPOCH, NO1, CENSUS);
        vm.prank(owner);
        coordinator.setPaused(false);
        vm.prank(rootSigner1);
        ctf.commitCtfCensus(CTF_FIRST_EPOCH, NO1, CENSUS);

        // A pause that spans the end of the next epoch's window cancels its
        // census: unpaused after it, the window has closed.
        uint256 end = _end(CTF_FIRST_EPOCH + 1);
        vm.roll(end);
        _pause();
        vm.roll(end + CENSUS_WINDOW + 1);
        vm.prank(owner);
        coordinator.setPaused(false);
        vm.prank(rootSigner1);
        vm.expectRevert(ISTSettlement2.InvalidWindow.selector);
        ctf.commitCtfCensus(CTF_FIRST_EPOCH + 1, NO1, CENSUS);
        (bytes32 hash, uint64 committed) = ctf.ctfCensus(NO1, CTF_FIRST_EPOCH + 1);
        assertEq(hash, bytes32(0));
        assertEq(committed, 0);
    }

    // CS-15: each operator's census is its own, and an epoch whose window
    // passed without one stays empty.
    function test_CS15_censusesAreKeyedByOperatorAndEpoch() public {
        _registerProviderPool2();
        _registerCtf(NO1, CTF_FIRST_EPOCH, CTF1_UID);
        _registerCtf(NO2, CTF_FIRST_EPOCH, CTF2_UID);
        vm.roll(_end(CTF_FIRST_EPOCH));
        vm.prank(rootSigner2);
        ctf.commitCtfCensus(CTF_FIRST_EPOCH, NO2, OTHER_CENSUS);
        vm.roll(_end(CTF_FIRST_EPOCH + 1) + 1);
        vm.prank(rootSigner1);
        ctf.commitCtfCensus(CTF_FIRST_EPOCH + 1, NO1, CENSUS);
        (bytes32 hash, uint64 committed) = ctf.ctfCensus(NO1, CTF_FIRST_EPOCH);
        assertEq(hash, bytes32(0));
        assertEq(committed, 0);
        (hash, committed) = ctf.ctfCensus(NO2, CTF_FIRST_EPOCH);
        assertEq(hash, OTHER_CENSUS);
        assertEq(committed, _end(CTF_FIRST_EPOCH));
        (hash,) = ctf.ctfCensus(NO1, CTF_FIRST_EPOCH + 1);
        assertEq(hash, CENSUS);
        (hash,) = ctf.ctfCensus(NO2, CTF_FIRST_EPOCH + 1);
        assertEq(hash, bytes32(0));
        // The window of epoch CTF_FIRST_EPOCH closed without NO1's census.
        vm.prank(rootSigner1);
        vm.expectRevert(ISTSettlement2.InvalidWindow.selector);
        ctf.commitCtfCensus(CTF_FIRST_EPOCH, NO1, CENSUS);
    }

    // ------------------------------------------------------------------
    // CS-16: the views
    // ------------------------------------------------------------------

    // CS-16, S-4: ctfSettlementAt answers (vault2, hotkey, first epoch) from
    // the CTF pool's first epoch and zeros before it, for an unregistered pool
    // and for an id from 2^32; settlementAt answers the CTF pool as a v2 pool.
    function test_CS16_ctfSettlementAtResolvesTheCtfPool() public {
        _assertCtfSettlement(NO1, CTF_FIRST_EPOCH, address(0), bytes32(0), 0);
        bytes32 hotkey = _registerCtf(NO1, CTF_FIRST_EPOCH, CTF1_UID);
        _assertCtfSettlement(NO1, CTF_FIRST_EPOCH - 1, address(0), bytes32(0), 0);
        _assertCtfSettlement(NO1, CTF_FIRST_EPOCH, address(vault2), hotkey, CTF_FIRST_EPOCH);
        _assertCtfSettlement(NO1, CTF_FIRST_EPOCH + 100, address(vault2), hotkey, CTF_FIRST_EPOCH);
        _assertCtfSettlement(NO2, CTF_FIRST_EPOCH, address(0), bytes32(0), 0);
        _assertCtfSettlement(CTF1, CTF_FIRST_EPOCH, address(0), bytes32(0), 0);
        _assertCtfSettlement(type(uint256).max, CTF_FIRST_EPOCH, address(0), bytes32(0), 0);
        (address v, bytes32 poolHotkey, uint8 version) = ctf.settlementAt(CTF1, CTF_FIRST_EPOCH);
        assertEq(v, address(vault2));
        assertEq(poolHotkey, hotkey);
        assertEq(version, 2);
        (v, poolHotkey, version) = ctf.settlementAt(NO1, CTF_FIRST_EPOCH);
        assertEq(poolHotkey, V2_POOL1);
        assertEq(ctf.ctfPoolId(NO1), CTF1);
        assertEq(ctf.ctfPoolId(0), CTF_BASE);
    }

    function _assertCtfSettlement(uint256 noId, uint256 epoch_, address v, bytes32 hotkey, uint64 firstEpoch)
        internal
        view
    {
        (address gotVault, bytes32 gotHotkey, uint64 gotFirst) = ctf.ctfSettlementAt(noId, epoch_);
        assertEq(gotVault, v);
        assertEq(gotHotkey, hotkey);
        assertEq(gotFirst, firstEpoch);
    }

    /// @dev Registers a CTF pool later, from a block where `firstEpoch` is
    ///      still in the future.
    function _registerCtfAfter(uint256 noId, uint64 firstEpoch, uint16 uid) internal returns (bytes32) {
        uint256 back = block.number;
        vm.roll(coordinator.epochStartBlock(firstEpoch) - 1);
        bytes32 hotkey = _registerCtf(noId, firstEpoch, uid);
        if (back > block.number) vm.roll(back);
        return hotkey;
    }
}
